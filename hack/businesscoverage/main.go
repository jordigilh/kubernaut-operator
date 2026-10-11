// Command businesscoverage enforces zero missed statements and inventories
// missing handwritten functions in the independent unit profile.
package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type coverageBlock struct {
	Path       string
	Start      token.Position
	End        token.Position
	Statements uint64
	Executed   bool
}

type functionScope struct {
	Path           string
	Name           string
	Start          token.Position
	End            token.Position
	Classification string
}

type coverageSummary struct {
	Statements uint64
	Missed     uint64
}

const businessClassification = "business"

// Runtime bootstrap/registration is not business logic. Every other handwritten
// function is included, even if absent from the profile. No resource, policy,
// webhook or controller logic is exempt. Wiring is independently tested.
var infrastructureFunctions = map[string]string{
	"cmd/main.go:init":                           "scheme registration",
	"cmd/main.go:main":                           "process lifecycle; requires separate positive wiring and integration",
	"api/v1alpha2/kubernaut_types.go:init":       "scheme registration",
	"api/v1alpha2/groupversion_info.go:Register": "scheme registration",
}

var blockPattern = regexp.MustCompile(`^(.+):([0-9]+)\.([0-9]+),([0-9]+)\.([0-9]+) ([0-9]+) ([0-9]+)$`)

func readProfile(input io.Reader) ([]coverageBlock, error) {
	scanner := bufio.NewScanner(input)
	if !scanner.Scan() {
		return nil, fmt.Errorf("coverage header missing")
	}
	header := scanner.Text()
	if header != "mode: set" && header != "mode: count" && header != "mode: atomic" {
		return nil, fmt.Errorf("unsupported coverage header %q", header)
	}
	blocks := make([]coverageBlock, 0, 1024)
	for scanner.Scan() {
		block, err := parseBlock(scanner.Text())
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading coverage: %w", err)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("coverage blocks missing")
	}
	return blocks, nil
}

func parsePosition(line, column string) (token.Position, error) {
	l, err := strconv.Atoi(line)
	if err != nil || l < 1 {
		return token.Position{}, fmt.Errorf("invalid coverage line %q", line)
	}
	c, err := strconv.Atoi(column)
	if err != nil || c < 1 {
		return token.Position{}, fmt.Errorf("invalid coverage column %q", column)
	}
	return token.Position{Line: l, Column: c}, nil
}

func parseBlock(line string) (coverageBlock, error) {
	match := blockPattern.FindStringSubmatch(line)
	if len(match) != 8 {
		return coverageBlock{}, fmt.Errorf("malformed coverage row %q", line)
	}
	start, err := parsePosition(match[2], match[3])
	if err != nil {
		return coverageBlock{}, err
	}
	end, err := parsePosition(match[4], match[5])
	if err != nil || !positionAtOrBefore(start, end) {
		return coverageBlock{}, fmt.Errorf("invalid coverage range in %q", line)
	}
	statements, err := strconv.ParseUint(match[6], 10, 64)
	if err != nil {
		return coverageBlock{}, fmt.Errorf("invalid statement count: %w", err)
	}
	executions, err := strconv.ParseUint(match[7], 10, 64)
	if err != nil {
		return coverageBlock{}, fmt.Errorf("invalid execution count: %w", err)
	}
	return coverageBlock{Path: match[1], Start: start, End: end, Statements: statements, Executed: executions > 0}, nil
}

func positionAtOrBefore(first, second token.Position) bool {
	return first.Line < second.Line || (first.Line == second.Line && first.Column <= second.Column)
}

func blockKey(block coverageBlock) string {
	return fmt.Sprintf("%s:%d.%d,%d.%d", block.Path,
		block.Start.Line, block.Start.Column, block.End.Line, block.End.Column)
}

func unionBlocks(blocks []coverageBlock) map[string]coverageBlock {
	unique := make(map[string]coverageBlock, len(blocks))
	for _, block := range blocks {
		key := blockKey(block)
		block.Executed = block.Executed || unique[key].Executed
		unique[key] = block
	}
	return unique
}

func summarize(blocks []coverageBlock) coverageSummary {
	report := coverageSummary{}
	for _, block := range unionBlocks(blocks) {
		report.Statements += block.Statements
		if !block.Executed {
			report.Missed += block.Statements
		}
	}
	return report
}

func uncoveredBlocks(blocks []coverageBlock) []coverageBlock {
	missed := make([]coverageBlock, 0)
	for _, block := range unionBlocks(blocks) {
		if !block.Executed && block.Statements > 0 {
			missed = append(missed, block)
		}
	}
	slices.SortFunc(missed, func(first, second coverageBlock) int {
		if order := cmp.Compare(first.Path, second.Path); order != 0 {
			return order
		}
		if order := cmp.Compare(first.Start.Line, second.Start.Line); order != 0 {
			return order
		}
		return cmp.Compare(first.Start.Column, second.Start.Column)
	})
	return missed
}

// A compile-only profile establishes the complete instrumentation independently
// from execution. One covered block cannot hide the rest of an untested function.
func validateSchema(blocks, baseline []coverageBlock) error {
	expected := make(map[string]uint64, len(baseline))
	for _, block := range baseline {
		key := blockKey(block)
		if previous, exists := expected[key]; exists && previous != block.Statements {
			return fmt.Errorf("inconsistent baseline instrumentation: %s", key)
		}
		expected[key] = block.Statements
	}
	seen := make(map[string]bool, len(blocks))
	for _, block := range blocks {
		key := blockKey(block)
		statements, exists := expected[key]
		if !exists || statements != block.Statements {
			return fmt.Errorf("coverage instrumentation differs from current source: %s", key)
		}
		seen[key] = true
	}
	for key := range expected {
		if !seen[key] {
			return fmt.Errorf("missing instrumentation block: %s", key)
		}
	}
	return nil
}

func sourceFunctions(set *token.FileSet, path string, source []byte) ([]functionScope, error) {
	file, err := parser.ParseFile(set, path, source, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("inventory %s: %w", path, err)
	}
	if ast.IsGenerated(file) {
		return nil, nil
	}
	return scopesForFile(set, path, file), nil
}

func scopesForFile(set *token.FileSet, path string, file *ast.File) []functionScope {
	functions := make([]functionScope, 0, len(file.Decls))
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || len(function.Body.List) == 0 {
			continue
		}
		classification := businessClassification
		if reason, infrastructure := infrastructureFunctions[path+":"+function.Name.Name]; infrastructure {
			classification = reason
		}
		functions = append(functions, functionScope{
			Path: path, Name: function.Name.Name, Start: set.Position(function.Body.Pos()),
			End: set.Position(function.Body.End()), Classification: classification,
		})
	}
	return functions
}

func containsBlock(function functionScope, block coverageBlock) bool {
	return function.Path == block.Path &&
		positionAtOrBefore(function.Start, block.Start) && positionAtOrBefore(block.End, function.End)
}

func missingFunctions(functions []functionScope, blocks []coverageBlock) []functionScope {
	missing := make([]functionScope, 0)
	for _, function := range functions {
		if function.Classification != businessClassification {
			continue
		}
		found := false
		for _, block := range blocks {
			if containsBlock(function, block) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, function)
		}
	}
	return missing
}

func inventory(root string) ([]functionScope, map[string]bool, error) {
	functions := make([]functionScope, 0, 512)
	handwritten := make(map[string]bool, 64)
	set := token.NewFileSet()
	for _, directory := range []string{"api", "internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			return inventoryFile(root, path, set, &functions, handwritten)
		})
		if err != nil {
			return nil, nil, fmt.Errorf("inventory %s: %w", directory, err)
		}
	}
	return functions, handwritten, nil
}

func inventoryFile(root, path string, set *token.FileSet,
	functions *[]functionScope, handwritten map[string]bool) error {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	source, err := os.ReadFile(path) //nolint:gosec // path comes from local WalkDir, never remote input
	if err != nil {
		return err
	}
	relative = filepath.ToSlash(relative)
	file, err := parser.ParseFile(set, relative, source, parser.ParseComments)
	if err != nil {
		return err
	}
	if ast.IsGenerated(file) {
		return nil
	}
	handwritten[relative] = true
	*functions = append(*functions, scopesForFile(set, relative, file)...)
	return nil
}

func scopedBlocks(blocks []coverageBlock, functions []functionScope,
	handwritten map[string]bool, module string) []coverageBlock {
	selected := make([]coverageBlock, 0, len(blocks))
	for _, block := range blocks {
		block.Path = strings.TrimPrefix(block.Path, module+"/")
		if !handwritten[block.Path] {
			continue
		}
		excluded := false
		for _, function := range functions {
			if function.Classification != businessClassification && containsBlock(function, block) {
				excluded = true
				break
			}
		}
		if !excluded {
			selected = append(selected, block)
		}
	}
	return selected
}

func loadProfile(path string) ([]coverageBlock, error) {
	input, err := os.Open(path) //nolint:gosec // explicit user-selected local coverage profile
	if err != nil {
		return nil, fmt.Errorf("open coverage: %w", err)
	}
	blocks, parseErr := readProfile(input)
	closeErr := input.Close()
	if parseErr != nil {
		return nil, parseErr
	}
	return blocks, closeErr
}

func run(root, profile, baselinePath, reportPath string) error {
	functions, handwritten, err := inventory(root)
	if err != nil {
		return err
	}
	blocks, err := loadProfile(profile)
	if err != nil {
		return err
	}
	baseline, err := loadProfile(baselinePath)
	if err != nil {
		return fmt.Errorf("compile-only instrumentation baseline: %w", err)
	}
	if err := validateSchema(blocks, baseline); err != nil {
		return err
	}
	blocks = scopedBlocks(blocks, functions, handwritten, "github.com/jordigilh/kubernaut-operator")
	missing := missingFunctions(functions, blocks)
	summary := summarize(blocks)
	if reportPath != "" {
		if err := writeReport(reportPath, summary, functions, missing, blocks); err != nil {
			return err
		}
	}
	fmt.Printf("Independent business-unit statements: %d; missed: %d; missing functions: %d (target: zero misses)\n",
		summary.Statements, summary.Missed, len(missing))
	if summary.Statements == 0 || summary.Missed > 0 || len(missing) > 0 {
		return fmt.Errorf("100%% business-unit coverage is not established; do not substitute integration coverage")
	}
	return nil
}

func writeReport(path string, summary coverageSummary,
	functions, missing []functionScope, blocks []coverageBlock) error {
	missed := uncoveredBlocks(blocks)
	data, err := json.MarshalIndent(struct {
		Summary   coverageSummary
		Inventory []functionScope
		Missing   []functionScope
		Uncovered []coverageBlock
	}{summary, functions, missing, missed}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func main() {
	root := flag.String("root", ".", "repository root")
	baseline := flag.String("baseline", "cover-unit-baseline.out", "compile-only instrumentation profile")
	report := flag.String("report", "", "optional detailed JSON inventory and uncovered blocks")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: businesscoverage [-root directory] [-report path] independent-unit.out")
		os.Exit(2)
	}
	if err := run(*root, flag.Arg(0), *baseline, *report); err != nil {
		fmt.Fprintln(os.Stderr, "business unit coverage validation failed:", err)
		os.Exit(1)
	}
}

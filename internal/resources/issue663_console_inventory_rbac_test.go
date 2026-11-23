/*
Copyright 2026 Jordi Gil.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
)

// pinnedKubernautModuleDir resolves the kubernaut module root from the
// operator's build list. It deliberately avoids importing the upstream
// package: policyread carries dependencies the operator does not otherwise
// need, and only the source file is inspected.
func pinnedKubernautModuleDir() string {
	out, err := exec.CommandContext(context.Background(), "go", "list", "-m", "-f", "{{.Dir}}", "github.com/jordigilh/kubernaut").Output()
	Expect(err).NotTo(HaveOccurred(), "resolving the pinned kubernaut module directory")
	dir := strings.TrimSpace(string(out))
	Expect(dir).NotTo(BeEmpty())
	return dir
}

// allowlistedResourceGroups parses DefaultLocalResourceKinds out of the
// pinned module's policyread source and returns {apiGroup: [resources]}.
func allowlistedResourceGroups(moduleDir string) map[string][]string {
	path := filepath.Join(moduleDir, "pkg", "apifrontend", "policyread", "local_inventory.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	Expect(err).NotTo(HaveOccurred(), "parsing pinned upstream allowlist at %s", path)

	entries := defaultLocalResourceKindEntries(file)
	Expect(entries).NotTo(BeEmpty(),
		"DefaultLocalResourceKinds not found as a composite literal in %s", path)

	groups := make(map[string][]string, len(entries))
	for _, fields := range entries {
		Expect(fields).To(HaveKey("Group"), "allowlist entry without a decoded Group literal")
		Expect(fields).To(HaveKey("Resource"), "allowlist entry without a decoded Resource literal")
		resource := fields["Resource"]
		Expect(resource).NotTo(BeEmpty(), "allowlist entry with an empty Resource literal")
		groups[fields["Group"]] = append(groups[fields["Group"]], resource)
	}
	return groups
}

// defaultLocalResourceKindEntries returns the decoded field maps of every
// element in the DefaultLocalResourceKinds composite literal. Empty when the
// declaration is absent or not a composite literal (the caller fails the
// spec with a clear message).
func defaultLocalResourceKindEntries(file *ast.File) []map[string]string {
	for _, declaration := range file.Decls {
		gen, isGenDecl := declaration.(*ast.GenDecl)
		if !isGenDecl || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, isValueSpec := spec.(*ast.ValueSpec)
			if !isValueSpec {
				continue
			}
			if entries := entriesInValueSpec(valueSpec); len(entries) > 0 {
				return entries
			}
		}
	}
	return nil
}

// entriesInValueSpec finds the DefaultLocalResourceKinds name in one var
// specification and decodes its composite literal elements.
func entriesInValueSpec(valueSpec *ast.ValueSpec) []map[string]string {
	for index, name := range valueSpec.Names {
		if name.Name != "DefaultLocalResourceKinds" || index >= len(valueSpec.Values) {
			continue
		}
		composite, isComposite := valueSpec.Values[index].(*ast.CompositeLit)
		if !isComposite {
			return nil
		}
		return decodeCompositeEntries(composite)
	}
	return nil
}

// decodeCompositeEntries decodes struct-literal elements into field maps.
// It fails closed: an element that is not a struct literal is an upstream
// shape this guard does not understand, and silently skipping it could make
// the drift check pass while the rendered role omits a new kind.
func decodeCompositeEntries(composite *ast.CompositeLit) []map[string]string {
	entries := make([]map[string]string, 0, len(composite.Elts))
	for _, element := range composite.Elts {
		entry, isComposite := element.(*ast.CompositeLit)
		if !isComposite {
			Fail("DefaultLocalResourceKinds contains an element this guard cannot decode; " +
				"extend the parser before changing the role")
		}
		fields := make(map[string]string, len(entry.Elts))
		for _, field := range entry.Elts {
			key, value, isStringField := stringField(field)
			if isStringField {
				fields[key] = value
			}
		}
		entries = append(entries, fields)
	}
	return entries
}

// stringField decodes a `Field: "literal"` struct-literal element.
func stringField(field ast.Expr) (string, string, bool) {
	keyValue, isKeyValue := field.(*ast.KeyValueExpr)
	if !isKeyValue {
		return "", "", false
	}
	key, isIdent := keyValue.Key.(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	literal, isBasicLit := keyValue.Value.(*ast.BasicLit)
	if !isBasicLit {
		return "", "", false
	}
	return key.Name, strings.Trim(literal.Value, `"`), true
}

// Issue #663: user-side local-target Kubernetes grants for the console
// managed-inventory projection.
//
// AF authorizes a console user's local-target reads through
// ResourceAttributes SubjectAccessReviews AS THE INVOKING USER (not the AF
// ServiceAccount): list reads check verb "list" per allowlisted kind (with
// the caller's namespace/name filters when present), exact reads check verb
// "get" with the exact group/version/resource/namespace/name (kubernaut
// pkg/apifrontend/policyread service.go authorize/authorizeTarget against
// DefaultLocalResourceKinds, pinned at
// v1.6.0-rc9.0.20261120145050-6bd88d5dc344). The allowlist is upstream-fixed:
// 11 Gateway-aligned kinds, secrets and configmaps intentionally excluded.
// The golden matrix below pins the rendered grant, and a companion spec
// parses the pinned module's actual DefaultLocalResourceKinds so an upstream
// allowlist drift (for example a kubernaut version bump) fails loudly too.
//
// Deliberate boundaries (#663):
//   - the role is rendered but never bound by the operator; which users or
//     groups hold it is the administrator's decision (issue #662 tracks the
//     upstream persona↔capability mapping and stays unresolved here);
//   - no namespaced Role is generated per managed scope: the projected
//     namespace set is deployment-specific, so narrower scoping is done by
//     the administrator binding this ClusterRole through a namespaced
//     RoleBinding (namespace-filtered reads, namespaced kinds only) or their
//     own role.
var _ = Describe("Issue #663: console inventory user-side target grants", func() {
	// Golden matrix mirrors upstream DefaultLocalResourceKinds exactly
	// (pinned module version above). Three rules, one per API group,
	// following the gatewayTargetResourceReadRules grouping style.
	goldenRules := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"pods", "services", "nodes", "persistentvolumes", "namespaces"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "statefulsets", "daemonsets", "replicasets"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list"}},
	}

	findRuleForGroup := func(rules []rbacv1.PolicyRule, group string) *rbacv1.PolicyRule {
		for i := range rules {
			if slices.Contains(rules[i].APIGroups, group) {
				return &rules[i]
			}
		}
		return nil
	}

	Describe("ConsoleInventoryReaderClusterRole", func() {
		It("grants exactly get+list on the 11 upstream-allowlisted kinds", func() {
			kn := testKubernautWithAF()
			role := ConsoleInventoryReaderClusterRole(kn)
			Expect(role).NotTo(BeNil())
			Expect(role.Name).To(Equal(kn.Namespace + "-console-inventory-reader"))
			Expect(role.Namespace).To(BeEmpty(), "role is cluster-scoped by design")
			Expect(role.Rules).To(HaveLen(len(goldenRules)),
				"one rule per API group, mirroring the upstream allowlist grouping")
			for _, golden := range goldenRules {
				rule := findRuleForGroup(role.Rules, golden.APIGroups[0])
				Expect(rule).NotTo(BeNil(), "missing rule for API group %q", golden.APIGroups[0])
				Expect(rule.Resources).To(ConsistOf(golden.Resources),
					"API group %q resource set drifted from the pinned upstream allowlist", golden.APIGroups[0])
				Expect(rule.Verbs).To(ConsistOf("get", "list"),
					"the projection performs exactly list and exact-get SARs; watch and everything else is denied")
				Expect(rule.ResourceNames).To(BeEmpty(),
					"resourceNames would break name-filtered list SARs and exact-get SARs on unnamed objects")
				Expect(rule.NonResourceURLs).To(BeEmpty())
			}
		})

		It("returns nil when AF is disabled", func() {
			kn := testKubernautWithAF()
			disabled := false
			kn.Spec.APIFrontend.Enabled = &disabled
			Expect(ConsoleInventoryReaderClusterRole(kn)).To(BeNil())
		})

		It("is rendered by the ClusterRoles aggregator and dropped when AF is disabled", func() {
			kn := testKubernautWithAF()
			roles := ClusterRoles(kn, testKnV2(kn))
			names := make([]string, 0, len(roles))
			for _, role := range roles {
				Expect(role).NotTo(BeNil(), "the aggregator must never contain nil entries")
				names = append(names, role.Name)
			}
			Expect(names).To(ContainElement(kn.Namespace + "-console-inventory-reader"))

			disabled := false
			kn.Spec.APIFrontend.Enabled = &disabled
			for _, role := range ClusterRoles(kn, testKnV2(kn)) {
				Expect(role).NotTo(BeNil(), "AF-disabled aggregation must not contain nil entries")
				Expect(role.Name).NotTo(Equal(kn.Namespace+"-console-inventory-reader"),
					"AF-disabled aggregation must drop the console-inventory-reader role")
			}
		})
	})

	Describe("authorization boundary of the rendered inventory role", func() {
		// ruleAllows mirrors Kubernetes RBAC matching for the ResourceAttributes
		// subset AF actually reviews (apiGroup, resource, verb). resourceNames is
		// intentionally irrelevant: the rendered rules carry none.
		ruleAllows := func(rules []rbacv1.PolicyRule, group, resource, verb string) bool {
			for _, rule := range rules {
				if !slices.Contains(rule.APIGroups, group) && !slices.Contains(rule.APIGroups, "*") {
					continue
				}
				if !slices.Contains(rule.Resources, resource) && !slices.Contains(rule.Resources, "*") {
					continue
				}
				if !slices.Contains(rule.Verbs, verb) && !slices.Contains(rule.Verbs, "*") {
					continue
				}
				return true
			}
			return false
		}

		It("allows every local-target SAR the projection performs and denies everything else", func() {
			rules := ConsoleInventoryReaderClusterRole(testKubernautWithAF()).Rules

			By("allowing list and exact-get on each allowlisted kind")
			for _, golden := range goldenRules {
				for _, resource := range golden.Resources {
					Expect(ruleAllows(rules, golden.APIGroups[0], resource, "list")).
						To(BeTrue(), "list on %s must be allowed", resource)
					Expect(ruleAllows(rules, golden.APIGroups[0], resource, "get")).
						To(BeTrue(), "exact get on %s must be allowed", resource)
				}
			}

			By("denying kinds upstream excludes from the allowlist")
			Expect(ruleAllows(rules, "", "secrets", "get")).To(BeFalse())
			Expect(ruleAllows(rules, "", "configmaps", "get")).To(BeFalse())
			Expect(ruleAllows(rules, "", "persistentvolumeclaims", "list")).To(BeFalse())
			Expect(ruleAllows(rules, "autoscaling", "horizontalpodautoscalers", "list")).To(BeFalse())

			By("denying every verb beyond get/list")
			for _, verb := range []string{"watch", "create", "update", "patch", "delete", "deletecollection"} {
				Expect(ruleAllows(rules, "", "pods", verb)).To(BeFalse())
				Expect(ruleAllows(rules, "apps", "deployments", verb)).To(BeFalse())
			}

			By("denying kubernaut.ai tool/capability and console-gate authorization")
			Expect(ruleAllows(rules, kubernautAPIGroup, "tools", "use")).To(BeFalse(),
				"the inventory role must not imply any console capability; that is console-reader's job")
			Expect(ruleAllows(rules, kubernautAPIGroup, "console", "use")).To(BeFalse())
		})

		It("keeps the inventory role disjoint from the capability role", func() {
			inventoryRules := ConsoleInventoryReaderClusterRole(testKubernautWithAF()).Rules
			readerRules := ConsoleReaderClusterRole(testKubernautWithAF()).Rules
			Expect(ruleAllows(inventoryRules, kubernautAPIGroup, "tools", "use")).To(BeFalse())
			for _, golden := range goldenRules {
				for _, resource := range golden.Resources {
					Expect(ruleAllows(readerRules, golden.APIGroups[0], resource, "get")).To(BeFalse(),
						"console-reader must not satisfy local-target SARs")
				}
			}
		})
	})

	Describe("ConsoleInventoryReaderClusterRoleName", func() {
		It("is namespace-prefixed for multi-instance safety", func() {
			kn := testKubernautWithAF()
			Expect(ConsoleInventoryReaderClusterRoleName(kn)).
				To(Equal(kn.Namespace + "-console-inventory-reader"))
			Expect(strings.HasPrefix(ConsoleInventoryReaderClusterRoleName(kn), kn.Namespace+"-")).To(BeTrue())
		})
	})

	Describe("upstream allowlist drift guard", func() {
		// The golden matrix above is a hand copy. This spec closes the drift
		// gap: it resolves the pinned kubernaut module from the build list
		// (no extra dependency, no package import -- policyread drags in
		// dependencies the operator does not need) and parses its actual
		// DefaultLocalResourceKinds declaration. A kubernaut bump that widens
		// or narrows the allowlist therefore fails this test until the
		// rendered role is deliberately updated.
		It("renders exactly the kinds in the pinned module's DefaultLocalResourceKinds", func() {
			moduleDir := pinnedKubernautModuleDir()
			resourceGroups := allowlistedResourceGroups(moduleDir)

			kindCount := 0
			for _, resources := range resourceGroups {
				kindCount += len(resources)
			}
			Expect(kindCount).To(Equal(11),
				"parsed an unexpected number of allowlisted kinds from the pinned kubernaut module; "+
					"if upstream renamed DefaultLocalResourceKinds, update this guard together with the role")

			role := ConsoleInventoryReaderClusterRole(testKubernautWithAF())
			Expect(role).NotTo(BeNil())

			rendered := make(map[string][]string, len(role.Rules))
			for _, rule := range role.Rules {
				Expect(rule.APIGroups).To(HaveLen(1), "one API group per rendered rule keeps the comparison unambiguous")
				rendered[rule.APIGroups[0]] = rule.Resources
			}

			expectedGroups := make([]string, 0, len(resourceGroups))
			for group := range resourceGroups {
				expectedGroups = append(expectedGroups, group)
			}
			sort.Strings(expectedGroups)
			renderedGroups := make([]string, 0, len(rendered))
			for group := range rendered {
				renderedGroups = append(renderedGroups, group)
			}
			sort.Strings(renderedGroups)
			Expect(renderedGroups).To(Equal(expectedGroups),
				"rendered API groups drifted from the pinned upstream allowlist")

			for group, resources := range resourceGroups {
				sort.Strings(resources)
				got := append([]string(nil), rendered[group]...)
				sort.Strings(got)
				Expect(got).To(Equal(resources),
					"rendered resources for API group %q drifted from the pinned upstream allowlist", group)
			}
		})
	})
})

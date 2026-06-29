# Kubernaut Operator Development Methodology

This file is the **single authoritative source** for Kubernaut Operator's development methodology. Every contributor -- human or AI agent, regardless of IDE or tooling -- must follow these rules.

For Cursor-specific implementation patterns (code examples, contextual snippets), see [`.cursor/rules/`](.cursor/rules/). Those files supplement this document but never override it.

---

## Getting Started for Contributors

If you are new to the Kubernaut Operator, here is the minimum path to your first contribution:

1. **Read this file** -- it defines what is mandatory and what will block your PR
2. **Understand the operator** -- single CRD (`kubernaut.ai/v1alpha1/Kubernaut`), singleton per cluster, managed by `KubernautReconciler` in `internal/controller/`
3. **Follow TDD**: write a failing test, make it pass with minimal code, then refactor
4. **Run the checks** before submitting:
   ```bash
   go build ./...
   golangci-lint run
   make test
   ```
5. **Map to business requirements** if your change affects the operator's reconciliation logic or resource generation

For complex changes (multi-component, architectural), follow the full [Pre-Implementation Workflow](#pre-implementation-workflow).

---

## Table of Contents

1. [Getting Started for Contributors](#getting-started-for-contributors)
2. [Architecture Overview](#architecture-overview)
3. [Pre-Implementation Workflow](#pre-implementation-workflow)
4. [TDD Workflow](#tdd-workflow)
5. [Wiring Verification](#wiring-verification)
6. [Audit and Compliance](#audit-and-compliance)
7. [Go Anti-Pattern Checklist](#go-anti-pattern-checklist)
8. [Testing Requirements](#testing-requirements)
9. [AI Agent Checkpoints](#ai-agent-checkpoints)
10. [Code Quality Standards](#code-quality-standards)
11. [GA Readiness Audit](#ga-readiness-audit)
12. [Completion Requirements](#completion-requirements)
13. [TDD Anti-Patterns](#tdd-anti-patterns)
14. [Collaboration Rules](#collaboration-rules)

---

## Architecture Overview

The Kubernaut Operator is a Kubernetes/OpenShift operator built with kubebuilder and controller-runtime.

### Key Components

| Component | Location | Purpose |
|-----------|----------|---------|
| CRD types | `api/v1alpha1/` | `Kubernaut` CRD definition (singleton) |
| Controller | `internal/controller/` | `KubernautReconciler` -- phase-based reconciliation |
| Resource builders | `internal/resources/` | Generate K8s objects (Deployments, RBAC, ConfigMaps, etc.) |
| Webhook | `internal/webhook/` | Singleton validation webhook |
| Entrypoint | `cmd/main.go` | Operator wiring -- controller + webhook registration |
| OLM bundle | `bundle/` | Operator Lifecycle Manager packaging |
| Kustomize | `config/` | CRDs, RBAC, samples, Prometheus, webhook config |

### Reconciliation Phases

```
Validating → Migrating → Deploying → Running / Degraded / Error
```

### Singleton Constraint

Only one `Kubernaut` CR named `"kubernaut"` is allowed per cluster, enforced by the validating webhook.

---

## Pre-Implementation Workflow

Before writing any implementation code, every non-trivial task must pass through this workflow in order.

### Step 1: Preflight Checks

Analyze the existing codebase to understand blast radius:

- Search for existing implementations of the target component
- Map dependencies and callers (resource builders ↔ controller ↔ cmd/main.go)
- Identify affected reconciliation phases
- Assess CRD schema impact (adding/changing fields in `api/v1alpha1/`)
- Verify no conflicting work in progress

**Gate**: Preflight confidence must reach **95%** before proceeding. If below 95%, identify what is unknown and proceed to Step 2.

### Step 2: Spikes (If Needed)

Time-boxed investigation (max 2 hours) to resolve unknowns identified in preflight:

- Prototype the uncertain approach in an isolated spike
- Validate assumptions against real code or infrastructure
- Document findings with evidence

**Gate**: Each spike must produce a clear YES/NO decision on the approach.

### Step 3: Confidence Score

After preflight + spikes, declare overall confidence:

- **95-100%**: Proceed directly to planning
- **90-94%**: Proceed with caution, flag remaining risks
- **Below 90%**: STOP. Escalate unknowns to the user before proceeding

### Step 4: Plan

Create an implementation plan with:

1. **TDD phase mapping**: RED, GREEN, REFACTOR sequence with estimated durations
2. **Wiring Manifest**: Required for all new components (see [Wiring Verification](#wiring-verification))
3. **CRD impact**: Schema changes, migration requirements, backward compatibility
4. **Success criteria**: Measurable outcomes
5. **Risk mitigation**: Contingency and rollback plans

**Gate**: User approval required before proceeding to implementation.

### Step 5: Readiness Audit (Pre-Implementation)

Before starting TDD, verify readiness against the [GA Readiness Audit](#ga-readiness-audit) dimensions to ensure the plan addresses all quality gates.

### Step 6: TDD Implementation

Execute TDD in sub-phases:

1. **DISCOVERY**: Search existing implementations (CHECKPOINT B)
2. **RED**: Write failing tests (UT for resource builders + IT for controller wiring)
3. **GREEN**: Minimal implementation + mandatory integration + CHECKPOINT W
4. **REFACTOR**: Enhance with sophisticated logic + Go anti-pattern validation

See [TDD Workflow](#tdd-workflow) for full details on each phase.

### Step 7: Verification

After implementation:

1. **Business verification**: Requirement fulfilled
2. **Technical validation**: Build, lint, tests pass
3. **Integration confirmation**: Controller reconciliation tested via envtest
4. **CRD validation**: Generated manifests are correct (`make manifests generate`)
5. **Confidence assessment**: Percentage + justification

### When to Use the Full Workflow

- New resource builders (Deployments, RBAC, ConfigMaps)
- Controller reconciliation logic changes
- CRD schema changes (`api/v1alpha1/`)
- Webhook modifications
- Cross-component integration (controller ↔ resource builders)
- Migration logic changes

### When to Use Standard TDD Only (Skip Steps 1-5)

- Simple bug fixes (single file)
- Documentation updates
- Configuration changes (kustomize, samples)
- Test-only modifications

---

## TDD Workflow

Every code change follows RED, GREEN, REFACTOR. No exceptions.

### RED Phase

Write failing tests that define the expected behavior:

- **Unit tests** for resource builders (`internal/resources/*_test.go`) -- pure function assertions
- **Integration tests** for controller wiring (`internal/controller/*_test.go`) -- envtest-backed
- Tests MUST use **Ginkgo/Gomega** BDD framework
- Follow existing patterns: `Describe`/`Context`/`It` blocks

### GREEN Phase

Minimal implementation to make tests pass:

- Wire the component into production code (`cmd/main.go`, controller, resource builders)
- Execute CHECKPOINT W (see [Wiring Verification](#wiring-verification))
- No sophisticated logic in GREEN -- keep it minimal
- GREEN is NOT complete until both UT and IT pass

### REFACTOR Phase

Enhance the implementation with production-quality logic:

- Apply the [Go Anti-Pattern Checklist](#go-anti-pattern-checklist)
- Optimize algorithms and data structures
- Improve error messages and observability
- Run `make manifests generate` if CRD types changed
- Validate build success across entire codebase after refactoring
- NEVER create new types or components in REFACTOR -- enhance existing only

### The Pyramid Invariant

> UT proves logic. IT proves wiring. E2E proves the journey.
> A resource builder with only UT coverage is prototyped, not implemented.
> GREEN is not complete until the IT test for the reconciliation path passes.

---

## Wiring Verification

Prevents the "built but not wired" failure where resource builders exist in `internal/resources/` with passing unit tests but are never called from the controller.

### Wiring Manifest (Plan Phase -- Mandatory)

Every plan introducing new components MUST include this table:

| Component | Production Entry Point | Wiring Code Location | IT Test ID |
|-----------|----------------------|---------------------|-----------|
| *resource builder / handler* | *where called in reconciler* | *exact file and function* | *IT test proving wiring* |

### CHECKPOINT W (GREEN Phase -- Mandatory)

After GREEN phase, verify for each Wiring Manifest row:

- [ ] Resource builder called from `KubernautReconciler.Reconcile()` or its sub-functions
- [ ] IT test exercises the reconciliation path through envtest
- [ ] No resource builder in `internal/resources/` without a corresponding controller caller
- [ ] No "TODO: wire later" deferred wiring

**Violation**: `WIRING CHECKPOINT FAILED: [Component] has no controller caller`

### Wiring-First TDD Sequence

```
RED:   Write IT test driving reconciliation through envtest -> fails
       Write UT test for resource builder logic -> fails
GREEN: Wire resource builder in controller -> IT passes
       Implement resource builder logic -> UT passes
REFACTOR: Clean up
```

### Detection Commands

**Preference hierarchy**: gopls MCP > gopls CLI > grep

#### gopls (preferred -- type-safe, import-aware)

gopls provides precise reference lookups using the Go type system. Results are identical
regardless of interface; MCP is preferred for AI agents because it avoids shell parsing.

**Setup**:
```bash
go install golang.org/x/tools/gopls@latest
gopls mcp
```

For Cursor IDE, the gopls MCP server is pre-configured (`user-gopls`).

**MCP usage** (AI agents):
```
go_symbol_references(file="/path/to/file.go", symbol="NewComponent")
go_symbol_references(file="/path/to/file.go", symbol="DeploymentForService")
go_search(query="NewComponent")
```

**CLI usage** (universal):
```bash
gopls references /path/to/file.go:42:6
gopls symbols -query="NewComponent"
```

Verify that each new exported function/type has at least one caller in production code
(`cmd/` or `internal/controller/` paths, excluding `_test.go`).

#### grep (fallback -- when gopls is unavailable)

```bash
grep -r "NewComponent\|BuildDeployment\|BuildService" cmd/ internal/controller/ --include="*.go" | grep -v "_test.go"

for f in $(git diff --name-only --diff-filter=A -- 'internal/resources/*.go' | grep -v _test.go); do
  base=$(basename "$f" .go)
  if ! grep -rq "$base" internal/controller/ --include="*.go"; then
    echo "WARNING: $f may be orphaned (no controller reference)"
  fi
done
```

---

## Audit and Compliance

The operator's compliance posture differs from the kubernaut platform services. The platform persists audit events to PostgreSQL with hash chains, retention policies, and SOC2/FedRAMP control mappings. The operator does not.

### Operator Audit Requirements

The operator produces **structured log-based audit traces** for reconciliation activity. These are captured by the OpenShift API server audit log and the operator's own structured logging.

**What the operator MUST do:**

- Use structured logging (`log.FromContext(ctx)`) with key-value pairs for all reconciliation actions
- Log phase transitions with the old and new phase values
- Log resource creation, update, and deletion with object kind, name, and namespace
- Log error conditions with sufficient context for postmortem reconstruction
- Include the CR generation and resourceVersion in reconciliation log entries

**What the operator does NOT do:**

- Persist audit events to a database
- Maintain hash chains or digital signatures on audit records
- Map individual log entries to SOC2/FedRAMP control IDs
- Implement retention policies (the cluster's log aggregation handles this)

### RBAC Auditability

All operator RBAC rules must follow least-privilege (AC-6). When adding new ClusterRole rules:

- Document why the permission is needed
- Use the narrowest verb set possible
- Prefer namespaced roles over cluster-scoped when feasible

---

## Go Anti-Pattern Checklist

Validated during the REFACTOR phase. Based on [100 Go Mistakes and How to Avoid Them](https://100go.co/).

### Mandatory Checks

| Anti-Pattern | Detection | Resolution |
|-------------|-----------|-----------|
| Function/method with 8+ parameters | Count params in signature | Use Options pattern or config struct |
| Variable shadowing | `go vet -shadow` or linter | Rename inner variable, use explicit assignment |
| Unnecessary nesting (> 3 levels) | Visual inspection | Early returns, guard clauses, extract functions |
| Interface pollution (5+ methods) | Count interface methods | Split into focused role interfaces |
| Inefficient slice pre-allocation | `make([]T, 0)` without capacity when size is known | `make([]T, 0, expectedSize)` |
| Inefficient map pre-allocation | `make(map[K]V)` without size hint when count is known | `make(map[K]V, expectedSize)` |
| Naked returns in functions > 5 lines | Lint check | Use explicit return values |
| Error strings with uppercase or punctuation | `grep -r "fmt.Errorf.*[A-Z]"` | Lowercase, no trailing punctuation, no newlines |
| Context stored in struct fields | Grep for `ctx context.Context` as struct field | Pass context as first function parameter |
| Goroutine leaks | Missing context cancellation or done channel | Ensure every goroutine has an exit path |
| Deep package nesting (> 4 levels) | Directory depth check | Flatten package hierarchy |
| `any`/`interface{}` usage | Grep for `any\|interface{}` | Use specific types or generics |
| Ignoring errors | Grep for unchecked error returns | Handle every error, log with context |
| God structs (15+ fields) | Count struct fields | Decompose into focused sub-structs |

---

## Testing Requirements

### Framework (Mandatory)

- **Ginkgo v2 + Gomega** BDD framework -- NO standard Go `testing.T` for business logic tests
- The `TestXxx` entrypoints delegate immediately to Ginkgo via `RegisterFailHandler(Fail)` + `RunSpecs()`

### Three-Tier Test Structure

| Tier | Location | Infrastructure | Purpose |
|------|----------|---------------|---------|
| Unit | `internal/resources/*_test.go`, `internal/webhook/` | None (pure Go) | Verify resource builder output, webhook logic |
| Integration | `internal/controller/*_test.go` | envtest (embedded etcd + kube-apiserver) | Verify controller reconciliation, phase transitions, error handling |
| E2E | `test/e2e/` | Live OCP cluster (`oc login` required) | Verify full operator lifecycle on a real cluster |

### Coverage Targets

| Tier | Target | CI Gate |
|------|--------|---------|
| Unit | 96%+ of `internal/resources/` | Enforced via `make test` |
| Integration | 78%+ of `internal/controller/` | Enforced via `make test` |
| All tiers merged | >= 80% of `internal/` | CI gate in `.github/workflows/test.yml` |

### Testing Conventions

- **No `time.Sleep`** -- use `gomega.Eventually` with explicit `timeout`/`interval`
- **Singleton key**: `singletonKey()` returns `types.NamespacedName{Name: "kubernaut", Namespace: "default"}`
- **RELATED_IMAGE env vars**: Must be set in `BeforeSuite` before calling resource builders
- **Fake client wrappers**: Use `deleteFailingClient`, `rbacCreateFailingClient` to inject specific errors for error-path testing
- **envtest binary discovery**: `getFirstFoundEnvTestBinaryDir()` auto-discovers binaries in `bin/k8s/`

### Mock Strategy

**Mock ONLY external dependencies:**
- Kubernetes API (use envtest or `fake.NewClientBuilder()`)
- OpenShift APIs (load CRDs into envtest)
- External services behind the operator's managed Deployments

**Use real business logic:**
- ALL resource builder functions in `internal/resources/`
- ALL controller reconciliation logic
- ALL webhook validation logic

### CI Parallel Safety

- Each test creates its own namespace or uses `singletonKey()` -- no shared state
- envtest instances are per-suite, not per-test
- E2E tests require exclusive cluster access

---

## AI Agent Checkpoints

Mandatory validation gates for AI coding agents. Human contributors should follow these as mental checklists.

### CHECKPOINT A: Type Reference Validation

**Trigger**: About to reference any CRD field or struct field.

**Action**: Read the type definition in `api/v1alpha1/kubernaut_types.go` BEFORE referencing fields. Verify the field exists in the struct definition.

**Violation**: Type reference without validation -- STOP.

### CHECKPOINT B: Implementation Discovery

**Trigger**: About to create a test file or new resource builder.

**Action**: Search for existing implementations first. Enhance existing patterns instead of creating new ones.

**Violation**: Creation without searching existing code -- STOP.

### CHECKPOINT C: Controller Integration Validation

**Trigger**: Creating new resource builders or types.

**Action**: Verify the resource builder is called from `KubernautReconciler`. Use `go_symbol_references` (gopls) to confirm the new function has at least one caller in the controller.

**Violation**: Resource builder without controller caller -- STOP.

### CHECKPOINT D: Build Error Investigation

**Trigger**: Build errors or undefined symbols reported.

**Action**: Execute comprehensive symbol analysis. Present options with evidence before implementing.

**Required format**:
```
UNDEFINED SYMBOL ANALYSIS:
Symbol: [undefined_symbol]
References found: [N files with paths]
Dependent infrastructure: [list missing types/functions]
Scope: [minimal/medium/extensive with evidence]

OPTIONS (Evidence-Based):
A) Implement complete infrastructure ([X] files affected)
B) Create minimal stub ([Z] files affected, may break [W] files)
C) Alternative approach: [evidence-based alternative]

MANDATORY USER DECISION REQUIRED: Which approach? (A/B/C)
```

### CHECKPOINT DD: Design Decision Validation

**Trigger**: Proposing or implementing a significant architectural change (new CRD fields, new reconciliation phases, new resource types, webhook changes).

**Action**: Before implementing, execute this sequence:

1. Search for similar patterns in the codebase
2. Identify 2-3 alternative approaches with pros/cons
3. Present alternatives to the user for approval
4. After approval, document the decision in `docs/design/`
5. Reference the decision in implementation code comments

**Violation**: Architectural change without documented alternatives and user approval -- STOP.

**Skip if**: Simple bug fix, adding a field to an existing resource builder, config change, test-only change.

### CHECKPOINT CRD: Schema Change Validation

**Trigger**: Modifying any type in `api/v1alpha1/`.

**Action**:
1. Verify backward compatibility (new fields must be optional or have defaults)
2. Run `make manifests generate` to regenerate CRD YAML
3. Verify the generated YAML in `config/crd/bases/` is correct
4. Update OLM bundle if needed (`make bundle`)

**Violation**: CRD change without manifest regeneration -- STOP.

---

## Code Quality Standards

### Error Handling (Mandatory)

- ALWAYS handle errors -- never ignore them
- ALWAYS add log entry for every error
- Wrap errors with context: `fmt.Errorf("operation description: %w", err)`
- Error strings: lowercase, no trailing punctuation, no newlines
- Use structured logging with `log.FromContext(ctx)`

### Type System

- AVOID `any` or `interface{}` unless absolutely necessary
- Use structured field values with specific types
- CRD types in `api/v1alpha1/` must follow kubebuilder conventions

### Controller Patterns

- Use `ctrl.Result{}` with appropriate requeue intervals
- Phase transitions must be explicit and tested
- Status conditions must follow `metav1.Condition` conventions
- Finalizers must have cleanup logic tested

### Resource Builder Patterns

- Each builder function takes `*kubernautv1alpha1.Kubernaut` and returns the K8s object
- Set owner references for garbage collection
- Use `RELATED_IMAGE_*` env vars for container images (OLM disconnect support)
- Labels: `app.kubernetes.io/managed-by: kubernaut-operator`

---

## GA Readiness Audit

A quality gate applied before declaring a feature production-ready.

### Dimensions

| # | Dimension | Pass Criteria |
|---|-----------|--------------|
| 1 | **Build** | `go build ./...` succeeds with zero errors |
| 2 | **Lint** | `golangci-lint run` produces zero new warnings |
| 3 | **Unit Tests** | 100% pass rate, 96%+ coverage on `internal/resources/` |
| 4 | **Integration Tests** | 100% pass rate, 78%+ coverage on `internal/controller/` |
| 5 | **Wiring Verification** | CHECKPOINT W passes for all components |
| 6 | **BDD Framework** | Zero standard `testing.T` usage in business tests |
| 7 | **100 Go Mistakes** | Zero violations from [anti-pattern checklist](#go-anti-pattern-checklist) |
| 8 | **CRD Validation** | `make manifests generate` produces no diff |
| 9 | **Regression** | Zero regressions in existing test suites |
| 10 | **Fail-Open Safety** | No silent failures -- all error paths are observable |
| 11 | **Operator-Specific** | Phase transitions tested, webhook validated, RBAC minimal |

### When to Apply

- Before merging a feature branch with > 100 lines changed
- Before tagging a release
- As the final gate after implementation (Step 7: Verification)

---

## Completion Requirements

### Post-Development Checklist (Mandatory)

After completing any development task:

1. **Build validation**: Code builds without errors
2. **Lint compliance**: No new lint errors
3. **Test pass**: All affected tests pass (`make test`)
4. **CRD manifests**: `make manifests generate` produces no diff
5. **Anti-patterns**: Refactored code passes Go anti-pattern checklist

### Confidence Assessment Format (Required)

Provide BOTH:
- **Percentage**: 60-100% confidence rating
- **Justification**: Risks, assumptions, validation approach

Example:
```
Confidence: 90%
Justification: New resource builder follows established patterns in internal/resources/
and is wired into the reconciler's Deploying phase. Risk: HPA configuration needs
validation on a live cluster. Validation: UT covers builder output, IT covers
reconciliation path.
```

---

## TDD Anti-Patterns

Forbidden patterns with detection rules.

| Anti-Pattern | Description | Rule |
|-------------|-------------|------|
| **Discovery Skip** | Creating without searching existing | Use CHECKPOINT B FIRST |
| **RED Skip** | Implementation without failing tests | Write tests FIRST |
| **GREEN Complexity** | Sophisticated logic in GREEN phase | Minimal implementation only |
| **REFACTOR Creation** | New types/components in REFACTOR | Enhance existing only |
| **Integration Delay** | Resource builder not wired in GREEN | Wire in GREEN, not later |
| **UT-Only GREEN** | Declaring GREEN when only UT passes | IT must also pass |
| **Pending Tests** | Using `XIt` or `Skip()` | Implement or remove |
| **Refactor Without Build** | Refactoring without checking build | Run `go build ./...` after ANY refactor |
| **Sleep in Tests** | Using `time.Sleep` instead of Eventually | Use `gomega.Eventually` with timeout/interval |

### Post-Refactor Validation (Mandatory)

```bash
go build ./...
make manifests generate
git diff --exit-code config/  # No unexpected CRD changes
go test ./... -run=^$ -timeout=30s
```

---

## Collaboration Rules

### Rule 1: Pause, Assess, Communicate

Before executing any significant action (tests, commits, refactors, new implementations, destructive operations), pause and assess.

**ALWAYS share if you have:**
- Questions: Ambiguities or missing information
- Concerns: Potential issues, risks, or anti-patterns
- Alternatives: Different approaches worth considering
- Confidence gaps: Uncertainty below 90%
- Recommendations: Improvements with rationale

**SKIP if:**
- Everything is clear, straightforward, and routine
- High confidence (> 95%) with no concerns
- User explicitly said "proceed without asking"

### Rule 2: No Pending Tests

Never use `XIt`, `PIt`, or `Skip()` to defer test implementation. Either:
- Implement the test following TDD
- Remove it from the test plan

### Rule 3: Critical Decision Escalation

MANDATORY: Ask for input on ALL critical decisions:
- CRD schema changes
- New reconciliation phases or conditions
- Webhook behavior changes
- RBAC modifications
- OLM bundle changes

Provide a recommendation with detailed justification when asking.

---

## Quick Reference

```bash
# Build and lint
go build ./...
golangci-lint run

# Test pyramid
make test-unit                    # Unit tests (internal/resources, webhook)
make test-integration             # Integration tests (envtest)
make test                         # All non-E2E tests + coverage merge
make test-e2e                     # E2E tests (requires oc login)

# CRD and codegen
make manifests                    # Regenerate CRD YAML
make generate                     # Regenerate deepcopy functions
make bundle                       # Regenerate OLM bundle
```

---

## Authority and References

- [Operator Test Plan](docs/test-plans/kubernaut-operator-test-plan.md) -- IEEE 829-2008 compliant
- [100 Go Mistakes](https://100go.co/) -- anti-pattern reference
- [controller-runtime docs](https://pkg.go.dev/sigs.k8s.io/controller-runtime) -- framework reference
- [kubebuilder book](https://book.kubebuilder.io/) -- operator patterns

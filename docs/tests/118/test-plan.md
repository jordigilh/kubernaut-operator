# SAR-Based Tool Authorization Test Plan

**IEEE 829-2008 compliant**

| Field | Value |
|-------|-------|
| Test Plan Identifier | KO-TP-003 |
| Version | 1.0 |
| Date | 2026-05-21 |
| Author | Jordi Gil |
| Status | Draft |
| Related Issues | [operator #118](https://github.com/jordigilh/kubernaut-operator/issues/118), [kubernaut #1221](https://github.com/jordigilh/kubernaut/issues/1221), [kubernaut #1222](https://github.com/jordigilh/kubernaut/pull/1222) |

---

## 1. Introduction

### 1.1 Purpose

This document defines the test strategy, scope, approach, and acceptance criteria for
verifying the operator-side implementation of SAR-based tool authorization (upstream
kubernaut PR #1222). The implementation replaces file-based RBAC (`rbac_roles.yaml`
ConfigMap) with Kubernetes SubjectAccessReview-based authorization, where the API
Frontend (AF) checks tool-level permissions at runtime via `SubjectAccessReview`
against ClusterRoles provisioned by the operator.

### 1.2 Scope

| In Scope | Out of Scope |
|----------|--------------|
| 6 persona-based tool ClusterRoles with exact `resourceNames` | AF binary SAR call logic (upstream) |
| CR-driven group-to-role ClusterRoleBindings | OIDC provider configuration |
| AF ClusterRole SAR + remediationrequests permissions | E2E tests on live OCP cluster |
| AF config.yaml `rbac.sarCacheTTL` generation | OIDC-direct mode (upstream #1226, separate issue) |
| File-based RBAC ConfigMap removal and volume simplification | OLM bundle regeneration |
| Orphaned ConfigMap deletion on upgrade | Helm chart changes (upstream) |
| Stale CRB pruning when role bindings removed | AF binary startup behavior |
| `ConditionToolRBACBound` status condition lifecycle | |
| Finalizer cleanup of tool ClusterRoles and CRBs | |
| CRD validation (persona enum, sarCacheTTL format, duplicate roles) | |

### 1.3 References

- IEEE 829-2008, Standard for Software and System Test Documentation
- [100 Go Mistakes and How to Avoid Them](https://100go.co/) (refactor checklist)
- [kubernaut PR #1222](https://github.com/jordigilh/kubernaut/pull/1222) -- SAR implementation (merged)
- [kubernaut #1221](https://github.com/jordigilh/kubernaut/issues/1221) -- SAR design issue
- [operator #118](https://github.com/jordigilh/kubernaut-operator/issues/118) -- Operator-side tracking
- [SAR implementation plan](../../.cursor/plans/implement_sar_tool_auth_61287b7b.plan.md)
- [AF GA readiness audit](../../.cursor/plans/af_ga_readiness_audit_100a64e7.plan.md)
- KO-TP-001: Kubernaut Operator Test Plan (`docs/test-plans/`)
- KO-TP-002: ConfigMap Drift Detection Test Plan (`docs/test/16-17/`)
- controller-runtime envtest documentation

### 1.4 Glossary

| Term | Definition |
|------|-----------|
| CR | Custom Resource (instance of `Kubernaut` CRD) |
| CRB | ClusterRoleBinding |
| SAR | SubjectAccessReview -- K8s API for authorization checks |
| AF | API Frontend -- MCP/A2A gateway service |
| BAC | Business Acceptance Criterion |
| TDD | Test-Driven Development (Red-Green-Refactor cycle) |
| envtest | controller-runtime test harness with embedded etcd + API server |

---

## 2. Test Items

### 2.1 Software Under Test

| Component | File(s) | Change Type |
|-----------|---------|-------------|
| `APIFrontendRBACSpec`, `ToolRoleBinding` types | `api/v1alpha1/kubernaut_types.go` | New |
| `ConditionToolRBACBound` constant | `api/v1alpha1/kubernaut_types.go` | New |
| `BoundToolRoleBindings` status field | `api/v1alpha1/kubernaut_types.go` | New |
| `ToolClusterRoles()` | `internal/resources/rbac.go` | New |
| `ToolClusterRoleBindings()` | `internal/resources/rbac.go` | New |
| `ToolClusterRoleNames()` | `internal/resources/rbac.go` | New |
| `ToolCRBNames()` | `internal/resources/rbac.go` | New |
| `apifrontendClusterRole()` (modified) | `internal/resources/rbac.go` | Modified |
| `ClusterRoles()` (modified) | `internal/resources/rbac.go` | Modified |
| `ClusterRoleBindings()` (modified) | `internal/resources/rbac.go` | Modified |
| `afRBACYAML` struct | `internal/resources/configmaps.go` | New |
| `APIFrontendConfigMap()` (modified) | `internal/resources/configmaps.go` | Modified |
| `APIFrontendRBACRolesConfigMap()` | `internal/resources/configmaps.go` | Deleted |
| `APIFrontendDeployment()` (modified) | `internal/resources/deployments.go` | Modified |
| `validateToolRoleBindings()` | `internal/resources/validation.go` | New |
| `deployToolRBAC()` | `internal/controller/kubernaut_controller.go` | New |
| `deployRBAC()` (modified) | `internal/controller/kubernaut_controller.go` | Modified |
| `deleteRBACResources()` (modified) | `internal/controller/kubernaut_controller.go` | Modified |
| `phaseDeploy()` (modified) | `internal/controller/kubernaut_controller.go` | Modified |

### 2.2 Unchanged Dependencies (not retested)

| Component | Rationale |
|-----------|-----------|
| `buildDeployment()` | Only volume source changes; function signature unchanged |
| `ensureUnowned()` | Existing, well-tested reconciler helper |
| `deleteIfExists()` | Existing, well-tested reconciler helper |
| `patchStatus()` | Existing, well-tested reconciler helper |
| `clusterRoleName()` | Existing namespace-prefixing helper |

---

## 3. Features to Be Tested

### 3.1 Business Acceptance Criteria

| ID | Criterion | Priority |
|----|-----------|----------|
| BAC-1 | 6 persona-based tool ClusterRoles are provisioned with exact `resourceNames` matching upstream `values.yaml` | P0 |
| BAC-2 | CR-driven group-to-role bindings create/update/prune ClusterRoleBindings | P0 |
| BAC-3 | AF ClusterRole includes `subjectaccessreviews/create` and `remediationrequests` get/list/create | P0 |
| BAC-4 | AF config.yaml includes `rbac.sarCacheTTL` with Go duration string | P0 |
| BAC-5 | File-based `rbac_roles.yaml` ConfigMap and projected volume are removed; orphaned CM deleted on upgrade | P0 |
| BAC-6 | `RBACRolesConfigMapRef` replaced by `APIFrontendRBACSpec` with `sarCacheTTL` + `roleBindings` (CRD validation) | P0 |
| BAC-7 | Stale CRBs pruned when role bindings removed from spec | P1 |
| BAC-8 | `ConditionToolRBACBound` status condition reflects binding health | P1 |
| BAC-9 | Finalizer cleans up tool ClusterRoles and CRBs on CR deletion | P0 |

### 3.2 Features Not Tested

- AF binary SAR call behavior (tested upstream in kubernaut repository)
- OIDC provider integration (external dependency)
- OLM bundle packaging (separate CI pipeline)

---

## 4. Approach

### 4.1 Test Tiers

| Tier | Scope | Framework | Target Coverage |
|------|-------|-----------|-----------------|
| 1 (Unit) | Resource builders in `internal/resources/` | Ginkgo/Gomega | >= 80% of new/modified functions |
| 2 (Integration) | Controller reconciliation in `internal/controller/` | envtest + Ginkgo/Gomega | >= 80% of new/modified methods |
| 3 (Validation) | CRD validation helpers | Ginkgo/Gomega | >= 80% of validation paths |

### 4.2 TDD Methodology

Each implementation phase follows strict TDD:

1. **Red**: Write all tests first. Tests compile but fail.
2. **Green**: Implement minimal production code to make all tests pass.
3. **Refactor**: Improve code quality against 100 Go Mistakes checklist without changing behavior. Static analysis with `golangci-lint` (shadow + prealloc linters).

### 4.3 Checkpoints

| Checkpoint | Gate | When |
|------------|------|------|
| CP-1 | Test design review: >= 30 test cases, all BACs covered, anti-patterns avoided | After Phase 1 (Red) |
| CP-2 | Post-green GA audit: API stability, security, coverage, upgrade path. Confidence >= 95% | After Phase 2 (Green) |
| CP-3 | Final GA audit: all dimensions. Escalate findings < 95% confidence | After Phase 3 (Refactor) |

### 4.4 Go Testing Anti-Patterns Avoided

- No `time.Sleep` -- use `Eventually`/`Consistently` with configurable timeout/interval
- No test-only production code paths (no `if testing.Testing()` guards)
- No assertion on error string content when typed errors are available
- No shared mutable state between `It` blocks -- each test is independent via `AfterEach` cleanup
- No excessive mocking -- envtest provides real API server for integration tests
- No testing private functions directly -- test through exported API surface
- No ignoring test cleanup -- `AfterEach` cleans namespaced and cluster-scoped resources
- Table-driven tests when 3+ cases share structure

---

## 5. Test Cases

### 5.1 Tier 1: Unit Tests -- Tool ClusterRoles (rbac_test.go)

| TC ID | Test Name | BAC | Type |
|-------|-----------|-----|------|
| TC-1.01 | ToolClusterRoles returns 6 roles when AF is enabled | BAC-1 | Positive |
| TC-1.02 | ToolClusterRoles returns empty when AF is disabled | BAC-1 | Negative |
| TC-1.03 | Each tool ClusterRole uses apiGroup kubernaut.ai, resource tools, verb use | BAC-1 | Positive |
| TC-1.04 | SRE role has exactly 19 resourceNames | BAC-1 | Positive |
| TC-1.05 | AI-orchestrator role has exactly 15 resourceNames | BAC-1 | Positive |
| TC-1.06 | CICD role has exactly 3 resourceNames | BAC-1 | Positive |
| TC-1.07 | Observability role has exactly 8 resourceNames | BAC-1 | Positive |
| TC-1.08 | L3-audit role has exactly 6 resourceNames | BAC-1 | Positive |
| TC-1.09 | Remediation-approver role has exactly 4 resourceNames | BAC-1 | Positive |
| TC-1.10 | Tool ClusterRole names are namespace-prefixed | BAC-1 | Positive |
| TC-1.11 | ToolClusterRoleNames returns all 6 names for finalizer cleanup | BAC-9 | Positive |

### 5.2 Tier 1: Unit Tests -- Tool ClusterRoleBindings (rbac_test.go)

| TC ID | Test Name | BAC | Type |
|-------|-----------|-----|------|
| TC-1.12 | ToolClusterRoleBindings returns CRBs matching spec roleBindings | BAC-2 | Positive |
| TC-1.13 | ToolClusterRoleBindings returns empty when no roleBindings specified | BAC-2 | Boundary |
| TC-1.14 | CRB subjects are Group kind with correct group names | BAC-2 | Positive |
| TC-1.15 | CRB roleRef points to namespace-prefixed tool ClusterRole | BAC-2 | Positive |
| TC-1.16 | Duplicate roles with different groups are merged | BAC-2 | Boundary |
| TC-1.17 | ToolCRBNames returns all CRB names for finalizer cleanup | BAC-9 | Positive |

### 5.3 Tier 1: Unit Tests -- AF ClusterRole SAR Permissions (rbac_test.go)

| TC ID | Test Name | BAC | Type |
|-------|-----------|-----|------|
| TC-1.18 | apifrontendClusterRole includes subjectaccessreviews/create | BAC-3 | Positive |
| TC-1.19 | apifrontendClusterRole includes remediationrequests get/list/create | BAC-3 | Positive |

### 5.4 Tier 1: Unit Tests -- AF Config SAR (configmaps_test.go)

| TC ID | Test Name | BAC | Type |
|-------|-----------|-----|------|
| TC-1.20 | AF config includes rbac.sarCacheTTL with default 30s | BAC-4 | Positive |
| TC-1.21 | AF config renders custom sarCacheTTL from spec | BAC-4 | Positive |

### 5.5 Tier 1: Unit Tests -- Volume Simplification (deployments_test.go)

| TC ID | Test Name | BAC | Type |
|-------|-----------|-----|------|
| TC-1.22 | AF deployment uses plain ConfigMap volume, not projected | BAC-5 | Positive |
| TC-1.23 | AF deployment does not reference rbac_roles.yaml | BAC-5 | Negative |

### 5.6 Tier 1: Unit Tests -- Validation (validation_test.go)

| TC ID | Test Name | BAC | Type |
|-------|-----------|-----|------|
| TC-1.24 | Rejects duplicate role names in roleBindings | BAC-6 | Negative |
| TC-1.25 | Accepts valid roleBindings with known persona names | BAC-6 | Positive |
| TC-1.26 | Accepts empty roleBindings list | BAC-6 | Boundary |
| TC-1.27 | Rejects invalid sarCacheTTL format (validated by CRD pattern) | BAC-6 | Negative |

### 5.7 Tier 2: Integration Tests (kubernaut_lifecycle_test.go)

| TC ID | Test Name | BAC | Type |
|-------|-----------|-----|------|
| TC-2.01 | Tool ClusterRoles are created during deploy phase | BAC-1 | Integration |
| TC-2.02 | Tool CRBs are created when roleBindings specified in CR | BAC-2 | Integration |
| TC-2.03 | Tool CRBs are pruned when roleBindings removed from spec | BAC-7 | Integration |
| TC-2.04 | ConditionToolRBACBound is True when all bindings active | BAC-8 | Integration |
| TC-2.05 | ConditionToolRBACBound is removed when no bindings specified | BAC-8 | Integration |
| TC-2.06 | Orphaned apifrontend-rbac-roles ConfigMap is deleted on upgrade | BAC-5 | Integration |
| TC-2.07 | Tool ClusterRoles and CRBs are deleted by finalizer | BAC-9 | Integration |
| TC-2.08 | AF Deployment uses plain ConfigMap volume after SAR migration | BAC-5 | Integration |

---

## 6. Pass/Fail Criteria

### 6.1 Test Level

- A test case **passes** when all `Expect()` assertions succeed.
- A test case **fails** when any assertion fails or an unexpected panic occurs.

### 6.2 Plan Level

| Criterion | Threshold |
|-----------|-----------|
| Tier 1 pass rate | 100% |
| Tier 2 pass rate | 100% |
| Code coverage (new/modified files) | >= 80% per file |
| GA readiness confidence (all dimensions) | >= 95% |
| `go vet` warnings | 0 |
| `golangci-lint` (shadow + prealloc) findings | 0 in new code |

---

## 7. Test Deliverables

| Deliverable | Location |
|-------------|----------|
| This test plan | `docs/tests/118/test-plan.md` |
| Tier 1 unit tests | `internal/resources/rbac_test.go`, `configmaps_test.go`, `deployments_test.go`, `validation_test.go` |
| Tier 2 integration tests | `internal/controller/kubernaut_lifecycle_test.go` |
| Coverage report | `cover.out` (generated by `make test`) |
| GA readiness audit notes | Checkpoint comments in implementation PR |

---

## 8. Test Environment

| Component | Version |
|-----------|---------|
| Go | 1.24+ |
| Kubernetes (envtest) | 1.35.0 |
| Ginkgo | v2 |
| Gomega | latest |
| controller-runtime | v0.21+ |
| golangci-lint | 1.62+ (shadow, prealloc linters) |

---

## 9. Schedule

| Phase | Duration | Gate |
|-------|----------|------|
| Phase 1: TDD Red | 1 session | Tests compile, all new tests fail |
| Checkpoint 1 | Inline | Test design audit passes |
| Phase 2: TDD Green | 1 session | `make test` passes, all tests green |
| Checkpoint 2 | Inline | Post-green GA audit, confidence >= 95% |
| Phase 3: TDD Refactor | 1 session | 100 Go Mistakes clean, lint clean, tests green |
| Checkpoint 3 | Inline | Final GA audit, all dimensions >= 95% |

---

## 10. Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Upstream renames tool names | Tool ClusterRoles have wrong `resourceNames` | Tool lists verified against merged PR#1222 `values.yaml` on `main` |
| `RBACRolesConfigMapRef` removal breaks existing CRs | Customer upgrade fails | Documented as v1alpha1 breaking change in release notes |
| Orphaned ConfigMap not cleaned up | Stale config in namespace | Integration test TC-2.06 verifies explicit deletion |
| Variable shadowing in new code | Silent bugs | `golangci-lint` shadow linter enforced in Phase 3 |
| Slice capacity not preallocated | Minor performance overhead | `golangci-lint` prealloc linter enforced in Phase 3 |

---

## 11. Approvals

| Role | Name | Date | Signature |
|------|------|------|-----------|
| Author | | | |
| Reviewer | | | |
| Approver | | | |

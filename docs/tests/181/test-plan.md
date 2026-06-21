# Test Plan — Issue #181: Custom ClusterRole References in roleBindings

**IEEE 829 Format**

## 1. Test Plan Identifier

`TP-181-custom-clusterrole-rbac`

## 2. Introduction

This test plan covers the addition of a `clusterRoleName` field to `ToolRoleBinding`, allowing users to reference their own ClusterRoles for fine-grained tool authorization instead of relying solely on the 6 hardcoded persona roles.

## 3. Test Items

- `api/v1alpha1/kubernaut_types.go` — `ToolRoleBinding` struct with new `ClusterRoleName` field
- `internal/resources/validation.go` — mutual exclusion validation (role XOR clusterRoleName)
- `internal/resources/rbac.go` — `ToolClusterRoleBindings()` handling custom refs

## 4. Features to Be Tested

| Feature | Description |
|---|---|
| F1 | `clusterRoleName` field accepted in CR |
| F2 | Validation rejects both `role` and `clusterRoleName` set simultaneously |
| F3 | Validation rejects both fields empty |
| F4 | `ToolClusterRoleBindings()` emits CRB referencing user-managed ClusterRole |
| F5 | Built-in persona bindings continue unchanged (backward compat) |
| F6 | Mixed persona + custom bindings coexist in same CR |

## 5. FedRAMP / SOC2 Control Mapping

| Control | Description | Test Coverage |
|---|---|---|
| AC-3 (Access Enforcement) | System enforces approved authorizations for logical access | T1, T2, T3, T4, T5 |
| AC-6 (Least Privilege) | System enforces most restrictive set of rights/privileges | T1, T6, T7 |
| SOC2 CC6.1 (Logical Access) | Entity implements logical access security measures | T1–T7 |

## 6. Test Cases

| ID | Scenario | FedRAMP | Tier | Pass Criteria |
|---|---|---|---|---|
| T1 | CRB created for custom `clusterRoleName` | AC-3 | Unit | `ToolClusterRoleBindings()` returns a CRB whose `RoleRef.Name` equals the user-specified ClusterRole name (not namespace-prefixed) |
| T2 | Built-in persona role still emits ClusterRole + CRB | AC-6 | Unit | CRB `RoleRef.Name` is `<ns>-tool-<persona>` |
| T3 | Mixed bindings (persona + custom) in same CR | AC-3 | Unit | Returns CRBs for both; persona CRB namespace-prefixed, custom CRB uses raw name |
| T4 | Validation rejects both `role` and `clusterRoleName` set | AC-3 | Unit | `ValidateKubernaut()` returns error containing "mutually exclusive" |
| T5 | Validation rejects both fields empty | AC-3 | Unit | `ValidateKubernaut()` returns error containing "one of role or clusterRoleName" |
| T6 | Reconciliation creates binding pointing to user-managed ClusterRole | AC-6 | Integration | Controller creates CRB with correct roleRef after CR apply |
| T7 | CRB removed on CR deletion (cleanup) | AC-6 | Integration | Finalizer deletes custom CRBs alongside persona CRBs |

## 7. Test Environment

- Go 1.23+
- Ginkgo v2 / Gomega
- `envtest` for integration tests (T6, T7)

## 8. Pass/Fail Criteria

- All unit tests pass (`go test ./internal/resources/...`)
- All integration tests pass (`go test ./internal/controller/...`)
- No regressions in existing RBAC tests
- 80%+ code coverage on new code paths

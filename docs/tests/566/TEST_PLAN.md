# Test Plan — Issue #566 API Frontend InvestigationSession patch RBAC

| Field | Value |
|---|---|
| Issue | https://github.com/jordigilh/kubernaut-operator/issues/566 |
| Scope | Operator-generated API Frontend `ClusterRole` and `ClusterRoleBinding` |
| Status | Implemented and fully validated |
| Test framework | Ginkgo v2 and Gomega |

## 1. Objective

Prove that an enabled API Frontend can perform the InvestigationSession
metadata/status patch operations required by its interactive A2A lifecycle,
while preserving the existing least-privilege rule boundaries and ensuring the
operator reconciliation path publishes the corrected RBAC.

This is engineering evidence adapted from NIST SP 800-53 Rev. 5/FedRAMP,
SOC 2, and OWASP ASVS 5.0.0. It does not claim formal FedRAMP authorization,
SOC 2 attestation, or OWASP ASVS conformance.

Implementation and evidence artifacts: `internal/resources/rbac.go`,
`internal/resources/rbac_test.go`, and
`internal/controller/kubernaut_lifecycle_test.go`.

## 2. Business behavior and control traceability

| Evidence ID | Business behavior | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 objective | Test location |
|---|---|---|---|---|---|
| `UT-RBAC-566-001` | The enabled API Frontend role grants the exact existing verbs plus `patch` on `investigationsessions` and `investigationsessions/status`. | `AC-3`, `AC-6` | `CC6.1`, `CC6.6` | `v5.0.0-V8.2.1` | `internal/resources/rbac_test.go` |
| `IT-RBAC-566-001` | A real operator Reconcile creates the corrected live role and binds it to the API Frontend service account. | `AC-3`, `AC-6`, `CM-6` | `CC6.1`, `CC6.6`, `CC8.1` | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | `internal/controller/kubernaut_lifecycle_test.go` |

The upstream FullPipeline owner-reference backfill is an external qualification
boundary under `kubernaut#2492`; this repository does not duplicate that
application E2E with a synthetic client or fixture.

## 3. Test cases

### UT-RBAC-566-001 — exact resource-builder permissions

**Precondition:** API Frontend is explicitly enabled in the test Kubernaut.

**Steps:**

1. Build `resources.ClusterRoles`.
2. Select the namespace-prefixed API Frontend `ClusterRole`.
3. Inspect the two `kubernaut.ai` InvestigationSession rules.

**Expected:**

- `investigationsessions` has exactly `get`, `list`, `watch`, `create`,
  `update`, `patch`, and `delete`.
- `investigationsessions/status` has exactly `get`, `update`, and `patch`.
- No unrelated API group or permission is introduced by this fix.

### IT-RBAC-566-001 — reconciled live RBAC

**Precondition:** envtest is running and the API Frontend is enabled in a
Kubernaut CR.

**Steps:**

1. Create the CR and reconcile through the deploy phase.
2. Read the live `<namespace>-apifrontend-role` ClusterRole.
3. Read the live `<namespace>-apifrontend-binding` ClusterRoleBinding.
4. Inspect the two InvestigationSession rules and the binding subject.

**Expected:**

- The live role contains the exact unit-tested verb sets.
- The binding references the live API Frontend role.
- The binding targets the API Frontend service account in the Kubernaut
  namespace.

## 4. Pyramid invariant

| Tier | Evidence | Boundary |
|---|---|---|
| Unit | `UT-RBAC-566-001` | Proves the RBAC builder contract and least privilege. |
| Integration | `IT-RBAC-566-001` | Proves `Reconcile` creates the live role and binding. |
| E2E | Upstream `kubernaut#2492` FullPipeline | Proves the real API Frontend A2A owner-reference backfill; exact operator SHA/image required. |

The operator Kind suite remains an operator contract harness and is not a
substitute for the upstream application FullPipeline.

## 5. Execution record

| Command | Result |
|---|---|
| `go test ./internal/resources -count=1 -ginkgo.focus='UT-RBAC-566-001'` | PASS after implementation; RED failure before implementation recorded in session evidence |
| `make test-integration` | PASS; includes the `IT-RBAC-566-001` envtest evidence and the full controller integration gate |
| `go build ./...` | PASS |
| `golangci-lint run` | PASS; 0 issues |
| `make test-unit` | PASS; 5,819 independent handwritten statements, 0 missed, 100.0% internal unit coverage |
| `make test-integration` | PASS; 78.7% controller integration coverage |
| `make test-pyramid` | PASS; security traceability, CI boundary, Helm checks, and 13/13 Helm specs |
| `make manifests generate` and `git diff --exit-code config/` | PASS; no generated config drift |

## 6. Coverage policy

The tests above are retained for distinct business behaviors, not line
coverage. The clean baseline had 100.0% internal unit coverage with zero missed
handwritten business statements; the implementation must retain the repository
unit threshold of at least 90% and the controller integration gate.

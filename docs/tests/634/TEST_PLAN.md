# IEEE 829 Test Plan — Issue #634 APIFrontend workflow catalog RBAC

| Field | Value |
|---|---|
| Test Plan ID | TP-634 |
| Issue | [kubernaut-operator#634](https://github.com/jordigilh/kubernaut-operator/issues/634) |
| Design | [`ISSUE-610-TRUSTED-WORKFLOW-CATALOG-RBAC.md`](../../design/ISSUE-610-TRUSTED-WORKFLOW-CATALOG-RBAC.md) |
| Scope | Feature-gated, namespace-scoped, read-only `RemediationWorkflow` catalog access for the APIFrontend ServiceAccount |
| Framework | Ginkgo v2/Gomega; envtest and SubjectAccessReview for reconciler authorization |

## Objective and boundary

Issue #634 requires APIFrontend to resolve trusted `RemediationWorkflow`
catalog entries during approval parameter handling. The operator must grant the
APIFrontend ServiceAccount `get` and `list` on the catalog resource in the
configured Kubernaut controller namespace (`kn.Namespace`). The grant must not
provide watch, mutation, subresource, foreign-namespace, or cluster-wide
access. The workflow-execution namespace is deliberately out of scope for this
catalog permission.

This plan verifies the operator's rendered RBAC and reconciliation behavior. It
does not claim authorization to change the upstream APIFrontend implementation
or prove the complete upstream approval journey.

## Pyramid invariant

- **UT proves logic:** the resource builder renders the exact Role and
  RoleBinding, namespace, ServiceAccount subject, API group/resource, and
  `get`/`list` verbs when APIFrontend is enabled, and omits the grant when it is
  disabled.
- **IT proves wiring:** the real reconciler creates the Role and RoleBinding;
  SubjectAccessReview verifies allowed and denied effective permissions, and
  disabling APIFrontend does not leave a stale grant.
- **E2E proves the journey:** a deployed operator and APIFrontend workload can
  read the trusted catalog in the live configured namespace. This remains a
  separate upstream/full-pipeline qualification gate when no authorized
  cluster is available.

Tests assert business behavior and security controls; no test exists only to
increase coverage. No pending or skipped tests and no `time.Sleep` are allowed.

## Evidence matrix

| ID | Business assertion | Evidence | Controls |
|---|---|---|---|
| `UT-RBAC-624-001/634-001` | `TrustedWorkflowCatalogRBAC` renders six exact namespaced catalog Roles and RoleBindings, including APIFrontend's `get`/`list` grant. | `internal/resources/rbac_test.go` | FedRAMP/NIST AC-3, AC-6; SOC 2 CC6.1, CC6.6; ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` |
| `UT-RBAC-634-002` | The builder omits the APIFrontend catalog Role and RoleBinding when `spec.apiFrontend.enabled=false`. | `internal/resources/rbac_test.go` | FedRAMP/NIST AC-3, AC-6, CM-6; SOC 2 CC6.1, CC6.6; ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` |
| `IT-RBAC-634-001` | Reconciliation creates `*-apifrontend-workflow-catalog` and its RoleBinding in `kn.Namespace`, bound to the APIFrontend ServiceAccount. | `internal/controller/trustedworkflowcatalogrbac_test.go` | FedRAMP/NIST AC-3, AC-6, CM-6; SOC 2 CC6.1, CC6.6, CC8.1; ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` |
| `IT-RBAC-634-001` | SubjectAccessReview allows only trusted-namespace `get` and `list`. | `internal/controller/trustedworkflowcatalogrbac_test.go` | FedRAMP/NIST AC-3, AC-6; SOC 2 CC6.1, CC6.6; ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` |
| `IT-RBAC-634-001` | SubjectAccessReview denies watch, create, update, patch, delete, status/finalizer subresources, foreign-namespace requests, and cluster-wide requests. | `internal/controller/trustedworkflowcatalogrbac_test.go` | FedRAMP/NIST AC-3, AC-6; SOC 2 CC6.1, CC6.6; ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` |
| `IT-RBAC-634-002` | A disabled APIFrontend does not receive the catalog Role or RoleBinding. | `internal/controller/trustedworkflowcatalogrbac_test.go` | FedRAMP/NIST AC-3, AC-6, CM-6; SOC 2 CC6.1, CC6.6; ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` |
| `IT-RBAC-634-003` | An enabled-to-disabled transition prunes the previously reconciled APIFrontend catalog Role and RoleBinding. | `internal/controller/trustedworkflowcatalogrbac_test.go` | FedRAMP/NIST AC-3, AC-6, CM-6, SI-4; SOC 2 CC6.1, CC6.6, CC8.1; ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` |
| `E2E-RBAC-634-001` | A live deployed APIFrontend identity can read the trusted catalog and cannot use the denied authorization cases above. | Upstream Kubernaut FullPipeline/live-cluster qualification | FedRAMP/NIST AC-3, AC-6; SOC 2 CC6.1, CC6.6, CC8.1; ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` |

## Test data and isolation

- Use the existing minimal `Kubernaut` fixture and its configured controller
  namespace.
- Set the APIFrontend identity to
  `system:serviceaccount:<kn.Namespace>:apifrontend`.
- Use a distinct foreign namespace and the empty namespace in SAR requests to
  exercise namespace and cluster-scope boundaries.
- Clean up namespaced and cluster-scoped fixtures after each integration spec.

## Execution gates

1. Focused unit and integration specs pass.
2. `make test-unit` passes with at least 90% internal unit coverage and no
   missed handwritten business statements.
3. `make test-integration` passes with at least 78% controller integration
   coverage.
4. `go build ./...`, `golangci-lint run`, `make manifests generate`, and the
   relevant Helm checks pass without unexpected generated/config drift.

The operator's local unit and envtest evidence does not replace live
qualification. If no authorized cluster is configured, `kubectl auth can-i`
and upstream FullPipeline results must be recorded as pending rather than
claimed as passed.

## Execution record — 2026-11-13

- Focused renderer and APIFrontend SAR specs passed.
- `make test-unit` passed: 0 missed handwritten business statements and
  100.0% internal unit coverage.
- `make test-integration` passed: 78.8% controller integration coverage.
- `go build ./...` passed.
- `golangci-lint run` passed with 0 issues.
- `make test-helm` passed: 13 Helm specs plus chart and release-contract
  validation passed.
- `make manifests generate && git diff --exit-code config/` passed with no
  unexpected generated/config drift.
- `go test ./... -run='^$' -timeout=30s` passed.
- Live `kubectl auth can-i` was not executed: no current kubeconfig context is
  configured, and the default localhost API endpoint is unreachable. Upstream
  Kubernaut FullPipeline qualification remains pending.

## Acceptance criteria

- When APIFrontend is enabled, it can `get` and `list`
  `kubernaut.ai/remediationworkflows` only in `kn.Namespace`.
- When APIFrontend is disabled, no APIFrontend catalog Role/RoleBinding remains
  after reconciliation.
- APIFrontend cannot watch, mutate, update subresources, access a foreign
  namespace, or make a cluster-wide catalog request through this grant.
- Existing trusted catalog grants for #610/#624 remain unchanged.
- No CRD, generated API, chart, bundle, or new reconciler path is required.
- Evidence is reported separately for local tests and live-cluster
  qualification; no formal FedRAMP, SOC 2, or ASVS certification claim is made.

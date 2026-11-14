# Issue #634 — APIFrontend workflow catalog control traceability

**Matrix ID:** KO-SEC-TR-634-001  
**Version:** 1.0  
**Status:** Local implementation evidence complete; live qualification pending  
**Test plan:** [`docs/tests/634/TEST_PLAN.md`](../tests/634/TEST_PLAN.md)  
**Design:** [`docs/design/ISSUE-610-TRUSTED-WORKFLOW-CATALOG-RBAC.md`](../design/ISSUE-610-TRUSTED-WORKFLOW-CATALOG-RBAC.md)

This matrix records engineering evidence for the APIFrontend trusted
`RemediationWorkflow` catalog permission using NIST SP 800-53 Rev. 5 /
FedRAMP-oriented objectives, SOC 2 objectives, and exact OWASP ASVS 5.0.0
requirement identifiers. It does not claim formal FedRAMP authorization, SOC 2
attestation, or OWASP ASVS conformance.

Authoritative references:

- [NIST SP 800-53 Rev. 5 Update 1](https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final)
- [FedRAMP Rev. 5 control reference](https://www.fedramp.gov/2026/reference/controls/)
- [OWASP ASVS 5.0.0 requirement catalog](https://raw.githubusercontent.com/OWASP/ASVS/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv)

| Business behavior | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 objective | Evidence | Status |
|---|---|---|---|---|---|
| When APIFrontend is enabled, the operator grants it access to the trusted `remediationworkflows` resource in the configured Kubernaut controller namespace, not the workflow-execution namespace. | AC-3, AC-6, CM-6 | CC6.1, CC6.6 | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | `UT-RBAC-624-001/634-001`; `IT-RBAC-634-001`; `internal/resources/rbac.go` | verified locally; repository gates passed |
| The permission is limited to `get` and `list`; no watch or mutation verb is included. | AC-3, AC-6 | CC6.1, CC6.6 | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | `UT-RBAC-624-001/634-001`; `IT-RBAC-634-001` | verified locally; repository gates passed |
| APIFrontend cannot use the grant for `status` or `finalizers` subresources, foreign namespaces, or cluster-wide requests. | AC-3, AC-6 | CC6.1, CC6.6 | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | `IT-RBAC-634-001` SubjectAccessReview denial matrix | verified by focused envtest |
| When APIFrontend is disabled, the operator omits the catalog grant and removes an owned Role/RoleBinding left by an enabled-to-disabled transition. | AC-3, AC-6, CM-6, SI-4 | CC6.1, CC6.6, CC8.1 | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | `UT-RBAC-634-002`; `IT-RBAC-634-002`, `IT-RBAC-634-003`; `internal/controller/kubernaut_controller.go` | verified locally; repository gates passed |
| The RoleBinding targets only the APIFrontend ServiceAccount in the trusted namespace and is reconciled through the existing namespaced RBAC path. | AC-6, CM-6 | CC6.6, CC8.1 | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | `UT-RBAC-624-001/634-001`; `IT-RBAC-634-001`; `internal/controller/kubernaut_controller.go` | verified locally; repository gates passed |
| The deployed APIFrontend identity can read trusted workflows during the upstream approval journey without receiving broader catalog authority. | AC-3, AC-6, CM-6 | CC6.1, CC6.6, CC8.1 | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | `E2E-RBAC-634-001`; upstream Kubernaut FullPipeline PR #2613 qualification | pending authorized live-cluster/upstream execution |

## Evidence artifacts

- `internal/resources/rbac.go`
- `internal/resources/rbac_test.go`
- `internal/controller/kubernaut_controller.go`
- `internal/controller/trustedworkflowcatalogrbac_test.go`
- `docs/design/ISSUE-610-TRUSTED-WORKFLOW-CATALOG-RBAC.md`
- `docs/tests/634/TEST_PLAN.md`

## Verification record

Local focused tests, `make test-unit`, `make test-integration`, `go build ./...`,
`golangci-lint run`, `make test-helm`, generated-artifact checks, and compile-only
tests passed on 2026-11-13. Internal unit coverage was 100.0% with zero missed
handwritten business statements; controller integration coverage was 78.8%.

Live `kubectl auth can-i` evidence is pending because no kubeconfig context is
configured in this environment. The default localhost API endpoint was also
unreachable. The upstream Kubernaut FullPipeline gate must deploy the final
operator artifact and verify the real APIFrontend ServiceAccount before the
E2E row can be marked passed.

## Residual boundary

The operator proves the rendered Role/RoleBinding and effective Kubernetes
authorization in envtest. A live `kubectl auth can-i` check requires an
authorized cluster and the actual deployed ServiceAccount. End-to-end approval
parameter resolution, APIFrontend image compatibility, release provenance, and
the Kubernaut FullPipeline gate remain upstream qualification concerns. Until
those checks execute against the final operator artifact, live E2E evidence is
pending and must not be inferred from unit or envtest success.

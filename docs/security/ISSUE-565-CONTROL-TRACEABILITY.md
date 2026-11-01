# Issue #565 — External exposure control traceability

**Matrix ID:** KO-SEC-TR-565-001  
**Version:** 1.0  
**Status:** Implementation evidence in progress  
**Test plan:** [`docs/tests/565/TEST_PLAN.md`](../tests/565/TEST_PLAN.md)  
**Design:** [`docs/design/DD-565-EXTERNAL-EXPOSURE-BOUNDARY.md`](../design/DD-565-EXTERNAL-EXPOSURE-BOUNDARY.md)

This is an engineering traceability matrix using NIST SP 800-53 Rev. 5 /
FedRAMP-oriented objectives, SOC 2 objectives, and exact OWASP ASVS 5.0.0
requirement identifiers. It does not claim formal FedRAMP authorization, SOC 2
attestation, or OWASP ASVS conformance.

| Business behavior | FedRAMP/NIST | SOC 2 | OWASP ASVS 5.0.0 | Evidence |
|---|---|---|---|---|
| The CRD publishes no CR-managed Route/Ingress exposure fields. | CM-3, CM-6, SI-10 | CC8.1 | v5.0.0-V16.5.2 | `UT-EXP-565-API-001` |
| The operator manager has no external Route/Ingress management permission. | AC-3, AC-6, SC-7 | CC6.1, CC6.6 | v5.0.0-V8.2.1, v5.0.0-V8.3.1, v5.0.0-V13.1.1 | `UT-EXP-565-RBAC-001` |
| Status reports internal/admin-owned exposure and never reports RouteReady. | CM-6, SI-4 | CC7.2 | v5.0.0-V16.5.2 | `UT-EXP-565-STATUS-001` |
| Administrator-selected Console callback URL is HTTPS, absolute, and bounded to the OAuth callback path. | AC-6, SC-8, SI-10 | CC6.1, CC8.1 | v5.0.0-V2.2.1, v5.0.0-V12.2.1 | `UT-EXP-565-CONSOLE-001`, `UT-EXP-565-CONSOLE-002` |
| Alertmanager delivery remains on the internal Gateway Service FQDN with HTTPS. | SC-8, SC-13, SC-17 | CC6.1, CC7.2 | v5.0.0-V12.2.1, v5.0.0-V13.2.1 | `UT-EXP-565-INTERNAL-001` |
| Operator deployments keep ambient CA trust read-only and leave environment ownership to the upstream runtime. | AC-6, CM-7, SC-8 | CC7.2 | v5.0.0-V12.3.1, v5.0.0-V15.2.5 | `UT-TLS-2545-OP-001`, `UT-TLS-2545-OP-002` |
| Reconciliation reaches readiness without external exposure APIs or objects. | SC-7, CM-6, SI-4 | CC7.2 | v5.0.0-V16.5.2 | `IT-EXP-565-RECONCILE-001` |
| Live OCP journey keeps Gateway on an internal ClusterIP and creates no operator-managed Gateway Route. | SC-7, SC-8, SI-4 | CC6.1, CC7.2 | v5.0.0-V12.2.1, v5.0.0-V16.5.2 | `E2E-EXP-565-001` |

## Evidence artifacts

- `api/v1alpha2/kubernaut_types.go`
- `internal/controller/kubernaut_controller.go`
- `internal/resources/console.go`
- `internal/resources/ocp.go`
- `config/crd/bases/kubernaut.ai_kubernauts.yaml`
- `config/rbac/role.yaml`
- `docs/design/DD-565-EXTERNAL-EXPOSURE-BOUNDARY.md`
- `docs/tests/565/TEST_PLAN.md`

## Residual boundary

Live edge-router, Gateway API, mesh, TLS termination, hostname, and external
authorization behavior remains the administrator/platform deployment’s
responsibility. OCP 4.22.16 API discovery was verified on reachable
`kubernaut492-sno-a` and `kubernaut492-sno-b` through `helios08`; the disposable
image could not be published because certificate-based `system:admin` access
did not provide a registry bearer token, so live E2E evidence remains pending.

# Issue #431 Fleet control traceability

This matrix records repository evidence for the v1alpha2 Fleet contract. It is
not a formal FedRAMP, SOC 2, or OWASP ASVS certification claim.

| Evidence | Control objectives | Repository assertion |
|---|---|---|
| `UT-FLEET-521-API-001` | CM-6, SI-10 | Removed direct scope-check fields and `fleet.resilience` are absent from the CRD. |
| `UT-FLEET-521-ACM-001` | SC-8, SC-12, SC-13 | ACM requires HTTPS and renders the selected trust source. |
| `UT-FLEET-521-SECRET-001` | AC-6, IA-5 | ACM token and CA Secret references are keyed and mounted read-only only where consumed. |
| `UT-FLEET-521-FMC-001` | SC-8, SC-12 | FMC uses only the operator-derived HTTPS Service URL and inter-service trust. |
| `UT-FLEET-521-VALIDATE-001` | SI-10, CM-6 | Invalid provider combinations and non-positive MCP-client durations fail before deployment. |
| `UT-FLEET-521-VALIDATE-002` | SC-8, SC-13, SI-10 | Plaintext MCP Gateway and OAuth2 endpoints are rejected. |
| `UT-FLEET-521-SECRET-001` | IA-5, AC-6 | ACM token Secret existence and selected key are validated before deployment. |
| `UT-FLEET-521-MCPCLIENT-001` | CM-6 | Shared MCP-client retry/timeout settings render consistently to Fleet-aware consumers. |
| `IT-FLEET-521-RECONCILE-ACM` / `IT-FLEET-521-RECONCILE-FMC` | AC-6, IA-5, SC-8, SC-12 | Controller reconciliation proves production wiring for both provider paths, including derived FMC trust and ACM token mounts. |

Full application Fleet E2E remains upstream-owned under kubernaut#2492/#2488.

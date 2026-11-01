# Issue #483 — Console logout control traceability

This document records business-level evidence for the operator-owned Console
logout configuration. It is not a standalone FedRAMP, SOC 2, or OWASP ASVS
compliance claim.

| Behavior | FedRAMP / NIST | SOC 2 | OWASP ASVS 5.0 | Evidence |
|---|---|---|---|---|
| Logout endpoint is optional, HTTPS-only, same-authority, and rejects unsafe return targets | AC-6 | CC6.1 | V1.2.2, V1.3.6, V5, V8.3.1 | `UT-483-VALIDATE-001..006`, `UT-CD-483-002/003` |
| OAuth2 Proxy receives the literal `{id_token}` placeholder; the SPA receives no logout URL or token | AC-6, AC-12 | CC6.1, CC6.3 | V3.3, V7.2.1, V8.3.1 | `UT-CD-483-001`, source review |
| Public and self-signed provider certificates remain certificate-verified | AC-6, SC-8 | CC6.1 | V12.3.2, V12.3.4, V8.3.1 | `UT-CD-483-001`; QE `E2E-483-TLS-001` |
| Fresh operator reconciliation creates and regenerates the logout configuration | AC-2, AC-12 | CC6.1, CC6.3 | V3.3, V7.2.1, V8.3.1 | `IT-CL-483-001`, `IT-483-FAIL-CLOSED-001` |
| Provider session termination, fresh login, and transcript clearing occur in the browser | AC-2, AC-12 | CC6.1, CC6.3 | V3.3, V7.2.1, V8.3.1 | QE `kubernaut-console/e2e/live/logout.spec.ts` |

Local unit and envtest evidence covers operator rendering, validation, trust
material wiring, and reconciliation. QE owns the external provider/browser
qualification; deployment logs and test artifacts must not contain ID token
values or credentials. ASVS V5 is retained as the chapter-level reference;
the precise input-validation controls used here are V1.2.2 and V1.3.6.

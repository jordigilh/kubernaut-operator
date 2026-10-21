# Issue #557 MCP Gateway TLS control traceability

This document records engineering evidence for the source-aware Fleet MCP
Gateway trust lane. It is **not** a formal FedRAMP, SOC 2, or OWASP ASVS
certification or authorization claim.

| Business assertion | FedRAMP/NIST | SOC 2 | OWASP ASVS 5.0.0 | Evidence |
|---|---|---|---|---|
| MCP Gateway endpoints remain absolute HTTPS URLs; no disable/plaintext mode exists. | SC-8, SC-13, SI-10 | CC6.6, CC8.1 | V2.2.1, V2.2.2, V12.2.1, V16.5.2 | `internal/resources/validation.go`, `UT-FLEET-521-VALIDATE-002`, `UT-FLEET-557-API-001` |
| Gateway, scope-check, OAuth2, and internal-service trust are distinct and source-resolved. | SC-8, SC-12, SC-13, SC-17, CM-6 | CC6.1, CC6.6 | V12.1.1, V12.1.3, V12.3.1, V12.3.2 | `internal/resources/configmaps.go`, `internal/resources/configmaps_test.go`, upstream PR #2541 |
| Secret-backed CA material is namespace-local, keyed, PEM-validated, read-only, and never adopted. | AC-6, IA-5, SC-12 | CC6.1, CC6.6 | V13.3.1, V13.3.2 | `validateFleetTrustSecrets`, `UT-FLEET-557-SECRET-001`, `UT-FLEET-557-MOUNT-001`, `IT-FLEET-557-RECONCILE-MCP-CA` |
| Invalid Secret rotations fail closed; valid rotations use non-sensitive rollout metadata. | SI-4, SI-10, CM-6 | CC7.2, A1 | V16.5.2, V13.3.1 | `fleetTrustSecretToKubernaut`, `fleetMCPGatewayTrustRevision`, `UT-FLEET-557-ROTATION-001` |
| Runtime MCP traffic uses the dedicated gateway CA while OAuth2 token acquisition remains independently trusted. | SC-8, SC-13, IA-5 | CC6.6, CC7.2 | V12.3.1, V12.3.2, V13.2.1 | upstream PR #2541 `UT-FLEET-CONN-004`, upstream `mcpclient.Connect` wiring |
| Reconciliation failures are observable without exposing protected material. | AU-2, AU-3, AU-12, SI-4 | CC7.2, CC8.1 | V16.1.1, V16.2.1, V16.5.2 | structured controller logs/status conditions, envtest reconciliation |

Full EAIGW/Kuadrant HTTPS Fleet qualification remains upstream-owned under
kubernaut#2492/#2488 and must be tied to the exact operator revision and image
provenance. Repository evidence does not establish formal compliance.

# Issue #431 Fleet contract test plan

**Issue:** [kubernaut-operator#431](https://github.com/jordigilh/kubernaut-operator/issues/431)
**Related API decision:** [DD-521](../../design/DD-521-FLEET-API-CONTRACT.md)
**Status:** v1alpha2 contract evidence

## Contract and pyramid

| Tier | Test ID | Artifact | Contract |
|---|---|---|---|
| Unit | `UT-FLEET-521-ACM-001` | `internal/resources/configmaps_test.go` | The rendered Gateway ConfigMap contains the ACM endpoint, trust path, and token path. |
| Unit | `UT-FLEET-521-RENDER-SECRET-001` | `internal/resources/configmaps_test.go` | The rendered APIFrontend ConfigMap contains ACM `tlsCAFile` and `tokenPath` resource paths. This is ConfigMap/resource-path evidence, not Secret existence or mount evidence. |
| Unit | `UT-FLEET-521-RENDER-SECRET-002` | `internal/resources/configmaps_test.go` | The rendered RemediationOrchestrator ConfigMap contains ACM trust and token resource paths. This is ConfigMap/resource-path evidence, not Secret existence or mount evidence. |
| Unit | `UT-FLEET-521-PREFLIGHT-SECRET-001` | `internal/controller/business_api_boundaries_unit_test.go` | Controller preflight rejects a missing ACM token Secret, rejects a missing selected key, and accepts the selected key before deployment. |
| Unit | `UT-FLEET-521-MOUNT-SECRET-001`; `UT-FLEET-521-MOUNT-SECRET-002` | `internal/resources/deployments_test.go` | Deployment builders mount ACM CA/token Secrets read-only only for backend consumers. |
| Unit | `UT-FLEET-521-FMC-001` | `internal/resources/fleetmetadatacache_test.go` | FMC endpoint is derived from the Kubernaut namespace and uses HTTPS/inter-service trust. |
| Unit | `UT-FLEET-521-MCPCLIENT-001` | `internal/resources/configmaps_test.go` | `mcpClient.retry/timeouts` map consistently to upstream runtime fields. |
| Unit | `UT-FLEET-521-VALIDATE-001`–`002` | `internal/resources/validation_test.go` | Unknown/provider-mismatched backends, plaintext Fleet endpoints, missing ACM token, and non-positive durations fail closed. |
| API contract | `UT-FLEET-521-API-001` | `api/v1alpha2/clean_break_test.go` | CRD contains nested ACM and mcpClient fields and omits direct scope-check/Fleet resilience fields. |
| Integration | `IT-FLEET-521-RECONCILE-ACM` | `internal/controller/fleetmetadatacache_test.go` | Actual Reconcile wires the ACM rendering and Secret boundaries. |
| Integration | `IT-FLEET-521-RECONCILE-FMC` | `internal/controller/fleetmetadatacache_test.go` | Actual Reconcile wires FMC derived endpoint and inter-service trust. |
| Integration | `IT-FLEET-521-ADMISSION-001` | `internal/controller/kubernaut_v1alpha2_admission_test.go` | v1alpha2 admission stores the clean-break schema and rejects invalid contract values. |
| Upstream E2E | `E2E-FLEET-431-UPSTREAM` | upstream kubernaut#2492/#2488 | Full Fleet qualification remains upstream-owned; no new local qualification is claimed. |

Repository tests prove generated object and reconciliation wiring contracts.
The statement-coverage gate is best-effort business-logic evidence, not a
claim of exhaustive branch coverage. These tests are not a formal FedRAMP,
SOC 2, or ASVS certification claim.

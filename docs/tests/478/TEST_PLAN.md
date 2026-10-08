# Issue #478 OTLP telemetry TLS test plan

**Plan ID:** `KO-TP-478-001`<br>
**Date:** 2026-10-08<br>
**Scope:** Gateway, DataStorage, and Kubernaut Agent OTLP telemetry

This is repository evidence for the Issue #478 contract. It is not a formal
FedRAMP, NIST, or OWASP ASVS assessment.

## Acceptance matrix

| ID | Scenario | Evidence | Result |
|---|---|---|---|
| `UT-TELEMETRY-TLS-001` | Disabled, log-sink-only, stdout-only, host:port, explicit HTTPS, and plaintext/invalid endpoint validation. | `internal/resources/validation_test.go`, `internal/resources/configmaps_test.go` | Pass |
| `UT-TELEMETRY-TLS-002` | CA source exclusivity, Secret key/path safety, complete client pair, mount-collision, and endpoint field-qualified errors. | `internal/resources/validation_test.go` | Pass |
| `UT-TELEMETRY-TLS-003` | Secret identity resolution and read-only CA/client/projected mounts. | `internal/resources/telemetry_test.go`, `internal/resources/deployments_test.go` | Pass |
| `UT-TELEMETRY-TLS-004` | Private-CA and mTLS configuration is rendered without a TLS-disable field. | `internal/resources/configmaps_test.go`, `internal/resources/telemetry_runtime_test.go` | Pass |
| `IT-TELEMETRY-TLS-001` | All three network producers reconcile with read-only Secret mounts, canonical config, and non-sensitive rollout revisions. | `internal/controller/telemetry_integration_test.go` | Pass |
| `IT-TELEMETRY-TLS-002` | Missing or invalid Secret material reports a readiness failure before deployment; invalid rotation preserves existing Deployments. | `internal/controller/telemetry_integration_test.go` | Pass |
| `IT-TELEMETRY-TLS-003` | Local-only telemetry reconciles without network Secret requirements or telemetry mounts. | `internal/controller/telemetry_integration_test.go` | Pass |
| `IT-TELEMETRY-TLS-004` | Gateway, DataStorage, and Kubernaut Agent use the shared source/path policy, including Gateway's optional enablement. | `internal/controller/telemetry_integration_test.go`, `internal/resources/configmaps_test.go` | Pass |
| `IT-TELEMETRY-TLS-005` | Referenced Secret identity and resource-version changes produce only non-sensitive rollout revisions; Secret ownership remains unchanged. | `internal/controller/telemetry_test.go`, `internal/controller/telemetry_integration_test.go` | Pass |
| `IT-TELEMETRY-TLS-006` | A private-CA/mTLS HTTPS handshake succeeds through the upstream runtime with explicit telemetry trust/client paths. | `internal/resources/telemetry_runtime_test.go` | Pass |
| `IT-TELEMETRY-TLS-007` | Referenced Secret events map to the singleton and valid/invalid rotation behavior is reconciled. | `internal/controller/telemetry_test.go`, `internal/controller/telemetry_integration_test.go` | Pass |
| `IT-TELEMETRY-TLS-008` | Ambient trust is validated fail-closed, pending injection is requeued, and malformed trust cannot create a rollout revision. | `internal/controller/telemetry_test.go`, `internal/controller/telemetry_integration_test.go` | Pass |

The four integration specs use the real reconciler and envtest API server;
focused controller tests cover the event-mapping and ambient-trust branches
that do not require starting a controller manager. The upstream runtime test
uses a local HTTPS collector with a private CA and client certificate and does
not emit certificate or key bytes in test output.

## Verification commands

```text
make test
make lint
go build ./...
make test-hack-scripts
```

Observed final repository-only evidence:

- unit coverage: **87.2%** overall internal unit profile;
- controller integration coverage: **79.2%**;
- issue-scoped resource validation, renderer, and Secret-mount helpers:
  **100% statement coverage**;
- `golangci-lint run`: no issues;
- generated CRD/manifests: regenerated with no unexpected generated diff;
- security traceability, CI-boundary, and test-pyramid checks: pass.

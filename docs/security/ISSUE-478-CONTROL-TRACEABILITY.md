# Issue #478 OTLP telemetry TLS control traceability

**Matrix ID:** `KO-SEC-TR-478-001`<br>
**Version:** `0.1`<br>
**Date:** 2026-10-08<br>
**Scope:** Operator-rendered OTLP telemetry for Gateway, DataStorage, and
Kubernaut Agent

This is an engineering evidence matrix, not a FedRAMP authorization or an
OWASP ASVS conformance claim. The control mappings use NIST SP 800-53 Rev. 5
and the FedRAMP Rev. 5 baseline as applicable objectives. ASVS identifiers are
the exact requirement IDs from OWASP ASVS 5.0.0. Formal compliance requires
independent assessment, organizational evidence, and qualified deployment
evidence.

## Control and evidence matrix

| Behavior | NIST SP 800-53 / FedRAMP | OWASP ASVS 5.0.0 | Repository evidence | Status |
|---|---|---|---|---|
| A configured OTLP endpoint is certificate-verifying TLS; plaintext HTTP and invalid endpoint forms are rejected. | SC-8, SC-8(1), SC-13, SI-10 | v5.0.0-V12.2.1, v5.0.0-V12.3.1, v5.0.0-V12.3.2, v5.0.0-V12.3.4 | `internal/resources/validation.go`; `internal/resources/validation_test.go`; `internal/resources/configmaps.go`; `internal/resources/configmaps_test.go`; `internal/resources/telemetry_runtime_test.go` | partially verified |
| Public collector trust preserves system roots; private CA material is explicit and isolated from inter-service trust. | SC-8, SC-12, SC-13, SC-17, CM-6 | v5.0.0-V12.3.1, v5.0.0-V12.3.2, v5.0.0-V12.3.3, v5.0.0-V12.3.4 | `internal/resources/telemetry.go`; `internal/resources/configmaps.go`; `internal/controller/telemetry.go`; `docs/security/credentials-and-tls.md` | partially verified |
| Optional mTLS client material is validated as a matching, time-valid client-authentication certificate/key pair. | IA-5, IA-5(2), SC-12, SC-13, SI-10 | v5.0.0-V12.3.2, v5.0.0-V12.3.4, v5.0.0-V12.3.5 | `internal/controller/telemetry.go`; `internal/controller/telemetry_test.go`; `internal/controller/telemetry_integration_test.go` | partially verified |
| Administrator-owned CA/client Secrets are read-only, never adopted, and never exposed in status, logs, annotations, or ConfigMaps. | AC-6, IA-5, SC-12, SC-13 | v5.0.0-V13.3.1, v5.0.0-V13.3.2, v5.0.0-V14.2.4 | `internal/resources/telemetry.go`; `internal/resources/telemetry_test.go`; `internal/controller/telemetry.go`; `internal/controller/telemetry_integration_test.go` | partially verified |
| Invalid material blocks progression and valid Secret/ambient-trust changes roll only affected network telemetry producers using non-sensitive revisions. | CM-6, SI-4, SI-10, SC-8 | v5.0.0-V12.3.4, v5.0.0-V14.2.4 | `internal/controller/telemetry.go`; `internal/controller/telemetry_integration_test.go`; `internal/controller/telemetry_test.go` | partially verified |
| Telemetry-disabled, log-sink-only, and stdout-only modes remain local and do not require network Secret material. | SC-7, SC-8, CM-6 | v5.0.0-V12.1.3, v5.0.0-V12.2.1 | `internal/resources/validation_test.go`; `internal/resources/configmaps_test.go`; `internal/resources/deployments_test.go`; `internal/controller/telemetry_integration_test.go` | partially verified |
| The implementation is wired through the reconciliation path and generated resources remain reproducible. | SA-11, CM-6, SI-4 | v5.0.0-V14.2.4 | `internal/controller/kubernaut_controller.go`; `hack/verify-test-pyramid.sh`; `Makefile`; `config/`; `bundle/`; `dist/` | partially verified |

## Evidence boundary and residual risk

- Unit evidence covers the pure validation, rendering, mount, and material
  resolution behavior; controller integration evidence uses envtest.
- The upstream runtime qualification test executes a private-CA/mTLS HTTPS
  handshake. It is not a substitute for a production collector, platform
  egress, PKI, or hostname-qualification review.
- OpenShift service-CA injection, external Secret providers, NetworkPolicy
  egress enforcement, and production rollout monitoring require live-cluster
  evidence.
- No private key, CA bytes, Secret data, or credential is asserted in status,
  logs, annotations, or generated ConfigMaps.
- This document does not claim formal FedRAMP authorization, NIST control
  approval, or OWASP ASVS compliance/conformance.

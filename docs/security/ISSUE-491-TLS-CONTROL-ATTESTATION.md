# Issue #491 TLS parity control-objective attestation

**Matrix ID:** `KO-SEC-AT-491-001`  
**Version:** `0.2`  
**Date:** `2026-10-03`  
**Scope:** Kubernaut Operator Issue #491 / PR #493  
**Status:** repository and hosted Kind runtime evidence complete; OpenShift qualification and external-assessment evidence pending

## Attestation boundary

This document is an engineering attestation of the repository behavior and
evidence produced for Helm-compatible TLS parity. It is not a FedRAMP
authorization, a SOC 2 report or attestation, or an OWASP ASVS certification.
Those claims require independent assessment, organizational control evidence,
and qualified deployment evidence that cannot be established by source code and
repository tests alone.

This document does not establish formal authorization and does not claim formal FedRAMP authorization, SOC 2 attestation, or OWASP ASVS compliance.

The attested claim is narrower and testable:

> Within the supported operator contract, the TLS source, ownership, trust,
> cryptographic, rotation, readiness, and failure behaviors listed below are
> implemented, wired into reconciliation, and covered by the cited automated
> evidence.

Evidence status has two dimensions:

- **Repository verified:** the cited source, test, generated artifact, or CI
  check passed in this repository.
- **Overall partially verified:** live-platform, organizational, or independent
  assessment evidence is still required before a formal control claim can be
  made.

## Control vocabulary

- **FedRAMP/NIST:** NIST SP 800-53 Rev. 5 control objectives selected for this
  operator: `AC-6`, `CM-6`, `IA-5`, `SC-8`, `SC-12`, `SC-13`, `SC-17`, `SI-4`,
  and `SI-10`.
- **SOC 2:** project control-objective references `CC6` (logical access and
  protection), `CC7` (monitoring and incident response), `CC8` (change
  management), and `A1` (availability).
- **OWASP ASVS:** exact requirement identifiers from ASVS `v5.0.0`:
  `v5.0.0-V11.1.1`, `v5.0.0-V11.1.2`, `v5.0.0-V12.1.1`,
  `v5.0.0-V12.1.2`, `v5.0.0-V12.1.3`, `v5.0.0-V12.2.1`,
  `v5.0.0-V13.2.1`, `v5.0.0-V13.3.1`, `v5.0.0-V13.3.2`, and
  `v5.0.0-V16.5.2`.

The ASVS source is the versioned official CSV:
<https://raw.githubusercontent.com/OWASP/ASVS/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv>.

## Business assertions and evidence

| Assertion | Business behavior verified | FedRAMP/NIST | SOC 2 | OWASP ASVS 5.0.0 | Automated evidence | Repository status |
|---|---|---|---|---|---|---|
| `BA-491-TLS-01` Source selection is explicit and safe | `hook`, lower-case `cert-manager`, legacy reference-only `CertManager`, and `manual` resolve to distinct ownership paths; unsupported values do not fall back to plaintext. | `AC-6`, `SC-8`, `CM-6`, `SI-10` | `CC6`, `CC8` | `V12.2.1`, `V13.3.1`, `V16.5.2` | `UT-TLS-GAP-002`; `UT-TLS-491-001`; `UT-TLS-491-002`; `IT-TLS-GAP-002` | verified |
| `BA-491-TLS-02` Administrator material is read-only | Manual trust ConfigMaps, serving Secrets, signing material, and webhook bundles are validated and consumed without adoption, owner references, overwrite, or deletion. | `AC-6`, `IA-5`, `SC-8`, `SC-17` | `CC6`, `CC7` | `V12.1.3`, `V13.3.1`, `V13.3.2` | `IT-TLS-MANUAL-001`; `IT-TLS-MANUAL-002`; `IT-TLS-WEBHOOK-001`; existing `UT-TLS-GAP-001` | verified |
| `BA-491-TLS-03` cert-manager owns generated Secrets | The operator creates only the intended Issuer/Certificate resources, owner-references Certificates to the Kubernaut CR, never adopts output Secrets, and waits for every Certificate to become Ready. | `AC-6`, `CM-6`, `SC-12`, `SC-13`, `SI-4` | `CC6`, `CC7`, `CC8` | `V11.1.1`, `V12.1.1`, `V13.3.1`, `V13.3.2`, `V16.5.2` | `IT-TLS-PARITY-001`; `IT-TLS-CERTMANAGER-READY-001`; `IT-TLS-GAP-001`; `UT-TLS-491-003`; `UT-TLS-491-004`; `UT-TLS-491-005`; `E2E-TLS-CERTMANAGER-001` | verified |
| `BA-491-TLS-04` Cryptography and identity are correct | Inter-service CA/leaves use ECDSA, DataStorage signing uses RSA-2048, server/client usages are explicit, stable service DNS SANs are present, and configured extra SANs plus loopback are honored. | `IA-5`, `SC-8`, `SC-12`, `SC-13`, `SC-17` | `CC6`, `CC7` | `V11.1.1`, `V11.1.2`, `V12.1.1`, `V12.1.2`, `V12.1.3`, `V13.2.1` | `UT-TLS-491-002`; `UT-TLS-491-004`; `IT-TLS-MANUAL-002`; `test/e2e/kind/scenarios_test.go` | verified |
| `BA-491-TLS-05` Trust reaches every consumer | Dynamic certificate/CA paths are propagated to workloads, Console, Fleet Metadata Cache, migration, generic trust ConfigMaps, webhooks, and Ingress without OpenShift-only annotations in generic mode. | `SC-8`, `SC-17`, `SI-4` | `CC6`, `CC7`, `A1` | `V12.1.3`, `V13.2.1`, `V16.5.2` | `UT-TLS-491-007`; `UT-TLS-491-008`; `UT-TLS-491-009`; `IT-TLS-GAP-003`; `test/e2e/kind/scenarios_test.go` | verified |
| `BA-491-TLS-06` Webhook trust fails closed | Provisioned webhook configurations use cert-manager cainjector metadata; manually injected bundles are preserved; readiness is not reported until a usable bundle exists. | `SC-8`, `SC-13`, `SC-17`, `SI-4`, `SI-10` | `CC6`, `CC7` | `V12.1.3`, `V12.2.1`, `V13.2.1`, `V16.5.2` | `UT-TLS-491-010`; `UT-TLS-491-011`; `IT-TLS-WEBHOOK-001`; `IT-TLS-CERTMANAGER-READY-001`; `test/e2e/kind/scenarios_test.go` | verified |
| `BA-491-TLS-07` Rotation preserves service availability | Development rotation retains the previous root until consumers move; cert-manager leaf reissuance preserves Certificate ownership, TLS readiness, and webhook trust; failed writes preserve the last working root. | `SC-8`, `SC-12`, `SC-13`, `SI-4` | `CC7`, `A1` | `V11.1.1`, `V11.1.2`, `V12.1.1`, `V16.5.2` | `UT-TLS-ROTATION-GAP-001`; `UT-TLS-ROTATION-GAP-002`; `IT-TLS-ROTATION-GAP-001`; `E2E-TLS-CERTMANAGER-002` | verified |
| `BA-491-TLS-08` Configuration remains compatible and bounded | Helm defaults, issuer precedence, stable names, legacy aliases, optional component selection, and explicit exclusion of PostgreSQL/Valkey server PKI remain deterministic and migration-safe. | `AC-6`, `CM-6`, `SI-10` | `CC6`, `CC8` | `V13.3.1`, `V15.2.4`, `V16.5.2` | `UT-TLS-491-001`; `UT-TLS-491-005`; `UT-TLS-491-006`; migration/resource tests; generated CRD/RBAC; `make manifests generate` | verified |

## Issue #498 dedicated Kind-lane evidence

Issue #498 extends the #491 repository contract with Helm-backed Kind
journeys for every generic TLS source. The status below is intentionally
`partially verified` until the hosted #498 jobs execute; local unit and
integration evidence does not substitute for that runtime evidence.

| Assertion | Business behavior verified | FedRAMP/NIST | SOC 2 | OWASP ASVS 5.0.0 | Automated evidence | Repository status |
|---|---|---|---|---|---|---|
| `BA-498-TLS-01` Source selectors are explicit | Development, hook, manual/admin, and cert-manager selectors map deterministically; unsupported input never falls back to plaintext. | `AC-6`, `SC-8`, `CM-6`, `SI-10` | `CC6`, `CC8` | `V12.2.1`, `V13.3.1`, `V16.5.2` | `UT-TLS-498-001`; `UT-TLS-498-002`; `UT-TLS-498-003`; `UT-TLS-498-004`; `E2E-TLS-HOOK-001`; `E2E-TLS-MANUAL-001`; `E2E-TLS-ADMIN-001`; `E2E-TLS-FAIL-CLOSED-001`; `test/e2e/kind/contract/selector_test.go`; `.github/workflows/test.yml` | partially verified |
| `BA-498-TLS-02` Hook material is operator-owned | The real `hook` CR mode generates usable CA/leaves/signing material, publishes trust, rotates a leaf, and removes only operator-owned TLS Secrets during cleanup. | `AC-6`, `SC-8`, `SC-12`, `SC-13`, `SI-4` | `CC6`, `CC7`, `A1` | `V11.1.1`, `V11.1.2`, `V12.1.1`, `V16.5.2` | `E2E-TLS-HOOK-001`; `E2E-TLS-HOOK-002`; `internal/resources/tls_test.go`; `internal/controller/tls_source_integration_test.go`; `test/e2e/kind/scenarios_test.go` | partially verified |
| `BA-498-TLS-03` Manual/admin material is read-only | Both `manual` and `AdministratorManaged` CR selections consume pre-created CA, serving, webhook, and signing material without owner references, mutation, OpenShift CA injection, or cleanup deletion. | `AC-6`, `IA-5`, `SC-8`, `SC-17` | `CC6`, `CC7` | `V12.1.3`, `V13.3.1`, `V13.3.2` | `IT-TLS-MANUAL-001`; `IT-TLS-MANUAL-002`; `IT-TLS-WEBHOOK-001`; `E2E-TLS-MANUAL-001`; `E2E-TLS-ADMIN-001`; `E2E-TLS-CLEANUP-001`; `test/e2e/kind/tls_fixtures.go` | partially verified |
| `BA-498-TLS-04` Trust and failure behavior are observable | Each lane proves TLS readiness, webhook CA trust, a real workload handshake, invalid-source fail-closed status, and source-specific cleanup. | `SC-8`, `SC-13`, `SI-4`, `SI-10` | `CC7`, `A1` | `V12.1.3`, `V12.2.1`, `V13.2.1`, `V16.5.2` | `E2E-TLS-HOOK-001`; `E2E-TLS-MANUAL-001`; `E2E-TLS-ADMIN-001`; `E2E-TLS-FAIL-CLOSED-001`; `hack/verify-test-pyramid.sh` | partially verified |

The issue-specific design and gate artifacts are
`docs/tests/498/TEST_PLAN.md`, `test/e2e/kind/tls_fixtures.go`,
`test/e2e/kind/scenarios_test.go`, `.github/workflows/test.yml`, and
`hack/verify-test-pyramid.sh`. Platform-facing source support and residual-risk
wording is maintained in `docs/installation/06-platform-support.md`. OpenShift
service-CA/router-CA qualification is explicitly deferred and is not counted as
passing evidence for these rows.

## Test-tier and wiring evidence

### Production artifacts

The control assertions are wired through these production entry points:

- `internal/resources/tls.go`
- `internal/resources/tls_certmanager.go`
- `internal/resources/webhooks.go`
- `internal/resources/common.go`
- `internal/controller/kubernaut_controller.go`
- `internal/controller/kubernaut_lifecycle_test.go`
- `config/crd/bases/kubernaut.ai_kubernauts.yaml`
- `config/rbac/role.yaml`

| Tier | Contract | Evidence |
|---|---|---|
| Unit | Resource builders and pure TLS policy validate shapes, defaults, ownership, SANs, algorithms, paths, and failure behavior without an API server. | `internal/resources/tls_test.go`; `internal/resources/tls_certmanager_test.go`; `internal/resources/webhooks_test.go`; `make test-unit` |
| Integration | Controller seams exercise source validation, manual preservation, cert-manager resource ownership/readiness, webhook behavior, generic trust publication, and failed rotation. | `internal/controller/tls_source_integration_test.go`; `internal/controller/kubernaut_lifecycle_test.go`; `make test-integration` |
| E2E | The operator Helm chart is installed, a real Kubernaut CR is reconciled, TLS readiness and webhook trust are observed, cert-manager-owned Secrets and rotation are checked, and finalizer cleanup runs. | `test/e2e/kind/`; `.github/workflows/test.yml`; `make test-e2e-kind` |
| Wiring | Every new resolver/builder has a production caller plus focused unit and controller evidence; no direct policy helper calls are used by E2E. | `hack/verify-test-pyramid.sh`; `make test-pyramid` |

## Verification record

The following repository gates passed for the implementation and the evidence
gate:

- `make test`: unit coverage `87.3%`; controller integration coverage `78.4%`.
- `make test-pyramid`: passed, including `make test-security-traceability`.
- `go build ./...` and `golangci-lint run` passed.
- `make manifests generate` produced no unexpected generated-artifact drift.
- `git diff --check` passed.

The hosted PR checks are the authoritative live execution record for the
production image and Kind lanes. On corrected revision `dc3b812`, GitHub Actions
run `37126833855` passed Unit/Integration, generic Kind, cert-manager Kind,
Cilium, Calico, and SBOM/vulnerability scanning; the separate build/push run
`37126833854` and Go Lint run `37126833852` also passed. The hosted Kind lanes
therefore provide runtime evidence for the supported generic and provider
scenarios, including the cert-manager source path.

## Residual evidence required for a formal claim

The repository evidence does not establish the following:

1. Independent FedRAMP/NIST control assessment and authorization evidence.
2. SOC 2 control-owner evidence, operating effectiveness, auditor evidence,
   access reviews, logging retention, and incident-response records.
3. Independent ASVS assessment, complete application/dependency inventory,
   runtime cipher/profile qualification, and downstream mTLS behavior for every
   deployment environment.
4. Enterprise PKI, HSM/key-management, Vault/External Secrets, and cluster RBAC
   review evidence.
5. OpenShift/OVN live qualification for the target deployment environment.

Until those artifacts exist, the defensible conclusion is **repository control
objectives verified; overall formal assurance partially verified**. No formal
FedRAMP, SOC 2, or OWASP ASVS compliance claim is made by this document.

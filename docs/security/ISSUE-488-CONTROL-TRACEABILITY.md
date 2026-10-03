# Issue #488 security control traceability

**Matrix ID:** KO-SEC-TR-488-001
**Version:** 0.1
**Date:** 2026-10-02
**Scope:** Kubernaut Operator Issue #488 gap closure
**Status:** evidence matrix in progress

## Reading this matrix

This is an engineering traceability matrix, not a certification package. The
NIST SP 800-53 Rev. 5 / FedRAMP control names are project-selected control
objectives. Each `AC-*`, `SC-*`, `IA-*`, `SI-*`, `AU-*`, and `CM-*` value below
is a requirement-level NIST SP 800-53 Rev. 5 control identifier used by the
corresponding FedRAMP baseline mapping; control enhancements not listed are out
of scope. These mappings do not establish FedRAMP authorization. The OWASP
entries are the exact requirement identifiers from **OWASP ASVS 5.0.0**, not a
claim of ASVS conformance. Formal FedRAMP or ASVS compliance is not claimed
without the required external assessment and organizational evidence.

Status means:

- **verified** — the repository contract has a passing automated artifact for
  the stated scope;
- **partially verified** — automated evidence exists, but live-platform,
  organizational, or external-assessment evidence is still required;
- **not verified** — no approved evidence exists for the stated scope;
- **not applicable** — the objective is outside the operator's responsibility.

The versioned ASVS source is the official `v5.0.0` CSV:
<https://raw.githubusercontent.com/OWASP/ASVS/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv>.

## Evidence identifiers

| Identifier | Evidence |
|---|---|
| `UT-*` | Ginkgo resource/webhook/policy unit tests; no API server |
| `IT-*` | Ginkgo controller tests using envtest or a fake client at the controller seam |
| `E2E-*` | Real production-manifest Kind journey; the cert-manager ID is executed with `KUBERNAUT_E2E_TLS_SOURCE=certmanager` and pinned cert-manager `v1.20.2` |
| `CI-*` | Repository workflow, generated-artifact, lint, SBOM, or vulnerability-scan evidence |

## NIST SP 800-53 Rev. 5 / FedRAMP-oriented objectives

| Control | Objective / requirement boundary | Design and production evidence | Test and CI evidence | Status | Residual risk / follow-up |
|---|---|---|---|---|---|
| AC-3, AC-6 | Enforce authorization and least privilege at the operator/API boundary. | `config/rbac/role.yaml`; `internal/resources/rbac.go`; native-policy ownership and cleanup in `internal/controller/`. | `UT-POLICY-*`; controller RBAC tests; Cilium/Calico Kind ownership and negative-enforcement scenarios. | partially verified | External RBAC review and supported OpenShift/OVN qualification remain required. |
| SC-7 | Boundary protection uses provider-native policy APIs and no raw Kubernetes fallback. | `internal/policy/`; `internal/controller/kubernaut_controller.go`; provider capability detection. | `test/e2e/kind/`; Generic/Cilium/Calico Kind lanes; `ProviderPolicyReady`, deny/allow probes, unmanaged-policy preservation. | partially verified | OVN/OpenShift live qualification is deferred and explicitly unverified. |
| SC-8, SC-12, SC-13, SC-17 | Protect communications, manage trust roots, use approved cryptographic material, and publish usable certificate trust. | `internal/resources/tls.go`; `internal/resources/tls_test.go`; `internal/resources/webhooks.go`; `internal/controller/kubernaut_controller.go`; `internal/controller/tls_source_integration_test.go`; `internal/controller/kubernaut_lifecycle_test.go`; `docs/security/credentials-and-tls.md`. | `UT-TLS-GAP-001`; `UT-TLS-ROTATION-GAP-001`; `IT-TLS-GAP-001`; `IT-TLS-GAP-003`; `E2E-TLS-CERTMANAGER-001`; `E2E-TLS-CERTMANAGER-002`; `.github/workflows/test.yml`. | partially verified | Administrator process, external PKI review, and hosted cert-manager execution evidence remain required. |
| IA-2, IA-5 | Validate authentication references and protect/rotate referenced authenticators. | `internal/controller/kubernaut_controller.go`; secret validation and webhook TokenReview/SAR wiring; `docs/security/credentials-and-tls.md`. | BYO secret negative tests; webhook/controller integration tests; `E2E-TLS-GAP-001`. | partially verified | Identity-provider and enterprise secret-vault qualification are outside this repository-only change. |
| SI-4 | Make capability, readiness, failure, generation, and resource-version signals observable. | Structured controller logging, status conditions, events, and phase transitions in `internal/controller/`. | Controller condition/event tests; Kind status assertions; `CI-CONTROLS-GAP-001` validation. | partially verified | Cluster log aggregation, alerting, retention, and operational monitoring still require deployment evidence. |
| AU-2, AU-3, AU-12 | Record reconciliation/security-relevant events with enough context for investigation. | `docs/security/auditing.md`; `internal/controller/kubernaut_controller.go` structured logs/events. | Controller error/status tests; `E2E-TLS-GAP-001`; generated CI artifacts. | partially verified | The operator intentionally uses structured log-based audit traces; it does not provide platform-service database persistence, hash chains, or retention. |
| SI-10 | Validate CR, TLS, hostname, issuer, policy-input, and optional-API inputs without unsafe fallback. | `api/v1alpha2/`; `internal/resources/validation.go`; TLS source validation and capability gates. | Resource validation suite; webhook tests; `IT-TLS-GAP-002`; generic Kind no-fallback assertions. | partially verified | A complete application-wide input inventory and independent security assessment remain follow-up work. |
| CM-2, CM-3, CM-6, CM-8 | Keep generated manifests, ownership boundaries, optional API inventory, and configuration baselines reproducible. | `Makefile`; `config/`; `bundle/`; `docs/installation/06-platform-support.md`; no operator Helm chart in this scope. | `make manifests generate`; `CI-PYRAMID-GAP-001`; build/lint/test gates. | partially verified | Dedicated operator Helm bootstrap and OpenShift/OVN inventory are deferred follow-ups. |

## OWASP ASVS 5.0.0 requirement traceability

The IDs below use the required versioned form `v5.0.0-<requirement-id>`.
Requirement text is summarized from the official ASVS source linked above.

| ASVS 5.0.0 requirement | Summary | Repository evidence | Test / CI evidence | Status | Residual risk / follow-up |
|---|---|---|---|---|---|
| `v5.0.0-V4.1.4` | Permit only explicitly supported HTTP methods. | Admission webhook rule declarations in `internal/resources/webhooks.go`. | Webhook resource tests; controller webhook wiring tests. | partially verified | No independent external API-method assessment is included in this work. |
| `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | Restrict function-level access and enforce authorization at a trusted service layer. | `config/rbac/role.yaml`; webhook authorization path; provider ownership checks. | `UT-POLICY-*`; webhook tests; provider Kind negative probes. | partially verified | Full consumer/application authorization assessment is outside operator scope. |
| `v5.0.0-V11.1.1`, `v5.0.0-V11.1.2` | Document cryptographic key lifecycle and maintain a cryptographic inventory. | `docs/security/credentials-and-tls.md`; TLS source and rotation implementation in `internal/resources/tls.go`. | `UT-TLS-ROTATION-GAP-001`; `IT-TLS-GAP-001`; CI traceability check. | partially verified | Enterprise key-management standard, HSM use, and inventory ownership require external evidence. |
| `v5.0.0-V12.1.1`, `v5.0.0-V12.1.2`, `v5.0.0-V12.1.3` | Use current TLS versions/ciphers and validate trusted mTLS client certificates where applicable. | TLS profile resolution; serving certificate/CA/SAN validation in `internal/resources/tls.go`; PostgreSQL/Valkey TLS configuration. | TLS resource tests; controller readiness tests; Kind TLS lane. | partially verified | Runtime cipher/profile and all downstream client behavior require platform-specific qualification. |
| `v5.0.0-V12.2.1` | Do not fall back to unencrypted external-facing HTTP connectivity. | Explicit TLS modes, fail-closed source validation, and generic webhook CA publication. | `UT-TLS-GAP-001`; `IT-TLS-GAP-002`; `E2E-TLS-GAP-001`. | partially verified | External ingress/controller and service mesh behavior are not fully assessed here. |
| `v5.0.0-V13.2.1` | Authenticate backend component communications. | Inter-service mTLS trust bundle, Service DNS SAN validation, projected ServiceAccount identity. | `UT-TLS-ROTATION-GAP-001`; controller deployment/trust tests; Kind lifecycle. | partially verified | Each production dependency's mutual-auth behavior needs deployment-specific validation. |
| `v5.0.0-V13.3.1`, `v5.0.0-V13.3.2` | Manage secrets securely and apply least privilege to secret access. | Secret references, owner boundaries, generated RBAC, and no adoption of administrator/cert-manager Secrets. | TLS source tests; `IT-TLS-GAP-001`; `E2E-TLS-CERTMANAGER-001`; `E2E-TLS-CERTMANAGER-002` cert-manager Secret ownership assertions. | partially verified | Vault/External Secrets integration and cluster-level Secret access review remain deployment responsibilities. |
| `v5.0.0-V15.2.4` | Source third-party components from expected repositories and reduce dependency-confusion risk. | `go.mod`/`go.sum`, pinned Kind/provider/cert-manager versions, production image references. | SBOM and Trivy workflow; pinned Kind workflow lane. | partially verified | Supply-chain provenance, signing, and organizational dependency policy require external controls. |
| `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1` | Maintain a logging inventory and include investigation metadata in entries. | `docs/security/auditing.md`; structured reconciliation logs include generation/resourceVersion and object identity. | Controller logging/status tests; `CI-CONTROLS-GAP-001`. | partially verified | Log storage access, synchronization, retention, and alerting require platform evidence. |
| `v5.0.0-V16.5.2` | Continue securely when external resource access fails. | Capability-gated optional APIs, fail-closed TLS/provider errors, and actionable status conditions. | `IT-TLS-GAP-002`; generic optional-API integration tests; provider failure tests. | partially verified | Broader dependency failure-injection and resilience testing remain follow-up work. |

## Explicit non-goals and deferred evidence

- **OVN/OpenShift:** live policy qualification is deferred; no release claim is
  made for that path by this matrix.
- **Dedicated operator Helm chart:** deferred to a follow-up; no chart ownership
  or chart conformance is claimed here. An upstream dependency chart may still
  be used independently for fixtures.
- **Formal compliance:** this matrix does not claim FedRAMP authorization,
  NIST control implementation approval, or OWASP ASVS compliance/conformance.
- **Operator audit model:** the operator emits structured log-based audit
  traces. Persistent PostgreSQL audit events, hash chains, retention controls,
  and formal control attestations belong to platform services or organizational
  processes, not this operator.

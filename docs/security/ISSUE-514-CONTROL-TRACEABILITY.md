# Issue #514 — Ownership security control traceability

**Matrix ID:** KO-SEC-TR-514-001

**Version:** 1.0

**Date:** 2026-10-10

**Scope:** authorization of operator runtime resource mutations and cleanup

**Status:** final host gates and final-source Kind qualification verified; broader live lanes remain unqualified

**Contract:** `docs/design/ISSUE-514-OWNERSHIP-CONTRACT.md`

**Test plan:** `docs/tests/514/TEST_PLAN.md`

**Execution evidence:** `docs/tests/514/VALIDATION.md`. Final `make test`, build,
lint, compile-only, generated-manifest, security, Helm and pyramid checks passed.
Independent unit coverage is 100% for six ownership/shared-CRD entry points;
controller integration coverage is 81.8%. The final-source Helm-backed Kind
journey passed 8/8 specs with zero failures, pending or skips. OpenShift/OVN,
Cilium/Calico and upstream exact-SHA application-service lanes remain unqualified.

These are engineering control objectives adapted from **NIST SP 800-53 Rev. 5**
(FedRAMP), **SOC 2**, and **OWASP ASVS 5.0.0**. This matrix **does not claim formal**
FedRAMP authorization, SOC 2 attestation, or ASVS conformance. Test labels are
traceability identifiers, not proof of the complete organizational control.

## Control matrix

| Objective | Implementation and business outcome | Independent evidence | Status / residual limits |
|---|---|---|---|
| NIST/FedRAMP `AC-3`, `AC-6`, `SI-10`; SOC 2 `CC6.1`, `CC6.6`; ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | `internal/resources/ownership.go` authorizes at the trusted controller layer before hashes, updates, status consumption or deletion. A name, hash or partial label is not write permission. | `UT-OWN-514-001`, `UT-OWN-514-002`; actual Reconcile `IT-OWN-514-001`, `IT-OWN-514-002`, `IT-OWN-514-005`, `IT-OWN-514-006`, `IT-OWN-514-014`; API race `IT-OWN-514-009`; `E2E-OWN-514-001` | Local scope verified; live execution pending. Kubernetes RBAC/admission must restrict ownership-marker writes. This is not protection against a cluster administrator. |
| NIST/FedRAMP `CM-3`, `CM-6`, `CM-8`; SOC 2 `CC8.1` | Namespace/UID provenance supports deterministic creation and bounded same-identity reinstall without a new adoption API. Shared operand CRDs require explicit transfer instead of Helm takeover. | `UT-OWN-514-002`, `UT-OWN-514-003`, `UT-OWN-514-004`; `IT-OWN-514-003`, `IT-OWN-514-004`, `IT-OWN-514-013`, `IT-OWN-514-016`; `E2E-OWN-514-001` | Local scope verified; live execution pending. Change approval and schema compatibility review remain administrator responsibilities. Fully marked legacy objects without namespace provenance are a documented compatibility exception. |
| NIST/FedRAMP `AC-6`, `CM-3`; SOC 2 `CC6.6` | `internal/controller/ownership.go` uses UID **and resourceVersion** deletion preconditions for authorized objects. The operator never deletes workflow or CR namespaces, so provisioning-owned content cannot be indirectly removed. Provider-policy gates remain separate. | `IT-OWN-514-005`, `IT-OWN-514-006`, `IT-OWN-514-010`, `IT-OWN-514-011`, `IT-OWN-514-015`; namespace retention/reuse regression; existing provider lifecycle tests; `E2E-OWN-514-001` | Local scope verified; envtest uses real API preconditions but has no GC. DB/Valkey, administrator inputs and workflow artifacts remain provisioning-owned; administrator-reviewed namespace removal is outside operator uninstall. |
| NIST/FedRAMP `AC-6`, `IA-5`; ASVS `v5.0.0-V13.3.1`, `v5.0.0-V13.3.2` | Generated TLS material must be authorized before reusing keys. Webhook trust/metadata preservation is authorized before merging, not just before writing. Administrator credential/TLS inputs and cert-manager output Secrets remain read-only. | `IT-OWN-514-001`, `IT-OWN-514-007`, `IT-OWN-514-008`, `IT-OWN-514-018`; existing TLS source/rotation tests and Kind TLS lanes | Partially verified. This proves the ownership/least-privilege slice of secret lifecycle; HSM-backed storage, cluster encryption, Secret RBAC and complete secrets-management controls are not established by this change. |
| NIST/FedRAMP `CM-6`, `AC-6`; ASVS `v5.0.0-V16.5.2` | Administrator workflow namespaces must already have restricted PSA labels and no operator identity claim/owner/termination; a conflict preserves the input rather than changing it. PSA never overrides conflicting provenance. Kubernetes API failures remain errors, not adoption permission. | `IT-OWN-514-012`, `IT-OWN-514-016`, `IT-OWN-514-017`; API-denial and raced-read cases in `UT-OWN-514-004`; create-race `IT-OWN-514-009` | Local scope verified. Prepared administrator namespaces are reused without metadata writes and retained on uninstall. Error-path safety is operator-specific, not a complete dependency-resilience assessment. |
| NIST/FedRAMP `AU-2`, `AU-3`, `AU-12`, `SI-4`; SOC 2 `CC7.2`; ASVS `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1` | `internal/controller/ownership.go` records kind/name/namespace, owner/marker metadata, CR generation/resourceVersion and object version; Warning events and existing false readiness expose conflicts. Secret data/keys are not logged. | Log/event/status observations in `IT-OWN-514-001`, `IT-OWN-514-002`, `IT-OWN-514-013`; `E2E-OWN-514-001`; `docs/resource-ownership.md` | Partially verified. The operator produces structured logs/events, not a database audit chain. Collection, retention, time synchronization, access control and incident response remain platform/organizational responsibilities. |

## Evidence gates and interpretation

The native-policy subset additionally preserves all five existing provider
markers and rejects foreign/invalid-scope owners and terminating objects.
`internal/policy/ownership.go` is wired to updates, stale pruning and finalization;
independent `UT-OWN-514-005` and real-Reconcile `IT-OWN-514-019` prove preservation
and bounded same-identity owner repair for `AC-3`, `AC-6`, SOC 2 `CC6.6`/`CC8.1`,
and ASVS `v5.0.0-V8.3.1`. This is authorization evidence, not provider dataplane
qualification or an alternative to the mandatory five-marker policy identity.

- `hack/verify-business-unit-coverage.sh` requires 100% unit statement coverage
  for all new ownership functions and `ensureSharedCRD`; aggregate existing
  coverage debt is not hidden by merged-tier coverage.
- `hack/verify-test-pyramid.sh` checks production callers, actual envtest
  Reconcile entry points and the installed-operator Kind journey artifact.
- `hack/validate-security-traceability.sh` checks versioned references and all
  executable IDs. This static check does **not** count as running the live lane.
- `test/e2e/kind/scenarios_test.go` installs the real operator via Helm, drives
  public CR APIs, observes failures/recovery/upgrade/reinstall and checks that
  administrator object UID, resourceVersion, metadata and content are unchanged.
  The application containers are contract fixtures, not upstream qualification.

`verified` means a passing artifact proves the stated operator boundary;
`partially verified` means broader live/organizational evidence remains;
`not verified` applies to unrun live lanes. See the test plan for actual command
results. No v1.6 release or upstream exact-SHA qualification is claimed here.

ASVS references were checked against the
[official 5.0.0 requirement catalog](https://github.com/OWASP/ASVS/blob/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv).
Authorization rows concern explicit permissions and trusted-layer enforcement;
logging rows concern documented logging and investigation metadata. They do not
redefine those requirements as Kubernetes-specific certifications.

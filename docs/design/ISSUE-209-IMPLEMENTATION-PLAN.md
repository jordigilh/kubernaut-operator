# Issue #209 — Mandatory DataStorage Audit HMAC Wiring

**Status:** Implemented; final audit residuals documented  
**Date:** 2026-10-14  
**Issue:** [kubernaut-operator#209](https://github.com/jordigilh/kubernaut-operator/issues/209)  
**Upstream follow-up:** [kubernaut#2509](https://github.com/jordigilh/kubernaut/issues/2509)  
**Business requirement:** [BR-AUDIT-010](https://github.com/jordigilh/kubernaut/blob/main/docs/requirements/BR-AUDIT-010-keyed-audit-hash-chain.md)

## 1. Decision summary

DataStorage audit HMAC is mandatory for deployments managed by the operator. The
operator will provision a stable HMAC Secret when no administrator-managed Secret
is configured, and will use an explicitly referenced Secret when one is provided.

There is no `enabled` opt-out. Existing `Kubernaut` objects remain valid without
spec changes: the effective HMAC configuration is defaulted by the operator.

The upstream DataStorage runtime currently retains a legacy unkeyed fallback. That
does not block this operator implementation because the pinned `kubernaut
v1.6.0-rc22` runtime already supports the HMAC configuration and migration 013.
Upstream issue #2509 requests that the runtime later fail closed for all deployment
paths.

## 2. Preflight evidence

The preflight and spikes confirmed:

| Area | Evidence | Finding |
|---|---|---|
| Upstream API | `go.mod` pins `github.com/jordigilh/kubernaut v1.6.0-rc22`; upstream `pkg/datastorage/config` contains `AuditConfig`, `loadAuditHashKeySecret`, and algorithm-aware verification | The current operand accepts the required configuration. |
| Database compatibility | Upstream `migrations/013_audit_hash_algorithm.sql` exists in v1.6.0-rc22 | Existing unkeyed rows remain verifiable; new rows can use HMAC. |
| ConfigMap path | `internal/resources/configmaps.go:507`, `DataStorageConfigMap()` at line 1328 | Add the upstream `audit` block to the existing typed YAML boundary. |
| Deployment path | `internal/resources/deployments.go:156`, `dataStorageVolumesAndMounts()` | Extend the existing projected `secrets` volume; the shared mount is already read-only. |
| Reconciliation path | `phaseValidate()` calls `validateDatabaseInputs()` before migration and deployment; DataStorage builders are reached from `buildCoreConfigMaps()` and `enabledDeploymentBuilders()` | No new reconciliation phase is required. |
| Secret RBAC | `config/rbac/role.yaml` already grants namespaced Secret create/get/list/patch/update/watch/delete | No new permission is required; least-privilege review remains mandatory. |
| Ownership | `ensureNamespaced()` adds an owner reference and generic cleanup uses ownership checks | The HMAC Secret cannot use ordinary owner-reference/GC behavior because deleting it can make historical HMAC records unverifiable. |
| Watch behavior | `SetupWithManager()` owns normal Secrets and maps administrator Secrets separately | A dedicated map for the persistent generated Secret is required because it intentionally has no CR owner reference. |
| Baseline | `go build ./...` and `make test-unit` pass; unit business-statement verification reports 100% | The implementation can begin from a clean baseline. |

## 3. Spikes and outcomes

### Spike A — Upstream contract

**Question:** Can the operator always render HMAC configuration before the
upstream runtime makes HMAC mandatory?

**Result:** Yes. The current runtime loads `audit.hashKeySecretsFile` and
`audit.hashKeyKey`, and migration 013 records `sha256-unkeyed` versus
`hmac-sha256` per row. The operator can enforce the stronger configuration now.

### Spike B — Secret lifecycle

**Question:** Can a missing generated Secret simply be regenerated?

**Result:** No. Regeneration after HMAC writes exist would create a new key and
make historical records unverifiable. The operator must create the Secret only
on first provisioning, preserve it across CR deletion/reinstallation, and fail
closed if it later disappears.

The plan therefore adds a non-secret state marker ConfigMap recording the
effective Secret name, key, and provisioning source. It contains no key material.

### Spike C — Control interpretation

SOC 2 and OWASP ASVS do not prescribe HMAC as the only implementation. However,
an optional fallback is insufficient for this project's stated GAP-05 objective:
an installation that omits the key remains vulnerable to database-privileged
recomputation of the unkeyed chain. Mandatory operator wiring provides a uniform
control for the operator's only supported deployment vector.

## 4. Effective API contract

Add an optional nested v1alpha2 field whose omitted value still resolves to the
mandatory operator default:

```yaml
spec:
  dataStorage:
    auditHashKey:
      # Optional. Empty means operator-generated Secret.
      existingSecret: ""
      # Optional. Defaults to hmacKey; this is the key inside the mounted YAML.
      secretKey: "hmacKey"
```

Proposed Go shape:

```go
type DataStorageAuditHashKeySpec struct {
    ExistingSecret string `json:"existingSecret,omitempty"`
    SecretKey      string `json:"secretKey,omitempty"`
}
```

`DataStorageSpec` will carry a pointer to this block so old CR manifests remain
valid. A nil block means the same effective configuration as an empty block;
there is no legacy unkeyed mode in the operator.

The Kubernetes Secret data key remains the upstream-compatible
`audit-hmac-key.yaml`. Its content contains the configured inner key, normally:

```yaml
hmacKey: <base64-encoded 256-bit random value>
```

The API field name follows the upstream `existingSecret`/`secretKey` contract;
the obsolete `enabled` field from the original issue is deliberately not added.

## 5. Secret lifecycle and security contract

### 5.1 Operator-generated default

- Default Secret name: `datastorage-audit-hmac-key`.
- Generate 32 bytes from `crypto/rand`, encoded for the mounted YAML file.
- Set `immutable: true` on the Secret.
- Set a deletion-protection finalizer; explicit administrative retirement is
  required before the Secret or state marker can be removed.
- Create only when the state marker does not exist.
- Never rotate, update, or delete automatically.
- Do not set a CR owner reference; preserve across CR deletion and reinstall.
- Add explicit operator/audit-HMAC identity labels and annotations.
- Do not log, status-publish, annotate, or place the key bytes in a ConfigMap.

If the generated Secret is later missing while the state marker exists, the
operator reports an actionable failure and does not generate a replacement.
Restoration from backup or an explicit key-lifecycle procedure is required.

### 5.2 Administrator-managed Secret

When `existingSecret` is set:

- The operator reads and validates the referenced Secret only.
- It never adds ownership metadata, changes data, rotates, or deletes it.
- The Secret must contain `audit-hmac-key.yaml` with a non-empty, valid YAML/JSON
  object and a configured HMAC key of at least 32 bytes.
- Missing, malformed, empty, or weak key material blocks reconciliation.

### 5.3 Key identity immutability

After initial readiness, changing the effective Secret name or inner key is
rejected. HMAC key rotation is outside this issue because safe rotation requires
an application-level dual-key verification/migration protocol.

The persistent state marker records only non-secret metadata:

```yaml
secretName: datastorage-audit-hmac-key
secretKey: hmacKey
source: operator-generated
```

It is retained across CR deletion/reinstallation and is never used to store or
recover key material.

## 6. Production wiring manifest

| Component/behavior | Production entry point | Wiring location | Required IT evidence |
|---|---|---|---|
| Effective key reference resolution | `DataStorageAuditHashKeySpec` resolver | `internal/resources/` helper used by controller and builders | `IT-AUDIT-209-INT-001/002` |
| First-provision Secret creation | validation before migration/deployment | `KubernautReconciler.phaseValidate()` / dedicated helper | `IT-AUDIT-209-INT-001/007` |
| Existing Secret validation | administrator-owned Secret preflight | `validateDatabaseInputs()` or adjacent validation helper | `IT-AUDIT-209-INT-002/003` |
| Persistent lifecycle marker | generated/BYO source identity | controller validation and cleanup-preservation path | `IT-AUDIT-209-INT-001/004` |
| DataStorage ConfigMap | `DataStorageConfigMap()` | `internal/resources/configmaps.go`, `buildCoreConfigMaps()` | `IT-AUDIT-209-INT-007` |
| DataStorage projected Secret | `DataStorageDeployment()` | `internal/resources/deployments.go`, `enabledDeploymentBuilders()` | `IT-AUDIT-209-INT-007` |
| Secret deletion/change watch | reconcile request mapping | `SetupWithManager()` and audit Secret mapper | `IT-AUDIT-209-INT-006` |
| Failure observability | status, Warning event, structured log | `ConditionBYOValidated`, generation/resourceVersion context | `IT-AUDIT-209-INT-003/004/005` |

**Checkpoint W requirement:** every row must have a production caller and an
envtest reconciliation test. No helper may remain resource-only or test-only.

## 7. TDD implementation sequence

### DISCOVERY

- Reconfirm the final `DataStorageSpec` definition before adding fields.
- Reconfirm upstream YAML names and Secret file shape from the pinned operand.
- Reconfirm all Secret ownership/cleanup paths and generated-manifest locations.
- Add the design/control mapping before implementation changes.

### RED — business behavior first

Add failing Ginkgo/Gomega tests for the scenarios in Section 8. Tests must use
the production YAML structs for full ConfigMap shape assertions; no hand-copied
mirror structs.

### GREEN — minimum wiring

- Add the CRD type and defaults.
- Add the key resolver and create-only Secret/state-marker path.
- Wire validation before migration/deployment.
- Always render the audit ConfigMap block and projected Secret.
- Add the generated Secret watch mapper.
- Ensure the invalid/missing-key path sets status and prevents dependent work.

### CHECKPOINT W

- [x] Generated key path is called from `phaseValidate()`.
- [x] ConfigMap and Deployment builders are reached from normal reconciliation.
- [x] Existing Secret path is read-only and never adopted.
- [x] Generated Secret is never rotated or deleted by generic cleanup.
- [x] Secret deletion/change events enqueue the singleton CR.
- [x] Every production path has an envtest test.

### REFACTOR

- Remove duplicated Secret parsing and effective-name logic.
- Keep key bytes out of all diagnostics and test failure messages.
- Apply the Go anti-pattern checklist and simplify nested validation paths.
- Update security/credential and ownership documentation.
- Run code generation, manifest regeneration, build, lint, and the full test suite.

## 8. Business-level test scenarios

| ID | Tier | Business scenario / acceptance criterion | Primary artifacts | Control objectives |
|---|---|---|---|---|
| `UT-AUDIT-209-CM-001`; `UT-AUDIT-209-CM-002` | Unit | Omitted CR block resolves to mandatory audit configuration; configured key name is rendered without key material | `configmaps_test.go` | AU-9; CC8.1; V11.1.1 |
| `UT-AUDIT-209-DEP-001`; `UT-AUDIT-209-DEP-002` | Unit | Default and BYO Secrets are projected at the upstream path and mounted read-only | `deployments_test.go` | AC-6; IA-5; V13.3.1/V13.3.2 |
| `UT-AUDIT-209-RES-001`–`011` | Unit | Generated key randomness/immutability, BYO rejection, malformed/weak/nil input, fingerprinting, and JSON/YAML validation are enforced | `audit_hmac_test.go` | AU-9; AC-6; IA-5; SI-10; V11.1.1; V13.3.1; V16.5.2 |
| `UT-AUDIT-209-CTRL-001`–`021` | Unit | Provisioning, crash-safe marker recovery, key stability, BYO preservation/correction, fail-closed errors, identity/fingerprint immutability, and event mapping are exercised | `audit_hash_key_unit_test.go` | AU-9; AC-6; SI-4; SI-10; CC6.6/CC7.2/CC8.1; V13.3.1/V13.3.2/V16.1.1/V16.2.1/V16.5.2 |
| `IT-AUDIT-209-INT-001`–`009` | Integration | Real reconciliation provisions generated/BYO keys, completes deployment wiring, blocks invalid/foreign/changed Secrets, fails closed after key loss, and maps Secret/state events | `audit_hash_key_integration_test.go` | AU-9; AC-6; SI-4; SI-10; CC6.6/CC7.2/CC8.1; V13.3.1/V13.3.2/V16.1.1/V16.2.1/V16.5.2 |
| `CONTRACT-AUDIT-209-001` | Contract | Operator contract package observes mandatory audit ConfigMap, Secret projection, finalizers, and key format; live application qualification remains upstream | `test/e2e/kind/contract` | AU-9; AC-6; SI-4; CC6.6/CC7.2; v5.0.0-V13.3.1/v5.0.0-V16.5.2 |
| `UP-AUDIT-2509-*` | Upstream | DataStorage itself rejects absent HMAC configuration while verifying historical unkeyed rows | upstream issue #2509 | AU-9; CC8.1; V11.1.1/V16.5.2 |

Coverage target is at least 90% for affected operator/resource code, with tests
selected for the above business cases rather than added solely to increase a
line or branch percentage.

## 9. Control traceability

| Control objective | Operator behavior | Evidence |
|---|---|---|
| FedRAMP/NIST `AU-9` | New audit writes always receive a keyed HMAC configuration; key is outside PostgreSQL and cannot be silently regenerated | `UT-AUDIT-209-CM-001`, `UT-AUDIT-209-RES-001`, `UT-AUDIT-209-CTRL-001/002`, `IT-AUDIT-209-INT-001/004`, generated Secret lifecycle, upstream migration 013 |
| FedRAMP/NIST `AC-6`, `IA-5` | BYO Secret is read-only; generated key access is limited to the existing namespaced operator/workload path | `UT-AUDIT-209-DEP-001/002`, `UT-AUDIT-209-CTRL-003`, `IT-AUDIT-209-INT-002/005`, RBAC review, Kind manifest assertions |
| FedRAMP/NIST `SI-4`, `SI-10` | Invalid or missing material blocks readiness and produces status/event/log diagnostics without secret bytes | `UT-AUDIT-209-RES-004`, `UT-AUDIT-209-CTRL-004/007/009/010`, `IT-AUDIT-209-INT-003/006` |
| SOC 2 `CC6.6` | Secret ownership boundaries prevent adoption or mutation of administrator material | `UT-AUDIT-209-CTRL-003/007/008`, `IT-AUDIT-209-INT-002/005` |
| SOC 2 `CC7.2` | Secret loss, invalidity, and ownership conflicts are observable and actionable | `UT-AUDIT-209-CTRL-004/007/010`, `IT-AUDIT-209-INT-001/003/004/005/006` |
| SOC 2 `CC8.1` | Effective key identity is stable; historical records are not invalidated by reconciliation or reinstall | `UT-AUDIT-209-CTRL-002/005`, `IT-AUDIT-209-INT-001/004` |
| OWASP ASVS `v5.0.0-V11.1.1` | Key is generated with CSPRNG, protected as Secret data, immutable, and never logged or rotated implicitly | `UT-AUDIT-209-RES-001/003`, `UT-AUDIT-209-CTRL-001/002`, documentation |
| OWASP ASVS `v5.0.0-V13.3.1`, `v5.0.0-V13.3.2` | Secret references are validated, mounted read-only, and administrator-owned material is never adopted | `UT-AUDIT-209-DEP-001/002`, `UT-AUDIT-209-CTRL-003/008`, `IT-AUDIT-209-INT-002/005` |
| OWASP ASVS `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1` | Security-relevant failures include resource identity, generation/resourceVersion, and outcome without sensitive data | `UT-AUDIT-209-CTRL-006/010`, `IT-AUDIT-209-INT-006`, structured-log review |
| OWASP ASVS `v5.0.0-V16.5.2` | Required external key material failure stops the dependent workload instead of falling back to unkeyed hashing | `UT-AUDIT-209-RES-004`, `UT-AUDIT-209-CTRL-004/008/009`, `IT-AUDIT-209-INT-003/004` |

These mappings are engineering evidence, not a claim of formal FedRAMP, SOC 2,
or OWASP ASVS certification.

## 10. Generated artifacts and completion gates

The implementation must update and verify:

- `api/v1alpha2/zz_generated.deepcopy.go`.
- `config/crd/bases/kubernaut.ai_kubernauts.yaml`.
- `charts/kubernaut-operator/files/kubernaut.ai_kubernauts.yaml`.
- Bundle and operator-installation CRD artifacts.
- Samples/fixtures and CRD field coverage documentation.
- `docs/security/credentials-and-tls.md` and ownership documentation.
- Issue #209 and upstream issue #2509 cross-references.

Required final commands:

```bash
go build ./...
golangci-lint run
make test
make manifests generate
git diff --exit-code config/ bundle/ dist/
make test-pyramid
```

## 11. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Generated Secret is deleted | Persistent state marker distinguishes first provisioning; later loss fails closed instead of regenerating. |
| HMAC key is changed | Secret is immutable; effective name/key identity is recorded and changes are rejected. |
| Existing Secret is accidentally adopted | BYO path is read-only and ownership-checked; foreign default-named Secrets fail closed. |
| Upstream still permits legacy fallback | Operator always renders HMAC configuration; upstream issue #2509 tracks runtime enforcement. |
| Rollback to an older operator | Document that rollback before this change can re-enable unkeyed writes; release qualification must include the control boundary. |
| Key rotation requirement emerges later | Track as a separate design; it requires dual-key verification and a database/application migration protocol. |

**Implementation confidence: 92%**  
The current operand, migration, controller phase ordering, RBAC, and resource
builders are confirmed. The main implementation risk is the deliberately strict
persistent Secret lifecycle, which is covered by explicit state-marker and
fail-closed tests rather than implicit Kubernetes ownership behavior.

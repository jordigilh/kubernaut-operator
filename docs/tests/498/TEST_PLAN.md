# Issue #498 TLS E2E Lane Completion Plan

**Issue:** [kubernaut-operator#498](https://github.com/jordigilh/kubernaut-operator/issues/498)

**Parent work:** [#491 — upstream Helm TLS parity](https://github.com/jordigilh/kubernaut-operator/issues/491)

**Status:** Implementation plan for the dedicated `feat/498-tls-e2e-lanes`
worktree. Repository changes are in progress, but the work is not complete
until every source has unit, envtest, and Helm-backed Kind evidence and
the hosted hook/manual-admin jobs are green.

**Methodology:** RED → GREEN → REFACTOR → CHECK, with the operator Helm-chart
E2E journey required for every supported generic TLS source.

## 1. Objective

Close the remaining TLS source qualification gap by giving every generic,
Kind-capable source a dedicated E2E lane:

1. development self-signed;
2. Helm-compatible `hook`;
3. `manual` / administrator-managed material; and
4. cert-manager provisioning/reference behavior.

OpenShift service-CA/router-CA qualification is explicitly deferred. It needs an
OCP-capable execution environment and is not a GitHub-hosted-runner acceptance
criterion for this issue.

## 2. Confirmed baseline

The TLS parity implementation already has the source API and controller/resource
seams on the `feat/491-tls-parity` line:

- `api/v1alpha2/kubernaut_types.go` defines `hook`, `cert-manager`, `manual`,
  `AdministratorManaged`, `CertManager`, and `DevelopmentSelfSigned` modes.
- `internal/controller/tls_source_integration_test.go` covers manual ownership
  preservation and cert-manager resource/readiness behavior through envtest.
- `.github/workflows/test.yml` currently has a generic/development Kind lane and
  a cert-manager Kind lane, but no hook or manual/administrator-managed lane.
- `test/e2e/kind/tlsSourceFromEnvironment` currently accepts only development and
  cert-manager selectors.
- `test/e2e/kind/journey.go` directly constructs only development self-signed or
  cert-manager CR fixtures.

This issue is therefore an E2E source-selection, fixture, assertion, and CI
wiring gap, not a request to introduce a new certificate ownership model.

`v1alpha2` has not been released. The plan does not preserve an enum or field
solely for backward compatibility with unreleased objects. If implementation
discovery shows that the API should be normalized, the RED contract tests may
change `api/v1alpha2/`, all in-repository references may be updated, and the
generated CRD/deepcopy artifacts must be regenerated. The runtime behaviors
below remain explicit requirements: `manual` uses the stable chart-compatible
material contract, `AdministratorManaged` uses explicitly named administrator
Secrets, `CertManager` is reference-only, lower-case `cert-manager` provisions
through cert-manager, and `DevelopmentSelfSigned` is the explicit development
provisioner.

## 3. Required lane matrix

| Lane | Selector | CR mode(s) | Ownership proof |
|---|---|---|---|
| Generic development | `development` | `DevelopmentSelfSigned` | Operator owns generated CA/leaves and removes them during CR cleanup |
| Hook | `hook` | `hook` | Operator-owned hook-equivalent material, trust publication, rotation, and cleanup |
| Manual/admin | `manual-admin` | `manual`, `AdministratorManaged` | Pre-created material remains administrator-owned; data and ownership metadata are unchanged (excluding server-managed `resourceVersion`) |
| Cert-manager | `certmanager` | `cert-manager` and reference-only `CertManager` scenarios | cert-manager owns output Secrets; operator owns only its Issuer/Certificate objects |

The manual/admin lane may use one isolated Kind cluster, but it must execute
both behaviorally distinct mode selections. Neither selection is retained
merely as a compatibility alias, and the lane must not silently test only
`manual` while claiming administrator-managed coverage.

No OpenShift lane is added by this issue. The release evidence must state that
service-CA/router-CA remains unqualified until an OCP-capable runner is
available.

## 4. Behavioral contracts

### 4.1 Hook lane

- Apply a real CR with `spec.tls.mode=hook`; do not substitute
  `DevelopmentSelfSigned` in the fixture.
- Prove the operator creates usable CA, leaf, trust-bundle, webhook, and
  signing material before reporting TLS readiness.
- Prove every generated object has the expected Kubernaut ownership boundary.
- Trigger a controlled leaf/CA rotation and verify an overlap window or
  coordinated restart, continuous successful probes, and preservation of the
  last working material on failure. Do not claim temporal trust-before-leaf
  ordering from final-state snapshots; the current reconciliation path must be
  instrumented or deliberately reordered before that stronger assertion is
  added.
- Delete the CR and verify operator-owned TLS objects are cleaned up.

### 4.2 Manual/administrator-managed lane

- Create valid administrator-owned CA, serving, AuthWebhook, and DataStorage
  signing material before creating the CR.
- Run both `manual` and `AdministratorManaged` source selections as separate
  scenarios because their input/reference contracts differ.
- Prove the operator references the configured objects without adding owner
  references, changing data, replacing names, or deleting them.
- Prove webhook `caBundle` values are preserved or populated through the
  administrator-managed path and that OpenShift CA-injection annotations are
  not required.
- Remove or invalidate required material in a negative scenario and prove the
  operator remains fail-closed: no plaintext workload, empty fail-closed
  webhook bundle, or deletion of the last working trust root.
- Delete the CR and verify administrator-owned objects survive cleanup.

### 4.3 Cert-manager lane

Retain the existing pinned cert-manager lane. It must continue to prove:

- cert-manager API/controller prerequisites are installed by the lane, not the
  operator;
- generated Secrets remain cert-manager-owned;
- the operator creates or references only the intended Issuer/Certificate
  resources;
- all required Certificates reach Ready before TLS readiness is reported;
- reissuance preserves trust and ownership; and
- cleanup does not delete cert-manager-owned output Secrets.

## 5. Pyramid invariant and control-objective evidence

### 5.1 Pyramid invariant

TLS lane completion follows the project invariant:

> **Unit tests prove logic. Integration tests prove production wiring. E2E
> tests prove the user journey. A TLS source is not implemented when only its
> unit tests pass.**

Every source mode must satisfy all three tiers:

- **Unit (RED/GREEN):** pure source resolution, certificate/CA/SAN validation,
  ownership classification, object shape, source-specific webhook metadata,
  rotation ordering, and fail-closed error behavior. These tests must assert
  business rules and invalid inputs, not only field presence.
- **Integration (envtest):** the real reconciler creates or references the
  source artifacts, persists status conditions/events, wires trust into the
  workloads and webhooks, preserves ownership boundaries, and refuses to claim
  readiness when material is invalid or unavailable. Direct builder tests do
  not satisfy this tier.
- **E2E (Kind):** the operator Helm chart installs the operator, a user applies a
  real `Kubernaut` CR, and workloads complete a real TLS trust/identity journey.
  The lane must observe readiness, usable workload trust, webhook behavior,
  source-specific ownership, rotation/failure behavior, and cleanup. E2E tests
  must not call resource builders or controller methods directly.

The `make test-pyramid`/`hack/verify-test-pyramid.sh` gate must fail when a
production source has no controller caller, no integration proof, or no
source-specific E2E journey. A passing unit suite alone is explicitly not a
completion signal.

### 5.2 Business-level control traceability

The tests extend the business assertions defined for #491 rather than claiming
formal compliance. Each assertion must have evidence at the appropriate test
tier, and the evidence matrix must use the exact versioned OWASP ASVS 5.0.0
identifiers. Passing repository tests demonstrate implementation evidence only;
they do not constitute a FedRAMP authorization, SOC 2 audit opinion, or
independent ASVS assessment.

| Business assertion | Business behavior that must be verified | Unit evidence | Integration evidence | E2E evidence required by #498 | FedRAMP/NIST | SOC 2 | OWASP ASVS 5.0.0 |
|---|---|---|---|---|---|---|---|
| `BA-491-TLS-01` Source selection is explicit and safe | Each supported mode selects its intended ownership path; unsupported or unavailable material never produces plaintext or an empty fail-closed webhook bundle. | `UT-TLS-491-001`, `UT-TLS-491-002` | `IT-TLS-GAP-002` | `E2E-TLS-DEV-001`, `E2E-TLS-HOOK-001`, `E2E-TLS-MANUAL-001`, `E2E-TLS-ADMIN-001`, existing `E2E-TLS-CERTMANAGER-001` | AC-6, SC-8, CM-6, SI-10 | CC6, CC8 | V12.2.1, V13.3.1, V16.5.2 |
| `BA-491-TLS-02` Administrator material is read-only | Manual/admin CA, serving, signing, and webhook material is consumed without adoption, overwrite, owner references, or deletion; the actual workload still verifies TLS with that material. | `UT-TLS-GAP-001` | `IT-TLS-MANUAL-001`, `IT-TLS-MANUAL-002`, `IT-TLS-WEBHOOK-001` | `E2E-TLS-MANUAL-001`, `E2E-TLS-ADMIN-001`, `E2E-TLS-CLEANUP-001` | AC-6, IA-5, SC-8, SC-17 | CC6, CC7 | V12.1.3, V13.3.1, V13.3.2 |
| `BA-491-TLS-03` Cert-manager owns generated Secrets | The operator owns only intended Issuer/Certificate objects, waits for readiness, and never adopts or deletes cert-manager output Secrets. | `UT-TLS-491-003`, `UT-TLS-491-004`, `UT-TLS-491-005` | `IT-TLS-PARITY-001`, `IT-TLS-CERTMANAGER-READY-001`, `IT-TLS-GAP-001` | Existing `E2E-TLS-CERTMANAGER-001`, `E2E-TLS-CERTMANAGER-002` | AC-6, CM-6, SC-12, SC-13, SI-4 | CC6, CC7, CC8 | V11.1.1, V12.1.1, V13.3.1, V13.3.2, V16.5.2 |
| `BA-491-TLS-04` Cryptography and identity are correct | Serving certificates use approved algorithms/usages and service DNS SANs; clients can verify the intended service identity rather than merely observe a Secret. | `UT-TLS-491-002`, `UT-TLS-491-004` | `IT-TLS-MANUAL-002` | `E2E-TLS-DEV-001`, `E2E-TLS-HOOK-001`, `E2E-TLS-MANUAL-001`, `E2E-TLS-CERTMANAGER-001` | IA-5, SC-8, SC-12, SC-13, SC-17 | CC6, CC7 | V11.1.1, V11.1.2, V12.1.1, V12.1.2, V12.1.3, V13.2.1 |
| `BA-491-TLS-05` Trust reaches every consumer | Mounted CA paths, trust ConfigMaps, webhook bundles, migration, and enabled workloads all consume the selected source; a real probe succeeds through the expected TLS path. | `UT-TLS-491-007`, `UT-TLS-491-008`, `UT-TLS-491-009` | `IT-TLS-GAP-003`, `IT-TLS-WEBHOOK-001` | `E2E-TLS-DEV-001`, `E2E-TLS-HOOK-001`, `E2E-TLS-MANUAL-001`, `E2E-TLS-ADMIN-001`, `E2E-TLS-FAIL-CLOSED-001` | SC-8, SC-17, SI-4 | CC6, CC7, A1 | V12.1.3, V13.2.1, V16.5.2 |
| `BA-491-TLS-06` Webhook trust fails closed | Webhook CA bundles are source-correct and non-empty before readiness; missing or invalid trust prevents a false Ready/Running result and preserves fail-closed behavior. | `UT-TLS-491-010`, `UT-TLS-491-011` | `IT-TLS-WEBHOOK-001`, `IT-TLS-CERTMANAGER-READY-001` | `E2E-TLS-FAIL-CLOSED-001` plus each source lane's readiness assertion | SC-8, SC-13, SC-17, SI-4, SI-10 | CC6, CC7 | V12.1.3, V12.2.1, V13.2.1, V16.5.2 |
| `BA-491-TLS-07` Rotation preserves service availability | Trust remains available through leaf change; overlap or coordinated restart keeps the service usable; failed rotation preserves the last working root and reports an actionable condition/event. | `UT-TLS-ROTATION-GAP-001`, `UT-TLS-ROTATION-GAP-002` | `IT-TLS-ROTATION-GAP-001` | `E2E-TLS-HOOK-002`, existing `E2E-TLS-CERTMANAGER-002` | SC-8, SC-12, SC-13, SI-4 | CC7, A1 | V11.1.1, V11.1.2, V12.1.1, V16.5.2 |
| `BA-498-TLS-01` Reconciliation decisions are observable | Status/events/logs identify the selected source, observed generation, resourceVersion, failure reason, and ownership outcome so operators can reconstruct the decision. | `UT-TLS-AUDIT-001` | `IT-TLS-AUDIT-001` | Each lane asserts source-specific status and failure evidence; `E2E-TLS-FAIL-CLOSED-001` | AU-2, AU-3, AU-12, SI-4 | CC7, CC8 | V16.2.1, V16.3.3, V16.3.4, V16.5.2, V16.5.3 |

### 5.3 Required business assertions by lane

| Lane | Minimum user/business outcome |
|---|---|
| Development | A development installation reaches TLS-ready state and a workload can verify another enabled service using the generated public CA. |
| Hook | The explicitly selected hook mode reaches TLS-ready state, owns its generated material, survives a controlled rotation, and cleans up only its own objects. |
| Manual/admin | An administrator-provisioned installation reaches TLS-ready state using unchanged external material; both explicit source contracts preserve that material through reconciliation and CR deletion. |
| Cert-manager | A cert-manager-provisioned installation reaches TLS-ready state only after Certificates are Ready, uses cert-manager-owned output, and preserves trust across reissuance. |

Negative cases must assert the business consequence—not only an error string:
the operator must not advertise readiness, start a plaintext fallback, publish an
empty fail-closed webhook trust bundle, or remove the last working trust root.

### 5.4 Pyramid scenario matrix

Each row is a completion unit. A row is incomplete until its UT, IT, and E2E
evidence exists and the source-specific E2E job actually executes it. Existing
#491 tests may satisfy a cell only when their assertions cover the same source
contract; a test name or shared helper alone is not evidence.

| Scenario ID | Unit evidence (pure logic) | Integration evidence (envtest/reconciler) | E2E evidence (operator Helm chart + Kind) | Required assertions |
|---|---|---|---|---|
| `PYR-TLS-DEV-001` Development source | `UT-TLS-DEV-001`: resolves `DevelopmentSelfSigned`, generated ownership, SANs, signing material, and invalid-input behavior | `IT-TLS-DEV-001`: reconciliation creates generated material, trust ConfigMaps, webhook bundles, and `TLSReady` only after validation | `E2E-TLS-DEV-001`: explicit `development` lane applies a real CR, reaches Running, performs a workload TLS probe, and removes owned objects | No plaintext fallback; generated objects are Kubernaut-owned; cleanup is source-specific |
| `PYR-TLS-HOOK-001` Hook readiness and ownership | `UT-TLS-491-002`, `UT-TLS-491-003`, plus hook ownership assertions | `IT-TLS-HOOK-001`: real reconciler path publishes hook trust/webhook/signing material and status | `E2E-TLS-HOOK-001`: `spec.tls.mode=hook`, real CA/SAN probe, non-empty webhook bundles, and owner-reference checks | The fixture must not select development mode |
| `PYR-TLS-HOOK-002` Hook rotation/failure | `UT-TLS-ROTATION-GAP-001`, `UT-TLS-ROTATION-GAP-002` | `IT-TLS-ROTATION-GAP-001`: overlap/preservation and actionable failure condition | `E2E-TLS-HOOK-002`: rotate a leaf, prove a new certificate and successful probe, then prove failed rotation preserves the last working root | No false Ready state and no loss of the last working trust root |
| `PYR-TLS-MANUAL-001` Helm-compatible manual source | `UT-TLS-GAP-001` plus stable-name/default and read-only validation tests | `IT-TLS-MANUAL-001`, `IT-TLS-MANUAL-002`, `IT-TLS-WEBHOOK-001` | `E2E-TLS-MANUAL-001`: pre-create ConfigMap/Secrets, apply a real manual CR, probe service identity, and snapshot inputs | No adoption, mutation, owner references, OpenShift CA-injection dependency, or cleanup deletion |
| `PYR-TLS-ADMIN-001` Explicit administrator-managed source | Administrator-managed CA/serving/signing/webhook validation tests | `IT-TLS-ADMIN-001`: explicit named references, readiness, invalid-material failure, and webhook preservation | `E2E-TLS-ADMIN-001`: apply an explicit `AdministratorManaged` CR and repeat the ownership, probe, failure, and cleanup assertions | This scenario is required independently of the manual scenario; it is not a compatibility-only alias test |
| `PYR-TLS-CM-001` Provisioning cert-manager source | `UT-TLS-491-001`, `UT-TLS-491-004`, `UT-TLS-491-005` | `IT-TLS-PARITY-001`, `IT-TLS-CERTMANAGER-READY-001` | Existing `E2E-TLS-CERTMANAGER-001`: pinned cert-manager installation, readiness, ownership, and trust probe | Operator owns only intended Issuer/Certificate resources; cert-manager owns output Secrets |
| `PYR-TLS-CM-002` Reference-only cert-manager source | `UT-TLS-GAP-001` and source-specific CA-key validation | `IT-TLS-GAP-001`, `IT-TLS-GAP-002` | Planned `E2E-TLS-CERTMANAGER-003` when the lane selects reference-only mode; existing `E2E-TLS-CERTMANAGER-002` remains the reissuance case | No operator creation/deletion of external output Secrets; missing issuer fails closed |
| `PYR-TLS-FAIL-001` Cross-source fail-closed behavior | `UT-TLS-GAP-002`, webhook CA validation tests | `IT-TLS-GAP-002`, `IT-TLS-CERTMANAGER-READY-001` | `E2E-TLS-FAIL-CLOSED-001` in the manual/admin lane, plus readiness assertions in every lane | Invalid material must not produce plaintext, empty webhook trust, or deletion of the last good root |
| `PYR-TLS-CLEANUP-001` Ownership-aware cleanup | Ownership classification and cleanup unit tests | Controller finalizer cleanup tests for generated versus external objects | `E2E-TLS-CLEANUP-001`: delete the CR and assert hook/development cleanup, manual/admin survival, and cert-manager output preservation before namespace teardown | Cleanup is proven through the real finalizer path |

The E2E tests may use a configured-source conditional because each CI job owns
one source selector, but no scenario may be silently skipped. The contract
suite, workflow assertions, and `hack/verify-test-pyramid.sh` must prove that
every conditional path has a corresponding job.

## 6. TDD execution plan

### Phase 0 — Discovery and contract checks

- [x] Confirm the issue scope: hook and manual/admin get dedicated lanes;
  OpenShift is deferred.
- [x] Validate the existing v1alpha2 TLS mode definitions before referencing
  them (CHECKPOINT A).
- [x] Search existing source-resolution, fixture, and E2E helpers before adding
  new ones (CHECKPOINT B).
- [x] Record the exact source-selector contract and cleanup behavior in RED
  tests.

### Phase 1 — RED

Add failing tests before production wiring:

- `tlsSourceFromEnvironment` accepts only the approved lane selectors and maps
  them deterministically; `manual` and `administrator-managed` select the
  manual/admin lane, while unsupported values fail instead of falling back.
- The CR fixture builder emits the selected mode rather than defaulting every
  non-cert-manager case to development self-signed.
- Each row in the pyramid matrix has a named Ginkgo test at every applicable
  tier; missing source-specific IT or E2E evidence fails the static pyramid
  gate.
- Hook E2E asserts operator-owned TLS artifacts, real trust/identity, rotation,
  failure preservation, and readiness.
- Manual and AdministratorManaged E2E each assert pre-created data and owner
  references are unchanged.
- Invalid manual/admin material prevents readiness and plaintext fallback.
- Cleanup distinguishes operator-owned development/hook objects from
  administrator-owned and cert-manager-owned objects.
- The workflow contains dedicated hook and manual/admin jobs with isolated
  cluster names.

### Phase 2 — GREEN

- Extend the Kind source selector and fixture model with explicit hook, manual,
  and AdministratorManaged scenarios.
- Add deterministic certificate fixtures using the repository's existing Go
  certificate helpers or a narrowly scoped test helper; do not use live PKI.
- Add `kind-hook` and `kind-manual-admin` workflow jobs.
- Set the existing generic lane's selector explicitly to `development` so its
  meaning is not inferred from an empty environment variable.
- Wire each scenario through the real operator Helm-chart install and
  reconciliation journey (CHECKPOINT W).

Implementation evidence currently being assembled is present in
`test/e2e/kind/cluster.go`, `test/e2e/kind/journey.go`,
`test/e2e/kind/tls_fixtures.go`, `test/e2e/kind/scenarios_test.go`, and
`.github/workflows/test.yml`. No tier may be marked verified from static
evidence alone; the hosted Kind jobs are required before the E2E cells move
from `planned` to `verified`.

### Phase 3 — REFACTOR

- Remove duplicated lane setup by extracting only shared Kind installation and
  polling helpers; keep source-specific setup and assertions explicit.
- Make ownership assertions reusable without weakening mode-specific checks.
- Add structured failure output containing TLS source, CR generation, phase,
  and relevant object/resourceVersion data.
- Update the TLS control-objective traceability document and platform-support
  documentation to distinguish verified generic modes from deferred OCP mode.

### Phase 4 — CHECK

Run the repository gates:

```text
go build ./...
golangci-lint run
make test-unit
make test-integration
make test
make test-pyramid
make manifests generate
git diff --exit-code config/ bundle/ dist/
KUBERNAUT_E2E_TLS_SOURCE=development make test-e2e-kind
KUBERNAUT_E2E_TLS_SOURCE=hook make test-e2e-kind
KUBERNAUT_E2E_TLS_SOURCE=manual-admin make test-e2e-kind
KUBERNAUT_E2E_TLS_SOURCE=certmanager make test-e2e-kind
```

The OCP command is intentionally not a required gate for this issue.

The pyramid gate must run before the live Kind lanes and must verify all of the
following:

1. `internal/resources` contains source-specific unit evidence for development,
   hook, manual, administrator-managed, and cert-manager behavior.
2. `internal/controller` contains envtest evidence for the corresponding
   reconciliation paths and fail-closed conditions.
3. `test/e2e/kind/scenarios_test.go` contains the required E2E IDs and calls the
   real CR/application/probe helpers rather than resource builders.
4. `.github/workflows/test.yml` contains one explicit job for each selector and
   does not rely on an empty selector or a generic development job.
5. No E2E test uses `Skip`, `XIt`, `PIt`, or a missing-source early return that
   can hide an unconfigured lane.

The live job then proves the final tier: the operator Helm chart is installed,
the CR is applied through the Kubernetes API, the reconciler reaches the
expected status, and a real workload trust/identity probe succeeds.

## 7. Wiring manifest

| Component | Production entry point | Wiring code location | IT/E2E test ID |
|---|---|---|---|
| TLS source resolution | `KubernautReconciler.validateTLSConfiguration` | `internal/controller/kubernaut_controller.go` → `internal/resources.ResolveTLSMaterial` | `IT-TLS-GAP-002`, `PYR-TLS-DEV-001`, `PYR-TLS-HOOK-001`, `PYR-TLS-MANUAL-001`, `PYR-TLS-ADMIN-001`, `PYR-TLS-CM-001` |
| Runtime TLS validation | `KubernautReconciler.validateExplicitTLS` | `internal/controller/kubernaut_controller.go` → `validateRuntimeTLSSecrets` and `validateTLSWebhookReadiness` | `IT-TLS-MANUAL-002`, `IT-TLS-WEBHOOK-001`, `E2E-TLS-FAIL-CLOSED-001` |
| Generated TLS material | `KubernautReconciler.ensureRuntimeTLS` | `internal/controller/kubernaut_controller.go` → `ensureDevelopmentSelfSignedTLS` | `IT-TLS-DEV-001`, `E2E-TLS-DEV-001`, `E2E-TLS-HOOK-001`, `E2E-TLS-HOOK-002` |
| Generic trust publication | `KubernautReconciler.ensureGenericTLSConfigMaps` | `internal/controller/kubernaut_controller.go` and `internal/resources/tlsconfigmaps.go` | `IT-TLS-GAP-003`, `E2E-TLS-HOOK-001`, `E2E-TLS-MANUAL-001` |
| Mode-specific CR fixture | Production CR application | `test/e2e/kind/journey.go` → `kubernautCR` and `manualAdminTLSConfig` | `E2E-TLS-DEV-001`, `E2E-TLS-HOOK-001`, `E2E-TLS-MANUAL-001`, `E2E-TLS-ADMIN-001` |
| Hook fixture/provisioner | Production Kind journey | `test/e2e/kind/journey.go`, `tls_fixtures.go`, `scenarios_test.go` | `IT-TLS-HOOK-001`, `E2E-TLS-HOOK-001`, `E2E-TLS-HOOK-002` |
| Manual/admin fixture | Production Kind journey | `test/e2e/kind/tls_fixtures.go` and `journey.go` | `IT-TLS-MANUAL-001`, `IT-TLS-ADMIN-001`, `E2E-TLS-MANUAL-001`, `E2E-TLS-ADMIN-001`, `E2E-TLS-CLEANUP-001` |
| Cert-manager resources | `KubernautReconciler.ensureCertManagerTLSResources` | `internal/controller/kubernaut_controller.go` → `internal/resources.CertManagerTLSResources` | `IT-TLS-PARITY-001`, `IT-TLS-CERTMANAGER-READY-001`, `E2E-TLS-CERTMANAGER-001`, `E2E-TLS-CERTMANAGER-002` |
| Generic Kind workflow lanes | `make test-e2e-kind` | `.github/workflows/test.yml` | `KUBERNAUT_E2E_TLS_SOURCE=development`, `hook`, `manual-admin`, `certmanager` |
| Pyramid enforcement | `make test-pyramid` | `hack/verify-test-pyramid.sh` and `test/e2e/kind/contract/selector_test.go` | `UT-TLS-498-001`, `PYR-TLS-*` |
| Evidence traceability | Repository security evidence | `docs/security/ISSUE-491-TLS-CONTROL-ATTESTATION.md` | All `BA-*`, `UT-*`, `IT-*`, and `E2E-*` rows |

Checkpoint W fails if a workflow job merely changes an environment variable but
the CR fixture still selects development mode, or if the manual/admin lane
mutates administrator-owned objects.

## 8. CRD and packaging impact

No CRD change is required merely to add the Kind selectors; selectors are test
configuration, not API fields. However, `v1alpha2` is unreleased, so an API
cleanup or normalization discovered by the RED contract tests is allowed.
Such a change must update all current Go/tests/docs references and run:

```text
make manifests generate
git diff --exit-code config/ bundle/ dist/
```

No migration or backward-compatibility contract is added for unreleased
`v1alpha2` enum/field values.

No cert-manager installation or upgrade is added to the operator. No OpenShift
RBAC, Route, service-CA, or router-CA qualification is added by this issue.

## 9. Completion criteria

- Dedicated hook and manual/admin Kind jobs are present and green.
- Development and cert-manager lanes remain green.
- Hook mode is selected directly and its operator ownership is proven.
- Both `manual` and `AdministratorManaged` selections are exercised.
- Manual/admin inputs are not adopted, overwritten, or deleted.
- Invalid sources fail closed and preserve the last working trust material.
- Every pyramid matrix row has passing unit, envtest, and applicable
  Helm-backed Kind E2E evidence; static markers alone do not satisfy the E2E
  tier.
- The pyramid gate fails when a source has a unit test but no controller wiring
  test, or a controller wiring test but no dedicated E2E job/scenario.
- OCP qualification is explicitly recorded as deferred, not reported as passed.
- Unit, integration, E2E, security traceability, lint, build, and generated
  artifact gates pass.
- Every business assertion has unit, integration, and—where applicable—E2E
  evidence; no source is declared complete on unit coverage alone.
- The traceability matrix links each test ID to a business behavior and to
  FedRAMP/NIST, SOC 2, and exact OWASP ASVS 5.0.0 objectives without claiming
  formal external compliance.
- The issue, this plan, and the evidence matrix link to one another.

## 10. Execution approval gate

This document is the implementation-plan artifact. Before further production
code or test edits, confirm that implementation is approved in this dedicated
worktree and that the existing uncommitted changes are the intended #498
baseline. The current primary checkout must remain untouched.

After approval, implementation proceeds in this order:

1. Run the RED tests and record the expected failures.
2. Reconcile any v1alpha2 normalization revealed by the contract tests; if the
   enum/field shape changes, regenerate the CRD and deepcopy artifacts before
   proceeding.
3. Complete GREEN wiring and run CHECKPOINT W for every row in the wiring
   manifest.
4. Complete REFACTOR and documentation/security traceability updates.
5. Run the repository gates, then all four hosted Kind selectors. A local
   compile or static pyramid pass cannot substitute for the hosted E2E tier.

**Plan confidence:** 97%

**Justification:** The source modes, existing controller ownership paths, Kind
journey, current CI gap, and dedicated worktree are identified. Removing the
unreleased backward-compatibility constraint allows direct v1alpha2 cleanup if
the RED contract exposes duplicate API behavior. Remaining risks are realistic
manual/admin fixtures, cert-manager cleanup timing, rotation observability, and
hosted Kind execution; the matrix isolates each risk and requires evidence at
all three pyramid tiers.

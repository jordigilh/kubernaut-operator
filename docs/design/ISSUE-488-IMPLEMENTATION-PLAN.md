# Issue #488 Implementation Plan

**Issue:** [kubernaut-operator#488](https://github.com/jordigilh/kubernaut-operator/issues/488)

**Parent initiative:** [#486](https://github.com/jordigilh/kubernaut-operator/issues/486)

**Status:** Core clean-break, platform-capability, TLS/exposure, monitoring,
native-policy, RBAC, packaging, and generic/Cilium/Calico Kind qualification are
complete on the working branch. Live OpenShift qualification remains blocked by
the local `oc` environment; Helm bootstrap scope remains a separate unresolved
packaging item.

**Methodology:** RED → GREEN → REFACTOR, with controller wiring verified in
GREEN and the full unit/integration/E2E pyramid required before completion.

**Related design documents:**

- `docs/design/ADR-PLATFORM-001-platform-neutral-provider-policy.md`
- `docs/design/ADR-PLATFORM-001-implementation-plan.md`
- `docs/installation/06-platform-support.md`

## 1. Objective and non-negotiable outcomes

Issue #488 is implemented as a bounded platform-neutrality initiative. The
operator must:

1. Reconcile its core lifecycle on generic Kubernetes/Kind without OpenShift,
   monitoring, certificate, CNI, or provider-policy APIs.
2. Keep OpenShift integrations optional, capability-gated, observable, and
   independently tested.
3. Use native, version-qualified Cilium, Calico, and OVN policy APIs when an
   active supported installation is positively identified.
4. Never install provider CRDs/operators, use static API-server CIDRs, or fall
   back to raw Kubernetes `NetworkPolicy` for provider policy.
5. Take `kubernaut.ai/v1alpha2` forward as a clean-break API with no
   v1alpha1 conversion path after the approved migration boundary.
6. Separate operator-bootstrap Helm ownership from the user-applied
   `Kubernaut` CR and operator-managed runtime workloads.
7. Make certificate, webhook CA-bundle, trust, exposure, monitoring, and
   provider failures visible through structured logs, events, and status.

## 2. Confirmed preflight baseline

- `SetupWithManager` unconditionally watches `config.openshift.io/APIServer`.
- The controller now reconciles `v1alpha2` directly; no v1alpha1 scheme or
  conversion path is registered.
- Optional OpenShift Route, service-CA, router-CA, monitoring, and provider
  policy paths are capability-gated; the raw Kubernaut NetworkPolicy renderer
  has been removed.
- `internal/controller/suite_test.go` keeps OpenShift-specific fixtures
  optional; the Kubernaut CRD itself is v1alpha2-only.
- Generic Ingress, explicit certificate sources, and provider-native policy
  adapters are wired into reconciliation; live enforcement qualification is
  still environment-dependent.
- Existing user-owned working-tree documentation changes must be preserved.

Current baseline checks already completed:

- `go build ./...`
- `golangci-lint run`
- `make test` (88.1% internal unit coverage; 78.0% controller integration coverage)
- `go test ./... -run=^$ -timeout=30s`
- `make manifests generate`, with no CRD or webhook drift
- `git diff --check`
- uncached unit and integration tests for all implemented non-E2E packages

Live OpenShift E2E remains environment-blocked because the local OCP kubeconfig
is incomplete. The Cilium and Calico dataplane lanes require a privileged
runtime; they were qualified locally with the rootful Podman connection.

## 3. Approval gates

No CRD, webhook, RBAC, or Helm ownership decision is to be inferred during
implementation. The following gates require explicit approval:

1. `v1alpha2` clean-break and `v1alpha1` removal/conversion boundary.
2. Provider-selection field, status-condition names, and phase semantics.
3. Common policy intent and API-server identity strategy.
4. Generic TLS/webhook/exposure ownership and certificate-source contract.
5. Helm ownership transition and clean-install boundary.
6. This TDD sequence and wiring manifest.

The current recommendation is the one recorded in ADR-PLATFORM-001: bounded
provider support, native policy only, capability-gated optional integrations,
explicit TLS sources, and bootstrap-only production Helm ownership.

## 3.1 Approved contract decisions

Approved by the issue owner on 2026-10-01:

- `v1alpha2` is the clean-break served/storage API; remove v1alpha1 conversion
  and reconcile paths and publish export/transform/recreate migration guidance.
- Use the bounded Cilium, Calico, and OVN provider matrix with active-installation
  evidence and runtime GVK/schema checks; do not install provider CRDs/operators
  or fall back to raw Kubernetes `NetworkPolicy`.
- Use provider-native API-server identity; never use static API-server CIDRs or
  unsafe allow-all fallbacks.
- Generic TLS/webhook/trust uses explicit administrator-managed, cert-manager,
  or opt-in development self-signed sources with source-specific CA publication
  and no plaintext fallback. OpenShift retains its existing service-CA/router-CA
  behavior behind the optional OpenShift adapter.
- Production Helm is bootstrap-only; the user/GitOps layer applies the
  `Kubernaut` CR, the operator owns runtime workloads, and dependencies remain
  explicitly non-production.
- The TDD sequence and wiring manifest in this document are approved.

## 4. TDD execution phases

### Phase 0 — Contract and RED-test preparation

- [x] Record preflight findings and feasibility outcomes in issue #488.
- [x] Add this issue-specific execution plan.
- [x] Confirm the six approval gates above.
- [x] Add the first RED test for generic-manager optional OpenShift watch
      discovery before production changes.
- [x] Add an envtest manager-startup proof with no OpenShift or monitoring
      CRDs.
- [x] Add RED tests for the clean-break CRD shape before production changes.

The first RED test is `internal/controller/platform_capabilities_test.go`.
GREEN gates the OpenShift `APIServer` watch on REST discovery in
`KubernautReconciler.SetupWithManager`; it does not yet implement the complete
capability registry.

### Phase 1 — `v1alpha2` clean break

**RED:** v1alpha2-only reconciliation, CRD served/storage shape, no conversion
webhook, and documented old-object migration behavior.

**GREEN:** migrate controller, resource builders, webhook, tests, and status
helpers; remove conversion registration and the v1alpha1 reconcile view.

**REFACTOR:** remove stale compatibility branches and regenerate deepcopy, CRD,
RBAC, OLM, and bundle artifacts.

**Integration:** `IT-CRD-V2-CLEAN-001`.

**Result:** complete. The generated CRD contains only served/storage
`v1alpha2`, conversion registration and implementations were removed, all
production/test callers use v1alpha2, and migration guidance is published in
`docs/upgrade-v1alpha1-to-v1alpha2.md`.

### Phase 2 — Optional capabilities and generic lifecycle

**RED:** envtest without OpenShift/monitoring CRDs; generic Ingress; explicit
certificate source; webhook CA publication; trust readiness/rotation; no
plaintext downgrade.

**GREEN:** capability discovery, optional OpenShift adapter, generic exposure,
certificate/trust orchestration, and independently gated monitoring.

**REFACTOR:** structured diagnostics, source-specific CA publication, and
fail-closed readiness/rotation behavior.

**Integration:** `IT-PLATFORM-KIND-001`, `IT-PLATFORM-OCP-001`,
`IT-TLS-WEBHOOK-001`, `IT-TLS-TRUST-001`.

### Phase 3 — Provider detection and common policy intent

**RED:** no provider, stale CRD, inactive installation, ambiguity, explicit
override, missing GVK/schema, unsupported version, and no-fallback cases.

**GREEN:** typed capability result, common traffic intent, deterministic names
and hashes, status conditions, events, and cleanup ownership.

**REFACTOR:** isolate discovery, compatibility validation, intent construction,
rendering, and lifecycle management.

**Integration:** `IT-POLICY-DETECTION-001`.

### Phase 4 — Native provider adapters

- Cilium 1.19.x–1.20.x: `cilium.io/v2` CNP/CCNP.
- Calico 3.31.x–3.32.x: `projectcalico.org/v3` NP/GNP.
- OpenShift OVN 4.19–4.22: `policy.networking.k8s.io/v1alpha1` ANP/BANP.

Each adapter uses fixture-first RED, minimal GREEN rendering, controller
wiring, schema/version validation, deterministic cleanup, and live enforcement
validation. No adapter installs CRDs/operators or emits raw NetworkPolicy.

**Integration:** `IT-POLICY-CILIUM-001`, `IT-POLICY-CALICO-001`,
`IT-POLICY-OVN-001`.

### Phase 5 — RBAC, cleanup, and observability

Split core and optional RBAC, assert exact provider verbs, test provider switch
and deletion cleanup, and emit structured create/update/delete/provider-change
audit traces with generation and resourceVersion.

**Integration:** `IT-POLICY-LIFECYCLE-001`.

### Phase 6 — Helm bootstrap

Add the production `kubernaut-operator` bootstrap chart and the separate,
explicitly non-production `kubernaut-dependencies` chart. Assert that neither
chart creates a `Kubernaut` CR or duplicates operator-owned runtime workloads.

**Integration:** `IT-HELM-BOOTSTRAP-001`.

### Phase 7 — Full validation

Run the test pyramid in separate lanes:

- Ginkgo/Gomega unit tests
- envtest with no optional APIs and optional capability fixtures
- plain Kind generic lifecycle
- Kind+Cilium and Kind+Calico native policy submission/enforcement
- OCP 4.19–4.22 + OVN Route/service-CA/monitoring/TLS/ANP enforcement
- negative unsupported, stale, ambiguous, and out-of-range cases

The Kind provider lanes are intentionally limited to the generic, Cilium, and
Calico profiles. OVN remains an OpenShift-only qualification lane; upstream
OVN-Kubernetes on Kind is not added without a separate provider-contract
decision.

The three isolated Kind lanes are implemented in `test/e2e/kind/` and run as
independent jobs in `.github/workflows/test.yml`, without a `needs` dependency
on the unit/integration job. The generic lane verifies fail-closed provider
detection and the absence of raw fallback policies; the Cilium and Calico
lanes install pinned providers, discover their live APIs, submit the native
rendered policy, probe allow/deny enforcement, and clean up managed objects.
The cleanup scenario also leaves a differently owned provider-native object in
place to verify ownership filtering.

## 5. Wiring manifest

| Component | Production entry point | Required integration test |
|---|---|---|
| Capability detector | `KubernautReconciler.Reconcile` | `IT-PLATFORM-KIND-001` |
| OpenShift adapter | deployment/exposure/monitoring subfunctions | `IT-PLATFORM-OCP-001` |
| Certificate resolver | TLS/trust reconciliation | `IT-TLS-WEBHOOK-001` |
| Webhook CA publisher | admission reconciliation | `IT-TLS-WEBHOOK-001` |
| Inter-service trust builder | workload deployment/rotation | `IT-TLS-TRUST-001` |
| Common policy intent | policy reconciliation replacing raw fallback | `IT-POLICY-DETECTION-001` |
| Provider detector | immediately before adapter selection | `IT-POLICY-DETECTION-001` |
| Cilium adapter | provider registry/lifecycle | `IT-POLICY-CILIUM-001` |
| Calico adapter | provider registry/lifecycle | `IT-POLICY-CALICO-001` |
| OVN adapter | provider registry/lifecycle | `IT-POLICY-OVN-001` |
| Generic exposure | workload exposure branch | `IT-PLATFORM-KIND-001` |
| Monitoring adapter | monitoring reconciliation | platform ITs |
| Helm bootstrap | chart installation pipeline | `IT-HELM-BOOTSTRAP-001` |

Checkpoint W fails if a new builder has no production caller, generic envtest
requires an OpenShift API, raw fallback policies remain, or TLS/trust output is
not consumed by the reconciliation path that needs it.

## 6. Control-objective test traceability

| Business assertion | FedRAMP/NIST | SOC 2 | OWASP ASVS |
|---|---|---|---|
| schema/migration boundary is enforced | CM-2, CM-3, CM-6 | CC8 | V1, V5, V14 |
| absent optional APIs do not break core lifecycle | CM-8, SI-4 | CC7, A1 | V1, V7, V14 |
| core/provider RBAC is least privilege | AC-3, AC-6 | CC6 | V4, V13, V14 |
| native policy has no unsafe fallback | AC-3, SI-10 | CC6, CC7 | V4, V5, V13 |
| TLS, CA publication, rotation, and fail-closed readiness work | SC-8, SC-12, SC-13, SC-17 | CC6, CC7, A1 | V6, V8, V9, V14 |
| actions and failures are observable | AU-2, AU-3, AU-12, SI-4 | CC7, CC8 | V7, V14 |
| Helm ownership is deterministic and non-duplicating | CM-2, CM-6, AC-6 | CC6, CC8 | V1, V14 |

## 7. Completion gate

The initiative is complete only when:

- all six approval gates are recorded;
- RED/GREEN/REFACTOR and Checkpoint W pass for every component;
- `go build ./...`, `golangci-lint run`, and `make test` pass;
- `make manifests` and `make generate` produce no unexpected diff;
- only the approved CRD version/conversion behavior remains;
- plain Kind, provider, and OpenShift results are recorded separately;
- TLS/webhook/trust rotation and failure behavior are recorded separately;
- support documentation and migration guidance are updated;
- final confidence is at least 90% with residual risks documented.

## 8. Execution log

| Date | Phase | Result |
|---|---|---|
| 2026-10-01 | Preflight/spikes | Completed; issue #488 updated with findings |
| 2026-10-01 | Branch | `fix/488-platform-neutrality` created from `origin/main` |
| 2026-10-01 | Phase 0 RED/GREEN | Optional OpenShift watch test added; REST-discovery gate implemented |
| 2026-10-01 | Phase 0 CHECK | `make test-integration` passed; controller coverage 78.6%; generic manager-startup IT passed |
| 2026-10-01 | Approval gate | All six gates approved; generic TLS explicit-source contract approved with existing OCP certificate logic retained |
| 2026-10-01 | Phase 1 RED/GREEN/REFACTOR | v1alpha2-only controller/resource/webhook path; conversion registration and v1alpha1 API/sample surface removed |
| 2026-10-01 | Phase 1 CHECK | `make test-unit` passed; `make test-integration` passed; controller coverage 79.1%; generated CRD has only v1alpha2 |
| 2026-10-01 | Final repository CHECK | `make test`, `go build ./...`, `go vet ./...`, `golangci-lint run`, `make manifests generate`, `make bundle`, `make build-installer`, and `git diff --check` passed; uncached non-E2E packages passed. Live OCP E2E and provider dataplane enforcement remain blocked by the unavailable `oc` kubeconfig/provider APIs. |
| 2026-10-01 | Kind lane CHECK | The production-manifest harness reached the real operator and component images. The first generic lifecycle run exposed OpenShift-only CA init wiring in explicit generic TLS; the adapter split was fixed. Cilium and Calico remain pinned as independent CI jobs; local rootless Podman cannot qualify their privileged dataplanes. |
| 2026-10-02 | Provider/wiring CHECK | Cilium deployment-phase envtest wiring passed; manager startup, monitoring resource creation, and stale-route cleanup coverage raised the controller suite to 78.0%. `make test-integration` now enforces the 78% floor. |
| 2026-10-02 | Unit/static CHECK | `make test-unit` passed at 88.1% internal unit coverage; `go build ./...`, `go vet ./...`, `golangci-lint run`, Kind package compile/vet, `make test-pyramid`, and `git diff --check` passed. |
| 2026-10-02 | Calico RBAC CHECK | Calico's extension API required `projectcalico.org` tier read access and the `tier.networkpolicies` pseudo-resource. Generated manager RBAC now grants only `get/list/watch` on tiers and the existing NetworkPolicy CRUD verbs on the pseudo-resource; a regression test covers both rules. |
| 2026-10-02 | Kind provider CHECK | Fresh production-manifest lanes passed with `kubernaut-operator:1.6.0-rc20` and matching component images: generic (3/3), Cilium 1.20.2 (3/3), and Calico 3.31.4 (3/3). Each verified CR lifecycle/status, native enforcement where applicable, finalizer cleanup, and preservation of an unmanaged provider policy. Rootless Podman cannot mount Cilium BPF; the successful provider runs used the rootful Podman connection. OCP E2E remains blocked by the incomplete local `oc` kubeconfig. |

# ADR-PLATFORM-001 Implementation Plan

**Issue**: [kubernaut-operator#486](https://github.com/jordigilh/kubernaut-operator/issues/486)
**ADR**: [ADR-PLATFORM-001-platform-neutral-provider-policy.md](ADR-PLATFORM-001-platform-neutral-provider-policy.md)
**Documentation issue**: [kubernaut-operator#487](https://github.com/jordigilh/kubernaut-operator/issues/487)
**Methodology**: RED → GREEN → REFACTOR → CHECK, with wiring verification in GREEN
**Status**: Implementation tracked on `fix/488-platform-neutrality`; approval gates are recorded below and generated artifacts remain part of the completion gate
**Estimated effort**: Multi-PR initiative; each phase is independently reviewable

---

## 1. Success criteria

The initiative is complete only when all of the following are true:

1. A plain Kind/generic Kubernetes cluster with no OpenShift APIs, monitoring CRDs, cert-manager, or CNI policy CRDs starts the operator and reconciles the core lifecycle.
2. A generic cluster with an explicitly configured Ingress/TLS/monitoring contract reaches the same core outcome as OpenShift without OpenShift defaults.
3. OpenShift 4.19–4.22 with OVN-Kubernetes remains a design target for Route, service-CA, monitoring, TLS-profile, and OVN policy adapters; live qualification is deferred and no release claim is made here.
4. Cilium 1.19.x–1.20.x and Calico 3.31.x–3.32.x render only their supported native policy GVKs when active installation evidence and runtime compatibility checks pass.
5. Unsupported, ambiguous, inactive, or out-of-range providers render no provider policy resources and produce actionable status/events.
6. No provider adapter installs CRDs/operators, uses static API-server CIDRs, or falls back to raw Kubernetes `NetworkPolicy`.
7. `v1alpha2` is the only CRD version in the redesigned API; no conversion webhook or v1alpha1 reconcile path remains.
8. The current OLM/Kustomize installation bootstraps the operator and CRD boundary, never creates a `Kubernaut` CR, and does not compete with the operator for managed workload ownership; a dedicated operator Helm chart is deferred.
9. The separate `kubernaut-dependencies` chart is explicitly limited to non-production testing, demos, and CI; it is not required by, or a dependency of, the current production installation.
10. Generic Kubernetes and Kind can select an approved certificate source for the manager webhook, managed AuthWebhook, inter-service leaves/trust, and external Ingress without OpenShift annotations or fixed OpenShift ConfigMaps.
11. Certificate rotation, webhook `caBundle` publication, trust-bundle readiness, and certificate-source failures are observable and never silently downgrade required TLS to plaintext.
12. `go build ./...`, `golangci-lint run`, `make test`, `make manifests`, and `make generate` pass, with no unexpected generated-manifest diff.

---

## 2. Pre-implementation analysis

### Confirmed current blast radius

| Area | Current dependency | Planned resolution |
|---|---|---|
| API path | Controller fetches v1alpha2 and converts to v1alpha1 on every reconcile | Make v1alpha2 the controller/resource-builder type; remove conversion path |
| Manager setup | Unconditional `config.openshift.io/APIServer` watch | Optional capability adapter; core setup contains no unavailable typed watch |
| Scheme | OpenShift, monitoring, v1alpha1, and v1alpha2 types are installed unconditionally | Keep only core types in the core scheme; register optional types only where their adapter needs them |
| Exposure | `internal/resources/ocp.go` and console builders emit Routes | Add portable Ingress builder; retain Route adapter and platform-neutral exposure status |
| TLS/trust | Service-CA annotations, injected bundles, router CA, OCP TLS profile, and file-mounted manager/AuthWebhook certificates | Separate manager, AuthWebhook, inter-service, external-exposure, outbound-trust, and audit-integrity contracts; administrator-managed, cert-manager, explicit development self-signed, and optional OpenShift sources; source-specific `caBundle` injection and rotation ordering |
| Monitoring | OCP Thanos/AlertManager defaults and optional `monitoring.coreos.com` resources | Explicit endpoints and capability-gated ServiceMonitor/PrometheusRule/AlertmanagerConfig |
| Policy | `internal/resources/networkpolicies.go` emits raw NetworkPolicy and API-server CIDRs | Common policy intent plus Cilium, Calico, and OVN adapters; no raw fallback |
| RBAC | Core generated RBAC includes Route, OCP config, monitoring, and raw NetworkPolicy permissions | Split core and optional integration permissions; add exact provider GVK verbs |
| Packaging | OLM/Kustomize only; no dedicated operator Helm chart | Keep current artifacts; add chart, ownership tests, chart lint/template tests, and clean-install docs in a follow-up |
| Tests | OCP-heavy integration/E2E and Calico-specific enforcement | Add plain Kind, capability fixtures, provider rendering, and separate live enforcement lanes |

### Required preflight checks

- Read `api/v1alpha2/kubernaut_types.go` before every CRD field reference (CHECKPOINT A).
- Search for existing builders/callers before adding a provider or platform component (CHECKPOINT B).
- Verify each new builder is called from `KubernautReconciler` and has an integration test (CHECKPOINT C/W).
- Treat the v1alpha2 CRD as the sole application-configuration schema; bootstrap chart values must not duplicate service/workload fields from the upstream direct-workload chart.
- Preserve unrelated user edits in `.cursor/rules/hindsight-memory.mdc` and `docs/installation/04-fleet-mcp-gateway.md`.
- **Completed clean-break gate:** the full-reference migration is complete;
  the clean-break CRD tests prove that no production path uses v1alpha1, and
  the v1alpha1 API/conversion packages were removed from this branch.

### Parity classification used by the implementation

The full evidence matrix is maintained in the ADR. The implementation backlog
uses these classifications so that “parity” does not accidentally mean copying
the upstream chart's direct workload ownership:

| Classification | Helm/operator surfaces | Implementation consequence |
|---|---|---|
| **P0 portable core** | Manager/AuthWebhook/inter-service/external TLS and trust, webhook `caBundle`, generic Ingress, explicit monitoring capabilities, Kubernaut CRD/bootstrap ownership, migration and hook ordering | Must work on plain Kubernetes and Kind without OpenShift APIs, cert-manager, monitoring CRDs, or provider CRDs unless the selected integration is explicitly enabled. The current OLM/Kustomize installation installs prerequisites only; the user/GitOps layer applies the CR. |
| **P1 configuration/security parity** | Input policy ConfigMaps, shared application configuration and optional integrations, scaling/scheduling controls, singleton install/secret preflight, DataStorage HMAC/rate-limit/replay-cache gaps, disconnected-image and RBAC extensions | Track as operator-owned API/configuration work; do not render duplicate runtime workloads from Helm. |
| **Intentional divergence** | Raw NetworkPolicy/static API-server CIDRs, bundled PostgreSQL/Valkey, upstream chart workload templates | Do not port as solutions. Replace policy with native adapters; retain BYO stateful infrastructure and operator runtime ownership. |
| **Platform/provider adapter** | OpenShift Routes/service-CA/router-CA/TLS profile/monitoring, Cilium/Calico/OVN policy APIs, cert-manager integration | Capability-gated, independently observable, and owned by the platform/provider operator where applicable. |

#### Certificate implementation contract to freeze in RED

Before writing the certificate builders, tests must fix the logical artifact
and ownership contract below. Field names may change during API review, but the
separation must not:

| Artifact | Generic source modes | Owner and readiness proof |
|---|---|---|
| Operator manager serving Secret and bootstrap webhook CA | Administrator-managed, cert-manager, explicit development self-signed | OLM/Kustomize owns bootstrap resources; manager starts only with a valid leaf, and the corresponding webhook configuration has the matching CA. A dedicated Helm path is deferred. |
| AuthWebhook serving Secret and mutating/validating `caBundle` | Administrator-managed, cert-manager, explicit development self-signed, or OpenShift service-CA adapter | Operator owns the per-instance runtime objects; tests verify SANs, Secret keys, exact CA bytes, and fail-closed readiness. |
| Dedicated inter-service CA, leaf Secrets, and public trust ConfigMap | Administrator-managed, cert-manager, explicit development self-signed, or OpenShift service-CA adapter | Operator owns runtime trust artifacts; tests verify every enabled TLS server has a leaf and every client mounts the matching public bundle. |
| Ingress TLS Secret / Route termination | Administrator-managed or cert-manager; OpenShift router adapter | Exposure adapter references the configured host/Secret and reports unavailable exposure; it does not invent a generic hostname. |
| Database/Valkey/monitoring/IdP/LLM CA and client material | Explicit external Secret/ConfigMap references | Referenced, not co-owned; tests verify paths and no system-trust replacement or plaintext fallback. |
| DataStorage signing and audit-HMAC material | Explicit Secret or separately approved provisioner | Separate from network PKI; tests verify absence is visible and service startup cannot silently proceed without required integrity material. |

Source-specific CA publication is tested separately: cert-manager/cainjector
for cert-manager mode, operator/bootstrap patching for administrator-managed or
self-signed mode, and service-CA injection for the OpenShift adapter. A generic
rendered object must not carry an OpenShift CA-injection annotation as its only
trust mechanism.

---

## 3. File and component change map

Paths for new packages are proposed and must be confirmed during the RED phase after implementation discovery.

| Area | Production files | Tests/fixtures | TDD phase |
|---|---|---|---|
| CRD clean break | `api/v1alpha2/kubernaut_types.go`, `api/v1alpha2/zz_generated.deepcopy.go`, `api/v1alpha1/*`, `cmd/main.go`, `config/crd/bases/kubernaut.ai_kubernauts.yaml` | CRD schema/admission and controller fixtures | RED then GREEN; CHECKPOINT CRD |
| Platform capability registry | `internal/platform/capabilities.go`, `internal/platform/discovery.go`, optional `internal/platform/openshift.go` | `internal/platform/*_test.go`, discovery fixtures with absent APIs | RED/IT then GREEN |
| Generic exposure | `internal/resources/ingress.go`, portable exposure types in v1alpha2, Route adapter updates | `internal/resources/ingress_test.go`, controller exposure IT | RED/IT then GREEN |
| TLS/webhook/trust | `internal/resources/tls.go` or existing TLS/trust builders, `internal/resources/webhooks.go`, controller certificate adapter | builder tests, rotation/readiness IT, cert-manager/administrator fixtures | RED/IT then GREEN |
| Monitoring | existing `internal/resources/monitoring.go`, `ocp.go`, ConfigMap builders, controller capability gates | no-monitoring, Prometheus Operator, OpenShift monitoring fixtures | RED/IT then GREEN |
| Common policy intent | new `internal/policy/intent.go` and policy lifecycle helpers | `internal/policy/intent_test.go` | RED then GREEN |
| Provider detection | new `internal/policy/detect.go` and discovery/schema helpers | auto/override/ambiguous/out-of-range fixtures | RED/IT then GREEN |
| Cilium adapter | new `internal/policy/cilium.go` | `cilium.io/v2` CNP/CCNP golden fixtures | RED then GREEN |
| Calico adapter | new `internal/policy/calico.go` | `projectcalico.org/v3` NP/GNP golden fixtures | RED then GREEN |
| OVN adapter | new `internal/policy/ovn.go` | ANP/BANP fixtures for OCP 4.19–4.22; 4.23 negative fixture | RED then GREEN |
| Policy controller wiring | `internal/controller/kubernaut_controller.go`, RBAC markers, cleanup helpers | controller envtest for all provider outcomes | RED IT first, then GREEN |
| RBAC split | `internal/resources/rbac.go`, controller markers, `config/rbac/*`, optional integration roles | generated RBAC assertions and least-privilege tests | GREEN/CHECK |
| Helm bootstrap | `charts/kubernaut-operator/**`, `charts/kubernaut-dependencies/**`, `Makefile`, CI workflow | production operator-chart lint/template/upgrade fixtures; non-production dependencies-chart render and isolation fixtures | RED then GREEN |
| Documentation | `docs/installation/06-platform-support.md`, README/index, upgrade/security docs | markdown/link checks where available | CHECK |

---

## 4. TDD execution order

### Phase 0 — Approval and contract freeze

**RED/design tests**

- Write table-driven BDD examples for the provider matrix, detection outcomes, and no-fallback rule before adapter code exists.
- Define the v1alpha2 provider-selection field, status condition names, and phase semantics in the API review.
- Define the common traffic-intent model and the provider-native API-server identity requirement.
- Define the Helm ownership labels/annotations, production operator-chart bootstrap-only values surface, non-production dependencies-chart boundary, and clean-install sequence (`helm install` → manager readiness → optional demo dependencies/BYO prerequisites → user/GitOps CR apply).

**Gate**: user approval is required for the six ADR approval gates. No production implementation starts before this gate.

### Phase 1 — v1alpha2 clean break

**RED**

- Add controller integration tests that create only a v1alpha2 CR and prove reconciliation does not invoke conversion or require a v1alpha1 scheme.
- Add a CRD manifest test that expects only v1alpha2 served/storage and no conversion webhook stanza.
- Add negative tests for old-version objects according to the documented recreate/migration contract.

**GREEN**

- Migrate controller signatures, resource builders, webhooks, tests, and status helpers to v1alpha2.
- Remove conversion registration and v1alpha1 conversion implementation after references are gone.
- Regenerate deepcopy, CRD, RBAC, and bundle artifacts.

**REFACTOR**

- Remove compatibility branches and stale v1alpha1 comments.
- Keep migration logic for data/resource migrations distinct from API conversion.

**Integration ID**: `IT-CRD-V2-CLEAN-001`.

### Phase 2 — Optional platform capabilities and generic lifecycle

**Approved API/architecture decision:** `ADR-PLATFORM-002-generic-lifecycle-tls-exposure.md`.
The user approved the explicit TLS source modes (`AdministratorManaged`,
`CertManager`, and `DevelopmentSelfSigned`) and the generic Ingress contract;
Phase 2 is implemented before provider-policy adapters.

**RED**

- Envtest suite starts with no OpenShift or monitoring CRDs and proves manager setup succeeds.
- Reconciliation tests prove absent optional APIs produce conditions/events, not watch errors or retry storms.
- Generic Ingress, supplied TLS Secret/CA, webhook certificate, and trust-bundle tests define the portable path.
- Certificate-source tests cover administrator-managed, cert-manager-present, cert-manager-absent, and explicit development self-signed modes. The negative cases prove that a missing/invalid source does not silently create plaintext services or an empty webhook `caBundle`.
- Artifact tests keep the manager webhook, managed AuthWebhook, inter-service CA/leaves, external Ingress certificate, outbound trust references, and DataStorage signing/HMAC material separate. They verify required keys, Service/Ingress SANs, CA consistency, owner labels, and readiness ordering.
- Rotation tests exercise leaf renewal and CA overlap/rollout behavior: the old trust root remains usable until all dependent workloads and webhook configurations have moved, and a failed rotation leaves an actionable condition rather than deleting the last working material.

**GREEN**

- Introduce capability discovery that uses discovery/API checks and explicit adapter interfaces.
- Remove the unconditional APIServer watch from the core builder; enqueue from optional adapters only when available.
- Add portable Ingress and certificate/CA paths while retaining Route/service-CA behind capability checks. The generic path must select an administrator-managed Secret, configured cert-manager resources, or an explicit development self-signed provisioner; it must not infer OpenShift annotations or fixed ConfigMaps.
- Publish and mount a dedicated internal trust bundle, issue or reference per-service leaves, and update admission `caBundle` values through the selected source-specific mechanism.
- Keep bootstrap chart/OLM ownership of manager serving-certificate prerequisites distinct from operator ownership of per-instance runtime certificate and trust artifacts.
- Make monitoring endpoints explicit on generic Kubernetes; gate ServiceMonitor/PrometheusRule/AlertmanagerConfig by discovered APIs.

**REFACTOR**

- Keep optional integration code out of core reconcile branches where possible.
- Ensure every absent-API error is classified as optional/unavailable rather than silently ignored.
- Ensure certificate readiness, rotation, and source diagnostics include generation/resourceVersion context and are emitted as structured logs, events, and status conditions.

**Integration IDs**: `IT-PLATFORM-KIND-001`, `IT-PLATFORM-OCP-001`, `IT-TLS-WEBHOOK-001`, `IT-TLS-TRUST-001`.

### Phase 3 — Provider detection and policy intent

**RED**

Add Ginkgo/Gomega tests for:

- no provider API and no provider installation evidence;
- one active supported Cilium, Calico, or OVN provider;
- stale CRD without active installation evidence;
- two active providers/ambiguous evidence;
- explicit override selecting one of several providers;
- explicit override with missing GVK, wrong schema, or out-of-range release;
- no rendered policy object for every unsupported/ambiguous outcome;
- no static API-server CIDR in the intent or rendered objects.

**GREEN**

- Implement a detector that returns a typed provider capability result.
- Implement a provider-neutral intent model from the current traffic matrix.
- Add `PolicyProviderDetected`/`ProviderPolicyReady`-class status reporting and structured events.
- Add deterministic policy names, labels, spec hashes, and cleanup/pruning.

**REFACTOR**

- Separate discovery, compatibility validation, intent construction, rendering, and lifecycle management.
- Avoid `any`/untyped maps except where dynamic discovery is unavoidable; wrap unstructured operations with typed validation.

**Integration ID**: `IT-POLICY-DETECTION-001`.

### Phase 4 — Cilium, Calico, and OVN adapters

Each adapter follows the same sequence: fixture-first RED, minimal GREEN renderer, controller wiring, then refactor.

#### Cilium

- Test `cilium.io/v2` CNP and CCNP GVK/schema validation for 1.19.x–1.20.x.
- Render only the common fields needed by the intent model.
- Use Cilium-native API-server entity semantics where supported; no IP fallback.
- Test namespaced and cluster-scoped cleanup separately.

#### Calico

- Test `projectcalico.org/v3` NetworkPolicy and GlobalNetworkPolicy schemas for 3.31.x–3.32.x.
- Render provider-native selectors and global scope only when the intent requires it.
- Reject an intent that would require a static API-server CIDR or unsupported Calico construct.

#### OVN/OpenShift

- Require positive OpenShift `Network` configuration identifying `OVNKubernetes`.
- Require `policy.networking.k8s.io/v1alpha1` ANP/BANP discovery/schema validation.
- Qualify only OCP 4.19–4.22 fixtures and live lanes.
- Explicitly test that `EgressFirewall` is not emitted as a substitute and OCP 4.23 is rejected by the initial contract.

**Integration IDs**: `IT-POLICY-CILIUM-001`, `IT-POLICY-CALICO-001`, `IT-POLICY-OVN-001`.

### Phase 5 — RBAC, cleanup, and observability

**RED**

- Generated RBAC tests assert core Kubernetes permissions are sufficient for Kind.
- Provider-specific roles assert only get/list/watch/create/update/patch/delete for the supported provider resources; no CRD/operator permissions.
- Cleanup tests cover provider switch, disabled components, CR deletion, cluster-scoped policy pruning, and stale resources.
- Status/event tests assert generation, provider, API version, diagnostic reason, and resource names.

**GREEN/REFACTOR**

- Split optional RBAC manifests from core RBAC and make bindings conditional on configured/detected integrations.
- Make deletion idempotent and tolerate an API disappearing during cleanup.
- Add structured audit logs for policy create/update/delete and provider transitions.

**Integration ID**: `IT-POLICY-LIFECYCLE-001`.

### Phase 6 — Deferred production Helm bootstrap and non-production dependencies chart

The dedicated operator Helm bootstrap chart is deferred and is not part of the
current release claim. The current installation path remains OLM/Kustomize;
the checks below are the follow-up acceptance contract, not completion gates
for this work.

**RED**

- `helm lint` and `helm template` tests define the operator/CRD bootstrap boundary and the explicit no-CR-rendering contract.
- Rendered chart tests assert that no `Kubernaut` CR and no application/workload configuration schema is present; the only application schema is the installed CRD.
- A rendered chart must not render Kubernaut workloads that the operator also owns.
- Dependencies-chart tests assert that PostgreSQL/Valkey resources are isolated to that chart, clearly marked non-production, and never become an implicit dependency of the production operator chart.
- Clean-install test verifies chart installation on Kind without `oc` or OpenShift CRDs.
- TLS render tests cover manager serving-certificate prerequisites, explicit certificate-source selection, webhook CA publication, and the absence of OpenShift-only annotations in the generic profile. They also verify that the chart does not co-own per-instance runtime Secrets/ConfigMaps reconciled by the operator.
- Secret/credential preflight tests distinguish administrator-owned prerequisites from operator-generated runtime material and fail with actionable messages when required data is missing.
- Hook/upgrade tests prove that the bootstrap chart does not duplicate the operator's database migration, per-instance TLS, or inter-service CA-sync Jobs, while CRD upgrade ownership is assigned to exactly one packaging path.
- Upgrade/reinstall documentation tests verify no claim of unsafe in-place adoption.

**GREEN/REFACTOR**

- When the follow-up starts, add the `kubernaut-operator` chart with image registry/pull-secret/operator bootstrap values.
- Add the separate `kubernaut-dependencies` chart for testing, demos, and CI only, with explicit non-production documentation and no production-readiness claim.
- Add CRD installation and document the separate user/GitOps CR application step; do not add chart values or templates that construct application configuration.
- Add bootstrap certificate resources only for the operator manager path; delegate per-instance AuthWebhook/inter-service certificate reconciliation to the operator or the explicitly selected certificate provider.
- Add chart CI and Make targets without removing OLM/Kustomize paths.
- Add explicit ownership labels and conflict detection.

**Integration ID**: `IT-HELM-BOOTSTRAP-001`.

### Phase 7 — Full platform/provider validation

Run the three-tier pyramid:

| Lane | Environment | Proof |
|---|---|---|
| Unit | Pure Go/Ginkgo | builder shape, schema gates, policy translation, status messages |
| Integration | envtest with no optional CRDs; optional CRD fixtures | controller wiring, absent API behavior, lifecycle, cleanup |
| Generic E2E | plain Kind | Production-manifest/operator bootstrap, core lifecycle, generic TLS/exposure/monitoring, no provider policy resources |
| Provider E2E | Kind + qualified Cilium/Calico | native CR submission and live enforcement, separately per provider |
| OpenShift E2E | OCP 4.19–4.22 + OVN (deferred) | Route/service-CA/monitoring/TLS and ANP/BANP submission/enforcement; no current release claim |
| Negative compatibility | fixtures/live as available | unsupported versions, OCP 4.23, ambiguous providers, stale CRDs, no fallback |

The operator tests submission and lifecycle. Provider/platform test suites prove dataplane enforcement; those responsibilities must not be conflated.

---

## 5. Wiring manifest

The following production callers are mandatory before a component is considered implemented:

| Component | Production entry point | Wiring location | Required IT |
|---|---|---|---|
| Platform capability detector | `KubernautReconciler.Reconcile` | `internal/controller/kubernaut_controller.go` phase/deploy orchestration | `IT-PLATFORM-KIND-001` |
| Optional OpenShift adapter | deploy/exposure/monitoring sub-functions | controller capability registry; no unconditional `SetupWithManager` watch | Deferred; `IT-PLATFORM-OCP-001` remains unverified |
| Certificate-source resolver/provisioner | platform/TLS reconciliation | manager bootstrap plus per-instance TLS/trust orchestration; source-specific ownership and no plaintext fallback | `IT-PLATFORM-KIND-001` and `IT-PLATFORM-OCP-001` |
| Webhook CA-bundle publisher | admission webhook reconciliation | administrator/self-signed patch, cert-manager injection, or OpenShift service-CA adapter | `IT-TLS-WEBHOOK-001` |
| Inter-service trust/leaf builder | workload deployment/configuration | TLS/trust orchestration before `deployWorkloads()` and on rotation | `IT-TLS-TRUST-001` |
| Common policy intent builder | policy reconciliation | replacement for `reconcileNetworkPolicies()` | `IT-POLICY-DETECTION-001` |
| Provider detector | policy reconciliation | immediately before adapter selection | `IT-POLICY-DETECTION-001` |
| Cilium adapter | policy reconciliation | provider registry → Cilium renderer/lifecycle | `IT-POLICY-CILIUM-001` |
| Calico adapter | policy reconciliation | provider registry → Calico renderer/lifecycle | `IT-POLICY-CALICO-001` |
| OVN adapter | policy reconciliation | provider registry → OVN renderer/lifecycle | Deferred; `IT-POLICY-OVN-001` remains unverified |
| Generic exposure builder | workload/exposure deployment | `deployWorkloads()` exposure branch | `IT-PLATFORM-KIND-001` |
| Monitoring adapter | monitoring reconciliation | `reconcileMonitoringAndAlerts()` capability branch | `IT-PLATFORM-KIND-001` and `IT-PLATFORM-OCP-001` |
| Helm bootstrap | installation pipeline | Deferred chart templates and CI, not controller ownership | `IT-HELM-BOOTSTRAP-001` remains unverified |

**Checkpoint W** fails if any provider/resource builder has no production caller, if the controller still emits raw fallback policies, if a certificate/trust builder is not wired into the reconciliation path that consumes it, or if a generic envtest needs an OpenShift API merely to start.

---

## 6. CRD and generated-artifact checklist

When the v1alpha2 type changes:

```bash
make manifests generate
git diff --exit-code config/
go test ./... -run=^$ -timeout=30s
```

Verify specifically:

- only v1alpha2 is served/storage;
- no conversion webhook configuration remains;
- provider selection has an enum/default and documented override semantics;
- API-server CIDR fields are removed from the clean-break API;
- status conditions are structural and have stable descriptions;
- provider-native RBAC is least privilege;
- OLM and Helm artifacts do not reintroduce unsupported OpenShift-only prerequisites.

---

## 7. Risks and mitigations

| Risk | Mitigation |
|---|---|
| v1alpha1 users lose transparent upgrades | publish migration/recreate procedure; block release until clean-break docs and tests exist |
| A provider CRD is present but inactive | require active-installation evidence and report ambiguity |
| Provider API changes within a minor release | runtime GVK/schema validation, bounded ranges, golden fixtures, compatibility review |
| Native adapters cannot express a common rule | omit that provider policy, report the exact capability gap, never broaden with CIDR/allow-all fallback |
| Generic webhook/TLS path is insecure | require explicit certificate source, rotation tests, and visible status; keep dev mode opt-in |
| CA rotation causes an admission or inter-service blackout | publish new trust before leaves, support an overlap window or coordinated restart, and test `caBundle`/workload ordering |
| Bootstrap and runtime certificate resources are co-owned | assign manager prerequisites to Helm/OLM and per-instance runtime artifacts to the operator/provider; reject conflicting ownership in chart tests |
| Helm adopts operator-owned workloads accidentally | chart ownership tests, labels, clean-install-only first phase |
| Optional typed watches recreate the OCP startup bug | capability-gated dynamic watches or event/requeue paths, tested with absent APIs |
| Cluster-scoped policy cleanup leaks | labels, finalizer cleanup, deterministic names, and deletion tests |

---

## 8. Completion gate

The implementation PR series may be called complete only after:

- CHECKPOINT W passes for every new component;
- `go build ./...` succeeds;
- `golangci-lint run` has no new findings;
- `make test` passes with the repository's Ginkgo/Gomega test framework;
- generated CRD/RBAC/CSV artifacts are clean;
- plain Kind, provider, and OCP test results are recorded separately;
- manager/AuthWebhook/inter-service/external TLS, trust-bundle, webhook `caBundle`, and rotation results are recorded separately for generic and OpenShift source modes;
- the support matrix and upgrade policy are updated with evidence;
- the final confidence assessment is at least 90%, with remaining risks documented.

**Plan confidence**: 95%.

**Justification**: the current controller/resource/RBAC blast radius and the compatibility contract are mapped, and the plan explicitly separates portable core behavior, optional OpenShift integrations, provider policy submission, and provider enforcement. Remaining uncertainty is limited to the final v1alpha2 field names, the exact provider-native expression of each traffic-intent rule, and the certificate-management implementation selected during the approved design phase.

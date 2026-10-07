# Issue #492: Kubernaut Agent monitoring egress policy

**Status:** Cilium/Calico and OVN monitoring implementations are complete. The OVN API/dataplane primitive is qualified on both disposable OpenShift 4.22.16 SNO clusters, and the working-tree operator-generated OVN policy plus Agent monitoring journey is manually qualified on SNO-A. Unit, integration, build, lint, manifest, security, and both provider E2E gates pass. The Cilium/Calico Agent journey passed on the rootful Linux fallback with pinned Cilium 1.20.2 and Calico v3.31.4; OCP/OVN qualification remains manual rather than CI-backed.
**Issue:** [kubernaut-operator#492](https://github.com/jordigilh/kubernaut-operator/issues/492)
**Milestone:** v1.6
**Related:** #488 (native provider policy), #489 (operator-only Helm/bootstrap), #468 (AlertManager RBAC), upstream `kubernaut#2484`

## 1. Scope and required outcome

Issue #492 is an open security-labelled bug titled **“fix(policy): preserve Kubernaut Agent Prometheus and AlertManager egress”**. Its acceptance contract is:

* preserve Kubernaut Agent access to the configured/resolved in-cluster Prometheus and AlertManager API Services;
* allow the actual TCP ports, including the reported generic Kubernetes case `9090` (Prometheus) and `9093` (AlertManager);
* retain OpenShift platform-specific service/port behavior (`9091` Thanos Querier and `9094` AlertManager service endpoint in the current adapter constants); OVN resolves the Service selectors and effective backend ports (`9091` and `9095`) and the working-tree operator journey is manually qualified on SNO-A;
* add access only to components that have the corresponding client, without broad CIDR or raw `NetworkPolicy` fallback;
* keep explicit external-endpoint/supplemental-policy behavior when a destination cannot be safely represented by the selected provider;
* keep runtime policies operator-owned; the #489 Helm chart remains bootstrap-only and must not render application NetworkPolicies.

The business failure is loss of RCA evidence: the Agent’s `get_alerts` and `get_metric_names` calls time out when default-deny policy omits monitoring egress.

**Approval boundary:** the selector-derived OVN pod-peer design was approved for implementation on 2026-10-06. Production OVN monitoring rendering is now wired and manually qualified on OpenShift 4.22.16 SNO-A. There is no automated OCP/OVN CI lane, so the manual evidence is not represented as an automated regression guarantee.

## 2. Worktree and preservation boundary

Preflight was run on the existing branch `docs/issue-489-helm-plan`.

* `HEAD` remains the existing docs commit `8aad209 docs: add issue 489 operator Helm plan`.
* The existing #489 plan commit remains untouched; the #492 implementation and tests remain uncommitted on this worktree.
* No reset, checkout, discard, amend, commit, or PR operation has been performed for this issue.
* The implementation includes the approved Cilium, Calico, and OVN native adapters. Disposable SNO probe resources, the temporary operator run, and its generated CRDs were removed after validation.

## 3. Preflight evidence

### Issue and architecture

The GitHub issue body states that the pre-migration raw policy had monitoring egress through `monitoringEgressRules(..., true)`, but the native migration in #488 models only API-server, DNS, and managed-namespace traffic. The repository confirms:

| Evidence | Current behavior | Consequence |
|---|---|---|
| `internal/policy/intent.go:34-44` | `Intent` has namespace, instance, components, API-server identity, DNS namespace, and legacy CIDR guard; no monitoring destinations | Monitoring cannot reach a renderer through provider-neutral intent |
| `internal/policy/intent.go:88-136` | `ValidateNativeOverrides` rejects `spec.networkPolicies.monitoring` overrides | Existing override fields must not be silently interpreted or dropped |
| `internal/policy/render.go:39-55` | `Render` dispatches only Cilium, Calico, and OVN; not-ready/unknown providers render nothing | Native provider gating is already fail-closed |
| `renderCilium`, `renderCalico`, `ovnEgressRules` | Rules cover managed peers, API server, and DNS only | Monitoring egress is absent in every supported adapter |
| `internal/controller/kubernaut_controller.go:1941-1999` | Controller builds intent, detects provider, renders, ensures, prunes, and patches conditions | The production wiring point is known and reusable |
| `internal/policy/types.go:138-149,306-321` | Cilium 1.19/1.20, Calico 3.31/3.32, and OpenShift OVN 4.19–4.22 are version-qualified gates | Monitoring capability must be subordinate to the selected native-provider gate |
| `internal/resources/common.go:366-400` | Generic URLs are explicit; OpenShift defaults are `thanos-querier.openshift-monitoring.svc:9091` and `alertmanager-main.openshift-monitoring.svc:9094` | Port must come from resolved URL/service, not a universal `9090/9093` constant |
| `api/v1alpha2/kubernaut_types.go:2483-2557` | Existing monitoring URL/enabled fields are provider/platform-aware | No immediate CRD field is required for this issue |
| `internal/resources/configmaps.go:1922-1938` | Agent tools consume the effective Prometheus and AlertManager URLs | Policy destination resolution must use the same effective view as Agent configuration |
| `internal/controller/provider_policy_integration_test.go` | Envtest already proves Cilium policy creation and readiness status | Extend the existing reconciliation journey rather than creating a parallel harness |
| `internal/resources/rbac.go` and controller tests | AlertManager API RBAC fix #468 is present | #468 solves authorization, not network-layer reachability; no RBAC relaxation should be bundled |

### Cilium and Calico capability evidence

The bounded provider spike used the current detector plus provider documentation:

* **Cilium:** official Cilium 1.19/1.20 policy documentation states that `CiliumNetworkPolicy` egress can use `toServices` by Service name/namespace or label selector, and that the Service selector is translated to endpoint identity. A Service without a selector is handled through EndpointSlice IP/CIDR selectors. For this issue’s least-privilege contract, accept selector-backed in-cluster Services and combine native `toServices` with an exact `toPorts` value for the effective backend port; selector-less or external endpoints remain explicit supplemental-policy cases. The current `schemaSupportsPolicy` check validates only top-level CNP fields, so nested `toServices`/`toPorts` support must be proven by the provider version/schema contract or a focused capability check before rendering it.
* **Calico:** official Calico 3.31–3.33 documentation states that `NetworkPolicy` destination `services` matching is supported only with the Kubernetes datastore and is ignored with the etcd datastore. Calico service rules automatically detect Service endpoint addresses and ports; explicit egress ports cannot be combined with a `destination.services` match. Therefore use native `destination.services` only when the URL resolves to an unambiguous single TCP Service port matching the configured URL (or the only TCP port when the URL omits one), allowing Calico to preserve Service DNAT/target-port behavior. Reject multi-port/ambiguous, selector-less, external, or non-Kubernetes-datastore destinations rather than synthesizing a potentially incorrect selector/target-port rule. The existing detector already discovers `DATASTORE_TYPE`/`--datastore-type` from ready `calico-node` DaemonSets through `calicoKubernetesDatastore`, and existing tests reject the etcd datastore.

### Current control-flow and failure behavior

`reconcileProviderPolicies` validates native overrides before discovery, builds the intent, detects one schema/version-qualified provider, renders native objects, ensures ownership-safe updates, prunes stale provider objects, and patches `ProviderDetected`/`ProviderPolicyReady`. A provider that is unavailable, ambiguous, schema-invalid, or version-invalid produces no policy objects and an observable false condition/event. Issue #492 must preserve that behavior when monitoring endpoints are absent, external, malformed, or not safely representable.

## 4. Bounded spikes and decisions

### Spike A — monitoring endpoint contract (completed)

**Question:** Which destinations and ports are authoritative?

**Evidence:** `MonitoringSpec` documents explicit generic-Kubernetes URLs and capability-gated OpenShift defaults. `effectivePrometheusURL` and `effectiveAlertManagerURL` are the same functions used to populate the Agent config. Existing tests use `https://prometheus.monitoring.svc:9090` and `https://alertmanager.monitoring.svc:9093`.

**Decision for Cilium/Calico:** Build monitoring intent from the same effective runtime view used by the Agent. Parse host and explicit port from each enabled URL. If a URL omits a port, resolve the referenced Service port rather than assuming `9090` or `9093`. For Cilium, resolve that Service port through `Service.spec.ports[].targetPort` to the effective numeric backend port; a named `targetPort` is usable only after an unambiguous backend/EndpointSlice port is resolved, otherwise the destination fails closed. Calico uses the Service match without an explicit egress port and therefore retains native Service/target-port translation. OpenShift fallback resolution and its exposed ports (`9091`/`9094`) are manually qualified in the OVN validation below, not treated as a generic-provider default. A TLS scheme changes no network-policy port.

### Spike B — provider API expressiveness (completed)

* **Cilium:** `CiliumNetworkPolicy` supports native endpoint/service-oriented egress. Prefer a service identity or service selector capability confirmed by the discovered CRD schema; do not fall back to a broad CIDR. The existing Cilium `toEntities: kube-apiserver` behavior remains unchanged. The enforcement spike showed that `toPorts` is evaluated against the backend port after Service identity selection: a `9090` Service targeting pod port `8080` was reachable only with `toPorts: 8080`, not `toPorts: 9090`.
* **Calico:** use the documented `projectcalico.org/v3 NetworkPolicy` destination `services` match under the Kubernetes datastore. Because egress Service matches automatically follow Service endpoint ports and cannot take an explicit egress port field, gate the monitoring destination to a single unambiguous TCP Service port matching the configured URL. Preserve the existing service-based API-server pattern; if the active Calico datastore or Service resolution is unsafe, mark the monitoring capability unavailable rather than silently broadening the rule.
* **OVN/OpenShift:** the AdminNetworkPolicy API has no native Service peer, so the adapter resolves selector-backed Services to pod peers and uses the effective backend port. The adapter rejects selector-less, headless, ExternalName, ambiguous, and unresolved destinations. The design was validated against the live API and dataplane on SNO-A/SNO-B, then manually qualified through the working-tree operator on SNO-A.

**Decision for this phase:** Add monitoring rules only after provider readiness and monitoring-Service capability validation. Use Cilium `toServices` + exact backend `toPorts`; use Calico `destination.services` only for a validated single-port Service, relying on Calico’s native endpoint-port resolution rather than adding an invalid egress port field; use OVN selector-derived pod peers + exact backend `portNumber` rules with an explicit monitoring-namespace deny boundary. Never use `0.0.0.0/0`, arbitrary endpoint CIDRs, or a raw Kubernetes `NetworkPolicy` as a portability workaround.

### Spike C — qualified native enforcement (completed)

The bounded Kind fixtures validated the provider contracts against actual dataplane enforcement, not only rendered YAML:

* **Cilium 1.20.2:** on the rootful Linux `helios08` fallback, Kind `v1.35.0` ran Cilium with a selector-backed `monitoring-single` Service on port `9090` targeting pod port `8080`, plus a distinct-backend `monitoring-blocked` Service. `toServices` with `toPorts: 9090` denied the intended Service and the distinct Service. The same policy with `toPorts: 8080` allowed `monitoring-single` and denied `monitoring-blocked`. This is the required production contract: resolve the effective backend port before rendering Cilium `toPorts`; do not copy the URL/Service port blindly. Rootless macOS failed only at BPF filesystem mounting, while the rootful startup and enforcement path succeeded.
* **Calico v3.31.4:** the existing Kind lane and the bounded enforcement fixture used the Kubernetes datastore. A `monitoring-single` Service on `9090` targeting `8080` was allowed through native `destination.services` with no explicit egress port, while the distinct-backend Service was denied. The fixture reported `datastore=kubernetes`, `allowed_single_service=PASS`, and `blocked_service=PASS`.

**Spike decision:** Cilium requires safe Service-port-to-backend-port resolution, including a fail-closed rule for ambiguous named target ports. Calico must use native `destination.services` without an explicit egress port after its Kubernetes-datastore and single-Service-port gates. The bounded spike itself did not claim the full Agent `get_metric_names`/`get_alerts` journey; that is covered by the implementation's qualified E2E test described below.

### Implementation validation record

The implemented Cilium/Calico slice now includes the qualified journey described above. The Kind suite provisions disposable Prometheus- and Alertmanager-shaped Services, configures the same URLs consumed by the Agent, calls the exact `get_metric_names` (`/api/v1/label/__name__/values`) and `get_alerts` (`/api/v2/alerts`) paths from the `kubernaut-agent` contract workload, and verifies a distinct monitoring Service remains denied. The suite deliberately uses its local contract image, so this is network-path qualification for the Agent workload rather than a claim about upstream Agent application internals.

Local macOS execution of that journey was attempted on 2026-10-06 but could not reach the tests: the Cilium lane hit a Podman Kind-node disk-quota failure during provider initialization, and the Calico lane timed out waiting for `calico-node` readiness. The rootful `helios08` fallback then ran the complete suite successfully with Podman: Cilium 1.20.2 passed 7/7 specs and Calico v3.31.4 passed 7/7 specs. Each run exercised the exact Agent `get_metric_names` and `get_alerts` paths against healthy fixtures and timed out against the unrelated monitoring Service; both Kind clusters were removed by the suite cleanup.

### OVN validation — live API, operator rendering, and dataplane validation

The two independent SNO environments are suitable for the follow-up slice:

* SNO-A and SNO-B both report OpenShift `4.22.16`, `OVNKubernetes`, a Ready `ovnkube-node`, and healthy Network Operator conditions.
* Both expose `AdminNetworkPolicy` and `BaselineAdminNetworkPolicy` at `policy.networking.k8s.io/v1alpha1`, with established CRD schemas. The API supports pod, namespace, node, and network peers plus `portNumber`; it has no native Service destination peer.
* The live OpenShift monitoring contract was confirmed on SNO-A and exercised on both clusters: `thanos-querier` exposes Service port `9091` to backend port `9091`, while `alertmanager-main` exposes Service port `9094` to EndpointSlice backend port `9095`.
* A disposable client in a separate namespace was selected by an `AdminNetworkPolicy` and allowed to reach the Thanos Service through its selector-derived pod peer on TCP `9091` and Alertmanager through its selector-derived pod peer on TCP `9095`. The same policy denied the unrelated `prometheus-k8s` Service. The probe returned HTTP `200` from Thanos, HTTP `401` from Alertmanager (connection reached the API), and a timeout for the denied Service on both SNO-A and SNO-B.

**OVN design and implementation:** an OVN monitoring adapter cannot preserve Service identity directly. It resolves a selector-backed Service to a pod peer and uses the effective backend port, including safe named `targetPort`/EndpointSlice resolution. Selector-less, headless, external, ambiguous, or unresolved destinations fail closed. The Agent-only policy is higher priority than the common namespace policy and ends with an explicit deny for the monitoring namespaces so unmatched monitoring traffic cannot fall through to the base policy.

The working-tree operator was built as a Linux/amd64 binary and run externally against SNO-A using only the remote kubeconfig path. In a disposable `kubernaut492-operator` namespace, an OVN-selected Kubernaut CR with OpenShift-default monitoring endpoints reconciled with `ProviderDetected=True/ProviderReady` and `ProviderPolicyReady=True`. The operator created `kubernaut-admin`, `default`, and `kubernaut-agent-monitoring`; the latter had priority `89`, the Agent selector, AlertManager backend port `9095`, Prometheus backend port `9091`, and the `deny-other-monitoring` rule. The generated Agent ConfigMap contained the expected `thanos-querier:9091` and `alertmanager-main:9094` URLs. A probe carrying the Agent selector reached the exact `get_metric_names` and `get_alerts` paths with HTTP `401` responses (network connectivity reached the authenticated APIs), while the unrelated `prometheus-k8s` path timed out. The temporary namespace, policies, CRDs, binary, and logs were removed afterward.

This is manual OCP assurance, not an automated OCP/OVN CI claim. The unit and envtest coverage exercises the resolver, renderer, and controller wiring; the SNO run exercises the production operator path and OVN dataplane manually.

### Spike D — RBAC and CRD impact (completed)

No CRD schema change is required because existing monitoring URLs and enabled flags remain the source of truth. The controller uses least-privilege read access to resolve referenced Services and named Cilium target ports through EndpointSlices. That production RBAC change is documented and generated, while the resolver/controller paths are covered by unit and integration tests. No write permissions or monitoring API permissions were added.

## 5. Alternatives considered

| Alternative | Result | Reason |
|---|---|---|
| Add monitoring rules directly in each renderer from hardcoded `9090/9093` and namespace guesses | Reject | Breaks OpenShift ports, explicit URLs, least privilege, and generic Kubernetes deployments |
| Add broad CIDR or raw Kubernetes `NetworkPolicy` fallback | Reject | Violates native-provider scope, can over-allow, and hides unsupported capability behind a false success |
| Add new CRD fields for Service names/ports | Defer/reject for #492 | Existing URL contract already identifies the destination; new fields create migration/defaulting and manifest impact without evidence they are needed |
| Calico selector/namespace-selector plus explicit port instead of `destination.services` | Reject for this slice | It requires targetPort/post-DNAT translation and can diverge from Service endpoint semantics; the enforcement spike confirmed that native Service matching tracks endpoint ports without an invalid `services`+ports combination |
| Resolve URLs once into provider-neutral Service/pod destination objects, then adapt per provider | **Recommended** | Preserves one traffic contract, enables exact ports, makes unsupported endpoint forms explicit, and keeps provider adapters bounded |
| Treat unresolved monitoring endpoints as a reconciliation error for the whole Kubernaut | Reject | Too disruptive; preserve current policy readiness semantics and surface a condition/event or explicit supplemental-policy requirement instead |

## 6. Implemented design (Cilium/Calico/OVN)

For the Cilium/Calico slice, the existing provider-neutral policy intent now carries an optional, normalized monitoring destination contract:

* client/component (`kubernaut-agent` at minimum; only add other clients with evidence of that client call path);
* destination kind (`Service`/selector-backed in-cluster, or external/unrepresentable);
* namespace, Service identity or stable pod selector, and one or more TCP ports;
* source evidence and a capability/diagnostic state so the renderer cannot silently drop a configured destination.

The implementation uses `MonitoringDestination` and `ResolveMonitoringDestinations`. The resolver consumes the controller’s effective monitoring view, parses and validates URL host/port, reads the required Service metadata, resolves Cilium’s effective numeric backend port (including only unambiguous named-port resolution from ready EndpointSlices), and produces deterministic output. The Agent is the only source component enabled by the #492 contract. Prometheus and AlertManager rules are independently gated by their corresponding `Enabled` state and endpoint availability. Cilium retains Service identity in `toServices` and uses the resolved backend port in `toPorts`; Calico retains Service identity in `destination.services` only after the single-port/datastore gate and does not add an explicit egress port. Calico support is detected independently from Cilium support.

For an endpoint that is external or cannot be safely represented by the selected provider, retain the existing explicit external/supplemental-policy contract: do not render an unsafe native allow, do not claim the provider policy is complete, and make the condition/event diagnostic actionable. The implementation reports `ProviderPolicyReady=False` with `MonitoringUnavailable` while preserving the native base policies.

For OVN, the resolver additionally requires a selector-backed Service and resolves each Service port to a numeric backend port. The renderer emits an Agent-only priority-89 `AdminNetworkPolicy` with selector-derived pod peers, exact TCP backend ports, and an explicit deny boundary for each monitoring namespace. The deny boundary prevents unmatched monitoring traffic from falling through to the lower-priority common policy. Selector-less, headless, `ExternalName`, ambiguous, and unresolved destinations fail closed.

## 7. TDD implementation plan

### RED — tests first

1. **Policy unit tests (`internal/policy/policy_test.go`):**
   * normalized intent carries Prometheus and AlertManager Service destinations and exact TCP ports;
   * Agent-only component receives both rules when both integrations are enabled;
   * `9090/9093` is covered explicitly;
   * OpenShift fallback coverage for `9091/9094` is manually qualified on SNO-A; no automated OCP lane is claimed;
   * disabled integration produces no corresponding rule;
   * Cilium renders `toServices` plus exact effective-backend `toPorts` for selector-backed Services, including a Service-port/targetPort translation case;
   * Calico renders native `destination.services` only for an unambiguous single-port TCP Service, and never combines an egress `services` match with an explicit port field;
   * malformed, external, selector-less, unresolved, or unsupported destinations fail closed and are observable, never converted to CIDR/all-egress;
   * deterministic ordering and no duplicate destination/port rules.
2. **Resolver tests:** URL parsing, default-port rejection/resolution, Service namespace/name resolution, single-port/multi-port ambiguity, numeric and named `targetPort` resolution for Cilium and OVN, selector-less/ExternalName behavior, Calico Service-port validation, and explicit provider capability handling. Use real business logic; mock only Kubernetes API reads. OpenShift endpoint mapping is manually qualified on SNO-A rather than claimed as automated coverage.
3. **Controller envtest RED:** drive `Deploying` reconciliation with monitoring Services and Cilium/Calico provider CRD fixtures; assert native objects and `ProviderPolicyReady`. Add negative cases for missing Service/capability and assert no unsafe policy plus false/diagnostic condition/event.
4. **E2E RED:** on qualified Cilium and Calico environments, deploy healthy monitoring Service fixtures/endpoints and a probe/Agent workload; verify `get_metric_names` and `get_alerts` reach `9090/9093` while unrelated egress remains denied. Implemented and executed in `test/e2e/kind/`; the equivalent OVN journey is manually qualified on SNO-A.

### GREEN — minimal implementation and wiring

1. Add the smallest provider-neutral destination model and resolver needed by the failing unit tests.
2. Wire resolution into `KubernautReconciler.reconcileProviderPolicies` before `policy.Render`; do not create a parallel reconciliation path.
3. Add adapter rules for Cilium, Calico, and the approved OVN selector-derived pod-peer design using only capabilities proven by detection/schema/datastore checks.
4. Add only required Service (and approved EndpointSlice, if unavoidable) read RBAC; regenerate manifests if RBAC markers change.
5. Ensure unsupported/unresolved cases use explicit status/event diagnostics and never produce broad fallback objects.
6. **Checkpoint W:** verify the resolver and renderer are called from reconciliation, the envtest exercises that path, stale policy pruning still works, and no new resource builder is orphaned.

### REFACTOR — production quality

1. Consolidate URL/service resolution and provider capability validation without introducing unrelated types/components.
2. Apply deterministic ordering, bounded allocations, wrapped lowercase errors, structured reconciliation logs including generation/resourceVersion, and ownership-safe updates.
3. Review Cilium and Calico schemas, Calico datastore evidence, OVN/OpenShift API behavior, and supported release gates; document the manual OCP qualification boundary.
4. Run build, lint, unit, envtest/integration, qualified E2E, and generated-manifest checks. Build, lint, unit, integration, security-traceability, CI-boundary, pyramid, and generated-manifest checks pass; the Cilium and Calico Agent journeys each pass 7/7 on the rootful fallback. OVN has manual SNO evidence but no automated OCP regression lane. The #489 plan file and unrelated work remain unchanged.

## 8. Wiring manifest

| Component | Production entry point | Wiring location | IT proof |
|---|---|---|---|
| Monitoring destination resolver | Native provider policy reconciliation | `internal/controller/kubernaut_controller.go`, `reconcileProviderPolicies` | Envtest creates configured Services and observes rendered destinations |
| Provider-neutral monitoring intent | `policy.Render` input | `internal/policy/intent.go` plus controller construction | Unit test proves both monitoring destinations survive normalization |
| Cilium monitoring egress | `Render` → `renderCilium` | `internal/policy/render.go` | Cilium envtest asserts `toServices`, exact `toPorts`, and selector-backed Service behavior |
| Calico monitoring egress | `Render` → `renderCalico` | `internal/policy/render.go` | Calico envtest/schema/datastore fixture asserts native Service identity, single-port gate, and no egress `services`+ports combination |
| OVN monitoring egress | `Render` → `renderOVN` | `internal/policy/render.go` plus monitoring resolver/controller wiring | Envtest inspects the Agent policy; SNO-A manually exercises operator rendering and enforcement |
| Unsupported endpoint observability | `patchProviderPolicyStatus` and event path | `internal/controller/kubernaut_controller.go` | Negative envtest asserts false condition and no broad policy |

## 9. Pyramid-invariant test matrix

| Tier | Scope | Business-level assertion | Infrastructure |
|---|---|---|---|
| Unit | Intent/resolver | Configured Agent monitoring contract is normalized exactly; invalid/external destinations are fail-closed | None except mocked API reader |
| Unit | Cilium/Calico renderers | Each current native adapter preserves Prometheus and AlertManager TCP egress while default deny remains | None |
| Integration | Controller deployment reconciliation | The reconciler creates/updates the provider-native policy, reports readiness, prunes stale objects, and exposes diagnostics | envtest CRDs + Service fixtures |
| Integration | Generic Kubernetes capability gates | Generic explicit URLs work; unset generic URLs do not inherit OpenShift defaults; Cilium/Calico capability gates are observable | envtest provider/API fixtures |
| Qualified E2E | Real Cilium/Calico provider enforcement | Agent `get_metric_names` and `get_alerts` succeed against healthy in-cluster endpoints; an unrelated destination remains denied | Rootful Kind fallback on `helios08`: Cilium 1.20.2 7/7 and Calico v3.31.4 7/7 |
| Manual OCP/OVN qualification | Native policy and operator enforcement | Selector-derived pod peers and effective backend ports allow Thanos/Alertmanager and deny an unrelated monitoring Service; operator-generated Agent policy reports readiness | Disposable SNO-A/SNO-B probes plus working-tree operator run on OpenShift 4.22.16 SNO-A |
| Packaging/regression | #489 boundary | Operator-only Helm output contains no application NetworkPolicy; runtime policy is operator-owned | Helm/render smoke test and repository diff |

The pyramid invariant is non-negotiable: unit tests prove policy logic, envtest proves controller wiring, and qualified E2E proves actual enforcement and the user journey. A renderer-only green build is not sufficient.

## 10. Control-objective traceability

These controls are business acceptance criteria, not merely comments or implementation details. The exact control IDs must be mapped to the project’s adopted compliance catalog during implementation review if a catalog revision differs.

| Objective | Issue #492 interpretation | Required evidence |
|---|---|---|
| **FedRAMP AC-4 / SC-7 (boundary and information-flow enforcement)** | Agent egress is an explicit allowlist to configured monitoring Services/ports; no broad CIDR or raw-policy bypass; unsupported destinations fail closed | Unit rule assertions, provider schema/capability tests, envtest object inspection, E2E denied unrelated destination |
| **FedRAMP AU-2/AU-12 (auditable events)** | Provider readiness, unsupported endpoint, resolution failure, and policy reconciliation outcomes are observable with structured logs/status/events and generation/resourceVersion | Controller tests inspect conditions/events; log assertions or documented structured-log evidence |
| **FedRAMP CM-6 / CM-8 (configuration and inventory)** | Effective URLs, resolved Service identities, provider/version/schema gates, and managed policy ownership are deterministic and discoverable | Resolver tests, status evidence, ownership/pruning tests, generated RBAC/manifests |
| **SOC 2 CC6.1 / CC6.6 (logical access and restriction)** | Least-privilege component-specific monitoring access; only Agent clients get Agent rules; no accidental cross-namespace/all-egress access | Per-component unit tests, selector/port assertions, RBAC diff review, E2E negative path |
| **SOC 2 CC7.2 (monitoring and anomaly response)** | Timeouts and unsupported provider capability cannot be silent; readiness and reconciliation diagnostics support operational response | Negative envtest, event/status assertions, structured logs |
| **OWASP ASVS V4.1/V4.2 (access control and least privilege)** | Every destination, namespace, selector, and port is validated and constrained; authorization is deny-by-default and no unsafe fallback is emitted | Input/resolution tests, malformed URL/Service tests, provider-specific exact rule tests |
| **OWASP ASVS V5.1 (validation/sanitization)** | URL host/port, Service metadata, and Cilium/Calico provider capability are validated before policy generation; platform fallback is a later gate | Resolver unit tests and schema/datastore-gate tests |
| **OWASP ASVS V7.1/V7.2 (error handling/logging)** | Errors are wrapped, observable, non-secret-bearing, and do not turn into permissive policy | Error-path tests, log/status review, no-secret assertions |

## 11. Risks and approval gates

| Risk | Mitigation / gate |
|---|---|
| URL host does not map to a Kubernetes Service | Require explicit external/supplemental-policy path; fail closed for native rendering; test it |
| Service has no stable selector, is `ExternalName`/headless, exposes ambiguous ports, or has an unresolved named `targetPort` | Do not synthesize a broad peer; fail closed with an explicit capability diagnostic for Cilium, Calico, or OVN |
| Cilium/Calico installed in a mode where service identity is unsupported | Detect capability/schema/datastore evidence; do not render a false-success rule |
| OCP/OVN automated regression coverage is unavailable | Manual SNO-A/SNO-B validation covers selector-derived pod peers, backend ports, operator rendering, and the Agent monitoring journey; keep the CI claim bounded to unit/envtest coverage |
| OpenShift service port differs from container port | Resolve the Service/EndpointSlice backend port (`9091` for Thanos and `9095` for AlertManager on the qualified SNOs); never assume the Service port is the backend port |
| New RBAC is broader than necessary | Prefer `get` for named Services; justify any `list/watch` or EndpointSlice access; run RBAC audit |
| CRD or generated manifests unexpectedly change | `make manifests generate` was run; CRDs are unchanged and only the intended EndpointSlice read RBAC was generated |
| Semantic Engram index remains unavailable | Rebuild/repair Engram index or escalate before relying on semantic search for further design work; this plan records the failure rather than silently replacing it |

**Review gates before commit/release:**

1. Review the implemented resolver/native-adapter approach and confirm broad/raw fallback remains rejected.
2. Confirm the implemented status semantics for an enabled but unrepresentable external monitoring endpoint: an explicit diagnostic condition/event without claiming native policy completeness.
3. Review the Service/EndpointSlice RBAC change and its exact read-only access scope.
4. Confirm the Cilium/Calico RED test cases and qualified enforcement E2E prerequisites. For OVN, retain the manual SNO evidence boundary because CI cannot reproduce the qualified environment.
5. Complete the Cilium/Calico qualified provider E2E run when a suitable environment is available; do not amend the existing #489 commit.

## 12. Confidence assessment

**Confidence: 97% for the implemented Cilium/Calico policy behavior, controller wiring, and qualified Agent journeys; 95% for the implemented OVN resolver/renderer and manually tested OpenShift 4.22.16 operator/dataplane path; 94% for release-complete provider qualification because OCP/OVN has no automated CI lane.**

The issue scope, current code gap, controller wiring, existing monitoring endpoint contract, Cilium `toServices`/effective-backend `toPorts` behavior, Calico Kubernetes-datastore requirement and egress Service-port constraint, OVN peer expressiveness, provider gates, fail-closed lifecycle, and test harness are directly evidenced. The Cilium and Calico rootful Kind runs validate the complete Agent journey and unrelated-destination denial. The SNO probes validate OVN `v1alpha1` peer expressiveness and Service/backend-port enforcement on two clusters, and the SNO-A operator run validates production rendering plus the Agent monitoring journey. The lack of an automated OCP lane remains an explicit release-qualification limitation.

No commit or PR has been created. The implementation remains available for review, with Cilium/Calico provider enforcement qualified on the rootful fallback and OVN evidence explicitly bounded to manual OpenShift qualification.

# ADR-PLATFORM-001: Platform-Neutral Operator and Bounded Provider Policy Contract

**Status**: Accepted — implementation complete on `fix/488-platform-neutrality`; live provider/OpenShift qualification remains environment-dependent
**Decision Date**: 2026-10-01
**Version**: 1.0
**Confidence**: 96%
**Deciders**: Kubernaut Operator Team
**Applies To**: `api/v1alpha2`, `internal/controller/`, `internal/resources/`, provider-policy adapters, Helm packaging, OLM packaging, and platform/provider test lanes

**Related Issues**:

- [kubernaut-operator#486](https://github.com/jordigilh/kubernaut-operator/issues/486) — parent platform-neutrality and Helm-bootstrap initiative
- [kubernaut-operator#487](https://github.com/jordigilh/kubernaut-operator/issues/487) — platform/provider support documentation
- [kubernaut-operator#410](https://github.com/jordigilh/kubernaut-operator/issues/410) — platform-neutral policy work
- [kubernaut-operator#298](https://github.com/jordigilh/kubernaut-operator/issues/298) — configurable monitoring endpoints
- [kubernaut-operator#342](https://github.com/jordigilh/kubernaut-operator/issues/342) — provider-specific policy enforcement coverage
- [kubernaut-operator#375](https://github.com/jordigilh/kubernaut-operator/issues/375) — policy and platform integration follow-up

**Related Design**:

- [`ADR-CRD-001-v1alpha2-redesign.md`](ADR-CRD-001-v1alpha2-redesign.md) — historical v1alpha2 redesign; this ADR supersedes its conversion-webhook choice for the clean-break rollout described here
- [`ADR-PLATFORM-001-implementation-plan.md`](ADR-PLATFORM-001-implementation-plan.md) — gated implementation and TDD plan
- [`docs/installation/06-platform-support.md`](../installation/06-platform-support.md) — user-facing compatibility contract

---

## Context

The operator currently owns the Kubernaut lifecycle but has OpenShift assumptions in the process that starts the controller, resource builders, RBAC, TLS, monitoring, exposure, samples, and tests. In particular:

- `cmd/main.go` and the controller register and watch OpenShift API types.
- `KubernautReconciler.Reconcile` historically read `v1alpha2` and converted it into an internal `v1alpha1` view before every phase; the clean-break implementation removes that compatibility path.
- Services, admission webhooks, trust bundles, and Routes assume the OpenShift service-CA, router CA, or Route API.
- Monitoring defaults assume `openshift-monitoring`, Thanos Querier, and OpenShift `AlertmanagerConfig`.
- The current network-policy path renders raw Kubernetes `NetworkPolicy` objects and can use API-server IP/CIDR values.
- There is no repository-owned Helm bootstrap chart, although Helm and the operator can currently be used as competing deployment paths.

The target is a common lifecycle manager for generic Kubernetes and OpenShift, not a universal CNI controller. The operator must submit application policy intent to an already-installed provider when the provider contract is positively identified. The provider or platform operator remains responsible for CRD installation, reconciliation, enforcement, and provider upgrades.

This is a clean-break API redesign. The current `kubernaut.ai/v1alpha2` shape is the API being taken forward; `v1alpha1` compatibility and conversion are not part of this initiative. Existing v1alpha1 installations require a documented migration/recreate boundary rather than a partially maintained conversion path.

---

## Decision

### 1. Keep portable lifecycle behavior in the core

The core reconciler must be able to start and reconcile on a Kubernetes cluster that has none of the following APIs or operators:

- `config.openshift.io`
- `route.openshift.io`
- OpenShift service-CA and ingress operators
- `monitoring.coreos.com`
- Cilium, Calico, or OVN policy CRDs
- cert-manager or another certificate operator

OpenShift, monitoring, certificate, exposure, and policy-provider features are optional capability adapters. An absent optional API is a capability result, not a controller-startup failure or a permanent watch retry loop.

Core behavior remains equivalent across supported platforms: singleton validation, CR lifecycle, migration jobs, Deployments, Services, ConfigMaps, Secrets, service accounts, workload RBAC, PDBs, status conditions, finalizers, cleanup, and configured external dependencies.

### 2. Take `v1alpha2` forward as a clean-break API

The implementation will:

- serve and store only `kubernaut.ai/v1alpha2` for the redesigned CRD;
- migrate controller and resource-builder signatures to `api/v1alpha2.Kubernaut`;
- remove the v1alpha1 conversion webhook, conversion stanza, conversion registration, and v1alpha1 reconcile view;
- remove the v1alpha1-only migration code after all production references are migrated;
- publish an upgrade document that explicitly requires export, transformation, delete/recreate, or another operator-approved migration procedure for existing v1alpha1 objects;
- avoid silently accepting an old object and pretending that it was migrated.

The existing CRD redesign ADR's hub-and-spoke conversion decision is therefore historical for this work. It must not be implemented as part of the platform-neutral rollout.

### 3. Support a bounded provider matrix

The initial provider contract is deliberately narrow:

| Provider | Provider release range | Policy resources | Platform/Kubernetes contract |
|---|---|---|---|
| Cilium | 1.19.x–1.20.x | `cilium.io/v2` `CiliumNetworkPolicy`; `CiliumClusterwideNetworkPolicy` only where the common intent requires cluster scope | Cilium 1.19: Kubernetes 1.32–1.35; Cilium 1.20: Kubernetes 1.33–1.36 |
| Calico | 3.31.x–3.32.x | `projectcalico.org/v3` `NetworkPolicy`; `GlobalNetworkPolicy` only where the common intent requires cluster scope | Calico 3.31: Kubernetes 1.32–1.35; Calico 3.32: Kubernetes 1.34–1.36 |
| OpenShift OVN-Kubernetes | OpenShift 4.19–4.22 | `policy.networking.k8s.io/v1alpha1` `AdminNetworkPolicy` and `BaselineAdminNetworkPolicy` | OpenShift must positively identify `OVNKubernetes`; OpenShift 4.23 is unsupported initially |

The initial matrix does **not** include Antrea, other Cilium/Calico releases, upstream `policy.networking.k8s.io/v1alpha2` `ClusterNetworkPolicy`, or OpenShift 4.23+. Expansion requires a new compatibility review, fixtures, qualified test environments, and enforcement evidence.

### 4. Detect a provider using API plus active-installation evidence

`auto` is the default selection mode. Detection must combine:

1. API discovery for the expected provider GVK and the required version/schema; and
2. evidence that the provider is actively installed and responsible for the cluster, such as provider-owned DaemonSets, pods, labels, or platform configuration.

CRD presence alone is never sufficient. A stale CRD, a second installed provider, or a provider that is outside the supported release range must not result in policy creation.

An explicit provider override remains available for an ambiguous generic Kubernetes cluster, but an override does not bypass the runtime GVK/schema and release-range checks. An override for an unavailable or incompatible provider produces no policy resources and an explicit status condition.

The detector returns a typed result containing provider, detected release/platform, expected GVKs, capability evidence, and a diagnostic. It does not create, patch, or delete provider CRDs or provider operators.

### 5. Render provider-native policy intent; never install or fall back

The current Kubernaut traffic matrix remains the source of policy intent: API-server access, DNS, AuthWebhook admission, inter-service dependencies, PostgreSQL/Valkey, monitoring, LLM/IdP, Fleet/MCP, and configured external webhooks.

Each adapter translates that intent into its provider-native resources. The adapter must:

- render only the common, version-qualified field subset;
- use provider-native identity/entity semantics for API-server access where the provider supports them;
- never use install-time or stale API-server CIDRs as the API-server identity;
- refuse the affected policy capability and report why when a required target cannot be expressed safely;
- never install provider CRDs, CNI operators, or platform operators;
- never use raw Kubernetes `NetworkPolicy` as a fallback for a missing, ambiguous, unsupported, or out-of-range provider;
- use deterministic names, labels, hashes, and cleanup semantics;
- use owner references for namespaced provider resources where valid, and labels plus finalizer-driven pruning for cluster-scoped resources where cross-scope owner references are invalid;
- avoid overwriting provider/platform-owned policy objects.

When no supported provider is present, a plain generic Kubernetes or Kind installation may continue without provider policy resources. The status must make that fact explicit; the operator must not claim that provider policy enforcement is active. An explicit incompatible provider request is a configuration/integration failure and must be surfaced separately from the normal no-provider profile.

### 6. Make platform integrations optional and observable

The platform capability layer will provide these independent capabilities:

- **Exposure**: portable Kubernetes `Ingress` with explicit class, host, TLS Secret, annotations, and timeout/redirect contract; OpenShift `Route` remains an optional adapter. Gateway API is a later optional capability, not a prerequisite for the first generic path.
- **TLS and trust**: administrator-supplied Secrets/CA references and a documented cert-manager (or equivalent) integration for generic Kubernetes; OpenShift service-CA/router-CA integration remains optional. No generic cluster may rely on an OpenShift annotation being interpreted.
- **Admission webhooks**: generic serving-certificate and CA-bundle management must work without service-CA; cert-manager or an explicit administrator-managed path is supported. Development fallback must be explicit and must not silently create an insecure production installation.
- **Monitoring**: Prometheus and AlertManager endpoints are explicit or positively discovered. `ServiceMonitor`/`PrometheusRule` are rendered only when the APIs exist and the integration is enabled. OpenShift `AlertmanagerConfig` and OpenShift monitoring RBAC are optional.
- **OpenShift cluster settings**: APIServer TLS profile, ingress domain, Routes, service-CA, and OpenShift monitoring are read only through an optional OpenShift adapter.

Missing optional integrations produce structured logs, events, and status conditions with the observed generation. They do not cause a generic Kubernetes manager to fail while setting up an unavailable typed watch.

#### 6.1 Certificate, trust, and webhook contract

TLS is treated as several related but independent contracts. A single
`tls.enabled` switch is insufficient because the manager webhook, the managed
AuthWebhook, east-west service traffic, and external exposure have different
certificate identities, consumers, and rotation ordering.

The final v1alpha2 field names remain an approval-gated API decision. The
logical contract is fixed as follows:

| Surface | Required material | Generic Kubernetes / Kind source | OpenShift source adapter |
|---|---|---|---|
| Operator manager webhook and singleton admission endpoint | Serving leaf keypair plus the CA bytes placed in the bootstrap webhook configuration | Administrator-managed Secret and CA, an explicitly configured cert-manager `Certificate`, or an explicitly selected development self-signed bootstrap | OLM/administrator-provided serving material; service-CA may be used only if the adapter can satisfy the manager's serving and CA-bundle contract |
| Managed AuthWebhook Service and admission configurations | AuthWebhook leaf keypair, Service DNS SANs, and the exact issuing CA in both mutating and validating webhook `caBundle` fields | The same administrator, cert-manager, or explicit development self-signed sources | Service-CA serving certificate plus service-CA CA injection |
| Inter-service TLS | A dedicated internal CA, leaf keypairs for every TLS server, and a published public CA bundle | Administrator-managed or cert-manager-managed Secrets/ConfigMaps, or the explicit development self-signed bootstrap | Service-CA leaves and a service-CA-backed trust bundle; router CA is added only for Route clients |
| External Ingress/Route | Certificate whose SANs cover the configured external host, plus the referenced Ingress TLS Secret or Route termination contract | Administrator-managed or cert-manager-managed Ingress Secret; no implicit hostname or router CA assumption | Route termination and optional router/default-ingress CA integration |
| Outbound database, Valkey, monitoring, IdP, LLM, and webhook trust | Explicit CA and, where required, client certificate/key references | CR references to administrator/secret-manager material; system trust is not silently replaced | Platform CA references may be discovered by the OpenShift adapter |
| DataStorage audit integrity | Signing key and audit-HMAC material, separate from network CAs and leaf certificates | Explicit Secret references or a separately approved provisioner | Same logical contract; OpenShift service-CA must never be treated as audit-signing material |

The supported source modes have these semantics:

1. **Administrator-managed**: the operator validates referenced Secrets and
   ConfigMaps, including required keys, certificate/CA consistency, and
   service/external SANs. It does not overwrite or delete those objects.
2. **Cert-manager**: cert-manager must already be installed and an issuer
   must be explicitly configured or discovered according to the approved API
   contract. The operator/chart may create `Certificate` resources, but never
   installs cert-manager or assumes that its CRDs exist. cainjector or an
   equivalent reconciler must populate webhook `caBundle` fields.
3. **Development self-signed**: an explicit, namespace-scoped bootstrap
   provisioner creates a dedicated CA and leaves. It is intended for Kind and
   development only, must expose its non-production status, and must not be a
   silent fallback when a production source is unavailable. The CA private key
   is restricted to the provisioner; workloads receive only their leaf keypair
   and the public CA bundle.
4. **OpenShift service-CA/router-CA**: an OpenShift-only adapter may use the
   service-CA and router/default-ingress CA contracts. Generic code must not
   emit or depend on OpenShift annotations, fixed ConfigMap names, or Route
   trust behavior.

Provisioning and rotation follow an ordered contract: establish the CA,
publish the public trust bundle, issue or obtain leaves, verify SANs and
expiry, then roll workloads and update webhook `caBundle` values. During CA
rotation the implementation must either support an overlap window or perform
a coordinated, observable restart; it must never delete the only trusted CA
before all consumers have moved. Missing, malformed, expired, or not-yet-ready
material produces explicit certificate/trust conditions and prevents a
fail-closed webhook from being advertised as ready. There is no generic
fallback from failed TLS provisioning to plaintext HTTP.

Webhook CA injection is source-specific: cert-manager/cainjector for a
cert-manager source, an operator/bootstrap patch for administrator-managed or
self-signed material, and service-CA injection for the OpenShift adapter. A
generic cluster must never receive an OpenShift
`service.beta.openshift.io/inject-cabundle` annotation as its only CA
mechanism.

### 7. Move Helm bootstrap ownership into this repository

The repository will contain two independent Helm charts with separate release
and ownership boundaries:

1. **`kubernaut-operator`** is the production-ready distribution. It installs
   only the operator bootstrap: operator Deployment, CRDs, bootstrap RBAC, and
   manager-webhook/certificate prerequisites. It will **not** render or create
   a `Kubernaut` CR.
2. **`kubernaut-dependencies`** is an optional, explicitly non-production
   development/demo/CI convenience chart for single-replica, PVC-backed
   PostgreSQL and Valkey. It is not a production database HA, backup, or
   upgrade solution, uses pre-created credential Secrets by default, and is
   never a dependency of `kubernaut-operator`.

Production installations are expected to install only `kubernaut-operator`
and use administrator- or platform-managed PostgreSQL and Valkey. The user or
a GitOps controller applies the singleton CR separately after the CRD and
manager are ready; Kubernetes CRD validation and the operator's
admission/reconcile path remain the only application-configuration contract.

Neither chart may reproduce the application configuration schema from the
upstream direct-workload chart in its `values.yaml` or `values.schema.json`.
The operator chart validates only bootstrap inputs such as operator image,
namespace/release settings, manager certificate source, and packaging options.
The dependencies chart validates only its own infrastructure inputs and
credential/TLS references; it does not translate values into a Kubernaut CR.

The migration is staged:

1. add and test `kubernaut-operator` as an operator-and-CRD bootstrap path;
2. add and test `kubernaut-dependencies` as an independent, explicitly
   non-production convenience path;
3. document the separate `helm install` → manager readiness → optional
   dependencies/BYO prerequisites → user/GitOps CR apply sequence;
4. add migration/adoption only if a separate readiness-gated design proves it safe;
5. deprecate direct Helm ownership of Kubernaut workloads rather than silently adopting them.

OLM remains a supported OpenShift packaging path. It is not the only installation path.

#### 7.1 Bootstrap and runtime ownership

The ownership boundary is explicit:

| Resource group | Helm/OLM/bootstrap owner | Runtime owner |
|---|---|---|
| Operator Deployment, ServiceAccount, manager RBAC, manager webhook configuration, and manager serving-certificate prerequisites | `kubernaut-operator` Helm chart or OLM, with the selected certificate source | Operator process consumes them; it does not adopt a second copy |
| Kubernaut CRD installation and upgrade artifact | The selected packaging path, under one documented CRD-upgrade contract | The operator does not apply a competing copy of its own CRD |
| Singleton `Kubernaut` CR and application configuration | **Not rendered by Helm/OLM**; applied by the user or GitOps controller | Kubernetes CRD validation, admission webhook, and Kubernaut reconciler |
| Kubernaut workloads, runtime Services, ConfigMaps, Secrets, workload RBAC, PDB/HPA, Ingress/Route, monitoring objects, and provider policy objects | Never rendered as duplicate workload templates by either chart | Kubernaut operator, subject to platform/provider ownership below |
| Optional PostgreSQL and Valkey StatefulSets, Services, PVCs, and chart-owned development TLS material | `kubernaut-dependencies` only, when explicitly installed | Dependencies chart; the operator only references the resulting endpoints and Secrets |
| Production PostgreSQL/Valkey, external credentials, policy input ConfigMaps, and external PKI material | Administrator or an independently selected infrastructure/secret-management platform | Referenced by the operator; not implicitly adopted |
| Cilium, Calico, OVN, monitoring, cert-manager, and ingress-controller CRDs/operators | Their respective platform/operator owners | Kubernaut submits only supported objects at its ownership boundary |

Certificate prerequisites need the same distinction: `kubernaut-operator` may
own the operator manager's serving-certificate resources, while the operator
owns certificate objects and runtime trust artifacts that are part of a
reconciled `Kubernaut` instance. `kubernaut-dependencies` may own only the
database/cache server material it creates. A Secret or ConfigMap must not be
co-owned by either chart and the operator merely to make a clean install
appear to work.

---

## Helm parity and gap classification

The `../kubernaut` `origin/main` chart was compared at baseline
`fa8601bd2963ddb29b7bb8499def8289007c353e`. It is a direct application-workload
chart, whereas the repository-owned chart defined by this ADR is an operator
bootstrap chart. Its templates are therefore a behavior inventory, not a list
of resources that the new chart should copy. In particular, copying the chart's
workload templates would recreate the two-owner failure this ADR is intended to
avoid.

| Surface | `../kubernaut` Helm behavior | Current operator finding | Classification and required action |
|---|---|---|---|
| **Certificates, trust, and webhook CA injection** | `tls.mode` supports `hook`, `cert-manager`, and `manual`. Hook mode generates an AuthWebhook CA/leaf, inter-service CA/leaves, DataStorage signing certificate, and audit-HMAC key; a post-install hook patches webhook `caBundle`. Cert-manager mode creates a dedicated internal CA/Issuer, service leaf `Certificate` objects, AuthWebhook certificate, DataStorage signing certificate, and synchronizes the CA into the mounted ConfigMap. | `internal/resources/services.go` relies on the OpenShift serving-certificate annotation; `trustbundle.go` reads fixed OpenShift service-CA and ingress ConfigMaps; `webhooks.go` relies on the OpenShift CA-injection annotation; `cmd/main.go` skips the operator singleton/conversion webhook when the file-mounted certificate is absent. There is no generic certificate source, rotation contract, or generic `caBundle` path. | **P0 portable-core gap.** Define one explicit generic TLS contract covering four separate surfaces: operator manager webhook, managed AuthWebhook and its admission configurations, inter-service leaf certificates/CA trust, and external Ingress TLS. Support administrator-managed Secrets, cert-manager when discovered/configured, and an explicit self-signed bootstrap mode for Kind/development; never silently downgrade a production webhook or mTLS path. Include DataStorage signing material and external database/Valkey CA/client references in the trust contract. |
| **Generic exposure** | Opt-in `networking.k8s.io/v1` Ingress for Gateway, APIFrontend, and Console with class, host, annotations, TLS Secret, and redirect/port-aware console settings. Service type can be `ClusterIP`, `NodePort`, or `LoadBalancer`. | The operator emits OpenShift Routes and uses the OpenShift ingress domain for Console redirect derivation. Services have no generic Ingress contract and default to ClusterIP without the chart's service-type/NodePort settings. | **P0 portable-core gap.** Add a capability-gated Ingress adapter and an explicit exposure status. Keep Route as an OpenShift adapter; do not infer generic hostnames from OpenShift configuration. Decide whether service type/NodePort belongs in `v1alpha2` or remains a chart/platform concern. |
| **Monitoring and alerting** | Prometheus and AlertManager URLs/CAs are explicit. ServiceMonitor and PrometheusRule are opt-in and render only when the Prometheus Operator API is available; OpenShift monitoring is not a prerequisite. Alert thresholds and additional rule labels are configurable. | `v1alpha2` has endpoint fields, but current defaults and config paths still assume OpenShift/Thanos. Reconciliation gates the whole monitoring builder on ServiceMonitor discovery, does not independently gate every resource kind, and currently constructs an AuthWebhook ServiceMonitor even though the service has no metrics port. OpenShift AlertmanagerConfig and monitoring RBAC are in the core path. | **P0 portable-core gap.** Make endpoint discovery/configuration explicit, gate ServiceMonitor, PrometheusRule, and AlertmanagerConfig independently, report unavailable/disabled status, remove invalid no-metrics targets, and retain OpenShift monitoring only in an adapter. Threshold/label parity is a separate API review item. |
| **Network policy** | The chart creates raw Kubernetes `NetworkPolicy` resources for every component and documents live lookup/static overrides for API-server endpoint CIDRs and broad external CIDRs. | `internal/resources/networkpolicies.go` creates the same class of raw policies and resolves API-server IPs/CIDRs. | **Intentional divergence, not a porting target.** Replace both paths with common traffic intent plus Cilium, Calico, and OVN-native adapters. Do not preserve the chart's raw policy, static API-server CIDR, or `0.0.0.0/0` fallback as a portability solution. |
| **Kubernaut CRDs and upgrade ownership** | The chart ships application CRDs and has a pre-upgrade server-side-apply hook because Helm does not upgrade CRDs automatically. | The operator currently calls `EnsureCRDs` during migration and the repository has no Helm bootstrap chart. | **P0 ownership gap.** The repository chart and OLM must have an explicit Kubernaut-CRD ownership/upgrade contract. Do not make the operator and Helm own the same CRD lifecycle. Provider/CNI CRDs remain entirely outside both ownership paths. |
| **Database migration and install/upgrade hooks** | Helm runs a pre-install/pre-upgrade database migration Job, a TLS certificate hook, an inter-service CA synchronization hook, and the CRD upgrade hook. Hook ordering is part of the chart's startup contract. | The operator has a migration phase/Job and runtime reconciliation ordering; its migration path also contains CRD handling. | **P0 portable-core/ownership gap.** Keep application migration in the operator's lifecycle, give bootstrap packaging one explicit CRD-upgrade owner, and do not duplicate per-instance migration/TLS/CA-sync Jobs when the operator owns the `Kubernaut` runtime. Hook readiness and uninstall behavior need clean-install tests. |
| **Runtime workload ownership** | The upstream chart directly renders Deployments, Services, ConfigMaps, Secrets, RBAC, PDBs, HPAs, NetworkPolicies, and hooks for all Kubernaut services. | The operator already reconciles those workload resources and performs cleanup/finalization. | **Intentional divergence.** The new chart renders only operator bootstrap resources, CRDs, and manager certificate/webhook prerequisites. It must not render upstream service workloads, application ConfigMaps, or a `Kubernaut` CR, and must not adopt existing Helm-owned workloads in the first rollout. |
| **Stateful infrastructure** | PostgreSQL and Valkey can be deployed as single-replica, PVC-backed convenience workloads with optional BYO endpoints. The chart also owns their TLS material in the bundled path. | The operator accepts BYO PostgreSQL/Valkey connection and credential references; it does not deploy databases. | **Intentional ownership boundary.** Do not move stateful database ownership into the operator for parity. The bootstrap chart may document BYO prerequisites and may add a separately scoped, non-production infrastructure dependency only after approval; any such dependency owns only its own resources and passes references to the CR. |
| **Policy and other input ConfigMaps** | Helm can materialize Rego/routing content from `--set-file` or reference pre-existing ConfigMaps, with reserved-name/ownership checks. | The operator consumes policy ConfigMap references from the CR and owns generated runtime ConfigMaps; policy input materialization is not a Helm capability in this repository. The operator's historical default name also differs from the upstream chart's AIAnalysis policy name. | **P1 application-configuration gap, not a bootstrap-chart feature.** The user/GitOps layer must pre-create or otherwise manage input ConfigMaps and reference them from the CR. The operator and bootstrap chart must not co-own runtime or input ConfigMaps merely to reproduce Helm's `--set-file` UX. |
| **Scaling, disruption, and scheduling** | Per-service replicas, resources, pod/container security context, node selection, tolerations, affinity/topology spread, opt-out PDBs, and opt-in DataStorage/APIFrontend HPAs are configurable. | The operator exposes resource requirements and applies common hardening/soft anti-affinity/PDB defaults, but replicas and scheduling are largely fixed; DataStorage and enabled APIFrontend HPAs are reconciled without the chart's explicit enable/configuration contract. | **P1 configuration-parity gap.** Preserve safe portable defaults, then decide which workload-tuning fields belong in `v1alpha2`. Any new field requires CRD review and must be reconciled by the operator, not rendered by Helm. |
| **Application security controls** | `datastorage.config.auditHashKey` is a mandatory keyed HMAC chain, DataStorage has mandatory per-IP rate limiting, and APIFrontend has an opt-in distributed JWT replay cache. Bundled PostgreSQL/Valkey are TLS-configured. | The operator has DataStorage signing-certificate and retention fields, APIFrontend/Agent rate limits, and BYO Valkey client TLS, but no corresponding DataStorage HMAC-key/replay-cache model or DataStorage per-IP rate-limit model in the current API/config builders. | **P1 application-parity backlog.** Track these as explicit upstream feature gaps; do not claim Helm parity merely because platform startup is portable. Implement only after confirming the upstream service version and API shape, with security-focused tests. |
| **Application configuration and optional integrations** | Shared values cover images, LLM profiles, Fleet federation, telemetry, per-component feature gates/configuration, Tekton auto-discovery, and config-gated AAP/AWX integration. | v1alpha2 already carries image overrides, LLM profiles, Fleet, telemetry, component gates, Tekton, and Ansible/AAP configuration, but shapes/defaults and some shared-vs-per-component behavior require field-by-field parity review. | **P1 portable configuration gap, not an OpenShift dependency.** Preserve optional integration behavior without installing external controllers/CRDs; validate absent Tekton/AAP/LLM/Fleet endpoints and keep their RBAC/config ownership explicit. |
| **Credential and Secret preflight** | Required PostgreSQL, Valkey, DataStorage audit-HMAC, signing certificate, console OAuth, LLM, and optional integration Secrets are validated at install/render time where possible; the chart deliberately does not auto-generate infrastructure credentials. | The operator validates several referenced Secrets during reconciliation and derives runtime Secrets, but generic certificate-source and chart-level preflight do not yet exist. | **P1 portable security gap.** Keep customer/infrastructure/PKI Secrets administrator-owned, validate required keys before enabling dependent workloads, expose actionable conditions/events, and never let Helm and the operator co-own a Secret. |
| **Singleton and teardown safeguards** | A live Helm `lookup` guard fails fast on a second cluster installation; hooks remove admission webhooks before uninstall and validate required Secrets. | The operator has a singleton validating webhook and finalizer cleanup, but the singleton webhook is skipped when its serving certificate is absent and the current chart-level install conflict/secret preflight does not exist. | **P1 bootstrap gap.** Add a generic certificate-backed singleton path, a chart install conflict guard, required-secret validation, and teardown tests that do not deadlock on fail-closed application webhooks. |
| **Disconnected images and RBAC extensions** | Global registry/namespace/tag/digest/pull-secret settings and additional pre-existing ClusterRole bindings are exposed by values. | OLM `RELATED_IMAGE_*`, CR image overrides, pull secrets, and top-level additional ClusterRoles already cover the corresponding operator behavior. | **Mostly aligned.** Preserve this behavior in the CR and bootstrap chart; verify chart values configure the operator image and do not reintroduce workload ownership. |

The highest-risk answer to the certificate question is therefore broader than
“add a generic TLS flag.” The implementation must make certificate source,
private-key/CA lifecycle, SANs, rotation, trust-bundle publication, webhook
`caBundle` injection, and readiness ordering explicit for Kubernetes, Kind, and
OpenShift. The OpenShift service-CA path is one adapter, not the generic
implementation.

## Status and failure contract

The implementation exposes the following v1alpha2 status conditions and
provider-detection reasons:

| Condition | Meaning |
|---|---|
| `PlatformCapabilitiesReady` | Core platform capabilities were evaluated; optional capabilities may be absent without being errors |
| `ProviderDetected` | A single supported provider and compatible API were identified, or the reason no provider was selected is recorded |
| `ProviderPolicyReady` | Desired provider-native policy resources were submitted and observed at the operator's ownership boundary; this does not claim dataplane enforcement |
| `ExposureReady` | Configured Ingress/Route exposure is present or the component is intentionally internal |
| `MonitoringReady` | Configured monitoring integration is present, disabled, or unavailable with an actionable reason |

Provider reasons distinguish at least: `NoSupportedProvider`,
`AmbiguousProvider`, `UnsupportedProvider`, `UnsupportedVersion`,
`ProviderUnavailable`, `NoActiveInstallation`, and `SchemaInvalid`.

The operator must emit structured logs and events for provider detection, policy creation/update/deletion, optional API absence, and status transitions. It must include the CR generation and resource version where available.

---

## Alternatives considered

### A. Continue emitting raw Kubernetes `NetworkPolicy` everywhere — rejected

This does not provide equivalent enforcement across CNIs, cannot express provider-native API-server identities consistently, and preserves the static-CIDR/DNAT assumptions that caused the current portability problem.

### B. Install or reconcile CNI CRDs/operators from Kubernaut — rejected

This makes the application operator a CNI lifecycle controller, creates upgrade and privilege conflicts, and violates the ownership boundary. Provider operators must remain responsible for their own APIs and dataplanes.

### C. Detect by CRD presence only — rejected

CRDs can remain after a provider is removed, and multiple providers can be installed in one cluster. CRD-only detection can select an inactive or incompatible provider and silently produce ineffective policy.

### D. Require an explicit provider on every CR — rejected for the default

It is deterministic but creates unnecessary configuration on managed OpenShift and common Cilium/Calico installations. `auto` plus an explicit escape hatch handles both normal and ambiguous clusters while preserving fail-closed diagnostics.

### E. Keep v1alpha1 conversion during the clean-break rollout — rejected

The conversion path currently forces every reconcile through two API shapes and makes platform/API cleanup harder to reason about. The agreed v1alpha2 redesign is a clean break; migration must be explicit rather than silently lossy.

---

## Consequences

### Positive

- Generic Kubernetes and Kind can start the operator without OpenShift APIs or CNI policy CRDs.
- Provider support is testable, versioned, and auditable instead of being inferred from historical assumptions.
- CNI/platform operators retain ownership of their CRDs and enforcement.
- OpenShift behavior remains available without making OpenShift a core dependency.
- Helm and OLM become bootstrap/distribution paths rather than competing workload owners.

### Costs and risks

- The operator needs dynamic capability discovery, provider adapters, provider-specific RBAC, and additional status conditions.
- The clean-break v1alpha2 migration needs an explicit upgrade procedure and cannot promise transparent v1alpha1 upgrades.
- Equivalent policy intent is not necessarily identical provider YAML; every adapter needs fixtures and live enforcement validation.
- Generic TLS/webhook/exposure behavior increases the API surface and must be designed before implementation.
- A supported provider matrix requires release qualification work whenever Cilium, Calico, OVN-Kubernetes, or Kubernetes/OpenShift versions move.

---

## Approval gates

Implementation must not begin until the following are approved:

1. `v1alpha2` clean-break and v1alpha1 removal/conversion boundary;
2. provider selection field and status condition names/phase semantics;
3. common policy intent and API-server identity strategy;
4. generic TLS/webhook and exposure ownership contract;
5. Helm ownership transition and clean-install boundary;
6. the TDD/wiring plan in [`ADR-PLATFORM-001-implementation-plan.md`](ADR-PLATFORM-001-implementation-plan.md).

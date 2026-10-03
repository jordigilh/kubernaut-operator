# Platform and Policy-Provider Support

This document is the compatibility contract for the platform-neutral operator work tracked by [#486](https://github.com/jordigilh/kubernaut-operator/issues/486) and documented by [#487](https://github.com/jordigilh/kubernaut-operator/issues/487).

> **Implementation status:** the core platform-neutral path and the Cilium/Calico capability-gated adapters are implemented and covered by their pinned Kind lanes on this branch. OpenShift/OVN live qualification is deferred and explicitly unverified; this document makes no OpenShift/OVN release claim.

## Platform outcomes

The operator's core lifecycle is intended to work on both generic Kubernetes and OpenShift:

| Platform profile | Core lifecycle | Optional integrations |
|---|---|---|
| Generic Kubernetes / Kind | Singleton CR, workloads, Services, ConfigMaps, Secrets, RBAC, PDBs, status, cleanup | Kubernetes Ingress, administrator/cert-manager TLS, explicitly configured Prometheus/AlertManager, supported Cilium or Calico policy adapter |
| OpenShift 4.19–4.22 with OVN-Kubernetes | Design target; not live-qualified by this work | OpenShift Routes, service-CA/router CA, TLS security profile, OpenShift monitoring, and OVN admin policies remain unverified |

OpenShift is not required for the generic profile. A plain Kind cluster with no policy provider may run the core lifecycle without creating provider-policy resources. It must report that provider policy enforcement is not active; it must not silently claim equivalent network enforcement.

OpenShift 4.19–4.22 with OVN-Kubernetes is also **not live-qualified by this
work**. It requires the deferred compatibility review, qualified payloads, and
enforcement testing before any release claim is made. OpenShift 4.23 and later
are outside the initial OVN policy contract as well.

## Initial policy-provider matrix

| Provider | Release range | Policy API/GVKs | Platform range |
|---|---|---|---|
| Cilium | 1.19.x–1.20.x | `cilium.io/v2` `CiliumNetworkPolicy`; `CiliumClusterwideNetworkPolicy` where cluster scope is required | Cilium 1.19 with Kubernetes 1.32–1.35; Cilium 1.20 with Kubernetes 1.33–1.36 |
| Calico | 3.31.x–3.32.x | `projectcalico.org/v3` `NetworkPolicy`; `GlobalNetworkPolicy` where cluster scope is required | Calico 3.31 with Kubernetes 1.32–1.35; Calico 3.32 with Kubernetes 1.34–1.36 |
| OpenShift OVN-Kubernetes | OpenShift 4.19–4.22 (deferred qualification) | `policy.networking.k8s.io/v1alpha1` `AdminNetworkPolicy` and `BaselineAdminNetworkPolicy` | OpenShift `Network` configuration must positively identify `OVNKubernetes`; no live qualification or release claim in this work |

The three version numbers are different contracts and must not be conflated:

1. **Kubernaut CRD version**: the operator API is the clean-break `kubernaut.ai/v1alpha2`.
2. **Provider resource API version**: the GVKs in the table are the APIs the operator submits.
3. **Provider/platform release**: the Cilium, Calico, or OpenShift release range is the qualification boundary.

The Calico adapter additionally requires the Kubernetes datastore and a
discovered `projectcalico.org/v3` API (either Calico's aggregation API server
or native v3 CRDs). Calico's etcd datastore, or an installation exposing only
the legacy `crd.projectcalico.org/v1` storage API, is rejected because the
provider-native ServiceMatch API-server identity is unavailable. The operator
does not install or enable the Calico API server, native CRDs, or any Calico
operator.

## Unsupported initially

The operator does not initially support:

- Antrea or other unlisted providers;
- Cilium or Calico releases outside the listed ranges;
- upstream `policy.networking.k8s.io/v1alpha2` `ClusterNetworkPolicy`;
- OpenShift 4.23+ for the OVN adapter;
- using OVN `EgressFirewall` as a replacement for general policy (it is egress-only);
- installing a CNI operator or provider CRD;
- falling back to raw Kubernetes `NetworkPolicy` when a provider policy API is absent or unsupported;
- static install-time API-server CIDRs as the API-server identity.

## Provider selection and detection

`auto` detection is the default. An explicit provider override is available for an ambiguous generic cluster, but the override still has to pass API and compatibility checks.

Detection requires both:

- discovery of the expected provider API and compatible GVK/schema; and
- evidence that the provider is actively installed and serving the cluster, such as provider-owned DaemonSets/pods, labels, or OpenShift network configuration.

CRD presence alone is insufficient. This prevents a stale CRD or an inactive second provider from being selected.

The operator follows this behavior:

| Detection result | Provider policy resources | Status behavior |
|---|---|---|
| One active supported provider in range | Render and reconcile the provider-native resources | Report provider and API version; `ProviderPolicyReady` reflects submission/readiness only |
| No provider / plain Kind profile | None | Report `NoSupportedProvider`; do not claim provider policy enforcement |
| Ambiguous active providers | None | Report `AmbiguousProvider`; recommend an explicit override |
| Unsupported or out-of-range provider | None | Report `UnsupportedProvider` or `UnsupportedVersion` |
| Explicit provider unavailable or API/schema mismatch | None | Report `ProviderUnavailable`, `NoActiveInstallation`, or `SchemaInvalid` |

The operator does not own dataplane enforcement. A successful policy-object update means that the provider API accepted the object; it does not mean that every node is enforcing it. Live enforcement is validated in provider-specific test lanes.

## Policy ownership and safety

The operator translates one common Kubernaut traffic intent into provider-native policy objects. The CNI or platform operator owns:

- provider CRD installation and upgrades;
- provider controller/agent lifecycle;
- dataplane programming and enforcement;
- provider-specific defaults and unrelated policy bundles.

The Kubernaut operator owns only its labeled policy objects and cleans them up deterministically. It must not overwrite platform-owned objects. Namespaced objects use owner references where Kubernetes permits them; cluster-scoped objects use deterministic labels and finalizer cleanup.

The API-server destination is represented through provider-native identity/entity semantics where supported. The operator does not pin policy to an install-time API-server CIDR. If a provider cannot safely express a required target, the adapter reports the capability gap instead of broadening access or emitting a raw `NetworkPolicy` fallback.

## Generic Kubernetes integrations

Generic Kubernetes installations use explicit portable configuration:

- standard Kubernetes `Ingress` for external exposure, with a declared ingress class, hosts, TLS Secret, annotations, timeout, and redirect contract;
- administrator-supplied serving certificates/CA references or a supported certificate provider such as cert-manager;
- explicitly configured Prometheus and AlertManager endpoints, TLS CAs, and credentials where required;
- `ServiceMonitor` and `PrometheusRule` only when Prometheus Operator APIs are installed and the integration is enabled;
- no dependency on `openshift-monitoring`, Thanos Querier, OpenShift `AlertmanagerConfig`, service-CA, or OpenShift router CA.

### TLS and certificate bootstrap

TLS is not one switch. The target contract covers the operator manager
webhook, the managed AuthWebhook and its admission `caBundle`, inter-service
leaf certificates and trust bundles, external Ingress/Route certificates, and
outbound database/Valkey/monitoring/IdP/LLM trust separately.

Supported source modes are:

| Source | Generic Kubernetes / Kind | OpenShift |
|---|---|---|
| Administrator-managed | Reference pre-created Secrets/ConfigMaps; the operator validates keys, CA consistency, and SANs without overwriting them | Supported where the referenced material satisfies the same contract |
| cert-manager | Requires a pre-installed cert-manager, configured issuer, and its output Secrets; the operator never installs cert-manager or its CRDs | Optional; not installed by Kubernaut |
| Development self-signed | Explicit opt-in for Kind/development; creates a dedicated CA and leaves and reports non-production status | Available only if explicitly selected; service-CA remains the preferred platform adapter |
| OpenShift service-CA/router-CA | Not available | Optional adapter for Service leaves, webhook CA injection, Routes, and router trust |

The operator must not silently fall back from a missing or invalid production
source to plaintext HTTP, an empty webhook `caBundle`, or a fixed OpenShift
ConfigMap. Internal service mTLS uses a dedicated CA; its public bundle may be
mounted by workloads, but CA private keys remain restricted to the provisioner.
Certificate rotation must publish new trust before replacing leaves and must
retain an overlap window or perform a coordinated restart. DataStorage signing
and audit-HMAC material are integrity keys, not network certificates, and are
managed as separate prerequisites.

The dedicated operator Helm chart is deferred to a follow-up. The current
production installation artifacts are the repository's Kustomize/OLM
manifests; they own the operator manager's serving-certificate prerequisites.
The operator owns per-instance AuthWebhook, inter-service, and runtime trust
artifacts once a `Kubernaut` instance is reconciled. A future Helm chart and the
operator must never co-own the same Secret or ConfigMap.

## OpenShift integrations

On a supported OpenShift release, optional adapters may use:

- `route.openshift.io` Routes;
- service-CA serving certificates and injected CA bundles;
- `config.openshift.io` TLS profile and ingress-domain data;
- OpenShift monitoring and `AlertmanagerConfig` APIs;
- OVN-Kubernetes `AdminNetworkPolicy` and `BaselineAdminNetworkPolicy`.

Missing OpenShift APIs must not prevent the generic operator manager from starting. OpenShift-specific status and events identify which optional capability was not available.

## Helm and ownership

The dedicated `kubernaut-operator` Helm chart is deferred to a follow-up and is
not a supported installation path in this work. The `kubernaut.ai/v1alpha2` CRD
remains the sole application schema; users or GitOps controllers apply the CR
separately after installing the current production manifests.

The separate `kubernaut-dependencies` chart is an explicitly non-production
convenience chart for testing, demos, and CI. It may deploy single-replica,
PVC-backed PostgreSQL and Valkey, but it is not a production-ready HA,
backup, or upgrade solution and is never installed as a dependency of the
operator chart. Production users should provide managed PostgreSQL and Valkey
outside this chart.

The current installation sequence is:

1. Install the repository's Kustomize/OLM package.
2. Wait for the operator manager and CRD to become ready.
3. For testing/demo/CI only, optionally install `kubernaut-dependencies`; in
   production, provision managed PostgreSQL and Valkey externally.
4. Provision credentials, policy input ConfigMaps, and any selected
   certificate-provider material.
5. Apply the `Kubernaut` CR from GitOps or another user-controlled workflow.
6. Observe the operator's status conditions and events as it reconciles the
   application workloads and optional integrations.

The deferred Helm chart, when implemented, must validate bootstrap values only.
CR validation is performed by the Kubernetes CRD schema and the operator's
admission/reconciliation paths.

Do not use Helm and the operator to manage the same Deployments, Services, ConfigMaps, Secrets, RBAC objects, exposure objects, or policy objects. The initial migration contract is clean install/reinstall; in-place adoption of old Helm-owned workloads requires a separate readiness-gated design.

## Updating this contract

A provider or platform release may be added only after all of the following are available:

1. official release/API compatibility evidence;
2. runtime GVK and schema validation;
3. rendered-object fixtures for the common policy intent;
4. unit and envtest coverage for detection, status, lifecycle, and cleanup;
5. a qualified live enforcement lane;
6. updated RBAC, installation documentation, and upgrade notes.

Until then, an unlisted release is unsupported and produces no provider policy resources.

## Upstream references

- [OpenShift 4.19 AdminNetworkPolicy API](https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html/network_apis/adminnetworkpolicy-policy-networking-k8s-io-v1alpha1)
- [OpenShift network policy APIs](https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html/network_security/network-policy-apis)
- [Cilium 1.19 Kubernetes compatibility](https://docs.cilium.io/en/v1.19/network/kubernetes/compatibility)
- [Cilium 1.20 Kubernetes compatibility](https://docs.cilium.io/en/stable/network/kubernetes/compatibility)
- [Calico Kubernetes requirements](https://docs.tigera.io/calico/latest/getting-started/kubernetes/requirements)
- [Calico NetworkPolicy resources](https://docs.tigera.io/calico/latest/reference/resources/networkpolicy)
- [Calico GlobalNetworkPolicy resources](https://docs.tigera.io/calico/latest/reference/resources/globalnetworkpolicy)

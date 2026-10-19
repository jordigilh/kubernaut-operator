# Platform and NetworkPolicy Support

This document is the compatibility contract for the platform-neutral operator work tracked by [#486](https://github.com/jordigilh/kubernaut-operator/issues/486) and documented by [#487](https://github.com/jordigilh/kubernaut-operator/issues/487).

> **Implementation status:** issue #555 replaces the provider-native policy architecture with the portable Kubernetes NetworkPolicy contract in `docs/design/ADR-PLATFORM-003-portable-kubernetes-networkpolicy.md`.

## Platform outcomes

The operator's core lifecycle is intended to work on both generic Kubernetes and OpenShift:

| Platform profile | Core lifecycle | Optional integrations |
|---|---|---|
| Generic Kubernetes / Kind | Singleton CR, workloads, Services, ConfigMaps, Secrets, RBAC, PDBs, status, cleanup, standard NetworkPolicies | Kubernetes Ingress, administrator/cert-manager TLS, explicitly configured Prometheus/AlertManager |
| OpenShift | Same core lifecycle and standard NetworkPolicy object contract | Routes, service-CA/router CA, TLS security profile, and OpenShift monitoring remain capability-specific; no OVN policy API is required |

The operator creates standard `NetworkPolicy` objects and reports
`NetworkPolicyReady=True` after ownership-safe application. This reports object
state only; it does not claim that the installed CNI enforces the policy.

Runtime policies are selector-based and ingress-only. The operator does not
discover provider APIs, version-gate a CNI, track Pod IPs, or generate
API-server CIDR/IPBlock egress. Kubernetes API-server and external dependency
egress is an administrator/SRE/cloud-network responsibility.

Only policies carrying the operator ownership markers are updated, pruned, or
deleted. Administrator-owned policies are preserved across reconcile, uninstall,
and reinstall. The OLM bundle carries a narrowly scoped static operator-manager
metrics ingress policy; the generic Helm bootstrap does not install a policy
before the webhook path is available. Operand policies are reconciled after the
Kubernaut CR is applied.

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
| `hook` | Explicit Helm-compatible hook mode; the operator provisions and owns the generated CA, leaves, trust bundle, webhook trust, and signing material | Not live-qualified by the current generic Kind evidence |
| `manual` | Reference pre-created Secrets/ConfigMaps; the operator validates keys, CA consistency, and SANs without overwriting them | Supported where the referenced material satisfies the same contract, but not live-qualified here |
| `AdministratorManaged` | Reference pre-created administrator-owned Secrets; the operator validates and publishes trust without adopting or deleting the inputs | Supported where the referenced material satisfies the same contract, but not live-qualified here |
| cert-manager | Requires a pre-installed cert-manager, configured issuer, and its output Secrets; the operator never installs cert-manager or its CRDs | Optional; not installed by Kubernaut |
| Development self-signed | Explicit opt-in for Kind/development; creates a dedicated CA and leaves and reports non-production status | Available only if explicitly selected; service-CA remains the preferred platform adapter |
| OpenShift service-CA/router-CA | Not available | Optional adapter for Service leaves, webhook CA injection, Routes, and router trust |

The generic Kind acceptance matrix runs isolated `development`, `hook`,
`manual-admin`, and `certmanager` selectors. The `manual-admin` lane exercises
both the lower-case `manual` mode and the title-case `AdministratorManaged`
mode against pre-created administrator-owned material. These hosted journeys
are implementation evidence rather than a FedRAMP, SOC 2, or OWASP ASVS
certification claim. OpenShift service-CA and router-CA qualification remains
deferred until an OCP-capable runner is available.

The operator must not silently fall back from a missing or invalid production
source to plaintext HTTP, an empty webhook `caBundle`, or a fixed OpenShift
ConfigMap. Internal service mTLS uses a dedicated CA; its public bundle may be
mounted by workloads, but CA private keys remain restricted to the provisioner.
Certificate rotation must publish new trust before replacing leaves and must
retain an overlap window or perform a coordinated restart. DataStorage signing
and audit-HMAC material are integrity keys, not network certificates, and are
managed as separate prerequisites.

The dedicated `kubernaut-operator` Helm chart is the supported bootstrap path
for generic Kubernetes and Kind. OpenShift production installations use the
OLM bundle and its service-CA integration. The repository's Kustomize
manifests remain available for compatibility and development workflows; they
are not used by the Kind CI deployment lanes. These installation paths own the
operator manager's serving-certificate prerequisites.
For generated `DevelopmentSelfSigned` and `hook` modes, the operator owns
per-instance AuthWebhook, inter-service, and runtime trust artifacts once a
`Kubernaut` instance is reconciled. In `manual` and `AdministratorManaged`
modes, the referenced Secrets remain administrator-owned and survive operator
cleanup. A future Helm chart and the operator must never co-own the same Secret
or ConfigMap.

## OpenShift integrations

On a supported OpenShift release, optional adapters may use:

- `route.openshift.io` Routes;
- service-CA serving certificates and injected CA bundles;
- `config.openshift.io` TLS profile and ingress-domain data;
- OpenShift monitoring and `AlertmanagerConfig` APIs;
- OpenShift NetworkPolicy API compatibility.

Missing OpenShift APIs must not prevent the generic operator manager from starting. OpenShift-specific status and events identify which optional capability was not available.

## Helm and ownership

The `kubernaut-operator` Helm chart installs only operator bootstrap resources;
it does not create a `Kubernaut` CR or application workloads. The
`kubernaut.ai/v1alpha2` CRD remains the sole application schema; users or
GitOps controllers apply the CR separately after installing the Helm chart or
the OLM package.

The separate `kubernaut-dependencies` chart is an explicitly non-production
convenience chart for testing, demos, and CI. It may deploy single-replica,
PVC-backed PostgreSQL and Valkey, but it is not a production-ready HA,
backup, or upgrade solution and is never installed as a dependency of the
operator chart. Production users should provide managed PostgreSQL and Valkey
outside this chart.

The installation sequence is:

1. On generic Kubernetes or Kind, install the operator Helm chart. On
   OpenShift, install the operator through OLM.
2. Wait for the operator manager and CRD to become ready.
3. For testing/demo/CI only, optionally install `kubernaut-dependencies`; in
   production, provision managed PostgreSQL and Valkey externally.
4. Provision credentials, policy input ConfigMaps, and any selected
   certificate-provider material.
5. Apply the `Kubernaut` CR from GitOps or another user-controlled workflow.
6. Observe the operator's status conditions and events as it reconciles the
   application workloads and optional integrations.

The Helm chart validates bootstrap values only. CR validation is performed by
the Kubernetes CRD schema and the operator's admission/reconciliation paths.

Do not use Helm and the operator to manage the same Deployments, Services, ConfigMaps, Secrets, RBAC objects, exposure objects, or policy objects. The initial migration contract is clean install/reinstall; in-place adoption of old Helm-owned workloads requires a separate readiness-gated design.

## Updating this contract

Changes to the portable policy contract require standard-API schema, selector,
ownership, lifecycle, packaging, and security-traceability evidence. A live
CNI qualification may establish enforcement for that CNI, but it must not be
presented as a baseline operator guarantee.

## Upstream references

- [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [OpenShift NetworkPolicy API](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/network_apis/networkpolicy-networking-k8s-io-v1)

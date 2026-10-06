# Issue #489 Implementation Plan

**Issue:** [kubernaut-operator#489](https://github.com/jordigilh/kubernaut-operator/issues/489)

**Parent initiative:** [#486](https://github.com/jordigilh/kubernaut-operator/issues/486)

**Baseline:** `origin/main` at `b1c4780` (2026-10-06)

**Status:** Proposed plan only. No chart or production implementation is included
in this change.

**Methodology:** RED → GREEN → REFACTOR → CHECK, with chart-install integration
proof and ownership verification before the chart is considered implemented.

## 1. Objective

Add a production-ready `kubernaut-operator` Helm chart for the deployment model
where the operator is the only supported Kubernaut application deployment tool
on OpenShift, generic Kubernetes, and Kind.

The chart is a **bootstrap and distribution path**. It installs the operator and
its prerequisites; the operator reconciles the application after a user or
GitOps controller applies the `Kubernaut` custom resource.

The chart must preserve the existing OLM/Kustomize paths and must not compete
with the operator for application-resource ownership.

## 2. Baseline and implementation discovery

The `origin/main` baseline confirms:

- no `charts/` directory or `Chart.yaml` exists;
- the current production packaging is OLM/Kustomize;
- `kubernaut.ai/v1alpha2` is the application configuration contract;
- the operator already owns runtime workload reconciliation;
- generic Kind, Cilium, and Calico lanes exist;
- OpenShift/OVN live qualification remains a separate release-validation lane;
- Fleet configuration and Envoy AI Gateway selection are already represented in
  the v1alpha2 operator API and runtime configuration;
- the operator-only Helm chart and its ownership tests are explicitly deferred
  in the platform-neutrality design.

The chart therefore adds missing bootstrap packaging. It does not recreate the
upstream direct-workload chart or copy its application-values schema.

## 3. Non-negotiable ownership contract

### 3.1 Resources the production chart may own

- operator Deployment and ServiceAccount;
- manager ClusterRoles, Roles, and bindings;
- the `Kubernaut` CRD under the selected CRD-upgrade contract;
- manager metrics and health bootstrap resources;
- manager admission webhook configuration;
- manager serving-certificate prerequisites and source-specific CA publication;
- namespace, labels, annotations, image, pull-secret, leader-election, metrics,
  health-probe, and bootstrap security settings;
- optional chart-managed certificate-provider references, when explicitly
  selected and the provider is already installed.

### 3.2 Resources the production chart must not own

- a `Kubernaut` custom resource;
- Kubernaut application Deployments, Services, ConfigMaps, Secrets, PDBs, HPAs,
  Ingresses, Routes, runtime RBAC, migration Jobs, or runtime policy objects;
- PostgreSQL or Valkey workloads;
- Envoy AI Gateway, Kuadrant, CNI/provider operators, provider CRDs, monitoring
  operators, ingress controllers, or cert-manager;
- per-instance AuthWebhook/inter-service certificates and trust artifacts owned
  by the operator after a `Kubernaut` CR is applied;
- duplicated application configuration values from the upstream Helm chart.

Envoy AI Gateway is selected and consumed through the `Kubernaut` CR/Fleet
configuration and remains an external platform dependency. This chart does not
install the gateway.

## 4. API, values, and compatibility decisions

### 4.1 Application schema

No CRD schema change is planned for #489. The chart must not introduce Helm
values for service/workload configuration that belongs in `v1alpha2`.

Required invariants:

- the generated CRD remains sourced from `api/v1alpha2/`;
- `make manifests generate` remains the generation path;
- Helm values configure bootstrap only;
- a user/GitOps workflow applies the `Kubernaut` CR separately;
- Helm values and rendered application manifests are not treated as public
  compatibility surfaces.

Upstream Helm v1.6 behavior is a behavioral/design reference for ownership,
defaults, TLS, trust, and installation sequencing. The operator API may remain
native and nested rather than copying the upstream values shape.

### 4.2 Proposed bootstrap values

The initial values schema should be deliberately small and typed:

- `namespace` / release namespace behavior;
- operator image registry, repository, tag, digest, pull policy, and pull
  secrets;
- ServiceAccount name and annotations;
- leader-election configuration;
- metrics Service and secure/insecure metrics selection;
- health/readiness probe configuration;
- pod security context, container security context, priority class, node
  selectors, tolerations, affinity, topology spread, and resource requests;
- bootstrap labels and annotations;
- manager serving-certificate source and webhook CA publication source;
- optional administrator-managed Secret/CA references;
- explicit development-only self-signed profile;
- explicit cert-manager reference/provisioning profile when cert-manager is
  already installed;
- OpenShift service-CA profile only when explicitly selected;
- CRD installation/upgrade policy and uninstall behavior.

No value should configure Gateway, DataStorage, API Frontend, Agent, Fleet,
PostgreSQL, Valkey, monitoring destinations, or application policy. Those belong
to the CRD and operator reconciliation.

## 5. CRD ownership and upgrade strategy

This is an approval gate because Helm's `crds/` directory installs CRDs but does
not upgrade them automatically.

### Recommended direction

Use the generated CRD as the single artifact and document an explicit CRD
upgrade workflow for Helm installations. The production chart must:

1. install the CRD on first install;
2. never delete the CRD on chart uninstall;
3. never create a competing CRD definition through an application template;
4. make upgrades explicit and idempotent;
5. identify whether Helm, OLM, or Kustomize is the selected CRD owner for an
   installation; never use two packaging paths simultaneously;
6. verify that the CRD is compatible before the manager Deployment becomes
   ready.

The RED phase must compare these alternatives before implementation:

- install-only `crds/` plus a documented explicit `kubectl apply`/upgrade
  command;
- Helm-templated CRD with a keep-on-uninstall policy and upgrade tests;
- a separate CRD chart/release with the operator chart depending on its
  documented installation contract.

The recommended default is the least-destructive option that preserves CRD
retention and keeps OLM/Kustomize ownership unambiguous. The final choice needs
approval before GREEN.

## 6. Certificate and webhook contract

The chart must support three generic bootstrap profiles and an optional
OpenShift adapter without silently downgrading to plaintext:

| Profile | Chart responsibility | External prerequisite |
|---|---|---|
| Administrator-managed | Reference and validate manager Secret/CA inputs; publish the configured CA | Administrator-provided material |
| Cert-manager | Render only explicitly selected cert-manager resources/references and source-specific CA injection | Existing cert-manager and issuer/ClusterIssuer |
| Development self-signed | Provide an explicit, visibly non-production bootstrap path for Kind/CI | None, but not a production default |
| OpenShift service-CA | Render only when the OpenShift profile is selected | OpenShift service-CA integration |

Tests must prove missing, malformed, expired, partial, and rotating material is
observable and fails closed. Generic chart rendering must not emit OpenShift-only
annotations by default.

The chart owns manager bootstrap certificates only. Once a `Kubernaut` CR is
applied, the operator owns per-instance runtime TLS/trust according to the CR's
selected source and ownership rules.

## 7. TDD execution plan

Estimated total: 7–12 engineer-days, excluding environment-specific OpenShift
qualification and any unresolved CRD-upgrade design work.

### Phase 0 — Contract and RED preparation (0.5–1 day)

**RED/design work:**

- add chart ownership tests before chart templates exist;
- define the bootstrap values schema and reject application configuration keys;
- define the CRD ownership/upgrade/uninstall contract;
- define certificate-source and CA-publication profiles;
- define clean-install, upgrade, reinstall, conflict, and uninstall journeys;
- document the Envoy AI Gateway boundary: CR-driven integration, not chart-owned
  gateway installation.

**Gate:** approve chart ownership, CRD upgrade, certificate-source, and uninstall
decisions before adding production templates.

### Phase 1 — Chart skeleton and bootstrap boundary (1–2 days)

**RED:**

- `helm lint` fails until required chart metadata, schema, and templates exist;
- `helm template` assertions require a manager, CRD, bootstrap RBAC, and no CR;
- rendered-object tests reject application workloads, dependencies, provider
  resources, and application-values fields.

**GREEN:**

- add `charts/kubernaut-operator/Chart.yaml`, `values.yaml`,
  `values.schema.json`, `crds/`, and bootstrap templates;
- render the operator Deployment, ServiceAccount, manager RBAC, metrics/health
  resources, webhook configuration, and namespace-scoped bootstrap resources;
- make image and pull-secret configuration work with tags and immutable digests;
- preserve OLM/Kustomize files unchanged.

**REFACTOR:**

- deterministic names and labels;
- minimal templates with no duplicated application schema;
- conflict detection for existing resources owned by another installer.

### Phase 2 — CRD lifecycle and packaging ownership (1–2 days)

**RED:**

- install, upgrade, reinstall, and uninstall tests define the selected CRD
  strategy;
- tests prove the chart never deletes the CRD or creates a `Kubernaut` CR;
- tests prove OLM/Kustomize and Helm are alternative owners, not co-owners.

**GREEN:**

- implement the approved CRD installation/upgrade path;
- add explicit documentation and Make targets for CRD upgrade verification;
- check generated CRD identity and version against `config/crd/bases`.

**REFACTOR:**

- add version skew diagnostics and actionable upgrade failures;
- verify a manager cannot become ready against an incompatible CRD.

### Phase 3 — Certificate, webhook, and security profiles (2–3 days)

**RED:**

- render tests for administrator-managed, cert-manager, development, and
  OpenShift service-CA profiles;
- negative tests for missing/invalid/expired/partial certificates;
- tests proving generic profiles emit no OpenShift-only annotations;
- tests proving runtime TLS Secrets are not chart-owned.

**GREEN:**

- implement source-specific manager Secret and CA publication templates;
- implement explicit development-only self-signed behavior;
- render cert-manager references/resources only when selected;
- configure webhook `caBundle` publication according to the selected source.

**REFACTOR:**

- make ownership labels and annotations explicit;
- prevent Secret material from leaking through values, NOTES, or rendered
  manifests;
- test certificate rotation and reinstall behavior.

### Phase 4 — Installation journeys and generic Kubernetes proof (1–2 days)

**RED:**

- Kind test installs the chart without `oc`, OpenShift APIs, monitoring CRDs,
  cert-manager, PostgreSQL, or Valkey;
- the test waits for manager and CRD readiness;
- a user-applied CR is the only path that starts application reconciliation;
- the chart does not install Envoy AI Gateway or any provider operator.

**GREEN:**

- add a CI job using the local operator image and immutable image override;
- prove manager readiness, CRD readiness, and successful user/GitOps CR apply;
- exercise generic bootstrap with the Envoy AI Gateway configuration contract,
  using an externally supplied gateway where the journey requires one.

**REFACTOR:**

- add disconnected-registry, pull-secret, digest, upgrade, reinstall, and
  conflict scenarios;
- keep chart installation independent from the full application pipeline.

### Phase 5 — CHECK and release evidence (1–2 days)

Run:

```text
helm lint charts/kubernaut-operator
helm template charts/kubernaut-operator --values <supported-profile>
go build ./...
golangci-lint run
make test
make manifests generate
git diff --exit-code config/ bundle/ dist/
make test-pyramid
```

Record separate evidence for generic Kind, OpenShift bootstrap, certificate
profiles, CRD lifecycle, RBAC, disconnected images, and ownership conflicts.

## 8. Wiring manifest

| Component | Production entry point | Wiring location | Required test |
|---|---|---|---|
| Operator Helm chart | `helm install kubernaut-operator` | `charts/kubernaut-operator/templates/**` | `IT-HELM-BOOTSTRAP-001` |
| CRD installation/upgrade | Selected Helm CRD workflow | `charts/kubernaut-operator/crds/**` and documented upgrade target | `IT-HELM-CRD-LIFECYCLE-001` |
| Manager Deployment | Helm chart bootstrap | `charts/kubernaut-operator/templates/deployment.yaml` | `IT-HELM-MANAGER-001` |
| Manager ServiceAccount/RBAC | Helm chart bootstrap | `templates/serviceaccount.yaml`, `templates/rbac/**` | `IT-HELM-RBAC-001` |
| Webhook configuration | Helm chart bootstrap | `templates/webhook/**` | `IT-HELM-WEBHOOK-001` |
| Manager certificate source | Selected chart certificate profile | `templates/certificates/**` | `IT-HELM-CERT-001` |
| Metrics/health resources | Helm chart bootstrap | `templates/service.yaml`, `templates/metrics/**` | `IT-HELM-OBSERVABILITY-001` |
| User application lifecycle | User/GitOps-applied `Kubernaut` CR | Existing `KubernautReconciler.Reconcile` path; intentionally not chart-owned | `IT-HELM-USER-CR-001` |
| Envoy AI Gateway | External platform installation plus CR configuration | `spec.fleet.mcpGateway.type` and endpoint wiring; intentionally not chart-owned | `IT-FLEET-EAIGW-001` |

Checkpoint W fails if the chart renders a `Kubernaut` CR, duplicates an
operator-owned runtime resource, installs an external operator/provider, or has
no installation test proving its bootstrap resources are usable.

## 9. RBAC and security requirements

- Grant the manager only the bootstrap/runtime permissions already required by
  the operator's generated RBAC contract.
- Do not grant chart hooks CRD installation, CNI administration, provider
  operator lifecycle, or application-level privileges beyond the existing
  operator role.
- Do not generate credentials in Helm values or print them in NOTES.
- Support immutable image digests and disconnected pull secrets.
- Apply restricted security contexts by default where compatible with manager
  operation.
- Make OpenShift-specific annotations opt-in and source-specific.
- Preserve structured reconciliation audit logging in the operator; the chart
  does not introduce a separate audit store.

## 10. Upgrade, uninstall, and conflict behavior

The plan must define and test:

- clean install into an empty namespace;
- upgrade between chart versions with a CRD change;
- reinstall after a failed manager rollout;
- installation over resources owned by OLM/Kustomize or another Helm release;
- uninstall with and without an existing `Kubernaut` CR;
- CRD retention and explicit CRD upgrade behavior;
- manager webhook and certificate cleanup;
- no deletion of user-owned application runtime resources by the chart.

The default safety posture should require the user to remove or migrate the
`Kubernaut` CR before removing the operator, and must never delete the CRD as a
side effect of chart uninstall. Any override that permits orphaned runtime
resources requires explicit documentation and a test.

## 11. Success criteria

The issue is complete only when:

- `helm lint` and supported `helm template` profiles pass;
- the chart installs on Kind without `oc`, OpenShift APIs, monitoring CRDs,
  cert-manager, PostgreSQL, or Valkey;
- manager and CRD readiness are proven;
- applying a user-owned `Kubernaut` CR starts the existing operator lifecycle;
- the chart renders no `Kubernaut` CR, application workload, dependency workload,
  provider CRD/operator, provider policy, or duplicated application schema;
- administrator-managed, cert-manager, development, and selected OpenShift
  certificate paths are tested with fail-closed negative cases;
- CRD upgrade, retention, uninstall, reinstall, and ownership conflict behavior
  is explicit and tested;
- disconnected images, immutable digests, pull secrets, security context, and
  least-privilege RBAC are covered;
- OLM/Kustomize installation remains unchanged and generated artifacts remain
  clean;
- Envoy AI Gateway remains externally owned and is validated through the CR
  configuration contract;
- generic Kind and OpenShift bootstrap evidence are recorded separately;
- the final confidence assessment is at least 90% with residual risks recorded.

## 12. Approval gates before implementation

User approval is required before GREEN for:

1. the CRD installation/upgrade strategy;
2. manager certificate-source and CA-publication behavior;
3. uninstall behavior when a `Kubernaut` CR exists;
4. the exact bootstrap values surface;
5. Helm/OLM/Kustomize ownership and conflict policy;
6. whether the non-production dependencies chart is explicitly deferred to a
   separate issue (recommended);
7. the TDD sequence and wiring manifest above.

**Planning confidence: 94%.** The origin/main baseline, ownership boundary,
current packaging, CRD source, existing TLS/profile foundations, and generic
Kind test harness are known. Remaining uncertainty is concentrated in the
CRD-upgrade mechanism, Helm certificate generation/rotation mechanics, and the
desired uninstall safety gate; those are intentionally approval-gated rather
than inferred during implementation.

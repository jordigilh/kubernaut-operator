# Issue #489 Implementation Plan — Preflight Revision

**Issue:** [kubernaut-operator#489](https://github.com/jordigilh/kubernaut-operator/issues/489)

**Parent initiative:** [#486](https://github.com/jordigilh/kubernaut-operator/issues/486)

**Baseline:** `origin/main` at `55f71eb` (2026-10-07), including the merged
native-monitoring changes from PR #507. The earlier plan recorded `b1c4780`;
that baseline is stale.

**Status:** Implementation and qualification are recorded in PR
[#509](https://github.com/jordigilh/kubernaut-operator/pull/509). The current
remediation keeps development certificate generation outside the manager Pod
and makes Helm lint plus the operator-only Helm Kind journey release gates.

**Methodology:** RED → GREEN → REFACTOR → CHECK, with the wiring checkpoint,
the unit/integration/E2E pyramid, and security-control evidence required before
implementation can be considered complete.

## 1. Objective and installation contract

Add a production-ready `kubernaut-operator` Helm chart for the operator-only
deployment model on generic Kubernetes, Kind, and OpenShift:

```text
helm install kubernaut-operator
  -> installs operator bootstrap, Kubernaut CRD, RBAC, and manager webhook prerequisites
  -> waits for manager and CRD readiness
  -> user or GitOps applies the Kubernaut CR
  -> the existing KubernautReconciler owns the application lifecycle
```

The chart is a bootstrap/distribution path, not a second application
configuration API. It must preserve OLM and Kustomize as supported alternatives
and must not render a `Kubernaut` CR, application workloads, databases, provider
operators, provider CRDs, or duplicated application values.

The `kubernaut.ai/v1alpha2` CRD remains the sole application configuration
schema. Production PostgreSQL and Valkey are administrator/platform-owned
prerequisites referenced by the user-applied CR.

## 2. Current source preflight

### 2.1 Repository and packaging findings

| Evidence | Current behavior | Implementation consequence |
|---|---|---|
| `charts/`, `Chart.yaml` | No repository-owned Helm chart exists. | The chart is new packaging, not a migration of existing chart templates. |
| `config/default/` | Kustomize creates the CRD, RBAC, manager, namespace, and HTTPS metrics Service. Webhook-specific resources and certificate mounts remain commented out. | The Helm chart cannot assume the default Kustomize overlay is a generic webhook bootstrap. |
| `bundle/manifests/` and `config/manifests/patches/webhook.yaml` | OLM carries a `webhookdefinitions` entry for the singleton endpoint with `failurePolicy: Fail`, port `9443`, and `/validate-kubernaut-singleton`. | OLM's webhook injection contract must be preserved, but it is not a reusable generic-Kubernetes certificate implementation. |
| `api/v1alpha2/kubernaut_types.go` and `config/crd/bases/kubernaut.ai_kubernauts.yaml` | `Kubernaut` is namespaced and `v1alpha2` is the sole served/storage version. | The chart must ship exactly this generated CRD and must not add a Helm-specific application schema. |
| `internal/resources/crds.go` | `EnsureCRDs` reads shared application CRDs from `kubernaut/pkg/shared/assets` during the operator's migration phase. | The chart must not package those runtime application CRDs. The chart owns only `kubernauts.kubernaut.ai`; the operator/runtime path owns the post-CR application CRDs. |
| `config/rbac/role.yaml` | The generated manager ClusterRole includes core, optional OpenShift, cert-manager, monitoring, Cilium, Calico, OVN, SPIRE, Kagenti, and `discovery.k8s.io/endpointslices` rules. | Chart RBAC must be sourced from the current generated contract, including the new EndpointSlice watch permission, without adding hook privileges. |
| `Makefile` and `.github/workflows/test.yml` | `make manifests generate`, independent unit/integration coverage, security traceability, CI-boundary, and test-pyramid checks are established. | Chart checks must extend these gates without collapsing the existing tiers or changing OLM/Kustomize generation semantics. |
| `test/e2e/kind/` | The existing Kind suite installs the operator through Kustomize and separately exercises runtime TLS/provider lanes. | Add a separate Helm bootstrap lane; do not reinterpret the existing Kustomize E2E as Helm evidence. |

The current `origin/main` delta also adds native monitoring policy resolution,
EndpointSlice watches, and the corresponding RBAC rule. Any chart ClusterRole
copy made from the earlier plan would be incomplete.

### 2.2 Manager entrypoint and webhook findings

`cmd/main.go` currently establishes these runtime contracts:

- metrics default to disabled (`--metrics-bind-address=0`) while secure metrics
  are enabled if an address is selected;
- the health/readiness server binds to `:8081`;
- leader election is enabled by the packaged manager args and uses the fixed ID
  `kubernaut-operator.kubernaut.ai`;
- no explicit `LeaderElectionNamespace` is passed to `ctrl.NewManager`;
  controller-runtime therefore derives it from
  `/var/run/secrets/kubernetes.io/serviceaccount/namespace` when running
  in-cluster;
- controller-runtime's default webhook server is port `9443`, with serving
  files at `/tmp/k8s-webhook-server/serving-certs/tls.crt` and `tls.key`;
- `registerSingletonWebhook` checks whether `tls.crt` exists before registering
  the handler, but it still calls `GetWebhookServer`, so the webhook server is
  added to the manager. The server's certificate watcher requires an initially
  readable and valid keypair. A missing certificate is not a safe generic-chart
  startup mode.

The chart therefore must mount or provision the manager serving Secret at the
controller-runtime path, publish a matching CA bundle, and make readiness
source-specific. A chart-only claim that the handler is “skipped when no cert
exists” is insufficient.

### 2.3 Singleton admission findings

The repository already contains the business handler in
`internal/webhook/singleton_webhook.go`; it lists all `Kubernaut` objects and
rejects non-canonical names or a second object. The existing
`internal/resources.SingletonValidatingWebhookConfiguration` helper is not
referenced by production code according to Engram symbol-reference lookup and
needs review before reuse:

- it currently declares `ClusterScope` even though the CRD is namespaced;
- it uses a namespace-derived cluster-scoped configuration name, which permits
  multiple installations to create separate configurations;
- it uses `failurePolicy: Ignore`, while the OLM webhook definition uses
  `Fail`.

The chart implementation must resolve these conflicts explicitly. The
recommended chart contract is a deterministic cluster-wide webhook configuration
and Service name, `NamespacedScope`, and `failurePolicy: Fail` only after the
certificate/readiness gate is proven. Retaining `Ignore` would weaken the
singleton guarantee; changing the current helper is an approval-gated webhook
behavior decision.

### 2.4 Generated packaging comparison

`kustomize build config/default` currently renders 15 objects and no operator
singleton `ValidatingWebhookConfiguration`, no manager webhook Service, and no
manager serving-certificate volume. The OLM bundle obtains its webhook
behavior through the CSV `webhookdefinitions` mechanism instead. This is an
important packaging difference, not evidence that generic Kubernetes webhook
bootstrap is already complete.

## 3. Non-negotiable ownership boundary

### 3.1 Resources the production chart may own

- operator Deployment and ServiceAccount;
- the current manager ClusterRole, leader-election Role, bindings, metrics
  authorization resources, and any explicitly approved bootstrap RBAC;
- the generated `kubernauts.kubernaut.ai` CRD under the selected lifecycle
  contract;
- manager webhook Service and singleton admission configuration;
- manager serving-certificate prerequisites and source-specific CA publication;
- metrics Service, health/readiness bootstrap, namespace, labels, annotations,
  image, pull secrets, leader-election, and pod security settings;
- chart-managed development-only certificate-provisioning resources, if the
  approved design requires them;
- explicitly selected cert-manager `Issuer`/`Certificate` references or
  resources only when cert-manager is already installed.

### 3.2 Resources the production chart must not own

- a `Kubernaut` custom resource;
- application Deployments, Services, ConfigMaps, Secrets, PDBs, HPAs,
  Ingresses, Routes, runtime RBAC, migration Jobs, or provider-policy objects;
- the shared application CRDs installed during the runtime migration phase;
- PostgreSQL or Valkey workloads or their production credentials;
- Envoy AI Gateway, Kuadrant, CNI/provider operators, provider CRDs, monitoring
  operators, ingress controllers, or cert-manager;
- per-instance AuthWebhook/inter-service certificates, trust bundles, or
  application webhook configurations created after a `Kubernaut` CR is applied;
- OIDC issuer, telemetry, Fleet, service, workload, database, cache, LLM, or
  policy values copied from the upstream direct-workload chart.

Envoy AI Gateway remains an external platform dependency selected through the
user-applied CR/Fleet configuration. The bootstrap chart does not install it.

## 4. Values, naming, and compatibility contract

### 4.1 Values surface

The initial schema should be small, typed, and limited to bootstrap concerns:

- release namespace and namespace creation policy;
- fixed operator name/release identity;
- operator image registry, repository, tag, digest, pull policy, and pull
  secrets;
- ServiceAccount name and annotations;
- fixed leader-election enablement and ID, with the in-cluster namespace as the
  default source;
- metrics enablement, Service annotations, and authorization settings; the
  secure `:8443` binding is chart-owned;
- fixed health/readiness probe binding at `:8081`;
- chart-owned restricted pod/container security defaults, with only the
  platform compatibility escape hatch exposed, plus priority class, selectors,
  tolerations, affinity, topology spread, and resource requests;
- bootstrap labels and annotations;
- fixed manager webhook Service/configuration names, ports, fail-closed policy,
  and timeout, plus the certificate source;
- administrator-managed serving Secret and CA reference;
- explicit development-only self-signed profile;
- explicit cert-manager reference/provisioning profile;
- explicit OpenShift service-CA profile;
- CRD lifecycle/upgrade and uninstall safety policy.

The schema must reject application configuration keys rather than silently
accepting them. In particular, it must not expose PostgreSQL, Valkey, OIDC
issuer, OTLP telemetry, Gateway, DataStorage, API Frontend, Agent, Fleet,
monitoring destination, or application-policy configuration.

### 4.2 Names and singleton safety

The release namespace may be configurable, but the manager webhook configuration
and any other cluster-scoped singleton bootstrap objects must have deterministic
names independent of the Helm release namespace. A second installation must
fail with an actionable ownership/conflict message rather than create a second
operator that competes for cluster-scoped application resources.

The chart must not use `--take-ownership` semantics by default and must not
adopt objects created by OLM, Kustomize, another Helm release, or an
administrator. An explicit migration/adoption workflow would require a separate
approved design.

### 4.3 Application schema and packaging compatibility

No CRD type change is planned for #489. The generated CRD remains sourced from
`api/v1alpha2/` through `make manifests generate`. Helm values configure only
bootstrap; a user or GitOps controller applies the `Kubernaut` CR separately.

The upstream Helm v1.6 behavior is a design and behavior reference, not a
values-schema or rendered-manifest compatibility contract. OLM/Kustomize must
remain supported and generated artifacts must remain clean.

## 5. Bounded preflight spikes and design decisions

### 5.1 CRD lifecycle spike

The spike used Helm v3.17.3 and a disposable Kind cluster. It tested both
Helm's conventional `crds/` directory and a templated CRD with
`helm.sh/resource-policy: keep`.

**Observed `crds/` behavior:**

- first install creates the CRD;
- upgrade skips the changed CRD rather than upgrading it;
- uninstall retains the CRD;
- reinstall succeeds against the retained CRD;
- normal rendered-resource ownership conflicts fail an installation;
- an existing CRD in `crds/` is not, by itself, an adequate upgrade or
  ownership-conflict guard.

**Observed templated-CRD behavior:**

- a `v1` CRD upgraded to `v1` plus `v2` through `helm upgrade`;
- `helm uninstall` retained the CRD and its keep annotation;
- reinstall under the same release identity succeeded and retained the schema;
- Helm ownership metadata remained available for conflict detection;
- installing over a pre-existing unowned CRD failed with Helm ownership
  metadata errors instead of adopting the CRD.

**Decision:** **YES**, a templated CRD with an explicit keep policy is
technically feasible for the single-release contract and is the recommended
direction, subject to implementation tests and approval. The implementation
must:

1. render only the generated `kubernauts.kubernaut.ai` CRD;
2. add `helm.sh/resource-policy: keep` and deterministic Helm ownership
   metadata;
3. reject incompatible pre-existing CRDs rather than adopt them;
4. validate schema/version compatibility before the manager is considered
   ready;
5. test Kubernetes CRD update restrictions, storage-version behavior, and
   failed upgrades;
6. document that OLM, Kustomize, and Helm are alternative CRD owners;
7. require an explicit export/transform/recreate migration for any future
   clean-break API version, because the current CRD has no conversion webhook.

**Non-negotiable lifecycle rule:** uninstalling the operator is not operand
cleanup. The chart must retain `kubernauts.kubernaut.ai` unconditionally, and
must not delete existing `Kubernaut` CRs or runtime operand resources merely
because the Helm release is removed. A retained CRD is required because the
cluster may still contain resources whose lifecycle depends on that API, and
the operator may be reinstalled later to resume management.

**Fallback alternative:** use `crds/` for install/retention and a separately
documented, explicitly executed server-side CRD upgrade command. This is less
privileged but provides weaker single-command upgrade behavior and must include
an incompatibility guard. A separate CRD release remains a valid alternative if
the templated keep policy cannot satisfy the release process.

The chart must never rely on the operator's `EnsureCRDs` migration call to
manage its own `Kubernaut` CRD. That call concerns the shared application CRDs
after a user has applied a `Kubernaut` CR.

### 5.2 Certificate and CA feasibility spike

The source review and a scratch Helm render established:

- controller-runtime can consume a mounted Secret at
  `/tmp/k8s-webhook-server/serving-certs` without a code-level path change;
- the manager needs both `tls.crt` and `tls.key` readable at startup;
- Helm `genCA`/`genSignedCert` can technically render a CA, serving
  certificate, and key;
- two `helm template` renders generated different certificate material;
- the generated private key appeared directly in rendered output and would be
  stored in Helm release data if used as a normal Secret template;
- Helm templating alone does not patch a generic admission configuration's
  `caBundle` or provide continuous rotation.

**Decision:** **YES** for technical rendering, **NO** as the production
certificate lifecycle. Helm crypto functions must not be the production
administrator-managed or cert-manager replacement.

The recommended source contract is:

| Source | Serving Secret | CA bundle publication | Rotation/ownership |
|---|---|---|---|
| Administrator-managed | Pre-existing Secret with validated `tls.crt`, `tls.key`, and CA material. | Explicit CA input or an approved generic publisher; no OpenShift-only annotation. | Administrator-owned and never overwritten/deleted. CA rotation requires an explicit, tested publication/update step. |
| Cert-manager | Chart renders only selected `Certificate`/issuer references/resources when cert-manager is already installed. | cert-manager cainjector annotation, optionally with a validated initial bundle to avoid a readiness gap. | cert-manager owns generated output Secret and rotation; chart does not adopt it. |
| Development self-signed | An explicit namespace-scoped bootstrap provisioner creates/reuses the Secret. | The same provisioner writes/updates the chart-owned singleton webhook bundle. | Development-only, visibly annotated, idempotent, and explicitly rotatable; no silent fallback from a production source. |
| OpenShift service-CA | Only when the OpenShift profile is selected. | OpenShift service-CA injection contract. | OpenShift platform owns injected material; generic profiles emit no OpenShift annotations. |

For the development profile, the implementation should prefer a restricted
chart-owned bootstrap Job/one-shot binary built from this repository (or an
equivalent approved provisioner) over Helm template crypto. Its Role must be
limited to the named serving Secret and singleton webhook configuration, and
the manager Deployment must wait on the resulting Secret. The exact provisioner
image/entrypoint is an approval-gated implementation decision because the
current production image is `scratch` and contains only `/manager`.

The chart must test missing, malformed, expired, mismatched-SAN, partial, and
rotating material. No failed TLS source may fall back to plaintext or advertise
a fail-closed webhook as ready.

### 5.3 Webhook policy decision

The chart must render the operator singleton validating webhook, its Service,
and a source-specific CA contract. The implementation must explicitly settle:

- `NamespacedScope` for the namespaced `Kubernaut` CR;
- deterministic cluster-wide resource names;
- `failurePolicy: Fail` after certificate readiness is guaranteed, matching the
  OLM definition, or an approved documented alternative;
- a readiness condition that is not reported until the manager can serve and
  the admission configuration has a usable matching CA bundle.

The existing helper's `ClusterScope`/`Ignore` combination must not be copied
without tests and approval.

### 5.4 Uninstall-guard spike and operand retention

A disposable Kind/Helm v3.17.3 spike used a failing `pre-delete` Job:

- normal `helm uninstall --wait` returned non-zero with
  `BackoffLimitExceeded`;
- the release remained in `uninstalling` and the release-owned ConfigMap was
  still present;
- `helm uninstall --no-hooks` then removed the release successfully.

**Decision:** the failing pre-delete hook is technically feasible, but it is
**not selected as the default behavior**. Blocking operator uninstall merely
because a `Kubernaut` CR exists would not match the OLM lifecycle expectation:
removing the operator must not imply removing the operand instance. The chart
must instead:

1. allow ordinary uninstall even when `Kubernaut` CRs or finalizers exist;
2. retain the `Kubernaut` CRD and existing operand resources;
3. remove only chart-owned operator bootstrap resources;
4. preserve CR finalizers and never use uninstall to force operand cleanup;
5. document that operands are unmanaged during the operator-absent interval
   and that reinstall is the recovery/resume path;
6. optionally provide a warning-only preflight/report of existing CRs, with no
   broad RBAC and no deletion authority.

The spike's `uninstalling`/`--no-hooks` behavior remains useful negative
evidence: a blocking hook would create an operationally awkward release state
and would not provide a stronger guarantee against direct Kubernetes deletion.
It must not be used to claim that Helm can enforce operand retention by
preventing operator removal.

## 6. Upgrade, uninstall, and conflict contract

The implementation must define and test all of these journeys:

1. clean install into an empty namespace;
2. install with a user-supplied immutable image digest and pull Secret;
3. upgrade with a manager change and a CRD schema change;
4. failed manager rollout followed by reinstall/rollback;
5. install over a CRD or manager object owned by OLM/Kustomize;
6. install from a second Helm release or namespace;
7. uninstall with no `Kubernaut` CR;
8. uninstall with existing `Kubernaut` CRs and finalizers, proving that the
   CRs, CRD, and operand resources remain;
9. reinstall after uninstall with the retained CRD;
10. administrator-managed certificate rotation;
11. cert-manager certificate reissuance and CA injection;
12. development certificate regeneration;
13. cleanup of chart-owned webhook/manager resources without deletion of
    administrator-owned Secrets or operator-owned runtime application objects.

Recommended safety behavior:

- CRD retention is unconditional through the keep policy;
- uninstall succeeds without deleting existing `Kubernaut` CRs, their
  finalizers, or application runtime resources;
- uninstall removes only chart-owned operator bootstrap resources, leaving
  operands temporarily unmanaged until the operator is reinstalled;
- chart-created webhook configuration, Service, manager Deployment, RBAC, and
  chart-owned certificate provisioner objects are removed when safe;
- administrator-managed and cert-manager-owned output Secrets are never
  adopted or deleted by a generic cleanup path;
- conflicts fail before any competing cluster-scoped resource is adopted.

Any optional warning-only preflight must use narrowly scoped read access and
must not become a prerequisite for operator removal.

## 7. TDD implementation plan

Estimated implementation effort: **9–15 engineer-days**, excluding hosted
OpenShift qualification and any separate dependency chart. The range is higher
than the original estimate because the current generic manager webhook path
needs an explicit certificate/CA lifecycle rather than only Helm templates.

### Phase 0 — Contract and RED preparation (0.5–1 day)

**RED:**

- add chart ownership/render tests before production templates;
- define values schema and reject application configuration keys;
- define fixed cluster-scoped names and second-install conflict behavior;
- define CRD template/keep/upgrade and uninstall behavior;
- define certificate profiles, CA publication, rotation, and ownership;
- define webhook scope/failure/readiness behavior;
- define user/GitOps CR sequencing and the Envoy AI Gateway boundary;
- record #478/#479 exclusions and the current RBAC source.

**Gate (resolved for this execution):** the CRD strategy, certificate
provisioner, webhook failure policy, operand-retention/uninstall semantics,
values surface, and ownership policy are recorded in §13.

### Phase 1 — Chart skeleton and bootstrap boundary (1–2 days)

**RED:** `helm lint` and render tests fail until chart metadata, schema,
templates, ownership labels, and negative render assertions exist.

**GREEN:** add `charts/kubernaut-operator/Chart.yaml`, `values.yaml`,
`values.schema.json`, templates, and the generated Kubernaut CRD. Render the
operator Deployment, ServiceAccount, current manager RBAC, manager Service,
metrics/health resources, and singleton webhook configuration. Support immutable
images, pull Secrets, namespace settings, and restricted security defaults.

**REFACTOR:** make names/labels deterministic, remove duplicated values, add
actionable conflicts, and preserve OLM/Kustomize files unchanged.

### Phase 2 — CRD lifecycle and packaging ownership (1–2 days)

**RED:** install/upgrade/reinstall/uninstall/conflict tests fail until the
selected CRD behavior is encoded. Tests must prove no `Kubernaut` CR and no
shared application CRDs are rendered, while existing operand CRs survive
operator uninstall.

**GREEN:** implement the approved templated keep CRD path or the approved
fallback, compatibility checks, explicit documentation, and upgrade target.

**REFACTOR:** add schema-diff diagnostics, storage-version checks, failed
upgrade recovery, and actionable OLM/Kustomize conflict messages.

### Phase 3 — Manager webhook and certificate profiles (2–4 days)

**RED:** profile render tests, negative certificate tests, CA-bundle tests,
scope/failure-policy tests, and manager startup/readiness tests fail.

**GREEN:** mount the serving Secret at the controller-runtime path; implement
administrator-managed and cert-manager profiles; implement the approved
development provisioner; publish the matching CA bundle; and wire any required
manager entrypoint/provisioner changes through `cmd/main.go`.

**REFACTOR:** make ownership annotations explicit, prevent secret material from
appearing in values/NOTES, preserve working material during rotation, and prove
generic profiles do not emit OpenShift annotations.

### Phase 4 — Security, operations, and conflict behavior (1–2 days)

**RED:** RBAC, security-context, namespace/leader-election, metrics, image,
pull-secret, operand-retention/uninstall, and cross-installer conflict tests
fail.

**GREEN:** implement least-privilege chart RBAC and operational settings without
adding hook privileges beyond the approved contract.

**REFACTOR:** verify no secret adoption/deletion, disconnected image behavior,
fixed cluster-scoped names, structured diagnostics, and safe rollback.

### Phase 5 — Generic Helm installation journey (2–3 days)

**RED:** a separate Kind Helm lane fails until it installs without `oc`,
OpenShift APIs, monitoring CRDs, cert-manager, CNI CRDs, PostgreSQL, or Valkey.

**GREEN:** install the chart with the local immutable operator image, wait for
CRD and manager readiness, and prove that no application starts until a
user/GitOps CR is applied. The chart lane must not install Envoy AI Gateway or
provider operators.

**REFACTOR:** add upgrade, reinstall, conflict, disconnected-registry,
pull-secret, digest, certificate rotation, and retained-CRD journeys. Keep any
full application journey dependent on separately provisioned external fixtures,
not on chart-owned databases or caches.

### Phase 6 — CHECK and release evidence (1–2 days)

Run at minimum:

```text
helm lint charts/kubernaut-operator
helm template charts/kubernaut-operator --values <supported-profile>
go build ./...
golangci-lint run
make test
make test-pyramid
make manifests generate
git diff --exit-code config/ bundle/ dist/
git diff --check
```

Pin a supported Helm version in CI. The local environment used Helm 4.1.1 and
also Helm v3.17.3 for the CRD spike; the installed `helm-unittest` plugin emits
a Helm 4 `platformHooks` schema warning. Chart CI must either pin a compatible
Helm 3/plugin combination or use a maintained test harness rather than treating
that local plugin as evidence.

## 8. Wiring manifest and Checkpoint W

| Component | Production entry point | Wiring location | Required evidence |
|---|---|---|---|
| Operator Helm chart | `helm install kubernaut-operator` | `charts/kubernaut-operator/templates/**` | `IT-HELM-BOOTSTRAP-001` |
| Kubernaut CRD | Selected Helm CRD lifecycle | `templates/crd.yaml` or approved `crds/` fallback | `IT-HELM-CRD-LIFECYCLE-001` |
| Manager Deployment | Helm bootstrap | `templates/deployment.yaml`; current manager contract in `cmd/main.go` | `IT-HELM-MANAGER-001` |
| Manager ServiceAccount/RBAC | Helm bootstrap | `templates/serviceaccount.yaml`, `templates/rbac/**`, current `config/rbac/role.yaml` source | `IT-HELM-RBAC-001` |
| Manager webhook Service | Controller-runtime port `9443` | `templates/service.yaml` and serving-cert mount | `IT-HELM-WEBHOOK-001` |
| Singleton webhook configuration | User `Kubernaut` admission | `templates/webhook/configuration.yaml`; handler remains `cmd/main.go` → `internal/webhook` | `IT-HELM-SINGLETON-001` |
| Manager certificate source | Selected chart profile | `templates/certificates/**` plus approved provisioner/entrypoint | `IT-HELM-CERT-001` |
| CA-bundle publication | Selected source | `templates/webhook/**`, cert-manager annotation, or restricted publisher | `IT-HELM-CA-001` |
| Metrics/health | Manager flags and Services | `templates/metrics-service.yaml`, deployment args | `IT-HELM-OBSERVABILITY-001` |
| CRD upgrade guard | Helm lifecycle | preflight/upgrade hook or documented explicit command | `IT-HELM-CRD-CONFLICT-001` |
| Uninstall safety | Helm uninstall lifecycle | retained CRD/operand contract; optional warning-only preflight | `IT-HELM-UNINSTALL-001` |
| User application lifecycle | User/GitOps `Kubernaut` CR | existing `KubernautReconciler.Reconcile`; intentionally not chart-owned | `E2E-HELM-USER-CR-001` |
| Runtime application CRDs | Operator migration phase | existing `EnsureCRDs` call from `phaseMigrate`; intentionally not chart-owned | negative render assertion plus controller tests |
| Envoy AI Gateway | External platform installation | CR/Fleet endpoint configuration; intentionally not chart-owned | `E2E-FLEET-EAIGW-001` |

Checkpoint W fails if the chart renders a `Kubernaut` CR, shared application
CRDs, application workload, provider/operator resource, duplicate cluster-wide
webhook, or certificate owned by another source; if a new manager/provisioner
entry point has no production caller; or if no Kind installation test proves
bootstrap usability.

## 9. Test pyramid and business assertions

The chart work must preserve the project invariant: unit tests prove pure
render/policy logic, integration tests prove API/controller wiring, and E2E
proves the installation journey.

| Tier | Planned evidence | Business assertions |
|---|---|---|
| Unit/render | Ginkgo or a pinned maintained Helm render harness; schema, object identity, values rejection, no-application-resource assertions, certificate-profile and RBAC shape tests. | `BA-489-OWNERSHIP-01`, `BA-489-VALUES-01`, `BA-489-TLS-01`, `BA-489-RBAC-01` |
| Integration | envtest/controller tests for singleton admission shape, manager configuration seams, CRD compatibility diagnostics, certificate readiness/failure, and user-CR sequencing. | `BA-489-SINGLETON-01`, `BA-489-CRD-01`, `BA-489-TLS-02`, `BA-489-READINESS-01` |
| E2E | Dedicated Kind Helm install with pinned Helm v3.17.3; development, administrator-managed, cert-manager, metrics, pull-secret, disconnected bootstrap, conflict, CRD schema upgrade/rollback, failed manager rollout recovery, uninstall retention, and reinstall journeys. Hosted OpenShift separately verifies service-CA, restricted SCC startup, singleton admission, upgrade, uninstall, and reinstall. | `BA-489-INSTALL-01`, `BA-489-UPGRADE-01`, `BA-489-UNINSTALL-01`, `BA-489-DISCONNECTED-01`, `BA-489-TLS-03`, `BA-489-OCP-01` |
| CI/wiring | Helm version pin, generated-artifact diff, `make test`, `make lint`, `make test-pyramid`, release-gated Helm Kind E2E, SBOM/vulnerability checks, and a production caller for every new helper. | `BA-489-CI-01`, `BA-489-AUDIT-01` |

No pending `XIt`, `PIt`, or skipped business test is acceptable. No E2E test may
call a resource helper directly in place of installing the rendered chart.

## 10. Security and control-objective traceability

These are engineering evidence mappings, not claims of FedRAMP authorization,
SOC 2 attestation, or OWASP ASVS conformance. Extend the existing
`docs/security/ISSUE-488-CONTROL-TRACEABILITY.md` evidence model when the chart
is implemented.

| Business assertion | Required behavior | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 references |
|---|---|---|---|---|
| `BA-489-OWNERSHIP-01` | Chart renders bootstrap only; no CR, application workload, provider operator/CRD, or duplicate schema. | `CM-2`, `CM-3`, `CM-6`, `CM-8`, `SI-10` | `CC8`, `CC6` | `V15.2.4`, `V16.5.2` |
| `BA-489-RBAC-01` | Current manager RBAC is least privilege; hooks/publishers are narrower and do not gain cluster-admin privileges. | `AC-3`, `AC-6`, `CM-6` | `CC6` | `V8.2.1`, `V8.3.1`, `V13.3.2` |
| `BA-489-TLS-01` | Certificate source is explicit; missing/malformed/expired/SAN-mismatched material fails closed; no plaintext fallback. | `IA-5`, `SC-8`, `SC-12`, `SC-13`, `SC-17`, `SI-10` | `CC6`, `CC7`, `A1` | `V11.1.1`, `V11.1.2`, `V12.1.1`, `V12.1.2`, `V12.1.3`, `V12.2.1`, `V13.2.1`, `V13.3.1`, `V13.3.2` |
| `BA-489-TLS-02` | CA reaches the singleton webhook configuration and rotates without deleting the only trusted root. | `SC-8`, `SC-12`, `SC-13`, `SC-17`, `SI-4` | `CC7`, `A1` | `V11.1.1`, `V12.1.3`, `V13.2.1`, `V16.5.2` |
| `BA-489-CRD-01` | CRD upgrades are explicit and compatible; the CRD and existing operand CRs are retained on operator uninstall and never co-owned by OLM/Kustomize/Helm. | `CM-3`, `CM-6`, `CM-8`, `SI-10` | `CC8`, `A1` | `V15.2.4`, `V16.5.2` |
| `BA-489-SINGLETON-01` | Only one namespaced `Kubernaut` CR is admitted cluster-wide; a second operator install conflicts deterministically. | `AC-3`, `AC-6`, `SI-10` | `CC6`, `CC8` | `V4.1.4`, `V8.2.1`, `V16.5.2` |
| `BA-489-READINESS-01` | Generation/resource-version, certificate state, conflicts, failures, and phase transitions are observable. | `SI-4`, `AU-2`, `AU-3`, `AU-12` | `CC7`, `A1` | `V16.1.1`, `V16.5.2` |
| `BA-489-DISCONNECTED-01` | Immutable operator images, pull Secrets, and reproducible source references work without public registry assumptions. | `CM-8`, `CM-6` | `CC8`, `CC9` | `V15.2.4` |

The operator continues to provide structured log-based audit traces; the chart
does not introduce a database audit store, hash chain, retention policy, or
formal compliance evidence.

## 11. Coordination and issue tracking

### 11.1 Related operator issues

- **#488:** remains the platform/security evidence dependency. Chart work must
  extend, not duplicate, its control vocabulary and generated-artifact gates.
- **#492:** is part of the current baseline. The chart must carry the current
  manager RBAC, including EndpointSlice access, and must not regress native
  monitoring capability detection.
- **#478:** remains open and owns TLS-only OTLP telemetry behavior. #489 must
  not add telemetry endpoint or TLS-disable values to the bootstrap chart.
- **#479:** remains open and owns shared OIDC issuer configuration. #489 must
  not add an issuer value or console/API Frontend configuration to Helm; those
  remain CR/operator application configuration.

The non-production PostgreSQL/Valkey convenience chart described by the
platform ADR is a separate issue and is not a dependency of this chart.

### 11.2 Closed #489 disposition

GitHub currently reports #489 as closed with `state_reason: completed` after
planning-only PR #506 merged. PR #506 explicitly states that it added no Helm
chart or production code. The implementation work is now being executed in this
worktree under the same acceptance criteria; issue state remains unchanged.

No GitHub issue or pull-request mutation is part of this implementation session.
The repository owner may reopen #489 or create a linked implementation issue
after reviewing the uncommitted changes.

## 12. Success criteria

The implementation is complete only when:

- supported Helm lint/render and schema checks pass;
- the chart installs on Kind without `oc`, OpenShift APIs, monitoring CRDs,
  cert-manager, CNI CRDs, PostgreSQL, or Valkey;
- manager and CRD readiness are proven;
- the manager webhook has a valid serving keypair and source-matching CA
  bundle before a fail-closed singleton webhook is advertised;
- applying a user-owned `Kubernaut` CR starts the existing operator lifecycle;
- the chart renders no `Kubernaut` CR, application workload, dependency
  workload, runtime application CRD, provider CRD/operator, provider policy, or
  duplicated application schema;
- administrator-managed, cert-manager, development, and selected OpenShift
  certificate paths have fail-closed negative and rotation evidence;
- CRD upgrade, retention, uninstall, reinstall, and ownership conflicts are
  explicit and tested;
- ordinary uninstall succeeds with existing CRs/finalizers while retaining the
  CRD, operand CRs, and runtime resources; the unmanaged interval and reinstall
  recovery path are documented;
- disconnected images, immutable digests, pull Secrets, security context,
  namespace/leader-election, metrics, and least-privilege RBAC are covered;
- OLM/Kustomize installation remains unchanged and `config/`, `bundle/`, and
  `dist/` generated checks are clean;
- #478 and #479 application configuration remains outside the bootstrap values;
- Envoy AI Gateway remains externally owned and is tested only through the CR
  configuration contract;
- generic Kind and OpenShift bootstrap evidence are recorded separately; and
- all residual risks and external qualification requirements are documented.

## 13. Approval gates and implementation decision record

The execution directive for this work resolved the implementation gates as
follows:

1. **CRD:** use the templated CRD with unconditional
   `helm.sh/resource-policy: keep`; Helm owns upgrades only when ownership
   metadata matches this release. OLM/Kustomize-owned CRDs are not adopted.
2. **Development TLS:** use a pinned `bitnami/kubectl` provisioner image in a
   pre-install/pre-upgrade bootstrap Job with a short-lived, namespace-scoped
   ServiceAccount. The manager mounts the generated Secret directly and never
   runs the certificate image. A separate restricted post-install/
   post-upgrade publisher waits for manager readiness, patches and verifies the
   webhook CA, and attaches the development Secret to the manager Deployment so
   Kubernetes garbage-collects only that Secret when the manager is removed.
   Generated private keys never enter Helm values or rendered release data, and
   uninstall does not depend on pulling the bootstrap image.
3. **Production TLS profiles:** administrator-managed, cert-manager, and
   OpenShift service-CA are explicit modes. The chart never adopts or deletes
   administrator- or cert-manager-owned output Secrets.
4. **Webhook:** use deterministic cluster-scoped names, `Namespaced` scope,
   and `failurePolicy: Fail`. Injected CA fields are intentionally omitted from
   Helm ownership for development, cert-manager, and OpenShift modes so their
   publishers can update them safely across upgrades.
5. **Uninstall:** ordinary uninstall is non-destructive. The CRD, user CRs,
   finalizers, runtime resources, and operand sentinel survive; only chart-owned
   operator resources are removed. Reinstall is the recovery path.
6. **Conflicts:** fixed cluster-scoped names and Helm ownership metadata cause a
   second release or another installer to fail rather than adopt resources.
7. **Values:** the JSON schema accepts only bootstrap settings and rejects
   application configuration such as PostgreSQL. Operational invariants are
   chart-owned rather than user-overridable: leader election is enabled,
   health probes bind to `:8081`, metrics use secure `:8443` when enabled, the
   webhook is always enabled with fixed names/ports and `failurePolicy: Fail`,
   and the restricted manager security context is fixed. Related images remain
   immutable, overridable references consumed by the manager only after a user
   applies a `Kubernaut` CR.
8. **Harness:** use Ginkgo render tests, Helm v3.17.3 in CI, and a dedicated
   Kind journey; local validation additionally exercised Helm 4.1.1.

Implementation evidence now includes the chart render suite, source-sync checks,
`make test`, `make lint`, a release-gated operator-only Helm Kind journey,
pinned Helm v3.17.3 Kind journeys, Helm conflict/upgrade/rollback/
failed-rollout/uninstall/reinstall coverage, all three TLS profiles, live secure
metrics scraping, image pull-secret rendering and use, disconnected bootstrap
image loading, generated-artifact/build/lint checks, and the hosted OpenShift
service-CA lane on OpenShift 4.22.16. The OpenShift lane also verified
restricted-SCC startup, singleton admission denial, certificate reuse, CR and
operand retention, and reinstall. No production controller code or
OLM/Kustomize packaging was changed.

The current revision was requalified on 2026-10-08: the generic Kind
development, disconnected, manual, and cert-manager journeys each passed all
six ordered scenarios, and the hosted OpenShift 4.22.16 service-CA journey
passed. The independent unit lane reports 87.2% overall and 88.1% for
`internal/resources`; controller integration coverage is 79.3%. These exceed
the repository's configured 80% CI floor where it applies, but
the broader 96% methodology target remains an explicit follow-up and is not
claimed as met here. The default `1.6.0-rc20` manager tag also remains a
pre-release source-tree reference until the release pipeline publishes its
immutable manifest; production and disconnected users should provide a digest.

Residual risks are limited to registry-specific authentication/proxy behavior
outside the tested local-disconnected image path, certificate-provider policy
choices outside the exercised self-signed cert-manager Issuer and
administrator-generated CA, and application/operand lifecycle behavior that is
intentionally outside this operator-only chart.

**Implementation confidence: 94%.** The chart and lifecycle contracts are
covered by unit/render tests, pinned Helm Kind journeys for development,
manual, cert-manager, metrics, pull-secret, disconnected, CRD rollback, and
failed-rollout recovery paths, plus live OpenShift service-CA qualification.
The remaining uncertainty is the repository-wide coverage target and release
image publication/registry policy rather than an untested chart control path.

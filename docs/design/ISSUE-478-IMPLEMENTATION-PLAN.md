# Issue #478 — Require TLS for configured OTLP telemetry exports

**Status:** Implemented; final verification and release review in progress

**Date:** 2026-10-07

**Scope:** `kubernaut-operator` v1alpha2 CRD, generated telemetry ConfigMaps,
and the Gateway, DataStorage, and Kubernaut Agent Deployment vectors

**Related:** upstream `kubernaut#2384`, operator `#479`, `#489`, `#491`, and
`#498`

> This document preserves the preflight findings and approved implementation
> sequence. The production implementation and generated artifacts now live in
> the repository; the completion record is maintained at the end of this
> document.

## 1. Executive decision

The operator must mirror the completed upstream `kubernaut#2384` contract:

1. An empty telemetry endpoint disables OTLP network export.
2. `logSink: true` with an empty endpoint remains valid and is local-only.
3. `endpoint: stdout` remains a local debugging/log-sink mode and does not
   require TLS material.
4. A network endpoint is represented as `host:port` without a URI scheme and
   always uses TLS with certificate verification. `http://` and `https://`
   endpoint forms are rejected rather than interpreted as transport switches.
5. `TelemetryTLSConfig.Enabled` is removed. There must be no CRD or rendered
   configuration field that can disable TLS for a network export.
6. System CA trust is the default for public collectors. An administrator may
   select a private CA file or an operator-mounted Secret-backed CA. Client
   certificate authentication is optional, but certificate and key material
   must be supplied together and must be valid.
7. One shared telemetry renderer and one shared telemetry Secret-mount helper
   serve Gateway, DataStorage, and Kubernaut Agent. No component may implement
   a separate TLS policy.
8. Validation remains controller-driven through `phaseValidate` and
   `resources.ValidateKubernaut`; this repository has no telemetry admission
   webhook to extend.
9. Ambient trust must follow the upstream operand contract: system roots plus
   inter-service trust are made available before telemetry bootstrap, without
   assigning the narrow inter-service CA file directly to `SSL_CERT_FILE`.
   Telemetry `CAFile` remains a separate, explicit trust source.
10. Rollout is the operator's correctness boundary for every effective OTLP
    trust/client-material revision, including ambient trust that feeds system
    CA lookup. Upstream hot reload remains an optimization for non-OTLP
    clients, not a reason to leave an OTLP provider running with stale
    startup material.

The recommended operator API is a narrow extension of the existing upstream
shape, not a second TLS lifecycle:

```go
type TelemetryTLSConfig struct {
    // Enabled is removed.
    CAFile             string                        `json:"caFile,omitempty"`
    CACertSecretRef    *CACertSecretRef             `json:"caCertSecretRef,omitempty"`
    CertFile           string                        `json:"certFile,omitempty"`
    KeyFile            string                        `json:"keyFile,omitempty"`
    TLSClientSecretRef string                        `json:"tlsClientSecretRef,omitempty"`
}
```

The exact field-name approval is still required. `CACertSecretRef` reuses the
existing `{name,key}` type and Secret/PEM validation from the #491/#498 trust
foundations. `TLSClientSecretRef` follows the existing operator convention in
`LLMProfileSpec`: the referenced Secret contains `tls.crt` and `tls.key`.

The source rules are deliberately explicit even though the compact API uses
the existing path fields:

| CR fields | Effective trust source | Operator behavior |
|---|---|---|
| Neither `caFile` nor `caCertSecretRef` | System CA | Render no `caFile`; do not add a telemetry CA volume. |
| `caFile` only | Administrator-owned file | Require an absolute path and render it. The operator does not claim ownership or validate a file outside the Pod. |
| `caCertSecretRef` only | Operator-mounted private CA | Validate the selected Secret key as PEM, mount it read-only at a deterministic path, and render that path. Never adopt or mutate the Secret. |
| Both CA fields | Ambiguous | Reject with an actionable validation error. |
| No client paths and no `tlsClientSecretRef` | No mTLS | Render no client material. |
| `certFile` and `keyFile` plus `tlsClientSecretRef` | Secret-backed mTLS | Require absolute paths with one parent directory, validate the Secret key pair, mount `tls.crt`/`tls.key` read-only at those paths, and render the same paths. |
| Any incomplete client combination | Invalid | Reject before deployment. |

The delegated #499 reassessment confirmed that #499 completed with a
Fleet-specific `FleetTrustSpec`; it did not extract a domain-neutral trust type.
Issue #478 should therefore not reuse `FleetTrustSpec` directly and should not
expand into a new generic trust refactor. The narrow telemetry extension above
is the recommended boundary unless a separate approved design changes it.

## 2. Preflight evidence and current gap

The baseline is clean commit `55f71eb`; the worktree was unmodified before
this planning document. The following production paths were inspected.

| Area | Evidence | Finding and impact |
|---|---|---|
| API contract | `api/v1alpha2/kubernaut_types.go`, `TelemetrySpec`, `TelemetryTLSConfig` | `Enabled *bool` defaults false and explicitly permits plaintext. Existing fields are paths only. |
| API generation | `api/v1alpha2/zz_generated.deepcopy.go` | `TelemetryTLSConfig.DeepCopyInto` copies `Enabled`; it must be regenerated after the type change. |
| Shared renderer | `internal/resources/configmaps.go`, `telemetryYAML`, `resolveTelemetryConfig` | Gateway, DataStorage, and Kubernaut Agent all use one renderer, but it currently emits `tls.enabled` and drops the entire block whenever `Endpoint` is empty, which prevents log-sink-only mode. |
| Gateway | `GatewayConfigMap` and `GatewayDeployment` | Gateway telemetry is rendered from `spec.gateway.config.telemetry`; its Deployment currently has inter-service trust volumes but no telemetry-specific CA/client Secret mount. |
| DataStorage | `DataStorageConfigMap`, `DataStorageDeployment`, `dataStorageVolumesAndMounts` | DataStorage telemetry is rendered from `spec.dataStorage.telemetry`; existing PostgreSQL/Valkey/TLS mounts are unrelated to OTLP trust. |
| Kubernaut Agent | `KubernautAgentConfigMap`, `KubernautAgentDeployment`, `kaCoreVolumesMountsEnv` | Kubernaut Agent telemetry is rendered from `spec.kubernautAgent.telemetry`; its LLM and monitoring trust mounts must remain separate from OTLP material. |
| Validation | `internal/resources/validation.go`, `ValidateKubernaut` | No telemetry validation currently exists. Static validation is called by `phaseValidate`; Secret-backed Fleet validation provides the established controller pattern. |
| Controller wiring | `internal/controller/kubernaut_controller.go`, `phaseValidate`, `buildCoreConfigMaps`, `appendOptionalComponentConfigMaps` | ConfigMaps are reconciled through the existing hash/rollout path. Validation must stop before `phaseDeploy` on invalid telemetry material. |
| Trust helpers | `internal/resources/common.go`, `deployments.go`, `FleetTrustSpec` and `CACertSecretRef` consumers | Source-aware CA semantics, read-only Secret mounts, and PEM validation already exist for Fleet/TLS work and should be reused. |
| Environment trust | `appendInterServiceTLSCA`, `TrustBundleConfigMap`, and component Deployment builders | OCP injects service CA and the operator's trust bundle adds the router CA, but Gateway/DataStorage currently point `SSL_CERT_FILE` at that narrow bundle, which replaces public system roots. It must not be overwritten with an external telemetry CA; it must instead be replaced by upstream-style ambient system-plus-inter-service trust. |
| Generated artifacts | `config/crd/bases`, `bundle/manifests`, `dist/install.yaml` | All three contain the obsolete `telemetry.tls.enabled` schema. Runtime ConfigMaps/Deployments are generated by the controller, not checked-in YAML. |
| Tests | `internal/resources/configmaps_test.go`, `common_test.go`, `deployments_test.go`, `validation_test.go` | Existing fixtures cover basic path rendering, but not fail-closed behavior, log-only mode, Secret mounts, invalid material, or all three Deployment vectors. |
| Documentation audit | `docs/tests/421/CRD_FIELD_COVERAGE_AUDIT.md` | The historical #421 snapshot explicitly lists telemetry fields as partial; it is not a live tracker and should not be silently rewritten as implementation evidence. |

The operator currently has no telemetry-producing component beyond Gateway,
DataStorage, and Kubernaut Agent. A narrow symbol search confirmed all three
ConfigMap call sites and no additional `TelemetrySpec` consumer.

## 3. Upstream and compatibility contract

The upstream issue `kubernaut#2384` is the behavior source of truth. Its
completed contract states:

- `Endpoint` remains `host:port` with no scheme.
- Empty endpoint plus `LogSink=false` is fully disabled.
- Empty endpoint plus `LogSink=true` is local log export.
- `Endpoint=stdout` is local debugging output.
- Network export always uses TLS and validates the server certificate.
- System trust is valid; explicit private CA is supported.
- Client certificate/key are optional and must be supplied together.
- `TelemetryTLSConfig.Enabled` and the rendered `telemetry.tls.enabled` field
  are removed.

The operator must not add operator-only behavior that diverges from that
runtime contract. The operator-specific Secret references exist only to make
the upstream path fields real inside generated Pods.

## 4. Alternatives and spike decisions

### Spike A — Network endpoint representation

**Decision: YES to the upstream host:port contract; reject schemes.**

The upstream exporter selects TLS in its network path, not from an
`https://` prefix. Accepting arbitrary endpoint strings in the CR currently
allows an ambiguous or malformed configuration. The validation helper should
accept `stdout` as the explicit local exception, otherwise reject a URI scheme,
whitespace, or an endpoint that cannot be represented as the upstream network
endpoint. The error should say that the endpoint must be `host:port` and that
TLS is implicit and mandatory.

Therefore, the acceptance criterion’s phrase “HTTPS OTLP endpoint” means a
network export whose transport is HTTPS/TLS at runtime; it does not mean that
the CR should accept an `https://` URI in the `endpoint` field.

The delegated runtime spike also confirmed that the upstream exporter does not
itself provide a strong scheme-validation error. The operator must own this
validation; it must not rely on malformed URL handling in the exporter.

### Spike B — Private CA and mTLS material

| Option | Assessment | Decision |
|---|---|---|
| A. Keep path-only fields and always force TLS | Smallest diff, but the operator cannot make a private CA or client key path exist in a generated Pod. It leaves the current dead-path gap. | Reject as incomplete. |
| **B. Preserve upstream paths and add typed CA/client Secret references** | Keeps the upstream ConfigMap shape, makes operator-mounted material explicit, reuses `CACertSecretRef` and existing read-only/PEM validation, and supports private CA plus mTLS. | **Recommended, pending API approval.** |
| C. Reuse `FleetTrustSpec` directly under telemetry | Shares fields but leaks Fleet domain terminology and couples independent runtime lanes. | Reject; #499 completed without a domain-neutral type. |
| D. Reuse only the inter-service CA | Avoids new fields but cannot represent a private OTLP collector trust root and conflates trust domains. | Reject. |
| E. Add a new operator-owned CA/certificate lifecycle | Could provide material automatically, but duplicates #491/#498 ownership, rotation, and cleanup work. | Reject. |

### Spike C — Secret ownership and validation

**Decision: Secret references are administrator-owned and read-only.**

The operator validates referenced keys before deployment, mounts them read-only,
does not set owner references, and never creates, updates, or deletes them.
Missing Secrets, missing keys, invalid CA PEM, and invalid client key pairs must
produce an actionable status condition through `phaseValidate`. This matches
the established `validateFleetTrustSecrets` path and the ownership guarantees
documented by #491/#498.

### Spike D — API version migration

**Decision: no v1alpha1 conversion or compatibility shim.**

`v1alpha2` is the sole served/storage API with a documented clean-break
export/transform/recreate migration. Removing `telemetry.tls.enabled` is
therefore a v1alpha2 schema change, not a conversion-webhook change. Existing
manifests with the removed field must be updated; an endpoint that was
previously plaintext must be migrated to a TLS-capable collector before the
new operator accepts it.

### Spike E — Validation entry point

**Decision: retain controller-driven validation.**

There is no `internal/webhook/` implementation for this contract. Static CR
validation belongs in `resources.ValidateKubernaut`; cluster-dependent Secret
content validation belongs beside `validateFleetTrustSecrets` in
`internal/controller/kubernaut_controller.go`. `phaseValidate` must complete
both before status is marked ready or deployment begins.

### Spike F — Ambient system CA versus process-wide trust environment

**Decision: YES to the upstream ambient-CA path, with operator configuration
parity; do not use the current narrow static `SSL_CERT_FILE` value.**

The current OCP path is:

1. `InterServiceCAConfigMap` is annotated with
   `service.beta.openshift.io/inject-cabundle=true`; the OpenShift service-ca
   operator populates `service-ca.crt`.
2. The operator's `TrustBundleConfigMap` merges that service CA with the
   cluster ingress/router CA, but does not include the container's public
   system roots.
3. Gateway and DataStorage currently set both `TLS_CA_FILE` and
   `SSL_CERT_FILE` to `/etc/tls-ca/service-ca.crt` (or its equivalent custom
   path). Because Go treats `SSL_CERT_FILE` as a replacement for the system
   trust store, a public OTLP collector can fail even when telemetry has an
   empty `caFile`.
4. Kubernaut Agent and API Frontend already have a special init-container
   path that concatenates the image system bundle with the operator trust
   bundle; that is why their public-CA behavior is broader, but it is not a
   single shared approach for all telemetry producers.

The pinned upstream `v1.6.0-rc22` operand and Helm chart use a better path:

- the chart mounts the inter-service CA and a writable `/tmp`, but does not
  declare a narrow static `SSL_CERT_FILE`/`TLS_CA_FILE` in the Pod;
- Gateway renders top-level `tlsCaFile`, Kubernaut Agent renders
  `runtime.server.tlsCaFile`, and DataStorage uses its resolved
  `redis.tls.caFile` as the ambient source;
- each process calls `sharedtls.InjectAmbientCACerts` immediately after
  loading configuration and before `telemetry.Bootstrap` or another outbound
  TLS operation;
- `InjectAmbientCACerts` validates the configured CA, concatenates it with
  the image's system bundle, writes a temporary combined PEM under `/tmp`,
  and sets process-local `SSL_CERT_FILE` and `TLS_CA_FILE` to the resolved
  values. This is upstream issue #2276's fix for public-CA regression.

The implementation should mirror that operand contract:

- render the upstream ambient source fields for Gateway and Kubernaut Agent;
- provide DataStorage's resolved inter-service CA path through the existing
  `redis.tls.caFile` source without enabling Valkey TLS as a side effect when
  the operator's Valkey mode is disabled;
- ensure Gateway has the writable `/tmp` volume required by the upstream
  process bootstrap (DataStorage and Kubernaut Agent already have one);
- remove the narrow static `SSL_CERT_FILE` assignments from the affected
  operator Pods, and do not place telemetry `CAFile` or client material in
  process-wide inter-service trust variables;
- preserve the Kubernaut Agent's existing merged-bundle behavior until the
  upstream-equivalent config path is proven for both OCP and generic TLS
  modes.

Validation must cover public/system CA, private telemetry `CAFile`, OCP
service-plus-router trust, generic inter-service trust, and mTLS. The
upstream runtime qualification is now complete; a live collector handshake
remains an implementation verification gate, not an unresolved design choice.

### Spike G — External Secret, ambient trust rotation, and rollout

**Decision: use a shared telemetry-material revision and controlled rollout for
all effective OTLP trust/client-material changes. Do not assume upstream OTLP
hot reload.**

The upstream rotation matrix is:

| Material/path | Upstream behavior | #478 consequence |
|---|---|---|
| Inter-service CA used by shared HTTP transports | `StartCAFileWatcher` watches `TLS_CA_FILE`; `CAReloader` atomically swaps the client transport and keeps the prior transport on invalid PEM. | Covered for clients that use the shared transport; it does **not** reload the OTLP exporter’s custom TLS config. |
| Inbound server `tls.crt`/`tls.key` | `CertReloader` plus a file watcher reloads the certificate without restarting the server; an invalid rotation preserves the previous certificate. | Not evidence that OTLP client certificates hot-reload. |
| Telemetry `CAFile` | `telemetry.Bootstrap` calls `BuildClientTLSConfig` once; a non-empty CA is parsed into a static `RootCAs` pool. | Requires a rollout to adopt a changed CA. |
| Telemetry `certFile`/`keyFile` | `tls.LoadX509KeyPair` runs while the tracer provider is created; no watcher or `GetClientCertificate` callback is wired. | Requires a rollout to adopt changed client credentials. |
| Kubernetes Secret volume bytes alone | Kubelet refreshes mounted files, but the already-running OTLP transport/provider does not reread them. | Volume refresh alone is insufficient. |
| Ambient system/inter-service trust used by a default-trust OTLP exporter | Ambient CA injection occurs before provider construction; later shared-transport reload does not rebuild the OTLP provider's startup trust state. | An effective ambient-bundle revision is also a rollout input for network telemetry producers. |

The operator should therefore:

- watch administrator-owned Secrets referenced by active telemetry lanes and
  the effective ambient trust source(s) used by the managed Pods, mapping
  changes to the singleton Kubernaut CR. Watching is read-only and never
  adopts or mutates an administrator-owned object;
- validate the new Secret contents in `phaseValidate` before changing any
  Deployment;
- validate the effective ambient trust material before accepting its revision
  for rollout;
- compute one shared, deterministic telemetry-material revision from the
  effective ambient revision and referenced Secret identity/version metadata.
  Put only non-sensitive metadata in the Pod-template annotation; never put
  Secret data or a data-derived secret hash in a rendered resource;
- roll out every affected network-enabled telemetry producer after a valid
  revision. For an ambient revision, the conservative v1 behavior may include
  all network-enabled telemetry producers that execute ambient trust
  bootstrap; local-only lanes never roll for telemetry material;
- retain upstream hot reload for inter-service clients and inbound
  certificates, but do not treat it as sufficient for OTLP exporter state;
- leave the existing working Deployment untouched when a changed Secret is
  missing, malformed, or has a mismatched client pair, or when ambient trust
  material is invalid. The reconciliation status must report the invalid
  revision, but must not deliberately delete or replace the last known-good
  workload;
- retain the documented manual restart/rollout contract for administrator
  file paths (`caFile`, `certFile`, `keyFile`) that are outside operator-owned
  Secret references.

This provides one operational rule across explicit and implicit OTLP trust
inputs: hot reload may reduce disruption for other clients, but a valid
effective telemetry-material change gets a controlled rolling update. The
operator still targets only affected producers rather than restarting every
workload for every unrelated Secret event.

## 5. Proposed implementation shape

### 5.1 API and resolution rules

Update `TelemetryTLSConfig` in `api/v1alpha2/kubernaut_types.go` as follows:

1. Delete `Enabled`, its false default marker, and comments describing
   plaintext operation.
2. Retain `CAFile`, `CertFile`, and `KeyFile` because they are part of the
   upstream runtime YAML contract.
3. Add `CACertSecretRef *CACertSecretRef` for a namespaced private CA Secret.
4. Add `TLSClientSecretRef string` for a namespaced mTLS Secret containing
   `tls.crt` and `tls.key`, matching the existing LLM profile contract.
5. Document that an empty CA selection uses system trust and that a network
   endpoint never permits plaintext.

The resolver in `internal/resources/configmaps.go` must apply the same rules
for all three components:

- Render no telemetry block only when both `Endpoint` is empty and `LogSink`
  is false.
- Render `logSink: true` without a network endpoint when requested.
- Render `stdout` as the upstream local mode without a telemetry CA/client
  mount.
- For a network endpoint, render no `enabled` key and always render the
  upstream TLS shape. An empty `caFile` means system CA trust.
- For `CACertSecretRef`, resolve the deterministic `/etc/telemetry/ca.crt`
  path and use it in both the ConfigMap and the matching Deployment mount.
- For `TLSClientSecretRef`, use `/etc/telemetry/tls.crt` and
  `/etc/telemetry/tls.key` (or require both configured paths to resolve to that
  common directory) in both the ConfigMap and Secret volume items.
- Never place private key bytes in a ConfigMap, environment variable, log, or
  status message.
- Render the upstream ambient inter-service trust source separately from the
  telemetry block: Gateway `tlsCaFile`, Kubernaut Agent
  `runtime.server.tlsCaFile`, and DataStorage's ambient `redis.tls.caFile`
  source. Mount a writable `/tmp` wherever the operand's
  `InjectAmbientCACerts` path can run.

The shared helper should return the effective rendered paths and the Secret
volume/mount additions as one contract, or expose equivalent pure helpers so a
ConfigMap cannot render a path that its Deployment does not mount.
Deployment construction must receive a controller-resolved telemetry
material revision. Resource builders must not read administrator-owned Secret
data merely to calculate a rollout annotation.

### 5.2 Static validation

Add telemetry validation to `internal/resources/validation.go` and call it
from `ValidateKubernaut` for each active telemetry lane. It should:

- reject URI schemes, whitespace, malformed host/port values, and any endpoint
  that would select plaintext;
- accept empty endpoint and `stdout` without requiring network TLS material;
- reject `caFile` and `caCertSecretRef` together;
- require an absolute `caFile` when it is used as an administrator-owned file
  path;
- require `certFile` and `keyFile` together;
- require `tlsClientSecretRef` whenever the client pair is configured and
  reject a client Secret reference without the pair;
- require absolute client paths with one common parent directory so the two
  Secret keys can be mounted deterministically;
- provide field-qualified errors such as
  `spec.gateway.config.telemetry.tls.certFile and ...keyFile must be set together`;
- treat Gateway as active only when `spec.gateway.enabled` resolves true, while
  always validating DataStorage and Kubernaut Agent telemetry shape.

The validator must not use the removed `Enabled` field as a compatibility
fallback. No default, nil pointer, or omitted field may select plaintext.

### 5.3 Cluster-dependent Secret validation

Add a controller helper, modelled on `validateFleetTrustSecrets`, that runs in
`phaseValidate` after static validation and before TLS/deployment readiness:

- iterate the effective Gateway, DataStorage, and Kubernaut Agent telemetry
  specs;
- skip Secret reads for disabled/log-only/stdout-only lanes with no network
  export;
- read CA Secret keys (default `ca.crt`) and append them to an x509 pool;
- read `tls.crt` and `tls.key` for mTLS references and validate them with
  `tls.X509KeyPair` plus certificate parsing;
- report missing Secret, missing key, invalid PEM, or mismatched key material
  through `ConditionBYOValidated=False` with a telemetry-specific reason and
  field-qualified message;
- validate the effective ambient trust source before advancing its rollout
  revision; an invalid ambient bundle must preserve the existing Deployment;
- never claim that a `caFile` outside the Pod was inspected;
- do not add ownership or cleanup behavior for administrator Secrets.

The validation log must use `log.FromContext(ctx)` with component, field path,
generation, and resourceVersion. It must not log certificate or key contents.

### 5.4 Deployment wiring

Add one shared telemetry mount helper in `internal/resources/deployments.go`
(or a narrowly scoped companion in `internal/resources/`) and call it from:

- `GatewayDeployment`;
- `DataStorageDeployment` / `dataStorageVolumesAndMounts`;
- `KubernautAgentDeployment` after its existing baseline volumes are built.

The helper must:

- add a CA Secret volume only for a network endpoint with
  `caCertSecretRef`;
- add a client Secret volume only for a network endpoint with
  `tlsClientSecretRef`;
- prefer one projected `telemetry-material` volume mounted read-only at
  `/etc/telemetry`, with explicit `ca.crt`, `tls.crt`, and `tls.key` item
  mappings from the selected Secret sources;
- map only the required keys and never mount separate volumes over the same
  directory;
- use deterministic volume names that cannot collide with `tls-ca`,
  `llm-tls-client`, `valkey-*`, or component config volumes;
- preserve the existing inter-service trust semantics, but do not assume the
  current narrow `SSL_CERT_FILE` values preserve public system trust; the
  upstream ambient-CA injection path must replace those narrow static values;
- avoid breaking the Kubernaut Agent combined system/inter-service CA used by
  LLM, Prometheus, and Alertmanager clients;
- add no telemetry volumes for disabled, log-sink-only, or stdout-only modes.
- add a non-sensitive telemetry-material revision annotation to the Pod
  template when a network telemetry lane has an effective Secret or ambient
  trust source; remove it for local-only lanes.

The ConfigMap and Deployment tests must assert exact path alignment. This is
the primary protection against the current class of “rendered path but no
mounted file” failure.

## 6. TDD implementation plan

Follow the repository’s wiring-first sequence: **RED IT → RED UT → GREEN
wiring → GREEN behavior → REFACTOR**. The estimates are planning estimates,
not implementation evidence.

### RED — integration tests first (45–60 minutes)

Add or extend `internal/controller/telemetry_wiring_integration_test.go` with
Ginkgo/Gomega envtest coverage:

- `IT-TELEMETRY-TLS-001`: a CR with network telemetry for Gateway, DataStorage,
  and Kubernaut Agent reconciles to ConfigMaps without `tls.enabled` and to
  Deployments with the expected CA/client mounts;
- `IT-TELEMETRY-TLS-002`: a missing CA Secret, missing key, invalid CA PEM,
  missing client key, or mismatched client pair prevents validation readiness
  and produces an actionable condition before deployment;
- `IT-TELEMETRY-TLS-003`: empty endpoint, log-sink-only, and stdout-only
  configurations reconcile without network Secret requirements or telemetry
  mounts;
- `IT-TELEMETRY-TLS-004`: all three component vectors use the same source/path
  rules, including Gateway’s optional enablement;
- `IT-TELEMETRY-TLS-005`: changing the CR’s telemetry source/path changes the
  rendered ConfigMap/Deployment desired state and rollout hash without
  adopting the referenced Secret.
- `IT-TELEMETRY-TLS-006`: a real or equivalent upstream runtime proves public
  system-CA export still succeeds with the component’s inter-service trust
  environment, while private-CA and mTLS cases use only their explicit
  telemetry material. It also proves the Gateway/DataStorage/Kubernaut Agent
  ambient source fields and writable `/tmp` wiring.
- `IT-TELEMETRY-TLS-007`: an external OTLP CA/client Secret update enqueues
  validation, a valid update changes only non-sensitive rollout metadata and
  restarts affected Pods, and an invalid update preserves the existing
  Deployment while reporting the failure.
- `IT-TELEMETRY-TLS-008`: an effective ambient trust-source update causes a
  controlled rollout of network-enabled telemetry producers, does not roll
  local-only lanes or unrelated workloads, and preserves the existing
  Deployment when the new bundle is invalid.

The integration tests must exercise the real reconciliation path, not call only
the resource builders. If the existing lifecycle fixtures require unrelated
BYO objects, use the established test fixture builders rather than bypassing
`phaseValidate`.

### RED — resource/API unit tests (45–60 minutes)

Extend the existing white-box suites before implementation:

- `internal/resources/validation_test.go`: endpoint matrix, removed disable
  field, CA source exclusivity, absolute paths, complete/incomplete client
  pair, and field-qualified errors;
- `internal/resources/configmaps_test.go`: disabled, log-only, stdout, system
  CA, private `caFile`, Secret-backed CA, and mTLS YAML for Gateway,
  DataStorage, and Kubernaut Agent;
- `internal/resources/deployments_test.go`: exact CA/client Secret volume
  sources, key mappings, read-only mounts, path alignment, no mounts for local
  modes, and preservation of inter-service environment variables;
- `internal/resources/common_test.go`: update `telemetrySpecFixture` and
  assertion helpers to use the production `telemetryYAML` types, not a copied
  anonymous mirror;
- `api/v1alpha2/clean_break_test.go` or a focused API test: only v1alpha2 is
  served/stored and the removed field is absent from the generated schema;
- `internal/controller/kubernaut_lifecycle_test.go` or the new telemetry
  integration file: status reason/message and no-deploy behavior for invalid
  Secret material.

### GREEN — minimum wiring (60–90 minutes)

1. Add the approved API fields and write the failing generated/deepcopy tests.
2. Wire the shared telemetry resolver into the existing three ConfigMap builders.
3. Wire the shared Secret-mount helper into the three Deployment builders.
4. Add static validation to `ValidateKubernaut`.
5. Add cluster-dependent Secret validation to `phaseValidate`.
6. Run Checkpoint W: every new helper has a production caller, focused unit
   assertions, and an envtest reconciliation assertion. Do not declare GREEN
   with only resource tests passing.

### REFACTOR — production quality (45–60 minutes)

- centralize endpoint/path/source resolution so the three components cannot
  drift;
- keep functions below the repository’s parameter/nesting limits by using a
  small telemetry material/config struct rather than adding long signatures;
- preserve deterministic volume ordering and stable ConfigMap hashes;
- use wrapped lowercase errors and structured controller logging;
- ensure no error is ignored and no private material is included in errors;
- remove stale comments, sample fields, and security documentation that imply
  plaintext OTLP is supported;
- run the complete build, lint, test, manifest, bundle, and generated-diff
  gates before declaring the implementation complete.

## 7. Wiring manifest and Checkpoint W

| Component/behavior | Production entry point | Wiring location | Required IT evidence |
|---|---|---|---|
| Shared telemetry YAML resolution | `resolveTelemetryConfig` | `internal/resources/configmaps.go`; called by `GatewayConfigMap`, `DataStorageConfigMap`, and `KubernautAgentConfigMap` | `IT-TELEMETRY-TLS-001`, `004` |
| Gateway ConfigMap telemetry | `GatewayConfigMap` | `internal/resources/configmaps.go`; reached through `appendOptionalComponentConfigMaps` | `IT-TELEMETRY-TLS-001` |
| DataStorage ConfigMap telemetry | `DataStorageConfigMap` | `internal/resources/configmaps.go`; reached through `buildCoreConfigMaps` | `IT-TELEMETRY-TLS-001` |
| Kubernaut Agent ConfigMap telemetry | `KubernautAgentConfigMap` | `internal/resources/configmaps.go`; reached through `buildCoreConfigMaps` | `IT-TELEMETRY-TLS-001` |
| Shared telemetry CA/client mounts | telemetry Deployment mount helper | `internal/resources/deployments.go`; called from Gateway, DataStorage, and Kubernaut Agent builders | `IT-TELEMETRY-TLS-001`, `003`, `004` |
| Static endpoint/material validation | `ValidateKubernaut` | `internal/resources/validation.go`, invoked by `phaseValidate` | `IT-TELEMETRY-TLS-002`, `003` |
| CA/client Secret content validation | telemetry Secret validator | `internal/controller/kubernaut_controller.go`, invoked from `phaseValidate` before deployment | `IT-TELEMETRY-TLS-002`, `005` |
| ConfigMap hash/rollout propagation | `deployConfigMaps` | `internal/controller/kubernaut_controller.go` and existing `ensureResource` flow | `IT-TELEMETRY-TLS-005` |
| Ambient trust and external Secret rotation | telemetry-material revision/watch path | upstream runtime qualification plus `SetupWithManager`, effective trust-source mapping, and Deployment template annotation | `IT-TELEMETRY-TLS-006`, `007`, `008` |

Checkpoint W is failed if any row lacks a production caller, if a new helper is
only tested directly, if one of the three components bypasses the shared
policy, or if a path is rendered without a matching mounted file.

## 8. Test pyramid and business matrix

### 8.1 Configuration matrix

| Case | Expected ConfigMap | Expected Deployment |
|---|---|---|
| Endpoint empty, `logSink=false` | No telemetry block | No telemetry mounts |
| Endpoint empty, `logSink=true` | Log sink only; no network TLS fields required | No telemetry mounts |
| Endpoint `stdout` | Local stdout/log sink; no network TLS requirement | No telemetry mounts |
| Network `host:port`, system CA | TLS block without `enabled` or `caFile` | No telemetry CA volume; existing inter-service trust remains usable and public system roots are preserved |
| Network `host:port`, file CA | TLS block with administrator path | No operator CA volume; path ownership documented |
| Network `host:port`, Secret CA | TLS block with deterministic mounted path | Read-only CA Secret volume/mount |
| Network `host:port`, mTLS | TLS block with cert/key paths | Read-only client Secret volume/mount |
| HTTP/HTTPS URI endpoint | No deployment | Validation failure naming the endpoint field |
| Incomplete/invalid Secret material | No deployment readiness | Validation failure/status; no plaintext fallback |

### 8.2 Business assertions and controls

These are repository evidence mappings, not a claim of formal FedRAMP or OWASP
certification. External platform, organizational, and independent assessment
evidence remains required.

| ID | Business assertion | Unit evidence | Integration/E2E evidence | FedRAMP/NIST | OWASP ASVS 5.0.0 |
|---|---|---|---|---|---|
| `BA-478-001` | A configured network exporter cannot select plaintext or a TLS-disable flag. | `UT-TELEMETRY-001/002` | `IT-TELEMETRY-TLS-001`, collector handshake lane | SC-8, SC-8(1), SC-13 | `v5.0.0-V12.2.1`, `v5.0.0-V12.3.1` |
| `BA-478-002` | Network export validates the collector certificate using system or explicitly selected private trust. | `UT-TELEMETRY-003/004` | `IT-TELEMETRY-TLS-001`, `E2E-TELEMETRY-TLS-001` | SC-8, SC-13 | `v5.0.0-V12.1.1`, `v5.0.0-V12.3.2` |
| `BA-478-003` | Secret-backed CA material is mounted read-only, is not adopted, and is never exposed in ConfigMaps/logs. | `UT-TELEMETRY-005/006` | `IT-TELEMETRY-TLS-001`, Secret ownership assertions | AC-6, IA-5, SC-12 | `v5.0.0-V12.1.3` |
| `BA-478-004` | Optional mTLS uses a complete, valid client certificate/key pair. | `UT-TELEMETRY-004/005` | `IT-TELEMETRY-TLS-002`, `E2E-TELEMETRY-MTLS-001` | IA-5, SC-8, SC-13 | `v5.0.0-V12.1.3`, `v5.0.0-V12.3.2` |
| `BA-478-005` | Missing, malformed, or contradictory trust/client material fails clearly before workload readiness. | `UT-TELEMETRY-002/005` | `IT-TELEMETRY-TLS-002` | CM-6, SI-10, SI-4 | `v5.0.0-V12.3.2` |
| `BA-478-006` | Disabled, log-sink-only, and stdout-only telemetry remain usable without network TLS dependencies. | `UT-TELEMETRY-001/007` | `IT-TELEMETRY-TLS-003` | SC-8 boundary/control correctness | N/A for local-only transport |
| `BA-478-007` | Gateway, DataStorage, and Kubernaut Agent receive identical TLS policy and generated-resource coverage. | `UT-TELEMETRY-006/007` | `IT-TELEMETRY-TLS-004`, Kind/E2E lane | SC-8, SC-12, SC-13 | `v5.0.0-V12.3.1` |
| `BA-478-008` | Every effective OTLP trust/client-material revision receives a controlled rollout, while invalid material preserves the last known-good Deployment and remains observable. | `UT-TELEMETRY-008` | `IT-TELEMETRY-TLS-007`, `008` | CM-6, SC-8, SC-12, SI-4 | `v5.0.0-V12.3.2` |

The implementation test plan should be recorded under `docs/tests/478/` (or
the repository’s then-current issue-test-plan convention) and should link each
business ID to its exact Ginkgo `It` block. The existing historical #421 audit
may be referenced for the gap, but must not be treated as a live completion
record.

## 9. Generated artifacts and documentation impact

After API approval and implementation:

1. Run `make manifests generate`.
2. Verify `api/v1alpha2/zz_generated.deepcopy.go` no longer copies `Enabled` and
   copies any new reference fields correctly.
3. Review the three telemetry schema occurrences in
   `config/crd/bases/kubernaut.ai_kubernauts.yaml` (Gateway, DataStorage, and
   Kubernaut Agent).
4. Regenerate/review:
   - `config/crd/bases/kubernaut.ai_kubernauts.yaml`;
   - `bundle/manifests/kubernaut.ai_kubernauts.yaml`;
   - `dist/install.yaml` after `make build-installer` or the repository’s
     equivalent generation target.
5. Update `config/samples/v1alpha2_kubernaut.yaml` with a commented, valid
   Secret-backed private-CA/mTLS example only if the sample convention accepts
   optional component examples; do not add a plaintext example.
6. Update operator documentation, at minimum:
    - `docs/security/credentials-and-tls.md` with OTLP Secret keys, ownership,
      rotation, system/private CA, mTLS rules, ambient trust behavior, rollout
      observability, and invalid-rotation recovery;
   - the relevant installation/configuration page with host:port/no-scheme
     examples and migration from `tls.enabled`;
   - an issue-specific control traceability/test-plan document.
7. Do not silently rewrite the historical `docs/tests/421` snapshot. If its
   owner wants the audit refreshed, do that as a separately identified update.

The expected generated diff is limited to the removed `enabled` schema,
approved Secret-reference schema/descriptions, generated deepcopy code, and
the corresponding bundle/install copies. Run:

```bash
make manifests generate
make build-installer
make bundle
git diff --exit-code -- config/ bundle/ dist/
```

The final command is a review gate, not a claim that the files are currently
unchanged after implementation.

## 10. Compatibility, migration, and ownership

### API and runtime compatibility

- This is a deliberate v1alpha2 security contract change. Existing
  `telemetry.tls.enabled: false` values must be removed; there is no supported
  false/disable value after the change.
- Existing network endpoints that relied on plaintext must be moved to a TLS
  collector. The operator must not silently convert a plaintext collector into
  a successful deployment.
- Existing `caFile` path values remain part of the upstream shape. Secret-backed
  private CA support adds a source that the operator can actually mount.
- Existing client path-only configurations must be reviewed. The recommended
  operator contract requires `tlsClientSecretRef` for generated mTLS material
  so a ConfigMap cannot point at an unmounted private key. Migration guidance
  must show the required Secret keys and paths.
- There is no v1alpha1 conversion update. The documented clean-break migration
  must export, transform, remove the obsolete field/add the new references,
  and recreate the v1alpha2 object.

### Secret ownership and rotation

- OTLP CA/client Secrets are administrator-owned BYO inputs.
- The operator validates but does not add owner references, mutate, rotate, or
  delete them.
- Kubernetes Secret volume refresh supplies changed bytes to the Pod, but the
  upstream OTLP tracer provider does not reread its CA/client material. The
  controller must watch referenced Secrets and effective ambient trust sources
  and roll out valid changes using non-sensitive version metadata; invalid
  changes must not deliberately replace the last known-good Deployment.
- A valid effective ambient-trust revision is a rollout input even when
  upstream shared HTTP clients can hot-reload that same trust. Hot reload is
  retained for non-OTLP clients but is not the OTLP freshness guarantee.
- Secret data must never be copied into a ConfigMap or status field.
- Administrator-owned file paths outside a referenced Secret remain a manual
  restart/rollout contract because the operator cannot observe or validate the
  external file.

## 11. Coordination with related issues

| Issue | Coordination requirement |
|---|---|
| `kubernaut#2384` | Source of truth for endpoint modes, removal of `Enabled`, TLS exporter behavior, and complete configuration matrix. Confirm the operator’s pinned upstream dependency/image accepts the rendered YAML. |
| Operator `#479` | OIDC issuer work is unrelated. Keep its API/config changes separate and avoid using its trust fields as an OTLP CA mechanism. |
| Operator `#489` | The operator-only Helm chart/package work may regenerate or redistribute CRD/install artifacts. Coordinate ownership of generated files and ensure chart examples do not reintroduce `telemetry.tls.enabled`. |
| Operator `#491` | Reuse the approved TLS source/ownership rules and do not create a second certificate lifecycle. External OTLP Secrets remain BYO, not cert-manager output. |
| Operator `#498` | Reuse manual/admin read-only Secret semantics, invalid-material/fail-closed evidence, and dedicated TLS E2E lanes where applicable. |

Before implementation, the selected API fields were checked against the
merged #499/domain-neutral trust type and the upstream #2384 runtime schema;
the narrow telemetry extension remains compatible with both.

## 12. Risks and mitigations

| Risk | Mitigation / gate |
|---|---|
| A removed field is still emitted by one of three CRD copies or a sample. | Search generated `config/`, `bundle/`, `dist/`, samples, and docs for `telemetry.tls.enabled`; generated-diff gate must be clean. |
| Log-sink-only mode regresses because the current resolver keys only on endpoint. | Add a dedicated matrix test and envtest case before changing the resolver. |
| External CA is confused with inter-service CA, or narrow `SSL_CERT_FILE` suppresses public roots. | Keep telemetry paths separate; qualify the ambient-CA strategy with the pinned runtime; preserve inter-service behavior through a merged bundle or equivalent injection path; assert public/private collector behavior. |
| A path is rendered but not mounted. | Shared path/material helper, exact ConfigMap-vs-Deployment assertions, and envtest resource inspection. |
| Invalid Secret material reaches a Pod and reports only a mount/startup event. | Validate Secret existence, keys, CA PEM, and client key pair in `phaseValidate`; stop before deployment and set an actionable condition. |
| Secret ownership or cleanup deletes administrator material. | No owner reference, no desired Secret object, read-only mount tests, and cleanup regression test. |
| API field shape duplicates #499 or creates a domain-leaking Fleet dependency. | #499 reassessment found no domain-neutral type; reject direct `FleetTrustSpec` reuse and approve the narrow telemetry extension. |
| External Secret rotation leaves a running tracer provider on stale CA/client material. | Watch referenced Secrets, validate before rollout, use non-sensitive resource-version metadata, preserve the existing Deployment on invalid rotations, and document manual handling for external file paths. |
| Ambient trust rotation leaves default-trust OTLP exporters on stale roots despite shared-client hot reload. | Include the effective ambient trust revision in the shared telemetry-material rollout contract; validate before updating the Pod template and document the expected rolling update. |
| Public OTLP export fails because `SSL_CERT_FILE` replaces system roots. | Render upstream ambient trust source fields, provide writable `/tmp`, remove narrow static `SSL_CERT_FILE` values, and test the real operand bootstrap before deployment. |
| Operators mistake a Secret volume refresh for a completed telemetry rotation. | Document `oc/kubectl rollout status`, CR conditions, Deployment events, and the manual restart path for unwatchable file-based material. |
| Existing consumers depend on plaintext or `https://` endpoint strings. | Explicit migration documentation, field-qualified rejection, and no silent downgrade or URL rewriting. |
| Core runtime and operator YAML drift. | Pin/qualify the upstream release, run upstream-compatible YAML tests, and include a real collector handshake lane. |
| Controls are overstated. | Label repository tests as evidence only; retain external platform, PKI, and formal assessment caveats. |

## 13. Approval gates and completion criteria

Before RED tests or production type changes, the issue owner must approve:

1. Removal of `TelemetryTLSConfig.Enabled` from the v1alpha2 API and all
   generated/rendered artifacts.
2. The host:port/no-scheme endpoint contract, including `stdout` as the only
   local endpoint exception.
3. The recommended `caCertSecretRef` and `tlsClientSecretRef` additions,
   their fixed Secret-key contracts, path rules, and administrator-owned
   read-only semantics.
4. Whether #499 provides a domain-neutral trust type that #478 must reuse,
   rather than adding a new equivalent type. The delegated spike currently
   answers no: retain the narrow telemetry extension.
5. The controller status behavior for missing/invalid telemetry material and
   the wiring/test IDs in section 7.
6. The clean-break migration policy and generated artifact list.
7. The upstream-style ambient system-CA strategy: config-derived combined
   system-plus-inter-service trust, writable `/tmp`, and removal of narrow
   static `SSL_CERT_FILE` values.
8. **Approved in this session:** a shared telemetry-material revision and
   controlled rollout for valid OTLP CA/client Secret updates **and effective
   ambient trust updates**, with invalid-rotation preservation of the current
   Deployment, affected-producer targeting, and a manual contract for external
   file paths.
9. The FedRAMP/OWASP traceability wording, with no claim of formal
   authorization or certification.

Implementation completion requires all of the following:

- `go build ./...` succeeds;
- `golangci-lint run` has no new findings;
- `make test-unit`, `make test-integration`, and `make test` pass;
- no business test uses standard `testing.T` instead of Ginkgo/Gomega;
- no pending/skipped tests or `time.Sleep` are introduced;
- Checkpoint W passes for all rows;
- `make manifests generate`, bundle generation, and installer generation are
  reviewed with no unrelated diff;
- the generated CRD and docs contain no telemetry TLS-disable switch;
- the selected ambient-CA behavior is proven for public collectors and does not
  regress inter-service, LLM, monitoring, or PostgreSQL trust;
- the selected Secret-rotation behavior is proven and documented;
- the operational runtime documentation explains valid/invalid rotation,
  rollout verification, ambient trust refresh, and manual file-path handling;
- all acceptance cases in section 8 have unit and integration evidence, with a
  real private-CA/mTLS handshake lane where the test environment supports it;
- security documentation maps SC-8, SC-8(1), SC-13, and the versioned ASVS
  communications requirements without overstating assurance.

## 14. Confidence assessment

**Confidence: 95%.**

The impact map and implementation direction are directly evidenced by the
current API, shared ConfigMap renderer, three Deployment builders, controller
validation/reconciliation path, generated CRD copies, upstream #2384 contract,
the upstream #2276 ambient-CA implementation, the upstream #756/#1505 CA
watcher path, and the #491/#498 trust ownership foundations. The read-only
spikes resolved the #499 API boundary, qualified the OCP versus Helm trust
behavior, and established that OTLP client CA/certificate material is static
after tracer-provider construction. The implementation and generated-artifact
verification are recorded below; a live collector handshake remains an
environment-dependent qualification step.

## 15. Implementation record

The approved contract is implemented in the shared telemetry renderer and
Secret-mount helper, with Gateway, DataStorage, and Kubernaut Agent wiring.
Controller validation rejects invalid endpoint or Secret material before
deployment, watches referenced and effective ambient trust sources, and stamps
only non-sensitive resource-version-derived rollout revisions. The CRD,
bundle, and installer artifacts were regenerated; no telemetry TLS disable
field is emitted.

Verification completed with `make test` (unit coverage 86.4%, controller
coverage 78.8%), `make lint`, `go build ./...`, `make test-pyramid`, and
regenerated manifests, bundle, and installer artifacts. The live private-CA
collector handshake remains an environment-dependent qualification step and
was not run in this repository-only validation.

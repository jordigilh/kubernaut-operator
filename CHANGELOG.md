# Changelog

All notable changes to the Kubernaut Operator will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- **NetworkPolicy**: fleet-aware components (FleetMetadataCache,
  SignalProcessing, RemediationOrchestrator, EffectivenessMonitor,
  KubernautAgent, APIFrontend, Gateway) could not reach a Route-fronted MCP
  Gateway -- `fleetDestinationsEgressRule`'s `namespaceSelector: {}` peer
  only matches destinations backed by a namespaced Pod IP, but an
  OpenShift Route resolves to the Ingress Router's hostNetwork VIP, which
  OVN-Kubernetes silently drops regardless of that peer. The rule now also
  resolves `spec.fleet.mcpGatewayEndpoint`/`oauth2.tokenURL`/`endpoint`'s
  hostnames via DNS and adds their real IPs as `ipBlock` peers, mirroring
  the same live-resolve/cache/fail-closed pattern already used for the
  Kubernetes API server egress fix. Confirmed via live on-cluster curl
  reproduction (#392)
- **NetworkPolicy**: `workflowExecutionNetworkPolicy` never granted any
  fleet egress at all, a gap missed by the original #224/#204 retrofit,
  despite WorkflowExecution being fully fleet-config-aware since #390 (#392)

## [1.6.0-rc4] - 2026-08-23

### Added
- **CR**: `spec.fleet.resilience` tunes the MCP client's startup backoff
  and per-operation timeouts (`initialInterval`, `maxInterval`,
  `maxElapsedTime`, `tokenRefreshTimeout`, `connectTimeout`,
  `discoverProbeTimeout`) shared by every fleet-aware component (Gateway,
  RemediationOrchestrator, APIFrontend, EffectivenessMonitor,
  SignalProcessing, WorkflowExecution, KubernautAgent,
  FleetMetadataCache). Mirrors upstream `pkg/fleet.FleetResilienceConfig`
  (kubernaut#2262 Phase 2, kubernaut#2268). Every field is optional and
  zero-value-safe -- omitting `spec.fleet.resilience` entirely preserves
  each service's existing hardcoded defaults unchanged (#390)

### Fixed
- **Resolves the rc3 Known Issue**: bumped the `RELATED_IMAGE_*` digests for
  all 13 operand images (Gateway, DataStorage, AIAnalysis,
  SignalProcessing, RemediationOrchestrator, WorkflowExecution,
  EffectivenessMonitor, Notification, KubernautAgent, AuthWebhook,
  APIFrontend, FleetMetadataCache, DB-Migrate) and the
  `github.com/jordigilh/kubernaut` Go module dependency to upstream
  [v1.6.0-rc6](https://github.com/jordigilh/kubernaut/releases/tag/v1.6.0-rc6).
  This pulls in the upstream fix for
  [kubernaut#2262](https://github.com/jordigilh/kubernaut/issues/2262) --
  FleetMetadataCache's `mcpclient.NewResilient()` no longer hangs
  establishing an MCP session against a Kuadrant-fronted MCP Gateway; the
  SEP-2575 `server/discover` probe is now bounded by its own sub-timeout
  independent of the connect timeout. Fleet-mode deployments should now
  reach `Ready` without the workaround noted in rc3.
- No changes to `kubernaut-console`; `RELATED_IMAGE_CONSOLE` is unchanged
  from rc3.

## [1.6.0-rc3] - 2026-08-23

### Known Issues
- **Fleet mode: FleetMetadataCache may not reach Ready.** On clusters where
  `fleetmetadatacache` (FMC) connects to a Kuadrant-fronted MCP Gateway,
  `mcpclient.NewResilient()` (upstream `kubernaut` core) can time out
  establishing an MCP session (`context deadline exceeded`) even though
  DNS, TCP, TLS, OAuth2, and the raw MCP protocol handshake all
  independently succeed against the same live endpoint. This is an
  upstream `kubernaut` core issue, not an operator defect -- tracked at
  [kubernaut#2262](https://github.com/jordigilh/kubernaut/issues/2262).
  Non-fleet (single-cluster) deployments are unaffected.

### Fixed
- **NetworkPolicy**: Egress rules to the Kubernetes API server used the
  `KUBERNETES_SERVICE_HOST` ClusterIP (e.g. `172.30.0.1/32`) as an `ipBlock`
  CIDR. On OVN-Kubernetes, NetworkPolicy egress ACLs are evaluated against
  the **post-DNAT** destination, i.e. the real API server endpoint IP(s),
  not the ClusterIP the pod dialed -- so this rule never actually matched
  traffic, and every operand pod (`apifrontend`, `authwebhook`,
  `notification`, `remediationorchestrator`, `effectivenessmonitor`,
  `workflowexecution`, `fleetmetadatacache`, `aianalysis`,
  `signalprocessing`, ...) got `dial tcp <ClusterIP>:443: i/o timeout`
  reaching the API server whenever NetworkPolicies were enabled.
  `resolveAPIServerIPs()`/`apiServerEgressRule()` already existed in
  `internal/resources/networkpolicies.go` to resolve the real endpoint
  IPs from the `default/kubernetes` Endpoints object and use those in the
  `ipBlock` instead, but the operator's ClusterRole never granted `get` on
  `endpoints`, so every lookup returned `403 Forbidden` and silently fell
  back to the broken `KUBERNETES_SERVICE_HOST` CIDR. Added a narrowly
  scoped RBAC rule (`resources=endpoints,verbs=get,resourceNames=kubernetes`)
  and switched `resolveAPIServerIPs()` to fail closed: on a failed live
  lookup it now reuses the last successfully-resolved IPs (cached
  in-memory) rather than falling back to the ClusterIP, and only uses
  `KUBERNETES_SERVICE_HOST` -- uncached, so every later call keeps
  retrying live -- when no successful resolution has occurred yet (e.g.
  the very first reconcile). This keeps egress correct across API server
  IP changes (e.g. HA failover) without ever regressing to the
  known-ineffective ClusterIP rule once a real IP has been resolved once
- **RBAC generation**: `config/rbac/role.yaml` has not actually been
  regenerated by `make manifests` for an unknown period of time, for two
  independent, compounding reasons:
  1. The `+kubebuilder:rbac` marker block in
     `internal/controller/kubernaut_controller.go` was written as an
     unbroken comment directly above `func (*KubernautReconciler) Reconcile`
     with no blank line separating it from the function. Go's parser
     attaches such a block as that function's GoDoc, and
     `controller-tools`' marker-association logic only treats markers as
     package-level (required for `+kubebuilder:rbac`) for specific AST
     node kinds (`GenDecl`/`TypeSpec`/`Field`/`File`) or free-floating
     comments -- a `FuncDecl`'s GoDoc isn't one of them, so the entire
     block was silently discarded on every generation (verified: 0 RBAC
     rules emitted without a separating blank line + doc comment, 231
     with one added)
  2. Independently, the `manifests` Makefile target never passed
     `output:rbac:artifacts:config=...` to `controller-gen` -- only
     `output:crd:artifacts:config=...` was set. `controller-gen`'s
     default output rule for any generator without an explicit
     `output:<generator>:...` flag is `OutputToNothing`, a silent no-op,
     so RBAC output had nowhere to go regardless of (1)
  Fixed both: added the blank line + doc comment, and added
  `output:rbac:artifacts:config=config/rbac` to the `manifests` target.
  Regenerating surfaced one real pre-existing gap beyond the `endpoints`
  rule above:   `spire.spiffe.io/clusterspiffeids` permissions declared via
  marker were never in the applied ClusterRole either. Removed
  `config/rbac/spire_role.yaml` and `spire_role_binding.yaml`, a
  hand-maintained standalone `ClusterRole`/`ClusterRoleBinding` pair that
  had been added as a workaround for this same missing permission (its
  own comment misattributed the cause to "API groups not in the Go
  module's dependency graph" -- not the actual root cause above), now
  redundant and removed to avoid duplicate rules in the generated OLM
  bundle CSV
- **TLS trust (ingress/router CA)**: `fleetmetadatacache` and
  `kubernaut-console` failed TLS verification
  (`x509: certificate signed by unknown authority`) against OpenShift
  Routes (MCP Gateway, Keycloak OIDC), which are signed by the
  cluster's default IngressController CA -- a separate chain from the
  service-ca-operator's inter-service CA that the operator's existing
  `inter-service-ca` ConfigMap already covered. Added a new
  operator-computed `inter-service-trust-bundle` ConfigMap that merges
  `inter-service-ca` with `openshift-config-managed/default-ingress-cert`
  (both reads fail open, live-read pattern mirroring
  `resolveAPIServerIPs`), granted a narrowly scoped RBAC rule
  (`get`-only, `resourceNames=default-ingress-cert`) to read it, and
  repointed every component's `tls-ca` mount (console, Gateway,
  DataStorage, APIFrontend, RemediationOrchestrator,
  WorkflowExecution/AAP, EffectivenessMonitor, FleetMetadataCache, the
  migration Job, and the Gateway `AlertmanagerConfig`'s
  `ServiceMonitor`) at this new bundle instead of `inter-service-ca`
  directly, at the same mount path -- no client-side code changes
  required
- **TLS trust (MCP Gateway session, operator-only workaround)**:
  `fleetmetadatacache`'s actual MCP Gateway session transport (upstream
  `pkg/fleet/mcpclient.WithReloadableOAuth2Transport`) builds its HTTP
  transport from an unmodified `http.DefaultTransport` -- none of its
  three call sites (`cmd/gateway`, `cmd/remediationorchestrator`,
  `cmd/fleetmetadatacache`) ever call `mcpclient.WithHTTPClient` to
  inject a custom CA, so `config.yaml`'s `fleet.oauth2.tlsCAFile` only
  ever covered the separate OAuth2 token fetch, never the actual MCP
  protocol traffic itself. Set `SSL_CERT_FILE` alongside `TLS_CA_FILE`
  on every component carrying the inter-service CA env var (plus a new
  dedicated env var on FleetMetadataCache, which previously had none)
  as an operator-only workaround, since Go's `crypto/x509` additively
  extends -- rather than replaces -- the system cert pool with the file
  it names. A proper fix requires an upstream `kubernaut` core change;
  tracked separately
- **Fleet backend TLS default**: `resolveFleetConfig`'s top-level
  `tlsCAFile` (verifies `fleet.Endpoint`'s TLS, e.g.
  FleetMetadataCache's in-cluster Service, via upstream's
  Backend/Endpoint scope-check adapter) was only ever set when the
  admin configured an explicit `fleet.CASecretName` -- the common case
  of relying on the operator's own service-ca-signed endpoint got no CA
  at all. Now defaults to the trust-bundle path, matching the adjacent
  `fleet.oauth2.tlsCAFile` default
- **FleetMetadataCache readiness**: `startupProbe`/`readinessProbe`/
  `livenessProbe` all targeted the `api` port (8080), but FMC's own
  startup log (`"healthAddr":":8081"`) shows `/healthz` and `/readyz`
  are actually served on the `metrics` port (8081) -- confirmed via
  on-cluster validation of the TLS trust-bundle fix above, which left
  FMC stuck at `0/1 Ready` (`startup probe: connection refused`) even
  once otherwise healthy. Probes now target the metrics port

## [1.6.0-rc2] - 2026-08-23

### Fixed
- **Webhook**: `SingletonValidator` (the `validate-kubernaut-singleton`
  admission webhook enforcing the one-CR-per-cluster constraint) panicked
  with a nil pointer dereference on every `Kubernaut` CR `CREATE`, denying
  all installs. `cmd/main.go` constructed it as a bare struct literal and
  relied on the legacy `InjectDecoder`/`DecoderInjector` mechanism to supply
  a decoder; that auto-injection path was removed from `controller-runtime`
  several versions ago (this repo is on v0.24.1), so `decoder` stayed `nil`
  and `Handle` crashed on the first `v.decoder.Decode(...)` call. Added
  `webhook.NewSingletonValidator(client, scheme)`, the only supported
  constructor, which builds a working `admission.NewDecoder(scheme)`;
  `cmd/main.go` now uses it instead of the literal. This had zero test
  coverage at any tier prior to this fix

## [1.6.0-rc1] - 2026-08-22

### Added
- **CR**: `spec.apiFrontend.rbac.consoleAccessGroups` for the coarse-grained
  `kubernaut.ai/console` `use` RBAC gate (kubernaut#1919/#1940). When unset,
  defaults to the deduplicated union of groups already present in
  `roleBindings`, so upgrading to an AF version enforcing this gate does not
  silently deny existing deployments' console access. Set to an explicit
  empty list to opt out, or a non-empty list for independent control (#290)
- **Webhook**: AuthWebhook's `ValidatingWebhookConfiguration` now includes
  an `agentsession.validate.kubernaut.ai` entry mirroring upstream
  kubernaut's AgentSession CREATE existence gate (kubernaut#2244,
  BR-AA-KA-065.13) -- denies creation when
  `Spec.RemediationRequestRef` does not resolve to a real
  `RemediationRequest` in the same namespace

### Changed
- **BREAKING**: `spec.monitoring` (and its `enabled` field) is removed from
  the CRD entirely. OCP monitoring integration (Prometheus/AlertManager
  auto-discovery, its RBAC, and its NetworkPolicy egress) is now
  provisioned unconditionally on every reconcile; there is no spec field
  left that can disable it. This operator has no non-OCP monitoring
  backend to fall back to, and disabling monitoring used to cause
  severity-gated remediation request creation to fail closed once upstream
  kubernaut#1839 removed AF's ungrounded LLM severity fallback. See
  `docs/upgrade-1.5-to-1.6.md` and
  `docs/design/DD-273-deprecate-monitoring-disabled.md` (#273)
- **RBAC**: SignalProcessing's `ClusterRole` no longer grants any verbs on
  `remediationrequests`, matching upstream's least-privilege fix
  (kubernaut#2243, FedRAMP AC-6). SP never performs CRUD against the
  RemediationRequest CRD -- it only reads `Spec.RemediationRequestRef.Name`
  (a plain string field on its own CRD) for audit correlation
- **Images**: Bumped the 13 kubernaut-service `RELATED_IMAGE_*` env vars,
  the bundle CSV, the airgap imageset, and `dist/install.yaml` to reference
  the freshly-built upstream kubernaut `v1.6.0-rc5` images. Bumped the
  `kubernaut` Go module dependency to `v1.6.0-rc5`. Bumped `VERSION` to
  `1.6.0-rc1`. The operator's own image and bundle references remain
  tag-based (`1.6.0-rc1`) pending the tag build; a follow-up PR will pin
  them to digests once published, matching the pattern used for
  v1.5.6/v1.5.7/v1.5.8

### Fixed
- `ConsoleRoute()` now sets the `haproxy.router.openshift.io/timeout: 3600s`
  annotation, mirroring `GatewayRoute`/`APIFrontendRoute`. Without it, OCP
  HAProxy's default route timeout silently killed long-running A2A
  investigation streams mid-stream ("context canceled"), dropping the
  console's train-of-thought and hanging tool calls (#268)

### Removed
- **BREAKING**: `spec.kubernautAgent.alignmentCheck.llm.apiKey` is removed
  from the CRD. This field never had any effect -- KA resolves LLM API keys
  purely via provider-named env vars (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
  etc.), never from a plaintext CR field, and has no config field to bind
  a rendered `apiKey` YAML key to. Setting this field on an existing CR is
  a silent no-op both before and after this change (#237)

## [1.5.0] - 2026-06-01

### Added
- **CR**: `spec.kubernautAgent.llm.tlsCertFile`, `tlsKeyFile`, and
  `tlsClientSecretRef` for mTLS client certificate authentication to LLM
  endpoints (#154)
- **CR**: `spec.apiFrontend.metricsPort` and `healthPort` for configurable
  AF metrics and health probe ports (#153)
- **CR**: `spec.apiFrontend.auth.tokenReviewAudience` for Kubernetes
  TokenReview audience validation (FedRAMP IA-5) (#139)
- **SPIRE**: Operator creates `ClusterSPIFFEID` for API Frontend when
  `spec.apiFrontend.spire.enabled=true` and the SPIRE CRD is present (#136)
- **Webhook**: Singleton `ValidatingWebhookConfiguration` enforces the
  one-Kubernaut-CR-per-cluster constraint at admission time
- **Monitoring**: ServiceMonitors for all 11 components (Gateway,
  AIAnalysis, SignalProcessing, RemediationOrchestrator, WorkflowExecution,
  EffectivenessMonitor, Notification, AuthWebhook, plus existing AF, DS, KA)
- **NetworkPolicy**: Kubernaut Agent now has port-443 egress for external
  LLM API calls when an LLM provider is configured

### Fixed
- **MCP**: Corrected MCPServerRegistration endpoint DNS from
  `apifrontend-service` to `apifrontend` (matching the actual Service name)
- **Init containers**: Aligned fallback image digests in `common.go` with
  the digests in `manager.yaml` for PostgreSQL and UBI-minimal init
  containers
- **Error handling**: Fixed silent error swallowing in RBAC status patching
  (`kubernaut_controller.go`) and MCP resource construction
  (`mcpgateway.go`) — errors are now returned/logged instead of discarded

### Security
- **TLS**: Replaced `InsecureSkipVerify: true` on the AlertManager →
  Gateway webhook with proper CA verification via the OCP service-CA
  trust bundle
- **Admission**: Singleton webhook prevents accidental creation of
  duplicate Kubernaut CRs

### Changed
- **OLM**: Bundle regenerated for v1.5.0 with updated CRD schema
- `MCPGatewayHTTPRoute` and `MCPServerRegistration` now return errors
  instead of silently ignoring `unstructured.SetNested*` failures

## [1.4.0] - 2026-05-12

### Added
- **RBAC**: Expanded `kubernaut-agent-investigator` ClusterRole with read-only
  access to OCP and core K8s resources for incident investigation:
  - OLM: CSVs, Subscriptions, InstallPlans, OperatorGroups, CatalogSources
  - OCP platform: Routes, DeploymentConfigs, SCCs, ImageStreams, Builds
  - OCP machine management: Machines, MachineSets, MachineConfigs, MCPs
  - Core K8s: RBAC objects, admission webhooks, CRDs, PriorityClasses
- **RBAC**: Added `persistentvolumeclaims` and `horizontalpodautoscalers`
  read access to the Gateway ClusterRole for owner-chain resolution during
  signal fingerprinting (#87)
- **RBAC**: Added egress rules to `kubernaut-agent` NetworkPolicy for
  API server, data-storage, and monitoring stack (Thanos Querier TCP 9091,
  AlertManager TCP 9094) access when NetworkPolicies are enabled
- **CR**: New `spec.kubernautAgent.alignmentCheck` configuration for shadow
  agent alignment verification (enabled, timeout, maxStepTokens, LLM config)
- **CR**: New `spec.remediationOrchestrator.dryRun` and `dryRunHoldPeriod`
  for remediation dry-run mode
- **CR**: New `spec.kubernautAgent.llm.tlsCaFile` for custom CA certificates
  on LLM endpoints
- **CR**: New `spec.kubernautAgent.llm.oauth2` configuration for OAuth2-based
  LLM authentication (tokenURL, scopes, credentialsSecretRef)
- **CR**: New `spec.kubernautAgent.safety` configuration for anomaly detection
  and input sanitization (maxToolCallsPerTool, maxTotalToolCalls,
  maxRepeatedFailures, injection pattern detection, credential scrubbing)
- **CR**: New `spec.kubernautAgent.summarizer` configuration for tool output
  summarization (threshold, maxToolOutputSize)
- **Security**: Agent deployment now uses projected service account tokens
  with 1-hour TTL and audience binding instead of long-lived automounted tokens
- **UX**: `RBACProvisioned` condition now set to `False` with descriptive
  message when RBAC provisioning fails, plus Warning Event emitted
- **UX**: `AdditionalRBACBound` condition now uses `Status=False` when
  referenced ClusterRoles do not exist (was incorrectly `True`)
- **CI**: Coverage threshold enforcement (80% minimum) in test workflow
- **Docs**: RBAC troubleshooting section in deployment guide
- **Docs**: Agent cluster access summary in OLM CSV description
- **Docs**: `SECURITY.md` investigator risk assessment section
- **Docs**: `CHANGELOG.md` established for tracking security-relevant changes
- **Docs**: Installation guide updated with new CR spec fields

### Fixed
- **CRDs**: Operator now embeds CRDs from kubernaut v1.4.0 (was v1.3.1),
  ensuring new enum values like `alignment_check_failed` are applied to
  the cluster on reconciliation (#88)
- **RBAC**: Gateway SA was missing read access for `PersistentVolumeClaim`
  and `HorizontalPodAutoscaler`, causing signals to be silently dropped
  during owner-chain resolution (#87)
- **Docs**: Corrected AWX ClusterRole name in `SECURITY.md`
  (`<ns>-awx-integration` → `<ns>-workflowexecution-awx`)
- **Docs**: Updated stale test plan entries (test counts, verb descriptions,
  CI coverage claims)

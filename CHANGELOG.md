# Changelog

All notable changes to the Kubernaut Operator will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

# Changelog

All notable changes to the Kubernaut Operator will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.5.8-rc2] - 2026-08-10

### Fixed
- **Console**: Disabled the raw-thinking panel unconditionally for
  `release/v1.5` by serving a fixed `enableRawThinking: false` from
  `ConsoleNginxConfigMap`'s `/runtime-config.js` — the v1.5 AF backend never
  emits `reasoning_content` events, so the panel had nothing to render on
  any v1.5 deployment. A real per-deployment CRD toggle is deferred to v1.6,
  where AF does emit these events (#313, #316)

### Changed
- **Images**: Bumped all 13 `RELATED_IMAGE_*` env vars (12 kubernaut
  services + console), the bundle CSV, the airgap imageset, and
  `dist/install.yaml` to reference the freshly-built upstream kubernaut
  `v1.5.6-rc3` images. Bumped `VERSION` to `1.5.8-rc2`. The operator's own
  image and bundle references remain tag-based (`1.5.8-rc2`) pending the tag
  build; a follow-up PR will pin them to digests once published, matching
  the pattern used for v1.5.6/v1.5.7/v1.5.8-rc1.

## [1.5.8-rc1] - 2026-08-06

### Added
- **RBAC**: Operator now provisions the `kubernaut.ai/console` "use" SubjectAccessReview
  RBAC support needed by kubernaut's new coarse-grained console-access
  authorization gate (kubernaut #1919/#1941), so clusters with a custom
  persona-group mapping no longer 403 every console session after
  upgrading (#289, #291)
- **RBAC**: `sre` persona now includes `kubernaut_get_approval_request` so
  the approval gate is usable end-to-end instead of silently failing for
  that persona (#278, #281)
- **NetworkPolicy**: API Frontend now has egress to the Thanos Querier
  (port 9091) for `severityTriage` Prometheus calls (#271, #275)

### Fixed
- **CR**: Removed the dead `spec.kubernautAgent.alignmentCheck.llm.apiKey`
  field — the Kubernaut Agent binary only ever read `apiKeyFile`, so the
  field had no effect and was misleading (#237, #295)
- **Route**: Added the missing HAProxy SSE timeout annotation on
  `ConsoleRoute`, which was dropping long investigations mid-stream with a
  "context canceled" error (#268, #293)
- **Docs**: Corrected a misleading `gemini-2.5-pro` example in
  `spec.kubernautAgent.llm.model`'s documentation (backport, #270)
- **Docs**: Fixed the PostgreSQL TLS certificate path for the `rhel10`
  init-container image in the install docs (#266)

### Security
- **Go toolchain**: Pinned the `go` directive to `1.26.5` on
  `release/v1.5` to close 23 stdlib CVEs (#264)
- **Dependencies**: Bumped `ubi10/go-toolset` and other `all-docker`-group
  base images (#284, #250), and the `all-go-deps` dependency group across
  two Dependabot batches, 9 updates then 4 (#255, #286)

### Changed
- **Images**: Bumped all 13 `RELATED_IMAGE_*` env vars (12 kubernaut
  services + console), the bundle CSV, the airgap imageset, and
  `dist/install.yaml` to reference the freshly-built upstream kubernaut
  `v1.5.6-rc2` images. Bumped `VERSION` to `1.5.8-rc1`. The operator's own
  image and bundle references remain tag-based (`1.5.8-rc1`) pending the
  tag build; a follow-up PR will pin them to digests once published,
  matching the pattern used for v1.5.6/v1.5.7.

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

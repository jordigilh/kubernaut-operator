# Changelog

All notable changes to the Kubernaut Operator will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.5.10] - 2026-08-12

### Changed (follow-up)
- Pinned the operator's own image and bundle references in `config/manager/kustomization.yaml`, `bundle/manifests/`, `dist/install.yaml`, and the airgap imageset to their published `v1.5.10` digests, matching the pattern used for every prior release. Also fixed the `containerImage` annotation in `bundle/manifests/kubernaut-operator.clusterserviceversion.yaml`, which had been left at `:1.5.9` because the base CSV template edit was made after the initial `make bundle` regeneration in the prior commit.

### Fixed
- **OLM bundle (`bundle/manifests/`) and `dist/install.yaml` were shipping a stale `Kubernaut` CRD schema** — missing the `apiFrontend.rbac.toolPersonaConfig.consoleAccessGroups` field (added by #289/#291) and still carrying the dead `kubernautAgent.alignmentCheck.llm.apiKey` field (removed by #296). Both `bundle/` and `dist/install.yaml` embed their own copy of the CRD rendered at release-prep time; the several digest-only patch releases since `rc4` (`rc4`–`rc6`, `v1.5.8`, `v1.5.9`) hand-edited image references via `sed` instead of running the full `make manifests generate bundle` / `make build-installer` regeneration, so `config/crd/bases/` (the source of truth, kept in sync by #289/#291's and #296's own PRs) never got re-propagated into the bundle/installer copies. `config/crd/bases/` itself was never wrong — this only affected the OLM/`dist/install.yaml` install paths, not `make install`. Regenerated both from source; no manual edits.
- **`config/manager/kustomization.yaml` was never updated to pin the operator's own image digest for `v1.5.9`** — the `v1.5.9` pin-digest follow-up (#330) patched `dist/install.yaml` and the airgap imageset directly but missed the kustomize source, so `make bundle`/`make build-installer` would have kept regenerating a tag reference (`:1.5.9`) for the operator's own image, silently reverting #330's fix on the next regeneration. Fixed at the source for `v1.5.10`.

### Changed
- Bumped `VERSION` to `1.5.10`. No operand (`kubernaut`/`kubernaut-console`) digest changes — this is a bundle/manifest-hygiene-only patch. A follow-up PR will pin the operator's own image and bundle to their published `v1.5.10` digests once the tag image is built, per the usual pattern.

## [1.5.9] - 2026-08-12

### Changed (follow-up)
- Pinned the operator's own image and bundle references in `dist/install.yaml` and the airgap imageset to their published `v1.5.9` digests, matching the pattern used for every prior release.

### Fixed
- **`dist/install.yaml` and the airgap imageset were pinning the operator's own image and bundle to their `v1.5.8-rc6` digests instead of `v1.5.8` GA's** — the `v1.5.8` GA version bump only rewrote tag-style references (`kubernaut-operator:1.5.8-rc6` → `:1.5.8`) and missed the two files where the prior `rc6`-pin-digest follow-up had already replaced the tag with a hardcoded digest, so those two files silently kept shipping the `rc6` build under the `v1.5.8` tag. Reset both back to tag references (`:1.5.9`) here; a follow-up PR will pin them to the real `v1.5.9` digest once the tag image is built, per the usual pattern.

### Changed
- **Images**: Re-points `RELATED_IMAGE_CONSOLE` to `kubernaut-console v1.5.6` GA (was pinned to a stale, no-longer-tagged digest left over from the `rc4` validation cycle — see below). This closes the GA-to-GA provenance gap where the fully-GA `v1.5.8` operator bundle attested to a console artifact whose own SBOM/build-provenance said "prerelease". Bumped `VERSION` to `1.5.9`. No operator code or CRD changes.

### Context
- `kubernaut-console`'s `release/v1.5` branch ref had regressed to a stale, divergent commit (missing 29 commits of validated `v1.5.6` work — the real `CHANGELOG.md` stamp, the approval-card fix, the `a2a`-disconnect fix, CI credential fixes, etc.) sometime after `v1.5.6-rc4` was tagged. The branch was reset back to the `rc4` commit and a proper `v1.5.6` GA tag was cut from it before this release; see `kubernaut-console`'s own history for detail. The two commits dropped from the branch tip (a session-fix backport and a test-coverage backport) are both redundant with fixes already present in the real `v1.5.6` line.

## [1.5.8] - 2026-08-12

### Changed
- **GA promotion**: Promotes `v1.5.8-rc6` to GA, now that upstream `kubernaut v1.5.6` GA is available. QE approved `v1.5.8-rc6` (validated against `kubernaut v1.5.6-rc6` + `kubernaut-console v1.5.6-rc4`) for release. The 12 kubernaut-service `RELATED_IMAGE_*` env vars, the bundle CSV, the airgap imageset, and `dist/install.yaml` now reference the final `kubernaut v1.5.6` GA images (content-identical to the validated `rc6` images — same commit, freshly tagged). `RELATED_IMAGE_CONSOLE` remains on `kubernaut-console v1.5.6-rc4` (console GA is tracked independently). Bumped `VERSION` to `1.5.8`.
- Summarizes the `v1.5.8-rc1` → `rc6` journey: RBAC visibility fix for tool-sre approval tools (#278), and successive upstream digest bumps tracking `kubernaut v1.5.6-rc2` → `rc6` (workflow-discovery/interactive-session hardening, gob-safety boundary hardening, RCA confidence and gate-retry fixes — see `kubernaut`'s own `CHANGELOG.md` for the full fix list). No operator code or CRD changes across the entire `v1.5.8` cycle.

## [1.5.8-rc6] - 2026-08-12

### Changed
- **Images**: Bumped the 12 kubernaut-service `RELATED_IMAGE_*` env vars,
  the bundle CSV, the airgap imageset, and `dist/install.yaml` to
  reference the freshly-built upstream kubernaut `v1.5.6-rc6` image,
  which bundles three additional fixes: kubernaut #2111 (gob-safety
  boundary hardening at `EventBridge.EmitArtifact`, closing the class of
  bug behind #2110 rather than just its one call site), #2118 (same-kind
  gate retry could silently zero a previously-validated RCA confidence),
  and #2120 (same-kind/API-version gate retry now recovers from an
  undeclared LLM tool call, and fixes dead `retry_outcome` audit
  persistence). `RELATED_IMAGE_CONSOLE` is unchanged — console stays on
  `v1.5.6-rc4`. Bumped `VERSION` to `1.5.8-rc6`. The operator's own image
  and bundle references were pinned to their published digests in
  `dist/install.yaml` and the airgap imageset once the `v1.5.8-rc6` tag
  build completed, matching the pattern used for
  v1.5.6/v1.5.7/v1.5.8-rc1/v1.5.8-rc2/v1.5.8-rc4/v1.5.8-rc5.

## [1.5.8-rc5] - 2026-08-11

### Changed
- **Images**: Bumped the 12 kubernaut-service `RELATED_IMAGE_*` env vars,
  the bundle CSV, the airgap imageset, and `dist/install.yaml` to
  reference the freshly-built upstream kubernaut `v1.5.6-rc5` image —
  QE found that `v1.5.6-rc4` crashed every `kubernaut_present_decision`
  call with grounded RCA content (kubernaut #2110: a2a-go's task-manager
  deep-copy fan-out couldn't gob-encode the `*tools.RCAData` pointer
  smuggled into `args["rca"]`), breaking the entire interactive
  approve/decline/dismiss flow. `RELATED_IMAGE_CONSOLE` is unchanged —
  console stays on `v1.5.6-rc4`. Bumped `VERSION` to `1.5.8-rc5`. The
  operator's own image and bundle references were pinned to their
  published digests in `dist/install.yaml` and the airgap imageset once
  the `v1.5.8-rc5` tag build completed, matching the pattern used for
  v1.5.6/v1.5.7/v1.5.8-rc1/v1.5.8-rc2/v1.5.8-rc4.

## [1.5.8-rc4] - 2026-08-11

### Changed
- **Images**: Bumped all 13 `RELATED_IMAGE_*` env vars (12 kubernaut
  services + console), the bundle CSV, the airgap imageset, and
  `dist/install.yaml` to reference the freshly-built upstream kubernaut
  `v1.5.6-rc4` and console `v1.5.6-rc4` images — the fixes for #2086,
  #2089, #2092, #2094, #2098, #2100, and #2103 validated in the dev
  environment over the last two days, and expected to be the last RC
  before upstream cuts `v1.5.6` GA. Bumped `VERSION` to `1.5.8-rc4`. The
  operator's own image and bundle references were pinned to their
  published digests in `dist/install.yaml` and the airgap imageset once
  the `v1.5.8-rc4` tag build completed, matching the pattern used for
  v1.5.6/v1.5.7/v1.5.8-rc1/v1.5.8-rc2.

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
  image and bundle references were pinned to their published digests in
  `dist/install.yaml` and the airgap imageset once the `v1.5.8-rc2` tag
  build completed, matching the pattern used for v1.5.6/v1.5.7/v1.5.8-rc1.

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

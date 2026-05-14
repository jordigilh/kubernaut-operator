# Changelog

All notable changes to the Kubernaut Operator will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

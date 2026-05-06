# Changelog

All notable changes to the Kubernaut Operator will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.4.0] - Unreleased

### Added
- **RBAC**: Expanded `kubernaut-agent-investigator` ClusterRole with read-only
  access to OCP and core K8s resources for incident investigation:
  - OLM: CSVs, Subscriptions, InstallPlans, OperatorGroups, CatalogSources
  - OCP platform: Routes, DeploymentConfigs, SCCs, ImageStreams, Builds
  - OCP machine management: Machines, MachineSets, MachineConfigs, MCPs
  - Core K8s: RBAC objects, admission webhooks, CRDs, PriorityClasses
- **RBAC**: Added egress rules to `kubernaut-agent` NetworkPolicy for
  API server, data-storage, and monitoring stack (Thanos Querier TCP 9091,
  AlertManager TCP 9094) access when NetworkPolicies are enabled
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

### Fixed
- **Docs**: Corrected AWX ClusterRole name in `SECURITY.md`
  (`<ns>-awx-integration` → `<ns>-workflowexecution-awx`)
- **Docs**: Updated stale test plan entries (test counts, verb descriptions,
  CI coverage claims)

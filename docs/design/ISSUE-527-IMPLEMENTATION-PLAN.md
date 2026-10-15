# Issue #527 — Operator-managed KA evidence handoff budget

| Field | Value |
|---|---|
| Issue | [#527](https://github.com/jordigilh/kubernaut-operator/issues/527) |
| Release line | v1.6, unreleased v1alpha2 clean-break API |
| Status | Implemented; local verification complete |
| Decision | [`DD-527-KA-AGENT-PROFILE-BOUNDARIES.md`](DD-527-KA-AGENT-PROFILE-BOUNDARIES.md) |

## Objective

Make the operator render the history-aware KA evidence handoff budget as
`262144` bytes by default, preserve administrator overrides, and expose a
clean CRD structure that gives investigation, workflow discovery, and API
Frontend independent LLM profile ownership.

## Effective API

```yaml
spec:
  llmProfiles: {}
  kubernautAgent:
    investigation:
      llmProfileRef: primary
      maxTurns: 40
    workflowDiscovery:
      llmProfileRef: lightweight
      maxEvidenceHandoffBytes: 262144
  apiFrontend:
    llmProfileRef: primary
```

The two numeric fields are optional pointer-backed values with CRD defaults
and positive-value validation. Renderers retain the same effective defaults
when handed an older/in-memory object whose new nested fields are absent.

## Wiring manifest

| Component | Production entry point | Wiring location | Integration evidence |
|---|---|---|---|
| Investigation profile | KA static/runtime ConfigMaps and Deployment | `internal/resources/configmaps.go`, `deployments.go` | live reconciliation renders the selected base profile |
| Workflow-discovery profile | KA runtime `phaseModels.workflow_discovery` and dedicated credential mount | `internal/resources/configmaps.go`, `deployments.go` | live reconciliation renders the phase and mount |
| Evidence handoff budget | KA `kubernaut-agent-config` | `KubernautAgentConfigMap` via `deployConfigMaps` | live ConfigMap contains 262144 or explicit override |
| API Frontend profile | AF ConfigMap/Deployment | existing AF builders and `AFLLMProfileRef` | independent profile validation/rendering test |

## TDD sequence

### RED

- CRD clean-break test proves the nested fields and absence of the removed flat
  fields.
- Resource tests prove the 256KiB default, explicit override, and independent
  workflow-discovery profile rendering.
- Validation tests reject undefined/ambiguous logical-agent profiles and
  prevent API Frontend from inheriting KA's profile when multiple profiles
  exist.
- Envtest reconciliation proves ConfigMap generation and Deployment mount
  wiring.

### GREEN

- Add `InvestigationSpec` and `WorkflowDiscoverySpec`.
- Replace old KA profile/phase resolution with explicit logical-agent
  resolution.
- Render the default/override evidence budget.
- Translate the workflow-discovery profile to upstream runtime phase config.
- Update API Frontend fallback and all precreated Secret references.

### REFACTOR

- Remove stale phase-model API code and dead validation branches.
- Regenerate deepcopy, CRD, bundle, distribution, chart-copy, and sample
  artifacts.
- Update installation/upgrade/config mapping documentation.
- Add the security traceability matrix and verify all test identifiers.

## Validation gates

```bash
go build ./...
golangci-lint run
make manifests generate
make test-unit
make test-integration
make test-pyramid
```

## Verification result

- `make test-unit`: passed; 100% independent business-unit coverage and 100%
  of the configured 90% internal unit threshold.
- `make test-integration`: passed; controller integration coverage 78.1%.
- `make test-pyramid`: passed, including security traceability, CI boundary,
  and Helm contract validation.
- `golangci-lint run`: passed with zero issues.
- `go build ./...`: passed.

The upstream/demo workflow journey remains the qualification boundary for a
sanitized handoff larger than 64KiB and within 256KiB. This repository proves
the operator's generated configuration and reconciliation contract without
duplicating upstream KA behavior.

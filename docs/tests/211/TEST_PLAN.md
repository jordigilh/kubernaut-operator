# IEEE 829 Test Plan — Issue #211: Add LLMReasoningSpec field to LLMProfileSpec for KA + AF reasoning/thinking token support

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-211                                             |
| **Issue**          | #211 — Add LLMReasoningSpec field to LLMProfileSpec for KA + AF reasoning/thinking token support |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-07-23                                         |
| **Scope**          | `api/v1alpha1/kubernaut_types.go`, `internal/resources/{configmaps,validation,common}.go`, `internal/controller/kubernaut_controller_test.go` |

## 1. Objective

Upstream `pkg/shared/types.LLMConfig` (consumed by both KA and AF) gained a
`Reasoning *LLMReasoningConfig` field over the past two weeks
(kubernaut#1598, #1601, #1603, #1606, #1607, #1627), opting a profile in to
model-aware reasoning/thinking-token support (BR-AI-086). The operator's
`LLMProfileSpec` (post-#187's move to named profiles) has no equivalent
field, so operators cannot configure this at all today.

A preflight check (triangulated via direct upstream source read, `gopls`
symbol references scoped to the operator's own build graph, and
`cocoindex_search` over the whole platform index) confirmed the issue's
proposed CRD shape matches upstream exactly, and surfaced two corrections
to the issue's literal text:

1. `kaLLMYAML` (the static KA ConfigMap's LLM block) is used **only** by
   `KubernautAgentConfigMap`, not `KubernautAgentLLMRuntimeConfigMap` as the
   issue states — the hot-reload runtime config uses a separate
   `llmRuntimeYAML` struct with no top-level `Reasoning` field upstream
   either, so this is actually correct behavior, not a gap.
2. Upstream's `LLMOverrideConfig` (used for per-phase hot-reload overrides
   via `LLMRuntimeConfig.PhaseModels[phase]`) *does* carry its own
   `Reasoning *types.LLMReasoningConfig`, explicitly exempted from the
   restart-required LLM-identity lock (DD-LLM-008). The operator's matching
   `llmPhaseOverrideYAML` struct does not yet forward it. Without this, a
   `phaseModels` entry pointing at a profile with different `reasoning`
   settings would silently not take effect at runtime — undermining the
   issue's own worked example. This is added here even though the issue
   frames per-phase support as requiring "no additional CRD surface" (true
   for the CRD; not true for the rendering code).

Verify that:

1. `LLMReasoningSpec` (`Enabled`, `BudgetTokens *int`, `Effort`,
   `CapabilityOverride`) is added to `LLMProfileSpec` as
   `Reasoning *LLMReasoningSpec`, nil-safe, with CRD markers matching
   upstream's vocabulary (`Effort` enum, `CapabilityOverride` enum/default).
2. KA's static ConfigMap (`kaLLMYAML`) forwards the base profile's
   `Reasoning` unchanged.
3. KA's hot-reload runtime ConfigMap (`llmRuntimeYAML.PhaseModels`) forwards
   each phase override's own `Reasoning`, distinct from the base profile's.
4. AF's ConfigMap (`afAgentLLMYAML`, via `afAgentLLMConfig`) forwards
   `Reasoning` for both its main agent LLM and its independent
   severity-triage LLM (single choke point, one fix covers both).
5. CR validation rejects `effort: "none"` + `enabled: true` for
   `anthropic`/`vertex_ai` profiles (Anthropic has no "thinking enabled,
   zero effort" wire state), naming the offending profile.
6. The regenerated CRD schema accepts a non-default `reasoning` block
   through a real API server (envtest), not just via Go-level struct
   construction in unit tests — closing the Pyramid Invariant's
   IT-proves-wiring gap that the issue's own AC10 (UT-only) left open.
7. No behavior change for existing profiles that leave `reasoning` unset.

**Explicitly out of scope** (per the issue's own "Related" section): AF's
`CircuitBreaker`/`CustomHeaders` fields, which are unrelated to reasoning
and not blocking anything currently reported.

## 2. Test Strategy

Standard TDD (RED -> GREEN -> REFACTOR), primarily unit tier
(`internal/resources`) — this is a pure additive rendering/validation
change with no new controller-owned lifecycle, secrets, mounts, RBAC, or
NetworkPolicy surface. One integration scenario is added specifically to
satisfy the Pyramid Invariant's "IT proves wiring" requirement for the new
CRD schema markers (kubebuilder `Enum`/`default` correctness can only be
verified by a real apply through envtest's API server, not by unit tests
that construct Go structs directly and never touch OpenAPI validation).

## 3. Test Scenarios

### 3.1 CRD fields and defaults (`api/v1alpha1` deepcopy)

| ID     | Description | Automated? |
|--------|--------------|------------|
| LR-001 | `LLMProfileSpec.Reasoning` defaults to nil (disabled everywhere until explicit opt-in) | Yes |
| LR-002 | Deepcopy round-trips `Reasoning` (including its `*int` `BudgetTokens`) without aliasing | Yes (`make generate` + build) |

### 3.2 KA static config rendering (`internal/resources/configmaps_test.go`, `KubernautAgent ConfigMap`)

Business objective: extended reasoning has real cost, latency, and output
budget implications, so the deployed agent's actual behavior must match
what the administrator declared — no more, no less (CM-6: automated
mechanism centrally applies configuration settings; secure-by-default
posture for an optional, cost-affecting capability).

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| LR-010 | CM-6    | KA does not spend extra reasoning tokens unless the administrator explicitly opts in (`ai.llm.reasoning` omitted when the resolved profile has no `Reasoning`) | Yes |
| LR-011 | CM-6    | KA's applied reasoning policy exactly matches what the administrator configured (`enabled`/`budgetTokens`/`effort`/`capabilityOverride` render verbatim) | Yes |

### 3.3 KA hot-reload/phase rendering (`internal/resources/configmaps_test.go`, LLM runtime ConfigMap)

Business objective: per-phase reasoning tuning (e.g., cheap/fast reasoning
for high-volume phases, deep reasoning for high-stakes phases) must
actually take effect at runtime and must not silently inherit or leak
another phase's/the base agent's setting (CM-6: configuration changes
apply only to their intended scope).

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| LR-020 | CM-6    | The base profile's `reasoning` is static-only and does not leak into the hot-reloadable runtime config where an operator could mistake it for a live-tunable setting (matches upstream `LLMRuntimeConfig` shape — no top-level `Reasoning` there) | Yes |
| LR-021 | CM-6    | A phase pointed at a lighter-weight reasoning profile actually gets that lighter setting at runtime, not the base agent's — proving per-phase cost/latency tuning is effective, not cosmetic | Yes |
| LR-022 | CM-6    | A phase that opts out of reasoning stays opted out even when the base agent has reasoning enabled — an administrator's phase-specific profile swap must not silently inherit reasoning spend it never configured | Yes |

### 3.4 AF rendering (`internal/resources/configmaps_test.go`, `APIFrontendConfigMap`)

Business objective: same as 3.2/3.3, applied to AF's main agent and its
independently-configured severity-triage LLM (CM-6).

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| LR-030 | CM-6    | AF does not spend extra reasoning tokens unless the administrator explicitly opts in (`agent.llm.reasoning` omitted when the resolved profile has no `Reasoning`) | Yes |
| LR-031 | CM-6    | AF's applied reasoning policy exactly matches what the administrator configured | Yes |
| LR-032 | CM-6    | Severity-triage's reasoning budget is independently configurable from AF's main agent (via `severityTriage.llmProfileRef`), so triage cost/latency can be tuned separately without affecting the main agent | Yes |

### 3.5 Validation (`internal/resources/validation_test.go`, "LLM Profile Content Validation")

Business objective: catch a class of misconfiguration at admission time
that would otherwise reconcile cleanly but fail every affected LLM call
at runtime, only surfacing as a production incident (SI-10: automated
input-validation checks with actionable, specific feedback).

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| LR-040 | SI-10   | An administrator may explicitly disable reasoning while still declaring an effort tier for later re-enablement (`effort: "none"` + `enabled: false` is not a contradiction) | Yes |
| LR-041 | SI-10   | An administrator may enable Anthropic's lowest real reasoning tier without being incorrectly blocked (`effort: "minimal"` + `enabled: true` for `anthropic`) | Yes |
| LR-042 | SI-10   | Rejects a profile that would deploy successfully but fail every Anthropic LLM call at runtime (`effort: "none"` + `enabled: true` for `anthropic`), naming the offending profile so it can be found among dozens | Yes |
| LR-043 | SI-10   | Rejects the same runtime-failure contradiction for Vertex-hosted Claude, not just native Anthropic (`vertex_ai`) | Yes |
| LR-044 | SI-10   | Does not over-broadly block a legitimate `openai`/`openai_compatible` configuration that has no analogous wire-level conflict | Yes |

### 3.6 No behavior change (regression)

| ID     | Description | Automated? |
|--------|--------------|------------|
| LR-050 | All pre-existing KA/AF ConfigMap and validation fixtures (no `Reasoning` set) render byte-identical output to the pre-change baseline | Yes |

### 3.7 Controller integration (`internal/controller/kubernaut_controller_test.go`)

Business objective: the CRD schema itself is an automated input-validation
and configuration-settings mechanism (kubebuilder `Enum`/`default` markers
compiled into the OpenAPI schema the API server enforces on every apply).
Unit tests construct Go structs directly and never touch that schema, so
only a real envtest apply can prove it is wired correctly end-to-end.

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| LR-060 | CM-6    | A Kubernaut CR with `spec.llmProfiles.primary.reasoning` set to a non-default value (`enabled: true`, `effort: "high"`) is accepted by the real envtest API server and reconciles successfully — proves the regenerated CRD schema doesn't block a legitimate administrator-declared configuration | Yes |
| LR-061 | SI-10   | A Kubernaut CR with `spec.llmProfiles.primary.reasoning.effort` set to a value outside the declared enum (e.g. `"extreme"`) is rejected by the real envtest API server itself, before any webhook or reconciler logic runs — proves the `Enum` marker is actually enforced, not just declared | Yes |

## 4. Acceptance Criteria

- All scenarios above pass via `make test-unit` and `make test-integration`.
- `make lint` reports 0 issues.
- `make generate manifests build-installer bundle` regenerated
  `zz_generated.deepcopy.go`, `config/crd/bases/kubernaut.ai_kubernauts.yaml`,
  `dist/install.yaml`, `bundle/manifests/*` with the new field.
- `config/samples/v1alpha1_kubernaut.yaml` and
  `docs/installation/02-configure-services.md` document a `reasoning`
  example, including the per-phase-via-profile-swap pattern.
- No behavior change for CRs that leave `reasoning` unset (fully backward
  compatible).

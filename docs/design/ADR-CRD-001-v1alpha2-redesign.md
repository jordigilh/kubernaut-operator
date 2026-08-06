# ADR-CRD-001: `kubernaut.ai/v1alpha2` CRD Redesign and v1alpha1 Deprecation

**Status**: Proposed -- awaiting sign-off (CHECKPOINT DD)
**Decision Date**: 2026-08-06
**Version**: 1.0
**Confidence**: 88%
**Deciders**: Kubernaut Operator Team
**Applies To**: `api/v1alpha1` -> `api/v1alpha2` CRD migration, conversion webhook, all `internal/resources/*.go` builders, `internal/controller/`, OLM bundle

**Related Business Requirements**:
- BR-API-001: CRD API surface alignment with the upstream Helm chart's `values.schema.json`
- BR-UX-001: Lower the barrier to a first successful `Kubernaut` CR by minimizing required fields

**Related Issues / Milestones**:
- `kubernaut-operator` milestone `v1alpha2` (tracking issues filed for every finding below)
- [#235](https://github.com/jordigilh/kubernaut-operator/issues/235), [#227](https://github.com/jordigilh/kubernaut-operator/issues/227), [#277](https://github.com/jordigilh/kubernaut-operator/issues/277) -- implemented directly against v1alpha2 (D4)
- [#288](https://github.com/jordigilh/kubernaut-operator/issues/288), [#139](https://github.com/jordigilh/kubernaut-operator/issues/139) -- blocked upstream (`kubernaut#1900`); `tokenReviewAudience` -> `tokenReviewAudiences []string` captured as a v1alpha2 design input below so it is correct-by-construction once unblocked

**Upstream References**:
- `jordigilh/kubernaut` `charts/kubernaut/values.schema.json` / `values.yaml` (comparative baseline)
- Five parallel comparative analyses (LLM/KubernautAgent, APIFrontend/Console, Fleet federation, cross-cutting infra, pipeline services) run against `origin/main` of both repos in this session

---

## Changelog

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-08-06 | Operator Team | Initial design spike for sign-off |

---

## Context & Problem

### Current State

The `Kubernaut` CRD (`api/v1alpha1/kubernaut_types.go`, 1858 lines) has grown organically across five releases (v1.1-v1.5) as features landed. It is functionally solid -- 96%+ unit coverage, 78%+ integration coverage, a full singleton reconciliation lifecycle -- but has accumulated structural drift against the upstream Helm chart (`jordigilh/kubernaut/charts/kubernaut`), which itself went through a values-schema cleanup pass this cycle to lower its own barrier to entry.

Two independent artifacts (Helm `values.schema.json`/`values.yaml`, and this CRD) now configure the *same ten kubernaut services* with different shapes for the same concerns. Every new feature that touches shared config (Fleet federation, LLM profiles, rate limiting) has to be implemented twice, in two different shapes, by whoever picks up the operator side vs. the chart side -- and the five-part comparative analysis run this session found the two have already drifted in ways that are bugs, not just style (see below).

### Problem Statement

1. **Structural misalignment increases dual-maintenance cost.** The same nine findings below each represent a place where a Helm chart change and a CRD change, addressing the identical underlying capability, must be implemented independently because the shapes don't correspond.
2. **The CRD has a higher "required fields" floor than the Helm chart.** A new user's first `Kubernaut` CR must specify `postgresql`, `valkey`, at least one `llmProfiles` entry, `kubernautAgent.llmProfileRef`, and (transitively) `aiAnalysis.policy`/`signalProcessing.policy` ConfigMap refs before the CR admits at all -- more mandatory surface than the Helm chart now requires for the equivalent `helm install`.
3. **v1alpha1 already ships in production** (v1.1-v1.5), so any of the fixes below that are breaking (field rename, restructure, type change) cannot land in v1alpha1 without breaking existing CRs. A new API version is the standard Kubernetes mechanism for this.

### Key Alignment Findings (condensed from the 5-part comparative analysis)

| # | Finding | Evidence |
|---|---------|----------|
| F1 | **Fleet flat-vs-nested mismatch, and an outright gap.** 5 existing specs (Gateway, RemediationOrchestrator, SignalProcessing, EffectivenessMonitor, KubernautAgent) each carry a flat `FleetOAuth2CredentialsSecretRef string` field; Helm nests the equivalent as `<component>.fleet.{oauth2.credentialsSecretRef,namespace}`. `WorkflowExecutionSpec` (needed for #235) has **no** Fleet field at all. `APIFrontendSpec`/`EffectivenessMonitorSpec` (needed for #227) have the OAuth2 override but are missing the `namespace` override Helm already ships (only `SignalProcessingSpec`/`FleetMetadataCacheSpec` have `MCPGatewayNamespace` today). | `api/v1alpha1/kubernaut_types.go:307-315,481-496,546-554,700-708,798-806`; Helm `values.schema.json` `<component>.fleet.*` |
| F2 | **No `MonitoringSpec` at all**, despite EM and AF severity-triage functionally depending on a Prometheus URL. The operator today hardcodes `OCPPrometheusURL = "https://thanos-querier.openshift-monitoring.svc:9091"` unconditionally (`internal/resources/common.go:218`) with no CRD override -- there is no BYO/external-Prometheus or non-OCP path. Helm exposes `monitoring.prometheus.{enabled,url,tlsCaFile}`. | `internal/resources/common.go:214-218`, `internal/resources/configmaps.go:1404-1405,2251-2253`; Helm `values.schema.json` `monitoring.prometheus.*` |
| F3 | **`NetworkPoliciesSpec` is a single bool** (`enabled`); Helm has ~10 parameterized override groups (API server CIDRs, per-component ingress/egress, monitoring namespace, IdP/LLM/MCP Gateway egress). | `api/v1alpha1/kubernaut_types.go:1716-1724`; Helm `values.schema.json` `networkPolicies.*` |
| F4 | **Ansible/AAP scope mismatch.** CRD models it top-level (`spec.ansible`); Helm scopes it privately under `workflowexecution.config.ansible` (Ansible is only ever consumed by WorkflowExecution). | `api/v1alpha1/kubernaut_types.go:39-41,350-378`; Helm `values.schema.json` `workflowexecution.config.ansible.*` |
| F5 | **`AlignmentCheckSpec.LLM` still uses the old `{Provider,Model,Endpoint}` shape** with no credentials field (same underlying bug class as `kubernaut#1726`/operator `#237`, which already fixed the *main* KA/AF LLM config to use `llmProfileRef` -- this one spot was missed). Helm already fixed this via `alignmentCheck.llmProfileRef`. | `api/v1alpha1/kubernaut_types.go:948-965` |
| F6 | **JWT provider field mismatch.** CRD `JWTProviderSpec{IssuerURL, Audiences []string, JWKSURL optional}` vs. Helm `{issuer, audience}` (both singular, `jwksURL` required). | `api/v1alpha1/kubernaut_types.go:870-896`; Helm `values.schema.json` `*.jwtProviders[]` |
| F7 | **Rate-limit default drift.** AF's CRD defaults (`ipRequestsPerSec=50, userRequestsPerSec=20, maxConcurrentSessions=100, toolCallsPerMinute=60`) vs. Helm's tuned defaults (`10000/100/50/600` respectively) -- same fields, materially different numbers, meaning a fresh CRD-based install is far more restrictive than a fresh Helm install for identical intent. | `api/v1alpha1/kubernaut_types.go:1545-1565`; Helm `values.schema.json` `apiFrontend.rateLimit.*` |
| F8 | **Smaller drift**: `LoggingSpec.Level` accepts both cases (`DEBUG;INFO;WARN;ERROR;debug;info;warn;error`) where Helm's convention is uppercase-only (ADR-030 upstream); Console route/ingress default posture flips between CRD (`false`, opt-in) and Helm (varies by install profile). | `api/v1alpha1/kubernaut_types.go:1190-1194,1706-1713` |
| F9 | **Required-field floor higher than necessary.** `aiAnalysis.policy` / `signalProcessing.policy` require a pre-existing, user-authored ConfigMap (`approval.rego` / `policy.rego`) with no operator-shipped default -- a new user must author Rego before their first CR admits, even though sane starter policies could ship with the operator. | `api/v1alpha1/kubernaut_types.go:444-467` |

**Note on `PostgreSQLSpec.SSLMode`**: verified separately (see [PR #306](https://github.com/jordigilh/kubernaut-operator/pull/306)) that the CRD's enum already correctly excludes `disable` -- this was a stale doc comment, not a structural finding, and is **not** part of this ADR's scope. The corresponding Helm gap (`datastorage.config.database.sslMode` has no enum and defaults to `disable`) was filed upstream as a comment on [`jordigilh/kubernaut#1120`](https://github.com/jordigilh/kubernaut/issues/1120) (SEC-3), also out of scope here.

---

## Decision Drivers

1. **Structural alignment reduces dual-maintenance cost** -- a change to one artifact's shape should be mechanically portable to the other.
2. **Lower the barrier to a first successful CR** -- minimize required fields, lean on defaults and derivation (the same philosophy already applied to the Helm chart this cycle), consistent with `AGENTS.md`'s "singleton, minimal-config-to-Running" design intent.
3. **No backward-compatibility burden for the *shape* changes themselves** -- v1alpha1 is already released, so shape-breaking fixes need a new API version; the version bump is the vehicle, not optional.
4. **Existing v1alpha1 CRs must keep working** -- this is a production operator; upgrading the operator binary must not orphan or corrupt an existing singleton CR.
5. **Every fix must be traceable to a specific finding above or a filed tracking issue** -- no speculative CRD surface added "for the future."

---

## Decision Axis 1: Versioning & Conversion Strategy

### Alternative A: Conversion webhook, v1alpha2 as hub, v1alpha1 as spoke ✅ CHOSEN

**Approach**: Standard controller-runtime multi-version CRD pattern. `v1alpha2.Kubernaut` implements `conversion.Hub` (marker interface, no-op). `v1alpha1.Kubernaut` implements `conversion.Convertible` (`ConvertTo(hub)`/`ConvertFrom(hub)`). The CRD manifest serves both versions; `v1alpha2` is `+kubebuilder:storageversion`. A conversion webhook (new, `cmd/main.go` registration) performs the translation at the API server boundary -- callers (kubectl, controllers) can read/write either version and the API server converts transparently.

**Why this is materially de-risked here vs. a typical CRD**: `Kubernaut` is a strict cluster-wide singleton (`SingletonName = "kubernaut"`, enforced by the existing validating webhook). At any point in time there is **at most one object** to convert, and the conversion function's correctness surface is one object's full field tree, not an unbounded fleet of differently-shaped instances. This eliminates the usual multi-version CRD risk of "some instances converted, some not, in an inconsistent mix."

**Pros**:
- Standard, well-documented kubebuilder pattern; `controller-gen` generates the boilerplate scaffolding.
- Existing v1alpha1 CRs (from v1.1-v1.5 installs) continue to reconcile with zero user action -- the operator's controller reads v1alpha2 (the hub) and the API server converts on the way in/out.
- `kubectl get kubernaut -o yaml` and `kubectl get kubernaut.v1alpha1` both keep working during the deprecation window.
- Singleton constraint bounds the blast radius to one object, at any time.

**Cons**:
- New infrastructure for this repo -- no conversion webhook exists today (`internal/webhook/singleton_webhook.go` is validation-only). D2 scaffolds this from scratch.
- Lossy-field handling needed for any v1alpha1 field with no v1alpha2 equivalent (see "Conversion & Migration Strategy" below) -- must be designed explicitly, not left implicit.
- Slightly more moving parts to test (IT-CRD-V2-002 must exercise both directions).

**Confidence**: 92% (chosen)

### Alternative B: No conversion -- v1alpha1 removed, users recreate the CR ❌ Rejected

**Approach**: Ship v1alpha2 as the only version. Users on v1alpha1 must delete and recreate their `Kubernaut` CR against the new schema during the upgrade.

**Pros**:
- Zero conversion-webhook infrastructure to build or maintain.
- Simplest possible implementation.

**Cons**:
- Every existing install (v1.1-v1.5 in the field) breaks on operator upgrade until the user manually intervenes -- for a singleton CR that provisions ten services' worth of Deployments/RBAC/NetworkPolicies, that is a disruptive, error-prone manual step with no automated fallback.
- No `kubectl` compatibility window; violates "existing v1alpha1 CRs must keep working" (Decision Driver 4).
- The singleton risk reduction that makes Alternative A cheap (at most one object) applies equally here -- it does not make *this* alternative's downside (forced manual recreation) any less severe, it just means there's only one CR per cluster to break.

**Confidence**: 15% (rejected)

### Alternative C: Serve both versions independently, no conversion, dual controller logic ❌ Rejected

**Approach**: Both `v1alpha1.Kubernaut` and `v1alpha2.Kubernaut` are served and reconciled by *separate* controller logic paths (or the controller special-cases both shapes internally). No `ConvertTo`/`ConvertFrom`.

**Pros**:
- Avoids writing a conversion function.

**Cons**:
- Duplicates (or heavily branches) every resource builder and validation function across two shapes indefinitely -- directly contradicts Decision Driver 1 (this is the exact dual-maintenance problem the whole initiative exists to eliminate, just moved inside the operator instead of between operator/Helm).
- No `kubectl convert`-style transparency; users must know which version their tooling expects.
- Not a pattern controller-runtime provides first-class support for; would be entirely bespoke.

**Confidence**: 10% (rejected)

---

## Decision Axis 2: Structural Alignment Depth

### Alternative A: Deep structural mirror via new shared spec types ✅ CHOSEN

**Approach**: For every cross-cutting concern duplicated or missing across components (Fleet, Monitoring, NetworkPolicies granularity), introduce one shared, embeddable Go type and reuse it everywhere the concern applies, mirroring Helm's nesting:

```go
// Embedded identically in Gateway, RemediationOrchestrator, SignalProcessing,
// EffectivenessMonitor, KubernautAgent, APIFrontend, WorkflowExecution.
type FleetOverrideSpec struct {
    OAuth2CredentialsSecretRef string `json:"oauth2CredentialsSecretRef,omitempty"`
    Namespace                  string `json:"namespace,omitempty"`
}
```

Top-level `spec.monitoring.prometheus.{enabled,url,tlsCaFile}` (new `MonitoringSpec`) replaces the hardcoded `OCPPrometheusURL` constant as the *default value*, not a required field -- unset still means "use OCP's built-in Thanos Querier," matching today's zero-config behavior exactly, but now overridable.

**Pros**:
- Directly closes F1, F2, F3, F4, F5, F6, F7 by construction -- each finding maps to exactly one shared type or field rename.
- One Go type per concern means a future Helm change to that concern has an unambiguous CRD counterpart to update, and vice versa -- this *is* the reduced-maintenance-cost goal.
- Matches the existing precedent already in the CRD: `ShutdownSpec` is already shared between AF and KA (`api/v1alpha1/kubernaut_types.go:1601-1613`); `LoggingSpec` and resource `Resources corev1.ResourceRequirements` are already shared across every component. This alternative generalizes a pattern already proven in this codebase, it does not invent a new one.

**Cons**:
- Larger diff than a field-by-field patch -- touches every component spec that has a Fleet override (7 structs).
- Requires the conversion function to map 5 different flat-string fields (v1alpha1) onto 7 nested `FleetOverrideSpec` structs (v1alpha2, adding WorkflowExecution and filling in the AF/EM namespace gap) -- asymmetric, needs explicit per-field mapping (see migration table below), not a generic transform.

**Confidence**: 90% (chosen)

### Alternative B: Targeted bug-fixes only, no new shared types ❌ Rejected

**Approach**: Fix only the clearest correctness bugs (F5 AlignmentCheck LLM shape, F6 JWT mismatch) in place on v1alpha1-compatible types; leave F1/F2/F3/F4/F7 as-is, filing them as "won't fix, low priority" instead.

**Pros**:
- Smallest possible change; no new API version even needed for F5/F6 alone (though F5/F6 are themselves breaking).

**Cons**:
- Leaves the dual-maintenance cost (Decision Driver 1) almost entirely unaddressed -- this was the primary reason the initiative started.
- F1 blocks #235 outright (WorkflowExecution has no Fleet field at all) and F1's namespace gap blocks half of #227 -- "won't fix" here means #235/#227 cannot be implemented cleanly regardless of API version.

**Confidence**: 20% (rejected)

### Alternative C: Mirror Helm's exact JSON key spelling/casing everywhere (e.g. `datastorage` not `dataStorage`) ❌ Rejected

**Approach**: In addition to structural nesting, also match Helm's exact key names and casing convention, including Helm's lowercase multi-word keys where the CRD currently uses camelCase.

**Pros**:
- Maximizes 1:1 portability -- a config value could theoretically be copy-pasted between a Helm `values.yaml` and a CR `spec:` block for shared concerns.

**Cons**:
- Kubernetes API convention is camelCase for JSON/YAML keys (`dataStorage`, not `datastorage`); deviating from this to match Helm would itself violate a stronger, ecosystem-wide convention than the one it's trying to satisfy, and would read as a bug to anyone familiar with core Kubernetes APIs (`apiVersion`, `resourceVersion`, etc. are all camelCase).
- Helm's own key casing is not fully consistent internally (`datastorage` vs `kubernautAgent` in the same file), so "mirroring Helm exactly" has no single consistent target to mirror.
- Structural nesting (Alternative A, Axis 2) already achieves the portability goal (same shape, same field semantics) without the casing downside -- a human or script translating between the two only needs to know the nesting matches, not that every key is spelled identically.

**Confidence**: 15% (rejected) -- structural mirroring is adopted; exact key-spelling mirroring is not.

---

## Decision Axis 3: Required-Field Minimization Strategy

### Alternative A: Ship operator-bundled default Rego policies for AIAnalysis/SignalProcessing ✅ CHOSEN

**Approach**: Bundle default `approval.rego` / `policy.rego` content in the operator binary (e.g. `internal/resources/policies/default_approval.rego`), auto-generate the ConfigMap when `spec.aiAnalysis.policy` / `spec.signalProcessing.policy` is unset, exactly mirroring how `spec.llmProfiles`/`llmProfileRef` already decouples LLM identity with sane per-component fallback (`APIFrontendSpec.LLMProfileRef` falls back to `KubernautAgentSpec.LLMProfileRef` today -- this is the existing precedent for "optional override with a working fallback").

**Pros**:
- Directly resolves F9; a minimal CR (`postgresql` + `valkey` + one `llmProfiles` entry + `kubernautAgent.llmProfileRef`) becomes sufficient to reach `Running`, matching Decision Driver 2.
- Reuses an already-proven pattern in this codebase rather than inventing a new one (CHECKPOINT B).

**Cons**:
- Default Rego policy content requires its own review (security-sensitive: it's an *approval* gate) -- scoped as a D4 implementation detail, not decided in this ADR. The *mechanism* (optional ref with a bundled fallback) is what this ADR decides; the *policy content* is not.

**Confidence**: 85% (chosen for the mechanism; policy content deferred)

### Alternative B: Keep `policy`/`proactiveSignalMappings` required, document a "day-2" starter policy in `kubernaut-docs` ❌ Rejected

**Pros**: No new operator-bundled content to review/maintain.

**Cons**: Does not resolve F9 -- a new user still cannot reach `Running` without authoring Rego first; directly contradicts Decision Driver 2, which is the entire reason this axis exists.

**Confidence**: 25% (rejected)

---

## Decisions

### Chosen: A / A / A across all three axes

v1alpha2 is the conversion hub; v1alpha1 is a spoke via a conversion webhook (Axis 1). Every cross-cutting finding (F1-F7) is resolved via a shared, embeddable spec type mirroring Helm's nesting, not a field-by-field patch (Axis 2). Required-field reduction is achieved via operator-bundled defaults following the existing `llmProfileRef` fallback precedent, not by leaving fields required (Axis 3).

### Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│                     CRD: kubernaut.ai/Kubernaut                    │
│                                                                     │
│  v1alpha1 (spoke, +kubebuilder:deprecated)   v1alpha2 (hub, storage)│
│  ConvertTo(hub) / ConvertFrom(hub)  <──────>  conversion.Hub{}      │
└───────────────────────┬─────────────────────────────┬──────────────┘
                        │ conversion webhook            │ read/write
                        ▼                               ▼
              ┌──────────────────┐          ┌───────────────────────┐
              │ kube-apiserver   │          │ KubernautReconciler    │
              │ (converts on the │          │ always reads/writes    │
              │  way in/out)     │          │ v1alpha2.Kubernaut     │
              └──────────────────┘          └───────────────────────┘
```

---

## Target v1alpha2 Shape

Each subsection below maps directly to a finding (F1-F9). Only the *changed* portions are shown; everything else in `KubernautSpec` (LLMProfiles, KubernautAgent internals, DataStorage, etc.) carries forward unchanged into v1alpha2 unless noted.

### F1 -- Unified `FleetOverrideSpec`, closes the WorkflowExecution gap and the AF/EM namespace gap

```go
// FleetOverrideSpec overrides spec.fleet's shared OAuth2 credentials and/or
// MCP Gateway namespace scope for a single component. Every field falls
// back to the corresponding spec.fleet.* value when unset.
type FleetOverrideSpec struct {
    // +optional
    OAuth2CredentialsSecretRef string `json:"oauth2CredentialsSecretRef,omitempty"`
    // +optional
    Namespace string `json:"namespace,omitempty"`
}
```

Replaces the flat `FleetOAuth2CredentialsSecretRef string` field with `Fleet *FleetOverrideSpec `json:"fleet,omitempty"`` on: `GatewaySpec`, `RemediationOrchestratorSpec`, `SignalProcessingSpec`, `EffectivenessMonitorSpec`, `KubernautAgentSpec`, `APIFrontendSpec` (new `Namespace` field added here and on EM), `WorkflowExecutionSpec` (entirely new field, unblocks #235), `FleetMetadataCacheSpec` (its bespoke `FleetOAuth2CredentialsSecretRef`/`MCPGatewayNamespace` pair collapses into the same shared type).

### F2 -- New top-level `MonitoringSpec`

```go
// MonitoringSpec configures the Prometheus endpoint used by
// EffectivenessMonitor and API Frontend severity-triage. Unset (the
// default) preserves today's behavior: OCP's built-in Thanos Querier
// at a well-known in-cluster URL, auto-detected, no user action needed.
type MonitoringSpec struct {
    // +optional
    Prometheus PrometheusSpec `json:"prometheus,omitempty"`
}

type PrometheusSpec struct {
    // Defaults to true (auto-detected OCP monitoring stack).
    // +kubebuilder:default=true
    // +optional
    Enabled *bool `json:"enabled,omitempty"`
    // Defaults to the OCP Thanos Querier route when empty.
    // +optional
    URL string `json:"url,omitempty"`
    // +optional
    TLSCaFile string `json:"tlsCaFile,omitempty"`
}
```

`spec.monitoring` added to `KubernautSpec`, `+optional`. `internal/resources/common.go`'s `OCPPrometheusURL` constant becomes the *default value* returned by a new `resolvePrometheusURL(kn)` helper, not the only value.

### F3 -- Parameterized `NetworkPoliciesSpec`

Expands the single `Enabled *bool` into the ~10 Helm override groups (API server CIDRs, per-component ingress/egress toggles, monitoring namespace override, IdP/LLM/MCP Gateway egress overrides). Exact field list is a D2/D3 implementation detail against the Helm schema at implementation time (not fully enumerated in this ADR to avoid the target drifting from Helm's schema between sign-off and D2); the *decision* here is that `NetworkPoliciesSpec` grows from 1 field to a fully parameterized struct mirroring `values.schema.json`'s `networkPolicies.*` tree.

### F4 -- Ansible relocation

`spec.ansible` (top-level `AnsibleSpec`) moves to `spec.workflowExecution.ansible` (`WorkflowExecutionSpec.Ansible AnsibleSpec`). `AnsibleSpec` itself is unchanged. This matches Ansible's actual single consumer (WorkflowExecution) and Helm's existing scoping.

### F5 -- `AlignmentCheckSpec.LLM` becomes a profile reference

```go
type AlignmentCheckSpec struct {
    Enabled       bool   `json:"enabled,omitempty"`
    Timeout       string `json:"timeout,omitempty"`
    MaxStepTokens int    `json:"maxStepTokens,omitempty"`
    // Replaces *AlignmentCheckLLMSpec{Provider,Model,Endpoint}. Reference
    // to a named profile in spec.llmProfiles, matching every other LLM
    // consumer in the CRD (KubernautAgent, APIFrontend, severity-triage).
    // +optional
    LLMProfileRef string `json:"llmProfileRef,omitempty"`
}
```

`AlignmentCheckLLMSpec` type is removed entirely (it never had a working credentials path -- same bug class as `#237`).

### F6 -- `JWTProviderSpec` aligned with Helm's field names

```go
type JWTProviderSpec struct {
    Name      string   `json:"name"`
    IssuerURL string   `json:"issuerURL"`           // was: matches Helm's "issuer" semantically, keeps CRD's more descriptive name (see Axis 2 Alt C rejection -- structure > exact spelling)
    JWKSURL   string   `json:"jwksURL"`              // was optional/derived; Helm requires it explicitly -- v1alpha2 makes it required to match, still auto-fillable by admission-time defaulting from IssuerURL if we choose to keep the derivation as a defaulting webhook behavior (D2 implementation detail)
    Audiences []string `json:"audiences"`            // unchanged; CRD's plural array is a strict superset of Helm's singular "audience" and is kept (no information loss, no rejected alternative needed)
    ClaimMappings *ClaimMappingsSpec `json:"claimMappings,omitempty"`
}
```

### F7 -- Rate-limit defaults aligned to Helm's tuned values

`APIFrontendRateLimitSpec` field set is unchanged; only `+kubebuilder:default` values change: `ipRequestsPerSec` 50->10000, `userRequestsPerSec` 20->100, `maxConcurrentSessions` 100->50, `toolCallsPerMinute` 60->600, matching Helm's `apiFrontend.rateLimit.*` defaults exactly.

### F8 -- Minor consistency fixes

`LoggingSpec.Level` enum narrows to uppercase-only (`DEBUG;INFO;WARN;ERROR`), matching upstream ADR-030. Console/Route default posture is explicitly decided per-field at D2 implementation time against the current Helm default for that specific install profile (not a single global flip -- flagged here so it isn't silently forgotten, not resolved in this ADR).

### F9 -- Optional policy refs with bundled defaults

```go
type AIAnalysisSpec struct {
    // Optional; when unset, the operator generates a ConfigMap from a
    // bundled default approval.rego (D4 implementation detail: policy
    // content reviewed separately, see Decision Axis 3).
    // +optional
    Policy *PolicyConfigMapRef `json:"policy,omitempty"`
    ConfidenceThreshold string `json:"confidenceThreshold,omitempty"`
    // ...unchanged
}
```

Same pattern for `SignalProcessingSpec.Policy`.

---

## v1alpha1 -> v1alpha2 Conversion & Migration Strategy

| v1alpha1 field | v1alpha2 field | Conversion notes |
|---|---|---|
| `<component>.fleetOAuth2CredentialsSecretRef` (5 components) | `<component>.fleet.oauth2CredentialsSecretRef` | Direct value copy into the new nested struct; `Namespace` left empty (was never present in v1alpha1 for these components) |
| `signalProcessing.mcpGatewayNamespace`, `fleetMetadataCache.mcpGatewayNamespace` | `signalProcessing.fleet.namespace`, `fleetMetadataCache.fleet.namespace` | Direct value copy |
| *(none -- gap)* | `workflowExecution.fleet.*` | No v1alpha1 source; `ConvertFrom` leaves it unset (zero value) |
| *(none -- gap)* | `apiFrontend.fleet.namespace`, `effectivenessMonitor.fleet.namespace` | No v1alpha1 source; unset on convert |
| *(none -- gap)* | `monitoring.prometheus.*` | No v1alpha1 source; unset on convert (falls back to OCP default, matching v1alpha1's only behavior today) |
| `ansible.*` (top-level) | `workflowExecution.ansible.*` | Direct value copy, relocated |
| `kubernautAgent.alignmentCheck.llm.{provider,model,endpoint}` | `kubernautAgent.alignmentCheck.llmProfileRef` | **Lossy / no automatic mapping.** v1alpha1's `{provider,model,endpoint}` never had a working credentials path (F5), so there is no live profile to reference. `ConvertTo` (v1alpha2->v1alpha1) drops `llmProfileRef` back into the old shape as best-effort (`provider`/`model` only, `endpoint` empty); `ConvertFrom` (v1alpha1->v1alpha2) leaves `llmProfileRef` empty and the conversion webhook emits a `Warning` response so `kubectl apply` surfaces it. Documented explicitly in the D5 migration guide as a manual step. |
| `jwtProviders[].jwksURL` (optional) | `jwtProviders[].jwksURL` (required) | Direct copy when present. When absent in v1alpha1, `ConvertFrom` derives it from `issuerURL + "/protocol/openid-connect/certs"` (same derivation the runtime already does today) so the required field is never left empty by an automatic conversion. |
| `apiFrontend.rateLimit.*` (unset -- uses old defaults) | `apiFrontend.rateLimit.*` (unset -- uses new defaults) | No value copy needed; CRD defaulting produces the new numbers automatically for any CR that didn't explicitly override these fields. **Only CRs that explicitly set the old defaults as explicit values are unaffected by the default change** (unlikely in practice, since they'd be redundant with the old default) -- flagged in the migration guide regardless. |
| `aiAnalysis.policy` / `signalProcessing.policy` (required) | `aiAnalysis.policy` / `signalProcessing.policy` (optional) | Direct value copy when present; conversion is non-lossy either direction (v1alpha2->v1alpha1 requires re-adding the field only if it was genuinely unset, which the webhook flags the same way as the AlignmentCheck case above). |

**General principle**: every conversion is a direct value copy except the two flagged rows (AlignmentCheck LLM shape, and the theoretical "explicit old default" edge case for rate limits), both of which are pre-existing bugs or genuinely unrepresentable states in v1alpha1, not information the conversion function is responsible for inventing. Both lossy cases are called out explicitly in the D5 migration guide with a manual remediation step.

---

## Wiring Manifest (D2 scaffold target -- finalized at D2 kickoff)

| Component | Production Entry Point | Wiring Code Location | IT Test ID |
|---|---|---|---|
| `v1alpha2.Kubernaut` (hub) | `KubernautReconciler.Reconcile()` | `internal/controller/kubernaut_controller.go` | IT-CRD-V2-001 |
| Conversion webhook (v1alpha1 <-> v1alpha2) | `cmd/main.go` (`ctrl.NewWebhookManagedBy`) | `api/v1alpha1/kubernaut_conversion.go` (new) | IT-CRD-V2-002 |
| `FleetOverrideSpec` resolution | `internal/resources/*.go` (per-component builders) | new `resolveFleetOverride(kn, component)` helper in `internal/resources/common.go` | re-points existing Fleet IT coverage |
| `MonitoringSpec`/`resolvePrometheusURL` | `internal/resources/configmaps.go` (EM, AF severity-triage) | `internal/resources/common.go` | re-points existing monitoring IT coverage |
| Default Rego policy bundling | `internal/controller/kubernaut_controller.go` (AIAnalysis/SignalProcessing reconcile) | new `internal/resources/policies/` package | new IT test, ID TBD at D4 |

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Conversion webhook is new, untested infrastructure for this repo | Singleton constraint bounds testing surface to one object; IT-CRD-V2-002 exercises both `ConvertTo`/`ConvertFrom` directions explicitly with the lossy-field cases from the migration table above |
| D2-D4 is a multi-week effort touching nearly every resource builder | Phased roadmap (D2 scaffold -> D3 migrate builders component-by-component -> D4 implement #235/#227/#277 -> D5 deprecate) keeps the pyramid invariant (UT+IT per component group) intact throughout rather than one big-bang PR |
| F3's NetworkPolicies field list is deliberately not fully enumerated here | Re-verified against Helm's `values.schema.json` at D2/D3 kickoff, not frozen at ADR sign-off time, to avoid the target silently drifting from upstream between now and implementation |
| Lossy AlignmentCheck LLM conversion could silently drop config on downgrade | Conversion webhook returns a `Warning` (not silent) on `ConvertFrom` when the legacy shape had non-empty `provider`/`model`; migration guide (D5) documents the manual `llmProfileRef` step explicitly |

---

## Confidence Assessment

**Confidence: 88%**

**Justification**: The conversion strategy (Axis 1) is a standard, well-documented kubebuilder pattern significantly de-risked by the CRD's strict singleton constraint (92% confidence on its own). The structural alignment approach (Axis 2) generalizes a pattern (`ShutdownSpec`, `LoggingSpec` sharing) already proven in this exact codebase (90% confidence on its own). The required-field minimization mechanism (Axis 3) also follows an existing precedent (`llmProfileRef` fallback chain) but its policy-content dependency is explicitly deferred, not resolved (85% on the mechanism only). The combined 88% reflects two remaining risks that are structural to the *size* of this initiative rather than to any single decision: (1) F3's NetworkPolicies field list is intentionally left unfrozen pending D2/D3 re-verification against Helm, and (2) the multi-week D2-D5 execution surface (every resource builder, every validation function) has more opportunity for an individual builder migration to surface an unanticipated edge case than a single-PR change would, even though the phased roadmap is designed to catch those incrementally rather than in one big-bang cutover.

**What would raise this to 95%+**: A completed D2 scaffold with the conversion webhook's IT-CRD-V2-002 passing end-to-end (validates Axis 1 empirically, not just on paper) before D3 migration begins in earnest.

---

## Sign-off Requested

Per `AGENTS.md` CHECKPOINT DD, this architectural change requires explicit user approval before D2 (scaffold) begins. Specifically requesting a decision on:

1. **Axis 1** (conversion webhook, v1alpha2 hub / v1alpha1 spoke) -- proceed, or reconsider Alternative B/C?
2. **Axis 2** (shared `FleetOverrideSpec`/`MonitoringSpec` types, deep structural mirror) -- proceed, or scope down to Alternative B (targeted fixes only)?
3. **Axis 3** (bundled default Rego policies, mechanism only -- content deferred to D4) -- proceed?
4. **F3's NetworkPolicies field list** left unfrozen until D2/D3 -- acceptable, or should it be fully enumerated in this ADR before sign-off?
5. Any of F1-F9 you'd like pulled out of v1alpha2 scope entirely (deferred to a future v1alpha3), given the size of this initiative?

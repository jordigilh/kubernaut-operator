# ADR-CRD-001: `kubernaut.ai/v1alpha2` CRD Redesign and v1alpha1 Deprecation

**Status**: Accepted (CHECKPOINT DD sign-off obtained 2026-08-06)
**Decision Date**: 2026-08-06
**Version**: 1.6
**Confidence**: 93%
**Deciders**: Kubernaut Operator Team
**Applies To**: `api/v1alpha1` -> `api/v1alpha2` CRD migration, conversion webhook, all `internal/resources/*.go` builders, `internal/controller/`, OLM bundle

**Related Business Requirements**:
- BR-API-001: CRD API surface alignment with the upstream Helm chart's `values.schema.json`
- BR-UX-001: Lower the barrier to a first successful `Kubernaut` CR by minimizing required fields

**Related Issues / Milestones**:
- `kubernaut-operator` milestone `v1alpha2` (tracking issues filed for every finding below)
- [#235](https://github.com/jordigilh/kubernaut-operator/issues/235), [#227](https://github.com/jordigilh/kubernaut-operator/issues/227), [#277](https://github.com/jordigilh/kubernaut-operator/issues/277) -- implemented directly against v1alpha2 (D4)
- [#288](https://github.com/jordigilh/kubernaut-operator/issues/288) -- resolved by removing `tokenReviewAudience` outright rather than renaming it. `kubernaut#1900`'s [2026-08-04 maintainer comment](https://github.com/jordigilh/kubernaut/issues/1900#issuecomment-5180682188) revealed PR #1909 implemented and then reverted audience-bound `TokenReview` for both AF and KA before merging: AF's check worked but was shelved as unclear incremental value over its existing `SubjectAccessReview` gate; KA's is architecturally incompatible with the shared `/api/v1/mcp` authenticator (`DD-AUTH-MCP-001`) and would need an endpoint redesign, not config wiring. No `tokenReviewAudience(s)` field exists in AF's real `AuthConfig` today and none is in flight, so the CRD field was dead code with no viable replacement; [#139](https://github.com/jordigilh/kubernaut-operator/issues/139) remains blocked pending that upstream redesign
- [#139](https://github.com/jordigilh/kubernaut-operator/issues/139) -- still blocked upstream (`kubernaut#1900`); revisit once AF's audience check (if resurrected) and/or a KA `/api/v1/mcp` authenticator redesign land

**Upstream References**:
- `jordigilh/kubernaut` `charts/kubernaut/values.schema.json` / `values.yaml` (comparative baseline)
- Five parallel comparative analyses (LLM/KubernautAgent, APIFrontend/Console, Fleet federation, cross-cutting infra, pipeline services) run against `origin/main` of both repos in this session

---

## Changelog

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-08-06 | Operator Team | Initial design spike for sign-off |
| 1.1 | 2026-08-06 | Operator Team | Reviewer feedback: (1) Axis 3/F9 premise retracted -- Helm's `aianalysis.yaml`/`signalprocessing.yaml` templates `fail` the install when policy content is unset, so `policy`/`proactiveSignalMappings` stay required in v1alpha2, matching Helm exactly, no bundled defaults. (2) F3 strengthened -- Helm has no `networkPolicies.enabled` toggle at all (unconditional render); `NetworkPoliciesSpec.Enabled *bool` is removed in v1alpha2, NetworkPolicies become unconditional, reinforced by Red Hat's OpenShift Hardening mandate (ship NetworkPolicies from 2027-02) and the already-satisfied Conforma RBAC check (`olm.required_network_policy_rbac_for_operands`, effective 2026-08-08). |
| 1.2 | 2026-08-06 | Operator Team | Reviewer confirmed Helm's `networkPolicies.*` schema is stable; F3's field list frozen and fully enumerated (was previously deferred to D2/D3) as `NetworkPoliciesSpec` + 5 shared sub-types (`NetworkPolicyIngressOverride`, `NetworkPolicyNamedIngressOverride`, `NetworkPolicyEgressOverride`, `NetworkPolicyIdPEgressOverride`, `NetworkPolicyMonitoringOverride`), mirroring `values.schema.json` exactly. Removes the "field list may drift before D2/D3" risk; confidence 90% -> 92%. |
| 1.3 | 2026-08-06 | Operator Team | F9 refinement, found during a post-scaffold live-cluster CRD spike (D2): "policy stays required" was implemented as F9 originally specified (only `AIAnalysisSpec.Policy`/`SignalProcessingSpec.Policy` marked required), but that alone doesn't close the gap -- `KubernautSpec.AIAnalysis`/`SignalProcessing` themselves were still `omitempty`, and Kubernetes' structural schema only checks a nested `required` list when the parent object is present in the request. A CR omitting `spec.aiAnalysis`/`spec.signalProcessing` entirely was admitted with no error, verified directly against a live OCP cluster. Fixed by also dropping `omitempty` on the two parent fields (`api/v1alpha2/kubernaut_types.go`) and backfilling `policy.configMapName` in the v1alpha1->v1alpha2 conversion webhook with the same default names `internal/resources/common.go` already falls back to, so existing v1alpha1 CRs relying on that convention don't fail to convert once v1alpha2 becomes the storage version. See F9 section below. Also filed [jordigilh/kubernaut#1984](https://github.com/jordigilh/kubernaut/issues/1984) upstream: Helm's `values.schema.json` has the same class of gap chart-wide (conditional mandatory-ness enforced only by template `fail` guards, never expressed in the schema itself) -- 27 occurrences across 13 templates, not just these two policy blocks. |
| 1.4 | 2026-08-06 | Operator Team | New finding, F10: `kubernautAgent.llmProfileRef` becomes optional in v1alpha2 when `spec.llmProfiles` defines exactly one profile -- the operator infers that sole profile by count rather than requiring it be explicitly named, lowering the barrier for the single-provider case. Corrects §Context finding note (line 47), which had claimed no required-field-floor gap existed for this field relative to Helm; Helm in fact already has a related mechanism (`kubernautAgent.llmProfileRef` defaults to the fixed string `"primary"`, DD-PLATFORM-006 DA4) that this ADR deliberately does *not* mirror -- a fixed-name convention is a worse UX than count-based inference (requires the user to already know the convention; count-based inference doesn't). Implemented via `internal/resources/common.go`'s new `EffectiveKALLMProfileRef`, which every existing fallback consumer (`AFLLMProfileRef`, `KubernautAgentDeployment`, the KA `NetworkPolicy` egress builder) now calls instead of reading `spec.kubernautAgent.llmProfileRef` directly, so severity-triage and AF inherit the inference transitively without their own cardinality check. Filed [jordigilh/kubernaut#1987](https://github.com/jordigilh/kubernaut/issues/1987) upstream proposing Helm adopt the same count-based rule in place of the `"primary"` default. See F10 section below. |
| 1.5 | 2026-08-06 | Operator Team | New finding, F11: renamed the AIAnalysis policy ConfigMap fallback default from plural `aianalysis-policies` to singular `aianalysis-policy`, matching SignalProcessing's already-singular `signalprocessing-policy` convention -- both ConfigMaps hold exactly one Rego file, so the plural name didn't match reality. Go-level default-string change only (no CRD schema diff); updated in lockstep across `internal/resources/common.go`, the F9 backfill constant in `api/v1alpha1/kubernaut_conversion.go`, both sample CRs, installation docs, and test fixtures. Filed [jordigilh/kubernaut#1989](https://github.com/jordigilh/kubernaut/issues/1989) upstream proposing Helm rename its identical `aianalysis-policies` default for the same reason. See F11 section below. |
| 1.6 | 2026-08-07 | Operator Team | New finding, F12: closed the same OAuth2-mandatory gap upstream just fixed for the Helm chart ([kubernaut#1991](https://github.com/jordigilh/kubernaut/issues/1991)/[#1992](https://github.com/jordigilh/kubernaut/pull/1992)) -- `FleetSpec.OAuth2` was documented as optional in v1alpha2, but every fleet-aware component except FleetMetadataCache fails closed (crash-loop) at startup with no unauthenticated MCP Gateway mode. Added a `FleetSpec`-level CEL rule (v1alpha2 only) requiring `oauth2.enabled=true` whenever `enabled=true` and `mcpGatewayEndpoint` is set, gated the same way `AnsibleSpec`'s existing rule is so Fleet's documented "inert while disabled" pre-staging contract is preserved. Discovered while reconciling three scattered Fleet sources (the ADR, this scaffold branch, and Fleet's existing v1alpha1-only implementation already shipped to `main` under seven closed v1.6 issues) as part of planning the Fleet v1alpha2 migration. See F12 section below. |

---

## Context & Problem

### Current State

The `Kubernaut` CRD (`api/v1alpha1/kubernaut_types.go`, 1858 lines) has grown organically across five releases (v1.1-v1.5) as features landed. It is functionally solid -- 96%+ unit coverage, 78%+ integration coverage, a full singleton reconciliation lifecycle -- but has accumulated structural drift against the upstream Helm chart (`jordigilh/kubernaut/charts/kubernaut`), which itself went through a values-schema cleanup pass this cycle to lower its own barrier to entry.

Two independent artifacts (Helm `values.schema.json`/`values.yaml`, and this CRD) now configure the *same ten kubernaut services* with different shapes for the same concerns. Every new feature that touches shared config (Fleet federation, LLM profiles, rate limiting) has to be implemented twice, in two different shapes, by whoever picks up the operator side vs. the chart side -- and the five-part comparative analysis run this session found the two have already drifted in ways that are bugs, not just style (see below).

### Problem Statement

1. **Structural misalignment increases dual-maintenance cost.** The eight active findings below (F1-F8; a ninth, F9, was investigated and retracted -- see below) each represent a place where a Helm chart change and a CRD change, addressing the identical underlying capability, must be implemented independently because the shapes don't correspond.
2. **The CRD makes NetworkPolicies opt-in where Helm makes them unconditional (F3).** This is the one place the *required-fields* framing runs backwards from the rest of this document: Helm has no `enabled` toggle at all and always creates NetworkPolicies, while the CRD defaults `spec.networkPolicies.enabled` to `false`. Aligning means *removing* an optional field, not adding a default to one that's already required. (`postgresql`, `valkey`, `llmProfiles`, and `aiAnalysis.policy`/`signalProcessing.policy` were also reviewed for a "required fields floor" gap -- none found; Helm requires the same fields for the equivalent `helm install`. `kubernautAgent.llmProfileRef` *is* such a gap, addressed as F10 below -- Helm already relaxes it via a fixed-name default, and this ADR goes one step further with count-based inference instead.)
3. **v1alpha1 already ships in production** (v1.1-v1.5), so any of the fixes below that are breaking (field rename, restructure, type change) cannot land in v1alpha1 without breaking existing CRs. A new API version is the standard Kubernetes mechanism for this.

### Key Alignment Findings (condensed from the 5-part comparative analysis)

| # | Finding | Evidence |
|---|---------|----------|
| F1 | **Fleet flat-vs-nested mismatch, and an outright gap.** 5 existing specs (Gateway, RemediationOrchestrator, SignalProcessing, EffectivenessMonitor, KubernautAgent) each carry a flat `FleetOAuth2CredentialsSecretRef string` field; Helm nests the equivalent as `<component>.fleet.{oauth2.credentialsSecretRef,namespace}`. `WorkflowExecutionSpec` (needed for #235) has **no** Fleet field at all. `APIFrontendSpec`/`EffectivenessMonitorSpec` (needed for #227) have the OAuth2 override but are missing the `namespace` override Helm already ships (only `SignalProcessingSpec`/`FleetMetadataCacheSpec` have `MCPGatewayNamespace` today). | `api/v1alpha1/kubernaut_types.go:307-315,481-496,546-554,700-708,798-806`; Helm `values.schema.json` `<component>.fleet.*` |
| F2 | **No `MonitoringSpec` at all**, despite EM and AF severity-triage functionally depending on a Prometheus URL. The operator today hardcodes `OCPPrometheusURL = "https://thanos-querier.openshift-monitoring.svc:9091"` unconditionally (`internal/resources/common.go:218`) with no CRD override -- there is no BYO/external-Prometheus or non-OCP path. Helm exposes `monitoring.prometheus.{enabled,url,tlsCaFile}`. | `internal/resources/common.go:214-218`, `internal/resources/configmaps.go:1404-1405,2251-2253`; Helm `values.schema.json` `monitoring.prometheus.*` |
| F3 | **`NetworkPoliciesSpec` is a single opt-in bool** (`enabled`, defaults `false`); Helm has **no toggle at all** -- `networkPolicies.*` in `values.schema.json` has no `enabled` property, and every `templates/<component>/networkpolicy.yaml` renders unconditionally (no `{{- if }}` guard). Helm creates NetworkPolicies for every install, unconditionally; the CRD makes them opt-in and off by default -- this is a **security regression relative to Helm**, not just a granularity gap. Reinforced externally: Conforma's `olm.required_network_policy_rbac_for_operands` check (RBAC-only, effective 2026-08-08 -- already satisfied by this operator's existing `ClusterRole`) and Red Hat's OpenShift Hardening requirement that operators actually *ship* NetworkPolicies (not just have permission to manage them), beginning 2027-02. | `api/v1alpha1/kubernaut_types.go:1716-1724`; Helm `values.schema.json` `networkPolicies.*` (no `enabled` key); `charts/kubernaut/templates/aianalysis/networkpolicy.yaml` (unconditional render); `config/rbac/role.yaml:152-163` (RBAC already compliant) |
| F4 | **Ansible/AAP scope mismatch.** CRD models it top-level (`spec.ansible`); Helm scopes it privately under `workflowexecution.config.ansible` (Ansible is only ever consumed by WorkflowExecution). | `api/v1alpha1/kubernaut_types.go:39-41,350-378`; Helm `values.schema.json` `workflowexecution.config.ansible.*` |
| F5 | **`AlignmentCheckSpec.LLM` still uses the old `{Provider,Model,Endpoint}` shape** with no credentials field (same underlying bug class as `kubernaut#1726`/operator `#237`, which already fixed the *main* KA/AF LLM config to use `llmProfileRef` -- this one spot was missed). Helm already fixed this via `alignmentCheck.llmProfileRef`. | `api/v1alpha1/kubernaut_types.go:948-965` |
| F6 | **JWT provider field mismatch.** CRD `JWTProviderSpec{IssuerURL, Audiences []string, JWKSURL optional}` vs. Helm `{issuer, audience}` (both singular, `jwksURL` required). | `api/v1alpha1/kubernaut_types.go:870-896`; Helm `values.schema.json` `*.jwtProviders[]` |
| F7 | **Rate-limit default drift.** AF's CRD defaults (`ipRequestsPerSec=50, userRequestsPerSec=20, maxConcurrentSessions=100, toolCallsPerMinute=60`) vs. Helm's tuned defaults (`10000/100/50/600` respectively) -- same fields, materially different numbers, meaning a fresh CRD-based install is far more restrictive than a fresh Helm install for identical intent. | `api/v1alpha1/kubernaut_types.go:1545-1565`; Helm `values.schema.json` `apiFrontend.rateLimit.*` |
| F8 | **Smaller drift**: `LoggingSpec.Level` accepts both cases (`DEBUG;INFO;WARN;ERROR;debug;info;warn;error`) where Helm's convention is uppercase-only (ADR-030 upstream); Console route/ingress default posture flips between CRD (`false`, opt-in) and Helm (varies by install profile). | `api/v1alpha1/kubernaut_types.go:1190-1194,1706-1713` |
| F9 | ~~Required-field floor higher than necessary~~ -- **investigated and retracted, kept for audit trail.** Originally proposed that `aiAnalysis.policy` / `signalProcessing.policy` should become optional with an operator-bundled default. Re-verified against `charts/kubernaut/templates/aianalysis/aianalysis.yaml` (and the SignalProcessing equivalent): Helm's own template `{{- fail "aianalysis.policies.content is required ..." }}` when neither `content` nor `existingConfigMap` is set -- **Helm requires this too.** There is no alignment gap here; both artifacts already agree that a policy is mandatory. See Decision Axis 3 below -- premise retracted, decision flipped accordingly. | `api/v1alpha1/kubernaut_types.go:444-467`; `charts/kubernaut/templates/aianalysis/aianalysis.yaml` (`fail` guard) |

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

**Premise correction (post-draft, pre-sign-off)**: this axis originally proposed bundling default Rego policies so `aiAnalysis.policy`/`signalProcessing.policy` could become optional (F9). That premise was **incorrect**. Re-verification against `charts/kubernaut/templates/aianalysis/aianalysis.yaml` (and the SignalProcessing equivalent) shows Helm's own chart fails the install outright when neither `policies.content` nor `policies.existingConfigMap` is set:

```yaml
{{- if not .Values.aianalysis.policies.existingConfigMap }}
{{- if not .Values.aianalysis.policies.content }}
{{- fail "aianalysis.policies.content is required (via --set-file aianalysis.policies.content=approval.rego) or provide aianalysis.policies.existingConfigMap" }}
{{- end }}
{{- end }}
```

Helm requires exactly what the CRD requires today. There is no alignment gap to close, and Decision Driver 2 ("lower the barrier... consistent with the philosophy already applied to the Helm chart") does not apply here, because the Helm chart itself never adopted that philosophy for this field. Bundling a default *approval* policy would make the operator **less strict than Helm**, not more aligned with it -- the opposite of this ADR's stated goal.

### Alternative A: Keep `policy`/`proactiveSignalMappings` required, matching Helm exactly ✅ CHOSEN

**Approach**: No change. `spec.aiAnalysis.policy` / `spec.signalProcessing.policy` remain required in v1alpha2, identical to v1alpha1 and identical to Helm's `aianalysis.policies.{content,existingConfigMap}` mandatory-one-of.

**Pros**:
- Matches Helm's actual (verified) behavior exactly -- true structural alignment, not just "fewer required fields for its own sake."
- Avoids shipping unrequested, security-sensitive default *approval* content (an approval gate is exactly the kind of default a platform team should author deliberately, not inherit silently from the operator binary).
- Zero new code, zero new review surface, zero new conversion complexity.

**Cons**: A new user must still author (or copy a documented starter) Rego policy before their first CR reaches `Running` -- but this is identical to the Helm `helm install` experience today, not a CRD-specific gap.

**Confidence**: 95%

### Alternative B: Ship operator-bundled default Rego policies for AIAnalysis/SignalProcessing ❌ Rejected

**Approach**: Bundle default `approval.rego`/`policy.rego` content in the operator binary and auto-generate the ConfigMap when the ref is unset.

**Pros**: Would let a minimal CR reach `Running` with even less user-authored content than Helm requires.

**Cons**:
- Contradicts the verified Helm behavior above -- this alternative would make the CRD *diverge* from Helm, not align with it.
- Introduces unrequested, security-sensitive default content (an approval gate) into the operator binary with no upstream precedent asking for it.
- No real user pain point motivates it once the false premise (F9) is removed -- both artifacts already agree a policy is mandatory.

**Confidence**: 15% (rejected)

---

## Decisions

### Chosen: A / A / A across all three axes

v1alpha2 is the conversion hub; v1alpha1 is a spoke via a conversion webhook (Axis 1). Every cross-cutting finding (F1-F8) is resolved via a shared, embeddable spec type mirroring Helm's nesting, not a field-by-field patch (Axis 2). Required-field minimization is scoped to where an actual gap exists (there is none for policy refs, once F9's premise is retracted); `aiAnalysis.policy`/`signalProcessing.policy` stay required, matching Helm exactly (Axis 3).

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

### F3 -- `NetworkPoliciesSpec` becomes unconditional; opt-out removed

**Correction from the initial draft**: F3 is not just "expand a bool into more granular options." Helm has no opt-out at all -- verified no `enabled` property in `values.schema.json`'s `networkPolicies.*`, and every `templates/<component>/networkpolicy.yaml` renders with no `{{- if }}` guard. NetworkPolicies are created unconditionally on every Helm install. The CRD's `Enabled *bool` (default `false`, opt-in) is the opposite posture, and is a security regression relative to Helm, not merely a parameterization gap.

This is also no longer just an alignment nice-to-have: Red Hat's OpenShift Hardening requirements mandate that operators actually *ship* NetworkPolicies (not just declare RBAC permission to manage them) beginning 2027-02, and the nearer-term Conforma check `olm.required_network_policy_rbac_for_operands` (RBAC-only, effective 2026-08-08) is already satisfied by this operator's existing `ClusterRole` (`config/rbac/role.yaml:152-163` already grants full CRUD on `networking.k8s.io/networkpolicies` -- no gap on the RBAC side).

**Decision**: `NetworkPoliciesSpec.Enabled *bool` is removed entirely in v1alpha2. NetworkPolicies are created unconditionally for every component, matching Helm's actual behavior and the Red Hat 2027-02 trajectory. The remaining Helm override groups (API server CIDRs, per-component ingress/egress, monitoring namespace override, IdP/LLM/MCP Gateway egress overrides) become tuning knobs for *how* the always-on policies are shaped, not gates for *whether* they exist.

**Field list -- frozen at sign-off** (Helm's `networkPolicies.*` schema for this area is stable; enumerated directly against `charts/kubernaut/values.schema.json`, not deferred to D2/D3):

```go
// NetworkPoliciesSpec configures the always-on NetworkPolicies the operator
// creates for every component (F3 -- Enabled is removed; NetworkPolicies are
// unconditional, matching Helm). Every field below tunes an already-created
// default-deny + explicit-allow policy set; none of them gate existence.
type NetworkPoliciesSpec struct {
    // Primary K8s API server backend CIDR, for environments where
    // default detection doesn't resolve correctly.
    // +optional
    APIServerCIDR string `json:"apiServerCIDR,omitempty"`
    // Additional API server backend endpoint IPs as /32 CIDRs, for HA
    // clusters with multiple control-plane nodes. Merged with APIServerCIDR.
    // +optional
    APIServerCIDRs []string `json:"apiServerCIDRs,omitempty"`
    // +optional
    APIServerPort int32 `json:"apiServerPort,omitempty"`

    // +optional
    Monitoring NetworkPolicyMonitoringOverride `json:"monitoring,omitempty"`
    // +optional
    ExternalWebhooks NetworkPolicyEgressOverride `json:"externalWebhooks,omitempty"`
    // +optional
    ExternalRegistry NetworkPolicyEgressOverride `json:"externalRegistry,omitempty"`
    // +optional
    IdP NetworkPolicyIdPEgressOverride `json:"idp,omitempty"`
    // +optional
    LLM NetworkPolicyEgressOverride `json:"llm,omitempty"`
    // +optional
    MCPGateway NetworkPolicyEgressOverride `json:"mcpGateway,omitempty"`
    // +optional
    Prometheus NetworkPolicyEgressOverride `json:"prometheus,omitempty"`

    // Helm also exposes a simple ingressNamespaces name-list here.
    // +optional
    Gateway NetworkPolicyNamedIngressOverride `json:"gateway,omitempty"`
    // +optional
    APIFrontend NetworkPolicyNamedIngressOverride `json:"apifrontend,omitempty"`
    // +optional
    Console NetworkPolicyNamedIngressOverride `json:"console,omitempty"`
    // Helm exposes only CIDR/selector overrides here (no ingressNamespaces).
    // +optional
    DataStorage NetworkPolicyIngressOverride `json:"datastorage,omitempty"`
    // +optional
    KubernautAgent NetworkPolicyIngressOverride `json:"kubernautAgent,omitempty"`
}

// NetworkPolicyIngressOverride adds allowed ingress sources beyond the
// operator's default same-namespace/component allow rules. CIDRs cover
// traffic not associated with any pod/namespace (e.g. NodePort-sourced host
// traffic, a hostNetwork-mode ingress controller); selectors cover cases the
// simple namespace-name list (below) cannot express.
type NetworkPolicyIngressOverride struct {
    // +optional
    IngressCIDRs []string `json:"ingressCIDRs,omitempty"`
    // +optional
    IngressNamespaceSelectors []metav1.LabelSelector `json:"ingressNamespaceSelectors,omitempty"`
}

// NetworkPolicyNamedIngressOverride extends NetworkPolicyIngressOverride with
// a namespace-name allowlist, mirroring the subset of components (Gateway,
// APIFrontend, Console) Helm exposes this simpler option on.
type NetworkPolicyNamedIngressOverride struct {
    NetworkPolicyIngressOverride `json:",inline"`
    // +optional
    IngressNamespaces []string `json:"ingressNamespaces,omitempty"`
}

// NetworkPolicyEgressOverride overrides a single egress allow rule's target.
type NetworkPolicyEgressOverride struct {
    // +optional
    // +kubebuilder:default="0.0.0.0/0"
    CIDR string `json:"cidr,omitempty"`
    // +optional
    Port int32 `json:"port,omitempty"`
}

// NetworkPolicyIdPEgressOverride is NetworkPolicyEgressOverride plus a second
// port, for deployments where a service must reach two IdPs on two ports.
type NetworkPolicyIdPEgressOverride struct {
    NetworkPolicyEgressOverride `json:",inline"`
    // +optional
    ExtraPorts []int32 `json:"extraPorts,omitempty"`
}

// NetworkPolicyMonitoringOverride overrides where/how the monitoring-stack
// ingress/egress rules (Prometheus scrape, AlertManager webhook) are shaped.
type NetworkPolicyMonitoringOverride struct {
    // +optional
    Namespace string `json:"namespace,omitempty"`
    // +optional
    // +kubebuilder:default=9090
    PrometheusPort int32 `json:"prometheusPort,omitempty"`
    // +optional
    // +kubebuilder:default=9093
    AlertManagerPort int32 `json:"alertManagerPort,omitempty"`
}
```

Grouped into shared sub-types (`NetworkPolicyIngressOverride`/`NetworkPolicyEgressOverride`/etc.) per Axis 2's shared-type philosophy, rather than 14 bespoke structs -- each sub-type is reused across every component that exposes the same shape in Helm. `metav1.LabelSelector` is the standard `k8s.io/apimachinery` type, matching Helm's underlying `k8sLabelSelector` JSON-schema definition exactly.

**Precedent already in this codebase**: issue #143 (closed) added a reconciler `Warning` event (`NetworkPoliciesDisabled`) when `spec.networkPolicies.enabled: false`, flagging FedRAMP SC-7 non-compliance without blocking it. v1alpha2 formalizes that warning into a hard requirement.

**New migration consideration** (not present in the initial draft): unlike F1/F2/F4-F8, which only add v1alpha2 surface with no v1alpha1 equivalent, this field's removal changes runtime *behavior* for any existing v1alpha1 CR that has `networkPolicies.enabled: false` today (the current default). See the conversion table below for how this is handled -- flagged explicitly for sign-off.

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

### F9 -- Retracted: no change to `Policy`, but a follow-on fix to its parent fields (v1.3 addendum)

`aiAnalysis.policy`/`signalProcessing.policy` stay required, unchanged from v1alpha1, per the corrected Decision Axis 3 above.

**Addendum (found during the D2 live-cluster CRD spike, after initial sign-off)**: "stays required" was correctly implemented at the `AIAnalysisSpec.Policy`/`SignalProcessingSpec.Policy` level (no `omitempty` there, matching v1alpha1), but `KubernautSpec.AIAnalysis`/`KubernautSpec.SignalProcessing` -- the *parent* fields -- were still `omitempty`. Kubernetes' structural-schema validator only evaluates a nested `required` list when the parent object is present in the submitted payload; if the parent key is absent entirely, there's nothing to check nested required-ness against. A minimal CR omitting `spec.aiAnalysis`/`spec.signalProcessing` altogether was admitted with no error when applied directly to a live OCP cluster -- the exact case F9 intended to forbid slipped through one level up.

Helm structurally cannot hit this same loophole: `values.yaml` always carries `aianalysis`/`signalprocessing` keys (with empty-string defaults), so its template-level `fail` guard always evaluates. The CRD's optional-parent-object mechanic has no equivalent "always present" concept, which is what let this diverge.

**Fix**: dropped `omitempty` on `KubernautSpec.AIAnalysis`/`SignalProcessing` too (`api/v1alpha2/kubernaut_types.go`), so both are now top-level required in the generated schema (`config/crd/bases/kubernaut.ai_kubernauts.yaml`'s `spec.required` includes `aiAnalysis`/`signalProcessing`). Re-verified against a live OCP cluster: the old loophole CR is now rejected (`spec.aiAnalysis: Required value`), a corrected CR with both policy refs admits cleanly.

This closes the loophole for *new* v1alpha2 CRs, but by itself would break *existing* v1alpha1 CRs that relied on the resource builders' implicit fallback (`internal/resources/common.go`'s `AIAnalysisPolicyName`/`SignalProcessingPolicyName` default to `"aianalysis-policies"`/`"signalprocessing-policy"` when `ConfigMapName` is empty) once v1alpha2 becomes the storage version -- the conversion webhook would otherwise hand the API server a `v1alpha2.Kubernaut` with an empty required field. Mitigated the same way F6's JWKSURL derivation works: `api/v1alpha1/kubernaut_conversion.go`'s `ConvertTo` now backfills `Policy.ConfigMapName` with the identical default name when empty in the v1alpha1 source, logged via `conversionLog.Info`.

Also considered and rejected as the primary fix: a `+kubebuilder:validation:XValidation` (CEL) rule at the `KubernautSpec` level instead of dropping `omitempty`. CEL would have been the closer structural match to Helm's *own* enforcement mechanism (Helm's `values.schema.json` has zero `required` declarations anywhere for this -- confirmed directly against a fresh clone -- the `fail` guard is a custom runtime check, not a schema constraint), but top-level `required` was chosen instead: simpler, self-documenting in schema-introspection tooling (`kubectl explain`, IDE YAML validation), and consistent with this codebase's existing convention of expressing required-ness via the OpenAPI `required` array rather than CEL for plain presence checks.

**Upstream, filed under the `v1.6` milestone**: [jordigilh/kubernaut#1984](https://github.com/jordigilh/kubernaut/issues/1984) proposes Helm adopt the equivalent fix chart-wide. An audit of `charts/kubernaut/templates/**/*.yaml` found this same template-only-`fail`-guard pattern in 27 places across 13 files -- not just the two policy blocks -- covering Fleet OAuth2 conditional requirements (8 components), MCP Gateway endpoint requirements, Console's OIDC/ingress/secret requirements, and several cross-component enable dependencies. Most of these are expressible via `anyOf`/`if`-`then`-`else` in `values.schema.json` (standard JSON Schema draft-07, which Helm already validates fully-merged values against pre-render); a few depend on live-cluster auto-discovery (TLS issuer, monitoring URLs, `apiServerCIDR`) and are reasonably left as template-level guards since the schema only sees `--set`/`values.yaml` input, not cluster state.

### F10 -- `kubernautAgent.llmProfileRef` becomes optional when `llmProfiles` has exactly one entry (v1.4 addendum)

**Finding**: `KubernautAgentSpec.LLMProfileRef` is the root of the entire `llmProfileRef` fallback chain (`AFLLMProfileRef` falls back to it; severity-triage and AlignmentCheck fall back to AF's own resolved value). In v1alpha1 -- and as scaffolded initially in v1alpha2 -- it is unconditionally required, so even a CR with a single LLM provider must still spell out `kubernautAgent.llmProfileRef: <name>`, one more required field than the "minimize required fields" driver (Decision Driver 2) calls for.

Helm has a related but weaker mechanism: `kubernautAgent.llmProfileRef` defaults to the fixed string `"primary"` (`values.schema.json`, DD-PLATFORM-006 DA4) -- "an undefined profile still fails the render" if the user's sole profile isn't named `primary`. Rejected as the model to mirror: a fixed-name convention requires the user to already know it exists, whereas count-based inference ("there's only one, so it's unambiguous") requires no convention at all and degrades gracefully to an explicit-required error the moment there's real ambiguity (2+ profiles).

**Decision**: `KubernautAgentSpec.LLMProfileRef` becomes `+optional` in v1alpha2 (dropped from `kubernautAgent`'s `required` list; `MinLength=1` kept as a guard against an explicitly-empty string). No CRD-level `+kubebuilder:default` is used, since the value is computed from `spec.llmProfiles`'s cardinality, not a static value defaulting can express. Resolution is implemented in Go: `internal/resources/common.go`'s new `EffectiveKALLMProfileRef(kn)` returns the explicit ref when set, else the sole key in `spec.llmProfiles` when there is exactly one, else `""`. `AFLLMProfileRef`, `KubernautAgentDeployment`, and the KA `NetworkPolicy` egress builder were switched from reading `spec.kubernautAgent.llmProfileRef` directly to calling this function, so every downstream consumer in the fallback chain inherits the inference without repeating the cardinality check. `validateLLMProfileRefs` (`internal/resources/validation.go`) now validates the *effective* ref instead of the raw field, and distinguishes the "zero profiles" and "2+ profiles, ambiguous" cases with separate error messages.

Because this is Go-level resolution rather than CRD-level defaulting, it applies uniformly regardless of which API version produced the object the (not-yet-migrated) v1alpha1-based controller reads -- a v1alpha2-authored CR relying on the inference converts to v1alpha1 with an empty `llmProfileRef` (reads don't re-validate against the target version's `required`/`MinLength` constraints), and the resolver handles that empty value identically to an object that was always v1alpha1-shaped.

**Upstream, filed under the `v1.6` milestone**: [jordigilh/kubernaut#1987](https://github.com/jordigilh/kubernaut/issues/1987) proposes Helm replace its `"primary"`-name default with the same count-based inference, since Helm templates can inspect `len(.Values.global.llmProfiles)` directly without needing a Go struct.

### F11 -- AIAnalysis policy ConfigMap default renamed `aianalysis-policies` -> `aianalysis-policy` (v1.5 addendum)

**Finding**: The fallback default name `internal/resources/common.go`'s `AIAnalysisPolicyName` used when `aiAnalysis.policy.configMapName` is empty was `"aianalysis-policies"` (plural), while the equivalent `SignalProcessingPolicyName` fallback was `"signalprocessing-policy"` (singular) -- an inconsistent pair with no underlying reason: both ConfigMaps hold exactly one Rego file (`approval.rego`), never a collection, so neither name should be plural. Noticed while re-rendering the minimal-CR example (D2 spike) and confirmed this same inconsistency exists upstream too -- Helm's `values.schema.json`/`aianalysis.yaml` template defaults to the identical plural `aianalysis-policies` name alongside its own singular `signalprocessing-policy`.

**Decision**: renamed the operator's default to singular `aianalysis-policy`, matching `signalprocessing-policy`'s convention. This is a Go-level constant/fallback-string change, not a CRD schema change -- `configMapName` itself has always been a plain user-supplied string with no `+kubebuilder:default`, so `make manifests` produces no CRD diff. Updated in lockstep: `internal/resources/common.go`'s `AIAnalysisPolicyName` fallback, `api/v1alpha1/kubernaut_conversion.go`'s `defaultAIAnalysisPolicyConfigMapName` (the F9 backfill constant, which must mirror the resource builder's fallback exactly per its own doc comment), both sample CRs, installation docs, and all test literals/fixtures referencing the old name.

**Compatibility note**: this fallback is only reachable when a CR omits `configMapName` entirely while still providing the (now-required, per F9) `policy` block with a valid ConfigMap that happens to be named after the old convention -- i.e. an existing v1alpha1 CR whose author relied on the implicit default rather than setting `configMapName` explicitly, and who separately created a ConfigMap literally named `aianalysis-policies`. For that specific (expected to be rare) case, the operator will look for `aianalysis-policy` after this change and fail to find the ConfigMap until the user either renames it or sets `configMapName: aianalysis-policies` explicitly. Flagged in the D5 migration guide as a one-line heads-up; not treated as a blocking risk given the narrow reachability condition and that this repo has not yet reached a stable release where such reliance would be established.

**Upstream, filed under the `v1.6` milestone**: [jordigilh/kubernaut#1989](https://github.com/jordigilh/kubernaut/issues/1989) proposes Helm rename its identical `aianalysis-policies` default to `aianalysis-policy` for the same consistency reason.

### F12 -- `FleetSpec.OAuth2` becomes admission-mandatory when `mcpGatewayEndpoint` is set (v1.6 addendum)

**Finding**: `v1alpha2.FleetSpec.OAuth2`'s doc comment read "Optional -- some MCP Gateway deployments do not require authentication," but this was never true for this codebase's own fleet-aware components: upstream `Fleet.ValidateFullFederation` (called by Gateway, RemediationOrchestrator, SignalProcessing, APIFrontend, EffectivenessMonitor, and KubernautAgent at startup) has no unauthenticated mode and crash-loops without a configured OAuth2 client. FleetMetadataCache is the sole exception -- `validateFleetMetadataCache` (`internal/resources/validation.go`) already enforces this unconditionally. Discovered while reconciling three previously-scattered Fleet sources for the Fleet v1alpha2 migration: this ADR/scaffold branch, and Fleet's actual implementation already shipped to `main` in `v1alpha1` under seven now-closed v1.6 issues (#180/#200/#201/#204/#222/#224/#184) that predated this ADR's existence. Directly analogous to the upstream Helm chart gap just fixed in [kubernaut#1991](https://github.com/jordigilh/kubernaut/issues/1991)/[#1992](https://github.com/jordigilh/kubernaut/pull/1992) (`global.fleet.oauth2.enabled` defaulted `false` for 7 of 8 fleet-capable services).

**Decision**: added a `FleetSpec`-level `+kubebuilder:validation:XValidation` CEL rule requiring `oauth2.enabled=true` whenever `enabled=true` and `mcpGatewayEndpoint` is non-empty, mirroring `AnsibleSpec`'s existing conditional-requirement pattern (`!has(self.enabled) || !self.enabled || ...`) so `FleetSpec`'s documented "every field inert while `enabled=false`" pre-staging contract is preserved -- a CR with `fleet.enabled: false` and `mcpGatewayEndpoint` pre-populated for later use is still admitted. The rule is null-safe at every nesting level (`has(self.oauth2) && has(self.oauth2.enabled)`) per the same `omitempty`-round-trip hazard documented for `AnsibleSpec` in D2. Verified directly against a live envtest apiserver (`internal/controller/kubernaut_v1alpha2_admission_test.go`): rejects `oauth2` omitted or `enabled: false`, accepts `oauth2.enabled: true`, and accepts `enabled: false` with `oauth2` unset (pre-staging case) or `fleet` omitted entirely.

**Scope note (important for the Fleet v1alpha2 migration plan)**: this CEL rule lives only on `v1alpha2.FleetSpec`'s schema, so it only protects **direct v1alpha2 writes**. A `v1alpha1` write with the same unsafe combination (`fleet.enabled: true`, `mcpGatewayEndpoint` set, `oauth2.enabled: false`) is *not* rejected by this rule -- CEL validation runs against the schema of the version a request targets, not the storage version it gets converted to, and v1alpha1 has no equivalent rule (confirmed by precedent: the existing `networkPolicies.enabled: false` v1alpha1 write is admitted today despite v1alpha2 treating that field as removed/always-on). This gap closes automatically once Fleet's fields are removed from `v1alpha1` (Fleet v1alpha2 migration PR 2) -- at that point every write to Fleet-related fields necessarily targets `v1alpha2` directly. Until then, PR 2 also adds a defense-in-depth fix to `internal/resources/validation.go`'s `validateFleetOAuth2` (which today only validates OAuth2's own sub-fields *if* `OAuth2.Enabled` is already true, never flagging `Enabled=false` itself as an error) so the runtime reconciliation path independently catches what v1alpha1 admission cannot.

**Upstream**: no new issue filed -- [jordigilh/kubernaut#1991](https://github.com/jordigilh/kubernaut/issues/1991)/[#1992](https://github.com/jordigilh/kubernaut/pull/1992) already cover the equivalent Helm chart fix, merged upstream prior to this finding.

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
| `aiAnalysis.policy` / `signalProcessing.policy` (required) | `aiAnalysis.policy` / `signalProcessing.policy` (required, unchanged) | Direct value copy; no behavior change either direction (F9 retracted -- see Decision Axis 3). |
| `networkPolicies.enabled` (bool, default `false`) | *(field removed -- always on)* | **Lossy / behavior change.** `ConvertFrom` (v1alpha1->v1alpha2) drops the field entirely; NetworkPolicies are created regardless of its prior value. If the source value was explicitly `false`, the conversion webhook emits a `Warning` response (same pattern as the AlignmentCheck LLM case) so `kubectl apply`/`get` surfaces that this CR will now have NetworkPolicies created where it previously did not. `ConvertTo` (v1alpha2->v1alpha1) sets `enabled: true` for round-trip fidelity, reflecting actual runtime behavior. Documented explicitly in the D5 migration guide as a heads-up (not a manual step -- there is nothing to configure, only to be aware of). |
| `kubernautAgent.llmProfileRef` (required) | `kubernautAgent.llmProfileRef` (optional, F10) | Direct value copy either direction -- no structural change to the field itself, only its required-ness and Go-level resolution (`EffectiveKALLMProfileRef`) change. A v1alpha1 CR always has this set (v1alpha1's own schema still requires it), so `ConvertTo` never sees an empty value to backfill. A v1alpha2 CR that omits it (relying on single-profile inference) converts to v1alpha1 with an empty string via `ConvertFrom`; not lossy in the sense of losing information (the profile is still unambiguously determinable from `llmProfiles`), but the resulting v1alpha1-shaped object would fail v1alpha1's own admission-time validation if resubmitted as-is -- reads don't re-validate, so this only matters if something re-submits the converted object verbatim as a v1alpha1 write, which no code path in this repo does today. |
| `fleet.oauth2.*` (no CEL requirement) | `fleet.oauth2.*` (F12: CEL-required when `enabled`+`mcpGatewayEndpoint` set) | Direct value copy, no structural change -- only v1alpha2 gains admission-time enforcement. **Scope-limited, see F12 above**: this CEL rule only protects direct v1alpha2 writes; a v1alpha1 write with `oauth2.enabled: false` in the same unsafe combination still converts through unrejected today (closed once Fleet is removed from v1alpha1 entirely in the Fleet v1alpha2 migration's PR 2, which also adds a runtime `validateFleetOAuth2` fix as an interim defense-in-depth measure). |

**General principle**: every conversion is a direct value copy except the two flagged rows (AlignmentCheck LLM shape, and the theoretical "explicit old default" edge case for rate limits), both of which are pre-existing bugs or genuinely unrepresentable states in v1alpha1, not information the conversion function is responsible for inventing. Both lossy cases are called out explicitly in the D5 migration guide with a manual remediation step.

---

## Wiring Manifest (D2 scaffold target -- finalized at D2 kickoff)

| Component | Production Entry Point | Wiring Code Location | IT Test ID |
|---|---|---|---|
| `v1alpha2.Kubernaut` (hub) | `KubernautReconciler.Reconcile()` | `internal/controller/kubernaut_controller.go` | IT-CRD-V2-001 |
| Conversion webhook (v1alpha1 <-> v1alpha2) | `cmd/main.go` (`ctrl.NewWebhookManagedBy`) | `api/v1alpha1/kubernaut_conversion.go` (new) | IT-CRD-V2-002 |
| `FleetOverrideSpec` resolution | `internal/resources/*.go` (per-component builders) | new `resolveFleetOverride(kn, component)` helper in `internal/resources/common.go` | re-points existing Fleet IT coverage |
| `MonitoringSpec`/`resolvePrometheusURL` | `internal/resources/configmaps.go` (EM, AF severity-triage) | `internal/resources/common.go` | re-points existing monitoring IT coverage |
| Unconditional `NetworkPoliciesSpec` (opt-out removed) | `internal/controller/kubernaut_controller.go` (all component reconcile paths) | `internal/resources/networkpolicies.go` (drop the `Enabled` gate) | re-points existing NetworkPolicy IT coverage; new IT asserts policies exist with no CR-level toggle |

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Conversion webhook is new, untested infrastructure for this repo | Singleton constraint bounds testing surface to one object; IT-CRD-V2-002 exercises both `ConvertTo`/`ConvertFrom` directions explicitly with the lossy-field cases from the migration table above |
| D2-D4 is a multi-week effort touching nearly every resource builder | Phased roadmap (D2 scaffold -> D3 migrate builders component-by-component -> D4 implement #235/#227/#277 -> D5 deprecate) keeps the pyramid invariant (UT+IT per component group) intact throughout rather than one big-bang PR |
| Removing `networkPolicies.enabled` changes runtime behavior for any existing v1alpha1 CR with `enabled: false` (today's default) -- NetworkPolicies will start being created where they weren't before | This is an intentional, security-positive change (matches Helm's always-on behavior and the Red Hat 2027-02 mandate), not an accidental regression. Mitigated by: (1) a non-silent `Warning` on conversion (same pattern as the AlignmentCheck LLM case), (2) explicit migration-guide callout, (3) precedent already established in-repo via issue #143's `NetworkPoliciesDisabled` warning event, so affected users have already been surfaced a signal today. Residual risk: a cluster whose CNI doesn't support `NetworkPolicy`, or that has conflicting third-party policies, could see new behavior on upgrade -- flagged for explicit sign-off below rather than silently deferred |
| Lossy AlignmentCheck LLM conversion could silently drop config on downgrade | Conversion webhook returns a `Warning` (not silent) on `ConvertFrom` when the legacy shape had non-empty `provider`/`model`; migration guide (D5) documents the manual `llmProfileRef` step explicitly |

---

## Confidence Assessment

**Confidence: 93%**

**Justification**: The conversion strategy (Axis 1) is a standard, well-documented kubebuilder pattern significantly de-risked by the CRD's strict singleton constraint (92% confidence on its own). The structural alignment approach (Axis 2) generalizes a pattern (`ShutdownSpec`, `LoggingSpec` sharing) already proven in this exact codebase (90% confidence on its own). Axis 3 now requires *zero* new mechanism -- `policy`/`proactiveSignalMappings` stay required, matching Helm's verified behavior exactly (95% confidence on its own, up from 85% when it depended on unreviewed default policy content). F3's NetworkPolicies decision strengthened from "expand a bool" to "remove the opt-out, matching Helm's unconditional behavior and the Red Hat 2027-02 mandate," and its full field list is now frozen and enumerated above (Helm's `networkPolicies.*` schema confirmed stable, removing the "drift before D2/D3" risk from v1.1 of this ADR) -- offset only by the explicitly-flagged behavior-change risk for existing CRs with `enabled: false` (today's default). F10's single-profile inference (v1.4) is fully implemented and tested (unit tests for both the ambiguous-2+-profiles rejection and the sole-profile inference, at both the validation and resource-builder level) rather than deferred, a small net positive to the overall assessment. The combined 93% now reflects one remaining risk structural to the *size* of this initiative rather than to any single decision: the multi-week D2-D5 execution surface (every resource builder, every validation function) has more opportunity for an individual builder migration to surface an unanticipated edge case than a single-PR change would, even though the phased roadmap is designed to catch those incrementally rather than in one big-bang cutover.

**What would raise this to 95%+**: A completed D2 scaffold with the conversion webhook's IT-CRD-V2-002 passing end-to-end (validates Axis 1 empirically, not just on paper) before D3 migration begins in earnest.

---

## Sign-off Requested

Per `AGENTS.md` CHECKPOINT DD, this architectural change requires explicit user approval before D2 (scaffold) begins. Specifically requesting a decision on:

1. **Axis 1** (conversion webhook, v1alpha2 hub / v1alpha1 spoke) -- proceed, or reconsider Alternative B/C?
2. **Axis 2** (shared `FleetOverrideSpec`/`MonitoringSpec` types, deep structural mirror) -- proceed, or scope down to Alternative B (targeted fixes only)?
3. **Axis 3** (keep `policy`/`proactiveSignalMappings` required, matching Helm exactly; no bundled defaults -- F9 retracted) -- proceed?
4. **F3, revised and frozen** (remove `NetworkPoliciesSpec.Enabled` entirely -- NetworkPolicies become unconditional, matching Helm's actual behavior and the Red Hat 2027-02 mandate; full field list now enumerated above as `NetworkPoliciesSpec` + 5 shared sub-types) -- proceed, including the explicitly-flagged behavior change for existing v1alpha1 CRs with `enabled: false` (today's default)?
5. Any of F1-F8 you'd like pulled out of v1alpha2 scope entirely (deferred to a future v1alpha3), given the size of this initiative?

**Status: all 5 points above approved** (Axis 1, Axis 2, Axis 3, F3, and no scope trim -- confirmed in review). D2 scaffolding may begin.

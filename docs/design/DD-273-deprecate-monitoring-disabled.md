# DD-273: Deprecate `spec.monitoring.enabled: false`

**Status**: Accepted
**Decision Date**: 2026-08-02
**Applies To**: `api/v1alpha1.MonitoringSpec`, `config/crd/bases/kubernaut.ai_kubernauts.yaml`
**Related Issues**:
- jordigilh/kubernaut-operator#273 (this decision)
- jordigilh/kubernaut-operator#271 (the NetworkPolicy bug that surfaced the underlying problem, backported to `release/v1.5` independently of this decision)
- [jordigilh/kubernaut#1839](https://github.com/jordigilh/kubernaut/issues/1839) / [PR#1841](https://github.com/jordigilh/kubernaut/pull/1841) (upstream removal of AF's ungrounded LLM severity fallback)

## Context

`spec.monitoring.enabled` (default `true`) toggles OCP cluster-monitoring
integration across the operator. Setting it to `false` disables, across six
files and roughly fifteen call sites: API Frontend's severity-triage
Prometheus/AlertManager lookups, Gateway's `AlertmanagerConfig` and token
Secret, EffectivenessMonitor's external Prometheus/AlertManager config,
Kubernaut Agent's Prometheus/AlertManager tool integration,
`alertmanager-view` RBAC, several NetworkPolicy ingress/egress rules, and
ServiceMonitor/PrometheusRule deployment.

Investigating #271 (AF's NetworkPolicy egress bug) surfaced a deeper
problem: upstream kubernaut#1839/#1841 removed AF's "Tier 3: Pure LLM
Fallback" severity-inference path, which had been silently absorbing
Prometheus query failures (including the ones caused by monitoring being
disabled) and returning a plausible-looking severity anyway. With that
fallback gone, `resolveCreateRRSeverity` now fails closed when Prometheus is
unreachable — which is always true when `monitoring.enabled=false` — so
every `RemediationRequest` creation gated on severity triage fails outright.

This operator is OCP-specific throughout: the NetworkPolicy, RBAC, and
ConfigMap generation all hardcode the `openshift-monitoring` namespace and
the in-cluster Thanos Querier URL. There is no non-OCP monitoring backend
this toggle could ever select between, and OCP ships cluster-monitoring by
default. `monitoring.enabled=false` therefore does not represent a
deployment topology worth continuing to support.

## Decision

Reject `spec.monitoring.enabled: false` at the CRD level via an
`x-kubernetes-validations` (CEL) rule on `MonitoringSpec`, enforced on both
Create and Update:

```go
// +kubebuilder:validation:XValidation:rule="!has(self.enabled) || self.enabled == true",message="..."
type MonitoringSpec struct {
    Enabled *bool `json:"enabled,omitempty"`
}
```

This targets `main`/v1.6 only. It is **not** backported to `release/v1.5`,
because it is a CRD-schema-narrowing change (any existing 1.5 CR with
`enabled: false` would fail admission), which is a compatibility break we
only accept at a CRD-version boundary already carrying other breaking
changes (see `docs/upgrade-1.5-to-1.6.md`) — consistent with this repo's
existing precedent for the `spec.kubernautAgent.llm` → `spec.llmProfiles`
migration in the same release. The independent NetworkPolicy fix (#271) was
backported on its own, since it doesn't change the CRD schema.

Enforcing on both Create *and* Update (rather than Create-only) matches
that same precedent: 1.6 is expected to be a fresh-install/re-apply
boundary rather than an in-place upgrade for existing CRs, so there is no
need to preserve a path for already-running clusters to keep the old value.
[Kubernetes CRD Validation Ratcheting](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation-ratcheting)
(GA since 1.33, default-on Beta since 1.30 — covered by this operator's
stated OCP 4.17+ prerequisite) additionally ensures that even an
already-invalid stored CR is not deadlocked on unrelated updates (e.g. the
reconciler's own finalizer add/remove), since only fields *changed* by a
given write are re-validated.

## Alternatives Considered

1. **Force `MonitoringEnabled()` to always return `true`, ignoring the
   field's value.** Rejected: this touches all ~15 call sites across
   `configmaps.go`, `deployments.go`, `networkpolicies.go`, `ocp.go`,
   `rbac.go`, and `kubernaut_controller.go`, each of which would need its
   "disabled" branch re-verified as dead code or removed outright — a much
   larger, riskier change than a single CRD constraint, for the same
   outcome. Left as a possible follow-up cleanup, not part of this change.
2. **Go-level validating webhook (`internal/webhook`) instead of CEL.**
   Rejected: this repo already has an established CEL (`XValidation`)
   precedent for exactly this shape of constraint (`AnsibleSpec`'s
   `enabled`/`apiURL` co-validation), needs no new wiring, webhook
   registration, or RBAC, and is enforced natively by the API server.
3. **Create-only enforcement.** Considered as the lower-risk option (avoids
   any dependency on CRD Validation Ratcheting semantics), but rejected in
   favor of Create+Update to match the existing 1.6 precedent of failing
   admission on stale shapes until migrated, and because this operator's
   upgrade path for 1.6 assumes a fresh install rather than an in-place
   CR carryover.

## Consequences

- `internal/resources/*_test.go` unit tests that construct
  `MonitoringSpec{Enabled: &disabled}` directly continue to work unchanged
  — they exercise Go business logic without going through API server
  admission, so the CEL rule doesn't apply to them.
- Two `internal/controller` integration tests that toggled
  `monitoring.enabled` on an already-admitted CR via `k8sClient.Update` were
  removed, since that transition is no longer reachable through the API
  (see the `NOTE (#273)` comments left in `kubernaut_lifecycle_test.go`).
- `pruneStaleMonitoringRBAC` and the other "disabled" branches remain in
  place defensively but are not expected to be reachable via any CRD-admitted
  CR going forward.

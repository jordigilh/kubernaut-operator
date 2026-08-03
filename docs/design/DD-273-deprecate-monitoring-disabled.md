# DD-273: Remove `spec.monitoring` and always reconcile OCP monitoring integration

**Status**: Accepted
**Decision Date**: 2026-08-02
**Applies To**: `api/v1alpha1.KubernautSpec`, `config/crd/bases/kubernaut.ai_kubernauts.yaml`
**Related Issues**:
- jordigilh/kubernaut-operator#273 (this decision)
- jordigilh/kubernaut-operator#271 (the NetworkPolicy bug that surfaced the underlying problem, backported to `release/v1.5` independently of this decision)
- [jordigilh/kubernaut#1839](https://github.com/jordigilh/kubernaut/issues/1839) / [PR#1841](https://github.com/jordigilh/kubernaut/pull/1841) (upstream removal of AF's ungrounded LLM severity fallback)

## Context

`spec.monitoring.enabled` (default `true`) toggled OCP cluster-monitoring
integration across the operator. Setting it to `false` disabled, across six
files and roughly fifteen call sites: API Frontend's severity-triage
Prometheus/AlertManager lookups, Gateway's `AlertmanagerConfig` and token
Secret, EffectivenessMonitor's external Prometheus/AlertManager config,
Kubernaut Agent's Prometheus/AlertManager tool integration,
`alertmanager-view`/`gateway-signal-source` RBAC, several NetworkPolicy
ingress/egress rules, and ServiceMonitor/PrometheusRule deployment.

Investigating #271 (AF's NetworkPolicy egress bug) surfaced a deeper
problem: upstream kubernaut#1839/#1841 removed AF's "Tier 3: Pure LLM
Fallback" severity-inference path, which had been silently absorbing
Prometheus query failures (including the ones caused by monitoring being
disabled) and returning a plausible-looking severity anyway. With that
fallback gone, `resolveCreateRRSeverity` now fails closed when Prometheus is
unreachable — which was always true when `monitoring.enabled=false` — so
every `RemediationRequest` creation gated on severity triage failed outright.

This operator is OCP-specific throughout: the NetworkPolicy, RBAC, and
ConfigMap generation all hardcode the `openshift-monitoring` namespace and
the in-cluster Thanos Querier URL. There is no non-OCP monitoring backend
this toggle could ever select between, and OCP ships cluster-monitoring by
default. `monitoring.enabled=false` therefore never represented a deployment
topology worth continuing to support.

An earlier iteration of this decision kept the field but rejected `false` at
admission via a CEL `XValidation` rule. That was rejected on review: keeping
a field that can only ever hold one value (`true`) is misleading API surface
— it implies a toggle exists when it doesn't — and it left all ~15
"disabled" code branches in place as unreachable dead code. Since 1.6 is
already making other CRD-breaking changes and this operator's monitoring
integration is genuinely mandatory, the field itself should go, and every
behavior it used to gate on `true` should become unconditional.

## Decision

Remove `spec.monitoring` (and the now-single-field `MonitoringSpec` type)
from `KubernautSpec` entirely as of `main`/v1.6. OCP monitoring integration
is provisioned unconditionally on every reconcile:

- `internal/resources/networkpolicies.go`: monitoring-namespace ingress
  (metrics scraping) and egress (Thanos Querier :9091, AlertManager :9094)
  rules are always included, for every component that previously gated them.
- `internal/resources/rbac.go`: `alertmanager-view` ClusterRole and its
  CRBs (EffectivenessMonitor, Kubernaut Agent, API Frontend
  `cluster-monitoring-view` bindings) are always included.
- `internal/resources/deployments.go`: service-CA volumes/mounts and the
  `wait-for-service-ca`/`build-ca-bundle` init containers (EffectivenessMonitor,
  Kubernaut Agent, API Frontend) are always wired.
- `internal/resources/configmaps.go`: EffectivenessMonitor's `external`
  Prometheus/AlertManager block, Kubernaut Agent's `tools.prometheus`/
  `tools.alertmanager` block, and API Frontend's `severityTriage` block
  (`enabled: true`, pointed at Thanos Querier) are always rendered.
- `internal/resources/ocp.go`: `GatewayAlertManagerConfig` and
  `GatewayAlertManagerTokenSecret` no longer have a monitoring-gated early
  return (they remain gated on `spec.gateway.enabled` at their call site,
  unrelated to monitoring).
- `internal/controller/kubernaut_controller.go`: `pruneStaleMonitoringRBAC`
  and its call in `deployToggleRBAC` are deleted outright — there is no
  longer a transition to clean up after, since the field can't be toggled.

This targets `main`/v1.6 only. It is **not** backported to `release/v1.5`,
because it is a CRD-schema-breaking removal (any existing 1.5 CR or manifest
with `spec.monitoring` would simply have that block silently pruned by 1.6's
structural schema — harmless, since the behavior it gated now always runs,
but still a breaking API shape change), which is a compatibility break we
only accept at a CRD-version boundary already carrying other breaking
changes (see `docs/upgrade-1.5-to-1.6.md`) — consistent with this repo's
existing precedent for the `spec.kubernautAgent.llm` → `spec.llmProfiles`
migration in the same release. The independent NetworkPolicy fix (#271) was
backported on its own, since it doesn't change the CRD schema.

## Alternatives Considered

1. **Reject `spec.monitoring.enabled: false` via CEL `XValidation`, keeping
   the field.** This was the initially-accepted approach, superseded by
   this decision. Rejected on review: a field that can only ever be `true`
   is misleading, and it left all ~15 "disabled" branches in place as
   unreachable dead code rather than actually removing them.
2. **Keep `spec.monitoring` as an empty reserved struct (`MonitoringSpec{}`)
   for future fields.** Rejected: an empty `{}` struct on the CRD schema
   serves no purpose today: YAGNI. If a genuine monitoring-related
   configuration need arises in a future release, it can be added then with
   real fields backing real behavior.
3. **Go-level validating webhook (`internal/webhook`) instead of removing
   the field.** Rejected for the same reason as (1) — the field shouldn't
   exist at all, so there's nothing left to validate.
4. **Create-only enforcement (reject `false` on Create, tolerate on
   Update).** Same rejection as (1); moot once the field is removed rather
   than restricted.

## Consequences

- `internal/resources/*_test.go` unit tests that constructed
  `kn.Spec.Monitoring.Enabled = &disabled` no longer compile (the field is
  gone) and were removed, since the behavior they exercised (a "disabled"
  code path) no longer exists. The corresponding "enabled" tests remain and
  now describe the operator's only behavior, unconditionally.
- Two `internal/controller` integration tests that toggled
  `monitoring.enabled` on an already-admitted CR via `k8sClient.Update` were
  removed in an earlier iteration of this change (see the `NOTE (#273)`
  comments in `kubernaut_lifecycle_test.go`); that removal still applies
  now that the field is deleted outright rather than merely restricted.
- New integration coverage (`MON-001`/`MON-002` in
  `kubernaut_controller_test.go`) proves two things end-to-end through
  envtest: a legacy 1.5-style manifest carrying `spec.monitoring.enabled:
  false` is accepted (the field is pruned, not rejected) rather than
  breaking existing automation outright, and monitoring RBAC/NetworkPolicy/
  severityTriage config are always provisioned regardless.
- `pruneStaleMonitoringRBAC` and its caller in `deployToggleRBAC` were
  deleted rather than left in place, since there is no longer any toggle
  transition for them to clean up after.

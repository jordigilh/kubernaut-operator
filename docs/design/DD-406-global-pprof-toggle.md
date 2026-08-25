# DD-406: Simplify `debug.pprofEnabled` to a single global toggle instead of per-component `DebugSpec`

**Status**: Accepted
**Decision Date**: 2026-08-25
**Applies To**: `api/v1alpha2.KubernautSpec`, `api/v1alpha2.DebugSpec` (embedding location only), `internal/resources/configmaps.go`, `internal/resources/fleetmetadatacache.go`, `internal/resources/deployments.go`
**Related Issues**:
- jordigilh/kubernaut-operator#406 (this decision)
- jordigilh/kubernaut-operator#403 / #404 (merged in #405) -- introduced the per-component `DebugSpec{PprofEnabled bool}` embedding this decision replaces

## Context

#403/#404 (merged in #405) added `debug.pprofEnabled` by embedding a
`DebugSpec{PprofEnabled bool}` struct in each of the 12 component specs
(`FleetMetadataCacheSpec`, `NotificationSpec`, `AIAnalysisSpec`,
`SignalProcessingSpec`, `RemediationOrchestratorSpec`,
`WorkflowExecutionSpec`, `EffectivenessMonitorSpec`, `KubernautAgentSpec`,
`GatewaySpec`, `AuthWebhookSpec`, `APIFrontendSpec`, `DataStorageSpec`),
mirroring upstream kubernaut's per-service config schema (each service reads
its own `debug.pprofEnabled` from its own config file).

In practice, every real usage of this toggle so far has been all-or-nothing:
enabling pprof on KA/AF for a specific upstream troubleshooting request, and
enabling pprof on all 12 services for QE to validate an RC. Nobody has asked
for per-service granularity. Requiring the same boolean to be set in 12
places in the CR to get the common "just turn profiling on everywhere"
behavior is configuration hassle with no compensating benefit observed in
practice.

## Decision

Replace the 12 embedded `Debug DebugSpec` fields with a single top-level
`spec.debug.pprofEnabled` field on `v1alpha2.KubernautSpec`, applied
uniformly to all 12 services' rendered configs and to the pprof container
port on the 7 controller-runtime-managed deployments that expose one
(AIAnalysis, SignalProcessing, RemediationOrchestrator, WorkflowExecution,
EffectivenessMonitor, Notification, AuthWebhook).

```go
type KubernautSpec struct {
	// ... existing fields ...

	// Debug configures short-lived diagnostic toggles applied uniformly to
	// all 12 services (DD-406) -- a single cluster-wide switch, not a
	// per-component one, since every observed real-world usage has been
	// all-or-nothing (enable pprof everywhere for an RC validation pass,
	// or on a specific pair of services for a troubleshooting request that
	// still only required setting one field, not twelve).
	// +optional
	Debug DebugSpec `json:"debug,omitempty"`
}
```

`DebugSpec` itself is unchanged -- only its embedding location moves from
12 component specs to `KubernautSpec` root.

## Alternatives Considered

1. **Keep per-component `DebugSpec` for future granularity.** Rejected: no
   real deployment has ever needed per-service pprof isolation; both
   observed usages (KA/AF troubleshooting, all-12 RC validation) are
   satisfied equally well, or better, by a single toggle. Speculative
   granularity that has never been requested is exactly the kind of
   configuration surface AGENTS.md's Go Anti-Pattern guidance and CM-6
   (least-functionality) argue against carrying indefinitely.
2. **Deprecate the per-component fields in place (mark unused, ignore at
   runtime) instead of removing them.** Rejected: `v1alpha2` has not
   reached a stable release yet (pre-GA, `rc` channel only), so there is
   zero migration cost to removing the fields outright -- a deprecate-in-
   place step is only warranted for an already-released API, per the same
   reasoning as `DD-362`.
3. **Add the global field alongside the 12 per-component ones, with the
   global field taking precedence when set.** Rejected: this doubles the
   schema surface and introduces a precedence-resolution rule with no
   observed use case to justify it -- strictly worse than either keeping
   the original per-component design or replacing it outright.

## Consequences

- **No breaking change**: `v1alpha2` is unreleased at stable GA (only
  `rc` tags exist); no production CR instances depend on the removed
  per-component fields outside test fixtures and the RC channel.
- `internal/resources/configmaps.go` (11 call sites), `fleetmetadatacache.go`
  (1 call site), and `deployments.go` (7 `pprofContainerPort` call sites) all
  switch from `knV2.Spec.<Component>.Debug.PprofEnabled` to
  `knV2.Spec.Debug.PprofEnabled`.
- `internal/resources/configmaps_test.go` and `deployments_test.go`'s
  `DescribeTable` suites for `#403` are reworked to prove the *single* flag
  drives all 12 ConfigMaps / 7 deployments together, replacing the previous
  per-component isolation assertions (which no longer apply -- there is no
  such thing as enabling pprof on just one service anymore).
- `make manifests generate` collapses 12 per-component `debug:` sub-schema
  blocks in `config/crd/bases/kubernaut.ai_kubernauts.yaml` into a single
  root-level `debug:` block.
- **Compliance framing, stated honestly**: the Go zero-value secure-by-
  default behavior (**AC-6**, least privilege / no unintended diagnostic
  exposure) is preserved unchanged by this decision -- collapsing 12 fields
  into 1 does not alter how that control is enforced (profiling still
  defaults to off cluster-wide). The complexity reduction itself is a
  **CM-6** (configuration management / least-functionality) side-benefit:
  one boolean an administrator can misconfigure instead of twelve. This is
  recorded as a side-benefit, not claimed as the change's driving control
  objective.

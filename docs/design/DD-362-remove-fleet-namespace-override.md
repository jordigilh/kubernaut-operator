# DD-362: Remove `FleetOverrideSpec.Namespace` -- all fleet-aware components use the shared `spec.fleet.mcpGatewayNamespace` directly

**Status**: Accepted
**Decision Date**: 2026-08-16
**Applies To**: `api/v1alpha2.FleetOverrideSpec`, `internal/resources/rbac.go`, `internal/resources/fleetmetadatacache.go`, `internal/resources/configmaps.go`, `internal/resources/common.go`, `api/v1alpha1/kubernaut_conversion.go`
**Related Issues**:
- jordigilh/kubernaut-operator#362 (this decision)
- jordigilh/kubernaut-operator#235 (WorkflowExecution's fleet credential type -- surfaced this question by deliberately omitting `Namespace`)
- jordigilh/kubernaut-operator#227 (extended per-component `Namespace` consumption to AF/EM; the RBAC helpers this decision removes)
- jordigilh/kubernaut-operator#354 (namespace-scoped RBAC pruning on override change; the pruning scenario this decision invalidates)
- `docs/design/ADR-CRD-001-v1alpha2-redesign.md` F1 (introduced `FleetOverrideSpec{OAuth2CredentialsSecretRef, Namespace}`)

## Context

`FleetOverrideSpec.Namespace` lets each of the 7 fleet-aware components
(Gateway, RemediationOrchestrator, SignalProcessing, APIFrontend,
EffectivenessMonitor, KubernautAgent, FleetMetadataCache) override
`spec.fleet.mcpGatewayNamespace` for its own namespace-scoped `Role`
watching the `Backend`/`MCPServerRegistration` CRDs
(`internal/resources/rbac.go`'s `MCPGatewayNamespaceRBAC`).

While designing #235's `WorkflowExecutionFleetSpec` (a deliberately
`Namespace`-less type, since WE never watches those CRDs), the underlying
question was raised: does *any* component genuinely need a namespace
divergent from the shared `spec.fleet.mcpGatewayNamespace`? Investigation
found:

- **Consumption is not even uniform today.** Only 4 of 7 components
  (`FleetMetadataCache`, `SignalProcessing`, `APIFrontend`,
  `EffectivenessMonitor`) actually read `FleetOverrideSpec.Namespace`, via
  `effective*MCPGatewayNamespace` helpers in `internal/resources/rbac.go`
  feeding `MCPGatewayNamespaceRBAC`'s grants table and each component's
  ConfigMap `fleet.namespace` render. The other 3 (`Gateway`,
  `RemediationOrchestrator`, `KubernautAgent`) carry the field on their
  `FleetOverrideSpec` but nothing ever reads it -- dead API surface.
- **No real topology motivates divergence.** Every fleet-aware component
  shares one `spec.fleet.mcpGatewayEndpoint` and calls into the same
  `Backend`/`MCPServerRegistration` CRD registry. A per-component namespace
  override would only matter if different components needed to watch
  *different* MCP Gateway CRD registries -- which would imply multiple,
  differently-configured MCP Gateways, a topology this operator does not
  support and has no plan to.
- **The override adds real maintenance cost for no benefit.** #227 and
  #354 both had to account for per-component override behavior in their
  RBAC-provisioning and RBAC-pruning logic respectively, growing the test
  matrix (own-override-vs-shared-fallback, override-change-triggers-prune)
  for a capability with no legitimate use case.

## Decision

Drop `Namespace` from `FleetOverrideSpec` entirely. Every fleet-aware
component always uses the shared `spec.fleet.mcpGatewayNamespace` directly
-- no per-component override capability at all.

```go
// FleetOverrideSpec overrides spec.fleet.oauth2.credentialsSecretRef for a
// single fleet-aware component (federated IdP scenario: each component
// authenticates as a distinct OAuth2 client against the same shared
// spec.fleet.oauth2.tokenURL). All fleet-aware components share one MCP
// Gateway CRD registry (spec.fleet.mcpGatewayNamespace) -- there is no
// per-component namespace override (DD-362).
type FleetOverrideSpec struct {
	// +optional
	OAuth2CredentialsSecretRef string `json:"oauth2CredentialsSecretRef,omitempty"`
}
```

Concretely:

- `internal/resources/rbac.go` / `fleetmetadatacache.go`: the 5
  `effective*MCPGatewayNamespace` helpers (FMC, SP, AF, EM, plus the shared
  one in `common.go`) are removed. `MCPGatewayNamespaceRBAC`'s grants table
  and each component's conditional CRD-rule-omission check
  (`fleetMetadataCacheClusterRole`, `signalprocessingClusterRole`,
  `apifrontendClusterRole`, `effectivenessMonitorControllerClusterRole`)
  read `knV2.Spec.Fleet.MCPGatewayNamespace` directly.
- `internal/resources/configmaps.go`: `resolveMCPGatewayOnlyFleetConfig`
  and `resolveSignalProcessingFleetConfig` drop their `effectiveNamespace`
  parameter; both render `fleet.namespace` from
  `knV2.Spec.Fleet.MCPGatewayNamespace` unconditionally.
- `api/v1alpha1/kubernaut_conversion.go`: the WE-downgrade log line drops
  its `namespace` field mention (WE's `WorkflowExecutionFleetSpec` never
  had one; this is a pre-existing log-line cleanup, not new behavior).

## Alternatives Considered

1. **Keep `Namespace` but only on the 4 components that read it (drop it
   from Gateway/RO/KA's `FleetOverrideSpec` only).** Rejected: this still
   leaves the override on FMC/SP/AF/EM with no real use case (single shared
   MCP Gateway registry), and would require either splitting
   `FleetOverrideSpec` into two variants or accepting an inconsistent
   per-field-per-type shape across the 7 components for a capability that
   should be removed outright.
2. **Leave `FleetOverrideSpec.Namespace` as-is, unused-by-3/consumed-by-4,
   and just accept the inconsistency.** Rejected: perpetuates dead API
   surface (Gateway/RO/KA) and a real maintenance/test cost (#227, #354)
   for a capability nobody has an operational reason to use.
3. **Deprecate but don't remove (mark `+optional`, ignore at runtime).**
   Rejected: `v1alpha2` is unreleased (no v1.6 RC has shipped), so there is
   zero migration cost to removing it outright -- a deprecate-in-place step
   would only be warranted for an already-released API.

## Consequences

- **No breaking change**: `v1alpha2` is unreleased; no CR instances exist
  outside test fixtures.
- `internal/controller/mcpgatewaynamespacerbac_af_em_test.go` (#227's IT
  test) is reworked, not deleted: its "AF/EM's own override resolves
  differently from shared" premise no longer exists, so it is refocused on
  proving the shared `spec.fleet.mcpGatewayNamespace` alone drives
  namespace-scoped RBAC creation for AF/EM.
- `internal/controller/mcpgatewaynamespacerbac_prune_test.go` (#354's IT
  test) drops its "per-component override change" pruning scenario
  specifically (invalid once there is no override) but keeps the "shared
  namespace change" pruning scenario -- pruning still matters whenever
  `spec.fleet.mcpGatewayNamespace` itself changes.
- `make manifests generate` removes `namespace` from every component's
  `fleet` sub-schema in `config/crd/bases/kubernaut.ai_kubernauts.yaml`.
- **Compliance framing, stated honestly**: this is a complexity/attack-
  surface reduction, not itself the enforcement mechanism for a specific
  NIST 800-53 control. The closest legitimate framing is an indirect CM-6
  (Configuration Management: least-functionality) benefit -- removing an
  override capability with no real operational use case shrinks the
  configuration surface an administrator could misconfigure. This is
  recorded as a side-benefit, not claimed as the change's driving control
  objective.

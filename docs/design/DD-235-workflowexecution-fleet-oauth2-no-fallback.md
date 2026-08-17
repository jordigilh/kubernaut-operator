# DD-235: WorkflowExecution's fleet OAuth2 credential is a dedicated, non-fallback type

**Status**: Accepted
**Decision Date**: 2026-08-16
**Applies To**: `api/v1alpha2.WorkflowExecutionSpec`, `internal/resources/configmaps.go`, `internal/resources/deployments.go`, `internal/resources/validation.go`
**Related Issues**:
- jordigilh/kubernaut-operator#235 (this decision)
- `docs/design/ADR-CRD-001-v1alpha2-redesign.md` F1 (the decision this amends)

## Context

Upstream WorkflowExecution (WE) is the only fleet-integration-capable
component that calls MCP **write** tools
(`resources_create_or_update`/`resources_delete`, `pkg/fleet/mcpclient/writer.go`)
instead of the read-only tools every other fleet-aware service (Gateway,
RemediationOrchestrator, SignalProcessing, APIFrontend,
EffectivenessMonitor, KubernautAgent, FleetMetadataCache) uses. Per the
upstream Helm chart's own documented rationale (BR-FLEET-054, ADR-068),
sharing the read-only credential with WE's write-scoped client would be a
least-privilege violation — WE's credential must always be independently
set, with no shared default.

ADR-CRD-001 (F1) already added a `Fleet` field to
`v1alpha2.WorkflowExecutionSpec` — entirely new, since `v1alpha1` had no
Fleet field on `WorkflowExecutionSpec` at all — citing "unblocks #235." That
field used the same `*FleetOverrideSpec` type as the other 7 fleet-aware
components: `OAuth2CredentialsSecretRef` + `Namespace`, both falling back to
`spec.fleet.*` when unset. It was never wired into the ConfigMap, Deployment,
or validation, so this gap went undetected until #235's implementation
began.

Left as `*FleetOverrideSpec` and wired the same way as the other 7
components, this field would satisfy CRD-completeness but not the
least-privilege requirement: an administrator who left it unset would
silently get the shared credential mounted — exactly what #235 exists to
prevent.

## Decision

Replace `WorkflowExecutionSpec.Fleet *FleetOverrideSpec` with a new,
dedicated, minimal type:

```go
// WorkflowExecutionFleetSpec configures WorkflowExecution's own write-scoped
// MCP Gateway OAuth2 client (BR-FLEET-054, ADR-068). Unlike every other
// fleet-aware component's FleetOverrideSpec, this does NOT fall back to
// spec.fleet.oauth2.credentialsSecretRef: WE is the only fleet-integration
// service that calls MCP write tools, so it must never share the read-only
// credential used by Gateway/RemediationOrchestrator/SignalProcessing/
// APIFrontend/EffectivenessMonitor/KubernautAgent/FleetMetadataCache
// (least-privilege). Required when spec.fleet.oauth2.enabled is true;
// enforced by validateFleetOAuth2, not by kubebuilder (cross-tree
// condition).
type WorkflowExecutionFleetSpec struct {
	// +optional
	OAuth2CredentialsSecretRef string `json:"oauth2CredentialsSecretRef,omitempty"`
}
```

on `WorkflowExecutionSpec`:

```go
// +optional
Fleet WorkflowExecutionFleetSpec `json:"fleet,omitempty"`
```

Rationale for diverging from ADR-CRD-001's literal text ("reuse
`FleetOverrideSpec`"):

1. **No `Namespace` field.** The per-component `Namespace` override exists
   purely to scope each component's namespace-scoped `Role` watching the
   `Backend`/`MCPServerRegistration` CRDs (`internal/resources/rbac.go`'s
   `MCPGatewayNamespaceRBAC`) — it has nothing to do with which MCP Gateway
   endpoint to call (that's the single shared `spec.fleet.mcpGatewayEndpoint`).
   WE never watches those CRDs; it only calls MCP write tools over the
   shared gateway endpoint. A `Namespace` field would be dead weight on WE
   specifically.
2. **No `GatewayType` field either.** Verified against upstream
   `pkg/workflowexecution/config.FleetConfig` — WE's own config has only
   `Endpoint`/`OAuth2`, unlike KubernautAgent's `FleetConfig` (which carries
   `GatewayType` for its own static discovery-strategy selection). WE
   resolves the MCP tool-name prefix dynamically per target cluster through
   the shared `registry.ToolPrefixResolver`/`mcpclient.DiscoverToolPrefix`
   path (`pkg/workflowexecution/executor/client_factory.go`) — there is no
   static gateway-type knob for WE to carry.
3. **A distinct type makes "this one doesn't fall back" self-evident from
   the schema**, not just from a doc comment on an otherwise-identical
   shared type — this avoids the footgun of a future maintainer
   copy-pasting the `effectiveFleetOAuth2SecretRef(...)` one-liner used by
   the other 7 components onto WE.
4. **Zero migration cost.** The existing field was unwired and unreleased
   (no v1.6 RC had shipped), so changing its shape was free.
5. JSON key stays `spec.workflowExecution.fleet.oauth2CredentialsSecretRef`
   — same path shape as every sibling component; only the Go type name
   differs internally.

**Enforcement is a plain Go admission-validation check** inside
`validateFleetOAuth2` (`internal/resources/validation.go`), unconditionally
requiring `spec.workflowExecution.fleet.oauth2CredentialsSecretRef` whenever
`spec.fleet.oauth2.enabled` is true — deliberately *not* folded into the
existing 6-component "at least one has an effective value" tolerance loop,
since that loop's whole purpose (fallback tolerance) is precisely the
violation #235 forbids for WE. Not CEL: matches this function's existing
Go-only precedent for the other 6 components.

**Secret mount** is at `/etc/workflowexecution/<credentialsSecretRef>` —
confirmed to match upstream's own hardcoded expectation
(`cmd/workflowexecution/main.go`: `basePath = "/etc/workflowexecution/" + cfg.Fleet.OAuth2.CredentialsSecretRef`),
and naturally distinct from every other component's own `/etc/<component>`
mount convention.

## Alternatives Considered

1. **Keep `Fleet *FleetOverrideSpec` and only diverge in
   validation/resolution (Go-only divergence, no type change).** Rejected:
   the ADR-CRD-001-recorded type gives WE a `Namespace` field it can never
   use, and relies entirely on validation/doc-comment discipline (rather
   than the schema itself) to prevent a future maintainer from wiring WE the
   same fallback-tolerant way as the other 7 components.
2. **A new field name entirely (e.g. `WriteFleetOAuth2CredentialsSecretRef`)
   outside any nested struct.** Rejected: breaks the
   `spec.<component>.fleet.oauth2CredentialsSecretRef` path-shape
   consistency every other fleet-aware component uses, for no added
   clarity beyond what the dedicated type already provides.
3. **CEL `XValidation` backstop alongside the Go validation.** Deferred, not
   rejected: would mirror the existing Go-only precedent for the other 6
   components' equivalent check; can be added later if defense-in-depth is
   wanted, but isn't required to close #235.

## Consequences

- No breaking change: the prior `Fleet *FleetOverrideSpec` field was
  unwired and unreleased.
- WE joins the other 7 fleet-aware components in having a `fleet` block
  under its own spec, but is the only one where the OAuth2 credential
  sub-field is enforced as independently required rather than
  fallback-eligible.
- New IT coverage (`internal/controller/workflowexecution_fleet_test.go`)
  proves the field flows end-to-end from the CR through `KubernautReconciler`
  into the rendered `workflowexecution-config` ConfigMap and the
  `workflowexecution` Deployment's volume mounts.

# IEEE 829 Test Plan — Issue #227: Namespace-scoped RBAC for AF/EM MCP Gateway CRD reads

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-227                                             |
| **Issue**          | #227 — Migrate APIFrontend/EffectivenessMonitor MCP Gateway CRD RBAC from cluster-scoped ClusterRole to namespace-scoped Role once upstream#1686 lands |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-08-16                                         |
| **Scope**          | `internal/resources/{configmaps,rbac}.go`, `internal/controller/kubernaut_controller.go` |

## 1. Objective

#224 (TP-224, Finding 4) deliberately left AF/EM's MCP Gateway CRD watch
permanently cluster-scoped: their upstream `ClusterRegistry` construction
hardcoded `registry.RegistryConfig{}` (empty `Namespace`), so granting them
only a namespace-scoped `Role` would have made the apiserver reject their
LIST/WATCH with `403 Forbidden`. That upstream gap is
[kubernaut#1686](https://github.com/jordigilh/kubernaut/issues/1686)
(FedRAMP AC-6/CM-6, BR-RBAC-020), closed by
[kubernaut#1720](https://github.com/jordigilh/kubernaut/pull/1720) (merged
2026-07-24), which threads a new `FleetConfig.Namespace` field into
`registry.RegistryConfig` for both `cmd/apifrontend/backend_deps.go` and
`cmd/effectivenessmonitor/main.go`. The operator's `go.mod` pin
(`v1.6.0-rc1.0.20260815033113-278e0c1614db`, #339) postdates that merge, so
the upstream blocker is resolved and this issue's action items are
unblocked.

**Preflight finding**: the operator-side API surface this issue calls for
(`FleetOverrideSpec.Namespace`, `APIFrontendSpec.Fleet`/
`EffectivenessMonitorSpec.Fleet`) already exists — #224 introduced
`FleetOverrideSpec` as a shared, embeddable type and already wired it onto
every fleet-aware component's spec, AF/EM included, in anticipation of this
follow-up. No CRD/API schema change is required (`make manifests generate`
produces no diff); this is a pure rendering + RBAC-shape change, exactly
mirroring FMC/SP's existing #224 Finding 5 retrofit pattern.

Verify that:

1. `resolveMCPGatewayOnlyFleetConfig` (AF/EM's `fleetConfigYAML` renderer)
   gains an `effectiveNamespace` parameter and renders it as `fleet.namespace`
   in AF's/EM's ConfigMap, resolved via new
   `effectiveAPIFrontendMCPGatewayNamespace`/
   `effectiveEffectivenessMonitorMCPGatewayNamespace` helpers (mirroring
   `effectiveSignalProcessingMCPGatewayNamespace`): AF's/EM's own
   `spec.{apiFrontend,effectivenessMonitor}.fleet.namespace` override, falling
   back to the shared `spec.fleet.mcpGatewayNamespace`.
2. `apifrontendClusterRole`/`effectivenessMonitorControllerClusterRole` omit
   the MCP Gateway CRD rules once their component's effective namespace
   resolves non-empty (previously: always included whenever fleet MCP
   Gateway reads were enabled, per #224 Finding 4).
3. `MCPGatewayNamespaceRBAC` is extended to grant AF/EM a namespace-scoped
   `Role`/`RoleBinding` in their resolved namespace, using the same shared
   rule-set/grant-table code path already serving FMC/SP.
4. The controller wiring requires no changes beyond doc-comment updates:
   `deployCoreRBAC`'s existing generic prune (#341) and
   `deployMCPGatewayNamespaceRBAC`'s existing `ensureUnowned` loop already
   generalize to any component in `MCPGatewayNamespaceRBAC`'s grant table,
   so adding AF/EM's two entries is sufficient — confirmed by extending the
   IT coverage rather than by inspection alone (Pyramid Invariant: IT proves
   wiring).
5. All pre-existing #224 FMC/SP namespace-retrofit and AF/EM cluster-scoped
   RBAC/config tests remain green (additive; every CR that never sets an AF/EM
   fleet namespace override keeps the pre-#227 cluster-scoped behavior
   unchanged).

**Explicitly out of scope:**

- AF/EM NetworkPolicy egress and OAuth2 Secret mounting — already fleet-aware
  since #224 (FW-051/FW-060), unaffected by this RBAC-only, watch-scope
  change.
- Any CRD/API schema change — the `Fleet *FleetOverrideSpec` surface already
  exists on both `APIFrontendSpec` and `EffectivenessMonitorSpec`.

## 2. Test Strategy

Standard TDD (RED -> GREEN -> REFACTOR). Pyramid Invariant applies in full:
unit tier (`internal/resources`) proves the rendering/RBAC-shape logic in
isolation; one integration scenario (`internal/controller`, envtest-backed)
proves the reconciler actually wires `MCPGatewayNamespaceRBAC`'s AF/EM grants
end-to-end — namespace-scoped `Role`/`RoleBinding` created in the target
namespace *and* the cluster-scoped `ClusterRole`'s MCP Gateway rules actually
gone, not just that the unit-level builder function returns the right Go
value. A resource-builder change proven only at the unit tier is prototyped,
not implemented, per the project's Pyramid Invariant rule.

## 3. Test Scenarios

### 3.1 AF/EM fleet ConfigMap rendering (`internal/resources/configmaps_test.go`)

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| NS-001 | AC-6    | EM's rendered `fleet:` block omits `namespace` when neither `spec.effectivenessMonitor.fleet.namespace` nor `spec.fleet.mcpGatewayNamespace` is set (preserves pre-#227 cluster-wide default) | Yes |
| NS-002 | AC-6    | EM's `fleet.namespace` falls back to the shared `spec.fleet.mcpGatewayNamespace` when EM has no own override | Yes |
| NS-003 | AC-6    | EM's own `spec.effectivenessMonitor.fleet.namespace` override takes precedence over the shared default | Yes |
| NS-004 | AC-6    | AF's `fleet.namespace` rendering mirrors EM's (own override, shared fallback, omitted-when-neither-set) — analogous `It`s alongside NS-001..003 | Yes |

### 3.2 RBAC (`internal/resources/rbac_test.go`)

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| NS-010 | AC-6    | `apifrontendClusterRole` omits the MCP Gateway CRD rules once AF's effective namespace resolves | Yes |
| NS-011 | —       | `apifrontendClusterRole` still includes the MCP Gateway CRD rules when AF has no effective namespace (regression guard, preserves pre-#227 behavior for the common case) | Yes |
| NS-012 | AC-6    | `effectivenessMonitorControllerClusterRole` omits the MCP Gateway CRD rules once EM's effective namespace resolves | Yes |
| NS-013 | —       | `effectivenessMonitorControllerClusterRole` still includes the MCP Gateway CRD rules when EM has no effective namespace (regression guard) | Yes |
| NS-014 | AC-6    | `MCPGatewayNamespaceRBAC` returns a `Role`/`RoleBinding` pair for AF, bound to AF's own ServiceAccount, when AF's effective namespace resolves | Yes |
| NS-015 | AC-6    | `MCPGatewayNamespaceRBAC` returns a `Role`/`RoleBinding` pair for EM, bound to EM's own ServiceAccount, when EM's effective namespace resolves | Yes |
| NS-016 (regression) | — | All pre-existing FMC/SP `MCPGatewayNamespaceRBAC` and cluster-scoped-rule assertions (TP-224 FW-041..048) stay green unchanged | Yes |

### 3.3 Controller integration (`internal/controller/mcpgatewaynamespacerbac_af_em_test.go`)

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| NS-020 | AC-6    | Setting `spec.apiFrontend.fleet.namespace`/`spec.effectivenessMonitor.fleet.namespace` and reconciling to Running creates AF's/EM's namespace-scoped `Role`+`RoleBinding` in their target namespaces, carrying the MCP Gateway CRD rules | Yes |
| NS-021 | AC-6    | The same reconcile removes the MCP Gateway CRD rules from AF's/EM's cluster-scoped `ClusterRole` — verified by reading the live `ClusterRole` object post-reconcile, not by unit-level inspection of the builder's return value | Yes |
| NS-022 (regression) | — | Full pre-existing `internal/controller` suite (176 specs) remains green | Yes |

## 4. Acceptance Criteria

- All scenarios above pass via `make test-unit` and `make test-integration`.
- `go vet ./...` and `golangci-lint run` report 0 issues.
- `make manifests generate` produces no diff (no CRD/API schema change — the
  `Fleet *FleetOverrideSpec` surface predates this issue).
- No behavior change for CRs that leave AF's/EM's fleet namespace override
  (and the shared `spec.fleet.mcpGatewayNamespace` fallback) unset — AF/EM
  keep the pre-#227 cluster-scoped grant, matching their pre-#1720 upstream
  behavior exactly.
- Business-level verification, not implementation-detail verification: every
  scenario above asserts on the actual security-relevant artifact (rendered
  `fleet.namespace` YAML value, `ClusterRole`/`Role` `Rules`/`APIGroups`
  content, or the live RBAC object read back from the API server in NS-020/
  NS-021) rather than on internal call counts or mock expectations — the
  control objective (AC-6 least-privilege: no cluster-wide grant once a
  narrower one is possible) is only actually verified by inspecting the
  resulting permission surface itself.

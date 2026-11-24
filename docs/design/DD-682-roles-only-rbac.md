# DD-682: Roles-Only RBAC — The Operator Renders Roles, Never User Bindings

**Status**: Accepted
**Decision Date**: 2026-11-23
**Version**: 1.0
**Confidence**: 94%
**Deciders**: Kubernaut Operator owner (Jordi Gil)
**Applies To**: `api/v1alpha2/kubernaut_types.go`,
`internal/resources/rbac.go`, `internal/resources/validation.go`,
`internal/controller/kubernaut_controller.go`

**Related Issues**:
- jordigilh/kubernaut-operator#682 (umbrella decision)
- jordigilh/kubernaut-operator#684 (execution, this change)
- jordigilh/kubernaut-operator#683 (documentation rules folded in below)
- jordigilh/kubernaut-operator#181 (custom ClusterRole refs — precedent)
- jordigilh/kubernaut-operator#662 / DD-662 (console-reader unbound model)
- jordigilh/kubernaut-operator#636 / DD-636 (persona audit; amended to v1.3)
- jordigilh/kubernaut-operator#665 / DD-278 (approval-read separation of duties)
- jordigilh/kubernaut-operator#664 (dual alert tool names)

---

## Context & Problem

`spec.apiFrontend.rbac.roleBindings` and `consoleAccessGroups` put
user-access policy — who is bound to which role — inside the application
CR. That is an operational/SRE decision owned by the platform team, not a
deployment decision: anyone delegated to manage the Kubernaut CR is
implicitly delegated user-access policy. The six built-in personas
(`tool-sre`, `tool-ai-orchestrator`, `tool-cicd`, `tool-observability`,
`tool-l3-audit`, `tool-remediation-approver`) additionally enforced
organizational choices the operator should not make: personas are
operational decisions that must be composable per site from real SOPs.

## Decision (owner, 2026-11-23)

Continues the direction of #181 (custom ClusterRole refs) and
#666/DD-662 (console-reader rendered, binding admin-owned):

1. **The operator renders roles only.** All user-facing
   ClusterRoleBindings become admin-owned (external CRBs, GitOps,
   OpenShift RBAC). The operator never renders a user-facing CRB; every
   CRB it renders binds a ServiceAccount of the managed components.
2. **Removed from the CRD** (`spec.apiFrontend.rbac`): `roleBindings`,
   `consoleAccessGroups`, and the `ToolRoleBinding` type. Kept:
   `sarCacheTTL` and `consoleAccessAuthorizationCheckEnabled` (they
   configure AF itself, not user bindings). The status field
   `boundToolRoleBindings` and the `ToolRBACBound` condition are removed
   with them.
3. **The six built-in persona ClusterRoles are dropped entirely.** The
   audited tool sets ([DD-636](DD-636-console-direct-action-rbac.md)
   v1.0–v1.2) are preserved as **documented example use-case bundles**
   in the installation guide, drift-guarded by unit tests. The operator
   keeps rendering: `console-access` (gate role), `console-reader` (read
   capabilities), and `console-inventory-reader` (local-target reads),
   plus all service/backend roles — unbound, label-pruned, and
   finalizer-swept.
4. **No migration.** v1.6 is pre-GA; clean deployments are expected.

## Documentation rules folded in (#683)

- Alert-capable example bundles carry **both** alert tool names —
  `list_alerts` (A2A) and `kubernaut_list_alerts` (MCP) — because AF
  authorizes the exact tool name each transport uses (#664).
- Approval reads and decisions stay with the human/approver bundles
  only, never the automated `ai-orchestrator` bundle (#665 resolution,
  DD-278 separation-of-duties intent).
- No change to upstream AF behavior: name unification and proxy/AF
  changes stay out of scope.

## Consequences

- The DD-662 explicit bind path
  (`spec.apiFrontend.rbac.roleBindings[].clusterRoleName`) is removed;
  the only supported bind path for `console-reader` (and every other
  rendered role) is an administrator-owned external ClusterRoleBinding.
  DD-662's permanent model — the coarse unbound role — is unchanged.
- Upgrading a pre-v1.6 deployment that used `roleBindings`/
  `consoleAccessGroups` leaves its stale user-facing CRBs in place
  (`<ns>-console-access-binding`, `<ns>-tool-*-binding`); they keep
  granting authorization the operator no longer tracks and must be
  removed manually. The installation guide documents the exact cleanup
  commands. This is accepted: pre-GA, clean deployments expected.
- The legacy `apifrontend-rbac-roles` ConfigMap orphan cleanup (#118)
  is preserved in the reconcile path.

## Alternatives Considered

| Option | Rejected Because |
|---|---|
| Keep `roleBindings` with only custom `clusterRoleName` refs | Still couples user-access policy to CR management |
| Keep persona roles, drop only the CRBs | Personas still encode organizational choices; sites need to compose roles from SOPs |
| One-shot legacy CRB sweep on upgrade | Contradicts the no-migration decision for a pre-GA release; manual cleanup is documented instead |

## Test Evidence (pyramid)

- UT (`internal/resources/issue636_console_rbac_test.go`): documented
  example bundles match the audited matrix; dual alert names (#683);
  approval separation (DD-278); no bundle is operator-rendered.
- UT (`internal/resources/rbac_test.go`, `issue662_console_read_model_test.go`):
  the `ClusterRoles()` aggregator renders no `tool-*` role; no rendered
  CRB carries a Group-kind subject; the three console roles render when
  AF is enabled and drop when disabled.
- IT (`internal/controller/kubernaut_lifecycle_test.go`): through a real
  envtest reconcile — no persona ClusterRole exists, the three console
  roles do, and no ClusterRoleBinding in the cluster carries a
  user-facing subject; finalizer sweeps the console roles without ever
  creating a CRB (IT-CONSOLE-005/010 rewritten).

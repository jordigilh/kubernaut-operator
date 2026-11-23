# DD-636: Console Direct-Action RBAC Audit and Least-Privilege Roles

**Status**: Accepted
**Decision Date**: 2026-11-22
**Version**: 1.1
**Confidence**: 92%
**Deciders**: Kubernaut Operator Team
**Applies To**: `internal/resources/rbac.go` (`toolPersonas`,
`ConsoleAccessClusterRole`, `ConsoleReaderClusterRole`),
`internal/controller/kubernaut_controller.go` (`deployToolRBAC`,
`deployConsoleAccessRBAC`, `deployClusterScopedCoreRBAC`)

**Related Issues**:
- jordigilh/kubernaut-operator#636 (this audit)
- jordigilh/kubernaut-operator#289/#290 (console-access gate, prior change)
- jordigilh/kubernaut-operator#278 / DD-278 (SRE approval-visibility tools, amended here)
- jordigilh/kubernaut#1919 (server-side console gate enforcement)
- jordigilh/kubernaut#2584 / DD-AF-018 (console policy/managed-resource read projection)
- jordigilh/kubernaut#1235 / jordigilh/kubernaut#1239 (approval least-privilege design)
- ADR-021 (SAR-based tool authorization)

---

## Changelog

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-11-22 | Operator Team | Initial audit, role matrix, console-reader role |
| 1.1 | 2026-11-23 | Operator Team | #664 decision (row 4d, F3): raw-MCP alert alias `kubernaut_list_alerts` granted to the alert-capable personas, Option 2 interim with upstream unification as end-state |

---

## Context & Problem

Console login authorization is not action authorization. An authenticated
user must receive an explicit role for every console action they may perform,
and login must not imply workflow listing, approval, investigation, or any
other direct action. The operator is the sole renderer of user-facing Kubernaut
RBAC in v1.6 (the upstream Helm chart no longer exists), so this document is
the operator-side source of truth for the console action inventory and the
role-to-action matrix.

## Authorization Contract (verified upstream)

AF authorizes every console action through a Kubernetes `SubjectAccessReview`
with `verb: use`, `group: kubernaut.ai`, and one of two synthetic resources:

1. **Console gate** — `resource: console`, no name, cluster-scoped
   (`pkg/apifrontend/auth/sar.go` `CheckConsoleAccess`, kubernaut#1919).
   It is enforced at `/a2a/access` and at every tool-invocation site, but it
   is **opt-in** server-side:
   `spec.apiFrontend.rbac.consoleAccessAuthorizationCheckEnabled` defaults to
   `false` (kubernaut-operator#338), which makes the gate authentication-only.
   The gate never authorizes an action by itself.
2. **Per-action** — `resource: tools`, `resourceName: <name>`, cluster-scoped
   (`pkg/apifrontend/auth/sar.go` `Check`, ADR-021). The same resource carries
   both tool names and the console REST read capability names
   (`pkg/apifrontend/policyread/service.go` `authorize`).

Additional user-scoped checks for the console REST read projection:
local target reads run exact/list SARs as the invoking user against the real
hub resource (namespace-scoped); remote reads use AF's `ScopeChecker` with
AF's backend identity (DD-FLEET-017 D5). Browser tokens are never forwarded.

## Console Direct-Action Inventory

| # | Surface / action | Authorization check | Required Kubernetes/API permissions | Operator role | Status |
|---|---|---|---|---|---|
| 1 | oauth2-proxy login, `/oauth2/userinfo` | none (authentication) | none | — | Separate from action authorization by design |
| 2 | `GET /a2a/access` + server-side gate on every tool call | SAR `kubernaut.ai/console` `use` | ClusterRole `console-access`: `kubernaut.ai/console` `use` | `ConsoleAccessClusterRole` + CRB | Enforced only when `consoleAccessAuthorizationCheckEnabled=true`; grants no action |
| 3 | A2A chat `message/stream`, `status/subscribe` | no per-action SAR; the agent's tool calls are checked per-tool | per-tool (row 4) | — | No direct grant |
| 4a | Investigation: `kubernaut_investigate`, `kubernaut_investigate_alert`, `kubernaut_check_existing_remediation`, `kubernaut_select_workflow`, `kubernaut_present_decision`, `kubernaut_message`, `kubernaut_complete`, `kubernaut_cancel`, `kubernaut_status`, `kubernaut_reconnect`, `kubernaut_await_session`, `kubectl_get`, `kubectl_list`, `kubectl_list_events`, `list_alerts`, `get_alert_details`, `list_clusters` | SAR `kubernaut.ai/tools` `use` name=`<tool>` | persona ClusterRole rule only; the AF ServiceAccount performs the backend reads (ADR-022) | `tool-*` personas | Granted per persona |
| 4b | Approval: `kubernaut_approve`, `kubernaut_list_approval_requests`, `kubernaut_get_approval_request`, `kubernaut_complete_no_action` | same | same | `tool-sre`, `tool-remediation-approver` | Granted; separation of duties per DD-278 |
| 4c | Remediation + workflow catalog: `kubernaut_remediate`, `kubernaut_cancel_remediation`, `kubernaut_watch`, `kubernaut_list_remediations`, `kubernaut_get_remediation`, `kubernaut_list_workflows`, `kubernaut_discover_workflows`, `kubernaut_get_remediation_history`, `kubernaut_get_effectiveness`, `kubernaut_get_audit_trail` | same | same | `tool-*` personas | Granted per persona |
| 4d | Raw MCP alias `kubernaut_list_alerts` (A2A name is `list_alerts`) | same | same | `tool-sre`, `tool-ai-orchestrator` | **Decided (#664)** — Option 2 interim: the two personas that hold the A2A alert names also hold the MCP alias, so one persona grant authorizes both surfaces; upstream unification (one authorization identity per capability) remains the end-state and removes the alias |
| 5 | Console REST read projection: `GET /api/v1/console/policies/scope`, `/inventory/managed-resources[/{id}]` (kubernaut#2584) | capability SARs `kubernaut_read_policy_scope`, `kubernaut_list_managed_resources`, `kubernaut_get_managed_resource` + user-scoped local target get/list; remote via ScopeChecker | ClusterRole `console-reader`: `kubernaut.ai/tools` `use` on the three capability names. Local target reads additionally need user-bound get/list on the exact hub resources, which remain administrator-owned (deployment-specific) | `ConsoleReaderClusterRole` (unbound by default) | New in this change; not reachable through the operator-managed Console nginx, which proxies only `/a2a/`, `/mcp`, `/.well-known/` — the role serves direct AF and future console/plugin clients |
| 6 | `/.well-known/agent-card` | authenticated surface, no per-action SAR | none | — | No grant |
| 7 | AF ServiceAccount metadata inventory read (`<ns>-apifrontend-inventory-read`) | not a user check; backend transport identity | get/list on namespaces, nodes, pods, services, PVs, PVCs, deployments, replicasets, statefulsets, daemonsets, jobs, cronjobs, HPAs + admin-approved `spec.additionalClusterRoles` read projection (`internal/resources/inventory_rbac.go`) | bound to the AF SA | Classified: backend reads, never a substitute for a user capability |

No console direct action is unclassified. Row 5's user-side target grants are explicitly deferred with tracked
follow-up F2 rather than guessed; row 4d was decided in #664.

## Which Role Is Which

| Role | Bound to | Authorizes | Notes |
|---|---|---|---|
| `console-access` | console-access groups (derived or explicit) | `kubernaut.ai/console` `use` — the UI gate | Login-level only; never implies a tool |
| `tool-<persona>` | OIDC groups from `spec.apiFrontend.rbac.roleBindings` | named tool actions | One ClusterRole per persona |
| `console-reader` | nothing by default | the three console REST read capabilities | Bind via `roleBindings[].clusterRoleName` (rendered name is `<ns>-console-reader`, used verbatim) or an external CRB |
| `<ns>-apifrontend-inventory-read` | AF ServiceAccount | backend metadata reads for the projection | Never a user grant |

A group bound to a persona also joins `effectiveConsoleAccessGroups`, so it
picks up the console gate automatically; that is an upgrade-safety default
(kubernaut#289), not an action grant.

## Decision

1. Keep authentication (row 1) separated from the opt-in console gate (row 2)
   and from per-action authorization (rows 4–5). No login or console-access
   grant may be widened to include tool or capability names.
2. Render a new, least-privilege `console-reader` ClusterRole exposing exactly
   the three console REST read capabilities, unbound by default, because
   upstream has not decided which persona owns those reads. The three
   capabilities are intentionally one coarse role ("coarse-grained user
   roles" per #636); splitting them further would add roles without an
   upstream authorization distinction.
3. Amend DD-278 to v1.1 (below): `tool-sre` gains
   `kubernaut_list_approval_requests` (approval visibility consistent with the
   existing `kubernaut_approve` grant) and `list_clusters` (read-only fleet
   discovery, BR-FLEET-054). `tool-ai-orchestrator` gains `list_clusters` only.
4. Do **not** grant approval reads or `kubernaut_get_approval_request` to
   `tool-ai-orchestrator`. The upstream E2E persona fixture currently lists
   them, but that is a test fixture, not a security decision; DD-278's
   separation-of-duties intent keeps approval actions with human personas.
   The divergence is tracked in follow-up F4.
5. Do not guess the raw-MCP alert alias (`kubernaut_list_alerts`) or user-side
   local target grants; both are follow-ups F3 and F2. **Update (#664):** F3 is
   now decided — see "Decision (#664)" below; F2 remains open.

## Decision (#664)

Issue #664 asked to choose between (1) upstream unifying the alert tool name
across both surfaces and (2) the operator granting both names. The decision is
**Option 2, interim**:

- `tool-sre` and `tool-ai-orchestrator` — the only personas that hold the A2A
  alert names (`list_alerts`, `get_alert_details`) — now also hold the raw-MCP
  alias `kubernaut_list_alerts`. One persona grant therefore authorizes the
  capability on both the A2A/ADK surface and raw `/mcp` (which the Console
  proxies), fixing the split-brain denial without widening any other persona.
- Least-privilege review (AC-6): the alias is granted to exactly the personas
  that already own the alert capability; the other four personas hold no alert
  tool under either name (pinned by the golden persona matrix in
  `issue636_console_rbac_test.go`; the #664 UT additionally asserts the two
  alert tool names per persona). SAR on a tool name
  that is never invoked is inert, so the alias is harmless if upstream renames.
- This is a deliberate, documented divergence from the upstream persona tool
  lists (see the inline comment in
  `internal/resources/rbac.go`): a future persona tool-list resync must not drop it.
- **Option 1 remains the end-state**: once upstream AF unifies the name (one
  authorization identity per capability) and its E2E drops the sre-only compat
  ClusterRole, the operator removes the alias and this decision collapses to a
  single name. Tracked here; the removal is a mechanical resync.

## Amendment to DD-278 (v1.1)

`kubernaut_list_approval_requests` is added to `tool-sre` under the same
interim rationale as DD-278 v1.0: `sre` already holds the approval decision
(`kubernaut_approve`) and can read a single RAR
(`kubernaut_get_approval_request`); listing pending RARs only makes an
already-possible decision discoverable. This does not reverse
kubernaut#1235/#1239, and the DD-278 revisit trigger still applies.
`list_clusters` is read-only fleet discovery and carries no approval or
mutation authority.

## Default and Migration Behavior

- Additive only: no existing role loses a permission; no existing binding is
  changed except that the persona ClusterRoles gain the names above on the
  next reconcile (create-or-update semantics).
- The console-reader role is new and unbound. Zero-config installs behave
  exactly as before: console reads return `forbidden` because no user holds
  the capability SAR, which is the intended deny-by-default behavior.
- Disabling `spec.apiFrontend` prunes the console-reader role through the
  label-based core ClusterRole prune; the finalizer removes it on CR deletion.

## Wiring Manifest

| Component | Production entry point | Wiring location | Integration proof |
|---|---|---|---|
| `console-reader` ClusterRole | `deployCoreRBAC` → `deployClusterScopedCoreRBAC` → `ClusterRoles()` aggregator | `internal/resources/rbac.go` AF-enabled block; deploy + label prune + finalizer sweep | IT-CONSOLE-006 (rendered, unbound), IT-CONSOLE-008 (prune on AF disable), IT-CONSOLE-009 (finalizer) |
| persona tool additions | `deployToolRBAC` → `ToolClusterRoles` | `internal/resources/rbac.go` `toolPersonas` | IT-CONSOLE-007; UT golden persona matrix |

## Test Evidence (pyramid)

| ID | Tier | Business behavior | Control |
|---|---|---|---|
| Issue #636 golden persona matrix | UT | every persona renders exactly the audited tool set; drift fails loudly | AC-6 |
| Issue #636 console-reader shape | UT | exactly three capabilities, `use` on `tools`, cluster-scoped | AC-3 / AC-6(2) |
| Issue #636 allow/deny boundary | UT | capabilities allowed; `kubernaut_approve`, remediation, unnamed tools, `console` gate, non-`use` verbs denied | AC-3, ASVS V4.1.1 |
| Issue #636 no implicit capability | UT | no built-in persona holds a console read capability | AC-6, CC6.3 |
| IT-CONSOLE-006 | IT | role rendered and left unbound after a real reconcile | AC-2 / CC6.1 |
| IT-CONSOLE-007 | IT | persona tool additions reach the cluster through the reconciler | AC-6 |
| IT-CONSOLE-008/009 | IT | prune on AF disable; finalizer cleanup | CM-2 / CC8.1 |

Namespace boundaries: the console SARs are cluster-scoped synthetic resources
by contract (AF never sets a namespace), so no namespace boundary applies to
rows 2/4/5 capability grants. Local target SARs (row 5) carry the target
namespace and remain administrator-bound.

## Acceptance-Criteria Traceability

| Issue #636 AC | Artifact | Status |
|---|---|---|
| Authenticate without receiving every action | Inventory rows 1–2; console-access rule shape test; console-reader unbound IT | Met |
| Workflow listing/reading and each direct action require the explicit role | Rows 4a–4c matrix; golden persona matrix; allow/deny boundary test | Met |
| Missing roles produce a clear denial without executing | Upstream AF enforcement (fail-closed 403 / permission-denied); deny-by-default asserted here | Met (operator side); upstream tests cited |
| Complete inventory, no unclassified direct actions | Full table above; row 5 deferred with F2, row 4d decided via #664 | Met |
| Tests and documentation cover the role matrix | This document + tests table | Met |

## Follow-Ups

- **F1** — (#662) Upstream must decide the persona↔console-read capability
  mapping and whether the operator-managed Console (or the OCP console plugin)
  should proxy `GET /api/v1/console/*` at all; until then `console-reader`
  stays unbound and the standalone Console cannot reach the projection routes.
- **F2** — (#663) User-side local-target Kubernetes get/list grants for the
  console inventory projection: the projected resource set depends on
  upstream's allowlist and the deployment's managed scope, so the operator
  cannot render them without guessing.
- **F3** — (#664) Raw-MCP alias `kubernaut_list_alerts`: **decided** — see
  "Decision (#664)".
- **F4** — (#665) `tool-ai-orchestrator` approval reads
  (`kubernaut_list_approval_requests`, `kubernaut_get_approval_request`):
  the upstream E2E persona fixture lists them; DD-278 separation of duties
  currently wins. Requires an explicit decision, not fixture parity.

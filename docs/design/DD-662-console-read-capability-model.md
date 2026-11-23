# DD-662: Console Read Capability Model (Persona Mapping and Proxy)

**Status**: Accepted
**Decision Date**: 2026-11-23
**Version**: 1.0
**Confidence**: 93%
**Deciders**: Kubernaut Operator Team (owner decision on #662 spike options)
**Applies To**: `internal/resources/rbac.go` (`ConsoleReaderClusterRole`,
`ToolClusterRoleBindings`, `ConsoleAccessClusterRoleBinding`),
`internal/resources/console.go` (nginx `/api/v1/console/` location, unchanged)

**Related Issues**:
- jordigilh/kubernaut-operator#662 (this decision)
- jordigilh/kubernaut-operator#636 / DD-636 (audit that deferred both questions as F1)
- jordigilh/kubernaut-operator#638 / #660 (Console nginx read-API proxy)
- jordigilh/kubernaut-operator#666 (console-reader ClusterRole, unbound)
- jordigilh/kubernaut#2584 / DD-AF-018 (console read projection contract)

---

## Changelog

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-11-23 | Operator Team | Resolves DD-636 F1: permanent capability model, proxy stays transport-only |

---

## Context & Problem

DD-636 rendered the `console-reader` ClusterRole (the three AF console REST
read capabilities) unbound, because two questions had no decided answer:

1. Which persona(s) own the console read capabilities?
2. Does the operator-managed Console proxy `GET /api/v1/console/*`, and if
   so, under what authorization model?

A #662 spike verified the upstream facts before deciding:

- The three capability names (`kubernaut_read_policy_scope`,
  `kubernaut_list_managed_resources`, `kubernaut_get_managed_resource`)
  appear in exactly one non-test upstream source file
  (`pkg/apifrontend/policyread/service.go`, `authorize`) plus upstream
  design/test-plan docs (DD-AF-018, DD-FLEET-017, the kubernaut#2584
  implementation plan, the #2661 test plan). No upstream persona
  definition, Helm values entry, ClusterRole, or E2E fixture binds them to
  any persona or group.
- Upstream authorization is per user: AF runs a `use` SAR on
  `kubernaut.ai/tools` with the capability `resourceName` as the invoking
  user (plus user-scoped local target SARs; remote reads use AF's backend
  identity via `ScopeChecker`). Upstream defines *how* reads are
  authorized, never *who* should hold them.
- The kubernaut#2584 implementation plan §8.2 explicitly deferred "add the
  named read capabilities to selected built-in personas" without selecting
  any personas — an open TODO, not an implicit mapping.
- The inference that "sre" owns the reads traces to the operator's Kind
  qualification realm having a single `sre` group/user — a test fixture,
  not a security decision (the same class of evidence DD-636 decision 4
  already rejected for DD-278/#665).
- The Console nginx `/api/v1/console/` location shipped by #638/#660 is
  transport-only: GET-restricted, rate-limited, TLS-verified, forwarding
  only oauth2-proxy's verified session token. `E2E-CN-638-HTTP-001`
  already accepts both the authorized 200 and AF's `forbidden` capability
  envelope as valid outcomes, so an unbound role is a fully supported
  state of the shipped surface. The Console SPA degrades gracefully on
  the `forbidden` read state (kubernaut-console read-state panel).

## Decision

1. **The coarse `console-reader` ClusterRole is the permanent model** (owner
   option 1A). No built-in persona gains the console read capabilities, and
   none is ever inferred from a persona, login, or console-access grant.
   Binding is an explicit administrator choice through
   `spec.apiFrontend.rbac.roleBindings[].clusterRoleName` (rendered name
   `<namespace>-console-reader`, used verbatim) or an external
   ClusterRoleBinding.
2. **The Console proxy stays, as transport only** (owner option 2A). The
   nginx `/api/v1/console/` location from #638/#660 is unchanged: it moves
   authenticated reads to AF and enforces no authorization of its own.
   Capability authorization happens entirely at AF via the named
   capability SARs; the proxy works unchanged whether or not any group is
   bound. Users who lack the capability see AF's `forbidden` envelope and
   the SPA's corresponding read state — the intended deny-by-default
   behavior. The boundary is the capability SAR itself, not this role's
   bind state: an administrator may equivalently grant `use` on the three
   capability resourceNames through any other ClusterRole.

Rejected alternatives (with reasons):

| Option | Rejected because |
|---|---|
| 1B: add the capabilities to `tool-sre` | Makes any SRE-group user implicitly a console reader; contradicts DD-636 decision 1 (login/persona grants never widen to capabilities) and #666's no-implicit-capability test; based on fixture evidence, not authority |
| 1C: new CRD field `consoleReaderGroups` | CRD schema change for a bind path `roleBindings[].clusterRoleName` already expresses; adds surface without an authorization distinction |
| 2B: bind console-reader to the derived console-access groups | Couples the login gate to reads; everyone who can log in reads the projection, defeating deny-by-default |
| 2C: remove the nginx location | Regresses kubernaut-console#164 (the SPA cannot reach the API; CSP is `connect-src 'self'`) and discards E2E-CN-638-HTTP-001; the surface is proven safe |

## Operator Contract (pinned by tests)

- `ConsoleReaderClusterRole` renders exactly one rule: `use` on
  `kubernaut.ai/tools` with the three capability names — no splitting, no
  verb widening without a new DD (`issue662_console_read_model_test.go`).
- No operator-rendered ClusterRoleBinding — persona path, custom path, or
  derived console-access path — ever references `console-reader` implicitly.
- The only supported bind path is an explicit `clusterRoleName` reference
  or an external CRB.
- `internal/resources/issue636_console_rbac_test.go` continues to pin that
  no built-in persona holds a console read capability.

## Default and Migration Behavior

No behavior change: this decision records the existing state as permanent.
Zero-config installs keep returning `forbidden` for console reads because no
user holds the capability SAR — deny-by-default is a property of the
capability check, not of this role's bind state (a deployment granting the
same `use` resourceNames through another ClusterRole is authorized by
design). The
documentation now states this as the model rather than as a pending
upstream decision.

## Wiring Manifest

No new components. Documentation and pinning tests only:

| Component | Production entry point | Wiring location | Integration proof |
|---|---|---|---|
| `console-reader` role (unchanged) | `deployCoreRBAC` → `ClusterRoles()` | `internal/resources/rbac.go` | IT-CONSOLE-006/008/009 (existing) |
| nginx `/api/v1/console/` location (unchanged) | Console ConfigMap render | `internal/resources/console.go` | UT-CN-638-001–007, E2E-CN-638-HTTP-001 (existing) |

## Test Evidence

| ID | Tier | Business behavior | Control |
|---|---|---|---|
| Issue #662 no implicit binding | UT | persona/custom/console-access CRBs never reference console-reader | AC-6, CC6.3 |
| Issue #662 explicit bind path | UT | `clusterRoleName` reference binds verbatim with the documented name | AC-3 |
| Issue #662 coarse role shape | UT | one rule, three capabilities, `use` only | AC-6(2) |
| IT-CONSOLE-010 | IT | after a real reconcile with persona bindings and the derived console-access CRB, no CRB in the cluster references console-reader | AC-6, CC6.3 |
| Issue #636 suite | UT | unchanged and green (regression; controller ITs IT-CONSOLE-006/008/009 unchanged) | AC-3/AC-6 |

## Follow-Ups

- **#663** (F2) remains open: user-side local-target Kubernetes get/list
  grants are still administrator-owned and deployment-specific.
- **#665** (F4) remains open and separately fenced: ai-orchestrator
  approval reads.
- **#664** (F3) remains open: raw-MCP `kubernaut_list_alerts` alias.
- If upstream ever ships an authoritative persona mapping, a new DD may
  supersede decision 1 here; the pinning tests would fail loudly on any
  implicit-binding drift in the meantime.

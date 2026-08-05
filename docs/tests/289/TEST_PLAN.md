# IEEE 829 Test Plan — Issue #289: Console-access RBAC gate

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-289                                             |
| **Issue**          | #289 — Operator has no support for kubernaut's new `kubernaut.ai/console` SAR gate (upstream #1919 / kubernaut#1942) |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-08-05                                         |
| **Scope**          | `api/v1alpha1/kubernaut_types.go`, `internal/resources/rbac.go`, `internal/controller/kubernaut_controller.go` |

## 1. Objective

Upstream `kubernaut` PR #1942 adds a new, unconditional server-side
authorization gate on the API Frontend: a `kubernaut.ai/console` `use`
SubjectAccessReview check enforced at both `/mcp` and `/a2a/invoke`,
additive to (not replacing) the existing per-tool `kubernaut.ai/tools`
check. This operator has zero support for it today. Issue #289 has
live-cluster confirmation that a deployment mapping all six personas to a
single custom OIDC group (`platform-engineering`) via
`spec.apiFrontend.rbac.roleBindings` would 403 every console session for
every persona once AF is upgraded, because the operator does not
provision the new `kubernaut-console-access` ClusterRole/CRB at all.

Verify that:

1. `spec.apiFrontend.rbac.consoleAccessGroups` is added as a new optional
   `[]string` field on `APIFrontendRBACSpec`. Unset (nil) defaults to the
   deduplicated union of all groups already present in `roleBindings`, so
   upgrading to an AF version enforcing this gate does not silently deny
   existing deployments' tool calls. An explicit empty list (`[]`) opts
   out (grants console access to nobody). An explicit non-empty list
   gives independent control, ignoring `roleBindings` entirely.
2. A new `kubernaut-console-access` ClusterRole (`kubernaut.ai/console`,
   verb `use`) renders unconditionally whenever AF is enabled, riding the
   existing `ClusterRoles(kn)` aggregator/`deployCoreRBAC`/
   `deleteRBACResources` create and finalizer paths — no new wiring point
   needed for the ClusterRole itself.
3. A new `kubernaut-console-access-binding` ClusterRoleBinding is created
   with one `Group` subject per effective console-access group, via a new
   `deployConsoleAccessRBAC` step in the reconciler's `deployRBAC` chain,
   and is deleted (not just left empty) when the effective group list
   becomes empty (opt-out or misconfiguration recovery).
4. The finalizer path removes the CRB (the ClusterRole is already covered
   by the existing `ClusterRoles(kn)` sweep in `deleteRBACResources`).
5. All pre-existing RBAC/controller tests remain green (additive change;
   CRs that leave `consoleAccessGroups` unset get the derived-default
   behavior, which for CRs with no `roleBindings` at all is an empty
   list — i.e. no console-access CRB is created, matching today's
   pre-#289 behavior of "no groups bound to anything tool-related either").

**Explicitly out of scope**: no changes to the `kubernaut.ai/tools`
per-tool SAR gate or `toolPersonas`; no Helm-side changes (this is the
operator repo only); porting to `main`/v1.6 is tracked as a separate PR
closing #290 per the approved execution order.

## 2. Test Strategy

Standard TDD (RED -> GREEN -> REFACTOR), following the project's Pyramid
Invariant: **UT proves logic, IT proves wiring, E2E proves the journey**.

- UT (`internal/resources/rbac_test.go`): pure-function coverage of the
  three new builders/helper in isolation, mirroring the existing
  `Describe("ToolClusterRoles", ...)` / `Describe("ToolClusterRoleBindings", ...)`
  structure.
- IT (`internal/controller/kubernaut_lifecycle_test.go`, envtest-backed,
  real `k8sClient`): a new `Context("Console Access RBAC Lifecycle", ...)`
  mirroring the existing `Context("SAR Tool RBAC Lifecycle", ...)` pattern
  (`reconcileToDeployPhase`/`reconcileToRunning`/`newCRWithRouteDisabled`).
  GREEN is not complete until these pass — a builder with only the UT
  below would be prototyped, not implemented.
- E2E: no new E2E test needed. `test/e2e/e2e_test.go`'s existing generic
  assertions ("at least 10 ClusterRoleBindings with kubernaut labels",
  "all ClusterRoleBindings cleaned up on CR deletion") already sweep in
  the new ClusterRole/CRB automatically once they exist.

## 3. Test Scenarios

### 3.1 CRD field (`api/v1alpha1`, deepcopy)

| ID     | Description | Automated? |
|--------|--------------|------------|
| CA-001 | `spec.apiFrontend.rbac.consoleAccessGroups` defaults to nil/unset | Yes (`make generate` + build) |
| CA-002 | Deepcopy round-trips the new `[]string` field without aliasing | Yes (`make generate` + build) |

### 3.2 `ConsoleAccessClusterRole` (`internal/resources/rbac_test.go`)

| ID     | Description | Automated? |
|--------|--------------|------------|
| CA-010 | Returns a ClusterRole with apiGroup `kubernaut.ai`, resource `console`, verb `use` when AF is enabled | Yes |
| CA-011 | Name is namespace-prefixed (`<ns>-console-access`) | Yes |
| CA-012 | Returns nil when AF is disabled | Yes |
| CA-013 | Renders even when no groups are bound anywhere (unconditional, matches upstream's always-render behavior) | Yes |

### 3.3 `effectiveConsoleAccessGroups` (`internal/resources/rbac_test.go`)

| ID     | Description | Automated? |
|--------|--------------|------------|
| CA-020 | Returns nil when `spec.apiFrontend.rbac` is nil | Yes |
| CA-021 | Nil `consoleAccessGroups` derives the deduplicated union of all `roleBindings[].groups` | Yes |
| CA-022 | Derivation collapses the #289 exact live scenario: 6 `roleBindings` entries all mapping to `platform-engineering` -> single-element `["platform-engineering"]` | Yes |
| CA-023 | Explicit empty list (`consoleAccessGroups: []`) overrides derivation, returning empty regardless of `roleBindings` content | Yes |
| CA-024 | Explicit non-empty list is used verbatim, ignoring `roleBindings` entirely | Yes |

### 3.4 `ConsoleAccessClusterRoleBinding` (`internal/resources/rbac_test.go`)

| ID     | Description | Automated? |
|--------|--------------|------------|
| CA-030 | Returns nil when AF is disabled | Yes |
| CA-031 | Returns nil when the effective group list is empty | Yes |
| CA-032 | Returns one `Group`-kind subject per effective group, no explicit `APIGroup` set (matches `ToolClusterRoleBindings` convention) | Yes |
| CA-033 | `RoleRef` points to the namespace-prefixed `console-access` ClusterRole with `Kind: ClusterRole`, `APIGroup: rbac.authorization.k8s.io` | Yes |
| CA-034 | CRB name is namespace-prefixed (`<ns>-console-access-binding`), static regardless of group count | Yes |

### 3.5 Controller integration (`internal/controller/kubernaut_lifecycle_test.go`)

New `Context("Console Access RBAC Lifecycle", ...)`:

| ID           | Description | Automated? |
|--------------|--------------|------------|
| IT-CONSOLE-001 | `kubernaut-console-access` ClusterRole exists after `reconcileToDeployPhase` when AF enabled | Yes |
| IT-CONSOLE-002 | CRB created with derived groups matching #289's exact live scenario (6 `roleBindings` entries, all `groups: [platform-engineering]`) after real reconciliation | Yes |
| IT-CONSOLE-003 | CRB reflects an explicit `consoleAccessGroups` override, independent of `roleBindings` | Yes |
| IT-CONSOLE-004 | CRB is absent/removed after updating the spec to `consoleAccessGroups: []` (create-then-delete transition through a second real `Reconcile()` call) | Yes |
| IT-CONSOLE-005 | Finalizer removes both the ClusterRole and the CRB on CR deletion | Yes |
| (regression)   | Full pre-existing `SAR Tool RBAC Lifecycle` context and `internal/controller` suite remain green | Yes |

## 4. Acceptance Criteria

- All scenarios above pass via `make test-unit` and `make test-integration`.
- `make lint` reports 0 new issues.
- `make manifests generate` regenerates `config/crd/bases/kubernaut.ai_kubernauts.yaml`,
  `bundle/manifests/*`, and `dist/install.yaml` with the new field, with no
  other unrelated diff.
- `CHANGELOG.md` `[Unreleased]` updated under Added, referencing #289 and
  the upstream gate.
- No behavior change for CRs that leave `consoleAccessGroups` unset and
  have no `roleBindings` either (fully backward compatible: no CRB is
  created, same as today).

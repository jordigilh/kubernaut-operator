# DD-278: SRE Persona Approval-Visibility Tools (Interim)

**Status**: Accepted
**Decision Date**: 2026-08-02
**Version**: 1.0
**Confidence**: 97%
**Deciders**: Kubernaut Operator Team
**Applies To**: `internal/resources/rbac.go` `toolPersonas` (`tool-sre`)

**Related Issues**:
- jordigilh/kubernaut-operator#278 (this repo, `release/v1.5`, fixed)
- jordigilh/kubernaut-operator#280 (this repo, `main`/v1.6 port, tracked)
- jordigilh/kubernaut#1869 (Helm chart twin, `main`/v1.6)
- jordigilh/kubernaut#1827 (`kubernaut_complete_no_action` gap, first flagged)
- jordigilh/kubernaut#1235 / jordigilh/kubernaut#1239 (original least-privilege design for approval tools)

---

## Changelog

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-08-02 | Operator Team | Initial decision |

---

## Context & Problem

kubernaut#1235/#1239 established that approval-decision tools (`kubernaut_approve`,
`kubernaut_list_approval_requests`, `kubernaut_get_approval_request`) should be
scoped to the `remediation-approver` persona only, per least-privilege /
separation-of-duties: approval is meant to be a human action scoped to a
dedicated approver role, not to `sre`.

That design was never fully carried out: `kubernaut_approve` was never removed
from `sre` in either the upstream Helm chart (`charts/kubernaut/values.yaml`)
or this operator's `internal/resources/rbac.go`. In practice `sre` has always
been able to approve/reject a `RemediationApprovalRequest` (RAR).

Live E2E testing against a real cluster (#278, 2026-08-02) confirmed the
resulting inconsistency: `sre` can call `kubernaut_approve` but not
`kubernaut_get_approval_request`, so kubernaut-console's Approve/Decline card
can never render the RAR details (confidence, reason, recommended workflow,
evidence) needed to make that decision. The approval gate is silently
unusable end-to-end for the only persona that can reach it.

`kubernaut_complete_no_action` — the console-only dismiss/escalate tool
(`DD-AF-007`), unrelated to the approval decision path — was also found
missing from every persona in this repo (and upstream `values.yaml`).

## Why `sre` Retains These Tools (Not `remediation-approver`-Only)

kubernaut-console currently ships a chatbox-only interface: there is no
separate management UI or login path that lets a user authenticated as
`remediation-approver` navigate to a pending RAR and act on it independently
of the `sre`-driven chat flow. `sre` is, today, the only
console-interactive persona. Restricting `kubernaut_get_approval_request` to
`remediation-approver` (completing the original #1239 design) would not move
approval decisions to the intended role — it would make them unreachable by
any real user, since nothing yet lets `remediation-approver` act on its own.

## Decision

1. Add `kubernaut_get_approval_request` and `kubernaut_complete_no_action` to
   the `tool-sre` persona in `internal/resources/rbac.go`.
2. Treat this as an **interim** state, not a reversal of #1235/#1239's
   least-privilege intent. It is purely additive: `sre` already held
   `kubernaut_approve`, so this only makes an already-possible decision an
   *informed* one.
3. `kubernaut_complete_no_action` is not added to `tool-ai-orchestrator` or
   other non-console personas — it is Console-only per `DD-AF-007` and must
   not appear in the A2A agent toolset.

## Revisit Trigger

Once kubernaut-console ships a management UI/login path that lets
`remediation-approver` view and act on RARs independently of the `sre`
chatbox, re-evaluate whether `kubernaut_approve` and the approval-visibility
tools should be removed from `sre` to complete the original #1239
least-privilege design.

## Alternatives Considered

| Option | Description | Rejected Because |
|---|---|---|
| A (chosen) | Add `kubernaut_get_approval_request` + `kubernaut_complete_no_action` to `tool-sre` | — |
| B | Remove `kubernaut_approve` from `tool-sre` instead, keep approval tools `remediation-approver`-only | Would make approvals unreachable by any real user today — no UI path exists for `remediation-approver` to act independently |
| C | Do nothing, treat as won't-fix | Leaves the approval gate silently broken end-to-end for the only persona that can reach it (confirmed live on a v1.5.7 cluster) |

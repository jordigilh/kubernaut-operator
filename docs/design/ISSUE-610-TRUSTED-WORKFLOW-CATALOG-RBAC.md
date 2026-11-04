# Issue #610 — Trusted workflow catalog RBAC

## Decision

Trusted workflow catalog access is provisioned with namespace-scoped Roles and
RoleBindings in the configured Kubernaut controller namespace (`kn.Namespace`):

| ServiceAccount | Resources | Verbs |
| --- | --- | --- |
| `kubernaut-agent-sa` | `remediationworkflows`, `actiontypes` | `list`, `watch` |
| `authwebhook` | `remediationworkflows` | `list` |
| `workflowexecution-controller` | `remediationworkflows` | `list` |

The workflow-execution namespace is not used for this catalog access.

KA's obsolete cluster-wide `remediationworkflows` and `actiontypes` reads are
removed from its investigator ClusterRole. AuthWebhook's existing broad
ClusterRole is preserved because it also provides workflow registration,
status, finalizer, ActionType, event, and other controller permissions. The
new AuthWebhook Role documents the trusted selector-list path but does not
narrow the preserved ClusterRole; effective foreign-namespace access remains
allowed for AuthWebhook by design.

## Wiring and evidence

The Roles and RoleBindings use the existing namespaced RBAC reconciliation path
(`deployNamespacedCoreRBAC`) and standard operator ownership metadata. Core
namespaced RBAC is reconciled before ClusterRoles so a failed replacement write
does not remove KA's existing cluster-wide catalog access during an upgrade.
Tests cover rendered objects, reconciled objects in standard and non-default
controller namespaces, injected Role/RoleBinding failures, and
SubjectAccessReview authorization:

- KA and WorkflowExecution: trusted namespace allowed, foreign namespace denied
- KA ActionType list/watch: trusted namespace allowed, foreign namespace denied
- AuthWebhook: trusted and foreign workflow access explicitly allowed through
  the preserved ClusterRole

## Qualification boundary

This operator change must be coordinated with upstream kubernaut issues #2570
and #2577. Production qualification requires an immutable upstream release that
contains trusted namespace cache scoping, direct selector-list verification, and
the `RemediationWorkflow` CRD selectable field for `.status.workflowId`. Passing
operator unit, envtest, and Helm tests does not establish that release or prove
deployed-cluster application behavior.

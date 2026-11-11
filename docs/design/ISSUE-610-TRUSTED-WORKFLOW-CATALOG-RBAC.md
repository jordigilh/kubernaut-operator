# Issues #610 and #624 — Trusted workflow catalog RBAC

## Decision

Trusted workflow catalog access is provisioned with namespace-scoped Roles and
RoleBindings in the configured Kubernaut controller namespace (`kn.Namespace`):

| ServiceAccount | Resources | Verbs |
| --- | --- | --- |
| `kubernaut-agent-sa` | `remediationworkflows`, `actiontypes` | `list`, `watch` |
| `authwebhook` | `remediationworkflows` | `list` |
| `workflowexecution-controller` | `remediationworkflows` | `list` |
| `aianalysis-controller` | `remediationworkflows` | `list` |
| `remediationorchestrator-controller` | `remediationworkflows` | `get`, `list` |

The workflow-execution namespace is not used for this catalog access.

The AIA and RO grants are intentionally asymmetric. AIA only discovers the
catalog, so it receives `list` and no object reads or writes. RO performs both
catalog discovery and trusted-workflow override resolution; the latter calls a
`RemediationWorkflow` `Get`, so RO receives `get,list` and no watch, mutation,
status, or finalizer access. Both grants are namespace-scoped to `kn.Namespace`;
they do not grant cluster-wide or `actiontypes` access.

KA's obsolete cluster-wide `remediationworkflows` and `actiontypes` reads are
removed from its investigator ClusterRole. AuthWebhook's existing broad
ClusterRole is preserved because it also provides workflow registration,
status, finalizer, ActionType, event, and other controller permissions. The
new AuthWebhook Role documents the trusted selector-list path but does not
narrow the preserved ClusterRole; effective foreign-namespace access remains
allowed for AuthWebhook by design.

During an upgrade, the old KA cluster-wide catalog rule is retained until the
replacement namespaced RBAC succeeds. AIA and RO have no legacy fallback: a
Role or RoleBinding provisioning error leaves their access denied and blocks
service-workload deployment. A successful same-CR retry reconciles each
catalog Role/RoleBinding pair and then allows normal cluster-RBAC reconciliation
to remove the obsolete KA rule while retaining the namespaced KA grant.

## Wiring and evidence

The Roles and RoleBindings use the existing namespaced RBAC reconciliation path
(`deployNamespacedCoreRBAC`) and standard operator ownership metadata. Core
namespaced RBAC is reconciled before ClusterRoles so a failed replacement write
does not remove KA's existing cluster-wide catalog access during an upgrade.
Catalog grants are reconciled as Role/RoleBinding pairs so the failure boundary
is observable: an RO failure can leave a completed AIA grant usable, while the
reconciler still blocks workloads until all grants succeed. Tests cover rendered
objects, reconciled objects in standard and non-default controller namespaces,
injected Role/RoleBinding failures, and SubjectAccessReview authorization:

- KA and WorkflowExecution: trusted namespace allowed, foreign namespace denied
- KA ActionType list/watch: trusted namespace allowed, foreign namespace denied
- AIA: trusted workflow list allowed; get/watch/mutation/status/finalizer and
  foreign or cluster-wide access denied
- RO: trusted workflow get/list allowed; watch/mutation/status/finalizer and
  foreign or cluster-wide access denied
- AuthWebhook: trusted and foreign workflow access explicitly allowed through
  the preserved ClusterRole

## Qualification boundary

Issue #624 local evidence is limited to the rendered Role/RoleBinding contract,
real reconciler/envtest authorization, and the operator's Helm path. Deployed
qualification remains a separate upstream gate linked to kubernaut #2575 and
application PR #2613: the operator-backed FullPipeline lane must install an
immutable operator artifact and prove the live AIA/RO ServiceAccount identity,
TokenRequest/TokenReview identity, direct authorization, and RO override path.
The upstream workflow/release automation prerequisite is kubernaut #2488. It is
intentionally deferred until the project has a mature release process that can
automate the qualification controls; current releases remain manual and
human-driven. Passing operator unit, envtest, or Helm tests alone does not
establish automated deployed-cluster qualification.
OLM/bundle qualification is deferred to kubernaut-operator #501.

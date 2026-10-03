# DD-457: Declarative Fleet caller RBAC uses fixed OIDC groups

**Status**: Accepted
**Decision Date**: 2026-10-03
**Applies To**: `internal/resources/rbac.go`, core cluster-scoped RBAC reconciliation, Fleet installation documentation
**Related Issue**: `jordigilh/kubernaut-operator#457`

## Context

Fleet MCP calls reach a target Kubernetes API server through RFC 8693 token
exchange. The target server authorizes the exchanged caller identity, not the
`kube-mcp-server` ServiceAccount. The operator manages the hub but has no
controller on a spoke, so the hub-side RBAC should be declarative while spoke
RBAC remains a documented bootstrap prerequisite.

The existing Fleet credentials have different security scopes:

- `spec.fleet.oauth2.credentialsSecretRef` is the shared read credential used
  by Fleet-aware services.
- `spec.workflowExecution.fleet.oauth2CredentialsSecretRef` is
  WorkflowExecution's dedicated write credential and intentionally has no
  fallback to the shared Secret (DD-235).

WorkflowExecution needs both read access and write access. Giving every Fleet
caller the write grant would defeat that credential separation.

## Decision

The operator creates two fixed, documented Kubernetes group subjects when
remote MCP access is enabled (`fleet.enabled=true` and
`mcpGatewayEndpoint` is set):

| Kubernetes group subject | Purpose | Bound permissions |
|---|---|---|
| `oidc:kubernaut-fleet-read` | All read-only Fleet callers | The cluster's `view` role plus a node-reader role (`nodes`: `get`, `list`, `watch`) |
| `oidc:kubernaut-fleet-workflow-execution` | Dedicated WorkflowExecution caller | Jobs and Tekton PipelineRuns lifecycle operations; Tekton TaskRun `get` |

The `oidc:` prefix is part of the fixed Kubernetes subject. Every cluster's
OIDC configuration must map the IdP `groups` claim with that prefix. The IdP
group claims without the Kubernetes prefix are:
`kubernaut-fleet-read` and `kubernaut-fleet-workflow-execution`.

WorkflowExecution's IdP identity belongs to both groups. Other Fleet
identities belong only to the read group. The operator does not infer or
discover opaque `sub` values and does not create User-scoped bindings.

The role and binding names are namespace-prefixed using the existing operator
convention (`<operator-namespace>-fleet-caller-*`). The objects are returned
by `ClusterRoles()` and `ClusterRoleBindings()`, receive the core RBAC label,
and therefore use the existing generic reconcile/prune and finalizer cleanup
paths.

No new CRD field is added. In particular, caller-group and role-name
overrides are intentionally not exposed: they would make a security-critical
identity-to-permission mapping easy to misconfigure and would require a
second API contract for every spoke bootstrap package.

## Alternatives considered

### Bind the exchanged `sub` as a User

Rejected. The original implementation required live token exchange and JWT
decoding to discover an opaque client UUID. It is imperative, difficult to
reproduce in GitOps, and does not provide a stable documented contract.

### Add `spec.fleet.callerRBAC.group` and configurable role names

Rejected. It adds mandatory deployment knowledge to the CR, permits accidental
binding of an execution identity, and does not solve spoke-side reconciliation.
Fixed names are sufficient for the product's supported OIDC setup.

### Use one combined Fleet caller group

Rejected. Read-only services would receive WorkflowExecution write access.
The two-group model preserves least privilege while allowing WorkflowExecution
to be a member of both groups.

### Manage only the hub

Accepted as the operator boundary, not as a complete fleet bootstrap. The
operator owns hub-side RBAC; the installation guide supplies equivalent
spoke-side manifests until a future remote reconciliation mechanism exists.

## Verification

- Resource unit tests verify conditional rendering, fixed group subjects, role
  rules, and disabled-state omission.
- Controller envtest verifies hub creation and pruning after Fleet remote access
  is disabled.
- Existing WorkflowExecution credential tests continue to verify that its
  dedicated Secret is mounted and never falls back to the shared Fleet Secret.

# Runtime resource ownership and safe migration

The operator does not adopt a resource just because its name matches a desired
Deployment, Secret, ConfigMap, RBAC object, webhook, Ingress or other runtime
object. It checks ownership even when the stored spec hash matches.

## Authorized resources

- Same-namespace objects: the current `Kubernaut` controller owner reference,
  or a complete matching operator marker with no foreign/contradictory owner.
- Cluster-scoped and cross-namespace objects: all three labels below, no owner
  references, and no contradictory namespace provenance.

```yaml
labels:
  app.kubernetes.io/managed-by: kubernaut-operator
  app.kubernetes.io/part-of: kubernaut
  app.kubernetes.io/instance: kubernaut
annotations:
  kubernaut.ai/owner-namespace: <Kubernaut CR namespace>
  kubernaut.ai/owner-uid: <Kubernaut CR UID>
```

The operator records the provenance annotations on creation/repair. Fully
marked legacy operator resources without annotations remain a bounded upgrade
exception. A new CR UID can repair only fully marked leftovers of the same
identity. A foreign owner, partial marker, different namespace identity or
terminating object is not adoption permission.

These markers express administrator intent, not cryptographic ownership. Limit
who can modify them through Kubernetes RBAC/admission.

## Resolving an ownership conflict

1. Read the CR conditions, `OwnershipConflict` Warning events and operator logs
   for the kind, namespace, name, observed owner/markers and versions.
2. Back up the object and identify its actual administrator/platform owner.
   Do not print Secret data into shared logs or issue reports.
3. Prefer changing the external deployment/name or removing a genuinely obsolete
   resource after approval. Do not overwrite a live platform resource.
4. If transferring ownership is intentional, review the rendered desired object,
   namespace identity, security policy and existing owner first. Record approval
   and explicitly prepare the object for the contract above. Never bulk-label
   all Secrets, RBAC or CRDs, or strip another controller's owner reference just
   to suppress an error. The operator may replace its managed content after a
   reviewed transfer.
5. Reconcile again and verify current-generation readiness. Already-created
   authorized resources are retryable; installation is not an atomic transaction.

Foreign stale/disabled/cleanup targets are preserved and logged, rather than
deleted or used to wedge the CR finalizer. Deletion of authorized resources
uses both UID and resourceVersion preconditions to reject a stale observation.

## Shared operand CRDs

The `kubernauts.kubernaut.ai` bootstrap CRD remains installation/packaging-owned.
The embedded **operand** CRDs are shared cluster resources: they are not deleted
with an instance, and existing schemas require explicit operator ownership
before an update. Unmarked old schemas and Helm-owned schemas are not
transparently adopted.

For each operand CRD, export/back up its schema and custom resources, review
compatibility against the operator's embedded version, coordinate with its
current owner, and approve transfer individually. Only then set
`app.kubernetes.io/managed-by=kubernaut-operator` on that reviewed operand CRD.
Existing foreign owner references still block the transfer. Schema migration
and release qualification are separate from merely changing the label.

## Workflow namespaces and external inputs

A pre-existing administrator workflow namespace is reused **read-only** only
when all restricted PSA labels already exist and it has no operator identity
claim, owner references or termination. Restricted PSA labels do not override
conflicting operator provenance. Prepare the namespace as an administrator:

```bash
kubectl label namespace "$WORKFLOW_NAMESPACE" \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/audit=restricted \
  pod-security.kubernetes.io/warn=restricted --overwrite
```

Reusing that namespace does not entrust the namespace itself to the operator;
same-named workflow RBAC/runner accounts still need their own ownership checks.
The CR/deployment namespace (`kubernaut-system` by default) must already exist
with provisioned DB/Valkey and administrator Secret inputs. The operator may
create a missing workflow namespace and stamp provenance, but **never deletes
either namespace**. Workflow artifacts belong to Kubernaut provisioning and are
outside the operator's cleanup scope. On uninstall it removes only individually
authorized operator resources, including its workflow Roles/Bindings/accounts,
without starting namespace termination or sweeping other contents. Namespace
ownership and the `kubernaut.ai/created-by` annotation do not authorize cascade
deletion. Any namespace removal is a separate administrator/provisioning action
after reviewing and backing up its contents; it is not an uninstall step.

Administrator credentials, policy/runtime input ConfigMaps, external TLS/issuers
and cert-manager output Secrets remain inputs, not adoption targets. Manual TLS
does not imply permission to take over webhook configurations; explicitly
entrust the configuration while retaining administrator-owned TLS material.
Native provider policies retain their separate managed-policy, managed-by,
policy-namespace, instance and provider checks; generic markers cannot replace
them. A foreign/invalid-scope owner or termination vetoes mutation/deletion even
when all five markers and the hash match. A controller reference to the same
namespaced Kubernaut identity may be repaired on a marked reinstall; cluster-scoped
policies cannot carry a namespaced owner reference.

For API migration, see [the v1alpha1-to-v1alpha2 guide](upgrade-v1alpha1-to-v1alpha2.md).
The precise decision and compatibility exceptions are recorded in
[the #514 contract](design/ISSUE-514-OWNERSHIP-CONTRACT.md).

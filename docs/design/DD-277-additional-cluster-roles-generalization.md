# DD-277: Generalize `additionalClusterRoleBindings`, shrink built-in owner-chain rules, and fix orphaned-CRB leaks

**Status**: Accepted
**Decision Date**: 2026-08-15
**Applies To**: `api/v1alpha2.KubernautSpec`/`KubernautAgentSpec`, `internal/resources/rbac.go`, `internal/controller/kubernaut_controller.go`
**Related Issues**:
- jordigilh/kubernaut-operator#277 (this decision)
- jordigilh/kubernaut-operator#341 (the orphaned-CRB pruning mechanism this decision's cleanup logic builds on)
- [kubernaut DD-GATEWAY-018](https://github.com/jordigilh/kubernaut/blob/main/docs/architecture/decisions/DD-GATEWAY-018-owner-chain-rbac-extensibility.md) (upstream Helm-chart precedent this generalizes)

## Context

`v1alpha1.KubernautAgentSpec.AdditionalClusterRoleBindings` let a cluster
administrator list pre-existing ClusterRole names that the operator would
bind, one ClusterRoleBinding per entry, to the Kubernaut Agent (KA)
ServiceAccount only. The name was also misleading: the field holds ClusterRole
*names*, and the operator creates the *bindings* automatically — nothing
about "bindings" is supplied by the caller.

Reviewing the field while auditing v1.6 gaps surfaced two problems:

1. **Scope was KA-only, but the need is not.** KA performs owner-chain
   resolution for local (non-fleet) remediation. Gateway and
   EffectivenessMonitor (EM) perform the *same* owner-chain resolution for
   fleet-mode signals and effectiveness assessment respectively — they
   traverse the same ecosystem CRDs (Knative Services, Strimzi Kafka topics,
   custom application CRDs) for the same reason. There is no legitimate case
   for Gateway or EM to see a different set of ecosystem CRDs than KA does;
   restricting the mechanism to KA just meant Gateway/EM silently couldn't
   resolve owner chains through anything the cluster admin hadn't already
   granted via KA's grant, with no way to grant it to them directly.
2. **The operator never cleaned up after itself.** Neither the per-role CRBs
   created from this field, nor a large family of other conditionally-gated
   cluster-scoped RBAC (ClusterRoles/ClusterRoleBindings for FMC, Gateway,
   console-access, etc.), were pruned when an entry was removed from the
   spec, a feature was disabled, or the Kubernaut CR itself was deleted.
   #341 (filed and fixed in the same v1.6 gap-closure pass) addresses the
   *mechanism* for this — a generic label-selector list-diff-delete pass
   instead of a family of hand-maintained, easily-missed static-name delete
   functions; this decision's generalized `AdditionalComponentCRB` builder
   and `pruneOrphanedAdditionalComponentRBAC` reuse that same mechanism for
   the newly-multi-component additional-role bindings specifically.

Separately, `internal/resources/rbac.go`'s `ownerChainResolutionRules()` (the
rule set literally embedded in Gateway's and EM's built-in ClusterRoles) had
grown to unconditionally grant read access to OLM, Istio, Linkerd,
cert-manager, ArgoCD, OpenShift Routes, and KubeVirt/CDI CRDs — every
ecosystem Kubernaut had ever needed to correlate an owner chain through,
regardless of whether a given cluster actually runs any of them. This
violated least-privilege (SC-7/AC-6): a cluster running none of those
ecosystems still had ClusterRoles granting read access to all of their CRDs.
It was also unbounded — every new ecosystem meant another hardcoded rule
here rather than an opt-in grant.

KA's own `kubernaut-agent-investigator` ClusterRole is a separate, much
broader, hardcoded rule set (unrelated to `ownerChainResolutionRules()`) that
already includes all of the above ecosystems plus OCP platform/machine/
network-operator resources — this decision does not touch it. KA's role is
intentionally broad because investigation, not owner-chain correlation, is
its job; Gateway/EM's roles are not.

## Decision

1. **Relocate and generalize the field.** Remove
   `v1alpha2.KubernautAgentSpec.AdditionalClusterRoleBindings`; add
   `v1alpha2.KubernautSpec.AdditionalClusterRoles []string` at the top level.
   `v1alpha1.KubernautAgentSpec.AdditionalClusterRoleBindings` is preserved
   unchanged (v1alpha1 is a served compatibility view over the v1alpha2
   storage version, per `ADR-CRD-001`); its conversion functions now map to
   and from v1alpha2's top-level field, so existing v1alpha1 manifests keep
   working without changes, but their listed roles now also bind to
   Gateway/EM on the v1alpha2 side (see Consequences).

2. **Shrink `ownerChainResolutionRules()`** to only the two kinds every
   deployment topology can genuinely need regardless of what runs on the
   cluster: `policy/poddisruptionbudgets` and
   `networking.k8s.io/{networkpolicies,ingresses}`. Every ecosystem-specific
   grant it used to carry (OLM, Istio, Linkerd, cert-manager, ArgoCD,
   OpenShift Routes, KubeVirt/CDI) is removed; a cluster that runs one of
   those ecosystems and wants Gateway/EM's owner-chain resolution to see it
   now grants it via `spec.additionalClusterRoles` — the same mechanism KA
   already used pre-#277, generalized.

3. **Generalize the RBAC builders and controller wiring.**
   `AdditionalAgentCRBName`/`AdditionalAgentCRB` become
   `AdditionalComponentCRBName`/`AdditionalComponentCRB`, taking a
   `component string` parameter so the same ClusterRole name produces
   distinct, collision-free CRB names per component (e.g.
   `<ns>-kubernaut-agent-ext-<hash>` vs `<ns>-gateway-ext-<hash>`).
   `LabelAdditionalAgentRBAC` becomes `LabelAdditionalComponentRBAC`. The
   controller's `deployAdditionalComponentRBAC` computes the desired
   (component × role-name) CRB set across KA and EM (always) and Gateway
   (only while `spec.gateway.enabled=true`), and
   `pruneOrphanedAdditionalComponentRBAC` — mirroring #341's
   `pruneOrphanedCoreClusterRBAC` pattern, and sharing its generic
   `namesOf`/`pointersOf`/`pruneUndesired` list-diff-delete helpers — prunes
   anything no longer desired, whether because a role name was removed from
   the spec or a component (Gateway) was disabled.

## Alternatives Considered

1. **Keep the field KA-only, add a separate Gateway/EM-specific field.**
   Rejected: there is no case where Gateway/EM legitimately need a
   *different* set of ecosystem ClusterRoles than KA — they resolve the same
   owner chains. A second field would just be more API surface for
   operators to keep in sync for no behavioral benefit.
2. **Keep `ownerChainResolutionRules()`'s full ecosystem list and only
   generalize the additional-roles mechanism.** Rejected on review: leaving
   the built-in list as-is perpetuates the least-privilege violation this
   decision was also meant to close, and having *both* a broad built-in
   grant and an opt-in mechanism for the same ecosystems is redundant API
   surface.
3. **Status-diff-based pruning (compare against
   `kn.Status.BoundAdditionalClusterRoles`) generalized to multi-component,
   instead of a label-selector list-diff.** Rejected: the status field only
   ever tracked role *names*, not which components they were bound to, so a
   status-diff approach couldn't correctly detect a component-level
   change (e.g. Gateway being disabled) without additional status fields.
   The label-selector approach used for #341 already solves this generically
   and was reused rather than inventing a second pruning strategy.

## Consequences

- **Breaking change, `v1alpha2` only.** `v1alpha2.KubernautAgentSpec` loses
  `additionalClusterRoleBindings`; any `v1alpha2` manifest using it must move
  the value to top-level `spec.additionalClusterRoles`. `v1alpha1` is
  unaffected and continues to accept the field at its old location.
- **Upgrade behavior change, not just a rename.** A `v1alpha1` CR using
  `spec.kubernautAgent.additionalClusterRoleBindings` will, once converted to
  `v1alpha2` and reconciled by an operator carrying this change, also bind
  the listed ClusterRole(s) to EM and (if enabled) Gateway — not just KA.
  This is called out explicitly in
  `docs/installation/02-configure-services.md` as a migration note: review
  listed ClusterRoles for whether Gateway/EM holding them too is
  appropriate.
- **Least-privilege improvement for Gateway/EM on fresh v1.6 installs.**
  Clusters that don't grant any `additionalClusterRoles` no longer get
  Gateway/EM ClusterRoles carrying OLM/Istio/cert-manager/ArgoCD/Routes/
  KubeVirt CDI read access by default — only PDB and core networking.
  KA's own investigator role is unaffected (out of scope, see Context).
- New integration coverage
  (`internal/controller/additionalcomponentrbac_test.go`) proves, through
  envtest: a role bound via the legacy `v1alpha1` nested field is bound to
  all three components on the `v1alpha2` side; disabling Gateway prunes only
  Gateway's CRB while KA/EM's remain; and CR deletion prunes all
  additional-component CRBs via the finalizer.

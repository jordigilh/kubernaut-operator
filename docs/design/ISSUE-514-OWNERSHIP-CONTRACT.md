# Issue #514 — Resource ownership and adoption contract

**Status:** approved contract implemented; final validation recorded in
`docs/tests/514/VALIDATION.md`. Implementation-to-commit handoff, not release approval.
**Base:** `origin/main` at `fa46f90` (includes merged #515)
**Scope:** #512 Helm-parity P0 safety gap; no release/qualification claim

## Preflight and decision

The generic `ensureResource` path, including its AlreadyExists race, did not
authorize existing objects before update or before the hash fast path. The
cert-manager path and migration-Job path have independent write/status paths.
Static-name cleanup and several label-selector sweeps can delete foreign
objects. `resources.EnsureCRDs` also updates shared operand CRDs through a
dynamic client. Provider policies already have a separate multi-label guard.
All of these seams are included; the operator's own bootstrap CRD/RBAC/webhook
remain packaging-owned and are not runtime-managed here.

Alternatives considered:

1. **Owner UID only:** simplest for same-namespace objects, but cannot represent
   cluster-scoped/cross-namespace ownership or recover marked reinstall leftovers.
2. **Matching operator identity with guarded reinstall (selected):** current
   controller ownership or the complete explicit operator marker, no foreign
   owners, and matching namespace identity. Preserves upgrades and a narrowly
   bounded reinstall while refusing implicit Helm/admin adoption.
3. **New adoption CRD field:** broadens the public API and increases takeover
   risk; unnecessary for this release-critical safety fix.

Confidence before RED: **96%**. Remaining risk is real-cluster qualification;
envtest does not run garbage collection, namespace lifecycle, or providers.

## Runtime ownership contract

An existing managed object is eligible for mutation only when:

- It has the current CR's controller owner reference (same Kubernaut API group,
  name, namespace and UID), with no contradictory ownership markers or foreign
  owner references; **or**
- Its complete explicit marker matches `app.kubernetes.io/managed-by=
  kubernaut-operator`, `app.kubernetes.io/part-of=kubernaut`, and
  `app.kubernetes.io/instance=<CR name>`. A missing controller reference or stale
  reference to the *same* Kubernaut identity is eligible for reinstall repair.
  Any different owner identity/controller is rejected, even with matching labels.

New and repaired resources additionally record `kubernaut.ai/owner-namespace`
and `kubernaut.ai/owner-uid`. A namespace marker that points elsewhere is always
rejected. Old operator resources without these annotations remain eligible only
through the complete legacy marker or current owner reference. This is a
bounded compatibility exception, **not** automatic adoption of unmarked objects.
Reconciliation receives the current singleton CR from the API: a stale UID for
the same name in the same namespace cannot simultaneously identify a live CR.
Cross-namespace and cluster-scoped resources must have no owner references;
they use the explicit identity marker instead of invalid Kubernetes references.

Ownership is checked **before** hash equality, use of migration Job status,
metadata merges, mutation, or deletion, including create races. A spec hash,
deterministic name, field manager, or partial marker is not authorization.
Updates retain Kubernetes resourceVersion concurrency protection. Deletes use
both UID and resourceVersion preconditions after checking the live object, so a
replacement or ownership change cannot be deleted using a prior authorization.

### Exceptions and read-only inputs

| Resource group | Contract |
|---|---|
| Same-namespace workloads, Services, derived Secrets/ConfigMaps, RBAC, PDB/HPA, exposure and monitoring | Current controller reference or complete matching marker; repair references for a marked reinstall; garbage collected with CR |
| Cluster-scoped RBAC/webhooks and cross-namespace workflow/MCP RBAC/SA | Complete marker; no owner references; explicit finalizer cleanup |
| Operator-created workflow namespace | Ownership-checked PSA convergence; retained on uninstall to avoid namespace-wide cascading deletion of unmarked content |
| Pre-existing administrator workflow namespace | Read-only reuse only when restricted enforce/audit/warn PSA labels are already set and there is no operator identity claim, owner reference or termination; otherwise actionable conflict, not an implicit label update or namespace adoption. Restricted PSA never overrides conflicting operator provenance |
| Shared operand CRDs embedded in the operator | Explicit `app.kubernetes.io/managed-by=kubernaut-operator`; no foreign owner; never deleted with an instance. Old/unmarked or Helm-owned CRDs require an administrator-reviewed ownership transfer or clean-install boundary before schema updates |
| Native provider policies | Existing managed-policy, managed-by, policy-namespace, instance and provider predicates remain mandatory; never relaxed to the generic marker. Foreign/invalid-scope owners and termination veto updates/deletes; only the same namespaced Kubernaut controller identity may be repaired on marked reinstall |
| Administrator credentials, runtime/policy input ConfigMaps, external TLS/issuers, cert-manager output Secrets, platform CRDs/controllers | Read-only inputs; never generic adoption/cleanup targets. Hook/development generated TLS is ownership-checked before even reusing its key material |

## Lifecycle and migration boundary

- **Clean install:** create missing desired objects with ownership metadata.
- **Upgrade:** current owners and fully marked legacy operator resources update
  idempotently; backfill namespace/UID metadata. Ownership checks still run when
  the spec hash matches. Preserve webhook CA injection and cert-manager metadata.
- **Reinstall:** a new CR UID can reclaim only the same-identity, fully marked
  leftovers above. Unmarked, foreign-owner and different-namespace objects block
  install; do not erase/steal them. Terminating objects are not re-adopted.
- **Conflict/failed reconciliation:** conflicting object data/spec, labels,
  annotations, owner references and resourceVersion remain unchanged. Record
  kind, namespace, name, owner/marker details and CR generation/resourceVersion;
  emit an actionable Warning event and mark existing readiness status false.
  No new condition type or phase is introduced. Already-created owned resources
  remain safely retryable; this is not an atomic cross-resource transaction.
- **Uninstall/disable/prune:** skip and log foreign resources rather than delete
  them or wedge the finalizer. Remove eligible resources with identity/version
  preconditions. Never delete shared operand CRDs, workflow namespaces or external
  dependencies. Retain workflow artifacts for administrator-reviewed removal.
- **Helm/v1alpha1 migration:** no transparent takeover. Export/review/recreate
  according to `docs/upgrade-v1alpha1-to-v1alpha2.md`. For an existing shared
  operand CRD, review schema compatibility and transfer ownership explicitly
  before the operator updates it; do not bulk-label all cluster CRDs. Kubernetes
  administrators authorized to change ownership metadata are trusted; markers
  are not protection against a malicious cluster administrator.

## TDD execution plan

| Phase | Work | Estimate |
|---|---|---|
| DISCOVERY | Map live callers, separate provider/CRD/TLS/namespace paths, check type definitions and existing test fixtures | 45–60 min |
| RED | Real Reconcile conflict/failure/uninstall tests first; pure ownership truth table and shared CRD API tests; create/replacement race tests; live Kind journey scenario | 60–90 min |
| GREEN | Minimal shared ownership predicates; wire every write/delete seam; preserve provider and external-input protections | 90–150 min |
| REFACTOR | Centralize identity stamping/audit traces/preconditioned deletion, align fixtures with the approved contract, review all production mutations | 45–75 min |
| VERIFY | Full independent UT/IT coverage, build/lint/manifests/pyramid/security gates; live Kind if the runtime is available | 45–90 min plus E2E |

### Wiring manifest / CHECKPOINT W

| Component | Production entry point | Wiring code location | IT ID |
|---|---|---|---|
| Pure ownership predicate and identity stamping | `Reconcile` → resource helpers | `internal/resources/ownership.go`; `ensureNamespaced`, `ensureUnowned`, `ensureResource` | `IT-OWN-514-001`, `002`, `003`, `004` |
| Safe deletion / namespace retention | `Reconcile` → disable/prune/finalizer | `deleteIfExists`, `pruneUndesired*`, migration deletes, `deleteWorkflowResources`, provider cleanup | `IT-OWN-514-005`, `006`, `010`, `011`, `015` |
| Non-generic resource guards | TLS/migration/namespace phases | `ensureCertManagerResource`, `createIfNotFound`, `ensureDevelopmentSelfSignedTLS`, `ensureWebhookConfiguration`, `deployWorkflowNamespace`, `clearStaleServingCertErrors` | `IT-OWN-514-007`, `008`, `012`, `016`, `017`, `018` |
| Shared operand CRD guard | `phaseMigrate` → `EnsureCRDs` | `internal/resources/crds.go` dynamic API path | `IT-OWN-514-013` plus `UT-OWN-514-004` API truth table |
| Native policy authorization | `Reconcile` → native policy update/prune/finalizer | `internal/policy/ownership.go`; `providerPolicyOwnershipError`, `updateProviderPolicy`, `pruneProviderPolicies`, `deleteProviderPolicies`, `deleteObservedProviderPolicy` | `IT-OWN-514-019` plus independent `UT-OWN-514-005` |
| Conflict observability | Real reconciliation failure | `Reconcile` and ownership-error helper, existing status conditions/events | `IT-OWN-514-001`, `002` |

GREEN is incomplete until pure UT **and actual envtest Reconcile IT** pass.
E2E must drive the installed operator/CR, not call a builder or reconciler.
No pending/skipped tests are added.

## Success criteria and risk mitigation

- No production update/adoption or cleanup bypass for runtime-owned resources.
- Business scenarios in `docs/tests/514/TEST_PLAN.md` have independent UT, IT,
  and `E2E-OWN-514-001` journey evidence.
- `go build ./...`, `golangci-lint run`, `make test`, `make manifests generate`,
  `git diff --exit-code config/`, compile-only tests and anti-pattern checks pass.
- New pure ownership entry points have full branch tests; do not reduce existing
  coverage thresholds or bypass the test pyramid to obtain a green result.
- CRD schema/RBAC/OLM bundle changes: **none**. Generated artifacts must not drift.
- Conservative conflicts may surface unsafe old installs: document precise
  remediation, never weaken the guard silently. On rollback preserve external
  resources/backups; reverting the guard is not a safe mitigation for conflicts.
- Unit/API fixtures are external-dependency mocks only; real builders and
  reconciliation remain under test. Report unavailable live evidence honestly;
  upstream exact-SHA qualification and v1.6 tagging remain separate work.

## Approved refinement: retain workflow namespaces

On 2026-10-10, `IT-OWN-514-015` demonstrated that the initial RBAC/accounts-only
namespace guard still deleted an operator-created namespace containing an
unmarked administrator ConfigMap. UID/resourceVersion preconditions protect the
Namespace object, not its children or concurrent child creation.

Alternatives presented and the user's decision:

1. **Always retain workflow namespaces (approved):** remove only individually
   authorized workflow RBAC/accounts, complete CR finalization, and document
   administrator-reviewed namespace removal. No additional CRD/RBAC surface;
   workflow artifacts remain, so uninstall is intentionally non-destructive.
2. **Inventory all contents:** wider read permissions, custom-resource discovery
   and continued child-creation races; rejected for this P0 safety boundary.
3. **Namespace-wide delegation:** keep cascade deletion as an ownership exception;
   rejected because it authorizes removal of unmarked content.

This decision supersedes the initial complete-marker/created-by namespace-delete
exception. The `created-by` annotation remains provenance, not cascade authority.
The existing ownership/PSA policy still applies on creation, reuse and reinstall.

The user clarified that the CR/deployment namespace (`kubernaut-system` by
default) must already exist with provisioned DB/Valkey and administrator Secret
inputs. Workflow contents likewise belong to Kubernaut provisioning and are
outside the operator's cleanup scope. A missing workflow namespace may be
created by the operator; neither namespace is ever deleted by it.

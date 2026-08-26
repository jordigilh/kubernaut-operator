# Test Plan: #444 -- Remove `networkPolicies.externalRegistry` (no pod-level enforcement point)

**Format**: IEEE 829 hybrid (per `AGENTS.md`)
**Issue**: [#444](https://github.com/jordigilh/kubernaut-operator/issues/444) -- "Determine ExternalRegistry NetworkPolicy target (kubelet-level enforcement is out of pod-NetworkPolicy scope)"
**Origin**: carve-out from [#422](https://github.com/jordigilh/kubernaut-operator/issues/422)/[#421](https://github.com/jordigilh/kubernaut-operator/issues/421) (`docs/tests/421/TEST_PLAN.md` §1.1)
**Related**: [#443](https://github.com/jordigilh/kubernaut-operator/issues/443) (companion follow-up, documentation-only, see `docs/tests/421/TEST_PLAN.md`)

## 1. Background and decision

`spec.networkPolicies.externalRegistry.{cidr,port}` (`NetworkPolicyEgressOverride`,
`api/v1alpha2/kubernaut_types.go`) was added in the v1alpha2 scaffold mirroring
upstream Helm's `networkPolicies.externalRegistry.*` schema key, but never had
a builder or runtime hook wired to it. Preflight for this issue (repeated
`cocoindex_search`/grep across `internal/`) confirms there is **zero**
operator-managed infrastructure that could consume it:

- Image pulls (`spec.image.repository`/`.pullSecrets`) happen at the
  kubelet/CRI-O level for every pod, entirely outside the scope of any
  `NetworkPolicy` -- `NetworkPolicy` only governs pod-to-pod/pod-to-CIDR
  traffic *initiated by a running container's network namespace*, not the
  node-level image pull that happens before the container starts.
- This operator does not run or manage a pull-through cache, registry mirror,
  or any other in-cluster component that itself makes outbound registry
  calls a `NetworkPolicy` egress rule could scope.

**Decision** (user-confirmed, see conversation record): drop the two fields
entirely rather than leave them accepted-but-inert (the same "dead field"
anti-pattern already fixed for `networkPolicies.*`/`monitoring.*.tlsCaFile`/
`alignmentCheck.llmProfileRef` in #422/#424/#423) or add a misleading
doc-only "reserved" comment. This is a v1alpha2-only field with no
v1alpha1 equivalent and no conversion-webhook dependency, so removal has no
migration impact (§4).

## 2. Business requirement / control mapping

| ID | Statement |
|----|-----------|
| BR-API-001 | CRD API surface must not accept configuration that has no effect (silent misconfiguration risk) |
| FedRAMP CM-6 | Configuration settings must have a documented, enforced effect; unenforceable settings must not be exposed |
| FedRAMP SC-7 | Boundary protection is asserted only where actually implemented -- documenting a naked schema field as a network control without an enforcement point overstates the operator's SC-7 posture |

## 3. Scope

**In scope:**
- Remove `NetworkPoliciesSpec.ExternalRegistry` (`api/v1alpha2/kubernaut_types.go`)
- Regenerate deepcopy (`make generate`), CRD manifests (`make manifests`),
  OLM bundle (`make bundle`), and the consolidated installer
  (`make build-installer`)
- Update `docs/design/ADR-CRD-001-v1alpha2-redesign.md` (F3 addendum +
  changelog row) to reflect the removal
- Remove the stale `externalRegistry.{cidr,port}` carve-out reference in
  `internal/resources/networkpolicies_test.go`'s `#422` comment block

**Out of scope:** no change to `ExternalWebhooks` (a distinct, already-wired
field with a real consumer, `notificationNetworkPolicy`'s Slack-webhook
egress rule) or any other `networkPolicies.*` field.

## 4. Migration / backward-compatibility impact

None. `ExternalRegistry` is v1alpha2-only (introduced in the same PR that
introduced `NetworkPoliciesSpec` itself, per ADR-CRD-001 F3) -- there is no
v1alpha1 field to convert to/from, and no conversion-webhook code path
touches it. A CRD schema field removal does not reject existing stored
objects that happen to have the (now-pruned) key set: Kubernetes' structural
schema pruning silently drops unknown fields on read; no CR is invalidated.
No CR in this repo's samples (`config/samples/`) sets this field.

## 5. Wiring manifest

N/A -- this is a removal, not a new component. There is no production entry
point to wire.

## 6. TDD phases

### RED
No new failing test is meaningful for a field *removal* (there is no
behavior to assert other than "the field no longer exists", which the Go
compiler itself enforces once the struct field is deleted -- any lingering
reference becomes a compile error). Per `AGENTS.md`'s Go Anti-Pattern
Checklist there is no "test that fails first" step that adds value here;
this task instead front-loads a repo-wide reference sweep as its
verification step (§7).

### GREEN
1. Delete the `ExternalRegistry NetworkPolicyEgressOverride
   \`json:"externalRegistry,omitempty"\`` field from `NetworkPoliciesSpec`.
2. `make generate` (deepcopy) -- confirms no other Go code referenced the
   field (compile would fail otherwise).
3. `make manifests` -- regenerates `config/crd/bases/kubernaut.ai_kubernauts.yaml`
   with the `externalRegistry` sub-schema removed from
   `networkPolicies.properties`.
4. `make bundle` / `make build-installer` -- propagates the same removal to
   `bundle/manifests/kubernaut.ai_kubernauts.yaml` and `dist/install.yaml`.

### REFACTOR
- Update the `#422` carve-out comment in `networkpolicies_test.go` (no
  longer accurate once the field doesn't exist).
- Update ADR-CRD-001's F3 code snippet and add a changelog row documenting
  the reversal, consistent with the #288/#302 dead-field-removal precedent
  already established in that document.

## 7. Verification

- `go build ./...` -- confirms no remaining reference to the removed field
  anywhere in the module (deepcopy, controller, resources, tests, samples).
- `git diff --stat config/crd/ bundle/manifests/ dist/install.yaml` --
  confirms the diff is scoped to the single `externalRegistry` property
  removal (no unrelated churn).
- `make test` (excluding e2e) -- full regression pass.
- `golangci-lint run` -- zero new warnings.

## 8. Confidence assessment

**Confidence: 97%**

Justification: this is a pure subtraction of an already-confirmed-dead CRD
field with no consumers anywhere in the codebase (verified by repo-wide
search prior to this plan), no v1alpha1 equivalent, and no conversion-webhook
dependency. The only residual risk (3%) is an out-of-tree consumer (a
downstream Helm values-to-CR translation layer, or a customer CR already
setting this field in a pre-release environment) silently losing the
setting on upgrade -- mitigated by this being pre-GA v1alpha2 surface and the
field having had zero documented effect since its introduction.

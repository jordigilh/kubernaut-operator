# Test Plan: #443 -- Console NetworkPolicy: document as intentionally excluded

**Format**: IEEE 829 hybrid (per `AGENTS.md`), abbreviated -- documentation-only
change, no test-plan-mandatory code path per `AGENTS.md`'s "When to Use
Standard TDD Only" (documentation updates).
**Issue**: [#443](https://github.com/jordigilh/kubernaut-operator/issues/443) -- "Add a Console NetworkPolicy builder"
**Origin**: carve-out from [#422](https://github.com/jordigilh/kubernaut-operator/issues/422)/[#421](https://github.com/jordigilh/kubernaut-operator/issues/421) (`docs/tests/421/TEST_PLAN.md` §1.1)

## 1. Background and decision

Preflight confirmed Console is the only externally-exposed component
(Deployment + Service + Route, `internal/resources/console.go`) with no
corresponding `NetworkPolicy` builder. The generic `namedIngressPeers()`
helper built for #422 would make wiring `spec.networkPolicies.console`
straightforward -- a `consoleNetworkPolicy()` builder mirroring
`gatewayNetworkPolicy`/`apifrontendNetworkPolicy` was fully spec'd during
preflight for this issue.

**User decision** (see conversation record): do not add an
operator-managed `NetworkPolicy` for Console. Rationale: every other
component's ingress peers are a predictable, operator-known set --
in-cluster pods (other Kubernaut components), the OCP monitoring namespace,
or the OCP ingress namespace/router. Console's ingress, by contrast, is
browser/end-user traffic reaching it through the OCP Route -- who those
users are and where they originate from is deployment- and
organization-specific, not something the operator can encode a sensible
default for. Console is also explicitly "an optional item at the end" of
the component list (lowest-priority, most recently added, most
UI/user-driven of the exposed components). Administrators who want to
restrict Console's ingress should write their own supplemental
`NetworkPolicy` for it, the same way `MonitoringSpec`'s v1.8 addendum
already documents for an external (non-in-cluster) Prometheus/AlertManager
URL.

## 2. Scope

**In scope:**
- Document, in `docs/security/credentials-and-tls.md`'s NetworkPolicy
  (SC-7) section, that Console is intentionally excluded from the
  operator's auto-created NetworkPolicy set, with the pod selector/port an
  administrator needs to write their own.
- Add a one-line pointer comment in `docs/installation/03-deploy.md`'s
  sample CR `networkPolicies` block cross-referencing that guidance.
- Record the decision in `docs/design/ADR-CRD-001-v1alpha2-redesign.md` (F3
  addendum), consistent with how the #444 companion decision is recorded.

**Out of scope:** no Go code change. `spec.networkPolicies.console` remains
in the v1alpha2 schema (unlike #444's `externalRegistry`) because it is a
legitimate, already-correctly-shaped `NetworkPolicyNamedIngressOverride` --
the issue is that *nothing else* in that struct's default posture (i.e. no
`consoleNetworkPolicy()` at all) exists for it to override. Leaving the
field in place costs nothing and does not mislead an administrator the way
`externalRegistry.{cidr,port}` did (that field implied an override to a
default the operator does not create either -- there was no code path where
setting it would ever result in *any* different behavior, whereas here an
administrator supplying their own `NetworkPolicy` and eventually a future
`consoleNetworkPolicy()` builder, if ever added, would both use the same
schema key).

## 3. Verification

- `docs/security/credentials-and-tls.md` and `docs/installation/03-deploy.md`
  render correctly (markdown lint / manual review).
- No Go files touched; `go build ./...`/`make test` unaffected (verified as
  part of the combined #443/#444 PR's full-suite run).

## 4. Confidence assessment

**Confidence: 98%** -- pure documentation change recording a decision
already made by the project owner; no code, no schema, no test risk.

# Issue #555 — Portable Kubernetes NetworkPolicy implementation plan

**Status:** Implementation in progress
**Decision:** `docs/design/ADR-PLATFORM-003-portable-kubernetes-networkpolicy.md`
**Confidence after spikes:** 96%

## RED → GREEN → REFACTOR → VERIFY

- [x] **RED:** contract tests define standard `NetworkPolicy` kind/API,
  selector relationships, ingress-only behavior, ownership, and no API-blocking
  egress; controller wiring tests drive reconciliation without provider APIs.
- [x] **GREEN:** provider discovery/version/schema gates, native renderers,
  provider status, watches, and provider RBAC are replaced by standard
  NetworkPolicy rendering and ownership-safe reconciliation.
- [x] **REFACTOR:** clean-break CRD/API, status, ADR, platform/security/
  migration docs, generated CRD/deepcopy/RBAC/bundle/Helm artifacts, and static
  OLM manager policy are synchronized.
- [ ] **VERIFY:** complete build, lint, unit/integration coverage, Helm,
  manifest/codegen, security traceability, pyramid, and relevant Kind/OCP E2E
  lanes.

## Wiring manifest

| Component | Production entry point | Wiring location | Evidence |
|---|---|---|---|
| Standard renderer | Deploying reconciliation | `reconcileNetworkPolicies` | `UT-NP-555-RENDER-*`, `IT-NP-555-RECONCILE-001` |
| Standard lifecycle | Reconcile/finalizer cleanup | `ensureNetworkPolicy`, `pruneNetworkPolicies`, `deleteNetworkPolicies` | ownership/update/cleanup tests |
| Static manager policy | OLM bundle install | `config/network-policy`, `config/manifests`, bundle | Bundle test; Helm asserts no pre-CR operand policy |
| Applied-state status | CR status patch | `patchNetworkPolicyStatus` | `NetworkPolicyReady` assertions |

## CRD and compatibility boundary

`v1alpha2` is the sole served/storage version and has not been published. The
obsolete provider/API-server fields are removed directly. No conversion
webhook, deprecated aliases, migration procedure, or backward-compatibility
tests are required. Generated CRD and deepcopy output must match the Go API.

## OLM/bundle boundary

The bundle contains a namespaced, ingress-only static operator-manager metrics
policy. Runtime operand policies are created only after the Kubernaut CR is
applied. No provider CRDs, provider RBAC, or provider policy manifests are
included. The policy object is applied-state evidence only, not enforcement
attestation.

## Security/business acceptance

Tests map selector least privilege and ownership to FedRAMP/NIST AC-3 and AC-6,
SC-7/SC-7(3), CM-2/CM-8; SOC 2 CC6.1/CC6.6/CC7.2/CC8.1; and OWASP ASVS
V4.1–V4.3 and V16.5. Affected policy/resource coverage target is at least 90%
and cases are added only for distinct business behavior.

Acceptance requires generic Kubernetes reconciliation without CNI detection,
only standard NetworkPolicy runtime objects, selector survival through Pod
churn, ownership-safe updates/cleanup, no generated API-server egress, intact
TLS/dependency/readiness behavior, synchronized generated artifacts, and
explicit documentation that CNI enforcement is not claimed.

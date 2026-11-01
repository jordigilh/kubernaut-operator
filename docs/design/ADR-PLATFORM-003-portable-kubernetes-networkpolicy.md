# ADR-PLATFORM-003: Portable Kubernetes NetworkPolicy contract

- **Status:** Accepted
- **Date:** 2026-10-19
- **Issue:** [#555](https://github.com/jordigilh/kubernaut-operator/issues/555)
- **Supersedes:** provider-native policy portions of ADR-PLATFORM-001, ISSUE-488, ISSUE-492, PR-518, and their security evidence.

## Decision

The operator manages only namespaced `networking.k8s.io/v1` `NetworkPolicy`
objects. Policies use stable namespace and pod selectors and are reconciled
through the core Kubernetes API. Cilium, Calico, and OpenShift OVN discovery,
version/schema gates, native renderers, provider-specific status, watches, and
RBAC are not part of the runtime contract.

Runtime policies are ingress-only. The operator does not emit API-server CIDR
or IPBlock egress rules, and does not emit restrictive egress policies that
could block Kubernetes API clients, DNS, databases, Valkey, monitoring, IdP,
LLM, or other external dependencies. Cluster administrators/SREs own those
egress and cloud-network paths.

The generated operand policy intentionally excludes `AuthWebhook`: its
admission server is called by the Kubernetes API server, whose host-network
source has no portable namespace/pod selector. Administrators and the CNI own
that external admission-ingress boundary.

Console browser/Route ingress is likewise intentionally excluded. Its browser
source identity is deployment- and organization-specific; administrators own
any supplemental Console ingress policy.

The operator reports `NetworkPolicyReady` when the desired objects are applied
and ownership-safe. This is not an assertion that the installed CNI enforces
the objects. Enforcement qualification is a separate cluster/CNI concern.

## Ownership and lifecycle

Only policies carrying the operator managed-policy, manager, namespace, and
instance markers are eligible for update, stale pruning, or finalizer cleanup.
Administrator-owned policies are never adopted or mutated. Namespaced policies
carry the Kubernaut owner reference. TLS validation, dependency checks,
readiness transitions, and workload activation ordering remain unchanged.

## Static OLM policy

The OLM bundle includes a narrowly scoped, namespaced operator-manager ingress
policy for the metrics endpoint. The generic Helm bootstrap intentionally does
not install a static policy before the webhook path is available; it renders no
operand policies until a Kubernaut CR is applied. The static bundle policy has
no egress rule and therefore cannot block the operator's API client.

Red Hat Customer Portal article 7133113 confirms that Red Hat Operators must
ship NetworkPolicies beginning February 2027 and that OLM support was updated
for NetworkPolicies in bundles. The exact release-specific Conforma selector
validation remains a packaging-lane check; the repository contract is the
manager-policy/runtime-operand split above.

## Security evidence

Business-level unit and controller integration tests cover:

| Behavior | FedRAMP/NIST | SOC 2 | OWASP ASVS |
|---|---|---|---|
| Least-privilege selectors, ownership, and no administrator adoption | AC-3, AC-6, CM-8 | CC6.1, CC6.6 | V4.1, V4.2 |
| No generated API-blocking egress and explicit administrator boundary | SC-7, SC-7(3) | CC6.6, CC7.2 | V4.3 |
| Apply/update/prune/delete and observable readiness/error state | CM-2, SI-4 | CC7.2, CC8.1 | V14.2, V16.5 |

Coverage is measured by business behavior, not filler cases; the affected
policy/resource unit target is at least 90% and the controller integration gate
remains mandatory.

## Consequences

- Generic Kubernetes clusters need only the core NetworkPolicy API to install
  and reconcile the operator.
- Object application is portable, but dataplane enforcement varies by CNI and
  is not attested by this operator.
- Existing native-provider fields are removed as a clean-break `v1alpha2`
  schema change; this CRD has not been published, so no conversion or
  backward-compatibility path is required.

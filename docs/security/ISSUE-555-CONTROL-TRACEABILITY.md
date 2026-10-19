# Issue #555 — Portable NetworkPolicy control traceability

**Matrix ID:** KO-SEC-TR-555-001
**Version:** 1.0
**Date:** 2026-10-19
**Scope:** portable runtime NetworkPolicy objects, ownership, lifecycle, and
administrator egress boundaries

These are engineering control objectives adapted from NIST SP 800-53 Rev. 5
(FedRAMP), SOC 2, and OWASP ASVS 5.0.0. This matrix does not claim formal
FedRAMP authorization, SOC 2 attestation, ASVS conformance, or CNI enforcement.

## Control matrix

| Objective | Business behavior | Evidence |
|---|---|---|
| NIST/FedRAMP AC-3, AC-6; SOC 2 CC6.1, CC6.6; ASVS V4.1, V4.2 | Standard policies select only managed component pods, preserve administrator-owned policies, and reject foreign ownership. | `internal/policy/networkpolicy_contract_test.go`; `internal/policy/ownership_test.go`; controller ownership tests |
| NIST/FedRAMP SC-7, SC-7(3); SOC 2 CC6.6, CC7.2; ASVS V4.3 | Rendered and reconciled policies contain no egress rules or API-server CIDR/IPBlock; API clients remain usable through the administrator-owned network path. | `networkpolicy_contract_test.go`; `network_policy_integration_test.go`; `NetworkPolicyReady` status |
| NIST/FedRAMP CM-2, CM-8; SOC 2 CC8.1; ASVS V14.2 | CRD clean-break schema publishes only standard policy fields; provider APIs and version gates are absent. | `api/v1alpha2/clean_break_test.go`; generated CRD; RBAC assertions |
| NIST/FedRAMP SI-4, AU-3; SOC 2 CC7.2; ASVS V16.1 | Apply/update/prune/delete failures and readiness state are observable through structured logs, events, and `NetworkPolicyReady`. | controller unit/integration lifecycle tests; `patchNetworkPolicyStatus` |
| NIST/FedRAMP CM-6; SOC 2 CC8.1; ASVS V16.5 | The OLM bundle includes the required static operator-manager ingress policy without egress; Helm installs no pre-CR operand policy. | `test/helm/chart_test.go`; generated bundle manifest; Helm render checks |

## Coverage and interpretation

Affected policy/resource business coverage target is at least 90%; additional
cases are added only for distinct behavior (ownership, selector semantics,
updates, cleanup, API-client safety, and packaging), not line coverage. The
project controller integration gate remains mandatory. Applied object state is
not dataplane enforcement evidence; CNI-specific enforcement requires a real
cluster qualification outside this portable baseline.

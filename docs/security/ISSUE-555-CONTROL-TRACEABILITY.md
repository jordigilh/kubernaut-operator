# Issue #555 — Portable NetworkPolicy control traceability

**Matrix ID:** KO-SEC-TR-555-001
**Version:** 1.0
**Date:** 2026-10-19
**Scope:** portable runtime NetworkPolicy objects, ownership, lifecycle, and
administrator egress boundaries

These are engineering control objectives adapted from NIST SP 800-53 Rev. 5
(FedRAMP), SOC 2, and OWASP ASVS 5.0.0. This matrix does not claim formal FedRAMP
authorization, SOC 2 attestation, ASVS conformance, or CNI enforcement.

## Control matrix

| Objective | Business behavior | Evidence |
|---|---|---|
| NIST/FedRAMP AC-3, AC-6; SOC 2 CC6.1, CC6.6; ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1`, `v5.0.0-V15.2.5` | Standard policies select only managed component pods, preserve administrator-owned policies, and reject foreign ownership. | `internal/policy/networkpolicy_contract_test.go`; `internal/policy/ownership_test.go`; controller ownership tests |
| NIST/FedRAMP SC-7, SC-7(3); SOC 2 CC6.6, CC7.2; ASVS `v5.0.0-V13.1.1`, `v5.0.0-V13.2.4`, `v5.0.0-V15.2.5` | Rendered and reconciled policies contain no egress rules or API-server CIDR/IPBlock; API clients remain usable through the administrator-owned network path; AuthWebhook and Console external ingress remain administrator-owned. | `internal/policy/networkpolicy_contract_test.go`; `internal/controller/network_policy_integration_test.go`; `NetworkPolicyReady` status |
| NIST/FedRAMP CM-2, CM-3, CM-6, CM-8, SI-10; SOC 2 CC8.1; ASVS `v5.0.0-V2.1.1`, `v5.0.0-V2.2.1`, `v5.0.0-V2.2.2`, `v5.0.0-V2.2.3`, `v5.0.0-V15.2.3` | CRD clean-break schema and samples publish only standard policy fields; provider APIs and version gates are absent; bounded CIDR/namespace/selector inputs reject invalid and empty allow-all selectors. | `api/v1alpha2/clean_break_test.go`; `internal/policy/networkpolicy_contract_test.go`; `config/samples/v1alpha2_kubernaut.yaml`; `config/crd/bases/kubernaut.ai_kubernauts.yaml`; `config/rbac/role.yaml` |
| NIST/FedRAMP SI-4, AU-3, AU-12; SOC 2 CC7.2; ASVS `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1`, `v5.0.0-V16.3.3`, `v5.0.0-V16.3.4`, `v5.0.0-V16.5.2`, `v5.0.0-V16.5.3` | Apply/update/prune/delete failures and readiness state are observable through structured logs, events, and `NetworkPolicyReady`; ownership conflicts have a distinct status reason. | `internal/controller/network_policy_unit_test.go`; `internal/controller/network_policy_integration_test.go`; `patchNetworkPolicyStatus` |
| NIST/FedRAMP CM-6, CM-8, SA-11; SOC 2 CC8.1; ASVS `v5.0.0-V15.1.2`, `v5.0.0-V15.2.3`, `v5.0.0-V15.2.4`, `v5.0.0-V15.2.5` | The OLM bundle includes the required static operator-manager ingress policy without egress; Helm installs no pre-CR operand policy; generated policy/RBAC artifacts are synchronized. | `test/helm/chart_test.go`; `bundle/manifests/allow-metrics-traffic_networking.k8s.io_v1_networkpolicy.yaml`; `config/rbac/role.yaml`; Helm render checks |

## Executable evidence IDs

The security gate validates that these identifiers remain present in executable
tests and that this matrix remains part of the repository security gate:

| Tier | Evidence IDs |
|---|---|
| API/policy unit | `UT-NP-555-API-001`, `UT-NP-555-RENDER-001`, `UT-NP-555-RENDER-002`, `UT-NP-555-RENDER-003`, `UT-NP-555-VALIDATE-001` |
| Controller unit | `UT-NP-555-CTRL-001`, `UT-NP-555-CTRL-002`, `UT-NP-555-CTRL-003`, `UT-NP-555-CTRL-004`, `UT-NP-555-CTRL-005`, `UT-NP-555-CTRL-006`, `UT-NP-555-CTRL-007` |
| Controller integration | `IT-NP-555-RECONCILE-001`, `IT-NP-555-OWNERSHIP-002`, `IT-NP-555-LIFECYCLE-003`, `IT-NP-555-FAILCLOSED-004`, `IT-NP-555-SCRAPE-005`, `IT-NP-555-API-ERROR-006`, `IT-NP-555-WIRING-007`, `IT-NP-555-WIRING-008`, `IT-NP-555-COVERAGE-009` |

## Coverage and interpretation

Affected policy/resource business coverage target is at least 90%; additional
cases are added only for distinct behavior (ownership, selector semantics,
updates, cleanup, API-client safety, and packaging), not line coverage. The
project controller integration gate remains mandatory. Applied object state is
not dataplane enforcement evidence; CNI-specific enforcement requires a real
cluster qualification outside this portable baseline.

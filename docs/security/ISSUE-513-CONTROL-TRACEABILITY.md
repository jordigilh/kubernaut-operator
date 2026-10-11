# Issue #513 security control traceability

**Matrix ID:** KO-SEC-TR-513-001

**Version:** 0.2

**Date:** 2026-10-10

**Scope:** Generated Service and ServiceMonitor consistency for the Kubernaut Operator

**Status:** Tier-corrected repository evidence; historical reported fleet Kind
resource-contract evidence is not remediation qualification; upstream lane migration pending

**Test plan:** `docs/tests/513/TEST_PLAN.md`

**Executable gates:** `hack/verify-monitoring-contract.sh`,
`hack/verify-business-unit-coverage.sh`, and `hack/verify-test-pyramid.sh`

This is an engineering traceability matrix, not a certification package. The
NIST/FedRAMP and SOC 2 entries are project-selected control objectives. The
OWASP ASVS 5.0.0 entries are versioned requirement references. This document
does not claim formal FedRAMP authorization, a SOC 2 audit opinion, or ASVS
conformance.

## Evidence status

- **verified:** a passing repository artifact directly proves the stated scope;
- **partially verified:** repository evidence exists, but live-platform or
  organizational evidence remains;
- **not verified:** no approved evidence exists for the stated scope.

## Evidence identifiers

Resource unit IDs are `UT-MON-513-001`–`004`. Fake-client controller cases formerly
labeled `IT-MON-513-001`–`007` are now distinct `UT-MON-513-CTRL-001`–`007`;
they do not prove API-server wiring. `IT-MON-513-008`, `IT-MON-513-009` and
`IT-MON-513-010` drive production `Reconcile` against envtest. The strengthened
positive `008` fails when monitoring is disconnected from production.

The individually checked controller unit IDs are `UT-MON-513-CTRL-001`,
`UT-MON-513-CTRL-002`, `UT-MON-513-CTRL-003`, `UT-MON-513-CTRL-004`,
`UT-MON-513-CTRL-005`, `UT-MON-513-CTRL-006` and `UT-MON-513-CTRL-007`.
The resource IDs are `UT-MON-513-001`, `UT-MON-513-002`, `UT-MON-513-003`
and `UT-MON-513-004`.

## Control matrix

| Control objective | Implementation evidence | Test/evidence artifact | Status | Residual risk |
|---|---|---|---|---|
| FedRAMP/NIST `CM-2`, `CM-3`, `CM-6`, `CM-8` | `internal/resources/services.go`; `internal/resources/monitoring.go`; `internal/controller/kubernaut_controller.go` | `UT-MON-513-001`–`004`; `UT-MON-513-CTRL-001`–`006`; `IT-MON-513-008`; `IT-MON-513-010`; `hack/verify-monitoring-contract.sh`; generated-manifest review | verified | Future component/port additions must extend the contract table and tests. |
| FedRAMP/NIST `SI-4` | Generated metrics discovery is explicit and invalid monitor targets are prevented; Gateway-disabled reconciliation omits the monitor; structured reconciliation logging records legacy cleanup. | `UT-MON-513-001`; `UT-MON-513-CTRL-001`–`007`; `IT-MON-513-008`; `hack/verify-monitoring-contract.sh`; historical reported `E2E-MON-513-001` | partially verified | Exact-revision Prometheus scrape health and alert delivery still require a live monitoring deployment. |
| FedRAMP/NIST `AC-6` | Legacy cleanup checks the controller owner reference before deletion and remains active when Prometheus provisioning is disabled. | `UT-MON-513-CTRL-003`, `UT-MON-513-CTRL-004`, and `IT-MON-513-009` | partially verified | Concurrent replacement/ownership-change deletion and broader adoption safety depend on #517; no race-safety claim before integration. |
| FedRAMP/NIST `AU-2`, `AU-3`, `AU-12` | Reconciliation cleanup emits structured kind, name, namespace, generation, and resourceVersion metadata. | `UT-MON-513-CTRL-007`; controller implementation review | partially verified | Log collection, retention, access, and time synchronization are platform responsibilities. |
| SOC 2 `CC6.1`, `CC6.6`, `CC8` | Non-racing helper cases preserve a user-owned same-named monitor. | `UT-MON-513-CTRL-003`, `UT-MON-513-CTRL-004` | partially verified | Atomic deletion/adoption safety remains dependent on #517; organizational change-management and access evidence are separate. |
| SOC 2 `CC7.2` | Metrics resources are rendered only when the optional API is available and target exposed generated ports. | `UT-MON-513-001`, `UT-MON-513-003`; `UT-MON-513-CTRL-001`, `UT-MON-513-CTRL-002`; `IT-MON-513-008`; historical `E2E-MON-513-001` | partially verified | Exact-revision scrape success remains to be observed from a running Prometheus scrape target. |
| OWASP ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | Non-racing helper cases honor monitor ownership boundaries. | `UT-MON-513-CTRL-003`, `UT-MON-513-CTRL-004` | partially verified | #517 concurrency protections and full application authorization assessment remain required. |
| OWASP ASVS `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1` | Monitoring resource identity and cleanup are observable through structured controller logs and deterministic names. | `UT-MON-513-CTRL-007`; controller implementation review | partially verified | Runtime log storage and investigation controls require deployment evidence. |
| OWASP ASVS `v5.0.0-V16.5.2` | Missing optional APIs fail safely without creating invalid monitoring resources; disabled Gateway and disabled Prometheus paths remain safe. | `UT-MON-513-CTRL-002`, `UT-MON-513-CTRL-005`, `UT-MON-513-CTRL-006`, `IT-MON-513-008`, `IT-MON-513-009`, `IT-MON-513-010` | verified | Broader dependency-failure testing remains outside this issue. |

## Real-cluster follow-up

The earlier implementation record reports `E2E-MON-513-001` against both owned fleet Kind clusters on `helios08`,
where the Prometheus Operator APIs are installed. It produced
`LIVE_MONITORING_CONTRACT_PASS` for the base nine-monitor deployment on the hub
and for the ten-monitor deployment with APIFrontend enabled on the remote
cluster. The temporary qualification resources were removed afterward. This
audit/remediation has not independently rerun those clusters or observed actual
scraping/alert delivery. Historical records do not qualify the remediation SHA.

The upstream `e2e fp` and `e2e fleet` lanes remain real-cluster follow-up
evidence for the future Helm-to-operator deployment replacement. Their current
application-chart journeys are intentionally not counted as direct operator
evidence; once the upstream deployment path switches to this operator,
`E2E-FP/FLEET-MON-513-001` will become the full application-lifecycle artifact.

# Issue #513 security control traceability

**Matrix ID:** KO-SEC-TR-513-001

**Version:** 0.1

**Date:** 2026-10-09

**Scope:** Generated Service and ServiceMonitor consistency for the Kubernaut Operator

**Status:** Repository and fleet Kind evidence verified; upstream lane migration pending

**Test plan:** `docs/tests/513/TEST_PLAN.md`

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

The executable evidence identifiers for this matrix are `UT-MON-513-001`,
`UT-MON-513-002`, `UT-MON-513-003`, `UT-MON-513-004`, `IT-MON-513-001`,
`IT-MON-513-002`, `IT-MON-513-003`, `IT-MON-513-004`, and `IT-MON-513-005`.

## Control matrix

| Control objective | Implementation evidence | Test/evidence artifact | Status | Residual risk |
|---|---|---|---|---|
| FedRAMP/NIST `CM-2`, `CM-3`, `CM-6`, `CM-8` | `internal/resources/services.go`; `internal/resources/monitoring.go`; `internal/controller/kubernaut_controller.go` | `UT-MON-513-001`–`004`; `IT-MON-513-001`–`005`; generated-manifest review | verified | Future component/port additions must extend the contract table and tests. |
| FedRAMP/NIST `SI-4` | Generated metrics discovery is explicit and invalid monitor targets are prevented; structured reconciliation logging records legacy cleanup. | `UT-MON-513-001`; `IT-MON-513-001`–`005`; live `E2E-MON-513-001` on both fleet Kind clusters | verified | Prometheus scrape health and alert delivery still require a live monitoring deployment. |
| FedRAMP/NIST `AC-6` | Legacy cleanup checks the controller owner reference before deletion. | `IT-MON-513-003` and `IT-MON-513-004` | verified | Broader cluster RBAC review remains outside this issue. |
| FedRAMP/NIST `AU-2`, `AU-3`, `AU-12` | Reconciliation cleanup emits structured object identity and namespace context. | Controller implementation review; controller integration suite | partially verified | Log collection, retention, access, and time synchronization are platform responsibilities. |
| SOC 2 `CC6.1`, `CC6.6`, `CC8` | Operator-owned resources are changed only through the reconciler; user-owned same-named resources are not deleted. | `IT-MON-513-003`, `IT-MON-513-004` | verified | External change-management approval and deployment access controls require organizational evidence. |
| SOC 2 `CC7.2` | Metrics resources are rendered only when the optional API is available and target real exposed ports. | `UT-MON-513-001`, `UT-MON-513-003`; `IT-MON-513-001`, `IT-MON-513-002`; `E2E-MON-513-001` on both fleet Kind clusters | verified | End-to-end scrape success remains to be observed from a running Prometheus scrape target. |
| OWASP ASVS `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1` | Reconciliation honors resource ownership boundaries and does not delete a user-owned monitor. | `IT-MON-513-003`, `IT-MON-513-004` | partially verified | Full application authorization assessment is outside operator scope. |
| OWASP ASVS `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1` | Monitoring resource identity and cleanup are observable through structured controller logs and deterministic names. | Controller implementation review; integration suite | partially verified | Runtime log storage and investigation controls require deployment evidence. |
| OWASP ASVS `v5.0.0-V16.5.2` | Missing optional APIs fail safely without creating invalid monitoring resources. | `IT-MON-513-002`, `IT-MON-513-005` | verified | Broader dependency-failure testing remains outside this issue. |

## Real-cluster follow-up

`E2E-MON-513-001` ran against both owned fleet Kind clusters on `helios08`,
where the Prometheus Operator APIs are installed. It produced
`LIVE_MONITORING_CONTRACT_PASS` for the base nine-monitor deployment on the hub
and for the ten-monitor deployment with APIFrontend enabled on the remote
cluster. The temporary qualification resources were removed afterward.

The upstream `e2e fp` and `e2e fleet` lanes remain real-cluster follow-up
evidence for the future Helm-to-operator deployment replacement. Their current
application-chart journeys are intentionally not counted as direct operator
evidence; once the upstream deployment path switches to this operator,
`E2E-FP/FLEET-MON-513-001` will become the full application-lifecycle artifact.

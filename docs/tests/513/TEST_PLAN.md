# Issue #513 ServiceMonitor/Service Contract Test Plan

**Issue:** [kubernaut-operator#513](https://github.com/jordigilh/kubernaut-operator/issues/513)

**Date:** 2026-10-09

**Status:** Repository evidence tier-corrected by PR #515 remediation; reported
fleet Kind results are historical resource-contract evidence, not independent
application/scrape qualification of the remediation; upstream lane migration pending

**Methodology:** RED → GREEN → REFACTOR → CHECK

## 1. Objective

Ensure every generated `ServiceMonitor` endpoint names a port exposed by the
selected generated `Service`, and ensure every generated Service port targets a
port declared by the selected workload. The operator must not create a
ServiceMonitor for AuthWebhook because AuthWebhook exposes no metrics endpoint.

The contract is platform-neutral. It applies to OpenShift and generic
Kubernetes whenever the Prometheus Operator `ServiceMonitor` API is installed.

## 2. Implementation contract

1. `data-storage-service` exposes the named `metrics` port at `9090`, matching
   `DataStorageDeployment`.
2. All generated ServiceMonitor endpoint ports resolve by name against their
   generated Service.
3. AuthWebhook has no generated ServiceMonitor.
4. Optional monitoring APIs remain capability-gated.
5. Upgrade reconciliation removes an operator-owned legacy `authwebhook-monitor`
   but preserves a same-named user-owned object.
6. APIFrontend monitoring remains wired through its existing optional-CRD path.

## 3. Test pyramid and evidence

| Tier | Test ID | Artifact | Contract |
|---|---|---|---|
| Unit | `UT-MON-513-001` | `internal/resources/monitoring_test.go` | Every generated ServiceMonitor endpoint resolves to a Service port. |
| Unit | `UT-MON-513-002` | `internal/resources/monitoring_test.go` | AuthWebhook is not included in the generated monitor set. |
| Unit | `UT-MON-513-003` | `internal/resources/services_test.go` | DataStorage exposes the named metrics Service port. |
| Unit | `UT-MON-513-004` | `internal/resources/services_test.go` | Every generated Service target resolves to a workload container port. |
| Unit (simulated API) | `UT-MON-513-CTRL-001` | `internal/controller/monitoring_wiring_integration_test.go` | Available monitoring APIs create the expected rules and nine component monitors. |
| Unit (simulated API) | `UT-MON-513-CTRL-002` | `internal/controller/monitoring_wiring_integration_test.go` | Missing ServiceMonitor API creates no optional monitors. |
| Unit (simulated API) | `UT-MON-513-CTRL-003` | `internal/controller/monitoring_wiring_integration_test.go` | Operator-owned legacy AuthWebhook monitor is removed. |
| Unit (simulated API) | `UT-MON-513-CTRL-004` | `internal/controller/monitoring_wiring_integration_test.go` | User-owned same-named monitor is preserved. |
| Unit (simulated API) | `UT-MON-513-CTRL-005` | `internal/controller/monitoring_wiring_integration_test.go` | APIFrontend monitor/rule wiring follows CRD discovery. |
| Unit (simulated API) | `UT-MON-513-CTRL-006` | `internal/controller/monitoring_wiring_integration_test.go` | Gateway-disabled reconciliation does not create a Gateway ServiceMonitor. |
| Unit (simulated API) | `UT-MON-513-CTRL-007` | `internal/controller/monitoring_wiring_integration_test.go` | Legacy cleanup records kind, name, namespace, generation, and resourceVersion. |
| Integration | `IT-MON-513-008` | `internal/controller/monitoring_wiring_integration_test.go` | Real reconciliation with Gateway disabled asserts every expected monitor/rule and the full monitor→Service→workload port contract, not only absence of a Gateway monitor. |
| Integration | `IT-MON-513-009` | `internal/controller/monitoring_wiring_integration_test.go` | Real reconciliation prunes an owned legacy monitor when Prometheus is disabled. |
| Integration | `IT-MON-513-010` | `internal/controller/monitoring_wiring_integration_test.go` | Real reconciliation reaches Running when optional monitoring CRDs are unavailable. |
| Live Kind (historical, reported) | `E2E-MON-513-001` | Earlier fleet Kind qualification on `helios08` | Real API discovery and rendered Service/ServiceMonitor contract; not rerun for the remediation and not scrape/alert evidence. |
| Upstream E2E | `E2E-FP/FLEET-MON-513-001` | Future operator-backed upstream FP/Fleet lanes | Full application journeys after the Helm-to-operator replacement. **Pending lane migration.** |

Unit tests prove the generated-object contract. Controller integration tests
prove production reconciliation and ownership wiring. Live Kind and upstream
FP/Fleet tests prove the real API-server journey; they are not replaced by
builder tests.

PR #515 remediation separated fake-client controller cases from envtest and
strengthened `IT-MON-513-008` after the audit's disconnected-caller mutation
survived all original monitoring specs. In the new isolated mutation, direct
helper units still pass but this positive envtest assertion fails when the
production monitoring caller is removed. Monitoring CRDs in envtest are minimal
fixtures, not a running Prometheus Operator or proof of live target `up=1`.

## 4. Qualification environments

- The owned fleet Kind pair on `helios08` has Prometheus Operator CRDs installed
  and was used for real-Kubernetes qualification on both `kubernaut-hub` and
  `kubernaut-remote-cluster`.
- Existing Helm-managed fleet workloads must remain untouched while the
  operator qualification uses an isolated namespace/resource set.
- The upstream `e2e fp` and `e2e fleet` lanes deploy real Kubernetes clusters.
  They currently exercise the application chart, but will become direct
  operator evidence when the upstream deployment path is replaced.
- The local operator Kind suite is not counted as ServiceMonitor evidence because
  its standard fixture does not install the monitoring CRD.

## 5. Completed fleet Kind evidence

The historical implementation record reports that on 2026-10-09 its operator image was loaded into the
existing fleet Kind nodes on `helios08`. The operator chart, CRD, and Kubernaut
CR were installed only in temporary `*-issue-513` namespaces; the existing
Helm-managed `kubernaut-system` workloads were not replaced or modified.

The live contract checker reported:

```text
kubernaut-hub:           LIVE_MONITORING_CONTRACT_PASS monitors=9 operatorServices=10
kubernaut-remote-cluster: LIVE_MONITORING_CONTRACT_PASS monitors=10 operatorServices=11
```

The remote-cluster run enabled APIFrontend, so it covered both the base nine
component monitors and the optional `apifrontend-monitor`. Both runs confirmed
that `data-storage-service` exposes `metrics`, every monitor endpoint resolves to
an existing Service port, every generated Service port resolves to a declared
workload container port, and `authwebhook-monitor` is absent. The owned legacy
AuthWebhook monitor was deleted on both clusters; a same-named user-owned monitor
was preserved on the hub run.

The temporary namespaces, operator releases, CRs, and CRD were removed after
verification. The pre-existing Helm workloads remained present.

## 6. Reproducible live-contract checker

The fleet qualification is reproducible with the repository checker below. It
reads only the selected namespace and validates operator-owned ServiceMonitor
selectors, named endpoint ports, Service target ports, and Deployment container
ports. Optional expected counts make the hub/remote qualification assertions
fail closed rather than relying on a manually transcribed output line.

```bash
EXPECTED_MONITORS=9 EXPECTED_OPERATOR_SERVICES=10 \
  hack/verify-monitoring-contract.sh kubernaut-hub-issue-513

EXPECTED_MONITORS=10 EXPECTED_OPERATOR_SERVICES=11 \
  hack/verify-monitoring-contract.sh kubernaut-remote-cluster-issue-513
```

The expected output is `LIVE_MONITORING_CONTRACT_PASS` with the observed
monitor and Service counts. Set `KUBECTL_CONTEXT` when the namespace is on a
named context. The checker requires `kubectl` and `jq`; it does not mutate the
cluster.

## 7. Verification commands

```bash
go test ./internal/resources ./internal/controller -timeout 15m
go build ./...
golangci-lint run
make test
make manifests generate
```

`make test-unit` now enforces zero missed handwritten business statements and
zero missing functions across `api/`, `internal/` and `cmd/` through
`hack/verify-business-unit-coverage.sh`. Only generated code and four explicitly
listed bootstrap/registration functions are excluded; integration/merged
coverage cannot satisfy the unit gate. `make test-integration` separately
requires 78% controller-only coverage. PR #515 remediation intentionally changes
the isolation/scraper schema; repeat generation must produce no additional drift.

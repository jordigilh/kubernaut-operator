# IEEE 829 Test Plan — Issue #574 stale RouteReady condition migration

| Field | Value |
|---|---|
| Test Plan ID | TP-574 |
| Issue | [kubernaut-operator#574](https://github.com/jordigilh/kubernaut-operator/issues/574) |
| Related design | [`DD-565-EXTERNAL-EXPOSURE-BOUNDARY.md`](../../design/DD-565-EXTERNAL-EXPOSURE-BOUNDARY.md) (status contract), [`ISSUE-565-IMPLEMENTATION-PLAN.md`](../../design/ISSUE-565-IMPLEMENTATION-PLAN.md) |
| Scope | Reconcile-time pruning of the retired `RouteReady` status condition on objects upgraded from a pre-#572 operator |
| Framework | Ginkgo v2/Gomega; envtest for production reconciliation |

## Pyramid invariant

- **UT proves logic:** `finalizeDeployStatus` removes a seeded stale
  `RouteReady` entry, keeps `ExposureReady=True/InternalOnly`, and is
  idempotent on a follow-up pass with no stale entry.
- **IT proves wiring:** a real reconciler drives an envtest object to
  `Running`, a status patch injects the pre-#572 operator's last write, and
  one more full reconcile persists the pruned status without creating any
  operator-managed external exposure resource.
- **E2E proves the journey:** covered by the existing #565 live-cluster
  exposure assertions; the upgrade path exercises the same reconciliation
  code, so no new E2E lane is added for this bounded fix.

Tests assert business behavior and control objectives; no test exists only to
increase coverage.

## Evidence matrix

| ID | Business behavior | Controls |
|---|---|---|
| `UT-EXP-574-STATUS-002` | A stale `RouteReady` condition written by a pre-#572 operator is removed while `ExposureReady=True/InternalOnly` and unrelated conditions remain; a second pass is idempotent. | FedRAMP CM-6/SI-4; SOC 2 CC7.2; ASVS V16.5.2 |
| `UT-EXP-574-STATUS-003` | A failed status write propagates the error and rolls back in-memory status, so the stale condition stays pending until the pruning persists. | FedRAMP CM-6/SI-4; SOC 2 CC7.2; ASVS V16.5.2 |
| `IT-EXP-574-UPGRADE-001` | Real reconciliation of an upgraded object drops the stale `RouteReady` condition, keeps phase `Running` and the current exposure contract, and creates no operator-managed Ingresses. | FedRAMP CM-6/SI-4; SOC 2 CC7.2; ASVS V16.5.2 |

## Coverage and gates

- Unit business coverage: at least 90%; zero missed handwritten business
  statements is the preferred gate.
- Controller integration coverage: at least 78%.
- No pending/skipped tests and no `time.Sleep`.
- Generated CRD/RBAC/bundle/chart/installer artifacts must match source
  (no schema change in this fix; `make manifests generate` produces no diff).
- This plan is engineering evidence, not a formal compliance claim.

## Execution evidence

- `go build ./...`, `golangci-lint run` (0 issues), `make manifests generate`
  (no diff): passed.
- `make test`: passed; internal unit coverage **100.0%** (threshold 90%),
  controller integration coverage **79.0%** (threshold 78%).
- `UT-EXP-574-STATUS-002` and `IT-EXP-574-UPGRADE-001` confirmed RED before
  the fix (stale condition persisted) and GREEN after.

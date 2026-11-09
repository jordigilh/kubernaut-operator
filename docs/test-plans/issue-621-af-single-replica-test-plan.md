# Issue #621 — API Frontend single-replica test plan

## Scope

This plan verifies that the operator-managed API Frontend (AF) Deployment stays
at one replica, does not overlap old and new AF pods during a Deployment
rollout, and does not allow the removed operator HPA to reintroduce scaling.
DataStorage autoscaling is a regression boundary and remains unchanged.

Current AF Deployment-rollout qualification on live Kind/OCP remains a required
residual check for this issue because envtest cannot prove pod ordering. Future
AF HA, coordination, and failover qualification are out of scope for this issue
and belong to `kubernaut#2606`. Console `#168` is separate.

## Acceptance matrix

| Objective | Evidence | Test level |
|---|---|---|
| AF Deployment requests exactly one replica | `APIFrontendDeployment` renders `spec.replicas=1` | Unit and controller lifecycle |
| AF replacement prevents old/new overlap | AF Deployment renders `strategy.type=Recreate`; actual pod ordering is reserved for live Kind/OCP qualification | Unit and envtest; live qualification residual |
| Operator does not manage AF HPA | AF HPA builder/desire path is absent; enabled reconciliation has no `apifrontend-hpa` | Unit and envtest |
| Legacy operator AF HPA is removed safely | Exact evidenced name `apifrontend-hpa` is pruned through `deleteIfExists` on enabled and disabled paths | Envtest |
| Foreign AF HPA is preserved | Same-name and differently named foreign HPAs targeting AF remain unchanged; the same-name case emits Warning `OwnershipConflict` | Envtest |
| Hash-preserved AF Deployment drift repairs | Manual replica/strategy changes converge even with the original spec hash; metadata/defaults remain intact; second reconcile is a no-op | Controller unit and envtest |
| DataStorage behavior is unchanged | DataStorage HPA remains `minReplicas=1`, `maxReplicas=5`; AF-only drift logic does not reset DataStorage scaling | Unit and controller unit/lifecycle |
| Sensitive data remains excluded | Assertions/log checks use object metadata and resource fields only; no credentials, tokens, cookies, or Authorization headers are added | Review gate |

## Test cases

- `UT-AF-621-001`: AF builder renders one replica and `Recreate`.
- `UT-AF-621-002`: hash-preserved AF Deployment replica/strategy drift is repaired,
  live annotations/labels, pod-template metadata, finalizers, and API-defaulted
  fields are retained, the second reconcile does not update, and DataStorage
  replica drift is not repaired by the AF-only guard.
- `IT-AF-621-001`: enabled AF lifecycle creates the one-replica `Recreate`
  Deployment without an AF HPA and keeps DataStorage HPA at `1/5`.
- `IT-AF-621-002`: hash-preserved AF Deployment drift is repaired through the
  real `Reconcile` path and a second reconcile is a no-op.
- `IT-AF-621-003`: an owned legacy `apifrontend-hpa` is pruned during enabled
  reconciliation.
- `IT-AF-621-004`: a foreign same-name HPA is preserved with an
  `OwnershipConflict` warning.
- `IT-AF-621-005`: AF enable→disable invokes unconditional legacy-HPA cleanup,
  removes the owned legacy HPA, and preserves an independently named foreign
  HPA.

## Ownership and observability requirements

- Cleanup uses a fixed, repository-evidenced candidate-name list; it never
  scans or deletes by labels, scale target, or wildcard.
- Foreign objects remain byte-for-byte unchanged by the operator cleanup path.
- Diagnostics identify kind, namespace, name, ownership provenance, and
  resource versions without exposing Secret data or bearer material.
- HPA fixtures are explicitly removed during test teardown because the shared
  lifecycle cleanup does not sweep HPAs.

## Verification record

Tested source: PR branch commit
`a3c8e2bcd15bbc47ec301f8d376973ecf31e0e16`.

- `make test-unit`: passed; 100.0% internal unit coverage and zero missed
  business statements.
- `make test-integration`: passed; 78.7% controller integration coverage
  against the 78% threshold.
- `make test-helm`: passed; all 13 Helm specs passed.
- `make test`: passed; aggregate unit, integration, and Helm gates passed.
- `go build ./...`, `golangci-lint run`, compile-only tests, and
  `make manifests generate`: passed. No config diff or whitespace errors.

Envtest proves rendered resources, ownership-safe cleanup, and reconciliation;
it does not prove kube-controller-manager HPA behavior or actual pod replacement
ordering. A live Kind/OCP qualification is residual evidence, not a substitute
for these unit/envtest checks.

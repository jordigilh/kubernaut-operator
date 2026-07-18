# IEEE 829 Test Plan — Issue #180: Fleet config (ADR-068 scope-checking)

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-180                                             |
| **Issue**          | #180 — Add shared `spec.fleet` block for Gateway/RemediationOrchestrator federated scope-checking |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-07-18                                         |
| **Scope**          | `api/v1alpha1/kubernaut_types.go`, `internal/resources/{validation,configmaps,deployments}.go` |

## 1. Objective

Verify that a new, shared `spec.fleet` CRD block lets Gateway and
RemediationOrchestrator be pointed at a federated scope-check backend per
ADR-068 — either the Fleet Metadata Cache (FMC) service's HTTP API or Red
Hat Advanced Cluster Management (ACM) Search's GraphQL API — correcting the
stale `valkeyAddr`/`backend: "valkey"` premise present in the issue as
originally filed (upstream did a hard removal of that path, not a
backward-compatible deprecation; see `pkg/fleet/fleet_test.go`'s
`UT-SF-054-002 [CM-6]` rejecting-`valkey` spec).

Confirm:
1. `spec.fleet` content validation enforces `backend`/`endpoint` only when
   `enabled: true`, and otherwise leaves the block inert for pre-staging.
2. Both Gateway's and RemediationOrchestrator's ConfigMaps render an
   identical resolved `fleet:` block (single shared CR field, no
   per-component override) when enabled, and omit it entirely when
   disabled.
3. Both components' Deployments conditionally mount BYO CA/token Secrets
   (`caSecretName`/`tokenSecretName`) only when those fields are set, at
   mount paths matching the rendered `tlsCAFile`/`tokenPath` config values.
4. The `github.com/jordigilh/kubernaut` Go dependency and the pinned
   `RELATED_IMAGE_GATEWAY`/`RELATED_IMAGE_REMEDIATIONORCHESTRATOR` image
   digests both track builds that include ADR-068, so the feature is
   functionally live end-to-end, not just schema plumbing.

## 2. Test Strategy

Unit tests only (no envtest needed for these builder-function and
validation-function tests), matching the existing `internal/resources`
convention (see TP-187). Controller-level fixtures
(`internal/controller/*_test.go`) require no changes since `spec.fleet`
introduces no new controller/reconciler behavior beyond what the existing
ConfigMap/Deployment builders already handle. Full `test-integration`
(envtest-backed) run confirms no regressions.

## 3. Test Scenarios

### 3.1 Fleet config content validation (`internal/resources/validation_test.go`, `Describe("Fleet Config Validation")`)

| ID     | FedRAMP | Description                                                              |
|--------|---------|---------------------------------------------------------------------------|
| FL-001 | CM-6    | accepts a nil/zero-value `fleet` spec (feature untouched)                |
| FL-002 | CM-6    | accepts `fleet` disabled with `backend`/`endpoint` unset (pre-staging allowed) |
| FL-003 | CM-6    | accepts `fleet` disabled even with an invalid `backend` value (inert fields are not validated) |
| FL-004 | CM-6    | rejects `fleet` enabled with an empty `backend`                          |
| FL-005 | CM-6    | rejects `fleet` enabled with an unsupported `backend` value (e.g. `valkey`, confirming the corrected premise) |
| FL-006 | CM-6    | rejects `fleet` enabled with an empty `endpoint`                         |
| FL-007 | CM-6    | accepts `fleet` enabled with `backend: fleetmetadatacache` and an `endpoint` |
| FL-008 | CM-6    | accepts `fleet` enabled with `backend: acm`, an `endpoint`, and a `tokenSecretName` |
| FL-009 | CM-6    | accepts `fleet` enabled with a `caSecretName` set alongside a valid `backend`/`endpoint` |
| FL-010 | IA-5    | rejects `fleet` enabled with `backend: acm` and no `tokenSecretName` — ACM Search GraphQL has no unauthenticated mode upstream; omitting the token crash-loops Gateway/RemediationOrchestrator at startup instead of failing fast at admission |
| FL-011 | IA-5    | accepts `fleet` enabled with `backend: fleetmetadatacache` and no `tokenSecretName` (optional for FMC, mandatory only for ACM) |

### 3.2 Resource rendering — ConfigMaps (`internal/resources/configmaps_test.go`, `Describe("Gateway ConfigMap")` / `Describe("RemediationOrchestrator ConfigMap")`)

| ID     | FedRAMP | Description                                                              |
|--------|---------|---------------------------------------------------------------------------|
| CM-101 | CM-6    | Gateway ConfigMap omits the `fleet:` block when fleet is disabled        |
| CM-102 | CM-6    | Gateway ConfigMap renders `fleet:` with `enabled`/`backend`/`endpoint` when enabled |
| CM-103 | SC-8    | Gateway ConfigMap renders `tlsCAFile`/`tokenPath` mount paths when the corresponding secrets are set |
| CM-104 | CM-6    | RemediationOrchestrator ConfigMap omits the `fleet:` block when fleet is disabled |
| CM-105 | CM-6    | RemediationOrchestrator ConfigMap renders `fleet:` with `enabled`/`backend`/`endpoint` when enabled |
| CM-106 | SC-8    | RemediationOrchestrator ConfigMap renders `tlsCAFile`/`tokenPath` mount paths when the corresponding secrets are set |

### 3.3 Resource rendering — Deployment secret mounts (`internal/resources/deployments_test.go`, `Describe("Gateway and RemediationOrchestrator Fleet secret mounts")`)

| ID     | FedRAMP | Description                                                              |
|--------|---------|---------------------------------------------------------------------------|
| DP-101 | SC-8    | Neither deployment mounts `fleet-ca`/`fleet-token` volumes when fleet is disabled |
| DP-102 | SC-8    | Neither deployment mounts `fleet-ca`/`fleet-token` volumes when enabled but no secret names are set |
| DP-103 | SC-8    | Both Gateway and RemediationOrchestrator mount `fleet-ca` at `/etc/fleet-tls/ca` from `caSecretName` when set |
| DP-104 | SC-8    | Both Gateway and RemediationOrchestrator mount `fleet-token` at `/etc/fleet-token` from `tokenSecretName` when set |

## 4. Acceptance Criteria

- All scenarios above pass (`make test-unit`), plus all pre-existing
  `internal/resources` suites (632+ specs) with no regressions.
- `make test-integration` (envtest-backed `internal/controller` suite)
  passes with no changes required to existing fixtures.
- `make build` succeeds; `go vet ./...` is clean; `golangci-lint run`
  reports no new findings.
- `make manifests generate build-installer bundle` regenerates
  `config/crd/bases/kubernaut.ai_kubernauts.yaml`, `dist/install.yaml`, and
  the bundle CSV with the new `fleet` schema and no unrelated diffs.
- The `github.com/jordigilh/kubernaut` dependency in `go.mod` and the
  `RELATED_IMAGE_GATEWAY`/`RELATED_IMAGE_REMEDIATIONORCHESTRATOR` digests in
  `config/manager/manager.yaml` both resolve to revisions/builds that are
  `ahead` of the upstream ADR-068 commit (`a673c1f8`), confirmed via
  `gh api .../compare/a673c1f8...<ref>` reporting `ahead`, not `diverged`.
- No `valkeyAddr` field or `backend: "valkey"` value exists anywhere in the
  new CRD surface, sample CR, or documentation — this was the stale premise
  in the original issue filing.

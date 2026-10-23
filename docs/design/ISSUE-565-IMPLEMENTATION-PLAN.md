# Issue #565 — Administrator-owned external exposure implementation plan

**Status**: Implementation complete; live OCP E2E qualification pending registry access
**Design decision**: [`DD-565-EXTERNAL-EXPOSURE-BOUNDARY.md`](DD-565-EXTERNAL-EXPOSURE-BOUNDARY.md)
**Test plan**: [`../tests/565/TEST_PLAN.md`](../tests/565/TEST_PLAN.md)
**Control traceability**: [`../security/ISSUE-565-CONTROL-TRACEABILITY.md`](../security/ISSUE-565-CONTROL-TRACEABILITY.md)

## Scope

Remove CR-managed external exposure for Gateway, API Frontend, and Console.
Preserve internal Services, service TLS, Alertmanager’s internal Gateway FQDN,
application authentication configuration, and workload RBAC. Add the explicit
Console OAuth2 callback URL contract.

## TDD sequence

### RED

- CRD contract test rejects Route/Ingress properties under all three components.
- Generated manager-RBAC contract test rejects Route and Kubernetes Ingress
  permissions while preserving unrelated read-only operand permissions.
- Status test rejects `RouteReady` and requires internal/admin-owned
  `ExposureReady`.
- Console callback tests cover missing, malformed, plaintext, wrong-path, and
  valid administrator-owned HTTPS URLs.
- Internal Alertmanager URL test asserts the exact Service FQDN and path.
- Envtest lifecycle test proves Gateway readiness without any operator-managed
  external exposure API.

### GREEN

- Remove API fields/types and regenerate deepcopy/CRD artifacts.
- Remove Route/Ingress builders, reconciliation, watches, scheme registration,
  and manager RBAC.
- Remove platform-derived Console hostname resolution and require
  `spec.console.auth.redirectURL` when Console is enabled.
- Preserve internal Service and Alertmanager configuration wiring.
- Update existing lifecycle and ownership tests to remove route-disable fixtures.

### REFACTOR

- Add design, control-traceability, and test-plan artifacts.
- Update README, installation, platform-support, samples, E2E fixtures, and
  operator test-plan documentation.
- Regenerate bundle, chart CRD/RBAC copies, CSV, and installer artifacts.
- Verify no stale external-exposure symbols or manager permissions remain.

## Wiring manifest

| Production path | Required result | Evidence |
|---|---|---|
| `deployWorkloads` → external exposure reconciliation | Removed | `IT-EXP-565-RECONCILE-001` |
| `SetupWithManager` → external Route/Ingress watches | Removed | `UT-EXP-565-RBAC-001` |
| `reconcileMonitoringAndAlerts` → internal AlertmanagerConfig | Preserved | `UT-EXP-565-INTERNAL-001` |
| Console deployment → explicit callback URL | Preserved with admin-owned URL | `UT-EXP-565-CONSOLE-001/002`, lifecycle tests |

## Acceptance criteria

- `v1alpha2` publishes no Route/Ingress fields for Gateway, API Frontend, or Console.
- Manager RBAC grants no Route or Kubernetes Ingress management permissions.
- No operator code creates or reconciles external Route/Ingress resources.
- Console requires a valid explicit HTTPS callback URL when enabled.
- Gateway and Alertmanager continue using internal Service DNS and HTTPS.
- Generic/envtest lifecycle reaches readiness without external exposure APIs.
- Generated artifacts, docs, samples, and control evidence are synchronized.

## Verification

```text
go build ./...
golangci-lint run
make test-unit
make test-integration
make test-pyramid
make manifests generate
make bundle
git diff --check
```

Unit coverage must remain at least 90%; the project’s zero-missed-business-
statement gate is preferred. No tests are added solely to raise a percentage.
The live qualification lane uses `helios08` and the reachable
`kubernaut492-sno-a` or `kubernaut492-sno-b` OpenShift 4.22.16 cluster.

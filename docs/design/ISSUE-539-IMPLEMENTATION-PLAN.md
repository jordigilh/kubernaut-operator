# Issue #539 — Gateway authentication contract

| Field | Value |
|---|---|
| Issue | [#539](https://github.com/jordigilh/kubernaut-operator/issues/539) |
| Parent audit | [#533](https://github.com/jordigilh/kubernaut-operator/issues/533) |
| Baseline | Helm `v1.6.0-rc24`; operator `origin/main` at `6dd8269` |
| API boundary | Unpublished `kubernaut.ai/v1alpha2`; clean-break changes are allowed |
| Status | Implementation complete; clean-branch validation recorded below |

## Objective

Expose Gateway authentication as one coherent operator-owned CRD contract. The
default ingress adapter is AlertManager, whose bearer credential and the
Gateway's TokenReview/SAR enforcement policy belong under the same Gateway auth
boundary.

The change must preserve the operator's current fail-closed default, keep
administrator-owned credentials read-only, and prove the production reconciliation
path rather than only testing isolated struct fields.

## Effective API

```yaml
spec:
  gateway:
    auth:
      enabled: true
      alertManager:
        tokenSecretName: alertmanager-gateway-token
```

Design rules:

- `auth.enabled` defaults to `true`, preserving the current operator contract
  that unauthenticated Gateway signal requests are rejected. An explicit
  `false` is an administrator-selected insecure mode and must be observable.
- `auth.alertManager.tokenSecretName` is a Secret name in the Kubernaut CR
  namespace. The Secret must contain `token`; its bytes must never enter a
  ConfigMap, status, event, or log.
- When authentication is disabled, the AlertmanagerConfig remains routable but
  does not require or reference the bearer Secret, and the
  `AlertManagerAuthConfigured` condition reports an intentional disabled state.
- Remove the unpublished flat
  `spec.gateway.alertManagerTokenSecretName` field; no compatibility alias is
  required. The replacement is `spec.gateway.auth.alertManager.tokenSecretName`.
- Do not add generic `signalSources`, Kubernetes Events, service type/NodePort,
  or deferred workload-tuning fields in this issue.

## Preflight findings

### Existing implementation and callers

| Concern | Current evidence | Finding | Planned impact |
|---|---|---|---|
| CRD field | `api/v1alpha2/kubernaut_types.go:GatewaySpec` | Flat `AlertManagerTokenSecretName` exists; no Gateway auth block exists. | Replace with `GatewayAuthSpec` and `GatewayAlertManagerAuthSpec`. |
| Runtime config | `internal/resources/configmaps.go:gatewayServerYAML`, `gatewayServerConfig`, `GatewayConfigMap` | Server configuration has no `authenticationEnabled` key. | Render the effective auth default/override as `server.authenticationEnabled`. |
| AlertManager adapter | `internal/resources/ocp.go:GatewayAlertManagerConfig` | Bearer Secret reference is already rendered from the flat field. | Read the nested field and omit authorization when auth is explicitly disabled. |
| Status validation | `internal/controller/kubernaut_controller.go:validateAlertManagerAuthConfig` | Secret existence/key validation is already non-blocking and status-visible. | Add disabled-state handling and update field paths/messages. |
| Production wiring | `internal/controller/kubernaut_controller.go:deployConfigMaps`, `reconcileMonitoringAndAlerts`, `phaseRunning` | Existing callers reach both renderers and status validation. | No new builder or reconciliation phase; update existing paths. |
| RBAC | `internal/resources/rbac.go:gatewayClusterRole`, `gatewaySignalSourceClusterRole`, `monitoringClusterRoleBindings` | Gateway TokenReview/SAR and AlertManager source binding already exist. | Preserve exact permissions; add no generic source RBAC. |
| Tests | `internal/resources/ocp_test.go`, `configmaps_test.go`, `internal/controller/kubernaut_lifecycle_test.go`, `business_errors_unit_test.go` | Business tests already cover token reference, Secret failure states, and lifecycle wiring. | Extend existing tests with nested API, runtime toggle, and disabled behavior. |
| Generated API | `api/v1alpha2/zz_generated.deepcopy.go`, `config/crd/bases/`, bundle/dist/sample manifests | Generated artifacts currently expose the flat field. | Regenerate and verify all tracked API artifacts. |

### Upstream contract spike

No further spike is required. The pinned rc24 artifacts establish the runtime
contract:

- `values.schema.json` defines `gateway.auth.enabled` and a schema-only
  `gateway.auth.signalSources` list.
- `gateway.yaml` renders `server.authenticationEnabled` and grants the
  Gateway ServiceAccount TokenReview/SAR access.
- The rc24 Gateway template does not consume or range over `signalSources`.
- No upstream Gateway Kubernetes-Events ingress adapter or template contract
  was found.

The operator will carry the functional auth contract, not the unused Helm
schema-only source list.

### Baseline checks

The isolated branch is based exactly on `origin/main` at `6dd8269`. Stale,
unrelated #537 provider-policy dependency-egress changes were removed before
validation; no `internal/policy` changes remain in this Issue #539 diff.

Passed before implementation:

- `go build ./...`
- `go test ./api/... ./internal/resources/... -count=1`
- `go test ./internal/controller/... -run '^$' -count=1`
- `go vet ./...`
- Focused resource coverage: `100.0%`

The final `golangci-lint run` is clean after the unrelated #537 provider-policy
changes were removed. The controller manager wiring tests explicitly disable
controller-name validation only for their short-lived duplicate test managers;
production registration remains uniquely named and validation-enabled.

## Alternatives considered

1. **Selected — nested `gateway.auth` contract.** Groups the Gateway-wide
   authentication policy with the default AlertManager source credential,
   preserves a clean API boundary, and leaves room for a future typed source
   without inventing a generic RBAC list now.
2. **Rejected — retain the flat Secret field and add only `auth.enabled`.**
   Unnecessary compatibility complexity for an unpublished API and leaves
   related security settings split across unrelated Gateway fields.
3. **Rejected — expose generic `signalSources` or `kubernetesEvents` now.**
   rc24 does not consume `signalSources`, and the operator has no Gateway Events
   ingress adapter. These would be dead or misleading API surface.

## Wiring manifest

| Component | Production entry point | Wiring location | Integration evidence |
|---|---|---|---|
| Gateway auth policy | Gateway ConfigMap `server.authenticationEnabled` | `GatewayConfigMap` → `gatewayServerConfig` → existing `deployConfigMaps` path | `IT-539-GW-AUTH-RECONCILE` |
| AlertManager source credential | AlertmanagerConfig webhook `Authorization` reference | `GatewayAlertManagerConfig` → existing `reconcileMonitoringAndAlerts` path | `IT-539-GW-AUTH-RECONCILE` |
| Auth readiness | `AlertManagerAuthConfigured` condition | `validateAlertManagerAuthConfig` → existing `phaseRunning` status path | `IT-539-GW-AUTH-CONDITION` |

No new resource builder is introduced. Checkpoint W will verify that the
existing ConfigMap, AlertmanagerConfig, and status callers remain live.

## TDD implementation sequence

### RED

Add business-level failing tests before production edits:

- `UT-539-GW-AUTH-API-001`: generated CRD exposes nested `gateway.auth`,
  publishes `enabled=true`, exposes `alertManager.tokenSecretName`, and no
  longer exposes the flat field, generic signal sources, or Kubernetes Events.
- `UT-539-GW-AUTH-CM-001`: an unset auth block renders
  `server.authenticationEnabled: true`; an explicit false renders false.
- `UT-539-GW-AUTH-AM-001`: a nested token Secret is referenced as a Bearer
  credential without copying token bytes into the AlertmanagerConfig.
- `UT-539-GW-AUTH-AM-002`: disabled authentication does not reference the
  bearer Secret; TLS routing remains present.
- Existing `AM1`/`AM3`/`AM4` lifecycle assertions and
  `UT-539-GW-AUTH-CONDITION-001`: enabled authentication reports missing
  Secret/key as actionable false conditions; `IT-539-GW-AUTH-CONDITION`
  verifies disabled authentication reports a deliberate disabled condition
  without a false missing-credential warning.
- `IT-539-GW-AUTH-RECONCILE`: a real envtest reconciliation produces the
  ConfigMap, AlertmanagerConfig, and condition from the nested CRD contract.

Tests will reuse the production YAML structs in white-box resource tests and
will assert rendered behavior, status, and credential boundaries—not merely
field existence. No coverage-only tests will be added.

### GREEN

- Add the two typed auth structs and effective-default helper to
  `api/v1alpha2/kubernaut_types.go`.
- Replace all production/test/sample/docs references to the flat field.
- Render `authenticationEnabled` in `gatewayServerYAML`.
- Update `GatewayAlertManagerConfig` and
  `validateAlertManagerAuthConfig` for nested auth and explicit disablement.
- Run Checkpoint W through the envtest reconciliation test.

### REFACTOR

- Regenerate deepcopy and CRD artifacts with `make generate` and
  `make manifests`; synchronize bundle, dist, chart-copy, and sample artifacts
  using the repository's established generation workflow.
- Update installation and security documentation.
- Add `docs/security/ISSUE-539-CONTROL-TRACEABILITY.md` and validate it with
  `make test-security-traceability`.
- Run formatting, build, vet, focused tests, unit/integration suites, and the
  repository test-pyramid checks without altering unrelated worktree changes.

## Pyramid and control-objective evidence

These are engineering evidence mappings, not claims of FedRAMP authorization,
SOC 2 attestation, or OWASP ASVS conformance.

| Business assertion | Unit evidence | Integration evidence | FedRAMP/NIST objective | SOC 2 | OWASP ASVS 5.0.0 |
|---|---|---|---|---|---|
| Gateway authentication is enabled by default and the explicit policy reaches the runtime config. | `UT-539-GW-AUTH-CM-001` | `IT-539-GW-AUTH-RECONCILE` | IA-2, AC-3, CM-6 | CC6.1, CC8.1 | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1`, `v5.0.0-V16.5.2` |
| AlertManager uses the administrator-supplied bearer Secret and the existing TokenReview/SAR authorization boundary; no token bytes are rendered or logged. | `UT-539-GW-AUTH-AM-001`, existing RBAC tests | `IT-539-GW-AUTH-RECONCILE` | AC-3, AC-6, IA-5 | CC6.1, CC6.6 | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1`, `v5.0.0-V13.3.1`, `v5.0.0-V13.3.2` |
| Missing or malformed Secret material is visible and never silently converted into a working authenticated configuration. | Existing `AM1`/`AM3`/`AM4` lifecycle assertions; `UT-539-GW-AUTH-CONDITION-001` | `IT-539-GW-AUTH-CONDITION` | IA-5, SI-10, SI-4 | CC7.2, CC8.1 | `v5.0.0-V2.2.1`, `v5.0.0-V16.1.1`, `v5.0.0-V16.5.2` |
| The unpublished API has one deterministic nested contract with no dead legacy/source fields. | `UT-539-GW-AUTH-API-001` | generated-manifest and reconciliation checks | CM-3, CM-6, CM-8, SI-10 | CC8.1 | `v5.0.0-V2.2.1`, `v5.0.0-V15.2.4`, `v5.0.0-V16.5.2` |

Unit tests will preserve the repository's configured `>=90%` business-unit
threshold; the modified Gateway resource paths are expected to remain at or
above 90% through meaningful default, override, failure, and security-boundary
cases. The operator repository cannot prove the upstream Gateway binary's
runtime SAR implementation or a formal compliance claim; those remain the
upstream/application and deployment qualification boundaries.

## Validation gates

```bash
go build ./...
go vet ./...
golangci-lint run
make manifests generate
make test-unit
make test-integration
make test-security-traceability
make test-pyramid
```

## Risks and mitigations

- **Runtime key mismatch:** use the exact rc24 `server.authenticationEnabled`
  key and assert the rendered YAML.
- **Security regression from defaulting:** default `enabled` to true and test
  both unset and explicit false behavior.
- **Credential leakage:** use SecretKeySelector references only; assert token
  values never occur in ConfigMaps, status, messages, or events.
- **False readiness:** retain non-blocking but explicit status conditions for
  invalid AlertManager credentials; never claim configured when required key
  material is absent.
- **Unrelated worktree lint failure:** do not reformat or refactor the existing
  policy changes; report the baseline findings separately.

## Confidence

**96% preflight confidence.** The API, renderer, controller, AlertManager
adapter, RBAC, generated schema, upstream rc24 contract, and existing tests were
cross-checked. The implementation is a focused clean-break API change with no
new resource lifecycle or external dependency. Remaining risk is limited to
the exact upstream application handling of the `authenticationEnabled` key and
generated-artifact synchronization; both are covered by the pinned rc24
contract and planned manifest tests.

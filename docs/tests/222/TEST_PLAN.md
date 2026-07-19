# IEEE 829 Test Plan — Issue #222: spec.fleet crash-loops Gateway/RemediationOrchestrator (missing mcpGatewayEndpoint)

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-222                                             |
| **Issue**          | #222 — `spec.fleet.enabled` crash-loops Gateway and RemediationOrchestrator (missing `mcpGatewayEndpoint`) |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-07-19                                         |
| **Scope**          | `api/v1alpha1/kubernaut_types.go` (`FleetSpec`), `internal/resources/validation.go`, `internal/resources/configmaps.go`, `internal/resources/deployments.go` |

## 1. Objective

#180 introduced `spec.fleet` (`FleetSpec`) but only rendered `backend`/
`endpoint`/`tlsCAFile`/`tokenPath` into Gateway's and RemediationOrchestrator's
ConfigMaps. Upstream `kubernaut` added a stricter startup check *after* #180
was scoped: both components' `ServerConfig.Validate()` unconditionally call
`Fleet.ValidateFullFederation()`, which requires `mcpGatewayEndpoint` (+
`mcpGatewayType`) whenever `fleet.enabled=true`. Since the operator never
rendered that field, enabling fleet crash-loops both components at startup
(`fleet: mcpGatewayEndpoint is required...`).

Verify that:

1. `spec.fleet` gains `mcpGatewayEndpoint`, `mcpGatewayType`, and `oauth2`
   fields — shared config, not specific to any one backend
   (`MCPGatewayEndpoint`/`MCPGatewayType`/`OAuth2` live on upstream's
   *shared* `pkg/fleet.FleetConfig`, consumed today by GW/RO/AF/EM).
2. Admission validation (`ValidateKubernaut`) rejects a fleet-enabled CR
   missing `mcpGatewayEndpoint` or `mcpGatewayType` (or with an unsupported
   `mcpGatewayType`), so the crash-loop is caught at admission instead of at
   pod startup.
3. Admission validation rejects `oauth2.enabled=true` missing `tokenURL` or
   `credentialsSecretRef` (mirrors upstream `FleetOAuth2Config`'s own pairing
   check — an unpaired config would silently send unauthenticated requests
   to the MCP Gateway).
4. Gateway's and RemediationOrchestrator's rendered ConfigMaps include
   `mcpGatewayEndpoint`/`mcpGatewayType` and, when enabled, an `oauth2:`
   block with `tokenURL`/`credentialsSecretRef`/`scopes`.
5. When `fleet.oauth2.enabled=true` and `credentialsSecretRef` is set, both
   Deployments mount that Secret — Gateway at
   `/etc/gateway/<credentialsSecretRef>`, RemediationOrchestrator at
   `/etc/remediationorchestrator/<credentialsSecretRef>` — matching the
   literal path each binary builds at runtime
   (`cmd/gateway/main.go:buildFleetOAuth2Option`,
   `cmd/remediationorchestrator/main.go:buildFleetReaderFactory`).
6. All pre-existing Fleet Config Validation / ConfigMap / Deployment mount
   tests remain green (backward compatible schema addition, no field
   removed or renamed).

**Explicitly out of scope**: `mcpGatewayNamespace` (restricting remote-cluster
resource discovery to a specific namespace, via upstream
`registry.EAIGWRegistryConfig.Namespace`) was considered but deliberately
left out — Gateway and RemediationOrchestrator have no consumer for it
today, so adding it here would be unvalidated, unrendered, speculative CRD
surface. It belongs in kubernaut-operator#200 (FMC), landing alongside the
code that actually reads it.

## 2. Test Strategy

Standard TDD (RED → GREEN → REFACTOR), unit-tier only — this is a pure
CRD-schema + Go-struct rendering change with no new Kubernetes API
interaction, so envtest/integration/E2E tiers are inapplicable (Pyramid
Invariant satisfied at the unit tier by design, not skipped for convenience).
Extends the existing Ginkgo suites in `internal/resources` (`validation_test.go`,
`configmaps_test.go`, `deployments_test.go`) rather than introducing new
test files, since this is additive to the #180 `FleetSpec` surface they
already cover.

## 3. Test Scenarios

### 3.1 Admission validation (`internal/resources/validation_test.go`)

| ID      | FedRAMP | Description | Automated? |
|---------|---------|-------------|------------|
| FL-012  | SC-8    | rejects fleet enabled with no `mcpGatewayEndpoint` | Yes |
| FL-013  | SC-8    | rejects fleet enabled with `mcpGatewayEndpoint` set but no `mcpGatewayType` | Yes |
| FL-014  | SC-8    | rejects fleet enabled with an unsupported `mcpGatewayType` value | Yes |
| FL-015  | SC-8    | accepts fleet enabled with `mcpGatewayType=kuadrant` | Yes |
| FL-016  | IA-5    | rejects `fleet.oauth2.enabled=true` with no `tokenURL` | Yes |
| FL-017  | IA-5    | rejects `fleet.oauth2.enabled=true` with no `credentialsSecretRef` | Yes |
| FL-018  | IA-5    | accepts `fleet.oauth2.enabled=true` with both fields set | Yes |

Eight pre-existing Fleet Config Validation tests (empty backend, unsupported
backend, empty endpoint, valid fleetmetadatacache/acm configs, FL-010,
FL-011) were updated to additionally set `mcpGatewayEndpoint`/`mcpGatewayType`
in their fixtures, so each continues to isolate exactly the one rule it was
written to test instead of also tripping the new mandatory fields.

### 3.2 ConfigMap rendering (`internal/resources/configmaps_test.go`)

| ID | Description | Automated? |
|----|--------------|------------|
| — | Gateway config renders `mcpGatewayEndpoint`/`mcpGatewayType` when set | Yes |
| — | Gateway config omits the `oauth2:` block when fleet oauth2 is disabled | Yes |
| — | Gateway config renders the `oauth2:` block (`enabled`/`tokenURL`/`credentialsSecretRef`) when enabled | Yes |
| — | RemediationOrchestrator config renders `mcpGatewayEndpoint`/`mcpGatewayType` when set | Yes |
| — | RemediationOrchestrator config renders the `oauth2:` block when enabled | Yes |

### 3.3 Deployment mounts (`internal/resources/deployments_test.go`)

| ID | Description | Automated? |
|----|--------------|------------|
| — | no `fleet-oauth2` volume on either Deployment when `fleet.oauth2.enabled` is false | Yes |
| — | Gateway mounts `fleet-oauth2` at `/etc/gateway/<credentialsSecretRef>` when enabled | Yes |
| — | RemediationOrchestrator mounts `fleet-oauth2` at `/etc/remediationorchestrator/<credentialsSecretRef>` when enabled | Yes |

## 4. Acceptance Criteria

- All scenarios above pass via `make test-unit` (`internal/resources`
  coverage ≥ 89%, matching pre-change baseline).
- `golangci-lint run ./...` reports 0 issues.
- `make manifests generate build-installer bundle` regenerated
  `config/crd/bases/kubernaut.ai_kubernauts.yaml`, `dist/install.yaml`,
  `bundle/manifests/*` with the new fields; operator's own image reference
  in `dist/install.yaml` re-pinned to its digest (kustomize resets it to a
  tag on every regen — known, documented drift from #180/#219).
- `config/samples/v1alpha1_kubernaut.yaml` and
  `docs/installation/03-deploy.md` document the new required
  `mcpGatewayEndpoint`/`mcpGatewayType` fields alongside the existing
  `backend`/`endpoint` fields.
- No behavior change for CRs that leave `spec.fleet.enabled` unset/false
  (fully backward compatible with #180).

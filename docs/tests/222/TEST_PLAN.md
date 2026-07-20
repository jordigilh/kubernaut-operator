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

### 1.1 Addendum: per-component OAuth2 client override

The original design in this plan rendered a single, shared
`spec.fleet.oauth2` block identically into both Gateway's and
RemediationOrchestrator's ConfigMaps — i.e. both components would
necessarily authenticate to the MCP Gateway with the *same* OAuth2 client.
Grounded review of upstream's own Helm chart
(`charts/kubernaut/templates/_helpers.tpl`, `kubernaut.fleet.oauth2`) shows
this is not how upstream itself models it: `global.fleet.oauth2.*` is a
fleet-wide *default*, and each service's own `<service>.fleet.oauth2.*`
(`tokenURL`/`credentialsSecretRef`/`scopes`/`tlsCAFile`) overrides it
per-field when set — resolved to one flat value per component before
being used for both the rendered config and the Secret volume mount. This
is the documented, working mechanism for a federated IdP (e.g. Keycloak)
issuing distinct per-service client registrations against a single token
endpoint — not a rare edge case.

Scope for this addendum (deliberately narrower than full Helm parity):
only `credentialsSecretRef` — the field that is inherently
per-registered-client — gains a per-component override, added to
`GatewaySpec`/`RemediationOrchestratorSpec`. `tokenURL`/`scopes`/`tlsCAFile`
stay fleet-wide only (same Keycloak realm/CA for every fleet-aware
component in practice, matching upstream's own comment that "in practice
they share one ... OAuth2 client" for those fields). Overriding just
`credentialsSecretRef` is sufficient to let each component authenticate as
its own distinct OAuth2 client against the same shared token endpoint.

Additional verification for this addendum:

- `spec.gateway.fleetOAuth2CredentialsSecretRef` and
  `spec.remediationOrchestrator.fleetOAuth2CredentialsSecretRef`
  (optional, per-component), when set, override
  `spec.fleet.oauth2.credentialsSecretRef` for that component only —
  admission validation, ConfigMap rendering, and Secret volume mounts all
  resolve the *effective* value (override-or-shared) per component.
- Admission validation's `credentialsSecretRef`-required check
  (`fleet.oauth2.enabled=true`) is satisfied by either the shared field or
  the component's own override — not the shared field alone.

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
| FL-019  | IA-5    | accepts `fleet.oauth2.enabled=true` with no shared `credentialsSecretRef` when both `gateway.fleetOAuth2CredentialsSecretRef` and `remediationOrchestrator.fleetOAuth2CredentialsSecretRef` are set | Yes |
| FL-020  | IA-5    | accepts a shared `credentialsSecretRef` while `gateway.fleetOAuth2CredentialsSecretRef` overrides Gateway's own (mixed; RO falls back to the shared value) | Yes |
| FL-021  | IA-5    | rejects when `gateway.fleetOAuth2CredentialsSecretRef` is set but RemediationOrchestrator has neither an override nor a shared fallback | Yes |
| FL-022  | IA-5    | rejects when `remediationOrchestrator.fleetOAuth2CredentialsSecretRef` is set but Gateway has neither an override nor a shared fallback | Yes |

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
| — | Gateway config renders `oauth2.credentialsSecretRef` from `gateway.fleetOAuth2CredentialsSecretRef` when set, overriding the shared value | Yes |
| — | RemediationOrchestrator config renders `oauth2.credentialsSecretRef` from `remediationOrchestrator.fleetOAuth2CredentialsSecretRef` when set, overriding the shared value | Yes |

### 3.3 Deployment mounts (`internal/resources/deployments_test.go`)

| ID | Description | Automated? |
|----|--------------|------------|
| — | no `fleet-oauth2` volume on either Deployment when `fleet.oauth2.enabled` is false | Yes |
| — | Gateway mounts `fleet-oauth2` at `/etc/gateway/<credentialsSecretRef>` when enabled | Yes |
| — | RemediationOrchestrator mounts `fleet-oauth2` at `/etc/remediationorchestrator/<credentialsSecretRef>` when enabled | Yes |
| — | Gateway mounts `fleet-oauth2` using its own override secret name when `gateway.fleetOAuth2CredentialsSecretRef` is set, not the shared one | Yes |
| — | RemediationOrchestrator mounts `fleet-oauth2` using its own override secret name when `remediationOrchestrator.fleetOAuth2CredentialsSecretRef` is set, not the shared one | Yes |

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

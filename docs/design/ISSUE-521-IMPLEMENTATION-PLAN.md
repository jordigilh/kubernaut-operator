# Issue #521 — v1alpha2 Fleet API contract implementation plan

| Field | Value |
|---|---|
| Issue | [#521 — v1alpha2 Fleet API: triage and finalize provider-scoped contract](https://github.com/jordigilh/kubernaut-operator/issues/521) |
| Related work | #431, #499, #512, #486, kubernaut#2492/#2488 |
| Status | Implemented; local verification complete; no commit created |
| Release line | v1.6, unreleased v1alpha2 clean-break API |

## 1. Objective

Finalize a clean, provider-scoped `spec.fleet` CRD contract before v1alpha2 is
released, then update the operator implementation, generated artifacts, tests,
and security evidence exactly once.

The operator API is native and capability-oriented. Helm values and rendered
service YAML are behavioral references, not a migration or input-API contract.

## Execution status — 2026-10-13

The approved contract is implemented in the current worktree. The companion
decision/field matrix is recorded in
`docs/design/DD-521-FLEET-API-CONTRACT.md`; generated API artifacts, unit and
envtest evidence, security traceability, pyramid validation, and lint/build
results are complete. The remaining unchecked items below are planning
placeholders for explicitly out-of-scope upstream E2E or remote qualification
work, not claims that the local implementation is unfinished.

## 2. Approved design direction

### 2.1 Fleet activation and shared capabilities

- `spec.fleet.enabled` remains the sole Fleet activation flag.
- `mcpGateway`, `oauth2`, and `mcpClient` are shared capabilities.
- OAuth2 has no Fleet-specific `enabled` field; active Fleet requires OAuth2.
- One shared MCP-client retry/timeout policy applies to all Fleet-aware services.
- Component-level OAuth2 credential overrides remain the only per-component
  credential exception unless the field matrix approves another need.

### 2.2 Provider-discriminated scope check

The target shape is:

```yaml
spec:
  fleet:
    scopeCheck:
      backend: acm
      acm:
        endpoint: https://acm-search.example/graphql
        tls:
          source: secret
          caCertSecretRef:
            name: acm-search-ca
            key: ca.crt
        tokenSecretRef:
          name: acm-search-token
          key: token
```

FMC is a built-in, operator-managed service, not a BYO provider:

```yaml
spec:
  fleet:
    scopeCheck:
      backend: fleetmetadatacache
```

For FMC, the operator derives the HTTPS Service URL from the Kubernaut
namespace and uses mandatory inter-service trust. There is no user-facing FMC
endpoint, scope-check token, or provider-specific scope-check TLS override.

### 2.3 MCP-client tuning

Replace the broad `resilience` name with an operator-native API:

```yaml
spec:
  fleet:
    mcpClient:
      retry:
        initialInterval: 1s
        maxInterval: 30s
        maxElapsedTime: 5m
      timeouts:
        tokenRefresh: 10s
        connect: 30s
        discoverProbe: 5s
```

The resolver maps these names to the upstream `FleetResilienceConfig` fields:
`initialInterval`, `maxInterval`, `maxElapsedTime`, `tokenRefreshTimeout`,
`connectTimeout`, and `discoverProbeTimeout`.

## 3. Approved API design gate

The exact API direction and duration-validation policy were approved in the
planning session. The companion design decision and field matrix are recorded
in `docs/design/DD-521-FLEET-API-CONTRACT.md`.

```go
type FleetScopeCheckSpec struct {
	Backend string                         `json:"backend,omitempty"`
	ACM     *FleetACMScopeCheckSpec       `json:"acm,omitempty"`
}

type FleetACMScopeCheckSpec struct {
	Endpoint       string                  `json:"endpoint,omitempty"`
	TLS            *FleetTrustSpec         `json:"tls,omitempty"`
	TokenSecretRef *SecretKeyRef           `json:"tokenSecretRef,omitempty"`
}

type FleetMCPClientSpec struct {
	Retry    *FleetMCPClientRetrySpec     `json:"retry,omitempty"`
	Timeouts *FleetMCPClientTimeoutsSpec  `json:"timeouts,omitempty"`
}
```

### Recommended validation semantics

- Fleet disabled or omitted: all nested Fleet configuration is inert and may
  be pre-staged.
- Active `backend=acm`: `scopeCheck.acm` is required; endpoint must be an
  absolute HTTPS URL; `tokenSecretRef.name` is required; TLS is always HTTPS
  and defaults to inter-service trust when no override is supplied.
- Active `backend=fleetmetadatacache`: `scopeCheck.acm` must be absent; FMC
  endpoint and scope-check trust are derived by the operator.
- Unknown backends are rejected.
- Non-empty `mcpClient` duration values are validated at admission as positive
  Go duration strings; invalid values fail before dependent workloads deploy.

## 4. Field matrix TODO

- [x] Inventory every current `spec.fleet` field and consumer.
- [x] Record Helm correspondence and intentional differences.
- [x] Record defaulting, activation conditions, provider applicability, Secret
      contents, rendered output, and RBAC implications.
- [x] Classify each field as retain, move, rename, constrain, or remove.
- [x] Confirm the duration-validation decision for `mcpClient`: validate
      non-empty values as positive Go durations at admission.
- [x] Confirm whether any component-level namespace override has a supported
      operator use case; default disposition is intentional shared namespace.
- [x] Add the final field matrix to `docs/design/DD-521-FLEET-API-CONTRACT.md`.
- [x] Obtain final design approval before changing `api/v1alpha2`.
- [x] Write `docs/design/DD-521-FLEET-API-CONTRACT.md` and attach the final
      field matrix before implementation.

## 4.1 Blast-radius inventory

The implementation must audit and update every affected business path, not
only the CRD declaration.

### API and generated contract

- [x] `api/v1alpha2/kubernaut_types.go`: replace the current Fleet fields and
      add the approved provider/client types and validation markers.
- [x] `api/v1alpha2/zz_generated.deepcopy.go`: regenerate and inspect nested
      pointer copies.
- [x] `api/v1alpha2/clean_break_test.go`: assert the final JSON schema and
      absence of removed direct/legacy fields.
- [x] Regenerate and inspect `config/crd/bases/`, `bundle/manifests/`,
      `dist/install.yaml`, chart CRD copies, samples, and contract fixtures.

### Existing business logic

- [x] `internal/resources/validation.go`: update Fleet backend/provider
      validation, ACM endpoint/token checks, FMC derived-only behavior,
      trust validation paths, error messages, and duration validation.
- [x] `internal/resources/common.go`: refactor `resolveFleetEndpoint`, Fleet
      trust resolution, and the shared MCP-client configuration resolver.
- [x] `internal/resources/configmaps.go`: update Gateway, RO, AF, EM, SP,
      KA, and WE Fleet rendering; preserve runtime YAML compatibility with
      upstream while changing only the CRD input paths.
- [x] `internal/resources/deployments.go`: update ACM CA/token Secret mounts,
      read-only/key behavior, and negative MCP-only consumer boundaries.
- [x] `internal/resources/fleetmetadatacache.go`: preserve FMC's derived URL,
      inter-service trust, MCP Gateway OAuth2 override, and shared MCP-client
      tuning without introducing a scope-check endpoint input.
- [x] `internal/resources/rbac.go` and related helpers: verify MCP Gateway
      provider/namespace RBAC remains unchanged and no scope-check Secret
      permission is accidentally broadened.
- [x] Controller production paths: verify Fleet activation, validation,
      status/error propagation, resource pruning, and reconciliation callers
      do not depend on removed endpoint/token/TLS fields.
- [x] `internal/policy/intent.go` and other API consumers: confirm shared
      MCP Gateway intent remains consistent with the final fields.

### Existing tests and fixtures

- [x] Update all `FleetScopeCheckSpec` composite literals and helpers in
      `internal/resources/`.
- [x] Update Fleet ConfigMap tests for Gateway, RO, AF, EM, SP, KA, WE, and
      FMC; distinguish operator CRD input from rendered upstream YAML.
- [x] Update Deployment tests for ACM CA/token mounts, FMC no-token/no-BYO
      endpoint behavior, OAuth2 mounts, and MCP-only negative boundaries.
- [x] Update Fleet validation and admission tests for new field paths,
      provider combinations, error messages, and pre-staging behavior.
- [x] Update controller Fleet/envtest tests, including lifecycle, admission,
      RBAC namespace, FMC, Gateway-type, and WorkflowExecution fixtures.
- [x] Update API clean-break, sample CR, migration/contract, TLS, and common
      test helpers that construct Fleet objects.
- [x] Update active documentation and examples for removed fields; leave
      historical test-plan snapshots unchanged unless the design review
      explicitly classifies them as active contracts.

### 4.2 Current reference map captured during preflight

This is the concrete starting map for the implementation. Re-run the searches
after GREEN and require every old reference to be either migrated or explicitly
classified as historical evidence.

#### Production symbols and paths

- `internal/resources/validation.go`
  - `validateFleetConfig`
  - `validateFleetMetadataCacheEndpoint`
  - `validateFleetTrust` call sites for scope-check trust
  - current direct reads of `ScopeCheck.Endpoint`, `ScopeCheck.TLS`, and
    `ScopeCheck.TokenSecretRef`
- `internal/resources/common.go`
  - `FleetMetadataCacheURL`
  - `resolveFleetEndpoint`
  - `resolveFleetTrustCAFile`
  - `resolveFleetResilience`
- `internal/resources/configmaps.go`
  - `fleetConfigYAML`
  - `fleetResilienceYAML`
  - `resolveFleetConfig`
  - `resolveMCPGatewayOnlyFleetConfig`
  - `resolveAPIFrontendFleetConfig`
  - all Gateway/RO/AF/EM/SP/KA/WE Fleet render sites
- `internal/resources/deployments.go`
  - `appendFleetSecretMounts`
  - `appendFleetSecretMountsVariant`
  - `appendMCPGatewayOnlyFleetSecretMount`
- `internal/resources/fleetmetadatacache.go`
  - FMC config rendering and `resolveFleetResilience` consumption
- `internal/controller/kubernaut_controller.go`
  - controller-side Fleet trust validation and failure/status propagation
- `internal/resources/rbac.go`
  - provider/namespace RBAC paths to verify unchanged; no scope-check Secret
    RBAC may be added accidentally
- `internal/policy/intent.go`
  - shared MCP Gateway intent projection to verify after API changes

#### Existing tests that must be migrated or reclassified

- `api/v1alpha2/clean_break_test.go`
- `internal/resources/validation_test.go`
- `internal/resources/configmaps_test.go`
- `internal/resources/deployments_test.go`
- `internal/resources/fleetmetadatacache_test.go`
- `internal/resources/common_test.go`
- `internal/resources/tls_test.go`
- `internal/resources/tls_certmanager_test.go`
- `internal/resources/business_boundary_test.go`
- `internal/controller/kubernaut_controller_test.go`
- `internal/controller/kubernaut_v1alpha2_admission_test.go`
- `internal/controller/fleetmetadatacache_test.go`
- `internal/controller/workflowexecution_fleet_test.go`
- `internal/controller/mcpgatewaynamespacerbac_af_em_test.go`
- `internal/controller/business_api_boundaries_unit_test.go`

Known high-value test cases include current Fleet ConfigMap/Deployment paths,
FMC endpoint derivation, ACM missing-token validation, Secret-backed trust
validation, Gateway-type reconciliation, WorkflowExecution OAuth2 wiring, and
API admission. The migration must preserve their business assertions while
changing only the approved input contract.

#### Active/generated documentation and artifacts

- `config/samples/v1alpha2_kubernaut.yaml`
- `config/crd/bases/kubernaut.ai_kubernauts.yaml`
- `bundle/manifests/kubernaut.ai_kubernauts.yaml`
- `dist/install.yaml`
- chart CRD copies under `charts/`
- active Fleet installation examples, especially `docs/installation/03-deploy.md`
- `docs/design/ISSUE-499-IMPLEMENTATION-PLAN.md` (superseding API decision)
- `CHANGELOG.md` entries that describe the public Fleet API

Historical snapshots such as `docs/tests/180/`, `docs/tests/224/`, and the
original #421 audit should not be mechanically rewritten; the design review
must classify each reference as historical or active before changing it.

#### Compile-first migration checkpoint

- [x] After the API type change, run `go test ./... -run=^$` to enumerate
      residual compile-time references before behavior tests.
- [x] Resolve every compile error using the reference map above.
- [x] Run `go build ./...` before declaring GREEN.
- [x] Only then run the independent UT/IT suites and refactor behavior.

### 4.3 Proactive impact-map gate

The impact map is a pre-implementation gate, not a failure-response artifact:

- [x] Search production code, tests, generated artifacts, samples, active docs,
      and scripts for every field being removed or renamed.
- [x] For each result, record the owning function/test/document, intended new
      field path, expected behavior, and whether the reference is active or
      historical.
- [x] Identify all business behavior affected by the change: validation,
      defaults, admission/status errors, rendered ConfigMaps, Secret mounts,
      RBAC, endpoint derivation, cleanup, and rollout/readiness decisions.
- [x] Identify all test tiers affected and assign the test ID that will prove
      each business assertion before editing implementation code.
- [x] Review the completed map at the design gate; unresolved consumers block
      GREEN.
- [x] Use compile-only testing and the full UT/IT suites as confirmation of the
      map, never as the primary discovery mechanism.

## 5. TDD execution plan

### RED — contract and evidence gates

- [x] Add clean-break API assertions for the provider-discriminated schema.
- [x] Add tests expecting `mcpClient.retry`/`timeouts` rather than
      `spec.fleet.resilience`.
- [x] Add traceability/pyramid references for the planned evidence IDs so the
      gate fails until executable evidence and documentation exist.
- [x] Confirm no legacy flat Fleet fields or `spec.fleet.oauth2.enabled` remain
      in active samples, fixtures, manifests, or tests.

### GREEN — minimum implementation

- [x] Add the approved v1alpha2 types and markers.
- [x] Update `validateFleetConfig` for ACM/FMC provider semantics.
- [x] Update Fleet endpoint resolution so FMC is always derived.
- [x] Update Fleet trust resolution: ACM uses its provider block; FMC uses
      mandatory inter-service trust.
- [x] Update config-map resolvers and deployment Secret mounts.
- [x] Update the MCP-client resolver from `mcpClient` to upstream runtime YAML.
- [x] Update FMC, Gateway, RemediationOrchestrator, APIFrontend, SignalProcessing,
      EffectivenessMonitor, Kubernaut Agent, and WorkflowExecution fixtures as
      applicable.

### REFACTOR — production implementation and test quality

This phase is a real TDD refactor after GREEN, not a documentation phase.
Enhance the existing Fleet resolver, validation, builders, and tests; do not
introduce a second Fleet configuration path or defer cleanup to documentation.

- [x] Remove obsolete direct `scopeCheck.endpoint`, `scopeCheck.tls`, and
      `scopeCheck.tokenSecretRef` branches from production code.
- [x] Refactor the existing Fleet resolution path so ACM provider data is
      resolved once and FMC remains derived-only; avoid duplicated backend
      conditionals across ConfigMap and Deployment builders.
- [x] Rename/refactor the existing `FleetResilienceSpec` resolution path to
      the approved `mcpClient.retry/timeouts` contract and preserve the exact
      upstream runtime field mapping.
- [x] Remove dead explicit-FMC-endpoint validation and resolver behavior.
- [x] Refactor error construction so provider-specific paths and invalid
      combinations are actionable and consistently named.
- [x] Refactor repetitive Fleet fixtures/assertions into focused existing test
      helpers or table-driven Ginkgo cases where that improves clarity; retain
      business-level assertions rather than weakening them.
- [x] Add/repair Ginkgo/Gomega assertions for ACM endpoint, trust, token
      Secret/key, read-only mounts, and FMC derived endpoint behavior.
- [x] Add envtest coverage through actual `Reconcile` for the ACM path and the
      FMC derived-only path.
- [x] Add negative tests for plaintext ACM endpoints, missing ACM token,
      mismatched provider blocks, and invalid MCP-client durations.
- [x] Run `go build ./...` immediately after refactoring, before documentation
      or evidence-matrix work.

### Evidence/documentation after REFACTOR

- [x] Add complete control tags and evidence IDs to the finalized tests.
- [x] Update active installation documentation and examples.
- [x] Update `docs/tests/431/TEST_PLAN.md` and
      `docs/security/ISSUE-431-CONTROL-TRACEABILITY.md` after the schema and
      implementation settle.
- [x] Update `DD-521`, `docs/security/ISSUE-431-CONTROL-TRACEABILITY.md`, and
      the traceability/pyramid validators so documentation cannot claim
      unexecuted evidence.

## 6. Wiring manifest

| Component | Production entry point | Wiring location | Required evidence |
|---|---|---|---|
| ACM scope-check config | Fleet config resolver | `internal/resources/configmaps.go` | `UT-FLEET-521-ACM-*`, `IT-FLEET-521-RECONCILE-*` |
| ACM CA/token mounts | Fleet deployment mount helper | `internal/resources/deployments.go` | `UT-FLEET-521-SECRET-*`, reconciliation assertions |
| FMC endpoint/trust | Derived Service URL and inter-service trust | `internal/resources/common.go`, config builders | `UT-FLEET-521-FMC-*`, envtest wiring |
| MCP client tuning | Shared Fleet config rendering | `internal/resources/configmaps.go` | `UT-FLEET-521-MCPCLIENT-*` |
| Fleet validation | `ValidateFleet` / admission path | `internal/resources/validation.go`, controller admission tests | `UT-FLEET-521-VALIDATE-*`, `IT-FLEET-521-ADMISSION-*` |

Checkpoint W requires a production caller, focused unit evidence, and actual
controller wiring evidence for every new resolver or contract path.

## 7. Security and business evidence

Tests must assert business effects, not merely field presence:

- ACM endpoint is HTTPS and trust is certificate-verifying.
- ACM bearer token is mounted from the referenced Secret/key, read-only, and
  used at the rendered token path.
- FMC uses the operator-derived HTTPS Service and inter-service trust without
  exposing a BYO endpoint or token surface.
- MCP retry/timeouts are rendered consistently to every Fleet-aware consumer.
- Invalid or ambiguous provider configuration fails closed before deployment.

Control mapping must cover the applicable objectives:

- FedRAMP/NIST: `AC-6`, `IA-5`, `SC-8`, `SC-12`, `SC-13`, `CM-6`, `SI-10`.
- SOC 2: `CC6.1`, `CC6.6`, `CC8`.
- OWASP ASVS 5.0.0: applicable `V2.2.x`, `V12.3.x`, and `V13.3.x`.

The matrix must state that repository evidence is not a formal FedRAMP,
SOC 2, or ASVS certification claim. Full Fleet E2E qualification remains
upstream-owned under kubernaut#2492/#2488.

## 8. Generated artifacts and verification record

- [x] Run `make manifests generate bundle build-installer`.
- [x] Review only expected Fleet schema/generated-artifact changes.
- [x] Run the independent unit suite locally with `make test-unit`.
- [x] Run the independent envtest/controller suite locally with
      `make test-integration`.
- [x] Remote fallback was not needed; local envtest binaries were available.
- [x] Confirm both independent tiers pass; a combined `make test` result must
      not replace the separate UT and IT evidence.
- [x] Run `make test-security-traceability`.
- [x] Run `make test-pyramid`.
- [x] Run `make test-hack-scripts`.
- [x] Run `make test`.
- [x] Run `go build ./...` and `golangci-lint run`.
- [x] Review `git diff --exit-code -- api config bundle dist`; the nonzero
      result is expected because these artifacts contain the approved contract.
- [x] Review and correct the pre-existing change in
      `docs/security/ISSUE-513-CONTROL-TRACEABILITY.md` after explicit user
      authorization; its traceability gate now passes.

## 9. Completion gate

The work is complete only when the design decision, field matrix, generated
artifacts, unit tests, envtest wiring, control matrix, and all applicable local
gates pass. No commit or release qualification is part of this planning step.

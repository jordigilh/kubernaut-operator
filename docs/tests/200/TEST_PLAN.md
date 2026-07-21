# IEEE 829 Test Plan — Issue #200: Add FMC (Fleet Metadata Cache) service to operator CRD and reconciliation

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-200                                             |
| **Issue**          | #200 — Add FMC (Fleet Metadata Cache) service to operator CRD and reconciliation |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-07-20                                         |
| **Scope**          | `api/v1alpha1/kubernaut_types.go` (`FleetMetadataCacheSpec`), `internal/resources/validation.go`, `internal/resources/fleetmetadatacache.go` (new), `internal/resources/{common,rbac,services,networkpolicies,deployments,serviceaccounts}.go`, `internal/controller/kubernaut_controller.go` |

## 1. Objective

ADR-068 (fleet federation) lets Gateway and RemediationOrchestrator
scope-check against either an existing RHACM Search installation
(`spec.fleet.backend: acm`) or a Fleet Metadata Cache (`backend:
fleetmetadatacache`). #180/#223 added the shared `spec.fleet` config
consumed by GW/RO, but neither the operator nor any other issue stood up
FMC itself — a BYO FMC deployment was the only option for
`backend: fleetmetadatacache`.

Verify that:

1. `spec.fleetMetadataCache` (`FleetMetadataCacheSpec`) lets the operator
   deploy and manage FMC directly: `enabled` (default `false` — most fleet
   deployments use `backend: acm` instead), `mcpGatewayNamespace`,
   `fleetOAuth2CredentialsSecretRef` (per-component OAuth2 override,
   mirroring GW/RO's own from #223), `syncInterval`, `keyTTL`, `logging`,
   `resources`.
2. Admission validation (`validateFleetMetadataCache`) enforces FMC's own
   mandatory fields — `spec.fleet.mcpGatewayEndpoint`/`mcpGatewayType` and a
   paired `oauth2` block — **independent of `spec.fleet.enabled`**: FMC's
   job (polling the MCP Gateway) is orthogonal to whether GW/RO are
   configured to consume it for scope-checking.
3. `spec.fleet.endpoint` becomes optional specifically when
   `backend: fleetmetadatacache` AND `fleetMetadataCache.enabled: true` —
   the operator auto-derives FMC's in-cluster URL
   (`resolveFleetEndpoint`/`FleetMetadataCacheURL`) so the user isn't
   required to hand-wire the address of a service the operator itself is
   about to create. BYO FMC (`fleetMetadataCache.enabled: false`) and
   `backend: acm` still require an explicit endpoint (unchanged).
4. The operator renders FMC's ConfigMap
   (`server`/`mcpGateway`/`valkey`/`sync`/`oauth2` — matching upstream's
   `pkg/fleet/fmc/config.ServiceConfig` field names/nesting exactly, since
   FMC's `LoadFromFile` unmarshals directly into that struct), Deployment
   (api :8080 + metrics :8081, config + fleet-oauth2 volume mounts),
   Service, cluster-scoped RBAC (gatewayType-conditional CRD watch rules,
   matching upstream's own Helm chart), and NetworkPolicy only when
   `fleetMetadataCache.enabled: true`, and cleans all of it up (except the
   ServiceAccount, matching the AuthWebhook/APIFrontend precedent) when
   disabled after having been enabled.
5. FMC participates in the operator's per-service status rollup
   (`status.services`) and default `PodDisruptionBudget` once active, the
   same as every other component in `AllComponents()`.
6. All pre-existing Fleet Config Validation / ConfigMap / Deployment /
   controller lifecycle tests remain green (additive change; FMC defaults
   fully disabled/inert).

**Explicitly out of scope** (tracked separately):

- SignalProcessing/APIFrontend/EffectivenessMonitor fleet wiring —
  [#224](https://github.com/jordigilh/kubernaut-operator/issues/224).
- Least-privilege (namespace-scoped) RBAC for MCP Gateway CRD watches —
  blocked on upstream Helm chart support,
  [kubernaut#1686](https://github.com/jordigilh/kubernaut/issues/1686). FMC's
  `ClusterRole` here is cluster-scoped regardless of
  `mcpGatewayNamespace`, deliberately matching upstream's own current
  behavior rather than inventing an operator-only capability ahead of it.
- `OAuth2.TLSCAFile` (upstream `FleetOAuth2Config` has it; the operator's
  `OAuth2Spec` and rendered ConfigMaps for GW/RO don't yet) — pre-existing
  gap from the #223 triage, not introduced or widened here.

## 2. Test Strategy

Standard TDD (RED -> GREEN -> REFACTOR), spanning both tiers of the
Pyramid Invariant:

- **Unit tier** (`internal/resources`, new `fleetmetadatacache_test.go` +
  extensions to `validation_test.go`/`common_test.go`): CRD defaulting,
  admission validation, ConfigMap/Deployment/Service/RBAC/NetworkPolicy
  builder output — no Kubernetes API interaction.
- **Integration tier** (`internal/controller`, new
  `fleetmetadatacache_test.go`, envtest-backed): full reconcile lifecycle —
  enable creates all five resource kinds, disable-after-enable cleans them
  up, the SA is created unconditionally, admission rejection surfaces as
  `PhaseError`, and FMC is included in `status.services` once `Running`.

This is broader than #222's unit-only scope because FMC is a genuinely new
managed component (new Deployment/Service/RBAC/NetworkPolicy/controller
wiring), not a rendering-only addition to two pre-existing components.

## 3. Test Scenarios

### 3.1 Admission validation (`internal/resources/validation_test.go`)

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| FM-001 | —       | `fleetMetadataCache` disabled (default) leaves FMC's own rules unvalidated | Yes |
| FM-002 | SC-8    | rejects `fleetMetadataCache` enabled with no `mcpGatewayEndpoint`, independent of `fleet.enabled` | Yes |
| FM-003 | SC-8    | rejects `fleetMetadataCache` enabled with `mcpGatewayEndpoint` set but no `mcpGatewayType` | Yes |
| FM-004 | IA-5    | rejects `fleetMetadataCache` enabled with `fleet.oauth2.enabled` false | Yes |
| FM-005 | IA-5    | rejects `fleetMetadataCache` enabled with `oauth2.enabled` true but no `tokenURL` | Yes |
| FM-006 | IA-5    | rejects `fleetMetadataCache` enabled with no `credentialsSecretRef` (neither shared nor own override) | Yes |
| FM-007 | IA-5    | accepts FMC's own `fleetOAuth2CredentialsSecretRef` override with no shared `fleet.oauth2.credentialsSecretRef` | Yes |
| FM-008 | —       | accepts `fleet.enabled` with `backend: fleetmetadatacache` and no `endpoint` when the operator manages FMC | Yes |
| FM-009 | —       | rejects `fleet.enabled` with `backend: fleetmetadatacache` and no `endpoint` when FMC is BYO (not operator-managed) | Yes |

### 3.2 ConfigMap / Deployment / Service / RBAC / NetworkPolicy rendering (`internal/resources/fleetmetadatacache_test.go`)

| Area | Description | Automated? |
|------|--------------|------------|
| ConfigMap | renders `server`/`mcpGateway`/`valkey`/`sync`/`oauth2` sections matching upstream's `ServiceConfig` field names | Yes |
| ConfigMap | renders `mcpGateway.namespace` only when `mcpGatewayNamespace` is set | Yes |
| ConfigMap | applies `syncInterval`/`keyTTL` overrides (defaults `30s`/`45s`) | Yes |
| ConfigMap | reuses the shared `spec.valkey` address (no duplicate Valkey config surface) | Yes |
| Deployment | exposes `api` (8080) and `metrics` (8081) container ports | Yes |
| Deployment | mounts `config` and `fleet-oauth2` volumes at the paths FMC's binary expects | Yes |
| Deployment | mounts the shared `fleet.oauth2.credentialsSecretRef` when FMC has no override | Yes |
| Deployment | mounts FMC's own `fleetOAuth2CredentialsSecretRef` override when set, ignoring the shared fallback | Yes |
| Deployment | passes `-config=/etc/fleetmetadatacache/config.yaml` | Yes |
| Service | selects the `fleetmetadatacache` component; included in `Services()` only when enabled | Yes |
| RBAC | grants Envoy AI Gateway (`gateway.envoyproxy.io`/`aigateway.envoyproxy.io`) rules when `mcpGatewayType: eaigw` | Yes |
| RBAC | grants Kuadrant (`mcp.kuadrant.io`/`gateway.networking.k8s.io`) rules when `mcpGatewayType: kuadrant` | Yes |
| RBAC | `ClusterRole`/`ClusterRoleBinding` included in `ClusterRoles()`/`ClusterRoleBindings()` only when enabled | Yes |
| NetworkPolicy | allows ingress from Gateway/RemediationOrchestrator pods on the api port; adds a metrics rule when monitoring is enabled | Yes |
| NetworkPolicy | included in `NetworkPolicies()` only when enabled | Yes |
| `resolveFleetEndpoint` | auto-derives FMC's in-cluster URL only for `backend: fleetmetadatacache` + operator-managed FMC; leaves explicit endpoints and BYO/`acm` untouched | Yes |
| `AllComponents`/`isComponentActive` | `fleetmetadatacache` included in the master list, active only when enabled | Yes |
| `PodDisruptionBudgets`/`ServiceAccountName` | PDB and SA name resolve correctly for the new component | Yes |

### 3.3 Controller lifecycle (`internal/controller/fleetmetadatacache_test.go`, envtest)

| Description | Automated? |
|--------------|------------|
| enabling creates FMC Deployment, Service, ConfigMap, ClusterRole, and ClusterRoleBinding | Yes |
| disabled (default) CR produces no FMC Deployment/Service/ConfigMap/ClusterRole | Yes |
| FMC's ServiceAccount is still created when disabled (matches AuthWebhook/APIFrontend precedent — `deployServiceAccounts` loops `AllComponents()` unconditionally) | Yes |
| FMC is included in `status.services` with `Ready: true` once the CR reaches `Running` | Yes |
| disabling after being enabled deletes the Deployment, Service, ConfigMap, ClusterRole, and ClusterRoleBinding | Yes |
| enabling with `spec.fleet.mcpGatewayEndpoint` missing drives the CR to `PhaseError` | Yes |

### 3.4 Unused-FMC warning (`internal/controller/fleetmetadatacache_test.go`, envtest)

Added after initial review: `spec.fleetMetadataCache.enabled=true` combined
with `spec.fleet.backend: acm` (or `spec.fleet.enabled: false`) passes both
`validateFleetMetadataCache` and `validateFleetConfig` independently (each
only checks its own component's requirements), but the result is FMC fully
deployed and polling/caching while zero components query it -- Gateway and
RemediationOrchestrator resolve `spec.fleet.endpoint` to the *other*
backend. Not unsafe (unlike a missing `tokenSecretName`/`mcpGatewayEndpoint`,
which crash-loops pods or sends unauthenticated requests), and not
necessarily a mistake -- a deliberate shadow-deploy of FMC ahead of cutting
over from `backend: acm` is legitimate. So this is surfaced as a `Warning`
Event (`FleetMetadataCacheUnused`), the same non-blocking, non-mutating
mechanism already used for `NetworkPoliciesDisabled`, rather than rejected
at admission or silently stripped from the stored spec.

| Description | Automated? |
|--------------|------------|
| emits `FleetMetadataCacheUnused` when enabled with `backend: acm` | Yes |
| does not emit it when enabled with `backend: fleetmetadatacache` (the consuming configuration) | Yes |

### 3.5 Regression fix surfaced during this work

`setAllDeploymentsReady` (test helper shared by most `internal/controller`
specs) iterated `resources.AllComponents()` unconditionally and blocked on
`Eventually(...).Should(Succeed())` for every component's Deployment. This
worked before #200 because every existing member of `AllComponents()`
defaults to *active* (Gateway/APIFrontend default enabled); FMC is the
first component in that list to default to *disabled*. Fixed by switching
the helper (and one inlined duplicate in a Gateway-degraded-status test)
to `resources.ActiveComponents(kn)`. Covered implicitly: the full
pre-existing `internal/controller` suite (145 specs) passes unchanged with
FMC left at its default.

## 4. Acceptance Criteria

- All scenarios above pass via `make test-unit` (`internal/resources`
  coverage ~89%, matching pre-change baseline) and `make test-integration`
  (`internal/controller`, all specs green, ~73% coverage).
- `make lint` reports 0 issues.
- `make manifests generate build-installer bundle` regenerated
  `config/crd/bases/kubernaut.ai_kubernauts.yaml`, `dist/install.yaml`,
  `bundle/manifests/*` with the new `fleetMetadataCache` field; operator's
  own image reference in `dist/install.yaml` verified unchanged (no
  kustomize tag-reset drift this time).
- `config/manager/manager.yaml` pins
  `RELATED_IMAGE_FLEETMETADATACACHE` to
  `quay.io/kubernaut-ai/fleetmetadatacache@sha256:3bcac1b0...` (the
  `linux/amd64` manifest digest from the `1.6.0-rc1` multi-arch tag,
  matching the single-arch pinning convention already used for every other
  `RELATED_IMAGE_*`).
- `config/samples/v1alpha1_kubernaut.yaml` and
  `docs/installation/03-deploy.md` document the new
  `spec.fleetMetadataCache` block.
- No behavior change for CRs that leave `spec.fleetMetadataCache` unset
  (fully backward compatible; disabled by default).

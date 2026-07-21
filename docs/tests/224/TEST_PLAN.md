# IEEE 829 Test Plan — Issue #224: Wire spec.fleet into SignalProcessing/APIFrontend/EffectivenessMonitor

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-224                                             |
| **Issue**          | #224 — Wire spec.fleet into SignalProcessing/APIFrontend/EffectivenessMonitor (cluster classification + multi-cluster reads) |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-07-21                                         |
| **Scope**          | `api/v1alpha1/kubernaut_types.go`, `internal/resources/{configmaps,rbac,networkpolicies,deployments,validation,common}.go`, `internal/controller/kubernaut_controller.go` |

## 1. Objective

#180/#223 added `spec.fleet` (consumed by Gateway/RemediationOrchestrator for
federated scope-checking) and #200 added the operator-managed FMC service.
SignalProcessing (SP), APIFrontend (AF), and EffectivenessMonitor (EM) were
left unwired even though upstream's own binaries (`pkg/signalprocessing`,
`pkg/apifrontend`, `internal/config/effectivenessmonitor`) already support
fleet-aware operation: SP uses `spec.fleet.mcpGatewayEndpoint`/`mcpGatewayType`
to construct a `ClusterRegistry` for cluster classification labels; AF uses it
to back the `list_clusters` MCP tool and route remote reads; EM uses it to
read a remediation's target cluster via the MCP Gateway.

A preflight spike (see the `Fleet wiring SP/AF/EM (#224)` plan doc) verified
this directly against upstream source at the commit this branch depends on
and surfaced two corrections to the original design: AF/EM reuse the shared
`fleetConfigYAML` shape (no new type needed), and AF/EM's RBAC must stay
cluster-scoped because their `ClusterRegistry` construction has no namespace
knob upstream (tracked as a separate upstream/follow-up issue).

Verify that:

1. `spec.fleet.mcpGatewayNamespace` is added as a shared fallback namespace
   for MCP Gateway CRD watches, with per-component overrides on
   `spec.signalProcessing.mcpGatewayNamespace` and the pre-existing
   `spec.fleetMetadataCache.mcpGatewayNamespace` (FMC). AF/EM get no
   namespace field (Finding 4 — no upstream consumer for it yet).
2. `spec.signalProcessing.fleetOAuth2CredentialsSecretRef`,
   `spec.apiFrontend.fleetOAuth2CredentialsSecretRef`, and
   `spec.effectivenessMonitor.fleetOAuth2CredentialsSecretRef` let each
   component authenticate to the MCP Gateway as its own OAuth2 client,
   mirroring the GW/RO/FMC precedent from #223/#200.
3. `fleetOAuth2YAML` gains a `tlsCAFile` field (pre-existing upstream/operator
   gap), defaulted to each component's inter-service CA path, for GW, RO, SP,
   AF, and EM (FMC excluded — it has no `TLSCAFile` mount today, tracked as a
   follow-up).
4. SP's ConfigMap renders a `fleet:` block matching
   `pkg/signalprocessing/config.FleetConfig`'s exact shape — critically,
   `endpoint` (not `mcpGatewayEndpoint`) for the MCP Gateway URL, plus
   `mcpGatewayType`, `namespace`, and `oauth2` (with its own `tlsCAFile`).
5. AF's and EM's ConfigMaps render a `fleet:` block reusing the same
   `fleetConfigYAML` GW/RO already use, via a variant that omits
   `backend`/`endpoint`/`tokenPath` (upstream's `FleetConfig.Validate()` never
   requires them for AF/EM) while still setting `mcpGatewayEndpoint`,
   `mcpGatewayType`, and `oauth2`.
6. SP's, AF's, and EM's `ClusterRole`s gain the gatewayType-conditional MCP
   Gateway CRD watch rules (`gateway.envoyproxy.io`/`aigateway.envoyproxy.io`
   for `eaigw`, `mcp.kuadrant.io`/`gateway.networking.k8s.io` for `kuadrant`)
   when fleet is enabled and `mcpGatewayEndpoint` is set — extracted from
   FMC's existing rule-set into a shared `mcpGatewayCRDPolicyRules` helper.
7. When SP's effective `mcpGatewayNamespace` resolves non-empty, SP's grant
   moves from the cluster-scoped rule to a namespace-scoped `Role`/
   `RoleBinding` (mirroring FMC's retrofit, both handled by one
   `MCPGatewayNamespaceRBAC` helper). AF/EM never get this option (Finding 4).
8. FMC's own RBAC is retrofitted the same way: `fleetMetadataCacheClusterRole`
   omits its rule (and is excluded from `ClusterRoles()`/`ClusterRoleBindings()`)
   once FMC's effective namespace resolves, replaced by
   `MCPGatewayNamespaceRBAC`'s namespace-scoped grant.
9. SP/AF/EM's NetworkPolicies, and (retrofitted) GW/RO's NetworkPolicies, gain
   an egress rule permitting the MCP Gateway/OAuth2/ACM destinations when
   fleet is enabled, extracted from FMC's existing rule into a shared
   `fleetDestinationsEgressRule()` helper.
10. SP/AF/EM's Deployments mount the OAuth2 client Secret (fleet-oauth2 only —
    no fleet-ca/fleet-token, since they never call the Backend/Endpoint
    adapter) when `fleet.oauth2.enabled` is true, via an OAuth2-only variant
    extracted from `appendFleetSecretMounts`.
11. `validateFleetConfig`'s per-component `credentialsSecretRef` effective-value
    check generalizes from the GW/RO-only 2-way switch to a loop covering
    GW/RO/SP/AF/EM.
12. All pre-existing Fleet/FMC/GW/RO/SP/AF/EM tests remain green (additive;
    every new field defaults to empty/disabled, preserving current behavior
    for CRs that don't set them).

**Explicitly out of scope** (tracked separately):

- AF/EM namespace-scoped RBAC — blocked on upstream adding a `Namespace`
  field to their `ClusterRegistry` construction (new upstream issue, filed
  alongside this work) and a follow-up operator-side tracking issue linking
  it with kubernaut#1686.
- FMC's own `OAuth2.TLSCAFile` mount (pre-existing gap, noted in #200's test
  plan; not widened here).
- A unified `spec.fleet.enabled` toggle across all fleet-aware services
  (separate upstream issue, filed alongside this work).

## 2. Test Strategy

Standard TDD (RED -> GREEN -> REFACTOR), unit tier only
(`internal/resources`) — this is a rendering/validation/RBAC-shape change
with no new controller-owned lifecycle (SP/AF/EM already exist as
components; only their fleet-related config surface changes), so the
existing `internal/controller` integration suite is re-run for regression
coverage rather than extended with new fleet-specific scenarios, except for
the FMC/SP namespace-scoped RBAC retrofit's `ensureUnowned`/`deleteIfExists`
lifecycle wiring, which does need one integration scenario.

## 3. Test Scenarios

### 3.1 CRD fields and defaults (`internal/resources/common_test.go`, `api/v1alpha1` deepcopy)

| ID     | Description | Automated? |
|--------|--------------|------------|
| FW-001 | `spec.fleet.mcpGatewayNamespace` defaults to empty (cluster-wide) | Yes |
| FW-002 | `spec.signalProcessing.mcpGatewayNamespace` overrides the shared value when set | Yes |
| FW-003 | `spec.signalProcessing.fleetOAuth2CredentialsSecretRef` / `spec.apiFrontend...` / `spec.effectivenessMonitor...` each default to empty (falls back to shared `fleet.oauth2.credentialsSecretRef`) | Yes |
| FW-004 | Deepcopy round-trips all new fields without aliasing | Yes (`make generate` + build) |

### 3.2 `resolveFleetConfig`/TLSCAFile (`internal/resources/configmaps_test.go`)

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| FW-010 | SC-8    | `fleetOAuth2YAML.tlsCAFile` renders `InterServiceTLSCAFile` for GW/RO when `oauth2.enabled` | Yes |
| FW-011 | —       | `resolveFleetConfig` omits `tlsCAFile` when `oauth2.enabled` is false | Yes |

### 3.3 SignalProcessing fleet rendering (`internal/resources/configmaps_test.go`)

| ID     | Description | Automated? |
|--------|--------------|------------|
| FW-020 | SP's ConfigMap omits `fleet:` entirely when `spec.fleet.enabled` is false | Yes |
| FW-021 | SP's `fleet.endpoint` (not `mcpGatewayEndpoint`) is set to `spec.fleet.mcpGatewayEndpoint` | Yes |
| FW-022 | SP's `fleet.mcpGatewayType`/`fleet.namespace` render from the shared+override fields | Yes |
| FW-023 | SP's `fleet.oauth2.tlsCAFile` defaults to `InterServiceTLSCAFile` when `oauth2.enabled` | Yes |
| FW-024 | SP's `fleet.oauth2.credentialsSecretRef` uses `spec.signalProcessing.fleetOAuth2CredentialsSecretRef` when set, else the shared value | Yes |

### 3.4 AF/EM fleet rendering (`internal/resources/configmaps_test.go`)

| ID     | Description | Automated? |
|--------|--------------|------------|
| FW-030 | AF's/EM's ConfigMap omits `fleet:` when `spec.fleet.enabled` is false | Yes |
| FW-031 | AF's/EM's rendered `fleet:` block omits `backend`/`endpoint`/`tokenPath` even when `spec.fleet.backend`/`endpoint` are set (only GW/RO render those) | Yes |
| FW-032 | AF's/EM's `fleet.mcpGatewayEndpoint`/`mcpGatewayType`/`oauth2` render identically to GW/RO's | Yes |
| FW-033 | AF's/EM's own `fleetOAuth2CredentialsSecretRef` override applies independently of GW/RO/SP's | Yes |

### 3.5 RBAC (`internal/resources/rbac_test.go`)

| ID     | Description | Automated? |
|--------|--------------|------------|
| FW-040 | `mcpGatewayCRDPolicyRules("eaigw")`/`("kuadrant")` returns the expected rule sets (extracted helper, byte-for-byte match with FMC's pre-existing behavior) | Yes |
| FW-041 | SP's `ClusterRole` gains the MCP Gateway CRD rules when fleet enabled + `mcpGatewayEndpoint` set + SP's effective namespace is empty | Yes |
| FW-042 | SP's `ClusterRole` omits those rules (moved to namespace-scoped `Role`) when SP's effective namespace resolves non-empty | Yes |
| FW-043 | AF's/EM's `ClusterRole`s gain the MCP Gateway CRD rules under the same enablement condition, unconditionally cluster-scoped (no namespace variant) | Yes |
| FW-044 | `MCPGatewayNamespaceRBAC` returns a `Role`/`RoleBinding` pair for FMC when FMC's effective namespace resolves | Yes |
| FW-045 | `MCPGatewayNamespaceRBAC` returns a `Role`/`RoleBinding` pair for SP when SP's effective namespace resolves | Yes |
| FW-046 | `MCPGatewayNamespaceRBAC` returns nothing for either when their effective namespace is empty | Yes |
| FW-047 | FMC's `ClusterRole`/`ClusterRoleBinding` are excluded from `ClusterRoles()`/`ClusterRoleBindings()` once FMC's effective namespace resolves (superseded by the namespace-scoped grant) | Yes |
| FW-048 (regression) | The three pre-existing FMC RBAC assertions (rule contents, presence when enabled, name-drift guard) stay green unchanged — `testKubernautWithFMC()` never sets a namespace | Yes |

### 3.6 NetworkPolicy (`internal/resources/networkpolicies_test.go`)

| ID     | Description | Automated? |
|--------|--------------|------------|
| FW-050 | `fleetDestinationsEgressRule()` matches FMC's pre-existing egress rule byte-for-byte (extracted helper, no behavior change for FMC) | Yes |
| FW-051 | SP's/AF's/EM's NetworkPolicy gains the fleet egress rule when fleet enabled + `mcpGatewayEndpoint` set | Yes |
| FW-052 | GW's/RO's NetworkPolicy gains the same egress rule under the same condition (retrofit; previously missing entirely) | Yes |
| FW-053 | The egress rule is absent for all five when fleet is disabled | Yes |

### 3.7 Deployment mounts (`internal/resources/deployments_test.go`)

| ID     | Description | Automated? |
|--------|--------------|------------|
| FW-060 | SP's/AF's/EM's Deployment mounts `fleet-oauth2` at their own `/etc/<component>/<credentialsSecretRef>` path when `oauth2.enabled` | Yes |
| FW-061 | SP's/AF's/EM's Deployment does NOT mount `fleet-ca`/`fleet-token` (they never consume the Backend/Endpoint adapter) even when `spec.fleet.caSecretName`/`tokenSecretName` are set | Yes |
| FW-062 | GW's/RO's existing `fleet-ca`/`fleet-token`/`fleet-oauth2` mounting behavior is unchanged (regression) | Yes |

### 3.8 Validation (`internal/resources/validation_test.go`)

| ID     | FedRAMP | Description | Automated? |
|--------|---------|--------------|------------|
| FW-070 | IA-5    | `validateFleetConfig`'s effective-`credentialsSecretRef` check covers SP/AF/EM the same way it already covers GW/RO (each needs its own override or the shared fallback) | Yes |
| FW-071 | IA-5    | Error message names the specific component(s) missing an effective value, generalized from the GW/RO-only switch | Yes |

### 3.9 Controller integration (`internal/controller/kubernaut_lifecycle_test.go`)

| Description | Automated? |
|--------------|------------|
| enabling fleet with SP's `mcpGatewayNamespace` set creates a namespace-scoped Role/RoleBinding via `ensureUnowned`, cleaned up on disable | Yes |
| full pre-existing `internal/controller` suite remains green (regression) | Yes |

## 4. Acceptance Criteria

- All scenarios above pass via `make test-unit` and `make test-integration`.
- `make lint` reports 0 issues.
- `make manifests generate build-installer bundle` regenerated
  `config/crd/bases/kubernaut.ai_kubernauts.yaml`, `dist/install.yaml`,
  `bundle/manifests/*` with the new fields.
- `config/samples/v1alpha1_kubernaut.yaml` and `docs/installation/03-deploy.md`
  document SP/AF/EM fleet examples.
- `kubernaut-docs#208` updated with an SP/AF/EM addendum.
- No behavior change for CRs that leave the new fields unset (fully backward
  compatible).

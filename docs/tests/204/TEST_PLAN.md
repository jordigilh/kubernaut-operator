# IEEE 829 Test Plan — Issue #204: Propagate mcpGatewayType to KA config for GatewayDiscoverer tool discovery

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-204                                             |
| **Issue**          | #204 — Propagate mcpGatewayType to KA config for GatewayDiscoverer tool discovery |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-07-23                                         |
| **Scope**          | `api/v1alpha1/kubernaut_types.go`, `internal/resources/{configmaps,deployments,networkpolicies,validation}.go`, `internal/controller/kubernaut_controller_test.go` |

## 1. Objective

ADR-068 decision #11 introduces a `GatewayDiscoverer` interface in KA
(Kubernaut Agent) with two LLM-facing tools -- `list_clusters` and
`list_tools_for_cluster(cluster_id)` -- so the LLM can pre-scope tool
discovery at fleet scale instead of loading every managed cluster's tools
into context. #201 added `spec.fleet.mcpGatewayType` to the CRD and wired
it into FMC; this issue closes the parallel gap for KA.

A preflight check against the vendored `kubernaut` module (read directly,
not inferred from the issue text) found KA's actual config shape and
several details the issue itself does not mention:

1. KA's real config field is `integrations.fleet.{endpoint,gatewayType}`
   (`internal/kubernautagent/config/config_types.go`, upstream-internal so
   not importable) -- a **different shape** from the shared
   `fleetConfigYAML`/`fleetOAuth2YAML` the operator already renders for
   GW/RO/SP/AF/EM (no `backend`/`tlsCAFile`/`tokenPath`/`mcpGateway`-prefixed
   names, no `oauth2.tlsCAFile`). Those existing helpers are not reusable
   for KA's YAML rendering.
2. `endpoint`/`gatewayType` must be set together or KA's own
   `Config.Validate()` (`validateFleetIntegration`) fails closed at
   startup. The operator's existing `validateFleetConfig` already requires
   `spec.fleet.mcpGatewayEndpoint`/`mcpGatewayType` together whenever
   `spec.fleet.enabled=true`, so rendering both from those same fields
   keeps KA automatically consistent -- no new CRD field needed for this
   part.
3. Upstream KA's `FleetConfig` also has an `OAuth2` sub-block
   (`Enabled`/`TokenURL`/`CredentialsSecretRef`/`Scopes`) consumed by
   `registerFleetTools()` to authenticate `list_clusters`/
   `list_tools_for_cluster` calls to the MCP Gateway. The issue's
   acceptance criteria mention only `gatewayType`, but every other
   fleet-aware component (GW/RO/SP/AF/EM/FMC) already has this wired; per
   product decision this PR includes it too rather than leaving KA unable
   to authenticate to an OAuth2-protected gateway.
4. **Quirk**: `registerFleetTools()`'s OAuth2 credentials path is a
   **hardcoded literal** `"/etc/kubernautagent/" + credentialsSecretRef`,
   not derived from KA's `-config` flag directory. The operator's own KA
   Deployment mounts everything else at `/etc/kubernaut-agent`
   (hyphenated) via an explicit `-config` arg override. The new
   fleet-oauth2 secret mount must go at the **unhyphenated**
   `/etc/kubernautagent/<credentialsSecretRef>` -- a different directory
   than every other KA mount.
5. KA makes no Kubernetes API/CRD calls for this feature (pure MCP/HTTP
   client via `fleetclient.NewResilient`) -- no RBAC changes needed.
6. KA's `NetworkPolicy` has no egress rule allowing it to reach the MCP
   Gateway endpoint today.

**Explicitly out of scope**: upstream's `FleetConfig.AlignmentCheck`
per-fleet override (issue doesn't ask for it; leaving it unset preserves
current alignment-check behavior exactly). Fixing upstream's hardcoded,
non-configurable fleet-oauth2 path (candidate for a small follow-up
upstream issue, non-blocking here).

## 2. Test Strategy

Standard TDD (RED -> GREEN -> REFACTOR), spanning the unit tier
(`internal/resources`: ConfigMap rendering, Deployment mount,
NetworkPolicy egress, validation) plus one integration scenario to satisfy
the Pyramid Invariant -- proving the reconcile loop actually renders KA's
fleet block end-to-end through a real envtest API server and controller,
not just via Go-level struct construction in unit tests.

## 3. Test Scenarios

### 3.1 CRD field (`api/v1alpha1` deepcopy)

| ID      | Description | Automated? |
|---------|--------------|------------|
| KFG-001 | `KubernautAgentSpec.FleetOAuth2CredentialsSecretRef` defaults to empty string (falls back to `spec.fleet.oauth2.credentialsSecretRef`) | Yes |

### 3.2 KA ConfigMap rendering (`internal/resources/configmaps_test.go`, `KubernautAgent ConfigMap`)

Business objective: KA's fleet tool discovery must be entirely inert
until an administrator explicitly enables `spec.fleet` -- no default-on
network calls or credentials exposure for a capability most single-cluster
deployments never use (CM-6: automated mechanism centrally applies
configuration settings; secure-by-default posture for an optional
capability).

| ID      | FedRAMP | Description | Automated? |
|---------|---------|--------------|------------|
| KFG-010 | CM-6    | `integrations.fleet` is omitted entirely when `spec.fleet.enabled` is false/unset -- matches upstream's "empty gatewayType = fleet disabled" contract and the issue's "default behavior unchanged" acceptance criterion | Yes |
| KFG-011 | CM-6    | `integrations.fleet.endpoint`/`gatewayType` render verbatim from `spec.fleet.mcpGatewayEndpoint`/`mcpGatewayType` when fleet is enabled, for both `kuadrant` and `eaigw` | Yes |
| KFG-012 | CM-6    | `integrations.fleet.oauth2` is omitted when `spec.fleet.oauth2.enabled` is false, even though fleet itself is enabled (KA does not send credentials it wasn't configured with) | Yes |
| KFG-013 | CM-6    | `integrations.fleet.oauth2.credentialsSecretRef` uses KA's own `spec.kubernautAgent.fleetOAuth2CredentialsSecretRef` override when set, falling back to `spec.fleet.oauth2.credentialsSecretRef` otherwise -- proves KA can authenticate as a distinct OAuth2 client from other fleet-aware components against a federated IdP | Yes |
| KFG-014 | CM-6    | `integrations.fleet.oauth2.tokenURL`/`scopes` render verbatim from `spec.fleet.oauth2` | Yes |

### 3.3 Deployment mount (`internal/resources/deployments_test.go`, `KubernautAgentDeployment`)

Business objective: the fleet OAuth2 client credentials must land exactly
where KA's hardcoded upstream lookup expects them, or KA silently runs
fleet tool discovery unauthenticated instead of failing closed (IA-5:
authenticator management).

| ID      | FedRAMP | Description | Automated? |
|---------|---------|--------------|------------|
| KFG-020 | IA-5    | No fleet-oauth2 volume/mount is added when fleet OAuth2 is disabled | Yes |
| KFG-021 | IA-5    | The fleet-oauth2 Secret is mounted at `/etc/kubernautagent/<effective credentialsSecretRef>` (the unhyphenated path KA's `registerFleetTools()` hardcodes) when fleet OAuth2 is enabled | Yes |

### 3.4 NetworkPolicy egress (`internal/resources/networkpolicies_test.go`)

Business objective: KA must be able to reach the MCP Gateway endpoint it
was configured to call -- an omitted egress rule would make the feature
silently non-functional (fail-closed by network policy) rather than
producing an actionable error (AC-4: information flow enforcement --
egress restricted to only what's needed, and only when needed).

| ID      | FedRAMP | Description | Automated? |
|---------|---------|--------------|------------|
| KFG-030 | AC-4    | KA's NetworkPolicy has no fleet egress rule when `spec.fleet.enabled` is false | Yes |
| KFG-031 | AC-4    | KA's NetworkPolicy gains the shared `fleetDestinationsEgressRule()` when `spec.fleet.enabled` is true | Yes |

### 3.5 Validation (`internal/resources/validation_test.go`, "Fleet Config Validation")

Business objective: catch a misconfiguration at admission time that would
otherwise reconcile cleanly but leave KA unable to authenticate to the MCP
Gateway, only surfacing as a production incident (SI-10: automated
input-validation checks with actionable, specific feedback).

| ID      | FedRAMP | Description | Automated? |
|---------|---------|--------------|------------|
| KFG-040 | SI-10   | `validateFleetConfig`'s effective-credentialsSecretRef check now includes KA as a 6th component: rejects `fleet.oauth2.enabled=true` when neither `spec.kubernautAgent.fleetOAuth2CredentialsSecretRef` nor `spec.fleet.oauth2.credentialsSecretRef` is set, naming KA's spec path in the error alongside the other five | Yes |
| KFG-041 | SI-10   | Accepts `fleet.oauth2.enabled=true` when KA relies on the shared `spec.fleet.oauth2.credentialsSecretRef` fallback (no regression for the common case where no component overrides it) | Yes |

### 3.6 No behavior change (regression)

| ID      | Description | Automated? |
|---------|--------------|------------|
| KFG-050 | All pre-existing KA ConfigMap, Deployment, and NetworkPolicy fixtures (fleet unset) render byte-identical output to the pre-change baseline | Yes |

### 3.7 Controller integration (`internal/controller/kubernaut_controller_test.go`)

Business objective: prove the reconcile loop actually threads
`spec.fleet` through to KA's rendered ConfigMap end-to-end via a real
envtest API server and controller run, not just via Go-level struct
construction in unit tests (Pyramid Invariant: IT proves wiring).

| ID      | FedRAMP | Description | Automated? |
|---------|---------|--------------|------------|
| KFG-060 | CM-6    | A Kubernaut CR with `spec.fleet.enabled=true`, `mcpGatewayType=kuadrant` reconciles successfully and KA's rendered ConfigMap contains `integrations.fleet.gatewayType: kuadrant` | Yes |

## 4. Acceptance Criteria

- All scenarios above pass via `make test-unit` and `make test-integration`.
- `make lint` reports 0 issues.
- `make generate manifests build-installer bundle` regenerated
  `zz_generated.deepcopy.go`, `config/crd/bases/kubernaut.ai_kubernauts.yaml`,
  `dist/install.yaml`, `bundle/manifests/*` with the new field.
- `config/samples/v1alpha1_kubernaut.yaml` and the install guide document
  KA's new fleet fields.
- No behavior change for CRs that leave `spec.fleet` unset (fully backward
  compatible) -- KA starts exactly as before, no discovery tools
  registered.

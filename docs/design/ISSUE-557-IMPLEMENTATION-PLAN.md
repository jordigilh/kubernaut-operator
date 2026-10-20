# Issue #557 — Fleet MCP Gateway TLS trust configuration

| Field | Value |
|---|---|
| Issue | https://github.com/jordigilh/kubernaut-operator/issues/557 |
| Operator branch | `feat/557-mcp-gateway-tls` |
| Upstream contract | kubernaut#2540 / PR #2541 |
| Status | Approved for RED; implementation in progress |

## Decision

Add `spec.fleet.mcpGateway.tls` using the existing `FleetTrustSpec` source
contract. The operator resolves it independently from `scopeCheck.acm.tls` and
`oauth2.tls`:

| Source | Effective behavior |
|---|---|
| omitted / `interService` | Existing resolved inter-service CA path |
| `system` | Empty explicit CA path; runtime uses system roots |
| `file` | Absolute administrator-owned path; no operator volume or byte validation |
| `secret` | Namespace-local keyed CA Secret, PEM-validated and mounted read-only |

The upstream MCP client now accepts a dedicated gateway CA path. The operator
must not map this lane to process-wide `TLS_CA_FILE`/`SSL_CERT_FILE`, because
that would conflate MCP Gateway, internal-service, scope-check, and OAuth2
trust domains.

## Wiring manifest

| Component | Production entry point | Evidence |
|---|---|---|
| CRD/API validation | `ValidateFleet` and `phaseValidate` | `UT-FLEET-557-API-*` |
| ConfigMap resolution | Gateway/RO/SP/AF/EM/WE/KA/FMC builders | `UT-FLEET-557-RENDER-*` |
| Secret mounts | shared Fleet deployment helpers and FMC/WE builders | `UT-FLEET-557-MOUNT-*` |
| Secret preflight | `validateFleetTrustSecrets` | `UT-FLEET-557-SECRET-*` |
| Secret events/rollout | `fleetTrustSecretToKubernaut`, metadata-only revision | `UT-FLEET-557-ROTATION-*`, envtest |
| Runtime contract | upstream `mcpclient.Connect` | upstream PR #2541 tests |
| Real provider qualification | upstream #2492 | exact-SHA EAIGW/Kuadrant smoke evidence |

## Failure and ownership contract

- Missing, unreadable, missing-key, or malformed Secret material prevents
  dependent workloads from advancing and produces a field-qualified status
  condition.
- Valid Secret changes trigger revalidation and a Deployment rollout using
  only Secret identity/resourceVersion metadata; CA bytes never enter status,
  ConfigMaps, annotations, or logs.
- Administrator-owned Secrets and external file paths are never adopted,
  mutated, rotated, or deleted by the operator.
- The upstream MCP client keeps OAuth2 token-endpoint trust separate from MCP
  Gateway trust and preserves Fleet reconnect/readiness behavior.

## Verification boundary

Operator UT/envtest proves API, rendering, mounts, ownership, readiness, and
reconciliation wiring. Upstream PR #2541 proves the scoped MCP transport. The
full EAIGW/Kuadrant Fleet smoke lane remains upstream-owned under kubernaut#2492
and must use the exact operator SHA/image provenance; it is not replaced by a
mock or certificate verification disablement.

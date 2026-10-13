# DD-521 — Fleet v1alpha2 API contract

| Field | Value |
|---|---|
| Issue | [#521](https://github.com/jordigilh/kubernaut-operator/issues/521) |
| Related coverage | [#431](https://github.com/jordigilh/kubernaut-operator/issues/431) |
| Status | Approved; implementation record |
| API line | `kubernaut.ai/v1alpha2` (unreleased clean break) |

## Decision

The v1alpha2 Fleet API is capability-oriented. `spec.fleet.enabled` is the
sole activation flag. Shared MCP Gateway transport, OAuth2 credentials, and
MCP-client tuning remain under `spec.fleet`; scope checking is
provider-discriminated so the operator can distinguish BYO ACM from its own
FMC service.

No migration aliases are provided. v1alpha2 is unreleased and follows the
approved clean-break policy.

## Final field matrix

| API field | Disposition | Activation/defaulting | Provider applicability | Runtime/rendered behavior | Secret/RBAC boundary |
|---|---|---|---|---|---|
| `fleet.enabled` | retain | sole Fleet gate; omitted/false makes nested Fleet config inert | all | enables Fleet-aware components and selected FMC | no direct Secret access |
| `fleet.mcpGateway.type` | retain | required when Fleet is active | all active Fleet | selects `eaigw` or `kuadrant` runtime adapter | existing provider-scoped MCP Gateway RBAC unchanged |
| `fleet.mcpGateway.endpoint` | retain/constrain | required when Fleet is active and must be absolute HTTPS | all active Fleet | shared remote-cluster MCP endpoint | no new Secret access |
| `fleet.mcpGateway.namespace` | retain | required when Fleet is active | all active Fleet | shared namespace for provider watches | existing namespace-scoped Role/RoleBinding behavior unchanged |
| `fleet.scopeCheck.backend` | constrain | required when Fleet is active; enum `acm`, `fleetmetadatacache` | provider selector | selects one scope-check resolver | invalid/ambiguous combinations fail closed |
| `fleet.scopeCheck.acm.endpoint` | move/constrain | required for active ACM; absolute HTTPS | ACM only | rendered as upstream Fleet scope-check endpoint | no endpoint-derived FMC permissions |
| `fleet.scopeCheck.acm.tls` | move/constrain | optional; defaults to inter-service trust | ACM only | resolves ACM CA path and optional Secret mount | only ACM consumers mount referenced CA Secret read-only/keyed |
| `fleet.scopeCheck.acm.tokenSecretRef` | move/constrain | required for active ACM; default key `token` | ACM only | token path rendered for scope-check clients | only ACM consumers mount referenced token Secret read-only/keyed |
| `fleet.scopeCheck` FMC provider fields | remove | not user-configurable | FMC only | endpoint derived from Kubernaut namespace as HTTPS Service URL; mandatory inter-service trust | no user endpoint, token, or provider-specific scope-check TLS Secret |
| `fleet.oauth2.tokenURL/scopes/credentialsSecretRef/tls` | retain/shared/constrain | tokenURL is required and must be absolute HTTPS; no Fleet `enabled` | all Fleet-aware components | shared OAuth2 runtime config; component overrides remain effective | existing OAuth2 credential/CA mounts and component boundaries |
| `fleet.mcpClient.retry.initialInterval` | rename from resilience | optional; non-empty values are positive Go durations | all Fleet-aware components | maps to upstream `initialInterval` | no Secret/RBAC impact |
| `fleet.mcpClient.retry.maxInterval` | rename from resilience | optional; positive Go duration | all Fleet-aware components | maps to upstream `maxInterval` | no Secret/RBAC impact |
| `fleet.mcpClient.retry.maxElapsedTime` | rename from resilience | optional; positive Go duration | all Fleet-aware components | maps to upstream `maxElapsedTime` | no Secret/RBAC impact |
| `fleet.mcpClient.timeouts.tokenRefresh` | rename from resilience | optional; positive Go duration | all Fleet-aware components | maps to upstream `tokenRefreshTimeout` | no Secret/RBAC impact |
| `fleet.mcpClient.timeouts.connect` | rename from resilience | optional; positive Go duration | all Fleet-aware components | maps to upstream `connectTimeout` | no Secret/RBAC impact |
| `fleet.mcpClient.timeouts.discoverProbe` | rename from resilience | optional; positive Go duration | all Fleet-aware components | maps to upstream `discoverProbeTimeout` | no Secret/RBAC impact |
| `fleet.resilience` | remove | no alias | none | no canonical input path | none |

## Validation and resolution rules

* Disabled or omitted Fleet leaves nested values inert for pre-staging.
* Active ACM requires `scopeCheck.acm`, an absolute HTTPS endpoint, and a
  non-empty token Secret name. ACM trust defaults to inter-service trust and
  may select the existing source-aware trust override.
* Active FMC is selected only by `backend: fleetmetadatacache`. The operator
  derives `https://fleetmetadatacache-service.<kubernaut namespace>.svc.cluster.local:8080`.
  FMC has no user endpoint, token, or provider-specific scope-check TLS block
  and always uses inter-service trust.
* Unknown backends and provider-mismatched blocks are rejected.
* Active MCP Gateway and OAuth2 token endpoints must be absolute HTTPS URLs
  without embedded user information.
* Active ACM token Secrets are checked before deployment for existence and the
  selected key (default `token`), with an actionable status condition.
* Every non-empty MCP-client tuning value is parsed as a positive Go duration
  during admission/runtime validation; zero and negative durations are not
  accepted.

## Evidence and wiring matrix

| Behavior | Unit evidence | Integration evidence | Production path |
|---|---|---|---|
| ACM endpoint/trust/token rendering | `UT-FLEET-521-ACM-001`, `UT-FLEET-521-SECRET-*` | `IT-FLEET-521-RECONCILE-ACM` | `internal/resources/configmaps.go`, `deployments.go` |
| FMC derived endpoint/inter-service trust | `UT-FLEET-521-FMC-*` | `IT-FLEET-521-RECONCILE-FMC` | `internal/resources/common.go`, Fleet config builders |
| MCP-client retry/timeouts | `UT-FLEET-521-MCPCLIENT-*` | shared reconciliation assertions | `internal/resources/configmaps.go` |
| Provider, endpoint, duration, and Secret validation | `UT-FLEET-521-VALIDATE-*`, `UT-FLEET-521-SECRET-001` | `IT-FLEET-521-ADMISSION-001` | `internal/resources/validation.go`, controller admission and preflight |

The repository's statement-coverage gate is a best-effort business-logic
measure derived from these scenarios; it does not claim exhaustive branch
coverage. The repository evidence maps to applicable AC-6, IA-5, SC-8, SC-12, SC-13,
CM-6, and SI-10 objectives and SOC 2/ASVS objectives. It is not a formal
FedRAMP, SOC 2, or ASVS certification claim. Full Fleet E2E qualification is
upstream-owned under kubernaut#2492/#2488 and is not added here.

## Classification of references

The active API, implementation, generated manifests, samples, installation
documentation, #431 control matrix, traceability validator, and pyramid
validator are migrated. `docs/tests/180`, `docs/tests/224`, `docs/tests/200`,
`docs/tests/222`, `docs/tests/204`, `docs/tests/413`, and the original #421
audit are historical snapshots and are retained as historical evidence unless
they are explicitly promoted to an active contract. The superseding #499 plan
is retained as design history; this document records the approved follow-on
contract for #521.

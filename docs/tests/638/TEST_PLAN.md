# IEEE 829 Test Plan — Issue #638: Route authenticated Console policy and inventory reads to API Frontend

## 1. Scope

Issue #638 adds the authenticated same-origin route for the API Frontend (AF)
Console read endpoints consumed by kubernaut-console#164 (upstream
kubernaut#2604):

- `GET /api/v1/console/policies/scope`
- `GET /api/v1/console/inventory/managed-resources`
- `GET /api/v1/console/inventory/managed-resources/{resourceID}`

The core routing (`location ^~ /api/v1/console/` proxying to the
namespace-scoped AF HTTPS upstream with path, query, and opaque-identifier
preservation, trusted `Authorization` forwarding, rate limiting, and inherited
TLS verification) landed in PR #660. This change completes the remaining
acceptance criteria of #638 on top of that baseline:

1. **Read-only boundary**: `limit_except GET { deny all; }` in the console API
   location, so non-GET/HEAD methods are rejected with 403 at nginx rather
   than proxied to AF.
2. **Token hygiene**: `proxy_set_header X-Forwarded-Access-Token "";` so the
   raw OAuth2 Proxy session token is not forwarded to AF in a second header
   (AF authenticates solely via `Authorization: Bearer`). This closes the
   trusted-header-boundary item CodeRabbit's pre-merge check left unresolved
   on #660.
3. **Drift rollout**: the `console-nginx` ConfigMap joins the
   `console-oauth2-proxy` subPath roll mechanism in `deployConfigMaps` — a
   drift-repaired nginx ConfigMap now changes the Console pod-template hash
   via the live resourceVersion, so the running nginx reloads the repaired
   configuration. Previously #660's reconciliation restored the ConfigMap
   content but left the running pod on the stale configuration.
4. **Runtime access-control qualification**: the opt-in Kind qualification
   case gains the POST-denied (403) assertion, complementing #660's forged
   identity, opaque-ID, and unauthenticated-denial assertions.

Authorization stays with AF (named capability SARs and user-scoped target
checks; remote lists intentionally return `unavailable`). A2A/MCP behavior,
the SPA fallback, security headers, rate limits, and upstream TLS certificate
verification are unchanged.

## 2. Control objectives

| Objective | Business behavior | Evidence |
|---|---|---|
| FedRAMP/NIST AC-3 (access enforcement) | The Console read API boundary is read-only: non-GET methods are denied at the nginx boundary. | `UT-CN-638-006`; `IT-CN-638-001`; `E2E-CN-638-HTTP-001` (POST → 403) |
| FedRAMP/NIST AC-6 (least privilege) | The raw session token is not forwarded to AF in a second header; identity forwarding uses only the oauth2-proxy session token. | `UT-CN-638-007`; `IT-CN-638-001`; `E2E-CN-638-HTTP-001` |
| SOC 2 CC6.1 | The proxy boundary enforces the trusted identity transport and denies unauthenticated access before AF. | `UT-CN-638-002` (#660); `E2E-CN-638-HTTP-001` |
| SOC 2 CC6.3 | Configured access restrictions (read-only methods, rate limit, TLS-verified upstream) are reviewed and evidenced. | `UT-CN-638-003`–`007`; `E2E-CN-638-HTTP-001` |
| OWASP ASVS v5.0.0 V4.1.1, V4.2.1 (access control) | Route-level access decisions happen at the trusted proxy/AF layer, not the browser; requests without a valid session never reach AF. | `E2E-CN-638-HTTP-001` |
| OWASP ASVS v5.0.0 V14.4.4 / V9.2.2 (transport, credential disclosure) | Upstream connections stay HTTPS with certificate verification; responses and logs do not disclose tokens. | `UT-CN-638-005` (#660); `E2E-CN-638-HTTP-001` |
| FedRAMP/NIST CM-6 / SOC 2 CC6.8 (configuration) | A repaired/drifted nginx ConfigMap is applied to a running deployment (subPath rollout). | `IT-CN-638-001` (drift + roll); `UT-AUDIT-515-API-020` (updated) |

These mappings are engineering objectives, not formal FedRAMP authorization,
SOC 2 attestation, or OWASP ASVS conformance claims.

## 3. Test pyramid

### Unit (`internal/resources/console_test.go`)

From #660 (unchanged): `UT-CN-638-001`–`005` — routing with path/query
preservation, trusted-token Authorization forwarding, rate limit, no
unrelated `/api/` routing, TLS verification inheritance.

Added by this change:

- `UT-CN-638-006`: the console API location restricts methods with
  `limit_except GET { deny all; }`. `limit_except GET` also permits HEAD;
  this is intentional parity with AF's Go `ServeMux` `"GET"` pattern
  registration (Go 1.22 net/http matches HEAD for GET patterns).
- `UT-CN-638-007`: the console API location clears
  `X-Forwarded-Access-Token` toward AF.
- `UT-AUDIT-515-API-020` (updated): revision read failures for both Console
  ConfigMaps surface as reconciliation errors.
- Uncached-reader unit test (updated): both `console-nginx` and
  `console-oauth2-proxy` revisions fold into the Console pod-template hash.

### Integration (`internal/controller/kubernaut_lifecycle_test.go`, envtest)

- `IT-CN-638-001` (#660, extended): create path renders the `^~` location
  with trusted identity forwarding, the cleared raw-token header, and the
  GET-only restriction; drift (tampered location prefix) is repaired on the
  next reconcile **and** changes the Console pod-template hash, rolling the
  subPath-mounted pod. (#660 asserted the hash stayed unchanged — the stale
  running configuration this change fixes.)

### E2E / qualification (`test/e2e/kind/qualification_test.go`, opt-in lane)

- `E2E-CN-638-HTTP-001` (#660, extended): authenticated reads (policies/scope,
  the managed-resource list with query parameters, and an opaque asset-like
  detail ID) reach AF and are shape-validated — HTTP 200 with an
  `application/json` body or AF's own RFC 7807 `application/problem+json`
  capability denial; 401, redirects, other statuses, and non-AF bodies fail
  the case. Forged identity and bearer headers are not reflected;
  unauthenticated requests are denied; **POST is rejected with 403 by the
  read-only boundary**; no `access_token` appears in bodies or oauth2-proxy
  logs.
- The existing `E2E-CN-609-HTTP-002` continues to prove A2A/MCP routing is
  unaffected.

## 4. Design notes

- **Method restriction scope.** `limit_except GET { deny all; }` sits inside
  the `^~ /api/v1/console/` location. nginx permits GET and HEAD under
  `limit_except GET`; HEAD parity matches AF's router. All other methods are
  answered 403 by nginx and never reach AF.
- **Token clearing.** oauth2-proxy (the only ingress) overwrites any
  client-supplied `X-Forwarded-Access-Token` with the verified claim;
  nginx rewrites client `Authorization` from it and now also clears the raw
  header toward AF. The pre-existing `/a2a/` and `= /mcp` locations do not
  clear it; that divergence is tracked in #668 and intentionally not bundled
  here.
- **Drift rollout.** `deployConfigMaps` folds the live resourceVersion of both
  Console ConfigMaps into `cmHashes["console"]` on every reconcile (not only
  when this pass changed them): a revision-read failure or reconcile abort
  after a successful repair cannot lose the pending roll, because the next
  reconcile re-reads the same resourceVersion. The hash stays stable in
  steady state (the resourceVersion only changes when the ConfigMap is
  written) and changes whenever the subPath-mounted configuration changes,
  rolling the Console pod.
- **AF version mismatch.** If the deployed AF predates the #2604 routes,
  nginx proxies to AF's own 404; AF's sanitized `state: "unavailable"`
  envelopes pass through untouched (no `proxy_intercept_errors`).

## 5. Version prerequisites and qualification gaps

- Operator: #660 (routing) plus this change (read-only boundary, token
  hygiene, drift rollout). Operators at #660 without this change repair a
  drifted nginx ConfigMap but do not roll the running pod.
- API Frontend: must include kubernaut#2604 (console policy read routes,
  commit `e667cfc` or later).
- Console: kubernaut-console#164 consumes the endpoints; the SPA degrades
  gracefully when AF returns 404/unavailable envelopes.
- The authenticated deployed browser/proxy-to-AF qualification
  (`E2E-CN-638-HTTP-001`) runs only in the opt-in manual Kind qualification
  lane (`test/e2e/kind`); it was not executed against a live cluster in this
  change's development environment. Remaining gaps for that lane: deployed
  missing/expired-token and capability-denial cases depend on the Keycloak
  fixture's user capabilities; AF denial (403 problem detail) is asserted
  only as "not SPA HTML" so the lane does not depend on a specific RBAC
  fixture.

## 6. Exit criteria

- `go build ./...`, `golangci-lint run` (0 issues), `make test` green.
- `make manifests generate` produces no diff (no CRD change).
- Unit coverage of `internal/resources/` stays ≥ 96%; controller coverage
  stays ≥ 78%.
- All `#638` UT/IT specs pass; A2A/MCP and SPA regression specs pass.

# IEEE 829 Test Plan — Issue #638: Route authenticated Console policy and inventory reads to API Frontend

## 1. Scope

Issue #638 adds the authenticated same-origin route for the API Frontend (AF)
Console read endpoints consumed by kubernaut-console#164 (upstream
kubernaut#2604):

- `GET /api/v1/console/policies/scope`
- `GET /api/v1/console/inventory/managed-resources`
- `GET /api/v1/console/inventory/managed-resources/{resourceID}`

The operator-generated Console nginx configuration
(`internal/resources/console.go`, `consoleNginxServerConf`) previously proxied
only `/a2a/`, `= /mcp`, and `/.well-known/`; everything else fell through to
the SPA. This change adds three location blocks that proxy exactly these GET
routes to the existing namespace-scoped AF HTTPS upstream. No new identity
transport is introduced: oauth2-proxy remains the only ingress and injects
`X-Forwarded-Access-Token`; nginx overwrites any client-supplied
`Authorization` header with the trusted session token and does not forward the
raw token to AF in a second header. Authorization stays with AF (named
capability SARs and user-scoped target checks; remote lists intentionally
return `unavailable`). A2A/MCP behavior, the SPA fallback, security headers,
rate limits, and upstream TLS certificate verification are unchanged.

A controller companion change extends the existing subPath rollout mechanism
(`deployConfigMaps`) to the `console-nginx` ConfigMap: a drift-repaired nginx
configuration now changes the Console pod-template hash so the running nginx
actually reloads it, mirroring the `console-oauth2-proxy` behavior from #606.

## 2. Control objectives

| Objective | Business behavior | Evidence |
|---|---|---|
| FedRAMP/NIST AC-3 (access enforcement) | The Console read API boundary is read-only: non-GET methods are denied at the nginx boundary, and only the three named read routes are proxied. | `UT-CN-638-001`, `UT-CN-638-003`, `UT-CN-638-006`; `IT-CN-638-001`; `E2E-CN-638-HTTP-001` |
| FedRAMP/NIST AC-6 (least privilege) | Identity forwarding uses only the oauth2-proxy session token; client-supplied identity and token headers cannot influence the forwarded credential, and the raw token is not passed to AF in a second header. | `UT-CN-638-004`; `IT-CN-638-001`; `E2E-CN-638-HTTP-001` |
| SOC 2 CC6.1 | The proxy boundary enforces the trusted identity transport and denies unauthenticated access before AF. | `UT-CN-638-004`; `E2E-CN-638-HTTP-001` (unauthenticated denial) |
| SOC 2 CC6.3 | Configured access restrictions (read-only methods, rate limit, TLS-verified upstream) are reviewed and evidenced. | `UT-CN-638-003`–`005`; `E2E-CN-638-HTTP-001` (POST denied with 403) |
| OWASP ASVS v5.0.0 V4.1.1, V4.2.1 (access control) | Route-level access decisions happen at the trusted proxy/AF layer, not the browser; requests without a valid session never reach AF. | `UT-CN-638-004`; `E2E-CN-638-HTTP-001` |
| OWASP ASVS v5.0.0 V14.4.4 / V9.2.2 (transport, credential disclosure) | Upstream connections stay HTTPS with certificate verification; responses and logs do not disclose tokens. | `UT-CN-638-005`; existing `UT-CN-198-*`; `E2E-CN-638-HTTP-001` (no `access_token` in bodies) |
| FedRAMP/NIST CM-6 / SOC 2 CC6.8 (configuration) | A repaired/drifted nginx ConfigMap is applied to a running deployment (subPath rollout). | `IT-CN-638-002`; `UT-AUDIT-515-API-020` (updated) |

These mappings are engineering objectives, not formal FedRAMP authorization,
SOC 2 attestation, or OWASP ASVS conformance claims.

## 3. Test pyramid

### Unit (`internal/resources/console_test.go`)

- `UT-CN-638-001`: the three Console read locations route to the
  namespace-scoped AF upstream over HTTPS.
- `UT-CN-638-002`: `proxy_pass` carries no URI suffix so the request path,
  query parameters, and opaque identifiers survive unchanged; the
  managed-resources regex is declared before the static-asset regex so dotted
  identifiers reach AF. Multi-segment tails and a trailing slash intentionally
  match the regex and receive AF's own 404.
- `UT-CN-638-003`: each read location restricts methods with
  `limit_except GET { deny all; }`. `limit_except GET` also permits HEAD; this
  is intentional parity with AF's Go `ServeMux` `"GET"` pattern registration
  (Go 1.22 net/http matches HEAD for GET patterns).
- `UT-CN-638-004`: each read location rewrites `Authorization` from the
  oauth2-proxy session token, clears `X-Forwarded-Access-Token` toward AF, and
  keeps the shared `zone=api` rate limit.
- `UT-CN-638-005`: `proxy_ssl_verify on` stays enabled and no plaintext
  `proxy_pass http://` appears.
- `UT-CN-638-006`: no generic `/api` prefix location exists; `/a2a/`, `= /mcp`,
  `/.well-known/`, and the SPA fallback remain unchanged.
- `UT-AUDIT-515-API-020` (updated): revision read failures for both Console
  ConfigMaps surface as reconciliation errors.
- Uncached-reader unit test (updated): both `console-nginx` and
  `console-oauth2-proxy` revisions fold into the Console pod-template hash.

### Integration (`internal/controller/kubernaut_lifecycle_test.go`, envtest)

- `IT-CN-638-001`: Console-enabled reconciliation creates the nginx ConfigMap
  with the `/api/v1/console/` read routing, trusted identity forwarding, and
  the GET-only restriction (create path).
- `IT-CN-638-002`: stripping the read routing from a live ConfigMap
  (simulating a pre-#638 deployment) is repaired on the next reconcile and
  changes the Console pod-template hash, rolling the subPath-mounted pod
  (upgrade/drift path).

### E2E / qualification (`test/e2e/kind/qualification_test.go`, opt-in lane)

- `E2E-CN-638-HTTP-001`: with a real Keycloak login through the deployed
  oauth2-proxy, the three endpoints (with query parameters and an opaque dotted
  identifier) return AF JSON responses rather than SPA HTML; forged identity
  and bearer headers are not reflected; unauthenticated requests are denied;
  POST is rejected with 403; no `access_token` appears in any body.
- The existing `E2E-CN-609-HTTP-002` continues to prove A2A/MCP routing is
  unaffected.

## 4. Design notes

- **Location precedence.** Exact-match (`=`) locations win over every other
  match. The managed-resources detail location is a regex placed before the
  static-asset regex (`~* \.(js|css|...)$`) so an opaque identifier ending in
  `.js` is proxied to AF instead of being captured by the SPA asset handler.
- **Path and query preservation.** `proxy_pass` is a bare
  `https://apifrontend.<ns>.svc:8443` with no URI suffix; nginx forwards the
  original request URI and query string unmodified (a URI suffix would also be
  forbidden inside a regex location).
- **Trusted identity only.** oauth2-proxy alpha-config
  `injectRequestHeaders` with `claimSource` overwrites any client-supplied
  `X-Forwarded-Access-Token` with the verified claim; nginx additionally
  overwrites client `Authorization` and clears `X-Forwarded-Access-Token`
  toward AF. AF authenticates solely via `Authorization: Bearer`
  (`pkg/apifrontend/auth/middleware.go`). The pre-existing `/a2a/` and `= /mcp`
  locations do not clear `X-Forwarded-Access-Token` toward AF; that divergence
  is tracked as a follow-up and intentionally not bundled here.
- **AF version mismatch.** If the deployed AF predates the #2604 routes, nginx
  proxies to AF's own 404; AF's sanitized `state: "unavailable"` envelopes pass
  through untouched (no `proxy_intercept_errors` is set).

## 5. Version prerequisites and qualification gaps

- Operator: this change (v1.6 milestone). The nginx ConfigMap drift rollout
  requires this operator version; older operators repair the ConfigMap but do
  not roll the pod.
- API Frontend: must include kubernaut#2604 (console policy read routes,
  commit `e667cfc` or later).
- Console: kubernaut-console#164 consumes the endpoints; the SPA degrades
  gracefully when AF returns 404/unavailable envelopes.
- The authenticated deployed browser/proxy-to-AF qualification
  (`E2E-CN-638-HTTP-001`) runs only in the opt-in manual Kind qualification
  lane (`test/e2e/kind`); it was not executed against a live cluster in this
  change's development environment. Remaining gaps for that lane: deployed
  missing/expired-token and capability-denial cases depend on the Keycloak
  fixture's user capabilities; AF denial (403 problem detail) is asserted only
  as "not SPA HTML" so the lane does not depend on a specific RBAC fixture.

## 6. Exit criteria

- `go build ./...`, `golangci-lint run` (0 issues), `make test` green.
- `make manifests generate` produces no diff (no CRD change).
- Unit coverage of `internal/resources/` stays ≥ 96%; controller coverage
  stays ≥ 78%.
- All `#638` UT/IT specs pass; A2A/MCP and SPA regression specs pass.

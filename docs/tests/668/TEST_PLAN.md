# IEEE 829 Test Plan — Issue #668: Clear X-Forwarded-Access-Token toward API Frontend on /a2a/ and /mcp proxy locations, and clear forgeable identity headers on all Authorization-rewriting AF-facing locations

## 1. Scope

Issue #668 extends the #638 token hygiene to every Console NGINX location
that proxies to API Frontend (AF) with a rewritten `Authorization`:

- `location /a2a/` — add `proxy_set_header X-Forwarded-Access-Token "";`
  (matching the #638 console read location).
- `location = /mcp` — the same clear.
- `location ^~ /api/v1/console/` — fold in clearing of the forged-identity
  headers `X-Forwarded-User` / `X-Forwarded-Email` (per the review follow-up
  comment on #668); the raw access-token clear is #638's `UT-CN-638-007`.

All three locations also clear `X-Forwarded-User` / `X-Forwarded-Email`.
AF authenticates solely via `Authorization: Bearer`
(`pkg/apifrontend/auth/middleware.go` `extractBearerToken` in the upstream
kubernaut repo; no upstream Go code reads `X-Forwarded-User`,
`X-Forwarded-Email`, or `X-Forwarded-Access-Token` — `X-Forwarded-For` is
read by AF's trusted-realip logic and is deliberately preserved), so this
is defense-in-depth only: no behavior change for A2A, MCP, or the Console
read API. The `Authorization` rewrite from the OAuth2 Proxy-injected
`$http_x_forwarded_access_token`, rate limits, streaming timeouts,
`Mcp-Session-Id` forwarding, and upstream TLS verification are unchanged.

## 2. Control objectives

| Objective | Business behavior | Evidence |
|---|---|---|
| FedRAMP/NIST AC-6 (least privilege) | The raw session token is not forwarded to AF in a second header on any AF-facing proxied route; client-forgeable identity headers do not reach AF. | `UT-CN-668-001`–`003`; `IT-CN-638-001` (extended) |
| SOC 2 CC6.1 | The trusted identity transport toward AF is exactly one header: `Authorization: Bearer` from the OAuth2 Proxy session. | `UT-CN-668-001`–`003`; `E2E-CN-609-HTTP-002` |
| OWASP ASVS v5.0.0 V14.4.4 / V9.2.2 (credential disclosure) | Responses and logs disclose neither the raw token nor forged identity values on A2A/MCP routes. | `E2E-CN-609-HTTP-002` (unchanged behavior, existing assertions) |

These mappings are engineering objectives, not formal FedRAMP authorization,
SOC 2 attestation, or OWASP ASVS conformance claims.

## 3. Test pyramid

### Unit (`internal/resources/console_test.go`)

New `#668` context with a block-scoped `nginxLocationBlock` extraction helper
(a missing marker fails loudly; each extracted block is guarded to contain
its `proxy_pass`):

- `UT-CN-668-001`: `/a2a/` clears `X-Forwarded-Access-Token`,
  `X-Forwarded-User`, `X-Forwarded-Email` toward AF; the trusted-token
  `Authorization` rewrite, `api` rate limit, and 3600s streaming overrides
  are unchanged.
- `UT-CN-668-002`: `= /mcp` clears the same three headers; `Mcp-Session-Id`
  forwarding, the `mcp` rate limit, and the `Authorization` rewrite are
  unchanged.
- `UT-CN-668-003`: the `^~ /api/v1/console/` read location clears
  `X-Forwarded-User` / `X-Forwarded-Email` (access-token clear already
  covered by `UT-CN-638-007`).

### Integration (`internal/controller/kubernaut_lifecycle_test.go`, envtest)

- `IT-CN-638-001` (extended): block-scoped assertions that all three
  AF-facing locations in the reconciled `server.conf` clear the three
  headers and keep the `Authorization` rewrite. Block scoping is required:
  a whole-file `ContainSubstring` would be vacuous once the console read
  location carries the same clear directives.

### E2E / qualification (`test/e2e/kind/qualification_test.go`, opt-in lane)

- `E2E-CN-609-HTTP-002` (unchanged): already sends forged
  `X-Forwarded-Access-Token` / `X-Forwarded-User` / `X-Forwarded-Email` (and
  forged `Authorization`) on `/a2a/access` and `/mcp` and asserts the valid
  OAuth2 Proxy session overrides them, responses do not reflect forged
  values, and unauthenticated requests are denied. The clears remove the
  residual pass-through of those headers toward AF; the observable E2E
  contract is unchanged, so no E2E assertion changes.

## 4. Design notes

- **Header semantics differ by header.** oauth2-proxy (the only ingress)
  injects only `X-Forwarded-Access-Token`
  (`InjectRequestHeaders` in `ConsoleOAuth2ProxyConfigMap`), overwriting any
  client-supplied value: toward nginx it is trusted data that must not leak
  onward. `X-Forwarded-User` / `X-Forwarded-Email` are **not** injected by
  this deployment, so toward nginx they are always client-supplied and
  forgeable: clearing them toward AF is defense-in-depth against forged
  identity reaching the backend hop. AF reads neither.
- **No behavior change.** AF's `extractBearerToken` is the sole
  authentication path; no upstream kubernaut Go code references
  `X-Forwarded-User`, `X-Forwarded-Email`, or `X-Forwarded-Access-Token`.
  The clears therefore cannot change A2A/MCP/Console-read outcomes.
- **Out of scope.** The `/.well-known/` location performs no
  `Authorization` rewrite and passes client `X-Forwarded-*` headers toward
  AF raw; it is not named by #668 and is left unchanged (tracked as #679).
  Other forgeable headers (`X-Forwarded-Groups`,
  `X-Forwarded-Preferred-Username`, `X-Auth-Request-*`) also pass through;
  nothing reads them.
- **Rollout.** No wiring change: the directives live in the same
  `consoleNginxServerConf` rendered into the `console-nginx` ConfigMap,
  whose resourceVersion already rolls the Console pod (#638 drift
  mechanism).

## 5. Version prerequisites

- Operator: #660 + #638 (PR #669) baseline; this change layers on top.
- API Frontend: any version; the change assumes nothing about AF beyond
  `Authorization: Bearer`-only authentication (verified in the upstream
  repo at the time of writing).

## 6. Exit criteria

- `UT-CN-668-001`–`003` pass; `UT-CN-638-001`–`007` and the #198/#609-era
  proxy assertions pass unchanged (no regression).
- `IT-CN-638-001` passes with the extended block-scoped assertions.
- `go build ./...`, `golangci-lint run`, and `make test` are green;
  `make manifests generate` produces no diff (no CRD change).

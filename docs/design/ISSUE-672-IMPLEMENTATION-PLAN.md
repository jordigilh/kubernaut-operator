# Issue #672 — Patterns for URL-typed CRD fields that carry none

## Status

Pre-implementation plan (follow-up to #658 / PR #670, applying the same
methodology to the URL-typed fields that carry no CRD pattern at all).
The issue body is the complete spec; owner pre-approved this workflow
including implementation and PR.

## 1. Preflight findings (corpus, blast radius)

Corpus re-verified on the post-#670 merge base (d9ec2e6). Seven URL-typed
fields in `api/v1alpha2/kubernaut_types.go` carry no
`+kubebuilder:validation:Pattern` marker; each field's contract was
derived from its corresponding admission validator in
`internal/resources/validation.go` (or, where no validator exists, from
the field's documentation and consumer contract):

| # | Field (json path) | Line | Validator | Derived contract |
|---|---|---|---|---|
| 1 | `spec.fleet.oauth2.tokenURL` (`FleetOAuth2Spec.TokenURL`) | ~:357 | `validateFleetOAuth2` → `validateFleetHTTPSURL` | absolute **https** URL, non-empty host, **no userinfo** (http rejected; whitespace rejected) |
| 2 | `spec.ai.llm.profiles[].oauth2.tokenURL` (`OAuth2Spec.TokenURL`, under `LLMProfileSpec`) | ~:1521 | `validateLLMProfileOAuth2` → `validateFleetHTTPSURL` | same as #1 |
| 3 | `spec.workflowExecution.ansible.apiURL` (`AnsibleSpec.APIURL`) | ~:560 | **none** (no shape validator anywhere; only the CEL required-when-enabled rule) | genuinely URL-shaped (AWX/AAP API endpoint; docs always show a URL); no validator constrains scheme — AWX dev deployments commonly run plain http, so scheme must NOT be pinned |
| 4 | `spec.apiFrontend.auth.jwtProviders[].jwksURL` (`JWTProviderSpec.JWKSURL`) | ~:1177 | `validateJWTProviderJWKSURL` → `validateOIDCURL` | absolute URL with scheme + host; **https, or http under the allowInsecure dev flag**; userinfo tolerated (OIDC validators do not check `u.User`) |
| 5 | `spec.apiFrontend.auth.jwksURL` (`APIFrontendAuthSpec.JWKSURL`) | ~:2180 | **none** on the explicit value (empty is derived from issuerURL at render time) | same URL family as its sibling `issuerURL`, which already carries `^https?://([^/?#]*@)?[^:/?#]`; http stays admissible for parity with the allowInsecure escape hatch |
| 6 | `spec.monitoring.prometheus.url` (`PrometheusSpec.URL`) | ~:2602 | `validateMonitoringEndpoints` | absolute **https** URL, non-empty host, **no userinfo** |
| 7 | `spec.monitoring.alertManager.url` (`AlertManagerSpec.URL`) | ~:2629 | `validateMonitoringEndpoints` | same as #6 |

All seven are genuinely URL-shaped — **no field is skipped**. Fields #3
and #5 have no admission validator, so their pattern is derived by the
#658 minimal-delta principle instead: change nothing except removing the
hostless class (no scheme pinning, no userinfo rejection), so no
in-use value can be newly rejected relative to today's behavior.

## 2. Spike: exact pattern form (per field)

Reusing the #670 derivation (`docs/design/ISSUE-658-IMPLEMENTATION-PLAN.md`
§2): net/url's `Hostname()` is empty exactly when the authority is empty
or its host part is missing, so `[^:/?#]` after the scheme (and after
optional userinfo) is precisely the host-present requirement.

| Field(s) | Pattern | Rationale |
|---|---|---|
| #1, #2 (fleet/LLM tokenURL) | `(?i)^$|^https://(?:[^:/?#\[]|\[[^]]+\])` | validator pins https and rejects userinfo; `^$|` keeps the empty value admissible; the bracketed alternative rejects an empty bracketed host (`https://[]:443`) while keeping IPv6 |
| #3 (ansible apiURL) | `(?i)^$|^https?://(?:[^/?#]*@)?(?:[^:/?#\[]|\[[^]]+\])` | no validator; minimal delta — http AWX endpoints stay admissible (newly-rejected-set analysis below) |
| #4, #5 (both jwksURL) | #4 `(?i)^https?://(?:[^/?#]*@)?(?:[^:/?#\[]|\[[^]]+\])`; #5 `(?i)^$|^https?://(?:[^/?#]*@)?(?:[^:/?#\[]|\[[^]]+\])` | OIDC contract: http admissible under allowInsecure flag, userinfo tolerated; #4 is required (MinLength=1) so it takes no `^$|` alternative, #5 is optional |
| #6, #7 (prometheus/alertManager url) | `(?i)^$|^https://(?:[^:/?#\[]|\[[^]]+\])` | `validateMonitoringEndpoints` pins https and rejects userinfo |

**Bracketed host.** The plain `[^:/?#]` host probe accepts `https://[]:443`
(the first character after the scheme is `[`). The bracketed alternative
`\[[^]]+\]` requires non-empty content inside the brackets, so an empty
bracketed host is schema-rejected exactly as net/url rejects it
("unable to parse IP"), while `[2001:db8::1]` style hosts stay
admissible.

**Scheme case.** `url.Parse` lowercases the scheme, so the admission
validators accept `HTTPS://host`; the patterns therefore carry inline
`(?i)` so the schema stays a superset of the validator accept-set. The
http-versus-https restriction is unaffected (the inline flag only
removes case sensitivity).

**Empty-string handling (six optional fields).** Kubernetes structural
schemas validate `pattern` against a *present* empty string, and all six
optional fields legally carry `""` today (validators treat it as unset).
The minimal-delta principle therefore requires the `^$|` alternative for
them. This is exactly how #670 treated its fields: `agentCardURL`'s old
pattern already carried `^$|` and kept it, while `issuerURL`'s old
pattern `^https?://.+` already rejected `""` and kept doing so — each
preserved its own prior accept-set. The pattern-less fields here admit
`""` today, so they keep admitting it. Only `JWTProviderSpec.JWKSURL`
(required, `MinLength=1`) takes the bare form. Note: because the Go
client's `omitempty` never sends `""`, the empty-value branch is
untestable through the typed client; it is covered by the
pattern-equality assertion in `UT-672-API-001` (mirroring the
`agentCardURL` assertion in `UT-658-API-001`), a documented decision
rather than an untested oversight.

**Newly-rejected set for the validator-less fields #3 and #5.** These
have no admission validator, so today's schema accept-set is every
string. Beyond the hostless class, the patterns also newly reject:
values with leading/trailing whitespace, non-http(s) schemes
(`ftp://…`, `unix://…`), and schemeless values (`awx.example.com:8080`).
None of these are usable by an AWX API client or a JWKS fetcher (both
require an absolute http(s) URL), so no working deployment can regress;
this is the same class of justified tightening the #658 plan applied
via its validators, here justified by the consumer contract instead.

Backward compatibility holds by construction per field: for #1/#2/#4/#6/#7
the only newly-rejected strings are ones the admission validators already
reject at reconcile (a hostless CR degrades today, so no working
deployment can regress); for #3/#5 the newly-rejected set is enumerated
above and contains no consumer-usable value.

Known accepted residue (documented; inherited from the #670
methodology): RE2 has no lookahead, so userinfo-with-empty-host
(`https://user@:443`) cannot be rejected by a prefix pattern — it
remains schema-admitted, validator-rejected (#1/#2/#4/#6/#7) or
consumer-rejected (#3/#5) exactly as today.

## 3. TDD plan

### RED

1. `api/v1alpha2/clean_break_test.go` — extend the `UT-658-API-001`
   contract suite with `UT-672-API-001`: assert each of the seven
   generated schema patterns equals the form above.
2. `internal/controller/issue672_url_pattern_integration_test.go` — new
   envtest CRD-apply suite following `issue658_url_pattern_integration_test.go`
   (pattern enforcement needs a real API server surface):
   - `IT-672-PAT-001..007`: per field, `https://:443` rejected at
     `k8sClient.Create` with an error naming the field and
     `should match`, and nothing persisted;
   - `IT-672-PAT-008`: representative valid values accepted per field
     (host, host:port, bracketed IPv6, path-carrying, per-field
     validator-compatible values so a lingering CR stays valid);
   - `IT-672-PAT-009`: userinfo-tolerant fields accept a
     colon-prefixed userinfo with a host (#658 CodeRabbit compatibility
     case); http acceptance for ansible apiURL / jwksURL covered.

### GREEN

1. Add the seven `+kubebuilder:validation:Pattern` markers in
   `api/v1alpha2/kubernaut_types.go` (comment text otherwise unchanged).
2. `make manifests generate`, `make generate`, `make bundle`,
   `make build-installer`; cp the CRD to
   `charts/kubernaut-operator/files/` (no make target exists); run
   `hack/verify-helm-chart.sh` and `hack/verify-console-release.sh` as
   drift gates; review the diff is limited to the seven patterns.

### REFACTOR

- Marker-only change, no new components; full gates: `go build ./...`,
  `golangci-lint run`, `make test` (envtest suite ~700s, extended
  timeout; copy `bin/k8s` envtest assets from a sibling worktree if
  the fresh worktree lacks them).

## 4. Wiring manifest

| Component | Production entry point | Wiring code location | IT test ID |
|---|---|---|---|
| (marker-only CRD schema change — no new resource builder or reconciler path) | API server structural-schema validation on CR apply | `api/v1alpha2/kubernaut_types.go` markers → generated `config/crd/bases`, bundle, chart, dist | `internal/controller/issue672_url_pattern_integration_test.go` |

CHECKPOINT W is trivially satisfied: no new `internal/resources` builder.

## 5. Risks

- Generated-artifact drift: mitigated by the full generation chain and
  a diff review limited to the seven patterns.
- Test fixtures asserting the absence of patterns on these fields or
  using hostless values for them: corpus search in RED (existing suites
  use well-formed URLs for all seven fields).
- envtest singleton constraint: rejection tests never persist the CR;
  acceptance tests clean up via `deleteCRIfExists`.

Confidence: 96% — marker-only change, six of seven patterns derived
directly from validators' accept-sets, two validator-less fields handled
by a documented newly-rejected-set analysis, empty-string semantics
preserved via the `^$|` alternative; residual risk is
generated-artifact sync and unknown fixture references, both
mechanically checkable.

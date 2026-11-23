# Issue #658 — Tighten CRD URL patterns to reject hostless values

## Status

Pre-implementation plan (owner-approved approach option 1 from the issue:
tighten the kubebuilder patterns so the schema requires a host character
after the scheme, mirroring the accept-set the admission validators
already enforce).

## 1. Preflight findings (corpus, blast radius)

Every URL-typed CRD field in `api/v1alpha2/kubernaut_types.go` carrying a
lenient `^https?://.+-style` pattern (exhaustive corpus search of the
package; no other `Pattern=` marker references `http`):

| # | Field (json path) | Line | Current pattern |
|---|---|---|---|
| 1 | `spec.apiFrontend.auth.jwtProviders[].issuerURL` (`JWTProviderSpec.IssuerURL`) | ~1167 | `^https?://.+` |
| 2 | `spec.console.auth.redirectURL` (`ConsoleAuthSpec.RedirectURL`) | ~1643 | `^https://.+` |
| 3 | `spec.console.auth.providerLogoutURL` (`ConsoleAuthSpec.ProviderLogoutURL`) | ~1652 | `^https://.+` |
| 4 | `spec.apiFrontend.agentCardURL` (`APIFrontendSpec.AgentCardURL`) | ~1998 | `^$|^https?://` |
| 5 | `spec.apiFrontend.auth.issuerURL` (`APIFrontendAuthSpec.IssuerURL`) | ~2167 | `^https?://.+` |

Notes:

- Field 4's pattern lacks even `.+` — it admits a bare `https://` (the
  hostless class plus the empty-authority class).
- `JWTProviderSpec.JWKSURL` and `APIFrontendAuthSpec.JWKSURL` carry no
  `http` pattern at all (MinLength/MaxLength only); they are **out of the
  bounded scope** ("fields carrying a lenient `^https?://.+-style`
  pattern"). No admission-validator change is in scope either.
- Known follow-up (out of scope, recorded for blast-radius accuracy):
  other URL-typed fields carry **no** CRD pattern at all and are only
  host-validated at reconcile — `FleetOAuth2Spec.TokenURL` (~:357),
  `LLMProfileSpec`-level `TokenURL` (~:1521), `AnsibleSpec.APIURL`
  (~:560), both `JWKSURL` fields (~:1177, ~:2180),
  `Monitoring.Prometheus.URL` (~:2602), and `AlertManagerSpec.URL`
  (~:2629). The hostless class is schema-admitted there today too;
  tightening those would be a separate, owner-approved scope change.

Authoritative admission validators (unchanged; the fail-closed layer):

- `validateOIDCURL` (issuer fields 1, 5): `url.Parse` ok, scheme
  non-empty, `Hostname() != ""`, scheme https (or http with the
  allowInsecure dev escape hatch).
- `validateAPIFrontend` agentCard check (field 4): `url.Parse` ok,
  scheme non-empty, `Hostname() != ""`.
- `validateConsoleRedirectURL` (field 2) and
  `parseConsoleProviderLogoutURL`/`validateConsoleLogoutURLShape`
  (field 3): https scheme (case-insensitive), `Hostname() != ""`, no
  userinfo, plus shape constraints.

## 2. Spike: exact pattern form

**Decision: `^https?://[^:/?#]` (https-only fields: `^https://[^:/?#]`;
agentCardURL: `^$|^https?://[^:/?#]`).**

Derivation from the validators' accept-set:

- `url.URL.Hostname()` is empty exactly when the authority is empty or
  starts with `:` (port-only, e.g. `https://:443`) — because the
  authority runs to the first `/`, `?`, or `#`. So the hostless class
  the validators reject is precisely: authority missing or starting
  with `:`, `/`, `?`, `#`.
- Therefore `[^:/?#]` after the scheme requires at least one authority
  character that can begin a host (or userinfo), which:
  - rejects `https://:443`, `https://`, `https:///path`,
    `https://?q`, `https://#f` — exactly the hostless class;
  - accepts everything else the old pattern accepted, including
    `localhost`, IPv4 (`https://10.0.0.1:8443`), bracketed IPv6
    (`https://[2001:db8::1]:443/realms/x`), `host:port`, path-carrying
    and query-carrying URLs, and `userinfo@host` forms the issuer/agent
    validators still accept.
- Rationale vs the issue's illustrative `[a-zA-Z0-9\[]`: a negated
  class is the minimal delta from the current accept-set — it changes
  nothing except removing the hostless class, so "no valid in-use value
  is newly rejected at the API server" holds by construction (the only
  newly-rejected strings are ones whose `Hostname()` is empty, which
  the admission validators already reject at reconcile → a hostless CR
  degrades today, so no working deployment can regress).
- Known degenerate residue (documented, accepted): a userinfo that
  itself starts with `:` (`https://:secret@host`) is
  validator-accepted but pattern-rejected; a leading-`@` authority
  (`https://@:443`) is pattern-accepted but validator-rejected. Both
  are absurd in practice, both were already schema-lenient before this
  change, and neither is newly rejected relative to a working
  deployment.
- Kubernetes structural-schema patterns are partial matches (RE2
  search), as today: the `^` anchor keeps the semantics identical to
  the current patterns, just with the host requirement added.

## 3. TDD plan

### RED

1. `api/v1alpha2/clean_break_test.go` — new spec asserting the five
   generated schema patterns equal the tightened forms (extend the
   existing `loadKubernautCRD()` contract-suite style; update the
   existing `UT-483-API-001` expectation for `providerLogoutURL`).
2. `internal/controller/issue658_url_pattern_integration_test.go` —
   envtest CRD-apply coverage (schema-pattern enforcement needs a real
   API server surface, following `issue617_confidence_threshold_integration_test.go`):
   - for each affected field: `https://:443` is rejected at
     `k8sClient.Create` with an error naming the field (also cover bare
     `https://` for agentCardURL);
   - representative valid values pass at apply time, chosen per field so
     they also satisfy the reconcile-level validators in case the CR
     lingers: issuerURL/provider issuerURL
     `https://idp.example.com/realms/x` and an IPv6 form
     `https://[2001:db8::1]:8443/realms/x` (with
     `allowInsecureIssuers`-free https), redirectURL
     `https://console.example.com:8443/oauth2/callback`, providerLogoutURL
     `https://idp.example.com/realms/x/protocol/openid-connect/logout?id_token_hint={id_token}`
     (matching the validation_test.go fixture), agentCardURL
     `https://localhost:8443/a2a/invoke` and omitted. Cleanup happens in
     AfterEach (`deleteCRIfExists`), which is immediate because the
     envtest suite runs no manager and the CR carries no finalizer.
     Rejection tests' creates fail at the API server and persist
     nothing, so the singleton name is free for the next case (fresh
     `newMinimalCR()` per rejected create).

### GREEN

1. Replace the five `+kubebuilder:validation:Pattern` markers in
   `api/v1alpha2/kubernaut_types.go` (comment text unchanged otherwise).
2. `make manifests generate`, `make generate`, `make bundle`,
   `make build-installer`; then sync the chart CRD copy manually —
   no Makefile target generates it — via
   `cp config/crd/bases/kubernaut.ai_kubernauts.yaml charts/kubernaut-operator/files/`,
   and run `hack/verify-helm-chart.sh` and `hack/verify-console-release.sh`
   as the drift gates; review the diff is limited to the five patterns.

### REFACTOR

- No new components (pattern-marker change only); verify no Go code
  referenced the old pattern strings except the contract test updated
  in RED. Full gates: `go build ./...`, `golangci-lint run`,
  `make test` (envtest controller suite ~700s, extended timeout).

## 4. Wiring manifest

| Component | Production entry point | Wiring code location | IT test ID |
|---|---|---|---|
| (marker-only CRD schema change — no new resource builder or reconciler path) | API server structural-schema validation on CR apply | `api/v1alpha2/kubernaut_types.go` markers → generated `config/crd/bases`, bundle, chart, dist | `internal/controller/issue658_url_pattern_integration_test.go` |

CHECKPOINT W is trivially satisfied: no new `internal/resources` builder.

## 5. Risks

- Generated-artifact drift: mitigated by running the full generation
  chain and diffing only the five patterns.
- Test fixtures elsewhere asserting the old pattern strings: found one
  (`UT-483-API-001`); corpus search repeats in RED to catch any others.
- envtest singleton constraint: rejection tests never create the CR;
  acceptance test cleans up via `deleteCRIfExists`.

Confidence: 95% — marker-only change derived directly from the
validators' accept-set; residual risk is generated-artifact sync and
unknown fixture references, both mechanically checkable.

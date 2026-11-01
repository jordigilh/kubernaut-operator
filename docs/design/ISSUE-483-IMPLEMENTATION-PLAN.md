# Issue #483 — Persist Console provider logout configuration

**Issue:** https://github.com/jordigilh/kubernaut-operator/issues/483
**Status:** Implementation complete locally; QE runtime qualification pending
**Scope:** `kubernaut.ai/v1alpha2` operator API and Console workload
**Date:** 2026-10-15

## 1. Decision summary

1. Add one optional `spec.console.auth.providerLogoutURL` field to the
   v1alpha2 API. The name describes the provider endpoint rather than exposing
   an OAuth2 Proxy implementation detail.
2. Render the configured value unchanged as
   `--backend-logout-url=<value>` in the oauth2-proxy container. The literal
   `{id_token}` placeholder remains literal in the CR and is substituted only
   by oauth2-proxy at sign-out time.
3. Accept only an absolute HTTPS URL. HTTP is rejected in every environment,
   including localhost and development. Local development must use HTTPS with
   an explicitly trusted development CA; no insecure override is added.
4. Require the endpoint authority to match the effective Console OIDC provider
   authority (the explicit issuer, or the first configured JWT provider). Reject
   malformed URLs, user information, fragments, unsafe placeholder syntax, and
   provider-authority mismatches. Do not make network requests during CR
   validation. Certificate verification for the actual backend logout request
   must be proven separately; HTTPS syntax alone is not sufficient.
5. Do not add a return-target field or client-side token handling. OAuth2 Proxy
   retains ownership of the validated sign-out return flow; the Console SPA
   receives neither the ID token nor a logout URL containing it.
6. Preserve the existing v1alpha2-only clean-break contract. The approved scope
   explicitly discards v1alpha1 conversion: do not add a v1alpha1 package,
   conversion webhook, dual-served CRD version, or compatibility path.

### Required methodology checkpoints completed

- **Checkpoint A:** read `api/v1alpha2/kubernaut_types.go` before selecting the
  field location; `ConsoleAuthSpec` currently contains `SecretName` only.
- **Checkpoint B:** searched existing Console builders, validation helpers,
  lifecycle tests, ownership paths, samples, CRD artifacts, and OAuth2 Proxy
  configuration before proposing a new field/helper.
- **Checkpoint C:** verified the existing production caller chain from
  `KubernautReconciler` through `ensureDeployments` to `ConsoleDeployment`; no
  orphan builder is proposed.
- **Checkpoint DD:** alternatives and approval gates are recorded below; the
  plan remains gated until the unresolved runtime/ownership decisions have
  explicit YES/NO outcomes.

## 2. Preflight findings

| Area | Evidence | Finding / impact |
|---|---|---|
| API | `api/v1alpha2/kubernaut_types.go:1548-1582` | `ConsoleAuthSpec` currently contains only `SecretName`; add the field here. |
| API versioning | `docs/upgrade-v1alpha1-to-v1alpha2.md`, `api/v1alpha2/clean_break_test.go` | v1alpha2 is sole served/storage API and has no conversion webhook. Do not recreate v1alpha1 for this issue. |
| Deployment rendering | `internal/resources/console.go:41-123` | `ConsoleDeployment` owns the oauth2-proxy argument list; no logout argument exists. |
| Reconciliation | `internal/controller/kubernaut_controller.go:2008-2012,2022-2039` | Existing `enabledDeploymentBuilders` → `ensureDeployments` path already rebuilds and updates Console Deployments. No new phase or builder is needed. |
| Validation | `internal/resources/validation.go:38-51,553-619` | `ValidateKubernaut` is the fail-closed pre-deployment validation seam. Existing URL validation patterns can be extended without network access. |
| Integration seam | `internal/controller/kubernaut_lifecycle_test.go:819-900` | Envtest already proves Console Deployment creation and redirect rendering; extend this path for logout persistence and drift repair. |
| Ownership | `internal/controller/kubernaut_controller.go:4588-4651`, `internal/resources/ownership.go` | #483 starts from a fresh operator deployment; the operator creates and owns the full Console resource set. The upstream Helm chart is a separate deprecated alternative, not a co-owner or migration step. |
| Documentation | `docs/installation/02-configure-services.md:525-586`, `config/samples/v1alpha2_kubernaut.yaml:348-353` | Console auth configuration is documented in one installation section and the full sample. |
| Runtime behavior | OAuth2 Proxy 7.9.0 operator image and upstream Helm's v7.15.3 default | Both source versions replace `{id_token}` and perform backend logout with server-side `http.Get`; no SPA implementation is required, but the configured CA path may not apply to this request. |
| Browser qualification | `kubernaut-console/e2e/live/logout.spec.ts`, Console issue #139 | The Console repository already has live Keycloak logout coverage, including local-state clearing; that journey previously passed, but must be rerun against a fresh operator-built deployment rather than creating a new browser harness here. |
| Operator full-deployment qualification | Upstream `kubernaut#2492`, operator #489 planning/evidence | The upstream full-pipeline/Fleet operator-deployment migration is tracked separately; #483 can consume its operator-built deployment pattern but must retain its own Console logout assertions. |
| Upstream Helm parity | `jordigilh/kubernaut/charts/kubernaut/templates/console/console.yaml`, `values.yaml`, `values.schema.json` | Upstream Helm already renders `console.oauth2Proxy.backendLogoutURL`, but it is a separate deprecated installation path and is out of scope for a fresh operator deployment. |

### Baseline verification

- Focused API/resources tests passed during preflight; final verification uses
  the repository `make test-unit`, `make test-integration`, and `make test`
  targets.
- `make test-unit` — **passed** on rerun with the project Make target:
  6,502 independent business-unit statements, 0 misses, 100% internal unit
  coverage, and no working-tree changes.
- `make setup-envtest` provisioned Kubernetes 1.37 envtest assets, then
  `make test-integration` **passed** with 78.1% controller coverage, meeting
  the repository's 78% integration threshold.
- `git diff --check` and working-tree status — **clean** before this plan file.

## 3. Design alternatives and trade-offs

| Decision | Alternative | Pros | Cons / decision |
|---|---|---|---|
| API surface | Derive logout endpoint from the issuer at runtime | No new CR field | Requires network discovery during reconciliation, provider-specific assumptions, and no deterministic offline render; **rejected**. |
| API surface | Expose a generic `backendLogoutURL` field | Maps directly to OAuth2 Proxy | Leaks implementation terminology and weakens the provider/return-target contract; **rejected**. |
| API surface | `ConsoleAuthSpec.providerLogoutURL` | Explicit, optional, provider-neutral, and preserves the existing Console auth grouping | Requires CRD generation and validation; **chosen**, pending normal implementation approval. |
| Endpoint validation | HTTPS syntax only | Works with more providers and is simple | Allows an administrator typo or unrelated HTTPS host to receive the ID token; **rejected** for this issue. |
| Endpoint validation | Match effective issuer authority | Deterministic, offline, and constrains token destination | May reject a standards-compliant provider whose discovered endpoint uses another authority; **chosen** as the stricter security/operational policy. |
| Endpoint validation | Fetch discovery and dynamically allow the discovered host | More flexible provider compatibility | Adds network dependency, SSRF/availability concerns, and non-deterministic reconciliation; **rejected**. |
| Ownership | Let operator and upstream Helm co-own the same Deployment | Appears to preserve both installation paths | Competing writers, ownership conflicts, and nondeterministic rollback/upgrade behavior; **rejected**. |
| Ownership | Use the upstream Helm chart as the production owner | Existing Helm path already renders a logout setting | It is a separate deprecated deployment path and is outside this operator issue; any remaining production support needs a separate chart issue. |
| Ownership | Make the operator the sole owner of the full deployment | Deterministic CR-driven lifecycle, owner references, drift repair, and upgrade behavior | **Chosen for #483.** Foreign Helm-owned objects are rejected rather than adopted. |

Each unresolved alternative is time-boxed to a spike of no more than two hours;
no implementation begins while a spike lacks a clear YES/NO outcome.

## 4. Bounded spikes and decisions

| Spike | Question | Evidence | Decision |
|---|---|---|---|
| S1 — API compatibility | Can the requested v1alpha1 conversion be extended? | No `api/v1alpha1` tree; clean-break docs/tests explicitly prohibit conversion; v1alpha2-only scope is approved. | **No.** Discard v1alpha1 conversion and implement only v1alpha2. |
| S2 — token location | Does the Console SPA need the token? | OAuth2 Proxy replaces `{id_token}` and calls the backend logout URL itself. | **No.** Keep token handling in oauth2-proxy only. |
| S3 — transport | Is HTTP acceptable for development? | The token is inserted into a URL used by `http.Get`; HTTP exposes it on the pod-to-provider path. | **No.** HTTPS-only, with no insecure escape hatch. |
| S4 — endpoint trust | How can static CR validation prevent an arbitrary endpoint? | The field is an IdP-discovered endpoint and the CR already resolves one effective issuer; OIDC RP-Initiated Logout requires HTTPS but does not require the logout endpoint to share the issuer authority. | **Chosen:** require absolute HTTPS and exact effective-provider authority as the stricter security/operational policy; do not perform discovery from admission/reconcile validation. Document that same-authority matching is an intentional product constraint, not an OIDC requirement. |
| S5 — redirect ownership | Should the CR expose a post-logout redirect field? | OAuth2 Proxy already validates its sign-out return target; the issue explicitly forbids a user-controlled redirect. | **No new redirect field.** Reject logout URL fragments and redirect-target query parameters if present; retain server-owned OAuth2 Proxy return behavior. |
| S6 — rollout/upgrade | What restores a lost temporary argument? | `ensureDeployments` rebuilds the Deployment when the CR generation changes or the Deployment is regenerated. | Add envtest that updates the CR and deletes/recreates the Deployment, proving the desired value is restored. |
| S7 — live provider | Can local tests prove Keycloak actually terminates the IdP session? | Unit/envtest can prove configuration and wiring, not provider browser behavior. | **QE-owned external gate:** QE runs the live Keycloak/browser qualification after a fresh operator deployment. |
| S8 — backend TLS trust | Does oauth2-proxy's backend logout request use `--provider-ca-file`/the mounted trust bundle? | Pinned v7.9.0 and upstream Helm's v7.15.3 source call Go `http.Get` in `backendLogout`; `pkg/requests/http.go` shows the configured provider client is separate. Existing operator `kaInitContainers`/`afInitContainers` and their Make-tested unit coverage solve this class of gap with a combined system-plus-provider CA bundle and `SSL_CERT_FILE`. | **Resolved:** reuse the existing `init-ubi-minimal` combined-CA pattern for Console oauth2-proxy. The backend request gets system roots plus the mounted provider/inter-service trust bundle; certificate verification remains enabled and self-signed providers are supported. |
| S9 — resource ownership | Who owns the full operator deployment from scratch? | A fresh CR causes the operator to create the Console resources and stamp Kubernaut ownership; upstream Helm is a separate deprecated alternative. | **Resolved:** #483 covers the fresh operator-owned full deployment only. No Helm/operator cutover is part of acceptance. |
| S10 — browser evidence | Can the existing browser journey qualify an operator-built deployment? | `kubernaut-console/e2e/live/logout.spec.ts` and issue #139 already cover live Keycloak logout and local-state clearing; that journey previously passed, while this checkout has no current live cluster. | **QE-owned external gate:** QE reruns the existing Console E2E against a fresh operator deployment; do not add a second browser harness here. |

## 5. Proposed implementation

### API and generated artifacts

Update `api/v1alpha2/kubernaut_types.go`:

- Add optional `ProviderLogoutURL string` with JSON name
  `providerLogoutURL`.
- Document that it is the provider-discovered OIDC `end_session_endpoint`.
- Document the literal `{id_token}` placeholder and that it is not a token
  value supplied by the user or Console.
- Add a CRD HTTPS syntax marker for early schema rejection. Dynamic authority
  matching remains controller validation because OpenAPI cannot compare two CR
  fields.

Regenerate and inspect:

- `api/v1alpha2/zz_generated.deepcopy.go`
- `config/crd/bases/kubernaut.ai_kubernauts.yaml`
- `bundle/manifests/` and `dist/install.yaml` via `make bundle`

The generated deepcopy implementation may have no textual change because the
field is a string, but generation must still be run and the result inspected.

### Validation

Add a focused, pure `validateConsoleProviderLogoutURL` helper to
`internal/resources/validation.go`. Call the same helper from both
`ValidateKubernaut` and `ConsoleDeployment`; validation only in the controller
phase is insufficient because direct builder use must also fail closed rather
than render an unsafe argument.

For a non-empty value:

- reject leading/trailing whitespace;
- require an absolute URL with non-empty host and `https` scheme;
- reject userinfo and fragments;
- permit only the exact literal `{id_token}` placeholder, where present;
- allow only the provider logout query parameters `id_token_hint`,
  `logout_hint`, `client_id`, and `ui_locales`; reject all other query
  parameters so return navigation remains OAuth2 Proxy-owned;
- when Console is enabled, require the URL authority to match the effective
  OIDC issuer authority, including the effective port;
- report the field path and reason, never the substituted token or raw URL value.

Validation failures must retain the existing structured reconciliation audit
path: log the operation with CR generation/resourceVersion, set the existing
validation condition, and emit the warning event. Do not add the URL value or
any substituted token to logs, events, status, annotations, or ConfigMaps.

An unset field remains valid and renders no argument. A configured value is
validated as HTTPS even when Console is disabled; authority comparison is only
needed when the Console has an effective issuer. No HTTP exception or
`allowInsecure...` reuse is permitted. Add a direct-builder negative test to
prove malformed/HTTP input cannot bypass the controller validation phase.

### Resource rendering and wiring

In `internal/resources/console.go`, append the argument only when the validated
field is non-empty:

```text
--backend-logout-url=<configured provider URL containing literal {id_token}>
```

Do not change `ConsoleNginxConfigMap`, Console image code, browser URLs, or
Console environment variables. The existing production path remains:

```text
phaseDeploy
  -> deployWorkloads
  -> enabledDeploymentBuilders
  -> ConsoleDeployment
  -> ensureDeployments
```

This satisfies the wiring checkpoint without adding a new controller component,
RBAC permission, status condition, or reconciliation phase.

Because the pinned oauth2-proxy image calls `http.Get` for backend logout, the
Console pod must use the existing operator pattern from
`kaInitContainers`: an `init-ubi-minimal` container concatenates the base image
system bundle with the mounted `tls-ca` bundle into an `emptyDir`, and the
oauth2-proxy container receives `SSL_CERT_FILE` pointing to that combined file.
`--provider-ca-file` remains configured for OAuth2 Proxy's provider clients, but
the combined bundle is what protects the backend logout request. Add resource
tests for the init container, read-only trust mount, combined bundle output path,
and `SSL_CERT_FILE`; QE verifies the real self-signed/public certificate cases.

### Deployment ownership boundary

For #483, the operator creates the complete deployment from scratch. It owns
the Console Deployment, Service, nginx ConfigMap, and configured exposure
resources through the newly created Kubernaut CR. The upstream Helm chart is a
separate deprecated alternative and is not rendered in the acceptance path.

The operator must continue to reject a foreign Helm-owned object with an
ownership conflict rather than adopting it. Any future Helm-to-operator
migration is a separate issue and is not part of #483 acceptance.

### Documentation and samples

Update:

- API comments and generated CRD description;
- `docs/installation/02-configure-services.md` Console section;
- `config/samples/v1alpha2_kubernaut.yaml`;
- `config/samples/v1alpha2_kubernaut_minimal.yaml` only if the optional Console
  example is present there;
- relevant v1.6 upgrade/changelog notes.

Document an HTTPS provider-discovered endpoint, for example:

```yaml
spec:
  console:
    auth:
      secretName: kubernaut-console-oidc
      providerLogoutURL: https://keycloak.example.com/realms/demo/protocol/openid-connect/logout?id_token_hint={id_token}
```

State explicitly:

- do not URL-encode or replace `{id_token}` in the CR;
- do not configure `http://`, even for localhost or development;
- use HTTPS; the operator combines system roots with the mounted provider trust
  bundle so public and self-signed provider certificates are both verified;
- do not add `post_logout_redirect_uri`, `rd`, or SPA token logic;
- changing the IdP realm requires coordinated client, issuer, CA, and logout
  endpoint changes.

## 6. TDD and test plan

Tests must be Ginkgo/Gomega business tests and must prove behavior, not merely
exercise lines. Add only the cases below; do not create table entries solely to
increase coverage.

### RED

1. Add a v1alpha2 CRD contract test proving `providerLogoutURL` is optional,
   has HTTPS syntax validation, and is present in the generated schema.
2. Add resource tests for omitted and configured logout arguments.
3. Add validation tests for the security boundary and provider-authority rule.
4. Add controller envtest for create, CR update/re-render, and Deployment delete/recreate.
5. Add a lifecycle negative test proving an unsafe URL reaches
   `SpecValidationFailed` and no Console Deployment is created or updated.
6. Add ownership-boundary coverage for an operator-owned Deployment and a
   foreign/Helm-owned Deployment; the latter must be preserved and reported,
   not adopted.

### GREEN

1. Add the v1alpha2 field and markers.
2. Implement the focused validation helper and call it from
   `ValidateKubernaut`.
3. Render the optional argument in the existing Console builder.
4. Regenerate artifacts.
5. Run the integration path through the existing reconciler; do not add a
   stand-alone builder or deferred wiring.
6. Reference this issue/plan in implementation comments where the ownership or
   token-handling boundary is non-obvious.

### REFACTOR

- centralize URL parsing/authority comparison rather than duplicating OIDC URL
  logic;
- keep errors lowercase, contextual, and free of URL/token values;
- preserve existing issuer precedence and Console-disabled behavior;
- preserve the existing resource-ownership contract and do not adopt a
  Helm-owned Deployment;
- assert no logout URL or token is added to the SPA/nginx ConfigMap;
- run the complete build, lint, unit, integration, and generated-artifact checks.

### Pyramid invariant

- **UT** proves URL policy, direct-builder fail-closed behavior, and exact
  oauth2-proxy argument rendering.
- **IT** proves the real CR → validation → reconciliation → Deployment path,
  ownership behavior, drift repair, and status/event observability.
- **E2E/live qualification** proves the actual HTTPS provider logout, browser
  session termination, fresh-login requirement, and transcript behavior. This
  evidence is external to the current operator test harness and must be rerun
  against the fresh operator deployment before being accepted for #483.

## 7. Test cases and business evidence

| Test ID | Tier / location | Business behavior | Control objective evidence |
|---|---|---|---|
| `UT-483-API-001` | v1alpha2 clean-break suite | Optional field is present in the v1alpha2 schema; no v1alpha1 conversion surface is reintroduced. | FedRAMP AC-6; SOC 2 CC6.1; ASVS V5/V8 |
| `UT-CD-483-001` | `internal/resources/console_test.go` | Configured HTTPS endpoint renders exactly once in oauth2-proxy args with literal `{id_token}`, and the combined CA init/env wiring is present. | FedRAMP AC-12/SC-8; SOC 2 CC6.1/CC6.3; ASVS V3.3/V7.2.1/V12.3.2 |
| `UT-483-VALIDATE-001..006` | `internal/resources/validation_test.go` | Valid same-authority HTTPS and malformed, HTTP, empty-host, userinfo, fragment, placeholder, query, disabled-Console, and HTTP-issuer cases fail/pass as specified. | FedRAMP AC-6/SC-8; SOC 2 CC6.1; ASVS V1.2.2/V1.3.6/V5/V8 |
| `UT-CD-483-002/003` | `internal/resources/console_test.go` | Direct `ConsoleDeployment` construction rejects unsafe URL cases and fails closed when the CA init image is unavailable. | FedRAMP AC-6/SC-8; SOC 2 CC6.1; ASVS V5/V8 |
| `UT-CD-483-001` | `internal/resources/console_test.go` | Configured logout adds the combined-CA init container, read-only consumer trust mount, and `SSL_CERT_FILE` without disabling TLS verification. | FedRAMP AC-6/SC-8; SOC 2 CC6.1; ASVS V12.3.2/V12.3.4/V8.3.1 |
| `IT-CL-483-001` | `internal/controller/kubernaut_lifecycle_test.go` | A fresh CR creates the Console Deployment, a CR update changes the logout argument, and deletion/recreation restores it with combined CA wiring. | FedRAMP AC-2/AC-12; SOC 2 CC6.1/CC6.3; ASVS V3.3/V7.2.1/V8 |
| `IT-483-FAIL-CLOSED-001` | controller envtest | Unsafe URL sets `SpecValidationFailed`, emits the warning/event and structured validation log with generation/resourceVersion, and prevents insecure workload configuration. | FedRAMP AC-6; SOC 2 CC6.1; ASVS V5/V8 |
| `IT-483-OWNERSHIP-001` | controller envtest | A foreign/Helm-owned Console Deployment is not adopted or overwritten; reconciliation reports the ownership conflict. | FedRAMP AC-6; SOC 2 CC6.1; ASVS V8 |
| `E2E-483-TLS-001` | **QE external qualification** | The pinned oauth2-proxy backend logout request completes with certificate verification against the approved provider trust source; no insecure TLS bypass is used. | FedRAMP AC-6/AC-12/SC-8; SOC 2 CC6.1; ASVS V5/V8 |
| `E2E-483-BROWSER-001` | **QE external qualification using `kubernaut-console/e2e/live/logout.spec.ts`** | Logout clears the Console session, terminates the Keycloak session, requires fresh login, leaves no transcript/local state, and does not expose or construct the ID-token logout request in the SPA. | FedRAMP AC-2/AC-6/AC-12; SOC 2 CC6.1/CC6.3; ASVS V3.3/V5/V7.2.1/V8 |

The touched `internal/resources` logic must retain the repository's 96% unit
coverage target (and never fall below the requested 90% floor). Coverage is a
verification signal; the business cases above, especially the negative
transport and reconciliation cases, are the reason for the tests.

The control mappings are evidence traceability, not a standalone compliance
claim. Local UT/IT evidence covers the operator configuration and ownership
slice; AC-2/AC-12, CC6.3, browser session behavior, provider TLS trust, and
transcript clearing are QE-owned external evidence.

### QE handoff package

QE should receive:

- the operator image digest and generated v1alpha2 CR manifest with an
  HTTPS-only `providerLogoutURL` (redact credentials; the literal placeholder
  is not a secret);
- the provider/Keycloak certificate-chain and trust-source details;
- the expected Console Deployment argument and operator ownership metadata;
- the existing `kubernaut-console/e2e/live/logout.spec.ts` entry point;
- the control-evidence checklist for AC-2/AC-6/AC-12, CC6.1/CC6.3, and
  ASVS V3.3/V5/V7.2.1/V8.

QE should return the cluster/operator/Console image provenance, test output,
TLS verification result, fresh-login result, transcript/local-state result, and
redacted logs/network evidence showing that the SPA did not receive the ID token.
No local deployment is required for this plan because the current session lacks
the necessary resources and time.

## 8. Wiring manifest and Checkpoint W

| Component | Production entry point | Wiring location | Proof |
|---|---|---|---|
| `providerLogoutURL` validation | `KubernautReconciler.phaseValidate` and direct resource construction | Shared helper called by `ValidateKubernaut` and `ConsoleDeployment` | `UT-483-VALIDATE-*`, `UT-CD-483-002`, `IT-483-FAIL-CLOSED-001` |
| Optional oauth2-proxy logout arg | `KubernautReconciler.phaseDeploy` | `deployWorkloads` → `ensureDeployments` → `ConsoleDeployment` | `UT-CD-483-001`, `IT-CL-483-001` |
| Backend logout TLS trust | `ConsoleDeployment` | Existing `kaInitContainers` pattern → combined CA `emptyDir` → oauth2-proxy `SSL_CERT_FILE` and read-only mount | `UT-CD-483-001`, QE `E2E-483-TLS-001` |
| Deployment ownership boundary | `KubernautReconciler.phaseDeploy` | `ensureNamespaced` → `ensureResource` → ownership check | `IT-483-OWNERSHIP-001`, existing ownership contract tests |
| v1alpha2 schema/artifact | Kubernetes API admission | `api/v1alpha2/kubernaut_types.go` → generated CRD | `UT-483-API-001`, `make manifests generate` |

Checkpoint W passes only if:

- the existing reconciler calls the modified builder;
- the envtest observes the generated argument through the API server;
- invalid HTTP/mismatched-provider inputs stop before workload rendering;
- foreign/Helm-owned Deployments are preserved rather than adopted;
- no v1alpha1 conversion or unowned compatibility path is added;
- no token or logout URL is wired into the Console SPA.

## 9. Verification and release gates

Required local checks after implementation:

```text
go build ./...
golangci-lint run
make test-unit
make test-integration
make test
make manifests generate
make bundle
go test ./... -run=^$ -timeout=30s
make vet
git diff --check
```

Also inspect CRD, bundle, and install-manifest diffs for only the intended
v1alpha2 field. Verify `config/crd/bases/`, `bundle/manifests/`, and
`dist/install.yaml` are synchronized; run the project's no-unexpected-diff
checks against `config/` after generation and do not silently overwrite
unrelated work.
The controller integration suite requires envtest binaries; the preflight
environment currently lacks them and must be provisioned before the integration
gate can pass.

The live Keycloak/browser qualification remains a release acceptance gate. Unit
and envtest evidence proves secure configuration, fail-closed validation, and
reconciliation ownership; it does not prove an external IdP actually terminates
its browser session.

## 10. GA readiness audit status

The plan is not implementation-ready until the following gates are explicit:

| Dimension | Preflight status | Required completion evidence |
|---|---|---|
| Build | Pending implementation | `go build ./...` |
| Lint | Pending implementation | `golangci-lint run` with no new warnings |
| Unit | Baseline API/resources suites pass; issue tests not written | Ginkgo UTs pass; `internal/resources` remains at least 96% covered |
| Integration | Baseline now passes with envtest Kubernetes 1.37 assets | Controller suite passes with at least 78% controller coverage |
| Wiring | Caller path identified; runtime proof pending | Checkpoint W and real reconciliation assertion |
| BDD framework | Existing suites use Ginkgo entrypoints | No business `testing.T`, `XIt`, `PIt`, `Skip`, or `time.Sleep` in new tests |
| Go anti-patterns | Not yet assessed on implementation | `make vet`, lint, and checklist review: errors, params, nesting, `any`, allocations, context, goroutines |
| CRD/artifacts | No field exists yet | `make manifests generate make bundle`; inspect CRD/bundle/install diffs |
| Regression | API/resources baseline passed; controller baseline blocked | Full `make test` and affected suites pass |
| Fail-open safety | Design requires fail-closed validation/ownership | Negative UT/IT plus observable status, event, and structured log evidence |
| Operator-specific | Existing phase/ownership seams identified | Phase transition, ownership conflict, rollout, and upgrade tests pass |

### Approval gates before implementation

1. Implement and test the S8 combined system-plus-provider CA bundle using the
   existing operator init-container pattern; no OAuth2 Proxy image change or
   insecure TLS option is allowed.
2. Verify S9 with a fresh operator installation: the operator creates and owns
   the complete Console resource set without preexisting Helm resources.
3. Assign the existing Console live logout E2E and backend TLS verification to
   QE; do not add a second browser framework or deploy locally for this issue.
4. Keep any remaining upstream Helm security work separate; #483 must not claim
   to fix or qualify the deprecated Helm deployment path.

## 11. Risks and approval gates

| Risk | Mitigation / gate |
|---|---|
| API compatibility drift | Keep the implementation v1alpha2-only. Do not reintroduce conversion or dual-version behavior opportunistically. |
| An HTTP value is promoted from development to production | Reject HTTP universally; do not add an environment flag or localhost exception. |
| A configured endpoint sends tokens to an unintended provider | Require HTTPS and exact effective-provider authority; fail closed before deployment. |
| Provider discovery returns an endpoint on another authority | Treat as unsupported until a separately approved provider allowlist contract exists; do not weaken validation silently. |
| Backend logout uses a trust path different from OIDC discovery | Build the existing combined system-plus-provider CA bundle and set `SSL_CERT_FILE`; never disable certificate verification. |
| A preexisting Helm object is present unexpectedly | Refuse foreign ownership and report the conflict; the accepted #483 path starts from a fresh operator deployment. |
| Deprecated upstream Helm path permits HTTP logout URLs | Track upstream chart hardening separately; do not claim #483 fixes or qualifies that path. |
| OAuth2 Proxy or operator reconciliation removes the argument | Existing reconciliation path plus drift-repair envtest and operator rollout/upgrade verification. |
| Logout silently returns to the SPA authenticated | Live Keycloak browser test must reload after logout and require a fresh login; transcript must be absent. |
| Token leaks into browser or operator diagnostics | No SPA changes, no token interpolation in operator code, no raw URL in validation errors/status/logs, and negative assertions in tests. |

## 12. Confidence

**Static v1alpha2 renderer/validation scope: 95% confidence.** The API,
renderer, validator, reconciler, generated-artifact, and existing test seams are
identified; the security policy is explicit and supported by OAuth2 Proxy
behavior. Remaining external risk is the live Keycloak/browser qualification and
the exact provider endpoint contract.

**Operator implementation readiness: 92% confidence.** Source-level spikes
resolved the current-image TLS behavior, the existing self-signed trust-bundle
pattern, repository ownership boundary, and test-harness limits. The accepted
path starts from a fresh operator deployment and must add focused init-container
and `SSL_CERT_FILE` coverage.

**Production acceptance readiness: 90% confidence.** The fresh operator
deployment path is defined, the existing Console repository has previously
passing live Keycloak logout coverage, and the operator already has a proven
combined-CA pattern for self-signed/public endpoints. QE owns the remaining
rerun against a fresh operator deployment and real certificate-trust
verification. Deprecated upstream Helm hardening is explicitly outside #483.
Do not claim the FedRAMP/SOC 2/ASVS runtime outcomes until QE returns the
external evidence package.

# Issue #479 — Shared OIDC issuer for Console and API Frontend

**Issue:** [kubernaut-operator#479](https://github.com/jordigilh/kubernaut-operator/issues/479)
**Related contract:** [kubernaut#2385](https://github.com/jordigilh/kubernaut/issues/2385)
**Baseline:** `55f71ebff7ccef52b935293ff2c0866d75c96f2f` on the current branch
**Status:** Implemented and qualified in this branch.

**Completion evidence:** `go build ./...`, unit/resource tests, controller
integration tests, lint, and generated-manifest checks. The default Kind lane
remains the fast operator contract gate. Live OIDC-provider qualification and
application Helm ownership remain separate qualification/packaging concerns.

**Methodology:** RED → GREEN → REFACTOR, with controller wiring verified in GREEN.

## v1.6 scope correction: OAuth2/OIDC is first-class

For v1.6, OAuth2/OIDC user authentication remains supported. Kagenti/rossctl is
not a first-class production dependency: it must not supply the OIDC issuer or
be required for a default install. There is no independent SPIFFE/SPIRE
consumer requirement in this scope; SPIFFE/SPIRE configuration is deferred to a
separate issue until an upstream consumer and acceptance contract exist.

The baseline contained Kagenti-specific issuer auto-detection and sidecar/port
plumbing. This implementation removes that coupling from the v1.6 operator path;
issue #479 uses the explicit/shared issuer contract instead.

### v1.6 removal/retention boundary

Remove Kagenti-specific reconciliation from the v1.6 operator path, including
issuer auto-detection, `KagentiSidecarMode` detection/port switching,
authbridge ConfigMap patching, Kagenti namespace labeling, and AgentRuntime
creation. Do not add or retain Kagenti-named configuration in the v1alpha2 CRD.

Do not retain the inherited SPIFFE/SPIRE CRD field or reconciliation path in
this OIDC change. A future SPIFFE/SPIRE issue must first define an upstream
consumer, provider compatibility contract, documentation, and unit/integration
or live qualification requirements.

## 1. Objective and acceptance contract

Expose one deterministic OIDC issuer contract for API Frontend and Console. The
production default is `https://login.kubernaut.ai/realms/kubernaut`. An explicit,
complete `issuerURL` may select any external, demo, or test realm, including
`https://login.kubernaut.ai/realms/kubernaut-demo`.

The implementation is complete only when:

1. An API Frontend-enabled CR with no issuer uses the production URL.
2. An explicit full URL is rendered unchanged by API Frontend and Console.
3. `kubernaut-demo` is never selected implicitly or synthesized from a short name.
4. Console oauth2-proxy and API Frontend token validation use the same effective
   issuer.
5. Existing multi-provider behavior remains valid and is tested.
6. No Kagenti/rossctl resource, ConfigMap, or sidecar is required for the default
   OAuth2 installation path.
7. v1alpha1 is explicitly EOL at v1alpha2 release; no concurrent v1alpha1 support
   or conversion path is introduced.
8. Installation, upgrade, demo/test, security, and operator-chart guidance is clear.

## 2. Recommended design decisions

### 2.1 Reuse the existing CR field

Use `spec.apiFrontend.auth.issuerURL` as the canonical single-provider surface.
Do not add `spec.sharedOIDC`, a root-level issuer, or a Console-specific field.
The existing v1alpha2 API already has the correct field, and a second source would
create precedence and migration ambiguity.

### 2.2 Effective-issuer precedence

For single-provider authentication, resolve in this order:

1. Non-empty `spec.apiFrontend.auth.issuerURL` — explicit and authoritative.
2. `https://login.kubernaut.ai/realms/kubernaut` — runtime production fallback.

The resolver must not mutate the CR, normalize away meaningful URL text, build a
realm from a short name, or introduce a demo fallback.

When `jwtProviders` is non-empty, that existing multi-provider contract wins. API
Frontend validates and renders all providers; Console uses the first validated
provider's `issuerURL`. The selected Console value must be asserted to exist in the
rendered API Frontend provider list. Top-level single-provider fields are ignored in
this mode, as the existing API comments state; do not introduce a new conflict error.

### 2.3 Defaulting decision after removing Kagenti from the core path

Removing Kagenti auto-detection and confirming that no v1alpha2 CRs already exist
removes the main compatibility argument for runtime-only defaulting. The preferred
contract now is:

- add `+kubebuilder:default="https://login.kubernaut.ai/realms/kubernaut"` to the
  existing single-provider field;
- retain a defensive runtime fallback because Kubernetes cannot default a child
  field when its optional parent object is absent, and unit tests call builders
  directly without API-server defaulting;
- keep `jwtProviders` authoritative when configured, even if the API server
  materializes the top-level default that multi-provider mode ignores.

This is a defaulting enhancement, not a new API field or migration mechanism. It
requires CRD regeneration and tests for both API-server-defaulted objects and
zero-value/direct-builder paths. No v1alpha1 object needs to retain or receive this
default after the EOL boundary.

## 3. Preflight evidence and resolved gap

Sections 3–4 retain the pre-implementation baseline and decision record. Paths
and “current gap” descriptions in those sections refer to the clean baseline
`55f71eb`, not to the completed source in this branch. Sections 5 onward record
the implemented design and final verification disposition.

### 3.1 Schema and API

`api/v1alpha2/kubernaut_types.go` already exposes `APIFrontendAuthSpec.IssuerURL`,
`JWKSURL`, `OIDCCAFile`, `AllowInsecureIssuers`, and `JWTProviders`. Its
`KubernautSpec.ConsoleIssuerURL()` currently selects the first provider issuer or
the top-level issuer. The generated CRD at
`config/crd/bases/kubernaut.ai_kubernauts.yaml` has no issuer default or URL format
constraint. The controller validator currently only requires a top-level issuer in
some modes, while `JWTProviderSpec.IssuerURL` is only schema-checked for non-empty
content. The “full issuerURL” requirement therefore needs an explicit absolute-URL
syntax check; it must not be left to the downstream runtime.

The current API is v1alpha2-only, served/storage, and has no conversion webhook.
v1alpha1 is EOL at the v1alpha2 release boundary; this plan adds no conversion
webhook, v1alpha1 path, or dual-version compatibility behavior. API comments must
remove Kagenti as an issuer fallback and describe the explicit OIDC contract.

### 3.2 Renderer/controller gap

| Area | Current evidence | Required change |
|---|---|---|
| API Frontend | `internal/resources/configmaps.go:2760` starts with the CR issuer and merges `KagentiOIDCDefaults` | Remove Kagenti as an issuer source and add the production fallback through the shared resolver |
| Console | `internal/resources/console.go:41` calls `ConsoleIssuerURL()` and writes oauth2-proxy args | Pass the same explicit/default OAuth2 context |
| Optional gateway registration | `internal/resources/mcpgateway.go:89` reads raw top-level issuer and is tied to the optional Kagenti gateway CRD | Keep outside the v1.6 core contract; if retained, make it an explicit optional integration using the shared OAuth2 value |
| Validation | `internal/resources/validation.go:307` requires a top-level issuer unless Kagenti or providers apply, but does not validate top-level issuer syntax | Accept the production default, validate explicit/provider issuers as absolute URLs, and remove the Kagenti exception |
| Controller | `phaseDeploy` resolves Kagenti and passes a Kagenti sidecar mode through AF deployment paths | Decouple OIDC resolution from obsolete provider and sidecar detection |

The concrete v1.6 risk is that the existing AF path can silently depend on
Kagenti-specific detection and sidecar port behavior even though OAuth2 should
work without that integration.

### 3.3 Existing test seams

- `internal/resources/configmaps_test.go` covers AF auth, Kagenti merge, JWKS
  derivation, multi-provider output, and audience injection; Kagenti-specific cases
  must be removed from the core v1.6 contract or moved to optional-integration tests.
- `internal/resources/console_test.go` covers missing issuer, Deployment shape, and
  oauth2-proxy TLS hardening.
- `internal/resources/validation_test.go` covers single/multi-provider auth and
  sidecar exceptions.
- `internal/controller/kubernaut_lifecycle_test.go` covers issuer enforcement,
  Kagenti failures, and OIDC auto-detection; v1.6 tests must instead prove that
  missing Kagenti/rossctl resources do not block OAuth2.
- `internal/controller/kubernaut_controller_test.go` covers ConfigMap reconciliation.

### 3.4 Packaging and upstream boundaries

Upstream `kubernaut#2391` was reviewed and is unrelated workflow work; it is not
implementation evidence for this plan. The contract evidence is #479/#2385 and the
current operator code/tests.

There is no application Helm chart in this repository at the baseline. #489 is an
operator-only bootstrap chart and must not duplicate application values, render a
Kubernaut CR, or own OIDC configuration. OIDC remains in the user-applied CR.
Because #479 explicitly names Helm values/templates, the Helm portion is a
cross-repository or separately owned packaging dependency: it must be assigned to
the owning application chart and verified for parity with this CR contract. It
cannot be satisfied by adding forbidden application values to #489.

## 4. Bounded spikes and decisions

| Spike | Question | Evidence | Decision |
|---|---|---|---|
| S1 — URL contract | What is the default and how is demo/test selected? | #479/#2385 requirements and existing samples | **YES:** exact full production URL; demo/test only by explicit full URL |
| S2 — API surface | Is a new shared field needed? | Existing AF auth field and Console helper | **NO:** centralize resolution over the existing field |
| S3 — default location | Is a CRD default still harmful after Kagenti removal and the clean v1alpha2 cutover? | No existing v1alpha2 CRs need migration; only omitted parent objects/direct builders need runtime defense | **CRD default is preferred, with runtime fallback for zero-value paths** |
| S4 — provider mismatch | Should top-level issuer conflict with `jwtProviders`? | Existing multi-provider precedence and first-provider Console helper | **NO new conflict:** providers remain authoritative |
| S5 — #478 | Does OTLP TLS enforcement affect this design? | #478 changes telemetry transport validation | **No code dependency:** coordinate release notes and run both suites |
| S6 — URL validation | Does “full issuerURL” currently reject relative/malformed values? | Top-level issuer has no syntax check; provider issuer has only `MinLength=1` | **YES:** add absolute scheme+host validation; approve HTTP/insecure policy explicitly |
| S7 — SPIFFE scope | Is there a current SPIFFE/SPIRE consumer requirement? | No independent consumer or acceptance contract exists after Kagenti removal | **NO:** defer SPIFFE/SPIRE configuration to a separately specified issue |

## 5. Implemented design

### 5.1 Shared resolver/context

The existing v1alpha2 API owns the resolver through
`KubernautSpec.EffectiveIssuerURL()` and `EffectiveIssuerSource()`. The logical
behavior is:

- non-empty `jwtProviders`: return provider zero's issuer and `multi-provider`;
- otherwise non-empty CR issuer: return it and `explicit`;
- otherwise return the exact production URL and `production-default`.

`ConsoleIssuerURL()` remains as a compatibility-named wrapper over the shared
resolver. API Frontend config uses the same resolver for the single-provider
path, while preserving the complete `jwtProviders` list for multi-provider
configuration. No resolver depends on a Kagenti/rossctl ConfigMap, CRD, webhook,
or sidecar. Existing single-provider Keycloak JWKS derivation remains limited to
that path; it is not applied to heterogeneous provider entries.

### 5.2 Controller propagation and wiring

1. `phaseDeploy` logs the effective issuer and non-sensitive source without
   consulting Kagenti/rossctl resources.
2. `APIFrontendConfigMap` and `ConsoleDeployment` call the shared API resolver;
   no separate fallback calculation or context-propagation parameter is needed.
3. `enabledDeploymentBuilders` wires the unchanged two-argument Console and API
   Frontend builders through the normal workload path.
4. Optional MCP gateway registration is outside the v1.6 core path and is not
   reconciled by this implementation.
5. No consumer performs a separate fallback calculation.

Log the selected source and issuer at the existing structured reconciliation
boundary, including generation and resourceVersion. Issuer URLs are not secrets,
but tokens, client secrets, and Secret data must not be logged or stored in status.

### 5.3 Validation

Change AF validation so an enabled single-provider AF with an omitted issuer is
accepted because the resolver supplies the production default. Preserve:

- AF-disabled short-circuit behavior;
- per-provider issuer, audience, name, JWKS, and length validation;
- absolute scheme-and-host validation for non-empty top-level and provider issuer
  URLs; the CRD also carries an HTTP(S) syntax pattern, with the controller
  retaining the stronger host check;
- no dependency on Kagenti/rossctl CRDs, ConfigMaps, or sidecars;
- existing `allowInsecureIssuers`, CA, and TLS safeguards.

The implemented contract keeps production values HTTPS-only by default while
retaining `allowInsecureIssuers` as the explicit development/test escape hatch
for HTTP issuer and JWKS URLs. Unsupported schemes such as `ftp` remain invalid
even when the escape hatch is enabled.

Do not make network requests during CR validation. Syntax/configuration validation
and runtime issuer/JWKS reachability remain separate concerns.

### 5.4 Documentation and samples

Update API comments, `config/samples/v1alpha2_kubernaut_minimal.yaml`, the full
sample's OAuth2 comments, and:

- `docs/installation/00-quickstart.md`;
- `docs/installation/02-configure-services.md`;
- `docs/installation/03-deploy.md`;
- `docs/upgrade-v1alpha1-to-v1alpha2.md`.

Document the production default, explicit external/demo/test full URL, Console
client/audience/redirect requirements, the disabled-AF path, and that
Kagenti/rossctl is not a v1.6 production prerequisite.
Explain that changing realms invalidates old tokens/cookies and requires matching
IdP clients, audiences, redirect URIs, JWKS reachability, and CA trust.

### 5.5 Helm acceptance disposition

The current checkout contains no application `Chart.yaml`, `values.yaml`, or Helm
templates. The implemented disposition is:

1. This operator change implements and documents the CRD/operator contract but
   does not claim application-chart template work.
2. The owning application chart remains responsible for mirroring the exact
   production default, full `issuerURL` override, and explicit `kubernaut-demo`
   demo/test override, with chart-template tests.
3. #489 remains bootstrap-only: it must not render an application Kubernaut CR or
   own application OIDC values.

When that chart is available, its documented installation path should add a
cross-repository parity check proving that the rendered CR and reconciled AF and
Console resources select the same issuer.

## 6. TDD execution record

The following RED/GREEN/REFACTOR sequence was used for the implementation; the
listed tests and wiring are present in this branch.

### RED — failing tests first

Extend existing Ginkgo/Gomega suites with:

1. Resolver cases for omitted/default, explicit external, explicit demo,
   first-provider selection, and no implicit realm building or external dependency.
2. AF renderer cases for default, exact override, and unchanged full
   multi-provider output without single-provider JWKS derivation.
3. Console renderer cases for default, explicit, first-provider selection,
   equality with the AF provider output, and retained TLS/CA args.
4. Validation cases for accepted default, rejected empty provider issuer, malformed
   or relative top-level/provider issuers, existing security errors, AF-disabled
   behavior, and absent Kagenti/rossctl resources not blocking the core path.
5. Envtest cases proving equal AF/Console issuers for default and explicit
   demo/external configurations, plus OAuth2 operation without Kagenti.
6. A cross-repository Helm contract check, when the owning chart is available,
   covering the production default, exact override, and explicit demo overlay; if it
   is not available, record the dependency rather than duplicating chart templates.

White-box resource tests must reuse production YAML structs for full-shape assertions;
do not create hand-copied mirror structs.

### GREEN — minimal implementation and wiring

The resolver was implemented in the existing v1alpha2 API and consumed by the
existing renderers. Resource unit tests and controller envtest tests pass. No new
reconciliation phase, CRD field, webhook, RBAC rule, or status condition was added
for issuer selection.

### REFACTOR

Duplicate fallback logic was removed, provider precedence is documented, errors
remain contextual and lower-case, structured source logging includes generation
and resourceVersion, and existing JWKS/CA/TLS/insecure-development behavior is
retained.

## 7. Wiring manifest and Checkpoint W

| Component | Production entry point | Wiring location | Proof |
|---|---|---|---|
| Effective issuer resolver | AF and Console builders | `api/v1alpha2.KubernautSpec.EffectiveIssuerURL()` and `ConsoleIssuerURL()` | API/resource renderer UTs |
| AF shared issuer | `phaseDeploy` | `deployConfigMaps` → `appendOptionalComponentConfigMaps` → `APIFrontendConfigMap` → `afAuthConfig` | Envtest default/override |
| Console shared issuer | `phaseDeploy` | `deployWorkloads` → `enabledDeploymentBuilders` → `ConsoleDeployment` → `ConsoleIssuerURL` | Console renderer UTs |
| Default validation | `phaseValidate` | `ValidateKubernaut` and provider validation | Validation/lifecycle tests |
| #489 boundary | User-applied CR | Bootstrap chart only; no application OIDC/CR rendering | Chart/static review |

Checkpoint W fails if any builder calculates a separate issuer, rendered values
diverge from the resolver, or #489 starts owning application OIDC configuration.

## 8. Verification matrix

| Tier | Required coverage | Acceptance |
|---|---|---|
| Unit | Resolver precedence, exact URL preservation, default, demo explicitness, renderers, TLS args | All Ginkgo specs pass; no business `testing.T` tests |
| Validation | Default, absolute issuer URLs, provider, AF-disabled, optional-integration absence | No IA-2/IA-5/SC-8/CM-6 regression |
| Integration/envtest | Default, external/demo override, multi-provider, and OAuth2 without Kagenti | Actual AF and Console issuer values are equal |
| Schema/codegen | API comments/markers and generated artifacts | `make manifests generate` has only intended diffs |
| Packaging | #489 bootstrap boundary | No application CR/workload/copied values |
| Helm contract | No application chart in this checkout | External chart parity remains an explicit follow-up; no duplicate values were added here |
| Documentation | Production, external, demo/test, OAuth2, optional rossctl, upgrade paths | Examples and precedence agree |
| Live qualification | Real IdP login/JWKS reachability | Release qualification; unit/envtest cannot prove provider behavior |

Post-implementation checks are `go build ./...`, `golangci-lint run`,
`make test-unit`, `make test-integration`, `make test`, `make manifests generate`,
`go test ./... -run=^$ -timeout=30s`, and `git diff --check`. Inspect CRD, bundle,
and `dist/install.yaml` diffs. The default Kind contract lane remains
`make test-e2e-kind`.

## 9. Compatibility, upgrade, and rollout

- Explicit external or `kubernaut-demo` issuers remain unchanged; the operator must
  not rewrite the CR or infer another realm.
- New v1alpha2 CRs with omitted issuers resolve to production, and the runtime
  fallback covers omitted nested objects/direct builder paths. Administrators
  must provision the production client, audience, redirect URI, JWKS access, and CA
  trust before enabling Console.
- Demo/test installations must set the complete demo URL before upgrading. Removing
  it switches new pods to production and invalidates demo tokens/cookies.
- Kagenti/rossctl migration or first-class support is not part of the v1.6 core
  contract. If retained, it requires a separate compatibility and production-readiness
  decision.
- v1alpha1 is EOL when v1alpha2 releases. The documented migration is an explicit
  export/transform/recreate operation during a maintenance window; no conversion
  webhook, dual-served API, or v1alpha1 runtime fallback is added.
- v1alpha2 is the sole supported stored configuration for the production default,
  explicit overrides, and OAuth2.

## 10. Security and audit requirements

| Requirement | Traceability |
|---|---|
| One selected issuer for AF and Console | FedRAMP IA-2/CM-6; SOC 2 CC6/CC8 |
| Full URL only; no realm guessing/demo fallback | CM-6, SI-10; OWASP ASVS V15.2.4 |
| JWKS/issuer TLS and CA verification retained | FedRAMP IA-5/SC-8/SC-13; ASVS V11.1.1/V13.2.1 |
| HTTP remains limited to explicitly approved dev/test behavior | SC-8 |
| Structured source, generation, resourceVersion, and failure logs | AU-2/AU-3/AU-12/SI-4; SOC 2 CC7 |
| No credentials/tokens in logs or status | AC-6/IA-5; SOC 2 CC6 |

## 11. Risks and approval gates

| Risk | Mitigation |
|---|---|
| API-server default is bypassed by omitted parent/direct builder paths | Retain the runtime fallback and test both defaulted and zero-value objects |
| Isolated cluster cannot reach production IdP | Explicit full override or disable AF; never infer demo |
| Future renderer drift | Central resolver, propagation, Checkpoint W, equality tests |
| Multi-provider Console ambiguity | Document first-provider rule and assert provider membership |
| Optional rossctl integration becomes an accidental prerequisite | Keep it out of core issuer resolution and test absent-resource operation |
| #489 duplicates application config | Keep chart bootstrap-only and review templates |
| Helm acceptance is assigned to the wrong repository | Track the owning chart/change explicitly and require cross-repo parity before closing #479 |
| Realm change invalidates sessions | Publish client/audience/JWKS/redirect rollout checklist |

The pre-implementation approval gates were the CRD-default-plus-runtime-fallback
contract, the first-provider Console rule, the OAuth2 versus rossctl scope
boundary, the #489 packaging boundary, and ownership of Helm acceptance. The
final implementation and verification above record those decisions and their
remaining cross-repository boundary.

**Final confidence: 95% for the operator scope.** The OIDC/defaulting,
Kagenti-removal, CRD-generation, resource-rendering, controller-wiring, and
OIDC-provider contract are covered by source review and automated tests. Remaining
boundaries are intentional: live IdP/JWKS acceptance and application Helm values
remain outside this operator change; #489 must not render an application Kubernaut
CR or own its OIDC values. SPIFFE/SPIRE configuration is deferred until a
separate requirement and acceptance contract exist.

# Issue #499 Implementation Plan

**Issue:** [kubernaut-operator#499](https://github.com/jordigilh/kubernaut-operator/issues/499)

**Parent initiative:** [#486](https://github.com/jordigilh/kubernaut-operator/issues/486)

**Behavior reference:** [kubernaut#1707](https://github.com/jordigilh/kubernaut/issues/1707), Helm `v1.6.0-rc20`

**Status:** Approved for RED tests on 2026-10-05; production implementation
may proceed only through the RED → GREEN → REFACTOR sequence below. Release
readiness also depends on the separate #501 evidence-schema follow-up described
below; that follow-up must remove the retired application-chart requirement
before an operator release can pass its qualification gate.

**Deployment contract:** The operator is the only supported way to deploy
Kubernaut services on OpenShift, generic Kubernetes, and Kind. A Helm chart may
install the operator, but the application chart is not a deployment/API
surface. After this work, the `Kubernaut` CRD is the only configuration
contract for deploying Kubernaut.

**Helm boundary:** Helm `v1.6.0-rc20` is a behavioral and design foundation,
not a compatibility contract. The operator does not need to accept Helm
values, preserve the chart's public field layout, or render byte-for-byte
Helm configuration. Existing service binaries will still receive the
configuration shape they require; that is a runtime implementation constraint,
not a Helm-compatibility promise.

**Branch/worktree:** `feat/499-fleet-crd-helm-parity` in
`/Users/jgil/go/src/github.com/jordigilh/kubernaut-operator-issue-499`.

**Methodology:** RED → GREEN → REFACTOR, with controller wiring verified in
GREEN, the operator-contract test pyramid required locally, and full
application qualification kept at the upstream boundary defined by #501.

## 1. Objective and non-negotiable outcomes

Issue #499 will refactor the unreleased `v1alpha2` Fleet contract and use the
Helm Fleet behavior as a foundation for the operator's native implementation
across generic Kubernetes without installing or reconciling an MCP Gateway
provider.

The implementation must:

1. Make `spec.fleet.enabled` the sole activation flag and separate the
   `mcpGateway`, `scopeCheck`, `oauth2`, and `resilience` concerns.
2. Remove `oauth2.enabled` from the canonical operator API. An active Fleet
   configuration always requires an OAuth2 token endpoint and effective read
   credentials; an unauthenticated Fleet mode is not supported.
3. Support Envoy AI Gateway (`eaigw`) and Kuadrant (`kuadrant`) as configuration
   and least-privilege RBAC selectors only.
4. Select FMC or ACM through the scope-check backend. FMC's endpoint is derived
   when the operator-managed FMC is selected; ACM requires an explicit endpoint
   and bearer-token Secret.
5. Keep MCP Gateway transport on the resolved generic/inter-service trust
   domain, while resolving scope-check backend trust and OAuth2 token-endpoint
   trust as distinct Fleet lanes. Do not invent a per-MCP-Gateway CA domain in
   the CRD.
6. Consume the Helm inter-service defaults (`/etc/tls`, `/etc/tls-ca/ca.crt`,
   and stable object names) from the merged TLS foundation, while adding only
   source-aware Fleet backend/OAuth2 trust overrides without creating
   per-service CA domains.
7. Keep administrator-managed and cert-manager material read-only from the
   operator's perspective; generated material must have an explicit ownership
   boundary.
8. Fail closed before dependent workloads claim readiness when provider,
   credential, TLS, or trust material is invalid or incomplete.
9. Prove the behavior with Ginkgo/Gomega unit tests, envtest reconciliation
   tests, and a Fleet-enabled operator-contract Kind journey; reuse #498's
   existing TLS source journeys and defer full application compatibility to
   #501's upstream qualification.
10. Regenerate and validate deepcopy, CRD, RBAC, bundle, operator-installation,
    security, and upgrade artifacts.

The implementation does **not** install EAIGW, Kuadrant, Gateway API, or their
controllers. Kuadrant external blockers #414 and #470 remain documented
qualification limits, not operator implementation work.

## 2. Confirmed preflight baseline

### 2.1 Repository state and gates

The isolated branch is now fast-forwarded to `origin/main` commit `ffa8577`,
which includes merged #498 (PR #500) and #501 (PR #502). Existing uncommitted
changes in the original checkout were not touched. No issue-499 production
implementation has started in this worktree.

The baseline passed before this plan was written:

- `go build ./...`
- `go test ./... -run=^$ -timeout=60s`
- `golangci-lint run` — zero issues
- `make test` — unit coverage 87.3%, controller integration coverage 78.3%
- `make test-pyramid`
- `make test-security-traceability`
- generated-artifact checks with no unexpected working-tree changes

Post-merge triage checks on `ffa8577` also passed:

- `make test-ci-boundary`
- `make test-hack-scripts`
- `go test ./... -run=^$ -timeout=60s`
- `make test` — unit coverage 87.3%, controller integration coverage 78.5%

The hosted Kind lanes remain completion gates after issue-499 implementation;
the checks above establish that the merged #498/#501 boundary is usable as the
new baseline.

### 2.2 Current production wiring map

| Concern | Current location | Preflight finding | Planned impact |
|---|---|---|---|
| Fleet API | `api/v1alpha2/kubernaut_types.go` | `FleetSpec` is flat: `backend`, `endpoint`, `caSecretName`, `tokenSecretName`, `mcpGatewayEndpoint`, `mcpGatewayType`, `mcpGatewayNamespace`, and `OAuth2.Enabled`. | Replace with nested canonical blocks; update all typed callers and generated code. |
| Fleet validation | `internal/resources/validation.go` | `ValidateFleet` is already called independently from `phaseValidate`; validation is fail-fast and Fleet-aware. | Move rules to nested paths and make active OAuth2 mandatory without a redundant enable flag. |
| Service config | `internal/resources/configmaps.go` | `resolveFleetConfig` is the shared flattening boundary for Gateway, RO, AF, SP, EM, KA, and WE variants. | Promote it to the single resolved-Fleet adapter; render each deployed binary's required configuration without treating Helm's shape as an API contract. |
| FMC config/deployment | `internal/resources/fleetmetadatacache.go` | FMC activation is derived from Fleet enabled + backend; OAuth2 and inter-service trust are mounted separately. | Derive from `scopeCheck.backend`; add distinct OAuth2/backend trust handling where configured. |
| Credentials and CA mounts | `internal/resources/deployments.go` | Gateway/RO/AF mount backend CA/token; SP/EM/KA mount only MCP OAuth2; WE has a mandatory write credential with no fallback. | Keep capability-specific mounts and add lane-specific OAuth2/backend CA resolution. |
| Provider RBAC | `internal/resources/rbac.go` | `mcpGatewayCRDPolicyRules` already emits exact EAIGW vs Kuadrant read rules; namespace-scoped Roles are pruned by label. | Change field paths only; retain provider selector, namespace scoping, and cleanup behavior. |
| Generic TLS | `internal/resources/tls.go`, `common.go`, `tlsconfigmaps.go`, `trustbundle.go` | #491 foundations plus merged #498 now resolve source ownership, default paths, generic trust ConfigMaps, readiness, and dedicated Kind source lanes. | Consume the existing inter-service resolver/mount contract for Fleet's `interService` source; do not add the deferred generic ConfigMap-name override or reimplement #498 source modes/lanes. |
| Reconciler | `internal/controller/kubernaut_controller.go` | Validation, TLS readiness, ConfigMap/deployment/RBAC wiring, and cleanup are already in the normal lifecycle. | Keep the same production entry points and add resolved Fleet/TLS inputs, status/log context, and transitions. |
| API versioning | `config/crd/bases/kubernaut.ai_kubernauts.yaml`, `api/v1alpha2/clean_break_test.go` | `v1alpha2` is the only served/storage version; there is no conversion webhook or v1alpha1 package in this checkout. | The CRD is the sole application contract. No Helm-values conversion, compatibility alias, or old-v1alpha2 migration check; update every in-repo manifest, sample, fixture, and test to the nested shape. |
| Existing evidence | `docs/security/ISSUE-488-CONTROL-TRACEABILITY.md`, `docs/security/ISSUE-491-TLS-CONTROL-ATTESTATION.md`, `docs/tests/498/TEST_PLAN.md`, `docs/design/ISSUE-501-OPERATOR-UPSTREAM-CI-BOUNDARY.md` | #498 owns generic TLS source-lane evidence; #501 owns the operator/upstream CI and release-qualification boundary. | Add only Fleet-specific assertions and reference existing #498/#501 evidence instead of duplicating it. |

### 2.3 Merged-issue triage and plan impact

#### #498 — generic TLS source qualification is complete in the baseline

The merged work now provides the reusable foundation that this issue must
consume:

- explicit `development`, `hook`, `manual-admin`, and `certmanager` Kind
  selectors;
- direct CR-mode coverage for `hook`, `manual`, and
  `AdministratorManaged`, including ownership, fail-closed, trust-probe,
  rotation, and cleanup assertions;
- a local contract image that lets operator Kind tests inspect generated
  resources without deploying the full Kubernaut application; and
- the administrator-owned/manual semantics in the reconciler and TLS volume
  helpers.

Issue #499 must not add another source selector, recreate those four jobs, or
claim #498's source qualification as new Fleet evidence. It only consumes the
existing inter-service TLS resolver and extends the contract journey for
Fleet-specific backend/OAuth2 CA selection, mounts, paths, and readiness. The
generic `spec.tls.interService.caConfigMapName` override remains a separate
TLS follow-up and is deliberately not part of this issue.

The current ownership boundary is material to the design: manual mode leaves
the stable `inter-service-ca` ConfigMap external, while explicit
`AdministratorManaged` mode reads the named CA Secret and avoids creating the
stable ConfigMap. Those #498 semantics remain unchanged; the deferred
`spec.tls.interService.caConfigMapName` generalization belongs to the separate
TLS follow-up and is not an issue-499 acceptance criterion.

#### #501 — operator CI is contract-only; full application qualification is upstream

The merged CI boundary requires the operator repository and its Kind harness to
avoid upstream source checkouts, application images, and upstream workflow
dispatch. `make test-pyramid` now includes `make test-ci-boundary`; release
publication also consumes an immutable upstream qualification record for the
exact operator SHA.

Therefore, issue-499 operator tests may prove CRD admission, rendered
configuration, Deployment mounts, RBAC, ownership, status, and trust behavior
using the local contract image. They must not use full Kubernaut images as an
operator-repository test dependency. Full application Fleet compatibility is an
upstream qualification responsibility, not an additional operator E2E lane.

The spike below confirms one follow-up remains outside this implementation: the
merged #501 evidence schema still requires an upstream chart artifact. That
must be replaced by an immutable application-deployment record in the upstream
release workflow when the application chart is retired. It does not make the
chart a Fleet API contract and does not expand issue #499's scope.

### 2.4 Spike A — Fleet trust API and runtime-consumer boundary

#### Evidence inspected

- The current operator `FleetSpec` has one `CASecretName` and renders it into
  both the top-level service `fleet.tlsCAFile` and the OAuth2 `tlsCAFile`; this
  is the ambiguity that issue #499 must remove.
- The upstream `pkg/fleet.FleetConfig` has a top-level `TLSCAFile` for the
  scope-check backend and `FleetOAuth2Config.TLSCAFile` for the OAuth2 token
  endpoint. The upstream `MCPGatewayConfig` has no independent CA field.
- The upstream MCP client builds its base transport from process-level
  `TLS_CA_FILE`; the operator already supplies that path from the generic
  inter-service TLS resolver. A third `mcpGateway.tls` CRD block would require
  an upstream runtime/configuration contract that does not exist and would
  risk splitting the shared inter-service trust domain.
- The operator already exposes typed `CACertSecretRef` (`name` + `key`,
  default `ca.crt`) and `SecretKeyRef` (`name` + `key`, default `token`) for
  exactly these reference shapes. OAuth2 client credentials are a two-key
  Secret (`client-id` and `client-secret`), so the existing name-only string
  remains the correct type for `credentialsSecretRef` and its per-component
  overrides.

#### Spike decisions

| Question | Decision | Reason |
|---|---|---|
| Nested Fleet API with one central resolver? | **YES** | It matches the issue contract and prevents six component renderers from resolving the same fields differently. |
| One reusable trust type for `scopeCheck.tls` and `oauth2.tls`? | **YES** | Both lanes have the same source/CA semantics but produce different upstream runtime fields. |
| Canonical source values? | **YES:** `interService`, `system`, `file`, `secret` | `interService` is the default; the other values are explicit and fail closed when their payload is absent or contradictory. |
| Canonical trust payload? | **YES:** `source`, `caFile`, `caCertSecretRef` | Reuses the existing typed CA reference and makes file ownership versus Secret mounting unambiguous. `caFile` and `caCertSecretRef` are mutually exclusive and each is valid only for its matching source. |
| Typed ACM bearer-token reference? | **YES:** `scopeCheck.tokenSecretRef *SecretKeyRef` | Reuses the existing name/key type and preserves the default `token` key. |
| Typed OAuth2 credentials reference? | **NO:** retain `credentialsSecretRef string` | A `SecretKeyRef` cannot represent the required two-key credential pair; the existing name-only contract is already used by every component override. |
| Public `mcpGateway.tls` block? | **NO** | MCP transport uses the shared process-level generic/inter-service CA. A new per-gateway trust domain would exceed the deployed service contract and violate the no-per-service-CA boundary. |

#### Resulting trust contract

The implementation-ready contract is:

- `scopeCheck.tls` resolves the backend CA used by `fleet.TLSCAFile`.
- `oauth2.tls` resolves the token-endpoint CA used by
  `fleet.oauth2.tlsCAFile`.
- Both default to the resolved generic/inter-service CA when omitted.
- MCP Gateway connections use the existing generic/inter-service transport
  (`TLS_CA_FILE`) and therefore consume `spec.tls` resolution, not a third
  Fleet TLS block.
- `source=file` validates an absolute path and renders it without creating a
  volume; the administrator owns that path. The operator must not claim that
  it validated file contents that are outside the Pod filesystem.
- `source=secret` validates the referenced key's CA PEM before readiness,
  mounts it read-only at a deterministic path, and never adds ownership to the
  Secret.

This resolves the trust API spike. User approval of the canonical CRD remains
the required architectural gate before RED tests or production type changes.

### 2.5 Spike B — #501 release-evidence schema after application-chart retirement

#### Evidence inspected

- `docs/design/ISSUE-501-OPERATOR-UPSTREAM-CI-BOUNDARY.md` currently assigns
  the upstream repository ownership of application images **and chart builds**
  and shows `upstream.chart` in schema
  `kubernaut-compatibility-qualification/v1`.
- `hack/verify-release-qualification.sh` currently rejects any record that does
  not contain an immutable `upstream.chart` digest. Its fixture tests encode the
  same requirement.
- The operator release workflow only consumes the JSON asset and embeds it in
  operator provenance; it does not need a chart to perform the handoff itself.

#### Spike decision

**YES — replace the chart field with an immutable application-deployment
record, and version the evidence schema to v2.** The old v1 record must not be
accepted after the application chart contract is retired.

The recommended v2 record keeps immutable upstream image digests and adds:

```json
{
  "schema": "kubernaut-compatibility-qualification/v2",
  "result": "passed",
  "upstream": {
    "repository": "jordigilh/kubernaut",
    "ref": "refs/tags/v1.6.0-rc1",
    "sha": "<upstream-sha>",
    "images": {
      "gateway": "quay.io/kubernaut-ai/gateway@sha256:<digest>"
    },
    "deployment": {
      "method": "operator",
      "operator": {
        "repository": "jordigilh/kubernaut-operator",
        "ref": "refs/heads/release/v1.6",
        "sha": "<operator-sha>",
        "source": "manifests",
        "path": "config/default"
      },
      "application": {
        "repository": "jordigilh/kubernaut",
        "ref": "refs/tags/v1.6.0-rc1",
        "sha": "<upstream-sha>",
        "path": "<immutable-kubernaut-cr-manifest-path>"
      }
    }
  },
  "operator": {
    "repository": "jordigilh/kubernaut-operator",
    "ref": "refs/heads/release/v1.6",
    "sha": "<operator-sha>",
    "image": "quay.io/kubernaut-ai/kubernaut-operator@sha256:<digest>"
  },
  "qualification": {
    "workflow_run_id": "<run-id>",
    "workflow_url": "https://github.com/jordigilh/kubernaut/actions/runs/<run-id>",
    "profile": "release-candidate",
    "completed_at": "2026-10-04T00:00:00Z"
  }
}
```

The qualification method is always `operator`: the application is installed by
the exact operator SHA and configured through a `Kubernaut` CR, never by
applying an application chart or directly applying operand manifests. The
operator installation source is `manifests` today and may become
`operatorHelm` only when a future Helm package installs the operator alone; it
must reference the operator repository and exact qualified SHA either way. The
application record identifies the exact upstream CR manifest (or equivalent
checked-in qualification configuration) used with the published upstream
images. The verifier must require the deployment method to be `operator`, the
operator deployment SHA to match `document.operator.sha`, matching repository
/ref/SHA for the application record, and non-empty immutable paths. It must
reject the legacy `chart` field and any direct application `helm` or
`kustomize` deployment method.

The operator and upstream repositories must update the schema, verifier,
fixtures, release documentation, and qualification producer together. The
release asset name and exact operator-SHA check remain unchanged. This is a
separate #501/upstream follow-up, not an issue-499 production dependency; until
it lands, the existing release gate intentionally remains tied to the retired
v1 chart schema and cannot certify an operator-only release.

This cleanly separates the future operator-installation Helm chart from full
application qualification: the former is an operator packaging artifact; the
latter records the exact operator installation source, upstream `Kubernaut` CR
configuration, and image digests used by the compatibility run.

### 2.6 Helm behavioral reference (not an external API contract)

Helm `v1.6.0-rc20` is the reference for the required Fleet behavior and uses a
flat `global.fleet` values shape:

- `enabled`
- `mcpGatewayEndpoint`, `mcpGatewayType`, `mcpGatewayNamespace`
- `backend`, `endpoint`, `tlsCAFile`, `tokenSecretRef`
- `oauth2.enabled`, `oauth2.tokenURL`, `oauth2.scopes`,
  `oauth2.credentialsSecretRef`, and `oauth2.tlsCAFile`
- shared `resilience`

The chart's helper templates centralize these shared values and merge the one
legitimate per-service OAuth2 credential override. The operator will borrow
that central-resolution approach, but will define its own typed API and
resolved internal shape. Resource builders may emit existing service-config
keys where the deployed binaries require them; the operator does not promise
that those keys, defaults, or manifests are interchangeable with Helm.

The issue clarification is binding: when Fleet is active, an MCP Gateway has no
unauthenticated mode. `oauth2.enabled` is a Helm-only behavioral input; it is
not accepted, represented, or validated by the operator API. Active Fleet
always renders the OAuth2 configuration required by the deployed services.

### 2.7 Concrete Helm-foundation-versus-operator example

The proposal uses Helm's behavior as a foundation, not a byte-for-byte or
drop-in-compatible input API. The following illustrates how the Helm grouping
and defaults inform the operator's native CRD contract. It is not a supported
Helm-to-operator values migration.

**Helm input (`global.fleet`, simplified):**

```yaml
global:
  fleet:
    enabled: true
    mcpGatewayEndpoint: https://mcp.example/mcp
    mcpGatewayType: eaigw
    mcpGatewayNamespace: mcp-system
    backend: fleetmetadatacache
    endpoint: ""                 # Helm helper derives FMC's Service URL
    tlsCAFile: /etc/tls-ca/ca.crt
    tokenSecretRef: ""           # unused for FMC
    oauth2:
      enabled: true
      tokenURL: https://idp.example/token
      scopes: [openid, groups]
      credentialsSecretRef: fleet-read
      tlsCAFile: /etc/tls-ca/ca.crt
```

**Proposed operator input (`spec.fleet`, capability-oriented):**

```yaml
spec:
  fleet:
    enabled: true
    mcpGateway:
      endpoint: https://mcp.example/mcp
      type: eaigw
      namespace: mcp-system
    scopeCheck:
      backend: fleetmetadatacache
      tls:
        source: interService
    oauth2:
      tokenURL: https://idp.example/token
      scopes: [openid, groups]
      credentialsSecretRef: fleet-read
      tls:
        source: interService
```

An example of the existing service configuration required by the deployed
binaries is:

```yaml
fleet:
  enabled: true
  backend: fleetmetadatacache
  endpoint: http://fleetmetadatacache-service.<namespace>.svc.cluster.local:8080
  mcpGatewayEndpoint: https://mcp.example/mcp
  mcpGatewayType: eaigw
  tlsCAFile: /etc/tls-ca/ca.crt
  oauth2:
    enabled: true              # rendered service-schema detail, not CRD API
    tokenURL: https://idp.example/token
    credentialsSecretRef: fleet-read
    scopes: [openid, groups]
    tlsCAFile: /etc/tls-ca/ca.crt
```

The operator's resolver may produce equivalent runtime values because the
deployed binaries consume this configuration shape, but that output is not a
public Helm compatibility surface. The important guarantees are the operator's
native validation, derived FMC endpoint, credential mount paths, ownership,
and runtime behavior. The operator's `scopeCheck.tls` and `oauth2.tls` remain
separate even when both default to the same inter-service bundle.

For a Secret-backed override, the operator API makes the ownership and mount
explicit:

```yaml
scopeCheck:
  tls:
    source: secret
    caCertSecretRef:
      name: acm-search-ca
      key: ca.crt
```

The rendered service YAML still receives a lane-specific file path (for
example, `/etc/fleet-tls/scope-check/ca.crt`), and the corresponding Deployment
receives a read-only Secret volume. Helm's `tlsCAFile` alone is only a path; the Helm chart
expects the administrator or another chart value to make that path exist. The
source-aware operator contract closes that ambiguity for Secret-backed material
while retaining a deliberate `file` source for an administrator-managed path.

## 3. Scope boundaries

### In scope

- `api/v1alpha2` Fleet type refactor and CRD regeneration.
- Nested validation, effective credential resolution, and source-aware trust
  resolution.
- ConfigMap rendering, Secret/CA mounts, deployment propagation, provider RBAC,
  namespace RBAC transitions, and cleanup.
- Fleet consumers of the existing generic/inter-service TLS default and trust
  bundle, reusing the merged #491/#498 foundations. Generic ConfigMap-name
  override design remains a separate TLS follow-up.
- Ginkgo unit tests, envtest wiring tests, operator-contract Kind coverage using
  the local fixture image, generated artifacts, clean-break/install/security
  documentation, and a control traceability matrix.

### Explicit non-goals

- No reimplementation of TLS source modes or duplicate source-qualification
  lanes from #491/#498.
- No per-service CA or second trust domain.
- No generic `spec.tls.interService.caConfigMapName` override; that belongs to
  the separate TLS follow-up.
- No FMC server TLS work from #477.
- No provider CRD/controller installation or reconciliation.
- No resolution of external Kuadrant issues #414/#470.
- No application Helm-chart compatibility, Helm-values conversion, or
  compatibility aliases for the current unreleased flat v1alpha2 Fleet fields.
- No supported deployment path that bypasses the operator and applies the
  Kubernaut application chart directly.
- No duplicate #498 TLS source lanes or full-application image dependency in
  operator CI; full application compatibility remains the upstream #501 gate.
- No upstream checkout, image pull, or workflow dispatch from this repository's
  tests or release workflows.

## 4. Design alternatives and approval gates

This is the required architectural decision point. The alternatives were
derived from the current resolver/rendering boundary and Helm's behavioral
reference; Helm's values and rendered manifests are not application contracts.

### 4.0 Approved decisions — 2026-10-05

The issue owner approved Option A and the corrected clean-break boundary:

- Fleet uses nested `mcpGateway`, `scopeCheck`, `oauth2`, and `resilience`
  blocks resolved once through a central adapter.
- Fleet has no `oauth2.enabled`; `spec.fleet.enabled` is the sole Fleet gate.
- Fleet OAuth2 uses a Fleet-specific type without `Enabled`. The existing
  `OAuth2Spec.Enabled` remains only for the separate LLM-profile OAuth2 API.
- Fleet trust uses independent `scopeCheck.tls` and `oauth2.tls` lanes with
  `interService`, `system`, `file`, and `secret` sources, `caFile`, and typed
  `caCertSecretRef`; ACM uses typed `scopeCheck.tokenSecretRef`.
- Current unreleased flat v1alpha2 fields are replaced directly. There is no
  migration transform, compatibility alias, conversion webhook, or old-field
  validation. Repository samples, fixtures, manifests, and tests must not
  carry `spec.fleet.oauth2.enabled`.
- The separate #501 release follow-up uses v2 operator-only qualification
  evidence with exact operator installation and upstream `Kubernaut` CR
  provenance instead of an application-chart digest.

### Option A — Nested canonical API with one resolved Fleet adapter (recommended)

Expose capability-oriented types:

```yaml
spec:
  fleet:
    enabled: true
    mcpGateway:
      type: eaigw
      endpoint: https://gateway.example/mcp
      namespace: mcp-system
    scopeCheck:
      backend: fleetmetadatacache
      endpoint: ""                 # derived for operator-managed FMC
      tls: ...
      tokenSecretRef: ...           # required for acm
    oauth2:
      tokenURL: https://idp.example/token
      scopes: [openid, groups]
      credentialsSecretRef: fleet-read
      tls: ...
    resilience: ...
```

The operator converts this once into a resolved internal Fleet configuration.
Each component renderer then selects only the capabilities it consumes and
emits the configuration required by that deployed binary.

**Advantages**

- Matches the issue's capability boundary and removes ambiguous field names.
- Keeps OAuth2 mandatory by construction and avoids a dead `enabled` boolean.
- Centralizes defaults, derived FMC endpoint behavior, effective credentials,
  TLS lane selection, and provider validation.
- Uses Helm's centralized merge/resolution pattern as a foundation without
  exposing Helm implementation details or promising Helm compatibility in the
  operator API.
- Makes RBAC and mounts auditable by capability rather than by incidental field
  presence.

**Costs/risks**

- Broad compile-time field migration across builders and tests.
- Requires an explicit source-aware TLS type and generated-schema review.
- Existing in-repo manifests and tests need to be updated to the nested shape;
  no compatibility transform is required for an unreleased API.

### Option B — Nested API with direct per-renderer field reads

Add the nested blocks but let each ConfigMap/deployment/RBAC builder read them
directly, retaining multiple local resolution paths.

**Advantages:** smaller initial refactor and fewer internal types.

**Costs/risks:** duplicates defaulting and validation logic, makes it easy for
FMC, Gateway, AF, or the read-only components to disagree about a CA or
credential, and makes future Helm changes harder to audit. This fails the issue's
"one internal resolved Fleet configuration" requirement and is not recommended.

### Option C — Keep flat fields and add nested aliases

Accept both the current flat fields and the proposed nested fields, with a
precedence rule.

**Advantages:** least immediate manifest churn.

**Costs/risks:** retains the ambiguous contract, creates two sources of truth,
requires conflict semantics, keeps `oauth2.enabled` alive, and undermines the
explicit v1alpha2 clean-break boundary. This is rejected for the unreleased API.

### 4.1 Recommended trust-source contract (approval required)

Option A should use a small, reusable source-aware trust type for both
`scopeCheck.tls` and `oauth2.tls`. The proposed source values are:

| Source | Meaning | Operator action |
|---|---|---|
| `interService` (default) | The resolved generic/inter-service trust bundle. | Use the resolved operator CA path and existing trust mount, with Helm's stable defaults as the starting point. |
| `system` | Public/system trust only. | Omit an explicit CA file; never silently substitute this when another source was requested. |
| `file` | An administrator-provided CA file path already available to the workload. | Render the exact path; validate it is absolute and document that the administrator owns the mount. |
| `secret` | A CA PEM in an operator-mounted Secret. | Mount read-only at a deterministic path, render that path, validate the Secret/key before readiness, and never adopt the Secret. |

The proposed fields are `source`, `caFile`, and `caCertSecretRef` (the latter
uses the existing `CACertSecretRef` shape). The source and payload fields are
mutually exclusive. If the issue owner prefers Helm-shaped strings, the
alternative is to keep `tlsCAFile` plus `caSecretName`; that is simpler but
cannot express ownership/key semantics as clearly and is not recommended.

The source contract is exact: an omitted `source` resolves to
`interService`; `interService` and `system` reject both payload fields;
`file` requires only an absolute `caFile`; and `secret` requires only
`caCertSecretRef`. Secret references are namespaced to the `Kubernaut` CR's
namespace, default the key to `ca.crt`, are validated as CA PEM, and are
mounted read-only at lane-specific paths
(`/etc/fleet-tls/scope-check/ca.crt` or `/etc/fleet-tls/oauth2/ca.crt`).

The generic `spec.tls.interService.caConfigMapName` override is intentionally
outside #499. A separate TLS follow-up owns its API, source/default/ownership
matrix, and manual E2E coverage. Issue #499 uses the current resolved
inter-service path and trust bundle exactly as provided by #498; it must not
create a second generic trust override or a per-service CA setting.

### 4.2 Approval gates

Implementation must not begin until the issue owner approves:

1. Option A versus B/C.
2. Canonical names and shapes for `mcpGateway`, `scopeCheck`, and Fleet OAuth2.
3. Removing `oauth2.enabled` from the operator API.
4. The recommended source-aware Fleet trust type (`source`, `caFile`, and
   `caCertSecretRef`), while preserving #498's existing generic inter-service
   trust contract without a new ConfigMap-name override.
5. `scopeCheck.tokenSecretRef` as a typed `SecretKeyRef` (default key `token`)
   versus a name-only string. The recommendation is the typed reference because
   the API already uses it for other credential-bearing integrations.
6. The clean-break schema boundary and the test/control matrix below. No
   compatibility migration or validation of the old Fleet field is required.
7. The #501 v2 qualification shape: `deployment.method=operator`, an exact
   operator installation record, and an exact upstream `Kubernaut` CR
   configuration record, with no application chart field.

## 5. Proposed canonical API and resolution rules

The following is a plan-level contract, not an implementation until approved.

```go
type FleetSpec struct {
    Enabled     *bool                  `json:"enabled,omitempty"`
    MCPGateway  FleetMCPGatewaySpec    `json:"mcpGateway,omitempty"`
    ScopeCheck  FleetScopeCheckSpec    `json:"scopeCheck,omitempty"`
    OAuth2      FleetOAuth2Spec        `json:"oauth2,omitempty"`
    Resilience  *FleetResilienceSpec  `json:"resilience,omitempty"`
}

type FleetMCPGatewaySpec struct {
    Type      string `json:"type,omitempty"`      // eaigw | kuadrant
    Endpoint  string `json:"endpoint,omitempty"`
    Namespace string `json:"namespace,omitempty"`
}

type FleetScopeCheckSpec struct {
    Backend       string            `json:"backend,omitempty"` // FMC | ACM
    Endpoint      string            `json:"endpoint,omitempty"`
    TLS           *FleetTrustSpec   `json:"tls,omitempty"`
    TokenSecretRef *SecretKeyRef    `json:"tokenSecretRef,omitempty"`
}

type FleetOAuth2Spec struct {
    TokenURL             string          `json:"tokenURL,omitempty"`
    Scopes               []string        `json:"scopes,omitempty"`
    CredentialsSecretRef string          `json:"credentialsSecretRef,omitempty"`
    TLS                  *FleetTrustSpec `json:"tls,omitempty"`
}

type FleetTrustSpec struct {
    Source       string           `json:"source,omitempty"` // interService | system | file | secret
    CAFile       string           `json:"caFile,omitempty"`
    CACertSecretRef *CACertSecretRef `json:"caCertSecretRef,omitempty"`
}
```

The exact Go names may be adjusted to surrounding repository conventions during
implementation, but the following semantics are fixed by the recommendation:

- `enabled=false` or omitted makes the whole Fleet subtree inert and permits
  pre-staging.
- `enabled=true` requires gateway type, gateway endpoint, gateway namespace,
  scope-check backend, and the OAuth2 token URL/effective credentials.
- FMC selection derives the scope-check endpoint from the operator-managed
  `fleetmetadatacache` Service when the endpoint is omitted.
- ACM selection requires an explicit endpoint and a token Secret/key.
- All six read-capable components may use a shared credential or a component
  override. FMC retains its own existing override.
- WorkflowExecution's write-scoped credential remains mandatory and never falls
  back to the shared read credential.
- Invalid provider/backend/source values produce no provider-specific RBAC or
  dependent workload readiness.

### 5.1 Clean-break schema boundary

`v1alpha2` is unreleased and has no conversion webhook. The current flat Fleet
fields are implementation predecessors, not a compatibility input contract.
Replace them directly with the nested API; do not add aliases, a conversion
webhook, a migration transform, or validation branches for old field names.

The Fleet API must use a Fleet-specific OAuth2 type without `Enabled`. The
current shared `OAuth2Spec.Enabled` is retained only for the separate
LLM-profile OAuth2 API (`spec.llmProfiles[*].oauth2.enabled`); it must not be
reachable through `spec.fleet.oauth2`. The implementation and generated
artifacts must prove that `spec.fleet.oauth2.enabled` is absent from:

- the CRD schema and webhook/CEL rules;
- samples and install manifests;
- serialized test fixtures and contract manifests; and
- Fleet validation, configuration, deployment, and mount logic.

`spec.fleet.enabled` is the sole Fleet activation flag. Active Fleet validation
checks the required gateway, scope-check, OAuth2 URL, effective credentials,
and trust material; it does not check for a removed `oauth2.enabled` field.
The component-level `FleetOverrideSpec` and WorkflowExecution's separate write
credential retain their existing meanings.

### 5.2 Helm foundation-to-operator mapping

The map documents how Helm's behavior informed the native API and fixtures; it
is not a second CRD alias or a compatibility promise.

| Helm `global.fleet` value | Canonical operator field | Rendering behavior |
|---|---|---|
| `enabled` | `spec.fleet.enabled` | Sole activation flag. |
| `mcpGatewayEndpoint` | `spec.fleet.mcpGateway.endpoint` | Supplies the deployed services' gateway endpoint configuration. |
| `mcpGatewayType` | `spec.fleet.mcpGateway.type` | Selects config and provider-specific RBAC. |
| `mcpGatewayNamespace` | `spec.fleet.mcpGateway.namespace` | Scopes registry watches and namespace Roles. |
| `backend` | `spec.fleet.scopeCheck.backend` | `fleetmetadatacache` derives endpoint; `acm` requires one. |
| `endpoint` | `spec.fleet.scopeCheck.endpoint` | Rendered to backend consumers only. |
| `tlsCAFile` | `scopeCheck.tls` | Operator resolves a trust source and path; the Helm path is only the defaulting/reference point. |
| `tokenSecretRef` | `scopeCheck.tokenSecretRef` | Mounted only by backend scope-check consumers. |
| service-level `fleet.oauth2.enabled` | no canonical field | Helm-only toggle; it is not copied or validated. Active Fleet is inferred from `spec.fleet.enabled`. LLM OAuth2 enablement remains a separate API. |
| `oauth2.tokenURL` | `spec.fleet.oauth2.tokenURL` | Required when Fleet is active. |
| `oauth2.scopes` | `spec.fleet.oauth2.scopes` | Shared by Fleet-aware consumers. |
| `oauth2.credentialsSecretRef` | `spec.fleet.oauth2.credentialsSecretRef` | Shared read credential; component overrides remain supported. |
| `oauth2.tlsCAFile` | `spec.fleet.oauth2.tls` | Defaults to generic/inter-service trust, but may be independently overridden. |
| `resilience` | `spec.fleet.resilience` | Shared zero-value-safe resilience block. |

## 6. TDD implementation phases

### Phase 0 — Approval and RED preparation

**RED:** Do not modify production types. Add/confirm contract tests that define
the nested Fleet shape and assert that `spec.fleet.oauth2.enabled` is absent
from the CRD, samples, fixtures, and contract manifests; do not add a
compatibility test for the unreleased flat shape.

**GREEN gate:** Record the approved API/trust decision in a design decision
document or this plan before changing the CRD.

**REFACTOR:** Confirm all affected symbols and generated-artifact commands.

### Phase 1 — API and validation

**RED tests**

- Ginkgo serialization/schema tests for nested Fleet blocks.
- Assert the generated CRD has no canonical `oauth2.enabled` field.
- Disabled Fleet accepts pre-staged nested values without validating them.
- Active Fleet rejects missing gateway/provider/backend/namespace, OAuth2 token
  URL, effective read credentials, ACM endpoint/token, invalid source payloads,
  and unsupported enum values.
- Active Fleet requires WorkflowExecution's separate write credential.
- Provider/backend errors identify the nested field path.

**GREEN**

- Add nested v1alpha2 types and helper methods.
- Update `ValidateFleet`, `FleetMetadataCacheEnabled`, and all typed references.
- Remove the obsolete Fleet CEL rule tied to `oauth2.enabled`; add only
  admission-safe nested invariants that do not duplicate effective-credential
  reconciliation checks.

**REFACTOR**

- Consolidate error accumulation and source validation.
- Preserve lowercase/no-punctuation error conventions and structured controller
  logging at the reconciliation boundary.
- Regenerate deepcopy and CRD schema.

### Phase 2 — Resolved Fleet configuration and resource builders

**RED tests**

- Unit tests for one resolved configuration consumed by every component lane.
- EAIGW and Kuadrant output/configuration differences.
- FMC-derived endpoint versus explicit ACM endpoint.
- Separate scope-check CA, OAuth2 CA, and generic/inter-service defaults.
- Secret key/path rendering and read/write credential separation.
- No dead volumes for components that do not consume backend scope checks.
- Resilience omission preserves upstream defaults.

**GREEN**

- Implement the resolved Fleet adapter.
- Update ConfigMap builders (`GatewayConfigMap`, RO, AF, SP, EM, KA, WE, and
  `FleetMetadataCacheConfigMap`) to render each deployed binary's required
  configuration through it; reuse existing service-config keys where needed,
  without treating Helm's flat YAML as an operator compatibility contract.
- Update deployment volume/mount helpers for the two trust lanes and token
  references.

**REFACTOR**

- Remove field-specific duplicate resolution.
- Reuse the existing production builder and YAML structs in white-box tests;
  do not create mirror structs for full rendered shapes.
- Keep existing component-specific least-privilege exceptions explicit.

### Phase 3 — RBAC, reconciler wiring, and lifecycle transitions

**RED tests**

- Envtest valid EAIGW and Kuadrant reconciliation journeys.
- Exact provider-specific CRD read rules and no cross-provider grants.
- Namespace-scoped Role/RoleBinding creation, provider switch, namespace switch,
  Fleet disable, and finalizer cleanup.
- Invalid configuration stops before dependent Deployments/ConfigMaps claim
  readiness and records an actionable phase/condition/event.
- Credential and CA Secret/ConfigMap updates roll the correct workloads.

**GREEN**

- Wire the resolver through `phaseValidate`, ConfigMap/deployment assembly,
  `ClusterRoles`, `ClusterRoleBindings`, `MCPGatewayNamespaceRBAC`, and existing
  prune/delete paths.
- Keep controller create/update/delete logs with generation and
  resourceVersion context.

**CHECKPOINT W**

- Every new resolver/helper has a production caller.
- Every new resource path is exercised through `KubernautReconciler.Reconcile`
  in envtest.
- No provider-specific builder or RBAC helper is orphaned.
- No `TODO: wire later` remains.

**REFACTOR**

- Ensure provider changes cannot leave stale Roles, RoleBindings, ConfigMaps,
  Secret mounts, or owner references.
- Verify no operator ownership is added to administrator/cert-manager material.

### Phase 4 — Fleet trust integration on the merged TLS foundation

Issue #498 already owns source selection and generic TLS source qualification.
This phase adds only the Fleet-specific trust consumers and verifies that their
`interService` source uses the existing resolved generic trust contract.

**RED tests**

- Resolve `scopeCheck.tls` and `oauth2.tls` independently for
  `interService`, `system`, `file`, and `secret` sources.
- Prove Secret-backed Fleet CA references produce matching read-only mounts and
  rendered paths, while file/system sources do not create invented volumes.
- Prove Fleet's `interService` source consumes the existing #498 path, volume,
  and ownership behavior without adding a Fleet-specific generic trust object.
- Prove missing or invalid Fleet CA material fails closed before dependent
  workloads claim readiness.
- Prove Fleet CA paths, OAuth2 CA paths, credentials, and component-specific
  configuration reach every intended consumer without dead volumes.
- Re-run the existing #498 source-lane contract tests as regression evidence;
  do not duplicate their source assertions in issue-499 tests.

**GREEN/REFACTOR**

- Extend the Fleet trust resolver and volume helpers around the existing
  `InterServiceTLSCAFileFor`/`InterServiceTLSCAVolume` contract rather than
  changing generic `TLSMaterial`, ConfigMap ownership, or adding a parallel
  TLS source mode.
- Keep manual/admin objects unowned and generated objects owner-referenced only
  within their approved ownership boundary.
- Add Fleet-specific assertions to the existing operator-contract Kind journey;
  the contract image proves resource shape, mounts, trust, and readiness, not
  full application compatibility.

### Phase 5 — Operator contract qualification and external application boundary

Use the merged #498/#501 test structure rather than creating new application
image lanes:

1. Run the existing `development`, `hook`, `manual-admin`, and `certmanager`
   selectors as regression gates owned by #498.
2. Add a Fleet-enabled contract scenario that applies a real `Kubernaut` CR and
   inspects resolved Fleet ConfigMaps, Deployment mounts, provider RBAC,
   ownership, status, and fail-closed behavior using the local contract image.
3. Cover EAIGW in the operator contract lane; cover Kuadrant configuration and
   exact RBAC through unit/envtest because external #414/#470 qualification
   remains unresolved.
4. Do not pull or deploy full Kubernaut application images in this repository.
   Full application lifecycle and Fleet compatibility are qualified by the
   upstream workflow against the exact operator SHA under #501.
5. Validate the operator through current Kustomize/OLM production manifests;
   when the future operator Helm chart exists, it must install the operator
   only and expose no second Fleet configuration API.

Update operator-installation, security/TLS, Fleet, and
`docs/upgrade-v1alpha1-to-v1alpha2.md` guidance. Documentation must state that
the application chart is deprecated, the operator is the only deployment path,
the CRD is the sole application API, there is no Helm-values migration or
conversion webhook, and OAuth2 is mandatory when Fleet is enabled. It must not
carry the removed `spec.fleet.oauth2.enabled` field in any sample or fixture,
and it must not present Helm values as a supported input contract.

### Phase 6 — Full validation and generated artifacts

Run, in addition to affected focused suites:

```bash
go build ./...
golangci-lint run
make manifests generate
make bundle
make test
make test-pyramid
make test-ci-boundary
make test-hack-scripts
make test-security-traceability
git diff --check
```

`make manifests generate` and `make bundle` must be followed by a review of CRD,
RBAC, webhook, OLM, and sample changes. No unrelated generated drift is
acceptable. The existing #498 Kind selectors must remain green, but the issue
#499 operator gate must use the local contract image and pass the #501 boundary
check. Upstream qualification evidence is consumed by the release workflow, not
generated by operator CI.

## 7. Wiring manifest

| Component | Production entry point | Wiring location | Required evidence |
|---|---|---|---|
| Canonical Fleet API | `KubernautReconciler.Reconcile` | `api/v1alpha2/`, `phaseValidate` | `UT-FLEET-API-*`, `IT-FLEET-ADMISSION-001` |
| Resolved Fleet adapter | service ConfigMap/deployment builders | `internal/resources/configmaps.go`, `deployments.go`, `fleetmetadatacache.go` | `UT-FLEET-RESOLVE-*`, `IT-FLEET-WIRING-001` |
| Fleet validation | validation phase before deployment | `internal/resources/validation.go`, controller `phaseValidate` | `UT-FLEET-VALIDATION-*`, `IT-FLEET-FAIL-CLOSED-001` |
| EAIGW/Kuadrant selector | provider config + RBAC | `internal/resources/rbac.go`, Fleet config builders | `UT-FLEET-PROVIDER-001`, `IT-FLEET-RBAC-001` |
| Namespace RBAC | reconciler deployment/prune and finalizer | `deployMCPGatewayNamespaceRBAC`, `pruneOrphanedMCPGatewayNamespaceRBAC`, `deleteMCPGatewayNamespaceRBAC` | `IT-FLEET-RBAC-LIFECYCLE-001` |
| Scope-check token/CA | GW/RO/AF deployment lanes | `appendFleetSecretMountsVariant` and resolved Fleet mount helpers | `UT-FLEET-CREDENTIALS-001`, `IT-FLEET-CREDENTIALS-001` |
| OAuth2 token-endpoint CA | every Fleet-aware OAuth2 consumer, including FMC | component deployment builders and ConfigMaps | `UT-FLEET-OAUTH2-TLS-001`, `IT-FLEET-OAUTH2-TLS-001` |
| Fleet trust consumer | validation, runtime trust, workload/config paths | resolved Fleet trust adapter, `InterServiceTLS*`, Fleet mount helpers, and existing TLS readiness | `UT-TLS-499-*`, `IT-TLS-499-*`, `E2E-FLEET-499-*` in the existing contract Kind journey |
| Operator CI boundary | contract-only Kind and release qualification handoff | `Makefile`, `hack/verify-ci-boundary.sh`, `hack/verify-release-qualification.sh`, `.github/workflows/release.yml` | `CI-501-*`; no upstream checkout/image/workflow caller |
| Status/audit diagnostics | reconcile failure/phase transitions | controller validation and error paths | `IT-FLEET-FAIL-CLOSED-001`, structured log evidence |

Checkpoint W fails if any row has no production caller, only unit coverage, or
an implementation that is not reached through reconciliation.

## 8. Control-objective test traceability

The implementation will add `docs/security/ISSUE-499-CONTROL-TRACEABILITY.md`
using the repository's existing control-evidence format.

| Business assertion | FedRAMP/NIST controls | SOC 2 | OWASP ASVS 5.0.0 evidence direction |
|---|---|---|---|
| Nested API clean break and in-repo manifest/test shape are deterministic | CM-2, CM-3, CM-6, SI-10 | CC8 | V15.2.4, V16.5.2; CRD/schema and fixture-shape tests |
| Operator CI does not consume the full application; compatibility is qualified at an exact upstream/operator SHA boundary | SA-12, CM-3, CM-8, AU-3, AU-12, RA-5, CA-7 | CC7, CC8 | V15.1.2, V15.2.4, V16.2.1, V16.5.3; reuse #501 boundary and release-evidence artifacts |
| Unsupported provider/backend combinations fail closed | AC-3, CM-6, SI-10 | CC6, CC7 | V5.1.1, V12.1.3, V16.5.2; validation/admission/envtest |
| EAIGW and Kuadrant receive only required CRD read permissions | AC-3, AC-6 | CC6 | V4.1.1, V4.2.1, V13.3.1; exact RBAC unit/envtest |
| Namespace scoping and stale RBAC cleanup are enforced | AC-3, AC-6, CM-6 | CC6, CC8 | V4.1.1, V13.3.1; provider/namespace transition tests |
| Read credentials and WE write credentials do not cross lanes | IA-2, IA-5, AC-6 | CC6, CC7 | V6.2.1, V13.3.1, V13.3.2; mount/path and negative fallback tests |
| Backend, OAuth2, and inter-service trust sources remain distinct | SC-8, SC-12, SC-13, SC-17 | CC6, CC7 | V12.1.1, V12.1.3, V13.2.1; resolver/mount/readiness tests |
| Invalid/missing trust material cannot result in ready plaintext workloads | SC-8, SC-13, SI-10 | CC7, A1 | V12.2.1, V13.2.1, V16.5.2; envtest and the operator-contract Kind trust probe |
| Administrator/cert-manager material is not adopted or deleted | AC-6, CM-6, SC-12 | CC6, CC8 | V13.3.1, V13.3.2; ownership and cleanup tests |
| Reconciliation actions and failures are reconstructable | AU-2, AU-3, AU-12, SI-4 | CC7, CC8 | V16.1.1, V16.2.1; structured logs/events/status tests |

The matrix will distinguish repository evidence from external IdP, provider,
cluster PKI, and formal assessor evidence; passing tests will not be presented
as a complete authorization or cryptographic assessment.

## 9. CRD and deployment-contract impact

- `v1alpha2` remains the sole served/storage version.
- The plan is based on merged `origin/main` `ffa8577`; #498 and #501 are
  consumed foundations, not parallel implementation work.
- There is no conversion webhook and no v1alpha1 package to update in this
  checkout.
- Current unreleased flat v1alpha2 Fleet fields are removed rather than kept as
  aliases. No migration transform or old-field validation is required; all
  in-repo samples, fixtures, and manifests are updated directly to the nested
  fields. Helm values are not converted because the application chart is not a
  supported deployment path.
- The operator-installation chart/package installs the operator only. It does
  not become a second Fleet configuration API; the `Kubernaut` CRD remains the
  sole application contract across OCP, generic Kubernetes, and Kind.
- Operator-local Kind evidence uses the #501 contract image and proves
  generated resource/configuration behavior. Full Kubernaut application
  compatibility is qualified externally against the exact operator SHA and is
  not repeated in this repository.
- Existing component-level `FleetOverrideSpec` read credential behavior is
  retained unless the approved API decision changes its name; WorkflowExecution
  remains explicitly separate.
- `api/v1alpha2/zz_generated.deepcopy.go`,
  `config/crd/bases/kubernaut.ai_kubernauts.yaml`, RBAC manifests, samples,
  bundle metadata, operator-installation artifacts, and documentation are
  generated/reviewed outputs.
- No database or application-data migration is introduced by this CRD refactor.

## 10. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Broad field rename leaves a stale caller | Complete symbol/reference search before each edit; compile after each RED/GREEN unit; final `go build ./...`. |
| Service config schema differs from CRD naming | Keep production YAML structs and service-specific fixture snapshots; assert exact rendered configuration for every consumer lane. |
| CA path is rendered but not mounted | Resolve source and mount together; unit-test volume/path pairs and envtest every active consumer. |
| Secret-backed Fleet CA is accidentally adopted or a path is left unmounted | Explicit Secret ownership/read-only tests, lane-specific volume/path assertions, cleanup test, and no desired object for administrator material. |
| OAuth2 token endpoint trust is conflated with backend trust | Separate resolved fields and test each override independently, including FMC. |
| Kuadrant APIs are absent in generic clusters | Provider configuration/RBAC is typed data only; no provider API watch is added to the operator; generic gate uses EAIGW. |
| FMC server TLS scope expands accidentally | Keep FMC API HTTP behavior and track #477 separately. |
| Existing #491/#498 behavior regresses | Reuse their resolver/material ownership functions and rerun their existing suites plus the full pyramid. |
| Contract image masks an application integration defect | Treat contract Kind as operator evidence only; require the #501 upstream exact-SHA qualification before release. |
| #501 qualification evidence still names a retired application chart | Keep issue #499 independent of chart compatibility and coordinate the evidence-schema correction with the upstream release workflow. |

## 11. Success and completion criteria

The issue is complete only when all of the following are true:

- The approved canonical nested Fleet API is the only v1alpha2 shape and has no
  `oauth2.enabled` field.
- The operator is the only supported Kubernaut deployment path on OCP, generic
  Kubernetes, and Kind; the `Kubernaut` CRD is the sole application API.
- The merged #498 TLS source lanes remain green without being duplicated, and
  issue-499 Fleet trust behavior is proven through the operator-contract Kind
  journey.
- Operator CI passes the merged #501 boundary; full application compatibility
  is represented by immutable upstream qualification evidence for the exact
  operator SHA rather than a local application-image E2E.
- Both providers and both scope backends have rendered-config, validation, and
  exact-RBAC coverage.
- FMC derivation, ACM endpoint/token requirements, component read overrides, and
  the WE write-credential boundary are proven.
- Scope-check CA, OAuth2 CA, and generic/inter-service trust are independently
  resolved, mounted, rendered, and failure-tested.
- Manual/admin/cert-manager ownership and cleanup assertions pass.
- Namespace RBAC creation, provider/namespace transitions, pruning, and
  finalizer cleanup pass.
- Unit, envtest, security traceability, and generic Kind tests pass with no
  sleeps or pending tests.
- `make manifests generate` and `make bundle` produce reviewed artifacts with no
  unrelated diff.
- Operator-installation, security/TLS, clean-break, Fleet, and issue-specific design/
  traceability documentation is updated.
- `go build ./...`, lint, `make test`, and the required pyramid checks pass.

## 12. Confidence assessment before approval

**Confidence: 96% for the impact map and recommended implementation direction.**

**Justification:** The production callers, resource builders, lifecycle wiring,
provider RBAC, merged #498 TLS ownership/source lanes, merged #501 CI boundary,
clean-break CRD boundary, Helm behavioral reference, and post-merge static gates
were inspected and verified. The remaining gates are issue-owner approval of
the exact public trust-reference shape (`source`/`caFile`/`caCertSecretRef`),
the typed `SecretKeyRef` for the ACM token, and the clean-break/test boundary,
plus the separate upstream follow-up that removes the retired application-chart
artifact from #501 qualification evidence. No implementation should begin
until the API/trust decisions are approved; after approval the remaining risks
are mechanical field refactoring, operator-installation packaging alignment,
external Kuadrant qualification, and the cross-repository release-evidence
correction.

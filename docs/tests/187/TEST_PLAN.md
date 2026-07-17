# IEEE 829 Test Plan — Issue #187: Refactor LLM configuration to top-level named profiles

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-187                                             |
| **Issue**          | #187 — Refactor LLM configuration to top-level named profiles |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-07-17                                         |
| **Scope**          | `api/v1alpha1/kubernaut_types.go`, `internal/resources/{validation,configmaps,deployments,networkpolicies,common}.go` |

## 1. Objective

Verify that LLM configuration moves cleanly from a single
`spec.kubernautAgent.llm` block into a top-level, named-profile map
(`spec.llmProfiles`), with Kubernaut Agent (KA) and API Frontend (AF)
independently resolving their own profile by reference
(`llmProfileRef`). Confirm referential-integrity and same-credentials
constraints are enforced at validation time, confirm every renderer
(ConfigMaps, Deployments, NetworkPolicy) resolves from the correct
profile instead of reaching into KA's block, and confirm two
pre-existing AF bugs are fixed as part of the decoupling:

1. AF's `llm-credentials`/`llm-tls-client` volumes always mounted KA's
   Secret, regardless of AF's own configured profile.
2. AF never mounted an `oauth2-credentials` volume at all, causing a
   crash-loop at startup whenever LLM OAuth2 was enabled (upstream AF
   hard-fails reading missing `client-id`/`client-secret` files).

Also verify the new independent, disable-able LLM profile for API
Frontend's severity-triage feature
(`apiFrontend.severityTriage.llmProfileRef` / `llmEnabled`), which
closes an operator-side rendering gap that caused severity-triage to
always silently inherit `agent.llm` regardless of intent.

## 2. Test Strategy

Unit tests only (no envtest needed for these builder-function and
validation-function tests), matching the existing `internal/resources`
convention. Controller-level fixtures (`internal/controller/*_test.go`)
are updated to the new shape and continue to run under the existing
envtest-backed `make test-integration` suite, but exercise no new
business logic themselves.

## 3. Test Scenarios

### 3.1 Profile content validation (`internal/resources/validation_test.go`, `Describe("LLM Profile Content Validation")`)

| ID     | FedRAMP | Description                                                              |
|--------|---------|---------------------------------------------------------------------------|
| VL-001 | CM-6    | rejects a profile with empty `provider`                                  |
| VL-002 | CM-6    | rejects a profile with empty `model`                                     |
| VL-003 | CM-6    | rejects a profile with empty `credentialsSecretName`                     |
| VL-004 | CM-6    | accumulates all missing-field errors for one profile                     |
| VL-005 | CM-6    | accepts a fully valid profile                                            |
| UT-VL-196-001/002 | SC-8 | provider `openai` requires an explicit `endpoint` (both KA and AF need it) |
| VL-006 | SC-8    | rejects `tlsCertFile` set without `tlsKeyFile` (and vice versa)          |
| VL-007 | SC-8    | rejects an mTLS cert/key pair set without `tlsClientSecretRef`           |
| VL-008 | SC-8    | rejects `tlsClientSecretRef` set without an mTLS cert/key pair          |
| VL-009 | SC-8    | accepts a complete mTLS configuration                                    |

### 3.2 Referential integrity (`Describe("LLM Profile Referential Integrity")`)

| ID     | FedRAMP | Description                                                              |
|--------|---------|---------------------------------------------------------------------------|
| VL-010 | CM-6    | rejects a missing `kubernautAgent.llmProfileRef` (required)              |
| VL-011 | CM-6    | rejects `kubernautAgent.llmProfileRef` naming an undefined profile        |
| VL-012 | CM-6    | accepts `kubernautAgent.llmProfileRef` naming a defined profile          |
| VL-013 | CM-6    | rejects `kubernautAgent.llmProfileRef` when `spec.llmProfiles` is empty (nil-map edge case, no panic) |
| VL-014 | CM-6    | rejects `apiFrontend.llmProfileRef` naming an undefined profile          |
| VL-015 | CM-6    | accepts an empty `apiFrontend.llmProfileRef` (defaults to KA's profile)  |
| VL-016 | CM-6    | accepts `apiFrontend.llmProfileRef` naming its own defined profile        |
| VL-017 | CM-6    | rejects an invalid `phaseModels` key (not one of `rca`/`workflow_discovery`/`validation`) |
| VL-018 | CM-6    | reports multiple invalid `phaseModels` keys independently                |
| VL-019 | CM-6    | accepts empty `phaseModels`                                              |
| VL-020 | CM-6    | rejects a `phaseModels` entry with an empty profile ref (no fallback, unlike `llmProfileRef` fields) |
| VL-021 | CM-6    | rejects a `phaseModels` value naming an undefined profile                |
| VL-022 | CM-6    | accepts a `phaseModels` value sharing KA's `credentialsSecretName`       |
| VL-023 | CM-6    | rejects a `phaseModels` value with a different `credentialsSecretName` than KA's |

### 3.3 API Frontend severity-triage LLM validation (`Describe("API Frontend Severity Triage LLM Validation")`)

| ID     | FedRAMP | Description                                                              |
|--------|---------|---------------------------------------------------------------------------|
| VL-024 | CM-6    | accepts a nil `severityTriage` (defaults to inheriting AF's resolved profile) |
| VL-025 | CM-6    | accepts an empty `severityTriage.llmProfileRef` (inherits AF's resolved profile) |
| VL-026 | CM-6    | rejects `severityTriage.llmProfileRef` naming an undefined profile        |
| VL-027 | CM-6    | accepts `severityTriage.llmProfileRef` sharing AF's resolved profile's `credentialsSecretName` |
| VL-028 | CM-6    | rejects `severityTriage.llmProfileRef` with a different `credentialsSecretName` than AF's resolved profile |
| VL-029 | CM-6    | accepts `llmEnabled: false` regardless of profile-ref validity concerns  |

### 3.4 Resource rendering — KA (`internal/resources/configmaps_test.go`, `deployments_test.go`, `networkpolicies_test.go`)

| ID     | FedRAMP | Description                                                              |
|--------|---------|---------------------------------------------------------------------------|
| CM-001 | CM-6    | `KubernautAgentConfigMap`/`KubernautAgentLLMRuntimeConfigMap` render from KA's resolved profile (provider, model, Vertex/Bedrock/Azure fields) |
| CM-002 | CM-6    | `phaseModels` overrides resolve and render per-phase provider/model/endpoint from the referenced profile, without an `apiKeyFile` (shared-credentials constraint) |
| DP-001 | CM-6    | KA deployment mounts `llm-credentials` from the resolved profile's `credentialsSecretName` |
| DP-002 | SC-8    | KA deployment mounts `oauth2-credentials` when the resolved profile's OAuth2 is enabled |
| NP-001 | SC-7    | KA NetworkPolicy's internet-egress condition resolves from KA's profile provider, not a stale direct field |

### 3.5 Resource rendering — AF's own profile identity (new specs added in Group 2, fixing bug #1 above)

| ID     | FedRAMP | Description                                                              |
|--------|---------|---------------------------------------------------------------------------|
| CM-003 | CM-6    | AF ConfigMap renders AF's own resolved profile when `apiFrontend.llmProfileRef` differs from KA's (not KA's provider/model) |
| CM-004 | CM-6    | AF ConfigMap defaults to KA's resolved profile when `apiFrontend.llmProfileRef` is empty |
| DP-003 | CM-6    | AF deployment mounts `llm-credentials` from AF's *own* resolved profile's Secret, not KA's (regression test for the pre-refactor bug) |
| DP-004 | SC-8    | AF deployment mounts `llm-tls-client` from AF's own resolved profile's `tlsClientSecretRef` |

### 3.6 Resource rendering — AF OAuth2 crash-loop fix (bug #2 above)

| ID     | FedRAMP | Description                                                              |
|--------|---------|---------------------------------------------------------------------------|
| DP-005 | SC-8    | AF deployment mounts an `oauth2-credentials` volume at `/etc/apifrontend/oauth2` when AF's resolved profile has OAuth2 enabled (regression test — this volume did not exist at all before this issue) |
| DP-006 | SC-8    | AF deployment omits the `oauth2-credentials` volume when OAuth2 is not enabled |

### 3.7 API Frontend severity-triage LLM rendering (three states)

| ID     | FedRAMP | Description                                                              |
|--------|---------|---------------------------------------------------------------------------|
| CM-005 | CM-6    | `severityTriage.llm` is omitted by default, so triage inherits AF's `agent.llm` connection (today's behavior, preserved) |
| CM-006 | CM-6    | `severityTriage.llm` is present-but-empty when `llmEnabled: false`, forcing upstream's rule-based-only (Noop) triager |
| CM-007 | CM-6    | `severityTriage.llm` renders an independent profile's provider/model when `llmProfileRef` is set, without leaking into `agent.llm` |

## 4. Acceptance Criteria

- All scenarios above pass (`make test-unit`), plus all pre-existing
  `internal/resources` and `internal/webhook` suites (611+ specs) with
  no regressions.
- `make test-integration` (envtest-backed `internal/controller` suite)
  passes against the updated fixtures.
- `make build` succeeds; `go vet ./...` is clean.
- Manually verified: temporarily reverting the AF OAuth2 volume-mount
  fix causes exactly DP-005 to fail (confirms the regression test has
  real detection power, not just incidental green).
- No remaining direct references to the removed `spec.kubernautAgent.llm`
  field/type outside `internal/resources/*_test.go` history and the
  untouched, out-of-scope `AlignmentCheckLLMSpec`.

# Test Plan: OpenAI Provider Support (Issue #196)

| Field              | Value                                                    |
|--------------------|----------------------------------------------------------|
| **Test Plan ID**   | TP-196                                                   |
| **Feature**        | OpenAI/OpenAI-compatible LLM provider support for AF     |
| **Issue**          | https://github.com/jordigilh/kubernaut-operator/issues/196 |
| **Upstream**       | kubernaut PR #1487, kubernaut #1488 (normalization)      |
| **Author**         | kubernaut-operator team                                  |
| **Date**           | 2026-06-24                                               |

## 1. Introduction

The upstream Kubernaut v1.5.2 release adds an OpenAI-compatible LLM adapter
to the API Frontend (AF). AF distinguishes `openai` (first-party API) from
`openai_compatible` (third-party endpoints like vLLM, LiteLLM, Ollama), while
KA uses `openai` for both via LangChain. AF also expects the endpoint to
include the `/v1` suffix, while KA appends it internally.

The operator must translate `provider: openai` from the CR into the correct
format for each component without changing the CR schema.

## 2. Scope

### In scope

- AF ConfigMap provider name translation (`openai` -> `openai_compatible`)
- AF endpoint `/v1` suffix normalization
- KA ConfigMap passthrough (no translation)
- Validation: require `endpoint` when `provider == "openai"`
- AF `apiKeyFile` wiring for OpenAI provider

### Out of scope

- CR schema changes (deferred to kubernaut#1488 normalization)
- KA ConfigMap generation changes
- New provider additions beyond `openai`/`openai_compatible`

## 3. Test Strategy

Unit tests using Ginkgo/Gomega, following existing patterns in the operator
codebase. Tests verify the operator's internal config generation logic.

## 4. Test Scenarios

### 4.1 ConfigMap Generation (AF)

| ID | Description | FedRAMP | Acceptance Criteria |
|----|-------------|---------|---------------------|
| UT-CM-196-001 | AF receives `openai_compatible` when CR specifies `openai` | SI-10 | `agent.llm.provider` in AF ConfigMap equals `"openai_compatible"` |
| UT-CM-196-002 | AF endpoint gets `/v1` suffix appended | CM-6 | Endpoint `http://gw:8080` becomes `http://gw:8080/v1` in AF config |
| UT-CM-196-003 | AF endpoint not doubled when `/v1` already present | CM-6 | Endpoint `http://gw:8080/v1` stays `http://gw:8080/v1` |
| UT-CM-196-004 | AF endpoint trailing slash handled before `/v1` append | CM-6 | Endpoint `http://gw:8080/` becomes `http://gw:8080/v1` |
| UT-CM-196-005 | KA receives raw `openai` provider, no endpoint mutation | CM-6 | KA llm-runtime.yaml has `provider: openai` and raw endpoint without `/v1` |
| UT-CM-196-006 | Non-OpenAI providers pass through untranslated | CM-6 | `vertex_ai` in CR produces `vertex_ai` in AF config |
| UT-CM-196-007 | AF `apiKeyFile` set for OpenAI provider | SC-7 | `agent.llm.apiKeyFile` is `/etc/apifrontend/llm-credentials/api_key` |

### 4.2 Validation

| ID | Description | FedRAMP | Acceptance Criteria |
|----|-------------|---------|---------------------|
| UT-VL-196-001 | `provider: openai` without endpoint fails validation | SI-10 | Validation returns error mentioning `endpoint` |
| UT-VL-196-002 | `provider: openai` with endpoint passes validation | SI-10 | No validation error for endpoint |

## 5. Acceptance Criteria

- All 9 test scenarios pass
- `go build ./...` clean
- `golangci-lint` clean
- No regressions in existing provider paths (`vertex_ai`, `gemini`, `anthropic`)
- KA ConfigMap generation unchanged
- CR schema unchanged (no `kubernaut_types.go` modifications)

# DD-527: Kubernaut Agent profile and investigation boundaries

**Status:** Implemented

**Issue:** [kubernaut-operator#527](https://github.com/jordigilh/kubernaut-operator/issues/527)

**Scope:** Unreleased `kubernaut.ai/v1alpha2` API

## Decision

The operator's CRD models the logical LLM consumers explicitly. The shared
`spec.llmProfiles` map remains the profile registry, while each consumer owns
an explicit `llmProfileRef`:

```yaml
spec:
  llmProfiles:
    primary: {}
    lightweight: {}

  kubernautAgent:
    investigation:
      llmProfileRef: primary
      maxTurns: 40
    workflowDiscovery:
      llmProfileRef: lightweight
      maxEvidenceHandoffBytes: 262144

  apiFrontend:
    llmProfileRef: primary
```

`workflowDiscovery` is a separate logical Agent invocation with its own
profile and evidence-handoff contract. It is implemented in the same KA
process, but it has a separate phase-scoped tool set, LLM resolution path, and
audit phase. The operator must not hide its identity behind a generic
`phaseModels` map.

The old flat KA fields `llmProfileRef`, `phaseModels`, and `maxTurns` are
removed from the unreleased v1alpha2 API. The operator translates the two
logical-agent references into the upstream `ai.llm` and runtime
`phaseModels.workflow_discovery` representation internally.

`apiFrontend.llmProfileRef` remains an independent consumer reference. When
multiple profiles exist, API Frontend does not silently inherit the
investigation profile. Sole-profile inference remains available when exactly
one profile is defined.

The upstream KA currently exposes one `ai.investigation.maxTurns` limit for
both LLM invocations. Issue #527 preserves that shared runtime behavior rather
than inventing an unsupported second turn-limit field.

## Alternatives rejected

1. **Add only a flat `maxEvidenceHandoffBytes` field** — preserves the
   existing structural drift and leaves logical Agent ownership implicit.
2. **Add a generic `kubernautAgent.llm` section** — makes profile ownership
   indirect even though the profile is consumed by a specific logical Agent.
3. **Keep `phaseModels` as the public CRD API** — exposes upstream runtime
   implementation details and permits profile references for phases that are
   not independent logical Agents.

## Security and business objectives

The design provides explicit, reviewable configuration ownership and prevents
an API Frontend profile from changing implicitly when the investigation model
changes. Evidence covers configuration management and validation objectives:
FedRAMP/NIST CM-2, CM-3, CM-6, SI-10, and SA-11; SOC 2 CC6.6 and CC8.1; and
versioned OWASP ASVS 5.0.0 input-validation, configuration-integrity, and
verification objectives.

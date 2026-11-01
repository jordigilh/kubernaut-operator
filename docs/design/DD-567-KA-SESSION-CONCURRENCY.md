# DD-567: Kubernaut Agent investigation concurrency

**Status:** Implemented and verified

**Issue:** [kubernaut-operator#567](https://github.com/jordigilh/kubernaut-operator/issues/567)

**Scope:** Unreleased `kubernaut.ai/v1alpha2` API

## Decision

Expose the Kubernaut Agent investigation admission capacity under the existing
session configuration:

```yaml
spec:
  kubernautAgent:
    session:
      maxConcurrentInvestigations: 50
```

The field is a plain `int` with an OpenAPI default of `50` and a minimum of
`1`. The operator renders the value as
`runtime.session.maxConcurrentInvestigations` in the existing
`kubernaut-agent-config` ConfigMap. The renderer uses the same effective
default for zero-value in-memory fixtures; the public CRD contract rejects
zero and negative values before reconciliation.

No upper bound is introduced. The pinned Kubernaut Agent validates the value
as positive, and the issue establishes `50` as the operator/Helm profile
default rather than an application-level limit.

## Rationale

The upstream Agent configuration defines `SessionConfig.MaxConcurrentInvestigations`
as an integer under `runtime.session`. Its binary default is `10`, while the
pinned Helm profile intentionally raises the deployment default to `50` for
parallel autonomous and interactive investigations. Keeping the same nesting
and spelling avoids a second translation contract and lets an administrator
override the capacity through the operator CRD.

v1alpha2 has not been released. No compatibility alias, migration, conversion
webhook, or pointer-backed omitted-state behavior is required.

## Alternatives rejected

1. **Pointer-backed integer** — unnecessary for the unreleased v1alpha2 API;
   CRD defaulting and minimum validation define the public contract.
2. **New top-level Agent field** — diverges from the upstream runtime grouping
   and makes session ownership less clear.
3. **Hardcoded renderer-only default** — does not expose the administrator
   override required by issue #567.

## Security and business objectives

The implementation provides engineering evidence for configuration management,
positive input validation, controlled change propagation, and observable
operator wiring. Planned evidence maps to FedRAMP/NIST CM-2, CM-3, CM-6,
SA-11, SI-4; SOC 2 CC6.6, CC7.2, CC8.1; and OWASP ASVS 5.0.0 V2.2.1,
V2.2.2, V2.2.3, V13.3.1, V14.2.4, and V16.5.2.

These mappings are engineering evidence only; they do not claim formal
FedRAMP authorization, SOC 2 attestation, or OWASP ASVS conformance.

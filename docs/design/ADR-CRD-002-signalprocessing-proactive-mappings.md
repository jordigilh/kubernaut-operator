# ADR-CRD-002: Keep Signal Processing proactive mappings out of the operator CRD

**Status:** Accepted  
**Date:** 2026-10-17  
**Scope:** Signal Processing only

## Context

`spec.signalProcessing.proactiveSignalMappings` was introduced for an older
alert/model contract. The current model contract treats the alert name as
sufficient context for inferring the signal's purpose. The related handling
may be deprecated in Kubernaut Agent later.

## Decision

The operator does not expose a user-configurable proactive-mappings field in
the `Kubernaut` CRD. The operator continues to render its built-in mappings
ConfigMap for compatibility with the current Signal Processing runtime until
the runtime/KA handling is retired.

The Helm chart may retain its legacy field temporarily, but it is not part of
the operator contract and must not be added back to the CRD.

## Alternatives considered

1. **Keep the CRD override:** rejected because it preserves a legacy public
   contract that is expected to be removed with the runtime handling.
2. **Remove the mappings runtime behavior immediately:** deferred; runtime
   and KA cleanup is a separate change.
3. **Remove the CRD field while retaining the built-in default:** accepted;
   this decouples future runtime removal from a new CRD exposure.

## Consequences

- Existing CRs using the field are no longer part of the supported operator
  contract and will not receive custom mappings.
- The generated CRD, deepcopy code, controller precreated-reference path, and
  custom-override tests/docs must not contain this field.
- The built-in default ConfigMap remains until the runtime deprecation work is
  completed.

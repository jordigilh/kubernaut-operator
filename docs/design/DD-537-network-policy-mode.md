# DD-537: make native network isolation implicit

## Context

The operator's native network-policy contract is fail-closed: migration and
workload activation require a positively identified, supported provider and
provider-native policy objects. `Required` was the only supported value of
`spec.networkPolicies.mode`; a weaker mode was rejected and there was no safe
best-effort alternative.

Exposing a field with one valid value suggested that administrators could
choose a different enforcement contract when they could not. That ambiguity
was especially harmful during upgrades because an old manifest could appear to
request a weaker policy while the operator still had to fail closed.

## Options considered

1. **Keep `mode` and deprecate it** — preserves manifest compatibility, but
   keeps a misleading single-value API and requires continuing to validate and
   document a redundant knob.
2. **Keep `mode` and accept only `Required`** — makes the contract explicit in
   validation, but still exposes the same redundant choice and retains a
   misleading user-facing weaker-mode concept.
3. **Remove `mode` and make fail-closed native isolation implicit** — gives the
   CRD one unambiguous contract; manifests select only the provider and bounded
   traffic identities that customize that contract.

## Decision

Choose option 3. Remove `NetworkPoliciesSpec.Mode` and the associated
`NetworkIsolationRequired` constant. Provider readiness, dependency readiness,
and native policy reconciliation remain mandatory; no raw `NetworkPolicy`
fallback or administrator-owned isolation bypass is introduced.

This is an intentional breaking change for v1alpha2 manifests that still set
`spec.networkPolicies.mode`. Such manifests must remove the field before
applying the updated CRD.

## Consequences

- Generated CRD, bundle, chart, installer, samples, and documentation no
  longer expose `mode` or a weaker-mode option.
- Controller watches and policy validation no longer branch on a mode value.
- Existing provider, dependency, TLS, ownership, and fail-closed tests continue
  to exercise the implicit contract without setting a mode.

# IEEE 829 Test Plan — Issue #476: Console UX release and raw-thinking policy

## 1. Scope

Issue #476 consumes the `kubernaut-console` `1.6.0-rc23` release and makes
the operator-generated Console hide raw LLM thinking by default. An explicit
`spec.console.enableRawThinking: true` remains available for administrator
diagnostics. The CRD change is v1alpha2-only.

This plan verifies business behavior, production wiring, and reproducible
release artifacts. It does not add tests solely to increase line coverage.
The explicit diagnostic override is deployment-wide; per-user authorization is
outside the operator's scope and is not claimed by this plan.

## 2. Control objectives

| Objective | Business behavior | Evidence |
|---|---|---|
| FedRAMP/NIST CM-3, CM-6, SI-10 | The Console configuration baseline is explicit, defaults safely, and validates the administrator's boolean policy at the trusted operator layer. | `UT-CN-476-001`–`003`; `IT-CN-476-001`; generated CRD |
| SOC 2 CC8.1 | A changed Console UX/configuration baseline is documented, reviewable, and reproducible across install artifacts. | `IT-CN-476-001`; `CI-CN-476-001`; changelog and generated-artifact checks |
| OWASP ASVS v5.0.0 V2.1.1, V2.2.1, V2.2.2 | The field's accepted structure and default are documented, validated in the CRD, and enforced by the trusted operator rather than relying on client-side behavior. | `UT-CN-476-001`–`003`; `IT-CN-476-001`; CRD schema |
| OWASP ASVS v5.0.0 V1.2.3 | The generated JavaScript policy contains only the operator-controlled boolean and is tested as the browser runtime configuration. | `UT-CN-476-001`–`003`; `UT-CN-314-004`; `IT-CN-476-001`; `IT-CN-476-002` |
| OWASP ASVS v5.0.0 V15.2.4 | The Console dependency is sourced from the expected Quay repository and pinned consistently by immutable digest. | `CI-CN-476-001`; generated artifact review |

These mappings are engineering objectives, not formal FedRAMP authorization,
SOC 2 attestation, or OWASP ASVS conformance claims.

## 3. Test pyramid

### Unit

- `UT-CN-476-001`: omitted field renders `enableRawThinking: false`.
- `UT-CN-476-002`: explicit `false` remains disabled.
- `UT-CN-476-003`: explicit `true` enables the diagnostic override.
- Existing `UT-CN-314-004`: runtime script remains JavaScript and uncached.

### Integration

- `IT-CN-476-001`: real envtest reconciliation with Console enabled creates
  the nginx ConfigMap and verifies its rendered runtime policy is false by
  default.
- `IT-CN-476-002`: changing the policy from the default to `true` and back to
  `false` changes the ConfigMap hash on the Console pod template, forcing a
  rollout for the `subPath`-mounted nginx configuration.

### E2E / release artifacts

- `CI-CN-476-001`: generated CRD, bundle, installer, manager manifest, Helm
  values, and air-gapped image metadata contain the v1alpha2 field and the
  same rc23 Console digest.
- The existing Kind journey remains the installed-operator contract gate;
  Console is optional and disabled in its dependency-light contract fixture,
  so no separate live UI test is added for this configuration-only change.

## 4. Exit criteria

- Unit and integration tests pass when envtest binaries are available.
- `make manifests generate bundle build-installer` is reproducible.
- No stale `1.1.0` Console digest remains in active install artifacts.
- Unit coverage remains at least the project 90% threshold without synthetic
  tests.

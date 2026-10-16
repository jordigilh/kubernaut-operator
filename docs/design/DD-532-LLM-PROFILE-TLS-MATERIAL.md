# DD-532: Operator-owned LLM profile TLS material

**Status:** Approved
**Issue:** [kubernaut-operator#532](https://github.com/jordigilh/kubernaut-operator/issues/532)
**Related:** operator #419 / upstream kubernaut #2515

## Decision

Extend `LLMProfileSpec` with an optional `tlsCaSecretRef` of type
`*CACertSecretRef`:

```go
TLSCaSecretRef *CACertSecretRef `json:"tlsCaSecretRef,omitempty"`
```

The referenced administrator-owned Secret contains a PEM CA bundle under
`ca.crt` by default, or the explicitly selected key. `tlsCaFile` remains the
runtime path rendered into the Kubernaut Agent configuration.

The operator supports three CA modes:

1. **System trust:** no `tlsCaFile` and no `tlsCaSecretRef`; no LLM CA volume.
2. **Administrator-managed external path:** `tlsCaFile` without
   `tlsCaSecretRef`; the operator validates path shape only and does not claim
   that an external file exists.
3. **Operator-mounted CA:** `tlsCaFile` with `tlsCaSecretRef`; the operator
   validates the Secret before deployment, projects only the selected key into
   a read-only deterministic profile mount, and renders the matching path.

`tlsClientSecretRef` remains the source of mTLS `tls.crt` and `tls.key`. When
the CA Secret and client Secret are both selected, the Deployment uses one
read-only projected material volume for the effective profile so the rendered
CA/cert/key paths cannot diverge from the mounted files.

Investigation, workflow discovery, and alignment each resolve their own
complete profile. A sibling may reuse the investigation material only when its
effective material identity is intentionally the same; otherwise it receives
only its own CA/client Secret projections.

## Alternatives considered

### A. Reuse existing provider/client Secrets — rejected

Putting CA data in `credentialsSecretName` or implicitly requiring `ca.crt` in
`tlsClientSecretRef` avoids a CRD field, but couples provider credentials and
trust roots and leaves CA-only profiles without an explicit source. It weakens
least-privilege and makes Secret rotation/validation contracts ambiguous.

### B. Keep all TLS paths external — rejected

This matches the current upstream Helm chart, which renders TLS paths but does
not provision LLM TLS material. It does not satisfy #532's requirement that
profile-owned CA/client material be mounted and path-aligned.

### C. Add a new nested TLS material object — rejected

A larger nested API could model all sources and paths, but duplicates the
existing fields and expands the v1alpha2 surface beyond the issue. The narrow
optional CA reference is sufficient.

## Security evidence boundary

The implementation provides engineering evidence aligned with FedRAMP/NIST
`SC-8`, `SC-12`, `AC-6`, `IA-5`, `SI-10`, and `CM-6`; SOC 2 `CC6.1`, `CC6.6`,
`CC7.2`, `CC8.1`; and OWASP ASVS 5.0.0 `V2.2.1–V2.2.2`, `V8.2.1`,
`V8.3.1`, `V12.1.1`, `V12.3.1–V12.3.5`, `V13.2.2`, `V13.3.1–V13.3.4`, and
`V14.2.4`. It is not a formal authorization, attestation, or conformance
claim.

Secret contents must never appear in ConfigMaps, status, annotations, errors,
or logs. Administrator-owned Secrets are read-only inputs and are never
adopted, mutated, rotated, or deleted by the operator.

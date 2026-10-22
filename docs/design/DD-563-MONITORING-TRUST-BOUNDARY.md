# DD-563 — Monitoring client trust boundary

| Field | Value |
|---|---|
| Issue | [#563](https://github.com/jordigilh/kubernaut-operator/issues/563) |
| Status | Proposed; approval required before implementation |
| API line | `kubernaut.ai/v1alpha2` (unreleased clean break) |
| Persisted test plan | [`docs/tests/563/TEST_PLAN.md`](../tests/563/TEST_PLAN.md) |

## Problem

The current monitoring TLS contract selects `interService`, `system`,
`secret`, or `configMap`. Its omitted/default behavior selects Kubernaut's
internal service CA, coupling external Prometheus and Alertmanager trust to an
operator-owned lifecycle. Monitoring infrastructure may be platform- or
administrator-owned, live in another namespace, or live in another cluster.

Serving-certificate provisioning and monitoring client trust are separate
concerns and must not be coupled.

## Decision

The monitoring client trust contract will contain one optional field:

```yaml
spec:
  monitoring:
    tls:
      caCertSecretRef:
        name: monitoring-ca
        key: ca.crt
```

- A populated `caCertSecretRef` selects an administrator-owned, namespace-local
  Secret. The operator validates the selected PEM CA before deployment and
  mounts it read-only at `/etc/monitoring-tls/ca.crt` for every monitoring
  consumer.
- An omitted reference uses the application/container system trust store. It
  creates no monitoring CA volume, monitoring CA file configuration, or
  monitoring-material revision annotation.
- Remove `source`, `MonitoringTLSSource`, `interService`, `system`,
  `configMap`, and `caCertConfigMapRef` from the public v1alpha2 contract.
- Preserve internal service-to-service CA mounts and configuration for internal
  clients. They are not a monitoring trust source.
- Keep `spec.tls` serving-certificate provisioning unchanged.
- Because v1alpha2 is unreleased, provide no compatibility alias or deprecation
  migration for the removed selector API.

## Alternatives considered

| Option | Advantages | Disadvantages | Decision |
|---|---|---|---|
| Retain/deprecate `source` and `configMap` | Allows a staged migration and a controller error for old values. | Preserves redundant API surface and the lifecycle coupling the issue is intended to remove. | Rejected. |
| Secret-only optional trust | Smallest API, explicit administrator ownership, system trust is the natural zero-value, and no internal-CA fallback. | Existing unpublished fixtures and generated artifacts must change together. | **Selected.** |
| Raw admission guard for removed fields | Can produce a custom error before unknown fields are pruned. | Adds webhook/raw-payload complexity for an API line explicitly approved for a clean break. | Rejected unless the acceptance criterion requires a controller-independent legacy-field message. |

## Failure and rotation behavior

- Missing/empty Secret name or invalid key fails validation with the field path.
- Missing Secret, missing key, malformed PEM, non-CA, expired, or otherwise
  invalid material sets the existing monitoring validation failure condition and
  prevents a new Deployment template from replacing the last-known-good one.
- Valid Secret resource-version changes produce a non-sensitive monitoring
  material revision and roll EffectivenessMonitor, Kubernaut Agent, and API
  Frontend together.
- The operator never copies, mutates, or owns the administrator-provided CA
  Secret.

## Removed-source interpretation

The generated CRD will no longer publish `source`. Strict Kubernetes field
validation is the boundary for old manifests that submit the removed field;
the reconciler cannot report a value that the API server rejects or prunes.
Reconciliation remains fail-closed for all invalid states representable by the
new API, especially invalid Secret material and invalid rotation.

If a controller-generated error for a raw legacy `source` field is mandatory,
the API must retain a compatibility field or add a raw admission guard. That
would supersede this clean-break decision and requires explicit approval.

## Security objective mapping

This decision provides implementation evidence for, but does not claim formal
certification against:

- FedRAMP/NIST: AC-6, CM-3, CM-6, SC-8, SC-12, SC-13, SC-17, SI-4, SI-10.
- SOC 2: CC6.1, CC7.2, CC8.1.
- OWASP ASVS 5.0.0: V2.2.1, V2.2.2, V12.3.1, V12.3.2, V13.3.1,
  V13.3.2, V13.3.4, V16.5.2.

The persisted business test evidence is defined in
[`docs/tests/563/TEST_PLAN.md`](../tests/563/TEST_PLAN.md).

# Issue #567 — Kubernaut Agent investigation concurrency

| Field | Value |
|---|---|
| Issue | [#567](https://github.com/jordigilh/kubernaut-operator/issues/567) |
| Release line | Unreleased v1alpha2 |
| Status | Implemented and verified |
| Decision | [`DD-567-KA-SESSION-CONCURRENCY.md`](DD-567-KA-SESSION-CONCURRENCY.md) |

## Objective

Expose `spec.kubernautAgent.session.maxConcurrentInvestigations`, default it
to `50` with a minimum of `1`, and render it as
`runtime.session.maxConcurrentInvestigations` in the operator-owned
`kubernaut-agent-config` ConfigMap. The value must reach the Agent through the
existing reconcile and read-only Deployment mount path.

The pinned Agent binary defaults to `10`; the upstream Helm qualification
profile defaults to `50`. No arbitrary upper bound is added.

## TDD and wiring plan

### RED

- `UT-KA-567-API-001` verifies the CRD type, default, minimum, and optional
  field shape.
- `UT-KA-567-CM-001` verifies the 50 default in rendered YAML.
- `UT-KA-567-CM-002` verifies an explicit positive override.
- `IT-KA-567-RECONCILE-001` verifies the default through envtest reconciliation.
- `IT-KA-567-RECONCILE-002` verifies the override and read-only ConfigMap mount.
- `E2E-KA-567-001` verifies the live Kind contract without installing the
  upstream application Helm chart.

### GREEN

- Add the plain integer field to `SessionSpec` with controller-gen default and
  minimum markers.
- Extend the existing `kaSessionYAML` and `KubernautAgentConfigMap` renderer.
- Keep the existing `buildCoreConfigMaps` production caller; no new builder,
  phase, or RBAC rule is required.

### REFACTOR and verification

- Regenerate CRD, bundle, installer, and embedded Helm CRD artifacts.
- Update samples, config-field mapping, and control traceability.
- Run the Makefile tiers and inspect generated diffs.

## Wiring manifest

| Component | Production entry point | Wiring location | Evidence |
|---|---|---|---|
| CRD field | Kubernetes admission | `api/v1alpha2/kubernaut_types.go` | `UT-KA-567-API-001` |
| ConfigMap rendering | `KubernautAgentConfigMap` | `internal/resources/configmaps.go` | `UT-KA-567-CM-001/002` |
| Reconciled ConfigMap | `buildCoreConfigMaps` | `internal/controller/kubernaut_controller.go` | `IT-KA-567-RECONCILE-001/002` |
| Live Agent contract | Existing Agent Deployment | `internal/resources/deployments.go`, Kind suite | `E2E-KA-567-001` |

## Control-objective evidence

The tests are business-level evidence for configuration management, positive
input validation, controlled change propagation, and workload wiring:

| Behavior | FedRAMP/NIST | SOC 2 | OWASP ASVS 5.0.0 |
|---|---|---|---|
| Default and minimum are published in the CRD | CM-6, SI-10 | CC6.6, CC8.1 | V2.2.1, V2.2.2, V2.2.3 |
| Default and override reach Agent YAML | CM-2, CM-3, CM-6 | CC6.6, CC8.1 | V2.2.1, V14.2.4, V16.5.2 |
| Reconcile persists the owned read-only ConfigMap contract | CM-3, SA-11, SI-4 | CC7.2, CC8.1 | V13.3.1, V16.5.2 |

This is engineering evidence, not a formal FedRAMP authorization, SOC 2
attestation, or OWASP ASVS conformance claim. FullPipeline throughput remains
upstream application qualification evidence.

## Gates

```bash
go build ./...
golangci-lint run
make manifests generate
make test-unit
make test-integration
make test-security-traceability
make test-pyramid
make test-e2e-kind
```

Changed resource logic must meet at least 90% unit coverage, with tests added
for business cases rather than coverage alone. The independent business
coverage gate must report zero missed handwritten statements.

## Confidence

**96%.** The runtime contract is confirmed at the pinned upstream revision,
the existing operator path already owns and mounts the ConfigMap, and the clean
baseline build/unit/security gates pass. Remaining risk is limited to
generated-artifact synchronization and live Kind availability.

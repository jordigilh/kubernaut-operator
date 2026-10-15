# Issue #480 — WorkflowExecution failed-resource retention

## Decision

Expose bounded failed-WorkflowExecution retention through the clean-break
`kubernaut.ai/v1alpha2` API only. The repository serves and stores only
v1alpha2; v1alpha1 conversion is not restored. Existing v1alpha1 users must
follow the export/transform/delete/recreate procedure in
[`docs/upgrade-v1alpha1-to-v1alpha2.md`](../upgrade-v1alpha1-to-v1alpha2.md).

## API contract

`spec.workflowExecution` gains:

| Field | Type | Default | Bounds |
|---|---|---:|---:|
| `retainFailedExecutions` | boolean | `false` | opt-in |
| `failedExecutionRetentionSeconds` | integer | `600` | `0`–`604800` |

When retention is enabled, the period must be greater than zero. The operator
renders both values under `execution` in `workflowexecution.yaml`, matching the
pinned WorkflowExecution service contract.

## Wiring manifest

| Component | Production entry point | Wiring location | Evidence |
|---|---|---|---|
| v1alpha2 retention fields | `KubernautReconciler.Reconcile` | `internal/controller/kubernaut_controller.go` → `WorkflowExecutionConfigMap` | `IT-WE-480-001` |
| ConfigMap rendering | `WorkflowExecutionConfigMap` | `internal/resources/configmaps.go` | `UT-WE-480-CM-001`, `UT-WE-480-CM-002` |
| CRD defaults and bounds | Kubernetes API admission | `config/crd/bases/kubernaut.ai_kubernauts.yaml` | v1alpha2 clean-break contract test |

## TDD and verification

### RED

- Add unit tests for disabled/default and enabled/custom rendering.
- Add a CRD schema contract test for defaults and bounds.
- Add envtest coverage proving CR → reconciliation → ConfigMap propagation.

### GREEN

- Add the v1alpha2 fields and validation.
- Render the fields in the existing WorkflowExecution ConfigMap builder.
- Regenerate deepcopy and CRD artifacts.

### REFACTOR

- Synchronize samples, API field-mapping documentation, bundle, dist, and Helm
  CRD parity fixtures.
- Run the full unit/integration/pyramid verification commands.

## Deployment consumer boundary

The operator repository does not contain `hack/setup-demo-infra`, fullpipeline,
or must-gather installer consumers named in the issue. Those consumers should
set `retainFailedExecutions=true` explicitly in their owning deployment
repository. Production/default operator behavior remains disabled.

## Control-objective evidence

The test assertions are business-level checks for configuration management,
safe defaults, bounded input, and reconciler propagation. They map to
FedRAMP/NIST CM-3, CM-6, SI-10, and the upstream service's AU-11 diagnostic
retention boundary; SOC 2 CC6.6, CC7.2, and CC8.1; and ASVS 5.0.0 V2.1.1,
V2.1.3, V2.2.1, V2.2.2, and V2.2.3. The accompanying matrix is engineering
evidence only; it is not a FedRAMP authorization, SOC 2 attestation, or OWASP
ASVS certification.

# Issue #480 control traceability

This matrix records repository-level engineering evidence for bounded failed
WorkflowExecution resource retention using NIST SP 800-53 Rev. 5/FedRAMP
control objectives and OWASP ASVS 5.0.0 requirement identifiers. It does not claim formal FedRAMP authorization, formal SOC 2 attestation, or formal OWASP ASVS conformance.

The ASVS identifiers and requirement summaries use the official v5.0.0 source:
<https://raw.githubusercontent.com/OWASP/ASVS/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv>.

### ASVS requirement summaries

| Requirement | Official requirement summary | Issue #480 application |
|---|---|---|
| `v5.0.0-V2.1.1` | Document input-validation rules for expected data structures and values. | The implementation plan and CRD describe defaults, bounds, and the enabled/period relationship. |
| `v5.0.0-V2.1.3` | Document business-logic limits and validation expectations. | The seven-day maximum and positive-period requirement are documented and schema-enforced. |
| `v5.0.0-V2.2.1` | Validate input against positive ranges, allowlists, expected structures, and logical limits. | Kubernetes CRD validation enforces the retention range and default contract. |
| `v5.0.0-V2.2.2` | Enforce validation at a trusted service layer rather than relying on client-side checks. | The API server admission schema and reconciler enforce the operator contract. |
| `v5.0.0-V2.2.3` | Validate that combinations of related data items are reasonable. | CEL rejects a non-positive period when retention is enabled. |

| Business behavior | Production evidence | Test evidence | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 objective | Status |
|---|---|---|---|---|---|---|
| Failed execution retention is disabled unless explicitly enabled | `WorkflowExecutionSpec` default and `WorkflowExecutionConfigMap` rendering | `UT-WE-480-CM-001`, `IT-WE-480-001` | CM-6, SI-10 | CC6.6, CC8.1 | `v5.0.0-V2.1.1` documented validation rules; `v5.0.0-V2.2.1` input validation | verified |
| Diagnostic retention period is explicit and bounded | CRD `Minimum=0`, `Maximum=604800`, default `600`; upstream-compatible YAML mapping | `UT-WE-480-API-001`, `UT-WE-480-CM-002`, `IT-WE-480-ADMISSION-002`, `IT-WE-480-ADMISSION-003`, `IT-WE-480-ADMISSION-004` | CM-3, CM-6, SI-10 | CC8.1 | `v5.0.0-V2.1.3` documented business limits; `v5.0.0-V2.2.1` range validation | verified |
| Invalid enabled retention is rejected before reconciliation | WorkflowExecutionSpec CEL validation for related fields | `IT-WE-480-ADMISSION-001` | CM-6, SI-10 | CC8.1 | `v5.0.0-V2.2.3` related-input consistency | verified |
| Approved CR configuration reaches the live WorkflowExecution controller | Existing reconciler dispatch calls `WorkflowExecutionConfigMap` | `IT-WE-480-001` | CM-3, CM-6 | CC7.2, CC8.1 | `v5.0.0-V2.2.2` trusted service-layer validation | verified |
| Failed Job/PipelineRun/AWX resources remain available only for the configured bound | Upstream WorkflowExecution consumes `execution.retainFailedExecutions` and `execution.failedExecutionRetentionSeconds`; upstream labels this diagnostic-retention objective FedRAMP AU-11 | Operator integration evidence; upstream service tests remain the service-owner evidence | SI-4, SI-10; AU-11 boundary is partially verified | CC7.2 | Not directly applicable to this operator configuration contract | partially verified |

## Evidence boundary

The operator verifies schema, defaulting, rendering, and reconciliation
propagation. Runtime deletion/retention behavior is implemented and tested by
the pinned WorkflowExecution service. The upstream service labels the feature
with AU-11, but the operator does not persist audit records or own audit-log
retention; AU-11 is therefore only partially evidenced here. Cluster log
retention, must-gather collection, administrator access, and formal control
assessments remain outside the operator repository.

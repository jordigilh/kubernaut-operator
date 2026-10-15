# Issue #527 control traceability

This matrix records engineering evidence for the operator configuration
contract using NIST SP 800-53 Rev. 5 / FedRAMP control objectives. It is not a
formal FedRAMP authorization, SOC 2 attestation, or OWASP ASVS conformance
claim.

| Business behavior | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 objective | Evidence |
|---|---|---|---|---|
| Investigation and workflow-discovery profiles are explicit, independently validated references into `spec.llmProfiles`. | CM-6, SI-10 | CC6.6, CC8.1 | V2.2.1, V2.2.2 | `UT-KA-527-API-001`, `UT-KA-527-VAL-001`, `UT-KA-527-VAL-002` |
| Omitted workflow-discovery evidence budget renders the approved 262144-byte baseline. | CM-2, CM-6 | CC8.1 | V14.2.4, V16.5.2 | `UT-KA-527-CM-001`, reconciliation evidence |
| An explicit administrator budget override is preserved without silent replacement. | CM-3, CM-6 | CC8.1 | V14.2.4, V16.5.2 | `UT-KA-527-CM-002`, reconciliation evidence |
| The generated KA ConfigMap reaches the deployed Agent through the owned read-only ConfigMap mount. | CM-3, SA-11 | CC8.1 | V13.3.1, V16.5.2 | `UT-KA-527-RECONCILE-001`, Deployment/resource tests |
| API Frontend retains an independent LLM profile reference rather than inheriting a changed KA investigation identity when profiles are ambiguous. | CM-6, SI-10 | CC6.6, CC8.1 | V2.2.1, V16.5.2 | `UT-KA-527-VAL-002`, AF rendering tests |

The full workflow-discovery scenario using sanitized evidence between 64KiB and
256KiB is upstream/demo qualification evidence, not an operator-only unit
test.

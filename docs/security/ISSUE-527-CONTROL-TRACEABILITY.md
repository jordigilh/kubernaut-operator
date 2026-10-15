# Issue #527 control traceability

This matrix records engineering evidence for the operator configuration
contract using NIST SP 800-53 Rev. 5 / FedRAMP control objectives. It is not a
formal FedRAMP authorization, SOC 2 attestation, or OWASP ASVS conformance
claim.

Authoritative references:

- [NIST SP 800-53 Rev. 5 Update 1](https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final)
- [FedRAMP Rev. 5 control reference](https://www.fedramp.gov/2026/reference/controls/)
- [OWASP ASVS 5.0.0 requirement catalog](https://raw.githubusercontent.com/OWASP/ASVS/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv)

| Business behavior | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 objective | Evidence | Status |
|---|---|---|---|---|---|
| Investigation and workflow-discovery profiles are explicit, independently validated references into `spec.llmProfiles`. | CM-6, SI-10 | CC6.6, CC8.1 | `v5.0.0-V2.2.1`, `v5.0.0-V2.2.2` | `UT-KA-527-API-001`, `UT-KA-527-VAL-001`, `UT-KA-527-VAL-002` | verified locally |
| Omitted workflow-discovery evidence budget renders the approved 262144-byte baseline. | CM-2, CM-6 | CC8.1 | `v5.0.0-V14.2.4`, `v5.0.0-V16.5.2` | `UT-KA-527-CM-001`, `IT-KA-527-RECONCILE-001`, `E2E-KA-527-001` | verified locally; live Kind contract requires CI execution |
| An explicit administrator budget override is preserved without silent replacement. | CM-3, CM-6 | CC8.1 | `v5.0.0-V14.2.4`, `v5.0.0-V16.5.2` | `UT-KA-527-CM-002`, `IT-KA-527-RECONCILE-001`, `E2E-KA-527-001` | verified locally; live Kind contract requires CI execution |
| The generated KA ConfigMap reaches the deployed Agent through the owned read-only ConfigMap mount. | CM-3, SA-11 | CC8.1 | `v5.0.0-V13.3.1`, `v5.0.0-V16.5.2` | `IT-KA-527-RECONCILE-001`, `E2E-KA-527-001`, Deployment/resource tests | verified locally; live Kind contract requires CI execution |
| API Frontend retains an independent LLM profile reference rather than inheriting a changed KA investigation identity when profiles are ambiguous. | CM-6, SI-10 | CC6.6, CC8.1 | `v5.0.0-V2.2.1`, `v5.0.0-V16.5.2` | `UT-KA-527-VAL-002`, AF rendering tests | verified locally |

The full workflow-discovery scenario using sanitized evidence between 64KiB and
256KiB remains upstream/demo qualification evidence, not an operator-only unit
test. `E2E-KA-527-001` proves the operator's live configuration and
reconciliation contract; application-level workflow selection remains outside
the operator repository's CI boundary.

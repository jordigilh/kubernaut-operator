# Issue #567 control traceability

This matrix records engineering evidence for the Kubernaut Agent investigation
concurrency configuration contract using NIST SP 800-53 Rev. 5 / FedRAMP
control objectives, SOC 2 objectives, and OWASP ASVS 5.0.0 references. It is
not a formal FedRAMP authorization, SOC 2 attestation, or OWASP ASVS
conformance claim.

Authoritative references:

- [NIST SP 800-53 Rev. 5 Update 1](https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final)
- [FedRAMP Rev. 5 control reference](https://www.fedramp.gov/2026/reference/controls/)
- [OWASP ASVS 5.0.0 requirement catalog](https://raw.githubusercontent.com/OWASP/ASVS/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv)

| Business behavior | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 objective | Evidence | Status |
|---|---|---|---|---|---|
| The v1alpha2 API publishes an optional positive integer capacity with a default of 50. | CM-6, SI-10 | CC6.6, CC8.1 | `v5.0.0-V2.2.1`, `v5.0.0-V2.2.2`, `v5.0.0-V2.2.3` | `UT-KA-567-API-001`; `api/v1alpha2/kubernaut_types.go`; generated CRD | verified locally |
| An omitted capacity renders the approved 50-investigation baseline instead of the Agent binary default of 10. | CM-2, CM-6 | CC8.1 | `v5.0.0-V14.2.4`, `v5.0.0-V16.5.2` | `UT-KA-567-CM-001`; `IT-KA-567-RECONCILE-001`; `E2E-KA-567-001` | verified locally and in the final Kind run |
| An explicit positive administrator override is preserved through ConfigMap rendering and reconciliation. | CM-3, CM-6 | CC6.6, CC8.1 | `v5.0.0-V2.2.1`, `v5.0.0-V14.2.4` | `UT-KA-567-CM-002`; `IT-KA-567-RECONCILE-002`; `E2E-KA-567-001` | verified locally and in the final Kind run |
| The generated KA ConfigMap reaches the deployed Agent through the existing owned read-only mount. | CM-3, SA-11, SI-4 | CC7.2, CC8.1 | `v5.0.0-V13.3.1`, `v5.0.0-V16.5.2` | `IT-KA-567-RECONCILE-002`; `E2E-KA-567-001`; `internal/controller/kubernaut_controller.go` | verified locally and in the final Kind run |

The operator proves configuration and reconciliation behavior only. FullPipeline
investigation throughput and application-level concurrency behavior remain
upstream qualification evidence.

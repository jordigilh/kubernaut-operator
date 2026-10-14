# Issue #209 — Audit HMAC control traceability

**Scope:** Operator-managed DataStorage audit HMAC provisioning and wiring  
**Status:** Engineering evidence; not a formal FedRAMP, SOC 2, or OWASP ASVS certification claim  
**Related:** [operator#209](https://github.com/jordigilh/kubernaut-operator/issues/209), [upstream#2509](https://github.com/jordigilh/kubernaut/issues/2509)

## Control boundary

The operator makes HMAC configuration mandatory for its deployment vector. It
generates a stable 256-bit key unless an administrator-managed Secret is
referenced. Historical unkeyed rows remain an upstream DataStorage compatibility
responsibility; new operator-managed writes receive HMAC configuration.

Authoritative references:

- NIST SP 800-53 Rev. 5: <https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final>
- FedRAMP Rev. 5 guidance: <https://www.fedramp.gov/rev5/documents-templates/>
- OWASP ASVS v5.0.0 catalog: <https://raw.githubusercontent.com/OWASP/ASVS/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv>

| Business behavior | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS v5.0.0 objective | Evidence | Status |
|---|---|---|---|---|---|
| Audit HMAC configuration is always rendered and validated before migration/deployment | `AU-9`, `SI-10`, `CM-6` | `CC7.2`, `CC8.1` | `v5.0.0-V16.5.2` | `UT-AUDIT-209-CM-001`; `IT-AUDIT-209-INT-001/003/004/007`; `internal/resources/configmaps.go`; controller validation | verified locally |
| Generated key uses CSPRNG, is immutable, fingerprint-bound, and protected from ordinary deletion | `AU-9`, `IA-5`, `CP-10` | `CC8.1` | `v5.0.0-V11.1.1`, `v5.0.0-V13.3.1` | `UT-AUDIT-209-RES-001/003/010/011`; `UT-AUDIT-209-CTRL-001/002/005/013/015/016/017/018/020`; Secret/state finalizers | verified locally |
| Administrator Secret is read-only, validated, and never adopted or mutated | `AC-3`, `AC-6`, `IA-5` | `CC6.6` | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1`, `v5.0.0-V13.3.1`, `v5.0.0-V13.3.2` | `UT-AUDIT-209-CTRL-003/004/008/011`; `IT-AUDIT-209-INT-002/005`; deployment projection tests | verified locally |
| Invalid, missing, foreign, or lost key material fails closed and is observable | `SI-4`, `SI-10`, `AU-3`, `AU-12` | `CC7.2` | `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1`, `v5.0.0-V16.5.2` | `UT-AUDIT-209-RES-004`; `UT-AUDIT-209-CTRL-004/007/009/010`; `IT-AUDIT-209-INT-003/004/005/006` | verified locally |
| Secret/configuration projection reaches the production DataStorage Deployment | `AC-6`, `CM-3`, `SA-11` | `CC6.6`, `CC8.1` | `v5.0.0-V13.3.1`, `v5.0.0-V13.3.2`, `v5.0.0-V16.5.2` | `IT-AUDIT-209-INT-007`; `CONTRACT-AUDIT-209-001`; `E2E-AUDIT-209-001` | local envtest/contract; live Kind requires CI execution |
| Secret/state watch paths requeue the singleton | `SI-4`, `AU-12`, `SA-11` | `CC7.2` | `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1` | `UT-AUDIT-209-CTRL-006/010`; `IT-AUDIT-209-INT-006` | mapper covered; manager-event qualification remains follow-up |

## Residual risks

- The operator's pre-existing manager ClusterRole and workload NamespaceRoles
  grant broad Secret access. This is not introduced by #209 and requires a
  separate least-privilege/RBAC decision before making a product-wide claim.
- The pinned upstream `v1.6.0-rc22` runtime retains an unkeyed fallback. The
  operator path always renders HMAC configuration; upstream issue #2509 tracks
  mandatory runtime enforcement for other deployment vectors and rollback paths.
- Secret encryption at rest, KMS/HSM custody, backup/restore, log retention,
  and live OpenShift qualification require deployment/organizational evidence.

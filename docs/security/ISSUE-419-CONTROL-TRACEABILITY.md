# Issue #419 control traceability

This is engineering evidence for the operator configuration boundary. It is
not a formal FedRAMP authorization, SOC 2 attestation, or OWASP ASVS
conformance claim.

Authoritative references:

- NIST SP 800-53 Rev. 5 / FedRAMP control objectives
- SOC 2 Trust Services Criteria
- OWASP ASVS 5.0.0

| Business behavior | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 objective | Evidence |
|---|---|---|---|---|
| Enabled alignment references a defined profile and cannot silently resolve to an absent profile. | CM-6, SI-10 | CC6.6, CC8.1 | V2.2.1, V2.2.2 | `UT-419-VAL-001` |
| OAuth2, tuning, and mTLS path inputs are rejected when incomplete or unsafe rather than silently omitted. | CM-6, SI-10, IA-5, SC-8 | CC6.6, CC8.1 | V2.2.1, V13.3.1 | `UT-419-VAL-002`, `UT-419-VAL-003`, `UT-419-VAL-004` |
| Alignment receives its own complete provider and credential configuration instead of inheriting investigation settings. | CM-6, IA-5 | CC6.6, CC8.1 | V2.2.1, V16.5.2 | `UT-419-CM-001`, `UT-419-CM-002` |
| Workflow discovery receives the same complete sibling-agent profile semantics. | CM-6, IA-5, SC-8 | CC6.6, CC8.1 | V8.2.1, V13.3.1 | `UT-419-CM-004`, `UT-419-CM-005`, `UT-419-DEP-005`, `UT-419-DEP-006` |
| API, OAuth2, and mTLS Secrets are mounted read-only and only when required by the selected sibling profile. | AC-6, IA-5, SC-12 | CC6.1, CC6.6 | V8.2.1, V8.3.1, V13.3.1, V13.3.2 | `UT-419-DEP-001`, `UT-419-DEP-002` |
| Required Secret keys are checked before deployment rather than failing silently at runtime. | IA-5, SI-10, AU-12 | CC7.2, CC8.1 | V13.3.1, V16.5.2 | `UT-419-CTRL-001`, existing pre-created-reference validation |
| The production reconcile path reaches the generated ConfigMap and Deployment with the selected alignment profile. | CM-3, CM-6, SA-11 | CC6.6, CC8.1 | V16.5.2 | `IT-419-RECONCILE-001` |
| Missing alignment Secret material blocks deployment and reports an actionable condition. | IA-5, SI-10 | CC7.2, CC8.1 | V2.2.1, V8.2.1, V16.5.2 | `IT-419-RECONCILE-002` |

The upstream runtime consumption contract is implemented in
[`kubernaut#2516`](https://github.com/jordigilh/kubernaut/pull/2516), pinned by
the operator at commit `50eafbb4cd12c06488939c8100c348a2de5badf2`.

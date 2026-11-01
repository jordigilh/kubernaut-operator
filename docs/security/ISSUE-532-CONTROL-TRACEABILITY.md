# Issue #532 control traceability

This document records engineering evidence for profile-owned LLM TLS material.
It is not a formal FedRAMP authorization, SOC 2 attestation, or OWASP ASVS
conformance claim.

| Business behavior | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 objective | Evidence |
|---|---|---|---|---|
| An operator-owned CA reference has a valid Secret/key and deterministic path before rendering. | CM-6, SI-10, SC-12 | CC8.1 | V2.2.1, V12.3.1 | `UT-532-VAL-001`, `UT-532-VAL-002` |
| Investigation, workflow discovery, and alignment render complete independent CA/cert/key paths. | SC-8, SC-12, CM-6 | CC6.6, CC8.1 | V12.3.1, V13.3.1 | `UT-532-CM-001`, `UT-532-CM-002`, `IT-532-RECONCILE-001` |
| API Frontend main and severity-triage profiles render and mount their selected TLS material. | SC-8, SC-12, AC-6, IA-5 | CC6.1, CC6.6, CC8.1 | V8.2.1, V12.3.1, V13.3.1 | `UT-532-AF-CM-001`, `UT-532-AF-CM-002`, `UT-532-AF-DEP-001` |
| CA/client material is projected read-only and only the selected sibling Secrets are mounted. | AC-6, IA-5, SC-12 | CC6.1, CC6.6 | V8.2.1, V8.3.1, V13.3.1 | `UT-532-DEP-001`, `UT-532-DEP-002`, `UT-532-DEP-003`, `IT-532-RECONCILE-001` |
| Intentionally shared material is reused without adding a sibling Secret mount. | AC-6, CM-6 | CC6.6, CC8.1 | V2.2.2, V13.3.2 | `UT-532-DEP-003` |
| Missing or invalid CA material blocks deployment and reports an actionable condition without exposing Secret data. | SC-8, SI-10, CM-6 | CC7.2, CC8.1 | V2.2.1, V12.3.2, V16.5.2 | `IT-532-RECONCILE-002` |
| Secret contents never enter ConfigMaps, status, or validation messages; controller logging is reviewed to preserve the same boundary. | AC-6, IA-5, AU-12 | CC6.1, CC7.2 | V12.3.4, V14.2.4 | `UT-532-DEP-001`, `IT-532-RECONCILE-001`, `IT-532-RECONCILE-002` and controller logging review |

The upstream Helm chart renders LLM TLS paths but does not provision LLM CA or
client material. This operator-specific evidence closes that deployment-boundary
gap while preserving administrator ownership of referenced Secrets.

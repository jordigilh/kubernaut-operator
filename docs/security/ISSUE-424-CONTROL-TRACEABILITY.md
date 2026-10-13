# Issue #424 monitoring TLS control traceability

**Matrix ID:** KO-SEC-TR-424-001  
**Version:** 0.1  
**Scope:** Shared `spec.monitoring.tls` trust sources, consumer wiring, rotation, and fail-closed behavior

This is an engineering evidence matrix, not a certification package. FedRAMP
references use the corresponding NIST SP 800-53 Rev. 5 control objectives;
OWASP identifiers are requirements from the official
[ASVS 5.0.0 source](https://raw.githubusercontent.com/OWASP/ASVS/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv).
Formal FedRAMP authorization, NIST implementation approval, and ASVS
conformance are not claimed.

Authoritative references:

- [NIST SP 800-53 Rev. 5 Update 1](https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final)
- [FedRAMP control reference](https://www.fedramp.gov/2026/reference/controls/)
- [OWASP ASVS 5.0.0](https://github.com/OWASP/ASVS/releases/tag/v5.0.0)

## Evidence matrix

| Control objective | Repository implementation evidence | Automated evidence | Status / residual risk |
|---|---|---|---|
| FedRAMP/NIST `AC-6`, `SC-8`, `SC-12`, `SC-13`; ASVS `V13.3.1`, `V13.3.2` | Namespace-local Secret/ConfigMap references, PEM validation, read-only `/etc/monitoring-tls/ca.crt` mounts, and no operator mutation of administrator data. | `TLS-SOURCE-001`; `TLS-CONSUMER-001`; `IT-MON-424-001`; `IT-MON-424-002`; `E2E-MON-424-001` | Partially verified: external RBAC and PKI review remain deployment responsibilities. |
| FedRAMP/NIST `SC-8`, `SC-12`, `SC-13`, `SC-17`, `SI-4`; ASVS `V12.3.1`, `V12.3.2`, `V12.3.4`, `V13.3.4`, `V16.5.2` | Selected source objects are watched; valid resource-version digests roll EM, KA, and AF; invalid material blocks validation without changing the last-known-good Deployment template. | `IT-MON-424-004`; `E2E-MON-424-001` | Partially verified: live downstream application reload behavior and OpenShift/OVN qualification remain follow-up evidence. |
| FedRAMP/NIST `SI-10`; ASVS `V2.2.1`, `V2.2.2`, `V2.2.3` | HTTPS-only monitoring endpoints reject leading/trailing whitespace and user information before rendering or policy resolution. | `TLS-VALIDATE-002`; existing monitoring URL validation tests | Partially verified: broader application input inventory is outside Issue #424. |
| FedRAMP/NIST `CM-3`, `CM-6`, `CM-8`; ASVS `V16.1.1`, `V16.2.1`, `V16.5.2` | Generated CRD/package artifacts, source-aware Deployment templates, non-sensitive revision annotations, and actionable validation conditions remain synchronized. | `IT-MON-424-001`–`004`; generated-manifest and hosted CI gates | Partially verified: release-signing and organizational change evidence remain external. |

## Pyramid invariant

- **Unit:** `TLS-SOURCE-001`, `TLS-SOURCE-002`, `TLS-CONSUMER-001`, and
  `TLS-VALIDATE-002` cover source resolution, read-only mounts, consumer path
  consistency, and strict endpoint validation.
- **Integration:** `IT-MON-424-001`–`004` drive production `Reconcile` through
  envtest for Secret, ConfigMap, system trust, invalid rotation, valid
  rotation, and event mapping.
- **E2E:** `E2E-MON-424-001` exercises native-provider Kind lanes with
  certificate-verifying `curl --cacert` handshakes for Secret and ConfigMap
  sources, explicit system-store behavior, wrong-CA rejection, and source
  restoration.

The configured business-unit gate must report 100% statement coverage for the
handwritten business packages; that metric does not substitute for branch,
assertion-strength, downstream-client, or live-platform qualification.

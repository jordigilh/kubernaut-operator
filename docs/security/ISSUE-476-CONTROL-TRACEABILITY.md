# Issue #476 control traceability

**Matrix ID:** KO-SEC-TR-476-001  
**Version:** 0.1  
**Scope:** Console UX release consumption and v1alpha2 raw-thinking policy  
**Status:** engineering evidence

This matrix records repository evidence for selected FedRAMP/NIST, SOC 2, and
OWASP ASVS 5.0.0 control objectives. It does not claim formal FedRAMP
authorization, a SOC 2 attestation, or OWASP ASVS compliance/conformance.

The executable test scope and exit criteria are defined in
`docs/tests/476/TEST_PLAN.md`.

The ASVS identifiers are versioned references to the official
[OWASP ASVS 5.0.0 catalog](https://raw.githubusercontent.com/OWASP/ASVS/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv).

| Business behavior | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 objective | Production evidence | Test evidence | Status |
|---|---|---|---|---|---|---|
| New Console deployments hide raw thinking unless an administrator explicitly enables the diagnostic view. | CM-6, SI-10 | CC8.1 | `v5.0.0-V2.1.1`, `v5.0.0-V2.2.1`, `v5.0.0-V2.2.2` | `api/v1alpha2/kubernaut_types.go`; `internal/resources/console.go`; `internal/resources/console_test.go`; `internal/controller/kubernaut_lifecycle_test.go`; `docs/installation/02-configure-services.md` | `UT-CN-476-001`; `UT-CN-476-002`; `UT-CN-476-003`; `IT-CN-476-001`; `IT-CN-476-002` | verified locally |
| The runtime JavaScript policy is generated from a trusted boolean and is not an uncontrolled script fragment. | CM-6, SI-10 | CC8.1 | `v5.0.0-V1.2.3` | `internal/resources/console.go`; nginx `runtime-config.js` block | `UT-CN-476-001`; `UT-CN-476-002`; `UT-CN-476-003`; `UT-CN-314-004`; `IT-CN-476-001`; `IT-CN-476-002` | verified locally |
| The released Console image and all operator distribution paths use one immutable rc23 artifact. | CM-3, CM-6 | CC8.1 | `v5.0.0-V15.2.4` | `config/manager/manager.yaml`; `bundle/`; `dist/`; `charts/`; `hack/airgap/imageset-config.yaml`; `hack/verify-console-release.sh` | `CI-CN-476-001`; generated-manifest review | verified locally |

## Evidence boundary

The operator provides configuration and generated-resource evidence only. Live
browser behavior, organizational change approval, image signing/provenance
verification, platform log retention, and formal control assessments remain
deployment or organizational responsibilities. `enableRawThinking` is a
deployment-wide diagnostic setting, not a per-user authorization control; when
enabled, all authenticated Console users can receive the raw-thinking view.

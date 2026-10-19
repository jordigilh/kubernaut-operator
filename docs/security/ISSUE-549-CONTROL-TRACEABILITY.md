# Issue #549 Notification credential control traceability

This matrix records repository-level engineering evidence for the Notification
provider-neutral credential projection contract. It uses NIST SP 800-53
Rev. 5/FedRAMP, SOC 2, and OWASP ASVS 5.0.0 control objectives. It does not
claim formal FedRAMP authorization, formal SOC 2 attestation, or formal OWASP
ASVS conformance.

The operator owns the CRD contract, namespace-local Secret reference
validation, projected-volume rendering, and routing ConfigMap ownership
boundary. The Notification service owns provider delivery and runtime
`credentialRef` resolution.

| Business behavior | Operator evidence | Test evidence | FedRAMP/NIST objective | SOC 2 objective | OWASP ASVS 5.0.0 objective | Status |
|---|---|---|---|---|---|---|
| Slack, Microsoft Teams, and PagerDuty credentials use one provider-neutral CRD shape. | `NotificationCredentialSpec` and `NotificationSecretRef` in `api/v1alpha2/kubernaut_types.go`; generated CRD in `config/crd/bases/kubernaut.ai_kubernauts.yaml`. | `UT-NOT-API-001`, `UT-NOT-DEP-002`, `IT-NOT-CRD-RECON-001`. | CM-6, SI-10 | CC6.6, CC8.1 | `v5.0.0-V2.1.1`, `v5.0.0-V2.2.1`, `v5.0.0-V2.2.2` | verified locally |
| Secret references are namespace-local and require the selected key before deployment. | `appendPrecreatedFeatureSecretReferences`; existing pre-created Secret validation and Secret watch. | `UT-NOT-SECRET-005`, `UT-CONFIG-512-002`, `IT-NOT-CRD-RECON-001`. | AC-6, IA-5, CM-6 | CC6.1, CC6.6 | `v5.0.0-V2.2.1`, `v5.0.0-V2.2.2`, `v5.0.0-V8.2.1` | verified locally |
| Credential values are projected read-only and never rendered into ConfigMaps, status, or Deployment JSON. | `NotificationDeployment` projects only Secret name/key/path; the routing ConfigMap contains only non-secret `credentialRef` names. | `UT-NOT-DEP-002`, `UT-NOT-ROUTE-004`, `IT-NOT-CRD-RECON-001`, `E2E-NOT-SECRET-001` (21/21 passed on `kubernaut492-sno-a`). | AC-6, IA-5, SC-28 | CC6.1, CC6.6 | `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1`, `v5.0.0-V16.5.2` | verified locally and on OCP |
| BYO routing explicitly maps receiver/provider blocks to projected credential names; the operator does not infer channel names or own provider-specific routes. | `NotificationRoutingConfigMap` emits the console fallback; `spec.notification.routing.configMapName` remains administrator-owned. | `UT-NOT-ROUTE-004`, `IT-NOT-CRD-RECON-001`. | CM-3, CM-6 | CC8.1 | `v5.0.0-V2.2.2`, `v5.0.0-V16.5.2` | verified locally |
| Referenced Secret updates requeue the owning Kubernaut without exposing rotated credential bytes. | Existing Secret watch plus `precreatedConfigurationToKubernaut` mapping. | `IT-NOT-CRD-RECON-001`. | CM-3, CM-6, SI-4 | CC7.2, CC8.1 | `v5.0.0-V2.2.3`, `v5.0.0-V16.5.2` | verified locally |

## Evidence boundary

The operator verifies schema, namespace-local Secret references, object
rendering, ownership boundaries, and reconciliation propagation. Provider
delivery, runtime routing validation, credential rotation behavior, and
external Secret lifecycle remain Notification-service/platform evidence.

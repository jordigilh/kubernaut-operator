# Issue #539 — Gateway authentication control traceability

## Scope and evidence boundary

This document records engineering evidence for the unpublished v1alpha2
Gateway authentication contract:

```yaml
spec:
  gateway:
    auth:
      enabled: false
```

Authenticated delivery is an explicit opt-in:

```yaml
spec:
  gateway:
    auth:
      enabled: true
      alertManager:
        tokenSecretName: alertmanager-gateway-token
```

The evidence covers operator API publication, deployable defaulting, explicit
authentication opt-in, runtime configuration rendering, Alertmanager
SecretKeySelector boundaries, default-disabled visibility, and real
reconciliation wiring. It does not execute
or independently qualify the upstream Gateway binary's TokenReview/SAR
implementation. It does not claim formal FedRAMP authorization, SOC 2
attestation, or OWASP ASVS authorization/conformance.

The mapped references are NIST SP 800-53 Rev. 5 / FedRAMP control objectives,
SOC 2 Trust Services Criteria, and OWASP ASVS 5.0.0 requirement objectives;
they are not certification claims.

## Control and evidence matrix

| Business assertion | Status | Unit evidence | Integration evidence | FedRAMP/NIST | SOC 2 | OWASP ASVS 5.0.0 |
|---|---|---|---|---|---|---|
| Gateway authentication is disabled when omitted so a minimal deployment has no extra Secret prerequisite; explicit opt-in reaches `server.authenticationEnabled` and identity enforcement. | verified | `UT-539-GW-AUTH-CM-001`, `UT-539-GW-AUTH-CM-002` in `internal/resources/configmaps_test.go` | `IT-539-GW-AUTH-RECONCILE` in `internal/controller/monitoring_wiring_integration_test.go` | AC-3, IA-2, CM-6 | CC6.1, CC8.1 | v5.0.0-V8.2.1, v5.0.0-V8.3.1, v5.0.0-V16.5.2 |
| AlertManager receives only a SecretKeySelector for the administrator-owned bearer Secret; token bytes are not rendered into the AlertmanagerConfig or operator-owned configuration. | verified | `UT-539-GW-AUTH-AM-001` in `internal/resources/ocp_test.go` | `IT-539-GW-AUTH-RECONCILE` in `internal/controller/monitoring_wiring_integration_test.go` | AC-3, AC-6, IA-5 | CC6.1, CC6.6 | v5.0.0-V8.2.1, v5.0.0-V8.3.1, v5.0.0-V13.3.1, v5.0.0-V13.3.2 |
| Default-disabled Gateway authentication is observable and removes the bearer reference while retaining Alertmanager TLS routing. | verified | `UT-539-GW-AUTH-AM-002` in `internal/resources/ocp_test.go`; `UT-539-GW-AUTH-CONDITION-001` in `internal/controller/business_errors_unit_test.go` | `IT-539-GW-AUTH-CONDITION` in `internal/controller/kubernaut_lifecycle_test.go` | AC-3, CM-6, CM-8 | CC6.1, CC8.1 | v5.0.0-V8.3.1, v5.0.0-V16.5.2 |
| Missing or malformed Secret material remains visible as an actionable condition and never becomes a falsely ready authenticated configuration. | verified | Existing AM1/AM3/AM4 lifecycle assertions in `internal/controller/kubernaut_lifecycle_test.go` | Existing AM2/AM5 lifecycle reconciliation assertions in `internal/controller/kubernaut_lifecycle_test.go` | IA-5, SI-4, SI-10 | CC7.2, CC8.1 | v5.0.0-V2.2.1, v5.0.0-V16.1.1, v5.0.0-V16.5.2 |
| The unpublished API has one deterministic nested contract and does not expose the removed flat field, generic signal sources, or Kubernetes Events. | verified | `UT-539-GW-AUTH-API-001` in `api/v1alpha2/clean_break_test.go` and `api/v1alpha2/kubernaut_types.go` | Generated CRD and reconciliation artifacts in `config/crd/bases/`, `bundle/`, `dist/`, and the Helm chart copy | CM-3, CM-6, CM-8, SI-10 | CC8.1 | v5.0.0-V2.2.1, v5.0.0-V15.2.4, v5.0.0-V16.5.2 |

## Qualification limits

- The operator's existing Gateway TokenReview/SAR RBAC is preserved; Issue
  #539 adds no generic source list or new RBAC permission.
- The operator validates Secret existence and the `token` key but never uses
  or copies token bytes into a ConfigMap, status condition, Kubernetes Event,
  or log message.
- Live-cluster qualification of upstream Gateway authentication behavior and
  formal control assessment remain deployment/application-owner boundaries.

## Evidence artifacts

- `api/v1alpha2/kubernaut_types.go`
- `api/v1alpha2/clean_break_test.go`
- `internal/resources/configmaps.go`
- `internal/resources/configmaps_test.go`
- `internal/resources/ocp.go`
- `internal/resources/ocp_test.go`
- `internal/controller/kubernaut_controller.go`
- `internal/controller/kubernaut_lifecycle_test.go`
- `internal/controller/monitoring_wiring_integration_test.go`
- `config/crd/bases/kubernaut.ai_kubernauts.yaml`
- `bundle/manifests/kubernaut.ai_kubernauts.yaml`
- `charts/kubernaut-operator/files/kubernaut.ai_kubernauts.yaml`
- `dist/install.yaml`

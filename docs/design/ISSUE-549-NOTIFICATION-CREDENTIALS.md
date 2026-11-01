# Issue #549 — Notification provider-neutral credentials

**Status:** Approved, implemented, authoritative current v1alpha2 reference
**Scope:** Notification service configuration contract only
**Decision:** Replace the Slack-specific operator credential API with a provider-neutral, namespace-local Secret projection contract.

This document is the authoritative reference for the current
`spec.notification` credential contract. It supersedes the Notification rows
in the historical `docs/design/v1.4-config-field-mapping.md` baseline and is
tracked by GitHub issue [#549](https://github.com/jordigilh/kubernaut-operator/issues/549).

## Decision

The operator exposes one credential shape for Slack, Microsoft Teams, PagerDuty,
and future Notification providers:

```yaml
spec:
  notification:
    credentials:
      - name: slack-ops
        secretRef:
          name: slack-webhook
          key: webhook-url
      - name: teams-primary
        secretRef:
          name: notification-teams
          key: webhook-url
      - name: pagerduty-critical
        secretRef:
          name: notification-pagerduty
          key: routing-key
    routing:
      configMapName: notification-routing
```

- `credentials[].name` is both the projected file name and the upstream
  routing `credentialRef`.
- `secretRef.name` and `secretRef.key` identify a Secret in the Kubernaut
  resource namespace. Cross-namespace references are not part of the API.
- Receiver/provider configuration remains in the administrator-owned routing
  ConfigMap. The operator does not infer credential names from channel names.
- The operator-generated routing ConfigMap is console-only; provider routes use
  `spec.notification.routing.configMapName`.
- No backwards-compatible Slack aliases are retained.

## Implementation boundaries

- API: `api/v1alpha2/kubernaut_types.go`
- Deployment projection: `internal/resources/deployments.go`
- Console fallback routing: `internal/resources/configmaps.go`
- Secret validation and update enqueue: `internal/controller/kubernaut_controller.go`
- Generated CRD/deepcopy/chart/bundle/dist artifacts are synchronized.
- Secret values are never rendered into ConfigMaps, Deployment JSON, status,
  events, or validation messages.

## Evidence

- Unit tests cover invalid/duplicate identities, namespace-local Secret refs,
  opaque credential values, provider-neutral projections, and read-only mounts.
- Envtest covers reconciliation to Running, three-provider projections, BYO
  routing ownership, Secret update requeue, and non-disclosure assertions.
- Live OCP E2E `E2E-NOT-SECRET-001` passed as part of **21/21** specs on
  `kubernaut492-sno-a`.
- Unit business coverage: **100%**, zero missed statements.
- Controller integration coverage: **79.1%** against a 78% threshold.
- Helm suite: **11/11** passed.
- `go build ./...`, `golangci-lint run`, generated-manifest, and security
  traceability gates passed.

## Control-objective boundary

Repository evidence is mapped to project-selected FedRAMP/NIST AC-6, IA-5,
CM-3, CM-6, SC-28, and SI-10; SOC 2 CC6.1, CC6.6, CC7.2, and CC8.1; and
OWASP ASVS 5.0.0 V2.1.1, V2.2.1–V2.2.3, V8.2.1, V8.3.1, and V16.5.2.

This is engineering evidence only. It does not claim formal FedRAMP
authorization, SOC 2 attestation, or OWASP ASVS conformance. Storage
encryption/KMS, organizational access control, credential issuance and
revocation, monitoring operations, and independent assessment remain outside
the operator repository.

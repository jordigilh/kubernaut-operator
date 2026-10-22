# Issue #561: Familiar TLS source modes

**Status:** Implemented
**Date:** 2026-10-22
**Issue:** https://github.com/jordigilh/kubernaut-operator/issues/561

## Decision

The unreleased `v1alpha2` API will expose three TLS source modes using names
already familiar to Kubernetes operators:

| Mode | Source and ownership |
| --- | --- |
| `manual` | The administrator or an external PKI/Secret lifecycle system supplies named CA and serving Secrets. The operator validates and consumes them read-only. |
| `cert-manager` | cert-manager issues and rotates output Secrets. The operator consumes pre-created Issuer/Certificate resources by default and can optionally reconcile them when provisioning is explicitly enabled; it never owns output Secrets. |
| `service-ca` | A platform service-CA capability supplies serving certificates and trust. The current implementation is the OpenShift service-CA adapter. |

The operator-generated self-signed source is removed. The Helm-only `hook`,
legacy `cert-manager`, and legacy administrator-managed mode split are also
removed from the application CR API because the application Helm chart is
being deprecated. The operator bootstrap Helm chart remains a separate
installation concern.

The empty `spec.tls.mode` value retains the current OpenShift service-CA
auto-discovery behavior. New generic Kubernetes/Kind fixtures select
`cert-manager` explicitly.

## API impact

- Replace the TLS mode enum with `manual`, `cert-manager`, and `service-ca`.
- Rename the manual configuration block to match the mode name.
- Retain the familiar `certManager` provider configuration block and its
  optional provisioning settings.
- Remove self-signed and Helm-hook configuration blocks and types.
- Regenerate the CRD, bundle, chart CRD copy, and `dist/install.yaml`.
- This is an unreleased API change; old title-case and Helm parity mode values
  do not need compatibility aliases.

## Runtime behavior

- `manual` uses explicit administrator-owned Secret references and never adds
  owner references, mutates Secret data, or deletes the inputs.
- `cert-manager` is reference-only by default. Set
  `certManager.provisioning.enabled=true` to reconcile Issuer/Certificate
  resources. The operator owns only those cert-manager resources and cert-manager
  owns output Secrets.
- `service-ca` uses the existing OpenShift service-CA annotations, injected
  ConfigMaps, and platform trust path. Generic clusters fail closed when the
  capability is unavailable.
- No mode falls back to plaintext or operator-generated self-signed material.

## TDD and verification

### RED

- Add API/resource tests for the three mode values and invalid removed values.
- Add manual read-only ownership tests.
- Add cert-manager reference-only and optional-provisioning tests.
- Add service-CA explicit/implicit resolution and generic-cluster fail-closed
  tests.
- Update Kind selector tests so the default lane resolves to `cert-manager`.

### GREEN

- Implement the new enum/config shape.
- Merge the old manual and administrator-managed resolver paths.
- Merge the old cert-manager mode paths behind the single `cert-manager` mode.
- Remove self-signed and Helm-hook reconciliation branches.
- Update the Kind journey to install/use cert-manager for the default TLS lane.

### REFACTOR and evidence

- Update installation, security, design, migration, sample, and test-plan docs.
- Remove stale old mode names from generated and source artifacts, except in
  the issue decision/migration history where they explain the replacement.
- Run `go build ./...`, affected Ginkgo suites, `golangci-lint run`,
  `make manifests generate`, and the repository regression checks.

## Wiring manifest

No new resource builder or controller entry point is introduced. Existing
`KubernautReconciler` TLS validation and reconciliation paths are simplified
and covered by the existing unit/envtest suites.

## Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Removing self-signed code breaks hidden test fixtures | Search all symbol references before deletion; replace Kind/default fixtures first and run full tests. |
| Cert-manager provisioning semantics regress | Preserve the existing optional provisioning block and ownership tests. |
| Service-CA behavior changes on OpenShift | Keep the empty-mode adapter path and add explicit `service-ca` equivalence tests. |
| Generated CRD copies drift | Run `make manifests generate` and verify all packaged CRD copies. |

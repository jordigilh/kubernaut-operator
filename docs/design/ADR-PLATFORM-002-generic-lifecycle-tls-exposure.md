# ADR-PLATFORM-002: Generic lifecycle, TLS, and exposure contract

**Status:** Approved for Phase 2 implementation

**Date:** 2026-10-01

**Parent:** `ADR-PLATFORM-001-platform-neutral-provider-policy.md`

## Decision

The operator's core lifecycle must work on a Kubernetes cluster that does not
serve OpenShift, Prometheus Operator, cert-manager, or CNI-provider APIs.
Optional integrations are selected by discovery and are never represented by
an unconditional typed watch or an unconditional resource write.

### Generic exposure

Gateway, API Frontend, and Console each have an explicit `Ingress` block. An
enabled block requires a host, an Ingress class, and a TLS Secret. The operator
creates a `networking.k8s.io/v1` Ingress and reports exposure readiness. An
unset block means the component remains internal and is not an error. OpenShift
Route builders remain an adapter used only when Route discovery succeeds.

The operator does not infer generic hostnames from OpenShift ingress settings,
cluster DNS, or `cluster.local`.

### Runtime TLS source

`spec.tls.mode` selects exactly one runtime source:

* `AdministratorManaged`: the administrator supplies the internal CA,
  per-service serving Secrets, the AuthWebhook serving Secret, and optional
  external Ingress Secrets through explicit references.
* `CertManager`: cert-manager must already be installed and the configured
  issuer references are validated. The operator may submit Certificate
  objects in a later adapter, but never installs cert-manager or assumes its
  CRDs exist.
* `DevelopmentSelfSigned`: the operator creates a namespace-scoped CA and
  leaves for development/Kind only. This mode is explicit and its status is
  non-production; it is never a fallback from another source.

When OpenShift service-CA capability is positively discovered, the existing
service-CA/router-CA behavior remains an OpenShift adapter. Generic code never
emits service-CA annotations or reads fixed OpenShift ConfigMaps.

The runtime artifacts are intentionally separate:

1. operator-manager serving material and bootstrap webhook CA;
2. AuthWebhook serving material and admission `caBundle` values;
3. the internal CA, service leaves, and published trust bundle;
4. external Ingress TLS Secrets;
5. database/Valkey/monitoring/IdP/LLM trust and client material; and
6. DataStorage signing and audit-HMAC material.

No source failure may produce plaintext HTTP, an empty fail-closed webhook
`caBundle`, or deletion of the last working trust root.

### Monitoring

Prometheus Operator resources are rendered independently and only when their
specific API is discovered. OpenShift AlertmanagerConfig and OpenShift
monitoring defaults remain optional adapters. Generic clusters use explicit
monitoring endpoints or report that monitoring is unavailable; they do not
inherit Thanos/AlertManager URLs from OpenShift.

## Status contract

Phase 2 adds these conditions without changing lifecycle phase names:

* `PlatformCapabilitiesReady` — capability discovery completed; absent
  optional APIs are recorded, not treated as manager-startup failures.
* `TLSReady` — the selected runtime source has valid, usable material.
* `ExposureReady` — configured Ingress/Route exposure exists, or all exposed
  components are intentionally internal.
* `MonitoringReady` — configured monitoring is ready, disabled, or unavailable
  with an actionable reason.

Every condition observes the CR generation. Reconciliation logs and events
include the source mode, capability result, resource kind/name, generation,
and resourceVersion where available.

## Alternatives rejected

* Treating OpenShift annotations and fixed ConfigMap names as generic TLS.
* Falling back to plaintext when a certificate source is unavailable.
* Creating Routes on clusters where Route discovery is absent.
* Treating one global `tls.enabled` switch as equivalent to the separate
  manager, AuthWebhook, inter-service, and external identities.

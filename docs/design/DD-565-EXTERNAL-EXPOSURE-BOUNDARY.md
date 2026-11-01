# DD-565: Administrator-owned external exposure boundary

**Status**: Accepted
**Decision Date**: 2026-10-22
**Confidence**: 100% for the design; implementation confidence requires the listed test and E2E gates
**Deciders**: Kubernaut Operator Team
**Applies To**: `api/v1alpha2`, `internal/controller/`, `internal/resources/`, RBAC, samples, Helm/OLM artifacts, and platform documentation

**Related Issues**:

- [kubernaut-operator#565](https://github.com/jordigilh/kubernaut-operator/issues/565)
- [kubernaut-operator#564](https://github.com/jordigilh/kubernaut-operator/pull/564)

**Related Design**:

- [`ISSUE-565-IMPLEMENTATION-PLAN.md`](ISSUE-565-IMPLEMENTATION-PLAN.md)
- [`ADR-PLATFORM-001-platform-neutral-provider-policy.md`](ADR-PLATFORM-001-platform-neutral-provider-policy.md)
- [`docs/installation/06-platform-support.md`](../installation/06-platform-support.md)

## Context

The Kubernaut CR currently contains Route and Ingress fields for Gateway, API
Frontend, and Console. The operator uses those fields to create platform
exposure resources, infer external hostnames, and grant itself Route/Ingress
management permissions.

External exposure is deployment- and platform-owned. Hostnames, ingress classes,
edge TLS termination, mesh attachment, network policy, and user-facing access
authorization vary by cluster and must be controlled by administrators or their
platform/GitOps controllers. The operator must not make a workload externally
reachable as a side effect of applying a Kubernaut CR.

`v1alpha2` has not been released. There is no supported legacy-CR migration
contract or legacy exposure cleanup obligation.

## Decision

1. Remove `route` and `ingress` from Gateway, API Frontend, and Console in the
   `v1alpha2` CRD. Remove their dedicated API types, builders, reconciliation,
   manager RBAC, scheme registration, and generated artifacts.
2. Keep operator-managed workloads and internal `ClusterIP` Services. The
   operator continues to provide service TLS and internal component wiring.
3. Keep Alertmanager delivery on the deterministic internal URL:
   `https://gateway-service.<namespace>.svc.cluster.local:8443/api/v1/signals/prometheus`.
4. Administrators own external Route, Ingress, Gateway API, mesh, hostname,
   edge-TLS, and edge-authorization resources. The operator does not create,
   update, watch, adopt, or delete those resources.
5. Console OAuth2 callback configuration is explicit in
   `spec.console.auth.redirectURL`. It must be an administrator-selected HTTPS
   callback URL ending in `/oauth2/callback`; the operator never derives it from
   an OpenShift ingress domain.
6. Replace the operator-created `RouteReady` status contract with an
   `ExposureReady=True` condition that states external exposure is
   administrator-owned and operator-managed Services remain internal.

## Alternatives considered

| Option | Decision | Rationale |
|---|---|---|
| Keep CR-managed Route/Ingress resources | Rejected | Couples application lifecycle to platform exposure and grants unnecessary manager RBAC. |
| Remove only Gateway Route support | Rejected | Leaves the same overreach for Console/API Frontend and retains shared external-exposure RBAC. |
| Remove all CR-managed exposure but derive Console callback from OpenShift ingress domain | Rejected | Still infers external access from a platform-specific API and breaks the ownership boundary. |
| Remove all CR-managed exposure and require explicit Console callback URL | **Selected** | Keeps application auth configuration usable while making external exposure and hostname ownership explicit. |

## Ownership and migration boundary

Because `v1alpha2` is unreleased, no compatibility alias, conversion field, or
legacy-object migration path is required. The operator is not given a cleanup
permission for administrator exposure objects. Ownership tests continue to prove
that unrelated administrator-created objects are preserved if observed by a
shared Kubernetes API or by an unrelated ownership path.

## Security/control objectives

This decision provides engineering evidence for, but does not claim formal
FedRAMP authorization, SOC 2 attestation, or OWASP ASVS conformance:

- FedRAMP/NIST: AC-3, AC-6, CM-3, CM-6, SC-7, SC-8, SC-13, SC-17, SI-4, SI-10.
- SOC 2: CC6.1, CC6.6, CC7.2, CC8.1.
- OWASP ASVS 5.0.0: V2.2.1, V8.2.1, V8.3.1, V12.2.1, V13.1.1, V13.2.1,
  V16.5.2.

Evidence is defined in [`docs/security/ISSUE-565-CONTROL-TRACEABILITY.md`](../security/ISSUE-565-CONTROL-TRACEABILITY.md) and [`docs/tests/565/TEST_PLAN.md`](../tests/565/TEST_PLAN.md).

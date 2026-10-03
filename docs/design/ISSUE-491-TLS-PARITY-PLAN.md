# Issue #491: Upstream Helm TLS parity in the operator

**Status:** Approved for implementation

**Date:** 2026-10-02

**Parent:** #488 / PR #490

**Related:** #489

## Decision

The TLS configuration surface is intentionally one-to-one with the upstream
chart: `spec.tls.mode` accepts `hook`, `cert-manager`, and `manual`, and
`spec.tls.certManager.issuerRef`, `spec.tls.interService`, and
`spec.tls.hooks.tlsCerts` mirror the corresponding chart values. The existing
title-case operator modes remain accepted as backward-compatible aliases.

The lower-case `cert-manager` mode provisions the chart-equivalent resources
by default. The legacy title-case `CertManager` mode remains reference-only
unless its optional `provisioning` block is explicitly enabled; this preserves
the ownership contract delivered by #488 for already-migrated CRs.

The operator will never install or upgrade cert-manager, its CRDs, or its
controller. When provisioning is enabled, the operator owns only the
cert-manager `Issuer` and `Certificate` resources that it creates. cert-manager
owns the generated output Secrets and remains the certificate rotation
authority. The operator must not adopt, update, or delete those Secrets.

Manual/administrator-managed mode remains strictly read-only. Development
self-signed mode remains operator-owned and development-only.

## Upstream parity contract

| Upstream Helm behavior | Operator contract |
| --- | --- |
| `tls.mode=hook` | Operator-reconciled equivalent of the Helm pre-install/pre-upgrade hook: stable existing Secret names, internal trust bundle, SANs, rotation overlap, and cleanup are retained and expanded for enabled runtime identities. |
| `tls.mode=cert-manager` | Create a dedicated internal CA bootstrap `Issuer`, CA `Certificate`, CA-backed `Issuer`, runtime leaf `Certificate` objects, AuthWebhook `Certificate`, and DataStorage RSA-2048 signing `Certificate`. |
| `tls.mode=manual` | Validate the chart's stable administrator-provided CA ConfigMap/serving Secrets, webhook Secret, and signing Secret without adoption or mutation, and fail closed when required material is absent or invalid. |
| `tls.certManager.issuerRef` | `spec.tls.certManager.issuer`; used for externally issued AuthWebhook and DataStorage signing certificates. Empty names follow the chart's live-cluster single-issuer selection behavior. |
| `tls.interService.certDir` / `caFile` | `spec.tls.interService.certDir` / `caFile`, defaulting to `/etc/tls` and `/etc/tls-ca/ca.crt`. |
| `hooks.tlsCerts.extraSANs` | `spec.tls.hooks.tlsCerts.extraSANs`; extra SANs apply to inter-service leaves and add loopback `127.0.0.1` when non-empty. |
| `cert-manager` output Secret ownership | Generated Secrets remain cert-manager-owned; operator-created `Certificate` resources are owner-referenced to the `Kubernaut` CR. |
| cert-manager webhook CA injection | Provisioned AuthWebhook webhook configurations use cert-manager CA-injection metadata and are checked for a non-empty CA bundle before TLS readiness is reported. |

The operator does not own PostgreSQL or Valkey workloads. Their server-side
certificates remain administrator/BYO material; this is the same ownership
boundary as the operator's existing infrastructure contract. The CR still
accepts the chart-compatible stable Secret names and preserves client-side
Valkey TLS references through the existing `spec.valkey.tls` contract.

## API compatibility

The new provisioning block is optional and disabled by default. Existing
reference-only `CertManager` objects continue to require and validate their
explicit CA and service Secret references. In provisioning mode, omitted
Secret names resolve to the existing stable operator names, while explicitly
provided names remain valid for migration. Existing fields are loosened only
where necessary to permit those defaults; no existing field is removed or
renamed.

Provisioning configuration covers:

- enabled/disabled ownership mode;
- internal CA and service Secret-name overrides;
- Certificate duration and renewal window;
- extra SAN DNS names;
- component-aware optional leaves for enabled operator workloads.

The DataStorage signing certificate defaults to `datastorage-signing-cert`,
uses RSA 2048, and is mounted at the existing `/etc/certs` path when
provisioning is enabled and no explicit `SigningCert` override is supplied.
The audit-HMAC key remains a separate integrity-critical input and is never
rotated implicitly by TLS reconciliation.

## Reconciliation and ownership

1. Validation discovers the selected cert-manager API and configured external
   Issuer/ClusterIssuer.
2. Provisioning reconciliation creates/updates only operator-owned
   `Issuer`/`Certificate` resources and waits for their Ready conditions and
   output Secrets.
3. The operator validates generated Secret material and service identities but
   never writes the output Secrets.
4. Trust ConfigMaps and workload/webhook resources are reconciled only after
   usable material exists; a source failure preserves the last working
   artifacts and never creates plaintext or an empty fail-closed CA bundle.
5. Certificate rotation is delegated to cert-manager. The operator re-reads
   the resulting material and updates trust consumers without deleting the
   previous working root.

## Test and evidence plan

- Unit tests for defaults, resource shapes, ownership boundaries, SANs,
  RSA/ECDSA algorithms, server/client usages, duration/renewal settings, and
  no-secret-adoption behavior.
- Envtest tests for cert-manager API absence/presence, provisioning resource
  ownership, readiness/failure preservation, webhook CA injection metadata, and
  signing-certificate mount defaults.
- Kind lanes for explicit development, administrator-managed, and
  cert-manager provisioning sources. The cert-manager lane uses pinned
  cert-manager/Kind/kubectl versions and verifies Certificate-owned output
  Secrets, rotation, webhook trust, and cleanup ownership.
- Documentation parity matrix and migration examples for each Helm mode.
- Control-objective traceability in
  `docs/security/ISSUE-491-TLS-CONTROL-ATTESTATION.md`, mapping each business
  assertion to NIST/FedRAMP, SOC 2, and versioned OWASP ASVS 5.0.0 evidence.

## Deferred or excluded

- Installing/upgrading cert-manager or its CRDs/controller.
- A dedicated operator Helm chart (#489).
- OpenShift/OVN live qualification beyond the platform adapter evidence in
  #488.
- Ownership of PostgreSQL/Valkey workloads or their server PKI.

## Control-objective traceability

The implementation-level attestation is maintained separately from this
architecture decision so that the design remains stable while test and CI
results change. The attestation explicitly distinguishes repository-verified
behavior from formal FedRAMP, SOC 2, and OWASP ASVS evidence that requires
independent assessment or deployment qualification:

`docs/security/ISSUE-491-TLS-CONTROL-ATTESTATION.md`

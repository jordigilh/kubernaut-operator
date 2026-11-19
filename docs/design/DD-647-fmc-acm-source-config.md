# DD-647: FMC ACM Search source configuration and credentials

Date: 2026-11-19
Issue: kubernaut-operator#647
Upstream contract: DD-FLEET-017 v2 D4.1a (jordigilh/kubernaut#2661, recorded on `docs/2661-acm-inventory-contract`)

## Decision

FMC's ACM Search source surface is `spec.fleetMetadataCache.sources.acm`
with `endpoint`, `tokenSecretRef`, and `tls`. Presence of `sources.acm`
selects the ACM source; omission leaves the GitOps/MCP lane rendering
byte-identical. Absent configuration under a selected source is a
validation error (fail-closed, no fallback).

## Alternatives considered

1. **Reuse `FleetTrustSpec` for CA trust (chosen).** Every existing CA
   pattern in this CRD is Secret-based (`interService|system|file|secret`).
   The issue's `caConfigMap` proposal would have introduced a first
   ConfigMap-CA reference pattern; the issue's own scope allowed reuse of
   the inter-service/fleet CA mechanism. The operator resolves the trust
   policy to a path; FMC consumes artifacts only — upstream confirmed no
   nested `tls` struct on the FMC side (D4.1a).
2. **New `caConfigMap` reference.** Rejected: new CRD pattern, no existing
   mount/validation plumbing, contradicting repo conventions.
3. **Reuse `FleetACMScopeCheckSpec`.** Rejected: identical field shape, but
   different validation semantics — the source endpoint is a base URL with
   no path (the client appends `/searchapi/graphql`), unlike the
   scope-check endpoint, which permits paths. Kept as a distinct
   `FleetACMSourceSpec`.

## Rendered YAML contract (pinned by UT-FLEET-647-SRC-002)

```yaml
sources:
  acm:
    endpoint: https://acm-search.example.com   # absolute HTTPS base URL, no path
    tokenPath: /etc/fleet-token/token          # mounted file, never the Secret
    caFile: /etc/fleet-tls/acm-source/ca.crt   # omitted when trust resolves to system
```

Upstream decodes `sources` with known-fields-only strictness: exactly
these three keys may render, anything extra fails FMC startup by design.
`caFile` is trust-source-resolved via `resolveFleetTrustCAFile`
(interService default → the shared inter-service CA mount; system → key
omitted; file → administrator path passthrough; secret → the read-only
keyed lane mount at `/etc/fleet-tls/acm-source`).

## Rotation

Token rotation is handled by FMC's file token source and CA rotation by
its `sharedtls.CAReloader` (D4.1a rule 6: rotation without restart, no
new mechanism) — so, unlike the MCP Gateway lane, no Deployment rollout
revision is rendered. The Secret watch (`fleetTrustSecretReferenced`) is
extended with the sources.acm lane for prompt `PhaseError` on Secret
deletion and re-heal on re-creation.

## Fail-closed validation

- `validateFleetMetadataCacheSources` (spec level, admission via
  `ValidateFleet`): source selected while FMC is not the active backend →
  error; endpoint required, absolute HTTPS base URL without path
  (`validateFleetACMBaseURL`, stricter than `validateFleetHTTPSURL`);
  tokenSecretRef required; trust lane validated.
- `validateFleetMetadataCacheACMSourceSecrets` (controller,
  `ReasonFleetACMSourceSecretInvalid`): token Secret existence and key.
- `validateFleetTrustSecrets` fourth lane: CA Secret existence, key, PEM
  parse for `spec.fleetMetadataCache.sources.acm.tls`.

## Privilege boundary

Rendering these values grants no privilege. The Search principal's
read-only RBAC on the ACM hub is an ACM-platform/operator-side action,
documented in the sample CR — not granted here. The FMC ServiceAccount
gains no Search RBAC through this change (asserted by
UT-FLEET-647-SRC-007 and IT-FLEET-647-SRC-001, FedRAMP AC-6).

## Controls

- Token never in logs, ConfigMap plaintext, status, or annotations:
  SOC2 CC6.1, OWASP ASVS v5.0.0-V9.1.1 (UT-FLEET-647-SRC-008,
  IT-FLEET-647-SRC-001).
- Fail-closed validation, no silent fallback: FedRAMP CM-7,
  OWASP ASVS v5.0.0-V5.1.1 (UT-FLEET-647-VALIDATE-001..003,
  IT-FLEET-647-SRC-002/003).
- Read-only keyed mounts: FedRAMP SC-28, ASVS v9.1.1.
- No new RBAC: FedRAMP AC-6.

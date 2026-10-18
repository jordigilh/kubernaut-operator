# DD-550: DataStorage Semantic v1alpha2 CRD Contract

## Decision

Use a semantic nested DataStorage API in the unreleased `v1alpha2` CRD:

```yaml
spec:
  dataStorage:
    server: {}
    database: {}
    redis: {}
    audit:
      retention: {}
      signingCertificate: {}
      hashKey: {}
    endpointPropagationDelay: 10s
    logging: {}
    telemetry: {}
    resources: {}
```

This is a clean breaking reshaping. No conversion shim or legacy aliases are
required because `v1alpha2` has not been released.

## Rationale

- `server`, `database`, and `redis` group runtime configuration by dependency
  and responsibility.
- `audit` keeps retention, audit-export signing, and HMAC-chain identity under
  one security-relevant boundary.
- PostgreSQL and Valkey connection/TLS settings remain shared under
  `spec.postgresql` and `spec.valkey`; DataStorage does not duplicate shared
  backend configuration.
- `signerCertDir` is derived from `audit.signingCertificate.mountPath` rather
  than exposed as a second source of truth.
- Workload-management controls and per-service Fleet overrides remain out of
  scope.

## Security and compliance decisions

- Retention renders enabled by default with an explicit opt-out. This preserves
  the upstream AU-11 production floor; the DataStorage service owns actual
  purge execution.
- Audit HMAC and signing Secrets are projected read-only. Secret data is never
  placed in pod annotations; only non-sensitive resource-version-derived
  revisions are used for rollout.
- Empty CORS origins remain deny-all.
- Body-size, timeout, rate-limit, connection-pool, and retention inputs are
  validated by the operator before deployment where the operator owns the
  contract; DataStorage runtime clamping remains the upstream service’s
  responsibility.
- `maxConcurrentRequests` is retained as an approved rendered contract, but
  upstream `v1.6.0-rc22` does not currently consume it. Operator evidence must
  not claim runtime concurrency enforcement until the operand supports it.

## Evidence objectives

Tests and implementation evidence map to project-selected objectives only:

- FedRAMP/NIST: `AU-9`, `AU-11`, `AC-6`, `IA-5`, `SC-5`, `SC-12`, `SC-13`,
  `SI-4`, `SI-10`, `CM-3`, `CM-6`, `SA-11` as applicable.
- SOC 2: `CC6.6`, `CC7.2`, `CC8.1` as applicable.
- OWASP ASVS 5.0.0: `V2.2.1`, `V2.2.2`, `V2.2.3`, `V11.1.1`, `V12.3.2`,
  `V13.3.1`, `V13.3.2`, `V14.2.4`, and `V16.5.2` only where repository
  evidence supports the mapping.

These are engineering evidence mappings, not formal compliance claims.

## Related plan

Implementation sequence and verification gates are tracked in
`docs/design/ISSUE-550-IMPLEMENTATION-PLAN.md`.

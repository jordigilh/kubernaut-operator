# Issue #550: DataStorage v1alpha2 Semantic CRD Refactor

## Status

**Implementation complete and verified (`make test`, `go build ./...`,
`golangci-lint run`).** The user approved the
semantic nested design. `kubernaut.ai/v1alpha2` is unreleased, so this change
may make a clean breaking API reshaping without a compatibility shim.

The durable architectural decision is recorded in
`docs/design/DD-550-DATASTORAGE-CRD-SEMANTIC.md`.

Scope is DataStorage only. This plan does not add replicas, autoscaling, PDB,
pod/container security contexts, affinity, topology spread, node selectors,
tolerations, or per-service Fleet configuration.

## Approved API shape

```yaml
spec:
  dataStorage:
    endpointPropagationDelay: 10s
    server:
      readTimeout: 30s
      writeTimeout: 30s
      maxBodySize: "5242880"
      maxConcurrentRequests: 100
      corsAllowedOrigins: []
      rateLimit:
        requestsPerSecond: 50
        burst: 100
    database:
      maxOpenConns: 100
      maxIdleConns: 20
      connMaxLifetime: 1h
      connMaxIdleTime: 10m
    redis:
      dlqMaxLen: 10000
    audit:
      retention:
        enabled: true
        interval: 24h
        batchSize: 1000
        defaultDays: 2555
      signingCertificate:
        secretName: datastorage-signing-cert
        mountPath: /etc/certs
      hashKey:
        existingSecret: ""
        secretKey: hmacKey
    logging:
      level: INFO
    telemetry: {}
    resources: {}
```

PostgreSQL remains shared under `spec.postgresql`; Valkey address, credential,
and TLS settings remain shared under `spec.valkey`. DataStorage owns only its
DataStorage-specific Redis/DLQ tuning. `signerCertDir` is not exposed as a
second source of truth; it is derived from
`audit.signingCertificate.mountPath`.

## Preflight evidence

- `api/v1alpha2/kubernaut_types.go:2410-2558` contains the current flat
  DataStorage fields and reusable retention/signing/logging types.
- `internal/resources/configmaps.go:457-530,1336-1461` already models the
  rendered `server`, `database`, `redis`, `retention`, and `audit` sections.
- `internal/resources/audit_hmac.go:78-94` is the audit-key reference resolver
  that must move to `spec.dataStorage.audit.hashKey`.
- `internal/resources/deployments.go:79-227` consumes DataStorage resources,
  telemetry, Valkey TLS, signing-certificate, and audit-key mounts.
- `internal/resources/tls_certmanager.go:97,265-270` and
  `internal/resources/tls.go:525` consume the signing-certificate field.
- `internal/controller/kubernaut_controller.go:1722-1723,1987-1988,2082-2084`
  already wires ConfigMap, Deployment, and Service builders; no new production
  entry point is required.
- Existing focused test surfaces are
  `internal/resources/configmaps_test.go`, `deployments_test.go`,
  `audit_hmac_test.go`, `tls_certmanager_test.go`, and the controller audit-key
  integration tests.
- Generated schema impact is limited to the `spec.dataStorage` block in
  `config/crd/bases/kubernaut.ai_kubernauts.yaml`, generated deepcopy/code
  artifacts, samples, and DataStorage documentation.
- The embedded operator-chart CRD must stay byte-for-byte synchronized with
  `config/crd/bases/kubernaut.ai_kubernauts.yaml`; `hack/verify-helm-chart.sh`
  is the packaging parity gate.

## Spike decisions

### Upstream runtime semantics

Indexed upstream evidence confirms:

- `pkg/datastorage/config/config.go:84-124` defines server body-size, CORS,
  signer-directory, endpoint-propagation, and rate-limit fields.
- `pkg/datastorage/config/config_accessors.go:104-145` defaults body size to
  5 MiB, clamps it to 1–50 MiB, preserves empty CORS as deny-all, and defaults
  the signer directory to `/etc/certs`.
- `pkg/datastorage/config/config.go:218-255` defines DLQ, audit, and retention
  configuration.
- `pkg/datastorage/config/config.go:126-142` defaults rate limiting to 50
  requests/second and burst 100 when unset or non-positive.
- `pkg/datastorage/config/config.go:247-283` defaults retention interval,
  batch size, and 2,555 days, with a maximum of 2,555 days.
- `charts/kubernaut/values.schema.json:1271-1465` confirms the Helm defaults
  and validation boundaries.

Decision: preserve these runtime semantics in the operator-rendered YAML.
`maxBodySize` remains a string; the operator must not introduce stricter CRD
validation that changes the upstream clamp/fallback behavior. Rate-limit
`enabled` remains a rendered security default, not a new public CRD field.
The operator CRD uses an integer `requestsPerSecond`, matching the existing
Kubernaut Agent rate-limit API and controller-gen schema conventions, while the
renderer emits the numeric operand value. `maxConcurrentRequests` remains in
the approved rendered contract; upstream v1.6.0-rc22 currently exposes the
Helm schema entry but does not consume that field in its DataStorage server
config, so the operator test proves contract rendering rather than runtime
enforcement for that field.

### Retention and compliance behavior

Retention is enabled by default in the approved shape to align the operator
with the upstream production contract and the AU-11/SOC 2 retention objective.
An explicit `enabled: false` remains supported for controlled development use.
Tests must cover both paths. The operator verifies and renders configuration;
the DataStorage service owns the actual purge behavior.

### Audit and signing boundaries

Nesting changes the API paths but not ownership, Secret projection, read-only
mounts, generated-key lifecycle, or certificate behavior. HMAC key and signing
certificate tests remain separate business contracts.

## TDD and wiring plan

1. **RED:** Add or update Ginkgo/Gomega tests for nested API paths, defaults,
   server limits, CORS, rate limiting, DLQ length, audit key/signing mounts,
   retention default/opt-out, and the real reconciliation path.
2. **GREEN:** Implement the smallest type and consumer changes needed for the
   tests. Keep shared PostgreSQL/Valkey configuration shared.
3. **REFACTOR:** Consolidate helper names/types, remove stale field paths, and
   preserve existing Secret/TLS ownership semantics.

Wiring manifest:

| Component | Production entry point | Wiring location | Required evidence |
|---|---|---|---|
| ConfigMap | `resources.DataStorageConfigMap` | `buildCoreConfigMaps` | Render contract plus envtest reconciliation |
| Deployment | `resources.DataStorageDeployment` | `enabledDeploymentBuilders` | Resources, config, Secret/TLS mounts plus envtest |
| Service | `resources.Services` | `ensureServices` | Existing fixed Service contract |
| Audit HMAC | Existing audit-key helpers | `internal/resources/audit_hmac.go`, `deployments.go` | Generated/existing Secret, read-only projection, key path |
| Signing certificate | Existing TLS helpers | `tls_certmanager.go`, `deployments.go` | Secret source, mount path, read-only behavior |

## Business-level control traceability

Tests must prove behavior, not merely increase coverage:

| Business behavior | Controls | Planned evidence |
|---|---|---|
| Secure body-size, concurrency, timeout, CORS, and rate-limit defaults/overrides reach `datastorage-config` | FedRAMP/NIST `SC-5`, `SI-10`, `CM-6`; SOC 2 `CC6.6`, `CC7.2`; ASVS `v5.0.0-V2.2.1`, `V2.2.2`, `V2.2.3` | Focused ConfigMap cases for defaults, overrides, bounds, and deny-all CORS |
| Retention is enabled by default, bounded to 2,555 days, and explicitly disableable | FedRAMP/NIST `AU-11`, `SI-10`, `CM-6`; SOC 2 `CC8.1`; ASVS `v5.0.0-V2.2.1`, `V2.2.2`, `V2.2.3` | ConfigMap plus admission/envtest cases for default, false, invalid duration, negative, and over-limit days |
| Audit HMAC configuration is rendered, projected read-only, and administrator-managed keys are not replaced | FedRAMP/NIST `AU-9`, `IA-5`, `AC-6`, `SI-10`; SOC 2 `CC6.6`, `CC8.1`; ASVS `v5.0.0-V11.1.1`, `V13.3.1`, `V13.3.2`, `V16.5.2` | Existing audit-key unit/integration contracts updated to nested paths |
| Signing certificate remains isolated from HMAC material and is mounted read-only at the derived path | FedRAMP/NIST `SC-12`, `SC-13`, `IA-5`; SOC 2 `CC6.6`; ASVS `v5.0.0-V12.3.2`, `V13.3.1`, `V13.3.2` | Deployment and TLS resource cases for default/custom Secret and mount paths |
| Nested CR configuration reaches the production reconciliation path | FedRAMP/NIST `CM-3`, `CM-6`, `SI-4`, `SA-11`; SOC 2 `CC7.2`, `CC8.1`; ASVS `v5.0.0-V14.2.4`, `V16.5.2` | Envtest reconciliation proving ConfigMap, Deployment, Secret projections, and Service wiring |

These mappings are engineering evidence only; they do not claim formal
FedRAMP authorization, SOC 2 attestation, or OWASP ASVS conformance.

## CodeRabbit follow-up disposition

- Helm embedded CRD synchronized and verified by `hack/verify-helm-chart.sh`.
- `endpointPropagationDelay` is rendered under `server`, matching the pinned
  operand’s runtime config.
- Effective signing Secret changes produce a non-sensitive pod revision and are
  watched for reconciliation; rotation is covered by focused unit/envtest
  cases.
- DataStorage duration and numeric validation is enforced before deployment.
- `maxConcurrentRequests` remains a render-only contract until the pinned
  operand consumes it; no runtime SC-5 enforcement claim is made for it.

## Coverage and verification gates

- Achieve at least **90% UT coverage for changed DataStorage resource/config
  logic**, with higher coverage where the security/compliance branches justify
  it. Do not add coverage-only tests.
- Preserve the project targets of 96%+ for `internal/resources` and 78%+ for
  controller integration coverage.
- Run `make manifests generate`, `go build ./...`, `golangci-lint run`, and
  `make test` after implementation.
- Review generated `config/` changes and confirm no stale references to
  `Spec.DataStorage.Retention`, `SigningCert`, or `AuditHashKey` remain.

## Confidence

**95%.** The approved shape is supported by the existing operator render
sections, exact upstream runtime semantics, and existing DataStorage
secret/TLS/controller wiring. Remaining risk is limited to nested
pointer/default behavior, generated CRD validation, and preserving audit-key
and signing-certificate lifecycle behavior. RED tests, an envtest reconciliation
case, focused control traceability, generated-artifact review, and the required
build/lint/test gates address those risks.

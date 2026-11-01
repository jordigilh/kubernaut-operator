# IEEE 829 Test Plan — Issue #563 monitoring trust boundary

| Field | Value |
|---|---|
| Test Plan ID | TP-563 |
| Issue | [kubernaut-operator#563](https://github.com/jordigilh/kubernaut-operator/issues/563) |
| Design decision | [`DD-563-MONITORING-TRUST-BOUNDARY.md`](../../design/DD-563-MONITORING-TRUST-BOUNDARY.md) |
| API line | `kubernaut.ai/v1alpha2` (unreleased clean break) |
| Scope | Monitoring CA API, validation, rendering, watches, rollout behavior, and HTTPS trust journey |
| Framework | Ginkgo v2/Gomega for operator business tests |

## 1. Objective

Prove that monitoring client trust is administrator-selected and independent of
Kubernaut's internal service CA:

1. An explicit Secret reference reaches EffectivenessMonitor, Kubernaut Agent,
   and API Frontend severity triage at the same read-only path.
2. An omitted reference uses system/container trust and does not select an
   internal-CA volume or monitoring CA file.
3. Invalid material fails closed with an actionable condition and does not
   replace a last-known-good Deployment template.
4. Valid Secret rotation rolls every monitoring consumer.
5. The generated v1alpha2 schema contains no removed source selector or
   ConfigMap reference.

Tests validate business outcomes; no test is added solely to increase a
coverage percentage.

## 2. Scope and non-goals

### In scope

- `spec.monitoring.tls.caCertSecretRef` name/key/default behavior.
- Secret validation, read-only projected volumes, deterministic paths, and
  non-sensitive revision annotations.
- EM, KA, and AF configuration consumers.
- Controller validation, Secret watches, reconciliation, and rotation.
- Generated CRD/schema, documentation contract, and Kind HTTPS journey.

### Out of scope

- Serving TLS under `spec.tls`.
- Internal service-to-service CA lifecycle except proving it remains separate.
- RBAC redesign; existing Secret access is reused.
- Formal FedRAMP, SOC 2, or ASVS certification.

## 3. Test strategy and pyramid

The pyramid invariant is mandatory:

- **UT proves logic:** API schema, Secret-only source resolution, validation,
  rendered ConfigMaps, volumes, and separation from internal CA mounts.
- **IT proves wiring:** envtest drives production `Reconcile` and verifies all
  three monitoring Deployments, conditions, watches, and rollouts.
- **E2E proves the journey:** Kind exercises certificate-verifying HTTPS calls,
  wrong-CA rejection, restoration, and system-trust behavior.

Targets:

- Unit business coverage: **at least 90%**, with the existing project gate
  preferred at 96% where business cases justify it.
- Controller integration coverage: **at least 78%**.
- No pending/skipped tests and no `time.Sleep`; use bounded `Eventually` or
  existing polling helpers.

## 4. Wiring manifest

| Component | Production entry point | Wiring location | Integration evidence |
|---|---|---|---|
| Secret-only monitoring volume | EM, KA, AF deployment builders | `internal/resources/common.go`, `deployments.go` | `IT-MON-563-001` |
| Monitoring CA config path | EM, KA, AF ConfigMap builders | `internal/resources/configmaps.go` | `IT-MON-563-001` |
| Secret validation | Validation phase before deployment | `phaseValidate` → `validateMonitoringTLS` | `IT-MON-563-002` |
| Secret rotation mapping | Secret event to singleton reconcile | `SetupWithManager` → `telemetrySecretToKubernaut` | `IT-MON-563-003` |

## 5. Unit test cases

| ID | Business behavior | Control objectives |
|---|---|---|
| `UT-MON-563-API-001` | Generated CRD exposes `caCertSecretRef` and omits `source` and `caCertConfigMapRef`; Secret key defaults to `ca.crt`. | FedRAMP CM-3/CM-6/SI-10; SOC 2 CC8.1; ASVS V16.1.1/V16.2.1/V16.5.2 |
| `UT-MON-563-RES-001` | A valid Secret reference renders one read-only `/etc/monitoring-tls/ca.crt` volume/mount and the same path in EM, KA, and AF config. | FedRAMP AC-6/SC-8/SC-12/SC-13; SOC 2 CC6.1; ASVS V12.3.1/V12.3.2/V13.3.1/V13.3.2 |
| `UT-MON-563-RES-002` | Omitted reference renders no monitoring CA volume, path, or revision; internal service CA remains a distinct mount where required. | FedRAMP SC-8/SC-13/SC-17; SOC 2 CC6.1/CC7.2; ASVS V12.3.1/V12.3.2/V13.3.4 |
| `UT-MON-563-VAL-001` | Omitted and valid references pass; empty/invalid name or key fails with the field path. | FedRAMP SI-10; SOC 2 CC8.1; ASVS V2.2.1/V2.2.2 |
| `UT-MON-563-VAL-002` | Non-PEM, non-CA, expired, trailing-invalid, missing Secret, and missing key fail closed. | FedRAMP SC-12/SC-13/SI-4/SI-10; SOC 2 CC7.2; ASVS V12.3.2/V13.3.4/V16.5.2 |
| `UT-MON-563-ROT-001` | Secret-backed material produces a non-sensitive revision digest; omitted trust produces no digest. | FedRAMP CM-3/SC-12/SI-4; SOC 2 CC7.2/CC8.1; ASVS V13.3.4/V16.5.2 |
| `UT-MON-563-WATCH-001` | Referenced Secret events enqueue the singleton; unrelated Secrets and ConfigMaps do not act as monitoring trust inputs. | FedRAMP AC-6/SI-4; SOC 2 CC6.1/CC7.2; ASVS V13.3.4 |

## 6. Integration test cases

| ID | Business behavior | Control objectives |
|---|---|---|
| `IT-MON-563-001` | Reconcile a valid Secret reference and verify EM, KA, and AF each mount the same Secret read-only, use the deterministic path, and receive a revision annotation. | FedRAMP AC-6/SC-8/SC-12/SC-13/SI-4; SOC 2 CC6.1/CC7.2; ASVS V12.3.1/V12.3.2/V13.3.1/V13.3.2 |
| `IT-MON-563-002` | Reconcile with monitoring endpoints enabled but no `caCertSecretRef`; verify system-trust configuration, no monitoring CA volume/path/revision, and unchanged serving/internal TLS resources. | FedRAMP SC-8/SC-13/SC-17; SOC 2 CC6.1/CC7.2; ASVS V12.3.1/V12.3.2/V13.3.4 |
| `IT-MON-563-003` | Replace a valid referenced Secret with invalid material; verify `MonitoringTLSNotReady`, actionable status, and unchanged last-known-good Deployment templates. | FedRAMP CM-3/CM-6/SC-12/SC-13/SI-4/SI-10; SOC 2 CC7.2/CC8.1; ASVS V2.2.1/V2.2.2/V16.5.2 |
| `IT-MON-563-004` | Restore valid Secret material; verify all three monitoring consumers roll and the Secret event maps to the singleton reconcile. | FedRAMP CM-3/SC-12/SC-13/SI-4; SOC 2 CC7.2/CC8.1; ASVS V13.3.4/V16.5.2 |
| `IT-MON-563-005` | Verify a valid monitoring Secret does not change serving TLS ownership, generated serving Secrets, or internal CA references. | FedRAMP AC-6/SC-12/SC-17; SOC 2 CC6.1/CC8.1; ASVS V13.3.1/V13.3.2 |

## 7. E2E test cases

| ID | Business behavior | Control objectives |
|---|---|---|
| `E2E-MON-563-001` | Kind private-CA HTTPS monitoring endpoint succeeds only after the referenced Secret is mounted; wrong CA fails; restoring the Secret restores the handshake. | FedRAMP SC-8/SC-13/SI-4; SOC 2 CC7.2; ASVS V12.3.1/V12.3.2/V13.3.4 |
| `E2E-MON-563-002` | A system-trusted HTTPS monitoring endpoint succeeds with no monitoring CA volume or source selector. | FedRAMP SC-8/SC-13/SC-17; SOC 2 CC7.2; ASVS V12.3.1/V13.3.4 |

## 8. Evidence and acceptance

Required evidence:

- `make test-unit` passes with unit business coverage at least 90%.
- `make test-integration` passes with controller coverage at least 78%.
- `make manifests generate` produces the expected v1alpha2 schema and no
  unrelated generated diff.
- `make test-pyramid`, build, and lint pass.
- The test IDs above are referenced in implementation test names and remain
  non-pending.

The mappings above are engineering evidence, not certification claims. Live
OpenShift/OVN qualification and external monitoring ownership remain deployment
responsibilities.

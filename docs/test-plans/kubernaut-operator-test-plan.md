# Kubernaut Operator Test Plan

**IEEE 829-2008 compliant**

| Field | Value |
|-------|-------|
| Test Plan Identifier | KO-TP-001 |
| Version | 1.0 |
| Date | 2026-03-29 |
| Author | Jordi Gil |
| Status | Active |

---

## 1. Introduction

### 1.1 Purpose

This document defines the test strategy, scope, resources, and schedule for
verifying the Kubernaut Operator. It ensures the operator reliably reconciles
the `Kubernaut` Custom Resource through its full lifecycle (creation, upgrade,
degradation, recovery, and deletion) and that every Kubernetes resource it
manages is correct, idempotent, and privilege-minimal.

### 1.2 Scope

| In Scope | Out of Scope |
|----------|--------------|
| Controller reconciliation loop (all phases) | Upstream Kubernetes/OpenShift platform bugs |
| RBAC resource correctness and privilege minimality | Kubernaut workload service business logic |
| Feature toggle conditional resource lifecycle | Third-party CRD integration (Tekton, Istio, Argo) |
| Resource builder output correctness | LLM provider API behavior |
| CRD installation and idempotency | UI/CLI tooling around the operator |
| Deletion and finalizer safety | |
| Status condition accuracy | |

### 1.3 References

- IEEE 829-2008, Standard for Software and System Test Documentation
- Kubernetes Operator Pattern (https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
- controller-runtime envtest documentation
- Kubernaut Operator source: `github.com/jordigilh/kubernaut-operator`

### 1.4 Glossary

| Term | Definition |
|------|-----------|
| CR | Custom Resource (instance of `Kubernaut` CRD) |
| CRD | Custom Resource Definition |
| CRB | ClusterRoleBinding |
| BYO | Bring Your Own (user-provided secrets) |
| envtest | controller-runtime test harness with embedded etcd + API server |
| SA | ServiceAccount |
| PDB | PodDisruptionBudget |

---

## 2. Test Items

### 2.1 Software Under Test

| Component | Path | Description |
|-----------|------|-------------|
| Controller | `internal/controller/kubernaut_controller.go` | Reconciliation loop: phases, status, finalizer |
| Resource Builders | `internal/resources/*.go` | RBAC, Deployments, Services, ConfigMaps, TLS, Webhooks, PDBs, Routes, CRDs |
| API Types | `api/v1alpha1/kubernaut_types.go` | CRD spec/status definitions |

### 2.2 Version

All tests target the current `main`/`fix/critical-review-p0-p3` branch at
commit `f7da6d4` or later.

---

## 3. Features to Be Tested

Each feature maps to a business outcome the operator must deliver.

### 3.1 Tier 1 -- Core Reconciliation (safety-critical)

| ID | Feature | Business Outcome |
|----|---------|-----------------|
| T1-01 | Phase progression (Validate -> Migrate -> Deploy -> Running) | Operator reliably brings a fresh CR to a healthy running state |
| T1-02 | Degraded detection and recovery | Operator detects partial failures and self-heals |
| T1-03 | Migration Job lifecycle (create, wait, fail, retry) | Database schema is migrated before services start |
| T1-04 | Deletion and finalizer cleanup | All operator-managed cluster-scoped resources are removed on CR delete |
| T1-05 | Singleton enforcement | Only one CR per namespace is reconciled; others are ignored |

### 3.2 Tier 2 -- Configuration Correctness

| ID | Feature | Business Outcome |
|----|---------|-----------------|
| T2-01 | Spec update propagation (ConfigMap, image tag, resources) | Live upgrades are applied without manual intervention |
| T2-02 | Feature toggle lifecycle (monitoring, ansible, route, SDK CM) | Conditional resources are created when enabled, removed when disabled |
| T2-03 | Owner reference propagation | Kubernetes GC cleans up namespaced resources automatically |
| T2-04 | BYO secret validation | Operator rejects invalid credentials early with actionable status |

### 3.3 Tier 3 -- RBAC and Security

| ID | Feature | Business Outcome |
|----|---------|-----------------|
| T3-01 | Namespace-prefixed ClusterRoles/CRBs | Multi-namespace deployments don't collide |
| T3-02 | Privilege minimality per component | No SA has broader access than its service requires |
| T3-03 | Workflow-runner scoped write access | Secrets/ConfigMaps write access limited to workflow namespace |
| T3-04 | Webhook config namespace-prefixing | Admission webhooks don't collide across instances |

### 3.4 Tier 4 -- Resource Builders

| ID | Feature | Business Outcome |
|----|---------|-----------------|
| T4-01 | Default port fallbacks (PostgreSQL 5432, Valkey 6379) | Zero-config deployments work out of the box |
| T4-02 | Image digest support | Deterministic container image pinning works |
| T4-03 | Pull secrets propagation | Private registry deployments work |
| T4-04 | Custom workflow namespace | Non-default workflow isolation works |
| T4-05 | Input validation (empty registry, missing tag) | Operator fails fast with clear errors |

---

## 4. Features Not Tested

| Feature | Rationale |
|---------|-----------|
| `SetupWithManager` wiring | Requires a running manager; verified via e2e tests |
| OpenShift Route TLS termination behavior | Platform-level; Route object creation is tested |
| Leader election | Requires multi-replica deployment; deferred to e2e |
| Actual database migration execution | Depends on external PostgreSQL; Job creation is tested |
| LLM provider connectivity | External dependency; ConfigMap/Secret wiring is tested |

---

## 5. Test Strategy

### 5.1 Test Levels

| Level | Tool | Purpose | Coverage Target |
|-------|------|---------|-----------------|
| **Unit** | Go `testing` | Resource builder correctness, RBAC privilege validation | >= 90% of `internal/resources` |
| **Integration** | envtest (Ginkgo/Gomega) | Controller reconciliation loop, phase transitions, status conditions | >= 75% of `internal/controller` |
| **E2E** | Ginkgo + real cluster | Full operator deploy/upgrade/uninstall (separate plan) | Smoke-level |

### 5.2 Coverage Targets and Actuals

| Package | Target | Actual | Status |
|---------|--------|--------|--------|
| `internal/resources` | >= 80% | **96.0%** | PASS |
| `internal/controller` | >= 80% | **78.4%** | GAP (see 5.3) |
| Combined | >= 80% | **89.4%** | PASS |

### 5.3 Coverage Gap Analysis

| Function | Coverage | Gap Reason | Mitigation |
|----------|----------|-----------|------------|
| `SetupWithManager` | 0% | Requires running manager, not envtest-compatible | Verified via e2e |
| `reconcilePhases` | 73.3% | Error paths in re-fetch after phase completion | Low risk: retried on next reconcile |
| `phaseMigrate` | 75.5% | Error branches (API failures) untriggerable in envtest | Retried on next reconcile cycle |
| `phaseDeploy` | 76.3% | Route creation path (requires routev1 in scheme) + error branches | Route toggle tested; OCP route tested in e2e |
| `deleteClusterScopedResources` | 72.0% | Error aggregation paths (all deletes succeed in envtest) | Happy-path completeness verified; all resource categories checked |
| `reconcileDelete` | 77.8% | Error returns from cleanup and Update | Low risk: retried on next reconcile |

### 5.4 Anti-Pattern Avoidance

| Anti-Pattern | Prevention |
|-------------|-----------|
| **Testing implementation, not behavior** | All assertions verify observable outcomes (status conditions, resource existence, phase values) not internal method calls |
| **Flaky time-dependent tests** | No `time.Sleep`; use Gomega `Eventually` with configurable timeout/interval |
| **Shared mutable state between tests** | `AfterEach` cleans all namespaced + cluster-scoped resources; `cleanupNamespacedResources` handles envtest GC gap |
| **Overly broad assertions** | Each test asserts specific fields (e.g., exact ClusterRole name, exact requeue duration) |
| **Missing negative tests** | Explicit tests for missing secrets, failed Jobs, disabled features, empty fields, invalid images |
| **Test-only code in production** | No test helpers in production code; `testKubernaut()` lives in `*_test.go` |
| **Ignoring errors** | All `Expect(err).NotTo(HaveOccurred())` on reconcile results |

---

## 6. Test Cases

### 6.1 Tier 1: Core Reconciliation

#### TC-T1-01: Phase Progression Happy Path

| Field | Value |
|-------|-------|
| Precondition | Valid BYO secrets exist; CR created with route disabled |
| Steps | 1. Reconcile (adds finalizer) 2. Reconcile (validates + starts migration) 3. Mark Job complete 4. Reconcile (migration done + deploy) 5. Mark all 10 Deployments ready 6. Reconcile (Running phase) |
| Expected | Phase=Running; BYOValidated=True; MigrationComplete=True; ServicesDeployed=True; 10 service statuses all Ready=true; RequeueAfter=60s |
| Test File | `kubernaut_lifecycle_test.go` |
| Tests | "should transition from Validating through Migrating to Deploying", "should reach Running phase", "should set per-service status for all 10 components", "should requeue after 60s when Running" |

#### TC-T1-02: Degraded State

| Field | Value |
|-------|-------|
| Precondition | CR at deploy phase; 9 of 10 Deployments ready |
| Steps | 1. Reconcile 2. Fix Deployment 3. Reconcile again |
| Expected | Phase=Degraded with RequeueAfter=15s; after fix, Phase=Running |
| Tests | "should report Degraded when a Deployment has insufficient replicas", "should recover from Degraded to Running" |

#### TC-T1-03: Migration Job Lifecycle

| Field | Value |
|-------|-------|
| Precondition | CR past validation; migration Job created |
| Steps | 1. Reconcile (Job in progress) 2. Mark Job failed 3. Reconcile (deletes Job) 4. Reconcile (recreates Job) |
| Expected | MigrationInProgress -> MigrationFailed (Job deleted) -> new Job created; RequeueAfter=10s while in progress |
| Tests | "should wait for migration Job to complete with requeue", "should delete a failed Job and requeue for retry", "should not duplicate the Job if it already exists" |

#### TC-T1-04: Deletion and Finalizer

| Field | Value |
|-------|-------|
| Precondition | CR at Running phase; cluster-scoped RBAC exists |
| Steps | 1. Delete CR 2. Reconcile deletion |
| Expected | All ClusterRoles/CRBs with managed-by label deleted; finalizer removed; CR garbage collected |
| Tests | "should clean up all cluster-scoped RBAC on deletion", "should skip deletion of workflow namespace not managed by operator", "should always attempt monitoring cleanup even when monitoring is disabled", "should always attempt AWX cleanup even when ansible is disabled" |

#### TC-T1-05: Singleton Enforcement

| Field | Value |
|-------|-------|
| Precondition | CR with non-singleton name created |
| Steps | 1. Reconcile |
| Expected | No finalizer added; no resources created; no status set |
| Tests | "should not create any resources for a non-singleton CR name", "should use namespace-prefixed names for cluster-scoped resources" |

### 6.2 Tier 2: Configuration Correctness

#### TC-T2-01: Spec Update Propagation

| Field | Value |
|-------|-------|
| Precondition | CR at Running phase |
| Steps | 1. Change spec field 2. Reconcile |
| Expected | ConfigMap data updated; Deployment image updated; resource limits applied |
| Tests | "should update ConfigMap data when spec changes", "should update Deployment image when spec.image.tag changes", "should apply custom resource limits from spec" |

#### TC-T2-02: Feature Toggle Lifecycle

| Field | Value |
|-------|-------|
| Precondition | CR at Running phase with monitoring/ansible enabled |
| Steps | 1. Disable feature 2. Reconcile |
| Expected | Feature resources deleted; re-enable recreates them |
| Tests | "should create monitoring RBAC when monitoring is enabled", "should delete monitoring RBAC when monitoring is disabled", "should create AWX RBAC when ansible is enabled", "should delete AWX RBAC when ansible is disabled", "should not generate SDK ConfigMap when sdkConfigMapName is set" |

#### TC-T2-03: Owner References

| Field | Value |
|-------|-------|
| Precondition | CR at Running phase |
| Steps | 1. Inspect namespaced resource |
| Expected | OwnerReference pointing to CR with correct UID and Kind |
| Tests | "should set owner references on namespaced resources" |

#### TC-T2-04: BYO Secret Validation

| Field | Value |
|-------|-------|
| Precondition | CR created with missing/invalid secrets |
| Steps | 1. Reconcile |
| Expected | Phase=Error; BYOValidated=False with specific reason |
| Tests | "should fail validation when PostgreSQL secret is missing", "should fail validation when Valkey secret is missing", "should fail when PostgreSQL secret is missing required keys", "should pass validation with correct secrets" |

### 6.3 Tier 3: RBAC and Security

#### TC-T3-01: Namespace-Prefixed Cluster Roles

| Field | Value |
|-------|-------|
| Precondition | CR in namespace "default" |
| Steps | 1. Deploy to Running |
| Expected | ClusterRole names prefixed with "default-" (e.g., "default-gateway-role") |
| Tests | "should use namespace-prefixed names for cluster-scoped resources" |

#### TC-T3-02: Privilege Minimality

| Field | Value |
|-------|-------|
| Precondition | Resource builders invoked |
| Steps | 1. Inspect ClusterRole rules |
| Expected | data-storage-client has only get/list (no create/delete); workflow-runner has no cluster-wide secrets access |
| Tests | `TestClusterRoles_ContainsExpectedNames`, `TestDataStorageClientRoleBindings_AllRefClusterRole`, lifecycle builder edge case tests |

#### TC-T3-03: Workflow Runner Scoped Access

| Field | Value |
|-------|-------|
| Precondition | WorkflowNamespaceRBAC invoked |
| Steps | 1. Inspect returned Roles |
| Expected | `workflow-runner-ns-writer` Role exists in workflow namespace with secrets/configmaps/services/PVC/networkpolicy/jobs write access |
| Tests | `TestWorkflowNamespaceRBAC_UsesDefaultNamespace`, `TestWorkflowNamespaceRBAC_UsesCustomNamespace`, "should use custom workflow namespace for RBAC resources" |

#### TC-T3-04: Webhook Namespace Prefixing

| Field | Value |
|-------|-------|
| Precondition | Webhook config builders invoked |
| Steps | 1. Inspect names |
| Expected | Names are `<namespace>-authwebhook-mutating` and `<namespace>-authwebhook-validating` |
| Tests | `TestMutatingWebhookConfiguration_Basic`, `TestValidatingWebhookConfiguration_Basic` |

### 6.4 Tier 4: Resource Builders

#### TC-T4-01: Default Port Fallbacks

| Field | Value |
|-------|-------|
| Precondition | CR with port=0 |
| Steps | 1. Build DataStorageDeployment |
| Expected | Init container uses `-p 5432`; ValkeyAddr returns `host:6379` |
| Tests | "should use default PostgreSQL port 5432 when not specified", "should use default Valkey port 6379 when not specified" |

#### TC-T4-02: Image Digest Support

| Field | Value |
|-------|-------|
| Precondition | CR with tag="" and digest="sha256:abc123" |
| Steps | 1. Build GatewayDeployment |
| Expected | Image contains `@sha256:abc123` |
| Tests | "should construct digest-based image references" |

#### TC-T4-03: Pull Secrets Propagation

| Field | Value |
|-------|-------|
| Precondition | CR with pullSecrets set |
| Steps | 1. Build Deployment |
| Expected | Pod spec contains ImagePullSecrets |
| Tests | "should propagate pull secrets to Deployments" |

#### TC-T4-04: Custom Workflow Namespace

| Field | Value |
|-------|-------|
| Precondition | CR with WorkflowNamespace="custom-wf" |
| Steps | 1. Build RBAC |
| Expected | Roles/RoleBindings in "custom-wf"; CRB subjects reference "custom-wf" |
| Tests | "should use custom workflow namespace for RBAC resources" |

#### TC-T4-05: Input Validation

| Field | Value |
|-------|-------|
| Precondition | CR with empty registry or empty tag+digest |
| Steps | 1. Build Deployment |
| Expected | Error returned with descriptive message |
| Tests | "should error when image registry is empty", "should error when both tag and digest are empty" |

#### TC-T4-06: Minimal CR Defaults

| Field | Value |
|-------|-------|
| Precondition | CR with only required fields |
| Steps | 1. Build all 10 Deployments |
| Expected | All produce valid resources with non-empty images and containers |
| Tests | "should produce valid resources with minimal required fields" |

---

## 7. Pass/Fail Criteria

### 7.1 Test Suite Level

| Criterion | Threshold |
|-----------|-----------|
| All unit tests pass | 100% |
| All integration tests pass | 100% |
| `internal/resources` statement coverage | >= 80% |
| `internal/controller` statement coverage | >= 75% |
| Combined statement coverage | >= 80% |
| Zero data races (`go test -race`) | 0 races |

### 7.2 Individual Test Level

A test passes when:
1. All `Expect()` assertions succeed
2. No panics or unrecovered errors occur
3. The test completes within the Ginkgo suite timeout (300s)
4. No goroutine leaks are detected

---

## 8. Test Deliverables

| Deliverable | Path |
|------------|------|
| This test plan | `docs/test-plans/kubernaut-operator-test-plan.md` |
| Unit tests (resources) | `internal/resources/*_test.go` (10 files, 2409 lines) |
| Integration tests (controller) | `internal/controller/kubernaut_controller_test.go` (365 lines) |
| Lifecycle integration tests | `internal/controller/kubernaut_lifecycle_test.go` (1248 lines) |
| Test suite harness | `internal/controller/suite_test.go` (120 lines) |
| E2E tests | `test/e2e/e2e_test.go` (330 lines) |
| Coverage reports | Generated via `go test -coverprofile` |

---

## 9. Test Environment

### 9.1 Unit Tests (`internal/resources`)

- **Runtime**: Go test runner (`go test`)
- **Dependencies**: None (pure Go, no external services)
- **Execution time**: ~1 second

### 9.2 Integration Tests (`internal/controller`)

- **Runtime**: envtest (embedded etcd + kube-apiserver)
- **Dependencies**: `bin/k8s/` binaries (installed via `make setup-envtest`)
- **CRDs**: Loaded from `config/crd/bases/`
- **Schemes**: `kubernautaiv1alpha1`, `apiextensionsv1`, `clientgoscheme`
- **Execution time**: ~30 seconds

### 9.3 E2E Tests (`test/e2e`)

- **Runtime**: Ginkgo + Docker + real cluster (OCP 4.21 or Kind)
- **Dependencies**: Docker daemon, `oc`/`kubectl`, built operator image
- **Execution time**: ~5 minutes

---

## 10. Test Execution

### 10.1 Commands

```bash
# Unit tests
go test ./internal/resources/... -v -count=1

# Integration tests
go test ./internal/controller/... -v -count=1

# All non-e2e tests with coverage
go test $(go list ./... | grep -v test/e2e) -coverprofile=coverage.out -count=1
go tool cover -func=coverage.out

# Race detector
go test ./internal/... -race -count=1

# E2E tests (requires running cluster)
go test ./test/e2e/... -v -count=1
```

### 10.2 CI Integration

Tests are executed on every pull request via GitHub Actions. The pipeline:
1. Runs `go build ./...`
2. Runs unit + integration tests with coverage
3. Fails the build if coverage drops below thresholds
4. Runs e2e tests against a Kind cluster (when Docker is available)

---

## 11. Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| envtest namespace deletion hangs | Tests that trigger CR deletion may leave namespaces in Terminating | Medium | `stripWorkflowNamespaceLabel()` helper prevents finalizer from deleting namespace in envtest |
| Job status validation strictness | K8s 1.30+ requires `SuccessCriteriaMet`/`FailureTarget` conditions | Medium | Test helpers set all required conditions |
| Cluster-scoped resource leaks between tests | Stale ClusterRoles from prior tests affect assertions | High | `cleanupClusterScoped()` in AfterEach deletes by managed-by label |
| Route tests require routev1 scheme | envtest doesn't include OpenShift types | Low | Route disabled in lifecycle tests; route creation tested separately |

---

## 12. Test Schedule

| Milestone | Date | Scope |
|-----------|------|-------|
| Unit tests complete | 2026-03-29 | All resource builders (96.0% coverage) |
| Integration tests complete | 2026-03-29 | Controller lifecycle (78.4% coverage, 50 specs) |
| Triage fixes validated | 2026-03-29 | P0-P2 issues verified by test suite |
| E2E tests on OCP 4.21 | Ongoing | Full operator deploy/upgrade/uninstall |

---

## 13. Approvals

| Role | Name | Date | Signature |
|------|------|------|-----------|
| Author | Jordi Gil | 2026-03-29 | |
| Reviewer | | | |

---

## Appendix A: Test Inventory

### A.1 Controller Integration Tests (47 total)

**Existing tests (10):**

| # | Context | Test |
|---|---------|------|
| 1 | Singleton Guard | should ignore a CR with a non-singleton name |
| 2 | Singleton Guard | should accept the singleton name 'kubernaut' |
| 3 | Finalizer Lifecycle | should add the finalizer on first reconcile |
| 4 | Finalizer Lifecycle | should remove the finalizer on deletion |
| 5 | BYO Secret Validation | should fail validation when PostgreSQL secret is missing |
| 6 | BYO Secret Validation | should fail validation when Valkey secret is missing |
| 7 | BYO Secret Validation | should fail when PostgreSQL secret is missing required keys |
| 8 | BYO Secret Validation | should pass validation with correct secrets |
| 9 | Phase Progression | should not have a NotFound error when CR does not exist |
| 10 | Status Conditions | should set ObservedGeneration on conditions |

**Lifecycle tests (37):**

| # | Context | Test |
|---|---------|------|
| 11 | Phase Progression | should transition from Validating through Migrating to Deploying |
| 12 | Phase Progression | should reach Running phase when all Deployments are ready |
| 13 | Phase Progression | should set per-service status for all 10 components |
| 14 | Phase Progression | should requeue after 60s when Running |
| 15 | Phase Progression | (existing: should not have a NotFound error) |
| 16 | Degraded State | should report Degraded when a Deployment has insufficient replicas |
| 17 | Degraded State | should recover from Degraded to Running when Deployments become ready |
| 18 | Migration Job | should wait for migration Job to complete with requeue |
| 19 | Migration Job | should delete a failed Job and requeue for retry |
| 20 | Migration Job | should not duplicate the Job if it already exists |
| 21 | Feature Toggles | should create monitoring RBAC when monitoring is enabled (default) |
| 22 | Feature Toggles | should delete monitoring RBAC when monitoring is disabled |
| 23 | Feature Toggles | should create AWX RBAC when ansible is enabled |
| 24 | Feature Toggles | should delete AWX RBAC when ansible is disabled |
| 25 | Feature Toggles | should not generate SDK ConfigMap when sdkConfigMapName is set |
| 26 | Spec Propagation | should update ConfigMap data when spec changes |
| 27 | Spec Propagation | should update Deployment image when spec.image.tag changes |
| 28 | Spec Propagation | should set owner references on namespaced resources |
| 29 | Spec Propagation | should apply custom resource limits from spec |
| 30 | Deletion | should skip deletion of workflow namespace not managed by operator |
| 31 | Deletion | should clean up all cluster-scoped RBAC on deletion |
| 32 | Deletion | should always attempt monitoring cleanup even when monitoring is disabled |
| 33 | Deletion | should always attempt AWX cleanup even when ansible is disabled |
| 34 | Concurrent | should handle status patch safely under concurrent spec changes |
| 35 | Singleton | should not create any resources for a non-singleton CR name |
| 36 | Singleton | should use namespace-prefixed names for cluster-scoped resources |
| 37 | CRD Install | should create workload CRDs during migration phase |
| 38 | CRD Install | should be idempotent on repeated EnsureCRDs calls |
| 39 | Builders | should use default PostgreSQL port 5432 when not specified |
| 40 | Builders | should use custom PostgreSQL port when specified |
| 41 | Builders | should construct digest-based image references |
| 42 | Builders | should propagate pull secrets to Deployments |
| 43 | Builders | should use custom workflow namespace for RBAC resources |
| 44 | Builders | should produce valid resources with minimal required fields |
| 45 | Builders | should error when image registry is empty |
| 46 | Builders | should error when both tag and digest are empty |
| 47 | Builders | should use default/custom Valkey port |

### A.2 Resource Unit Tests

| File | Tests | Coverage |
|------|-------|---------|
| `rbac_test.go` | 18 | ClusterRoles, CRBs, DataStorage bindings, Namespace roles, Workflow NS, Ansible, Monitoring, HolmesGPT client RB, Monitoring name enumerators |
| `deployments_test.go` | 20+ | All 10 deployment builders, image construction, volume mounts, volume source CM accuracy, security contexts |
| `configmaps_test.go` | 19+ | All ConfigMap builders, default values, YAML formatting, default Rego policies, Slack absence |
| `common_test.go` | 19+ | Labels, selectors, Image(), ObjectMeta, ValkeyAddr, MergeResources, SetOwnerReference, ServiceAccountName fallback, intPtrDefault |
| `services_test.go` | 8 | Service count, ports, authwebhook HTTPS, selector-to-component validation, PDB count/selectors |
| `serviceaccounts_test.go` | 3 | ServiceAccount per-component, WorkflowRunnerServiceAccount default/custom namespace |
| `webhooks_test.go` | 7 | MWC/VWC names, rules, paths, sideEffects, failurePolicy, labels |
| `ocp_test.go` | 9 | Route enabled/disabled, hostname, GatewayRouteStub, DataStorageDBSecret, WorkflowNamespace |
| `migration_test.go` | 6 | MigrationConfigMap, MigrationJob, backoffLimit, TTL, image |
| `tls_test.go` | 5 | TLS generation, CA bundle, DNS names, cert validity |
| `crds_test.go` | 4 | EnsureCRDs create, update, idempotency |

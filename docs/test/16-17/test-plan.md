# ConfigMap Drift Detection & AnsibleReady Condition Test Plan

**IEEE 829-2008 compliant**

| Field | Value |
|-------|-------|
| Test Plan Identifier | KO-TP-002 |
| Version | 1.0 |
| Date | 2026-04-27 |
| Author | Jordi Gil |
| Status | Draft |
| Related Issues | #16 (ConfigMap data corruption), #17 (AAP/Ansible validation) |

---

## 1. Introduction

### 1.1 Purpose

This document defines the test strategy, scope, approach, and acceptance criteria
for verifying two related features:

1. **ConfigMap drift detection** (issue #16): `ensureResource` must detect and
   correct external modifications to operator-managed ConfigMap data, even when
   the `kubernaut.ai/spec-hash` annotation is not modified by the external actor.

2. **AnsibleReady status condition** (issue #17): The operator must validate
   AAP/Ansible configuration (token Secret existence and key presence) and expose
   the result via a non-blocking `AnsibleReady` status condition on the Kubernaut CR.

### 1.2 Scope

| In Scope | Out of Scope |
|----------|--------------|
| `contentDrifted` function correctness | HTTP reachability checks to AAP controller |
| `ensureResource` dual-check (annotation + content) | AAP license validation |
| `AnsibleReady` condition lifecycle (all states) | Dedicated Secret watch (deferred per H1) |
| Non-blocking phase behavior (AnsibleReady does not gate Running) | OLM bundle regeneration |
| OCP service-CA ConfigMap safety (no false positives) | E2E tests on live OCP cluster |
| Event emission on Ansible config changes | Changes to the WFE service binary |

### 1.3 References

- IEEE 829-2008, Standard for Software and System Test Documentation
- [100 Go Mistakes and How to Avoid Them](https://100go.co/) (refactor checklist)
- Issue #16: `ensureResource` spec-hash check does not detect ConfigMap data corruption
- Issue #17: Operator should validate AAP/Ansible reachability and expose readiness
- KO-TP-001: Kubernaut Operator Test Plan (existing, `docs/test-plans/`)
- controller-runtime envtest documentation

### 1.4 Glossary

| Term | Definition |
|------|-----------|
| CR | Custom Resource (instance of `Kubernaut` CRD) |
| CM | ConfigMap |
| WFE | Workflow Execution (service component) |
| AAP | Ansible Automation Platform |
| BYO | Bring Your Own (user-provided secrets) |
| envtest | controller-runtime test harness with embedded etcd + API server |
| spec-hash | `kubernaut.ai/spec-hash` annotation for reconcile optimization |
| TDD | Test-Driven Development (Red-Green-Refactor cycle) |

---

## 2. Test Items

### 2.1 Software Under Test

| Component | File(s) | Version |
|-----------|---------|---------|
| `contentDrifted` function | `internal/controller/kubernaut_controller.go` | New |
| `ensureResource` (modified) | `internal/controller/kubernaut_controller.go` | Modified |
| `validateAnsibleConfig` method | `internal/controller/kubernaut_controller.go` | New |
| `phaseRunning` (modified) | `internal/controller/kubernaut_controller.go` | Modified |
| `ConditionAnsibleReady` constant | `api/v1alpha1/kubernaut_types.go` | New |
| Reason constants | `internal/controller/kubernaut_controller.go` | New |

### 2.2 Unchanged Dependencies (not retested)

- `SpecHash`, `ConfigMapDataHash` (`internal/resources/hash.go`)
- `WorkflowExecutionConfigMap` (`internal/resources/configmaps.go`)
- `validateSecret` helper (existing)
- envtest infrastructure (`internal/controller/suite_test.go`)

---

## 3. Features to be Tested

### 3.1 Tier 1 — ConfigMap Drift Detection (Issue #16)

| ID | Feature | Business Acceptance Criterion |
|----|---------|-------------------------------|
| D1 | Annotation-preserved data tampering | When an external actor modifies a ConfigMap's Data but preserves the spec-hash annotation, the next reconcile MUST restore the desired data. |
| D2 | OCP service-CA ConfigMap safety | ConfigMaps with `service.beta.openshift.io/inject-cabundle` annotation and empty operator-managed Data MUST NOT trigger false positive drift detection. |
| D3 | Key deletion detection | When an external actor deletes a desired key from a ConfigMap, the next reconcile MUST restore it. |
| D4 | No-op when no drift | When a ConfigMap's Data matches the desired state and the annotation matches, `ensureResource` MUST NOT issue an Update (ResourceVersion unchanged). |
| D5 | Non-ConfigMap types unaffected | `contentDrifted` MUST return `false` for Deployments, Services, and other non-ConfigMap types. |
| D6 | Nil Data safety | `contentDrifted` MUST NOT panic when existing ConfigMap has nil Data. |
| D7 | BinaryData comparison | `contentDrifted` MUST also compare BinaryData keys (future-proofing). |

### 3.2 Tier 2 — AnsibleReady Condition (Issue #17)

| ID | Feature | Business Acceptance Criterion |
|----|---------|-------------------------------|
| A1 | Disabled state | When `spec.ansible.enabled=false`, the `AnsibleReady` condition MUST be `True` with reason `Disabled`. |
| A2 | Valid configuration | When `spec.ansible.enabled=true` with a valid token Secret, the condition MUST be `True` with reason `Ready`. |
| A3 | Missing Secret | When `spec.ansible.enabled=true` and the referenced Secret does not exist, the condition MUST be `False` with reason `TokenSecretNotFound`. |
| A4 | Missing key in Secret | When the Secret exists but lacks the expected key, the condition MUST be `False` with reason `TokenKeyMissing`. |
| A5 | Nil tokenSecretRef | When `spec.ansible.enabled=true` but `tokenSecretRef` is nil, the condition MUST be `False` with reason `TokenSecretNotFound`. |
| A6 | Recovery | Creating the token Secret after initial failure MUST flip the condition to `True` on the next reconcile. |
| A7 | Non-blocking phase | The CR MUST reach `PhaseRunning` even when `AnsibleReady=False`. |
| A8 | Event emission | Ansible config status transitions MUST emit a Kubernetes event. |

---

## 4. Features Not to be Tested

| Feature | Rationale |
|---------|-----------|
| HTTP reachability to AAP controller | Deferred to follow-up issue; transient AAP downtime should not flip condition |
| Dedicated Secret watch | Dropped per adversarial review H1 (cluster-wide blast radius); rely on 60s requeueRunning |
| AAP license/subscription validation | Requires HTTP client; out of scope for config validation |
| OLM bundle validation | Separate CI pipeline concern |

---

## 5. Approach

### 5.1 Test Framework

- **Unit tests**: Go standard `testing` package for `contentDrifted`, `validateAnsibleConfig`
- **Integration tests**: Ginkgo/Gomega + envtest for full reconcile-loop behavior
- **Event assertions**: `events.FakeRecorder` channel drain pattern (established in codebase)

### 5.2 TDD Methodology

Each feature tier follows strict Red-Green-Refactor cycles. Each phase is a
single implementation commit for traceability.

### 5.3 Coverage Target

Minimum 80% of testable new/modified code per tier, measured by `go test -coverprofile`.

### 5.4 Go Anti-Pattern Avoidance

Tests MUST NOT exhibit these common Go test anti-patterns:
- **Shared mutable state between tests** (each `It` block creates its own CR and resources)
- **Testing implementation not behavior** (assert observable outputs: conditions, events, ResourceVersion)
- **Flaky timing dependencies** (no `time.Sleep`; use `Eventually`/`Consistently` or direct reconcile calls)
- **Ignoring cleanup** (use `AfterEach` with `cleanupResources`)
- **Over-mocking** (prefer envtest real API server over hand-rolled fakes for CRUD)
- **Missing error assertions** (every `Expect(err)` must have `.NotTo(HaveOccurred())` or `.To(HaveOccurred())`)

---

## 6. Item Pass/Fail Criteria

### 6.1 Pass Criteria

- All tests in Tier 1 and Tier 2 pass (`go test ./internal/...`)
- `golangci-lint` reports 0 new issues
- Coverage of new code >= 80% per tier
- TDD Refactor phase checklist (section 10) completed with no findings

### 6.2 Fail Criteria

- Any Tier 1 or Tier 2 test fails
- `contentDrifted` causes false positive drift on OCP-injected ConfigMaps (D2)
- `AnsibleReady=False` blocks CR from reaching `PhaseRunning` (A7)
- Regression in existing 65 tests

---

## 7. Suspension Criteria and Resumption Requirements

| Suspension Trigger | Resumption Requirement |
|--------------------|------------------------|
| envtest infrastructure broken (etcd/API server won't start) | Fix suite_test.go or upgrade controller-runtime |
| Upstream controller-runtime regression | Pin to last-known-good version |
| CRD schema change blocks CR creation in tests | Regenerate CRD manifests |

---

## 8. Test Deliverables

| Deliverable | Location |
|-------------|----------|
| This test plan | `docs/test/16-17/test-plan.md` |
| Unit tests for `contentDrifted` | `internal/controller/kubernaut_controller_test.go` |
| Integration tests for drift detection | `internal/controller/kubernaut_lifecycle_test.go` |
| Integration tests for AnsibleReady | `internal/controller/kubernaut_lifecycle_test.go` |
| Coverage report | `cover.out` (generated by `make test`) |

---

## 9. Testing Tasks — TDD Phases

### Phase 1: TDD Red — ConfigMap Drift Detection

Write failing tests BEFORE any production code changes.

| Test ID | Test Case | Expected Failure Reason |
|---------|-----------|------------------------|
| D1-R | `contentDrifted` returns true when desired key has different value in live CM | Function does not exist yet |
| D2-R | `contentDrifted` returns false for CM with empty desired Data (OCP inject-cabundle) | Function does not exist yet |
| D3-R | `contentDrifted` returns true when desired key is absent from live CM | Function does not exist yet |
| D4-R | `ensureResource` skips update when annotation and content match | Passes (existing behavior) — serves as regression guard |
| D5-R | `contentDrifted` returns false for Deployment type | Function does not exist yet |
| D6-R | `contentDrifted` returns false when live CM Data is nil but desired Data is empty | Function does not exist yet |
| D7-R | `contentDrifted` returns true when desired BinaryData key differs in live CM | Function does not exist yet |
| D1-I | Integration: reconcile restores CM data after annotation-preserved tampering | `ensureResource` skips update (current bug) |

**Files**: `internal/controller/kubernaut_controller_test.go`, `internal/controller/kubernaut_lifecycle_test.go`

### Phase 2: TDD Green — ConfigMap Drift Detection

Implement the minimum code to make all Phase 1 tests pass.

| Implementation Step | File |
|---------------------|------|
| Add `contentDrifted` function with ConfigMap type switch | `internal/controller/kubernaut_controller.go` |
| Use comma-ok assertion for type safety (L1 fix) | `internal/controller/kubernaut_controller.go` |
| Add BinaryData comparison loop (L2 fix) | `internal/controller/kubernaut_controller.go` |
| Update `ensureResource`: call `contentDrifted` when annotation matches | `internal/controller/kubernaut_controller.go` |

### Phase 3: TDD Refactor — ConfigMap Drift Detection

Validate production code against the 100 Go Mistakes checklist (relevant subset):

| Mistake # | Check | Status |
|-----------|-------|--------|
| #1 | No unintended variable shadowing in `contentDrifted` | |
| #2 | Happy path left-aligned; early returns for non-CM types | |
| #5 | No unnecessary interface; `contentDrifted` takes concrete `client.Object` | |
| #15 | Exported/new functions have godoc comments | |
| #21 | Slice initialization with known capacity (BinaryData keys) | |
| #22 | Nil vs empty slice handling in Data/BinaryData maps | |
| #30 | Range loop values are copies — OK for map iteration (read-only) | |
| #48 | No ignored errors from `bytes.Equal` or map lookups | |
| #53 | No goroutines in `contentDrifted` (synchronous, no concurrency needed) | |
| #78 | No `reflect.DeepEqual` when direct comparison suffices | |

### Phase 4: TDD Red — AnsibleReady Condition

Write failing tests BEFORE any production code changes.

| Test ID | Test Case | Expected Failure Reason |
|---------|-----------|------------------------|
| A1-R | CR with `ansible.enabled=false` has `AnsibleReady=True/Disabled` after reconcile | Condition type doesn't exist |
| A2-R | CR with valid ansible Secret has `AnsibleReady=True/Ready` after reconcile | Condition type doesn't exist |
| A3-R | CR with missing ansible Secret has `AnsibleReady=False/TokenSecretNotFound` | Condition type doesn't exist |
| A4-R | CR with Secret missing key has `AnsibleReady=False/TokenKeyMissing` | Condition type doesn't exist |
| A5-R | CR with `tokenSecretRef=nil` has `AnsibleReady=False/TokenSecretNotFound` | Condition type doesn't exist |
| A6-R | Creating Secret after failure flips condition on re-reconcile | Condition type doesn't exist |
| A7-R | CR reaches `PhaseRunning` when `AnsibleReady=False` | Condition type doesn't exist |
| A8-R | `AnsibleConfigInvalid` event emitted when Secret missing | No event emission logic |

**Files**: `internal/controller/kubernaut_lifecycle_test.go`

### Phase 5: TDD Green — AnsibleReady Condition

Implement the minimum code to make all Phase 4 tests pass.

| Implementation Step | File |
|---------------------|------|
| Add `ConditionAnsibleReady` constant | `api/v1alpha1/kubernaut_types.go` |
| Add reason constants (`Disabled`, `Ready`, `TokenSecretNotFound`, `TokenKeyMissing`) | `internal/controller/kubernaut_controller.go` |
| Implement `validateAnsibleConfig` returning `metav1.Condition` | `internal/controller/kubernaut_controller.go` |
| Integrate into `phaseRunning` `patchStatus` closure (non-blocking, single writer per M3) | `internal/controller/kubernaut_controller.go` |
| Emit events on Ansible config status transitions | `internal/controller/kubernaut_controller.go` |

### Phase 6: TDD Refactor — AnsibleReady Condition

Validate production code against the 100 Go Mistakes checklist (relevant subset):

| Mistake # | Check | Status |
|-----------|-------|--------|
| #1 | No variable shadowing in `validateAnsibleConfig` | |
| #2 | Happy path left-aligned; early return for disabled case | |
| #5 | No unnecessary interface for validation; direct method on reconciler | |
| #6 | No interface on producer side for condition builder | |
| #10 | No type embedding that leaks internal fields | |
| #15 | All exported constants and methods have godoc | |
| #48 | `r.Get` error checked; `secret.Data[key]` nil-safe | |
| #49 | Error wrapping uses `%w` for sentinel-compatible errors | |
| #53 | No goroutines; validation is synchronous in reconcile loop | |
| #54 | No race conditions; single writer for AnsibleReady (phaseRunning only) | |
| #73 | No misuse of `time` package (no time-dependent logic in validation) | |
| #78 | No `reflect.DeepEqual` for condition comparison; use `meta.FindStatusCondition` | |
| #80 | HTTP client not used (deferred); no resource leak risk | |

---

## 10. Environmental Needs

| Component | Requirement |
|-----------|-------------|
| Go | >= 1.25 (per go.mod) |
| envtest binaries | Kubernetes 1.31 (ENVTEST_K8S_VERSION) |
| golangci-lint | v2.11.4+ |
| Ginkgo CLI | v2 (optional; `go test` with Ginkgo runner suffices) |
| OpenShift API types | `github.com/openshift/api` (already in go.mod) |

---

## 11. Responsibilities

| Role | Person | Responsibility |
|------|--------|---------------|
| Test author | Jordi Gil | Write tests, implement code, run TDD cycles |
| Reviewer | (TBD) | Review PR for correctness and coverage |

---

## 12. Schedule

| Phase | Description | Estimated Duration |
|-------|-------------|--------------------|
| Phase 1 | TDD Red: Drift detection tests | 15 min |
| Phase 2 | TDD Green: Drift detection implementation | 20 min |
| Phase 3 | TDD Refactor: Drift code review vs 100-go-mistakes | 10 min |
| Phase 4 | TDD Red: AnsibleReady tests | 20 min |
| Phase 5 | TDD Green: AnsibleReady implementation | 25 min |
| Phase 6 | TDD Refactor: Ansible code review vs 100-go-mistakes | 10 min |
| Total | | ~100 min |

---

## 13. Risks and Contingencies

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| `contentDrifted` causes reconcile loop on OCP-injected CMs | High | Low (preflight verified empty Data) | D2 test covers this; guard on inject-cabundle annotation if needed |
| `Update` (PUT) clobbers OCP-injected data when drift detected | Medium | Low (only triggers on real tampering) | Document that inject-cabundle CMs with operator Data are unsupported |
| `validateAnsibleConfig` accidentally blocks phase via `setConditionAndRequeue` | High | Medium (developer error) | A7 test explicitly verifies PhaseRunning when AnsibleReady=False |
| Existing 65 tests regress | High | Low | Run full suite in each TDD Green phase |
| envtest flakiness with Secret CRUD timing | Medium | Low | Use direct reconcile calls, not watches |

---

## 14. Approvals

| Role | Name | Date | Signature |
|------|------|------|-----------|
| Author | Jordi Gil | 2026-04-27 | |
| Reviewer | | | |

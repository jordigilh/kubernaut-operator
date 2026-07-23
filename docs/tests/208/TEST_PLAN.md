# IEEE 829 Test Plan — Issue #208: Add Pod Security Admission 'restricted' labels to kubernaut-workflows Namespace

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-208                                             |
| **Issue**          | #208 — Add Pod Security Admission 'restricted' labels to kubernaut-workflows Namespace |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-07-23                                         |
| **Scope**          | `internal/resources/ocp.go`, `internal/controller/kubernaut_controller.go`, `internal/controller/kubernaut_controller_test.go` |

## 1. Objective

Companion to kubernaut `BR-WE-018`, which closes GAP-03 from the GA
Readiness Audit (kubernaut#1505). That work hardens the WE controller's
spawned Job/Tekton pods to a restricted `SecurityContext`, and the
Helm-managed `kubernaut-workflows` namespace gets a Pod Security Admission
(PSA) `restricted` namespace-label backstop as defense-in-depth, so the
Kubernetes API server independently rejects non-compliant pods even if the
controller's `SecurityContext`-authoring code ever regresses. The operator
builds its own `Namespace` object for `kubernaut-workflows` independently
of the Helm chart, in `WorkflowNamespace()`
(`internal/resources/ocp.go:251-262`), and today does not set these
labels.

A preflight check found a scope gap beyond the issue's stated acceptance
criteria (which only mention updating the `WorkflowNamespace()` builder):
the reconciler's `deployWorkflowNamespace()`
(`internal/controller/kubernaut_controller.go:514`) is **Create-only** --
it `Get`s the namespace and does nothing further if it already exists.
Fresh installs get the new labels correctly, but a `kubernaut-workflows`
namespace created by an older operator version would keep running
*without* the `restricted` PSA backstop indefinitely after an upgrade,
since nothing ever patches it. This is the same category of problem the
operator already solved for a different namespace (the AF/SPIRE app
namespace) via `ensurePSALabels()`/`ensureKagentiNamespaceLabel()`
(`internal/controller/kubernaut_controller.go:1484-1526`), which `Get`s
the namespace on every reconcile and patches in any missing PSA label.
Per product decision, this PR includes the same reconcile-time patch
behavior for `kubernaut-workflows`, closing the upgrade-path gap now.

**Explicitly out of scope**: the existing *privileged* SPIFFE-CSI
namespace labeling logic (`ensurePSALabels`, unrelated namespace/purpose)
and any Helm-chart-side change (this issue is operator-only; the Helm
chart already has its own equivalent). No new CR/CRD field is introduced
-- unconditional, matching kubernaut's non-configurable decision in
BR-WE-018.

## 2. Test Strategy

Standard TDD (RED -> GREEN -> REFACTOR), spanning the unit tier
(`internal/resources`: namespace builder; `internal/controller`:
fake-client reconciler unit tests for the patch-on-existing-namespace
behavior) plus an extension of an existing envtest integration assertion
to satisfy the Pyramid Invariant -- proving the reconcile loop actually
creates the live `kubernaut-workflows` namespace with the labels present,
not just via Go-level struct construction in unit tests.

## 3. Test Scenarios

### 3.1 Namespace builder (`internal/resources/ocp_test.go`, `WorkflowNamespace`)

Business objective: every fresh install gets the PSA `restricted`
backstop on `kubernaut-workflows` by default, unconditionally -- no
configuration required, no opt-out (CM-6: automated mechanism centrally
applies configuration settings; secure-by-default posture).

| ID      | FedRAMP | Description | Automated? |
|---------|---------|--------------|------------|
| WNS-001 | AC-4    | `WorkflowNamespace()` sets `pod-security.kubernetes.io/enforce`, `/audit`, and `/warn` all to `restricted` | Yes |
| WNS-002 | CM-6    | The PSA labels are present alongside the existing `CommonLabels()` (managed-by/part-of/instance), not instead of them | Yes |

### 3.2 Reconcile-time patch on pre-existing namespace (`internal/controller/kubernaut_lifecycle_test.go`, fake-client unit tests mirroring "Kagenti Namespace Label")

Business objective: an operator upgrade must converge an existing
`kubernaut-workflows` namespace (created by an older operator version) to
carry the `restricted` PSA backstop too, not just namespaces created
after the upgrade -- otherwise the defense-in-depth control silently does
not apply to any cluster that adopted the feature before this release
(AC-4: information flow/admission enforcement must apply uniformly,
independent of when the namespace was first created).

| ID      | FedRAMP | Description | Automated? |
|---------|---------|--------------|------------|
| WNS-010 | AC-4    | `deployWorkflowNamespace()` patches (`Update`s) an existing `kubernaut-workflows` namespace that is missing one or more of the 3 PSA `restricted` labels | Yes |
| WNS-011 | CM-6    | `deployWorkflowNamespace()` performs no `Update` call when an existing namespace already has all 3 labels set correctly (idempotent, no unnecessary writes) | Yes |
| WNS-012 | CM-6    | Patching preserves pre-existing unrelated labels on the namespace (no clobbering) | Yes |

### 3.3 No behavior change (regression)

| ID      | Description | Automated? |
|---------|--------------|------------|
| WNS-020 | Pre-existing `WorkflowNamespace()`/namespace-deletion fixtures (name resolution, `AnnotationCreatedBy`, custom namespace name) continue to pass unmodified | Yes |

### 3.4 Controller integration (`internal/controller/kubernaut_lifecycle_test.go`, envtest)

Business objective: prove the reconcile loop actually creates the live
`kubernaut-workflows` namespace with the PSA `restricted` labels present,
not just via Go-level struct construction in unit tests (Pyramid
Invariant: IT proves wiring).

| ID      | FedRAMP | Description | Automated? |
|---------|---------|--------------|------------|
| WNS-030 | AC-4    | An existing envtest lifecycle test that already asserts on the live `kubernaut-workflows` namespace is extended to also assert the 3 `pod-security.kubernetes.io/{enforce,audit,warn}=restricted` labels are present after reconcile | Yes |

## 4. Acceptance Criteria

- All scenarios above pass via `make test-unit` and `make test-integration`.
- `make lint` reports 0 issues.
- No CRD/CR field added or changed -- `make generate`/`make manifests` produce no diff.
- No behavior change for the SPIFFE-CSI `ensurePSALabels()`/`ensureKagentiNamespaceLabel()` path (different namespace, different PSA level, untouched).

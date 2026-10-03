# Issue #488 Gap-Closure Test and Implementation Plan

**Test Plan Identifier:** KO-TP-488-GAP-001
**Version:** 0.1
**Date:** 2026-10-02
**Status:** Approved scope — implementation in progress
**Branch baseline:** `fix/488-platform-neutrality` at `680baa2`
**Related PR:** [#490](https://github.com/jordigilh/kubernaut-operator/pull/490)

## 1. Purpose

This plan closes the evidence gaps identified after the generic, Cilium, and
Calico platform-neutrality implementation passed CI. It preserves the project
test pyramid:

> Unit tests prove logic. Integration tests prove controller/API wiring. E2E
> tests prove the real platform journey.

The plan does **not** claim FedRAMP authorization, OWASP ASVS conformance, or
OpenShift qualification from source inspection alone. Each control objective is
classified as `verified`, `partially verified`, `not verified`, or `not
applicable`, with a linked artifact and test result.

## 2. Baseline evidence

The current baseline has:

- green CI for build, lint, SBOM/vulnerability scan, unit/integration tests, and
  generic/Cilium/Calico Kind E2E;
- unit coverage of 88.1% and controller integration coverage of 78.0%;
- `make test-pyramid` checks for controller policy wiring, production-manifest
  Kind installation, CR creation, provider status assertions, and no direct
  policy application from the E2E package;
- live generic/Cilium/Calico lifecycle, enforcement, ownership, and cleanup
  evidence;
- no repository Helm chart;
- no live OpenShift/OVN qualification because the local `oc` environment is
  incomplete;
- generic Kind E2E coverage using explicit development self-signed TLS, but no
  live administrator-managed, cert-manager, or certificate-rotation journey;
- a control-objective table in
  `docs/design/ISSUE-488-IMPLEMENTATION-PLAN.md`, but no versioned,
  requirement-level ASVS evidence matrix.

## 3. Scope and priorities

| Gap | Priority | Outcome | Owner artifact |
|---|---:|---|---|
| OpenShift/OVN live qualification | P0 | Supported OCP/OVN path is tested through the real operator and native policy API | `test/e2e/openshift/`, CI or documented qualified-cluster run |
| Helm bootstrap and ownership | P0 if Helm remains in #488 | Bootstrap chart cannot create a `Kubernaut` CR or duplicate runtime ownership | `charts/`, Helm tests, clean-install evidence |
| TLS source and rotation qualification | P0 | Source selection, CA publication, overlap, failure, and no-plaintext behavior are proven | resource/controller tests plus Kind TLS lanes |
| FedRAMP/NIST traceability | P1 | Targeted controls have exact evidence links and residual-risk status | `docs/security/ISSUE-488-CONTROL-TRACEABILITY.md` |
| OWASP ASVS traceability | P1 | ASVS 5.0.0 requirements relevant to the operator are version-pinned and evidenced | same matrix and security tests |
| Pyramid gate strength | P1 | The gate checks real test-tier wiring, not only string presence | `Makefile`, CI, test metadata |
| Platform-support documentation | P2 | Documentation reflects the now-green Cilium/Calico CI qualification | `docs/installation/06-platform-support.md` |

### Approved scope decisions (2026-10-02)

- **OVN/OpenShift:** deferred; it remains explicitly unverified and is not a
  release claim for this work.
- **Helm:** the new dedicated operator bootstrap chart is a follow-up. The
  existing upstream dependencies chart may be reused for Kubernetes dependency
  fixtures, but no chart ownership is added in this work.
- **cert-manager:** implement a pinned Kind lane that exercises the real
  operator source and CA path.
- **OWASP ASVS:** use versioned OWASP ASVS 5.0.0 requirement IDs.

## 4. Acceptance criteria

### AC-488-GAP-01 — OpenShift and OVN

1. A supported OpenShift 4.19–4.22 cluster with OVN-Kubernetes is explicitly
   identified through the OpenShift `Network` configuration and active API
   discovery.
2. The operator submits only supported
   `policy.networking.k8s.io/v1alpha1` AdminNetworkPolicy and
   BaselineAdminNetworkPolicy objects.
3. Unsupported, ambiguous, stale, or unavailable provider states submit no
   policy and report actionable status/events.
4. The live lane verifies lifecycle, native policy submission, dataplane
   enforcement, unmanaged-policy preservation, and finalizer cleanup.
5. The lane is opt-in and credential-free from the repository: kubeconfig or
   `oc` authentication is supplied by the execution environment, never stored
   in GitHub or test fixtures.

### AC-488-GAP-02 — Helm bootstrap ownership

The dedicated operator chart is deferred to a follow-up. When that work starts,
the production chart must:

1. install only the operator, CRDs, RBAC, manager serving-certificate/webhook
   prerequisites, and chart-owned bootstrap resources;
2. never create a `Kubernaut` CR or duplicate operator-managed workloads,
   runtime Secrets, ConfigMaps, exposure objects, or provider policies;
3. expose bootstrap values without duplicating the v1alpha2 application schema;
4. provide a separately scoped, explicitly non-production dependencies chart if
   PostgreSQL/Valkey fixtures are included;
5. pass lint, render, clean install, upgrade, uninstall, and ownership checks;
6. prove the sequence `helm install → manager ready → prerequisites → user CR`
   on Kind.

### AC-488-GAP-03 — TLS sources and rotation

1. Administrator-managed, cert-manager, and explicit development self-signed
   modes select distinct, observable source paths.
2. Missing or invalid production material fails closed: no plaintext service,
   empty admission `caBundle`, or deletion of the last working trust root.
3. Administrator-managed Secrets are referenced, not adopted or overwritten.
4. cert-manager resources are used only when the API/issuer is present; the
   operator never installs cert-manager.
5. Rotation publishes new trust before replacing leaves and retains an overlap
   or coordinated restart until all consumers move.
6. Failed rotation leaves an actionable condition/event and preserves the last
   working material.
7. Tests assert generation/resourceVersion context and owner boundaries.

### AC-488-GAP-04 — Security control traceability

Create a versioned matrix that links each in-scope control objective to:

1. the requirement/design decision;
2. production code and wiring entry point;
3. unit, integration, and E2E test IDs where applicable;
4. CI or runtime evidence artifact;
5. status (`verified`, `partially verified`, `not verified`, or `N/A`);
6. residual risk and accountable follow-up.

The matrix must distinguish:

- NIST SP 800-53 Rev. 5 / FedRAMP control objectives used by this project;
- OWASP ASVS **5.0.0** requirement IDs, referenced in the versioned form
  `v5.0.0-<chapter>.<section>.<requirement>`;
- operator-specific non-goals, including the operator's log-based audit model
  versus platform-service persistent audit storage.

### AC-488-GAP-05 — Pyramid and wiring gate

The repository gate must verify that:

1. unit packages and their independent coverage threshold run;
2. controller envtest packages and their independent coverage threshold run;
3. the E2E harness installs production manifests, loads the operator image, and
   creates a real `Kubernaut` CR;
4. provider policy E2E does not call policy render/apply helpers directly;
5. every new adapter or builder in the wiring manifest has a production caller
   and a corresponding integration test;
6. unsupported tiers (OCP/OVN and Helm until enabled) are reported as
   `not verified`, not silently counted as complete.

## 5. TDD implementation sequence

### Phase A — RED: tests and evidence contracts first

1. Add failing unit tests for the approved TLS source/rotation and failure
   behavior; retain the existing OVN schema/ownership evidence as deferred
   follow-up work.
2. Add failing envtest cases for TLS readiness/rotation/failure paths. OVN
   discovery/reconciliation and live qualification are explicitly deferred.
3. Record Helm chart tests as deferred follow-up tests; do not add a partial
   operator chart or duplicate ownership in this work.
4. Add the control-traceability document with all rows initially marked
   `not verified` or `partially verified`; no row may be marked verified without
   a concrete test/evidence link.
5. Add failing assertions for the stronger pyramid gate.

### Phase B — GREEN: minimal implementation and wiring

1. Keep OVN/OpenShift implementation and live qualification deferred; retain
   the existing unit evidence and explicit unsupported/untested status.
2. Wire TLS rotation/readiness through the existing reconciliation path before
   adding optimizations.
3. Do not implement the dedicated operator Helm chart in this work. Reuse of
   the upstream dependencies chart remains a separate fixture concern.
4. Add the pinned cert-manager Kind lane; no credentials or cluster endpoints
   enter the repository.
5. Run the complete unit and integration suites after each green slice.

### Phase C — REFACTOR: production quality

1. Apply the Go anti-pattern checklist and preserve explicit error context.
2. Consolidate duplicated TLS/provider fixtures without weakening assertions.
3. Make control IDs and test IDs stable and searchable.
4. Update installation/security documentation and residual-risk entries.
5. Regenerate CRD, RBAC, bundle, and installer artifacts if affected.

## 6. Wiring manifest

| Component | Production entry point | RED test | Integration/E2E evidence |
|---|---|---|---|
| OVN detector/adapter | Deferred follow-up | Existing policy unit tests | `IT-POLICY-OVN-GAP-001`, `E2E-OCP-OVN-001` remain unverified |
| TLS source resolver | TLS/trust reconciliation | `UT-TLS-GAP-001` | `IT-TLS-GAP-001`, `E2E-TLS-GAP-001` |
| TLS rotation coordinator | trust publication before leaf rollout | `UT-TLS-ROTATION-GAP-001` | `IT-TLS-ROTATION-GAP-001` |
| Operator Helm bootstrap | Deferred follow-up | No tests added in this work | `IT-HELM-GAP-001`, `E2E-HELM-GAP-001` deferred |
| Control traceability gate | CI/security documentation check | `UT-CONTROLS-GAP-001` | CI evidence artifact |
| Pyramid verifier | `make test-pyramid` and CI job graph | `UT-PYRAMID-GAP-001` | unit + integration + E2E job results |

## 7. Test case inventory

### Unit tests

| ID | Expected behavior |
|---|---|
| `UT-POLICY-OVN-GAP-001` | Existing evidence: OVN active-installation and supported-version evidence is required; follow-up integration/live tests remain deferred |
| `UT-POLICY-OVN-GAP-002` | Existing evidence: ANP/BANP render with provider-native identity and deterministic ownership; live qualification remains deferred |
| `UT-POLICY-OVN-GAP-003` | Existing evidence: unsupported/ambiguous OVN state renders no policy; live qualification remains deferred |
| `UT-TLS-GAP-001` | All source modes resolve distinct artifacts and ownership |
| `UT-TLS-GAP-002` | Invalid source cannot produce plaintext or empty `caBundle` |
| `UT-TLS-ROTATION-GAP-001` | New trust overlaps old trust until consumers move |
| `UT-TLS-ROTATION-GAP-002` | Failed rotation preserves the last working root |
| `UT-HELM-GAP-001` | Deferred follow-up: chart values/templates contain no runtime workload or CR ownership |
| `CI-CONTROLS-GAP-001` | Every required traceability row has a valid code/test/evidence link or explicit gap status |
| `CI-PYRAMID-GAP-001` | Pyramid verifier rejects missing tier, direct policy calls, or orphaned wiring rows |

### Integration tests

| ID | Infrastructure | Expected behavior |
|---|---|---|
| `IT-POLICY-OVN-GAP-001` | Deferred follow-up | envtest + OpenShift Network/ANP/BANP schemas; no release claim until implemented |
| `IT-TLS-GAP-001` | envtest | TLS readiness, webhook CA publication, and source failure conditions are correct |
| `IT-TLS-GAP-003` | envtest | Generic TLS, trust ConfigMaps, webhook CA bundles, and Ingress remain wired without OpenShift annotations |
| `IT-TLS-ROTATION-GAP-001` | envtest | Trust-before-leaf ordering and failed-rotation preservation are observable |
| `IT-HELM-GAP-001` | Deferred follow-up | Helm render/install fixture for bootstrap ownership |

### E2E tests

| ID | Environment | Expected behavior |
|---|---|---|
| `E2E-OCP-OVN-001` | Deferred follow-up | OpenShift 4.19–4.22 with OVN; no release claim until qualified |
| `E2E-TLS-GAP-001` | Kind, explicit development TLS | Real admission/trust path succeeds without OpenShift annotations |
| `E2E-TLS-CERTMANAGER-001` | Kind + pinned cert-manager `v1.20.2` | Certificate source and CA publication work without operator-owned cert-manager |
| `E2E-TLS-CERTMANAGER-002` | Same cert-manager Kind lane | Leaf reissuance preserves Certificate ownership, TLS readiness, and webhook trust |
| `E2E-HELM-GAP-001` | Deferred follow-up | Kind + Helm bootstrap, manager readiness, user CR lifecycle, upgrade/uninstall ownership |

## 8. Control-objective scope

The first matrix shall cover at least these project-relevant NIST/FedRAMP
families, without claiming the full baseline:

| Objective | Required evidence |
|---|---|
| AC-3 / AC-6 | Exact generated RBAC, negative privilege assertions, provider ownership tests |
| SC-7 | Native-provider-only policy, no static CIDR/raw fallback, live enforcement |
| SC-8 / SC-12 / SC-13 / SC-17 | TLS source, trust, crypto, certificate publication and rotation evidence |
| IA-2 / IA-5 | Auth source, audience, secret reference and invalid-credential behavior |
| SI-4 / AU-2 / AU-3 / AU-12 | Structured logs/events/status with generation/resourceVersion context; operator audit non-goals documented |
| SI-10 | CRD/CEL and policy-input validation negative tests |
| CM-2 / CM-3 / CM-6 / CM-8 | Clean-break schema, generated artifacts, optional API inventory, Helm ownership |

OWASP ASVS 5.0.0 rows must use exact versioned requirement IDs from the
official standard rather than the current category-only `V1`/`V4` notation.
The source reference is:
<https://owasp.org/www-project-application-security-verification-standard/>.

## 9. Required gates

### Local gates

```text
go build ./...
golangci-lint run
make test-unit
make test-integration
make test
make test-pyramid
make manifests generate
git diff --exit-code config/ bundle/ dist/
```

### Environment-dependent gates

```text
make test-e2e-kind                         # generic/Cilium/Calico
KUBERNAUT_E2E_PROVIDER=... make test-e2e-kind
KUBERNAUT_E2E_TLS_SOURCE=certmanager make test-e2e-kind
make test-e2e                              # authenticated OpenShift
helm lint charts/kubernaut-operator       # after Helm scope approval
helm template ...
```

No environment-dependent gate may be represented as passing when its required
cluster or toolchain is unavailable.

## 10. Approval decisions required

The following decisions are architectural and must be approved before GREEN:

1. **Helm scope:** approved split follow-up. The existing upstream dependencies
   chart may be reused for Kubernetes dependency fixtures; a dedicated operator
   chart is a next step.
2. **OpenShift qualification:** approved deferment. OVN/OCP remains explicitly
   unverified for this release.
3. **cert-manager E2E:** approved pinned Kind lane.
4. **ASVS baseline:** approved OWASP ASVS 5.0.0 with exact versioned IDs.

## 11. Exit criteria

This gap-closure work is complete only when:

- the approved architectural decisions are recorded;
- RED/GREEN/REFACTOR and Checkpoint W pass for each approved workstream;
- every claimed control row has evidence or an explicit residual-risk status;
- generic/Cilium/Calico remain green after changes;
- approved OCP/OVN, Helm, and TLS lanes have passing evidence, or are explicitly
  deferred and removed from the release claim;
- generated artifacts and documentation are current;
- no formal FedRAMP or ASVS compliance claim is made without an external
  assessment and the required organizational evidence.

# Issue #486 Preflight, Spike, and Methodology Review

**Issue:** [kubernaut-operator#486](https://github.com/jordigilh/kubernaut-operator/issues/486)

**Date:** 2026-10-15

**Status:** Preflight and focused IT spikes complete; no new production
implementation started from this review.

**Confidence:** 94%

This document records the residual qualification scope after the platform-neutral
implementation work in #488, the operator bootstrap work in #489, and the
focused parity/safety work tracked by #512 and #514. It is an engineering
evidence record, not a FedRAMP authorization, SOC 2 attestation, or OWASP ASVS
conformance claim.

## 1. Scope boundary

The core implementation already present in this checkout includes:

- generic Kubernetes lifecycle and capability-gated OpenShift integration;
- generic TLS, webhook, trust, exposure, and monitoring contracts;
- native Cilium, Calico, and OVN policy adapters;
- no raw `NetworkPolicy` fallback for required native isolation;
- deterministic provider-policy ownership and cleanup;
- operator-only Helm bootstrap ownership;
- v1alpha2 clean-break API behavior.

This review does not reopen those design decisions. It distinguishes repository
IT evidence from optional live platform qualification.

## 2. Spike evidence

### Focused controller IT lane

Only targeted integration specs were run; the full integration suite was not
rerun after it was stopped. The following focused IT groups passed:

| Business behavior | Result | Evidence |
|---|---:|---|
| Generic manager starts without OpenShift/optional APIs | 1/1 | `internal/controller/platformneutrality` |
| Required isolation blocks migration when no provider is qualified | 1/1 | `required isolation through real reconciliation` |
| Cilium/Calico native policy submission and readiness | 5/5 | `native provider policy reconciliation wiring` |
| OVN DNS backend and terminal-deny wiring | 1/1 | `IT-AUDIT-518-DNS-001` |
| Optional monitoring reconciliation and unavailable-API behavior | 4/4 | `monitoring reconciliation wiring` |

All focused IT cases passed against the project-provisioned envtest 1.37
assets. No full-suite result is inferred from these targeted runs.

### Unit and static evidence

- `make test-unit`: passed with 6,502 independent business statements and
  100% project-measured independent business-statement coverage, above the
  current Makefile 90% gate. This is not a claim of 100% Go/package coverage;
  the full integration suite was stopped and its broader coverage was not used
  as a passing result.
- Security traceability validation: passed.
- CI-boundary validation: passed.
- The working tree was clean before the separate removal of the external OCP
  qualification harness.

### OpenShift capability and bootstrap observation

The external `helios08` environment contains two healthy SNO clusters:

- OpenShift 4.22.16;
- Kubernetes 1.35.8;
- OVN-Kubernetes;
- `AdminNetworkPolicy` and `BaselineAdminNetworkPolicy` at
  `policy.networking.k8s.io/v1alpha1`;
- Routes, service-CA, OpenShift monitoring, ServiceMonitor, PrometheusRule,
  and AlertmanagerConfig APIs.

An exact checkout image was manually used to verify the operator bootstrap
contract on an isolated cluster namespace. The observed assertions included
manager rollout, service-CA serving material, webhook CA publication,
singleton admission, upgrade certificate preservation, and uninstall/reinstall
behavior. A separate focused CR submission also rendered live OVN policy
objects: `AdminNetworkPolicy/kubernaut-admin`,
`AdminNetworkPolicy/kubernaut-agent-monitoring`, and
`BaselineAdminNetworkPolicy/default`.

This is **not** full application lifecycle or OVN dataplane qualification. The
manual run proves native policy submission/rendering only; policy enforcement,
workload readiness, and complete status-condition observation remain open. No
release claim is made for OpenShift/OVN policy enforcement. The manual cluster
procedure is external qualification evidence and is intentionally not retained
as a repository-owned harness.

#### External-run provenance

The focused manual run was executed on 2026-10-15 against `kubernaut492-sno-b`
on `helios08` (OpenShift 4.22.16, Kubernetes 1.35.8), from source checkout
`6479812`. The temporary image, namespaces, CRD, RBAC, policies, and logs were
removed after the observation. An immutable image digest and retained artifact
bundle were not produced; consequently this is a preflight spike receipt, not
durable release-qualification evidence. The cluster owner/release operator must
own any future retained qualification artifact and cleanup receipt.

## 3. Spike decisions

| Risk | Decision | Rationale |
|---|---|---|
| Generic/no-provider lifecycle | Code path is sufficiently evidenced for the current plan | Focused envtest proves optional API startup and required-isolation blocking; no provider fallback is rendered |
| Cilium/Calico policy wiring | No production change indicated by this spike | Focused IT proves deployment-phase submission, readiness, monitoring degradation, and native object behavior |
| OVN policy path | Keep dataplane qualification deferred | Focused IT and the manual OCP run prove DNS/policy intent and live ANP/BANP submission, but not dataplane enforcement |
| Full OCP application lifecycle | External release-qualification item | The available cluster proves platform/API availability, bootstrap behavior, and native policy submission, not complete operand readiness |
| Full integration-suite execution | No conclusion drawn | The run was stopped; targeted IT evidence is the valid result for this review |
| Repository OCP qualification harness | Do not retain as repository implementation | Qualification is environment-specific and manual; no shared CI lane is required by this scope |

## 4. Methodology review of the implementation plan

Reviewed plans:

- `docs/design/ADR-PLATFORM-001-implementation-plan.md`;
- `docs/design/ISSUE-488-IMPLEMENTATION-PLAN.md`;
- `docs/security/ISSUE-488-CONTROL-TRACEABILITY.md`;
- `docs/installation/06-platform-support.md`.

| Methodology requirement | Review result | Required correction |
|---|---|---|
| Preflight blast-radius analysis | Present and substantially complete | Record current residual scope separately from historical implementation work; this document does so |
| Time-boxed spikes with YES/NO outcomes | Previously implicit | Record explicit spike decisions and evidence; completed above |
| User approval before implementation | Present for the original ADR gates | Any new CRD, RBAC, phase, provider, or release-contract change still requires a new explicit approval |
| RED → GREEN → REFACTOR | Complete for historical implementation phases | Do not invent a new production implementation phase without a failing business test; remaining work is qualification unless a gap is found |
| Wiring manifest / Checkpoint W | Present, but some rows are stale | Focused IT now proves generic capability, provider-policy, monitoring, and OVN DNS callers; full OVN live qualification remains deferred |
| Test pyramid invariant | Present, but broad completion language is easy to misread | Keep focused IT, generic E2E, provider E2E, and OCP E2E as separate evidence classes; never substitute one for another |
| Overall Checkpoint W | Not complete for the full initiative | Focused IT proves selected callers, but unverified OpenShift/TLS/Helm rows and live dataplane evidence keep the overall wiring/completion gate open |
| Unit coverage ≥90% | Makefile enforces it | Current focused run measured 100%; retain business-case-only testing rule |
| FedRAMP/SOC 2/ASVS traceability | Matrix exists | Add current focused IT evidence and preserve partial status for live platform and organizational controls |
| Generated artifacts and completion gates | Present | Do not claim `make test` or full integration passed from this spike; record only executed gates |
| Risk mitigation and rollback | Under-specified for external qualification | Treat manual qualification as pre-authorized, isolated, and disposable; record cluster/version/source/image provenance and cleanup ownership, and do not make it a production dependency |

### Plan gap assessment

The plans are methodologically sound for the completed implementation, but they
should not be used as evidence that the remaining live qualification is closed.
The material gaps are evidence-management gaps, not identified production-code
gaps:

1. separate historical implementation checks from current focused validation;
2. explicitly label OVN/OpenShift live enforcement and full lifecycle as unverified;
3. state that no repository-owned OCP harness is part of this scope;
4. keep the 90% unit-coverage gate tied to independent business statements;
5. require a new approval and RED test before any additional production change.

## 5. Control-objective mapping for this review

| Business assertion | FedRAMP/NIST | SOC 2 | OWASP ASVS 5.0.0 (project-selected analog) | Evidence |
|---|---|---|---|---|
| Missing optional APIs do not block generic startup | CM-8, SI-4 | CC7, A1 | V16.5.2 | Platform-neutrality IT |
| Required isolation fails closed without a qualified provider | AC-3, SC-7, SI-10 | CC6.1, CC7.2 | V8.2.1, V13.2.1, V16.5.2 | Required-isolation IT |
| Native Cilium/Calico policies are submitted through reconciliation | AC-6, SC-7, CM-6 | CC6.6, CC8.1 | V8.3.1, V13.2.1 | Provider-policy IT |
| OVN DNS policy intent and native policy submission are wired through reconciliation | SC-7, SI-10 | CC6.1, CC7.2 | V13.2.1, V16.5.2 | `IT-AUDIT-518-DNS-001`; manual OCP 4.22 submission spike |
| Optional monitoring destinations fail visibly without unsafe fallback | SI-4, SI-10 | CC7.2 | V16.1.1, V16.5.2 | Monitoring IT |

The ASVS entries in this focused table are project-selected analog mappings for
engineering evidence; they are not direct conformance determinations. These are
repository engineering objectives only and do not establish formal compliance.

## 6. Revised next gate

No new production implementation is justified by the focused spikes. Before any
new code is proposed:

1. obtain explicit approval for the exact remaining behavior;
2. write a failing Ginkgo/Gomega business test;
3. update the wiring manifest and control matrix;
4. implement the smallest GREEN change;
5. run only the affected focused IT lane plus the required unit coverage gate;
6. run generated-manifest, build, lint, and traceability checks when applicable.

**Review confidence: 94%.** Residual uncertainty is limited to live provider
dataplane enforcement and complete OpenShift application lifecycle readiness.

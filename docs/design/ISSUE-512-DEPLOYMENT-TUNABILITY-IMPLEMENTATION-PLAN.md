# Issue #512: deployment tunability parity implementation plan

**Status:** Approved Option A; implementation complete and validation recorded
**Scope:** Kubernaut Operator v1.6 deployment-tunability parity slice
**Upstream reference:** `jordigilh/kubernaut@v1.6.0-rc9` chart
**Related issue:** [#512](https://github.com/jordigilh/kubernaut-operator/issues/512)

This document is an engineering implementation plan. Repository evidence is
not a FedRAMP authorization, SOC 2 attestation, or OWASP ASVS conformance
claim.

## 1. Preflight and parity findings

The audit compared the pinned upstream chart with
`api/v1alpha2/kubernaut_types.go`, `internal/resources/deployments.go`,
`pdb.go`, `hpa.go`, and `common.go`.

| Capability | Current operator | Classification |
|---|---|---|
| Component enablement | Existing enablement helpers for optional components | Covered, with intentional lifecycle differences |
| Per-component resources | Existing `Resources` fields and `MergeResources` | Covered; defaults are workload-specific |
| Pod/container hardening | Fixed restricted defaults | Partially covered; Helm permits component overrides |
| Replicas | `buildDeployment` fixes replicas at `1` | Unsupported; API field required |
| Node selection/tolerations | Not exposed | Unsupported; API fields required |
| Affinity | Fixed preferred hostname anti-affinity | Partially covered; API override required |
| Topology spread | Not exposed | Unsupported; API field required |
| PDB | Active components receive `maxUnavailable: 1` | Default covered; enablement and thresholds require API fields |
| HPA | DataStorage and APIFrontend are always reconciled | Intentional current behavior, but not Helm parity |
| HPA tuning | Fixed at min `1`, max `5`, CPU `75`, memory `80` | Defaults covered; enablement/tuning require API fields |

The Helm chart makes HPA creation disabled by default and supports per-service
replicas, PDB policy, scheduling, security-context overrides, and HPA tuning.
The current v1alpha2 API has no equivalent workload-tuning contract. Changing
that contract also changes availability and scaling behavior, so it requires
explicit API/design approval before implementation.

## 2. Recommended contract

Add an optional, typed workload-tuning block to each operator-managed operand
component. The exact field placement is the approval gate, but the effective
contract should cover:

- `replicas`;
- `nodeSelector`, `tolerations`, `affinity`, and
  `topologySpreadConstraints`;
- pod and container security-context overrides, merged with safe defaults;
- PDB `enabled`, `minAvailable`, and `maxUnavailable`;
- HPA `enabled`, `minReplicas`, `maxReplicas`, CPU target, and memory target;
- existing per-component `resources` without creating a second resource path.

Secure defaults must remain explicit. An override must not silently remove
required hardening, and invalid or contradictory PDB/HPA values must fail
validation before resource creation.

### Alternatives

| Option | Assessment |
|---|---|
| **A. Shared typed workload contract (recommended)** | One consistent API shape, centralized default resolution, lower drift, and direct mapping to existing builders. Requires CRD review and generated-artifact changes. |
| B. Direct fields duplicated on every component spec | Most literal chart mapping, but high schema and implementation duplication and greater drift risk. |
| C. Annotations or an untyped ConfigMap | Avoids a CRD change, but weakens validation, discoverability, ownership, and security evidence. Not recommended. |
| D. Preserve current behavior and document gaps | Safest short-term compatibility, but does not deliver the requested parity slice; HPA remains unconditionally enabled. |

## 3. TDD execution plan

### RED — business behavior first

Add only meaningful Ginkgo/Gomega tests, not coverage-only cases:

- deployment replicas and resource defaults/overrides;
- default restricted pod/container security and safe override behavior;
- node selector, tolerations, affinity, and topology spread rendering;
- PDB default, explicit disablement, and min/max threshold behavior;
- HPA disabled-by-default behavior, explicit enablement, and CPU/memory/min/max
  tuning;
- active/disabled component filtering;
- controller/envtest reconciliation proving the production builder path is used.

Proposed stable evidence IDs:

| Tier | IDs | Business behavior |
|---|---|---|
| Unit | `UT-TUNE-512-DEPLOY-001`–`006` | Deployment defaults, overrides, and scheduling/security fields |
| Unit | `UT-TUNE-512-PDB-001`–`003` | PDB defaults, disablement, and thresholds |
| Unit | `UT-TUNE-512-HPA-001`–`004` | HPA default, enablement, and tuning contract |
| Integration | `IT-TUNE-512-RECONCILE-001`–`003` | Reconciliation wiring, disabled components, and generated workload set |

Affected resource packages must retain at least 90% unit coverage, with
assertions tied to business behavior rather than line count.

### GREEN — smallest implementation

After API approval:

1. Add the approved optional v1alpha2 fields with backward-compatible defaults.
2. Regenerate deepcopy and CRD artifacts.
3. Thread effective values through the existing `DeploymentParams` and
   `buildDeployment` path.
4. Update existing PDB/HPA builders rather than adding parallel builders.
5. Wire the production reconciliation path and make the envtest cases pass.

The HPA default must be explicitly decided: recommended Helm parity is
`enabled: false` unless the CR opts in.

### REFACTOR — only after GREEN is passing

The refactor is behavior-preserving relative to the approved contract:

- centralize effective workload-default resolution instead of repeating
  component conditionals;
- reuse the existing `DeploymentParams`, `MergeResources`, PDB, and HPA paths;
- remove duplicated default/merge logic introduced during GREEN;
- keep secure pod/container defaults fail-closed and make weakening them
  explicit and reviewable;
- simplify branches with guard clauses and preserve actionable errors;
- verify no new parameter-heavy functions, shadowing, ignored errors, unsafe
  `any` usage, or test sleeps are introduced;
- do not add new API surface or unrelated components during REFACTOR.

## 4. Wiring manifest

| Component | Production entry point | Wiring location | Integration evidence |
|---|---|---|---|
| Workload policy resolution | Existing deployment aggregation | `internal/controller/kubernaut_controller.go` deploy phase and `internal/resources/deployments.go` | `IT-TUNE-512-RECONCILE-001` |
| PDB policy | Existing `resources.PodDisruptionBudgets` call | `internal/controller/kubernaut_controller.go` | `IT-TUNE-512-RECONCILE-002` |
| HPA policy | Existing HPA aggregation | `internal/controller/kubernaut_controller.go` | `IT-TUNE-512-RECONCILE-003` |

Checkpoint W requires every new policy path to be exercised through
reconciliation; unit-only builder tests are insufficient.

## 5. Control-objective traceability

The evidence matrix will use identifiers already used by this repository:

| Assertion area | FedRAMP/NIST | SOC 2 | OWASP ASVS 5.0.0 |
|---|---|---|---|
| Secure workload defaults and least privilege | `AC-6`, `CM-6`, `CM-7` | `CC6.1`, `CC6.6` | `v5.0.0-V15.1.1`, `v5.0.0-V15.1.2`, `v5.0.0-V15.2.1` |
| Availability-aware replicas/PDB/HPA behavior | `CM-6`, `SI-4`, `SA-11` | `CC7.2`, `A1` | `v5.0.0-V16.5.2` |
| Reproducible API and generated artifacts | `CM-2`, `CM-3`, `CM-6`, `CM-8` | `CC8.1` | `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1`, `v5.0.0-V16.5.2` |

These mappings describe engineering evidence only. Live platform scheduling,
cluster admission, image provenance, organizational change approval, and
production availability remain residual deployment responsibilities.

## 6. Validation and delivery gates

After implementation authorization:

- affected Ginkgo/Gomega resource tests and controller envtest;
- Helm chart render/lint tests and parity assertions;
- `go build ./...`;
- `golangci-lint run`;
- `make manifests generate` and generated-artifact diff review;
- pyramid and traceability checks, including `hack/verify-test-pyramid.sh`;
- applicable Kind production-manifest evidence;
- report envtest, Kind, OpenShift, and live-cluster limitations honestly.

Only the intended implementation files will be committed and pushed. No pull
request will be created or merged by this task.

## 7. Implementation progress

The approved contract is implemented through the existing builder and
reconciliation paths. RED tests failed against the absent API, GREEN added the
typed contract and minimal wiring, and REFACTOR centralized effective defaults,
security-preserving merges, and stale-resource cleanup. Focused resource tests,
controller envtest, build, lint, Helm, and both Kind lanes pass. Generated
deepcopy, CRD, chart, bundle, and `dist/install.yaml` artifacts are refreshed.
The repository-wide independent business-coverage gate remains blocked by 42
missed statements, primarily existing controller branches exercised only by
envtest; affected `internal/resources` coverage is 98.9%.

## 8. Reassessment

- **Parity-audit confidence: 96%.** The chart defaults and operator builders
  were compared directly, including the HPA enablement mismatch.
- **Implementation-plan confidence: 93%.** The existing builder and
  reconciliation paths are known; the remaining uncertainty is the approved
  v1alpha2 field shape and security-override semantics.
- **Implementation confidence: 95%.** The approved API is wired through the
  existing resource builders and reconciler; focused unit tests, 79.3% controller
  envtest coverage, build, lint, Helm, and Kind evidence pass. Remaining risks
  are the pre-existing repository-wide business-coverage gate and live
  scheduler, metrics-server, SCC/PSA, and production-availability evidence.

The user approved Option A and the Helm-compatible disabled-by-default HPA
availability contract; no further design decision is pending.

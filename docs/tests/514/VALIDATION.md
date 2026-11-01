# Issue #514 — Implementation handoff evidence

**Date:** 2026-10-10

**Branch:** `fix/514-resource-ownership`

**Base:** `fa46f90` (merged #515 / #513)

**Scope:** P0 runtime ownership safety under #512; #420 remains subsequent work.

The implementation follows the approved ownership and namespace-retention
contract. No public CRD field, reconciliation phase/condition type, RBAC rule or
OLM bundle change was introduced. Neither the CR namespace nor workflow namespace
is an operator cleanup target. This record covers PR #517 remediation evidence;
release qualification remains separate.

## TDD evidence

Evidence logs are retained in the session's `opencode` temporary directory, not
committed: `/private/var/folders/r7/gktmmltd1zq7wqhsjjslwsm80000gn/T/opencode/`.

| Regression | RED evidence | GREEN evidence / business result |
|---|---|---|
| Namespace-wide indirect deletion | `514-red-namespace-content.log` | `IT-OWN-514-015`: retain namespace and unmarked administrator content |
| Restricted PSA overriding foreign namespace identity | `514-red-workflow-identity.log` | `IT-OWN-514-016`, `017`: reject foreign identity; reuse a prepared administrator namespace without changing it |
| Foreign webhook CA copied before authorization | `514-red-webhook-trust.log`: expected ownership error, got nil | `514-green-webhook-trust.log`; `IT-OWN-514-018`: authorize the observation before preserving CA/metadata |
| Provider markers overriding foreign owners | `514-red-provider-unit.log`: two failing unit cases; `514-red-provider-wiring.log`: update/prune/finalizer failures | `514-green-provider-unit.log`, `514-green-ownership-final.log`; `UT-OWN-514-005` and `IT-OWN-514-019`: retain all five markers, preserve foreign policies and repair only same-identity owners |
| CRD conversion/configuration error paths | Focused Ginkgo cases cover invalid dynamic-client configuration, nested YAML conversion, malformed YAML, and non-object YAML | `UT-OWN-514-004` expanded; resources package rerun: **96.1%**; `EnsureCRDs` **100.0%**, `ensureSharedCRD` **100.0%**, `yamlToUnstructured` **100.0%** |

Unit logic runs independently of the controller/envtest package. Integration
contracts drive actual `Reconcile`, including completed administrator migration
Jobs, Certificates, create races, UID/version deletion races and matching-hash
ownership changes. Only external Kubernetes API/cache behavior is injected.

## Host gates

| Gate | Latest result / artifact |
|---|---|
| `go build ./...` | Passed after final refactor |
| `golangci-lint run` | Passed; `514-final-lint-green.log` |
| `go test ./... -run=^$ -timeout=30s` | Passed; `514-final-compile-only.log` |
| `make manifests generate`; `git diff --exit-code config/ api/v1alpha2/zz_generated.deepcopy.go` | Passed; generated CRD/RBAC/deepcopy unchanged; `514-final-codegen.log` |
| `make test` | Passed on the final remediation checkout; resources **96.1%**, all internal unit packages **93.6%**, controller integration **82.2%**, Helm **11/11**, and no failed/pending/skipped specs in the executed gates. GitHub Unit & Integration Tests also passed after the pipefail-safe coverage-gate fix in run `38109685938`. |
| `make test-unit` after the coverage backfill | Passed; internal unit coverage **93.6%**, resources **96.1%**, all 12 business entry points **100%** |
| `make test-hack-scripts test-pyramid` | Passed; `514-final-static-gates.log` |

Earlier `514-final-make-test.log` failed because Kubernetes 1.37 rejects a Job
marked Complete without start/completion times and SuccessCriteriaMet. The new
foreign-Job test now uses the existing valid completed-Job fixture helper. The
subsequent full run passed, but predates the last webhook/provider refinements;
only the final full-run artifact can qualify the handoff source.

Earlier lint failures were fixed without suppressing security checks or reducing
thresholds: namespace predicate extraction, shorter diagnostic lines, a namespace
constant rename avoiding a credential-name false positive, and centralized
provider conditional cleanup.

Ownership/shared-CRD authorization functions have **100% independent unit
statement coverage**; the business coverage gate additionally enforces the
existing monitoring/TLS entry points. Aggregate resource-package coverage now
meets the methodology's 96% target at **96.1%**; the unchanged configured unit
gate is 80% across `internal/`, with controller integration gated at 78%.
Passing configured gates does not turn coverage into proof of all branches.

Final pre-remediation coverage: resources **90.1%**, policy **84.5%**, all
internal unit packages **88.9%** (80% configured floor), controller integration
**81.8%** (78% floor). The remediation adds focused CRD error/conversion and
resource/TLS helper coverage without exclusions or a lowered threshold. The
final remediation run reports resources **96.1%**, policy **84.5%**, all internal
unit packages **93.6%**, and controller integration **82.2%**; the resources
package's 96% project target is met.
All **12** gated business entry points passed at **100%**; six are the #514
ownership/shared-CRD functions. No coverage threshold was reduced.

## Live journey evidence and blockers

- `514-kind-final-source.log`: real Helm-installed operator, final source image
  `localhost/kubernaut-operator:ownership-514-final` built from this checkout,
  **8/8 specs passed**, zero failures/pending/skips (293.503 seconds). This includes ownership conflict,
  failed-install uninstall, recovery, upgrade, marked reinstall, TLS rotation,
  retained namespace UIDs, workflow ConfigMap and DB/Valkey/input witnesses.
- The earlier retention-image run is historical only; the final-source run above
  qualifies the installed Kind contract after the webhook/provider refinements.
- Podman initially failed with exit 125: **`no space left on device`** while
  writing Go build-cache layers and overlay metadata. Capacity was restored by
  removing only stale #514/operator-tagged images and recent dangling layers;
  no broad prune, VM restart or unrelated containers/images were removed.
- Final-source OpenShift/OVN and upstream exact-SHA application-service
  qualification are separate live lanes. The Kind contract fixtures do not
  establish them; see the platform evidence section below for exact attempts.

### SNO OVN attempt

- `514-ovn-e2e-remediation.log` records the exact-source attempt on
  `kubernaut492-sno-a`: OpenShift **4.22.16**, Kubernetes **1.35.8**,
  `OVNKubernetes`, source commit **`46d74bd`**, and internal-registry image
  `image-registry.openshift-image-registry.svc:5000/kubernaut-operator-system/kubernaut-operator@sha256:c4a08513cab4a214c05b2ed9ce88fbf5746db25edb9c9953e954a52ff8142a95`.
- The executed lane ran `make manifests generate` followed by
  `IMG=<immutable-image> go test ./test/e2e/ -v -ginkgo.v -timeout 30m` from
  the remediation checkout. It ran from **2026-10-11T03:15:08Z** through
  **2026-10-11T03:26:37Z** and returned **1**: **3 passed, 1 failed, 0
  pending, 16 skipped** out of 20 ordered specs.
- The failed spec, `should reach Running phase (all services deployed)`, timed
  out after 600 seconds because `ServicesDeployed` never became `True`.
  Controller logs repeatedly reported the ownership contract rejecting the
  pre-existing unmarked shared CRD `actiontypes.kubernaut.ai`:
  `existing manager="" owners=[]`. The CRD was not implicitly adopted,
  transferred, or deleted.
- This is **not SNO qualification evidence**. The lane must be rerun on a
  clean cluster or after an administrator-approved explicit shared-CRD transfer;
  the deny-by-default behavior is intentional. No upstream exact-SHA
  application-service qualification was run, so that lane also remains
  unqualified.

## Platform and CI evidence

- A later source head, `67d0a7bbbda6396c48a7f56c1c66c53b537a250a`, failed
  the Hook TLS Kind job in run `38112936915`. The startup-only OpenSSL fixture
  retained a pre-reinstall serving certificate while projected trust/material
  changed. See [the root-cause analysis](CI-HOOK-TLS-RCA.md) for exact failure,
  deterministic RED/GREEN evidence and bounded convergence/current-leaf checks.
  First corrective run `38135457048` passed that initial TLS check, then exposed
  fixed-name probe reuse during pending deletion in the new leaf-rotation check.
  The follow-up uses fresh server-assigned Pod names, with real-API RED/GREEN
  lifecycle evidence and a persistent identity regression. Its new hosted run
  remains pending. Earlier green CI below is historical, not verification of
  either later failed head or the follow-up correction.
- GitHub PR #517 validation run
  [`38109685938`](https://github.com/jordigilh/kubernaut-operator/actions/runs/38109685938)
  for source head `060b851d4268c138db8e390db7f2027842aa27d2` completed
  successfully with **14 successful** checks; `auto-merge` was skipped and no
  check failed. The checks included Cilium, Calico, TLS, Helm, SBOM, unit,
  integration, and operator-image build checks.
- The SNO attempt above is recorded with its command, timestamps, cluster and
  component versions, immutable image SHA, result, and blocking condition. A
  lane is marked verified only when it passes; failed or unavailable lanes
  remain explicit qualification blockers.

## Control-objective interpretation

The versioned NIST SP 800-53 Rev. 5/FedRAMP, SOC 2 and OWASP ASVS 5.0.0 matrix is
`docs/security/ISSUE-514-CONTROL-TRACEABILITY.md`. Business observations prove the
operator's authorization, preservation, identity/concurrency and diagnostic
boundaries. Static traceability checks are not live execution or formal
compliance evidence. Administrator permissions on markers, schema-transfer
approval, log aggregation/retention and cluster Secret protection remain external
responsibilities. Engram project recall/search recovered during the handoff;
source-backed evidence remains authoritative and no infrastructure maintenance
was added to this change.

**Confidence: 96%** in the implementation-to-commit handoff. The final host
suite, independent authorization coverage, real reconciliation/race tests,
final-source Kind journey and source wiring inspection support the ownership
boundary. The residual risk is the unqualified non-Kind live lanes; this is not
a GA/release readiness declaration.

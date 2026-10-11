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
| CRD conversion/configuration error paths | Focused Ginkgo cases cover invalid dynamic-client configuration, nested YAML conversion, malformed YAML, and non-object YAML | `UT-OWN-514-004` expanded; resources package rerun: **90.4%**; `EnsureCRDs` **100.0%**, `ensureSharedCRD` **100.0%**, `yamlToUnstructured` **100.0%** |

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
| `make test` | Passed on remediation source; `514-make-test-remediation.log`; controller suite 278.318 seconds |
| `make test-security-traceability test-hack-scripts test-pyramid` | Passed; `514-final-static-gates.log` |

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
existing monitoring/TLS entry points. Aggregate resource-package coverage remains
below the methodology's 96% target; the unchanged configured unit gate is 80%
across `internal/`, with controller integration gated at 78%. Passing configured
gates does not waive the higher target or turn coverage into proof of all branches.

Final pre-remediation coverage: resources **90.1%**, policy **84.5%**, all
internal unit packages **88.9%** (80% configured floor), controller integration
**81.8%** (78% floor). The remediation adds focused CRD error/conversion
coverage and reruns the scoped changed-business-logic gate; aggregate historical
coverage debt is not hidden by exclusions or a lowered threshold. The current
remediation run reports resources **90.4%**, policy **84.5%**, all internal unit
packages **89.1%**, and controller integration **82.2%**. The resources package's
96% project target remains an explicit residual gap requiring broader non-#514
test work.
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

## Platform and CI evidence

- GitHub PR #517 check-runs for head `0e80af91c12c7d5402587650c85b99b3c1bebf30`
  were **15/15 successful**, including Cilium, Calico, TLS, Helm, SBOM, unit,
  and integration checks. The legacy combined status is misleadingly pending
  with zero statuses; it is not treated as a test failure.
- SNO platform attempts and their exact cluster/image evidence are recorded in
  the remediation run log. A lane is marked verified only when its command,
  timestamp, cluster/operator/application versions and immutable image SHA are
  available; unavailable lanes remain explicit blockers with an owner and next
  action.

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
boundary. Residual risks are unqualified non-Kind live lanes and the aggregate
resource coverage target; this is not a GA/release readiness declaration.

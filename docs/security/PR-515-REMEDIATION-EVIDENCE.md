# PR #515 remediation: bounded verification evidence

**Date:** 2026-10-11<br>
**Branch:** `fix/515-audit-gap-closure`<br>
**Status:** full local verification and the committed-snapshot rerun pass;
five logical code groups committed. Ownership dependency
#517 is open; remediation is not pushed and no remediation PR exists yet

## Snapshot and assurance boundary

The original audit remains **NO-GO**, bound to reviewed head
`3f4d91b91a7cb529fdea1e7191a5e176965cb465`, base
`aa0b00257dc68fe8118880b9040aac30f1c5d1cc`, merge
`fa46f90d5e1ea205bd3a4712f73866c5faf74c6d`, and tree
`170063de42ecbd98bb2d98b666aba77d1c08dad9`. New remediation evidence must not
rewrite that snapshot's findings or be attributed to the original merged PR.

This working branch starts at the merge SHA above. The original results below
apply to the then-**uncommitted remediation**, not an immutable release or hosted
CI revision. The original execution-input digest is
`17a7b30a1ff8f32f4390d12820e4e822d29fa858a2246d5e5467df8547a6b6b6`
(126 files: all tracked/nonignored Go source, `go.mod`, `go.sum`, `Makefile` and
the test workflow; sorted path, NUL, bytes, NUL, SHA-256). The local artifact
`remediation-test-inputs.json` inventories per-file hashes. Documentation is not
part of that execution-input digest. A committed/published candidate needs new
exact-SHA verification after integrating #517.

## Logical commits and publication gate

The user requested logical commits, a push after completion, and CI monitoring
with failures addressed. The verified implementation is organized as follows:

| Commit | Scope |
|---|---|
| `2743b22` | Explicit webhook/metrics TLS protocols, patched Go floor, strict telemetry-chain validation and uncached admission reads |
| `75f0ae4` | Stable current-owner-qualified issuer discovery and shared checked TLS material validation |
| `e4013e6` | Persisted singleton activation, live Service convergence and truthful reconciliation/cleanup traces |
| `6c1bf08` | Required/Inherited isolation, bounded scraper identities, generated-port wiring, CRD/chart/bundle and migration contract |
| `58a0e15` | Independent exhaustive business coverage, real-API wiring, checked generation boundaries and honest evidence tiers |

Each staged code snapshot was separately built and all packages test-compiled
before committing; focused executable regressions were run for its scope.
The fifth snapshot also passes the full independent `make test-unit` gate.
Git's pre-commit guards pass. The only implementation-snapshot change after the
original full run is a stronger protocol-test startup assertion: a valid bound
loopback socket replaces an empty-only address check. Actual protocol tests and
lint were rerun successfully; no production code or coverage exclusions changed.

The current code execution-input digest is
`ba71ca6b03d9509b767d40cdadab14acf2ea73c13686efd9f4fa2d73d1f28737`
for the same 126 source/test/module/Makefile/workflow files, at source commit
`58a0e156691963cf27931597a3330030d061757b`. Its per-file inventory is retained as
`remediation-committed-inputs.json`, and staged validation receipts under
`logical-commits/`. The fresh complete `make test` run in
`remediation-committed-test.log` **passes**: 5,562/5,562 handwritten business
statements across 715 functions, zero misses/missing functions, and 78.7%
controller-only integration coverage. The remaining exact-source checks still
pass; Helm render and hack-script fixtures were separately rerun uncached.
The input inventory was checked afterward: all 126 execution files are unchanged.

Fresh profiles/report are preserved with `remediation-committed-` prefixes and
hashed in `remediation-committed-profile-hashes.json`, separate from the original
run's artifacts. The fresh unit-profile SHA-256 is
`5e09246528f6e5e3b1dcd267be9f5603a6b28ea257f7022e4debbfa08664bb68`;
the business report remains byte-identical at
`f6a9d113d773ced846bab94f5f3389a3dfae9160b5f4b104f339f9de2b78c3be`.
This evidence-document commit changes no execution input and does not turn
the pre-integration result into post-#517 verification or hosted CI evidence.

Feature-branch pushes do not trigger this repository's test/lint workflows:
they run on PRs targeting main/release branches and on main/release pushes.
The agreed strategy still requires #517's merge and integration before the
remediation PR is ready. No dependency branch is stacked/copied, no #517 merge
is performed on the user's behalf, and no green pipeline is claimed for these
unpushed commits. After integration, rerun the gates, push the completed branch,
open/link the approved remediation PR, and watch its exact-head pipeline to
completion, fixing genuine failures rather than weakening security/test gates.

Local environment: **Go 1.26.9, darwin/arm64**, envtest **Kubernetes 1.37.0**,
golangci-lint **v2.11.4**. No customer/fleet cluster is modified or counted as
new qualification. Minimal monitoring/provider/cert-manager CRD fixtures and
simulated issuer/cainjector output are not their live controllers or real CNI
packet enforcement.

## Executed verification

| Gate | Result | What it establishes |
|---|---|---|
| Independent `make test-unit` | **5,562/5,562 handwritten business statements across 715 functions; 0 misses; 0 missing functions** | No envtest startup; API, resource, policy, webhook, controller and bootstrap decisions execute with real business code and simulated external dependencies |
| Independent `make test-integration` | **78.7% controller-only coverage; all selected specs pass** | Production reconciliation against envtest; actual metrics/webhook serving protocol tests remain separately reported (`cmd`: 22.7%), not blended into the controller denominator |
| `go build ./...` | PASS | Local source build, not released-image qualification |
| `make lint` | PASS, zero issues | Pinned Go lint rules |
| `make test-pyramid` | PASS | Security traceability, CI-boundary, Helm render/schema and static wiring checks; not E2E execution |
| `make test-hack-scripts` | PASS: 14 migration, 7 CI-boundary, 4 release-qualification cases | Real negative fixtures for the existing script contracts |
| Shellcheck for changed gate scripts | PASS | Shell syntax/static diagnostics, not behavioral assurance by itself |
| `go test ./... -run='^$' -timeout=30s` | PASS | Compile-only, explicitly not execution coverage |
| `govulncheck -json ./...` and Linux/amd64 `./cmd/...` source scan | **0 finding records in each** | v1.3.0 symbol-level source scan on Go 1.26.9; DB last modified 2026-10-08; no binary/image/SBOM/FIPS claim |
| Repeat `make manifests generate` | PASS: 68 artifact files unchanged | Chart CRD is byte-identical to source; OLM bundle configuration/schema match, differing only in document separator, null creation timestamp and empty status serialization |
| Complete `make test` | **PASS** | Uncached independent unit/integration execution, coverage gates, security traceability, CI boundary, Helm and static pyramid checks; Helm render specs also rerun separately with `-count=1` |
| `git diff --check` | PASS | No whitespace errors; the original uncommitted record and fresh committed-source rerun are distinguished above |

Artifacts are retained in the session's approved `opencode/pr515-audit/`
directory: `remediation-final-test.log`, `remediation-unit-full.log`,
`remediation-integration-full.log`, `remediation-final-govulncheck.json`,
`remediation-final-govulncheck-linux.json`, `remediation-generation-stability.json`,
`remediation-test-inputs.json`, and the monitoring mutation snapshot/logs.
Root coverage outputs are generated evidence, not checked-in substitutes for CI:
`cover-unit.out`, `cover-unit-baseline.out`, `cover-business-unit.json`,
`cover-integration.out`, `cover-integration-controller.out` and `cover.out`.
The final profiles/report are also copied into the artifact directory with
`remediation-final-` prefixes and hashed in
`remediation-final-profile-hashes.json`. The unit profile SHA-256 is
`4039e2f282c4525a7d6aa6cc6fbadea7e3886361d980d97c7bc83909533e6b5b`;
the detailed business report SHA-256 is
`f6a9d113d773ced846bab94f5f3389a3dfae9160b5f4b104f339f9de2b78c3be`.

### Independent 100% business-unit criterion

The denominator inventories every handwritten function and initializer block
under `api/`, `internal/` and `cmd/`. Only generated code and these four
registration/process functions are excluded:

1. `cmd/main.go:init`
2. `cmd/main.go:main`
3. `api/v1alpha2/kubernaut_types.go:init`
4. `api/v1alpha2/groupversion_info.go:Register`

No resource, policy, webhook, controller or TLS business function is exempt.
Cross-package atomic instrumentation is compared with a compile-only baseline;
duplicate blocks are unioned, inconsistent weights/missing blocks are rejected,
and omitted handwritten functions fail the gate. Raw missed statements, not a
rounded `100.0%`, determine success. Integration or merged coverage cannot fill
unit misses. `UT-AUDIT-515-GATE-001`–`009` include actual verifier invocations
rejecting zero execution, incomplete instrumentation and missing functions.
Synthetic gate fixtures test the verifier; they are not the source of the real
business-coverage numerator above.

Serialization/filesystem/crypto boundaries remain real: strongly typed YAML
builders share checked library boundaries; filesystem fixtures vary only
external asset reads; TLS tests retain real parsing, validation and production
key sizes. Refactoring removes duplicate error checks, not error observability
or coverage obligations. Statement coverage is **not** path/branch completeness,
concurrency proof, assertion strength or a guarantee that no bugs remain.

### Wiring-first evidence and monitoring mutation

`IT-MON-513-008` enters production `Reconcile`, asserts the complete expected
monitor/rule set, and checks monitor→Service→workload port consistency. The
unmutated case passes. In an isolated source snapshot, removing only the
`reconcilePrometheusMonitoring` production caller leaves the direct helper unit
tests and build passing but makes this envtest case fail because the expected
EffectivenessMonitor monitor is absent. The production branch is not mutated.

Mutation source digest:
`cbc2c4bf741dcf3eac665daedb0975e7971608e568138145aa990b67fb1493c3`.
See `monitoring-disconnection-remediation-20261010/mutation-provenance.json`,
`mutation-integration.log` and `mutation-unit.log`. This snapshot predates
controller-unit ID disambiguation to `UT-MON-513-CTRL-001`–`007`; only identifiers
changed afterward. It establishes detection of this one disconnected-caller
mutation, not every possible wiring failure.

Fake-client TLS/monitoring helpers are now honestly unit evidence. Separate
real-API journeys cover Hook, DevelopmentSelfSigned, Manual,
AdministratorManaged, namespaced Issuer and ClusterIssuer provisioning;
`IT-TLS-AUDIT-515-004` reproduces and fixes automatic issuer selection after
internal bootstrap Issuers appear. Only exact generated identities controlled
by the current CR API version/kind/name/nonempty UID are excluded as candidates.
`IT-TELEMETRY-AUDIT-515-001` blocks malformed trailing client-chain DER before
migration, restores valid external material, and resumes production reconciliation.
Provider journeys persist bounded Cilium/Calico/OVN scrape rules; the OVN journey
also checks custom AF port **19180** against the actual persisted Service.

**Pyramid conclusion:** unit logic and positive real-API wiring are established
locally. Full real-application E2E scraping/alert delivery for this exact candidate
is **not established**. Contract Kind lanes and historical fleet reports cannot
substitute for that missing journey or imply broad production readiness.

## Control objectives and finding disposition

Authorities: [NIST SP 800-53 Rev. 5][nist], [official OSCAL catalog Rev.
5.2.0][catalog] (modified 2026-05-11), [official FedRAMP 2026 control
reference][fedramp], and [OWASP ASVS 5.0.0][asvs]. Engram MCP recall/root-scoped
source retrieval recovered project methodology/control references; local source
and the audit's official-source extracts preserve exact snapshot/version
provenance when indexed content is stale or recall returns metadata only.

These are engineering objectives, **not** a certification, authorization,
baseline-tailoring approval or official one-to-one crosswalk. ASVS identifiers
below are versioned 5.0.0 requirement IDs; applicability is adapted to the
operator/service boundary. The requested 100% unit threshold and pyramid come
from the user/project, not a universal percentage requirement in SA-11 or ASVS.

| Finding / reviewed area | NIST/FedRAMP IDs | OWASP ASVS 5.0.0 IDs | Bounded disposition |
|---|---|---|---|
| F1/F5 ownership, adoption and deletion races | AC-3, AC-6, CM-3, CM-5 | `v5.0.0-V8.2.2`, `v5.0.0-V8.3.1`, `v5.0.0-V15.4.2` | **Blocked on #517**; default adoption rejection and UID/resource-version deletion preconditions are not copied or claimed integrated |
| F2 effective protocol/toolchain hardening | CM-6, SC-5, SI-2, RA-5 | `v5.0.0-V15.1.3`, `v5.0.0-V15.2.1`, `v5.0.0-V15.2.2` | Actual webhook/metrics HTTP/1.1 defaults, HTTP/2 opt-in, TLS 1.1 rejection and unauthenticated metrics 401 pass; patched source scan passes; immutable artifact qualification remains |
| F3 scraper ingress and communication contract | AC-4, SC-7, SI-4 | `v5.0.0-V13.1.1`, `v5.0.0-V15.2.5` | Exact namespace/nonempty pod-label identities authorize only generated metrics ports; no URL-derived identity, namespace-only access or raw CIDRs; live allowed-scraper/denied-outsider/target `up=1` remains |
| F4 singleton activation | AC-3, CM-5, SI-10 | `v5.0.0-V2.3.2`, `v5.0.0-V15.4.2` | Uncached post-persistence current-UID authorization precedes finalizer/phases; duplicate persisted CRs fail closed. **Not atomic pre-CREATE admission**; administrator bypass, forced cleanup/orphan recovery and broader ownership remain bounded risks |
| F6 live Service convergence | CM-2, CM-6, SI-4, SI-7 | `v5.0.0-V2.2.1` | Live owned selector/port/type drift is repaired despite unchanged hash; allocated addressing and unrelated platform metadata are preserved |
| F7 testing, wiring and qualification | SA-11, CA-2, CA-7, CM-3 | `v5.0.0-V2.2.1`, `v5.0.0-V15.4.2`, `v5.0.0-V16.5.2` | Independent zero-miss unit gate and positive envtest/mutation evidence pass. **Full application E2E qualification remains unverified** |
| F8 Required/Inherited isolation | AC-4, SC-7, CM-6 | `v5.0.0-V13.1.1`, `v5.0.0-V13.2.4`, `v5.0.0-V16.5.2` | Default Required blocks activation on provider discovery failure and retains policies; lost readiness cannot report Running. Explicit Inherited acknowledges administrator protection, never attests enforcement |
| F9 action/phase/outcome audit traces | AU-2, AU-3, AU-12, SI-11 | `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1`, `v5.0.0-V16.3.3`, `v5.0.0-V16.5.2` | Committed old/new phase transitions and resource actions carry identity/version/outcome; forced cleanup emits timeout, not false completion. Inherited collection/retention/access/time evidence remains |
| TLS/PKI, full-chain input validation and key rotation | IA-5, SC-8, SC-12, SC-13, SC-17, SI-10 | `v5.0.0-V11.1.1`, `v5.0.0-V11.2.1`, `v5.0.0-V12.1.1`, `v5.0.0-V12.3.2`, `v5.0.0-V12.3.3` | Real library/boundary tests and API journeys pass; real cert-manager, private-CA/client negotiation and approved FIPS module/operating-mode evidence remain |
| Secret, RBAC and storage responsibilities | AC-3, AC-6, IA-5, SC-28, CM-7 | `v5.0.0-V8.1.1`, `v5.0.0-V8.2.1`, `v5.0.0-V13.2.2`, `v5.0.0-V13.3.1`, `v5.0.0-V14.1.1` | Builder/decision evidence does not qualify etcd encryption, Secret/RBAC access, HSM/vault or organizational key lifecycle; singleton introduces no new permission |
| Migration/recovery and application compatibility | CM-3, CP-9, CP-10, SI-11 | `v5.0.0-V2.3.3`, `v5.0.0-V16.5.2` | Real Job/ConfigMap/Secret API wiring and recovery decisions pass; simulated Job completion does not qualify goose/database migration, backup or rollback |
| Hardening, inventory and supply chain | CM-6, CM-7, CM-8, RA-5, SR-4, SC-5 | `v5.0.0-V15.1.1`, `v5.0.0-V15.1.2`, `v5.0.0-V15.2.1`, `v5.0.0-V15.2.5` | Local builder/render/security-context evidence remains distinct from immutable image/SBOM/signature/provenance and live availability |

HTTP scrape endpoints remain HTTP. Authorization rules do not establish
`v5.0.0-V12.3.1` encrypted monitoring communication; inherited transport
protection/data-classification decisions still need deployment evidence.
Likewise, the 100% unit result does not close #420/#424/#512 application/parity
and private-trust qualification boundaries by implication.

## Merge and release conditions

1. Preserve the approved namespaced CR/canonical name and deployment-namespace
   flexibility; no Lease, fixed namespace or singleton-specific RBAC is added.
2. Wait for [ownership PR #517](https://github.com/jordigilh/kubernaut-operator/pull/517)
   to merge, then rebase/integrate and rerun adoption, create-race, reinstall,
   replacement/same-UID ownership-change deletion and finalizer regressions.
   Last observed #517 head: `c360e5e60d815881dcf316be8fe9f56243a033c2`, **OPEN**.
   No stacking or duplication is authorized by the approved dependency strategy.
3. Repeat all local gates and generated CRD/chart/bundle consistency checks on
   the integrated commit before publishing a truthful remediation PR.
4. Retain exact-SHA upstream application, CNI traffic, scrape/alert, release
   image/SBOM/FIPS and inherited-control qualification as separate unfulfilled
   release/deployment gates under `ISSUE-501-OPERATOR-UPSTREAM-CI-BOUNDARY.md`.

**Confidence: 96% in the bounded repository findings/evidence.** Justification:
independent instrumentation and negative verifier fixtures, real-API positive
wiring and mutation detection, actual serving-protocol probes, source scan,
build/lint and compatibility gates. This is not confidence in production
qualification: ownership integration, live/application and inherited-control
evidence remain explicit blockers. The original snapshot-bound NO-GO stands.

[nist]: https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final
[catalog]: https://raw.githubusercontent.com/usnistgov/oscal-content/main/nist.gov/SP800-53/rev5/json/NIST_SP-800-53_rev5_catalog.json
[fedramp]: https://www.fedramp.gov/2026/reference/controls/
[asvs]: https://raw.githubusercontent.com/OWASP/ASVS/v5.0.0/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.csv

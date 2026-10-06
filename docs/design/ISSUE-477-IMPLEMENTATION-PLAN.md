# Issue #477 — Make Fleet Metadata Cache API TLS-only

**Status:** Implemented; focused validation is complete on the clean post-#499 branch
**Scope:** `kubernaut-operator` Fleet Metadata Cache (FMC) API transport
**Date:** 2026-10-05
**Owner:** Kubernaut Operator

> This document records the approved design, implementation shape, and validation
> outcome for issue #477. The implementation branch is based on the clean
> post-#499 `origin/main` baseline at `5ed4a0a`; unrelated work remains outside
> this branch.

## 1. Executive decision

Implement TLS-only transport for FMC's **scope-check API port** using
the operator's existing inter-service serving certificate and trust material. Keep
the dedicated health port and Prometheus metrics port plain HTTP because they are
node-local/kubelet and monitoring surfaces, respectively; they are not the
cross-service FMC API. The API port must serve TLS and must not accept plaintext.

The latest upstream release candidate, `v1.6.0-rc20`, contains the required
server-side TLS implementation and changes FMC API setup to fail closed. The
approved release bump pins the operator's Go dependency and operand images to
`v1.6.0-rc20`, and the operator-side TLS wiring described here is implemented.
The exact digest, signed source metadata, and image-backed listener check are
qualified, and the clean post-#499 repository test baseline is passing.

## 2. Preflight evidence

### 2.1 Repository and worktree audit

- The public `kubernaut` repository's issue #477 is an unrelated HAPI issue; this
  plan treats “#477” as the operator issue named by the user and does not use the
  upstream issue number as implementation evidence.
- The implementation branch is based on `origin/main` at `5ed4a0a` and keeps
  unrelated Fleet migration work out of the PR.
- No unrelated work was reset, checked out, discarded, or overwritten.
- `go build ./...` and `go test ./... -run=^$` pass on the clean baseline.
- `git diff --check` passes.

### 2.2 Current FMC operator path

| Concern | Current evidence | #477 implication |
|---|---|---|
| FMC config | `internal/resources/fleetmetadatacache.go:FleetMetadataCacheConfigMap` renders `server.apiAddr`, health, metrics, and `server.tls.certDir` | Config shape is already compatible with upstream TLS; retain the API/health/metrics split |
| FMC Deployment | `internal/resources/fleetmetadatacache.go:FleetMetadataCacheDeployment` mounts the config, OAuth2 credentials, and inter-service CA | Add the serving Secret mount at the rendered `certDir`; do not expose the private key through ConfigMap data |
| FMC Service | `FleetMetadataCacheService` exposes API, health, and metrics ports | Mark the API contract HTTPS by changing the derived endpoint and consumer trust wiring; health/metrics remain operational HTTP |
| Derived endpoint | `internal/resources/common.go:FleetMetadataCacheURL` currently returns `http://...:8080` | Change only the FMC API URL to `https://...:8080` (or an approved TLS API port); never silently fall back to HTTP |
| Consumers | `resolveFleetEndpoint` feeds Gateway/RO/APIFrontend and other Fleet config builders | Every consuming client must use the HTTPS endpoint and the same CA trust bundle |
| Serving material | Cert-manager provisioning already emits a `fleetmetadatacache-service` leaf as `fleetmetadatacache-tls`, but the generic TLS resolver, development/OpenShift mappings, validation, and FMC Deployment mount do not yet consume it | Complete the existing source-aware mapping and mount the stable serving Secret; do not invent a second certificate lifecycle |
| Wiring | Controller/resource aggregation already reconciles FMC resources through existing component paths | The implementation must add no orphan builder; controller envtest must prove the resulting resource set and endpoint behavior |

#### Serving Secret and trust mapping completed

The source-by-source audit is now explicit:

| Runtime source | Current FMC state | Required #477 delta |
|---|---|---|
| OpenShift service-CA (`tls.mode` empty) | `openShiftServiceTLSMaterial` resolves only the five pre-existing service keys; `FleetMetadataCacheService` does not apply the service-CA annotation | Add the FMC service key, annotation, and read-only serving Secret mount |
| Administrator-managed and reference-only cert-manager | `requiredServiceTLSSecretNames` requires only gateway, datastorage, kubernautagent, apifrontend, and authwebhook | Require/resolve/validate the FMC serving Secret when FMC is enabled, without changing the CRD shape |
| Cert-manager provisioning | `certManagerLeafResources` already emits a `Certificate` for `fleetmetadatacache-service` with default Secret `fleetmetadatacache-tls`; `TLSMaterial.ServiceTLSSecretNames` omits that key | Resolve the emitted Secret, validate readiness/SANs, and mount it |
| Development self-signed and hook modes | The generated CA/leaf maps and `developmentTLSServiceNames` contain only the five pre-existing services | Add FMC to the same generated CA, rotation, ownership, and DNS-identity path |
| Client trust | FMC already mounts the inter-service trust material; Gateway, RemediationOrchestrator, and APIFrontend already have the corresponding Fleet CA paths/mount helpers | Preserve the existing CA path and prove every FMC consumer uses it with the HTTPS endpoint |

The stable internal key should be `fleetmetadatacache`; the Kubernetes Service DNS
identity is `fleetmetadatacache-service.<namespace>.svc.cluster.local`. This is an
internal resolver extension, not a CRD field change.

### 2.3 Dependency and deployed-binary spike

Before the approved release bump, the operator's `go.mod` required
`github.com/jordigilh/kubernaut v1.6.0-rc9`; it now requires
`v1.6.0-rc20`. The newest upstream RC was verified through the Go module proxy
and GitHub, tagged at
`d09bacc45c7f82e4aded7eb70e5f3c0ac23362a8` on 2026-10-01. The `rc20` source was
fetched from that immutable tag and contains:

1. `pkg/fleet/fmc/config.ServerConfig.TLS` with `certDir` and an `Enabled()`
   predicate.
2. `cmd/fleetmetadatacache/main.go:buildFMCServers`, which calls
   `sharedtls.ConfigureRequiredTLS` for the API server. Missing/invalid
   certificate material returns an error instead of silently downgrading.
3. `serveAndReport`, which calls `ListenAndServeTLS` when the API server has TLS
   configured.
4. A dedicated plain health server and plain metrics server.
5. Certificate hot reload for the API server.
6. Upstream Ginkgo wiring tests covering TLS handshake success, rejection of a
   plaintext request against a TLS-only listener, `/readyz` behavior, and TLS
   profile enforcement.

**Spike result:** `v1.6.0-rc20` supports server-side TLS and is the preferred
upstream baseline for #477. `rc9` also contains conditional TLS code, but its
fail-open behavior is not sufficient for a TLS-only requirement. The old operator
comments and installation text stating that the binary has “no TLS server
support” are stale and must be corrected as part of #477.

**Image gate:** the release bump now pins
`quay.io/kubernaut-ai/fleetmetadatacache@sha256:c315aeb4afb2f71e9c5ba439c387c4925823513ef1a5225e9db7c4bc6937cc54`,
which `crane digest` verifies is the registry digest for the
`1.6.0-rc20` multi-architecture tag. The image-content gate is now complete;
the exact digest and runtime evidence are recorded below.

### 2.4 Exact operand-image gate (completed)

The pinned OCI index and child manifests are:

| Architecture | Digest |
|---|---|
| `linux/amd64` | `sha256:96fc84143522321b4e546820a131bf86ca869f0d62f1986d598b6774c8f6782e` |
| `linux/arm64` | `sha256:38496bfd9aec03538a30973b5c1be3953b2cd5b2e4bd3a9fd33a2c83f18327d2` |
| Multi-architecture index | `sha256:c315aeb4afb2f71e9c5ba439c387c4925823513ef1a5225e9db7c4bc6937cc54` |

`crane config --platform` reports, for both child manifests:

- `org.opencontainers.image.source=https://github.com/jordigilh/kubernaut`
- `org.opencontainers.image.version=1.6.0-rc20`
- `org.opencontainers.image.revision=d09bacc45c7f82e4aded7eb70e5f3c0ac23362a8`
- the release build timestamp `2026-10-01T18:20:02Z`

Cosign verification succeeded for both child manifests (signature and SBOM
attestation), and the signed SLSA provenance binds the release artifact to the
upstream `v1.6.0-rc20` tag and commit `d09bacc45c7f82e4aded7eb70e5f3c0ac23362a8`.

The exact `linux/amd64` image was also run with a temporary config containing
`server.tls.certDir: /tls`, a mounted self-signed test certificate, and small
fake MCP/Kubernetes/Valkey dependencies so startup could reach the real server
wiring. Observed results:

| Probe | Result |
|---|---|
| HTTPS API `/readyz` with the test CA | `200`, body `ok` |
| Plain HTTP health `/readyz` | `200`, body `ok` |
| Plain HTTP metrics `/metrics` | `200` |
| Plain HTTP request to the API `/readyz` | `400`, `Client sent an HTTP request to an HTTPS server.` |
| Container log | `TLS configured for FMC API server`, followed by `API server listening ... tls:true` |

This black-box check qualifies the released image's listener and port split;
the upstream Ginkgo wiring/integration suites remain the evidence for handler
authorization, profile enforcement, and full FMC behavior.

### 2.5 Clean-branch test baseline

The implementation baseline is the clean post-#499 branch:

- `go build ./...` and `git diff --check` pass.
- The full resources suite and focused FMC/TLS resource specs pass.
- The controller envtest suite passes all 227 specs with 78.4% coverage.
- Unit coverage is 87.2%, above the repository's 80% unit threshold.

## 3. Alternatives and spike decisions

| Option | Assessment | Decision |
|---|---|---|
| **A. Upgrade/qualify upstream `v1.6.0-rc20`, configure FMC TLS, and mount the existing serving Secret** | Uses the upstream fail-closed TLS implementation, existing cert ownership, CA publication, rotation, and upstream tests; preserves the three-port contract | **Selected and implemented** |
| B. Add an Envoy/nginx TLS sidecar or Service mesh termination | Could protect an old binary, but adds a proxy, certificate/config lifecycle, readiness semantics, ports, RBAC, NetworkPolicy, and another failure mode | Reject unless image gate disproves A |
| C. Fork upstream FMC or stay on `rc9` | A fork duplicates available code; `rc9` has conditional/fail-open API TLS and does not meet the desired fail-closed contract | Reject; use only if `rc20` image qualification fails and upstream cannot publish a usable artifact |
| D. TLS-terminate at an ingress/Route only | Does not protect in-cluster Gateway/RO-to-FMC traffic and leaves the Service API plaintext | Reject |

### Image spike (completed)

1. The pinned image manifest/config and source/build labels were inspected.
2. The exact image was run with a config containing `server.tls.certDir` and mounted
   `tls.crt`/`tls.key`.
3. A TLS client reached `/readyz` and a plaintext client could not reach the
   API handler.
4. Image digest, architecture, source commit, and command output are recorded in
   section 2.4 of this plan.

## 4. Implemented shape

The approved Option A shape was implemented against the clean post-#499
`origin/main` baseline:

1. Upgrade the upstream Go dependency and all FMC operand image references to the
   exact qualified `v1.6.0-rc20` artifacts (or a later explicitly approved RC).
2. Reuse the existing FMC serving Secret generated/resolved by the operator.
3. Mount it read-only at `InterServiceTLSCertDirFor(knV2)` in the FMC Deployment.
4. Keep `server.tls.certDir` in the FMC ConfigMap and ensure the API listener is
   configured with that directory.
5. Change the operator-managed derived FMC endpoint to HTTPS. Explicit FMC
   endpoints must be validated as HTTPS when they select the operator-managed FMC
   path; no plaintext fallback is permitted.
6. Ensure Gateway, RemediationOrchestrator, APIFrontend, and any other FMC
   consumer receive the serving CA through the existing trust-bundle path and use
   the service DNS name for hostname verification.
7. Preserve health probes on the plain health port and metrics on the metrics port;
   do not make kubelet or Prometheus depend on API client credentials or TLS.
8. Add structured logs/status evidence for TLS configuration failure and ensure a
   missing/invalid serving Secret fails closed rather than producing an HTTP API.
9. Update stale comments, installation examples, and FMC test-plan language.
10. Regenerate manifests and dependency/image metadata for the approved RC. The
   current recommendation requires no CRD schema change.

## 5. TDD plan and pyramid invariant

Follow the required wiring-first sequence: **RED IT → RED UT → GREEN wiring →
GREEN builder/config → REFACTOR**.

### RED — integration first

- Add envtest cases in `internal/controller/` proving an enabled FMC produces:
  serving Secret reference/mount, `server.tls.certDir`, HTTPS derived endpoint,
  and CA trust material for every FMC consumer.
- Prove an invalid/missing serving material drives an observable failure/status
  and never produces a usable plaintext FMC API contract.
- Prove disable-after-enable cleanup does not delete administrator-owned material.

### RED — unit/resource behavior

- `FleetMetadataCacheConfigMap`: exact upstream YAML shape, API TLS certDir,
  unchanged plain health/metrics addresses, and no private key in ConfigMap.
- `FleetMetadataCacheDeployment`: serving Secret volume/mount, read-only mode,
  correct path alignment, and no accidental removal of existing CA/OAuth2/Valkey
  mounts.
- `FleetMetadataCacheURL` and endpoint resolution: HTTPS by default, stable DNS
  identity, and no HTTP fallback.
- Consumer config builders: HTTPS endpoint plus the correct CA path.
- Validation: reject insecure operator-managed FMC endpoint configurations and
  incomplete TLS references with actionable errors.

### GREEN — minimum wiring

- Wire the existing serving Secret and endpoint resolver through the current
  controller/resource aggregation path.
- Make both the new unit tests and the envtest reconciliation tests pass.
- Run Checkpoint W before claiming GREEN.

### REFACTOR

- Remove stale “no TLS support” documentation/comments.
- Centralize the FMC API URL/port/path constants if needed; avoid duplicate
  hand-built URL logic.
- Add structured error logging with CR generation/resourceVersion context.
- Run build, lint, full tests, manifest generation, diff checks, and security
  traceability checks.

## 6. Wiring manifest

| Component | Production entry point | Wiring location to verify | IT evidence |
|---|---|---|---|
| FMC serving Secret mount | `FleetMetadataCacheDeployment` | `internal/resources/fleetmetadatacache.go` via existing deployment aggregation | `IT-FMC-TLS-001` |
| HTTPS derived FMC endpoint | `FleetMetadataCacheURL` / `resolveFleetEndpoint` | `internal/resources/common.go` and all Fleet config builders | `IT-FMC-TLS-002` |
| FMC TLS ConfigMap | `FleetMetadataCacheConfigMap` | `internal/resources/fleetmetadatacache.go` | `IT-FMC-TLS-003` |
| Consumer CA trust | existing Fleet deployment/config builders | `internal/resources/configmaps.go`, deployment helpers, controller reconciliation | `IT-FMC-TLS-004` |
| Fail-closed validation/readiness | existing validation and lifecycle reconciliation | `internal/resources/validation.go`, `internal/controller/` | `IT-FMC-TLS-005` |

Checkpoint W must confirm every row has a production caller, a focused unit
assertion, and an envtest reconciliation assertion. No new builder may remain
unreferenced.

## 7. CRD and compatibility impact

- **Recommended:** no CRD schema change. Existing TLS source and inter-service
  serving material are reused.
- This changes the operator-managed FMC API scheme from HTTP to HTTPS. It is a
  behavioral/security compatibility change, not a field rename.
- Explicit BYO FMC endpoints remain outside the operator-managed server lifecycle,
  but must not be silently rewritten. Their scheme validation and migration policy
  require approval if the existing API permits plaintext BYO endpoints.
- Existing HTTP clients or manually configured consumers of the operator-managed
  FMC Service will need the HTTPS endpoint and CA. Document this migration.

## 8. Business-level test matrix and control traceability

These are business assertions, not claims of formal certification. Runtime,
platform, organizational, and independent assessment evidence remain necessary.

| ID | Business behavior | Unit | Integration/E2E | FedRAMP/NIST-oriented | SOC 2 | OWASP ASVS 5.0.0 |
|---|---|---|---|---|---|---|
| `BA-FMC-TLS-001` | Operator-managed FMC scope-check API is TLS-only; plaintext never reaches the handler | `UT-FMC-TLS-001` | `IT-FMC-TLS-001`, image spike | SC-8, SC-13, SC-17 | CC6, CC7 | V12.2.1, V13.2.1 |
| `BA-FMC-TLS-002` | All in-cluster FMC consumers use HTTPS with CA and DNS identity verification | `UT-FMC-TLS-002` | `IT-FMC-TLS-002`, `E2E-FMC-TLS-001` | SC-8, SC-17, AC-4 | CC6, CC8 | V12.1.3, V13.3.1 |
| `BA-FMC-TLS-003` | Serving private keys are mounted read-only from Secrets, never ConfigMaps or logs | `UT-FMC-TLS-003` | `IT-FMC-TLS-003` | AC-6, IA-5, SC-12 | CC6, CC7 | V11.1.1, V12.1.1 |
| `BA-FMC-TLS-004` | Missing/invalid TLS material fails closed and is observable | `UT-FMC-TLS-004` | `IT-FMC-TLS-004`, `E2E-FMC-TLS-002` | CM-6, SI-10, SI-4 | CC7, CC8 | V16.5.2, V12.2.1 |
| `BA-FMC-TLS-005` | Certificate rotation preserves service identity and client trust without plaintext downgrade | `UT-FMC-TLS-005` | `IT-FMC-TLS-005`, `E2E-FMC-TLS-003` | SC-12, SC-13, SI-4 | CC6, CC7, A1 | V11.1.2, V12.1.2, V13.2.1 |
| `BA-FMC-TLS-006` | Health/metrics remain available on their intentionally separate operational ports | `UT-FMC-TLS-006` | `IT-FMC-TLS-006` | SI-4, SC-8 boundary validation | CC7 | V13.2.1 |

Evidence must test actual business behavior (handshake, rejection, trust,
rotation, and reconciliation), not merely assert that a field or annotation is
present.

## 9. Risks and mitigations

| Risk | Mitigation / approval gate |
|---|---|
| The `rc20` image must remain aligned across dependency and digest-pinned operand metadata | Keep the recorded OCI index/child digests, source labels, and signed provenance together through implementation and release regeneration |
| Existing uncommitted #499 migration masks regressions | Preserve WIP; rebase/repair only with owner approval; test #477 on a clean compatible baseline or after WIP is stabilized |
| Wrong CA or DNS SAN causes consumers to fail | Assert service DNS SAN and real TLS client handshake in unit/envtest/Kind evidence |
| Plain health/metrics are mistaken for plaintext API exposure | Document and test port separation; API only is the TLS-only contract |
| Cert rotation causes outage | Reuse existing cert-manager/manual ownership and upstream hot reload; test old/new trust overlap and readiness |
| BYO endpoint semantics are ambiguous | Do not rewrite BYO values silently; obtain approval for validation/backward-compatibility policy |
| Controls are overstated | Label repository evidence as partially verified where runtime/platform assessment is still required |

## 10. Approval gates and completion criteria

The implementation started after the following approvals and gates:

1. User approves Option A, the `rc20` dependency/image update, and the no-CRD-schema-change
   compatibility policy.
2. The exact `rc20` operand image digest is qualified in section 2.4 and must not
   be changed without a new image gate or explicit later-RC approval.
 3. The clean post-#499 branch is selected as the testable baseline.
4. Wiring manifest and business-level test IDs are accepted.

Completion evidence:

- `go build ./...`, `git diff --check`, and production lint with
  `golangci-lint run --tests=false --disable unused` pass.
- Focused FMC/TLS resource specs and overlayed FMC/controller integration specs
  pass; the exact image-backed TLS handshake, plaintext rejection, health, and
  metrics checks are recorded in section 2.4.
- `make manifests generate` completes with no RBAC changes; the generated CRD
  description changes only document the HTTPS FMC contract.
- The full unit and controller integration targets pass, including security
  traceability, CI-boundary, and test-pyramid checks.
- Operator/user documentation no longer claims that FMC lacks TLS server support.

## 11. Confidence

**Confidence: 95%.**

The architecture, operator call paths, current TLS foundations, and upstream
`v1.6.0-rc20` FMC implementation are directly evidenced, including a fail-closed
TLS setup, real upstream handshake tests, source/provenance metadata, and a
black-box run of the exact pinned amd64 image. Focused implementation validation
passed. The remaining risks are BYO endpoint compatibility and cert rotation
behavior across all Fleet consumers; these are explicit repository limitations
rather than unknown upstream image behavior.

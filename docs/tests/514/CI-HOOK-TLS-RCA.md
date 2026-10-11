# PR #517 — Hook TLS CI root-cause analysis

**Date:** 2026-10-11<br>
**Failing source:** `67d0a7bbbda6396c48a7f56c1c66c53b537a250a`<br>
**Run:** [38112936915](https://github.com/jordigilh/kubernaut-operator/actions/runs/38112936915)<br>
**Failed job:** [Kind E2E (hook TLS)](https://github.com/jordigilh/kubernaut-operator/actions/runs/38112936915/job/114393271252)

## Failure and cause

The failing assertion is the authenticated DataStorage HTTPS probe in the
ordered TLS journey, after the preceding ownership journey installs, upgrades,
uninstalls and reinstalls the CR. It fails with:

```text
curl: (60) SSL certificate problem: unable to get local issuer certificate
```

The earlier `OwnershipConflict` messages are intentional negative cases: the
journey verifies administrator resources are preserved. They are not the
failing assertion, and their authorization gates must not be bypassed.

The diagnostics show the DataStorage Deployment/Pod surviving from the prior
installation (about 78/77 seconds old), while the reinstalled controller
workloads are about 38 seconds old. The approved complete-marker reinstall
contract permits repairing that Deployment for the new CR UID without changing
its Pod template. Kubernetes garbage collection and projected-volume refresh
are asynchronous; CR deletion or `Running` does not synchronize them.

The contract image's `entrypoint.sh` previously started `openssl s_server` once.
That process keeps its startup certificate/key in memory, even when the mounted
Secret generation changes. It could therefore serve the old leaf against a
newly published CA. The probe also attempted the handshake only once, without
waiting for either mounted trust or serving material to converge.

This is a fixture/runtime-contract gap, not evidence that ownership denial should
be relaxed. The upstream source inspected at
`8a907bb57d3530da32de9b3b8250b53587ed6f41` wires DataStorage to
`pkg/shared/tls.ConfigureRequiredTLS`, `CertReloader.GetCertificate` and a file
watcher in `pkg/datastorage/server/server_lifecycle.go`. Source inspection is not
a fresh qualification run of an upstream application image.

## Correction and regression protection

1. Refresh the fixture's OpenSSL server when mounted certificate/key content
   changes, when material arrives after startup, or when its TLS child exits.
   Do not print key material or fingerprints. Keep the health process and the
   TLS listener separate; do not add a plaintext API fallback.
2. Poll the existing authenticated HTTPS probe for a bounded three-minute
   projected-volume convergence window. Keep CA, hostname and TLS verification
   enabled on every attempt; persistent errors still fail the journey.
3. Require the peer's first certificate to match the current DataStorage Secret,
   not merely any leaf trusted by the same root. Parse curl's `Cert:`-prefixed
   `%{certs}` output fail-closed. The pinned curl 8.11.1 image uses OpenSSL 3.3.2,
   which supports this output.
4. Rotate both Gateway and the actually probed DataStorage leaf in
   `E2E-TLS-HOOK-002`. Verify changed Secret bytes, Kubernaut ownership, TLSReady,
   and current-leaf authenticated service reachability.

No production controller/resource/API code, ownership policy, RBAC, schema,
bundle, Helm chart, CI workflow, timeout configuration or coverage threshold is
changed by this correction.
The correction is recorded in code commit `6fb0108`; the companion evidence
commit changes only documentation.

## Verification receipts

Artifacts are retained under the session's approved `opencode/pr515-audit/`
directory. They are separate from the original PR #515 remediation receipts.

| Evidence | Outcome |
|---|---|
| `pr517-contract-tls-red-complete.log` | Original script fails both selected host regressions: new-root projection replacement and delayed material; zero pending cases |
| `pr517-linux-contract-red.log` | Original script fails all three selected Linux/Alpine runtime regressions, including reissuance under an unchanged CA |
| `pr517-rca-contract-final.log` | Corrected host contract suite: **10/10 pass**, zero pending/skipped |
| `pr517-linux-contract-green.log` | Corrected Linux/arm64 contract suite in the Alpine fixture: **10/10 pass**, zero pending/skipped |
| `pr517-rca-ci-toolchain-contract.log` | Contract suite also passes on the PR's configured Go 1.26.6 toolchain; broader host gates used Go 1.26.9 |
| `pr517-rca-make-test.log` | Full host gate passes: resources **96.1%**, internal unit **93.6%**, controller integration **82.2%**, all 12 existing business entry points **100%**, Helm **11/11**, traceability/CI-boundary/pyramid pass |
| `pr517-rca-lint-green.log` | Zero lint issues; confined temporary filesystem and context-aware socket/process boundaries |
| Build / compile-only / shellcheck | `go build ./...`, all-package test compilation and shellcheck pass; compilation alone is not execution evidence |
| Code generation / scope check | `make test` regenerates manifests/deepcopy with no API/config/chart/bundle/production/workflow diff |

The full host run precedes the final test-harness-only context/filesystem cleanup;
the affected contract suite, build and lint were rerun after that cleanup.
The independent controller and resource implementation was unchanged.
The existing 12-entry-point gate is not the separate exhaustive 100% handwritten
business gate on the unmerged PR #515 remediation branch.
The reported 82.2% is the existing controller-suite denominator; that suite
still includes legacy fake-client helper cases. It is not a claim of 82.2%
independent real-API-only coverage or closure of the separate assurance audit.

The runtime regressions execute the actual shell lifecycle with real production
certificate generation, OpenSSL and verified Go TLS handshakes. Only the
unrelated health process is stubbed, and container mount/listen paths are
translated for isolation. These are fixture/runtime evidence, not operator
envtest or real-application E2E qualification. Focused RED runs exclude unrelated
specs by command-line selection; no pending or skipped test implementation is
introduced.

**Hosted pipeline disposition:** pending the corrective push. Do not attribute
the local/Linux regressions to a successful new Kind run before CI completes.
The prior successful head recorded in `VALIDATION.md` remains historical; it
does not erase this later failed run. Review approval and merge remain the user's
responsibility. The PR #515 remediation branch is not stacked or merged here.

**Confidence: 96%** in the bounded RCA and fixture correction, based on the exact
failing assertion, surviving-Pod diagnostics, deterministic RED/GREEN results
on host and Linux, source wiring, and unchanged security gates. Hosted Kind
matrix verification remains the final CI gate, not broad release qualification.

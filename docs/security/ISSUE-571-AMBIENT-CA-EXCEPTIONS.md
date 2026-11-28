# ISSUE-571: Ambient CA Exception Register — Threat-Model Documentation

**Issue**: [kubernaut-operator#571](https://github.com/jordigilh/kubernaut-operator/issues/571)
**Status**: Approved exception lane (post-spike, 2026-11-24)
**Upstream unblock tracker**: [kubernaut#2548](https://github.com/jordigilh/kubernaut/issues/2548)
**Related**: `DD-TLS-001` (upstream no-write ambient CA trust injection), `BR-SECURITY-2545`, operator issue #404

## Purpose

Issue #571 requires that no long-running production operand container holds a
writable directory solely for ambient CA injection, `SSL_CERT_FILE`,
`SSL_CERT_DIR`, or TLS/SSL certificate assembly. The 11 core operand
Deployments were migrated to the upstream runtime ambient-CA contract
(`5931441`, `UT-TLS-2545-OP-001/002`). Three rendering paths remain on the
legacy pattern because their elimination is **blocked on upstream runtime
changes** — an operator-only workaround would bypass upstream TLS semantics,
which issue #571 explicitly forbids. Separately, a fourth path, the chart's
development
certificate bootstrap hook Job, stages generated CA material in a writable
`/tmp` by design (development-mode only, never a production operand). This
register documents each retained exception under the issue's own escape hatch:

> "If an init-only certificate operation remains unavoidable, it must be
> isolated from the main container, bounded/memory-backed where appropriate,
> read-only to the main container, and documented as an approved
> threat-model exception."

Every exception below holds that shape — E-4 differs from E-1/E-2 in that
no long-running container exists at all. Contract tests
(`UT-TLS-571-001..004`, `internal/resources/ambient_ca_2545_test.go`),
`UT-TLS-571-005` on the rendered chart (`test/helm/chart_test.go`), and the
Console lifecycle integration test enforce it.

## Exception register

### E-1 — Console oauth2-proxy backend-logout CA bundle

**Location**: `internal/resources/console.go` (`consoleLogoutTLSSettings`,
`consoleLogoutCAInitContainer`, `consoleDeploymentVolumes`), rendered when
`spec.console.auth.providerLogoutURL` is set.

**Why it exists**: the pinned oauth2-proxy image's `backendLogout` performs a
bare `http.Get` that ignores the provider `CAFiles` configuration (documented
in operator issue #483). Ambient `SSL_CERT_FILE` pointing at a merged
system+provider bundle is the only mechanism that keeps provider logout
certificate-verified against self-signed IdP CAs without changing the image.

**Exception shape**: `build-ca-bundle` runs only as an init container; the
`combined-ca` staging volume is **memory-backed and bounded**
(`emptyDir.medium: Memory`, `sizeLimit: 16Mi`); the oauth2-proxy container
mounts it **read-only**.

**Unblock condition**: upstream oauth2-proxy fix routing backend logout
through the provider's CA-configured client, followed by a pinned-image
upgrade to the release containing it — tracked as
[oauth2-proxy#3621](https://github.com/oauth2-proxy/oauth2-proxy/issues/3621).
Verified 2026-11-24: no released version does this — `backendLogout` still
uses a bare `http.Get` on master (ahead of v7.15.6), and v7.15.0's #3352 only
fixed logout/session ordering, not the TLS client. Until then,
`SSL_CERT_FILE` against the merged bundle is the only certificate-verified
backend-logout path for self-signed IdPs. On adoption, drop the init
container, the `combined-ca` volume, and the `SSL_CERT_FILE` env from
`consoleOAuth2ProxyContainer`.

### E-2 — WorkflowExecution AAP CA merge

**Location**: `internal/resources/deployments.go`
(`WorkflowExecutionDeployment`), rendered when
`spec.workflowExecution.ansible.caCertSecretRef` is set.

**Why it exists**: upstream `sharedtls.DefaultBaseTransport()` — the AWX/AAP
client's trust path (upstream issue #902) — trusts only the single
`TLS_CA_FILE` bundle and ignores `SSL_CERT_DIR`. The inter-service CA and the
AAP CA must therefore be merged into one file before the process starts.

**Exception shape**: `build-ca-bundle` runs only as an init container; the
`combined-ca` staging volume is **memory-backed and bounded**
(`emptyDir.medium: Memory`, `sizeLimit: 16Mi`); the long-running container
mounts it **read-only**; no static `SSL_CERT_FILE`/`SSL_CERT_DIR` env is
declared (the `tlsCaFile` config field flows through the runtime adapter per
`DD-TLS-001`).

**Unblock condition**: upstream kubernaut#2548 delivering a reloadable
transport that consumes a multi-CA directory. On adoption, replace the merge
with a read-only projected volume holding both CA files.

### E-3 — FleetMetadataCache static `SSL_CERT_FILE`

**Location**: `internal/resources/fleetmetadatacache.go`
(`FleetMetadataCacheDeployment` env).

**Why it exists**: upstream `cmd/fleetmetadatacache` never calls
`sharedtls.InjectAmbientCACerts`; its MCP Gateway session transport reads no
explicit CA field. A static `SSL_CERT_FILE` pointing at the read-only
inter-service CA file is the only private-CA trust path.

**Exception shape**: the env var points at `InterServiceTLSCAFileFor(kn)` — a
**read-only ConfigMap-mounted file**; FMC renders **no writable volume** and
performs no CA assembly.

**Known trade-off**: Go's `SSL_CERT_FILE` replaces the default bundle *file*
search but does not disable the default certificate *directory* scan
(`SSL_CERT_DIR` unset): public roots present in the FMC image's certificate
directories (for example `/etc/pki/tls/certs` on UBI) remain trusted. Whether
a given public root verifies therefore depends on the image's directory
contents rather than the complete system bundle. This nuance is accepted
pending the upstream fix.

**Unblock condition**: upstream kubernaut#2548 wiring FMC to the runtime
ambient-CA adapter. On adoption, remove the env var from the Deployment.

### E-4 — Chart development certificate bootstrap Job

**Location**: `charts/kubernaut-operator/templates/certificates/development-bootstrap-job.yaml`
(`kubernaut-operator-cert-bootstrap`), rendered only when
`webhook.tls.mode=development` (the chart default for non-production
installs); the operator's OLM bundle renders no certificate Job at all.

**Why it exists**: development mode bootstraps the webhook serving CA and
certificate because no cluster CA infrastructure (cert-manager, OpenShift
service CA, or an administrator-supplied Secret) is in play. The pre-install
hook Job generates the material with `openssl` in a `mktemp -d /tmp/...`
workspace, publishes it to the `kubernaut-operator-webhook-cert` Secret, and
is deleted on completion (`helm.sh/hook-delete-policy`), so no consumer
container ever mounts the staging directory. Unlike E-1–E-3 this path is not
upstream-blocked; it is the chart's sanctioned development-only certificate
provisioning, explicitly classified per #571's boundary rather than silently
treated as a production-operand exception.

**Exception shape**: the ephemeral hook Job's sole container keeps
`readOnlyRootFilesystem: true`; its only writable path is the
`cert-bootstrap-tmp` volume at `/tmp`, which is **memory-backed and
bounded** (`emptyDir.medium: Memory`, `sizeLimit: 16Mi`) — the shape the
upstream rendered-bundle gate (kubernaut#2548, PR kubernaut#2721,
`UT-TLS-2548-009`) requires for init-only `kind: Job` certificate staging.
The `sizeLimit` is advisory accounting: the Job container sets no memory
limit, so tmpfs usage is bounded by kubelet eviction rather than a cgroup;
the enforced contract is `medium: Memory`, which removes disk-backed
persistence of generated TLS material.

**Unblock condition**: none tracked upstream. Remove this exception when the
chart stops bootstrapping development certificates (for example if the
development TLS mode is retired in favor of cert-manager/service-CA
everywhere).

## Non-TLS writable storage (classified, not exceptions)

The following writable paths remain by business purpose and are **out of
scope** for #571 per the issue's own boundary:

| Path | Component | Purpose |
|---|---|---|
| `/tmp` (`tmp` emptyDir) | DataStorage | generic scratch for the Go runtime; not TLS-specific |
| `/data` (`data` emptyDir) | DataStorage | data volume with independent business purpose |
| `nginx-tmp` | Console nginx | nginx runtime scratch (client bodies, proxy temp) |
| Notification file output, workflow execution scratch | per upstream | independently justified upstream storage |

## Control mapping

| Control | How this register satisfies it |
|---|---|
| FedRAMP AC-6 / CM-7 | Retained writable locations are minimized to memory-backed, init-only, read-only-to-workload staging; every exception is enumerated and test-enforced |
| FedRAMP SC-8 / SC-13 | No trust path is silently downgraded: certificate verification against private CAs is preserved in all four exceptions; generated development CA material never touches container disk |
| SOC 2 CC7.2 | The exception inventory plus contract tests give auditable evidence of the deployment boundary |
| OWASP ASVS V15.2.5 (V14/15 deployment boundaries) | No long-running container can persist staged TLS material to disk; staging is tmpfs-only and read-only after assembly |

## Enforcement

- `UT-TLS-571-001..004` — `internal/resources/ambient_ca_2545_test.go`
- `UT-TLS-571-005` — `test/helm/chart_test.go` (rendered chart: bootstrap
  Job staging volume is memory-backed, 16Mi-bounded, container root stays
  read-only)
- Console lifecycle IT — `internal/controller/kubernaut_lifecycle_test.go`
  (memory-backed combined-ca assertion on the reconciled Deployment)
- `UT-TLS-2545-OP-001/002` — the 11 migrated Deployments remain on the
  runtime ambient-CA contract (regression guard for this register's scope)

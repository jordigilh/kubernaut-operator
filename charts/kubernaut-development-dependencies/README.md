# Kubernaut development dependencies

This chart is an explicit, disposable development path for the operator's
bring-your-own (BYO) PostgreSQL and Valkey contract. It is **not** part of the
production operator chart and it does not change the operator's pre-created
dependency requirement.

This is an **internal-only** chart for operator CI/CD testing and upstream demo
cluster infrastructure. It is not a production dependency release and must
not be used as a production database or cache deployment.

The chart has an independent release cadence and version from the operator.
The dedicated publication workflow emits a unique chart version based on the
chart's own `Chart.yaml` version and workflow run, plus a mutable `latest`
convenience alias. Pin the published digest for repeatable internal runs.

The operator repository owns this chart's release cadence and image upgrades.
Each PostgreSQL/Valkey digest change must be reviewed against the target
OpenShift restricted SCC and the Kind qualification lane; the chart does not
inherit versions from the production operator's related-image map. Platform
administrators remain responsible for production database lifecycle,
backups, upgrades, and certificates.

## One-command bootstrap

The default images are immutable references to PostgreSQL 16 and Valkey 8. The
PostgreSQL image is the OpenShift-compatible RHEL image and the rendered Pods
use restricted-compatible container settings. The default PostgreSQL server
generates a short-lived, in-cluster certificate and rejects non-TLS host
connections.

```bash
helm upgrade --install kubernaut-development-dependencies \
  ./charts/kubernaut-development-dependencies \
  --namespace kubernaut-system \
  --create-namespace \
  --wait --timeout 10m
```

## Demo clusters from the OCI release

The dedicated development-dependencies publication workflow publishes this
chart to Quay as an OCI artifact. Use the immutable digest from that workflow's
summary or its `kubernaut-development-dependencies-oci.json` artifact:

```bash
helm upgrade --install kubernaut-development-dependencies \
  oci://quay.io/kubernaut-ai/kubernaut-development-dependencies@sha256:<chart-digest> \
  --namespace kubernaut-system \
  --create-namespace \
  --wait --timeout 10m
```

The dedicated development-dependencies publication workflow writes the same
reference to `kubernaut-development-dependencies-oci.json`. The operator
release provenance does not include this internal-only chart because it has an
independent release cadence.

The version tag is also available when digest pinning is not required:

```bash
helm upgrade --install kubernaut-development-dependencies \
  oci://quay.io/kubernaut-ai/kubernaut-development-dependencies \
  --version <release-version> \
  --namespace kubernaut-system \
  --create-namespace \
  --wait --timeout 10m
```

The chart creates:

* selector-backed `postgresql` and `valkey` ClusterIP Services;
* one disposable Deployment for each dependency, with readiness/liveness
  probes and `emptyDir` storage by default;
* a pre-install/pre-upgrade hook that generates `postgresql-secret` at runtime
  with random `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, and
  `db-secrets.yaml` values;
* the same hook generates `valkey-secret` at runtime with the operator-required
  `valkey-secrets.yaml` compatibility key; the value is not used for Valkey
  authentication;
* Valkey server, CA, and shared client certificates, with plaintext Valkey
  disabled and mTLS client authentication required;
* `kubernaut-development-dependencies-contract`, which records the hostnames, Secret
  names, and selected PostgreSQL `sslMode` for the Kubernaut CR.

No database credentials are embedded in the chart values or rendered Secret
manifests. The pre-install hook generates them inside the cluster and preserves
them across upgrades. The generated Valkey password exists only to satisfy the
operator's Secret contract; mTLS is the only Valkey authentication mechanism.

Application credentials, policies, notification routing, and identity-provider
configuration are outside this chart. Create and manage those resources
separately before applying a Kubernaut CR that references them.

## Migration from `kubernaut-dev-bootstrap`

The chart rename changes the Helm release identity and is **not** an
in-place upgrade. Do not install both releases in the same namespace: they
create fixed-name PostgreSQL and Valkey resources, and the old release's
selectors cannot be changed safely by an upgrade.

For a disposable environment, remove the old release before installing this
chart:

```bash
helm uninstall kubernaut-dev-bootstrap \
  --namespace kubernaut-system --wait
```

This deletes the old Helm-owned Deployments, Services, Secrets, and any
Helm-owned PVCs. Back up or migrate data first if persistence was enabled.
Then install `kubernaut-development-dependencies` using one of the commands
above.

Apply the operator CR only after the chart is ready. For the generated
self-signed PostgreSQL certificate, use the supported CR value
`spec.postgresql.sslMode: require` (the chart default). The operator accepts
only `require`, `verify-ca`, and `verify-full`; `disable` is intentionally
rejected. `verify-ca` and `verify-full` require a CA trusted by the operator's
runtime trust bundle and are intended for administrator-managed database
certificates, not this disposable self-signed default.

## Cleanup and ownership

The chart owns only the PostgreSQL/Valkey resources listed above. Remove the whole disposable
environment with:

```bash
helm uninstall kubernaut-development-dependencies --namespace kubernaut-system --wait
```

Do not install this release into a namespace containing administrator-owned
`postgresql`/`valkey` Services or Secrets. Do not add owner references from the
operator CR to these resources; the operator must continue to treat them as
BYO dependencies and must never adopt, mutate, or prune them.

The chart defaults to no PVCs so cleanup is complete. Set
`postgresql.persistence.enabled=true` or `valkey.persistence.enabled=true`
only when a disposable environment needs restart persistence; the resulting
PVCs remain Helm-owned and are removed by the normal uninstall lifecycle.

## Kind qualification override

The Kind harness uses the same chart with locally loaded Docker Hub images and
explicitly disables the chart's OpenShift security-context gate for those
developer images. This keeps the harness on the production BYO path while
making the image/runtime distinction visible rather than silently falling back
to direct `kubectl` setup.

The harness equivalent is:

```bash
helm upgrade --install kubernaut-development-dependencies \
  ./charts/kubernaut-development-dependencies \
  --namespace kubernaut-system --create-namespace \
  --set postgresql.commandMode=official \
  --set postgresql.securityContext.enabled=false \
  --set valkey.securityContext.enabled=false \
  --wait --timeout 10m
```

The OCP E2E suite consumes the default chart path and uninstalls it after the
CR lifecycle completes. This makes database migration failures observable in
the chart-backed `postgresql` Pod and `kubernaut-db-migration` Job logs rather
than hiding dependency setup in test-only Secret creation.

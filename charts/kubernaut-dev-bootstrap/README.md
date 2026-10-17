# Kubernaut development dependency bootstrap

This chart is an explicit, disposable development path for the operator's
bring-your-own (BYO) PostgreSQL and Valkey contract. It is **not** part of the
production operator chart and it does not change the operator's pre-created
dependency requirement.

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
helm upgrade --install kubernaut-dev-bootstrap \
  ./charts/kubernaut-dev-bootstrap \
  --namespace kubernaut-system \
  --create-namespace \
  --wait --timeout 10m
```

The chart creates:

* selector-backed `postgresql` and `valkey` ClusterIP Services;
* one disposable Deployment for each dependency, with readiness/liveness
  probes and `emptyDir` storage by default;
* `postgresql-secret` with `POSTGRES_USER`, `POSTGRES_PASSWORD`, and
  `POSTGRES_DB`;
* `valkey-secret` with the operator-required `valkey-secrets.yaml` key;
* synthetic `llm-credentials`, `aianalysis-policy`, and
  `signalprocessing-policy` fixtures for the sample CR; and
* `kubernaut-dev-bootstrap-contract`, which records the hostnames, Secret
  names, and selected PostgreSQL `sslMode` for the Kubernaut CR.

No real credentials are embedded. Override the disposable values for a local
environment and keep them out of shell history where appropriate:

```bash
helm upgrade --install kubernaut-dev-bootstrap \
  ./charts/kubernaut-dev-bootstrap \
  --namespace kubernaut-system --create-namespace --wait --timeout 10m \
  --set-string postgresql.password='local-only-value' \
  --set-string valkey.password='local-only-value'
```

Apply the operator CR only after the chart is ready. For the generated
self-signed PostgreSQL certificate, use the supported CR value
`spec.postgresql.sslMode: require` (the chart default). The operator accepts
only `require`, `verify-ca`, and `verify-full`; `disable` is intentionally
rejected. `verify-ca` and `verify-full` require a CA trusted by the operator's
runtime trust bundle and are intended for administrator-managed database
certificates, not this disposable self-signed default.

## Cleanup and ownership

The chart owns the resources listed above. Remove the whole disposable
environment with:

```bash
helm uninstall kubernaut-dev-bootstrap --namespace kubernaut-system --wait
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
to direct `kubectl` fixtures.

The harness equivalent is:

```bash
helm upgrade --install kubernaut-dev-bootstrap \
  ./charts/kubernaut-dev-bootstrap \
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

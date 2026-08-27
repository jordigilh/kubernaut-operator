# Infrastructure Prerequisites (BYO)

Before deploying Kubernaut, provision the required backing services and namespace on your OCP cluster.

## Namespace

```bash
oc new-project kubernaut-system
```

## PostgreSQL

Kubernaut requires PostgreSQL 15+ for persistent storage. Use any managed service (RDS, Azure Database, Cloud SQL) or deploy in-cluster.

**In-cluster example (testing only):**

```bash
oc new-app --image=registry.redhat.io/rhel10/postgresql-16:latest \
  -e POSTGRESQL_USER=kubernaut \
  -e POSTGRESQL_PASSWORD=changeme \
  -e POSTGRESQL_DATABASE=kubernaut \
  --name=postgresql \
  -n kubernaut-system
```

### Enable TLS

Kubernaut requires TLS connections to PostgreSQL (`sslMode` defaults to `verify-full`; `disable` is rejected). On OCP, use the service-ca operator to provision a serving certificate.

The `registry.redhat.io/rhel10/postgresql-16` image referenced above only auto-includes
config snippets dropped into `/opt/app-root/src/postgresql-cfg`; it has no built-in
default certificate path, so `ssl_cert_file`/`ssl_key_file` must point at wherever you
actually mount the secret (`/etc/tls` below -- verified against a real deployment of this
exact image; an earlier revision of this doc used `/etc/pki/tls/certs/postgresql`, which
this image does not create or expect, causing `postgres` to fail to start with `FATAL:
could not load server certificate file`):

```bash
# Request a service-ca TLS certificate
oc annotate service postgresql -n kubernaut-system \
  service.beta.openshift.io/serving-cert-secret-name=postgresql-tls

# Create a PostgreSQL config snippet to enable SSL
oc create configmap postgresql-ssl-config -n kubernaut-system \
  --from-literal=postgresql-ssl.conf="$(cat <<CONF
ssl = on
ssl_cert_file = '/etc/tls/tls.crt'
ssl_key_file = '/etc/tls/tls.key'
CONF
)"

# Mount the TLS cert and config into the PostgreSQL deployment
oc patch deployment postgresql -n kubernaut-system --type=json -p '[
  {"op": "add", "path": "/spec/template/spec/volumes", "value": [
    {"name": "tls-certs", "secret": {"secretName": "postgresql-tls", "defaultMode": 384}},
    {"name": "ssl-config", "configMap": {"name": "postgresql-ssl-config"}}
  ]},
  {"op": "add", "path": "/spec/template/spec/containers/0/volumeMounts", "value": [
    {"name": "tls-certs", "mountPath": "/etc/tls", "readOnly": true},
    {"name": "ssl-config", "mountPath": "/opt/app-root/src/postgresql-cfg", "readOnly": true}
  ]}
]'
```

Verify TLS is enabled after the pod restarts:

```bash
oc exec deployment/postgresql -n kubernaut-system -- \
  bash -c 'psql -h localhost -U kubernaut -d kubernaut -c "SHOW ssl"'
# Expected output: ssl = on
```

For testing with the service-ca certificate (not externally verifiable), set `sslMode: require` in the Kubernaut CR. For production with a trusted CA, use `verify-full` (the default).

### Create the operator secret

Keys must be `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: postgresql-secret
  namespace: kubernaut-system
stringData:
  POSTGRES_USER: kubernaut
  POSTGRES_PASSWORD: changeme
  POSTGRES_DB: kubernaut
EOF
```

## Valkey / Redis

Kubernaut requires Valkey 7+ (or Redis 7+) for deduplication and event streaming.

### Authentication: mTLS, not password

A Valkey `requirepass` password provides **no real protection** on its own: the Go
Redis client Kubernaut's services use silently tolerates an `AUTH` failure against
a server with no password configured, so a deployment that forgets to set
`requirepass` (or a client that sends the wrong password) fails open rather than
closed. Upstream `kubernaut` hit this exact gap
([kubernaut#2269](https://github.com/jordigilh/kubernaut/issues/2269)) and closed
it with mutual TLS instead
([kubernaut#2272](https://github.com/jordigilh/kubernaut/pull/2272)): Valkey
requires every client to present a certificate signed by a trusted CA
(`--tls-auth-clients yes`), which fails closed.

Kubernaut's operator-managed services (DataStorage, FleetMetadataCache) already
have client-side mTLS wiring; it activates when `spec.valkey.tls.enabled: true` is
set on the Kubernaut CR. Configure the BYO Valkey below to require mTLS, then set
that field -- see [Enable mTLS](#enable-mtls) below. Plain `requirepass` is
supported for backward compatibility but is **not sufficient authentication** by
itself; enabling mTLS is strongly recommended for any non-throwaway deployment.

### Persistence

By default Valkey enables RDB snapshots (`save` directives) and refuses writes when
a background save fails (`stop-writes-on-bgsave-error yes`). If no writable volume is
mounted at `/data`, the background save fails immediately and Valkey rejects all write
commands, which prevents the DataStorage service from starting.

Choose one of:

| Strategy | When to use |
|---|---|
| **Disable RDB** (`save ""`) | Valkey is used only as a cache/stream broker; data loss on restart is acceptable (typical for Kubernaut). |
| **Mount a PVC** on `/data` | You need persistence across pod restarts (HA / disaster recovery). |

### In-cluster example (testing only, password auth)

The example below disables RDB persistence so that no volume is required. It uses
`requirepass` only, with **no client authentication enforced** -- suitable for a
disposable test cluster, not for anything else. See [Enable mTLS](#enable-mtls)
for a deployment that actually enforces client authentication.

```bash
oc apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: valkey
  namespace: kubernaut-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: valkey
  template:
    metadata:
      labels:
        app: valkey
    spec:
      containers:
        - name: valkey
          image: valkey/valkey:8
          args: ["--requirepass", "changeme", "--save", ""]
          ports:
            - containerPort: 6379
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 256Mi
---
apiVersion: v1
kind: Service
metadata:
  name: valkey
  namespace: kubernaut-system
spec:
  selector:
    app: valkey
  ports:
    - port: 6379
      targetPort: 6379
EOF
```

If you need persistence, add a PVC and mount it at `/data` instead of passing `--save ""`.

### Enable mTLS

This is the recommended configuration: Valkey requires every client to present a
certificate signed by a trusted CA (`--tls-auth-clients yes`), and rejects
connections that don't -- unlike `requirepass`, this fails closed. One CA and one
client certificate is sufficient; Valkey 8 (the version this operator supports)
checks only the certificate chain, not the client's Common Name, so the same
client certificate can be shared by every Kubernaut service that talks to Valkey
(DataStorage, FleetMetadataCache). Per-service certificates are not required.

**1. Generate a CA and a cert/key pair:**

```bash
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 \
  -out ca.crt -subj "/CN=kubernaut-valkey-ca"

openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr \
  -subj "/CN=valkey.kubernaut-system.svc.cluster.local"
cat > server.ext <<EOF
subjectAltName = DNS:valkey, DNS:valkey.kubernaut-system.svc.cluster.local, DNS:valkey.kubernaut-system.svc
extendedKeyUsage = serverAuth, clientAuth
EOF
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 3650 -sha256 -extfile server.ext
```

**2. Create the three secrets Valkey and its clients need:**

```bash
# Valkey's own server certificate (also reused as its client identity for its
# own liveness/readiness probes, which must authenticate like any other client)
oc create secret generic valkey-tls -n kubernaut-system \
  --from-file=tls.crt=server.crt --from-file=tls.key=server.key

# CA bundle -- referenced by spec.valkey.tls.caSecretName so every Kubernaut
# client trusts Valkey's server certificate
oc create secret generic valkey-ca -n kubernaut-system \
  --from-file=ca.crt=ca.crt

# Client certificate shared by every Kubernaut service -- referenced by
# spec.valkey.tls.clientCertSecretName
oc create secret generic valkey-client-cert -n kubernaut-system \
  --from-file=tls.crt=server.crt --from-file=tls.key=server.key
```

**3. Deploy Valkey with the plaintext port disabled and client certs required:**

```bash
oc apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: valkey
  namespace: kubernaut-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: valkey
  template:
    metadata:
      labels:
        app: valkey
    spec:
      containers:
        - name: valkey
          image: valkey/valkey:8
          args:
            - "--port"
            - "0"
            - "--tls-port"
            - "6379"
            - "--tls-cert-file"
            - "/certs/tls.crt"
            - "--tls-key-file"
            - "/certs/tls.key"
            - "--tls-ca-cert-file"
            - "/etc/tls-ca/ca.crt"
            - "--tls-auth-clients"
            - "yes"
            - "--save"
            - ""
          ports:
            - containerPort: 6379
          volumeMounts:
            - name: valkey-tls
              mountPath: /certs
              readOnly: true
            - name: valkey-ca
              mountPath: /etc/tls-ca
              readOnly: true
          readinessProbe:
            exec:
              command: ["valkey-cli", "--tls", "--cacert", "/etc/tls-ca/ca.crt", "--cert", "/certs/tls.crt", "--key", "/certs/tls.key", "-p", "6379", "ping"]
            initialDelaySeconds: 5
            periodSeconds: 5
          livenessProbe:
            exec:
              command: ["valkey-cli", "--tls", "--cacert", "/etc/tls-ca/ca.crt", "--cert", "/certs/tls.crt", "--key", "/certs/tls.key", "-p", "6379", "ping"]
            initialDelaySeconds: 30
            periodSeconds: 10
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 256Mi
      volumes:
        - name: valkey-tls
          secret:
            secretName: valkey-tls
        - name: valkey-ca
          secret:
            secretName: valkey-ca
---
apiVersion: v1
kind: Service
metadata:
  name: valkey
  namespace: kubernaut-system
spec:
  selector:
    app: valkey
  ports:
    - port: 6379
      targetPort: 6379
EOF
```

Add a PVC mounted at `/data` instead of `--save ""` if you need persistence across restarts.

**4. Point the Kubernaut CR at the new secrets** (see [03-deploy.md](03-deploy.md)):

```yaml
spec:
  valkey:
    secretName: valkey-secret   # still required; see note below
    host: valkey.kubernaut-system.svc.cluster.local
    port: 6379
    tls:
      enabled: true
      caSecretName: valkey-ca
      clientCertSecretName: valkey-client-cert
```

DataStorage's config loader still requires `spec.valkey.secretName` to resolve
(the `password` key), even with mTLS enabled -- it can be any value once mTLS is
the actual authentication mechanism, but the secret and key must still exist.

### Production recommendations

- **Resources**: set memory `requests` and `limits` to prevent OOM kills. Valkey is single-threaded; 1 CPU is sufficient for most workloads.
- **High availability**: consider Valkey Sentinel or a managed Redis service (ElastiCache, Azure Cache, Memorystore) for production clusters.
- **Authentication**: enable mTLS ([above](#enable-mtls)) rather than relying on `requirepass` alone -- see [Authentication: mTLS, not password](#authentication-mtls-not-password).

### Troubleshooting

If the DataStorage pod logs show:

```
MISCONF Valkey is configured to save RDB snapshots, but it's currently unable to persist to disk.
```

Valkey cannot write its RDB dump file. Fix by either:

1. Disabling RDB: `valkey-cli CONFIG SET save ""` and `valkey-cli CONFIG SET stop-writes-on-bgsave-error no`
2. Mounting a writable volume at `/data`

If Valkey's logs show a stream of `Error accepting a client connection:
error:0A00010B:SSL routines::wrong version number`, a client is still connecting
in plaintext against a `--tls-port`-only Valkey. This is expected for a few
seconds during the cutover to mTLS while DataStorage/FMC pods roll to pick up
`spec.valkey.tls`, but if it persists, check that the CR patch in [Enable
mTLS](#enable-mtls) step 4 actually applied (`oc get kubernaut kubernaut -n
kubernaut-system -o jsonpath='{.spec.valkey.tls}'`).

### Create the operator secret

The key must be `valkey-secrets.yaml` containing YAML with a `password` field.
This is required by DataStorage's config loader regardless of whether mTLS is
also enabled (see [Enable mTLS](#enable-mtls) step 4):

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: valkey-secret
  namespace: kubernaut-system
stringData:
  valkey-secrets.yaml: |
    password: "changeme"
EOF
```

## LLM Credentials

The Kubernaut Agent requires credentials for an LLM provider. Create a secret named to match what you will set in the profile's `spec.llmProfiles.<name>.credentialsSecretName`.

**OpenAI / Anthropic:**

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: llm-credentials
  namespace: kubernaut-system
stringData:
  credentials.json: |
    {"api_key": "<YOUR_API_KEY>"}
EOF
```

**GCP Vertex AI (service account JSON):**

```bash
oc create secret generic llm-credentials \
  -n kubernaut-system \
  --from-file=credentials.json=/path/to/your-service-account.json
```

## Verification

Confirm all backing services are reachable before proceeding:

```bash
# PostgreSQL pod ready
oc rollout status deployment/postgresql -n kubernaut-system --timeout=2m

# Valkey pod ready
oc rollout status deployment/valkey -n kubernaut-system --timeout=2m

# Secrets exist
oc get secret postgresql-secret valkey-secret llm-credentials -n kubernaut-system
```

---

Previous: [Quickstart](00-quickstart.md) | Next: [Configure Services](02-configure-services.md)

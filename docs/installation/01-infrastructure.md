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

Kubernaut requires TLS connections to PostgreSQL (`sslMode` defaults to `verify-full`; `disable` is rejected). On OCP, use the service-ca operator to provision a serving certificate:

```bash
# Request a service-ca TLS certificate
oc annotate service postgresql -n kubernaut-system \
  service.beta.openshift.io/serving-cert-secret-name=postgresql-tls

# Create a PostgreSQL config snippet to enable SSL
oc create configmap postgresql-ssl-config -n kubernaut-system \
  --from-literal=ssl.conf="$(cat <<CONF
ssl = on
ssl_cert_file = '/etc/pki/tls/certs/postgresql/tls.crt'
ssl_key_file = '/etc/pki/tls/certs/postgresql/tls.key'
CONF
)"

# Mount the TLS cert and config into the PostgreSQL deployment
oc patch deployment postgresql -n kubernaut-system --type=json -p '[
  {"op": "add", "path": "/spec/template/spec/volumes", "value": [
    {"name": "tls-certs", "secret": {"secretName": "postgresql-tls", "defaultMode": 384}},
    {"name": "ssl-config", "configMap": {"name": "postgresql-ssl-config"}}
  ]},
  {"op": "add", "path": "/spec/template/spec/containers/0/volumeMounts", "value": [
    {"name": "tls-certs", "mountPath": "/etc/pki/tls/certs/postgresql", "readOnly": true},
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

### In-cluster example (testing only)

The example below disables RDB persistence so that no volume is required:

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

### Production recommendations

- **Resources**: set memory `requests` and `limits` to prevent OOM kills. Valkey is single-threaded; 1 CPU is sufficient for most workloads.
- **High availability**: consider Valkey Sentinel or a managed Redis service (ElastiCache, Azure Cache, Memorystore) for production clusters.
- **TLS**: Valkey supports TLS natively (`--tls-port`, `--tls-cert-file`, `--tls-key-file`). For in-cluster traffic behind NetworkPolicies, plaintext is acceptable.

### Troubleshooting

If the DataStorage pod logs show:

```
MISCONF Valkey is configured to save RDB snapshots, but it's currently unable to persist to disk.
```

Valkey cannot write its RDB dump file. Fix by either:

1. Disabling RDB: `valkey-cli CONFIG SET save ""` and `valkey-cli CONFIG SET stop-writes-on-bgsave-error no`
2. Mounting a writable volume at `/data`

### Create the operator secret

The key must be `valkey-secrets.yaml` containing YAML with a `password` field:

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

The Kubernaut Agent requires credentials for an LLM provider. Create a secret named to match what you will set in `spec.kubernautAgent.llm.credentialsSecretName`.

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

Next: [Configure Services](02-configure-services.md)

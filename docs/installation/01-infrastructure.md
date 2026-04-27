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
oc new-app postgresql:16-el9 \
  -e POSTGRESQL_USER=kubernaut \
  -e POSTGRESQL_PASSWORD=changeme \
  -e POSTGRESQL_DATABASE=kubernaut \
  -n kubernaut-system
```

Create the operator secret. Keys must be `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`:

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

**In-cluster example (testing only):**

```bash
oc new-app valkey/valkey:8 \
  -e VALKEY_PASSWORD=changeme \
  -n kubernaut-system
```

Create the operator secret. The key must be `valkey-secrets.yaml` containing YAML with a `password` field:

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

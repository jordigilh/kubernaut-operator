# Kubernaut Operator

Kubernetes operator for deploying and managing the [Kubernaut](https://kubernaut.ai) autonomous remediation platform on OpenShift (OCP 4.18+).

## Overview

The Kubernaut Operator manages the full lifecycle of a Kubernaut deployment:

- **Validates** BYO PostgreSQL and Valkey secrets
- **Migrates** the database schema via embedded SQL migrations
- **Deploys** 10 microservices, RBAC, ConfigMaps, PDBs, webhooks, and OCP Routes
- **Monitors** workload readiness and reports per-service status
- **Cleans up** all cluster-scoped resources on CR deletion via a finalizer

The operator is designed as a **singleton**: exactly one `Kubernaut` CR named `kubernaut` should exist per cluster.

## Prerequisites

| Requirement | Version |
|---|---|
| OpenShift | 4.18+ |
| PostgreSQL | 15+ (BYO) |
| Valkey/Redis | 7+ (BYO) |
| LLM API key | OpenAI, Anthropic, or GCP Vertex AI |

## Installation via OLM

```bash
# Install from OperatorHub (when published)
oc create -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: kubernaut-operator
  namespace: openshift-operators
spec:
  channel: alpha
  name: kubernaut-operator
  source: community-operators
  sourceNamespace: openshift-marketplace
EOF
```

## CR Configuration

Create a `Kubernaut` CR in any namespace. The operator watches all namespaces.

```yaml
apiVersion: kubernaut.ai/v1alpha1
kind: Kubernaut
metadata:
  name: kubernaut
spec:
  image:
    registry: quay.io
    namespace: kubernaut-ai
    tag: v1.3.0-rc1
  postgresql:
    secretName: kubernaut-pg-secret
    host: postgresql.kubernaut-system.svc.cluster.local
  valkey:
    secretName: kubernaut-valkey-secret
    host: valkey.kubernaut-system.svc.cluster.local
  holmesgptApi:
    llm:
      provider: openai
      model: gpt-4o
      credentialsSecretName: llm-credentials
```

### Required Secrets

**PostgreSQL** (`spec.postgresql.secretName`):
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kubernaut-pg-secret
stringData:
  POSTGRES_USER: kubernaut
  POSTGRES_PASSWORD: <password>
  POSTGRES_DB: kubernaut
```

**Valkey** (`spec.valkey.secretName`):
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kubernaut-valkey-secret
stringData:
  valkey-secrets.yaml: |
    password: <password>
```

**LLM Credentials** (`spec.holmesgptApi.llm.credentialsSecretName`):
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: llm-credentials
stringData:
  credentials.json: |
    {"api_key": "<your-api-key>"}
```

## Development

```bash
# Build
make build

# Run tests
make test

# Build operator image
make docker-build IMG=quay.io/yourorg/kubernaut-operator:dev

# Deploy to connected cluster
make deploy IMG=quay.io/yourorg/kubernaut-operator:dev

# Undeploy
make undeploy
```

## License

Apache License 2.0. See [LICENSE](LICENSE).

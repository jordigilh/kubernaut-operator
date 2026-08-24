# Quickstart: Minimal Kubernaut CR

This page is the fast path to a `Running` Kubernaut deployment: only the fields the operator actually requires, nothing else. Everything not listed here has a working default -- no need to touch it to get started.

If you want the fuller, every-knob-annotated walkthrough instead (LLM reasoning, safety controls, rate limits, Fleet federation, Ansible/AAP, GitOps layout, Slack, etc.), skip ahead to [Configure Services](02-configure-services.md) and [Deploy Kubernaut](03-deploy.md). This page and those two aren't redundant: this one gets you to `Running` fastest; those cover depth.

## Why each field below is required

Mandatory-ness comes from two different places, and it matters which one applies:

- **Schema-required** (enforced by the Kubernetes API server itself, before the object is even persisted): `spec.postgresql`, `spec.valkey`, `spec.llmProfiles`, `spec.kubernautAgent`, `spec.aiAnalysis.policy.configMapName`, `spec.signalProcessing.policy.configMapName`. Omit any of these and `oc apply` is rejected immediately with a validation error -- there's no operator-bundled default for any of them (this matches the upstream Helm chart's own behavior exactly: `helm install` fails the same way).
- **Operator-required** (enforced at reconcile time, after admission succeeds): LLM profile content (`provider`/`model`/`credentialsSecretName`, plus `endpoint` for `provider: openai`), and `apiFrontend.auth.issuerURL` whenever API Frontend is enabled (the default). These pass admission but the CR gets stuck with a `SpecValidationFailed`/`BYOValidated=False` condition until fixed.

## 1. Prerequisites

Provision these before applying the CR (see [Infrastructure Prerequisites](01-infrastructure.md) for full detail):

- A namespace (e.g. `kubernaut-system`).
- PostgreSQL 15+, reachable, with a Secret containing `POSTGRES_USER`/`POSTGRES_PASSWORD`/`POSTGRES_DB`.
- Valkey/Redis 7+, reachable, with a Secret containing key `valkey-secrets.yaml`.
- LLM API credentials (OpenAI, Anthropic, or GCP Vertex AI) in a Secret.
- An OIDC issuer for API Frontend (e.g. RHBK/Keycloak) -- or set `apiFrontend.enabled: false` in the CR below if you don't have one yet and want to add API/Console access later.

Both Rego policy ConfigMaps below are schema-required with no bundled default -- create minimal starter policies now (tune them properly later, see [Configure Services](02-configure-services.md#signal-processing-sp----classification-policy-required) and [Configure Services](02-configure-services.md#ai-analysis-aa----approval-policy-required)):

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: signalprocessing-policy
  namespace: kubernaut-system
data:
  policy.rego: |
    package kubernaut.signalprocessing
    default allow = true
    default priority = "P2"
    default remediation_path = "automated"
EOF

oc apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: aianalysis-policy
  namespace: kubernaut-system
data:
  approval.rego: |
    package kubernaut.aianalysis
    default allow = false
    allow if {
      input.confidence >= 0.8
      input.risk_level != "critical"
    }
EOF
```

## 2. The minimal CR

This is identical to [config/samples/v1alpha2_kubernaut_minimal.yaml](../../config/samples/v1alpha2_kubernaut_minimal.yaml) -- adjust `host`/`secretName`/LLM `provider`/`issuerURL` to match your environment, then apply as-is:

```yaml
apiVersion: kubernaut.ai/v1alpha2
kind: Kubernaut
metadata:
  name: kubernaut
  namespace: kubernaut-system
spec:
  # BYO PostgreSQL (schema-required parent object + host/secretName).
  postgresql:
    host: postgresql.kubernaut-system.svc.cluster.local
    secretName: postgresql-secret   # keys: POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB

  # BYO Valkey/Redis (schema-required parent object + host/secretName).
  valkey:
    host: valkey.kubernaut-system.svc.cluster.local
    secretName: valkey-secret       # key: valkey-secrets.yaml

  # At least one named LLM profile is schema-required. provider/model/
  # credentialsSecretName are always required; endpoint is additionally
  # required for provider: openai. claude-sonnet-4-6 is the model validated
  # against this operator's KA/AF integration -- swap in openai/vertex_ai/
  # bedrock/azure once your own provider is validated.
  llmProfiles:
    primary:
      provider: anthropic           # or: openai, vertex_ai, bedrock, azure
      model: claude-sonnet-4-6
      credentialsSecretName: llm-credentials

  # kubernautAgent is a schema-required parent key, but llmProfileRef itself
  # can stay unset here: exactly one profile is defined above, so the
  # operator infers it automatically (v1alpha2/F10).
  kubernautAgent: {}

  # Both Rego policy ConfigMaps are schema-required -- must already exist
  # (see step 1 above).
  aiAnalysis:
    policy:
      configMapName: aianalysis-policy
  signalProcessing:
    policy:
      configMapName: signalprocessing-policy

  # apiFrontend is enabled by default and requires an OIDC issuer -- replace
  # with your real IdP, or set apiFrontend.enabled: false if you don't have
  # one yet.
  apiFrontend:
    auth:
      issuerURL: "https://login.example.com/realms/kubernaut"
      audience: "kubernaut-apifrontend"
```

Apply it:

```bash
oc apply -f config/samples/v1alpha2_kubernaut_minimal.yaml
```

## 3. Watch the rollout

```bash
oc get kubernaut kubernaut -n kubernaut-system -w
```

The CR progresses through **Validating** -> **Migrating** -> **Deploying** -> **Running**. If it stalls, check `status.conditions` -- see [Deploy Kubernaut: Troubleshooting](03-deploy.md#troubleshooting) for the common causes.

## Before you rely on this for real use

The minimal CR above deliberately omits one thing you should not skip in practice: `apiFrontend.rbac.roleBindings`. It isn't schema-required and isn't checked at reconcile time either, so the CR above reaches `Running` without it -- but with it unset, **every** tool call is rejected for **every** user (including `cluster-admin`), fail-closed by design, with no permissive fallback. The Console itself still opens: its own coarse-grained access gate is authentication-only by default (`apiFrontend.rbac.consoleAccessAuthorizationCheckEnabled` defaults to `false`, kubernaut#2150) -- but every chat/tool action inside it 403s until `roleBindings` is set. Add it as soon as you're ready to actually use API Frontend/Console -- see [Additional RBAC for API Frontend](02-configure-services.md#additional-rbac-for-api-frontend).

## What's next

- [Configure Services](02-configure-services.md) -- LLM reasoning/safety tuning, Ansible/AAP, GitOps layout, Slack, additional RBAC.
- [Deploy Kubernaut](03-deploy.md) -- the fully-annotated CR reference, AlertManager wiring, workflow catalog seeding.
- [Fleet: Kuadrant MCP Gateway](04-fleet-mcp-gateway.md) -- optional, only if you enable `spec.fleet`.

---

Next: [Infrastructure Prerequisites](01-infrastructure.md)

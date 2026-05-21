# Credentials, TLS, and protected communications

**Organization:** Kubernaut AI  
**Product:** Kubernaut Operator (OpenShift 4.18+)  
**Security contact:** jgil@redhat.com  

**NIST 800-53 Rev. 5:** IA-5 (Authenticator Management), SC-8 (Transmission Confidentiality and Integrity)

---

This document summarizes how Kubernaut manages secrets referenced by the operator, expectations for credential rotation, inter-service authentication patterns, TLS configuration, and optional network segmentation controls.

## Managed secrets

The operator and managed workloads expect Kubernetes Secret objects (bring-your-own or provisioned by the customer) with the keys below. Secret names are typically set on the Kubernaut CR.

| Secret | Required keys | Purpose |
|--------|---------------|---------|
| PostgreSQL (`spec.postgresql.secretName`) | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | Database access for DataStorage and migrations |
| Valkey or Redis (`spec.valkey.secretName`) | `password` | Cache and stream access for DataStorage |
| LLM credentials (`spec.kubernautAgent.llm.credentialsSecretName`) | Provider-dependent: `credentials.json` (Vertex AI), `api_key` (OpenAI or Anthropic), and similar | Authenticate to the LLM provider API |
| OAuth2 (optional, `spec.kubernautAgent.llm.oauth2.credentialsSecretName`) | `client_id`, `client_secret` | OAuth2 client-credentials flow for LLM access |
| Notification Slack (optional, `spec.notification.slack.secretName`) | `webhook_url` or `bot_token` | Deliver notifications to Slack |
| Ansible or AAP (optional, `spec.ansible.tokenSecretRef`) | `token` | Authenticate to AWX or Ansible Automation Platform API |

Additional keys may be required for specific provider integrations; consult provider documentation and the Kubernaut CRD for optional fields.

## Credential rotation

The operator watches Secret resources referenced from the Kubernaut CR. When referenced Secret data changes, reconciliation runs so that derived configuration (for example, ConfigMaps that reference secret material) is regenerated and workload Deployments roll forward using a Deployment annotation hash strategy.

For automated rotation at scale, Kubernaut AI recommends integrating External Secrets Operator, HashiCorp Vault CSI, or an equivalent secret lifecycle tool with your OpenShift platform.

LLM API keys and OAuth client secrets should be rotated on a regular schedule; rotating LLM API credentials at least every ninety days is recommended.

## Inter-service authentication

The following table summarizes primary east-west trust patterns for the managed platform. Exact ServiceAccount names and audiences are defined in the operator-managed manifests and may evolve by release.

| Source | Target | Mechanism |
|--------|--------|-----------|
| AIAnalysis | Kubernaut Agent | Mutual TLS using OpenShift service CA, plus projected ServiceAccount token |
| Gateway | DataStorage | Mutual TLS using OpenShift service CA, plus projected ServiceAccount token |
| All controllers | DataStorage | Mutual TLS using OpenShift service CA, plus ServiceAccount token |
| AuthWebhook | Kubernetes API server | TokenReview and SubjectAccessReview APIs |
| EffectivenessMonitor | Prometheus | Mutual TLS using OpenShift service CA, plus ServiceAccount token with `thanos-querier` audience where applicable |
| Kubernaut Agent | External LLM provider | HTTPS with API key or OAuth2 bearer credentials |
| AlertManager | Gateway | Mutual TLS using OpenShift service CA, plus ServiceAccount token (bound via `gateway-signal-source` ClusterRoleBinding) |

## TLS configuration

**Inter-service TLS:** OpenShift injects TLS material for internal services using the platform service CA. Service objects are annotated so workloads receive appropriate certificates.

**External TLS (LLM and corporate egress):** Custom CA bundles for corporate TLS inspection or private PKI may be supplied via `spec.kubernautAgent.llm.tlsCaFile` so the agent trusts required roots when calling external LLM endpoints.

**Platform TLS profile:** The effective TLS minimum version and cipher policy for routes and platform components follows the OpenShift APIServer cluster configuration (`tlsProfile`). Mapping is summarized below.

| OpenShift profile | Minimum TLS | Cipher policy |
|-------------------|-------------|----------------|
| Old | TLS 1.0 | Broad compatibility set |
| Intermediate | TLS 1.2 | Modern cipher suites (typical default) |
| Modern | TLS 1.3 | TLS 1.3 only |
| Custom | Configurable | Administrator-defined |

**PostgreSQL:** Use `spec.postgresql.sslMode` with values `require`, `verify-ca`, or `verify-full`. The default is **verify-full**, which enforces both server authenticity and hostname verification. The `disable` value is rejected at validation time (FedRAMP SC-8).

**Admission webhooks:** TLS for the AuthWebhook admission endpoints is managed using OpenShift service CA patterns appropriate for in-cluster webhook servers.

## NetworkPolicy (SC-7)

Optional `NetworkPolicy` resources tighten pod-to-pod and egress traffic. They are disabled by default (`spec.networkPolicies.enabled: false`). When enabled, the operator creates a default-deny posture plus component-specific allow rules.

Enabling NetworkPolicies requires `spec.networkPolicies.apiServerCIDR` so workloads can reach the Kubernetes API server. Policies constrain Gateway ingress, inter-component gRPC and HTTPS paths, and monitoring egress toward Thanos and AlertManager as defined in the operator implementation.

For questions about this document or security practices, contact **jgil@redhat.com** on behalf of **Kubernaut AI**.

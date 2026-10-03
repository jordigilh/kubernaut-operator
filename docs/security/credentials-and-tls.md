# Credentials, TLS, and protected communications

**Organization:** Kubernaut AI  
**Product:** Kubernaut Operator (Kubernetes and OpenShift; see the platform-support contract)
**Security contact:** jgil@redhat.com  

**NIST 800-53 Rev. 5:** IA-5 (Authenticator Management), SC-8 (Transmission Confidentiality and Integrity)

---

This document summarizes how Kubernaut manages secrets referenced by the operator, expectations for credential rotation, inter-service authentication patterns, TLS configuration, and optional network segmentation controls.

## Managed secrets

The operator and managed workloads expect Kubernetes Secret objects (bring-your-own or provisioned by the customer) with the keys below. Secret names are typically set on the Kubernaut CR.

| Secret | Required keys | Purpose |
|--------|---------------|---------|
| PostgreSQL (`spec.postgresql.secretName`) | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | Database access for DataStorage and migrations |
| Valkey or Redis (`spec.valkey.secretName`) | `password` | Cache and stream access for DataStorage. **Not sufficient authentication on its own** -- see [TLS configuration](#tls-configuration) below |
| Valkey mTLS (optional but recommended, `spec.valkey.tls.caSecretName` / `clientCertSecretName`) | `ca.crt` / `tls.crt`, `tls.key` | Client authentication to Valkey via mutual TLS |
| LLM credentials (`spec.llmProfiles.<name>.credentialsSecretName`) | Provider-dependent: `credentials.json` (Vertex AI), `api_key` (OpenAI or Anthropic), and similar | Authenticate to the LLM provider API |
| OAuth2 (optional, `spec.llmProfiles.<name>.oauth2.credentialsSecretRef`) | `client_id`, `client_secret` | OAuth2 client-credentials flow for LLM access |
| Notification Slack (optional, `spec.notification.slack.secretName`) | `webhook_url` or `bot_token` | Deliver notifications to Slack |
| Ansible or AAP (optional, `spec.ansible.tokenSecretRef`) | `token` | Authenticate to AWX or Ansible Automation Platform API |

Additional keys may be required for specific provider integrations; consult provider documentation and the Kubernaut CRD for optional fields.

## Credential rotation

The operator watches its owned Secret resources and periodically revalidates
administrator-managed or cert-manager Secret references. When referenced
Secret data changes, reconciliation regenerates derived configuration (for
example, ConfigMaps that reference trust material) and workload Deployments
roll forward using a Deployment annotation hash strategy.

For automated rotation at scale, Kubernaut AI recommends integrating External Secrets Operator, HashiCorp Vault CSI, or an equivalent secret lifecycle tool with your OpenShift platform.

LLM API keys and OAuth client secrets should be rotated on a regular schedule; rotating LLM API credentials at least every ninety days is recommended.

## Inter-service authentication

The following table summarizes primary east-west trust patterns for the managed platform. Exact ServiceAccount names and audiences are defined in the operator-managed manifests and may evolve by release.

| Source | Target | Mechanism |
|--------|--------|-----------|
| AIAnalysis | Kubernaut Agent | Mutual TLS using the selected runtime CA (OpenShift service CA only when the optional adapter is discovered), plus projected ServiceAccount token |
| Gateway | DataStorage | Mutual TLS using the selected runtime CA, plus projected ServiceAccount token |
| All controllers | DataStorage | Mutual TLS using the selected runtime CA, plus ServiceAccount token |
| AuthWebhook | Kubernetes API server | TokenReview and SubjectAccessReview APIs |
| EffectivenessMonitor | Prometheus | Mutual TLS using the selected runtime CA, plus ServiceAccount token with `thanos-querier` audience where applicable |
| Kubernaut Agent | External LLM provider | HTTPS with API key or OAuth2 bearer credentials |
| AlertManager | Gateway | Mutual TLS using the selected runtime CA, plus ServiceAccount token (bound via `gateway-signal-source` ClusterRoleBinding) |

## TLS configuration

**Inter-service TLS:** the selected runtime source supplies internal service certificates and the public trust bundle. Administrator-managed and cert-manager sources are read without adoption; explicit development self-signed mode is limited to development/Kind; OpenShift service-CA injection is an optional capability adapter rather than a generic fallback.

**External TLS (LLM and corporate egress):** Custom CA bundles for corporate TLS inspection or private PKI may be supplied via `spec.llmProfiles.<name>.tlsCaFile` so any component resolving to that profile trusts required roots when calling external LLM endpoints.

**Platform TLS profile:** The effective TLS minimum version and cipher policy for routes and platform components follows the OpenShift APIServer cluster configuration (`tlsProfile`). Mapping is summarized below.

| OpenShift profile | Minimum TLS | Cipher policy |
|-------------------|-------------|----------------|
| Old | TLS 1.0 | Broad compatibility set |
| Intermediate | TLS 1.2 | Modern cipher suites (typical default) |
| Modern | TLS 1.3 | TLS 1.3 only |
| Custom | Configurable | Administrator-defined |

**PostgreSQL:** Use `spec.postgresql.sslMode` with values `require`, `verify-ca`, or `verify-full`. The default is **verify-full**, which enforces both server authenticity and hostname verification. The `disable` value is rejected at validation time (FedRAMP SC-8).

**Valkey (IA-5, SC-8):** A `requirepass` password (`spec.valkey.secretName`) does **not** provide real client authentication -- the Go Redis client used by Kubernaut's services silently tolerates an `AUTH` failure against a server with no password configured, so a misconfigured deployment fails open rather than closed (the same gap upstream `kubernaut` closed in [kubernaut#2269](https://github.com/jordigilh/kubernaut/issues/2269)/[#2272](https://github.com/jordigilh/kubernaut/pull/2272)). Set `spec.valkey.tls.enabled: true` with `caSecretName` and `clientCertSecretName` to require mutual TLS instead: Valkey rejects any client that doesn't present a certificate signed by the trusted CA, which fails closed. DataStorage and FleetMetadataCache both already support this; a single shared client certificate is sufficient since Valkey 8 only validates the certificate chain, not the caller's identity. See [01-infrastructure.md: Enable mTLS](../installation/01-infrastructure.md#enable-mtls) for setup steps.

**Admission webhooks:** TLS for the AuthWebhook admission endpoints uses the selected runtime TLS source. OpenShift service-CA injection is used only when the optional OpenShift capability is discovered; generic clusters use the explicit administrator-managed, cert-manager, or development self-signed source.

## NetworkPolicy (SC-7)

Network policy intent is always enabled, but the operator submits it only through a positively identified native provider adapter. Select `spec.networkPolicies.provider` as `Auto`, `Cilium`, `Calico`, or `OVN`. `Auto` requires exactly one active, schema-compatible provider in the supported release range. A plain Kubernetes/Kind cluster without one continues without provider policy objects and reports `ProviderPolicyReady=False`; it never receives a raw Kubernetes `NetworkPolicy` fallback.

The adapters use provider-native API-server identities/entities and do not accept static API-server CIDRs. Unsupported, ambiguous, unavailable, or incompatible providers fail closed and are surfaced in status. Provider CRDs and operators are prerequisites owned by the platform administrator; Kubernaut does not install them.

The operator owns only its labeled native policy objects. It does not prescribe browser-origin policy for the Console or overwrite platform/admin policies; administrators may add supplemental provider-native policy for deployment-specific ingress requirements.

For questions about this document or security practices, contact **jgil@redhat.com** on behalf of **Kubernaut AI**.

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
| OTLP private CA (Issue #478, `spec.<component>.telemetry.tls.caCertSecretRef`) | `ca.crt` by default, or the referenced key | Verify a private OTLP collector certificate |
| OTLP mTLS client (Issue #478, `spec.<component>.telemetry.tls.tlsClientSecretRef`) | `tls.crt`, `tls.key` | Authenticate the telemetry exporter to an OTLP collector |

Additional keys may be required for specific provider integrations; consult provider documentation and the Kubernaut CRD for optional fields.

## Credential rotation

The operator watches its owned Secret resources and periodically revalidates
administrator-managed or cert-manager Secret references. When referenced
Secret data changes, reconciliation regenerates derived configuration (for
example, ConfigMaps that reference trust material) and workload Deployments
roll forward using a Deployment annotation hash strategy.

For automated rotation at scale, Kubernaut AI recommends integrating External Secrets Operator, HashiCorp Vault CSI, or an equivalent secret lifecycle tool with your OpenShift platform.

OTLP CA and client Secrets remain administrator-owned inputs. The operator
validates and mounts them read-only, but never adopts, edits, rotates, or
deletes them. See [OTLP telemetry TLS and operational rotation](#otlp-telemetry-tls-and-operational-rotation)
for the runtime rollout contract.

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

## OTLP telemetry TLS and operational rotation

This section defines the approved Issue #478 runtime contract to be applied by
the implementation for Gateway, DataStorage, and Kubernaut Agent telemetry.
Network OTLP endpoints use implicit TLS and
`host:port` syntax; URI schemes are rejected. An empty endpoint disables network
export, while `logSink`-only and `stdout` modes remain local-only and do not
require telemetry TLS material.

### Trust and client-material sources

- An empty `caFile` uses the process system trust pool. The operator also makes
  the merged system/inter-service ambient trust available before telemetry
  bootstrap so public and private platform trust are not accidentally
  narrowed to the inter-service bundle.
- `caFile` is an administrator-owned path outside the operator's observation
  boundary. The operator validates the path shape, not the external file
  contents.
- `caCertSecretRef` supplies a private CA Secret containing `ca.crt` by
  default. `tlsClientSecretRef` supplies `tls.crt` and `tls.key` together.
- Private keys are mounted read-only and are never copied into ConfigMaps,
  environment variables, status, annotations, or logs.

### Rotation behavior

The operator treats a change to any effective OTLP trust/client-material source
as a startup-boundary change:

1. A referenced OTLP Secret or effective ambient trust source changes.
2. The operator validates the new material before changing a Deployment.
3. If valid, the operator updates a non-sensitive telemetry-material revision
   on the affected network-enabled telemetry Pod templates and performs a
   normal rolling update.
4. `oc rollout status` or `kubectl rollout status` can be used to verify that
   the new Pods become Ready and load the new exporter configuration.

Upstream hot reload remains useful for inter-service HTTP clients and inbound
server certificates, but it is not relied on to refresh the OTLP exporter.
This avoids different freshness guarantees between CA, client certificate, and
key rotations. An ambient trust revision may therefore cause a rolling update
even when another non-OTLP client can hot-reload the same CA.

Only affected telemetry producers are restarted. Local-only telemetry lanes
and unrelated workloads are not restarted for an OTLP material event. A
conservative implementation may restart all network-enabled telemetry
producers that execute ambient trust bootstrap when the shared ambient bundle
changes.

### Invalid rotation and recovery

If a referenced Secret is missing, lacks a required key, contains invalid PEM,
or has a mismatched client certificate/key pair, the operator reports the
failure through the Kubernaut status condition and does not deliberately
replace the last known-good Deployment. The same rule applies to an invalid
ambient trust bundle.

Operators should:

1. Inspect the CR condition and operator events for the field-qualified
   telemetry validation error.
2. Correct or restore the administrator-owned Secret or trust source.
3. Wait for reconciliation and verify the resulting Deployment rollout.
4. Confirm the component logs and collector-side telemetry show successful
   exporter startup/connection.

The operator does not roll back or rewrite administrator-owned Secret data.
For `caFile`, `certFile`, or `keyFile` paths whose bytes are managed outside a
watched Kubernetes Secret, the operator cannot detect content rotation. After
updating those files, administrators must restart the affected Deployment and
verify the rollout manually, for example:

```bash
oc rollout restart deployment/<component> -n <namespace>
oc rollout status deployment/<component> -n <namespace> --timeout=5m
```

Use `gateway`, `datastorage`, or `kubernaut-agent` for `<component>` as
appropriate.

Use the equivalent `kubectl` commands on generic Kubernetes. Never place
certificate or private-key contents in command output, issue comments, or
support bundles.

## NetworkPolicy (SC-7)

Network policy intent is always enabled, but the operator submits it only through a positively identified native provider adapter. Select `spec.networkPolicies.provider` as `Auto`, `Cilium`, `Calico`, or `OVN`. `Auto` requires exactly one active, schema-compatible provider in the supported release range. A plain Kubernetes/Kind cluster without one continues without provider policy objects and reports `ProviderPolicyReady=False`; it never receives a raw Kubernetes `NetworkPolicy` fallback.

The adapters use provider-native API-server identities/entities and do not accept static API-server CIDRs. Unsupported, ambiguous, unavailable, or incompatible providers fail closed and are surfaced in status. Provider CRDs and operators are prerequisites owned by the platform administrator; Kubernaut does not install them.

The operator owns only its labeled native policy objects. It does not prescribe browser-origin policy for the Console or overwrite platform/admin policies; administrators may add supplemental provider-native policy for deployment-specific ingress requirements.

For questions about this document or security practices, contact **jgil@redhat.com** on behalf of **Kubernaut AI**.

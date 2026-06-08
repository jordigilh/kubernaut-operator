# Upgrade Guide: 1.4 → 1.5

## Prerequisites

- OpenShift 4.17+ cluster
- Kubernaut Operator 1.4.x running
- `oc` CLI authenticated with cluster-admin

## Breaking Changes

### MCPServerRegistration endpoint DNS fix

The MCP endpoint URL has been corrected from `apifrontend-service.<ns>` to
`apifrontend.<ns>`. If you have NetworkPolicies or firewall rules referencing
the old DNS name, update them.

### MCPGatewayHTTPRoute / MCPServerRegistration return errors

These resource builder functions now return `(*unstructured.Unstructured, error)`
instead of `*unstructured.Unstructured`. If you import these functions in
external tooling, update callers to handle the error return.

### AlertManager webhook TLS

The AlertManager → Gateway webhook no longer uses `InsecureSkipVerify: true`.
It now references the OCP service-CA trust bundle via the `inter-service-ca`
ConfigMap. Ensure this ConfigMap exists in the operator namespace (created
automatically by the operator when monitoring is enabled).

## New CRD Fields

All new fields are optional with backward-compatible defaults.

### LLM mTLS (`spec.kubernautAgent.llm`)

| Field | Type | Description |
|-------|------|-------------|
| `tlsCertFile` | string | Path to client certificate for mTLS |
| `tlsKeyFile` | string | Path to client key for mTLS |
| `tlsClientSecretRef` | string | Secret containing tls.crt and tls.key |

All three must be set together, or all left empty.

### API Frontend configurable ports (`spec.apiFrontend`)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `metricsPort` | int32 | 9090 (or 9092 with sidecar) | Override metrics port |
| `healthPort` | int32 | 8081 (or 8082 with sidecar) | Override health probe port |

### TokenReview audience (`spec.apiFrontend.auth`)

| Field | Type | Description |
|-------|------|-------------|
| `tokenReviewAudience` | string | Audience for K8s SA token validation |

When empty, a warning is logged (FedRAMP IA-5 recommendation).

## New Features

### Singleton webhook

A `ValidatingWebhookConfiguration` is now registered by the operator to enforce
the singleton Kubernaut CR constraint at admission time. Previously, duplicate
CRs were silently ignored by the controller.

The webhook uses `FailurePolicy: Ignore` so it does not block the API server if
the operator is down.

### ClusterSPIFFEID

When `spec.apiFrontend.spire.enabled=true` and the `clusterspiffeids.spire.spiffe.io`
CRD is present, the operator creates a `ClusterSPIFFEID` for the API Frontend
service account. Set `spec.apiFrontend.spire.className` if your SPIRE installation
uses a non-default class.

### Full ServiceMonitor coverage

All 11 components now have ServiceMonitors when monitoring is enabled. Previously
only AF, DS, and KA had them.

## Upgrade Steps

1. **Update the CRD** before upgrading the operator:
   ```bash
   oc apply -f config/crd/bases/kubernaut.ai_kubernauts.yaml
   ```

2. **Upgrade the operator image** to 1.5.0.

3. **(Optional)** Configure new fields in your Kubernaut CR:
   ```yaml
   spec:
     apiFrontend:
       auth:
         tokenReviewAudience: "kubernaut-apifrontend"
     kubernautAgent:
       llm:
         # Only if using mTLS to LLM endpoint:
         tlsCertFile: "/etc/kubernaut-agent/llm-tls-client/tls.crt"
         tlsKeyFile: "/etc/kubernaut-agent/llm-tls-client/tls.key"
         tlsClientSecretRef: "llm-client-tls"
   ```

4. **Verify** the operator is running:
   ```bash
   oc get pods -l app.kubernetes.io/name=kubernaut-operator
   oc get kubernaut -o jsonpath='{.items[0].status.phase}'
   ```

## Rollback

To roll back to 1.4.x:

1. Scale down the 1.5.0 operator deployment.
2. Re-apply the 1.4.x CRD (new fields are ignored by 1.4.x).
3. Deploy the 1.4.x operator image.
4. Remove any 1.5.0-specific CR fields (`tlsCertFile`, `tlsKeyFile`,
   `tlsClientSecretRef`, `metricsPort`, `healthPort`, `tokenReviewAudience`).

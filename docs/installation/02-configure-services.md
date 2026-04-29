# Configure Services

This section covers the ConfigMaps and Secrets required by each Kubernaut service before deploying the CR.

## Kubernaut Agent (KA) -- LLM Configuration

The operator auto-generates the KA SDK config from the `provider` and `model` fields in the CR. For advanced LLM setups (Vertex AI, tool use, MCP servers, custom endpoints), create an SDK config ConfigMap and reference it via `sdkConfigMapName`:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: kubernaut-agent-sdk-config
  namespace: kubernaut-system
data:
  sdk-config.yaml: |
    llm:
      provider: vertex_ai
      model: claude-sonnet-4-6
EOF
```

When `sdkConfigMapName` is set in the CR, it overrides the `provider` and `model` fields.

If you use a simple provider (OpenAI, Anthropic) with no advanced features, skip this step -- the operator generates the ConfigMap for you.

## Signal Processing (SP) -- Classification Policy

The SP controller uses a Rego policy to classify incoming signals by priority and remediation path. If you omit `policy.configMapName` from the CR, the operator creates a permissive stub. For production, provide a real policy.

The ConfigMap must contain the key `policy.rego`:

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

    priority = "P1" {
      input.severity == "critical"
    }

    priority = "P3" {
      input.severity == "info"
    }

    remediation_path = "manual" {
      input.environment == "production"
      input.severity == "critical"
    }
EOF
```

### Proactive signal mappings (optional)

To enable proactive remediation for `predict_linear()` alerts, create a mapping ConfigMap. Signals matching a key are classified as `proactive` and normalized to the base type so existing workflows are reused:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: proactive-signal-mappings
  namespace: kubernaut-system
data:
  proactive-signal-mappings.yaml: |
    proactive_signal_mappings:
      PredictedOOMKill: OOMKilled
      PredictedCPUThrottling: CPUThrottling
      PredictedDiskPressure: DiskPressure
      PredictedNodeNotReady: NodeNotReady
EOF
```

Reference it in the CR under `spec.signalProcessing.proactiveSignalMappings.configMapName`.

## AI Analysis (AA) -- Approval Policy

The AA controller uses a Rego policy to decide whether an AI-generated remediation plan should be auto-approved or require human review. The ConfigMap must contain the key `approval.rego`:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: aianalysis-policies
  namespace: kubernaut-system
data:
  approval.rego: |
    package kubernaut.aianalysis

    default allow = false

    allow {
      input.confidence >= 0.8
      input.risk_level != "critical"
    }
EOF
```

## Ansible Automation Platform (AAP) -- Optional

If you have AWX or AAP and want Kubernaut to execute Ansible-based remediation workflows, configure the integration in the CR:

```yaml
spec:
  ansible:
    enabled: true
    apiURL: "https://awx.example.com"
    organizationID: 1
    tokenSecretRef:
      name: awx-token
      key: token
```

Create the token secret:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: awx-token
  namespace: kubernaut-system
stringData:
  token: "<YOUR_AWX_API_TOKEN>"
EOF
```

### Custom CA for AAP/AWX TLS

If your AAP/AWX endpoint uses a self-signed certificate or a private CA, provide the CA certificate so the operator can establish trust:

```bash
oc create secret generic aap-ca-cert \
  --from-file=ca.crt=/path/to/aap-ca.pem \
  -n kubernaut-system
```

Then reference it in the CR:

```yaml
spec:
  ansible:
    enabled: true
    apiURL: "https://awx.example.com"
    caCertSecretRef:
      name: aap-ca-cert
      key: ca.crt   # default, can be omitted
```

If your AAP uses a publicly trusted CA (e.g., Let's Encrypt), omit `caCertSecretRef` — the system trust store handles it automatically.

If you do not use Ansible, omit the `ansible` block entirely (it defaults to disabled).

## ArgoCD / GitOps Integration

Kubernaut integrates with GitOps workflows natively. The Kubernaut CR and all prerequisite ConfigMaps and Secrets can be managed as manifests in a Git repository and synced by ArgoCD or Flux.

Recommended repository layout:

```
kubernaut-ocp/
  namespace.yaml
  secrets/
    postgresql-secret.yaml          # SealedSecret or ExternalSecret
    valkey-secret.yaml
    llm-credentials.yaml
  configmaps/
    signalprocessing-policy.yaml
    aianalysis-policies.yaml
    kubernaut-agent-sdk-config.yaml  # if using advanced LLM
  kubernaut-cr.yaml
```

The operator watches for CR changes and reconciles automatically. ConfigMap changes to Rego policies are picked up via hot-reload without pod restarts.

## Slack Notifications (optional)

To deliver notifications to Slack, create a webhook secret and configure it in the CR:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: slack-webhook
  namespace: kubernaut-system
stringData:
  webhook-url: "https://hooks.slack.com/services/T.../B.../..."
EOF
```

```yaml
spec:
  notification:
    slack:
      secretName: slack-webhook
      channel: "#kubernaut-alerts"
```

If omitted, notifications are delivered to the console log and file output only.

## Verification

Confirm all ConfigMaps are created:

```bash
oc get configmap -n kubernaut-system \
  signalprocessing-policy \
  aianalysis-policies
```

If using advanced LLM or proactive mappings:

```bash
oc get configmap -n kubernaut-system \
  kubernaut-agent-sdk-config \
  proactive-signal-mappings
```

## Additional RBAC for Kubernaut Agent

By default, the operator creates a base `kubernaut-agent-investigator` ClusterRole
covering well-known Kubernetes resources (pods, deployments, storage, networking,
etc.). If your environment includes custom CRDs that the KA agent should be able
to investigate, use `spec.kubernautAgent.additionalClusterRoleBindings` to layer
on pre-existing ClusterRoles:

```yaml
spec:
  kubernautAgent:
    additionalClusterRoleBindings:
      - strimzi-kafka-reader        # Kafka topics, brokers
      - knative-service-reader      # Knative Serving resources
      - my-app-crds-viewer          # Your custom application CRDs
```

The operator creates one ClusterRoleBinding per entry, binding the named
ClusterRole to the `kubernaut-agent-sa` ServiceAccount. It does **not** create
or manage the ClusterRoles themselves — you must create them separately.

The `AdditionalRBACBound` status condition reports whether all referenced
ClusterRoles exist:
- `FullyBound` — all ClusterRoles found
- `PartiallyBound` — CRBs created but some ClusterRoles don't exist yet (check
  the condition message for details)

### Security considerations

Anyone with `update` permission on the `kubernauts.kubernaut.ai` CR can bind
**any** ClusterRole to the KA ServiceAccount, including highly privileged roles
like `cluster-admin`. RBAC on the Kubernaut CR itself is the access control
boundary. Restrict who can edit the CR using standard Kubernetes RBAC.

### Operational notes

- The `AdditionalRBACBound` condition updates every reconcile cycle (~60s). If
  you create a referenced ClusterRole after the CR, the condition will reflect it
  within one minute.
- Removing entries from the list automatically prunes the corresponding
  ClusterRoleBindings.
- **Downgrade cleanup**: If downgrading to an operator version without this
  feature, remove orphaned CRBs manually:
  ```bash
  kubectl delete clusterrolebinding -l kubernaut.ai/additional-agent-rbac=true
  ```

---

Previous: [Infrastructure Prerequisites](01-infrastructure.md) | Next: [Deploy Kubernaut](03-deploy.md)

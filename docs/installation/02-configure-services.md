# Configure Services

This section covers the ConfigMaps and Secrets required by each Kubernaut service before deploying the CR.

## Kubernaut Agent (KA) -- LLM Configuration

The operator auto-generates the KA LLM runtime config from the `provider`, `model`, and related fields in the CR. For advanced LLM setups (Vertex AI, Bedrock, Azure OpenAI, custom endpoints), set the relevant CR fields:

```yaml
spec:
  kubernautAgent:
    llm:
      provider: openai
      model: gpt-4o
      credentialsSecretName: llm-credentials
      endpoint: ""                  # custom LLM endpoint (optional)
      temperature: "0.7"            # string, parsed to float (optional)
      maxRetries: 3                 # retry count (optional)
      timeoutSeconds: 120           # per-request timeout (optional)
      # Vertex AI:
      # vertexProject: my-project
      # vertexLocation: us-central1
      # Bedrock:
      # bedrockRegion: us-east-1
      # Azure OpenAI:
      # azureApiVersion: "2024-02-01"
      # Custom CA for LLM TLS:
      # tlsCaFile: /path/to/ca.pem
```

### OAuth2 authentication for LLM endpoints

If your LLM endpoint requires OAuth2 token exchange (e.g. corporate proxy, IAP), configure it in the CR:

```yaml
spec:
  kubernautAgent:
    llm:
      oauth2:
        enabled: true
        tokenURL: "https://auth.example.com/token"
        scopes:
          - "openai:chat"
        credentialsSecretRef:
          name: oauth2-credentials
```

Create the credentials secret containing client ID and secret:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: oauth2-credentials
  namespace: kubernaut-system
stringData:
  client_id: "<YOUR_CLIENT_ID>"
  client_secret: "<YOUR_CLIENT_SECRET>"
EOF
```

### Alignment check (shadow agent verification)

The alignment check feature runs a shadow agent that independently verifies each investigation step. Enable it in the CR:

```yaml
spec:
  kubernautAgent:
    alignmentCheck:
      enabled: true
      timeout: "10s"
      maxStepTokens: 500
      llm:                      # optional: use a different LLM for alignment
        provider: openai
        model: gpt-4o-mini
```

When enabled, the agent will flag investigation steps that diverge from the shadow agent's analysis.

### Safety controls

The agent ships with safety controls enabled by default. Customize thresholds in the CR:

```yaml
spec:
  kubernautAgent:
    safety:
      sanitization:
        injectionPatternsEnabled: true    # detect prompt injection patterns
        credentialScrubEnabled: true      # scrub credentials from tool output
      anomaly:
        maxToolCallsPerTool: 10           # max calls to a single tool per investigation
        maxTotalToolCalls: 40             # max total tool calls per investigation
        maxRepeatedFailures: 3            # abort after N consecutive tool failures
```

### Tool output summarizer

Large tool outputs are automatically summarized before being sent to the LLM. Configure thresholds:

```yaml
spec:
  kubernautAgent:
    summarizer:
      threshold: 8000           # token count that triggers summarization
      maxToolOutputSize: 100000 # max tool output size in bytes (truncated beyond this)
```

For fully custom LLM runtime configs (e.g. MCP servers, tool-use), create an LLM runtime ConfigMap and reference it via `runtimeConfigMapName`:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: custom-llm-runtime
  namespace: kubernaut-system
data:
  llm-runtime.yaml: |
    llm:
      provider: vertex_ai
      model: claude-sonnet-4-6
EOF
```

```yaml
spec:
  kubernautAgent:
    llm:
      runtimeConfigMapName: custom-llm-runtime
```

When `runtimeConfigMapName` is set, the operator skips generating the LLM runtime ConfigMap and mounts the user-provided one instead.

If you use a simple provider (OpenAI, Anthropic) with no advanced features, skip BYO config -- the operator generates the ConfigMap for you.

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

    priority = "P1" if {
      input.severity == "critical"
    }

    priority = "P3" if {
      input.severity == "info"
    }

    remediation_path = "manual" if {
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

    allow if {
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

## Gateway Configuration (optional)

The Gateway accepts signals from AlertManager and routes them to Signal Processing. Tune its behavior in the CR:

```yaml
spec:
  gateway:
    route:
      enabled: true
    logging:
      level: info               # debug, info, warn, error
    config:
      trustedProxyCIDRs:        # CIDRs trusted for X-Forwarded-For headers
        - "10.128.0.0/14"
      deduplicationCooldown: "5m"   # dedup window for identical signals
      k8sRequestTimeout: "15s"     # timeout for K8s API calls during fingerprinting
```

All gateway config fields are optional; the operator uses sensible defaults when omitted.

## Remediation Orchestrator Tuning (optional)

The Remediation Orchestrator manages the full lifecycle of remediation requests. Customize timeouts, routing behavior, and dry-run mode:

```yaml
spec:
  remediationOrchestrator:
    dryRun: false                          # when true, plans are created but not executed
    dryRunHoldPeriod: "1h"                 # how long dry-run plans are held before expiry
    timeouts:
      global: "1h"
      processing: "5m"
      analyzing: "10m"
      executing: "30m"
      awaitingApproval: "15m"
      verifying: "30m"
    routing:
      consecutiveFailureThreshold: 3       # failures before circuit-breaker cooldown
      consecutiveFailureCooldown: "1h"
      recentlyRemediatedCooldown: "5m"     # dedup window for repeated signals
      exponentialBackoffBase: "1m"
      exponentialBackoffMax: "10m"
      noActionRequiredDelayHours: 24       # re-evaluation delay for no-action signals
    effectivenessAssessment:
      stabilizationWindow: "5m"            # wait time before verifying remediation
    asyncPropagation:
      gitOpsSyncDelay: "3m"                # allow GitOps sync before verification
      operatorReconcileDelay: "1m"
      proactiveAlertDelay: "5m"
    notifications:
      notifySelfResolved: false            # notify when signals self-resolve
    retention:
      period: "24h"                        # data retention period
```

All fields are optional; the operator uses the defaults shown above.

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
    custom-llm-runtime.yaml          # if using BYO LLM runtime config
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

If using BYO LLM runtime config or proactive mappings:

```bash
oc get configmap -n kubernaut-system \
  custom-llm-runtime \
  proactive-signal-mappings
```

## Additional RBAC for Kubernaut Agent

By default, the operator creates a `kubernaut-agent-investigator` ClusterRole
with **read-only** access to:

- **Core Kubernetes**: Pods, Deployments, StatefulSets, DaemonSets, Jobs, Services,
  Secrets, ConfigMaps, Events, Namespaces, Nodes, PersistentVolumes,
  PersistentVolumeClaims, Ingresses, NetworkPolicies, HPAs, PDBs, ReplicaSets,
  ResourceQuotas, LimitRanges, ServiceAccounts, Endpoints
- **RBAC & admission**: Roles, ClusterRoles, RoleBindings, ClusterRoleBindings,
  ValidatingWebhookConfigurations, MutatingWebhookConfigurations, CRDs,
  PriorityClasses
- **OCP platform**: Routes, DeploymentConfigs, SecurityContextConstraints,
  ImageStreams, Builds, ClusterOperators, ClusterVersions, Infrastructures,
  AppliedClusterResourceQuotas
- **OCP machine management**: Machines, MachineSets, MachineHealthChecks,
  MachineConfigs, MachineConfigPools
- **OCP networking**: EgressNetworkPolicies, HostSubnets, NetNamespaces
- **OLM**: ClusterServiceVersions, Subscriptions, InstallPlans, OperatorGroups,
  CatalogSources, PackageManifests
- **Ecosystem**: Istio (AuthorizationPolicy, PeerAuthentication, VirtualService,
  DestinationRule, Gateway, ServiceEntry), Linkerd (Server, ServerAuthorization),
  cert-manager (Certificate, Issuer, ClusterIssuer), ArgoCD (Application,
  AppProject), Prometheus (ServiceMonitor, PodMonitor, PrometheusRule)

If your environment includes custom CRDs that the KA agent should be able
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

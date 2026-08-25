# Configure Services

This section covers the ConfigMaps and Secrets required by each Kubernaut service before deploying the CR.

## LLM Profiles

LLM configuration lives in a top-level, named-profile map at `spec.llmProfiles`, not nested under any one component. Each component then *references* a profile by name:

- `spec.kubernautAgent.llmProfileRef` (required) -- KA's investigation LLM.
- `spec.apiFrontend.llmProfileRef` (optional) -- AF's own LLM connection. When empty, AF defaults to KA's profile (`kubernautAgent.llmProfileRef`).
- `spec.apiFrontend.severityTriage.llmProfileRef` (optional) -- an independent LLM for severity-triage's fallback tiers, distinct from AF's main connection. When empty, triage inherits AF's resolved profile.
- `spec.kubernautAgent.phaseModels` (optional) -- per-phase overrides (`rca`, `workflow_discovery`, `validation`), each naming a profile.

This lets you, for example, use a stronger model for the primary agent and a cheaper/faster one for a specific investigation phase, all from one place:

```yaml
spec:
  llmProfiles:
    primary:
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
    lightweight:
      provider: openai
      model: gpt-4o-mini
      credentialsSecretName: llm-credentials   # see constraint below

  kubernautAgent:
    llmProfileRef: primary
    phaseModels:
      workflow_discovery: lightweight
```

> **Constraint**: a profile referenced from `phaseModels` or `severityTriage.llmProfileRef` must share the *same* `credentialsSecretName` as the profile it overrides (KA's for `phaseModels`, AF's resolved profile for `severityTriage`). Cross-credential overrides aren't supported yet -- the operator rejects the CR with a validation error naming the offending field if they don't match. You can still point them at a different provider/model/endpoint, as long as the credentials Secret is the same one.

The operator auto-generates each component's LLM runtime config from its resolved profile. If your environment includes custom CRDs or advanced routing needs beyond `provider`/`model`, see the BYO runtime ConfigMap section below.

### Independent LLM for AI Frontend severity-triage

By default, severity-triage's LLM-based fallback tiers inherit whichever profile API Frontend itself resolves to. To give triage its own profile (e.g. a cheaper model, since triage calls are higher-volume) or to disable LLM-based triage entirely (falling back to upstream's rule-based-only triager):

```yaml
spec:
  llmProfiles:
    triage:
      provider: openai
      model: gpt-4o-mini
      credentialsSecretName: llm-credentials   # must match AF's resolved profile

  apiFrontend:
    severityTriage:
      llmProfileRef: triage   # omit to inherit AF's resolved profile (default)
      llmEnabled: true        # set to false to force the rule-based-only fallback
```

### Reasoning/thinking tokens

Model-aware reasoning (a.k.a. thinking tokens) is disabled by default on every profile until you explicitly opt in. Both Kubernaut Agent's main LLM connection and API Frontend's (main agent *and* independent severity-triage) forward the same `reasoning` block from whichever profile they resolve to:

```yaml
spec:
  llmProfiles:
    primary:
      provider: anthropic
      model: claude-sonnet-4-6
      credentialsSecretName: llm-credentials
      reasoning:
        enabled: true
        effort: high              # none | minimal | low | medium | high | xhigh
        # budgetTokens: 4096      # exact-value override; wins over effort for Anthropic when set
        # capabilityOverride: auto  # auto (default) | force_on | force_off -- self-hosted/custom models only
    lightweight:
      provider: anthropic
      model: claude-haiku-4-5
      credentialsSecretName: llm-credentials
      reasoning:
        enabled: true
        effort: minimal            # cheaper/faster tier for the phase it's swapped into below

  kubernautAgent:
    llmProfileRef: primary
    phaseModels:
      workflow_discovery: lightweight   # gets its own reasoning policy, independent of primary's
```

`effort` is a unified, provider-agnostic depth knob: the same value means the same thing regardless of which provider a profile points at, but each provider's client maps it into its own wire dialect (e.g. Anthropic's thinking-level tiers, OpenAI/Azure o-series and gpt-5's `reasoning_effort`, DeepSeek's own two-tier dialect). Providers with no effort-dial concept simply ignore it.

> **Constraint**: for Anthropic-family providers (`anthropic`, and Claude models served via `vertex_ai`), `effort: "none"` combined with `enabled: true` is rejected -- Anthropic has no "thinking enabled, zero effort" wire state. Use `enabled: false` to fully disable reasoning, or `effort: "minimal"` for Anthropic's lowest real tier.

Because `reasoning` is a per-profile field, `severityTriage.llmProfileRef` and every `phaseModels` entry each get their own independent reasoning policy -- pointing a phase or triage at a different profile changes its reasoning budget/effort along with everything else on that profile, without touching the base agent's.

### OAuth2 authentication for LLM endpoints

If your LLM endpoint requires OAuth2 token exchange (e.g. corporate proxy, IAP), configure it on the profile:

```yaml
spec:
  llmProfiles:
    primary:
      oauth2:
        enabled: true
        tokenURL: "https://auth.example.com/token"
        scopes:
          - "openai:chat"
        credentialsSecretRef: oauth2-credentials
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

Any component resolving to this profile (KA, and AF if it shares the profile) mounts the same Secret and authenticates the same way.

### Alignment check (shadow agent verification)

The alignment check feature runs a shadow agent that independently verifies each investigation step. Enable it in the CR:

```yaml
spec:
  kubernautAgent:
    alignmentCheck:
      enabled: true
      timeout: "10s"
      maxStepTokens: 500
      llmProfileRef: lightweight   # optional: use a different named profile (from spec.llmProfiles) for alignment; defaults to kubernautAgent's own resolved profile
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
    runtimeConfigMapName: custom-llm-runtime
```

When `runtimeConfigMapName` is set, the operator skips generating the LLM runtime ConfigMap and mounts the user-provided one instead.

If you use a simple provider (OpenAI, Anthropic) with no advanced features, skip BYO config -- the operator generates the ConfigMap for you.

## Signal Processing (SP) -- Classification Policy (Required)

The SP controller uses a Rego policy to classify incoming signals by priority and remediation path. **This is a required prerequisite** -- the operator will not create a default policy. You must provide a ConfigMap with your Rego policy and reference it in `spec.signalProcessing.policy.configMapName`. The operator will reject the CR with a validation error if this field is empty.

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

## AI Analysis (AA) -- Approval Policy (Required)

The AA controller uses a Rego policy to decide whether an AI-generated remediation plan should be auto-approved or require human review. **This is a required prerequisite** -- the operator will not create a default policy. You must provide a ConfigMap with your Rego policy and reference it in `spec.aiAnalysis.policy.configMapName`. The operator will reject the CR with a validation error if this field is empty.

The ConfigMap must contain the key `approval.rego`:

```bash
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

## Ansible Automation Platform (AAP) -- Optional

If you have AWX or AAP and want Kubernaut to execute Ansible-based remediation workflows, configure the integration in the CR:

```yaml
spec:
  workflowExecution:
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
  workflowExecution:
    ansible:
      enabled: true
      apiURL: "https://awx.example.com"
      caCertSecretRef:
        name: aap-ca-cert
        key: ca.crt   # default, can be omitted
```

If your AAP uses a publicly trusted CA (e.g., Let's Encrypt), omit `caCertSecretRef` — the system trust store handles it automatically.

If you do not use Ansible, omit the `ansible` block entirely (it defaults to disabled). Note: v1alpha1 modeled this as a top-level `spec.ansible` block; v1alpha2 relocates it under `spec.workflowExecution.ansible` (F4 -- WorkflowExecution is Ansible's only consumer). The v1alpha1 API still accepts the old location and converts it losslessly.

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
    fleet-oauth2-creds.yaml          # if spec.fleet.enabled
    fleet-oauth2-write-creds.yaml    # if spec.fleet.enabled
    kubernaut-console-oidc.yaml      # if spec.console.enabled
  configmaps/
    signalprocessing-policy.yaml
    aianalysis-policy.yaml
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

## Console (optional)

The standalone web Console (A2A chat UI, `spec.console.enabled`) is fronted by an oauth2-proxy sidecar that needs its own OIDC client -- distinct from the Fleet client-credentials clients above and from AF's own OIDC config. Provision a **confidential, Authorization Code flow** client (`standardFlowEnabled`, not a service-account/client-credentials client) on your OIDC provider:

- Redirect URI: `https://<console-route-host>/oauth2/callback`, matching `spec.console.route.host`.
- Web origin: `https://<console-route-host>`.

Build the Secret from the client's credentials plus a locally-generated cookie secret (used by oauth2-proxy to encrypt the session cookie, not an OIDC value). `spec.console.auth.secretName` requires exactly these three keys:

```bash
oc create secret generic kubernaut-console-oidc -n kubernaut-system \
  --from-literal=client-id=<CLIENT_ID> \
  --from-literal=client-secret=<CLIENT_SECRET> \
  --from-literal=cookie-secret="$(openssl rand -base64 32 | head -c 32 | base64)"
```

```yaml
spec:
  console:
    enabled: true
    auth:
      secretName: kubernaut-console-oidc
```

If you don't need the standalone Console UI, omit the `console` block entirely (it defaults to disabled).

## Verification

Confirm all required prerequisite ConfigMaps exist before applying the Kubernaut CR:

```bash
oc get configmap -n kubernaut-system \
  signalprocessing-policy \
  aianalysis-policy
```

> **Note**: The operator validates that `spec.signalProcessing.policy.configMapName` and `spec.aiAnalysis.policy.configMapName` are set. If either is missing, reconciliation will fail with a clear validation error on the CR status.

If using BYO LLM runtime config or proactive mappings:

```bash
oc get configmap -n kubernaut-system \
  custom-llm-runtime \
  proactive-signal-mappings
```

## Additional RBAC for API Frontend

> **Hard requirement, not optional, for tool access:** whenever
> `spec.apiFrontend.enabled` is true (the default),
> `spec.apiFrontend.rbac.roleBindings` **must** list at least one OIDC
> group your users actually belong to, before you apply the CR. There is
> no permissive fallback if it's left unset — every user, including
> `cluster-admin`, gets every tool call rejected. This is fail-closed by
> design (least-privilege), with no dev-mode bypass; skip this section
> only if you set `spec.apiFrontend.enabled: false`.
>
> The Console UI itself is a *separate* gate and, unlike tool access, is
> permissive by default (kubernaut#2150) — see "Console access" (2) below
> before assuming an unset `roleBindings` blocks Console login too; it
> doesn't, only tool/chat actions inside it.

Required whenever `spec.apiFrontend.enabled: true` and any human or agent
actually calls AF's `/mcp`, `/a2a/invoke`, or Console endpoints (skip if AF
is enabled only for internal Gateway wiring with no external callers).

AF enforces two independent, OIDC-group-based SubjectAccessReview (SAR)
checks, both configured through `spec.apiFrontend.rbac`:

1. **Per-tool authorization** — one ClusterRole per persona (`sre`,
   `ai-orchestrator`, `cicd`, `observability`, `l3-audit`,
   `remediation-approver`), granting `use` on specific tool
   `resourceNames`. Enforced on every `/mcp`/`/a2a/invoke` call,
   unconditionally fail-closed — there is no toggle for this one.
2. **Console access** — a coarse-grained gate (`kubernaut.ai/console`,
   verb `use`) that the Console's `/a2a/access` pre-flight check must pass
   before it will render its UI, independent of (1) (kubernaut#1919,
   kubernaut-operator#289/#290). Unlike (1), this gate is **opt-in**:
   `spec.apiFrontend.rbac.consoleAccessAuthorizationCheckEnabled` defaults
   to `false` (kubernaut-operator#338, kubernaut#2148/#2150), which makes
   it authentication-only — any authenticated user passes, regardless of
   `consoleAccessGroups`/`roleBindings` — so a zero-config CR's Console
   still opens. Set it to `true` once you've populated
   `roleBindings`/`consoleAccessGroups` for production use, to actually
   enforce group-based Console access.

```yaml
spec:
  apiFrontend:
    rbac:
      sarCacheTTL: "30s"
      # Enforce gate (2) too, not just authentication-only (see above) --
      # set this once roleBindings/consoleAccessGroups are populated for
      # your real personas.
      consoleAccessAuthorizationCheckEnabled: true
      roleBindings:
        - role: sre
          groups: ["platform-engineering"]
        - role: observability
          groups: ["platform-engineering"]
        - role: remediation-approver
          groups: ["platform-engineering"]
```

Each entry maps a built-in persona (`role`) — or a pre-created custom
ClusterRole (`clusterRoleName`, mutually exclusive with `role`) — to one or
more OIDC groups. Those groups must appear in the `groups` claim of the JWT
AF receives; if your IdP doesn't emit that claim by default (Keycloak, for
example, requires an explicit "Group Membership" protocol mapper on the
client), every tool call will be denied regardless of this config (and, if
you've set `consoleAccessAuthorizationCheckEnabled: true`, Console access
too — with it left at the default `false`, Console access doesn't consult
groups at all and is unaffected by a missing claim).

**Console access is derived automatically — leave `consoleAccessGroups`
unset.** `spec.apiFrontend.rbac.consoleAccessGroups` only has any effect
once `consoleAccessAuthorizationCheckEnabled: true` is set (see above) --
while that stays `false` (the default), `consoleAccessGroups` is inert.
Once enabled, and by default (`consoleAccessGroups` unset), the operator
derives it as the deduplicated union of every group already listed in
`roleBindings`, so Console access tracks your existing tool-persona grants
with zero extra configuration. Only set it explicitly if Console access
should be scoped narrower/wider than tool access, or to `[]` to disable
Console access entirely while keeping tool access intact.

If users see "Access Denied" on the Console despite `roleBindings` looking
correct, first confirm whether you actually meant `consoleAccessAuthorizationCheckEnabled: true`
(gate (2)) or the tool-call SAR check (gate (1), always enforced) --
tool/chat *actions* inside the Console failing with "Access Denied" is
gate (1), and is the far more common case since it has no opt-out. See
[Troubleshooting](03-deploy.md#troubleshooting) or the detailed writeup:
[Kubernaut Console: troubleshooting "Access
Denied"](https://gist.github.com/jordigilh/5984f65c88da042f2207825a9e57df62).

## Additional ClusterRoles for ecosystem CRDs (KA, Gateway, EM)

By default, the operator creates a `kubernaut-agent-investigator` ClusterRole
(bound only to the Kubernaut Agent ServiceAccount) with **read-only** access
to:

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

Gateway and EffectivenessMonitor (EM) carry their own, much smaller built-in
owner-chain-resolution rules (PDB + `networking.k8s.io` only, as of #277) --
they don't get the ecosystem-specific grants above, since neither has KA's
investigation role and forcing every ecosystem's CRDs onto every cluster
regardless of whether it runs that ecosystem violated least-privilege
(SC-7/AC-6). If your environment includes ecosystem or custom CRDs that KA,
Gateway, or EM's owner-chain resolution needs to traverse -- e.g. Gateway or
EM correlating a signal's owner chain through a Knative Service or a
Strimzi-managed Kafka topic -- use the top-level `spec.additionalClusterRoles`
to layer on pre-existing ClusterRoles:

```yaml
spec:
  additionalClusterRoles:
    - strimzi-kafka-reader        # Kafka topics, brokers
    - knative-service-reader      # Knative Serving resources
    - my-app-crds-viewer          # Your custom application CRDs
```

The operator creates one ClusterRoleBinding per (entry, component) pair,
binding the named ClusterRole to KA's and EM's ServiceAccounts unconditionally,
and to Gateway's ServiceAccount while `spec.gateway.enabled=true` -- e.g. two
entries with Gateway enabled produce 6 ClusterRoleBindings, not 2. It does
**not** create or manage the ClusterRoles themselves — you must create them
separately.

> **Migrating from v1alpha1 / pre-#277 operators**: the field was previously
> `spec.kubernautAgent.additionalClusterRoleBindings` and only bound KA. The
> v1alpha1 API still accepts it (converted losslessly to the new top-level
> field on the v1alpha2 storage version), but the Gateway/EM binding is new
> behavior on upgrade -- review the ClusterRoles you list for whether it's
> appropriate for Gateway/EM to hold them too.

The `AdditionalRBACBound` status condition reports whether all referenced
ClusterRoles exist:
- `FullyBound` — all ClusterRoles found
- `PartiallyBound` — CRBs created but some ClusterRoles don't exist yet (check
  the condition message for details)

### Security considerations

Anyone with `update` permission on the `kubernauts.kubernaut.ai` CR can bind
**any** ClusterRole to the KA/Gateway/EM ServiceAccounts, including highly
privileged roles like `cluster-admin`. RBAC on the Kubernaut CR itself is the
access control boundary. Restrict who can edit the CR using standard
Kubernetes RBAC.

### Operational notes

- The `AdditionalRBACBound` condition updates every reconcile cycle (~60s). If
  you create a referenced ClusterRole after the CR, the condition will reflect it
  within one minute.
- Removing entries from the list, or disabling Gateway, automatically prunes
  the corresponding ClusterRoleBindings for the affected component(s).
- **Downgrade cleanup**: If downgrading to an operator version without this
  feature, remove orphaned CRBs manually:
  ```bash
  kubectl delete clusterrolebinding -l kubernaut.ai/additional-component-rbac=true
  ```

---

Previous: [Infrastructure Prerequisites](01-infrastructure.md) | Next: [Deploy Kubernaut](03-deploy.md)

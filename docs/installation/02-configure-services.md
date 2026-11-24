# Configure Services

This section covers the ConfigMaps and Secrets required by each Kubernaut service before deploying the CR.

> For the approved OTLP telemetry TLS contract, Secret ownership, rotation
> behavior, and rollout verification, see [OTLP telemetry TLS and operational
> rotation](../security/credentials-and-tls.md#otlp-telemetry-tls-and-operational-rotation).

## OTLP telemetry (optional)

Configure telemetry independently for Gateway, DataStorage, and Kubernaut
Agent. Network endpoints may use `host:port` or an explicit
`https://host:port` form; both render as the upstream `host:port` contract and
always use certificate-verifying TLS. `http://` and other URI schemes are
rejected:

```yaml
spec:
  gateway:
    config:
      telemetry:
        endpoint: https://otel-collector.observability.svc:4317
        tls:
          caCertSecretRef:
            name: otlp-ca
          certFile: /etc/telemetry/tls.crt
          keyFile: /etc/telemetry/tls.key
          tlsClientSecretRef: otlp-client
  dataStorage:
    telemetry:
      endpoint: otel-collector.observability.svc:4317
  kubernautAgent:
    telemetry:
      endpoint: stdout                 # local-only debugging mode
```

`otlp-ca` must contain `ca.crt` (or the key named by `key`), and `otlp-client`
must contain the matching `tls.crt` and `tls.key`. Secret-backed material is
mounted read-only. Omit `tls` for a public collector using system trust, or
use `logSink: true` with an empty endpoint for log-only spans. Existing
`telemetry.tls.enabled` fields must be removed; there is no plaintext mode.

## LLM Profiles

LLM configuration lives in a top-level, named-profile map at `spec.llmProfiles`, not nested under any one component. Each logical Agent or service then references a profile by name:

- `spec.kubernautAgent.investigation.llmProfileRef` -- KA's root-cause investigation LLM.
- `spec.kubernautAgent.workflowDiscovery.llmProfileRef` -- KA's independent workflow-discovery LLM.
- `spec.apiFrontend.llmProfileRef` -- AF's own LLM connection. When empty, AF infers the sole profile; with multiple profiles it must be explicit.
- `spec.apiFrontend.severityTriage.llmProfileRef` (optional) -- an independent LLM for severity-triage's fallback tiers, distinct from AF's main connection. When empty, triage inherits AF's resolved profile.

This lets you use a stronger model for RCA and a cheaper/faster model for workflow discovery, all from one place:

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
      # tlsCaFile: /etc/kubernaut-agent/llm-tls-client/ca.crt
      # tlsCaSecretRef:
      #   name: primary-llm-ca       # key defaults to ca.crt
      # Optional LLM mTLS client material:
      # tlsCertFile: /etc/kubernaut-agent/llm-tls-client/tls.crt
      # tlsKeyFile: /etc/kubernaut-agent/llm-tls-client/tls.key
      # tlsClientSecretRef: primary-llm-client  # tls.crt + tls.key
    lightweight:
      provider: openai
      model: gpt-4o-mini
      credentialsSecretName: llm-credentials   # see constraint below

  kubernautAgent:
    investigation:
      llmProfileRef: primary
      maxTurns: 40
    workflowDiscovery:
      llmProfileRef: lightweight
```

Cross-credential Agent profile references are supported. The operator mounts each distinct credentials Secret read-only for its owning Agent invocation. Severity-triage retains its independent profile and fallback behavior.

When `tlsCaSecretRef` is set, the selected CA key is validated as PEM before
workloads are created and mounted read-only. `tlsClientSecretRef` is similarly
validated for `tls.crt` and `tls.key`. Workflow-discovery and alignment profiles
use their own deterministic TLS directories; material is reused only when the
effective Secret/path identity is intentionally shared. Path-only `tlsCaFile`
values without a Secret reference remain administrator-managed external files
and are not inspected by the operator.

The operator auto-generates each component's LLM runtime config from its resolved profile. If your environment includes custom CRDs or advanced routing needs beyond `provider`/`model`, see the BYO runtime ConfigMap section below.

### Independent LLM for AI Frontend severity-triage

By default, severity-triage's LLM-based fallback tiers inherit whichever profile API Frontend itself resolves to. To give triage its own profile (e.g. a cheaper model, since triage calls are higher-volume) or to disable LLM-based triage entirely (falling back to upstream's rule-based-only triager):

```yaml
spec:
  llmProfiles:
    triage:
      provider: openai
      model: gpt-4o-mini
      credentialsSecretName: triage-llm-credentials   # may be distinct; the operator mounts it read-only

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
        effort: minimal            # cheaper/faster workflow-discovery tier

  kubernautAgent:
    investigation:
      llmProfileRef: primary
      maxTurns: 40
    workflowDiscovery:
      llmProfileRef: lightweight   # gets its own reasoning policy, independent of primary's
```

`effort` is a unified, provider-agnostic depth knob: the same value means the same thing regardless of which provider a profile points at, but each provider's client maps it into its own wire dialect (e.g. Anthropic's thinking-level tiers, OpenAI/Azure o-series and gpt-5's `reasoning_effort`, DeepSeek's own two-tier dialect). Providers with no effort-dial concept simply ignore it.

> **Constraint**: for Anthropic-family providers (`anthropic`, and Claude models served via `vertex_ai`), `effort: "none"` combined with `enabled: true` is rejected -- Anthropic has no "thinking enabled, zero effort" wire state. Use `enabled: false` to fully disable reasoning, or `effort: "minimal"` for Anthropic's lowest real tier.

Because `reasoning` is a per-profile field, `severityTriage.llmProfileRef` and `workflowDiscovery.llmProfileRef` each get their own independent reasoning policy -- pointing a logical Agent or triage service at a different profile changes its reasoning budget/effort along with everything else on that profile, without touching the other consumer.

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
	client-id: "<YOUR_CLIENT_ID>"
	client-secret: "<YOUR_CLIENT_SECRET>"
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

If you do not use Ansible, omit the `ansible` block entirely (it defaults to disabled). The clean-break v1alpha2 API models this under `spec.workflowExecution.ansible`; manifests using the retired top-level `spec.ansible` field must be transformed before recreation. See `docs/upgrade-v1alpha1-to-v1alpha2.md`.

## Failed WorkflowExecution diagnostics retention (optional)

WorkflowExecution removes failed Job, Tekton PipelineRun, and AWX execution
resources by default. To retain them for bounded post-failure diagnosis and
must-gather collection, opt in explicitly:

```yaml
spec:
  workflowExecution:
    retainFailedExecutions: true
    failedExecutionRetentionSeconds: 600
```

The retention period defaults to 600 seconds and is limited to seven days.
Retention remains disabled when the boolean is omitted or false. This is an
operator-rendered WorkflowExecution setting; it does not change the separate
operator log or audit-storage responsibilities described in
[`docs/security/auditing.md`](../security/auditing.md).

## Gateway Configuration (optional)

The Gateway accepts signals from AlertManager and routes them to Signal Processing. Tune its behavior in the CR:

```yaml
spec:
  gateway:
    logging:
      level: info               # debug, info, warn, error
    config:
      trustedProxyCIDRs:        # CIDRs trusted for X-Forwarded-For headers
        - "10.128.0.0/14"
      deduplicationCooldown: "5m"   # dedup window for identical signals
      k8sRequestTimeout: "15s"     # timeout for K8s API calls during fingerprinting
```

All gateway config fields are optional; the operator uses sensible defaults when omitted.

The operator creates the internal Gateway Service only. Configure any external
Route, Ingress, Gateway API, mesh, hostname, edge TLS, and edge authorization
resource separately as an administrator-owned object.

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

## API Frontend OIDC

API Frontend and the standalone Console use the same effective OIDC issuer.
The operator is provider-neutral and has no built-in or generic portal URL.
When `jwtProviders` is empty, set a complete URL for the IdP used by this
installation:

```yaml
spec:
  apiFrontend:
    auth:
      issuerURL: "https://idp.example.com/realms/kubernaut"
      audience: kubernaut-apifrontend
```

Replace the example with the actual issuer URL for the installation. Omitting
it is a validation error, and the operator never constructs an issuer from a
short realm name or selects a portal implicitly. Explicit issuer URLs must use HTTPS;
HTTP is accepted only when `allowInsecureIssuers: true` is deliberately set
for development or testing. `jwksURL` may be omitted for a Keycloak-compatible
issuer, in which case the standard realm JWKS path is derived. Set it
explicitly for other providers.

When `jwtProviders` is non-empty, API Frontend validates all listed providers
and renders the complete list. Console oauth2-proxy uses the first validated
provider's issuer. Keep that provider suitable for browser login and configure
its client with the Console redirect URI and the API Frontend audience as
needed. Changing realms invalidates existing tokens and Console cookies;
update the IdP clients, audiences, redirect URIs, JWKS reachability, and CA
trust together.

If external MCP/A2A access is not needed, disable the API Frontend explicitly:

```yaml
spec:
  apiFrontend:
    enabled: false
```

### API Frontend scaling and availability

API Frontend is intentionally a **single-replica** workload. The operator sets
`replicas: 1` and uses a Kubernetes `Recreate` rollout, so the old pod is
terminated before its replacement starts. An upgrade or AF template change can
therefore cause a short AF outage; schedule planned changes during a suitable
maintenance window and expect active AF connections or sessions to terminate.

The operator does not create an API Frontend HPA. DataStorage is the only
operator-managed HPA and remains bounded at `minReplicas: 1` and
`maxReplicas: 5`. Do not add a second autoscaler if the single-replica AF
contract is required.

If a pre-existing or manually created HPA named `apifrontend-hpa` is owned by
another controller, the operator preserves it and emits an `OwnershipConflict`
warning rather than adopting or deleting it. Such an HPA can still scale AF
above one replica; resolve that external ownership conflict explicitly before
relying on the operator-managed single-replica guarantee.

## Console (optional)

The standalone web Console (A2A chat UI, `spec.console.enabled`) is fronted by an oauth2-proxy sidecar that needs its own OIDC client -- distinct from the Fleet client-credentials clients above and from AF's own OIDC config. Provision a **confidential, Authorization Code flow** client (`standardFlowEnabled`, not a service-account/client-credentials client) on your OIDC provider:

- Redirect URI: the explicit HTTPS callback URL set in
  `spec.console.auth.redirectURL` (for example,
  `https://console.example.com/oauth2/callback`).
- Web origin: the administrator-owned external Console URL.

Build the Secret from the client's credentials plus a locally-generated cookie secret (used by oauth2-proxy to encrypt the session cookie, not an OIDC value). `spec.console.auth.secretName` requires exactly these three keys. The cookie secret's raw value must be exactly 16, 24, or 32 bytes (oauth2-proxy uses it as an AES key) -- `head -c 32` truncates the base64 text to 32 *characters*, which is already the right raw length, so do not additionally base64-encode the result or oauth2-proxy will reject it (`cookie_secret must be 16, 24, or 32 bytes ... but is 44 bytes`):

The operator's alpha configuration requires oauth2-proxy **v7.15.5 or newer**. If `spec.image.overrides["oauth2-proxy"]` is set, keep it at v7.15.5 or newer; the operator does not validate override image versions, and older images may reject the `additionalClaims` configuration.

```bash
oc create secret generic kubernaut-console-oidc -n kubernaut-system \
  --from-literal=client-id=<CLIENT_ID> \
  --from-literal=client-secret=<CLIENT_SECRET> \
  --from-literal=cookie-secret="$(openssl rand -base64 32 | head -c 32)"
```

```yaml
spec:
  console:
    enabled: true
    auth:
      secretName: kubernaut-console-oidc
      redirectURL: https://console.example.com/oauth2/callback
      # Optional provider-discovered OIDC end_session_endpoint. Keep the
      # literal {id_token} placeholder; oauth2-proxy substitutes it server-side.
      providerLogoutURL: "https://idp.example.com/realms/kubernaut/protocol/openid-connect/logout?id_token_hint={id_token}"
```

Raw thinking is hidden by default for new Console users. The operator serves
this policy through the Console's same-origin runtime configuration, so the
released Console image cannot silently restore the old header toggle. Set
`spec.console.enableRawThinking: true` only when an administrator explicitly
wants the raw LLM reasoning view for diagnostic purposes; leaving it unset or
setting it to `false` keeps the view disabled.

This is a deployment-wide setting: when enabled, raw thinking is available to
every authenticated Console user. The operator does not provide per-user
authorization for this diagnostic view, so enable it only when that audience
is appropriate for the deployment.
`providerLogoutURL` is optional. When configured, it must be an absolute HTTPS
endpoint whose authority exactly matches the effective Console OIDC issuer. The
operator rejects HTTP (including localhost), malformed placeholders, fragments,
and client-controlled return-target parameters. It does not send the ID token
to the Console SPA. The Console oauth2-proxy pod builds a combined system and
configured-provider CA bundle so public and self-signed provider certificates
are verified; certificate verification is never disabled.

If you don't need the standalone Console UI, omit the `console` block entirely (it defaults to disabled).

### Console read API prerequisites (#638)

The operator's Console NGINX configuration routes the authenticated read API
(`GET /api/v1/console/policies/scope` and
`GET /api/v1/console/inventory/managed-resources[/{resourceID}]`) to API
Frontend through the same OAuth2 Proxy session used for A2A/MCP. Version
prerequisites:

- **API Frontend**: an image carrying the authenticated console read API
  (kubernaut PR #2604, decision DD-AF-018). On older AF images the route
  returns AF's own 404 instead of the Console SPA -- a safe failure that
  discloses no credentials -- but the endpoints are unusable.
- **Console**: an image carrying the read client from kubernaut-console
  issue #164. Older Console images simply do not issue these requests.

The routing itself (OAuth2 Proxy session forwarding, upstream TLS
verification, rate limiting) needs no configuration and is applied by
ordinary operator reconciliation on upgrade. Browser-level qualification of
the full read path additionally requires the versions above; the operator's
Kind qualification lane (`E2E-CN-638-HTTP-001`) verifies routing and access
control against the deployed components.

## Verification

Confirm all required prerequisite ConfigMaps exist before applying the Kubernaut CR:

```bash
oc get configmap -n kubernaut-system \
  signalprocessing-policy \
  aianalysis-policy
```

> **Note**: The operator validates that `spec.signalProcessing.policy.configMapName` and `spec.aiAnalysis.policy.configMapName` are set. If either is missing, reconciliation will fail with a clear validation error on the CR status.

If using BYO LLM runtime config:

```bash
oc get configmap -n kubernaut-system \
  custom-llm-runtime
```

## Additional RBAC for API Frontend

> **Roles-only model (v1.6, [#682](https://github.com/jordigilh/kubernaut-operator/issues/682)/[#684](https://github.com/jordigilh/kubernaut-operator/issues/684)):**
> the operator renders *roles* and never user-facing ClusterRoleBindings.
> Who is bound to which role is an operational/SRE decision owned by the
> platform team, not a deployment decision — anyone delegated to manage the
> Kubernaut CR must not be implicitly delegated user-access policy. The
> former `spec.apiFrontend.rbac.roleBindings` and `consoleAccessGroups`
> fields and the six built-in persona ClusterRoles are gone; compose your
> own roles from your SOPs and bind them with your own RBAC objects
> (GitOps, OpenShift RBAC, or `oc`).

Required whenever `spec.apiFrontend.enabled: true` and any human or agent
actually calls AF's `/mcp`, `/a2a/invoke`, or Console endpoints (skip if AF
is enabled only for internal Gateway wiring with no external callers).

AF enforces two independent, OIDC-group-based SubjectAccessReview (SAR)
checks. Both authorize through Kubernetes RBAC that **you** own:

1. **Per-tool authorization** — every tool call must pass a SAR for `use`
   on the tool's name in `kubernaut.ai/tools`. Enforced unconditionally,
   fail-closed — there is no toggle and no permissive fallback. Compose
   one ClusterRole per use case with the exact tool names that use case
   needs (see the example bundles below), and bind your OIDC groups to it
   with your own ClusterRoleBinding.
2. **Console access** — a coarse-grained gate (`kubernaut.ai/console`,
   verb `use`) that the Console's `/a2a/access` pre-flight check must pass
   before it will render its UI, independent of (1) (kubernaut#1919,
   kubernaut-operator#289/#290). Unlike (1), this gate is **opt-in**:
   `spec.apiFrontend.rbac.consoleAccessAuthorizationCheckEnabled` defaults
   to `false` (kubernaut-operator#338, kubernaut#2148/#2150), which makes
   it authentication-only — any authenticated user passes. Set it to
   `true` once you've bound your groups to the rendered
   `console-access` ClusterRole, to actually enforce group-based Console
   access.

```yaml
spec:
  apiFrontend:
    rbac:
      sarCacheTTL: "30s"
      # Enforce gate (2) too, not just authentication-only (see above) --
      # set this once your groups are bound to <namespace>-console-access.
      consoleAccessAuthorizationCheckEnabled: true
```

The operator renders three user-facing ClusterRoles whenever AF is enabled
(all unbound; all named with your CR's namespace prefix):

| Role | Grants | Notes |
|---|---|---|
| `<namespace>-console-access` | `kubernaut.ai/console` `use` | the login gate for check (2) |
| `<namespace>-console-reader` | `use` on the three console read capabilities | see below |
| `<namespace>-console-inventory-reader` | `get`/`list` on AF's pinned 11-kind inventory allowlist | see below |

Bind them with your own RBAC objects, never through the Kubernaut CR. For
a Kubernaut CR in `kubernaut-system`:

```bash
oc create clusterrolebinding kubernaut-console-access \
  --clusterrole=kubernaut-system-console-access \
  --group=platform-engineering
```

Your users' groups must appear in the `groups` claim of the JWT AF
receives; if your IdP doesn't emit that claim by default (Keycloak, for
example, requires an explicit "Group Membership" protocol mapper on the
client), every tool call will be denied regardless of this config (and, if
you've set `consoleAccessAuthorizationCheckEnabled: true`, Console access
too — with it left at the default `false`, Console access doesn't consult
groups at all and is unaffected by a missing claim).

### Composing your own tool roles

A tool role is any ClusterRole granting `use` on `kubernaut.ai/tools`
with the tool names you intend as `resourceNames` — nothing more. Start
from the audited use-case bundles below (they carry the exact tool sets
[DD-636](../design/DD-636-console-direct-action-rbac.md) audited for the
former built-in personas), adjust them to your SOPs, and bind them to
your OIDC groups with your own ClusterRoleBindings:

<!-- bundle-matrix-start -->
```yaml
# Example use-case bundles (administrator-owned; rendered verbatim from the
# DD-636 audit). Alert-capable bundles carry BOTH tool names: list_alerts
# (A2A) and kubernaut_list_alerts (MCP) -- AF authorizes the exact name
# each transport uses (#664/#683). Approval reads/decisions stay with the
# human/approver bundles only (DD-278, #665).
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubernaut-sre
rules:
  - apiGroups: ["kubernaut.ai"]
    resources: ["tools"]
    verbs: ["use"]
    resourceNames:
      - kubernaut_list_remediations
      - kubernaut_get_remediation
      - kubernaut_approve
      - kubernaut_cancel_remediation
      - kubernaut_watch
      - kubernaut_await_session
      - kubernaut_investigate
      - kubernaut_discover_workflows
      - kubernaut_select_workflow
      - kubernaut_present_decision
      - kubernaut_list_workflows
      - kubernaut_get_remediation_history
      - kubernaut_get_effectiveness
      - kubernaut_get_audit_trail
      - kubernaut_message
      - kubernaut_complete
      - kubernaut_cancel
      - kubernaut_status
      - kubernaut_reconnect
      - kubectl_get
      - kubectl_list
      - kubectl_list_events
      - kubernaut_check_existing_remediation
      - kubernaut_remediate
      - list_alerts
      - kubernaut_list_alerts
      - get_alert_details
      - kubernaut_investigate_alert
      - kubernaut_get_approval_request
      - kubernaut_complete_no_action
      - kubernaut_list_approval_requests
      - list_clusters
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubernaut-ai-orchestrator
rules:
  - apiGroups: ["kubernaut.ai"]
    resources: ["tools"]
    verbs: ["use"]
    resourceNames:
      - kubernaut_list_remediations
      - kubernaut_get_remediation
      - kubernaut_watch
      - kubernaut_await_session
      - kubernaut_investigate
      - kubernaut_discover_workflows
      - kubernaut_select_workflow
      - kubernaut_present_decision
      - kubernaut_message
      - kubernaut_complete
      - kubernaut_cancel
      - kubernaut_status
      - kubernaut_reconnect
      - kubectl_get
      - kubectl_list
      - kubectl_list_events
      - kubernaut_check_existing_remediation
      - kubernaut_remediate
      - list_alerts
      - kubernaut_list_alerts
      - get_alert_details
      - kubernaut_investigate_alert
      - list_clusters
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubernaut-cicd
rules:
  - apiGroups: ["kubernaut.ai"]
    resources: ["tools"]
    verbs: ["use"]
    resourceNames:
      - kubernaut_list_remediations
      - kubernaut_get_remediation
      - kubernaut_watch
      - kubernaut_await_session
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubernaut-observability
rules:
  - apiGroups: ["kubernaut.ai"]
    resources: ["tools"]
    verbs: ["use"]
    resourceNames:
      - kubernaut_list_remediations
      - kubernaut_get_remediation
      - kubernaut_watch
      - kubernaut_await_session
      - kubernaut_get_effectiveness
      - kubernaut_list_workflows
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubernaut-l3-audit
rules:
  - apiGroups: ["kubernaut.ai"]
    resources: ["tools"]
    verbs: ["use"]
    resourceNames:
      - kubernaut_list_remediations
      - kubernaut_get_remediation
      - kubernaut_list_workflows
      - kubernaut_get_remediation_history
      - kubernaut_get_effectiveness
      - kubernaut_get_audit_trail
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubernaut-remediation-approver
rules:
  - apiGroups: ["kubernaut.ai"]
    resources: ["tools"]
    verbs: ["use"]
    resourceNames:
      - kubernaut_approve
      - kubernaut_list_approval_requests
      - kubernaut_get_approval_request
      - kubernaut_list_remediations
      - kubernaut_get_remediation
      - kubernaut_watch
      - kubernaut_await_session
```
<!-- bundle-matrix-end -->

Bind a bundle to your groups exactly as with the console roles above:

```bash
oc apply -f kubernaut-sre.yaml
oc create clusterrolebinding kubernaut-sre \
  --clusterrole=kubernaut-sre \
  --group=platform-engineering
```

The bundles are a starting point, not a contract: SREs are expected to
define their own roles based on their SOPs. The audited separation of
duties is worth keeping, though — approval reads and decisions
(`kubernaut_approve`, `kubernaut_list_approval_requests`,
`kubernaut_get_approval_request`, `kubernaut_complete_no_action`) belong
in human/approver bundles only, never in the automated
`ai-orchestrator` bundle ([DD-278](../design/DD-278-sre-approval-visibility.md),
[#665](https://github.com/jordigilh/kubernaut-operator/issues/665)).

**Console policy/inventory reads need a separate role — permanently.** The
operator renders `console-reader` (cluster-scoped), granting only the
three AF console read capabilities (`kubernaut_read_policy_scope`,
`kubernaut_list_managed_resources`, `kubernaut_get_managed_resource`). Per
[DD-662](../design/DD-662-console-read-capability-model.md) this is the
permanent model, not a placeholder: **no documented bundle implies it and
login never grants console reads**. Under the roles-only model, the only
supported bind path is your own ClusterRoleBinding:

```bash
oc create clusterrolebinding kubernaut-console-readers \
  --clusterrole=kubernaut-system-console-reader \
  --group=console-readers
```

Until a user holds the capability, console reads return AF's `forbidden`
envelope and the Console renders its access-denied read state — the
intended deny-by-default behavior. The boundary is the
capability SAR, not this role's bind state: a user granted `use` on the
same three `kubernaut.ai/tools` resourceNames through any other
ClusterRole is equally authorized. The Console nginx `/api/v1/console/`
proxy is transport only: it changes nothing about who can read.

Local inventory reads additionally check the user's own Kubernetes
`get`/`list` permissions on the exact projected resources. The projected
resource *set* is fixed by AF's Gateway-aligned allowlist (pods, services,
nodes, persistent volumes, namespaces, deployments, statefulsets, daemonsets,
replicasets, jobs, cronjobs — secrets and configmaps are excluded by design),
so the operator renders those user-side grants as a dedicated
`console-inventory-reader` ClusterRole
(`<namespace>-console-inventory-reader`), granting only `get` and `list` on
exactly those kinds. Like `console-reader` it is **not bound by default**: the
operator renders roles, and your platform/SRE team owns every binding.

Three scoping notes:

- A bound user can also use the grant outside the Console (for example
  `kubectl get pods`) — the Console projection authorizes with your own
  Kubernetes permissions, so any grant satisfying it is a real grant.
- The default inventory page is unfiltered, so it issues a cluster-scoped
  list SAR; users need a cluster-level binding to see it.
- A namespaced RoleBinding satisfies only namespace-filtered reads, and only
  for the eight namespaced kinds in the namespaces you bind. `nodes`,
  `persistentvolumes` and `namespaces` are cluster-scoped and always need a
  cluster-level binding. Use namespace scoping deliberately: with it, any
  unfiltered Console read returns `forbidden`.

For a Kubernaut CR in `kubernaut-system`, bind the rendered role with your
own RBAC objects (not through the Kubernaut CR):

```bash
oc create clusterrolebinding console-inventory-readers \
  --clusterrole=kubernaut-system-console-inventory-reader \
  --group=console-readers
```

If users see "Access Denied" on the Console, first confirm whether you
actually meant `consoleAccessAuthorizationCheckEnabled: true` (gate (2))
or the tool-call SAR check (gate (1), always enforced) -- tool/chat
*actions* inside the Console failing with "Access Denied" is gate (1),
and is the far more common case since it has no opt-out. See
[Troubleshooting](03-deploy.md#troubleshooting) or the detailed writeup:
[Kubernaut Console: troubleshooting "Access
Denied"](https://gist.github.com/jordigilh/5984f65c88da042f2207825a9e57df62).

**Upgrading from the pre-v1.6 persona/binding model.** v1.6 is pre-GA and
expects clean deployments ([DD-682](../design/DD-682-roles-only-rbac.md));
the operator does not migrate the retired RBAC. If you deployed the
former `roleBindings`/`consoleAccessGroups` surface, remove its stale
user-facing RBAC yourself — those objects keep granting authorization
the operator no longer tracks:

```bash
NS=kubernaut-system  # Set to the namespace of your Kubernaut CR.

# Delete a named object only when it carries the operator's managed-by
# label — the retired pre-v1.6 objects do; an administrator-owned object
# with the same name is preserved, and a missing object is skipped. If a
# same-named object exists but is not operator-owned, review it manually.
delete_if_operator_owned() {
  local kind=$1 name=$2
  if oc get "$kind" "$name" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}' 2>/dev/null \
    | grep -q '^kubernaut-operator$'; then
    oc delete "$kind" "$name"
  else
    echo "skipping $kind/$name: absent or not operator-owned"
  fi
}

# The retired console-access ClusterRoleBinding.
delete_if_operator_owned clusterrolebinding "${NS}-console-access-binding"

# The six retired persona ClusterRoles and their bindings — the only
# persona names a pre-v1.6 operator rendered. (The ClusterRoles are also
# swept automatically when you delete the Kubernaut instance; remove them
# manually only if you keep the instance.)
for persona in sre ai-orchestrator cicd observability l3-audit remediation-approver; do
  delete_if_operator_owned clusterrole "${NS}-tool-${persona}"
  delete_if_operator_owned clusterrolebinding "${NS}-tool-${persona}-binding"
done

# Bindings the pre-v1.6 custom-clusterRoleName path rendered
# (<ns>-tool-custom-<name>-binding): the <name> half was
# administrator-chosen, so no name pattern or label can prove a matching
# object is the retired operator render rather than an
# administrator-owned lookalike (a copied backup keeps the managed-by
# label too). These are therefore NOT deleted automatically: review each
# candidate and delete it yourself if it is the retired render.
for crb in $(oc get clusterrolebinding -o name | grep "/${NS}-tool-custom-.*-binding$" || true); do
  echo "review before deleting (retired pre-v1.6 render vs administrator-owned): $crb"
done
```

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

> **Migrating from pre-#277 operators**: the field was previously
> `spec.kubernautAgent.additionalClusterRoleBindings` and only bound KA. The
> v1alpha2 uses the top-level `spec.additionalClusterRoles`; transform the old
> field during the export/transform/recreate migration. Gateway/EM binding is
> new behavior -- review the ClusterRoles you list for whether it is
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

### Monitoring client TLS trust

Prometheus, AlertManager, EffectivenessMonitor, Kubernaut Agent, and API
Frontend severity-triage share the optional `spec.monitoring.tls` trust
configuration. Omit it for the container/system trust store, or provide an
administrator-owned Secret:

```yaml
spec:
  monitoring:
    tls:
      caCertSecretRef:
        name: monitoring-ca
        key: ca.crt
```

The operator verifies the referenced Secret and CA PEM before deployment and
mounts it read-only at `/etc/monitoring-tls/ca.crt`. This is client trust only;
serving TLS remains under `spec.tls`. Configured monitoring endpoints must use
`https://`; the operator rejects plaintext monitoring URLs. The removed
pre-publication fields `monitoring.tls.source`,
`monitoring.tls.caCertConfigMapRef`, `monitoring.prometheus.tlsCaFile`, and
`monitoring.alertManager.tlsCaFile` must not be used.

---

Previous: [Infrastructure Prerequisites](01-infrastructure.md) | Next: [Deploy Kubernaut](03-deploy.md)

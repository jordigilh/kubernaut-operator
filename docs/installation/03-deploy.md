# Deploy Kubernaut

## Install the Operator

**From OperatorHub (after publication):**

Navigate to **Operators > OperatorHub** in the OCP console, search for **Kubernaut**, and click **Install**.

**From a custom CatalogSource (pre-publication / testing):**

```bash
oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: kubernaut-operator-catalog
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: quay.io/kubernaut-ai/kubernaut-operator-catalog:v1.4.0
  displayName: Kubernaut Operator
  publisher: Kubernaut AI
EOF
```

Then install via **Operators > OperatorHub > Kubernaut** in the console, or create a Subscription:

```bash
oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: kubernaut-operator
  namespace: openshift-operators
spec:
  channel: alpha
  name: kubernaut-operator
  source: kubernaut-operator-catalog
  sourceNamespace: openshift-marketplace
EOF
```

Wait for the operator pod to be ready:

```bash
oc get csv -n openshift-operators | grep kubernaut
```

## Create the Kubernaut CR

Apply the CR in the namespace where your secrets and ConfigMaps were created. Adjust `host`, `secretName`, and LLM settings to match your environment:

```bash
oc apply -f - <<EOF
apiVersion: kubernaut.ai/v1alpha2
kind: Kubernaut
metadata:
  name: kubernaut
  namespace: kubernaut-system
spec:
  # --- Data stores (from Step 1: Infrastructure) ---
  postgresql:
    secretName: postgresql-secret
    host: postgresql.kubernaut-system.svc.cluster.local
    port: 5432

  valkey:
    secretName: valkey-secret
    host: valkey.kubernaut-system.svc.cluster.local
    port: 6379

  # --- LLM profiles (from Step 2: Configure Services) ---
  llmProfiles:
    primary:
      provider: openai                     # or: anthropic, vertex_ai
      model: gpt-4o                        # or: claude-sonnet-4-6, etc.
      credentialsSecretName: llm-credentials
      # tlsCaFile: /path/to/ca.pem         # custom CA for LLM endpoint TLS
      # oauth2:                            # OAuth2-based LLM authentication
      #   enabled: false
      #   tokenURL: ""
      #   scopes: []
      #   credentialsSecretRef: oauth2-credentials

  kubernautAgent:
    llmProfileRef: primary
    # runtimeConfigMapName: custom-llm-runtime  # uncomment for BYO LLM config
    # logging:
    #   level: info                        # debug, info, warn, error
    # alignmentCheck:                      # shadow agent alignment verification
    #   enabled: false
    #   timeout: "10s"
    #   maxStepTokens: 500
    #   llmProfileRef: primary             # optional: use a different profile for alignment (defaults to the same profile as llmProfileRef above)
    # safety:                              # agent safety controls
    #   sanitization:
    #     injectionPatternsEnabled: true    # detect prompt injection patterns
    #     credentialScrubEnabled: true      # scrub credentials from tool output
    #   anomaly:
    #     maxToolCallsPerTool: 10
    #     maxTotalToolCalls: 40
    #     maxRepeatedFailures: 3
    # summarizer:                          # tool output summarization
    #   threshold: 8000                    # token count to trigger summarization
    #   maxToolOutputSize: 100000          # max tool output size in bytes
    # fleet:
    #   oauth2CredentialsSecretRef: ka-oauth2-creds   # overrides fleet.oauth2.credentialsSecretRef for KubernautAgent only -- used when fleet.enabled: true, for its list_clusters/list_tools_for_cluster MCP tools (ADR-068 decision #11)

  # --- NetworkPolicies (always created, F3 -- no enabled toggle; tune only) ---
  # networkPolicies:
  #   apiServerCIDR: "10.0.0.1/32"         # override when default API server CIDR detection doesn't resolve correctly

  # --- Policies (from Step 2: Configure Services) ---
  aiAnalysis:
    policy:
      configMapName: aianalysis-policy

  signalProcessing:
    policy:
      configMapName: signalprocessing-policy
    # proactiveSignalMappings:
    #   configMapName: proactive-signal-mappings  # uncomment to enable proactive mode
    # fleet:
    #   oauth2CredentialsSecretRef: sp-oauth2-creds   # overrides fleet.oauth2.credentialsSecretRef for SignalProcessing only

  # --- Notifications (from Step 2: Configure Services) ---
  notification:
    slack:
      secretName: slack-webhook            # omit to disable Slack delivery
      channel: "#kubernaut-alerts"

  # --- API Frontend (optional) ---
  # The API Frontend provides external MCP/A2A access to Kubernaut Agent.
  # It requires an OIDC issuer (e.g. RHBK/Keycloak) when TLS is enabled.
  # Set enabled: false to skip AF deployment entirely.
  apiFrontend:
    # enabled: false                        # uncomment to disable AF
    auth:
      issuerURL: "https://keycloak.apps.example.com/realms/kubernaut"
      audience: "kubernaut-apifrontend"     # must match the OIDC client
    # REQUIRED whenever AF is enabled (default) -- there is no working
    # fallback if rbac/roleBindings is left unset: every user, including
    # cluster-admin, sees "Access Denied" on the Console and every tool
    # call is rejected. Replace the group name with a real OIDC group your
    # users actually belong to (see docs/installation/02-configure-services.md
    # #additional-rbac-for-api-frontend).
    rbac:
      roleBindings:
        - role: sre
          groups: ["platform-engineering"]
    # fleet:
    #   oauth2CredentialsSecretRef: af-oauth2-creds   # overrides fleet.oauth2.credentialsSecretRef for APIFrontend only -- used when fleet.enabled: true, for its list_clusters MCP tool

  # --- Gateway tuning (optional) ---
  gateway:
    route:
      enabled: true
    # logging:
    #   level: info
    # config:
    #   trustedProxyCIDRs:              # CIDRs trusted for X-Forwarded-For
    #     - "10.128.0.0/14"
    #   deduplicationCooldown: "5m"     # dedup window for identical signals
    #   k8sRequestTimeout: "15s"        # timeout for K8s API calls
    # fleet:
    #   oauth2CredentialsSecretRef: gateway-oauth2-creds   # overrides fleet.oauth2.credentialsSecretRef for Gateway only

  # --- Fleet federation (optional, ADR-068) ---
  # Points Gateway/RemediationOrchestrator (scope-checking),
  # SignalProcessing/APIFrontend/EffectivenessMonitor (cluster classification
  # + multi-cluster reads, #224), and KubernautAgent (GatewayDiscoverer tool
  # discovery -- list_clusters/list_tools_for_cluster, #204) at a shared
  # fleet backend across a fleet of clusters. All fields are inert until
  # enabled: true is set — safe to pre-stage ahead of enabling.
  #
  # mcpGatewayEndpoint/mcpGatewayType are REQUIRED alongside backend/endpoint
  # when enabled: Gateway and RemediationOrchestrator fail closed at startup
  # without them (upstream Fleet.ValidateFullFederation). SignalProcessing/
  # APIFrontend/EffectivenessMonitor/KubernautAgent only need
  # mcpGatewayEndpoint+mcpGatewayType — they never call the Backend/Endpoint
  # scope-check adapter, so backend/endpoint don't apply to them and are
  # omitted from their rendered config.
  # fleet:
  #   enabled: false
  #   backend: fleetmetadatacache          # or: acm (Red Hat ACM Search GraphQL)
  #   endpoint: "https://fleet-metadata-cache.fleet-system.svc.cluster.local:8443"
  #   caSecretName: fmc-ca-bundle          # optional; Secret key: ca.crt
  #   tokenSecretName: acm-search-token    # optional; Secret key: token (typically required for backend: acm)
  #   mcpGatewayEndpoint: "https://mcp-gateway.example.com/sse"
  #   mcpGatewayType: eaigw                # or: kuadrant
  #   mcpGatewayNamespace: managed-clusters   # optional; scopes the MCP Gateway CRD watch (and its RBAC) to one namespace instead of cluster-wide, shared by every fleet-aware component (DD-362 -- no per-component override)
  #   oauth2:
  #     enabled: true
  #     tokenURL: "https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/token"
  #     credentialsSecretRef: fleet-oauth2-creds   # optional; Secret keys: client-id, client-secret
  #     scopes: ["openid", "groups"]
  #
  # A federated IdP (e.g. Keycloak) can issue distinct per-service OAuth2
  # client registrations against the same tokenURL above — set
  # gateway.fleet.oauth2CredentialsSecretRef,
  # remediationOrchestrator.fleet.oauth2CredentialsSecretRef,
  # signalProcessing.fleet.oauth2CredentialsSecretRef,
  # apiFrontend.fleet.oauth2CredentialsSecretRef,
  # effectivenessMonitor.fleet.oauth2CredentialsSecretRef, and/or
  # kubernautAgent.fleet.oauth2CredentialsSecretRef to override
  # fleet.oauth2.credentialsSecretRef for that component only. Each falls
  # back to the shared value when unset, so setting the shared field alone
  # is enough when every component uses the same OAuth2 client.

  # --- Fleet Metadata Cache (FMC) — optional, ADR-068 ---
  # Deploys the operator-managed FMC service, which polls managed clusters
  # via the MCP Gateway (fleet.mcpGatewayEndpoint/mcpGatewayType above) and
  # serves federated scope-check results from Valkey over plain HTTP inside
  # the cluster (upstream's binary has no TLS server support). Most
  # deployments that enable spec.fleet use backend: acm (an existing RHACM
  # Search installation) instead of standing up FMC — only set
  # fleetMetadataCache.enabled: true when backend: fleetmetadatacache above
  # and you want the operator, rather than a separately-managed FMC, to be
  # the thing Gateway/RemediationOrchestrator query. When enabled with no
  # fleet.endpoint set, the operator auto-derives FMC's in-cluster URL — no
  # need to also fill in fleet.endpoint by hand.
  #
  # fleetMetadataCache:
  #   enabled: false
  #   fleet:
  #     oauth2CredentialsSecretRef: fmc-oauth2-creds   # optional; overrides fleet.oauth2.credentialsSecretRef for FMC only
  #   syncInterval: "30s"
  #   keyTTL: "45s"
  #   logging:
  #     level: info

  # --- Remediation orchestrator tuning (optional) ---
  remediationOrchestrator:
    # dryRun: false                     # enable dry-run mode (plans but does not execute)
    # dryRunHoldPeriod: "1h"            # how long to hold dry-run plans before expiry
    # fleet:
    #   oauth2CredentialsSecretRef: ro-oauth2-creds   # overrides fleet.oauth2.credentialsSecretRef for RemediationOrchestrator only
    # logging:
    #   level: info
    # timeouts:
    #   global: "1h"
    #   processing: "5m"
    #   analyzing: "10m"
    #   executing: "30m"
    #   awaitingApproval: "15m"
    #   verifying: "30m"
    # routing:
    #   consecutiveFailureThreshold: 3
    #   consecutiveFailureCooldown: "1h"
    #   recentlyRemediatedCooldown: "5m"
    #   exponentialBackoffBase: "1m"
    #   exponentialBackoffMax: "10m"
    #   noActionRequiredDelayHours: 24
    # retention:
    #   period: "24h"

  # --- Effectiveness Monitor tuning (optional) ---
  effectivenessMonitor:
    # fleet:
    #   oauth2CredentialsSecretRef: em-oauth2-creds   # overrides fleet.oauth2.credentialsSecretRef for EffectivenessMonitor only -- used when fleet.enabled: true, for remote-cluster effectiveness reads
    # logging:
    #   level: info

EOF
```

## Watch the rollout

The CR progresses through phases: **Validating** > **Migrating** > **Deploying** > **Running**.

```bash
oc get kubernaut kubernaut -n kubernaut-system -w
```

Check pod status:

```bash
oc get pods -n kubernaut-system -l app.kubernetes.io/managed-by=kubernaut-operator
```

All 10 services should reach Running within 2-5 minutes.

## Verify

```bash
# Per-service readiness
oc get kubernaut kubernaut -n kubernaut-system \
  -o jsonpath='{range .status.services[*]}{.name}{"\t"}{.ready}{"\t"}{.readyReplicas}/{.desiredReplicas}{"\n"}{end}'

# Gateway Route
oc get route -n kubernaut-system -l app=gateway

# Gateway health
GATEWAY_URL=$(oc get route gateway -n kubernaut-system -o jsonpath='{.spec.host}')
curl -sk "https://${GATEWAY_URL}/healthz"
```

## Seed the Workflow Catalog

After the AuthWebhook is ready, apply ActionTypes and RemediationWorkflows. The webhook uses `failurePolicy: Fail`, so CRD mutations are rejected until the webhook pod is healthy.

```bash
oc rollout status deployment/authwebhook -n kubernaut-system --timeout=3m

# Clone the demo scenarios repo (if not already available)
git clone https://github.com/jordigilh/kubernaut-demo-scenarios.git

# Apply ActionTypes first (order matters)
oc apply -f kubernaut-demo-scenarios/deploy/action-types/

# Then RemediationWorkflows
for dir in kubernaut-demo-scenarios/deploy/remediation-workflows/*/; do
  oc apply -f "$dir"
done
```

## Configure AlertManager

For signals to flow into Kubernaut, OCP AlertManager must route alerts to the Gateway webhook endpoint.

### Option A: Operator-managed AlertmanagerConfig (recommended)

The operator always creates a namespace-scoped `AlertmanagerConfig` CR that routes alerts originating from the `kubernaut-system` namespace to the Gateway, and provisions the RBAC binding (`alertmanager-main` SA → `gateway-signal-source` ClusterRole) so the AlertManager bearer token is authorized by the Gateway's SAR middleware.

**One manual step is required**: unlike Option B (where AlertManager's pod uses its own auto-mounted ServiceAccount token), the `AlertmanagerConfig` CRD's `authorization` field only accepts a Secret reference, not a file path -- Prometheus Operator's `SafeAuthorization` type intentionally has no `credentials_file` option, to prevent namespaced `AlertmanagerConfig` authors from reading arbitrary files off the Alertmanager container. The operator therefore never mints or stores this token itself (it is bring-your-own, like `spec.postgresql.secretName`); you must create it once:

```bash
# Mint a long-lived token for alertmanager-main and copy it into a Secret
# in the same namespace as your Kubernaut CR. Choose a duration you're
# comfortable re-running this command before (there is no auto-rotation
# for this copy -- see the note below).
TOKEN=$(oc create token alertmanager-main -n openshift-monitoring --duration=8760h)
oc create secret generic alertmanager-gateway-token \
  -n kubernaut-system \
  --from-literal=token="$TOKEN"
```

Then set `spec.gateway.alertManagerTokenSecretName: alertmanager-gateway-token` on your Kubernaut CR. The CR's `AlertManagerAuthConfigured` status condition reports whether this is correctly configured (`Ready`) or explains what's missing (`SecretNameNotConfigured`, `TokenSecretNotFound`, `TokenKeyMissing`) -- check it if alerts aren't reaching Gateway.

> **Note on rotation**: this token is a snapshot, not a live projection -- it does **not** auto-rotate the way a pod's mounted ServiceAccount token does. Re-run the command above (and update the Secret) before the `--duration` you chose elapses, or scope a shorter duration and rotate via your own automation/cron if you need tighter token lifetimes.

To route alerts from **other** namespaces (e.g. `demo-*`), you must use Option B or add additional `AlertmanagerConfig` CRs in those namespaces.

### Option B: Manual global AlertManager configuration

For cluster-wide alert routing (e.g. matching alerts from any namespace), edit the global AlertManager secret. The Gateway serves HTTPS on port 8443 with SAR-based bearer token authentication:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: alertmanager-main
  namespace: openshift-monitoring
type: Opaque
stringData:
  alertmanager.yaml: |
    global:
      resolve_timeout: 1m

    route:
      receiver: default
      group_wait: 5s
      group_interval: 5s
      repeat_interval: 1m
      routes:
        - match_re:
            namespace: "demo-.*"
          receiver: gateway-webhook
          group_by: [alertname, namespace]
          continue: false

    receivers:
      - name: default
      - name: gateway-webhook
        webhook_configs:
          - url: "https://gateway-service.kubernaut-system.svc.cluster.local:8443/api/v1/signals/prometheus"
            send_resolved: false
            http_config:
              authorization:
                type: Bearer
                credentials_file: /var/run/secrets/kubernetes.io/serviceaccount/token
              tls_config:
                insecure_skip_verify: true
EOF
```

Key points:
- **HTTPS required**: The Gateway serves TLS on port 8443 (inter-service encryption per FedRAMP SC-8).
- **Bearer token auth**: The Gateway validates the caller's identity via Kubernetes SubjectAccessReview. The `alertmanager-main` ServiceAccount is bound to the `gateway-signal-source` ClusterRole by the operator.
- **`insecure_skip_verify`**: The Gateway uses an OCP service-ca certificate. Set this to `true` for intra-cluster traffic, or mount the service CA and set `ca_file` for strict verification.

## Troubleshooting

**CR stuck in Validating:**

```bash
oc get kubernaut kubernaut -n kubernaut-system -o jsonpath='{.status.conditions}' | python3 -m json.tool
```

Common causes: missing secrets, unreachable PostgreSQL/Valkey host, hostname validation failure.

**CR stuck in Migrating:**

```bash
oc get jobs -n kubernaut-system
oc logs job/kubernaut-db-migration -n kubernaut-system
```

Common causes: PostgreSQL not accepting connections, wrong credentials, database does not exist.

**Agent RBAC / Permission Errors:**

If the Kubernaut Agent exhausts tool call budgets on `403 Forbidden` errors during
investigation, the agent SA may be missing required RBAC. Diagnose with:

```bash
# Check the investigator ClusterRole exists and has rules
oc get clusterrole <ns>-kubernaut-agent-investigator -o yaml

# Test specific access (e.g., SCCs)
oc auth can-i list securitycontextconstraints \
  --as=system:serviceaccount:kubernaut-system:kubernaut-agent-sa

# Check the RBACProvisioned condition
oc get kubernaut kubernaut -n kubernaut-system \
  -o jsonpath='{.status.conditions[?(@.type=="RBACProvisioned")]}'
```

Common causes: operator SA lacks `escalate`/`bind` permissions (check the
operator's own ClusterRole), manual edits to operator-managed ClusterRoles
(the operator will overwrite on the next reconcile), or OLM permission
conflicts with other operators managing the same ClusterRole names.

If using `spec.additionalClusterRoles`, check the `AdditionalRBACBound`
condition for `PartiallyBound` — one or more referenced ClusterRoles may
not exist.

**AlertManager alerts never reach Gateway (Option A):**

Check the `AlertManagerAuthConfigured` status condition:

```bash
oc get kubernaut kubernaut -n kubernaut-system \
  -o jsonpath='{.status.conditions[?(@.type=="AlertManagerAuthConfigured")]}'
```

| Reason | Meaning | Fix |
|---|---|---|
| `SecretNameNotConfigured` | `spec.gateway.alertManagerTokenSecretName` is unset | Follow the one-time setup step in [Option A](#option-a-operator-managed-alertmanagerconfig-recommended) above |
| `TokenSecretNotFound` | The named Secret doesn't exist in the CR's namespace | Re-check the Secret name/namespace, or re-run the `oc create secret` step |
| `TokenKeyMissing` | The Secret exists but has no `token` key | Recreate the Secret with `--from-literal=token=...` |
| `Ready` | Configuration is valid | If alerts still aren't arriving, check the token hasn't expired (see the rotation note above) or inspect Gateway's logs for `403` SAR-authorization failures |

**API Frontend crash-looping with `auth.issuerURL is required`:**

The AF requires OIDC authentication in production (TLS) mode. Set `spec.apiFrontend.auth.issuerURL` to your OIDC provider's issuer URL (e.g. RHBK/Keycloak realm). If you don't need external MCP/A2A access, disable the AF entirely:

```yaml
spec:
  apiFrontend:
    enabled: false
```

**Console shows "Access Denied — You don't have permission to use Kubernaut":**

This is a separate, coarse-grained authorization gate from per-tool RBAC (AF v1.5.6+ / operator v1.5.8+): the Console's pre-flight check calls `GET /a2a/access` on AF, which runs a SubjectAccessReview against a synthetic `kubernaut.ai/console` resource. See [Additional RBAC for API Frontend](02-configure-services.md#additional-rbac-for-api-frontend) for how this is configured, and this dedicated writeup for full diagnosis (Keycloak group-claim verification, AF debug-log inspection): [Kubernaut Console: troubleshooting "Access Denied"](https://gist.github.com/jordigilh/5984f65c88da042f2207825a9e57df62).

Quick check: if `spec.apiFrontend.rbac.roleBindings` already lists groups, the operator auto-derives console access from them and the CR is very likely already correct — the more common cause at that point is the denied user's JWT not actually carrying the expected `groups` claim.

**API Frontend `proxy-init` crash-looping with `can't initialize iptables table 'mangle': Permission denied`:**

When Kagenti is installed, the `proxy-init` init container sets up iptables rules
for traffic interception. On some OpenShift versions (observed on OCP 4.21), the
`iptable_mangle` kernel module is not loaded by default even though `iptable_nat`
is. This causes `proxy-init` to select the `iptables-legacy` backend (because
`iptable_nat` is present) and then fail when it tries to manipulate the `mangle`
table.

Diagnose by checking the init container logs:

```bash
oc logs <apifrontend-pod> -n kubernaut-system -c proxy-init
```

If you see:

```
Using iptables command: iptables-legacy (iptables v1.8.11 (legacy))
iptables v1.8.11 (legacy): can't initialize iptables table `mangle': Permission denied
```

Load the missing kernel module on **every worker node** where Kubernaut pods may
be scheduled:

```bash
# From a debug shell or SSH session on each node:
sudo modprobe iptable_mangle
```

Then delete the crashing pod so it restarts:

```bash
oc delete pod <apifrontend-pod> -n kubernaut-system
```

To persist the module across node reboots, create a `MachineConfig`:

```bash
oc apply -f - <<EOF
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: 99-worker-iptable-mangle
  labels:
    machineconfiguration.openshift.io/role: worker
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      files:
        - path: /etc/modules-load.d/iptable_mangle.conf
          mode: 0644
          contents:
            source: data:,iptable_mangle
EOF
```

> **Note:** This issue is tracked upstream as
> [kagenti/kagenti-extensions#502](https://github.com/kagenti/kagenti-extensions/issues/502).
> On OCP 4.18 and earlier, `iptable_mangle` is typically loaded by default and
> this workaround is not needed.

**CR in Degraded:**

One or more services are not ready. Check which:

```bash
oc get kubernaut kubernaut -n kubernaut-system \
  -o jsonpath='{range .status.services[?(@.ready==false)]}{.name}{"\n"}{end}'
```

Then inspect the failing deployment:

```bash
oc logs -n kubernaut-system deployment/<service>-controller --tail=50
oc describe pod -n kubernaut-system -l app=<service>
```

---

Previous: [Configure Services](02-configure-services.md)

Next (optional): [Fleet: Kuadrant MCP Gateway](04-fleet-mcp-gateway.md) -- required only if you enable `spec.fleet.mcpGatewayType: kuadrant` above.

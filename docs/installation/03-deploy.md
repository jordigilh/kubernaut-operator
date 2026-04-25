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
  image: quay.io/kubernaut-ai/kubernaut-operator-catalog:v1.3.0
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
apiVersion: kubernaut.ai/v1alpha1
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

  # --- LLM (from Step 2: Configure Services) ---
  kubernautAgent:
    llm:
      provider: openai                     # or: anthropic, vertex_ai
      model: gpt-4o                        # or: claude-sonnet-4-6, etc.
      credentialsSecretName: llm-credentials
      # sdkConfigMapName: kubernaut-agent-sdk-config  # uncomment for Vertex AI / advanced

  # --- Policies (from Step 2: Configure Services) ---
  aiAnalysis:
    policy:
      configMapName: aianalysis-policies

  signalProcessing:
    policy:
      configMapName: signalprocessing-policy
    # proactiveSignalMappings:
    #   configMapName: proactive-signal-mappings  # uncomment to enable proactive mode

  # --- Notifications (from Step 2: Configure Services) ---
  notification:
    slack:
      secretName: slack-webhook            # omit to disable Slack delivery
      channel: "#kubernaut-alerts"

  # --- OCP integration ---
  monitoring:
    enabled: true

  gateway:
    route:
      enabled: true
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
oc rollout status deployment/authwebhook-controller -n kubernaut-system --timeout=3m

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

For signals to flow into Kubernaut, OCP AlertManager must route alerts to the Gateway:

```bash
GATEWAY_URL=$(oc get route gateway -n kubernaut-system -o jsonpath='{.spec.host}')
echo "Configure AlertManager receiver → https://${GATEWAY_URL}/api/v1/alerts"
```

Add a receiver in your AlertManager configuration (via the OCP console under **Administration > Cluster Settings > Configuration > Alertmanager**) pointing to this URL.

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

# Fleet: Kuadrant MCP Gateway (Optional)

This is a prerequisite for enabling Fleet federation with `spec.fleet.mcpGatewayType: kuadrant` (see the `fleet:` block in [Deploy Kubernaut](03-deploy.md#create-the-kubernaut-cr)). It provisions the cluster-side MCP Gateway stack that Kubernaut's `fleet.mcpGatewayEndpoint` points at; no `kubernaut-operator` code changes are involved.

Skip this guide entirely if you don't use Fleet, or if you use `mcpGatewayType: eaigw` instead.

## Architecture

```
External client
      |
      v
OpenShift Route (edge TLS)
      |
      v
Gateway API `Gateway` (GatewayClass: istio)
      |
      v
Kuadrant MCP broker (aggregates tools across MCPServerRegistrations)
      |
      v
kube-mcp-server(s)  (one per managed cluster, registered via MCPServerRegistration)
```

The Kuadrant **controller** watches `MCPGatewayExtension`/`MCPServerRegistration` CRs and reconciles the **broker** Deployment/Service, an Istio `EnvoyFilter` (for MCP-aware request handling), and — unless disabled — the `HTTPRoute` that exposes the broker on the `Gateway`. The broker itself proxies `tools/*` calls to whichever backend MCP server (e.g. `kube-mcp-server`) each `MCPServerRegistration` points at, aggregating and prefixing their tools.

## Prerequisites

- Cluster-admin `oc`/`kubectl` access (installs cluster-scoped CRDs and RBAC).
- Gateway API CRDs v1.1.0 or later, cluster-wide (`gateway.networking.k8s.io` group: `GatewayClass`, `Gateway`, `HTTPRoute`, `ReferenceGrant`).
- Istio or OpenShift Service Mesh (OSSM, Sail Operator) installed and providing an `istio` `GatewayClass`. Both sidecar and ambient mesh modes work — the stack only relies on `EnvoyFilter` and Gateway API objects, neither of which is ambient-mode-specific.
- A namespace to host the gateway and MCP components. This guide uses `gateway-system` for the `Gateway`/`Route` and `mcp-system` for everything else, matching the upstream [Kuadrant mcp-gateway](https://github.com/Kuadrant/mcp-gateway) `overlays/mcp-system` naming.
- An OIDC identity provider (RHBK — Red Hat build of Keycloak — or upstream Keycloak) with a realm that provides: a `client_credentials` client for callers (e.g. `kubernaut-fleet-read`), a client with RFC 8693 Standard Token Exchange enabled for `kube-mcp-server` itself, and a bearer-only audience client the target Kubernetes API server validates against (e.g. `k8s-api`). The target cluster's API server must already trust this issuer as an OIDC provider (on OpenShift, via a `type: OIDC` entry in the cluster `Authentication` CR's `oidcProviders`; see your OCP version's OIDC identity provider documentation). This is required for **Step 6**'s `passthrough` mode below — the only mode validated end-to-end by kubernaut's own fleet E2E suite, and the only one that lets the target API server enforce RBAC per caller identity instead of a single blanket ServiceAccount for every fleet caller.

> **Note:** if your cluster already has a `kagenti` Helm-based deployment, it may have already created a `Gateway`, `gateway-system`/`mcp-system` namespaces, and a `ReferenceGrant` for its own (unrelated) `mcp.kagenti.com/MCPGatewayExtension` CRD. See [Troubleshooting: naming collision with kagenti](#referencegrantrequired-despite-having-a-referencegrant) before assuming a pre-existing `ReferenceGrant` covers Kuadrant's.

## Step 1: Gateway API `Gateway` + Route

Skip this step if a `Gateway` already exists for MCP traffic — just confirm its listener `hostname` is set to the exact external hostname you'll expose (see the note below), not a wildcard or placeholder value.

```bash
oc new-project gateway-system 2>/dev/null || true

oc apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: mcp-gateway
  namespace: gateway-system
spec:
  gatewayClassName: istio
  listeners:
  - name: mcp
    port: 8080
    protocol: HTTP
    hostname: <MCP_GATEWAY_HOST>   # e.g. mcp-gateway-gateway-system.apps.<cluster-domain>
    allowedRoutes:
      namespaces:
        from: All
EOF
```

Expose it externally with an OpenShift `Route` targeting the Istio-provisioned Service (`<gateway-name>-istio`), on the listener's named port:

```bash
oc apply -f - <<EOF
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: mcp-gateway
  namespace: gateway-system
spec:
  to:
    kind: Service
    name: mcp-gateway-istio
  port:
    targetPort: mcp
  tls:
    termination: edge
EOF
```

Record the Route's hostname — every hostname used below (the `Gateway` listener, the `MCPGatewayExtension`, and every `HTTPRoute`) must match it exactly:

```bash
export MCP_GATEWAY_HOST=$(oc get route mcp-gateway -n gateway-system -o jsonpath='{.spec.host}')
echo "$MCP_GATEWAY_HOST"
```

> **Why an exact hostname, not a wildcard:** Gateway API only attaches an `HTTPRoute` to a listener when their hostnames intersect. A wildcard or placeholder listener hostname (e.g. a Kind-style `*.127-0-0-1.sslip.io` copied from a test fixture) will not intersect with your real OpenShift Route hostname, silently leaving every `HTTPRoute` unroutable. If you inherited a `Gateway` with a placeholder hostname, patch it:
>
> ```bash
> oc patch gateway mcp-gateway -n gateway-system --type=json \
>   -p="[{\"op\":\"replace\",\"path\":\"/spec/listeners/0/hostname\",\"value\":\"${MCP_GATEWAY_HOST}\"}]"
> ```

## Step 2: Install the Kuadrant MCP Gateway CRDs

```bash
export KUADRANT_MCP_GATEWAY_REF=v0.7.1

oc apply -k "https://github.com/Kuadrant/mcp-gateway/config/crd?ref=${KUADRANT_MCP_GATEWAY_REF}"
```

This installs three cluster-scoped CRDs in the `mcp.kuadrant.io` group: `MCPGatewayExtension`, `MCPServerRegistration`, `MCPVirtualServer`.

## Step 3: Allow the extension to reference the Gateway

The controller needs a `ReferenceGrant` in the `Gateway`'s namespace to reconcile a cross-namespace `MCPGatewayExtension` -> `Gateway` reference:

```bash
oc apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
metadata:
  name: allow-kuadrant-mcp-gateway-extension
  namespace: gateway-system
spec:
  from:
  - group: mcp.kuadrant.io
    kind: MCPGatewayExtension
    namespace: mcp-system
  to:
  - group: gateway.networking.k8s.io
    kind: Gateway
EOF
```

## Step 4: Deploy the Kuadrant controller and configure the broker

Apply the RBAC/controller/Secret resources from the `mcp-system` overlay, **excluding** its bundled example `HTTPRoute`. The controller creates and manages that `HTTPRoute` itself once the `MCPGatewayExtension` below sets `httpRouteManagement: Enabled` (the default) — hand-applying the example manifest first only creates a route that gets immediately superseded, and risks pinning the wrong (example) hostname if reapplied later.

```bash
oc kustomize "https://github.com/Kuadrant/mcp-gateway/config/mcp-gateway/overlays/mcp-system?ref=${KUADRANT_MCP_GATEWAY_REF}" \
  > /tmp/kuadrant-mcp-system-overlay.yaml

# Drop the bundled example HTTPRoute (kind: HTTPRoute) before applying --
# see the paragraph above for why.
python3 -c "
import re
docs = open('/tmp/kuadrant-mcp-system-overlay.yaml').read().split('\n---\n')
docs = [d for d in docs if not re.search(r'^kind:\s*HTTPRoute', d, re.MULTILINE)]
open('/tmp/kuadrant-mcp-system-overlay.yaml', 'w').write('\n---\n'.join(docs))
"

oc apply -f /tmp/kuadrant-mcp-system-overlay.yaml
oc rollout status deployment/mcp-gateway-controller -n mcp-system --timeout=2m
```

This creates the `mcp-system` namespace, the `mcp-controller` ServiceAccount/ClusterRole/ClusterRoleBinding, a `trusted-headers-public-key` Secret, the `mcp-gateway-controller` Deployment, and an `MCPGatewayExtension` named `mcp-gateway-extension` (still pointing at the example's placeholder `publicHost` at this point — fixed next).

## Step 5: Point the extension at your real hostname

```bash
oc patch mcpgatewayextension mcp-gateway-extension -n mcp-system --type=merge \
  -p="{\"spec\":{\"publicHost\":\"${MCP_GATEWAY_HOST}\"}}"

oc wait --for=condition=Ready mcpgatewayextension/mcp-gateway-extension \
  -n mcp-system --timeout=60s
```

Once `Ready`, the controller creates the broker Deployment/Service (`mcp-gateway`, port 8080), an `EnvoyFilter` in `gateway-system`, and an `HTTPRoute` (`mcp-gateway-route`) in `mcp-system` with the correct hostname and backend:

```bash
oc rollout status deployment/mcp-gateway -n mcp-system --timeout=2m
```

## Step 6: Deploy a backend MCP server (kube-mcp-server)

> **Use `passthrough` mode, not `cluster_auth_mode = "kubeconfig"`.** `kubeconfig` mode makes kube-mcp-server ignore any caller-forwarded `Authorization` header and always act as its own ServiceAccount — every fleet caller gets the same blanket RBAC regardless of who they are, and (per [Issue #414](https://github.com/jordigilh/kubernaut-operator/issues/414)) it is a combination kubernaut's own fleet E2E suite has never exercised against a real `tools/call`: every E2E lane pins `cluster_auth_mode = "passthrough"` with RFC 8693 Standard Token Exchange. `passthrough` below is the only mode with actual E2E coverage and the only one that lets the target API server enforce RBAC per caller identity — use it even for a single-cluster/loopback registration like this one.

kube-mcp-server validates the caller's own Bearer token (issued by RHBK/Keycloak) as an OAuth resource server, then exchanges it via RFC 8693 for a token the target Kubernetes API server's OIDC integration accepts (see [Prerequisites](#prerequisites)). Replace `<RHBK_ISSUER_URL>` (e.g. `https://rhbk.apps.<cluster-domain>/realms/kubernaut-fleet`), `<STS_CLIENT_ID>`/`<STS_CLIENT_SECRET>` (the token-exchange-enabled client), and `<K8S_API_AUDIENCE_SCOPE>` (the client scope carrying the audience-mapper that gates the exchange, e.g. `k8s-api-audience`) with your realm's actual values:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kube-mcp-server
  namespace: mcp-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-mcp-server-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
- kind: ServiceAccount
  name: kube-mcp-server
  namespace: mcp-system
---
# A Secret, not a ConfigMap: this file carries sts_client_secret in plaintext.
apiVersion: v1
kind: Secret
metadata:
  name: kube-mcp-server-config
  namespace: mcp-system
  labels:
    app: kube-mcp-server
    component: fleet
type: Opaque
stringData:
  config.toml: |
    require_oauth = true
    authorization_url = "<RHBK_ISSUER_URL>"
    oauth_audience = "kube-mcp-server"
    cluster_auth_mode = "passthrough"
    sts_client_id = "<STS_CLIENT_ID>"
    sts_client_secret = "<STS_CLIENT_SECRET>"
    sts_audience = "k8s-api"
    sts_scopes = ["<K8S_API_AUDIENCE_SCOPE>"]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-mcp-server
  namespace: mcp-system
  labels:
    app: kube-mcp-server
    component: fleet
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kube-mcp-server
  template:
    metadata:
      labels:
        app: kube-mcp-server
        component: fleet
    spec:
      serviceAccountName: kube-mcp-server
      containers:
      - name: kube-mcp-server
        image: ghcr.io/containers/kubernetes-mcp-server:latest
        args:
        - "--port=8080"
        - "--cluster-provider=in-cluster"
        - "--toolsets=core"
        - "--stateless"
        - "--list-output=yaml"
        - "--config=/etc/kubernetes-mcp-server/config.toml"
        ports:
        - name: http
          containerPort: 8080
        volumeMounts:
        - name: config
          mountPath: /etc/kubernetes-mcp-server
          readOnly: true
        readinessProbe:
          httpGet: {path: /healthz, port: 8080}
          initialDelaySeconds: 3
          periodSeconds: 5
        livenessProbe:
          httpGet: {path: /healthz, port: 8080}
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          requests: {memory: "32Mi", cpu: "50m"}
          limits: {memory: "128Mi", cpu: "250m"}
      volumes:
      - name: config
        secret:
          secretName: kube-mcp-server-config
---
apiVersion: v1
kind: Service
metadata:
  name: kube-mcp-server
  namespace: mcp-system
  labels:
    app: kube-mcp-server
    component: fleet
spec:
  ports:
  - name: http
    port: 8080
    targetPort: 8080
  selector:
    app: kube-mcp-server
EOF

oc rollout status deployment/kube-mcp-server -n mcp-system --timeout=2m
```

`require_oauth = true` makes kube-mcp-server validate the caller's incoming Bearer token before attempting the exchange (this is also what the broker's own discovery-connection credential in **Step 7** authenticates against). `sts_scopes` must be set explicitly even if the scope is already a default client scope — RHBK/Keycloak's token-exchange endpoint rejects an explicitly-empty `scope` parameter rather than treating it as "no filter." The `kube-mcp-server` ServiceAccount/ClusterRoleBinding above is vestigial in `passthrough` mode (RBAC is enforced against the *exchanged caller identity* on the target API server, not this ServiceAccount) but still required for the pod's own liveness/readiness probing and any fallback paths. No SCC/PSA changes are needed: this pod spec runs cleanly under the `restricted` Pod Security profile `mcp-system` enforces by default.

## Step 7: Register the backend with the broker

The broker discovers backend MCP servers via `MCPServerRegistration` CRs, which target an `HTTPRoute` describing where the backend lives.

> **`credentialRef` is required now that `require_oauth = true` (Step 6).** The broker maintains its own upstream tool-discovery/session-management connection to kube-mcp-server, separate from per-request `tools/call` proxying (which forwards the caller's own `Authorization` header unmodified — see [Kuadrant's `MCPServerRegistration` reference](https://docs.kuadrant.io/dev/mcp-gateway/docs/reference/mcpserverregistration/)). With `require_oauth = true`, that discovery connection is itself subject to kube-mcp-server's OAuth resource-server check, so the broker needs its own static credential here or its discovery/health probe gets rejected — surfacing later as tools that list correctly but fail `tools/call` (the exact symptom in [Issue #414](https://github.com/jordigilh/kubernaut-operator/issues/414)). Get a token the same way a real caller would (`client_credentials` against your `kubernaut-fleet-read`-equivalent client, scoped to the `kube-mcp-server` audience) and store it verbatim — Kuadrant sends the Secret's value as-is as the `Authorization` header, it does not prepend `Bearer ` itself:
>
> ```bash
> BROKER_TOKEN=$(curl -sk -X POST "<RHBK_ISSUER_URL>/protocol/openid-connect/token" \
>   -d grant_type=client_credentials -d client_id=<CALLER_CLIENT_ID> \
>   -d client_secret=<CALLER_CLIENT_SECRET> -d scope=kube-mcp-server-audience \
>   | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')
>
> oc apply -n mcp-system -f - <<EOF
> apiVersion: v1
> kind: Secret
> metadata:
>   name: kube-mcp-server-broker-cred
>   labels:
>     mcp.kuadrant.io/secret: "true"
> type: Opaque
> stringData:
>   token: "Bearer ${BROKER_TOKEN}"
> EOF
> ```
>
> Reference it from the registration below with `spec.credentialRef: {name: kube-mcp-server-broker-cred}`. The token is static for its own lifetime (no refresh) — pick a realm client whose access-token lifespan comfortably outlives your operational window, and rotate the Secret (re-run the `curl` above and re-apply) before it expires.

> **The `kubernaut.ai/managed: "true"` label is mandatory, not decorative.** Kubernaut's `KuadrantRegistry` (`pkg/fleet/registry/kuadrant_registry.go`) only tracks `MCPServerRegistration` CRs carrying this exact label; anything without it is silently ignored and never enters `ClusterRegistry`. A cluster missing this label is invisible to Fleet: SignalProcessing/RemediationOrchestrator/APIFrontend won't classify or route signals for it, so alerts and remediations targeting that cluster are effectively dropped with no error surfaced. Do not omit or rename it when adding registrations for additional clusters.

> **Every other label you add here becomes that cluster's Rego-visible "spec."** `KuadrantRegistry` doesn't just check for `kubernaut.ai/managed` -- it copies the *entire* `metadata.labels` map of each `MCPServerRegistration` into `ClusterInfo.Labels`, which SignalProcessing's enricher then surfaces as the `cluster` classification dimension (`KubernetesContext.Cluster.Labels`, upstream `BR-FLEET-003`/`#1511`) for every signal coming from that cluster. In other words, this is how a Rego policy (the same `signalprocessing-policy`/`aianalysis-policy` ConfigMaps from [Quickstart](00-quickstart.md#1-prerequisites)) tells clusters apart and applies different rules per cluster -- e.g. stricter auto-remediation gating for `environment: "production"` than for `environment: "staging"`. The `environment: "production"` label on the example registration below isn't just a cosmetic tag; it's exactly this mechanism. When registering additional clusters, add whatever labels your policies need to differentiate them (environment, region, criticality tier, etc.) -- they'll be readable in Rego as `input.cluster.labels.<key>`.

```bash
oc apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: kube-mcp-server-route
  namespace: mcp-system
spec:
  hostnames:
  - ${MCP_GATEWAY_HOST}
  parentRefs:
  - name: mcp-gateway
    namespace: gateway-system
    sectionName: mcp
  rules:
  - backendRefs:
    - name: kube-mcp-server
      port: 8080
---
apiVersion: mcp.kuadrant.io/v1alpha1
kind: MCPServerRegistration
metadata:
  name: loopback-cluster
  namespace: mcp-system
  labels:
    kubernaut.ai/managed: "true"
    environment: "production"
spec:
  prefix: "loopback_cluster_"
  credentialRef:
    name: kube-mcp-server-broker-cred
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: kube-mcp-server-route
    namespace: mcp-system
EOF
```

Add one `HTTPRoute` + `MCPServerRegistration` pair per managed cluster you want Fleet to see, each with its own `prefix` (tool names are exposed as `<prefix><tool-name>`, e.g. `loopback_cluster_pods_list`) and its own classification labels (see the callout above) so downstream Rego policies can tell clusters apart.

## Verification

```bash
# All three Deployments Ready
oc rollout status deployment/mcp-gateway-controller -n mcp-system --timeout=1m
oc rollout status deployment/mcp-gateway -n mcp-system --timeout=1m
oc rollout status deployment/kube-mcp-server -n mcp-system --timeout=1m

# Registration picked up tools from the backend (may take up to ~60s
# after creation -- the broker validates registrations on a polling loop)
oc get mcpserverregistration -n mcp-system
# Expect READY=True and a non-zero TOOLS count

# End-to-end MCP handshake through the real external Route
curl -sS -D /tmp/mcp-headers.txt -X POST "https://${MCP_GATEWAY_HOST}/mcp" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"preflight-check","version":"0.1"}}}'
# Expect HTTP 200 with a "Kuadrant MCP Gateway" serverInfo result
```

> **A successful `initialize` does NOT prove `tools/call` works — verify a real authenticated tool call too.** `tools/list`/`discover_tools` are served straight from the broker's own aggregated catalog; `tools/call` is routed separately (Envoy `ext_proc` parses the tool name, strips the prefix, and resolves the target backend). A cluster can pass every check above and still fail every real tool call — this exact gap is what [Issue #414](https://github.com/jordigilh/kubernaut-operator/issues/414) found. Don't consider this stack verified until this succeeds:
>
> ```bash
> SESSION_ID=$(grep -i mcp-session-id /tmp/mcp-headers.txt | awk -F': ' '{print $2}' | tr -d '\r')
>
> # Same client_credentials token a real fleet caller (e.g. fleetmetadatacache) would use.
> TOKEN=$(curl -sk -X POST "<RHBK_ISSUER_URL>/protocol/openid-connect/token" \
>   -d grant_type=client_credentials -d client_id=<CALLER_CLIENT_ID> \
>   -d client_secret=<CALLER_CLIENT_SECRET> -d scope=kube-mcp-server-audience \
>   | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')
>
> curl -sS -X POST "https://${MCP_GATEWAY_HOST}/mcp" \
>   -H "Authorization: Bearer ${TOKEN}" \
>   -H "Content-Type: application/json" \
>   -H "Accept: application/json, text/event-stream" \
>   -H "Mcp-Session-Id: ${SESSION_ID}" \
>   -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"loopback_cluster_resources_list","arguments":{"kind":"Pod","apiVersion":"v1","namespace":"mcp-system"}}}'
> # Expect a real JSON-RPC result carrying Pod data, NOT
> # {"error":{"code":-32602,"message":"tool '...' not found: tool not found"}}
> ```

A full `tools/list` call requires the session established by `initialize`: capture the `Mcp-Session-Id` response header and send it back as a request header on the follow-up call (see the `curl -D` pattern in [Troubleshooting: "Invalid session ID"](#invalid-session-id-on-toolslist)).

## Wiring into the Kubernaut CR

Once verified, point Fleet at the gateway (see [Deploy Kubernaut](03-deploy.md) for the full `spec.fleet` block):

```yaml
spec:
  fleet:
    enabled: true
    mcpGatewayEndpoint: "https://<MCP_GATEWAY_HOST>/mcp"
    mcpGatewayType: kuadrant
```

## Troubleshooting

### `ReferenceGrantRequired` despite having a `ReferenceGrant`

```
message: 'invalid: ReferenceGrant required in gateway-system to allow cross-namespace reference from mcp-system'
reason: ReferenceGrantRequired
```

A `ReferenceGrant` only satisfies a reference whose `from.group`/`from.kind` match exactly. If the cluster already has another MCP-related product installed (e.g. `kagenti`'s Helm chart), it may have created a same-named-sounding but different-group `ReferenceGrant` — check:

```bash
oc get referencegrant -n gateway-system -o yaml
```

`meta.helm.sh/release-name` in a grant's annotations tells you which product owns it. If none has `from.group: mcp.kuadrant.io` with `kind: MCPGatewayExtension`, create the one in [Step 3](#step-3-allow-the-extension-to-reference-the-gateway) — it's additive and safe to have alongside an unrelated product's own grant.

### `HTTPRoute` never becomes `Accepted`, or `MCPServerRegistration` stays `NotReady`

Almost always a hostname/listener mismatch. Confirm the `Gateway` listener's `hostname` is an exact match (not a wildcard, not a leftover placeholder) for every `HTTPRoute` hostname you create:

```bash
oc get gateway mcp-gateway -n gateway-system -o jsonpath='{.spec.listeners}' | python3 -m json.tool
oc get httproute -A -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}: {.spec.hostnames}{"\n"}{end}'
```

If you inherited a `Gateway`/`HTTPRoute` from a test fixture or another install attempt, look for Kind-cluster artifacts that don't apply to a real OpenShift Route: `*.sslip.io`/`*.mcp.local` hostnames, or an `HTTPRoute` with a `parentRef` pointing at a `Gateway` that doesn't exist in this cluster/namespace. Delete and recreate rather than patching around a broken route — this stack has few enough objects that a clean recreate (Steps 1 and 7) is faster than diffing a stale one.

### `"mcp server is not present in gateway yet"` right after creating a registration

Transient. The broker validates registrations on a background polling loop (roughly every 5s, converging within ~60s), not synchronously on creation. Re-check after a short wait:

```bash
oc get mcpserverregistration -n mcp-system -w
```

If it's still `NotReady` after 2 minutes, check the broker's own logs (not the controller's) for the actual upstream connection error:

```bash
oc logs deployment/mcp-gateway -n mcp-system --tail=50
```

### Tools appear in `tools/list`/`discover_tools` but `tools/call` fails with `"tool ... not found"`

```json
{"jsonrpc":"2.0","id":3,"error":{"code":-32602,"message":"tool 'resources_list' not found: tool not found"}}
```

with the broker logging something like:

```
level=INFO msg="received 404 from backend MCP " component=router method=server/discover server=""
```

This is [Issue #414](https://github.com/jordigilh/kubernaut-operator/issues/414)'s exact signature. `tools/list`/`discover_tools` are served directly from the broker's own aggregated catalog and don't prove `tools/call` routing works — that's a separate code path (Envoy `ext_proc` parses the tool name out of the request, strips the prefix, and resolves the target backend). Root-cause investigation for #414 found this combination is not covered by kubernaut's own fleet E2E suite in two dimensions simultaneously:

- **Auth mode**: every E2E lane pins `cluster_auth_mode = "passthrough"` with RFC 8693 token exchange and a `credentialRef` on the registration. If you're running `cluster_auth_mode = "kubeconfig"` with no `credentialRef` (an earlier revision of **Step 6**/**Step 7** in this doc showed exactly that, untested, configuration), switch to the `passthrough` example now in this doc.
- **Network path**: every E2E lane reaches the gateway via a Kind cluster's NodePort directly; none goes through an OpenShift `Route` (edge/re-encrypt TLS termination) in front of the Istio `Gateway`, as this doc's Step 1 does. If switching to `passthrough` mode doesn't resolve it, this is the next thing to isolate — try reaching the gateway's in-cluster Service directly (bypassing the `Route`) from a debug pod to see if the failure persists without the Route hop.

If neither resolves it, capture the broker's `tools/call` logs (`oc logs deployment/mcp-gateway -n mcp-system`) at debug verbosity and compare against [Kuadrant's own troubleshooting guide](https://docs.kuadrant.io/dev/mcp-gateway/docs/guides/troubleshooting/) before filing upstream against `Kuadrant/mcp-gateway`.

### `unknown field "spec.rules[0].name"` in controller logs

Harmless. This is the controller setting an `HTTPRouteRule.name` field that only exists in Gateway API v1.2+; against an older (but still supported, v1.1.0+) Gateway API CRD bundle, the API server prunes the unrecognized field with an informational log line rather than rejecting the request. The `HTTPRoute` still gets created/updated correctly — verify with `oc get httproute -A` rather than treating this log line as a failure signal.

### `Invalid session ID` on `tools/list`

The Kuadrant broker's MCP transport is session-based (Streamable HTTP): every call after `initialize` must carry the `Mcp-Session-Id` header returned by `initialize`'s response headers. A bare `tools/list` with no prior `initialize` in the same "session" always 404s with this message — it is not an auth or routing failure. Reproduce the full handshake:

```bash
curl -sS -D /tmp/headers.txt -X POST "https://${MCP_GATEWAY_HOST}/mcp" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"c","version":"0.1"}}}'
SESSION_ID=$(grep -i mcp-session-id /tmp/headers.txt | awk -F': ' '{print $2}' | tr -d '\r')

curl -sS -X POST "https://${MCP_GATEWAY_HOST}/mcp" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: ${SESSION_ID}" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```

### A registered cluster's alerts/remediations are silently ignored

If `oc get mcpserverregistration` shows a registration as `READY=True` with tools discovered, but Fleet-aware components (SignalProcessing, RemediationOrchestrator, APIFrontend) never classify or route signals for that cluster, check the `kubernaut.ai/managed` label first:

```bash
oc get mcpserverregistration <name> -n mcp-system -o jsonpath='{.metadata.labels.kubernaut\.ai/managed}{"\n"}'
# Must print exactly: true
```

`KuadrantRegistry` filters on this label when building `ClusterRegistry` (`pkg/fleet/registry/kuadrant_registry.go`) -- a registration without it reconciles and serves tools normally (the broker/backend path doesn't care about it at all), but is invisible to every Fleet consumer that reads `ClusterRegistry`. There is no error or event for this; the cluster just never appears in `list_clusters` or classification output. Add the label and it picks up on the registry's next watch event, no restart required.

### OLM's `kuadrant-operator` is not this stack

The Community Operators `kuadrant-operator` (installable via OperatorHub) is a *different* Kuadrant product — API-management policies (rate limiting, auth policies) for arbitrary Gateway API traffic. It does not provide the `mcp.kuadrant.io` CRDs, controller, or broker this guide installs. Don't install it expecting it to satisfy this prerequisite; use the `kubectl apply -k` steps above instead.

---

Previous: [Deploy Kubernaut](03-deploy.md)

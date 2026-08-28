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
- Gateway API CRDs v1.1.0 or later, cluster-wide (`gateway.networking.k8s.io` group: `GatewayClass`, `Gateway`, `HTTPRoute`, `ReferenceGrant`). **v1.4+ (with `HTTPRoute.spec.rules[].name`) is not required** — see [Troubleshooting: `unknown field "spec.rules[0].name"`](#unknown-field-specrules0name-in-controller-logs). We've verified this stack end-to-end on Gateway API v1.2.1 and v1.3.0 (OCP 4.20/4.21's bundled versions).
- Istio or OpenShift Service Mesh (OSSM, Sail Operator) installed and providing a `GatewayClass`. Both sidecar and ambient mesh modes work — the stack only relies on `EnvoyFilter` and Gateway API objects, neither of which is ambient-mode-specific. Two supported paths, with different minimum OCP versions and different follow-on config (see [Step 1](#step-1-gateway-api-gateway--route)):
    - **Self-managed Istio/OSSM** (Sail Operator, your own `istio` `GatewayClass`) — OCP **4.14+** (OSSM 3.0 supports 4.14–4.19; OSSM 3.2/3.3 support 4.18+). This is what the rest of this guide assumes by default.
    - **OpenShift's native `openshift-default` `GatewayClass`** (Ingress-Operator-managed, no separate Istio operator to install) — OCP **4.19+** only (Gateway API CRDs and the native controller aren't available before then). Requires one extra field on the `MCPGatewayExtension` in Step 5 — see the callout there.
- A namespace to host the gateway and MCP components. This guide uses `gateway-system` for the `Gateway`/`Route` and `mcp-system` for everything else, matching the upstream [Kuadrant mcp-gateway](https://github.com/Kuadrant/mcp-gateway) `overlays/mcp-system` naming.
- An OIDC identity provider (RHBK — Red Hat build of Keycloak — or upstream Keycloak) with a realm that provides: a `client_credentials` client for callers (e.g. `kubernaut-fleet-read`), a client with RFC 8693 Standard Token Exchange enabled for `kube-mcp-server` itself, and a bearer-only audience client the target Kubernetes API server validates against (e.g. `k8s-api`). The target cluster's API server must already trust this issuer as an OIDC provider (on OpenShift, via a `type: OIDC` entry in the cluster `Authentication` CR's `oidcProviders`). This is required for **Step 6**'s `passthrough` mode below — the only mode validated end-to-end by kubernaut's own fleet E2E suite, and the only one that lets the target API server enforce RBAC per caller identity instead of a single blanket ServiceAccount for every fleet caller.
    - **Don't have an OIDC provider yet, or adding more than one managed cluster?** See [Fleet: Multi-Cluster Setup](05-fleet-multi-cluster.md) for a from-scratch RHBK install, realm/client config, and the extra steps for registering a second ("spoke") cluster's `kube-mcp-server` with this same broker.
    - **This is a separate OCP version requirement from the Gateway/Istio one above, and the binding one for most deployments.** The `Authentication` CR's external-OIDC support (`type: OIDC`) is Tech Preview in OCP 4.19 (requires the irreversible `TechPreviewNoUpgrade` feature gate — not recommended for any cluster you might need to upgrade later) and **GA in OCP 4.20+**. Plan on **OCP 4.20+** for the full stack (Gateway/Istio + passthrough auth) regardless of which Gateway path above you pick, unless you're deliberately staying on `kubeconfig` mode (not recommended — see the callout in **Step 6**).

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
  gatewayClassName: istio   # or "openshift-default" -- see Prerequisites
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

> **Using `openshift-default` instead of `istio`?** You must first create the `GatewayClass` yourself (the Ingress Operator only manages it once it exists):
>
> ```bash
> oc apply -f - <<EOF
> apiVersion: gateway.networking.k8s.io/v1
> kind: GatewayClass
> metadata:
>   name: openshift-default
> spec:
>   controllerName: openshift.io/gateway-controller/v1
> EOF
> ```
>
> Creating this triggers the Ingress Operator to provision a lightweight Istio control plane automatically — no separate Sail Operator/OSSM install needed. The resulting Gateway-backing Service is named `<gateway-name>-openshift-default`, **not** `<gateway-name>-istio` — substitute that name in the Route step below, and see the `privateHost` callout in **Step 5**, which this naming difference directly affects.

Expose it externally with an OpenShift `Route` targeting the Istio-provisioned Service (`<gateway-name>-istio`, or `<gateway-name>-openshift-default` — see callout above), on the listener's named port:

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

> **Set the same idle-timeout override the operator applies to its own Routes.** By default, OpenShift's router (HAProxy) closes an idle backend connection after 30s, and this `Route` gets no timeout annotation from anything in this guide. A fleet client that goes quiet between MCP calls for longer than that (e.g. `kubernaut#2305`'s SignalProcessing/Gateway session drop) can have its session silently killed mid-flight, surfacing later as session-not-found errors on `tools/call` that have nothing to do with auth or routing. `kubernaut-operator` already sets `haproxy.router.openshift.io/timeout: 3600s` on its own Console/Gateway/APIFrontend Routes for this exact reason (`internal/resources/ocp.go`, `console.go`) — apply it here too, since `kubernaut-operator` doesn't manage this Route itself:
>
> ```bash
> oc annotate route mcp-gateway -n gateway-system haproxy.router.openshift.io/timeout=3600s --overwrite
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

> **Using `openshift-default` (see Step 1)? Also set `spec.privateHost`.** The broker hairpins `tools/call` requests back through the gateway using an internal address, whose computed default (`<gateway>-istio.<ns>.svc.cluster.local`, see [`mcpgatewayextension_types.go`](https://github.com/Kuadrant/mcp-gateway/blob/v0.7.1/api/v1alpha1/mcpgatewayextension_types.go)) assumes the classic self-managed-Istio Service naming convention. That default is wrong for `openshift-default`, whose Gateway-backing Service is actually named `<gateway>-openshift-default` — every `tools/call` fails with a DNS lookup error (`tools/list`/`initialize` succeed regardless, since neither exercises the hairpin path) until you override it explicitly:
>
> ```bash
> oc patch mcpgatewayextension mcp-gateway-extension -n mcp-system --type=merge \
>   -p="{\"spec\":{\"privateHost\":\"mcp-gateway-openshift-default.gateway-system.svc.cluster.local:8080\"}}"
> ```
>
> Not needed on the self-managed-Istio path — its default already matches.

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
    sts_scopes = ["<K8S_API_AUDIENCE_SCOPE>"]
    token_exchange_strategy = "rfc8693"
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

> **`kube-mcp-server` crash-loops on OIDC discovery if it doesn't trust your Keycloak/RHBK route's TLS cert.** This is common when RHBK's Route serves a cert from an internal/self-signed CA that isn't in the container's default trust store — `kube-mcp-server` fails `authorization_url`'s OIDC discovery fetch (`.well-known/openid-configuration`) at startup and never becomes Ready, with a TLS verification error in its logs. Fix: extract the CA chain, mount it as a `ConfigMap`, and point `SSL_CERT_FILE` at it:
>
> ```bash
> # Extract the serving cert chain from the Keycloak/RHBK route
> openssl s_client -connect <RHBK-ROUTE-HOST>:443 -showcerts </dev/null 2>/dev/null \
>   | sed -n '/-----BEGIN CERTIFICATE-----/,/-----END CERTIFICATE-----/p' \
>   > /tmp/keycloak-ca-bundle.crt
>
> oc create configmap keycloak-oidc-ca -n mcp-system \
>   --from-file=ca-bundle.crt=/tmp/keycloak-ca-bundle.crt
> ```
>
> Then add to the Deployment spec below: an `env` entry `SSL_CERT_FILE=/etc/keycloak-ca/ca-bundle.crt`, plus a `volumeMounts`/`volumes` pair mounting the `keycloak-oidc-ca` ConfigMap at `/etc/keycloak-ca` (read-only). Not needed if your OIDC issuer's cert already chains to a CA in the container's default trust store (e.g. a publicly-trusted CA, or one your cluster's default CA bundle already includes).

> **Do not set `sts_audience`; use `token_exchange_strategy = "rfc8693"` + `sts_scopes` instead.** Setting `sts_audience` makes kube-mcp-server send an RFC 8693 `audience` parameter on the token-exchange request, which routes RHBK/Keycloak down its *legacy* V1 token-exchange code path (`V1TokenExchangeProvider`) regardless of the `token_exchange_strategy` setting. RHBK 26.4+ defaults to Fine-Grained Admin Permissions V2 (FGAPv2), and that legacy V1 path calls `ClientPermissionsV2.canExchangeTo()`, which is `UnsupportedOperationException("Not supported in V2")` under FGAPv2 -- Keycloak throws a 500 on every exchange attempt. This is what QE hit testing kubernaut fleet management: `tools/call` failed with a Keycloak-side crash even though `tools/list` worked, because discovery doesn't exercise the exchange path but a real `tools/call` does. The fix is audience-via-scope, not audience-via-parameter: drop `sts_audience` entirely, keep only `sts_scopes` referencing a client scope with an audience mapper (as shown above), and set `token_exchange_strategy = "rfc8693"` explicitly so kube-mcp-server uses the standard RFC 8693 V2-compatible exchange. No Keycloak-side feature-flag change is needed or recommended -- this is purely a `kube-mcp-server` config fix.
>
> **Update (2026-08-26):** on the `kube-mcp-server:latest` image as of this date, the opposite was observed on a real multi-cluster deployment: *omitting* `sts_audience` failed with `invalid_client: Audience not found` (the exchanger sends an empty `audience=` parameter rather than none), and setting `sts_audience` explicitly to the bearer-only audience client (e.g. `k8s-api`) fixed it, without reproducing the V1/FGAPv2 crash above. `:latest` is a rolling tag; either behavior may be current depending on what you pull. Try omitting it first per this callout; if you hit `Audience not found` instead of the `UnsupportedOperationException` above, set it explicitly -- see [Fleet: Multi-Cluster Setup](05-fleet-multi-cluster.md#part-e-hubs-own-kube-mcp-server) for the full context.

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
    mcpGatewayNamespace: mcp-system # required -- see note below
```

`mcpGatewayNamespace` is mandatory (rejected at admission if empty) whenever `fleet.enabled` is `true`. Set it to the namespace holding your `MCPServerRegistration`/`Gateway` objects (`mcp-system` in this guide's examples). Every fleet-aware component's MCP Gateway CRD watch is scoped to exactly this namespace instead of cluster-wide, which matters if the cluster has more than one MCP Gateway installed. Leaving it unset used to fall back to a cluster-wide watch, but that fallback didn't work as documented for every consumer — see [jordigilh/kubernaut#2298](https://github.com/jordigilh/kubernaut/issues/2298) — so `kubernaut-operator` now requires it explicitly (kubernaut-operator#455).

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

### Broker logs `"transport error: authorization required"` on every connection attempt, and never recovers even after fixing the credential

```
level=ERROR msg="connection failed" component=broker sub-component=mcp-manager "upstream mcp server"=mcp-system/<name>:... error="failed to connect to upstream mcp ... : transport error: authorization required"
```

Check both of these before assuming this is the same thing as [05's "returns HTTP 500 with `authorization required`"](05-fleet-multi-cluster.md#toolscall-returns-http-500-with-authorization-required-in-broker-logs) (an expired static token, the common case):

```bash
oc get mcpgatewayextension -n mcp-system -o jsonpath='{.items[*].status.conditions[0].reason}{" "}{.items[*].status.conditions[0].message}{"\n"}'
oc get mcpserverregistration -n mcp-system
```

If the extension's reason is `DeploymentNotReady` ("broker-router deployment is not ready") and every registration's reason is `NotReady`/`"no valid mcpgatewayextensions configured"`, rotating the credential Secret **will not fix it by itself**, even if the new token is fresh. This is a self-reinforcing deadlock in Kuadrant's `MCPServerRegistration` controller (verified against `internal/controller/mcpserverregistration_controller.go` at `v0.7.1`): the controller only re-reads the `credentialRef` Secret and rewrites the broker's `config.yaml` *after* confirming a `Ready` `MCPGatewayExtension` — but the extension only becomes `Ready` once the broker Deployment passes its readiness probe, which requires its upstream connections (using whatever credential is already baked into `config.yaml`) to succeed. If a bad credential (e.g. one minted without the `Bearer ` prefix — the broker uses the `credentialRef` Secret's value verbatim as the `Authorization` header, per `internal/broker/upstream/mcp.go`) ever gets written into `config.yaml`, the loop can't self-heal: broker unhealthy → extension `NotReady` → registration reconciler exits before it reaches the code that would refresh the credential → broker stays unhealthy on the stale bad value indefinitely. This is why `kubernaut`'s own fleet E2E suite never hits it — it always provisions a correct, `Bearer `-prefixed Secret before the very first reconcile, so the extension goes `Ready` on the first try and this gate never trips.

Confirm you're in the deadlock (not just an unlucky timing race) by checking whether `config.yaml`'s `credential` field actually matches your current Secret:

```bash
oc get secret kube-mcp-server-broker-cred -n mcp-system -o jsonpath='{.data.token}' | base64 -d
oc get secret mcp-gateway-config -n mcp-system -o jsonpath='{.data.config\.yaml}' | base64 -d | grep credential
```

If the Secret has `Bearer <token>` but `config.yaml`'s `credential:` line is the bare, unprefixed token (or otherwise stale), rotating the Secret again and re-annotating the registration for `force-resync` won't help — the registration reconciler is gated out and never reaches the code that reads the Secret. Break the deadlock by patching `config.yaml` directly and forcing an immediate broker reload (don't wait on the ~60s kubelet Secret-sync mentioned above — that only helps once the controller itself is unstuck):

```bash
oc get secret mcp-gateway-config -n mcp-system -o jsonpath='{.data.config\.yaml}' | base64 -d > /tmp/mcp-gateway-config.yaml
python3 -c "
import re
with open('/tmp/mcp-gateway-config.yaml') as f:
    content = f.read()
def fix(m):
    val = m.group(1)
    return m.group(0) if val.startswith('Bearer ') else 'credential: Bearer ' + val
with open('/tmp/mcp-gateway-config-fixed.yaml', 'w') as f:
    f.write(re.sub(r'credential: (\S+)', fix, content))
"
oc create secret generic mcp-gateway-config -n mcp-system \
  --from-file=config.yaml=/tmp/mcp-gateway-config-fixed.yaml \
  --dry-run=client -o yaml | \
  oc label --local -f - app=mcp-gateway mcp.kuadrant.io/aggregated=true mcp.kuadrant.io/secret=true -o yaml | \
  oc apply -f -
oc delete pods -n mcp-system -l app.kubernetes.io/name=mcp-gateway
```

Once the broker reconnects successfully (`oc logs deployment/mcp-gateway -n mcp-system` shows `overallValid=true`), the extension flips `Ready`, which un-sticks the registration reconciler and restores normal automatic reconciliation going forward — you shouldn't need to repeat this unless a bad credential lands in `config.yaml` again.

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

### `tools/call` fails and Keycloak/RHBK logs `UnsupportedOperationException: Not supported in V2`

```
jakarta.ws.rs.InternalServerErrorException: HTTP 500 Internal Server Error
Caused by: java.lang.UnsupportedOperationException: Not supported in V2
	at org.keycloak.services.resources.admin.permissions.ClientPermissionsV2.canExchangeTo(...)
	at org.keycloak.protocol.oidc.tokenexchange.V1TokenExchangeProvider.validateAudience(...)
```

Caused by setting `sts_audience` in `kube-mcp-server`'s `config.toml` (Step 6). `sts_audience` forces the legacy Keycloak V1 token-exchange code path, which is incompatible with Fine-Grained Admin Permissions V2 (FGAPv2) -- the default in RHBK 26.4+. `tools/list`/`discover_tools`/`initialize` all succeed because none of them exercise the exchange path; only a real `tools/call` does, so this can pass every check in [Verification](#verification) up to the authenticated `tools/call` step and still fail there. Fix: remove `sts_audience` from `config.toml`, keep `sts_scopes` (pointing at a client scope with an audience mapper), and set `token_exchange_strategy = "rfc8693"` explicitly -- see the callout under **Step 6** above. Restart `kube-mcp-server` after editing the Secret (`oc rollout restart deployment/kube-mcp-server -n mcp-system`) since it only reads `config.toml` at startup.

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

Previous: [Deploy Kubernaut](03-deploy.md) | Next: [Fleet: Multi-Cluster Setup](05-fleet-multi-cluster.md)

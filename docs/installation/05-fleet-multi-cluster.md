# Fleet: Multi-Cluster Setup (RHBK + Hub/Spoke)

This guide covers the two things [Fleet: Kuadrant MCP Gateway](04-fleet-mcp-gateway.md) assumes you already have: an OIDC identity provider, and a second ("spoke") cluster for Fleet to manage. It's written from a real deployment across two OpenShift SNO clusters (OCP 4.20), each provisioned with `kcli`/libvirt on the same hypervisor.

Read this if you're setting up Fleet for the first time and don't already have RHBK, or if you're adding a second (or Nth) cluster to an existing Fleet setup. Skip it if your OIDC provider and target clusters are already wired up — go straight to [04](04-fleet-mcp-gateway.md).

## Architecture

```
                    hub cluster
  ┌───────────────────────────────────────────────────────┐
  │  RHBK (Keycloak) -- realm: kubernaut-fleet             │
  │    clients: kube-mcp-server-sts, k8s-api,               │
  │             kubernaut-fleet-read, console, oc-cli       │
  │       ▲ issues/validates tokens for  ▲                  │
  │       │                              │                  │
  │  kube-apiserver (hub)          Kuadrant Gateway/broker   │
  │  trusts RHBK as OIDC provider   (from 04-fleet-mcp-      │
  │       ▲                         gateway.md)              │
  │       │                              │                  │
  │  kube-mcp-server (hub)  <───registered as "loopback"─────┘
  └───────────────────────────────────────────────────────┘
                            │
                            │  MCPServerRegistration targets a
                            │  headless Service + EndpointSlice
                            │  pointing at the spoke node's IP
                            ▼
                    spoke cluster
  ┌───────────────────────────────────────────────────────┐
  │  kube-apiserver (spoke)                                 │
  │  ALSO trusts the SAME hub RHBK realm as OIDC provider   │
  │       ▲                                                  │
  │  kube-mcp-server (spoke), exposed via NodePort           │
  └───────────────────────────────────────────────────────┘
```

Only **one** RHBK instance exists, on the hub. Every managed cluster's `kube-apiserver` trusts that same realm as an OIDC provider (`Authentication` CR, `type: OIDC`) — this is what lets `kube-mcp-server`'s `passthrough` auth mode (see [04, Step 6](04-fleet-mcp-gateway.md#step-6-deploy-a-backend-mcp-server-kube-mcp-server)) work identically on every cluster: it exchanges the caller's token via RFC 8693 for one the *local* `kube-apiserver` accepts.

## Prerequisites

- Two (or more) OpenShift SNO clusters, **OCP 4.20+** on every cluster (the `Authentication` CR's `type: OIDC` is GA in 4.20; Tech Preview and not recommended before that — see [04's Prerequisites](04-fleet-mcp-gateway.md#prerequisites)).
- `cluster-admin` `oc` access to every cluster, plus a way to recover if you lock yourself out (see [Troubleshooting: `kubeadmin` login stops working](#kubeadmin-login-stops-working-after-switching-to-oidc)) — get a `system:admin` client-certificate kubeconfig saved somewhere *before* you touch the `Authentication` CR.
- Network reachability between the hub and every spoke's API server and `*.apps` wildcard domain. If your clusters are libvirt VMs on the same hypervisor/subnet, direct node IPs work with no extra load balancer — see [Part F1](#f1-cross-cluster-network-reachability).
- One cluster designated "hub" (runs RHBK, the Kuadrant Gateway/broker, and Kubernaut itself). Every other cluster is a "spoke" (just runs its own `kube-mcp-server`).

## Part A: Deploy RHBK on the hub cluster

### A1: Install the operator

```bash
oc new-project keycloak 2>/dev/null || true

oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: keycloak
  namespace: keycloak
spec:
  targetNamespaces: ["keycloak"]
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: rhbk-operator
  namespace: keycloak
spec:
  channel: stable-v26.6
  name: rhbk-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF

oc rollout status deployment/rhbk-operator -n keycloak --timeout=3m
```

### A2: Backing Postgres

RHBK needs its own database; the operator does not provision one for you. A minimal in-cluster Postgres is enough for a lab/fleet-test deployment:

```bash
oc apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: keycloak-db-secret
  namespace: keycloak
stringData:
  username: keycloak
  password: <CHOOSE-A-PASSWORD>
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres-kc
  namespace: keycloak
spec:
  serviceName: postgres-kc
  replicas: 1
  selector: {matchLabels: {app: postgres-kc}}
  template:
    metadata: {labels: {app: postgres-kc}}
    spec:
      containers:
      - name: postgres
        image: registry.redhat.io/rhel9/postgresql-16:latest
        env:
        - {name: POSTGRESQL_USER, valueFrom: {secretKeyRef: {name: keycloak-db-secret, key: username}}}
        - {name: POSTGRESQL_PASSWORD, valueFrom: {secretKeyRef: {name: keycloak-db-secret, key: password}}}
        - {name: POSTGRESQL_DATABASE, value: keycloak}
        ports: [{containerPort: 5432}]
        readinessProbe: {exec: {command: ["pg_isready", "-U", "keycloak"]}, periodSeconds: 5}
        resources: {requests: {cpu: 100m, memory: 256Mi}, limits: {cpu: 500m, memory: 512Mi}}
        volumeMounts: [{name: data, mountPath: /var/lib/pgsql/data}]
  volumeClaimTemplates:
  - metadata: {name: data}
    spec: {accessModes: ["ReadWriteOnce"], resources: {requests: {storage: 2Gi}}}
---
apiVersion: v1
kind: Service
metadata: {name: postgres-kc, namespace: keycloak}
spec:
  selector: {app: postgres-kc}
  ports: [{port: 5432, targetPort: 5432}]
EOF

oc rollout status statefulset/postgres-kc -n keycloak --timeout=2m
```

### A3: Keycloak CR

```bash
oc apply -f - <<EOF
apiVersion: k8s.keycloak.org/v2beta1
kind: Keycloak
metadata:
  name: keycloak
  namespace: keycloak
spec:
  instances: 1
  db:
    vendor: postgres
    host: postgres-kc
    port: 5432
    database: keycloak
    usernameSecret: {name: keycloak-db-secret, key: username}
    passwordSecret: {name: keycloak-db-secret, key: password}
  http:
    httpEnabled: true          # see Troubleshooting -- pod crash-loops without this
  hostname:
    hostname: keycloak-keycloak.apps.<HUB_CLUSTER_DOMAIN>
  proxy:
    headers: xforwarded
  ingress:
    enabled: false             # we expose via our own Route below, not the operator's Ingress
  features:
    enabled: ["token-exchange"]  # mandatory for RFC 8693 -- see Prerequisites in 04
EOF

oc wait --for=condition=Ready keycloak/keycloak -n keycloak --timeout=5m
```

Let the operator generate its own bootstrap admin — **do not** pre-create a `keycloak-initial-admin` Secret or set `spec.bootstrapAdmin` (see [Troubleshooting](#keycloak-pod-crash-loops-or-wont-start-after-first-apply)). It appears once the pod is `Ready`:

```bash
export ADMIN_USER=$(oc get secret keycloak-initial-admin -n keycloak -o jsonpath='{.data.username}' | base64 -d)
export ADMIN_PASS=$(oc get secret keycloak-initial-admin -n keycloak -o jsonpath='{.data.password}' | base64 -d)
```

### A4: Expose via Route

```bash
oc apply -f - <<EOF
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: keycloak
  namespace: keycloak
spec:
  host: keycloak-keycloak.apps.<HUB_CLUSTER_DOMAIN>
  to: {kind: Service, name: keycloak-service}
  port: {targetPort: http}
  tls: {termination: edge, insecureEdgeTerminationPolicy: Redirect}
EOF

export ISSUER_BASE="https://$(oc get route keycloak -n keycloak -o jsonpath='{.spec.host}')"
```

Every command below uses `$ISSUER_BASE`. Get an admin token (valid ~60s — re-run this before any admin API call that fails with `401`):

```bash
export ADMIN_TOKEN=$(curl -sk -X POST "$ISSUER_BASE/realms/master/protocol/openid-connect/token" \
  -d grant_type=password -d client_id=admin-cli \
  -d username="$ADMIN_USER" -d password="$ADMIN_PASS" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')
```

## Part B: Create the fleet realm and clients

```bash
export REALM=kubernaut-fleet

curl -sk -X POST "$ISSUER_BASE/admin/realms" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"realm\":\"$REALM\",\"enabled\":true,\"sslRequired\":\"external\",\"ssoSessionMaxLifespan\":432000,\"accessTokenLifespan\":300}"
```

`ssoSessionMaxLifespan` is set to 5 days here to support the long-lived CLI tokens in [Part D](#part-d-console--cli-oidc-login-for-human-operators); tighten it if that doesn't apply to you.

Every client below is created with `POST $ISSUER_BASE/admin/realms/$REALM/clients`. Helper function to keep the rest of this section short:

```bash
kc_create_client() {  # $1 = JSON body
  curl -sk -X POST "$ISSUER_BASE/admin/realms/$REALM/clients" \
    -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" -d "$1"
}
kc_client_uuid() {  # $1 = clientId
  curl -sk "$ISSUER_BASE/admin/realms/$REALM/clients?clientId=$1" \
    -H "Authorization: Bearer $ADMIN_TOKEN" | python3 -c "import json,sys;print(json.load(sys.stdin)[0]['id'])"
}
```

### B1: `k8s-api` — the bearer-only audience target

Every cluster's `Authentication` CR (Part C) validates tokens by their `aud` claim naming this client. It's `bearerOnly` because nothing ever logs in *as* this client directly — it only needs to exist so an audience mapper can reference it.

```bash
kc_create_client '{
  "clientId": "k8s-api", "protocol": "openid-connect",
  "bearerOnly": true, "publicClient": false,
  "standardFlowEnabled": true, "directAccessGrantsEnabled": true
}'

curl -sk -X POST "$ISSUER_BASE/admin/realms/$REALM/client-scopes" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"k8s-api-audience","protocol":"openid-connect"}'
K8S_API_SCOPE_ID=$(curl -sk "$ISSUER_BASE/admin/realms/$REALM/client-scopes" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | python3 -c "import json,sys;print([s['id'] for s in json.load(sys.stdin) if s['name']=='k8s-api-audience'][0])")

curl -sk -X POST "$ISSUER_BASE/admin/realms/$REALM/client-scopes/$K8S_API_SCOPE_ID/protocol-mappers/models" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" -d '{
    "name": "k8s-api-aud-mapper", "protocol": "openid-connect",
    "protocolMapper": "oidc-audience-mapper",
    "config": {"included.client.audience": "k8s-api", "id.token.claim": "false", "access.token.claim": "true"}
  }'
```

### B2: `kube-mcp-server-sts` — the token-exchange client

This is the `sts_client_id`/`sts_client_secret` in `kube-mcp-server`'s `config.toml` ([04, Step 6](04-fleet-mcp-gateway.md#step-6-deploy-a-backend-mcp-server-kube-mcp-server)). Also create its own audience scope (needed by the caller-facing clients below, and by the STS exchange itself), plus the corresponding audience mapper:

```bash
curl -sk -X POST "$ISSUER_BASE/admin/realms/$REALM/client-scopes" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"kube-mcp-server-audience","protocol":"openid-connect"}'
MCP_SCOPE_ID=$(curl -sk "$ISSUER_BASE/admin/realms/$REALM/client-scopes" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | python3 -c "import json,sys;print([s['id'] for s in json.load(sys.stdin) if s['name']=='kube-mcp-server-audience'][0])")

curl -sk -X POST "$ISSUER_BASE/admin/realms/$REALM/client-scopes/$MCP_SCOPE_ID/protocol-mappers/models" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" -d '{
    "name": "kube-mcp-server-aud-mapper", "protocol": "openid-connect", "protocolMapper": "oidc-audience-mapper",
    "config": {"included.custom.audience": "kube-mcp-server", "id.token.claim": "false", "access.token.claim": "true"}
  }'

kc_create_client '{
  "clientId": "kube-mcp-server-sts", "protocol": "openid-connect",
  "publicClient": false, "serviceAccountsEnabled": true,
  "defaultClientScopes": ["service_account", "kube-mcp-server-audience"],
  "optionalClientScopes": ["k8s-api-audience"]
}'
STS_UUID=$(kc_client_uuid kube-mcp-server-sts)
STS_SECRET=$(curl -sk "$ISSUER_BASE/admin/realms/$REALM/clients/$STS_UUID/client-secret" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["value"])')
echo "sts_client_secret: $STS_SECRET"   # goes into every cluster's kube-mcp-server config.toml
```

> **A caller's token must carry `kube-mcp-server` as an audience *before* it reaches `kube-mcp-server`, in addition to whatever audience the exchange produces.** Add a second audience mapper to the `kube-mcp-server-audience` scope targeting `kube-mcp-server-sts` itself — otherwise the exchange fails with `access_denied: Client is not within the token audience` (see [Troubleshooting](#toolscall-fails-with-access_denied-client-is-not-within-the-token-audience)):
>
> ```bash
> curl -sk -X POST "$ISSUER_BASE/admin/realms/$REALM/client-scopes/$MCP_SCOPE_ID/protocol-mappers/models" \
>   -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" -d '{
>     "name": "kube-mcp-server-sts-aud-mapper", "protocol": "openid-connect", "protocolMapper": "oidc-audience-mapper",
>     "config": {"included.client.audience": "kube-mcp-server-sts", "id.token.claim": "false", "access.token.claim": "true"}
>   }'
> ```

### B3: `kubernaut-fleet-read` — the broker's discovery credential

This is what backs the `kube-mcp-server-broker-cred` Secret in [04, Step 7](04-fleet-mcp-gateway.md#step-7-register-the-backend-with-the-broker). Give it a long-lived token since it's static (no refresh):

```bash
kc_create_client '{
  "clientId": "kubernaut-fleet-read", "protocol": "openid-connect",
  "publicClient": false, "serviceAccountsEnabled": true,
  "defaultClientScopes": ["service_account", "kube-mcp-server-audience"],
  "attributes": {"access.token.lifespan": "86400"}
}'
```

`86400` (24h) is itself capped by the realm's `accessTokenLifespan`-independent per-client override support — verify the issued token's actual `exp` and re-run [04, Step 7](04-fleet-mcp-gateway.md#step-7-register-the-backend-with-the-broker)'s token mint + Secret rotation before it expires (see [Troubleshooting](#toolscall-returns-http-500-with-authorization-required-in-broker-logs)).

## Part C: Trust the realm from every cluster's `kube-apiserver`

Repeat this on **every** cluster (hub and every spoke) that will run a `kube-mcp-server` in `passthrough` mode.

```bash
# 1. Extract RHBK's Route serving cert chain
openssl s_client -connect keycloak-keycloak.apps.<HUB_CLUSTER_DOMAIN>:443 -showcerts </dev/null 2>/dev/null \
  | sed -n '/-----BEGIN CERTIFICATE-----/,/-----END CERTIFICATE-----/p' > /tmp/rhbk-ca.crt

oc create configmap rhbk-kubernaut-fleet-ca -n openshift-config \
  --from-file=ca-bundle.crt=/tmp/rhbk-ca.crt --dry-run=client -o yaml | oc apply -f -

# 2. Console client secret (create the "console" client in Part D first if you want console login too;
#    otherwise you can omit oidcClients here and add it later)
oc create secret generic rhbk-console-client-secret -n openshift-config \
  --from-literal=clientSecret=<CONSOLE_CLIENT_SECRET> --dry-run=client -o yaml | oc apply -f -

# 3. Authentication CR
cat > /tmp/auth-oidc-patch.json <<EOF
{
  "spec": {
    "type": "OIDC",
    "webhookTokenAuthenticator": null,
    "oidcProviders": [{
      "name": "rhbk-kubernaut-fleet",
      "issuer": {
        "issuerURL": "$ISSUER_BASE/realms/$REALM",
        "audiences": ["k8s-api", "console"],
        "issuerCertificateAuthority": {"name": "rhbk-kubernaut-fleet-ca"}
      },
      "claimMappings": {
        "username": {"claim": "sub", "prefixPolicy": "Prefix", "prefix": {"prefixString": "oidc:"}},
        "groups": {"claim": "groups", "prefix": "oidc:"}
      },
      "oidcClients": [{
        "componentName": "console", "componentNamespace": "openshift-console",
        "clientID": "console", "clientSecret": {"name": "rhbk-console-client-secret"}
      }]
    }]
  }
}
EOF

oc patch authentication.config.openshift.io cluster --type=merge -p "$(cat /tmp/auth-oidc-patch.json)"
```

> **Before you run this: save a `system:admin` kubeconfig you can fall back to.** Applying this immediately invalidates any `kubeadmin`/`kube:admin` session on this cluster (see [Troubleshooting](#kubeadmin-login-stops-working-after-switching-to-oidc)) while `kube-apiserver` and `oauth-openshift` roll out the new config (5-10 min). `webhookTokenAuthenticator: null` must be set explicitly in the same patch — the API server rejects `type: OIDC` with a stale `webhookTokenAuthenticator` still set.

Wait for the rollout, then verify:

```bash
oc adm wait-for-stable-cluster --minimum-stable-period=1m 2>&1 || \
  watch oc get co kube-apiserver authentication console
```

## Part D: Console + CLI OIDC login (for human operators)

### D1: Console client

```bash
kc_create_client '{
  "clientId": "console", "protocol": "openid-connect",
  "publicClient": false, "standardFlowEnabled": true,
  "redirectUris": ["https://console-openshift-console.apps.<CLUSTER_DOMAIN>/auth/callback"],
  "defaultClientScopes": ["k8s-api-audience", "profile", "roles", "basic", "email"]
}'
CONSOLE_UUID=$(kc_client_uuid console)
curl -sk "$ISSUER_BASE/admin/realms/$REALM/clients/$CONSOLE_UUID/client-secret" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["value"])'
```

Feed that secret into Part C's `rhbk-console-client-secret` Secret on **every** cluster whose console should use it (add its own redirect URI to `redirectUris` above for each). The `basic` scope is required — see [Troubleshooting](#console-login-redirects-forever-without-ever-showing-the-console).

### D2: Create a human user (e.g. `admin`)

```bash
curl -sk -X POST "$ISSUER_BASE/admin/realms/$REALM/users" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" -d '{
    "username": "admin", "enabled": true, "firstName": "Admin", "lastName": "User",
    "credentials": [{"type": "password", "value": "admin", "temporary": false}]
  }'
```

`firstName`/`lastName` are not optional — see [Troubleshooting](#console-login-redirects-forever-without-ever-showing-the-console). Get this user's `sub` (needed for RBAC below):

```bash
export ADMIN_SUB=$(curl -sk -X POST "$ISSUER_BASE/realms/$REALM/protocol/openid-connect/token" \
  -d grant_type=password -d client_id=console -d client_secret=<CONSOLE_SECRET> \
  -d username=admin -d password=admin -d scope=openid \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])' \
  | cut -d. -f2 | python3 -c "import sys,base64,json; s=sys.stdin.read()+'=='; print(json.loads(base64.urlsafe_b64decode(s))['sub'])")
```

### D3: Grant RBAC on every cluster

```bash
oc create clusterrolebinding oidc-admin-cluster-admin \
  --clusterrole=cluster-admin --user="oidc:${ADMIN_SUB}"
```

Same `oidc:<sub>` on every cluster trusting this realm — it's the same identity everywhere.

### D4: Long-lived CLI tokens

The default OIDC token lifespan (minutes) is too short for interactive CLI sessions. Add a dedicated public client with a long-lived token, scoped narrowly to `k8s-api-audience`:

```bash
kc_create_client '{
  "clientId": "oc-cli", "protocol": "openid-connect",
  "publicClient": true, "directAccessGrantsEnabled": true,
  "defaultClientScopes": ["k8s-api-audience", "profile", "roles", "basic", "email"],
  "attributes": {"access.token.lifespan": "432000"}
}'
```

`432000` (5 days) matches the realm's `ssoSessionMaxLifespan` from Part B — a client-level lifespan longer than the realm's session cap is silently truncated to the session cap.

```bash
TOKEN=$(curl -sk -X POST "$ISSUER_BASE/realms/$REALM/protocol/openid-connect/token" \
  -d grant_type=password -d client_id=oc-cli -d username=admin -d password=admin \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')
oc login --token="$TOKEN" --server=https://api.<CLUSTER_DOMAIN>:6443
```

## Part E: Hub's own `kube-mcp-server`

Follow [04, Steps 6-7](04-fleet-mcp-gateway.md#step-6-deploy-a-backend-mcp-server-kube-mcp-server) verbatim, using:

- `authorization_url` = `$ISSUER_BASE/realms/$REALM`
- `sts_client_id`/`sts_client_secret` = the `kube-mcp-server-sts` client from Part B2
- `sts_scopes` = `["k8s-api-audience"]`
- `sts_audience` = `"k8s-api"` — **see the note below, this differs from 04's current guidance**
- `token_exchange_strategy` = `"rfc8693"`

> **On the `kube-mcp-server:latest` image as of 2026-08-26, you need `sts_audience` set, not omitted.** [04's Step 6](04-fleet-mcp-gateway.md#step-6-deploy-a-backend-mcp-server-kube-mcp-server) currently says to omit `sts_audience` (it forced a legacy, FGAPv2-incompatible Keycloak code path per [Issue #414](https://github.com/jordigilh/kubernaut-operator/issues/414)). In this deployment, omitting it produced a *different* failure — Keycloak's exchange endpoint rejected an empty `audience=` parameter with `invalid_client: Audience not found` (`kube-mcp-server`'s `rfc8693_exchanger.go` sends `audience=<cfg.Audience>` unconditionally, even when empty, rather than omitting the parameter). Setting `sts_audience = "k8s-api"` explicitly resolved it, on the current `:latest` image, without reproducing the #414 symptom. This is a rolling tag — both behaviors may be real, at different image versions. **If you hit `UnsupportedOperationException: Not supported in V2` in Keycloak logs, try omitting `sts_audience` (04's guidance); if you hit `Audience not found`, set it explicitly (this guidance).** Confirm against your actual pulled image if in doubt.

## Part F: Add a spoke cluster

### F1: Cross-cluster network reachability

You need two things working before anything else here: hub can resolve and reach spoke's API server, and vice versa (for Console OIDC redirects and any spoke-initiated calls back to hub's RHBK).

**Don't start with firewalld.** OpenShift's installer already opens the ports that matter (443, 6443) by default; in this deployment, raw TCP connectivity between hub and spoke node IPs worked from the start (verified with a quick `/dev/tcp` check from a debug pod) — the actual, and only, blocker was **DNS resolution** of the other cluster's `api`/`*.apps` hostnames.

If your clusters are libvirt VMs on the same hypervisor (e.g. via `kcli`), the simplest fix is direct node IPs — no load balancer, no extra DNS infrastructure:

```bash
# From a debug pod on hub, confirm raw reachability to spoke's node IP first
oc debug node/<hub-node> -- chroot /host bash -c \
  'curl -sk -o /dev/null -w "%{http_code}\n" --max-time 5 https://<SPOKE_NODE_IP>:6443/version'
```

If that returns an HTTP code (even 401/403), connectivity is fine and your problem is DNS, not network. Fix DNS **at the node level**, not just the cluster level:

> **Why: `dns.operator`'s `spec.servers` only fixes pod-network DNS, not `kube-apiserver`'s own DNS.** `kube-apiserver` (and any other `hostNetwork: true` static pod) resolves names via the *node's* local CoreDNS instance, configured by the static `/etc/kubernetes/Corefile` — a file the cluster's DNS Operator does not manage and `spec.servers` forwarding does not reach. If `kube-apiserver` needs to resolve the other cluster's OIDC issuer URL (it does, at every restart, to validate the `Authentication` CR's `issuerURL`) or its API server needs to be resolved by name from the other side, you must edit this file directly on **both** nodes:

```bash
oc debug node/<node> -- chroot /host bash -c 'cat /etc/kubernetes/Corefile'
```

Add (or merge into the existing) single `hosts` block — CoreDNS 1.8.x (OCP's bundled version) rejects a **second** `hosts` block in the same server stanza with `plugin/hosts: this plugin can only be used once per Server Block`, so merge entries into the one that's already there:

```
. {
    errors
    health :18080
    forward . 192.168.122.1
    cache 30
    reload
    hosts {
        <THIS_NODE_IP> <this-node-shortname> <this-node-fqdn> api-int.<this-cluster-domain> api.<this-cluster-domain>
        <OTHER_NODE_IP> api.<other-cluster-domain> api-int.<other-cluster-domain>
        fallthrough
    }
    template ANY ANY apps.<this-cluster-domain> {
       answer "{{ .Name }} A <THIS_NODE_IP>"
    }
    template ANY ANY apps.<other-cluster-domain> {
       answer "{{ .Name }} A <OTHER_NODE_IP>"
    }
}
```

Apply via `oc debug node/<node> -- chroot /host bash -c 'cat > /etc/kubernetes/Corefile <<EOF ... EOF'`, or edit directly if you have node shell access (SSH via a bastion, etc). This file hot-reloads on write (the `reload` plugin) — no service restart needed. Repeat on the other cluster's node with the roles swapped. Verify both directions:

```bash
oc debug node/<hub-node> -- chroot /host bash -c 'getent hosts api.<spoke-domain>'
oc debug node/<spoke-node> -- chroot /host bash -c 'getent hosts api.<hub-domain>'
```

Regular (non-`hostNetwork`) pods pick this up transparently too — cluster CoreDNS's default `forward . /etc/resolv.conf` points at the node's own resolver, which is this same fixed Corefile. No `dns.operator` changes needed.

### F2: Trust the hub's realm from the spoke

Run [Part C](#part-c-trust-the-realm-from-every-clusters-kube-apiserver) on the spoke cluster too, pointing at the **same** `$ISSUER_BASE`/`$REALM` (hub's RHBK — do not deploy a second RHBK instance). Reuse the same `rhbk-kubernaut-fleet-ca` ConfigMap content and `rhbk-console-client-secret` value.

### F3: Grant RBAC on the spoke

Same as [Part D3](#d3-grant-rbac-on-every-cluster), same `oidc:<sub>` — same person, same cluster-admin grant, different cluster.

### F4: Deploy `kube-mcp-server` on the spoke

Identical to [Part E](#part-e-hubs-own-kube-mcp-server) / [04, Step 6](04-fleet-mcp-gateway.md#step-6-deploy-a-backend-mcp-server-kube-mcp-server) — same `authorization_url` (hub's RHBK), same `sts_client_id`/`secret` (`kube-mcp-server-sts` — the *same* realm client works from any cluster, since the exchange is identity-preserving, not cluster-scoped). `cluster_auth_mode = "passthrough"` + `--cluster-provider=in-cluster` means this pod always targets **its own** cluster's `kube-apiserver`, regardless of which cluster it runs on — no spoke-specific config needed beyond the reused RHBK client.

Expose it so hub can reach it. If you're on the direct-node-IP path from F1, a `NodePort` Service is enough:

```bash
oc patch svc kube-mcp-server -n mcp-system -p '{"spec":{"type":"NodePort"}}'
export SPOKE_NODEPORT=$(oc get svc kube-mcp-server -n mcp-system -o jsonpath='{.spec.ports[0].nodePort}')
```

### F5: Register the spoke with hub's broker

`MCPServerRegistration.spec.targetRef` only accepts an `HTTPRoute` in the **same cluster** as the broker — there's no "remote URL" field (verify against your installed CRD: `oc explain mcpserverregistration.spec.targetRef`). Since the spoke's `kube-mcp-server` is genuinely external to the hub cluster, federate it with a headless `Service` + manually-pinned `EndpointSlice` pointing at the spoke node's IP:port, then a normal `HTTPRoute`/`MCPServerRegistration` on top of that — no custom Gateway API extensions needed.

Run this on the **hub** cluster:

```bash
oc apply -n mcp-system -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: kube-mcp-server-spoke
spec:
  ports: [{port: 8080, targetPort: 8080, protocol: TCP}]
---
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: kube-mcp-server-spoke
  labels:
    kubernetes.io/service-name: kube-mcp-server-spoke
addressType: IPv4
ports:
- {protocol: TCP, port: ${SPOKE_NODEPORT}}
endpoints:
- addresses: ["<SPOKE_NODE_IP>"]
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: kube-mcp-server-spoke-route
spec:
  parentRefs:
  - {name: mcp-gateway, namespace: gateway-system, sectionName: mcp}
  hostnames: ["kube-mcp-server-spoke.\${MCP_GATEWAY_HOST}"]
  rules:
  - matches: [{path: {type: PathPrefix, value: /}}]
    backendRefs: [{name: kube-mcp-server-spoke, port: 8080}]
---
apiVersion: mcp.kuadrant.io/v1alpha1
kind: MCPServerRegistration
metadata:
  name: spoke
  labels:
    kubernaut.ai/managed: "true"   # mandatory -- see 04's callout under Step 7
    environment: "production"       # or whatever your Rego policies key on
spec:
  prefix: spoke_
  targetRef: {group: gateway.networking.k8s.io, kind: HTTPRoute, name: kube-mcp-server-spoke-route}
  credentialRef: {name: kube-mcp-server-broker-cred, key: token}   # reuse hub's own broker credential -- same realm, same STS
EOF
```

`kube-mcp-server-spoke-route` needs a **distinct hostname** from the hub's own `kube-mcp-server-route` — see [Troubleshooting](#tools-appear-in-toolslist-but-toolscall-fails-with-tool--not-found-after-adding-a-second-registration) for why reusing the same hostname across two `HTTPRoute`s breaks the broker's routing.

### F6: Verify

```bash
oc get mcpserverregistration -n mcp-system
# Expect spoke READY=True with a non-zero TOOLS count, same as loopback/hub's own registration
```

Confirm it's actually hitting the spoke, not the hub, by calling a tool whose output differs between clusters (any resource that exists on one but not the other):

```bash
# after the initialize handshake from 04's Verification section, using the spoke_ prefix
curl ... -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"spoke_namespaces_list","arguments":{}}}'
# Expect namespaces that exist only on the spoke cluster
```

F6 only proves read-only tool calls work. Real remediation needs two more things, below — without them every fleet tool call is a 403 (F7), or a remediation Job dispatched to a spoke fails with "namespace not found" (F8).

### F7: Grant RBAC to the exchanged fleet-caller identity

RFC 8693 token exchange preserves the *original* caller's `sub` claim — the identity `kube-apiserver` sees is not `kube-mcp-server-sts`'s own identity, it's whichever fleet-aware component's client (e.g. `kubernaut-fleet-read`) minted the original token. Until you grant RBAC to that exact `oidc:<sub>`, every fleet tool call 403s on every cluster, including the hub. This is separate from, and happens after, the `kube-mcp-server` ServiceAccount/ClusterRoleBinding in [04, Step 6](04-fleet-mcp-gateway.md#step-6-deploy-a-backend-mcp-server-kube-mcp-server) — that binding only covers the pod's own probes, not the RFC 8693-exchanged caller identity the target API server actually authorizes against.

Find the identity by reproducing `kube-mcp-server`'s own exchange manually, using the same clients from Part B:

```bash
# 1. Mint the caller's own token (same client_credentials grant FMC/Gateway/RO use)
FLEET_READ_UUID=$(kc_client_uuid kubernaut-fleet-read)
FLEET_READ_SECRET=$(curl -sk "$ISSUER_BASE/admin/realms/$REALM/clients/$FLEET_READ_UUID/client-secret" \
  -H "Authorization: Bearer $ADMIN_TOKEN" | python3 -c 'import json,sys;print(json.load(sys.stdin)["value"])')
CALLER_TOKEN=$(curl -sk -X POST "$ISSUER_BASE/realms/$REALM/protocol/openid-connect/token" \
  -d grant_type=client_credentials -d client_id=kubernaut-fleet-read -d client_secret=$FLEET_READ_SECRET \
  -d scope=kube-mcp-server-audience | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')

# 2. Exchange it the same way kube-mcp-server does internally (RFC 8693, via the STS client from B2)
EXCHANGED_TOKEN=$(curl -sk -X POST "$ISSUER_BASE/realms/$REALM/protocol/openid-connect/token" \
  -d grant_type=urn:ietf:params:oauth:grant-type:token-exchange \
  -d client_id=kube-mcp-server-sts -d client_secret=$STS_SECRET \
  -d subject_token=$CALLER_TOKEN -d scope=k8s-api-audience \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')

# 3. Decode the exchanged JWT's "sub" claim -- this is the identity kube-apiserver sees
export FLEET_CALLER_SUB=$(python3 -c "
import base64, json
payload = '$EXCHANGED_TOKEN'.split('.')[1]
payload += '=' * (-len(payload) % 4)
print(json.loads(base64.urlsafe_b64decode(payload))['sub'])
")
```

`view` alone isn't enough — WorkflowExecution dispatching a remediation Job to a remote cluster additionally needs `batch/jobs` create/get/list/watch/delete/patch/update. Apply both **on every cluster in the fleet** (hub and every spoke) using the same `$FLEET_CALLER_SUB` everywhere — token exchange preserves identity, so it's the same `oidc:<sub>` regardless of which cluster the exchange targets:

```bash
oc apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubernaut-fleet-job-write
rules:
- apiGroups: ["batch"]
  resources: ["jobs"]
  verbs: ["create", "get", "list", "watch", "delete", "patch", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubernaut-fleet-read-job-write
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kubernaut-fleet-job-write}
subjects:
- {kind: User, name: "oidc:${FLEET_CALLER_SUB}", apiGroup: rbac.authorization.k8s.io}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubernaut-fleet-read-view
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: view}
subjects:
- {kind: User, name: "oidc:${FLEET_CALLER_SUB}", apiGroup: rbac.authorization.k8s.io}
EOF
```

`view` also isn't enough for `FleetMetadataCache` (FMC): it lists `nodes` on every registered cluster to build per-cluster metadata (capacity, labels, taints), and the default `view` `ClusterRole` deliberately excludes cluster-scoped resources like `nodes`. Without this, FMC reports `clusters: 0` for every cluster even though the MCP Gateway registration itself is healthy — the only symptom is silence, no error surfaced anywhere obvious. Apply on every cluster in the fleet, same as above:

```bash
oc apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubernaut-fleet-node-reader
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubernaut-fleet-read-node-reader
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kubernaut-fleet-node-reader}
subjects:
- {kind: User, name: "oidc:${FLEET_CALLER_SUB}", apiGroup: rbac.authorization.k8s.io}
EOF
```

### F8: Replicate the workflow-execution namespace

`kubernaut-operator` only provisions the `kubernaut-workflows` namespace and its `kubernaut-workflow-runner` ServiceAccount/RBAC on the cluster it's installed on (the hub) — see `deployWorkflowNamespace`/`workflowRunnerClusterRole`/`WorkflowNamespaceRBAC` in `internal/controller/kubernaut_controller.go` and `internal/resources/rbac.go`. When `RemediationRequest.ClusterID` routes a remediation Job to a spoke, that namespace and its RBAC must already exist there — nothing creates it remotely. First symptom is the spawned Job failing with `namespace not found`.

Replicate it manually on every spoke. `<HUB_NS>` below is whatever namespace the operator runs in on the **hub** (e.g. `kubernaut-system`) — the ClusterRole name is namespace-prefixed there to avoid collisions if you ever run more than one Kubernaut instance, so it must match exactly:

```bash
export HUB_NS=kubernaut-system   # namespace the OPERATOR runs in on the hub
oc apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: kubernaut-workflows
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kubernaut-workflow-runner
  namespace: kubernaut-workflows
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ${HUB_NS}-workflow-runner
rules:
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list", "patch", "update"]
- apiGroups: ["apps"]
  resources: ["replicasets"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "delete", "watch"]
- apiGroups: [""]
  resources: ["pods/eviction"]
  verbs: ["create"]
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "patch", "update"]
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list"]
- apiGroups: ["policy"]
  resources: ["poddisruptionbudgets"]
  verbs: ["get", "list", "patch"]
- apiGroups: ["autoscaling"]
  resources: ["horizontalpodautoscalers"]
  verbs: ["get", "list", "patch"]
- apiGroups: [""]
  resources: ["serviceaccounts/token"]
  verbs: ["create"]
- apiGroups: ["storage.k8s.io"]
  resources: ["storageclasses"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["endpoints"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${HUB_NS}-workflow-runner-binding
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: ${HUB_NS}-workflow-runner}
subjects:
- {kind: ServiceAccount, name: kubernaut-workflow-runner, namespace: kubernaut-workflows}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: workflow-runner-ns-writer
  namespace: kubernaut-workflows
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "list", "create", "delete", "patch", "update"]
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "create", "update", "patch"]
- apiGroups: [""]
  resources: ["services"]
  verbs: ["get", "list", "create", "update", "patch"]
- apiGroups: [""]
  resources: ["persistentvolumeclaims"]
  verbs: ["get", "list", "create", "update", "patch", "delete"]
- apiGroups: ["networking.k8s.io"]
  resources: ["networkpolicies"]
  verbs: ["get", "list", "create", "update", "patch", "delete"]
- apiGroups: ["batch"]
  resources: ["jobs"]
  verbs: ["get", "list", "create", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: workflow-runner-ns-writer-binding
  namespace: kubernaut-workflows
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: workflow-runner-ns-writer}
subjects:
- {kind: ServiceAccount, name: kubernaut-workflow-runner, namespace: kubernaut-workflows}
EOF
```

This intentionally omits a few rules from the hub's own `workflowRunnerClusterRole()` that only matter to controllers running on the hub itself (`argoproj.io` Applications, `cert-manager.io` Certificates/ClusterIssuers, `kubernaut.ai` WorkflowExecutions, the Istio/Linkerd mesh-policy rules) — add the matching rule from `internal/resources/rbac.go` if a remediation workflow on the spoke actually needs one of those.

If `spec.workflowExecution.workflowNamespace` overrides the default `kubernaut-workflows` name on the hub CR, use that same name here instead of `kubernaut-workflows`.

## Wiring into the Kubernaut CR

Once RHBK, the Gateway/broker, every cluster registration, and RBAC (F7/F8) on every cluster are verified, continue to [04's "Wiring into the Kubernaut CR"](04-fleet-mcp-gateway.md#wiring-into-the-kubernaut-cr) on the hub — that's where `spec.fleet.enabled`/`oauth2` get set.

## Troubleshooting

### Keycloak pod crash-loops or won't start after first apply

Two independent causes, both from over-specifying the Keycloak CR on first install:

- **`bootstrapAdmin` conflict**: if you (or a previous attempt) manually created a `keycloak-initial-admin` Secret *and* the CR also sets `spec.bootstrapAdmin`, the operator refuses to reconcile. Delete the manually-created Secret and drop `bootstrapAdmin` from the CR entirely — let the operator generate and own that Secret (Part A3 already does this correctly).
- **Missing `http.httpEnabled`**: with `features.enabled: ["token-exchange"]` set but no explicit `http: {httpEnabled: true}`, the pod starts in HTTPS-only mode with no cert configured and crashes immediately. Part A3's CR sets this correctly; if you inherited a CR without it, patch it in.

### `401` calling the Keycloak admin API mid-script

Admin tokens are short-lived (~60s default). If you're running the Part B commands interactively rather than as one script, re-mint `$ADMIN_TOKEN` before continuing:

```bash
ADMIN_TOKEN=$(curl -sk -X POST "$ISSUER_BASE/realms/master/protocol/openid-connect/token" \
  -d grant_type=password -d client_id=admin-cli -d username="$ADMIN_USER" -d password="$ADMIN_PASS" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')
```

### `kube-mcp-server` running but ignoring its config (no OAuth enforcement, or wrong hostname)

Check the Deployment actually has **both** the `--config=/etc/kubernetes-mcp-server/config.toml` arg **and** the matching `volumeMounts`/`volumes` for the config Secret. It's easy to update the Secret's contents and expect a restart to pick it up, while forgetting the pod spec itself never wires the file in — the process silently runs with defaults (`require_oauth` effectively off) instead of erroring.

```bash
oc get deployment kube-mcp-server -n mcp-system -o jsonpath='{.spec.template.spec.containers[0].args}'
# must include --config=/etc/kubernetes-mcp-server/config.toml
```

### `tools/call` returns HTTP 500 with `"authorization required"` in broker logs

The broker's own discovery/session credential (`kube-mcp-server-broker-cred`, [Part B3](#b3-kubernaut-fleet-read--the-brokers-discovery-credential)) has expired — it's a static token with no refresh. Symptoms: `tools/list` may still work (served from a cached catalog) but a real `tools/call` fails, and the broker's own connection logs show `authorization required` talking to the backend.

Fix: mint a fresh token and force the controller to pick it up (it caches aggressively):

```bash
BROKER_TOKEN=$(curl -sk -X POST "$ISSUER_BASE/realms/$REALM/protocol/openid-connect/token" \
  -d grant_type=client_credentials -d client_id=kubernaut-fleet-read -d client_secret=<SECRET> \
  -d scope=kube-mcp-server-audience | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')
oc delete secret kube-mcp-server-config kube-mcp-server-broker-cred -n mcp-system --ignore-not-found
oc create secret generic kube-mcp-server-broker-cred -n mcp-system \
  --from-literal=token="Bearer ${BROKER_TOKEN}" --dry-run=client -o yaml | \
  oc label --local -f - mcp.kuadrant.io/secret=true -o yaml | oc apply -f -
oc annotate mcpserverregistration <name> -n mcp-system force-resync="$(date +%s)" --overwrite
```

If the underlying issue keeps recurring, increase the client's `access.token.lifespan` ([Part B3](#b3-kubernaut-fleet-read--the-brokers-discovery-credential)) rather than rotating manually on a schedule.

**If the rotation above doesn't clear the error** (`mcpserverregistration` stays `NotReady`, `mcpgatewayextension` reports `DeploymentNotReady`/"broker-router deployment is not ready"), don't keep re-rotating the token — this is a different, unrecoverable-without-manual-intervention failure mode, not an expired credential. A credential minted without the `Bearer ` prefix (e.g. a copy-paste of just `$BROKER_TOKEN` instead of `"Bearer ${BROKER_TOKEN}"` above) can wedge the broker/controller into a deadlock that a normal re-rotation can't escape. See [04's troubleshooting entry](04-fleet-mcp-gateway.md#broker-logs-transport-error-authorization-required-on-every-connection-attempt-and-never-recovers-even-after-fixing-the-credential) for how to confirm you're in that state and the direct-patch recovery procedure.

### `tools/call` fails with `invalid_client: Audience not found`

See the callout under [Part E](#part-e-hubs-own-kube-mcp-server) — set `sts_audience` explicitly in `kube-mcp-server`'s `config.toml` rather than omitting it, on current `kube-mcp-server:latest` images. Restart the deployment after editing the Secret (it only reads `config.toml` at startup):

```bash
oc rollout restart deployment/kube-mcp-server -n mcp-system
```

### `tools/call` fails with `access_denied: Client is not within the token audience`

The caller's token carries an audience list that doesn't include the STS client itself. Add the `kube-mcp-server-sts` client to the `kube-mcp-server-audience` scope's audience mappers — see the callout at the end of [Part B2](#b2-kube-mcp-server-sts--the-token-exchange-client).

### Tools appear in `tools/list` but `tools/call` fails with `"tool ... not found"`, after adding a second registration

If you have more than one `MCPServerRegistration` (e.g. hub's own `loopback` plus a `spoke`), **every backing `HTTPRoute` must have a distinct hostname**. Collapsing them onto the same hostname (e.g. because it seemed simpler to reuse the Gateway's public hostname everywhere) breaks the broker's internal routing disambiguation between backends — tool discovery still works (it's served from a merged catalog), but `tools/call` can no longer tell which upstream a given tool belongs to. Give each `HTTPRoute` its own hostname (e.g. `kube-mcp-server-spoke.<gateway-host>` vs. the gateway's own bare hostname) and re-verify.

This is a different failure mode from [04's own version of this symptom](04-fleet-mcp-gateway.md#tools-appear-in-toolslistdiscover_tools-but-toolscall-fails-with-tool--not-found) (which is about `cluster_auth_mode`/E2E coverage gaps) — check hostnames first if you have multiple registrations, before chasing the auth-mode angle.

### `kube-apiserver` OIDC init fails with `dial tcp ...: connect: no route to host` for the issuer URL

`kube-apiserver` couldn't resolve or reach the OIDC issuer URL from [Part C](#part-c-trust-the-realm-from-every-clusters-kube-apiserver) at startup. This is almost always the node-level DNS issue from [Part F1](#f1-cross-cluster-network-reachability) — even on the hub cluster itself, since `kube-apiserver` re-resolves the issuer URL on every restart, not just once. Fix the static `/etc/kubernetes/Corefile` on the affected node as described there; a `dns.operator` CR change alone will not fix this.

### `kubeadmin` login stops working after switching to OIDC

Expected. `Authentication` CR `type: OIDC` fully replaces the integrated OAuth server — `kubeadmin`/`kube:admin` sessions and new logins via that path stop working the moment the new config rolls out, cluster-wide, immediately. This is not a bug to fix; it's the intended effect of the switch. Recovery paths:

- **Planned**: before switching, save a `system:admin` client-certificate kubeconfig (`/etc/kubernetes/static-pod-resources/kube-apiserver-certs/secrets/node-kubeconfigs/localhost.kubeconfig` on a control-plane node, or your install-time `kubeconfig` if you kept the original client cert). This bypasses OIDC entirely and always works.
- **Reverting**: patch `Authentication` back:
  ```bash
  oc patch authentication.config.openshift.io cluster --type=merge -p '{
    "spec": {"type": "IntegratedOAuth", "oidcProviders": null,
             "webhookTokenAuthenticator": {"kubeConfig": {"name": "webhook-authentication-integrated-oauth"}}}
  }'
  ```
  Wait for `kube-apiserver`/`authentication`/`console` cluster operators to finish rolling out before assuming `kubeadmin` works again — a `404` on `.well-known/oauth-authorization-server` right after reverting is the operators still mid-rollout, not a permanent failure.

### Console login redirects forever without ever showing the console

Three independent causes, all producing the same symptom (bounce back to Keycloak, or a blank/looping redirect):

1. **User missing `firstName`/`lastName`**: Keycloak's account-setup validation blocks the token issuance path the console needs (`"Account is not fully set up"` on a direct password-grant test). Fix: set both attributes on the user ([Part D2](#d2-create-a-human-user-eg-admin)).
2. **`console` client missing the `basic` scope**: without it, the issued token has no `sub` claim, and `kube-apiserver` can't map a username — console logs show `isKubeAdmin`/`canGetNamespaces` failing as `Unauthorized`. Add `basic` to the client's default scopes.
3. **`kube-apiserver` rejects the console's ID token audience**: the console sends its **ID token** (audience `console`) to `kube-apiserver` for whoami-style calls, not the access token (audience `k8s-api`). If `Authentication.spec.oidcProviders[0].issuer.audiences` only lists `k8s-api` (as you'd naturally do for the fleet-only use case), `kube-apiserver` rejects it with `oidc: expected audience "k8s-api" got ["console"]`. Fix: list **both** audiences, as Part C's example already does: `"audiences": ["k8s-api", "console"]`. This triggers another `kube-apiserver` rollout — wait for it.

Diagnose which one you're hitting from the console pod logs (`oc logs -n openshift-console deployment/console`) rather than guessing — the three failure messages are distinct.

### `oc login` with a Keycloak token says "The token provided is invalid or expired"

The token's audience doesn't include `k8s-api`. This happens when logging in with a token from a client that never had `k8s-api-audience` as a scope (e.g. `admin-cli`). Use the dedicated `oc-cli` client from [Part D4](#d4-long-lived-cli-tokens), which has it by default, rather than a generic/admin client.

### CLI token expires after a few minutes despite requesting a longer lifespan

Two settings gate this independently and both must be raised — a client-level `access.token.lifespan` longer than the realm's `ssoSessionMaxLifespan` is silently capped at the realm value:

```bash
# realm-level cap
curl -sk -X PUT "$ISSUER_BASE/admin/realms/$REALM" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" -d '{"ssoSessionMaxLifespan": 432000}'
# client-level request
# (attributes.\"access.token.lifespan\" on the oc-cli client, Part D4)
```

### CoreDNS `reload` error: `plugin/hosts: this plugin can only be used once per Server Block`

You (or an automated patch) added a *second* `hosts {}` block to the static `/etc/kubernetes/Corefile` instead of merging into the existing one. OCP's bundled CoreDNS (1.8.x) enforces one `hosts` plugin per server block. Merge all entries into the single existing block — see [Part F1](#f1-cross-cluster-network-reachability)'s example.

### Everything above is fine, but a spoke's alerts/remediations are never classified

Not specific to multi-cluster — see [04's own troubleshooting entry](04-fleet-mcp-gateway.md#a-registered-clusters-alertsremediations-are-silently-ignored) for the `kubernaut.ai/managed` label requirement, which applies identically to a `spoke` registration.

---

Previous: [Fleet: Kuadrant MCP Gateway](04-fleet-mcp-gateway.md)

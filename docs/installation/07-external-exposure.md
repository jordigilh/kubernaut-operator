# External Exposure (Administrator-Owned)

The operator deliberately does not create or manage external exposure
resources. This is a security boundary, not a missing feature: making a
workload externally reachable is a platform decision that belongs to
cluster administrators or their GitOps controllers — not a side effect of
applying a `Kubernaut` CR. The operator's manager RBAC grants no
permissions on Route, Ingress, or Gateway API exposure resources (it only
retains read-only access to OpenShift cluster configuration objects), and
the operator never creates, adopts, updates, or deletes those resources.

The design decision and its rationale are recorded in
[DD-565: Administrator-owned external exposure boundary](../design/DD-565-EXTERNAL-EXPOSURE-BOUNDARY.md).

This document shows how an administrator exposes the operator-managed
internal Services on OpenShift (Route) and on generic Kubernetes/Kind
(Ingress). Everything here is applied by you or your GitOps controller in
the Kubernaut namespace; the operator observes nothing and cleans up
nothing — your exposure objects survive operator reinstall and uninstall.

## What the operator manages

The operator deploys internal `ClusterIP` Services with HTTPS
service-internal TLS (or HTTP for the Console proxy). The ones you
typically expose:

| Component | Service name | Port (name) | Backend protocol |
|---|---|---|---|
| Gateway (main API) | `gateway-service` | `8443` (`https`) | HTTPS (service-internal CA) |
| API Frontend | `apifrontend` | `8443` (`https`) | HTTPS (service-internal CA) |
| Console | `kubernaut-console` | `4180` (`http`) | HTTP (OAuth2 proxy) |

The Gateway's deterministic internal URL stays as-is regardless of
exposure: `https://gateway-service.<namespace>.svc.cluster.local:8443`.

## OpenShift: Route

Because the Gateway and API Frontend backends serve HTTPS with the
inter-service CA, use `reencrypt` termination and trust the destination
with the CA the operator provisioned (for service-CA mode this is the
OpenShift service-signing CA, available via the `service-ca.crt` key of the
`inter-service-ca` ConfigMap):

> **Authorization prerequisite:** with default settings, Gateway
> authentication is disabled, and a Route adds no edge authorization on its
> own. Any client that can reach the Route can then submit requests to the
> Gateway — including starting remediation workflows — without credentials.
> Before applying the Route, put one of these in place:
>
> - an administrator-controlled edge authorization policy in front of the
>   Route, or
> - Gateway authentication enabled (`spec.gateway.auth.enabled: true`, see
>   [Configure Services](02-configure-services.md)).

```bash
oc apply -n kubernaut-system -f - <<'EOF'
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: kubernaut-gateway
spec:
  to:
    kind: Service
    name: gateway-service
  port:
    targetPort: https
  tls:
    termination: reencrypt
    destinationCACertificate: |
      -----BEGIN CERTIFICATE-----
      ... service-internal CA that signed gateway-tls ...
      -----END CERTIFICATE-----
EOF
```

**CA rotation:** the OpenShift service CA rotates before its 26-month
expiry. After it does, the backend presents certificates signed by the new
CA and this Route fails backend TLS validation until the copied PEM is
refreshed. Read the current CA and re-apply the Route above with it as the
`destinationCACertificate`:

```bash
oc get configmap inter-service-ca -n kubernaut-system \
  -o jsonpath='{.data.service-ca\.crt}' > current-service-ca.pem
```

Avoid `passthrough` for these Services: the backend certificates are only
valid for the internal Service DNS names (`gateway-service.<namespace>.svc`
and friends), so external clients would fail hostname validation against
the Route hostname. If you need passthrough, add the external hostname to
the backend certificate SANs first. Use one of these sources:

- cert-manager provisioning: enable provisioning and set
  `spec.tls.certManager.provisioning.extraSANs`.
- manual TLS: set `spec.tls.mode: manual` and supply the serving Secret
  named in `spec.tls.manual.serviceTLSSecretNames`. The operator validates
  but never modifies these Secrets; the certificate must include the
  external hostname as a SAN alongside the internal Service DNS names.

Do **not** use `edge` termination for `gateway-service` or `apifrontend`:
those backends speak TLS on their `https` port, so a plain-HTTP edge hop
will fail.

**NetworkPolicy prerequisite:** by default the operator's Gateway and API
Frontend NetworkPolicies admit ingress only from pods of the same
Kubernaut instance. An OpenShift router or an ingress controller running
in another namespace is not admitted, so where the CNI enforces
NetworkPolicy the exposure below cannot reach `gateway-service` or
`apifrontend` until you authorize the controller. Add the controller's
namespace via the CR override — for example
`spec.networkPolicies.gateway.ingressNamespaces: ["openshift-ingress"]`
(and the matching `spec.networkPolicies.apifrontend` entry if you expose
it) — or use `ingressCIDRs` for a hostNetwork-mode controller. Confirm
the controller can reach the Service before relying on the exposure. The
Console has no operator-managed NetworkPolicy and needs no override.

The Console proxy speaks HTTP on `4180`, so terminate TLS at the Route with
`edge` termination targeting the `http` port. Do **not** use a plain
(non-TLS) Route: the Console OAuth2 callback (`spec.console.auth.redirectURL`)
must be an HTTPS URL, and a plain Route cannot serve it.

```bash
oc apply -n kubernaut-system -f - <<'EOF'
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: kubernaut-console
spec:
  to:
    kind: Service
    name: kubernaut-console
  port:
    targetPort: http
  tls:
    termination: edge
EOF
```

Then set the Console OAuth2 callback to the Route hostname — the operator
never derives it from the cluster ingress domain:

```yaml
spec:
  console:
    auth:
      redirectURL: https://kubernaut-console-kubernaut-system.<cluster-domain>/oauth2/callback
```

See [Configure Services](02-configure-services.md) for the full Console
auth contract.

## Generic Kubernetes / Kind: Ingress

With an nginx-class ingress controller, terminate TLS at the ingress and
proxy to the HTTPS backends. The example verifies the backend certificate
against the service-internal CA by default. First create the CA Secret the
verification reads — the ingress controller requires it in the Ingress
namespace:

```bash
kubectl get configmap inter-service-ca -n kubernaut-system \
  -o jsonpath='{.data.service-ca\.crt}' > current-service-ca.pem
kubectl create secret generic kubernaut-backend-ca -n kubernaut-system \
  --from-file=ca.crt=current-service-ca.pem
```

Then apply the Ingress:

```bash
kubectl apply -n kubernaut-system -f - <<'EOF'
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: kubernaut-gateway
  annotations:
    # gateway-service and apifrontend serve HTTPS with the service-internal CA
    nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"
    # verify the backend certificate: the Secret above supplies the CA as
    # ca.crt, and proxy-ssl-name must match a backend cert SAN
    nginx.ingress.kubernetes.io/proxy-ssl-verify: "on"
    nginx.ingress.kubernetes.io/proxy-ssl-secret: kubernaut-system/kubernaut-backend-ca
    nginx.ingress.kubernetes.io/proxy-ssl-server-name: "on"
    nginx.ingress.kubernetes.io/proxy-ssl-name: gateway-service.kubernaut-system.svc
spec:
  ingressClassName: nginx
  rules:
  - host: kubernaut-gateway.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: gateway-service
            port:
              number: 8443
  tls:
  - hosts: [kubernaut-gateway.example.com]
    secretName: kubernaut-gateway-tls   # administrator-owned edge certificate
EOF
```

The Console needs no `backend-protocol` or `proxy-ssl-*` annotations (it
is HTTP on `4180`) — point an Ingress at `kubernaut-console` / `4180` and
mirror the external HTTPS URL in `spec.console.auth.redirectURL`, exactly
as above.

Gateway API is equally supported as an administrator-owned path
(`GatewayClass`/`Gateway`/`HTTPRoute`); see
[Fleet: Kuadrant MCP Gateway](04-fleet-mcp-gateway.md) for a worked
Gateway API + Route example on OpenShift. The NetworkPolicy prerequisite
above applies to the generic Ingress path the same way — authorize your
ingress controller's namespace before relying on it.

## Rules the operator holds regardless of your choice

- Hostnames, ingress classes, edge certificates, and edge authorization
  are yours. The operator never infers or rewrites them.
- `spec.console.auth.redirectURL` is the only place an external URL enters
  the CR, and it must be an absolute HTTPS URL whose path is exactly
  `/oauth2/callback` — no query string, fragment, or userinfo, and a
  sub-path like `https://example.com/console/oauth2/callback` is
  rejected.
- Status reports exposure as `ExposureReady=True` with reason
  `InternalOnly`: a statement of the operator's exposure boundary — its
  managed Services are deployed as internal ClusterIPs and external
  exposure is administrator-owned. It is not a live check of Service
  types, and the operator does not monitor your Route, Ingress, or
  Gateway objects.
- Do not use Helm and the operator to manage the same exposure objects.

# External Exposure (Administrator-Owned)

The operator deliberately does not create or manage external exposure
resources. This is a security boundary, not a missing feature: making a
workload externally reachable is a platform decision that belongs to
cluster administrators or their GitOps controllers — not a side effect of
applying a `Kubernaut` CR. The operator's manager RBAC grants no
permissions on Route, Ingress, or Gateway API exposure resources (it only
retains read-only access to OpenShift's cluster Ingress configuration
object), and the operator never creates, adopts, updates, or deletes
those resources.

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
| Console | `console` | `4180` (`http`) | HTTP (OAuth2 proxy) |

The Gateway's deterministic internal URL stays as-is regardless of
exposure: `https://gateway-service.<namespace>.svc.cluster.local:8443`.

## OpenShift: Route

Because the Gateway and API Frontend backends serve HTTPS with the
inter-service CA, use `reencrypt` termination and trust the destination
with the CA the operator provisioned (for service-CA mode this is the
OpenShift service-signing CA, available via the `service-ca.crt` key of the
`inter-service-ca` ConfigMap, or use `passthrough`):

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

Avoid `passthrough` for these Services: the backend certificates are only
valid for the internal Service DNS names (`gateway-service.<namespace>.svc`
and friends), so external clients would fail hostname validation against
the Route hostname. If you need passthrough, add the external hostname to
the backend certificate SANs first (`spec.tls.certManager.extraSANs`).

Do **not** use `edge` termination for `gateway-service` or `apifrontend`:
those backends speak TLS on their `https` port, so a plain-HTTP edge hop
will fail.

The Console proxy speaks HTTP on `4180`, so a plain (non-TLS) Route or edge
termination targeting `http` is correct:

```bash
oc apply -n kubernaut-system -f - <<'EOF'
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: kubernaut-console
spec:
  to:
    kind: Service
    name: console
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
proxy to the HTTPS backends. Adjust class, annotations, and certificate
source (for example cert-manager) to your cluster:

```bash
kubectl apply -n kubernaut-system -f - <<'EOF'
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: kubernaut-gateway
  annotations:
    # gateway-service and apifrontend serve HTTPS with the service-internal CA
    nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"
    # nginx does not verify the backend certificate by default. To harden,
    # trust the service-internal CA with proxy-ssl-verify: "true" plus
    # proxy-ssl-secret, proxy-ssl-server-name, and a proxy-ssl-name matching
    # the backend cert SANs (gateway-service.kubernaut-system.svc).
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

The Console needs no `backend-protocol` annotation (it is HTTP on `4180`)
— point an Ingress at `console` / `4180` and mirror the external HTTPS URL
in `spec.console.auth.redirectURL`, exactly as above.

Gateway API is equally supported as an administrator-owned path
(`GatewayClass`/`Gateway`/`HTTPRoute`); see
[Fleet: Kuadrant MCP Gateway](04-fleet-mcp-gateway.md) for a worked
Gateway API + Route example on OpenShift.

## Rules the operator holds regardless of your choice

- Hostnames, ingress classes, edge certificates, and edge authorization
  are yours. The operator never infers or rewrites them.
- `spec.console.auth.redirectURL` is the only place an external URL enters
  the CR, and it must be an explicit HTTPS URL ending in
  `/oauth2/callback`.
- Status reports exposure as `ExposureReady=True` with reason
  `InternalOnly`: the operator confirms its own Services remain internal
  and exposure is administrator-owned. It does not monitor your Route,
  Ingress, or Gateway objects.
- Do not use Helm and the operator to manage the same exposure objects.

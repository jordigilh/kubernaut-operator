# Kubernaut operator bootstrap chart

This chart installs the Kubernaut operator and its bootstrap prerequisites. It
does **not** install a `Kubernaut` custom resource, application workloads,
databases, provider operators, provider CRDs, or runtime application CRDs.

## Install

The default `development` TLS profile is intended only for Kind and CI:

```bash
helm install kubernaut-operator ./charts/kubernaut-operator \
  --namespace kubernaut-operator-system \
  --create-namespace \
  --wait --timeout 10m
```

For production, select one of the explicit certificate profiles.

Formal releases publish this chart as the `kubernaut-operator` OCI artifact:

```bash
helm upgrade --install kubernaut-operator \
  oci://quay.io/kubernaut-ai/kubernaut-operator@sha256:<chart-digest> \
  --namespace kubernaut-operator-system \
  --create-namespace \
  --wait --timeout 10m
```

Use the immutable digest printed by the release workflow rather than a mutable
version tag for production or disconnected installations.

The development profile creates and reuses its serving Secret from a
pre-install/pre-upgrade Job. That Job uses a short-lived certificate-bootstrap
ServiceAccount with namespace-scoped Secret access; the manager Pod does not
run the certificate image or receive its RBAC identity. A separate restricted
publisher patches and verifies the singleton webhook CA after installation,
then attaches the development Secret to the manager Deployment for safe
 garbage collection on uninstall.

### Administrator-managed certificates

Create a Secret containing `tls.crt` and `tls.key`, and provide the base64 CA
bundle used to validate the serving certificate:

```bash
helm install kubernaut-operator ./charts/kubernaut-operator \
  --namespace kubernaut-operator-system \
  --set webhook.tls.mode=manual \
  --set webhook.tls.existingSecret=operator-webhook-cert \
  --set webhook.tls.caBundle="$(base64 < ca.crt | tr -d '\n')"
```

The chart never creates, adopts, rotates, or deletes the administrator-owned
Secret.

### cert-manager

Install cert-manager and an `Issuer` or `ClusterIssuer` first, then select the
profile and issuer reference:

```bash
helm install kubernaut-operator ./charts/kubernaut-operator \
  --namespace kubernaut-operator-system \
  --set webhook.tls.mode=certManager \
  --set webhook.tls.certManager.issuerRef.name=operator-issuer \
  --set webhook.tls.certManager.issuerRef.kind=ClusterIssuer
```

The chart creates only the selected `Certificate`; cert-manager owns the
resulting Secret and CA rotation. The Certificate is retained on Helm uninstall
so cert-manager-owned output is not deleted as a side effect of removing the
operator.

### OpenShift service CA

On OpenShift, select `webhook.tls.mode=openshift`. The chart then emits the
OpenShift service-CA annotations. Generic Kubernetes profiles emit no
OpenShift-specific annotations.

## CRD and operand lifecycle

The generated `kubernauts.kubernaut.ai` CRD is rendered as a Helm resource with
`helm.sh/resource-policy: keep`. It is retained across `helm uninstall` and
reinstall. A pre-existing CRD owned by OLM, Kustomize, another Helm release, or
an administrator is not adopted; installation fails with an ownership conflict.

Uninstalling the operator is not operand cleanup. Existing `Kubernaut` CRs,
their finalizers, runtime application CRDs, and operand resources remain in the
cluster. The operator is absent and those operands are temporarily unmanaged;
reinstall the chart to resume reconciliation. Do not delete the CRD as part of
operator removal.

The operator's runtime `EnsureCRDs` path continues to install application CRDs
only after a user or GitOps controller applies a `Kubernaut` CR. Those CRDs are
not packaged by this chart.

## Disconnected and immutable images

The chart supports the same disconnected image model as the OLM deployment,
but mirroring is an installation responsibility. The chart has no remote Helm
chart dependencies; its archive/OCI artifact must be available from the
air-gapped Helm source, and every image used by the selected path must be
available from a registry reachable by the cluster.

For an operator-only installation with the default development TLS profile,
mirror the manager and certificate-bootstrap images and provide pull Secrets
for both workloads:

```yaml
# airgap-values.yaml
image:
  repository: registry.example.com/kubernaut/kubernaut-operator
  digest: sha256:<manager-manifest-digest>
  pullSecrets:
    - name: registry-pull

webhook:
  tls:
    mode: development
    development:
      image:
        repository: registry.example.com/kubernaut/kubectl
        digest: sha256:<bootstrap-image-digest>
        pullSecrets:
          - name: registry-pull
```

Install from the local or mirrored chart without changing the operator's
other defaults:

```bash
helm install kubernaut-operator ./charts/kubernaut-operator \
  --namespace kubernaut-operator-system \
  --create-namespace \
  --values airgap-values.yaml \
  --wait --timeout 10m
```

The development bootstrap image is not needed when using `manual` TLS. In
that case, pre-create the administrator-owned serving Secret and configure
`webhook.tls.existingSecret` and `webhook.tls.caBundle` instead. A
`certManager` installation requires cert-manager and its issuer to be
installed from mirrored artifacts first; the `openshift` profile requires the
OpenShift service CA.

When the operator later reconciles a `Kubernaut` CR, mirror the operand images
as well. There are two equivalent image-resolution paths:

1. Replace **every** entry in the chart's `relatedImages` map with its mirrored
   immutable reference. Do not override only one key, because omitted map keys
   retain the public chart defaults.
2. Set `spec.image.overrides` on the `Kubernaut` CR for every operand, using the
   component keys documented by the CRD. CR overrides take precedence over
   `RELATED_IMAGE_*`, just as they do with OLM.

In both cases, set `spec.image.pullSecrets` on the `Kubernaut` CR so operand
Pods can pull from the private mirror. The chart's `image.pullSecrets` only
covers the operator and its bootstrap Jobs; it is not implicitly copied to
operand workloads. Related images are environment variables consumed by the
operator; they do not cause operand workloads to be rendered or started by
Helm.

The Helm Kind qualification lane exercises the disconnected bootstrap contract
by loading the manager and certificate-provisioner image pulled by immutable
digest into Kind,
supplying an image pull Secret, and asserting that no operand or related image
is pulled during operator-only installation:

```bash
KUBERNAUT_HELM_E2E_DISCONNECTED=true \
KUBERNAUT_HELM_E2E_METRICS=true \
make test-e2e-kind-helm
```

Set `KUBERNAUT_HELM_E2E_TLS_PROFILE=manual` or `certmanager` to exercise the
administrator-managed or cert-manager profile in the same journey. CI runs the
development, manual, cert-manager, and disconnected profiles with Helm v4.3.0.

## OpenShift security defaults

The OpenShift TLS profile does not force `fsGroup` or `hostUsers` in the pod
template. OpenShift SCC admission assigns a compatible UID, supplemental group,
and user-namespace mode; a fixed group is not portable across project ranges.
Generic development TLS retains `fsGroup: 65534` as its fixed
generic-Kubernetes supplemental-group default. Set
`hostUsers=false` explicitly only when the target cluster requires pod-level
user namespaces.

## Configuration boundary

Values are limited to operator bootstrap: image references, RBAC identity,
metrics enablement and service annotations, webhook/TLS profiles, CRD
lifecycle, scheduling, and pod-security compatibility. Leader election,
health probes, webhook fail-closed behavior, fixed service ports, termination
grace, and the restricted manager security context are chart-owned invariants.
PostgreSQL, Valkey, OIDC, telemetry, Fleet, Gateway, application policy, and
workload settings belong to the `Kubernaut` CR and are intentionally not
accepted by the chart schema.

# Kubernaut Operator

Kubernetes operator for deploying and managing the [Kubernaut](https://kubernaut.ai) autonomous remediation platform on OpenShift (OCP 4.18+).

## Overview

The Kubernaut Operator manages the full lifecycle of a Kubernaut deployment:

- **Validates** BYO PostgreSQL and Valkey secrets
- **Migrates** the database schema via embedded SQL migrations
- **Deploys** 10 microservices, RBAC, ConfigMaps, PDBs, webhooks, and OCP Routes
- **Monitors** workload readiness and reports per-service status
- **Cleans up** all cluster-scoped resources on CR deletion via a finalizer

The operator is designed as a **singleton**: exactly one `Kubernaut` CR named `kubernaut` should exist per cluster.

## Prerequisites

| Requirement | Version |
|---|---|
| OpenShift | 4.18+ |
| PostgreSQL | 15+ (BYO) |
| Valkey/Redis | 7+ (BYO) |
| LLM API credentials | OpenAI, Anthropic, or GCP Vertex AI |

## Installation Guide

Follow the three-part installation guide to deploy Kubernaut on OCP:

| Step | Document | What it covers |
|---|---|---|
| 1 | [Infrastructure Prerequisites](docs/installation/01-infrastructure.md) | Namespace, PostgreSQL, Valkey, LLM credentials |
| 2 | [Configure Services](docs/installation/02-configure-services.md) | KA (LLM/SDK), SP (Rego policy), AA (approval policy), AAP (Ansible), ArgoCD, Slack |
| 3 | [Deploy Kubernaut](docs/installation/03-deploy.md) | Install operator, create CR, verify, seed catalog, AlertManager |

## CR Reference

### Image overrides

Service images are resolved from `RELATED_IMAGE_*` environment variables on the operator pod (set at build time, rewritten by OLM for disconnected registries). For non-OLM deployments or testing, use per-component overrides:

```yaml
spec:
  image:
    overrides:
      gateway: "myregistry.example.com/gateway:custom"
      kubernautagent: "myregistry.example.com/kubernautagent:custom"
```

### Inter-service TLS

All inter-service communication uses TLS, provisioned automatically by the OpenShift `service-ca` operator. This is always enabled and not configurable. The operator annotates Gateway and DataStorage Services so that `service-ca` generates serving certificates, and injects the CA bundle into an `inter-service-ca` ConfigMap mounted by all components.

### Gateway configuration

```yaml
spec:
  gateway:
    route:
      enabled: true           # set false if using a custom Ingress
      hostname: ""             # leave empty for OCP auto-generated hostname
    config:
      k8sRequestTimeout: "15s"                              # default
      corsAllowedOrigins: "https://no-browser-clients.invalid"  # default (M2M API)
```

## Uninstall

When the `Kubernaut` CR is deleted, the operator's finalizer cleans up all cluster-scoped RBAC resources (ClusterRoles, ClusterRoleBindings) and the workflow namespace. **CRDs are intentionally retained** to prevent accidental data loss. To fully remove CRDs after uninstalling:

```bash
oc delete crd actiontypes.kubernaut.ai remediationworkflows.kubernaut.ai \
  remediationrequests.kubernaut.ai remediationapprovalrequests.kubernaut.ai \
  notificationrequests.kubernaut.ai workflowexecutions.kubernaut.ai
```

## Operational Notes

### Admission webhook blackout during upgrades

The AuthWebhook deployment uses a **Recreate** strategy to prevent TLS certificate routing conflicts between old and new pods. During a rollout the old pod is terminated before the new one is ready, creating a brief window (~15-30 s) where admission requests are unavailable. Because the webhook `failurePolicy` is `Fail`, any Kubernaut CRD mutations will be rejected until the new pod passes its readiness probe.

**Recommendation:** schedule operator upgrades during low-activity windows.

## Development

```bash
make build          # Build the operator binary
make test           # Run unit and integration tests
make manifests      # Regenerate CRD, RBAC, and webhook manifests
make generate       # Regenerate deepcopy
make bundle         # Regenerate the OLM bundle

# Deploy to a connected cluster (non-OLM)
make deploy IMG=quay.io/kubernaut-ai/kubernaut-operator:v1.3.0

# Undeploy
make undeploy
```

## License

Apache License 2.0. See [LICENSE](LICENSE).

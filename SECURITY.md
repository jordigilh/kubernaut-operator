# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.5.x   | :white_check_mark: |
| 1.4.x   | :white_check_mark: |
| 1.3.x   | :white_check_mark: |
| < 1.3   | :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability in the Kubernaut Operator, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please email **jgil@redhat.com** with:

1. A description of the vulnerability
2. Steps to reproduce
3. Potential impact
4. Any suggested fix (optional)

### What to Expect

- **Acknowledgment** within 48 hours of your report
- **Assessment** within 5 business days
- **Fix timeline** communicated after assessment, typically within 30 days for critical issues
- **Credit** in the release notes (unless you prefer to remain anonymous)

## Security Considerations

The Kubernaut Operator runs with elevated Kubernetes RBAC permissions to manage
cluster-scoped resources (ClusterRoles, ClusterRoleBindings, CRDs, webhook
configurations) and namespace-scoped workloads. When deploying:

- Follow the principle of least privilege for the operator's ServiceAccount
- TLS for the AuthWebhook is managed by OCP service-CA; the operator annotates
  the Service and webhook configurations for automatic certificate injection
- Restrict who can create/modify the singleton `Kubernaut` CR
- BYO secrets (PostgreSQL, Valkey, LLM credentials) should use short-lived
  credentials where possible and be rotated regularly
- The operator requires `escalate` and `bind` RBAC verbs to provision
  ClusterRoles for managed workloads; see [RBAC documentation](#rbac) below

### Admission Webhooks

The operator creates MutatingWebhookConfiguration and ValidatingWebhookConfiguration
resources for the AuthWebhook service. Key security properties:

- **FailurePolicy: Fail** -- webhook requests are rejected if the AuthWebhook
  service is unavailable. This prevents unauthenticated mutations from bypassing
  policy enforcement. In HA deployments, ensure the AuthWebhook Deployment has
  `replicas >= 2` and an appropriate PodDisruptionBudget to avoid blocking
  API server operations during rolling updates.
- **TimeoutSeconds: 10** -- explicit timeout to prevent slow webhook backends
  from causing API server request pile-up.
- TLS is managed by OCP service-CA: the `authwebhook-service` annotation
  requests automatic certificate injection.

### RBAC

The operator requires these elevated permissions:

- **`escalate` / `bind`** on ClusterRoles/ClusterRoleBindings: Required to
  create RBAC bindings for managed workloads (Kubernaut Agent investigator,
  signal processing, monitoring) without granting itself the permissions
  those roles carry.
- **`get/list/watch` on Secrets cluster-wide**: The Kubernaut Agent investigator
  role requires read access to secrets for root-cause analysis across
  namespaces. This is a deliberate design choice documented in the
  `kubernaut-agent-investigator` ClusterRole.

#### Per-ClusterRole Justification

| ClusterRole | Purpose | Scoped Resources |
|---|---|---|
| `<ns>-kubernaut-agent-investigator` | Cluster-wide read-only access for root-cause analysis | Core K8s (pods, deployments, secrets, events, RBAC, etc.), OCP platform (routes, SCCs, DeploymentConfigs, ImageStreams, Builds), OCP machine management (Machines, MachineSets, MachineConfigs, MCPs), OLM (CSVs, Subscriptions, InstallPlans, CatalogSources), admission webhooks, CRDs, PriorityClasses, Istio/Linkerd/cert-manager/ArgoCD/Prometheus. See `internal/resources/rbac.go` for the full rule set. |
| `<ns>-kubernaut-agent-client` | AIAnalysis service calls the KA service | Services (get/create on `kubernaut-agent-service`) |
| `<ns>-gateway-role` | Signal fingerprinting and owner-chain resolution | Core K8s read-only (nodes, pods, services, PVs, PVCs), apps (deployments, replicasets, statefulsets, daemonsets), autoscaling (HPAs), batch (jobs), networking (ingresses) |
| `<ns>-signalprocessing-controller` | Process signals and manage remediation lifecycle | Full CRUD on `kubernaut.ai` CRs (signalprocessings, remediationrequests), status/finalizer updates, read-only core K8s (pods, services, namespaces, nodes, deployments, replicasets, statefulsets, daemonsets, HPAs, PDBs, NetworkPolicies), event creation, leader election leases |
| `<ns>-aianalysis-controller` | Manage AI analysis lifecycle | Full CRUD on `kubernaut.ai` CRs (aianalyses), status/finalizer updates, read-only core K8s, event creation, leader election leases |
| `<ns>-remediationorchestrator-controller` | Orchestrate end-to-end remediation | Full CRUD on `kubernaut.ai` CRs (remediationrequests, remediationexecutions, workflowruns), status/finalizer updates, read-only core K8s, event creation, leader election leases |
| `<ns>-workflowexecution-controller` | Execute remediation workflows | Full CRUD on `kubernaut.ai` CRs (workflowruns), Argo Workflows (workflows), status/finalizer updates, read-only core K8s, event creation, leader election leases |
| `<ns>-workflow-runner` | Run remediation actions inside workflows | Broad read-write on core K8s (pods, deployments, statefulsets, daemonsets, services, configmaps, secrets, etc.), Istio/Linkerd/cert-manager/ArgoCD resources |
| `<ns>-effectivenessmonitor-controller` | Monitor remediation effectiveness | Full CRUD on `kubernaut.ai` CRs (effectivenessassessments), read-only core K8s, event creation, leader election leases |
| `<ns>-notification-controller` | Deliver notifications | Full CRUD on `kubernaut.ai` CRs (notifications), status/finalizer updates, event creation, leader election leases |
| `<ns>-data-storage-auth-middleware` | Auth webhook token review | TokenReview create, SubjectAccessReview create |
| `<ns>-data-storage-client` | Service-to-DataStorage API access | Full CRUD on `kubernaut.ai` CRs (datastorageapis). Bound per-service via RoleBindings |
| `<ns>-authwebhook-role` | AuthWebhook admission control | TokenReview create, SubjectAccessReview create, webhook configuration read |
| `<ns>-alertmanager-view` | EffectivenessMonitor reads Prometheus/AlertManager metrics | Created only when `monitoring.enabled=true` |
| `<ns>-gateway-signal-source` | AlertManager pushes signals to Gateway | Created only when `monitoring.enabled=true`. Bound to OCP alertmanager SA |
| `<ns>-workflowexecution-awx` | WorkflowExecution talks to AWX/AAP | AWX Jobs (CRUD). Created only when `ansible.enabled=true` |

#### Investigator RBAC Risk Assessment

The `kubernaut-agent-investigator` ClusterRole grants **read-only** access to a
broad set of resources across 30+ API groups. Key security considerations:

- **Cluster-wide secret read** (pre-existing): The agent can read all secrets
  in all namespaces. This is required for cross-namespace root-cause analysis.
- **RBAC topology read**: The agent can enumerate all Roles, ClusterRoles, and
  their bindings. An attacker who compromises the agent pod could map the
  entire cluster's principal-to-privilege chains.
- **SCC read** (OCP): Exposes which SecurityContextConstraints exist and which
  SAs are bound to privileged SCCs.
- **MachineConfig read** (OCP): Exposes node-level configuration (ignition
  snippets, kubelet config).
- **OCP networking read**: Includes EgressNetworkPolicies, HostSubnets, and
  NetNamespaces, exposing network topology details.
- **Ecosystem CRD read**: Istio/Linkerd security policies, cert-manager
  certificates, and ArgoCD application state are readable, potentially
  exposing service mesh mTLS configuration and GitOps deployment details.

These capabilities are equivalent to a **cluster auditor** tier — high-sensitivity
read access with no write or escalation capability. The accepted risk boundary
is: "compromise of the agent SA = full cluster read reconnaissance, but no
mutation or data exfiltration beyond secret content."

## Operator RBAC (`manager-role`)

The operator's own ClusterRole (`config/rbac/role.yaml`, `manager-role`) grants the following API access:

| API Group | Resources | Verbs | Justification |
|-----------|-----------|-------|---------------|
| `""` (core) | configmaps, namespaces, secrets, serviceaccounts, services | create, delete, get, list, patch, update, watch | Manage all namespace-scoped resources for the 10 kubernaut services |
| `""` (core) | events | create, patch | Emit Kubernetes events for reconciliation status |
| `admissionregistration.k8s.io` | mutatingwebhookconfigurations, validatingwebhookconfigurations | create, delete, get, list, patch, update, watch | Manage AuthWebhook admission configurations |
| `apiextensions.k8s.io` | customresourcedefinitions | create, get, list, patch, update, watch | Install and update kubernaut workload CRDs |
| `apps` | deployments | create, delete, get, list, patch, update, watch | Manage Deployments for all 10 services |
| `batch` | jobs | create, delete, get, list, patch, update, watch | Manage database migration Jobs |
| `config.openshift.io` | apiservers | get, list, watch | Read OCP APIServer CR to derive TLS profile |
| `kubernaut.ai` | kubernauts, kubernauts/finalizers, kubernauts/status | full CRUD | Reconcile the Kubernaut CR |
| `networking.k8s.io` | networkpolicies | create, delete, get, list, patch, update, watch | Manage NetworkPolicies when enabled |
| `policy` | poddisruptionbudgets | create, delete, get, list, patch, update, watch | Manage PDBs for service availability |
| `rbac.authorization.k8s.io` | clusterroles, clusterrolebindings, roles, rolebindings | bind, create, delete, escalate, get, list, patch, update, watch | Provision RBAC for managed workloads (requires escalate/bind) |
| `route.openshift.io` | routes | create, delete, get, list, patch, update, watch | Manage OCP Routes for Gateway |

## ServiceAccount Token Model

- Kubernaut Agent uses projected SA tokens (not automounted).
- `AutomountServiceAccountToken: false` on the `kubernaut-agent-sa` ServiceAccount.
- Projected token: audience `https://kubernetes.default.svc`, expiration 3600s (1 hour), mount path `/var/run/secrets/kubernetes.io/serviceaccount/token`.
- All other service SAs use standard automounted tokens.

## Container Hardening

- All containers run as non-root (`runAsNonRoot: true`).
- Privilege escalation disabled (`allowPrivilegeEscalation: false`).
- All Linux capabilities dropped (`capabilities.drop: ["ALL"]`).
- Seccomp profile: `RuntimeDefault`.
- Read-only root filesystem on the operator container.
- Init containers follow the same security context as application containers.

## Supply Chain Integrity

- Operator images signed with Cosign (keyless, GitHub Actions OIDC).
- SBOM (CycloneDX JSON) generated via Syft, attached to GitHub Releases.
- SBOM attested to images via Cosign (`cosign attest --type cyclonedx`).
- Image vulnerability scanning via Trivy (CRITICAL/HIGH gate in CI).
- Build provenance: SLSA 1–2 via `actions/attest-build-provenance`.
- Init container images pinned by digest and configurable via `RELATED_IMAGE_*` env vars.
- Consumer verification:

```bash
cosign verify \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  --certificate-identity-regexp="^https://github.com/jordigilh/kubernaut-operator/.github/workflows/release.yml@refs/tags/" \
  quay.io/kubernaut-ai/kubernaut-operator:<version>
```

## Disclosure Policy

We follow coordinated disclosure. We ask that you give us reasonable time to address the vulnerability before public disclosure.

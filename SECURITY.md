# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
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
| `<ns>-kubernaut-agent-investigator` | Root-cause analysis across namespaces | Pods, Events, Deployments, ReplicaSets, Secrets (read-only) |
| `<ns>-kubernaut-agent-client` | AIAnalysis service calls the KA service | Services (get/create on `kubernaut-agent-service`) |
| `<ns>-signalprocessing-controller` | Watch Kubernetes events for signal ingestion | Events (cluster-wide, read-only) |
| `<ns>-alertmanager-view` | EffectivenessMonitor reads Prometheus/AlertManager metrics | Created only when `monitoring.enabled=true` |
| `<ns>-awx-integration` | WorkflowExecution talks to AWX/AAP | Created only when `ansible.enabled=true` |

## Disclosure Policy

We follow coordinated disclosure. We ask that you give us reasonable time to address the vulnerability before public disclosure.

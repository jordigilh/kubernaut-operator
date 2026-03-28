# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.x     | :white_check_mark: |

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
- The operator programmatically generates TLS certificates for the AuthWebhook;
  review the generated CA trust chain
- Restrict who can create/modify the singleton `Kubernaut` CR
- BYO secrets (PostgreSQL, Valkey, LLM credentials) should use short-lived
  credentials where possible and be rotated regularly

## Disclosure Policy

We follow coordinated disclosure. We ask that you give us reasonable time to address the vulnerability before public disclosure.

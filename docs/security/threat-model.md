# Threat model

**Organization:** Kubernaut AI  
**Product:** Kubernaut Operator (OpenShift 4.18+)  
**Security contact:** jgil@redhat.com  

**NIST 800-53 Rev. 5:** RA-3 (Risk Assessment)

---

This document provides a STRIDE-oriented threat model for the Kubernaut AIOps remediation platform as operated on OpenShift. It highlights trust boundaries, representative threats, and mitigations or residual risks. It does not replace an organization-specific risk assessment.

## System overview

Kubernaut is an AIOps platform that runs on OpenShift. It ingests operational signals (for example, AlertManager webhooks), performs AI-assisted root-cause analysis using an external or managed large language model, and drives remediation workflows through orchestrated execution and policy gates. The Kubernaut Operator installs and reconciles the namespace-scoped or cluster-scoped resources required for that stack.

## Trust boundaries

1. **Cluster perimeter:** External actors and systems enter through OpenShift Routes (for example, to the Gateway) and platform ingress, versus workloads and services inside the cluster.
2. **Namespace boundary:** The `kubernaut-system` (or customer-chosen) namespace hosting Kubernaut components versus other namespaces and tenants on the cluster.
3. **LLM boundary:** The Kubernaut Agent and related controllers versus external LLM software-as-a-service APIs on the public internet or partner networks.
4. **Database boundary:** Application services versus the PostgreSQL instance backing DataStorage.
5. **Custom resource administration boundary:** Principals who can create or mutate the Kubernaut CR versus principals who only consume metrics or read-only data.

## STRIDE analysis

| Threat | Category | Component | Risk | Mitigation |
|--------|----------|-----------|------|------------|
| Malicious AlertManager signal injection | Spoofing | Gateway | Medium | Mutual TLS and ServiceAccount token verification; `gateway-signal-source` ClusterRoleBinding limits which ServiceAccounts may present signals |
| Prompt injection via alert payload | Tampering | Kubernaut Agent | High | Safety sanitization (`injectionPatternsEnabled`, `credentialScrubEnabled`), anomaly controls (`maxToolCallsPerTool`, `maxTotalToolCalls`), optional alignment checks |
| LLM data exfiltration via secrets or verbose tool output in prompts | Information Disclosure | Kubernaut Agent | High | Tool output summarization limiting data sent to the LLM (`maxToolOutputSize`), credential scrubbing of known patterns, least-privilege review of tools |
| Unauthorized remediation execution | Elevation of Privilege | Remediation Orchestrator | High | Rego policy approval gates, `dryRun` mode, human review via `awaitingApproval` timeouts, optional alignment checks for step-level oversight |
| Cluster administrator binds excessive roles to the agent via CR | Elevation of Privilege | Operator | Critical | `additionalClusterRoleBindings` permits arbitrary ClusterRoleBindings; restrict who may edit the Kubernaut CR and use cluster RBAC reviews |
| Compromised agent pod with broad secret visibility | Information Disclosure | Kubernaut Agent | High | Accepted design trade-off for investigator-style cluster read; compensate with NetworkPolicy isolation, short-lived projected tokens (for example one-hour TTL), namespace separation, and monitoring |
| Webhook bypass or impersonation during upgrades | Spoofing | AuthWebhook | Medium | `failurePolicy: Fail` fail-closed admission; Deployment recreate strategy implies brief admission unavailability; PodDisBudget recommended for high availability |

## Accepted risks

The following items are acknowledged as residual or intentional risks that customers must govern through policy, RBAC, and compensating controls:

- **Cluster-wide secret readability** for the investigation role is comparable to a cluster auditor; customers must align this with their threat model and network policy posture.
- **LLM providers receive investigation context** necessary for analysis; exposure is reduced via summarization and sanitization but not eliminated.
- **`additionalClusterRoleBindings`** can escalate privileges if the Kubernaut CR is writable by untrusted actors; protection is organizational RBAC on the CR and change management, not a separate operator-side block.

Organizations deploying Kubernaut should extend this model with their own data classification, compliance obligations, and incident response playbooks.

For questions about this document or security practices, contact **jgil@redhat.com** on behalf of **Kubernaut AI**.

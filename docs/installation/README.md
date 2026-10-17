# Installation Guide

Start here: **[Quickstart: Minimal Kubernaut CR](00-quickstart.md)** -- the fastest path to a `Running` Kubernaut deployment.

For the fuller, every-knob-annotated walkthrough:

| Step | Document | What it covers |
|---|---|---|
| 1 | [Infrastructure Prerequisites](01-infrastructure.md) | BYO services or the disposable PostgreSQL/Valkey bootstrap chart |
| 2 | [Configure Services](02-configure-services.md) | KA (LLM/SDK), SP (Rego policy), AA (approval policy), AAP (Ansible), ArgoCD, Slack, API Frontend RBAC |
| 3 | [Deploy Kubernaut](03-deploy.md) | Install operator, create CR, verify, seed catalog, AlertManager |
| 4 | [Fleet: Kuadrant MCP Gateway](04-fleet-mcp-gateway.md) | Optional -- only if you enable `spec.fleet.mcpGateway.type: kuadrant` |
| 5 | [Fleet: Multi-Cluster Setup](05-fleet-multi-cluster.md) | Optional -- deploying RHBK from scratch, and/or adding a second ("spoke") managed cluster to Fleet |

See also: [Credentials, TLS, and protected communications](../security/credentials-and-tls.md), [Threat model](../security/threat-model.md), and [Auditing and accountability logging](../security/auditing.md).

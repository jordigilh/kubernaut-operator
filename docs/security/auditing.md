# Auditing and accountability logging

**Organization:** Kubernaut AI  
**Product:** Kubernaut Operator (OpenShift 4.18+)  
**Security contact:** jgil@redhat.com  

**NIST 800-53 Rev. 5:** AU-2 (Event Logging), AU-3 (Content of Audit Records)

---

This document describes audit-relevant event sources, log collection expectations, Kubernetes API audit coverage, and retention responsibilities for deployments of the Kubernaut AIOps remediation platform managed by the Kubernaut Operator.

## Audit event sources

### Kubernaut Agent audit subsystem

When enabled via the Kubernaut custom resource (`spec.kubernautAgent.audit.enabled`, default `true`), the Kubernaut Agent audit subsystem records security- and operations-relevant activity including investigation sessions, tool calls, LLM interactions, and operator or model decisions that affect analysis outcomes.

Records are emitted as structured JSON to standard output. The following tuning parameters are fixed in the implementation: flush interval one second, buffer size ten thousand events, and batch size fifty events. Adjusting these values requires a product change; they are not exposed on the CR.

### Remediation Orchestrator state transitions

The Remediation Orchestrator persists workflow state transitions (for example: Processing, Analyzing, Executing, Verifying, Complete, Failed) through the DataStorage API, which is backed by PostgreSQL. Retention of this application-level history is governed by `spec.remediationOrchestrator.retention.period` (default twenty-four hours).

### AuthWebhook admission decisions

The AuthWebhook component logs admission decisions (admit or deny) to standard output, including outcomes of TokenReview and SubjectAccessReview calls against the Kubernetes API. These records support traceability of authentication and authorization enforcement at admission time.

### Gateway signal ingestion

The Gateway logs receipt and processing of AlertManager webhook traffic (signal ingestion), providing an auditable trail that external alerts reached the platform boundary.

### Component logging levels and format

All platform components support a per-component `logging.level` (default `info`), configurable through the Kubernaut CR. Log format is JSON across the stack: DataStorage uses JSON by configuration default, and other components default to JSON for consistency with centralized collection.

## Log collection

Consistent with twelve-factor application practice, all component logs are written to container standard output and standard error streams. On OpenShift Container Platform, these streams are collected automatically by the Cluster Logging Operator (CLO) stack using Vector (or equivalent supported collector).

Customers may forward aggregated logs to enterprise security tooling via a `ClusterLogForwarder` resource (for example: Splunk, Elasticsearch, Amazon CloudWatch, Grafana Loki). Configuration of CLO, destinations, transport encryption, and access controls is a **customer responsibility**, as are retention policies in the downstream SIEM and operational dashboards.

## Kubernetes API audit

Operator reconciliation performs Kubernetes API operations such as Kubernaut CR lifecycle management, Deployment create and update, ConfigMap and Secret synchronization, and RBAC provisioning. These actions are recorded in the OpenShift API server audit log according to cluster audit policy.

Selecting audit policy profile, log verbosity, storage, and review procedures for API server audit data is a **customer responsibility**.

## Retention

- **Application data (orchestration and remediation history):** Controlled by `spec.remediationOrchestrator.retention.period` (configurable; default twenty-four hours).
- **Platform and container logs:** Controlled by customer-configured CLO forwarding and SIEM retention schedules.
- **Database:** PostgreSQL schema and data lifecycle are managed through goose migrations; read-only retention enforcement for stored records is applied according to product database design (retention-related behavior is enforced in conjunction with orchestrator retention settings).

For questions about this document or security practices, contact **jgil@redhat.com** on behalf of **Kubernaut AI**.

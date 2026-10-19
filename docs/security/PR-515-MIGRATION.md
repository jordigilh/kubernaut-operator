# PR #515 remediation: deployment preflight and migration

> **Historical / superseded by issue #555 for network-policy behavior.**

These are operator engineering changes, not a FedRAMP authorization or proof of
outside cluster controls. Do not upgrade blindly: the default isolation behavior
changes intentionally from best-effort provider submission to fail-closed native
enforcement.

## Before upgrading

1. Confirm there is only one Kubernaut CR cluster-wide, named `kubernaut`:
   `kubectl get kubernauts.kubernaut.ai --all-namespaces`.
   Do not automatically delete an existing installation or strip its finalizer.
2. Verify that the cluster has a supported, active Cilium, Calico or OVN provider
   and that its discovered schema/version is qualified by the operator. Missing
   or failed discovery blocks migration and new workload reconciliation; retained
   policies are not pruned to disguise the failure. Unsupported topology has no
   administrator-owned isolation bypass.
3. PostgreSQL and Valkey are always administrator-provisioned BYO Services. Verify
   that each configured host resolves to an in-cluster selector-backed Service
   with the expected port. External DNS, `ExternalName`, headless and
   selector-less destinations fail closed. Verify the CR's PostgreSQL TLS and
   Valkey mTLS requirements before activation.
4. Authorize the actual scraper pod identity if monitoring is
   enabled. Use its exact namespace and nonempty exact pod labels, not the
   Prometheus query/Thanos URL:

   ```yaml
   spec:
     networkPolicies:
       provider: Auto
       scrapeSources:
         - namespace: monitoring
           podLabels:
             app.kubernetes.io/name: prometheus
   ```

   Empty `scrapeSources` authorizes no external scraper. Each rule grants TCP
   only to that component's generated metrics port, including a custom AF
   metrics port; AuthWebhook has no metrics endpoint. The selector is an
   administrator-owned trust boundary: prevent untrusted principals from creating
   pods or changing matching labels in the selected scraper namespace.
5. Verify live allowed scraper/denied outsider traffic on the real CNI, metrics
   delivery and alert delivery using the exact candidate SHA/images. Operator
   contract fixtures do not qualify upstream application images. HTTP metrics
   remain HTTP; ingress authorization is not transport encryption.

## Failure and recovery

- Admission still denies competitors across namespaces. Reconcile additionally
  uses authoritative persisted state to block activation if concurrent admission
  or webhook bypass created duplicates. Conflicts require explicit administrator
  recovery, not automatic winner selection or resource takeover.
- A deleting, finalizer-protected holder continues to block another CR. Cleanup
  failures emit `CleanupFailed`, retain the finalizer and retry even after the
  former timeout; they never emit `CleanupComplete`. Fix the reported API or
  permission failure instead of stripping the finalizer. Merged PR #517's
  ownership checks and conditional deletes remain in force. Administrator force
  removal is outside ordinary recovery and requires explicit orphan review.
- Operator uninstall retains the CR's namespace and the workflow namespace, provisioning-owned
  workflow artifacts, database/Valkey resources and administrator inputs.
  Reinstall can reuse these namespaces; an administrator-supplied workflow
  namespace must already satisfy restricted PSA and is reused read-only.
- On provider API/discovery or BYO dependency failure, diagnose `ProviderDetected`
  and `ProviderPolicyReady`. Restore the provider or dependency topology; do not
  opt out merely to hide the failure. The operator must not be presented as
  `Running` with failed policy readiness.
- To roll back the operator, preserve the CR, namespaces, data and Secrets.
  Retain isolation and independently review the old controller's weaker checks;
  rolling back the binary is not a security remediation.

## Qualification limits

The operator-local PR needs independent zero-miss business-unit evidence, genuine
real-API wiring tests, generated/bundled schema consistency and binary scans.
Full upstream exact-SHA compatibility, released-image/SBOM/signature evidence,
FIPS operating-mode evidence and administrator-owned BYO ingress/CNI enforcement
qualification remain separate release/deployment gates under the issue #501 CI
boundary.

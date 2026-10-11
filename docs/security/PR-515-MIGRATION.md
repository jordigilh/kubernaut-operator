# PR #515 remediation: deployment preflight and migration

These are operator engineering changes, not a FedRAMP authorization or proof of
outside cluster controls. Do not upgrade blindly: the default isolation behavior
changes intentionally from best-effort provider submission to **Required**.

## Before upgrading

1. Confirm there is only one Kubernaut CR cluster-wide, named `kubernaut`:
   `kubectl get kubernauts.kubernaut.ai --all-namespaces`.
   Do not automatically delete an existing installation or strip its finalizer.
2. For `networkPolicies.mode: Required` (including an omitted mode), verify that
   the cluster has a supported, active Cilium, Calico or OVN provider and that
   its discovered schema/version is qualified by the operator. Missing or failed
   discovery blocks migration and new workload reconciliation; retained policies
   are not pruned to disguise the failure.
3. If the deployment deliberately inherits isolation from the administrator,
   explicitly configure the following **before** the operator upgrade:

   ```yaml
   spec:
     networkPolicies:
       mode: Inherited
       provider: Auto
   ```

   This is an acknowledgement, **not attestation** of isolation. Verify the
   inherited allow/deny traffic tests separately. Existing native policies are
   retained in this mode; their retirement is an explicit administrator-owned
   operation. Do not select a native provider or scraper allowlist in this mode.
4. In Required mode, authorize the actual scraper pod identity if monitoring is
   enabled. Use its exact namespace and nonempty exact pod labels, not the
   Prometheus query/Thanos URL:

   ```yaml
   spec:
     networkPolicies:
       mode: Required
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
- A deleting, finalizer-protected holder continues to block another CR. A forced
  cleanup timeout emits `FinalizerTimeout`, never a false `CleanupComplete`.
  Inspect orphan resources before reinstallation; ownership safety depends on
  PR #517. Do not force-remove finalizers as an ordinary upgrade step.
- On provider API/discovery failure, diagnose `ProviderDetected` and
  `ProviderPolicyReady`. Restore the provider; do not opt out merely to hide the
  failure. Required mode must not be presented as `Running` with failed policy
  readiness.
- To roll back the operator, preserve the CR, namespaces, data and Secrets.
  Retain isolation and independently review the old controller's weaker checks;
  rolling back the binary is not a security remediation.

## Qualification limits

The operator-local PR needs independent zero-miss business-unit evidence, genuine
real-API wiring tests, generated/bundled schema consistency and binary scans.
Full upstream exact-SHA compatibility, released-image/SBOM/signature evidence,
FIPS operating-mode evidence and inherited cluster control qualification remain
separate release/deployment gates under the issue #501 CI boundary.

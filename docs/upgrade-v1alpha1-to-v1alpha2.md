# Migrating `Kubernaut` from v1alpha1 to v1alpha2

`kubernaut.ai/v1alpha2` is the only served and stored Kubernaut API. The
operator does not register a conversion webhook and does not accept
`kubernaut.ai/v1alpha1` objects after the clean-break release.

This is an export/transform/recreate migration. Perform it during a planned
operator maintenance window and keep the PostgreSQL/Valkey data and
administrator-owned Secrets backed up.

## 1. Export before upgrading

```bash
kubectl get kubernaut kubernaut -n "$KUBERNAUT_NAMESPACE" -o yaml \
  > kubernaut-v1alpha1-backup.yaml
```

Save the file outside the cluster. Do not rely on retrieving a v1alpha1 object
after the CRD has been upgraded.

## 2. Transform the manifest

Create a new manifest from the exported object and make these changes:

1. Set `apiVersion: kubernaut.ai/v1alpha2`.
2. Keep `metadata.name: kubernaut` and the original namespace.
3. Remove server-owned metadata (`uid`, `resourceVersion`, `generation`,
   `creationTimestamp`, and `managedFields`) and remove `status`.
4. Move `spec.ansible` to `spec.workflowExecution.ansible`.
5. Move `spec.kubernautAgent.additionalClusterRoleBindings` to the v2 field
   `spec.additionalClusterRoles`.
6. Remove `spec.networkPolicies.enabled`; NetworkPolicies are mandatory in
   v1alpha2. Keep the remaining network-policy tuning fields only if the
   selected platform policy contract supports them.
7. Replace the removed inline
   `spec.kubernautAgent.alignmentCheck.llm.{provider,model,endpoint}` block
   with `alignmentCheck.llmProfileRef` and a matching entry in
   `spec.llmProfiles`. Review credentials and endpoints rather than copying
   them blindly.
8. Add any v1alpha2-required fields, especially the AIAnalysis and
   SignalProcessing policy references and the required LLM profile map.
9. Remove legacy `spec.apiFrontend.spire` configuration; v1alpha2 does not
   expose a SPIFFE/SPIRE operator contract. Review
   `spec.apiFrontend.auth.issuerURL`: v1alpha2 requires a complete explicit
   issuer URL for single-provider API Frontend auth; the operator does not
   select or synthesize an IdP URL. Do not replace it with a short realm name.

Review the complete v1alpha2 schema with:

```bash
kubectl explain kubernaut.spec --api-version=kubernaut.ai/v1alpha2
```

Do not copy `status` into the new manifest. The operator will rebuild status
conditions and service readiness after recreation.

## 3. Recreate during the maintenance window

1. Upgrade/install the operator and apply the v1alpha2-only CRD.
2. Confirm the manager is ready and that the CRD reports only `v1alpha2`:

   ```bash
   kubectl get crd kubernauts.kubernaut.ai \
     -o jsonpath='{.spec.versions[*].name}{"\n"}'
   ```

3. Delete the old Kubernaut CR and wait for the old operator-owned runtime
   resources to be cleaned up.
4. Apply the transformed manifest:

   ```bash
   kubectl apply -f kubernaut-v1alpha2.yaml
   ```

5. Follow the new CR through `Validating`, `Migrating`, `Deploying`, and
   `Running` (or `Degraded`) and verify the operator events and logs.

The operator does not migrate application data between API versions. The
database migration Job remains the application-schema migration mechanism;
the API migration only changes the Kubernaut object shape and ownership
boundary.

Changing the OIDC realm during this migration invalidates existing access
tokens and Console cookies. Coordinate IdP client registrations, audiences,
redirect URIs, JWKS reachability, and CA trust before applying the new object.

## Unsupported shortcut

Applying a v1alpha1 manifest after the clean-break CRD is installed is not a
supported compatibility path. It should fail API discovery or schema
validation. Do not bypass this with an unstructured object or by editing the
stored object directly.

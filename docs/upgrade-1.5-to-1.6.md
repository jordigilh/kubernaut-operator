# Upgrade Guide: 1.5 → 1.6

## Prerequisites

- OpenShift 4.17+ cluster
- Kubernaut Operator 1.5.x running
- `oc` CLI authenticated with cluster-admin

## Breaking Changes

### LLM configuration moved to top-level named profiles (#187)

`spec.kubernautAgent.llm` has been **removed**. LLM provider configuration
(provider, model, credentials, endpoint, mTLS, OAuth2, etc.) now lives in
`spec.llmProfiles`, a map of named profiles, with `spec.kubernautAgent.llmProfileRef`
selecting which profile Kubernaut Agent (KA) uses for its investigator LLM
calls. Other components with their own LLM triage (API Frontend's severity
triage) reference profiles the same way via their own `llmProfileRef` field.

This is a breaking change with **no automated in-operator conversion** —
accepted for the 1.6 milestone. Existing 1.5 CRs using
`spec.kubernautAgent.llm` will fail admission validation on upgrade until
migrated. See [Migrating your CR](#migrating-your-cr) below for both a
migration helper and the manual path.

#### Before (1.5)

```yaml
spec:
  kubernautAgent:
    llm:
      provider: vertex_ai
      model: claude-sonnet-4-6
      credentialsSecretName: llm-credentials
      vertexProject: itpc-gcp-eco-eng-claude
      vertexLocation: global
      maxRetries: 3
      timeoutSeconds: 120
      phaseModels:
        workflow_discovery:
          model: claude-haiku-4-5
    maxTurns: 40
```

#### After (1.6)

```yaml
spec:
  llmProfiles:
    primary:
      provider: vertex_ai
      model: claude-sonnet-4-6
      credentialsSecretName: llm-credentials
      vertexProject: itpc-gcp-eco-eng-claude
      vertexLocation: global
      maxRetries: 3
      timeoutSeconds: 120
    workflow_discovery:
      provider: vertex_ai
      model: claude-haiku-4-5
      credentialsSecretName: llm-credentials
      vertexProject: itpc-gcp-eco-eng-claude
      vertexLocation: global
      maxRetries: 3
      timeoutSeconds: 120
  kubernautAgent:
    llmProfileRef: primary
    phaseModels:
      workflow_discovery: workflow_discovery
    maxTurns: 40
```

Two constraints on the new shape carried over from #187, neither of which
has an automated workaround:

- A `phaseModels` entry must reference a profile that shares the base
  profile's exact `credentialsSecretName` (cross-credential phase overrides
  are not yet supported — tracked in
  [jordigilh/kubernaut#1676](https://github.com/jordigilh/kubernaut/issues/1676)).
- Credentials are Secret-only. There is no `apiKey` field on `LLMProfileSpec`;
  any old inline `apiKey` must be moved into a Secret referenced by
  `credentialsSecretName` by hand.

### API Frontend severity triage LLM is now independently configurable (#187)

`spec.apiFrontend.severityTriage.llmProfileRef` was added so severity triage
can reference its own profile (or be disabled independently of KA) instead
of implicitly sharing KA's LLM configuration.

## Migrating your CR

### Option A: migration script (recommended for the common case)

A best-effort bash script ships in `hack/migrate-llm-profile.sh`. It's a
single file with one runtime dependency — [`yq`](https://github.com/mikefarah/yq)
(v4.x) — so you can run it directly from a checkout, or `curl` it standalone
without cloning the repo or installing Go. It converts the common case — a
base LLM config with optional same-provider/model-only phase overrides —
automatically:

```bash
hack/migrate-llm-profile.sh -in old-cr.yaml -out new-cr.yaml
# or, piping:
hack/migrate-llm-profile.sh < old-cr.yaml > new-cr.yaml
```

The script **refuses** (with a clear error on stderr naming the offending
phase and a non-zero exit code, rather than silently producing an invalid or
lossy CR) the two cases the new schema cannot represent:

- a phase override with a different `provider` than the base profile
- a phase override with an inline `apiKey`

Both require manual resolution — see the constraints above.

Always review the script's output and diff it against your original CR
before applying; it does not currently move OAuth2/mTLS secret *contents*,
only the fields that reference them.

### Option B: manual migration

1. For each distinct provider/credential combination in your old
   `spec.kubernautAgent.llm` (base + any `phaseModels` overrides), create one
   entry under `spec.llmProfiles`, giving it a descriptive name.
2. Set `spec.kubernautAgent.llmProfileRef` to the profile that should be the
   default (previously the base `llm` block).
3. For each old `phaseModels` override, add an entry to
   `spec.kubernautAgent.phaseModels` mapping the phase name to the new
   profile name from step 1.
4. Remove `spec.kubernautAgent.llm` entirely.
5. If you use API Frontend severity triage, set
   `spec.apiFrontend.severityTriage.llmProfileRef` explicitly (it no longer
   implicitly inherits KA's profile).

## Upgrade Steps

1. **Update the CRD** before upgrading the operator:
   ```bash
   oc apply -f config/crd/bases/kubernaut.ai_kubernauts.yaml
   ```

2. **Migrate your CR** using Option A or B above. Do this *before* step 3 —
   the old shape fails admission against the 1.6 CRD.

3. **Apply the migrated CR**:
   ```bash
   oc apply -f new-cr.yaml
   ```

4. **Upgrade the operator image** to 1.6.0.

5. **Verify** the operator is running and the CR is accepted:
   ```bash
   oc get pods -l app.kubernetes.io/name=kubernaut-operator
   oc get kubernaut -o jsonpath='{.items[0].status.phase}'
   ```

## Rollback

To roll back to 1.5.x:

1. Scale down the 1.6.0 operator deployment.
2. Re-apply the 1.5.x CRD.
3. Restore your pre-migration CR (`spec.kubernautAgent.llm`) — the 1.6.0
   `spec.llmProfiles` shape is rejected by the 1.5.x CRD schema.
4. Deploy the 1.5.x operator image.

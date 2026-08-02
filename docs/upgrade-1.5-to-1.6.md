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
      vertexProject: example-gcp-project
      vertexLocation: us-central1
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
      vertexProject: example-gcp-project
      vertexLocation: us-central1
      maxRetries: 3
      timeoutSeconds: 120
    workflow_discovery:
      provider: vertex_ai
      model: claude-haiku-4-5
      credentialsSecretName: llm-credentials
      vertexProject: example-gcp-project
      vertexLocation: us-central1
      maxRetries: 3
      timeoutSeconds: 120
  kubernautAgent:
    llmProfileRef: primary
    phaseModels:
      workflow_discovery: workflow_discovery
    maxTurns: 40
```

One constraint on the new shape carried over from #187, with no automated
workaround:

- Credentials are Secret-only. There is no `apiKey` field on `LLMProfileSpec`;
  any old inline `apiKey` must be moved into a Secret referenced by
  `credentialsSecretName` by hand.

Earlier 1.6 milestones additionally required every `phaseModels` entry to
reference a profile sharing the base profile's exact `credentialsSecretName`.
That constraint has been lifted (#233): each `phaseModels` entry may now
reference a profile with its own `provider` and `credentialsSecretName`,
independent of `llmProfileRef`'s profile, once
[jordigilh/kubernaut#1728](https://github.com/jordigilh/kubernaut/pull/1728)
fixed Kubernaut Agent to resolve each phase's own credentials independently
rather than silently reusing the base profile's. The migration script below
still can't *auto-migrate* a 1.5 phase override that changes `provider`,
though — the 1.5 schema never recorded a per-phase `credentialsSecretName`,
so there is no Secret reference for it to carry forward; that case still
needs manual migration.

### API Frontend severity triage LLM is now independently configurable (#187)

`spec.apiFrontend.severityTriage.llmProfileRef` was added so severity triage
can reference its own profile (or be disabled independently of KA) instead
of implicitly sharing KA's LLM configuration.

Earlier 1.6 milestones additionally required `severityTriage.llmProfileRef`
to reference a profile sharing API Frontend's own resolved profile's exact
`credentialsSecretName`. That constraint has been lifted for every provider
except `vertex_ai` (#234): severity triage may now reference a profile with
its own `provider` and `credentialsSecretName`, since API Frontend already
resolves `severityTriage.llm` independently of `agent.llm`. The one
remaining restriction is that when *both* severity triage's and API
Frontend's own resolved profile use `vertex_ai`, they must still share a
`credentialsSecretName` — API Frontend's Vertex AI client relies on ambient
Application Default Credentials rather than per-profile credentials
([jordigilh/kubernaut#1731](https://github.com/jordigilh/kubernaut/issues/1731)),
so two different `vertex_ai` Secrets would silently collide rather than
each taking effect.

### `spec.monitoring` is removed — OCP monitoring integration can no longer be disabled (#273)

`spec.monitoring` (and its only field, `enabled`) is removed from the CRD
schema entirely as of 1.6. It is not deprecated or defaulted — it no longer
exists. OCP monitoring integration (Prometheus/AlertManager auto-discovery,
the RBAC that grants it, and the NetworkPolicy egress that allows it) is now
provisioned unconditionally on every reconcile, with no spec field left that
can turn it off.

This operator is OCP-specific throughout — NetworkPolicy rules, RBAC, and
ConfigMap generation all hardcode the `openshift-monitoring` namespace and
Thanos Querier URL, and OCP ships cluster-monitoring by default, so there was
never a supported non-OCP fallback for this toggle to select between.
Disabling it used to silently degrade several integrations at once: Gateway's
`AlertmanagerConfig`/token Secret, EffectivenessMonitor's external
Prometheus/AlertManager config, Kubernaut Agent's Prometheus/AlertManager
tools, and API Frontend's severity-triage Prometheus lookups. The last of
these became a reliability bug rather than a silent no-op once upstream
[jordigilh/kubernaut#1839](https://github.com/jordigilh/kubernaut/issues/1839)
removed the ungrounded LLM fallback that had been absorbing the resulting
"no data" response — with monitoring disabled, severity-gated remediation
request creation failed closed. See
[jordigilh/kubernaut-operator#273](https://github.com/jordigilh/kubernaut-operator/issues/273)
for the full analysis and alternatives considered.

There is no automated in-operator conversion. If your 1.5 CR sets
`spec.monitoring` (with `enabled` set to either `true` or `false`), delete
the block before applying against the 1.6 CRD — the field is not part of the
1.6 schema, so the apiserver's structural schema will silently prune it if
you apply the manifest as-is (this is harmless, since the behavior it used
to gate now always runs), but the field should still be removed from your
source-controlled manifests to avoid confusion.

This change is **not** backported to `release/v1.5` — see #271 for the
separately-backported NetworkPolicy fix that applies to both lines.

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
lossy CR) two cases it cannot safely auto-migrate:

- a phase override with a different `provider` than the base profile — the
  operator itself supports this shape fine as of #233, but the 1.5 schema
  never recorded a per-phase `credentialsSecretName`, so the script has no
  way to infer which Secret the new profile should reference
- a phase override with an inline `apiKey` — the new schema is Secret-only
  for credentials, with no equivalent field to convert into

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

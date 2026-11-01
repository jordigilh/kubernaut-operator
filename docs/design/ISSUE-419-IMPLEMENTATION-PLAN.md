# Issue #419 implementation decision

## Decision

`spec.kubernautAgent.investigation`,
`spec.kubernautAgent.workflowDiscovery`, and
`spec.kubernautAgent.alignmentCheck` are sibling agents. Each enabled sibling
resolves its own `llmProfileRef` from `spec.llmProfiles` and receives a complete
effective LLM configuration. Neither workflow discovery nor alignment may be
rendered as a partial override of investigation.

The operator therefore:

1. validates the alignment profile reference when alignment is enabled;
2. renders the selected profile's provider, model, endpoint, credentials,
   provider-specific, OAuth2, TLS, reasoning, retry, and timeout fields;
3. mounts the selected profile's API, OAuth2, and mTLS Secrets read-only;
4. reuses a base Secret only when the corresponding Secret is actually shared;
5. validates alignment profile Secrets through the pre-created configuration
   path before deployment.

For an independently mounted mTLS Secret, the operator uses a dedicated fixed
mount directory and projects the selected profile's certificate/key basenames
into it. The rendered profile paths are rewritten to that safe mount directory,
preventing administrator-supplied paths from overlaying unrelated credential
directories.

The upstream runtime change is merged in
[`kubernaut#2516`](https://github.com/jordigilh/kubernaut/pull/2516) at commit
`50eafbb4cd12c06488939c8100c348a2de5badf2`. The operator pins that commit's
pseudo-version and does not add a CRD schema field.

## Alternatives considered

### Partial alignment override

Rejected. It allows credentials and transport settings to leak from
investigation into alignment and cannot represent the full profile contract.

### Reject OAuth2 and mTLS alignment profiles

Rejected. This would make the CRD expose a profile catalog that alignment could
not actually consume and would violate sibling-agent parity.

### Independent complete profile resolution

Selected. It matches the CRD model, prevents cross-agent configuration
inheritance, and makes Secret ownership and control evidence explicit.

## Wiring manifest

| Component | Production entry point | Wiring location | Evidence |
|---|---|---|---|
| Alignment profile validation | `ValidateKubernaut` | `internal/resources/validation.go` | `UT-419-VAL-001` |
| Complete alignment ConfigMap block | `KubernautAgentConfigMap` | `internal/resources/configmaps.go` | `UT-419-CM-001`, `UT-419-CM-002` |
| Alignment Secret mounts | `KubernautAgentDeployment` | `internal/resources/deployments.go` | `UT-419-DEP-001`, `UT-419-DEP-002` |
| Pre-created Secret validation | validation phase | `internal/controller/kubernaut_controller.go` | `UT-419-CTRL-001` |
| End-to-end operator wiring | reconciler | `internal/controller/alignment_llm_integration_test.go` | `IT-419-RECONCILE-001` |

## Compatibility

- `alignmentCheck` remains an optional peer under `kubernautAgent`.
- An omitted alignment profile is inferred only when exactly one profile exists.
- Alignment disabled means no alignment profile validation or Secret mounts.
- No v1alpha1 conversion path exists in this checkout; no conversion change is
  required.

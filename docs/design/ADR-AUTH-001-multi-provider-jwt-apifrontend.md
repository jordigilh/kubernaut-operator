# ADR-AUTH-001: Multi-Provider JWT Authentication for API Frontend

**Status**: Proposed
**Decision Date**: 2026-06-16
**Version**: 1.0
**Confidence**: 90%
**Deciders**: Kubernaut Operator Team
**Applies To**: kubernaut-operator CRD, ConfigMap generation, CR validation

**Related Business Requirements**:
- BR-SECURITY-001: Multi-provider OIDC/JWT authentication (FedRAMP IA-2)
- BR-SECURITY-002: Session authenticity via audience binding (FedRAMP SC-23)
- BR-API-001: CRD API surface alignment with upstream configuration

**Related Design Decisions**:
- DD-AUTH-016: Signed user identity delegation (upstream KA JWT providers)
- ADR-036: Authentication & authorization strategy

**Upstream References**:
- kubernaut#1436: Add SPIRE as JWT provider for multi-platform console auth
- kubernaut PR#1397: `feat/structured-decision-payload` (implementation branch)

---

## Changelog

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-06-16 | Operator Team | Initial design |

---

## Context & Problem

### Current State

The API Frontend (AF) `auth` section in the Kubernaut CR currently supports only a **single OIDC provider**:

```go
type APIFrontendAuthSpec struct {
    IssuerURL             string `json:"issuerURL,omitempty"`
    Audience              string `json:"audience,omitempty"`
    TokenReviewAudience   string `json:"tokenReviewAudience,omitempty"`
    JWKSURL               string `json:"jwksURL,omitempty"`
    OIDCCAFile            string `json:"oidcCaFile,omitempty"`
    AllowInsecureIssuers  bool   `json:"allowInsecureIssuers,omitempty"`
}
```

Meanwhile, the Kubernaut Agent (KA) `interactive` section already supports multiple JWT providers via `JWTProviders []JWTProviderSpec`, but using a different field schema:

```go
type JWTProviderSpec struct {
    Name     string `json:"name"`
    JWKSURL  string `json:"jwksURL"`
    Audience string `json:"audience,omitempty"`   // singular
    Issuer   string `json:"issuer,omitempty"`     // different name from upstream
}
```

### Problem Statement

Upstream kubernaut (PR#1397 / issue#1436) has added multi-provider JWT authentication to the AF service. The AF now accepts a `jwtProviders[]` array in its `config.yaml`, allowing concurrent validation of JWTs from multiple OIDC issuers (e.g., Keycloak + SPIRE). The operator CRD does not expose this capability and the existing KA `JWTProviderSpec` has field mismatches with the upstream `JWTProviderConfig`.

### Constraints

- No backward compatibility required — the operator has not yet released with the AF `jwtProviders` field, and the existing `JWTProviderSpec` for KA can be safely modified since it was introduced in a pre-release cycle.
- The upstream `JWTProviderConfig` schema is the contract; the operator must emit config that matches it exactly.
- The existing single-provider AF fields (`issuerURL`, `audience`, `jwksURL`) must remain functional when `jwtProviders` is empty (kagenti auto-detection path).

---

## Decision Drivers

1. **Upstream alignment**: The operator's emitted ConfigMap YAML must match the upstream AF's `JWTProviderConfig` schema exactly.
2. **Type reuse**: A single `JWTProviderSpec` Go type should serve both KA interactive and AF auth to avoid drift.
3. **No backward compatibility burden**: Pre-release API surface allows breaking changes to `JWTProviderSpec`.
4. **FedRAMP compliance**: Multi-provider JWT supports IA-2 (multi-factor), SC-23 (session authenticity), and AC-6 (least privilege via claim-based authorization).

---

## Alternatives Considered

### Alternative A: Modify existing `JWTProviderSpec` to match upstream — reuse for both KA and AF ✅ CHOSEN

**Approach**: Modify the existing `JWTProviderSpec` in-place to align with upstream's `JWTProviderConfig`. Add a new `JWTProviders []JWTProviderSpec` field to `APIFrontendAuthSpec`. Both KA and AF share the same type.

**Changes to `JWTProviderSpec`**:

| Field | Before | After | Reason |
|-------|--------|-------|--------|
| `Audience` | `string` | removed | Replaced by `Audiences` |
| `Audiences` | — | `[]string` | Upstream uses plural slice |
| `Issuer` | `string` | removed | Replaced by `IssuerURL` |
| `IssuerURL` | — | `string` | Aligns with upstream naming |
| `ClaimMappings` | — | `*ClaimMappingsSpec` | New: username/groups claim mapping |

**Resulting type**:

```go
type JWTProviderSpec struct {
    Name          string             `json:"name"`
    IssuerURL     string             `json:"issuerURL"`
    JWKSURL       string             `json:"jwksURL,omitempty"`
    Audiences     []string           `json:"audiences"`
    ClaimMappings *ClaimMappingsSpec `json:"claimMappings,omitempty"`
}

type ClaimMappingsSpec struct {
    Username string `json:"username,omitempty"`
    Groups   string `json:"groups,omitempty"`
}
```

**Pros**:
- Single type for both KA and AF — no drift, maximum reuse
- Direct alignment with upstream `JWTProviderConfig` schema
- Cleaner API: `IssuerURL` is more descriptive than `Issuer`; `Audiences` (plural) correctly models multi-audience support
- Simpler validation: one `validateJWTProviders` function serves both paths

**Cons**:
- Breaking change to `JWTProviderSpec` (`Audience` → `Audiences`, `Issuer` → `IssuerURL`)
- Any existing CRs using KA `JWTProviders` must be updated

**Confidence**: 90% (chosen)

### Alternative B: Create separate `AFJWTProviderSpec` type for AF ❌ Rejected

**Approach**: Keep existing `JWTProviderSpec` unchanged for KA. Create a new `AFJWTProviderSpec` with the upstream-aligned fields for AF only.

**Pros**:
- No breaking change to existing KA `JWTProviderSpec`
- Independent evolution of KA and AF JWT configs

**Cons**:
- Two near-identical types diverge over time
- The existing `JWTProviderSpec` remains misaligned with upstream
- Increased maintenance burden and code duplication
- Violates the "AVOID duplication and REUSE existing code" principle

**Confidence**: 30% (rejected)

### Alternative C: Add `jwtProviders` to AF only, auto-convert legacy AF fields ❌ Rejected

**Approach**: Keep existing single-provider AF fields. Add `jwtProviders` as an alternative. Auto-convert legacy fields into a synthetic single-provider entry at runtime.

**Pros**:
- Full backward compatibility for AF single-provider config

**Cons**:
- Backward compatibility is not required (pre-release)
- Adds complexity for a transition that serves no users
- Legacy fields create ambiguity (which takes precedence?)

**Confidence**: 20% (rejected)

---

## Decision

### Chosen: Alternative A — Modify existing `JWTProviderSpec`, reuse for both KA and AF

The pre-release status eliminates the primary objection (breaking changes). A single type aligned with upstream ensures long-term maintainability and prevents the type drift that would inevitably occur with Alternative B.

### Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                     Kubernaut CR (CRD)                            │
│                                                                   │
│  spec.kubernautAgent.interactive.jwtProviders: []JWTProviderSpec  │
│  spec.apiFrontend.auth.jwtProviders:           []JWTProviderSpec  │
│                                                                   │
│  JWTProviderSpec ──► shared type (name, issuerURL, jwksURL,       │
│                       audiences[], claimMappings{})               │
└─────────────────────────┬─────────────────────────────────────────┘
                          │ Reconciler
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│                    ConfigMap Generation                          │
│                                                                  │
│  kubernaut-agent-config.yaml:                                    │
│    interactive.jwtProviders[] → KA runtime JWKS validation       │
│                                                                  │
│  apifrontend-config.yaml:                                        │
│    auth.jwtProviders[] → AF multi-provider JWT validation        │
│    auth.issuerURL      → legacy single-provider (when no         │
│                           jwtProviders configured)               │
└──────────────────────────────────────────────────────────────────┘
```

### Implementation Details

#### 1. CRD Types (`api/v1alpha1/kubernaut_types.go`)

**New type**:

```go
type ClaimMappingsSpec struct {
    // Claim name for username extraction.
    // +optional
    Username string `json:"username,omitempty"`
    // Claim name for group membership extraction.
    // +optional
    Groups string `json:"groups,omitempty"`
}
```

**Modified `JWTProviderSpec`**:

```go
type JWTProviderSpec struct {
    // Human-readable name for this provider (e.g. "rhbk", "spire").
    // +kubebuilder:validation:MinLength=1
    // +kubebuilder:validation:MaxLength=63
    Name string `json:"name"`

    // OIDC issuer URL. Must be non-empty.
    // +kubebuilder:validation:MinLength=1
    IssuerURL string `json:"issuerURL"`

    // JWKS endpoint URL. When empty, derived from issuerURL.
    // Must use HTTPS unless allowInsecureJWKS/allowInsecureIssuers is true.
    // +kubebuilder:validation:MaxLength=2048
    // +optional
    JWKSURL string `json:"jwksURL,omitempty"`

    // Expected audience claim values. At least one required.
    // +kubebuilder:validation:MinItems=1
    Audiences []string `json:"audiences"`

    // Claim mappings for username and group extraction.
    // +optional
    ClaimMappings *ClaimMappingsSpec `json:"claimMappings,omitempty"`
}
```

**New field in `APIFrontendAuthSpec`**:

```go
type APIFrontendAuthSpec struct {
    // existing fields remain for single-provider / kagenti-detected config...
    IssuerURL             string `json:"issuerURL,omitempty"`
    Audience              string `json:"audience,omitempty"`
    TokenReviewAudience   string `json:"tokenReviewAudience,omitempty"`
    JWKSURL               string `json:"jwksURL,omitempty"`
    OIDCCAFile            string `json:"oidcCaFile,omitempty"`
    AllowInsecureIssuers  bool   `json:"allowInsecureIssuers,omitempty"`

    // Multi-provider JWT configuration. When non-empty, the AF validates
    // tokens against all configured providers. Takes precedence over the
    // single-provider issuerURL/audience/jwksURL fields above.
    // +optional
    // +kubebuilder:validation:MaxItems=8
    JWTProviders []JWTProviderSpec `json:"jwtProviders,omitempty"`
}
```

#### 2. ConfigMap YAML types (`internal/resources/configmaps.go`)

**New YAML structs**:

```go
type afJWTProviderYAML struct {
    Name          string                `json:"name" yaml:"name"`
    IssuerURL     string                `json:"issuerURL" yaml:"issuerURL"`
    JWKSURL       string                `json:"jwksURL,omitempty" yaml:"jwksURL,omitempty"`
    Audiences     []string              `json:"audiences" yaml:"audiences"`
    ClaimMappings *afClaimMappingsYAML  `json:"claimMappings,omitempty" yaml:"claimMappings,omitempty"`
}

type afClaimMappingsYAML struct {
    Username string `json:"username,omitempty" yaml:"username,omitempty"`
    Groups   string `json:"groups,omitempty" yaml:"groups,omitempty"`
}
```

**Modified `afAuthYAML`** — add `JWTProviders` field:

```go
type afAuthYAML struct {
    // existing fields...
    IssuerURL             string              `json:"issuerURL" yaml:"issuerURL"`
    Audience              string              `json:"audience" yaml:"audience"`
    // ...
    JWTProviders          []afJWTProviderYAML  `json:"jwtProviders,omitempty" yaml:"jwtProviders,omitempty"`
}
```

**Modified `afAuthConfig()`** — populate `jwtProviders` when CR has them:

```go
func afAuthConfig(kn *kubernautv1alpha1.Kubernaut, oidc *KagentiOIDCDefaults) afAuthYAML {
    af := kn.Spec.APIFrontend
    // ... existing logic for issuer, jwks, insecure ...

    auth := afAuthYAML{
        IssuerURL: issuer,
        Audience:  withDefault(af.Auth.Audience, "kubernaut-apifrontend"),
        // ... existing fields ...
    }

    // Multi-provider JWT: map CR providers to YAML
    if len(af.Auth.JWTProviders) > 0 {
        providers := make([]afJWTProviderYAML, 0, len(af.Auth.JWTProviders))
        for _, p := range af.Auth.JWTProviders {
            yp := afJWTProviderYAML{
                Name:      p.Name,
                IssuerURL: p.IssuerURL,
                JWKSURL:   p.JWKSURL,
                Audiences: p.Audiences,
            }
            if p.ClaimMappings != nil {
                yp.ClaimMappings = &afClaimMappingsYAML{
                    Username: p.ClaimMappings.Username,
                    Groups:   p.ClaimMappings.Groups,
                }
            }
            providers = append(providers, yp)
        }
        auth.JWTProviders = providers
    }

    // ... replay cache ...
    return auth
}
```

**Modified KA interactive YAML** — align with new field names:

The existing `kaInteractiveYAML` does not emit `jwtProviders` into the KA config; JWT provider configuration for KA is consumed via separate validation. No KA ConfigMap changes needed unless upstream KA config also expects the new field schema (verify during implementation).

#### 3. Validation (`internal/resources/validation.go`)

**Generalize `validateJWTProviders`** to accept a provider slice and context path:

```go
func validateJWTProviderList(providers []kubernautv1alpha1.JWTProviderSpec, basePath string, allowInsecure bool) []error {
    var errs []error
    seen := make(map[string]bool, len(providers))

    for i, p := range providers {
        path := fmt.Sprintf("%s[%d]", basePath, i)

        // Unique name
        if seen[p.Name] {
            errs = append(errs, fmt.Errorf("%s.name: duplicate provider name %q", path, p.Name))
        }
        seen[p.Name] = true

        // Non-empty issuerURL
        if p.IssuerURL == "" {
            errs = append(errs, fmt.Errorf("%s.issuerURL: required", path))
        }

        // At least one audience
        if len(p.Audiences) == 0 {
            errs = append(errs, fmt.Errorf("%s.audiences: at least one audience required", path))
        }

        // JWKS URL validation (when set)
        if p.JWKSURL != "" {
            // length, parse, scheme checks...
        }
    }
    return errs
}
```

**Call sites**:
- `validateJWKSProviders` (KA) → delegates to `validateJWTProviderList`
- New `validateAPIFrontend` logic → calls `validateJWTProviderList` for AF providers

**Interaction with single-provider `issuerURL`**:
- When `jwtProviders` is non-empty, the single-provider `issuerURL` requirement is relaxed (multi-provider takes precedence).
- When both `jwtProviders` is empty and `issuerURL` is empty (and kagenti is not active), validation fails.

#### 4. CRD Regeneration

Run `make generate && make manifests` to regenerate `config/crd/bases/kubernaut.ai_kubernauts.yaml`.

---

## Consequences

### Positive Consequences
1. Operator CRD matches upstream AF's multi-provider JWT capability
2. Single `JWTProviderSpec` type eliminates drift between KA and AF
3. Enables SPIRE + Keycloak concurrent authentication for FedRAMP multi-factor compliance (IA-2)
4. `ClaimMappings` enables fine-grained RBAC based on OIDC claims (AC-6)

### Negative Consequences
1. Breaking change to `JWTProviderSpec` (`Audience` → `Audiences`, `Issuer` → `IssuerURL`)
   - **Mitigation**: Pre-release API — no external consumers affected. KA test fixtures updated in the same PR.

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| KA upstream config schema diverges from AF | Low | Medium | Both consume the same `JWTProviderConfig` upstream type |
| Existing dev cluster CRs break | Medium | Low | No production users; dev CRs updated manually |

---

## Compliance

| Requirement | Status | Notes |
|-------------|--------|-------|
| FedRAMP IA-2 | ✅ | Multi-provider JWT enables multi-factor/multi-source auth |
| FedRAMP SC-23 | ✅ | Per-provider audience binding ensures session authenticity |
| FedRAMP AC-6 | ✅ | ClaimMappings enables group-based tool authorization |
| FedRAMP CM-6 | ✅ | All auth config declaratively managed via CRD |

---

## Validation Strategy

1. **Unit tests**: Verify `afAuthConfig()` emits correct YAML with 0, 1, and 2+ providers
2. **Unit tests**: Verify `validateJWTProviderList` rejects invalid configs (empty issuerURL, empty audiences, duplicate names, non-HTTPS JWKS)
3. **Unit tests**: Verify KA validation still works after `JWTProviderSpec` field renames
4. **Integration test**: Apply CR with multi-provider config, verify AF ConfigMap content
5. **E2E**: Deploy with Keycloak + SPIRE providers, verify AF accepts tokens from both

---

## Affected Files

| File | Change |
|------|--------|
| `api/v1alpha1/kubernaut_types.go` | Add `ClaimMappingsSpec`, modify `JWTProviderSpec`, add `JWTProviders` to `APIFrontendAuthSpec` |
| `internal/resources/configmaps.go` | Add YAML types, update `afAuthConfig()` |
| `internal/resources/validation.go` | Generalize `validateJWKSProviders`, add AF provider validation |
| `config/crd/bases/kubernaut.ai_kubernauts.yaml` | Regenerated |
| Unit test files | Update for renamed fields, add AF provider tests |

---

## References

- Upstream AF config: `kubernaut-v1.5/pkg/apifrontend/config/config.go` (`JWTProviderConfig`, `ConfigClaimMappings`)
- Upstream Helm template: `kubernaut-v1.5/charts/kubernaut/templates/apifrontend/apifrontend.yaml`
- Upstream values: `kubernaut-v1.5/charts/kubernaut/values.yaml` (commented jwtProviders examples)
- Operator issue: [kubernaut-operator#174](https://github.com/jordigilh/kubernaut-operator/issues/174)

---

**Document Version**: 1.0
**Last Updated**: 2026-06-16

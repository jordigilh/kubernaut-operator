# ADR-AUTH-001 Implementation Plan (APDC)

**Issue**: [kubernaut-operator#174](https://github.com/jordigilh/kubernaut-operator/issues/174)
**ADR**: [ADR-AUTH-001-multi-provider-jwt-apifrontend.md](ADR-AUTH-001-multi-provider-jwt-apifrontend.md)
**Methodology**: APDC (Analysis-Plan-Do-Check)
**Estimated effort**: 1 day

---

## Phase 1: Analysis ✅ Complete

- Upstream `JWTProviderConfig` contract verified in `kubernaut-v1.5/pkg/apifrontend/config/config.go`
- Upstream Helm chart templates and values confirmed the field schema
- Existing operator CRD, ConfigMap generation, and validation reviewed
- Decision: no backward compatibility, modify `JWTProviderSpec` in place (ADR-AUTH-001)

---

## Phase 2: Plan ✅ This Document

### Business Requirements

| BR | Description | Test Coverage |
|----|-------------|---------------|
| BR-SECURITY-001 | Multi-provider JWT auth (FedRAMP IA-2) | UT: multi-provider ConfigMap rendering |
| BR-SECURITY-002 | Audience binding per provider (FedRAMP SC-23) | UT: audiences field validation |
| BR-API-001 | CRD alignment with upstream config | UT: field names match upstream YAML keys |

### File Change Map

| # | File | Change | TDD Phase |
|---|------|--------|-----------|
| 1 | `api/v1alpha1/kubernaut_types.go` | Add `ClaimMappingsSpec`, modify `JWTProviderSpec` (Audiences, IssuerURL, ClaimMappings), add `JWTProviders` to `APIFrontendAuthSpec` | GREEN (types needed for tests to compile) |
| 2 | `internal/resources/configmaps.go` | Add `afJWTProviderYAML`, `afClaimMappingsYAML`; add `JWTProviders` to `afAuthYAML`; update `afAuthConfig()` | GREEN |
| 3 | `internal/resources/validation.go` | Extract `validateJWTProviderList()`; update `validateJWKSProviders()` to delegate; add AF provider validation in `validateAPIFrontend()` | GREEN |
| 4 | `internal/resources/configmaps_test.go` | New tests for AF multi-provider rendering; update existing tests for field renames | RED (first) |
| 5 | `internal/resources/validation_test.go` | New tests for AF provider validation; update existing KA JWT tests for field renames | RED (first) |
| 6 | `config/crd/bases/kubernaut.ai_kubernauts.yaml` | Regenerated via `make manifests` | CHECK |

---

## Phase 3: Do

### Step 3.1 — DO-RED: Write failing tests first

#### 3.1.1 Update existing KA JWT validation tests (field renames)

In `validation_test.go`, update all `JWTProviderSpec` literals:
- `Audience: "..."` → `Audiences: []string{"..."}`
- `Issuer: "..."` → `IssuerURL: "..."`
- Add `IssuerURL` field where missing (currently optional, now required)

Tests should fail to compile until Step 3.2 applies the type changes.

#### 3.1.2 New AF multi-provider ConfigMap tests

Add to `configmaps_test.go`:

```ginkgo
Describe("APIFrontendConfigMap multi-provider JWT", func() {
    It("renders jwtProviders when CR has multiple providers", func() {
        // CR with 2 JWTProviders → config.yaml contains jwtProviders array
    })

    It("omits jwtProviders when CR has none", func() {
        // Existing single-provider path → no jwtProviders key in YAML
    })

    It("renders claimMappings when configured", func() {
        // Provider with ClaimMappings → YAML includes username/groups
    })

    It("omits claimMappings when nil", func() {
        // Provider without ClaimMappings → no claimMappings key
    })
})
```

#### 3.1.3 New AF provider validation tests

Add to `validation_test.go`:

```ginkgo
Describe("APIFrontend JWT provider validation", func() {
    It("accepts valid multi-provider config", func() {
        // 2 providers with distinct names, valid issuerURLs, non-empty audiences → no errors
    })

    It("rejects provider with empty issuerURL", func() {})

    It("rejects provider with empty audiences", func() {})

    It("rejects duplicate provider names", func() {})

    It("rejects non-HTTPS jwksURL without allowInsecureIssuers", func() {})

    It("accepts HTTP jwksURL with allowInsecureIssuers=true", func() {})

    It("relaxes issuerURL requirement when jwtProviders is non-empty", func() {
        // AF with jwtProviders but no top-level issuerURL → no validation error
    })
})
```

### CHECKPOINT A: Type Reference Validation
- Read `JWTProviderSpec` definition before modifying
- Read `afAuthYAML` definition before adding fields
- Confirm field names match upstream `JWTProviderConfig`

---

### Step 3.2 — DO-GREEN: Minimal code to make tests pass

#### 3.2.1 CRD types (`kubernaut_types.go`)

1. Add `ClaimMappingsSpec` struct
2. Modify `JWTProviderSpec`:
   - Remove `Audience string` field
   - Remove `Issuer string` field
   - Add `IssuerURL string` field with `+kubebuilder:validation:MinLength=1`
   - Add `Audiences []string` field with `+kubebuilder:validation:MinItems=1`
   - Add `ClaimMappings *ClaimMappingsSpec` field
3. Add `JWTProviders []JWTProviderSpec` to `APIFrontendAuthSpec`

#### 3.2.2 ConfigMap generation (`configmaps.go`)

1. Add `afJWTProviderYAML` and `afClaimMappingsYAML` YAML structs
2. Add `JWTProviders []afJWTProviderYAML` to `afAuthYAML`
3. Update `afAuthConfig()` to populate `JWTProviders` when `af.Auth.JWTProviders` is non-empty

#### 3.2.3 Validation (`validation.go`)

1. Extract shared `validateJWTProviderList(providers, basePath, allowInsecure)` function
2. Update `validateJWKSProviders()` to:
   - Use `p.IssuerURL` instead of `p.Issuer` (if applicable)
   - Validate `p.Audiences` non-empty
   - Validate `p.IssuerURL` non-empty
   - Validate unique names
   - Delegate URL checks to `validateJWTProviderList`
3. Add AF provider validation in `validateAPIFrontend()`:
   - Call `validateJWTProviderList` for `af.Auth.JWTProviders`
   - Relax `issuerURL` requirement when `jwtProviders` is non-empty

### CHECKPOINT B: Test Compilation & Execution
- All tests compile
- New RED tests now pass (GREEN)
- Existing tests pass with updated field names

---

### Step 3.3 — DO-REFACTOR

1. Ensure `validateJWKSProviders` (KA) and AF validation share the same `validateJWTProviderList` function — no duplicated logic
2. Verify YAML struct field ordering matches upstream config schema
3. Run `golangci-lint` for any new lint issues

### CHECKPOINT C: Business Integration Validation
- Verify `ValidateKubernaut()` calls AF provider validation
- Verify `APIFrontendConfigMap()` renders providers correctly
- Verify no orphaned code from the old `Audience`/`Issuer` fields

---

## Phase 4: Check

### 4.1 Build Validation
```bash
make build
make test
make lint
```

### 4.2 CRD Regeneration
```bash
make generate
make manifests
```
Verify `config/crd/bases/kubernaut.ai_kubernauts.yaml` includes:
- `jwtProviders` under `apiFrontend.auth`
- `audiences` (array) replacing `audience` (string) in `JWTProviderSpec`
- `issuerURL` replacing `issuer` in `JWTProviderSpec`
- `claimMappings` with `username`/`groups` under each provider

### 4.3 Confidence Assessment

| Criteria | Target |
|----------|--------|
| Build passes | ✅ |
| All tests pass | ✅ |
| No lint errors | ✅ |
| CRD schema correct | ✅ |
| Upstream field alignment | ✅ |
| Confidence | ≥80% |

---

## Wiring Manifest

| Source | Target | Wiring |
|--------|--------|--------|
| `kubernaut_types.go:JWTProviderSpec` | `configmaps.go:afJWTProviderYAML` | `afAuthConfig()` maps CR → YAML |
| `kubernaut_types.go:APIFrontendAuthSpec.JWTProviders` | `configmaps.go:afAuthYAML.JWTProviders` | Same mapping |
| `kubernaut_types.go:JWTProviderSpec` | `validation.go:validateJWTProviderList()` | Called from `validateAPIFrontend()` and `validateJWKSProviders()` |
| `configmaps.go:afAuthYAML` | AF service `config.yaml` | ConfigMap mounted into AF pod |

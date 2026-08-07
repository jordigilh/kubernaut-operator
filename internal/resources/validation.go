/*
Copyright 2026 Jordi Gil.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const maxJWKSURLLength = 2048

// ValidateKubernaut runs all CR-level validations and returns accumulated errors.
// sidecar indicates whether kagenti is active; when it is, issuerURL is not
// required because the operator auto-detects it from kagenti's authbridge-config.
func ValidateKubernaut(kn *kubernautv1alpha1.Kubernaut, sidecar KagentiSidecarMode) []error {
	errs := make([]error, 0, 4)
	errs = append(errs, validatePostgreSQLSSLMode(kn)...)
	errs = append(errs, validatePolicyPrerequisites(kn)...)
	errs = append(errs, validateLLMProfiles(kn)...)
	errs = append(errs, validateJWKSProviders(kn)...)
	errs = append(errs, validateAPIFrontend(kn, sidecar)...)
	errs = append(errs, validateAlignmentCheck(kn)...)
	errs = append(errs, validateDryRun(kn)...)
	errs = append(errs, validateInteractive(kn)...)
	return errs
}

// ValidateFleet runs Fleet-specific validations against the v1alpha2 view of
// the CR. Fleet's entire CRD surface lives in v1alpha2 (Fleet v1alpha2
// migration) -- called alongside, not merged into, ValidateKubernaut so the
// latter's v1alpha1-only signature stays stable for its other callers.
func ValidateFleet(knV2 *kubernautv1alpha2.Kubernaut) []error {
	errs := make([]error, 0, 2)
	errs = append(errs, validateFleetConfig(knV2)...)
	errs = append(errs, validateFleetMetadataCache(knV2)...)
	return errs
}

func validatePostgreSQLSSLMode(kn *kubernautv1alpha1.Kubernaut) []error {
	mode := strings.ToLower(kn.Spec.PostgreSQL.SSLMode)
	if mode == "disable" {
		return []error{fmt.Errorf(
			"spec.postgresql.sslMode: \"disable\" is rejected (FedRAMP SC-8); use \"verify-full\" or \"verify-ca\"")}
	}
	return nil
}

func validatePolicyPrerequisites(_ *kubernautv1alpha1.Kubernaut) []error {
	return nil
}

// validateLLMProfiles validates every named profile's own content, then the
// referential integrity and same-credentialsSecretName constraints for every
// component that references a profile by name.
func validateLLMProfiles(kn *kubernautv1alpha1.Kubernaut) []error {
	errs := make([]error, 0, len(kn.Spec.LLMProfiles))
	for name, profile := range kn.Spec.LLMProfiles {
		errs = append(errs, validateLLMProfileContent(name, &profile)...)
	}
	errs = append(errs, validateLLMProfileRefs(kn)...)
	return errs
}

// validateLLMProfileContent validates a single named profile's own fields.
// These checks mirror the pre-refactor validateLLMPrerequisites, now scoped
// per-profile instead of to the single spec.kubernautAgent.llm block.
func validateLLMProfileContent(name string, llm *kubernautv1alpha1.LLMProfileSpec) []error {
	var errs []error
	base := fmt.Sprintf("spec.llmProfiles[%q]", name)
	if llm.Provider == "" {
		errs = append(errs, fmt.Errorf("%s.provider: required — specify the LLM provider (e.g. \"openai\", \"vertexai\")", base))
	}
	if llm.Model == "" {
		errs = append(errs, fmt.Errorf("%s.model: required — specify the LLM model name (e.g. \"gpt-4o\" for provider \"openai\", \"claude-sonnet-5\" for provider \"vertexai\"/\"anthropic\")", base))
	}
	if llm.CredentialsSecretName == "" {
		errs = append(errs, fmt.Errorf("%s.credentialsSecretName: required — provide a Secret with LLM API credentials", base))
	}
	if llm.Provider == LLMProviderOpenAI && llm.Endpoint == "" {
		errs = append(errs, fmt.Errorf("%s.endpoint: required when provider is %q — both KA and AF need an explicit endpoint for OpenAI", base, LLMProviderOpenAI))
	}
	certSet := llm.TLSCertFile != "" || llm.TLSKeyFile != ""
	if certSet && (llm.TLSCertFile == "" || llm.TLSKeyFile == "") {
		errs = append(errs, fmt.Errorf("%s.tlsCertFile and %s.tlsKeyFile must both be set or both empty for mTLS", base, base))
	}
	if certSet && llm.TLSClientSecretRef == "" {
		errs = append(errs, fmt.Errorf("%s.tlsClientSecretRef: required when mTLS cert/key paths are configured", base))
	}
	if !certSet && llm.TLSClientSecretRef != "" {
		errs = append(errs, fmt.Errorf("%s.tlsClientSecretRef: set but tlsCertFile/tlsKeyFile are empty — both pairs are required for mTLS", base))
	}
	if err := validateLLMReasoning(base, llm.Reasoning, llm.Provider); err != nil {
		errs = append(errs, err)
	}
	return errs
}

// validateLLMReasoning rejects effort: "none" combined with enabled: true
// for Anthropic-family providers, mirroring upstream's
// pkg/shared/types.ValidateReasoningConfig verbatim (verified against
// kubernaut@v1.6.0-rc1.0.20260723152521-56562f9f1adb) — Anthropic has no
// "thinking enabled, zero effort" wire state, so this is a genuine
// contradiction rather than a gracefully-degradable value. Left undetected,
// the CR reconciles cleanly but every affected LLM call fails at runtime.
//
// Unlike upstream, this does not re-validate r.Effort against the allowed
// set: the CRD's kubebuilder Enum marker on LLMReasoningSpec.Effort already
// rejects out-of-set values at admission, before this function ever runs.
//
// r may be nil (no reasoning configured on this profile).
func validateLLMReasoning(base string, r *kubernautv1alpha1.LLMReasoningSpec, provider string) error {
	if r == nil {
		return nil
	}
	if r.Enabled && r.Effort == "none" && anthropicFamilyReasoningProviders[provider] {
		return fmt.Errorf(
			"%s.reasoning: effort: \"none\" is not supported for provider %q while reasoning is enabled "+
				"(Anthropic has no \"thinking enabled with zero effort\" state) — use enabled: false to fully "+
				"disable reasoning, or effort: \"minimal\" for Anthropic's lowest real tier",
			base, provider)
	}
	return nil
}

// validPhaseModelKeys enumerates the agent phases that support per-phase LLM overrides.
var validPhaseModelKeys = map[string]bool{
	"rca":                true,
	"workflow_discovery": true,
	"validation":         true,
}

// lookupProfileRef checks that ref matches a key in profiles for the field
// at fieldPath, and produces the standard "undefined profile" error shared
// by every llmProfileRef/phaseModels field when it doesn't -- so the
// message stays consistent without re-deriving it at each call site.
//
// allowEmpty controls how an empty ref is treated: fields like
// apiFrontend.llmProfileRef fall back to another profile when empty (not an
// error), while phaseModels values name no profile at all when empty and
// are rejected the same as any other non-matching name.
func lookupProfileRef(profiles map[string]kubernautv1alpha1.LLMProfileSpec, ref, fieldPath string, allowEmpty bool) error {
	if ref == "" && allowEmpty {
		return nil
	}
	if _, ok := profiles[ref]; !ok {
		return fmt.Errorf(
			"%s: references undefined profile %q — must match a key in spec.llmProfiles", fieldPath, ref)
	}
	return nil
}

// validateLLMProfileRefs validates that every llmProfileRef (KA, AF,
// AF severity-triage) and every phaseModels value points at a profile
// defined in spec.llmProfiles, and enforces the same-credentialsSecretName
// constraint on severity-triage relative to the profile it would otherwise
// inherit from.
//
// #233: phaseModels is deliberately exempt from that constraint. It
// originally existed because KA's LLMOverrideConfig silently reused the
// base profile's already-resolved API key for every phase override,
// regardless of what credentialsSecretName the phase's own profile named
// (kubernaut#1726) — so representing independent phase credentials in the
// CRD would have produced a config the operator could validate but KA
// would silently misauthenticate. kubernaut#1728 fixed that by resolving
// each phase's own APIKeyFile independently, so cross-credential (and
// cross-provider) phase overrides are now safe to accept.
func validateLLMProfileRefs(kn *kubernautv1alpha1.Kubernaut) []error {
	var errs []error
	profiles := kn.Spec.LLMProfiles

	kaRef := EffectiveKALLMProfileRef(kn)
	const kaBase = "spec.kubernautAgent.llmProfileRef"
	if kaRef == "" {
		// EffectiveKALLMProfileRef only returns "" when the raw field is
		// empty AND spec.llmProfiles doesn't have exactly one entry to
		// infer from -- an explicit ref is unambiguous with any other
		// count, so the two counts get distinct messages.
		if len(profiles) == 0 {
			errs = append(errs, fmt.Errorf("%s: required — spec.llmProfiles defines no profiles to infer from", kaBase))
		} else {
			errs = append(errs, fmt.Errorf(
				"%s: required — spec.llmProfiles defines %d profiles, so the profile is ambiguous without an explicit reference (auto-inferred only when exactly one profile is defined)",
				kaBase, len(profiles)))
		}
	}
	if err := lookupProfileRef(profiles, kaRef, kaBase, true); err != nil {
		errs = append(errs, err)
	}

	for phase, ref := range kn.Spec.KubernautAgent.PhaseModels {
		phaseBase := fmt.Sprintf("spec.kubernautAgent.phaseModels[%q]", phase)
		if !validPhaseModelKeys[phase] {
			errs = append(errs, fmt.Errorf("%s: invalid phase key %q — must be one of: rca, workflow_discovery, validation", phaseBase, phase))
			continue
		}
		if err := lookupProfileRef(profiles, ref, phaseBase, false); err != nil {
			errs = append(errs, err)
		}
	}

	if kn.Spec.APIFrontendEnabled() {
		errs = append(errs, validateAFLLMProfileRefs(kn, profiles)...)
	}

	return errs
}

// validateAFLLMProfileRefs validates that both of API Frontend's own
// llmProfileRef fields resolve to defined profiles: its main agent
// connection (apiFrontend.llmProfileRef) and its independent
// severity-triage override (apiFrontend.severityTriage.llmProfileRef).
//
// #234 unblocked cross-credential severity-triage overrides for every
// provider except vertex_ai-vs-vertex_ai, because AF's Vertex AI client
// construction relied solely on ambient Application Default Credentials
// rather than a profile's own APIKey/APIKeyFile (kubernaut#1731). #279
// removes that restriction now that kubernaut#1731 is fixed upstream and
// afAgentLLMConfig/afSeverityTriageConfig (configmaps.go) render an
// explicit apiKeyFile for vertex_ai profiles too, mirroring every other
// provider -- cross-credential vertex_ai-vs-vertex_ai overrides are safe
// like any other combination.
func validateAFLLMProfileRefs(kn *kubernautv1alpha1.Kubernaut, profiles map[string]kubernautv1alpha1.LLMProfileSpec) []error {
	var errs []error

	if err := lookupProfileRef(profiles, kn.Spec.APIFrontend.LLMProfileRef, "spec.apiFrontend.llmProfileRef", true); err != nil {
		errs = append(errs, err)
	}

	if st := kn.Spec.APIFrontend.SeverityTriage; st != nil {
		if err := lookupProfileRef(profiles, st.LLMProfileRef, "spec.apiFrontend.severityTriage.llmProfileRef", true); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// validateJWTProviderList validates a slice of JWT providers, checking issuerURL,
// audiences, name uniqueness, and JWKS URL validity. The insecureFlagPath
// parameter is used in error messages to reference the correct parent flag
// (e.g. "spec.kubernautAgent.interactive.allowInsecureJWKS").
func validateJWTProviderList(providers []kubernautv1alpha1.JWTProviderSpec, basePath string, allowInsecure bool, insecureFlagPath string) []error {
	var errs []error
	seen := make(map[string]bool, len(providers))

	for i, p := range providers {
		path := fmt.Sprintf("%s.jwtProviders[%d]", basePath, i)

		if seen[p.Name] {
			errs = append(errs, fmt.Errorf("%s.name: duplicate provider name %q", path, p.Name))
		}
		seen[p.Name] = true

		if p.IssuerURL == "" {
			errs = append(errs, fmt.Errorf("%s.issuerURL: required", path))
		}

		if len(p.Audiences) == 0 {
			errs = append(errs, fmt.Errorf("%s.audiences: at least one audience required", path))
		}

		if p.JWKSURL == "" {
			continue
		}

		if len(p.JWKSURL) > maxJWKSURLLength {
			errs = append(errs, fmt.Errorf("%s.jwksURL: must be <= %d characters (got %d)", path, maxJWKSURLLength, len(p.JWKSURL)))
			continue
		}

		u, err := url.Parse(p.JWKSURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s.jwksURL: invalid URL: %w", path, err))
			continue
		}
		if u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s.jwksURL: must be an absolute URL with scheme and host", path))
			continue
		}

		if !allowInsecure && strings.ToLower(u.Scheme) != "https" {
			errs = append(errs, fmt.Errorf(
				"%s.jwksURL: scheme must be https (got %q); set %s=true to permit HTTP for dev/test",
				path, u.Scheme, insecureFlagPath))
		}
	}
	return errs
}

func validateJWKSProviders(kn *kubernautv1alpha1.Kubernaut) []error {
	interactive := kn.Spec.KubernautAgent.Interactive
	if interactive == nil || len(interactive.JWTProviders) == 0 {
		return nil
	}
	return validateJWTProviderList(
		interactive.JWTProviders,
		"spec.kubernautAgent.interactive",
		interactive.AllowInsecureJWKS,
		"spec.kubernautAgent.interactive.allowInsecureJWKS",
	)
}

func validateAPIFrontend(kn *kubernautv1alpha1.Kubernaut, sidecar KagentiSidecarMode) []error {
	if !kn.Spec.APIFrontendEnabled() {
		return nil
	}
	var errs []error

	af := kn.Spec.APIFrontend
	if af.AgentCardURL != "" {
		u, err := url.Parse(af.AgentCardURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("spec.apiFrontend.agentCardURL: must be a valid URL with scheme and host"))
		}
	}

	// IA-2: When jwtProviders is configured, multi-provider JWT auth
	// satisfies the authentication requirement — top-level issuerURL is
	// not needed. When kagenti sidecar is active, issuerURL is
	// auto-detected. Only require issuerURL when neither source is available.
	hasMultiProvider := len(af.Auth.JWTProviders) > 0
	if af.Auth.IssuerURL == "" && sidecar == KagentiSidecarNone && !hasMultiProvider {
		errs = append(errs, fmt.Errorf(
			"spec.apiFrontend.auth.issuerURL: required — API Frontend requires OAuth/OIDC authentication (FedRAMP IA-2, CM-6)"))
	}

	// Deprecated field, but still supported for backward compatibility until
	// removed from the API -- must keep validating it while it's honored.
	if ref := af.RBACRolesConfigMapRef; ref != nil && ref.ConfigMapName == "" { //nolint:staticcheck
		errs = append(errs, fmt.Errorf("spec.apiFrontend.rbacRolesConfigMapRef.configMapName: must not be empty when rbacRolesConfigMapRef is set"))
	}

	errs = append(errs, validateToolRoleBindings(kn)...)

	if hasMultiProvider {
		errs = append(errs, validateJWTProviderList(
			af.Auth.JWTProviders,
			"spec.apiFrontend.auth",
			af.Auth.AllowInsecureIssuers,
			"spec.apiFrontend.auth.allowInsecureIssuers",
		)...)
	}

	return errs
}

// validToolPersonas is the set of known persona names for tool role bindings.
var validToolPersonas = map[string]bool{
	"sre": true, "ai-orchestrator": true, "cicd": true,
	"observability": true, "l3-audit": true, "remediation-approver": true,
}

func validateToolRoleBindings(kn *kubernautv1alpha1.Kubernaut) []error {
	rbac := kn.Spec.APIFrontend.RBAC
	if rbac == nil {
		return nil
	}

	var errs []error

	if rbac.SARCacheTTL != "" {
		if _, err := time.ParseDuration(rbac.SARCacheTTL); err != nil {
			errs = append(errs, fmt.Errorf("spec.apiFrontend.rbac.sarCacheTTL: invalid Go duration %q: %w", rbac.SARCacheTTL, err))
		}
	}

	seen := make(map[string]bool, len(rbac.RoleBindings))
	for i, rb := range rbac.RoleBindings {
		if err := validateToolRoleBinding(rb, i, seen); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// validateToolRoleBinding validates a single spec.apiFrontend.rbac.roleBindings
// entry: exactly one of role/clusterRoleName must be set, it must not
// duplicate an earlier entry (tracked via seen), and a persona-based role
// must be a known persona.
func validateToolRoleBinding(rb kubernautv1alpha1.ToolRoleBinding, index int, seen map[string]bool) error {
	if rb.Role != "" && rb.ClusterRoleName != "" {
		return fmt.Errorf("spec.apiFrontend.rbac.roleBindings[%d]: role and clusterRoleName are mutually exclusive", index)
	}
	if rb.Role == "" && rb.ClusterRoleName == "" {
		return fmt.Errorf("spec.apiFrontend.rbac.roleBindings[%d]: one of role or clusterRoleName must be set", index)
	}

	if rb.Role == "" {
		if seen[rb.ClusterRoleName] {
			return fmt.Errorf("spec.apiFrontend.rbac.roleBindings: duplicate clusterRoleName %q", rb.ClusterRoleName)
		}
		seen[rb.ClusterRoleName] = true
		return nil
	}

	if seen[rb.Role] {
		return fmt.Errorf("spec.apiFrontend.rbac.roleBindings: duplicate role %q", rb.Role)
	}
	seen[rb.Role] = true
	if !validToolPersonas[rb.Role] {
		return fmt.Errorf("spec.apiFrontend.rbac.roleBindings: unknown persona %q", rb.Role)
	}
	return nil
}

const (
	alignmentCheckTimeoutMin = time.Second
	alignmentCheckTimeoutMax = 60 * time.Second
)

func validateAlignmentCheck(kn *kubernautv1alpha1.Kubernaut) []error {
	ac := &kn.Spec.KubernautAgent.AlignmentCheck
	if !ac.Enabled {
		return nil
	}

	var errs []error
	const base = "spec.kubernautAgent.alignmentCheck"

	if ac.Timeout != "" {
		d, err := time.ParseDuration(ac.Timeout)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s.timeout: invalid Go duration %q: %w", base, ac.Timeout, err))
		} else if d < alignmentCheckTimeoutMin || d > alignmentCheckTimeoutMax {
			errs = append(errs, fmt.Errorf("%s.timeout: must be between %s and %s, got %s", base, alignmentCheckTimeoutMin, alignmentCheckTimeoutMax, d))
		}
	}

	if ac.MaxStepTokens < 0 {
		errs = append(errs, fmt.Errorf("%s.maxStepTokens: must be positive, got %d", base, ac.MaxStepTokens))
	}

	if llm := ac.LLM; llm != nil {
		if llm.Provider == "" {
			errs = append(errs, fmt.Errorf("%s.llm.provider: must not be empty when llm is set", base))
		}
		if llm.Model == "" {
			errs = append(errs, fmt.Errorf("%s.llm.model: must not be empty when llm is set", base))
		}
	}

	return errs
}

func validateDryRun(kn *kubernautv1alpha1.Kubernaut) []error {
	ro := &kn.Spec.RemediationOrchestrator
	if !ro.DryRun {
		return nil
	}

	if ro.DryRunHoldPeriod == "" {
		return nil
	}

	if _, err := time.ParseDuration(ro.DryRunHoldPeriod); err != nil {
		return []error{fmt.Errorf("spec.remediationOrchestrator.dryRunHoldPeriod: invalid Go duration %q: %w", ro.DryRunHoldPeriod, err)}
	}
	return nil
}

func validateInteractive(kn *kubernautv1alpha1.Kubernaut) []error {
	interactive := kn.Spec.KubernautAgent.Interactive
	if interactive == nil || !interactive.InteractiveEnabled() {
		return nil
	}

	var errs []error
	const base = "spec.kubernautAgent.interactive"

	if interactive.SessionTTL != "" {
		if _, err := time.ParseDuration(interactive.SessionTTL); err != nil {
			errs = append(errs, fmt.Errorf("%s.sessionTTL: invalid Go duration %q: %w", base, interactive.SessionTTL, err))
		}
	}

	if interactive.InactivityTimeout != "" {
		if _, err := time.ParseDuration(interactive.InactivityTimeout); err != nil {
			errs = append(errs, fmt.Errorf("%s.inactivityTimeout: invalid Go duration %q: %w", base, interactive.InactivityTimeout, err))
		}
	}

	return errs
}

// validFleetBackends are the only backends recognized by upstream
// pkg/fleet.FleetConfig (jordigilh/kubernaut). "valkey" was explicitly
// removed upstream and is rejected here for the same reason.
// validMCPGatewayTypes are the only MCP Gateway implementations recognized
// by upstream pkg/fleet/registry (jordigilh/kubernaut).
var validMCPGatewayTypes = map[string]bool{
	"eaigw":                true,
	mcpGatewayTypeKuadrant: true,
}

var validFleetBackends = map[string]bool{
	"fleetmetadatacache": true,
	"acm":                true,
}

// validateFleetConfig validates spec.fleet. When Enabled is false or
// omitted, the other fields are inert and left unvalidated so users can
// pre-stage configuration ahead of enabling it.
func validateFleetConfig(knV2 *kubernautv1alpha2.Kubernaut) []error {
	fleet := &knV2.Spec.Fleet
	if fleet.Enabled == nil || !*fleet.Enabled {
		return nil
	}

	var errs []error
	const base = "spec.fleet"

	if fleet.Backend == "" {
		errs = append(errs, fmt.Errorf("%s.backend: must not be empty when fleet.enabled is true — must be one of: fleetmetadatacache, acm", base))
	} else if !validFleetBackends[fleet.Backend] {
		errs = append(errs, fmt.Errorf("%s.backend: invalid backend %q — must be one of: fleetmetadatacache, acm", base, fleet.Backend))
	}

	// Endpoint is auto-derived (resolveFleetEndpoint) when the operator
	// itself is managing FMC (backend=fleetmetadatacache,
	// fleetMetadataCache.enabled=true) -- the user doesn't need to also
	// wire up FMC's in-cluster URL by hand. BYO FMC and backend=acm still
	// require an explicit endpoint.
	fmcManaged := fleet.Backend == "fleetmetadatacache" && knV2.Spec.FleetMetadataCacheEnabled()
	if fleet.Endpoint == "" && !fmcManaged {
		errs = append(errs, fmt.Errorf("%s.endpoint: must be set when fleet.enabled is true (unless backend=fleetmetadatacache and fleetMetadataCache.enabled=true, which auto-derives the operator-managed FMC's in-cluster URL)", base))
	}

	// FedRAMP IA-5 (authenticator management): upstream pkg/fleet has no
	// unauthenticated mode for the ACM Search GraphQL API. Without a token,
	// Gateway/RemediationOrchestrator crash-loop at startup instead of
	// failing fast here at admission.
	if fleet.Backend == "acm" && fleet.TokenSecretName == "" {
		errs = append(errs, fmt.Errorf("%s.tokenSecretName: must be set when fleet.backend is \"acm\" — the ACM Search GraphQL API requires bearer token authentication", base))
	}

	// #222: Gateway and RemediationOrchestrator both unconditionally call
	// upstream Fleet.ValidateFullFederation() at startup, which requires
	// mcpGatewayEndpoint+mcpGatewayType whenever fleet is enabled. Omitting
	// either crash-loops both components instead of failing fast here.
	if fleet.MCPGatewayEndpoint == "" {
		errs = append(errs, fmt.Errorf("%s.mcpGatewayEndpoint: must be set when fleet.enabled is true — Gateway and RemediationOrchestrator require it for remote-cluster reads (upstream Fleet.ValidateFullFederation)", base))
	}

	if fleet.MCPGatewayType == "" {
		errs = append(errs, fmt.Errorf("%s.mcpGatewayType: must be set when fleet.enabled is true — must be one of: eaigw, kuadrant", base))
	} else if !validMCPGatewayTypes[fleet.MCPGatewayType] {
		errs = append(errs, fmt.Errorf("%s.mcpGatewayType: invalid value %q — must be one of: eaigw, kuadrant", base, fleet.MCPGatewayType))
	}

	errs = append(errs, validateFleetOAuth2(knV2, fleet)...)

	return errs
}

// validateFleetOAuth2 validates spec.fleet.oauth2, mirroring upstream
// FleetConfig.Validate()'s pairing check: oauth2.enabled=true without both
// fields silently sends unauthenticated requests to the MCP Gateway instead
// of failing closed at startup.
//
// Fix (Fleet v1alpha2 migration, defense-in-depth alongside ADR-CRD-001
// F12's CEL rule): oauth2.enabled=false is no longer a silent early return
// when fleet.enabled=true and mcpGatewayEndpoint is set -- there is no
// unauthenticated mode for the MCP Gateway
// (kubernaut#1991/kubernaut#1992). The v1alpha2 CEL rule already blocks
// this at admission; this check is the reconcile-time backstop for the
// same invariant (e.g. a v1alpha2 CR created before the CEL rule shipped).
func validateFleetOAuth2(knV2 *kubernautv1alpha2.Kubernaut, fleet *kubernautv1alpha2.FleetSpec) []error {
	const base = "spec.fleet"
	if !fleet.OAuth2.Enabled {
		if fleet.MCPGatewayEndpoint != "" {
			return []error{fmt.Errorf("%s.oauth2.enabled: must be true when fleet.enabled is true and fleet.mcpGatewayEndpoint is set — there is no unauthenticated mode for the MCP Gateway", base)}
		}
		return nil
	}

	var errs []error
	if fleet.OAuth2.TokenURL == "" {
		errs = append(errs, fmt.Errorf("%s.oauth2.tokenURL: must be set when fleet.oauth2.enabled is true", base))
	}

	// A federated IdP (e.g. Keycloak) issues distinct per-service OAuth2
	// client registrations against one shared token endpoint (confirmed
	// against upstream's own Helm chart: kubernaut.fleet.oauth2 helper
	// resolves each service's own credentialsSecretRef, falling back to
	// the fleet-wide default). Each of the six fleet-aware components
	// needs its own *effective* value (own override, or the shared
	// fallback) — a shared credentialsSecretRef covers whichever
	// component doesn't override it, but a component that overrides it
	// no longer benefits from the shared value covering it too.
	//
	// #224: generalized from a 2-way Gateway/RemediationOrchestrator
	// switch to a loop over all five fleet-aware components (SP/AF/EM
	// gained their own Fleet override fields) -- see Finding 7.
	//
	// #204: extended to a sixth component -- KA authenticates its own
	// list_clusters/list_tools_for_cluster GatewayDiscoverer calls to
	// the MCP Gateway and needs the same effective-value guarantee.
	//
	// F1 (v1alpha2): each component's bespoke FleetOAuth2CredentialsSecretRef
	// field collapsed into a shared *FleetOverrideSpec.
	components := []struct {
		specPath string
		override *kubernautv1alpha2.FleetOverrideSpec
	}{
		{"spec.gateway.fleet.oauth2CredentialsSecretRef", knV2.Spec.Gateway.Fleet},
		{"spec.remediationOrchestrator.fleet.oauth2CredentialsSecretRef", knV2.Spec.RemediationOrchestrator.Fleet},
		{"spec.signalProcessing.fleet.oauth2CredentialsSecretRef", knV2.Spec.SignalProcessing.Fleet},
		{"spec.apiFrontend.fleet.oauth2CredentialsSecretRef", knV2.Spec.APIFrontend.Fleet},
		{"spec.effectivenessMonitor.fleet.oauth2CredentialsSecretRef", knV2.Spec.EffectivenessMonitor.Fleet},
		{"spec.kubernautAgent.fleet.oauth2CredentialsSecretRef", knV2.Spec.KubernautAgent.Fleet},
	}
	var missing []string
	for _, c := range components {
		if effectiveFleetOAuth2SecretRef(c.override, fleet.OAuth2.CredentialsSecretRef) == "" {
			missing = append(missing, c.specPath)
		}
	}
	switch len(missing) {
	case 0:
		// all six have an effective value.
	case len(components):
		errs = append(errs, fmt.Errorf("%s.oauth2.credentialsSecretRef: must be set when fleet.oauth2.enabled is true", base))
	default:
		errs = append(errs, fmt.Errorf("%s.oauth2.credentialsSecretRef: must be set, or %s must be set, when fleet.oauth2.enabled is true (the other fleet-aware components already override their own)", base, strings.Join(missing, " or ")))
	}
	return errs
}

// validateFleetMetadataCache validates spec.fleetMetadataCache. When
// Enabled is false or omitted (the default), the other fields are inert
// and left unvalidated so users can pre-stage configuration. FMC's MCP
// Gateway/OAuth2 requirements are checked independently of
// spec.fleet.enabled -- that flag only gates Gateway/RemediationOrchestrator's
// own scope-check consumption, but FMC needs the MCP Gateway address and
// credentials to poll managed clusters regardless of whether any consumer
// is configured to query it.
func validateFleetMetadataCache(knV2 *kubernautv1alpha2.Kubernaut) []error {
	if !knV2.Spec.FleetMetadataCacheEnabled() {
		return nil
	}

	var errs []error
	const base = "spec.fleetMetadataCache"
	fleet := &knV2.Spec.Fleet

	// Mirrors upstream's own Helm chart guard (fleetmetadatacache.yaml's
	// "fail" checks): FMC's entire purpose is polling remote clusters via
	// the MCP Gateway, so this is non-negotiable regardless of
	// spec.fleet.enabled.
	if fleet.MCPGatewayEndpoint == "" {
		errs = append(errs, fmt.Errorf("%s: spec.fleet.mcpGatewayEndpoint must be set when fleetMetadataCache.enabled is true -- FMC polls managed clusters via the MCP Gateway", base))
	}
	if fleet.MCPGatewayType == "" {
		errs = append(errs, fmt.Errorf("%s: spec.fleet.mcpGatewayType must be set when fleetMetadataCache.enabled is true -- must be one of: eaigw, kuadrant", base))
	} else if !validMCPGatewayTypes[fleet.MCPGatewayType] {
		errs = append(errs, fmt.Errorf("%s: spec.fleet.mcpGatewayType invalid value %q -- must be one of: eaigw, kuadrant", base, fleet.MCPGatewayType))
	}

	// FedRAMP IA-5: upstream FMC's config.Validate() rejects a missing
	// oauth2.tokenUrl unconditionally -- there is no unauthenticated mode
	// for the MCP Gateway in FMC.
	if !fleet.OAuth2.Enabled {
		errs = append(errs, fmt.Errorf("%s: spec.fleet.oauth2.enabled must be true when fleetMetadataCache.enabled is true -- FMC has no unauthenticated mode for the MCP Gateway", base))
	} else {
		if fleet.OAuth2.TokenURL == "" {
			errs = append(errs, fmt.Errorf("%s: spec.fleet.oauth2.tokenURL must be set when fleetMetadataCache.enabled is true", base))
		}
		if effectiveFleetOAuth2SecretRef(knV2.Spec.FleetMetadataCache.Fleet, fleet.OAuth2.CredentialsSecretRef) == "" {
			errs = append(errs, fmt.Errorf("%s.fleet.oauth2CredentialsSecretRef: must be set, or spec.fleet.oauth2.credentialsSecretRef must be set, when fleetMetadataCache.enabled is true", base))
		}
	}

	return errs
}

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
)

const maxJWKSURLLength = 2048

// ValidateKubernaut runs all CR-level validations and returns accumulated errors.
// sidecar indicates whether kagenti is active; when it is, issuerURL is not
// required because the operator auto-detects it from kagenti's authbridge-config.
func ValidateKubernaut(kn *kubernautv1alpha1.Kubernaut, sidecar KagentiSidecarMode) []error {
	errs := make([]error, 0, 4)
	errs = append(errs, validatePostgreSQLSSLMode(kn)...)
	errs = append(errs, validatePolicyPrerequisites(kn)...)
	errs = append(errs, validateLLMPrerequisites(kn)...)
	errs = append(errs, validateJWKSProviders(kn)...)
	errs = append(errs, validateAPIFrontend(kn, sidecar)...)
	errs = append(errs, validateAlignmentCheck(kn)...)
	errs = append(errs, validateDryRun(kn)...)
	errs = append(errs, validateInteractive(kn)...)
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

func validateLLMPrerequisites(kn *kubernautv1alpha1.Kubernaut) []error {
	var errs []error
	const base = "spec.kubernautAgent.llm"
	llm := &kn.Spec.KubernautAgent.LLM
	if llm.Provider == "" {
		errs = append(errs, fmt.Errorf("%s.provider: required — specify the LLM provider (e.g. \"openai\", \"vertexai\")", base))
	}
	if llm.Model == "" {
		errs = append(errs, fmt.Errorf("%s.model: required — specify the LLM model name (e.g. \"gpt-4o\", \"gemini-2.5-pro\")", base))
	}
	if llm.CredentialsSecretName == "" {
		errs = append(errs, fmt.Errorf("%s.credentialsSecretName: required — provide a Secret with LLM API credentials", base))
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
	errs = append(errs, validatePhaseModels(llm.PhaseModels, base+".phaseModels")...)
	return errs
}

// validPhaseModelKeys enumerates the agent phases that support per-phase LLM overrides.
var validPhaseModelKeys = map[string]bool{
	"rca":                true,
	"workflow_discovery": true,
	"validation":         true,
}

func validatePhaseModels(pm map[string]kubernautv1alpha1.LLMPhaseOverrideSpec, base string) []error {
	var errs []error
	for key := range pm {
		if !validPhaseModelKeys[key] {
			errs = append(errs, fmt.Errorf("%s: invalid phase key %q — must be one of: rca, workflow_discovery, validation", base, key))
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

	if ref := af.RBACRolesConfigMapRef; ref != nil && ref.ConfigMapName == "" {
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
		if rb.Role != "" && rb.ClusterRoleName != "" {
			errs = append(errs, fmt.Errorf("spec.apiFrontend.rbac.roleBindings[%d]: role and clusterRoleName are mutually exclusive", i))
			continue
		}
		if rb.Role == "" && rb.ClusterRoleName == "" {
			errs = append(errs, fmt.Errorf("spec.apiFrontend.rbac.roleBindings[%d]: one of role or clusterRoleName must be set", i))
			continue
		}

		if rb.Role != "" {
			if seen[rb.Role] {
				errs = append(errs, fmt.Errorf("spec.apiFrontend.rbac.roleBindings: duplicate role %q", rb.Role))
				continue
			}
			seen[rb.Role] = true

			if !validToolPersonas[rb.Role] {
				errs = append(errs, fmt.Errorf("spec.apiFrontend.rbac.roleBindings: unknown persona %q", rb.Role))
			}
		} else {
			if seen[rb.ClusterRoleName] {
				errs = append(errs, fmt.Errorf("spec.apiFrontend.rbac.roleBindings: duplicate clusterRoleName %q", rb.ClusterRoleName))
				continue
			}
			seen[rb.ClusterRoleName] = true
		}
	}

	return errs
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

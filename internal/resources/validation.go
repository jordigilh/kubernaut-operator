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
func ValidateKubernaut(kn *kubernautv1alpha1.Kubernaut) []error {
	errs := make([]error, 0, 4)
	errs = append(errs, validatePostgreSQLSSLMode(kn)...)
	errs = append(errs, validateJWKSProviders(kn)...)
	errs = append(errs, validateAPIFrontend(kn)...)
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

func validateJWKSProviders(kn *kubernautv1alpha1.Kubernaut) []error {
	interactive := kn.Spec.KubernautAgent.Interactive
	if interactive == nil || len(interactive.JWTProviders) == 0 {
		return nil
	}
	var errs []error
	for i, p := range interactive.JWTProviders {
		path := fmt.Sprintf("spec.kubernautAgent.interactive.jwtProviders[%d]", i)

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

		if !interactive.AllowInsecureJWKS && strings.ToLower(u.Scheme) != "https" {
			errs = append(errs, fmt.Errorf(
				"%s.jwksURL: scheme must be https (got %q); set spec.kubernautAgent.interactive.allowInsecureJWKS=true to permit HTTP for dev/test",
				path, u.Scheme))
		}
	}
	return errs
}

func validateAPIFrontend(kn *kubernautv1alpha1.Kubernaut) []error {
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

	if ref := af.RBACRolesConfigMapRef; ref != nil && ref.ConfigMapName == "" {
		errs = append(errs, fmt.Errorf("spec.apiFrontend.rbacRolesConfigMapRef.configMapName: must not be empty when rbacRolesConfigMapRef is set"))
	}

	errs = append(errs, validateToolRoleBindings(kn)...)
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
	for _, rb := range rbac.RoleBindings {
		if seen[rb.Role] {
			errs = append(errs, fmt.Errorf("spec.apiFrontend.rbac.roleBindings: duplicate role %q", rb.Role))
			continue
		}
		seen[rb.Role] = true

		if !validToolPersonas[rb.Role] {
			errs = append(errs, fmt.Errorf("spec.apiFrontend.rbac.roleBindings: unknown persona %q", rb.Role))
		}
	}

	return errs
}

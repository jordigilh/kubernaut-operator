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

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

const maxJWKSURLLength = 2048

// ValidateKubernaut runs all CR-level validations and returns accumulated errors.
func ValidateKubernaut(kn *kubernautv1alpha1.Kubernaut) []error {
	var errs []error
	errs = append(errs, validatePostgreSQLSSLMode(kn)...)
	errs = append(errs, validateJWKSProviders(kn)...)
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

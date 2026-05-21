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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

var _ = Describe("JWKS URL Validation", func() {
	withInteractive := func(providers []kubernautv1alpha1.JWTProviderSpec, allowInsecure bool) *kubernautv1alpha1.Kubernaut {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.Interactive = &kubernautv1alpha1.InteractiveSpec{
			JWTProviders:      providers,
			AllowInsecureJWKS: allowInsecure,
		}
		return kn
	}

	It("accepts HTTPS JWKS URL", func() {
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{
			{Name: "rhbk", JWKSURL: "https://login.kubernaut.ai/realms/kubernaut/protocol/openid-connect/certs"},
		}, false)
		errs := ValidateKubernaut(kn)
		Expect(errs).To(BeEmpty())
	})

	It("rejects HTTP JWKS URL without override", func() {
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{
			{Name: "dev", JWKSURL: "http://mock-jwks:8080/.well-known/jwks.json"},
		}, false)
		errs := ValidateKubernaut(kn)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("scheme must be https"))
		Expect(errs[0].Error()).To(ContainSubstring("allowInsecureJWKS"))
	})

	It("accepts HTTP JWKS URL with allowInsecureJWKS=true", func() {
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{
			{Name: "dev", JWKSURL: "http://mock-jwks:8080/.well-known/jwks.json"},
		}, true)
		errs := ValidateKubernaut(kn)
		Expect(errs).To(BeEmpty())
	})

	It("rejects JWKS URL longer than 2048 characters", func() {
		longURL := "https://example.com/" + strings.Repeat("a", 2040)
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{
			{Name: "long", JWKSURL: longURL},
		}, false)
		errs := ValidateKubernaut(kn)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("must be <= 2048 characters"))
	})

	It("rejects malformed JWKS URL", func() {
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{
			{Name: "bad", JWKSURL: "not-a-url"},
		}, false)
		errs := ValidateKubernaut(kn)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("must be an absolute URL"))
	})

	It("reports multiple errors for multiple bad providers", func() {
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{
			{Name: "http1", JWKSURL: "http://foo.com/jwks"},
			{Name: "http2", JWKSURL: "http://bar.com/jwks"},
		}, false)
		errs := ValidateKubernaut(kn)
		Expect(errs).To(HaveLen(2))
	})

	It("returns no errors when interactive is nil", func() {
		kn := testKubernaut()
		errs := ValidateKubernaut(kn)
		Expect(errs).To(BeEmpty())
	})

	It("returns no errors when providers list is empty", func() {
		kn := withInteractive(nil, false)
		errs := ValidateKubernaut(kn)
		Expect(errs).To(BeEmpty())
	})
})

var _ = Describe("PostgreSQL SSLMode Validation", func() {
	It("rejects sslMode=disable", func() {
		kn := testKubernaut()
		kn.Spec.PostgreSQL.SSLMode = "disable"
		errs := ValidateKubernaut(kn)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("disable"))
		Expect(errs[0].Error()).To(ContainSubstring("SC-8"))
	})

	It("accepts sslMode=verify-full", func() {
		kn := testKubernaut()
		kn.Spec.PostgreSQL.SSLMode = "verify-full"
		errs := ValidateKubernaut(kn)
		Expect(errs).To(BeEmpty())
	})

	It("accepts sslMode=verify-ca", func() {
		kn := testKubernaut()
		kn.Spec.PostgreSQL.SSLMode = "verify-ca"
		errs := ValidateKubernaut(kn)
		Expect(errs).To(BeEmpty())
	})

	It("accepts empty sslMode (defaults to verify-full in ConfigMap)", func() {
		kn := testKubernaut()
		kn.Spec.PostgreSQL.SSLMode = ""
		errs := ValidateKubernaut(kn)
		Expect(errs).To(BeEmpty())
	})
})

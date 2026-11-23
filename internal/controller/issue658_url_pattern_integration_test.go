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

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// Issue #658: the CRD schema patterns for URL-typed fields used to admit
// hostless values like "https://:443" at API-acceptance time, deferring the
// rejection to reconcile (Degraded phase). The patterns now require a host
// character after the scheme, mirroring the accept-set the admission
// validators in internal/resources/validation.go already enforce. Pattern
// enforcement only exists on a real API server surface, so these specs drive
// the CRD apply through envtest rather than asserting on the pattern string.
var _ = Describe("Issue #658 CRD URL pattern admission [SI-10, CM-6]", Label("integration"), func() {
	// hostlessURL is the representative hostless value from issue #658:
	// scheme plus port-only authority, which net/url parses with an empty
	// Hostname() and the admission validators reject at reconcile.
	const hostlessURL = "https://:443"

	BeforeEach(func() {
		deleteCRIfExists(ctx)
	})
	AfterEach(func() {
		deleteCRIfExists(ctx)
	})

	DescribeTable("rejects hostless URLs at CRD admission",
		func(fieldJSON string, mutate func(kn *kubernautv1alpha2.Kubernaut)) {
			kn := newMinimalCR()
			mutate(kn)
			err := k8sClient.Create(ctx, kn)
			// "should match" proves it is the pattern doing the rejecting,
			// not another schema rule on the same field.
			Expect(err).To(MatchError(SatisfyAll(
				ContainSubstring(fieldJSON),
				ContainSubstring("should match"),
			)), "a hostless URL must be rejected by the CRD pattern at apply time")
			fetched := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), fetched)).To(MatchError(ContainSubstring("not found")),
				"a rejected create must persist nothing")
		},
		Entry("IT-658-PAT-001 top-level issuerURL", "issuerURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.APIFrontend.Auth.IssuerURL = hostlessURL
			}),
		Entry("IT-658-PAT-002 jwtProviders issuerURL", "issuerURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{{
					Name:      "rhbk",
					IssuerURL: hostlessURL,
					JWKSURL:   "https://rhbk.example.com/realms/y/protocol/openid-connect/certs",
					Audiences: []string{"kubernaut-apifrontend"},
				}}
			}),
		Entry("IT-658-PAT-003 agentCardURL", "agentCardURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.APIFrontend.AgentCardURL = hostlessURL
			}),
		Entry("IT-658-PAT-004 bare-scheme agentCardURL", "agentCardURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.APIFrontend.AgentCardURL = "https://"
			}),
		Entry("IT-658-PAT-005 console redirectURL", "redirectURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.Console.Auth.RedirectURL = hostlessURL
			}),
		Entry("IT-658-PAT-006 console providerLogoutURL", "providerLogoutURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.Console.Auth.ProviderLogoutURL = hostlessURL
			}),
	)

	It("IT-658-PAT-007 accepts URL values carrying a host at CRD admission", func() {
		kn := newMinimalCR()
		kn.Spec.APIFrontend.AgentCardURL = "https://localhost:8443/a2a/invoke"
		kn.Spec.Console.Auth.RedirectURL = "https://console.example.com:8443/oauth2/callback"
		kn.Spec.Console.Auth.ProviderLogoutURL = "https://idp.example.com/realms/kubernaut/protocol/openid-connect/logout?id_token_hint={id_token}"
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{{
			Name:      "rhbk",
			IssuerURL: "https://rhbk.example.com/realms/y",
			JWKSURL:   "https://rhbk.example.com/realms/y/protocol/openid-connect/certs",
			Audiences: []string{"kubernaut-apifrontend"},
		}}
		Expect(k8sClient.Create(ctx, kn)).To(Succeed(),
			"host, host:port, localhost, path- and query-carrying URLs must stay admissible")
	})

	It("IT-658-PAT-008 accepts bracketed IPv6 hosts at CRD admission", func() {
		kn := newMinimalCR()
		kn.Spec.APIFrontend.Auth.IssuerURL = "https://[2001:db8::1]:8443/realms/kubernaut"
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{{
			Name:      "rhbk",
			IssuerURL: "https://[2001:db8::2]/realms/y",
			JWKSURL:   "https://rhbk.example.com/realms/y/protocol/openid-connect/certs",
			Audiences: []string{"kubernaut-apifrontend"},
		}}
		Expect(k8sClient.Create(ctx, kn)).To(Succeed(),
			"bracketed IPv6 hosts (with and without port) must stay admissible")
	})
})

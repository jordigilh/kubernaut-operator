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

// Issue #672: follow-up to #658. Several URL-typed CRD fields carried no
// schema pattern at all, so hostless values like "https://:443" were
// admitted at apply time and only rejected later at reconcile by the
// admission validators in internal/resources/validation.go (Degraded phase).
// The fields now carry host-requiring patterns derived from each field's
// validator accept-set. Pattern enforcement only exists on a real API server
// surface, so these specs drive the CRD apply through envtest rather than
// asserting on the pattern string.
var _ = Describe("Issue #672 CRD URL pattern admission [SI-10, CM-6]", Label("integration"), func() {
	// hostlessURL is the representative hostless value from the issue:
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
		Entry("IT-672-PAT-001 fleet oauth2 tokenURL", "tokenURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.Fleet.OAuth2.TokenURL = hostlessURL
			}),
		Entry("IT-672-PAT-002 LLM profile oauth2 tokenURL", "tokenURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				profile := kn.Spec.LLMProfiles["primary"]
				profile.OAuth2.TokenURL = hostlessURL
				kn.Spec.LLMProfiles["primary"] = profile
			}),
		Entry("IT-672-PAT-003 ansible apiURL", "apiURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.WorkflowExecution.Ansible.APIURL = hostlessURL
			}),
		Entry("IT-672-PAT-004 jwtProviders jwksURL", "jwksURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{{
					Name:      "rhbk",
					IssuerURL: "https://rhbk.example.com/realms/y",
					JWKSURL:   hostlessURL,
					Audiences: []string{"kubernaut-apifrontend"},
				}}
			}),
		Entry("IT-672-PAT-005 apiFrontend auth jwksURL", "jwksURL",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.APIFrontend.Auth.JWKSURL = hostlessURL
			}),
		Entry("IT-672-PAT-006 prometheus url", "url",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.Monitoring.Prometheus.URL = hostlessURL
			}),
		Entry("IT-672-PAT-007 alertManager url", "url",
			func(kn *kubernautv1alpha2.Kubernaut) {
				kn.Spec.Monitoring.AlertManager.URL = hostlessURL
			}),
	)

	It("IT-672-PAT-008 accepts representative validator-compatible URLs at CRD admission", func() {
		kn := newMinimalCR()
		kn.Spec.Fleet.OAuth2.TokenURL = "https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/token"
		profile := kn.Spec.LLMProfiles["primary"]
		profile.OAuth2.TokenURL = "https://[2001:db8::1]:8443/oauth2/token"
		kn.Spec.LLMProfiles["primary"] = profile
		kn.Spec.WorkflowExecution.Ansible.APIURL = "http://awx.example.com:8052"
		kn.Spec.APIFrontend.Auth.JWKSURL = "https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs"
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{{
			Name:      "rhbk",
			IssuerURL: "https://rhbk.example.com/realms/y",
			JWKSURL:   "https://rhbk.example.com/realms/y/protocol/openid-connect/certs",
			Audiences: []string{"kubernaut-apifrontend"},
		}}
		kn.Spec.Monitoring.Prometheus.URL = "https://prometheus.monitoring.svc:9090"
		kn.Spec.Monitoring.AlertManager.URL = "https://alertmanager.monitoring.svc:9093"
		Expect(k8sClient.Create(ctx, kn)).To(Succeed(),
			"host, host:port, bracketed IPv6, path-carrying, and http-under-validator-contract URLs must stay admissible")
	})

	It("IT-672-PAT-009 accepts colon-prefixed userinfo where the validators tolerate it", func() {
		// net/url splits userinfo before the host, so the host is nonempty
		// and the OIDC validators accept it. The userinfo-tolerant pattern
		// must still require a host character after the userinfo.
		kn := newMinimalCR()
		kn.Spec.APIFrontend.Auth.JWKSURL = "https://:example@idp.example.com/realms/y/certs"
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{{
			Name:      "rhbk",
			IssuerURL: "https://rhbk.example.com/realms/y",
			JWKSURL:   "https://:example@rhbk.example.com/realms/y/certs",
			Audiences: []string{"kubernaut-apifrontend"},
		}}
		expectErr := k8sClient.Create(ctx, kn)
		Expect(expectErr).NotTo(HaveOccurred(),
			"a host-bearing JWKS URL whose userinfo starts with ':' must stay admissible")
	})
})

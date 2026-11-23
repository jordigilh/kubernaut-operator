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
	"fmt"

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
	// emptyBracketedHostURL carries a bracketed host with no content.
	// net/url rejects it outright ("unable to parse IP"), and the pattern's
	// bracketed-host alternative requires at least one character inside the
	// brackets.
	const emptyBracketedHostURL = "https://[]:443"

	BeforeEach(func() {
		deleteCRIfExists(ctx)
	})
	AfterEach(func() {
		deleteCRIfExists(ctx)
	})

	// rejectionEntries builds one entry per affected field for a value the
	// CRD pattern must reject at apply time.
	rejectionEntries := func(idBase int, raw string) []TableEntry {
		return []TableEntry{
			Entry(fmt.Sprintf("IT-672-PAT-%03d fleet oauth2 tokenURL", idBase), "tokenURL", raw,
				func(kn *kubernautv1alpha2.Kubernaut) {
					kn.Spec.Fleet.OAuth2.TokenURL = raw
				}),
			Entry(fmt.Sprintf("IT-672-PAT-%03d LLM profile oauth2 tokenURL", idBase+1), "tokenURL", raw,
				func(kn *kubernautv1alpha2.Kubernaut) {
					profile := kn.Spec.LLMProfiles["primary"]
					profile.OAuth2.TokenURL = raw
					kn.Spec.LLMProfiles["primary"] = profile
				}),
			Entry(fmt.Sprintf("IT-672-PAT-%03d ansible apiURL", idBase+2), "apiURL", raw,
				func(kn *kubernautv1alpha2.Kubernaut) {
					kn.Spec.WorkflowExecution.Ansible.APIURL = raw
				}),
			Entry(fmt.Sprintf("IT-672-PAT-%03d jwtProviders jwksURL", idBase+3), "jwksURL", raw,
				func(kn *kubernautv1alpha2.Kubernaut) {
					kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{{
						Name:      "rhbk",
						IssuerURL: "https://rhbk.example.com/realms/y",
						JWKSURL:   raw,
						Audiences: []string{"kubernaut-apifrontend"},
					}}
				}),
			Entry(fmt.Sprintf("IT-672-PAT-%03d apiFrontend auth jwksURL", idBase+4), "jwksURL", raw,
				func(kn *kubernautv1alpha2.Kubernaut) {
					kn.Spec.APIFrontend.Auth.JWKSURL = raw
				}),
			Entry(fmt.Sprintf("IT-672-PAT-%03d prometheus url", idBase+5), "url", raw,
				func(kn *kubernautv1alpha2.Kubernaut) {
					kn.Spec.Monitoring.Prometheus.URL = raw
				}),
			Entry(fmt.Sprintf("IT-672-PAT-%03d alertManager url", idBase+6), "url", raw,
				func(kn *kubernautv1alpha2.Kubernaut) {
					kn.Spec.Monitoring.AlertManager.URL = raw
				}),
		}
	}

	DescribeTable("rejects hostless URLs at CRD admission",
		func(fieldJSON, raw string, mutate func(kn *kubernautv1alpha2.Kubernaut)) {
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
		rejectionEntries(1, hostlessURL),
	)

	DescribeTable("rejects empty bracketed hosts at CRD admission",
		func(fieldJSON, raw string, mutate func(kn *kubernautv1alpha2.Kubernaut)) {
			kn := newMinimalCR()
			mutate(kn)
			err := k8sClient.Create(ctx, kn)
			Expect(err).To(MatchError(SatisfyAll(
				ContainSubstring(fieldJSON),
				ContainSubstring("should match"),
			)), "an empty bracketed host must be rejected by the CRD pattern at apply time")
			fetched := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), fetched)).To(MatchError(ContainSubstring("not found")),
				"a rejected create must persist nothing")
		},
		rejectionEntries(10, emptyBracketedHostURL),
	)

	It("IT-672-PAT-020 accepts representative validator-compatible URLs at CRD admission", func() {
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

	It("IT-672-PAT-021 accepts uppercase schemes, which the validators normalize", func() {
		// net/url lowercases the scheme before the admission validators inspect
		// it, so "HTTPS://host" is validator-accepted; the patterns match the
		// scheme case-insensitively to stay a superset of that accept-set.
		kn := newMinimalCR()
		kn.Spec.Fleet.OAuth2.TokenURL = "HTTPS://keycloak.example.com/token"
		kn.Spec.WorkflowExecution.Ansible.APIURL = "HTTP://awx.example.com:8052"
		kn.Spec.APIFrontend.Auth.JWKSURL = "HTTPS://idp.example.com/realms/y/certs"
		kn.Spec.Monitoring.Prometheus.URL = "HTTPS://prometheus.monitoring.svc:9090"
		kn.Spec.Monitoring.AlertManager.URL = "HTTPS://alertmanager.monitoring.svc:9093"
		Expect(k8sClient.Create(ctx, kn)).To(Succeed(),
			"uppercase schemes must stay admissible because the validators accept them")
	})

	It("IT-672-PAT-022 accepts colon-prefixed userinfo where the validators tolerate it", func() {
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
		Expect(k8sClient.Create(ctx, kn)).NotTo(HaveOccurred(),
			"a host-bearing JWKS URL whose userinfo starts with ':' must stay admissible")
	})
})

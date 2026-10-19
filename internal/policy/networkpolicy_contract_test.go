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

package policy

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	kubernetnetworking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("portable Kubernetes NetworkPolicy contract", func() {
	It("UT-NP-555-RENDER-001 [AC-3, AC-6, SC-7; SOC2 CC6.1, CC6.6; ASVS V4.1, V4.2] renders selector-based standard policies without provider or API-server gating", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"gateway", "kubernaut-agent"})
		Expect(err).NotTo(HaveOccurred())
		intent.InstanceName = "kubernaut"
		intent.MetricsPorts = map[string]int32{"kubernaut-agent": 9090}
		intent.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{
			Namespace: "monitoring",
			PodLabels: map[string]string{"app": "prometheus"},
		}}

		rendered, err := Render(intent)
		Expect(err).NotTo(HaveOccurred())
		Expect(rendered).To(HaveLen(2))
		for _, item := range rendered {
			Expect(item.Namespaced).To(BeTrue())
			Expect(item.Object.APIVersion).To(Equal("networking.k8s.io/v1"))
			Expect(item.Object.Kind).To(Equal("NetworkPolicy"))
			Expect(item.Object.Spec.PolicyTypes).To(Equal([]kubernetnetworking.PolicyType{kubernetnetworking.PolicyTypeIngress}))
			Expect(item.Object.Spec.Egress).To(BeEmpty(), "the operator must not block Kubernetes API or external dependency egress")
			Expect(item.Object.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "kubernaut-operator"))
			Expect(item.Object.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/instance", "kubernaut"))
		}

		agent := rendered[1].Object
		Expect(agent.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", "kubernaut-agent"))
		foundScrapeRule := false
		for _, rule := range agent.Spec.Ingress {
			if len(rule.Ports) != 1 || rule.Ports[0].Port == nil || rule.Ports[0].Port.IntValue() != 9090 {
				continue
			}
			if len(rule.From) != 1 || rule.From[0].NamespaceSelector == nil || rule.From[0].PodSelector == nil {
				continue
			}
			if rule.From[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == "monitoring" &&
				rule.From[0].PodSelector.MatchLabels["app"] == "prometheus" &&
				rule.Ports[0].Protocol != nil && *rule.Ports[0].Protocol == corev1.ProtocolTCP {
				foundScrapeRule = true
			}
		}
		Expect(foundScrapeRule).To(BeTrue())
	})

	It("UT-NP-555-RENDER-002 [AC-3, SC-7; SOC2 CC6.1; ASVS V4.1] converts standard ingress overrides into namespace, pod, and CIDR peers", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"gateway"})
		Expect(err).NotTo(HaveOccurred())
		intent.InstanceName = "kubernaut"
		intent.IngressOverrides = map[string]kubernautv1alpha2.NetworkPolicyNamedIngressOverride{
			"gateway": {
				IngressNamespaces: []string{"openshift-ingress"},
				NetworkPolicyIngressOverride: kubernautv1alpha2.NetworkPolicyIngressOverride{
					IngressCIDRs:              []string{"10.0.0.0/8"},
					IngressNamespaceSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"team": "platform"}}},
				},
			},
		}

		rendered, err := Render(intent)
		Expect(err).NotTo(HaveOccurred())
		Expect(rendered).To(HaveLen(1))
		Expect(rendered[0].Object.Spec.Ingress).To(HaveLen(4), "managed peers plus three explicit standard peers")
		Expect(rendered[0].Object.Spec.Egress).To(BeEmpty())
	})

	It("UT-NP-555-RENDER-003 [SC-7; SOC2 CC6.6; ASVS V4.1] leaves AuthWebhook admission ingress administrator-owned", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"gateway", "authwebhook"})
		Expect(err).NotTo(HaveOccurred())
		rendered, err := Render(intent)
		Expect(err).NotTo(HaveOccurred())
		Expect(rendered).To(HaveLen(1))
		Expect(rendered[0].Object.Name).To(Equal("kubernaut-gateway"))
	})

	It("UT-NP-555-VALIDATE-001 [AC-3, AC-6; SOC2 CC6.6; ASVS V4.1] rejects malformed standard policy inputs", func() {
		_, err := BuildIntent("", []string{"gateway"})
		Expect(err).To(MatchError("policy namespace is required"))
		_, err = BuildIntent("kubernaut-system", []string{"", ""})
		Expect(err).To(MatchError("at least one policy component is required"))

		Expect(ValidateNetworkPoliciesSpec(kubernautv1alpha2.NetworkPoliciesSpec{
			Gateway: kubernautv1alpha2.NetworkPolicyNamedIngressOverride{
				NetworkPolicyIngressOverride: kubernautv1alpha2.NetworkPolicyIngressOverride{IngressCIDRs: []string{"not-a-cidr"}},
			},
		})).To(MatchError(ContainSubstring("ingress CIDR")))
		Expect(ValidateNetworkPoliciesSpec(kubernautv1alpha2.NetworkPoliciesSpec{
			Gateway: kubernautv1alpha2.NetworkPolicyNamedIngressOverride{IngressNamespaces: []string{"Invalid_Namespace"}},
		})).To(MatchError(ContainSubstring("ingress namespace")))
		Expect(ValidateNetworkPoliciesSpec(kubernautv1alpha2.NetworkPoliciesSpec{
			Gateway: kubernautv1alpha2.NetworkPolicyNamedIngressOverride{
				NetworkPolicyIngressOverride: kubernautv1alpha2.NetworkPolicyIngressOverride{
					IngressNamespaceSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"bad key": "value"}}},
				},
			},
		})).To(MatchError(ContainSubstring("namespace selector")))
		Expect(ValidateNetworkPoliciesSpec(kubernautv1alpha2.NetworkPoliciesSpec{
			DataStorage: kubernautv1alpha2.NetworkPolicyIngressOverride{IngressCIDRs: []string{"invalid"}},
		})).To(MatchError(ContainSubstring("datastorage")))
		Expect(ValidateNetworkPoliciesSpec(kubernautv1alpha2.NetworkPoliciesSpec{
			ScrapeSources: make([]kubernautv1alpha2.MetricsScrapeSource, 17),
		})).To(MatchError(ContainSubstring("at most 16")))
		Expect(ValidateNetworkPoliciesSpec(kubernautv1alpha2.NetworkPoliciesSpec{
			ScrapeSources: []kubernautv1alpha2.MetricsScrapeSource{{Namespace: "Invalid_Namespace", PodLabels: map[string]string{"app": "prometheus"}}},
		})).To(MatchError(ContainSubstring("valid exact namespace")))
		Expect(ValidateNetworkPoliciesSpec(kubernautv1alpha2.NetworkPoliciesSpec{
			ScrapeSources: []kubernautv1alpha2.MetricsScrapeSource{{Namespace: "monitoring"}},
		})).To(MatchError(ContainSubstring("1 to 16 exact pod labels")))
		Expect(ValidateNetworkPoliciesSpec(kubernautv1alpha2.NetworkPoliciesSpec{
			ScrapeSources: []kubernautv1alpha2.MetricsScrapeSource{{Namespace: "monitoring", PodLabels: map[string]string{"bad key": "value"}}},
		})).To(MatchError(ContainSubstring("invalid pod label")))
		Expect(ValidateNetworkPoliciesSpec(kubernautv1alpha2.NetworkPoliciesSpec{
			ScrapeSources: []kubernautv1alpha2.MetricsScrapeSource{{Namespace: "monitoring", PodLabels: map[string]string{"kubernetes.io/name": "prometheus"}}},
		})).To(MatchError(ContainSubstring("reserved namespace identity")))

		_, err = Render(Intent{Namespace: "kubernaut-system", Components: []string{"gateway"}, MetricsPorts: map[string]int32{}})
		Expect(err).NotTo(HaveOccurred(), "an omitted metrics port does not create a port rule")
		_, err = Render(Intent{Namespace: "kubernaut-system", Components: []string{"gateway"}, MetricsPorts: map[string]int32{"gateway": 65536}})
		Expect(err).To(MatchError(ContainSubstring("invalid metrics port")))
		_, err = Render(Intent{MetricsPorts: map[string]int32{"gateway": 9090}})
		Expect(err).To(MatchError("policy namespace is required"))
		_, err = Render(Intent{
			Namespace: "kubernaut-system", Components: []string{"gateway"},
			ScrapeSources: []kubernautv1alpha2.MetricsScrapeSource{{
				Namespace: "Invalid_Namespace", PodLabels: map[string]string{"app": "prometheus"},
			}},
		})
		Expect(err).To(MatchError(ContainSubstring("valid exact namespace")))
	})
})

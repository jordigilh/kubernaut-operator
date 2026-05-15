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
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("NetworkPolicies", func() {
	Context("when disabled or default", func() {
		It("returns nil when enabled is false", func() {
			kn := testKubernaut()
			disabled := false
			kn.Spec.NetworkPolicies.Enabled = &disabled
			Expect(NetworkPolicies(kn)).To(BeNil(), "NetworkPolicies() = %#v, want nil when enabled=false", NetworkPolicies(kn))
		})

		It("returns nil when enabled is unset", func() {
			kn := testKubernaut()
			Expect(NetworkPolicies(kn)).To(BeNil(), "NetworkPolicies() = %#v, want nil when enabled is not set (default false)", NetworkPolicies(kn))
		})
	})

	Context("when enabled", func() {
		var kn *kubernautv1alpha1.Kubernaut

		BeforeEach(func() {
			kn = testKubernaut()
			enabled := true
			kn.Spec.NetworkPolicies.Enabled = &enabled
		})

		It("returns ten policies", func() {
			nps := NetworkPolicies(kn)
			Expect(nps).To(HaveLen(10), "len(NetworkPolicies()) = %d, want 10", len(nps))
		})

		It("names match component netpol names", func() {
			nps := NetworkPolicies(kn)
			wantNames := make(map[string]bool)
			for _, c := range AllComponents() {
				wantNames[c+"-netpol"] = true
			}
			Expect(wantNames).To(HaveLen(10), "internal: expected 10 component netpol names, got %d", len(wantNames))
			for _, np := range nps {
				Expect(wantNames[np.Name]).To(BeTrue(), "unexpected NetworkPolicy name %q", np.Name)
				delete(wantNames, np.Name)
			}
			missing := make([]string, 0, len(wantNames))
			for name := range wantNames {
				missing = append(missing, name)
			}
			Expect(missing).To(BeEmpty(), "missing NetworkPolicy %v", missing)
		})

		It("adds gateway ingress from configured namespaces", func() {
			kn.Spec.NetworkPolicies.GatewayIngressNamespaces = []string{"ns1", "ns2"}
			var gatewayNP *networkingv1.NetworkPolicy
			for _, np := range NetworkPolicies(kn) {
				if np.Name == ComponentGateway+"-netpol" {
					gatewayNP = np
					break
				}
			}
			Expect(gatewayNP).NotTo(BeNil(), "gateway NetworkPolicy not found")
			Expect(gatewayNP.Spec.Ingress).To(HaveLen(1), "gateway ingress rule count = %d, want 1", len(gatewayNP.Spec.Ingress))
			nsSeen := ingressNamespaceNames(gatewayNP.Spec.Ingress[0])
			Expect(nsSeen["ns1"] && nsSeen["ns2"]).To(BeTrue(), "gateway ingress should allow namespaces ns1 and ns2, got %#v", nsSeen)
			Expect(nsSeen).To(HaveLen(2), "gateway ingress namespaces = %#v, want only ns1 and ns2", nsSeen)
		})

		It("gives kubernaut-agent ingress and egress with default egress count", func() {
			var agentNP *networkingv1.NetworkPolicy
			for _, np := range NetworkPolicies(kn) {
				if np.Name == ComponentKubernautAgent+"-netpol" {
					agentNP = np
					break
				}
			}
			Expect(agentNP).NotTo(BeNil(), "kubernaut-agent NetworkPolicy not found")
			wantTypes := []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			}
			Expect(slices.Equal(agentNP.Spec.PolicyTypes, wantTypes)).To(BeTrue(), "PolicyTypes = %v, want %v", agentNP.Spec.PolicyTypes, wantTypes)
			Expect(len(agentNP.Spec.Ingress)).To(BeNumerically(">=", 1), "kubernaut-agent ingress rule count = %d, want at least 1", len(agentNP.Spec.Ingress))
			Expect(len(agentNP.Spec.Egress)).To(Equal(2), "kubernaut-agent egress rule count = %d, want %d", len(agentNP.Spec.Egress), 2)
		})

		It("adds monitoring egress when MonitoringNamespace is set", func() {
			kn.Spec.NetworkPolicies.MonitoringNamespace = OCPMonitoringNamespace
			var agentNP *networkingv1.NetworkPolicy
			for _, np := range NetworkPolicies(kn) {
				if np.Name == ComponentKubernautAgent+"-netpol" {
					agentNP = np
					break
				}
			}
			Expect(agentNP).NotTo(BeNil(), "kubernaut-agent NetworkPolicy not found")
			Expect(len(agentNP.Spec.Egress)).To(Equal(3), "kubernaut-agent egress rule count = %d, want %d", len(agentNP.Spec.Egress), 3)
			monRule := agentNP.Spec.Egress[2]
			Expect(monRule.To).To(HaveLen(1), "monitoring egress peer count = %d, want 1", len(monRule.To))
			ns := monRule.To[0].NamespaceSelector
			Expect(ns != nil && ns.MatchLabels["kubernetes.io/metadata.name"] == OCPMonitoringNamespace).To(BeTrue(),
				"monitoring egress namespace selector = %v, want %s", ns, OCPMonitoringNamespace)
			Expect(monRule.Ports).To(HaveLen(2), "monitoring egress port count = %d, want 2", len(monRule.Ports))
			Expect(monRule.Ports[0].Port.IntValue()).To(Equal(9091), "monitoring egress port[0] = %d, want 9091 (Thanos)", monRule.Ports[0].Port.IntValue())
			Expect(monRule.Ports[1].Port.IntValue()).To(Equal(9094), "monitoring egress port[1] = %d, want 9094 (AlertManager)", monRule.Ports[1].Port.IntValue())
		})

		It("allows data-storage ingress from all client components", func() {
			var dsNP *networkingv1.NetworkPolicy
			for _, np := range NetworkPolicies(kn) {
				if np.Name == ComponentDataStorage+"-netpol" {
					dsNP = np
					break
				}
			}
			Expect(dsNP).NotTo(BeNil(), "data-storage NetworkPolicy not found")
			Expect(len(dsNP.Spec.Ingress)).To(BeNumerically(">=", 1), "data-storage should have at least one ingress rule")
			rule := dsNP.Spec.Ingress[0]
			Expect(rule.From).To(HaveLen(9), "data-storage client ingress peers = %d, want 9", len(rule.From))
			wantApps := map[string]struct{}{
				ComponentGateway:                 {},
				ComponentAIAnalysis:              {},
				ComponentSignalProcessing:        {},
				ComponentRemediationOrchestrator: {},
				ComponentWorkflowExecution:       {},
				ComponentNotification:            {},
				ComponentEffectivenessMonitor:    {},
				ComponentAuthWebhook:             {},
				ComponentKubernautAgent:          {},
			}
			gotApps := make(map[string]struct{})
			for _, peer := range rule.From {
				Expect(peer.PodSelector).NotTo(BeNil(), "expected PodSelector peer, got %#v", peer)
				Expect(peer.PodSelector.MatchLabels).NotTo(BeNil())
				app := peer.PodSelector.MatchLabels["app"]
				Expect(app).NotTo(BeEmpty(), "peer missing app label: %#v", peer.PodSelector.MatchLabels)
				gotApps[app] = struct{}{}
			}
			for a := range wantApps {
				_, ok := gotApps[a]
				Expect(ok).To(BeTrue(), "missing ingress from client %q", a)
			}
			Expect(gotApps).To(HaveLen(9), "unexpected peer count or duplicate apps: %#v", gotApps)
		})

		It("allows metrics scrape ingress from openshift-monitoring", func() {
			kn.Spec.NetworkPolicies.MonitoringNamespace = OCPMonitoringNamespace
			var dsNP *networkingv1.NetworkPolicy
			for _, np := range NetworkPolicies(kn) {
				if np.Name == ComponentDataStorage+"-netpol" {
					dsNP = np
					break
				}
			}
			Expect(dsNP).NotTo(BeNil(), "data-storage NetworkPolicy not found")
			p9090 := intstr.FromInt32(PortMetrics)
			proto := corev1.ProtocolTCP
			found := false
			for _, rule := range dsNP.Spec.Ingress {
				nsOK := false
				for _, peer := range rule.From {
					if peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels != nil &&
						peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == OCPMonitoringNamespace {
						nsOK = true
						break
					}
				}
				if !nsOK {
					continue
				}
				for _, port := range rule.Ports {
					if port.Protocol != nil && *port.Protocol == proto && port.Port != nil && port.Port.String() == p9090.String() {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			Expect(found).To(BeTrue(), "data-storage NP should allow metrics scrape ingress from openshift-monitoring on port %d", PortMetrics)
		})
	})

	It("adds API server egress when APIServerCIDR is set", func() {
		kn := testKubernaut()
		enabled := true
		kn.Spec.NetworkPolicies.Enabled = &enabled
		kn.Spec.NetworkPolicies.APIServerCIDR = "10.0.0.0/16"
		nps := NetworkPolicies(kn)
		p443 := intstr.FromInt32(443)
		proto := corev1.ProtocolTCP
		found := false
	outer:
		for _, np := range nps {
			for _, rule := range np.Spec.Egress {
				for _, peer := range rule.To {
					if peer.IPBlock == nil || peer.IPBlock.CIDR != "10.0.0.0/16" {
						continue
					}
					for _, port := range rule.Ports {
						if port.Protocol != nil && *port.Protocol == proto && port.Port != nil && port.Port.String() == p443.String() {
							found = true
							break outer
						}
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "expected at least one NetworkPolicy with API server egress (10.0.0.0/16:443)")
	})
})

func ingressNamespaceNames(rule networkingv1.NetworkPolicyIngressRule) map[string]bool {
	out := make(map[string]bool)
	for _, peer := range rule.From {
		if peer.NamespaceSelector == nil || peer.NamespaceSelector.MatchLabels == nil {
			continue
		}
		if ns, ok := peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]; ok && ns != "" {
			out[ns] = true
		}
	}
	return out
}

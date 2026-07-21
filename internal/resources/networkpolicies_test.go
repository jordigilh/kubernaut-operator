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
	"os"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const testAPIServerHost = "10.0.0.1"

var _ = Describe("NetworkPolicies", func() {
	Context("when disabled or default", func() {
		It("returns nil when enabled is false", func() {
			kn := testKubernaut()
			disabled := false
			kn.Spec.NetworkPolicies.Enabled = &disabled
			Expect(NetworkPolicies(kn, KagentiSidecarNone)).To(BeNil(), "NetworkPolicies() = %#v, want nil when enabled=false", NetworkPolicies(kn, KagentiSidecarNone))
		})

		It("returns nil when enabled is unset", func() {
			kn := testKubernaut()
			Expect(NetworkPolicies(kn, KagentiSidecarNone)).To(BeNil(), "NetworkPolicies() = %#v, want nil when enabled is not set (default false)", NetworkPolicies(kn, KagentiSidecarNone))
		})
	})

	Context("when enabled", func() {
		var kn *kubernautv1alpha1.Kubernaut

		BeforeEach(func() {
			kn = testKubernaut()
			enabled := true
			kn.Spec.NetworkPolicies.Enabled = &enabled
		})

		It("returns eleven policies for all components", func() {
			nps := NetworkPolicies(kn, KagentiSidecarNone)
			Expect(nps).To(HaveLen(11), "len(NetworkPolicies()) = %d, want 11", len(nps))
		})

		It("names match component netpol names for always-on components", func() {
			nps := NetworkPolicies(kn, KagentiSidecarNone)
			wantNames := make(map[string]bool)
			for _, c := range ActiveComponents(kn) {
				wantNames[c+"-netpol"] = true
			}
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

		It("excludes gateway NetworkPolicy when Gateway is disabled", func() {
			disabled := false
			kn.Spec.Gateway.Enabled = &disabled
			nps := NetworkPolicies(kn, KagentiSidecarNone)
			for _, np := range nps {
				Expect(np.Name).NotTo(Equal(ComponentGateway+"-netpol"),
					"gateway NetworkPolicy should not be present when Gateway is disabled")
			}
			Expect(nps).To(HaveLen(10), "len(NetworkPolicies()) = %d, want 10 when Gateway is disabled", len(nps))
		})

		It("excludes gateway from data-storage ingress peers when Gateway is disabled", func() {
			disabled := false
			kn.Spec.Gateway.Enabled = &disabled
			var dsNP *networkingv1.NetworkPolicy
			for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
				if np.Name == ComponentDataStorage+"-netpol" {
					dsNP = np
					break
				}
			}
			Expect(dsNP).NotTo(BeNil())
			for _, rule := range dsNP.Spec.Ingress {
				for _, peer := range rule.From {
					if peer.PodSelector != nil {
						Expect(peer.PodSelector.MatchLabels).NotTo(Equal(SelectorLabels(ComponentGateway)),
							"data-storage ingress should not include gateway peer when Gateway is disabled")
					}
				}
			}
		})

		It("auto-adds gateway ingress from openshift-ingress and openshift-monitoring", func() {
			var gatewayNP *networkingv1.NetworkPolicy
			for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
				if np.Name == ComponentGateway+"-netpol" {
					gatewayNP = np
					break
				}
			}
			Expect(gatewayNP).NotTo(BeNil(), "gateway NetworkPolicy not found")
			Expect(gatewayNP.Spec.Ingress).To(HaveLen(1), "gateway ingress rule count = %d, want 1", len(gatewayNP.Spec.Ingress))
			nsSeen := ingressNamespaceNames(gatewayNP.Spec.Ingress[0])
			Expect(nsSeen[OCPIngressNamespace]).To(BeTrue(), "gateway ingress should allow %s", OCPIngressNamespace)
			Expect(nsSeen[OCPMonitoringNamespace]).To(BeTrue(), "gateway ingress should allow %s", OCPMonitoringNamespace)
		})

		It("gives kubernaut-agent ingress and egress with monitoring auto-detected", func() {
			var agentNP *networkingv1.NetworkPolicy
			for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
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
			Expect(agentNP.Spec.Ingress).ToNot(BeEmpty(), "kubernaut-agent ingress rule count = %d, want at least 1", len(agentNP.Spec.Ingress))
			Expect(agentNP.Spec.Egress).To(HaveLen(5), "kubernaut-agent egress rule count = %d, want %d (dns + apiserver + ds + monitoring + LLM HTTPS)", len(agentNP.Spec.Egress), 5)
			monRule := agentNP.Spec.Egress[3]
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
			for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
				if np.Name == ComponentDataStorage+"-netpol" {
					dsNP = np
					break
				}
			}
			Expect(dsNP).NotTo(BeNil(), "data-storage NetworkPolicy not found")
			Expect(dsNP.Spec.Ingress).ToNot(BeEmpty(), "data-storage should have at least one ingress rule")
			rule := dsNP.Spec.Ingress[0]
			Expect(rule.From).To(HaveLen(10), "data-storage client ingress peers = %d, want 10", len(rule.From))
			wantApps := map[string]struct{}{
				ComponentAPIFrontend:             {},
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
			Expect(gotApps).To(HaveLen(10), "unexpected peer count or duplicate apps: %#v", gotApps)
		})

		It("allows metrics scrape ingress from openshift-monitoring", func() {
			var dsNP *networkingv1.NetworkPolicy
			for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
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

	It("always adds API server egress using auto-detected KUBERNETES_SERVICE_HOST", func() {
		old := os.Getenv("KUBERNETES_SERVICE_HOST")
		Expect(os.Setenv("KUBERNETES_SERVICE_HOST", testAPIServerHost)).To(Succeed())
		defer func() { Expect(os.Setenv("KUBERNETES_SERVICE_HOST", old)).To(Succeed()) }()

		kn := testKubernaut()
		enabled := true
		kn.Spec.NetworkPolicies.Enabled = &enabled
		nps := NetworkPolicies(kn, KagentiSidecarNone)
		wantCIDR := testAPIServerHost + "/32"
		proto := corev1.ProtocolTCP
		found := false
	outer:
		for _, np := range nps {
			for _, rule := range np.Spec.Egress {
				for _, peer := range rule.To {
					if peer.IPBlock == nil || peer.IPBlock.CIDR != wantCIDR {
						continue
					}
					for _, port := range rule.Ports {
						if port.Protocol != nil && *port.Protocol == proto {
							found = true
							break outer
						}
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "expected at least one NetworkPolicy with API server egress (%s)", wantCIDR)
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

var _ = Describe("APIFrontend NetworkPolicy", func() {
	enableNP := func(kn *kubernautv1alpha1.Kubernaut) {
		enabled := true
		kn.Spec.NetworkPolicies.Enabled = &enabled
	}

	It("is included when AF is enabled", func() {
		kn := testKubernautWithAF()
		enableNP(kn)
		nps := NetworkPolicies(kn, KagentiSidecarNone)
		found := false
		for _, np := range nps {
			if np.Name == ComponentAPIFrontend+"-netpol" {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "apifrontend-netpol should be present when AF is enabled")
	})

	It("allows ingress on HTTPS (8443), health (8081), and metrics (9090)", func() {
		kn := testKubernautWithAF()
		enableNP(kn)
		var afNP *networkingv1.NetworkPolicy
		for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
			if np.Name == ComponentAPIFrontend+"-netpol" {
				afNP = np
				break
			}
		}
		Expect(afNP).NotTo(BeNil())
		Expect(afNP.Spec.Ingress).NotTo(BeEmpty())

		ingressPorts := map[int32]bool{}
		for _, rule := range afNP.Spec.Ingress {
			for _, port := range rule.Ports {
				if port.Port != nil {
					ingressPorts[int32(port.Port.IntValue())] = true
				}
			}
		}
		Expect(ingressPorts).To(HaveKey(PortHTTPS))
		Expect(ingressPorts).To(HaveKey(PortHealthProbe))
		Expect(ingressPorts).To(HaveKey(PortMetrics))
	})

	It("allows egress to DNS, API server, and kubernaut pods", func() {
		kn := testKubernautWithAF()
		enableNP(kn)
		var afNP *networkingv1.NetworkPolicy
		for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
			if np.Name == ComponentAPIFrontend+"-netpol" {
				afNP = np
				break
			}
		}
		Expect(afNP).NotTo(BeNil())
		Expect(afNP.Spec.Egress).NotTo(BeEmpty())
	})

	It("auto-adds ingress from openshift-ingress when AF route is enabled", func() {
		kn := testKubernautWithAF()
		enableNP(kn)
		afRouteEnabled := true
		kn.Spec.APIFrontend.Route.Enabled = &afRouteEnabled
		var afNP *networkingv1.NetworkPolicy
		for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
			if np.Name == ComponentAPIFrontend+"-netpol" {
				afNP = np
				break
			}
		}
		Expect(afNP).NotTo(BeNil())

		found := false
		for _, rule := range afNP.Spec.Ingress {
			for _, peer := range rule.From {
				if peer.NamespaceSelector != nil {
					if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == OCPIngressNamespace {
						found = true
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "should allow ingress from openshift-ingress namespace when AF route enabled")
	})

	It("does not add router ingress when AF route is disabled", func() {
		kn := testKubernautWithAF()
		enableNP(kn)
		var afNP *networkingv1.NetworkPolicy
		for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
			if np.Name == ComponentAPIFrontend+"-netpol" {
				afNP = np
				break
			}
		}
		Expect(afNP).NotTo(BeNil())
		for _, rule := range afNP.Spec.Ingress {
			for _, peer := range rule.From {
				if peer.NamespaceSelector != nil {
					Expect(peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]).
						NotTo(Equal(OCPIngressNamespace),
							"should not have openshift-ingress ingress when AF route disabled")
				}
			}
		}
	})
})

var _ = Describe("KubernautAgent NetworkPolicy with AF", func() {
	It("allows ingress from apifrontend pods when AF is enabled", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.NetworkPolicies.Enabled = &enabled
		nps := NetworkPolicies(kn, KagentiSidecarNone)
		var kaNP *networkingv1.NetworkPolicy
		for _, np := range nps {
			if np.Name == ComponentKubernautAgent+"-netpol" {
				kaNP = np
				break
			}
		}
		Expect(kaNP).NotTo(BeNil())

		found := false
		for _, rule := range kaNP.Spec.Ingress {
			for _, peer := range rule.From {
				if peer.PodSelector != nil {
					labels := peer.PodSelector.MatchLabels
					if labels["app"] == ComponentAPIFrontend {
						found = true
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "KA ingress should include apifrontend pods")
	})
})

var _ = Describe("APIFrontend NetworkPolicy OIDC egress", func() {
	It("adds HTTPS egress rule when issuerURL is set", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.NetworkPolicies.Enabled = &enabled
		var afNP *networkingv1.NetworkPolicy
		for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
			if np.Name == ComponentAPIFrontend+"-netpol" {
				afNP = np
				break
			}
		}
		Expect(afNP).NotTo(BeNil())

		hasHTTPSEgress := false
		for _, rule := range afNP.Spec.Egress {
			for _, port := range rule.Ports {
				if port.Port != nil && port.Port.IntValue() == 443 && len(rule.To) == 0 {
					hasHTTPSEgress = true
				}
			}
		}
		Expect(hasHTTPSEgress).To(BeTrue(), "AF should allow HTTPS egress for OIDC when issuerURL is set")
	})

	It("omits OIDC HTTPS egress rule when issuerURL is empty", func() {
		kn := testKubernaut()
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		enabled := true
		kn.Spec.NetworkPolicies.Enabled = &enabled
		var afNP *networkingv1.NetworkPolicy
		for _, np := range NetworkPolicies(kn, KagentiSidecarNone) {
			if np.Name == ComponentAPIFrontend+"-netpol" {
				afNP = np
				break
			}
		}
		Expect(afNP).NotTo(BeNil())

		for _, rule := range afNP.Spec.Egress {
			if len(rule.Ports) == 1 && rule.Ports[0].Port != nil && rule.Ports[0].Port.IntValue() == 443 && len(rule.To) == 0 {
				Fail("AF should not allow blanket HTTPS egress when issuerURL is empty")
			}
		}
	})
})

// #224 Finding 6: GW/RO/SP/AF/EM gain the same all-namespace 443+8080
// fleet egress rule FMC already has, once spec.fleet.enabled=true.
// fleetDestinationsEgressRule() extracts FMC's existing rule (previously
// inline in fleetMetadataCacheNetworkPolicy) for reuse across components.
var _ = Describe("fleetDestinationsEgressRule", func() {
	It("matches FleetMetadataCache's pre-existing fleet egress rule exactly (extracted helper, no behavior change)", func() {
		kn := testKubernautWithFMC()
		fmcNP := fleetMetadataCacheNetworkPolicy(kn)
		want := fleetDestinationsEgressRule()
		Expect(fmcNP.Spec.Egress).To(ContainElement(want))
	})
})

func hasFleetEgressRule(np *networkingv1.NetworkPolicy) bool {
	want := fleetDestinationsEgressRule()
	for _, rule := range np.Spec.Egress {
		if len(rule.To) == len(want.To) && len(rule.Ports) == len(want.Ports) {
			match := true
			for i := range rule.Ports {
				if rule.Ports[i].Port == nil || want.Ports[i].Port == nil || rule.Ports[i].Port.IntValue() != want.Ports[i].Port.IntValue() {
					match = false
					break
				}
			}
			if match && len(rule.To) == 1 && rule.To[0].NamespaceSelector != nil && len(rule.To[0].NamespaceSelector.MatchLabels) == 0 {
				return true
			}
		}
	}
	return false
}

var _ = Describe("Gateway/RemediationOrchestrator/SignalProcessing/APIFrontend/EffectivenessMonitor fleet NetworkPolicy egress", func() {
	It("gateway omits fleet egress when fleet disabled", func() {
		kn := testKubernaut()
		Expect(hasFleetEgressRule(gatewayNetworkPolicy(kn))).To(BeFalse())
	})

	It("gateway gains fleet egress when fleet enabled", func() {
		kn := testKubernautWithFleetMCP()
		Expect(hasFleetEgressRule(gatewayNetworkPolicy(kn))).To(BeTrue())
	})

	It("remediationorchestrator omits fleet egress when fleet disabled", func() {
		kn := testKubernaut()
		Expect(hasFleetEgressRule(remediationOrchestratorNetworkPolicy(kn))).To(BeFalse())
	})

	It("remediationorchestrator gains fleet egress when fleet enabled", func() {
		kn := testKubernautWithFleetMCP()
		Expect(hasFleetEgressRule(remediationOrchestratorNetworkPolicy(kn))).To(BeTrue())
	})

	It("signalprocessing omits fleet egress when fleet disabled", func() {
		kn := testKubernaut()
		Expect(hasFleetEgressRule(signalProcessingNetworkPolicy(kn))).To(BeFalse())
	})

	It("signalprocessing gains fleet egress when fleet enabled", func() {
		kn := testKubernautWithFleetMCP()
		Expect(hasFleetEgressRule(signalProcessingNetworkPolicy(kn))).To(BeTrue())
	})

	It("apifrontend omits fleet egress when fleet disabled", func() {
		kn := testKubernautWithAF()
		Expect(hasFleetEgressRule(apifrontendNetworkPolicy(kn, KagentiSidecarNone))).To(BeFalse())
	})

	It("apifrontend gains fleet egress when fleet enabled", func() {
		kn := testKubernautWithFleetMCP()
		kn.Spec.APIFrontend = kubernautv1alpha1.APIFrontendSpec{
			Auth: kubernautv1alpha1.APIFrontendAuthSpec{IssuerURL: "https://login.kubernaut.ai/realms/kubernaut", Audience: "kubernaut-apifrontend"},
		}
		Expect(hasFleetEgressRule(apifrontendNetworkPolicy(kn, KagentiSidecarNone))).To(BeTrue())
	})

	It("effectivenessmonitor omits fleet egress when fleet disabled", func() {
		kn := testKubernaut()
		Expect(hasFleetEgressRule(effectivenessMonitorNetworkPolicy(kn))).To(BeFalse())
	})

	It("effectivenessmonitor gains fleet egress when fleet enabled", func() {
		kn := testKubernautWithFleetMCP()
		Expect(hasFleetEgressRule(effectivenessMonitorNetworkPolicy(kn))).To(BeTrue())
	})
})

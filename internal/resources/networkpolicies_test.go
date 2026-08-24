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
	"errors"
	"fmt"
	"os"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"

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
			Expect(NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)).To(BeNil(), "NetworkPolicies() = %#v, want nil when enabled=false", NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone))
		})

		It("returns nil when enabled is unset", func() {
			kn := testKubernaut()
			Expect(NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)).To(BeNil(), "NetworkPolicies() = %#v, want nil when enabled is not set (default false)", NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone))
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
			nps := NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)
			Expect(nps).To(HaveLen(11), "len(NetworkPolicies()) = %d, want 11", len(nps))
		})

		It("names match component netpol names for always-on components", func() {
			nps := NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)
			wantNames := make(map[string]bool)
			for _, c := range ActiveComponents(kn, testKnV2(kn)) {
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
			nps := NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)
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
			for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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
			for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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
			for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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
			// #298: Prometheus and AlertManager are now separate egress rules
			// (dns + apiserver + ds + prometheus + alertmanager + LLM HTTPS)
			// since each destination's namespace is resolved independently.
			Expect(agentNP.Spec.Egress).To(HaveLen(6), "kubernaut-agent egress rule count = %d, want %d (dns + apiserver + ds + prometheus + alertmanager + LLM HTTPS)", len(agentNP.Spec.Egress), 6)
			promRule := agentNP.Spec.Egress[3]
			Expect(promRule.To).To(HaveLen(1), "prometheus egress peer count = %d, want 1", len(promRule.To))
			promNS := promRule.To[0].NamespaceSelector
			Expect(promNS != nil && promNS.MatchLabels["kubernetes.io/metadata.name"] == OCPMonitoringNamespace).To(BeTrue(),
				"prometheus egress namespace selector = %v, want %s", promNS, OCPMonitoringNamespace)
			Expect(promRule.Ports).To(HaveLen(1), "prometheus egress port count = %d, want 1", len(promRule.Ports))
			Expect(promRule.Ports[0].Port.IntValue()).To(Equal(9091), "prometheus egress port[0] = %d, want 9091 (Thanos)", promRule.Ports[0].Port.IntValue())

			amRule := agentNP.Spec.Egress[4]
			Expect(amRule.Ports).To(HaveLen(2), "alertmanager egress port count = %d, want 2", len(amRule.Ports))
			Expect(amRule.Ports[0].Port.IntValue()).To(Equal(9094), "alertmanager egress port[0] = %d, want 9094 (documented Service port)", amRule.Ports[0].Port.IntValue())
			Expect(amRule.Ports[1].Port.IntValue()).To(Equal(9095), "MON-002 [SC-7]: alertmanager egress port[1] = %d, want 9095 -- OCP's alertmanager-main Service DNATs its documented 'web' port (9094) to the kube-rbac-proxy-web sidecar's real container port (9095); OVN-Kubernetes evaluates egress NetworkPolicy ACLs after this DNAT (apply-after-lb=true, see dnsEgressRule), so omitting 9095 silently drops all AlertManager traffic on real OpenShift despite an otherwise-correct policy", amRule.Ports[1].Port.IntValue())
		})

		It("allows data-storage ingress from all client components", func() {
			var dsNP *networkingv1.NetworkPolicy
			for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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
			for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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
		resetAPIServerIPsCacheForTest()
		stubLiveResolveAPIServerIPsForTest(nil, errors.New("no in-cluster config in test"))

		old := os.Getenv("KUBERNETES_SERVICE_HOST")
		Expect(os.Setenv("KUBERNETES_SERVICE_HOST", testAPIServerHost)).To(Succeed())
		defer func() { Expect(os.Setenv("KUBERNETES_SERVICE_HOST", old)).To(Succeed()) }()

		kn := testKubernaut()
		enabled := true
		kn.Spec.NetworkPolicies.Enabled = &enabled
		nps := NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)
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

	It("uses live-resolved API server IPs when the lookup succeeds, not KUBERNETES_SERVICE_HOST", func() {
		resetAPIServerIPsCacheForTest()
		const liveIP = "192.168.100.50"
		stubLiveResolveAPIServerIPsForTest([]string{liveIP}, nil)

		old := os.Getenv("KUBERNETES_SERVICE_HOST")
		Expect(os.Setenv("KUBERNETES_SERVICE_HOST", "172.30.0.1")).To(Succeed())
		defer func() { Expect(os.Setenv("KUBERNETES_SERVICE_HOST", old)).To(Succeed()) }()

		kn := testKubernaut()
		enabled := true
		kn.Spec.NetworkPolicies.Enabled = &enabled
		nps := NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)

		Expect(anyEgressHasIPBlock(nps, liveIP+"/32")).To(BeTrue(), "expected egress to the live-resolved API server IP %s", liveIP)
		Expect(anyEgressHasIPBlock(nps, "172.30.0.1/32")).To(BeFalse(), "must not fall back to the ClusterIP when the live lookup succeeds")
	})

	It("reuses the last known-good API server IPs when a live lookup fails, instead of falling back to the ClusterIP", func() {
		resetAPIServerIPsCacheForTest()
		const goodIP = "192.168.100.51"

		old := os.Getenv("KUBERNETES_SERVICE_HOST")
		Expect(os.Setenv("KUBERNETES_SERVICE_HOST", "172.30.0.1")).To(Succeed())
		defer func() { Expect(os.Setenv("KUBERNETES_SERVICE_HOST", old)).To(Succeed()) }()

		// First reconcile: live lookup succeeds, populating the cache.
		stubLiveResolveAPIServerIPsForTest([]string{goodIP}, nil)
		kn := testKubernaut()
		enabled := true
		kn.Spec.NetworkPolicies.Enabled = &enabled
		_ = NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)

		// Second reconcile: live lookup fails (e.g. transient API error).
		stubLiveResolveAPIServerIPsForTest(nil, errors.New("transient apiserver error"))
		nps := NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)

		Expect(anyEgressHasIPBlock(nps, goodIP+"/32")).To(BeTrue(), "expected egress to still target the last known-good IP %s", goodIP)
		Expect(anyEgressHasIPBlock(nps, "172.30.0.1/32")).To(BeFalse(), "must fail closed (reuse last known-good), never fall back to the ClusterIP once a cache exists")
	})
})

func anyEgressHasIPBlock(nps []*networkingv1.NetworkPolicy, cidr string) bool {
	for _, np := range nps {
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.IPBlock != nil && peer.IPBlock.CIDR == cidr {
					return true
				}
			}
		}
	}
	return false
}

// resetAPIServerIPsCacheForTest clears the package-level last-known-good IP
// cache so each test starts from a clean slate regardless of execution
// order (Ginkgo specs share process-level state within this package).
func resetAPIServerIPsCacheForTest() {
	apiServerIPsCache.mu.Lock()
	apiServerIPsCache.ips = nil
	apiServerIPsCache.mu.Unlock()
}

// stubLiveResolveAPIServerIPsForTest swaps the live-lookup seam for the
// duration of the current spec (restored via DeferCleanup), letting tests
// simulate live Endpoints lookup success/failure without a real in-cluster
// config.
func stubLiveResolveAPIServerIPsForTest(ips []string, err error) {
	original := liveResolveAPIServerIPsFunc
	liveResolveAPIServerIPsFunc = func() ([]string, error) { return ips, err }
	DeferCleanup(func() { liveResolveAPIServerIPsFunc = original })
}

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
		nps := NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)
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
		for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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
		for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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
		for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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
		for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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

	// Issue #1839 (upstream kubernaut, merged as PR #1841): AF's severity-triage
	// pipeline calls Prometheus (Thanos Querier) directly for GetAlerts/GetRules/
	// InstantQuery via afSeverityTriageConfig's PrometheusURL (thanos-querier.
	// openshift-monitoring.svc:9091), but AF's NetworkPolicy egress never opened
	// port 9091 -- only the metrics port (9090, meant for the reverse/ingress
	// direction) was ever allowed toward openshift-monitoring. On any cluster
	// enforcing NetworkPolicy (OVN-Kubernetes on OpenShift), every AF->Prometheus
	// call was silently dropped. Upstream's now-removed Tier 3 LLM fallback
	// absorbed the resulting error identically to a genuine "no alert data"
	// response, fully masking the connectivity gap until Tier 3's removal
	// exposed it (see DD-AF-010 upstream).
	It("allows egress to Thanos Querier (9091) when monitoring is enabled", func() {
		kn := testKubernautWithAF()
		enableNP(kn)
		var afNP *networkingv1.NetworkPolicy
		for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
			if np.Name == ComponentAPIFrontend+"-netpol" {
				afNP = np
				break
			}
		}
		Expect(afNP).NotTo(BeNil())

		found := false
		for _, rule := range afNP.Spec.Egress {
			for _, peer := range rule.To {
				if peer.NamespaceSelector == nil ||
					peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != OCPMonitoringNamespace {
					continue
				}
				for _, port := range rule.Ports {
					if port.Port != nil && int32(port.Port.IntValue()) == 9091 {
						found = true
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "AF egress should allow port 9091 (Thanos Querier) to %s for severity-triage Prometheus calls", OCPMonitoringNamespace)
	})

	// MON-001 [SC-7]: AF's severityTriage config has no AlertManager client
	// (only afSeverityTriageConfig.PrometheusURL exists) -- granting it
	// AlertManager egress (as the old shared monitoringStackEgressRule did)
	// is unused, over-provisioned egress. Least-privilege: AF should never
	// carry a rule for a destination it has no client for.
	It("MON-001 [SC-7]: does not allow egress to AlertManager (9094/9095) -- AF has no AlertManager client", func() {
		kn := testKubernautWithAF()
		enableNP(kn)
		var afNP *networkingv1.NetworkPolicy
		for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
			if np.Name == ComponentAPIFrontend+"-netpol" {
				afNP = np
				break
			}
		}
		Expect(afNP).NotTo(BeNil())

		found := false
		for _, rule := range afNP.Spec.Egress {
			for _, port := range rule.Ports {
				if port.Port != nil && (int32(port.Port.IntValue()) == 9094 || int32(port.Port.IntValue()) == 9095) {
					found = true
				}
			}
		}
		Expect(found).To(BeFalse(), "AF egress should not allow AlertManager ports (9094/9095) -- AF never queries AlertManager")
	})
})

var _ = Describe("KubernautAgent NetworkPolicy with AF", func() {
	It("allows ingress from apifrontend pods when AF is enabled", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.NetworkPolicies.Enabled = &enabled
		nps := NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone)
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
		for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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
		for _, np := range NetworkPolicies(kn, testKnV2(kn), KagentiSidecarNone) {
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
		kn, knV2 := testKubernautWithFMC()
		fmcNP := fleetMetadataCacheNetworkPolicy(kn, knV2)
		want := fleetDestinationsEgressRule(knV2)
		Expect(fmcNP.Spec.Egress).To(ContainElement(want))
	})

	// #392: the namespaceSelector peer alone cannot match a Route-fronted
	// MCP Gateway, which resolves to the Ingress Router's hostNetwork VIP,
	// not a namespaced Pod IP (confirmed via live on-cluster curl
	// reproduction). ipBlock peers, by contrast, match on the literal
	// packet destination IP regardless of whether OVN can attribute it to
	// a namespace/pod, so resolving the configured destination hostnames'
	// real IPs and adding them as ipBlock peers closes that gap.
	It("EGR-392-003 [AC-4, SC-7]: adds an ipBlock peer for mcpGatewayEndpoint's resolved IP alongside the existing namespaceSelector peer", func() {
		_, knV2 := testKubernautWithFleetMCP()
		stubLiveResolveFleetHostIPsForTest(map[string][]string{
			"mcp-gateway.example.com": {"203.0.113.10"},
		})

		rule := fleetDestinationsEgressRule(knV2)

		Expect(hasNamespaceSelectorPeer(rule)).To(BeTrue(), "must keep the namespaceSelector peer for in-cluster-Service-backed destinations")
		Expect(hasIPBlockPeer(rule, "203.0.113.10/32")).To(BeTrue(), "must add an ipBlock peer for the resolved MCP Gateway IP")
	})

	It("EGR-392-004 [AC-4, SC-7]: resolves and adds ipBlock peers for both mcpGatewayEndpoint and oauth2.tokenURL when both are set", func() {
		_, knV2 := testKubernautWithFMC()
		stubLiveResolveFleetHostIPsForTest(map[string][]string{
			"mcp-gateway.example.com": {"203.0.113.10"},
			"keycloak.example.com":    {"203.0.113.20"},
		})

		rule := fleetDestinationsEgressRule(knV2)

		Expect(hasIPBlockPeer(rule, "203.0.113.10/32")).To(BeTrue(), "must resolve the MCP Gateway host")
		Expect(hasIPBlockPeer(rule, "203.0.113.20/32")).To(BeTrue(), "must resolve the OAuth2 token endpoint host")
	})

	It("EGR-392-005: adds a /128 ipBlock for an IPv6-resolved address", func() {
		_, knV2 := testKubernautWithFleetMCP()
		stubLiveResolveFleetHostIPsForTest(map[string][]string{
			"mcp-gateway.example.com": {"2001:db8::1"},
		})

		rule := fleetDestinationsEgressRule(knV2)
		Expect(hasIPBlockPeer(rule, "2001:db8::1/128")).To(BeTrue())
	})

	It("EGR-392-006: omits the ipBlock peer (namespaceSelector-only) when DNS resolution fails and no cache exists yet", func() {
		_, knV2 := testKubernautWithFleetMCP()
		stubLiveResolveFleetHostIPsForTestErr(errors.New("no such host"))

		rule := fleetDestinationsEgressRule(knV2)
		Expect(hasNamespaceSelectorPeer(rule)).To(BeTrue())
		Expect(rule.To).To(HaveLen(1), "no ipBlock peer should be added when resolution has never succeeded")
	})

	It("EGR-392-007: fails closed by reusing the last known-good IP after a transient DNS failure", func() {
		_, knV2 := testKubernautWithFleetMCP()

		stubLiveResolveFleetHostIPsForTest(map[string][]string{"mcp-gateway.example.com": {"203.0.113.10"}})
		_ = fleetDestinationsEgressRule(knV2)

		stubLiveResolveFleetHostIPsForTestErr(errors.New("transient DNS error"))
		rule := fleetDestinationsEgressRule(knV2)

		Expect(hasIPBlockPeer(rule, "203.0.113.10/32")).To(BeTrue(), "must reuse the last known-good IP, never silently drop egress once resolved once")
	})
})

func hasNamespaceSelectorPeer(rule networkingv1.NetworkPolicyEgressRule) bool {
	for _, peer := range rule.To {
		if peer.NamespaceSelector != nil && len(peer.NamespaceSelector.MatchLabels) == 0 {
			return true
		}
	}
	return false
}

func hasIPBlockPeer(rule networkingv1.NetworkPolicyEgressRule, cidr string) bool {
	for _, peer := range rule.To {
		if peer.IPBlock != nil && peer.IPBlock.CIDR == cidr {
			return true
		}
	}
	return false
}

// resetFleetHostIPsCacheForTest clears the package-level per-hostname IP
// cache so each test starts from a clean slate regardless of execution
// order (Ginkgo specs share process-level state within this package).
func resetFleetHostIPsCacheForTest() {
	fleetHostIPsCache.mu.Lock()
	fleetHostIPsCache.ips = nil
	fleetHostIPsCache.mu.Unlock()
}

// stubLiveResolveFleetHostIPsForTest swaps the live DNS-lookup seam for the
// duration of the current spec (restored via DeferCleanup) to return the
// given per-hostname IPs, letting tests simulate DNS success
// deterministically without a real network lookup.
func stubLiveResolveFleetHostIPsForTest(byHost map[string][]string) {
	original := liveResolveFleetHostIPsFunc
	liveResolveFleetHostIPsFunc = func(host string) ([]string, error) {
		if ips, ok := byHost[host]; ok {
			return ips, nil
		}
		return nil, fmt.Errorf("no stubbed resolution for host %q", host)
	}
	DeferCleanup(func() { liveResolveFleetHostIPsFunc = original })
}

// stubLiveResolveFleetHostIPsForTestErr swaps the live DNS-lookup seam to
// always fail with err, for testing the fail-closed cache-reuse path.
func stubLiveResolveFleetHostIPsForTestErr(err error) {
	original := liveResolveFleetHostIPsFunc
	liveResolveFleetHostIPsFunc = func(string) ([]string, error) { return nil, err }
	DeferCleanup(func() { liveResolveFleetHostIPsFunc = original })
}

func hasFleetEgressRule(np *networkingv1.NetworkPolicy, knV2 *kubernautv1alpha2.Kubernaut) bool {
	want := fleetDestinationsEgressRule(knV2)
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
		knV2 := testKnV2(kn)
		Expect(hasFleetEgressRule(gatewayNetworkPolicy(kn, knV2), knV2)).To(BeFalse())
	})

	It("gateway gains fleet egress when fleet enabled", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		Expect(hasFleetEgressRule(gatewayNetworkPolicy(kn, knV2), knV2)).To(BeTrue())
	})

	It("remediationorchestrator omits fleet egress when fleet disabled", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		Expect(hasFleetEgressRule(remediationOrchestratorNetworkPolicy(kn, knV2), knV2)).To(BeFalse())
	})

	It("remediationorchestrator gains fleet egress when fleet enabled", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		Expect(hasFleetEgressRule(remediationOrchestratorNetworkPolicy(kn, knV2), knV2)).To(BeTrue())
	})

	It("signalprocessing omits fleet egress when fleet disabled", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		Expect(hasFleetEgressRule(signalProcessingNetworkPolicy(kn, knV2), knV2)).To(BeFalse())
	})

	It("signalprocessing gains fleet egress when fleet enabled", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		Expect(hasFleetEgressRule(signalProcessingNetworkPolicy(kn, knV2), knV2)).To(BeTrue())
	})

	It("apifrontend omits fleet egress when fleet disabled", func() {
		kn := testKubernautWithAF()
		knV2 := testKnV2(kn)
		Expect(hasFleetEgressRule(apifrontendNetworkPolicy(kn, knV2, KagentiSidecarNone), knV2)).To(BeFalse())
	})

	It("apifrontend gains fleet egress when fleet enabled", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		kn.Spec.APIFrontend = kubernautv1alpha1.APIFrontendSpec{
			Auth: kubernautv1alpha1.APIFrontendAuthSpec{IssuerURL: "https://login.kubernaut.ai/realms/kubernaut", Audience: "kubernaut-apifrontend"},
		}
		Expect(hasFleetEgressRule(apifrontendNetworkPolicy(kn, knV2, KagentiSidecarNone), knV2)).To(BeTrue())
	})

	It("effectivenessmonitor omits fleet egress when fleet disabled", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		Expect(hasFleetEgressRule(effectivenessMonitorNetworkPolicy(kn, knV2), knV2)).To(BeFalse())
	})

	It("effectivenessmonitor gains fleet egress when fleet enabled", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		Expect(hasFleetEgressRule(effectivenessMonitorNetworkPolicy(kn, knV2), knV2)).To(BeTrue())
	})

	It("KFG-030 [AC-4]: kubernautagent omits fleet egress when fleet disabled", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		Expect(hasFleetEgressRule(kubernautAgentNetworkPolicy(kn, knV2), knV2)).To(BeFalse())
	})

	It("KFG-031 [AC-4]: kubernautagent gains fleet egress when fleet enabled, so KA can reach the MCP Gateway for GatewayDiscoverer tool calls", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		Expect(hasFleetEgressRule(kubernautAgentNetworkPolicy(kn, knV2), knV2)).To(BeTrue())
	})

	It("EGR-392-001 [AC-4]: workflowexecution omits fleet egress when fleet disabled", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		Expect(hasFleetEgressRule(workflowExecutionNetworkPolicy(kn, knV2), knV2)).To(BeFalse())
	})

	It("EGR-392-002 [AC-4]: workflowexecution gains fleet egress when fleet enabled, closing the gap missed by the original #224/#204 retrofit", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		Expect(hasFleetEgressRule(workflowExecutionNetworkPolicy(kn, knV2), knV2)).To(BeTrue())
	})
})

// #298: EM's monitoring egress previously used ipWorldPeers() (all
// destinations, 0.0.0.0/0 and ::/0) on ports 9090/9093 -- both wrong (EM's
// actual calls go to Thanos Querier :9091 and AlertManager :9094/9095) and
// over-broad (no namespace scoping at all). This left EM unable to reach
// real monitoring endpoints on any cluster enforcing NetworkPolicy while
// simultaneously granting it unscoped egress to the entire internet on the
// (wrong) ports -- a defense-in-depth regression with no compensating
// benefit. Confirmed on two independent CNIs (kind+Calico, live OCP 4.18
// OVN-Kubernetes) via manual spike testing (not reproducible in envtest,
// which has no CNI/NetworkPolicy enforcement).
var _ = Describe("EffectivenessMonitor NetworkPolicy monitoring egress (#298)", func() {
	It("MON-003 [SC-7]: allows egress to Prometheus (9091) and AlertManager (9094/9095) scoped to openshift-monitoring, not the whole internet", func() {
		kn := testKubernaut()
		np := effectivenessMonitorNetworkPolicy(kn, testKnV2(kn))

		var promRule, amRule *networkingv1.NetworkPolicyEgressRule
		for i := range np.Spec.Egress {
			rule := &np.Spec.Egress[i]
			for _, port := range rule.Ports {
				if port.Port == nil {
					continue
				}
				switch int32(port.Port.IntValue()) {
				case 9091:
					promRule = rule
				case 9095:
					amRule = rule
				}
			}
		}
		Expect(promRule).NotTo(BeNil(), "EM egress should allow port 9091 (Thanos Querier)")
		Expect(amRule).NotTo(BeNil(), "EM egress should allow port 9095 (AlertManager real post-DNAT port)")

		for _, rule := range []*networkingv1.NetworkPolicyEgressRule{promRule, amRule} {
			Expect(rule.To).To(HaveLen(1), "monitoring egress peer count = %d, want 1 (namespace-scoped, not all-destination)", len(rule.To))
			ns := rule.To[0].NamespaceSelector
			Expect(ns != nil && ns.MatchLabels["kubernetes.io/metadata.name"] == OCPMonitoringNamespace).To(BeTrue(),
				"monitoring egress should be scoped to %s, got %v", OCPMonitoringNamespace, ns)
			Expect(rule.To[0].IPBlock).To(BeNil(), "monitoring egress must not use an all-destination IPBlock peer")
		}
	})

	It("MON-004 [SC-7]: does not allow unrestricted (0.0.0.0/0) egress on the old wrong ports 9090/9093", func() {
		kn := testKubernaut()
		np := effectivenessMonitorNetworkPolicy(kn, testKnV2(kn))
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.IPBlock == nil {
					continue
				}
				for _, port := range rule.Ports {
					if port.Port == nil {
						continue
					}
					p := int32(port.Port.IntValue())
					Expect(p).NotTo(Or(Equal(int32(9090)), Equal(int32(9093))),
						"EM should not have all-destination egress on the old wrong ports 9090/9093")
				}
			}
		}
	})
})

// #298: three-tier URL/namespace resolution, shared by AF (Prometheus
// only)/KA/EM (both destinations). Documented on MonitoringSpec.
var _ = Describe("Monitoring egress URL resolution tiers (#298)", func() {
	It("MON-005 [SC-7]: tier 2 -- parses the namespace from an in-cluster *.svc URL override and scopes egress to it", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Monitoring.Prometheus.URL = testCustomPrometheusURL
		np := effectivenessMonitorNetworkPolicy(kn, knV2)

		found := false
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == "custom-monitoring" {
					found = true
				}
			}
		}
		Expect(found).To(BeTrue(), "egress should be scoped to the 'custom-monitoring' namespace parsed from the *.svc override URL")
	})

	It("MON-005b [SC-7]: tier 2 -- also parses the namespace from a *.svc.cluster.local URL override", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Monitoring.AlertManager.URL = "https://custom-alertmanager.custom-monitoring.svc.cluster.local:9094"
		np := effectivenessMonitorNetworkPolicy(kn, knV2)

		found := false
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == "custom-monitoring" {
					found = true
				}
			}
		}
		Expect(found).To(BeTrue(), "egress should be scoped to the 'custom-monitoring' namespace parsed from the *.svc.cluster.local override URL")
	})

	// MON-006: tier 3. An external/unparseable URL means the operator
	// cannot determine a namespace to scope egress to, so it omits its own
	// rule for that destination entirely rather than guessing an
	// overly-broad (or wrong) peer. The platform operator is expected to
	// supply a supplemental NetworkPolicy for that destination (documented
	// on MonitoringSpec) -- verified additive/non-conflicting on a live OCP
	// 4.18 cluster during the #298 spike.
	It("MON-006 [SC-7]: tier 3 -- omits the egress rule entirely for an external/unparseable URL override, rather than defaulting to openshift-monitoring or an all-destination peer", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Monitoring.Prometheus.URL = "https://prometheus.example.com:9091"
		np := effectivenessMonitorNetworkPolicy(kn, knV2)

		// AlertManager is left unset (tier 1, openshift-monitoring) in this
		// test, so only assert on the overridden destination's port (9091):
		// no rule should grant it once its URL is external/unparseable.
		for _, rule := range np.Spec.Egress {
			for _, port := range rule.Ports {
				if port.Port != nil && int32(port.Port.IntValue()) == 9091 {
					Fail("no egress rule should grant port 9091 when the Prometheus URL is external/unparseable -- expected the operator to omit its own rule for this destination")
				}
			}
		}
	})

	It("MON-007 [SC-7]: tier 1 -- an unset URL still resolves to the openshift-monitoring default (regression guard)", func() {
		kn := testKubernaut()
		np := effectivenessMonitorNetworkPolicy(kn, testKnV2(kn))

		found := false
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == OCPMonitoringNamespace {
					found = true
				}
			}
		}
		Expect(found).To(BeTrue(), "default (unset URL) should scope to openshift-monitoring")
	})
})

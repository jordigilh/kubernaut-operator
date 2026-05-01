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
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestNetworkPolicies_DisabledReturnsNil(t *testing.T) {
	kn := testKubernaut()
	disabled := false
	kn.Spec.NetworkPolicies.Enabled = &disabled
	if got := NetworkPolicies(kn); got != nil {
		t.Fatalf("NetworkPolicies() = %#v, want nil when enabled=false", got)
	}
}

func TestNetworkPolicies_DefaultDisabled(t *testing.T) {
	kn := testKubernaut()
	if got := NetworkPolicies(kn); got != nil {
		t.Fatalf("NetworkPolicies() = %#v, want nil when enabled is not set (default false)", got)
	}
}

func TestNetworkPolicies_ExplicitEnabled(t *testing.T) {
	kn := testKubernaut()
	enabled := true
	kn.Spec.NetworkPolicies.Enabled = &enabled
	nps := NetworkPolicies(kn)
	if len(nps) != 10 {
		t.Fatalf("len(NetworkPolicies()) = %d, want 10", len(nps))
	}
}

func TestNetworkPolicies_Names(t *testing.T) {
	kn := testKubernaut()
	enabled := true
	kn.Spec.NetworkPolicies.Enabled = &enabled
	nps := NetworkPolicies(kn)
	wantNames := make(map[string]bool)
	for _, c := range AllComponents() {
		wantNames[c+"-netpol"] = true
	}
	if len(wantNames) != 10 {
		t.Fatalf("internal: expected 10 component netpol names, got %d", len(wantNames))
	}
	for _, np := range nps {
		if !wantNames[np.Name] {
			t.Errorf("unexpected NetworkPolicy name %q", np.Name)
			continue
		}
		delete(wantNames, np.Name)
	}
	for name := range wantNames {
		t.Errorf("missing NetworkPolicy %q", name)
	}
}

func TestNetworkPolicies_GatewayIngress(t *testing.T) {
	kn := testKubernaut()
	enabled := true
	kn.Spec.NetworkPolicies.Enabled = &enabled
	kn.Spec.NetworkPolicies.GatewayIngressNamespaces = []string{"ns1", "ns2"}
	var gatewayNP *networkingv1.NetworkPolicy
	for _, np := range NetworkPolicies(kn) {
		if np.Name == ComponentGateway+"-netpol" {
			gatewayNP = np
			break
		}
	}
	if gatewayNP == nil {
		t.Fatal("gateway NetworkPolicy not found")
	}
	if len(gatewayNP.Spec.Ingress) != 1 {
		t.Fatalf("gateway ingress rule count = %d, want 1", len(gatewayNP.Spec.Ingress))
	}
	nsSeen := ingressNamespaceNames(gatewayNP.Spec.Ingress[0])
	if !nsSeen["ns1"] || !nsSeen["ns2"] {
		t.Errorf("gateway ingress should allow namespaces ns1 and ns2, got %#v", nsSeen)
	}
	if len(nsSeen) != 2 {
		t.Errorf("gateway ingress namespaces = %#v, want only ns1 and ns2", nsSeen)
	}
}

func TestNetworkPolicies_KubernautAgentIngressOnly(t *testing.T) {
	kn := testKubernaut()
	enabled := true
	kn.Spec.NetworkPolicies.Enabled = &enabled
	var agentNP *networkingv1.NetworkPolicy
	for _, np := range NetworkPolicies(kn) {
		if np.Name == ComponentKubernautAgent+"-netpol" {
			agentNP = np
			break
		}
	}
	if agentNP == nil {
		t.Fatal("kubernaut-agent NetworkPolicy not found")
	}
	if !slices.Equal(agentNP.Spec.PolicyTypes, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}) {
		t.Errorf("PolicyTypes = %v, want [Ingress] only", agentNP.Spec.PolicyTypes)
	}
	if len(agentNP.Spec.Egress) > 0 {
		t.Errorf("kubernaut-agent NP should have no Egress rules, got %d", len(agentNP.Spec.Egress))
	}
}

func TestNetworkPolicies_DataStorageIngressFromAllServices(t *testing.T) {
	kn := testKubernaut()
	enabled := true
	kn.Spec.NetworkPolicies.Enabled = &enabled
	var dsNP *networkingv1.NetworkPolicy
	for _, np := range NetworkPolicies(kn) {
		if np.Name == ComponentDataStorage+"-netpol" {
			dsNP = np
			break
		}
	}
	if dsNP == nil {
		t.Fatal("data-storage NetworkPolicy not found")
	}
	if len(dsNP.Spec.Ingress) < 1 {
		t.Fatal("data-storage should have at least one ingress rule")
	}
	rule := dsNP.Spec.Ingress[0]
	if len(rule.From) != 9 {
		t.Fatalf("data-storage client ingress peers = %d, want 9", len(rule.From))
	}
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
		if peer.PodSelector == nil || peer.PodSelector.MatchLabels == nil {
			t.Fatalf("expected PodSelector peer, got %#v", peer)
		}
		app := peer.PodSelector.MatchLabels["app"]
		if app == "" {
			t.Fatalf("peer missing app label: %#v", peer.PodSelector.MatchLabels)
		}
		gotApps[app] = struct{}{}
	}
	for a := range wantApps {
		if _, ok := gotApps[a]; !ok {
			t.Errorf("missing ingress from client %q", a)
		}
	}
	if len(gotApps) != 9 {
		t.Errorf("unexpected peer count or duplicate apps: %#v", gotApps)
	}
}

func TestNetworkPolicies_MetricsIngress(t *testing.T) {
	kn := testKubernaut()
	enabled := true
	kn.Spec.NetworkPolicies.Enabled = &enabled
	kn.Spec.NetworkPolicies.MonitoringNamespace = "openshift-monitoring"
	var dsNP *networkingv1.NetworkPolicy
	for _, np := range NetworkPolicies(kn) {
		if np.Name == ComponentDataStorage+"-netpol" {
			dsNP = np
			break
		}
	}
	if dsNP == nil {
		t.Fatal("data-storage NetworkPolicy not found")
	}
	p9090 := intstr.FromInt32(PortMetrics)
	proto := corev1.ProtocolTCP
	found := false
	for _, rule := range dsNP.Spec.Ingress {
		nsOK := false
		for _, peer := range rule.From {
			if peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels != nil &&
				peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == "openshift-monitoring" {
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
	if !found {
		t.Errorf("data-storage NP should allow metrics scrape ingress from openshift-monitoring on port %d", PortMetrics)
	}
}

func TestNetworkPolicies_APIServerEgress(t *testing.T) {
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
	if !found {
		t.Error("expected at least one NetworkPolicy with API server egress (10.0.0.0/16:443)")
	}
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

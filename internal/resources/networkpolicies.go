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
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// NetworkPolicies returns NetworkPolicy resources matching the upstream Helm
// chart v1.4.0-rc3 traffic matrix. Returns nil when NetworkPolicies are
// disabled on the CR.
func NetworkPolicies(kn *kubernautv1alpha1.Kubernaut) []*networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	if !spec.NetworkPoliciesEnabled() {
		return nil
	}
	return []*networkingv1.NetworkPolicy{
		gatewayNetworkPolicy(kn),
		dataStorageNetworkPolicy(kn),
		aiAnalysisNetworkPolicy(kn),
		signalProcessingNetworkPolicy(kn),
		remediationOrchestratorNetworkPolicy(kn),
		workflowExecutionNetworkPolicy(kn),
		notificationNetworkPolicy(kn),
		effectivenessMonitorNetworkPolicy(kn),
		authWebhookNetworkPolicy(kn),
		kubernautAgentNetworkPolicy(kn),
	}
}

func gatewayNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	protoTCP := corev1.ProtocolTCP
	p8080 := intstr.FromInt32(PortHTTP)

	var ingressFrom []networkingv1.NetworkPolicyPeer
	for _, ns := range spec.GatewayIngressNamespaces {
		if ns == "" {
			continue
		}
		ingressFrom = append(ingressFrom, networkingv1.NetworkPolicyPeer{
			NamespaceSelector: namespaceNameSelector(ns),
		})
	}
	if spec.MonitoringNamespace != "" {
		ingressFrom = append(ingressFrom, networkingv1.NetworkPolicyPeer{
			NamespaceSelector: namespaceNameSelector(spec.MonitoringNamespace),
		})
	}

	ingress := []networkingv1.NetworkPolicyIngressRule{}
	if len(ingressFrom) > 0 {
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From: ingressFrom,
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p8080},
			},
		})
	}

	egress := []networkingv1.NetworkPolicyEgressRule{dnsEgressRule()}
	if r := apiServerEgressRule(spec.APIServerCIDR); r != nil {
		egress = append(egress, *r)
	}
	egress = append(egress, datastorageEgressRule())

	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, ComponentGateway+"-netpol", ComponentGateway),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentGateway)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func dataStorageNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	protoTCP := corev1.ProtocolTCP
	p8080 := intstr.FromInt32(PortHTTP)

	dataIngressPeers := []networkingv1.NetworkPolicyPeer{
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentGateway)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAIAnalysis)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentSignalProcessing)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentRemediationOrchestrator)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentWorkflowExecution)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentNotification)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentEffectivenessMonitor)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAuthWebhook)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentKubernautAgent)}},
	}

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From: dataIngressPeers,
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p8080},
			},
		},
	}
	if m := metricsIngressRule(spec.MonitoringNamespace); m != nil {
		ingress = append(ingress, *m)
	}

	valkeyPort := kn.Spec.Valkey.Port
	if valkeyPort == 0 {
		valkeyPort = DefaultValkeyPort
	}
	pPG := intstr.FromInt32(PostgreSQLPort(kn))
	pValkey := intstr.FromInt32(valkeyPort)

	egress := []networkingv1.NetworkPolicyEgressRule{dnsEgressRule()}
	if r := apiServerEgressRule(spec.APIServerCIDR); r != nil {
		egress = append(egress, *r)
	}
	egress = append(egress, networkingv1.NetworkPolicyEgressRule{
		To: ipWorldPeers(),
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &pPG},
			{Protocol: &protoTCP, Port: &pValkey},
		},
	})

	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, ComponentDataStorage+"-netpol", ComponentDataStorage),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentDataStorage)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func aiAnalysisNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	return controllerWithDataStorageAndAgentEgress(kn, ComponentAIAnalysis, metricsOnlyIngress(kn))
}

func signalProcessingNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	return controllerWithDataStorageEgressOnly(kn, ComponentSignalProcessing, metricsOnlyIngress(kn))
}

func remediationOrchestratorNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	return controllerWithDataStorageEgressOnly(kn, ComponentRemediationOrchestrator, metricsOnlyIngress(kn))
}

func workflowExecutionNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	if kn.Spec.Ansible.Enabled {
		return controllerWithDataStorageAndHTTPSEgress(kn, ComponentWorkflowExecution, metricsOnlyIngress(kn))
	}
	return controllerWithDataStorageEgressOnly(kn, ComponentWorkflowExecution, metricsOnlyIngress(kn))
}

func notificationNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	protoTCP := corev1.ProtocolTCP
	p443 := intstr.FromInt32(443)

	ingress := metricsOnlyIngress(kn)
	egress := []networkingv1.NetworkPolicyEgressRule{dnsEgressRule()}
	if r := apiServerEgressRule(spec.APIServerCIDR); r != nil {
		egress = append(egress, *r)
	}
	egress = append(egress, datastorageEgressRule(), networkingv1.NetworkPolicyEgressRule{
		To: ipWorldPeers(),
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p443},
		},
	})

	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, ComponentNotification+"-netpol", ComponentNotification),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentNotification)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func effectivenessMonitorNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	protoTCP := corev1.ProtocolTCP
	p9090 := intstr.FromInt32(PortMetrics)
	p9093 := intstr.FromInt32(9093)

	ingress := metricsOnlyIngress(kn)
	egress := []networkingv1.NetworkPolicyEgressRule{dnsEgressRule()}
	if r := apiServerEgressRule(spec.APIServerCIDR); r != nil {
		egress = append(egress, *r)
	}
	egress = append(egress, datastorageEgressRule(), networkingv1.NetworkPolicyEgressRule{
		To: ipWorldPeers(),
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p9090},
			{Protocol: &protoTCP, Port: &p9093},
		},
	})

	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, ComponentEffectivenessMonitor+"-netpol", ComponentEffectivenessMonitor),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentEffectivenessMonitor)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func authWebhookNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	protoTCP := corev1.ProtocolTCP
	p9443 := intstr.FromInt32(PortWebhookServer)

	var ingress []networkingv1.NetworkPolicyIngressRule
	if spec.APIServerCIDR != "" {
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{
				{IPBlock: &networkingv1.IPBlock{CIDR: spec.APIServerCIDR}},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p9443},
			},
		})
	}

	egress := []networkingv1.NetworkPolicyEgressRule{dnsEgressRule()}
	if r := apiServerEgressRule(spec.APIServerCIDR); r != nil {
		egress = append(egress, *r)
	}
	egress = append(egress, datastorageEgressRule())

	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, ComponentAuthWebhook+"-netpol", ComponentAuthWebhook),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAuthWebhook)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func kubernautAgentNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	protoTCP := corev1.ProtocolTCP
	p8080 := intstr.FromInt32(PortHTTP)

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From: []networkingv1.NetworkPolicyPeer{
				{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAIAnalysis)}},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p8080},
			},
		},
	}
	if m := metricsIngressRule(spec.MonitoringNamespace); m != nil {
		ingress = append(ingress, *m)
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, ComponentKubernautAgent+"-netpol", ComponentKubernautAgent),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentKubernautAgent)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: ingress,
		},
	}
}

func metricsOnlyIngress(kn *kubernautv1alpha1.Kubernaut) []networkingv1.NetworkPolicyIngressRule {
	spec := kn.Spec.NetworkPolicies
	if m := metricsIngressRule(spec.MonitoringNamespace); m != nil {
		return []networkingv1.NetworkPolicyIngressRule{*m}
	}
	return nil
}

func controllerWithDataStorageEgressOnly(kn *kubernautv1alpha1.Kubernaut, component string, ingress []networkingv1.NetworkPolicyIngressRule) *networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	egress := []networkingv1.NetworkPolicyEgressRule{dnsEgressRule()}
	if r := apiServerEgressRule(spec.APIServerCIDR); r != nil {
		egress = append(egress, *r)
	}
	egress = append(egress, datastorageEgressRule())
	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, component+"-netpol", component),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(component)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func controllerWithDataStorageAndHTTPSEgress(kn *kubernautv1alpha1.Kubernaut, component string, ingress []networkingv1.NetworkPolicyIngressRule) *networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	protoTCP := corev1.ProtocolTCP
	p443 := intstr.FromInt32(443)

	egress := []networkingv1.NetworkPolicyEgressRule{dnsEgressRule()}
	if r := apiServerEgressRule(spec.APIServerCIDR); r != nil {
		egress = append(egress, *r)
	}
	egress = append(egress, datastorageEgressRule(), networkingv1.NetworkPolicyEgressRule{
		To: ipWorldPeers(),
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p443},
		},
	})
	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, component+"-netpol", component),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(component)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func controllerWithDataStorageAndAgentEgress(kn *kubernautv1alpha1.Kubernaut, component string, ingress []networkingv1.NetworkPolicyIngressRule) *networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	protoTCP := corev1.ProtocolTCP
	p8080 := intstr.FromInt32(PortHTTP)

	egress := []networkingv1.NetworkPolicyEgressRule{dnsEgressRule()}
	if r := apiServerEgressRule(spec.APIServerCIDR); r != nil {
		egress = append(egress, *r)
	}
	egress = append(egress, datastorageEgressRule(), networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentKubernautAgent)}},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p8080},
		},
	})
	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, component+"-netpol", component),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(component)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func namespaceNameSelector(ns string) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"kubernetes.io/metadata.name": ns,
		},
	}
}

func ipWorldPeers() []networkingv1.NetworkPolicyPeer {
	return []networkingv1.NetworkPolicyPeer{
		{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0"}},
		{IPBlock: &networkingv1.IPBlock{CIDR: "::/0"}},
	}
}

// dnsEgressRule allows DNS resolution via kube-system (UDP/TCP 53).
func dnsEgressRule() networkingv1.NetworkPolicyEgressRule {
	protoUDP := corev1.ProtocolUDP
	protoTCP := corev1.ProtocolTCP
	p53 := intstr.FromInt32(53)
	return networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{NamespaceSelector: namespaceNameSelector("kube-system")},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoUDP, Port: &p53},
			{Protocol: &protoTCP, Port: &p53},
		},
	}
}

// apiServerEgressRule allows HTTPS to the Kubernetes API by CIDR. Returns
// nil when cidr is empty.
func apiServerEgressRule(cidr string) *networkingv1.NetworkPolicyEgressRule {
	if cidr == "" {
		return nil
	}
	protoTCP := corev1.ProtocolTCP
	p443 := intstr.FromInt32(443)
	return &networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{IPBlock: &networkingv1.IPBlock{CIDR: cidr}},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p443},
		},
	}
}

// metricsIngressRule allows TCP 9090 scrape traffic from pods in the
// monitoring namespace. Returns nil when monitoringNS is empty.
func metricsIngressRule(monitoringNS string) *networkingv1.NetworkPolicyIngressRule {
	if monitoringNS == "" {
		return nil
	}
	protoTCP := corev1.ProtocolTCP
	p9090 := intstr.FromInt32(PortMetrics)
	return &networkingv1.NetworkPolicyIngressRule{
		From: []networkingv1.NetworkPolicyPeer{
			{NamespaceSelector: namespaceNameSelector(monitoringNS)},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p9090},
		},
	}
}

// datastorageEgressRule allows TCP 8080 to DataStorage pods in the same
// namespace as the NetworkPolicy.
func datastorageEgressRule() networkingv1.NetworkPolicyEgressRule {
	protoTCP := corev1.ProtocolTCP
	p8080 := intstr.FromInt32(PortHTTP)
	return networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentDataStorage)}},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p8080},
		},
	}
}

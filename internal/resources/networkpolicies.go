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
	"context"
	"os"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// NetworkPolicies returns NetworkPolicy resources matching the upstream
// kubernaut v1.4.0 traffic matrix. Returns nil when NetworkPolicies are
// disabled on the CR.
func NetworkPolicies(kn *kubernautv1alpha1.Kubernaut, sidecar KagentiSidecarMode) []*networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	if !spec.NetworkPoliciesEnabled() {
		return nil
	}
	nps := []*networkingv1.NetworkPolicy{
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
	if kn.Spec.GatewayEnabled() {
		nps = append(nps, gatewayNetworkPolicy(kn))
	}
	if kn.Spec.APIFrontendEnabled() {
		nps = append(nps, apifrontendNetworkPolicy(kn, sidecar))
	}
	return nps
}

func gatewayNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	protoTCP := corev1.ProtocolTCP
	p8443 := intstr.FromInt32(PortHTTPS)

	ingressFrom := []networkingv1.NetworkPolicyPeer{
		{NamespaceSelector: namespaceNameSelector(OCPIngressNamespace)},
	}
	if kn.Spec.Monitoring.MonitoringEnabled() {
		ingressFrom = append(ingressFrom, networkingv1.NetworkPolicyPeer{
			NamespaceSelector: namespaceNameSelector(OCPMonitoringNamespace),
		})
	}

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From: ingressFrom,
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p8443},
			},
		},
	}

	egress := baseEgress(1)
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
	protoTCP := corev1.ProtocolTCP
	p8443 := intstr.FromInt32(PortHTTPS)

	dataIngressPeers := []networkingv1.NetworkPolicyPeer{
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAIAnalysis)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentSignalProcessing)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentRemediationOrchestrator)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentWorkflowExecution)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentNotification)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentEffectivenessMonitor)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAuthWebhook)}},
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentKubernautAgent)}},
	}
	if kn.Spec.GatewayEnabled() {
		dataIngressPeers = append(dataIngressPeers,
			networkingv1.NetworkPolicyPeer{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentGateway)}},
		)
	}

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From: dataIngressPeers,
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p8443},
			},
		},
	}
	if kn.Spec.Monitoring.MonitoringEnabled() {
		ingress = append(ingress, *metricsIngressRule(OCPMonitoringNamespace))
	}

	valkeyPort := kn.Spec.Valkey.Port
	if valkeyPort == 0 {
		valkeyPort = DefaultValkeyPort
	}
	pPG := intstr.FromInt32(PostgreSQLPort(kn))
	pValkey := intstr.FromInt32(valkeyPort)

	egress := baseEgress(1)
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
	protoTCP := corev1.ProtocolTCP
	p443 := intstr.FromInt32(443)

	ingress := metricsOnlyIngress(kn)
	egress := baseEgress(2)
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
	protoTCP := corev1.ProtocolTCP
	p9090 := intstr.FromInt32(PortMetrics)
	p9093 := intstr.FromInt32(9093)

	ingress := metricsOnlyIngress(kn)
	egress := baseEgress(2)
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
	protoTCP := corev1.ProtocolTCP
	p9443 := intstr.FromInt32(PortWebhookServer)

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p9443},
			},
		},
	}

	egress := baseEgress(1)
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
	protoTCP := corev1.ProtocolTCP
	p8443 := intstr.FromInt32(PortHTTPS)

	kaIngressPeers := []networkingv1.NetworkPolicyPeer{
		{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAIAnalysis)}},
	}
	if kn.Spec.APIFrontendEnabled() {
		kaIngressPeers = append(kaIngressPeers,
			networkingv1.NetworkPolicyPeer{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAPIFrontend)}},
		)
	}

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From:  kaIngressPeers,
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protoTCP, Port: &p8443}},
		},
	}
	if kn.Spec.Monitoring.MonitoringEnabled() {
		ingress = append(ingress, *metricsIngressRule(OCPMonitoringNamespace))
	}

	egress := baseEgress(3)
	egress = append(egress, datastorageEgressRule())
	if kn.Spec.Monitoring.MonitoringEnabled() {
		egress = append(egress, monitoringStackEgressRule(OCPMonitoringNamespace))
	}
	if kn.Spec.KubernautAgent.LLM.Provider != "" {
		p443 := intstr.FromInt32(443)
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{
			To: ipWorldPeers(),
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p443},
			},
		})
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, ComponentKubernautAgent+"-netpol", ComponentKubernautAgent),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentKubernautAgent)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func metricsOnlyIngress(kn *kubernautv1alpha1.Kubernaut) []networkingv1.NetworkPolicyIngressRule {
	if kn.Spec.Monitoring.MonitoringEnabled() {
		return []networkingv1.NetworkPolicyIngressRule{*metricsIngressRule(OCPMonitoringNamespace)}
	}
	return nil
}

func controllerWithDataStorageEgressOnly(kn *kubernautv1alpha1.Kubernaut, component string, ingress []networkingv1.NetworkPolicyIngressRule) *networkingv1.NetworkPolicy {
	egress := baseEgress(1)
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
	protoTCP := corev1.ProtocolTCP
	p443 := intstr.FromInt32(443)

	egress := baseEgress(2)
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
	protoTCP := corev1.ProtocolTCP
	p8443 := intstr.FromInt32(PortHTTPS)

	egress := baseEgress(2)
	egress = append(egress, datastorageEgressRule(), networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentKubernautAgent)}},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p8443},
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

// baseEgress returns the standard DNS + API server egress rules with
// pre-allocated capacity for additional rules.
func baseEgress(extraCap int) []networkingv1.NetworkPolicyEgressRule {
	rules := make([]networkingv1.NetworkPolicyEgressRule, 2, 2+extraCap)
	rules[0] = dnsEgressRule()
	rules[1] = apiServerEgressRule()
	return rules
}

// dnsEgressRule allows DNS resolution via openshift-dns and kube-system
// (UDP/TCP 53). OCP runs CoreDNS in openshift-dns; vanilla K8s uses
// kube-system — both are included for portability.
func dnsEgressRule() networkingv1.NetworkPolicyEgressRule {
	protoUDP := corev1.ProtocolUDP
	protoTCP := corev1.ProtocolTCP
	p53 := intstr.FromInt32(53)
	return networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{NamespaceSelector: namespaceNameSelector(OCPDNSNamespace)},
			{NamespaceSelector: namespaceNameSelector("kube-system")},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoUDP, Port: &p53},
			{Protocol: &protoTCP, Port: &p53},
		},
	}
}

// resolveAPIServerIPs returns the real endpoint IPs of the kubernetes API
// server by querying the "kubernetes" endpoints in the default namespace.
// Falls back to KUBERNETES_SERVICE_HOST if endpoint lookup fails.
func resolveAPIServerIPs() []string {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		if host := os.Getenv("KUBERNETES_SERVICE_HOST"); host != "" {
			return []string{host}
		}
		return nil
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		if host := os.Getenv("KUBERNETES_SERVICE_HOST"); host != "" {
			return []string{host}
		}
		return nil
	}
	ep, err := cs.CoreV1().Endpoints("default").Get(context.Background(), "kubernetes", metav1.GetOptions{})
	if err != nil {
		if host := os.Getenv("KUBERNETES_SERVICE_HOST"); host != "" {
			return []string{host}
		}
		return nil
	}
	var ips []string
	for _, subset := range ep.Subsets {
		for _, addr := range subset.Addresses {
			ips = append(ips, addr.IP)
		}
	}
	if len(ips) == 0 {
		if host := os.Getenv("KUBERNETES_SERVICE_HOST"); host != "" {
			return []string{host}
		}
	}
	return ips
}

// apiServerEgressRule allows HTTPS to the Kubernetes API server. The
// endpoint IPs are resolved from the "kubernetes" endpoints in the
// default namespace (the real IPs, not the ClusterIP which is DNAT'd
// before NetworkPolicy evaluation on OVN-Kubernetes).
func apiServerEgressRule() networkingv1.NetworkPolicyEgressRule {
	protoTCP := corev1.ProtocolTCP
	p6443 := intstr.FromInt32(6443)
	p443 := intstr.FromInt32(443)
	rule := networkingv1.NetworkPolicyEgressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p6443},
			{Protocol: &protoTCP, Port: &p443},
		},
	}
	ips := resolveAPIServerIPs()
	if len(ips) > 0 {
		peers := make([]networkingv1.NetworkPolicyPeer, 0, len(ips))
		for _, ip := range ips {
			peers = append(peers, networkingv1.NetworkPolicyPeer{
				IPBlock: &networkingv1.IPBlock{CIDR: ip + "/32"},
			})
		}
		rule.To = peers
	}
	return rule
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

// monitoringStackEgressRule allows TCP 9091 (Thanos Querier) and TCP 9094
// (AlertManager) to pods in the monitoring namespace. The agent uses these
// for Prometheus metric queries and alert correlation.
func monitoringStackEgressRule(monitoringNS string) networkingv1.NetworkPolicyEgressRule {
	protoTCP := corev1.ProtocolTCP
	p9091 := intstr.FromInt32(9091)
	p9094 := intstr.FromInt32(9094)
	return networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{NamespaceSelector: namespaceNameSelector(monitoringNS)},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p9091},
			{Protocol: &protoTCP, Port: &p9094},
		},
	}
}

func apifrontendNetworkPolicy(kn *kubernautv1alpha1.Kubernaut, sidecar KagentiSidecarMode) *networkingv1.NetworkPolicy {
	protoTCP := corev1.ProtocolTCP
	p8443 := intstr.FromInt32(PortHTTPS)

	healthPort := PortHealthProbe
	metricsPort := PortMetrics
	if sidecar.ShiftsPorts() {
		healthPort = 8082
		metricsPort = 9092
	}
	if kn.Spec.APIFrontend.HealthPort != nil {
		healthPort = *kn.Spec.APIFrontend.HealthPort
	}
	if kn.Spec.APIFrontend.MetricsPort != nil {
		metricsPort = *kn.Spec.APIFrontend.MetricsPort
	}
	pHealth := intstr.FromInt32(healthPort)
	pMetrics := intstr.FromInt32(metricsPort)

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From: []networkingv1.NetworkPolicyPeer{
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					"kubernetes.io/metadata.name": kn.Namespace,
				}}},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p8443},
			},
		},
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &pHealth},
			},
		},
	}
	if kn.Spec.Monitoring.MonitoringEnabled() {
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{
				{NamespaceSelector: namespaceNameSelector(OCPMonitoringNamespace)},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &pMetrics},
			},
		})
	}

	if kn.Spec.APIFrontend.Route.AFRouteEnabled() {
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{
				{NamespaceSelector: namespaceNameSelector(OCPIngressNamespace)},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p8443},
			},
		})
	}

	egress := baseEgress(5)
	egress = append(egress, networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"app.kubernetes.io/part-of": "kubernaut",
			}}},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p8443},
		},
	})
	if kn.Spec.Monitoring.MonitoringEnabled() {
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{NamespaceSelector: namespaceNameSelector(OCPMonitoringNamespace)},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &pMetrics},
			},
		})
	}

	if kn.Spec.Valkey.SecretName != "" {
		valkeyPort := kn.Spec.Valkey.Port
		if valkeyPort == 0 {
			valkeyPort = DefaultValkeyPort
		}
		pValkey := intstr.FromInt32(valkeyPort)
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &pValkey},
			},
		})
	}

	if hasOIDCEgress(kn) {
		p443 := intstr.FromInt32(443)
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p443},
			},
		})
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, ComponentAPIFrontend+"-netpol", ComponentAPIFrontend),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAPIFrontend)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

// hasOIDCEgress returns true when AF needs outbound HTTPS for OIDC/JWKS
// discovery, checking both top-level issuerURL and per-provider jwtProviders.
func hasOIDCEgress(kn *kubernautv1alpha1.Kubernaut) bool {
	if kn.Spec.APIFrontend.Auth.IssuerURL != "" {
		return true
	}
	for _, p := range kn.Spec.APIFrontend.Auth.JWTProviders {
		if p.IssuerURL != "" {
			return true
		}
	}
	return false
}

// datastorageEgressRule allows TCP 8443 to DataStorage pods in the same
// namespace as the NetworkPolicy.
func datastorageEgressRule() networkingv1.NetworkPolicyEgressRule {
	protoTCP := corev1.ProtocolTCP
	p8443 := intstr.FromInt32(PortHTTPS)
	return networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentDataStorage)}},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p8443},
		},
	}
}

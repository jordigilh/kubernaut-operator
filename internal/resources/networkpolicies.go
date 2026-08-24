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
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var networkPoliciesLog = logf.Log.WithName("networkpolicies")

// NetworkPolicies returns NetworkPolicy resources matching the upstream
// kubernaut v1.4.0 traffic matrix. Returns nil when NetworkPolicies are
// disabled on the CR. knV2 supplies Fleet's egress gating (Fleet's entire
// CRD surface lives in v1alpha2, Fleet v1alpha2 migration).
func NetworkPolicies(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, sidecar KagentiSidecarMode) []*networkingv1.NetworkPolicy {
	spec := kn.Spec.NetworkPolicies
	if !spec.NetworkPoliciesEnabled() {
		return nil
	}
	nps := []*networkingv1.NetworkPolicy{
		dataStorageNetworkPolicy(kn),
		aiAnalysisNetworkPolicy(kn),
		signalProcessingNetworkPolicy(kn, knV2),
		remediationOrchestratorNetworkPolicy(kn, knV2),
		workflowExecutionNetworkPolicy(kn, knV2),
		notificationNetworkPolicy(kn),
		effectivenessMonitorNetworkPolicy(kn, knV2),
		authWebhookNetworkPolicy(kn),
		kubernautAgentNetworkPolicy(kn, knV2),
	}
	if kn.Spec.GatewayEnabled() {
		nps = append(nps, gatewayNetworkPolicy(kn, knV2))
	}
	if kn.Spec.APIFrontendEnabled() {
		nps = append(nps, apifrontendNetworkPolicy(kn, knV2, sidecar))
	}
	if knV2.Spec.FleetMetadataCacheEnabled() {
		nps = append(nps, fleetMetadataCacheNetworkPolicy(kn, knV2))
	}
	return nps
}

func gatewayNetworkPolicy(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) *networkingv1.NetworkPolicy {
	protoTCP := corev1.ProtocolTCP
	p8443 := intstr.FromInt32(PortHTTPS)

	ingressFrom := []networkingv1.NetworkPolicyPeer{
		{NamespaceSelector: namespaceNameSelector(OCPIngressNamespace)},
		{NamespaceSelector: namespaceNameSelector(OCPMonitoringNamespace)},
	}

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From: ingressFrom,
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p8443},
			},
		},
	}

	egress := baseEgress(2)
	egress = append(egress, datastorageEgressRule())
	if knV2.Spec.FleetEnabled() {
		egress = append(egress, fleetDestinationsEgressRule(knV2))
	}

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
	if kn.Spec.APIFrontendEnabled() {
		dataIngressPeers = append(dataIngressPeers,
			networkingv1.NetworkPolicyPeer{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAPIFrontend)}},
		)
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
		*metricsIngressRule(OCPMonitoringNamespace),
	}

	valkeyPort := kn.Spec.Valkey.Port
	if valkeyPort == 0 {
		valkeyPort = DefaultValkeyPort
	}
	pPG := intstr.FromInt32(PostgreSQLPort(kn))
	pValkey := intstr.FromInt32(valkeyPort)

	egress := baseEgress(1)
	egress = append(egress, networkingv1.NetworkPolicyEgressRule{
		To: sameNamespacePeers(),
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
	return controllerWithDataStorageAndAgentEgress(kn, ComponentAIAnalysis, metricsOnlyIngress())
}

func signalProcessingNetworkPolicy(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) *networkingv1.NetworkPolicy {
	np := controllerWithDataStorageEgressOnly(kn, ComponentSignalProcessing, metricsOnlyIngress())
	if knV2.Spec.FleetEnabled() {
		np.Spec.Egress = append(np.Spec.Egress, fleetDestinationsEgressRule(knV2))
	}
	return np
}

func remediationOrchestratorNetworkPolicy(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) *networkingv1.NetworkPolicy {
	np := controllerWithDataStorageEgressOnly(kn, ComponentRemediationOrchestrator, metricsOnlyIngress())
	if knV2.Spec.FleetEnabled() {
		np.Spec.Egress = append(np.Spec.Egress, fleetDestinationsEgressRule(knV2))
	}
	return np
}

func workflowExecutionNetworkPolicy(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) *networkingv1.NetworkPolicy {
	var np *networkingv1.NetworkPolicy
	if kn.Spec.Ansible.Enabled {
		np = controllerWithDataStorageAndHTTPSEgress(kn, ComponentWorkflowExecution, metricsOnlyIngress())
	} else {
		np = controllerWithDataStorageEgressOnly(kn, ComponentWorkflowExecution, metricsOnlyIngress())
	}
	// WorkflowExecution is fleet-config-aware (resolveWEFleetConfig/
	// weFleetYAML, #390) but was never granted egress to reach the MCP
	// Gateway it's configured to call -- a gap missed by the original
	// #224/#204 retrofit that added this rule to GW/RO/SP/AF/EM/KA. Fixed
	// alongside the fleetDestinationsEgressRule Route/VIP fix (#392) since
	// both surfaced from the same on-cluster triage.
	if knV2.Spec.FleetEnabled() {
		np.Spec.Egress = append(np.Spec.Egress, fleetDestinationsEgressRule(knV2))
	}
	return np
}

func notificationNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	protoTCP := corev1.ProtocolTCP
	p443 := intstr.FromInt32(443)

	ingress := metricsOnlyIngress()
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

func effectivenessMonitorNetworkPolicy(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) *networkingv1.NetworkPolicy {
	ingress := metricsOnlyIngress()
	egress := baseEgress(4)
	egress = append(egress, datastorageEgressRule())
	egress = append(egress, monitoringEgressRules(knV2, true)...)
	if knV2.Spec.FleetEnabled() {
		egress = append(egress, fleetDestinationsEgressRule(knV2))
	}

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

func kubernautAgentNetworkPolicy(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) *networkingv1.NetworkPolicy {
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
		*metricsIngressRule(OCPMonitoringNamespace),
	}

	egress := baseEgress(4)
	egress = append(egress, datastorageEgressRule())
	egress = append(egress, monitoringEgressRules(knV2, true)...)
	kaProfile, _ := ResolveLLMProfile(kn, EffectiveKALLMProfileRef(kn))
	if kaProfile.Provider != "" {
		p443 := intstr.FromInt32(443)
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{
			To: ipWorldPeers(),
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p443},
			},
		})
	}
	if knV2.Spec.FleetEnabled() {
		egress = append(egress, fleetDestinationsEgressRule(knV2))
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

func metricsOnlyIngress() []networkingv1.NetworkPolicyIngressRule {
	return []networkingv1.NetworkPolicyIngressRule{*metricsIngressRule(OCPMonitoringNamespace)}
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

// sameNamespacePeers allows traffic to any pod in the same namespace.
// This avoids the OVN-Kubernetes limitation where IPBlock rules do not
// match ClusterIP traffic (DNAT happens before NP evaluation).
func sameNamespacePeers() []networkingv1.NetworkPolicyPeer {
	return []networkingv1.NetworkPolicyPeer{
		{PodSelector: &metav1.LabelSelector{}},
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

// dnsEgressRule allows DNS resolution to any destination.
// Both the service port (53) and the backend target port (5353) must be
// allowed because OVN-Kubernetes applies egress NetworkPolicy ACLs with
// "apply-after-lb=true" — the match is evaluated AFTER load-balancer
// DNAT. On OpenShift, the dns-default service remaps 53 → 5353
// (coredns listens on 5353 to avoid running as root), so after DNAT the
// destination port is 5353, not 53.
func dnsEgressRule() networkingv1.NetworkPolicyEgressRule {
	protoUDP := corev1.ProtocolUDP
	protoTCP := corev1.ProtocolTCP
	p53 := intstr.FromInt32(53)
	p5353 := intstr.FromInt32(5353)
	return networkingv1.NetworkPolicyEgressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoUDP, Port: &p53},
			{Protocol: &protoTCP, Port: &p53},
			{Protocol: &protoUDP, Port: &p5353},
			{Protocol: &protoTCP, Port: &p5353},
		},
	}
}

// apiServerIPsCache holds the last successfully-resolved real API server
// endpoint IPs, guarded by mu. Read/written only by resolveAPIServerIPs.
var apiServerIPsCache struct {
	mu  sync.RWMutex
	ips []string
}

// liveResolveAPIServerIPsFunc is a seam over liveResolveAPIServerIPs so unit
// tests can simulate live-lookup success/failure without a real in-cluster
// config (internal/resources/*_test.go is the pure-Go unit tier -- no
// envtest/live cluster per AGENTS.md Testing Requirements).
var liveResolveAPIServerIPsFunc = liveResolveAPIServerIPs

// liveResolveAPIServerIPs queries the real endpoint IPs of the Kubernetes API
// server from the "kubernetes" Endpoints object in the default namespace.
//
// Deliberately uses context.Background() instead of a reconcile-scoped
// context: propagating ctx here would require adding a context parameter to
// all ~14 NetworkPolicy builder functions in this file, breaking the
// "builder takes only *Kubernaut" convention (AGENTS.md Resource Builder
// Patterns) for a call whose behavior would not otherwise change.
func liveResolveAPIServerIPs() ([]string, error) { //nolint:contextcheck
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes clientset: %w", err)
	}
	ep, err := cs.CoreV1().Endpoints("default").Get(context.Background(), "kubernetes", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get default/kubernetes endpoints: %w", err)
	}
	var ips []string
	for _, subset := range ep.Subsets {
		for _, addr := range subset.Addresses {
			ips = append(ips, addr.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("default/kubernetes endpoints has no ready addresses")
	}
	return ips, nil
}

// resolveAPIServerIPs returns the real endpoint IPs of the Kubernetes API
// server, re-resolving live on every call so NetworkPolicy egress stays
// correct across control-plane IP changes (e.g. HA failover).
//
// On a failed live lookup this fails closed, not open: it reuses the last
// successfully-resolved IPs rather than falling back to
// KUBERNETES_SERVICE_HOST, which is always the ClusterIP -- the one address
// this whole mechanism exists to avoid, since NetworkPolicy egress to it
// does not reliably match on OVN-Kubernetes (the ClusterIP is DNAT'd to the
// real endpoint before NetworkPolicy ACL evaluation). KUBERNETES_SERVICE_HOST
// is only used when no successful resolution has ever occurred yet (e.g. the
// very first reconcile), and that value is deliberately never cached, so
// every subsequent call keeps retrying the live lookup instead of getting
// stuck on it.
func resolveAPIServerIPs() []string {
	ips, err := liveResolveAPIServerIPsFunc()
	if err == nil {
		apiServerIPsCache.mu.Lock()
		apiServerIPsCache.ips = ips
		apiServerIPsCache.mu.Unlock()
		return ips
	}

	networkPoliciesLog.Error(err, "failed to resolve live Kubernetes API server endpoint IPs")

	apiServerIPsCache.mu.RLock()
	cached := apiServerIPsCache.ips
	apiServerIPsCache.mu.RUnlock()
	if len(cached) > 0 {
		networkPoliciesLog.Info("reusing last known-good API server endpoint IPs after a failed live lookup", "ips", cached)
		return cached
	}

	if host := os.Getenv("KUBERNETES_SERVICE_HOST"); host != "" {
		networkPoliciesLog.Info("no cached API server endpoint IPs yet; falling back to KUBERNETES_SERVICE_HOST (the ClusterIP, which NetworkPolicy egress may not match on OVN-Kubernetes)", "host", host)
		return []string{host}
	}
	return nil
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
//
//nolint:unparam // all current call sites pass OCPMonitoringNamespace, but the nil-on-empty guard keeps this generic for a future non-OCP monitoring namespace.
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

// prometheusEgressPorts is Thanos Querier's real endpoint port. OCP's
// thanos-querier Service happens to expose the same port number (9091) its
// kube-rbac-proxy-web sidecar container listens on, so no OVN
// apply-after-lb DNAT quirk applies here (unlike AlertManager, below).
var prometheusEgressPorts = []int32{9091}

// alertManagerEgressPorts allows both AlertManager's documented Service
// port (9094) and its real post-DNAT container port (9095). OCP's
// alertmanager-main Service exposes "web" as 9094, but that DNATs to the
// kube-rbac-proxy-web sidecar's actual container port 9095 (port 9094 on
// the container itself is Alertmanager's own gossip-mesh port -- an
// unrelated naming collision with the Service's port number).
// OVN-Kubernetes evaluates egress NetworkPolicy ACLs after load-balancer
// DNAT (apply-after-lb=true, see dnsEgressRule for the analogous DNS
// quirk), so a rule matching only 9094 never matches real AlertManager
// traffic -- confirmed via kube-rbac-proxy-web container port inspection
// and live traffic testing on OCP 4.18/OVN-Kubernetes (#298 spike).
var alertManagerEgressPorts = []int32{9094, 9095}

// monitoringEgressRules returns the egress rules needed to reach the
// resolved Prometheus and (when includeAlertManager) AlertManager
// endpoints, driven by spec.monitoring (#298). Each destination is
// resolved independently across three tiers (documented on
// MonitoringSpec):
//  1. URL unset: namespace-scoped to openshift-monitoring.
//  2. URL set, host is an in-cluster Service
//     (<service>.<namespace>.svc[.cluster.local]): namespace-scoped to the
//     parsed namespace.
//  3. URL set, host doesn't parse as an in-cluster Service (external DNS,
//     load balancer, etc.): the operator omits its own egress rule for
//     that destination entirely rather than guessing an overly broad (or
//     wrong) peer -- SC-7 boundary protection then relies on a
//     platform-operator-supplied supplemental NetworkPolicy.
//
// includeAlertManager is false for AF, which has no AlertManager client
// (afSeverityTriageConfig only ever sets PrometheusURL) -- granting AF
// that egress would be unused, over-provisioned access.
func monitoringEgressRules(knV2 *kubernautv1alpha2.Kubernaut, includeAlertManager bool) []networkingv1.NetworkPolicyEgressRule {
	var rules []networkingv1.NetworkPolicyEgressRule
	if rule, ok := monitoringDestinationEgressRule(knV2.Spec.Monitoring.Prometheus.URL, prometheusEgressPorts); ok {
		rules = append(rules, rule)
	}
	if includeAlertManager {
		if rule, ok := monitoringDestinationEgressRule(knV2.Spec.Monitoring.AlertManager.URL, alertManagerEgressPorts); ok {
			rules = append(rules, rule)
		}
	}
	return rules
}

// monitoringDestinationEgressRule builds the namespace-scoped egress rule
// for one monitoring destination, or ok=false when tier 3 (see
// monitoringEgressRules) applies and the rule must be omitted.
func monitoringDestinationEgressRule(rawURL string, ports []int32) (networkingv1.NetworkPolicyEgressRule, bool) {
	ns := OCPMonitoringNamespace
	if rawURL != "" {
		parsedNS, ok := inClusterServiceNamespace(rawURL)
		if !ok {
			return networkingv1.NetworkPolicyEgressRule{}, false
		}
		ns = parsedNS
	}
	protoTCP := corev1.ProtocolTCP
	npPorts := make([]networkingv1.NetworkPolicyPort, 0, len(ports))
	for _, p := range ports {
		port := intstr.FromInt32(p)
		npPorts = append(npPorts, networkingv1.NetworkPolicyPort{Protocol: &protoTCP, Port: &port})
	}
	return networkingv1.NetworkPolicyEgressRule{
		To:    []networkingv1.NetworkPolicyPeer{{NamespaceSelector: namespaceNameSelector(ns)}},
		Ports: npPorts,
	}, true
}

// inClusterServiceNamespace extracts the namespace from an in-cluster
// Kubernetes Service hostname of the form <service>.<namespace>.svc or
// <service>.<namespace>.svc.cluster.local. Returns ok=false for any other
// hostname shape (external DNS, bare IP, etc.) so callers can fall back to
// tier 3 rather than guess.
func inClusterServiceNamespace(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	labels := strings.Split(u.Hostname(), ".")
	if len(labels) >= 3 && labels[2] == "svc" {
		return labels[1], true
	}
	return "", false
}

func apifrontendNetworkPolicy(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, sidecar KagentiSidecarMode) *networkingv1.NetworkPolicy {
	healthPort, metricsPort := apifrontendHealthAndMetricsPorts(kn, sidecar)
	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, ComponentAPIFrontend+"-netpol", ComponentAPIFrontend),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentAPIFrontend)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: apifrontendIngressRules(kn, healthPort, metricsPort),
			Egress:  apifrontendEgressRules(kn, knV2),
		},
	}
}

// apifrontendHealthAndMetricsPorts resolves AF's health and metrics ports,
// applying the sidecar's shifted defaults and then any administrator
// override, shared by both the ingress and egress rule builders below.
func apifrontendHealthAndMetricsPorts(kn *kubernautv1alpha1.Kubernaut, sidecar KagentiSidecarMode) (healthPort, metricsPort int32) {
	healthPort = PortHealthProbe
	metricsPort = PortMetrics
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
	return healthPort, metricsPort
}

// apifrontendIngressRules builds AF's ingress rules: same-namespace HTTPS,
// cluster-wide health probes, monitoring-namespace metrics scraping (when
// enabled), and ingress-namespace HTTPS (when the OCP Route is enabled).
func apifrontendIngressRules(kn *kubernautv1alpha1.Kubernaut, healthPort, metricsPort int32) []networkingv1.NetworkPolicyIngressRule {
	protoTCP := corev1.ProtocolTCP
	p8443 := intstr.FromInt32(PortHTTPS)
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
		{
			From: []networkingv1.NetworkPolicyPeer{
				{NamespaceSelector: namespaceNameSelector(OCPMonitoringNamespace)},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &pMetrics},
			},
		},
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
	return ingress
}

// apifrontendEgressRules builds AF's egress rules: the shared base egress
// (DNS/API server), intra-kubernaut HTTPS, monitoring/Valkey/OIDC/Fleet
// destinations gated on their respective spec fields.
func apifrontendEgressRules(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []networkingv1.NetworkPolicyEgressRule {
	protoTCP := corev1.ProtocolTCP
	p8443 := intstr.FromInt32(PortHTTPS)

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
	// #1839 (upstream, PR #1841): AF's severityTriage config wires
	// PrometheusURL directly at effectivePrometheusURL() (Thanos Querier,
	// :9091 by default) for GetAlerts/GetRules/InstantQuery. This
	// previously granted only the metrics port (9090, the ingress/scrape
	// direction) here instead, so every severity-triage call to Prometheus
	// was silently dropped by this NetworkPolicy -- masked until upstream
	// removed the ungrounded LLM fallback that had absorbed the resulting
	// "no data" error identically to a genuinely alert-less resource.
	// Monitoring integration can no longer be disabled (#273), so this
	// egress is unconditional. AF has no AlertManager client, so
	// includeAlertManager=false keeps this rule minimal (#298, SC-7).
	egress = append(egress, monitoringEgressRules(knV2, false)...)

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

	if knV2.Spec.FleetEnabled() {
		egress = append(egress, fleetDestinationsEgressRule(knV2))
	}
	return egress
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

// fleetDestinationsCommonPort is the conventional MCP Gateway listener port
// used in upstream's own example
// (fleetmetadatacache.mcpGatewayEndpoint: "http://envoy-ai-gateway...:8080/mcp").
// spec.fleet.mcpGatewayEndpoint (and the OAuth2 token endpoint / ACM Search
// API) are free-form URLs, so this is a best-effort allow, not a parsed
// value.
const fleetDestinationsCommonPort int32 = 8080

// fleetDestinationsEgressRule allows all-namespace egress on 443 (HTTPS --
// covers the OAuth2 token endpoint, ACM Search API, and HTTPS-fronted MCP
// Gateways) plus fleetDestinationsCommonPort (the conventional MCP Gateway
// listener port). Extracted from FMC's original rule (#200) so
// GW/RO/SP/AF/EM can share it once spec.fleet.enabled=true (#224 Finding
// 6) -- none of these destinations live in a fixed, known namespace (the
// MCP Gateway, OAuth2 IdP, and ACM Search API are all external or
// cluster-wide), so, like FMC, an all-namespace peer is the correct scope,
// not a namespace- or pod-selector peer.
//
// The namespaceSelector peer alone is not sufficient: it only matches
// destinations backed by a namespaced Pod IP (in-cluster Services).
// mcpGatewayEndpoint/oauth2.tokenURL/endpoint are free-form and may instead
// point at an OpenShift Route, which resolves to the Ingress Router's
// hostNetwork VIP -- not a namespaced Pod IP -- so OVN-Kubernetes silently
// drops the traffic regardless of this rule (#392, confirmed via live
// on-cluster curl reproduction: identical request succeeds with the
// NetworkPolicy unapplied, times out once it applies). To cover that case
// too, this also resolves each configured destination hostname's real IPs
// via DNS and adds them as ipBlock peers alongside the namespaceSelector
// peer -- ipBlock peers match on the literal packet destination IP
// (correct for Route VIPs and other external/hostNetwork destinations),
// whereas an ipBlock peer for an in-cluster ClusterIP is a harmless no-op
// under OVN's DNAT-before-NetworkPolicy-evaluation behavior (see
// sameNamespacePeers), not a false grant -- so adding both peer kinds is
// always safe.
func fleetDestinationsEgressRule(knV2 *kubernautv1alpha2.Kubernaut) networkingv1.NetworkPolicyEgressRule {
	protoTCP := corev1.ProtocolTCP
	p443 := intstr.FromInt32(443)
	pMCPGateway := intstr.FromInt32(fleetDestinationsCommonPort)

	peers := []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{}}}
	for _, host := range fleetDestinationHostnames(knV2) {
		for _, ip := range resolveFleetHostIPs(host) {
			peers = append(peers, networkingv1.NetworkPolicyPeer{
				IPBlock: &networkingv1.IPBlock{CIDR: hostIPCIDR(ip)},
			})
		}
	}

	return networkingv1.NetworkPolicyEgressRule{
		To: peers,
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &p443},
			{Protocol: &protoTCP, Port: &pMCPGateway},
		},
	}
}

// fleetDestinationHostnames returns the deduplicated hostnames parsed from
// every configured free-form fleet destination URL
// (mcpGatewayEndpoint/oauth2.tokenURL/endpoint), so fleetDestinationsEgressRule
// can resolve their real IPs for its ipBlock peers. A URL that fails to
// parse or has no hostname is skipped rather than erroring -- best-effort,
// matching this rule's existing design (see fleetDestinationsCommonPort).
func fleetDestinationHostnames(knV2 *kubernautv1alpha2.Kubernaut) []string {
	seen := make(map[string]bool, 3)
	hosts := make([]string, 0, 3)
	for _, raw := range []string{
		knV2.Spec.Fleet.MCPGatewayEndpoint,
		knV2.Spec.Fleet.OAuth2.TokenURL,
		knV2.Spec.Fleet.Endpoint,
	} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			continue
		}
		host := u.Hostname()
		if !seen[host] {
			seen[host] = true
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// hostIPCIDR returns the /32 (IPv4) or /128 (IPv6) host CIDR for ip,
// matching ipWorldPeers' dual-stack handling elsewhere in this file.
// Malformed input (should not happen -- callers only pass addresses
// returned by net.DefaultResolver.LookupHost) falls back to /32.
func hostIPCIDR(ip string) string {
	if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() == nil {
		return ip + "/128"
	}
	return ip + "/32"
}

// fleetHostIPsCache holds the last successfully-resolved IPs per hostname
// for fleet destination hosts (MCP Gateway, OAuth2 token endpoint, fleet
// backend endpoint), guarded by mu. Read/written only by resolveFleetHostIPs.
var fleetHostIPsCache struct {
	mu  sync.RWMutex
	ips map[string][]string
}

// liveResolveFleetHostIPsFunc is a seam over liveResolveFleetHostIPs so unit
// tests can simulate DNS success/failure deterministically instead of
// making a real network call (internal/resources/*_test.go is the pure-Go
// unit tier -- no envtest/live cluster, no real DNS, per AGENTS.md Testing
// Requirements / Mock Strategy). suite_test.go's BeforeSuite overrides this
// to a fast, network-free failure by default for every test in this
// package; individual tests override further via
// stubLiveResolveFleetHostIPsForTest.
var liveResolveFleetHostIPsFunc = liveResolveFleetHostIPs

// liveResolveFleetHostIPs resolves host via DNS -- the same resolution path
// any HTTP client (including the fleet-aware components themselves) would
// use to reach it. Unlike resolveAPIServerIPs (which reads a known
// Kubernetes Endpoints object), fleet destination hosts are free-form and
// not necessarily backed by any Kubernetes API object, so DNS is the only
// generally-correct way to resolve their real IPs.
func liveResolveFleetHostIPs(host string) ([]string, error) { //nolint:contextcheck // see resolveAPIServerIPs' identical rationale: a context param here would require plumbing ctx through ~14 builder functions for a call whose behavior would not otherwise change.
	addrs, err := net.DefaultResolver.LookupHost(context.Background(), host)
	if err != nil {
		return nil, fmt.Errorf("resolve fleet destination host %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("fleet destination host %q resolved to no addresses", host)
	}
	return addrs, nil
}

// resolveFleetHostIPs returns the resolved IPs for host, re-resolving live
// on every call so egress stays correct as DNS/VIP assignment changes.
// Like resolveAPIServerIPs, a failed live lookup fails closed by reusing
// the last successfully-resolved IPs for that host rather than granting no
// egress at all. With no prior successful resolution, it logs and returns
// nil -- fleetDestinationsEgressRule's namespaceSelector peer still covers
// in-cluster-Service-backed destinations in that case, so this is reduced
// coverage, not a hard failure.
func resolveFleetHostIPs(host string) []string {
	ips, err := liveResolveFleetHostIPsFunc(host)
	if err == nil {
		fleetHostIPsCache.mu.Lock()
		if fleetHostIPsCache.ips == nil {
			fleetHostIPsCache.ips = make(map[string][]string)
		}
		fleetHostIPsCache.ips[host] = ips
		fleetHostIPsCache.mu.Unlock()
		return ips
	}

	networkPoliciesLog.Error(err, "failed to resolve fleet destination host IPs", "host", host)

	fleetHostIPsCache.mu.RLock()
	cached := fleetHostIPsCache.ips[host]
	fleetHostIPsCache.mu.RUnlock()
	if len(cached) > 0 {
		networkPoliciesLog.Info("reusing last known-good fleet destination IPs after a failed DNS lookup", "host", host, "ips", cached)
		return cached
	}
	return nil
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

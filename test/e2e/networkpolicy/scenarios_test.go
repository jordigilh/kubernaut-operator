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

package networkpolicy

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// Labels matching the PodSelector each real per-component NetworkPolicy
// uses (resources.SelectorLabels), so the client stub pods are actually
// governed by the same policy object a real EM/AF/KA pod would be.
var (
	emLabels = map[string]string{"app": resources.ComponentEffectivenessMonitor}
	afLabels = map[string]string{"app": resources.ComponentAPIFrontend}
	kaLabels = map[string]string{"app": resources.ComponentKubernautAgent}
)

const (
	prometheusStubName   = "thanos-querier"
	alertmanagerStubName = "alertmanager-main"
	prometheusStubHost   = prometheusStubName + "." + monitoringNamespace + ".svc.cluster.local"
	alertmanagerStubHost = alertmanagerStubName + "." + monitoringNamespace + ".svc.cluster.local"

	altPrometheusStubName = "prometheus-alt"
	altPrometheusStubHost = altPrometheusStubName + "." + altMonitoringNamespace + ".svc.cluster.local"

	thirdPartyNamespace = "third-party-monitoring"
	thirdPartyStubName  = "prometheus-thirdparty"
	// thirdPartyStubURL is external-shaped: it doesn't parse as an
	// in-cluster <service>.<namespace>.svc[.cluster.local] hostname, so
	// the operator treats it as tier 3 (see inClusterServiceNamespace).
	thirdPartyStubURL    = "https://prometheus.example.com:9091"
	thirdPartyStubTarget = thirdPartyStubName + "." + thirdPartyNamespace + ".svc.cluster.local"
)

var _ = Describe("NetworkPolicy enforcement (kind+Calico) -- #342 Phase 1", Ordered, func() {
	var ctx context.Context

	BeforeAll(func() {
		ctx = context.Background()

		for _, ns := range []string{systemNamespace, monitoringNamespace, altMonitoringNamespace, thirdPartyNamespace} {
			Expect(ensureNamespace(ctx, ns)).To(Succeed(), "creating namespace %q", ns)
		}

		// Prometheus/Thanos Querier: no Service port remap on real OCP
		// (see prometheusEgressPorts doc comment in networkpolicies.go).
		Expect(deployStub(ctx, monitoringNamespace, prometheusStubName,
			map[string]string{"app": prometheusStubName}, 9091, 9091)).To(Succeed())
		// AlertManager: Service port 9094 DNATs to container port 9095 on
		// real OCP (kube-rbac-proxy-web sidecar) -- reproduced here per the
		// #342 spike findings.
		Expect(deployStub(ctx, monitoringNamespace, alertmanagerStubName,
			map[string]string{"app": alertmanagerStubName}, 9094, 9095)).To(Succeed())
		// Stand-in for a platform-operator-supplied, non-default monitoring
		// stack (NPE2E-005 tier 2: in-cluster *.svc override).
		Expect(deployStub(ctx, altMonitoringNamespace, altPrometheusStubName,
			map[string]string{"app": altPrometheusStubName}, 9091, 9091)).To(Succeed())
		// Stand-in for an external/unparseable monitoring URL (NPE2E-005
		// tier 3): the operator's own egress rule is never granted for
		// this destination, so connectivity is proven purely by whether a
		// hand-added supplemental NetworkPolicy is present.
		Expect(deployStub(ctx, thirdPartyNamespace, thirdPartyStubName,
			map[string]string{"app": thirdPartyStubName}, 9091, 9091)).To(Succeed())

		Expect(deployClient(ctx, systemNamespace, "effectivenessmonitor", emLabels)).To(Succeed())
		Expect(deployClient(ctx, systemNamespace, "apifrontend", afLabels)).To(Succeed())
		Expect(deployClient(ctx, systemNamespace, "kubernaut-agent", kaLabels)).To(Succeed())
	})

	AfterEach(func() {
		// Every It applies/replaces the EM (and sometimes AF) NetworkPolicy
		// under test; leaving a stale one in place would leak into the
		// next spec's assertions.
		_ = deleteNetworkPolicy(ctx, systemNamespace, resources.ComponentEffectivenessMonitor+"-netpol")
		_ = deleteNetworkPolicy(ctx, systemNamespace, resources.ComponentAPIFrontend+"-netpol")
		_ = deleteNetworkPolicy(ctx, systemNamespace, "em-supplemental-thirdparty-egress")
	})

	It("NPE2E-001 [SC-7]: EM's egress to monitoring on the correct ports (9091, 9094) succeeds -- "+
		"counterpart to UT MON-003", func() {
		kn, knV2 := buildKubernautCR()
		np := renderNetworkPolicy(kn, knV2, resources.ComponentEffectivenessMonitor)
		Expect(np).NotTo(BeNil())
		Expect(applyNetworkPolicy(ctx, np)).To(Succeed())

		ok, err := probeTCP(ctx, emLabels, prometheusStubHost, 9091)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue(), "EM must reach Thanos Querier on port 9091")

		ok, err = probeTCP(ctx, emLabels, alertmanagerStubHost, 9094)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue(), "EM must reach AlertManager on its documented Service port 9094")
	})

	It("NPE2E-002 [SC-7]: egress on the old wrong ports (9090/9093) is denied -- "+
		"regression guard for bug #1, counterpart to UT MON-004", func() {
		kn, knV2 := buildKubernautCR()
		fixed := renderNetworkPolicy(kn, knV2, resources.ComponentEffectivenessMonitor)
		Expect(fixed).NotTo(BeNil())

		// RED: prove the harness actually detects bug #1 by reintroducing
		// it -- an unscoped (0.0.0.0/0) egress rule on the old wrong ports
		// only, with no rule at all for the correct port 9091. If this
		// harness can't fail against a known-bad policy, it isn't proving
		// anything.
		broken := fixed.DeepCopy()
		broken.Spec.Egress = onlyDNSAndAPIServerEgress(broken.Spec.Egress)
		broken.Spec.Egress = append(broken.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: protoPtr(corev1.ProtocolTCP), Port: portPtr(9090)},
				{Protocol: protoPtr(corev1.ProtocolTCP), Port: portPtr(9093)},
			},
		})
		Expect(applyNetworkPolicy(ctx, broken)).To(Succeed())
		ok, err := probeTCP(ctx, emLabels, prometheusStubHost, 9091)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse(),
			"harness sanity check: a policy granting only 9090/9093 must deny 9091, else the harness can't detect bug #1")

		// GREEN: the real, current policy does not grant 9090/9093 at all.
		Expect(applyNetworkPolicy(ctx, fixed)).To(Succeed())
		ok, err = probeTCP(ctx, emLabels, prometheusStubHost, 9090)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse(), "EM must not reach port 9090 (old wrong Prometheus port) under the current policy")

		ok, err = probeTCP(ctx, emLabels, alertmanagerStubHost, 9093)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse(), "EM must not reach port 9093 (old wrong AlertManager port) under the current policy")
	})

	It("NPE2E-003 [SC-7]: AF's egress to the AlertManager stub is denied -- "+
		"AF has no AlertManager client, counterpart to UT MON-001", func() {
		kn, knV2 := buildKubernautCR()
		np := renderNetworkPolicy(kn, knV2, resources.ComponentAPIFrontend)
		Expect(np).NotTo(BeNil())
		Expect(applyNetworkPolicy(ctx, np)).To(Succeed())

		ok, err := probeTCP(ctx, afLabels, prometheusStubHost, 9091)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue(), "AF must still reach Prometheus for severity triage (#1839)")

		ok, err = probeTCP(ctx, afLabels, alertmanagerStubHost, 9094)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse(), "AF must not reach AlertManager -- it has no AlertManager client")
	})

	It("NPE2E-004 [SC-7]: AlertManager Service port 9094 DNATs to 9095 and succeeds; "+
		"denied when only 9094 is allowed -- regression guard for bug #2, counterpart to UT MON-002", func() {
		kn, knV2 := buildKubernautCR()
		fixed := renderNetworkPolicy(kn, knV2, resources.ComponentEffectivenessMonitor)
		Expect(fixed).NotTo(BeNil())

		// RED: reproduce bug #2 -- a rule allowing only the documented
		// Service port 9094, missing the post-DNAT container port 9095
		// that OVN-Kubernetes (and, per the #342 spike, Calico) actually
		// evaluates traffic against.
		broken := fixed.DeepCopy()
		broken.Spec.Egress = replacePortsForRule(broken.Spec.Egress, 9095, []networkingv1.NetworkPolicyPort{
			{Protocol: protoPtr(corev1.ProtocolTCP), Port: portPtr(9094)},
		})
		Expect(applyNetworkPolicy(ctx, broken)).To(Succeed())
		ok, err := probeTCP(ctx, emLabels, alertmanagerStubHost, 9094)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse(),
			"harness sanity check: allowing only Service port 9094 (missing post-DNAT 9095) must deny AlertManager traffic")

		// GREEN: the real, current policy allows both 9094 and 9095.
		Expect(applyNetworkPolicy(ctx, fixed)).To(Succeed())
		ok, err = probeTCP(ctx, emLabels, alertmanagerStubHost, 9094)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue(), "EM must reach AlertManager's Service port 9094 through the DNAT to container port 9095")
	})

	Describe("NPE2E-005 [AC-4]: three-tier Monitoring.URL resolution -- "+
		"counterpart to UT MON-005/MON-005b/MON-006/MON-007", func() {
		It("tier 1: an unset URL reaches the stubbed openshift-monitoring default", func() {
			kn, knV2 := buildKubernautCR()
			np := renderNetworkPolicy(kn, knV2, resources.ComponentEffectivenessMonitor)
			Expect(applyNetworkPolicy(ctx, np)).To(Succeed())

			ok, err := probeTCP(ctx, emLabels, prometheusStubHost, 9091)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue(), "tier 1: EM must reach the default openshift-monitoring Prometheus stub")
		})

		It("tier 2: an in-cluster *.svc URL override reaches the overridden namespace's stub instead of the default", func() {
			kn, knV2 := buildKubernautCR()
			knV2.Spec.Monitoring.Prometheus.URL = "https://" + altPrometheusStubHost + ":9091"
			np := renderNetworkPolicy(kn, knV2, resources.ComponentEffectivenessMonitor)
			Expect(applyNetworkPolicy(ctx, np)).To(Succeed())

			ok, err := probeTCP(ctx, emLabels, altPrometheusStubHost, 9091)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue(), "tier 2: EM must reach the overridden custom-monitoring namespace's Prometheus stub")

			ok, err = probeTCP(ctx, emLabels, prometheusStubHost, 9091)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse(), "tier 2: EM must no longer reach the default stub once scoped to the override")
		})

		It("tier 3: an external/unparseable URL omits the operator's own rule -- "+
			"denied without a supplemental policy, allowed with one", func() {
			kn, knV2 := buildKubernautCR()
			knV2.Spec.Monitoring.Prometheus.URL = thirdPartyStubURL
			np := renderNetworkPolicy(kn, knV2, resources.ComponentEffectivenessMonitor)
			Expect(applyNetworkPolicy(ctx, np)).To(Succeed())

			ok, err := probeTCP(ctx, emLabels, thirdPartyStubTarget, 9091)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse(), "tier 3: the operator must not grant egress to an external/unparseable destination")

			// The platform operator supplies its own, additive
			// NetworkPolicy for this destination (documented on
			// MonitoringSpec) -- Kubernetes NetworkPolicy egress rules
			// union across every policy selecting the same pod, so this
			// is expected to compose with the operator's policy rather
			// than replace it.
			supplemental := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "em-supplemental-thirdparty-egress", Namespace: systemNamespace},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{MatchLabels: emLabels},
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
					Egress: []networkingv1.NetworkPolicyEgressRule{
						{
							To: []networkingv1.NetworkPolicyPeer{
								{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": thirdPartyNamespace,
								}}},
							},
							Ports: []networkingv1.NetworkPolicyPort{
								{Protocol: protoPtr(corev1.ProtocolTCP), Port: portPtr(9091)},
							},
						},
					},
				},
			}
			Expect(applyNetworkPolicy(ctx, supplemental)).To(Succeed())

			ok, err = probeTCP(ctx, emLabels, thirdPartyStubTarget, 9091)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue(),
				"tier 3: a platform-operator-supplied supplemental policy must additively grant the destination")
		})
	})
})

func protoPtr(p corev1.Protocol) *corev1.Protocol { return &p }
func portPtr(p int32) *intstr.IntOrString         { v := intstr.FromInt32(p); return &v }

// onlyDNSAndAPIServerEgress keeps just the first two egress rules (DNS,
// API server -- see baseEgress in networkpolicies.go), dropping every
// component-specific rule that follows. Used to build a minimal,
// deliberately-broken policy variant for RED-phase regression guards.
func onlyDNSAndAPIServerEgress(rules []networkingv1.NetworkPolicyEgressRule) []networkingv1.NetworkPolicyEgressRule {
	if len(rules) < 2 {
		return rules
	}
	return append([]networkingv1.NetworkPolicyEgressRule{}, rules[:2]...)
}

// replacePortsForRule finds the egress rule containing containsPort and
// replaces its Ports list with newPorts, leaving every other rule
// untouched. Used to reintroduce a known-fixed bug's pre-fix port set
// without hand-duplicating the rest of the real policy's shape.
func replacePortsForRule(
	rules []networkingv1.NetworkPolicyEgressRule,
	containsPort int32,
	newPorts []networkingv1.NetworkPolicyPort,
) []networkingv1.NetworkPolicyEgressRule {
	out := make([]networkingv1.NetworkPolicyEgressRule, len(rules))
	copy(out, rules)
	for i, rule := range out {
		for _, port := range rule.Ports {
			// Port numbers are always in [0, 65535]; IntValue() cannot
			// overflow int32 here.
			if port.Port != nil && int32(port.Port.IntValue()) == containsPort { //nolint:gosec
				out[i].Ports = newPorts
				return out
			}
		}
	}
	return out
}

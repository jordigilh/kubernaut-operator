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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	kubernetnetworking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	instanceLabel        = "app.kubernetes.io/instance"
	componentLabel       = "app.kubernetes.io/component"
	authWebhookComponent = "authwebhook"
	consoleComponent     = "console"
)

// RenderedPolicy is a standard Kubernetes NetworkPolicy plus its namespaced
// ownership scope.
type RenderedPolicy struct {
	Object     *kubernetnetworking.NetworkPolicy
	Namespaced bool
}

// Render translates portable intent into one standard NetworkPolicy per active
// component. Egress is intentionally absent: API-server and external
// dependency reachability is owned by cluster administrators/SREs, and an
// operator-generated egress policy could silently break Kubernetes clients.
func Render(intent Intent) ([]RenderedPolicy, error) {
	if intent.Namespace == "" {
		return nil, fmt.Errorf("policy namespace is required")
	}
	if intent.InstanceName == "" {
		intent.InstanceName = intent.Namespace
	}
	if err := validateScrapeSources(intent.ScrapeSources); err != nil {
		return nil, err
	}
	for component, port := range intent.MetricsPorts {
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid metrics port for component %s", component)
		}
	}
	objects := make([]RenderedPolicy, 0, len(intent.Components))
	for _, component := range intent.Components {
		// AuthWebhook serves Kubernetes admission webhooks and Console serves
		// browser/Route traffic. Neither caller has a portable namespace/pod
		// identity; administrators own both external ingress boundaries.
		if component == authWebhookComponent || component == consoleComponent {
			continue
		}
		policy := &kubernetnetworking.NetworkPolicy{
			TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("kubernaut-%s", component),
				Namespace: intent.Namespace,
				Labels: map[string]string{
					ManagedPolicyLabel:   "true",
					ManagedByLabel:       "kubernaut-operator",
					PolicyNamespaceLabel: intent.Namespace,
					instanceLabel:        intent.InstanceName,
					componentLabel:       component,
				},
			},
			Spec: kubernetnetworking.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: managedPodLabels(intent.InstanceName, component)},
				PolicyTypes: []kubernetnetworking.PolicyType{kubernetnetworking.PolicyTypeIngress},
			},
		}

		policy.Spec.Ingress = append(policy.Spec.Ingress, kubernetnetworking.NetworkPolicyIngressRule{
			From: []kubernetnetworking.NetworkPolicyPeer{{
				PodSelector: &metav1.LabelSelector{MatchLabels: managedInstanceLabels(intent.InstanceName)},
			}},
		})
		if port := intent.MetricsPorts[component]; port != 0 {
			for _, source := range intent.ScrapeSources {
				policy.Spec.Ingress = append(policy.Spec.Ingress, kubernetnetworking.NetworkPolicyIngressRule{
					From: []kubernetnetworking.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": source.Namespace,
						}},
						PodSelector: &metav1.LabelSelector{MatchLabels: source.PodLabels},
					}},
					Ports: []kubernetnetworking.NetworkPolicyPort{{
						Port:     ptr.To(intstr.FromInt32(port)),
						Protocol: ptr.To(corev1.ProtocolTCP),
					}},
				})
			}
		}
		policy.Spec.Ingress = append(policy.Spec.Ingress, ingressOverrideRules(intent.IngressOverrides[component])...)
		objects = append(objects, RenderedPolicy{Object: policy, Namespaced: true})
	}
	return objects, nil
}

func managedInstanceLabels(instance string) map[string]string {
	return map[string]string{
		ManagedByLabel: "kubernaut-operator",
		instanceLabel:  instance,
	}
}

func managedPodLabels(instance, component string) map[string]string {
	labels := managedInstanceLabels(instance)
	labels["app"] = component
	return labels
}

func ingressOverrideRules(override kubernautv1alpha2.NetworkPolicyNamedIngressOverride) []kubernetnetworking.NetworkPolicyIngressRule {
	rules := make([]kubernetnetworking.NetworkPolicyIngressRule, 0,
		len(override.IngressNamespaces)+len(override.IngressNamespaceSelectors)+len(override.IngressCIDRs))
	for _, namespace := range override.IngressNamespaces {
		rules = append(rules, kubernetnetworking.NetworkPolicyIngressRule{From: []kubernetnetworking.NetworkPolicyPeer{{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}},
		}}})
	}
	selectors := append([]metav1.LabelSelector(nil), override.IngressNamespaceSelectors...)
	for _, selector := range selectors {
		rules = append(rules, kubernetnetworking.NetworkPolicyIngressRule{From: []kubernetnetworking.NetworkPolicyPeer{{
			NamespaceSelector: selector.DeepCopy(),
		}}})
	}
	for _, cidr := range override.IngressCIDRs {
		rules = append(rules, kubernetnetworking.NetworkPolicyIngressRule{From: []kubernetnetworking.NetworkPolicyPeer{{
			IPBlock: &kubernetnetworking.IPBlock{CIDR: cidr},
		}}})
	}
	if len(rules) == 0 {
		return nil
	}
	return rules
}

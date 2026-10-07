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
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	instanceLabel  = "app.kubernetes.io/instance"
	componentLabel = "app.kubernetes.io/component"
)

// RenderedPolicy is a provider-native object plus its ownership scope.
type RenderedPolicy struct {
	Object     *unstructured.Unstructured
	Namespaced bool
}

// Render translates common intent into exactly one provider-native policy
// family. No Kubernetes NetworkPolicy is returned, and an unavailable
// provider intentionally renders nothing.
func Render(result DetectionResult, intent Intent) ([]RenderedPolicy, error) {
	if len(intent.StaticAPIServerCIDRs) > 0 {
		return nil, fmt.Errorf("static API-server CIDRs are not supported by native policy adapters")
	}
	if !result.Ready {
		return nil, nil
	}
	if err := ValidateMonitoringDestinations(result.Provider, intent.Monitoring); err != nil {
		return nil, err
	}
	switch result.Provider {
	case ProviderCilium:
		return renderCilium(intent), nil
	case ProviderCalico:
		return renderCalico(intent), nil
	case ProviderOVN:
		return renderOVN(intent), nil
	default:
		return nil, nil
	}
}

func renderCilium(intent Intent) []RenderedPolicy {
	objects := make([]RenderedPolicy, 0, len(intent.Components))
	instance := intent.InstanceName
	if instance == "" {
		instance = intent.Namespace
	}
	dnsNamespace := effectiveDNSNamespace(intent)
	for _, component := range intent.Components {
		selector := endpointSelector(intent.Namespace, instance, component)
		managedSelector := map[string]interface{}{
			"matchLabels": map[string]interface{}{
				ciliumKubernetesLabel(ManagedByLabel): "kubernaut-operator",
				ciliumKubernetesLabel(instanceLabel):  instance,
				"k8s:io.kubernetes.pod.namespace":     intent.Namespace,
			},
		}
		egress := []interface{}{
			map[string]interface{}{
				"toEndpoints": []interface{}{managedSelector},
			},
			map[string]interface{}{
				"toEntities": []interface{}{"kube-apiserver"},
			},
			map[string]interface{}{
				"toEndpoints": []interface{}{
					map[string]interface{}{
						"matchLabels": map[string]interface{}{
							"k8s:k8s-app": "kube-dns",
						},
						"matchExpressions": []interface{}{
							map[string]interface{}{
								"key":      "k8s:io.kubernetes.pod.namespace",
								"operator": "In",
								"values":   []interface{}{dnsNamespace},
							},
						},
					},
				},
			},
		}
		if component == MonitoringAgentComponent {
			for _, destination := range intent.Monitoring {
				egress = append(egress, ciliumMonitoringRule(destination))
			}
		}
		spec := map[string]interface{}{
			"endpointSelector": selector,
			"enableDefaultDeny": map[string]interface{}{
				"ingress": true,
				"egress":  true,
			},
			"ingress": []interface{}{
				map[string]interface{}{
					"fromEndpoints": []interface{}{managedSelector},
				},
			},
			"egress": egress,
		}
		objects = append(objects, RenderedPolicy{
			Object: newPolicyObject(
				"cilium.io/v2", "CiliumNetworkPolicy",
				fmt.Sprintf("kubernaut-%s", component), intent.Namespace,
				policyLabelsForIntent(ProviderCilium, component, intent), spec,
			),
			Namespaced: true,
		})
	}
	return objects
}

func renderCalico(intent Intent) []RenderedPolicy {
	objects := make([]RenderedPolicy, 0, len(intent.Components))
	instance := intent.InstanceName
	if instance == "" {
		instance = intent.Namespace
	}
	dnsNamespace := effectiveDNSNamespace(intent)
	for _, component := range intent.Components {
		selector := fmt.Sprintf("app == '%s' && %s == '%s' && %s == 'kubernaut-operator'", component, quoteSelectorKey(instanceLabel), instance, quoteSelectorKey(ManagedByLabel))
		managedSelector := fmt.Sprintf("%s == 'kubernaut-operator' && %s == '%s'", quoteSelectorKey(ManagedByLabel), quoteSelectorKey(instanceLabel), instance)
		egress := []interface{}{
			map[string]interface{}{
				"action":      "Allow",
				"destination": map[string]interface{}{"selector": managedSelector},
			},
			map[string]interface{}{
				"action": "Allow",
				"destination": map[string]interface{}{
					"services": map[string]interface{}{
						"name":      intent.APIServerIdentity.ServiceName,
						"namespace": intent.APIServerIdentity.Namespace,
					},
				},
			},
			map[string]interface{}{
				"action": "Allow",
				"destination": map[string]interface{}{
					"namespaceSelector": fmt.Sprintf("projectcalico.org/name == '%s'", dnsNamespace),
					"selector":          "k8s-app == 'kube-dns'",
				},
			},
		}
		if component == MonitoringAgentComponent {
			for _, destination := range intent.Monitoring {
				egress = append(egress, calicoMonitoringRule(destination))
			}
		}
		spec := map[string]interface{}{
			"selector": selector,
			"order":    float64(100),
			"types":    []interface{}{"Ingress", "Egress"},
			"ingress": []interface{}{
				map[string]interface{}{
					"action": "Allow",
					"source": map[string]interface{}{"selector": managedSelector},
				},
			},
			"egress": egress,
		}
		objects = append(objects, RenderedPolicy{
			Object: newPolicyObject(
				"projectcalico.org/v3", "NetworkPolicy",
				fmt.Sprintf("kubernaut-%s", component), intent.Namespace,
				policyLabelsForIntent(ProviderCalico, component, intent), spec,
			),
			Namespaced: true,
		})
	}
	return objects
}

func ciliumMonitoringRule(destination MonitoringDestination) map[string]interface{} {
	return map[string]interface{}{
		"toServices": []interface{}{map[string]interface{}{
			"k8sService": map[string]interface{}{
				"serviceName": destination.ServiceName,
				"namespace":   destination.Namespace,
			},
		}},
		"toPorts": []interface{}{map[string]interface{}{
			"ports": []interface{}{map[string]interface{}{
				"port":     fmt.Sprintf("%d", destination.BackendPort),
				"protocol": "TCP",
			}},
		}},
	}
}

func calicoMonitoringRule(destination MonitoringDestination) map[string]interface{} {
	return map[string]interface{}{
		"action": "Allow",
		"destination": map[string]interface{}{
			"services": map[string]interface{}{
				"name":      destination.ServiceName,
				"namespace": destination.Namespace,
			},
		},
	}
}

func renderOVN(intent Intent) []RenderedPolicy {
	instance := intent.InstanceName
	if instance == "" {
		instance = intent.Namespace
	}
	dnsNamespace := effectiveDNSNamespace(intent)
	subject := ovnPodsPeer(intent.Namespace, instance)
	allowIngress := map[string]interface{}{
		"name":   "allow-kubernaut-namespace",
		"action": "Allow",
		"from":   []interface{}{ovnPodsPeer(intent.Namespace, instance)},
	}
	allowEgress := ovnEgressRules(intent.Namespace, instance, dnsNamespace)

	anpSpec := map[string]interface{}{
		// AdminNetworkPolicy priorities are 0-99; lower values have higher
		// precedence. Keep this policy below platform-owned priorities while
		// remaining valid for the v1alpha1 API.
		"priority": float64(90),
		"subject":  subject,
		"ingress":  []interface{}{allowIngress},
		"egress":   allowEgress,
	}
	banpSpec := map[string]interface{}{
		"subject": subject,
		"ingress": []interface{}{allowIngress},
		"egress":  allowEgress,
	}
	labels := policyLabelsForIntent(ProviderOVN, "namespace", intent)
	objects := []RenderedPolicy{
		{
			Object: newPolicyObject(
				"policy.networking.k8s.io/v1alpha1", "AdminNetworkPolicy",
				"kubernaut-admin", "", labels, anpSpec,
			),
			Namespaced: false,
		},
		{
			Object: newPolicyObject(
				"policy.networking.k8s.io/v1alpha1", "BaselineAdminNetworkPolicy",
				"default", "", labels, banpSpec,
			),
			Namespaced: false,
		},
	}
	if hasPolicyComponent(intent.Components, MonitoringAgentComponent) && len(intent.Monitoring) > 0 {
		monitoringEgress := append(ovnEgressRules(intent.Namespace, instance, dnsNamespace), ovnMonitoringEgressRules(intent.Monitoring)...)
		monitoringSpec := map[string]interface{}{
			// Priority 89 is higher precedence than the common namespace policy
			// at 90, while remaining below platform-owned priorities.
			"priority": float64(89),
			"subject":  ovnAgentPodsPeer(intent.Namespace, instance),
			"egress":   monitoringEgress,
		}
		objects = append(objects, RenderedPolicy{
			Object: newPolicyObject(
				"policy.networking.k8s.io/v1alpha1", "AdminNetworkPolicy",
				"kubernaut-agent-monitoring", "",
				policyLabelsForIntent(ProviderOVN, MonitoringAgentComponent, intent), monitoringSpec,
			),
			Namespaced: false,
		})
	}
	return objects
}

func ovnPodsPeer(namespace, instance string) map[string]interface{} {
	return ovnPodsPeerWithLabels(namespace, map[string]string{
		ManagedByLabel: "kubernaut-operator",
		instanceLabel:  instance,
	})
}

func ovnAgentPodsPeer(namespace, instance string) map[string]interface{} {
	return ovnPodsPeerWithLabels(namespace, map[string]string{
		ManagedByLabel: "kubernaut-operator",
		instanceLabel:  instance,
		"app":          MonitoringAgentComponent,
	})
}

func ovnServicePodsPeer(destination MonitoringDestination) map[string]interface{} {
	return ovnPodsPeerWithLabels(destination.Namespace, destination.ServiceSelector)
}

func ovnPodsPeerWithLabels(namespace string, labels map[string]string) map[string]interface{} {
	return map[string]interface{}{
		"pods": map[string]interface{}{
			"namespaceSelector": map[string]interface{}{
				"matchLabels": map[string]interface{}{
					"kubernetes.io/metadata.name": namespace,
				},
			},
			"podSelector": map[string]interface{}{
				"matchLabels": stringMapToInterface(labels),
			},
		},
	}
}

func ovnNamespacePeer(namespace string) map[string]interface{} {
	return map[string]interface{}{
		"namespaces": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"kubernetes.io/metadata.name": namespace,
			},
		},
	}
}

func ovnMonitoringEgressRules(destinations []MonitoringDestination) []interface{} {
	ordered := append([]MonitoringDestination(nil), destinations...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	rules := make([]interface{}, 0, len(ordered)+1)
	namespaces := make(map[string]struct{}, len(ordered))
	for _, destination := range ordered {
		rules = append(rules, ovnMonitoringRule(destination))
		namespaces[destination.Namespace] = struct{}{}
	}
	orderedNamespaces := make([]string, 0, len(namespaces))
	for namespace := range namespaces {
		orderedNamespaces = append(orderedNamespaces, namespace)
	}
	sort.Strings(orderedNamespaces)
	denyPeers := make([]interface{}, 0, len(orderedNamespaces))
	for _, namespace := range orderedNamespaces {
		denyPeers = append(denyPeers, ovnNamespacePeer(namespace))
	}
	if len(denyPeers) > 0 {
		rules = append(rules, map[string]interface{}{
			"name":   "deny-other-monitoring",
			"action": "Deny",
			"to":     denyPeers,
		})
	}
	return rules
}

func ovnMonitoringRule(destination MonitoringDestination) map[string]interface{} {
	return map[string]interface{}{
		"name":   fmt.Sprintf("allow-monitoring-%s", destination.Name),
		"action": "Allow",
		"to":     []interface{}{ovnServicePodsPeer(destination)},
		"ports":  ovnTCPPortRules(int(destination.BackendPort)),
	}
}

func hasPolicyComponent(components []string, wanted string) bool {
	for _, component := range components {
		if component == wanted {
			return true
		}
	}
	return false
}

func ovnEgressRules(namespace, instance, dnsNamespace string) []interface{} {
	return []interface{}{
		map[string]interface{}{
			"name":   "allow-kubernetes-api",
			"action": "Allow",
			"to": []interface{}{
				ovnNodePeer("node-role.kubernetes.io/control-plane"),
				ovnNodePeer("node-role.kubernetes.io/master"),
			},
			"ports": ovnTCPPortRules(6443),
		},
		map[string]interface{}{
			"name":   "allow-kubernaut-namespace",
			"action": "Allow",
			"to":     []interface{}{ovnPodsPeer(namespace, instance)},
		},
		map[string]interface{}{
			"name":   "allow-dns",
			"action": "Allow",
			"to": []interface{}{map[string]interface{}{
				"pods": map[string]interface{}{
					"namespaceSelector": map[string]interface{}{
						"matchLabels": map[string]interface{}{
							"kubernetes.io/metadata.name": dnsNamespace,
						},
					},
					"podSelector": map[string]interface{}{
						"matchLabels": map[string]interface{}{"app": "dns"},
					},
				},
			}},
			"ports": ovnPortRules(53, 5353),
		},
	}
}

func effectiveDNSNamespace(intent Intent) string {
	if intent.DNSNamespace != "" {
		return intent.DNSNamespace
	}
	return "kube-system"
}

func ovnNodePeer(label string) map[string]interface{} {
	return map[string]interface{}{
		"nodes": map[string]interface{}{
			"matchExpressions": []interface{}{
				map[string]interface{}{
					"key":      label,
					"operator": "Exists",
				},
			},
		},
	}
}

func ovnPortRules(ports ...int) []interface{} {
	return ovnPortRulesForProtocols([]string{"TCP", "UDP"}, ports...)
}

func ovnTCPPortRules(ports ...int) []interface{} {
	return ovnPortRulesForProtocols([]string{"TCP"}, ports...)
}

func ovnPortRulesForProtocols(protocols []string, ports ...int) []interface{} {
	rules := make([]interface{}, 0, len(ports)*len(protocols))
	for _, port := range ports {
		for _, protocol := range protocols {
			rules = append(rules, map[string]interface{}{
				"portNumber": map[string]interface{}{"port": float64(port), "protocol": protocol},
			})
		}
	}
	return rules
}

func endpointSelector(namespace, instance, component string) map[string]interface{} {
	if instance == "" {
		instance = namespace
	}
	return map[string]interface{}{
		"matchLabels": map[string]interface{}{
			ciliumKubernetesLabel(ManagedByLabel): "kubernaut-operator",
			ciliumKubernetesLabel(instanceLabel):  instance,
			ciliumKubernetesLabel(componentLabel): component,
		},
	}
}

func ciliumKubernetesLabel(key string) string {
	return "k8s:" + key
}

func policyLabels(provider Provider, component string) map[string]string {
	return map[string]string{
		ManagedPolicyLabel: "true",
		ProviderLabel:      string(provider),
		ManagedByLabel:     "kubernaut-operator",
		componentLabel:     component,
	}
}

func policyLabelsForIntent(provider Provider, component string, intent Intent) map[string]string {
	labels := policyLabels(provider, component)
	instance := intent.InstanceName
	if instance == "" {
		instance = intent.Namespace
	}
	labels[instanceLabel] = instance
	labels[PolicyNamespaceLabel] = intent.Namespace
	return labels
}

func newPolicyObject(apiVersion, kind, name, namespace string, labels map[string]string, spec map[string]interface{}) *unstructured.Unstructured {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name":   name,
			"labels": stringMapToInterface(labels),
		},
		"spec": spec,
	}}
	if namespace != "" {
		object.SetNamespace(namespace)
	}
	return object
}

func stringMapToInterface(input map[string]string) map[string]interface{} {
	result := make(map[string]interface{}, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func quoteSelectorKey(key string) string {
	return key
}

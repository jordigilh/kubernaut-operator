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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const policyTestInstance = "kubernaut"

var _ = Describe("managed policy provider ownership", func() {
	It("maps each managed provider GVK to its ownership label", func() {
		Expect(ProviderForGVK(schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"})).To(Equal(ProviderCilium))
		Expect(ProviderForGVK(schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"})).To(Equal(ProviderCalico))
		Expect(ProviderForGVK(schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "AdminNetworkPolicy"})).To(Equal(ProviderOVN))
		Expect(ProviderForGVK(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Policy"})).To(Equal(ProviderNone))
	})
})

func supportedSnapshot(provider Provider, version string) DiscoverySnapshot {
	return DiscoverySnapshot{Candidates: map[Provider]ProviderSnapshot{
		provider: {
			Provider:                   provider,
			APIAvailable:               true,
			Active:                     true,
			Version:                    version,
			SchemaValid:                true,
			APIServerIdentityAvailable: true,
			Evidence:                   []string{"provider-daemonset-ready"},
			RequiredGVKs:               RequiredGVKs(provider),
		},
	}}
}

var _ = Describe("provider detection", func() {
	It("continues without native policy when no supported provider is available", func() {
		result := Detect(DiscoverySnapshot{}, ProviderAuto)

		Expect(result.Provider).To(Equal(ProviderNone))
		Expect(result.Reason).To(Equal(ReasonNoSupportedProvider))
		Expect(result.Ready).To(BeFalse())
	})

	It("rejects ambiguous auto-detection", func() {
		snapshot := supportedSnapshot(ProviderCilium, "1.19.4")
		snapshot.Candidates[ProviderCalico] = ProviderSnapshot{
			Provider:                   ProviderCalico,
			APIAvailable:               true,
			Active:                     true,
			Version:                    "3.31.1",
			SchemaValid:                true,
			APIServerIdentityAvailable: true,
			Evidence:                   []string{"calico-node-ready"},
			RequiredGVKs:               RequiredGVKs(ProviderCalico),
		}

		result := Detect(snapshot, ProviderAuto)

		Expect(result.Provider).To(Equal(ProviderNone))
		Expect(result.Reason).To(Equal(ReasonAmbiguousProvider))
		Expect(result.Candidates).To(ConsistOf(ProviderCilium, ProviderCalico))
	})

	It("honors an explicit provider while retaining compatibility checks", func() {
		snapshot := supportedSnapshot(ProviderCilium, "1.20.2")
		snapshot.Candidates[ProviderCalico] = ProviderSnapshot{
			Provider:                   ProviderCalico,
			APIAvailable:               true,
			Active:                     true,
			Version:                    "3.31.1",
			SchemaValid:                true,
			APIServerIdentityAvailable: true,
			Evidence:                   []string{"calico-node-ready"},
			RequiredGVKs:               RequiredGVKs(ProviderCalico),
		}

		result := Detect(snapshot, ProviderCilium)

		Expect(result.Provider).To(Equal(ProviderCilium))
		Expect(result.Ready).To(BeTrue())
		Expect(result.Reason).To(Equal(ReasonProviderReady))
	})

	It("fails closed for an unsupported release or incomplete schema", func() {
		outOfRange := supportedSnapshot(ProviderCilium, "1.18.9")
		result := Detect(outOfRange, ProviderAuto)
		Expect(result.Provider).To(Equal(ProviderNone))
		Expect(result.Reason).To(Equal(ReasonUnsupportedVersion))

		invalidSchema := supportedSnapshot(ProviderCalico, "3.32.0")
		invalidSchema.Candidates[ProviderCalico] = ProviderSnapshot{
			Provider:     ProviderCalico,
			APIAvailable: true,
			Active:       true,
			Version:      "3.32.0",
			SchemaValid:  false,
			Evidence:     []string{"calico-node-ready"},
			RequiredGVKs: RequiredGVKs(ProviderCalico),
		}
		result = Detect(invalidSchema, ProviderCalico)
		Expect(result.Provider).To(Equal(ProviderNone))
		Expect(result.Reason).To(Equal(ReasonSchemaInvalid))
	})

	It("qualifies OVN from the OpenShift platform version rather than the platform name", func() {
		snapshot := DiscoverySnapshot{Candidates: map[Provider]ProviderSnapshot{
			ProviderOVN: {
				Provider:                   ProviderOVN,
				APIAvailable:               true,
				Active:                     true,
				Platform:                   "OpenShift",
				PlatformVersion:            "4.19.3",
				SchemaValid:                true,
				APIServerIdentityAvailable: true,
				RequiredGVKs:               RequiredGVKs(ProviderOVN),
				Evidence:                   []string{"config.openshift.io/Network:cluster"},
			},
		}}

		result := Detect(snapshot, ProviderOVN)

		Expect(result.Ready).To(BeTrue())
		Expect(result.Provider).To(Equal(ProviderOVN))
		Expect(result.Version).To(Equal("4.19.3"))
	})
})

var _ = Describe("common policy intent", func() {
	It("does not carry static API-server CIDRs", func() {
		intent, err := BuildIntent(policyTestInstance, []string{"gateway", "datastorage"}, nil)

		Expect(err).NotTo(HaveOccurred())
		Expect(intent.APIServerIdentity).To(Equal(APIServerIdentity{ServiceName: "kubernetes", Namespace: "default"}))
		Expect(intent.StaticAPIServerCIDRs).To(BeEmpty())
	})

	It("rejects static API-server CIDR input instead of rendering it", func() {
		_, err := BuildIntent(policyTestInstance, []string{"gateway"}, []string{"10.0.0.1/32"})

		Expect(err).To(MatchError(ContainSubstring("static API-server CIDRs are not supported")))
	})

	It("rejects static API-server CIDR fields through native override validation", func() {
		err := ValidateNativeOverrides(kubernautv1alpha2.NetworkPoliciesSpec{
			APIServerCIDR:  "10.0.0.1/32",
			APIServerCIDRs: []string{"10.0.0.2/32"},
		})

		Expect(err).To(MatchError(ContainSubstring("apiServerCIDR")))
	})

	It("rejects legacy raw-policy overrides that native adapters cannot express", func() {
		spec := kubernautv1alpha2.NetworkPoliciesSpec{
			ExternalWebhooks: kubernautv1alpha2.NetworkPolicyEgressOverride{CIDR: "203.0.113.0/24"},
			Gateway: kubernautv1alpha2.NetworkPolicyNamedIngressOverride{
				IngressNamespaces: []string{"monitoring"},
			},
		}

		err := ValidateNativeOverrides(spec)

		Expect(err).To(MatchError(ContainSubstring("externalWebhooks")))
		Expect(err).To(MatchError(ContainSubstring("gateway")))
	})
})

var _ = Describe("native policy rendering", func() {
	It("renders deterministic Cilium policies without CIDR fallback", func() {
		intent, err := BuildIntent(policyTestInstance, []string{"gateway", "datastorage"}, nil)
		Expect(err).NotTo(HaveOccurred())

		result := Detect(supportedSnapshot(ProviderCilium, "1.19.8"), ProviderAuto)
		objects, err := Render(result, intent)

		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(HaveLen(2))
		for _, object := range objects {
			Expect(object.Object.GetAPIVersion()).To(Equal("cilium.io/v2"))
			Expect(object.Object.GetKind()).To(Equal("CiliumNetworkPolicy"))
			Expect(object.Object.GetNamespace()).To(Equal(policyTestInstance))
			Expect(object.Object.GetName()).To(HavePrefix("kubernaut-"))
			Expect(object.Object.Object).NotTo(HaveKey("cidr"))
		}
	})

	It("uses Cilium endpoint/entity semantics for the API server", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"gateway"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.InstanceName = policyTestInstance

		result := Detect(supportedSnapshot(ProviderCilium, "1.19.8"), ProviderCilium)
		objects, err := Render(result, intent)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(HaveLen(1))

		spec, found, err := unstructured.NestedMap(objects[0].Object.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		endpointSelector, found, err := unstructured.NestedMap(spec, "endpointSelector")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		labels, found, err := unstructured.NestedStringMap(endpointSelector, "matchLabels")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(labels).To(HaveKeyWithValue(ciliumKubernetesLabel(instanceLabel), policyTestInstance))
		ingress, found, err := unstructured.NestedSlice(spec, "ingress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		managedPeer, ok := ingress[0].(map[string]interface{})
		Expect(ok).To(BeTrue())
		fromEndpoints, found, err := unstructured.NestedSlice(managedPeer, "fromEndpoints")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		managedSelector, found, err := unstructured.NestedMap(fromEndpoints[0].(map[string]interface{}), "matchLabels")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(managedSelector["k8s:io.kubernetes.pod.namespace"]).To(Equal("kubernaut-system"))
		Expect(managedSelector).To(HaveKeyWithValue(ciliumKubernetesLabel(instanceLabel), policyTestInstance))

		egress, found, err := unstructured.NestedSlice(spec, "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(egress).To(ContainElement(HaveKeyWithValue("toEntities", []interface{}{"kube-apiserver"})))
		dnsRule := egress[2].(map[string]interface{})
		dnsPeers, found, err := unstructured.NestedSlice(dnsRule, "toEndpoints")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		dnsPeer := dnsPeers[0].(map[string]interface{})
		dnsExpressions, found, err := unstructured.NestedSlice(dnsPeer, "matchExpressions")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(dnsExpressions[0]).To(HaveKeyWithValue("operator", "In"))
		Expect(dnsExpressions[0]).To(HaveKeyWithValue("values", []interface{}{"kube-system"}))
		Expect(objects[0].Object.Object).NotTo(HaveKey("cidr"))
	})

	It("uses Calico service matching without CIDR API-server fallbacks", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"gateway"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.InstanceName = policyTestInstance

		result := Detect(supportedSnapshot(ProviderCalico, "3.31.1"), ProviderCalico)
		objects, err := Render(result, intent)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(HaveLen(1))
		Expect(objects[0].Object.GetAPIVersion()).To(Equal("projectcalico.org/v3"))

		spec, found, err := unstructured.NestedMap(objects[0].Object.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(spec["selector"]).To(ContainSubstring("app == 'gateway'"))
		egress, found, err := unstructured.NestedSlice(spec, "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		servicesRule := false
		dnsRule := false
		for _, item := range egress {
			rule, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			destination, ok := rule["destination"].(map[string]interface{})
			if !ok {
				continue
			}
			if _, ok := destination["services"]; ok {
				servicesRule = true
			}
			if destination["namespaceSelector"] == "projectcalico.org/name == 'kube-system'" && destination["selector"] == "k8s-app == 'kube-dns'" {
				dnsRule = true
				Expect(destination).NotTo(HaveKey("nets"))
			}
		}
		Expect(servicesRule).To(BeTrue())
		Expect(dnsRule).To(BeTrue())
		Expect(objects[0].Object.Object).NotTo(HaveKey("cidr"))
	})

	It("renders OVN AdminNetworkPolicy and BaselineAdminNetworkPolicy with scoped subjects", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"gateway", "datastorage"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.InstanceName = policyTestInstance
		intent.DNSNamespace = "openshift-dns"

		result := DiscoveryResultForTest(ProviderOVN, "4.19.3")
		objects, err := Render(result, intent)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(HaveLen(2))
		Expect(objects[0].Object.GetAPIVersion()).To(Equal("policy.networking.k8s.io/v1alpha1"))
		Expect(objects[0].Object.GetKind()).To(Equal("AdminNetworkPolicy"))
		Expect(objects[1].Object.GetKind()).To(Equal("BaselineAdminNetworkPolicy"))
		Expect(objects[1].Object.GetName()).To(Equal("default"))
		for _, object := range objects {
			spec, found, err := unstructured.NestedMap(object.Object.Object, "spec")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			subject, found, err := unstructured.NestedMap(spec, "subject", "pods")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			namespaceSelector, found, err := unstructured.NestedMap(subject, "namespaceSelector")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			namespaceLabels, found, err := unstructured.NestedStringMap(namespaceSelector, "matchLabels")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(namespaceLabels).To(HaveKeyWithValue("kubernetes.io/metadata.name", "kubernaut-system"))
		}
		spec, found, err := unstructured.NestedMap(objects[0].Object.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(spec).To(HaveKeyWithValue("priority", float64(90)))
		egress, found, err := unstructured.NestedSlice(spec, "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		foundDNS := false
		for _, item := range egress {
			rule := item.(map[string]interface{})
			if rule["name"] != "allow-dns" {
				continue
			}
			foundDNS = true
			to := rule["to"].([]interface{})[0].(map[string]interface{})
			pods := to["pods"].(map[string]interface{})
			dnsNamespace, _, err := unstructured.NestedString(pods, "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name")
			Expect(err).NotTo(HaveOccurred())
			Expect(dnsNamespace).To(Equal("openshift-dns"))
			dnsApp, _, err := unstructured.NestedString(pods, "podSelector", "matchLabels", "app")
			Expect(err).NotTo(HaveOccurred())
			Expect(dnsApp).To(Equal("dns"))
			Expect(rule).NotTo(HaveKey("namespaces"))
		}
		Expect(foundDNS).To(BeTrue())
		apiRule := egress[0].(map[string]interface{})
		apiPorts := apiRule["ports"].([]interface{})
		Expect(apiPorts).To(HaveLen(1))
		apiPort := apiPorts[0].(map[string]interface{})["portNumber"].(map[string]interface{})
		Expect(apiPort).To(HaveKeyWithValue("port", float64(6443)))
		Expect(apiPort).To(HaveKeyWithValue("protocol", "TCP"))
	})
})

// DiscoveryResultForTest builds a ready native-provider result for renderer
// tests without coupling those tests to live API discovery.
func DiscoveryResultForTest(provider Provider, version string) DetectionResult {
	return DetectionResult{
		Provider: provider,
		Ready:    true,
		Reason:   ReasonProviderReady,
		Version:  version,
	}
}

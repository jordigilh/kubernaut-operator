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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("provider discovery", func() {
	It("requires both a served provider schema and ready Cilium installation evidence", func() {
		gvk := schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}
		mapper := testRESTMapper(gvk, meta.RESTScopeNamespace)
		client := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(
			testProviderCRD(gvk, "ciliumnetworkpolicies", apiextensionsv1.NamespaceScoped),
			readyDaemonSet("cilium", "quay.io/cilium/cilium:v1.19.8"),
		).Build()

		snapshot := (Detector{Client: client, RESTMapper: mapper}).Discover(context.Background())
		candidate := snapshot.Candidates[ProviderCilium]
		Expect(candidate.Active).To(BeTrue())
		Expect(candidate.SchemaValid).To(BeTrue())
		Expect(candidate.Version).To(Equal("1.19.8"))

		result := Detect(snapshot, ProviderCilium)
		Expect(result.Ready).To(BeTrue())
		Expect(result.Provider).To(Equal(ProviderCilium))
	})

	It("does not treat an unrelated DaemonSet label containing cilium as active evidence", func() {
		gvk := schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}
		mapper := testRESTMapper(gvk, meta.RESTScopeNamespace)
		unrelated := readyDaemonSet("network-agent", "quay.io/example/network-agent:v1.19.8")
		unrelated.Labels = map[string]string{"network": "cilium-compatible"}
		client := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(
			testProviderCRD(gvk, "ciliumnetworkpolicies", apiextensionsv1.NamespaceScoped),
			unrelated,
		).Build()

		candidate := (Detector{Client: client, RESTMapper: mapper}).Discover(context.Background()).Candidates[ProviderCilium]
		Expect(candidate.Active).To(BeFalse())
	})

	It("detects Calico from its ready daemonset and native CRD", func() {
		gvk := schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}
		mapper := testRESTMapper(gvk, meta.RESTScopeNamespace)
		client := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(
			testProviderCRD(gvk, "networkpolicies", apiextensionsv1.NamespaceScoped),
			readyDaemonSet("calico-node", "docker.io/calico/node:v3.31.1"),
		).Build()

		candidate := (Detector{Client: client, RESTMapper: mapper}).Discover(context.Background()).Candidates[ProviderCalico]
		Expect(candidate.Active).To(BeTrue())
		Expect(candidate.SchemaValid).To(BeTrue())
		Expect(candidate.Version).To(Equal("3.31.1"))
		Expect(Detect(DiscoverySnapshot{Candidates: map[Provider]ProviderSnapshot{ProviderCalico: candidate}}, ProviderCalico).Ready).To(BeTrue())
	})

	It("accepts Calico's aggregated v3 API when no v3 CRD is present", func() {
		gvk := schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}
		mapper := testRESTMapper(gvk, meta.RESTScopeNamespace)
		client := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(
			readyDaemonSet("calico-node", "docker.io/calico/node:v3.31.1"),
		).Build()

		candidate := (Detector{Client: client, RESTMapper: mapper}).Discover(context.Background()).Candidates[ProviderCalico]
		Expect(candidate.APIAvailable).To(BeTrue())
		Expect(candidate.Active).To(BeTrue())
		Expect(candidate.SchemaValid).To(BeTrue())
		Expect(candidate.APIServerIdentityAvailable).To(BeTrue())
		Expect(Detect(DiscoverySnapshot{Candidates: map[Provider]ProviderSnapshot{ProviderCalico: candidate}}, ProviderCalico).Ready).To(BeTrue())
	})

	It("rejects Calico's etcd datastore because service identity is unavailable", func() {
		gvk := schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}
		mapper := testRESTMapper(gvk, meta.RESTScopeNamespace)
		calicoNode := readyDaemonSet("calico-node", "docker.io/calico/node:v3.31.1")
		calicoNode.Spec.Template.Spec.Containers[0].Env[0].Value = "etcdv3"
		client := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(
			testProviderCRD(gvk, "networkpolicies", apiextensionsv1.NamespaceScoped),
			calicoNode,
		).Build()

		candidate := (Detector{Client: client, RESTMapper: mapper}).Discover(context.Background()).Candidates[ProviderCalico]
		Expect(candidate.Active).To(BeTrue())
		Expect(candidate.SchemaValid).To(BeTrue())
		Expect(candidate.APIServerIdentityAvailable).To(BeFalse())
		result := Detect(DiscoverySnapshot{Candidates: map[Provider]ProviderSnapshot{ProviderCalico: candidate}}, ProviderCalico)
		Expect(result.Ready).To(BeFalse())
		Expect(result.Reason).To(Equal(ReasonProviderUnavailable))
	})

	It("rejects a provider CRD whose spec omits fields consumed by the adapter", func() {
		gvk := schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}
		mapper := testRESTMapper(gvk, meta.RESTScopeNamespace)
		crd := testProviderCRD(gvk, "ciliumnetworkpolicies", apiextensionsv1.NamespaceScoped)
		specSchema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
		delete(specSchema.Properties, "egress")
		crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"] = specSchema
		client := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(
			crd,
			readyDaemonSet("cilium", "quay.io/cilium/cilium:v1.19.8"),
		).Build()

		candidate := (Detector{Client: client, RESTMapper: mapper}).Discover(context.Background()).Candidates[ProviderCilium]
		Expect(candidate.APIAvailable).To(BeFalse())
		Expect(candidate.SchemaValid).To(BeFalse())
		Expect(candidate.RequiredGVKs).To(BeEmpty())
	})

	It("rejects a provider CRD whose consumed field has the wrong schema type", func() {
		gvk := schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}
		mapper := testRESTMapper(gvk, meta.RESTScopeNamespace)
		crd := testProviderCRD(gvk, "networkpolicies", apiextensionsv1.NamespaceScoped)
		properties := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
		properties.Properties["selector"] = apiextensionsv1.JSONSchemaProps{Type: "array"}
		crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"] = properties
		client := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(
			crd,
			readyDaemonSet("calico-node", "docker.io/calico/node:v3.31.1"),
		).Build()

		candidate := (Detector{Client: client, RESTMapper: mapper}).Discover(context.Background()).Candidates[ProviderCalico]
		Expect(candidate.SchemaValid).To(BeFalse())
		Expect(candidate.RequiredGVKs).To(BeEmpty())
	})

	It("rejects a Cilium schema that omits the default-deny field consumed by the renderer", func() {
		gvk := schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}
		mapper := testRESTMapper(gvk, meta.RESTScopeNamespace)
		crd := testProviderCRD(gvk, "ciliumnetworkpolicies", apiextensionsv1.NamespaceScoped)
		specSchema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
		delete(specSchema.Properties, "enableDefaultDeny")
		crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"] = specSchema
		client := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(
			crd,
			readyDaemonSet("cilium", "quay.io/cilium/cilium:v1.19.8"),
		).Build()

		candidate := (Detector{Client: client, RESTMapper: mapper}).Discover(context.Background()).Candidates[ProviderCilium]
		Expect(candidate.SchemaValid).To(BeFalse())
	})

	It("rejects a Calico schema that omits policy order consumed by the renderer", func() {
		gvk := schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}
		mapper := testRESTMapper(gvk, meta.RESTScopeNamespace)
		crd := testProviderCRD(gvk, "networkpolicies", apiextensionsv1.NamespaceScoped)
		specSchema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
		delete(specSchema.Properties, "order")
		crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"] = specSchema
		client := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(
			crd,
			readyDaemonSet("calico-node", "docker.io/calico/node:v3.31.1"),
		).Build()

		candidate := (Detector{Client: client, RESTMapper: mapper}).Discover(context.Background()).Candidates[ProviderCalico]
		Expect(candidate.SchemaValid).To(BeFalse())
	})

	It("requires positive OVN configuration and qualifies only supported OpenShift versions", func() {
		adminGVK := schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "AdminNetworkPolicy"}
		baselineGVK := schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "BaselineAdminNetworkPolicy"}
		network := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "Network",
			"metadata":   map[string]interface{}{"name": "cluster"},
			"spec": map[string]interface{}{
				"defaultNetwork": map[string]interface{}{"type": "OVNKubernetes"},
			},
		}}
		clusterVersion := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "ClusterVersion",
			"metadata":   map[string]interface{}{"name": "version"},
			"status": map[string]interface{}{
				"desired": map[string]interface{}{"version": "4.19.3"},
			},
		}}
		mapper := testRESTMapper(adminGVK, meta.RESTScopeRoot)
		mapper.Add(baselineGVK, meta.RESTScopeRoot)
		client := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(
			testProviderCRD(adminGVK, "adminnetworkpolicies", apiextensionsv1.ClusterScoped),
			testProviderCRD(baselineGVK, "baselineadminnetworkpolicies", apiextensionsv1.ClusterScoped),
			network, clusterVersion,
		).Build()

		candidate := (Detector{Client: client, RESTMapper: mapper}).Discover(context.Background()).Candidates[ProviderOVN]
		Expect(candidate.Active).To(BeTrue())
		Expect(candidate.Platform).To(Equal("OpenShift"))
		Expect(candidate.PlatformVersion).To(Equal("4.19.3"))
		Expect(candidate.SchemaValid).To(BeTrue())
		Expect(Detect(DiscoverySnapshot{Candidates: map[Provider]ProviderSnapshot{ProviderOVN: candidate}}, ProviderOVN).Ready).To(BeTrue())

		candidate.PlatformVersion = "4.23.0"
		unsupported := Detect(DiscoverySnapshot{Candidates: map[Provider]ProviderSnapshot{ProviderOVN: candidate}}, ProviderOVN)
		Expect(unsupported.Ready).To(BeFalse())
		Expect(unsupported.Reason).To(Equal(ReasonUnsupportedVersion))
	})
})

func testDiscoveryScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(apiextensionsv1.AddToScheme(scheme)).To(Succeed())
	return scheme
}

func testRESTMapper(gvk schema.GroupVersionKind, scope meta.RESTScope) *meta.DefaultRESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gvk.GroupVersion()})
	mapper.Add(gvk, scope)
	return mapper
}

func testProviderCRD(gvk schema.GroupVersionKind, plural string, scope apiextensionsv1.ResourceScope) *apiextensionsv1.CustomResourceDefinition {
	properties := map[string]apiextensionsv1.JSONSchemaProps{}
	switch gvk.Kind {
	case "CiliumNetworkPolicy":
		properties = map[string]apiextensionsv1.JSONSchemaProps{
			"endpointSelector":  {Type: "object"},
			"enableDefaultDeny": {Type: "object"},
			"ingress":           {Type: "array"},
			"egress":            {Type: "array"},
		}
	case "NetworkPolicy":
		properties = map[string]apiextensionsv1.JSONSchemaProps{
			"selector": {Type: "string"},
			"order":    {Type: "number"},
			"types":    {Type: "array"},
			"ingress":  {Type: "array"},
			"egress":   {Type: "array"},
		}
	case "AdminNetworkPolicy":
		properties = map[string]apiextensionsv1.JSONSchemaProps{
			"priority": {Type: "integer"},
			"subject":  {Type: "object"},
			"ingress":  {Type: "array"},
			"egress":   {Type: "array"},
		}
	case "BaselineAdminNetworkPolicy":
		properties = map[string]apiextensionsv1.JSONSchemaProps{
			"subject": {Type: "object"},
			"ingress": {Type: "array"},
			"egress":  {Type: "array"},
		}
	}
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: plural + "." + gvk.Group},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: gvk.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind: gvk.Kind, Plural: plural, Singular: plural[:len(plural)-1],
			},
			Scope: scope,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: gvk.Version, Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec": {Type: "object", Properties: properties},
						},
					},
				},
			}},
		},
	}
}

func readyDaemonSet(name, image string) *appsv1.DaemonSet {
	env := []corev1.EnvVar(nil)
	if name == "calico-node" {
		env = []corev1.EnvVar{{Name: "DATASTORE_TYPE", Value: "kubernetes"}}
	}
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kube-system", Labels: map[string]string{"app": name}},
		Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: name, Image: image, Env: env}},
		}}},
		Status: appsv1.DaemonSetStatus{NumberReady: 1},
	}
}

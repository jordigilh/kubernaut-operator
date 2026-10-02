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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("native provider policy reconciliation wiring", func() {
	It("submits a Cilium policy through the deployment phase and reports readiness", func() {
		createBYOSecrets(ctx)

		providerCRD := ciliumPolicyCRD()
		providerDaemonSet := ciliumProviderDaemonSet()
		Expect(k8sClient.Create(ctx, providerCRD)).To(Succeed())
		Expect(k8sClient.Create(ctx, providerDaemonSet)).To(Succeed())
		providerDaemonSet.Status.NumberReady = 1
		Expect(k8sClient.Status().Update(ctx, providerDaemonSet)).To(Succeed())

		DeferCleanup(func() {
			cleanupCtx := context.Background()
			cleanupErrors := newReconciler().deleteProviderPolicies(cleanupCtx, testNamespace, kubernautv1alpha2.SingletonName)
			Expect(cleanupErrors).To(BeEmpty())
			deleteCRIfExists(cleanupCtx)
			cleanupNamespacedResources(cleanupCtx)
			deleteBYOSecrets(cleanupCtx)
			cleanupClusterScoped(cleanupCtx)
			cleanupProviderFixture(cleanupCtx, providerDaemonSet, providerCRD)
		})

		cr := newCRWithRouteDisabled()
		cr.Spec.NetworkPolicies.Provider = kubernautv1alpha2.NetworkPolicyProviderCilium
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		reconcileToDeployPhase(ctx)

		policies := &unstructured.UnstructuredList{}
		policies.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicyList",
		})
		Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace))).To(Succeed())
		Expect(policies.Items).NotTo(BeEmpty(), "deployment reconciliation must create a native Cilium policy")

		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		condition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})
})

func ciliumPolicyCRD() *apiextensionsv1.CustomResourceDefinition {
	object := &apiextensionsv1.JSONSchemaProps{Type: "object", XPreserveUnknownFields: ptr.To(true)}
	array := &apiextensionsv1.JSONSchemaProps{
		Type: "array",
		Items: &apiextensionsv1.JSONSchemaPropsOrArray{
			Schema: &apiextensionsv1.JSONSchemaProps{Type: "object", XPreserveUnknownFields: ptr.To(true)},
		},
	}
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "ciliumnetworkpolicies.cilium.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "cilium.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind: "CiliumNetworkPolicy", Plural: "ciliumnetworkpolicies", Singular: "ciliumnetworkpolicy",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v2", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"endpointSelector":  *object,
							"enableDefaultDeny": *object,
							"ingress":           *array,
							"egress":            *array,
						}},
					},
				}},
			}},
		},
	}
}

func ciliumProviderDaemonSet() *appsv1.DaemonSet {
	labels := map[string]string{"app": "cilium"}
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cilium", Namespace: "kube-system", Labels: labels},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "cilium", Image: "quay.io/cilium/cilium:v1.20.2",
				}}},
			},
		},
	}
}

func cleanupProviderFixture(ctx context.Context, daemonSet *appsv1.DaemonSet, crd *apiextensionsv1.CustomResourceDefinition) {
	if err := k8sClient.Delete(ctx, daemonSet); err != nil && !apierrors.IsNotFound(err) {
		return
	}
	_ = k8sClient.Delete(ctx, crd)
}

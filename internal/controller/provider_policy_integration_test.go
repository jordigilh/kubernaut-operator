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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/policy"
)

const agentPolicyName = "kubernaut-kubernaut-agent"

var _ = Describe("native provider policy reconciliation wiring", func() {
	It("submits a Cilium policy through the deployment phase and reports readiness", func() {
		createBYOSecrets(ctx)
		waitForCiliumPolicyCRDDeletion(ctx)
		monitoringNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-policy"}}
		Expect(k8sClient.Create(ctx, monitoringNamespace)).To(Succeed())
		monitoringServices := []*corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: monitoringNamespace.Name},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "prometheus"},
					Ports: []corev1.ServicePort{{
						Name: "web", Protocol: corev1.ProtocolTCP, Port: 9090, TargetPort: intstr.FromInt(8080),
					}},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "alertmanager", Namespace: monitoringNamespace.Name},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "alertmanager"},
					Ports: []corev1.ServicePort{{
						Name: "web", Protocol: corev1.ProtocolTCP, Port: 9093, TargetPort: intstr.FromInt(8081),
					}},
				},
			},
		}
		for _, service := range monitoringServices {
			Expect(k8sClient.Create(ctx, service)).To(Succeed())
		}

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
			for _, service := range monitoringServices {
				_ = k8sClient.Delete(cleanupCtx, service)
			}
			_ = k8sClient.Delete(cleanupCtx, monitoringNamespace)
			cleanupProviderFixture(cleanupCtx, providerDaemonSet, providerCRD)
		})

		cr := newCRWithRouteDisabled()
		cr.Spec.NetworkPolicies.Provider = kubernautv1alpha2.NetworkPolicyProviderCilium
		cr.Spec.Monitoring.Prometheus.URL = "https://prometheus.monitoring-policy.svc:9090"
		cr.Spec.Monitoring.AlertManager.URL = "https://alertmanager.monitoring-policy.svc:9093"
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		reconcileToDeployPhase(ctx)

		policies := &unstructured.UnstructuredList{}
		policies.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicyList",
		})
		Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace))).To(Succeed())
		Expect(policies.Items).NotTo(BeEmpty(), "deployment reconciliation must create a native Cilium policy")
		agentPolicy := (*unstructured.Unstructured)(nil)
		for index := range policies.Items {
			if policies.Items[index].GetName() == agentPolicyName {
				agentPolicy = &policies.Items[index]
				break
			}
		}
		Expect(agentPolicy).NotTo(BeNil(), "deployment reconciliation must create the Agent policy")
		spec, found, err := unstructured.NestedMap(agentPolicy.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		egress, found, err := unstructured.NestedSlice(spec, "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		monitoringPorts := make(map[string]string, 2)
		for _, item := range egress {
			rule, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			services, ok := rule["toServices"].([]interface{})
			if !ok || len(services) != 1 {
				continue
			}
			serviceRule, ok := services[0].(map[string]interface{})
			if !ok {
				continue
			}
			k8sService, ok := serviceRule["k8sService"].(map[string]interface{})
			if !ok {
				continue
			}
			serviceName, ok := k8sService["serviceName"].(string)
			if !ok {
				continue
			}
			toPorts, ok := rule["toPorts"].([]interface{})
			if !ok || len(toPorts) != 1 {
				continue
			}
			portRule, ok := toPorts[0].(map[string]interface{})
			if !ok {
				continue
			}
			ports, ok := portRule["ports"].([]interface{})
			if !ok || len(ports) != 1 {
				continue
			}
			port, ok := ports[0].(map[string]interface{})
			if !ok {
				continue
			}
			portValue, ok := port["port"].(string)
			if ok {
				monitoringPorts[serviceName] = portValue
			}
		}
		Expect(monitoringPorts).To(Equal(map[string]string{"prometheus": "8080", "alertmanager": "8081"}))

		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		condition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})

	It("keeps native base policy but reports an unresolved monitoring destination", func() {
		createBYOSecrets(ctx)
		waitForCiliumPolicyCRDDeletion(ctx)

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
		cr.Spec.Monitoring.Prometheus.URL = "https://missing.monitoring-policy.svc:9090"
		cr.Spec.Monitoring.AlertManager.URL = "https://missing-alertmanager.monitoring-policy.svc:9093"
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		reconcileToDeployPhase(ctx)

		policies := &unstructured.UnstructuredList{}
		policies.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicyList",
		})
		Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace))).To(Succeed())
		var agentPolicy *unstructured.Unstructured
		for index := range policies.Items {
			if policies.Items[index].GetName() == agentPolicyName {
				agentPolicy = &policies.Items[index]
				break
			}
		}
		Expect(agentPolicy).NotTo(BeNil())
		spec, found, err := unstructured.NestedMap(agentPolicy.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		egress, found, err := unstructured.NestedSlice(spec, "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		for _, item := range egress {
			Expect(item.(map[string]interface{})).NotTo(HaveKey("toServices"))
		}

		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		providerCondition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionProviderDetected)
		Expect(providerCondition).NotTo(BeNil())
		Expect(providerCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(providerCondition.Reason).To(Equal(policy.ReasonProviderReady))
		policyCondition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(policyCondition).NotTo(BeNil())
		Expect(policyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(policyCondition.Reason).To(Equal(policy.ReasonMonitoringUnavailable))
		Expect(policyCondition.Message).To(ContainSubstring("missing"))
	})

	It("keeps a healthy monitoring rule when another destination is unresolved", func() {
		createBYOSecrets(ctx)
		waitForCiliumPolicyCRDDeletion(ctx)
		monitoringNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-policy-partial"}}
		Expect(k8sClient.Create(ctx, monitoringNamespace)).To(Succeed())
		prometheus := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: monitoringNamespace.Name},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "prometheus"},
				Ports: []corev1.ServicePort{{
					Name: "web", Protocol: corev1.ProtocolTCP, Port: 9090, TargetPort: intstr.FromInt(8080),
				}},
			},
		}
		Expect(k8sClient.Create(ctx, prometheus)).To(Succeed())

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
			_ = k8sClient.Delete(cleanupCtx, prometheus)
			_ = k8sClient.Delete(cleanupCtx, monitoringNamespace)
			cleanupProviderFixture(cleanupCtx, providerDaemonSet, providerCRD)
		})

		cr := newCRWithRouteDisabled()
		cr.Spec.NetworkPolicies.Provider = kubernautv1alpha2.NetworkPolicyProviderCilium
		cr.Spec.Monitoring.Prometheus.URL = "https://prometheus.monitoring-policy-partial.svc:9090"
		cr.Spec.Monitoring.AlertManager.URL = "https://missing.monitoring-policy-partial.svc:9093"
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		reconcileToDeployPhase(ctx)

		policies := &unstructured.UnstructuredList{}
		policies.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicyList",
		})
		Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace))).To(Succeed())
		var agentPolicy *unstructured.Unstructured
		for index := range policies.Items {
			if policies.Items[index].GetName() == agentPolicyName {
				agentPolicy = &policies.Items[index]
				break
			}
		}
		Expect(agentPolicy).NotTo(BeNil())
		spec, found, err := unstructured.NestedMap(agentPolicy.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		egress, found, err := unstructured.NestedSlice(spec, "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		monitoringServicesFound := make(map[string]bool)
		for _, item := range egress {
			rule, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			services, ok := rule["toServices"].([]interface{})
			if !ok || len(services) != 1 {
				continue
			}
			serviceRule, ok := services[0].(map[string]interface{})
			if !ok {
				continue
			}
			k8sService, ok := serviceRule["k8sService"].(map[string]interface{})
			if !ok {
				continue
			}
			if name, ok := k8sService["serviceName"].(string); ok {
				monitoringServicesFound[name] = true
			}
		}
		Expect(monitoringServicesFound).To(Equal(map[string]bool{"prometheus": true}))

		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		policyCondition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(policyCondition).NotTo(BeNil())
		Expect(policyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(policyCondition.Reason).To(Equal(policy.ReasonMonitoringUnavailable))
	})

	It("submits a Calico Service policy through the deployment phase", func() {
		createBYOSecrets(ctx)
		waitForCalicoPolicyCRDDeletion(ctx)
		monitoringNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-policy-calico"}}
		Expect(k8sClient.Create(ctx, monitoringNamespace)).To(Succeed())
		monitoringServices := []*corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: monitoringNamespace.Name},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "prometheus"},
					Ports: []corev1.ServicePort{{
						Name: "web", Protocol: corev1.ProtocolTCP, Port: 9090, TargetPort: intstr.FromInt(8080),
					}},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "alertmanager", Namespace: monitoringNamespace.Name},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "alertmanager"},
					Ports: []corev1.ServicePort{{
						Name: "web", Protocol: corev1.ProtocolTCP, Port: 9093, TargetPort: intstr.FromInt(8081),
					}},
				},
			},
		}
		for _, service := range monitoringServices {
			Expect(k8sClient.Create(ctx, service)).To(Succeed())
		}

		providerCRD := calicoPolicyCRD()
		providerDaemonSet := calicoProviderDaemonSet()
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
			for _, service := range monitoringServices {
				_ = k8sClient.Delete(cleanupCtx, service)
			}
			_ = k8sClient.Delete(cleanupCtx, monitoringNamespace)
			cleanupProviderFixture(cleanupCtx, providerDaemonSet, providerCRD)
		})

		cr := newCRWithRouteDisabled()
		cr.Spec.NetworkPolicies.Provider = kubernautv1alpha2.NetworkPolicyProviderCalico
		cr.Spec.Monitoring.Prometheus.URL = "https://prometheus.monitoring-policy-calico.svc:9090"
		cr.Spec.Monitoring.AlertManager.URL = "https://alertmanager.monitoring-policy-calico.svc:9093"
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		reconcileToDeployPhase(ctx)

		policies := &unstructured.UnstructuredList{}
		policies.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicyList",
		})
		Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace))).To(Succeed())
		var agentPolicy *unstructured.Unstructured
		for index := range policies.Items {
			if policies.Items[index].GetName() == agentPolicyName {
				agentPolicy = &policies.Items[index]
				break
			}
		}
		Expect(agentPolicy).NotTo(BeNil())
		spec, found, err := unstructured.NestedMap(agentPolicy.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		egress, found, err := unstructured.NestedSlice(spec, "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		monitoringServicesFound := make(map[string]bool, 2)
		for _, item := range egress {
			rule, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			destination, ok := rule["destination"].(map[string]interface{})
			if !ok {
				continue
			}
			service, ok := destination["services"].(map[string]interface{})
			if !ok {
				continue
			}
			name, ok := service["name"].(string)
			if ok && (name == "prometheus" || name == "alertmanager") {
				monitoringServicesFound[name] = true
				Expect(destination).NotTo(HaveKey("ports"))
			}
		}
		Expect(monitoringServicesFound).To(Equal(map[string]bool{"prometheus": true, "alertmanager": true}))

		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		condition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})

	It("submits OVN monitoring policies through the deployment phase", func() {
		createBYOSecrets(ctx)
		waitForOVNPolicyCRDDeletion(ctx)
		adminCRD := ovnPolicyCRD("AdminNetworkPolicy", "adminnetworkpolicies", true)
		baselineCRD := ovnPolicyCRD("BaselineAdminNetworkPolicy", "baselineadminnetworkpolicies", false)
		Expect(k8sClient.Create(ctx, adminCRD)).To(Succeed())
		Expect(k8sClient.Create(ctx, baselineCRD)).To(Succeed())
		monitoringNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-policy-ovn"}}
		Expect(k8sClient.Create(ctx, monitoringNamespace)).To(Succeed())
		prometheus := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: monitoringNamespace.Name},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "prometheus", "tier": "monitoring"},
				Ports: []corev1.ServicePort{{
					Name: "web", Protocol: corev1.ProtocolTCP, Port: 9091, TargetPort: intstr.FromInt(9091),
				}},
			},
		}
		alertmanager := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "alertmanager", Namespace: monitoringNamespace.Name},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "alertmanager", "tier": "monitoring"},
				Ports: []corev1.ServicePort{{
					Name: "web", Protocol: corev1.ProtocolTCP, Port: 9094, TargetPort: intstr.FromString("web"),
				}},
			},
		}
		Expect(k8sClient.Create(ctx, prometheus)).To(Succeed())
		Expect(k8sClient.Create(ctx, alertmanager)).To(Succeed())
		ready := true
		alertmanagerBackendPort := int32(9095)
		alertmanagerEndpoints := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alertmanager-1",
				Namespace: monitoringNamespace.Name,
				Labels:    map[string]string{discoveryv1.LabelServiceName: alertmanager.Name},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{{
				Name: ptr.To("web"), Protocol: ptr.To(corev1.ProtocolTCP), Port: &alertmanagerBackendPort,
			}},
			Endpoints: []discoveryv1.Endpoint{{
				Addresses:  []string{"10.0.0.10"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			}},
		}
		Expect(k8sClient.Create(ctx, alertmanagerEndpoints)).To(Succeed())

		network := &configv1.Network{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       configv1.NetworkSpec{NetworkType: "OVNKubernetes"},
		}
		clusterVersion := &configv1.ClusterVersion{ObjectMeta: metav1.ObjectMeta{Name: "version"}}
		Expect(k8sClient.Create(ctx, network)).To(Succeed())
		Expect(k8sClient.Create(ctx, clusterVersion)).To(Succeed())
		clusterVersion.Status.Desired.Version = "4.22.16"
		Expect(k8sClient.Status().Update(ctx, clusterVersion)).To(Succeed())

		DeferCleanup(func() {
			cleanupCtx := context.Background()
			cleanupErrors := newReconciler().deleteProviderPolicies(cleanupCtx, testNamespace, kubernautv1alpha2.SingletonName)
			Expect(cleanupErrors).To(BeEmpty())
			deleteCRIfExists(cleanupCtx)
			cleanupNamespacedResources(cleanupCtx)
			deleteBYOSecrets(cleanupCtx)
			cleanupClusterScoped(cleanupCtx)
			for _, object := range []client.Object{alertmanagerEndpoints, alertmanager, prometheus, monitoringNamespace} {
				_ = k8sClient.Delete(cleanupCtx, object)
			}
			_ = k8sClient.Delete(cleanupCtx, clusterVersion)
			_ = k8sClient.Delete(cleanupCtx, network)
		})

		cr := newCRWithRouteDisabled()
		cr.Spec.NetworkPolicies.Provider = kubernautv1alpha2.NetworkPolicyProviderOVN
		cr.Spec.Monitoring.Prometheus.URL = "https://prometheus.monitoring-policy-ovn.svc:9091"
		cr.Spec.Monitoring.AlertManager.URL = "https://alertmanager.monitoring-policy-ovn.svc:9094"
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		reconcileToDeployPhase(ctx)
		providerStatus := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), providerStatus)).To(Succeed())
		providerCondition := findCondition(providerStatus.Status.Conditions, kubernautv1alpha2.ConditionProviderDetected)
		Expect(providerCondition).NotTo(BeNil())
		Expect(providerCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(providerCondition.Reason).To(Equal(policy.ReasonProviderReady))

		anps := &unstructured.UnstructuredList{}
		anps.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "AdminNetworkPolicyList",
		})
		Expect(k8sClient.List(ctx, anps)).To(Succeed())
		var monitoringPolicy *unstructured.Unstructured
		for index := range anps.Items {
			if anps.Items[index].GetName() == "kubernaut-agent-monitoring" {
				monitoringPolicy = &anps.Items[index]
				break
			}
		}
		Expect(monitoringPolicy).NotTo(BeNil())
		spec, found, err := unstructured.NestedMap(monitoringPolicy.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(spec).To(HaveKeyWithValue("priority", int64(89)))
		egress, found, err := unstructured.NestedSlice(spec, "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		alertmanagerRuleFound := false
		denyRuleFound := false
		for _, item := range egress {
			rule := item.(map[string]interface{})
			switch rule["name"] {
			case "allow-monitoring-alertmanager":
				alertmanagerRuleFound = true
				port := rule["ports"].([]interface{})[0].(map[string]interface{})["portNumber"].(map[string]interface{})
				Expect(port).To(HaveKeyWithValue("port", int64(9095)))
			case "deny-other-monitoring":
				denyRuleFound = true
			}
		}
		Expect(alertmanagerRuleFound).To(BeTrue())
		Expect(denyRuleFound).To(BeTrue())

		banps := &unstructured.UnstructuredList{}
		banps.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "BaselineAdminNetworkPolicyList",
		})
		Expect(k8sClient.List(ctx, banps)).To(Succeed())
		Expect(banps.Items).To(HaveLen(1))

		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		condition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})
})

func ovnPolicyCRD(kind, plural string, includesPriority bool) *apiextensionsv1.CustomResourceDefinition {
	object := &apiextensionsv1.JSONSchemaProps{Type: "object", XPreserveUnknownFields: ptr.To(true)}
	array := &apiextensionsv1.JSONSchemaProps{
		Type:  "array",
		Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: object},
	}
	specProperties := map[string]apiextensionsv1.JSONSchemaProps{
		"subject": {Type: "object", XPreserveUnknownFields: ptr.To(true)},
		"ingress": *array,
		"egress":  *array,
	}
	if includesPriority {
		specProperties["priority"] = apiextensionsv1.JSONSchemaProps{Type: "integer"}
	}
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: plural + ".policy.networking.k8s.io",
			Annotations: map[string]string{
				"api-approved.kubernetes.io": "https://github.com/kubernetes-sigs/network-policy-api",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "policy.networking.k8s.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind: kind, Plural: plural, Singular: strings.ToLower(kind),
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1alpha1", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {Type: "object", Properties: specProperties},
					},
				}},
			}},
		},
	}
}

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
							"egress": {
								Type: "array",
								Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"toServices": {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{
											Schema: &apiextensionsv1.JSONSchemaProps{Type: "object", XPreserveUnknownFields: ptr.To(true)},
										}},
										"toPorts": {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{
											Schema: &apiextensionsv1.JSONSchemaProps{Type: "object", XPreserveUnknownFields: ptr.To(true)},
										}},
									},
								}},
							},
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

func calicoPolicyCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "networkpolicies.projectcalico.org"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "projectcalico.org",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind: "NetworkPolicy", Plural: "networkpolicies", Singular: "networkpolicy",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v3", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"selector": {Type: "string"},
								"order":    {Type: "number"},
								"types":    {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"}}},
								"ingress":  {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{Type: "object", XPreserveUnknownFields: ptr.To(true)}}},
								"egress": {
									Type: "array",
									Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{
										Type: "object",
										Properties: map[string]apiextensionsv1.JSONSchemaProps{
											"destination": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
												"services": {Type: "object", XPreserveUnknownFields: ptr.To(true)},
											}},
										},
									}},
								},
							},
						},
					},
				}},
			}},
		},
	}
}

func calicoProviderDaemonSet() *appsv1.DaemonSet {
	labels := map[string]string{"app": "calico-node"}
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "calico-node", Namespace: "kube-system", Labels: labels},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "calico-node", Image: "docker.io/calico/node:v3.31.4",
					Env: []corev1.EnvVar{{Name: "DATASTORE_TYPE", Value: "kubernetes"}},
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

func waitForCiliumPolicyCRDDeletion(ctx context.Context) {
	Eventually(func() bool {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := k8sClient.Get(ctx, client.ObjectKey{Name: "ciliumnetworkpolicies.cilium.io"}, crd)
		return apierrors.IsNotFound(err)
	}, timeout, interval).Should(BeTrue())
}

func waitForCalicoPolicyCRDDeletion(ctx context.Context) {
	Eventually(func() bool {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := k8sClient.Get(ctx, client.ObjectKey{Name: "networkpolicies.projectcalico.org"}, crd)
		return apierrors.IsNotFound(err)
	}, timeout, interval).Should(BeTrue())
}

func waitForOVNPolicyCRDDeletion(ctx context.Context) {
	for _, name := range []string{
		"adminnetworkpolicies.policy.networking.k8s.io",
		"baselineadminnetworkpolicies.policy.networking.k8s.io",
	} {
		crdName := name
		Eventually(func() bool {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: crdName}, crd)
			return apierrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	}
}

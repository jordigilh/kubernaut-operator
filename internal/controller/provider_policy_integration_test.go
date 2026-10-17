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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/policy"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

const agentPolicyName = "kubernaut-kubernaut-agent"

var _ = Describe("native provider policy reconciliation wiring", Label("manages-provider-fixture"), func() {
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
		cr.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		cr.Spec.NetworkPolicies.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{
			Namespace: monitoringNamespace.Name, PodLabels: map[string]string{"app": "prometheus"},
		}}
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
		servicePorts := make(map[string]string, 4)
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
				servicePorts[serviceName] = portValue
			}
		}
		Expect(servicePorts).To(Equal(map[string]string{
			"postgresql": "5432", "valkey": "6379", "prometheus": "8080", "alertmanager": "8081",
		}))
		ingress, found, err := unstructured.NestedSlice(spec, "ingress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(ingress).To(HaveLen(2), "actual Reconcile must wire the external scraper identity, not just monitoring egress")
		scrapeRule := ingress[1].(map[string]interface{})
		from := scrapeRule["fromEndpoints"].([]interface{})[0].(map[string]interface{})
		Expect(from["matchLabels"]).To(HaveKeyWithValue("k8s:io.kubernetes.pod.namespace", monitoringNamespace.Name))
		Expect(from["matchLabels"]).To(HaveKeyWithValue("k8s:app", "prometheus"))
		ports := scrapeRule["toPorts"].([]interface{})[0].(map[string]interface{})["ports"]
		Expect(ports).To(Equal([]interface{}{map[string]interface{}{"port": "9090", "protocol": "TCP"}}))

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
		cr.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
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
		resolvedDependencies := make(map[string]bool)
		for _, item := range egress {
			rule := item.(map[string]interface{})
			services, hasServices := rule["toServices"].([]interface{})
			if !hasServices || len(services) != 1 {
				continue
			}
			serviceRule := services[0].(map[string]interface{})
			service := serviceRule["k8sService"].(map[string]interface{})
			name := service["serviceName"].(string)
			Expect(name).To(Or(Equal("postgresql"), Equal("valkey")))
			resolvedDependencies[name] = true
		}
		Expect(resolvedDependencies).To(Equal(map[string]bool{"postgresql": true, "valkey": true}))

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
		cr.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
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
		Expect(monitoringServicesFound).To(Equal(map[string]bool{
			"postgresql": true, "valkey": true, "prometheus": true,
		}))

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
		cr.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		cr.Spec.NetworkPolicies.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{
			Namespace: monitoringNamespace.Name, PodLabels: map[string]string{"app": "prometheus"},
		}}
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
			protocol, ok := rule["protocol"].(string)
			if !ok || protocol != string(corev1.ProtocolTCP) {
				continue
			}
			destination, ok := rule["destination"].(map[string]interface{})
			if !ok {
				continue
			}
			if destination["namespaceSelector"] != "projectcalico.org/name == 'monitoring-policy-calico'" {
				continue
			}
			selector, ok := destination["selector"].(string)
			if !ok {
				continue
			}
			var name string
			var backendPort int64
			switch selector {
			case "app == 'prometheus'":
				name, backendPort = "prometheus", 8080
			case "app == 'alertmanager'":
				name, backendPort = "alertmanager", 8081
			default:
				continue
			}
			ports, ok := destination["ports"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(ports).To(Equal([]interface{}{backendPort}))
			Expect(destination).NotTo(HaveKey("services"))
			monitoringServicesFound[name] = true
		}
		Expect(monitoringServicesFound).To(Equal(map[string]bool{"prometheus": true, "alertmanager": true}))
		ingress, found, err := unstructured.NestedSlice(spec, "ingress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(ingress).To(HaveLen(2), "Reconcile must positively authorize the Calico scraper")
		Expect(ingress[1]).To(HaveKeyWithValue("source", map[string]interface{}{
			"namespaceSelector": "projectcalico.org/name == 'monitoring-policy-calico'", "selector": "app == 'prometheus'",
		}))
		Expect(ingress[1]).To(HaveKeyWithValue("destination", map[string]interface{}{"ports": []interface{}{int64(9090)}}))

		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		condition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})

	It("IT-AUDIT-518-DNS-001: wires discovered DNS backends and terminal denies through deployment reconciliation", func() {
		createBYOSecrets(ctx)
		waitForOVNPolicyCRDDeletion(ctx)
		adminCRD := ovnPolicyCRD("AdminNetworkPolicy", "adminnetworkpolicies", true)
		baselineCRD := ovnPolicyCRD("BaselineAdminNetworkPolicy", "baselineadminnetworkpolicies", false)
		Expect(k8sClient.Create(ctx, adminCRD)).To(Succeed())
		Expect(k8sClient.Create(ctx, baselineCRD)).To(Succeed())
		dnsNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: resources.OCPDNSNamespace}}
		Expect(k8sClient.Create(ctx, dnsNamespace)).To(Succeed())
		dnsService := ovnDNSServiceFixture()
		Expect(k8sClient.Create(ctx, dnsService)).To(Succeed())
		dnsEndpoints := ovnDNSEndpointSliceFixture(dnsService)
		Expect(k8sClient.Create(ctx, dnsEndpoints)).To(Succeed())
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
			for _, object := range []client.Object{alertmanagerEndpoints, alertmanager, prometheus, monitoringNamespace,
				dnsEndpoints, dnsService, dnsNamespace} {
				deleteErr := k8sClient.Delete(cleanupCtx, object)
				Expect(deleteErr == nil || apierrors.IsNotFound(deleteErr)).To(BeTrue())
			}
			_ = k8sClient.Delete(cleanupCtx, clusterVersion)
			_ = k8sClient.Delete(cleanupCtx, network)
		})

		cr := newCRWithRouteDisabled()
		cr.Spec.NetworkPolicies.Provider = kubernautv1alpha2.NetworkPolicyProviderOVN
		cr.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		cr.Spec.NetworkPolicies.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{
			Namespace: monitoringNamespace.Name, PodLabels: map[string]string{"app": "prometheus"},
		}}
		cr.Spec.Monitoring.Prometheus.URL = "https://prometheus.monitoring-policy-ovn.svc:9091"
		cr.Spec.APIFrontend.Enabled = ptr.To(true)
		cr.Spec.APIFrontend.MetricsPort = ptr.To[int32](19180)
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
		common := &unstructured.Unstructured{}
		common.SetGroupVersionKind(policy.RequiredGVKs(policy.ProviderOVN)[0])
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "kubernaut-admin"}, common)).To(Succeed())
		dnsEgress, found, err := unstructured.NestedSlice(common.Object, "spec", "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(dnsEgress).To(ContainElement(HaveKeyWithValue("name", "allow-dns")))
		for _, item := range dnsEgress {
			rule := item.(map[string]interface{})
			if rule["name"] != "allow-dns" {
				continue
			}
			peer := rule["to"].([]interface{})[0].(map[string]interface{})
			selector, selectorFound, selectorErr := unstructured.NestedStringMap(peer, "pods", "podSelector", "matchLabels")
			Expect(selectorErr).NotTo(HaveOccurred())
			Expect(selectorFound).To(BeTrue())
			Expect(selector).To(Equal(dnsService.Spec.Selector), "the DNS peer must come from the persisted Service")
			Expect(rule["ports"]).To(ConsistOf(
				map[string]interface{}{"portNumber": map[string]interface{}{"protocol": "UDP", "port": int64(5353)}},
				map[string]interface{}{"portNumber": map[string]interface{}{"protocol": "TCP", "port": int64(5454)}},
			))
		}
		Expect(dnsEgress[len(dnsEgress)-1]).To(HaveKeyWithValue("action", "Deny"))
		commonIngress, found, err := unstructured.NestedSlice(common.Object, "spec", "ingress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(commonIngress[len(commonIngress)-1]).To(HaveKeyWithValue("action", "Deny"))
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
		metricsPolicy := &unstructured.Unstructured{}
		metricsPolicy.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "AdminNetworkPolicy",
		})
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "kubernaut-data-storage-metrics"}, metricsPolicy)).To(Succeed())
		metricsSpec, found, err := unstructured.NestedMap(metricsPolicy.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		subjectLabels, found, err := unstructured.NestedStringMap(metricsSpec, "subject", "pods", "podSelector", "matchLabels")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(subjectLabels).To(HaveKeyWithValue("app", "data-storage"))
		ingress := metricsSpec["ingress"].([]interface{})
		Expect(ingress).To(HaveLen(1))
		Expect(ingress[0]).To(HaveKeyWithValue("ports", []interface{}{map[string]interface{}{"portNumber": map[string]interface{}{
			"port": int64(9090), "protocol": "TCP",
		}}}))
		Expect(ingress[0]).To(HaveKeyWithValue("from", []interface{}{map[string]interface{}{"pods": map[string]interface{}{
			"namespaceSelector": map[string]interface{}{"matchLabels": map[string]interface{}{
				"kubernetes.io/metadata.name": monitoringNamespace.Name,
			}},
			"podSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "prometheus"}},
		}}}))
		metricsPolicy.SetName("kubernaut-apifrontend-metrics")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(metricsPolicy), metricsPolicy)).To(Succeed())
		customIngress, found, err := unstructured.NestedSlice(metricsPolicy.Object, "spec", "ingress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(customIngress).To(HaveLen(1))
		Expect(customIngress[0]).To(HaveKeyWithValue("ports", []interface{}{map[string]interface{}{"portNumber": map[string]interface{}{
			"port": int64(19180), "protocol": "TCP",
		}}}))
		customService := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: resources.ComponentAPIFrontend}, customService)).To(Succeed())
		Expect(customService.Spec.Ports).To(ContainElement(HaveField("Port", int32(19180))))

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

		Expect(newReconciler().monitoringObjectToKubernaut(ctx, dnsService)).To(HaveLen(1))
		Expect(newReconciler().monitoringObjectToKubernaut(ctx, dnsEndpoints)).To(HaveLen(1))
		// Reconcile dependency changes from the actual persisted objects. Old
		// frontend/metrics/backend ports must not survive in the desired rule.
		dnsService.Spec.Selector = map[string]string{"test.kubernaut.ai/dns-backend": "changed"}
		Expect(k8sClient.Update(ctx, dnsService)).To(Succeed())
		dnsEndpoints.Ports[0].Port = ptr.To[int32](5555)
		dnsEndpoints.Ports[1].Port = ptr.To[int32](5656)
		Expect(k8sClient.Update(ctx, dnsEndpoints)).To(Succeed())
		_, reconcileErr := newReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: singletonKey()})
		Expect(reconcileErr).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(common), common)).To(Succeed())
		updatedEgress, found, err := unstructured.NestedSlice(common.Object, "spec", "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(updatedEgress).To(ContainElement(HaveKeyWithValue("ports", ConsistOf(
			map[string]interface{}{"portNumber": map[string]interface{}{"protocol": "UDP", "port": int64(5555)}},
			map[string]interface{}{"portNumber": map[string]interface{}{"protocol": "TCP", "port": int64(5656)}},
		))))
		for _, item := range updatedEgress {
			rule := item.(map[string]interface{})
			if rule["name"] == "allow-dns" {
				peer := rule["to"].([]interface{})[0].(map[string]interface{})
				selector, _, selectorErr := unstructured.NestedStringMap(peer, "pods", "podSelector", "matchLabels")
				Expect(selectorErr).NotTo(HaveOccurred())
				Expect(selector).To(Equal(dnsService.Spec.Selector))
			}
		}
		retained := common.DeepCopy()
		Expect(k8sClient.Delete(ctx, dnsService)).To(Succeed())
		_, reconcileErr = newReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: singletonKey()})
		Expect(reconcileErr).To(MatchError(ContainSubstring("dns-default")))
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(common), common)).To(Succeed())
		Expect(common.Object["spec"]).To(Equal(retained.Object["spec"]), "DNS failures must not remove existing native policies")
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		condition = findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("DNSUnavailable"))
	})
})

func ovnDNSServiceFixture() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "dns-default", Namespace: resources.OCPDNSNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"test.kubernaut.ai/dns-backend": "platform"},
			Ports: []corev1.ServicePort{
				{Name: "dns", Protocol: corev1.ProtocolUDP, Port: 53, TargetPort: intstr.FromString("dns-udp")},
				{Name: "dns-tcp", Protocol: corev1.ProtocolTCP, Port: 53, TargetPort: intstr.FromString("dns-tcp")},
				{Name: "metrics", Protocol: corev1.ProtocolTCP, Port: 9154, TargetPort: intstr.FromInt(9154)},
			},
		},
	}
}

func ovnDNSEndpointSliceFixture(service *corev1.Service) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dns-default-backends", Namespace: service.Namespace,
			Labels:          map[string]string{discoveryv1.LabelServiceName: service.Name},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Service", Name: service.Name, UID: service.UID}},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{Name: ptr.To("dns"), Protocol: ptr.To(corev1.ProtocolUDP), Port: ptr.To[int32](5353)},
			{Name: ptr.To("dns-tcp"), Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To[int32](5454)},
			{Name: ptr.To("metrics"), Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To[int32](9154)},
		},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.53"},
			Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}}},
	}
}

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
											"action":   {Type: "string"},
											"protocol": {Type: "string"},
											"destination": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
												"services":          {Type: "object", XPreserveUnknownFields: ptr.To(true)},
												"namespaceSelector": {Type: "string"},
												"selector":          {Type: "string"},
												"ports": {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{
													Schema: &apiextensionsv1.JSONSchemaProps{Type: "integer"},
												}},
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

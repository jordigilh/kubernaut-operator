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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("monitoring reconciliation wiring", func() {
	It("does not register optional monitoring watches without discovery", func() {
		Expect(monitoringResourceSupported(nil, "ServiceMonitor")).To(BeFalse())
	})

	It("maps external monitoring Services and EndpointSlices to the configured Kubernaut", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(discoveryv1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		kn := newMinimalCR()
		kn.Spec.Monitoring.Prometheus.URL = "https://prometheus.external-monitoring.svc:9090"
		kn.Spec.Monitoring.AlertManager.Enabled = ptr.To(false)
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(kn).Build()
		reconciler := &KubernautReconciler{Client: fakeClient, Scheme: scheme}
		service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: "external-monitoring"}}
		endpointSlice := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
			Name: "prometheus-1", Namespace: "external-monitoring",
			Labels: map[string]string{discoveryv1.LabelServiceName: "prometheus"},
		}}

		serviceRequests := reconciler.monitoringObjectToKubernaut(ctx, service)
		endpointRequests := reconciler.monitoringObjectToKubernaut(ctx, endpointSlice)
		Expect(serviceRequests).To(Equal([]reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(kn)}}))
		Expect(endpointRequests).To(Equal(serviceRequests))
	})

	It("removes a stale optional route when no route is desired", func() {
		reconciler := newReconciler()
		kn := newMinimalCR()
		stale := &routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "stale-route", Namespace: kn.Namespace}}

		hasRoute, err := reconciler.reconcileOptionalRoute(ctx, kn, "Gateway", nil, stale)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasRoute).To(BeFalse())
	})

	It("IT-MON-513-001 [CM-3, CM-6, CM-8, SI-4]: creates rules and service monitors when their APIs are available", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		Expect(monitoringv1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		reconciler := &KubernautReconciler{Client: fakeClient, Scheme: scheme}
		kn := newMinimalCR()
		kn.UID = types.UID("monitoring-test")

		Expect(reconciler.deployMonitoring(ctx, kn, true, true)).To(Succeed())

		rules := &monitoringv1.PrometheusRuleList{}
		Expect(fakeClient.List(ctx, rules, client.InNamespace(kn.Namespace))).To(Succeed())
		Expect(rules.Items).To(HaveLen(2))

		monitors := &monitoringv1.ServiceMonitorList{}
		Expect(fakeClient.List(ctx, monitors, client.InNamespace(kn.Namespace))).To(Succeed())
		Expect(monitors.Items).To(HaveLen(9))
		Expect(monitors.Items).NotTo(ContainElement(HaveField("Name", "authwebhook-monitor")))
	})

	It("IT-MON-513-002 [CM-6, CM-8, SI-4]: does not create optional monitoring objects when the ServiceMonitor API is unavailable", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		Expect(monitoringv1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		reconciler := &KubernautReconciler{Client: fakeClient, Scheme: scheme}
		kn := newMinimalCR()
		Expect(reconciler.deployMonitoring(ctx, kn, false, false)).To(Succeed())

		monitors := &monitoringv1.ServiceMonitorList{}
		Expect(fakeClient.List(ctx, monitors, client.InNamespace(kn.Namespace))).To(Succeed())
		Expect(monitors.Items).To(BeEmpty())
	})

	It("IT-MON-513-005 [CM-3, CM-6, CM-8, SI-4]: wires APIFrontend monitoring through optional CRD discovery", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		Expect(monitoringv1.AddToScheme(scheme)).To(Succeed())
		Expect(apiextensionsv1.AddToScheme(scheme)).To(Succeed())

		kn := newMinimalCR()
		kn.UID = types.UID("monitoring-test")
		serviceMonitorCRD := &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{
			Name: "servicemonitors.monitoring.coreos.com",
		}}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(serviceMonitorCRD).Build()
		reconciler := &KubernautReconciler{Client: fakeClient, Scheme: scheme}

		Expect(reconciler.ensureAPIFrontendMonitoring(ctx, kn)).To(Succeed())

		serviceMonitor := &monitoringv1.ServiceMonitor{}
		Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: "apifrontend-monitor"}, serviceMonitor)).To(Succeed())
		prometheusRule := &monitoringv1.PrometheusRule{}
		Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: "apifrontend-rules"}, prometheusRule)).To(Succeed())
	})

	It("IT-MON-513-003 [AC-6, CM-3, CM-6, CM-8]: removes an operator-owned legacy AuthWebhook monitor", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		Expect(monitoringv1.AddToScheme(scheme)).To(Succeed())

		kn := newMinimalCR()
		kn.UID = types.UID("monitoring-test")
		stale := &monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{
			Name: "authwebhook-monitor", Namespace: kn.Namespace,
		}}
		Expect(resources.SetOwnerReference(kn, stale, scheme)).To(Succeed())
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(stale).Build()
		reconciler := &KubernautReconciler{Client: fakeClient, Scheme: scheme}

		Expect(reconciler.deployMonitoring(ctx, kn, true, false)).To(Succeed())
		remaining := &monitoringv1.ServiceMonitor{}
		err := fakeClient.Get(ctx, client.ObjectKeyFromObject(stale), remaining)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("IT-MON-513-004 [AC-6, CM-3, CM-6]: preserves a same-named user-owned AuthWebhook monitor", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		Expect(monitoringv1.AddToScheme(scheme)).To(Succeed())

		kn := newMinimalCR()
		stale := &monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{
			Name: "authwebhook-monitor", Namespace: kn.Namespace,
		}}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(stale).Build()
		reconciler := &KubernautReconciler{Client: fakeClient, Scheme: scheme}

		Expect(reconciler.deployMonitoring(ctx, kn, true, false)).To(Succeed())
		remaining := &monitoringv1.ServiceMonitor{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(stale), remaining)).To(Succeed())
	})
})

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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("generic monitoring defaults", func() {
	newGenericReconciler := func() *KubernautReconciler {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		return &KubernautReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		}
	}

	It("does not inherit OpenShift monitoring endpoints on a generic cluster", func() {
		reconciler := newGenericReconciler()
		kn := &kubernautv1alpha2.Kubernaut{}

		view := reconciler.monitoringConfigView(ctx, kn)

		Expect(view).NotTo(BeIdenticalTo(kn))
		Expect(view.Spec.Monitoring.Prometheus.PrometheusEnabled()).To(BeFalse())
		Expect(view.Spec.Monitoring.AlertManager.AlertManagerEnabled()).To(BeFalse())
		Expect(view.Spec.Monitoring.Prometheus.URL).To(BeEmpty())
		Expect(view.Spec.Monitoring.AlertManager.URL).To(BeEmpty())
		Expect(kn.Spec.Monitoring.Prometheus.Enabled).To(BeNil())
		Expect(kn.Spec.Monitoring.AlertManager.Enabled).To(BeNil())
	})

	It("preserves explicitly configured generic monitoring endpoints", func() {
		reconciler := newGenericReconciler()
		kn := &kubernautv1alpha2.Kubernaut{}
		kn.Spec.Monitoring.Prometheus.URL = "https://prometheus.monitoring.svc:9090"
		kn.Spec.Monitoring.AlertManager.URL = "https://alertmanager.monitoring.svc:9093"

		view := reconciler.monitoringConfigView(ctx, kn)

		Expect(view.Spec.Monitoring.Prometheus.PrometheusEnabled()).To(BeTrue())
		Expect(view.Spec.Monitoring.AlertManager.AlertManagerEnabled()).To(BeTrue())
		Expect(view.Spec.Monitoring.Prometheus.URL).To(Equal(kn.Spec.Monitoring.Prometheus.URL))
		Expect(view.Spec.Monitoring.AlertManager.URL).To(Equal(kn.Spec.Monitoring.AlertManager.URL))
	})

	It("reports an unset generic endpoint as unavailable rather than disabled", func() {
		reconciler := newGenericReconciler()
		kn := &kubernautv1alpha2.Kubernaut{}
		view := reconciler.monitoringConfigView(ctx, kn)

		condition := reconciler.monitoringCondition(ctx, kn)

		Expect(view.Spec.Monitoring.Prometheus.PrometheusEnabled()).To(BeFalse())
		Expect(view.Spec.Monitoring.AlertManager.AlertManagerEnabled()).To(BeFalse())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonMonitoringUnavailable))
	})

	It("reports explicit monitoring disablement as ready without optional APIs", func() {
		reconciler := newGenericReconciler()
		kn := &kubernautv1alpha2.Kubernaut{}
		kn.Spec.Monitoring.Prometheus.Enabled = ptr.To(false)
		kn.Spec.Monitoring.AlertManager.Enabled = ptr.To(false)

		condition := reconciler.monitoringCondition(ctx, kn)

		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(ReasonMonitoringDisabled))
	})
})

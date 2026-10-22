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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("monitoring TLS source lifecycle", Label("unit"), func() {
	newMonitoringScheme := func() *runtime.Scheme {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		return scheme
	}

	It("UT-MON-563-ROT-001 [CM-3, SC-12, SI-4; SOC2 CC7.2, CC8.1; ASVS V13.3.4, V16.5.2] resolves Secret revisions and none for system trust", func() {
		scheme := newMonitoringScheme()
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca", Namespace: testNamespace, ResourceVersion: "11"}}
		reconciler := &KubernautReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(),
		}
		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: secret.Name},
		}
		first, err := reconciler.monitoringTLSMaterialRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(HaveLen(64))

		secret = secret.DeepCopy()
		secret.ResourceVersion = "12"
		reconciler.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		second, err := reconciler.monitoringTLSMaterialRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(second).NotTo(Equal(first))

		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "missing-monitoring-ca"},
		}
		_, err = reconciler.monitoringTLSMaterialRevision(context.Background(), kn)
		Expect(err).To(MatchError(ContainSubstring("could not be read")))

		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{}
		systemRevision, err := reconciler.monitoringTLSMaterialRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(systemRevision).To(BeEmpty())

		unconfiguredRevision, err := reconciler.monitoringTLSMaterialRevision(context.Background(), newMinimalCR())
		Expect(err).NotTo(HaveOccurred())
		Expect(unconfiguredRevision).To(BeEmpty())
	})

	It("UT-MON-563-WATCH-001 [AC-6, SI-4; SOC2 CC6.1, CC7.2; ASVS V13.3.4] fails closed for nil and incomplete Secret references", func() {
		Expect(monitoringTLSConfigured(nil)).To(BeFalse())
		_, _, _, ok := monitoringTLSReference(nil)
		Expect(ok).To(BeFalse())
		_, _, _, ok = monitoringTLSSecretReference(nil)
		Expect(ok).To(BeFalse())

		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{}
		_, _, _, ok = monitoringTLSReference(kn)
		Expect(ok).To(BeFalse())
	})

	It("includes the monitoring revision in the shared material resolver", func() {
		scheme := newMonitoringScheme()
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca", Namespace: testNamespace, ResourceVersion: "31"}}
		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: secret.Name},
		}
		reconciler := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()}
		revisions, err := reconciler.telemetryMaterialRevisions(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions.Monitoring).To(HaveLen(64))

		missing := kn.DeepCopy()
		missing.Spec.Monitoring.TLS.CACertSecretRef.Name = "missing-monitoring-ca"
		reconciler.Client = fake.NewClientBuilder().WithScheme(scheme).Build()
		_, err = reconciler.telemetryMaterialRevisions(context.Background(), missing)
		Expect(err).To(MatchError(ContainSubstring("could not be read")))
		Expect(monitoringTLSSecretReferenced(newMinimalCR(), secret.Name)).To(BeFalse())
	})

	It("maps referenced monitoring trust objects to the singleton CR", func() {
		scheme := newMonitoringScheme()
		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "monitoring-ca", Key: "bundle.pem"},
		}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca", Namespace: testNamespace}}
		reconciler := &KubernautReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(kn, secret).Build(),
		}
		expected := []reconcile.Request{{NamespacedName: types.NamespacedName{Name: kn.Name, Namespace: kn.Namespace}}}
		Expect(reconciler.telemetrySecretToKubernaut(context.Background(), secret)).To(Equal(expected))

		Expect(reconciler.telemetryAmbientConfigMapToKubernaut(context.Background(), &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: testNamespace},
		})).To(BeEmpty())
	})

	It("stamps only monitoring consumers with the monitoring material revision", func() {
		revisions := telemetryMaterialRevisionSet{Monitoring: "monitoring-revision"}
		for _, component := range []string{resources.ComponentEffectivenessMonitor, resources.ComponentKubernautAgent, resources.ComponentAPIFrontend} {
			dep := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": component}},
			}}}
			stampMonitoringMaterialRevision(dep, revisions)
			Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(resources.AnnotationMonitoringMaterialRevision, "monitoring-revision"), component)
		}

		other := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": resources.ComponentGateway}},
		}}}
		stampMonitoringMaterialRevision(other, revisions)
		Expect(other.Spec.Template.Annotations).NotTo(HaveKey(resources.AnnotationMonitoringMaterialRevision))
	})
})

var _ = Describe("monitoring TLS source references", Label("unit"), func() {
	It("resolves only an explicit administrator-owned monitoring Secret", func() {
		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "monitoring-ca", Key: "bundle.pem"},
		}
		kind, name, key, ok := monitoringTLSReference(kn)
		Expect(ok).To(BeTrue())
		Expect(kind).To(Equal(monitoringTLSSecretKind))
		Expect(name).To(Equal("monitoring-ca"))
		Expect(key).To(Equal("bundle.pem"))
	})
})

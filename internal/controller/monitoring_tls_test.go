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

	It("resolves non-sensitive revisions for Secret and ConfigMap sources and none for system trust", func() {
		scheme := newMonitoringScheme()
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca", Namespace: testNamespace, ResourceVersion: "11"}}
		reconciler := &KubernautReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(),
		}
		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			Source:          kubernautv1alpha2.MonitoringTLSSourceSecret,
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

		configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca-config", Namespace: testNamespace, ResourceVersion: "21"}}
		reconciler.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			Source:             kubernautv1alpha2.MonitoringTLSSourceConfigMap,
			CACertConfigMapRef: &kubernautv1alpha2.CACertConfigMapRef{Name: configMap.Name, Key: "bundle.pem"},
		}
		configMapRevision, err := reconciler.monitoringTLSMaterialRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(configMapRevision).To(HaveLen(64))
		kn.Spec.Monitoring.TLS.CACertConfigMapRef.Name = "missing-monitoring-ca-config"
		_, err = reconciler.monitoringTLSMaterialRevision(context.Background(), kn)
		Expect(err).To(MatchError(ContainSubstring("could not be read")))

		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			Source:          kubernautv1alpha2.MonitoringTLSSourceSecret,
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "missing-monitoring-ca"},
		}
		_, err = reconciler.monitoringTLSMaterialRevision(context.Background(), kn)
		Expect(err).To(MatchError(ContainSubstring("could not be read")))

		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{Source: kubernautv1alpha2.MonitoringTLSSourceSystem}
		systemRevision, err := reconciler.monitoringTLSMaterialRevision(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(systemRevision).To(BeEmpty())

		unconfiguredRevision, err := reconciler.monitoringTLSMaterialRevision(context.Background(), newMinimalCR())
		Expect(err).NotTo(HaveOccurred())
		Expect(unconfiguredRevision).To(BeEmpty())
	})

	It("fails closed for nil and incomplete source objects", func() {
		Expect(monitoringTLSConfigured(nil)).To(BeFalse())
		_, _, _, ok := monitoringTLSReference(nil)
		Expect(ok).To(BeFalse())
		_, _, _, ok = monitoringTLSSecretReference(nil)
		Expect(ok).To(BeFalse())
		_, _, _, ok = monitoringTLSConfigMapReference(nil)
		Expect(ok).To(BeFalse())

		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{Source: kubernautv1alpha2.MonitoringTLSSourceConfigMap}
		_, _, _, ok = monitoringTLSReference(kn)
		Expect(ok).To(BeFalse())
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		kind, name, key, ok := monitoringTLSInterServiceReference(kn)
		Expect(ok).To(BeTrue())
		Expect(kind).To(Equal(monitoringTLSConfigMapKind))
		Expect(name).To(Equal(resources.InterServiceCAConfigMapName))
		Expect(key).To(Equal("ca.crt"))
	})

	It("includes the monitoring revision in the shared material resolver", func() {
		scheme := newMonitoringScheme()
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca", Namespace: testNamespace, ResourceVersion: "31"}}
		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			Source:          kubernautv1alpha2.MonitoringTLSSourceSecret,
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
			Source:          kubernautv1alpha2.MonitoringTLSSourceSecret,
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "monitoring-ca"},
		}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca", Namespace: testNamespace}}
		reconciler := &KubernautReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(kn, secret).Build(),
		}
		expected := []reconcile.Request{{NamespacedName: types.NamespacedName{Name: kn.Name, Namespace: kn.Namespace}}}
		Expect(reconciler.telemetrySecretToKubernaut(context.Background(), secret)).To(Equal(expected))

		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			Source:             kubernautv1alpha2.MonitoringTLSSourceConfigMap,
			CACertConfigMapRef: &kubernautv1alpha2.CACertConfigMapRef{Name: "monitoring-ca-config"},
		}
		configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca-config", Namespace: testNamespace}}
		Expect(reconciler.Client.Update(context.Background(), kn)).To(Succeed())
		Expect(reconciler.Client.Create(context.Background(), configMap)).To(Succeed())
		Expect(reconciler.telemetryAmbientConfigMapToKubernaut(context.Background(), configMap)).To(Equal(expected))
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
	It("follows the administrator-owned manual inter-service Secret", func() {
		kn := newMinimalCR()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		kn.Spec.TLS.Manual = &kubernautv1alpha2.ManualTLSConfig{InternalCASecretName: "administrator-ca"}
		kind, name, key, ok := monitoringTLSReference(kn)
		Expect(ok).To(BeTrue())
		Expect(kind).To(Equal("secret"))
		Expect(name).To(Equal("administrator-ca"))
		Expect(key).To(Equal("ca.crt"))
	})

	It("resolves the generated inter-service ConfigMap and rejects unknown sources", func() {
		kn := newMinimalCR()
		kind, name, key, ok := monitoringTLSReference(kn)
		Expect(ok).To(BeTrue())
		Expect(kind).To(Equal(monitoringTLSConfigMapKind))
		Expect(name).To(Equal(resources.TrustBundleConfigMapName))
		Expect(key).To(Equal("service-ca.crt"))

		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{Source: "unknown"}
		_, _, _, ok = monitoringTLSReference(kn)
		Expect(ok).To(BeFalse())
	})
})

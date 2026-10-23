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
	"crypto/x509"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const monitoringPrometheusServiceURL = "https://prometheus.monitoring.svc:9090"

var _ = Describe("generic monitoring defaults", Label("unit"), func() {
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
		kn.Spec.Monitoring.Prometheus.URL = monitoringPrometheusServiceURL
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

var _ = Describe("monitoring TLS trust validation", Label("unit"), func() {
	It("UT-MON-563-VAL-002 [SC-12, SC-13, SI-4, SI-10; SOC2 CC7.2; ASVS V12.3.2, V13.3.4, V16.5.2] rejects missing or malformed Secret CA material", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		reconciler := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
		kn := &kubernautv1alpha2.Kubernaut{}
		kn.Namespace = "default"
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "missing"}}
		Expect(reconciler.validateMonitoringTLS(ctx, kn)).To(MatchError(ContainSubstring("could not be read")))
		kn.Spec.Monitoring.TLS.CACertSecretRef.Name = ""
		Expect(reconciler.validateMonitoringTLS(ctx, kn)).To(MatchError(ContainSubstring("required when configured")))

		bad := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "default"}, Data: map[string][]byte{"ca.crt": []byte("not pem")}}
		Expect(reconciler.Client.Create(ctx, bad)).To(Succeed())
		kn.Spec.Monitoring.TLS.CACertSecretRef.Name = bad.Name
		Expect(reconciler.validateMonitoringTLS(ctx, kn)).To(MatchError(ContainSubstring("valid ca pem")))

		missingKeySecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "missing-key", Namespace: "default"}, Data: map[string][]byte{"other": telemetryCAPEM()}}
		Expect(reconciler.Client.Create(ctx, missingKeySecret)).To(Succeed())
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: missingKeySecret.Name},
		}
		Expect(reconciler.validateMonitoringTLS(ctx, kn)).To(MatchError(ContainSubstring("missing key")))
	})

	It("accepts valid Secret CA material without changing serving TLS", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca", Namespace: "default"}, Data: map[string][]byte{"ca.crt": telemetryCAPEM()}}
		configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca-config", Namespace: "default"}, Data: map[string]string{"ca.crt": string(telemetryCAPEM())}}
		reconciler := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, configMap).Build()}
		kn := &kubernautv1alpha2.Kubernaut{}
		kn.Namespace = "default"
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "monitoring-ca"}}
		Expect(reconciler.validateMonitoringTLS(ctx, kn)).To(Succeed())
		Expect(kn.Spec.TLS.Mode).To(BeEmpty())
	})

	It("rejects non-CA, expired, and trailing malformed certificate material", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())

		_, nonCA := telemetryClientKeyPair()
		expired := telemetrySecretForTemplate(&x509.Certificate{
			SerialNumber:          big.NewInt(42),
			NotBefore:             time.Now().Add(-2 * time.Hour),
			NotAfter:              time.Now().Add(-time.Hour),
			BasicConstraintsValid: true,
			IsCA:                  true,
			KeyUsage:              x509.KeyUsageCertSign,
		})
		cases := []struct {
			name string
			data []byte
			err  string
		}{
			{name: "non-ca", data: nonCA, err: "non-ca certificate"},
			{name: "expired", data: expired.Data[corev1.TLSCertKey], err: "expired"},
			{name: "trailing malformed", data: append(telemetryCAPEM(), []byte("trailing")...), err: "non-pem data"},
		}

		for _, tc := range cases {
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tc.name, Namespace: "default"}, Data: map[string][]byte{"ca.crt": tc.data}}
			reconciler := &KubernautReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()}
			kn := &kubernautv1alpha2.Kubernaut{ObjectMeta: metav1.ObjectMeta{Namespace: "default"}}
			kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
				CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: tc.name},
			}
			Expect(reconciler.validateMonitoringTLS(ctx, kn)).To(MatchError(ContainSubstring(tc.err)), tc.name)
		}
	})

	It("keeps validation in error when a monitoring Secret is not ready", func() {
		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "missing-monitoring-ca"},
		}
		postgresSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: kn.Spec.PostgreSQL.SecretName, Namespace: kn.Namespace},
			Data: map[string][]byte{
				"POSTGRES_USER":     []byte("kubernaut"),
				"POSTGRES_PASSWORD": []byte("secret"),
				"POSTGRES_DB":       []byte("kubernaut"),
			},
		}
		valkeySecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: kn.Spec.Valkey.SecretName, Namespace: kn.Namespace},
			Data:       map[string][]byte{"valkey-secrets.yaml": []byte("password: secret")},
		}
		r := &KubernautReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&kubernautv1alpha2.Kubernaut{}).WithObjects(kn, postgresSecret, valkeySecret).Build(),
			Scheme: scheme,
		}
		seedValidPrecreatedInputs(r, kn)

		result, err := r.phaseValidate(ctx, kn, kn.DeepCopy())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
		condition := meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Reason).To(Equal(ReasonMonitoringTLSNotReady))
	})
})

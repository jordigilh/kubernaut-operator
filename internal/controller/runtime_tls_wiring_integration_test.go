package controller

import (
	"context"
	"encoding/pem"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// Unlike the source-decision unit fixtures, every operation here uses envtest's
// API server and enters the production Reconcile path.
var _ = Describe("runtime TLS real-API reconciliation wiring", func() {
	BeforeEach(func() {
		createBYOSecrets(ctx)
		for _, object := range []client.Object{
			&admissionv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: testNamespace + "-authwebhook-mutating"}},
			&admissionv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: testNamespace + "-authwebhook-validating"}},
		} {
			deleteErr := k8sClient.Delete(ctx, object)
			Expect(deleteErr == nil || apierrors.IsNotFound(deleteErr)).To(BeTrue())
		}
	})
	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
		for _, object := range []client.Object{
			&admissionv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: testNamespace + "-authwebhook-mutating"}},
			&admissionv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: testNamespace + "-authwebhook-validating"}},
		} {
			deleteErr := k8sClient.Delete(ctx, object)
			Expect(deleteErr == nil || apierrors.IsNotFound(deleteErr)).To(BeTrue())
		}
	})

	It("IT-TELEMETRY-AUDIT-515-001: blocks malformed client chains through Reconcile and resumes only after valid external material is restored", func() {
		kn := newCRWithRouteDisabled()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeHook
		kn.Spec.DataStorage.Telemetry = telemetrySecretSpec()
		ca := telemetryCASecret("telemetry-ca", "ca.crt")
		clientSecret := telemetryClientSecret()
		ca.Namespace, clientSecret.Namespace = kn.Namespace, kn.Namespace
		validClientChain := append([]byte(nil), clientSecret.Data[corev1.TLSCertKey]...)
		clientSecret.Data[corev1.TLSCertKey] = append(clientSecret.Data[corev1.TLSCertKey],
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid trailing DER")})...)
		for _, secret := range []*corev1.Secret{ca, clientSecret} {
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func() {
				deleteErr := k8sClient.Delete(context.Background(), secret)
				Expect(deleteErr == nil || apierrors.IsNotFound(deleteErr)).To(BeTrue())
			})
		}
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := newReconciler()
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
		condition := meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonTelemetrySecretInvalid))
		Expect(condition.Message).To(ContainSubstring("invalid client certificate"))
		jobs := &batchv1.JobList{}
		Expect(k8sClient.List(ctx, jobs, client.InNamespace(kn.Namespace))).To(Succeed())
		Expect(jobs.Items).To(BeEmpty(), "invalid client material must block migration, not only report a warning")
		clientSecret.Data[corev1.TLSCertKey] = validClientChain
		Expect(k8sClient.Update(ctx, clientSecret)).To(Succeed())
		reconcileToRunning(ctx)
		Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
		Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseRunning))
		expected := []reconcile.Request{{NamespacedName: singletonKey()}}
		Expect(r.telemetrySecretToKubernaut(ctx, clientSecret)).To(Equal(expected))
		Expect(r.telemetrySecretToKubernaut(ctx, ca)).To(Equal(expected))
		Expect(r.telemetrySecretToKubernaut(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: kn.Namespace, Name: "unrelated"}})).To(BeEmpty())
		kn.Spec.Monitoring.Prometheus.Enabled = ptr.To(true)
		kn.Spec.Monitoring.Prometheus.URL = monitoringPrometheusServiceURL
		kn.Spec.Monitoring.AlertManager.Enabled = ptr.To(false)
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		Expect(k8sClient.Update(ctx, kn)).To(Succeed())
		trust := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.InterServiceCAConfigMapName}, trust)).To(Succeed())
		Expect(r.telemetryAmbientConfigMapToKubernaut(ctx, trust)).To(Equal(expected))
		Expect(r.monitoringObjectToKubernaut(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: "monitoring"}})).To(Equal(expected))
		Expect(r.monitoringObjectToKubernaut(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "monitoring"}})).To(BeEmpty())
	})

	for _, mode := range []kubernautv1alpha2.TLSMode{kubernautv1alpha2.TLSModeHook, kubernautv1alpha2.TLSModeDevelopmentSelfSigned} {
		It("IT-TLS-AUDIT-515-001: persists development trust, serving material and webhook bundles through Reconcile for "+string(mode), func() {
			kn := newCRWithRouteDisabled()
			kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: mode,
				DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{}}
			Expect(k8sClient.Create(ctx, kn)).To(Succeed())
			r := reconcileToRunning(ctx)
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseRunning))
			Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionTLSReady)).To(BeTrue())
			ca := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: "kubernaut-internal-ca"}, ca)).To(Succeed())
			serving := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.GatewayTLSSecretName}, serving)).To(Succeed())
			Expect(resources.ValidateServingTLSSecretForService(serving, ca, resources.TLSServiceGateway, kn.Namespace)).To(Succeed())
			Expect(serving.OwnerReferences).To(ContainElement(HaveField("UID", kn.UID)))
			trust := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.TrustBundleConfigMapName}, trust)).To(Succeed())
			Expect(trust.Data["ca.crt"]).To(Equal(string(ca.Data["ca.crt"])))
			mutating := &admissionv1.MutatingWebhookConfiguration{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: kn.Namespace + "-authwebhook-mutating"}, mutating)).To(Succeed())
			Expect(mutating.Webhooks[0].ClientConfig.CABundle).To(Equal(ca.Data["ca.crt"]))
			oldLeaf := append([]byte(nil), serving.Data[corev1.TLSCertKey]...)
			r.now = func() time.Time { return time.Now().Add(29 * 24 * time.Hour) }
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(serving), serving)).To(Succeed())
			Expect(serving.Data[corev1.TLSCertKey]).NotTo(Equal(oldLeaf), "leaf rotation must remain wired after initial readiness")
		})
	}

	for _, mode := range []kubernautv1alpha2.TLSMode{kubernautv1alpha2.TLSModeManual, kubernautv1alpha2.TLSModeAdministratorManaged} {
		It("IT-TLS-AUDIT-515-002: validates and mounts external TLS without adopting its trust or Secrets for "+string(mode), func() {
			kn := newCRWithRouteDisabled()
			generator := kn.DeepCopy()
			generator.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeHook}
			material, err := resources.DevelopmentSelfSignedTLSSecrets(generator, nil, time.Now().Add(-time.Hour))
			Expect(err).NotTo(HaveOccurred())
			for _, secret := range material {
				secret.Labels, secret.OwnerReferences = nil, nil
				Expect(k8sClient.Create(ctx, secret)).To(Succeed())
				DeferCleanup(func() {
					deleteErr := k8sClient.Delete(context.Background(), secret)
					Expect(deleteErr == nil || apierrors.IsNotFound(deleteErr)).To(BeTrue())
				})
			}
			kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: mode}
			if mode == kubernautv1alpha2.TLSModeAdministratorManaged {
				kn.Spec.TLS.AdministratorManaged = &kubernautv1alpha2.AdministratorManagedTLSConfig{
					InternalCASecretName: material[0].Name, ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
				}
			}
			trust := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: resources.InterServiceCAConfigMapName, Namespace: kn.Namespace},
				Data: map[string]string{"ca.crt": string(material[0].Data["ca.crt"]), "administrator-key": "preserved"}}
			Expect(k8sClient.Create(ctx, trust)).To(Succeed())
			DeferCleanup(func() { Expect(k8sClient.Delete(context.Background(), trust)).To(Succeed()) })
			for _, object := range []client.Object{
				resources.MutatingWebhookConfigurationWithCABundle(kn, material[0].Data["ca.crt"]),
				resources.ValidatingWebhookConfigurationWithCABundle(kn, material[0].Data["ca.crt"]),
			} {
				Expect(k8sClient.Create(ctx, object)).To(Succeed())
			}
			Expect(k8sClient.Create(ctx, kn)).To(Succeed())
			reconcileToRunning(ctx)
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseRunning))
			liveTrust := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(trust), liveTrust)).To(Succeed())
			Expect(liveTrust.Data).To(Equal(trust.Data))
			Expect(liveTrust.OwnerReferences).To(BeEmpty())
			for _, secret := range material {
				live := &corev1.Secret{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), live)).To(Succeed())
				Expect(live.Data).To(Equal(secret.Data))
				Expect(live.OwnerReferences).To(BeEmpty())
			}
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.ComponentGateway}, deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Volumes).To(ContainElement(HaveField("Secret", PointTo(HaveField("SecretName", resources.GatewayTLSSecretName)))))
		})
	}
})

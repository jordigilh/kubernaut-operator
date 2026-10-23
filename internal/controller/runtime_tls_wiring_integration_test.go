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
		kn := newMinimalCR()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		kn.Spec.TLS.Manual = &kubernautv1alpha2.ManualTLSConfig{
			InternalCASecretName:  "kubernaut-internal-ca",
			ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
		}
		runtimeSecrets, err := testRuntimeTLSSecrets(kn, nil, time.Now().Add(-time.Hour))
		Expect(err).NotTo(HaveOccurred())
		for _, secret := range runtimeSecrets {
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func() {
				deleteErr := k8sClient.Delete(context.Background(), secret)
				Expect(deleteErr == nil || apierrors.IsNotFound(deleteErr)).To(BeTrue())
			})
		}
		for _, webhook := range []client.Object{
			resources.MutatingWebhookConfigurationWithCABundle(kn, runtimeSecrets[0].Data["ca.crt"]),
			resources.ValidatingWebhookConfigurationWithCABundle(kn, runtimeSecrets[0].Data["ca.crt"]),
		} {
			Expect(k8sClient.Create(ctx, webhook)).To(Succeed())
		}
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
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
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
		trustFixture := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: resources.InterServiceCAConfigMapName, Namespace: kn.Namespace},
			Data:       map[string]string{"ca.crt": string(runtimeSecrets[0].Data["ca.crt"])},
		}
		Expect(k8sClient.Create(ctx, trustFixture)).To(Succeed())
		DeferCleanup(func() {
			deleteErr := k8sClient.Delete(context.Background(), trustFixture)
			Expect(deleteErr == nil || apierrors.IsNotFound(deleteErr)).To(BeTrue())
		})
		trust := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.InterServiceCAConfigMapName}, trust)).To(Succeed())
		Expect(r.telemetryAmbientConfigMapToKubernaut(ctx, trust)).To(BeEmpty())
		Expect(r.monitoringObjectToKubernaut(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: "monitoring"}})).To(BeEmpty())
		Expect(r.monitoringObjectToKubernaut(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "monitoring"}})).To(BeEmpty())
	})

	for _, mode := range []kubernautv1alpha2.TLSMode{kubernautv1alpha2.TLSModeManual} {
		It("IT-TLS-AUDIT-515-001 IT-TLS-AUDIT-515-002: validates and mounts external TLS without adopting its trust or Secrets for "+string(mode), func() {
			kn := newMinimalCR()
			generator := kn.DeepCopy()
			generator.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeManual}
			material, err := testRuntimeTLSSecrets(generator, nil, time.Now().Add(-time.Hour))
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
			if mode == kubernautv1alpha2.TLSModeManual {
				kn.Spec.TLS.Manual = &kubernautv1alpha2.ManualTLSConfig{
					InternalCASecretName: material[0].Name, ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
				}
			}
			trust := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: resources.InterServiceCAConfigMapName, Namespace: kn.Namespace},
				Data: map[string]string{"ca.crt": string(material[0].Data["ca.crt"]), "administrator-key": "preserved"}}
			Expect(k8sClient.Create(ctx, trust)).To(Succeed())
			DeferCleanup(func() {
				deleteErr := k8sClient.Delete(context.Background(), trust)
				Expect(deleteErr == nil || apierrors.IsNotFound(deleteErr)).To(BeTrue())
			})
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

	It("IT-MON-563-001 [AC-6, SC-8, SC-12, SC-13, SI-4; SOC2 CC6.1, CC7.2; ASVS V12.3.1, V12.3.2, V13.3.1, V13.3.2]: validates and mounts a Secret-backed monitoring CA through Reconcile", func() {
		monitoringCA := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca", Namespace: testNamespace},
			Data:       map[string][]byte{"ca.crt": telemetryCAPEM()},
		}
		Expect(k8sClient.Create(ctx, monitoringCA)).To(Succeed())
		DeferCleanup(func() {
			deleteErr := k8sClient.Delete(context.Background(), monitoringCA)
			Expect(deleteErr == nil || apierrors.IsNotFound(deleteErr)).To(BeTrue())
		})

		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: monitoringCA.Name},
		}
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		reconcileToRunning(ctx)

		for _, component := range []string{resources.ComponentEffectivenessMonitor, resources.ComponentKubernautAgent, resources.ComponentAPIFrontend} {
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.DeploymentName(component)}, deployment)).To(Succeed(), component)
			Expect(deployment.Spec.Template.Annotations).To(HaveKey(resources.AnnotationMonitoringMaterialRevision), component)
			volume := findMonitoringDeploymentVolume(deployment)
			Expect(volume).NotTo(BeNil(), component)
			Expect(volume.Secret).NotTo(BeNil(), component)
			Expect(volume.Secret.SecretName).To(Equal(monitoringCA.Name), component)
			var mounted bool
			for _, container := range deployment.Spec.Template.Spec.Containers {
				for _, mount := range container.VolumeMounts {
					if mount.Name != resources.MonitoringTLSCAVolumeName {
						continue
					}
					mounted = true
					Expect(mount.MountPath).To(Equal(resources.MonitoringTLSCADir), component)
					Expect(mount.ReadOnly).To(BeTrue(), component)
				}
			}
			Expect(mounted).To(BeTrue(), component)
		}
	})

	It("IT-MON-563-002 [SC-8, SC-13, SC-17, SI-4; SOC2 CC6.1, CC7.2; ASVS V12.3.1, V12.3.2, V13.3.4]: uses system trust without a monitoring CA volume", func() {
		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = nil
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		reconcileToRunning(ctx)

		for _, component := range []string{resources.ComponentEffectivenessMonitor, resources.ComponentKubernautAgent, resources.ComponentAPIFrontend} {
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.DeploymentName(component)}, deployment)).To(Succeed(), component)
			Expect(findMonitoringDeploymentVolume(deployment)).To(BeNil(), component)
			Expect(deployment.Spec.Template.Annotations).NotTo(HaveKey(resources.AnnotationMonitoringMaterialRevision), component)
		}
		for _, name := range []string{"effectivenessmonitor-config", "kubernaut-agent-config", "apifrontend-config"} {
			configMap := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: name}, configMap)).To(Succeed(), name)
			Expect(configMap.Data).NotTo(ContainElement(ContainSubstring(resources.MonitoringTLSCAFile)), name)
			if name == "effectivenessmonitor-config" {
				Expect(configMap.Data["effectivenessmonitor.yaml"]).To(ContainSubstring("tlsCaFile: /etc/tls-ca/service-ca.crt"), name)
			}
		}
	})

	It("IT-MON-563-003 [AC-6, CM-3, SC-8, SC-12, SC-13, SI-4; SOC2 CC7.2, CC8.1; ASVS V12.3.1, V12.3.2, V13.3.4, V16.5.2]: preserves last-known-good Deployments on invalid rotation and rolls all consumers after valid rotation", func() {
		monitoringCA := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "monitoring-ca", Namespace: testNamespace},
			Data:       map[string][]byte{"ca.crt": telemetryCAPEM()},
		}
		Expect(k8sClient.Create(ctx, monitoringCA)).To(Succeed())
		DeferCleanup(func() {
			deleteErr := k8sClient.Delete(context.Background(), monitoringCA)
			Expect(deleteErr == nil || apierrors.IsNotFound(deleteErr)).To(BeTrue())
		})

		kn := newMinimalCR()
		kn.Spec.Monitoring.TLS = &kubernautv1alpha2.MonitoringTLSSpec{
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: monitoringCA.Name},
		}
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := reconcileToRunning(ctx)
		before := monitoringDeploymentRevisions(ctx)

		liveCA := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(monitoringCA), liveCA)).To(Succeed())
		liveCA.Data["ca.crt"] = []byte("not a certificate")
		Expect(k8sClient.Update(ctx, liveCA)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		Expect(monitoringDeploymentRevisions(ctx)).To(Equal(before), "invalid monitoring CA rotation must not alter last-known-good Deployment templates")
		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		Expect(findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated).Reason).To(Equal(ReasonMonitoringTLSNotReady))

		liveCA.Data["ca.crt"] = telemetryCAPEM()
		Expect(k8sClient.Update(ctx, liveCA)).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		after := monitoringDeploymentRevisions(ctx)
		for _, component := range []string{resources.ComponentEffectivenessMonitor, resources.ComponentKubernautAgent, resources.ComponentAPIFrontend} {
			Expect(after[component]).NotTo(Equal(before[component]), component)
		}
		expected := []reconcile.Request{{NamespacedName: singletonKey()}}
		Expect(r.telemetrySecretToKubernaut(ctx, liveCA)).To(Equal(expected))
	})
})

func findMonitoringDeploymentVolume(deployment *appsv1.Deployment) *corev1.Volume {
	for index := range deployment.Spec.Template.Spec.Volumes {
		volume := &deployment.Spec.Template.Spec.Volumes[index]
		if volume.Name == resources.MonitoringTLSCAVolumeName {
			return volume
		}
	}
	return nil
}

func monitoringDeploymentRevisions(ctx context.Context) map[string]string {
	revisions := make(map[string]string, 3)
	for _, component := range []string{resources.ComponentEffectivenessMonitor, resources.ComponentKubernautAgent, resources.ComponentAPIFrontend} {
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: resources.DeploymentName(component)}, deployment)).To(Succeed(), component)
		revisions[component] = deployment.Spec.Template.Annotations[resources.AnnotationMonitoringMaterialRevision]
	}
	return revisions
}

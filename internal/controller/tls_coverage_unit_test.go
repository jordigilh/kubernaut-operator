package controller

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("TLS source coverage boundaries", Label("unit"), func() {
	It("covers manual and reference-only cert-manager webhook rendering", func() {
		kn := newMinimalCR()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		manual := resources.TLSMaterial{Source: resources.TLSMaterialSourceManual}
		Expect(mutatingWebhookConfigurationForTLS(kn, manual, nil)).NotTo(BeNil())
		Expect(validatingWebhookConfigurationForTLS(kn, manual, nil)).NotTo(BeNil())

		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeCertManager
		kn.Spec.TLS.CertManager = &kubernautv1alpha2.CertManagerTLSConfig{
			IssuerRef:             kubernautv1alpha2.TLSIssuerRef{Name: "issuer"},
			InternalCASecretName:  "ca",
			ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
			Provisioning:          &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: ptr.To(false)},
		}
		certManager := resources.TLSMaterial{Source: resources.TLSMaterialSourceCertManager}
		Expect(mutatingWebhookConfigurationForTLS(kn, certManager, []byte("bundle"))).NotTo(BeNil())
		Expect(validatingWebhookConfigurationForTLS(kn, certManager, []byte("bundle"))).NotTo(BeNil())
	})

	It("covers reference-only issuer validation and not-ready certificate status", func() {
		kn := certManagerLifecycleCR("Issuer")
		if kn.Spec.TLS.CertManager.Provisioning == nil {
			kn.Spec.TLS.CertManager.Provisioning = &kubernautv1alpha2.CertManagerTLSProvisioning{}
		}
		kn.Spec.TLS.CertManager.Provisioning.Enabled = ptr.To(false)
		kn.Spec.TLS.CertManager.IssuerRef.Name = "customer-issuer"
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		for _, crd := range certManagerLifecycleCRDs() {
			Expect(r.Create(ctx, crd)).To(Succeed())
		}
		issuer := certManagerExternalLifecycleIssuer(kn)
		issuer.SetName("customer-issuer")
		Expect(r.Create(ctx, issuer)).To(Succeed())
		Expect(r.validateCertManagerIssuer(ctx, kn)).To(Succeed())
		Expect(r.validateCertManagerTLS(ctx, kn)).To(Succeed())

		provisioned := kn.DeepCopy()
		provisioned.Spec.TLS.CertManager.Provisioning.Enabled = ptr.To(true)
		objects, err := resources.CertManagerTLSResources(provisioned)
		Expect(err).NotTo(HaveOccurred())
		var certificate *unstructured.Unstructured
		for _, object := range objects {
			if object.GetKind() == "Certificate" {
				certificate = object.DeepCopy()
				break
			}
		}
		Expect(certificate).NotTo(BeNil())
		certificate.SetResourceVersion("")
		certificate.SetUID("")
		certificate.SetManagedFields(nil)
		certificate.Object["status"] = map[string]interface{}{"conditions": []interface{}{
			map[string]interface{}{"type": "Ready", "status": "False", "reason": "Pending", "message": "waiting"},
		}}
		Expect(r.Create(ctx, certificate)).To(Succeed())
		Expect(r.validateCertManagerCertificatesReady(ctx, provisioned)).To(MatchError(ContainSubstring("Pending: waiting")))
		desired := certificate.DeepCopy()
		desired.SetName("coverage-certificate")
		desired.SetResourceVersion("")
		desired.SetUID("")
		desired.SetManagedFields(nil)
		Expect(r.ensureCertManagerResource(ctx, kn, desired)).To(Succeed())
		desired.Object["spec"].(map[string]interface{})["duration"] = "8761h"
		Expect(r.ensureCertManagerResource(ctx, kn, desired)).To(Succeed())
	})

	It("defaults cert-manager issuer references to ClusterIssuer", func() {
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				IssuerRef:    kubernautv1alpha2.TLSIssuerRef{Name: "cluster-issuer"},
				Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: ptr.To(false)},
			},
		}
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.Create(ctx, certManagerLifecycleCRDs()[2])).To(Succeed())
		issuer := &unstructured.Unstructured{}
		issuer.SetAPIVersion("cert-manager.io/v1")
		issuer.SetKind("ClusterIssuer")
		issuer.SetName("cluster-issuer")
		Expect(r.Create(ctx, issuer)).To(Succeed())
		resolved, err := r.resolveCertManagerIssuer(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved.kind).To(Equal("ClusterIssuer"))
	})

	It("covers empty generic trust and no-signing/service-ca paths", func() {
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				IssuerRef:             kubernautv1alpha2.TLSIssuerRef{Name: "issuer"},
				InternalCASecretName:  "ca",
				ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
				Provisioning:          &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: ptr.To(false)},
			},
		}
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.ensureGenericTLSConfigMaps(ctx, kn, nil)).To(MatchError(ContainSubstring("CA bundle is empty")))
		Expect(r.ensureGenericTLSConfigMaps(ctx, kn, []byte("ca"))).To(Succeed())
		Expect(r.ensureTLSConfigMapsForMaterial(ctx, kn, resources.TLSMaterial{Source: resources.TLSMaterialSourceCertManager}, []byte("ca"))).To(Succeed())
		Expect(r.ensureTLSConfigMapsForMaterial(ctx, kn, resources.TLSMaterial{Source: resources.TLSMaterialSourceServiceCA}, nil)).To(Succeed())
		failing := businessUnitReconciler(kn, interceptor.Funcs{Create: deniedUnitCreate})
		Expect(failing.ensureGenericTLSConfigMaps(ctx, kn, []byte("ca"))).To(HaveOccurred())
		Expect(failing.ensureTLSConfigMapsForMaterial(ctx, kn, resources.TLSMaterial{Source: resources.TLSMaterialSourceCertManager}, []byte("ca"))).To(HaveOccurred())
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeServiceCA
		Expect(resources.DataStorageSigningSecretName(kn)).To(BeEmpty())
		Expect(r.validateTLSWebhookReadiness(ctx, kn, resources.TLSMaterial{Source: resources.TLSMaterialSourceServiceCA})).To(Succeed())
		Expect(r.validateDataStorageSigningCertificate(ctx, kn)).To(Succeed())
	})

	It("covers generic telemetry trust publication", func() {
		kn := newMinimalCR()
		kn.Spec.DataStorage.Telemetry.Endpoint = "otel-collector:4317"
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				IssuerRef:             kubernautv1alpha2.TLSIssuerRef{Name: "issuer"},
				InternalCASecretName:  "telemetry-ca",
				ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
				Provisioning:          &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: ptr.To(false)},
			},
		}
		ca := telemetryCASecret("telemetry-ca", "ca.crt")
		ca.Namespace = kn.Namespace
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.Create(ctx, ca)).To(Succeed())
		Expect(r.ensureTelemetryAmbientTrustSource(ctx, kn)).To(Succeed())
		failing := businessUnitReconciler(kn, interceptor.Funcs{})
		seedCA := ca.DeepCopy()
		seedCA.SetResourceVersion("")
		seedCA.SetUID("")
		seedCA.SetManagedFields(nil)
		Expect(failing.Create(ctx, seedCA)).To(Succeed())
		failing.Client = interceptor.NewClient(failing.Client.(client.WithWatch), interceptor.Funcs{Create: deniedUnitCreate})
		Expect(failing.ensureTelemetryAmbientTrustSource(ctx, kn)).To(HaveOccurred())
	})

	It("covers phase-level generic trust failures", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		r.ensureTLSConfigMapsOverride = func(context.Context, *kubernautv1alpha2.Kubernaut, resources.TLSMaterial, []byte) error {
			return errors.New("injected TLS ConfigMap failure")
		}
		Expect(r.ensureMigrationPrereqs(ctx, kn)).To(MatchError(ContainSubstring("generic TLS trust")))

		kn = newMinimalCR()
		r = businessUnitReconciler(kn, interceptor.Funcs{})
		r.ensureTLSConfigMapsOverride = func(context.Context, *kubernautv1alpha2.Kubernaut, resources.TLSMaterial, []byte) error {
			return errors.New("injected TLS ConfigMap failure")
		}
		Expect(r.phaseDeploy(ctx, kn, kn)).To(MatchError("injected TLS ConfigMap failure"))
	})

	It("covers current-time injection", func() {
		r := &KubernautReconciler{}
		before := r.currentTime()
		Expect(before).NotTo(BeZero())
		r.now = func() (value time.Time) { return time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC) }
		Expect(r.currentTime()).To(Equal(time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC)))
	})
})

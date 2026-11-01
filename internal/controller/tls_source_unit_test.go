package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("runtime TLS source decisions", Label("unit"), func() {
	It("accepts manual material without assigning ownership", func() {
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeManual,
			Manual: &kubernautv1alpha2.ManualTLSConfig{
				InternalCASecretName:  "administrator-internal-ca",
				ServiceTLSSecretNames: testServiceTLSSecretNames(),
			},
		}

		material, err := resources.ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(resources.TLSMaterialSourceManual))
		Expect(material.OwnsSecrets).To(BeFalse())
	})

	It("accepts cert-manager references without enabling provisioning", func() {
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				IssuerRef:             kubernautv1alpha2.TLSIssuerRef{Name: "customer-issuer"},
				InternalCASecretName:  "customer-ca",
				ServiceTLSSecretNames: testServiceTLSSecretNames(),
				Provisioning:          &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: ptr.To(false)},
			},
		}

		Expect(resources.CertManagerTLSProvisioningEnabled(kn)).To(BeFalse())
		material, err := resources.ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(resources.TLSMaterialSourceCertManager))
	})

	It("resolves service-ca only as a platform source", func() {
		for _, mode := range []kubernautv1alpha2.TLSMode{"", kubernautv1alpha2.TLSModeServiceCA} {
			kn := newMinimalCR()
			kn.Spec.TLS.Mode = mode
			material, err := resources.ResolveTLSMaterial(kn)
			Expect(err).NotTo(HaveOccurred(), mode)
			Expect(material.Source).To(Equal(resources.TLSMaterialSourceServiceCA), mode)
		}
	})

	It("rejects removed TLS source values", func() {
		for _, mode := range []kubernautv1alpha2.TLSMode{"hook", "DevelopmentSelfSigned", "AdministratorManaged", "CertManager"} {
			kn := newMinimalCR()
			kn.Spec.TLS.Mode = mode
			_, err := resources.ResolveTLSMaterial(kn)
			Expect(err).To(MatchError(ContainSubstring("unsupported")), mode)
		}
	})

	It("uses explicit mode validation before deployment", func() {
		kn := newMinimalCR()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSMode("unsupported")
		r := newReconcilerWithCRDScheme()
		_, err := r.validateTLSConfiguration(context.Background(), kn)
		Expect(err).To(MatchError(ContainSubstring("unsupported")))
	})
})

func testServiceTLSSecretNames() map[string]string {
	return map[string]string{
		resources.TLSServiceGateway:        "gateway-tls",
		resources.TLSServiceDataStorage:    "datastorage-tls",
		resources.TLSServiceKubernautAgent: "kubernautagent-tls",
		resources.TLSServiceAPIFrontend:    "apifrontend-tls",
		resources.TLSServiceAuthWebhook:    "authwebhook-tls",
	}
}

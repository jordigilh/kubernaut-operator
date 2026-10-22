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

package resources

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("runtime TLS sources", func() {
	It("rejects a nil Kubernaut", func() {
		_, err := ResolveTLSMaterial(nil)
		Expect(err).To(MatchError("kubernaut is required"))
	})

	It("covers cert-manager reference validation failures", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeCertManager}
		_, err := ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("issuer.name")))

		kn.Spec.TLS.CertManager = &kubernautv1alpha2.CertManagerTLSConfig{
			Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: ptr.To(false)},
		}
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("issuer.name")))
		kn.Spec.TLS.CertManager.IssuerRef.Name = "issuer"
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("serviceTLSSecretNames")))
		kn.Spec.TLS.CertManager.ServiceTLSSecretNames = validServiceTLSSecretNames()
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("internalCASecretName")))
	})
	It("UT-TLS-491-001 UT-TLS-MANUAL-001 resolves manual material without taking ownership", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeManual,
			Manual: &kubernautv1alpha2.ManualTLSConfig{
				InternalCASecretName:  "customer-ca",
				ServiceTLSSecretNames: validServiceTLSSecretNames(),
			},
		}

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceManual))
		Expect(material.InternalCASecretName).To(Equal("customer-ca"))
		Expect(material.OwnsSecrets).To(BeFalse())
	})

	It("uses stable manual names when explicit references are omitted", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceManual))
		Expect(material.InternalCAConfigMapName).To(Equal(InterServiceCAConfigMapName))
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceGateway, GatewayTLSSecretName))
		Expect(material.OwnsSecrets).To(BeFalse())
	})

	It("resolves cert-manager output without enabling operator provisioning", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				IssuerRef:             kubernautv1alpha2.TLSIssuerRef{Name: "customer-issuer"},
				InternalCASecretName:  "customer-ca",
				ServiceTLSSecretNames: validServiceTLSSecretNames(),
				Provisioning:          &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: ptr.To(false)},
			},
		}

		Expect(CertManagerTLSProvisioningEnabled(kn)).To(BeFalse())
		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceCertManager))
		Expect(material.InternalCASecretName).To(Equal("customer-ca"))
		Expect(material.OwnsSecrets).To(BeFalse())
	})

	It("UT-TLS-GAP-002 resolves explicit and implicit service-ca mode to the platform source", func() {
		for _, mode := range []kubernautv1alpha2.TLSMode{"", kubernautv1alpha2.TLSModeServiceCA} {
			kn := testKubernaut()
			kn.Spec.TLS.Mode = mode

			material, err := ResolveTLSMaterial(kn)
			Expect(err).NotTo(HaveOccurred(), mode)
			Expect(material.Source).To(Equal(TLSMaterialSourceServiceCA), mode)
			Expect(material.OwnsSecrets).To(BeFalse(), mode)
		}
	})

	It("UT-TLS-GAP-001 rejects removed TLS source values", func() {
		for _, mode := range []kubernautv1alpha2.TLSMode{
			"hook", "DevelopmentSelfSigned", "AdministratorManaged", "CertManager",
		} {
			kn := testKubernaut()
			kn.Spec.TLS.Mode = mode
			_, err := ResolveTLSMaterial(kn)
			Expect(err).To(MatchError(ContainSubstring("unsupported")), mode)
		}
	})
})

func validServiceTLSSecretNames() map[string]string {
	return map[string]string{
		TLSServiceGateway:        "gateway-tls",
		TLSServiceDataStorage:    "datastorage-tls",
		TLSServiceKubernautAgent: "kubernautagent-tls",
		TLSServiceAPIFrontend:    "apifrontend-tls",
		TLSServiceAuthWebhook:    "authwebhook-tls",
	}
}

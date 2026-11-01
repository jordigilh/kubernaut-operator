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

var _ = Describe("Issue #561 TLS source modes", func() {
	It("resolves the explicit service-ca mode to the platform source", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSMode("service-ca")

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal("ServiceCA"))
		Expect(material.OwnsSecrets).To(BeFalse())
	})

	It("treats cert-manager as reference-only unless provisioning is explicit", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSMode("cert-manager")
		kn.Spec.TLS.CertManager = &kubernautv1alpha2.CertManagerTLSConfig{
			IssuerRef:             kubernautv1alpha2.TLSIssuerRef{Name: "customer-issuer"},
			InternalCASecretName:  "customer-ca",
			ServiceTLSSecretNames: validServiceTLSSecretNames(),
			Provisioning:          &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: ptr.To(false)},
		}

		Expect(CertManagerTLSProvisioningEnabled(kn)).To(BeFalse())
		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceCertManager))
		Expect(material.InternalCASecretName).To(Equal("customer-ca"))
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceGateway, "gateway-tls"))
	})

	It("rejects the removed operator-generated self-signed mode", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSMode("DevelopmentSelfSigned")

		_, err := ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("unsupported")))
	})

	It("keeps manual runtime material administrator-owned", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		kn.Spec.TLS.Manual = &kubernautv1alpha2.ManualTLSConfig{
			InternalCASecretName:  "customer-ca",
			ServiceTLSSecretNames: validServiceTLSSecretNames(),
		}

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceManual))
		Expect(material.OwnsSecrets).To(BeFalse())
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceGateway, "gateway-tls"))
	})
})

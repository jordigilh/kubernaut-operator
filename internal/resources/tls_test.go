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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("runtime TLS source", func() {
	It("resolves administrator-managed material without adopting its Secrets", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeAdministratorManaged,
			AdministratorManaged: &kubernautv1alpha2.AdministratorManagedTLSConfig{
				InternalCASecretName:  "customer-ca",
				ServiceTLSSecretNames: validServiceTLSSecretNames(),
			},
		}

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceAdministratorManaged))
		Expect(material.InternalCASecretName).To(Equal("customer-ca"))
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceAuthWebhook, "authwebhook-tls"))
		Expect(material.OwnsSecrets).To(BeFalse())
	})

	It("uses stable generated names for explicit development self-signed mode", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceDevelopmentSelfSigned))
		Expect(material.InternalCASecretName).To(Equal("kubernaut-internal-ca"))
		Expect(material.OwnsSecrets).To(BeTrue())
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceGateway, GatewayTLSSecretName))
	})

	It("rejects a selected source without its required references", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeAdministratorManaged}

		_, err := ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("administratorManaged")))
	})

	It("rejects cert-manager mode without an existing issuer reference", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				InternalCASecretName:  "customer-ca",
				ServiceTLSSecretNames: validServiceTLSSecretNames(),
			},
		}

		_, err := ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("issuer.name")))
	})

	It("keeps the legacy OpenShift source explicit and never calls it a generic source", func() {
		material, err := ResolveTLSMaterial(testKubernaut())
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceOpenShiftServiceCA))
		Expect(material.OwnsSecrets).To(BeFalse())
	})

	It("generates and reuses a complete development CA and serving-certificate set", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}
		now := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)

		first, err := DevelopmentSelfSignedTLSSecrets(kn, nil, now)
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(HaveLen(6))
		existing := make(map[string]*corev1.Secret, len(first))
		for _, secret := range first {
			existing[secret.Name] = secret
			if secret.Name == "kubernaut-internal-ca" {
				Expect(ValidateInternalCASecret(secret)).To(Succeed())
				continue
			}
			Expect(ValidateServingTLSSecret(secret)).To(Succeed())
		}

		second, err := DevelopmentSelfSignedTLSSecrets(kn, existing, now.Add(time.Hour))
		Expect(err).NotTo(HaveOccurred())
		Expect(second).To(HaveLen(len(first)))
		for _, secret := range second {
			Expect(secret.Data).To(Equal(existing[secret.Name].Data), secret.Name)
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

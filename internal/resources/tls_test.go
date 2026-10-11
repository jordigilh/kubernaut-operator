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
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const invalidRotationBefore = "not-a-duration"

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

	It("resolves the administrator-managed FMC serving Secret when FMC is enabled", func() {
		kn := testKubernaut()
		kn.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:    &testFMCEnabled,
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: ComponentFleetMetadataCache},
		}
		serviceNames := validServiceTLSSecretNames()
		serviceNames[TLSServiceFleetMetadataCache] = "customer-fmc-tls"
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeAdministratorManaged,
			AdministratorManaged: &kubernautv1alpha2.AdministratorManagedTLSConfig{
				InternalCASecretName:  "customer-ca",
				ServiceTLSSecretNames: serviceNames,
			},
		}

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceFleetMetadataCache, "customer-fmc-tls"))
	})

	It("generates an FMC serving Secret in development self-signed mode when FMC is enabled", func() {
		kn := testKubernaut()
		kn.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:    &testFMCEnabled,
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: ComponentFleetMetadataCache},
		}
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}

		secrets, err := DevelopmentSelfSignedTLSSecrets(kn, nil, time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC))
		Expect(err).NotTo(HaveOccurred())
		Expect(secretsByName(secrets)).To(HaveKey(FleetMetadataCacheTLSSecretName))
		Expect(TLSServiceDNSNames(TLSServiceFleetMetadataCache, kn.Namespace)).To(ContainElement("fleetmetadatacache-service." + kn.Namespace + ".svc.cluster.local"))
	})

	It("generates and resolves an FMC serving Secret in Helm hook mode when FMC is enabled", func() {
		kn := testKubernaut()
		kn.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:    &testFMCEnabled,
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: ComponentFleetMetadataCache},
		}
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeHook

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceFleetMetadataCache, FleetMetadataCacheTLSSecretName))

		secrets, err := DevelopmentSelfSignedTLSSecrets(kn, nil, time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC))
		Expect(err).NotTo(HaveOccurred())
		byName := secretsByName(secrets)
		Expect(ValidateServingTLSSecretForService(
			byName[FleetMetadataCacheTLSSecretName],
			byName[defaultDevelopmentSelfSignedCASecretName],
			TLSServiceFleetMetadataCache,
			kn.Namespace,
		)).To(Succeed())
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

	It("UT-TLS-GAP-002 rejects unsupported TLS modes instead of falling back to plaintext", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSMode("Plaintext")

		_, err := ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("tls.mode")))
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

	It("UT-TLS-491-001 [SC-8, CM-6; SOC2 CC6, CC8; ASVS v5.0.0-V12.1.1, v5.0.0-V15.2.4] resolves the lower-case Helm cert-manager mode with chart-compatible defaults", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeHelmCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				Issuer: kubernautv1alpha2.TLSIssuerRef{Name: "customer-issuer"},
			},
		}

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceCertManager))
		Expect(material.InternalCASecretName).To(Equal("kubernaut-interservice-ca-secret"))
		Expect(material.ServiceTLSSecretNames).To(HaveKey(TLSServiceGateway))
		Expect(material.ServiceTLSSecretNames).To(HaveKey(TLSServiceDataStorage))
		Expect(material.ServiceTLSSecretNames).To(HaveKey(TLSServiceKubernautAgent))
		Expect(material.ServiceTLSSecretNames).To(HaveKey(TLSServiceAPIFrontend))
		Expect(material.ServiceTLSSecretNames).To(HaveKey(TLSServiceAuthWebhook))
		Expect(CertManagerTLSProvisioningEnabled(kn)).To(BeTrue())
	})

	It("UT-TLS-491-002 [SC-8, SC-12, SC-13; SOC2 CC6, CC7; ASVS v5.0.0-V12.2.1, v5.0.0-V13.2.1] resolves the lower-case Helm hook mode as explicit self-signed TLS", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeHook

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceDevelopmentSelfSigned))
		Expect(material.InternalCASecretName).To(Equal("kubernaut-internal-ca"))
		Expect(material.OwnsSecrets).To(BeTrue())
	})

	It("UT-TLS-491-003 [IA-5, SC-8, SC-12, SC-13, SC-17; SOC2 CC6, CC7; ASVS v5.0.0-V11.1.1, v5.0.0-V12.1.1, v5.0.0-V12.1.3, v5.0.0-V13.2.1] reconciles Helm hook-compatible extra SANs and the separate RSA signing certificate", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeHook,
			Hooks: &kubernautv1alpha2.TLSHooksConfig{TLSCerts: kubernautv1alpha2.TLSCertsConfig{
				ExtraSANs: []string{"localhost", "gateway.example.test"},
			}},
		}

		secrets, err := DevelopmentSelfSignedTLSSecrets(kn, nil, time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC))
		Expect(err).NotTo(HaveOccurred())
		Expect(secrets).To(HaveLen(7))

		byName := secretsByName(secrets)
		gateway, err := parseCertificate(byName[GatewayTLSSecretName].Data[corev1.TLSCertKey])
		Expect(err).NotTo(HaveOccurred())
		_, isECDSA := gateway.PublicKey.(*ecdsa.PublicKey)
		Expect(isECDSA).To(BeTrue(), "Helm hook inter-service leaves must use ECDSA P-256")
		Expect(gateway.VerifyHostname("localhost")).To(Succeed())
		Expect(gateway.VerifyHostname("gateway.example.test")).To(Succeed())
		authwebhook, err := parseCertificate(byName["authwebhook-tls"].Data[corev1.TLSCertKey])
		Expect(err).NotTo(HaveOccurred())
		Expect(authwebhook.VerifyHostname("authwebhook-service." + kn.Namespace + ".svc")).To(Succeed())
		Expect(authwebhook.VerifyHostname("authwebhook." + kn.Namespace + ".svc")).To(Succeed())
		Expect(AuthWebhookCABundle(byName["authwebhook-tls"])).To(Equal(byName["kubernaut-internal-ca"].Data["ca.crt"]))

		signing := byName["datastorage-signing-cert"]
		Expect(signing).NotTo(BeNil())
		signingCertificate, err := parseCertificate(signing.Data[corev1.TLSCertKey])
		Expect(err).NotTo(HaveOccurred())
		Expect(signingCertificate.Subject.CommonName).To(Equal("datastorage-signing-cert"))
		signingKey, err := parseRSAKey(signing.Data[corev1.TLSPrivateKeyKey])
		Expect(err).NotTo(HaveOccurred())
		Expect(signingKey.N.BitLen()).To(Equal(2048))
		Expect(ValidateDataStorageSigningTLSSecret(signing)).To(Succeed())
	})

	It("generates a separate RSA-2048 DataStorage signing certificate in development self-signed mode", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}

		secrets, err := DevelopmentSelfSignedTLSSecrets(kn, nil, time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC))
		Expect(err).NotTo(HaveOccurred())

		byName := secretsByName(secrets)
		Expect(byName).To(HaveKey(defaultCertManagerSigningSecretName))
		Expect(ValidateDataStorageSigningTLSSecret(byName[defaultCertManagerSigningSecretName])).To(Succeed())
	})

	It("keeps the legacy OpenShift source explicit and never calls it a generic source", func() {
		material, err := ResolveTLSMaterial(testKubernaut())
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceOpenShiftServiceCA))
		Expect(material.OwnsSecrets).To(BeFalse())
	})

	It("generates and reuses a complete development CA, serving-certificate, and signing-certificate set", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}
		now := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)

		first, err := DevelopmentSelfSignedTLSSecrets(kn, nil, now)
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(HaveLen(7))
		existing := make(map[string]*corev1.Secret, len(first))
		for _, secret := range first {
			existing[secret.Name] = secret
			if secret.Name == "kubernaut-internal-ca" {
				Expect(ValidateInternalCASecret(secret)).To(Succeed())
				continue
			}
			if secret.Name == defaultCertManagerSigningSecretName {
				Expect(ValidateDataStorageSigningTLSSecret(secret)).To(Succeed())
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

	It("UT-TLS-ROTATION-GAP-001 keeps the previous CA in the trust bundle while rotating the active CA", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}
		initialTime := time.Now().UTC().Add(-359 * 24 * time.Hour)
		initial, err := DevelopmentSelfSignedTLSSecrets(kn, nil, initialTime)
		Expect(err).NotTo(HaveOccurred())

		existing := secretsByName(initial)
		rotated, err := DevelopmentSelfSignedTLSSecrets(kn, existing, initialTime.Add(359*24*time.Hour))
		Expect(err).NotTo(HaveOccurred())
		rotatedByName := secretsByName(rotated)

		caBundle := parseCertificatesForTest(rotatedByName["kubernaut-internal-ca"].Data["ca.crt"])
		Expect(caBundle).To(HaveLen(2), "rotation must overlap the active and previous roots")
		Expect(rotatedByName["kubernaut-internal-ca"].Data["ca.key"]).NotTo(Equal(existing["kubernaut-internal-ca"].Data["ca.key"]))
		Expect(rotatedByName[GatewayTLSSecretName].Data[corev1.TLSCertKey]).NotTo(Equal(existing[GatewayTLSSecretName].Data[corev1.TLSCertKey]))

		oldLeaf, err := parseCertificate(existing[GatewayTLSSecretName].Data[corev1.TLSCertKey])
		Expect(err).NotTo(HaveOccurred())
		Expect(oldLeaf.CheckSignatureFrom(caBundle[1])).To(Succeed(), "the previous leaf's root must remain in the overlap bundle")
		Expect(ValidateServingTLSSecretForService(
			rotatedByName[GatewayTLSSecretName],
			rotatedByName["kubernaut-internal-ca"],
			TLSServiceGateway,
			kn.Namespace,
		)).To(Succeed(), "the new leaf must be trusted by the overlap bundle")

		settled, err := DevelopmentSelfSignedTLSSecrets(kn, rotatedByName, initialTime.Add(360*24*time.Hour))
		Expect(err).NotTo(HaveOccurred())
		settledCA := parseCertificatesForTest(secretsByName(settled)["kubernaut-internal-ca"].Data["ca.crt"])
		Expect(settledCA).To(HaveLen(1), "the previous root can be removed after every leaf has moved")
	})

	It("UT-TLS-GAP-001 accepts cert-manager's tls.crt CA output without weakening administrator-managed validation", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}
		generated, err := DevelopmentSelfSignedTLSSecrets(kn, nil, time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC))
		Expect(err).NotTo(HaveOccurred())
		ca := secretsByName(generated)["kubernaut-internal-ca"].DeepCopy()
		ca.Data[corev1.TLSCertKey] = ca.Data["ca.crt"]
		ca.Data["ca.crt"] = []byte("not-the-cert-manager-ca")

		Expect(ValidateInternalCASecretForSource(ca, TLSMaterialSourceCertManager)).To(Succeed())
		Expect(ValidateInternalCASecret(ca)).To(MatchError(ContainSubstring("ca.crt")))
	})

	It("UT-TLS-ROTATION-GAP-002 preserves the previous root when the active CA key cannot be reused", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{RotationBefore: "9600h"},
		}
		initialTime := time.Now().UTC().Add(-20 * 24 * time.Hour)
		initial, err := DevelopmentSelfSignedTLSSecrets(kn, nil, initialTime)
		Expect(err).NotTo(HaveOccurred())
		existing := secretsByName(initial)
		existing["kubernaut-internal-ca"].Data["ca.key"] = []byte("not-a-private-key")

		rotated, err := DevelopmentSelfSignedTLSSecrets(kn, existing, initialTime.Add(20*24*time.Hour))
		Expect(err).NotTo(HaveOccurred())
		rotatedByName := secretsByName(rotated)
		caBundle := parseCertificatesForTest(rotatedByName["kubernaut-internal-ca"].Data["ca.crt"])
		Expect(caBundle).To(HaveLen(2), "a failed key reuse must retain the last working root")
		Expect(ValidateServingTLSSecretForService(
			existing[GatewayTLSSecretName],
			rotatedByName["kubernaut-internal-ca"],
			TLSServiceGateway,
			kn.Namespace,
		)).To(Succeed(), "the previous leaf must remain trusted after failed rotation reuse")
	})

	It("UT-TLS-ROTATION-GAP-003 does not reuse development TLS material from Secrets being deleted", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}
		initialTime := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
		initial, err := DevelopmentSelfSignedTLSSecrets(kn, nil, initialTime)
		Expect(err).NotTo(HaveOccurred())

		for _, testCase := range []struct {
			name       string
			secretName string
			dataKey    string
		}{
			{name: "CA", secretName: defaultDevelopmentSelfSignedCASecretName, dataKey: "ca.key"},
			{name: "serving leaf", secretName: GatewayTLSSecretName, dataKey: corev1.TLSCertKey},
			{name: "signing certificate", secretName: defaultCertManagerSigningSecretName, dataKey: corev1.TLSCertKey},
		} {
			existing := secretsByName(initial)
			deletedAt := metav1.NewTime(initialTime.Add(time.Minute))
			existing[testCase.secretName].DeletionTimestamp = &deletedAt
			previous := append([]byte(nil), existing[testCase.secretName].Data[testCase.dataKey]...)

			rotated, err := DevelopmentSelfSignedTLSSecrets(kn, existing, initialTime.Add(2*time.Minute))
			Expect(err).NotTo(HaveOccurred(), testCase.name)
			Expect(secretsByName(rotated)[testCase.secretName].Data[testCase.dataKey]).NotTo(Equal(previous), testCase.name)
		}
	})

	It("covers rejected development serving and signing material reuse paths", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}
		now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
		generated, err := DevelopmentSelfSignedTLSSecrets(kn, nil, now)
		Expect(err).NotTo(HaveOccurred())
		existing := secretsByName(generated)
		caPEM := existing[defaultDevelopmentSelfSignedCASecretName].Data[tlsCACertificateKey]
		leaf := existing[GatewayTLSSecretName]

		Expect(reusableDevelopmentLeaf(nil, caPEM, "gateway-service", kn.Namespace, now, defaultDevelopmentTLSRotation, nil)).To(BeFalse())
		invalidCertificate := leaf.DeepCopy()
		invalidCertificate.Data[corev1.TLSCertKey] = []byte("invalid")
		Expect(reusableDevelopmentLeaf(invalidCertificate, caPEM, "gateway-service", kn.Namespace, now, defaultDevelopmentTLSRotation, nil)).To(BeFalse())
		Expect(reusableDevelopmentLeaf(leaf, caPEM, "gateway-service", kn.Namespace, now.Add(29*24*time.Hour), defaultDevelopmentTLSRotation, nil)).To(BeFalse())
		invalidKey := leaf.DeepCopy()
		invalidKey.Data[corev1.TLSPrivateKeyKey] = []byte("invalid")
		Expect(reusableDevelopmentLeaf(invalidKey, caPEM, "gateway-service", kn.Namespace, now, defaultDevelopmentTLSRotation, nil)).To(BeFalse())
		mismatchedKey := leaf.DeepCopy()
		mismatchedKey.Data[corev1.TLSPrivateKeyKey] = existing[DataStorageTLSSecretName].Data[corev1.TLSPrivateKeyKey]
		Expect(reusableDevelopmentLeaf(mismatchedKey, caPEM, "gateway-service", kn.Namespace, now, defaultDevelopmentTLSRotation, nil)).To(BeFalse())
		Expect(reusableDevelopmentLeaf(leaf, []byte("invalid"), "gateway-service", kn.Namespace, now, defaultDevelopmentTLSRotation, nil)).To(BeFalse())
		Expect(reusableDevelopmentLeaf(leaf, caPEM, "not-gateway-service", kn.Namespace, now, defaultDevelopmentTLSRotation, nil)).To(BeFalse())

		signing := existing[defaultCertManagerSigningSecretName]
		Expect(reusableDevelopmentSigningCertificate(signing, now, defaultDevelopmentTLSRotation)).To(BeTrue())
		Expect(reusableDevelopmentSigningCertificate(nil, now, defaultDevelopmentTLSRotation)).To(BeFalse())
		invalidSigningCertificate := signing.DeepCopy()
		invalidSigningCertificate.Data[corev1.TLSCertKey] = []byte("invalid")
		Expect(reusableDevelopmentSigningCertificate(invalidSigningCertificate, now, defaultDevelopmentTLSRotation)).To(BeFalse())
		Expect(reusableDevelopmentSigningCertificate(signing, now.Add(29*24*time.Hour), defaultDevelopmentTLSRotation)).To(BeFalse())
		invalidSigningKey := signing.DeepCopy()
		invalidSigningKey.Data[corev1.TLSPrivateKeyKey] = []byte("invalid")
		Expect(reusableDevelopmentSigningCertificate(invalidSigningKey, now, defaultDevelopmentTLSRotation)).To(BeFalse())
		//nolint:gosec // an undersized key is intentional here: the production validator must reject it.
		smallKey, err := rsa.GenerateKey(rand.Reader, 1024)
		Expect(err).NotTo(HaveOccurred())
		smallSigningKey := signing.DeepCopy()
		smallSigningKey.Data[corev1.TLSPrivateKeyKey] = pemEncode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(smallKey))
		Expect(reusableDevelopmentSigningCertificate(smallSigningKey, now, defaultDevelopmentTLSRotation)).To(BeFalse())
		_, alternateSigningKey, err := generateDevelopmentSigningCertificate(now)
		Expect(err).NotTo(HaveOccurred())
		mismatchedSigningKey := signing.DeepCopy()
		mismatchedSigningKey.Data[corev1.TLSPrivateKeyKey] = alternateSigningKey
		Expect(reusableDevelopmentSigningCertificate(mismatchedSigningKey, now, defaultDevelopmentTLSRotation)).To(BeFalse())
		ecdsaCertificate := signing.DeepCopy()
		ecdsaCertificate.Data[corev1.TLSCertKey] = leaf.Data[corev1.TLSCertKey]
		Expect(reusableDevelopmentSigningCertificate(ecdsaCertificate, now, defaultDevelopmentTLSRotation)).To(BeFalse())
	})
})

var _ = Describe("runtime TLS resolver edge cases", func() {
	It("rejects nil input and resolves manual mode without taking ownership", func() {
		_, err := ResolveTLSMaterial(nil)
		Expect(err).To(MatchError("kubernaut is required"))

		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeManual,
			AdministratorManaged: &kubernautv1alpha2.AdministratorManagedTLSConfig{
				ServiceTLSSecretNames: map[string]string{TLSServiceGateway: "custom-gateway-tls"},
			},
		}
		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceAdministratorManaged))
		Expect(material.InternalCAConfigMapName).To(Equal(InterServiceCAConfigMapName))
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceGateway, "custom-gateway-tls"))
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceAuthWebhook, "authwebhook-tls"))
		Expect(material.OwnsSecrets).To(BeFalse())

		kn.Spec.TLS.AdministratorManaged.InternalCASecretName = "customer-ca"
		material, err = ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.InternalCASecretName).To(Equal("customer-ca"))
	})

	It("validates administrator-managed, cert-manager, and development references", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeAdministratorManaged,
			AdministratorManaged: &kubernautv1alpha2.AdministratorManagedTLSConfig{
				InternalCASecretName:  "customer-ca",
				ServiceTLSSecretNames: validServiceTLSSecretNames(),
			},
		}
		delete(kn.Spec.TLS.AdministratorManaged.ServiceTLSSecretNames, TLSServiceGateway)
		_, err := ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("serviceTLSSecretNames")))

		kn.Spec.TLS.AdministratorManaged.ServiceTLSSecretNames = validServiceTLSSecretNames()
		kn.Spec.TLS.AdministratorManaged.InternalCASecretName = ""
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("internalCASecretName")))

		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeCertManager}
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("certManager is required")))

		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				Issuer:                kubernautv1alpha2.TLSIssuerRef{Name: "customer-issuer"},
				InternalCASecretName:  "customer-ca",
				ServiceTLSSecretNames: validServiceTLSSecretNames(),
			},
		}
		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceCertManager))
		Expect(material.InternalCASecretName).To(Equal("customer-ca"))

		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:        kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{Issuer: kubernautv1alpha2.TLSIssuerRef{Name: "customer-issuer"}},
		}
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("serviceTLSSecretNames")))

		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeDevelopmentSelfSigned}
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("developmentSelfSigned is required")))

		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeHelmCertManager}
		material, err = ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(TLSMaterialSourceCertManager))
		Expect(CertManagerTLSProvisioningEnabled(kn)).To(BeTrue())
	})
})

var _ = Describe("TLS Secret validation", func() {
	var (
		kn      *kubernautv1alpha2.Kubernaut
		secrets map[string]*corev1.Secret
		ca      *corev1.Secret
		leaf    *corev1.Secret
		auth    *corev1.Secret
		signing *corev1.Secret
		now     = time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	)

	BeforeEach(func() {
		kn = testKubernautWithDevelopmentTLS()
		generated, err := DevelopmentSelfSignedTLSSecrets(kn, nil, now)
		Expect(err).NotTo(HaveOccurred())
		secrets = secretsByName(generated)
		ca = secrets[defaultDevelopmentSelfSignedCASecretName]
		leaf = secrets[GatewayTLSSecretName]
		auth = secrets["authwebhook-tls"]
		signing = secrets[defaultCertManagerSigningSecretName]
	})

	It("rejects missing, malformed, and non-CA internal material", func() {
		_, err := InternalCAPEMForSource(nil, "")
		Expect(err).To(MatchError("internal CA Secret is required"))

		missing := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "missing-ca"}, Data: map[string][]byte{}}
		_, err = InternalCAPEMForSource(missing, "")
		Expect(err).To(MatchError(ContainSubstring("missing ca.crt")))
		_, err = InternalCAPEMForSource(missing, TLSMaterialSourceCertManager)
		Expect(err).To(MatchError(ContainSubstring("missing ca.crt or tls.crt")))

		nonCA := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "not-a-ca"},
			Data:       map[string][]byte{"ca.crt": append([]byte(nil), leaf.Data[corev1.TLSCertKey]...)},
		}
		_, err = InternalCAPEMForSource(nonCA, "")
		Expect(err).To(MatchError(ContainSubstring("non-CA certificate")))

		certManagerCA := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "cert-manager-ca"},
			Data:       map[string][]byte{corev1.TLSCertKey: append([]byte(nil), ca.Data[tlsCACertificateKey]...)},
		}
		pemBytes, err := InternalCAPEMForSource(certManagerCA, TLSMaterialSourceCertManager)
		Expect(err).NotTo(HaveOccurred())
		Expect(pemBytes).To(Equal(ca.Data[tlsCACertificateKey]))
	})

	It("rejects invalid serving and signing Secret formats", func() {
		Expect(ValidateServingTLSSecret(nil)).To(MatchError("serving TLS Secret is required"))
		Expect(ValidateServingTLSSecret(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "empty"}})).To(MatchError(ContainSubstring("tls.crt and tls.key")))

		invalid := leaf.DeepCopy()
		invalid.Data[corev1.TLSPrivateKeyKey] = []byte("not-a-key")
		Expect(ValidateServingTLSSecret(invalid)).To(MatchError(ContainSubstring("invalid TLS key pair")))

		invalidCertificate := signing.DeepCopy()
		invalidCertificate.Data[corev1.TLSCertKey] = []byte("not-a-certificate")
		Expect(ValidateDataStorageSigningTLSSecret(invalidCertificate)).To(HaveOccurred())

		signingKey, err := parseRSAKey(signing.Data[corev1.TLSPrivateKeyKey])
		Expect(err).NotTo(HaveOccurred())
		pkcs8Key, err := x509.MarshalPKCS8PrivateKey(signingKey)
		Expect(err).NotTo(HaveOccurred())
		pkcs8Signing := signing.DeepCopy()
		pkcs8Signing.Data[corev1.TLSPrivateKeyKey] = pemEncode("PRIVATE KEY", pkcs8Key)
		Expect(ValidateDataStorageSigningTLSSecret(pkcs8Signing)).To(Succeed())

		smallKey := mustRSAKey(1024)
		smallSigning := signing.DeepCopy()
		smallSigning.Data[corev1.TLSCertKey] = mustRSACertificate(smallKey)
		smallSigning.Data[corev1.TLSPrivateKeyKey] = pemEncode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(smallKey))
		Expect(ValidateDataStorageSigningTLSSecret(smallSigning)).To(MatchError(ContainSubstring("RSA-2048")))
	})

	It("enforces service identity, AuthWebhook identity, and ingress-host identity", func() {
		Expect(ValidateServingTLSSecretForService(nil, ca, TLSServiceGateway, kn.Namespace)).To(HaveOccurred())
		Expect(ValidateServingTLSSecretForService(leaf, nil, TLSServiceGateway, kn.Namespace)).To(HaveOccurred())
		Expect(ValidateServingTLSSecretForService(leaf, ca, "not-a-service", kn.Namespace)).To(MatchError(ContainSubstring("not signed")))
		Expect(ValidateServingTLSSecretForServiceWithSource(leaf, ca, TLSServiceGateway, kn.Namespace, TLSMaterialSourceCertManager)).To(Succeed())

		Expect(ValidateAuthWebhookTLSSecret(nil, kn.Namespace)).To(HaveOccurred())
		invalidAuth := auth.DeepCopy()
		invalidAuth.Data[corev1.TLSPrivateKeyKey] = []byte("not-a-key")
		Expect(ValidateAuthWebhookTLSSecret(invalidAuth, kn.Namespace)).To(HaveOccurred())
		wrongAuth := leaf.DeepCopy()
		Expect(ValidateAuthWebhookTLSSecret(wrongAuth, kn.Namespace)).To(MatchError(ContainSubstring("does not cover")))
		Expect(ValidateAuthWebhookTLSSecret(auth, kn.Namespace)).To(Succeed())

		Expect(ValidateServingTLSSecretForHost(nil, "gateway.example.test")).To(HaveOccurred())
		invalidHost := leaf.DeepCopy()
		invalidHost.Data[corev1.TLSPrivateKeyKey] = []byte("not-a-key")
		Expect(ValidateServingTLSSecretForHost(invalidHost, "gateway.example.test")).To(HaveOccurred())
		Expect(ValidateServingTLSSecretForHost(leaf, "gateway-service."+kn.Namespace)).To(Succeed())
		Expect(ValidateServingTLSSecretForHost(leaf, "not-gateway.example.test")).To(MatchError(ContainSubstring("does not cover ingress host")))
	})

	It("validates AuthWebhook CA bundles and required service-name maps", func() {
		_, err := AuthWebhookCABundle(nil)
		Expect(err).To(MatchError(ContainSubstring("missing ca.crt")))
		invalid := auth.DeepCopy()
		invalid.Data[tlsCACertificateKey] = []byte("invalid")
		_, err = AuthWebhookCABundle(invalid)
		Expect(err).To(MatchError(ContainSubstring("invalid ca.crt")))
		nonCA := auth.DeepCopy()
		nonCA.Data[tlsCACertificateKey] = append([]byte(nil), leaf.Data[corev1.TLSCertKey]...)
		_, err = AuthWebhookCABundle(nonCA)
		Expect(err).To(MatchError(ContainSubstring("non-CA certificate")))
		bundle, err := AuthWebhookCABundle(auth)
		Expect(err).NotTo(HaveOccurred())
		Expect(bundle).To(Equal(auth.Data[tlsCACertificateKey]))

		_, err = requiredServiceTLSSecretNames(nil, false)
		Expect(err).To(MatchError(ContainSubstring("serviceTLSSecretNames is required")))
		_, err = requiredServiceTLSSecretNames(map[string]string{TLSServiceGateway: "gateway-tls"}, false)
		Expect(err).To(MatchError(ContainSubstring("serviceTLSSecretNames[\"datastorage\"] is required")))
	})
})

var _ = Describe("TLS helper edge cases", func() {
	It("covers manual defaults, unknown service mappings, and cert-manager validation", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				Issuer:                kubernautv1alpha2.TLSIssuerRef{Name: "customer-issuer"},
				ServiceTLSSecretNames: validServiceTLSSecretNames(),
			},
		}
		_, err := ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("internalCASecretName")))

		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeManual}
		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.InternalCAConfigMapName).To(Equal(InterServiceCAConfigMapName))
		Expect(material.ServiceTLSSecretNames).To(HaveKey(TLSServiceGateway))

		Expect(boolToInt(false)).To(Equal(0))
		Expect(ResolveDevelopmentTLSSecretName("unknown")).To(BeEmpty())
		Expect(developmentTLSComponent("unknown")).To(Equal("inter-service-tls"))
		Expect(TLSServiceDNSNames("unknown", kn.Namespace)).To(BeNil())
	})

	It("covers development configuration and parser rejection paths", func() {
		now := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
		_, err := DevelopmentSelfSignedTLSSecrets(nil, nil, now)
		Expect(err).To(MatchError("kubernaut is required"))

		kn := testKubernautWithDevelopmentTLS()
		kn.Spec.TLS.DevelopmentSelfSigned.RotationBefore = invalidRotationBefore
		_, err = DevelopmentSelfSignedTLSSecrets(kn, nil, now)
		Expect(err).To(MatchError(ContainSubstring("non-negative duration")))
		kn.Spec.TLS.DevelopmentSelfSigned.RotationBefore = "-1h"
		_, err = DevelopmentSelfSignedTLSSecrets(kn, nil, now)
		Expect(err).To(MatchError(ContainSubstring("non-negative duration")))
		kn.Spec.TLS.DevelopmentSelfSigned.RotationBefore = ""
		kn.Spec.TLS.DevelopmentSelfSigned.CASecretName = "custom-development-ca"
		generated, err := DevelopmentSelfSignedTLSSecrets(kn, nil, now)
		Expect(err).NotTo(HaveOccurred())
		Expect(secretsByName(generated)).To(HaveKey("custom-development-ca"))

		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeAdministratorManaged
		settings, err := developmentTLSSettingsFor(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(settings.signingSecretName).To(Equal(defaultCertManagerSigningSecretName))

		ca := secretsByName(generated)["custom-development-ca"]
		existing := secretsByName(generated)
		Expect(allDevelopmentLeavesUseCA(existing, []byte("invalid"), kn, nil)).To(BeFalse())
		missingLeaf := secretsByName(generated)
		delete(missingLeaf, GatewayTLSSecretName)
		Expect(allDevelopmentLeavesUseCA(missingLeaf, ca.Data[tlsCACertificateKey], kn, nil)).To(BeFalse())
		wrongDNS := secretsByName(generated)
		wrongGateway := wrongDNS[GatewayTLSSecretName].DeepCopy()
		wrongGateway.Data[corev1.TLSCertKey] = wrongDNS[DataStorageTLSSecretName].Data[corev1.TLSCertKey]
		wrongDNS[GatewayTLSSecretName] = wrongGateway
		Expect(allDevelopmentLeavesUseCA(wrongDNS, ca.Data[tlsCACertificateKey], kn, nil)).To(BeFalse())

		bundle := developmentCABundle(&corev1.Secret{Data: map[string][]byte{tlsCACertificateKey: []byte("invalid")}}, now, []byte("active-der"), []byte("active-pem"))
		Expect(bundle).To(Equal([]byte("active-pem")))
		Expect(parseCertificatesOrNil([]byte("invalid"))).To(BeNil())
		_, err = parseCertificates(nil)
		Expect(err).To(MatchError("certificate bundle is empty"))
		_, err = parseCertificates(pemEncode("PRIVATE KEY", []byte("invalid")))
		Expect(err).To(MatchError(ContainSubstring("contains PEM block")))
		_, err = parseCertificates(pemEncode("CERTIFICATE", []byte("invalid")))
		Expect(err).To(HaveOccurred())
		_, err = parsePrivateKey(pemEncode("PRIVATE KEY", []byte("invalid")))
		Expect(err).To(HaveOccurred())

		_, _, err = signDevelopmentLeaf([]byte("invalid"), ca.Data["ca.key"], "gateway-service", kn.Namespace, now, nil)
		Expect(err).To(MatchError(ContainSubstring("parsing CA certificate")))
		_, _, err = signDevelopmentLeaf(ca.Data[tlsCACertificateKey], []byte("invalid"), "gateway-service", kn.Namespace, now, nil)
		Expect(err).To(MatchError(ContainSubstring("parsing CA key")))

		_, ips := developmentDNSNamesAndIPs("gateway-service", kn.Namespace, []string{" ", "10.0.0.1", "127.0.0.1"})
		Expect(ips).To(HaveLen(2))
		Expect(ips[0].String()).To(Equal("10.0.0.1"))
		Expect(ips[1].String()).To(Equal("127.0.0.1"))
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

func secretsByName(secrets []*corev1.Secret) map[string]*corev1.Secret {
	result := make(map[string]*corev1.Secret, len(secrets))
	for _, secret := range secrets {
		result[secret.Name] = secret
	}
	return result
}

func parseCertificatesForTest(data []byte) []*x509.Certificate {
	certificates, err := parseCertificates(data)
	Expect(err).NotTo(HaveOccurred())
	return certificates
}

func mustRSAKey(bits int) *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, bits)
	Expect(err).NotTo(HaveOccurred())
	return key
}

func mustRSACertificate(key *rsa.PrivateKey) []byte {
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-signing"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return pemEncode("CERTIFICATE", der)
}

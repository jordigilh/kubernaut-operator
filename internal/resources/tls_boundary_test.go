package resources

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"math/big"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("runtime TLS material rejection boundaries", func() {
	It("UT-AUDIT-515-TLS-VALIDATION-001: rejects absent and malformed serving, signing, host and CA material", func() {
		for _, secret := range []*corev1.Secret{nil, {}, {Data: map[string][]byte{
			corev1.TLSCertKey: []byte("invalid"), corev1.TLSPrivateKeyKey: []byte("invalid"),
		}}} {
			Expect(ValidateServingTLSSecret(secret)).To(HaveOccurred())
			Expect(ValidateDataStorageSigningTLSSecret(secret)).To(HaveOccurred())
			Expect(ValidateServingTLSSecretForService(secret, nil, TLSServiceGateway, "default")).To(HaveOccurred())
			Expect(ValidateAuthWebhookTLSSecret(secret, "default")).To(HaveOccurred())
			Expect(ValidateServingTLSSecretForHost(secret, "gateway-service")).To(HaveOccurred())
			_, err := AuthWebhookCABundle(secret)
			Expect(err).To(HaveOccurred())
		}
		for _, source := range []string{TLSMaterialSourceAdministratorManaged, TLSMaterialSourceCertManager} {
			_, err := InternalCAPEMForSource(nil, source)
			Expect(err).To(HaveOccurred())
			_, err = InternalCAPEMForSource(&corev1.Secret{}, source)
			Expect(err).To(HaveOccurred())
		}
	})

	It("UT-AUDIT-515-TLS-VALIDATION-002: enforces CA identity, service DNS names, host names and signing-key strength", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeHook}
		generated, err := DevelopmentSelfSignedTLSSecrets(kn, nil, time.Now().Add(-time.Hour))
		Expect(err).NotTo(HaveOccurred())
		secrets := secretsByName(generated)
		serving := secrets[GatewayTLSSecretName]
		ca := generated[0]
		Expect(ValidateDataStorageSigningTLSSecret(serving)).To(MatchError(ContainSubstring("RSA-2048")))
		Expect(ValidateServingTLSSecretForHost(serving, "gateway-service")).To(Succeed())
		Expect(ValidateServingTLSSecretForHost(serving, "wrong.example.com")).To(HaveOccurred())
		Expect(ValidateServingTLSSecretForService(serving, nil, TLSServiceGateway, kn.Namespace)).To(HaveOccurred())
		Expect(ValidateServingTLSSecretForService(serving, ca, TLSServiceAPIFrontend, kn.Namespace)).To(HaveOccurred())
		Expect(ValidateAuthWebhookTLSSecret(serving, kn.Namespace)).To(HaveOccurred())
		nonCA := &corev1.Secret{Data: map[string][]byte{"ca.crt": serving.Data[corev1.TLSCertKey]}}
		_, err = InternalCAPEMForSource(nonCA, TLSMaterialSourceAdministratorManaged)
		Expect(err).To(MatchError(ContainSubstring("non-CA")))
		_, err = AuthWebhookCABundle(nonCA)
		Expect(err).To(MatchError(ContainSubstring("non-CA")))
		_, err = AuthWebhookCABundle(&corev1.Secret{Data: map[string][]byte{"ca.crt": []byte("bad PEM")}})
		Expect(err).To(HaveOccurred())
		_, err = InternalCAPEMForSource(ca, TLSMaterialSourceCertManager)
		Expect(err).NotTo(HaveOccurred(), "cert-manager's ca.crt fallback remains valid")
		malformedFirst := serving.DeepCopy()
		malformedFirst.Data[corev1.TLSCertKey] = append(pemEncode("IGNORED", []byte("invalid DER")), serving.Data[corev1.TLSCertKey]...)
		Expect(ValidateServingTLSSecret(malformedFirst)).To(Succeed(), "crypto/tls ignores non-certificate PEM blocks")
		Expect(ValidateDataStorageSigningTLSSecret(malformedFirst)).To(HaveOccurred())
		Expect(ValidateServingTLSSecretForService(malformedFirst, ca, TLSServiceGateway, kn.Namespace)).To(HaveOccurred())
		Expect(ValidateAuthWebhookTLSSecret(malformedFirst, kn.Namespace)).To(HaveOccurred())
		Expect(ValidateServingTLSSecretForHost(malformedFirst, "gateway-service")).To(HaveOccurred())
		malformedKey := secrets[DataStorageSigningSecretName(kn)].DeepCopy()
		malformedKey.Data[corev1.TLSPrivateKeyKey] = append(pemEncode("IGNORED", []byte("invalid DER")), malformedKey.Data[corev1.TLSPrivateKeyKey]...)
		Expect(ValidateDataStorageSigningTLSSecret(malformedKey)).To(MatchError(ContainSubstring("private key is invalid")))
	})

	It("UT-AUDIT-515-TLS-VALIDATION-003: rejects invalid references and development settings without fallback", func() {
		_, err := ResolveTLSMaterial(nil)
		Expect(err).To(HaveOccurred())
		_, err = DevelopmentSelfSignedTLSSecrets(nil, nil, time.Now())
		Expect(err).To(HaveOccurred())
		for _, mode := range []kubernautv1alpha2.TLSMode{kubernautv1alpha2.TLSModeAdministratorManaged,
			kubernautv1alpha2.TLSModeCertManager, kubernautv1alpha2.TLSModeDevelopmentSelfSigned} {
			kn := testKubernaut()
			kn.Spec.TLS.Mode = mode
			_, err = ResolveTLSMaterial(kn)
			Expect(err).To(HaveOccurred())
		}
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeAdministratorManaged,
			AdministratorManaged: &kubernautv1alpha2.AdministratorManagedTLSConfig{ServiceTLSSecretNames: validServiceTLSSecretNames()}}
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("internalCASecretName")))
		kn.Spec.TLS.AdministratorManaged.ServiceTLSSecretNames = nil
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("serviceTLSSecretNames")))
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{Issuer: kubernautv1alpha2.TLSIssuerRef{Name: "issuer"},
				ServiceTLSSecretNames: validServiceTLSSecretNames()}}
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(MatchError(ContainSubstring("internalCASecretName")))
		kn.Spec.TLS.CertManager.ServiceTLSSecretNames = map[string]string{TLSServiceGateway: "only-one"}
		_, err = ResolveTLSMaterial(kn)
		Expect(err).To(HaveOccurred())
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeDevelopmentSelfSigned}
		_, err = DevelopmentSelfSignedTLSSecrets(kn, nil, time.Now())
		Expect(err).To(HaveOccurred())
		for _, duration := range []string{"invalid", "-1h"} {
			kn.Spec.TLS.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{RotationBefore: duration}
			_, err = DevelopmentSelfSignedTLSSecrets(kn, nil, time.Now())
			Expect(err).To(HaveOccurred())
		}
		kn.Spec.TLS.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{CASecretName: "custom-ca"}
		generated, err := DevelopmentSelfSignedTLSSecrets(kn, nil, time.Now())
		Expect(err).NotTo(HaveOccurred())
		Expect(generated[0].Name).To(Equal("custom-ca"))
		Expect(ResolveDevelopmentTLSSecretName("unknown")).To(BeEmpty())
		Expect(developmentTLSComponent("unknown")).To(Equal("inter-service-tls"))
		Expect(TLSServiceDNSNames("unknown", kn.Namespace)).To(BeEmpty())
		Expect(boolToInt(false)).To(BeZero())
	})

	It("UT-AUDIT-515-TLS-VALIDATION-004: rejects malformed PEM, non-signing keys and unsupported public keys", func() {
		for _, data := range [][]byte{nil, pemEncode("PRIVATE KEY", []byte("invalid")), pemEncode("CERTIFICATE", []byte("invalid"))} {
			_, err := parseCertificates(data)
			Expect(err).To(HaveOccurred())
		}
		Expect(parseCertificatesOrNil(nil)).To(BeNil())
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		Expect(err).NotTo(HaveOccurred())
		encoded, err := x509.MarshalPKCS8PrivateKey(key)
		Expect(err).NotTo(HaveOccurred())
		_, err = parsePrivateKey(pemEncode("PRIVATE KEY", encoded))
		Expect(err).To(MatchError(ContainSubstring("not a signing key")))
		_, err = parsePrivateKey(pemEncode("PRIVATE KEY", []byte("invalid DER")))
		Expect(err).To(HaveOccurred())
		Expect(signerMatchesCertificate(nil, &x509.Certificate{PublicKey: "unsupported"})).To(BeFalse())
	})

	It("UT-AUDIT-515-TLS-VALIDATION-005: reports unrepresentable certificate dates and invalid signing inputs", func() {
		impossibleDate := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
		_, _, _, err := generateDevelopmentCA(nil, impossibleDate)
		Expect(err).To(HaveOccurred())
		_, _, err = generateDevelopmentSigningCertificate(impossibleDate)
		Expect(err).To(HaveOccurred())
		_, _, err = signDevelopmentLeaf(nil, nil, "gateway-service", "default", time.Now(), nil)
		Expect(err).To(HaveOccurred())
		ca, key, _, err := generateDevelopmentCA(nil, time.Now().Add(-time.Hour))
		Expect(err).NotTo(HaveOccurred())
		_, _, err = signDevelopmentLeaf(ca, nil, "gateway-service", "default", time.Now(), nil)
		Expect(err).To(HaveOccurred())
		_, _, err = signDevelopmentLeaf(ca, key, "gateway-service", "default", impossibleDate, nil)
		Expect(err).To(HaveOccurred())
		Expect(developmentCABundle(&corev1.Secret{}, time.Now(), nil, ca)).To(Equal(ca))
		Expect(allDevelopmentLeavesUseCA(nil, nil, testKubernaut(), nil)).To(BeFalse())
		Expect(allDevelopmentLeavesUseCA(nil, ca, testKubernaut(), nil)).To(BeFalse())
		dns, ips := developmentDNSNamesAndIPs("gateway-service", "default", []string{" ", "127.0.0.1", "10.0.0.1"})
		Expect(dns).NotTo(ContainElement(" "))
		Expect(ips).To(HaveLen(2), "the explicit loopback address is not added twice")
	})

	It("UT-AUDIT-515-TLS-VALIDATION-006: rejects mixed CA PEM, mismatched services and unrepresentable serving identities", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeHook
		generated, err := DevelopmentSelfSignedTLSSecrets(kn, nil, time.Now().Add(-time.Hour))
		Expect(err).NotTo(HaveOccurred())
		secrets := secretsByName(generated)
		ca := generated[0].DeepCopy()
		ca.Data["ca.crt"] = append(pemEncode("UNSUPPORTED", []byte("not a certificate")), ca.Data["ca.crt"]...)
		Expect(ValidateServingTLSSecretForService(secrets[GatewayTLSSecretName], ca, TLSServiceGateway, kn.Namespace)).
			To(MatchError(ContainSubstring("ca.crt is invalid")))
		secrets[GatewayTLSSecretName] = secrets[APIFrontendTLSSecretName].DeepCopy()
		Expect(allDevelopmentLeavesUseCA(secrets, generated[0].Data["ca.crt"], kn, nil)).To(BeFalse())
		key, err := parsePrivateKey(generated[0].Data["ca.key"])
		Expect(err).NotTo(HaveOccurred())
		encoded, err := x509.MarshalPKCS8PrivateKey(key)
		Expect(err).NotTo(HaveOccurred())
		parsed, err := parsePrivateKey(pemEncode("PRIVATE KEY", encoded))
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Public()).To(Equal(key.Public()))
		defaultKn := testKubernaut()
		defaultKn.Spec.TLS.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{}
		settings, err := developmentTLSSettingsFor(defaultKn)
		Expect(err).NotTo(HaveOccurred())
		Expect(settings.signingSecretName).To(Equal(defaultCertManagerSigningSecretName))
		kn.Namespace = "not-an-ascii-dns-name-☃"
		_, err = DevelopmentSelfSignedTLSSecrets(kn, nil, time.Now())
		Expect(err).To(MatchError(ContainSubstring("development certificate")))
		kn.Namespace = "default"
		_, err = DevelopmentSelfSignedTLSSecrets(kn, nil, time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC))
		Expect(err).To(HaveOccurred())
	})

	It("UT-AUDIT-515-TLS-VALIDATION-007: propagates external entropy failures from CA, leaf and signing generation", func() {
		kn := testKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeHook
		now := time.Now().Add(-time.Hour)
		ca, key, bundle, err := generateDevelopmentCA(nil, now)
		Expect(err).NotTo(HaveOccurred())
		settings, err := developmentTLSSettingsFor(kn)
		Expect(err).NotTo(HaveOccurred())
		originalReader := rand.Reader
		originalDebug, debugWasSet := os.LookupEnv("GODEBUG")
		DeferCleanup(func() {
			rand.Reader = originalReader
			if debugWasSet {
				Expect(os.Setenv("GODEBUG", originalDebug)).To(Succeed())
			} else {
				Expect(os.Unsetenv("GODEBUG")).To(Succeed())
			}
		})
		// Go 1.26 ignores custom readers for key generation by default. Serial
		// generation still reads its explicit external source. Neither test mode
		// changes production key sizes, curves or the secure default reader.
		for _, compatibility := range []string{"0", "1"} {
			Expect(os.Setenv("GODEBUG", originalDebug+",cryptocustomrand="+compatibility)).To(Succeed())
			rand.Reader = &unitEntropyReader{}
			_, _, _, err = generateDevelopmentCA(nil, now)
			Expect(err).To(MatchError(ContainSubstring("injected entropy failure")))
			_, _, err = signDevelopmentLeaf(ca, key, "gateway-service", kn.Namespace, now, nil)
			Expect(err).To(MatchError(ContainSubstring("injected entropy failure")))
			_, _, err = generateDevelopmentSigningCertificate(now)
			Expect(err).To(MatchError(ContainSubstring("injected entropy failure")))
			_, err = developmentSigningTLSSecret(kn, nil, now, settings)
			Expect(err).To(MatchError(ContainSubstring("generating datastorage signing certificate")))
			_, err = developmentServingTLSSecrets(kn, nil, now, ca, key, bundle, settings)
			Expect(err).To(MatchError(ContainSubstring("development certificate")))
		}
		Expect(os.Setenv("GODEBUG", originalDebug+",cryptocustomrand=0")).To(Succeed())
		rand.Reader = &unitEntropyReader{remainingSuccessfulReads: len(developmentTLSServiceNamesFor(kn)) + 1}
		_, err = DevelopmentSelfSignedTLSSecrets(kn, nil, now)
		Expect(err).To(MatchError(ContainSubstring("generating datastorage signing certificate")))
	})
	It("UT-AUDIT-515-TLS-VALIDATION-008: preserves checked EC encoding failures without replacing production key generation", func() {
		key := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: &elliptic.CurveParams{Name: "unsupported-curve"}}, D: big.NewInt(1)}
		encoded, err := developmentECKeyPEM(key, "encoding development leaf key")
		Expect(encoded).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("encoding development leaf key")))
	})
})

// An external entropy transport fault, never a replacement of business logic.
// The BDD suite is serial; process-global cryptographic state is always restored.
type unitEntropyReader struct{ remainingSuccessfulReads int }

func (r *unitEntropyReader) Read(buffer []byte) (int, error) {
	if r.remainingSuccessfulReads == 0 {
		return 0, fmt.Errorf("injected entropy failure")
	}
	r.remainingSuccessfulReads--
	clear(buffer)
	return len(buffer), nil
}

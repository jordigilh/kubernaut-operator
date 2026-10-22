package resources

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("runtime TLS validation boundaries", func() {
	It("covers invalid and valid CA payloads", func() {
		ca, leaf := validationTLSSecrets()
		Expect(ValidateInternalCASecret(nil)).To(HaveOccurred())
		Expect(ValidateInternalCASecret(ca)).To(Succeed())
		Expect(ValidateInternalCASecretForSource(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "empty"}}, TLSMaterialSourceManual)).To(HaveOccurred())
		certManagerCA := ca.DeepCopy()
		certManagerCA.Data = map[string][]byte{corev1.TLSCertKey: ca.Data[tlsCACertificateKey]}
		Expect(ValidateInternalCASecretForSource(certManagerCA, TLSMaterialSourceCertManager)).To(Succeed())
		leafCA := leaf.DeepCopy()
		leafCA.Data[tlsCACertificateKey] = leaf.Data[corev1.TLSCertKey]
		Expect(ValidateInternalCASecretForSource(leafCA, TLSMaterialSourceManual)).To(MatchError(ContainSubstring("non-CA")))
		missingCertManager := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "missing-cert-manager"}, Data: map[string][]byte{}}
		Expect(ValidateInternalCASecretForSource(missingCertManager, TLSMaterialSourceCertManager)).To(HaveOccurred())
		invalid := ca.DeepCopy()
		invalid.Data[tlsCACertificateKey] = []byte("invalid")
		Expect(ValidateInternalCASecret(invalid)).To(HaveOccurred())
		Expect(InternalCAPEMForSource(ca, TLSMaterialSourceManual)).To(Equal(ca.Data[tlsCACertificateKey]))
	})

	It("covers serving, signing, AuthWebhook, and host validation", func() {
		ca, leaf := validationTLSSecrets()
		auth := validationAuthWebhookSecret()
		Expect(ValidateServingTLSSecret(nil)).To(HaveOccurred())
		Expect(ValidateServingTLSSecret(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "missing"}})).To(HaveOccurred())
		invalid := leaf.DeepCopy()
		invalid.Data[corev1.TLSPrivateKeyKey] = []byte("invalid")
		Expect(ValidateServingTLSSecret(invalid)).To(HaveOccurred())
		Expect(ValidateDataStorageSigningTLSSecret(leaf)).To(Succeed())
		Expect(ValidateDataStorageSigningTLSSecret(nil)).To(HaveOccurred())
		badCertificate := leaf.DeepCopy()
		badCertificate.Data[corev1.TLSCertKey] = prefixedCertificatePEM(leaf.Data[corev1.TLSCertKey])
		Expect(ValidateDataStorageSigningTLSSecret(badCertificate)).To(HaveOccurred())
		badKey := leaf.DeepCopy()
		badKey.Data[corev1.TLSPrivateKeyKey] = []byte("invalid")
		Expect(ValidateDataStorageSigningTLSSecret(badKey)).To(HaveOccurred())
		small := validationSelfSignedSigningSecret(1024)
		Expect(ValidateDataStorageSigningTLSSecret(small)).To(MatchError(ContainSubstring("RSA-2048")))
		Expect(ValidateServingTLSSecretForService(leaf, ca, TLSServiceGateway, "default")).To(Succeed())
		Expect(ValidateServingTLSSecretForService(nil, ca, TLSServiceGateway, "default")).To(HaveOccurred())
		Expect(ValidateServingTLSSecretForService(leaf, nil, TLSServiceGateway, "default")).To(HaveOccurred())
		badServing := leaf.DeepCopy()
		badServing.Data[corev1.TLSCertKey] = prefixedCertificatePEM(leaf.Data[corev1.TLSCertKey])
		Expect(ValidateServingTLSSecretForService(badServing, ca, TLSServiceGateway, "default")).To(HaveOccurred())
		Expect(ValidateAuthWebhookTLSSecret(nil, "default")).To(HaveOccurred())
		badAuth := auth.DeepCopy()
		badAuth.Data[corev1.TLSCertKey] = prefixedCertificatePEM(auth.Data[corev1.TLSCertKey])
		Expect(ValidateAuthWebhookTLSSecret(badAuth, "default")).To(HaveOccurred())
		Expect(ValidateServingTLSSecretForService(leaf, ca, TLSServiceDataStorage, "default")).To(MatchError(ContainSubstring("not signed")))
		Expect(ValidateServingTLSSecretForHost(leaf, "gateway-service.default.svc")).To(Succeed())
		Expect(ValidateServingTLSSecretForHost(leaf, "wrong.example")).To(HaveOccurred())
		Expect(ValidateServingTLSSecretForHost(nil, "wrong.example")).To(HaveOccurred())
		badHost := leaf.DeepCopy()
		badHost.Data[corev1.TLSCertKey] = prefixedCertificatePEM(leaf.Data[corev1.TLSCertKey])
		Expect(ValidateServingTLSSecretForHost(badHost, "gateway-service.default.svc")).To(HaveOccurred())
		Expect(ValidateAuthWebhookTLSSecret(auth, "default")).To(Succeed())
		_, err := AuthWebhookCABundle(nil)
		Expect(err).To(HaveOccurred())
		_, err = AuthWebhookCABundle(leaf)
		Expect(err).To(HaveOccurred())
		webhook := auth.DeepCopy()
		webhook.Data[tlsCACertificateKey] = ca.Data[tlsCACertificateKey]
		Expect(AuthWebhookCABundle(webhook)).To(Equal(ca.Data[tlsCACertificateKey]))
		invalidBundle := webhook.DeepCopy()
		invalidBundle.Data[tlsCACertificateKey] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("bad")})
		_, err = AuthWebhookCABundle(invalidBundle)
		Expect(err).To(HaveOccurred())
		nonCABundle := webhook.DeepCopy()
		nonCABundle.Data[tlsCACertificateKey] = leaf.Data[corev1.TLSCertKey]
		_, err = AuthWebhookCABundle(nonCABundle)
		Expect(err).To(HaveOccurred())
	})

	It("covers required Secret names, parser failures, and unknown services", func() {
		_, err := requiredServiceTLSSecretNames(nil, false)
		Expect(err).To(HaveOccurred())
		_, err = requiredServiceTLSSecretNames(map[string]string{TLSServiceGateway: "gateway"}, false)
		Expect(err).To(HaveOccurred())
		names := validServiceTLSSecretNames()
		names[TLSServiceFleetMetadataCache] = "fmc"
		_, err = requiredServiceTLSSecretNames(names, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(TLSServiceDNSNames("unknown", "default")).To(BeNil())
		_, err = parseCertificate([]byte("invalid"))
		Expect(err).To(HaveOccurred())
		_, err = parseCertificates(nil)
		Expect(err).To(HaveOccurred())
		_, err = parseCertificates(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("bad")}))
		Expect(err).To(HaveOccurred())
		_, err = parseCertificates(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("bad")}))
		Expect(err).To(HaveOccurred())
		Expect(pemEncode("TEST", []byte("data"))).NotTo(BeEmpty())
	})
})

func validationTLSSecrets() (*corev1.Secret, *corev1.Secret) {
	caKey := mustRSAKeyForTLS(2048)
	now := time.Now().Add(-time.Minute)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "validation-ca"},
		NotBefore:             now,
		NotAfter:              now.Add(time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	Expect(err).NotTo(HaveOccurred())
	leafKey := mustRSAKeyForTLS(2048)
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "gateway-service"},
		DNSNames:     TLSServiceDNSNames(TLSServiceGateway, "default"),
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	Expect(err).NotTo(HaveOccurred())
	return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "validation-ca", Namespace: "default"},
			Data: map[string][]byte{
				tlsCACertificateKey: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
				"ca.key":            pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)}),
			},
		}, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-tls", Namespace: "default"},
			Type:       corev1.SecretTypeTLS,
			Data: map[string][]byte{
				corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
				corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)}),
			},
		}
}

func prefixedCertificatePEM(certificate []byte) []byte {
	return append(pemEncode("PRIVATE KEY", []byte("invalid DER")), certificate...)
}

func validationAuthWebhookSecret() *corev1.Secret {
	key := mustRSAKeyForTLS(2048)
	now := time.Now().Add(-time.Minute)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "authwebhook-service"},
		DNSNames:     TLSServiceDNSNames(TLSServiceAuthWebhook, "default"),
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "authwebhook-tls", Namespace: "default"},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
		},
	}
}

func validationSelfSignedSigningSecret(bits int) *corev1.Secret {
	key := mustRSAKeyForTLS(bits)
	now := time.Now().Add(-time.Minute)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(4),
		Subject:      pkix.Name{CommonName: "signing"},
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "signing", Namespace: "default"},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
		},
	}
}

func mustRSAKeyForTLS(bits int) *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, bits)
	Expect(err).NotTo(HaveOccurred())
	return key
}

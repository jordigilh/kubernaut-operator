package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// testRuntimeTLSSecrets creates disposable external material for controller
// tests. Production reconciliation no longer generates this material.
func testRuntimeTLSSecrets(
	kn *kubernautv1alpha2.Kubernaut,
	_ map[string]*corev1.Secret,
	_ time.Time,
) ([]*corev1.Secret, error) {
	if kn == nil {
		return nil, fmt.Errorf("kubernaut is required")
	}
	now := time.Now().Add(-time.Minute)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating test TLS CA key: %w", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-runtime-ca"},
		NotBefore:             now,
		NotAfter:              now.Add(365 * 24 * time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating test TLS CA certificate: %w", err)
	}
	caTemplate, err = x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("parsing test TLS CA certificate: %w", err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	caKeyPEM, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return nil, fmt.Errorf("encoding test TLS CA key: %w", err)
	}
	secrets := []*corev1.Secret{{
		ObjectMeta: metav1.ObjectMeta{Name: "kubernaut-internal-ca", Namespace: kn.Namespace},
		Data:       map[string][]byte{"ca.crt": caPEM, "ca.key": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: caKeyPEM})},
	}}

	serviceNames := map[string]string{
		resources.TLSServiceGateway:        resources.GatewayTLSSecretName,
		resources.TLSServiceDataStorage:    resources.DataStorageTLSSecretName,
		resources.TLSServiceKubernautAgent: resources.KubernautAgentTLSSecretName,
		resources.TLSServiceAPIFrontend:    resources.APIFrontendTLSSecretName,
		resources.TLSServiceAuthWebhook:    "authwebhook-tls",
	}
	if kn.Spec.FleetMetadataCacheEnabled() {
		serviceNames[resources.TLSServiceFleetMetadataCache] = resources.FleetMetadataCacheTLSSecretName
	}
	for serviceKey, secretName := range serviceNames {
		secret, createErr := testServingSecret(kn, caTemplate, caKey, serviceKey, secretName, now)
		if createErr != nil {
			return nil, createErr
		}
		secrets = append(secrets, secret)
	}
	if signingName := resources.DataStorageSigningSecretName(kn); signingName != "" {
		signing, signingErr := testSigningSecret(kn, signingName, now)
		if signingErr != nil {
			return nil, signingErr
		}
		secrets = append(secrets, signing)
	}
	return secrets, nil
}

func certManagerServiceTLSSecretNames() map[string]string {
	return testServiceTLSSecretNames()
}

func createManualRuntimeTLSFixtures(ctx context.Context, kn *kubernautv1alpha2.Kubernaut) error {
	kn.Spec.TLS.Manual = &kubernautv1alpha2.ManualTLSConfig{
		InternalCASecretName:  "kubernaut-internal-ca",
		ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
	}
	secrets, err := testRuntimeTLSSecrets(kn, nil, time.Now().Add(-time.Hour))
	if err != nil {
		return err
	}
	for _, secret := range secrets {
		if err := k8sClient.Create(ctx, secret); err != nil {
			return err
		}
	}
	for _, webhook := range []client.Object{
		resources.MutatingWebhookConfigurationWithCABundle(kn, secrets[0].Data["ca.crt"]),
		resources.ValidatingWebhookConfigurationWithCABundle(kn, secrets[0].Data["ca.crt"]),
	} {
		if err := k8sClient.Create(ctx, webhook); err != nil {
			return err
		}
	}
	return nil
}

func testServingSecret(
	kn *kubernautv1alpha2.Kubernaut,
	caTemplate *x509.Certificate,
	caKey *ecdsa.PrivateKey,
	serviceKey, secretName string,
	now time.Time,
) (*corev1.Secret, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating test serving key for %s: %w", serviceKey, err)
	}
	names := resources.TLSServiceDNSNames(serviceKey, kn.Namespace)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(int64(len(serviceKey) + 2)),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    now,
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caTemplate, &key.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating test serving certificate for %s: %w", serviceKey, err)
	}
	keyPEM, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("encoding test serving key for %s: %w", serviceKey, err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: kn.Namespace},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyPEM}),
		},
	}
	if serviceKey == resources.TLSServiceAuthWebhook {
		secret.Data["ca.crt"] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caTemplate.Raw})
	}
	return secret, nil
}

func testSigningSecret(kn *kubernautv1alpha2.Kubernaut, name string, now time.Time) (*corev1.Secret, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generating test signing key: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    now,
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("creating test signing certificate: %w", err)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: kn.Namespace},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
		},
	}, nil
}

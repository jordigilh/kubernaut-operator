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
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// TLS source identifiers used internally by resource/controller adapters.
const (
	TLSMaterialSourceOpenShiftServiceCA      = "OpenShiftServiceCA"
	TLSMaterialSourceAdministratorManaged    = "AdministratorManaged"
	TLSMaterialSourceCertManager             = "CertManager"
	TLSMaterialSourceDevelopmentSelfSigned   = "DevelopmentSelfSigned"
	TLSServiceGateway                        = "gateway"
	TLSServiceDataStorage                    = "datastorage"
	TLSServiceKubernautAgent                 = "kubernautagent"
	TLSServiceAPIFrontend                    = "apifrontend"
	TLSServiceAuthWebhook                    = "authwebhook"
	tlsCACertificateKey                      = "ca.crt"
	defaultDevelopmentSelfSignedCASecretName = "kubernaut-internal-ca" //nolint:gosec // this is a Secret object name, not credential material
)

// TLSMaterial is the resolved runtime certificate reference set. The
// controller validates referenced Secrets or creates the material only when
// OwnsSecrets is true. Administrator-managed and cert-manager Secrets remain
// outside the operator's ownership boundary.
type TLSMaterial struct {
	Source                string
	InternalCASecretName  string
	ServiceTLSSecretNames map[string]string
	OwnsSecrets           bool
}

// ResolveTLSMaterial validates the explicit source contract and resolves the
// serving Secret names used by Services and Deployments. An empty source is
// reserved for the optional OpenShift service-CA adapter; it is never a
// generic plaintext mode.
func ResolveTLSMaterial(kn *kubernautv1alpha2.Kubernaut) (TLSMaterial, error) {
	if kn == nil {
		return TLSMaterial{}, fmt.Errorf("kubernaut is required")
	}

	legacy := TLSMaterial{
		Source:               TLSMaterialSourceOpenShiftServiceCA,
		InternalCASecretName: InterServiceCAConfigMapName,
		ServiceTLSSecretNames: map[string]string{
			TLSServiceGateway:        GatewayTLSSecretName,
			TLSServiceDataStorage:    DataStorageTLSSecretName,
			TLSServiceKubernautAgent: KubernautAgentTLSSecretName,
			TLSServiceAPIFrontend:    APIFrontendTLSSecretName,
			TLSServiceAuthWebhook:    "authwebhook-tls",
		},
	}

	switch kn.Spec.TLS.Mode {
	case "":
		return legacy, nil
	case kubernautv1alpha2.TLSModeAdministratorManaged:
		cfg := kn.Spec.TLS.AdministratorManaged
		if cfg == nil {
			return TLSMaterial{}, fmt.Errorf("tls.administratorManaged is required for mode %q", kn.Spec.TLS.Mode)
		}
		names, err := requiredServiceTLSSecretNames(cfg.ServiceTLSSecretNames)
		if err != nil {
			return TLSMaterial{}, fmt.Errorf("tls.administratorManaged: %w", err)
		}
		if cfg.InternalCASecretName == "" {
			return TLSMaterial{}, fmt.Errorf("tls.administratorManaged.internalCASecretName is required")
		}
		return TLSMaterial{
			Source:                TLSMaterialSourceAdministratorManaged,
			InternalCASecretName:  cfg.InternalCASecretName,
			ServiceTLSSecretNames: names,
		}, nil
	case kubernautv1alpha2.TLSModeCertManager:
		cfg := kn.Spec.TLS.CertManager
		if cfg == nil {
			return TLSMaterial{}, fmt.Errorf("tls.certManager is required for mode %q", kn.Spec.TLS.Mode)
		}
		if cfg.Issuer.Name == "" {
			return TLSMaterial{}, fmt.Errorf("tls.certManager.issuer.name is required")
		}
		names, err := requiredServiceTLSSecretNames(cfg.ServiceTLSSecretNames)
		if err != nil {
			return TLSMaterial{}, fmt.Errorf("tls.certManager: %w", err)
		}
		if cfg.InternalCASecretName == "" {
			return TLSMaterial{}, fmt.Errorf("tls.certManager.internalCASecretName is required")
		}
		return TLSMaterial{
			Source:                TLSMaterialSourceCertManager,
			InternalCASecretName:  cfg.InternalCASecretName,
			ServiceTLSSecretNames: names,
		}, nil
	case kubernautv1alpha2.TLSModeDevelopmentSelfSigned:
		cfg := kn.Spec.TLS.DevelopmentSelfSigned
		if cfg == nil {
			return TLSMaterial{}, fmt.Errorf("tls.developmentSelfSigned is required for mode %q", kn.Spec.TLS.Mode)
		}
		caName := cfg.CASecretName
		if caName == "" {
			caName = defaultDevelopmentSelfSignedCASecretName
		}
		return TLSMaterial{
			Source:               TLSMaterialSourceDevelopmentSelfSigned,
			InternalCASecretName: caName,
			ServiceTLSSecretNames: map[string]string{
				TLSServiceGateway:        GatewayTLSSecretName,
				TLSServiceDataStorage:    DataStorageTLSSecretName,
				TLSServiceKubernautAgent: KubernautAgentTLSSecretName,
				TLSServiceAPIFrontend:    APIFrontendTLSSecretName,
				TLSServiceAuthWebhook:    "authwebhook-tls",
			},
			OwnsSecrets: true,
		}, nil
	default:
		return TLSMaterial{}, fmt.Errorf("tls.mode %q is unsupported", kn.Spec.TLS.Mode)
	}
}

// ValidateInternalCASecret validates the public CA payload referenced by an
// administrator-managed or cert-manager TLS source.
func ValidateInternalCASecret(secret *corev1.Secret) error {
	return ValidateInternalCASecretForSource(secret, "")
}

// ValidateInternalCASecretForSource validates the public CA payload selected
// by a runtime TLS source. cert-manager's self-signed bootstrap Certificate
// stores its CA certificate under tls.crt, while administrator-managed
// material follows the operator contract and uses ca.crt.
func ValidateInternalCASecretForSource(secret *corev1.Secret, source string) error {
	_, err := InternalCAPEMForSource(secret, source)
	return err
}

// InternalCAPEMForSource returns the validated public CA bundle for a runtime
// TLS source. The returned bytes are copied so callers cannot mutate the
// Secret's backing data.
func InternalCAPEMForSource(secret *corev1.Secret, source string) ([]byte, error) {
	if secret == nil {
		return nil, fmt.Errorf("internal CA Secret is required")
	}
	key := tlsCACertificateKey
	if source == TLSMaterialSourceCertManager {
		key = corev1.TLSCertKey
		if len(secret.Data[key]) == 0 {
			key = tlsCACertificateKey
		}
	}
	data := secret.Data[key]
	if len(data) == 0 {
		if source == TLSMaterialSourceCertManager {
			return nil, fmt.Errorf("secret %q is missing ca.crt or tls.crt", secret.Name)
		}
		return nil, fmt.Errorf("secret %q is missing ca.crt", secret.Name)
	}
	certificates, err := parseCertificates(data)
	if err != nil {
		return nil, fmt.Errorf("secret %q %s is invalid: %w", secret.Name, key, err)
	}
	for _, certificate := range certificates {
		if !certificate.IsCA {
			return nil, fmt.Errorf("secret %q %s contains a non-CA certificate", secret.Name, key)
		}
	}
	return append([]byte(nil), data...), nil
}

// ValidateServingTLSSecret validates a Kubernetes TLS Secret without making
// ownership assumptions. It accepts the standard PKCS#1/PKCS#8 key formats
// supported by crypto/tls and verifies that the certificate/key pair matches.
func ValidateServingTLSSecret(secret *corev1.Secret) error {
	if secret == nil {
		return fmt.Errorf("serving TLS Secret is required")
	}
	if len(secret.Data[corev1.TLSCertKey]) == 0 || len(secret.Data[corev1.TLSPrivateKeyKey]) == 0 {
		return fmt.Errorf("secret %q must contain tls.crt and tls.key", secret.Name)
	}
	if _, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey]); err != nil {
		return fmt.Errorf("secret %q contains an invalid TLS key pair: %w", secret.Name, err)
	}
	return nil
}

// ValidateServingTLSSecretForService validates a serving Secret against the
// selected internal CA and the DNS identities of one Kubernetes Service. This
// keeps administrator-managed and cert-manager material from being accepted
// merely because it is a syntactically valid keypair for another workload.
func ValidateServingTLSSecretForService(secret, ca *corev1.Secret, serviceKey, namespace string) error {
	return ValidateServingTLSSecretForServiceWithSource(secret, ca, serviceKey, namespace, "")
}

// ValidateServingTLSSecretForServiceWithSource validates a serving Secret
// against the CA format selected by the runtime TLS source.
func ValidateServingTLSSecretForServiceWithSource(
	secret, ca *corev1.Secret,
	serviceKey, namespace, source string,
) error {
	if err := ValidateServingTLSSecret(secret); err != nil {
		return err
	}
	caPEM, err := InternalCAPEMForSource(ca, source)
	if err != nil {
		return err
	}
	leaf, err := parseCertificate(secret.Data[corev1.TLSCertKey])
	if err != nil {
		return fmt.Errorf("secret %q certificate is invalid: %w", secret.Name, err)
	}
	caCertificates, err := parseCertificates(caPEM)
	if err != nil {
		return fmt.Errorf("secret %q CA bundle is invalid: %w", ca.Name, err)
	}
	pool := x509.NewCertPool()
	for _, caCertificate := range caCertificates {
		pool.AddCert(caCertificate)
	}
	for _, dnsName := range TLSServiceDNSNames(serviceKey, namespace) {
		if _, err := leaf.Verify(x509.VerifyOptions{
			Roots:     pool,
			DNSName:   dnsName,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}); err == nil {
			return nil
		}
	}
	return fmt.Errorf("secret %q certificate is not signed by %q for the %s service DNS names", secret.Name, ca.Name, serviceKey)
}

// ValidateServingTLSSecretForHost validates an externally terminated Ingress
// certificate without requiring the operator to own or know its issuing CA.
func ValidateServingTLSSecretForHost(secret *corev1.Secret, host string) error {
	if err := ValidateServingTLSSecret(secret); err != nil {
		return err
	}
	certificate, err := parseCertificate(secret.Data[corev1.TLSCertKey])
	if err != nil {
		return fmt.Errorf("secret %q certificate is invalid: %w", secret.Name, err)
	}
	if err := certificate.VerifyHostname(host); err != nil {
		return fmt.Errorf("secret %q certificate does not cover ingress host %q: %w", secret.Name, host, err)
	}
	return nil
}

func requiredServiceTLSSecretNames(input map[string]string) (map[string]string, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("serviceTLSSecretNames is required")
	}
	const required = "gateway datastorage kubernautagent apifrontend authwebhook"
	result := make(map[string]string, len(input))
	for _, key := range []string{TLSServiceGateway, TLSServiceDataStorage, TLSServiceKubernautAgent, TLSServiceAPIFrontend, TLSServiceAuthWebhook} {
		value := input[key]
		if value == "" {
			return nil, fmt.Errorf("serviceTLSSecretNames[%q] is required; required keys: %s", key, required)
		}
		result[key] = value
	}
	return result, nil
}

const (
	defaultDevelopmentTLSValidity = 30 * 24 * time.Hour
	defaultDevelopmentTLSRotation = 7 * 24 * time.Hour
)

// DevelopmentSelfSignedTLSSecrets returns the namespace-scoped CA and
// service serving Secrets for the explicit DevelopmentSelfSigned mode. Valid
// unexpired material is retained; otherwise a new CA/leaf set is generated.
// The private CA key is stored only in the operator-managed namespace Secret
// so the operator can rotate leaves without requiring an external PKI.
func DevelopmentSelfSignedTLSSecrets(
	kn *kubernautv1alpha2.Kubernaut,
	existing map[string]*corev1.Secret,
	now time.Time,
) ([]*corev1.Secret, error) {
	if kn == nil {
		return nil, fmt.Errorf("kubernaut is required")
	}
	cfg := kn.Spec.TLS.DevelopmentSelfSigned
	if cfg == nil {
		return nil, fmt.Errorf("tls.developmentSelfSigned is required")
	}
	rotationBefore := defaultDevelopmentTLSRotation
	if cfg.RotationBefore != "" {
		parsed, err := time.ParseDuration(cfg.RotationBefore)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("tls.developmentSelfSigned.rotationBefore must be a non-negative duration")
		}
		rotationBefore = parsed
	}
	caName := cfg.CASecretName
	if caName == "" {
		caName = defaultDevelopmentSelfSignedCASecretName
	}

	activeCACert, caKey, caBundle, err := reusableDevelopmentCA(existing[caName], now, rotationBefore)
	if err != nil {
		return nil, err
	}
	secrets := make([]*corev1.Secret, 0, len(developmentTLSServiceNames)+1)
	secrets = append(secrets, &corev1.Secret{
		ObjectMeta: ObjectMeta(kn, caName, "inter-service-tls"),
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			tlsCACertificateKey: caBundle,
			"ca.key":            caKey,
		},
	})

	for serviceKey, serviceName := range developmentTLSServiceNames {
		secretName := ResolveDevelopmentTLSSecretName(serviceKey)
		secret := existing[secretName]
		if reusableDevelopmentLeaf(secret, activeCACert, serviceName, kn.Namespace, now, rotationBefore) {
			secrets = append(secrets, &corev1.Secret{
				ObjectMeta: ObjectMeta(kn, secretName, developmentTLSComponent(serviceKey)),
				Type:       corev1.SecretTypeTLS,
				Data: map[string][]byte{
					corev1.TLSCertKey:       secret.Data[corev1.TLSCertKey],
					corev1.TLSPrivateKeyKey: secret.Data[corev1.TLSPrivateKeyKey],
				},
			})
			continue
		}
		cert, key, err := signDevelopmentLeaf(activeCACert, caKey, serviceName, kn.Namespace, now)
		if err != nil {
			return nil, fmt.Errorf("generating %s development certificate: %w", serviceKey, err)
		}
		secrets = append(secrets, &corev1.Secret{
			ObjectMeta: ObjectMeta(kn, secretName, developmentTLSComponent(serviceKey)),
			Type:       corev1.SecretTypeTLS,
			Data: map[string][]byte{
				corev1.TLSCertKey:       cert,
				corev1.TLSPrivateKeyKey: key,
			},
		})
	}
	if len(parseCertificatesOrNil(caBundle)) > 1 && allDevelopmentLeavesUseCA(existing, activeCACert, kn.Namespace) {
		secrets[0].Data[tlsCACertificateKey] = append([]byte(nil), activeCACert...)
	}
	return secrets, nil
}

var developmentTLSServiceNames = map[string]string{
	TLSServiceGateway:        "gateway-service",
	TLSServiceDataStorage:    "data-storage-service",
	TLSServiceKubernautAgent: "kubernaut-agent",
	TLSServiceAPIFrontend:    "apifrontend",
	TLSServiceAuthWebhook:    "authwebhook-service",
}

// ResolveDevelopmentTLSSecretName returns the stable serving Secret name for
// one of the documented TLS service keys.
func ResolveDevelopmentTLSSecretName(serviceKey string) string {
	switch serviceKey {
	case TLSServiceGateway:
		return GatewayTLSSecretName
	case TLSServiceDataStorage:
		return DataStorageTLSSecretName
	case TLSServiceKubernautAgent:
		return KubernautAgentTLSSecretName
	case TLSServiceAPIFrontend:
		return APIFrontendTLSSecretName
	case TLSServiceAuthWebhook:
		return "authwebhook-tls"
	default:
		return ""
	}
}

func developmentTLSComponent(serviceKey string) string {
	switch serviceKey {
	case TLSServiceGateway:
		return ComponentGateway
	case TLSServiceDataStorage:
		return ComponentDataStorage
	case TLSServiceKubernautAgent:
		return ComponentKubernautAgent
	case TLSServiceAPIFrontend:
		return ComponentAPIFrontend
	case TLSServiceAuthWebhook:
		return ComponentAuthWebhook
	default:
		return "inter-service-tls"
	}
}

// TLSServiceDNSNames returns the Service DNS identities required for one
// runtime TLS service. It is shared by generated and administrator-managed
// certificate validation so both paths enforce the same identity contract.
func TLSServiceDNSNames(serviceKey, namespace string) []string {
	serviceName := developmentTLSServiceNames[serviceKey]
	if serviceName == "" {
		return nil
	}
	return developmentDNSNames(serviceName, namespace)
}

func reusableDevelopmentCA(secret *corev1.Secret, now time.Time, rotationBefore time.Duration) ([]byte, []byte, []byte, error) {
	if secret != nil {
		certificates, certErr := parseCertificates(secret.Data[tlsCACertificateKey])
		key, keyErr := parseRSAKey(secret.Data["ca.key"])
		if certErr == nil && keyErr == nil && len(certificates) > 0 &&
			rsaKeyMatchesCertificate(key, certificates[0]) &&
			certificates[0].NotAfter.After(now.Add(rotationBefore)) {
			return pemEncode("CERTIFICATE", certificates[0].Raw),
				append([]byte(nil), secret.Data["ca.key"]...),
				append([]byte(nil), secret.Data[tlsCACertificateKey]...), nil
		}
	}
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating development CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating development CA serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "kubernaut development CA"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating development CA certificate: %w", err)
	}
	activePEM := pemEncode("CERTIFICATE", der)
	bundle := append([]byte(nil), activePEM...)
	if secret != nil {
		if previous, err := parseCertificates(secret.Data[tlsCACertificateKey]); err == nil {
			for _, certificate := range previous {
				if certificate.NotAfter.After(now) && !bytes.Equal(certificate.Raw, der) {
					bundle = append(bundle, pemEncode("CERTIFICATE", certificate.Raw)...)
				}
			}
		}
	}
	return activePEM, pemEncode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key)), bundle, nil
}

func rsaKeyMatchesCertificate(key *rsa.PrivateKey, certificate *x509.Certificate) bool {
	publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
	return ok && publicKey.N.Cmp(key.N) == 0 && publicKey.E == key.E
}

func allDevelopmentLeavesUseCA(existing map[string]*corev1.Secret, caPEM []byte, namespace string) bool {
	caCertificates, err := parseCertificates(caPEM)
	if err != nil || len(caCertificates) != 1 {
		return false
	}
	ca := caCertificates[0]
	for serviceKey, serviceName := range developmentTLSServiceNames {
		secret := existing[ResolveDevelopmentTLSSecretName(serviceKey)]
		if secret == nil {
			return false
		}
		certificate, err := parseCertificate(secret.Data[corev1.TLSCertKey])
		if err != nil || certificate.CheckSignatureFrom(ca) != nil {
			return false
		}
		validDNSName := false
		for _, dnsName := range developmentDNSNames(serviceName, namespace) {
			if certificate.VerifyHostname(dnsName) == nil {
				validDNSName = true
				break
			}
		}
		if !validDNSName {
			return false
		}
	}
	return true
}

func parseCertificatesOrNil(data []byte) []*x509.Certificate {
	certificates, err := parseCertificates(data)
	if err != nil {
		return nil
	}
	return certificates
}

func reusableDevelopmentLeaf(secret *corev1.Secret, caPEM []byte, serviceName, namespace string, now time.Time, rotationBefore time.Duration) bool {
	if secret == nil {
		return false
	}
	cert, err := parseCertificate(secret.Data[corev1.TLSCertKey])
	if err != nil || !cert.NotAfter.After(now.Add(rotationBefore)) {
		return false
	}
	if _, err := parseRSAKey(secret.Data[corev1.TLSPrivateKeyKey]); err != nil {
		return false
	}
	ca, err := parseCertificate(caPEM)
	if err != nil {
		return false
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	names := developmentDNSNames(serviceName, namespace)
	for _, name := range names {
		if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: name}); err == nil {
			return true
		}
	}
	return false
}

func signDevelopmentLeaf(caPEM, caKeyPEM []byte, serviceName, namespace string, now time.Time) ([]byte, []byte, error) {
	ca, err := parseCertificate(caPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing CA certificate: %w", err)
	}
	caKey, err := parseRSAKey(caKeyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing CA key: %w", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generating leaf key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, fmt.Errorf("generating leaf serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: serviceName},
		DNSNames:     developmentDNSNames(serviceName, namespace),
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(defaultDevelopmentTLSValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("creating leaf certificate: %w", err)
	}
	return pemEncode("CERTIFICATE", der), pemEncode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key)), nil
}

func developmentDNSNames(serviceName, namespace string) []string {
	return []string{
		serviceName,
		serviceName + "." + namespace,
		serviceName + "." + namespace + ".svc",
		serviceName + "." + namespace + ".svc.cluster.local",
	}
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func parseCertificate(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("certificate is not PEM encoded")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseCertificates(data []byte) ([]*x509.Certificate, error) {
	rest := bytes.TrimSpace(data)
	certificates := make([]*x509.Certificate, 0, 1)
	for len(rest) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil {
			return nil, fmt.Errorf("certificate bundle is not PEM encoded")
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("certificate bundle contains PEM block %q", block.Type)
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certificates = append(certificates, certificate)
		rest = bytes.TrimSpace(remaining)
	}
	if len(certificates) == 0 {
		return nil, fmt.Errorf("certificate bundle is empty")
	}
	return certificates, nil
}

func parseRSAKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("private key is not PEM encoded")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func pemEncode(kind string, data []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: data})
}

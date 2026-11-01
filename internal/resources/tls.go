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
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// TLS source identifiers used internally by resource/controller adapters.
const (
	TLSMaterialSourceServiceCA   = "ServiceCA"
	TLSMaterialSourceManual      = "Manual"
	TLSMaterialSourceCertManager = "CertManager"
	TLSServiceGateway            = "gateway"
	TLSServiceDataStorage        = "datastorage"
	TLSServiceKubernautAgent     = "kubernautagent"
	TLSServiceAPIFrontend        = "apifrontend"
	TLSServiceAuthWebhook        = "authwebhook"
	TLSServiceFleetMetadataCache = "fleetmetadatacache"
	tlsCACertificateKey          = "ca.crt"
)

// TLSMaterial is the resolved runtime certificate reference set. The
// controller validates referenced Secrets or creates the material only when
// OwnsSecrets is true. Manual and cert-manager Secrets remain
// outside the operator's ownership boundary.
type TLSMaterial struct {
	Source                  string
	InternalCASecretName    string
	InternalCAConfigMapName string
	ServiceTLSSecretNames   map[string]string
	OwnsSecrets             bool
}

// ResolveTLSMaterial validates the explicit source contract and resolves the
// serving Secret names used by Services and Deployments. An empty source is
// reserved for the optional OpenShift service-CA adapter; it is never a
// generic plaintext mode.
func ResolveTLSMaterial(kn *kubernautv1alpha2.Kubernaut) (TLSMaterial, error) {
	if kn == nil {
		return TLSMaterial{}, fmt.Errorf("kubernaut is required")
	}

	switch kn.Spec.TLS.Mode {
	case "":
		return openShiftServiceTLSMaterial(kn), nil
	case kubernautv1alpha2.TLSModeServiceCA:
		return openShiftServiceTLSMaterial(kn), nil
	case kubernautv1alpha2.TLSModeCertManager:
		return resolveCertManagerTLS(kn)
	case kubernautv1alpha2.TLSModeManual:
		return resolveManualTLS(kn)
	default:
		return TLSMaterial{}, fmt.Errorf("tls.mode %q is unsupported", kn.Spec.TLS.Mode)
	}
}

func openShiftServiceTLSMaterial(kn *kubernautv1alpha2.Kubernaut) TLSMaterial {
	serviceNames := map[string]string{
		TLSServiceGateway:        GatewayTLSSecretName,
		TLSServiceDataStorage:    DataStorageTLSSecretName,
		TLSServiceKubernautAgent: KubernautAgentTLSSecretName,
		TLSServiceAPIFrontend:    APIFrontendTLSSecretName,
		TLSServiceAuthWebhook:    "authwebhook-tls",
	}
	addFleetMetadataCacheTLSSecret(kn, serviceNames)
	return TLSMaterial{
		Source:                TLSMaterialSourceServiceCA,
		InternalCASecretName:  InterServiceCAConfigMapName,
		ServiceTLSSecretNames: serviceNames,
	}
}

func resolveCertManagerTLS(kn *kubernautv1alpha2.Kubernaut) (TLSMaterial, error) {
	cfg := kn.Spec.TLS.CertManager
	provisioning := CertManagerTLSProvisioningEnabled(kn)
	if cfg == nil {
		cfg = &kubernautv1alpha2.CertManagerTLSConfig{}
	}
	if cfg.EffectiveIssuerRef().Name == "" && !provisioning {
		return TLSMaterial{}, fmt.Errorf("tls.certManager.issuer.name is required")
	}
	if CertManagerTLSProvisioningEnabled(kn) {
		settings := certManagerProvisioningValuesFor(kn, cfg)
		return TLSMaterial{
			Source:                TLSMaterialSourceCertManager,
			InternalCASecretName:  settings.caSecretName,
			ServiceTLSSecretNames: settings.serviceSecretNames,
		}, nil
	}
	names, err := requiredServiceTLSSecretNames(cfg.ServiceTLSSecretNames, kn.Spec.FleetMetadataCacheEnabled())
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
}

func resolveManualTLS(kn *kubernautv1alpha2.Kubernaut) (TLSMaterial, error) {
	// Manual mode uses stable inter-service-ca and serving Secret names by
	// default. Its full read-only validation is handled by the controller.
	cfg := kn.Spec.TLS.Manual
	names := defaultCertManagerServiceSecretNames(kn)
	if cfg == nil {
		return TLSMaterial{
			Source:                  TLSMaterialSourceManual,
			InternalCAConfigMapName: InterServiceCAConfigMapName,
			ServiceTLSSecretNames:   names,
		}, nil
	}
	for key, value := range cfg.ServiceTLSSecretNames {
		if value != "" {
			names[key] = value
		}
	}
	if cfg.InternalCASecretName == "" {
		return TLSMaterial{
			Source:                  TLSMaterialSourceManual,
			InternalCAConfigMapName: InterServiceCAConfigMapName,
			ServiceTLSSecretNames:   names,
		}, nil
	}
	return TLSMaterial{
		Source:                TLSMaterialSourceManual,
		InternalCASecretName:  cfg.InternalCASecretName,
		ServiceTLSSecretNames: names,
	}, nil
}

func addFleetMetadataCacheTLSSecret(kn *kubernautv1alpha2.Kubernaut, serviceNames map[string]string) {
	if kn != nil && kn.Spec.FleetMetadataCacheEnabled() {
		serviceNames[TLSServiceFleetMetadataCache] = FleetMetadataCacheTLSSecretName
	}
}

// ValidateInternalCASecret validates the public CA payload referenced by an
// manual or cert-manager TLS source.
func ValidateInternalCASecret(secret *corev1.Secret) error {
	return ValidateInternalCASecretForSource(secret, "")
}

// ValidateInternalCASecretForSource validates the public CA payload selected
// by a runtime TLS source. cert-manager output may store its CA certificate
// under tls.crt, while manual material follows the operator contract and uses
// ca.crt.
func ValidateInternalCASecretForSource(secret *corev1.Secret, source string) error {
	_, err := InternalCAPEMForSource(secret, source)
	return err
}

// InternalCAPEMForSource returns the validated public CA bundle for a runtime
// TLS source. The returned bytes are copied so callers cannot mutate the
// Secret's backing data.
func InternalCAPEMForSource(secret *corev1.Secret, source string) ([]byte, error) {
	_, data, err := internalCAMaterialForSource(secret, source)
	return append([]byte(nil), data...), err
}

// internalCAMaterialForSource validates the source once and exposes parsed CA
// certificates to the serving-chain verifier. Public PEM callers receive a copy.
func internalCAMaterialForSource(secret *corev1.Secret, source string) ([]*x509.Certificate, []byte, error) {
	if secret == nil {
		return nil, nil, fmt.Errorf("internal CA Secret is required")
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
			return nil, nil, fmt.Errorf("secret %q is missing ca.crt or tls.crt", secret.Name)
		}
		return nil, nil, fmt.Errorf("secret %q is missing ca.crt", secret.Name)
	}
	certificates, err := parseCertificates(data)
	if err != nil {
		return nil, nil, fmt.Errorf("secret %q %s is invalid: %w", secret.Name, key, err)
	}
	for _, certificate := range certificates {
		if !certificate.IsCA {
			return nil, nil, fmt.Errorf("secret %q %s contains a non-CA certificate", secret.Name, key)
		}
	}
	return certificates, data, nil
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

// ValidateDataStorageSigningTLSSecret enforces the chart's AU-9 signing-key
// contract: a standard TLS Secret containing a matching RSA-2048 keypair.
// This certificate is not used for network serving, so SAN validation is not
// applicable.
func ValidateDataStorageSigningTLSSecret(secret *corev1.Secret) error {
	if secret == nil {
		return fmt.Errorf("serving TLS Secret is required")
	}
	if len(secret.Data[corev1.TLSCertKey]) == 0 || len(secret.Data[corev1.TLSPrivateKeyKey]) == 0 {
		return fmt.Errorf("secret %q must contain tls.crt and tls.key", secret.Name)
	}
	if _, err := parseCertificate(secret.Data[corev1.TLSCertKey]); err != nil {
		return fmt.Errorf("secret %q certificate is invalid: %w", secret.Name, err)
	}
	keyPair, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		return fmt.Errorf("secret %q private key is invalid: %w", secret.Name, err)
	}
	rsaKey, ok := keyPair.PrivateKey.(*rsa.PrivateKey)
	if !ok || rsaKey.N.BitLen() != 2048 {
		return fmt.Errorf("secret %q must contain an RSA-2048 private key", secret.Name)
	}
	// ValidateServingTLSSecret already verified key/certificate correspondence.
	return nil
}

// ValidateServingTLSSecretForService validates a serving Secret against the
// selected internal CA and the DNS identities of one Kubernetes Service. This
// keeps manual and cert-manager material from being accepted
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
	caCertificates, _, err := internalCAMaterialForSource(ca, source)
	if err != nil {
		return err
	}
	leaf, err := parseCertificate(secret.Data[corev1.TLSCertKey])
	if err != nil {
		return fmt.Errorf("secret %q certificate is invalid: %w", secret.Name, err)
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

// ValidateAuthWebhookTLSSecret validates the AuthWebhook serving Secret's
// keypair and both the chart and operator Service DNS identities. AuthWebhook
// certificates may be signed by a public/external issuer in cert-manager or
// manual mode, so this check intentionally does not require the inter-service
// CA.
func ValidateAuthWebhookTLSSecret(secret *corev1.Secret, namespace string) error {
	if err := ValidateServingTLSSecret(secret); err != nil {
		return err
	}
	certificate, err := parseCertificate(secret.Data[corev1.TLSCertKey])
	if err != nil {
		return fmt.Errorf("secret %q certificate is invalid: %w", secret.Name, err)
	}
	for _, dnsName := range TLSServiceDNSNames(TLSServiceAuthWebhook, namespace) {
		if certificate.VerifyHostname(dnsName) == nil {
			return nil
		}
	}
	return fmt.Errorf("secret %q certificate does not cover AuthWebhook Service DNS names", secret.Name)
}

// AuthWebhookCABundle returns the CA chain embedded by hook-mode generation or
// cert-manager in the AuthWebhook Secret. It is used only for webhook client
// trust; the inter-service CA remains a separate trust domain.
func AuthWebhookCABundle(secret *corev1.Secret) ([]byte, error) {
	if secret == nil || len(secret.Data[tlsCACertificateKey]) == 0 {
		return nil, fmt.Errorf("AuthWebhook Secret %q is missing ca.crt", secretName(secret))
	}
	certificates, err := parseCertificates(secret.Data[tlsCACertificateKey])
	if err != nil {
		return nil, fmt.Errorf("AuthWebhook Secret %q has an invalid ca.crt: %w", secretName(secret), err)
	}
	for _, certificate := range certificates {
		if !certificate.IsCA {
			return nil, fmt.Errorf("AuthWebhook Secret %q ca.crt contains a non-CA certificate", secretName(secret))
		}
	}
	return append([]byte(nil), secret.Data[tlsCACertificateKey]...), nil
}

func secretName(secret *corev1.Secret) string {
	if secret == nil {
		return ""
	}
	return secret.Name
}

func requiredServiceTLSSecretNames(input map[string]string, includeFleetMetadataCache bool) (map[string]string, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("serviceTLSSecretNames is required")
	}
	keys := []string{TLSServiceGateway, TLSServiceDataStorage, TLSServiceKubernautAgent, TLSServiceAPIFrontend, TLSServiceAuthWebhook}
	if includeFleetMetadataCache {
		keys = append(keys, TLSServiceFleetMetadataCache)
	}
	required := strings.Join(keys, " ")
	result := make(map[string]string, len(input))
	for _, key := range keys {
		value := input[key]
		if value == "" {
			return nil, fmt.Errorf("serviceTLSSecretNames[%q] is required; required keys: %s", key, required)
		}
		result[key] = value
	}
	return result, nil
}

var tlsServiceNames = map[string]string{
	TLSServiceGateway:            "gateway-service",
	TLSServiceDataStorage:        "data-storage-service",
	TLSServiceKubernautAgent:     "kubernaut-agent",
	TLSServiceAPIFrontend:        "apifrontend",
	TLSServiceAuthWebhook:        "authwebhook-service",
	TLSServiceFleetMetadataCache: "fleetmetadatacache-service",
}

// TLSServiceDNSNames returns the Service DNS identities required for one
// runtime TLS service. All source modes validate the same identity contract.
func TLSServiceDNSNames(serviceKey, namespace string) []string {
	serviceName := tlsServiceNames[serviceKey]
	if serviceName == "" {
		return nil
	}
	names := []string{
		serviceName,
		serviceName + "." + namespace,
		serviceName + "." + namespace + ".svc",
		serviceName + "." + namespace + ".svc.cluster.local",
	}
	if serviceName == "authwebhook-service" {
		names = append(names,
			"authwebhook",
			"authwebhook."+namespace,
			"authwebhook."+namespace+".svc",
			"authwebhook."+namespace+".svc.cluster.local",
		)
	}
	return names
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

func pemEncode(kind string, data []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: data})
}

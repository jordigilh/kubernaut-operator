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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
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
	TLSServiceFleetMetadataCache             = "fleetmetadatacache"
	tlsCACertificateKey                      = "ca.crt"
	defaultDevelopmentSelfSignedCASecretName = "kubernaut-internal-ca" //nolint:gosec // this is a Secret object name, not credential material
)

// TLSMaterial is the resolved runtime certificate reference set. The
// controller validates referenced Secrets or creates the material only when
// OwnsSecrets is true. Administrator-managed and cert-manager Secrets remain
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
	case kubernautv1alpha2.TLSModeHook:
		return developmentTLSMaterial(kn), nil
	case kubernautv1alpha2.TLSModeAdministratorManaged:
		return resolveAdministratorManagedTLS(kn)
	case kubernautv1alpha2.TLSModeCertManager, kubernautv1alpha2.TLSModeHelmCertManager:
		return resolveCertManagerTLS(kn)
	case kubernautv1alpha2.TLSModeDevelopmentSelfSigned:
		return resolveDevelopmentSelfSignedTLS(kn)
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
		Source:                TLSMaterialSourceOpenShiftServiceCA,
		InternalCASecretName:  InterServiceCAConfigMapName,
		ServiceTLSSecretNames: serviceNames,
	}
}

func resolveAdministratorManagedTLS(kn *kubernautv1alpha2.Kubernaut) (TLSMaterial, error) {
	cfg := kn.Spec.TLS.AdministratorManaged
	if cfg == nil {
		return TLSMaterial{}, fmt.Errorf("tls.administratorManaged is required for mode %q", kn.Spec.TLS.Mode)
	}
	names, err := requiredServiceTLSSecretNames(cfg.ServiceTLSSecretNames, kn.Spec.FleetMetadataCacheEnabled())
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
}

func resolveCertManagerTLS(kn *kubernautv1alpha2.Kubernaut) (TLSMaterial, error) {
	cfg := kn.Spec.TLS.CertManager
	if cfg == nil && kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeHelmCertManager {
		cfg = &kubernautv1alpha2.CertManagerTLSConfig{}
	}
	if cfg == nil {
		return TLSMaterial{}, fmt.Errorf("tls.certManager is required for mode %q", kn.Spec.TLS.Mode)
	}
	if cfg.EffectiveIssuerRef().Name == "" && kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeCertManager && !CertManagerTLSProvisioningEnabled(kn) {
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

func resolveDevelopmentSelfSignedTLS(kn *kubernautv1alpha2.Kubernaut) (TLSMaterial, error) {
	cfg := kn.Spec.TLS.DevelopmentSelfSigned
	if cfg == nil {
		return TLSMaterial{}, fmt.Errorf("tls.developmentSelfSigned is required for mode %q", kn.Spec.TLS.Mode)
	}
	caName := cfg.CASecretName
	if caName == "" {
		caName = defaultDevelopmentSelfSignedCASecretName
	}
	material := developmentTLSMaterial(kn)
	material.InternalCASecretName = caName
	return material, nil
}

func resolveManualTLS(kn *kubernautv1alpha2.Kubernaut) (TLSMaterial, error) {
	// The chart's manual mode uses the stable inter-service-ca ConfigMap and
	// stable serving Secret names. Its full read-only validation is handled
	// by the controller adapter; this resolver only establishes the names.
	cfg := kn.Spec.TLS.AdministratorManaged
	names := defaultCertManagerServiceSecretNames(kn)
	if cfg == nil {
		return TLSMaterial{
			Source:                  TLSMaterialSourceAdministratorManaged,
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
			Source:                  TLSMaterialSourceAdministratorManaged,
			InternalCAConfigMapName: InterServiceCAConfigMapName,
			ServiceTLSSecretNames:   names,
		}, nil
	}
	return TLSMaterial{
		Source:                TLSMaterialSourceAdministratorManaged,
		InternalCASecretName:  cfg.InternalCASecretName,
		ServiceTLSSecretNames: names,
	}, nil
}

func developmentTLSMaterial(kn *kubernautv1alpha2.Kubernaut) TLSMaterial {
	serviceNames := map[string]string{
		TLSServiceGateway:        GatewayTLSSecretName,
		TLSServiceDataStorage:    DataStorageTLSSecretName,
		TLSServiceKubernautAgent: KubernautAgentTLSSecretName,
		TLSServiceAPIFrontend:    APIFrontendTLSSecretName,
		TLSServiceAuthWebhook:    "authwebhook-tls",
	}
	addFleetMetadataCacheTLSSecret(kn, serviceNames)
	return TLSMaterial{
		Source:                TLSMaterialSourceDevelopmentSelfSigned,
		InternalCASecretName:  defaultDevelopmentSelfSignedCASecretName,
		ServiceTLSSecretNames: serviceNames,
		OwnsSecrets:           true,
	}
}

func addFleetMetadataCacheTLSSecret(kn *kubernautv1alpha2.Kubernaut, serviceNames map[string]string) {
	if kn != nil && kn.Spec.FleetMetadataCacheEnabled() {
		serviceNames[TLSServiceFleetMetadataCache] = FleetMetadataCacheTLSSecretName
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

// ValidateDataStorageSigningTLSSecret enforces the chart's AU-9 signing-key
// contract: a standard TLS Secret containing a matching RSA-2048 keypair.
// This certificate is not used for network serving, so SAN validation is not
// applicable.
func ValidateDataStorageSigningTLSSecret(secret *corev1.Secret) error {
	if err := ValidateServingTLSSecret(secret); err != nil {
		return err
	}
	certificate, err := parseCertificate(secret.Data[corev1.TLSCertKey])
	if err != nil {
		return fmt.Errorf("secret %q certificate is invalid: %w", secret.Name, err)
	}
	signer, err := parsePrivateKey(secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		return fmt.Errorf("secret %q private key is invalid: %w", secret.Name, err)
	}
	rsaKey, ok := signer.(*rsa.PrivateKey)
	if !ok || rsaKey.N.BitLen() != 2048 {
		return fmt.Errorf("secret %q must contain an RSA-2048 private key", secret.Name)
	}
	if !signerMatchesCertificate(signer, certificate) {
		return fmt.Errorf("secret %q certificate and private key do not match", secret.Name)
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

const (
	defaultDevelopmentTLSValidity = 30 * 24 * time.Hour
	defaultDevelopmentTLSRotation = 7 * 24 * time.Hour
)

type developmentTLSSettings struct {
	rotationBefore            time.Duration
	caName                    string
	extraSANs                 []string
	includeSigningCertificate bool
	signingSecretName         string
}

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
	settings, err := developmentTLSSettingsFor(kn)
	if err != nil {
		return nil, err
	}

	activeCACert, caKey, caBundle, err := reusableDevelopmentCA(existing[settings.caName], now, settings.rotationBefore)
	if err != nil {
		return nil, err
	}
	serviceNames := developmentTLSServiceNamesFor(kn)
	secrets := make([]*corev1.Secret, 0, len(serviceNames)+1+boolToInt(settings.includeSigningCertificate))
	secrets = append(secrets, &corev1.Secret{
		ObjectMeta: ObjectMeta(kn, settings.caName, "inter-service-tls"),
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			tlsCACertificateKey: caBundle,
			"ca.key":            caKey,
		},
	})

	servingSecrets, err := developmentServingTLSSecrets(kn, existing, now, activeCACert, caKey, caBundle, settings)
	if err != nil {
		return nil, err
	}
	secrets = append(secrets, servingSecrets...)
	if settings.includeSigningCertificate {
		signingSecret, err := developmentSigningTLSSecret(kn, existing[settings.signingSecretName], now, settings)
		if err != nil {
			return nil, err
		}
		secrets = append(secrets, signingSecret)
	}
	if len(parseCertificatesOrNil(caBundle)) > 1 && allDevelopmentLeavesUseCA(existing, activeCACert, kn, settings.extraSANs) {
		secrets[0].Data[tlsCACertificateKey] = append([]byte(nil), activeCACert...)
	}
	return secrets, nil
}

func developmentTLSSettingsFor(kn *kubernautv1alpha2.Kubernaut) (developmentTLSSettings, error) {
	cfg := kn.Spec.TLS.DevelopmentSelfSigned
	if cfg == nil && kn.Spec.TLS.Mode != kubernautv1alpha2.TLSModeHook {
		return developmentTLSSettings{}, fmt.Errorf("tls.developmentSelfSigned is required")
	}
	settings := developmentTLSSettings{
		rotationBefore: defaultDevelopmentTLSRotation,
		caName:         defaultDevelopmentSelfSignedCASecretName,
		extraSANs:      developmentExtraSANs(kn),
		includeSigningCertificate: (kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeHook ||
			kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeDevelopmentSelfSigned) &&
			(kn.Spec.DataStorage.SigningCert == nil || kn.Spec.DataStorage.SigningCert.SecretName == ""),
		signingSecretName: DataStorageSigningSecretName(kn),
	}
	if cfg != nil {
		if cfg.RotationBefore != "" {
			parsed, err := time.ParseDuration(cfg.RotationBefore)
			if err != nil || parsed < 0 {
				return developmentTLSSettings{}, fmt.Errorf("tls.developmentSelfSigned.rotationBefore must be a non-negative duration")
			}
			settings.rotationBefore = parsed
		}
		if cfg.CASecretName != "" {
			settings.caName = cfg.CASecretName
		}
	}
	if settings.signingSecretName == "" {
		settings.signingSecretName = defaultCertManagerSigningSecretName
	}
	return settings, nil
}

func developmentServingTLSSecrets(
	kn *kubernautv1alpha2.Kubernaut,
	existing map[string]*corev1.Secret,
	now time.Time,
	activeCACert, caKey, caBundle []byte,
	settings developmentTLSSettings,
) ([]*corev1.Secret, error) {
	serviceNames := developmentTLSServiceNamesFor(kn)
	secrets := make([]*corev1.Secret, 0, len(serviceNames))
	for serviceKey, serviceName := range serviceNames {
		secretName := ResolveDevelopmentTLSSecretName(serviceKey)
		secret := existing[secretName]
		leafExtraSANs := developmentLeafExtraSANs(serviceKey, settings.extraSANs)
		if reusableDevelopmentLeaf(secret, activeCACert, serviceName, kn.Namespace, now, settings.rotationBefore, leafExtraSANs) {
			secrets = append(secrets, developmentServingTLSSecret(kn, serviceKey, secretName,
				secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey], caBundle, caKey))
			continue
		}
		cert, key, err := signDevelopmentLeaf(activeCACert, caKey, serviceName, kn.Namespace, now, leafExtraSANs)
		if err != nil {
			return nil, fmt.Errorf("generating %s development certificate: %w", serviceKey, err)
		}
		secrets = append(secrets, developmentServingTLSSecret(kn, serviceKey, secretName, cert, key, caBundle, caKey))
	}
	return secrets, nil
}

func developmentServingTLSSecret(
	kn *kubernautv1alpha2.Kubernaut,
	serviceKey, secretName string,
	cert, key, caBundle, caKey []byte,
) *corev1.Secret {
	data := map[string][]byte{
		corev1.TLSCertKey:       cert,
		corev1.TLSPrivateKeyKey: key,
	}
	if serviceKey == TLSServiceAuthWebhook {
		data[tlsCACertificateKey] = append([]byte(nil), caBundle...)
		data["ca.key"] = append([]byte(nil), caKey...)
	}
	return &corev1.Secret{
		ObjectMeta: ObjectMeta(kn, secretName, developmentTLSComponent(serviceKey)),
		Type:       corev1.SecretTypeTLS,
		Data:       data,
	}
}

func developmentSigningTLSSecret(
	kn *kubernautv1alpha2.Kubernaut,
	existing *corev1.Secret,
	now time.Time,
	settings developmentTLSSettings,
) (*corev1.Secret, error) {
	if reusableDevelopmentSigningCertificate(existing, now, settings.rotationBefore) {
		return &corev1.Secret{
			ObjectMeta: ObjectMeta(kn, settings.signingSecretName, ComponentDataStorage),
			Type:       corev1.SecretTypeTLS,
			Data: map[string][]byte{
				corev1.TLSCertKey:       append([]byte(nil), existing.Data[corev1.TLSCertKey]...),
				corev1.TLSPrivateKeyKey: append([]byte(nil), existing.Data[corev1.TLSPrivateKeyKey]...),
			},
		}, nil
	}
	cert, key, err := generateDevelopmentSigningCertificate(now)
	if err != nil {
		return nil, fmt.Errorf("generating datastorage signing certificate: %w", err)
	}
	return &corev1.Secret{
		ObjectMeta: ObjectMeta(kn, settings.signingSecretName, ComponentDataStorage),
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert,
			corev1.TLSPrivateKeyKey: key,
		},
	}, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func developmentExtraSANs(kn *kubernautv1alpha2.Kubernaut) []string {
	if kn == nil || kn.Spec.TLS.Hooks == nil {
		return nil
	}
	return append([]string(nil), kn.Spec.TLS.Hooks.TLSCerts.ExtraSANs...)
}

func developmentLeafExtraSANs(serviceKey string, extraSANs []string) []string {
	if serviceKey == TLSServiceAuthWebhook {
		return nil
	}
	return extraSANs
}

var developmentTLSServiceNames = map[string]string{
	TLSServiceGateway:            "gateway-service",
	TLSServiceDataStorage:        "data-storage-service",
	TLSServiceKubernautAgent:     "kubernaut-agent",
	TLSServiceAPIFrontend:        "apifrontend",
	TLSServiceAuthWebhook:        "authwebhook-service",
	TLSServiceFleetMetadataCache: "fleetmetadatacache-service",
}

func developmentTLSServiceNamesFor(kn *kubernautv1alpha2.Kubernaut) map[string]string {
	serviceNames := make(map[string]string, len(developmentTLSServiceNames))
	for serviceKey, serviceName := range developmentTLSServiceNames {
		if serviceKey == TLSServiceFleetMetadataCache && (kn == nil || !kn.Spec.FleetMetadataCacheEnabled()) {
			continue
		}
		serviceNames[serviceKey] = serviceName
	}
	return serviceNames
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
	case TLSServiceFleetMetadataCache:
		return FleetMetadataCacheTLSSecretName
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
	case TLSServiceFleetMetadataCache:
		return ComponentFleetMetadataCache
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
	return developmentServiceDNSNames(serviceName, namespace)
}

func reusableDevelopmentCA(secret *corev1.Secret, now time.Time, rotationBefore time.Duration) ([]byte, []byte, []byte, error) {
	if active, key, bundle, ok := existingDevelopmentCA(secret, now, rotationBefore); ok {
		return active, key, bundle, nil
	}
	return generateDevelopmentCA(secret, now)
}

func existingDevelopmentCA(secret *corev1.Secret, now time.Time, rotationBefore time.Duration) ([]byte, []byte, []byte, bool) {
	if secret == nil {
		return nil, nil, nil, false
	}
	certificates, certErr := parseCertificates(secret.Data[tlsCACertificateKey])
	key, keyErr := parsePrivateKey(secret.Data["ca.key"])
	if certErr != nil || keyErr != nil || len(certificates) == 0 {
		return nil, nil, nil, false
	}
	if !signerMatchesCertificate(key, certificates[0]) || !certificates[0].NotAfter.After(now.Add(rotationBefore)) {
		return nil, nil, nil, false
	}
	return pemEncode("CERTIFICATE", certificates[0].Raw),
		append([]byte(nil), secret.Data["ca.key"]...),
		append([]byte(nil), secret.Data[tlsCACertificateKey]...), true
}

func generateDevelopmentCA(secret *corev1.Secret, now time.Time) ([]byte, []byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
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
	bundle := developmentCABundle(secret, now, der, activePEM)
	keyPEM, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encoding development CA key: %w", err)
	}
	return activePEM, pemEncode("EC PRIVATE KEY", keyPEM), bundle, nil
}

func developmentCABundle(secret *corev1.Secret, now time.Time, activeDER, activePEM []byte) []byte {
	bundle := append([]byte(nil), activePEM...)
	if secret == nil {
		return bundle
	}
	previous, err := parseCertificates(secret.Data[tlsCACertificateKey])
	if err != nil {
		return bundle
	}
	for _, certificate := range previous {
		if certificate.NotAfter.After(now) && !bytes.Equal(certificate.Raw, activeDER) {
			bundle = append(bundle, pemEncode("CERTIFICATE", certificate.Raw)...)
		}
	}
	return bundle
}

func signerMatchesCertificate(key crypto.Signer, certificate *x509.Certificate) bool {
	certificatePublicKey, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return false
	}
	signerPublicKey, err := x509.MarshalPKIXPublicKey(key.Public())
	return err == nil && bytes.Equal(certificatePublicKey, signerPublicKey)
}

func allDevelopmentLeavesUseCA(existing map[string]*corev1.Secret, caPEM []byte, kn *kubernautv1alpha2.Kubernaut, extraSANs []string) bool {
	caCertificates, err := parseCertificates(caPEM)
	if err != nil || len(caCertificates) != 1 {
		return false
	}
	ca := caCertificates[0]
	for serviceKey, serviceName := range developmentTLSServiceNamesFor(kn) {
		secret := existing[ResolveDevelopmentTLSSecretName(serviceKey)]
		if secret == nil {
			return false
		}
		certificate, err := parseCertificate(secret.Data[corev1.TLSCertKey])
		if err != nil || certificate.CheckSignatureFrom(ca) != nil {
			return false
		}
		validDNSName := false
		serviceSANs := developmentDNSNamesWithExtras(serviceName, kn.Namespace, developmentLeafExtraSANs(serviceKey, extraSANs))
		for _, dnsName := range serviceSANs {
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

func reusableDevelopmentLeaf(secret *corev1.Secret, caPEM []byte, serviceName, namespace string, now time.Time, rotationBefore time.Duration, extraSANs []string) bool {
	if secret == nil {
		return false
	}
	cert, err := parseCertificate(secret.Data[corev1.TLSCertKey])
	if err != nil || !cert.NotAfter.After(now.Add(rotationBefore)) {
		return false
	}
	key, err := parsePrivateKey(secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil || !signerMatchesCertificate(key, cert) {
		return false
	}
	ca, err := parseCertificate(caPEM)
	if err != nil {
		return false
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	names := developmentDNSNamesWithExtras(serviceName, namespace, extraSANs)
	for _, name := range names {
		if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: name}); err == nil {
			return true
		}
	}
	return false
}

func signDevelopmentLeaf(caPEM, caKeyPEM []byte, serviceName, namespace string, now time.Time, extraSANs []string) ([]byte, []byte, error) {
	ca, err := parseCertificate(caPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing CA certificate: %w", err)
	}
	caKey, err := parsePrivateKey(caKeyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing CA key: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generating leaf key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, fmt.Errorf("generating leaf serial: %w", err)
	}
	dnsNames, ipAddresses := developmentDNSNamesAndIPs(serviceName, namespace, extraSANs)
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: serviceName},
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(defaultDevelopmentTLSValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("creating leaf certificate: %w", err)
	}
	keyPEM, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding development leaf key: %w", err)
	}
	return pemEncode("CERTIFICATE", der), pemEncode("EC PRIVATE KEY", keyPEM), nil
}

func reusableDevelopmentSigningCertificate(secret *corev1.Secret, now time.Time, rotationBefore time.Duration) bool {
	if secret == nil {
		return false
	}
	certificate, err := parseCertificate(secret.Data[corev1.TLSCertKey])
	if err != nil || !certificate.NotAfter.After(now.Add(rotationBefore)) {
		return false
	}
	key, err := parseRSAKey(secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil || key.N.BitLen() != 2048 {
		return false
	}
	publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
	return ok && publicKey.N.Cmp(key.N) == 0 && publicKey.E == key.E
}

func generateDevelopmentSigningCertificate(now time.Time) ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generating signing key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, fmt.Errorf("generating signing serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: defaultCertManagerSigningCertificateName},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(defaultDevelopmentTLSValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("creating signing certificate: %w", err)
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

func developmentServiceDNSNames(serviceName, namespace string) []string {
	names := developmentDNSNames(serviceName, namespace)
	if serviceName == "authwebhook-service" {
		names = append(names, developmentDNSNames("authwebhook", namespace)...)
	}
	return names
}

func developmentDNSNamesWithExtras(serviceName, namespace string, extraSANs []string) []string {
	dnsNames, _ := developmentDNSNamesAndIPs(serviceName, namespace, extraSANs)
	return dnsNames
}

func developmentDNSNamesAndIPs(serviceName, namespace string, extraSANs []string) ([]string, []net.IP) {
	dnsNames := developmentServiceDNSNames(serviceName, namespace)
	ipAddresses := make([]net.IP, 0, 1)
	for _, extra := range extraSANs {
		extra = strings.TrimSpace(extra)
		if extra == "" {
			continue
		}
		if ip := net.ParseIP(extra); ip != nil {
			ipAddresses = append(ipAddresses, ip)
			continue
		}
		dnsNames = append(dnsNames, extra)
	}
	if len(extraSANs) > 0 {
		loopback := net.ParseIP("127.0.0.1")
		found := false
		for _, ip := range ipAddresses {
			if ip.Equal(loopback) {
				found = true
				break
			}
		}
		if !found {
			ipAddresses = append(ipAddresses, loopback)
		}
	}
	return dnsNames, ipAddresses
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

func parsePrivateKey(data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("private key is not PEM encoded")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("private key type %T is not a signing key", key)
	}
	return signer, nil
}

func pemEncode(kind string, data []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: data})
}

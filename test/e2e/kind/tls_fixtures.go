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

package kind

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

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

type manualTLSSelection string

const (
	manualTLSSelectionManual manualTLSSelection = "manual"
	manualTLSSelectionAdmin  manualTLSSelection = "administrator-managed"
	manualTLSAdminPrefix                        = "admin"
	tlsCACertificateKey                         = "ca.crt"
	tlsTrustProbeRole                           = "tls-probe"
)

type manualTLSFixture struct {
	mode                  kubernautv1alpha2.TLSMode
	caSecretName          string
	caConfigMapName       string
	serviceTLSSecretNames map[string]string
	signingSecretName     string
	secrets               map[string]*corev1.Secret
	configMaps            map[string]*corev1.ConfigMap
	webhookCABundle       []byte
}

var (
	activeManualTLSSelection = manualTLSSelectionManual
	manualTLSFixtures        = make(map[manualTLSSelection]manualTLSFixture, 2)
)

func manualTLSFixtureFor(selection manualTLSSelection) (manualTLSFixture, error) {
	fixture, ok := manualTLSFixtures[selection]
	if !ok {
		return manualTLSFixture{}, fmt.Errorf("manual TLS fixture %q has not been initialized", selection)
	}
	return fixture, nil
}

func setActiveManualTLSSelection(selection manualTLSSelection) {
	activeManualTLSSelection = selection
}

func activeManualTLSFixture() (manualTLSFixture, error) {
	return manualTLSFixtureFor(activeManualTLSSelection)
}

// TLSProbeLabels returns labels that make the disposable TLS trust probe an
// allowed peer of operator-managed provider policies.
func TLSProbeLabels() map[string]string {
	labels := managedProbeLabels(tlsTrustProbeRole)
	labels["kubernaut.ai/tls-probe"] = "true"
	return labels
}

func ensureManualAdminTLSFixtures(ctx context.Context) error {
	for _, definition := range []struct {
		selection manualTLSSelection
		mode      kubernautv1alpha2.TLSMode
		prefix    string
	}{
		{selection: manualTLSSelectionManual, mode: kubernautv1alpha2.TLSModeManual, prefix: "manual"},
		{selection: manualTLSSelectionAdmin, mode: kubernautv1alpha2.TLSModeAdministratorManaged, prefix: "admin"},
	} {
		fixture, err := newManualTLSFixture(definition.mode, definition.prefix, kubernautNamespace)
		if err != nil {
			return fmt.Errorf("creating %s TLS fixture: %w", definition.selection, err)
		}
		manualTLSFixtures[definition.selection] = fixture
		objects := make([]interface{}, 0, len(fixture.secrets)+len(fixture.configMaps))
		for _, secret := range fixture.secrets {
			objects = append(objects, secret)
		}
		for _, configMap := range fixture.configMaps {
			objects = append(objects, configMap)
		}
		if err := applyYAML(ctx, objects...); err != nil {
			return fmt.Errorf("applying %s TLS fixture: %w", definition.selection, err)
		}
	}
	return ensureManualTLSWebhookFixtures(ctx, manualTLSSelectionManual)
}

func ensureManualTLSWebhookFixtures(ctx context.Context, selection manualTLSSelection) error {
	fixture, err := manualTLSFixtureFor(selection)
	if err != nil {
		return err
	}
	if err := applyYAML(ctx, manualMutatingWebhookFixture(fixture), manualValidatingWebhookFixture(fixture)); err != nil {
		return fmt.Errorf("applying %s TLS webhook fixtures: %w", selection, err)
	}
	return nil
}

func assertManualTLSFixtureUnchanged(ctx context.Context, selection manualTLSSelection) error {
	fixture, err := manualTLSFixtureFor(selection)
	if err != nil {
		return err
	}
	for name, expected := range fixture.secrets {
		output, getErr := kubectl(ctx, "get", "secret", name, "-n", kubernautNamespace, "-o", "json")
		if getErr != nil {
			return fmt.Errorf("reading administrator-owned TLS Secret %q: %w", name, getErr)
		}
		live := &corev1.Secret{}
		if unmarshalErr := yaml.Unmarshal([]byte(output), live); unmarshalErr != nil {
			return fmt.Errorf("decoding administrator-owned TLS Secret %q: %w", name, unmarshalErr)
		}
		if len(live.OwnerReferences) != 0 {
			return fmt.Errorf("administrator-owned TLS Secret %q was adopted", name)
		}
		if stringMapBytes(live.Data) != stringMapBytes(expected.Data) {
			return fmt.Errorf("administrator-owned TLS Secret %q was mutated", name)
		}
	}
	for name, expected := range fixture.configMaps {
		output, getErr := kubectl(ctx, "get", "configmap", name, "-n", kubernautNamespace, "-o", "json")
		if getErr != nil {
			return fmt.Errorf("reading administrator-owned TLS ConfigMap %q: %w", name, getErr)
		}
		live := &corev1.ConfigMap{}
		if unmarshalErr := yaml.Unmarshal([]byte(output), live); unmarshalErr != nil {
			return fmt.Errorf("decoding administrator-owned TLS ConfigMap %q: %w", name, unmarshalErr)
		}
		if len(live.OwnerReferences) != 0 {
			return fmt.Errorf("administrator-owned TLS ConfigMap %q was adopted", name)
		}
		if stringMap(live.Data) != stringMap(expected.Data) {
			return fmt.Errorf("administrator-owned TLS ConfigMap %q was mutated", name)
		}
	}
	return nil
}

func assertManualTLSWebhookBundlesUnchanged(ctx context.Context, fixture manualTLSFixture) error {
	for _, webhookType := range []string{"mutating", "validating"} {
		resource := webhookType + "webhookconfiguration"
		name := kubernautNamespace + "-authwebhook-" + webhookType
		output, err := kubectl(ctx, "get", resource, name, "-o", "json")
		if err != nil {
			return fmt.Errorf("reading administrator-owned %s webhook configuration: %w", webhookType, err)
		}
		var live struct {
			Metadata struct {
				OwnerReferences []metav1.OwnerReference `json:"ownerReferences"`
			} `json:"metadata"`
			Webhooks []struct {
				ClientConfig struct {
					CABundle []byte `json:"caBundle"`
				} `json:"clientConfig"`
			} `json:"webhooks"`
		}
		if err := yaml.Unmarshal([]byte(output), &live); err != nil {
			return fmt.Errorf("decoding administrator-owned %s webhook configuration: %w", webhookType, err)
		}
		if len(live.Metadata.OwnerReferences) != 0 {
			return fmt.Errorf("administrator-owned %s webhook configuration was adopted", webhookType)
		}
		if len(live.Webhooks) == 0 {
			return fmt.Errorf("administrator-owned %s webhook configuration has no entries", webhookType)
		}
		for index, webhook := range live.Webhooks {
			if string(webhook.ClientConfig.CABundle) != string(fixture.webhookCABundle) {
				return fmt.Errorf("administrator-owned %s webhook caBundle %d was mutated", webhookType, index)
			}
		}
	}
	return nil
}

func corruptManualTLSSecret(fixture manualTLSFixture, serviceKey string) (*corev1.Secret, error) {
	secretName, ok := fixture.serviceTLSSecretNames[serviceKey]
	if !ok {
		return nil, fmt.Errorf("manual TLS fixture has no service %q", serviceKey)
	}
	secret, ok := fixture.secrets[secretName]
	if !ok {
		return nil, fmt.Errorf("manual TLS fixture has no Secret %q", secretName)
	}
	corrupt := secret.DeepCopy()
	corrupt.Data[corev1.TLSCertKey] = []byte("not-a-certificate")
	return corrupt, nil
}

func stringMapBytes(input map[string][]byte) string {
	encoded, err := yaml.Marshal(input)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func stringMap(input map[string]string) string {
	encoded, err := yaml.Marshal(input)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func verifyTLSWorkloadTrust(ctx context.Context, caKey string) error {
	const probeName = "tls-trust-probe"
	trustVolume := corev1.Volume{
		Name: "tls-ca",
		VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "inter-service-trust-bundle"},
			Items:                []corev1.KeyToPath{{Key: tlsCACertificateKey, Path: tlsCACertificateKey}},
		}},
	}
	if configuredTLS == tlsManualAdmin && activeManualTLSSelection == manualTLSSelectionManual {
		fixture, err := activeManualTLSFixture()
		if err != nil {
			return err
		}
		trustVolume.VolumeSource = corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: fixture.caConfigMapName},
			Items:                []corev1.KeyToPath{{Key: caKey, Path: tlsCACertificateKey}},
		}}
	}
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      probeName,
			Namespace: kubernautNamespace,
			Labels:    TLSProbeLabels(),
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:    "curl",
				Image:   probeImage,
				Command: []string{"sh", "-c"},
				Args:    []string{"sleep 3600"},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "tls-ca",
					MountPath: "/etc/tls",
					ReadOnly:  true,
				}},
			}},
			Volumes: []corev1.Volume{trustVolume},
		},
	}
	if err := applyYAML(ctx, pod); err != nil {
		return err
	}
	defer func() {
		_, _ = kubectl(
			ctx, "delete", "pod", probeName, "-n", kubernautNamespace,
			"--ignore-not-found=true", "--wait=false",
		) //nolint:errcheck
	}()
	if _, err := kubectl(
		ctx, "wait", "--for=condition=Ready", "pod/"+probeName,
		"-n", kubernautNamespace, "--timeout=5m",
	); err != nil {
		return fmt.Errorf("waiting for TLS trust probe: %w", err)
	}
	if _, err := kubectl(
		ctx, "exec", "-n", kubernautNamespace, probeName, "--", "curl", "--silent", "--show-error",
		"--output", "/dev/null", "--connect-timeout", "5", "--max-time", "10", "--cacert", "/etc/tls/ca.crt",
		"https://data-storage-service:8443/readyz",
	); err != nil {
		return fmt.Errorf("verifying TLS trust to data-storage-service: %w", err)
	}
	return nil
}

func newManualTLSFixture(mode kubernautv1alpha2.TLSMode, prefix, namespace string) (manualTLSFixture, error) {
	now := time.Now().UTC()
	caCertificate, caKey, caPEM, err := generateManualCA(now, prefix)
	if err != nil {
		return manualTLSFixture{}, err
	}

	serviceNames := manualTLSServiceSecretNames(prefix)
	secrets := make(map[string]*corev1.Secret, len(serviceNames)+2)
	configMaps := make(map[string]*corev1.ConfigMap, 1)
	caSecretName := ""
	caConfigMapName := ""
	if mode == kubernautv1alpha2.TLSModeManual {
		caConfigMapName = resources.InterServiceCAConfigMapName
		configMaps[caConfigMapName] = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      caConfigMapName,
				Namespace: namespace,
				Labels:    manualTLSFixtureLabels(prefix),
			},
			Data: map[string]string{tlsCACertificateKey: string(caPEM)},
		}
	} else {
		caSecretName = manualTLSCASecretName(prefix)
		secrets[caSecretName] = &corev1.Secret{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      caSecretName,
				Namespace: namespace,
				Labels:    manualTLSFixtureLabels(prefix),
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{tlsCACertificateKey: caPEM},
		}
	}

	serviceBases := map[string]string{
		"gateway":        "gateway-service",
		"datastorage":    "data-storage-service",
		"kubernautagent": "kubernaut-agent",
		"apifrontend":    "apifrontend",
		"authwebhook":    "authwebhook-service",
	}
	for serviceKey, secretName := range serviceNames {
		leafCert, leafKey, err := generateManualLeaf(
			now, caCertificate, caKey, serviceBases[serviceKey], namespace, serviceKey,
		)
		if err != nil {
			return manualTLSFixture{}, fmt.Errorf("generating %s serving certificate: %w", serviceKey, err)
		}
		data := map[string][]byte{
			corev1.TLSCertKey:       leafCert,
			corev1.TLSPrivateKeyKey: leafKey,
		}
		if serviceKey == "authwebhook" {
			data[tlsCACertificateKey] = append([]byte(nil), caPEM...)
		}
		secrets[secretName] = &corev1.Secret{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
				Labels:    manualTLSFixtureLabels(prefix),
			},
			Type: corev1.SecretTypeTLS,
			Data: data,
		}
	}

	signingSecretName := manualTLSSigningSecretName(prefix)
	signingCert, signingKey, err := generateManualSigningCertificate(now, prefix)
	if err != nil {
		return manualTLSFixture{}, fmt.Errorf("generating DataStorage signing certificate: %w", err)
	}
	secrets[signingSecretName] = &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      signingSecretName,
			Namespace: namespace,
			Labels:    manualTLSFixtureLabels(prefix),
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       signingCert,
			corev1.TLSPrivateKeyKey: signingKey,
		},
	}

	return manualTLSFixture{
		mode:                  mode,
		caSecretName:          caSecretName,
		serviceTLSSecretNames: serviceNames,
		signingSecretName:     signingSecretName,
		secrets:               secrets,
		configMaps:            configMaps,
		webhookCABundle:       append([]byte(nil), caPEM...),
		caConfigMapName:       caConfigMapName,
	}, nil
}

func manualTLSServiceSecretNames(prefix string) map[string]string {
	if prefix == string(manualTLSSelectionManual) {
		return map[string]string{
			"gateway":        "gateway-tls",
			"datastorage":    "datastorage-tls",
			"kubernautagent": "kubernautagent-tls",
			"apifrontend":    "apifrontend-tls",
			"authwebhook":    "authwebhook-tls",
		}
	}
	return map[string]string{
		"gateway":        prefix + "-gateway-tls",
		"datastorage":    prefix + "-datastorage-tls",
		"kubernautagent": prefix + "-kubernautagent-tls",
		"apifrontend":    prefix + "-apifrontend-tls",
		"authwebhook":    prefix + "-authwebhook-tls",
	}
}

func manualTLSCASecretName(prefix string) string {
	return prefix + "-internal-ca"
}

func manualTLSSigningSecretName(prefix string) string {
	if prefix == string(manualTLSSelectionManual) {
		return "datastorage-signing-cert"
	}
	return prefix + "-datastorage-signing-tls"
}

func manualTLSFixtureLabels(prefix string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "manual-tls-e2e",
		"kubernaut.ai/tls-fixture":     prefix,
	}
}

func manualMutatingWebhookFixture(fixture manualTLSFixture) *admissionregistrationv1.MutatingWebhookConfiguration {
	webhooks := make([]admissionregistrationv1.MutatingWebhook, 3)
	for index := range webhooks {
		webhooks[index] = admissionregistrationv1.MutatingWebhook{
			Name:                    fmt.Sprintf("mutating-%d.manual-tls.kubernaut.ai", index),
			AdmissionReviewVersions: []string{"v1"},
			SideEffects:             ptr.To(admissionregistrationv1.SideEffectClassNone),
			FailurePolicy:           ptr.To(admissionregistrationv1.Fail),
			ClientConfig:            manualWebhookClientConfig(fixture),
		}
	}
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{APIVersion: "admissionregistration.k8s.io/v1", Kind: "MutatingWebhookConfiguration"},
		ObjectMeta: metav1.ObjectMeta{
			Name: kubernautNamespace + "-authwebhook-mutating",
			// Explicitly entrust the configuration (not the CA/Secrets) to the
			// operator. Manual TLS input is not automatic adoption permission.
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kubernaut-operator",
				"app.kubernetes.io/part-of":    "kubernaut",
				"app.kubernetes.io/instance":   "kubernaut",
				"kubernaut.ai/tls-fixture":     string(fixture.mode),
			},
			Annotations: map[string]string{"kubernaut.ai/owner-namespace": kubernautNamespace},
		},
		Webhooks: webhooks,
	}
}

func manualValidatingWebhookFixture(fixture manualTLSFixture) *admissionregistrationv1.ValidatingWebhookConfiguration {
	webhooks := make([]admissionregistrationv1.ValidatingWebhook, 4)
	for index := range webhooks {
		webhooks[index] = admissionregistrationv1.ValidatingWebhook{
			Name:                    fmt.Sprintf("validating-%d.manual-tls.kubernaut.ai", index),
			AdmissionReviewVersions: []string{"v1"},
			SideEffects:             ptr.To(admissionregistrationv1.SideEffectClassNone),
			FailurePolicy:           ptr.To(admissionregistrationv1.Fail),
			ClientConfig:            manualWebhookClientConfig(fixture),
		}
	}
	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{APIVersion: "admissionregistration.k8s.io/v1", Kind: "ValidatingWebhookConfiguration"},
		ObjectMeta: metav1.ObjectMeta{
			Name: kubernautNamespace + "-authwebhook-validating",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kubernaut-operator",
				"app.kubernetes.io/part-of":    "kubernaut",
				"app.kubernetes.io/instance":   "kubernaut",
				"kubernaut.ai/tls-fixture":     string(fixture.mode),
			},
			Annotations: map[string]string{"kubernaut.ai/owner-namespace": kubernautNamespace},
		},
		Webhooks: webhooks,
	}
}

func manualWebhookClientConfig(fixture manualTLSFixture) admissionregistrationv1.WebhookClientConfig {
	return admissionregistrationv1.WebhookClientConfig{
		Service: &admissionregistrationv1.ServiceReference{
			Namespace: kubernautNamespace,
			Name:      "authwebhook-service",
			Path:      ptr.To("/healthz"),
			Port:      ptr.To(int32(443)),
		},
		CABundle: append([]byte(nil), fixture.webhookCABundle...),
	}
}

func generateManualCA(now time.Time, prefix string) (*x509.Certificate, *ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating CA key: %w", err)
	}
	serial, err := manualSerial()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating CA serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: prefix + " administrator CA"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating CA certificate: %w", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parsing CA certificate: %w", err)
	}
	return certificate, key, pemEncodeCertificate(der), nil
}

func generateManualLeaf(
	now time.Time,
	ca *x509.Certificate,
	caKey *ecdsa.PrivateKey,
	serviceName, namespace, serviceKey string,
) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generating serving key: %w", err)
	}
	serial, err := manualSerial()
	if err != nil {
		return nil, nil, fmt.Errorf("generating serving serial: %w", err)
	}
	dnsNames := []string{
		serviceName,
		serviceName + "." + namespace,
		serviceName + "." + namespace + ".svc",
		serviceName + "." + namespace + ".svc.cluster.local",
	}
	if serviceKey == "authwebhook" {
		dnsNames = append(dnsNames,
			"authwebhook",
			"authwebhook."+namespace,
			"authwebhook."+namespace+".svc",
			"authwebhook."+namespace+".svc.cluster.local",
		)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: serviceName},
		DNSNames:     dnsNames,
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("creating serving certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding serving key: %w", err)
	}
	return pemEncodeCertificate(der), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

func generateManualSigningCertificate(now time.Time, prefix string) ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generating signing key: %w", err)
	}
	serial, err := manualSerial()
	if err != nil {
		return nil, nil, fmt.Errorf("generating signing serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: prefix + " DataStorage signing certificate"},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("creating signing certificate: %w", err)
	}
	return pemEncodeCertificate(der), pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}), nil
}

func manualSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}

func pemEncodeCertificate(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

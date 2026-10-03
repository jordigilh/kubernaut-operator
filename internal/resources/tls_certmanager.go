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
	"fmt"
	"net"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	defaultCertManagerBootstrapIssuerName    = "kubernaut-interservice-bootstrap-issuer"
	defaultCertManagerCACertificateName      = "kubernaut-interservice-ca"
	defaultCertManagerCAIssuerName           = "kubernaut-interservice-ca-issuer"
	defaultCertManagerCASecretName           = "kubernaut-interservice-ca-secret" //nolint:gosec // this is a Secret object name, not a credential
	defaultCertManagerSigningCertificateName = "datastorage-signing-cert"
	defaultCertManagerSigningSecretName      = "datastorage-signing-cert"
	defaultCertManagerDuration               = "8760h"
	defaultCertManagerRenewBefore            = "720h"
	defaultCertManagerCADuration             = "87600h"
	defaultCertManagerGroup                  = "cert-manager.io"
)

// certManagerTLSLeaf identifies one operator-owned inter-service identity.
type certManagerTLSLeaf struct {
	serviceKey      string
	certificateName string
	dnsNameBase     string
	secretName      string
}

var certManagerTLSLeaves = []certManagerTLSLeaf{
	{serviceKey: TLSServiceGateway, certificateName: GatewayTLSSecretName, dnsNameBase: "gateway-service"},
	{serviceKey: TLSServiceDataStorage, certificateName: DataStorageTLSSecretName, dnsNameBase: "data-storage-service"},
	{serviceKey: TLSServiceKubernautAgent, certificateName: KubernautAgentTLSSecretName, dnsNameBase: "kubernaut-agent"},
	{serviceKey: TLSServiceAPIFrontend, certificateName: APIFrontendTLSSecretName, dnsNameBase: "apifrontend"},
}

// CertManagerTLSProvisioningEnabled reports whether the operator should create
// cert-manager Issuer/Certificate resources. The lower-case Helm mode enables
// provisioning by definition; the title-case operator mode remains
// reference-only unless its explicit provisioning block opts in.
func CertManagerTLSProvisioningEnabled(kn *kubernautv1alpha2.Kubernaut) bool {
	if kn == nil {
		return false
	}
	if kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeHelmCertManager {
		return true
	}
	cfg := kn.Spec.TLS.CertManager
	if cfg == nil || cfg.Provisioning == nil {
		return false
	}
	return cfg.Provisioning.Enabled == nil || *cfg.Provisioning.Enabled
}

// DataStorageSigningSecretName resolves the chart-compatible AU-9 signing
// Secret name. Explicit development and Helm hook modes use the dedicated
// RSA signing Secret; legacy OpenShift/cert-manager reference modes retain the
// prior service-certificate fallback unless an explicit SigningCert is set.
func DataStorageSigningSecretName(kn *kubernautv1alpha2.Kubernaut) string {
	if kn == nil {
		return ""
	}
	if signing := kn.Spec.DataStorage.SigningCert; signing != nil && signing.SecretName != "" {
		return signing.SecretName
	}
	if CertManagerTLSProvisioningEnabled(kn) {
		cfg := kn.Spec.TLS.CertManager
		if cfg == nil {
			cfg = &kubernautv1alpha2.CertManagerTLSConfig{}
		}
		return certManagerProvisioningValuesFor(kn, cfg).signingSecretName
	}
	if kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeHook ||
		kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeDevelopmentSelfSigned ||
		kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeManual {
		return defaultCertManagerSigningSecretName
	}
	return ""
}

// CertManagerTLSResources builds the cert-manager resources required by the
// chart's cert-manager mode. It deliberately returns no objects for the
// reference-only contract. Generated Secrets are not returned: cert-manager,
// not the operator, owns and rotates them.
func CertManagerTLSResources(kn *kubernautv1alpha2.Kubernaut) ([]*unstructured.Unstructured, error) {
	if kn == nil {
		return nil, fmt.Errorf("kubernaut is required")
	}
	if !CertManagerTLSProvisioningEnabled(kn) {
		return nil, nil
	}

	cfg := kn.Spec.TLS.CertManager
	if cfg == nil {
		cfg = &kubernautv1alpha2.CertManagerTLSConfig{}
	}
	settings := certManagerProvisioningValuesFor(kn, cfg)
	issuerRef := certManagerExternalIssuerRef(kn, cfg)

	objects := certManagerBootstrapResources(kn, settings)
	objects = append(objects, certManagerLeafResources(kn, settings)...)
	objects = append(objects,
		certManagerAuthWebhookCertificate(kn, settings, issuerRef),
		certManagerSigningCertificate(kn, settings, issuerRef),
	)
	return objects, nil
}

func certManagerBootstrapResources(kn *kubernautv1alpha2.Kubernaut, settings certManagerProvisioningValues) []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		certManagerIssuer(kn, settings.bootstrapIssuerName, map[string]interface{}{
			"selfSigned": map[string]interface{}{},
		}),
		certManagerCertificate(kn, settings.caCertificateName, map[string]interface{}{
			"isCA":        true,
			"commonName":  settings.caCertificateName,
			"secretName":  settings.caSecretName,
			"duration":    settings.caDuration,
			"renewBefore": settings.renewBefore,
			"privateKey": map[string]interface{}{
				"algorithm": "ECDSA",
				"size":      int64(256),
			},
			"issuerRef": map[string]interface{}{
				"name":  settings.bootstrapIssuerName,
				"kind":  "Issuer",
				"group": defaultCertManagerGroup,
			},
		}),
		certManagerIssuer(kn, settings.caIssuerName, map[string]interface{}{
			"ca": map[string]interface{}{"secretName": settings.caSecretName},
		}),
	}
}

func certManagerLeafResources(kn *kubernautv1alpha2.Kubernaut, settings certManagerProvisioningValues) []*unstructured.Unstructured {
	leafs := make([]certManagerTLSLeaf, len(certManagerTLSLeaves))
	copy(leafs, certManagerTLSLeaves)
	if kn.Spec.FleetMetadataCacheEnabled() {
		leafs = append(leafs, certManagerTLSLeaf{
			serviceKey:      "fleetmetadatacache",
			certificateName: "fleetmetadatacache-tls",
			dnsNameBase:     "fleetmetadatacache-service",
			secretName:      "fleetmetadatacache-tls",
		})
	}
	objects := make([]*unstructured.Unstructured, 0, len(leafs))
	for _, leaf := range leafs {
		if secretName, ok := settings.serviceSecretNames[leaf.serviceKey]; ok && secretName != "" {
			leaf.secretName = secretName
		}
		objects = append(objects, certManagerLeafCertificate(kn, leaf, settings))
	}
	return objects
}

func certManagerAuthWebhookCertificate(kn *kubernautv1alpha2.Kubernaut, settings certManagerProvisioningValues, issuerRef map[string]interface{}) *unstructured.Unstructured {
	return certManagerCertificate(kn, "authwebhook-cert", map[string]interface{}{
		"secretName":  settings.serviceSecretNames[TLSServiceAuthWebhook],
		"duration":    settings.duration,
		"renewBefore": settings.renewBefore,
		"privateKey":  map[string]interface{}{"algorithm": "RSA", "size": int64(2048)},
		"issuerRef":   issuerRef,
		"dnsNames": []interface{}{
			"authwebhook",
			"authwebhook." + kn.Namespace,
			"authwebhook." + kn.Namespace + ".svc",
			"authwebhook." + kn.Namespace + ".svc.cluster.local",
			"authwebhook-service",
			"authwebhook-service." + kn.Namespace,
			"authwebhook-service." + kn.Namespace + ".svc",
			"authwebhook-service." + kn.Namespace + ".svc.cluster.local",
		},
	})
}

func certManagerSigningCertificate(kn *kubernautv1alpha2.Kubernaut, settings certManagerProvisioningValues, issuerRef map[string]interface{}) *unstructured.Unstructured {
	return certManagerCertificate(kn, settings.signingCertificateName, map[string]interface{}{
		"secretName":  settings.signingSecretName,
		"duration":    settings.duration,
		"renewBefore": settings.renewBefore,
		"commonName":  settings.signingCertificateName,
		"privateKey":  map[string]interface{}{"algorithm": "RSA", "size": int64(2048)},
		"issuerRef":   issuerRef,
	})
}

type certManagerProvisioningValues struct {
	bootstrapIssuerName    string
	caCertificateName      string
	caIssuerName           string
	caSecretName           string
	duration               string
	renewBefore            string
	caDuration             string
	extraSANs              []string
	serviceSecretNames     map[string]string
	signingCertificateName string
	signingSecretName      string
}

func certManagerProvisioningValuesFor(kn *kubernautv1alpha2.Kubernaut, cfg *kubernautv1alpha2.CertManagerTLSConfig) certManagerProvisioningValues {
	p := cfg.Provisioning
	settings := certManagerProvisioningValues{
		bootstrapIssuerName:    defaultCertManagerBootstrapIssuerName,
		caCertificateName:      defaultCertManagerCACertificateName,
		caIssuerName:           defaultCertManagerCAIssuerName,
		caSecretName:           firstNonEmpty(cfg.InternalCASecretName, defaultCertManagerCASecretName),
		duration:               defaultCertManagerDuration,
		renewBefore:            defaultCertManagerRenewBefore,
		caDuration:             defaultCertManagerCADuration,
		serviceSecretNames:     defaultCertManagerServiceSecretNames(),
		signingCertificateName: defaultCertManagerSigningCertificateName,
		signingSecretName:      defaultCertManagerSigningSecretName,
	}
	if p != nil {
		settings.bootstrapIssuerName = firstNonEmpty(p.BootstrapIssuerName, settings.bootstrapIssuerName)
		settings.caCertificateName = firstNonEmpty(p.InternalCACertificateName, settings.caCertificateName)
		settings.caIssuerName = firstNonEmpty(p.InternalCAIssuerName, settings.caIssuerName)
		settings.caSecretName = firstNonEmpty(p.InternalCASecretName, settings.caSecretName)
		settings.duration = firstNonEmpty(p.Duration, settings.duration)
		settings.renewBefore = firstNonEmpty(p.RenewBefore, settings.renewBefore)
		settings.caDuration = firstNonEmpty(p.InternalCADuration, settings.caDuration)
		settings.extraSANs = append([]string(nil), p.ExtraSANs...)
	}
	for key, name := range cfg.ServiceTLSSecretNames {
		if name != "" {
			settings.serviceSecretNames[key] = name
		}
	}
	if sc := kn.Spec.DataStorage.SigningCert; sc != nil && sc.SecretName != "" {
		settings.signingSecretName = sc.SecretName
	}
	if p != nil {
		settings.signingCertificateName = firstNonEmpty(p.SigningCertificateName, settings.signingCertificateName)
		if sc := kn.Spec.DataStorage.SigningCert; sc == nil || sc.SecretName == "" {
			settings.signingSecretName = firstNonEmpty(p.SigningCertificateSecretName, settings.signingSecretName)
		}
	}
	return settings
}

func defaultCertManagerServiceSecretNames() map[string]string {
	return map[string]string{
		TLSServiceGateway:        GatewayTLSSecretName,
		TLSServiceDataStorage:    DataStorageTLSSecretName,
		TLSServiceKubernautAgent: KubernautAgentTLSSecretName,
		TLSServiceAPIFrontend:    APIFrontendTLSSecretName,
		TLSServiceAuthWebhook:    "authwebhook-tls",
	}
}

func certManagerExternalIssuerRef(kn *kubernautv1alpha2.Kubernaut, cfg *kubernautv1alpha2.CertManagerTLSConfig) map[string]interface{} {
	issuer := cfg.EffectiveIssuerRef()
	kind := issuer.Kind
	if kind == "" {
		kind = "ClusterIssuer"
		if kn.Spec.TLS.Mode == kubernautv1alpha2.TLSModeCertManager {
			kind = "Issuer"
		}
	}
	group := firstNonEmpty(issuer.Group, defaultCertManagerGroup)
	return map[string]interface{}{
		"name":  issuer.Name,
		"kind":  kind,
		"group": group,
	}
}

func certManagerLeafCertificate(kn *kubernautv1alpha2.Kubernaut, leaf certManagerTLSLeaf, settings certManagerProvisioningValues) *unstructured.Unstructured {
	dnsNames := []interface{}{
		leaf.dnsNameBase,
		leaf.dnsNameBase + "." + kn.Namespace,
		leaf.dnsNameBase + "." + kn.Namespace + ".svc",
		leaf.dnsNameBase + "." + kn.Namespace + ".svc.cluster.local",
	}
	ipAddresses := make([]interface{}, 0, 1)
	for _, extra := range settings.extraSANs {
		extra = strings.TrimSpace(extra)
		if extra == "" {
			continue
		}
		if net.ParseIP(extra) != nil {
			ipAddresses = append(ipAddresses, extra)
			continue
		}
		dnsNames = append(dnsNames, extra)
	}
	if len(settings.extraSANs) > 0 {
		foundLoopback := false
		for _, ip := range ipAddresses {
			if ip == "127.0.0.1" {
				foundLoopback = true
				break
			}
		}
		if !foundLoopback {
			ipAddresses = append(ipAddresses, "127.0.0.1")
		}
	}
	spec := map[string]interface{}{
		"secretName":  leaf.secretName,
		"duration":    settings.duration,
		"renewBefore": settings.renewBefore,
		"privateKey":  map[string]interface{}{"algorithm": "ECDSA", "size": int64(256)},
		"issuerRef": map[string]interface{}{
			"name":  settings.caIssuerName,
			"kind":  "Issuer",
			"group": defaultCertManagerGroup,
		},
		"usages":   []interface{}{"server auth", "client auth"},
		"dnsNames": dnsNames,
	}
	if len(ipAddresses) > 0 {
		spec["ipAddresses"] = ipAddresses
	}
	return certManagerCertificate(kn, leaf.certificateName, spec)
}

func certManagerIssuer(kn *kubernautv1alpha2.Kubernaut, name string, spec map[string]interface{}) *unstructured.Unstructured {
	return certManagerObject(kn, name, "Issuer", spec)
}

func certManagerCertificate(kn *kubernautv1alpha2.Kubernaut, name string, spec map[string]interface{}) *unstructured.Unstructured {
	return certManagerObject(kn, name, "Certificate", spec)
}

func certManagerObject(kn *kubernautv1alpha2.Kubernaut, name, kind string, spec map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": defaultCertManagerGroup + "/v1",
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": kn.Namespace,
			"labels":    stringMapToInterface(CommonLabels(kn)),
		},
		"spec": spec,
	}}
}

func stringMapToInterface(input map[string]string) map[string]interface{} {
	result := make(map[string]interface{}, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

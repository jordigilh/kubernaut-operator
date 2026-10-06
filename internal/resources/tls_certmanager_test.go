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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("cert-manager TLS provisioning", func() {
	It("uses the legacy issuer when issuerRef contains only the API-defaulted group", func() {
		cfg := &kubernautv1alpha2.CertManagerTLSConfig{
			Issuer: kubernautv1alpha2.TLSIssuerRef{
				Name:  "legacy-issuer",
				Kind:  "Issuer",
				Group: "cert-manager.io",
			},
			// The Kubernetes API applies this default even when issuerRef was
			// submitted as an empty object. It must not hide the legacy field.
			IssuerRef: kubernautv1alpha2.TLSIssuerRef{Group: "cert-manager.io"},
		}

		Expect(cfg.EffectiveIssuerRef()).To(Equal(cfg.Issuer))
	})

	It("UT-TLS-491-004 [AC-6, CM-6, SC-12; SOC2 CC6, CC8; ASVS v5.0.0-V13.3.1, v5.0.0-V15.2.4] does not build operator-owned cert-manager resources when provisioning is disabled", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				Issuer:                kubernautv1alpha2.TLSIssuerRef{Name: "customer-issuer"},
				InternalCASecretName:  "customer-ca",
				ServiceTLSSecretNames: validServiceTLSSecretNames(),
			},
		}

		objects, err := CertManagerTLSResources(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(BeEmpty())
	})

	It("UT-TLS-491-005 [IA-5, SC-8, SC-12, SC-13, SC-17; SOC2 CC6, CC7; ASVS v5.0.0-V11.1.1, v5.0.0-V11.1.2, v5.0.0-V12.1.1, v5.0.0-V12.1.2, v5.0.0-V12.1.3, v5.0.0-V13.2.1] builds the dedicated CA, service leaves, webhook certificate, and RSA signing certificate", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				IssuerRef: kubernautv1alpha2.TLSIssuerRef{
					Name:  "customer-issuer",
					Kind:  "ClusterIssuer",
					Group: "cert-manager.io",
				},
				Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{
					Enabled:     boolPtr(true),
					ExtraSANs:   []string{"localhost", "gateway.example.test"},
					Duration:    "720h",
					RenewBefore: "120h",
				},
			},
		}

		objects, err := CertManagerTLSResources(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(HaveLen(9))

		byName := make(map[string]*unstructured.Unstructured, len(objects))
		for _, object := range objects {
			byName[object.GetName()] = object
			Expect(object.GetAPIVersion()).To(Equal("cert-manager.io/v1"))
			Expect(object.GetNamespace()).To(Equal(kn.Namespace))
			Expect(object.GetLabels()).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "kubernaut-operator"))
		}

		bootstrap := byName["kubernaut-interservice-bootstrap-issuer"]
		Expect(bootstrap).NotTo(BeNil())
		Expect(bootstrap.Object).To(HaveKeyWithValue("kind", "Issuer"))
		Expect(bootstrap.Object).To(HaveKey("spec"))
		Expect(bootstrap.Object["spec"]).To(Equal(map[string]interface{}{"selfSigned": map[string]interface{}{}}))

		ca := byName["kubernaut-interservice-ca"]
		Expect(ca).NotTo(BeNil())
		caSpec := ca.Object["spec"].(map[string]interface{})
		Expect(caSpec["isCA"]).To(BeTrue())
		Expect(caSpec["secretName"]).To(Equal("kubernaut-interservice-ca-secret"))
		Expect(caSpec["duration"]).To(Equal("87600h"), "the internal root keeps the upstream ten-year lifetime")
		Expect(caSpec["privateKey"]).To(Equal(map[string]interface{}{"algorithm": "ECDSA", "size": int64(256)}))

		issuer := byName["kubernaut-interservice-ca-issuer"]
		Expect(issuer).NotTo(BeNil())
		Expect(issuer.Object["spec"]).To(Equal(map[string]interface{}{
			"ca": map[string]interface{}{"secretName": "kubernaut-interservice-ca-secret"},
		}))

		gateway := byName["gateway-tls"]
		Expect(gateway).NotTo(BeNil())
		gatewaySpec := gateway.Object["spec"].(map[string]interface{})
		Expect(gatewaySpec["secretName"]).To(Equal("gateway-tls"))
		Expect(gatewaySpec["issuerRef"]).To(Equal(map[string]interface{}{
			"name": "kubernaut-interservice-ca-issuer", "kind": "Issuer", "group": "cert-manager.io",
		}))
		Expect(gatewaySpec["privateKey"]).To(Equal(map[string]interface{}{"algorithm": "ECDSA", "size": int64(256)}))
		Expect(gatewaySpec["usages"]).To(Equal([]interface{}{"server auth", "client auth"}))
		Expect(gatewaySpec["dnsNames"]).To(Equal([]interface{}{
			"gateway-service", "gateway-service." + kn.Namespace,
			"gateway-service." + kn.Namespace + ".svc",
			"gateway-service." + kn.Namespace + ".svc.cluster.local",
			"localhost", "gateway.example.test",
		}))
		Expect(gatewaySpec["ipAddresses"]).To(Equal([]interface{}{"127.0.0.1"}))

		webhook := byName["authwebhook-cert"]
		Expect(webhook).NotTo(BeNil())
		webhookSpec := webhook.Object["spec"].(map[string]interface{})
		Expect(webhookSpec["secretName"]).To(Equal("authwebhook-tls"))
		Expect(webhookSpec["issuerRef"]).To(Equal(map[string]interface{}{
			"name": "customer-issuer", "kind": "ClusterIssuer", "group": "cert-manager.io",
		}))
		Expect(webhookSpec["privateKey"]).To(Equal(map[string]interface{}{"algorithm": "RSA", "size": int64(2048)}))

		signing := byName["datastorage-signing-cert"]
		Expect(signing).NotTo(BeNil())
		signingSpec := signing.Object["spec"].(map[string]interface{})
		Expect(signingSpec["secretName"]).To(Equal("datastorage-signing-cert"))
		Expect(signingSpec["commonName"]).To(Equal("datastorage-signing-cert"))
		Expect(signingSpec["privateKey"]).To(Equal(map[string]interface{}{"algorithm": "RSA", "size": int64(2048)}))
	})

	It("UT-TLS-491-006 [CM-6, SC-8; SOC2 CC6, CC8; ASVS v5.0.0-V13.3.1, v5.0.0-V15.2.4] uses configured output names and does not include disabled optional components", func() {
		kn := testKubernaut()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				Issuer:               kubernautv1alpha2.TLSIssuerRef{Name: "customer-issuer"},
				InternalCASecretName: "migrated-ca",
				ServiceTLSSecretNames: map[string]string{
					TLSServiceGateway:        "migrated-gateway-tls",
					TLSServiceDataStorage:    "migrated-datastorage-tls",
					TLSServiceKubernautAgent: "migrated-agent-tls",
					TLSServiceAPIFrontend:    "migrated-apifrontend-tls",
					TLSServiceAuthWebhook:    "migrated-authwebhook-tls",
				},
				Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{
					Enabled:                      boolPtr(true),
					InternalCASecretName:         "provisioned-ca",
					SigningCertificateSecretName: "migrated-signing-cert",
				},
			},
		}

		objects, err := CertManagerTLSResources(kn)
		Expect(err).NotTo(HaveOccurred())
		byName := make(map[string]*unstructured.Unstructured, len(objects))
		for _, object := range objects {
			byName[object.GetName()] = object
		}

		caSpec := byName["kubernaut-interservice-ca"].Object["spec"].(map[string]interface{})
		Expect(caSpec["secretName"]).To(Equal("provisioned-ca"))
		gatewaySpec := byName["gateway-tls"].Object["spec"].(map[string]interface{})
		Expect(gatewaySpec["secretName"]).To(Equal("migrated-gateway-tls"))
		signingSpec := byName["datastorage-signing-cert"].Object["spec"].(map[string]interface{})
		Expect(signingSpec["secretName"]).To(Equal("migrated-signing-cert"))
	})

	It("provisions and resolves the FMC serving Certificate when FMC is enabled", func() {
		kn := testKubernaut()
		kn.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:    &testFMCEnabled,
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: ComponentFleetMetadataCache},
		}
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				IssuerRef:    kubernautv1alpha2.TLSIssuerRef{Name: "customer-issuer"},
				Provisioning: &kubernautv1alpha2.CertManagerTLSProvisioning{Enabled: boolPtr(true)},
			},
		}

		material, err := ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.ServiceTLSSecretNames).To(HaveKeyWithValue(TLSServiceFleetMetadataCache, FleetMetadataCacheTLSSecretName))

		objects, err := CertManagerTLSResources(kn)
		Expect(err).NotTo(HaveOccurred())
		var fmc *unstructured.Unstructured
		for _, object := range objects {
			if object.GetName() == FleetMetadataCacheTLSSecretName {
				fmc = object
				break
			}
		}
		Expect(fmc).NotTo(BeNil())
		spec, found, err := unstructured.NestedMap(fmc.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(spec["secretName"]).To(Equal(FleetMetadataCacheTLSSecretName))
		Expect(spec["dnsNames"]).To(Equal([]interface{}{
			"fleetmetadatacache-service",
			"fleetmetadatacache-service." + kn.Namespace,
			"fleetmetadatacache-service." + kn.Namespace + ".svc",
			"fleetmetadatacache-service." + kn.Namespace + ".svc.cluster.local",
		}))
	})
})

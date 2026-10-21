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

package v1alpha2

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	sigsyaml "sigs.k8s.io/yaml"
)

func TestCleanBreak(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "v1alpha2 Clean-Break Contract Suite")
}

var _ = Describe("v1alpha2 CRD clean-break contract", func() {
	It("serves and stores only v1alpha2 without a conversion webhook", func() {
		crd := loadKubernautCRD()
		Expect(crd.Spec.Versions).To(HaveLen(1))
		Expect(crd.Spec.Versions[0].Name).To(Equal("v1alpha2"))
		Expect(crd.Spec.Versions[0].Served).To(BeTrue())
		Expect(crd.Spec.Versions[0].Storage).To(BeTrue())
		Expect(crd.Spec.Conversion).To(BeNil())
	})

	It("defaults API Frontend to disabled for autonomous deployments", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		apiFrontend := schema.Properties["spec"].Properties["apiFrontend"]
		enabled := apiFrontend.Properties["enabled"]
		Expect(enabled.Default).NotTo(BeNil())
		Expect(string(enabled.Default.Raw)).To(Equal("false"))
		spec := KubernautSpec{}
		Expect(spec.APIFrontendEnabled()).To(BeFalse())
	})

	It("does not publish a fabricated issuer default", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		auth := schema.Properties["spec"].Properties["apiFrontend"].Properties["auth"]
		issuer := auth.Properties["issuerURL"]
		Expect(issuer.Default).To(BeNil())
	})

	It("does not expose deferred SPIFFE/SPIRE configuration", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		apiFrontend := schema.Properties["spec"].Properties["apiFrontend"]
		Expect(apiFrontend.Properties).NotTo(HaveKey("spire"))
	})

	It("UT-NP-555-API-001 [AC-3, AC-6; SOC2 CC6.1, CC6.6; ASVS V4.1, V4.2] publishes only the portable NetworkPolicy contract", func() {
		crd := loadKubernautCRD()
		networkPolicies := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties["networkPolicies"]
		Expect(networkPolicies.Properties).To(HaveKey("scrapeSources"))
		Expect(networkPolicies.Properties).To(HaveKey("gateway"))
		Expect(networkPolicies.Properties).To(HaveKey("apifrontend"))
		Expect(networkPolicies.Properties).To(HaveKey("console"))
		Expect(networkPolicies.Properties).To(HaveKey("datastorage"))
		Expect(networkPolicies.Properties).To(HaveKey("kubernautAgent"))
		for _, removed := range []string{"mode", "provider", "apiServerCIDR", "apiServerCIDRs", "apiServerPort", "monitoring", "externalWebhooks", "idp", "llm", "mcpGateway", "prometheus"} {
			Expect(networkPolicies.Properties).NotTo(HaveKey(removed), "obsolete network policy field %s must not be published", removed)
		}
	})

	It("UT-WE-480-API-001 publishes bounded failed WorkflowExecution retention with safe defaults", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		workflowExecution := schema.Properties["spec"].Properties["workflowExecution"]

		retain := workflowExecution.Properties["retainFailedExecutions"]
		Expect(retain.Type).To(Equal("boolean"))
		Expect(retain.Default).NotTo(BeNil())
		Expect(string(retain.Default.Raw)).To(Equal("false"))

		seconds := workflowExecution.Properties["failedExecutionRetentionSeconds"]
		Expect(seconds.Type).To(Equal("integer"))
		Expect(seconds.Default).NotTo(BeNil())
		Expect(string(seconds.Default.Raw)).To(Equal("600"))
		Expect(seconds.Minimum).NotTo(BeNil())
		Expect(*seconds.Minimum).To(BeZero())
		Expect(seconds.Maximum).NotTo(BeNil())
		Expect(*seconds.Maximum).To(BeEquivalentTo(604800))
		Expect(workflowExecution.XValidations).To(HaveLen(1))
		Expect(workflowExecution.XValidations[0].Rule).To(ContainSubstring("retainFailedExecutions"))
		Expect(workflowExecution.XValidations[0].Rule).To(ContainSubstring("failedExecutionRetentionSeconds > 0"))
	})

	It("UT-483-API-001 publishes optional HTTPS Console provider logout configuration", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		console := schema.Properties["spec"].Properties["console"]
		auth := console.Properties["auth"]
		logout := auth.Properties["providerLogoutURL"]

		Expect(logout.Type).To(Equal("string"))
		Expect(logout.Pattern).To(Equal(`^https://.+`))
		Expect(auth.Required).NotTo(ContainElement("providerLogoutURL"))
	})

	It("UT-DS-550-API-001 publishes the semantic DataStorage contract with secure defaults", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		dataStorage := schema.Properties["spec"].Properties["dataStorage"]
		Expect(dataStorage.Properties).To(HaveKey("server"))
		Expect(dataStorage.Properties).To(HaveKey("database"))
		Expect(dataStorage.Properties).To(HaveKey("redis"))
		Expect(dataStorage.Properties).To(HaveKey("audit"))
		Expect(dataStorage.Properties).NotTo(HaveKey("retention"))
		Expect(dataStorage.Properties).NotTo(HaveKey("signingCert"))
		Expect(dataStorage.Properties).NotTo(HaveKey("auditHashKey"))

		server := dataStorage.Properties["server"]
		Expect(string(server.Properties["maxBodySize"].Default.Raw)).To(Equal(`"5242880"`))
		Expect(string(server.Properties["maxConcurrentRequests"].Default.Raw)).To(Equal("100"))
		Expect(string(server.Properties["rateLimit"].Properties["requestsPerSecond"].Default.Raw)).To(Equal("50"))

		audit := dataStorage.Properties["audit"]
		retention := audit.Properties["retention"]
		Expect(string(retention.Properties["enabled"].Default.Raw)).To(Equal("true"))
		Expect(string(retention.Properties["defaultDays"].Default.Raw)).To(Equal("2555"))
		Expect(*retention.Properties["defaultDays"].Maximum).To(BeEquivalentTo(2555))
	})

	It("resolves only configured issuer sources", func() {
		spec := KubernautSpec{}
		Expect(spec.EffectiveIssuerURL()).To(BeEmpty())
		Expect(spec.EffectiveIssuerSource()).To(Equal(OIDCIssuerSourceUnset))

		spec.APIFrontend.Auth.IssuerURL = "https://login.example.com/realms/demo"
		Expect(spec.EffectiveIssuerURL()).To(Equal("https://login.example.com/realms/demo"))
		Expect(spec.EffectiveIssuerSource()).To(Equal(OIDCIssuerSourceExplicit))

		spec.APIFrontend.Auth.JWTProviders = []JWTProviderSpec{{
			Name: "first", IssuerURL: "https://first.example.com", JWKSURL: "https://first.example.com/keys",
			Audiences: []string{"aud-first"},
		}, {
			Name: "second", IssuerURL: "https://second.example.com", JWKSURL: "https://second.example.com/keys",
			Audiences: []string{"aud-second"},
		}}
		Expect(spec.EffectiveIssuerURL()).To(Equal("https://first.example.com"))
		Expect(spec.EffectiveIssuerSource()).To(Equal(OIDCIssuerSourceMultiProvider))
	})

	It("UT-FLEET-521-API-001: publishes the provider-scoped Fleet API without removed fields", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		specSchema, hasSpec := schema.Properties["spec"]
		Expect(hasSpec).To(BeTrue())
		fleetSchema, hasFleet := specSchema.Properties["fleet"]
		Expect(hasFleet).To(BeTrue())

		for _, field := range []string{"enabled", "mcpGateway", "scopeCheck", "oauth2", "mcpClient"} {
			Expect(fleetSchema.Properties).To(HaveKey(field))
		}
		for _, field := range []string{"backend", "endpoint", "caSecretName", "tokenSecretName", "mcpGatewayEndpoint", "mcpGatewayType", "mcpGatewayNamespace", "resilience"} {
			Expect(fleetSchema.Properties).NotTo(HaveKey(field))
		}

		mcpGateway := fleetSchema.Properties["mcpGateway"]
		for _, field := range []string{"type", "endpoint", "namespace"} {
			Expect(mcpGateway.Properties).To(HaveKey(field))
		}

		scopeCheck := fleetSchema.Properties["scopeCheck"]
		for _, field := range []string{"backend", "acm"} {
			Expect(scopeCheck.Properties).To(HaveKey(field))
		}
		for _, field := range []string{"endpoint", "tls", "tokenSecretRef"} {
			Expect(scopeCheck.Properties).NotTo(HaveKey(field))
		}

		acm := scopeCheck.Properties["acm"]
		for _, field := range []string{"endpoint", "tls", "tokenSecretRef"} {
			Expect(acm.Properties).To(HaveKey(field))
		}

		oauth2 := fleetSchema.Properties["oauth2"]
		for _, field := range []string{"tokenURL", "scopes", "credentialsSecretRef", "tls"} {
			Expect(oauth2.Properties).To(HaveKey(field))
		}
		Expect(oauth2.Properties).NotTo(HaveKey("enabled"))

		Expect(scopeCheck.Properties).NotTo(HaveKey("tls"))

		mcpClient := fleetSchema.Properties["mcpClient"]
		Expect(mcpClient.Properties).To(HaveKey("retry"))
		Expect(mcpClient.Properties).To(HaveKey("timeouts"))
		Expect(mcpClient.Properties).NotTo(HaveKey("resilience"))
		for _, field := range []string{"initialInterval", "maxInterval", "maxElapsedTime"} {
			Expect(mcpClient.Properties["retry"].Properties).To(HaveKey(field))
		}
		for _, field := range []string{"tokenRefresh", "connect", "discoverProbe"} {
			Expect(mcpClient.Properties["timeouts"].Properties).To(HaveKey(field))
		}
	})

	It("UT-539-GW-AUTH-API-001 [IA-2, AC-3, CM-3, CM-6; ASVS v5.0.0-V8.2.1, v5.0.0-V8.3.1, v5.0.0-V16.5.2]: publishes the nested Gateway auth contract with a deployable default and no dead source fields", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		gateway := schema.Properties["spec"].Properties["gateway"]
		auth := gateway.Properties["auth"]

		Expect(auth.Type).To(Equal("object"))
		Expect(auth.Properties).To(HaveKey("enabled"))
		enabled := auth.Properties["enabled"]
		Expect(enabled.Type).To(Equal("boolean"))
		Expect(enabled.Default).NotTo(BeNil())
		Expect(string(enabled.Default.Raw)).To(Equal("false"))

		Expect(auth.Properties).To(HaveKey("alertManager"))
		alertManager := auth.Properties["alertManager"]
		Expect(alertManager.Properties).To(HaveKey("tokenSecretName"))
		Expect(gateway.Properties).NotTo(HaveKey("alertManagerTokenSecretName"))
		Expect(gateway.Properties).NotTo(HaveKey("signalSources"))
		Expect(gateway.Properties).NotTo(HaveKey("kubernetesEvents"))
		Expect(auth.Properties).NotTo(HaveKey("signalSources"))
		Expect(auth.Properties).NotTo(HaveKey("kubernetesEvents"))
	})

	It("retains OAuth2 enabled only for the separate LLM profile API", func() {
		profile := LLMProfileSpec{
			Provider:              "openai",
			Model:                 "gpt-4o",
			CredentialsSecretName: "llm-credentials",
			OAuth2:                OAuth2Spec{Enabled: true},
		}
		serialized, err := json.Marshal(profile)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(serialized)).To(ContainSubstring(`"oauth2":{"enabled":true}`))
	})

	It("does not carry Fleet oauth2.enabled in repository manifests", func() {
		for _, relativePath := range []string{
			filepath.Join("..", "..", "config", "crd", "bases", "kubernaut.ai_kubernauts.yaml"),
			filepath.Join("..", "..", "bundle", "manifests", "kubernaut.ai_kubernauts.yaml"),
			filepath.Join("..", "..", "dist", "install.yaml"),
			filepath.Join("..", "..", "config", "samples", "v1alpha2_kubernaut.yaml"),
		} {
			manifest, err := os.ReadFile(relativePath) //nolint:gosec // test paths are fixed within the repository
			Expect(err).NotTo(HaveOccurred())
			Expect(string(manifest)).NotTo(ContainSubstring("spec.fleet.oauth2.enabled"))
		}
	})
})

func loadKubernautCRD() *apiextensionsv1.CustomResourceDefinition {
	manifestPath := filepath.Join("..", "..", "config", "crd", "bases", "kubernaut.ai_kubernauts.yaml")
	manifest, err := os.ReadFile(manifestPath) //nolint:gosec // test path is fixed within the repository
	Expect(err).NotTo(HaveOccurred())

	crd := &apiextensionsv1.CustomResourceDefinition{}
	Expect(sigsyaml.Unmarshal(manifest, crd)).To(Succeed())
	return crd
}

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

	It("publishes the nested Fleet API without legacy flat fields or Fleet oauth2.enabled", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		specSchema, hasSpec := schema.Properties["spec"]
		Expect(hasSpec).To(BeTrue())
		fleetSchema, hasFleet := specSchema.Properties["fleet"]
		Expect(hasFleet).To(BeTrue())

		for _, field := range []string{"enabled", "mcpGateway", "scopeCheck", "oauth2", "resilience"} {
			Expect(fleetSchema.Properties).To(HaveKey(field))
		}
		for _, field := range []string{"backend", "endpoint", "caSecretName", "tokenSecretName", "mcpGatewayEndpoint", "mcpGatewayType", "mcpGatewayNamespace"} {
			Expect(fleetSchema.Properties).NotTo(HaveKey(field))
		}

		mcpGateway := fleetSchema.Properties["mcpGateway"]
		for _, field := range []string{"type", "endpoint", "namespace"} {
			Expect(mcpGateway.Properties).To(HaveKey(field))
		}

		scopeCheck := fleetSchema.Properties["scopeCheck"]
		for _, field := range []string{"backend", "endpoint", "tls", "tokenSecretRef"} {
			Expect(scopeCheck.Properties).To(HaveKey(field))
		}

		oauth2 := fleetSchema.Properties["oauth2"]
		for _, field := range []string{"tokenURL", "scopes", "credentialsSecretRef", "tls"} {
			Expect(oauth2.Properties).To(HaveKey(field))
		}
		Expect(oauth2.Properties).NotTo(HaveKey("enabled"))

		trust := scopeCheck.Properties["tls"]
		for _, field := range []string{"source", "caFile", "caCertSecretRef"} {
			Expect(trust.Properties).To(HaveKey(field))
		}
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

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
		manifestPath := filepath.Join("..", "..", "config", "crd", "bases", "kubernaut.ai_kubernauts.yaml")
		manifest, err := os.ReadFile(manifestPath) //nolint:gosec // test path is fixed within the repository
		Expect(err).NotTo(HaveOccurred())

		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(sigsyaml.Unmarshal(manifest, crd)).To(Succeed())
		Expect(crd.Spec.Versions).To(HaveLen(1))
		Expect(crd.Spec.Versions[0].Name).To(Equal("v1alpha2"))
		Expect(crd.Spec.Versions[0].Served).To(BeTrue())
		Expect(crd.Spec.Versions[0].Storage).To(BeTrue())
		Expect(crd.Spec.Conversion).To(BeNil())
	})
})

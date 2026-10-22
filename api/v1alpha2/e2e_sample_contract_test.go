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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sigsyaml "sigs.k8s.io/yaml"
)

var _ = Describe("disposable OCP sample contract", func() {
	It("UT-E2E-DB-001 [SC-8, CM-6; SOC2 CC8.1; ASVS V12.3.1, V16.5.2]: keeps PostgreSQL TLS aligned with the bootstrap chart", func() {
		manifestPath := filepath.Join("..", "..", "config", "samples", "v1alpha2_kubernaut.yaml")
		manifest, err := os.ReadFile(manifestPath) //nolint:gosec // test path is fixed within the repository
		Expect(err).NotTo(HaveOccurred())

		sample := &Kubernaut{}
		Expect(sigsyaml.Unmarshal(manifest, sample)).To(Succeed())
		Expect(sample.Spec.PostgreSQL.SSLMode).To(Equal("require"),
			"the disposable bootstrap owns a private PostgreSQL CA; the E2E sample must request encrypted transport without assuming the operator CA")
	})
})

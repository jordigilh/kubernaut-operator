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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Issue #567: Kubernaut Agent investigation concurrency", func() {
	It("UT-KA-567-API-001 [CM-6, SI-10; SOC2 CC6.6, CC8.1; ASVS v5.0.0-V2.2.1, v5.0.0-V2.2.2, v5.0.0-V2.2.3] publishes a positive defaulted session capacity", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		ka := schema.Properties["spec"].Properties["kubernautAgent"]
		session := ka.Properties["session"]

		Expect(session.Properties).To(HaveKey("maxConcurrentInvestigations"))
		capacity := session.Properties["maxConcurrentInvestigations"]
		Expect(capacity.Type).To(Equal("integer"))
		Expect(capacity.Default).NotTo(BeNil())
		Expect(string(capacity.Default.Raw)).To(Equal("50"))
		Expect(capacity.Minimum).NotTo(BeNil())
		Expect(*capacity.Minimum).To(Equal(float64(1)))
		Expect(session.Required).NotTo(ContainElement("maxConcurrentInvestigations"))
	})
})

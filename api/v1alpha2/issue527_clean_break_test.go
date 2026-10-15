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

var _ = Describe("Issue #527: logical Agent configuration boundaries", func() {
	It("UT-KA-527-API-001 [CM-6, SI-10; SOC2 CC8.1; ASVS v5.0.0-V16.5.2] exposes independent investigation and workflow-discovery profile references", func() {
		crd := loadKubernautCRD()
		schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
		ka := schema.Properties["spec"].Properties["kubernautAgent"]

		Expect(ka.Properties).To(HaveKey("investigation"))
		Expect(ka.Properties).To(HaveKey("workflowDiscovery"))
		Expect(ka.Properties).NotTo(HaveKey("llmProfileRef"))
		Expect(ka.Properties).NotTo(HaveKey("phaseModels"))
		Expect(ka.Properties).NotTo(HaveKey("maxTurns"))

		investigation := ka.Properties["investigation"]
		Expect(investigation.Properties).To(HaveKey("llmProfileRef"))
		Expect(string(investigation.Properties["maxTurns"].Default.Raw)).To(Equal("40"))

		workflowDiscovery := ka.Properties["workflowDiscovery"]
		Expect(workflowDiscovery.Properties).To(HaveKey("llmProfileRef"))
		Expect(string(workflowDiscovery.Properties["maxEvidenceHandoffBytes"].Default.Raw)).To(Equal("262144"))
		Expect(workflowDiscovery.Properties["maxEvidenceHandoffBytes"].Minimum).NotTo(BeNil())
		Expect(*workflowDiscovery.Properties["maxEvidenceHandoffBytes"].Minimum).To(Equal(float64(1)))

		apiFrontend := schema.Properties["spec"].Properties["apiFrontend"]
		Expect(apiFrontend.Properties["llmProfileRef"]).NotTo(BeNil())
	})
})

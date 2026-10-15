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
)

var _ = Describe("Issue #527: logical Agent profile references", Label("unit"), func() {
	It("UT-KA-527-VAL-001 [CM-6, SI-10; SOC2 CC8.1; ASVS v5.0.0-V2.2.1] rejects an undefined workflow-discovery profile", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.WorkflowDiscovery.LLMProfileRef = testMissingProfile

		errs := ValidateKubernaut(kn)
		Expect(errs).NotTo(BeEmpty())
		Expect(errs[0].Error()).To(ContainSubstring("spec.kubernautAgent.workflowDiscovery.llmProfileRef"))
	})

	It("UT-KA-527-VAL-002 [CM-6, SI-10; SOC2 CC8.1; ASVS v5.0.0-V2.2.1] does not make API Frontend inherit the investigation profile when multiple profiles exist", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["workflow"] = kn.Spec.LLMProfiles["primary"]
		kn.Spec.KubernautAgent.Investigation.LLMProfileRef = "primary"
		kn.Spec.KubernautAgent.WorkflowDiscovery.LLMProfileRef = "workflow"
		kn.Spec.APIFrontend.LLMProfileRef = ""

		errs := ValidateKubernaut(kn)
		Expect(errs).NotTo(BeEmpty())
		Expect(errs).To(ContainElement(MatchError(ContainSubstring("spec.apiFrontend.llmProfileRef"))))
	})
})

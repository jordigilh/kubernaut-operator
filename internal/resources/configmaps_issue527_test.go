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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("Issue #527: workflow-discovery evidence handoff", Label("unit"), func() {
	It("UT-KA-527-CM-001 [CM-2, CM-6; SOC2 CC8.1; ASVS v5.0.0-V16.5.2] renders the 256KiB default", func() {
		kn := testKubernaut()
		cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())

		var root kubernautAgentConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.AI.Investigation.MaxEvidenceHandoffBytes).To(Equal(262144),
			"operator-managed KA must default the complete evidence handoff budget to 256KiB")
	})

	It("UT-KA-527-CM-002 [CM-6, SI-10; SOC2 CC8.1; ASVS v5.0.0-V2.2.1] preserves an explicit workflow-discovery override", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.WorkflowDiscovery.MaxEvidenceHandoffBytes = ptr.To(131072)

		cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())

		var root kubernautAgentConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.AI.Investigation.MaxEvidenceHandoffBytes).To(Equal(131072),
			"an administrator override must reach the upstream ai.investigation contract unchanged")
	})

	It("UT-KA-527-LLM-001 [CM-6; SOC2 CC8.1; ASVS v5.0.0-V16.5.2] renders the workflow-discovery agent profile as the upstream phase override", func() {
		kn := testKubernaut()
		kn.Spec.LLMProfiles[testWorkflowLiteProfile] = kubernautv1alpha2.LLMProfileSpec{
			Provider:              LLMProviderOpenAI,
			Model:                 "claude-haiku-4-6",
			Endpoint:              testOpenAIEndpoint,
			CredentialsSecretName: "workflow-credentials",
		}
		kn.Spec.KubernautAgent.WorkflowDiscovery.LLMProfileRef = testWorkflowLiteProfile

		cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["llm-runtime.yaml"]
		Expect(data).To(ContainSubstring("phaseModels:"))
		Expect(data).To(ContainSubstring("workflow_discovery:"))
		Expect(data).To(ContainSubstring("model: claude-haiku-4-6"))
	})
})

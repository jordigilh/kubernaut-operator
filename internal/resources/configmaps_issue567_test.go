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
	"sigs.k8s.io/yaml"
)

const issue567SessionTTL = "45m"

var _ = Describe("Issue #567: Kubernaut Agent investigation concurrency", Label("unit"), func() {
	It("UT-KA-567-CM-001 [CM-2, CM-6; SOC2 CC8.1; ASVS v5.0.0-V14.2.4, v5.0.0-V16.5.2] renders the approved default", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.Session.TTL = issue567SessionTTL

		cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())

		var rendered kubernautAgentConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &rendered)).To(Succeed())
		Expect(rendered.Runtime.Session).NotTo(BeNil())
		Expect(rendered.Runtime.Session.MaxConcurrentInvestigations).To(Equal(50))
		Expect(rendered.Runtime.Session.TTL).To(Equal(issue567SessionTTL))
	})

	It("UT-KA-567-CM-002 [CM-3, CM-6; SOC2 CC6.6, CC8.1; ASVS v5.0.0-V2.2.1, v5.0.0-V14.2.4] preserves an explicit positive override", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.Session.TTL = issue567SessionTTL
		kn.Spec.KubernautAgent.Session.MaxConcurrentInvestigations = 75

		cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())

		var rendered kubernautAgentConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &rendered)).To(Succeed())
		Expect(rendered.Runtime.Session).NotTo(BeNil())
		Expect(rendered.Runtime.Session.MaxConcurrentInvestigations).To(Equal(75))
		Expect(rendered.Runtime.Session.TTL).To(Equal(issue567SessionTTL))
	})
})

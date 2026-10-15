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
)

var _ = Describe("Issue #480: failed WorkflowExecution retention", Label("unit"), func() {
	It("UT-WE-480-CM-001 [CM-6, SI-10; SOC2 CC8.1; ASVS v5.0.0-V2.1.1, V2.2.1] keeps diagnostic retention disabled with the bounded default", func() {
		kn := testKubernaut()
		cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())

		var root workflowExecutionConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["workflowexecution.yaml"]), &root)).To(Succeed())
		Expect(root.Execution.RetainFailedExecutions).To(BeFalse(),
			"omitted retention configuration must not retain failed workload resources")
		Expect(root.Execution.FailedExecutionRetentionSeconds).To(Equal(int64(600)),
			"the upstream bounded diagnostic default must remain explicit in operator-managed configuration")
	})

	It("UT-WE-480-CM-002 [CM-3, CM-6; SOC2 CC8.1; ASVS v5.0.0-V2.2.1, V2.2.2] renders an explicit bounded retention opt-in", func() {
		kn := testKubernaut()
		kn.Spec.WorkflowExecution.RetainFailedExecutions = true
		kn.Spec.WorkflowExecution.FailedExecutionRetentionSeconds = ptr.To(int64(900))

		cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())

		var root workflowExecutionConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["workflowexecution.yaml"]), &root)).To(Succeed())
		Expect(root.Execution.RetainFailedExecutions).To(BeTrue())
		Expect(root.Execution.FailedExecutionRetentionSeconds).To(Equal(int64(900)),
			"the administrator-selected diagnostic bound must reach WorkflowExecution unchanged")
	})
})

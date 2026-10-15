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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("WorkflowExecution retention API admission (#480)", func() {
	ctx := context.Background()

	var created *kubernautv1alpha2.Kubernaut

	AfterEach(func() {
		if created != nil {
			_ = k8sClient.Delete(ctx, created)
			created = nil
		}
	})

	It("IT-WE-480-ADMISSION-001 [CM-6, SI-10; SOC2 CC8.1; ASVS v5.0.0-V2.2.1, V2.2.3] rejects enabled retention with a zero period", func() {
		cr := newMinimalV1alpha2CR("workflowexecution-retention-invalid")
		cr.Spec.WorkflowExecution.RetainFailedExecutions = true
		cr.Spec.WorkflowExecution.FailedExecutionRetentionSeconds = ptr.To(int64(0))

		err := k8sClient.Create(ctx, cr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failedExecutionRetentionSeconds must be greater than zero"))
	})

	It("IT-WE-480-ADMISSION-002 [CM-6; SOC2 CC8.1; ASVS v5.0.0-V2.2.3] applies the 600-second default when retention is enabled without a period", func() {
		cr := newMinimalV1alpha2CR("workflowexecution-retention-default")
		cr.Spec.WorkflowExecution.RetainFailedExecutions = true

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		created = cr

		fetched := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, fetched)).To(Succeed())
		Expect(fetched.Spec.WorkflowExecution.FailedExecutionRetentionSeconds).NotTo(BeNil())
		Expect(*fetched.Spec.WorkflowExecution.FailedExecutionRetentionSeconds).To(Equal(int64(600)))
	})

	It("IT-WE-480-ADMISSION-003 [CM-6, SI-10; SOC2 CC8.1; ASVS v5.0.0-V2.2.1] rejects retention periods longer than seven days", func() {
		cr := newMinimalV1alpha2CR("workflowexecution-retention-too-long")
		cr.Spec.WorkflowExecution.FailedExecutionRetentionSeconds = ptr.To(int64(604801))

		err := k8sClient.Create(ctx, cr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("should be less than or equal to 604800"))
	})

	It("IT-WE-480-ADMISSION-004 [CM-6, SI-10; SOC2 CC8.1; ASVS v5.0.0-V2.2.1] accepts a zero period while retention is disabled", func() {
		cr := newMinimalV1alpha2CR("workflowexecution-retention-disabled-zero")
		cr.Spec.WorkflowExecution.FailedExecutionRetentionSeconds = ptr.To(int64(0))

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		created = cr

		fetched := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, fetched)).To(Succeed())
		Expect(fetched.Spec.WorkflowExecution.RetainFailedExecutions).To(BeFalse())
		Expect(fetched.Spec.WorkflowExecution.FailedExecutionRetentionSeconds).NotTo(BeNil())
		Expect(*fetched.Spec.WorkflowExecution.FailedExecutionRetentionSeconds).To(BeZero())
	})
})

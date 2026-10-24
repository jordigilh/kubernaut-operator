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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("WorkflowExecution failed-resource retention wiring (#480)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("IT-WE-480-001 [CM-3, CM-6, SI-10; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V2.2.2] propagates disabled defaults and an explicit opt-in through reconciliation", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
		r := reconcileToRunning(ctx)

		cmKey := types.NamespacedName{Name: "workflowexecution-config", Namespace: testNamespace}
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
		defaultConfig := cm.Data["workflowexecution.yaml"]
		Expect(defaultConfig).To(ContainSubstring("retainFailedExecutions: false"),
			"the reconciled default must not retain failed Job, PipelineRun, or AWX diagnostics")
		Expect(defaultConfig).To(ContainSubstring("failedExecutionRetentionSeconds: 600"),
			"the reconciled default must preserve the upstream bounded retention period")

		kn := newMinimalCR()
		Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
		kn.Spec.WorkflowExecution.RetainFailedExecutions = true
		kn.Spec.WorkflowExecution.FailedExecutionRetentionSeconds = ptr.To(int64(900))
		Expect(k8sClient.Update(ctx, kn)).To(Succeed())

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
		updatedConfig := cm.Data["workflowexecution.yaml"]
		Expect(updatedConfig).To(ContainSubstring("retainFailedExecutions: true"),
			"an explicit opt-in must reach the live WorkflowExecution configuration")
		Expect(updatedConfig).To(ContainSubstring("failedExecutionRetentionSeconds: 900"),
			"an explicit bounded diagnostic period must reach the live WorkflowExecution configuration")
	})
})

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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("Issue #527: KA evidence handoff reconciliation", func() {
	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
	})

	It("IT-KA-527-RECONCILE-001 [CM-2, CM-3, CM-6, SA-11; SOC2 CC8.1; ASVS v5.0.0-V13.3.1/V16.5.2] renders the default, preserves an override, and wires the Agent ConfigMap mount", func() {
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		r := reconcileToDeployPhase(ctx)

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent-config", Namespace: testNamespace}, configMap)).To(Succeed())
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("maxEvidenceHandoffBytes: 262144"),
			"a fresh operator-managed Agent must receive the 256KiB handoff baseline")

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent", Namespace: testNamespace}, deployment)).To(Succeed())
		foundConfigVolume := false
		for _, volume := range deployment.Spec.Template.Spec.Volumes {
			if volume.Name == "config" {
				foundConfigVolume = true
				Expect(volume.ConfigMap).NotTo(BeNil())
				Expect(volume.ConfigMap.Name).To(Equal("kubernaut-agent-config"))
			}
		}
		Expect(foundConfigVolume).To(BeTrue(), "KA Deployment must mount the generated Agent ConfigMap")
		Expect(deployment.Spec.Template.Spec.Containers).NotTo(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElement(
			HaveField("Name", Equal("config")),
		), "KA container must consume the generated config volume")

		updated := &client.ObjectKey{Name: kubernautv1alpha2.SingletonName, Namespace: testNamespace}
		current := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, *updated, current)).To(Succeed())
		current.Spec.KubernautAgent.WorkflowDiscovery.MaxEvidenceHandoffBytes = ptr.To(131072)
		Expect(k8sClient.Update(ctx, current)).To(Succeed())
		_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: *updated})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent-config", Namespace: testNamespace}, configMap)).To(Succeed())
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("maxEvidenceHandoffBytes: 131072"),
			"reconciliation must preserve an explicit administrator override")
	})
})

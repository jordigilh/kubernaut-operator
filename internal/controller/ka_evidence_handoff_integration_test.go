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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Issue #527: KA evidence handoff reconciliation", func() {
	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
	})

	It("IT-KA-527-RECONCILE-001 [CM-2, CM-3, CM-6, SA-11; SOC2 CC8.1; ASVS v5.0.0-V13.3.1/V16.5.2] wires the Agent ConfigMap mount", func() {
		createBYOSecrets(ctx)
		kn := newMinimalCR()
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		reconcileToDeployPhase(ctx)

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent-config", Namespace: testNamespace}, configMap)).To(Succeed())

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
	})
})

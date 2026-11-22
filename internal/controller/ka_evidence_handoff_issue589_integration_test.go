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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Issue #589: removal of the operator-facing evidence handoff byte budget", func() {
	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
	})

	It("IT-KA-589-RECONCILE-001 [CM-2, CM-6; SOC2 CC8.1; ASVS v5.0.0-V16.5.2] renders no maxEvidenceHandoffBytes knob on a fresh deployment", func() {
		createBYOSecrets(ctx)
		kn := newMinimalCR()
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		reconcileToDeployPhase(ctx)

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent-config", Namespace: testNamespace}, configMap)).To(Succeed())
		Expect(configMap.Data["config.yaml"]).NotTo(ContainSubstring("maxEvidenceHandoffBytes"),
			"the structured context budget is code-owned in Kubernaut Agent; a fresh deployment must not render the removed ai.investigation knob")
	})
})

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
	"sigs.k8s.io/yaml"
)

const configVolumeName = "config"

var _ = Describe("Issue #567: Kubernaut Agent investigation concurrency", func() {
	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
	})

	It("IT-KA-567-RECONCILE-001 [CM-2, CM-3, CM-6, SA-11, SI-4; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V14.2.4, v5.0.0-V16.5.2] renders the default through Reconcile", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

		reconcileToDeployPhase(ctx)

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent-config", Namespace: testNamespace}, cm)).To(Succeed())
		Expect(sessionCapacity(cm.Data["config.yaml"])).To(Equal(50))
	})

	It("IT-KA-567-RECONCILE-002 [CM-3, CM-6, SI-4, SA-11; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V13.3.1, v5.0.0-V16.5.2] propagates an override and keeps the ConfigMap mount read-only", func() {
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		kn.Spec.KubernautAgent.Session.MaxConcurrentInvestigations = 75
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		reconcileToDeployPhase(ctx)

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent-config", Namespace: testNamespace}, cm)).To(Succeed())
		Expect(sessionCapacity(cm.Data["config.yaml"])).To(Equal(75))

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent", Namespace: testNamespace}, deployment)).To(Succeed())
		Expect(deploymentHasReadOnlyConfigMount(deployment)).To(BeTrue())
	})
})

// sessionCapacity extracts the rendered KA investigation capacity from YAML.
func sessionCapacity(data string) int {
	var rendered struct {
		Runtime struct {
			Session struct {
				MaxConcurrentInvestigations int `yaml:"maxConcurrentInvestigations"`
			} `yaml:"session"`
		} `yaml:"runtime"`
	}
	ExpectWithOffset(1, yaml.Unmarshal([]byte(data), &rendered)).To(Succeed())
	return rendered.Runtime.Session.MaxConcurrentInvestigations
}

// deploymentHasReadOnlyConfigMount reports whether the KA config is mounted read-only.
func deploymentHasReadOnlyConfigMount(deployment *appsv1.Deployment) bool {
	for _, container := range deployment.Spec.Template.Spec.Containers {
		for _, mount := range container.VolumeMounts {
			if mount.Name == configVolumeName && mount.ReadOnly {
				return true
			}
		}
	}
	return false
}

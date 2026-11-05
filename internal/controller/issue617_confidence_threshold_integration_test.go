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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("Issue #617 AIAnalysis confidence threshold reconciliation", Label("integration"), func() {
	BeforeEach(func() {
		deleteCRIfExists(ctx)
		Eventually(func() bool {
			cr := &kubernautv1alpha2.Kubernaut{}
			err := k8sClient.Get(ctx, singletonKey(), cr)
			return apierrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})
	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("persists a configured confidence threshold as numeric runtime YAML", func() {
		createBYOSecrets(ctx)
		kn := newMinimalCR()
		kn.Spec.AIAnalysis.ConfidenceThreshold = "0.85"
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		reconcileToDeployPhase(ctx)

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "aianalysis-config", Namespace: testNamespace}, configMap)).To(Succeed())

		var rendered struct {
			Rego struct {
				ConfidenceThreshold *float64 `yaml:"confidenceThreshold"`
			} `yaml:"rego"`
		}
		Expect(yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &rendered)).To(Succeed())
		Expect(rendered.Rego.ConfidenceThreshold).NotTo(BeNil())
		Expect(*rendered.Rego.ConfidenceThreshold).To(Equal(float64(0.85)))
	})

	It("persists the operator default confidence threshold when omitted", func() {
		createBYOSecrets(ctx)
		kn := newMinimalCR()
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		reconcileToDeployPhase(ctx)

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "aianalysis-config", Namespace: testNamespace}, configMap)).To(Succeed())

		var rendered struct {
			Rego struct {
				ConfidenceThreshold *float64 `yaml:"confidenceThreshold"`
			} `yaml:"rego"`
		}
		Expect(yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &rendered)).To(Succeed())
		Expect(rendered.Rego.ConfidenceThreshold).NotTo(BeNil())
		Expect(*rendered.Rego.ConfidenceThreshold).To(Equal(float64(0.8)))
	})

	It("fails closed before creating AIAnalysis resources for an invalid threshold", func() {
		createBYOSecrets(ctx)
		kn := newMinimalCR()
		kn.Spec.AIAnalysis.ConfidenceThreshold = "not-a-number"
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		deployErr := err
		if deployErr == nil {
			markMigrationJobComplete(ctx)
			for range 2 {
				_, deployErr = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
				if deployErr != nil {
					break
				}
			}
		}
		Expect(deployErr).To(MatchError(ContainSubstring("confidenceThreshold")))

		configMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "aianalysis-config", Namespace: testNamespace}, configMap)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "invalid configuration must not create aianalysis-config")

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{Name: "aianalysis", Namespace: testNamespace}, deployment)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "invalid configuration must not create the AIAnalysis Deployment")
	})
})

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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

const (
	testNamespace = "default"
	pgSecretName  = "postgresql-secret"
	vkSecretName  = "valkey-secret"
	llmSecretName = "llm-credentials"
	timeout       = 10 * time.Second
	interval      = 250 * time.Millisecond
)

func singletonKey() types.NamespacedName {
	return types.NamespacedName{Name: kubernautv1alpha1.SingletonName, Namespace: testNamespace}
}

func newMinimalCR() *kubernautv1alpha1.Kubernaut {
	return &kubernautv1alpha1.Kubernaut{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernautv1alpha1.SingletonName,
			Namespace: testNamespace,
		},
		Spec: kubernautv1alpha1.KubernautSpec{
			Image: kubernautv1alpha1.ImageSpec{
				Registry: "quay.io",
				Tag:      "v1.3.0",
			},
			PostgreSQL: kubernautv1alpha1.PostgreSQLSpec{
				SecretName: pgSecretName,
				Host:       "postgresql",
			},
			Valkey: kubernautv1alpha1.ValkeySpec{
				SecretName: vkSecretName,
				Host:       "valkey",
			},
			HolmesGPTAPI: kubernautv1alpha1.HolmesGPTAPISpec{
				LLM: kubernautv1alpha1.LLMSpec{
					Provider:              "openai",
					Model:                 "gpt-4o",
					CredentialsSecretName: llmSecretName,
				},
			},
			AIAnalysis: kubernautv1alpha1.AIAnalysisSpec{
				Policy: kubernautv1alpha1.PolicyConfigMapRef{ConfigMapName: "aianalysis-policies"},
			},
			SignalProcessing: kubernautv1alpha1.SignalProcessingSpec{
				Policy: kubernautv1alpha1.PolicyConfigMapRef{ConfigMapName: "signalprocessing-policy"},
			},
		},
	}
}

func newReconciler() *KubernautReconciler {
	return &KubernautReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: record.NewFakeRecorder(100),
	}
}

func createBYOSecrets(ctx context.Context) {
	secrets := []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{Name: pgSecretName, Namespace: testNamespace},
			Data: map[string][]byte{
				"POSTGRES_USER":     []byte("kubernaut"),
				"POSTGRES_PASSWORD": []byte("secret"),
				"POSTGRES_DB":       []byte("kubernaut"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: vkSecretName, Namespace: testNamespace},
			Data: map[string][]byte{
				"valkey-secrets.yaml": []byte("password: secret"),
			},
		},
	}
	for _, s := range secrets {
		existing := &corev1.Secret{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: s.Namespace}, existing)
		if errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, s)).To(Succeed())
		}
	}
}

func deleteBYOSecrets(ctx context.Context) {
	for _, name := range []string{pgSecretName, vkSecretName} {
		s := &corev1.Secret{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, s)
		if err == nil {
			_ = k8sClient.Delete(ctx, s)
		}
	}
}

func deleteCRIfExists(ctx context.Context) {
	cr := &kubernautv1alpha1.Kubernaut{}
	err := k8sClient.Get(ctx, singletonKey(), cr)
	if err == nil {
		controllerutil.RemoveFinalizer(cr, kubernautv1alpha1.FinalizerName)
		_ = k8sClient.Update(ctx, cr)
		_ = k8sClient.Delete(ctx, cr)
	}
}

var _ = Describe("Kubernaut Controller", func() {

	ctx := context.Background()

	AfterEach(func() {
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
	})

	// ---- Singleton Guard ----

	Context("Singleton Guard", func() {
		It("should ignore a CR with a non-singleton name", func() {
			badCR := newMinimalCR()
			badCR.Name = "not-kubernaut"
			Expect(k8sClient.Create(ctx, badCR)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, badCR) }()

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "not-kubernaut", Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			result := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "not-kubernaut", Namespace: testNamespace}, result)).To(Succeed())
			Expect(result.Finalizers).To(BeEmpty(), "non-singleton CR should not get a finalizer")
		})

		It("should accept the singleton name 'kubernaut'", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			result := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), result)).To(Succeed())
			Expect(result.Finalizers).To(ContainElement(kubernautv1alpha1.FinalizerName))
		})
	})

	// ---- Finalizer Lifecycle ----

	Context("Finalizer Lifecycle", func() {
		It("should add the finalizer on first reconcile", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(kn, kubernautv1alpha1.FinalizerName)).To(BeTrue())
		})

		It("should remove the finalizer on deletion", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()

			By("adding the finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("deleting the CR")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(k8sClient.Delete(ctx, kn)).To(Succeed())

			By("reconciling the deletion")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, singletonKey(), kn)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "CR should be deleted after finalizer removal")
		})
	})

	// ---- BYO Secret Validation ----

	Context("BYO Secret Validation", func() {
		It("should fail validation when PostgreSQL secret is missing", func() {
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()

			By("adding finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("attempting validation")
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "should requeue on validation failure")

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseError))

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("PostgreSQLSecretInvalid"))
		})

		It("should fail validation when Valkey secret is missing", func() {
			pgOnly := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: pgSecretName, Namespace: testNamespace},
				Data: map[string][]byte{
					"POSTGRES_USER":     []byte("u"),
					"POSTGRES_PASSWORD": []byte("p"),
					"POSTGRES_DB":       []byte("d"),
				},
			}
			Expect(k8sClient.Create(ctx, pgOnly)).To(Succeed())
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("ValkeySecretInvalid"))
		})

		It("should fail when PostgreSQL secret is missing required keys", func() {
			badPG := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: pgSecretName, Namespace: testNamespace},
				Data: map[string][]byte{
					"POSTGRES_USER": []byte("u"),
				},
			}
			vk := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: vkSecretName, Namespace: testNamespace},
				Data: map[string][]byte{
					"valkey-secrets.yaml": []byte("password: s"),
				},
			}
			Expect(k8sClient.Create(ctx, badPG)).To(Succeed())
			Expect(k8sClient.Create(ctx, vk)).To(Succeed())
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Message).To(ContainSubstring("missing required key"))
		})

		It("should pass validation with correct secrets", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	// ---- Phase Progression ----

	Context("Phase Progression", func() {
		It("should return success when CR does not exist", func() {
			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	// ---- Status Conditions ----

	Context("Status Conditions", func() {
		It("should set ObservedGeneration on conditions", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Conditions).NotTo(BeEmpty(),
				"conditions should not be empty after reconciliation")

			for _, cond := range kn.Status.Conditions {
				Expect(cond.ObservedGeneration).To(Equal(kn.Generation),
					"condition %q should have ObservedGeneration matching CR generation", cond.Type)
			}
		})
	})
})

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

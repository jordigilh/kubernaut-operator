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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// #358, #359: the internal/controller suite shares one hardcoded workflow
// namespace (resources.DefaultWorkflowNamespace) across effectively every
// spec. Without the BeforeSuite namespace-finalizer watcher (suite_test.go's
// finalizeTerminatingNamespaces), envtest -- which runs a real kube-apiserver
// but no kube-controller-manager -- leaves a deleted Namespace wedged in
// Terminating forever (its default "kubernetes" finalizer is never cleared),
// so any spec whose finalizer cleanup legitimately deletes the shared
// namespace would permanently break every later spec in the same test
// binary that tries to create content in it: "forbidden: unable to create
// new content ... because it is being terminated". This spec exercises that
// exact real-delete path end-to-end (deliberately WITHOUT
// stripWorkflowNamespaceCreatedByAnnotation, unlike every other spec in this
// suite) and proves the namespace not only finishes deleting but can be
// recreated afterward in the same process -- the scenario that was
// structurally impossible before the fix.
var _ = Describe("envtest namespace-termination lifecycle (#358, #359)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("fully finalizes the shared workflow namespace on operator-driven deletion, so a later spec can recreate it", func() {
		wfNsName := resources.DefaultWorkflowNamespace

		// The workflow namespace is shared across every spec in this suite and
		// is deliberately NOT torn down by AfterEach (only namespaced content
		// within it is). To make this spec's precondition (operator CREATED,
		// not adopted, the namespace) hold regardless of run order, reclaim a
		// clean slate here: delete any leftover namespace from a prior spec
		// and wait for it to fully finalize before proceeding. This itself
		// already exercises the #358/#359 fix once.
		if err := k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: wfNsName}}); err != nil {
			Expect(errors.IsNotFound(err)).To(BeTrue(), "unexpected error deleting leftover workflow namespace")
		}
		Eventually(func() bool {
			getErr := k8sClient.Get(ctx, types.NamespacedName{Name: wfNsName}, &corev1.Namespace{})
			return errors.IsNotFound(getErr)
		}, "10s", "100ms").Should(BeTrue(), "leftover workflow namespace from a prior spec never finished finalizing")

		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := reconcileToRunning(ctx)

		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wfNsName}, ns)).To(Succeed())
		Expect(ns.Annotations[resources.AnnotationCreatedBy]).To(Equal("kubernaut-operator"),
			"precondition: the workflow namespace must be operator-managed for the finalizer's real "+
				"delete path (deleteOperatorManagedWorkflowNamespace) to trigger below")

		By("deleting the CR WITHOUT stripping the created-by annotation, exercising the real namespace-delete path")
		kn := &kubernautv1alpha1.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
		Expect(k8sClient.Delete(ctx, kn)).To(Succeed())

		By("reconciling the deletion")
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())

		By("verifying the namespace fully finalizes instead of hanging Terminating forever (#358/#359 root cause)")
		Eventually(func() bool {
			getErr := k8sClient.Get(ctx, types.NamespacedName{Name: wfNsName}, &corev1.Namespace{})
			return errors.IsNotFound(getErr)
		}, "10s", "100ms").Should(BeTrue(),
			"without the BeforeSuite namespace-finalizer watcher, envtest leaves a deleted Namespace stuck "+
				"in Terminating forever (no kube-controller-manager to clear spec.finalizers) -- the exact "+
				"#358/#359 cascading-failure root cause")

		By("recreating the CR and verifying the workflow namespace can be provisioned again in the SAME process")
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		reconcileToRunning(ctx)

		recreated := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wfNsName}, recreated)).To(Succeed())
		Expect(recreated.DeletionTimestamp).To(BeNil(), "recreated workflow namespace should not be terminating")
	})
})

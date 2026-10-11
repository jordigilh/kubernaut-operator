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

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// #514: even an operator-created namespace must survive CR removal because
// namespace ownership is not authority over provisioning-owned contents.
// The #358/#359 envtest finalizer watcher still supports explicit fixture/admin
// deletion, but production uninstall must not start namespace termination.
var _ = Describe("envtest workflow namespace retention and reuse (#514)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("retains an operator-created workflow namespace and reuses its UID after CR reinstall", func() {
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
		Expect(ns.Annotations[resources.AnnotationCreatedBy]).To(Equal("kubernaut-operator"))
		originalUID := ns.UID

		By("deleting the CR without stripping namespace provenance")
		kn := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
		Expect(k8sClient.Delete(ctx, kn)).To(Succeed())

		By("reconciling the deletion")
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())

		By("verifying CR finalization completed without starting namespace termination")
		Expect(k8sClient.Get(ctx, singletonKey(), &kubernautv1alpha2.Kubernaut{})).To(MatchError(ContainSubstring("not found")))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wfNsName}, ns)).To(Succeed())
		Expect(ns.UID).To(Equal(originalUID))
		Expect(ns.DeletionTimestamp).To(BeNil())

		// envtest also has no garbage-collector controller, so the deleted
		// CR's owned namespaced resources (e.g. the migration Job) outlive
		// it. Without this, recreating the CR below reuses the same Job name
		// and the apiserver rejects re-setting its now-immutable
		// status.startTime/completionTime fields (mirrors what every other
		// spec's AfterEach already does between specs).
		cleanupNamespacedResources(ctx)

		By("recreating the CR and reusing the retained namespace")
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		reconcileToRunning(ctx)

		recreated := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wfNsName}, recreated)).To(Succeed())
		Expect(recreated.UID).To(Equal(originalUID))
		Expect(recreated.DeletionTimestamp).To(BeNil())
	})
})

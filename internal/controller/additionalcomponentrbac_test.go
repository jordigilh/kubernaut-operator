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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// additionalRBACTestComponents mirrors the set additionalRBACComponents (in
// kubernaut_controller.go) binds spec.additionalClusterRoles entries to
// (#277): KA and EM unconditionally, Gateway only while enabled.
var additionalRBACTestComponents = []string{
	resources.ComponentKubernautAgent, resources.ComponentGateway, resources.ComponentEffectivenessMonitor,
}

// additionalCRBName computes the expected CRB name for a given component
// and role name, using the same helper the controller/builder use, scoped
// to testNamespace like every other test CR in this package.
func additionalCRBName(component, crName string) string {
	kn := &kubernautv1alpha1.Kubernaut{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace}}
	return resources.AdditionalComponentCRBName(kn, component, crName)
}

// createSharedEcosystemClusterRole creates a stand-in "user-provided"
// ClusterRole -- the kind of pre-existing role a cluster admin would grant
// out-of-band per DD-GATEWAY-018 -- and schedules its deletion via
// DeferCleanup so each test doesn't hand-roll the create/defer-delete
// boilerplate.
func createSharedEcosystemClusterRole(ctx context.Context, name string) {
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}}},
	}
	Expect(k8sClient.Create(ctx, cr)).To(Succeed())
	DeferCleanup(func() { _ = k8sClient.Delete(ctx, cr) })
}

// expectAdditionalCRBBound asserts that crName is bound, via an
// additional-component ClusterRoleBinding, to every component's
// ServiceAccount in components.
func expectAdditionalCRBBound(ctx context.Context, crName string, components []string) {
	for _, component := range components {
		crb := &rbacv1.ClusterRoleBinding{}
		name := additionalCRBName(component, crName)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, crb)).To(Succeed(),
			"expected additional CRB %q for component %q to exist", name, component)
		Expect(crb.Subjects).To(HaveLen(1))
		Expect(crb.Subjects[0].Name).To(Equal(resources.ServiceAccountName(component)))
	}
}

// expectAdditionalCRBPruned asserts that crName's additional-component
// ClusterRoleBinding no longer exists for every component in components.
func expectAdditionalCRBPruned(ctx context.Context, crName string, components []string) {
	for _, component := range components {
		crb := &rbacv1.ClusterRoleBinding{}
		name := additionalCRBName(component, crName)
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, crb)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"additional CRB %q for component %q should be pruned, got: %v", name, component, err)
	}
}

// Business acceptance criteria (#277, SC-7/AC-6 least-privilege): spec's
// additional-ClusterRole mechanism is shared by every component that
// performs owner-reference-chain resolution across ecosystem CRDs (KA,
// Gateway, EM) since none of them has a legitimate reason to see a
// different set of ecosystem CRDs than the others -- they all resolve the
// same owner chains. Generalized from v1alpha1's KA-only
// additionalClusterRoleBindings. A ClusterRole name in the list must bind
// to every applicable component's ServiceAccount, and must be pruned from
// all of them -- individually or via a component being disabled -- without
// operator intervention.
var _ = Describe("Additional component ClusterRoleBindings (#277)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("binds a user-specified ClusterRole to KubernautAgent, Gateway, and EffectivenessMonitor ServiceAccounts", func() {
		createBYOSecrets(ctx)
		createSharedEcosystemClusterRole(ctx, "shared-ecosystem-reader")

		cr := newCRWithRouteDisabled()
		// v1alpha1's KA-nested field still works -- conversion relocates it
		// to v1alpha2's top-level spec.additionalClusterRoles (#277).
		cr.Spec.KubernautAgent.AdditionalClusterRoleBindings = []string{"shared-ecosystem-reader"}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		reconcileToRunning(ctx)

		expectAdditionalCRBBound(ctx, "shared-ecosystem-reader", additionalRBACTestComponents)
	})

	It("prunes Gateway's additional-role CRB but keeps KA/EM's when gateway is later disabled", func() {
		createBYOSecrets(ctx)
		createSharedEcosystemClusterRole(ctx, "shared-ecosystem-reader-2")

		cr := newCRWithRouteDisabled()
		cr.Spec.KubernautAgent.AdditionalClusterRoleBindings = []string{"shared-ecosystem-reader-2"}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		reconcileToRunning(ctx)

		By("sanity: all 3 components are bound while gateway is enabled (the default)")
		expectAdditionalCRBBound(ctx, "shared-ecosystem-reader-2", additionalRBACTestComponents)

		By("disabling gateway")
		existing := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		f := false
		existing.Spec.Gateway.Enabled = &f
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())

		r := newReconciler()
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}

		By("verifying Gateway's additional CRB is pruned, KA/EM's remain")
		expectAdditionalCRBPruned(ctx, "shared-ecosystem-reader-2", []string{resources.ComponentGateway})
		expectAdditionalCRBBound(ctx, "shared-ecosystem-reader-2",
			[]string{resources.ComponentKubernautAgent, resources.ComponentEffectivenessMonitor})
	})

	It("removes all additional-component CRBs by finalizer", func() {
		createBYOSecrets(ctx)
		createSharedEcosystemClusterRole(ctx, "shared-ecosystem-reader-3")

		cr := newCRWithRouteDisabled()
		cr.Spec.KubernautAgent.AdditionalClusterRoleBindings = []string{"shared-ecosystem-reader-3"}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		r := reconcileToRunning(ctx)

		expectAdditionalCRBBound(ctx, "shared-ecosystem-reader-3", additionalRBACTestComponents)

		By("deleting the CR")
		kn := &kubernautv1alpha1.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
		Expect(k8sClient.Delete(ctx, kn)).To(Succeed())
		stripWorkflowNamespaceCreatedByAnnotation(ctx)

		By("reconciling the finalizer")
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())

		expectAdditionalCRBPruned(ctx, "shared-ecosystem-reader-3", additionalRBACTestComponents)
	})
})

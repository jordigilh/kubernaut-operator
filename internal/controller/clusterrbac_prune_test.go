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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// Business acceptance criteria (#341, SC-7/AC-6 least-privilege): a
// Kubernaut Agent, Fleet Gateway, or Fleet Metadata Cache ServiceAccount
// whose feature has been toggled off must not retain any cluster-scoped
// RBAC grant on the cluster after the next reconcile -- a leaked
// ClusterRole/ClusterRoleBinding is a standing over-privilege that no
// admission control catches, since it is never bound to the disabled
// feature's live workload again but remains bindable by anything that
// discovers its name. These tests assert the *absence* of the RBAC object,
// not any internal reconciler mechanism, so they hold regardless of how
// pruning is implemented.
var _ = Describe("Core cluster-scoped RBAC pruning on feature toggle-off (#341)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("removes FMC's #1993 scope-check-client and auth-middleware RBAC once fleetMetadataCache is disabled (regression)", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
		enableFleetMetadataCache(ctx)
		reconcileToRunning(ctx)

		By("sanity: the #1993 objects exist while FMC is enabled")
		authCR := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-auth-middleware",
		}, authCR)).To(Succeed())

		scopeCR := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fmc-scope-check-client",
		}, scopeCR)).To(Succeed())

		By("disabling fleetMetadataCache")
		existing := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		f := false
		existing.Spec.FleetMetadataCache.Enabled = &f
		existing.Spec.Fleet.Enabled = &f
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())

		r := newReconciler()
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}

		By("verifying the #1993 ClusterRoles are pruned, not just left behind")
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-auth-middleware",
		}, authCR)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"fleetmetadatacache-auth-middleware ClusterRole should be pruned after fleetMetadataCache.enabled=false, got: %v", err)

		err = k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fmc-scope-check-client",
		}, scopeCR)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"fmc-scope-check-client ClusterRole should be pruned after fleetMetadataCache.enabled=false, got: %v", err)

		By("verifying the #1993 ClusterRoleBindings are pruned")
		authCRB := &rbacv1.ClusterRoleBinding{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-auth-middleware-binding",
		}, authCRB)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"fleetmetadatacache-auth-middleware-binding CRB should be pruned after fleetMetadataCache.enabled=false, got: %v", err)

		roCRB := &rbacv1.ClusterRoleBinding{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-remediationorchestrator-fmc-scope-check-client",
		}, roCRB)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"remediationorchestrator-fmc-scope-check-client CRB should be pruned after fleetMetadataCache.enabled=false, got: %v", err)
	})

	It("removes Gateway's ClusterRole and ClusterRoleBinding once gateway is disabled (pre-existing gap)", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		reconcileToRunning(ctx)

		By("sanity: Gateway's cluster RBAC exists while enabled (the default)")
		gwCR := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-gateway-role",
		}, gwCR)).To(Succeed())

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

		By("verifying Gateway's ClusterRole and CRB are pruned")
		err := k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-gateway-role"}, gwCR)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"gateway-role ClusterRole should be pruned after gateway.enabled=false, got: %v", err)

		gwCRB := &rbacv1.ClusterRoleBinding{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-gateway-role-binding"}, gwCRB)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"gateway-role-binding CRB should be pruned after gateway.enabled=false, got: %v", err)
	})

	It("removes the console-access ClusterRole once apiFrontend is disabled (pre-existing gap)", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		reconcileToRunning(ctx)

		By("sanity: console-access ClusterRole exists while apiFrontend is enabled (the default)")
		caCR := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-console-access",
		}, caCR)).To(Succeed())

		By("disabling apiFrontend")
		existing := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		f := false
		existing.Spec.APIFrontend.Enabled = &f
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())

		r := newReconciler()
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}

		By("verifying the console-access ClusterRole is pruned")
		err := k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-console-access"}, caCR)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"console-access ClusterRole should be pruned after apiFrontend.enabled=false, got: %v", err)
	})
})

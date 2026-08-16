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

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// Business acceptance criteria (#341/#352 backport, SC-7/AC-6 least-privilege):
// a ServiceAccount whose feature has been toggled off must not retain any
// cluster-scoped RBAC grant on the cluster after the next reconcile -- a
// leaked ClusterRole/ClusterRoleBinding is a standing over-privilege that no
// admission control catches, since it is never bound to the disabled
// feature's live workload again but remains bindable by anything that
// discovers its name. These tests assert the *absence* of the RBAC object,
// not any internal reconciler mechanism, so they hold regardless of how
// pruning is implemented.
//
// This is the release/v1.5 (v1alpha1) backport of main's clusterrbac_prune_test.go
// (#341/#343). FleetMetadataCache/Fleet/MCP-Gateway-namespace do not exist on
// this line, so the regression cases ported here are the ones that also
// reproduce on v1alpha1: Gateway and APIFrontend/console-access toggle-off
// (both pre-existing gaps, not v1.6-specific regressions).
var _ = Describe("Core cluster-scoped RBAC pruning on feature toggle-off (#341/#352)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
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
		existing := &kubernautv1alpha1.Kubernaut{}
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

	It("removes the console-access ClusterRole and apifrontend RBAC once apiFrontend is disabled (pre-existing gap)", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		reconcileToRunning(ctx)

		By("sanity: console-access and apifrontend ClusterRoles exist while apiFrontend is enabled (the default)")
		caCR := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-console-access",
		}, caCR)).To(Succeed())

		afCR := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-apifrontend-role",
		}, afCR)).To(Succeed())

		By("disabling apiFrontend")
		existing := &kubernautv1alpha1.Kubernaut{}
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

		By("verifying the apifrontend-role ClusterRole and binding are pruned")
		err = k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-apifrontend-role"}, afCR)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"apifrontend-role ClusterRole should be pruned after apiFrontend.enabled=false, got: %v", err)

		afCRB := &rbacv1.ClusterRoleBinding{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-apifrontend-binding"}, afCRB)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"apifrontend-binding CRB should be pruned after apiFrontend.enabled=false, got: %v", err)
	})

	It("removes monitoring-view ClusterRoleBindings once monitoring is disabled (pre-existing gap)", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		reconcileToRunning(ctx)

		By("sanity: monitoring-view CRBs exist while monitoring is enabled (the default)")
		emMonCRB := &rbacv1.ClusterRoleBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-effectivenessmonitor-monitoring-view",
		}, emMonCRB)).To(Succeed())

		By("disabling monitoring")
		existing := &kubernautv1alpha1.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		f := false
		existing.Spec.Monitoring.Enabled = &f
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())

		r := newReconciler()
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}

		By("verifying monitoring-view CRBs are pruned")
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-effectivenessmonitor-monitoring-view",
		}, emMonCRB)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"effectivenessmonitor-monitoring-view CRB should be pruned after monitoring.enabled=false, got: %v", err)

		kaMonCRB := &rbacv1.ClusterRoleBinding{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-kubernaut-agent-monitoring-view",
		}, kaMonCRB)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"kubernaut-agent-monitoring-view CRB should be pruned after monitoring.enabled=false, got: %v", err)
	})

	It("removes every core cluster-scoped RBAC object when the Kubernaut CR itself is deleted, even under a spec state that no longer enumerates it (finalizer blind-spot)", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		reconcileToRunning(ctx)

		By("sanity: gateway-role ClusterRole exists while gateway is enabled (the default)")
		gwCR := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-gateway-role"}, gwCR)).To(Succeed())

		By("disabling gateway WITHOUT reconciling first, simulating a spec state change that lands just before deletion")
		existing := &kubernautv1alpha1.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		f := false
		existing.Spec.Gateway.Enabled = &f
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())
		Expect(k8sClient.Delete(ctx, existing)).To(Succeed())

		r := newReconciler()
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			if errors.IsNotFound(err) {
				break
			}
			Expect(err).NotTo(HaveOccurred())
		}

		By("verifying gateway-role ClusterRole is gone even though the finalizer's spec-recompute would no longer enumerate it")
		err := k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-gateway-role"}, gwCR)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"gateway-role ClusterRole should be pruned on CR deletion regardless of the spec state at delete time, got: %v", err)
	})
})

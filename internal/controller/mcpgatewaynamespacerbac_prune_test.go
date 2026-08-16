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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// Business acceptance criteria (#354, AC-6 least-privilege): a namespace-
// scoped Role/RoleBinding granting MCP Gateway CRD read access (FMC/SP) must
// not persist in a namespace that no longer matches the component's current
// effective mcpGatewayNamespace -- whether that namespace changed via the
// shared spec.fleet.mcpGatewayNamespace default, a per-component override,
// or the CR was deleted before a normal reconcile had a chance to prune the
// stale copy. A name-only diff (as #341's cluster-scoped prune uses) is not
// sufficient here because the same logical role name is expected to exist in
// different namespaces over the CR's lifetime -- pruning must be keyed by
// (namespace, name).
var _ = Describe("Namespace-scoped MCP Gateway RBAC pruning on namespace change (#354)", func() {
	ctx := context.Background()

	const (
		fmcNSa    = "fmc-mcpgw-prune-ns-a"
		fmcNSb    = "fmc-mcpgw-prune-ns-b"
		sharedNS  = "shared-mcpgw-prune-ns"
		overrideA = "fmc-override-prune-ns-a"
		overrideB = "fmc-override-prune-ns-b"
	)

	ensureNamespace := func(name string) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		err := k8sClient.Create(ctx, ns)
		Expect(err == nil || errors.IsAlreadyExists(err)).To(BeTrue(), "creating namespace %s: %v", name, err)
	}

	// cleanupMCPGatewayNamespaceRoleRBAC removes every Role/RoleBinding
	// carrying LabelMCPGatewayNamespaceRBAC across all namespaces, since
	// these tests deliberately create objects outside testNamespace (which
	// cleanupNamespacedResources does not sweep).
	cleanupMCPGatewayNamespaceRoleRBAC := func() {
		roleList := &rbacv1.RoleList{}
		_ = k8sClient.List(ctx, roleList, client.MatchingLabels{resources.LabelMCPGatewayNamespaceRBAC: resources.LabelValueTrue})
		for i := range roleList.Items {
			_ = k8sClient.Delete(ctx, &roleList.Items[i])
		}
		rbList := &rbacv1.RoleBindingList{}
		_ = k8sClient.List(ctx, rbList, client.MatchingLabels{resources.LabelMCPGatewayNamespaceRBAC: resources.LabelValueTrue})
		for i := range rbList.Items {
			_ = k8sClient.Delete(ctx, &rbList.Items[i])
		}
	}

	AfterEach(func() {
		cleanupMCPGatewayNamespaceRoleRBAC()
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("prunes FMC and SignalProcessing's Role/RoleBinding from the old shared mcpGatewayNamespace once it changes to a new namespace", func() {
		ensureNamespace(fmcNSa)
		ensureNamespace(fmcNSb)
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
		fleet := defaultFMCFleetSpec()
		fleet.MCPGatewayNamespace = fmcNSa
		enableFleetMetadataCacheWithFleet(ctx, fleet)
		reconcileToRunning(ctx)

		By("sanity: FMC and SP Roles exist in the original shared namespace")
		fmcRole := &rbacv1.Role{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: fmcNSa,
		}, fmcRole)).To(Succeed())
		spRole := &rbacv1.Role{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-signalprocessing-mcpgateway", Namespace: fmcNSa,
		}, spRole)).To(Succeed())

		By("changing the shared mcpGatewayNamespace to a different namespace")
		existing := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		existing.Spec.Fleet.MCPGatewayNamespace = fmcNSb
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())

		r := newReconciler()
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}

		By("verifying the old namespace's Role/RoleBinding are pruned, not just left behind")
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: fmcNSa,
		}, fmcRole)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"fleetmetadatacache-mcpgateway Role in the old namespace should be pruned, got: %v", err)

		err = k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-signalprocessing-mcpgateway", Namespace: fmcNSa,
		}, spRole)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"signalprocessing-mcpgateway Role in the old namespace should be pruned, got: %v", err)

		fmcRB := &rbacv1.RoleBinding{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-mcpgateway-binding", Namespace: fmcNSa,
		}, fmcRB)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"fleetmetadatacache-mcpgateway-binding RoleBinding in the old namespace should be pruned, got: %v", err)

		By("verifying the new namespace's Role/RoleBinding now exist")
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: fmcNSb,
		}, fmcRole)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-signalprocessing-mcpgateway", Namespace: fmcNSb,
		}, spRole)).To(Succeed())
	})

	It("prunes FMC's own mcpGatewayNamespace override RBAC when the override changes, without touching SignalProcessing's independently-resolved shared namespace", func() {
		ensureNamespace(sharedNS)
		ensureNamespace(overrideA)
		ensureNamespace(overrideB)
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
		fleet := defaultFMCFleetSpec()
		fleet.MCPGatewayNamespace = sharedNS
		enableFleetMetadataCacheWithFleet(ctx, fleet)

		existing := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		existing.Spec.FleetMetadataCache.Fleet = &kubernautv1alpha2.FleetOverrideSpec{Namespace: overrideA}
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())

		reconcileToRunning(ctx)

		By("sanity: FMC's Role is in its override namespace, SP's Role is in the shared namespace")
		fmcRole := &rbacv1.Role{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: overrideA,
		}, fmcRole)).To(Succeed())
		spRole := &rbacv1.Role{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-signalprocessing-mcpgateway", Namespace: sharedNS,
		}, spRole)).To(Succeed())

		By("changing only FMC's own namespace override")
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		existing.Spec.FleetMetadataCache.Fleet = &kubernautv1alpha2.FleetOverrideSpec{Namespace: overrideB}
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())

		r := newReconciler()
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}

		By("verifying FMC's old override namespace Role is pruned")
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: overrideA,
		}, fmcRole)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"fleetmetadatacache-mcpgateway Role in the old override namespace should be pruned, got: %v", err)

		By("verifying FMC's new override namespace Role exists")
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: overrideB,
		}, fmcRole)).To(Succeed())

		By("verifying SignalProcessing's Role in the shared namespace was left untouched")
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-signalprocessing-mcpgateway", Namespace: sharedNS,
		}, spRole)).To(Succeed())
	})

	It("removes FMC's namespace-scoped RBAC from a namespace no longer reflected by the spec when the CR is deleted before a reconcile prunes it (finalizer path regression)", func() {
		ensureNamespace(fmcNSa)
		ensureNamespace(fmcNSb)
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
		fleet := defaultFMCFleetSpec()
		fleet.MCPGatewayNamespace = fmcNSa
		enableFleetMetadataCacheWithFleet(ctx, fleet)
		reconcileToRunning(ctx)

		By("sanity: FMC's Role exists in the original namespace")
		fmcRole := &rbacv1.Role{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: fmcNSa,
		}, fmcRole)).To(Succeed())

		By("changing the namespace and deleting the CR in the same beat, without an intervening reconcile")
		existing := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		existing.Spec.Fleet.MCPGatewayNamespace = fmcNSb
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())
		Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
		stripWorkflowNamespaceCreatedByAnnotation(ctx)

		r := newReconciler()
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			if errors.IsNotFound(err) {
				break
			}
			Expect(err).NotTo(HaveOccurred())
		}

		By("verifying the stale namespace's Role is removed even though it never matched the CR's final spec value")
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: fmcNSa,
		}, fmcRole)
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"fleetmetadatacache-mcpgateway Role in the stale namespace should be removed by the finalizer path, got: %v", err)
	})
})

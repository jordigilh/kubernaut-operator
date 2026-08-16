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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// isMCPGatewayNamespaceRBACLabeled reports whether obj is a Role/RoleBinding
// carrying LabelMCPGatewayNamespaceRBAC, used by the failing-client wrappers
// below to scope injected failures to exactly the objects
// deployMCPGatewayNamespaceRBAC/pruneOrphanedMCPGatewayNamespaceRBAC manage
// (#354), leaving every other RBAC object's Create/Update/Delete unaffected
// so the rest of a normal reconcile still succeeds.
func isMCPGatewayNamespaceRBACLabeled(obj client.Object) bool {
	switch obj.(type) {
	case *rbacv1.Role, *rbacv1.RoleBinding:
	default:
		return false
	}
	return obj.GetLabels()[resources.LabelMCPGatewayNamespaceRBAC] == resources.LabelValueTrue
}

// mcpGatewayNamespaceRoleWriteFailingClient wraps a real client but returns
// an error on Create/Update calls for Role and/or RoleBinding objects
// carrying LabelMCPGatewayNamespaceRBAC (per failRole/failRoleBinding),
// simulating an RBAC provisioning failure inside
// deployMCPGatewayNamespaceRBAC's ensureUnowned calls (#354). Split by
// object kind (rather than a single failEither flag) so a test can fail
// only the RoleBinding half while the Role half still succeeds --
// deployMCPGatewayNamespaceRBAC ensures all Roles before any RoleBinding,
// so failing both indistinguishably would only ever exercise the Role
// branch.
type mcpGatewayNamespaceRoleWriteFailingClient struct {
	client.Client
	failRole        bool
	failRoleBinding bool
}

func (c *mcpGatewayNamespaceRoleWriteFailingClient) shouldFail(obj client.Object) bool {
	if !isMCPGatewayNamespaceRBACLabeled(obj) {
		return false
	}
	switch obj.(type) {
	case *rbacv1.Role:
		return c.failRole
	case *rbacv1.RoleBinding:
		return c.failRoleBinding
	default:
		return false
	}
}

func (c *mcpGatewayNamespaceRoleWriteFailingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if c.shouldFail(obj) {
		return fmt.Errorf("simulated: forbidden creating %s/%s", obj.GetNamespace(), obj.GetName())
	}
	return c.Client.Create(ctx, obj, opts...)
}

func (c *mcpGatewayNamespaceRoleWriteFailingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if c.shouldFail(obj) {
		return fmt.Errorf("simulated: forbidden updating %s/%s", obj.GetNamespace(), obj.GetName())
	}
	return c.Client.Update(ctx, obj, opts...)
}

// mcpGatewayNamespaceRoleDeleteFailingClient wraps a real client but returns
// an error on Delete calls for Role/RoleBinding objects carrying
// LabelMCPGatewayNamespaceRBAC, simulating a persistent cleanup failure
// inside pruneUndesiredNamespaced's deleteIfExists calls (#354).
type mcpGatewayNamespaceRoleDeleteFailingClient struct {
	client.Client
}

func (c *mcpGatewayNamespaceRoleDeleteFailingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if isMCPGatewayNamespaceRBACLabeled(obj) {
		return fmt.Errorf("simulated: forbidden deleting %s/%s", obj.GetNamespace(), obj.GetName())
	}
	return c.Client.Delete(ctx, obj, opts...)
}

// mcpGatewayNamespaceRoleListFailingClient wraps a real client but returns
// an error on List calls for the list type(s) selected by failRoleList/
// failRoleBindingList, simulating an apiserver failure inside
// pruneOrphanedMCPGatewayNamespaceRBAC's label-selector list calls (#354).
type mcpGatewayNamespaceRoleListFailingClient struct {
	client.Client
	failRoleList        bool
	failRoleBindingList bool
}

func (c *mcpGatewayNamespaceRoleListFailingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	switch list.(type) {
	case *rbacv1.RoleList:
		if c.failRoleList {
			return fmt.Errorf("simulated: forbidden listing Roles")
		}
	case *rbacv1.RoleBindingList:
		if c.failRoleBindingList {
			return fmt.Errorf("simulated: forbidden listing RoleBindings")
		}
	}
	return c.Client.List(ctx, list, opts...)
}

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

	// Business acceptance criteria (AGENTS.md GA Readiness Audit #10,
	// "Fail-Open Safety": no silent failures -- all error paths are
	// observable): if the apiserver itself rejects a create/update/delete/
	// list call during MCP Gateway namespace RBAC provisioning or pruning,
	// the reconciler must surface that failure (return an error, and for
	// the deploy path, an RBACProvisioned=False condition) rather than
	// silently treating the reconcile as successful while a Role/
	// RoleBinding is left missing, stale, or unverified.
	Context("error paths (#354)", func() {
		It("surfaces an error and sets RBACProvisioned=False when creating FMC's namespace-scoped Role fails", func() {
			ensureNamespace(fmcNSa)
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			fleet := defaultFMCFleetSpec()
			fleet.MCPGatewayNamespace = fmcNSa
			enableFleetMetadataCacheWithFleet(ctx, fleet)

			r := newReconciler()
			By("reconcile 1: add finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("reconcile 2: validate + start migration")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			markMigrationJobComplete(ctx)

			By("injecting a client that fails creating MCP Gateway namespace Roles")
			r.Client = &mcpGatewayNamespaceRoleWriteFailingClient{Client: k8sClient, failRole: true}

			By("reconcile 3: migration complete + deploy -- FMC's namespace Role creation should fail")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).To(HaveOccurred())

			By("restoring the real client to read status")
			r.Client = k8sClient
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionRBACProvisioned)
			Expect(cond).NotTo(BeNil(), "RBACProvisioned condition should exist")
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(ReasonRBACApplyFailed))
		})

		It("surfaces an error and sets RBACProvisioned=False when creating FMC's namespace-scoped RoleBinding fails", func() {
			ensureNamespace(fmcNSa)
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			fleet := defaultFMCFleetSpec()
			fleet.MCPGatewayNamespace = fmcNSa
			enableFleetMetadataCacheWithFleet(ctx, fleet)

			r := newReconciler()
			By("reconcile 1: add finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("reconcile 2: validate + start migration")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			markMigrationJobComplete(ctx)

			By("injecting a client that fails creating MCP Gateway namespace RoleBindings only (Roles still succeed)")
			r.Client = &mcpGatewayNamespaceRoleWriteFailingClient{Client: k8sClient, failRoleBinding: true}

			By("reconcile 3: migration complete + deploy -- FMC's namespace RoleBinding creation should fail")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).To(HaveOccurred())

			By("restoring the real client to read status")
			r.Client = k8sClient
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionRBACProvisioned)
			Expect(cond).NotTo(BeNil(), "RBACProvisioned condition should exist")
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(ReasonRBACApplyFailed))
		})

		It("surfaces an error, and leaves the stale Role in place, when pruning FMC's old-namespace Role fails", func() {
			ensureNamespace(fmcNSa)
			ensureNamespace(fmcNSb)
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			fleet := defaultFMCFleetSpec()
			fleet.MCPGatewayNamespace = fmcNSa
			enableFleetMetadataCacheWithFleet(ctx, fleet)
			reconcileToRunning(ctx)

			By("sanity: FMC's Role exists in the original namespace")
			staleRole := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: fmcNSa,
			}, staleRole)).To(Succeed())

			By("changing the shared mcpGatewayNamespace so the old namespace's Role becomes orphaned")
			existing := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
			existing.Spec.Fleet.MCPGatewayNamespace = fmcNSb
			Expect(k8sClient.Update(ctx, existing)).To(Succeed())

			r := newReconciler()
			By("injecting a client that fails deleting MCP Gateway namespace Roles/RoleBindings")
			r.Client = &mcpGatewayNamespaceRoleDeleteFailingClient{Client: k8sClient}

			By("reconciling -- pruning the now-orphaned Role should fail and surface an error")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).To(HaveOccurred())

			By("verifying the stale Role was not silently dropped from status/observability -- it's still on the cluster")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: fmcNSa,
			}, staleRole)
			Expect(err).NotTo(HaveOccurred(),
				"the stale Role should still exist -- a failed prune must not be silently treated as success")
		})

		It("surfaces an error when listing live Roles fails during MCP Gateway namespace RBAC pruning", func() {
			ensureNamespace(fmcNSa)
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			fleet := defaultFMCFleetSpec()
			fleet.MCPGatewayNamespace = fmcNSa
			enableFleetMetadataCacheWithFleet(ctx, fleet)
			reconcileToRunning(ctx)

			r := newReconciler()
			r.Client = &mcpGatewayNamespaceRoleListFailingClient{Client: k8sClient, failRoleList: true}

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).To(HaveOccurred())
		})

		It("surfaces an error when listing live RoleBindings fails during MCP Gateway namespace RBAC pruning", func() {
			ensureNamespace(fmcNSa)
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			fleet := defaultFMCFleetSpec()
			fleet.MCPGatewayNamespace = fmcNSa
			enableFleetMetadataCacheWithFleet(ctx, fleet)
			reconcileToRunning(ctx)

			r := newReconciler()
			r.Client = &mcpGatewayNamespaceRoleListFailingClient{Client: k8sClient, failRoleBindingList: true}

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).To(HaveOccurred())
		})
	})
})

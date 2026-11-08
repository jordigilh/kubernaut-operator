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
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

func inventoryWiringHasRule(role *rbacv1.ClusterRole, apiGroup, resource string) bool {
	for _, rule := range role.Rules {
		if slices.Contains(rule.APIGroups, apiGroup) && slices.Contains(rule.Resources, resource) {
			return true
		}
	}
	return false
}

var _ = Describe("API Frontend inventory RBAC wiring (#623)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
		_ = k8sClient.Delete(ctx, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "inventory-dynamic-reader"}})
	})

	It("IT-RBAC-623-001 creates a separate AF inventory role from approved dynamic reads", func() {
		createBYOSecrets(ctx)
		additional := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-dynamic-reader"},
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{"widgets.example.io"}, Resources: []string{"widgets"}, Verbs: []string{"get", "list", "watch"}},
				{APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{"bootstrap"}, Verbs: []string{"get", "list"}},
			},
		}
		Expect(k8sClient.Create(ctx, additional)).To(Succeed())

		cr := newMinimalCR()
		cr.Spec.AdditionalClusterRoles = []string{additional.Name}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		reconcileToDeployPhase(ctx)

		role := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.APIFrontendInventoryReadClusterRoleName(&kubernautv1alpha2.Kubernaut{
			ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace},
		})}, role)).To(Succeed())
		Expect(inventoryWiringHasRule(role, "widgets.example.io", "widgets")).To(BeTrue())
		Expect(inventoryWiringHasRule(role, "", "secrets")).To(BeTrue())

		Expect(role.Name).NotTo(Equal(testNamespace + "-gateway-role"))
		for _, rule := range role.Rules {
			Expect(rule.NonResourceURLs).To(BeEmpty())
			for _, resource := range rule.Resources {
				Expect(resource).NotTo(ContainSubstring("/"), "inventory role must not grant subresources")
			}
			Expect(rule.Verbs).To(ConsistOf("get", "list"))
		}

		binding := &rbacv1.ClusterRoleBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.APIFrontendInventoryReadClusterRoleBindingName(&kubernautv1alpha2.Kubernaut{
			ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace},
		})}, binding)).To(Succeed())
		Expect(binding.RoleRef.Name).To(Equal(role.Name))
		Expect(binding.Subjects).To(ConsistOf(rbacv1.Subject{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      resources.ServiceAccountName(resources.ComponentAPIFrontend),
			Namespace: testNamespace,
		}))
	})

	It("IT-RBAC-623-002 replaces revoked additional-role reads on reconcile", func() {
		createBYOSecrets(ctx)
		additional := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-dynamic-reader"},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{"widgets.example.io"}, Resources: []string{"widgets"}, Verbs: []string{"get", "list"},
			}},
		}
		Expect(k8sClient.Create(ctx, additional)).To(Succeed())

		cr := newMinimalCR()
		cr.Spec.AdditionalClusterRoles = []string{additional.Name}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		reconciler := reconcileToDeployPhase(ctx)

		roleKey := types.NamespacedName{Name: resources.APIFrontendInventoryReadClusterRoleName(cr), Namespace: ""}
		role := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, roleKey, role)).To(Succeed())
		Expect(inventoryWiringHasRule(role, "widgets.example.io", "widgets")).To(BeTrue())

		additional.Rules = []rbacv1.PolicyRule{{
			APIGroups: []string{"apps.example.io"}, Resources: []string{"widgets"}, Verbs: []string{"get", "list"},
		}}
		Expect(k8sClient.Update(ctx, additional)).To(Succeed())
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, roleKey, role)).To(Succeed())
		Expect(inventoryWiringHasRule(role, "widgets.example.io", "widgets")).To(BeFalse())
		Expect(inventoryWiringHasRule(role, "apps.example.io", "widgets")).To(BeTrue())
	})

	It("IT-RBAC-623-003 prunes the projection when AF is disabled", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		reconcileToDeployPhase(ctx)

		roleKey := types.NamespacedName{Name: resources.APIFrontendInventoryReadClusterRoleName(cr)}
		Expect(k8sClient.Get(ctx, roleKey, &rbacv1.ClusterRole{})).To(Succeed())

		existing := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		disabled := false
		existing.Spec.APIFrontend.Enabled = &disabled
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())
		for i := 0; i < 3; i++ {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}

		err := k8sClient.Get(ctx, roleKey, &rbacv1.ClusterRole{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "inventory role should be pruned, got: %v", err)
		err = k8sClient.Get(ctx, types.NamespacedName{Name: resources.APIFrontendInventoryReadClusterRoleBindingName(existing)}, &rbacv1.ClusterRoleBinding{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "inventory binding should be pruned, got: %v", err)
	})

	It("IT-RBAC-623-004 keeps AF inventory RBAC when Gateway is disabled", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		disabled := false
		cr.Spec.Gateway.Enabled = &disabled
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		reconcileToDeployPhase(ctx)

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.APIFrontendInventoryReadClusterRoleName(cr)}, &rbacv1.ClusterRole{})).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.APIFrontendInventoryReadClusterRoleBindingName(cr)}, &rbacv1.ClusterRoleBinding{})).To(Succeed())
	})

	It("IT-RBAC-623-005 maps source-role and generated-binding drift to the Kubernaut", func() {
		cr := newMinimalCR()
		cr.Spec.AdditionalClusterRoles = []string{"inventory-dynamic-reader"}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		r := newReconciler()
		requests := r.inventorySourceClusterRoleToKubernaut(ctx, &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory-dynamic-reader"},
		})
		Expect(requests).To(ConsistOf(reconcile.Request{NamespacedName: singletonKey()}))

		requests = r.inventoryBindingToKubernaut(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: resources.APIFrontendInventoryReadClusterRoleBindingName(cr)},
		})
		Expect(requests).To(ConsistOf(reconcile.Request{NamespacedName: singletonKey()}))
	})
})

var _ = Describe("API Frontend inventory RBAC unit decisions (#623)", Label("unit"), func() {
	It("covers source-role omission, success, disabled, and read-error outcomes", func() {
		kn := newMinimalCR()
		readerRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "unit-inventory-reader"},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{"example.io"}, Resources: []string{"widgets"}, Verbs: []string{"get", "list"},
			}},
		}
		kn.Spec.AdditionalClusterRoles = []string{readerRole.Name}
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.Create(ctx, readerRole)).To(Succeed())

		roles, err := r.inventoryReadSourceRoles(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(roles).To(ConsistOf(*readerRole))

		kn.Spec.AdditionalClusterRoles = []string{"missing-inventory-reader"}
		roles, err = r.inventoryReadSourceRoles(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(roles).To(BeEmpty())

		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		roles, err = r.inventoryReadSourceRoles(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(roles).To(BeNil())

		enabled := true
		kn.Spec.APIFrontend.Enabled = &enabled
		kn.Spec.AdditionalClusterRoles = []string{readerRole.Name, "denied-inventory-reader"}
		denied := businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if key.Name == "denied-inventory-reader" {
				return fmt.Errorf("inventory source read denied")
			}
			return api.Get(ctx, key, object, options...)
		}})
		readerRoleForDenied := readerRole.DeepCopy()
		readerRoleForDenied.ResourceVersion = ""
		readerRoleForDenied.UID = ""
		Expect(denied.Create(ctx, readerRoleForDenied)).To(Succeed())
		roles, err = denied.inventoryReadSourceRoles(ctx, kn, kn)
		Expect(roles).To(BeEmpty())
		Expect(err).To(MatchError(ContainSubstring("inventory source read denied")))

		roles, err = r.inventoryReadSourceRoles(ctx, nil, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(roles).To(BeNil())
	})

	It("covers source-role and binding watch mapper fallbacks, filters, matches, and errors", func() {
		kn := newMinimalCR()
		kn.Spec.AdditionalClusterRoles = []string{"unit-inventory-reader"}
		r := businessUnitReconciler(kn, interceptor.Funcs{})

		other := kn.DeepCopy()
		other.Namespace = "other"
		other.ResourceVersion = ""
		other.UID = ""
		other.Spec.AdditionalClusterRoles = nil
		Expect(r.Create(ctx, other)).To(Succeed())

		roleEvent := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "unit-inventory-reader"}}
		bindingEvent := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{
			Name: resources.APIFrontendInventoryReadClusterRoleBindingName(kn),
		}}

		r.APIReader = nil
		Expect(r.inventorySourceClusterRoleToKubernaut(ctx, nil)).To(BeNil())
		Expect(r.inventoryBindingToKubernaut(ctx, nil)).To(BeNil())
		Expect(r.inventorySourceClusterRoleToKubernaut(ctx, roleEvent)).To(ConsistOf(
			reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}))
		Expect(r.inventoryBindingToKubernaut(ctx, bindingEvent)).To(ConsistOf(
			reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}))

		listDenied := businessUnitReconciler(kn, interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return fmt.Errorf("inventory mapper list denied")
		}})
		Expect(listDenied.inventorySourceClusterRoleToKubernaut(ctx, roleEvent)).To(BeNil())
		Expect(listDenied.inventoryBindingToKubernaut(ctx, bindingEvent)).To(BeNil())
	})

	It("reconciles the safe projection before returning a source-role read error", func() {
		kn := newMinimalCR()
		kn.Spec.AdditionalClusterRoles = []string{"denied-inventory-reader"}
		r := businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if key.Name == "denied-inventory-reader" {
				return fmt.Errorf("inventory source read denied")
			}
			return api.Get(ctx, key, object, options...)
		}})

		err := r.deployClusterScopedCoreRBAC(ctx, kn, kn)
		Expect(err).To(MatchError(ContainSubstring("resolving AF inventory source ClusterRoles")))
		role := &rbacv1.ClusterRole{}
		Expect(r.Get(ctx, client.ObjectKey{Name: resources.APIFrontendInventoryReadClusterRoleName(kn)}, role)).To(Succeed())
	})
})

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
	authorizationv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// Issue #663 integration proof (Pyramid Invariant: IT proves wiring): the
// user-side local-target grant for the console inventory projection must
// reach the cluster through a real reconcile, stay unbound (deny-by-default
// until an administrator binds it), and be pruned once apiFrontend is
// disabled. The assertions target the live objects by name, not reconciler
// internals.
var _ = Describe("Console inventory user-side target grants (#663)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("IT-CONSOLE-663-001 renders the console-inventory-reader role unbound after a real reconcile", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		reconcileToDeployPhase(ctx)

		role := &rbacv1.ClusterRole{}
		roleKey := types.NamespacedName{Name: resources.ConsoleInventoryReaderClusterRoleName(cr)}
		Expect(k8sClient.Get(ctx, roleKey, role)).To(Succeed())

		By("pinning the rendered grant to the pinned upstream allowlist contract")
		Expect(role.Rules).To(Equal(resources.ConsoleInventoryReaderClusterRole(cr).Rules))

		By("leaving the role unbound: no operator-rendered binding references it")
		crbs := &rbacv1.ClusterRoleBindingList{}
		Expect(k8sClient.List(ctx, crbs)).To(Succeed())
		for _, crb := range crbs.Items {
			Expect(crb.RoleRef.Name).NotTo(Equal(role.Name),
				"the inventory role must stay unbound by default; binding is the administrator's decision (#662)")
		}
		rbs := &rbacv1.RoleBindingList{}
		Expect(k8sClient.List(ctx, rbs)).To(Succeed())
		for _, rb := range rbs.Items {
			Expect(rb.RoleRef.Name).NotTo(Equal(role.Name),
				"a namespaced RoleBinding would also bind the inventory role; the operator must render none")
		}
	})

	It("IT-CONSOLE-663-002 prunes the console-inventory-reader role once apiFrontend is disabled", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
		reconcileToRunning(ctx)

		roleKey := types.NamespacedName{Name: testNamespace + "-console-inventory-reader"}
		By("sanity: the role exists while apiFrontend is enabled")
		Expect(k8sClient.Get(ctx, roleKey, &rbacv1.ClusterRole{})).To(Succeed())

		By("disabling apiFrontend")
		existing := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
		f := false
		existing.Spec.APIFrontend.Enabled = &f
		Expect(k8sClient.Update(ctx, existing)).To(Succeed())

		Eventually(func() error {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			return err
		}, "60s", "2s").Should(Succeed())

		By("verifying the role is pruned, not just left behind")
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, roleKey, &rbacv1.ClusterRole{}))
		}, "60s", "2s").Should(BeTrue(),
			"console-inventory-reader ClusterRole should be pruned after apiFrontend.enabled=false")
	})

	It("IT-CONSOLE-663-003 satisfies a local-target list SAR for an allowlisted kind once an administrator binds the role", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		reconcileToDeployPhase(ctx)

		By("binding the rendered role to a group the way an administrator would")
		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: testNamespace + "-console-inventory-reader-e2e"},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     resources.ConsoleInventoryReaderClusterRoleName(cr),
			},
			Subjects: []rbacv1.Subject{{
				Kind:     rbacv1.GroupKind,
				APIGroup: rbacv1.GroupName,
				Name:     "console-inventory-readers",
			}},
		}
		Expect(k8sClient.Create(ctx, crb)).To(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, crb)
		}()

		By("running the exact local-target SAR shape AF performs, as the bound user")
		sar := func(verb, group, resource string) *authorizationv1.SubjectAccessReview {
			review := &authorizationv1.SubjectAccessReview{
				Spec: authorizationv1.SubjectAccessReviewSpec{
					User:   "console-user@example.invalid",
					Groups: []string{"console-inventory-readers"},
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Verb: verb, Group: group, Resource: resource,
					},
				},
			}
			Expect(k8sClient.Create(ctx, review)).To(Succeed())
			return review
		}
		Expect(sar("list", "", "pods").Status.Allowed).To(BeTrue(),
			"a bound user must satisfy the projection's list SAR on pods")
		Expect(sar("get", "apps", "deployments").Status.Allowed).To(BeTrue(),
			"a bound user must satisfy the projection's exact-get SAR on deployments")

		By("denying what the role excludes")
		Expect(sar("watch", "", "pods").Status.Allowed).To(BeFalse())
		Expect(sar("get", "", "secrets").Status.Allowed).To(BeFalse(),
			"secrets are outside the upstream allowlist and must stay denied")
		Expect(sar("use", "kubernaut.ai", "tools").Status.Allowed).To(BeFalse(),
			"the inventory role must not satisfy the console capability SAR")
	})

	It("IT-CONSOLE-663-004 scopes a namespaced RoleBinding to namespace-filtered reads only", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		reconcileToDeployPhase(ctx)

		By("binding the role in one namespace only")
		rb := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "console-inventory-reader-scoped", Namespace: testNamespace},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     resources.ConsoleInventoryReaderClusterRoleName(cr),
			},
			Subjects: []rbacv1.Subject{{
				Kind:     rbacv1.GroupKind,
				APIGroup: rbacv1.GroupName,
				Name:     "console-inventory-readers-ns",
			}},
		}
		Expect(k8sClient.Create(ctx, rb)).To(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, rb)
		}()

		sar := func(resource, namespace string) *authorizationv1.SubjectAccessReview {
			review := &authorizationv1.SubjectAccessReview{
				Spec: authorizationv1.SubjectAccessReviewSpec{
					User:   "console-user@example.invalid",
					Groups: []string{"console-inventory-readers-ns"},
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Verb: "list", Group: "", Resource: resource, Namespace: namespace,
					},
				},
			}
			Expect(k8sClient.Create(ctx, review)).To(Succeed())
			return review
		}

		Expect(sar("pods", testNamespace).Status.Allowed).To(BeTrue(),
			"a namespaced RoleBinding must satisfy a namespace-filtered list SAR in its namespace")
		Expect(sar("pods", "another-namespace").Status.Allowed).To(BeFalse(),
			"a namespaced RoleBinding must not satisfy a list SAR in another namespace")
		Expect(sar("pods", "").Status.Allowed).To(BeFalse(),
			"the default unfiltered list SAR is cluster-scoped and needs a cluster-level binding")
		Expect(sar("nodes", "").Status.Allowed).To(BeFalse(),
			"cluster-scoped kinds always need a cluster-level binding")
	})
})

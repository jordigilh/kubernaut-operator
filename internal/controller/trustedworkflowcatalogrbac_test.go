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

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

func serviceAccountUser(namespace, name string) string {
	return fmt.Sprintf("system:serviceaccount:%s:%s", namespace, name)
}

func subjectAccessAllowed(ctx context.Context, user, namespace, group, resource, verb string) (bool, error) {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return false, fmt.Errorf("creating authorization client: %w", err)
	}
	review := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User: user,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Group:     group,
				Resource:  resource,
				Verb:      verb,
				Namespace: namespace,
			},
		},
	}
	result, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Errorf("creating SubjectAccessReview for %s %s/%s in %s: %w", verb, group, resource, namespace, err)
	}
	return result.Status.Allowed, nil
}

func eventuallySubjectAccess(ctx context.Context, user, namespace, resource, verb string) func() bool {
	return func() bool {
		allowed, err := subjectAccessAllowed(ctx, user, namespace, "kubernaut.ai", resource, verb)
		return err == nil && allowed
	}
}

func eventuallySubjectDenied(ctx context.Context, user, namespace, resource, verb string) func() bool {
	return func() bool {
		allowed, err := subjectAccessAllowed(ctx, user, namespace, "kubernaut.ai", resource, verb)
		return err == nil && !allowed
	}
}

var _ = Describe("Trusted workflow catalog RBAC (#610)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("IT-RBAC-610-001 [AC-3, AC-6, CM-6]: reconciles catalog Roles and RoleBindings through the namespaced RBAC path", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
		reconcileToDeployPhase(ctx)

		kn := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
		roles, bindings := resources.TrustedWorkflowCatalogRBAC(kn)
		for _, desired := range roles {
			live := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, live)).To(Succeed(),
				"catalog Role %s/%s should be reconciled", desired.Namespace, desired.Name)
			Expect(live.Rules).To(Equal(desired.Rules))
		}
		for _, desired := range bindings {
			live := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, live)).To(Succeed(),
				"catalog RoleBinding %s/%s should be reconciled", desired.Namespace, desired.Name)
			Expect(live.RoleRef).To(Equal(desired.RoleRef))
			Expect(live.Subjects).To(Equal(desired.Subjects))
		}
	})

	It("IT-RBAC-610-003 [AC-3, AC-6]: uses a non-default controller namespace, not the workflow namespace", func() {
		controllerNamespace := "trusted-controller"
		workflowNamespace := "workflow-execution"
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: controllerNamespace}})).To(Succeed())

		kn := newMinimalCR()
		kn.Namespace = controllerNamespace
		kn.UID = types.UID("trusted-controller-test")
		kn.Spec.WorkflowExecution.WorkflowNamespace = workflowNamespace
		r := newReconciler()
		Expect(r.deployNamespacedCoreRBAC(ctx, kn, kn)).To(Succeed())

		roles, bindings := resources.TrustedWorkflowCatalogRBAC(kn)
		for _, role := range roles {
			Expect(role.Namespace).To(Equal(controllerNamespace))
			Expect(role.Namespace).NotTo(Equal(workflowNamespace))
			live := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(role), live)).To(Succeed())
		}
		for _, binding := range bindings {
			Expect(binding.Namespace).To(Equal(controllerNamespace))
			Expect(binding.Subjects[0].Namespace).To(Equal(controllerNamespace))
			Expect(binding.Subjects[0].Namespace).NotTo(Equal(workflowNamespace))
			live := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(binding), live)).To(Succeed())
		}

		rolesInNamespace := &rbacv1.RoleList{}
		Expect(k8sClient.List(ctx, rolesInNamespace, client.InNamespace(controllerNamespace))).To(Succeed())
		for i := range rolesInNamespace.Items {
			Expect(k8sClient.Delete(ctx, &rolesInNamespace.Items[i])).To(Succeed())
		}
		bindingsInNamespace := &rbacv1.RoleBindingList{}
		Expect(k8sClient.List(ctx, bindingsInNamespace, client.InNamespace(controllerNamespace))).To(Succeed())
		for i := range bindingsInNamespace.Items {
			Expect(k8sClient.Delete(ctx, &bindingsInNamespace.Items[i])).To(Succeed())
		}
		namespace := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: controllerNamespace}, namespace)).To(Succeed())
		namespace.Spec.Finalizers = nil
		Expect(k8sClient.Update(ctx, namespace)).To(Succeed())
		Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
	})

	It("IT-RBAC-610-004/005 [AC-3, AC-6, SC-7]: proves effective trusted and foreign authorization, including AuthWebhook's preserved broad grant", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
		reconcileToDeployPhase(ctx)

		trustedNamespace := testNamespace
		foreignNamespace := "foreign-workflow-catalog"
		kaUser := serviceAccountUser(trustedNamespace, resources.ServiceAccountName(resources.ComponentKubernautAgent))
		webhookUser := serviceAccountUser(trustedNamespace, resources.ServiceAccountName(resources.ComponentAuthWebhook))
		workflowExecutionUser := serviceAccountUser(trustedNamespace, resources.ServiceAccountName(resources.ComponentWorkflowExecution))

		Eventually(eventuallySubjectAccess(ctx, kaUser, trustedNamespace, "remediationworkflows", "list"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectAccess(ctx, kaUser, trustedNamespace, "actiontypes", "watch"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectDenied(ctx, kaUser, foreignNamespace, "remediationworkflows", "list"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectDenied(ctx, kaUser, foreignNamespace, "actiontypes", "watch"), timeout, interval).Should(BeTrue())

		Eventually(eventuallySubjectAccess(ctx, workflowExecutionUser, trustedNamespace, "remediationworkflows", "list"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectDenied(ctx, workflowExecutionUser, foreignNamespace, "remediationworkflows", "list"), timeout, interval).Should(BeTrue())

		Eventually(eventuallySubjectAccess(ctx, webhookUser, trustedNamespace, "remediationworkflows", "list"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectAccess(ctx, webhookUser, foreignNamespace, "remediationworkflows", "list"), timeout, interval).Should(BeTrue(),
			"AuthWebhook's preserved ClusterRole intentionally keeps foreign workflow access")
		Eventually(eventuallySubjectAccess(ctx, webhookUser, foreignNamespace, "remediationworkflows/status", "patch"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectAccess(ctx, webhookUser, foreignNamespace, "remediationworkflows/finalizers", "update"), timeout, interval).Should(BeTrue())
	})
})

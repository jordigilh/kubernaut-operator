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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// serviceAccountUser returns the Kubernetes user string for a ServiceAccount.
func serviceAccountUser(namespace, name string) string {
	return fmt.Sprintf("system:serviceaccount:%s:%s", namespace, name)
}

// serviceAccountGroups returns the groups Kubernetes assigns to a ServiceAccount.
func serviceAccountGroups(user string) []string {
	const prefix = "system:serviceaccount:"
	identity, ok := strings.CutPrefix(user, prefix)
	if !ok {
		return nil
	}
	namespace, _, ok := strings.Cut(identity, ":")
	if !ok || namespace == "" {
		return nil
	}
	return []string{
		"system:serviceaccounts",
		"system:serviceaccounts:" + namespace,
		"system:authenticated",
	}
}

// subjectAccessAllowed submits a SubjectAccessReview and returns its decision.
func subjectAccessAllowed(ctx context.Context, user, namespace, group, resource, subresource, verb string) (bool, error) {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return false, fmt.Errorf("creating authorization client: %w", err)
	}
	review := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   user,
			Groups: serviceAccountGroups(user),
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Group:       group,
				Resource:    resource,
				Subresource: subresource,
				Verb:        verb,
				Namespace:   namespace,
			},
		},
	}
	result, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Errorf("creating SubjectAccessReview for %s %s/%s in %s: %w", verb, group, resource, namespace, err)
	}
	return result.Status.Allowed, nil
}

// eventuallySubjectAccess returns an Eventually predicate for an allowed SAR.
func eventuallySubjectAccess(ctx context.Context, user, namespace, resource, subresource, verb string) func() bool {
	return func() bool {
		allowed, err := subjectAccessAllowed(ctx, user, namespace, "kubernaut.ai", resource, subresource, verb)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		return allowed
	}
}

// eventuallySubjectDenied returns an Eventually predicate for a denied SAR.
func eventuallySubjectDenied(ctx context.Context, user, namespace, resource, subresource, verb string) func() bool {
	return func() bool {
		allowed, err := subjectAccessAllowed(ctx, user, namespace, "kubernaut.ai", resource, subresource, verb)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		return !allowed
	}
}

type issue624CatalogFault struct {
	objectType string
	name       string
	enabled    bool
}

type issue624CatalogFaultClient struct {
	client.Client
	fault *issue624CatalogFault
}

const (
	catalogObjectRole        = "role"
	catalogObjectRoleBinding = "rolebinding"
)

// shouldFail reports whether the configured fault targets the object.
func (c *issue624CatalogFaultClient) shouldFail(object client.Object) bool {
	if c.fault == nil || !c.fault.enabled || object.GetName() != c.fault.name {
		return false
	}
	switch c.fault.objectType {
	case catalogObjectRole:
		_, ok := object.(*rbacv1.Role)
		return ok
	case catalogObjectRoleBinding:
		_, ok := object.(*rbacv1.RoleBinding)
		return ok
	default:
		return false
	}
}

// Create injects the configured catalog Create failure before delegating.
func (c *issue624CatalogFaultClient) Create(ctx context.Context, object client.Object, options ...client.CreateOption) error {
	if c.shouldFail(object) {
		return fmt.Errorf("injected issue-624 %s create failure for %s", c.fault.objectType, c.fault.name)
	}
	return c.Client.Create(ctx, object, options...)
}

// Update injects the configured catalog Update failure before delegating.
func (c *issue624CatalogFaultClient) Update(ctx context.Context, object client.Object, options ...client.UpdateOption) error {
	if c.shouldFail(object) {
		return fmt.Errorf("injected issue-624 %s update failure for %s", c.fault.objectType, c.fault.name)
	}
	return c.Client.Update(ctx, object, options...)
}

// issue624CatalogNames returns the desired catalog Role and RoleBinding names.
func issue624CatalogNames(kn *kubernautv1alpha2.Kubernaut) (map[string]struct{}, map[string]struct{}) {
	roles, bindings := resources.TrustedWorkflowCatalogRBAC(kn)
	roleNames := make(map[string]struct{}, len(roles))
	bindingNames := make(map[string]struct{}, len(bindings))
	for _, role := range roles {
		roleNames[role.Name] = struct{}{}
	}
	for _, binding := range bindings {
		bindingNames[binding.Name] = struct{}{}
	}
	return roleNames, bindingNames
}

// ensureIssue624Namespace creates the named fixture namespace when absent.
func ensureIssue624Namespace(ctx context.Context, name string) {
	namespace := &corev1.Namespace{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, namespace)
	if apierrors.IsNotFound(err) {
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})).To(Succeed())
		return
	}
	Expect(err).NotTo(HaveOccurred())
}

// deleteIssue624Namespace removes the named fixture namespace and its accounts.
func deleteIssue624Namespace(ctx context.Context, name string) {
	for _, serviceAccountName := range []string{
		resources.ServiceAccountName(resources.ComponentAIAnalysis),
		resources.ServiceAccountName(resources.ComponentRemediationOrchestrator),
		"issue-624-unrelated",
	} {
		serviceAccount := &corev1.ServiceAccount{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: serviceAccountName, Namespace: name}, serviceAccount)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			Expect(err).NotTo(HaveOccurred(), "getting ServiceAccount %s/%s during cleanup", name, serviceAccountName)
			continue
		}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, serviceAccount))).To(Succeed())
	}

	namespace := &corev1.Namespace{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, namespace); apierrors.IsNotFound(err) {
		return
	} else {
		Expect(err).NotTo(HaveOccurred())
	}
	namespace.Spec.Finalizers = nil
	Expect(k8sClient.Update(ctx, namespace)).To(Succeed())
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, namespace))).To(Succeed())
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: name}, &corev1.Namespace{}))
	}, timeout, interval).Should(BeTrue(), "foreign fixture namespace %s should be deleted", name)
}

// startIssue624FaultedReconcile prepares a CR and injects one catalog failure.
func startIssue624FaultedReconcile(ctx context.Context, objectType, name string) (*KubernautReconciler, *issue624CatalogFault) {
	createBYOSecrets(ctx)
	Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())

	fault := &issue624CatalogFault{objectType: objectType, name: name, enabled: true}
	r := newReconciler()
	r.Client = &issue624CatalogFaultClient{Client: k8sClient, fault: fault}
	request := reconcileRequest()
	_, err := r.Reconcile(ctx, request)
	Expect(err).NotTo(HaveOccurred())
	_, err = r.Reconcile(ctx, request)
	Expect(err).NotTo(HaveOccurred())
	markMigrationJobComplete(ctx)
	return r, fault
}

// reconcileRequest returns the singleton reconciliation request used by tests.
func reconcileRequest() ctrl.Request {
	return ctrl.Request{NamespacedName: singletonKey()}
}

// issue624LegacyKAFixture adds the pre-migration cluster catalog grant.
func issue624LegacyKAFixture(kn *kubernautv1alpha2.Kubernaut) *rbacv1.ClusterRoleBinding {
	var legacyRole *rbacv1.ClusterRole
	for _, role := range resources.ClusterRoles(kn, kn) {
		if role.Name == kn.Namespace+"-kubernaut-agent-investigator" {
			legacyRole = role.DeepCopy()
			break
		}
	}
	Expect(legacyRole).NotTo(BeNil())
	legacyRole.Rules = append(legacyRole.Rules, rbacv1.PolicyRule{
		APIGroups: []string{kubernautv1alpha2.GroupVersion.Group},
		Resources: []string{"remediationworkflows", "actiontypes"},
		Verbs:     []string{"get", "list", "watch"},
	})

	var legacyBinding *rbacv1.ClusterRoleBinding
	for _, binding := range resources.ClusterRoleBindings(kn, kn) {
		if binding.RoleRef.Name == legacyRole.Name {
			legacyBinding = binding.DeepCopy()
			break
		}
	}
	Expect(legacyBinding).NotTo(BeNil())
	resources.StampOwnership(kn, legacyRole)
	resources.StampOwnership(kn, legacyBinding)
	Expect(k8sClient.Create(context.Background(), legacyRole)).To(Succeed())
	Expect(k8sClient.Create(context.Background(), legacyBinding)).To(Succeed())
	return legacyBinding
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

		Eventually(eventuallySubjectAccess(ctx, kaUser, trustedNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectAccess(ctx, kaUser, trustedNamespace, "actiontypes", "", "watch"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectDenied(ctx, kaUser, foreignNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectDenied(ctx, kaUser, foreignNamespace, "actiontypes", "", "watch"), timeout, interval).Should(BeTrue())

		Eventually(eventuallySubjectAccess(ctx, workflowExecutionUser, trustedNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectDenied(ctx, workflowExecutionUser, foreignNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())

		Eventually(eventuallySubjectAccess(ctx, webhookUser, trustedNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectAccess(ctx, webhookUser, foreignNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue(),
			"AuthWebhook's preserved ClusterRole intentionally keeps foreign workflow access")
		Eventually(eventuallySubjectAccess(ctx, webhookUser, foreignNamespace, "remediationworkflows", "status", "patch"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectAccess(ctx, webhookUser, foreignNamespace, "remediationworkflows", "finalizers", "update"), timeout, interval).Should(BeTrue())
	})
})

var _ = Describe("Trusted workflow catalog RBAC (#624)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
		unrelated := &corev1.ServiceAccount{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "issue-624-unrelated", Namespace: testNamespace}, unrelated)
		if apierrors.IsNotFound(err) {
			return
		}
		if err != nil {
			Expect(err).NotTo(HaveOccurred(), "getting ServiceAccount %s/%s during cleanup", testNamespace, "issue-624-unrelated")
			return
		}
		Expect(k8sClient.Delete(ctx, unrelated)).To(Succeed())
	})

	It("IT-RBAC-624-001 [AC-3, AC-6, CM-6]: reconciles six catalog Roles and RoleBindings with exact namespace-scoped subjects and ownership", func() {
		foreignNamespace := "issue-624-foreign"
		ensureIssue624Namespace(ctx, foreignNamespace)
		DeferCleanup(func() { deleteIssue624Namespace(context.Background(), foreignNamespace) })

		for _, serviceAccount := range []*corev1.ServiceAccount{
			{ObjectMeta: metav1.ObjectMeta{Name: resources.ServiceAccountName(resources.ComponentAIAnalysis), Namespace: foreignNamespace}},
			{ObjectMeta: metav1.ObjectMeta{Name: resources.ServiceAccountName(resources.ComponentRemediationOrchestrator), Namespace: foreignNamespace}},
			{ObjectMeta: metav1.ObjectMeta{Name: "issue-624-unrelated", Namespace: testNamespace}},
		} {
			Expect(k8sClient.Create(ctx, serviceAccount)).To(Succeed())
		}

		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
		reconcileToDeployPhase(ctx)

		kn := fetchKnV2(ctx)
		roles, bindings := resources.TrustedWorkflowCatalogRBAC(kn)
		Expect(roles).To(HaveLen(6))
		Expect(bindings).To(HaveLen(6))
		roleNames, bindingNames := issue624CatalogNames(kn)
		Expect(roleNames).To(HaveLen(6))
		Expect(bindingNames).To(HaveLen(6))

		for _, desired := range roles {
			live := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, live)).To(Succeed(),
				"catalog Role %s/%s should be reconciled", desired.Namespace, desired.Name)
			Expect(live.Namespace).To(Equal(kn.Namespace))
			Expect(live.Rules).To(Equal(desired.Rules))
			Expect(live.Labels).To(Equal(resources.CommonLabels(kn)))
			Expect(live.Annotations).To(HaveKeyWithValue(resources.AnnotationOwnerNamespace, kn.Namespace))
			Expect(live.Annotations).To(HaveKeyWithValue(resources.AnnotationOwnerUID, string(kn.UID)))
			Expect(live.Annotations).To(HaveKey(resources.AnnotationSpecHash))
			Expect(live.OwnerReferences).To(HaveLen(1))
			Expect(metav1.GetControllerOf(live)).NotTo(BeNil())
			Expect(metav1.GetControllerOf(live).UID).To(Equal(kn.UID))
		}
		for _, desired := range bindings {
			live := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, live)).To(Succeed(),
				"catalog RoleBinding %s/%s should be reconciled", desired.Namespace, desired.Name)
			Expect(live.Namespace).To(Equal(kn.Namespace))
			Expect(live.RoleRef).To(Equal(desired.RoleRef))
			Expect(live.Subjects).To(Equal(desired.Subjects))
			Expect(live.Subjects).To(HaveLen(1))
			Expect(live.Subjects[0].Kind).To(Equal(rbacv1.ServiceAccountKind))
			Expect(serviceAccountUser(live.Subjects[0].Namespace, live.Subjects[0].Name)).To(Equal(
				serviceAccountUser(kn.Namespace, desired.Subjects[0].Name)))
			Expect(live.Labels).To(Equal(resources.CommonLabels(kn)))
			Expect(live.Annotations).To(HaveKeyWithValue(resources.AnnotationOwnerNamespace, kn.Namespace))
			Expect(live.Annotations).To(HaveKeyWithValue(resources.AnnotationOwnerUID, string(kn.UID)))
			Expect(live.Annotations).To(HaveKey(resources.AnnotationSpecHash))
			Expect(live.OwnerReferences).To(HaveLen(1))
			Expect(metav1.GetControllerOf(live)).NotTo(BeNil())
			Expect(metav1.GetControllerOf(live).UID).To(Equal(kn.UID))
		}

		allRoles := &rbacv1.RoleList{}
		Expect(k8sClient.List(ctx, allRoles, client.InNamespace(kn.Namespace), client.MatchingLabels{"app.kubernetes.io/instance": kn.Name})).To(Succeed())
		catalogRoleCount := 0
		for i := range allRoles.Items {
			if _, ok := roleNames[allRoles.Items[i].Name]; ok {
				catalogRoleCount++
			}
		}
		Expect(catalogRoleCount).To(Equal(6), "all six catalog Roles must be reconciled without duplicate or raw fixture grants")
	})

	It("IT-RBAC-624-002 [AC-3, AC-6, SC-7]: uses correctly formed admin SARs for the asymmetric trusted-catalog boundary", func() {
		foreignNamespace := "issue-624-sar-foreign"
		ensureIssue624Namespace(ctx, foreignNamespace)
		DeferCleanup(func() { deleteIssue624Namespace(context.Background(), foreignNamespace) })
		for _, serviceAccount := range []*corev1.ServiceAccount{
			{ObjectMeta: metav1.ObjectMeta{Name: resources.ServiceAccountName(resources.ComponentAIAnalysis), Namespace: foreignNamespace}},
			{ObjectMeta: metav1.ObjectMeta{Name: resources.ServiceAccountName(resources.ComponentRemediationOrchestrator), Namespace: foreignNamespace}},
			{ObjectMeta: metav1.ObjectMeta{Name: "issue-624-unrelated", Namespace: testNamespace}},
		} {
			Expect(k8sClient.Create(ctx, serviceAccount)).To(Succeed())
		}

		Expect(resources.EnsureCRDs(ctx, cfg)).To(Succeed())
		waitForIssue613SelectorCRD(ctx)
		createIssue613Workflow(ctx, testNamespace, "issue-624-trusted-workflow", "issue-624-trusted")
		createIssue613Workflow(ctx, foreignNamespace, "issue-624-foreign-workflow", "issue-624-foreign")

		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
		reconcileToDeployPhase(ctx)

		trustedNamespace := testNamespace
		aiaUser := serviceAccountUser(trustedNamespace, resources.ServiceAccountName(resources.ComponentAIAnalysis))
		roUser := serviceAccountUser(trustedNamespace, resources.ServiceAccountName(resources.ComponentRemediationOrchestrator))
		unrelatedUser := serviceAccountUser(trustedNamespace, "issue-624-unrelated")
		webhookUser := serviceAccountUser(trustedNamespace, resources.ServiceAccountName(resources.ComponentAuthWebhook))
		checks := []struct {
			user        string
			namespace   string
			resource    string
			subresource string
			verb        string
			allowed     bool
		}{
			{aiaUser, trustedNamespace, "remediationworkflows", "", "list", true},
			{unrelatedUser, trustedNamespace, "remediationworkflows", "", "list", false},
			{aiaUser, trustedNamespace, "remediationworkflows", "", "get", false},
			{aiaUser, trustedNamespace, "remediationworkflows", "", "watch", false},
			{aiaUser, trustedNamespace, "remediationworkflows", "", "create", false},
			{aiaUser, trustedNamespace, "remediationworkflows", "", "update", false},
			{aiaUser, trustedNamespace, "remediationworkflows", "", "patch", false},
			{aiaUser, trustedNamespace, "remediationworkflows", "", "delete", false},
			{aiaUser, trustedNamespace, "remediationworkflows", "status", "update", false},
			{aiaUser, trustedNamespace, "remediationworkflows", "status", "patch", false},
			{aiaUser, trustedNamespace, "remediationworkflows", "finalizers", "update", false},
			{aiaUser, trustedNamespace, "remediationworkflows", "finalizers", "patch", false},
			{roUser, trustedNamespace, "remediationworkflows", "", "get", true},
			{roUser, trustedNamespace, "remediationworkflows", "", "list", true},
			{unrelatedUser, trustedNamespace, "remediationworkflows", "", "get", false},
			{unrelatedUser, trustedNamespace, "remediationworkflows", "", "list", false},
			{roUser, trustedNamespace, "remediationworkflows", "", "watch", false},
			{roUser, trustedNamespace, "remediationworkflows", "", "create", false},
			{roUser, trustedNamespace, "remediationworkflows", "", "update", false},
			{roUser, trustedNamespace, "remediationworkflows", "", "patch", false},
			{roUser, trustedNamespace, "remediationworkflows", "", "delete", false},
			{roUser, trustedNamespace, "remediationworkflows", "status", "update", false},
			{roUser, trustedNamespace, "remediationworkflows", "status", "patch", false},
			{roUser, trustedNamespace, "remediationworkflows", "finalizers", "update", false},
			{roUser, trustedNamespace, "remediationworkflows", "finalizers", "patch", false},
			{aiaUser, foreignNamespace, "remediationworkflows", "", "list", false},
			{roUser, foreignNamespace, "remediationworkflows", "", "get", false},
			{roUser, foreignNamespace, "remediationworkflows", "", "list", false},
			{aiaUser, "", "remediationworkflows", "", "list", false},
			{roUser, "", "remediationworkflows", "", "get", false},
			{roUser, "", "remediationworkflows", "", "list", false},
			{aiaUser, trustedNamespace, "actiontypes", "", "list", false},
			{roUser, trustedNamespace, "actiontypes", "", "list", false},
		}
		for _, check := range checks {
			description := fmt.Sprintf("user=%s namespace=%s resource=%s subresource=%s verb=%s allowed=%t",
				check.user, check.namespace, check.resource, check.subresource, check.verb, check.allowed)
			if check.allowed {
				Eventually(eventuallySubjectAccess(ctx, check.user, check.namespace, check.resource, check.subresource, check.verb), timeout, interval).Should(BeTrue(), description)
			} else {
				Eventually(eventuallySubjectDenied(ctx, check.user, check.namespace, check.resource, check.subresource, check.verb), timeout, interval).Should(BeTrue(), description)
			}
		}
		Eventually(eventuallySubjectAccess(ctx, webhookUser, foreignNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue(),
			"the known broad AuthWebhook binding is a positive SAR control")
	})

	DescribeTable("IT-RBAC-624-003 [AC-3, AC-6, CM-6]: fails closed for each sequential catalog Role/RoleBinding fault and recovers on the same CR",
		func(objectType, component, objectName string, aiaAllowedOnFailure bool) {
			foreignNamespace := "issue-624-failure-foreign-" + component
			ensureIssue624Namespace(ctx, foreignNamespace)
			DeferCleanup(func() { deleteIssue624Namespace(context.Background(), foreignNamespace) })

			r, fault := startIssue624FaultedReconcile(ctx, objectType, objectName)
			kn := fetchKnV2(ctx)
			legacyBinding := issue624LegacyKAFixture(kn)
			_, err := r.Reconcile(ctx, reconcileRequest())
			Expect(err).To(HaveOccurred())

			failed := fetchKnV2(ctx)
			failedRBAC := findCondition(failed.Status.Conditions, kubernautv1alpha2.ConditionRBACProvisioned)
			Expect(failedRBAC).NotTo(BeNil())
			Expect(failedRBAC.Status).To(Equal(metav1.ConditionFalse))
			deployments := &appsv1.DeploymentList{}
			Expect(k8sClient.List(ctx, deployments, client.InNamespace(testNamespace), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})).To(Succeed())
			Expect(deployments.Items).To(BeEmpty(), "RBAC failure must block service workloads")

			kaUser := serviceAccountUser(testNamespace, resources.ServiceAccountName(resources.ComponentKubernautAgent))
			aiaUser := serviceAccountUser(testNamespace, resources.ServiceAccountName(resources.ComponentAIAnalysis))
			roUser := serviceAccountUser(testNamespace, resources.ServiceAccountName(resources.ComponentRemediationOrchestrator))
			Eventually(eventuallySubjectAccess(ctx, kaUser, "", "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue(), "legacy KA catalog access remains the fallback while replacement RBAC is incomplete")
			if aiaAllowedOnFailure {
				Eventually(eventuallySubjectAccess(ctx, aiaUser, testNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())
			} else {
				Eventually(eventuallySubjectDenied(ctx, aiaUser, testNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())
			}
			Eventually(eventuallySubjectDenied(ctx, roUser, testNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())

			liveBinding := &rbacv1.ClusterRoleBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(legacyBinding), liveBinding)).To(Succeed())
			Expect(liveBinding.RoleRef).To(Equal(legacyBinding.RoleRef))
			Expect(liveBinding.Subjects).To(Equal(legacyBinding.Subjects))

			fault.enabled = false
			_, err = r.Reconcile(ctx, reconcileRequest())
			Expect(err).NotTo(HaveOccurred())
			recovered := fetchKnV2(ctx)
			Expect(findCondition(recovered.Status.Conditions, kubernautv1alpha2.ConditionRBACProvisioned).Status).To(Equal(metav1.ConditionTrue))
			Expect(findCondition(recovered.Status.Conditions, kubernautv1alpha2.ConditionServicesDeployed).Status).To(Equal(metav1.ConditionTrue))
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gateway", Namespace: testNamespace}, &appsv1.Deployment{})).NotTo(HaveOccurred())
			Eventually(eventuallySubjectAccess(ctx, aiaUser, testNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())
			Eventually(eventuallySubjectAccess(ctx, roUser, testNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())
		},
		Entry("AIA Role", catalogObjectRole, "aianalysis", testNamespace+"-aianalysis-workflow-catalog", false),
		Entry("AIA RoleBinding", catalogObjectRoleBinding, "aianalysis", testNamespace+"-aianalysis-workflow-catalog-binding", false),
		Entry("RO Role", catalogObjectRole, "remediationorchestrator", testNamespace+"-remediationorchestrator-workflow-catalog", true),
		Entry("RO RoleBinding", catalogObjectRoleBinding, "remediationorchestrator", testNamespace+"-remediationorchestrator-workflow-catalog-binding", true),
	)

	It("IT-RBAC-624-004 [AC-3, AC-6, CM-6]: removes the obsolete KA cluster grant only after a successful same-CR retry", func() {
		foreignNamespace := "issue-624-retry-foreign"
		ensureIssue624Namespace(ctx, foreignNamespace)
		DeferCleanup(func() { deleteIssue624Namespace(context.Background(), foreignNamespace) })
		r, fault := startIssue624FaultedReconcile(ctx, catalogObjectRoleBinding, testNamespace+"-aianalysis-workflow-catalog-binding")
		kn := fetchKnV2(ctx)
		legacyBinding := issue624LegacyKAFixture(kn)
		_, err := r.Reconcile(ctx, reconcileRequest())
		Expect(err).To(HaveOccurred())
		fault.enabled = false
		_, err = r.Reconcile(ctx, reconcileRequest())
		Expect(err).NotTo(HaveOccurred())

		liveRole := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: legacyBinding.RoleRef.Name}, liveRole)).To(Succeed())
		Expect(liveRole.Rules).NotTo(ContainElement(SatisfyAll(
			HaveField("APIGroups", ConsistOf(kubernautv1alpha2.GroupVersion.Group)),
			HaveField("Resources", ConsistOf("remediationworkflows", "actiontypes")),
			HaveField("Verbs", ConsistOf("get", "list", "watch")),
		)), "obsolete KA cluster-wide catalog grant must be removed after replacement succeeds")
		liveBinding := &rbacv1.ClusterRoleBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(legacyBinding), liveBinding)).To(Succeed())
		Expect(liveBinding.RoleRef).To(Equal(legacyBinding.RoleRef))
		Expect(liveBinding.Subjects).To(Equal(legacyBinding.Subjects))

		kaUser := serviceAccountUser(testNamespace, resources.ServiceAccountName(resources.ComponentKubernautAgent))
		Eventually(eventuallySubjectDenied(ctx, kaUser, "", "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectDenied(ctx, kaUser, "", "actiontypes", "", "list"), timeout, interval).Should(BeTrue())
		Eventually(eventuallySubjectAccess(ctx, kaUser, testNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue(), "the namespaced KA grant must remain")
		Eventually(eventuallySubjectDenied(ctx, kaUser, foreignNamespace, "remediationworkflows", "", "list"), timeout, interval).Should(BeTrue())
		Expect(findCondition(fetchKnV2(ctx).Status.Conditions, kubernautv1alpha2.ConditionRBACProvisioned).Status).To(Equal(metav1.ConditionTrue))
		Expect(findCondition(fetchKnV2(ctx).Status.Conditions, kubernautv1alpha2.ConditionServicesDeployed).Status).To(Equal(metav1.ConditionTrue))
	})
})

var _ = Describe("APIFrontend trusted workflow catalog RBAC (#634)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("IT-RBAC-634-001 [AC-3, AC-6, CM-6; SOC2 CC6.1, CC6.6, CC8.1; ASVS v5.0.0-V8.2.1, v5.0.0-V8.3.1] reconciles read-only trusted catalog access for APIFrontend", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
		reconcileToDeployPhase(ctx)

		kn := fetchKnV2(ctx)
		role := &rbacv1.Role{}
		roleName := testNamespace + "-apifrontend-workflow-catalog"
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: testNamespace}, role)).To(Succeed())
		Expect(role.Rules).To(ConsistOf(rbacv1.PolicyRule{
			APIGroups: []string{kubernautv1alpha2.GroupVersion.Group},
			Resources: []string{"remediationworkflows"},
			Verbs:     []string{"get", "list"},
		}))

		binding := &rbacv1.RoleBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: roleName + "-binding", Namespace: testNamespace}, binding)).To(Succeed())
		Expect(binding.RoleRef).To(Equal(rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		}))
		Expect(binding.Subjects).To(ConsistOf(rbacv1.Subject{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      resources.ServiceAccountName(resources.ComponentAPIFrontend),
			Namespace: testNamespace,
		}))
		Expect(kn.Namespace).To(Equal(testNamespace), "trusted catalog access must follow the Kubernaut controller namespace")

		afUser := serviceAccountUser(testNamespace, resources.ServiceAccountName(resources.ComponentAPIFrontend))
		foreignNamespace := "issue-634-foreign"
		checks := []struct {
			namespace   string
			subresource string
			verb        string
			allowed     bool
		}{
			{testNamespace, "", "get", true},
			{testNamespace, "", "list", true},
			{testNamespace, "", "watch", false},
			{testNamespace, "", "create", false},
			{testNamespace, "", "update", false},
			{testNamespace, "", "patch", false},
			{testNamespace, "", "delete", false},
			{testNamespace, "status", "get", false},
			{testNamespace, "status", "update", false},
			{testNamespace, "status", "patch", false},
			{testNamespace, "finalizers", "update", false},
			{foreignNamespace, "", "get", false},
			{foreignNamespace, "", "list", false},
			{"", "", "get", false},
			{"", "", "list", false},
		}
		for _, check := range checks {
			description := fmt.Sprintf("APIFrontend user=%s namespace=%s subresource=%s verb=%s allowed=%t",
				afUser, check.namespace, check.subresource, check.verb, check.allowed)
			if check.allowed {
				Eventually(eventuallySubjectAccess(ctx, afUser, check.namespace, "remediationworkflows", check.subresource, check.verb), timeout, interval).Should(BeTrue(), description)
			} else {
				Eventually(eventuallySubjectDenied(ctx, afUser, check.namespace, "remediationworkflows", check.subresource, check.verb), timeout, interval).Should(BeTrue(), description)
			}
		}
	})

	It("IT-RBAC-634-002 does not reconcile the APIFrontend catalog grant when APIFrontend is disabled", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		disabled := false
		cr.Spec.APIFrontend.Enabled = &disabled
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		reconcileToDeployPhase(ctx)

		roleName, bindingName := resources.APIFrontendWorkflowCatalogRBACNames(fetchKnV2(ctx))
		role := &rbacv1.Role{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: testNamespace}, role)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "disabled APIFrontend must not receive a catalog Role")
		binding := &rbacv1.RoleBinding{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: bindingName, Namespace: testNamespace}, binding)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "disabled APIFrontend must not receive a catalog RoleBinding")
	})

	It("IT-RBAC-634-003 prunes the APIFrontend catalog grant after an enabled-to-disabled transition", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
		r := reconcileToDeployPhase(ctx)

		kn := fetchKnV2(ctx)
		roleName, bindingName := resources.APIFrontendWorkflowCatalogRBACNames(kn)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: testNamespace}, &rbacv1.Role{})).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bindingName, Namespace: testNamespace}, &rbacv1.RoleBinding{})).To(Succeed())

		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		Expect(k8sClient.Update(ctx, kn)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcileRequest())
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: testNamespace}, &rbacv1.Role{})
		}, timeout, interval).Should(MatchError(ContainSubstring("not found")))
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: bindingName, Namespace: testNamespace}, &rbacv1.RoleBinding{})
		}, timeout, interval).Should(MatchError(ContainSubstring("not found")))
	})
})

var _ = Describe("Trusted workflow catalog RBAC failure handling (#610)", Label("unit"), func() {
	It("keeps the legacy KA catalog grant until replacement Role or RoleBinding reconciliation succeeds", func() {
		for _, test := range []struct {
			name       string
			objectType string
			wantError  string
		}{
			{
				name:       "catalog Role",
				objectType: catalogObjectRole,
				wantError:  "ensuring workflow catalog role",
			},
			{
				name:       "catalog RoleBinding",
				objectType: catalogObjectRoleBinding,
				wantError:  "ensuring workflow catalog rolebinding",
			},
		} {
			By("injecting " + test.name + " reconciliation failure")

			kn := newMinimalCR()
			kn.UID = types.UID("unit-610-rbac-upgrade")
			catalogRoles, catalogBindings := resources.TrustedWorkflowCatalogRBAC(kn)
			catalogRoleNames := make(map[string]struct{}, len(catalogRoles))
			catalogBindingNames := make(map[string]struct{}, len(catalogBindings))
			for _, role := range catalogRoles {
				catalogRoleNames[role.Name] = struct{}{}
			}
			for _, binding := range catalogBindings {
				catalogBindingNames[binding.Name] = struct{}{}
			}

			faults := interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.CreateOption) error {
				if test.objectType == catalogObjectRole {
					if _, ok := catalogRoleNames[object.GetName()]; ok {
						if _, ok := object.(*rbacv1.Role); ok {
							return fmt.Errorf("injected catalog Role failure")
						}
					}
				}
				if test.objectType == catalogObjectRoleBinding {
					if _, ok := catalogBindingNames[object.GetName()]; ok {
						if _, ok := object.(*rbacv1.RoleBinding); ok {
							return fmt.Errorf("injected catalog RoleBinding failure")
						}
					}
				}
				return api.Create(ctx, object, options...)
			}}
			r := businessUnitReconciler(kn, faults)

			var legacy *rbacv1.ClusterRole
			for _, role := range resources.ClusterRoles(kn, kn) {
				if role.Name == kn.Namespace+"-kubernaut-agent-investigator" {
					legacy = role.DeepCopy()
					break
				}
			}
			Expect(legacy).NotTo(BeNil())
			legacy.Rules = append(legacy.Rules, rbacv1.PolicyRule{
				APIGroups: []string{kubernautv1alpha2.GroupVersion.Group},
				Resources: []string{"remediationworkflows", "actiontypes"},
				Verbs:     []string{"get", "list", "watch"},
			})
			resources.StampOwnership(kn, legacy)
			Expect(r.Create(ctx, legacy)).To(Succeed())

			Expect(r.deployCoreRBAC(ctx, kn, kn)).To(MatchError(ContainSubstring(test.wantError)))

			live := &rbacv1.ClusterRole{}
			Expect(r.Get(ctx, client.ObjectKeyFromObject(legacy), live)).To(Succeed())
			Expect(live.Rules).To(ContainElement(SatisfyAll(
				HaveField("APIGroups", ConsistOf(kubernautv1alpha2.GroupVersion.Group)),
				HaveField("Resources", ConsistOf("remediationworkflows", "actiontypes")),
				HaveField("Verbs", ConsistOf("get", "list", "watch")),
			)), "the old cluster-wide KA catalog grant must remain while replacement RBAC is unavailable")
		}
	})
})

var _ = Describe("APIFrontend workflow catalog RBAC cleanup (#634)", Label("unit"), func() {
	ctx := context.Background()

	It("UT-RBAC-634-004 routes disabled APIFrontend through namespaced catalog cleanup", func() {
		kn := newMinimalCR()
		kn.UID = types.UID("unit-634-disabled")
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		r := businessUnitReconciler(kn, interceptor.Funcs{})

		Expect(r.deployNamespacedCoreRBAC(ctx, kn, kn)).To(Succeed())
		roleName, bindingName := resources.APIFrontendWorkflowCatalogRBACNames(kn)
		Expect(r.Get(ctx, types.NamespacedName{Name: roleName, Namespace: kn.Namespace}, &rbacv1.Role{})).To(MatchError(ContainSubstring("not found")))
		Expect(r.Get(ctx, types.NamespacedName{Name: bindingName, Namespace: kn.Namespace}, &rbacv1.RoleBinding{})).To(MatchError(ContainSubstring("not found")))

		failingKn := newMinimalCR()
		failingKn.UID = types.UID("unit-634-disabled-failure")
		failingKn.Spec.APIFrontend.Enabled = &disabled
		failingRoleName, _ := resources.APIFrontendWorkflowCatalogRBACNames(failingKn)
		failingRole := &rbacv1.Role{}
		catalogRoles, _ := resources.TrustedWorkflowCatalogRBAC(newMinimalCR())
		for _, desired := range catalogRoles {
			if desired.Name == failingRoleName {
				failingRole = desired.DeepCopy()
				break
			}
		}
		faults := interceptor.Funcs{Delete: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.DeleteOption) error {
			if object.GetName() == failingRoleName {
				return fmt.Errorf("injected stale APIFrontend Role delete failure")
			}
			return api.Delete(ctx, object, options...)
		}}
		failing := businessUnitReconciler(failingKn, faults)
		seedBusinessOwnedResources(failing, failingKn, failingRole)
		Expect(failing.deployNamespacedCoreRBAC(ctx, failingKn, failingKn)).To(MatchError(ContainSubstring("removing stale APIFrontend workflow catalog role")))
	})

	It("UT-RBAC-634-003 removes owned catalog resources and reports delete failures", func() {
		for _, test := range []struct {
			name       string
			objectType string
			wantError  string
		}{
			{name: "successful cleanup"},
			{name: "Role delete failure", objectType: catalogObjectRole, wantError: "removing stale APIFrontend workflow catalog role"},
			{name: "RoleBinding delete failure", objectType: catalogObjectRoleBinding, wantError: "removing stale APIFrontend workflow catalog rolebinding"},
		} {
			By("testing " + test.name)
			kn := newMinimalCR()
			kn.UID = types.UID("unit-634-rbac-cleanup")
			roleName, bindingName := resources.APIFrontendWorkflowCatalogRBACNames(kn)
			faults := interceptor.Funcs{Delete: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.DeleteOption) error {
				if (test.objectType == catalogObjectRole && object.GetName() == roleName) ||
					(test.objectType == catalogObjectRoleBinding && object.GetName() == bindingName) {
					return fmt.Errorf("injected APIFrontend catalog %s delete failure", test.objectType)
				}
				return api.Delete(ctx, object, options...)
			}}
			r := businessUnitReconciler(kn, faults)
			catalogRoles, catalogBindings := resources.TrustedWorkflowCatalogRBAC(kn)
			var catalogRole *rbacv1.Role
			var catalogBinding *rbacv1.RoleBinding
			for _, role := range catalogRoles {
				if role.Name == roleName {
					catalogRole = role.DeepCopy()
				}
			}
			for _, binding := range catalogBindings {
				if binding.Name == bindingName {
					catalogBinding = binding.DeepCopy()
				}
			}
			Expect(catalogRole).NotTo(BeNil())
			Expect(catalogBinding).NotTo(BeNil())
			seedBusinessOwnedResources(r, kn, catalogRole, catalogBinding)

			err := r.pruneStaleAPIFrontendWorkflowCatalogRBAC(ctx, kn)
			if test.wantError == "" {
				Expect(err).NotTo(HaveOccurred())
				Expect(r.Get(ctx, types.NamespacedName{Name: roleName, Namespace: kn.Namespace}, &rbacv1.Role{})).To(MatchError(ContainSubstring("not found")))
				Expect(r.Get(ctx, types.NamespacedName{Name: bindingName, Namespace: kn.Namespace}, &rbacv1.RoleBinding{})).To(MatchError(ContainSubstring("not found")))
				continue
			}
			Expect(err).To(MatchError(ContainSubstring(test.wantError)))
		}
	})
})

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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

const testAwxAPIURL = "https://awx.example.com"

// deleteFailingClient wraps a real client but returns an error on Delete
// calls for ClusterRole objects, simulating a persistent cleanup failure.
type deleteFailingClient struct {
	client.Client
}

func (c *deleteFailingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if _, ok := obj.(*rbacv1.ClusterRole); ok {
		return fmt.Errorf("simulated: permission denied deleting ClusterRole %s", obj.GetName())
	}
	return c.Client.Delete(ctx, obj, opts...)
}

// markMigrationJobComplete creates or patches the migration Job to have a
// JobComplete condition, allowing the reconciler to proceed past phaseMigrate.
func markMigrationJobComplete(ctx context.Context) {
	job := &batchv1.Job{}
	key := types.NamespacedName{Name: "kubernaut-db-migration", Namespace: testNamespace}
	Eventually(func() error {
		return k8sClient.Get(ctx, key, job)
	}, timeout, interval).Should(Succeed(), "migration Job should be created by reconcile")

	now := metav1.Now()
	job.Status.StartTime = &now
	job.Status.CompletionTime = &now
	job.Status.Conditions = append(job.Status.Conditions,
		batchv1.JobCondition{
			Type:   "SuccessCriteriaMet",
			Status: corev1.ConditionTrue,
		},
		batchv1.JobCondition{
			Type:   batchv1.JobComplete,
			Status: corev1.ConditionTrue,
		},
	)
	Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())
}

// markMigrationJobFailed patches the Job to have a JobFailed condition.
func markMigrationJobFailed(ctx context.Context) {
	job := &batchv1.Job{}
	key := types.NamespacedName{Name: "kubernaut-db-migration", Namespace: testNamespace}
	Eventually(func() error {
		return k8sClient.Get(ctx, key, job)
	}, timeout, interval).Should(Succeed())

	now := metav1.Now()
	job.Status.StartTime = &now
	job.Status.Conditions = append(job.Status.Conditions,
		batchv1.JobCondition{
			Type:   "FailureTarget",
			Status: corev1.ConditionTrue,
		},
		batchv1.JobCondition{
			Type:   batchv1.JobFailed,
			Status: corev1.ConditionTrue,
		},
	)
	Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())
}

// setDeploymentReady patches a Deployment status to have ready replicas.
func setDeploymentReady(ctx context.Context, name string) {
	dep := &appsv1.Deployment{}
	key := types.NamespacedName{Name: name, Namespace: testNamespace}
	Eventually(func() error {
		return k8sClient.Get(ctx, key, dep)
	}, timeout, interval).Should(Succeed(), "Deployment %s should exist", name)

	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	dep.Status.ReadyReplicas = desired
	dep.Status.Replicas = desired
	dep.Status.AvailableReplicas = desired
	Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())
}

// setAllDeploymentsReady marks all 10 service Deployments as ready.
func setAllDeploymentsReady(ctx context.Context) {
	for _, c := range resources.AllComponents() {
		setDeploymentReady(ctx, resources.DeploymentName(c))
	}
}

// newCRWithAnsibleEnabled returns a minimal CR with Ansible enabled and a
// tokenSecretRef pointing to the given secret name.
func newCRWithAnsibleEnabled(secretName string) *kubernautv1alpha1.Kubernaut {
	cr := newCRWithRouteDisabled()
	cr.Spec.Ansible.Enabled = true
	cr.Spec.Ansible.APIURL = testAwxAPIURL
	cr.Spec.Ansible.TokenSecretRef = &kubernautv1alpha1.SecretKeyRef{
		Name: secretName,
		Key:  "token",
	}
	return cr
}

// createAnsibleSecret creates a Secret with the given name and key/value pair.
func createAnsibleSecret(ctx context.Context, name, key, value string) { //nolint:unparam // name varies in future tests
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Data:       map[string][]byte{key: []byte(value)},
	}
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())
}

// deleteAnsibleSecret removes the ansible token Secret if it exists.
func deleteAnsibleSecret(ctx context.Context, name string) {
	secret := &corev1.Secret{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, secret)
	if err == nil {
		_ = k8sClient.Delete(ctx, secret)
	}
}

// reconcileToDeployPhase drives a reconciler through finalizer, validate,
// migration, and deploy phases, returning the reconciler for further use.
func reconcileToDeployPhase(ctx context.Context) *KubernautReconciler {
	r := newReconciler()
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
	Expect(err).NotTo(HaveOccurred())
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
	Expect(err).NotTo(HaveOccurred())
	markMigrationJobComplete(ctx)
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
	Expect(err).NotTo(HaveOccurred())
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
	Expect(err).NotTo(HaveOccurred())
	return r
}

// newCRWithRouteDisabled returns a minimal CR with the OCP route disabled
// to avoid needing routev1 in the test scheme.
func newCRWithRouteDisabled() *kubernautv1alpha1.Kubernaut {
	cr := newMinimalCR()
	f := false
	cr.Spec.Gateway.Route.Enabled = &f
	return cr
}

// reconcileToRunning drives a CR through all phases until Running.
// Returns the reconciler for subsequent use.
func reconcileToRunning(ctx context.Context) *KubernautReconciler {
	r := newReconciler()

	By("reconcile 1: add finalizer")
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
	Expect(err).NotTo(HaveOccurred())

	By("reconcile 2: validate + start migration")
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
	Expect(err).NotTo(HaveOccurred())

	By("marking migration Job complete")
	markMigrationJobComplete(ctx)

	By("reconcile 3: complete migration + deploy")
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
	Expect(err).NotTo(HaveOccurred())

	By("reconcile 4: deploy phase (may need additional cycle)")
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
	Expect(err).NotTo(HaveOccurred())

	By("marking all Deployments ready")
	setAllDeploymentsReady(ctx)

	By("reconcile 5: Running phase")
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
	Expect(err).NotTo(HaveOccurred())

	return r
}

// cleanupNamespacedResources removes operator-managed namespaced resources
// that envtest's lack of GC won't handle via owner references.
func cleanupNamespacedResources(ctx context.Context) {
	propagation := metav1.DeletePropagationBackground

	// Migration Job
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-db-migration", Namespace: testNamespace}, job); err == nil {
		_ = k8sClient.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &propagation})
	}

	// Deployments
	depList := &appsv1.DeploymentList{}
	_ = k8sClient.List(ctx, depList, client.InNamespace(testNamespace), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range depList.Items {
		_ = k8sClient.Delete(ctx, &depList.Items[i], &client.DeleteOptions{PropagationPolicy: &propagation})
	}

	// ConfigMaps
	cmList := &corev1.ConfigMapList{}
	_ = k8sClient.List(ctx, cmList, client.InNamespace(testNamespace), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range cmList.Items {
		_ = k8sClient.Delete(ctx, &cmList.Items[i])
	}

	// Secrets (operator-managed, not BYO)
	secretList := &corev1.SecretList{}
	_ = k8sClient.List(ctx, secretList, client.InNamespace(testNamespace), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range secretList.Items {
		_ = k8sClient.Delete(ctx, &secretList.Items[i])
	}

	// ServiceAccounts
	saList := &corev1.ServiceAccountList{}
	_ = k8sClient.List(ctx, saList, client.InNamespace(testNamespace), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range saList.Items {
		_ = k8sClient.Delete(ctx, &saList.Items[i])
	}

	// Services
	svcList := &corev1.ServiceList{}
	_ = k8sClient.List(ctx, svcList, client.InNamespace(testNamespace), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range svcList.Items {
		_ = k8sClient.Delete(ctx, &svcList.Items[i])
	}

	// Roles and RoleBindings
	roleList := &rbacv1.RoleList{}
	_ = k8sClient.List(ctx, roleList, client.InNamespace(testNamespace), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range roleList.Items {
		_ = k8sClient.Delete(ctx, &roleList.Items[i])
	}
	rbList := &rbacv1.RoleBindingList{}
	_ = k8sClient.List(ctx, rbList, client.InNamespace(testNamespace), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range rbList.Items {
		_ = k8sClient.Delete(ctx, &rbList.Items[i])
	}
}

// stripWorkflowNamespaceCreatedByAnnotation removes the created-by annotation so the finalizer
// skips namespace deletion, keeping envtest healthy. envtest lacks a namespace
// controller, so deleting a namespace puts it into Terminating forever.
func stripWorkflowNamespaceCreatedByAnnotation(ctx context.Context) {
	ns := &corev1.Namespace{}
	wfNsName := resources.DefaultWorkflowNamespace
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: wfNsName}, ns); err == nil {
		delete(ns.Annotations, resources.AnnotationCreatedBy)
		Expect(k8sClient.Update(ctx, ns)).To(Succeed())
	}
}

// cleanupClusterScoped removes all operator-managed cluster-scoped resources.
// Note: we intentionally skip deleting the workflow namespace because envtest
// lacks a namespace controller and namespace deletion leaves the namespace stuck
// in Terminating, blocking subsequent tests from creating resources in it.
func cleanupClusterScoped(ctx context.Context) {
	crList := &rbacv1.ClusterRoleList{}
	_ = k8sClient.List(ctx, crList, client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range crList.Items {
		_ = k8sClient.Delete(ctx, &crList.Items[i])
	}
	crbList := &rbacv1.ClusterRoleBindingList{}
	_ = k8sClient.List(ctx, crbList, client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range crbList.Items {
		_ = k8sClient.Delete(ctx, &crbList.Items[i])
	}

	// Clean up resources *inside* the workflow namespace without deleting the namespace itself.
	wfNsName := resources.DefaultWorkflowNamespace
	wfSAList := &corev1.ServiceAccountList{}
	_ = k8sClient.List(ctx, wfSAList, client.InNamespace(wfNsName), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range wfSAList.Items {
		_ = k8sClient.Delete(ctx, &wfSAList.Items[i])
	}
	wfRoleList := &rbacv1.RoleList{}
	_ = k8sClient.List(ctx, wfRoleList, client.InNamespace(wfNsName), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range wfRoleList.Items {
		_ = k8sClient.Delete(ctx, &wfRoleList.Items[i])
	}
	wfRBList := &rbacv1.RoleBindingList{}
	_ = k8sClient.List(ctx, wfRBList, client.InNamespace(wfNsName), client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"})
	for i := range wfRBList.Items {
		_ = k8sClient.Delete(ctx, &wfRBList.Items[i])
	}
}

var _ = Describe("Kubernaut Lifecycle", func() {

	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	// ======================================================================
	// 1. Phase Progression
	// ======================================================================

	Context("Phase Progression", func() {
		It("should transition from Validating through Migrating to Deploying", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			r := newReconciler()

			By("first reconcile: adds finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("second reconcile: validates + starts migration")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))

			By("marking migration Job complete")
			markMigrationJobComplete(ctx)

			By("third reconcile: migration done + deploy starts")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			migCond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionMigrationComplete)
			Expect(migCond).NotTo(BeNil())
			Expect(migCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should reach Running phase when all Deployments are ready", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseRunning))

			deployCond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionServicesDeployed)
			Expect(deployCond).NotTo(BeNil())
			Expect(deployCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should set per-service status for all 10 components", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Services).To(HaveLen(10))

			for _, svc := range kn.Status.Services {
				Expect(svc.Ready).To(BeTrue(), "service %q should be ready", svc.Name)
				Expect(svc.ReadyReplicas).To(BeNumerically(">=", int32(1)))
			}
		})

		It("should requeue after 60s when Running", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically("==", 60_000_000_000),
				"should requeue after 60s when healthy")
		})
	})

	// ======================================================================
	// 2. Degraded State
	// ======================================================================

	Context("Degraded State", func() {
		It("should report Degraded when a Deployment has insufficient replicas", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToDeployPhase(ctx)

			By("marking all except gateway as ready")
			for _, c := range resources.AllComponents() {
				if c != resources.ComponentGateway {
					setDeploymentReady(ctx, resources.DeploymentName(c))
				}
			}

			By("reconciling to check readiness")
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically("==", 15_000_000_000),
				"should requeue after 15s when degraded")

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseDegraded))

			for _, svc := range kn.Status.Services {
				if svc.Name == resources.ComponentGateway {
					Expect(svc.Ready).To(BeFalse())
				}
			}
		})

		It("should recover from Degraded to Running when Deployments become ready", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToDeployPhase(ctx)

			By("driving to degraded")
			for _, c := range resources.AllComponents() {
				if c != resources.ComponentGateway {
					setDeploymentReady(ctx, resources.DeploymentName(c))
				}
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseDegraded))

			By("marking the gateway as ready")
			setDeploymentReady(ctx, "gateway")

			By("reconciling - should recover to Running")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseRunning))
		})
	})

	// ======================================================================
	// 3. Migration Job Lifecycle
	// ======================================================================

	Context("Migration Job Lifecycle", func() {
		It("should wait for migration Job to complete with requeue", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("reconciling after validate - Job created, not complete yet")
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically("==", 10_000_000_000),
				"should requeue after 10s while migration is in progress")

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseMigrating))

			migCond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionMigrationComplete)
			Expect(migCond).NotTo(BeNil())
			Expect(migCond.Reason).To(Equal("MigrationInProgress"))
		})

		It("should delete a failed Job and requeue for retry", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("marking Job as failed")
			markMigrationJobFailed(ctx)

			By("reconciling - should detect failure and delete Job")
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			migCond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionMigrationComplete)
			Expect(migCond).NotTo(BeNil())
			Expect(migCond.Reason).To(Equal("MigrationFailed"))
		})

		It("should not duplicate the Job if it already exists", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("getting the existing Job UID")
			job := &batchv1.Job{}
			key := types.NamespacedName{Name: "kubernaut-db-migration", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, key, job)).To(Succeed())
			origUID := job.UID

			By("reconciling again - Job should not be recreated")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, key, job)).To(Succeed())
			Expect(job.UID).To(Equal(origUID), "Job should not be recreated")
		})

		It("should skip migration when a completed Job with matching spec-hash exists", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			By("capturing the completed Job UID and resourceVersion")
			job := &batchv1.Job{}
			key := types.NamespacedName{Name: "kubernaut-db-migration", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, key, job)).To(Succeed())
			origUID := job.UID
			origRV := job.ResourceVersion

			Expect(job.Annotations).To(HaveKey(resources.AnnotationSpecHash),
				"completed Job should carry a spec-hash annotation")

			By("reconciling again (simulates operator restart)")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the Job was not deleted/recreated")
			Expect(k8sClient.Get(ctx, key, job)).To(Succeed())
			Expect(job.UID).To(Equal(origUID), "Job UID should be unchanged")
			Expect(job.ResourceVersion).To(Equal(origRV), "Job should not have been updated")
		})
	})

	// ======================================================================
	// 4. Feature Toggle Conditional Resources
	// ======================================================================

	Context("Feature Toggle Conditional Resources", func() {
		It("should create monitoring RBAC when monitoring is enabled (default)", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			crList := &rbacv1.ClusterRoleList{}
			Expect(k8sClient.List(ctx, crList, client.MatchingLabels{
				"app.kubernetes.io/managed-by": "kubernaut-operator",
			})).To(Succeed())

			names := make(map[string]bool)
			for _, cr := range crList.Items {
				names[cr.Name] = true
			}
			Expect(names).To(HaveKey(testNamespace+"-alertmanager-view"),
				"monitoring ClusterRole should exist when monitoring is enabled")
			Expect(names).To(HaveKey(testNamespace + "-gateway-signal-source"))
		})

		It("should delete monitoring RBAC and service-CA CMs when monitoring is disabled", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			r := reconcileToRunning(ctx)

			By("verifying monitoring CRs exist")
			monCR := &rbacv1.ClusterRole{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-alertmanager-view"}, monCR)).To(Succeed())

			By("verifying service-CA CMs exist")
			emCA := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "effectivenessmonitor-service-ca", Namespace: testNamespace,
			}, emCA)).To(Succeed())
			kaCA := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "kubernaut-agent-service-ca", Namespace: testNamespace,
			}, kaCA)).To(Succeed())

			By("disabling monitoring")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			f := false
			kn.Spec.Monitoring.Enabled = &f
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			By("reconciling after toggle")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-alertmanager-view"}, monCR)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "monitoring ClusterRole should be deleted")

			By("verifying service-CA CMs are cleaned up")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "effectivenessmonitor-service-ca", Namespace: testNamespace,
			}, emCA)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"effectivenessmonitor-service-ca should be deleted when monitoring is disabled")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "kubernaut-agent-service-ca", Namespace: testNamespace,
			}, kaCA)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"kubernaut-agent-service-ca should be deleted when monitoring is disabled")
		})

		It("should create AWX RBAC when ansible is enabled", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			cr.Spec.Ansible.Enabled = true
			cr.Spec.Ansible.APIURL = testAwxAPIURL
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			reconcileToRunning(ctx)

			awxCR := &rbacv1.ClusterRole{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-workflowexecution-awx"}, awxCR)).To(Succeed())
		})

		It("should delete AWX RBAC when ansible is disabled", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			cr.Spec.Ansible.Enabled = true
			cr.Spec.Ansible.APIURL = testAwxAPIURL
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			r := reconcileToRunning(ctx)

			By("disabling ansible")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			kn.Spec.Ansible.Enabled = false
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			awxCR := &rbacv1.ClusterRole{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-workflowexecution-awx"}, awxCR)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "AWX ClusterRole should be deleted")
		})

		It("should not generate SDK ConfigMap when sdkConfigMapName is set", func() {
			cm, err := resources.KubernautAgentSDKConfigMap(newMinimalCR())
			Expect(err).NotTo(HaveOccurred())
			Expect(cm).NotTo(BeNil(), "should generate default SDK ConfigMap")

			crWithSDK := newMinimalCR()
			crWithSDK.Spec.KubernautAgent.LLM.SdkConfigMapName = "user-managed-sdk"
			cm, err = resources.KubernautAgentSDKConfigMap(crWithSDK)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm).To(BeNil(), "should not generate SDK ConfigMap when user provides one")
		})
	})

	// ======================================================================
	// 5. Spec Update Propagation
	// ======================================================================

	Context("Spec Update Propagation", func() {
		It("should update ConfigMap data when spec changes", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			By("checking initial timeout value")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			Expect(cm.Data["remediationorchestrator.yaml"]).To(ContainSubstring("global: 1h"))

			By("updating the spec")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			kn.Spec.RemediationOrchestrator.Timeouts.Global = "2h"
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			By("reconciling to propagate")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			Expect(cm.Data["remediationorchestrator.yaml"]).To(ContainSubstring("global: 2h"))
		})

		It("should update Deployment image when spec.image.tag changes", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			By("verifying initial image tag")
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gateway", Namespace: testNamespace,
			}, dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring(":test"))

			By("setting an image override for gateway")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			kn.Spec.Image.Overrides = map[string]string{
				"gateway": "myregistry.example.com/gateway:v2.0.0",
			}
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			By("reconciling to propagate")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gateway", Namespace: testNamespace,
			}, dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("myregistry.example.com/gateway:v2.0.0"))
		})

		It("should set owner references on namespaced resources", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gateway", Namespace: testNamespace,
			}, dep)).To(Succeed())

			Expect(dep.OwnerReferences).NotTo(BeEmpty())
			Expect(dep.OwnerReferences[0].UID).To(Equal(kn.UID))
			Expect(dep.OwnerReferences[0].Kind).To(Equal("Kubernaut"))
		})

		It("should apply custom resource limits from spec", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			cr.Spec.Gateway.Resources = corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			reconcileToRunning(ctx)

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gateway", Namespace: testNamespace,
			}, dep)).To(Succeed())

			limit := dep.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory]
			Expect(limit.String()).To(Equal("512Mi"))
		})
	})

	// ======================================================================
	// 6. Deletion / Finalizer Edge Cases
	// ======================================================================

	Context("Deletion Edge Cases", func() {
		It("should skip deletion of workflow namespace not managed by operator", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			By("removing created-by annotation to simulate user-managed namespace")
			stripWorkflowNamespaceCreatedByAnnotation(ctx)

			By("deleting the CR")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(k8sClient.Delete(ctx, kn)).To(Succeed())

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the namespace is NOT being deleted")
			existingNs := &corev1.Namespace{}
			wfNsName := resources.DefaultWorkflowNamespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wfNsName}, existingNs)).To(Succeed())
			Expect(existingNs.DeletionTimestamp).To(BeNil(),
				"namespace without created-by annotation should not be deleted")
		})

		It("should clean up all cluster-scoped RBAC on deletion", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			By("verifying cluster-scoped resources exist")
			crList := &rbacv1.ClusterRoleList{}
			Expect(k8sClient.List(ctx, crList, client.MatchingLabels{
				"app.kubernetes.io/managed-by": "kubernaut-operator",
			})).To(Succeed())
			Expect(crList.Items).NotTo(BeEmpty())

			By("preventing namespace deletion for envtest stability")
			stripWorkflowNamespaceCreatedByAnnotation(ctx)

			By("deleting the CR")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(k8sClient.Delete(ctx, kn)).To(Succeed())

			By("reconciling the deletion")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("verifying ClusterRoles are cleaned up")
			Expect(k8sClient.List(ctx, crList, client.MatchingLabels{
				"app.kubernetes.io/managed-by": "kubernaut-operator",
			})).To(Succeed())
			Expect(crList.Items).To(BeEmpty(), "all operator-managed ClusterRoles should be deleted")

			By("verifying CRBs are cleaned up")
			crbList := &rbacv1.ClusterRoleBindingList{}
			Expect(k8sClient.List(ctx, crbList, client.MatchingLabels{
				"app.kubernetes.io/managed-by": "kubernaut-operator",
			})).To(Succeed())
			Expect(crbList.Items).To(BeEmpty(), "all operator-managed CRBs should be deleted")
		})

		It("should always attempt monitoring cleanup even when monitoring is disabled", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			By("verifying monitoring CRBs exist")
			monCRBName := testNamespace + "-effectivenessmonitor-alertmanager-view-binding"
			crb := &rbacv1.ClusterRoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: monCRBName}, crb)).To(Succeed())

			By("disabling monitoring and deleting CR")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			f := false
			kn.Spec.Monitoring.Enabled = &f
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			stripWorkflowNamespaceCreatedByAnnotation(ctx)
			Expect(k8sClient.Delete(ctx, kn)).To(Succeed())

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: monCRBName}, crb)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"monitoring CRB should be cleaned up even when monitoring is disabled")
		})

		It("should always attempt AWX cleanup even when ansible is disabled", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			cr.Spec.Ansible.Enabled = true
			cr.Spec.Ansible.APIURL = testAwxAPIURL
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			r := reconcileToRunning(ctx)

			awxName := testNamespace + "-workflowexecution-awx"
			awxCR := &rbacv1.ClusterRole{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: awxName}, awxCR)).To(Succeed())

			By("disabling ansible and deleting CR")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			kn.Spec.Ansible.Enabled = false
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			stripWorkflowNamespaceCreatedByAnnotation(ctx)
			Expect(k8sClient.Delete(ctx, kn)).To(Succeed())

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: awxName}, awxCR)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"AWX ClusterRole should be cleaned up even when ansible is disabled")
		})
	})

	// ======================================================================
	// 7. Concurrent / Conflict Handling
	// ======================================================================

	Context("Concurrent Operations", func() {
		It("should handle status patch safely under concurrent spec changes", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("modifying spec between reconcile cycles (simulating concurrent edit)")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			kn.Spec.RemediationOrchestrator.Timeouts.Global = "3h"
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			By("reconciling - status patch should not conflict with spec change")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	// ======================================================================
	// 8. Singleton Enforcement
	// ======================================================================

	Context("Singleton Enforcement", func() {
		It("should not create any resources for a non-singleton CR name", func() {
			createBYOSecrets(ctx)
			badCR := newMinimalCR()
			badCR.Name = "not-kubernaut-2"
			Expect(k8sClient.Create(ctx, badCR)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, badCR) }()

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "not-kubernaut-2", Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			result := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "not-kubernaut-2", Namespace: testNamespace}, result)).To(Succeed())
			Expect(result.Finalizers).To(BeEmpty())
			Expect(result.Status.Phase).To(BeEmpty())
		})

		It("should use namespace-prefixed names for cluster-scoped resources", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			cr := &rbacv1.ClusterRole{}
			prefixedName := testNamespace + "-gateway-role"
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: prefixedName}, cr)).To(Succeed())

			unprefixedName := "gateway-role"
			err := k8sClient.Get(ctx, types.NamespacedName{Name: unprefixedName}, cr)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"non-prefixed ClusterRole should not exist")
		})
	})

	// ======================================================================
	// 9. CRD Installation Idempotency
	// ======================================================================

	Context("CRD Installation", func() {
		It("should create workload CRDs during migration phase", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			crdCond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionCRDsInstalled)
			Expect(crdCond).NotTo(BeNil(), "ConditionCRDsInstalled should be set")
			Expect(crdCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should be idempotent on repeated EnsureCRDs calls", func() {
			err := resources.EnsureCRDs(ctx, cfg)
			Expect(err).NotTo(HaveOccurred())

			err = resources.EnsureCRDs(ctx, cfg)
			Expect(err).NotTo(HaveOccurred(), "second call should be idempotent")
		})
	})

	// ======================================================================
	// 10. Resource Builder Edge Cases
	// ======================================================================

	Context("Resource Builder Edge Cases", func() {
		It("should use default PostgreSQL port 5432 when not specified", func() {
			cr := newMinimalCR()
			cr.Spec.PostgreSQL.Port = 0

			dep, err := resources.DataStorageDeployment(cr)
			Expect(err).NotTo(HaveOccurred())

			initEnv := dep.Spec.Template.Spec.InitContainers[0].Env
			var found bool
			for _, e := range initEnv {
				if e.Name == "PGPORT" {
					Expect(e.Value).To(Equal("5432"))
					found = true
				}
			}
			Expect(found).To(BeTrue(), "PGPORT env var should be set")
		})

		It("should use custom PostgreSQL port when specified", func() {
			cr := newMinimalCR()
			cr.Spec.PostgreSQL.Port = 5433

			dep, err := resources.DataStorageDeployment(cr)
			Expect(err).NotTo(HaveOccurred())

			initEnv := dep.Spec.Template.Spec.InitContainers[0].Env
			var found bool
			for _, e := range initEnv {
				if e.Name == "PGPORT" {
					Expect(e.Value).To(Equal("5433"))
					found = true
				}
			}
			Expect(found).To(BeTrue(), "PGPORT env var should be set")
		})

		It("should use image override when set", func() {
			cr := newMinimalCR()
			cr.Spec.Image.Overrides = map[string]string{
				"gateway": "custom.io/gw:v2@sha256:abc123",
			}

			dep, err := resources.GatewayDeployment(cr)
			Expect(err).NotTo(HaveOccurred())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("custom.io/gw:v2@sha256:abc123"))
		})

		It("should propagate pull secrets to Deployments", func() {
			cr := newMinimalCR()
			cr.Spec.Image.PullSecrets = []corev1.LocalObjectReference{
				{Name: "my-pull-secret"},
			}

			dep, err := resources.GatewayDeployment(cr)
			Expect(err).NotTo(HaveOccurred())
			Expect(dep.Spec.Template.Spec.ImagePullSecrets).To(HaveLen(1))
			Expect(dep.Spec.Template.Spec.ImagePullSecrets[0].Name).To(Equal("my-pull-secret"))
		})

		It("should use custom workflow namespace for RBAC resources", func() {
			cr := newMinimalCR()
			cr.Spec.WorkflowExecution.WorkflowNamespace = "custom-wf"

			roles, rbs := resources.WorkflowNamespaceRBAC(cr)
			for _, r := range roles {
				Expect(r.Namespace).To(Equal("custom-wf"))
			}
			for _, rb := range rbs {
				Expect(rb.Namespace).To(Equal("custom-wf"))
			}

			crbs := resources.ClusterRoleBindings(cr)
			found := false
			for _, crb := range crbs {
				if crb.Name == cr.Namespace+"-workflow-runner-binding" {
					found = true
					Expect(crb.Subjects[0].Namespace).To(Equal("custom-wf"))
				}
			}
			Expect(found).To(BeTrue(), "should find workflow-runner CRB")
		})

		It("should produce valid resources with minimal required fields", func() {
			cr := newMinimalCR()

			for _, build := range []func(*kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error){
				resources.GatewayDeployment,
				resources.DataStorageDeployment,
				resources.AIAnalysisDeployment,
				resources.SignalProcessingDeployment,
				resources.RemediationOrchestratorDeployment,
				resources.WorkflowExecutionDeployment,
				resources.EffectivenessMonitorDeployment,
				resources.NotificationDeployment,
				resources.KubernautAgentDeployment,
				resources.AuthWebhookDeployment,
			} {
				dep, err := build(cr)
				Expect(err).NotTo(HaveOccurred())
				Expect(dep.Name).NotTo(BeEmpty())
				Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty())
				Expect(dep.Spec.Template.Spec.Containers[0].Image).NotTo(BeEmpty())
			}
		})

		It("should resolve image from RELATED_IMAGE env var", func() {
			cr := newMinimalCR()
			dep, err := resources.GatewayDeployment(cr)
			Expect(err).NotTo(HaveOccurred())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("quay.io/kubernaut-ai/gateway:test"))
		})

		It("should use default Valkey port 6379 when not specified", func() {
			spec := &kubernautv1alpha1.ValkeySpec{Host: "valkey", Port: 0}
			addr := resources.ValkeyAddr(spec)
			Expect(addr).To(Equal("valkey:6379"))
		})

		It("should use custom Valkey port when specified", func() {
			spec := &kubernautv1alpha1.ValkeySpec{Host: "valkey", Port: 6380}
			addr := resources.ValkeyAddr(spec)
			Expect(addr).To(Equal("valkey:6380"))
		})
	})

	// ======================================================================
	// 11. Monitoring + Default Policy Coverage Gaps
	// ======================================================================

	Context("Monitoring and Default Policy Resources", func() {
		It("should create service-CA ConfigMaps and default policy CMs when user omits policy names", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			cr.Spec.AIAnalysis.Policy.ConfigMapName = ""
			cr.Spec.SignalProcessing.Policy.ConfigMapName = ""
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			reconcileToRunning(ctx)

			By("verifying effectivenessmonitor-service-ca exists with inject-cabundle annotation")
			emCA := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "effectivenessmonitor-service-ca", Namespace: testNamespace,
			}, emCA)).To(Succeed())
			Expect(emCA.Annotations).To(HaveKeyWithValue(
				"service.beta.openshift.io/inject-cabundle", "true"))

			By("verifying kubernaut-agent-service-ca exists with inject-cabundle annotation")
			kaCA := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "kubernaut-agent-service-ca", Namespace: testNamespace,
			}, kaCA)).To(Succeed())
			Expect(kaCA.Annotations).To(HaveKeyWithValue(
				"service.beta.openshift.io/inject-cabundle", "true"))

			By("verifying aianalysis-policies CM exists with approval.rego key")
			aiPolicyCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "aianalysis-policies", Namespace: testNamespace,
			}, aiPolicyCM)).To(Succeed())
			Expect(aiPolicyCM.Data).To(HaveKey("approval.rego"))

			By("verifying signalprocessing-policy CM exists with policy.rego key")
			spPolicyCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "signalprocessing-policy", Namespace: testNamespace,
			}, spPolicyCM)).To(Succeed())
			Expect(spPolicyCM.Data).To(HaveKey("policy.rego"))

			By("verifying signalprocessing-proactive-signal-mappings CM exists with proactive-signal-mappings.yaml key")
			proactiveCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "signalprocessing-proactive-signal-mappings", Namespace: testNamespace,
			}, proactiveCM)).To(Succeed())
			Expect(proactiveCM.Data).To(HaveKey("proactive-signal-mappings.yaml"))
		})
	})

	// ======================================================================
	// 12. Kubernaut Agent Client RoleBinding Provisioning
	// ======================================================================

	Context("Kubernaut Agent Client RoleBinding", func() {
		It("should create kubernaut-agent-client-aianalysis RoleBinding during deploy", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "kubernaut-agent-client-aianalysis", Namespace: testNamespace,
			}, rb)).To(Succeed())

			Expect(rb.RoleRef.Kind).To(Equal("ClusterRole"))
			expectedRoleRef := testNamespace + "-kubernaut-agent-client"
			Expect(rb.RoleRef.Name).To(Equal(expectedRoleRef))

			Expect(rb.Subjects).NotTo(BeEmpty())
			Expect(rb.Subjects[0].Name).To(Equal(resources.ServiceAccountName(resources.ComponentAIAnalysis)))
			Expect(rb.Subjects[0].Namespace).To(Equal(testNamespace))
		})
	})

	// ======================================================================
	// 13. Spec-Hash Reconcile Optimization
	// ======================================================================

	Context("Spec-Hash Reconcile Optimization", func() {
		It("should stamp spec-hash annotation on created resources", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			Expect(cm.Annotations).To(HaveKey(resources.AnnotationSpecHash),
				"ConfigMap should have spec-hash annotation after creation")
			Expect(cm.Annotations[resources.AnnotationSpecHash]).NotTo(BeEmpty())
		})

		It("should skip update when hash matches (no drift)", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			origRV := cm.ResourceVersion

			By("reconciling again without spec changes")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			Expect(cm.ResourceVersion).To(Equal(origRV),
				"ResourceVersion should not change when there is no drift")
		})

		It("should detect and correct out-of-band ConfigMap edits", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			origHash := cm.Annotations[resources.AnnotationSpecHash]

			By("simulating an out-of-band edit")
			cm.Data["remediationorchestrator.yaml"] = "tampered: true"
			cm.Annotations[resources.AnnotationSpecHash] = "stale-hash"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			By("reconciling to detect and correct the drift")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			Expect(cm.Data["remediationorchestrator.yaml"]).To(ContainSubstring("global: 1h"),
				"ConfigMap should be restored to the desired state")
			Expect(cm.Annotations[resources.AnnotationSpecHash]).To(Equal(origHash),
				"hash should be restored to the original value")
		})

		It("should update hash when CR spec changes", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			oldHash := cm.Annotations[resources.AnnotationSpecHash]

			By("changing the CR spec")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			kn.Spec.RemediationOrchestrator.Timeouts.Global = "5h"
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			By("reconciling to propagate the change")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			Expect(cm.Data["remediationorchestrator.yaml"]).To(ContainSubstring("global: 5h"))
			Expect(cm.Annotations[resources.AnnotationSpecHash]).NotTo(Equal(oldHash),
				"hash should change when spec changes")
		})
	})

	// ======================================================================
	// 14. TLS Profile Injection and ConfigMap-Hash Rollout
	// ======================================================================

	Context("TLS Profile Injection and ConfigMap-Hash Rollout", func() {
		It("should stamp configmap-hash annotation on Deployment pod templates", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gateway", Namespace: testNamespace,
			}, dep)).To(Succeed())

			podAnnotations := dep.Spec.Template.Annotations
			Expect(podAnnotations).To(HaveKey(resources.AnnotationConfigMapHash),
				"Deployment pod template should have configmap-hash annotation")
			Expect(podAnnotations[resources.AnnotationConfigMapHash]).NotTo(BeEmpty())
		})

		It("should update configmap-hash when a ConfigMap-affecting spec field changes", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-controller", Namespace: testNamespace,
			}, dep)).To(Succeed())
			oldHash := dep.Spec.Template.Annotations[resources.AnnotationConfigMapHash]
			Expect(oldHash).NotTo(BeEmpty())

			By("changing a spec field that alters the RO ConfigMap")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			kn.Spec.RemediationOrchestrator.Timeouts.Global = "99h"
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			By("reconciling to propagate")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "remediationorchestrator-controller", Namespace: testNamespace,
			}, dep)).To(Succeed())
			newHash := dep.Spec.Template.Annotations[resources.AnnotationConfigMapHash]
			Expect(newHash).NotTo(Equal(oldHash),
				"configmap-hash should change when the underlying ConfigMap content changes")
		})

		It("should gracefully skip TLS profile on non-OCP clusters (no tlsProfile in ConfigMaps)", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gateway-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("tlsProfile"),
				"tlsProfile should be omitted when the APIServer CR is not available")
		})
	})

	// ======================================================================
	// 15. Deletion Cleanup Completeness
	// ======================================================================

	Context("Deletion Cleanup Completeness", func() {
		It("should clean up all resource categories on deletion including webhooks and workflow namespace resources", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			ns := testNamespace

			By("verifying webhook configurations exist")
			mwcName := ns + "-authwebhook-mutating"
			vwcName := ns + "-authwebhook-validating"
			preMWC := &admissionregistrationv1.MutatingWebhookConfiguration{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mwcName}, preMWC)).To(Succeed(),
				"MutatingWebhookConfiguration should exist before deletion")
			preVWC := &admissionregistrationv1.ValidatingWebhookConfiguration{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vwcName}, preVWC)).To(Succeed(),
				"ValidatingWebhookConfiguration should exist before deletion")

			By("verifying workflow namespace resources exist")
			wfNsName := resources.DefaultWorkflowNamespace
			wfSAList := &corev1.ServiceAccountList{}
			Expect(k8sClient.List(ctx, wfSAList,
				client.InNamespace(wfNsName),
				client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"},
			)).To(Succeed())
			Expect(wfSAList.Items).NotTo(BeEmpty(), "workflow runner SA should exist before deletion")

			wfRoleList := &rbacv1.RoleList{}
			Expect(k8sClient.List(ctx, wfRoleList,
				client.InNamespace(wfNsName),
				client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"},
			)).To(Succeed())
			Expect(wfRoleList.Items).NotTo(BeEmpty(), "workflow roles should exist before deletion")

			By("preventing namespace deletion for envtest stability")
			stripWorkflowNamespaceCreatedByAnnotation(ctx)

			By("deleting the CR")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(k8sClient.Delete(ctx, kn)).To(Succeed())

			By("reconciling the deletion")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("verifying ClusterRoles are cleaned up")
			crList := &rbacv1.ClusterRoleList{}
			Expect(k8sClient.List(ctx, crList, client.MatchingLabels{
				"app.kubernetes.io/managed-by": "kubernaut-operator",
			})).To(Succeed())
			Expect(crList.Items).To(BeEmpty(), "all operator-managed ClusterRoles should be deleted")

			By("verifying CRBs are cleaned up")
			crbList := &rbacv1.ClusterRoleBindingList{}
			Expect(k8sClient.List(ctx, crbList, client.MatchingLabels{
				"app.kubernetes.io/managed-by": "kubernaut-operator",
			})).To(Succeed())
			Expect(crbList.Items).To(BeEmpty(), "all operator-managed CRBs should be deleted")

			By("verifying MutatingWebhookConfiguration is cleaned up")
			mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: mwcName}, mwc)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"MutatingWebhookConfiguration should be deleted")

			By("verifying ValidatingWebhookConfiguration is cleaned up")
			vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: vwcName}, vwc)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"ValidatingWebhookConfiguration should be deleted")

			By("verifying workflow namespace roles are cleaned up")
			Expect(k8sClient.List(ctx, wfRoleList,
				client.InNamespace(wfNsName),
				client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"},
			)).To(Succeed())
			Expect(wfRoleList.Items).To(BeEmpty(),
				"workflow namespace roles should be deleted")

			By("verifying workflow namespace SAs are cleaned up")
			Expect(k8sClient.List(ctx, wfSAList,
				client.InNamespace(wfNsName),
				client.MatchingLabels{"app.kubernetes.io/managed-by": "kubernaut-operator"},
			)).To(Succeed())
			Expect(wfSAList.Items).To(BeEmpty(),
				"workflow namespace SAs should be deleted")
		})
	})

	// ======================================================================
	// 16. ConfigMap Drift Detection (Issue #16, TDD Red — Phase 1)
	// ======================================================================

	Context("ConfigMap Drift Detection", func() {
		BeforeEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
		})
		AfterEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
		})

		It("D1-I: should restore ConfigMap data when annotation is preserved but data is tampered", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			cm := &corev1.ConfigMap{}
			cmKey := types.NamespacedName{Name: "workflowexecution-config", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())

			origData := cm.Data["workflowexecution.yaml"]
			origHash := cm.Annotations[resources.AnnotationSpecHash]
			Expect(origData).NotTo(BeEmpty())
			Expect(origHash).NotTo(BeEmpty())

			By("simulating external actor modifying data but PRESERVING the spec-hash annotation")
			cm.Data["workflowexecution.yaml"] = "tampered: true\n"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			By("verifying annotation is still the original hash (simulates the issue)")
			Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
			Expect(cm.Annotations[resources.AnnotationSpecHash]).To(Equal(origHash))
			Expect(cm.Data["workflowexecution.yaml"]).To(Equal("tampered: true\n"))

			By("reconciling — operator should detect content drift and restore")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("verifying data is restored to operator-desired state")
			Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
			Expect(cm.Data["workflowexecution.yaml"]).To(Equal(origData),
				"ConfigMap data should be restored after annotation-preserved tampering")
		})

		It("D4-I: should not update ConfigMap when annotation and content both match (regression guard)", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			cm := &corev1.ConfigMap{}
			cmKey := types.NamespacedName{Name: "workflowexecution-config", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
			origRV := cm.ResourceVersion

			By("reconciling again without any external changes")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
			Expect(cm.ResourceVersion).To(Equal(origRV),
				"ResourceVersion should not change when there is no drift")
		})
	})

	// ======================================================================
	// 17. AnsibleReady Condition Lifecycle (Issue #17, TDD Red — Phase 4)
	// ======================================================================

	Context("AnsibleReady Condition Lifecycle", func() {
		const ansibleSecretName = "ansible-token"

		BeforeEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
			deleteAnsibleSecret(ctx, ansibleSecretName)
		})
		AfterEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
			deleteAnsibleSecret(ctx, ansibleSecretName)
		})

		It("A1: should set AnsibleReady=True/Disabled when ansible is not enabled", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			cr.Spec.Ansible.Enabled = false
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionAnsibleReady)
			Expect(cond).NotTo(BeNil(), "AnsibleReady condition should be present")
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("Disabled"))
		})

		It("A2: should set AnsibleReady=True/Ready when ansible is enabled with valid token Secret", func() {
			createBYOSecrets(ctx)
			createAnsibleSecret(ctx, ansibleSecretName, "token", "my-token-value")
			cr := newCRWithAnsibleEnabled(ansibleSecretName)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionAnsibleReady)
			Expect(cond).NotTo(BeNil(), "AnsibleReady condition should be present")
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("Ready"))
		})

		It("A3: should set AnsibleReady=False/TokenSecretNotFound when Secret does not exist", func() {
			createBYOSecrets(ctx)
			cr := newCRWithAnsibleEnabled("nonexistent-secret")
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionAnsibleReady)
			Expect(cond).NotTo(BeNil(), "AnsibleReady condition should be present")
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("TokenSecretNotFound"))
		})

		It("A4: should set AnsibleReady=False/TokenKeyMissing when Secret lacks the expected key", func() {
			createBYOSecrets(ctx)
			createAnsibleSecret(ctx, ansibleSecretName, "wrong-key", "value")
			cr := newCRWithAnsibleEnabled(ansibleSecretName)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionAnsibleReady)
			Expect(cond).NotTo(BeNil(), "AnsibleReady condition should be present")
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("TokenKeyMissing"))
		})

		It("A5: should set AnsibleReady=False/TokenSecretNotFound when tokenSecretRef is nil", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			cr.Spec.Ansible.Enabled = true
			cr.Spec.Ansible.APIURL = testAwxAPIURL
			cr.Spec.Ansible.TokenSecretRef = nil
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionAnsibleReady)
			Expect(cond).NotTo(BeNil(), "AnsibleReady condition should be present")
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("TokenSecretNotFound"))
		})

		It("A6: should recover AnsibleReady to True when Secret is created after initial failure", func() {
			createBYOSecrets(ctx)
			cr := newCRWithAnsibleEnabled(ansibleSecretName)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			r := reconcileToRunning(ctx)

			By("verifying initially False")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionAnsibleReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))

			By("creating the Secret")
			createAnsibleSecret(ctx, ansibleSecretName, "token", "my-token")

			By("re-reconciling (simulates periodic requeue)")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("verifying recovery")
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			cond = findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionAnsibleReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("Ready"))
		})

		It("A7: should reach PhaseRunning even when AnsibleReady=False (non-blocking)", func() {
			createBYOSecrets(ctx)
			cr := newCRWithAnsibleEnabled("nonexistent-secret")
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseRunning),
				"PhaseRunning must be reached even when AnsibleReady is False")

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionAnsibleReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		})

		It("A6b: should flip AnsibleReady to False when a previously valid Secret is deleted", func() {
			createBYOSecrets(ctx)
			createAnsibleSecret(ctx, ansibleSecretName, "token", "my-token")
			cr := newCRWithAnsibleEnabled(ansibleSecretName)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			r := reconcileToRunning(ctx)

			By("verifying initially True/Ready")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionAnsibleReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))

			By("deleting the Secret")
			deleteAnsibleSecret(ctx, ansibleSecretName)

			By("re-reconciling")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("verifying condition flipped to False")
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			cond = findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionAnsibleReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("TokenSecretNotFound"))
		})

		It("A8: should emit AnsibleConfigInvalid event when Secret is missing", func() {
			createBYOSecrets(ctx)
			cr := newCRWithAnsibleEnabled("nonexistent-secret")
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			r := reconcileToRunning(ctx)

			recorder := r.Recorder.(*events.FakeRecorder)
			var collected []string
		drain:
			for {
				select {
				case ev := <-recorder.Events:
					collected = append(collected, ev)
				default:
					break drain
				}
			}

			Expect(collected).To(ContainElement(ContainSubstring("AnsibleConfigInvalid")))
		})
	})

	Context("Wiring Verification — Hostname Validation via Reconcile", func() {
		BeforeEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
		})
		AfterEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
		})

		It("should reject a CR with an invalid PostgreSQL hostname", func() {
			cr := newMinimalCR()
			cr.Spec.PostgreSQL.Host = "host;rm -rf /"
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			createBYOSecrets(ctx)

			r := newReconciler()
			By("reconcile 1: add finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("reconcile 2: validate — should fail on hostname")
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseError))

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("PostgreSQLHostInvalid"))
		})

		It("should reject a CR with an invalid Valkey hostname", func() {
			cr := newMinimalCR()
			cr.Spec.Valkey.Host = "host user=admin"
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			createBYOSecrets(ctx)

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseError))

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionBYOValidated)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("ValkeyHostInvalid"))
		})
	})

	Context("Wiring Verification — APIServer Watch Handler", func() {
		BeforeEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
		})
		AfterEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
		})

		It("should return reconcile requests for existing Kubernaut CRs", func() {
			cr := newMinimalCR()
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			r := newReconciler()
			reqs := r.apiServerToKubernaut(ctx, &configv1.APIServer{})
			Expect(reqs).To(HaveLen(1))
			Expect(reqs[0].NamespacedName).To(Equal(singletonKey()))
		})

		It("should return empty list when no Kubernaut CRs exist", func() {
			r := newReconciler()
			reqs := r.apiServerToKubernaut(ctx, &configv1.APIServer{})
			Expect(reqs).To(BeEmpty())
		})
	})

	Context("Wiring Verification — Event Recorder Assertions", func() {
		BeforeEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
		})
		AfterEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
		})

		It("should emit MigrationComplete and ManifestsApplied events during reconcileToRunning", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			r := reconcileToRunning(ctx)

			recorder := r.Recorder.(*events.FakeRecorder)
			var collected []string
		drain:
			for {
				select {
				case ev := <-recorder.Events:
					collected = append(collected, ev)
				default:
					break drain
				}
			}

			Expect(collected).To(ContainElement("Normal MigrationComplete Database migration job succeeded"))
			Expect(collected).To(ContainElement("Normal ManifestsApplied All service manifests applied"))
		})

		It("should emit CleanupComplete event on successful deletion", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			r := reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(k8sClient.Delete(ctx, kn)).To(Succeed())

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			recorder := r.Recorder.(*events.FakeRecorder)
			var collected []string
		drain:
			for {
				select {
				case ev := <-recorder.Events:
					collected = append(collected, ev)
				default:
					break drain
				}
			}

			Expect(collected).To(ContainElement("Normal CleanupComplete Cluster-scoped resources cleaned up"))
		})
	})

	Context("Wiring Verification — Finalizer Timeout Force-Removal", func() {
		BeforeEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
		})
		AfterEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
		})

		It("should force-remove finalizer and emit warning when cleanup fails past timeout", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			r := newReconciler()

			By("reconcile 1: add finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("swapping client to one that fails on Delete for ClusterRoles")
			r.Client = &deleteFailingClient{Client: k8sClient}

			By("deleting the CR")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(k8sClient.Delete(ctx, kn)).To(Succeed())

			By("setting now() to 11 minutes after DeletionTimestamp (past 10min timeout)")
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			r.now = func() time.Time {
				return kn.DeletionTimestamp.Add(11 * time.Minute)
			}

			By("reconciling deletion — should force-remove finalizer despite cleanup error")
			r.Client = &deleteFailingClient{Client: k8sClient}
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("verifying finalizer is removed")
			err = k8sClient.Get(ctx, singletonKey(), kn)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "CR should be gone after forced finalizer removal")

			By("verifying FinalizerTimeout warning event")
			recorder := r.Recorder.(*events.FakeRecorder)
			var collected []string
		drain:
			for {
				select {
				case ev := <-recorder.Events:
					collected = append(collected, ev)
				default:
					break drain
				}
			}
			Expect(collected).To(ContainElement(ContainSubstring("Warning FinalizerTimeout")))
		})
	})

	Context("Wiring Verification — Migration Max Retry Exhaustion", func() {
		BeforeEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
		})
		AfterEach(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
		})

		It("should transition to PhaseError when migration Job exceeds max retries", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

			r := newReconciler()
			By("reconcile 1: add finalizer")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("reconcile 2: validate + create migration Job")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			By("patching migration Job status to failed with 10 attempts")
			job := &batchv1.Job{}
			key := types.NamespacedName{Name: "kubernaut-db-migration", Namespace: testNamespace}
			Eventually(func() error {
				return k8sClient.Get(ctx, key, job)
			}, timeout, interval).Should(Succeed())

			now := metav1.Now()
			job.Status.StartTime = &now
			job.Status.Failed = 10
			job.Status.Conditions = append(job.Status.Conditions,
				batchv1.JobCondition{
					Type:   "FailureTarget",
					Status: corev1.ConditionTrue,
				},
				batchv1.JobCondition{
					Type:   batchv1.JobFailed,
					Status: corev1.ConditionTrue,
				},
			)
			Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())

			By("reconcile 3: should detect max retry exhaustion")
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseError))

			cond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionMigrationComplete)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("MigrationFailed"))
			Expect(cond.Message).To(ContainSubstring("manual intervention required"))
		})
	})
})

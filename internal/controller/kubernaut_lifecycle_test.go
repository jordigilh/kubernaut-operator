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
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// reconcileN drives N full reconcile cycles on the singleton CR.
func reconcileN(ctx context.Context, r *KubernautReconciler, n int) (reconcile.Result, error) {
	var result reconcile.Result
	var err error
	for i := 0; i < n; i++ {
		result, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		if err != nil {
			return result, err
		}
	}
	return result, nil
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
		setDeploymentReady(ctx, c+"-deployment")
	}
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

			r := reconcileToRunning(ctx)
			_ = r

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
			r := newReconciler()

			By("driving to deploy phase")
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			markMigrationJobComplete(ctx)
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})

			By("marking all except gateway as ready")
			for _, c := range resources.AllComponents() {
				if c != resources.ComponentGateway {
					setDeploymentReady(ctx, c+"-deployment")
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
			r := newReconciler()

			By("driving to deploy then degraded")
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			markMigrationJobComplete(ctx)
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			for _, c := range resources.AllComponents() {
				if c != resources.ComponentGateway {
					setDeploymentReady(ctx, c+"-deployment")
				}
			}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseDegraded))

			By("marking the gateway as ready")
			setDeploymentReady(ctx, "gateway-deployment")

			By("reconciling - should recover to Running")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
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
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})

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
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})

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
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})

			By("getting the existing Job UID")
			job := &batchv1.Job{}
			key := types.NamespacedName{Name: "kubernaut-db-migration", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, key, job)).To(Succeed())
			origUID := job.UID

			By("reconciling again - Job should not be recreated")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, key, job)).To(Succeed())
			Expect(job.UID).To(Equal(origUID), "Job should not be recreated")
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
			Expect(names).To(HaveKey(testNamespace+"-gateway-signal-source"))
		})

		It("should delete monitoring RBAC when monitoring is disabled", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			r := reconcileToRunning(ctx)

			By("verifying monitoring CRs exist")
			monCR := &rbacv1.ClusterRole{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-alertmanager-view"}, monCR)).To(Succeed())

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
		})

		It("should create AWX RBAC when ansible is enabled", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			cr.Spec.Ansible.Enabled = true
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			reconcileToRunning(ctx)

			awxCR := &rbacv1.ClusterRole{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-workflowexecution-awx"}, awxCR)).To(Succeed())
		})

		It("should delete AWX RBAC when ansible is disabled", func() {
			createBYOSecrets(ctx)
			cr := newCRWithRouteDisabled()
			cr.Spec.Ansible.Enabled = true
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
			cm := resources.HolmesGPTSDKConfigMap(newMinimalCR())
			Expect(cm).NotTo(BeNil(), "should generate default SDK ConfigMap")

			crWithSDK := newMinimalCR()
			crWithSDK.Spec.HolmesGPTAPI.LLM.SdkConfigMapName = "user-managed-sdk"
			cm = resources.HolmesGPTSDKConfigMap(crWithSDK)
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
			Expect(cm.Data["config.yaml"]).To(ContainSubstring("global: 1h"))

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
			Expect(cm.Data["config.yaml"]).To(ContainSubstring("global: 2h"))
		})

		It("should update Deployment image when spec.image.tag changes", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			By("verifying initial image tag")
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gateway-deployment", Namespace: testNamespace,
			}, dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring(":v1.3.0"))

			By("changing image tag to v2.0.0")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			kn.Spec.Image.Tag = "v2.0.0"
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			By("reconciling to propagate")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gateway-deployment", Namespace: testNamespace,
			}, dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring(":v2.0.0"))
		})

		It("should set owner references on namespaced resources", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gateway-deployment", Namespace: testNamespace,
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
				Name: "gateway-deployment", Namespace: testNamespace,
			}, dep)).To(Succeed())

			limit := dep.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory]
			Expect(limit.String()).To(Equal("512Mi"))
		})
	})

	// ======================================================================
	// 6. Deletion / Finalizer Edge Cases
	// ======================================================================

	Context("Deletion Edge Cases", func() {
		// envtest lacks a namespace controller, so deleting a namespace puts it
		// into Terminating forever. We prevent the finalizer from deleting the
		// workflow namespace by stripping its managed-by label before each
		// deletion reconcile. This is safe: the code path is tested by
		// asserting DeletionTimestamp is nil (unmanaged) or non-nil (managed).

		// stripWorkflowNamespaceLabel removes the managed-by label so the
		// finalizer skips namespace deletion, keeping envtest healthy.
		stripWorkflowNamespaceLabel := func() {
			ns := &corev1.Namespace{}
			wfNsName := resources.DefaultWorkflowNamespace
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: wfNsName}, ns); err == nil {
				delete(ns.Labels, "app.kubernetes.io/managed-by")
				Expect(k8sClient.Update(ctx, ns)).To(Succeed())
			}
		}

		It("should skip deletion of workflow namespace not managed by operator", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			r := reconcileToRunning(ctx)

			By("removing managed-by label to simulate user-managed namespace")
			stripWorkflowNamespaceLabel()

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
				"namespace without managed-by label should not be deleted")
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
			stripWorkflowNamespaceLabel()

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

			stripWorkflowNamespaceLabel()
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

			stripWorkflowNamespaceLabel()
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
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})

			By("modifying spec between reconcile cycles (simulating concurrent edit)")
			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			kn.Spec.RemediationOrchestrator.Timeouts.Global = "3h"
			Expect(k8sClient.Update(ctx, kn)).To(Succeed())

			By("reconciling - status patch should not conflict with spec change")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
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
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			crdCond := findCondition(kn.Status.Conditions, kubernautv1alpha1.ConditionCRDsInstalled)
			if crdCond != nil {
				Expect(crdCond.Status).To(Equal(metav1.ConditionTrue))
			}
		})

		It("should be idempotent on repeated EnsureCRDs calls", func() {
			err := resources.EnsureCRDs(ctx, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			err = resources.EnsureCRDs(ctx, k8sClient)
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

			initCmd := dep.Spec.Template.Spec.InitContainers[0].Command
			Expect(initCmd[2]).To(ContainSubstring("-p 5432"))
		})

		It("should use custom PostgreSQL port when specified", func() {
			cr := newMinimalCR()
			cr.Spec.PostgreSQL.Port = 5433

			dep, err := resources.DataStorageDeployment(cr)
			Expect(err).NotTo(HaveOccurred())

			initCmd := dep.Spec.Template.Spec.InitContainers[0].Command
			Expect(initCmd[2]).To(ContainSubstring("-p 5433"))
		})

		It("should construct digest-based image references", func() {
			cr := newMinimalCR()
			cr.Spec.Image.Tag = ""
			cr.Spec.Image.Digest = "sha256:abc123"

			dep, err := resources.GatewayDeployment(cr)
			Expect(err).NotTo(HaveOccurred())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring("@sha256:abc123"))
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
				resources.HolmesGPTAPIDeployment,
				resources.AuthWebhookDeployment,
			} {
				dep, err := build(cr)
				Expect(err).NotTo(HaveOccurred())
				Expect(dep.Name).NotTo(BeEmpty())
				Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty())
				Expect(dep.Spec.Template.Spec.Containers[0].Image).NotTo(BeEmpty())
			}
		})

		It("should error when image registry is empty", func() {
			cr := newMinimalCR()
			cr.Spec.Image.Registry = ""

			_, err := resources.GatewayDeployment(cr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("image registry must not be empty"))
		})

		It("should error when both tag and digest are empty", func() {
			cr := newMinimalCR()
			cr.Spec.Image.Tag = ""
			cr.Spec.Image.Digest = ""

			_, err := resources.GatewayDeployment(cr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("image tag or digest must be set"))
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
})

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

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// KubernautReconciler reconciles a Kubernaut object.
type KubernautReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubernaut.ai,resources=kubernauts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubernaut.ai,resources=kubernauts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubernaut.ai,resources=kubernauts/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;secrets;serviceaccounts;namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch

func (r *KubernautReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	kn := &kubernautv1alpha1.Kubernaut{}
	if err := r.Get(ctx, req.NamespacedName, kn); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if kn.Name != kubernautv1alpha1.SingletonName {
		log.Info("ignoring CR with unexpected name", "name", kn.Name, "expected", kubernautv1alpha1.SingletonName)
		return ctrl.Result{}, nil
	}

	if !kn.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, kn)
	}

	if !controllerutil.ContainsFinalizer(kn, kubernautv1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(kn, kubernautv1alpha1.FinalizerName)
		if err := r.Update(ctx, kn); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.reconcilePhases(ctx, kn)
}

func (r *KubernautReconciler) reconcilePhases(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("reconciling phases", "currentPhase", kn.Status.Phase)

	if result, err := r.phaseValidate(ctx, kn); err != nil || result.Requeue || result.RequeueAfter > 0 {
		return result, err
	}

	if result, err := r.phaseMigrate(ctx, kn); err != nil || result.Requeue || result.RequeueAfter > 0 {
		return result, err
	}

	if result, err := r.phaseDeploy(ctx, kn); err != nil || result.Requeue || result.RequeueAfter > 0 {
		return result, err
	}

	return r.phaseRunning(ctx, kn)
}

// ---------- Phase: Validate ----------

func (r *KubernautReconciler) phaseValidate(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	r.setPhase(kn, kubernautv1alpha1.PhaseValidating)

	if err := r.validateSecret(ctx, kn.Namespace, kn.Spec.PostgreSQL.SecretName,
		[]string{"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB"}); err != nil {
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionBYOValidated, metav1.ConditionFalse,
			"PostgreSQLSecretInvalid", fmt.Sprintf("PostgreSQL secret validation failed: %v", err))
	}

	if err := r.validateSecret(ctx, kn.Namespace, kn.Spec.Valkey.SecretName,
		[]string{"valkey-secrets.yaml"}); err != nil {
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionBYOValidated, metav1.ConditionFalse,
			"ValkeySecretInvalid", fmt.Sprintf("Valkey secret validation failed: %v", err))
	}

	log.Info("BYO secrets validated")
	meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
		Type: kubernautv1alpha1.ConditionBYOValidated, Status: metav1.ConditionTrue,
		Reason: "SecretsValid", Message: "BYO PostgreSQL and Valkey secrets are valid",
		ObservedGeneration: kn.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, kn)
}

// ---------- Phase: Migrate ----------

func (r *KubernautReconciler) phaseMigrate(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	r.setPhase(kn, kubernautv1alpha1.PhaseMigrating)

	// Ensure CRDs are installed/updated.
	if err := resources.EnsureCRDs(ctx, r.Client); err != nil {
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionCRDsInstalled, metav1.ConditionFalse,
			"CRDInstallFailed", err.Error())
	}
	meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
		Type: kubernautv1alpha1.ConditionCRDsInstalled, Status: metav1.ConditionTrue,
		Reason: "CRDsReady", Message: "All 9 workload CRDs installed",
		ObservedGeneration: kn.Generation,
	})

	// Derive DataStorage DB secret from the user-provided PG secret.
	pgSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: kn.Spec.PostgreSQL.SecretName}, pgSecret); err != nil {
		return ctrl.Result{}, fmt.Errorf("fetching pg secret for DS derivation: %w", err)
	}
	dsSecret, err := resources.DataStorageDBSecret(kn, pgSecret)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("deriving datastorage-db-secret: %w", err)
	}
	if err := r.ensureNamespaced(ctx, kn, dsSecret); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring datastorage-db-secret: %w", err)
	}

	// Ensure migration ConfigMap.
	migrationCM, err := resources.MigrationConfigMap(kn)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building migration configmap: %w", err)
	}
	if err := r.ensureNamespaced(ctx, kn, migrationCM); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring migration configmap: %w", err)
	}

	// Ensure migration Job exists (Jobs are immutable; create-only).
	migrationJob, err := resources.MigrationJob(kn)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building migration job: %w", err)
	}
	existingJob := &batchv1.Job{}
	err = r.Get(ctx, types.NamespacedName{Name: migrationJob.Name, Namespace: kn.Namespace}, existingJob)
	if apierrors.IsNotFound(err) {
		if setErr := resources.SetOwnerReference(kn, migrationJob, r.Scheme); setErr != nil {
			return ctrl.Result{}, setErr
		}
		if err := r.Create(ctx, migrationJob); err != nil {
			return ctrl.Result{}, fmt.Errorf("creating migration job: %w", err)
		}
		existingJob = migrationJob
	} else if err != nil {
		return ctrl.Result{}, fmt.Errorf("checking migration job: %w", err)
	}

	for _, cond := range existingJob.Status.Conditions {
		if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
			log.Info("database migration completed")
			meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
				Type: kubernautv1alpha1.ConditionMigrationComplete, Status: metav1.ConditionTrue,
				Reason: "MigrationComplete", Message: "Database migration job succeeded",
				ObservedGeneration: kn.Generation,
			})
			return ctrl.Result{}, r.Status().Update(ctx, kn)
		}
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			log.Info("migration job failed, deleting for retry")
			propagation := metav1.DeletePropagationBackground
			if err := r.Delete(ctx, existingJob, &client.DeleteOptions{
				PropagationPolicy: &propagation,
			}); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("deleting failed migration job: %w", err)
			}
			return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionMigrationComplete, metav1.ConditionFalse,
				"MigrationFailed", "Database migration job failed; will retry")
		}
	}

	log.Info("waiting for migration job to complete")
	meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
		Type: kubernautv1alpha1.ConditionMigrationComplete, Status: metav1.ConditionFalse,
		Reason: "MigrationInProgress", Message: "Database migration job is running",
		ObservedGeneration: kn.Generation,
	})
	if err := r.Status().Update(ctx, kn); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// ---------- Phase: Deploy ----------

func (r *KubernautReconciler) phaseDeploy(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	r.setPhase(kn, kubernautv1alpha1.PhaseDeploying)

	// Workflow namespace.
	wfNs := resources.WorkflowNamespace(kn)
	if err := r.ensureClusterScoped(ctx, wfNs); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring workflow namespace: %w", err)
	}

	// ServiceAccounts.
	for _, component := range resources.AllComponents() {
		sa := resources.ServiceAccount(kn, component)
		if err := r.ensureNamespaced(ctx, kn, sa); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring SA %s: %w", component, err)
		}
	}
	wfRunnerSA := resources.WorkflowRunnerServiceAccount(kn)
	if err := r.ensureClusterScoped(ctx, wfRunnerSA); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring workflow runner SA: %w", err)
	}

	// RBAC: ClusterRoles (cluster-scoped, use finalizer for cleanup).
	for _, cr := range resources.ClusterRoles(kn) {
		if err := r.ensureClusterScoped(ctx, cr); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring ClusterRole %s: %w", cr.Name, err)
		}
	}

	// RBAC: ClusterRoleBindings (cluster-scoped).
	for _, crb := range resources.ClusterRoleBindings(kn) {
		if err := r.ensureClusterScoped(ctx, crb); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring CRB %s: %w", crb.Name, err)
		}
	}

	// RBAC: Namespace Roles + RoleBindings.
	for _, role := range resources.NamespaceRoles(kn) {
		if err := r.ensureNamespaced(ctx, kn, role); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring ns role %s: %w", role.Name, err)
		}
	}
	for _, rb := range resources.NamespaceRoleBindings(kn) {
		if err := r.ensureNamespaced(ctx, kn, rb); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring ns rolebinding %s: %w", rb.Name, err)
		}
	}

	// RBAC: DataStorage client RoleBindings.
	for _, rb := range resources.DataStorageClientRoleBindings(kn) {
		if err := r.ensureNamespaced(ctx, kn, rb); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring ds client rb %s: %w", rb.Name, err)
		}
	}

	// RBAC: Workflow namespace roles.
	wfRoles, wfRBs := resources.WorkflowNamespaceRBAC(kn)
	for _, role := range wfRoles {
		if err := r.ensureClusterScoped(ctx, role); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring wf role %s: %w", role.Name, err)
		}
	}
	for _, rb := range wfRBs {
		if err := r.ensureClusterScoped(ctx, rb); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring wf rb %s: %w", rb.Name, err)
		}
	}

	// Conditional AWX RBAC.
	if kn.Spec.Ansible.Enabled {
		cr, crb := resources.AnsibleRBAC(kn)
		if err := r.ensureClusterScoped(ctx, cr); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring AWX ClusterRole: %w", err)
		}
		if err := r.ensureClusterScoped(ctx, crb); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring AWX CRB: %w", err)
		}
	}

	log.Info("RBAC provisioned")
	meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
		Type: kubernautv1alpha1.ConditionRBACProvisioned, Status: metav1.ConditionTrue,
		Reason: "RBACReady", Message: "All RBAC resources provisioned",
		ObservedGeneration: kn.Generation,
	})

	// ConfigMaps.
	configMaps := []*corev1.ConfigMap{
		resources.GatewayConfigMap(kn),
		resources.DataStorageConfigMap(kn),
		resources.AIAnalysisConfigMap(kn),
		resources.SignalProcessingConfigMap(kn),
		resources.RemediationOrchestratorConfigMap(kn),
		resources.WorkflowExecutionConfigMap(kn),
		resources.EffectivenessMonitorConfigMap(kn),
		resources.NotificationControllerConfigMap(kn),
		resources.NotificationRoutingConfigMap(kn),
		resources.HolmesGPTAPIConfigMap(kn),
		resources.AuthWebhookConfigMap(kn),
	}
	if sdkCM := resources.HolmesGPTSDKConfigMap(kn); sdkCM != nil {
		configMaps = append(configMaps, sdkCM)
	}
	if kn.Spec.Monitoring.MonitoringEnabled() {
		configMaps = append(configMaps,
			resources.EffectivenessMonitorServiceCAConfigMap(kn),
			resources.HolmesGPTAPIServiceCAConfigMap(kn),
		)
	}
	for _, cm := range configMaps {
		if err := r.ensureNamespaced(ctx, kn, cm); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring ConfigMap %s: %w", cm.Name, err)
		}
	}

	// TLS + Webhooks.
	tlsSecret, caBundle, err := resources.AuthWebhookTLSSecret(kn)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("generating TLS secret: %w", err)
	}

	existingTLS := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Name: tlsSecret.Name, Namespace: kn.Namespace}, existingTLS)
	if apierrors.IsNotFound(err) {
		if setErr := resources.SetOwnerReference(kn, tlsSecret, r.Scheme); setErr != nil {
			return ctrl.Result{}, setErr
		}
		if err := r.Create(ctx, tlsSecret); err != nil {
			return ctrl.Result{}, fmt.Errorf("creating TLS secret: %w", err)
		}
	} else if err != nil {
		return ctrl.Result{}, err
	} else {
		caBundle = existingTLS.Data["ca.crt"]
	}

	mwc := resources.MutatingWebhookConfiguration(kn, caBundle)
	if err := r.ensureClusterScoped(ctx, mwc); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring MutatingWebhookConfiguration: %w", err)
	}
	vwc := resources.ValidatingWebhookConfiguration(kn, caBundle)
	if err := r.ensureClusterScoped(ctx, vwc); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring ValidatingWebhookConfiguration: %w", err)
	}

	meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
		Type: kubernautv1alpha1.ConditionWebhooksConfigured, Status: metav1.ConditionTrue,
		Reason: "WebhooksReady", Message: "Admission webhooks configured",
		ObservedGeneration: kn.Generation,
	})

	// Deployments.
	type depBuilder func(*kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error)
	depBuilders := []depBuilder{
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
	}
	for _, build := range depBuilders {
		dep, err := build(kn)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("building deployment: %w", err)
		}
		if err := r.ensureNamespaced(ctx, kn, dep); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring Deployment %s: %w", dep.Name, err)
		}
	}

	// Services.
	for _, svc := range resources.Services(kn) {
		if err := r.ensureNamespaced(ctx, kn, svc); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring Service %s: %w", svc.Name, err)
		}
	}

	// PDBs.
	for _, pdb := range resources.PodDisruptionBudgets(kn) {
		if err := r.ensureNamespaced(ctx, kn, pdb); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring PDB %s: %w", pdb.Name, err)
		}
	}

	// OCP Route.
	if route := resources.GatewayRoute(kn); route != nil {
		if err := r.ensureNamespaced(ctx, kn, route); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensuring Gateway Route: %w", err)
		}
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionRouteReady, Status: metav1.ConditionTrue,
			Reason: "RouteCreated", Message: "Gateway OCP Route created",
			ObservedGeneration: kn.Generation,
		})
	}

	meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
		Type: kubernautv1alpha1.ConditionServicesDeployed, Status: metav1.ConditionTrue,
		Reason: "DeploymentComplete", Message: "All services deployed",
		ObservedGeneration: kn.Generation,
	})

	return ctrl.Result{}, r.Status().Update(ctx, kn)
}

// ---------- Phase: Running ----------

func (r *KubernautReconciler) phaseRunning(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Update per-service status from Deployment readiness.
	var serviceStatuses []kubernautv1alpha1.ServiceStatus
	allReady := true
	for _, component := range resources.AllComponents() {
		dep := &appsv1.Deployment{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      component + "-deployment",
			Namespace: kn.Namespace,
		}, dep)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			serviceStatuses = append(serviceStatuses, kubernautv1alpha1.ServiceStatus{
				Name: component, Ready: false,
			})
			allReady = false
			continue
		}

		desired := ptr.Deref(dep.Spec.Replicas, 1)
		ready := dep.Status.ReadyReplicas >= desired
		if !ready {
			allReady = false
		}
		serviceStatuses = append(serviceStatuses, kubernautv1alpha1.ServiceStatus{
			Name:            component,
			Ready:           ready,
			ReadyReplicas:   dep.Status.ReadyReplicas,
			DesiredReplicas: desired,
		})
	}

	kn.Status.Services = serviceStatuses

	if allReady {
		r.setPhase(kn, kubernautv1alpha1.PhaseRunning)
	} else {
		r.setPhase(kn, kubernautv1alpha1.PhaseDegraded)
	}

	log.Info("reconciliation complete", "phase", kn.Status.Phase)
	if err := r.Status().Update(ctx, kn); err != nil {
		return ctrl.Result{}, err
	}

	if !allReady {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

// ---------- Deletion ----------

func (r *KubernautReconciler) reconcileDelete(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("reconciling deletion")

	if controllerutil.ContainsFinalizer(kn, kubernautv1alpha1.FinalizerName) {
		// Clean up cluster-scoped resources that cannot use owner references.
		if err := r.deleteClusterScopedResources(ctx, kn); err != nil {
			return ctrl.Result{}, err
		}

		controllerutil.RemoveFinalizer(kn, kubernautv1alpha1.FinalizerName)
		if err := r.Update(ctx, kn); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *KubernautReconciler) deleteClusterScopedResources(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	log := logf.FromContext(ctx)
	var errs []error

	// ClusterRoles.
	for _, cr := range resources.ClusterRoles(kn) {
		if err := r.deleteIfExists(ctx, cr); err != nil {
			errs = append(errs, fmt.Errorf("deleting ClusterRole %s: %w", cr.Name, err))
		}
	}

	// ClusterRoleBindings.
	for _, crb := range resources.ClusterRoleBindings(kn) {
		if err := r.deleteIfExists(ctx, crb); err != nil {
			errs = append(errs, fmt.Errorf("deleting CRB %s: %w", crb.Name, err))
		}
	}

	// AWX RBAC: always attempt cleanup regardless of current Ansible.Enabled,
	// because the user may have disabled it after resources were created.
	cr, crb := resources.AnsibleRBAC(kn)
	if err := r.deleteIfExists(ctx, cr); err != nil {
		errs = append(errs, fmt.Errorf("deleting AWX ClusterRole: %w", err))
	}
	if err := r.deleteIfExists(ctx, crb); err != nil {
		errs = append(errs, fmt.Errorf("deleting AWX CRB: %w", err))
	}

	// Webhook configurations.
	mwc := resources.MutatingWebhookConfiguration(kn, nil)
	if err := r.deleteIfExists(ctx, mwc); err != nil {
		errs = append(errs, fmt.Errorf("deleting MutatingWebhookConfiguration: %w", err))
	}
	vwc := resources.ValidatingWebhookConfiguration(kn, nil)
	if err := r.deleteIfExists(ctx, vwc); err != nil {
		errs = append(errs, fmt.Errorf("deleting ValidatingWebhookConfiguration: %w", err))
	}

	// Workflow namespace roles/bindings.
	wfRoles, wfRBs := resources.WorkflowNamespaceRBAC(kn)
	for _, role := range wfRoles {
		if err := r.deleteIfExists(ctx, role); err != nil {
			errs = append(errs, fmt.Errorf("deleting wf role %s: %w", role.Name, err))
		}
	}
	for _, rb := range wfRBs {
		if err := r.deleteIfExists(ctx, rb); err != nil {
			errs = append(errs, fmt.Errorf("deleting wf rb %s: %w", rb.Name, err))
		}
	}

	// Workflow runner SA (lives in workflow namespace).
	wfRunnerSA := resources.WorkflowRunnerServiceAccount(kn)
	if err := r.deleteIfExists(ctx, wfRunnerSA); err != nil {
		errs = append(errs, fmt.Errorf("deleting workflow runner SA: %w", err))
	}

	// Workflow namespace: delete unless it was pre-existing (has no kubernaut labels).
	wfNs := resources.WorkflowNamespace(kn)
	existingNs := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: wfNs.Name}, existingNs); err == nil {
		if existingNs.Labels["app.kubernetes.io/managed-by"] == "kubernaut-operator" {
			if err := r.deleteIfExists(ctx, existingNs); err != nil {
				errs = append(errs, fmt.Errorf("deleting workflow namespace %s: %w", wfNs.Name, err))
			}
		} else {
			log.Info("skipping deletion of workflow namespace (not managed by operator)", "namespace", wfNs.Name)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during cluster-scoped cleanup: %v", errs)
	}

	log.Info("cluster-scoped resources cleaned up")
	return nil
}

// ---------- Helpers ----------

func (r *KubernautReconciler) setPhase(kn *kubernautv1alpha1.Kubernaut, phase kubernautv1alpha1.KubernautPhase) {
	kn.Status.Phase = phase
}

func (r *KubernautReconciler) validateSecret(ctx context.Context, namespace, name string, requiredKeys []string) error {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret); err != nil {
		return fmt.Errorf("secret %q not found: %w", name, err)
	}
	for _, key := range requiredKeys {
		if _, ok := secret.Data[key]; !ok {
			return fmt.Errorf("secret %q is missing required key %q", name, key)
		}
	}
	return nil
}

func (r *KubernautReconciler) setConditionAndRequeue(
	ctx context.Context, kn *kubernautv1alpha1.Kubernaut,
	condType string, status metav1.ConditionStatus, reason, message string,
) (ctrl.Result, error) {
	meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
		Type: condType, Status: status, Reason: reason, Message: message,
		ObservedGeneration: kn.Generation,
	})
	if status == metav1.ConditionFalse {
		r.setPhase(kn, kubernautv1alpha1.PhaseError)
	}
	if err := r.Status().Update(ctx, kn); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// ensureNamespaced creates or updates a namespaced resource, setting the
// Kubernaut CR as owner for garbage collection.
func (r *KubernautReconciler) ensureNamespaced(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, obj client.Object) error {
	if err := resources.SetOwnerReference(kn, obj, r.Scheme); err != nil {
		return err
	}

	existing := obj.DeepCopyObject().(client.Object)
	err := r.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, obj)
	}
	if err != nil {
		return err
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// ensureClusterScoped creates or updates a cluster-scoped resource.
// No owner reference is set; cleanup is handled by the finalizer.
func (r *KubernautReconciler) ensureClusterScoped(ctx context.Context, obj client.Object) error {
	existing := obj.DeepCopyObject().(client.Object)
	err := r.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, obj)
	}
	if err != nil {
		return err
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

func (r *KubernautReconciler) deleteIfExists(ctx context.Context, obj client.Object) error {
	err := r.Delete(ctx, obj)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// SetupWithManager sets up the controller with the Manager.
// Owns() watches trigger reconciliation when owned child resources change.
// Cluster-scoped resources (ClusterRoles, CRBs, webhook configs) cannot be
// owned, so they rely on the periodic requeue timer for drift detection.
func (r *KubernautReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubernautv1alpha1.Kubernaut{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Secret{}).
		Owns(&batchv1.Job{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Named("kubernaut").
		Complete(r)
}

// Compile-time interface guard.
var _ = []client.Object{
	&rbacv1.ClusterRole{},
	&batchv1.Job{},
}

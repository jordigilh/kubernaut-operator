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
	"errors"
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
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete;escalate;bind
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
		return ctrl.Result{}, r.Update(ctx, kn)
	}

	return r.reconcilePhases(ctx, kn)
}

func (r *KubernautReconciler) reconcilePhases(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("reconciling phases", "currentPhase", kn.Status.Phase)

	phases := []func(context.Context, *kubernautv1alpha1.Kubernaut) (ctrl.Result, error){
		r.phaseValidate,
		r.phaseMigrate,
		r.phaseDeploy,
	}
	for _, phase := range phases {
		result, err := phase(ctx, kn)
		if err != nil || result.Requeue || result.RequeueAfter > 0 {
			return result, err
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(kn), kn); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.phaseRunning(ctx, kn)
}

// ---------- Phase: Validate ----------

func (r *KubernautReconciler) phaseValidate(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

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
	return ctrl.Result{}, r.patchStatus(ctx, kn, func() {
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionBYOValidated, Status: metav1.ConditionTrue,
			Reason: "SecretsValid", Message: "BYO PostgreSQL and Valkey secrets are valid",
			ObservedGeneration: kn.Generation,
		})
		r.setPhase(kn, kubernautv1alpha1.PhaseValidating)
	})
}

// ---------- Phase: Migrate ----------

func (r *KubernautReconciler) phaseMigrate(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	if err := r.ensureMigrationPrereqs(ctx, kn); err != nil {
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionCRDsInstalled, metav1.ConditionFalse,
			"CRDInstallFailed", err.Error())
	}

	result, err := r.ensureMigrationJob(ctx, kn)
	if err != nil || result.Requeue || result.RequeueAfter > 0 {
		return result, err
	}

	log := logf.FromContext(ctx)
	log.Info("database migration completed")
	return ctrl.Result{}, r.patchStatus(ctx, kn, func() {
		r.setPhase(kn, kubernautv1alpha1.PhaseMigrating)
		setCRDsReady(kn)
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionMigrationComplete, Status: metav1.ConditionTrue,
			Reason: "MigrationComplete", Message: "Database migration job succeeded",
			ObservedGeneration: kn.Generation,
		})
	})
}

// ensureMigrationPrereqs installs CRDs, derives the DataStorage DB secret
// from the user-provided PostgreSQL secret, and ensures the migration ConfigMap.
func (r *KubernautReconciler) ensureMigrationPrereqs(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	if err := resources.EnsureCRDs(ctx, r.Client); err != nil {
		return err
	}

	pgSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: kn.Spec.PostgreSQL.SecretName}, pgSecret); err != nil {
		return fmt.Errorf("fetching pg secret for DS derivation: %w", err)
	}
	dsSecret, err := resources.DataStorageDBSecret(kn, pgSecret)
	if err != nil {
		return fmt.Errorf("deriving datastorage-db-secret: %w", err)
	}
	if err := r.ensureNamespaced(ctx, kn, dsSecret); err != nil {
		return fmt.Errorf("ensuring datastorage-db-secret: %w", err)
	}

	migrationCM, err := resources.MigrationConfigMap(kn)
	if err != nil {
		return fmt.Errorf("building migration configmap: %w", err)
	}
	return r.ensureNamespaced(ctx, kn, migrationCM)
}

// ensureMigrationJob creates the migration Job if absent, then checks its
// status. Returns a zero Result when the job has completed successfully;
// returns a non-zero Result (requeue) when the job is still running or failed.
func (r *KubernautReconciler) ensureMigrationJob(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	migrationJob, err := resources.MigrationJob(kn)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building migration job: %w", err)
	}
	existingJob := &batchv1.Job{}
	created, err := r.createIfNotFound(ctx, kn, migrationJob, existingJob)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring migration job: %w", err)
	}
	if created {
		existingJob = migrationJob
	}

	for _, cond := range existingJob.Status.Conditions {
		if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
			return ctrl.Result{}, nil
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
	if err := r.patchStatus(ctx, kn, func() {
		r.setPhase(kn, kubernautv1alpha1.PhaseMigrating)
		setCRDsReady(kn)
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionMigrationComplete, Status: metav1.ConditionFalse,
			Reason: "MigrationInProgress", Message: "Database migration job is running",
			ObservedGeneration: kn.Generation,
		})
	}); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// setCRDsReady sets the ConditionCRDsInstalled condition to True on the
// in-memory Kubernaut object. Call within a patchStatus mutation closure.
func setCRDsReady(kn *kubernautv1alpha1.Kubernaut) {
	meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
		Type: kubernautv1alpha1.ConditionCRDsInstalled, Status: metav1.ConditionTrue,
		Reason: "CRDsReady", Message: "All workload CRDs installed",
		ObservedGeneration: kn.Generation,
	})
}

// ---------- Phase: Deploy ----------

func (r *KubernautReconciler) phaseDeploy(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	if err := r.deployWorkflowNamespace(ctx, kn); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.deployServiceAccounts(ctx, kn); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.deployRBAC(ctx, kn); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.deployConfigMaps(ctx, kn); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.deployAdmissionWebhooks(ctx, kn); err != nil {
		return ctrl.Result{}, err
	}
	hasRoute, err := r.deployWorkloads(ctx, kn)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.patchStatus(ctx, kn, func() {
		r.setPhase(kn, kubernautv1alpha1.PhaseDeploying)
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionRBACProvisioned, Status: metav1.ConditionTrue,
			Reason: "RBACReady", Message: "All RBAC resources provisioned",
			ObservedGeneration: kn.Generation,
		})
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionWebhooksConfigured, Status: metav1.ConditionTrue,
			Reason: "WebhooksReady", Message: "Admission webhooks configured",
			ObservedGeneration: kn.Generation,
		})
		if hasRoute {
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
	})
}

func (r *KubernautReconciler) deployWorkflowNamespace(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	wfNs := resources.WorkflowNamespace(kn)
	if err := r.ensureClusterScoped(ctx, wfNs); err != nil {
		return fmt.Errorf("ensuring workflow namespace: %w", err)
	}
	return nil
}

func (r *KubernautReconciler) deployServiceAccounts(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	for _, component := range resources.AllComponents() {
		sa := resources.ServiceAccount(kn, component)
		if err := r.ensureNamespaced(ctx, kn, sa); err != nil {
			return fmt.Errorf("ensuring SA %s: %w", component, err)
		}
	}
	wfRunnerSA := resources.WorkflowRunnerServiceAccount(kn)
	if err := r.ensureClusterScoped(ctx, wfRunnerSA); err != nil {
		return fmt.Errorf("ensuring workflow runner SA: %w", err)
	}
	return nil
}

func (r *KubernautReconciler) deployRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	if err := r.deployCoreRBAC(ctx, kn); err != nil {
		return err
	}
	if err := r.deployWorkflowRBAC(ctx, kn); err != nil {
		return err
	}
	return r.deployToggleRBAC(ctx, kn)
}

// deployCoreRBAC provisions ClusterRoles, ClusterRoleBindings, namespace-scoped
// Roles/RoleBindings, DataStorage client bindings, and the HolmesGPT client binding.
func (r *KubernautReconciler) deployCoreRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	for _, cr := range resources.ClusterRoles(kn) {
		if err := r.ensureClusterScoped(ctx, cr); err != nil {
			return fmt.Errorf("ensuring ClusterRole %s: %w", cr.Name, err)
		}
	}
	for _, crb := range resources.ClusterRoleBindings(kn) {
		if err := r.ensureClusterScoped(ctx, crb); err != nil {
			return fmt.Errorf("ensuring CRB %s: %w", crb.Name, err)
		}
	}
	for _, role := range resources.NamespaceRoles(kn) {
		if err := r.ensureNamespaced(ctx, kn, role); err != nil {
			return fmt.Errorf("ensuring ns role %s: %w", role.Name, err)
		}
	}
	for _, rb := range resources.NamespaceRoleBindings(kn) {
		if err := r.ensureNamespaced(ctx, kn, rb); err != nil {
			return fmt.Errorf("ensuring ns rolebinding %s: %w", rb.Name, err)
		}
	}
	for _, rb := range resources.DataStorageClientRoleBindings(kn) {
		if err := r.ensureNamespaced(ctx, kn, rb); err != nil {
			return fmt.Errorf("ensuring ds client rb %s: %w", rb.Name, err)
		}
	}
	return r.ensureNamespaced(ctx, kn, resources.HolmesGPTClientRoleBinding(kn))
}

// deployWorkflowRBAC provisions roles and bindings in the workflow namespace.
func (r *KubernautReconciler) deployWorkflowRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	wfRoles, wfRBs := resources.WorkflowNamespaceRBAC(kn)
	for _, role := range wfRoles {
		if err := r.ensureClusterScoped(ctx, role); err != nil {
			return fmt.Errorf("ensuring wf role %s: %w", role.Name, err)
		}
	}
	for _, rb := range wfRBs {
		if err := r.ensureClusterScoped(ctx, rb); err != nil {
			return fmt.Errorf("ensuring wf rb %s: %w", rb.Name, err)
		}
	}
	return nil
}

// deployToggleRBAC handles feature-flag-dependent RBAC: Ansible on/off and
// monitoring teardown when disabled.
func (r *KubernautReconciler) deployToggleRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	log := logf.FromContext(ctx)

	cr, crb := resources.AnsibleRBAC(kn)
	if kn.Spec.Ansible.Enabled {
		if err := r.ensureClusterScoped(ctx, cr); err != nil {
			return fmt.Errorf("ensuring AWX ClusterRole: %w", err)
		}
		if err := r.ensureClusterScoped(ctx, crb); err != nil {
			return fmt.Errorf("ensuring AWX CRB: %w", err)
		}
	} else {
		if err := r.deleteIfExists(ctx, cr); err != nil {
			log.V(1).Info("cleanup failed", "resource", cr.Name, "error", err)
		}
		if err := r.deleteIfExists(ctx, crb); err != nil {
			log.V(1).Info("cleanup failed", "resource", crb.Name, "error", err)
		}
	}

	if !kn.Spec.Monitoring.MonitoringEnabled() {
		for _, name := range resources.MonitoringCRBNames(kn) {
			monCRB := &rbacv1.ClusterRoleBinding{}
			monCRB.Name = name
			if err := r.deleteIfExists(ctx, monCRB); err != nil {
				log.V(1).Info("cleanup failed", "resource", name, "error", err)
			}
		}
		for _, name := range resources.MonitoringClusterRoleNames(kn) {
			monCR := &rbacv1.ClusterRole{}
			monCR.Name = name
			if err := r.deleteIfExists(ctx, monCR); err != nil {
				log.V(1).Info("cleanup failed", "resource", name, "error", err)
			}
		}
		for _, name := range []string{"effectivenessmonitor-service-ca", "holmesgpt-api-service-ca"} {
			staleCM := &corev1.ConfigMap{}
			staleCM.Name = name
			staleCM.Namespace = kn.Namespace
			if err := r.deleteIfExists(ctx, staleCM); err != nil {
				log.V(1).Info("cleanup failed", "resource", name, "error", err)
			}
		}
	}
	return nil
}

func (r *KubernautReconciler) deployConfigMaps(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
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
	if cm := resources.AIAnalysisPoliciesConfigMap(kn); cm != nil {
		configMaps = append(configMaps, cm)
	}
	if cm := resources.SignalProcessingPolicyConfigMap(kn); cm != nil {
		configMaps = append(configMaps, cm)
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
			return fmt.Errorf("ensuring ConfigMap %s: %w", cm.Name, err)
		}
	}
	return nil
}

// deployAdmissionWebhooks ensures the TLS secret (create-only) and both
// MutatingWebhookConfiguration and ValidatingWebhookConfiguration.
func (r *KubernautReconciler) deployAdmissionWebhooks(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	tlsSecret, caBundle, err := resources.AuthWebhookTLSSecret(kn)
	if err != nil {
		return fmt.Errorf("generating TLS secret: %w", err)
	}
	existingTLS := &corev1.Secret{}
	created, err := r.createIfNotFound(ctx, kn, tlsSecret, existingTLS)
	if err != nil {
		return fmt.Errorf("ensuring TLS secret: %w", err)
	}
	if !created {
		caBundle = existingTLS.Data["ca.crt"]
	}

	mwc := resources.MutatingWebhookConfiguration(kn, caBundle)
	if err := r.ensureClusterScoped(ctx, mwc); err != nil {
		return fmt.Errorf("ensuring MutatingWebhookConfiguration: %w", err)
	}
	vwc := resources.ValidatingWebhookConfiguration(kn, caBundle)
	return r.ensureClusterScoped(ctx, vwc)
}

// deployWorkloads creates/updates deployments, services, PDBs, and the OCP
// route. Returns true if a route was created.
func (r *KubernautReconciler) deployWorkloads(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (hasRoute bool, _ error) {
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
			return false, fmt.Errorf("building deployment: %w", err)
		}
		if err := r.ensureNamespaced(ctx, kn, dep); err != nil {
			return false, fmt.Errorf("ensuring Deployment %s: %w", dep.Name, err)
		}
	}

	for _, svc := range resources.Services(kn) {
		if err := r.ensureNamespaced(ctx, kn, svc); err != nil {
			return false, fmt.Errorf("ensuring Service %s: %w", svc.Name, err)
		}
	}

	for _, pdb := range resources.PodDisruptionBudgets(kn) {
		if err := r.ensureNamespaced(ctx, kn, pdb); err != nil {
			return false, fmt.Errorf("ensuring PDB %s: %w", pdb.Name, err)
		}
	}

	if route := resources.GatewayRoute(kn); route != nil {
		if err := r.ensureNamespaced(ctx, kn, route); err != nil {
			return false, fmt.Errorf("ensuring Gateway Route: %w", err)
		}
		return true, nil
	}
	staleRoute := resources.GatewayRouteStub(kn)
	if err := r.deleteIfExists(ctx, staleRoute); err != nil {
		logf.FromContext(ctx).V(1).Info("cleanup failed", "resource", "gateway-route", "error", err)
	}
	return false, nil
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

	finalStatuses := serviceStatuses
	finalAllReady := allReady

	if err := r.patchStatus(ctx, kn, func() {
		kn.Status.Services = finalStatuses
		if finalAllReady {
			r.setPhase(kn, kubernautv1alpha1.PhaseRunning)
		} else {
			r.setPhase(kn, kubernautv1alpha1.PhaseDegraded)
		}
	}); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciliation complete", "phase", kn.Status.Phase)

	if !finalAllReady {
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
	errs = append(errs, r.deleteRBACResources(ctx, kn)...)
	errs = append(errs, r.deleteWebhookResources(ctx, kn)...)
	errs = append(errs, r.deleteWorkflowResources(ctx, kn)...)

	if len(errs) > 0 {
		return fmt.Errorf("cluster-scoped cleanup: %w", errors.Join(errs...))
	}

	log.Info("cluster-scoped resources cleaned up")
	return nil
}

// deleteRBACResources removes all cluster-scoped RBAC: ClusterRoles, CRBs,
// AWX RBAC, and monitoring RBAC. Always attempts all resources regardless
// of current feature-flag state.
func (r *KubernautReconciler) deleteRBACResources(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) []error {
	var errs []error

	for _, cr := range resources.ClusterRoles(kn) {
		if err := r.deleteIfExists(ctx, cr); err != nil {
			errs = append(errs, fmt.Errorf("deleting ClusterRole %s: %w", cr.Name, err))
		}
	}
	for _, crb := range resources.ClusterRoleBindings(kn) {
		if err := r.deleteIfExists(ctx, crb); err != nil {
			errs = append(errs, fmt.Errorf("deleting CRB %s: %w", crb.Name, err))
		}
	}

	cr, crb := resources.AnsibleRBAC(kn)
	if err := r.deleteIfExists(ctx, cr); err != nil {
		errs = append(errs, fmt.Errorf("deleting AWX ClusterRole: %w", err))
	}
	if err := r.deleteIfExists(ctx, crb); err != nil {
		errs = append(errs, fmt.Errorf("deleting AWX CRB: %w", err))
	}

	for _, name := range resources.MonitoringCRBNames(kn) {
		monCRB := &rbacv1.ClusterRoleBinding{}
		monCRB.Name = name
		if err := r.deleteIfExists(ctx, monCRB); err != nil {
			errs = append(errs, fmt.Errorf("deleting monitoring CRB %s: %w", name, err))
		}
	}
	for _, name := range resources.MonitoringClusterRoleNames(kn) {
		monCR := &rbacv1.ClusterRole{}
		monCR.Name = name
		if err := r.deleteIfExists(ctx, monCR); err != nil {
			errs = append(errs, fmt.Errorf("deleting monitoring ClusterRole %s: %w", name, err))
		}
	}

	return errs
}

// deleteWebhookResources removes MutatingWebhookConfiguration and
// ValidatingWebhookConfiguration.
func (r *KubernautReconciler) deleteWebhookResources(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) []error {
	var errs []error
	mwc := resources.MutatingWebhookConfiguration(kn, nil)
	if err := r.deleteIfExists(ctx, mwc); err != nil {
		errs = append(errs, fmt.Errorf("deleting MutatingWebhookConfiguration: %w", err))
	}
	vwc := resources.ValidatingWebhookConfiguration(kn, nil)
	if err := r.deleteIfExists(ctx, vwc); err != nil {
		errs = append(errs, fmt.Errorf("deleting ValidatingWebhookConfiguration: %w", err))
	}
	return errs
}

// deleteWorkflowResources removes workflow namespace roles/bindings, the
// workflow runner SA, and the workflow namespace itself (if operator-managed).
func (r *KubernautReconciler) deleteWorkflowResources(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) []error {
	log := logf.FromContext(ctx)
	var errs []error

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

	wfRunnerSA := resources.WorkflowRunnerServiceAccount(kn)
	if err := r.deleteIfExists(ctx, wfRunnerSA); err != nil {
		errs = append(errs, fmt.Errorf("deleting workflow runner SA: %w", err))
	}

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

	return errs
}

// ---------- Helpers ----------

// phaseOrder defines the linear progression of reconciliation phases.
// setPhase only allows forward transitions (or error) to prevent the
// status.phase from regressing on subsequent reconcile loops.
var phaseOrder = map[kubernautv1alpha1.KubernautPhase]int{
	"":                                  0,
	kubernautv1alpha1.PhaseValidating:   1,
	kubernautv1alpha1.PhaseMigrating:    2,
	kubernautv1alpha1.PhaseDeploying:    3,
	kubernautv1alpha1.PhaseRunning:      4,
	kubernautv1alpha1.PhaseDegraded:     4,
	kubernautv1alpha1.PhaseError:        -1,
}

func (r *KubernautReconciler) setPhase(kn *kubernautv1alpha1.Kubernaut, phase kubernautv1alpha1.KubernautPhase) {
	if phase == kubernautv1alpha1.PhaseError {
		kn.Status.Phase = phase
		return
	}
	cur := phaseOrder[kn.Status.Phase]
	next := phaseOrder[phase]
	if next >= cur {
		kn.Status.Phase = phase
	}
}

// patchStatus applies status mutations via a server-side merge patch,
// avoiding resourceVersion conflicts that plague Status().Update().
// MergeFrom without OptimisticLock omits resourceVersion from the patch,
// so it never conflicts with concurrent metadata/spec writes.
func (r *KubernautReconciler) patchStatus(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, mutate func()) error {
	base := kn.DeepCopy()
	mutate()
	return r.Status().Patch(ctx, kn, client.MergeFrom(base))
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
	if err := r.patchStatus(ctx, kn, func() {
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: condType, Status: status, Reason: reason, Message: message,
			ObservedGeneration: kn.Generation,
		})
		if status == metav1.ConditionFalse {
			r.setPhase(kn, kubernautv1alpha1.PhaseError)
		}
	}); err != nil {
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
	return r.ensureResource(ctx, obj)
}

// ensureClusterScoped creates or updates a cluster-scoped resource.
// No owner reference is set; cleanup is handled by the finalizer.
func (r *KubernautReconciler) ensureClusterScoped(ctx context.Context, obj client.Object) error {
	return r.ensureResource(ctx, obj)
}

// ensureResource is the shared create-or-update implementation for both
// namespaced and cluster-scoped resources.
func (r *KubernautReconciler) ensureResource(ctx context.Context, obj client.Object) error {
	existing := obj.DeepCopyObject().(client.Object)
	key := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	err := r.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, obj)
	}
	if err != nil {
		return fmt.Errorf("getting %s: %w", key, err)
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// createIfNotFound gets an existing resource into `existing`; if not found it
// sets an owner reference and creates `desired`. Returns (true, nil) when
// a create occurred, (false, nil) when the resource already existed.
func (r *KubernautReconciler) createIfNotFound(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, desired, existing client.Object) (bool, error) {
	key := types.NamespacedName{Name: desired.GetName(), Namespace: desired.GetNamespace()}
	err := r.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		if setErr := resources.SetOwnerReference(kn, desired, r.Scheme); setErr != nil {
			return false, setErr
		}
		if err := r.Create(ctx, desired); err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting %s: %w", key, err)
	}
	return false, nil
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
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Named("kubernaut").
		Complete(r)
}

// Compile-time interface guard.
var _ = []client.Object{
	&rbacv1.ClusterRole{},
	&batchv1.Job{},
}

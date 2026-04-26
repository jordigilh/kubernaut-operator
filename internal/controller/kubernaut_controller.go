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

	configv1 "github.com/openshift/api/config/v1"
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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// Requeue intervals for different reconciliation states.
const (
	requeueMigrationPoll = 10 * time.Second
	requeueDegraded      = 15 * time.Second
	requeueError         = 30 * time.Second
	requeueRunning       = 60 * time.Second
)

// maxMigrationRetries caps the number of times a failed migration Job is
// deleted and re-created before the operator transitions to PhaseError.
const maxMigrationRetries = 10

// Condition reasons used in status patches.
const (
	ReasonSecretsValid        = "SecretsValid"
	ReasonCRDsReady           = "CRDsReady"
	ReasonMigrationComplete   = "MigrationComplete"
	ReasonMigrationFailed     = "MigrationFailed"
	ReasonMigrationInProgress = "MigrationInProgress"
	ReasonRBACReady           = "RBACReady"
	ReasonWebhooksReady       = "WebhooksReady"
	ReasonRouteCreated        = "RouteCreated"
	ReasonRouteDisabled       = "RouteDisabled"
	ReasonManifestsApplied    = "ManifestsApplied"
)

// maxFinalizerAttempts is the number of consecutive reconcile attempts during
// deletion cleanup before the finalizer is force-removed.
const maxFinalizerAttempts = 20

// KubernautReconciler reconciles a Kubernaut object.
type KubernautReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	RestCfg  *rest.Config
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
// +kubebuilder:rbac:groups=config.openshift.io,resources=apiservers,verbs=get;list;watch

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

	// Validate and migrate always run (cheap: secret checks + Job status).
	for _, phase := range []func(context.Context, *kubernautv1alpha1.Kubernaut) (ctrl.Result, error){
		r.phaseValidate,
		r.phaseMigrate,
	} {
		result, err := phase(ctx, kn)
		if err != nil || result.Requeue || result.RequeueAfter > 0 {
			return result, err
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(kn), kn); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Always run phaseDeploy — the spec-hash check inside ensureResource
	// short-circuits API writes when no drift is detected, making this cheap.
	result, err := r.phaseDeploy(ctx, kn)
	if err != nil || result.Requeue || result.RequeueAfter > 0 {
		return result, err
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(kn), kn); err != nil {
		return ctrl.Result{}, err
	}

	return r.phaseRunning(ctx, kn)
}

// ---------- Phase: Validate ----------

func (r *KubernautReconciler) phaseValidate(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if err := resources.ValidateHostname(kn.Spec.PostgreSQL.Host); err != nil {
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionBYOValidated, metav1.ConditionFalse,
			"PostgreSQLHostInvalid", fmt.Sprintf("PostgreSQL host validation failed: %v", err))
	}
	if err := resources.ValidateHostname(kn.Spec.Valkey.Host); err != nil {
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionBYOValidated, metav1.ConditionFalse,
			"ValkeyHostInvalid", fmt.Sprintf("Valkey host validation failed: %v", err))
	}

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
			Reason: ReasonSecretsValid, Message: "BYO PostgreSQL and Valkey secrets are valid",
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
	r.Recorder.Event(kn, corev1.EventTypeNormal, ReasonMigrationComplete, "Database migration job succeeded")
	return ctrl.Result{}, r.patchStatus(ctx, kn, func() {
		r.setPhase(kn, kubernautv1alpha1.PhaseDeploying)
		setCRDsReady(kn)
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionMigrationComplete, Status: metav1.ConditionTrue,
			Reason: ReasonMigrationComplete, Message: "Database migration job succeeded",
			ObservedGeneration: kn.Generation,
		})
	})
}

// ensureMigrationPrereqs installs CRDs, derives the DataStorage DB secret
// from the user-provided PostgreSQL secret, and ensures the migration ConfigMap.
func (r *KubernautReconciler) ensureMigrationPrereqs(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	if err := resources.EnsureCRDs(ctx, r.RestCfg); err != nil {
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
			if existingJob.Status.Failed >= int32(maxMigrationRetries) {
				log.Info("migration job exceeded retry limit", "failed", existingJob.Status.Failed, "max", maxMigrationRetries)
				return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionMigrationComplete, metav1.ConditionFalse,
					ReasonMigrationFailed, fmt.Sprintf("Database migration failed after %d attempts; manual intervention required", existingJob.Status.Failed))
			}
			log.Info("migration job failed, deleting for retry", "attempt", existingJob.Status.Failed)
			propagation := metav1.DeletePropagationBackground
			if err := r.Delete(ctx, existingJob, &client.DeleteOptions{
				PropagationPolicy: &propagation,
			}); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("deleting failed migration job: %w", err)
			}
			return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionMigrationComplete, metav1.ConditionFalse,
				ReasonMigrationFailed, "Database migration job failed; will retry")
		}
	}

	log.Info("waiting for migration job to complete")
	if err := r.patchStatus(ctx, kn, func() {
		r.setPhase(kn, kubernautv1alpha1.PhaseMigrating)
		setCRDsReady(kn)
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionMigrationComplete, Status: metav1.ConditionFalse,
			Reason: ReasonMigrationInProgress, Message: "Database migration job is running",
			ObservedGeneration: kn.Generation,
		})
	}); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueMigrationPoll}, nil
}

// setCRDsReady sets the ConditionCRDsInstalled condition to True on the
// in-memory Kubernaut object. Call within a patchStatus mutation closure.
func setCRDsReady(kn *kubernautv1alpha1.Kubernaut) {
	meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
		Type: kubernautv1alpha1.ConditionCRDsInstalled, Status: metav1.ConditionTrue,
		Reason: ReasonCRDsReady, Message: "All workload CRDs installed",
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

	pgSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: kn.Spec.PostgreSQL.SecretName}, pgSecret); err != nil {
		return ctrl.Result{}, fmt.Errorf("fetching pg secret for config: %w", err)
	}
	dbName := string(pgSecret.Data["POSTGRES_DB"])
	dbUser := string(pgSecret.Data["POSTGRES_USER"])

	tlsProfile := r.resolveClusterTLSProfile(ctx)

	cmHashes, err := r.deployConfigMaps(ctx, kn, dbName, dbUser, tlsProfile)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.deployAdmissionWebhooks(ctx, kn); err != nil {
		return ctrl.Result{}, err
	}
	hasRoute, err := r.deployWorkloads(ctx, kn, cmHashes)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.Recorder.Event(kn, corev1.EventTypeNormal, ReasonManifestsApplied, "All service manifests applied")
	return ctrl.Result{}, r.patchStatus(ctx, kn, func() {
		r.setPhase(kn, kubernautv1alpha1.PhaseDeploying)
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionRBACProvisioned, Status: metav1.ConditionTrue,
			Reason: ReasonRBACReady, Message: "All RBAC resources provisioned",
			ObservedGeneration: kn.Generation,
		})
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionWebhooksConfigured, Status: metav1.ConditionTrue,
			Reason: ReasonWebhooksReady, Message: "Admission webhooks configured",
			ObservedGeneration: kn.Generation,
		})
		if hasRoute {
			meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
				Type: kubernautv1alpha1.ConditionRouteReady, Status: metav1.ConditionTrue,
				Reason: ReasonRouteCreated, Message: "Gateway OCP Route created",
				ObservedGeneration: kn.Generation,
			})
		} else {
			meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
				Type: kubernautv1alpha1.ConditionRouteReady, Status: metav1.ConditionFalse,
				Reason: ReasonRouteDisabled, Message: "Gateway OCP Route is disabled",
				ObservedGeneration: kn.Generation,
			})
		}
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionServicesDeployed, Status: metav1.ConditionTrue,
			Reason: ReasonManifestsApplied, Message: "All service manifests applied",
			ObservedGeneration: kn.Generation,
		})
	})
}

func (r *KubernautReconciler) deployWorkflowNamespace(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	wfNs := resources.WorkflowNamespace(kn)
	existing := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: wfNs.Name}, existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("checking workflow namespace: %w", err)
		}
		if err := r.Create(ctx, wfNs); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating workflow namespace: %w", err)
		}
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
	if err := r.ensureUnowned(ctx, wfRunnerSA); err != nil {
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
// Roles/RoleBindings, DataStorage client bindings, and the Kubernaut Agent client binding.
func (r *KubernautReconciler) deployCoreRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	for _, cr := range resources.ClusterRoles(kn) {
		if err := r.ensureUnowned(ctx, cr); err != nil {
			return fmt.Errorf("ensuring ClusterRole %s: %w", cr.Name, err)
		}
	}
	for _, crb := range resources.ClusterRoleBindings(kn) {
		if err := r.ensureUnowned(ctx, crb); err != nil {
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
	return r.ensureNamespaced(ctx, kn, resources.KubernautAgentClientRoleBinding(kn))
}

// deployWorkflowRBAC provisions roles and bindings in the workflow namespace.
func (r *KubernautReconciler) deployWorkflowRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	wfRoles, wfRBs := resources.WorkflowNamespaceRBAC(kn)
	for _, role := range wfRoles {
		if err := r.ensureUnowned(ctx, role); err != nil {
			return fmt.Errorf("ensuring wf role %s: %w", role.Name, err)
		}
	}
	for _, rb := range wfRBs {
		if err := r.ensureUnowned(ctx, rb); err != nil {
			return fmt.Errorf("ensuring wf rb %s: %w", rb.Name, err)
		}
	}
	return nil
}

// deployToggleRBAC handles feature-flag-dependent RBAC: Ansible on/off and
// monitoring teardown when disabled. Cleanup errors are collected and returned
// so the reconcile loop retries (stale RBAC is a security concern).
func (r *KubernautReconciler) deployToggleRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	var errs []error

	cr, crb := resources.AnsibleRBAC(kn)
	if kn.Spec.Ansible.Enabled {
		if err := r.ensureUnowned(ctx, cr); err != nil {
			return fmt.Errorf("ensuring AWX ClusterRole: %w", err)
		}
		if err := r.ensureUnowned(ctx, crb); err != nil {
			return fmt.Errorf("ensuring AWX CRB: %w", err)
		}
	} else {
		if err := r.deleteIfExists(ctx, cr); err != nil {
			errs = append(errs, fmt.Errorf("removing stale AWX ClusterRole: %w", err))
		}
		if err := r.deleteIfExists(ctx, crb); err != nil {
			errs = append(errs, fmt.Errorf("removing stale AWX CRB: %w", err))
		}
	}

	if !kn.Spec.Monitoring.MonitoringEnabled() {
		for _, name := range resources.MonitoringCRBNames(kn) {
			monCRB := &rbacv1.ClusterRoleBinding{}
			monCRB.Name = name
			if err := r.deleteIfExists(ctx, monCRB); err != nil {
				errs = append(errs, fmt.Errorf("removing stale monitoring CRB %s: %w", name, err))
			}
		}
		for _, name := range resources.MonitoringClusterRoleNames(kn) {
			monCR := &rbacv1.ClusterRole{}
			monCR.Name = name
			if err := r.deleteIfExists(ctx, monCR); err != nil {
				errs = append(errs, fmt.Errorf("removing stale monitoring ClusterRole %s: %w", name, err))
			}
		}
		for _, name := range []string{"effectivenessmonitor-service-ca", "kubernaut-agent-service-ca"} {
			staleCM := &corev1.ConfigMap{}
			staleCM.Name = name
			staleCM.Namespace = kn.Namespace
			if err := r.deleteIfExists(ctx, staleCM); err != nil {
				errs = append(errs, fmt.Errorf("removing stale service-ca ConfigMap %s: %w", name, err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("toggle cleanup: %w", errors.Join(errs...))
	}
	return nil
}

// deployConfigMaps builds and ensures all service ConfigMaps. Returns a map
// of component name to SHA-256 hash of the ConfigMap data, used to stamp pod
// template annotations and force rolling restarts when config content changes.
func (r *KubernautReconciler) deployConfigMaps(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, dbName, dbUser, tlsProfile string) (map[string]string, error) {
	type cmBuilder struct {
		name string
		fn   func() (*corev1.ConfigMap, error)
	}
	tlsOpt := resources.WithTLSProfile(tlsProfile)
	builders := []cmBuilder{
		{"gateway", func() (*corev1.ConfigMap, error) { return resources.GatewayConfigMap(kn, tlsOpt) }},
		{"datastorage", func() (*corev1.ConfigMap, error) { return resources.DataStorageConfigMap(kn, dbName, dbUser, tlsOpt) }},
		{"aianalysis", func() (*corev1.ConfigMap, error) { return resources.AIAnalysisConfigMap(kn, tlsOpt) }},
		{"signalprocessing", func() (*corev1.ConfigMap, error) { return resources.SignalProcessingConfigMap(kn, tlsOpt) }},
		{"remediationorchestrator", func() (*corev1.ConfigMap, error) { return resources.RemediationOrchestratorConfigMap(kn, tlsOpt) }},
		{"workflowexecution", func() (*corev1.ConfigMap, error) { return resources.WorkflowExecutionConfigMap(kn, tlsOpt) }},
		{"effectivenessmonitor", func() (*corev1.ConfigMap, error) { return resources.EffectivenessMonitorConfigMap(kn, tlsOpt) }},
		{"notification-controller", func() (*corev1.ConfigMap, error) { return resources.NotificationControllerConfigMap(kn, tlsOpt) }},
		{"notification-routing", func() (*corev1.ConfigMap, error) { return resources.NotificationRoutingConfigMap(kn) }},
		{"kubernaut-agent", func() (*corev1.ConfigMap, error) { return resources.KubernautAgentConfigMap(kn, tlsOpt) }},
		{"authwebhook", func() (*corev1.ConfigMap, error) { return resources.AuthWebhookConfigMap(kn, tlsOpt) }},
	}

	cmHashes := make(map[string]string, len(builders))
	var configMaps []*corev1.ConfigMap
	for _, b := range builders {
		cm, err := b.fn()
		if err != nil {
			return nil, fmt.Errorf("building %s ConfigMap: %w", b.name, err)
		}
		configMaps = append(configMaps, cm)
		cmHashes[b.name] = resources.ConfigMapDataHash(cm.Data)
	}

	if cm := resources.AIAnalysisPoliciesConfigMap(kn); cm != nil {
		configMaps = append(configMaps, cm)
	}
	if cm := resources.SignalProcessingPolicyConfigMap(kn); cm != nil {
		configMaps = append(configMaps, cm)
	}
	if cm := resources.ProactiveSignalMappingsConfigMap(kn); cm != nil {
		configMaps = append(configMaps, cm)
	}
	sdkCM, err := resources.KubernautAgentSDKConfigMap(kn)
	if err != nil {
		return nil, fmt.Errorf("building kubernaut-agent-sdk ConfigMap: %w", err)
	}
	if sdkCM != nil {
		configMaps = append(configMaps, sdkCM)
	}
	configMaps = append(configMaps, resources.InterServiceCAConfigMap(kn))
	if kn.Spec.Monitoring.MonitoringEnabled() {
		configMaps = append(configMaps,
			resources.EffectivenessMonitorServiceCAConfigMap(kn),
			resources.KubernautAgentServiceCAConfigMap(kn),
		)
	}
	for _, cm := range configMaps {
		if err := r.ensureNamespaced(ctx, kn, cm); err != nil {
			return nil, fmt.Errorf("ensuring ConfigMap %s: %w", cm.Name, err)
		}
	}
	return cmHashes, nil
}

// deployAdmissionWebhooks ensures both MutatingWebhookConfiguration and
// ValidatingWebhookConfiguration. TLS is managed by OCP service-CA: the
// authwebhook-service annotation creates the authwebhook-tls Secret, and
// the inject-cabundle annotation on MWC/VWC injects the CA bundle.
func (r *KubernautReconciler) deployAdmissionWebhooks(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	mwc := resources.MutatingWebhookConfiguration(kn)
	if err := r.ensureUnowned(ctx, mwc); err != nil {
		return fmt.Errorf("ensuring MutatingWebhookConfiguration: %w", err)
	}
	vwc := resources.ValidatingWebhookConfiguration(kn)
	if err := r.ensureUnowned(ctx, vwc); err != nil {
		return fmt.Errorf("ensuring ValidatingWebhookConfiguration: %w", err)
	}
	return nil
}

// componentCMHashKey maps a deployment component name to its corresponding
// ConfigMap hash key (from deployConfigMaps).
var componentCMHashKey = map[string]string{
	resources.ComponentGateway:                 "gateway",
	resources.ComponentDataStorage:             "datastorage",
	resources.ComponentAIAnalysis:              "aianalysis",
	resources.ComponentSignalProcessing:        "signalprocessing",
	resources.ComponentRemediationOrchestrator: "remediationorchestrator",
	resources.ComponentWorkflowExecution:       "workflowexecution",
	resources.ComponentEffectivenessMonitor:    "effectivenessmonitor",
	resources.ComponentNotification:            "notification-controller",
	resources.ComponentKubernautAgent:          "kubernaut-agent",
	resources.ComponentAuthWebhook:             "authwebhook",
}

// deployWorkloads creates/updates deployments, services, PDBs, and the OCP
// route. cmHashes maps ConfigMap builder names to content hashes; these are
// stamped as pod template annotations to force rolling restarts when config
// content changes. Returns true if a route was created.
func (r *KubernautReconciler) deployWorkloads(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, cmHashes map[string]string) (hasRoute bool, _ error) {
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
		resources.KubernautAgentDeployment,
		resources.AuthWebhookDeployment,
	}
	for _, build := range depBuilders {
		dep, err := build(kn)
		if err != nil {
			return false, fmt.Errorf("building deployment: %w", err)
		}
		stampConfigMapHash(dep, cmHashes)
		if err := r.ensureNamespaced(ctx, kn, dep); err != nil {
			return false, fmt.Errorf("ensuring Deployment %s: %w", dep.Name, err)
		}
	}

	for _, svc := range resources.Services(kn) {
		if err := r.ensureNamespaced(ctx, kn, svc); err != nil {
			return false, fmt.Errorf("ensuring Service %s: %w", svc.Name, err)
		}
	}

	for _, svc := range resources.MetricsServices(kn) {
		if err := r.ensureNamespaced(ctx, kn, svc); err != nil {
			return false, fmt.Errorf("ensuring metrics Service %s: %w", svc.Name, err)
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
		if runtime.IsNotRegisteredError(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting stale Gateway Route: %w", err)
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
			Name:      resources.DeploymentName(component),
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
		return ctrl.Result{RequeueAfter: requeueDegraded}, nil
	}

	return ctrl.Result{RequeueAfter: requeueRunning}, nil
}

// ---------- Deletion ----------

func (r *KubernautReconciler) reconcileDelete(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("reconciling deletion")

	if controllerutil.ContainsFinalizer(kn, kubernautv1alpha1.FinalizerName) {
		if err := r.deleteClusterScopedResources(ctx, kn); err != nil {
			deletionAge := time.Since(kn.DeletionTimestamp.Time)
			if deletionAge > time.Duration(maxFinalizerAttempts)*requeueError {
				r.Recorder.Eventf(kn, corev1.EventTypeWarning, "FinalizerTimeout",
					"cleanup failed after %s; force-removing finalizer: %v", deletionAge.Round(time.Second), err)
				log.Error(err, "cleanup failed past timeout, force-removing finalizer")
			} else {
				return ctrl.Result{RequeueAfter: requeueError}, err
			}
		}
		r.Recorder.Event(kn, corev1.EventTypeNormal, "CleanupComplete", "Cluster-scoped resources cleaned up")

		controllerutil.RemoveFinalizer(kn, kubernautv1alpha1.FinalizerName)
		if err := r.Update(ctx, kn); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// NOTE: CRDs installed during migration are intentionally NOT deleted here.
// They are cluster-scoped and potentially shared across namespaces; removing
// them would destroy all CRs of those types cluster-wide.
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
	mwc := resources.MutatingWebhookConfiguration(kn)
	if err := r.deleteIfExists(ctx, mwc); err != nil {
		errs = append(errs, fmt.Errorf("deleting MutatingWebhookConfiguration: %w", err))
	}
	vwc := resources.ValidatingWebhookConfiguration(kn)
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
	if err := r.Get(ctx, types.NamespacedName{Name: wfNs.Name}, existingNs); err != nil {
		if !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("getting workflow namespace %s: %w", wfNs.Name, err))
		}
	} else {
		if existingNs.Annotations[resources.AnnotationCreatedBy] == "kubernaut-operator" {
			if err := r.deleteIfExists(ctx, existingNs); err != nil {
				errs = append(errs, fmt.Errorf("deleting workflow namespace %s: %w", wfNs.Name, err))
			}
		} else {
			log.Info("skipping deletion of workflow namespace (not created by operator)", "namespace", wfNs.Name)
		}
	}

	return errs
}

// ---------- Helpers ----------

// phaseOrder defines the linear progression of reconciliation phases.
// setPhase only allows forward transitions (or error) to prevent the
// status.phase from regressing on subsequent reconcile loops.
var phaseOrder = map[kubernautv1alpha1.KubernautPhase]int{
	"":                                0,
	kubernautv1alpha1.PhaseValidating: 1,
	kubernautv1alpha1.PhaseMigrating:  2,
	kubernautv1alpha1.PhaseDeploying:  3,
	kubernautv1alpha1.PhaseRunning:    4,
	kubernautv1alpha1.PhaseDegraded:   4,
	kubernautv1alpha1.PhaseError:      -1,
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
// Mutations are applied to a copy so that kn's in-memory state is only
// updated when the API call succeeds; a failed Patch does not leave kn
// in a divergent state.
func (r *KubernautReconciler) patchStatus(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, mutate func()) error {
	patched := kn.DeepCopy()
	mutate()
	if err := r.Status().Patch(ctx, kn, client.MergeFrom(patched)); err != nil {
		*kn = *patched
		return err
	}
	return nil
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
	return ctrl.Result{RequeueAfter: requeueError}, nil
}

// resolveClusterTLSProfile reads the OpenShift APIServer CR and maps the
// cluster-wide TLS security profile to a service-consumable profile name.
// Returns "" on non-OCP clusters or when the profile is unset.
func (r *KubernautReconciler) resolveClusterTLSProfile(ctx context.Context) string {
	log := logf.FromContext(ctx)
	apiServer := &configv1.APIServer{}
	if err := r.Get(ctx, client.ObjectKey{Name: "cluster"}, apiServer); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			log.V(1).Info("APIServer CR not found (non-OCP cluster), skipping TLS profile injection")
			return ""
		}
		log.V(1).Info("failed to read APIServer CR, skipping TLS profile injection", "error", err)
		return ""
	}
	profile := resources.MapTLSProfile(apiServer.Spec.TLSSecurityProfile)
	if profile != "" {
		log.V(1).Info("resolved cluster TLS profile", "profile", profile)
	}
	return profile
}

// ensureNamespaced creates or updates a namespaced resource, setting the
// Kubernaut CR as owner for garbage collection.
func (r *KubernautReconciler) ensureNamespaced(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, obj client.Object) error {
	if err := resources.SetOwnerReference(kn, obj, r.Scheme); err != nil {
		return err
	}
	return r.ensureResource(ctx, obj)
}

// ensureUnowned creates or updates a resource without setting an owner
// reference. Used for cluster-scoped resources (ClusterRoles, CRBs, webhooks)
// and cross-namespace resources (workflow Roles/RoleBindings) where
// OwnerReferences cannot be used. Cleanup is handled by the finalizer.
func (r *KubernautReconciler) ensureUnowned(ctx context.Context, obj client.Object) error {
	return r.ensureResource(ctx, obj)
}

// ensureResource is the shared create-or-update implementation for both
// namespaced and cluster-scoped resources. It stamps a spec-hash annotation
// on the desired object and compares it with the live object to skip
// unnecessary API server writes. AlreadyExists on Create is handled by
// falling through to the update path.
func (r *KubernautReconciler) ensureResource(ctx context.Context, obj client.Object) error {
	desiredHash := resources.SpecHash(obj)
	setHashAnnotation(obj, desiredHash)

	existing := obj.DeepCopyObject().(client.Object)
	key := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	err := r.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		if createErr := r.Create(ctx, obj); createErr != nil {
			if !apierrors.IsAlreadyExists(createErr) {
				return createErr
			}
			if err := r.Get(ctx, key, existing); err != nil {
				return fmt.Errorf("getting %s after AlreadyExists: %w", key, err)
			}
		} else {
			return nil
		}
	} else if err != nil {
		return fmt.Errorf("getting %s: %w", key, err)
	}

	if existing.GetAnnotations()[resources.AnnotationSpecHash] == desiredHash {
		return nil
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// setHashAnnotation merges the spec-hash annotation into the object's
// existing annotations without clobbering others.
// stampConfigMapHash looks up the component name from the Deployment's labels
// and stamps the corresponding ConfigMap content hash as a pod template
// annotation. This forces Kubernetes to roll out new pods when config changes.
func stampConfigMapHash(dep *appsv1.Deployment, cmHashes map[string]string) {
	component := dep.Spec.Template.Labels["app"]
	hashKey, ok := componentCMHashKey[component]
	if !ok {
		return
	}
	hash, ok := cmHashes[hashKey]
	if !ok {
		return
	}
	a := dep.Spec.Template.Annotations
	if a == nil {
		a = make(map[string]string, 1)
	}
	a[resources.AnnotationConfigMapHash] = hash
	dep.Spec.Template.Annotations = a
}

func setHashAnnotation(obj client.Object, hash string) {
	a := obj.GetAnnotations()
	if a == nil {
		a = make(map[string]string, 1)
	}
	a[resources.AnnotationSpecHash] = hash
	obj.SetAnnotations(a)
}

// createIfNotFound gets an existing resource into `existing`; if not found it
// sets an owner reference and creates `desired`. Returns (true, nil) when
// a create occurred, (false, nil) when the resource already existed.
// AlreadyExists from a concurrent create is treated as success.
func (r *KubernautReconciler) createIfNotFound(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, desired, existing client.Object) (bool, error) {
	key := types.NamespacedName{Name: desired.GetName(), Namespace: desired.GetNamespace()}
	err := r.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		if setErr := resources.SetOwnerReference(kn, desired, r.Scheme); setErr != nil {
			return false, setErr
		}
		if err := r.Create(ctx, desired); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return false, nil
			}
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
	r.Recorder = mgr.GetEventRecorderFor("kubernaut-controller")
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
		Watches(&configv1.APIServer{},
			handler.EnqueueRequestsFromMapFunc(r.apiServerToKubernaut)).
		Named("kubernaut").
		Complete(r)
}

// apiServerToKubernaut maps APIServer CR changes to the singleton Kubernaut
// reconcile request so that TLS profile changes trigger a config update.
func (r *KubernautReconciler) apiServerToKubernaut(ctx context.Context, _ client.Object) []reconcile.Request {
	list := &kubernautv1alpha1.KubernautList{}
	if err := r.List(ctx, list); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for _, kn := range list.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: kn.Name, Namespace: kn.Namespace},
		})
	}
	return reqs
}

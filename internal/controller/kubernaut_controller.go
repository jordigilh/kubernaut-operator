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
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"golang.org/x/time/rate"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	sigsyaml "sigs.k8s.io/yaml"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
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

	ReasonAnsibleDisabled        = "Disabled"
	ReasonAnsibleReady           = "Ready"
	ReasonAnsibleTokenNotFound   = "TokenSecretNotFound"
	ReasonAnsibleTokenKeyMissing = "TokenKeyMissing"

	ReasonRBACApplyFailed            = "RBACApplyFailed"
	ReasonAdditionalRBACFullyBound   = "FullyBound"
	ReasonAdditionalRBACPartialBound = "PartiallyBound"

	ReasonOIDCAutoDetected    = "OIDCAutoDetected"
	ReasonOIDCDetectionFailed = "OIDCDetectionFailed"

	ReasonAlertManagerAuthReady           = "Ready"
	ReasonAlertManagerAuthNotConfigured   = "SecretNameNotConfigured"
	ReasonAlertManagerAuthSecretMissing   = "TokenSecretNotFound"
	ReasonAlertManagerAuthKeyMissing      = "TokenKeyMissing"
	ReasonAlertManagerAuthGatewayDisabled = "GatewayDisabled"
)

// kagentiAuthbridgeConfigMapName is the well-known ConfigMap the kagenti
// operator maintains in kagenti-system with Keycloak/OIDC settings.
const kagentiAuthbridgeConfigMapName = "authbridge-config"

// kagentiSystemNamespace is the namespace where the kagenti operator runs.
const kagentiSystemNamespace = "kagenti-system"

// maxFinalizerAttempts is the number of consecutive reconcile attempts during
// deletion cleanup before the finalizer is force-removed.
const maxFinalizerAttempts = 20

// KubernautReconciler reconciles a Kubernaut object.
type KubernautReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
	RestCfg  *rest.Config
	now      func() time.Time
}

// +kubebuilder:rbac:groups=kubernaut.ai,resources=kubernauts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubernaut.ai,resources=kubernauts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubernaut.ai,resources=kubernauts/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;secrets;serviceaccounts;namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=endpoints,resourceNames=kubernetes,verbs=get
// +kubebuilder:rbac:groups="",resources=configmaps,resourceNames=default-ingress-cert,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete;escalate;bind
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes/custom-host,verbs=create;update
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=config.openshift.io,resources=apiservers;ingresses,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules;alertmanagerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=spire.spiffe.io,resources=clusterspiffeids,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.kagenti.dev,resources=agentruntimes,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the main reconciliation loop for the Kubernaut singleton CR.
//
// The blank line and this doc comment above are required, not stylistic: Go
// attaches an unbroken comment block directly above a func as that func's
// GoDoc, and controller-gen's marker-association logic only treats markers
// as package-level (which +kubebuilder:rbac markers must be) for specific
// node kinds (GenDecl/TypeSpec/Field/File) or "floating" comments -- a
// FuncDecl's GoDoc isn't one of them, so a marker block glued directly to
// this function's declaration is silently dropped by `make manifests`
// (verified: 0 RBAC rules generated without this separation, 231 with it).
func (r *KubernautReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fleet v1alpha2 migration: fetch the v1alpha2 hub/storage version once,
	// then derive the v1alpha1 view in-memory via the same ConvertFrom logic
	// the conversion webhook uses (already unit-tested) instead of a second
	// network round-trip. knV2 is threaded alongside kn wherever Fleet's
	// spec fields are needed -- Fleet's entire CRD surface lives in
	// v1alpha2.
	knV2 := &kubernautv1alpha2.Kubernaut{}
	if err := r.Get(ctx, req.NamespacedName, knV2); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	kn := &kubernautv1alpha1.Kubernaut{}
	if err := kn.ConvertFrom(knV2); err != nil {
		return ctrl.Result{}, fmt.Errorf("deriving v1alpha1 view from v1alpha2: %w", err)
	}

	if kn.Name != kubernautv1alpha1.SingletonName {
		log.Info("ignoring CR with unexpected name", "name", kn.Name, "expected", kubernautv1alpha1.SingletonName)
		return ctrl.Result{}, nil
	}

	if !kn.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, kn, knV2)
	}

	if !controllerutil.ContainsFinalizer(kn, kubernautv1alpha1.FinalizerName) {
		// Fleet v1alpha2 migration: a full (non-status-subresource) Update
		// must go through knV2, not kn. The conversion webhook's ConvertTo
		// (v1alpha1 -> v1alpha2) has no v1alpha1 source for Fleet/
		// FleetMetadataCache, so it necessarily zeroes them; updating via
		// the v1alpha1 view would silently wipe any Fleet config already
		// stored in v1alpha2. Writing via knV2 (the hub/storage version)
		// round-trips losslessly.
		controllerutil.AddFinalizer(knV2, kubernautv1alpha1.FinalizerName)
		return ctrl.Result{}, r.Update(ctx, knV2)
	}

	return r.reconcilePhases(ctx, kn, knV2)
}

func (r *KubernautReconciler) reconcilePhases(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("reconciling phases", "currentPhase", kn.Status.Phase)

	// Validate and migrate always run (cheap: secret checks + Job status).
	for _, phase := range []func(context.Context, *kubernautv1alpha1.Kubernaut) (ctrl.Result, error){
		func(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
			return r.phaseValidate(ctx, kn, knV2)
		},
		r.phaseMigrate,
	} {
		result, err := phase(ctx, kn)
		if err != nil || result.RequeueAfter > 0 {
			return result, err
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(kn), kn); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Always run phaseDeploy — the spec-hash check inside ensureResource
	// short-circuits API writes when no drift is detected, making this cheap.
	if err := r.phaseDeploy(ctx, kn, knV2); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(kn), kn); err != nil {
		return ctrl.Result{}, err
	}

	return r.phaseRunning(ctx, kn, knV2)
}

// ---------- Phase: Validate ----------

func (r *KubernautReconciler) phaseValidate(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if err := resources.ValidateHostname(kn.Spec.PostgreSQL.Host); err != nil {
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionBYOValidated,
			"PostgreSQLHostInvalid", fmt.Sprintf("PostgreSQL host validation failed: %v", err))
	}
	if err := resources.ValidateHostname(kn.Spec.Valkey.Host); err != nil {
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionBYOValidated,
			"ValkeyHostInvalid", fmt.Sprintf("Valkey host validation failed: %v", err))
	}

	if err := r.validateSecret(ctx, kn.Namespace, kn.Spec.PostgreSQL.SecretName,
		[]string{"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB"}); err != nil {
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionBYOValidated,
			"PostgreSQLSecretInvalid", fmt.Sprintf("PostgreSQL secret validation failed: %v", err))
	}

	if err := r.validateSecret(ctx, kn.Namespace, kn.Spec.Valkey.SecretName,
		[]string{"valkey-secrets.yaml"}); err != nil {
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionBYOValidated,
			"ValkeySecretInvalid", fmt.Sprintf("Valkey secret validation failed: %v", err))
	}

	sidecar := r.detectKagentiSidecarMode(ctx, kn)
	validationErrs := resources.ValidateKubernaut(kn, sidecar)
	validationErrs = append(validationErrs, resources.ValidateFleet(knV2)...)
	if len(validationErrs) > 0 {
		msgs := make([]string, len(validationErrs))
		for i, e := range validationErrs {
			msgs[i] = e.Error()
		}
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionBYOValidated,
			"SpecValidationFailed", fmt.Sprintf("CR validation failed: %s", strings.Join(msgs, "; ")))
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
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionCRDsInstalled,
			"CRDInstallFailed", err.Error())
	}

	result, err := r.ensureMigrationJob(ctx, kn)
	if err != nil || result.RequeueAfter > 0 {
		return result, err
	}

	log := logf.FromContext(ctx)
	log.Info("database migration completed")
	r.Recorder.Eventf(kn, nil, corev1.EventTypeNormal, ReasonMigrationComplete, "Reconcile", "Database migration job succeeded")
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
	if err := r.ensureNamespaced(ctx, kn, migrationCM); err != nil {
		return fmt.Errorf("ensuring migration configmap: %w", err)
	}

	sslMode := kn.Spec.PostgreSQL.SSLMode
	if sslMode == "" {
		sslMode = resources.DefaultSSLMode
	}
	if sslMode == resources.DefaultSSLMode {
		caCM := resources.InterServiceCAConfigMap(kn)
		if err := r.ensureNamespaced(ctx, kn, caCM); err != nil {
			return fmt.Errorf("ensuring inter-service-ca configmap: %w", err)
		}
	}
	return nil
}

// ensureMigrationJob creates the migration Job if absent, then checks its
// status. Returns a zero Result when the job has completed successfully;
// returns a non-zero Result (requeue) when the job is still running or failed.
//
// A completed Job with a matching spec-hash annotation is considered
// up-to-date and short-circuits the entire migration phase, avoiding
// unnecessary pod churn on operator restarts.
func (r *KubernautReconciler) ensureMigrationJob(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	migrationJob, err := resources.MigrationJob(kn)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building migration job: %w", err)
	}
	desiredHash := resources.SpecHash(migrationJob)
	setHashAnnotation(migrationJob, desiredHash)

	if kn.Status.LastMigrationHash == desiredHash {
		return ctrl.Result{}, nil
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
			return r.handleCompleteMigrationJob(ctx, kn, existingJob, desiredHash)
		}
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return r.handleFailedMigrationJob(ctx, kn, existingJob)
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

// handleCompleteMigrationJob processes a migration Job that has reached
// JobComplete. If its spec-hash annotation still matches the desired hash,
// migration status is recorded as done; otherwise the stale job is deleted
// so a fresh one gets created on the next reconcile.
func (r *KubernautReconciler) handleCompleteMigrationJob(
	ctx context.Context, kn *kubernautv1alpha1.Kubernaut, existingJob *batchv1.Job, desiredHash string,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if existingJob.GetAnnotations()[resources.AnnotationSpecHash] == desiredHash {
		now := metav1.Now()
		return ctrl.Result{}, r.patchStatus(ctx, kn, func() {
			kn.Status.LastMigrationHash = desiredHash
			kn.Status.LastMigrationTime = &now
		})
	}

	log.Info("completed migration job has stale spec-hash, deleting for re-run")
	propagation := metav1.DeletePropagationBackground
	if err := r.Delete(ctx, existingJob, &client.DeleteOptions{
		PropagationPolicy: &propagation,
	}); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("deleting stale migration job: %w", err)
	}
	return ctrl.Result{RequeueAfter: time.Millisecond}, nil
}

// handleFailedMigrationJob processes a migration Job that has reached
// JobFailed. Once the retry limit is exceeded, migration is marked failed
// requiring manual intervention; otherwise the failed job is deleted so a
// fresh attempt is created on the next reconcile.
func (r *KubernautReconciler) handleFailedMigrationJob(
	ctx context.Context, kn *kubernautv1alpha1.Kubernaut, existingJob *batchv1.Job,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if existingJob.Status.Failed >= int32(maxMigrationRetries) {
		log.Info("migration job exceeded retry limit", "failed", existingJob.Status.Failed, "max", maxMigrationRetries)
		return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionMigrationComplete,
			ReasonMigrationFailed, fmt.Sprintf("Database migration failed after %d attempts; manual intervention required", existingJob.Status.Failed))
	}

	log.Info("migration job failed, deleting for retry", "attempt", existingJob.Status.Failed)
	propagation := metav1.DeletePropagationBackground
	if err := r.Delete(ctx, existingJob, &client.DeleteOptions{
		PropagationPolicy: &propagation,
	}); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("deleting failed migration job: %w", err)
	}
	return r.setConditionAndRequeue(ctx, kn, kubernautv1alpha1.ConditionMigrationComplete,
		ReasonMigrationFailed, "Database migration job failed; will retry")
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

func (r *KubernautReconciler) phaseDeploy(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) error {
	if err := r.deployWorkflowNamespace(ctx, kn); err != nil {
		return err
	}
	if err := r.deployServiceAccounts(ctx, kn); err != nil {
		return err
	}
	if err := r.deployRBAC(ctx, kn, knV2); err != nil {
		return r.handleRBACDeployError(ctx, kn, err)
	}

	pgSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: kn.Spec.PostgreSQL.SecretName}, pgSecret); err != nil {
		return fmt.Errorf("fetching pg secret for config: %w", err)
	}
	dbName := string(pgSecret.Data["POSTGRES_DB"])
	dbUser := string(pgSecret.Data["POSTGRES_USER"])

	tlsProfile := r.resolveClusterTLSProfile(ctx)

	sidecar := r.detectKagentiSidecarMode(ctx, kn)

	oidcDefaults, err := r.resolveKagentiOIDCDefaults(ctx, kn, sidecar)
	if err != nil {
		return r.handleOIDCDetectionError(ctx, kn, err)
	}

	cmHashes, err := r.deployConfigMaps(ctx, kn, knV2, dbName, dbUser, tlsProfile, sidecar, oidcDefaults)
	if err != nil {
		return err
	}
	if err := r.deployAdmissionWebhooks(ctx, kn); err != nil {
		return err
	}
	if err := r.ensureKagentiNamespaceLabel(ctx, kn); err != nil {
		return err
	}
	if err := r.ensureAgentRuntimeCR(ctx, kn, sidecar); err != nil {
		return err
	}
	if err := r.ensureAuthbridgeMetricsBypass(ctx, kn, sidecar); err != nil {
		return err
	}
	if err := r.ensureAuthbridgeClientID(ctx, kn, sidecar); err != nil {
		return err
	}
	hasRoute, err := r.deployWorkloads(ctx, kn, knV2, cmHashes, sidecar)
	if err != nil {
		return err
	}

	r.Recorder.Eventf(kn, nil, corev1.EventTypeNormal, ReasonManifestsApplied, "Reconcile", "All service manifests applied")
	return r.finalizeDeployStatus(ctx, kn, hasRoute)
}

// handleRBACDeployError records a failed-RBAC status condition and a
// warning event, then returns the original error for the caller to
// propagate. Extracted from phaseDeploy to keep its cyclomatic complexity
// within threshold.
func (r *KubernautReconciler) handleRBACDeployError(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, err error) error {
	if statusErr := r.patchStatus(ctx, kn, func() {
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionRBACProvisioned, Status: metav1.ConditionFalse,
			Reason: ReasonRBACApplyFailed, Message: err.Error(),
			ObservedGeneration: kn.Generation,
		})
	}); statusErr != nil {
		logf.FromContext(ctx).Error(statusErr, "failed to patch RBAC status condition")
	}
	r.Recorder.Eventf(kn, nil, corev1.EventTypeWarning, ReasonRBACApplyFailed, "Reconcile",
		"Failed to provision RBAC: %v", err)
	return err
}

// handleOIDCDetectionError records a failed-BYO-OIDC status condition, then
// returns a wrapped error for the caller to propagate. Extracted from
// phaseDeploy to keep its cyclomatic complexity within threshold.
func (r *KubernautReconciler) handleOIDCDetectionError(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, err error) error {
	if statusErr := r.patchStatus(ctx, kn, func() {
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: kubernautv1alpha1.ConditionBYOValidated, Status: metav1.ConditionFalse,
			Reason: ReasonOIDCDetectionFailed, Message: err.Error(),
			ObservedGeneration: kn.Generation,
		})
	}); statusErr != nil {
		logf.FromContext(ctx).Error(statusErr, "failed to patch OIDC detection status")
	}
	return fmt.Errorf("resolving kagenti OIDC defaults: %w", err)
}

// finalizeDeployStatus records the terminal Deploying-phase status
// conditions (RBAC, webhooks, route, services) once all deploy sub-steps
// have succeeded. Extracted from phaseDeploy to keep its cyclomatic
// complexity within threshold.
func (r *KubernautReconciler) finalizeDeployStatus(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, hasRoute bool) error {
	return r.patchStatus(ctx, kn, func() {
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
		routeCondition := metav1.Condition{
			Type: kubernautv1alpha1.ConditionRouteReady, Status: metav1.ConditionFalse,
			Reason: ReasonRouteDisabled, Message: "Gateway OCP Route is disabled",
			ObservedGeneration: kn.Generation,
		}
		if hasRoute {
			routeCondition.Status = metav1.ConditionTrue
			routeCondition.Reason = ReasonRouteCreated
			routeCondition.Message = "Gateway OCP Route created"
		}
		meta.SetStatusCondition(&kn.Status.Conditions, routeCondition)
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
		return nil
	}

	// #208: converge a pre-existing kubernaut-workflows namespace (created
	// by an older operator version, before this defense-in-depth backstop
	// existed) to carry the restricted PSA labels too, not just namespaces
	// created after this upgrade.
	if existing.Labels == nil {
		existing.Labels = make(map[string]string)
	}
	if resources.EnsureRestrictedPSALabels(existing.Labels) {
		logf.FromContext(ctx).Info("setting restricted pod security labels on workflow namespace", "namespace", existing.Name)
		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("patching restricted PSA labels on workflow namespace: %w", err)
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

func (r *KubernautReconciler) deployRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) error {
	if err := r.deployCoreRBAC(ctx, kn, knV2); err != nil {
		return err
	}
	if err := r.deployWorkflowRBAC(ctx, kn); err != nil {
		return err
	}
	if err := r.deployToggleRBAC(ctx, kn); err != nil {
		return err
	}
	if err := r.deployToolRBAC(ctx, kn); err != nil {
		return err
	}
	return r.deployConsoleAccessRBAC(ctx, kn)
}

// deployConsoleAccessRBAC ensures the console-access ClusterRoleBinding
// reflects the current effective group list (#289). The ClusterRole itself
// rides the ClusterRoles(kn) aggregator via deployCoreRBAC and needs no
// dedicated handling here. Unlike the multi-name tool CRBs, this CRB has a
// single static name, so a plain ensure-or-delete on every reconcile is
// sufficient to handle the "groups shrunk to zero / explicit opt-out"
// transition -- no status-field pruning list is needed.
func (r *KubernautReconciler) deployConsoleAccessRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	crb := resources.ConsoleAccessClusterRoleBinding(kn)
	if crb == nil {
		staleCRB := &rbacv1.ClusterRoleBinding{}
		staleCRB.Name = resources.ConsoleAccessCRBName(kn)
		if err := r.deleteIfExists(ctx, staleCRB); err != nil {
			return fmt.Errorf("deleting console-access CRB: %w", err)
		}
		return nil
	}
	if err := r.ensureUnowned(ctx, crb); err != nil {
		return fmt.Errorf("ensuring console-access CRB %s: %w", crb.Name, err)
	}
	return nil
}

// deployCoreRBAC provisions ClusterRoles, ClusterRoleBindings, namespace-scoped
// Roles/RoleBindings, DataStorage client bindings, and the Kubernaut Agent client binding.
func (r *KubernautReconciler) deployCoreRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) error {
	if err := r.deployClusterScopedCoreRBAC(ctx, kn, knV2); err != nil {
		return err
	}
	if err := r.deployNamespacedCoreRBAC(ctx, kn, knV2); err != nil {
		return err
	}
	if err := r.deployMCPGatewayNamespaceRBAC(ctx, kn, knV2); err != nil {
		return err
	}
	return r.deployAdditionalComponentRBAC(ctx, kn, knV2)
}

// deployClusterScopedCoreRBAC ensures the always-computed ClusterRoles/
// ClusterRoleBindings and prunes any orphaned by a feature toggle-off
// (#341) in the same pass.
func (r *KubernautReconciler) deployClusterScopedCoreRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) error {
	desiredCRs := resources.ClusterRoles(kn, knV2)
	for _, cr := range desiredCRs {
		if err := r.ensureUnowned(ctx, cr); err != nil {
			return fmt.Errorf("ensuring ClusterRole %s: %w", cr.Name, err)
		}
	}
	desiredCRBs := resources.ClusterRoleBindings(kn, knV2)
	for _, crb := range desiredCRBs {
		if err := r.ensureUnowned(ctx, crb); err != nil {
			return fmt.Errorf("ensuring CRB %s: %w", crb.Name, err)
		}
	}
	if errs := r.pruneOrphanedCoreClusterRBAC(ctx, kn, desiredCRs, desiredCRBs); len(errs) > 0 {
		return fmt.Errorf("pruning orphaned core cluster RBAC: %w", errors.Join(errs...))
	}
	return nil
}

// deployNamespacedCoreRBAC ensures the namespace-scoped Roles/RoleBindings
// and DataStorage/KubernautAgent client bindings.
func (r *KubernautReconciler) deployNamespacedCoreRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) error {
	for _, role := range resources.NamespaceRoles(kn, knV2) {
		if err := r.ensureNamespaced(ctx, kn, role); err != nil {
			return fmt.Errorf("ensuring ns role %s: %w", role.Name, err)
		}
	}
	for _, rb := range resources.NamespaceRoleBindings(kn, knV2) {
		if err := r.ensureNamespaced(ctx, kn, rb); err != nil {
			return fmt.Errorf("ensuring ns rolebinding %s: %w", rb.Name, err)
		}
	}
	for _, rb := range resources.DataStorageClientRoleBindings(kn) {
		if err := r.ensureNamespaced(ctx, kn, rb); err != nil {
			return fmt.Errorf("ensuring ds client rb %s: %w", rb.Name, err)
		}
	}
	if err := r.ensureNamespaced(ctx, kn, resources.KubernautAgentClientRoleBinding(kn)); err != nil {
		return err
	}
	if kn.Spec.APIFrontendEnabled() {
		if err := r.ensureNamespaced(ctx, kn, resources.KubernautAgentClientAPIfrontendRoleBinding(kn)); err != nil {
			return err
		}
	}
	return nil
}

// namesOf returns the Name of every object in objs as a lookup set, used to
// build the "desired" side of a label-selector prune diff.
func namesOf[T client.Object](objs []T) map[string]bool {
	names := make(map[string]bool, len(objs))
	for _, o := range objs {
		names[o.GetName()] = true
	}
	return names
}

// pointersOf converts a value slice -- as returned by a
// client.ObjectList's .Items field, e.g. rbacv1.ClusterRoleList.Items -- into
// a slice of pointers, since only the pointer type implements client.Object.
func pointersOf[T any](items []T) []*T {
	out := make([]*T, len(items))
	for i := range items {
		out[i] = &items[i]
	}
	return out
}

// pruneUndesired deletes every element of live whose name is not present in
// desiredNames, logging each successful deletion (object kind + name, per
// AGENTS.md's operator audit-trace requirement -- deleteIfExists itself is
// silent) and returning one wrapped error per failed deletion. kind is used
// for both the log/error context and is expected to be a stable, human
// -readable object-kind label (e.g. "ClusterRole"). Shared by
// pruneOrphanedCoreClusterRBAC (#341) and
// pruneOrphanedAdditionalComponentRBAC (#277) so the list-diff-delete shape
// is defined once regardless of which RBAC object kind is being pruned.
func pruneUndesired[T client.Object](r *KubernautReconciler, ctx context.Context, kind string, live []T, desiredNames map[string]bool) []error {
	log := logf.FromContext(ctx)
	var errs []error
	for _, obj := range live {
		if desiredNames[obj.GetName()] {
			continue
		}
		if err := r.deleteIfExists(ctx, obj); err != nil {
			errs = append(errs, fmt.Errorf("pruning orphaned %s %s: %w", kind, obj.GetName(), err))
			continue
		}
		log.Info("pruned orphaned RBAC object", "kind", kind, "name", obj.GetName())
	}
	return errs
}

// pruneOrphanedCoreClusterRBAC deletes any ClusterRole/ClusterRoleBinding
// carrying LabelCoreClusterRBAC for this instance that is not present in
// desiredCRs/desiredCRBs (#341). It replaces a family of dedicated,
// static-name delete functions that each had to be kept in sync by hand
// whenever a conditionally-gated entry was added to
// resources.ClusterRoles()/ClusterRoleBindings() -- FMC's #1993
// auth-middleware/scope-check-client pair shipped without one, leaking on
// disable. A single label-selector diff instead closes the gap uniformly
// for every current and future entry in those two aggregators.
//
// Passing nil/empty desired slices (as the finalizer path does via
// deleteCoreClusterRBAC) prunes every labeled object unconditionally,
// regardless of the instance's current spec -- appropriate when the
// instance itself is being deleted.
func (r *KubernautReconciler) pruneOrphanedCoreClusterRBAC(
	ctx context.Context, kn *kubernautv1alpha1.Kubernaut,
	desiredCRs []*rbacv1.ClusterRole, desiredCRBs []*rbacv1.ClusterRoleBinding,
) []error {
	var errs []error
	selector := client.MatchingLabels{
		resources.LabelCoreClusterRBAC: resources.LabelValueTrue,
		"app.kubernetes.io/instance":   kn.Name,
	}

	liveCRs := &rbacv1.ClusterRoleList{}
	if err := r.List(ctx, liveCRs, selector); err != nil {
		return append(errs, fmt.Errorf("listing core ClusterRoles for pruning: %w", err))
	}
	errs = append(errs, pruneUndesired(r, ctx, "ClusterRole", pointersOf(liveCRs.Items), namesOf(desiredCRs))...)

	liveCRBs := &rbacv1.ClusterRoleBindingList{}
	if err := r.List(ctx, liveCRBs, selector); err != nil {
		return append(errs, fmt.Errorf("listing core ClusterRoleBindings for pruning: %w", err))
	}
	errs = append(errs, pruneUndesired(r, ctx, "ClusterRoleBinding", pointersOf(liveCRBs.Items), namesOf(desiredCRBs))...)

	return errs
}

// deployMCPGatewayNamespaceRBAC provisions the namespace-scoped Roles/
// RoleBindings for FMC/SP/AF/EM's MCP Gateway CRD reads (#224 Finding 5,
// extended to AF/EM by #227). FMC's cluster-scoped ClusterRole/
// ClusterRoleBinding naturally drop out of
// resources.ClusterRoles()/ClusterRoleBindings() once its effective
// mcpGatewayNamespace resolves (the entire ClusterRole is MCP-Gateway-only),
// and deployCoreRBAC's generic prune (#341) removes the now-stale objects in
// the same reconcile -- no dedicated delete is needed here. SP/AF/EM's
// ClusterRoles are always present (they carry unconditional core rules
// too), so ensureUnowned naturally drops their MCP Gateway rules in place
// once each component's effective namespace resolves.
//
// #341's name-only prune isn't sufficient for these namespace-scoped
// objects: the same role name (e.g. "<instance>-fleetmetadatacache-
// mcpgateway") is expected to exist in a *different* namespace after an
// administrator changes the effective mcpGatewayNamespace, so
// pruneOrphanedMCPGatewayNamespaceRBAC (#354) diffs by (namespace, name)
// instead of name alone.
func (r *KubernautReconciler) deployMCPGatewayNamespaceRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) error {
	roles, rbs := resources.MCPGatewayNamespaceRBAC(kn, knV2)
	for _, role := range roles {
		if err := r.ensureUnowned(ctx, role); err != nil {
			return fmt.Errorf("ensuring mcp gateway namespace role %s/%s: %w", role.Namespace, role.Name, err)
		}
	}
	for _, rb := range rbs {
		if err := r.ensureUnowned(ctx, rb); err != nil {
			return fmt.Errorf("ensuring mcp gateway namespace rolebinding %s/%s: %w", rb.Namespace, rb.Name, err)
		}
	}

	if errs := r.pruneOrphanedMCPGatewayNamespaceRBAC(ctx, kn, roles, rbs); len(errs) > 0 {
		return fmt.Errorf("pruning orphaned mcp gateway namespace RBAC: %w", errors.Join(errs...))
	}

	return nil
}

// namespacedKeysOf returns the (namespace, name) of every object in objs as
// a lookup set, used to build the "desired" side of a namespace-aware
// label-selector prune diff (#354) -- unlike namesOf, this distinguishes a
// same-named object across different namespaces, since a role name
// legitimately recurs in a new namespace after an mcpGatewayNamespace
// change while the old namespace's copy becomes orphaned.
func namespacedKeysOf[T client.Object](objs []T) map[types.NamespacedName]bool {
	keys := make(map[types.NamespacedName]bool, len(objs))
	for _, o := range objs {
		keys[types.NamespacedName{Namespace: o.GetNamespace(), Name: o.GetName()}] = true
	}
	return keys
}

// pruneUndesiredNamespaced deletes every element of live whose (namespace,
// name) is not present in desiredKeys, mirroring pruneUndesired's logging
// and error-wrapping shape (#341) but keyed on the full NamespacedName so a
// same-named object in a since-abandoned namespace is correctly treated as
// orphaned even though an object with the same name is still desired
// elsewhere.
func pruneUndesiredNamespaced[T client.Object](r *KubernautReconciler, ctx context.Context, kind string, live []T, desiredKeys map[types.NamespacedName]bool) []error {
	log := logf.FromContext(ctx)
	var errs []error
	for _, obj := range live {
		key := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		if desiredKeys[key] {
			continue
		}
		if err := r.deleteIfExists(ctx, obj); err != nil {
			errs = append(errs, fmt.Errorf("pruning orphaned %s %s: %w", kind, key, err))
			continue
		}
		log.Info("pruned orphaned RBAC object", "kind", kind, "namespace", key.Namespace, "name", key.Name)
	}
	return errs
}

// pruneOrphanedMCPGatewayNamespaceRBAC deletes any Role/RoleBinding carrying
// LabelMCPGatewayNamespaceRBAC for this instance whose (namespace, name) is
// not present in desiredRoles/desiredRBs (#354). Lists across all
// namespaces since the whole point is to catch a stale copy left behind in
// a namespace the component no longer targets.
//
// Passing nil/empty desired slices (as the finalizer path does via
// deleteMCPGatewayNamespaceRBAC) prunes every labeled object for this
// instance unconditionally, regardless of which namespace the current spec
// resolves to -- necessary because recomputing desired state from the
// current spec at delete time can miss a namespace the CR pointed at
// earlier in its lifetime but was never reconciled away from before
// deletion began.
func (r *KubernautReconciler) pruneOrphanedMCPGatewayNamespaceRBAC(
	ctx context.Context, kn *kubernautv1alpha1.Kubernaut,
	desiredRoles []*rbacv1.Role, desiredRBs []*rbacv1.RoleBinding,
) []error {
	var errs []error
	selector := client.MatchingLabels{
		resources.LabelMCPGatewayNamespaceRBAC: resources.LabelValueTrue,
		"app.kubernetes.io/instance":           kn.Name,
	}

	liveRoles := &rbacv1.RoleList{}
	if err := r.List(ctx, liveRoles, selector); err != nil {
		return append(errs, fmt.Errorf("listing mcp gateway namespace Roles for pruning: %w", err))
	}
	errs = append(errs, pruneUndesiredNamespaced(r, ctx, "Role", pointersOf(liveRoles.Items), namespacedKeysOf(desiredRoles))...)

	liveRBs := &rbacv1.RoleBindingList{}
	if err := r.List(ctx, liveRBs, selector); err != nil {
		return append(errs, fmt.Errorf("listing mcp gateway namespace RoleBindings for pruning: %w", err))
	}
	errs = append(errs, pruneUndesiredNamespaced(r, ctx, "RoleBinding", pointersOf(liveRBs.Items), namespacedKeysOf(desiredRBs))...)

	return errs
}

// additionalRBACComponentSA pairs a component name with its ServiceAccount
// name for spec.additionalClusterRoles binding (#277).
type additionalRBACComponentSA struct {
	name string
	sa   string
}

// additionalRBACComponents returns the components eligible for
// spec.additionalClusterRoles bindings (#277): KA and EM are always
// active; Gateway only when spec.gateway.enabled=true, since a disabled
// Gateway has no ServiceAccount worth binding to.
func additionalRBACComponents(knV2 *kubernautv1alpha2.Kubernaut) []additionalRBACComponentSA {
	comps := []additionalRBACComponentSA{
		{name: resources.ComponentKubernautAgent, sa: resources.ServiceAccountName(resources.ComponentKubernautAgent)},
		{name: resources.ComponentEffectivenessMonitor, sa: resources.ServiceAccountName(resources.ComponentEffectivenessMonitor)},
	}
	if knV2.Spec.GatewayEnabled() {
		comps = append(comps, additionalRBACComponentSA{name: resources.ComponentGateway, sa: resources.ServiceAccountName(resources.ComponentGateway)})
	}
	return comps
}

// deployAdditionalComponentRBAC ensures user-specified additional
// ClusterRoleBindings (spec.additionalClusterRoles, #277) across every
// applicable component's ServiceAccount (KA, EM, and Gateway when
// enabled), prunes any that are no longer desired -- whether a role name
// was removed from the spec or a component itself was disabled --
// validates that referenced ClusterRoles exist, and updates status +
// conditions.
func (r *KubernautReconciler) deployAdditionalComponentRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) error {
	desiredNames := deduplicate(knV2.Spec.AdditionalClusterRoles)
	boundSet := kn.Status.BoundAdditionalClusterRoles
	components := additionalRBACComponents(knV2)

	desiredCRBs := make([]*rbacv1.ClusterRoleBinding, 0, len(desiredNames)*len(components))
	for _, comp := range components {
		for _, crName := range desiredNames {
			crb := resources.AdditionalComponentCRB(kn, comp.name, comp.sa, crName)
			if err := r.ensureUnowned(ctx, crb); err != nil {
				return fmt.Errorf("ensuring additional CRB for %s/%s: %w", comp.name, crName, err)
			}
			desiredCRBs = append(desiredCRBs, crb)
		}
	}

	if errs := r.pruneOrphanedAdditionalComponentRBAC(ctx, kn, desiredCRBs); len(errs) > 0 {
		return fmt.Errorf("pruning orphaned additional component RBAC: %w", errors.Join(errs...))
	}
	r.recordAdditionalRBACBoundEvents(kn, desiredNames, boundSet, len(components))

	missingRoles, err := r.detectMissingClusterRoles(ctx, desiredNames)
	if err != nil {
		return err
	}

	if err := r.patchStatus(ctx, kn, func() {
		kn.Status.BoundAdditionalClusterRoles = desiredNames
		if len(desiredNames) == 0 {
			meta.RemoveStatusCondition(&kn.Status.Conditions, kubernautv1alpha1.ConditionAdditionalRBACBound)
			return
		}
		meta.SetStatusCondition(&kn.Status.Conditions, additionalRBACCondition(kn.Generation, desiredNames, missingRoles, len(components)))
	}); err != nil {
		return fmt.Errorf("patching additional RBAC status: %w", err)
	}

	if len(missingRoles) > 0 {
		r.Recorder.Eventf(kn, nil, corev1.EventTypeWarning, "AdditionalRBACPartial", "Reconcile",
			"ClusterRoles not found: %v", missingRoles)
	}

	return nil
}

// pruneOrphanedAdditionalComponentRBAC deletes any ClusterRoleBinding
// carrying LabelAdditionalComponentRBAC for this instance that is not
// present in desiredCRBs (#277 -- generalized from the KA-only,
// status-diff-based pruneStaleAdditionalAgentCRBs, mirroring #341's
// pruneOrphanedCoreClusterRBAC pattern). Listing live objects by label
// instead of diffing against kn.Status.BoundAdditionalClusterRoles (which
// only ever tracked role *names*, not which components they were bound to)
// also closes a latent leak this generalization would otherwise introduce:
// if Gateway is later disabled, its component's CRBs simply drop out of
// desiredCRBs and are pruned in the same pass.
//
// Passing a nil/empty desiredCRBs (as the finalizer path does) prunes every
// labeled object for this instance unconditionally.
func (r *KubernautReconciler) pruneOrphanedAdditionalComponentRBAC(
	ctx context.Context, kn *kubernautv1alpha1.Kubernaut, desiredCRBs []*rbacv1.ClusterRoleBinding,
) []error {
	liveCRBs := &rbacv1.ClusterRoleBindingList{}
	if err := r.List(ctx, liveCRBs, client.MatchingLabels{
		resources.LabelAdditionalComponentRBAC: resources.LabelValueTrue,
		"app.kubernetes.io/instance":           kn.Name,
	}); err != nil {
		return []error{fmt.Errorf("listing additional component CRBs for pruning: %w", err)}
	}
	return pruneUndesired(r, ctx, "additional component ClusterRoleBinding", pointersOf(liveCRBs.Items), namesOf(desiredCRBs))
}

// recordAdditionalRBACBoundEvents emits a "bound" event for every entry in
// desiredSet that was not already present in boundSet from a prior reconcile.
func (r *KubernautReconciler) recordAdditionalRBACBoundEvents(kn *kubernautv1alpha1.Kubernaut, desiredSet, boundSet []string, componentCount int) {
	for _, crName := range desiredSet {
		if !contains(boundSet, crName) {
			r.Recorder.Eventf(kn, nil, corev1.EventTypeNormal, "AdditionalRBACBound", "Reconcile",
				"Bound ClusterRole %s to %d component(s)", crName, componentCount)
		}
	}
}

// detectMissingClusterRoles returns the subset of crNames that do not
// currently exist as ClusterRoles on the cluster.
func (r *KubernautReconciler) detectMissingClusterRoles(ctx context.Context, crNames []string) ([]string, error) {
	var missingRoles []string
	for _, crName := range crNames {
		cr := &rbacv1.ClusterRole{}
		if err := r.Get(ctx, types.NamespacedName{Name: crName}, cr); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("checking ClusterRole %s: %w", crName, err)
			}
			missingRoles = append(missingRoles, crName)
		}
	}
	return missingRoles, nil
}

// additionalRBACCondition builds the ConditionAdditionalRBACBound status
// condition reflecting whether all desired additional ClusterRoles resolved
// to existing ClusterRoles. componentCount is the number of components
// (KA, EM, and Gateway when enabled -- #277) each ClusterRole name is bound
// to; the message spells it out explicitly since a single entry in
// spec.additionalClusterRoles now produces componentCount ClusterRoleBindings,
// not one, which would otherwise read as a discrepancy to an operator
// cross-checking the message against `oc get clusterrolebindings`.
func additionalRBACCondition(generation int64, desiredSet, missingRoles []string, componentCount int) metav1.Condition {
	cond := metav1.Condition{
		Type:               kubernautv1alpha1.ConditionAdditionalRBACBound,
		ObservedGeneration: generation,
	}
	if len(missingRoles) > 0 {
		cond.Status = metav1.ConditionFalse
		cond.Reason = ReasonAdditionalRBACPartialBound
		cond.Message = fmt.Sprintf("ClusterRoleBindings created but ClusterRoles not found: %v", missingRoles)
		return cond
	}
	cond.Status = metav1.ConditionTrue
	cond.Reason = ReasonAdditionalRBACFullyBound
	cond.Message = fmt.Sprintf("%d additional ClusterRole(s) bound across %d component(s) (%d ClusterRoleBindings)",
		len(desiredSet), componentCount, len(desiredSet)*componentCount)
	return cond
}

// deployToolRBAC provisions persona-based tool ClusterRoles and group-to-role
// ClusterRoleBindings for SAR-based tool authorization (issue #118).
// It also prunes stale CRBs when role bindings are removed from the spec,
// deletes the orphaned legacy apifrontend-rbac-roles ConfigMap,
// and sets the ConditionToolRBACBound condition.
func (r *KubernautReconciler) deployToolRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	log := logf.FromContext(ctx)

	for _, cr := range resources.ToolClusterRoles(kn) {
		if err := r.ensureUnowned(ctx, cr); err != nil {
			return fmt.Errorf("ensuring tool ClusterRole %s: %w", cr.Name, err)
		}
	}

	for _, crb := range resources.ToolClusterRoleBindings(kn) {
		if err := r.ensureUnowned(ctx, crb); err != nil {
			return fmt.Errorf("ensuring tool CRB %s: %w", crb.Name, err)
		}
	}

	desiredCRBNames := resources.ToolCRBNames(kn)
	desiredMap := make(map[string]struct{}, len(desiredCRBNames))
	for _, n := range desiredCRBNames {
		desiredMap[n] = struct{}{}
	}
	for _, name := range kn.Status.BoundToolRoleBindings {
		if _, ok := desiredMap[name]; !ok {
			staleCRB := &rbacv1.ClusterRoleBinding{}
			staleCRB.Name = name
			if err := r.deleteIfExists(ctx, staleCRB); err != nil {
				return fmt.Errorf("pruning stale tool CRB %s: %w", name, err)
			}
			log.Info("pruned stale tool CRB", "name", name)
		}
	}

	legacyCM := &corev1.ConfigMap{}
	legacyCM.Name = "apifrontend-rbac-roles"
	legacyCM.Namespace = kn.Namespace
	if err := r.deleteIfExists(ctx, legacyCM); err != nil {
		return fmt.Errorf("deleting orphaned apifrontend-rbac-roles ConfigMap: %w", err)
	}

	return r.patchStatus(ctx, kn, func() {
		kn.Status.BoundToolRoleBindings = desiredCRBNames

		if len(desiredCRBNames) == 0 {
			meta.RemoveStatusCondition(&kn.Status.Conditions, kubernautv1alpha1.ConditionToolRBACBound)
			return
		}
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type:               kubernautv1alpha1.ConditionToolRBACBound,
			Status:             metav1.ConditionTrue,
			Reason:             "ToolRBACProvisioned",
			Message:            fmt.Sprintf("%d tool role bindings active", len(desiredCRBNames)),
			ObservedGeneration: kn.Generation,
		})
	})
}

func deduplicate(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
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

// deployToggleRBAC handles feature-flag-dependent RBAC: Ansible on/off.
// Cleanup errors are collected and returned so the reconcile loop retries
// (stale RBAC is a security concern).
func (r *KubernautReconciler) deployToggleRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	cr, crb := resources.AnsibleRBAC(kn)
	if kn.Spec.Ansible.Enabled {
		if err := r.ensureUnowned(ctx, cr); err != nil {
			return fmt.Errorf("ensuring AWX ClusterRole: %w", err)
		}
		if err := r.ensureUnowned(ctx, crb); err != nil {
			return fmt.Errorf("ensuring AWX CRB: %w", err)
		}
	}

	var errs []error
	if !kn.Spec.Ansible.Enabled {
		errs = append(errs, r.pruneStaleAnsibleRBAC(ctx, cr, crb)...)
	}
	if !kn.Spec.GatewayEnabled() {
		errs = append(errs, r.pruneStaleGatewayRBAC(ctx, kn)...)
	}

	if len(errs) > 0 {
		return fmt.Errorf("toggle cleanup: %w", errors.Join(errs...))
	}
	return nil
}

// pruneStaleAnsibleRBAC deletes the AWX ClusterRole/ClusterRoleBinding when
// Ansible integration is disabled.
func (r *KubernautReconciler) pruneStaleAnsibleRBAC(ctx context.Context, cr *rbacv1.ClusterRole, crb *rbacv1.ClusterRoleBinding) []error {
	var errs []error
	if err := r.deleteIfExists(ctx, cr); err != nil {
		errs = append(errs, fmt.Errorf("removing stale AWX ClusterRole: %w", err))
	}
	if err := r.deleteIfExists(ctx, crb); err != nil {
		errs = append(errs, fmt.Errorf("removing stale AWX CRB: %w", err))
	}
	return errs
}

// pruneStaleGatewayRBAC deletes the namespaced DataStorage-client
// RoleBinding when the gateway component is disabled. Gateway's
// cluster-scoped ClusterRole/ClusterRoleBinding (gateway-role,
// gateway-role-binding, alertmanager-gateway-signal-source) are handled by
// deployCoreRBAC's generic prune (#341) instead -- they naturally drop out
// of resources.ClusterRoles()/ClusterRoleBindings() when gateway is
// disabled, so the same label-selector diff that closes the FMC leak covers
// them too, without a dedicated static-name delete list to keep in sync.
func (r *KubernautReconciler) pruneStaleGatewayRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) []error {
	var errs []error
	staleRB := &rbacv1.RoleBinding{}
	staleRB.Name = "data-storage-client-gateway"
	staleRB.Namespace = kn.Namespace
	if err := r.deleteIfExists(ctx, staleRB); err != nil {
		errs = append(errs, fmt.Errorf("removing stale gateway DS client RoleBinding: %w", err))
	}
	return errs
}

// cleanupDisabledGateway removes all namespaced gateway resources when the
// gateway component is disabled via spec.gateway.enabled=false. Cluster-scoped
// RBAC cleanup is handled separately in deployToggleRBAC.
func (r *KubernautReconciler) cleanupDisabledGateway(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	ns := kn.Namespace
	var errs []error

	dep := &appsv1.Deployment{}
	dep.Name = resources.DeploymentName(resources.ComponentGateway)
	dep.Namespace = ns
	if err := r.deleteIfExists(ctx, dep); err != nil {
		errs = append(errs, fmt.Errorf("deleting gateway Deployment: %w", err))
	}

	svc := &corev1.Service{}
	svc.Name = "gateway-service"
	svc.Namespace = ns
	if err := r.deleteIfExists(ctx, svc); err != nil {
		errs = append(errs, fmt.Errorf("deleting gateway Service: %w", err))
	}

	sa := &corev1.ServiceAccount{}
	sa.Name = resources.ServiceAccountName(resources.ComponentGateway)
	sa.Namespace = ns
	if err := r.deleteIfExists(ctx, sa); err != nil {
		errs = append(errs, fmt.Errorf("deleting gateway ServiceAccount: %w", err))
	}

	cm := &corev1.ConfigMap{}
	cm.Name = "gateway-config"
	cm.Namespace = ns
	if err := r.deleteIfExists(ctx, cm); err != nil {
		errs = append(errs, fmt.Errorf("deleting gateway ConfigMap: %w", err))
	}

	np := &networkingv1.NetworkPolicy{}
	np.Name = resources.ComponentGateway + "-netpol"
	np.Namespace = ns
	if err := r.deleteIfExists(ctx, np); err != nil {
		errs = append(errs, fmt.Errorf("deleting gateway NetworkPolicy: %w", err))
	}

	if r.hasCRD(ctx, "alertmanagerconfigs.monitoring.coreos.com") {
		amCfg := &monitoringv1alpha1.AlertmanagerConfig{}
		amCfg.Name = "kubernaut-gateway-alerts"
		amCfg.Namespace = ns
		if err := r.deleteIfExists(ctx, amCfg); err != nil {
			errs = append(errs, fmt.Errorf("deleting gateway AlertManagerConfig: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// cleanupDisabledFleetMetadataCache removes FMC's Deployment, Service,
// ConfigMap, and NetworkPolicy when spec.fleetMetadataCache.enabled
// transitions to false. The ServiceAccount and PDB are left in place --
// like AuthWebhook/APIFrontend, FMC's SA is managed unconditionally via
// deployServiceAccounts, and PodDisruptionBudgets already skips inactive
// components on its own. FMC's cluster-scoped ClusterRole/ClusterRoleBinding
// (both the original MCP-Gateway-CRD pair and the #1993 auth-middleware/
// scope-check-client pair) are handled by deployCoreRBAC's generic prune
// (#341), not here.
func (r *KubernautReconciler) cleanupDisabledFleetMetadataCache(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	ns := kn.Namespace
	var errs []error

	dep := &appsv1.Deployment{}
	dep.Name = resources.DeploymentName(resources.ComponentFleetMetadataCache)
	dep.Namespace = ns
	if err := r.deleteIfExists(ctx, dep); err != nil {
		errs = append(errs, fmt.Errorf("deleting fleetmetadatacache Deployment: %w", err))
	}

	svc := &corev1.Service{}
	svc.Name = "fleetmetadatacache-service"
	svc.Namespace = ns
	if err := r.deleteIfExists(ctx, svc); err != nil {
		errs = append(errs, fmt.Errorf("deleting fleetmetadatacache Service: %w", err))
	}

	cm := &corev1.ConfigMap{}
	cm.Name = "fleetmetadatacache-config"
	cm.Namespace = ns
	if err := r.deleteIfExists(ctx, cm); err != nil {
		errs = append(errs, fmt.Errorf("deleting fleetmetadatacache ConfigMap: %w", err))
	}

	np := &networkingv1.NetworkPolicy{}
	np.Name = resources.ComponentFleetMetadataCache + "-netpol"
	np.Namespace = ns
	if err := r.deleteIfExists(ctx, np); err != nil {
		errs = append(errs, fmt.Errorf("deleting fleetmetadatacache NetworkPolicy: %w", err))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// warnIfFleetMetadataCacheUnused emits a Warning Event (not an admission
// rejection, and never mutates spec.fleetMetadataCache) when FMC is deployed
// but spec.fleet.enabled/backend don't actually route any component's
// queries to it. This isn't unsafe -- unlike the fail-closed checks in
// validateFleetConfig/validateFleetMetadataCache (missing tokenSecretName,
// mcpGatewayEndpoint, etc. would crash-loop pods or send unauthenticated
// requests) -- so it's surfaced the same way as NetworkPoliciesDisabled
// below rather than blocked at admission: a deliberate shadow-deploy of FMC
// ahead of cutting Gateway/RemediationOrchestrator over from backend=acm is
// a legitimate use of this combination, just one worth flagging rather than
// leaving silent.
func (r *KubernautReconciler) warnIfFleetMetadataCacheUnused(knV2 *kubernautv1alpha2.Kubernaut) {
	fleet := &knV2.Spec.Fleet
	fleetEnabled := fleet.Enabled != nil && *fleet.Enabled
	if fleetEnabled && fleet.Backend == "fleetmetadatacache" {
		return
	}
	r.Recorder.Eventf(knV2, nil, corev1.EventTypeWarning, "FleetMetadataCacheUnused", "Reconcile",
		"spec.fleetMetadataCache.enabled is true but spec.fleet.enabled=%v and spec.fleet.backend=%q -- FMC is deployed and syncing but not queried by any component (only fleet.enabled=true with backend=fleetmetadatacache routes to it)",
		fleetEnabled, fleet.Backend)
}

// deployConfigMaps builds and ensures all service ConfigMaps. Returns a map
// of component name to SHA-256 hash of the ConfigMap data, used to stamp pod
// template annotations and force rolling restarts when config content changes.
func (r *KubernautReconciler) deployConfigMaps(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, dbName, dbUser, tlsProfile string, sidecar resources.KagentiSidecarMode, oidc *resources.KagentiOIDCDefaults) (map[string]string, error) {
	tlsOpt := resources.WithTLSProfile(tlsProfile)

	// buildCoreConfigMaps transitively calls resolveHostToIP (via
	// DataStorageConfigMap), a best-effort DNS lookup that deliberately uses
	// context.Background() (see its doc comment): every error path,
	// including a cancelled context, falls back to the configured hostname,
	// so threading ctx through the builder-table plumbing would not change
	// behavior. See the inline nolint on the datastorage builder entry.
	configMaps, cmHashes, err := buildCoreConfigMaps(kn, knV2, tlsOpt, dbName, dbUser) //nolint:contextcheck
	if err != nil {
		return nil, err
	}

	configMaps, err = r.appendOptionalComponentConfigMaps(kn, knV2, sidecar, oidc, tlsOpt, configMaps, cmHashes)
	if err != nil {
		return nil, err
	}
	configMaps = appendServiceCAConfigMaps(kn, configMaps)

	for _, cm := range configMaps {
		if err := r.ensureNamespaced(ctx, kn, cm); err != nil {
			return nil, fmt.Errorf("ensuring ConfigMap %s: %w", cm.Name, err)
		}
	}
	return cmHashes, nil
}

// buildCoreConfigMaps constructs the ConfigMaps that are always considered
// regardless of component toggles: the strategy-table-driven builder set,
// the proactive-signal-mappings ConfigMap, and the
// kubernaut-agent-llm-runtime ConfigMap (whose content hash is folded into
// the kubernaut-agent entry so either change triggers a rollout). Returns
// the accumulated ConfigMap list and their name->hash map, used to stamp pod
// template annotations and force rolling restarts when config content changes.
func buildCoreConfigMaps(
	kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, tlsOpt resources.ConfigMapOption, dbName, dbUser string,
) ([]*corev1.ConfigMap, map[string]string, error) {
	type cmBuilder struct {
		name string
		fn   func() (*corev1.ConfigMap, error)
	}
	builders := []cmBuilder{
		// DataStorageConfigMap transitively calls resolveHostToIP, a best-effort
		// DNS lookup that deliberately uses context.Background() (see its doc
		// comment): every error path, including a cancelled context, falls back
		// to the configured hostname, so propagating ctx would not change behavior.
		{"datastorage", func() (*corev1.ConfigMap, error) {
			return resources.DataStorageConfigMap(kn, knV2, dbName, dbUser, tlsOpt)
		}}, //nolint:contextcheck
		{"aianalysis", func() (*corev1.ConfigMap, error) { return resources.AIAnalysisConfigMap(kn, tlsOpt) }},
		{"signalprocessing", func() (*corev1.ConfigMap, error) { return resources.SignalProcessingConfigMap(kn, knV2, tlsOpt) }},
		{"remediationorchestrator", func() (*corev1.ConfigMap, error) { return resources.RemediationOrchestratorConfigMap(kn, knV2, tlsOpt) }},
		{"workflowexecution", func() (*corev1.ConfigMap, error) { return resources.WorkflowExecutionConfigMap(kn, knV2, tlsOpt) }},
		{"effectivenessmonitor", func() (*corev1.ConfigMap, error) { return resources.EffectivenessMonitorConfigMap(kn, knV2, tlsOpt) }},
		{"notification-controller", func() (*corev1.ConfigMap, error) { return resources.NotificationControllerConfigMap(kn, tlsOpt) }},
		{"notification-routing", func() (*corev1.ConfigMap, error) {
			if kn.Spec.Notification.Routing != nil && kn.Spec.Notification.Routing.ConfigMapName != "" {
				// Caller supplied their own routing ConfigMap (spec.notification.routing.configMapName);
				// the operator must not build/own one. (nil, nil) is a deliberate
				// "nothing to build" sentinel -- the loop below skips nil results.
				return nil, nil //nolint:nilnil
			}
			return resources.NotificationRoutingConfigMap(kn)
		}},
		{"kubernaut-agent", func() (*corev1.ConfigMap, error) { return resources.KubernautAgentConfigMap(kn, knV2, tlsOpt) }},
		{"authwebhook", func() (*corev1.ConfigMap, error) { return resources.AuthWebhookConfigMap(kn, tlsOpt) }},
	}

	cmHashes := make(map[string]string, len(builders))
	var configMaps []*corev1.ConfigMap
	for _, b := range builders {
		cm, err := b.fn()
		if err != nil {
			return nil, nil, fmt.Errorf("building %s ConfigMap: %w", b.name, err)
		}
		// A nil result (no error) means the builder deliberately has nothing to
		// create -- e.g. notification-routing when the caller supplied their
		// own ConfigMap via spec.notification.routing.configMapName.
		if cm == nil {
			continue
		}
		configMaps = append(configMaps, cm)
		cmHashes[b.name] = resources.ConfigMapDataHash(cm.Data)
	}

	if cm := resources.ProactiveSignalMappingsConfigMap(kn); cm != nil {
		configMaps = append(configMaps, cm)
	}
	llmRuntimeCM, err := resources.KubernautAgentLLMRuntimeConfigMap(kn)
	if err != nil {
		return nil, nil, fmt.Errorf("building kubernaut-agent-llm-runtime ConfigMap: %w", err)
	}
	if llmRuntimeCM != nil {
		llmHash := resources.ConfigMapDataHash(llmRuntimeCM.Data)
		if agentHash, ok := cmHashes["kubernaut-agent"]; ok {
			cmHashes["kubernaut-agent"] = agentHash + "+" + llmHash
		}
		configMaps = append(configMaps, llmRuntimeCM)
	}
	return configMaps, cmHashes, nil
}

// appendOptionalComponentConfigMaps builds and appends ConfigMaps for
// components that are toggled on/off via the CR spec (gateway, apifrontend,
// fleetmetadatacache), recording each one's content hash in cmHashes.
func (r *KubernautReconciler) appendOptionalComponentConfigMaps(
	kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, sidecar resources.KagentiSidecarMode, oidc *resources.KagentiOIDCDefaults,
	tlsOpt resources.ConfigMapOption, configMaps []*corev1.ConfigMap, cmHashes map[string]string,
) ([]*corev1.ConfigMap, error) {
	if kn.Spec.GatewayEnabled() {
		gwCM, err := resources.GatewayConfigMap(kn, knV2, tlsOpt)
		if err != nil {
			return nil, fmt.Errorf("building gateway ConfigMap: %w", err)
		}
		configMaps = append(configMaps, gwCM)
		cmHashes["gateway"] = resources.ConfigMapDataHash(gwCM.Data)
	}
	if kn.Spec.APIFrontendEnabled() {
		afCM, err := resources.APIFrontendConfigMap(kn, knV2, sidecar, oidc)
		if err != nil {
			return nil, fmt.Errorf("building apifrontend ConfigMap: %w", err)
		}
		configMaps = append(configMaps, afCM)
		cmHashes["apifrontend"] = resources.ConfigMapDataHash(afCM.Data)
	}
	if knV2.Spec.FleetMetadataCacheEnabled() {
		r.warnIfFleetMetadataCacheUnused(knV2)
		fmcCM, err := resources.FleetMetadataCacheConfigMap(kn, knV2)
		if err != nil {
			return nil, fmt.Errorf("building fleetmetadatacache ConfigMap: %w", err)
		}
		configMaps = append(configMaps, fmcCM)
		cmHashes["fleetmetadatacache"] = resources.ConfigMapDataHash(fmcCM.Data)
	}
	return configMaps, nil
}

// appendServiceCAConfigMaps appends the always-present inter-service CA
// bundle ConfigMap, the operator-computed trust-bundle ConfigMap that
// merges it with the cluster's default ingress/router CA, and the
// per-component service-CA ConfigMaps consumed by mTLS-scraping sidecars.
func appendServiceCAConfigMaps(kn *kubernautv1alpha1.Kubernaut, configMaps []*corev1.ConfigMap) []*corev1.ConfigMap {
	configMaps = append(configMaps, resources.InterServiceCAConfigMap(kn), resources.TrustBundleConfigMap(kn))
	configMaps = append(configMaps,
		resources.EffectivenessMonitorServiceCAConfigMap(kn),
		resources.KubernautAgentServiceCAConfigMap(kn),
		resources.APIFrontendServiceCAConfigMap(kn),
	)
	return configMaps
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
	resources.ComponentAPIFrontend:             "apifrontend",
	resources.ComponentFleetMetadataCache:      "fleetmetadatacache",
}

// deployWorkloads creates/updates deployments, services, PDBs, and the OCP
// route. cmHashes maps ConfigMap builder names to content hashes; these are
// stamped as pod template annotations to force rolling restarts when config
// content changes. Returns true if a route was created.
// deploymentBuilderFunc constructs a component Deployment from the
// Kubernaut CR. Used to assemble the list of builders to run in
// deployWorkloads, which varies by which optional components are enabled.
// knV2 supplies Fleet-gated fields for the builders that need them (Fleet's
// entire CRD surface lives in v1alpha2, Fleet v1alpha2 migration); builders
// unrelated to Fleet simply ignore it.
type deploymentBuilderFunc func(*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error)

// enabledDeploymentBuilders returns the deployment builder functions for all
// always-on components plus any toggled-on optional components (gateway,
// apifrontend, console, fleetmetadatacache). When an optional component is
// disabled instead, its namespaced resources are cleaned up here.
func (r *KubernautReconciler) enabledDeploymentBuilders(
	ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, sidecar resources.KagentiSidecarMode,
) ([]deploymentBuilderFunc, error) {
	depBuilders := []deploymentBuilderFunc{
		func(kn *kubernautv1alpha1.Kubernaut, _ *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
			return resources.DataStorageDeployment(kn)
		},
		func(kn *kubernautv1alpha1.Kubernaut, _ *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
			return resources.AIAnalysisDeployment(kn)
		},
		resources.SignalProcessingDeployment,
		resources.RemediationOrchestratorDeployment,
		resources.WorkflowExecutionDeployment,
		resources.EffectivenessMonitorDeployment,
		func(kn *kubernautv1alpha1.Kubernaut, _ *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
			return resources.NotificationDeployment(kn)
		},
		resources.KubernautAgentDeployment,
		func(kn *kubernautv1alpha1.Kubernaut, _ *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
			return resources.AuthWebhookDeployment(kn)
		},
	}
	if kn.Spec.GatewayEnabled() {
		depBuilders = append(depBuilders, resources.GatewayDeployment)
	} else if err := r.cleanupDisabledGateway(ctx, kn); err != nil {
		return nil, fmt.Errorf("cleaning up disabled gateway: %w", err)
	}
	if kn.Spec.APIFrontendEnabled() {
		depBuilders = append(depBuilders, func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
			return resources.APIFrontendDeployment(kn, knV2, sidecar)
		})
	}
	if kn.Spec.ConsoleEnabled() {
		ingressDomain := r.clusterIngressDomain(ctx)
		depBuilders = append(depBuilders, func(kn *kubernautv1alpha1.Kubernaut, _ *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
			return resources.ConsoleDeployment(kn, ingressDomain)
		})
	}
	if knV2.Spec.FleetMetadataCacheEnabled() {
		depBuilders = append(depBuilders, resources.FleetMetadataCacheDeployment)
	} else if err := r.cleanupDisabledFleetMetadataCache(ctx, kn); err != nil {
		return nil, fmt.Errorf("cleaning up disabled fleetmetadatacache: %w", err)
	}
	return depBuilders, nil
}

// ensureDeployments builds and reconciles each Deployment produced by
// builders, stamping ConfigMap-hash pod-template annotations from cmHashes
// so configuration changes trigger rolling restarts.
func (r *KubernautReconciler) ensureDeployments(
	ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, builders []deploymentBuilderFunc, cmHashes map[string]string,
) error {
	for _, build := range builders {
		dep, err := build(kn, knV2)
		if err != nil {
			return fmt.Errorf("building deployment: %w", err)
		}
		stampConfigMapHash(dep, cmHashes)
		if err := r.ensureNamespaced(ctx, kn, dep); err != nil {
			return fmt.Errorf("ensuring Deployment %s: %w", dep.Name, err)
		}
	}
	return nil
}

func (r *KubernautReconciler) deployWorkloads(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, cmHashes map[string]string, sidecar resources.KagentiSidecarMode) (hasRoute bool, _ error) {
	depBuilders, err := r.enabledDeploymentBuilders(ctx, kn, knV2, sidecar)
	if err != nil {
		return false, err
	}
	if err := r.ensureDeployments(ctx, kn, knV2, depBuilders, cmHashes); err != nil {
		return false, err
	}

	if err := r.ensureServices(ctx, kn, knV2, sidecar); err != nil {
		return false, err
	}

	for _, pdb := range resources.PodDisruptionBudgets(kn, knV2) {
		if err := r.ensureNamespaced(ctx, kn, pdb); err != nil {
			return false, fmt.Errorf("ensuring PDB %s: %w", pdb.Name, err)
		}
	}

	if err := r.reconcileNetworkPolicies(ctx, kn, knV2, sidecar); err != nil {
		return false, err
	}

	dsHPA := resources.DataStorageHPA(kn)
	if err := r.ensureNamespaced(ctx, kn, dsHPA); err != nil {
		return false, fmt.Errorf("ensuring DS HPA: %w", err)
	}

	if err := r.reconcileMonitoringAndAlerts(ctx, kn); err != nil {
		return false, err
	}

	if kn.Spec.APIFrontendEnabled() {
		if err := r.deployAPIFrontendExtras(ctx, kn); err != nil {
			return false, err
		}
	}

	return r.reconcileRoutes(ctx, kn)
}

func (r *KubernautReconciler) ensureServices(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, sidecar resources.KagentiSidecarMode) error {
	for _, svc := range resources.Services(kn, knV2, sidecar) {
		if err := r.ensureNamespaced(ctx, kn, svc); err != nil {
			return fmt.Errorf("ensuring Service %s: %w", svc.Name, err)
		}
		if err := r.clearStaleServingCertErrors(ctx, svc); err != nil {
			return fmt.Errorf("clearing stale serving-cert annotations on %s: %w", svc.Name, err)
		}
	}
	for _, svc := range resources.MetricsServices(kn) {
		if err := r.ensureNamespaced(ctx, kn, svc); err != nil {
			return fmt.Errorf("ensuring metrics Service %s: %w", svc.Name, err)
		}
	}
	if kn.Spec.ConsoleEnabled() {
		if err := r.ensureNamespaced(ctx, kn, resources.ConsoleService(kn)); err != nil {
			return fmt.Errorf("ensuring Console Service: %w", err)
		}
		if err := r.ensureNamespaced(ctx, kn, resources.ConsoleNginxConfigMap(kn)); err != nil {
			return fmt.Errorf("ensuring Console nginx ConfigMap: %w", err)
		}
	}
	return nil
}

func (r *KubernautReconciler) reconcileNetworkPolicies(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, sidecar resources.KagentiSidecarMode) error {
	if kn.Spec.NetworkPolicies.NetworkPoliciesEnabled() {
		// NetworkPolicies transitively calls resolveAPIServerIPs, a best-effort
		// API-server-IP discovery that deliberately uses context.Background()
		// (see its doc comment) rather than threading ctx through all ~14
		// NetworkPolicy builder functions in networkpolicies.go.
		for _, np := range resources.NetworkPolicies(kn, knV2, sidecar) { //nolint:contextcheck
			if err := r.ensureNamespaced(ctx, kn, np); err != nil {
				return fmt.Errorf("ensuring NetworkPolicy %s: %w", np.Name, err)
			}
		}
		return nil
	}
	r.Recorder.Eventf(kn, nil, corev1.EventTypeWarning, "NetworkPoliciesDisabled", "Reconcile",
		"spec.networkPolicies.enabled is false — network segmentation is required for FedRAMP SC-7 compliance; set to true for production")
	var npList networkingv1.NetworkPolicyList
	if err := r.List(ctx, &npList, client.InNamespace(kn.Namespace), client.MatchingLabels{
		"app.kubernetes.io/managed-by": "kubernaut-operator",
	}); err == nil {
		for i := range npList.Items {
			if err := r.deleteIfExists(ctx, &npList.Items[i]); err != nil {
				return fmt.Errorf("deleting NetworkPolicy %s: %w", npList.Items[i].Name, err)
			}
		}
	}
	return nil
}

func (r *KubernautReconciler) reconcileMonitoringAndAlerts(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	if r.hasCRD(ctx, "servicemonitors.monitoring.coreos.com") {
		if err := r.deployMonitoring(ctx, kn); err != nil {
			return err
		}
	}
	if kn.Spec.GatewayEnabled() {
		if amCfg := resources.GatewayAlertManagerConfig(kn); amCfg != nil && r.hasCRD(ctx, "alertmanagerconfigs.monitoring.coreos.com") {
			if err := r.ensureNamespaced(ctx, kn, amCfg); err != nil {
				return fmt.Errorf("ensuring Gateway AlertManagerConfig: %w", err)
			}
		}
	}
	return nil
}

func (r *KubernautReconciler) deployMonitoring(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	dsSM := resources.DataStorageServiceMonitor(kn)
	if err := r.ensureNamespaced(ctx, kn, dsSM); err != nil {
		return fmt.Errorf("ensuring DS ServiceMonitor: %w", err)
	}
	dsPR := resources.DataStoragePrometheusRule(kn)
	if err := r.ensureNamespaced(ctx, kn, dsPR); err != nil {
		return fmt.Errorf("ensuring DS PrometheusRule: %w", err)
	}
	kaSM := resources.KubernautAgentServiceMonitor(kn)
	if err := r.ensureNamespaced(ctx, kn, kaSM); err != nil {
		return fmt.Errorf("ensuring KA ServiceMonitor: %w", err)
	}
	kaPR := resources.KubernautAgentPrometheusRule(kn)
	if err := r.ensureNamespaced(ctx, kn, kaPR); err != nil {
		return fmt.Errorf("ensuring KA PrometheusRule: %w", err)
	}

	componentMonitors := []*monitoringv1.ServiceMonitor{
		resources.GatewayServiceMonitor(kn),
		resources.AIAnalysisServiceMonitor(kn),
		resources.SignalProcessingServiceMonitor(kn),
		resources.RemediationOrchestratorServiceMonitor(kn),
		resources.WorkflowExecutionServiceMonitor(kn),
		resources.EffectivenessMonitorServiceMonitor(kn),
		resources.NotificationServiceMonitor(kn),
		resources.AuthWebhookServiceMonitor(kn),
	}
	for _, sm := range componentMonitors {
		if err := r.ensureNamespaced(ctx, kn, sm); err != nil {
			return fmt.Errorf("ensuring %s ServiceMonitor: %w", sm.Name, err)
		}
	}
	return nil
}

func (r *KubernautReconciler) reconcileRoutes(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) (bool, error) {
	hasRoute := false

	var gwRoute *routev1.Route
	if kn.Spec.GatewayEnabled() {
		gwRoute = resources.GatewayRoute(kn)
	}
	gwHasRoute, err := r.reconcileOptionalRoute(ctx, kn, "Gateway", gwRoute, resources.GatewayRouteStub(kn))
	if err != nil {
		return false, err
	}
	hasRoute = hasRoute || gwHasRoute

	afHasRoute, err := r.reconcileOptionalRoute(ctx, kn, "AF", resources.APIFrontendRoute(kn), resources.APIFrontendRouteStub(kn))
	if err != nil {
		return false, err
	}
	hasRoute = hasRoute || afHasRoute

	var consoleRoute *routev1.Route
	if kn.Spec.ConsoleEnabled() {
		consoleRoute = resources.ConsoleRoute(kn)
	}
	consoleHasRoute, err := r.reconcileOptionalRoute(ctx, kn, "Console", consoleRoute, resources.ConsoleRouteStub(kn))
	if err != nil {
		return false, err
	}
	hasRoute = hasRoute || consoleHasRoute

	return hasRoute, nil
}

// reconcileOptionalRoute ensures route when it is non-nil (the component is
// enabled and its builder produced a Route, e.g. not BYO-ingress), or
// deletes staleRoute -- a name-only stub -- otherwise, covering both
// "component disabled" and "component enabled but no Route needed". Returns
// true when a Route now exists.
func (r *KubernautReconciler) reconcileOptionalRoute(
	ctx context.Context, kn *kubernautv1alpha1.Kubernaut, label string, route, staleRoute *routev1.Route,
) (bool, error) {
	if route != nil {
		if err := r.ensureNamespaced(ctx, kn, route); err != nil {
			return false, fmt.Errorf("ensuring %s Route: %w", label, err)
		}
		return true, nil
	}
	if err := r.deleteIfExists(ctx, staleRoute); err != nil && !runtime.IsNotRegisteredError(err) {
		return false, fmt.Errorf("deleting stale %s Route: %w", label, err)
	}
	return false, nil
}

// detectKagentiSidecarMode determines which sidecar injection strategy the
// installed kagenti version uses. kagenti 0.3.x+ ships the agents.agent.kagenti.dev
// CRD and uses authbridge-proxy (shifts app port to +1). Older 0.2.x versions
// use an envoy sidecar with iptables interception (no port shift).
func (r *KubernautReconciler) detectKagentiSidecarMode(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) resources.KagentiSidecarMode {
	if !kn.Spec.APIFrontendEnabled() || !kn.Spec.APIFrontend.SPIRE.SPIREEnabled() {
		return resources.KagentiSidecarNone
	}
	if r.hasCRD(ctx, "agents.agent.kagenti.dev") {
		logf.FromContext(ctx).Info("detected kagenti 0.3.x+ (authbridge-proxy sidecar)")
		return resources.KagentiSidecarAuthbridge
	}
	logf.FromContext(ctx).Info("detected kagenti 0.2.x (envoy sidecar)")
	return resources.KagentiSidecarEnvoy
}

// resolveKagentiOIDCDefaults reads the kagenti authbridge-config ConfigMap
// and extracts OIDC settings that the AF needs to validate tokens issued
// by the same Keycloak realm kagenti uses. This eliminates the manual step
// of copying realm URLs into the Kubernaut CR on every fresh deploy.
//
// FedRAMP IA-2: the operator ensures AF authenticates users against the
// correct identity provider by deriving settings from the kagenti source of
// truth rather than relying on error-prone manual configuration.
func (r *KubernautReconciler) resolveKagentiOIDCDefaults(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, sidecar resources.KagentiSidecarMode) (*resources.KagentiOIDCDefaults, error) {
	if sidecar == resources.KagentiSidecarNone {
		// No kagenti sidecar active: OIDC auto-detection is not applicable, not an error.
		return nil, nil //nolint:nilnil
	}

	log := logf.FromContext(ctx)

	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: kagentiSystemNamespace, Name: kagentiAuthbridgeConfigMapName}
	if err := r.Get(ctx, key, cm); err != nil {
		if apierrors.IsNotFound(err) {
			// If the CR already provides issuerURL, the ConfigMap is not
			// strictly needed — the operator can proceed without auto-detection.
			if kn.Spec.APIFrontend.Auth.IssuerURL != "" {
				log.Info("kagenti authbridge-config not found but CR has issuerURL set — skipping OIDC auto-detection")
				// CR already provides issuerURL manually: auto-detection is
				// unnecessary, not a failure.
				return nil, nil //nolint:nilnil
			}
			return nil, fmt.Errorf("kagenti sidecar is active but %s/%s ConfigMap not found — "+
				"set spec.apiFrontend.auth.issuerURL manually or ensure kagenti-operator is installed",
				kagentiSystemNamespace, kagentiAuthbridgeConfigMapName)
		}
		return nil, fmt.Errorf("reading kagenti authbridge-config: %w", err)
	}

	issuer := cm.Data["ISSUER"]
	if issuer == "" {
		if kn.Spec.APIFrontend.Auth.IssuerURL != "" {
			log.Info("kagenti authbridge-config missing ISSUER key but CR has issuerURL — skipping OIDC auto-detection")
			// CR already provides issuerURL manually: auto-detection is
			// unnecessary, not a failure.
			return nil, nil //nolint:nilnil
		}
		return nil, fmt.Errorf("kagenti %s/%s ConfigMap is missing the ISSUER key — "+
			"set spec.apiFrontend.auth.issuerURL manually", kagentiSystemNamespace, kagentiAuthbridgeConfigMapName)
	}

	keycloakURL := cm.Data["KEYCLOAK_URL"]
	realm := cm.Data["KEYCLOAK_REALM"]

	defaults := &resources.KagentiOIDCDefaults{
		IssuerURL: issuer,
	}

	if keycloakURL != "" && realm != "" {
		defaults.JWKSURL = strings.TrimRight(keycloakURL, "/") +
			"/realms/" + realm + "/protocol/openid-connect/certs"
		defaults.AllowInsecureIssuers = strings.HasPrefix(keycloakURL, "http://")
	}

	log.Info("auto-detected OIDC defaults from kagenti",
		"issuerURL", defaults.IssuerURL,
		"jwksURL", defaults.JWKSURL,
		"allowInsecureIssuers", defaults.AllowInsecureIssuers)
	r.Recorder.Eventf(kn, nil, corev1.EventTypeNormal, ReasonOIDCAutoDetected, "Reconcile",
		"Auto-detected OIDC issuerURL from kagenti authbridge-config: %s", defaults.IssuerURL)

	return defaults, nil
}

// ensureKagentiNamespaceLabel adds or removes the "kagenti-enabled" label on
// the kubernaut-system namespace. The kagenti mutating webhook requires this
// label in its namespaceSelector to inject the authbridge sidecar into AF pods.
// Additionally, when SPIRE is enabled the authbridge sidecar uses a SPIFFE CSI
// inline volume that requires pod-security.kubernetes.io/enforce=privileged.
func (r *KubernautReconciler) ensureKagentiNamespaceLabel(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	log := logf.FromContext(ctx)

	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: kn.Namespace}, ns); err != nil {
		return fmt.Errorf("fetching namespace for kagenti label: %w", err)
	}

	want := kn.Spec.APIFrontendEnabled() && kn.Spec.APIFrontend.SPIRE.SPIREEnabled()
	have := ns.Labels["kagenti-enabled"] == resources.LabelValueTrue

	needsUpdate := want != have

	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}

	if want {
		if !have {
			log.Info("labeling namespace for kagenti authbridge injection", "namespace", kn.Namespace)
			ns.Labels["kagenti-enabled"] = resources.LabelValueTrue
		}
		if ensurePSALabels(ns.Labels) {
			log.Info("setting pod security labels for SPIFFE CSI volume", "namespace", kn.Namespace)
			needsUpdate = true
		}
	} else if have {
		log.Info("removing kagenti-enabled label from namespace", "namespace", kn.Namespace)
		delete(ns.Labels, "kagenti-enabled")
	}

	if !needsUpdate {
		return nil
	}
	return r.Update(ctx, ns)
}

// ensurePSALabels sets the Pod Security Admission labels required for the
// SPIFFE CSI inline volume. Returns true if any label was added or changed.
func ensurePSALabels(labels map[string]string) bool {
	changed := false
	psaLabels := map[string]string{
		"pod-security.kubernetes.io/enforce":         "privileged",
		"pod-security.kubernetes.io/enforce-version": "latest",
		"pod-security.kubernetes.io/audit":           "privileged",
		"pod-security.kubernetes.io/audit-version":   "latest",
		"pod-security.kubernetes.io/warn":            "privileged",
		"pod-security.kubernetes.io/warn-version":    "latest",
	}
	for k, v := range psaLabels {
		if labels[k] != v {
			labels[k] = v
			changed = true
		}
	}
	return changed
}

// agentRuntimeGVR is the GroupVersionResource for kagenti AgentRuntime CRs.
var agentRuntimeGVR = schema.GroupVersionResource{
	Group:    "agent.kagenti.dev",
	Version:  "v1alpha1",
	Resource: "agentruntimes",
}

// ensureAgentRuntimeCR creates or deletes the kagenti AgentRuntime CR for
// apifrontend. When kagenti sidecar injection is active, the CR tells the
// kagenti operator to provision authbridge ConfigMaps, SCC RoleBindings,
// and discovery labels in the kubernaut-system namespace. When sidecar
// injection is disabled, any existing CR is cleaned up.
func (r *KubernautReconciler) ensureAgentRuntimeCR(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, sidecar resources.KagentiSidecarMode) error {
	log := logf.FromContext(ctx)

	if !r.hasCRD(ctx, "agentruntimes.agent.kagenti.dev") {
		return nil
	}

	name := string(resources.ComponentAPIFrontend)
	want := sidecar != resources.KagentiSidecarNone

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(schema.GroupVersionKind{
		Group: agentRuntimeGVR.Group, Version: agentRuntimeGVR.Version, Kind: "AgentRuntime",
	})
	err := r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: name}, existing)

	if !want {
		if err == nil {
			log.Info("deleting AgentRuntime CR (kagenti sidecar disabled)", "name", name)
			return client.IgnoreNotFound(r.Delete(ctx, existing))
		}
		return nil
	}

	desired := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": agentRuntimeGVR.Group + "/" + agentRuntimeGVR.Version,
			"kind":       "AgentRuntime",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": kn.Namespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "kubernaut-operator",
					"app.kubernetes.io/part-of":    "kubernaut",
					"app.kubernetes.io/instance":   kn.Name,
				},
			},
			"spec": map[string]interface{}{
				"type": "agent",
				"targetRef": map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"name":       name,
				},
			},
		},
	}

	if apierrors.IsNotFound(err) {
		log.Info("creating AgentRuntime CR for apifrontend", "name", name)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return fmt.Errorf("checking AgentRuntime CR: %w", err)
	}

	return nil
}

// patchAuthbridgeConfig reads the kagenti-generated authbridge ConfigMap,
// unmarshals config.yaml as a generic map, calls patchFn to apply modifications,
// and writes back only if patchFn signals a change. Returns nil without error
// when the sidecar is inactive, AF is disabled, or the ConfigMap doesn't exist yet.
func (r *KubernautReconciler) patchAuthbridgeConfig(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, sidecar resources.KagentiSidecarMode, desc string, patchFn func(map[string]interface{}) (changed bool)) error {
	if sidecar == resources.KagentiSidecarNone || !kn.Spec.APIFrontendEnabled() {
		return nil
	}

	cmName := "authbridge-config-" + string(resources.ComponentAPIFrontend)
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: cmName}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("reading authbridge config %s: %w", cmName, err)
	}

	raw, ok := cm.Data["config.yaml"]
	if !ok {
		return nil
	}

	var full map[string]interface{}
	if err := sigsyaml.Unmarshal([]byte(raw), &full); err != nil {
		return fmt.Errorf("parsing authbridge config %s: %w", cmName, err)
	}

	if !patchFn(full) {
		return nil
	}

	patched, err := sigsyaml.Marshal(full)
	if err != nil {
		return fmt.Errorf("marshaling patched authbridge config: %w", err)
	}

	cm.Data["config.yaml"] = string(patched)
	if err := r.Update(ctx, cm); err != nil {
		return fmt.Errorf("patching authbridge config %s with %s: %w", cmName, desc, err)
	}

	logf.FromContext(ctx).Info("patched authbridge config", "configmap", cmName, "patch", desc)
	return nil
}

// ensureAuthbridgeMetricsBypass patches the kagenti-generated per-workload
// authbridge ConfigMap to add /metrics to bypass.inbound_paths. Without this,
// the envoy sidecar returns 401 on the metrics endpoint, breaking Prometheus
// scraping. Upstream fix tracked in kagenti/kagenti-extensions#524.
func (r *KubernautReconciler) ensureAuthbridgeMetricsBypass(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, sidecar resources.KagentiSidecarMode) error {
	return r.patchAuthbridgeConfig(ctx, kn, sidecar, "/metrics bypass", func(full map[string]interface{}) bool {
		bypassMap, _ := full["bypass"].(map[string]interface{})
		if bypassMap == nil {
			bypassMap = make(map[string]interface{})
			full["bypass"] = bypassMap
		}

		pathsRaw, _ := bypassMap["inbound_paths"].([]interface{})
		for _, p := range pathsRaw {
			if s, ok := p.(string); ok && s == "/metrics" {
				return false
			}
		}

		bypassMap["inbound_paths"] = append(pathsRaw, "/metrics")
		return true
	})
}

// ensureAuthbridgeClientID patches the kagenti-generated authbridge ConfigMap
// to include an inline identity.client_id (the AF's SPIFFE ID). Without this,
// the authbridge cannot validate the aud claim of inbound JWTs and rejects all
// tokens. This replaces the kagenti-client-registration sidecar which required
// keycloak-admin-secret in the app namespace (issue #171).
func (r *KubernautReconciler) ensureAuthbridgeClientID(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, sidecar resources.KagentiSidecarMode) error {
	return r.patchAuthbridgeConfig(ctx, kn, sidecar, "identity.client_id", func(full map[string]interface{}) bool {
		identityMap, _ := full["identity"].(map[string]interface{})
		if identityMap == nil {
			identityMap = make(map[string]interface{})
			full["identity"] = identityMap
		}

		wantID := resources.AFSpiffeID(kn)
		if current, _ := identityMap["client_id"].(string); current == wantID {
			return false
		}
		identityMap["client_id"] = wantID
		return true
	})
}

func (r *KubernautReconciler) deployAPIFrontendExtras(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	afHPA := resources.APIFrontendHPA(kn)
	if err := r.ensureNamespaced(ctx, kn, afHPA); err != nil {
		return fmt.Errorf("ensuring AF HPA: %w", err)
	}

	if err := r.ensureAPIFrontendMonitoring(ctx, kn); err != nil {
		return err
	}
	if err := r.ensureMCPGatewayResources(ctx, kn); err != nil {
		return err
	}
	return r.ensureAPIFrontendSPIFFEID(ctx, kn)
}

// ensureAPIFrontendMonitoring provisions the APIFrontend ServiceMonitor and
// PrometheusRule when the Prometheus Operator CRDs are installed.
func (r *KubernautReconciler) ensureAPIFrontendMonitoring(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	if !r.hasCRD(ctx, "servicemonitors.monitoring.coreos.com") {
		return nil
	}
	sm := resources.APIFrontendServiceMonitor(kn)
	if err := r.ensureNamespaced(ctx, kn, sm); err != nil {
		return fmt.Errorf("ensuring AF ServiceMonitor: %w", err)
	}
	pr := resources.APIFrontendPrometheusRule(kn)
	if err := r.ensureNamespaced(ctx, kn, pr); err != nil {
		return fmt.Errorf("ensuring AF PrometheusRule: %w", err)
	}
	return nil
}

// ensureMCPGatewayResources provisions the MCP HTTPRoute and
// MCPServerRegistration when the kagenti MCPServerRegistration CRD is
// installed and the builders determine one is needed (e.g. not BYO gateway).
func (r *KubernautReconciler) ensureMCPGatewayResources(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	if !r.hasCRD(ctx, "mcpserverregistrations.kagenti.dev") {
		return nil
	}
	route, err := resources.MCPGatewayHTTPRoute(kn)
	if err != nil {
		return fmt.Errorf("building MCP HTTPRoute: %w", err)
	}
	if route != nil {
		if err := r.ensureNamespaced(ctx, kn, route); err != nil {
			return fmt.Errorf("ensuring MCP HTTPRoute: %w", err)
		}
	}
	reg, err := resources.MCPServerRegistration(kn)
	if err != nil {
		return fmt.Errorf("building MCPServerRegistration: %w", err)
	}
	if reg != nil {
		if err := r.ensureNamespaced(ctx, kn, reg); err != nil {
			return fmt.Errorf("ensuring MCPServerRegistration: %w", err)
		}
	}
	return nil
}

// ensureAPIFrontendSPIFFEID provisions the ClusterSPIFFEID for APIFrontend
// when the SPIRE CRD is installed and the builder determines one is needed.
func (r *KubernautReconciler) ensureAPIFrontendSPIFFEID(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) error {
	if !r.hasCRD(ctx, "clusterspiffeids.spire.spiffe.io") {
		return nil
	}
	spiffeID, err := resources.ClusterSPIFFEID(kn)
	if err != nil {
		return fmt.Errorf("building ClusterSPIFFEID: %w", err)
	}
	if spiffeID != nil {
		if err := r.ensureUnowned(ctx, spiffeID); err != nil {
			return fmt.Errorf("ensuring ClusterSPIFFEID: %w", err)
		}
	}
	return nil
}

// ---------- Phase: Running ----------

func (r *KubernautReconciler) phaseRunning(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Update per-service status from Deployment readiness.
	var serviceStatuses []kubernautv1alpha1.ServiceStatus
	allReady := true
	for _, component := range resources.ActiveComponents(kn, knV2) {
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

	ansibleCond := r.validateAnsibleConfig(ctx, kn)
	amAuthCond := r.validateAlertManagerAuthConfig(ctx, kn)

	if err := r.patchStatus(ctx, kn, func() {
		kn.Status.Services = finalStatuses
		if finalAllReady {
			r.setPhase(kn, kubernautv1alpha1.PhaseRunning)
		} else {
			r.setPhase(kn, kubernautv1alpha1.PhaseDegraded)
		}
		meta.SetStatusCondition(&kn.Status.Conditions, ansibleCond)
		meta.SetStatusCondition(&kn.Status.Conditions, amAuthCond)
	}); err != nil {
		return ctrl.Result{}, err
	}

	if ansibleCond.Status == metav1.ConditionFalse {
		r.Recorder.Eventf(kn, nil, corev1.EventTypeWarning, "AnsibleConfigInvalid", "Reconcile",
			"%s: %s", ansibleCond.Reason, ansibleCond.Message)
	}
	if amAuthCond.Status == metav1.ConditionFalse {
		r.Recorder.Eventf(kn, nil, corev1.EventTypeWarning, "AlertManagerAuthNotConfigured", "Reconcile",
			"%s: %s", amAuthCond.Reason, amAuthCond.Message)
	}

	log.Info("reconciliation complete", "phase", kn.Status.Phase)

	if !finalAllReady {
		return ctrl.Result{RequeueAfter: requeueDegraded}, nil
	}

	return ctrl.Result{RequeueAfter: requeueRunning}, nil
}

// ---------- Deletion ----------

func (r *KubernautReconciler) reconcileDelete(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (ctrl.Result, error) {
	logf.FromContext(ctx).Info("reconciling deletion")

	if controllerutil.ContainsFinalizer(kn, kubernautv1alpha1.FinalizerName) {
		if result, err, retry := r.runFinalizerCleanup(ctx, kn, knV2); retry {
			return result, err
		}
	}

	return ctrl.Result{}, nil
}

// runFinalizerCleanup deletes cluster-scoped resources and removes the
// finalizer once cleanup succeeds. If cleanup keeps failing past
// maxFinalizerAttempts, it force-removes the finalizer instead of blocking
// deletion forever. retry is true when the caller should return immediately
// with (result, err) to requeue and try again later.
func (r *KubernautReconciler) runFinalizerCleanup(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (result ctrl.Result, err error, retry bool) {
	log := logf.FromContext(ctx)

	if err := r.deleteClusterScopedResources(ctx, kn, knV2); err != nil {
		deletionAge := r.now().Sub(kn.DeletionTimestamp.Time)
		if deletionAge <= time.Duration(maxFinalizerAttempts)*requeueError {
			return ctrl.Result{RequeueAfter: requeueError}, err, true
		}
		r.Recorder.Eventf(kn, nil, corev1.EventTypeWarning, "FinalizerTimeout", "Reconcile",
			"cleanup failed after %s; force-removing finalizer: %v", deletionAge.Round(time.Second), err)
		log.Error(err, "cleanup failed past timeout, force-removing finalizer")
	}
	r.Recorder.Eventf(kn, nil, corev1.EventTypeNormal, "CleanupComplete", "Reconcile", "Cluster-scoped resources cleaned up")

	// Write via knV2 (the hub/storage version), not kn: a full Update through
	// the v1alpha1 view would round-trip through ConvertTo, which has no
	// v1alpha1 source for Fleet/FleetMetadataCache and would zero them.
	controllerutil.RemoveFinalizer(knV2, kubernautv1alpha1.FinalizerName)
	if err := r.Update(ctx, knV2); err != nil {
		return ctrl.Result{}, err, true
	}
	return ctrl.Result{}, nil, false
}

// NOTE: CRDs installed during migration are intentionally NOT deleted here.
// They are cluster-scoped and potentially shared across namespaces; removing
// them would destroy all CRs of those types cluster-wide.
func (r *KubernautReconciler) deleteClusterScopedResources(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) error {
	log := logf.FromContext(ctx)
	errs := make([]error, 0, 4)
	errs = append(errs, r.deleteRBACResources(ctx, kn, knV2)...)
	errs = append(errs, r.deleteWebhookResources(ctx, kn)...)
	errs = append(errs, r.deleteWorkflowResources(ctx, kn)...)
	errs = append(errs, r.deleteSPIREResources(ctx, kn)...)
	errs = append(errs, r.deleteAgentRuntimeCR(ctx, kn)...)

	if len(errs) > 0 {
		return fmt.Errorf("cluster-scoped cleanup: %w", errors.Join(errs...))
	}

	log.Info("cluster-scoped resources cleaned up")
	return nil
}

func (r *KubernautReconciler) deleteSPIREResources(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) []error {
	if !r.hasCRD(ctx, "clusterspiffeids.spire.spiffe.io") {
		return nil
	}
	spiffeID, err := resources.ClusterSPIFFEID(kn)
	if err != nil || spiffeID == nil {
		return nil
	}
	if err := r.deleteIfExists(ctx, spiffeID); err != nil {
		return []error{fmt.Errorf("deleting ClusterSPIFFEID %s: %w", spiffeID.GetName(), err)}
	}
	return nil
}

func (r *KubernautReconciler) deleteAgentRuntimeCR(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) []error {
	if !r.hasCRD(ctx, "agentruntimes.agent.kagenti.dev") {
		return nil
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: agentRuntimeGVR.Group, Version: agentRuntimeGVR.Version, Kind: "AgentRuntime",
	})
	obj.SetName(string(resources.ComponentAPIFrontend))
	obj.SetNamespace(kn.Namespace)
	if err := r.deleteIfExists(ctx, obj); err != nil {
		return []error{fmt.Errorf("deleting AgentRuntime %s: %w", obj.GetName(), err)}
	}
	return nil
}

// deleteRBACResources removes all cluster-scoped RBAC: ClusterRoles, CRBs,
// AWX RBAC, and monitoring RBAC. Always attempts all resources regardless
// of current feature-flag state.
func (r *KubernautReconciler) deleteRBACResources(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []error {
	errs := make([]error, 0, 5)
	errs = append(errs, r.deleteCoreClusterRBAC(ctx, kn)...)
	errs = append(errs, r.deleteAnsibleClusterRBAC(ctx, kn)...)
	errs = append(errs, r.deleteMonitoringClusterRBAC(ctx, kn)...)
	errs = append(errs, r.deleteAdditionalAgentAndToolRBAC(ctx, kn)...)
	errs = append(errs, r.deleteMCPGatewayNamespaceRBAC(ctx, kn, knV2)...)
	return errs
}

// deleteCoreClusterRBAC removes every ClusterRole/ClusterRoleBinding this
// instance could ever have produced via resources.ClusterRoles()/
// ClusterRoleBindings() (#341), by label rather than by recomputing the
// current spec's desired list -- the instance is being deleted, so nothing
// computed from its current spec should survive, including objects a prior
// (now-changed) spec state left behind and that ClusterRoles()/
// ClusterRoleBindings() would no longer even enumerate (e.g. FMC's
// cluster-scoped-mode pair after a namespace retrofit).
func (r *KubernautReconciler) deleteCoreClusterRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) []error {
	return r.pruneOrphanedCoreClusterRBAC(ctx, kn, nil, nil)
}

// deleteAnsibleClusterRBAC removes the AWX ClusterRole/ClusterRoleBinding.
func (r *KubernautReconciler) deleteAnsibleClusterRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) []error {
	var errs []error
	cr, crb := resources.AnsibleRBAC(kn)
	if err := r.deleteIfExists(ctx, cr); err != nil {
		errs = append(errs, fmt.Errorf("deleting AWX ClusterRole: %w", err))
	}
	if err := r.deleteIfExists(ctx, crb); err != nil {
		errs = append(errs, fmt.Errorf("deleting AWX CRB: %w", err))
	}
	return errs
}

// deleteMonitoringClusterRBAC removes monitoring ClusterRoleBindings and
// ClusterRoles.
func (r *KubernautReconciler) deleteMonitoringClusterRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) []error {
	var errs []error
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

// deleteAdditionalAgentAndToolRBAC removes every additional-component
// ClusterRoleBinding (KA/Gateway/EM, #277) via the generic label-selector
// prune, all tool ClusterRoles and their bound ClusterRoleBindings, and the
// console-access CRB.
func (r *KubernautReconciler) deleteAdditionalAgentAndToolRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) []error {
	errs := r.pruneOrphanedAdditionalComponentRBAC(ctx, kn, nil)

	for _, name := range resources.ToolClusterRoleNames(kn) {
		toolCR := &rbacv1.ClusterRole{}
		toolCR.Name = name
		if err := r.deleteIfExists(ctx, toolCR); err != nil {
			errs = append(errs, fmt.Errorf("deleting tool ClusterRole %s: %w", name, err))
		}
	}
	for _, name := range kn.Status.BoundToolRoleBindings {
		toolCRB := &rbacv1.ClusterRoleBinding{}
		toolCRB.Name = name
		if err := r.deleteIfExists(ctx, toolCRB); err != nil {
			errs = append(errs, fmt.Errorf("deleting tool CRB %s: %w", name, err))
		}
	}

	// #289: the console-access ClusterRole itself is already covered by the
	// resources.ClusterRoles(kn) sweep above; only its CRB needs an explicit
	// static-name delete here.
	consoleCRB := &rbacv1.ClusterRoleBinding{}
	consoleCRB.Name = resources.ConsoleAccessCRBName(kn)
	if err := r.deleteIfExists(ctx, consoleCRB); err != nil {
		errs = append(errs, fmt.Errorf("deleting console-access CRB: %w", err))
	}

	return errs
}

// deleteMCPGatewayNamespaceRBAC removes every namespaced Role/RoleBinding
// used for MCP Gateway authorization, via the unconditional form of the
// (namespace, name)-keyed prune (#354). Deliberately does not recompute
// resources.MCPGatewayNamespaceRBAC(kn, knV2) from the CR's current spec:
// that would only find/delete whatever namespace the last-set
// mcpGatewayNamespace value resolves to, missing any namespace an earlier,
// never-reconciled spec change pointed at -- the label-selector list below
// catches those regardless of what the spec says at delete time.
func (r *KubernautReconciler) deleteMCPGatewayNamespaceRBAC(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, _ *kubernautv1alpha2.Kubernaut) []error {
	return r.pruneOrphanedMCPGatewayNamespaceRBAC(ctx, kn, nil, nil)
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

	if err := r.deleteOperatorManagedWorkflowNamespace(ctx, resources.WorkflowNamespace(kn)); err != nil {
		errs = append(errs, err)
	}

	return errs
}

// deleteOperatorManagedWorkflowNamespace deletes the workflow namespace only
// if it exists and was created by this operator (see the #208 backstop
// comment in deployWorkflowNamespace); a user-provided pre-existing
// namespace is left alone.
func (r *KubernautReconciler) deleteOperatorManagedWorkflowNamespace(ctx context.Context, wfNs *corev1.Namespace) error {
	existingNs := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: wfNs.Name}, existingNs); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("getting workflow namespace %s: %w", wfNs.Name, err)
	}

	if existingNs.Annotations[resources.AnnotationCreatedBy] != "kubernaut-operator" {
		logf.FromContext(ctx).Info("skipping deletion of workflow namespace (not created by operator)", "namespace", wfNs.Name)
		return nil
	}
	if err := r.deleteIfExists(ctx, existingNs); err != nil {
		return fmt.Errorf("deleting workflow namespace %s: %w", wfNs.Name, err)
	}
	return nil
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
//
// Fleet v1alpha2 migration: the patch is submitted against knV2 (the
// hub/storage version), not kn. Status().Patch on a CRD with a conversion
// webhook still round-trips the whole object through the requested
// apiVersion's Convert{To,From} -- submitting via kn (v1alpha1) would force
// ConvertTo to reconstruct spec.fleet with no v1alpha1 source, silently
// zeroing it. Status converts losslessly both ways (convertStatusToV1/V2),
// so re-deriving it from the mutated kn and layering it onto a freshly
// fetched knV2 keeps spec.fleet untouched while still patching only the
// changed status fields.
func (r *KubernautReconciler) patchStatus(ctx context.Context, kn *kubernautv1alpha1.Kubernaut, mutate func()) error {
	patched := kn.DeepCopy()
	mutate()

	knV2 := &kubernautv1alpha2.Kubernaut{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(kn), knV2); err != nil {
		*kn = *patched
		return err
	}
	patchedV2 := knV2.DeepCopy()
	scratch := &kubernautv1alpha2.Kubernaut{}
	if err := kn.ConvertTo(scratch); err != nil {
		*kn = *patched
		return fmt.Errorf("deriving v1alpha2 status from v1alpha1 view: %w", err)
	}
	knV2.Status = scratch.Status

	if err := r.Status().Patch(ctx, knV2, client.MergeFrom(patchedV2)); err != nil {
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

// validateAnsibleConfig evaluates the AnsibleReady condition based on the
// current CR spec and cluster state. It returns a condition to be set in the
// status patch. When ansible is disabled, it returns True/Disabled. When
// enabled, it validates the token Secret exists and contains the expected key.
// This method is non-blocking: it never returns an error or sets PhaseError.
func (r *KubernautReconciler) validateAnsibleConfig(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) metav1.Condition {
	if !kn.Spec.Ansible.Enabled {
		return metav1.Condition{
			Type:               kubernautv1alpha1.ConditionAnsibleReady,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonAnsibleDisabled,
			Message:            "Ansible integration is disabled",
			ObservedGeneration: kn.Generation,
		}
	}

	if kn.Spec.Ansible.TokenSecretRef == nil {
		return metav1.Condition{
			Type:               kubernautv1alpha1.ConditionAnsibleReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonAnsibleTokenNotFound,
			Message:            "spec.ansible.tokenSecretRef is not configured",
			ObservedGeneration: kn.Generation,
		}
	}

	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: kn.Namespace,
		Name:      kn.Spec.Ansible.TokenSecretRef.Name,
	}
	if err := r.Get(ctx, secretKey, secret); err != nil {
		return metav1.Condition{
			Type:               kubernautv1alpha1.ConditionAnsibleReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonAnsibleTokenNotFound,
			Message:            fmt.Sprintf("Secret %q not found", secretKey.Name),
			ObservedGeneration: kn.Generation,
		}
	}

	tokenKey := kn.Spec.Ansible.TokenSecretRef.Key
	if tokenKey == "" {
		tokenKey = "token"
	}
	if _, ok := secret.Data[tokenKey]; !ok {
		return metav1.Condition{
			Type:               kubernautv1alpha1.ConditionAnsibleReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonAnsibleTokenKeyMissing,
			Message:            fmt.Sprintf("Secret %q is missing key %q", secretKey.Name, tokenKey),
			ObservedGeneration: kn.Generation,
		}
	}

	return metav1.Condition{
		Type:               kubernautv1alpha1.ConditionAnsibleReady,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonAnsibleReady,
		Message:            "Ansible token Secret is valid",
		ObservedGeneration: kn.Generation,
	}
}

// validateAlertManagerAuthConfig evaluates the AlertManagerAuthConfigured
// condition (#377) based on spec.gateway.alertManagerTokenSecretName and
// cluster state. It mirrors validateAnsibleConfig's BYO-token-Secret
// validation shape. When Gateway is disabled the condition is vacuously
// True. When enabled but the field is unset, resources.GatewayAlertManagerConfig
// still renders a valid (unauthenticated) AlertmanagerConfig, so this
// reports False without blocking reconciliation -- non-blocking by design,
// matching validateAnsibleConfig's contract (never returns an error or sets
// PhaseError).
func (r *KubernautReconciler) validateAlertManagerAuthConfig(ctx context.Context, kn *kubernautv1alpha1.Kubernaut) metav1.Condition {
	if !kn.Spec.GatewayEnabled() {
		return metav1.Condition{
			Type:               kubernautv1alpha1.ConditionAlertManagerAuthConfigured,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonAlertManagerAuthGatewayDisabled,
			Message:            "Gateway is disabled",
			ObservedGeneration: kn.Generation,
		}
	}

	secretName := kn.Spec.Gateway.AlertManagerTokenSecretName
	if secretName == "" {
		return metav1.Condition{
			Type:   kubernautv1alpha1.ConditionAlertManagerAuthConfigured,
			Status: metav1.ConditionFalse,
			Reason: ReasonAlertManagerAuthNotConfigured,
			Message: "spec.gateway.alertManagerTokenSecretName is not set -- AlertManager's webhook calls to " +
				"Gateway will be unauthenticated and rejected; see docs/installation/03-deploy.md",
			ObservedGeneration: kn.Generation,
		}
	}

	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{Namespace: kn.Namespace, Name: secretName}
	if err := r.Get(ctx, secretKey, secret); err != nil {
		return metav1.Condition{
			Type:               kubernautv1alpha1.ConditionAlertManagerAuthConfigured,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonAlertManagerAuthSecretMissing,
			Message:            fmt.Sprintf("Secret %q not found", secretName),
			ObservedGeneration: kn.Generation,
		}
	}

	if _, ok := secret.Data["token"]; !ok {
		return metav1.Condition{
			Type:               kubernautv1alpha1.ConditionAlertManagerAuthConfigured,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonAlertManagerAuthKeyMissing,
			Message:            fmt.Sprintf("Secret %q is missing key %q", secretName, "token"),
			ObservedGeneration: kn.Generation,
		}
	}

	return metav1.Condition{
		Type:               kubernautv1alpha1.ConditionAlertManagerAuthConfigured,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonAlertManagerAuthReady,
		Message:            fmt.Sprintf("AlertManager gateway token Secret %q is valid", secretName),
		ObservedGeneration: kn.Generation,
	}
}

func (r *KubernautReconciler) setConditionAndRequeue(
	ctx context.Context, kn *kubernautv1alpha1.Kubernaut,
	condType string, reason, message string,
) (ctrl.Result, error) {
	if err := r.patchStatus(ctx, kn, func() {
		meta.SetStatusCondition(&kn.Status.Conditions, metav1.Condition{
			Type: condType, Status: metav1.ConditionFalse, Reason: reason, Message: message,
			ObservedGeneration: kn.Generation,
		})
		r.setPhase(kn, kubernautv1alpha1.PhaseError)
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

// clusterIngressDomain returns the cluster's ingress domain from
// ingresses.config.openshift.io/cluster (e.g. "apps.dev.example.com").
// Returns empty on non-OpenShift clusters or if the resource is unavailable.
func (r *KubernautReconciler) clusterIngressDomain(ctx context.Context) string {
	log := logf.FromContext(ctx)
	ingress := &configv1.Ingress{}
	if err := r.Get(ctx, client.ObjectKey{Name: "cluster"}, ingress); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			log.V(1).Info("Ingress config not found (non-OCP cluster), console redirect URL will use fallback domain")
			return ""
		}
		log.V(1).Info("failed to read Ingress config, console redirect URL will use fallback domain", "error", err)
		return ""
	}
	domain := ingress.Spec.Domain
	if domain != "" {
		log.V(1).Info("resolved cluster ingress domain", "domain", domain)
	}
	return domain
}

// hasCRD checks if a CRD with the given name exists in the cluster.
func (r *KubernautReconciler) hasCRD(ctx context.Context, crdName string) bool {
	_, err := r.RESTMapper().ResourceFor(schema.GroupVersionResource{
		Group: strings.SplitN(crdName, ".", 2)[1] + "",
	})
	if err == nil {
		return true
	}
	crd := &unstructured.Unstructured{}
	crd.SetAPIVersion("apiextensions.k8s.io/v1")
	crd.SetKind("CustomResourceDefinition")
	if err := r.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
		return false
	}
	return true
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
//
// When the spec-hash annotation matches (no spec change), ensureResource
// additionally checks for content drift on ConfigMaps — detecting external
// modifications that preserved the annotation but altered the data.
func (r *KubernautReconciler) ensureResource(ctx context.Context, obj client.Object) error {
	desiredHash := resources.SpecHash(obj)
	setHashAnnotation(obj, desiredHash)

	existing := obj.DeepCopyObject().(client.Object)
	key := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	err := r.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		created, err := r.createOrAdoptExisting(ctx, obj, existing, key)
		if err != nil {
			return err
		}
		if created {
			return nil
		}
	} else if err != nil {
		return fmt.Errorf("getting %s: %w", key, err)
	}

	if existing.GetAnnotations()[resources.AnnotationSpecHash] == desiredHash && !contentDrifted(obj, existing) {
		return nil
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// createOrAdoptExisting attempts to Create obj. If Create raced with another
// writer (AlreadyExists), it re-Gets the now-existing object into existing
// so the caller's update-or-skip comparison has current state to work with.
// created is true when Create succeeded and no further action is needed.
func (r *KubernautReconciler) createOrAdoptExisting(ctx context.Context, obj, existing client.Object, key types.NamespacedName) (bool, error) {
	if err := r.Create(ctx, obj); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return false, err
		}
		if err := r.Get(ctx, key, existing); err != nil {
			return false, fmt.Errorf("getting %s after AlreadyExists: %w", key, err)
		}
		return false, nil
	}
	return true, nil
}

// contentDrifted performs a targeted comparison of operator-managed content
// for types that are susceptible to external data modification without
// annotation changes (e.g., ConfigMaps edited by users or tooling).
//
// Only keys present in the desired object are compared; extra keys in the
// live object (e.g., OCP-injected service-ca.crt) are ignored to prevent
// false positives. Returns false for non-ConfigMap types.
func contentDrifted(desired, existing client.Object) bool {
	desiredCM, ok := desired.(*corev1.ConfigMap)
	if !ok {
		return false
	}
	existingCM, ok := existing.(*corev1.ConfigMap)
	if !ok {
		return false
	}

	for key, val := range desiredCM.Data {
		if existingCM.Data[key] != val {
			return true
		}
	}

	for key, val := range desiredCM.BinaryData {
		existingVal, exists := existingCM.BinaryData[key]
		if !exists || !bytes.Equal(val, existingVal) {
			return true
		}
	}

	return false
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

// clearStaleServingCertErrors checks whether a Service with the OCP
// serving-cert annotation has stale generation-error annotations (e.g. after
// a service rename) and clears them so the service-CA controller retries
// certificate generation.
func (r *KubernautReconciler) clearStaleServingCertErrors(ctx context.Context, desired *corev1.Service) error {
	secretName, ok := desired.Annotations[resources.OCPServingCertAnnotation]
	if !ok || secretName == "" {
		return nil
	}

	live := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, live); err != nil {
		return client.IgnoreNotFound(err)
	}

	const errAnnotation = "service.beta.openshift.io/serving-cert-generation-error"
	const errNumAnnotation = "service.beta.openshift.io/serving-cert-generation-error-num"
	const alphaErrAnnotation = "service.alpha.openshift.io/serving-cert-generation-error"
	const alphaErrNumAnnotation = "service.alpha.openshift.io/serving-cert-generation-error-num"

	annotations := live.GetAnnotations()
	if annotations[errAnnotation] == "" && annotations[alphaErrAnnotation] == "" {
		return nil
	}

	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: desired.Namespace}, secret)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	log := logf.FromContext(ctx)
	log.Info("clearing stale serving-cert error annotations", "service", desired.Name, "secret", secretName)

	delete(annotations, errAnnotation)
	delete(annotations, errNumAnnotation)
	delete(annotations, alphaErrAnnotation)
	delete(annotations, alphaErrNumAnnotation)
	live.SetAnnotations(annotations)
	return r.Update(ctx, live)
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
	r.Recorder = mgr.GetEventRecorder("kubernaut-controller")
	if r.now == nil {
		r.now = time.Now
	}
	rl := workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Second, 5*time.Minute),
		&workqueue.TypedBucketRateLimiter[reconcile.Request]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
	)
	b := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
			RateLimiter:             rl,
		}).
		For(&kubernautv1alpha1.Kubernaut{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Secret{}).
		Owns(&batchv1.Job{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		Watches(&configv1.APIServer{},
			handler.EnqueueRequestsFromMapFunc(r.apiServerToKubernaut))

	if _, err := mgr.GetRESTMapper().RESTMapping(
		schema.GroupKind{Group: "monitoring.coreos.com", Kind: "ServiceMonitor"}); err == nil {
		b = b.Owns(&monitoringv1.ServiceMonitor{}).
			Owns(&monitoringv1.PrometheusRule{}).
			Owns(&monitoringv1alpha1.AlertmanagerConfig{})
	}

	return b.Named("kubernaut").
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

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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KubernautSpec defines the desired state of a Kubernaut deployment on OCP.
// The operator deploys all Kubernaut services into the CR's namespace and
// auto-derives OCP platform configuration (monitoring, service-ca, Routes).
type KubernautSpec struct {
	// Image pull policy, pull secrets, and optional per-component overrides.
	// +optional
	Image ImageSpec `json:"image,omitempty"`

	// BYO PostgreSQL connection. The operator validates the secret and derives
	// the DataStorage db-secrets.yaml Secret automatically.
	PostgreSQL PostgreSQLSpec `json:"postgresql"`

	// BYO Valkey/Redis connection.
	Valkey ValkeySpec `json:"valkey"`

	// Optional AWX/AAP integration for Ansible-based remediation workflows.
	// +optional
	Ansible AnsibleSpec `json:"ansible,omitempty"`

	// OCP monitoring integration (Prometheus, AlertManager).
	// +optional
	Monitoring MonitoringSpec `json:"monitoring,omitempty"`

	// Notification controller settings.
	// +optional
	Notification NotificationSpec `json:"notification,omitempty"`

	// AIAnalysis controller configuration.
	// +optional
	AIAnalysis AIAnalysisSpec `json:"aiAnalysis,omitempty"`

	// SignalProcessing controller configuration.
	// +optional
	SignalProcessing SignalProcessingSpec `json:"signalProcessing,omitempty"`

	// RemediationOrchestrator controller configuration.
	// +optional
	RemediationOrchestrator RemediationOrchestratorSpec `json:"remediationOrchestrator,omitempty"`

	// WorkflowExecution controller configuration.
	// +optional
	WorkflowExecution WorkflowExecutionSpec `json:"workflowExecution,omitempty"`

	// EffectivenessMonitor controller configuration.
	// +optional
	EffectivenessMonitor EffectivenessMonitorSpec `json:"effectivenessMonitor,omitempty"`

	// Kubernaut Agent (KA) -- LLM-powered investigation and analysis service.
	KubernautAgent KubernautAgentSpec `json:"kubernautAgent"`

	// Gateway service settings.
	// +optional
	Gateway GatewaySpec `json:"gateway,omitempty"`

	// AuthWebhook admission controller settings.
	// +optional
	AuthWebhook AuthWebhookSpec `json:"authWebhook,omitempty"`

	// DataStorage service settings.
	// +optional
	DataStorage DataStorageSpec `json:"dataStorage,omitempty"`
}

// ImageSpec configures container image policy for all services.
// Service images are resolved from RELATED_IMAGE_* environment variables
// set on the operator manager pod (populated at build time and rewritten
// by OLM for disconnected/mirrored registries). Use Overrides only for
// non-OLM or advanced deployments.
type ImageSpec struct {
	// Pull policy for all containers.
	// +kubebuilder:default="IfNotPresent"
	// +optional
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`

	// Pull secrets for private registries.
	// +optional
	PullSecrets []corev1.LocalObjectReference `json:"pullSecrets,omitempty"`

	// Per-component image overrides. Keys are component names
	// (e.g. "gateway", "datastorage", "kubernautagent"), values are full
	// image references (e.g. "myregistry.example.com/gateway:v1.4.0").
	// When set, overrides the RELATED_IMAGE env var for that component.
	// +optional
	Overrides map[string]string `json:"overrides,omitempty"`
}

// PostgreSQLSpec defines the BYO PostgreSQL connection.
type PostgreSQLSpec struct {
	// Name of the Secret containing PostgreSQL credentials.
	// Required keys: POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB.
	// +kubebuilder:validation:MinLength=1
	SecretName string `json:"secretName"`

	// PostgreSQL hostname or service DNS.
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`

	// PostgreSQL port.
	// +kubebuilder:default=5432
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`
}

// ValkeySpec defines the BYO Valkey/Redis connection.
type ValkeySpec struct {
	// Name of the Secret containing Valkey credentials.
	// Required key: valkey-secrets.yaml (YAML content: "password: <value>").
	// +kubebuilder:validation:MinLength=1
	SecretName string `json:"secretName"`

	// Valkey hostname or service DNS.
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`

	// Valkey port.
	// +kubebuilder:default=6379
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`
}

// AnsibleSpec configures the optional AWX/AAP integration.
// +kubebuilder:validation:XValidation:rule="!self.enabled || has(self.apiURL)",message="ansible.apiURL is required when ansible.enabled is true"
type AnsibleSpec struct {
	// Whether AWX/AAP integration is enabled.
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// AWX/AAP API URL. Required when Enabled is true.
	// +optional
	APIURL string `json:"apiURL,omitempty"`

	// AWX organization ID.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	OrganizationID int `json:"organizationID,omitempty"`

	// Reference to the Secret containing the AWX API token.
	// +optional
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`

	// CACertSecretRef references a Secret containing the PEM-encoded CA
	// certificate for the AAP/AWX API endpoint. Use this when the AAP uses
	// a self-signed certificate or a private CA. If omitted, the system
	// trust store is used.
	// +optional
	CACertSecretRef *CACertSecretRef `json:"caCertSecretRef,omitempty"`
}

// CACertSecretRef references a Secret containing a PEM-encoded CA certificate.
type CACertSecretRef struct {
	// Name of the Secret.
	Name string `json:"name"`
	// Key within the Secret containing the CA PEM.
	// +kubebuilder:default="ca.crt"
	// +optional
	Key string `json:"key,omitempty"`
}

// SecretKeyRef references a key within a Secret.
type SecretKeyRef struct {
	// Name of the Secret.
	Name string `json:"name"`
	// Key within the Secret. Default: "token".
	// +kubebuilder:default="token"
	// +optional
	Key string `json:"key,omitempty"`
}

// MonitoringSpec configures OCP monitoring integration.
type MonitoringSpec struct {
	// Whether OCP monitoring integration is enabled.
	// When true, the operator auto-derives Prometheus/AlertManager URLs
	// and provisions monitoring RBAC.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// NotificationSpec configures the notification controller.
type NotificationSpec struct {
	// Slack quickstart shortcut.
	// +optional
	Slack SlackSpec `json:"slack,omitempty"`

	// Optional routing ConfigMap reference for advanced notification routing.
	// Must contain key "routing.yaml" with Alertmanager-style routing rules.
	// +optional
	Routing *ConfigMapRef `json:"routing,omitempty"`

	// Resource requirements for the notification controller.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// SlackSpec configures Slack delivery for notifications.
type SlackSpec struct {
	// Name of the Secret containing the Slack webhook URL (key: "webhook-url").
	// Empty = no Slack, console-only delivery.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// Slack channel for notifications.
	// +kubebuilder:default="#kubernaut-alerts"
	// +optional
	Channel string `json:"channel,omitempty"`
}

// ConfigMapRef references a ConfigMap by name.
type ConfigMapRef struct {
	// Name of the ConfigMap.
	ConfigMapName string `json:"configMapName"`
}

// PolicyConfigMapRef references a ConfigMap containing a Rego policy.
type PolicyConfigMapRef struct {
	// Name of the ConfigMap.
	ConfigMapName string `json:"configMapName"`
}

// AIAnalysisSpec configures the AIAnalysis controller.
type AIAnalysisSpec struct {
	// Policy ConfigMap reference. Required.
	// The ConfigMap must contain key "approval.rego".
	Policy PolicyConfigMapRef `json:"policy"`

	// Optional confidence threshold override for the Rego policy.
	// Expressed as a decimal string, e.g. "0.85".
	// +optional
	ConfidenceThreshold string `json:"confidenceThreshold,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// SignalProcessingSpec configures the SignalProcessing controller.
type SignalProcessingSpec struct {
	// Policy ConfigMap reference. Required.
	// The ConfigMap must contain key "policy.rego".
	Policy PolicyConfigMapRef `json:"policy"`

	// Optional proactive signal mappings ConfigMap reference.
	// Must contain key "proactive-signal-mappings.yaml".
	// +optional
	ProactiveSignalMappings *ConfigMapRef `json:"proactiveSignalMappings,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// RemediationOrchestratorSpec configures the RemediationOrchestrator controller.
type RemediationOrchestratorSpec struct {
	// Timeout configuration for remediation phases.
	// +optional
	Timeouts ROTimeoutsSpec `json:"timeouts,omitempty"`

	// Routing thresholds for failure detection and cooldowns.
	// +optional
	Routing RORoutingSpec `json:"routing,omitempty"`

	// Effectiveness assessment configuration.
	// +optional
	EffectivenessAssessment ROEffectivenessSpec `json:"effectivenessAssessment,omitempty"`

	// Async propagation delay configuration.
	// +optional
	AsyncPropagation ROAsyncPropagationSpec `json:"asyncPropagation,omitempty"`

	// DryRun enables dry-run mode: the pipeline stops after AI analysis
	// without executing remediation workflows. Operators use this to
	// build confidence before enabling fully autonomous remediation.
	// +kubebuilder:default=false
	// +optional
	DryRun bool `json:"dryRun,omitempty"`

	// DryRunHoldPeriod suppresses re-triggering of the same signal after
	// a dry-run completion. Must be a valid Go duration string.
	// Only effective when DryRun is true.
	// +kubebuilder:default="1h"
	// +optional
	DryRunHoldPeriod string `json:"dryRunHoldPeriod,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// ROTimeoutsSpec defines phase-level timeouts for the RemediationOrchestrator.
type ROTimeoutsSpec struct {
	// +kubebuilder:default="1h"
	// +optional
	Global string `json:"global,omitempty"`
	// +kubebuilder:default="5m"
	// +optional
	Processing string `json:"processing,omitempty"`
	// +kubebuilder:default="10m"
	// +optional
	Analyzing string `json:"analyzing,omitempty"`
	// +kubebuilder:default="30m"
	// +optional
	Executing string `json:"executing,omitempty"`
	// +kubebuilder:default="30m"
	// +optional
	Verifying string `json:"verifying,omitempty"`
}

// RORoutingSpec defines routing thresholds for failure detection.
// Integer thresholds use pointers to distinguish zero from unset.
type RORoutingSpec struct {
	// +kubebuilder:default=3
	// +optional
	ConsecutiveFailureThreshold *int `json:"consecutiveFailureThreshold,omitempty"`
	// +kubebuilder:default="1h"
	// +optional
	ConsecutiveFailureCooldown string `json:"consecutiveFailureCooldown,omitempty"`
	// +kubebuilder:default="5m"
	// +optional
	RecentlyRemediatedCooldown string `json:"recentlyRemediatedCooldown,omitempty"`
	// +kubebuilder:default=3
	// +optional
	IneffectiveChainThreshold *int `json:"ineffectiveChainThreshold,omitempty"`
	// +kubebuilder:default=5
	// +optional
	RecurrenceCountThreshold *int `json:"recurrenceCountThreshold,omitempty"`
	// +kubebuilder:default="4h"
	// +optional
	IneffectiveTimeWindow string `json:"ineffectiveTimeWindow,omitempty"`
}

// ROEffectivenessSpec defines effectiveness assessment parameters.
type ROEffectivenessSpec struct {
	// +kubebuilder:default="5m"
	// +optional
	StabilizationWindow string `json:"stabilizationWindow,omitempty"`
}

// ROAsyncPropagationSpec defines async propagation delay settings.
type ROAsyncPropagationSpec struct {
	// +kubebuilder:default="3m"
	// +optional
	GitOpsSyncDelay string `json:"gitOpsSyncDelay,omitempty"`
	// +kubebuilder:default="1m"
	// +optional
	OperatorReconcileDelay string `json:"operatorReconcileDelay,omitempty"`
	// +kubebuilder:default="5m"
	// +optional
	ProactiveAlertDelay string `json:"proactiveAlertDelay,omitempty"`
}

// WorkflowExecutionSpec configures the WorkflowExecution controller.
type WorkflowExecutionSpec struct {
	// Namespace for workflow Job/PipelineRun execution.
	// +kubebuilder:default="kubernaut-workflows"
	// +optional
	WorkflowNamespace string `json:"workflowNamespace,omitempty"`

	// Cooldown period between workflow executions.
	// +kubebuilder:default="1m"
	// +optional
	CooldownPeriod string `json:"cooldownPeriod,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// EffectivenessMonitorSpec configures the EffectivenessMonitor controller.
type EffectivenessMonitorSpec struct {
	// Assessment windows for remediation effectiveness evaluation.
	// +optional
	Assessment EMAssessmentSpec `json:"assessment,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// EMAssessmentSpec defines effectiveness assessment windows.
type EMAssessmentSpec struct {
	// +kubebuilder:default="30s"
	// +optional
	StabilizationWindow string `json:"stabilizationWindow,omitempty"`
	// +kubebuilder:default="120s"
	// +optional
	ValidityWindow string `json:"validityWindow,omitempty"`
}

// KubernautAgentSpec configures the Kubernaut Agent (KA) LLM integration service.
type KubernautAgentSpec struct {
	// LLM provider and credentials configuration.
	LLM LLMSpec `json:"llm"`

	// AdditionalClusterRoleBindings is an optional list of pre-existing
	// ClusterRole names to bind to the Kubernaut Agent ServiceAccount.
	// Use this to grant KA read access to environment-specific CRDs
	// (e.g. Kafka, Knative, custom application resources) that the
	// base investigator ClusterRole does not cover.
	// The operator creates one ClusterRoleBinding per entry; it does
	// NOT create or manage the ClusterRoles themselves.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=set
	AdditionalClusterRoleBindings []string `json:"additionalClusterRoleBindings,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// LLMSpec defines the LLM provider configuration.
type LLMSpec struct {
	// LLM provider name (e.g. "openai", "anthropic").
	// +kubebuilder:validation:MinLength=1
	Provider string `json:"provider"`

	// LLM model name (e.g. "gpt-4o", "claude-sonnet-4-20250514").
	// +kubebuilder:validation:MinLength=1
	Model string `json:"model"`

	// Name of the Secret containing LLM API credentials.
	// +kubebuilder:validation:MinLength=1
	CredentialsSecretName string `json:"credentialsSecretName"`

	// Name of a pre-existing ConfigMap for full SDK configuration.
	// When set, overrides Provider and Model fields.
	// +optional
	SdkConfigMapName string `json:"sdkConfigMapName,omitempty"`
}

// GatewaySpec configures the Gateway service.
type GatewaySpec struct {
	// Route configuration for OCP external access.
	// +optional
	Route RouteSpec `json:"route,omitempty"`

	// Gateway server and middleware configuration.
	// +optional
	Config GatewayConfigSpec `json:"config,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// GatewayConfigSpec configures Gateway server behaviour, middleware, and CORS.
type GatewayConfigSpec struct {
	// Timeout for outbound K8s API requests. Default: "15s".
	// +kubebuilder:default="15s"
	// +optional
	K8sRequestTimeout string `json:"k8sRequestTimeout,omitempty"`

	// Trusted proxy CIDRs for X-Forwarded-For / RealIP extraction.
	// Empty = fail-closed (proxy headers never trusted).
	// +optional
	TrustedProxyCIDRs []string `json:"trustedProxyCIDRs,omitempty"`

	// CORS allowed origins. Gateway is an M2M webhook API, not a browser
	// target, so the default is a non-matching origin that blocks CORS.
	// +kubebuilder:default="https://no-browser-clients.invalid"
	// +optional
	CORSAllowedOrigins string `json:"corsAllowedOrigins,omitempty"`
}

// RouteSpec configures the OCP Route for the Gateway.
type RouteSpec struct {
	// Whether to create an OCP Route for the Gateway.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Hostname override. When empty, the OCP router auto-generates a hostname.
	// +optional
	Hostname string `json:"hostname,omitempty"`
}

// AuthWebhookSpec configures the AuthWebhook admission controller.
type AuthWebhookSpec struct {
	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// DataStorageSpec configures the DataStorage service.
type DataStorageSpec struct {
	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// ---------- Status ----------

// KubernautPhase represents the aggregate lifecycle phase.
// +kubebuilder:validation:Enum=Validating;Migrating;Deploying;Running;Degraded;Error
type KubernautPhase string

const (
	PhaseValidating KubernautPhase = "Validating"
	PhaseMigrating  KubernautPhase = "Migrating"
	PhaseDeploying  KubernautPhase = "Deploying"
	PhaseRunning    KubernautPhase = "Running"
	PhaseDegraded   KubernautPhase = "Degraded"
	PhaseError      KubernautPhase = "Error"
)

// KubernautStatus defines the observed state of a Kubernaut deployment.
type KubernautStatus struct {
	// Aggregate lifecycle phase.
	// +optional
	Phase KubernautPhase `json:"phase,omitempty"`

	// Standard conditions following the metav1.Condition contract.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Per-service readiness.
	// +optional
	Services []ServiceStatus `json:"services,omitempty"`

	// ClusterRole names for which the operator has created additional
	// agent ClusterRoleBindings. Used for stale-pruning on spec changes
	// and finalizer cleanup.
	// +optional
	BoundAdditionalClusterRoles []string `json:"boundAdditionalClusterRoles,omitempty"`
}

// ServiceStatus reports the readiness of a single managed service.
type ServiceStatus struct {
	// Service name (e.g. "gateway", "datastorage").
	Name string `json:"name"`
	// Whether the service has all desired replicas ready.
	Ready bool `json:"ready"`
	// Number of ready replicas.
	ReadyReplicas int32 `json:"readyReplicas"`
	// Desired number of replicas.
	DesiredReplicas int32 `json:"desiredReplicas"`
}

// ConditionType is a string alias for condition type names. It is an alias
// (not a distinct type) so these constants can be passed directly to
// metav1.Condition.Type without conversion.
type ConditionType = string

// Condition types used in KubernautStatus.Conditions.
const (
	ConditionBYOValidated        ConditionType = "BYOValidated"
	ConditionMigrationComplete   ConditionType = "MigrationComplete"
	ConditionCRDsInstalled       ConditionType = "CRDsInstalled"
	ConditionRBACProvisioned     ConditionType = "RBACProvisioned"
	ConditionWebhooksConfigured  ConditionType = "WebhooksConfigured"
	ConditionServicesDeployed    ConditionType = "ServicesDeployed"
	ConditionRouteReady          ConditionType = "RouteReady"
	ConditionAnsibleReady        ConditionType = "AnsibleReady"
	ConditionAdditionalRBACBound ConditionType = "AdditionalRBACBound"
)

// Finalizer used for cluster-scoped resource cleanup.
const FinalizerName = "kubernaut.ai/cleanup"

// SingletonName is the only accepted CR name; the reconciler rejects others.
// NOTE: The singleton guard operates at the namespace level. Two namespaces
// could each contain a CR named "kubernaut", and both controllers would
// compete over the same cluster-scoped resources (ClusterRoles, CRBs,
// webhook configurations). A validating admission webhook that enforces
// cluster-wide uniqueness is planned for a future release. Until then,
// only one Kubernaut CR should exist per cluster.
const SingletonName = "kubernaut"

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Kubernaut is the Schema for the kubernauts API.
// It declares a single Kubernaut deployment within the namespace it is created in.
type Kubernaut struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubernautSpec   `json:"spec,omitempty"`
	Status KubernautStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubernautList contains a list of Kubernaut.
type KubernautList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Kubernaut `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Kubernaut{}, &KubernautList{})
}

// MonitoringEnabled returns true when OCP monitoring integration is active.
func (s *MonitoringSpec) MonitoringEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// RouteEnabled returns true when the Gateway Route should be created.
func (s *RouteSpec) RouteEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

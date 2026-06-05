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

	// NetworkPolicies controls the creation of Kubernetes NetworkPolicy
	// resources that enforce a default-deny posture with explicit allow rules.
	// +optional
	NetworkPolicies NetworkPoliciesSpec `json:"networkPolicies,omitempty"`

	// APIFrontend configures the API Frontend (MCP/A2A gateway) service.
	APIFrontend APIFrontendSpec `json:"apiFrontend,omitempty"`
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

	// PostgreSQL SSL mode (disable, require, verify-ca, verify-full).
	// +kubebuilder:default="verify-full"
	// +kubebuilder:validation:Enum=require;verify-ca;verify-full
	// +optional
	SSLMode string `json:"sslMode,omitempty"`
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

	// TLS configures client-side TLS for the Valkey/Redis connection.
	// Server-side TLS provisioning is the platform admin's responsibility
	// (Valkey is BYO).
	// +optional
	TLS *ValkeyTLSSpec `json:"tls,omitempty"`
}

// ValkeyTLSSpec configures client-side TLS for BYO Valkey/Redis.
type ValkeyTLSSpec struct {
	// Whether TLS is enabled for the Valkey/Redis connection.
	Enabled bool `json:"enabled"`

	// Name of the Secret containing the CA certificate to verify the server.
	// Required key: ca.crt
	// +optional
	CASecretName string `json:"caSecretName,omitempty"`

	// Name of the Secret containing client certificate and key for mTLS.
	// Required keys: tls.crt, tls.key
	// +optional
	ClientCertSecretName string `json:"clientCertSecretName,omitempty"`
}

// ValkeyTLSEnabled returns true when Valkey TLS is configured and enabled.
func (v *ValkeySpec) ValkeyTLSEnabled() bool {
	return v.TLS != nil && v.TLS.Enabled
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

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

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

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

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

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

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

	// Notification behaviour for remediation events.
	// +optional
	Notifications RONotificationsSpec `json:"notifications,omitempty"`

	// Retention policy for completed remediation records.
	// +optional
	Retention RORetentionSpec `json:"retention,omitempty"`

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

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
	// +kubebuilder:default="15m"
	// +optional
	AwaitingApproval string `json:"awaitingApproval,omitempty"`
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
	// +kubebuilder:default="1m"
	// +optional
	ExponentialBackoffBase string `json:"exponentialBackoffBase,omitempty"`
	// +kubebuilder:default="10m"
	// +optional
	ExponentialBackoffMax string `json:"exponentialBackoffMax,omitempty"`
	// +kubebuilder:default=4
	// +optional
	ExponentialBackoffMaxExponent *int `json:"exponentialBackoffMaxExponent,omitempty"`
	// +kubebuilder:default="5s"
	// +optional
	ScopeBackoffBase string `json:"scopeBackoffBase,omitempty"`
	// +kubebuilder:default="5m"
	// +optional
	ScopeBackoffMax string `json:"scopeBackoffMax,omitempty"`
	// +kubebuilder:default=24
	// +optional
	NoActionRequiredDelayHours *int `json:"noActionRequiredDelayHours,omitempty"`
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

// RONotificationsSpec configures RO notification behaviour.
type RONotificationsSpec struct {
	// Whether to notify on self-resolved remediations.
	// +kubebuilder:default=false
	// +optional
	NotifySelfResolved bool `json:"notifySelfResolved,omitempty"`
}

// RORetentionSpec configures retention for completed remediation records.
type RORetentionSpec struct {
	// How long to retain completed remediation records.
	// +kubebuilder:default="24h"
	// +optional
	Period string `json:"period,omitempty"`
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

	// Tekton integration configuration.
	// +optional
	Tekton TektonSpec `json:"tekton,omitempty"`

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// TektonSpec configures Tekton PipelineRun integration for workflow execution.
type TektonSpec struct {
	// Whether Tekton integration is enabled. When nil, auto-detected.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// EffectivenessMonitorSpec configures the EffectivenessMonitor controller.
type EffectivenessMonitorSpec struct {
	// Assessment windows for remediation effectiveness evaluation.
	// +optional
	Assessment EMAssessmentSpec `json:"assessment,omitempty"`

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// EMAssessmentSpec defines effectiveness assessment windows.
type EMAssessmentSpec struct {
	// +kubebuilder:default="30s"
	// +optional
	StabilizationWindow string `json:"stabilizationWindow,omitempty"`
	// +kubebuilder:default="300s"
	// +optional
	ValidityWindow string `json:"validityWindow,omitempty"`
}

// KubernautAgentSpec configures the Kubernaut Agent (KA) LLM integration service.
type KubernautAgentSpec struct {
	// LLM provider and credentials configuration.
	LLM LLMSpec `json:"llm"`

	// MaxTurns is the maximum number of LLM conversation turns the
	// investigator may execute per analysis session.
	// +kubebuilder:default=40
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxTurns int `json:"maxTurns,omitempty"`

	// Session configuration.
	// +optional
	Session SessionSpec `json:"session,omitempty"`

	// Audit logging configuration.
	// +optional
	Audit AuditSpec `json:"audit,omitempty"`

	// Alignment check (shadow agent) configuration.
	// +optional
	AlignmentCheck AlignmentCheckSpec `json:"alignmentCheck,omitempty"`

	// Summarizer configuration for tool output compression.
	// +optional
	Summarizer SummarizerSpec `json:"summarizer,omitempty"`

	// Safety guardrails for LLM interactions.
	// +optional
	Safety SafetySpec `json:"safety,omitempty"`

	// Interactive mode JWT identity delegation configuration.
	// +optional
	Interactive *InteractiveSpec `json:"interactive,omitempty"`

	// AdditionalClusterRoleBindings is an optional list of pre-existing
	// ClusterRole names to bind to the Kubernaut Agent ServiceAccount.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=set
	AdditionalClusterRoleBindings []string `json:"additionalClusterRoleBindings,omitempty"`

	// Graceful shutdown configuration.
	// +optional
	Shutdown ShutdownSpec `json:"shutdown,omitempty"`

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// InteractiveSpec configures KA interactive mode with JWT-based identity delegation.
type InteractiveSpec struct {
	// Whether MCP interactive mode endpoint and Lease-based session
	// management are enabled. When true, KA exposes a Streamable HTTP
	// MCP endpoint at POST /api/v1/mcp.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Maximum duration for an interactive session before auto-release.
	// Must be a valid Go duration string (e.g. "30m").
	// +optional
	SessionTTL string `json:"sessionTTL,omitempty"`

	// Session timeout after last operator activity.
	// Must be a valid Go duration string (e.g. "10m").
	// +optional
	InactivityTimeout string `json:"inactivityTimeout,omitempty"`

	// Maximum concurrent interactive sessions per agent replica.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxConcurrentSessions *int `json:"maxConcurrentSessions,omitempty"`

	// Maximum MCP requests per second per authenticated user.
	// +kubebuilder:validation:Minimum=1
	// +optional
	RateLimitPerUser *int `json:"rateLimitPerUser,omitempty"`

	// JWT providers for OIDC-based identity delegation.
	// +optional
	// +kubebuilder:validation:MaxItems=8
	JWTProviders []JWTProviderSpec `json:"jwtProviders,omitempty"`

	// AllowInsecureJWKS permits HTTP (non-TLS) JWKS URLs for dev/test.
	// Production deployments MUST leave this false.
	// +optional
	AllowInsecureJWKS bool `json:"allowInsecureJWKS,omitempty"`
}

// InteractiveEnabled returns true when interactive mode is active.
// Defaults to true (nil Enabled) so investigations work out of the box
// when the API Frontend is deployed.
func (s *InteractiveSpec) InteractiveEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// JWTProviderSpec configures a single OIDC JWT provider.
type JWTProviderSpec struct {
	// Human-readable name for this provider (e.g. "rhbk", "auth0").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// JWKS endpoint URL. Must use HTTPS unless allowInsecureJWKS is true.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	JWKSURL string `json:"jwksURL"`

	// Expected audience claim value.
	// +optional
	Audience string `json:"audience,omitempty"`

	// Expected issuer claim value.
	// +optional
	Issuer string `json:"issuer,omitempty"`
}

// SessionSpec configures KA session behaviour.
type SessionSpec struct {
	// Session time-to-live.
	// +kubebuilder:default="30m"
	// +optional
	TTL string `json:"ttl,omitempty"`
}

// AuditSpec configures KA audit logging.
type AuditSpec struct {
	// Whether audit logging is enabled.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// AuditEnabled returns true when audit logging is active (default: true).
func (s *AuditSpec) AuditEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// AlignmentCheckSpec configures the shadow agent alignment check.
type AlignmentCheckSpec struct {
	// Whether alignment check is enabled.
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Timeout for alignment check requests.
	// +kubebuilder:default="10s"
	// +optional
	Timeout string `json:"timeout,omitempty"`

	// Maximum tokens per alignment step.
	// +kubebuilder:default=500
	// +optional
	MaxStepTokens int `json:"maxStepTokens,omitempty"`

	// Optional dedicated LLM for alignment checks.
	// +optional
	LLM *AlignmentCheckLLMSpec `json:"llm,omitempty"`
}

// AlignmentCheckLLMSpec configures a dedicated LLM for the alignment check shadow agent.
type AlignmentCheckLLMSpec struct {
	// +optional
	Provider string `json:"provider,omitempty"`
	// +optional
	Model string `json:"model,omitempty"`
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
	// +optional
	APIKey string `json:"apiKey,omitempty"`
}

// SummarizerSpec configures tool output summarization thresholds.
type SummarizerSpec struct {
	// Token threshold above which tool output is summarized.
	// +kubebuilder:default=8000
	// +optional
	Threshold int `json:"threshold,omitempty"`
	// Maximum tool output size in bytes before truncation.
	// +kubebuilder:default=100000
	// +optional
	MaxToolOutputSize int `json:"maxToolOutputSize,omitempty"`
}

// SafetySpec configures LLM safety guardrails.
type SafetySpec struct {
	// Input sanitization rules.
	// +optional
	Sanitization SanitizationSpec `json:"sanitization,omitempty"`
	// Anomaly detection thresholds.
	// +optional
	Anomaly AnomalySpec `json:"anomaly,omitempty"`
}

// SanitizationSpec configures input sanitization.
type SanitizationSpec struct {
	// Whether prompt injection pattern detection is enabled.
	// +kubebuilder:default=true
	// +optional
	InjectionPatternsEnabled *bool `json:"injectionPatternsEnabled,omitempty"`
	// Whether credential scrubbing is enabled.
	// +kubebuilder:default=true
	// +optional
	CredentialScrubEnabled *bool `json:"credentialScrubEnabled,omitempty"`
}

// AnomalySpec configures tool call anomaly detection.
type AnomalySpec struct {
	// Max tool calls per individual tool.
	// +kubebuilder:default=10
	// +optional
	MaxToolCallsPerTool *int `json:"maxToolCallsPerTool,omitempty"`
	// Max total tool calls across all tools.
	// +kubebuilder:default=40
	// +optional
	MaxTotalToolCalls *int `json:"maxTotalToolCalls,omitempty"`
	// Max repeated failures before circuit-breaker.
	// +kubebuilder:default=3
	// +optional
	MaxRepeatedFailures *int `json:"maxRepeatedFailures,omitempty"`
}

// LLMSpec defines the LLM provider configuration.
type LLMSpec struct {
	// LLM provider name (e.g. "openai", "vertexai", "bedrock", "azure").
	// +kubebuilder:validation:MinLength=1
	Provider string `json:"provider"`

	// LLM model name (e.g. "gpt-4o", "gemini-2.5-pro").
	// +kubebuilder:validation:MinLength=1
	Model string `json:"model"`

	// Name of the Secret containing LLM API credentials.
	// +kubebuilder:validation:MinLength=1
	CredentialsSecretName string `json:"credentialsSecretName"`

	// LLM API endpoint override. When empty, uses the provider default.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Sampling temperature for LLM responses (e.g. "0.7").
	// Serialized as string to avoid CRD float portability issues.
	// +optional
	Temperature string `json:"temperature,omitempty"`

	// Maximum number of retries for LLM API calls.
	// +kubebuilder:default=3
	// +optional
	MaxRetries *int `json:"maxRetries,omitempty"`

	// Timeout in seconds for LLM API calls.
	// +kubebuilder:default=120
	// +optional
	TimeoutSeconds *int `json:"timeoutSeconds,omitempty"`

	// GCP Vertex AI project ID.
	// +optional
	VertexProject string `json:"vertexProject,omitempty"`

	// GCP Vertex AI location (e.g. "us-central1").
	// +optional
	VertexLocation string `json:"vertexLocation,omitempty"`

	// AWS Bedrock region.
	// +optional
	BedrockRegion string `json:"bedrockRegion,omitempty"`

	// Azure OpenAI API version.
	// +optional
	AzureAPIVersion string `json:"azureApiVersion,omitempty"`

	// Path to a CA certificate file for TLS to the LLM endpoint.
	// +optional
	TLSCaFile string `json:"tlsCaFile,omitempty"`

	// Path to a client certificate file for mTLS to the LLM endpoint.
	// Must be set together with TLSKeyFile.
	// +optional
	TLSCertFile string `json:"tlsCertFile,omitempty"`

	// Path to a client key file for mTLS to the LLM endpoint.
	// Must be set together with TLSCertFile.
	// +optional
	TLSKeyFile string `json:"tlsKeyFile,omitempty"`

	// Name of the Secret containing the TLS client certificate and key
	// for mTLS to the LLM endpoint. The Secret must contain tls.crt and
	// tls.key entries. Required when TLSCertFile and TLSKeyFile are set.
	// +optional
	TLSClientSecretRef string `json:"tlsClientSecretRef,omitempty"`

	// OAuth2 configuration for LLM authentication.
	// +optional
	OAuth2 OAuth2Spec `json:"oauth2,omitempty"`

	// Name of a pre-existing ConfigMap for the LLM runtime configuration.
	// When set, the operator skips generating kubernaut-agent-llm-runtime
	// and mounts this ConfigMap instead. Must contain key "llm-runtime.yaml".
	// +optional
	RuntimeConfigMapName string `json:"runtimeConfigMapName,omitempty"`
}

// OAuth2Spec configures OAuth2 token-based authentication for LLM endpoints.
type OAuth2Spec struct {
	// Whether OAuth2 authentication is enabled.
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Token endpoint URL.
	// +optional
	TokenURL string `json:"tokenURL,omitempty"`
	// OAuth2 scopes.
	// +optional
	Scopes []string `json:"scopes,omitempty"`
	// Name of the Secret containing OAuth2 client credentials
	// (keys: "client-id", "client-secret").
	// +optional
	CredentialsSecretRef string `json:"credentialsSecretRef,omitempty"`
}

// GatewaySpec configures the Gateway service.
type GatewaySpec struct {
	// Whether the Gateway component is deployed. Defaults to true.
	// Set to false to skip all Gateway resources (Deployment, Service, RBAC, etc.).
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Route configuration for OCP external access.
	// +optional
	Route RouteSpec `json:"route,omitempty"`

	// Gateway server and middleware configuration.
	// +optional
	Config GatewayConfigSpec `json:"config,omitempty"`

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

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

	// CORS configuration. Gateway is an M2M webhook API, not a browser
	// target, so the defaults block all cross-origin requests.
	// +optional
	CORS GatewayCORSSpec `json:"cors,omitempty"`

	// Deduplication cooldown period for alert processing.
	// +kubebuilder:default="5m"
	// +optional
	DeduplicationCooldown string `json:"deduplicationCooldown,omitempty"`
}

// GatewayCORSSpec configures CORS for the Gateway HTTP API.
type GatewayCORSSpec struct {
	// Allowed origins for CORS requests.
	// Default: ["https://no-browser-clients.invalid"] (blocks all browser clients).
	// +optional
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`

	// HTTP methods allowed for cross-origin requests.
	// Default: ["GET","POST","PUT","PATCH","DELETE","OPTIONS"].
	// +optional
	AllowedMethods []string `json:"allowedMethods,omitempty"`

	// Whether cross-origin requests may include credentials.
	// +kubebuilder:default=false
	// +optional
	AllowCredentials *bool `json:"allowCredentials,omitempty"`

	// Preflight cache duration in seconds.
	// +kubebuilder:default=300
	// +optional
	MaxAge *int `json:"maxAge,omitempty"`
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

// APIFrontendRouteSpec configures the OCP Route for the API Frontend.
// Unlike GatewayRouteSpec, defaults to disabled (opt-in external access).
type APIFrontendRouteSpec struct {
	// Whether to create an OCP Route for the API Frontend.
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Hostname override. When empty, the OCP router auto-generates a hostname.
	// +optional
	Hostname string `json:"hostname,omitempty"`
}

// AFRouteEnabled returns true when the AF Route should be created.
// Defaults to false when Enabled is nil (opt-in).
func (s *APIFrontendRouteSpec) AFRouteEnabled() bool {
	return s.Enabled != nil && *s.Enabled
}

// APIFrontendSPIRESpec configures SPIRE mTLS identity for kagenti agent card
// verified fetch. The operator creates a ClusterSPIFFEID and injects a
// SPIRE-aware mTLS sidecar into the AF deployment.
type APIFrontendSPIRESpec struct {
	// Whether SPIRE mTLS sidecar injection is enabled.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// SPIRE class name for the ClusterSPIFFEID (e.g. "zero-trust-workload-identity-manager-spire").
	// When empty, the className field is omitted from the ClusterSPIFFEID spec.
	// +optional
	ClassName string `json:"className,omitempty"`

	// TrustDomain overrides the SPIFFE ID trust domain. When empty (default),
	// the operator uses SPIRE's {{ .TrustDomain }} template variable, which
	// resolves to the cluster's configured trust domain at SVID registration
	// time. Set this only if you need a fixed trust domain that differs from
	// the SPIRE server's.
	// +optional
	TrustDomain string `json:"trustDomain,omitempty"`
}

// SPIREEnabled returns true when SPIRE mTLS sidecar injection is active.
func (s *APIFrontendSPIRESpec) SPIREEnabled() bool {
	return s.Enabled
}

// AuthWebhookSpec configures the AuthWebhook admission controller.
type AuthWebhookSpec struct {
	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// APIFrontendSpec configures the API Frontend (MCP Streamable HTTP / A2A) service.
// The API Frontend provides external access to Kubernaut Agent via MCP and A2A
// protocols with OIDC authentication, rate limiting, and RBAC-scoped tool access.
type APIFrontendSpec struct {
	// Whether the API Frontend component is deployed. Defaults to true.
	// Set to false to skip all AF resources (Deployment, Service, RBAC, etc.).
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Route configuration for OCP external access (FedRAMP SC-8).
	// Disabled by default; set route.enabled=true to expose AF via an
	// OpenShift Route with reencrypt TLS termination.
	// +optional
	Route APIFrontendRouteSpec `json:"route,omitempty"`

	// SPIRE mTLS identity configuration for kagenti agent card discovery
	// (FedRAMP SC-8, IA-5). When enabled, a ClusterSPIFFEID is created and
	// a SPIRE-aware mTLS sidecar is injected into the AF deployment so the
	// kagenti-operator can perform verified fetch with identity binding.
	// +optional
	SPIRE APIFrontendSPIRESpec `json:"spire,omitempty"`

	// OIDC authentication configuration.
	// +optional
	Auth APIFrontendAuthSpec `json:"auth,omitempty"`

	// Request rate limiting configuration.
	// +optional
	RateLimit APIFrontendRateLimitSpec `json:"rateLimit,omitempty"`

	// Graceful shutdown configuration.
	// +optional
	Shutdown APIFrontendShutdownSpec `json:"shutdown,omitempty"`

	// Display name for the A2A agent card (/.well-known/agent-card.json).
	// External URL for the A2A agent card discovery endpoint.
	// When empty, auto-derived from the in-cluster service FQDN.
	// Must be a valid HTTPS URL when set.
	// +kubebuilder:validation:Pattern=`^$|^https?://`
	// +optional
	AgentCardURL string `json:"agentCardURL,omitempty"`

	// Reference to a pre-existing ConfigMap containing RBAC role-to-tool
	// mappings (key: "rbac_roles.yaml"). When empty, the operator generates
	// a default RBAC roles ConfigMap.
	// Deprecated: replaced by RBAC field with SAR-based tool authorization.
	// +optional
	RBACRolesConfigMapRef *ConfigMapRef `json:"rbacRolesConfigMapRef,omitempty"`

	// SAR-based RBAC configuration for tool authorization.
	// When set, the operator provisions persona-based tool ClusterRoles
	// and group-to-role ClusterRoleBindings instead of file-based RBAC.
	// +optional
	RBAC *APIFrontendRBACSpec `json:"rbac,omitempty"`

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

	// Override for the AF metrics port. Defaults to 9090 (or 9092 when
	// kagenti sidecar port shifting is active). Use when cluster policies
	// restrict port ranges.
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	// +optional
	MetricsPort *int32 `json:"metricsPort,omitempty"`

	// Override for the AF health probe port. Defaults to 8081 (or 8082 when
	// kagenti sidecar port shifting is active). Use when cluster policies
	// restrict port ranges.
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	// +optional
	HealthPort *int32 `json:"healthPort,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// APIFrontendRBACSpec configures SAR-based tool authorization for the API Frontend.
type APIFrontendRBACSpec struct {
	// SARCacheTTL is the cache duration for SubjectAccessReview results.
	// Must be a valid Go duration string (e.g. "30s", "2m").
	// +kubebuilder:default="30s"
	// +optional
	SARCacheTTL string `json:"sarCacheTTL,omitempty"`

	// RoleBindings maps persona-based tool roles to OIDC groups.
	// +optional
	RoleBindings []ToolRoleBinding `json:"roleBindings,omitempty"`
}

// ToolRoleBinding binds a persona-based tool role to one or more OIDC groups.
type ToolRoleBinding struct {
	// Role is the persona name. Must be one of: sre, ai-orchestrator, cicd,
	// observability, l3-audit, remediation-approver.
	// +kubebuilder:validation:Enum=sre;ai-orchestrator;cicd;observability;l3-audit;remediation-approver
	Role string `json:"role"`

	// Groups are the OIDC group names to bind to this role.
	// +kubebuilder:validation:MinItems=1
	Groups []string `json:"groups"`
}

// APIFrontendAuthSpec configures OIDC authentication for the API Frontend.
type APIFrontendAuthSpec struct {
	// OIDC issuer URL (e.g. "https://login.kubernaut.ai/realms/kubernaut").
	// +optional
	IssuerURL string `json:"issuerURL,omitempty"`

	// Expected JWT audience claim (FedRAMP SC-23: session authenticity).
	// +kubebuilder:default="kubernaut-apifrontend"
	// +optional
	Audience string `json:"audience,omitempty"`

	// TokenReview audience for Kubernetes ServiceAccount token validation.
	// When set, the API Frontend passes this audience to the TokenReview API
	// so only tokens issued for this specific audience are accepted
	// (FedRAMP IA-5: authenticator management).
	// +optional
	TokenReviewAudience string `json:"tokenReviewAudience,omitempty"`

	// Explicit JWKS endpoint URL for token signature verification
	// (FedRAMP IA-5: authenticator management). When empty, derived from
	// issuerURL + "/protocol/openid-connect/certs".
	// +optional
	JWKSURL string `json:"jwksURL,omitempty"`

	// Path to CA bundle for OIDC/JWKS TLS trust (FedRAMP IA-5). When set,
	// AF uses this CA to verify the OIDC provider's certificate chain.
	// +optional
	OIDCCAFile string `json:"oidcCaFile,omitempty"`

	// Allow HTTP (non-TLS) JWKS URLs. Must remain false in production
	// (FedRAMP SC-8: transmission confidentiality). Intended for dev/test only.
	// +optional
	AllowInsecureIssuers bool `json:"allowInsecureIssuers,omitempty"`
}

// APIFrontendRateLimitSpec configures request rate limiting for the API Frontend.
type APIFrontendRateLimitSpec struct {
	// Per-IP requests per second.
	// +kubebuilder:default=50
	// +optional
	IPRequestsPerSec *int `json:"ipRequestsPerSec,omitempty"`

	// Per-user requests per second.
	// +kubebuilder:default=20
	// +optional
	UserRequestsPerSec *int `json:"userRequestsPerSec,omitempty"`

	// Maximum concurrent MCP/A2A sessions.
	// +kubebuilder:default=100
	// +optional
	MaxConcurrentSessions *int `json:"maxConcurrentSessions,omitempty"`

	// Tool calls per minute per user.
	// +kubebuilder:default=60
	// +optional
	ToolCallsPerMinute *int `json:"toolCallsPerMinute,omitempty"`
}

// ShutdownSpec configures graceful shutdown for a service component.
// Shared by API Frontend and Kubernaut Agent for consistent knob naming.
type ShutdownSpec struct {
	// Seconds to wait for in-flight requests to drain during shutdown.
	// +kubebuilder:default=15
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=300
	// +optional
	DrainSeconds *int `json:"drainSeconds,omitempty"`
}

// APIFrontendShutdownSpec is an alias retained for CRD backward compatibility.
type APIFrontendShutdownSpec = ShutdownSpec

// APIFrontendEnabled returns whether the API Frontend component should be deployed.
// Defaults to true when Enabled is nil.
func (s *KubernautSpec) APIFrontendEnabled() bool {
	return s.APIFrontend.Enabled == nil || *s.APIFrontend.Enabled
}

// GatewayEnabled returns whether the Gateway component should be deployed.
// Defaults to true when Enabled is nil.
func (s *KubernautSpec) GatewayEnabled() bool {
	return s.Gateway.Enabled == nil || *s.Gateway.Enabled
}

// DataStorageSpec configures the DataStorage service.
type DataStorageSpec struct {
	// EndpointPropagationDelay is the delay before newly created endpoints
	// are considered ready. Prevents traffic routing to pods that haven't
	// finished warming up. Must be a valid Go duration string.
	// +kubebuilder:default="10s"
	// +optional
	EndpointPropagationDelay string `json:"endpointPropagationDelay,omitempty"`

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

	// Resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Retention configures periodic purge of expired audit events (FedRAMP AU-11).
	// +optional
	Retention *RetentionSpec `json:"retention,omitempty"`

	// SigningCert configures the audit export signing certificate (FedRAMP AU-9).
	// When set, the named Secret is mounted into the DS pod at /etc/certs.
	// +optional
	SigningCert *SigningCertSpec `json:"signingCert,omitempty"`
}

// SigningCertSpec configures the audit export signing certificate.
type SigningCertSpec struct {
	// Name of the Kubernetes Secret containing the signing cert (tls.crt, tls.key).
	SecretName string `json:"secretName"`

	// Mount path inside the container. Defaults to /etc/certs.
	// +kubebuilder:default="/etc/certs"
	// +optional
	MountPath string `json:"mountPath,omitempty"`
}

// RetentionSpec configures audit event retention and purge for FedRAMP AU-11.
type RetentionSpec struct {
	// Whether the retention purge worker is active.
	// Defaults to false (safe default — no data is deleted without opt-in).
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// How often the purge worker runs. Must be a valid Go duration string.
	// +kubebuilder:default="24h"
	// +optional
	Interval string `json:"interval,omitempty"`

	// Maximum number of rows deleted per batch.
	// +kubebuilder:default=1000
	// +kubebuilder:validation:Minimum=1
	// +optional
	BatchSize *int `json:"batchSize,omitempty"`

	// Number of days to retain audit events before purge.
	// Clamped to a maximum of 2555 (≈7 years per ADR-034 / SOC 2 / ISO 27001).
	// +kubebuilder:default=2555
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=2555
	// +optional
	DefaultDays *int `json:"defaultDays,omitempty"`
}

// LoggingSpec configures the log level for a service.
type LoggingSpec struct {
	// Log level. One of: DEBUG, INFO, WARN, ERROR.
	// +kubebuilder:default="info"
	// +kubebuilder:validation:Enum=DEBUG;INFO;WARN;ERROR;debug;info;warn;error
	// +optional
	Level string `json:"level,omitempty"`
}

// NetworkPoliciesSpec controls creation of Kubernetes NetworkPolicy resources.
type NetworkPoliciesSpec struct {
	// Whether the operator creates NetworkPolicy resources.
	// When true, a default-deny posture is applied with explicit allow rules
	// matching the upstream Helm chart traffic matrix.
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// API server CIDR for egress rules (e.g. "10.0.0.0/16").
	// When empty, API server egress rules are omitted.
	// +optional
	APIServerCIDR string `json:"apiServerCIDR,omitempty"`

	// Monitoring namespace for Prometheus scrape ingress (e.g. "openshift-monitoring").
	// +optional
	MonitoringNamespace string `json:"monitoringNamespace,omitempty"`

	// Gateway ingress namespaces. Namespaces allowed to send traffic to the Gateway.
	// +optional
	GatewayIngressNamespaces []string `json:"gatewayIngressNamespaces,omitempty"`

	// IngressNamespace is the namespace where the OpenShift Router (or ingress
	// controller) runs. AF needs ingress from this namespace for Route-based traffic.
	// Typically "openshift-ingress" on OCP.
	// +optional
	IngressNamespace string `json:"ingressNamespace,omitempty"`

	// ExternalEgressCIDRs lists CIDRs that AF is allowed to reach for external
	// services (OIDC/Keycloak JWKS endpoints, etc.) on port 443.
	// Example: ["10.128.0.0/14"] for cluster-internal Keycloak, or
	// ["0.0.0.0/0"] to allow any external HTTPS destination.
	// +optional
	ExternalEgressCIDRs []string `json:"externalEgressCIDRs,omitempty"`
}

// NetworkPoliciesEnabled returns true when NP creation is active (default: false).
func (s *NetworkPoliciesSpec) NetworkPoliciesEnabled() bool {
	return s.Enabled != nil && *s.Enabled
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

	// Hash of the last successfully completed migration Job spec.
	// Used to skip re-running migration when the Job has been deleted
	// (e.g. TTL cleanup, manual deletion) but nothing has changed.
	// +optional
	LastMigrationHash string `json:"lastMigrationHash,omitempty"`

	// Timestamp of the last successfully completed migration.
	// +optional
	LastMigrationTime *metav1.Time `json:"lastMigrationTime,omitempty"`

	// ClusterRole names for which the operator has created additional
	// agent ClusterRoleBindings. Used for stale-pruning on spec changes
	// and finalizer cleanup.
	// +optional
	BoundAdditionalClusterRoles []string `json:"boundAdditionalClusterRoles,omitempty"`

	// BoundToolRoleBindings tracks the set of tool role binding CRB names
	// currently managed by the operator for stale-pruning and finalizer cleanup.
	// +optional
	BoundToolRoleBindings []string `json:"boundToolRoleBindings,omitempty"`
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
	ConditionToolRBACBound       ConditionType = "ToolRBACBound"
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

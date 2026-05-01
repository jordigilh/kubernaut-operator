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

package resources

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// controllerConfig holds controller-runtime style settings shared by the
// AIAnalysis, SignalProcessing, and Notification controllers.
type controllerConfig struct {
	MetricsAddr      string `json:"metricsAddr" yaml:"metricsAddr"`
	HealthProbeAddr  string `json:"healthProbeAddr" yaml:"healthProbeAddr"`
	LeaderElection   bool   `json:"leaderElection" yaml:"leaderElection"`
	LeaderElectionID string `json:"leaderElectionId" yaml:"leaderElectionId"`
}

// controllerBlock nests controllerConfig fields under the YAML mapping key "controller".
type controllerBlock struct {
	controllerConfig `json:",inline" yaml:",inline"`
}

func newControllerBlock(leaderElectionID string) controllerBlock {
	return controllerBlock{controllerConfig: controllerConfig{
		MetricsAddr:      ":9090",
		HealthProbeAddr:  ":8081",
		LeaderElection:   false,
		LeaderElectionID: leaderElectionID,
	}}
}

type tlsConfigYAML struct {
	CertDir string `json:"certDir" yaml:"certDir"`
}

type gatewayServerYAML struct {
	ListenAddr            string `json:"listenAddr" yaml:"listenAddr"`
	HealthAddr            string `json:"healthAddr" yaml:"healthAddr"`
	MetricsAddr           string `json:"metricsAddr" yaml:"metricsAddr"`
	MaxConcurrentRequests int    `json:"maxConcurrentRequests" yaml:"maxConcurrentRequests"`
	ReadTimeout           string `json:"readTimeout" yaml:"readTimeout"`
	WriteTimeout          string `json:"writeTimeout" yaml:"writeTimeout"`
	IdleTimeout           string `json:"idleTimeout" yaml:"idleTimeout"`
	K8sRequestTimeout     string `json:"k8sRequestTimeout" yaml:"k8sRequestTimeout"`
}

type loggingYAML struct {
	Level string `json:"level" yaml:"level"`
}

type gatewayMiddlewareYAML struct {
	TrustedProxyCIDRs []string `json:"trustedProxyCIDRs" yaml:"trustedProxyCIDRs"`
}

type gatewayProcessingYAML struct {
	Deduplication gatewayDeduplicationYAML `json:"deduplication" yaml:"deduplication"`
	Retry         gatewayRetryYAML         `json:"retry" yaml:"retry"`
}

type gatewayDeduplicationYAML struct {
	CooldownPeriod string `json:"cooldownPeriod" yaml:"cooldownPeriod"`
}

type gatewayRetryYAML struct {
	MaxAttempts    int    `json:"maxAttempts" yaml:"maxAttempts"`
	InitialBackoff string `json:"initialBackoff" yaml:"initialBackoff"`
	MaxBackoff     string `json:"maxBackoff" yaml:"maxBackoff"`
}

type gatewayDatastorageYAML struct {
	URL     string                `json:"url" yaml:"url"`
	Timeout string                `json:"timeout" yaml:"timeout"`
	Buffer  dataStorageBufferYAML `json:"buffer" yaml:"buffer"`
}

type gatewayConfigYAML struct {
	TLSProfile  string                 `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Logging     loggingYAML            `json:"logging" yaml:"logging"`
	Processing  gatewayProcessingYAML  `json:"processing" yaml:"processing"`
	Server      gatewayServerYAML      `json:"server" yaml:"server"`
	Middleware  gatewayMiddlewareYAML  `json:"middleware" yaml:"middleware"`
	Datastorage gatewayDatastorageYAML `json:"datastorage" yaml:"datastorage"`
}

type dataStorageServerYAML struct {
	Port         int           `json:"port" yaml:"port"`
	Host         string        `json:"host" yaml:"host"`
	HealthPort   int           `json:"healthPort" yaml:"healthPort"`
	MetricsPort  int           `json:"metricsPort" yaml:"metricsPort"`
	ReadTimeout  string        `json:"readTimeout" yaml:"readTimeout"`
	WriteTimeout string        `json:"writeTimeout" yaml:"writeTimeout"`
	TLS          tlsConfigYAML `json:"tls" yaml:"tls"`
}

type dataStorageDatabaseYAML struct {
	Host            string `json:"host" yaml:"host"`
	Port            int32  `json:"port" yaml:"port"`
	Name            string `json:"name" yaml:"name"`
	User            string `json:"user" yaml:"user"`
	SSLMode         string `json:"sslMode" yaml:"sslMode"`
	MaxOpenConns    int    `json:"maxOpenConns" yaml:"maxOpenConns"`
	MaxIdleConns    int    `json:"maxIdleConns" yaml:"maxIdleConns"`
	ConnMaxLifetime string `json:"connMaxLifetime" yaml:"connMaxLifetime"`
	ConnMaxIdleTime string `json:"connMaxIdleTime" yaml:"connMaxIdleTime"`
	SecretsFile     string `json:"secretsFile" yaml:"secretsFile"`
	UsernameKey     string `json:"usernameKey" yaml:"usernameKey"`
	PasswordKey     string `json:"passwordKey" yaml:"passwordKey"`
}

type dataStorageRedisYAML struct {
	Addr             string `json:"addr" yaml:"addr"`
	DB               int    `json:"db" yaml:"db"`
	DLQStreamName    string `json:"dlqStreamName" yaml:"dlqStreamName"`
	DLQMaxLen        int    `json:"dlqMaxLen" yaml:"dlqMaxLen"`
	DLQConsumerGroup string `json:"dlqConsumerGroup" yaml:"dlqConsumerGroup"`
	SecretsFile      string `json:"secretsFile" yaml:"secretsFile"`
	PasswordKey      string `json:"passwordKey" yaml:"passwordKey"`
}

type dataStorageLoggingYAML struct {
	Level  string `json:"level" yaml:"level"`
	Format string `json:"format" yaml:"format"`
}

type dataStorageConfigYAML struct {
	TLSProfile string                  `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Server     dataStorageServerYAML   `json:"server" yaml:"server"`
	Database   dataStorageDatabaseYAML `json:"database" yaml:"database"`
	Redis      dataStorageRedisYAML    `json:"redis" yaml:"redis"`
	Logging    dataStorageLoggingYAML  `json:"logging" yaml:"logging"`
}

type dataStorageBufferYAML struct {
	BufferSize    int    `json:"bufferSize" yaml:"bufferSize"`
	BatchSize     int    `json:"batchSize" yaml:"batchSize"`
	FlushInterval string `json:"flushInterval" yaml:"flushInterval"`
	MaxRetries    int    `json:"maxRetries" yaml:"maxRetries"`
}

type aiAnalysisKubernautAgentYAML struct {
	URL                 string `json:"url" yaml:"url"`
	Timeout             string `json:"timeout" yaml:"timeout"`
	SessionPollInterval string `json:"sessionPollInterval" yaml:"sessionPollInterval"`
}

type aiAnalysisDatastorageYAML struct {
	URL     string                `json:"url" yaml:"url"`
	Timeout string                `json:"timeout" yaml:"timeout"`
	Buffer  dataStorageBufferYAML `json:"buffer" yaml:"buffer"`
}

type aiAnalysisRegoYAML struct {
	PolicyPath          string `json:"policyPath" yaml:"policyPath"`
	ConfidenceThreshold string `json:"confidenceThreshold,omitempty" yaml:"confidenceThreshold,omitempty"`
}

// aiAnalysisConfigYAML embeds controllerConfig under "controller" via controllerBlock.
type aiAnalysisConfigYAML struct {
	TLSProfile  string                       `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Logging     loggingYAML                  `json:"logging" yaml:"logging"`
	Controller  controllerBlock              `json:"controller" yaml:"controller"`
	Agent       aiAnalysisKubernautAgentYAML `json:"agent" yaml:"agent"`
	Datastorage aiAnalysisDatastorageYAML    `json:"datastorage" yaml:"datastorage"`
	Rego        aiAnalysisRegoYAML           `json:"rego" yaml:"rego"`
}

type signalProcessingEnrichmentYAML struct {
	CacheTTL string `json:"cacheTtl" yaml:"cacheTtl"`
	Timeout  string `json:"timeout" yaml:"timeout"`
}

type signalProcessingClassifierYAML struct {
	RegoConfigMapName string `json:"regoConfigMapName" yaml:"regoConfigMapName"`
	RegoConfigMapKey  string `json:"regoConfigMapKey" yaml:"regoConfigMapKey"`
	HotReloadInterval string `json:"hotReloadInterval" yaml:"hotReloadInterval"`
}

type signalProcessingBufferYAML struct {
	BufferSize    int    `json:"bufferSize" yaml:"bufferSize"`
	BatchSize     int    `json:"batchSize" yaml:"batchSize"`
	FlushInterval string `json:"flushInterval" yaml:"flushInterval"`
	MaxRetries    int    `json:"maxRetries" yaml:"maxRetries"`
}

type signalProcessingDatastorageYAML struct {
	URL     string                     `json:"url" yaml:"url"`
	Timeout string                     `json:"timeout" yaml:"timeout"`
	Buffer  signalProcessingBufferYAML `json:"buffer" yaml:"buffer"`
}

// signalProcessingConfigYAML embeds controllerConfig under "controller" via controllerBlock.
type signalProcessingConfigYAML struct {
	TLSProfile  string                          `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Logging     loggingYAML                     `json:"logging" yaml:"logging"`
	Controller  controllerBlock                 `json:"controller" yaml:"controller"`
	Enrichment  signalProcessingEnrichmentYAML  `json:"enrichment" yaml:"enrichment"`
	Classifier  signalProcessingClassifierYAML  `json:"classifier" yaml:"classifier"`
	Datastorage signalProcessingDatastorageYAML `json:"datastorage" yaml:"datastorage"`
}

type roTimeoutsYAML struct {
	Global           string `json:"global" yaml:"global"`
	Processing       string `json:"processing" yaml:"processing"`
	Analyzing        string `json:"analyzing" yaml:"analyzing"`
	Executing        string `json:"executing" yaml:"executing"`
	AwaitingApproval string `json:"awaitingApproval" yaml:"awaitingApproval"`
	Verifying        string `json:"verifying" yaml:"verifying"`
}

type roRoutingYAML struct {
	ConsecutiveFailureThreshold   int    `json:"consecutiveFailureThreshold" yaml:"consecutiveFailureThreshold"`
	ConsecutiveFailureCooldown    string `json:"consecutiveFailureCooldown" yaml:"consecutiveFailureCooldown"`
	RecentlyRemediatedCooldown    string `json:"recentlyRemediatedCooldown" yaml:"recentlyRemediatedCooldown"`
	ExponentialBackoffBase        string `json:"exponentialBackoffBase" yaml:"exponentialBackoffBase"`
	ExponentialBackoffMax         string `json:"exponentialBackoffMax" yaml:"exponentialBackoffMax"`
	ExponentialBackoffMaxExponent int    `json:"exponentialBackoffMaxExponent" yaml:"exponentialBackoffMaxExponent"`
	ScopeBackoffBase              string `json:"scopeBackoffBase" yaml:"scopeBackoffBase"`
	ScopeBackoffMax               string `json:"scopeBackoffMax" yaml:"scopeBackoffMax"`
	NoActionRequiredDelayHours    int    `json:"noActionRequiredDelayHours" yaml:"noActionRequiredDelayHours"`
	IneffectiveChainThreshold     int    `json:"ineffectiveChainThreshold" yaml:"ineffectiveChainThreshold"`
	RecurrenceCountThreshold      int    `json:"recurrenceCountThreshold" yaml:"recurrenceCountThreshold"`
	IneffectiveTimeWindow         string `json:"ineffectiveTimeWindow" yaml:"ineffectiveTimeWindow"`
}

type roNotificationsYAML struct {
	NotifySelfResolved bool `json:"notifySelfResolved" yaml:"notifySelfResolved"`
}

type roRetentionYAML struct {
	Period string `json:"period" yaml:"period"`
}

type roEffectivenessYAML struct {
	StabilizationWindow string `json:"stabilizationWindow" yaml:"stabilizationWindow"`
}

type roAsyncPropagationYAML struct {
	GitOpsSyncDelay        string `json:"gitOpsSyncDelay" yaml:"gitOpsSyncDelay"`
	OperatorReconcileDelay string `json:"operatorReconcileDelay" yaml:"operatorReconcileDelay"`
	ProactiveAlertDelay    string `json:"proactiveAlertDelay" yaml:"proactiveAlertDelay"`
}

type roDatastorageYAML struct {
	URL     string                `json:"url" yaml:"url"`
	Timeout string                `json:"timeout" yaml:"timeout"`
	Buffer  dataStorageBufferYAML `json:"buffer" yaml:"buffer"`
}

type remediationOrchestratorConfigYAML struct {
	TLSProfile              string                 `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Controller              controllerBlock        `json:"controller" yaml:"controller"`
	Logging                 loggingYAML            `json:"logging" yaml:"logging"`
	Timeouts                roTimeoutsYAML         `json:"timeouts" yaml:"timeouts"`
	Datastorage             roDatastorageYAML      `json:"datastorage" yaml:"datastorage"`
	Routing                 roRoutingYAML          `json:"routing" yaml:"routing"`
	EffectivenessAssessment roEffectivenessYAML    `json:"effectivenessAssessment" yaml:"effectivenessAssessment"`
	AsyncPropagation        roAsyncPropagationYAML `json:"asyncPropagation" yaml:"asyncPropagation"`
	Notifications           roNotificationsYAML    `json:"notifications" yaml:"notifications"`
	Retention               roRetentionYAML        `json:"retention" yaml:"retention"`
	DryRun                  bool                   `json:"dryRun" yaml:"dryRun"`
	DryRunHoldPeriod        string                 `json:"dryRunHoldPeriod" yaml:"dryRunHoldPeriod"`
}

type weExecutionYAML struct {
	Namespace      string `json:"namespace" yaml:"namespace"`
	CooldownPeriod string `json:"cooldownPeriod" yaml:"cooldownPeriod"`
}

type weControllerYAML struct {
	MetricsAddr      string `json:"metricsAddr" yaml:"metricsAddr"`
	HealthProbeAddr  string `json:"healthProbeAddr" yaml:"healthProbeAddr"`
	LeaderElection   bool   `json:"leaderElection" yaml:"leaderElection"`
	LeaderElectionID string `json:"leaderElectionId" yaml:"leaderElectionId"`
}

type weDatastorageYAML struct {
	URL     string                `json:"url" yaml:"url"`
	Timeout string                `json:"timeout" yaml:"timeout"`
	Buffer  dataStorageBufferYAML `json:"buffer" yaml:"buffer"`
}

type weTokenSecretRefYAML struct {
	Name      string `json:"name" yaml:"name"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Key       string `json:"key" yaml:"key"`
}

type workflowExecutionAnsibleYAML struct {
	APIURL         string                `json:"apiURL" yaml:"apiURL"`
	TokenSecretRef *weTokenSecretRefYAML `json:"tokenSecretRef,omitempty" yaml:"tokenSecretRef,omitempty"`
	OrganizationID int                   `json:"organizationID,omitempty" yaml:"organizationID,omitempty"`
}

type weTektonYAML struct {
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

type workflowExecutionConfigYAML struct {
	TLSProfile  string                        `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Logging     loggingYAML                   `json:"logging" yaml:"logging"`
	Execution   weExecutionYAML               `json:"execution" yaml:"execution"`
	Ansible     *workflowExecutionAnsibleYAML `json:"ansible,omitempty" yaml:"ansible,omitempty"`
	Tekton      *weTektonYAML                 `json:"tekton,omitempty" yaml:"tekton,omitempty"`
	Datastorage weDatastorageYAML             `json:"datastorage" yaml:"datastorage"`
	Controller  weControllerYAML              `json:"controller" yaml:"controller"`
}

type emAssessmentYAML struct {
	StabilizationWindow string `json:"stabilizationWindow" yaml:"stabilizationWindow"`
	ValidityWindow      string `json:"validityWindow" yaml:"validityWindow"`
}

type emDatastorageYAML struct {
	URL     string                `json:"url" yaml:"url"`
	Timeout string                `json:"timeout" yaml:"timeout"`
	Buffer  dataStorageBufferYAML `json:"buffer" yaml:"buffer"`
}

type emExternalYAML struct {
	PrometheusURL       string `json:"prometheusUrl" yaml:"prometheusUrl"`
	PrometheusEnabled   bool   `json:"prometheusEnabled" yaml:"prometheusEnabled"`
	AlertManagerURL     string `json:"alertManagerUrl" yaml:"alertManagerUrl"`
	AlertManagerEnabled bool   `json:"alertManagerEnabled" yaml:"alertManagerEnabled"`
	ConnectionTimeout   string `json:"connectionTimeout,omitempty" yaml:"connectionTimeout,omitempty"`
	TLSCaFile           string `json:"tlsCaFile,omitempty" yaml:"tlsCaFile,omitempty"`
}

type effectivenessMonitorConfigYAML struct {
	TLSProfile  string            `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Logging     loggingYAML       `json:"logging" yaml:"logging"`
	Assessment  emAssessmentYAML  `json:"assessment" yaml:"assessment"`
	Controller  controllerBlock   `json:"controller" yaml:"controller"`
	Datastorage emDatastorageYAML `json:"datastorage" yaml:"datastorage"`
	External    *emExternalYAML   `json:"external,omitempty" yaml:"external,omitempty"`
}

type notificationConsoleYAML struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

type notificationFileYAML struct {
	OutputDir string `json:"outputDir" yaml:"outputDir"`
	Format    string `json:"format" yaml:"format"`
	Timeout   string `json:"timeout" yaml:"timeout"`
}

type notificationLogYAML struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Format  string `json:"format" yaml:"format"`
}

type notificationSlackYAML struct {
	Timeout string `json:"timeout" yaml:"timeout"`
}

type notificationDeliveryYAML struct {
	Console     notificationConsoleYAML     `json:"console" yaml:"console"`
	File        notificationFileYAML        `json:"file" yaml:"file"`
	Log         notificationLogYAML         `json:"log" yaml:"log"`
	Slack       notificationSlackYAML       `json:"slack" yaml:"slack"`
	Credentials notificationCredentialsYAML `json:"credentials" yaml:"credentials"`
}

type notificationCredentialsYAML struct {
	Dir string `json:"dir" yaml:"dir"`
}

type notificationBufferYAML struct {
	BufferSize    int    `json:"bufferSize" yaml:"bufferSize"`
	BatchSize     int    `json:"batchSize" yaml:"batchSize"`
	FlushInterval string `json:"flushInterval" yaml:"flushInterval"`
	MaxRetries    int    `json:"maxRetries" yaml:"maxRetries"`
}

type notificationDatastorageYAML struct {
	URL     string                 `json:"url" yaml:"url"`
	Timeout string                 `json:"timeout" yaml:"timeout"`
	Buffer  notificationBufferYAML `json:"buffer" yaml:"buffer"`
}

// notificationControllerConfigYAML embeds controllerConfig under "controller" via controllerBlock.
type notificationControllerConfigYAML struct {
	TLSProfile  string                      `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Logging     loggingYAML                 `json:"logging" yaml:"logging"`
	Controller  controllerBlock             `json:"controller" yaml:"controller"`
	Delivery    notificationDeliveryYAML    `json:"delivery" yaml:"delivery"`
	Datastorage notificationDatastorageYAML `json:"datastorage" yaml:"datastorage"`
}

type notificationRoutingSlackRoute struct {
	Receiver string            `json:"receiver" yaml:"receiver"`
	Match    map[string]string `json:"match" yaml:"match"`
}

type notificationRoutingSlackReceiver struct {
	Name  string `json:"name" yaml:"name"`
	Slack struct {
		Channel               string `json:"channel" yaml:"channel"`
		CredentialsSecretName string `json:"credentialsSecretName" yaml:"credentialsSecretName"`
	} `json:"slack" yaml:"slack"`
}

type notificationRoutingSlackYAML struct {
	Routes    []notificationRoutingSlackRoute    `json:"routes" yaml:"routes"`
	Receivers []notificationRoutingSlackReceiver `json:"receivers" yaml:"receivers"`
}

type notificationRoutingConsoleReceiver struct {
	Name    string   `json:"name" yaml:"name"`
	Console struct{} `json:"console" yaml:"console"`
}

type notificationRoutingConsoleYAML struct {
	Routes    []struct{}                           `json:"routes" yaml:"routes"`
	Receivers []notificationRoutingConsoleReceiver `json:"receivers" yaml:"receivers"`
}

type kubernautAgentServerTLSYAML struct {
	CertDir string `json:"certDir" yaml:"certDir"`
}

type kaRuntimeYAML struct {
	Logging    loggingYAML         `json:"logging" yaml:"logging"`
	Server     kaRuntimeServerYAML `json:"server" yaml:"server"`
	Audit      *kaAuditYAML        `json:"audit,omitempty" yaml:"audit,omitempty"`
	Session    *kaSessionYAML      `json:"session,omitempty" yaml:"session,omitempty"`
}

type kaRuntimeServerYAML struct {
	Address     string                      `json:"address" yaml:"address"`
	Port        int                         `json:"port" yaml:"port"`
	HealthAddr  string                      `json:"healthAddr" yaml:"healthAddr"`
	MetricsAddr string                      `json:"metricsAddr" yaml:"metricsAddr"`
	TLS         kubernautAgentServerTLSYAML `json:"tls" yaml:"tls"`
	TLSProfile  string                      `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
}

type kaAuditYAML struct {
	FlushIntervalSeconds float64 `json:"flushIntervalSeconds" yaml:"flushIntervalSeconds"`
	BufferSize           int     `json:"bufferSize" yaml:"bufferSize"`
	BatchSize            int     `json:"batchSize" yaml:"batchSize"`
}

type kaSessionYAML struct {
	TTL string `json:"ttl" yaml:"ttl"`
}

type kaAIYAML struct {
	LLM            kaLLMYAML           `json:"llm" yaml:"llm"`
	Investigation  kaInvestigationYAML `json:"investigation" yaml:"investigation"`
	AlignmentCheck *kaAlignmentYAML    `json:"alignmentCheck,omitempty" yaml:"alignmentCheck,omitempty"`
	Summarizer     *kaSummarizerYAML   `json:"summarizer,omitempty" yaml:"summarizer,omitempty"`
	Safety         *kaSafetyYAML       `json:"safety,omitempty" yaml:"safety,omitempty"`
}

type kaLLMYAML struct {
	Provider        string        `json:"provider" yaml:"provider"`
	VertexProject   string        `json:"vertexProject,omitempty" yaml:"vertexProject,omitempty"`
	VertexLocation  string        `json:"vertexLocation,omitempty" yaml:"vertexLocation,omitempty"`
	BedrockRegion   string        `json:"bedrockRegion,omitempty" yaml:"bedrockRegion,omitempty"`
	AzureApiVersion string        `json:"azureApiVersion,omitempty" yaml:"azureApiVersion,omitempty"`
	TLSCaFile       string        `json:"tlsCaFile,omitempty" yaml:"tlsCaFile,omitempty"`
	OAuth2          *kaOAuth2YAML `json:"oauth2,omitempty" yaml:"oauth2,omitempty"`
}

type kaOAuth2YAML struct {
	Enabled        bool     `json:"enabled" yaml:"enabled"`
	TokenURL       string   `json:"tokenURL" yaml:"tokenURL"`
	CredentialsDir string   `json:"credentialsDir" yaml:"credentialsDir"`
	Scopes         []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

type kaInvestigationYAML struct {
	MaxTurns int `json:"maxTurns" yaml:"maxTurns"`
}

type kaAlignmentYAML struct {
	Enabled       bool            `json:"enabled" yaml:"enabled"`
	Timeout       string          `json:"timeout" yaml:"timeout"`
	MaxStepTokens int             `json:"maxStepTokens" yaml:"maxStepTokens"`
	LLM           *kaAlignLLMYAML `json:"llm,omitempty" yaml:"llm,omitempty"`
}

type kaAlignLLMYAML struct {
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model    string `json:"model,omitempty" yaml:"model,omitempty"`
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	APIKey   string `json:"apiKey,omitempty" yaml:"apiKey,omitempty"`
}

type kaSummarizerYAML struct {
	Threshold         int `json:"threshold" yaml:"threshold"`
	MaxToolOutputSize int `json:"maxToolOutputSize" yaml:"maxToolOutputSize"`
}

type kaSafetyYAML struct {
	Sanitization kaSanitizationYAML `json:"sanitization" yaml:"sanitization"`
	Anomaly      kaAnomalyYAML      `json:"anomaly" yaml:"anomaly"`
}

type kaSanitizationYAML struct {
	InjectionPatternsEnabled bool `json:"injectionPatternsEnabled" yaml:"injectionPatternsEnabled"`
	CredentialScrubEnabled   bool `json:"credentialScrubEnabled" yaml:"credentialScrubEnabled"`
}

type kaAnomalyYAML struct {
	MaxToolCallsPerTool int `json:"maxToolCallsPerTool" yaml:"maxToolCallsPerTool"`
	MaxTotalToolCalls   int `json:"maxTotalToolCalls" yaml:"maxTotalToolCalls"`
	MaxRepeatedFailures int `json:"maxRepeatedFailures" yaml:"maxRepeatedFailures"`
}

type kaIntegrationsYAML struct {
	DataStorage kaIntegrationsDataStorageYAML `json:"dataStorage" yaml:"dataStorage"`
	Tools       *kaIntegrationsToolsYAML      `json:"tools,omitempty" yaml:"tools,omitempty"`
}

type kaIntegrationsDataStorageYAML struct {
	URL string `json:"url" yaml:"url"`
}

type kaIntegrationsToolsYAML struct {
	Prometheus kaIntegrationsPrometheusYAML `json:"prometheus" yaml:"prometheus"`
}

type kaIntegrationsPrometheusYAML struct {
	URL string `json:"url" yaml:"url"`
}

type kubernautAgentConfigYAML struct {
	Runtime      kaRuntimeYAML      `json:"runtime" yaml:"runtime"`
	AI           kaAIYAML           `json:"ai" yaml:"ai"`
	Integrations kaIntegrationsYAML `json:"integrations" yaml:"integrations"`
}

type llmRuntimeYAML struct {
	Model          string  `json:"model" yaml:"model"`
	Endpoint       string  `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Temperature    float64 `json:"temperature" yaml:"temperature"` //nolint:musttag
	MaxRetries     int     `json:"maxRetries" yaml:"maxRetries"`
	TimeoutSeconds int     `json:"timeoutSeconds" yaml:"timeoutSeconds"`
}

type authWebhookWebhookYAML struct {
	Port            int    `json:"port" yaml:"port"`
	CertDir         string `json:"certDir" yaml:"certDir"`
	HealthProbeAddr string `json:"healthProbeAddr" yaml:"healthProbeAddr"`
}

type authWebhookBufferYAML struct {
	BufferSize    int    `json:"bufferSize" yaml:"bufferSize"`
	BatchSize     int    `json:"batchSize" yaml:"batchSize"`
	FlushInterval string `json:"flushInterval" yaml:"flushInterval"`
	MaxRetries    int    `json:"maxRetries" yaml:"maxRetries"`
}

type authWebhookDatastorageYAML struct {
	URL     string                `json:"url" yaml:"url"`
	Timeout string                `json:"timeout" yaml:"timeout"`
	Buffer  authWebhookBufferYAML `json:"buffer" yaml:"buffer"`
}

type authWebhookConfigYAML struct {
	TLSProfile  string                     `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Logging     loggingYAML                `json:"logging" yaml:"logging"`
	Webhook     authWebhookWebhookYAML     `json:"webhook" yaml:"webhook"`
	Datastorage authWebhookDatastorageYAML `json:"datastorage" yaml:"datastorage"`
}

// configMapOpts holds resolved functional options for ConfigMap builders.
type configMapOpts struct {
	tlsProfile string
}

// ConfigMapOption is a functional option for ConfigMap builders.
type ConfigMapOption func(*configMapOpts)

// WithTLSProfile injects the cluster TLS security profile name into the
// service's ConfigMap YAML. Omit this option on non-OCP clusters or when
// the profile is unset (services fall back to Go's TLS 1.2 defaults).
func WithTLSProfile(profile string) ConfigMapOption {
	return func(o *configMapOpts) { o.tlsProfile = profile }
}

func resolveOpts(opts []ConfigMapOption) configMapOpts {
	var o configMapOpts
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// marshalYAML uses sigs.k8s.io/yaml.Marshal, which serializes via encoding/json
// (struct fields must carry json tags for correct YAML key names).
func marshalYAML(v any) (string, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("resources: yaml marshal: %w", err)
	}
	return string(b), nil
}

// GatewayConfigMap builds the gateway-config ConfigMap.
func GatewayConfigMap(kn *kubernautv1alpha1.Kubernaut, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	ns := kn.Namespace
	gwCfg := &kn.Spec.Gateway.Config

	proxyCIDRs := gwCfg.TrustedProxyCIDRs
	if proxyCIDRs == nil {
		proxyCIDRs = []string{}
	}

	cfg := gatewayConfigYAML{
		TLSProfile: o.tlsProfile,
		Logging: loggingYAML{
			Level: withDefault(kn.Spec.Gateway.Logging.Level, "info"),
		},
		Processing: gatewayProcessingYAML{
			Deduplication: gatewayDeduplicationYAML{
				CooldownPeriod: withDefault(gwCfg.DeduplicationCooldown, "5m"),
			},
			Retry: gatewayRetryYAML{MaxAttempts: 3, InitialBackoff: "100ms", MaxBackoff: "5s"},
		},
		Server: gatewayServerYAML{
			ListenAddr:            ":8080",
			HealthAddr:            ":8081",
			MetricsAddr:           ":9090",
			MaxConcurrentRequests: 100,
			ReadTimeout:           "30s",
			WriteTimeout:          "30s",
			IdleTimeout:           "120s",
			K8sRequestTimeout:     withDefault(gwCfg.K8sRequestTimeout, "15s"),
		},
		Middleware: gatewayMiddlewareYAML{
			TrustedProxyCIDRs: proxyCIDRs,
		},
		Datastorage: gatewayDatastorageYAML{
			URL:     DataStorageURL(ns),
			Timeout: "10s",
			Buffer:  dataStorageBufferYAML{BufferSize: 10000, BatchSize: 100, FlushInterval: "1s", MaxRetries: 3},
		},
	}
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("gateway config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "gateway-config", ComponentGateway),
		Data:       map[string]string{"config.yaml": data},
	}, nil
}

// DataStorageConfigMap builds the data-storage config ConfigMap. The dbName and
// dbUser parameters must match the values written into the DataStorageDBSecret
// to avoid a config/secret mismatch.
func DataStorageConfigMap(kn *kubernautv1alpha1.Kubernaut, dbName, dbUser string, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	pgPort := PostgreSQLPort(kn)
	cfg := dataStorageConfigYAML{
		TLSProfile: o.tlsProfile,
		Server: dataStorageServerYAML{
			Port:         8080,
			Host:         "0.0.0.0",
			HealthPort:   8081,
			MetricsPort:  9090,
			ReadTimeout:  "30s",
			WriteTimeout: "30s",
			TLS:          tlsConfigYAML{CertDir: InterServiceTLSCertDir},
		},
		Database: dataStorageDatabaseYAML{
			Host:            kn.Spec.PostgreSQL.Host,
			Port:            pgPort,
			Name:            dbName,
			User:            dbUser,
			SSLMode:         "disable",
			MaxOpenConns:    100,
			MaxIdleConns:    20,
			ConnMaxLifetime: "1h",
			ConnMaxIdleTime: "10m",
			SecretsFile:     "/etc/datastorage/secrets/db-secrets.yaml",
			UsernameKey:     "username",
			PasswordKey:     "password",
		},
		Redis: dataStorageRedisYAML{
			Addr:             ValkeyAddr(&kn.Spec.Valkey),
			DB:               0,
			DLQStreamName:    "dlq-stream",
			DLQMaxLen:        1000,
			DLQConsumerGroup: "dlq-group",
			SecretsFile:      "/etc/datastorage/secrets/valkey-secrets.yaml",
			PasswordKey:      "password",
		},
		Logging: dataStorageLoggingYAML{
			Level:  withDefault(kn.Spec.DataStorage.Logging.Level, "info"),
			Format: "json",
		},
	}
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("datastorage config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "datastorage-config", ComponentDataStorage),
		Data:       map[string]string{"config.yaml": data},
	}, nil
}

// AIAnalysisConfigMap builds the aianalysis-config ConfigMap.
func AIAnalysisConfigMap(kn *kubernautv1alpha1.Kubernaut, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	ns := kn.Namespace
	rego := aiAnalysisRegoYAML{
		PolicyPath: "/etc/aianalysis/policies/approval.rego",
	}
	if kn.Spec.AIAnalysis.ConfidenceThreshold != "" {
		rego.ConfidenceThreshold = kn.Spec.AIAnalysis.ConfidenceThreshold
	}
	cfg := aiAnalysisConfigYAML{
		TLSProfile: o.tlsProfile,
		Logging:    loggingYAML{Level: withDefault(kn.Spec.AIAnalysis.Logging.Level, "info")},
		Controller: newControllerBlock("aianalysis.kubernaut.ai"),
		Agent: aiAnalysisKubernautAgentYAML{
			URL:                 fmt.Sprintf("https://kubernaut-agent.%s.svc.cluster.local:8080", ns),
			Timeout:             "180s",
			SessionPollInterval: "15s",
		},
		Datastorage: aiAnalysisDatastorageYAML{
			URL:     DataStorageURL(ns),
			Timeout: "10s",
			Buffer: dataStorageBufferYAML{
				BufferSize:    20000,
				BatchSize:     1000,
				FlushInterval: "1s",
				MaxRetries:    3,
			},
		},
		Rego: rego,
	}
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("aianalysis config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "aianalysis-config", ComponentAIAnalysis),
		Data:       map[string]string{"config.yaml": data},
	}, nil
}

// AIAnalysisPoliciesConfigMap builds the default aianalysis-policies ConfigMap
// containing the approval Rego policy.
func AIAnalysisPoliciesConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	if kn.Spec.AIAnalysis.Policy.ConfigMapName != "" {
		return nil
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, AIAnalysisPolicyName(kn), ComponentAIAnalysis),
		Data: map[string]string{
			"approval.rego": "package kubernaut.aianalysis\ndefault allow = true\n",
		},
	}
}

// SignalProcessingConfigMap builds the signalprocessing-config ConfigMap.
func SignalProcessingConfigMap(kn *kubernautv1alpha1.Kubernaut, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	ns := kn.Namespace
	buf := signalProcessingBufferYAML{
		BufferSize:    10000,
		BatchSize:     100,
		FlushInterval: "1s",
		MaxRetries:    3,
	}
	cfg := signalProcessingConfigYAML{
		TLSProfile: o.tlsProfile,
		Logging:    loggingYAML{Level: withDefault(kn.Spec.SignalProcessing.Logging.Level, "info")},
		Controller: newControllerBlock("signalprocessing.kubernaut.ai"),
		Enrichment: signalProcessingEnrichmentYAML{
			CacheTTL: "5m",
			Timeout:  "10s",
		},
		Classifier: signalProcessingClassifierYAML{
			RegoConfigMapName: SignalProcessingPolicyName(kn),
			RegoConfigMapKey:  "policy.rego",
			HotReloadInterval: "30s",
		},
		Datastorage: signalProcessingDatastorageYAML{
			URL:     DataStorageURL(ns),
			Timeout: "10s",
			Buffer:  buf,
		},
	}
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("signalprocessing config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "signalprocessing-config", ComponentSignalProcessing),
		Data:       map[string]string{"config.yaml": data},
	}, nil
}

// SignalProcessingPolicyConfigMap builds the default signalprocessing-policy ConfigMap
// containing the classification Rego policy.
func SignalProcessingPolicyConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	if kn.Spec.SignalProcessing.Policy.ConfigMapName != "" {
		return nil
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, SignalProcessingPolicyName(kn), ComponentSignalProcessing),
		Data: map[string]string{
			"policy.rego": "package kubernaut.signalprocessing\ndefault allow = true\n",
		},
	}
}

// ProactiveSignalMappingsConfigMap builds the default signalprocessing-proactive-signal-mappings
// ConfigMap with BR-SP-106 proactive signal mode mappings. Returns nil when the user
// provides their own ConfigMap via spec.signalProcessing.proactiveSignalMappings.
func ProactiveSignalMappingsConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	if kn.Spec.SignalProcessing.ProactiveSignalMappings != nil {
		return nil
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "signalprocessing-proactive-signal-mappings", ComponentSignalProcessing),
		Data: map[string]string{
			"proactive-signal-mappings.yaml": `proactive_signal_mappings:
  PredictedOOMKill: OOMKilled
  PredictedCPUThrottling: CPUThrottling
  PredictedDiskPressure: DiskPressure
  PredictedNodeNotReady: NodeNotReady
`,
		},
	}
}

// RemediationOrchestratorConfigMap builds the remediationorchestrator-config ConfigMap.
func RemediationOrchestratorConfigMap(kn *kubernautv1alpha1.Kubernaut, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	ro := &kn.Spec.RemediationOrchestrator
	ns := kn.Namespace
	cfg := remediationOrchestratorConfigYAML{
		TLSProfile: o.tlsProfile,
		Controller: newControllerBlock("remediationorchestrator.kubernaut.ai"),
		Logging:    loggingYAML{Level: withDefault(ro.Logging.Level, "info")},
		Datastorage: roDatastorageYAML{
			URL:     DataStorageURL(ns),
			Timeout: "10s",
			Buffer: dataStorageBufferYAML{
				BufferSize:    30000,
				BatchSize:     1000,
				FlushInterval: "1s",
				MaxRetries:    3,
			},
		},
		Timeouts: roTimeoutsYAML{
			Global:           withDefault(ro.Timeouts.Global, "1h"),
			Processing:       withDefault(ro.Timeouts.Processing, "5m"),
			Analyzing:        withDefault(ro.Timeouts.Analyzing, "10m"),
			Executing:        withDefault(ro.Timeouts.Executing, "30m"),
			AwaitingApproval: withDefault(ro.Timeouts.AwaitingApproval, "15m"),
			Verifying:        withDefault(ro.Timeouts.Verifying, "30m"),
		},
		Routing: roRoutingYAML{
			ConsecutiveFailureThreshold:   intPtrDefault(ro.Routing.ConsecutiveFailureThreshold, 3),
			ConsecutiveFailureCooldown:    withDefault(ro.Routing.ConsecutiveFailureCooldown, "1h"),
			RecentlyRemediatedCooldown:    withDefault(ro.Routing.RecentlyRemediatedCooldown, "5m"),
			ExponentialBackoffBase:        withDefault(ro.Routing.ExponentialBackoffBase, "1m"),
			ExponentialBackoffMax:         withDefault(ro.Routing.ExponentialBackoffMax, "10m"),
			ExponentialBackoffMaxExponent: intPtrDefault(ro.Routing.ExponentialBackoffMaxExponent, 4),
			ScopeBackoffBase:              withDefault(ro.Routing.ScopeBackoffBase, "5s"),
			ScopeBackoffMax:               withDefault(ro.Routing.ScopeBackoffMax, "5m"),
			NoActionRequiredDelayHours:    intPtrDefault(ro.Routing.NoActionRequiredDelayHours, 24),
			IneffectiveChainThreshold:     intPtrDefault(ro.Routing.IneffectiveChainThreshold, 3),
			RecurrenceCountThreshold:      intPtrDefault(ro.Routing.RecurrenceCountThreshold, 5),
			IneffectiveTimeWindow:         withDefault(ro.Routing.IneffectiveTimeWindow, "4h"),
		},
		EffectivenessAssessment: roEffectivenessYAML{
			StabilizationWindow: withDefault(ro.EffectivenessAssessment.StabilizationWindow, "5m"),
		},
		AsyncPropagation: roAsyncPropagationYAML{
			GitOpsSyncDelay:        withDefault(ro.AsyncPropagation.GitOpsSyncDelay, "3m"),
			OperatorReconcileDelay: withDefault(ro.AsyncPropagation.OperatorReconcileDelay, "1m"),
			ProactiveAlertDelay:    withDefault(ro.AsyncPropagation.ProactiveAlertDelay, "5m"),
		},
		Notifications: roNotificationsYAML{
			NotifySelfResolved: ro.Notifications.NotifySelfResolved,
		},
		Retention: roRetentionYAML{
			Period: withDefault(ro.Retention.Period, "24h"),
		},
		DryRun:           ro.DryRun,
		DryRunHoldPeriod: withDefault(ro.DryRunHoldPeriod, "1h"),
	}
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("remediationorchestrator config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "remediationorchestrator-config", ComponentRemediationOrchestrator),
		Data:       map[string]string{"remediationorchestrator.yaml": data},
	}, nil
}

// WorkflowExecutionConfigMap builds the workflowexecution-config ConfigMap.
func WorkflowExecutionConfigMap(kn *kubernautv1alpha1.Kubernaut, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	we := &kn.Spec.WorkflowExecution
	wfNs := ResolveWorkflowNamespace(kn)
	cooldown := we.CooldownPeriod
	if cooldown == "" {
		cooldown = "1m"
	}
	cfg := workflowExecutionConfigYAML{
		TLSProfile: o.tlsProfile,
		Logging:    loggingYAML{Level: withDefault(we.Logging.Level, "info")},
		Execution: weExecutionYAML{
			Namespace:      wfNs,
			CooldownPeriod: cooldown,
		},
		Datastorage: weDatastorageYAML{
			URL:     DataStorageURL(kn.Namespace),
			Timeout: "10s",
			Buffer: dataStorageBufferYAML{
				BufferSize:    10000,
				BatchSize:     500,
				FlushInterval: "1s",
				MaxRetries:    3,
			},
		},
		Controller: weControllerYAML{
			MetricsAddr:      ":9090",
			HealthProbeAddr:  ":8081",
			LeaderElection:   false,
			LeaderElectionID: "workflowexecution.kubernaut.ai",
		},
	}
	if kn.Spec.Ansible.Enabled {
		ansible := workflowExecutionAnsibleYAML{
			APIURL:         kn.Spec.Ansible.APIURL,
			OrganizationID: kn.Spec.Ansible.OrganizationID,
		}
		if kn.Spec.Ansible.TokenSecretRef != nil {
			ansible.TokenSecretRef = &weTokenSecretRefYAML{
				Name:      kn.Spec.Ansible.TokenSecretRef.Name,
				Namespace: kn.Namespace,
				Key:       withDefault(kn.Spec.Ansible.TokenSecretRef.Key, "token"),
			}
		}
		cfg.Ansible = &ansible
	}
	if we.Tekton.Enabled != nil {
		cfg.Tekton = &weTektonYAML{Enabled: we.Tekton.Enabled}
	}
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("workflowexecution config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "workflowexecution-config", ComponentWorkflowExecution),
		Data:       map[string]string{"workflowexecution.yaml": data},
	}, nil
}

// EffectivenessMonitorConfigMap builds the effectivenessmonitor-config ConfigMap.
func EffectivenessMonitorConfigMap(kn *kubernautv1alpha1.Kubernaut, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	em := &kn.Spec.EffectivenessMonitor
	cfg := effectivenessMonitorConfigYAML{
		TLSProfile: o.tlsProfile,
		Logging:    loggingYAML{Level: withDefault(em.Logging.Level, "info")},
		Assessment: emAssessmentYAML{
			StabilizationWindow: withDefault(em.Assessment.StabilizationWindow, "30s"),
			ValidityWindow:      withDefault(em.Assessment.ValidityWindow, "300s"),
		},
		Controller: newControllerBlock("effectivenessmonitor.kubernaut.ai"),
		Datastorage: emDatastorageYAML{
			URL:     DataStorageURL(kn.Namespace),
			Timeout: "10s",
			Buffer: dataStorageBufferYAML{
				BufferSize:    100,
				BatchSize:     10,
				FlushInterval: "1s",
				MaxRetries:    3,
			},
		},
	}
	if kn.Spec.Monitoring.MonitoringEnabled() {
		cfg.External = &emExternalYAML{
			PrometheusURL:       OCPPrometheusURL,
			PrometheusEnabled:   true,
			AlertManagerURL:     OCPAlertManagerURL,
			AlertManagerEnabled: true,
			ConnectionTimeout:   "10s",
			TLSCaFile:           "/etc/ssl/em/service-ca.crt",
		}
	}
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("effectivenessmonitor config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "effectivenessmonitor-config", ComponentEffectivenessMonitor),
		Data:       map[string]string{"effectivenessmonitor.yaml": data},
	}, nil
}

// NotificationControllerConfigMap builds the notification-controller-config ConfigMap.
func NotificationControllerConfigMap(kn *kubernautv1alpha1.Kubernaut, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	buf := notificationBufferYAML{
		BufferSize:    10000,
		BatchSize:     100,
		FlushInterval: "1s",
		MaxRetries:    3,
	}
	ds := notificationDatastorageYAML{
		URL:     DataStorageURL(kn.Namespace),
		Timeout: "10s",
		Buffer:  buf,
	}
	cfg := notificationControllerConfigYAML{
		TLSProfile: o.tlsProfile,
		Logging:    loggingYAML{Level: withDefault(kn.Spec.Notification.Logging.Level, "info")},
		Controller: newControllerBlock("notification.kubernaut.ai"),
		Delivery: notificationDeliveryYAML{
			Console: notificationConsoleYAML{Enabled: true},
			File: notificationFileYAML{
				OutputDir: "/tmp/notifications",
				Format:    "json",
				Timeout:   "5s",
			},
			Log: notificationLogYAML{
				Enabled: true,
				Format:  "json",
			},
			Slack: notificationSlackYAML{Timeout: "10s"},
			Credentials: notificationCredentialsYAML{
				Dir: "/etc/notification/credentials",
			},
		},
		Datastorage: ds,
	}
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("notification-controller config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "notification-controller-config", ComponentNotification),
		Data:       map[string]string{"config.yaml": data},
	}, nil
}

// NotificationRoutingConfigMap builds the notification-routing-config ConfigMap.
// When Slack is configured, routes are generated; otherwise a console-only fallback is used.
func NotificationRoutingConfigMap(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
	slack := kn.Spec.Notification.Slack
	channel := slack.Channel
	if channel == "" {
		channel = "#kubernaut-alerts"
	}
	var routing string
	if slack.SecretName != "" {
		cfg := notificationRoutingSlackYAML{
			Routes: []notificationRoutingSlackRoute{
				{
					Receiver: "slack",
					Match:    map[string]string{"severity": ".*"},
				},
			},
			Receivers: []notificationRoutingSlackReceiver{
				{Name: "slack"},
			},
		}
		cfg.Receivers[0].Slack.Channel = channel
		cfg.Receivers[0].Slack.CredentialsSecretName = slack.SecretName
		var err error
		routing, err = marshalYAML(cfg)
		if err != nil {
			return nil, fmt.Errorf("notification-routing slack config: %w", err)
		}
	} else {
		var console notificationRoutingConsoleYAML
		console.Receivers = []notificationRoutingConsoleReceiver{
			{Name: "console"},
		}
		var err error
		routing, err = marshalYAML(console)
		if err != nil {
			return nil, fmt.Errorf("notification-routing console config: %w", err)
		}
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "notification-routing-config", ComponentNotification),
		Data:       map[string]string{"routing.yaml": routing},
	}, nil
}

// KubernautAgentConfigMap builds the kubernaut-agent-config ConfigMap.
func KubernautAgentConfigMap(kn *kubernautv1alpha1.Kubernaut, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	ns := kn.Namespace
	ka := &kn.Spec.KubernautAgent
	maxTurns := ka.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 40
	}

	cfg := kubernautAgentConfigYAML{
		Runtime: kaRuntimeYAML{
			Logging: loggingYAML{Level: withDefault(ka.Logging.Level, "info")},
			Server: kaRuntimeServerYAML{
				Address:     "0.0.0.0",
				Port:        8080,
				HealthAddr:  ":8081",
				MetricsAddr: ":9090",
				TLS:         kubernautAgentServerTLSYAML{CertDir: InterServiceTLSCertDir},
				TLSProfile:  o.tlsProfile,
			},
		},
		AI: kaAIYAML{
			LLM: kaLLMYAML{
				Provider: ka.LLM.Provider,
			},
			Investigation: kaInvestigationYAML{MaxTurns: maxTurns},
		},
		Integrations: kaIntegrationsYAML{
			DataStorage: kaIntegrationsDataStorageYAML{URL: DataStorageURL(ns)},
		},
	}

	if ka.Audit.AuditEnabled() {
		cfg.Runtime.Audit = &kaAuditYAML{
			FlushIntervalSeconds: 1.0,
			BufferSize:           10000,
			BatchSize:            50,
		}
	}

	if ttl := ka.Session.TTL; ttl != "" {
		cfg.Runtime.Session = &kaSessionYAML{TTL: ttl}
	}

	if ka.LLM.VertexProject != "" {
		cfg.AI.LLM.VertexProject = ka.LLM.VertexProject
	}
	if ka.LLM.VertexLocation != "" {
		cfg.AI.LLM.VertexLocation = ka.LLM.VertexLocation
	}
	if ka.LLM.BedrockRegion != "" {
		cfg.AI.LLM.BedrockRegion = ka.LLM.BedrockRegion
	}
	if ka.LLM.AzureAPIVersion != "" {
		cfg.AI.LLM.AzureApiVersion = ka.LLM.AzureAPIVersion
	}
	if ka.LLM.TLSCaFile != "" {
		cfg.AI.LLM.TLSCaFile = ka.LLM.TLSCaFile
	}

	if ka.LLM.OAuth2.Enabled {
		cfg.AI.LLM.OAuth2 = &kaOAuth2YAML{
			Enabled:        true,
			TokenURL:       ka.LLM.OAuth2.TokenURL,
			CredentialsDir: "/etc/kubernaut-agent/oauth2",
			Scopes:         ka.LLM.OAuth2.Scopes,
		}
	}

	if ka.AlignmentCheck.Enabled {
		ac := &kaAlignmentYAML{
			Enabled:       true,
			Timeout:       withDefault(ka.AlignmentCheck.Timeout, "10s"),
			MaxStepTokens: intDefault(ka.AlignmentCheck.MaxStepTokens, 500),
		}
		if ka.AlignmentCheck.LLM != nil {
			ac.LLM = &kaAlignLLMYAML{
				Provider: ka.AlignmentCheck.LLM.Provider,
				Model:    ka.AlignmentCheck.LLM.Model,
				Endpoint: ka.AlignmentCheck.LLM.Endpoint,
				APIKey:   ka.AlignmentCheck.LLM.APIKey,
			}
		}
		cfg.AI.AlignmentCheck = ac
	}

	threshold := intDefault(ka.Summarizer.Threshold, 8000)
	maxOutput := intDefault(ka.Summarizer.MaxToolOutputSize, 100000)
	if threshold != 8000 || maxOutput != 100000 {
		cfg.AI.Summarizer = &kaSummarizerYAML{
			Threshold:         threshold,
			MaxToolOutputSize: maxOutput,
		}
	}

	injEnabled := ka.Safety.Sanitization.InjectionPatternsEnabled == nil || *ka.Safety.Sanitization.InjectionPatternsEnabled
	credEnabled := ka.Safety.Sanitization.CredentialScrubEnabled == nil || *ka.Safety.Sanitization.CredentialScrubEnabled
	maxPerTool := intPtrDefault(ka.Safety.Anomaly.MaxToolCallsPerTool, 10)
	maxTotal := intPtrDefault(ka.Safety.Anomaly.MaxTotalToolCalls, 30)
	maxFail := intPtrDefault(ka.Safety.Anomaly.MaxRepeatedFailures, 3)
	if !injEnabled || !credEnabled || maxPerTool != 10 || maxTotal != 30 || maxFail != 3 {
		cfg.AI.Safety = &kaSafetyYAML{
			Sanitization: kaSanitizationYAML{
				InjectionPatternsEnabled: injEnabled,
				CredentialScrubEnabled:   credEnabled,
			},
			Anomaly: kaAnomalyYAML{
				MaxToolCallsPerTool: maxPerTool,
				MaxTotalToolCalls:   maxTotal,
				MaxRepeatedFailures: maxFail,
			},
		}
	}

	if kn.Spec.Monitoring.MonitoringEnabled() {
		cfg.Integrations.Tools = &kaIntegrationsToolsYAML{
			Prometheus: kaIntegrationsPrometheusYAML{URL: OCPPrometheusURL},
		}
	}

	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernaut-agent config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "kubernaut-agent-config", ComponentKubernautAgent),
		Data:       map[string]string{"config.yaml": data},
	}, nil
}

// KubernautAgentLLMRuntimeConfigMap builds the kubernaut-agent-llm-runtime ConfigMap
// when the user hasn't provided a pre-existing one.
func KubernautAgentLLMRuntimeConfigMap(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
	if kn.Spec.KubernautAgent.LLM.RuntimeConfigMapName != "" {
		return nil, nil
	}
	temp := 0.7
	if kn.Spec.KubernautAgent.LLM.Temperature != "" {
		if parsed, err := strconv.ParseFloat(kn.Spec.KubernautAgent.LLM.Temperature, 64); err == nil {
			temp = parsed
		}
	}
	cfg := llmRuntimeYAML{
		Model:          kn.Spec.KubernautAgent.LLM.Model,
		Endpoint:       kn.Spec.KubernautAgent.LLM.Endpoint,
		Temperature:    temp,
		MaxRetries:     intPtrDefault(kn.Spec.KubernautAgent.LLM.MaxRetries, 3),
		TimeoutSeconds: intPtrDefault(kn.Spec.KubernautAgent.LLM.TimeoutSeconds, 120),
	}
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernaut-agent-llm-runtime config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, KubernautAgentLLMRuntimeConfigName(kn), ComponentKubernautAgent),
		Data:       map[string]string{"llm-runtime.yaml": data},
	}, nil
}

// AuthWebhookConfigMap builds the authwebhook-config ConfigMap.
func AuthWebhookConfigMap(kn *kubernautv1alpha1.Kubernaut, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	buf := authWebhookBufferYAML{
		BufferSize:    1000,
		BatchSize:     100,
		FlushInterval: "5s",
		MaxRetries:    3,
	}
	ds := authWebhookDatastorageYAML{
		URL:     DataStorageURL(kn.Namespace),
		Timeout: "30s",
		Buffer:  buf,
	}
	cfg := authWebhookConfigYAML{
		TLSProfile: o.tlsProfile,
		Logging:    loggingYAML{Level: withDefault(kn.Spec.AuthWebhook.Logging.Level, "info")},
		Webhook: authWebhookWebhookYAML{
			Port:            9443,
			CertDir:         "/tmp/k8s-webhook-server/serving-certs",
			HealthProbeAddr: ":8081",
		},
		Datastorage: ds,
	}
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("authwebhook config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "authwebhook-config", ComponentAuthWebhook),
		Data:       map[string]string{"authwebhook.yaml": data},
	}, nil
}

// InterServiceCAConfigMap returns the ConfigMap used for OCP service-ca
// injection that provides the shared CA trust bundle for inter-service TLS.
// All components that communicate with Gateway or DataStorage mount this
// ConfigMap to verify server certificates.
func InterServiceCAConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, InterServiceCAConfigMapName, "inter-service-tls"),
	}
	cm.Annotations = map[string]string{
		OCPServiceCAInjectAnnotation: "true",
	}
	return cm
}

// EffectivenessMonitorServiceCAConfigMap returns the ConfigMap used for
// OCP service-ca injection for EffectivenessMonitor.
func EffectivenessMonitorServiceCAConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	return serviceCAConfigMap(kn, "effectivenessmonitor-service-ca", ComponentEffectivenessMonitor)
}

// KubernautAgentServiceCAConfigMap returns the ConfigMap for OCP service-ca injection
// for Kubernaut Agent.
func KubernautAgentServiceCAConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	return serviceCAConfigMap(kn, "kubernaut-agent-service-ca", ComponentKubernautAgent)
}

func serviceCAConfigMap(kn *kubernautv1alpha1.Kubernaut, name, component string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, name, component),
	}
	cm.Annotations = map[string]string{
		OCPServiceCAInjectAnnotation: "true",
	}
	return cm
}

func withDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

func intDefault(val, def int) int {
	if val != 0 {
		return val
	}
	return def
}

// intPtrDefault dereferences val if non-nil, otherwise returns def.
// This allows explicitly setting 0 as a valid value.
func intPtrDefault(val *int, def int) int {
	if val != nil {
		return *val
	}
	return def
}

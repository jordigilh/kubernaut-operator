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
	"slices"
	"strconv"
	"strings"

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
	ListenAddr            string        `json:"listenAddr" yaml:"listenAddr"`
	HealthAddr            string        `json:"healthAddr" yaml:"healthAddr"`
	MetricsAddr           string        `json:"metricsAddr" yaml:"metricsAddr"`
	MaxConcurrentRequests int           `json:"maxConcurrentRequests" yaml:"maxConcurrentRequests"`
	ReadTimeout           string        `json:"readTimeout" yaml:"readTimeout"`
	WriteTimeout          string        `json:"writeTimeout" yaml:"writeTimeout"`
	IdleTimeout           string        `json:"idleTimeout" yaml:"idleTimeout"`
	K8sRequestTimeout     string        `json:"k8sRequestTimeout" yaml:"k8sRequestTimeout"`
	TLS                   tlsConfigYAML `json:"tls" yaml:"tls"`
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
	CORS        gatewayCORSYAML        `json:"cors" yaml:"cors"`
	Middleware  gatewayMiddlewareYAML  `json:"middleware" yaml:"middleware"`
	Datastorage gatewayDatastorageYAML `json:"datastorage" yaml:"datastorage"`
	Fleet       *fleetConfigYAML       `json:"fleet,omitempty" yaml:"fleet,omitempty"`
}

// fleetConfigYAML mirrors upstream pkg/fleet.FleetConfig's rendered subset
// (ADR-068): scope-checking against FMC's HTTP API or ACM Search's GraphQL
// API, plus the shared MCP Gateway endpoint/auth used for remote-cluster
// reads. See FleetSpec for the CRD-level field documentation.
type fleetConfigYAML struct {
	Enabled            bool             `json:"enabled" yaml:"enabled"`
	Backend            string           `json:"backend" yaml:"backend"`
	Endpoint           string           `json:"endpoint" yaml:"endpoint"`
	TLSCAFile          string           `json:"tlsCAFile,omitempty" yaml:"tlsCAFile,omitempty"`
	TokenPath          string           `json:"tokenPath,omitempty" yaml:"tokenPath,omitempty"`
	MCPGatewayEndpoint string           `json:"mcpGatewayEndpoint,omitempty" yaml:"mcpGatewayEndpoint,omitempty"`
	MCPGatewayType     string           `json:"mcpGatewayType,omitempty" yaml:"mcpGatewayType,omitempty"`
	OAuth2             *fleetOAuth2YAML `json:"oauth2,omitempty" yaml:"oauth2,omitempty"`
}

// fleetOAuth2YAML mirrors upstream pkg/fleet.FleetOAuth2Config. CredentialsSecretRef
// is rendered as the bare Secret name (not a mount path) — GW/RO each build
// their own "/etc/<component>/<credentialsSecretRef>/{client-id,client-secret}"
// path at runtime, so the mount path (deployments.go) must use the same name.
type fleetOAuth2YAML struct {
	Enabled              bool     `json:"enabled" yaml:"enabled"`
	TokenURL             string   `json:"tokenURL,omitempty" yaml:"tokenURL,omitempty"`
	CredentialsSecretRef string   `json:"credentialsSecretRef,omitempty" yaml:"credentialsSecretRef,omitempty"`
	Scopes               []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// fleetCAMountPath and fleetTokenMountPath must stay in sync with the
// volume mounts added in deployments.go (GatewayDeployment /
// RemediationOrchestratorDeployment).
const (
	fleetCAMountPath    = "/etc/fleet-tls/ca/ca.crt"
	fleetTokenMountPath = "/etc/fleet-token/token"
)

// resolveFleetConfig builds the fleet: block rendered into a component's
// ConfigMap. Returns nil when fleet is disabled so the key is omitted
// entirely. credentialsSecretRefOverride, when non-empty, overrides
// spec.fleet.oauth2.credentialsSecretRef for this component only — a
// federated IdP (e.g. Keycloak) issues distinct per-service OAuth2 client
// registrations against one shared token endpoint (confirmed against
// upstream's own Helm chart: kubernaut.fleet.oauth2 helper), so Gateway and
// RemediationOrchestrator must be able to authenticate as different clients.
func resolveFleetConfig(kn *kubernautv1alpha1.Kubernaut, credentialsSecretRefOverride string) *fleetConfigYAML {
	fleet := &kn.Spec.Fleet
	if fleet.Enabled == nil || !*fleet.Enabled {
		return nil
	}
	cfg := &fleetConfigYAML{
		Enabled:            true,
		Backend:            fleet.Backend,
		Endpoint:           resolveFleetEndpoint(kn),
		MCPGatewayEndpoint: fleet.MCPGatewayEndpoint,
		MCPGatewayType:     fleet.MCPGatewayType,
	}
	if fleet.CASecretName != "" {
		cfg.TLSCAFile = fleetCAMountPath
	}
	if fleet.TokenSecretName != "" {
		cfg.TokenPath = fleetTokenMountPath
	}
	if fleet.OAuth2.Enabled {
		cfg.OAuth2 = &fleetOAuth2YAML{
			Enabled:              true,
			TokenURL:             fleet.OAuth2.TokenURL,
			CredentialsSecretRef: withDefault(credentialsSecretRefOverride, fleet.OAuth2.CredentialsSecretRef),
			Scopes:               fleet.OAuth2.Scopes,
		}
	}
	return cfg
}

type gatewayCORSYAML struct {
	AllowedOrigins   []string `json:"allowedOrigins" yaml:"allowedOrigins"`
	AllowedMethods   []string `json:"allowedMethods" yaml:"allowedMethods"`
	AllowCredentials bool     `json:"allowCredentials" yaml:"allowCredentials"`
	MaxAge           int      `json:"maxAge" yaml:"maxAge"`
}

type dataStorageServerYAML struct {
	Port          int           `json:"port" yaml:"port"`
	Host          string        `json:"host" yaml:"host"`
	HealthPort    int           `json:"healthPort" yaml:"healthPort"`
	MetricsPort   int           `json:"metricsPort" yaml:"metricsPort"`
	ReadTimeout   string        `json:"readTimeout" yaml:"readTimeout"`
	WriteTimeout  string        `json:"writeTimeout" yaml:"writeTimeout"`
	TLS           tlsConfigYAML `json:"tls" yaml:"tls"`
	SignerCertDir string        `json:"signerCertDir,omitempty" yaml:"signerCertDir,omitempty"`
}

type dataStorageDatabaseYAML struct {
	Host            string `json:"host" yaml:"host"`
	Port            int32  `json:"port" yaml:"port"`
	Name            string `json:"name" yaml:"name"`
	User            string `json:"user" yaml:"user"`
	SSLMode         string `json:"sslMode" yaml:"sslMode"`
	SSLRootCert     string `json:"sslRootCert,omitempty" yaml:"sslRootCert,omitempty"`
	MaxOpenConns    int    `json:"maxOpenConns" yaml:"maxOpenConns"`
	MaxIdleConns    int    `json:"maxIdleConns" yaml:"maxIdleConns"`
	ConnMaxLifetime string `json:"connMaxLifetime" yaml:"connMaxLifetime"`
	ConnMaxIdleTime string `json:"connMaxIdleTime" yaml:"connMaxIdleTime"`
	SecretsFile     string `json:"secretsFile" yaml:"secretsFile"`
	UsernameKey     string `json:"usernameKey" yaml:"usernameKey"`
	PasswordKey     string `json:"passwordKey" yaml:"passwordKey"`
}

type dataStorageRedisYAML struct {
	Addr             string                   `json:"addr" yaml:"addr"`
	DB               int                      `json:"db" yaml:"db"`
	DLQStreamName    string                   `json:"dlqStreamName" yaml:"dlqStreamName"`
	DLQMaxLen        int                      `json:"dlqMaxLen" yaml:"dlqMaxLen"`
	DLQConsumerGroup string                   `json:"dlqConsumerGroup" yaml:"dlqConsumerGroup"`
	SecretsFile      string                   `json:"secretsFile" yaml:"secretsFile"`
	PasswordKey      string                   `json:"passwordKey" yaml:"passwordKey"`
	TLS              *dataStorageRedisTLSYAML `json:"tls,omitempty" yaml:"tls,omitempty"`
}

type dataStorageRedisTLSYAML struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	CAFile   string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	CertFile string `json:"certFile,omitempty" yaml:"certFile,omitempty"`
	KeyFile  string `json:"keyFile,omitempty" yaml:"keyFile,omitempty"`
}

type dataStorageLoggingYAML struct {
	Level  string `json:"level" yaml:"level"`
	Format string `json:"format" yaml:"format"`
}

type dataStorageConfigYAML struct {
	TLSProfile               string                    `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Server                   dataStorageServerYAML     `json:"server" yaml:"server"`
	Database                 dataStorageDatabaseYAML   `json:"database" yaml:"database"`
	Redis                    dataStorageRedisYAML      `json:"redis" yaml:"redis"`
	Logging                  dataStorageLoggingYAML    `json:"logging" yaml:"logging"`
	EndpointPropagationDelay string                    `json:"endpointPropagationDelay,omitempty" yaml:"endpointPropagationDelay,omitempty"`
	Retention                *dataStorageRetentionYAML `json:"retention,omitempty" yaml:"retention,omitempty"`
}

type dataStorageRetentionYAML struct {
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Interval    string `json:"interval" yaml:"interval"`
	BatchSize   int    `json:"batchSize" yaml:"batchSize"`
	DefaultDays int    `json:"defaultDays" yaml:"defaultDays"`
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
	Fleet                   *fleetConfigYAML       `json:"fleet,omitempty" yaml:"fleet,omitempty"`
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
	MatchRE  map[string]string `json:"matchRe,omitempty" yaml:"matchRe,omitempty"`
}

type notificationRoutingSlackReceiver struct {
	Name  string `json:"name" yaml:"name"`
	Slack struct {
		Channel               string `json:"channel" yaml:"channel"`
		CredentialsSecretName string `json:"credentialsSecretName" yaml:"credentialsSecretName"`
	} `json:"slack" yaml:"slack"`
}

type notificationRoutingSlackYAML struct {
	Route     notificationRoutingSlackRoute      `json:"route" yaml:"route"`
	Receivers []notificationRoutingSlackReceiver `json:"receivers" yaml:"receivers"`
}

type notificationRoutingConsoleReceiver struct {
	Name    string   `json:"name" yaml:"name"`
	Console struct{} `json:"console" yaml:"console"`
}

type notificationRoutingConsoleYAML struct {
	Route     notificationRoutingSlackRoute        `json:"route" yaml:"route"`
	Receivers []notificationRoutingConsoleReceiver `json:"receivers" yaml:"receivers"`
}

type kubernautAgentServerTLSYAML struct {
	CertDir string `json:"certDir" yaml:"certDir"`
}

type kaLoggingYAML struct {
	Level  string `json:"level" yaml:"level"`
	Format string `json:"format" yaml:"format"`
}

type kaRuntimeYAML struct {
	Logging  kaLoggingYAML       `json:"logging" yaml:"logging"`
	Server   kaRuntimeServerYAML `json:"server" yaml:"server"`
	Audit    *kaAuditYAML        `json:"audit,omitempty" yaml:"audit,omitempty"`
	Session  *kaSessionYAML      `json:"session,omitempty" yaml:"session,omitempty"`
	Shutdown kaShutdownYAML      `json:"shutdown" yaml:"shutdown"`
}

type kaShutdownYAML struct {
	DrainSeconds int `json:"drainSeconds" yaml:"drainSeconds"`
}

type kaRuntimeServerYAML struct {
	Address     string                      `json:"address" yaml:"address"`
	Port        int                         `json:"port" yaml:"port"`
	HealthAddr  string                      `json:"healthAddr" yaml:"healthAddr"`
	MetricsAddr string                      `json:"metricsAddr" yaml:"metricsAddr"`
	TLS         kubernautAgentServerTLSYAML `json:"tls" yaml:"tls"`
	TLSProfile  string                      `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	RateLimit   kaRateLimitYAML             `json:"rateLimit" yaml:"rateLimit"`
}

type kaRateLimitYAML struct {
	RequestsPerSecond int `json:"requestsPerSecond" yaml:"requestsPerSecond"`
	Burst             int `json:"burst" yaml:"burst"`
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
	TLSCertFile     string        `json:"tlsCertFile,omitempty" yaml:"tlsCertFile,omitempty"`
	TLSKeyFile      string        `json:"tlsKeyFile,omitempty" yaml:"tlsKeyFile,omitempty"`
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
	Prometheus   kaIntegrationsPrometheusYAML    `json:"prometheus" yaml:"prometheus"`
	Alertmanager *kaIntegrationsAlertmanagerYAML `json:"alertmanager,omitempty" yaml:"alertmanager,omitempty"`
}

type kaIntegrationsPrometheusYAML struct {
	URL       string `json:"url" yaml:"url"`
	TLSCaFile string `json:"tlsCaFile,omitempty" yaml:"tlsCaFile,omitempty"`
}

type kaIntegrationsAlertmanagerYAML struct {
	URL       string `json:"url" yaml:"url"`
	TLSCaFile string `json:"tlsCaFile,omitempty" yaml:"tlsCaFile,omitempty"`
}

type kubernautAgentConfigYAML struct {
	Runtime      kaRuntimeYAML      `json:"runtime" yaml:"runtime"`
	AI           kaAIYAML           `json:"ai" yaml:"ai"`
	Integrations kaIntegrationsYAML `json:"integrations" yaml:"integrations"`
	Interactive  *kaInteractiveYAML `json:"interactive,omitempty" yaml:"interactive,omitempty"`
}

type kaInteractiveYAML struct {
	Enabled               bool   `json:"enabled" yaml:"enabled"`
	SessionTTL            string `json:"sessionTTL,omitempty" yaml:"sessionTTL,omitempty"`
	InactivityTimeout     string `json:"inactivityTimeout,omitempty" yaml:"inactivityTimeout,omitempty"`
	MaxConcurrentSessions *int   `json:"maxConcurrentSessions,omitempty" yaml:"maxConcurrentSessions,omitempty"`
	RateLimitPerUser      *int   `json:"rateLimitPerUser,omitempty" yaml:"rateLimitPerUser,omitempty"`
}

type llmRuntimeYAML struct {
	Provider       string                          `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model          string                          `json:"model" yaml:"model"`
	Endpoint       string                          `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Temperature    float64                         `json:"temperature" yaml:"temperature"` //nolint:musttag
	MaxRetries     int                             `json:"maxRetries" yaml:"maxRetries"`
	TimeoutSeconds int                             `json:"timeoutSeconds" yaml:"timeoutSeconds"`
	VertexProject  string                          `json:"vertexProject,omitempty" yaml:"vertexProject,omitempty"`
	VertexLocation string                          `json:"vertexLocation,omitempty" yaml:"vertexLocation,omitempty"`
	PhaseModels    map[string]llmPhaseOverrideYAML `json:"phaseModels,omitempty" yaml:"phaseModels,omitempty"`
}

// llmPhaseOverrideYAML has no credential field: phase overrides must share
// the primary profile's credentialsSecretName (enforced by
// validateLLMProfileRefs), so the phase always authenticates via the same
// mounted credentials as the primary LLM connection.
type llmPhaseOverrideYAML struct {
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model    string `json:"model,omitempty" yaml:"model,omitempty"`
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
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

	corsOrigins := gwCfg.CORS.AllowedOrigins
	if len(corsOrigins) == 0 {
		corsOrigins = []string{"https://no-browser-clients.invalid"}
	}
	corsMethods := gwCfg.CORS.AllowedMethods
	if len(corsMethods) == 0 {
		corsMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	corsCredentials := false
	if gwCfg.CORS.AllowCredentials != nil {
		corsCredentials = *gwCfg.CORS.AllowCredentials
	}
	corsMaxAge := 300
	if gwCfg.CORS.MaxAge != nil {
		corsMaxAge = *gwCfg.CORS.MaxAge
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
			ListenAddr:            ":8443",
			HealthAddr:            ":8081",
			MetricsAddr:           ":9090",
			MaxConcurrentRequests: 100,
			ReadTimeout:           "3600s",
			WriteTimeout:          "3600s",
			IdleTimeout:           "120s",
			K8sRequestTimeout:     withDefault(gwCfg.K8sRequestTimeout, "15s"),
			TLS:                   tlsConfigYAML{CertDir: InterServiceTLSCertDir},
		},
		CORS: gatewayCORSYAML{
			AllowedOrigins:   corsOrigins,
			AllowedMethods:   corsMethods,
			AllowCredentials: corsCredentials,
			MaxAge:           corsMaxAge,
		},
		Middleware: gatewayMiddlewareYAML{
			TrustedProxyCIDRs: proxyCIDRs,
		},
		Datastorage: gatewayDatastorageYAML{
			URL:     DataStorageURL(ns),
			Timeout: "10s",
			Buffer:  dataStorageBufferYAML{BufferSize: 10000, BatchSize: 100, FlushInterval: "1s", MaxRetries: 3},
		},
		Fleet: resolveFleetConfig(kn, kn.Spec.Gateway.FleetOAuth2CredentialsSecretRef),
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
	pgHost := resolveHostToIP(kn.Spec.PostgreSQL.Host)
	cfg := dataStorageConfigYAML{
		TLSProfile: o.tlsProfile,
		Server:     dataStorageServerConfig(kn),
		Database: func() dataStorageDatabaseYAML {
			sslMode := withDefault(kn.Spec.PostgreSQL.SSLMode, DefaultSSLMode)
			db := dataStorageDatabaseYAML{
				Host:            pgHost,
				Port:            pgPort,
				Name:            dbName,
				User:            dbUser,
				SSLMode:         sslMode,
				MaxOpenConns:    100,
				MaxIdleConns:    20,
				ConnMaxLifetime: "1h",
				ConnMaxIdleTime: "10m",
				SecretsFile:     "/etc/datastorage/secrets/db-secrets.yaml",
				UsernameKey:     "username",
				PasswordKey:     "password",
			}
			if sslMode == DefaultSSLMode {
				db.SSLRootCert = InterServiceTLSCAFile
			}
			return db
		}(),
		Redis: dataStorageRedisConfig(kn),
		Logging: dataStorageLoggingYAML{
			Level:  withDefault(kn.Spec.DataStorage.Logging.Level, "info"),
			Format: "json",
		},
		EndpointPropagationDelay: withDefault(kn.Spec.DataStorage.EndpointPropagationDelay, "10s"),
		Retention:                dataStorageRetentionConfig(kn),
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

func dataStorageRetentionConfig(kn *kubernautv1alpha1.Kubernaut) *dataStorageRetentionYAML {
	r := kn.Spec.DataStorage.Retention
	if r == nil {
		return nil
	}
	days := intPtrDefault(r.DefaultDays, 2555)
	if days > 2555 {
		days = 2555
	}
	return &dataStorageRetentionYAML{
		Enabled:     r.Enabled != nil && *r.Enabled,
		Interval:    withDefault(r.Interval, "24h"),
		BatchSize:   intPtrDefault(r.BatchSize, 1000),
		DefaultDays: days,
	}
}

func dataStorageServerConfig(kn *kubernautv1alpha1.Kubernaut) dataStorageServerYAML {
	s := dataStorageServerYAML{
		Port:         8443,
		Host:         "0.0.0.0",
		HealthPort:   8081,
		MetricsPort:  9090,
		ReadTimeout:  "30s",
		WriteTimeout: "30s",
		TLS:          tlsConfigYAML{CertDir: InterServiceTLSCertDir},
	}
	dir := "/etc/certs"
	if sc := kn.Spec.DataStorage.SigningCert; sc != nil && sc.MountPath != "" {
		dir = sc.MountPath
	}
	s.SignerCertDir = dir
	return s
}

func dataStorageRedisConfig(kn *kubernautv1alpha1.Kubernaut) dataStorageRedisYAML {
	valkeyHost := resolveHostToIP(kn.Spec.Valkey.Host)
	valkeyPort := kn.Spec.Valkey.Port
	if valkeyPort == 0 {
		valkeyPort = DefaultValkeyPort
	}
	r := dataStorageRedisYAML{
		Addr:             fmt.Sprintf("%s:%d", valkeyHost, valkeyPort),
		DB:               0,
		DLQStreamName:    "dlq-stream",
		DLQMaxLen:        1000,
		DLQConsumerGroup: "dlq-group",
		SecretsFile:      "/etc/datastorage/secrets/valkey-secrets.yaml",
		PasswordKey:      "password",
	}
	if kn.Spec.Valkey.ValkeyTLSEnabled() {
		t := &dataStorageRedisTLSYAML{Enabled: true}
		if kn.Spec.Valkey.TLS.CASecretName != "" {
			t.CAFile = "/etc/valkey-tls/ca/ca.crt"
		}
		if kn.Spec.Valkey.TLS.ClientCertSecretName != "" {
			t.CertFile = "/etc/valkey-tls/client/tls.crt"
			t.KeyFile = "/etc/valkey-tls/client/tls.key"
		}
		r.TLS = t
	}
	return r
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
			URL:                 fmt.Sprintf("https://kubernaut-agent.%s.svc.cluster.local:8443", ns),
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
		Fleet:            resolveFleetConfig(kn, kn.Spec.RemediationOrchestrator.FleetOAuth2CredentialsSecretRef),
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
			Route: notificationRoutingSlackRoute{
				Receiver: "slack",
				MatchRE:  map[string]string{"severity": ".*"},
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
		console := notificationRoutingConsoleYAML{
			Route: notificationRoutingSlackRoute{
				Receiver: "console",
			},
			Receivers: []notificationRoutingConsoleReceiver{
				{Name: "console"},
			},
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
	kaProfile, _ := ResolveLLMProfile(kn, ka.LLMProfileRef)
	maxTurns := ka.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 40
	}

	cfg := kubernautAgentConfigYAML{
		Runtime: kaRuntimeYAML{
			Logging: kaLoggingYAML{Level: withDefault(ka.Logging.Level, "info"), Format: "json"},
			Server: kaRuntimeServerYAML{
				Address:     "0.0.0.0",
				Port:        8443,
				HealthAddr:  ":8081",
				MetricsAddr: ":9090",
				TLS:         kubernautAgentServerTLSYAML{CertDir: InterServiceTLSCertDir},
				TLSProfile:  o.tlsProfile,
				RateLimit:   kaRateLimitFromSpec(ka.ServerRateLimit),
			},
			Shutdown: kaShutdownYAML{
				DrainSeconds: intPtrDefault(ka.Shutdown.DrainSeconds, 30),
			},
		},
		AI: kaAIYAML{
			LLM: kaLLMYAML{
				Provider: kaProfile.Provider,
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

	if kaProfile.VertexProject != "" {
		cfg.AI.LLM.VertexProject = kaProfile.VertexProject
	}
	if kaProfile.VertexLocation != "" {
		cfg.AI.LLM.VertexLocation = kaProfile.VertexLocation
	}
	if kaProfile.BedrockRegion != "" {
		cfg.AI.LLM.BedrockRegion = kaProfile.BedrockRegion
	}
	if kaProfile.AzureAPIVersion != "" {
		cfg.AI.LLM.AzureApiVersion = kaProfile.AzureAPIVersion
	}
	if kaProfile.TLSCaFile != "" {
		cfg.AI.LLM.TLSCaFile = kaProfile.TLSCaFile
	}
	if kaProfile.TLSCertFile != "" {
		cfg.AI.LLM.TLSCertFile = kaProfile.TLSCertFile
	}
	if kaProfile.TLSKeyFile != "" {
		cfg.AI.LLM.TLSKeyFile = kaProfile.TLSKeyFile
	}

	if kaProfile.OAuth2.Enabled {
		cfg.AI.LLM.OAuth2 = &kaOAuth2YAML{
			Enabled:        true,
			TokenURL:       kaProfile.OAuth2.TokenURL,
			CredentialsDir: "/etc/kubernaut-agent/oauth2",
			Scopes:         kaProfile.OAuth2.Scopes,
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
	maxTotal := intPtrDefault(ka.Safety.Anomaly.MaxTotalToolCalls, 40)
	maxFail := intPtrDefault(ka.Safety.Anomaly.MaxRepeatedFailures, 3)
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

	if kn.Spec.Monitoring.MonitoringEnabled() {
		cfg.Integrations.Tools = &kaIntegrationsToolsYAML{
			Prometheus: kaIntegrationsPrometheusYAML{
				URL:       OCPPrometheusURL,
				TLSCaFile: "/etc/ssl/ka/service-ca.crt",
			},
			// Alertmanager tools (get_alerts, get_silences) added upstream in
			// kubernaut#1508 (#205). Follows the same SA-bearer-auth-via-service-CA
			// pattern as Prometheus.
			Alertmanager: &kaIntegrationsAlertmanagerYAML{
				URL:       OCPAlertManagerURL,
				TLSCaFile: "/etc/ssl/ka/service-ca.crt",
			},
		}
	}

	if interactive := ka.Interactive; interactive == nil || interactive.InteractiveEnabled() {
		defaultMaxSessions := 100
		defaultRateLimit := 20
		ic := &kaInteractiveYAML{
			Enabled:               true,
			SessionTTL:            "30m",
			InactivityTimeout:     "10m",
			MaxConcurrentSessions: &defaultMaxSessions,
			RateLimitPerUser:      &defaultRateLimit,
		}
		if interactive != nil {
			if interactive.SessionTTL != "" {
				ic.SessionTTL = interactive.SessionTTL
			}
			if interactive.InactivityTimeout != "" {
				ic.InactivityTimeout = interactive.InactivityTimeout
			}
			if interactive.MaxConcurrentSessions != nil {
				ic.MaxConcurrentSessions = interactive.MaxConcurrentSessions
			}
			if interactive.RateLimitPerUser != nil {
				ic.RateLimitPerUser = interactive.RateLimitPerUser
			}
		}
		cfg.Interactive = ic
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
	ka := &kn.Spec.KubernautAgent
	if ka.RuntimeConfigMapName != "" {
		return nil, nil
	}
	kaProfile, _ := ResolveLLMProfile(kn, ka.LLMProfileRef)
	temp := 0.7
	if kaProfile.Temperature != "" {
		if parsed, err := strconv.ParseFloat(kaProfile.Temperature, 64); err == nil {
			temp = parsed
		}
	}
	cfg := llmRuntimeYAML{
		Provider:       kaProfile.Provider,
		Model:          kaProfile.Model,
		Endpoint:       kaProfile.Endpoint,
		Temperature:    temp,
		MaxRetries:     intPtrDefault(kaProfile.MaxRetries, 3),
		TimeoutSeconds: intPtrDefault(kaProfile.TimeoutSeconds, 120),
		VertexProject:  kaProfile.VertexProject,
		VertexLocation: kaProfile.VertexLocation,
	}
	if len(ka.PhaseModels) > 0 {
		cfg.PhaseModels = make(map[string]llmPhaseOverrideYAML, len(ka.PhaseModels))
		for phase, ref := range ka.PhaseModels {
			phaseProfile, _ := ResolveLLMProfile(kn, ref)
			cfg.PhaseModels[phase] = llmPhaseOverrideYAML{
				Provider: phaseProfile.Provider,
				Model:    phaseProfile.Model,
				Endpoint: phaseProfile.Endpoint,
			}
		}
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

// APIFrontendServiceCAConfigMap returns the ConfigMap for OCP service-ca injection
// for API Frontend (used by severity triage to trust the Thanos Querier certificate).
func APIFrontendServiceCAConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	return serviceCAConfigMap(kn, "apifrontend-service-ca", ComponentAPIFrontend)
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

func kaRateLimitFromSpec(spec *kubernautv1alpha1.KARateLimitSpec) kaRateLimitYAML {
	rl := kaRateLimitYAML{RequestsPerSecond: 50, Burst: 100}
	if spec != nil {
		if spec.RequestsPerSecond != nil {
			rl.RequestsPerSecond = *spec.RequestsPerSecond
		}
		if spec.Burst != nil {
			rl.Burst = *spec.Burst
		}
	}
	return rl
}

// intPtrDefault dereferences val if non-nil, otherwise returns def.
// This allows explicitly setting 0 as a valid value.
func intPtrDefault(val *int, def int) int {
	if val != nil {
		return *val
	}
	return def
}

// ---------- APIFrontend ConfigMaps ----------

type afConfigYAML struct {
	Server         afServerYAML         `json:"server" yaml:"server"`
	Agent          afAgentYAML          `json:"agent" yaml:"agent"`
	MCP            afMCPYAML            `json:"mcp" yaml:"mcp"`
	AgentCard      afAgentCardYAML      `json:"agentCard" yaml:"agentCard"`
	Auth           afAuthYAML           `json:"auth" yaml:"auth"`
	RBAC           afRBACYAML           `json:"rbac" yaml:"rbac"`
	Logging        afLoggingYAML        `json:"logging" yaml:"logging"`
	RateLimit      afRateLimitYAML      `json:"rateLimit" yaml:"rateLimit"`
	Shutdown       afShutdownYAML       `json:"shutdown" yaml:"shutdown"`
	SeverityTriage afSeverityTriageYAML `json:"severityTriage" yaml:"severityTriage"`
	Resilience     afResilienceYAML     `json:"resilience" yaml:"resilience"`
	Session        afSessionYAML        `json:"session" yaml:"session"`
}

type afSessionYAML struct {
	Namespace     string `json:"namespace" yaml:"namespace"`
	DisconnectTTL string `json:"disconnectTTL,omitempty" yaml:"disconnectTTL,omitempty"`
	RetentionTTL  string `json:"retentionTTL,omitempty" yaml:"retentionTTL,omitempty"`
}

type afRBACYAML struct {
	SARCacheTTL string `json:"sarCacheTTL" yaml:"sarCacheTTL"`
}

type afServerYAML struct {
	Port        int       `json:"port" yaml:"port"`
	MetricsPort int       `json:"metricsPort,omitempty" yaml:"metricsPort,omitempty"`
	HealthPort  int       `json:"healthPort,omitempty" yaml:"healthPort,omitempty"`
	TLS         afTLSYAML `json:"tls" yaml:"tls"`
}

type afTLSYAML struct {
	CertDir  string `json:"certDir" yaml:"certDir"`
	Required bool   `json:"required" yaml:"required"`
}

type afAgentYAML struct {
	KABaseURL         string         `json:"kaBaseURL" yaml:"kaBaseURL"`
	KAMCPEndpoint     string         `json:"kaMCPEndpoint" yaml:"kaMCPEndpoint"`
	DSBaseURL         string         `json:"dsBaseURL" yaml:"dsBaseURL"`
	DSBearerTokenFile string         `json:"dsBearerTokenFile,omitempty" yaml:"dsBearerTokenFile,omitempty"`
	KABearerTokenFile string         `json:"kaBearerTokenFile,omitempty" yaml:"kaBearerTokenFile,omitempty"`
	KATLSCAFile       string         `json:"kaTlsCaFile" yaml:"kaTlsCaFile"`
	DSTLSCAFile       string         `json:"dsTlsCaFile" yaml:"dsTlsCaFile"`
	LLM               afAgentLLMYAML `json:"llm" yaml:"llm"`
}

type afAgentLLMYAML struct {
	Provider       string                `json:"provider" yaml:"provider"`
	Model          string                `json:"model" yaml:"model"`
	Endpoint       string                `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	APIKeyFile     string                `json:"apiKeyFile,omitempty" yaml:"apiKeyFile,omitempty"`
	VertexProject  string                `json:"vertexProject,omitempty" yaml:"vertexProject,omitempty"`
	VertexLocation string                `json:"vertexLocation,omitempty" yaml:"vertexLocation,omitempty"`
	TLSCaFile      string                `json:"tlsCaFile,omitempty" yaml:"tlsCaFile,omitempty"`
	TLSCertFile    string                `json:"tlsCertFile,omitempty" yaml:"tlsCertFile,omitempty"`
	TLSKeyFile     string                `json:"tlsKeyFile,omitempty" yaml:"tlsKeyFile,omitempty"`
	OAuth2         *afAgentLLMOAuth2YAML `json:"oauth2,omitempty" yaml:"oauth2,omitempty"`
}

type afAgentLLMOAuth2YAML struct {
	Enabled        bool     `json:"enabled" yaml:"enabled"`
	TokenURL       string   `json:"tokenURL,omitempty" yaml:"tokenURL,omitempty"`
	Scopes         []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	CredentialsDir string   `json:"credentialsDir,omitempty" yaml:"credentialsDir,omitempty"`
}

type afMCPYAML struct {
	Enabled            bool              `json:"enabled" yaml:"enabled"`
	SessionIdleTimeout string            `json:"sessionIdleTimeout" yaml:"sessionIdleTimeout"`
	ToolTimeout        string            `json:"toolTimeout" yaml:"toolTimeout"`
	ToolTimeouts       map[string]string `json:"toolTimeouts,omitempty" yaml:"toolTimeouts,omitempty"`
}

type afAgentCardYAML struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	URL  string `json:"url" yaml:"url"`
}

type afAuthYAML struct {
	IssuerURL             string              `json:"issuerURL" yaml:"issuerURL"`
	Audience              string              `json:"audience" yaml:"audience"`
	TokenReviewAudience   string              `json:"tokenReviewAudience,omitempty" yaml:"tokenReviewAudience,omitempty"`
	JWKSURL               string              `json:"jwksURL,omitempty" yaml:"jwksURL,omitempty"`
	OIDCCAFile            string              `json:"oidcCaFile,omitempty" yaml:"oidcCaFile,omitempty"`
	AllowInsecureIssuers  bool                `json:"allowInsecureIssuers,omitempty" yaml:"allowInsecureIssuers,omitempty"`
	KubernetesAuthEnabled bool                `json:"kubernetesAuthEnabled,omitempty" yaml:"kubernetesAuthEnabled,omitempty"`
	ReplayCache           *afReplayCacheYAML  `json:"replayCache,omitempty" yaml:"replayCache,omitempty"`
	JWTProviders          []afJWTProviderYAML `json:"jwtProviders,omitempty" yaml:"jwtProviders,omitempty"`
}

type afJWTProviderYAML struct {
	Name          string               `json:"name" yaml:"name"`
	IssuerURL     string               `json:"issuerURL" yaml:"issuerURL"`
	JWKSURL       string               `json:"jwksURL,omitempty" yaml:"jwksURL,omitempty"`
	Audiences     []string             `json:"audiences" yaml:"audiences"`
	ClaimMappings *afClaimMappingsYAML `json:"claimMappings,omitempty" yaml:"claimMappings,omitempty"`
}

type afClaimMappingsYAML struct {
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	Groups   string `json:"groups,omitempty" yaml:"groups,omitempty"`
}

type afReplayCacheYAML struct {
	Backend         string `json:"backend" yaml:"backend"`
	RedisAddr       string `json:"redisAddr,omitempty" yaml:"redisAddr,omitempty"`
	RedisDB         int    `json:"redisDB,omitempty" yaml:"redisDB,omitempty"`
	CredentialsPath string `json:"credentialsPath,omitempty" yaml:"credentialsPath,omitempty"`
}

type afLoggingYAML struct {
	Level string `json:"level" yaml:"level"`
}

type afRateLimitYAML struct {
	IPRequestsPerSec      int `json:"ipRequestsPerSec" yaml:"ipRequestsPerSec"`
	UserRequestsPerSec    int `json:"userRequestsPerSec" yaml:"userRequestsPerSec"`
	MaxConcurrentSessions int `json:"maxConcurrentSessions" yaml:"maxConcurrentSessions"`
	ToolCallsPerMinute    int `json:"toolCallsPerMinute" yaml:"toolCallsPerMinute"`
}

type afShutdownYAML struct {
	DrainSeconds int `json:"drainSeconds" yaml:"drainSeconds"`
}

type afSeverityTriageYAML struct {
	Enabled                   bool    `json:"enabled" yaml:"enabled"`
	PrometheusURL             string  `json:"prometheusURL" yaml:"prometheusURL"`
	PrometheusTLSCAFile       string  `json:"prometheusTlsCaFile" yaml:"prometheusTlsCaFile"`
	PrometheusBearerTokenFile string  `json:"prometheusBearerTokenFile" yaml:"prometheusBearerTokenFile"`
	CacheTTLSeconds           int     `json:"cacheTTLSeconds" yaml:"cacheTTLSeconds"`
	MaxQueriesPerCall         int     `json:"maxQueriesPerCall" yaml:"maxQueriesPerCall"`
	MaxRulesEvaluated         int     `json:"maxRulesEvaluated" yaml:"maxRulesEvaluated"`
	LLMConfidence             float64 `json:"llmConfidence" yaml:"llmConfidence"`

	// LLM is independent from Agent.LLM (kubernaut#1404). Nil (omitted)
	// means triage inherits AF's main agent.llm connection -- today's
	// default behavior. A non-nil, zero-value LLM forces upstream's
	// rule-based-only Noop triager. A populated LLM configures triage's
	// own independent provider/credentials.
	LLM *afAgentLLMYAML `json:"llm,omitempty" yaml:"llm,omitempty"`
}

type afCircuitBreakerYAML struct {
	ConnectTimeout     string `json:"connectTimeout" yaml:"connectTimeout"`
	RequestTimeout     string `json:"requestTimeout" yaml:"requestTimeout"`
	CBMaxRequests      int    `json:"cbMaxRequests" yaml:"cbMaxRequests"`
	CBInterval         string `json:"cbInterval" yaml:"cbInterval"`
	CBTimeout          string `json:"cbTimeout" yaml:"cbTimeout"`
	CBFailureThreshold int    `json:"cbFailureThreshold" yaml:"cbFailureThreshold"`
	RetryMax           int    `json:"retryMax" yaml:"retryMax"`
	RetryInitBackoff   string `json:"retryInitBackoff,omitempty" yaml:"retryInitBackoff,omitempty"`
	RetryMaxBackoff    string `json:"retryMaxBackoff,omitempty" yaml:"retryMaxBackoff,omitempty"`
	RetryableStatuses  []int  `json:"retryableStatuses" yaml:"retryableStatuses"`
}

type afResilienceYAML struct {
	KA  afCircuitBreakerYAML `json:"ka" yaml:"ka"`
	DS  afCircuitBreakerYAML `json:"ds" yaml:"ds"`
	K8s afCircuitBreakerYAML `json:"k8s" yaml:"k8s"`
}

// KagentiOIDCDefaults holds OIDC values auto-detected from kagenti.
// Exported so the controller can pass them to ConfigMap generation.
type KagentiOIDCDefaults struct {
	IssuerURL            string
	JWKSURL              string
	AllowInsecureIssuers bool
}

// APIFrontendConfigMap generates the apifrontend-config ConfigMap.
// oidc may be nil when kagenti is not active; when non-nil, its values
// fill in any OIDC fields left empty in the CR (CR values always win).
func APIFrontendConfigMap(kn *kubernautv1alpha1.Kubernaut, sidecar KagentiSidecarMode, oidc *KagentiOIDCDefaults) (*corev1.ConfigMap, error) {
	af := kn.Spec.APIFrontend
	ns := kn.Namespace
	afProfile, _ := ResolveLLMProfile(kn, AFLLMProfileRef(kn))

	kaBaseURL := fmt.Sprintf("https://kubernaut-agent.%s.svc.cluster.local:%d", ns, PortHTTPS)
	dsBaseURL := fmt.Sprintf("https://data-storage-service.%s.svc.cluster.local:%d", ns, PortHTTPS)
	agentCardURL := af.AgentCardURL
	if agentCardURL == "" {
		agentCardURL = fmt.Sprintf("https://apifrontend.%s.svc.cluster.local:%d/a2a/invoke", ns, PortHTTPS)
	}

	listenPort := sidecar.AFListenPort()

	afTLS := afTLSYAML{CertDir: "/etc/apifrontend/tls", Required: true}
	if sidecar != KagentiSidecarNone {
		afTLS = afTLSYAML{}
	}

	afServer := afServerYAML{
		Port: int(listenPort),
		TLS:  afTLS,
	}
	if sidecar.ShiftsPorts() {
		afServer.MetricsPort = 9092
		afServer.HealthPort = 8082
	}
	if af.MetricsPort != nil {
		afServer.MetricsPort = int(*af.MetricsPort)
	}
	if af.HealthPort != nil {
		afServer.HealthPort = int(*af.HealthPort)
	}

	cfg := afConfigYAML{
		Server: afServer,
		Agent: afAgentYAML{
			KABaseURL:         kaBaseURL,
			KAMCPEndpoint:     kaBaseURL + "/api/v1/mcp/",
			DSBaseURL:         dsBaseURL,
			DSBearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
			KABearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
			KATLSCAFile:       "/etc/apifrontend/tls-ca/ca.crt",
			DSTLSCAFile:       "/etc/apifrontend/tls-ca/ca.crt",
			LLM:               afAgentLLMConfig(afProfile),
		},
		MCP: afMCPYAML{
			Enabled:            true,
			SessionIdleTimeout: "30m",
			ToolTimeout:        "30s",
			ToolTimeouts: map[string]string{
				"kubernaut_investigate":   "15m",
				"kubernaut_await_session": "3m",
			},
		},
		AgentCard: afAgentCardYAML{
			Name: "Kubernaut Agent",
			URL:  agentCardURL,
		},
		Auth: afAuthConfig(kn, oidc),
		RBAC: afRBACConfig(kn),
		Logging: afLoggingYAML{
			Level: withDefault(af.Logging.Level, "info"),
		},
		RateLimit: afRateLimitYAML{
			IPRequestsPerSec:      intPtrDefault(af.RateLimit.IPRequestsPerSec, 50),
			UserRequestsPerSec:    intPtrDefault(af.RateLimit.UserRequestsPerSec, 20),
			MaxConcurrentSessions: intPtrDefault(af.RateLimit.MaxConcurrentSessions, 100),
			ToolCallsPerMinute:    intPtrDefault(af.RateLimit.ToolCallsPerMinute, 60),
		},
		Shutdown: afShutdownYAML{
			DrainSeconds: intPtrDefault(af.Shutdown.DrainSeconds, 15),
		},
		SeverityTriage: afSeverityTriageConfig(kn),
		Session: afSessionYAML{
			Namespace:     ns,
			DisconnectTTL: "10m",
			RetentionTTL:  "720h",
		},
		Resilience: afResilienceYAML{
			KA: afCircuitBreakerYAML{
				ConnectTimeout: "5s", RequestTimeout: "30s",
				CBMaxRequests: 3, CBInterval: "10s", CBTimeout: "30s", CBFailureThreshold: 5,
				RetryMax: 2, RetryInitBackoff: "500ms", RetryMaxBackoff: "5s",
				RetryableStatuses: []int{502, 503, 504},
			},
			DS: afCircuitBreakerYAML{
				ConnectTimeout: "3s", RequestTimeout: "10s",
				CBMaxRequests: 3, CBInterval: "10s", CBTimeout: "15s", CBFailureThreshold: 3,
				RetryMax: 3, RetryInitBackoff: "200ms", RetryMaxBackoff: "3s",
				RetryableStatuses: []int{502, 503, 504},
			},
			K8s: afCircuitBreakerYAML{
				ConnectTimeout: "5s", RequestTimeout: "30s",
				CBMaxRequests: 3, CBInterval: "10s", CBTimeout: "30s", CBFailureThreshold: 5,
				RetryMax: 0, RetryableStatuses: []int{},
			},
		},
	}

	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("apifrontend config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "apifrontend-config", ComponentAPIFrontend),
		Data:       map[string]string{"config.yaml": data},
	}, nil
}

// afSeverityTriageConfig builds the severityTriage config section. The
// same-credentialsSecretName constraint between severityTriage.llmProfileRef
// and AF's own resolved profile is enforced by validateAFLLMProfileRefs
// at validation time, so rendering here only needs the triage profile itself.
func afSeverityTriageConfig(kn *kubernautv1alpha1.Kubernaut) afSeverityTriageYAML {
	if !kn.Spec.Monitoring.MonitoringEnabled() {
		return afSeverityTriageYAML{Enabled: false}
	}
	cfg := afSeverityTriageYAML{
		Enabled:                   true,
		PrometheusURL:             OCPPrometheusURL,
		PrometheusTLSCAFile:       "/etc/ssl/af/service-ca.crt",
		PrometheusBearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
		CacheTTLSeconds:           30,
		MaxQueriesPerCall:         10,
		MaxRulesEvaluated:         100,
		LLMConfidence:             0.7,
	}

	st := kn.Spec.APIFrontend.SeverityTriage
	switch {
	case !st.LLMTriageEnabled():
		// Non-nil, zero-value LLM forces upstream's rule-based-only Noop triager.
		cfg.LLM = &afAgentLLMYAML{}
	case st != nil && st.LLMProfileRef != "":
		triageProfile, ok := ResolveLLMProfile(kn, st.LLMProfileRef)
		if ok {
			llm := afAgentLLMConfig(triageProfile)
			cfg.LLM = &llm
		}
	}
	// default (nil severityTriage, or empty llmProfileRef): LLM left nil,
	// triage inherits AF's main agent.llm connection.

	return cfg
}

// afAgentLLMConfig builds an agent.llm-shaped config section from a resolved
// LLM profile, matching the schema introduced in kubernaut#1252 / PR#1255.
// Shared by AF's main agent.llm block and severity-triage's independent
// llm block (both need identical provider/credential/OAuth2 translation).
//
// AF and KA have divergent OpenAI config formats (kubernaut#1487/#1488):
//   - CR "openai" -> AF receives "openai_compatible" (superset that works without API key)
//   - AF expects the endpoint to include "/v1"; KA appends it internally
//
// This translation will be removed when upstream normalizes (kubernaut#1488).
func afAgentLLMConfig(llm kubernautv1alpha1.LLMProfileSpec) afAgentLLMYAML {
	provider := llm.Provider
	if provider == "" {
		provider = LLMProviderVertexAI
	}

	afProvider := provider
	if provider == LLMProviderOpenAI {
		afProvider = LLMProviderOpenAICompatible
	}

	endpoint := llm.Endpoint
	if afProvider == LLMProviderOpenAICompatible &&
		endpoint != "" && !strings.HasSuffix(endpoint, "/v1") {
		endpoint = strings.TrimRight(endpoint, "/") + "/v1"
	}

	cfg := afAgentLLMYAML{
		Provider:       afProvider,
		Model:          llm.Model,
		Endpoint:       endpoint,
		VertexProject:  llm.VertexProject,
		VertexLocation: llm.VertexLocation,
		TLSCaFile:      llm.TLSCaFile,
		TLSCertFile:    llm.TLSCertFile,
		TLSKeyFile:     llm.TLSKeyFile,
	}

	if llm.CredentialsSecretName != "" && afProvider != LLMProviderVertexAI {
		cfg.APIKeyFile = "/etc/apifrontend/llm-credentials/api_key"
	}

	if llm.OAuth2.Enabled {
		cfg.OAuth2 = &afAgentLLMOAuth2YAML{
			Enabled:        true,
			TokenURL:       llm.OAuth2.TokenURL,
			Scopes:         llm.OAuth2.Scopes,
			CredentialsDir: "/etc/apifrontend/oauth2",
		}
	}

	return cfg
}

func afAuthConfig(kn *kubernautv1alpha1.Kubernaut, oidc *KagentiOIDCDefaults) afAuthYAML {
	af := kn.Spec.APIFrontend

	issuer := af.Auth.IssuerURL
	jwks := af.Auth.JWKSURL
	insecure := af.Auth.AllowInsecureIssuers

	// Merge kagenti-detected OIDC defaults; CR values always win.
	if oidc != nil {
		if issuer == "" {
			issuer = oidc.IssuerURL
		}
		if jwks == "" {
			jwks = oidc.JWKSURL
		}
		if !insecure {
			insecure = oidc.AllowInsecureIssuers
		}
	}

	auth := afAuthYAML{
		IssuerURL:             issuer,
		Audience:              withDefault(af.Auth.Audience, "kubernaut-apifrontend"),
		TokenReviewAudience:   af.Auth.TokenReviewAudience,
		JWKSURL:               jwks,
		OIDCCAFile:            af.Auth.OIDCCAFile,
		AllowInsecureIssuers:  insecure,
		KubernetesAuthEnabled: true,
	}

	if len(af.Auth.JWTProviders) > 0 {
		providers := make([]afJWTProviderYAML, 0, len(af.Auth.JWTProviders))
		for _, p := range af.Auth.JWTProviders {
			yp := afJWTProviderYAML{
				Name:      p.Name,
				IssuerURL: p.IssuerURL,
				JWKSURL:   p.JWKSURL,
				Audiences: append([]string(nil), p.Audiences...),
			}
			if p.ClaimMappings != nil {
				yp.ClaimMappings = &afClaimMappingsYAML{
					Username: p.ClaimMappings.Username,
					Groups:   p.ClaimMappings.Groups,
				}
			}
			providers = append(providers, yp)
		}
		if kn.Spec.ConsoleEnabled() {
			injectConsoleAudience(providers)
		}
		auth.JWTProviders = providers
	} else if kn.Spec.ConsoleEnabled() && issuer != "" {
		auth.JWTProviders = []afJWTProviderYAML{{
			Name:      "default",
			IssuerURL: issuer,
			JWKSURL:   jwks,
			Audiences: []string{auth.Audience, ComponentConsole},
		}}
	}

	if kn.Spec.Valkey.SecretName != "" {
		auth.ReplayCache = &afReplayCacheYAML{
			Backend:         "redis",
			RedisAddr:       ValkeyAddr(&kn.Spec.Valkey),
			RedisDB:         1,
			CredentialsPath: "/etc/apifrontend/valkey/valkey-secrets.yaml",
		}
	}
	return auth
}

// injectConsoleAudience ensures each JWT provider accepts tokens issued for the
// console OIDC client. Called only when the console is enabled; when disabled the
// audience is simply absent from the regenerated configmap, effectively revoking
// console access on the next AF restart.
func injectConsoleAudience(providers []afJWTProviderYAML) {
	for i := range providers {
		if !slices.Contains(providers[i].Audiences, ComponentConsole) {
			providers[i].Audiences = append(providers[i].Audiences, ComponentConsole)
		}
	}
}

func afRBACConfig(kn *kubernautv1alpha1.Kubernaut) afRBACYAML {
	ttl := "30s"
	if kn.Spec.APIFrontend.RBAC != nil && kn.Spec.APIFrontend.RBAC.SARCacheTTL != "" {
		ttl = kn.Spec.APIFrontend.RBAC.SARCacheTTL
	}
	return afRBACYAML{SARCacheTTL: ttl}
}

// APIFrontendRBACRolesConfigMap generates the default RBAC roles mapping
// ConfigMap. Users can override this by setting spec.apiFrontend.rbacRolesConfigMapRef.
func APIFrontendRBACRolesConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	defaultRoles := `roles:
  admin: ["*"]
  viewer: ["list_investigations", "get_investigation", "search_signals"]
`
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "apifrontend-rbac-roles", ComponentAPIFrontend),
		Data:       map[string]string{"rbac_roles.yaml": defaultRoles},
	}
}

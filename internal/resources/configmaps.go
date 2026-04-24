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

type gatewayMiddlewareYAML struct {
	TrustedProxyCIDRs []string `json:"trustedProxyCIDRs" yaml:"trustedProxyCIDRs"`
}

type gatewayDatastorageYAML struct {
	URL     string `json:"url" yaml:"url"`
	Timeout string `json:"timeout" yaml:"timeout"`
}

type gatewayConfigYAML struct {
	TLSProfile  string                 `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
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
	TLSProfile     string                       `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Controller     controllerBlock              `json:"controller" yaml:"controller"`
	KubernautAgent aiAnalysisKubernautAgentYAML `json:"kubernautAgent" yaml:"kubernautAgent"`
	Datastorage    aiAnalysisDatastorageYAML    `json:"datastorage" yaml:"datastorage"`
	Rego           aiAnalysisRegoYAML           `json:"rego" yaml:"rego"`
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
	Controller  controllerBlock                 `json:"controller" yaml:"controller"`
	Enrichment  signalProcessingEnrichmentYAML  `json:"enrichment" yaml:"enrichment"`
	Classifier  signalProcessingClassifierYAML  `json:"classifier" yaml:"classifier"`
	Datastorage signalProcessingDatastorageYAML `json:"datastorage" yaml:"datastorage"`
}

type roTimeoutsYAML struct {
	Global     string `json:"global" yaml:"global"`
	Processing string `json:"processing" yaml:"processing"`
	Analyzing  string `json:"analyzing" yaml:"analyzing"`
	Executing  string `json:"executing" yaml:"executing"`
	Verifying  string `json:"verifying" yaml:"verifying"`
}

type roRoutingYAML struct {
	ConsecutiveFailureThreshold int    `json:"consecutiveFailureThreshold" yaml:"consecutiveFailureThreshold"`
	ConsecutiveFailureCooldown  string `json:"consecutiveFailureCooldown" yaml:"consecutiveFailureCooldown"`
	RecentlyRemediatedCooldown  string `json:"recentlyRemediatedCooldown" yaml:"recentlyRemediatedCooldown"`
	IneffectiveChainThreshold   int    `json:"ineffectiveChainThreshold" yaml:"ineffectiveChainThreshold"`
	RecurrenceCountThreshold    int    `json:"recurrenceCountThreshold" yaml:"recurrenceCountThreshold"`
	IneffectiveTimeWindow       string `json:"ineffectiveTimeWindow" yaml:"ineffectiveTimeWindow"`
}

type roEffectivenessYAML struct {
	StabilizationWindow string `json:"stabilizationWindow" yaml:"stabilizationWindow"`
}

type roAsyncPropagationYAML struct {
	GitOpsSyncDelay        string `json:"gitOpsSyncDelay" yaml:"gitOpsSyncDelay"`
	OperatorReconcileDelay string `json:"operatorReconcileDelay" yaml:"operatorReconcileDelay"`
	ProactiveAlertDelay    string `json:"proactiveAlertDelay" yaml:"proactiveAlertDelay"`
}

type remediationOrchestratorConfigYAML struct {
	TLSProfile              string                 `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	DataStorageURL          string                 `json:"dataStorageUrl" yaml:"dataStorageUrl"`
	Timeouts                roTimeoutsYAML         `json:"timeouts" yaml:"timeouts"`
	Routing                 roRoutingYAML          `json:"routing" yaml:"routing"`
	EffectivenessAssessment roEffectivenessYAML    `json:"effectivenessAssessment" yaml:"effectivenessAssessment"`
	AsyncPropagation        roAsyncPropagationYAML `json:"asyncPropagation" yaml:"asyncPropagation"`
}

type workflowExecutionAnsibleYAML struct {
	Enabled         bool   `json:"enabled" yaml:"enabled"`
	APIURL          string `json:"apiURL" yaml:"apiURL"`
	OrganizationID  int    `json:"organizationID" yaml:"organizationID"`
	TokenSecretName string `json:"tokenSecretName,omitempty" yaml:"tokenSecretName,omitempty"`
	TokenSecretKey  string `json:"tokenSecretKey,omitempty" yaml:"tokenSecretKey,omitempty"`
}

type workflowExecutionConfigYAML struct {
	TLSProfile         string                        `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	DataStorageURL     string                        `json:"dataStorageUrl" yaml:"dataStorageUrl"`
	WorkflowNamespace  string                        `json:"workflowNamespace" yaml:"workflowNamespace"`
	CooldownPeriod     string                        `json:"cooldownPeriod" yaml:"cooldownPeriod"`
	ServiceAccountName string                        `json:"serviceAccountName" yaml:"serviceAccountName"`
	Ansible            *workflowExecutionAnsibleYAML `json:"ansible,omitempty" yaml:"ansible,omitempty"`
}

type emAssessmentYAML struct {
	StabilizationWindow string `json:"stabilizationWindow" yaml:"stabilizationWindow"`
	ValidityWindow      string `json:"validityWindow" yaml:"validityWindow"`
}

type emDatastorageYAML struct {
	URL string `json:"url" yaml:"url"`
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
	Console notificationConsoleYAML `json:"console" yaml:"console"`
	File    notificationFileYAML    `json:"file" yaml:"file"`
	Log     notificationLogYAML     `json:"log" yaml:"log"`
	Slack   notificationSlackYAML   `json:"slack" yaml:"slack"`
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
	Controller  controllerBlock             `json:"controller" yaml:"controller"`
	Delivery    notificationDeliveryYAML    `json:"delivery" yaml:"delivery"`
	Credentials notificationCredentialsYAML `json:"credentials" yaml:"credentials"`
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

type kubernautAgentDatastorageYAML struct {
	URL string `json:"url" yaml:"url"`
}

type kubernautAgentPrometheusYAML struct {
	URL string `json:"url" yaml:"url"`
}

type kubernautAgentToolsYAML struct {
	Prometheus kubernautAgentPrometheusYAML `json:"prometheus" yaml:"prometheus"`
}

type kubernautAgentServerTLSYAML struct {
	CertDir string `json:"certDir" yaml:"certDir"`
}

type kubernautAgentServerYAML struct {
	Address     string                      `json:"address" yaml:"address"`
	Port        int                         `json:"port" yaml:"port"`
	HealthAddr  string                      `json:"health_addr" yaml:"health_addr"`
	MetricsAddr string                      `json:"metrics_addr" yaml:"metrics_addr"`
	TLS         kubernautAgentServerTLSYAML `json:"tls" yaml:"tls"`
}

type kubernautAgentConfigYAML struct {
	TLSProfile  string                        `json:"tlsProfile,omitempty" yaml:"tlsProfile,omitempty"`
	Server      kubernautAgentServerYAML      `json:"server" yaml:"server"`
	DataStorage kubernautAgentDatastorageYAML `json:"data_storage" yaml:"data_storage"`
	Tools       *kubernautAgentToolsYAML      `json:"tools,omitempty" yaml:"tools,omitempty"`
}

type kubernautAgentSDKConfigYAML struct {
	LLM struct {
		Provider string `json:"provider" yaml:"provider"`
		Model    string `json:"model" yaml:"model"`
	} `json:"llm" yaml:"llm"`
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
			Level:  "info",
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
		Controller: newControllerBlock("aianalysis.kubernaut.ai"),
		KubernautAgent: aiAnalysisKubernautAgentYAML{
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

// RemediationOrchestratorConfigMap builds the remediationorchestrator-config ConfigMap.
func RemediationOrchestratorConfigMap(kn *kubernautv1alpha1.Kubernaut, opts ...ConfigMapOption) (*corev1.ConfigMap, error) {
	o := resolveOpts(opts)
	ro := &kn.Spec.RemediationOrchestrator
	ns := kn.Namespace
	cfg := remediationOrchestratorConfigYAML{
		TLSProfile:     o.tlsProfile,
		DataStorageURL: DataStorageURL(ns),
		Timeouts: roTimeoutsYAML{
			Global:     withDefault(ro.Timeouts.Global, "1h"),
			Processing: withDefault(ro.Timeouts.Processing, "5m"),
			Analyzing:  withDefault(ro.Timeouts.Analyzing, "10m"),
			Executing:  withDefault(ro.Timeouts.Executing, "30m"),
			Verifying:  withDefault(ro.Timeouts.Verifying, "30m"),
		},
		Routing: roRoutingYAML{
			ConsecutiveFailureThreshold: intPtrDefault(ro.Routing.ConsecutiveFailureThreshold, 3),
			ConsecutiveFailureCooldown:  withDefault(ro.Routing.ConsecutiveFailureCooldown, "1h"),
			RecentlyRemediatedCooldown:  withDefault(ro.Routing.RecentlyRemediatedCooldown, "5m"),
			IneffectiveChainThreshold:   intPtrDefault(ro.Routing.IneffectiveChainThreshold, 3),
			RecurrenceCountThreshold:    intPtrDefault(ro.Routing.RecurrenceCountThreshold, 5),
			IneffectiveTimeWindow:       withDefault(ro.Routing.IneffectiveTimeWindow, "4h"),
		},
		EffectivenessAssessment: roEffectivenessYAML{
			StabilizationWindow: withDefault(ro.EffectivenessAssessment.StabilizationWindow, "5m"),
		},
		AsyncPropagation: roAsyncPropagationYAML{
			GitOpsSyncDelay:        withDefault(ro.AsyncPropagation.GitOpsSyncDelay, "3m"),
			OperatorReconcileDelay: withDefault(ro.AsyncPropagation.OperatorReconcileDelay, "1m"),
			ProactiveAlertDelay:    withDefault(ro.AsyncPropagation.ProactiveAlertDelay, "5m"),
		},
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
		TLSProfile:         o.tlsProfile,
		DataStorageURL:     DataStorageURL(kn.Namespace),
		WorkflowNamespace:  wfNs,
		CooldownPeriod:     cooldown,
		ServiceAccountName: "kubernaut-workflow-runner",
	}
	if kn.Spec.Ansible.Enabled {
		ansible := workflowExecutionAnsibleYAML{
			Enabled:        true,
			APIURL:         kn.Spec.Ansible.APIURL,
			OrganizationID: kn.Spec.Ansible.OrganizationID,
		}
		if kn.Spec.Ansible.TokenSecretRef != nil {
			ansible.TokenSecretName = kn.Spec.Ansible.TokenSecretRef.Name
			ansible.TokenSecretKey = withDefault(kn.Spec.Ansible.TokenSecretRef.Key, "token")
		}
		cfg.Ansible = &ansible
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
		Assessment: emAssessmentYAML{
			StabilizationWindow: withDefault(em.Assessment.StabilizationWindow, "30s"),
			ValidityWindow:      withDefault(em.Assessment.ValidityWindow, "120s"),
		},
		Controller: newControllerBlock("effectivenessmonitor.kubernaut.ai"),
		Datastorage: emDatastorageYAML{
			URL: DataStorageURL(kn.Namespace),
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
		},
		Credentials: notificationCredentialsYAML{
			Dir: "/etc/notification/credentials",
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
	cfg := kubernautAgentConfigYAML{
		TLSProfile: o.tlsProfile,
		Server: kubernautAgentServerYAML{
			Address:     "0.0.0.0",
			Port:        8080,
			HealthAddr:  ":8081",
			MetricsAddr: ":9090",
			TLS:         kubernautAgentServerTLSYAML{CertDir: InterServiceTLSCertDir},
		},
		DataStorage: kubernautAgentDatastorageYAML{
			URL: DataStorageURL(ns),
		},
	}
	if kn.Spec.Monitoring.MonitoringEnabled() {
		cfg.Tools = &kubernautAgentToolsYAML{
			Prometheus: kubernautAgentPrometheusYAML{
				URL: OCPPrometheusURL,
			},
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

// KubernautAgentSDKConfigMap builds the kubernaut-agent-sdk-config ConfigMap
// when the user hasn't provided a pre-existing one.
func KubernautAgentSDKConfigMap(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
	if kn.Spec.KubernautAgent.LLM.SdkConfigMapName != "" {
		return nil, nil
	}
	var cfg kubernautAgentSDKConfigYAML
	cfg.LLM.Provider = kn.Spec.KubernautAgent.LLM.Provider
	cfg.LLM.Model = kn.Spec.KubernautAgent.LLM.Model
	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernaut-agent-sdk config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, KubernautAgentSDKConfigName(kn), ComponentKubernautAgent),
		Data:       map[string]string{"sdk-config.yaml": data},
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

// intPtrDefault dereferences val if non-nil, otherwise returns def.
// This allows explicitly setting 0 as a valid value.
func intPtrDefault(val *int, def int) int {
	if val != nil {
		return *val
	}
	return def
}

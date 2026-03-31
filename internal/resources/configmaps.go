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
	"strings"

	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// GatewayConfigMap builds the gateway-config ConfigMap.
func GatewayConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	ns := kn.Namespace
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "gateway-config", ComponentGateway),
		Data: map[string]string{
			"config.yaml": fmt.Sprintf(
				"dataStorageUrl: %s\nlistenAddr: :8080\n",
				DataStorageURL(ns),
			),
		},
	}
}

// DataStorageConfigMap builds the datastorage-config ConfigMap.
func DataStorageConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	pgPort := PostgreSQLPort(kn)

	var b strings.Builder
	fmt.Fprintf(&b, "server:\n")
	fmt.Fprintf(&b, "  port: 8080\n")
	fmt.Fprintf(&b, "  host: \"0.0.0.0\"\n")
	fmt.Fprintf(&b, "  metricsPort: 9090\n")
	fmt.Fprintf(&b, "  readTimeout: 30s\n")
	fmt.Fprintf(&b, "  writeTimeout: 30s\n")
	fmt.Fprintf(&b, "database:\n")
	fmt.Fprintf(&b, "  host: %s\n", kn.Spec.PostgreSQL.Host)
	fmt.Fprintf(&b, "  port: %d\n", pgPort)
	fmt.Fprintf(&b, "  name: kubernaut\n")
	fmt.Fprintf(&b, "  user: kubernaut\n")
	fmt.Fprintf(&b, "  sslMode: disable\n")
	fmt.Fprintf(&b, "  maxOpenConns: 100\n")
	fmt.Fprintf(&b, "  maxIdleConns: 20\n")
	fmt.Fprintf(&b, "  connMaxLifetime: 1h\n")
	fmt.Fprintf(&b, "  connMaxIdleTime: 10m\n")
	fmt.Fprintf(&b, "  secretsFile: \"/etc/datastorage/secrets/db-secrets.yaml\"\n")
	fmt.Fprintf(&b, "  usernameKey: \"username\"\n")
	fmt.Fprintf(&b, "  passwordKey: \"password\"\n")
	fmt.Fprintf(&b, "redis:\n")
	fmt.Fprintf(&b, "  addr: %s\n", ValkeyAddr(&kn.Spec.Valkey))
	fmt.Fprintf(&b, "  db: 0\n")
	fmt.Fprintf(&b, "  dlqStreamName: dlq-stream\n")
	fmt.Fprintf(&b, "  dlqMaxLen: 1000\n")
	fmt.Fprintf(&b, "  dlqConsumerGroup: dlq-group\n")
	fmt.Fprintf(&b, "  secretsFile: \"/etc/datastorage/secrets/valkey-secrets.yaml\"\n")
	fmt.Fprintf(&b, "  passwordKey: \"password\"\n")
	fmt.Fprintf(&b, "logging:\n")
	fmt.Fprintf(&b, "  level: debug\n")
	fmt.Fprintf(&b, "  format: json\n")

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "datastorage-config", ComponentDataStorage),
		Data:       map[string]string{"config.yaml": b.String()},
	}
}

// AIAnalysisConfigMap builds the aianalysis-config ConfigMap.
func AIAnalysisConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	ns := kn.Namespace

	var b strings.Builder
	writeControllerBlock(&b, "aianalysis.kubernaut.ai")
	fmt.Fprintf(&b, "holmesgpt:\n")
	fmt.Fprintf(&b, "  url: \"http://holmesgpt-api-service.%s.svc.cluster.local:8080\"\n", ns)
	fmt.Fprintf(&b, "  timeout: \"180s\"\n")
	fmt.Fprintf(&b, "  sessionPollInterval: \"15s\"\n")
	fmt.Fprintf(&b, "datastorage:\n")
	fmt.Fprintf(&b, "  url: \"%s\"\n", DataStorageURL(ns))
	fmt.Fprintf(&b, "  timeout: \"10s\"\n")
	fmt.Fprintf(&b, "  buffer:\n")
	fmt.Fprintf(&b, "    bufferSize: 20000\n")
	fmt.Fprintf(&b, "    batchSize: 1000\n")
	fmt.Fprintf(&b, "    flushInterval: \"1s\"\n")
	fmt.Fprintf(&b, "    maxRetries: 3\n")
	fmt.Fprintf(&b, "rego:\n")
	fmt.Fprintf(&b, "  policyPath: \"/etc/aianalysis/policies/approval.rego\"\n")
	if kn.Spec.AIAnalysis.ConfidenceThreshold != "" {
		fmt.Fprintf(&b, "  confidenceThreshold: %s\n", kn.Spec.AIAnalysis.ConfidenceThreshold)
	}

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "aianalysis-config", ComponentAIAnalysis),
		Data:       map[string]string{"config.yaml": b.String()},
	}
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
func SignalProcessingConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	ns := kn.Namespace

	var b strings.Builder
	writeControllerBlock(&b, "signalprocessing.kubernaut.ai")
	fmt.Fprintf(&b, "enrichment:\n")
	fmt.Fprintf(&b, "  cacheTtl: \"5m\"\n")
	fmt.Fprintf(&b, "  timeout: \"10s\"\n")
	fmt.Fprintf(&b, "classifier:\n")
	fmt.Fprintf(&b, "  regoConfigMapName: \"%s\"\n", SignalProcessingPolicyName(kn))
	fmt.Fprintf(&b, "  regoConfigMapKey: \"policy.rego\"\n")
	fmt.Fprintf(&b, "  hotReloadInterval: \"30s\"\n")
	fmt.Fprintf(&b, "datastorage:\n")
	fmt.Fprintf(&b, "  url: \"%s\"\n", DataStorageURL(ns))
	fmt.Fprintf(&b, "  timeout: \"10s\"\n")
	fmt.Fprintf(&b, "  buffer:\n")
	fmt.Fprintf(&b, "    bufferSize: 10000\n")
	fmt.Fprintf(&b, "    batchSize: 100\n")
	fmt.Fprintf(&b, "    flushInterval: \"1s\"\n")
	fmt.Fprintf(&b, "    maxRetries: 3\n")

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "signalprocessing-config", ComponentSignalProcessing),
		Data:       map[string]string{"config.yaml": b.String()},
	}
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
func RemediationOrchestratorConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	ro := &kn.Spec.RemediationOrchestrator
	ns := kn.Namespace

	var b strings.Builder
	fmt.Fprintf(&b, "dataStorageUrl: %s\n", DataStorageURL(ns))
	fmt.Fprintf(&b, "timeouts:\n")
	fmt.Fprintf(&b, "  global: %s\n", withDefault(ro.Timeouts.Global, "1h"))
	fmt.Fprintf(&b, "  processing: %s\n", withDefault(ro.Timeouts.Processing, "5m"))
	fmt.Fprintf(&b, "  analyzing: %s\n", withDefault(ro.Timeouts.Analyzing, "10m"))
	fmt.Fprintf(&b, "  executing: %s\n", withDefault(ro.Timeouts.Executing, "30m"))
	fmt.Fprintf(&b, "  verifying: %s\n", withDefault(ro.Timeouts.Verifying, "30m"))
	fmt.Fprintf(&b, "routing:\n")
	fmt.Fprintf(&b, "  consecutiveFailureThreshold: %d\n", intPtrDefault(ro.Routing.ConsecutiveFailureThreshold, 3))
	fmt.Fprintf(&b, "  consecutiveFailureCooldown: %s\n", withDefault(ro.Routing.ConsecutiveFailureCooldown, "1h"))
	fmt.Fprintf(&b, "  recentlyRemediatedCooldown: %s\n", withDefault(ro.Routing.RecentlyRemediatedCooldown, "5m"))
	fmt.Fprintf(&b, "  ineffectiveChainThreshold: %d\n", intPtrDefault(ro.Routing.IneffectiveChainThreshold, 3))
	fmt.Fprintf(&b, "  recurrenceCountThreshold: %d\n", intPtrDefault(ro.Routing.RecurrenceCountThreshold, 5))
	fmt.Fprintf(&b, "  ineffectiveTimeWindow: %s\n", withDefault(ro.Routing.IneffectiveTimeWindow, "4h"))
	fmt.Fprintf(&b, "effectivenessAssessment:\n")
	fmt.Fprintf(&b, "  stabilizationWindow: %s\n", withDefault(ro.EffectivenessAssessment.StabilizationWindow, "5m"))
	fmt.Fprintf(&b, "asyncPropagation:\n")
	fmt.Fprintf(&b, "  gitOpsSyncDelay: %s\n", withDefault(ro.AsyncPropagation.GitOpsSyncDelay, "3m"))
	fmt.Fprintf(&b, "  operatorReconcileDelay: %s\n", withDefault(ro.AsyncPropagation.OperatorReconcileDelay, "1m"))
	fmt.Fprintf(&b, "  proactiveAlertDelay: %s\n", withDefault(ro.AsyncPropagation.ProactiveAlertDelay, "5m"))

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "remediationorchestrator-config", ComponentRemediationOrchestrator),
		Data:       map[string]string{"config.yaml": b.String()},
	}
}

// WorkflowExecutionConfigMap builds the workflowexecution-config ConfigMap.
func WorkflowExecutionConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	we := &kn.Spec.WorkflowExecution
	wfNs := ResolveWorkflowNamespace(kn)
	cooldown := we.CooldownPeriod
	if cooldown == "" {
		cooldown = "1m"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "dataStorageUrl: %s\n", DataStorageURL(kn.Namespace))
	fmt.Fprintf(&b, "workflowNamespace: %s\n", wfNs)
	fmt.Fprintf(&b, "cooldownPeriod: %s\n", cooldown)
	fmt.Fprintf(&b, "serviceAccountName: kubernaut-workflow-runner\n")

	if kn.Spec.Ansible.Enabled {
		fmt.Fprintf(&b, "ansible:\n")
		fmt.Fprintf(&b, "  enabled: true\n")
		fmt.Fprintf(&b, "  apiURL: %s\n", kn.Spec.Ansible.APIURL)
		fmt.Fprintf(&b, "  organizationID: %d\n", kn.Spec.Ansible.OrganizationID)
		if kn.Spec.Ansible.TokenSecretRef != nil {
			fmt.Fprintf(&b, "  tokenSecretName: %s\n", kn.Spec.Ansible.TokenSecretRef.Name)
			fmt.Fprintf(&b, "  tokenSecretKey: %s\n", withDefault(kn.Spec.Ansible.TokenSecretRef.Key, "token"))
		}
	}

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "workflowexecution-config", ComponentWorkflowExecution),
		Data:       map[string]string{"config.yaml": b.String()},
	}
}

// EffectivenessMonitorConfigMap builds the effectivenessmonitor-config ConfigMap.
func EffectivenessMonitorConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	em := &kn.Spec.EffectivenessMonitor

	var b strings.Builder
	fmt.Fprintf(&b, "dataStorageUrl: %s\n", DataStorageURL(kn.Namespace))
	fmt.Fprintf(&b, "assessment:\n")
	fmt.Fprintf(&b, "  stabilizationWindow: %s\n", withDefault(em.Assessment.StabilizationWindow, "30s"))
	fmt.Fprintf(&b, "  validityWindow: %s\n", withDefault(em.Assessment.ValidityWindow, "120s"))

	if kn.Spec.Monitoring.MonitoringEnabled() {
		fmt.Fprintf(&b, "monitoring:\n")
		fmt.Fprintf(&b, "  prometheusUrl: %s\n", OCPPrometheusURL)
		fmt.Fprintf(&b, "  alertManagerUrl: %s\n", OCPAlertManagerURL)
		fmt.Fprintf(&b, "  tlsCaPath: /etc/ssl/effectivenessmonitor/service-ca.crt\n")
	}

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "effectivenessmonitor-config", ComponentEffectivenessMonitor),
		Data:       map[string]string{"config.yaml": b.String()},
	}
}

// NotificationControllerConfigMap builds the notification-controller-config ConfigMap.
func NotificationControllerConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	var b strings.Builder
	writeControllerBlock(&b, "notification.kubernaut.ai")
	fmt.Fprintf(&b, "delivery:\n")
	fmt.Fprintf(&b, "  console:\n")
	fmt.Fprintf(&b, "    enabled: true\n")
	fmt.Fprintf(&b, "  file:\n")
	fmt.Fprintf(&b, "    outputDir: \"/tmp/notifications\"\n")
	fmt.Fprintf(&b, "    format: \"json\"\n")
	fmt.Fprintf(&b, "    timeout: 5s\n")
	fmt.Fprintf(&b, "  log:\n")
	fmt.Fprintf(&b, "    enabled: true\n")
	fmt.Fprintf(&b, "    format: \"json\"\n")
	fmt.Fprintf(&b, "  slack:\n")
	fmt.Fprintf(&b, "    timeout: 10s\n")
	fmt.Fprintf(&b, "  credentials:\n")
	fmt.Fprintf(&b, "    dir: \"/etc/notification/credentials\"\n")
	fmt.Fprintf(&b, "datastorage:\n")
	fmt.Fprintf(&b, "  url: \"%s\"\n", DataStorageURL(kn.Namespace))
	fmt.Fprintf(&b, "  timeout: 10s\n")
	fmt.Fprintf(&b, "  buffer:\n")
	fmt.Fprintf(&b, "    bufferSize: 10000\n")
	fmt.Fprintf(&b, "    batchSize: 100\n")
	fmt.Fprintf(&b, "    flushInterval: 1s\n")
	fmt.Fprintf(&b, "    maxRetries: 3\n")

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "notification-controller-config", ComponentNotification),
		Data:       map[string]string{"config.yaml": b.String()},
	}
}

// NotificationRoutingConfigMap builds the notification-routing-config ConfigMap.
// When Slack is configured, routes are generated; otherwise a console-only fallback is used.
func NotificationRoutingConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	slack := kn.Spec.Notification.Slack
	channel := slack.Channel
	if channel == "" {
		channel = "#kubernaut-alerts"
	}

	var routing string
	if slack.SecretName != "" {
		routing = fmt.Sprintf(
			"routes:\n  - receiver: slack\n    match:\n      severity: \".*\"\nreceivers:\n  - name: slack\n    slack:\n      channel: %q\n      credentialsSecretName: %s\n",
			channel, slack.SecretName,
		)
	} else {
		routing = "routes: []\nreceivers:\n  - name: console\n    console: {}\n"
	}

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "notification-routing-config", ComponentNotification),
		Data:       map[string]string{"routing.yaml": routing},
	}
}

// HolmesGPTAPIConfigMap builds the holmesgpt-api-config ConfigMap.
func HolmesGPTAPIConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	ns := kn.Namespace

	var b strings.Builder
	fmt.Fprintf(&b, "dataStorageUrl: %s\n", DataStorageURL(ns))
	fmt.Fprintf(&b, "gatewayUrl: %s\n", GatewayURL(ns))
	fmt.Fprintf(&b, "listenAddr: :8080\n")
	fmt.Fprintf(&b, "metricsAddr: :8080\n")

	if kn.Spec.Monitoring.MonitoringEnabled() {
		fmt.Fprintf(&b, "monitoring:\n")
		fmt.Fprintf(&b, "  prometheusUrl: %s\n", OCPPrometheusURL)
		fmt.Fprintf(&b, "  tlsCaPath: /etc/ssl/hapi/service-ca.crt\n")
	}

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "holmesgpt-api-config", ComponentHolmesGPTAPI),
		Data:       map[string]string{"config.yaml": b.String()},
	}
}

// HolmesGPTSDKConfigMap builds the holmesgpt-sdk-config ConfigMap
// when the user hasn't provided a pre-existing one.
func HolmesGPTSDKConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	if kn.Spec.HolmesGPTAPI.LLM.SdkConfigMapName != "" {
		return nil
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, HolmesGPTSDKConfigName(kn), ComponentHolmesGPTAPI),
		Data: map[string]string{
			"sdk-config.yaml": fmt.Sprintf(
				"llm:\n  provider: %s\n  model: %s\n",
				kn.Spec.HolmesGPTAPI.LLM.Provider,
				kn.Spec.HolmesGPTAPI.LLM.Model,
			),
		},
	}
}

// AuthWebhookConfigMap builds the authwebhook-config ConfigMap.
func AuthWebhookConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	var b strings.Builder
	fmt.Fprintf(&b, "webhook:\n")
	fmt.Fprintf(&b, "  port: 9443\n")
	fmt.Fprintf(&b, "  certDir: /tmp/k8s-webhook-server/serving-certs\n")
	fmt.Fprintf(&b, "  healthProbeAddr: \":8081\"\n")
	fmt.Fprintf(&b, "datastorage:\n")
	fmt.Fprintf(&b, "  url: \"%s\"\n", DataStorageURL(kn.Namespace))
	fmt.Fprintf(&b, "  timeout: 30s\n")
	fmt.Fprintf(&b, "  buffer:\n")
	fmt.Fprintf(&b, "    bufferSize: 1000\n")
	fmt.Fprintf(&b, "    batchSize: 100\n")
	fmt.Fprintf(&b, "    flushInterval: 5s\n")
	fmt.Fprintf(&b, "    maxRetries: 3\n")

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "authwebhook-config", ComponentAuthWebhook),
		Data:       map[string]string{"authwebhook.yaml": b.String()},
	}
}

// EffectivenessMonitorServiceCAConfigMap returns the ConfigMap used for
// OCP service-ca injection for EffectivenessMonitor.
func EffectivenessMonitorServiceCAConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	return serviceCAConfigMap(kn, "effectivenessmonitor-service-ca", ComponentEffectivenessMonitor)
}

// HolmesGPTAPIServiceCAConfigMap returns the ConfigMap for OCP service-ca injection
// for HolmesGPT-API.
func HolmesGPTAPIServiceCAConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	return serviceCAConfigMap(kn, "holmesgpt-api-service-ca", ComponentHolmesGPTAPI)
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

func writeControllerBlock(b *strings.Builder, leaderElectionID string) {
	fmt.Fprintf(b, "controller:\n")
	fmt.Fprintf(b, "  metricsAddr: \":9090\"\n")
	fmt.Fprintf(b, "  healthProbeAddr: \":8081\"\n")
	fmt.Fprintf(b, "  leaderElection: false\n")
	fmt.Fprintf(b, "  leaderElectionId: %q\n", leaderElectionID)
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

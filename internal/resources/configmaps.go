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
	pgPort := kn.Spec.PostgreSQL.Port
	if pgPort == 0 {
		pgPort = 5432
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "datastorage-config", ComponentDataStorage),
		Data: map[string]string{
			"config.yaml": fmt.Sprintf(
				"listenAddr: :8080\npostgresql:\n  host: %s\n  port: %d\nvalkey:\n  addr: %s\n",
				kn.Spec.PostgreSQL.Host,
				pgPort,
				ValkeyAddr(&kn.Spec.Valkey),
			),
		},
	}
}

// AIAnalysisConfigMap builds the aianalysis-config ConfigMap.
func AIAnalysisConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	ns := kn.Namespace
	conf := fmt.Sprintf(
		"dataStorageUrl: %s\nholmesGptApiUrl: http://holmesgpt-api-service.%s.svc.cluster.local:8080\n",
		DataStorageURL(ns), ns,
	)
	if kn.Spec.AIAnalysis.ConfidenceThreshold != "" {
		conf += fmt.Sprintf("confidenceThreshold: %s\n", kn.Spec.AIAnalysis.ConfidenceThreshold)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "aianalysis-config", ComponentAIAnalysis),
		Data:       map[string]string{"config.yaml": conf},
	}
}

// SignalProcessingConfigMap builds the signalprocessing-config ConfigMap.
func SignalProcessingConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	ns := kn.Namespace
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "signalprocessing-config", ComponentSignalProcessing),
		Data: map[string]string{
			"config.yaml": fmt.Sprintf(
				"dataStorageUrl: %s\ngatewayUrl: %s\n",
				DataStorageURL(ns), GatewayURL(ns),
			),
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
	wfNs := we.WorkflowNamespace
	if wfNs == "" {
		wfNs = DefaultWorkflowNamespace
	}
	cooldown := we.CooldownPeriod
	if cooldown == "" {
		cooldown = "1m"
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "workflowexecution-config", ComponentWorkflowExecution),
		Data: map[string]string{
			"config.yaml": fmt.Sprintf(
				"dataStorageUrl: %s\nworkflowNamespace: %s\ncooldownPeriod: %s\n",
				DataStorageURL(kn.Namespace), wfNs, cooldown,
			),
		},
	}
}

// EffectivenessMonitorConfigMap builds the effectivenessmonitor-config ConfigMap.
func EffectivenessMonitorConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	em := &kn.Spec.EffectivenessMonitor
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "effectivenessmonitor-config", ComponentEffectivenessMonitor),
		Data: map[string]string{
			"config.yaml": fmt.Sprintf(
				"dataStorageUrl: %s\nassessment:\n  stabilizationWindow: %s\n  validityWindow: %s\n",
				DataStorageURL(kn.Namespace),
				withDefault(em.Assessment.StabilizationWindow, "30s"),
				withDefault(em.Assessment.ValidityWindow, "120s"),
			),
		},
	}
}

// NotificationControllerConfigMap builds the notification-controller-config ConfigMap.
func NotificationControllerConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "notification-controller-config", ComponentNotification),
		Data: map[string]string{
			"config.yaml": fmt.Sprintf(
				"dataStorageUrl: %s\n",
				DataStorageURL(kn.Namespace),
			),
		},
	}
}

// NotificationRoutingConfigMap builds the notification-routing-config ConfigMap.
// Returns nil if no routing ConfigMap is specified (operator generates a minimal one).
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
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "holmesgpt-api-config", ComponentHolmesGPTAPI),
		Data: map[string]string{
			"config.yaml": fmt.Sprintf(
				"dataStorageUrl: %s\ngatewayUrl: %s\nlistenAddr: :8080\nmetricsAddr: :8080\n",
				DataStorageURL(ns), GatewayURL(ns),
			),
		},
	}
}

// HolmesGPTSDKConfigMap builds the holmesgpt-sdk-config ConfigMap
// when the user hasn't provided a pre-existing one.
func HolmesGPTSDKConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	if kn.Spec.HolmesGPTAPI.LLM.SdkConfigMapName != "" {
		return nil
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "holmesgpt-sdk-config", ComponentHolmesGPTAPI),
		Data: map[string]string{
			"sdk-config.yaml": fmt.Sprintf(
				"provider: %s\nmodel: %s\n",
				kn.Spec.HolmesGPTAPI.LLM.Provider,
				kn.Spec.HolmesGPTAPI.LLM.Model,
			),
		},
	}
}

// AuthWebhookConfigMap builds the authwebhook-config ConfigMap.
func AuthWebhookConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "authwebhook-config", ComponentAuthWebhook),
		Data: map[string]string{
			"config.yaml": fmt.Sprintf(
				"dataStorageUrl: %s\n",
				DataStorageURL(kn.Namespace),
			),
		},
	}
}

// EffectivenessMonitorServiceCAConfigMap returns the ConfigMap used for
// OCP service-ca injection for EffectivenessMonitor.
func EffectivenessMonitorServiceCAConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "effectivenessmonitor-service-ca", ComponentEffectivenessMonitor),
	}
	cm.Annotations = map[string]string{
		"service.beta.openshift.io/inject-cabundle": "true",
	}
	return cm
}

// HolmesGPTAPIServiceCAConfigMap returns the ConfigMap for OCP service-ca injection
// for HolmesGPT-API.
func HolmesGPTAPIServiceCAConfigMap(kn *kubernautv1alpha1.Kubernaut) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "holmesgpt-api-service-ca", ComponentHolmesGPTAPI),
	}
	cm.Annotations = map[string]string{
		"service.beta.openshift.io/inject-cabundle": "true",
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

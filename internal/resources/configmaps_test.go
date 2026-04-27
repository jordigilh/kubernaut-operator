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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

const injectCABundleAnnotationValue = "true"

func TestGatewayConfigMap_ContainsDataStorageURL(t *testing.T) {
	kn := testKubernaut()
	cm, err := GatewayConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	if cm.Name != "gateway-config" {
		t.Errorf("name = %q, want %q", cm.Name, "gateway-config")
	}
	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "https://data-storage-service.kubernaut-system.svc.cluster.local") {
		t.Errorf("gateway config should reference DataStorage HTTPS URL, got:\n%s", data)
	}
	if !strings.Contains(data, "k8sRequestTimeout") {
		t.Errorf("gateway config should contain k8sRequestTimeout, got:\n%s", data)
	}
	if !strings.Contains(data, "trustedProxyCIDRs") {
		t.Errorf("gateway config should contain trustedProxyCIDRs, got:\n%s", data)
	}
	if !strings.Contains(data, "maxConcurrentRequests") {
		t.Errorf("gateway config should contain maxConcurrentRequests, got:\n%s", data)
	}
}

func TestGatewayConfigMap_CustomK8sTimeout(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.Gateway.Config.K8sRequestTimeout = "30s"
	cm, err := GatewayConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "k8sRequestTimeout: 30s") {
		t.Errorf("gateway config should respect custom k8sRequestTimeout, got:\n%s", data)
	}
}

func TestDataStorageConfigMap_ContainsPgAndValkey(t *testing.T) {
	kn := testKubernaut()
	cm, err := DataStorageConfigMap(kn, "kubernautdb", "kubernautuser")
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "host: pg.example.com") {
		t.Errorf("datastorage config should contain PG host, got:\n%s", data)
	}
	if !strings.Contains(data, "addr: valkey.example.com:6379") {
		t.Errorf("datastorage config should contain Valkey addr, got:\n%s", data)
	}
	if !strings.Contains(data, "secretsFile: /etc/datastorage/secrets/db-secrets.yaml") {
		t.Errorf("datastorage config should reference db secrets file, got:\n%s", data)
	}
}

func TestDataStorageConfigMap_DefaultPort(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.PostgreSQL.Port = 0
	cm, err := DataStorageConfigMap(kn, "kubernautdb", "kubernautuser")
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "port: 5432") {
		t.Errorf("datastorage config should default to port 5432, got:\n%s", data)
	}
}

func TestAIAnalysisConfigMap_IncludesConfidenceThreshold(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.AIAnalysis.ConfidenceThreshold = "0.85"
	cm, err := AIAnalysisConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "confidenceThreshold") || !strings.Contains(data, "0.85") {
		t.Errorf("aianalysis config should contain confidence threshold, got:\n%s", data)
	}
}

func TestAIAnalysisConfigMap_AgentKey(t *testing.T) {
	kn := testKubernaut()
	cm, err := AIAnalysisConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "agent:") {
		t.Errorf("aianalysis config should contain 'agent:' key, got:\n%s", data)
	}
	if strings.Contains(data, "kubernautAgent:") {
		t.Errorf("aianalysis config should not contain old 'kubernautAgent:' key, got:\n%s", data)
	}
}

func TestAIAnalysisConfigMap_OmitsThresholdWhenEmpty(t *testing.T) {
	kn := testKubernaut()
	cm, err := AIAnalysisConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["config.yaml"]
	if strings.Contains(data, "confidenceThreshold") {
		t.Errorf("aianalysis config should not contain threshold when empty, got:\n%s", data)
	}
}

func TestSignalProcessingConfigMap_ContainsDataStorageURL(t *testing.T) {
	kn := testKubernaut()
	cm, err := SignalProcessingConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "data-storage-service.kubernaut-system.svc.cluster.local") {
		t.Errorf("signalprocessing config should contain datastorage URL, got:\n%s", data)
	}
	if !strings.Contains(data, "classifier:") {
		t.Errorf("signalprocessing config should contain classifier section, got:\n%s", data)
	}
}

func TestRemediationOrchestratorConfigMap_Defaults(t *testing.T) {
	kn := testKubernaut()
	cm, err := RemediationOrchestratorConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["remediationorchestrator.yaml"]
	defaults := []string{
		"global: 1h", "processing: 5m", "analyzing: 10m", "executing: 30m", "verifying: 30m",
		"ineffectiveChainThreshold: 3", "recurrenceCountThreshold: 5", "ineffectiveTimeWindow: 4h",
	}
	for _, d := range defaults {
		if !strings.Contains(data, d) {
			t.Errorf("RO config should contain default %q, got:\n%s", d, data)
		}
	}
}

func TestRemediationOrchestratorConfigMap_NestedStructure(t *testing.T) {
	kn := testKubernaut()
	cm, err := RemediationOrchestratorConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["remediationorchestrator.yaml"]

	for _, want := range []string{
		"controller:",
		"leaderElectionId: remediationorchestrator.kubernaut.ai",
		"datastorage:",
		"url: https://data-storage-service",
		"timeout:",
		"buffer:",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("RO config should contain %q, got:\n%s", want, data)
		}
	}
	if strings.Contains(data, "dataStorageUrl") {
		t.Errorf("RO config should not contain flat dataStorageUrl key, got:\n%s", data)
	}
}

func TestRemediationOrchestratorConfigMap_CustomValues(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.RemediationOrchestrator.Timeouts.Global = "2h"
	kn.Spec.RemediationOrchestrator.Timeouts.Processing = "10m"
	cm, err := RemediationOrchestratorConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["remediationorchestrator.yaml"]
	if !strings.Contains(data, "global: 2h") {
		t.Errorf("RO config should use custom global timeout, got:\n%s", data)
	}
	if !strings.Contains(data, "processing: 10m") {
		t.Errorf("RO config should use custom processing timeout, got:\n%s", data)
	}
}

func TestWorkflowExecutionConfigMap_DefaultNamespace(t *testing.T) {
	kn := testKubernaut()
	cm, err := WorkflowExecutionConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["workflowexecution.yaml"]
	if !strings.Contains(data, "kubernaut-workflows") {
		t.Errorf("WE config should use default workflow namespace, got:\n%s", data)
	}
}

func TestWorkflowExecutionConfigMap_CustomNamespace(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.WorkflowExecution.WorkflowNamespace = "custom-wf"
	cm, err := WorkflowExecutionConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["workflowexecution.yaml"]
	if !strings.Contains(data, "custom-wf") {
		t.Errorf("WE config should use custom workflow namespace, got:\n%s", data)
	}
}

func TestEffectivenessMonitorConfigMap_Defaults(t *testing.T) {
	kn := testKubernaut()
	cm, err := EffectivenessMonitorConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["effectivenessmonitor.yaml"]
	if !strings.Contains(data, "stabilizationWindow: 30s") {
		t.Errorf("EM config should have default stabilization window, got:\n%s", data)
	}
}

func TestNotificationRoutingConfigMap_SlackConfigured(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.Notification.Slack.SecretName = "slack-webhook"
	kn.Spec.Notification.Slack.Channel = "#ops"
	cm, err := NotificationRoutingConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["routing.yaml"]
	if !strings.Contains(data, "slack") {
		t.Errorf("routing config should reference slack receiver, got:\n%s", data)
	}
	if !strings.Contains(data, "#ops") {
		t.Errorf("routing config should contain channel #ops, got:\n%s", data)
	}
}

func TestNotificationRoutingConfigMap_NoSlack(t *testing.T) {
	kn := testKubernaut()
	cm, err := NotificationRoutingConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	data := cm.Data["routing.yaml"]
	if !strings.Contains(data, "console") {
		t.Errorf("routing config without slack should use console receiver, got:\n%s", data)
	}
	if strings.Contains(data, "slack") {
		t.Errorf("routing config should not contain slack when Slack is unconfigured, got:\n%s", data)
	}
}

func TestNotificationControllerConfigMap_CredentialsInDelivery(t *testing.T) {
	kn := testKubernaut()
	cm, err := NotificationControllerConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "delivery:") {
		t.Errorf("notification config should contain delivery: block, got:\n%s", data)
	}
	if !strings.Contains(data, "credentials:") {
		t.Errorf("notification config should contain credentials: block, got:\n%s", data)
	}
	if !strings.Contains(data, "dir: /etc/notification/credentials") {
		t.Errorf("notification config should contain credentials dir, got:\n%s", data)
	}
}

func TestKubernautAgentSDKConfigMap_GeneratedWhenNoExisting(t *testing.T) {
	kn := testKubernaut()
	cm, err := KubernautAgentSDKConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	if cm == nil {
		t.Fatal("KubernautAgentSDKConfigMap should not be nil when no existing CM specified")
	}
	data := cm.Data["sdk-config.yaml"]
	if !strings.Contains(data, "provider: openai") {
		t.Errorf("SDK config should contain llm.provider, got:\n%s", data)
	}
	if !strings.Contains(data, "model: gpt-4o") {
		t.Errorf("SDK config should contain model, got:\n%s", data)
	}
}

func TestKubernautAgentSDKConfigMap_NilWhenExistingProvided(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.KubernautAgent.LLM.SdkConfigMapName = "my-sdk-config"
	cm, err := KubernautAgentSDKConfigMap(kn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cm != nil {
		t.Error("KubernautAgentSDKConfigMap should be nil when user provides existing CM")
	}
}

func TestInterServiceCAConfigMap_HasInjectAnnotation(t *testing.T) {
	kn := testKubernaut()
	cm := InterServiceCAConfigMap(kn)
	if cm.Name != InterServiceCAConfigMapName {
		t.Errorf("name = %q, want %q", cm.Name, InterServiceCAConfigMapName)
	}
	v, ok := cm.Annotations[OCPServiceCAInjectAnnotation]
	if !ok || v != injectCABundleAnnotationValue {
		t.Error("inter-service-ca ConfigMap should have inject-cabundle annotation")
	}
}

func TestServiceCAConfigMaps_HaveAnnotation(t *testing.T) {
	kn := testKubernaut()
	cms := []*struct {
		name string
		fn   func(*testing.T)
	}{
		{"effectivenessmonitor-service-ca", func(t *testing.T) {
			cm := EffectivenessMonitorServiceCAConfigMap(kn)
			if cm.Annotations["service.beta.openshift.io/inject-cabundle"] != injectCABundleAnnotationValue {
				t.Error("EM service-ca ConfigMap should have inject-cabundle annotation")
			}
		}},
		{"kubernaut-agent-service-ca", func(t *testing.T) {
			cm := KubernautAgentServiceCAConfigMap(kn)
			if cm.Annotations["service.beta.openshift.io/inject-cabundle"] != injectCABundleAnnotationValue {
				t.Error("KA service-ca ConfigMap should have inject-cabundle annotation")
			}
		}},
	}
	for _, tc := range cms {
		t.Run(tc.name, tc.fn)
	}
}

func TestEffectivenessMonitorConfigMap_MonitoringURLs(t *testing.T) {
	kn := testKubernaut()
	cm, err := EffectivenessMonitorConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["effectivenessmonitor.yaml"]

	if !strings.Contains(data, OCPPrometheusURL) {
		t.Errorf("EM config should contain Prometheus URL when monitoring enabled, got:\n%s", data)
	}
	if !strings.Contains(data, OCPAlertManagerURL) {
		t.Errorf("EM config should contain AlertManager URL when monitoring enabled, got:\n%s", data)
	}
	if !strings.Contains(data, "external:") {
		t.Errorf("EM config should contain external section when monitoring enabled, got:\n%s", data)
	}
	if !strings.Contains(data, "tlsCaFile: /etc/ssl/em/service-ca.crt") {
		t.Errorf("EM config should contain external.tlsCaFile when monitoring enabled, got:\n%s", data)
	}
}

func TestEffectivenessMonitorConfigMap_NoMonitoringURLsWhenDisabled(t *testing.T) {
	kn := testKubernaut()
	disabled := false
	kn.Spec.Monitoring.Enabled = &disabled
	cm, err := EffectivenessMonitorConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["effectivenessmonitor.yaml"]

	if strings.Contains(data, "external:") {
		t.Errorf("EM config should not contain external monitoring section when disabled, got:\n%s", data)
	}
}

func TestKubernautAgentConfigMap_MonitoringURL(t *testing.T) {
	kn := testKubernaut()
	cm, err := KubernautAgentConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["config.yaml"]

	if !strings.Contains(data, OCPPrometheusURL) {
		t.Errorf("KA config should contain Prometheus URL when monitoring enabled, got:\n%s", data)
	}
	if !strings.Contains(data, "data_storage:") {
		t.Errorf("KA config should contain upstream data_storage section, got:\n%s", data)
	}
	if !strings.Contains(data, "url: https://data-storage-service.kubernaut-system.svc.cluster.local:8080") {
		t.Errorf("KA config should contain HTTPS data_storage.url, got:\n%s", data)
	}
	if !strings.Contains(data, "tools:") || !strings.Contains(data, "prometheus:") {
		t.Errorf("KA config should contain upstream tools.prometheus section when monitoring enabled, got:\n%s", data)
	}
}

func TestKubernautAgentConfigMap_NoMonitoringWhenDisabled(t *testing.T) {
	kn := testKubernaut()
	disabled := false
	kn.Spec.Monitoring.Enabled = &disabled
	cm, err := KubernautAgentConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["config.yaml"]

	if strings.Contains(data, "prometheusUrl") || strings.Contains(data, "tools:") {
		t.Errorf("KA config should not contain Prometheus tools section when monitoring is disabled, got:\n%s", data)
	}
}

func TestAuthWebhookConfigMap_UsesDefaultConfigFilename(t *testing.T) {
	kn := testKubernaut()
	cm, err := AuthWebhookConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := cm.Data["authwebhook.yaml"]; !ok {
		t.Fatalf("AuthWebhookConfigMap should write authwebhook.yaml, keys: %#v", cm.Data)
	}
}

func TestWorkflowExecutionConfigMap_AWXWiring(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.Ansible.Enabled = true
	kn.Spec.Ansible.APIURL = "https://awx.example.com"
	kn.Spec.Ansible.OrganizationID = 42
	kn.Spec.Ansible.TokenSecretRef = &kubernautv1alpha1.SecretKeyRef{
		Name: "awx-token",
		Key:  "api-token",
	}
	cm, err := WorkflowExecutionConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["workflowexecution.yaml"]

	for _, want := range []string{
		"ansible:",
		"apiURL: https://awx.example.com",
		"organizationID: 42",
		"tokenSecretRef:",
		"name: awx-token",
		"key: api-token",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("WE config should contain %q when Ansible enabled, got:\n%s", want, data)
		}
	}
}

func TestWorkflowExecutionConfigMap_NoAWXWhenDisabled(t *testing.T) {
	kn := testKubernaut()
	cm, err := WorkflowExecutionConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["workflowexecution.yaml"]

	if strings.Contains(data, "ansible:") {
		t.Errorf("WE config should not contain ansible section when disabled, got:\n%s", data)
	}
}

func TestWorkflowExecutionConfigMap_NestedStructure(t *testing.T) {
	kn := testKubernaut()
	cm, err := WorkflowExecutionConfigMap(kn)
	if err != nil {
		t.Fatal(err)
	}
	data := cm.Data["workflowexecution.yaml"]

	for _, want := range []string{
		"execution:",
		"namespace: kubernaut-workflows",
		"cooldownPeriod:",
		"datastorage:",
		"url: https://data-storage-service",
		"controller:",
		"leaderElectionId: workflowexecution.kubernaut.ai",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("WE config should contain %q, got:\n%s", want, data)
		}
	}
}

func TestConfigMaps_AllInCorrectNamespace(t *testing.T) {
	kn := testKubernaut()
	type builder struct {
		name string
		fn   func() (*corev1.ConfigMap, error)
	}
	builders := []builder{
		{"gateway", func() (*corev1.ConfigMap, error) { return GatewayConfigMap(kn) }},
		{"datastorage", func() (*corev1.ConfigMap, error) { return DataStorageConfigMap(kn, "db", "user") }},
		{"aianalysis", func() (*corev1.ConfigMap, error) { return AIAnalysisConfigMap(kn) }},
		{"signalprocessing", func() (*corev1.ConfigMap, error) { return SignalProcessingConfigMap(kn) }},
		{"remediationorchestrator", func() (*corev1.ConfigMap, error) { return RemediationOrchestratorConfigMap(kn) }},
		{"workflowexecution", func() (*corev1.ConfigMap, error) { return WorkflowExecutionConfigMap(kn) }},
		{"effectivenessmonitor", func() (*corev1.ConfigMap, error) { return EffectivenessMonitorConfigMap(kn) }},
		{"notification-controller", func() (*corev1.ConfigMap, error) { return NotificationControllerConfigMap(kn) }},
		{"kubernaut-agent", func() (*corev1.ConfigMap, error) { return KubernautAgentConfigMap(kn) }},
		{"authwebhook", func() (*corev1.ConfigMap, error) { return AuthWebhookConfigMap(kn) }},
	}
	for _, b := range builders {
		cm, err := b.fn()
		if err != nil {
			t.Fatalf("building %s ConfigMap: %v", b.name, err)
		}
		if cm.Namespace != testSystemNamespace {
			t.Errorf("ConfigMap %q namespace = %q, want %q", cm.Name, cm.Namespace, testSystemNamespace)
		}
	}
}

func TestAIAnalysisPoliciesConfigMap_DefaultRegoPolicy(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.AIAnalysis.Policy.ConfigMapName = ""

	cm := AIAnalysisPoliciesConfigMap(kn)
	if cm == nil {
		t.Fatal("AIAnalysisPoliciesConfigMap should return non-nil when ConfigMapName is empty")
	}
	if cm.Name != "aianalysis-policies" {
		t.Errorf("Name = %q, want %q", cm.Name, "aianalysis-policies")
	}
	rego, ok := cm.Data["approval.rego"]
	if !ok {
		t.Fatal("ConfigMap should contain approval.rego key")
	}
	if !strings.Contains(rego, "package kubernaut.aianalysis") {
		t.Errorf("approval.rego should contain Rego package declaration, got:\n%s", rego)
	}
}

func TestAIAnalysisPoliciesConfigMap_NilWhenUserProvided(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.AIAnalysis.Policy.ConfigMapName = "user-custom-policies"

	cm := AIAnalysisPoliciesConfigMap(kn)
	if cm != nil {
		t.Error("AIAnalysisPoliciesConfigMap should return nil when user provides ConfigMapName")
	}
}

func TestSignalProcessingPolicyConfigMap_DefaultRegoPolicy(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.SignalProcessing.Policy.ConfigMapName = ""

	cm := SignalProcessingPolicyConfigMap(kn)
	if cm == nil {
		t.Fatal("SignalProcessingPolicyConfigMap should return non-nil when ConfigMapName is empty")
	}
	if cm.Name != "signalprocessing-policy" {
		t.Errorf("Name = %q, want %q", cm.Name, "signalprocessing-policy")
	}
	rego, ok := cm.Data["policy.rego"]
	if !ok {
		t.Fatal("ConfigMap should contain policy.rego key")
	}
	if !strings.Contains(rego, "package kubernaut.signalprocessing") {
		t.Errorf("policy.rego should contain Rego package declaration, got:\n%s", rego)
	}
}

func TestSignalProcessingPolicyConfigMap_NilWhenUserProvided(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.SignalProcessing.Policy.ConfigMapName = "user-sp-policy"

	cm := SignalProcessingPolicyConfigMap(kn)
	if cm != nil {
		t.Error("SignalProcessingPolicyConfigMap should return nil when user provides ConfigMapName")
	}
}

func TestProactiveSignalMappingsConfigMap_DefaultMappings(t *testing.T) {
	kn := testKubernaut()

	cm := ProactiveSignalMappingsConfigMap(kn)
	if cm == nil {
		t.Fatal("ProactiveSignalMappingsConfigMap should return non-nil when no user override")
	}
	if cm.Name != "signalprocessing-proactive-signal-mappings" {
		t.Errorf("Name = %q, want %q", cm.Name, "signalprocessing-proactive-signal-mappings")
	}
	data, ok := cm.Data["proactive-signal-mappings.yaml"]
	if !ok {
		t.Fatal("ConfigMap should contain proactive-signal-mappings.yaml key")
	}
	for _, mapping := range []string{
		"PredictedOOMKill", "OOMKilled",
		"PredictedCPUThrottling", "CPUThrottling",
		"PredictedDiskPressure", "DiskPressure",
		"PredictedNodeNotReady", "NodeNotReady",
	} {
		if !strings.Contains(data, mapping) {
			t.Errorf("proactive-signal-mappings.yaml should contain %q, got:\n%s", mapping, data)
		}
	}
}

func TestProactiveSignalMappingsConfigMap_NilWhenUserProvided(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.SignalProcessing.ProactiveSignalMappings = &kubernautv1alpha1.ConfigMapRef{
		ConfigMapName: "user-proactive-mappings",
	}

	cm := ProactiveSignalMappingsConfigMap(kn)
	if cm != nil {
		t.Error("ProactiveSignalMappingsConfigMap should return nil when user provides ConfigMapName")
	}
}

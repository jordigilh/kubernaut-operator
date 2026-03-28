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
)

func TestGatewayConfigMap_ContainsDataStorageURL(t *testing.T) {
	kn := testKubernaut()
	cm := GatewayConfigMap(kn)

	if cm.Name != "gateway-config" {
		t.Errorf("name = %q, want %q", cm.Name, "gateway-config")
	}
	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "data-storage-service.kubernaut-system.svc.cluster.local") {
		t.Errorf("gateway config should reference DataStorage URL, got:\n%s", data)
	}
}

func TestDataStorageConfigMap_ContainsPgAndValkey(t *testing.T) {
	kn := testKubernaut()
	cm := DataStorageConfigMap(kn)

	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "pg.example.com") {
		t.Errorf("datastorage config should contain PG host, got:\n%s", data)
	}
	if !strings.Contains(data, "valkey.example.com:6379") {
		t.Errorf("datastorage config should contain Valkey addr, got:\n%s", data)
	}
}

func TestDataStorageConfigMap_DefaultPort(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.PostgreSQL.Port = 0
	cm := DataStorageConfigMap(kn)
	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "port: 5432") {
		t.Errorf("datastorage config should default to port 5432, got:\n%s", data)
	}
}

func TestAIAnalysisConfigMap_IncludesConfidenceThreshold(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.AIAnalysis.ConfidenceThreshold = "0.85"
	cm := AIAnalysisConfigMap(kn)

	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "confidenceThreshold: 0.85") {
		t.Errorf("aianalysis config should contain confidence threshold, got:\n%s", data)
	}
}

func TestAIAnalysisConfigMap_OmitsThresholdWhenEmpty(t *testing.T) {
	kn := testKubernaut()
	cm := AIAnalysisConfigMap(kn)

	data := cm.Data["config.yaml"]
	if strings.Contains(data, "confidenceThreshold") {
		t.Errorf("aianalysis config should not contain threshold when empty, got:\n%s", data)
	}
}

func TestSignalProcessingConfigMap_ContainsBothURLs(t *testing.T) {
	kn := testKubernaut()
	cm := SignalProcessingConfigMap(kn)

	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "dataStorageUrl") {
		t.Errorf("signalprocessing config should contain dataStorageUrl, got:\n%s", data)
	}
	if !strings.Contains(data, "gatewayUrl") {
		t.Errorf("signalprocessing config should contain gatewayUrl, got:\n%s", data)
	}
}

func TestRemediationOrchestratorConfigMap_Defaults(t *testing.T) {
	kn := testKubernaut()
	cm := RemediationOrchestratorConfigMap(kn)

	data := cm.Data["config.yaml"]
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

func TestRemediationOrchestratorConfigMap_CustomValues(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.RemediationOrchestrator.Timeouts.Global = "2h"
	kn.Spec.RemediationOrchestrator.Timeouts.Processing = "10m"
	cm := RemediationOrchestratorConfigMap(kn)

	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "global: 2h") {
		t.Errorf("RO config should use custom global timeout, got:\n%s", data)
	}
	if !strings.Contains(data, "processing: 10m") {
		t.Errorf("RO config should use custom processing timeout, got:\n%s", data)
	}
}

func TestWorkflowExecutionConfigMap_DefaultNamespace(t *testing.T) {
	kn := testKubernaut()
	cm := WorkflowExecutionConfigMap(kn)

	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "kubernaut-workflows") {
		t.Errorf("WE config should use default workflow namespace, got:\n%s", data)
	}
}

func TestWorkflowExecutionConfigMap_CustomNamespace(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.WorkflowExecution.WorkflowNamespace = "custom-wf"
	cm := WorkflowExecutionConfigMap(kn)

	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "custom-wf") {
		t.Errorf("WE config should use custom workflow namespace, got:\n%s", data)
	}
}

func TestEffectivenessMonitorConfigMap_Defaults(t *testing.T) {
	kn := testKubernaut()
	cm := EffectivenessMonitorConfigMap(kn)

	data := cm.Data["config.yaml"]
	if !strings.Contains(data, "stabilizationWindow: 30s") {
		t.Errorf("EM config should have default stabilization window, got:\n%s", data)
	}
}

func TestNotificationRoutingConfigMap_SlackConfigured(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.Notification.Slack.SecretName = "slack-webhook"
	kn.Spec.Notification.Slack.Channel = "#ops"
	cm := NotificationRoutingConfigMap(kn)

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
	cm := NotificationRoutingConfigMap(kn)

	data := cm.Data["routing.yaml"]
	if !strings.Contains(data, "console") {
		t.Errorf("routing config without slack should use console receiver, got:\n%s", data)
	}
}

func TestHolmesGPTSDKConfigMap_GeneratedWhenNoExisting(t *testing.T) {
	kn := testKubernaut()
	cm := HolmesGPTSDKConfigMap(kn)

	if cm == nil {
		t.Fatal("HolmesGPTSDKConfigMap should not be nil when no existing CM specified")
	}
	data := cm.Data["sdk-config.yaml"]
	if !strings.Contains(data, "provider: openai") {
		t.Errorf("SDK config should contain provider, got:\n%s", data)
	}
	if !strings.Contains(data, "model: gpt-4o") {
		t.Errorf("SDK config should contain model, got:\n%s", data)
	}
}

func TestHolmesGPTSDKConfigMap_NilWhenExistingProvided(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.HolmesGPTAPI.LLM.SdkConfigMapName = "my-sdk-config"
	cm := HolmesGPTSDKConfigMap(kn)

	if cm != nil {
		t.Error("HolmesGPTSDKConfigMap should be nil when user provides existing CM")
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
			if cm.Annotations["service.beta.openshift.io/inject-cabundle"] != "true" {
				t.Error("EM service-ca ConfigMap should have inject-cabundle annotation")
			}
		}},
		{"holmesgpt-api-service-ca", func(t *testing.T) {
			cm := HolmesGPTAPIServiceCAConfigMap(kn)
			if cm.Annotations["service.beta.openshift.io/inject-cabundle"] != "true" {
				t.Error("HAPI service-ca ConfigMap should have inject-cabundle annotation")
			}
		}},
	}
	for _, tc := range cms {
		t.Run(tc.name, tc.fn)
	}
}

func TestConfigMaps_AllInCorrectNamespace(t *testing.T) {
	kn := testKubernaut()
	cms := []struct{ name, ns string }{
		{GatewayConfigMap(kn).Name, GatewayConfigMap(kn).Namespace},
		{DataStorageConfigMap(kn).Name, DataStorageConfigMap(kn).Namespace},
		{AIAnalysisConfigMap(kn).Name, AIAnalysisConfigMap(kn).Namespace},
		{SignalProcessingConfigMap(kn).Name, SignalProcessingConfigMap(kn).Namespace},
		{RemediationOrchestratorConfigMap(kn).Name, RemediationOrchestratorConfigMap(kn).Namespace},
		{WorkflowExecutionConfigMap(kn).Name, WorkflowExecutionConfigMap(kn).Namespace},
		{EffectivenessMonitorConfigMap(kn).Name, EffectivenessMonitorConfigMap(kn).Namespace},
		{NotificationControllerConfigMap(kn).Name, NotificationControllerConfigMap(kn).Namespace},
		{HolmesGPTAPIConfigMap(kn).Name, HolmesGPTAPIConfigMap(kn).Namespace},
		{AuthWebhookConfigMap(kn).Name, AuthWebhookConfigMap(kn).Namespace},
	}
	for _, cm := range cms {
		if cm.ns != "kubernaut-system" {
			t.Errorf("ConfigMap %q namespace = %q, want %q", cm.name, cm.ns, "kubernaut-system")
		}
	}
}

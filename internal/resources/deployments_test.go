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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

func TestGatewayDeployment_Basic(t *testing.T) {
	kn := testKubernaut()
	dep, err := GatewayDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertDeploymentBasics(t, dep, ComponentGateway, "gateway")
	assertHasVolume(t, dep, "config")
	assertHasVolumeMount(t, dep, "config", "/etc/kubernaut/config.yaml")
}

func TestDataStorageDeployment_InitContainer(t *testing.T) {
	kn := testKubernaut()
	dep, err := DataStorageDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertDeploymentBasics(t, dep, ComponentDataStorage, "datastorage")

	if len(dep.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("DataStorage should have 1 init container, got %d", len(dep.Spec.Template.Spec.InitContainers))
	}
	init := dep.Spec.Template.Spec.InitContainers[0]
	if init.Name != "wait-for-postgres" {
		t.Errorf("init container name = %q, want %q", init.Name, "wait-for-postgres")
	}
	if init.Resources.Requests == nil {
		t.Error("init container should have resource requests")
	}
}

func TestDataStorageDeployment_ProjectedSecretsVolume(t *testing.T) {
	kn := testKubernaut()
	dep, err := DataStorageDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "secrets" && v.Projected != nil {
			found = true
			if len(v.Projected.Sources) != 2 {
				t.Errorf("secrets projected volume should have 2 sources, got %d", len(v.Projected.Sources))
			}
		}
	}
	if !found {
		t.Error("DataStorage should have a 'secrets' projected volume")
	}
}

func TestAIAnalysisDeployment_PolicyVolume(t *testing.T) {
	kn := testKubernaut()
	dep, err := AIAnalysisDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertDeploymentBasics(t, dep, ComponentAIAnalysis, "aianalysis")
	assertHasVolume(t, dep, "rego-policies")
	assertHasVolumeMount(t, dep, "rego-policies", "/etc/aianalysis/policies")
}

func TestSignalProcessingDeployment_PolicyMount(t *testing.T) {
	kn := testKubernaut()
	dep, err := SignalProcessingDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertHasVolume(t, dep, "policy")
	assertHasVolumeMount(t, dep, "policy", "/etc/signalprocessing/policies")
}

func TestSignalProcessingDeployment_ProactiveSignalMappings(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.SignalProcessing.ProactiveSignalMappings = &kubernautv1alpha1.ConfigMapRef{ConfigMapName: "my-mappings"}
	dep, err := SignalProcessingDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertHasVolume(t, dep, "proactive-mappings")
	assertHasVolumeMount(t, dep, "proactive-mappings", "/etc/signalprocessing/proactive-signal-mappings.yaml")
}

func TestSignalProcessingDeployment_NoProactiveSignalMappings(t *testing.T) {
	kn := testKubernaut()
	dep, err := SignalProcessingDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "proactive-mappings" {
			t.Error("SignalProcessing should not have proactive-mappings volume when not configured")
		}
	}
}

func TestNotificationDeployment_SlackCredentialsVolume(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.Notification.Slack.SecretName = "slack-secret"
	dep, err := NotificationDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertHasVolume(t, dep, "credentials")
	assertHasVolumeMount(t, dep, "credentials", "/etc/notification/credentials")
}

func TestNotificationDeployment_NoSlack_EmptyDirCredentials(t *testing.T) {
	kn := testKubernaut()
	dep, err := NotificationDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "credentials" {
			if v.EmptyDir == nil {
				t.Error("Notification credentials volume should be an emptyDir when Slack is not configured")
			}
			return
		}
	}
	t.Error("Notification should still have a credentials volume (emptyDir) even without Slack")
}

func TestNotificationDeployment_OutputEmptyDir(t *testing.T) {
	kn := testKubernaut()
	dep, err := NotificationDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertHasVolume(t, dep, "notification-output")
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "notification-output" {
			if v.EmptyDir == nil {
				t.Error("notification-output volume should be an emptyDir")
			}
		}
	}
}

func TestHolmesGPTAPIDeployment_LLMCredentials(t *testing.T) {
	kn := testKubernaut()
	dep, err := HolmesGPTAPIDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertDeploymentBasics(t, dep, ComponentHolmesGPTAPI, "holmesgpt-api")
	assertHasVolume(t, dep, "llm-credentials")
	assertHasVolumeMount(t, dep, "llm-credentials", "/etc/holmesgpt/credentials")
}

func TestHolmesGPTAPIDeployment_ServiceCA_WhenMonitoringEnabled(t *testing.T) {
	kn := testKubernaut()
	dep, err := HolmesGPTAPIDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertHasVolume(t, dep, "service-ca")
	assertHasVolumeMount(t, dep, "service-ca", "/etc/ssl/hapi")
}

func TestHolmesGPTAPIDeployment_IsOpenShiftEnv_WhenMonitoringEnabled(t *testing.T) {
	kn := testKubernaut()
	dep, err := HolmesGPTAPIDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	container := dep.Spec.Template.Spec.Containers[0]
	found := false
	for _, env := range container.Env {
		if env.Name == "IS_OPENSHIFT" && env.Value == "True" {
			found = true
		}
	}
	if !found {
		t.Error("HAPI container should have IS_OPENSHIFT=True env var when monitoring is enabled")
	}
}

func TestHolmesGPTAPIDeployment_NoIsOpenShiftEnv_WhenMonitoringDisabled(t *testing.T) {
	kn := testKubernaut()
	disabled := false
	kn.Spec.Monitoring.Enabled = &disabled
	dep, err := HolmesGPTAPIDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	container := dep.Spec.Template.Spec.Containers[0]
	for _, env := range container.Env {
		if env.Name == "IS_OPENSHIFT" {
			t.Error("HAPI container should not have IS_OPENSHIFT env var when monitoring is disabled")
		}
	}
}

func TestHolmesGPTAPIDeployment_NoServiceCA_WhenMonitoringDisabled(t *testing.T) {
	kn := testKubernaut()
	disabled := false
	kn.Spec.Monitoring.Enabled = &disabled
	dep, err := HolmesGPTAPIDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "service-ca" {
			t.Error("HAPI should not have service-ca volume when monitoring is disabled")
		}
	}
}

func TestEffectivenessMonitorDeployment_ServiceCA(t *testing.T) {
	kn := testKubernaut()
	dep, err := EffectivenessMonitorDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertHasVolume(t, dep, "service-ca")
}

func TestAuthWebhookDeployment_TLSAndPort(t *testing.T) {
	kn := testKubernaut()
	dep, err := AuthWebhookDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertDeploymentBasics(t, dep, ComponentAuthWebhook, "authwebhook")
	assertHasVolume(t, dep, "webhook-certs")
	assertHasVolumeMount(t, dep, "webhook-certs", "/tmp/k8s-webhook-server/serving-certs")

	container := dep.Spec.Template.Spec.Containers[0]
	found := false
	for _, p := range container.Ports {
		if p.ContainerPort == 9443 && p.Name == "webhook" {
			found = true
		}
	}
	if !found {
		t.Error("AuthWebhook should expose port 9443 named 'webhook'")
	}
}

func TestAllDeployments_SecurityContexts(t *testing.T) {
	kn := testKubernaut()
	allDeps := getAllDeployments(t, kn)
	for _, dep := range allDeps {
		psc := dep.Spec.Template.Spec.SecurityContext
		if psc == nil {
			t.Errorf("Deployment %q should have pod security context", dep.Name)
			continue
		}
		if psc.RunAsNonRoot == nil || !*psc.RunAsNonRoot {
			t.Errorf("Deployment %q should have RunAsNonRoot=true", dep.Name)
		}

		for _, c := range dep.Spec.Template.Spec.Containers {
			if c.SecurityContext == nil {
				t.Errorf("Deployment %q container %q should have security context", dep.Name, c.Name)
				continue
			}
			if c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
				t.Errorf("Deployment %q container %q should have AllowPrivilegeEscalation=false", dep.Name, c.Name)
			}
		}
	}
}

func TestAllDeployments_ImagePullPolicy(t *testing.T) {
	kn := testKubernaut()
	for _, dep := range getAllDeployments(t, kn) {
		for _, c := range dep.Spec.Template.Spec.Containers {
			if c.ImagePullPolicy != corev1.PullIfNotPresent {
				t.Errorf("Deployment %q container %q pullPolicy = %q, want %q",
					dep.Name, c.Name, c.ImagePullPolicy, corev1.PullIfNotPresent)
			}
		}
	}
}

func TestAllDeployments_ServiceAccounts(t *testing.T) {
	kn := testKubernaut()
	for _, dep := range getAllDeployments(t, kn) {
		if dep.Spec.Template.Spec.ServiceAccountName == "" {
			t.Errorf("Deployment %q should have a service account", dep.Name)
		}
	}
}

func TestServiceAccountNaming(t *testing.T) {
	expected := map[string]string{
		ComponentGateway:                 "gateway",
		ComponentDataStorage:             "data-storage-sa",
		ComponentAIAnalysis:              "aianalysis-controller",
		ComponentSignalProcessing:        "signalprocessing-controller",
		ComponentRemediationOrchestrator: "remediationorchestrator-controller",
		ComponentWorkflowExecution:       "workflowexecution-controller",
		ComponentEffectivenessMonitor:    "effectivenessmonitor-controller",
		ComponentNotification:            "notification-controller",
		ComponentHolmesGPTAPI:            "holmesgpt-api-sa",
		ComponentAuthWebhook:             "authwebhook",
	}
	for component, wantName := range expected {
		got := ServiceAccountName(component)
		if got != wantName {
			t.Errorf("ServiceAccountName(%q) = %q, want %q", component, got, wantName)
		}
	}
}

func TestServiceAccountNaming_AllComponentsCovered(t *testing.T) {
	for _, component := range AllComponents() {
		name := ServiceAccountName(component)
		if name == "" {
			t.Errorf("ServiceAccountName(%q) returned empty string", component)
		}
	}
}

// --- helpers ---

func getAllDeployments(t *testing.T, kn *kubernautv1alpha1.Kubernaut) []*appsv1.Deployment {
	t.Helper()
	type builder func(*kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error)
	builders := []builder{
		GatewayDeployment,
		DataStorageDeployment,
		AIAnalysisDeployment,
		SignalProcessingDeployment,
		RemediationOrchestratorDeployment,
		WorkflowExecutionDeployment,
		EffectivenessMonitorDeployment,
		NotificationDeployment,
		HolmesGPTAPIDeployment,
		AuthWebhookDeployment,
	}
	var deps []*appsv1.Deployment
	for _, b := range builders {
		dep, err := b(kn)
		if err != nil {
			t.Fatalf("unexpected error building deployment: %v", err)
		}
		deps = append(deps, dep)
	}
	return deps
}

func assertDeploymentBasics(t *testing.T, dep *appsv1.Deployment, component, imageSuffix string) {
	t.Helper()

	if dep.Namespace != "kubernaut-system" {
		t.Errorf("Deployment %q namespace = %q, want %q", dep.Name, dep.Namespace, "kubernaut-system")
	}
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 1 {
		t.Errorf("Deployment %q should have 1 replica", dep.Name)
	}
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		t.Fatalf("Deployment %q should have at least 1 container", dep.Name)
	}

	container := dep.Spec.Template.Spec.Containers[0]
	wantImage := "quay.io/kubernaut-ai/" + imageSuffix + ":v1.3.0"
	if container.Image != wantImage {
		t.Errorf("Deployment %q image = %q, want %q", dep.Name, container.Image, wantImage)
	}
}

func assertHasVolume(t *testing.T, dep *appsv1.Deployment, name string) {
	t.Helper()
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == name {
			return
		}
	}
	t.Errorf("Deployment %q should have volume %q", dep.Name, name)
}

func assertHasVolumeMount(t *testing.T, dep *appsv1.Deployment, name, mountPath string) {
	t.Helper()
	container := dep.Spec.Template.Spec.Containers[0]
	for _, vm := range container.VolumeMounts {
		if vm.Name == name {
			if vm.MountPath != mountPath {
				t.Errorf("Deployment %q volume mount %q path = %q, want %q",
					dep.Name, name, vm.MountPath, mountPath)
			}
			return
		}
	}
	t.Errorf("Deployment %q container should have volume mount %q", dep.Name, name)
}

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
	assertVolumeSourceConfigMap(t, dep, "config", "gateway-config")
	assertHasVolumeMount(t, dep, "config", "/etc/gateway")
}

func TestGatewayDeployment_CORSEnvVar(t *testing.T) {
	kn := testKubernaut()
	dep, err := GatewayDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	container := dep.Spec.Template.Spec.Containers[0]
	found := false
	for _, env := range container.Env {
		if env.Name == "CORS_ALLOWED_ORIGINS" {
			found = true
			if env.Value != "https://no-browser-clients.invalid" {
				t.Errorf("CORS_ALLOWED_ORIGINS = %q, want default deny-all origin", env.Value)
			}
		}
	}
	if !found {
		t.Error("Gateway deployment should have CORS_ALLOWED_ORIGINS env var")
	}
}

func TestGatewayDeployment_CustomCORS(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.Gateway.Config.CORSAllowedOrigins = "https://my-dashboard.example.com"
	dep, err := GatewayDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	container := dep.Spec.Template.Spec.Containers[0]
	for _, env := range container.Env {
		if env.Name == "CORS_ALLOWED_ORIGINS" {
			if env.Value != "https://my-dashboard.example.com" {
				t.Errorf("CORS_ALLOWED_ORIGINS = %q, want custom value", env.Value)
			}
			return
		}
	}
	t.Error("CORS_ALLOWED_ORIGINS env var not found")
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
	assertVolumeSourceConfigMap(t, dep, "rego-policies", "aianalysis-policies")
	assertHasVolumeMount(t, dep, "rego-policies", "/etc/aianalysis/policies")
}

func TestSignalProcessingDeployment_PolicyMount(t *testing.T) {
	kn := testKubernaut()
	dep, err := SignalProcessingDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertHasVolume(t, dep, "policy")
	assertVolumeSourceConfigMap(t, dep, "policy", "signalprocessing-policy")
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

func TestNotificationDeployment_RoutingConfigMount(t *testing.T) {
	kn := testKubernaut()
	dep, err := NotificationDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertHasVolume(t, dep, "routing-config")
	assertVolumeSourceConfigMap(t, dep, "routing-config", "notification-routing-config")
	assertHasVolumeMount(t, dep, "routing-config", "/etc/notification-routing")
}

func TestKubernautAgentDeployment_LLMCredentials(t *testing.T) {
	kn := testKubernaut()
	dep, err := KubernautAgentDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertDeploymentBasics(t, dep, ComponentKubernautAgent, "kubernaut-agent")
	assertHasVolume(t, dep, "llm-credentials")
	assertHasVolumeMount(t, dep, "llm-credentials", "/etc/kubernaut-agent/credentials")
}

func TestKubernautAgentDeployment_ConfigArgs(t *testing.T) {
	kn := testKubernaut()
	dep, err := KubernautAgentDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	container := dep.Spec.Template.Spec.Containers[0]
	want := []string{
		"-config", "/etc/kubernaut-agent/config.yaml",
		"-sdk-config", "/etc/kubernaut-agent/sdk/sdk-config.yaml",
	}
	if len(container.Args) != len(want) {
		t.Fatalf("KA args len = %d, want %d (%v)", len(container.Args), len(want), container.Args)
	}
	for i := range want {
		if container.Args[i] != want[i] {
			t.Errorf("KA args[%d] = %q, want %q", i, container.Args[i], want[i])
		}
	}
}

func TestKubernautAgentDeployment_ServiceCA_WhenMonitoringEnabled(t *testing.T) {
	kn := testKubernaut()
	dep, err := KubernautAgentDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	assertHasVolume(t, dep, "service-ca")
	assertHasVolumeMount(t, dep, "service-ca", "/etc/ssl/ka")
}

func TestKubernautAgentDeployment_IsOpenShiftEnv_WhenMonitoringEnabled(t *testing.T) {
	kn := testKubernaut()
	dep, err := KubernautAgentDeployment(kn)
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
		t.Error("KA container should have IS_OPENSHIFT=True env var when monitoring is enabled")
	}
}

func TestKubernautAgentDeployment_NoIsOpenShiftEnv_WhenMonitoringDisabled(t *testing.T) {
	kn := testKubernaut()
	disabled := false
	kn.Spec.Monitoring.Enabled = &disabled
	dep, err := KubernautAgentDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	container := dep.Spec.Template.Spec.Containers[0]
	for _, env := range container.Env {
		if env.Name == "IS_OPENSHIFT" {
			t.Error("KA container should not have IS_OPENSHIFT env var when monitoring is disabled")
		}
	}
}

func TestKubernautAgentDeployment_NoServiceCA_WhenMonitoringDisabled(t *testing.T) {
	kn := testKubernaut()
	disabled := false
	kn.Spec.Monitoring.Enabled = &disabled
	dep, err := KubernautAgentDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "service-ca" {
			t.Error("KA should not have service-ca volume when monitoring is disabled")
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

func TestEffectivenessMonitorDeployment_WaitForServiceCA_InitContainer(t *testing.T) {
	kn := testKubernaut()
	dep, err := EffectivenessMonitorDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	if len(dep.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("EM should have 1 init container when monitoring enabled, got %d", len(dep.Spec.Template.Spec.InitContainers))
	}
	init := dep.Spec.Template.Spec.InitContainers[0]
	if init.Name != "wait-for-service-ca" {
		t.Errorf("init container name = %q, want %q", init.Name, "wait-for-service-ca")
	}
	if init.Resources.Requests == nil {
		t.Error("init container should have resource requests")
	}

	hasMount := false
	for _, vm := range init.VolumeMounts {
		if vm.Name == "service-ca" && vm.MountPath == "/etc/ssl/em" {
			hasMount = true
		}
	}
	if !hasMount {
		t.Error("init container should mount service-ca at /etc/ssl/em")
	}
}

func TestEffectivenessMonitorDeployment_NoInitContainer_MonitoringDisabled(t *testing.T) {
	kn := testKubernaut()
	disabled := false
	kn.Spec.Monitoring.Enabled = &disabled
	dep, err := EffectivenessMonitorDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	if len(dep.Spec.Template.Spec.InitContainers) != 0 {
		t.Errorf("EM should have 0 init containers when monitoring disabled, got %d", len(dep.Spec.Template.Spec.InitContainers))
	}
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

func TestAuthWebhookDeployment_RecreateStrategy(t *testing.T) {
	kn := testKubernaut()
	dep, err := AuthWebhookDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}

	if dep.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("AuthWebhook strategy = %q, want Recreate", dep.Spec.Strategy.Type)
	}
}

func TestAllDeployments_HTTPGetProbes(t *testing.T) {
	kn := testKubernaut()
	deps := getAllDeployments(t, kn)

	for _, dep := range deps {
		container := dep.Spec.Template.Spec.Containers[0]
		component := dep.Spec.Template.ObjectMeta.Labels["app"]

		if container.LivenessProbe == nil {
			t.Fatalf("Deployment %q should have liveness probe", dep.Name)
		}
		if container.ReadinessProbe == nil {
			t.Fatalf("Deployment %q should have readiness probe", dep.Name)
		}

		if container.LivenessProbe.HTTPGet == nil {
			t.Errorf("Deployment %q liveness probe should use HTTPGet (not TCPSocket)", dep.Name)
		}
		if container.ReadinessProbe.HTTPGet == nil {
			t.Errorf("Deployment %q readiness probe should use HTTPGet (not TCPSocket)", dep.Name)
		}

		pc := probeConfigForComponent(component)
		lp := container.LivenessProbe
		rp := container.ReadinessProbe

		if lp.HTTPGet != nil && lp.HTTPGet.Path != pc.LivenessPath {
			t.Errorf("Deployment %q liveness path = %q, want %q", dep.Name, lp.HTTPGet.Path, pc.LivenessPath)
		}
		if rp.HTTPGet != nil && rp.HTTPGet.Path != pc.ReadinessPath {
			t.Errorf("Deployment %q readiness path = %q, want %q", dep.Name, rp.HTTPGet.Path, pc.ReadinessPath)
		}
	}
}

func TestAllDeployments_ProbeTimingMatchesHelmChart(t *testing.T) {
	kn := testKubernaut()
	deps := getAllDeployments(t, kn)

	for _, dep := range deps {
		container := dep.Spec.Template.Spec.Containers[0]
		component := dep.Spec.Template.ObjectMeta.Labels["app"]
		pc := probeConfigForComponent(component)

		lp := container.LivenessProbe
		rp := container.ReadinessProbe

		if lp.InitialDelaySeconds != pc.LivenessInitialDelay {
			t.Errorf("%s liveness InitialDelaySeconds = %d, want %d", dep.Name, lp.InitialDelaySeconds, pc.LivenessInitialDelay)
		}
		if lp.PeriodSeconds != pc.LivenessPeriod {
			t.Errorf("%s liveness PeriodSeconds = %d, want %d", dep.Name, lp.PeriodSeconds, pc.LivenessPeriod)
		}
		if lp.TimeoutSeconds != pc.LivenessTimeout {
			t.Errorf("%s liveness TimeoutSeconds = %d, want %d", dep.Name, lp.TimeoutSeconds, pc.LivenessTimeout)
		}
		if lp.FailureThreshold != pc.LivenessFailureThreshold {
			t.Errorf("%s liveness FailureThreshold = %d, want %d", dep.Name, lp.FailureThreshold, pc.LivenessFailureThreshold)
		}

		if rp.InitialDelaySeconds != pc.ReadinessInitialDelay {
			t.Errorf("%s readiness InitialDelaySeconds = %d, want %d", dep.Name, rp.InitialDelaySeconds, pc.ReadinessInitialDelay)
		}
		if rp.PeriodSeconds != pc.ReadinessPeriod {
			t.Errorf("%s readiness PeriodSeconds = %d, want %d", dep.Name, rp.PeriodSeconds, pc.ReadinessPeriod)
		}
		if rp.TimeoutSeconds != pc.ReadinessTimeout {
			t.Errorf("%s readiness TimeoutSeconds = %d, want %d", dep.Name, rp.TimeoutSeconds, pc.ReadinessTimeout)
		}
		if rp.FailureThreshold != pc.ReadinessFailureThreshold {
			t.Errorf("%s readiness FailureThreshold = %d, want %d", dep.Name, rp.FailureThreshold, pc.ReadinessFailureThreshold)
		}
	}
}

func TestDeployments_MetricsPort(t *testing.T) {
	kn := testKubernaut()
	withMetrics := map[string]bool{
		ComponentDataStorage:             true,
		ComponentAIAnalysis:              true,
		ComponentSignalProcessing:        true,
		ComponentRemediationOrchestrator: true,
		ComponentWorkflowExecution:       true,
		ComponentEffectivenessMonitor:    true,
		ComponentNotification:            true,
	}

	for _, dep := range getAllDeployments(t, kn) {
		component := dep.Spec.Template.ObjectMeta.Labels["app"]
		container := dep.Spec.Template.Spec.Containers[0]

		hasMetrics := false
		for _, p := range container.Ports {
			if p.ContainerPort == PortMetrics && p.Name == "metrics" {
				hasMetrics = true
			}
		}

		if withMetrics[component] && !hasMetrics {
			t.Errorf("Deployment %q should expose metrics port 9090", dep.Name)
		}
		if !withMetrics[component] && hasMetrics {
			t.Errorf("Deployment %q should NOT expose metrics port 9090", dep.Name)
		}
	}
}

func TestDeployments_ConfigArgs(t *testing.T) {
	kn := testKubernaut()

	wantArgs := map[string][]string{
		ComponentGateway:                 {"--config=/etc/gateway/config.yaml"},
		ComponentAIAnalysis:              {"-config", "/etc/aianalysis/config.yaml"},
		ComponentSignalProcessing:        {"--config=/etc/signalprocessing/config.yaml"},
		ComponentRemediationOrchestrator: {"--config=/etc/config/remediationorchestrator.yaml"},
		ComponentWorkflowExecution:       {"--config=/etc/config/workflowexecution.yaml"},
		ComponentEffectivenessMonitor:    {"--config=/etc/effectivenessmonitor/effectivenessmonitor.yaml"},
		ComponentNotification:            {"-config", "/etc/notification/config.yaml"},
		ComponentKubernautAgent:          {"-config", "/etc/kubernaut-agent/config.yaml", "-sdk-config", "/etc/kubernaut-agent/sdk/sdk-config.yaml"},
		ComponentAuthWebhook:             {"-config=/etc/authwebhook/authwebhook.yaml"},
	}

	for _, dep := range getAllDeployments(t, kn) {
		component := dep.Spec.Template.ObjectMeta.Labels["app"]
		container := dep.Spec.Template.Spec.Containers[0]

		want, hasExpected := wantArgs[component]
		if !hasExpected {
			if len(container.Args) != 0 {
				t.Errorf("Deployment %q should have no args, got %v", dep.Name, container.Args)
			}
			continue
		}

		if len(container.Args) != len(want) {
			t.Errorf("Deployment %q args len = %d, want %d (%v vs %v)", dep.Name, len(container.Args), len(want), container.Args, want)
			continue
		}
		for i := range want {
			if container.Args[i] != want[i] {
				t.Errorf("Deployment %q args[%d] = %q, want %q", dep.Name, i, container.Args[i], want[i])
			}
		}
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
		component := dep.Spec.Template.ObjectMeta.Labels["app"]
		if component == "" {
			t.Fatalf("Deployment %q missing 'app' label on pod template", dep.Name)
		}
		wantSA := ServiceAccountName(component)
		gotSA := dep.Spec.Template.Spec.ServiceAccountName
		if gotSA != wantSA {
			t.Errorf("Deployment %q SA = %q, want %q (component %q)", dep.Name, gotSA, wantSA, component)
		}
	}
}

func TestAllDeployments_HaveInterServiceTLSCA(t *testing.T) {
	kn := testKubernaut()
	deps := getAllDeployments(t, kn)

	for _, dep := range deps {
		name := dep.Name
		container := dep.Spec.Template.Spec.Containers[0]

		hasCAVolume := false
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "tls-ca" && v.ConfigMap != nil && v.ConfigMap.Name == InterServiceCAConfigMapName {
				hasCAVolume = true
			}
		}
		if !hasCAVolume {
			t.Errorf("Deployment %q missing tls-ca volume backed by %q ConfigMap", name, InterServiceCAConfigMapName)
		}

		hasCAMount := false
		for _, vm := range container.VolumeMounts {
			if vm.Name == "tls-ca" && vm.MountPath == "/etc/tls-ca" {
				hasCAMount = true
			}
		}
		if !hasCAMount {
			t.Errorf("Deployment %q missing tls-ca volume mount at /etc/tls-ca", name)
		}

		hasCAEnv := false
		for _, env := range container.Env {
			if env.Name == "TLS_CA_FILE" && env.Value == InterServiceTLSCAFile {
				hasCAEnv = true
			}
		}
		if !hasCAEnv {
			t.Errorf("Deployment %q missing TLS_CA_FILE env var", name)
		}
	}
}

func TestGatewayDeployment_HasTLSCertVolume(t *testing.T) {
	kn := testKubernaut()
	dep, err := GatewayDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}
	assertHasVolume(t, dep, "tls-certs")
	assertHasVolumeMount(t, dep, "tls-certs", InterServiceTLSCertDir)
}

func TestDataStorageDeployment_HasTLSCertVolume(t *testing.T) {
	kn := testKubernaut()
	dep, err := DataStorageDeployment(kn)
	if err != nil {
		t.Fatal(err)
	}
	assertHasVolume(t, dep, "tls-certs")
	assertHasVolumeMount(t, dep, "tls-certs", InterServiceTLSCertDir)
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
		ComponentKubernautAgent:          "kubernaut-agent-sa",
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
		KubernautAgentDeployment,
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

func assertVolumeSourceConfigMap(t *testing.T, dep *appsv1.Deployment, volumeName, expectedCMName string) {
	t.Helper()
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == volumeName {
			if v.ConfigMap == nil {
				t.Errorf("Deployment %q volume %q should be backed by a ConfigMap", dep.Name, volumeName)
				return
			}
			if v.ConfigMap.Name != expectedCMName {
				t.Errorf("Deployment %q volume %q ConfigMap = %q, want %q",
					dep.Name, volumeName, v.ConfigMap.Name, expectedCMName)
			}
			return
		}
	}
	t.Errorf("Deployment %q should have volume %q", dep.Name, volumeName)
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

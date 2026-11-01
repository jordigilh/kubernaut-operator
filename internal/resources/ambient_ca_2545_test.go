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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Ambient CA deployment contract (#2545)", func() {
	It("UT-TLS-2545-OP-001 [AC-6, CM-7; SOC2 CC7.2; ASVS V15.2.5]: does not declare legacy ambient env vars or ambient-only writable volumes", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		builders := []struct {
			name  string
			build func() (*appsv1.Deployment, error)
		}{
			{name: "gateway", build: func() (*appsv1.Deployment, error) { return GatewayDeployment(kn, knV2) }},
			{name: "datastorage", build: func() (*appsv1.Deployment, error) { return DataStorageDeployment(kn) }},
			{name: "aianalysis", build: func() (*appsv1.Deployment, error) { return AIAnalysisDeployment(kn, knV2) }},
			{name: "signalprocessing", build: func() (*appsv1.Deployment, error) { return SignalProcessingDeployment(kn, knV2) }},
			{name: "remediationorchestrator", build: func() (*appsv1.Deployment, error) { return RemediationOrchestratorDeployment(kn, knV2) }},
			{name: "workflowexecution", build: func() (*appsv1.Deployment, error) { return WorkflowExecutionDeployment(kn, knV2) }},
			{name: "effectivenessmonitor", build: func() (*appsv1.Deployment, error) { return EffectivenessMonitorDeployment(kn, knV2) }},
			{name: "notification", build: func() (*appsv1.Deployment, error) { return NotificationDeployment(kn, knV2) }},
			{name: "authwebhook", build: func() (*appsv1.Deployment, error) { return AuthWebhookDeployment(kn, knV2) }},
			{name: "kubernautagent", build: func() (*appsv1.Deployment, error) { return KubernautAgentDeployment(kn, knV2) }},
			{name: "apifrontend", build: func() (*appsv1.Deployment, error) { return APIFrontendDeployment(kn, knV2) }},
		}

		for _, entry := range builders {
			dep, err := entry.build()
			Expect(err).NotTo(HaveOccurred(), entry.name)

			for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
				Expect([]string{"SSL_CERT_FILE", "SSL_CERT_DIR", "TLS_CA_FILE"}).NotTo(ContainElement(env.Name), entry.name)
			}
			for _, volume := range dep.Spec.Template.Spec.Volumes {
				Expect([]string{"ambient-tmp", "combined-ca"}).NotTo(ContainElement(volume.Name), entry.name)
			}
			for _, mount := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
				Expect(mount.MountPath).NotTo(Equal("/var/run/kubernaut/ambient-tmp"), entry.name)
			}
		}
	})

	It("UT-TLS-2545-OP-002 [SC-8, SC-13; SOC2 CC7.2; ASVS V12.3.1]: renders runtime tlsCaFile for every shared-TLS bootstrap service", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		builders := []struct {
			name  string
			key   string
			build func() (*corev1.ConfigMap, error)
		}{
			{name: "aianalysis", key: "config.yaml", build: func() (*corev1.ConfigMap, error) { return AIAnalysisConfigMap(kn, knV2) }},
			{name: "signalprocessing", key: "config.yaml", build: func() (*corev1.ConfigMap, error) { return SignalProcessingConfigMap(kn, knV2) }},
			{name: "remediationorchestrator", key: "remediationorchestrator.yaml", build: func() (*corev1.ConfigMap, error) { return RemediationOrchestratorConfigMap(kn, knV2) }},
			{name: "workflowexecution", key: "workflowexecution.yaml", build: func() (*corev1.ConfigMap, error) { return WorkflowExecutionConfigMap(kn, knV2) }},
			{name: "notification", key: "config.yaml", build: func() (*corev1.ConfigMap, error) { return NotificationControllerConfigMap(kn, knV2) }},
			{name: "authwebhook", key: "authwebhook.yaml", build: func() (*corev1.ConfigMap, error) { return AuthWebhookConfigMap(kn, knV2) }},
		}

		for _, entry := range builders {
			configMap, err := entry.build()
			Expect(err).NotTo(HaveOccurred(), entry.name)
			var config struct {
				TLSCAFile string `yaml:"tlsCaFile"`
			}
			Expect(yaml.Unmarshal([]byte(configMap.Data[entry.key]), &config)).To(Succeed(), entry.name)
			Expect(config.TLSCAFile).To(Equal(InterServiceTLSCAFileFor(knV2)), entry.name)
		}
	})
})

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

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
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

	// #571: three locations still render legacy ambient-CA shapes because
	// their elimination is blocked on upstream runtime changes (kubernaut
	// #2548; the pinned oauth2-proxy image). They are retained under the
	// documented threat-model exception register in
	// docs/security/ISSUE-571-AMBIENT-CA-EXCEPTIONS.md and must hold the
	// exception shape the register allows: init-isolated assembly,
	// memory-backed staging, read-only exposure to the long-running
	// container.
	Context("documented ambient CA exceptions (#571)", func() {
		const testProviderLogoutURL = "https://idp.example.com/logout?id_token_hint={id_token}"

		findVolume := func(dep *appsv1.Deployment, name string) *corev1.Volume {
			for i := range dep.Spec.Template.Spec.Volumes {
				if dep.Spec.Template.Spec.Volumes[i].Name == name {
					return &dep.Spec.Template.Spec.Volumes[i]
				}
			}
			return nil
		}

		isInitOnly := func(dep *appsv1.Deployment, name string) bool {
			for _, c := range dep.Spec.Template.Spec.Containers {
				if c.Name == name {
					return false
				}
			}
			for _, c := range dep.Spec.Template.Spec.InitContainers {
				if c.Name == name {
					return true
				}
			}
			return false
		}

		It("UT-TLS-571-001 [AC-6, CM-7; SOC2 CC7.2; ASVS V15.2.5]: WorkflowExecution AAP combined-ca holds the documented exception shape", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			kn.Spec.WorkflowExecution.Ansible.CACertSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn, knV2)
			Expect(err).NotTo(HaveOccurred())

			combined := findVolume(dep, testVolumeCombinedCA)
			Expect(combined).NotTo(BeNil(), "the AAP CA exception renders the combined-ca volume")
			Expect(combined.EmptyDir).NotTo(BeNil())
			Expect(combined.EmptyDir.Medium).To(Equal(corev1.StorageMediumMemory),
				"the combined-ca staging volume must be memory-backed so a compromised process gains no disk-backed persistence location")
			Expect(combined.EmptyDir.SizeLimit).To(Equal(ambientCAStagingSizeLimit),
				"the combined-ca staging volume must be bounded")
			Expect(isInitOnly(dep, "build-ca-bundle")).To(BeTrue(),
				"CA assembly must run only in the init container, never in the long-running container")
			for _, c := range dep.Spec.Template.Spec.Containers {
				for _, env := range c.Env {
					Expect([]string{"SSL_CERT_FILE", "SSL_CERT_DIR"}).NotTo(ContainElement(env.Name),
						c.Name+" must not statically declare ambient trust env; tlsCaFile flows through the runtime adapter")
				}
				for _, m := range c.VolumeMounts {
					if m.Name == "combined-ca" {
						Expect(m.ReadOnly).To(BeTrue(),
							"the long-running container must only read the assembled bundle")
					}
				}
			}
		})

		It("UT-TLS-571-002 [AC-6, CM-7; SOC2 CC7.2; ASVS V15.2.5]: Console logout combined-ca holds the documented exception shape", func() {
			kn := testKubernautWithConsole()
			kn.Spec.Console.Auth.ProviderLogoutURL = testProviderLogoutURL
			dep, err := ConsoleDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			combined := findVolume(dep, testVolumeCombinedCA)
			Expect(combined).NotTo(BeNil(), "the oauth2-proxy backend-logout exception renders the combined-ca volume")
			Expect(combined.EmptyDir).NotTo(BeNil())
			Expect(combined.EmptyDir.Medium).To(Equal(corev1.StorageMediumMemory),
				"the combined-ca staging volume must be memory-backed so a compromised process gains no disk-backed persistence location")
			Expect(combined.EmptyDir.SizeLimit).To(Equal(ambientCAStagingSizeLimit),
				"the combined-ca staging volume must be bounded")
			Expect(isInitOnly(dep, "build-ca-bundle")).To(BeTrue(),
				"CA assembly must run only in the init container, never in the long-running console containers")
			for _, c := range dep.Spec.Template.Spec.Containers {
				for _, m := range c.VolumeMounts {
					if m.Name == "combined-ca" {
						Expect(m.ReadOnly).To(BeTrue(),
							"the long-running container must only read the assembled bundle")
					}
				}
			}
		})

		It("UT-TLS-571-003 [AC-6, CM-7; SOC2 CC7.2; ASVS V15.2.5]: FleetMetadataCache ambient trust is the documented read-only SSL_CERT_FILE exception", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			dep, err := FleetMetadataCacheDeployment(kn, knV2)
			Expect(err).NotTo(HaveOccurred())

			// Upstream FMC never calls sharedtls.InjectAmbientCACerts, so a
			// static SSL_CERT_FILE pointing at the read-only inter-service
			// CA file is the only private-CA trust path (exception register).
			var sslCertFile *corev1.EnvVar
			for _, c := range dep.Spec.Template.Spec.Containers {
				for i := range c.Env {
					if c.Env[i].Name == "SSL_CERT_FILE" {
						sslCertFile = &c.Env[i]
					}
				}
			}
			Expect(sslCertFile).NotTo(BeNil(), "FMC's private-CA trust requires the documented SSL_CERT_FILE exception")
			Expect(sslCertFile.Value).To(Equal(InterServiceTLSCAFileFor(knV2)),
				"the exception must point at the read-only mounted inter-service CA file, not an assembled bundle")
			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.EmptyDir).To(BeNil(),
					v.Name+" must not be a writable volume: the FMC exception is read-only file trust with no CA assembly")
			}
		})

		It("UT-TLS-571-004 [AC-6, CM-7; SOC2 CC7.2; ASVS V15.2.5]: no operand renders a disk-backed combined-ca volume (negative)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knConsole := testKubernautWithConsole()
			knConsole.Spec.Console.Auth.ProviderLogoutURL = testProviderLogoutURL
			builders := []struct {
				name  string
				build func() (*appsv1.Deployment, error)
			}{
				{name: "workflowexecution+aap-ca", build: func() (*appsv1.Deployment, error) {
					k := testKubernaut()
					k.Spec.WorkflowExecution.Ansible.CACertSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "aap-ca-secret"}
					return WorkflowExecutionDeployment(k, testKnV2(k))
				}},
				{name: "console+logout", build: func() (*appsv1.Deployment, error) { return ConsoleDeployment(knConsole) }},
				{name: "fleetmetadatacache", build: func() (*appsv1.Deployment, error) { return FleetMetadataCacheDeployment(kn, knV2) }},
			}
			for _, entry := range builders {
				dep, err := entry.build()
				Expect(err).NotTo(HaveOccurred(), entry.name)
				for i := range dep.Spec.Template.Spec.Volumes {
					v := dep.Spec.Template.Spec.Volumes[i]
					if v.Name == testVolumeCombinedCA {
						Expect(v.EmptyDir).NotTo(BeNil(), entry.name)
						Expect(v.EmptyDir.Medium).To(Equal(corev1.StorageMediumMemory), entry.name,
							"any retained combined-ca staging volume must be memory-backed")
						Expect(v.EmptyDir.SizeLimit).To(Equal(ambientCAStagingSizeLimit), entry.name,
							"any retained combined-ca staging volume must be bounded")
					}
				}
			}
		})
	})
})

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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

const (
	testEnvTLSCAFile     = "TLS_CA_FILE"
	testVolumeTLSCA      = "tls-ca"
	testVolumeAAPCA      = "aap-ca"
	testVolumeCombinedCA = "combined-ca"
)

func getAllDeployments(kn *kubernautv1alpha1.Kubernaut) []*appsv1.Deployment {
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
	deps := make([]*appsv1.Deployment, 0, len(builders))
	for _, b := range builders {
		dep, err := b(kn)
		Expect(err).NotTo(HaveOccurred())
		deps = append(deps, dep)
	}
	return deps
}

func expectDeploymentBasics(dep *appsv1.Deployment, imageSuffix string) {
	Expect(dep.Namespace).To(Equal(testSystemNamespace), "Deployment %q namespace", dep.Name)
	Expect(dep.Spec.Replicas).NotTo(BeNil(), "Deployment %q replicas", dep.Name)
	Expect(*dep.Spec.Replicas).To(Equal(int32(1)), "Deployment %q replicas", dep.Name)
	Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty(), "Deployment %q containers", dep.Name)

	container := dep.Spec.Template.Spec.Containers[0]
	Expect(container.Image).NotTo(BeEmpty(), "Deployment %q image", dep.Name)
	Expect(container.Image).To(ContainSubstring(imageSuffix), "Deployment %q image should contain %q", dep.Name, imageSuffix)
}

func expectHasVolume(dep *appsv1.Deployment, name string) {
	found := false
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == name {
			found = true
			break
		}
	}
	Expect(found).To(BeTrue(), "Deployment %q should have volume %q", dep.Name, name)
}

func expectVolumeSourceConfigMap(dep *appsv1.Deployment, volumeName, expectedCMName string) {
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == volumeName {
			Expect(v.ConfigMap).NotTo(BeNil(), "Deployment %q volume %q should be backed by a ConfigMap", dep.Name, volumeName)
			Expect(v.ConfigMap.Name).To(Equal(expectedCMName), "Deployment %q volume %q ConfigMap name", dep.Name, volumeName)
			return
		}
	}
	Fail("Deployment " + dep.Name + " should have volume " + volumeName)
}

func expectHasVolumeMount(dep *appsv1.Deployment, name, mountPath string) {
	container := dep.Spec.Template.Spec.Containers[0]
	for _, vm := range container.VolumeMounts {
		if vm.Name == name {
			Expect(vm.MountPath).To(Equal(mountPath), "Deployment %q volume mount %q path", dep.Name, name)
			return
		}
	}
	Fail("Deployment " + dep.Name + " container should have volume mount " + name)
}

var _ = Describe("Deployments", func() {
	Context("Gateway", func() {
		It("has basic deployment properties", func() {
			kn := testKubernaut()
			dep, err := GatewayDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectDeploymentBasics(dep, "gateway")
			expectHasVolume(dep, "config")
			expectVolumeSourceConfigMap(dep, "config", "gateway-config")
			expectHasVolumeMount(dep, "config", "/etc/gateway")
		})

		It("does not set CORS_ALLOWED_ORIGINS env var (CORS moved to config YAML)", func() {
			kn := testKubernaut()
			dep, err := GatewayDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			container := dep.Spec.Template.Spec.Containers[0]
			for _, env := range container.Env {
				Expect(env.Name).NotTo(Equal("CORS_ALLOWED_ORIGINS"),
					"CORS is configured via config.yaml, not env vars")
			}
		})

		It("has tls-certs volume from gateway-tls Secret", func() {
			kn := testKubernaut()
			dep, err := GatewayDeployment(kn)
			Expect(err).NotTo(HaveOccurred())
			expectHasVolume(dep, "tls-certs")
			expectHasVolumeMount(dep, "tls-certs", InterServiceTLSCertDir)

			found := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "tls-certs" && v.Secret != nil {
					Expect(v.Secret.SecretName).To(Equal(GatewayTLSSecretName))
					found = true
				}
			}
			Expect(found).To(BeTrue(), "tls-certs volume should reference gateway-tls Secret")
		})
	})

	Context("DataStorage", func() {
		It("has init container for postgres", func() {
			kn := testKubernaut()
			dep, err := DataStorageDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectDeploymentBasics(dep, "datastorage")
			Expect(dep.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			init := dep.Spec.Template.Spec.InitContainers[0]
			Expect(init.Name).To(Equal("wait-for-postgres"))
			Expect(init.Resources.Requests).NotTo(BeNil())
		})

		It("has projected secrets volume", func() {
			kn := testKubernaut()
			dep, err := DataStorageDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			found := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "secrets" && v.Projected != nil {
					found = true
					Expect(v.Projected.Sources).To(HaveLen(2))
				}
			}
			Expect(found).To(BeTrue(), "DataStorage should have a 'secrets' projected volume")
		})

		It("has TLS cert volume", func() {
			kn := testKubernaut()
			dep, err := DataStorageDeployment(kn)
			Expect(err).NotTo(HaveOccurred())
			expectHasVolume(dep, "tls-certs")
			expectHasVolumeMount(dep, "tls-certs", InterServiceTLSCertDir)
		})
	})

	Context("AIAnalysis", func() {
		It("has policy volume", func() {
			kn := testKubernaut()
			dep, err := AIAnalysisDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectDeploymentBasics(dep, "aianalysis")
			expectHasVolume(dep, "rego-policies")
			expectVolumeSourceConfigMap(dep, "rego-policies", "aianalysis-policies")
			expectHasVolumeMount(dep, "rego-policies", "/etc/aianalysis/policies")
		})
	})

	Context("SignalProcessing", func() {
		It("has policy mount", func() {
			kn := testKubernaut()
			dep, err := SignalProcessingDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "policy")
			expectVolumeSourceConfigMap(dep, "policy", "signalprocessing-policy")
			expectHasVolumeMount(dep, "policy", "/etc/signalprocessing/policies")
		})

		It("uses custom proactive signal mappings", func() {
			kn := testKubernaut()
			kn.Spec.SignalProcessing.ProactiveSignalMappings = &kubernautv1alpha1.ConfigMapRef{ConfigMapName: "my-mappings"}
			dep, err := SignalProcessingDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "proactive-mappings")
			expectHasVolumeMount(dep, "proactive-mappings", "/etc/signalprocessing/proactive-signal-mappings.yaml")
		})

		It("uses default proactive signal mappings", func() {
			kn := testKubernaut()
			dep, err := SignalProcessingDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "proactive-mappings")
			expectVolumeSourceConfigMap(dep, "proactive-mappings", "signalprocessing-proactive-signal-mappings")
			expectHasVolumeMount(dep, "proactive-mappings", "/etc/signalprocessing/proactive-signal-mappings.yaml")
		})
	})

	Context("Notification", func() {
		It("mounts Slack credentials when configured", func() {
			kn := testKubernaut()
			kn.Spec.Notification.Slack.SecretName = "slack-secret"
			dep, err := NotificationDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "credentials")
			expectHasVolumeMount(dep, "credentials", "/etc/notification/credentials")

			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "credentials" {
					Expect(v.Projected).NotTo(BeNil())
					src := v.Projected.Sources[0].Secret
					Expect(src.Optional).NotTo(BeNil())
					Expect(*src.Optional).To(BeTrue(), "slack secret projection should be optional")
				}
			}
		})

		It("uses emptyDir credentials when Slack is not configured", func() {
			kn := testKubernaut()
			dep, err := NotificationDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			found := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "credentials" {
					found = true
					Expect(v.EmptyDir).NotTo(BeNil(), "credentials volume should be an emptyDir without Slack")
				}
			}
			Expect(found).To(BeTrue(), "Notification should have a credentials volume even without Slack")
		})

		It("has notification-output emptyDir volume", func() {
			kn := testKubernaut()
			dep, err := NotificationDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "notification-output")
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "notification-output" {
					Expect(v.EmptyDir).NotTo(BeNil())
				}
			}
		})

		It("has routing config mount", func() {
			kn := testKubernaut()
			dep, err := NotificationDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "routing-config")
			expectVolumeSourceConfigMap(dep, "routing-config", "notification-routing-config")
			expectHasVolumeMount(dep, "routing-config", "/etc/notification-routing")
		})

		It("uses BYO routing config map name", func() {
			kn := testKubernaut()
			kn.Spec.Notification.Routing = &kubernautv1alpha1.ConfigMapRef{ConfigMapName: "my-routing"}
			dep, err := NotificationDeployment(kn)
			Expect(err).NotTo(HaveOccurred())
			expectHasVolume(dep, "routing-config")
			expectVolumeSourceConfigMap(dep, "routing-config", "my-routing")
			expectHasVolumeMount(dep, "routing-config", "/etc/notification-routing")
		})
	})

	Context("KubernautAgent", func() {
		It("has LLM credentials volume", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectDeploymentBasics(dep, "kubernautagent")
			expectHasVolume(dep, "llm-credentials")
			expectHasVolumeMount(dep, "llm-credentials", "/etc/kubernaut-agent/credentials")
		})

		It("passes config args", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			container := dep.Spec.Template.Spec.Containers[0]
			want := []string{
				"-config", "/etc/kubernaut-agent/config.yaml",
				"-llm-runtime", "/etc/kubernaut-agent/llm-runtime/llm-runtime.yaml",
			}
			Expect(container.Args).To(Equal(want))
		})

		It("uses llm-runtime volume instead of sdk-config", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(Equal("sdk-config"), "should not use sdk-config volume")
			}
			expectHasVolume(dep, "llm-runtime")
			expectVolumeSourceConfigMap(dep, "llm-runtime", "kubernaut-agent-llm-runtime")
			expectHasVolumeMount(dep, "llm-runtime", "/etc/kubernaut-agent/llm-runtime")

			container := dep.Spec.Template.Spec.Containers[0]
			hasPair := false
			for i := 0; i < len(container.Args)-1; i++ {
				if container.Args[i] == "-llm-runtime" && container.Args[i+1] == "/etc/kubernaut-agent/llm-runtime/llm-runtime.yaml" {
					hasPair = true
					break
				}
			}
			Expect(hasPair).To(BeTrue(), "should pass -llm-runtime with llm-runtime.yaml path")
		})

		It("mounts OAuth2 credentials when enabled", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.LLM.OAuth2.Enabled = true
			kn.Spec.KubernautAgent.LLM.OAuth2.CredentialsSecretRef = "oauth2-credentials-secret"
			dep, err := KubernautAgentDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "oauth2-credentials")
			expectHasVolumeMount(dep, "oauth2-credentials", "/etc/kubernaut-agent/oauth2")
			found := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "oauth2-credentials" {
					found = true
					Expect(v.Secret).NotTo(BeNil())
					Expect(v.Secret.SecretName).To(Equal("oauth2-credentials-secret"))
				}
			}
			Expect(found).To(BeTrue(), "oauth2-credentials volume not found")
		})

		It("has service-ca volume when monitoring enabled", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "service-ca")
			expectHasVolumeMount(dep, "service-ca", "/etc/ssl/ka")
		})

		It("sets IS_OPENSHIFT env when monitoring enabled", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			container := dep.Spec.Template.Spec.Containers[0]
			found := false
			for _, env := range container.Env {
				if env.Name == "IS_OPENSHIFT" && env.Value == "True" {
					found = true
				}
			}
			Expect(found).To(BeTrue(), "should have IS_OPENSHIFT=True when monitoring enabled")
		})

		It("omits IS_OPENSHIFT env when monitoring disabled", func() {
			kn := testKubernaut()
			disabled := false
			kn.Spec.Monitoring.Enabled = &disabled
			dep, err := KubernautAgentDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			container := dep.Spec.Template.Spec.Containers[0]
			for _, env := range container.Env {
				Expect(env.Name).NotTo(Equal("IS_OPENSHIFT"), "should not have IS_OPENSHIFT when monitoring disabled")
			}
		})

		It("omits service-ca volume when monitoring disabled", func() {
			kn := testKubernaut()
			disabled := false
			kn.Spec.Monitoring.Enabled = &disabled
			dep, err := KubernautAgentDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(Equal("service-ca"), "should not have service-ca when monitoring disabled")
			}
		})

		It("has TLS cert volume", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn)
			Expect(err).NotTo(HaveOccurred())
			expectHasVolume(dep, "tls-certs")
			expectHasVolumeMount(dep, "tls-certs", InterServiceTLSCertDir)
		})
	})

	Context("EffectivenessMonitor", func() {
		It("has service-ca volume", func() {
			kn := testKubernaut()
			dep, err := EffectivenessMonitorDeployment(kn)
			Expect(err).NotTo(HaveOccurred())
			expectHasVolume(dep, "service-ca")
		})

		It("has wait-for-service-ca init container when monitoring enabled", func() {
			kn := testKubernaut()
			dep, err := EffectivenessMonitorDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			Expect(dep.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			init := dep.Spec.Template.Spec.InitContainers[0]
			Expect(init.Name).To(Equal("wait-for-service-ca"))
			Expect(init.Resources.Requests).NotTo(BeNil())

			hasMount := false
			for _, vm := range init.VolumeMounts {
				if vm.Name == "service-ca" && vm.MountPath == "/etc/ssl/em" {
					hasMount = true
				}
			}
			Expect(hasMount).To(BeTrue(), "init container should mount service-ca at /etc/ssl/em")
		})

		It("has no init container when monitoring disabled", func() {
			kn := testKubernaut()
			disabled := false
			kn.Spec.Monitoring.Enabled = &disabled
			dep, err := EffectivenessMonitorDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			Expect(dep.Spec.Template.Spec.InitContainers).To(BeEmpty())
		})
	})

	Context("AuthWebhook", func() {
		It("has TLS and webhook port", func() {
			kn := testKubernaut()
			dep, err := AuthWebhookDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			expectDeploymentBasics(dep, "authwebhook")
			expectHasVolume(dep, "webhook-certs")
			expectHasVolumeMount(dep, "webhook-certs", "/tmp/k8s-webhook-server/serving-certs")

			container := dep.Spec.Template.Spec.Containers[0]
			found := false
			for _, p := range container.Ports {
				if p.ContainerPort == 9443 && p.Name == "webhook" {
					found = true
				}
			}
			Expect(found).To(BeTrue(), "AuthWebhook should expose port 9443 named 'webhook'")
		})

		It("uses Recreate strategy", func() {
			kn := testKubernaut()
			dep, err := AuthWebhookDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			Expect(dep.Spec.Strategy.Type).To(Equal(appsv1.RecreateDeploymentStrategyType))
		})
	})

	Context("WorkflowExecution AAP CA cert", func() {
		It("has no init container without caCertSecretRef", func() {
			kn := testKubernaut()
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			Expect(dep.Spec.Template.Spec.InitContainers).To(BeEmpty())

			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(Equal(testVolumeAAPCA))
				Expect(v.Name).NotTo(Equal(testVolumeCombinedCA))
			}

			container := dep.Spec.Template.Spec.Containers[0]
			for _, e := range container.Env {
				if e.Name == testEnvTLSCAFile {
					Expect(e.Value).To(Equal(InterServiceTLSCAFile))
				}
			}
		})

		It("does not set SSL_CERT_FILE", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			container := dep.Spec.Template.Spec.Containers[0]
			for _, e := range container.Env {
				Expect(e.Name).NotTo(Equal("SSL_CERT_FILE"), "WFE must not set SSL_CERT_FILE env var")
			}
		})

		It("has init container with caCertSecretRef", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			Expect(dep.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(dep.Spec.Template.Spec.InitContainers[0].Name).To(Equal("build-ca-bundle"))
		})

		It("mounts secret volume with custom key", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{
				Name: "aap-ca-secret",
				Key:  "custom-ca.pem",
			}
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			found := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == testVolumeAAPCA {
					found = true
					Expect(v.Secret).NotTo(BeNil())
					Expect(v.Secret.SecretName).To(Equal("aap-ca-secret"))
					Expect(v.Secret.Items).To(HaveLen(1))
					Expect(v.Secret.Items[0].Key).To(Equal("custom-ca.pem"))
				}
			}
			Expect(found).To(BeTrue(), "WFE with caCertSecretRef should have aap-ca volume")
		})

		It("uses default key ca.crt", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == testVolumeAAPCA {
					Expect(v.Secret.Items).To(HaveLen(1))
					Expect(v.Secret.Items[0].Key).To(Equal("ca.crt"))
					return
				}
			}
			Fail("aap-ca volume not found")
		})

		It("has combined-ca emptyDir volume", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == testVolumeCombinedCA {
					Expect(v.EmptyDir).NotTo(BeNil())
					return
				}
			}
			Fail("combined-ca volume not found")
		})

		It("overrides TLS_CA_FILE", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			container := dep.Spec.Template.Spec.Containers[0]
			for _, e := range container.Env {
				if e.Name == testEnvTLSCAFile {
					Expect(e.Value).To(Equal("/etc/combined-ca/ca-bundle.crt"))
					return
				}
			}
			Fail("TLS_CA_FILE env var not found")
		})

		It("init container concatenates correct sources", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			init := dep.Spec.Template.Spec.InitContainers[0]
			Expect(init.Args).NotTo(BeEmpty())
			cmd := init.Args[0]
			Expect(cmd).To(ContainSubstring("/etc/tls-ca/service-ca.crt"))
			Expect(cmd).To(ContainSubstring("/aap-ca/aap-ca.crt"))
			Expect(cmd).To(ContainSubstring("/combined/ca-bundle.crt"))
		})

		It("init container has required volume mounts", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			init := dep.Spec.Template.Spec.InitContainers[0]
			mountNames := make(map[string]bool)
			for _, vm := range init.VolumeMounts {
				mountNames[vm.Name] = true
			}
			for _, required := range []string{testVolumeTLSCA, testVolumeAAPCA, testVolumeCombinedCA} {
				Expect(mountNames[required]).To(BeTrue(), "init container should mount volume %q", required)
			}
		})

		It("main container mounts combined-ca", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			container := dep.Spec.Template.Spec.Containers[0]
			for _, vm := range container.VolumeMounts {
				if vm.Name == testVolumeCombinedCA {
					Expect(vm.MountPath).To(Equal("/etc/combined-ca"))
					Expect(vm.ReadOnly).To(BeTrue())
					return
				}
			}
			Fail("main container should mount combined-ca volume")
		})

		It("init container has restricted security context", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			init := dep.Spec.Template.Spec.InitContainers[0]
			sc := init.SecurityContext
			Expect(sc).NotTo(BeNil())
			Expect(sc.AllowPrivilegeEscalation).NotTo(BeNil())
			Expect(*sc.AllowPrivilegeEscalation).To(BeFalse())
			Expect(sc.ReadOnlyRootFilesystem).NotTo(BeNil())
			Expect(*sc.ReadOnlyRootFilesystem).To(BeTrue())
			Expect(sc.Capabilities).NotTo(BeNil())
			Expect(sc.Capabilities.Drop).NotTo(BeEmpty())
		})
	})

	Context("overrideTLSCAFile helper", func() {
		It("replaces existing TLS_CA_FILE", func() {
			env := []corev1.EnvVar{
				{Name: "OTHER", Value: "foo"},
				{Name: testEnvTLSCAFile, Value: "/old/path"},
			}
			result := overrideTLSCAFile(env, "/new/path")
			found := false
			for _, e := range result {
				if e.Name == testEnvTLSCAFile {
					found = true
					Expect(e.Value).To(Equal("/new/path"))
				}
			}
			Expect(found).To(BeTrue(), "TLS_CA_FILE not found after override")
		})

		It("appends when missing", func() {
			env := []corev1.EnvVar{{Name: "OTHER", Value: "foo"}}
			result := overrideTLSCAFile(env, "/new/path")
			Expect(result).To(HaveLen(2))
			found := false
			for _, e := range result {
				if e.Name == testEnvTLSCAFile && e.Value == "/new/path" {
					found = true
				}
			}
			Expect(found).To(BeTrue(), "TLS_CA_FILE should be appended when missing")
		})
	})

	Context("cross-cutting: all deployments", func() {
		It("have HTTPGet probes with correct paths and timing", func() {
			kn := testKubernaut()
			deps := getAllDeployments(kn)

			for _, dep := range deps {
				container := dep.Spec.Template.Spec.Containers[0]
				component := dep.Spec.Template.Labels["app"]

				Expect(container.LivenessProbe).NotTo(BeNil(), "Deployment %q should have liveness probe", dep.Name)
				Expect(container.ReadinessProbe).NotTo(BeNil(), "Deployment %q should have readiness probe", dep.Name)

				Expect(container.LivenessProbe.HTTPGet).NotTo(BeNil(), "Deployment %q liveness probe should use HTTPGet", dep.Name)
				Expect(container.ReadinessProbe.HTTPGet).NotTo(BeNil(), "Deployment %q readiness probe should use HTTPGet", dep.Name)

				pc := probeConfigForComponent(component)
				lp := container.LivenessProbe
				rp := container.ReadinessProbe

				Expect(lp.HTTPGet.Path).To(Equal(pc.LivenessPath), "Deployment %q liveness path", dep.Name)
				Expect(rp.HTTPGet.Path).To(Equal(pc.ReadinessPath), "Deployment %q readiness path", dep.Name)

				Expect(lp.InitialDelaySeconds).To(Equal(pc.LivenessInitialDelay), "%s liveness InitialDelaySeconds", dep.Name)
				Expect(lp.PeriodSeconds).To(Equal(pc.LivenessPeriod), "%s liveness PeriodSeconds", dep.Name)
				Expect(lp.TimeoutSeconds).To(Equal(pc.LivenessTimeout), "%s liveness TimeoutSeconds", dep.Name)
				Expect(lp.FailureThreshold).To(Equal(pc.LivenessFailureThreshold), "%s liveness FailureThreshold", dep.Name)

				Expect(rp.InitialDelaySeconds).To(Equal(pc.ReadinessInitialDelay), "%s readiness InitialDelaySeconds", dep.Name)
				Expect(rp.PeriodSeconds).To(Equal(pc.ReadinessPeriod), "%s readiness PeriodSeconds", dep.Name)
				Expect(rp.TimeoutSeconds).To(Equal(pc.ReadinessTimeout), "%s readiness TimeoutSeconds", dep.Name)
				Expect(rp.FailureThreshold).To(Equal(pc.ReadinessFailureThreshold), "%s readiness FailureThreshold", dep.Name)
			}
		})

		It("expose metrics port on expected components", func() {
			kn := testKubernaut()
			withMetrics := map[string]bool{
				ComponentGateway:                 true,
				ComponentDataStorage:             true,
				ComponentAIAnalysis:              true,
				ComponentSignalProcessing:        true,
				ComponentRemediationOrchestrator: true,
				ComponentWorkflowExecution:       true,
				ComponentEffectivenessMonitor:    true,
				ComponentNotification:            true,
				ComponentKubernautAgent:          true,
			}

			for _, dep := range getAllDeployments(kn) {
				component := dep.Spec.Template.Labels["app"]
				container := dep.Spec.Template.Spec.Containers[0]

				hasMetrics := false
				for _, p := range container.Ports {
					if p.ContainerPort == PortMetrics && p.Name == "metrics" {
						hasMetrics = true
					}
				}

				if withMetrics[component] {
					Expect(hasMetrics).To(BeTrue(), "Deployment %q should expose metrics port 9090", dep.Name)
				} else {
					Expect(hasMetrics).To(BeFalse(), "Deployment %q should NOT expose metrics port 9090", dep.Name)
				}
			}
		})

		It("pass correct config args", func() {
			kn := testKubernaut()

			wantArgs := map[string][]string{
				ComponentGateway:                 {"--config=/etc/gateway/config.yaml"},
				ComponentAIAnalysis:              {"-config", "/etc/aianalysis/config.yaml"},
				ComponentSignalProcessing:        {"--config=/etc/signalprocessing/config.yaml"},
				ComponentRemediationOrchestrator: {"--config=/etc/config/remediationorchestrator.yaml"},
				ComponentWorkflowExecution:       {"--config=/etc/config/workflowexecution.yaml"},
				ComponentEffectivenessMonitor:    {"--config=/etc/effectivenessmonitor/effectivenessmonitor.yaml"},
				ComponentNotification:            {"-config", "/etc/notification/config.yaml"},
				ComponentKubernautAgent:          {"-config", "/etc/kubernaut-agent/config.yaml", "-llm-runtime", "/etc/kubernaut-agent/llm-runtime/llm-runtime.yaml"},
				ComponentAuthWebhook:             {"-config=/etc/authwebhook/authwebhook.yaml"},
			}

			for _, dep := range getAllDeployments(kn) {
				component := dep.Spec.Template.Labels["app"]
				container := dep.Spec.Template.Spec.Containers[0]

				want, hasExpected := wantArgs[component]
				if !hasExpected {
					Expect(container.Args).To(BeEmpty(), "Deployment %q should have no args", dep.Name)
					continue
				}
				Expect(container.Args).To(Equal(want), "Deployment %q args", dep.Name)
			}
		})

		It("have restricted security contexts", func() {
			kn := testKubernaut()
			for _, dep := range getAllDeployments(kn) {
				psc := dep.Spec.Template.Spec.SecurityContext
				Expect(psc).NotTo(BeNil(), "Deployment %q should have pod security context", dep.Name)
				Expect(psc.RunAsNonRoot).NotTo(BeNil(), "Deployment %q RunAsNonRoot", dep.Name)
				Expect(*psc.RunAsNonRoot).To(BeTrue(), "Deployment %q RunAsNonRoot should be true", dep.Name)

				for _, c := range dep.Spec.Template.Spec.Containers {
					Expect(c.SecurityContext).NotTo(BeNil(), "Deployment %q container %q security context", dep.Name, c.Name)
					Expect(c.SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil(), "Deployment %q container %q AllowPrivilegeEscalation", dep.Name, c.Name)
					Expect(*c.SecurityContext.AllowPrivilegeEscalation).To(BeFalse(), "Deployment %q container %q AllowPrivilegeEscalation should be false", dep.Name, c.Name)
				}
			}
		})

		It("use IfNotPresent pull policy", func() {
			kn := testKubernaut()
			for _, dep := range getAllDeployments(kn) {
				for _, c := range dep.Spec.Template.Spec.Containers {
					Expect(c.ImagePullPolicy).To(Equal(corev1.PullIfNotPresent), "Deployment %q container %q pullPolicy", dep.Name, c.Name)
				}
			}
		})

		It("set correct service accounts", func() {
			kn := testKubernaut()
			for _, dep := range getAllDeployments(kn) {
				component := dep.Spec.Template.Labels["app"]
				Expect(component).NotTo(BeEmpty(), "Deployment %q missing 'app' label", dep.Name)
				wantSA := ServiceAccountName(component)
				Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal(wantSA), "Deployment %q SA", dep.Name)
			}
		})

		It("have inter-service TLS CA volume, mount, and env var", func() {
			kn := testKubernaut()
			for _, dep := range getAllDeployments(kn) {
				container := dep.Spec.Template.Spec.Containers[0]

				hasCAVolume := false
				for _, v := range dep.Spec.Template.Spec.Volumes {
					if v.Name == testVolumeTLSCA && v.ConfigMap != nil && v.ConfigMap.Name == InterServiceCAConfigMapName {
						hasCAVolume = true
					}
				}
				Expect(hasCAVolume).To(BeTrue(), "Deployment %q missing %s volume", dep.Name, testVolumeTLSCA)

				hasCAMount := false
				for _, vm := range container.VolumeMounts {
					if vm.Name == testVolumeTLSCA && vm.MountPath == "/etc/tls-ca" {
						hasCAMount = true
					}
				}
				Expect(hasCAMount).To(BeTrue(), "Deployment %q missing %s volume mount", dep.Name, testVolumeTLSCA)

				hasCAEnv := false
				for _, env := range container.Env {
					if env.Name == "TLS_CA_FILE" && env.Value == InterServiceTLSCAFile {
						hasCAEnv = true
					}
				}
				Expect(hasCAEnv).To(BeTrue(), "Deployment %q missing TLS_CA_FILE env var", dep.Name)
			}
		})

		It("map ServiceAccountName correctly for all components", func() {
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
				Expect(ServiceAccountName(component)).To(Equal(wantName), "ServiceAccountName(%q)", component)
			}
		})

		It("cover all components in ServiceAccountName", func() {
			for _, component := range AllComponents() {
				Expect(ServiceAccountName(component)).NotTo(BeEmpty(), "ServiceAccountName(%q)", component)
			}
		})

		It("have preferred pod anti-affinity", func() {
			kn := testKubernaut()
			for _, dep := range getAllDeployments(kn) {
				component := dep.Spec.Template.Labels["app"]

				affinity := dep.Spec.Template.Spec.Affinity
				Expect(affinity).NotTo(BeNil(), "Deployment %q Affinity", dep.Name)
				paa := affinity.PodAntiAffinity
				Expect(paa).NotTo(BeNil(), "Deployment %q PodAntiAffinity", dep.Name)

				preferred := paa.PreferredDuringSchedulingIgnoredDuringExecution
				Expect(preferred).NotTo(BeEmpty(), "Deployment %q preferred anti-affinity terms", dep.Name)

				term := preferred[0]
				Expect(term.Weight).To(Equal(int32(100)), "Deployment %q anti-affinity weight", dep.Name)
				Expect(term.PodAffinityTerm.TopologyKey).To(Equal("kubernetes.io/hostname"), "Deployment %q topology key", dep.Name)

				sel := term.PodAffinityTerm.LabelSelector
				Expect(sel).NotTo(BeNil(), "Deployment %q anti-affinity label selector", dep.Name)

				for k, v := range SelectorLabels(component) {
					Expect(sel.MatchLabels[k]).To(Equal(v), "Deployment %q anti-affinity selector label %q", dep.Name, k)
				}
			}
		})
	})
})

var _ = Describe("overrideTLSCAFile standalone", func() {
	It("replaces existing entry", func() {
		env := []corev1.EnvVar{
			{Name: "OTHER", Value: "foo"},
			{Name: testEnvTLSCAFile, Value: "/old/path"},
		}
		result := overrideTLSCAFile(env, "/new/path")
		for _, e := range result {
			if e.Name == testEnvTLSCAFile {
				Expect(e.Value).To(Equal("/new/path"))
				return
			}
		}
		Fail("TLS_CA_FILE not found after override")
	})

	It("appends when missing", func() {
		env := []corev1.EnvVar{{Name: "OTHER", Value: "foo"}}
		result := overrideTLSCAFile(env, "/new/path")
		Expect(result).To(HaveLen(2))
		found := false
		for _, e := range result {
			if e.Name == testEnvTLSCAFile && e.Value == "/new/path" {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})
})

var _ = Describe("APIFrontendDeployment", func() {
	It("builds successfully with AF enabled", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		expectDeploymentBasics(dep, "apifrontend")
	})

	It("exposes HTTPS (8443), health (8081), and metrics (9090) ports", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		portMap := map[string]int32{}
		for _, p := range container.Ports {
			portMap[p.Name] = p.ContainerPort
		}
		Expect(portMap).To(HaveKeyWithValue("https", PortHTTPS))
		Expect(portMap).To(HaveKeyWithValue("health", PortHealthProbe))
		Expect(portMap).To(HaveKeyWithValue("metrics", PortMetrics))
	})

	It("mounts config, tls-server, tls-ca, and tmp volumes", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(dep, "config")
		expectHasVolume(dep, "tls-server")
		expectHasVolume(dep, testVolumeTLSCA)
		expectHasVolume(dep, "tmp")
		expectHasVolumeMount(dep, "config", "/etc/apifrontend")
		expectHasVolumeMount(dep, "tls-server", "/etc/apifrontend/tls")
		expectHasVolumeMount(dep, testVolumeTLSCA, "/etc/apifrontend/tls-ca")
		expectHasVolumeMount(dep, "tmp", "/tmp")
	})

	It("sets liveness and readiness probes", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		Expect(container.LivenessProbe).NotTo(BeNil())
		Expect(container.ReadinessProbe).NotTo(BeNil())
		Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/healthz"))
		Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/readyz"))
	})

	It("includes Prometheus annotations", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		ann := dep.Spec.Template.Annotations
		Expect(ann["prometheus.io/scrape"]).To(Equal("true"))
		Expect(ann["prometheus.io/port"]).To(Equal("9090"))
	})

	It("ignores deprecated rbacRolesConfigMapRef (volume is plain ConfigMap)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBACRolesConfigMapRef = &kubernautv1alpha1.ConfigMapRef{
			ConfigMapName: "my-custom-rbac",
		}
		dep, err := APIFrontendDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "config" {
				Expect(v.Projected).To(BeNil(),
					"config volume should be plain ConfigMap, not projected")
				Expect(v.ConfigMap).NotTo(BeNil())
				Expect(v.ConfigMap.Name).To(Equal("apifrontend-config"))
				return
			}
		}
		Fail("AF deployment should have a 'config' volume")
	})

	It("sets terminationGracePeriodSeconds to drainSeconds + 5", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(dep.Spec.Template.Spec.TerminationGracePeriodSeconds).NotTo(BeNil())
		Expect(*dep.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(20)),
			"default drainSeconds=15 + 5 buffer = 20")
	})

	It("adjusts terminationGracePeriodSeconds for custom drainSeconds", func() {
		kn := testKubernautWithAF()
		drain := 60
		kn.Spec.APIFrontend.Shutdown.DrainSeconds = &drain
		dep, err := APIFrontendDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(*dep.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(65)))
	})

	It("uses plain ConfigMap volume, not projected", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "config" {
				Expect(v.Projected).To(BeNil(),
					"AF config volume should be a plain ConfigMap, not projected")
				Expect(v.ConfigMap).NotTo(BeNil(),
					"AF config volume should use ConfigMap source")
				Expect(v.ConfigMap.Name).To(Equal("apifrontend-config"))
				return
			}
		}
		Fail("AF deployment should have a 'config' volume")
	})

	It("does not reference rbac_roles.yaml", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Projected != nil {
				for _, src := range v.Projected.Sources {
					if src.ConfigMap != nil {
						for _, item := range src.ConfigMap.Items {
							Expect(item.Key).NotTo(Equal("rbac_roles.yaml"),
								"AF deployment should not reference rbac_roles.yaml")
						}
					}
				}
			}
			if v.ConfigMap != nil {
				for _, item := range v.ConfigMap.Items {
					Expect(item.Key).NotTo(Equal("rbac_roles.yaml"),
						"AF deployment should not reference rbac_roles.yaml")
				}
			}
		}
	})

	It("SC-8: AF container declares PortHTTPS regardless of SPIRE config", func() {
		for _, spireEnabled := range []bool{false, true} {
			kn := testKubernautWithAF()
			kn.Spec.APIFrontend.SPIRE.Enabled = spireEnabled
			dep, err := APIFrontendDeployment(kn)
			Expect(err).NotTo(HaveOccurred())
			portMap := map[string]int32{}
			for _, p := range dep.Spec.Template.Spec.Containers[0].Ports {
				portMap[p.Name] = p.ContainerPort
			}
			Expect(portMap).To(HaveKeyWithValue("https", PortHTTPS))
		}
	})
})

var _ = Describe("DataStorageDeployment with Valkey TLS", func() {
	It("mounts valkey-ca and valkey-client-cert when TLS is enabled", func() {
		kn := testKubernautWithValkeyTLS()
		dep, err := DataStorageDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(dep, "valkey-ca")
		expectHasVolume(dep, "valkey-client-cert")
		expectHasVolumeMount(dep, "valkey-ca", "/etc/valkey-tls/ca")
		expectHasVolumeMount(dep, "valkey-client-cert", "/etc/valkey-tls/client")
	})

	It("does not mount valkey TLS volumes when TLS is disabled", func() {
		kn := testKubernaut()
		dep, err := DataStorageDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		for _, v := range dep.Spec.Template.Spec.Volumes {
			Expect(v.Name).NotTo(HavePrefix("valkey-"),
				"should not have valkey TLS volume %q when TLS is disabled", v.Name)
		}
	})
})

var _ = Describe("DataStorage Signing Cert", func() {
	It("mounts signing cert when configured", func() {
		kn := testKubernaut()
		kn.Spec.DataStorage.SigningCert = &kubernautv1alpha1.SigningCertSpec{
			SecretName: "datastorage-signing-cert",
		}
		dep, err := DataStorageDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(dep, "signing-cert")
		expectHasVolumeMount(dep, "signing-cert", "/etc/certs")
	})

	It("uses custom mount path when specified", func() {
		kn := testKubernaut()
		kn.Spec.DataStorage.SigningCert = &kubernautv1alpha1.SigningCertSpec{
			SecretName: "datastorage-signing-cert",
			MountPath:  "/custom/certs",
		}
		dep, err := DataStorageDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolumeMount(dep, "signing-cert", "/custom/certs")
	})

	It("falls back to service-ca TLS cert when signing cert is not configured", func() {
		kn := testKubernaut()
		dep, err := DataStorageDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolumeMount(dep, "signing-cert", "/etc/certs")
		found := false
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "signing-cert" {
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal(DataStorageTLSSecretName))
				found = true
			}
		}
		Expect(found).To(BeTrue(), "signing-cert volume should use the service-ca TLS secret")
	})
})

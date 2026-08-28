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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	testEnvTLSCAFile       = "TLS_CA_FILE"
	testEnvSSLCertFile     = "SSL_CERT_FILE"
	testVolumeTLSCA        = "tls-ca"
	testVolumeAAPCA        = "aap-ca"
	testVolumeCombinedCA   = "combined-ca"
	testVolumeLLMTLSClient = "llm-tls-client"
	testMTLSCertFile       = "/etc/tls/tls.crt"
	testMTLSKeyFile        = "/etc/tls/tls.key"
	testVolumeFleetOAuth2  = "fleet-oauth2"
	testVolumeSecrets      = "secrets"
)

func getAllDeployments(kn *kubernautv1alpha1.Kubernaut) []*appsv1.Deployment {
	knV2 := testKnV2(kn)
	type builder func(*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error)
	builders := []builder{
		GatewayDeployment,
		func(kn *kubernautv1alpha1.Kubernaut, _ *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
			return DataStorageDeployment(kn)
		},
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
		dep, err := b(kn, knV2)
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

// hasPprofContainerPort reports whether dep's first container exposes the
// pprof containerPort (name "pprof", PortPprof/6060) (#403).
func hasPprofContainerPort(dep *appsv1.Deployment) bool {
	for _, p := range dep.Spec.Template.Spec.Containers[0].Ports {
		if p.Name == "pprof" && p.ContainerPort == PortPprof {
			return true
		}
	}
	return false
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
			dep, err := GatewayDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			expectDeploymentBasics(dep, "gateway")
			expectHasVolume(dep, "config")
			expectVolumeSourceConfigMap(dep, "config", "gateway-config")
			expectHasVolumeMount(dep, "config", "/etc/gateway")
		})

		It("does not set CORS_ALLOWED_ORIGINS env var (CORS moved to config YAML)", func() {
			kn := testKubernaut()
			dep, err := GatewayDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			container := dep.Spec.Template.Spec.Containers[0]
			for _, env := range container.Env {
				Expect(env.Name).NotTo(Equal("CORS_ALLOWED_ORIGINS"),
					"CORS is configured via config.yaml, not env vars")
			}
		})

		It("has tls-certs volume from gateway-tls Secret", func() {
			kn := testKubernaut()
			dep, err := GatewayDeployment(kn, testKnV2(kn))
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

		// CONS-005 (#423): gateway.resources was flagged as a cross-consumer
		// consistency gap (consumer exists in internal/resources, no test in
		// that package). This asserts spec.gateway.resources propagates
		// verbatim onto the rendered Gateway Deployment's container.
		It("CONS-005 [CM-6]: propagates spec.gateway.resources verbatim onto the container", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Resources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
				Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
			}
			dep, err := GatewayDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			got := dep.Spec.Template.Spec.Containers[0].Resources
			Expect(got.Requests.Cpu().String()).To(Equal("250m"), "gateway container should render spec.gateway.resources.requests.cpu verbatim")
			Expect(got.Requests.Memory().String()).To(Equal("256Mi"), "gateway container should render spec.gateway.resources.requests.memory verbatim")
			Expect(got.Limits.Cpu().String()).To(Equal("500m"), "gateway container should render spec.gateway.resources.limits.cpu verbatim")
			Expect(got.Limits.Memory().String()).To(Equal("512Mi"), "gateway container should render spec.gateway.resources.limits.memory verbatim")
		})
	})

	// CONS-001 (#423): image.pullSecrets was flagged as a cross-consumer
	// consistency gap (consumer exists in internal/resources, no test in
	// that package). buildDeployment is the single shared builder every
	// component's DeploymentParams flows through, so testing it via
	// GatewayDeployment exercises the same code path for every component.
	Context("Image.PullSecrets", func() {
		It("CONS-001 [CM-6, SC-13]: propagates spec.image.pullSecrets verbatim onto every rendered pod spec", func() {
			kn := testKubernaut()
			kn.Spec.Image.PullSecrets = []corev1.LocalObjectReference{{Name: "my-registry-creds"}}
			dep, err := GatewayDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			Expect(dep.Spec.Template.Spec.ImagePullSecrets).To(Equal([]corev1.LocalObjectReference{{Name: "my-registry-creds"}}),
				"pod spec should render spec.image.pullSecrets verbatim so private-registry image pulls succeed")
		})

		It("CONS-001b: omits imagePullSecrets from the pod spec when unset (regression guard)", func() {
			kn := testKubernaut()
			dep, err := GatewayDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			Expect(dep.Spec.Template.Spec.ImagePullSecrets).To(BeEmpty(), "pod spec should have no imagePullSecrets when spec.image.pullSecrets is unset")
		})
	})

	// #423 coverage backfill: image.pullPolicy had zero test references
	// anywhere in the codebase, despite every component's buildDeployment
	// call site propagating it onto the container's ImagePullPolicy.
	Context("Image.PullPolicy", func() {
		It("[CM-6] propagates a non-default spec.image.pullPolicy onto the container", func() {
			kn := testKubernaut()
			kn.Spec.Image.PullPolicy = corev1.PullAlways
			dep, err := GatewayDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			Expect(dep.Spec.Template.Spec.Containers[0].ImagePullPolicy).To(Equal(corev1.PullAlways),
				"container should render spec.image.pullPolicy verbatim")
		})
	})

	// #423 coverage backfill: the remaining 10 corev1.ResourceRequirements
	// passthrough fields (gateway.resources was already closed as CONS-005
	// above) had zero test references anywhere in the codebase per
	// docs/tests/421/CRD_FIELD_COVERAGE_AUDIT.md. Each component's builder
	// flows its own DeploymentParams.Resources through the single shared
	// buildDeployment -> corev1.Container.Resources assignment, so one
	// table covers all 10 with the same verbatim-passthrough assertion.
	Describe("Component .resources passthrough (#423 coverage backfill)", func() {
		type resourcesCase struct {
			component string
			newKn     func() *kubernautv1alpha1.Kubernaut
			setRes    func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements)
			build     func(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error)
		}

		testResources := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("111m"), corev1.ResourceMemory: resource.MustParse("111Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("222m"), corev1.ResourceMemory: resource.MustParse("222Mi")},
		}

		DescribeTable("UT-RES [CM-6]: propagates spec.<component>.resources verbatim onto the container",
			func(tc resourcesCase) {
				kn := tc.newKn()
				tc.setRes(kn, testResources)
				dep, err := tc.build(kn)
				Expect(err).NotTo(HaveOccurred())
				got := dep.Spec.Template.Spec.Containers[0].Resources
				Expect(got.Requests.Cpu().String()).To(Equal("111m"), "%s container should render spec.%s.resources.requests.cpu verbatim", tc.component, tc.component)
				Expect(got.Requests.Memory().String()).To(Equal("111Mi"), "%s container should render spec.%s.resources.requests.memory verbatim", tc.component, tc.component)
				Expect(got.Limits.Cpu().String()).To(Equal("222m"), "%s container should render spec.%s.resources.limits.cpu verbatim", tc.component, tc.component)
				Expect(got.Limits.Memory().String()).To(Equal("222Mi"), "%s container should render spec.%s.resources.limits.memory verbatim", tc.component, tc.component)
			},
			Entry("apiFrontend", resourcesCase{
				component: "apiFrontend",
				newKn:     testKubernautWithAF,
				setRes: func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements) {
					kn.Spec.APIFrontend.Resources = res
				},
				build: func(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
					return APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
				},
			}),
			Entry("kubernautAgent", resourcesCase{
				component: "kubernautAgent",
				newKn:     testKubernaut,
				setRes: func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements) {
					kn.Spec.KubernautAgent.Resources = res
				},
				build: func(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
					return KubernautAgentDeployment(kn, testKnV2(kn))
				},
			}),
			Entry("remediationOrchestrator", resourcesCase{
				component: "remediationOrchestrator",
				newKn:     testKubernaut,
				setRes: func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements) {
					kn.Spec.RemediationOrchestrator.Resources = res
				},
				build: func(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
					return RemediationOrchestratorDeployment(kn, testKnV2(kn))
				},
			}),
			Entry("dataStorage", resourcesCase{
				component: "dataStorage",
				newKn:     testKubernaut,
				setRes: func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements) {
					kn.Spec.DataStorage.Resources = res
				},
				build: DataStorageDeployment,
			}),
			Entry("workflowExecution", resourcesCase{
				component: "workflowExecution",
				newKn:     testKubernaut,
				setRes: func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements) {
					kn.Spec.WorkflowExecution.Resources = res
				},
				build: func(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
					return WorkflowExecutionDeployment(kn, testKnV2(kn))
				},
			}),
			Entry("notification", resourcesCase{
				component: "notification",
				newKn:     testKubernaut,
				setRes: func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements) {
					kn.Spec.Notification.Resources = res
				},
				build: func(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
					return NotificationDeployment(kn, testKnV2(kn))
				},
			}),
			Entry("signalProcessing", resourcesCase{
				component: "signalProcessing",
				newKn:     testKubernaut,
				setRes: func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements) {
					kn.Spec.SignalProcessing.Resources = res
				},
				build: func(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
					return SignalProcessingDeployment(kn, testKnV2(kn))
				},
			}),
			Entry("effectivenessMonitor", resourcesCase{
				component: "effectivenessMonitor",
				newKn:     testKubernaut,
				setRes: func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements) {
					kn.Spec.EffectivenessMonitor.Resources = res
				},
				build: func(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
					return EffectivenessMonitorDeployment(kn, testKnV2(kn))
				},
			}),
			Entry("aiAnalysis", resourcesCase{
				component: "aiAnalysis",
				newKn:     testKubernaut,
				setRes: func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements) {
					kn.Spec.AIAnalysis.Resources = res
				},
				build: func(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
					return AIAnalysisDeployment(kn, testKnV2(kn))
				},
			}),
			Entry("authWebhook", resourcesCase{
				component: "authWebhook",
				newKn:     testKubernaut,
				setRes: func(kn *kubernautv1alpha1.Kubernaut, res corev1.ResourceRequirements) {
					kn.Spec.AuthWebhook.Resources = res
				},
				build: func(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
					return AuthWebhookDeployment(kn, testKnV2(kn))
				},
			}),
		)

		// kaResources (KubernautAgentDeployment's resources resolver) has a
		// distinct, non-obvious merge rule worth its own regression test:
		// it is all-or-nothing on the *pair* (Requests, Limits), not a
		// per-field deep merge -- setting only one of the two still counts
		// as "user specified" and the other stays at its zero value rather
		// than falling back to the default for that half.
		DescribeTable("UT-RES-KA [CM-6]: kaResources() default-merge behavior",
			func(userSpec corev1.ResourceRequirements, expectRequestsCPU, expectLimitsCPU string) {
				kn := testKubernaut()
				kn.Spec.KubernautAgent.Resources = userSpec
				dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
				Expect(err).NotTo(HaveOccurred())
				got := dep.Spec.Template.Spec.Containers[0].Resources
				Expect(got.Requests.Cpu().String()).To(Equal(expectRequestsCPU))
				Expect(got.Limits.Cpu().String()).To(Equal(expectLimitsCPU))
			},
			Entry("unset -> documented defaults (200m request / 1000m limit)",
				corev1.ResourceRequirements{}, "200m", "1"),
			Entry("fully specified -> verbatim passthrough",
				corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("300m")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("900m")},
				}, "300m", "900m"),
			Entry("partially specified (Requests only) -> still verbatim, no per-field default merge for Limits",
				corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("300m")},
				}, "300m", "0"),
		)
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
				if v.Name == testVolumeSecrets && v.Projected != nil {
					found = true
					Expect(v.Projected.Sources).To(HaveLen(2))
				}
			}
			Expect(found).To(BeTrue(), "DataStorage should have a 'secrets' projected volume")
		})

		// #423 coverage backfill: valkey.secretName had no test asserting
		// the actual configured name reaches the projected volume (the test
		// above only checks source count, using the default fixture value).
		It("[IA-5, SC-28] propagates spec.valkey.secretName into the projected secrets volume", func() {
			kn := testKubernaut()
			kn.Spec.Valkey.SecretName = "custom-valkey-secret"
			dep, err := DataStorageDeployment(kn)
			Expect(err).NotTo(HaveOccurred())

			var names []string
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == testVolumeSecrets && v.Projected != nil {
					for _, src := range v.Projected.Sources {
						if src.Secret != nil {
							names = append(names, src.Secret.Name)
						}
					}
				}
			}
			Expect(names).To(ContainElement("custom-valkey-secret"), "spec.valkey.secretName should be projected into the 'secrets' volume, got sources: %v", names)
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
			dep, err := AIAnalysisDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			expectDeploymentBasics(dep, "aianalysis")
			expectHasVolume(dep, "rego-policies")
			expectVolumeSourceConfigMap(dep, "rego-policies", "aianalysis-policy")
			expectHasVolumeMount(dep, "rego-policies", "/etc/aianalysis/policies")
		})
	})

	Context("SignalProcessing", func() {
		It("has policy mount", func() {
			kn := testKubernaut()
			dep, err := SignalProcessingDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "policy")
			expectVolumeSourceConfigMap(dep, "policy", "signalprocessing-policy")
			expectHasVolumeMount(dep, "policy", "/etc/signalprocessing/policies")
		})

		It("uses custom proactive signal mappings", func() {
			kn := testKubernaut()
			kn.Spec.SignalProcessing.ProactiveSignalMappings = &kubernautv1alpha1.ConfigMapRef{ConfigMapName: "my-mappings"}
			dep, err := SignalProcessingDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "proactive-mappings")
			expectHasVolumeMount(dep, "proactive-mappings", "/etc/signalprocessing/proactive-signal-mappings.yaml")
		})

		It("uses default proactive signal mappings", func() {
			kn := testKubernaut()
			dep, err := SignalProcessingDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "proactive-mappings")
			expectVolumeSourceConfigMap(dep, "proactive-mappings", "signalprocessing-proactive-signal-mappings")
			expectHasVolumeMount(dep, "proactive-mappings", "/etc/signalprocessing/proactive-signal-mappings.yaml")
		})
	})

	Context("Notification", func() {
		It("[IA-5, SC-28] mounts Slack credentials when configured", func() {
			kn := testKubernaut()
			kn.Spec.Notification.Slack.SecretName = "slack-secret"
			dep, err := NotificationDeployment(kn, testKnV2(kn))
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
			dep, err := NotificationDeployment(kn, testKnV2(kn))
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
			dep, err := NotificationDeployment(kn, testKnV2(kn))
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
			dep, err := NotificationDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "routing-config")
			expectVolumeSourceConfigMap(dep, "routing-config", "notification-routing-config")
			expectHasVolumeMount(dep, "routing-config", "/etc/notification-routing")
		})

		It("uses BYO routing config map name", func() {
			kn := testKubernaut()
			kn.Spec.Notification.Routing = &kubernautv1alpha1.ConfigMapRef{ConfigMapName: "my-routing"}
			dep, err := NotificationDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			expectHasVolume(dep, "routing-config")
			expectVolumeSourceConfigMap(dep, "routing-config", "my-routing")
			expectHasVolumeMount(dep, "routing-config", "/etc/notification-routing")
		})
	})

	Context("KubernautAgent", func() {
		It("has LLM credentials volume", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			expectDeploymentBasics(dep, "kubernautagent")
			expectHasVolume(dep, "llm-credentials")
			expectHasVolumeMount(dep, "llm-credentials", "/etc/kubernaut-agent/credentials")
		})

		It("infers the sole spec.llmProfiles entry when kubernautAgent.llmProfileRef is empty", func() {
			kn := testKubernaut() // exactly one profile ("primary")
			kn.Spec.KubernautAgent.LLMProfileRef = ""
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "llm-credentials")
			found := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "llm-credentials" {
					found = true
					Expect(v.Secret).NotTo(BeNil())
					Expect(v.Secret.SecretName).To(Equal("llm-creds"),
						"the sole profile's credentialsSecretName must be used, matching testKubernaut()'s \"primary\" profile")
				}
			}
			Expect(found).To(BeTrue(), "llm-credentials volume not found")
		})

		It("#404 [SC-8]: declares SSL_CERT_FILE exactly once, pointing at KA's own merged (system+service-ca+router) bundle -- not the narrower inter-service-only bundle", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			container := dep.Spec.Template.Spec.Containers[0]

			var sslCertValues []string
			for _, e := range container.Env {
				if e.Name == testEnvSSLCertFile {
					sslCertValues = append(sslCertValues, e.Value)
				}
			}
			Expect(sslCertValues).To(HaveLen(1),
				"#404: SSL_CERT_FILE must be declared exactly once -- a second declaration silently shadows the first at runtime (confirmed via the API server's own admission warning), and Go's SSL_CERT_FILE replaces rather than extends the system trust store, so whichever value wins must be a superset of everything KA needs (system/public CAs for LLM providers + service-ca/router-CA for the fleet MCP client)")
			Expect(sslCertValues[0]).To(Equal("/etc/ssl/combined/ca-bundle.crt"),
				"#404: KA's own merged bundle (built by the build-ca-bundle init container) must win, not the narrower inter-service-only bundle")

			// TLS_CA_FILE keeps its narrower, intentionally-scoped value --
			// unaffected by this fix, unlike SSL_CERT_FILE above.
			hasTLSCAFile := false
			for _, e := range container.Env {
				if e.Name == testEnvTLSCAFile && e.Value == InterServiceTLSCAFile {
					hasTLSCAFile = true
				}
			}
			Expect(hasTLSCAFile).To(BeTrue(), "TLS_CA_FILE must remain set to the narrower inter-service bundle")
		})

		It("#404 [SC-8]: build-ca-bundle init container merges system CA with the router-inclusive trust bundle (tls-ca), not the narrower service-ca-only bundle", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			var initContainer *corev1.Container
			for i := range dep.Spec.Template.Spec.InitContainers {
				if dep.Spec.Template.Spec.InitContainers[i].Name == "build-ca-bundle" {
					initContainer = &dep.Spec.Template.Spec.InitContainers[i]
				}
			}
			Expect(initContainer).NotTo(BeNil(), "build-ca-bundle init container not found")

			Expect(initContainer.Command).To(ContainElement(ContainSubstring("/etc/tls-ca/service-ca.crt")),
				"#404: init container must read the router-inclusive trust bundle (mounted from tls-ca/TrustBundleConfigMapName), not the narrower kubernaut-agent-service-ca-only bundle")
			Expect(initContainer.Command).NotTo(ContainElement(ContainSubstring("/service-ca/service-ca.crt")),
				"#404: must no longer read the narrower service-ca-only bundle -- it's a strict subset of the tls-ca bundle now used")

			const tlsCAMountPath = "/etc/tls-ca"
			foundMount := false
			for _, vm := range initContainer.VolumeMounts {
				if vm.Name == testVolumeTLSCA && vm.MountPath == tlsCAMountPath {
					foundMount = true
				}
			}
			Expect(foundMount).To(BeTrue(), "build-ca-bundle init container must mount the tls-ca volume at %s", tlsCAMountPath)
		})

		It("KFG-022 [IA-5]: mounts a dedicated phase-credentials Secret volume when a phase's profile has a different credentialsSecretName than KA's (#233)", func() {
			kn := testKubernaut()
			kn.Spec.LLMProfiles["workflow-cross-cred"] = kubernautv1alpha1.LLMProfileSpec{
				Provider:              "anthropic",
				Model:                 "claude-haiku-4-6",
				CredentialsSecretName: "different-secret",
			}
			kn.Spec.KubernautAgent.PhaseModels = map[string]string{"workflow_discovery": "workflow-cross-cred"}
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "phase-credentials-workflow_discovery")
			expectHasVolumeMount(dep, "phase-credentials-workflow_discovery", "/etc/kubernaut-agent/phase-credentials/workflow_discovery")
			found := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "phase-credentials-workflow_discovery" {
					found = true
					Expect(v.Secret).NotTo(BeNil())
					Expect(v.Secret.SecretName).To(Equal("different-secret"),
						"#233: dedicated phase volume must mount the phase's own profile's Secret, not KA's")
				}
			}
			Expect(found).To(BeTrue(), "phase-credentials-workflow_discovery volume not found")
		})

		It("KFG-023 [IA-5]: does not mount a dedicated phase-credentials volume when a phase shares KA's credentialsSecretName (regression guard, #233)", func() {
			kn := testKubernaut()
			kn.Spec.LLMProfiles["workflow-lite"] = kubernautv1alpha1.LLMProfileSpec{
				Provider:              "openai",
				Model:                 "gpt-4o-mini",
				Endpoint:              testOpenAIEndpoint,
				CredentialsSecretName: "llm-creds", // same as testKubernaut()'s "primary" profile
			}
			kn.Spec.KubernautAgent.PhaseModels = map[string]string{"validation": "workflow-lite"}
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(Equal("phase-credentials-validation"),
					"#233: a phase sharing KA's credentialsSecretName must keep using the already-mounted llm-credentials volume")
			}
		})

		It("KFG-024 [IA-5]: mounts phase-credentials volumes in deterministic (sorted-by-phase) order across multiple cross-credential phase overrides (#233)", func() {
			kn := testKubernaut()
			kn.Spec.LLMProfiles["rca-vertex"] = kubernautv1alpha1.LLMProfileSpec{
				Provider: LLMProviderVertexAI, Model: "gemini-2.5-flash",
				CredentialsSecretName: "secret-a",
				VertexProject:         "example-gcp-project", VertexLocation: "us-central1",
			}
			kn.Spec.LLMProfiles["workflow-cross-cred"] = kubernautv1alpha1.LLMProfileSpec{
				Provider: "anthropic", Model: "claude-haiku-4-6",
				CredentialsSecretName: "secret-b",
			}
			kn.Spec.KubernautAgent.PhaseModels = map[string]string{
				"workflow_discovery": "workflow-cross-cred",
				"rca":                "rca-vertex",
			}
			for i := 0; i < 15; i++ {
				dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
				Expect(err).NotTo(HaveOccurred())
				var phaseVolumeNames []string
				for _, v := range dep.Spec.Template.Spec.Volumes {
					if strings.HasPrefix(v.Name, "phase-credentials-") {
						phaseVolumeNames = append(phaseVolumeNames, v.Name)
					}
				}
				Expect(phaseVolumeNames).To(Equal([]string{"phase-credentials-rca", "phase-credentials-workflow_discovery"}),
					"#233: phase-credentials volumes must be sorted by phase name for deterministic Deployment diffs, iteration %d", i)
			}
		})

		It("passes config args", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
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
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
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
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.Enabled = true })
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.CredentialsSecretRef = "oauth2-credentials-secret" })
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
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
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			expectHasVolume(dep, "service-ca")
			expectHasVolumeMount(dep, "service-ca", "/etc/ssl/ka")
		})

		It("sets IS_OPENSHIFT env when monitoring enabled", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
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

		It("has TLS cert volume", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			expectHasVolume(dep, "tls-certs")
			expectHasVolumeMount(dep, "tls-certs", InterServiceTLSCertDir)
		})

		It("mounts emptyDir /tmp for readOnlyRootFilesystem", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			expectHasVolume(dep, "tmp")
			expectHasVolumeMount(dep, "tmp", "/tmp")
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "tmp" {
					Expect(v.EmptyDir).NotTo(BeNil())
				}
			}
		})

		It("sets terminationGracePeriodSeconds to drainSeconds + 5 (default 30)", func() {
			kn := testKubernaut()
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			Expect(dep.Spec.Template.Spec.TerminationGracePeriodSeconds).NotTo(BeNil())
			Expect(*dep.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(35)),
				"default drainSeconds=30 + 5 buffer = 35")
		})

		It("adjusts terminationGracePeriodSeconds for custom drainSeconds", func() {
			kn := testKubernaut()
			drain := 120
			kn.Spec.KubernautAgent.Shutdown.DrainSeconds = &drain
			dep, err := KubernautAgentDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			Expect(*dep.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(125)))
		})

		Context("Fleet OAuth2 credentials mount (#204)", func() {
			It("KFG-020 [IA-5]: no fleet-oauth2 volume/mount when fleet OAuth2 is disabled", func() {
				kn, knV2 := testKubernautWithFleetMCP()
				dep, err := KubernautAgentDeployment(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				for _, v := range dep.Spec.Template.Spec.Volumes {
					Expect(v.Name).NotTo(Equal(testVolumeFleetOAuth2), "should not mount fleet-oauth2 when fleet OAuth2 is disabled")
				}
			})

			It("KFG-021 [IA-5]: mounts the fleet-oauth2 Secret at the unhyphenated /etc/kubernautagent path KA's registerFleetTools() hardcodes, not /etc/kubernaut-agent", func() {
				kn, knV2 := testKubernautWithFleetMCP()
				knV2.Spec.Fleet.OAuth2 = kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				}
				dep, err := KubernautAgentDeployment(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				expectHasVolume(dep, testVolumeFleetOAuth2)
				expectHasVolumeMount(dep, testVolumeFleetOAuth2, "/etc/kubernautagent/fleet-oauth2-creds")
				for _, v := range dep.Spec.Template.Spec.Volumes {
					if v.Name == testVolumeFleetOAuth2 {
						Expect(v.Secret).NotTo(BeNil())
						Expect(v.Secret.SecretName).To(Equal("fleet-oauth2-creds"))
					}
				}
			})

			It("KFG-021b [IA-5]: uses KA's own FleetOAuth2CredentialsSecretRef override for the mount when set, not the shared credentialsSecretRef", func() {
				kn, knV2 := testKubernautWithFleetMCP()
				knV2.Spec.Fleet.OAuth2 = kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "shared-fleet-oauth2-creds",
				}
				knV2.Spec.KubernautAgent.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: "ka-oauth2-creds"}
				dep, err := KubernautAgentDeployment(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				expectHasVolume(dep, testVolumeFleetOAuth2)
				expectHasVolumeMount(dep, testVolumeFleetOAuth2, "/etc/kubernautagent/ka-oauth2-creds")
				for _, v := range dep.Spec.Template.Spec.Volumes {
					if v.Name == testVolumeFleetOAuth2 {
						Expect(v.Secret).NotTo(BeNil())
						Expect(v.Secret.SecretName).To(Equal("ka-oauth2-creds"))
					}
				}
			})
		})
	})

	Context("EffectivenessMonitor", func() {
		It("has service-ca volume", func() {
			kn := testKubernaut()
			dep, err := EffectivenessMonitorDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			expectHasVolume(dep, "service-ca")
		})

		It("has wait-for-service-ca init container when monitoring enabled", func() {
			kn := testKubernaut()
			dep, err := EffectivenessMonitorDeployment(kn, testKnV2(kn))
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

	})

	Context("AuthWebhook", func() {
		It("has TLS and webhook port", func() {
			kn := testKubernaut()
			dep, err := AuthWebhookDeployment(kn, testKnV2(kn))
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
			dep, err := AuthWebhookDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			Expect(dep.Spec.Strategy.Type).To(Equal(appsv1.RecreateDeploymentStrategyType))
		})
	})

	Context("WorkflowExecution AAP CA cert", func() {
		It("has no init container without caCertSecretRef", func() {
			kn := testKubernaut()
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
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

		It("overrides SSL_CERT_FILE to the combined-CA path alongside TLS_CA_FILE", func() {
			// overrideTLSCAFile keeps SSL_CERT_FILE in sync with
			// TLS_CA_FILE (both must point at the AAP+inter-service
			// combined bundle, not just the inter-service one) so the Go
			// runtime's system cert pool also trusts the AAP CA.
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			container := dep.Spec.Template.Spec.Containers[0]
			found := false
			for _, e := range container.Env {
				if e.Name == testEnvSSLCertFile {
					found = true
					Expect(e.Value).To(Equal("/etc/combined-ca/ca-bundle.crt"))
				}
			}
			Expect(found).To(BeTrue(), "WFE must set SSL_CERT_FILE env var")
		})

		It("has init container with caCertSecretRef", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			Expect(dep.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(dep.Spec.Template.Spec.InitContainers[0].Name).To(Equal("build-ca-bundle"))
		})

		It("[IA-5, SC-12] mounts secret volume with custom key", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{
				Name: "aap-ca-secret",
				Key:  "custom-ca.pem",
			}
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
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

		It("[IA-5, SC-12] uses default key ca.crt", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.CACertSecretRef = &kubernautv1alpha1.CACertSecretRef{Name: "aap-ca-secret"}
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
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
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
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
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
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
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
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
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
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
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
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
			dep, err := WorkflowExecutionDeployment(kn, testKnV2(kn))
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

	// #235/DD-235: WE's own write-scoped fleet-oauth2 mount must never fall
	// back to the shared spec.fleet.oauth2.credentialsSecretRef the way
	// SP/AF/EM's appendMCPGatewayOnlyFleetSecretMount does.
	Context("WorkflowExecution fleet OAuth2 secret mount", func() {
		It("has no fleet-oauth2 volume when spec.fleet.enabled is false", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
			dep, err := WorkflowExecutionDeployment(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(Equal(testVolumeFleetOAuth2), "WE should not have a fleet-oauth2 volume when fleet is disabled")
			}
		})

		It("has no fleet-oauth2 volume when WE's own oauth2CredentialsSecretRef is unset, even though fleet+oauth2 are enabled", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			enabled := true
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			dep, err := WorkflowExecutionDeployment(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(Equal(testVolumeFleetOAuth2),
					"[AC-6] WE must never mount the shared fleet-oauth2-creds Secret in place of its own unset credential")
			}
		})

		It("[AC-6] mounts only WE's own write-scoped credential Secret, never the shared one, at a path distinct from every other component", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			enabled := true
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
			dep, err := WorkflowExecutionDeployment(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			expectHasVolume(dep, testVolumeFleetOAuth2)
			expectHasVolumeMount(dep, testVolumeFleetOAuth2, "/etc/workflowexecution/"+testWEFleetOAuth2SecretRef)
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == testVolumeFleetOAuth2 {
					Expect(v.Secret).NotTo(BeNil())
					Expect(v.Secret.SecretName).To(Equal(testWEFleetOAuth2SecretRef),
						"[AC-6] WE must mount its own write-scoped Secret, never the shared fleet-oauth2-creds one")
				}
			}
		})
	})

	Context("overrideTLSCAFile helper", func() {
		It("replaces existing TLS_CA_FILE and SSL_CERT_FILE", func() {
			env := []corev1.EnvVar{
				{Name: "OTHER", Value: "foo"},
				{Name: testEnvTLSCAFile, Value: "/old/path"},
				{Name: testEnvSSLCertFile, Value: "/old/path"},
			}
			result := overrideTLSCAFile(env, "/new/path")
			for _, name := range []string{testEnvTLSCAFile, testEnvSSLCertFile} {
				found := false
				for _, e := range result {
					if e.Name == name {
						found = true
						Expect(e.Value).To(Equal("/new/path"))
					}
				}
				Expect(found).To(BeTrue(), "%s not found after override", name)
			}
		})

		It("appends both TLS_CA_FILE and SSL_CERT_FILE when missing", func() {
			env := []corev1.EnvVar{{Name: "OTHER", Value: "foo"}}
			result := overrideTLSCAFile(env, "/new/path")
			Expect(result).To(HaveLen(3))
			for _, name := range []string{testEnvTLSCAFile, testEnvSSLCertFile} {
				found := false
				for _, e := range result {
					if e.Name == name && e.Value == "/new/path" {
						found = true
					}
				}
				Expect(found).To(BeTrue(), "%s should be appended when missing", name)
			}
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

		It("#267: sets startupProbe on fleet-aware components with DD-PLATFORM-008 thresholds (305s grace)", func() {
			kn := testKubernaut()
			fleetAware := map[string]bool{
				ComponentGateway:                 true,
				ComponentSignalProcessing:        true,
				ComponentRemediationOrchestrator: true,
				ComponentWorkflowExecution:       true,
				ComponentEffectivenessMonitor:    true,
			}

			for _, dep := range getAllDeployments(kn) {
				container := dep.Spec.Template.Spec.Containers[0]
				component := dep.Spec.Template.Labels["app"]

				if !fleetAware[component] {
					Expect(container.StartupProbe).To(BeNil(), "Deployment %q is not fleet-aware and should not have a startupProbe", dep.Name)
					continue
				}

				sp := container.StartupProbe
				Expect(sp).NotTo(BeNil(), "Deployment %q is fleet-aware and needs a startupProbe (DD-PLATFORM-008 cold-start budget)", dep.Name)
				Expect(sp.HTTPGet).NotTo(BeNil(), "Deployment %q startupProbe should use HTTPGet", dep.Name)
				Expect(sp.HTTPGet.Path).To(Equal("/healthz"), "%s startupProbe path", dep.Name)
				Expect(sp.InitialDelaySeconds).To(Equal(int32(5)), "%s startupProbe InitialDelaySeconds", dep.Name)
				Expect(sp.PeriodSeconds).To(Equal(int32(5)), "%s startupProbe PeriodSeconds", dep.Name)
				Expect(sp.TimeoutSeconds).To(Equal(int32(5)), "%s startupProbe TimeoutSeconds", dep.Name)
				Expect(sp.FailureThreshold).To(Equal(int32(60)), "%s startupProbe FailureThreshold (60x5s=300s+5s initial=305s grace)", dep.Name)
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

		// #406 (BR-PLATFORM-012, AC-6): the 7 controller-runtime-managed
		// services (AIAnalysis, AuthWebhook, EffectivenessMonitor,
		// Notification, RemediationOrchestrator, SignalProcessing,
		// WorkflowExecution) start ctrl.Options.PprofBindAddress's listener
		// on :6060 only when the single shared spec.debug.pprofEnabled
		// (KubernautSpec root) is true -- there is no per-component
		// override anymore (DD-406). The containerPort must track that one
		// toggle exactly: absent by default (secure-by-default), present
		// on every ctrl-runtime service simultaneously once opted in.
		DescribeTable("debug.pprofEnabled conditionally exposes a pprof containerPort on ctrl-runtime services (#406)",
			func(fn func(*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error)) {
				kn := testKubernaut()
				knV2 := testKnV2(kn)

				depOff, err := fn(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				Expect(hasPprofContainerPort(depOff)).To(BeFalse(),
					"Deployment %q should NOT expose the pprof port by default", depOff.Name)

				knV2.Spec.Debug.PprofEnabled = true
				depOn, err := fn(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				Expect(hasPprofContainerPort(depOn)).To(BeTrue(),
					"Deployment %q should expose containerPort 6060 named %q after the global debug.pprofEnabled=true", depOn.Name, "pprof")
			},
			Entry("aianalysis", AIAnalysisDeployment),
			Entry("signalprocessing", SignalProcessingDeployment),
			Entry("remediationorchestrator", RemediationOrchestratorDeployment),
			Entry("workflowexecution", WorkflowExecutionDeployment),
			Entry("effectivenessmonitor", EffectivenessMonitorDeployment),
			Entry("notification", NotificationDeployment),
			Entry("authwebhook", AuthWebhookDeployment),
		)

		// #406: a single spec.debug.pprofEnabled=true must expose the pprof
		// port on all 7 ctrl-runtime-managed deployments simultaneously --
		// this is the business-level guarantee the global toggle exists to
		// provide (the previous per-component design required setting the
		// same boolean on up to 7 different component paths to get this
		// same effect). One CR mutation is enough.
		It("enables the pprof containerPort on all 7 ctrl-runtime deployments simultaneously from one root-level flag (#406)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.Debug.PprofEnabled = true

			builders := map[string]func(*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error){
				"aianalysis":              AIAnalysisDeployment,
				"signalprocessing":        SignalProcessingDeployment,
				"remediationorchestrator": RemediationOrchestratorDeployment,
				"workflowexecution":       WorkflowExecutionDeployment,
				"effectivenessmonitor":    EffectivenessMonitorDeployment,
				"notification":            NotificationDeployment,
				"authwebhook":             AuthWebhookDeployment,
			}
			for component, fn := range builders {
				dep, err := fn(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				Expect(hasPprofContainerPort(dep)).To(BeTrue(),
					"expected %s to expose the pprof port after enabling the single global toggle once", component)
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
					if v.Name == testVolumeTLSCA && v.ConfigMap != nil && v.ConfigMap.Name == TrustBundleConfigMapName {
						hasCAVolume = true
					}
				}
				Expect(hasCAVolume).To(BeTrue(), "Deployment %q missing %s volume sourced from the trust-bundle ConfigMap", dep.Name, testVolumeTLSCA)

				hasCAMount := false
				for _, vm := range container.VolumeMounts {
					if vm.Name == testVolumeTLSCA && vm.MountPath == "/etc/tls-ca" {
						hasCAMount = true
					}
				}
				Expect(hasCAMount).To(BeTrue(), "Deployment %q missing %s volume mount", dep.Name, testVolumeTLSCA)

				// #404: KubernautAgent is the sole exception -- its
				// SSL_CERT_FILE must point at its own merged (system+
				// inter-service) bundle, a strict superset of
				// InterServiceTLSCAFile, since KA also verifies public-CA
				// LLM providers via the same global trust store. TLS_CA_FILE
				// stays at the narrower InterServiceTLSCAFile for everyone,
				// KA included.
				wantSSLCertFile := InterServiceTLSCAFile
				if dep.Spec.Template.Labels["app"] == ComponentKubernautAgent {
					wantSSLCertFile = "/etc/ssl/combined/ca-bundle.crt"
				}

				hasCAEnv := false
				hasSSLCertEnv := false
				for _, env := range container.Env {
					if env.Name == "TLS_CA_FILE" && env.Value == InterServiceTLSCAFile {
						hasCAEnv = true
					}
					if env.Name == testEnvSSLCertFile && env.Value == wantSSLCertFile {
						hasSSLCertEnv = true
					}
				}
				Expect(hasCAEnv).To(BeTrue(), "Deployment %q missing TLS_CA_FILE env var", dep.Name)
				Expect(hasSSLCertEnv).To(BeTrue(), "Deployment %q missing SSL_CERT_FILE=%s env var (workaround for kubernaut#TBD: MCP client base transport doesn't honor a custom CA)", dep.Name, wantSSLCertFile)
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
		Expect(result).To(HaveLen(3))
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
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		expectDeploymentBasics(dep, "apifrontend")
	})

	It("exposes HTTPS (8443), health (8081), and metrics (9090) ports", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
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

	// #423 coverage backfill: valkey.secretName had zero test references for
	// the AF deployment's own valkey-secrets volume (its mount is gated only
	// on SecretName != "", and testKubernautWithAF's fixture always sets one).
	It("[IA-5, SC-28] mounts valkey-secrets volume from spec.valkey.secretName", func() {
		kn := testKubernautWithAF()
		kn.Spec.Valkey.SecretName = "custom-af-valkey-secret"
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(dep, "valkey-secrets")
		expectHasVolumeMount(dep, "valkey-secrets", "/etc/apifrontend/valkey")
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "valkey-secrets" {
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal("custom-af-valkey-secret"))
			}
		}
	})

	It("mounts config, tls-server, tls-ca, and tmp volumes", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
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

	It("#404 [SC-8]: SSL_CERT_FILE points at a merged system+inter-service bundle built by a build-ca-bundle init container, not the narrower router/service-ca-only bundle", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]

		var sslCertValues []string
		var tlsCAValue string
		for _, e := range container.Env {
			if e.Name == testEnvSSLCertFile {
				sslCertValues = append(sslCertValues, e.Value)
			}
			if e.Name == testEnvTLSCAFile {
				tlsCAValue = e.Value
			}
		}
		Expect(sslCertValues).To(HaveLen(1), "SSL_CERT_FILE must be declared exactly once")
		Expect(sslCertValues[0]).To(Equal("/etc/ssl/combined/ca-bundle.crt"),
			"#404: AF's severityTriage/LLM calls to public-CA providers need system CAs, which the narrower /etc/apifrontend/tls-ca/ca.crt bundle (service-ca+router only) lacks")
		Expect(tlsCAValue).To(Equal("/etc/apifrontend/tls-ca/ca.crt"),
			"TLS_CA_FILE keeps its narrower, intentionally-scoped inter-service value -- unaffected by this fix")

		expectHasVolume(dep, testVolumeCombinedCA)
		expectHasVolumeMount(dep, testVolumeCombinedCA, "/etc/ssl/combined")

		var initContainer *corev1.Container
		for i := range dep.Spec.Template.Spec.InitContainers {
			if dep.Spec.Template.Spec.InitContainers[i].Name == "build-ca-bundle" {
				initContainer = &dep.Spec.Template.Spec.InitContainers[i]
			}
		}
		Expect(initContainer).NotTo(BeNil(), "AF must have a build-ca-bundle init container, mirroring KA's pattern")
		Expect(initContainer.Command).To(ContainElement(ContainSubstring("/etc/apifrontend/tls-ca/ca.crt")),
			"init container must merge the inter-service trust bundle (mounted at AF's existing tls-ca path) into the combined bundle")
		Expect(initContainer.Command).To(ContainElement(ContainSubstring("/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem")),
			"init container must also merge the base image's system CA bundle")
	})

	It("mounts llm-credentials volume from AF's own resolved profile", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles[testAFOnlyProfile] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              LLMProviderVertexAI,
			Model:                 "gemini-2.5-flash",
			CredentialsSecretName: "af-llm-creds",
		}
		kn.Spec.APIFrontend.LLMProfileRef = testAFOnlyProfile
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())

		expectHasVolume(dep, "llm-credentials")
		expectHasVolumeMount(dep, "llm-credentials", "/etc/apifrontend/llm-credentials")
		found := false
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "llm-credentials" {
				found = true
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal("af-llm-creds"), "AF must mount its own profile's secret, not KA's (llm-creds)")
			}
		}
		Expect(found).To(BeTrue(), "llm-credentials volume not found")
	})

	It("KFG-025 [IA-5]: mounts a dedicated severity-triage-credentials Secret volume when severityTriage's profile has a different credentialsSecretName than AF's own (#234)", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["triage-other-creds"] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              "anthropic",
			Model:                 "claude-haiku-4-6",
			CredentialsSecretName: "different-secret",
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{LLMProfileRef: "triage-other-creds"}
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())

		expectHasVolume(dep, "severity-triage-credentials")
		expectHasVolumeMount(dep, "severity-triage-credentials", "/etc/apifrontend/severity-triage-credentials")
		found := false
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "severity-triage-credentials" {
				found = true
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal("different-secret"),
					"#234: dedicated severity-triage volume must mount severityTriage's own profile's Secret, not AF's")
			}
		}
		Expect(found).To(BeTrue(), "severity-triage-credentials volume not found")
	})

	It("KFG-026 [IA-5]: does not mount a dedicated severity-triage-credentials volume when severityTriage shares AF's credentialsSecretName (regression guard, #234)", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["triage-shared-creds"] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              "anthropic",
			Model:                 "claude-haiku-4-6",
			CredentialsSecretName: "llm-creds", // same as testKubernaut()'s "primary" profile
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{LLMProfileRef: "triage-shared-creds"}
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())

		for _, v := range dep.Spec.Template.Spec.Volumes {
			Expect(v.Name).NotTo(Equal("severity-triage-credentials"),
				"#234: a severityTriage profile sharing AF's credentialsSecretName must keep using the already-mounted llm-credentials volume")
		}
	})

	It("KFG-027 [IA-5]: mounts the dedicated severity-triage-credentials Secret without redirecting GOOGLE_APPLICATION_CREDENTIALS when severityTriage's profile is vertex_ai with a different credentialsSecretName than AF's non-vertex_ai own profile (#279, kubernaut#1731 is fixed)", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles[testAFOnlyProfile] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              LLMProviderOpenAI,
			Model:                 "gpt-4o",
			Endpoint:              testOpenAIEndpoint,
			CredentialsSecretName: "af-llm-creds",
		}
		kn.Spec.APIFrontend.LLMProfileRef = testAFOnlyProfile
		kn.Spec.LLMProfiles["triage-vertex"] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              LLMProviderVertexAI,
			Model:                 "gemini-2.5-flash",
			CredentialsSecretName: "triage-vertex-creds",
			VertexProject:         "example-gcp-project", VertexLocation: "us-central1",
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{LLMProfileRef: "triage-vertex"}
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())

		expectHasVolume(dep, "severity-triage-credentials")
		expectHasVolumeMount(dep, "severity-triage-credentials", "/etc/apifrontend/severity-triage-credentials")
		container := dep.Spec.Template.Spec.Containers[0]
		var gacValue string
		gacCount := 0
		for _, e := range container.Env {
			if e.Name == "GOOGLE_APPLICATION_CREDENTIALS" {
				gacCount++
				gacValue = e.Value
			}
		}
		Expect(gacCount).To(Equal(1), "GOOGLE_APPLICATION_CREDENTIALS must be set exactly once, from AF's own profile only")
		Expect(gacValue).To(Equal("/etc/apifrontend/llm-credentials/credentials.json"), // pre-commit:allow-sensitive -- mount-path convention constant, not a real credential/secret value
			"#279: severityTriage's own vertex_ai credentials now flow through its dedicated apiKeyFile (rendered in the ConfigMap, resolved via its own credentials.json mount) instead of a process-wide GOOGLE_APPLICATION_CREDENTIALS redirect")
	})

	It("[IA-5, SC-12] mounts llm-tls-client volume from AF's own resolved profile's tlsClientSecretRef", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles[testAFOnlyProfile] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              LLMProviderOpenAI,
			Model:                 "gpt-4o",
			Endpoint:              testOpenAIEndpoint,
			CredentialsSecretName: "af-llm-creds",
			TLSCertFile:           testMTLSCertFile,
			TLSKeyFile:            testMTLSKeyFile,
			TLSClientSecretRef:    "af-llm-tls-client",
		}
		kn.Spec.APIFrontend.LLMProfileRef = testAFOnlyProfile
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())

		expectHasVolume(dep, testVolumeLLMTLSClient)
		expectHasVolumeMount(dep, testVolumeLLMTLSClient, "/etc/apifrontend/llm-tls-client")
		found := false
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == testVolumeLLMTLSClient {
				found = true
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal("af-llm-tls-client"))
			}
		}
		Expect(found).To(BeTrue(), "llm-tls-client volume not found")
	})

	It("mounts OAuth2 credentials when enabled on AF's resolved profile (regression: pre-existing crash-loop)", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.Enabled = true })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
			p.OAuth2.CredentialsSecretRef = "af-oauth2-credentials-secret"
		})
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())

		expectHasVolume(dep, "oauth2-credentials")
		expectHasVolumeMount(dep, "oauth2-credentials", "/etc/apifrontend/oauth2")
		found := false
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "oauth2-credentials" {
				found = true
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal("af-oauth2-credentials-secret"))
			}
		}
		Expect(found).To(BeTrue(), "AF must mount an oauth2-credentials volume when its resolved profile has OAuth2 enabled -- "+
			"otherwise upstream AF hard-fails at startup reading client-id/client-secret from a directory nothing ever mounted")
	})

	It("omits oauth2-credentials volume when OAuth2 is not enabled", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		for _, v := range dep.Spec.Template.Spec.Volumes {
			Expect(v.Name).NotTo(Equal("oauth2-credentials"))
		}
	})

	It("sets liveness and readiness probes", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		Expect(container.LivenessProbe).NotTo(BeNil())
		Expect(container.ReadinessProbe).NotTo(BeNil())
		Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/healthz"))
		Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/readyz"))
	})

	It("#267: sets startupProbe with DD-PLATFORM-008 thresholds (fleet-aware, cold-start budget)", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		Expect(container.StartupProbe).NotTo(BeNil())
		Expect(container.StartupProbe.HTTPGet.Path).To(Equal("/healthz"))
		Expect(container.StartupProbe.InitialDelaySeconds).To(Equal(int32(5)))
		Expect(container.StartupProbe.PeriodSeconds).To(Equal(int32(5)))
		Expect(container.StartupProbe.TimeoutSeconds).To(Equal(int32(5)))
		Expect(container.StartupProbe.FailureThreshold).To(Equal(int32(60)))
	})

	It("includes Prometheus annotations", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		ann := dep.Spec.Template.Annotations
		Expect(ann["prometheus.io/scrape"]).To(Equal("true"))
		Expect(ann["prometheus.io/port"]).To(Equal("9090"))
	})

	It("ignores deprecated rbacRolesConfigMapRef (volume is plain ConfigMap)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBACRolesConfigMapRef = &kubernautv1alpha1.ConfigMapRef{ //nolint:staticcheck // exercising deprecated-field backward compat
			ConfigMapName: "my-custom-rbac",
		}
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
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
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		Expect(dep.Spec.Template.Spec.TerminationGracePeriodSeconds).NotTo(BeNil())
		Expect(*dep.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(20)),
			"default drainSeconds=15 + 5 buffer = 20")
	})

	It("adjusts terminationGracePeriodSeconds for custom drainSeconds", func() {
		kn := testKubernautWithAF()
		drain := 60
		kn.Spec.APIFrontend.Shutdown.DrainSeconds = &drain
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		Expect(*dep.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(65)))
	})

	It("uses plain ConfigMap volume, not projected", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
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
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
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

	It("AF container uses PortHTTPS when no sidecar is active", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		portMap := map[string]int32{}
		for _, p := range dep.Spec.Template.Spec.Containers[0].Ports {
			portMap[p.Name] = p.ContainerPort
		}
		Expect(portMap).To(HaveKeyWithValue("https", PortHTTPS))
	})

	It("AF container uses PortHTTPS for envoy sidecar (no port shift)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarEnvoy)
		Expect(err).NotTo(HaveOccurred())
		portMap := map[string]int32{}
		for _, p := range dep.Spec.Template.Spec.Containers[0].Ports {
			portMap[p.Name] = p.ContainerPort
		}
		Expect(portMap).To(HaveKeyWithValue("https", PortHTTPS),
			"envoy sidecar uses iptables; AF keeps original port")
	})

	It("AF container shifts to PortHTTPS+1 for authbridge sidecar", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarAuthbridge)
		Expect(err).NotTo(HaveOccurred())
		portMap := map[string]int32{}
		for _, p := range dep.Spec.Template.Spec.Containers[0].Ports {
			portMap[p.Name] = p.ContainerPort
		}
		Expect(portMap).To(HaveKeyWithValue("https", PortHTTPS),
			"AF declares 8443; kagenti webhook shifts AF to 8444 and authbridge takes 8443")
	})

	It("sets NO_PROXY for KA and DS with envoy sidecar", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarEnvoy)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		var noProxy string
		for _, e := range container.Env {
			if e.Name == "NO_PROXY" {
				noProxy = e.Value
			}
		}
		Expect(noProxy).To(ContainSubstring("kubernaut-agent.%s.svc.cluster.local", kn.Namespace),
			"NO_PROXY must include KA service to bypass sidecar for SA bearer token")
		Expect(noProxy).To(ContainSubstring("data-storage-service.%s.svc.cluster.local", kn.Namespace))
	})

	It("sets NO_PROXY for KA and DS with authbridge sidecar", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarAuthbridge)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		var noProxy string
		for _, e := range container.Env {
			if e.Name == "NO_PROXY" {
				noProxy = e.Value
			}
		}
		Expect(noProxy).To(ContainSubstring("kubernaut-agent.%s.svc.cluster.local", kn.Namespace))
	})

	It("omits NO_PROXY when no sidecar is active", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		for _, e := range container.Env {
			Expect(e.Name).NotTo(Equal("NO_PROXY"),
				"NO_PROXY should not be set when no sidecar is injected")
		}
	})

	It("does not set kagenti client-registration-inject label on pod template", func() {
		kn := testKubernautWithAF()
		dep, err := APIFrontendDeployment(kn, testKnV2(kn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		Expect(dep.Spec.Template.Labels).NotTo(HaveKey(KagentiClientRegistrationInjectLabel))
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
	It("[IA-5, SC-12] mounts signing cert when configured", func() {
		kn := testKubernaut()
		kn.Spec.DataStorage.SigningCert = &kubernautv1alpha1.SigningCertSpec{
			SecretName: "datastorage-signing-cert",
		}
		dep, err := DataStorageDeployment(kn)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(dep, "signing-cert")
		expectHasVolumeMount(dep, "signing-cert", "/etc/certs")
	})

	It("[SC-12] uses custom mount path when specified", func() {
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

var _ = Describe("Gateway and RemediationOrchestrator Fleet secret mounts", func() {
	enabled := true

	It("does not mount fleet-ca or fleet-token volumes when fleet is disabled", func() {
		kn := testKubernaut()
		gwDep, err := GatewayDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		roDep, err := RemediationOrchestratorDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		for _, dep := range []*appsv1.Deployment{gwDep, roDep} {
			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(HavePrefix("fleet-"),
					"%s should not have fleet volume %q when fleet is disabled", dep.Name, v.Name)
			}
		}
	})

	It("does not mount fleet-ca or fleet-token volumes when enabled but no secret names are set", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
		}
		gwDep, err := GatewayDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		roDep, err := RemediationOrchestratorDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		for _, dep := range []*appsv1.Deployment{gwDep, roDep} {
			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(HavePrefix("fleet-"),
					"%s should not have fleet volume %q when no secret names are set", dep.Name, v.Name)
			}
		}
	})

	It("[IA-5, SC-12] mounts fleet-ca on both Gateway and RemediationOrchestrator when caSecretName is set", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			CASecretName: "fmc-ca-bundle",
		}
		gwDep, err := GatewayDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		roDep, err := RemediationOrchestratorDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		for _, dep := range []*appsv1.Deployment{gwDep, roDep} {
			expectHasVolume(dep, "fleet-ca")
			expectHasVolumeMount(dep, "fleet-ca", "/etc/fleet-tls/ca")
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "fleet-ca" {
					Expect(v.Secret).NotTo(BeNil())
					Expect(v.Secret.SecretName).To(Equal("fmc-ca-bundle"))
				}
			}
		}
	})

	It("[IA-5] mounts fleet-token on both Gateway and RemediationOrchestrator when tokenSecretName is set", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "acm", Endpoint: "https://acm-search.example.com/graphql",
			TokenSecretName: "acm-search-token",
		}
		gwDep, err := GatewayDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		roDep, err := RemediationOrchestratorDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		for _, dep := range []*appsv1.Deployment{gwDep, roDep} {
			expectHasVolume(dep, "fleet-token")
			expectHasVolumeMount(dep, "fleet-token", "/etc/fleet-token")
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == "fleet-token" {
					Expect(v.Secret).NotTo(BeNil())
					Expect(v.Secret.SecretName).To(Equal("acm-search-token"))
				}
			}
		}
	})

	// #222: upstream GW/RO read the OAuth2 client-id/client-secret files from
	// "/etc/<component>/<credentialsSecretRef>/{client-id,client-secret}" —
	// each component needs its own mount under its own /etc/<component> tree.
	It("does not mount fleet-oauth2 volume when fleet oauth2 is disabled", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
		}
		gwDep, err := GatewayDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		roDep, err := RemediationOrchestratorDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		for _, dep := range []*appsv1.Deployment{gwDep, roDep} {
			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(Equal(testVolumeFleetOAuth2),
					"%s should not have a fleet-oauth2 volume when fleet.oauth2.enabled is false", dep.Name)
			}
		}
	})

	It("mounts fleet-oauth2 on Gateway at /etc/gateway/<credentialsSecretRef> when oauth2 is enabled", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled: true, TokenURL: "https://keycloak.example.com/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}
		gwDep, err := GatewayDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(gwDep, testVolumeFleetOAuth2)
		expectHasVolumeMount(gwDep, testVolumeFleetOAuth2, "/etc/gateway/fleet-oauth2-creds")
		for _, v := range gwDep.Spec.Template.Spec.Volumes {
			if v.Name == testVolumeFleetOAuth2 {
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal("fleet-oauth2-creds"))
			}
		}
	})

	It("mounts fleet-oauth2 on RemediationOrchestrator at /etc/remediationorchestrator/<credentialsSecretRef> when oauth2 is enabled", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled: true, TokenURL: "https://keycloak.example.com/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}
		roDep, err := RemediationOrchestratorDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(roDep, testVolumeFleetOAuth2)
		expectHasVolumeMount(roDep, testVolumeFleetOAuth2, "/etc/remediationorchestrator/fleet-oauth2-creds")
		for _, v := range roDep.Spec.Template.Spec.Volumes {
			if v.Name == testVolumeFleetOAuth2 {
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal("fleet-oauth2-creds"))
			}
		}
	})

	// A federated IdP issuing distinct per-service OAuth2 client
	// registrations (confirmed against upstream's own Helm chart) means
	// Gateway and RemediationOrchestrator must be able to mount *different*
	// Secrets, not the one shared fleet.oauth2.credentialsSecretRef.
	It("mounts fleet-oauth2 on Gateway using gateway.fleetOAuth2CredentialsSecretRef when set, not the shared credentialsSecretRef", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled: true, TokenURL: "https://keycloak.example.com/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}
		knV2.Spec.Gateway.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testGatewayFleetOAuth2SecretRef}
		gwDep, err := GatewayDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(gwDep, testVolumeFleetOAuth2)
		expectHasVolumeMount(gwDep, testVolumeFleetOAuth2, "/etc/gateway/gateway-oauth2-creds")
		for _, v := range gwDep.Spec.Template.Spec.Volumes {
			if v.Name == testVolumeFleetOAuth2 {
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal(testGatewayFleetOAuth2SecretRef))
			}
		}
	})

	It("mounts fleet-oauth2 on RemediationOrchestrator using remediationOrchestrator.fleetOAuth2CredentialsSecretRef when set, not the shared credentialsSecretRef", func() {
		kn := testKubernaut()
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled: true, TokenURL: "https://keycloak.example.com/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}
		knV2.Spec.RemediationOrchestrator.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testROFleetOAuth2SecretRef}
		roDep, err := RemediationOrchestratorDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(roDep, testVolumeFleetOAuth2)
		expectHasVolumeMount(roDep, testVolumeFleetOAuth2, "/etc/remediationorchestrator/ro-oauth2-creds")
		for _, v := range roDep.Spec.Template.Spec.Volumes {
			if v.Name == testVolumeFleetOAuth2 {
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal(testROFleetOAuth2SecretRef))
			}
		}
	})
})

// #224: SP/EM only ever consume the MCP Gateway remote-read path (never
// Backend/Endpoint), so they must never mount fleet-ca/fleet-token --
// those Secrets back Backend/Endpoint's ACM TLS CA / bearer token, which
// SP/EM's own upstream config schemas have no field for at all. #464: AF
// no longer belongs to this invariant -- upstream kubernaut#2025/#2022
// added a Backend/Endpoint scope-check adapter call to AF's own
// checkRRScope path, so AF now needs fleet-ca/fleet-token mounted just
// like Gateway/RemediationOrchestrator (see the dedicated AF Describe
// block below).
var _ = Describe("SignalProcessing/APIFrontend/EffectivenessMonitor Fleet secret mounts", func() {
	It("does not mount any fleet- volumes when fleet is disabled", func() {
		kn := testKubernaut()
		spDep, err := SignalProcessingDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		emDep, err := EffectivenessMonitorDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())
		afKn := testKubernautWithAF()
		afDep, err := APIFrontendDeployment(afKn, testKnV2(afKn), KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		for _, dep := range []*appsv1.Deployment{spDep, emDep, afDep} {
			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(HavePrefix("fleet-"),
					"%s should not have fleet volume %q when fleet is disabled", dep.Name, v.Name)
			}
		}
	})

	It("never mounts fleet-ca or fleet-token on SignalProcessing/EffectivenessMonitor even when caSecretName/tokenSecretName are set", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		knV2.Spec.Fleet.CASecretName = "fmc-ca-bundle"
		knV2.Spec.Fleet.TokenSecretName = "acm-search-token"
		spDep, err := SignalProcessingDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		emDep, err := EffectivenessMonitorDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		for _, dep := range []*appsv1.Deployment{spDep, emDep} {
			for _, v := range dep.Spec.Template.Spec.Volumes {
				Expect(v.Name).NotTo(Equal("fleet-ca"), "%s should never mount fleet-ca (Backend/Endpoint-only concern)", dep.Name)
				Expect(v.Name).NotTo(Equal("fleet-token"), "%s should never mount fleet-token (Backend/Endpoint-only concern)", dep.Name)
			}
		}
	})

	It("mounts fleet-oauth2 on SignalProcessing at /etc/signalprocessing/<credentialsSecretRef> when oauth2 is enabled", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		knV2.Spec.Fleet.OAuth2 = kubernautv1alpha2.OAuth2Spec{
			Enabled: true, TokenURL: "https://keycloak.example.com/token",
			CredentialsSecretRef: "fleet-oauth2-creds",
		}
		spDep, err := SignalProcessingDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(spDep, testVolumeFleetOAuth2)
		expectHasVolumeMount(spDep, testVolumeFleetOAuth2, "/etc/signalprocessing/fleet-oauth2-creds")
	})

	It("mounts fleet-oauth2 on APIFrontend at /etc/apifrontend/<credentialsSecretRef> when oauth2 is enabled", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		knV2.Spec.Fleet.OAuth2 = kubernautv1alpha2.OAuth2Spec{
			Enabled: true, TokenURL: "https://keycloak.example.com/token",
			CredentialsSecretRef: "fleet-oauth2-creds",
		}
		kn.Spec.APIFrontend = kubernautv1alpha1.APIFrontendSpec{
			Auth: kubernautv1alpha1.APIFrontendAuthSpec{IssuerURL: "https://login.kubernaut.ai/realms/kubernaut", Audience: "kubernaut-apifrontend"},
		}
		afDep, err := APIFrontendDeployment(kn, knV2, KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(afDep, testVolumeFleetOAuth2)
		expectHasVolumeMount(afDep, testVolumeFleetOAuth2, "/etc/apifrontend/fleet-oauth2-creds")
	})

	It("mounts fleet-oauth2 on EffectivenessMonitor at /etc/effectivenessmonitor/<credentialsSecretRef> when oauth2 is enabled", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		knV2.Spec.Fleet.OAuth2 = kubernautv1alpha2.OAuth2Spec{
			Enabled: true, TokenURL: "https://keycloak.example.com/token",
			CredentialsSecretRef: "fleet-oauth2-creds",
		}
		emDep, err := EffectivenessMonitorDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(emDep, testVolumeFleetOAuth2)
		expectHasVolumeMount(emDep, testVolumeFleetOAuth2, "/etc/effectivenessmonitor/fleet-oauth2-creds")
	})

	It("SignalProcessing uses its own fleetOAuth2CredentialsSecretRef override instead of the shared credentialsSecretRef", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		knV2.Spec.Fleet.OAuth2 = kubernautv1alpha2.OAuth2Spec{
			Enabled: true, TokenURL: "https://keycloak.example.com/token",
			CredentialsSecretRef: "fleet-oauth2-creds",
		}
		knV2.Spec.SignalProcessing.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testSPFleetOAuth2SecretRef}
		spDep, err := SignalProcessingDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolumeMount(spDep, testVolumeFleetOAuth2, "/etc/signalprocessing/"+testSPFleetOAuth2SecretRef)
	})
})

// #464: upstream kubernaut#2025/#2022 added a Backend/Endpoint scope-check
// adapter call to AF's own checkRRScope path, so AF now needs fleet-ca/
// fleet-token mounted just like Gateway/RemediationOrchestrator -- mirrors
// the GW/RO tests above exactly (see "Gateway/RemediationOrchestrator Fleet
// secret mounts" further up this file).
var _ = Describe("APIFrontend Fleet secret mounts (#464)", func() {
	It("[IA-5, SC-12] mounts fleet-ca on APIFrontend when caSecretName is set", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		knV2.Spec.Fleet.CASecretName = "fmc-ca-bundle"
		kn.Spec.APIFrontend = kubernautv1alpha1.APIFrontendSpec{
			Auth: kubernautv1alpha1.APIFrontendAuthSpec{IssuerURL: "https://login.kubernaut.ai/realms/kubernaut", Audience: "kubernaut-apifrontend"},
		}
		afDep, err := APIFrontendDeployment(kn, knV2, KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(afDep, "fleet-ca")
		expectHasVolumeMount(afDep, "fleet-ca", "/etc/fleet-tls/ca")
		for _, v := range afDep.Spec.Template.Spec.Volumes {
			if v.Name == "fleet-ca" {
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal("fmc-ca-bundle"))
			}
		}
	})

	It("[IA-5] mounts fleet-token on APIFrontend when tokenSecretName is set", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		knV2.Spec.Fleet.Backend = fleetBackendACM
		knV2.Spec.Fleet.Endpoint = "https://acm-search.example.com/graphql"
		knV2.Spec.Fleet.TokenSecretName = "acm-search-token"
		kn.Spec.APIFrontend = kubernautv1alpha1.APIFrontendSpec{
			Auth: kubernautv1alpha1.APIFrontendAuthSpec{IssuerURL: "https://login.kubernaut.ai/realms/kubernaut", Audience: "kubernaut-apifrontend"},
		}
		afDep, err := APIFrontendDeployment(kn, knV2, KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(afDep, "fleet-token")
		expectHasVolumeMount(afDep, "fleet-token", "/etc/fleet-token")
		for _, v := range afDep.Spec.Template.Spec.Volumes {
			if v.Name == "fleet-token" {
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal("acm-search-token"))
			}
		}
	})

	It("does not mount fleet-ca or fleet-token when enabled but no secret names are set", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		kn.Spec.APIFrontend = kubernautv1alpha1.APIFrontendSpec{
			Auth: kubernautv1alpha1.APIFrontendAuthSpec{IssuerURL: "https://login.kubernaut.ai/realms/kubernaut", Audience: "kubernaut-apifrontend"},
		}
		afDep, err := APIFrontendDeployment(kn, knV2, KagentiSidecarNone)
		Expect(err).NotTo(HaveOccurred())
		for _, v := range afDep.Spec.Template.Spec.Volumes {
			Expect(v.Name).NotTo(HavePrefix("fleet-"),
				"apifrontend should not have fleet volume %q when no secret names are set", v.Name)
		}
	})
})

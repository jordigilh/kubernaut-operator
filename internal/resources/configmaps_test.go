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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

const injectCABundleAnnotationValue = "true"

var _ = Describe("ConfigMaps", func() {
	Describe("Gateway ConfigMap", func() {
		It("contains DataStorage URL and expected keys", func() {
			kn := testKubernaut()
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			Expect(cm.Name).To(Equal("gateway-config"))
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("https://data-storage-service.kubernaut-system.svc.cluster.local"))
			Expect(data).To(ContainSubstring("k8sRequestTimeout"))
			Expect(data).To(ContainSubstring("trustedProxyCIDRs"))
			Expect(data).To(ContainSubstring("maxConcurrentRequests"))
		})

		It("includes TLS certDir for inter-service encryption", func() {
			kn := testKubernaut()
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("tls:"))
			Expect(data).To(ContainSubstring("certDir: /etc/tls"))
		})

		It("respects custom K8s request timeout", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Config.K8sRequestTimeout = "30s"
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("k8sRequestTimeout: 30s"))
		})

		It("renders v1.4 processing and related fields", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Logging.Level = "debug"
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			for _, want := range []string{
				"logging:",
				"level: debug",
				"processing:",
				"deduplication:",
				"cooldownPeriod: 5m",
				"retry:",
				"maxAttempts: 3",
				"initialBackoff: 100ms",
				"maxBackoff: 5s",
				"datastorage:",
				"buffer:",
				"bufferSize: 10000",
				"batchSize: 100",
				"flushInterval: 1s",
				"maxRetries: 3",
			} {
				Expect(data).To(ContainSubstring(want), "gateway v1.4 config should contain %q, got:\n%s", want, data)
			}
		})

		It("renders custom trusted proxy CIDRs", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Config.TrustedProxyCIDRs = []string{"10.0.0.0/8"}
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(strings.Contains(data, "trustedProxyCIDRs") && strings.Contains(data, "10.0.0.0/8")).To(BeTrue(), "gateway config should contain trustedProxyCIDRs with 10.0.0.0/8, got:\n%s", data)
		})

		It("renders custom deduplication cooldown", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Config.DeduplicationCooldown = "10m" //nolint:goconst // test value
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("cooldownPeriod: 10m"), "gateway config should contain cooldownPeriod 10m, got:\n%s", data)
		})

		It("renders default CORS config", func() {
			kn := testKubernaut()
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("cors:"))
			Expect(data).To(ContainSubstring("allowedOrigins:"))
			Expect(data).To(ContainSubstring("https://no-browser-clients.invalid"))
			Expect(data).To(ContainSubstring("allowedMethods:"))
			Expect(data).To(ContainSubstring("allowCredentials: false"))
			Expect(data).To(ContainSubstring("maxAge: 300"))
		})

		It("renders custom CORS origins", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Config.CORS.AllowedOrigins = []string{"https://dashboard.example.com", "https://admin.example.com"}
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("https://dashboard.example.com"))
			Expect(data).To(ContainSubstring("https://admin.example.com"))
			Expect(data).NotTo(ContainSubstring("no-browser-clients"))
		})

		It("renders custom CORS credentials and maxAge", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Config.CORS.AllowCredentials = ptr.To(true)
			kn.Spec.Gateway.Config.CORS.MaxAge = ptr.To(600)
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("allowCredentials: true"))
			Expect(data).To(ContainSubstring("maxAge: 600"))
		})
	})

	Describe("DataStorage ConfigMap", func() {
		It("contains PostgreSQL and Valkey settings", func() {
			kn := testKubernaut()
			cm, err := DataStorageConfigMap(kn, "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("host: pg.example.com"), "datastorage config should contain PG host, got:\n%s", data)
			Expect(data).To(ContainSubstring("addr: valkey.example.com:6379"), "datastorage config should contain Valkey addr, got:\n%s", data)
			Expect(data).To(ContainSubstring("secretsFile: /etc/datastorage/secrets/db-secrets.yaml"), "datastorage config should reference db secrets file, got:\n%s", data)
		})

		It("defaults PostgreSQL port to 5432 when unset", func() {
			kn := testKubernaut()
			kn.Spec.PostgreSQL.Port = 0
			cm, err := DataStorageConfigMap(kn, "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("port: 5432"), "datastorage config should default to port 5432, got:\n%s", data)
		})

		It("passes through PostgreSQL SSL mode", func() {
			kn := testKubernaut()
			kn.Spec.PostgreSQL.SSLMode = "require"
			cm, err := DataStorageConfigMap(kn, "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				Database struct {
					SSLMode string `yaml:"sslMode"`
				} `yaml:"database"`
			}
			err = yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)
			Expect(err).NotTo(HaveOccurred())
			Expect(root.Database.SSLMode).To(Equal("require"), "database.sslMode = %q, want require", root.Database.SSLMode)
		})
	})

	Describe("AIAnalysis ConfigMap", func() {
		It("includes confidence threshold when set", func() {
			kn := testKubernaut()
			kn.Spec.AIAnalysis.ConfidenceThreshold = "0.85"
			cm, err := AIAnalysisConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["config.yaml"]
			Expect(strings.Contains(data, "confidenceThreshold") && strings.Contains(data, "0.85")).To(BeTrue(), "aianalysis config should contain confidence threshold, got:\n%s", data)
		})

		It("uses agent key and not legacy kubernautAgent key", func() {
			kn := testKubernaut()
			cm, err := AIAnalysisConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("agent:"), "aianalysis config should contain 'agent:' key, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("kubernautAgent:"), "aianalysis config should not contain old 'kubernautAgent:' key, got:\n%s", data)
		})

		It("omits threshold when empty", func() {
			kn := testKubernaut()
			cm, err := AIAnalysisConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["config.yaml"]
			Expect(data).NotTo(ContainSubstring("confidenceThreshold"), "aianalysis config should not contain threshold when empty, got:\n%s", data)
		})
	})

	Describe("SignalProcessing ConfigMap", func() {
		It("contains DataStorage URL and classifier section", func() {
			kn := testKubernaut()
			cm, err := SignalProcessingConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("data-storage-service.kubernaut-system.svc.cluster.local"), "signalprocessing config should contain datastorage URL, got:\n%s", data)
			Expect(data).To(ContainSubstring("classifier:"), "signalprocessing config should contain classifier section, got:\n%s", data)
		})
	})

	Describe("RemediationOrchestrator ConfigMap", func() {
		It("includes default timeout and threshold strings", func() {
			kn := testKubernaut()
			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["remediationorchestrator.yaml"]
			defaults := []string{
				"global: 1h", "processing: 5m", "analyzing: 10m", "executing: 30m", "verifying: 30m",
				"ineffectiveChainThreshold: 3", "recurrenceCountThreshold: 5", "ineffectiveTimeWindow: 4h",
				"dryRun: false", "dryRunHoldPeriod: 1h",
			}
			for _, d := range defaults {
				Expect(data).To(ContainSubstring(d), "RO config should contain default %q, got:\n%s", d, data)
			}
		})

		It("uses nested structure for controller, datastorage, and timeouts", func() {
			kn := testKubernaut()
			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]

			for _, want := range []string{
				"controller:",
				"leaderElectionId: remediationorchestrator.kubernaut.ai",
				"datastorage:",
				"url: https://data-storage-service",
				"timeout:",
				"buffer:",
			} {
				Expect(data).To(ContainSubstring(want), "RO config should contain %q, got:\n%s", want, data)
			}
			Expect(data).NotTo(ContainSubstring("dataStorageUrl"), "RO config should not contain flat dataStorageUrl key, got:\n%s", data)
		})

		It("applies custom timeout values from the CR", func() {
			kn := testKubernaut()
			kn.Spec.RemediationOrchestrator.Timeouts.Global = "2h"
			kn.Spec.RemediationOrchestrator.Timeouts.Processing = "10m" //nolint:goconst // test value, not a meaningful constant
			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).To(ContainSubstring("global: 2h"), "RO config should use custom global timeout, got:\n%s", data)
			Expect(data).To(ContainSubstring("processing: 10m"), "RO config should use custom processing timeout, got:\n%s", data)
		})

		Context("BAC requirements", func() {
			It("BAC-2: default CR renders explicit dryRun false", func() {
				kn := testKubernaut()
				cm, err := RemediationOrchestratorConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				Expect(data).To(ContainSubstring("dryRun: false"), "BAC-2: default CR must render explicit 'dryRun: false', got:\n%s", data)
			})

			It("BAC-3: default CR renders dryRunHoldPeriod 1h", func() {
				kn := testKubernaut()
				cm, err := RemediationOrchestratorConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				Expect(data).To(ContainSubstring("dryRunHoldPeriod: 1h"), "BAC-3: default CR must render 'dryRunHoldPeriod: 1h', got:\n%s", data)
			})

			It("BAC-1: DryRun true renders dryRun true in ConfigMap", func() {
				kn := testKubernaut()
				kn.Spec.RemediationOrchestrator.DryRun = true
				cm, err := RemediationOrchestratorConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				Expect(data).To(ContainSubstring("dryRun: true"), "BAC-1: setting DryRun=true must render 'dryRun: true', got:\n%s", data)
			})

			It("BAC-4: custom hold period is rendered", func() {
				kn := testKubernaut()
				kn.Spec.RemediationOrchestrator.DryRun = true
				kn.Spec.RemediationOrchestrator.DryRunHoldPeriod = "30m"
				cm, err := RemediationOrchestratorConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				Expect(data).To(ContainSubstring("dryRunHoldPeriod: 30m"), "BAC-4: custom hold period must be rendered, got:\n%s", data)
			})

			It("BAC-6: dry-run changes do not alter unrelated settings", func() {
				kn := testKubernaut()
				kn.Spec.RemediationOrchestrator.DryRun = true
				kn.Spec.RemediationOrchestrator.DryRunHoldPeriod = "2h"
				cm, err := RemediationOrchestratorConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				unchanged := []string{
					"global: 1h", "processing: 5m", "analyzing: 10m",
					"consecutiveFailureThreshold: 3", "stabilizationWindow: 5m",
					"gitOpsSyncDelay: 3m",
				}
				for _, want := range unchanged {
					Expect(data).To(ContainSubstring(want), "BAC-6: enabling dry-run must not alter %q, got:\n%s", want, data)
				}
			})

			It("BAC-7: default CR remains backward compatible", func() {
				kn := testKubernaut()
				cm, err := RemediationOrchestratorConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				required := []string{
					"dryRun: false",
					"dryRunHoldPeriod: 1h",
					"global: 1h",
					"consecutiveFailureThreshold: 3",
				}
				for _, want := range required {
					Expect(data).To(ContainSubstring(want), "BAC-7: upgraded CR must still render %q, got:\n%s", want, data)
				}
			})
		})

		It("renders v1.4 logging, notifications, retention, routing, and timeouts", func() {
			kn := testKubernaut()
			kn.Spec.RemediationOrchestrator.Logging.Level = "warn"
			kn.Spec.RemediationOrchestrator.Notifications.NotifySelfResolved = true
			kn.Spec.RemediationOrchestrator.Retention.Period = "72h"
			kn.Spec.RemediationOrchestrator.Timeouts.AwaitingApproval = "25m"
			kn.Spec.RemediationOrchestrator.Routing.ExponentialBackoffBase = "2m"
			kn.Spec.RemediationOrchestrator.Routing.ExponentialBackoffMax = "20m"
			exp := 6
			kn.Spec.RemediationOrchestrator.Routing.ExponentialBackoffMaxExponent = &exp
			kn.Spec.RemediationOrchestrator.Routing.ScopeBackoffBase = "10s"
			kn.Spec.RemediationOrchestrator.Routing.ScopeBackoffMax = "10m" //nolint:goconst // test value
			delay := 48
			kn.Spec.RemediationOrchestrator.Routing.NoActionRequiredDelayHours = &delay

			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			for _, want := range []string{
				"logging:",
				"level: warn",
				"notifications:",
				"notifySelfResolved: true",
				"retention:",
				"period: 72h",
				"routing:",
				"exponentialBackoffBase: 2m",
				"exponentialBackoffMax: 20m",
				"exponentialBackoffMaxExponent: 6",
				"scopeBackoffBase: 10s",
				"scopeBackoffMax: 10m",
				"noActionRequiredDelayHours: 48",
				"timeouts:",
				"awaitingApproval: 25m",
			} {
				Expect(data).To(ContainSubstring(want), "RO v1.4 config should contain %q, got:\n%s", want, data)
			}
		})
	})

	Describe("WorkflowExecution ConfigMap", func() {
		It("uses default workflow namespace", func() {
			kn := testKubernaut()
			cm, err := WorkflowExecutionConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["workflowexecution.yaml"]
			Expect(data).To(ContainSubstring("kubernaut-workflows"), "WE config should use default workflow namespace, got:\n%s", data)
		})

		It("uses custom workflow namespace from the CR", func() {
			kn := testKubernaut()
			kn.Spec.WorkflowExecution.WorkflowNamespace = "custom-wf"
			cm, err := WorkflowExecutionConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["workflowexecution.yaml"]
			Expect(data).To(ContainSubstring("custom-wf"), "WE config should use custom workflow namespace, got:\n%s", data)
		})

		It("wires Ansible when enabled", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.Enabled = true
			kn.Spec.Ansible.APIURL = "https://awx.example.com"
			kn.Spec.Ansible.OrganizationID = 42
			kn.Spec.Ansible.TokenSecretRef = &kubernautv1alpha1.SecretKeyRef{
				Name: "awx-token",
				Key:  "api-token",
			}
			cm, err := WorkflowExecutionConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]

			for _, want := range []string{
				"ansible:",
				"apiURL: https://awx.example.com",
				"organizationID: 42",
				"tokenSecretRef:",
				"name: awx-token",
				"key: api-token",
			} {
				Expect(data).To(ContainSubstring(want), "WE config should contain %q when Ansible enabled, got:\n%s", want, data)
			}
		})

		It("omits Ansible when disabled", func() {
			kn := testKubernaut()
			cm, err := WorkflowExecutionConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]

			Expect(data).NotTo(ContainSubstring("ansible:"), "WE config should not contain ansible section when disabled, got:\n%s", data)
		})

		It("uses nested execution, datastorage, and controller structure", func() {
			kn := testKubernaut()
			cm, err := WorkflowExecutionConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
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
				Expect(data).To(ContainSubstring(want), "WE config should contain %q, got:\n%s", want, data)
			}
		})

		It("renders logging level", func() {
			kn := testKubernaut()
			kn.Spec.WorkflowExecution.Logging.Level = "error"
			cm, err := WorkflowExecutionConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]
			Expect(strings.Contains(data, "logging:") && strings.Contains(data, "level: error")).To(BeTrue(), "WE config should render logging.level, got:\n%s", data)
		})

		It("renders Tekton enabled when set", func() {
			kn := testKubernaut()
			on := true
			kn.Spec.WorkflowExecution.Tekton.Enabled = &on
			cm, err := WorkflowExecutionConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]
			Expect(strings.Contains(data, "tekton:") && strings.Contains(data, "enabled: true")).To(BeTrue(), "WE config should render tekton.enabled, got:\n%s", data)
		})
	})

	Describe("EffectivenessMonitor ConfigMap", func() {
		It("includes default stabilization window", func() {
			kn := testKubernaut()
			cm, err := EffectivenessMonitorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("stabilizationWindow: 30s"), "EM config should have default stabilization window, got:\n%s", data)
		})

		It("includes monitoring URLs when monitoring is enabled", func() {
			kn := testKubernaut()
			cm, err := EffectivenessMonitorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]

			Expect(data).To(ContainSubstring(OCPPrometheusURL), "EM config should contain Prometheus URL when monitoring enabled, got:\n%s", data)
			Expect(data).To(ContainSubstring(OCPAlertManagerURL), "EM config should contain AlertManager URL when monitoring enabled, got:\n%s", data)
			Expect(data).To(ContainSubstring("external:"), "EM config should contain external section when monitoring enabled, got:\n%s", data)
			Expect(data).To(ContainSubstring("tlsCaFile: /etc/ssl/em/service-ca.crt"), "EM config should contain external.tlsCaFile when monitoring enabled, got:\n%s", data)
		})

		It("omits external monitoring when monitoring is disabled", func() {
			kn := testKubernaut()
			disabled := false
			kn.Spec.Monitoring.Enabled = &disabled
			cm, err := EffectivenessMonitorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]

			Expect(data).NotTo(ContainSubstring("external:"), "EM config should not contain external monitoring section when disabled, got:\n%s", data)
		})

		It("renders v1.4 logging and datastorage buffer settings", func() {
			kn := testKubernaut()
			kn.Spec.EffectivenessMonitor.Logging.Level = "debug"
			cm, err := EffectivenessMonitorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			for _, want := range []string{
				"logging:",
				"level: debug",
				"datastorage:",
				"timeout: 10s",
				"buffer:",
				"bufferSize: 100",
				"batchSize: 10",
				"flushInterval: 1s",
				"maxRetries: 3",
			} {
				Expect(data).To(ContainSubstring(want), "EM v1.4 config should contain %q, got:\n%s", want, data)
			}
		})
	})

	Describe("Notification ConfigMap", func() {
		It("routing includes Slack when configured", func() {
			kn := testKubernaut()
			kn.Spec.Notification.Slack.SecretName = "slack-webhook"
			kn.Spec.Notification.Slack.Channel = "#ops"
			cm, err := NotificationRoutingConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["routing.yaml"]
			Expect(data).To(ContainSubstring("slack"), "routing config should reference slack receiver, got:\n%s", data)
			Expect(data).To(ContainSubstring("#ops"), "routing config should contain channel #ops, got:\n%s", data)
		})

		It("routing falls back to console without Slack", func() {
			kn := testKubernaut()
			cm, err := NotificationRoutingConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["routing.yaml"]
			Expect(data).To(ContainSubstring("console"), "routing config without slack should use console receiver, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("slack"), "routing config should not contain slack when Slack is unconfigured, got:\n%s", data)
		})

		It("controller config places credentials under delivery", func() {
			kn := testKubernaut()
			cm, err := NotificationControllerConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("delivery:"), "notification config should contain delivery: block, got:\n%s", data)
			Expect(data).To(ContainSubstring("credentials:"), "notification config should contain credentials: block, got:\n%s", data)
			Expect(data).To(ContainSubstring("dir: /etc/notification/credentials"), "notification config should contain credentials dir, got:\n%s", data)
		})

		It("routing still builds default content when Routing ConfigMap is BYO", func() {
			kn := testKubernaut()
			kn.Spec.Notification.Routing = &kubernautv1alpha1.ConfigMapRef{ConfigMapName: "my-routing"}
			cm, err := NotificationRoutingConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Name).To(Equal("notification-routing-config"), "NotificationRoutingConfigMap name = %q, want notification-routing-config (BYO affects deployment/controller, not this builder)", cm.Name)
			data := cm.Data["routing.yaml"]
			Expect(data).To(ContainSubstring("console"), "expected default routing content when builder invoked, got:\n%s", data)
		})
	})

	Describe("KubernautAgent ConfigMap", func() {
		It("includes monitoring and data storage integration when monitoring enabled", func() {
			kn := testKubernaut()
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]

			Expect(data).To(ContainSubstring(OCPPrometheusURL), "KA config should contain Prometheus URL when monitoring enabled, got:\n%s", data)
			Expect(data).To(ContainSubstring("tlsCaFile: /etc/ssl/ka/service-ca.crt"), "KA config should contain Prometheus tlsCaFile for SA bearer auth, got:\n%s", data)
			Expect(data).To(ContainSubstring("dataStorage:"), "KA config should contain dataStorage section, got:\n%s", data)
			Expect(data).To(ContainSubstring("url: https://data-storage-service.kubernaut-system.svc.cluster.local:8443"), "KA config should contain HTTPS dataStorage.url, got:\n%s", data)
			Expect(strings.Contains(data, "tools:") && strings.Contains(data, "prometheus:")).To(BeTrue(), "KA config should contain upstream tools.prometheus section when monitoring enabled, got:\n%s", data)
		})

		It("omits Prometheus tools when monitoring is disabled", func() {
			kn := testKubernaut()
			disabled := false
			kn.Spec.Monitoring.Enabled = &disabled
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]

			Expect(strings.Contains(data, "prometheusUrl") || strings.Contains(data, "tools:")).To(BeFalse(), "KA config should not contain Prometheus tools section when monitoring is disabled, got:\n%s", data)
		})

		It("matches expected v1.4 structure and defaults", func() {
			kn := testKubernaut()
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				Runtime struct {
					Logging struct {
						Level string `yaml:"level"`
					} `yaml:"logging"`
					Server struct {
						Address string `yaml:"address"`
						Port    int    `yaml:"port"`
					} `yaml:"server"`
					Audit struct {
						BufferSize int `yaml:"bufferSize"`
					} `yaml:"audit"`
				} `yaml:"runtime"`
				AI struct {
					LLM struct {
						Provider string `yaml:"provider"`
					} `yaml:"llm"`
					Investigation struct {
						MaxTurns int `yaml:"maxTurns"`
					} `yaml:"investigation"`
				} `yaml:"ai"`
				Integrations struct {
					DataStorage struct {
						URL string `yaml:"url"`
					} `yaml:"dataStorage"`
					Tools *struct {
						Prometheus struct {
							URL       string `yaml:"url"`
							TLSCaFile string `yaml:"tlsCaFile"`
						} `yaml:"prometheus"`
					} `yaml:"tools,omitempty"`
				} `yaml:"integrations"`
			}
			err = yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)
			Expect(err).NotTo(HaveOccurred())
			Expect(root.Runtime.Logging.Level).To(Equal("info"), "runtime.logging.level = %q, want info", root.Runtime.Logging.Level)
			Expect(root.Runtime.Server.Port == 8443 && root.Runtime.Server.Address == "0.0.0.0").To(BeTrue(), "runtime.server = %#v, want address 0.0.0.0 port 8443", root.Runtime.Server)
			Expect(root.Runtime.Audit.BufferSize).To(Equal(10000), "runtime.audit.bufferSize = %d, want 10000", root.Runtime.Audit.BufferSize)
			Expect(root.AI.LLM.Provider).To(Equal("openai"), "ai.llm.provider = %q, want openai", root.AI.LLM.Provider)
			Expect(root.AI.Investigation.MaxTurns).To(Equal(40), "ai.investigation.maxTurns = %d, want 40", root.AI.Investigation.MaxTurns)
			wantDS := DataStorageURL(kn.Namespace)
			Expect(root.Integrations.DataStorage.URL).To(Equal(wantDS), "integrations.dataStorage.url = %q, want %q", root.Integrations.DataStorage.URL, wantDS)
			Expect(root.Integrations.Tools).NotTo(BeNil(), "integrations.tools should be present when monitoring is enabled by default")
			Expect(root.Integrations.Tools.Prometheus.URL).To(Equal(OCPPrometheusURL), "integrations.tools.prometheus.url = %q, want %q", root.Integrations.Tools.Prometheus.URL, OCPPrometheusURL)
			Expect(root.Integrations.Tools.Prometheus.TLSCaFile).To(Equal("/etc/ssl/ka/service-ca.crt"), "integrations.tools.prometheus.tlsCaFile = %q, want /etc/ssl/ka/service-ca.crt", root.Integrations.Tools.Prometheus.TLSCaFile)
		})

		It("renders alignment check settings when enabled", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.AlignmentCheck.Enabled = true
			kn.Spec.KubernautAgent.AlignmentCheck.Timeout = "20s"
			kn.Spec.KubernautAgent.AlignmentCheck.MaxStepTokens = 1024
			kn.Spec.KubernautAgent.AlignmentCheck.LLM = &kubernautv1alpha1.AlignmentCheckLLMSpec{
				Provider: "openai",
				Model:    "gpt-4o-mini",
				Endpoint: "https://align.example/v1",
			}
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			for _, want := range []string{
				"alignmentCheck:",
				"enabled: true",
				"timeout: 20s",
				"maxStepTokens: 1024",
				"llm:",
				"provider: openai",
				"model: gpt-4o-mini",
				"endpoint: https://align.example/v1",
			} {
				Expect(data).To(ContainSubstring(want), "KA config should contain %q when alignment check enabled, got:\n%s", want, data)
			}
		})

		It("propagates custom LLM TLS CA file", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.LLM.TLSCaFile = "/etc/custom-ca/llm.pem"
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				AI struct {
					LLM struct {
						TLSCaFile string `yaml:"tlsCaFile"`
					} `yaml:"llm"`
				} `yaml:"ai"`
			}
			err = yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)
			Expect(err).NotTo(HaveOccurred())
			Expect(root.AI.LLM.TLSCaFile).To(Equal("/etc/custom-ca/llm.pem"), "ai.llm.tlsCaFile = %q, want /etc/custom-ca/llm.pem", root.AI.LLM.TLSCaFile)
		})

		It("renders non-default summarizer thresholds", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.Summarizer.Threshold = 5000
			kn.Spec.KubernautAgent.Summarizer.MaxToolOutputSize = 50000
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				AI struct {
					Summarizer *struct {
						Threshold         int `yaml:"threshold"`
						MaxToolOutputSize int `yaml:"maxToolOutputSize"`
					} `yaml:"summarizer"`
				} `yaml:"ai"`
			}
			err = yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)
			Expect(err).NotTo(HaveOccurred())
			Expect(root.AI.Summarizer).NotTo(BeNil(), "expected ai.summarizer block for non-default summarizer settings")
			Expect(root.AI.Summarizer.Threshold).To(Equal(5000), "summarizer.threshold = %d, want 5000", root.AI.Summarizer.Threshold)
			Expect(root.AI.Summarizer.MaxToolOutputSize).To(Equal(50000), "summarizer.maxToolOutputSize = %d, want 50000", root.AI.Summarizer.MaxToolOutputSize)
		})

		It("renders safety anomaly max tool calls per tool", func() {
			kn := testKubernaut()
			maxPer := 5
			kn.Spec.KubernautAgent.Safety.Anomaly.MaxToolCallsPerTool = &maxPer
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				AI struct {
					Safety struct {
						Anomaly struct {
							MaxToolCallsPerTool int `yaml:"maxToolCallsPerTool"`
						} `yaml:"anomaly"`
					} `yaml:"safety"`
				} `yaml:"ai"`
			}
			err = yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)
			Expect(err).NotTo(HaveOccurred())
			Expect(root.AI.Safety.Anomaly.MaxToolCallsPerTool).To(Equal(5), "ai.safety.anomaly.maxToolCallsPerTool = %d, want 5", root.AI.Safety.Anomaly.MaxToolCallsPerTool)
		})

		It("renders LLM OAuth2 block when enabled", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.LLM.OAuth2.Enabled = true
			kn.Spec.KubernautAgent.LLM.OAuth2.TokenURL = "https://idp.example/oauth/token"
			kn.Spec.KubernautAgent.LLM.OAuth2.Scopes = []string{"openid", "api.read"}
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				AI struct {
					LLM struct {
						OAuth2 *struct {
							Enabled  bool     `yaml:"enabled"`
							TokenURL string   `yaml:"tokenURL"`
							Scopes   []string `yaml:"scopes"`
						} `yaml:"oauth2"`
					} `yaml:"llm"`
				} `yaml:"ai"`
			}
			err = yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)
			Expect(err).NotTo(HaveOccurred())
			Expect(root.AI.LLM.OAuth2).NotTo(BeNil(), "expected ai.llm.oauth2 block when OAuth2 enabled")
			Expect(root.AI.LLM.OAuth2.Enabled).To(BeTrue(), "oauth2.enabled should be true")
			Expect(root.AI.LLM.OAuth2.TokenURL).To(Equal("https://idp.example/oauth/token"), "oauth2.tokenURL = %q", root.AI.LLM.OAuth2.TokenURL)
			Expect(len(root.AI.LLM.OAuth2.Scopes) == 2 && root.AI.LLM.OAuth2.Scopes[0] == "openid" && root.AI.LLM.OAuth2.Scopes[1] == "api.read").To(BeTrue(), "oauth2.scopes = %#v, want [openid api.read]", root.AI.LLM.OAuth2.Scopes)
		})

		Describe("LLM runtime ConfigMap", func() {
			It("is generated when no existing ConfigMap is specified", func() {
				kn := testKubernaut()
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())

				Expect(cm).NotTo(BeNil(), "KubernautAgentLLMRuntimeConfigMap should not be nil when no existing CM specified")
				data := cm.Data["llm-runtime.yaml"]
				Expect(data).To(ContainSubstring("model: gpt-4o"), "LLM runtime config should contain model, got:\n%s", data)
				Expect(data).To(ContainSubstring("temperature:"), "LLM runtime config should contain temperature, got:\n%s", data)
			})

			It("is nil when user provides existing ConfigMap name", func() {
				kn := testKubernaut()
				kn.Spec.KubernautAgent.LLM.RuntimeConfigMapName = "my-llm-runtime-config"
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				Expect(cm).To(BeNil(), "KubernautAgentLLMRuntimeConfigMap should be nil when user provides existing CM")
			})

			It("includes default model and retry settings", func() {
				kn := testKubernaut()
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["llm-runtime.yaml"]
				for _, want := range []string{
					"model: gpt-4o",
					"temperature: 0.7",
					"maxRetries: 3",
					"timeoutSeconds: 120",
				} {
					Expect(data).To(ContainSubstring(want), "llm-runtime defaults should contain %q, got:\n%s", want, data)
				}
			})

			It("applies custom LLM runtime values", func() {
				kn := testKubernaut()
				kn.Spec.KubernautAgent.LLM.Temperature = "0.5"
				kn.Spec.KubernautAgent.LLM.Endpoint = "https://llm-custom.example/v1"
				maxR := 7
				kn.Spec.KubernautAgent.LLM.MaxRetries = &maxR
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["llm-runtime.yaml"]
				for _, want := range []string{
					"temperature: 0.5",
					"endpoint: https://llm-custom.example/v1",
					"maxRetries: 7",
				} {
					Expect(data).To(ContainSubstring(want), "llm-runtime custom values should contain %q, got:\n%s", want, data)
				}
			})

			It("returns nil when runtimeConfigMapName is set (BYO)", func() {
				kn := testKubernaut()
				kn.Spec.KubernautAgent.LLM.RuntimeConfigMapName = "user-llm-runtime"
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				Expect(cm).To(BeNil(), "KubernautAgentLLMRuntimeConfigMap should return nil when runtimeConfigMapName is set (BYO)")
			})
		})
	})

	Describe("AuthWebhook ConfigMap", func() {
		It("writes authwebhook.yaml as the config key", func() {
			kn := testKubernaut()
			cm, err := AuthWebhookConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())

			Expect(cm.Data).To(HaveKey("authwebhook.yaml"), "AuthWebhookConfigMap should write authwebhook.yaml, keys: %#v", cm.Data)
		})
	})

	Describe("Inter-service CA and service-ca ConfigMaps", func() {
		It("inter-service CA ConfigMap has inject-cabundle annotation and expected name", func() {
			kn := testKubernaut()
			cm := InterServiceCAConfigMap(kn)
			Expect(cm.Name).To(Equal(InterServiceCAConfigMapName))
			v, ok := cm.Annotations[OCPServiceCAInjectAnnotation]
			Expect(ok && v == injectCABundleAnnotationValue).To(BeTrue(), "inter-service-ca ConfigMap should have inject-cabundle annotation")
		})

		DescribeTable("OpenShift service-ca ConfigMaps have inject-cabundle annotation",
			func(mkCM func(*kubernautv1alpha1.Kubernaut) *corev1.ConfigMap) {
				kn := testKubernaut()
				cm := mkCM(kn)
				Expect(cm.Annotations["service.beta.openshift.io/inject-cabundle"]).To(Equal(injectCABundleAnnotationValue))
			},
			Entry("effectivenessmonitor-service-ca", EffectivenessMonitorServiceCAConfigMap),
			Entry("kubernaut-agent-service-ca", KubernautAgentServiceCAConfigMap),
		)
	})

	Describe("ProactiveSignalMappings", func() {
		It("default mappings are generated when no user override", func() {
			kn := testKubernaut()

			cm := ProactiveSignalMappingsConfigMap(kn)
			Expect(cm).NotTo(BeNil(), "ProactiveSignalMappingsConfigMap should return non-nil when no user override")
			Expect(cm.Name).To(Equal("signalprocessing-proactive-signal-mappings"), "Name = %q, want %q", cm.Name, "signalprocessing-proactive-signal-mappings")
			data, ok := cm.Data["proactive-signal-mappings.yaml"]
			Expect(ok).To(BeTrue(), "ConfigMap should contain proactive-signal-mappings.yaml key")
			for _, mapping := range []string{
				"PredictedOOMKill", "OOMKilled",
				"PredictedCPUThrottling", "CPUThrottling",
				"PredictedDiskPressure", "DiskPressure",
				"PredictedNodeNotReady", "NodeNotReady",
			} {
				Expect(data).To(ContainSubstring(mapping), "proactive-signal-mappings.yaml should contain %q, got:\n%s", mapping, data)
			}
		})

		It("returns nil when user provides ConfigMapName", func() {
			kn := testKubernaut()
			kn.Spec.SignalProcessing.ProactiveSignalMappings = &kubernautv1alpha1.ConfigMapRef{
				ConfigMapName: "user-proactive-mappings",
			}

			cm := ProactiveSignalMappingsConfigMap(kn)
			Expect(cm).To(BeNil(), "ProactiveSignalMappingsConfigMap should return nil when user provides ConfigMapName")
		})
	})

	Describe("Cross-cutting", func() {
		It("built service ConfigMaps use the system namespace", func() {
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
				Expect(err).NotTo(HaveOccurred(), "building %s ConfigMap", b.name)
				Expect(cm.Namespace).To(Equal(testSystemNamespace), "ConfigMap %q namespace = %q, want %q", cm.Name, cm.Namespace, testSystemNamespace)
			}
		})

		const loggingLevelAllServicesTestLevel = "error"

		DescribeTable("logging level propagates to each service ConfigMap",
			func(prep func(*kubernautv1alpha1.Kubernaut), key string, fn func(*kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error)) {
				kn := testKubernaut()
				prep(kn)
				cm, err := fn(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data[key]
				Expect(data).To(ContainSubstring("level: "+loggingLevelAllServicesTestLevel), "expected logging level %q in %s, got:\n%s", loggingLevelAllServicesTestLevel, key, data)
			},
			Entry("gateway",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.Gateway.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) { return GatewayConfigMap(kn) },
			),
			Entry("datastorage",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.DataStorage.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return DataStorageConfigMap(kn, "kubernautdb", "kubernautuser")
				},
			),
			Entry("aianalysis",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.AIAnalysis.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) { return AIAnalysisConfigMap(kn) },
			),
			Entry("signalprocessing",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.SignalProcessing.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return SignalProcessingConfigMap(kn)
				},
			),
			Entry("remediationorchestrator",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.RemediationOrchestrator.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"remediationorchestrator.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return RemediationOrchestratorConfigMap(kn)
				},
			),
			Entry("workflowexecution",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.WorkflowExecution.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"workflowexecution.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return WorkflowExecutionConfigMap(kn)
				},
			),
			Entry("effectivenessmonitor",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.EffectivenessMonitor.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"effectivenessmonitor.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return EffectivenessMonitorConfigMap(kn)
				},
			),
			Entry("notification-controller",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.Notification.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return NotificationControllerConfigMap(kn)
				},
			),
			Entry("kubernaut-agent",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.KubernautAgent.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) { return KubernautAgentConfigMap(kn) },
			),
			Entry("authwebhook",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.AuthWebhook.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"authwebhook.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) { return AuthWebhookConfigMap(kn) },
			),
		)
	})
})

var _ = Describe("APIFrontendConfigMap", func() {
	It("generates a valid config.yaml", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Name).To(Equal("apifrontend-config"))
		data, ok := cm.Data["config.yaml"]
		Expect(ok).To(BeTrue(), "config.yaml key missing")
		Expect(data).To(ContainSubstring("port: 8443"))
		Expect(data).To(ContainSubstring("kaBaseURL"))
		Expect(data).To(ContainSubstring("dsBaseURL"))
		Expect(data).To(ContainSubstring("issuerURL"))
	})

	It("renders config with empty issuerURL when auth is not configured", func() {
		kn := testKubernaut()
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("port: 8443"))
		Expect(data).NotTo(ContainSubstring("issuerURL: https://"))
	})

	It("disables severityTriage when monitoring is disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.Monitoring.Enabled = &disabled
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("enabled: false"))
		Expect(data).NotTo(ContainSubstring("thanos-querier"),
			"disabled severityTriage should not reference Thanos Querier URL")
	})

	It("uses SA token CA for severity triage when monitoring is enabled", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("prometheusTlsCaFile: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt"))
	})

	It("renders auth issuerURL and audience from spec", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("https://login.kubernaut.ai/realms/kubernaut"))
		Expect(data).To(ContainSubstring("kubernaut-apifrontend"))
	})

	It("renders rate limit defaults", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("ipRequestsPerSec: 50"))
		Expect(data).To(ContainSubstring("userRequestsPerSec: 20"))
	})

	It("renders resilience circuit breaker config", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("cbFailureThreshold:"))
		Expect(data).To(ContainSubstring("retryMax:"))
	})

	It("renders replayCache when Valkey secret is set", func() {
		kn := testKubernautWithAF()
		kn.Spec.Valkey.SecretName = "my-valkey-secret"
		kn.Spec.Valkey.Host = "valkey.kubernaut-system.svc.cluster.local"
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("replayCache:"))
		Expect(data).To(ContainSubstring("backend: redis"))
		Expect(data).To(ContainSubstring("redisDB: 1"))
		Expect(data).To(ContainSubstring("credentialsPath: /etc/apifrontend/valkey/valkey-secrets.yaml"))
	})

	It("omits replayCache when Valkey secret is empty", func() {
		kn := testKubernautWithAF()
		kn.Spec.Valkey.SecretName = ""
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("replayCache:"))
	})

	It("renders nested agent.llm config section", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]

		var root struct {
			Agent struct {
				LLM struct {
					Provider   string `yaml:"provider"`
					Model      string `yaml:"model"`
					APIKeyFile string `yaml:"apiKeyFile"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		err = yaml.Unmarshal([]byte(data), &root)
		Expect(err).NotTo(HaveOccurred())
		Expect(root.Agent.LLM.Provider).To(Equal("openai"), "agent.llm.provider = %q, want openai", root.Agent.LLM.Provider)
		Expect(root.Agent.LLM.Model).To(Equal("gpt-4o"), "agent.llm.model = %q, want gpt-4o", root.Agent.LLM.Model)
		Expect(root.Agent.LLM.APIKeyFile).To(Equal("/etc/apifrontend/llm-credentials/api_key"),
			"agent.llm.apiKeyFile should point to mounted secret")
	})

	It("does not emit flat llmEndpoint or llmModel fields", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("llmEndpoint:"), "flat llmEndpoint field should not be emitted")
		Expect(data).NotTo(ContainSubstring("llmModel:"), "flat llmModel field should not be emitted")
	})

	It("renders Vertex AI fields in agent.llm config", func() {
		kn := testKubernautWithAF()
		kn.Spec.KubernautAgent.LLM.Provider = "vertex_ai"
		kn.Spec.KubernautAgent.LLM.Model = "gemini-2.5-pro"
		kn.Spec.KubernautAgent.LLM.VertexProject = "my-project"
		kn.Spec.KubernautAgent.LLM.VertexLocation = "us-central1"
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]

		var root struct {
			Agent struct {
				LLM struct {
					Provider       string `yaml:"provider"`
					Model          string `yaml:"model"`
					VertexProject  string `yaml:"vertexProject"`
					VertexLocation string `yaml:"vertexLocation"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		err = yaml.Unmarshal([]byte(data), &root)
		Expect(err).NotTo(HaveOccurred())
		Expect(root.Agent.LLM.Provider).To(Equal("vertex_ai"))
		Expect(root.Agent.LLM.Model).To(Equal("gemini-2.5-pro"))
		Expect(root.Agent.LLM.VertexProject).To(Equal("my-project"))
		Expect(root.Agent.LLM.VertexLocation).To(Equal("us-central1"))
	})

	It("renders OAuth2 block in agent.llm when enabled", func() {
		kn := testKubernautWithAF()
		kn.Spec.KubernautAgent.LLM.OAuth2.Enabled = true
		kn.Spec.KubernautAgent.LLM.OAuth2.TokenURL = "https://idp.example/oauth/token"
		kn.Spec.KubernautAgent.LLM.OAuth2.Scopes = []string{"openid", "llm.invoke"}
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]

		var root struct {
			Agent struct {
				LLM struct {
					OAuth2 *struct {
						Enabled        bool     `yaml:"enabled"`
						TokenURL       string   `yaml:"tokenURL"`
						Scopes         []string `yaml:"scopes"`
						CredentialsDir string   `yaml:"credentialsDir"`
					} `yaml:"oauth2"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		err = yaml.Unmarshal([]byte(data), &root)
		Expect(err).NotTo(HaveOccurred())
		Expect(root.Agent.LLM.OAuth2).NotTo(BeNil())
		Expect(root.Agent.LLM.OAuth2.Enabled).To(BeTrue())
		Expect(root.Agent.LLM.OAuth2.TokenURL).To(Equal("https://idp.example/oauth/token"))
		Expect(root.Agent.LLM.OAuth2.CredentialsDir).To(Equal("/etc/apifrontend/oauth2"))
	})

	It("omits OAuth2 block when not enabled", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("oauth2:"))
	})
})

var _ = Describe("APIFrontendConfigMap SAR", func() {
	It("includes rbac.sarCacheTTL with default 30s", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("sarCacheTTL: 30s"),
			"AF config should include rbac.sarCacheTTL default 30s, got:\n%s", data)
	})

	It("renders custom sarCacheTTL from spec", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			SARCacheTTL: "2m",
		}
		cm, err := APIFrontendConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("sarCacheTTL: 2m"),
			"AF config should render custom sarCacheTTL, got:\n%s", data)
	})
})

var _ = Describe("APIFrontendRBACRolesConfigMap", func() {
	It("generates default RBAC roles", func() {
		kn := testKubernautWithAF()
		cm := APIFrontendRBACRolesConfigMap(kn)
		Expect(cm.Name).To(Equal("apifrontend-rbac-roles"))
		data, ok := cm.Data["rbac_roles.yaml"]
		Expect(ok).To(BeTrue(), "rbac_roles.yaml key missing")
		Expect(data).To(ContainSubstring("admin:"))
		Expect(data).To(ContainSubstring("viewer:"))
		Expect(data).NotTo(ContainSubstring("tools:"),
			"RBAC roles must use flat list format (role: [...]), not nested map (role: {tools: [...]})")
	})
})

var _ = Describe("DataStorage SignerCertDir Config", func() {
	It("renders signerCertDir when signing cert is configured", func() {
		kn := testKubernaut()
		kn.Spec.DataStorage.SigningCert = &kubernautv1alpha1.SigningCertSpec{
			SecretName: "datastorage-signing-cert",
		}
		cm, err := DataStorageConfigMap(kn, "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("signerCertDir: /etc/certs"))
	})

	It("defaults signerCertDir to /etc/certs when signing cert is not configured", func() {
		kn := testKubernaut()
		cm, err := DataStorageConfigMap(kn, "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("signerCertDir: /etc/certs"))
	})
})

var _ = Describe("DataStorage Redis TLS Config", func() {
	It("renders TLS config when Valkey TLS is enabled", func() {
		kn := testKubernautWithValkeyTLS()
		cm, err := DataStorageConfigMap(kn, "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("enabled: true"))
		Expect(data).To(ContainSubstring("caFile: /etc/valkey-tls/ca/ca.crt"))
		Expect(data).To(ContainSubstring("certFile: /etc/valkey-tls/client/tls.crt"))
		Expect(data).To(ContainSubstring("keyFile: /etc/valkey-tls/client/tls.key"))
	})

	It("omits TLS block when Valkey TLS is not configured", func() {
		kn := testKubernaut()
		cm, err := DataStorageConfigMap(kn, "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("caFile:"))
	})
})

var _ = Describe("DataStorage Retention Config", func() {
	It("renders retention block with defaults when spec is provided", func() {
		kn := testKubernaut()
		enabled := true
		kn.Spec.DataStorage.Retention = &kubernautv1alpha1.RetentionSpec{
			Enabled: &enabled,
		}
		cm, err := DataStorageConfigMap(kn, "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("retention:"))
		Expect(data).To(ContainSubstring("enabled: true"))
		Expect(data).To(ContainSubstring("interval: 24h"))
		Expect(data).To(ContainSubstring("batchSize: 1000"))
		Expect(data).To(ContainSubstring("defaultDays: 2555"))
	})

	It("omits retention block when spec is nil", func() {
		kn := testKubernaut()
		cm, err := DataStorageConfigMap(kn, "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("retention:"))
	})

	It("clamps defaultDays to 2555", func() {
		kn := testKubernaut()
		days := 5000
		kn.Spec.DataStorage.Retention = &kubernautv1alpha1.RetentionSpec{
			DefaultDays: &days,
		}
		cm, err := DataStorageConfigMap(kn, "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("defaultDays: 2555"))
		Expect(data).NotTo(ContainSubstring("defaultDays: 5000"))
	})

	It("respects custom values", func() {
		kn := testKubernaut()
		enabled := false
		batch := 500
		days := 365
		kn.Spec.DataStorage.Retention = &kubernautv1alpha1.RetentionSpec{
			Enabled:     &enabled,
			Interval:    "12h",
			BatchSize:   &batch,
			DefaultDays: &days,
		}
		cm, err := DataStorageConfigMap(kn, "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("enabled: false"))
		Expect(data).To(ContainSubstring("interval: 12h"))
		Expect(data).To(ContainSubstring("batchSize: 500"))
		Expect(data).To(ContainSubstring("defaultDays: 365"))
	})
})

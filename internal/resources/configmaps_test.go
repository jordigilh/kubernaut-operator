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

const testOpenAIEndpoint = "http://llm-gateway:8080"

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

		It("omits the fleet block when fleet is disabled", func() {
			kn := testKubernaut()
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).NotTo(ContainSubstring("fleet:"), "gateway config should omit fleet block when disabled, got:\n%s", data)
		})

		It("renders the fleet block with backend and endpoint when enabled", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			}
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			for _, want := range []string{
				"fleet:",
				"enabled: true",
				"backend: fleetmetadatacache",
				"endpoint: https://fmc.kubernaut.svc:8443",
			} {
				Expect(data).To(ContainSubstring(want), "gateway config should contain %q when fleet enabled, got:\n%s", want, data)
			}
		})

		It("renders tlsCAFile and tokenPath when the corresponding secrets are set", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "acm", Endpoint: "https://acm-search.example.com/graphql",
				CASecretName: "fmc-ca-bundle", TokenSecretName: "acm-search-token",
			}
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("tlsCAFile: /etc/fleet-tls/ca/ca.crt"), "gateway config should render tlsCAFile mount path, got:\n%s", data)
			Expect(data).To(ContainSubstring("tokenPath: /etc/fleet-token/token"), "gateway config should render tokenPath mount path, got:\n%s", data)
		})

		// #222: mcpGatewayEndpoint/Type must be rendered — Gateway crash-loops
		// at startup without them (upstream Fleet.ValidateFullFederation).
		It("renders mcpGatewayEndpoint and mcpGatewayType when set", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			}
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("mcpGatewayEndpoint: https://mcp-gateway.example.com/sse"), "gateway config should render mcpGatewayEndpoint, got:\n%s", data)
			Expect(data).To(ContainSubstring("mcpGatewayType: eaigw"), "gateway config should render mcpGatewayType, got:\n%s", data)
		})

		It("omits the oauth2 block when fleet oauth2 is disabled", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			}
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).NotTo(ContainSubstring("oauth2:"), "gateway config should omit fleet oauth2 block when disabled, got:\n%s", data)
		})

		It("renders the fleet oauth2 block when enabled", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha1.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds", Scopes: []string{"openid", "groups"},
				},
			}
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			for _, want := range []string{
				"oauth2:",
				"enabled: true",
				"tokenURL: https://keycloak.example.com/token",
				"credentialsSecretRef: fleet-oauth2-creds",
			} {
				Expect(data).To(ContainSubstring(want), "gateway config should contain %q when fleet oauth2 enabled, got:\n%s", want, data)
			}
		})

		// Pre-existing gap (#223 triage): upstream FleetOAuth2Config.TLSCAFile
		// exists and is consumed, but the operator's rendered oauth2 block
		// never included it. Defaults to InterServiceTLSCAFile so a
		// cluster-local OAuth2 provider's TLS cert (signed by the
		// service-ca operator) verifies without extra configuration.
		It("renders oauth2.tlsCAFile defaulting to the inter-service CA path when oauth2 is enabled", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha1.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("tlsCAFile: "+InterServiceTLSCAFile), "gateway oauth2 block should default tlsCAFile to the inter-service CA path, got:\n%s", data)
		})

		It("renders gateway.fleetOAuth2CredentialsSecretRef instead of the shared credentialsSecretRef when set", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha1.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			kn.Spec.Gateway.FleetOAuth2CredentialsSecretRef = testGatewayFleetOAuth2SecretRef
			cm, err := GatewayConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: gateway-oauth2-creds"), "gateway config should use its own oauth2 client override, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "gateway config should not fall back to the shared credentialsSecretRef when it has its own override, got:\n%s", data)
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

		// #224: SP constructs a ClusterRegistry (BR-FLEET-003) for cluster
		// classification labels via its own bespoke FleetConfig shape --
		// critically "endpoint", not "mcpGatewayEndpoint" (upstream
		// pkg/signalprocessing/config.FleetConfig.Endpoint).
		It("omits the fleet block when fleet is disabled", func() {
			kn := testKubernaut()
			cm, err := SignalProcessingConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).NotTo(ContainSubstring("fleet:"), "signalprocessing config should omit fleet block when disabled, got:\n%s", data)
		})

		It("renders fleet.endpoint (not mcpGatewayEndpoint) from spec.fleet.mcpGatewayEndpoint", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			}
			cm, err := SignalProcessingConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("fleet:"), "signalprocessing config should contain fleet block when enabled, got:\n%s", data)
			Expect(data).To(ContainSubstring("endpoint: https://mcp-gateway.example.com/sse"), "signalprocessing fleet.endpoint should carry spec.fleet.mcpGatewayEndpoint's value (upstream field is named 'endpoint', not 'mcpGatewayEndpoint'), got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("mcpGatewayEndpoint:"), "signalprocessing config must not render the mcpGatewayEndpoint key -- upstream FleetConfig.Endpoint has no such key, got:\n%s", data)
			Expect(data).To(ContainSubstring("mcpGatewayType: eaigw"), "signalprocessing config should render mcpGatewayType, got:\n%s", data)
		})

		It("renders fleet.namespace from spec.signalProcessing.mcpGatewayNamespace, overriding the shared spec.fleet.mcpGatewayNamespace", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				MCPGatewayNamespace: testSharedMCPGatewayNamespace,
			}
			kn.Spec.SignalProcessing.MCPGatewayNamespace = testSPMCPGatewayNamespace
			cm, err := SignalProcessingConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("namespace: sp-ns"), "signalprocessing fleet.namespace should use its own override, got:\n%s", data)
		})

		It("falls back to the shared spec.fleet.mcpGatewayNamespace when signalProcessing has no override", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				MCPGatewayNamespace: testSharedMCPGatewayNamespace,
			}
			cm, err := SignalProcessingConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("namespace: shared-ns"), "signalprocessing fleet.namespace should fall back to the shared value, got:\n%s", data)
		})

		It("renders fleet.oauth2.tlsCAFile defaulting to the inter-service CA path when oauth2 is enabled", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha1.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			cm, err := SignalProcessingConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "signalprocessing fleet.oauth2 should render the shared credentialsSecretRef, got:\n%s", data)
			Expect(data).To(ContainSubstring("tlsCAFile: "+InterServiceTLSCAFile), "signalprocessing fleet.oauth2 should default tlsCAFile to the inter-service CA path, got:\n%s", data)
		})

		It("renders signalProcessing.fleetOAuth2CredentialsSecretRef instead of the shared credentialsSecretRef when set", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha1.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			kn.Spec.SignalProcessing.FleetOAuth2CredentialsSecretRef = testSPFleetOAuth2SecretRef
			cm, err := SignalProcessingConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: sp-oauth2-creds"), "signalprocessing config should use its own oauth2 client override, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "signalprocessing config should not fall back to the shared credentialsSecretRef when it has its own override, got:\n%s", data)
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

		It("omits the fleet block when fleet is disabled", func() {
			kn := testKubernaut()
			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).NotTo(ContainSubstring("fleet:"), "RO config should omit fleet block when disabled, got:\n%s", data)
		})

		It("renders the fleet block with backend and endpoint when enabled", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			}
			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			for _, want := range []string{
				"fleet:",
				"enabled: true",
				"backend: fleetmetadatacache",
				"endpoint: https://fmc.kubernaut.svc:8443",
			} {
				Expect(data).To(ContainSubstring(want), "RO config should contain %q when fleet enabled, got:\n%s", want, data)
			}
			Expect(data).NotTo(ContainSubstring("tlsCAFile"), "RO config should omit tlsCAFile when no CA secret set, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("tokenPath"), "RO config should omit tokenPath when no token secret set, got:\n%s", data)
		})

		It("renders tlsCAFile and tokenPath when the corresponding secrets are set", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "acm", Endpoint: "https://acm-search.example.com/graphql",
				CASecretName: "fmc-ca-bundle", TokenSecretName: "acm-search-token",
			}
			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).To(ContainSubstring("tlsCAFile: /etc/fleet-tls/ca/ca.crt"), "RO config should render tlsCAFile mount path, got:\n%s", data)
			Expect(data).To(ContainSubstring("tokenPath: /etc/fleet-token/token"), "RO config should render tokenPath mount path, got:\n%s", data)
		})

		// #222: mcpGatewayEndpoint/Type must be rendered — RemediationOrchestrator
		// crash-loops at startup without them (upstream Fleet.ValidateFullFederation).
		It("renders mcpGatewayEndpoint and mcpGatewayType when set", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			}
			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).To(ContainSubstring("mcpGatewayEndpoint: https://mcp-gateway.example.com/sse"), "RO config should render mcpGatewayEndpoint, got:\n%s", data)
			Expect(data).To(ContainSubstring("mcpGatewayType: eaigw"), "RO config should render mcpGatewayType, got:\n%s", data)
		})

		It("renders the fleet oauth2 block when enabled", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha1.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds", Scopes: []string{"openid", "groups"},
				},
			}
			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			for _, want := range []string{
				"oauth2:",
				"enabled: true",
				"tokenURL: https://keycloak.example.com/token",
				"credentialsSecretRef: fleet-oauth2-creds",
			} {
				Expect(data).To(ContainSubstring(want), "RO config should contain %q when fleet oauth2 enabled, got:\n%s", want, data)
			}
		})

		It("renders oauth2.tlsCAFile defaulting to the inter-service CA path when oauth2 is enabled", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha1.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).To(ContainSubstring("tlsCAFile: "+InterServiceTLSCAFile), "RO oauth2 block should default tlsCAFile to the inter-service CA path, got:\n%s", data)
		})

		It("renders remediationOrchestrator.fleetOAuth2CredentialsSecretRef instead of the shared credentialsSecretRef when set", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha1.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			kn.Spec.RemediationOrchestrator.FleetOAuth2CredentialsSecretRef = testROFleetOAuth2SecretRef
			cm, err := RemediationOrchestratorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: ro-oauth2-creds"), "RO config should use its own oauth2 client override, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "RO config should not fall back to the shared credentialsSecretRef when it has its own override, got:\n%s", data)
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

		// #224: EM reads a remediation's target cluster via the MCP
		// Gateway, reusing the shared fleet.FleetConfig shape (upstream
		// internal/config/effectivenessmonitor.Config.Fleet), never the
		// Backend/Endpoint scope-check adapter.
		It("omits the fleet block when fleet is disabled", func() {
			kn := testKubernaut()
			cm, err := EffectivenessMonitorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).NotTo(ContainSubstring("fleet:"), "EM config should omit fleet block when disabled, got:\n%s", data)
		})

		It("renders mcpGatewayEndpoint/mcpGatewayType but omits backend/endpoint/tokenPath even when spec.fleet.backend/endpoint are set", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				CASecretName: "fmc-ca-bundle", TokenSecretName: "acm-search-token",
			}
			cm, err := EffectivenessMonitorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("fleet:"), "EM config should contain fleet block when enabled, got:\n%s", data)
			Expect(data).To(ContainSubstring("mcpGatewayEndpoint: https://mcp-gateway.example.com/sse"), "EM config should render mcpGatewayEndpoint, got:\n%s", data)
			Expect(data).To(ContainSubstring("mcpGatewayType: eaigw"), "EM config should render mcpGatewayType, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("backend:"), "EM never calls the Backend/Endpoint scope-check adapter -- backend must be omitted even when spec.fleet.backend is set, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("endpoint:"), "EM fleet block must omit endpoint, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("tokenPath:"), "EM fleet block must omit tokenPath, got:\n%s", data)
		})

		It("renders fleet.oauth2.tlsCAFile defaulting to the inter-service CA path when oauth2 is enabled", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha1.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			cm, err := EffectivenessMonitorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("tlsCAFile: "+InterServiceTLSCAFile), "EM fleet.oauth2 should default tlsCAFile to the inter-service CA path, got:\n%s", data)
		})

		It("renders effectivenessMonitor.fleetOAuth2CredentialsSecretRef instead of the shared credentialsSecretRef when set", func() {
			kn := testKubernaut()
			enabled := true
			kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha1.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			kn.Spec.EffectivenessMonitor.FleetOAuth2CredentialsSecretRef = testEMFleetOAuth2SecretRef
			cm, err := EffectivenessMonitorConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: em-oauth2-creds"), "EM config should use its own oauth2 client override, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "EM config should not fall back to the shared credentialsSecretRef when it has its own override, got:\n%s", data)
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
			Expect(data).To(ContainSubstring("alertmanager:"), "KA config should contain upstream tools.alertmanager section when monitoring enabled (#205), got:\n%s", data)
			Expect(data).To(ContainSubstring(OCPAlertManagerURL), "KA config should contain AlertManager URL when monitoring enabled (#205), got:\n%s", data)
		})

		It("omits Prometheus and Alertmanager tools when monitoring is disabled", func() {
			kn := testKubernaut()
			disabled := false
			kn.Spec.Monitoring.Enabled = &disabled
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]

			Expect(strings.Contains(data, "prometheusUrl") || strings.Contains(data, "tools:")).To(BeFalse(), "KA config should not contain Prometheus tools section when monitoring is disabled, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("alertmanager:"), "KA config should not contain alertmanager section when monitoring is disabled (#205), got:\n%s", data)
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
						Alertmanager *struct {
							URL       string `yaml:"url"`
							TLSCaFile string `yaml:"tlsCaFile"`
						} `yaml:"alertmanager,omitempty"`
					} `yaml:"tools,omitempty"`
				} `yaml:"integrations"`
			}
			err = yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)
			Expect(err).NotTo(HaveOccurred())
			Expect(root.Runtime.Logging.Level).To(Equal("info"), "runtime.logging.level = %q, want info", root.Runtime.Logging.Level)
			Expect(root.Runtime.Server.Port == 8443 && root.Runtime.Server.Address == "0.0.0.0").To(BeTrue(), "runtime.server = %#v, want address 0.0.0.0 port 8443", root.Runtime.Server)
			Expect(root.Runtime.Audit.BufferSize).To(Equal(10000), "runtime.audit.bufferSize = %d, want 10000", root.Runtime.Audit.BufferSize)
			Expect(root.AI.LLM.Provider).To(Equal(LLMProviderOpenAI), "ai.llm.provider = %q, want openai", root.AI.LLM.Provider)
			Expect(root.AI.Investigation.MaxTurns).To(Equal(40), "ai.investigation.maxTurns = %d, want 40", root.AI.Investigation.MaxTurns)
			wantDS := DataStorageURL(kn.Namespace)
			Expect(root.Integrations.DataStorage.URL).To(Equal(wantDS), "integrations.dataStorage.url = %q, want %q", root.Integrations.DataStorage.URL, wantDS)
			Expect(root.Integrations.Tools).NotTo(BeNil(), "integrations.tools should be present when monitoring is enabled by default")
			Expect(root.Integrations.Tools.Prometheus.URL).To(Equal(OCPPrometheusURL), "integrations.tools.prometheus.url = %q, want %q", root.Integrations.Tools.Prometheus.URL, OCPPrometheusURL)
			Expect(root.Integrations.Tools.Prometheus.TLSCaFile).To(Equal("/etc/ssl/ka/service-ca.crt"), "integrations.tools.prometheus.tlsCaFile = %q, want /etc/ssl/ka/service-ca.crt", root.Integrations.Tools.Prometheus.TLSCaFile)
			Expect(root.Integrations.Tools.Alertmanager).NotTo(BeNil(), "integrations.tools.alertmanager should be present when monitoring is enabled by default (#205)")
			Expect(root.Integrations.Tools.Alertmanager.URL).To(Equal(OCPAlertManagerURL), "integrations.tools.alertmanager.url = %q, want %q", root.Integrations.Tools.Alertmanager.URL, OCPAlertManagerURL)
			Expect(root.Integrations.Tools.Alertmanager.TLSCaFile).To(Equal("/etc/ssl/ka/service-ca.crt"), "integrations.tools.alertmanager.tlsCaFile = %q, want /etc/ssl/ka/service-ca.crt", root.Integrations.Tools.Alertmanager.TLSCaFile)
		})

		It("renders logging format as JSON", func() {
			kn := testKubernaut()
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				Runtime struct {
					Logging struct {
						Level  string `yaml:"level"`
						Format string `yaml:"format"`
					} `yaml:"logging"`
				} `yaml:"runtime"`
			}
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Runtime.Logging.Format).To(Equal("json"))
		})

		It("renders shutdown.drainSeconds with default 30", func() {
			kn := testKubernaut()
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				Runtime struct {
					Shutdown struct {
						DrainSeconds int `yaml:"drainSeconds"`
					} `yaml:"shutdown"`
				} `yaml:"runtime"`
			}
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Runtime.Shutdown.DrainSeconds).To(Equal(30))
		})

		It("renders custom shutdown.drainSeconds from CR", func() {
			kn := testKubernaut()
			drain := 120
			kn.Spec.KubernautAgent.Shutdown.DrainSeconds = &drain
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				Runtime struct {
					Shutdown struct {
						DrainSeconds int `yaml:"drainSeconds"`
					} `yaml:"shutdown"`
				} `yaml:"runtime"`
			}
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Runtime.Shutdown.DrainSeconds).To(Equal(120))
		})

		It("renders alignment check settings when enabled", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.AlignmentCheck.Enabled = true
			kn.Spec.KubernautAgent.AlignmentCheck.Timeout = "20s"
			kn.Spec.KubernautAgent.AlignmentCheck.MaxStepTokens = 1024
			kn.Spec.KubernautAgent.AlignmentCheck.LLM = &kubernautv1alpha1.AlignmentCheckLLMSpec{
				Provider: LLMProviderOpenAI,
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
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.TLSCaFile = "/etc/custom-ca/llm.pem" })
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
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.Enabled = true })
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.TokenURL = "https://idp.example/oauth/token" })
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.Scopes = []string{"openid", "api.read"} })
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

		It("LR-010 [CM-6]: KA does not spend extra reasoning/thinking tokens unless the administrator explicitly opts in", func() {
			kn := testKubernaut()
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).NotTo(ContainSubstring("reasoning:"), "CM-6: extended reasoning has real cost/latency impact and must stay off by default (ai.llm.reasoning omitted), got:\n%s", data)
		})

		It("LR-011 [CM-6]: KA's reasoning/thinking-token policy exactly matches what the administrator configured on the profile", func() {
			kn := testKubernaut()
			budget := 4096
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
				p.Reasoning = &kubernautv1alpha1.LLMReasoningSpec{
					Enabled:            true,
					BudgetTokens:       &budget,
					Effort:             "high",
					CapabilityOverride: "force_on",
				}
			})
			cm, err := KubernautAgentConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				AI struct {
					LLM struct {
						Reasoning *struct {
							Enabled            bool   `yaml:"enabled"`
							BudgetTokens       int    `yaml:"budgetTokens"`
							Effort             string `yaml:"effort"`
							CapabilityOverride string `yaml:"capabilityOverride"`
						} `yaml:"reasoning"`
					} `yaml:"llm"`
				} `yaml:"ai"`
			}
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.AI.LLM.Reasoning).NotTo(BeNil(), "CM-6: expected ai.llm.reasoning block when the administrator sets Reasoning on the profile")
			Expect(root.AI.LLM.Reasoning.Enabled).To(BeTrue(), "CM-6: enabled must match the administrator's configured value")
			Expect(root.AI.LLM.Reasoning.BudgetTokens).To(Equal(4096), "CM-6: budgetTokens must match the administrator's configured token spend cap")
			Expect(root.AI.LLM.Reasoning.Effort).To(Equal("high"), "CM-6: effort must match the administrator's configured depth")
			Expect(root.AI.LLM.Reasoning.CapabilityOverride).To(Equal("force_on"), "CM-6: capabilityOverride must match the administrator's configured value")
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
				kn.Spec.KubernautAgent.RuntimeConfigMapName = "my-llm-runtime-config"
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
				mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Temperature = "0.5" })
				mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Endpoint = "https://llm-custom.example/v1" })
				maxR := 7
				mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.MaxRetries = &maxR })
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
				kn.Spec.KubernautAgent.RuntimeConfigMapName = "user-llm-runtime"
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				Expect(cm).To(BeNil(), "KubernautAgentLLMRuntimeConfigMap should return nil when runtimeConfigMapName is set (BYO)")
			})

			It("includes phaseModels when configured", func() {
				kn := testKubernaut()
				kn.Spec.LLMProfiles["workflow-lite"] = kubernautv1alpha1.LLMProfileSpec{
					Provider:              "openai",
					Model:                 "claude-haiku-4-6",
					Endpoint:              "http://llm-gateway:8080",
					CredentialsSecretName: "llm-creds",
				}
				kn.Spec.KubernautAgent.PhaseModels = map[string]string{"workflow_discovery": "workflow-lite"}
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["llm-runtime.yaml"]
				Expect(data).To(ContainSubstring("phaseModels:"), "should contain phaseModels key, got:\n%s", data)
				Expect(data).To(ContainSubstring("workflow_discovery:"), "should contain workflow_discovery phase, got:\n%s", data)
				Expect(data).To(ContainSubstring("model: claude-haiku-4-6"), "should contain haiku model, got:\n%s", data)
			})

			It("omits phaseModels when not configured", func() {
				kn := testKubernaut()
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["llm-runtime.yaml"]
				Expect(data).NotTo(ContainSubstring("phaseModels"), "should not contain phaseModels when empty, got:\n%s", data)
			})

			It("propagates all override fields for a phase", func() {
				kn := testKubernaut()
				kn.Spec.LLMProfiles["rca-anthropic"] = kubernautv1alpha1.LLMProfileSpec{
					Provider:              "anthropic",
					Model:                 "claude-sonnet-4-6",
					Endpoint:              "https://api.anthropic.com",
					CredentialsSecretName: "llm-creds",
				}
				kn.Spec.KubernautAgent.PhaseModels = map[string]string{"rca": "rca-anthropic"}
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["llm-runtime.yaml"]
				for _, want := range []string{
					"provider: anthropic",
					"model: claude-sonnet-4-6",
					"endpoint: https://api.anthropic.com",
				} {
					Expect(data).To(ContainSubstring(want), "phase override should contain %q, got:\n%s", want, data)
				}
			})

			It("LR-020 [CM-6]: the base profile's reasoning policy is static-only and does not leak into the hot-reloadable runtime config", func() {
				kn := testKubernaut()
				mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
					p.Reasoning = &kubernautv1alpha1.LLMReasoningSpec{Enabled: true, Effort: "high"}
				})
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["llm-runtime.yaml"]
				Expect(data).NotTo(ContainSubstring("reasoning:"), "CM-6: base profile's reasoning is static-only (matches upstream LLMRuntimeConfig, which has no top-level Reasoning field) — it must not appear where an operator could mistake it for a hot-reloadable setting, got:\n%s", data)
			})

			It("LR-021 [CM-6]: a workflow phase's reasoning budget is independently configurable from the base agent's, so per-phase cost/latency tuning actually takes effect", func() {
				kn := testKubernaut()
				mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
					p.Reasoning = &kubernautv1alpha1.LLMReasoningSpec{Enabled: true, Effort: "high"}
				})
				kn.Spec.LLMProfiles["workflow_discovery_profile"] = kubernautv1alpha1.LLMProfileSpec{
					Provider:              "anthropic",
					Model:                 "claude-sonnet-4-6",
					CredentialsSecretName: "llm-creds",
					Reasoning:             &kubernautv1alpha1.LLMReasoningSpec{Enabled: true, Effort: "low"},
				}
				kn.Spec.KubernautAgent.PhaseModels = map[string]string{"workflow_discovery": "workflow_discovery_profile"}
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				var root struct {
					PhaseModels map[string]struct {
						Reasoning *struct {
							Enabled bool   `yaml:"enabled"`
							Effort  string `yaml:"effort"`
						} `yaml:"reasoning"`
					} `yaml:"phaseModels"`
				}
				Expect(yaml.Unmarshal([]byte(cm.Data["llm-runtime.yaml"]), &root)).To(Succeed())
				phase, ok := root.PhaseModels["workflow_discovery"]
				Expect(ok).To(BeTrue(), "expected phaseModels.workflow_discovery entry")
				Expect(phase.Reasoning).NotTo(BeNil(), "CM-6: expected phaseModels.workflow_discovery.reasoning when its own profile sets Reasoning — without this, a hot-reload phase override pointing at a lighter-weight reasoning profile would silently not apply at runtime")
				Expect(phase.Reasoning.Effort).To(Equal("low"), "CM-6: phase override's reasoning.effort must reflect its own profile ('low'), not the base agent's ('high'), or the administrator's per-phase cost tuning is ineffective")
			})

			It("LR-022 [CM-6]: a phase that opts out of reasoning stays opted out, even when the base agent has reasoning enabled", func() {
				kn := testKubernaut()
				mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
					p.Reasoning = &kubernautv1alpha1.LLMReasoningSpec{Enabled: true, Effort: "high"}
				})
				kn.Spec.LLMProfiles["validation_profile"] = kubernautv1alpha1.LLMProfileSpec{
					Provider:              "anthropic",
					Model:                 "claude-sonnet-4-6",
					CredentialsSecretName: "llm-creds",
				}
				kn.Spec.KubernautAgent.PhaseModels = map[string]string{"validation": "validation_profile"}
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				var root struct {
					PhaseModels map[string]struct {
						Reasoning *struct{} `yaml:"reasoning"`
					} `yaml:"phaseModels"`
				}
				Expect(yaml.Unmarshal([]byte(cm.Data["llm-runtime.yaml"]), &root)).To(Succeed())
				phase, ok := root.PhaseModels["validation"]
				Expect(ok).To(BeTrue(), "expected phaseModels.validation entry")
				Expect(phase.Reasoning).To(BeNil(), "CM-6: phaseModels.validation.reasoning must stay absent when its own profile has none, even though the base agent's does — a phase-specific profile swap must not inherit reasoning spend it never opted into")
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
			Entry("apifrontend-service-ca", APIFrontendServiceCAConfigMap),
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
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
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
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("port: 8443"))
		Expect(data).NotTo(ContainSubstring("issuerURL: https://"))
	})

	It("disables severityTriage when monitoring is disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.Monitoring.Enabled = &disabled
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("enabled: false"))
		Expect(data).NotTo(ContainSubstring("thanos-querier"),
			"disabled severityTriage should not reference Thanos Querier URL")
	})

	It("uses OCP service-ca for severity triage when monitoring is enabled", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("prometheusTlsCaFile: /etc/ssl/af/service-ca.crt"))
	})

	It("renders auth issuerURL and audience from spec", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("https://login.kubernaut.ai/realms/kubernaut"))
		Expect(data).To(ContainSubstring("kubernaut-apifrontend"))
	})

	It("hardcodes agent card name to Kubernaut Agent", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("name: Kubernaut Agent"))
	})

	It("sets session.namespace to the CR namespace for prompt context", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("namespace: kubernaut-system"),
			"session.namespace must be set so AF BuildInstruction injects deployment context into the prompt")
	})

	It("keeps server.port at 8443 for authbridge sidecar (kagenti 0.3.x)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarAuthbridge, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("port: 8443"),
			"AF declares 8443; kagenti webhook shifts AF to 8444 and authbridge takes 8443")
	})

	It("keeps server.port at 8443 for envoy sidecar (kagenti 0.2.x)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarEnvoy, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("port: 8443"),
			"envoy sidecar uses iptables; AF keeps original port")
	})

	It("disables AF TLS for authbridge sidecar", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarAuthbridge, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("certDir: \"\""))
		Expect(data).To(ContainSubstring("required: false"))
	})

	It("disables AF TLS for envoy sidecar", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarEnvoy, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("certDir: \"\""))
		Expect(data).To(ContainSubstring("required: false"))
	})

	It("enables AF TLS when no sidecar is active", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("certDir: /etc/apifrontend/tls"))
		Expect(data).To(ContainSubstring("required: true"))
	})

	It("renders rate limit defaults", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("ipRequestsPerSec: 50"))
		Expect(data).To(ContainSubstring("userRequestsPerSec: 20"))
	})

	It("renders resilience circuit breaker config", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("cbFailureThreshold:"))
		Expect(data).To(ContainSubstring("retryMax:"))
	})

	It("renders replayCache when Valkey secret is set", func() {
		kn := testKubernautWithAF()
		kn.Spec.Valkey.SecretName = "my-valkey-secret"
		kn.Spec.Valkey.Host = "valkey.kubernaut-system.svc.cluster.local"
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("replayCache:"))
	})

	It("renders nested agent.llm config section", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
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
		Expect(root.Agent.LLM.Provider).To(Equal("openai_compatible"), "agent.llm.provider = %q, want openai_compatible (kubernaut#1487)", root.Agent.LLM.Provider)
		Expect(root.Agent.LLM.Model).To(Equal("gpt-4o"), "agent.llm.model = %q, want gpt-4o", root.Agent.LLM.Model)
		Expect(root.Agent.LLM.APIKeyFile).To(Equal("/etc/apifrontend/llm-credentials/api_key"),
			"agent.llm.apiKeyFile should point to mounted secret")
	})

	It("does not emit flat llmEndpoint or llmModel fields", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("llmEndpoint:"), "flat llmEndpoint field should not be emitted")
		Expect(data).NotTo(ContainSubstring("llmModel:"), "flat llmModel field should not be emitted")
	})

	It("renders Vertex AI fields in agent.llm config without apiKeyFile", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Provider = LLMProviderVertexAI })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Model = "gemini-2.5-pro" })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.VertexProject = "my-project" })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.VertexLocation = testVertexLocation })
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]

		var root struct {
			Agent struct {
				LLM struct {
					Provider       string `yaml:"provider"`
					Model          string `yaml:"model"`
					APIKeyFile     string `yaml:"apiKeyFile"`
					VertexProject  string `yaml:"vertexProject"`
					VertexLocation string `yaml:"vertexLocation"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		err = yaml.Unmarshal([]byte(data), &root)
		Expect(err).NotTo(HaveOccurred())
		Expect(root.Agent.LLM.Provider).To(Equal(LLMProviderVertexAI))
		Expect(root.Agent.LLM.Model).To(Equal("gemini-2.5-pro"))
		Expect(root.Agent.LLM.VertexProject).To(Equal("my-project"))
		Expect(root.Agent.LLM.VertexLocation).To(Equal(testVertexLocation))
		Expect(root.Agent.LLM.APIKeyFile).To(BeEmpty(),
			"vertex_ai should use GOOGLE_APPLICATION_CREDENTIALS (ADC), not apiKeyFile")
	})

	It("UT-CM-196-001 [SI-10]: AF receives openai_compatible when CR specifies openai", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Provider = LLMProviderOpenAI })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Endpoint = testOpenAIEndpoint })
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					Provider string `yaml:"provider"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.Provider).To(Equal("openai_compatible"),
			"AF must receive openai_compatible for openai provider (kubernaut#1487)")
	})

	It("UT-CM-196-002 [CM-6]: AF endpoint gets /v1 suffix appended", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Provider = LLMProviderOpenAI })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Endpoint = testOpenAIEndpoint })
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					Endpoint string `yaml:"endpoint"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.Endpoint).To(Equal(testOpenAIEndpoint+"/v1"),
			"AF OpenAI adapter requires /v1 suffix on endpoint")
	})

	It("UT-CM-196-003 [CM-6]: AF endpoint not doubled when /v1 already present", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Provider = LLMProviderOpenAI })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Endpoint = testOpenAIEndpoint + "/v1" })
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					Endpoint string `yaml:"endpoint"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.Endpoint).To(Equal(testOpenAIEndpoint+"/v1"),
			"/v1 suffix must not be doubled")
	})

	It("UT-CM-196-004 [CM-6]: AF endpoint trailing slash handled before /v1 append", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Provider = LLMProviderOpenAI })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Endpoint = testOpenAIEndpoint + "/" })
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					Endpoint string `yaml:"endpoint"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.Endpoint).To(Equal(testOpenAIEndpoint+"/v1"),
			"trailing slash must be normalized before appending /v1")
	})

	It("UT-CM-196-005 [CM-6]: KA gets raw openai provider, no endpoint mutation", func() {
		kn := testKubernaut()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Provider = LLMProviderOpenAI })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Endpoint = testOpenAIEndpoint })
		cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Provider string `yaml:"provider"`
			Endpoint string `yaml:"endpoint"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["llm-runtime.yaml"]), &root)).To(Succeed())
		Expect(root.Provider).To(Equal(LLMProviderOpenAI),
			"KA must receive raw openai provider (KA handles translation internally)")
		Expect(root.Endpoint).To(Equal(testOpenAIEndpoint),
			"KA endpoint must not be mutated (KA appends /v1 internally)")
	})

	It("UT-CM-196-006 [CM-6]: non-OpenAI providers are not translated", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Provider = LLMProviderVertexAI })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.VertexProject = "my-project" })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.VertexLocation = testVertexLocation })
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					Provider string `yaml:"provider"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.Provider).To(Equal(LLMProviderVertexAI),
			"non-OpenAI providers must pass through untranslated")
	})

	It("UT-CM-196-007 [SC-7]: AF apiKeyFile set for OpenAI provider", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Provider = LLMProviderOpenAI })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Endpoint = testOpenAIEndpoint })
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					APIKeyFile string `yaml:"apiKeyFile"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.APIKeyFile).To(Equal("/etc/apifrontend/llm-credentials/api_key"),
			"apiKeyFile must be set for OpenAI provider (secret always mounted)")
	})

	It("renders OAuth2 block in agent.llm when enabled", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.Enabled = true })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.TokenURL = "https://idp.example/oauth/token" })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.Scopes = []string{"openid", "llm.invoke"} })
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("oauth2:"))
	})

	It("LR-030 [CM-6]: AF does not spend extra reasoning/thinking tokens unless the administrator explicitly opts in", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("reasoning:"), "CM-6: extended reasoning has real cost/latency impact and must stay off by default (agent.llm.reasoning omitted), got:\n%s", data)
	})

	It("LR-031 [CM-6]: AF's reasoning/thinking-token policy exactly matches what the administrator configured on the profile", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
			p.Reasoning = &kubernautv1alpha1.LLMReasoningSpec{Enabled: true, Effort: "medium", CapabilityOverride: "force_off"}
		})
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					Reasoning *struct {
						Enabled            bool   `yaml:"enabled"`
						Effort             string `yaml:"effort"`
						CapabilityOverride string `yaml:"capabilityOverride"`
					} `yaml:"reasoning"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.Reasoning).NotTo(BeNil(), "CM-6: expected agent.llm.reasoning block when the administrator sets Reasoning on the profile")
		Expect(root.Agent.LLM.Reasoning.Enabled).To(BeTrue(), "CM-6: enabled must match the administrator's configured value")
		Expect(root.Agent.LLM.Reasoning.Effort).To(Equal("medium"), "CM-6: effort must match the administrator's configured depth")
		Expect(root.Agent.LLM.Reasoning.CapabilityOverride).To(Equal("force_off"), "CM-6: capabilityOverride must match the administrator's configured value")
	})

	It("renders AF's own resolved profile when apiFrontend.llmProfileRef differs from KA's", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles[testAFOnlyProfile] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              LLMProviderVertexAI,
			Model:                 "gemini-2.5-flash",
			VertexProject:         "af-project",
			VertexLocation:        "europe-west1",
			CredentialsSecretName: "af-llm-creds",
		}
		kn.Spec.APIFrontend.LLMProfileRef = testAFOnlyProfile
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					Provider      string `yaml:"provider"`
					Model         string `yaml:"model"`
					VertexProject string `yaml:"vertexProject"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.Provider).To(Equal(LLMProviderVertexAI), "AF must render its own profile, not KA's (openai/gpt-4o)")
		Expect(root.Agent.LLM.Model).To(Equal("gemini-2.5-flash"))
		Expect(root.Agent.LLM.VertexProject).To(Equal("af-project"))
	})

	It("defaults AF's LLM profile to KA's when apiFrontend.llmProfileRef is empty", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.LLMProfileRef = ""
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Model = "gpt-4o-mini" })
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					Model string `yaml:"model"`
				} `yaml:"llm"`
			} `yaml:"agent"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.Model).To(Equal("gpt-4o-mini"), "empty apiFrontend.llmProfileRef must default to KA's resolved profile")
	})

	It("severityTriage.llm is omitted by default, inheriting AF's agent.llm connection", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			SeverityTriage struct {
				LLM *struct{} `yaml:"llm"`
			} `yaml:"severityTriage"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.LLM).To(BeNil(), "default severityTriage.llmProfileRef must omit the llm key so triage inherits agent.llm")
	})

	It("severityTriage.llm is present-but-empty when llmEnabled is false, forcing the rule-based-only fallback", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{LLMEnabled: &disabled}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("llm:"), "llmEnabled=false must still render a present llm key (non-nil, empty) to force upstream's Noop triager")
		var root struct {
			SeverityTriage struct {
				LLM *struct {
					Provider string `yaml:"provider"`
				} `yaml:"llm"`
			} `yaml:"severityTriage"`
		}
		Expect(yaml.Unmarshal([]byte(data), &root)).To(Succeed())
		Expect(root.SeverityTriage.LLM).NotTo(BeNil())
		Expect(root.SeverityTriage.LLM.Provider).To(BeEmpty())
	})

	It("severityTriage.llm renders an independent profile when llmProfileRef is set", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["triage-profile"] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              "anthropic",
			Model:                 "claude-haiku-4-6",
			Endpoint:              "https://api.anthropic.com",
			CredentialsSecretName: "llm-creds",
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{LLMProfileRef: "triage-profile"}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					Provider string `yaml:"provider"`
				} `yaml:"llm"`
			} `yaml:"agent"`
			SeverityTriage struct {
				LLM *struct {
					Provider string `yaml:"provider"`
					Model    string `yaml:"model"`
				} `yaml:"llm"`
			} `yaml:"severityTriage"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.LLM).NotTo(BeNil())
		Expect(root.SeverityTriage.LLM.Provider).To(Equal("anthropic"))
		Expect(root.SeverityTriage.LLM.Model).To(Equal("claude-haiku-4-6"))
		Expect(root.Agent.LLM.Provider).NotTo(Equal("anthropic"), "triage's independent profile must not leak into AF's main agent.llm")
	})

	It("LR-032 [CM-6]: severity-triage's reasoning budget is independently configurable from AF's main agent, so triage cost/latency can be tuned separately", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
			p.Reasoning = &kubernautv1alpha1.LLMReasoningSpec{Enabled: true, Effort: "high"}
		})
		kn.Spec.LLMProfiles["triage-profile"] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              "anthropic",
			Model:                 "claude-haiku-4-6",
			Endpoint:              "https://api.anthropic.com",
			CredentialsSecretName: "llm-creds",
			Reasoning:             &kubernautv1alpha1.LLMReasoningSpec{Enabled: true, Effort: "minimal"},
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{LLMProfileRef: "triage-profile"}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			Agent struct {
				LLM struct {
					Reasoning *struct {
						Effort string `yaml:"effort"`
					} `yaml:"reasoning"`
				} `yaml:"llm"`
			} `yaml:"agent"`
			SeverityTriage struct {
				LLM *struct {
					Reasoning *struct {
						Effort string `yaml:"effort"`
					} `yaml:"reasoning"`
				} `yaml:"llm"`
			} `yaml:"severityTriage"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Agent.LLM.Reasoning).NotTo(BeNil())
		Expect(root.Agent.LLM.Reasoning.Effort).To(Equal("high"))
		Expect(root.SeverityTriage.LLM).NotTo(BeNil())
		Expect(root.SeverityTriage.LLM.Reasoning).NotTo(BeNil(), "CM-6: expected severityTriage.llm.reasoning when triage's own profile sets Reasoning — without this, an administrator cannot dial down triage's reasoning spend independently of the main agent's")
		Expect(root.SeverityTriage.LLM.Reasoning.Effort).To(Equal("minimal"), "CM-6: triage's reasoning.effort must reflect its own profile ('minimal'), not AF's main agent.llm.reasoning ('high')")
	})

	// #224: AF backs the list_clusters MCP tool and routes remote reads via
	// a ClusterRegistry, reusing the shared fleet.FleetConfig shape GW/RO
	// already use (upstream pkg/apifrontend/config.Config.Fleet), but AF
	// never calls the Backend/Endpoint scope-check adapter -- see
	// FleetConfig.Validate()'s own doc comment.
	It("omits the fleet block when fleet is disabled", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("fleet:"), "apifrontend config should omit fleet block when disabled, got:\n%s", data)
	})

	It("renders mcpGatewayEndpoint/mcpGatewayType but omits backend/endpoint/tokenPath even when spec.fleet.backend/endpoint are set", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			CASecretName: "fmc-ca-bundle", TokenSecretName: "acm-search-token",
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("fleet:"), "apifrontend config should contain fleet block when enabled, got:\n%s", data)
		Expect(data).To(ContainSubstring("mcpGatewayEndpoint: https://mcp-gateway.example.com/sse"), "apifrontend config should render mcpGatewayEndpoint, got:\n%s", data)
		Expect(data).To(ContainSubstring("mcpGatewayType: eaigw"), "apifrontend config should render mcpGatewayType, got:\n%s", data)
		// fleet:'s own keys are 2-space indented; other "backend:"/"endpoint:"
		// substrings exist elsewhere in AF's config (e.g. auth.replayCache.backend)
		// at deeper indentation, so match on indentation to target fleet: only.
		Expect(data).NotTo(ContainSubstring("\n  backend:"), "apifrontend never calls the Backend/Endpoint scope-check adapter -- backend must be omitted even when spec.fleet.backend is set, got:\n%s", data)
		Expect(data).NotTo(ContainSubstring("\n  endpoint:"), "apifrontend fleet block must omit endpoint (AF has no Backend/Endpoint adapter use), got:\n%s", data)
		Expect(data).NotTo(ContainSubstring("\n  tokenPath:"), "apifrontend fleet block must omit tokenPath (no Backend/Endpoint adapter use), got:\n%s", data)
	})

	It("renders fleet.oauth2 with tlsCAFile defaulting to AF's own inter-service CA path", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			OAuth2: kubernautv1alpha1.OAuth2Spec{
				Enabled: true, TokenURL: "https://keycloak.example.com/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "apifrontend fleet.oauth2 should render the shared credentialsSecretRef, got:\n%s", data)
		Expect(data).To(ContainSubstring("tlsCAFile: "+apifrontendTLSCAFile), "apifrontend fleet.oauth2 should default tlsCAFile to AF's own CA mount path, got:\n%s", data)
	})

	It("renders apiFrontend.fleetOAuth2CredentialsSecretRef instead of the shared credentialsSecretRef when set", func() {
		kn := testKubernautWithAF()
		enabled := true
		kn.Spec.Fleet = kubernautv1alpha1.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			OAuth2: kubernautv1alpha1.OAuth2Spec{
				Enabled: true, TokenURL: "https://keycloak.example.com/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}
		kn.Spec.APIFrontend.FleetOAuth2CredentialsSecretRef = testAFFleetOAuth2SecretRef
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("credentialsSecretRef: af-oauth2-creds"), "apifrontend config should use its own oauth2 client override, got:\n%s", data)
		Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "apifrontend config should not fall back to the shared credentialsSecretRef when it has its own override, got:\n%s", data)
	})
})

var _ = Describe("APIFrontendConfigMap OIDC", func() {
	It("IA-5: propagates jwksURL to AF config for explicit JWKS endpoint trust", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWKSURL = "https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs"
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("jwksURL: https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs"),
			"IA-5: jwksURL must be propagated for explicit JWKS endpoint configuration")
	})

	It("IA-5: propagates oidcCaFile to AF config for OIDC CA verification", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.OIDCCAFile = "/etc/pki/tls/certs/oidc-ca.crt"
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("oidcCaFile: /etc/pki/tls/certs/oidc-ca.crt"),
			"IA-5: oidcCaFile must be propagated for OIDC provider CA trust")
	})

	It("IA-5: omits allowInsecureIssuers by default (secure-by-default)", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("allowInsecureIssuers: true"),
			"IA-5: allowInsecureIssuers must default to false (secure-by-default)")
	})

	It("SC-8: propagates allowInsecureIssuers when explicitly enabled", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.AllowInsecureIssuers = true
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("allowInsecureIssuers: true"),
			"SC-8: allowInsecureIssuers must be propagated when explicitly set")
	})

	It("SC-23: propagates audience claim for token binding validation", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.Audience = "custom-audience"
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("audience: custom-audience"),
			"SC-23: audience claim must be propagated for token binding")
	})
})

var _ = Describe("APIFrontendConfigMap kagenti OIDC auto-detection", func() {
	It("IA-2: uses kagenti-detected issuerURL when CR field is empty", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		oidc := &KagentiOIDCDefaults{
			IssuerURL:            "https://keycloak.example.com/realms/kagenti",
			JWKSURL:              "http://keycloak-service.keycloak.svc:8080/realms/kagenti/protocol/openid-connect/certs",
			AllowInsecureIssuers: true,
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarAuthbridge, oidc)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("issuerURL: https://keycloak.example.com/realms/kagenti"),
			"IA-2: AF must authenticate against the kagenti-detected issuer")
		Expect(data).To(ContainSubstring("jwksURL: http://keycloak-service.keycloak.svc:8080/realms/kagenti/protocol/openid-connect/certs"),
			"IA-5: JWKS endpoint must point to in-cluster Keycloak for secure key retrieval")
		Expect(data).To(ContainSubstring("allowInsecureIssuers: true"),
			"SC-8: allowInsecureIssuers required when in-cluster JWKS uses HTTP")
	})

	It("CM-6: CR issuerURL takes precedence over kagenti-detected value", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = "https://custom-idp.example.com/realms/custom"
		oidc := &KagentiOIDCDefaults{
			IssuerURL:            "https://keycloak.example.com/realms/kagenti",
			JWKSURL:              "http://keycloak-service.keycloak.svc:8080/realms/kagenti/protocol/openid-connect/certs",
			AllowInsecureIssuers: true,
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarAuthbridge, oidc)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("issuerURL: https://custom-idp.example.com/realms/custom"),
			"CM-6: explicit CR value must override auto-detected issuerURL")
		Expect(data).NotTo(ContainSubstring("issuerURL: https://keycloak.example.com/realms/kagenti"))
	})

	It("CM-6: CR jwksURL takes precedence over kagenti-detected value", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWKSURL = "https://custom-jwks.example.com/certs"
		oidc := &KagentiOIDCDefaults{
			IssuerURL: "https://keycloak.example.com/realms/kagenti",
			JWKSURL:   "http://keycloak-service.keycloak.svc:8080/realms/kagenti/protocol/openid-connect/certs",
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarAuthbridge, oidc)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("jwksURL: https://custom-jwks.example.com/certs"),
			"CM-6: explicit CR jwksURL must override auto-detected value")
	})

	It("SC-8: CR allowInsecureIssuers=true overrides secure detection", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.AllowInsecureIssuers = true
		oidc := &KagentiOIDCDefaults{
			IssuerURL:            "https://keycloak.example.com/realms/kagenti",
			AllowInsecureIssuers: false,
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarAuthbridge, oidc)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("allowInsecureIssuers: true"),
			"SC-8: explicit CR allowInsecureIssuers must be honored")
	})

	It("IA-2: nil OIDC defaults produce unchanged behavior", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("issuerURL: https://login.kubernaut.ai/realms/kubernaut"),
			"IA-2: without kagenti, AF must use the CR-specified issuerURL")
		Expect(data).NotTo(ContainSubstring("allowInsecureIssuers: true"),
			"SC-8: allowInsecureIssuers must default to false without kagenti")
	})

	It("IA-2: works with envoy sidecar mode (kagenti 0.2.x)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		oidc := &KagentiOIDCDefaults{
			IssuerURL: "https://keycloak.example.com/realms/kagenti",
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarEnvoy, oidc)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("issuerURL: https://keycloak.example.com/realms/kagenti"),
			"IA-2: auto-detection must work for both envoy and authbridge sidecar modes")
	})
})

var _ = Describe("IA-2: AF multi-provider JWT config emission", func() {
	It("IA-2: emits jwtProviders array enabling concurrent multi-issuer token validation", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
			{
				Name:      "keycloak",
				IssuerURL: "https://keycloak.example.com/realms/kubernaut",
				JWKSURL:   "https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs",
				Audiences: []string{"kubernaut-console"},
			},
			{
				Name:      "spire",
				IssuerURL: "https://spire.example.com",
				Audiences: []string{"kubernaut-workload"},
			},
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]

		Expect(data).To(ContainSubstring("jwtProviders:"),
			"IA-2: config must contain jwtProviders array for multi-issuer validation")

		Expect(data).To(ContainSubstring("name: keycloak"),
			"IA-2: first provider name must be keycloak")
		Expect(data).To(ContainSubstring("issuerURL: https://keycloak.example.com/realms/kubernaut"),
			"IA-2: keycloak issuerURL must be propagated")
		Expect(data).To(ContainSubstring("jwksURL: https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs"),
			"IA-2: keycloak jwksURL must be propagated")

		Expect(data).To(ContainSubstring("name: spire"),
			"IA-2: second provider name must be spire")
		Expect(data).To(ContainSubstring("issuerURL: https://spire.example.com"),
			"IA-2: spire issuerURL must be propagated")
	})

	It("IA-2: omits jwtProviders when single-provider legacy path is used", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("jwtProviders:"),
			"IA-2: jwtProviders must not appear when no multi-provider config is set")
	})
})

var _ = Describe("AC-6: claim-based authorization config", func() {
	It("AC-6: propagates claim mappings enabling group-based tool authorization", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
			{
				Name:      "keycloak",
				IssuerURL: "https://keycloak.example.com/realms/kubernaut",
				Audiences: []string{"kubernaut-console"},
				ClaimMappings: &kubernautv1alpha1.ClaimMappingsSpec{
					Username: "preferred_username",
					Groups:   "realm_roles",
				},
			},
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("username: preferred_username"),
			"AC-6: username claim mapping must be propagated for identity extraction")
		Expect(data).To(ContainSubstring("groups: realm_roles"),
			"AC-6: groups claim mapping must be propagated for tool authorization")
	})

	It("AC-6: omits claim mappings when not configured — AF falls back to default claim extraction", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
			{
				Name:      "spire",
				IssuerURL: "https://spire.example.com",
				Audiences: []string{"kubernaut-workload"},
			},
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("claimMappings:"),
			"AC-6: claimMappings must be omitted when not configured to avoid overriding AF defaults")
	})
})

var _ = Describe("SC-23: per-provider audience config", func() {
	It("SC-23: emits audiences array per provider for audience-scoped token validation", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
			{
				Name:      "keycloak",
				IssuerURL: "https://keycloak.example.com/realms/kubernaut",
				Audiences: []string{"kubernaut-console", "kubernaut-api"},
			},
		}
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("kubernaut-console"),
			"SC-23: first audience must be propagated")
		Expect(data).To(ContainSubstring("kubernaut-api"),
			"SC-23: second audience must be propagated")
	})
})

var _ = Describe("APIFrontendConfigMap SAR", func() {
	It("includes rbac.sarCacheTTL with default 30s", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, KagentiSidecarNone, nil)
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

var _ = Describe("injectConsoleAudience", func() {
	It("UT-CA-01 [AC-4, CC6.1]: appends console audience to providers that lack it", func() {
		providers := []afJWTProviderYAML{
			{Name: "keycloak", Audiences: []string{"apifrontend"}},
		}
		injectConsoleAudience(providers)
		Expect(providers[0].Audiences).To(ContainElement(ComponentConsole))
		Expect(providers[0].Audiences).To(HaveLen(2))
	})

	It("UT-CA-02 [AC-4, CC6.1]: idempotent when console audience already present", func() {
		providers := []afJWTProviderYAML{
			{Name: "keycloak", Audiences: []string{"apifrontend", ComponentConsole}},
		}
		injectConsoleAudience(providers)
		count := 0
		for _, a := range providers[0].Audiences {
			if a == ComponentConsole {
				count++
			}
		}
		Expect(count).To(Equal(1))
	})

	It("UT-CA-03 [AC-4, CC6.1]: mixed multi-provider injects only where missing", func() {
		providers := []afJWTProviderYAML{
			{Name: "provider-a", Audiences: []string{"apifrontend"}},
			{Name: "provider-b", Audiences: []string{"apifrontend", ComponentConsole}},
			{Name: "provider-c", Audiences: []string{"other"}},
		}
		injectConsoleAudience(providers)
		Expect(providers[0].Audiences).To(ContainElement(ComponentConsole))
		Expect(providers[1].Audiences).To(HaveLen(2))
		Expect(providers[2].Audiences).To(ContainElement(ComponentConsole))
	})

	It("UT-CA-04 [SI-10]: empty provider list causes no mutation or panic", func() {
		var providers []afJWTProviderYAML
		Expect(func() { injectConsoleAudience(providers) }).NotTo(Panic())
	})
})

var _ = Describe("kaRateLimitFromSpec", func() {
	It("UT-RL-01 [SC-5, CC6.6]: nil spec produces safe defaults", func() {
		rl := kaRateLimitFromSpec(nil)
		Expect(rl.RequestsPerSecond).To(Equal(50))
		Expect(rl.Burst).To(Equal(100))
	})

	It("UT-RL-02 [SC-5, CC6.6]: partial override applies RPS only", func() {
		rps := 10
		rl := kaRateLimitFromSpec(&kubernautv1alpha1.KARateLimitSpec{
			RequestsPerSecond: &rps,
		})
		Expect(rl.RequestsPerSecond).To(Equal(10))
		Expect(rl.Burst).To(Equal(100))
	})

	It("UT-RL-03 [SC-5, CC6.6]: partial override applies burst only", func() {
		burst := 200
		rl := kaRateLimitFromSpec(&kubernautv1alpha1.KARateLimitSpec{
			Burst: &burst,
		})
		Expect(rl.RequestsPerSecond).To(Equal(50))
		Expect(rl.Burst).To(Equal(200))
	})

	It("UT-RL-04 [CM-6, CC8.1]: both fields set overrides all defaults", func() {
		rps := 25
		burst := 50
		rl := kaRateLimitFromSpec(&kubernautv1alpha1.KARateLimitSpec{
			RequestsPerSecond: &rps,
			Burst:             &burst,
		})
		Expect(rl.RequestsPerSecond).To(Equal(25))
		Expect(rl.Burst).To(Equal(50))
	})
})

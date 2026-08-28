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
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const injectCABundleAnnotationValue = "true"

const testOpenAIEndpoint = "http://llm-gateway:8080"

var _ = Describe("ConfigMaps", func() {
	Describe("Gateway ConfigMap", func() {
		It("contains DataStorage URL and expected keys", func() {
			kn := testKubernaut()
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
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
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("tls:"))
			Expect(data).To(ContainSubstring("certDir: /etc/tls"))
		})

		It("respects custom K8s request timeout", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Config.K8sRequestTimeout = "30s"
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("k8sRequestTimeout: 30s"))
		})

		It("renders v1.4 processing and related fields", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Logging.Level = "debug"
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
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
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(strings.Contains(data, "trustedProxyCIDRs") && strings.Contains(data, "10.0.0.0/8")).To(BeTrue(), "gateway config should contain trustedProxyCIDRs with 10.0.0.0/8, got:\n%s", data)
		})

		It("renders custom deduplication cooldown", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Config.DeduplicationCooldown = "10m" //nolint:goconst // test value
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("cooldownPeriod: 10m"), "gateway config should contain cooldownPeriod 10m, got:\n%s", data)
		})

		It("renders default CORS config", func() {
			kn := testKubernaut()
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
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
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("https://dashboard.example.com"))
			Expect(data).To(ContainSubstring("https://admin.example.com"))
			Expect(data).NotTo(ContainSubstring("no-browser-clients"))
		})

		It("[SC-8] renders custom CORS credentials and maxAge", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Config.CORS.AllowCredentials = ptr.To(true)
			kn.Spec.Gateway.Config.CORS.MaxAge = ptr.To(600)
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("allowCredentials: true"))
			Expect(data).To(ContainSubstring("maxAge: 600"))
		})

		// #423 coverage backfill: config.cors.allowedMethods had zero test
		// references anywhere in the codebase.
		It("[SC-8] renders custom CORS allowedMethods, overriding the default method list", func() {
			kn := testKubernaut()
			kn.Spec.Gateway.Config.CORS.AllowedMethods = []string{"GET", "POST"}
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("allowedMethods:"))
			Expect(data).To(ContainSubstring("- GET"))
			Expect(data).To(ContainSubstring("- POST"))
			Expect(data).NotTo(ContainSubstring("- PUT"), "custom allowedMethods should fully replace the default method list, got:\n%s", data)
		})

		It("omits the fleet block when fleet is disabled", func() {
			kn := testKubernaut()
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).NotTo(ContainSubstring("fleet:"), "gateway config should omit fleet block when disabled, got:\n%s", data)
		})

		It("renders the fleet block with backend and endpoint when enabled", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			}
			cm, err := GatewayConfigMap(kn, knV2)
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
			Expect(data).To(ContainSubstring("tlsCAFile: "+InterServiceTLSCAFile),
				"gateway config should default tlsCAFile to the inter-service trust-bundle path when no explicit CA secret is set (fleet.Endpoint is typically an in-cluster, service-ca-signed Service), got:\n%s", data)
		})

		It("renders tlsCAFile and tokenPath when the corresponding secrets are set", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "acm", Endpoint: "https://acm-search.example.com/graphql",
				CASecretName: "fmc-ca-bundle", TokenSecretName: "acm-search-token",
			}
			cm, err := GatewayConfigMap(kn, knV2)
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
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			}
			cm, err := GatewayConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("mcpGatewayEndpoint: https://mcp-gateway.example.com/sse"), "gateway config should render mcpGatewayEndpoint, got:\n%s", data)
			Expect(data).To(ContainSubstring("mcpGatewayType: eaigw"), "gateway config should render mcpGatewayType, got:\n%s", data)
		})

		It("omits the oauth2 block when fleet oauth2 is disabled", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			}
			cm, err := GatewayConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).NotTo(ContainSubstring("oauth2:"), "gateway config should omit fleet oauth2 block when disabled, got:\n%s", data)
		})

		It("renders the fleet oauth2 block when enabled", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds", Scopes: []string{"openid", "groups"},
				},
			}
			cm, err := GatewayConfigMap(kn, knV2)
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
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			cm, err := GatewayConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("tlsCAFile: "+InterServiceTLSCAFile), "gateway oauth2 block should default tlsCAFile to the inter-service CA path, got:\n%s", data)
		})

		It("renders gateway.fleetOAuth2CredentialsSecretRef instead of the shared credentialsSecretRef when set", func() {
			kn := testKubernaut()
			enabled := true
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
			cm, err := GatewayConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: gateway-oauth2-creds"), "gateway config should use its own oauth2 client override, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "gateway config should not fall back to the shared credentialsSecretRef when it has its own override, got:\n%s", data)
		})

		It("omits telemetry entirely by default (#323)", func() {
			kn := testKubernaut()
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("telemetry:"), "gateway config should omit telemetry when spec.gateway.config.telemetry.endpoint is unset (zero overhead when off)")
		})

		It("[SC-8, SC-12] renders telemetry endpoint/logSink/tls from spec.gateway.config.telemetry (#323)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.Gateway.Config.Telemetry = telemetrySpecFixture()
			cm, err := GatewayConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			assertTelemetryYAML(cm.Data["config.yaml"])
		})
	})

	// #259 [CM-6]: server.maxConcurrentRequests/readTimeout/writeTimeout/
	// idleTimeout and retry.maxAttempts/initialBackoff/maxBackoff were
	// hardcoded; these tests prove spec.gateway.config.{server,retry} make
	// them administrator-tunable while preserving current hardcoded
	// defaults (readTimeout/writeTimeout intentionally stay at 3600s, not
	// upstream's chart default of 30s, to avoid a behavior change for
	// existing CRs).
	Describe("Gateway Server/Retry Config", func() {
		It("renders default server tuning fields when spec.gateway.config.server is unset (#259) [CM-6]", func() {
			kn := testKubernaut()
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			var root gatewayConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Server.MaxConcurrentRequests).To(Equal(100))
			Expect(root.Server.ReadTimeout).To(Equal("3600s"), "server.readTimeout must default to the current hardcoded value, not upstream's chart default, to avoid a behavior change")
			Expect(root.Server.WriteTimeout).To(Equal("3600s"), "server.writeTimeout must default to the current hardcoded value, not upstream's chart default, to avoid a behavior change")
			Expect(root.Server.IdleTimeout).To(Equal("120s"))
		})

		It("renders custom server tuning fields from spec.gateway.config.server (#259) [CM-6]", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			maxConcurrent := 250
			knV2.Spec.Gateway.Config.Server = &kubernautv1alpha2.GatewayServerSpec{
				MaxConcurrentRequests: &maxConcurrent,
				ReadTimeout:           "60s",
				WriteTimeout:          "60s",
				IdleTimeout:           "90s",
			}
			cm, err := GatewayConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())

			var root gatewayConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Server.MaxConcurrentRequests).To(Equal(250))
			Expect(root.Server.ReadTimeout).To(Equal("60s"))
			Expect(root.Server.WriteTimeout).To(Equal("60s"))
			Expect(root.Server.IdleTimeout).To(Equal("90s"))
		})

		It("renders default retry tuning fields when spec.gateway.config.retry is unset (#259) [CM-6]", func() {
			kn := testKubernaut()
			cm, err := GatewayConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			var root gatewayConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Processing.Retry.MaxAttempts).To(Equal(3))
			Expect(root.Processing.Retry.InitialBackoff).To(Equal("100ms"))
			Expect(root.Processing.Retry.MaxBackoff).To(Equal("5s"))
		})

		It("renders custom retry tuning fields from spec.gateway.config.retry (#259) [CM-6]", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			maxAttempts := 5
			knV2.Spec.Gateway.Config.Retry = &kubernautv1alpha2.GatewayRetrySpec{
				MaxAttempts:    &maxAttempts,
				InitialBackoff: "200ms",
				MaxBackoff:     "10s",
			}
			cm, err := GatewayConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())

			var root gatewayConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Processing.Retry.MaxAttempts).To(Equal(5))
			Expect(root.Processing.Retry.InitialBackoff).To(Equal("200ms"))
			Expect(root.Processing.Retry.MaxBackoff).To(Equal("10s"))
		})
	})

	Describe("DataStorage ConfigMap", func() {
		It("contains PostgreSQL and Valkey settings", func() {
			kn := testKubernaut()
			cm, err := DataStorageConfigMap(kn, testKnV2(kn), "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("host: pg.example.com"), "datastorage config should contain PG host, got:\n%s", data)
			Expect(data).To(ContainSubstring("addr: valkey.example.com:6379"), "datastorage config should contain Valkey addr, got:\n%s", data)
			Expect(data).To(ContainSubstring("secretsFile: /etc/datastorage/secrets/db-secrets.yaml"), "datastorage config should reference db secrets file, got:\n%s", data)
		})

		It("defaults PostgreSQL port to 5432 when unset", func() {
			kn := testKubernaut()
			kn.Spec.PostgreSQL.Port = 0
			cm, err := DataStorageConfigMap(kn, testKnV2(kn), "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("port: 5432"), "datastorage config should default to port 5432, got:\n%s", data)
		})

		// #423 coverage backfill: dataStorage.endpointPropagationDelay had
		// zero test references anywhere in the codebase.
		It("[CM-6] propagates a non-default spec.dataStorage.endpointPropagationDelay, defaulting to 10s when unset", func() {
			kn := testKubernaut()
			cmDefault, err := DataStorageConfigMap(kn, testKnV2(kn), "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())
			Expect(cmDefault.Data["config.yaml"]).To(ContainSubstring("endpointPropagationDelay: 10s"), "unset endpointPropagationDelay should default to 10s")

			kn.Spec.DataStorage.EndpointPropagationDelay = "25s"
			cm, err := DataStorageConfigMap(kn, testKnV2(kn), "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("endpointPropagationDelay: 25s"), "spec.dataStorage.endpointPropagationDelay should propagate verbatim, got:\n%s", data)
		})

		It("#399 [SC-8]: renders the configured PostgreSQL/Valkey hostnames verbatim, never pre-resolved to an IP", func() {
			// "localhost" reliably resolves via /etc/hosts in every
			// environment (CI sandboxes included), making it a deterministic
			// probe for the IP-substitution regression fixed by #399: prior
			// to the fix, resolveHostToIP("localhost") returned "127.0.0.1",
			// which breaks TLS hostname verification against a serving cert
			// that only has DNS SANs (see #398's on-cluster discovery).
			kn := testKubernaut()
			kn.Spec.PostgreSQL.Host = "localhost"
			kn.Spec.Valkey.Host = "localhost"
			cm, err := DataStorageConfigMap(kn, testKnV2(kn), "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("host: localhost"), "PostgreSQL host must remain the literal hostname, not be pre-resolved to an IP, got:\n%s", data)
			Expect(data).To(ContainSubstring("addr: localhost:6379"), "Valkey addr must remain the literal hostname, not be pre-resolved to an IP, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("127.0.0.1"), "DataStorage config must never contain a pre-resolved IP in place of the configured hostname, got:\n%s", data)
		})

		It("omits telemetry entirely by default (#323)", func() {
			kn := testKubernaut()
			cm, err := DataStorageConfigMap(kn, testKnV2(kn), "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("telemetry:"), "datastorage config should omit telemetry when spec.dataStorage.telemetry.endpoint is unset (zero overhead when off)")
		})

		It("[SC-8, SC-12] renders telemetry endpoint/logSink/tls from spec.dataStorage.telemetry (#323)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.DataStorage.Telemetry = telemetrySpecFixture()
			cm, err := DataStorageConfigMap(kn, knV2, "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())
			assertTelemetryYAML(cm.Data["config.yaml"])
		})

		It("passes through PostgreSQL SSL mode", func() {
			kn := testKubernaut()
			kn.Spec.PostgreSQL.SSLMode = "require"
			cm, err := DataStorageConfigMap(kn, testKnV2(kn), "kubernautdb", "kubernautuser")
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

	// #260 [CM-6]: database.maxOpenConns/maxIdleConns/connMaxLifetime/
	// connMaxIdleTime and server.readTimeout/writeTimeout were hardcoded;
	// these tests prove spec.dataStorage.{database,server} make them
	// administrator-tunable while preserving current hardcoded defaults.
	Describe("DataStorage Database/Server Config", func() {
		It("renders default database connection-pool fields when spec.dataStorage.database is unset (#260) [CM-6]", func() {
			kn := testKubernaut()
			cm, err := DataStorageConfigMap(kn, testKnV2(kn), "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())

			var root dataStorageConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Database.MaxOpenConns).To(Equal(100))
			Expect(root.Database.MaxIdleConns).To(Equal(20))
			Expect(root.Database.ConnMaxLifetime).To(Equal("1h"))
			Expect(root.Database.ConnMaxIdleTime).To(Equal("10m"))
		})

		It("renders custom database connection-pool fields from spec.dataStorage.database (#260) [CM-6]", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			maxOpen := 200
			maxIdle := 50
			knV2.Spec.DataStorage.Database = &kubernautv1alpha2.DataStorageDatabaseSpec{
				MaxOpenConns:    &maxOpen,
				MaxIdleConns:    &maxIdle,
				ConnMaxLifetime: "30m",
				ConnMaxIdleTime: "5m",
			}
			cm, err := DataStorageConfigMap(kn, knV2, "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())

			var root dataStorageConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Database.MaxOpenConns).To(Equal(200))
			Expect(root.Database.MaxIdleConns).To(Equal(50))
			Expect(root.Database.ConnMaxLifetime).To(Equal("30m"))
			Expect(root.Database.ConnMaxIdleTime).To(Equal("5m"))
		})

		It("renders default server.readTimeout/writeTimeout when spec.dataStorage.server is unset (#260) [CM-6]", func() {
			kn := testKubernaut()
			cm, err := DataStorageConfigMap(kn, testKnV2(kn), "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())

			var root dataStorageConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Server.ReadTimeout).To(Equal("30s"))
			Expect(root.Server.WriteTimeout).To(Equal("30s"))
		})

		It("renders custom server.readTimeout/writeTimeout from spec.dataStorage.server (#260) [CM-6]", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.DataStorage.Server = &kubernautv1alpha2.DataStorageServerSpec{
				ReadTimeout:  "60s",
				WriteTimeout: "45s",
			}
			cm, err := DataStorageConfigMap(kn, knV2, "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())

			var root dataStorageConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Server.ReadTimeout).To(Equal("60s"))
			Expect(root.Server.WriteTimeout).To(Equal("45s"))
		})
	})

	Describe("AIAnalysis ConfigMap", func() {
		It("includes confidence threshold when set", func() {
			kn := testKubernaut()
			kn.Spec.AIAnalysis.ConfidenceThreshold = "0.85"
			cm, err := AIAnalysisConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["config.yaml"]
			Expect(strings.Contains(data, "confidenceThreshold") && strings.Contains(data, "0.85")).To(BeTrue(), "aianalysis config should contain confidence threshold, got:\n%s", data)
		})

		It("uses agent key and not legacy kubernautAgent key", func() {
			kn := testKubernaut()
			cm, err := AIAnalysisConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("agent:"), "aianalysis config should contain 'agent:' key, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("kubernautAgent:"), "aianalysis config should not contain old 'kubernautAgent:' key, got:\n%s", data)
		})

		It("omits threshold when empty", func() {
			kn := testKubernaut()
			cm, err := AIAnalysisConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["config.yaml"]
			Expect(data).NotTo(ContainSubstring("confidenceThreshold"), "aianalysis config should not contain threshold when empty, got:\n%s", data)
		})
	})

	Describe("SignalProcessing ConfigMap", func() {
		It("contains DataStorage URL and classifier section", func() {
			kn := testKubernaut()
			cm, err := SignalProcessingConfigMap(kn, testKnV2(kn))
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
			cm, err := SignalProcessingConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).NotTo(ContainSubstring("fleet:"), "signalprocessing config should omit fleet block when disabled, got:\n%s", data)
		})

		It("renders fleet.endpoint (not mcpGatewayEndpoint) from spec.fleet.mcpGatewayEndpoint", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			}
			cm, err := SignalProcessingConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("fleet:"), "signalprocessing config should contain fleet block when enabled, got:\n%s", data)
			Expect(data).To(ContainSubstring("endpoint: https://mcp-gateway.example.com/sse"), "signalprocessing fleet.endpoint should carry spec.fleet.mcpGatewayEndpoint's value (upstream field is named 'endpoint', not 'mcpGatewayEndpoint'), got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("mcpGatewayEndpoint:"), "signalprocessing config must not render the mcpGatewayEndpoint key -- upstream FleetConfig.Endpoint has no such key, got:\n%s", data)
			Expect(data).To(ContainSubstring("mcpGatewayType: eaigw"), "signalprocessing config should render mcpGatewayType, got:\n%s", data)
		})

		// DD-362: SignalProcessing always renders fleet.namespace from the
		// shared spec.fleet.mcpGatewayNamespace -- there is no
		// per-component override (FleetOverrideSpec.Namespace was removed).
		It("renders fleet.namespace from the shared spec.fleet.mcpGatewayNamespace", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				MCPGatewayNamespace: testSharedMCPGatewayNamespace,
			}
			cm, err := SignalProcessingConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("namespace: shared-ns"), "signalprocessing fleet.namespace should use the shared value, got:\n%s", data)
		})

		It("renders fleet.oauth2.tlsCAFile defaulting to the inter-service CA path when oauth2 is enabled", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			cm, err := SignalProcessingConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "signalprocessing fleet.oauth2 should render the shared credentialsSecretRef, got:\n%s", data)
			Expect(data).To(ContainSubstring("tlsCAFile: "+InterServiceTLSCAFile), "signalprocessing fleet.oauth2 should default tlsCAFile to the inter-service CA path, got:\n%s", data)
		})

		It("renders signalProcessing.fleetOAuth2CredentialsSecretRef instead of the shared credentialsSecretRef when set", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			knV2.Spec.SignalProcessing.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testSPFleetOAuth2SecretRef}
			cm, err := SignalProcessingConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: sp-oauth2-creds"), "signalprocessing config should use its own oauth2 client override, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "signalprocessing config should not fall back to the shared credentialsSecretRef when it has its own override, got:\n%s", data)
		})
	})

	Describe("RemediationOrchestrator ConfigMap", func() {
		It("includes default timeout and threshold strings", func() {
			kn := testKubernaut()
			cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
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
			cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
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
			cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).To(ContainSubstring("global: 2h"), "RO config should use custom global timeout, got:\n%s", data)
			Expect(data).To(ContainSubstring("processing: 10m"), "RO config should use custom processing timeout, got:\n%s", data)
		})

		// #423 coverage backfill: 13 fields across timeouts/routing/
		// effectivenessAssessment/asyncPropagation had zero test references
		// anywhere in the codebase per
		// docs/tests/421/CRD_FIELD_COVERAGE_AUDIT.md. All are simple
		// withDefault/intPtrDefault string-or-int passthroughs into the
		// same flat YAML blocks, so one table covers overrides for all 13
		// plus their documented defaults.
		DescribeTable("[CM-6] propagates spec.remediationOrchestrator.* overrides verbatim, defaulting when unset",
			func(setSpec func(ro *kubernautv1alpha1.RemediationOrchestratorSpec), wantSubstring, wantDefaultSubstring string) {
				kn := testKubernaut()
				setSpec(&kn.Spec.RemediationOrchestrator)
				cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				Expect(data).To(ContainSubstring(wantSubstring), "override not propagated, got:\n%s", data)

				knDefault := testKubernaut()
				cmDefault, err := RemediationOrchestratorConfigMap(knDefault, testKnV2(knDefault))
				Expect(err).NotTo(HaveOccurred())
				Expect(cmDefault.Data["remediationorchestrator.yaml"]).To(ContainSubstring(wantDefaultSubstring), "unset default mismatch")
			},
			Entry("timeouts.analyzing", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) { ro.Timeouts.Analyzing = "22m" },
				"analyzing: 22m", "analyzing: 10m"),
			Entry("timeouts.executing", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) { ro.Timeouts.Executing = "44m" },
				"executing: 44m", "executing: 30m"),
			Entry("timeouts.verifying", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) { ro.Timeouts.Verifying = "55m" },
				"verifying: 55m", "verifying: 30m"),
			Entry("effectivenessAssessment.stabilizationWindow", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) {
				ro.EffectivenessAssessment.StabilizationWindow = "7m"
			}, "stabilizationWindow: 7m", "stabilizationWindow: 5m"),
			Entry("routing.consecutiveFailureCooldown", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) {
				ro.Routing.ConsecutiveFailureCooldown = "2h"
			}, "consecutiveFailureCooldown: 2h", "consecutiveFailureCooldown: 1h"),
			Entry("routing.consecutiveFailureThreshold", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) {
				ro.Routing.ConsecutiveFailureThreshold = ptr.To(9)
			}, "consecutiveFailureThreshold: 9", "consecutiveFailureThreshold: 3"),
			Entry("routing.ineffectiveChainThreshold", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) {
				ro.Routing.IneffectiveChainThreshold = ptr.To(8)
			}, "ineffectiveChainThreshold: 8", "ineffectiveChainThreshold: 3"),
			Entry("routing.ineffectiveTimeWindow", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) {
				ro.Routing.IneffectiveTimeWindow = "9h"
			}, "ineffectiveTimeWindow: 9h", "ineffectiveTimeWindow: 4h"),
			Entry("routing.recentlyRemediatedCooldown", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) {
				ro.Routing.RecentlyRemediatedCooldown = "13m"
			}, "recentlyRemediatedCooldown: 13m", "recentlyRemediatedCooldown: 5m"),
			Entry("routing.recurrenceCountThreshold", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) {
				ro.Routing.RecurrenceCountThreshold = ptr.To(11)
			}, "recurrenceCountThreshold: 11", "recurrenceCountThreshold: 5"),
			Entry("asyncPropagation.gitOpsSyncDelay", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) {
				ro.AsyncPropagation.GitOpsSyncDelay = "6m"
			}, "gitOpsSyncDelay: 6m", "gitOpsSyncDelay: 3m"),
			Entry("asyncPropagation.operatorReconcileDelay", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) {
				ro.AsyncPropagation.OperatorReconcileDelay = "2m"
			}, "operatorReconcileDelay: 2m", "operatorReconcileDelay: 1m"),
			Entry("asyncPropagation.proactiveAlertDelay", func(ro *kubernautv1alpha1.RemediationOrchestratorSpec) {
				ro.AsyncPropagation.ProactiveAlertDelay = "8m"
			}, "proactiveAlertDelay: 8m", "proactiveAlertDelay: 5m"),
		)

		// #423 coverage backfill: remediationOrchestrator.resources is
		// covered by the shared "Component .resources passthrough" table in
		// deployments_test.go (CONS-005-adjacent); no separate ConfigMap
		// assertion needed since RO's resources aren't config-rendered.

		Context("BAC requirements", func() {
			It("BAC-2: default CR renders explicit dryRun false", func() {
				kn := testKubernaut()
				cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				Expect(data).To(ContainSubstring("dryRun: false"), "BAC-2: default CR must render explicit 'dryRun: false', got:\n%s", data)
			})

			It("BAC-3: default CR renders dryRunHoldPeriod 1h", func() {
				kn := testKubernaut()
				cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				Expect(data).To(ContainSubstring("dryRunHoldPeriod: 1h"), "BAC-3: default CR must render 'dryRunHoldPeriod: 1h', got:\n%s", data)
			})

			It("BAC-1: DryRun true renders dryRun true in ConfigMap", func() {
				kn := testKubernaut()
				kn.Spec.RemediationOrchestrator.DryRun = true
				cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				Expect(data).To(ContainSubstring("dryRun: true"), "BAC-1: setting DryRun=true must render 'dryRun: true', got:\n%s", data)
			})

			It("BAC-4: custom hold period is rendered", func() {
				kn := testKubernaut()
				kn.Spec.RemediationOrchestrator.DryRun = true
				kn.Spec.RemediationOrchestrator.DryRunHoldPeriod = "30m"
				cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["remediationorchestrator.yaml"]
				Expect(data).To(ContainSubstring("dryRunHoldPeriod: 30m"), "BAC-4: custom hold period must be rendered, got:\n%s", data)
			})

			It("BAC-6: dry-run changes do not alter unrelated settings", func() {
				kn := testKubernaut()
				kn.Spec.RemediationOrchestrator.DryRun = true
				kn.Spec.RemediationOrchestrator.DryRunHoldPeriod = "2h"
				cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
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
				cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
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

			cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
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
			cm, err := RemediationOrchestratorConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).NotTo(ContainSubstring("fleet:"), "RO config should omit fleet block when disabled, got:\n%s", data)
		})

		It("renders the fleet block with backend and endpoint when enabled", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			}
			cm, err := RemediationOrchestratorConfigMap(kn, knV2)
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
			Expect(data).To(ContainSubstring("tlsCAFile: "+InterServiceTLSCAFile),
				"RO config should default tlsCAFile to the inter-service trust-bundle path when no explicit CA secret is set, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("tokenPath"), "RO config should omit tokenPath when no token secret set, got:\n%s", data)
		})

		It("renders tlsCAFile and tokenPath when the corresponding secrets are set", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "acm", Endpoint: "https://acm-search.example.com/graphql",
				CASecretName: "fmc-ca-bundle", TokenSecretName: "acm-search-token",
			}
			cm, err := RemediationOrchestratorConfigMap(kn, knV2)
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
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			}
			cm, err := RemediationOrchestratorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).To(ContainSubstring("mcpGatewayEndpoint: https://mcp-gateway.example.com/sse"), "RO config should render mcpGatewayEndpoint, got:\n%s", data)
			Expect(data).To(ContainSubstring("mcpGatewayType: eaigw"), "RO config should render mcpGatewayType, got:\n%s", data)
		})

		It("renders the fleet oauth2 block when enabled", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds", Scopes: []string{"openid", "groups"},
				},
			}
			cm, err := RemediationOrchestratorConfigMap(kn, knV2)
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
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			cm, err := RemediationOrchestratorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).To(ContainSubstring("tlsCAFile: "+InterServiceTLSCAFile), "RO oauth2 block should default tlsCAFile to the inter-service CA path, got:\n%s", data)
		})

		It("renders remediationOrchestrator.fleetOAuth2CredentialsSecretRef instead of the shared credentialsSecretRef when set", func() {
			kn := testKubernaut()
			enabled := true
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
			cm, err := RemediationOrchestratorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["remediationorchestrator.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: ro-oauth2-creds"), "RO config should use its own oauth2 client override, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "RO config should not fall back to the shared credentialsSecretRef when it has its own override, got:\n%s", data)
		})
	})

	Describe("WorkflowExecution ConfigMap", func() {
		It("uses default workflow namespace", func() {
			kn := testKubernaut()
			cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["workflowexecution.yaml"]
			Expect(data).To(ContainSubstring("kubernaut-workflows"), "WE config should use default workflow namespace, got:\n%s", data)
		})

		It("uses custom workflow namespace from the CR", func() {
			kn := testKubernaut()
			kn.Spec.WorkflowExecution.WorkflowNamespace = "custom-wf"
			cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["workflowexecution.yaml"]
			Expect(data).To(ContainSubstring("custom-wf"), "WE config should use custom workflow namespace, got:\n%s", data)
		})

		// #423 coverage backfill: workflowExecution.cooldownPeriod had zero
		// test references anywhere in the codebase.
		It("[CM-6] propagates a non-default spec.workflowExecution.cooldownPeriod, defaulting to 1m when unset", func() {
			kn := testKubernaut()
			cmDefault, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			Expect(cmDefault.Data["workflowexecution.yaml"]).To(ContainSubstring("cooldownPeriod: 1m"), "unset cooldownPeriod should default to 1m")

			kn.Spec.WorkflowExecution.CooldownPeriod = "3m"
			cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]
			Expect(data).To(ContainSubstring("cooldownPeriod: 3m"), "spec.workflowExecution.cooldownPeriod should propagate verbatim, got:\n%s", data)
		})

		It("[AC-6, IA-5] wires Ansible when enabled", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.Enabled = true
			kn.Spec.Ansible.APIURL = "https://awx.example.com"
			kn.Spec.Ansible.OrganizationID = 42
			kn.Spec.Ansible.TokenSecretRef = &kubernautv1alpha1.SecretKeyRef{
				Name: "awx-token",
				Key:  "api-token",
			}
			cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
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

		// #423 coverage backfill: ansible.tokenSecretRef.key had no default
		// test reference (the test above always sets an explicit key).
		It("[AC-6, IA-5] defaults ansible.tokenSecretRef.key to \"token\" when unset", func() {
			kn := testKubernaut()
			kn.Spec.Ansible.Enabled = true
			kn.Spec.Ansible.APIURL = "https://awx.example.com"
			kn.Spec.Ansible.TokenSecretRef = &kubernautv1alpha1.SecretKeyRef{Name: "awx-token"}
			cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]
			Expect(data).To(ContainSubstring("key: token"), "unset ansible.tokenSecretRef.key should default to \"token\", got:\n%s", data)
		})

		It("omits Ansible when disabled", func() {
			kn := testKubernaut()
			cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]

			Expect(data).NotTo(ContainSubstring("ansible:"), "WE config should not contain ansible section when disabled, got:\n%s", data)
		})

		It("uses nested execution, datastorage, and controller structure", func() {
			kn := testKubernaut()
			cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
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
			cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]
			Expect(strings.Contains(data, "logging:") && strings.Contains(data, "level: error")).To(BeTrue(), "WE config should render logging.level, got:\n%s", data)
		})

		It("renders Tekton enabled when set", func() {
			kn := testKubernaut()
			on := true
			kn.Spec.WorkflowExecution.Tekton.Enabled = &on
			cm, err := WorkflowExecutionConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]
			Expect(strings.Contains(data, "tekton:") && strings.Contains(data, "enabled: true")).To(BeTrue(), "WE config should render tekton.enabled, got:\n%s", data)
		})

		// #235/DD-235: WE's fleet block is the only one whose OAuth2
		// credential must never fall back to the shared
		// spec.fleet.oauth2.credentialsSecretRef (least-privilege --
		// WE is the only fleet-aware component that calls MCP write
		// tools).
		It("omits the fleet block when spec.fleet.enabled is false", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
			cm, err := WorkflowExecutionConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]
			Expect(data).NotTo(ContainSubstring("fleet:"), "WE config should omit fleet block entirely when fleet is disabled, got:\n%s", data)
		})

		It("renders fleet.endpoint and fleet.oauth2 (no gatewayType) when fleet is enabled", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
			cm, err := WorkflowExecutionConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]
			for _, want := range []string{
				"fleet:",
				"endpoint: https://mcp-gateway.example.com/sse",
				"oauth2:",
				"tokenURL: https://keycloak.example.com/token",
				"credentialsSecretRef: we-write-oauth2-creds",
			} {
				Expect(data).To(ContainSubstring(want), "WE config should contain %q when fleet enabled, got:\n%s", want, data)
			}
			Expect(data).NotTo(ContainSubstring("mcpGatewayType:"), "WE config has no static gatewayType knob (upstream pkg/workflowexecution/config.FleetConfig has none), got:\n%s", data)
		})

		It("[AC-6] never inherits the shared read-only fleet credential even when both WE's own and the shared oauth2CredentialsSecretRef are set", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
			cm, err := WorkflowExecutionConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["workflowexecution.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: we-write-oauth2-creds"),
				"WE config must render its own write-scoped credential, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"),
				"[AC-6] WE must never render the shared read-only fleet-oauth2-creds -- its write-scoped credential has no fallback path, got:\n%s", data)
		})
	})

	Describe("EffectivenessMonitor ConfigMap", func() {
		It("includes default stabilization window", func() {
			kn := testKubernaut()
			cm, err := EffectivenessMonitorConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())

			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("stabilizationWindow: 30s"), "EM config should have default stabilization window, got:\n%s", data)
		})

		// #423 coverage backfill: assessment.stabilizationWindow/
		// validityWindow non-default overrides had zero test references
		// (only the stabilizationWindow default was covered above).
		It("[CM-6] propagates non-default spec.effectivenessMonitor.assessment overrides", func() {
			kn := testKubernaut()
			kn.Spec.EffectivenessMonitor.Assessment.StabilizationWindow = "45s"
			kn.Spec.EffectivenessMonitor.Assessment.ValidityWindow = "600s"
			cm, err := EffectivenessMonitorConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("stabilizationWindow: 45s"), "spec.effectivenessMonitor.assessment.stabilizationWindow should propagate verbatim, got:\n%s", data)
			Expect(data).To(ContainSubstring("validityWindow: 600s"), "spec.effectivenessMonitor.assessment.validityWindow should propagate verbatim, got:\n%s", data)
		})

		It("[CM-6] defaults assessment.validityWindow to 300s when unset", func() {
			kn := testKubernaut()
			cm, err := EffectivenessMonitorConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("validityWindow: 300s"), "unset assessment.validityWindow should default to 300s, got:\n%s", data)
		})

		It("includes monitoring URLs when monitoring is enabled", func() {
			kn := testKubernaut()
			cm, err := EffectivenessMonitorConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]

			Expect(data).To(ContainSubstring(OCPPrometheusURL), "EM config should contain Prometheus URL when monitoring enabled, got:\n%s", data)
			Expect(data).To(ContainSubstring(OCPAlertManagerURL), "EM config should contain AlertManager URL when monitoring enabled, got:\n%s", data)
			Expect(data).To(ContainSubstring("external:"), "EM config should contain external section when monitoring enabled, got:\n%s", data)
			Expect(data).To(ContainSubstring("tlsCaFile: /etc/ssl/em/service-ca.crt"), "EM config should contain external.tlsCaFile when monitoring enabled, got:\n%s", data)
		})

		// #298: spec.monitoring.prometheus.url/spec.monitoring.alertManager.url
		// were CRD fields with zero non-test references anywhere in the
		// codebase -- setting them had no effect on the rendered config. This
		// asserts the override actually takes effect instead of the hardcoded
		// OCP default.
		It("overrides Prometheus and AlertManager URLs from spec.monitoring (#298)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.Monitoring.Prometheus.URL = testCustomPrometheusURL
			knV2.Spec.Monitoring.AlertManager.URL = "https://custom-alertmanager.custom-monitoring.svc:9094"
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]

			Expect(data).To(ContainSubstring(testCustomPrometheusURL), "EM config should use spec.monitoring.prometheus.url override, got:\n%s", data)
			Expect(data).To(ContainSubstring("https://custom-alertmanager.custom-monitoring.svc:9094"), "EM config should use spec.monitoring.alertManager.url override, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring(OCPPrometheusURL), "EM config should not fall back to the OCP default once overridden, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring(OCPAlertManagerURL), "EM config should not fall back to the OCP default once overridden, got:\n%s", data)
		})

		// #424: spec.monitoring.prometheus.tlsCaFile/alertManager.tlsCaFile
		// were CRD fields with zero non-test references anywhere in the
		// codebase -- external.tlsCaFile was always the hardcoded
		// "/etc/ssl/em/service-ca.crt" literal regardless of either field.
		// EM's upstream Config.External.TLSCaFile
		// (kubernaut/internal/config/effectivenessmonitor/config.go) is a
		// single CA bundle shared by both its Prometheus and AlertManager
		// HTTP clients -- there is no per-destination key to wire into --
		// so Prometheus.TLSCaFile takes precedence over
		// AlertManager.TLSCaFile when both are set (EM's primary
		// assessment-scoring consumer).
		It("overrides external.tlsCaFile from spec.monitoring.prometheus.tlsCaFile (#424)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.Monitoring.Prometheus.TLSCaFile = testCustomPrometheusTLSCaFile
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("tlsCaFile: "+testCustomPrometheusTLSCaFile), "EM config should use spec.monitoring.prometheus.tlsCaFile override, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("/etc/ssl/em/service-ca.crt"), "EM config should not fall back to the hardcoded default once overridden, got:\n%s", data)
		})

		It("falls back to spec.monitoring.alertManager.tlsCaFile when prometheus.tlsCaFile is unset (#424)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.Monitoring.AlertManager.TLSCaFile = testCustomAlertManagerTLSCaFile
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("tlsCaFile: "+testCustomAlertManagerTLSCaFile), "EM config should fall back to spec.monitoring.alertManager.tlsCaFile when prometheus.tlsCaFile is unset, got:\n%s", data)
		})

		It("prometheus.tlsCaFile takes precedence over alertManager.tlsCaFile when both are set (#424)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.Monitoring.Prometheus.TLSCaFile = testCustomPrometheusTLSCaFile
			knV2.Spec.Monitoring.AlertManager.TLSCaFile = testCustomAlertManagerTLSCaFile
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("tlsCaFile: "+testCustomPrometheusTLSCaFile), "EM's single external.tlsCaFile key should prefer prometheus.tlsCaFile when both overrides are set, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring(testCustomAlertManagerTLSCaFile), "EM's single external.tlsCaFile key cannot hold both values -- alertManager.tlsCaFile must not appear, got:\n%s", data)
		})

		It("unset preserves today's hardcoded external.tlsCaFile default (regression guard, #424)", func() {
			kn := testKubernaut()
			cm, err := EffectivenessMonitorConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("tlsCaFile: /etc/ssl/em/service-ca.crt"), "EM config should preserve the hardcoded default when neither override is set, got:\n%s", data)
		})

		// #298: emExternalYAML.PrometheusEnabled/AlertManagerEnabled were
		// hardcoded `true` regardless of spec.monitoring.prometheus.enabled/
		// spec.monitoring.alertManager.enabled -- the CRD's opt-out had no
		// effect.
		It("reflects spec.monitoring.prometheus.enabled=false and alertManager.enabled=false (#298)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			disabled := false
			knV2.Spec.Monitoring.Prometheus.Enabled = &disabled
			knV2.Spec.Monitoring.AlertManager.Enabled = &disabled
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]

			Expect(data).To(ContainSubstring("prometheusEnabled: false"), "EM config should reflect spec.monitoring.prometheus.enabled=false, got:\n%s", data)
			Expect(data).To(ContainSubstring("alertManagerEnabled: false"), "EM config should reflect spec.monitoring.alertManager.enabled=false, got:\n%s", data)
		})

		It("renders v1.4 logging and datastorage buffer settings", func() {
			kn := testKubernaut()
			kn.Spec.EffectivenessMonitor.Logging.Level = "debug"
			cm, err := EffectivenessMonitorConfigMap(kn, testKnV2(kn))
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
			cm, err := EffectivenessMonitorConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).NotTo(ContainSubstring("fleet:"), "EM config should omit fleet block when disabled, got:\n%s", data)
		})

		It("renders mcpGatewayEndpoint/mcpGatewayType but omits backend/endpoint/tokenPath even when spec.fleet.backend/endpoint are set", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				CASecretName: "fmc-ca-bundle", TokenSecretName: "acm-search-token",
			}
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
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
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("tlsCAFile: "+InterServiceTLSCAFile), "EM fleet.oauth2 should default tlsCAFile to the inter-service CA path, got:\n%s", data)
		})

		It("renders effectivenessMonitor.fleetOAuth2CredentialsSecretRef instead of the shared credentialsSecretRef when set", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
				MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				OAuth2: kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
				},
			}
			knV2.Spec.EffectivenessMonitor.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testEMFleetOAuth2SecretRef}
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(data).To(ContainSubstring("credentialsSecretRef: em-oauth2-creds"), "EM config should use its own oauth2 client override, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "EM config should not fall back to the shared credentialsSecretRef when it has its own override, got:\n%s", data)
		})

		// DD-362: EM always renders fleet.namespace from the shared
		// spec.fleet.mcpGatewayNamespace -- there is no per-component
		// override (FleetOverrideSpec.Namespace was removed).
		It("omits fleet.namespace when spec.fleet.mcpGatewayNamespace is unset (#227)", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			}
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(fleetNamespaceFromYAML(data)).To(BeEmpty(), "EM fleet block should omit namespace when the shared namespace is unset, got:\n%s", data)
		})

		It("renders fleet.namespace from the shared spec.fleet.mcpGatewayNamespace (#227)", func() {
			kn := testKubernaut()
			enabled := true
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &enabled, MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				MCPGatewayNamespace: "kubernaut-fleet",
			}
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["effectivenessmonitor.yaml"]
			Expect(fleetNamespaceFromYAML(data)).To(Equal("kubernaut-fleet"), "EM fleet block should use the shared spec.fleet.mcpGatewayNamespace, got:\n%s", data)
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
			cm, err := NotificationControllerConfigMap(kn, testKnV2(kn))
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
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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

		// #298: kaToolsConfig() hardcoded OCPPrometheusURL/OCPAlertManagerURL,
		// ignoring spec.monitoring entirely.
		It("overrides Prometheus and AlertManager URLs from spec.monitoring (#298)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.Monitoring.Prometheus.URL = testCustomPrometheusURL
			knV2.Spec.Monitoring.AlertManager.URL = "https://custom-alertmanager.custom-monitoring.svc:9094"
			cm, err := KubernautAgentConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]

			Expect(data).To(ContainSubstring(testCustomPrometheusURL), "KA config should use spec.monitoring.prometheus.url override, got:\n%s", data)
			Expect(data).To(ContainSubstring("https://custom-alertmanager.custom-monitoring.svc:9094"), "KA config should use spec.monitoring.alertManager.url override, got:\n%s", data)
		})

		It("[AU-3, AU-4] matches expected v1.4 structure and defaults", func() {
			kn := testKubernaut()
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			// Unmarshal into the actual production kubernautAgentConfigYAML
			// type (this file is package resources, white-box) instead of a
			// hand-copied mirror struct -- see AGENTS.md Testing Conventions.
			var root kubernautAgentConfigYAML
			err = yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)
			Expect(err).NotTo(HaveOccurred())
			Expect(root.Runtime.Logging.Level).To(Equal("info"), "runtime.logging.level = %q, want info", root.Runtime.Logging.Level)
			Expect(root.Runtime.Server.Port == 8443 && root.Runtime.Server.Address == "0.0.0.0").To(BeTrue(), "runtime.server = %#v, want address 0.0.0.0 port 8443", root.Runtime.Server)
			Expect(root.Runtime.Audit).NotTo(BeNil(), "runtime.audit should be present when audit is enabled by default")
			Expect(root.Runtime.Audit.FlushIntervalSeconds).To(Equal(1.0), "runtime.audit.flushIntervalSeconds = %v, want 1.0", root.Runtime.Audit.FlushIntervalSeconds)
			Expect(root.Runtime.Audit.BufferSize).To(Equal(10000), "runtime.audit.bufferSize = %d, want 10000", root.Runtime.Audit.BufferSize)
			Expect(root.Runtime.Audit.BatchSize).To(Equal(50), "runtime.audit.batchSize = %d, want 50", root.Runtime.Audit.BatchSize)
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

		// #424: spec.monitoring.prometheus.tlsCaFile/alertManager.tlsCaFile
		// were CRD fields with zero non-test references anywhere in the
		// codebase -- integrations.tools.{prometheus,alertmanager}.tlsCaFile
		// were always the hardcoded "/etc/ssl/ka/service-ca.crt" literal
		// regardless of either field. Unlike EM, KA's upstream
		// PrometheusToolConfig/AlertmanagerToolConfig
		// (kubernaut/internal/kubernautagent/config/config_types.go) are
		// genuinely separate structs/keys, so each override wires
		// independently with no precedence collision.
		It("overrides tools.prometheus.tlsCaFile from spec.monitoring.prometheus.tlsCaFile (#424)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.Monitoring.Prometheus.TLSCaFile = testCustomPrometheusTLSCaFile
			cm, err := KubernautAgentConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			var root kubernautAgentConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Integrations.Tools.Prometheus.TLSCaFile).To(Equal(testCustomPrometheusTLSCaFile), "tools.prometheus.tlsCaFile should honor spec.monitoring.prometheus.tlsCaFile override, got %q", root.Integrations.Tools.Prometheus.TLSCaFile)
		})

		It("overrides tools.alertmanager.tlsCaFile from spec.monitoring.alertManager.tlsCaFile (#424)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.Monitoring.AlertManager.TLSCaFile = testCustomAlertManagerTLSCaFile
			cm, err := KubernautAgentConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			var root kubernautAgentConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Integrations.Tools.Alertmanager.TLSCaFile).To(Equal(testCustomAlertManagerTLSCaFile), "tools.alertmanager.tlsCaFile should honor spec.monitoring.alertManager.tlsCaFile override, got %q", root.Integrations.Tools.Alertmanager.TLSCaFile)
			Expect(root.Integrations.Tools.Prometheus.TLSCaFile).To(Equal("/etc/ssl/ka/service-ca.crt"), "prometheus.tlsCaFile should keep its own default, unaffected by the alertmanager override -- KA's two keys are independent, got %q", root.Integrations.Tools.Prometheus.TLSCaFile)
		})

		It("F10 regression (#417): resolves ai.llm.provider via single-profile inference when kubernautAgent.llmProfileRef is omitted", func() {
			kn := testKubernaut() // testKubernaut() defines exactly one profile ("primary")
			kn.Spec.KubernautAgent.LLMProfileRef = ""
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			var root kubernautAgentConfigYAML
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.AI.LLM.Provider).To(Equal(LLMProviderOpenAI),
				"config.yaml must resolve the sole spec.llmProfiles entry via EffectiveKALLMProfileRef (ADR-CRD-001 F10) when llmProfileRef is omitted, not render an empty provider, got %q", root.AI.LLM.Provider)
		})

		It("renders logging format as JSON", func() {
			kn := testKubernaut()
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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

		It("[AU-3, AU-4] renders custom runtime.audit tuning fields from spec.kubernautAgent.audit (#257)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			buffer := 25000
			batch := 200
			knV2.Spec.KubernautAgent.Audit.FlushIntervalSeconds = "0.5"
			knV2.Spec.KubernautAgent.Audit.BufferSize = &buffer
			knV2.Spec.KubernautAgent.Audit.BatchSize = &batch
			cm, err := KubernautAgentConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			// Unmarshal into the production kaAuditYAML type directly (this
			// file is package resources, white-box) instead of a hand-copied
			// mirror struct -- see AGENTS.md Testing Conventions.
			var root struct {
				Runtime struct {
					Audit kaAuditYAML `yaml:"audit"`
				} `yaml:"runtime"`
			}
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Runtime.Audit.FlushIntervalSeconds).To(Equal(0.5), "runtime.audit.flushIntervalSeconds should honor spec.kubernautAgent.audit.flushIntervalSeconds override")
			Expect(root.Runtime.Audit.BufferSize).To(Equal(25000), "runtime.audit.bufferSize should honor spec.kubernautAgent.audit.bufferSize override")
			Expect(root.Runtime.Audit.BatchSize).To(Equal(200), "runtime.audit.batchSize should honor spec.kubernautAgent.audit.batchSize override")
		})

		It("[AU-3] omits runtime.audit entirely when audit is disabled, even with tuning overrides set (#257)", func() {
			kn := testKubernaut()
			disabled := false
			kn.Spec.KubernautAgent.Audit.Enabled = &disabled
			knV2 := testKnV2(kn)
			buffer := 25000
			knV2.Spec.KubernautAgent.Audit.BufferSize = &buffer
			cm, err := KubernautAgentConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("bufferSize"), "runtime.audit block (and its tuning fields) should be omitted entirely when audit is disabled")
		})

		It("omits telemetry entirely by default (#323)", func() {
			kn := testKubernaut()
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("telemetry:"), "KA config should omit telemetry when spec.kubernautAgent.telemetry.endpoint is unset (zero overhead when off)")
		})

		It("[SC-8, SC-12] renders telemetry endpoint/logSink/tls from spec.kubernautAgent.telemetry (#323)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.KubernautAgent.Telemetry = telemetrySpecFixture()
			cm, err := KubernautAgentConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			assertTelemetryYAML(cm.Data["config.yaml"])
		})

		It("[CM-6] renders alignment check settings when enabled", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.AlignmentCheck.Enabled = true
			kn.Spec.KubernautAgent.AlignmentCheck.Timeout = "20s"
			kn.Spec.KubernautAgent.AlignmentCheck.MaxStepTokens = 1024
			kn.Spec.KubernautAgent.AlignmentCheck.LLM = &kubernautv1alpha1.AlignmentCheckLLMSpec{
				Provider: LLMProviderOpenAI,
				Model:    "gpt-4o-mini",
				Endpoint: "https://align.example/v1",
			}
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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

		// #423: kubernautAgent.alignmentCheck.llmProfileRef (v1alpha2) was
		// found entirely inert -- kaAlignmentConfig never read it, only the
		// legacy v1alpha1 ac.LLM literal fields. Wired with llmProfileRef
		// taking precedence over the legacy literal for backward compat.
		It("[CM-6] resolves alignmentCheck.llmProfileRef (v1alpha2) onto ai.alignmentCheck.llm, taking precedence over the legacy llm literal", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.AlignmentCheck.Enabled = true
			kn.Spec.KubernautAgent.AlignmentCheck.LLM = &kubernautv1alpha1.AlignmentCheckLLMSpec{
				Provider: LLMProviderOpenAI,
				Model:    "should-be-overridden",
			}
			kn.Spec.LLMProfiles = map[string]kubernautv1alpha1.LLMProfileSpec{
				"align-profile": {Provider: LLMProviderAnthropic, Model: "claude-3-5-sonnet", Endpoint: "https://align-profile.example/v1"},
			}
			knV2 := testKnV2(kn)
			knV2.Spec.KubernautAgent.AlignmentCheck.LLMProfileRef = "align-profile"
			cm, err := KubernautAgentConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("provider: anthropic"), "llmProfileRef-resolved provider should take precedence over the legacy llm literal, got:\n%s", data)
			Expect(data).To(ContainSubstring("model: claude-3-5-sonnet"), "llmProfileRef-resolved model should take precedence over the legacy llm literal, got:\n%s", data)
			Expect(data).To(ContainSubstring("endpoint: https://align-profile.example/v1"), "llmProfileRef-resolved endpoint should be propagated, got:\n%s", data)
			Expect(data).NotTo(ContainSubstring("should-be-overridden"), "legacy llm literal must be ignored once llmProfileRef is set")
		})

		It("[CM-6] falls back to the legacy alignmentCheck.llm literal when llmProfileRef is unset", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.AlignmentCheck.Enabled = true
			kn.Spec.KubernautAgent.AlignmentCheck.LLM = &kubernautv1alpha1.AlignmentCheckLLMSpec{
				Provider: LLMProviderOpenAI,
				Model:    "legacy-model",
			}
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("model: legacy-model"), "legacy llm literal should still be honored when llmProfileRef is unset (backward compat), got:\n%s", data)
		})

		It("[SC-8, SC-12] propagates custom LLM TLS CA file", func() {
			kn := testKubernaut()
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.TLSCaFile = "/etc/custom-ca/llm.pem" })
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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

		// #423 coverage backfill: llmProfiles.tlsCertFile/tlsKeyFile had no
		// tagged test reference (mirrors the tlsCaFile test just above).
		It("[SC-8, SC-12] propagates custom LLM TLS client cert and key files", func() {
			kn := testKubernaut()
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
				p.TLSCertFile = "/etc/custom-ca/llm-client.crt"
				p.TLSKeyFile = "/etc/custom-ca/llm-client.key"
			})
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("tlsCertFile: /etc/custom-ca/llm-client.crt"), "llmProfiles.tlsCertFile should propagate verbatim, got:\n%s", data)
			Expect(data).To(ContainSubstring("tlsKeyFile: /etc/custom-ca/llm-client.key"), "llmProfiles.tlsKeyFile should propagate verbatim, got:\n%s", data)
		})

		// #423 coverage backfill: llmProfiles.azureApiVersion/bedrockRegion/
		// timeoutSeconds had zero test references anywhere in the codebase.
		It("[CM-6] propagates llmProfiles.azureApiVersion and bedrockRegion overrides", func() {
			kn := testKubernaut()
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
				p.AzureAPIVersion = "2024-06-01"
				p.BedrockRegion = "us-west-2"
			})
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("azureApiVersion: \"2024-06-01\""), "llmProfiles.azureApiVersion should propagate verbatim, got:\n%s", data)
			Expect(data).To(ContainSubstring("bedrockRegion: us-west-2"), "llmProfiles.bedrockRegion should propagate verbatim, got:\n%s", data)
		})

		It("[CM-6] propagates a non-default llmProfiles.timeoutSeconds onto the llm-runtime ConfigMap", func() {
			kn := testKubernaut()
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.TimeoutSeconds = ptr.To(45) })
			cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm).NotTo(BeNil())
			data := cm.Data["llm-runtime.yaml"]
			Expect(data).To(ContainSubstring("timeoutSeconds: 45"), "llmProfiles.timeoutSeconds should propagate verbatim, got:\n%s", data)
		})

		It("[SC-5] renders non-default summarizer thresholds", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.Summarizer.Threshold = 5000
			kn.Spec.KubernautAgent.Summarizer.MaxToolOutputSize = 50000
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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

		// #423 coverage backfill: safety.anomaly.maxTotalToolCalls/
		// maxRepeatedFailures had zero test references anywhere in the
		// codebase.
		It("[SI-10] renders safety anomaly maxTotalToolCalls and maxRepeatedFailures overrides", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.Safety.Anomaly.MaxTotalToolCalls = ptr.To(77)
			kn.Spec.KubernautAgent.Safety.Anomaly.MaxRepeatedFailures = ptr.To(9)
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("maxTotalToolCalls: 77"), "spec.kubernautAgent.safety.anomaly.maxTotalToolCalls should propagate verbatim, got:\n%s", data)
			Expect(data).To(ContainSubstring("maxRepeatedFailures: 9"), "spec.kubernautAgent.safety.anomaly.maxRepeatedFailures should propagate verbatim, got:\n%s", data)
		})

		// #423 coverage backfill: safety.sanitization.credentialScrubEnabled/
		// injectionPatternsEnabled had zero test references. Both default to
		// true (secure-by-default: sanitize unless explicitly disabled).
		It("[SI-10, SC-8] defaults safety.sanitization guardrails to enabled", func() {
			kn := testKubernaut()
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("credentialScrubEnabled: true"), "credentialScrubEnabled should default to true (secure-by-default), got:\n%s", data)
			Expect(data).To(ContainSubstring("injectionPatternsEnabled: true"), "injectionPatternsEnabled should default to true (secure-by-default), got:\n%s", data)
		})

		It("[SI-10, SC-8] honors explicit safety.sanitization overrides to disable guardrails", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.Safety.Sanitization.CredentialScrubEnabled = ptr.To(false)
			kn.Spec.KubernautAgent.Safety.Sanitization.InjectionPatternsEnabled = ptr.To(false)
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("credentialScrubEnabled: false"), "explicit spec.kubernautAgent.safety.sanitization.credentialScrubEnabled=false must be honored, got:\n%s", data)
			Expect(data).To(ContainSubstring("injectionPatternsEnabled: false"), "explicit spec.kubernautAgent.safety.sanitization.injectionPatternsEnabled=false must be honored, got:\n%s", data)
		})

		// #423 coverage backfill: kubernautAgent.maxTurns had zero test
		// references for an explicit (non-default) override.
		It("[CM-6] propagates a non-default spec.kubernautAgent.maxTurns onto ai.investigation.maxTurns", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.MaxTurns = 15
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("maxTurns: 15"), "spec.kubernautAgent.maxTurns should propagate verbatim, got:\n%s", data)
		})

		// #423 coverage backfill: interactive.maxConcurrentSessions/
		// rateLimitPerUser had zero test references.
		It("[SC-5] propagates interactive.maxConcurrentSessions and rateLimitPerUser overrides", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.Interactive = &kubernautv1alpha1.InteractiveSpec{
				MaxConcurrentSessions: ptr.To(250),
				RateLimitPerUser:      ptr.To(42),
			}
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("maxConcurrentSessions: 250"), "spec.kubernautAgent.interactive.maxConcurrentSessions should propagate verbatim, got:\n%s", data)
			Expect(data).To(ContainSubstring("rateLimitPerUser: 42"), "spec.kubernautAgent.interactive.rateLimitPerUser should propagate verbatim, got:\n%s", data)
		})

		// #423 coverage backfill: session.ttl had zero test references.
		It("[SC-23] propagates spec.kubernautAgent.session.ttl onto runtime.session.ttl", func() {
			kn := testKubernaut()
			kn.Spec.KubernautAgent.Session.TTL = "45m"
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("ttl: 45m"), "spec.kubernautAgent.session.ttl should propagate verbatim, got:\n%s", data)
		})

		It("renders LLM OAuth2 block when enabled", func() {
			kn := testKubernaut()
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.Enabled = true })
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.TokenURL = "https://idp.example/oauth/token" })
			mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.OAuth2.Scopes = []string{"openid", "api.read"} })
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
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
					"maxRetries: 3",
					"timeoutSeconds: 120",
				} {
					Expect(data).To(ContainSubstring(want), "llm-runtime defaults should contain %q, got:\n%s", want, data)
				}
			})

			It("F10 regression (#417): resolves model/provider via single-profile inference when kubernautAgent.llmProfileRef is omitted", func() {
				kn := testKubernaut() // testKubernaut() defines exactly one profile ("primary")
				kn.Spec.KubernautAgent.LLMProfileRef = ""
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["llm-runtime.yaml"]
				Expect(data).To(ContainSubstring("model: gpt-4o"),
					"llm-runtime.yaml must resolve the sole spec.llmProfiles entry via EffectiveKALLMProfileRef (ADR-CRD-001 F10) when llmProfileRef is omitted, not render an empty model, got:\n%s", data)
				Expect(data).NotTo(ContainSubstring(`model: ""`),
					"llm-runtime.yaml rendered an empty model -- single-profile inference was not applied, got:\n%s", data)
			})

			It("LR-030: omits temperature from llm-runtime.yaml when not configured on the profile (regression: kubernaut v1.5.5 CHANGELOG -- an explicit temperature alongside top_p causes HTTP 400 on models like claude-opus-4 that reject the combination; unset must mean 'let the provider default', not 'send 0.7')", func() {
				kn := testKubernaut()
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["llm-runtime.yaml"]
				Expect(data).NotTo(ContainSubstring("temperature:"), "llm-runtime.yaml must omit temperature entirely when LLMProfileSpec.Temperature is unset, got:\n%s", data)
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

			It("LR-023 [IA-5]: emits apiKeyFile pointing at a dedicated mount when a phase's own profile has a different credentialsSecretName than KA's (#233)", func() {
				kn := testKubernaut()
				kn.Spec.LLMProfiles["workflow-cross-cred"] = kubernautv1alpha1.LLMProfileSpec{
					Provider:              "anthropic",
					Model:                 "claude-haiku-4-6",
					CredentialsSecretName: "different-secret",
				}
				kn.Spec.KubernautAgent.PhaseModels = map[string]string{"workflow_discovery": "workflow-cross-cred"}
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				var root struct {
					PhaseModels map[string]struct {
						APIKeyFile string `yaml:"apiKeyFile"`
					} `yaml:"phaseModels"`
				}
				Expect(yaml.Unmarshal([]byte(cm.Data["llm-runtime.yaml"]), &root)).To(Succeed())
				phase, ok := root.PhaseModels["workflow_discovery"]
				Expect(ok).To(BeTrue(), "expected phaseModels.workflow_discovery entry")
				Expect(phase.APIKeyFile).To(Equal("/etc/kubernaut-agent/phase-credentials/workflow_discovery/api_key"),
					"#233: a phase override with its own credentialsSecretName must render its own apiKeyFile pointing at its dedicated mount, not inherit KA's")
			})

			It("LR-024 [IA-5]: emits vertexProject/vertexLocation and a credentials.json apiKeyFile for a vertex_ai phase override with its own credentials (#233)", func() {
				kn := testKubernaut()
				kn.Spec.LLMProfiles["rca-vertex"] = kubernautv1alpha1.LLMProfileSpec{
					Provider:              LLMProviderVertexAI,
					Model:                 "gemini-2.5-flash",
					CredentialsSecretName: "vertex-phase-creds",
					VertexProject:         "example-gcp-project",
					VertexLocation:        "us-central1",
				}
				kn.Spec.KubernautAgent.PhaseModels = map[string]string{"rca": "rca-vertex"}
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				var root struct {
					PhaseModels map[string]struct {
						APIKeyFile     string `yaml:"apiKeyFile"`
						VertexProject  string `yaml:"vertexProject"`
						VertexLocation string `yaml:"vertexLocation"`
					} `yaml:"phaseModels"`
				}
				Expect(yaml.Unmarshal([]byte(cm.Data["llm-runtime.yaml"]), &root)).To(Succeed())
				phase, ok := root.PhaseModels["rca"]
				Expect(ok).To(BeTrue(), "expected phaseModels.rca entry")
				Expect(phase.APIKeyFile).To(Equal("/etc/kubernaut-agent/phase-credentials/rca/credentials.json"), // pre-commit:allow-sensitive -- mount-path convention constant, not a real credential/secret value
					"#233: a vertex_ai phase override's own credentials must resolve via its own credentials.json file, matching the base profile's ADC-file convention")
				Expect(phase.VertexProject).To(Equal("example-gcp-project"))
				Expect(phase.VertexLocation).To(Equal("us-central1"))
			})

			It("LR-025 [IA-5]: does not emit apiKeyFile when a phase shares KA's credentialsSecretName (regression guard, #233)", func() {
				kn := testKubernaut()
				kn.Spec.LLMProfiles["workflow-lite"] = kubernautv1alpha1.LLMProfileSpec{
					Provider:              "openai",
					Model:                 "gpt-4o-mini",
					Endpoint:              testOpenAIEndpoint,
					CredentialsSecretName: "llm-creds", // same as testKubernaut()'s "primary" profile
				}
				kn.Spec.KubernautAgent.PhaseModels = map[string]string{"validation": "workflow-lite"}
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["llm-runtime.yaml"]
				Expect(data).NotTo(ContainSubstring("apiKeyFile"),
					"#233: a phase sharing KA's credentialsSecretName must keep inheriting the base's already-mounted credentials, not get a redundant apiKeyFile")
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

			It("LR-031 [#241]: a phase's own temperature is independently configurable from the base agent's, so per-phase model-compatibility tuning actually takes effect", func() {
				kn := testKubernaut()
				mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
					p.Temperature = "0.7"
				})
				kn.Spec.LLMProfiles["workflow_discovery_profile"] = kubernautv1alpha1.LLMProfileSpec{
					Provider:              "anthropic",
					Model:                 "claude-sonnet-4-6",
					CredentialsSecretName: "llm-creds",
					Temperature:           "0.3",
				}
				kn.Spec.KubernautAgent.PhaseModels = map[string]string{"workflow_discovery": "workflow_discovery_profile"}
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				var root struct {
					PhaseModels map[string]struct {
						Temperature *float64 `yaml:"temperature"`
					} `yaml:"phaseModels"`
				}
				Expect(yaml.Unmarshal([]byte(cm.Data["llm-runtime.yaml"]), &root)).To(Succeed())
				phase, ok := root.PhaseModels["workflow_discovery"]
				Expect(ok).To(BeTrue(), "expected phaseModels.workflow_discovery entry")
				Expect(phase.Temperature).NotTo(BeNil(), "#241: expected phaseModels.workflow_discovery.temperature when its own profile sets Temperature -- without this, a phase pinned to a model needing a different temperature than the base agent would silently not apply it at runtime")
				Expect(*phase.Temperature).To(Equal(0.3), "#241: phase override's temperature must reflect its own profile (0.3), not the base agent's (0.7)")
			})

			It("LR-032 [#241]: a phase without its own temperature omits it, even when the base agent has one configured (mirrors the primary-profile fix in #239 -- unset must mean 'let the provider default')", func() {
				kn := testKubernaut()
				mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
					p.Temperature = "0.7"
				})
				kn.Spec.LLMProfiles["validation_profile"] = kubernautv1alpha1.LLMProfileSpec{
					Provider:              "anthropic",
					Model:                 "claude-sonnet-4-6",
					CredentialsSecretName: "llm-creds",
				}
				kn.Spec.KubernautAgent.PhaseModels = map[string]string{"validation": "validation_profile"}
				cm, err := KubernautAgentLLMRuntimeConfigMap(kn)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["llm-runtime.yaml"]
				var root struct {
					PhaseModels map[string]struct {
						Temperature *float64 `yaml:"temperature"`
					} `yaml:"phaseModels"`
				}
				Expect(yaml.Unmarshal([]byte(data), &root)).To(Succeed())
				phase, ok := root.PhaseModels["validation"]
				Expect(ok).To(BeTrue(), "expected phaseModels.validation entry")
				Expect(phase.Temperature).To(BeNil(), "#241: phaseModels.validation.temperature must stay absent when its own profile has none, even though the base agent's does -- a phase-specific profile swap must not inherit a temperature it never opted into, and some models (e.g. claude-opus-4) reject an explicit temperature entirely")
			})
		})

		Context("Fleet Gateway Discovery (#204)", func() {
			It("KFG-010 [CM-6]: omits integrations.fleet entirely when spec.fleet is disabled — KA registers no fleet discovery tools by default", func() {
				kn := testKubernaut()
				cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["config.yaml"]
				Expect(data).NotTo(ContainSubstring("fleet:"), "KA config should omit integrations.fleet when spec.fleet.enabled is false, got:\n%s", data)
			})

			It("KFG-011 [CM-6]: renders integrations.fleet.endpoint/gatewayType verbatim from spec.fleet when enabled (kuadrant)", func() {
				kn, knV2 := testKubernautWithFleetMCP()
				knV2.Spec.Fleet.MCPGatewayType = mcpGatewayTypeKuadrant
				cm, err := KubernautAgentConfigMap(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				var root struct {
					Integrations struct {
						Fleet *struct {
							Endpoint    string `yaml:"endpoint"`
							GatewayType string `yaml:"gatewayType"`
						} `yaml:"fleet"`
					} `yaml:"integrations"`
				}
				Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
				Expect(root.Integrations.Fleet).NotTo(BeNil(), "integrations.fleet should be present when spec.fleet.enabled is true")
				Expect(root.Integrations.Fleet.Endpoint).To(Equal(knV2.Spec.Fleet.MCPGatewayEndpoint), "integrations.fleet.endpoint = %q, want %q", root.Integrations.Fleet.Endpoint, knV2.Spec.Fleet.MCPGatewayEndpoint)
				Expect(root.Integrations.Fleet.GatewayType).To(Equal(mcpGatewayTypeKuadrant), "integrations.fleet.gatewayType = %q, want %q", root.Integrations.Fleet.GatewayType, mcpGatewayTypeKuadrant)
			})

			It("KFG-011b [CM-6]: renders integrations.fleet.gatewayType verbatim for eaigw too", func() {
				kn, knV2 := testKubernautWithFleetMCP()
				cm, err := KubernautAgentConfigMap(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["config.yaml"]
				Expect(data).To(ContainSubstring("gatewayType: eaigw"), "KA config should render gatewayType: eaigw, got:\n%s", data)
			})

			It("KFG-012 [CM-6]: omits integrations.fleet.oauth2 when spec.fleet.oauth2.enabled is false, even though fleet itself is enabled", func() {
				kn, knV2 := testKubernautWithFleetMCP()
				cm, err := KubernautAgentConfigMap(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["config.yaml"]
				Expect(data).NotTo(ContainSubstring("oauth2:"), "KA should not send fleet OAuth2 credentials it wasn't configured with, got:\n%s", data)
			})

			It("KFG-013 [CM-6]: integrations.fleet.oauth2.credentialsSecretRef uses KA's own override when set, so KA can authenticate as a distinct OAuth2 client from other fleet-aware components", func() {
				kn, knV2 := testKubernautWithFleetMCP()
				knV2.Spec.Fleet.OAuth2 = kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "shared-fleet-oauth2-creds",
				}
				knV2.Spec.KubernautAgent.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: "ka-oauth2-creds"}
				cm, err := KubernautAgentConfigMap(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				var root struct {
					Integrations struct {
						Fleet *struct {
							OAuth2 *struct {
								CredentialsSecretRef string `yaml:"credentialsSecretRef"`
							} `yaml:"oauth2"`
						} `yaml:"fleet"`
					} `yaml:"integrations"`
				}
				Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
				Expect(root.Integrations.Fleet.OAuth2).NotTo(BeNil(), "integrations.fleet.oauth2 should be present when spec.fleet.oauth2.enabled is true")
				Expect(root.Integrations.Fleet.OAuth2.CredentialsSecretRef).To(Equal("ka-oauth2-creds"), "integrations.fleet.oauth2.credentialsSecretRef should use KA's own override, got %q", root.Integrations.Fleet.OAuth2.CredentialsSecretRef)
			})

			It("KFG-013b [CM-6]: integrations.fleet.oauth2.credentialsSecretRef falls back to spec.fleet.oauth2.credentialsSecretRef when KA has no override", func() {
				kn, knV2 := testKubernautWithFleetMCP()
				knV2.Spec.Fleet.OAuth2 = kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "shared-fleet-oauth2-creds",
				}
				cm, err := KubernautAgentConfigMap(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["config.yaml"]
				Expect(data).To(ContainSubstring("credentialsSecretRef: shared-fleet-oauth2-creds"), "KA should fall back to the shared spec.fleet.oauth2.credentialsSecretRef, got:\n%s", data)
			})

			It("KFG-014 [CM-6]: renders integrations.fleet.oauth2.tokenURL/scopes verbatim from spec.fleet.oauth2", func() {
				kn, knV2 := testKubernautWithFleetMCP()
				knV2.Spec.Fleet.OAuth2 = kubernautv1alpha2.OAuth2Spec{
					Enabled: true, TokenURL: "https://keycloak.example.com/token",
					CredentialsSecretRef: "fleet-oauth2-creds",
					Scopes:               []string{"fleet:read"},
				}
				cm, err := KubernautAgentConfigMap(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				data := cm.Data["config.yaml"]
				Expect(data).To(ContainSubstring("tokenURL: https://keycloak.example.com/token"), "KA config should contain fleet oauth2 tokenURL, got:\n%s", data)
				Expect(data).To(ContainSubstring("fleet:read"), "KA config should contain fleet oauth2 scopes, got:\n%s", data)
			})
		})
	})

	Describe("AuthWebhook ConfigMap", func() {
		It("writes authwebhook.yaml as the config key", func() {
			kn := testKubernaut()
			cm, err := AuthWebhookConfigMap(kn, testKnV2(kn))
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
				{"gateway", func() (*corev1.ConfigMap, error) { return GatewayConfigMap(kn, testKnV2(kn)) }},
				{"datastorage", func() (*corev1.ConfigMap, error) { return DataStorageConfigMap(kn, testKnV2(kn), "db", "user") }},
				{"aianalysis", func() (*corev1.ConfigMap, error) { return AIAnalysisConfigMap(kn, testKnV2(kn)) }},
				{"signalprocessing", func() (*corev1.ConfigMap, error) { return SignalProcessingConfigMap(kn, testKnV2(kn)) }},
				{"remediationorchestrator", func() (*corev1.ConfigMap, error) { return RemediationOrchestratorConfigMap(kn, testKnV2(kn)) }},
				{"workflowexecution", func() (*corev1.ConfigMap, error) { return WorkflowExecutionConfigMap(kn, testKnV2(kn)) }},
				{"effectivenessmonitor", func() (*corev1.ConfigMap, error) { return EffectivenessMonitorConfigMap(kn, testKnV2(kn)) }},
				{"notification-controller", func() (*corev1.ConfigMap, error) { return NotificationControllerConfigMap(kn, testKnV2(kn)) }},
				{"kubernaut-agent", func() (*corev1.ConfigMap, error) { return KubernautAgentConfigMap(kn, testKnV2(kn)) }},
				{"authwebhook", func() (*corev1.ConfigMap, error) { return AuthWebhookConfigMap(kn, testKnV2(kn)) }},
			}
			for _, b := range builders {
				cm, err := b.fn()
				Expect(err).NotTo(HaveOccurred(), "building %s ConfigMap", b.name)
				Expect(cm.Namespace).To(Equal(testSystemNamespace), "ConfigMap %q namespace = %q, want %q", cm.Name, cm.Namespace, testSystemNamespace)
			}
		})

		// #360: every audit-writing service embeds upstream's
		// internal/config.DataStorageConfig under a top-level `datastorage:`
		// section, which -- since kubernaut v1.6.0-rc2 (DD-PLATFORM-010,
		// BR-AUDIT-005 v2.0) -- validates HealthURL as a REQUIRED field and
		// fails closed at startup ("datastorage.healthUrl is required") when
		// it is empty. DataStorage itself is exempt (it doesn't audit-write
		// to itself); KubernautAgent and APIFrontend use their own
		// differently-shaped config sections, asserted separately below.
		It("renders datastorage.healthUrl for every audit-writing service (DD-PLATFORM-010, #360)", func() {
			kn := testKubernaut()
			type builder struct {
				name string
				key  string
				fn   func() (*corev1.ConfigMap, error)
			}
			auditWritingBuilders := []builder{
				{"gateway", "config.yaml", func() (*corev1.ConfigMap, error) { return GatewayConfigMap(kn, testKnV2(kn)) }},
				{"aianalysis", "config.yaml", func() (*corev1.ConfigMap, error) { return AIAnalysisConfigMap(kn, testKnV2(kn)) }},
				{"signalprocessing", "config.yaml", func() (*corev1.ConfigMap, error) { return SignalProcessingConfigMap(kn, testKnV2(kn)) }},
				{"remediationorchestrator", "remediationorchestrator.yaml", func() (*corev1.ConfigMap, error) { return RemediationOrchestratorConfigMap(kn, testKnV2(kn)) }},
				{"workflowexecution", "workflowexecution.yaml", func() (*corev1.ConfigMap, error) { return WorkflowExecutionConfigMap(kn, testKnV2(kn)) }},
				{"effectivenessmonitor", "effectivenessmonitor.yaml", func() (*corev1.ConfigMap, error) { return EffectivenessMonitorConfigMap(kn, testKnV2(kn)) }},
				{"notification-controller", "config.yaml", func() (*corev1.ConfigMap, error) { return NotificationControllerConfigMap(kn, testKnV2(kn)) }},
				{"authwebhook", "authwebhook.yaml", func() (*corev1.ConfigMap, error) { return AuthWebhookConfigMap(kn, testKnV2(kn)) }},
			}
			wantHealthURL := "healthUrl: " + DataStorageHealthURL(testSystemNamespace)
			for _, b := range auditWritingBuilders {
				cm, err := b.fn()
				Expect(err).NotTo(HaveOccurred(), "building %s ConfigMap", b.name)
				data := cm.Data[b.key]
				Expect(data).To(ContainSubstring(wantHealthURL),
					"%s %s should render %q so the service's own /readyz can gate on "+
						"DataStorage reachability (DD-PLATFORM-010); got:\n%s", b.name, b.key, wantHealthURL, data)
			}
		})

		It("renders integrations.dataStorage.healthUrl for KubernautAgent (#360)", func() {
			kn := testKubernaut()
			cm, err := KubernautAgentConfigMap(kn, testKnV2(kn))
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("healthUrl: " + DataStorageHealthURL(testSystemNamespace)))
		})

		It("renders agent.dsHealthURL for APIFrontend (#360)", func() {
			kn := testKubernaut()
			cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
			Expect(err).NotTo(HaveOccurred())
			data := cm.Data["config.yaml"]
			Expect(data).To(ContainSubstring("dsHealthURL: " + DataStorageHealthURL(testSystemNamespace)))
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
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return GatewayConfigMap(kn, testKnV2(kn))
				},
			),
			Entry("datastorage",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.DataStorage.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return DataStorageConfigMap(kn, testKnV2(kn), "kubernautdb", "kubernautuser")
				},
			),
			Entry("aianalysis",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.AIAnalysis.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return AIAnalysisConfigMap(kn, testKnV2(kn))
				},
			),
			Entry("signalprocessing",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.SignalProcessing.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return SignalProcessingConfigMap(kn, testKnV2(kn))
				},
			),
			Entry("remediationorchestrator",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.RemediationOrchestrator.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"remediationorchestrator.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return RemediationOrchestratorConfigMap(kn, testKnV2(kn))
				},
			),
			Entry("workflowexecution",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.WorkflowExecution.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"workflowexecution.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return WorkflowExecutionConfigMap(kn, testKnV2(kn))
				},
			),
			Entry("effectivenessmonitor",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.EffectivenessMonitor.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"effectivenessmonitor.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return EffectivenessMonitorConfigMap(kn, testKnV2(kn))
				},
			),
			Entry("notification-controller",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.Notification.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return NotificationControllerConfigMap(kn, testKnV2(kn))
				},
			),
			Entry("kubernaut-agent",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.KubernautAgent.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return KubernautAgentConfigMap(kn, testKnV2(kn))
				},
			),
			Entry("authwebhook",
				func(kn *kubernautv1alpha1.Kubernaut) {
					kn.Spec.AuthWebhook.Logging.Level = loggingLevelAllServicesTestLevel
				},
				"authwebhook.yaml",
				func(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
					return AuthWebhookConfigMap(kn, testKnV2(kn))
				},
			),
		)

		// #406 (BR-PLATFORM-012, AC-6): a single shared spec.debug.pprofEnabled
		// field (KubernautSpec root) drives every one of the 12 services'
		// rendered configs -- there is no per-component override anymore
		// (DD-406 replaced #403's per-component DebugSpec embedding, since
		// every observed real-world usage was all-or-nothing). Each entry
		// below builds its own kn/knV2 fixture and renders with the flag
		// off, then flips the single root-level knV2.Spec.Debug.PprofEnabled
		// and renders again -- proving the global flag reaches all 12
		// services, not per-component isolation (which no longer exists).
		// KubernautAgent nests the rendered field under runtime.debug
		// (matching its existing runtime.* layout); the other 11 render it
		// at the config root -- ContainSubstring on the rendered value is
		// nesting-agnostic, so one table covers both shapes.
		DescribeTable("debug.pprofEnabled propagates from the single global toggle to every service ConfigMap (#406)",
			func(build func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut), key string, fn func(*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error)) {
				kn, knV2 := build()

				cmOff, err := fn(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				dataOff := cmOff.Data[key]
				Expect(dataOff).To(ContainSubstring("pprofEnabled: false"),
					"expected pprofEnabled: false by default (AC-6 secure-by-default) in %s, got:\n%s", key, dataOff)

				knV2.Spec.Debug.PprofEnabled = true
				cmOn, err := fn(kn, knV2)
				Expect(err).NotTo(HaveOccurred())
				dataOn := cmOn.Data[key]
				Expect(dataOn).To(ContainSubstring("pprofEnabled: true"),
					"expected pprofEnabled: true after the single global toggle is enabled in %s, got:\n%s", key, dataOn)
			},
			Entry("gateway",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return GatewayConfigMap(kn, knV2)
				},
			),
			Entry("datastorage",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return DataStorageConfigMap(kn, knV2, "kubernautdb", "kubernautuser")
				},
			),
			Entry("fleetmetadatacache",
				testKubernautWithFMC,
				"config.yaml",
				FleetMetadataCacheConfigMap,
			),
			Entry("aianalysis",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return AIAnalysisConfigMap(kn, knV2)
				},
			),
			Entry("signalprocessing",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return SignalProcessingConfigMap(kn, knV2)
				},
			),
			Entry("remediationorchestrator",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"remediationorchestrator.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return RemediationOrchestratorConfigMap(kn, knV2)
				},
			),
			Entry("workflowexecution",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"workflowexecution.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return WorkflowExecutionConfigMap(kn, knV2)
				},
			),
			Entry("effectivenessmonitor",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"effectivenessmonitor.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return EffectivenessMonitorConfigMap(kn, knV2)
				},
			),
			Entry("notification-controller",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return NotificationControllerConfigMap(kn, knV2)
				},
			),
			Entry("kubernaut-agent (nested under runtime.debug)",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return KubernautAgentConfigMap(kn, knV2)
				},
			),
			Entry("authwebhook",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"authwebhook.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return AuthWebhookConfigMap(kn, knV2)
				},
			),
			Entry("apifrontend",
				func() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
					kn := testKubernaut()
					return kn, testKnV2(kn)
				},
				"config.yaml",
				func(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
					return APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
				},
			),
		)

		// #406: a single spec.debug.pprofEnabled=true must flip every one
		// of the 12 services simultaneously -- this is the business-level
		// guarantee the global toggle exists to provide (the previous
		// per-component design required setting the same boolean in up to
		// 12 places to get this same effect). One CR mutation is enough.
		It("enables pprof on all 12 services simultaneously from one root-level flag (#406)", func() {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			knV2.Spec.Debug.PprofEnabled = true
			knFMC, knV2FMC := testKubernautWithFMC()
			knV2FMC.Spec.Debug.PprofEnabled = true

			renders := map[string]string{}
			var err error

			cm, err := GatewayConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			renders["gateway"] = cm.Data["config.yaml"]

			cm, err = DataStorageConfigMap(kn, knV2, "kubernautdb", "kubernautuser")
			Expect(err).NotTo(HaveOccurred())
			renders["datastorage"] = cm.Data["config.yaml"]

			cm, err = FleetMetadataCacheConfigMap(knFMC, knV2FMC)
			Expect(err).NotTo(HaveOccurred())
			renders["fleetmetadatacache"] = cm.Data["config.yaml"]

			cm, err = AIAnalysisConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			renders["aianalysis"] = cm.Data["config.yaml"]

			cm, err = SignalProcessingConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			renders["signalprocessing"] = cm.Data["config.yaml"]

			cm, err = RemediationOrchestratorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			renders["remediationorchestrator"] = cm.Data["remediationorchestrator.yaml"]

			cm, err = WorkflowExecutionConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			renders["workflowexecution"] = cm.Data["workflowexecution.yaml"]

			cm, err = EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			renders["effectivenessmonitor"] = cm.Data["effectivenessmonitor.yaml"]

			cm, err = NotificationControllerConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			renders["notification"] = cm.Data["config.yaml"]

			cm, err = KubernautAgentConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			renders["kubernautagent"] = cm.Data["config.yaml"]

			cm, err = AuthWebhookConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			renders["authwebhook"] = cm.Data["authwebhook.yaml"]

			cm, err = APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
			Expect(err).NotTo(HaveOccurred())
			renders["apifrontend"] = cm.Data["config.yaml"]

			Expect(renders).To(HaveLen(12), "expected all 12 components rendered")
			for component, rendered := range renders {
				Expect(rendered).To(ContainSubstring("pprofEnabled: true"),
					"expected pprofEnabled: true for %s after enabling the single global toggle once, got:\n%s", component, rendered)
			}
		})
	})
})

var _ = Describe("APIFrontendConfigMap", func() {
	It("generates a valid config.yaml", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("port: 8443"))
		Expect(data).NotTo(ContainSubstring("issuerURL: https://"))
	})

	// #423 coverage backfill: server.healthPort/metricsPort overrides had
	// zero test references anywhere in the codebase per
	// docs/tests/421/CRD_FIELD_COVERAGE_AUDIT.md.
	It("[CM-6] propagates spec.apiFrontend.healthPort and metricsPort overrides onto server config", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.HealthPort = ptr.To(int32(18081))
		kn.Spec.APIFrontend.MetricsPort = ptr.To(int32(19090))
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("healthPort: 18081"), "spec.apiFrontend.healthPort should override the server's health port, got:\n%s", data)
		Expect(data).To(ContainSubstring("metricsPort: 19090"), "spec.apiFrontend.metricsPort should override the server's metrics port, got:\n%s", data)
	})

	// #423 coverage backfill: all 4 rateLimit.* fields had zero test
	// references anywhere in the codebase.
	It("[SC-5] propagates spec.apiFrontend.rateLimit overrides onto the rendered rateLimit block", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RateLimit = kubernautv1alpha1.APIFrontendRateLimitSpec{
			IPRequestsPerSec:      ptr.To(12345),
			UserRequestsPerSec:    ptr.To(234),
			MaxConcurrentSessions: ptr.To(77),
			ToolCallsPerMinute:    ptr.To(999),
		}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("ipRequestsPerSec: 12345"), "spec.apiFrontend.rateLimit.ipRequestsPerSec should propagate verbatim, got:\n%s", data)
		Expect(data).To(ContainSubstring("userRequestsPerSec: 234"), "spec.apiFrontend.rateLimit.userRequestsPerSec should propagate verbatim, got:\n%s", data)
		Expect(data).To(ContainSubstring("maxConcurrentSessions: 77"), "spec.apiFrontend.rateLimit.maxConcurrentSessions should propagate verbatim, got:\n%s", data)
		Expect(data).To(ContainSubstring("toolCallsPerMinute: 999"), "spec.apiFrontend.rateLimit.toolCallsPerMinute should propagate verbatim, got:\n%s", data)
	})

	It("[SC-5] defaults spec.apiFrontend.rateLimit fields when unset", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("ipRequestsPerSec: 50"), "unset rateLimit.ipRequestsPerSec should default to 50, got:\n%s", data)
		Expect(data).To(ContainSubstring("userRequestsPerSec: 20"), "unset rateLimit.userRequestsPerSec should default to 20, got:\n%s", data)
		Expect(data).To(ContainSubstring("maxConcurrentSessions: 100"), "unset rateLimit.maxConcurrentSessions should default to 100, got:\n%s", data)
		Expect(data).To(ContainSubstring("toolCallsPerMinute: 60"), "unset rateLimit.toolCallsPerMinute should default to 60, got:\n%s", data)
	})

	It("uses OCP service-ca for severity triage when monitoring is enabled", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("prometheusTlsCaFile: /etc/ssl/af/service-ca.crt"))
	})

	It("renders auth issuerURL and audience from spec", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("https://login.kubernaut.ai/realms/kubernaut"))
		Expect(data).To(ContainSubstring("kubernaut-apifrontend"))
	})

	It("hardcodes agent card name to Kubernaut Agent", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("name: Kubernaut Agent"))
	})

	It("sets session.namespace to the CR namespace for prompt context", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("namespace: kubernaut-system"),
			"session.namespace must be set so AF BuildInstruction injects deployment context into the prompt")
	})

	It("keeps server.port at 8443 for authbridge sidecar (kagenti 0.3.x)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarAuthbridge, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("port: 8443"),
			"AF declares 8443; kagenti webhook shifts AF to 8444 and authbridge takes 8443")
	})

	It("keeps server.port at 8443 for envoy sidecar (kagenti 0.2.x)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarEnvoy, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("port: 8443"),
			"envoy sidecar uses iptables; AF keeps original port")
	})

	It("disables AF TLS for authbridge sidecar", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarAuthbridge, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("certDir: \"\""))
		Expect(data).To(ContainSubstring("required: false"))
	})

	It("disables AF TLS for envoy sidecar", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = boolPtr(true)
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarEnvoy, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("certDir: \"\""))
		Expect(data).To(ContainSubstring("required: false"))
	})

	It("enables AF TLS when no sidecar is active", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("certDir: /etc/apifrontend/tls"))
		Expect(data).To(ContainSubstring("required: true"))
	})

	It("renders rate limit defaults", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("ipRequestsPerSec: 50"))
		Expect(data).To(ContainSubstring("userRequestsPerSec: 20"))
	})

	It("renders resilience circuit breaker config", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("cbFailureThreshold:"))
		Expect(data).To(ContainSubstring("retryMax:"))
	})

	It("AF-TT-001 [CM-6]: renders mcp.sessionIdleTimeout/toolTimeout/toolTimeouts matching AF's own config.DefaultConfig() values", func() {
		// #374's root cause was a hand-maintained operator-side copy of
		// these values silently drifting from AF's own binary defaults
		// (pkg/apifrontend/config.DefaultConfig()) when AF added new
		// tools. #258 reintroduces these as CRD-configurable fields
		// (see "APIFrontend MCP Config" below) whose +kubebuilder:default
		// values are set to match AF's binary defaults exactly, making
		// the CRD the single source of truth instead of a second,
		// independently-maintained copy.
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]

		var root afConfigYAML
		Expect(yaml.Unmarshal([]byte(data), &root)).To(Succeed())
		Expect(root.MCP.Enabled).To(BeTrue())
		Expect(root.MCP.SessionIdleTimeout).To(Equal("30m"))
		Expect(root.MCP.ToolTimeout).To(Equal("30s"))
	})

	It("renders replayCache when Valkey secret is set", func() {
		kn := testKubernautWithAF()
		kn.Spec.Valkey.SecretName = "my-valkey-secret"
		kn.Spec.Valkey.Host = "valkey.kubernaut-system.svc.cluster.local"
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("replayCache:"))
	})

	It("renders nested agent.llm config section", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("llmEndpoint:"), "flat llmEndpoint field should not be emitted")
		Expect(data).NotTo(ContainSubstring("llmModel:"), "flat llmModel field should not be emitted")
	})

	It("#279: renders Vertex AI fields in agent.llm config with a credentials.json apiKeyFile now that kubernaut#1731 is fixed", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Provider = LLMProviderVertexAI })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Model = "gemini-2.5-pro" })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.VertexProject = "my-project" })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.VertexLocation = testVertexLocation })
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		Expect(root.Agent.LLM.APIKeyFile).To(Equal("/etc/apifrontend/llm-credentials/credentials.json"), // pre-commit:allow-sensitive -- mount-path convention constant, not a real credential/secret value
			"#279: vertex_ai now renders an explicit apiKeyFile pointing at the already-mounted credentials.json, same mount AF's own profile has used since kubernaut#1731 was fixed")
	})

	It("UT-CM-196-001 [SI-10]: AF receives openai_compatible when CR specifies openai", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Provider = LLMProviderOpenAI })
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) { p.Endpoint = testOpenAIEndpoint })
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("oauth2:"))
	})

	It("LR-030 [CM-6]: AF does not spend extra reasoning/thinking tokens unless the administrator explicitly opts in", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("reasoning:"), "CM-6: extended reasoning has real cost/latency impact and must stay off by default (agent.llm.reasoning omitted), got:\n%s", data)
	})

	It("LR-031 [CM-6]: AF's reasoning/thinking-token policy exactly matches what the administrator configured on the profile", func() {
		kn := testKubernautWithAF()
		mutateLLMProfile(kn, func(p *kubernautv1alpha1.LLMProfileSpec) {
			p.Reasoning = &kubernautv1alpha1.LLMReasoningSpec{Enabled: true, Effort: "medium", CapabilityOverride: "force_off"}
		})
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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

	// #298: afSeverityTriageConfig hardcoded OCPPrometheusURL, ignoring
	// spec.monitoring.prometheus.url entirely.
	It("overrides severityTriage.prometheusURL from spec.monitoring.prometheus.url (#298)", func() {
		kn := testKubernautWithAF()
		knV2 := testKnV2(kn)
		knV2.Spec.Monitoring.Prometheus.URL = testCustomPrometheusURL
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			SeverityTriage struct {
				PrometheusURL string `yaml:"prometheusURL"`
			} `yaml:"severityTriage"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.PrometheusURL).To(Equal(testCustomPrometheusURL),
			"AF severityTriage.prometheusURL should use spec.monitoring.prometheus.url override, got %q", root.SeverityTriage.PrometheusURL)
	})

	// #424: spec.monitoring.prometheus.tlsCaFile was a CRD field with zero
	// non-test references anywhere in the codebase --
	// severityTriage.prometheusTlsCaFile was always the hardcoded
	// "/etc/ssl/af/service-ca.crt" literal regardless of this field. AF has
	// no AlertManager client (afSeverityTriageConfig only ever sets
	// PrometheusURL), so spec.monitoring.alertManager.tlsCaFile has no AF
	// counterpart to wire.
	It("overrides severityTriage.prometheusTlsCaFile from spec.monitoring.prometheus.tlsCaFile (#424)", func() {
		kn := testKubernautWithAF()
		knV2 := testKnV2(kn)
		knV2.Spec.Monitoring.Prometheus.TLSCaFile = testCustomPrometheusTLSCaFile
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			SeverityTriage struct {
				PrometheusTLSCAFile string `yaml:"prometheusTlsCaFile"`
			} `yaml:"severityTriage"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.PrometheusTLSCAFile).To(Equal(testCustomPrometheusTLSCaFile),
			"AF severityTriage.prometheusTlsCaFile should use spec.monitoring.prometheus.tlsCaFile override, got %q", root.SeverityTriage.PrometheusTLSCAFile)
	})

	It("unset preserves today's hardcoded severityTriage.prometheusTlsCaFile default (regression guard, #424)", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			SeverityTriage struct {
				PrometheusTLSCAFile string `yaml:"prometheusTlsCaFile"`
			} `yaml:"severityTriage"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.PrometheusTLSCAFile).To(Equal("/etc/ssl/af/service-ca.crt"),
			"AF severityTriage.prometheusTlsCaFile should preserve the hardcoded default when unset, got %q", root.SeverityTriage.PrometheusTLSCAFile)
	})

	It("severityTriage.llm is omitted by default, inheriting AF's agent.llm connection", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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

	It("LR-033 [IA-5]: emits apiKeyFile pointing at a dedicated mount when severityTriage's own profile has a different credentialsSecretName than AF's (#234)", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["triage-other-creds"] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              "anthropic",
			Model:                 "claude-haiku-4-6",
			Endpoint:              "https://api.anthropic.com",
			CredentialsSecretName: "different-secret",
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{LLMProfileRef: "triage-other-creds"}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			SeverityTriage struct {
				LLM *struct {
					APIKeyFile string `yaml:"apiKeyFile"`
				} `yaml:"llm"`
			} `yaml:"severityTriage"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.LLM).NotTo(BeNil())
		Expect(root.SeverityTriage.LLM.APIKeyFile).To(Equal("/etc/apifrontend/severity-triage-credentials/api_key"),
			"#234: a severityTriage override with its own credentialsSecretName must render its own apiKeyFile pointing at its dedicated mount, not AF's shared llm-credentials one")
	})

	It("LR-034 [IA-5]: emits vertexProject/vertexLocation and a dedicated credentials.json apiKeyFile for a vertex_ai severityTriage override with its own credentials (#279, kubernaut#1731 is fixed)", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["triage-vertex"] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              LLMProviderVertexAI,
			Model:                 "gemini-2.5-flash",
			CredentialsSecretName: "triage-vertex-creds",
			VertexProject:         "example-gcp-project",
			VertexLocation:        "us-central1",
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{LLMProfileRef: "triage-vertex"}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			SeverityTriage struct {
				LLM *struct {
					APIKeyFile     string `yaml:"apiKeyFile"`
					VertexProject  string `yaml:"vertexProject"`
					VertexLocation string `yaml:"vertexLocation"`
				} `yaml:"llm"`
			} `yaml:"severityTriage"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.LLM).NotTo(BeNil())
		Expect(root.SeverityTriage.LLM.APIKeyFile).To(Equal("/etc/apifrontend/severity-triage-credentials/credentials.json"), // pre-commit:allow-sensitive -- mount-path convention constant, not a real credential/secret value
			"#279: a vertex_ai severityTriage override with its own credentialsSecretName must render its own credentials.json apiKeyFile pointing at its dedicated mount, mirroring #233's KA phaseModels pattern")
		Expect(root.SeverityTriage.LLM.VertexProject).To(Equal("example-gcp-project"))
		Expect(root.SeverityTriage.LLM.VertexLocation).To(Equal("us-central1"))
	})

	It("LR-035 [IA-5]: emits AF's own shared apiKeyFile when severityTriage shares AF's credentialsSecretName (regression guard, #234)", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["triage-shared-creds"] = kubernautv1alpha1.LLMProfileSpec{
			Provider:              "anthropic",
			Model:                 "claude-haiku-4-6",
			Endpoint:              "https://api.anthropic.com",
			CredentialsSecretName: "llm-creds", // same as testKubernaut()'s "primary" profile
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{LLMProfileRef: "triage-shared-creds"}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		var root struct {
			SeverityTriage struct {
				LLM *struct {
					APIKeyFile string `yaml:"apiKeyFile"`
				} `yaml:"llm"`
			} `yaml:"severityTriage"`
		}
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.LLM).NotTo(BeNil())
		Expect(root.SeverityTriage.LLM.APIKeyFile).To(Equal("/etc/apifrontend/llm-credentials/api_key"),
			"#234: a severityTriage profile sharing AF's credentialsSecretName must keep pointing at AF's already-mounted shared credentials, not a redundant dedicated one")
	})

	// #224: AF backs the list_clusters MCP tool and routes remote reads via
	// a ClusterRegistry, reusing the shared fleet.FleetConfig shape GW/RO
	// already use (upstream pkg/apifrontend/config.Config.Fleet). #464:
	// upstream kubernaut#2025/#2022 added a Backend/Endpoint scope-check
	// adapter call to AF's own checkRRScope path (every RR creation, local
	// or fleet) -- AF now needs backend/endpoint/tlsCAFile/tokenPath
	// rendered just like GW/RO, not stripped.
	It("omits the fleet block when fleet is disabled", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("fleet:"), "apifrontend config should omit fleet block when disabled, got:\n%s", data)
	})

	It("[AC-3] renders the fleet block with backend/endpoint/tlsCAFile when enabled (#464)", func() {
		kn := testKubernautWithAF()
		enabled := true
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
		}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("fleet:"), "apifrontend config should contain fleet block when enabled, got:\n%s", data)
		Expect(data).To(ContainSubstring("mcpGatewayEndpoint: https://mcp-gateway.example.com/sse"), "apifrontend config should render mcpGatewayEndpoint, got:\n%s", data)
		Expect(data).To(ContainSubstring("mcpGatewayType: eaigw"), "apifrontend config should render mcpGatewayType, got:\n%s", data)
		Expect(data).To(ContainSubstring("backend: fleetmetadatacache"), "apifrontend's own checkRRScope path now calls the Backend/Endpoint scope-check adapter (kubernaut#2025/#2022) -- backend must be rendered, not stripped, got:\n%s", data)
		Expect(data).To(ContainSubstring("endpoint: https://fmc.kubernaut.svc:8443"), "apifrontend fleet block must render endpoint so the scope-check adapter can reach the backend, got:\n%s", data)
		Expect(data).To(ContainSubstring("tlsCAFile: "+apifrontendTLSCAFile), "apifrontend fleet block should default the top-level tlsCAFile to AF's own CA mount path when no explicit CA secret is set, got:\n%s", data)
	})

	It("[IA-5, SC-12] renders tlsCAFile and tokenPath mount paths when the corresponding secrets are set (#464)", func() {
		kn := testKubernautWithAF()
		enabled := true
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "acm", Endpoint: "https://acm-search.example.com/graphql",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			CASecretName: "fmc-ca-bundle", TokenSecretName: "acm-search-token",
		}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("tlsCAFile: /etc/fleet-tls/ca/ca.crt"), "apifrontend config should render the top-level tlsCAFile mount path when fleet.caSecretName is set, got:\n%s", data)
		Expect(data).To(ContainSubstring("tokenPath: /etc/fleet-token/token"), "apifrontend config should render tokenPath mount path when fleet.tokenSecretName is set, got:\n%s", data)
	})

	It("renders fleet.oauth2 with tlsCAFile defaulting to AF's own inter-service CA path", func() {
		kn := testKubernautWithAF()
		enabled := true
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled: true, TokenURL: "https://keycloak.example.com/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "apifrontend fleet.oauth2 should render the shared credentialsSecretRef, got:\n%s", data)
		Expect(data).To(ContainSubstring("tlsCAFile: "+apifrontendTLSCAFile), "apifrontend fleet.oauth2 should default tlsCAFile to AF's own CA mount path, got:\n%s", data)
	})

	It("renders apiFrontend.fleetOAuth2CredentialsSecretRef instead of the shared credentialsSecretRef when set", func() {
		kn := testKubernautWithAF()
		enabled := true
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled: true, TokenURL: "https://keycloak.example.com/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}
		knV2.Spec.APIFrontend.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testAFFleetOAuth2SecretRef}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("credentialsSecretRef: af-oauth2-creds"), "apifrontend config should use its own oauth2 client override, got:\n%s", data)
		Expect(data).NotTo(ContainSubstring("credentialsSecretRef: fleet-oauth2-creds"), "apifrontend config should not fall back to the shared credentialsSecretRef when it has its own override, got:\n%s", data)
	})

	// DD-362: AF always renders fleet.namespace from the shared
	// spec.fleet.mcpGatewayNamespace -- there is no per-component
	// override (FleetOverrideSpec.Namespace was removed).
	It("omits fleet.namespace when spec.fleet.mcpGatewayNamespace is unset (#227)", func() {
		kn := testKubernautWithAF()
		enabled := true
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
		}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(fleetNamespaceFromYAML(data)).To(BeEmpty(), "apifrontend fleet block should omit namespace when the shared namespace is unset, got:\n%s", data)
	})

	It("renders fleet.namespace from the shared spec.fleet.mcpGatewayNamespace (#227)", func() {
		kn := testKubernautWithAF()
		enabled := true
		knV2 := testKnV2(kn)
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled, MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			MCPGatewayNamespace: "kubernaut-fleet",
		}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(fleetNamespaceFromYAML(data)).To(Equal("kubernaut-fleet"), "apifrontend fleet block should use the shared spec.fleet.mcpGatewayNamespace, got:\n%s", data)
	})

})

// #258 [CM-6]: session.disconnectTTL/retentionTTL were hardcoded; these
// tests prove spec.apiFrontend.session makes them administrator-tunable
// while preserving the current hardcoded values as defaults.
var _ = Describe("APIFrontend Session Config", func() {
	It("renders default session.disconnectTTL/retentionTTL when spec.apiFrontend.session is unset (#258) [CM-6]", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())

		var root afConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Session.DisconnectTTL).To(Equal("10m"), "session.disconnectTTL must default to the current hardcoded value for backward compatibility")
		Expect(root.Session.RetentionTTL).To(Equal("720h"), "session.retentionTTL must default to the current hardcoded value for backward compatibility")
	})

	It("renders custom session.disconnectTTL/retentionTTL from spec.apiFrontend.session (#258) [CM-6]", func() {
		kn := testKubernautWithAF()
		knV2 := testKnV2(kn)
		knV2.Spec.APIFrontend.Session = &kubernautv1alpha2.APIFrontendSessionSpec{
			DisconnectTTL: "5m",
			RetentionTTL:  "48h",
		}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())

		var root afConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.Session.DisconnectTTL).To(Equal("5m"), "session.disconnectTTL should honor spec.apiFrontend.session.disconnectTTL override")
		Expect(root.Session.RetentionTTL).To(Equal("48h"), "session.retentionTTL should honor spec.apiFrontend.session.retentionTTL override")
	})
})

// #258/#374 [CM-6]: mcp.sessionIdleTimeout/toolTimeout/toolTimeouts were
// deliberately never rendered (#374 root cause: AF's own binary defaults
// silently drifted from the operator's hand-maintained copy). Reintroducing
// them as CRD-configurable, defaulting to AF's own current binary defaults
// (config.DefaultConfig()), closes the drift risk permanently: the operator
// now has a single source of truth (the CRD) instead of a second copy that
// can go stale.
var _ = Describe("APIFrontend MCP Config", func() {
	It("renders mcp.sessionIdleTimeout/toolTimeout/toolTimeouts matching AF's own binary defaults when spec.apiFrontend.mcp is unset (#258) [CM-6]", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())

		var root afConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.MCP.Enabled).To(BeTrue())
		Expect(root.MCP.SessionIdleTimeout).To(Equal("30m"), "mcp.sessionIdleTimeout must default to AF's own binary default (30m)")
		Expect(root.MCP.ToolTimeout).To(Equal("30s"), "mcp.toolTimeout must default to AF's own binary default (30s)")
		Expect(root.MCP.ToolTimeouts).To(Equal(map[string]string{
			"kubernaut_investigate":        "15m",
			"kubernaut_await_session":      "3m",
			"kubernaut_watch":              "15m",
			"kubernaut_discover_workflows": "60s",
		}), "mcp.toolTimeouts must default to AF's own binary defaults (config.DefaultConfig())")
	})

	It("renders custom mcp.sessionIdleTimeout/toolTimeout from spec.apiFrontend.mcp (#258) [CM-6]", func() {
		kn := testKubernautWithAF()
		knV2 := testKnV2(kn)
		knV2.Spec.APIFrontend.MCP = &kubernautv1alpha2.APIFrontendMCPSpec{
			SessionIdleTimeout: "45m",
			ToolTimeout:        "10s",
		}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())

		var root afConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.MCP.SessionIdleTimeout).To(Equal("45m"), "mcp.sessionIdleTimeout should honor spec.apiFrontend.mcp.sessionIdleTimeout override")
		Expect(root.MCP.ToolTimeout).To(Equal("10s"), "mcp.toolTimeout should honor spec.apiFrontend.mcp.toolTimeout override")
	})

	It("merges a partial mcp.toolTimeouts override with the remaining per-tool defaults (#258) [CM-6]", func() {
		kn := testKubernautWithAF()
		knV2 := testKnV2(kn)
		knV2.Spec.APIFrontend.MCP = &kubernautv1alpha2.APIFrontendMCPSpec{
			ToolTimeouts: map[string]string{"kubernaut_investigate": "20m"},
		}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())

		var root afConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.MCP.ToolTimeouts["kubernaut_investigate"]).To(Equal("20m"), "explicit override must take effect")
		Expect(root.MCP.ToolTimeouts["kubernaut_await_session"]).To(Equal("3m"), "unset keys must keep their AF binary default")
		Expect(root.MCP.ToolTimeouts["kubernaut_watch"]).To(Equal("15m"), "unset keys must keep their AF binary default")
		Expect(root.MCP.ToolTimeouts["kubernaut_discover_workflows"]).To(Equal("60s"), "unset keys must keep their AF binary default")
	})
})

// #258 [CM-6]: severityTriage.cacheTTLSeconds/llmConfidence were hardcoded;
// these tests prove spec.apiFrontend.severityTriage makes them
// administrator-tunable while preserving current hardcoded defaults.
var _ = Describe("APIFrontend SeverityTriage Cache/Confidence Config", func() {
	It("renders default severityTriage.cacheTTLSeconds/llmConfidence when spec.apiFrontend.severityTriage is unset (#258) [CM-6]", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())

		var root afConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.CacheTTLSeconds).To(Equal(30), "severityTriage.cacheTTLSeconds must default to the current hardcoded value")
		Expect(root.SeverityTriage.LLMConfidence).To(Equal(0.7), "severityTriage.llmConfidence must default to the current hardcoded value")
	})

	It("renders custom severityTriage.cacheTTLSeconds/llmConfidence from spec.apiFrontend.severityTriage (#258) [CM-6]", func() {
		kn := testKubernautWithAF()
		knV2 := testKnV2(kn)
		cacheTTL := 60
		knV2.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{
			CacheTTLSeconds: &cacheTTL,
			LLMConfidence:   "0.85",
		}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())

		var root afConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
		Expect(root.SeverityTriage.CacheTTLSeconds).To(Equal(60), "severityTriage.cacheTTLSeconds should honor spec.apiFrontend.severityTriage.cacheTTLSeconds override")
		Expect(root.SeverityTriage.LLMConfidence).To(Equal(0.85), "severityTriage.llmConfidence should honor spec.apiFrontend.severityTriage.llmConfidence override")
	})
})

var _ = Describe("APIFrontendConfigMap OIDC", func() {
	It("[IA-5] propagates jwksURL to AF config for explicit JWKS endpoint trust", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWKSURL = "https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs"
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("jwksURL: https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs"),
			"IA-5: jwksURL must be propagated for explicit JWKS endpoint configuration")
	})

	It("kubernaut-operator#462: derives jwksURL from issuerURL via the Keycloak convention when jwksURL is left empty", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = "https://keycloak.example.com/realms/kubernaut"
		kn.Spec.APIFrontend.Auth.JWKSURL = ""
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("jwksURL: https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs"),
			"#462: an empty jwksURL must not be left for AF's own runtime to mishandle -- AF's fallback "+
				"treats the issuer URL itself as the JWKS endpoint, which for Keycloak silently fails every "+
				"token's signature verification")
	})

	It("kubernaut-operator#462: strips a trailing slash from issuerURL before deriving jwksURL", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = "https://keycloak.example.com/realms/kubernaut/"
		kn.Spec.APIFrontend.Auth.JWKSURL = ""
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("jwksURL: https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs"))
		Expect(data).NotTo(ContainSubstring("realms/kubernaut//protocol"))
	})

	It("[IA-5, SC-8] propagates oidcCaFile to AF config for OIDC CA verification", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.OIDCCAFile = "/etc/pki/tls/certs/oidc-ca.crt"
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("oidcCaFile: /etc/pki/tls/certs/oidc-ca.crt"),
			"IA-5: oidcCaFile must be propagated for OIDC provider CA trust")
	})

	It("[IA-5, SC-8] omits allowInsecureIssuers by default (secure-by-default)", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("allowInsecureIssuers: true"),
			"IA-5: allowInsecureIssuers must default to false (secure-by-default)")
	})

	It("[SC-8] propagates allowInsecureIssuers when explicitly enabled", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.AllowInsecureIssuers = true
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("allowInsecureIssuers: true"),
			"SC-8: allowInsecureIssuers must be propagated when explicitly set")
	})

	It("[SC-23, IA-5] propagates audience claim for token binding validation", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.Audience = "custom-audience"
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("audience: custom-audience"),
			"SC-23: audience claim must be propagated for token binding")
	})

	It("#288: never renders tokenReviewAudience/kubernetesAuthEnabled (dead keys AF's real config schema does not parse)", func() {
		// kubernaut#1900: audience-bound TokenReview was implemented and reverted
		// upstream for both AF and KA before merging (AF: unclear incremental value
		// over existing SAR authorization; KA: architecturally incompatible with the
		// shared /api/v1/mcp authenticator). AF's actual AuthConfig struct has no
		// tokenReviewAudience(s) field, so rendering either key here is a no-op that
		// upstream silently ignores.
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("tokenReviewAudience"),
			"#288: AF's real config schema has no tokenReviewAudience(s) field (kubernaut#1900 reverted upstream)")
		Expect(data).NotTo(ContainSubstring("kubernetesAuthEnabled"),
			"#288: AF's real config schema has no kubernetesAuthEnabled field")
	})
})

var _ = Describe("APIFrontendConfigMap kagenti OIDC auto-detection", func() {
	It("[IA-2, IA-5, SC-8] uses kagenti-detected issuerURL when CR field is empty", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		oidc := &KagentiOIDCDefaults{
			IssuerURL:            "https://keycloak.example.com/realms/kagenti",
			JWKSURL:              "http://keycloak-service.keycloak.svc:8080/realms/kagenti/protocol/openid-connect/certs",
			AllowInsecureIssuers: true,
		}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarAuthbridge, oidc)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("issuerURL: https://keycloak.example.com/realms/kagenti"),
			"IA-2: AF must authenticate against the kagenti-detected issuer")
		Expect(data).To(ContainSubstring("jwksURL: http://keycloak-service.keycloak.svc:8080/realms/kagenti/protocol/openid-connect/certs"),
			"IA-5: JWKS endpoint must point to in-cluster Keycloak for secure key retrieval")
		Expect(data).To(ContainSubstring("allowInsecureIssuers: true"),
			"SC-8: allowInsecureIssuers required when in-cluster JWKS uses HTTP")
	})

	It("[CM-6] CR issuerURL takes precedence over kagenti-detected value", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = "https://custom-idp.example.com/realms/custom"
		oidc := &KagentiOIDCDefaults{
			IssuerURL:            "https://keycloak.example.com/realms/kagenti",
			JWKSURL:              "http://keycloak-service.keycloak.svc:8080/realms/kagenti/protocol/openid-connect/certs",
			AllowInsecureIssuers: true,
		}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarAuthbridge, oidc)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("issuerURL: https://custom-idp.example.com/realms/custom"),
			"CM-6: explicit CR value must override auto-detected issuerURL")
		Expect(data).NotTo(ContainSubstring("issuerURL: https://keycloak.example.com/realms/kagenti"))
	})

	It("[CM-6, IA-5] CR jwksURL takes precedence over kagenti-detected value", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWKSURL = "https://custom-jwks.example.com/certs"
		oidc := &KagentiOIDCDefaults{
			IssuerURL: "https://keycloak.example.com/realms/kagenti",
			JWKSURL:   "http://keycloak-service.keycloak.svc:8080/realms/kagenti/protocol/openid-connect/certs",
		}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarAuthbridge, oidc)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("jwksURL: https://custom-jwks.example.com/certs"),
			"CM-6: explicit CR jwksURL must override auto-detected value")
	})

	It("[SC-8] CR allowInsecureIssuers=true overrides secure detection", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.AllowInsecureIssuers = true
		oidc := &KagentiOIDCDefaults{
			IssuerURL:            "https://keycloak.example.com/realms/kagenti",
			AllowInsecureIssuers: false,
		}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarAuthbridge, oidc)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("allowInsecureIssuers: true"),
			"SC-8: explicit CR allowInsecureIssuers must be honored")
	})

	It("[IA-2, SC-8] nil OIDC defaults produce unchanged behavior", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("issuerURL: https://login.kubernaut.ai/realms/kubernaut"),
			"IA-2: without kagenti, AF must use the CR-specified issuerURL")
		Expect(data).NotTo(ContainSubstring("allowInsecureIssuers: true"),
			"SC-8: allowInsecureIssuers must default to false without kagenti")
	})

	It("[IA-2] works with envoy sidecar mode (kagenti 0.2.x)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		oidc := &KagentiOIDCDefaults{
			IssuerURL: "https://keycloak.example.com/realms/kagenti",
		}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarEnvoy, oidc)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("issuerURL: https://keycloak.example.com/realms/kagenti"),
			"IA-2: auto-detection must work for both envoy and authbridge sidecar modes")
	})
})

var _ = Describe("IA-2: AF multi-provider JWT config emission", func() {
	It("[IA-2, IA-5] emits jwtProviders array enabling concurrent multi-issuer token validation", func() {
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
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

	It("[IA-2] omits jwtProviders when single-provider legacy path is used", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("jwtProviders:"),
			"IA-2: jwtProviders must not appear when no multi-provider config is set")
	})

	It("kubernaut-operator#462: does not apply the Keycloak jwksURL derivation to jwtProviders[] entries (heterogeneous IdPs, e.g. SPIRE)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = "" // isolate: only the spire provider has an issuerURL
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
			{
				Name:      "spire",
				IssuerURL: "https://spire.example.com",
				Audiences: []string{"kubernaut-workload"},
			},
		}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("protocol/openid-connect/certs"),
			"#462: the Keycloak-convention derivation is only safe for the single-provider issuerURL/jwksURL "+
				"fields -- jwtProviders[] entries can be non-Keycloak IdPs (e.g. SPIRE) and must be left as-is "+
				"when their own jwksURL is empty")
	})
})

var _ = Describe("AC-6: claim-based authorization config", func() {
	It("[AC-6] propagates claim mappings enabling group-based tool authorization", func() {
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
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("username: preferred_username"),
			"AC-6: username claim mapping must be propagated for identity extraction")
		Expect(data).To(ContainSubstring("groups: realm_roles"),
			"AC-6: groups claim mapping must be propagated for tool authorization")
	})

	It("[AC-6] omits claim mappings when not configured — AF falls back to default claim extraction", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
			{
				Name:      "spire",
				IssuerURL: "https://spire.example.com",
				Audiences: []string{"kubernaut-workload"},
			},
		}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("claimMappings:"),
			"AC-6: claimMappings must be omitted when not configured to avoid overriding AF defaults")
	})
})

var _ = Describe("SC-23: per-provider audience config", func() {
	It("[SC-23, IA-5] emits audiences array per provider for audience-scoped token validation", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
			{
				Name:      "keycloak",
				IssuerURL: "https://keycloak.example.com/realms/kubernaut",
				Audiences: []string{"kubernaut-console", "kubernaut-api"},
			},
		}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("kubernaut-console"),
			"SC-23: first audience must be propagated")
		Expect(data).To(ContainSubstring("kubernaut-api"),
			"SC-23: second audience must be propagated")
	})
})

var _ = Describe("APIFrontendConfigMap SAR", func() {
	It("[AC-6] includes rbac.sarCacheTTL with default 30s", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("sarCacheTTL: 30s"),
			"AF config should include rbac.sarCacheTTL default 30s, got:\n%s", data)
	})

	It("[AC-6] renders custom sarCacheTTL from spec", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			SARCacheTTL: "2m",
		}
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("sarCacheTTL: 2m"),
			"AF config should render custom sarCacheTTL, got:\n%s", data)
	})

	It("[AC-6] defaults rbac.consoleAccessAuthorizationCheckEnabled to false (#338)", func() {
		kn := testKubernautWithAF()
		cm, err := APIFrontendConfigMap(kn, testKnV2(kn), KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("consoleAccessAuthorizationCheckEnabled: false"),
			"AF config should default rbac.consoleAccessAuthorizationCheckEnabled to false so zero-config installs never need RBAC set up first, got:\n%s", data)
	})

	It("[AC-6] renders rbac.consoleAccessAuthorizationCheckEnabled: true when explicitly enabled via spec.apiFrontend.rbac (#338)", func() {
		kn := testKubernautWithAF()
		knV2 := testKnV2(kn)
		enabled := true
		knV2.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{ConsoleAccessAuthorizationCheckEnabled: &enabled}
		cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("consoleAccessAuthorizationCheckEnabled: true"),
			"AF config should honor an explicit spec.apiFrontend.rbac.consoleAccessAuthorizationCheckEnabled=true override, got:\n%s", data)
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
	It("[SC-12] renders signerCertDir when signing cert is configured", func() {
		kn := testKubernaut()
		kn.Spec.DataStorage.SigningCert = &kubernautv1alpha1.SigningCertSpec{
			SecretName: "datastorage-signing-cert",
		}
		cm, err := DataStorageConfigMap(kn, testKnV2(kn), "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("signerCertDir: /etc/certs"))
	})

	It("[SC-12] defaults signerCertDir to /etc/certs when signing cert is not configured", func() {
		kn := testKubernaut()
		cm, err := DataStorageConfigMap(kn, testKnV2(kn), "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("signerCertDir: /etc/certs"))
	})
})

var _ = Describe("DataStorage Redis TLS Config", func() {
	It("renders TLS config when Valkey TLS is enabled", func() {
		kn := testKubernautWithValkeyTLS()
		cm, err := DataStorageConfigMap(kn, testKnV2(kn), "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("enabled: true"))
		Expect(data).To(ContainSubstring("caFile: /etc/valkey-tls/ca/ca.crt"))
		Expect(data).To(ContainSubstring("certFile: /etc/valkey-tls/client/tls.crt"))
		Expect(data).To(ContainSubstring("keyFile: /etc/valkey-tls/client/tls.key"))
	})

	It("omits TLS block when Valkey TLS is not configured", func() {
		kn := testKubernaut()
		cm, err := DataStorageConfigMap(kn, testKnV2(kn), "testdb", "testuser")
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
		cm, err := DataStorageConfigMap(kn, testKnV2(kn), "testdb", "testuser")
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
		cm, err := DataStorageConfigMap(kn, testKnV2(kn), "testdb", "testuser")
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
		cm, err := DataStorageConfigMap(kn, testKnV2(kn), "testdb", "testuser")
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("defaultDays: 2555"))
		Expect(data).NotTo(ContainSubstring("defaultDays: 5000"))
	})

	It("[SI-12, AU-11] respects custom values", func() {
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
		cm, err := DataStorageConfigMap(kn, testKnV2(kn), "testdb", "testuser")
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

// Fleet Resilience Config (#390) mirrors upstream pkg/fleet.FleetResilienceConfig
// (kubernaut#2262 Phase 2, kubernaut PR #2268): every field is optional and
// zero-value-safe, so omitting spec.fleet.resilience entirely must leave
// every rendered ConfigMap's resilience key absent (upstream's own
// mcpclient.DefaultResilienceConfig() then supplies its existing hardcoded
// defaults unchanged). Grouped in its own top-level Describe (matching the
// "Cross-cutting" precedent above) rather than scattered across each
// component's own Describe block, since this is one CRD field threaded
// through 6 render sites, not 6 independent features.
var _ = Describe("Fleet Resilience Config (#390)", func() {
	newResilienceSpec := func() *kubernautv1alpha2.FleetResilienceSpec {
		return &kubernautv1alpha2.FleetResilienceSpec{
			InitialInterval:      "2s",
			MaxInterval:          "45s",
			MaxElapsedTime:       "10m",
			TokenRefreshTimeout:  "15s",
			ConnectTimeout:       "20s",
			DiscoverProbeTimeout: "8s",
		}
	}

	assertResilienceRendered := func(data string) {
		for _, want := range []string{
			"resilience:",
			"initialInterval: 2s",
			"maxInterval: 45s",
			"maxElapsedTime: 10m",
			"tokenRefreshTimeout: 15s",
			"connectTimeout: 20s",
			"discoverProbeTimeout: 8s",
		} {
			Expect(data).To(ContainSubstring(want), "expected rendered config to contain %q, got:\n%s", want, data)
		}
	}

	Context("Gateway/RemediationOrchestrator/APIFrontend/EffectivenessMonitor (shared fleetConfigYAML)", func() {
		It("UT-FLEET-RES-001 [CM-6]: omits resilience from GatewayConfigMap when spec.fleet.resilience is unset", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			cm, err := GatewayConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("resilience:"), "gateway config should omit resilience when spec.fleet.resilience is unset, got:\n%s", cm.Data["config.yaml"])
		})

		It("UT-FLEET-RES-002 [CM-6, SC-5]: renders fleet.resilience.* verbatim in GatewayConfigMap", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			knV2.Spec.Fleet.Resilience = newResilienceSpec()
			cm, err := GatewayConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			assertResilienceRendered(cm.Data["config.yaml"])
		})

		It("UT-FLEET-RES-003 [CM-6]: omits resilience from RemediationOrchestratorConfigMap when unset", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			cm, err := RemediationOrchestratorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["remediationorchestrator.yaml"]).NotTo(ContainSubstring("resilience:"), "RO config should omit resilience when spec.fleet.resilience is unset, got:\n%s", cm.Data["remediationorchestrator.yaml"])
		})

		It("UT-FLEET-RES-004 [CM-6, SC-5]: renders fleet.resilience.* verbatim in RemediationOrchestratorConfigMap", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			knV2.Spec.Fleet.Resilience = newResilienceSpec()
			cm, err := RemediationOrchestratorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			assertResilienceRendered(cm.Data["remediationorchestrator.yaml"])
		})

		// AF already renders an unrelated top-level "resilience:" key
		// (afResilienceYAML, #1839's per-dependency Prometheus
		// circuit-breaker config) -- a plain ContainSubstring("resilience:")
		// would false-positive against that pre-existing block, so this
		// scopes the assertion to fleet.resilience specifically by
		// unmarshaling into the real production fleetConfigYAML type
		// (package resources is white-box here).
		It("UT-FLEET-RES-005 [CM-6]: omits fleet.resilience from APIFrontendConfigMap when unset", func() {
			kn := testKubernautWithAF()
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &testFleetEnabled, MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			}
			cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
			Expect(err).NotTo(HaveOccurred())
			var root struct {
				Fleet *fleetConfigYAML `yaml:"fleet"`
			}
			Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &root)).To(Succeed())
			Expect(root.Fleet).NotTo(BeNil())
			Expect(root.Fleet.Resilience).To(BeNil(), "apifrontend fleet.resilience should be nil when spec.fleet.resilience is unset")
		})

		It("UT-FLEET-RES-006 [CM-6, SC-5]: renders fleet.resilience.* verbatim in APIFrontendConfigMap", func() {
			kn := testKubernautWithAF()
			knV2 := testKnV2(kn)
			knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
				Enabled: &testFleetEnabled, MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				Resilience: newResilienceSpec(),
			}
			cm, err := APIFrontendConfigMap(kn, knV2, KagentiSidecarNone, nil)
			Expect(err).NotTo(HaveOccurred())
			assertResilienceRendered(cm.Data["config.yaml"])
		})

		It("UT-FLEET-RES-007 [CM-6]: omits resilience from EffectivenessMonitorConfigMap when unset", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["effectivenessmonitor.yaml"]).NotTo(ContainSubstring("resilience:"), "EM config should omit resilience when spec.fleet.resilience is unset, got:\n%s", cm.Data["effectivenessmonitor.yaml"])
		})

		It("UT-FLEET-RES-008 [CM-6, SC-5]: renders fleet.resilience.* verbatim in EffectivenessMonitorConfigMap", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			knV2.Spec.Fleet.Resilience = newResilienceSpec()
			cm, err := EffectivenessMonitorConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			assertResilienceRendered(cm.Data["effectivenessmonitor.yaml"])
		})
	})

	Context("SignalProcessing (bespoke signalProcessingFleetYAML)", func() {
		It("UT-FLEET-RES-009 [CM-6]: omits resilience from SignalProcessingConfigMap when unset", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			cm, err := SignalProcessingConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("resilience:"), "SP config should omit resilience when spec.fleet.resilience is unset, got:\n%s", cm.Data["config.yaml"])
		})

		It("UT-FLEET-RES-010 [CM-6, SC-5]: renders fleet.resilience.* verbatim in SignalProcessingConfigMap", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			knV2.Spec.Fleet.Resilience = newResilienceSpec()
			cm, err := SignalProcessingConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			assertResilienceRendered(cm.Data["config.yaml"])
		})
	})

	Context("WorkflowExecution (bespoke weFleetYAML)", func() {
		It("UT-FLEET-RES-011 [CM-6]: omits resilience from WorkflowExecutionConfigMap when unset", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
			cm, err := WorkflowExecutionConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["workflowexecution.yaml"]).NotTo(ContainSubstring("resilience:"), "WE config should omit resilience when spec.fleet.resilience is unset, got:\n%s", cm.Data["workflowexecution.yaml"])
		})

		It("UT-FLEET-RES-012 [CM-6, SC-5]: renders fleet.resilience.* verbatim in WorkflowExecutionConfigMap", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			knV2.Spec.Fleet.Resilience = newResilienceSpec()
			knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
			cm, err := WorkflowExecutionConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			assertResilienceRendered(cm.Data["workflowexecution.yaml"])
		})
	})

	Context("KubernautAgent (bespoke kaFleetYAML)", func() {
		It("UT-FLEET-RES-013 [CM-6]: omits integrations.fleet.resilience from KubernautAgentConfigMap when unset", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			cm, err := KubernautAgentConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("resilience:"), "KA config should omit integrations.fleet.resilience when spec.fleet.resilience is unset, got:\n%s", cm.Data["config.yaml"])
		})

		It("UT-FLEET-RES-014 [CM-6, SC-5]: renders integrations.fleet.resilience.* verbatim in KubernautAgentConfigMap", func() {
			kn, knV2 := testKubernautWithFleetMCP()
			knV2.Spec.Fleet.Resilience = newResilienceSpec()
			cm, err := KubernautAgentConfigMap(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			assertResilienceRendered(cm.Data["config.yaml"])
		})
	})
})

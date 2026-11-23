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
	"sigs.k8s.io/yaml"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("FleetMetadataCacheConfigMap", func() {
	It("renders server, mcpGateway, valkey, sync, and oauth2 sections", func() {
		kn, knV2 := testKubernautWithFMC()
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Name).To(Equal("fleetmetadatacache-config"))

		data := cm.Data["config.yaml"]
		for _, want := range []string{
			"server:", "apiAddr: :8080", "healthAddr: :8081", "metricsAddr: :9090",
			"certDir: /etc/tls",
			"mcpGateway:", "endpoint: https://mcp-gateway.example.com/sse", "gatewayType: eaigw",
			"valkey:", "sync:", "keyTtl: 45s", "interval: 30s",
			"oauth2:", "tokenUrl: https://keycloak.example.com/token",
			"credentialsDir: /etc/fleetmetadatacache/fleet-oauth2",
		} {
			Expect(data).To(ContainSubstring(want), "FMC config should contain %q, got:\n%s", want, data)
		}
	})

	// #396: healthAddr and metricsAddr must resolve to different ports.
	// Upstream's ServiceConfig has three independent server addresses
	// (apiAddr/healthAddr/metricsAddr); omitting healthAddr here previously
	// left FMC falling back to its own default (:8081) for the health
	// server while this ConfigMap explicitly set metricsAddr to that same
	// value, so both servers tried to bind :8081 and the metrics server
	// crashed with "bind: address already in use" -- confirmed on-cluster.
	It("#396 [CM-6]: healthAddr and metricsAddr never collide", func() {
		kn, knV2 := testKubernautWithFMC()
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).To(ContainSubstring("healthAddr: :8081"),
			"FMC config must explicitly set healthAddr so it never depends on FMC's own implicit default")
		Expect(cm.Data["config.yaml"]).To(ContainSubstring("metricsAddr: :9090"),
			"metricsAddr must be a genuinely separate port from healthAddr (:8081), not reuse it")
	})

	// DD-362: FMC always renders mcpGateway.namespace from the shared
	// spec.fleet.mcpGateway.namespace -- there is no per-component
	// override (FleetOverrideSpec.Namespace was removed).
	It("renders the mcpGateway namespace from the shared spec.fleet.mcpGateway.namespace when set", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.MCPGateway.Namespace = "managed-clusters"
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).To(ContainSubstring("namespace: managed-clusters"))
	})

	It("omits the mcpGateway namespace key when unset", func() {
		kn, knV2 := testKubernautWithFMC()
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("namespace:"))
	})

	It("applies custom syncInterval and keyTTL overrides", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.FleetMetadataCache.SyncInterval = "1m"
		knV2.Spec.FleetMetadataCache.KeyTTL = "90s"
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("interval: 1m"))
		Expect(data).To(ContainSubstring("keyTtl: 90s"))
	})

	It("reuses the shared spec.valkey address", func() {
		kn, knV2 := testKubernautWithFMC()
		kn.Spec.Valkey.Host = "valkey.kubernaut.svc"
		kn.Spec.Valkey.Port = 6380
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).To(ContainSubstring(ValkeyAddr(&kn.Spec.Valkey)))
	})

	It("#267: propagates spec.fleet.oauth2.scopes to oauth2.scopes (client-credentials providers reject the openid/groups default)", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.OAuth2.Scopes = []string{"fleet.read", "fleet.write"}
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("scopes:"), "FMC config should render oauth2.scopes when set, got:\n%s", data)
		Expect(data).To(ContainSubstring("fleet.read"))
		Expect(data).To(ContainSubstring("fleet.write"))
	})

	It("#267: renders oauth2.tlsCaFile so FMC's token-source client trusts a self-signed IdP CA", func() {
		kn, knV2 := testKubernautWithFMC()
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).To(ContainSubstring("tlsCaFile: " + InterServiceTLSCAFile))
	})

	It("UT-TLS-491-009 [SC-8; SOC2 CC6, CC7; ASVS v5.0.0-V12.1.3, v5.0.0-V13.2.1] renders chart-compatible server TLS and OAuth2 CA paths", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		knV2.Spec.TLS.InterService = &kubernautv1alpha2.InterServiceTLSConfig{
			CertDir: "/custom/tls",
			CAFile:  "/custom/ca/ca.pem",
		}

		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("certDir: /custom/tls"))
		Expect(data).To(ContainSubstring("tlsCaFile: /custom/ca/ca.pem"))
	})

	// #390: mcpGateway.resilience mirrors upstream pkg/fleet.MCPGatewayConfig.Resilience
	// (kubernaut#2262 Phase 2, kubernaut PR #2268) -- FMC's
	// fmcconfig.ServiceConfig.MCPGateway is typed directly as
	// fleet.MCPGatewayConfig, so it inherits Resilience automatically.
	It("UT-FLEET-RES-015 [CM-6]: omits mcpGateway.resilience when spec.fleet.resilience is unset", func() {
		kn, knV2 := testKubernautWithFMC()
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("resilience:"), "FMC config should omit mcpGateway.resilience when spec.fleet.resilience is unset, got:\n%s", cm.Data["config.yaml"])
	})

	It("UT-FLEET-521-MCPCLIENT-001 [CM-6, SC-5]: renders mcpGateway.resilience.* from spec.fleet.mcpClient", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.MCPClient = &kubernautv1alpha2.FleetMCPClientSpec{
			Retry:    &kubernautv1alpha2.FleetMCPClientRetrySpec{InitialInterval: "2s", MaxInterval: "45s", MaxElapsedTime: "10m"},
			Timeouts: &kubernautv1alpha2.FleetMCPClientTimeoutsSpec{TokenRefresh: "15s", Connect: "20s", DiscoverProbe: "8s"},
		}
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		for _, want := range []string{
			"resilience:", "initialInterval: 2s", "maxInterval: 45s", "maxElapsedTime: 10m",
			"tokenRefreshTimeout: 15s", "connectTimeout: 20s", "discoverProbeTimeout: 8s",
		} {
			Expect(data).To(ContainSubstring(want), "FMC config should contain %q when spec.fleet.resilience is set, got:\n%s", want, data)
		}
	})

	// #398: FMC's Valkey client (unlike DataStorage/APIFrontend) never
	// rendered a valkey.tls block at all, even though upstream's own
	// pkg/fleet/fmc/config.ValkeyConfig supports TLS ValkeyTLSConfig. Per
	// upstream DD-PLATFORM-006 DA8, the chart's own Valkey is TLS-only
	// (one-way TLS -- no client cert required), so without this block FMC
	// cannot connect to a TLS-secured Valkey at all. Mirrors DataStorage's
	// exact rendering (dataStorageRedisTLSYAML) for cross-service consistency.
	It("#398 [SC-8]: omits valkey.tls when spec.valkey.tls is not configured", func() {
		kn, knV2 := testKubernautWithFMC()
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("valkey:\n    tls:"), "FMC config should omit valkey.tls when spec.valkey.tls is unset, got:\n%s", cm.Data["config.yaml"])
	})

	It("#398 [SC-8]: renders valkey.tls.* from spec.valkey.tls, mirroring DataStorage's rendering", func() {
		kn, knV2 := testKubernautWithFMC()
		kn.Spec.Valkey.TLS = &kubernautv1alpha2.ValkeyTLSSpec{
			Enabled:              true,
			CASecretName:         "valkey-ca",
			ClientCertSecretName: "valkey-client-cert",
		}
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		for _, want := range []string{
			"enabled: true", "caFile: /etc/valkey-tls/ca/ca.crt",
			"certFile: /etc/valkey-tls/client/tls.crt", "keyFile: /etc/valkey-tls/client/tls.key",
		} {
			Expect(data).To(ContainSubstring(want), "FMC config should contain %q when spec.valkey.tls is set, got:\n%s", want, data)
		}
	})
})

var _ = Describe("FleetMetadataCacheDeployment", func() {
	It("builds successfully with FMC enabled", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectDeploymentBasics(dep, "fleetmetadatacache")
	})

	It("uses shared stateless defaults when resources are unset", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(dep.Spec.Template.Spec.Containers[0].Resources).To(Equal(DefaultResources()))
	})

	It("exposes api (8080), health (8081), and metrics (9090) ports", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		portMap := map[string]int32{}
		for _, p := range container.Ports {
			portMap[p.Name] = p.ContainerPort
		}
		Expect(portMap).To(HaveKeyWithValue("api", int32(8080)))
		Expect(portMap).To(HaveKeyWithValue("health", int32(8081)))
		Expect(portMap).To(HaveKeyWithValue("metrics", int32(9090)))
	})

	It("probes /healthz and /readyz on the health port (8081), matching FMC's own healthAddr, not the api port (8080)", func() {
		// On-cluster validation (2026-08-23): probing 8080 left FMC
		// permanently stuck at 0/1 Ready (startup probe: connection
		// refused) even when otherwise healthy, since FMC's healthAddr
		// binds /healthz and /readyz on a dedicated health port, not the
		// api port which only serves request traffic.
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		for _, probe := range []*corev1.Probe{container.StartupProbe, container.ReadinessProbe, container.LivenessProbe} {
			Expect(probe).NotTo(BeNil())
			Expect(probe.HTTPGet).NotTo(BeNil())
			Expect(probe.HTTPGet.Port.IntValue()).To(Equal(8081))
		}
	})

	It("mounts config and fleet-oauth2 volumes", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(dep, "config")
		expectHasVolume(dep, "fleet-oauth2")
		expectHasVolume(dep, "tls-certs")
		expectHasVolumeMount(dep, "config", "/etc/fleetmetadatacache")
		expectHasVolumeMount(dep, "fleet-oauth2", fleetMetadataCacheOAuth2Dir)
		expectHasVolumeMount(dep, "tls-certs", InterServiceTLSCertDirFor(knV2))
		expectVolumeSourceConfigMap(dep, "config", "fleetmetadatacache-config")
		for _, volume := range dep.Spec.Template.Spec.Volumes {
			if volume.Name == "tls-certs" {
				Expect(volume.Secret).NotTo(BeNil())
				Expect(volume.Secret.SecretName).To(Equal(FleetMetadataCacheTLSSecretName))
				break
			}
		}
	})

	It("UT-FLEET-557-MOUNT-003 [AC-6, SC-8, SC-12]: mounts FMC's MCP Gateway CA Secret read-only", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.MCPGateway.TLS = &kubernautv1alpha2.FleetTrustSpec{
			Source:          kubernautv1alpha2.FleetTrustSourceSecret,
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "fleet-mcp-gateway-ca"},
		}
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(dep, fleetMCPGatewayCAVolumeName)
		expectHasVolumeMount(dep, fleetMCPGatewayCAVolumeName, fleetMCPGatewayCAMountDir)
		for _, volume := range dep.Spec.Template.Spec.Volumes {
			if volume.Name == fleetMCPGatewayCAVolumeName {
				Expect(volume.Secret).NotTo(BeNil())
				Expect(volume.Secret.SecretName).To(Equal("fleet-mcp-gateway-ca"))
			}
		}
	})

	It("#267: mounts tls-ca volume from the trust-bundle ConfigMap so the OAuth2 token-source client can trust a self-signed IdP CA", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(dep, "tls-ca")
		expectHasVolumeMount(dep, "tls-ca", "/etc/tls-ca")
		expectVolumeSourceConfigMap(dep, "tls-ca", TrustBundleConfigMapName)
	})

	It("sets SSL_CERT_FILE so the MCP Gateway session transport (which never reads OAuth2.TlsCaFile) also trusts the merged bundle", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		found := false
		for _, e := range container.Env {
			if e.Name == "SSL_CERT_FILE" && e.Value == InterServiceTLSCAFile {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "SSL_CERT_FILE env var not found")
	})

	It("#398 [SC-8]: mounts valkey-ca and valkey-client-cert when spec.valkey.tls is configured, mirroring DataStorageDeployment", func() {
		kn, knV2 := testKubernautWithFMC()
		kn.Spec.Valkey.TLS = &kubernautv1alpha2.ValkeyTLSSpec{
			Enabled:              true,
			CASecretName:         "valkey-ca",
			ClientCertSecretName: "valkey-client-cert",
		}
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(dep, "valkey-ca")
		expectHasVolume(dep, "valkey-client-cert")
		expectHasVolumeMount(dep, "valkey-ca", "/etc/valkey-tls/ca")
		expectHasVolumeMount(dep, "valkey-client-cert", "/etc/valkey-tls/client")
	})

	It("#398: does not mount valkey TLS volumes when spec.valkey.tls is disabled", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		for _, v := range dep.Spec.Template.Spec.Volumes {
			Expect(v.Name).NotTo(HavePrefix("valkey-"),
				"should not have valkey TLS volume %q when TLS is disabled", v.Name)
		}
	})

	It("mounts the shared fleet.oauth2.credentialsSecretRef when FMC has no override", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		found := false
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "fleet-oauth2" {
				found = true
				Expect(v.Secret).NotTo(BeNil())
				Expect(v.Secret.SecretName).To(Equal("fleet-oauth2-creds"))
			}
		}
		Expect(found).To(BeTrue(), "fleet-oauth2 volume not found")
	})

	It("mounts FMC's own credentialsSecretRef override when set, ignoring the shared fallback", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.FleetMetadataCache.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: "fmc-own-oauth2-creds"}
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		found := false
		for _, v := range dep.Spec.Template.Spec.Volumes {
			if v.Name == "fleet-oauth2" {
				found = true
				Expect(v.Secret.SecretName).To(Equal("fmc-own-oauth2-creds"))
			}
		}
		Expect(found).To(BeTrue(), "fleet-oauth2 volume not found")
	})

	It("passes the -config flag pointing at the mounted config file", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(dep.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-config=/etc/fleetmetadatacache/config.yaml"))
	})

	It("applies spec.fleetMetadataCache.resources", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.FleetMetadataCache.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m")},
		}
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(dep.Spec.Template.Spec.Containers[0].Resources.Requests).NotTo(BeEmpty())
	})
})

var _ = Describe("FleetMetadataCacheService", func() {
	It("selects the fleetmetadatacache component and exposes api+health+metrics ports", func() {
		kn, knV2 := testKubernautWithFMC()
		kn.Spec.Fleet = knV2.Spec.Fleet
		svc := FleetMetadataCacheService(kn)
		Expect(svc.Name).To(Equal("fleetmetadatacache-service"))
		Expect(svc.Spec.Selector).To(Equal(SelectorLabels(ComponentFleetMetadataCache)))
		Expect(svc.Annotations).To(HaveKeyWithValue(OCPServingCertAnnotation, FleetMetadataCacheTLSSecretName))

		portMap := map[string]int32{}
		for _, p := range svc.Spec.Ports {
			portMap[p.Name] = p.Port
		}
		Expect(portMap).To(HaveKeyWithValue("api", int32(8080)))
		Expect(portMap).To(HaveKeyWithValue("health", int32(8081)))
		Expect(portMap).To(HaveKeyWithValue("metrics", int32(9090)))
	})

	It("is included in Services() when fleetMetadataCache.enabled is true", func() {
		kn, knV2 := testKubernautWithFMC()
		svcs := Services(kn, knV2)
		names := make([]string, 0, len(svcs))
		for _, s := range svcs {
			names = append(names, s.Name)
		}
		Expect(names).To(ContainElement("fleetmetadatacache-service"))
	})

	It("is excluded from Services() when fleetMetadataCache.enabled is false", func() {
		kn := testKubernaut()
		svcs := Services(kn, testKnV2(kn))
		names := make([]string, 0, len(svcs))
		for _, s := range svcs {
			names = append(names, s.Name)
		}
		Expect(names).NotTo(ContainElement("fleetmetadatacache-service"))
	})
})

var _ = Describe("FleetMetadataCache RBAC", func() {
	It("grants Envoy AI Gateway CRD watch access when mcpGateway.type=eaigw", func() {
		kn, knV2 := testKubernautWithFMC()
		labels := CommonLabels(kn)
		cr := fleetMetadataCacheClusterRole(kn, knV2, labels)
		Expect(cr.Name).To(Equal(kn.Namespace + "-fleetmetadatacache"))

		apiGroups := make([]string, 0, len(cr.Rules))
		for _, r := range cr.Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).To(ContainElement("gateway.envoyproxy.io"))
		Expect(apiGroups).NotTo(ContainElement("mcp.kuadrant.io"))
	})

	It("grants Kuadrant CRD watch access when mcpGateway.type=kuadrant", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.MCPGateway.Type = mcpGatewayTypeKuadrant
		labels := CommonLabels(kn)
		cr := fleetMetadataCacheClusterRole(kn, knV2, labels)

		apiGroups := make([]string, 0, len(cr.Rules))
		for _, r := range cr.Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).To(ContainElement("mcp.kuadrant.io"))
		Expect(apiGroups).NotTo(ContainElement("gateway.envoyproxy.io"))
	})

	It("is included in ClusterRoles()/ClusterRoleBindings() only when fleetMetadataCache.enabled is true", func() {
		enabledKn, enabledKnV2 := testKubernautWithFMC()
		found := false
		for _, cr := range ClusterRoles(enabledKn, enabledKnV2) {
			if cr.Name == enabledKn.Namespace+"-fleetmetadatacache" {
				found = true
			}
		}
		Expect(found).To(BeTrue())

		foundBinding := false
		for _, crb := range ClusterRoleBindings(enabledKn, enabledKnV2) {
			if crb.Name == enabledKn.Namespace+"-fleetmetadatacache-binding" {
				foundBinding = true
			}
		}
		Expect(foundBinding).To(BeTrue())

		disabledKn := testKubernaut()
		for _, cr := range ClusterRoles(disabledKn, testKnV2(disabledKn)) {
			Expect(cr.Name).NotTo(Equal(disabledKn.Namespace + "-fleetmetadatacache"))
		}
	})
})

var _ = Describe("FleetMetadataCacheURL", func() {
	It("returns a TLS-only in-cluster URL on port 8080", func() {
		Expect(FleetMetadataCacheURL("kubernaut-system")).To(Equal("https://fleetmetadatacache-service.kubernaut-system.svc.cluster.local:8080"))
	})
})

var _ = Describe("FleetMetadataCacheEnabled helper", func() {
	It("defaults to false", func() {
		kn := testKubernaut()
		Expect(testKnV2(kn).Spec.FleetMetadataCacheEnabled()).To(BeFalse())
	})

	It("returns true when fleet.enabled is true", func() {
		_, knV2 := testKubernautWithFMC()
		Expect(knV2.Spec.FleetMetadataCacheEnabled()).To(BeTrue())
	})

	It("returns false when fleet.enabled is false", func() {
		_, knV2 := testKubernautWithFMC()
		f := false
		knV2.Spec.Fleet.Enabled = &f
		Expect(knV2.Spec.FleetMetadataCacheEnabled()).To(BeFalse())
	})
})

var _ = Describe("isComponentActive for FleetMetadataCache", func() {
	It("is inactive by default", func() {
		kn := testKubernaut()
		Expect(ActiveComponents(kn, testKnV2(kn))).NotTo(ContainElement(ComponentFleetMetadataCache))
	})

	It("is active when fleet.enabled is true", func() {
		kn, knV2 := testKubernautWithFMC()
		Expect(ActiveComponents(kn, knV2)).To(ContainElement(ComponentFleetMetadataCache))
	})
})

// resolveFleetEndpoint (used by configmaps.go for Gateway/RO's rendered
// fleet.endpoint) is exercised here too since it is the mechanism tying
// FMC's own service URL to the rendered fleet endpoint.
var _ = Describe("resolveFleetEndpoint", func() {
	It("UT-FLEET-521-FMC-001: auto-derives the in-cluster FMC URL whenever fleet is enabled", func() {
		kn, knV2 := testKubernautWithFMC()
		Expect(resolveFleetEndpoint(knV2)).To(Equal(FleetMetadataCacheURL(kn.Namespace)))
	})

	It("does not expose a user-configurable FMC endpoint", func() {
		_, knV2 := testKubernautWithFMC()
		Expect(resolveFleetEndpoint(knV2)).To(Equal(FleetMetadataCacheURL(knV2.Namespace)))
	})

	It("does not auto-derive when fleet.enabled is false", func() {
		_, knV2 := testKubernautWithFMC()
		f := false
		knV2.Spec.Fleet.Enabled = &f
		Expect(resolveFleetEndpoint(knV2)).To(Equal(""))
	})
})

var _ = Describe("cleanupDisabledFleetMetadataCache resource names", func() {
	// Guards the hardcoded names in the controller's cleanup routine against
	// silent drift from the builders here, since the controller (a
	// different package) can't reference these unexported constants.
	It("matches the ConfigMap, Service, and ClusterRole names this file produces", func() {
		kn, knV2 := testKubernautWithFMC()
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Name).To(Equal("fleetmetadatacache-config"))

		svc := FleetMetadataCacheService(kn)
		Expect(svc.Name).To(Equal("fleetmetadatacache-service"))

		cr := fleetMetadataCacheClusterRole(kn, knV2, CommonLabels(kn))
		Expect(cr.Name).To(Equal(kn.Namespace + "-fleetmetadatacache"))

		crb := fleetMetadataCacheClusterRoleBinding(kn, CommonLabels(kn))
		Expect(crb.Name).To(Equal(kn.Namespace + "-fleetmetadatacache-binding"))
	})
})

var _ = Describe("FleetMetadataCache probe configuration", func() {
	It("uses /healthz and /readyz matching upstream's own Helm chart timing", func() {
		pc := probeConfigForComponent(ComponentFleetMetadataCache)
		Expect(pc.LivenessPath).To(Equal("/healthz"))
		Expect(pc.ReadinessPath).To(Equal("/readyz"))
	})
})

var _ = Describe("AllComponents includes fleetmetadatacache", func() {
	It("is present in the master component list", func() {
		Expect(AllComponents()).To(ContainElement(ComponentFleetMetadataCache))
	})
})

var _ = Describe("PodDisruptionBudgets for FleetMetadataCache", func() {
	It("includes a PDB only when enabled", func() {
		kn, knV2 := testKubernautWithFMC()
		found := false
		for _, pdb := range PodDisruptionBudgets(kn, knV2) {
			if pdb.Name == ComponentFleetMetadataCache {
				found = true
			}
		}
		Expect(found).To(BeTrue())

		disabledKn := testKubernaut()
		for _, pdb := range PodDisruptionBudgets(disabledKn, testKnV2(disabledKn)) {
			Expect(pdb.Name).NotTo(Equal(ComponentFleetMetadataCache))
		}
	})
})

var _ = Describe("ServiceAccount for FleetMetadataCache", func() {
	It("resolves to the fleetmetadatacache SA name", func() {
		Expect(ServiceAccountName(ComponentFleetMetadataCache)).To(Equal("fleetmetadatacache"))
	})

	// #647/1e: with the consumer fleet token mount gone, FMC's scope lane
	// falls back to the in-cluster SA token (upstream scope_factory.go
	// TokenPath-absent fallback). Disabling automount for FMC's SA would
	// 401 every scope check and fail GW/RO/AF closed — pin that it can't
	// drift (only the KubernautAgent SA disables automount).
	It("UT-FLEET-647-SA-001 [IA-5]: leaves ServiceAccount token automount enabled for the SA-token fallback", func() {
		kn, _ := testKubernautWithFMC()
		sa := ServiceAccount(kn, ComponentFleetMetadataCache)
		Expect(sa.AutomountServiceAccountToken).To(BeNil(),
			"FMC authenticates with the auto-mounted SA token when tokenPath is absent — automount must not be disabled")
	})
})

// #647 (DD-FLEET-017 v2 D4.1a): FMC's ACM Search source surface. The
// rendered sources.acm YAML keys are a cross-repo contract with upstream
// pkg/fleet/fmc/config (kubernaut#2661 increment 2) — upstream decodes
// sources with known-fields-only strictness, so these tests pin the exact
// key set, not just the values.
var _ = Describe("FleetMetadataCache ACM source (#647, DD-FLEET-017 D4.1a)", func() {
	withACMSource := func(mutate func(*kubernautv1alpha2.FleetACMSourceSpec)) (*kubernautv1alpha2.Kubernaut, *kubernautv1alpha2.Kubernaut) {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.FleetMetadataCache.Sources = &kubernautv1alpha2.FleetMetadataCacheSourcesSpec{
			ACM: &kubernautv1alpha2.FleetACMSourceSpec{
				Endpoint:       "https://acm-search.example.com",
				TokenSecretRef: &kubernautv1alpha2.SecretKeyRef{Name: "acm-search-token"},
			},
		}
		if mutate != nil {
			mutate(knV2.Spec.FleetMetadataCache.Sources.ACM)
		}
		return kn, knV2
	}

	findVolume := func(dep *appsv1.Deployment, name string) *corev1.Volume {
		for i := range dep.Spec.Template.Spec.Volumes {
			if dep.Spec.Template.Spec.Volumes[i].Name == name {
				return &dep.Spec.Template.Spec.Volumes[i]
			}
		}
		return nil
	}

	findMount := func(dep *appsv1.Deployment, name string) *corev1.VolumeMount {
		for i := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
			if dep.Spec.Template.Spec.Containers[0].VolumeMounts[i].Name == name {
				return &dep.Spec.Template.Spec.Containers[0].VolumeMounts[i]
			}
		}
		return nil
	}

	// Acceptance (a): the default (GitOps/MCP lane) rendering is
	// byte-identical to today's — no sources key at all. Asserted both as
	// a substring (byte-level) and by unmarshalling into the production
	// YAML struct per the white-box convention (AGENTS.md).
	It("UT-FLEET-647-SRC-001: omits sources entirely when no source is selected", func() {
		kn, knV2 := testKubernautWithFMC()
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("sources:"))

		var cfg fleetMetadataCacheConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &cfg)).To(Succeed())
		Expect(cfg.Sources).To(BeNil())
	})

	// Cross-team alignment (kubernaut#2661, upstream row 1): FMC fails to
	// load a rendered "sources:" key that names no source, and an MCP
	// deployment must carry no sources key at all. Validation rejects an
	// empty declaration (UT-FLEET-647-VALIDATE-005); this pins that the
	// renderer, even fed an empty declaration directly, cannot emit the
	// fatal empty block — a templating artifact defense in depth.
	It("UT-FLEET-647-SRC-009 [CM-7]: never renders an empty sources key, even for an empty declaration", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.FleetMetadataCache.Sources = &kubernautv1alpha2.FleetMetadataCacheSourcesSpec{}
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).NotTo(ContainSubstring("sources:"),
			"an empty sources declaration must render no key at all — upstream FMC fails startup on an empty sources block")

		var cfg fleetMetadataCacheConfigYAML
		Expect(yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &cfg)).To(Succeed())
		Expect(cfg.Sources).To(BeNil())
	})

	It("UT-FLEET-647-SRC-002 [SC-8, SC-12; ASVS v5.0.0-V5.1.1]: renders exactly endpoint, tokenPath, and caFile with interService default trust", func() {
		kn, knV2 := withACMSource(nil)
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]

		var cfg fleetMetadataCacheConfigYAML
		Expect(yaml.Unmarshal([]byte(data), &cfg)).To(Succeed())
		Expect(cfg.Sources).NotTo(BeNil())
		Expect(cfg.Sources.ACM).NotTo(BeNil())
		Expect(cfg.Sources.ACM.Endpoint).To(Equal("https://acm-search.example.com"))
		Expect(cfg.Sources.ACM.TokenPath).To(Equal(fleetTokenMountPath))
		Expect(cfg.Sources.ACM.CAFile).To(Equal(InterServiceTLSCAFileFor(knV2)))

		// Contract pin: upstream decodes sources.acm with
		// known-fields-only strictness — exactly these three keys, no
		// extras, or FMC fails startup by design.
		var raw map[string]any
		Expect(yaml.Unmarshal([]byte(data), &raw)).To(Succeed())
		sources := raw["sources"].(map[string]any)
		Expect(sources).To(HaveLen(1))
		acm := sources["acm"].(map[string]any)
		Expect(acm).To(HaveLen(3))
		Expect(acm).To(HaveKey("endpoint"))
		Expect(acm).To(HaveKey("tokenPath"))
		Expect(acm).To(HaveKey("caFile"))
	})

	It("UT-FLEET-647-SRC-003 [SC-8]: omits caFile when the ACM source trust resolves to system", func() {
		kn, knV2 := withACMSource(func(acm *kubernautv1alpha2.FleetACMSourceSpec) {
			acm.TLS = &kubernautv1alpha2.FleetTrustSpec{Source: kubernautv1alpha2.FleetTrustSourceSystem}
		})
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).NotTo(ContainSubstring("caFile"))

		var raw map[string]any
		Expect(yaml.Unmarshal([]byte(data), &raw)).To(Succeed())
		acm := raw["sources"].(map[string]any)["acm"].(map[string]any)
		Expect(acm).To(HaveLen(2), "system trust must omit caFile entirely, not render it empty")
	})

	It("UT-FLEET-647-SRC-004 [SC-8]: passes an administrator file path through as caFile", func() {
		kn, knV2 := withACMSource(func(acm *kubernautv1alpha2.FleetACMSourceSpec) {
			acm.TLS = &kubernautv1alpha2.FleetTrustSpec{Source: kubernautv1alpha2.FleetTrustSourceFile, CAFile: "/etc/admin-ca/acm.pem"}
		})
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).To(ContainSubstring("caFile: /etc/admin-ca/acm.pem"))
	})

	It("UT-FLEET-647-SRC-005 [SC-8; ASVS v5.0.0-V9.1.1]: renders the secret-lane caFile and mounts the CA Secret read-only and keyed", func() {
		kn, knV2 := withACMSource(func(acm *kubernautv1alpha2.FleetACMSourceSpec) {
			acm.TLS = &kubernautv1alpha2.FleetTrustSpec{
				Source:          kubernautv1alpha2.FleetTrustSourceSecret,
				CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "acm-source-ca", Key: "bundle.pem"},
			}
		})
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["config.yaml"]).To(ContainSubstring("caFile: " + fleetACMSourceCAMountPath))

		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		volume := findVolume(dep, "fleet-acm-source-ca")
		Expect(volume).NotTo(BeNil(), "secret-trust ACM source must mount its CA Secret")
		Expect(volume.Secret.SecretName).To(Equal("acm-source-ca"))
		Expect(volume.Secret.Items).To(HaveLen(1))
		Expect(volume.Secret.Items[0].Key).To(Equal("bundle.pem"))
		Expect(volume.Secret.Items[0].Path).To(Equal("ca.crt"))
		mount := findMount(dep, "fleet-acm-source-ca")
		Expect(mount).NotTo(BeNil())
		Expect(mount.MountPath).To(Equal("/etc/fleet-tls/acm-source"))
		Expect(mount.ReadOnly).To(BeTrue())
	})

	It("UT-FLEET-647-SRC-006 [SC-8; ASVS v5.0.0-V9.1.1]: mounts the ACM token Secret read-only and keyed, only when the source is selected", func() {
		kn, knV2 := withACMSource(func(acm *kubernautv1alpha2.FleetACMSourceSpec) {
			acm.TokenSecretRef.Key = "bearer"
		})
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		volume := findVolume(dep, "fleet-acm-source-token")
		Expect(volume).NotTo(BeNil(), "selected ACM source must mount its token Secret")
		Expect(volume.Secret.SecretName).To(Equal("acm-search-token"))
		Expect(volume.Secret.Items).To(HaveLen(1))
		Expect(volume.Secret.Items[0].Key).To(Equal("bearer"))
		Expect(volume.Secret.Items[0].Path).To(Equal("token"))
		mount := findMount(dep, "fleet-acm-source-token")
		Expect(mount).NotTo(BeNil())
		Expect(mount.MountPath).To(Equal("/etc/fleet-token"))
		Expect(mount.ReadOnly).To(BeTrue())

		defaultKn, defaultKnV2 := testKubernautWithFMC()
		defaultDep, err := FleetMetadataCacheDeployment(defaultKn, defaultKnV2)
		Expect(err).NotTo(HaveOccurred())
		Expect(findVolume(defaultDep, "fleet-acm-source-token")).To(BeNil(),
			"the token volume must not exist when no source is selected (default rendering unchanged)")
		Expect(findVolume(defaultDep, "fleet-acm-source-ca")).To(BeNil())
	})

	// Acceptance (c): the FMC ServiceAccount gains no Search RBAC through
	// this change. The ClusterRole's rules with sources.acm selected must
	// equal the rules without it.
	It("UT-FLEET-647-SRC-007 [AC-6]: leaves FMC's ClusterRole rules unchanged when the ACM source is selected", func() {
		withSrcKn, withSrcKnV2 := withACMSource(nil)
		withoutSrcKn, withoutSrcKnV2 := testKubernautWithFMC()

		var withRules, withoutRules []rbacv1.PolicyRule
		for _, cr := range ClusterRoles(withSrcKn, withSrcKnV2) {
			if cr.Name == clusterRoleName(withSrcKn, "fleetmetadatacache") {
				withRules = cr.Rules
			}
		}
		for _, cr := range ClusterRoles(withoutSrcKn, withoutSrcKnV2) {
			if cr.Name == clusterRoleName(withoutSrcKn, "fleetmetadatacache") {
				withoutRules = cr.Rules
			}
		}
		Expect(withRules).NotTo(BeNil())
		Expect(withoutRules).NotTo(BeNil())
		Expect(withRules).To(Equal(withoutRules),
			"selecting the ACM source must not grant the FMC ServiceAccount any Search RBAC (AC-6)")
		for _, rule := range withRules {
			Expect(rule.APIGroups).NotTo(ContainElement("search.open-cluster-management.io"))
		}
	})

	// Constraint: no token in logs, plaintext ConfigMap, status, or
	// annotations. The rendered config carries only the mount path —
	// never the Secret name or its contents.
	It("UT-FLEET-647-SRC-008 [SOC2 CC6.1; ASVS v5.0.0-V9.1.1]: renders the token mount path, never the Secret name or contents", func() {
		kn, knV2 := withACMSource(nil)
		cm, err := FleetMetadataCacheConfigMap(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		data := cm.Data["config.yaml"]
		Expect(data).To(ContainSubstring("tokenPath: " + fleetTokenMountPath))
		Expect(data).NotTo(ContainSubstring("acm-search-token"),
			"the ConfigMap must reference the token by mount path only, never by Secret name")
	})
})

// Compile-time sanity: FleetMetadataCacheSpec fields referenced by builders
// above must exist with the expected types.
var _ kubernautv1alpha2.FleetMetadataCacheSpec

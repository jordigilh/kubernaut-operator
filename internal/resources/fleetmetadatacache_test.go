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

	corev1 "k8s.io/api/core/v1"
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
			"server:", "apiAddr: :8080", "metricsAddr: :8081",
			"mcpGateway:", "endpoint: https://mcp-gateway.example.com/sse", "gatewayType: eaigw",
			"valkey:", "sync:", "keyTtl: 45s", "interval: 30s",
			"oauth2:", "tokenUrl: https://keycloak.example.com/token",
			"credentialsDir: /etc/fleetmetadatacache/fleet-oauth2",
		} {
			Expect(data).To(ContainSubstring(want), "FMC config should contain %q, got:\n%s", want, data)
		}
	})

	It("renders the mcpGateway namespace when set", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.FleetMetadataCache.Fleet = &kubernautv1alpha2.FleetOverrideSpec{Namespace: "managed-clusters"}
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
})

var _ = Describe("FleetMetadataCacheDeployment", func() {
	It("builds successfully with FMC enabled", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectDeploymentBasics(dep, "fleetmetadatacache")
	})

	It("exposes api (8080) and metrics (8081) ports", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		container := dep.Spec.Template.Spec.Containers[0]
		portMap := map[string]int32{}
		for _, p := range container.Ports {
			portMap[p.Name] = p.ContainerPort
		}
		Expect(portMap).To(HaveKeyWithValue("api", int32(8080)))
		Expect(portMap).To(HaveKeyWithValue("metrics", int32(8081)))
	})

	It("mounts config and fleet-oauth2 volumes", func() {
		kn, knV2 := testKubernautWithFMC()
		dep, err := FleetMetadataCacheDeployment(kn, knV2)
		Expect(err).NotTo(HaveOccurred())
		expectHasVolume(dep, "config")
		expectHasVolume(dep, "fleet-oauth2")
		expectHasVolumeMount(dep, "config", "/etc/fleetmetadatacache")
		expectHasVolumeMount(dep, "fleet-oauth2", fleetMetadataCacheOAuth2Dir)
		expectVolumeSourceConfigMap(dep, "config", "fleetmetadatacache-config")
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
	It("selects the fleetmetadatacache component and exposes api+metrics ports", func() {
		kn, _ := testKubernautWithFMC()
		svc := FleetMetadataCacheService(kn)
		Expect(svc.Name).To(Equal("fleetmetadatacache-service"))
		Expect(svc.Spec.Selector).To(Equal(SelectorLabels(ComponentFleetMetadataCache)))

		portMap := map[string]int32{}
		for _, p := range svc.Spec.Ports {
			portMap[p.Name] = p.Port
		}
		Expect(portMap).To(HaveKeyWithValue("api", int32(8080)))
		Expect(portMap).To(HaveKeyWithValue("metrics", int32(8081)))
	})

	It("is included in Services() when fleetMetadataCache.enabled is true", func() {
		kn, knV2 := testKubernautWithFMC()
		svcs := Services(kn, knV2, KagentiSidecarNone)
		names := make([]string, 0, len(svcs))
		for _, s := range svcs {
			names = append(names, s.Name)
		}
		Expect(names).To(ContainElement("fleetmetadatacache-service"))
	})

	It("is excluded from Services() when fleetMetadataCache.enabled is false", func() {
		kn := testKubernaut()
		svcs := Services(kn, testKnV2(kn), KagentiSidecarNone)
		names := make([]string, 0, len(svcs))
		for _, s := range svcs {
			names = append(names, s.Name)
		}
		Expect(names).NotTo(ContainElement("fleetmetadatacache-service"))
	})
})

var _ = Describe("FleetMetadataCache RBAC", func() {
	It("grants Envoy AI Gateway CRD watch access when mcpGatewayType=eaigw", func() {
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

	It("grants Kuadrant CRD watch access when mcpGatewayType=kuadrant", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.MCPGatewayType = mcpGatewayTypeKuadrant
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

var _ = Describe("fleetMetadataCacheNetworkPolicy", func() {
	It("allows ingress from gateway and remediationorchestrator on the api port", func() {
		kn, _ := testKubernautWithFMC()
		np := fleetMetadataCacheNetworkPolicy(kn)
		Expect(np.Spec.Ingress).NotTo(BeEmpty())

		var selectors []map[string]string
		for _, peer := range np.Spec.Ingress[0].From {
			if peer.PodSelector != nil {
				selectors = append(selectors, peer.PodSelector.MatchLabels)
			}
		}
		Expect(selectors).To(ContainElement(SelectorLabels(ComponentGateway)))
		Expect(selectors).To(ContainElement(SelectorLabels(ComponentRemediationOrchestrator)))
	})

	It("adds a metrics ingress rule", func() {
		kn, _ := testKubernautWithFMC()
		np := fleetMetadataCacheNetworkPolicy(kn)
		Expect(len(np.Spec.Ingress)).To(BeNumerically(">=", 2))
	})

	It("is included in NetworkPolicies() only when fleetMetadataCache.enabled is true", func() {
		kn, knV2 := testKubernautWithFMC()
		npEnabled := true
		kn.Spec.NetworkPolicies.Enabled = &npEnabled
		nps := NetworkPolicies(kn, knV2, KagentiSidecarNone)
		found := false
		for _, np := range nps {
			if np.Name == ComponentFleetMetadataCache+"-netpol" {
				found = true
			}
		}
		Expect(found).To(BeTrue())

		disabledKn := testKubernaut()
		disabledKn.Spec.NetworkPolicies.Enabled = &npEnabled
		for _, np := range NetworkPolicies(disabledKn, testKnV2(disabledKn), KagentiSidecarNone) {
			Expect(np.Name).NotTo(Equal(ComponentFleetMetadataCache + "-netpol"))
		}
	})
})

var _ = Describe("FleetMetadataCacheURL", func() {
	It("returns a plain-HTTP in-cluster URL on port 8080", func() {
		Expect(FleetMetadataCacheURL("kubernaut-system")).To(Equal("http://fleetmetadatacache-service.kubernaut-system.svc.cluster.local:8080"))
	})
})

var _ = Describe("FleetMetadataCacheEnabled helper", func() {
	It("defaults to false", func() {
		kn := testKubernaut()
		Expect(testKnV2(kn).Spec.FleetMetadataCacheEnabled()).To(BeFalse())
	})

	It("returns true when explicitly enabled", func() {
		_, knV2 := testKubernautWithFMC()
		Expect(knV2.Spec.FleetMetadataCacheEnabled()).To(BeTrue())
	})
})

var _ = Describe("isComponentActive for FleetMetadataCache", func() {
	It("is inactive by default", func() {
		kn := testKubernaut()
		Expect(ActiveComponents(kn, testKnV2(kn))).NotTo(ContainElement(ComponentFleetMetadataCache))
	})

	It("is active when spec.fleetMetadataCache.enabled is true", func() {
		kn, knV2 := testKubernautWithFMC()
		Expect(ActiveComponents(kn, knV2)).To(ContainElement(ComponentFleetMetadataCache))
	})
})

// resolveFleetEndpoint (used by configmaps.go for Gateway/RO's rendered
// fleet.endpoint) is exercised here too since it is the mechanism tying
// FMC's own service URL to spec.fleet.endpoint auto-derivation.
var _ = Describe("resolveFleetEndpoint", func() {
	It("auto-derives the in-cluster FMC URL when backend=fleetmetadatacache and FMC is operator-managed", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.Endpoint = ""
		Expect(resolveFleetEndpoint(knV2)).To(Equal(FleetMetadataCacheURL(kn.Namespace)))
	})

	It("leaves an explicit endpoint untouched", func() {
		_, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.Endpoint = "https://byo-fmc.example.com"
		Expect(resolveFleetEndpoint(knV2)).To(Equal("https://byo-fmc.example.com"))
	})

	It("does not auto-derive when FMC is not operator-managed (BYO)", func() {
		_, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.Endpoint = ""
		knV2.Spec.FleetMetadataCache.Enabled = nil
		Expect(resolveFleetEndpoint(knV2)).To(Equal(""))
	})

	It("does not auto-derive when backend=acm even if FMC is enabled", func() {
		_, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.Backend = "acm"
		knV2.Spec.Fleet.Endpoint = ""
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
})

// Compile-time sanity: FleetMetadataCacheSpec fields referenced by builders
// above must exist with the expected types.
var _ kubernautv1alpha2.FleetMetadataCacheSpec

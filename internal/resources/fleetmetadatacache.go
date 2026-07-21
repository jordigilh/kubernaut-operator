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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// FMC (Fleet Metadata Cache, ADR-068, #200) ports and paths. These mirror
// upstream's cmd/fleetmetadatacache/config.DefaultConfigPath and Helm chart
// (charts/kubernaut/templates/fleetmetadatacache/fleetmetadatacache.yaml)
// exactly -- FMC's LoadFromFile unmarshals directly into
// pkg/fleet/fmc/config.ServiceConfig, so field names/nesting here must match.
const (
	fleetMetadataCacheAPIPort     int32 = 8080
	fleetMetadataCacheMetricsPort int32 = 8081

	fleetMetadataCacheConfigMapName = "fleetmetadatacache-config"
	fleetMetadataCacheServiceName   = "fleetmetadatacache-service"

	// fleetMetadataCacheOAuth2Dir must stay in sync with
	// fleetMetadataCacheOAuth2YAML.CredentialsDir below and the volume mount
	// path added in FleetMetadataCacheDeployment.
	fleetMetadataCacheOAuth2Dir = "/etc/fleetmetadatacache/fleet-oauth2"

	// fleetMetadataCacheMCPGatewayCommonPort is the conventional MCP
	// Gateway listener port used in upstream's own example
	// (fleetmetadatacache.mcpGatewayEndpoint: "http://envoy-ai-gateway...:8080/mcp")
	// and NetworkPolicy egress. spec.fleet.mcpGatewayEndpoint is a free-form
	// URL, so this is a best-effort allow, not a parsed value.
	fleetMetadataCacheMCPGatewayCommonPort int32 = 8080
)

// --- ConfigMap ---

type fleetMetadataCacheServerYAML struct {
	APIAddr     string `json:"apiAddr" yaml:"apiAddr"`
	MetricsAddr string `json:"metricsAddr" yaml:"metricsAddr"`
}

type fleetMetadataCacheMCPGatewayYAML struct {
	Endpoint    string `json:"endpoint" yaml:"endpoint"`
	GatewayType string `json:"gatewayType" yaml:"gatewayType"`
	Namespace   string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

type fleetMetadataCacheValkeyYAML struct {
	Addr string `json:"addr" yaml:"addr"`
}

type fleetMetadataCacheSyncYAML struct {
	Interval string `json:"interval" yaml:"interval"`
	KeyTTL   string `json:"keyTtl" yaml:"keyTtl"`
}

type fleetMetadataCacheOAuth2YAML struct {
	TokenURL       string `json:"tokenUrl" yaml:"tokenUrl"`
	CredentialsDir string `json:"credentialsDir" yaml:"credentialsDir"`
}

// fleetMetadataCacheConfigYAML mirrors upstream's
// pkg/fleet/fmc/config.ServiceConfig field names and nesting exactly.
type fleetMetadataCacheConfigYAML struct {
	Server     fleetMetadataCacheServerYAML     `json:"server" yaml:"server"`
	MCPGateway fleetMetadataCacheMCPGatewayYAML `json:"mcpGateway" yaml:"mcpGateway"`
	Valkey     fleetMetadataCacheValkeyYAML     `json:"valkey" yaml:"valkey"`
	Sync       fleetMetadataCacheSyncYAML       `json:"sync" yaml:"sync"`
	OAuth2     fleetMetadataCacheOAuth2YAML     `json:"oauth2" yaml:"oauth2"`
}

// FleetMetadataCacheConfigMap builds the fleetmetadatacache-config ConfigMap.
// Only called when spec.fleetMetadataCache.enabled is true (validated by
// validateFleetMetadataCache at admission), so
// spec.fleet.mcpGatewayEndpoint/mcpGatewayType and spec.fleet.oauth2.tokenURL
// are guaranteed non-empty.
func FleetMetadataCacheConfigMap(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
	fleet := &kn.Spec.Fleet
	fmc := &kn.Spec.FleetMetadataCache

	cfg := fleetMetadataCacheConfigYAML{
		Server: fleetMetadataCacheServerYAML{
			APIAddr:     fmt.Sprintf(":%d", fleetMetadataCacheAPIPort),
			MetricsAddr: fmt.Sprintf(":%d", fleetMetadataCacheMetricsPort),
		},
		MCPGateway: fleetMetadataCacheMCPGatewayYAML{
			Endpoint:    fleet.MCPGatewayEndpoint,
			GatewayType: fleet.MCPGatewayType,
			Namespace:   fmc.MCPGatewayNamespace,
		},
		Valkey: fleetMetadataCacheValkeyYAML{
			Addr: ValkeyAddr(&kn.Spec.Valkey),
		},
		Sync: fleetMetadataCacheSyncYAML{
			Interval: withDefault(fmc.SyncInterval, "30s"),
			KeyTTL:   withDefault(fmc.KeyTTL, "45s"),
		},
		OAuth2: fleetMetadataCacheOAuth2YAML{
			TokenURL:       fleet.OAuth2.TokenURL,
			CredentialsDir: fleetMetadataCacheOAuth2Dir,
		},
	}

	data, err := marshalYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("fleetmetadatacache config: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, fleetMetadataCacheConfigMapName, ComponentFleetMetadataCache),
		Data:       map[string]string{"config.yaml": data},
	}, nil
}

// --- Deployment ---

// fleetMetadataCacheEffectiveOAuth2SecretRef resolves the Secret FMC mounts
// for its OAuth2 client credentials: its own override, falling back to the
// shared spec.fleet.oauth2.credentialsSecretRef. Guaranteed non-empty when
// FMC is enabled (validateFleetMetadataCache).
func fleetMetadataCacheEffectiveOAuth2SecretRef(kn *kubernautv1alpha1.Kubernaut) string {
	return withDefault(kn.Spec.FleetMetadataCache.FleetOAuth2CredentialsSecretRef, kn.Spec.Fleet.OAuth2.CredentialsSecretRef)
}

// FleetMetadataCacheDeployment builds the FMC Deployment. FMC's OAuth2
// credentials mount is independent of spec.fleet.enabled/appendFleetSecretMounts
// (which gate Gateway/RemediationOrchestrator's own scope-check consumption)
// -- FMC always requires it, enforced by validateFleetMetadataCache.
func FleetMetadataCacheDeployment(kn *kubernautv1alpha1.Kubernaut) (*appsv1.Deployment, error) {
	credRef := fleetMetadataCacheEffectiveOAuth2SecretRef(kn)

	volumes := []corev1.Volume{
		configMapVolume("config", fleetMetadataCacheConfigMapName),
		secretVolume("fleet-oauth2", credRef),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/fleetmetadatacache", ReadOnly: true},
		{Name: "fleet-oauth2", MountPath: fleetMetadataCacheOAuth2Dir, ReadOnly: true},
	}

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentFleetMetadataCache, ImageName: "fleetmetadatacache",
		Resources: kn.Spec.FleetMetadataCache.Resources, VolumeMounts: mounts, Volumes: volumes,
		Args: []string{"-config=/etc/fleetmetadatacache/config.yaml"},
		Ports: []corev1.ContainerPort{
			{Name: "api", ContainerPort: fleetMetadataCacheAPIPort, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: fleetMetadataCacheMetricsPort, Protocol: corev1.ProtocolTCP},
		},
		ProbePort: fleetMetadataCacheAPIPort,
	})
}

// --- Service ---

// FleetMetadataCacheService builds the Service fronting FMC's api and
// metrics ports. Plain ClusterIP, no TLS -- see FleetMetadataCacheURL for
// why (upstream's binary has no TLS server support).
func FleetMetadataCacheService(kn *kubernautv1alpha1.Kubernaut) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: ObjectMeta(kn, fleetMetadataCacheServiceName, ComponentFleetMetadataCache),
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(ComponentFleetMetadataCache),
			Ports: []corev1.ServicePort{
				ServicePort("api", fleetMetadataCacheAPIPort),
				ServicePort("metrics", fleetMetadataCacheMetricsPort),
			},
		},
	}
}

// --- RBAC ---

// fleetMetadataCacheClusterRole grants watch access to the MCP Gateway CRDs
// (Backend/MCPRoute for Envoy AI Gateway, MCPServerRegistration/Gateway/
// HTTPRoute for Kuadrant) that represent managed clusters, matching
// upstream's own Helm chart rules exactly (gatewayType-conditional).
// Cluster-scoped regardless of spec.fleetMetadataCache.mcpGatewayNamespace:
// upstream's chart grants this ClusterRole unconditionally today even when
// a watch namespace is configured -- a namespace-scoped Role is not yet an
// upstream option (tracked in kubernaut#1686). Mirroring that here (rather
// than inventing an operator-only namespace-scoped Role ahead of upstream
// capability) avoids the same unconsumed-CRD-surface mistake as the
// mcpGatewayNamespace field removed from spec.fleet in #223.
func fleetMetadataCacheClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	var rules []rbacv1.PolicyRule
	if kn.Spec.Fleet.MCPGatewayType == "kuadrant" {
		rules = []rbacv1.PolicyRule{
			{APIGroups: []string{"mcp.kuadrant.io"}, Resources: []string{"mcpserverregistrations"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"gateway.networking.k8s.io"}, Resources: []string{"gateways", "httproutes"}, Verbs: []string{"get", "list", "watch"}},
		}
	} else {
		rules = []rbacv1.PolicyRule{
			{APIGroups: []string{"gateway.envoyproxy.io"}, Resources: []string{"backends"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"aigateway.envoyproxy.io"}, Resources: []string{"mcproutes"}, Verbs: []string{"get", "list", "watch"}},
		}
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   clusterRoleName(kn, "fleetmetadatacache"),
			Labels: labels,
		},
		Rules: rules,
	}
}

// FleetMetadataCacheClusterRoleBinding binds FMC's SA to its ClusterRole.
func fleetMetadataCacheClusterRoleBinding(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRoleBinding {
	return clusterRoleBinding(
		clusterRoleName(kn, "fleetmetadatacache-binding"),
		clusterRoleName(kn, "fleetmetadatacache"),
		ServiceAccountName(ComponentFleetMetadataCache), kn.Namespace, labels,
	)
}

// --- NetworkPolicy ---

// fleetMetadataCacheNetworkPolicy allows Gateway/RemediationOrchestrator to
// reach FMC's api port, monitoring to scrape metrics, and FMC itself to
// reach Valkey plus the MCP Gateway/OAuth2 token endpoint egress.
func fleetMetadataCacheNetworkPolicy(kn *kubernautv1alpha1.Kubernaut) *networkingv1.NetworkPolicy {
	protoTCP := corev1.ProtocolTCP
	pAPI := intstr.FromInt32(fleetMetadataCacheAPIPort)

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From: []networkingv1.NetworkPolicyPeer{
				{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentGateway)}},
				{PodSelector: &metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentRemediationOrchestrator)}},
			},
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protoTCP, Port: &pAPI}},
		},
	}
	if kn.Spec.Monitoring.MonitoringEnabled() {
		ingress = append(ingress, *metricsIngressRule(OCPMonitoringNamespace))
	}

	valkeyPort := kn.Spec.Valkey.Port
	if valkeyPort == 0 {
		valkeyPort = DefaultValkeyPort
	}
	pValkey := intstr.FromInt32(valkeyPort)
	p443 := intstr.FromInt32(443)
	pMCPGateway := intstr.FromInt32(fleetMetadataCacheMCPGatewayCommonPort)

	egress := baseEgress(2)
	egress = append(egress,
		networkingv1.NetworkPolicyEgressRule{
			To:    sameNamespacePeers(),
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protoTCP, Port: &pValkey}},
		},
		networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{}}},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &p443},
				{Protocol: &protoTCP, Port: &pMCPGateway},
			},
		},
	)

	return &networkingv1.NetworkPolicy{
		ObjectMeta: ObjectMeta(kn, ComponentFleetMetadataCache+"-netpol", ComponentFleetMetadataCache),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(ComponentFleetMetadataCache)},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

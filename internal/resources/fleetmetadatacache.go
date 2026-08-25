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
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// FMC (Fleet Metadata Cache, ADR-068, #200) ports and paths. These mirror
// upstream's cmd/fleetmetadatacache/config.DefaultConfigPath and Helm chart
// (charts/kubernaut/templates/fleetmetadatacache/fleetmetadatacache.yaml)
// exactly -- FMC's LoadFromFile unmarshals directly into
// pkg/fleet/fmc/config.ServiceConfig, so field names/nesting here must match.
const (
	fleetMetadataCacheAPIPort int32 = 8080

	fleetMetadataCacheConfigMapName = "fleetmetadatacache-config"
	fleetMetadataCacheServiceName   = "fleetmetadatacache-service"

	// fleetMetadataCacheOAuth2Dir must stay in sync with
	// fleetMetadataCacheOAuth2YAML.CredentialsDir below and the volume mount
	// path added in FleetMetadataCacheDeployment.
	fleetMetadataCacheOAuth2Dir = "/etc/fleetmetadatacache/fleet-oauth2"
)

// --- ConfigMap ---

type fleetMetadataCacheServerYAML struct {
	APIAddr     string `json:"apiAddr" yaml:"apiAddr"`
	HealthAddr  string `json:"healthAddr" yaml:"healthAddr"`
	MetricsAddr string `json:"metricsAddr" yaml:"metricsAddr"`
}

type fleetMetadataCacheMCPGatewayYAML struct {
	Endpoint    string               `json:"endpoint" yaml:"endpoint"`
	GatewayType string               `json:"gatewayType" yaml:"gatewayType"`
	Namespace   string               `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Resilience  *fleetResilienceYAML `json:"resilience,omitempty" yaml:"resilience,omitempty"`
}

type fleetMetadataCacheValkeyYAML struct {
	Addr string                           `json:"addr" yaml:"addr"`
	TLS  *fleetMetadataCacheValkeyTLSYAML `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// fleetMetadataCacheValkeyTLSYAML mirrors upstream's
// pkg/fleet/fmc/config.ValkeyTLSConfig field-for-field (issue #398). Per
// upstream DD-PLATFORM-006 Decision Area 8, the chart's own Valkey is
// TLS-only using one-way TLS -- the server presents a cert the client
// verifies via CAFile, and no client certificate is required ("a client
// presenting no certificate still succeeds"). CertFile/KeyFile are
// therefore optional mTLS, kept only for cross-service consistency with
// DataStorage's identical dataStorageRedisTLSYAML rendering.
type fleetMetadataCacheValkeyTLSYAML struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	CAFile   string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	CertFile string `json:"certFile,omitempty" yaml:"certFile,omitempty"`
	KeyFile  string `json:"keyFile,omitempty" yaml:"keyFile,omitempty"`
}

type fleetMetadataCacheSyncYAML struct {
	Interval string `json:"interval" yaml:"interval"`
	KeyTTL   string `json:"keyTtl" yaml:"keyTtl"`
}

type fleetMetadataCacheOAuth2YAML struct {
	TokenURL       string   `json:"tokenUrl" yaml:"tokenUrl"`
	CredentialsDir string   `json:"credentialsDir" yaml:"credentialsDir"`
	Scopes         []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	TLSCaFile      string   `json:"tlsCaFile,omitempty" yaml:"tlsCaFile,omitempty"`
}

// fleetMetadataCacheConfigYAML mirrors upstream's
// pkg/fleet/fmc/config.ServiceConfig field names and nesting exactly.
type fleetMetadataCacheConfigYAML struct {
	Server     fleetMetadataCacheServerYAML     `json:"server" yaml:"server"`
	MCPGateway fleetMetadataCacheMCPGatewayYAML `json:"mcpGateway" yaml:"mcpGateway"`
	Valkey     fleetMetadataCacheValkeyYAML     `json:"valkey" yaml:"valkey"`
	Sync       fleetMetadataCacheSyncYAML       `json:"sync" yaml:"sync"`
	OAuth2     fleetMetadataCacheOAuth2YAML     `json:"oauth2" yaml:"oauth2"`
	Debug      debugYAML                        `json:"debug" yaml:"debug"`
}

// FleetMetadataCacheConfigMap builds the fleetmetadatacache-config ConfigMap.
// Only called when spec.fleetMetadataCache.enabled is true (validated by
// ValidateFleet at admission), so
// spec.fleet.mcpGatewayEndpoint/mcpGatewayType and spec.fleet.oauth2.tokenURL
// are guaranteed non-empty. Fleet's entire CRD surface lives in v1alpha2
// (Fleet v1alpha2 migration); kn is still needed for object metadata/labels
// and non-Fleet fields (Valkey).
func FleetMetadataCacheConfigMap(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*corev1.ConfigMap, error) {
	fleet := &knV2.Spec.Fleet
	fmc := &knV2.Spec.FleetMetadataCache

	cfg := fleetMetadataCacheConfigYAML{
		Server: fleetMetadataCacheServerYAML{
			APIAddr:     fmt.Sprintf(":%d", fleetMetadataCacheAPIPort),
			HealthAddr:  fmt.Sprintf(":%d", PortHealthProbe),
			MetricsAddr: fmt.Sprintf(":%d", PortMetrics),
		},
		MCPGateway: fleetMetadataCacheMCPGatewayYAML{
			Endpoint:    fleet.MCPGatewayEndpoint,
			GatewayType: fleet.MCPGatewayType,
			Namespace:   fleet.MCPGatewayNamespace,
			Resilience:  resolveFleetResilience(knV2),
		},
		Valkey: fleetMetadataCacheValkeyYAML{
			Addr: ValkeyAddr(&kn.Spec.Valkey),
			TLS:  resolveFleetMetadataCacheValkeyTLS(kn),
		},
		Sync: fleetMetadataCacheSyncYAML{
			Interval: withDefault(fmc.SyncInterval, "30s"),
			KeyTTL:   withDefault(fmc.KeyTTL, "45s"),
		},
		OAuth2: fleetMetadataCacheOAuth2YAML{
			TokenURL:       fleet.OAuth2.TokenURL,
			CredentialsDir: fleetMetadataCacheOAuth2Dir,
			Scopes:         fleet.OAuth2.Scopes,
			TLSCaFile:      InterServiceTLSCAFile,
		},
		Debug: debugYAML{PprofEnabled: fmc.Debug.PprofEnabled},
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

// resolveFleetMetadataCacheValkeyTLS mirrors DataStorageConfigMap's identical
// spec.valkey.tls resolution (issue #398): omitted entirely when TLS is
// disabled, CAFile only when an explicit CA secret is configured, CertFile/
// KeyFile only when an explicit client-cert secret is configured. Paths must
// stay in sync with the volume mounts FleetMetadataCacheDeployment adds.
func resolveFleetMetadataCacheValkeyTLS(kn *kubernautv1alpha1.Kubernaut) *fleetMetadataCacheValkeyTLSYAML {
	if !kn.Spec.Valkey.ValkeyTLSEnabled() {
		return nil
	}
	t := &fleetMetadataCacheValkeyTLSYAML{Enabled: true}
	if kn.Spec.Valkey.TLS.CASecretName != "" {
		t.CAFile = "/etc/valkey-tls/ca/ca.crt"
	}
	if kn.Spec.Valkey.TLS.ClientCertSecretName != "" {
		t.CertFile = "/etc/valkey-tls/client/tls.crt"
		t.KeyFile = "/etc/valkey-tls/client/tls.key"
	}
	return t
}

// --- Deployment ---

// fleetMetadataCacheEffectiveOAuth2SecretRef resolves the Secret FMC mounts
// for its OAuth2 client credentials: its own override (F1 --
// FleetMetadataCacheSpec.Fleet in v1alpha2), falling back to the shared
// spec.fleet.oauth2.credentialsSecretRef. Guaranteed non-empty when FMC is
// enabled (ValidateFleet).
func fleetMetadataCacheEffectiveOAuth2SecretRef(knV2 *kubernautv1alpha2.Kubernaut) string {
	return effectiveFleetOAuth2SecretRef(knV2.Spec.FleetMetadataCache.Fleet, knV2.Spec.Fleet.OAuth2.CredentialsSecretRef)
}

// FleetMetadataCacheDeployment builds the FMC Deployment. FMC's OAuth2
// credentials mount is independent of spec.fleet.enabled/appendFleetSecretMounts
// (which gate Gateway/RemediationOrchestrator's own scope-check consumption)
// -- FMC always requires it, enforced by ValidateFleet.
func FleetMetadataCacheDeployment(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) (*appsv1.Deployment, error) {
	credRef := fleetMetadataCacheEffectiveOAuth2SecretRef(knV2)

	volumes := []corev1.Volume{
		configMapVolume("config", fleetMetadataCacheConfigMapName),
		secretVolume("fleet-oauth2", credRef),
		optionalConfigMapVolume("tls-ca", TrustBundleConfigMapName),
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/fleetmetadatacache", ReadOnly: true},
		{Name: "fleet-oauth2", MountPath: fleetMetadataCacheOAuth2Dir, ReadOnly: true},
		{Name: "tls-ca", MountPath: "/etc/tls-ca", ReadOnly: true},
	}
	// #398: mirrors DataStorageDeployment's identical spec.valkey.tls volume
	// wiring -- without this, FMC's rendered valkey.tls.caFile/certFile/
	// keyFile paths (resolveFleetMetadataCacheValkeyTLS) point at files that
	// are never actually mounted into the container.
	if kn.Spec.Valkey.ValkeyTLSEnabled() {
		if kn.Spec.Valkey.TLS.CASecretName != "" {
			volumes = append(volumes, secretVolume("valkey-ca", kn.Spec.Valkey.TLS.CASecretName))
			mounts = append(mounts, corev1.VolumeMount{Name: "valkey-ca", MountPath: "/etc/valkey-tls/ca", ReadOnly: true})
		}
		if kn.Spec.Valkey.TLS.ClientCertSecretName != "" {
			volumes = append(volumes, secretVolume("valkey-client-cert", kn.Spec.Valkey.TLS.ClientCertSecretName))
			mounts = append(mounts, corev1.VolumeMount{Name: "valkey-client-cert", MountPath: "/etc/valkey-tls/client", ReadOnly: true})
		}
	}
	// SSL_CERT_FILE: FMC's actual MCP Gateway session transport
	// (pkg/fleet/mcpclient.WithReloadableOAuth2Transport) falls back to an
	// unmodified http.DefaultTransport for the real MCP protocol calls --
	// OAuth2.TlsCaFile (config.yaml) only covers the separate OAuth2 token
	// fetch. Setting SSL_CERT_FILE extends the process's Go system cert
	// pool (additive: the container's own base-image trust directories are
	// still scanned) to also trust this bundle, closing that gap without
	// an upstream kubernaut-core code change. Tracked upstream for a proper
	// fix (WithHTTPClient wiring in cmd/fleetmetadatacache/main.go).
	env := []corev1.EnvVar{{Name: "SSL_CERT_FILE", Value: InterServiceTLSCAFile}}

	return buildDeployment(kn, DeploymentParams{
		Component: ComponentFleetMetadataCache, ImageName: "fleetmetadatacache",
		Resources: knV2.Spec.FleetMetadataCache.Resources, VolumeMounts: mounts, Volumes: volumes, Env: env,
		Args: []string{"-config=/etc/fleetmetadatacache/config.yaml"},
		Ports: []corev1.ContainerPort{
			{Name: "api", ContainerPort: fleetMetadataCacheAPIPort, Protocol: corev1.ProtocolTCP},
			{Name: "health", ContainerPort: PortHealthProbe, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: PortMetrics, Protocol: corev1.ProtocolTCP},
		},
		// FMC's own healthAddr binds /healthz and /readyz on a dedicated
		// health port (8081), not the api port (8080, request traffic
		// only) -- confirmed against live startup logs
		// ("healthAddr":":8081"). Probing 8080 left the container
		// permanently stuck at 0/1 Ready (startup probe: connection
		// refused) even when healthy. #396: metricsAddr is a genuinely
		// separate port (9090, PortMetrics) from healthAddr -- FMC used to
		// crash on "bind: address already in use" because this ConfigMap
		// rendered metricsAddr as the same port FMC's own default
		// (undeclared here before #396) already used for healthAddr.
		ProbePort: PortHealthProbe,
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
				ServicePort("health", PortHealthProbe),
				ServicePort("metrics", PortMetrics),
			},
		},
	}
}

// --- RBAC ---

// fleetMetadataCacheClusterRole grants watch access to the MCP Gateway CRDs
// (Backend/MCPRoute for Envoy AI Gateway, MCPServerRegistration/Gateway/
// HTTPRoute for Kuadrant) that represent managed clusters, matching
// upstream's own Helm chart rules exactly (gatewayType-conditional).
//
// #224: cluster-scoped only when the shared spec.fleet.mcpGatewayNamespace
// (DD-362 -- no per-component override) is empty. When a namespace
// resolves, these rules move to a namespace-scoped Role instead (see
// MCPGatewayNamespaceRBAC) -- unlike upstream's own Helm chart, which still
// grants this ClusterRole unconditionally (tracked in kubernaut#1686), the
// operator can do better because FMC's own binary already supports
// Namespace-scoped watches (cmd/fleetmetadatacache/main.go passes
// cfg.MCPGateway.Namespace straight into
// registry.RegistryConfig{Namespace: ...}).
func fleetMetadataCacheClusterRole(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	var rules []rbacv1.PolicyRule
	if knV2.Spec.Fleet.MCPGatewayNamespace == "" {
		rules = mcpGatewayCRDPolicyRules(knV2.Spec.Fleet.MCPGatewayType)
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
func fleetMetadataCacheNetworkPolicy(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) *networkingv1.NetworkPolicy {
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
		*metricsIngressRule(OCPMonitoringNamespace),
	}

	valkeyPort := kn.Spec.Valkey.Port
	if valkeyPort == 0 {
		valkeyPort = DefaultValkeyPort
	}
	pValkey := intstr.FromInt32(valkeyPort)

	egress := baseEgress(2)
	egress = append(egress,
		networkingv1.NetworkPolicyEgressRule{
			To:    sameNamespacePeers(),
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protoTCP, Port: &pValkey}},
		},
		fleetDestinationsEgressRule(knV2),
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

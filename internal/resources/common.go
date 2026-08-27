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
	"os"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// Service names match the Helm chart's naming conventions.
const (
	ComponentGateway                 = "gateway"
	ComponentDataStorage             = "data-storage"
	ComponentAIAnalysis              = "aianalysis"
	ComponentSignalProcessing        = "signalprocessing"
	ComponentRemediationOrchestrator = "remediationorchestrator"
	ComponentWorkflowExecution       = "workflowexecution"
	ComponentEffectivenessMonitor    = "effectivenessmonitor"
	ComponentNotification            = "notification"
	ComponentKubernautAgent          = "kubernaut-agent"
	ComponentAuthWebhook             = "authwebhook"
	ComponentAPIFrontend             = "apifrontend"
	ComponentConsole                 = "kubernaut-console"
	ComponentFleetMetadataCache      = "fleetmetadatacache"
)

// controllerSuffix lists components that are actual Kubernetes controllers
// (reconciliation loops) and use the "-controller" deployment name suffix,
// matching the Helm chart convention. REST API services and webhook servers
// use their bare component name.
var controllerSuffix = map[string]bool{
	ComponentAIAnalysis:              true,
	ComponentSignalProcessing:        true,
	ComponentRemediationOrchestrator: true,
	ComponentWorkflowExecution:       true,
	ComponentEffectivenessMonitor:    true,
	ComponentNotification:            true,
}

// DeploymentName returns the Deployment resource name for a component,
// aligned with the Helm chart naming convention.
func DeploymentName(component string) string {
	if controllerSuffix[component] {
		return component + "-controller"
	}
	return component
}

// Well-known ports used across services.
const (
	PortHTTPS   int32 = 8443
	PortMetrics int32 = 9090
	// PortAuthWebhookService is the standard HTTPS port (443) exposed by the
	// auth-webhook Kubernetes Service, distinct from PortHTTPS (8443) used by
	// application containers.
	PortAuthWebhookService int32 = 443
	PortWebhookServer      int32 = 9443
	PortHealthProbe        int32 = 8081
	// PortPprof is controller-runtime's ctrl.Options.PprofBindAddress
	// default (":6060") for the 7 controller-runtime-managed services
	// (AIAnalysis, AuthWebhook, EffectivenessMonitor, Notification,
	// RemediationOrchestrator, SignalProcessing, WorkflowExecution) --
	// see upstream internal/config.PprofBindAddress (BR-PLATFORM-012,
	// kubernaut#2275/#2277).
	PortPprof int32 = 6060
)

// pprofContainerPort returns a single-element []corev1.ContainerPort
// exposing PortPprof when enabled, or nil otherwise, for appending onto a
// ctrl-runtime-managed service's Ports slice (BR-PLATFORM-012, #403).
// Secure-by-default (AC-6): omits the port entirely rather than merely
// documenting it as unused when profiling is off.
func pprofContainerPort(enabled bool) []corev1.ContainerPort {
	if !enabled {
		return nil
	}
	return []corev1.ContainerPort{{Name: "pprof", ContainerPort: PortPprof, Protocol: corev1.ProtocolTCP}}
}

// Default PostgreSQL port when not specified in the CR.
const DefaultPostgreSQLPort int32 = 5432

// Default Valkey port when not specified in the CR.
const DefaultValkeyPort int32 = 6379

// Migration Job tuning constants.
// MigrationBackoffLimit controls the Kubernetes Job's pod-level retry count
// (spec.backoffLimit). This is distinct from the operator's reconciliation
// loop, which will re-check the Job's status on each requeue until it
// succeeds or reaches the backoff limit.
const (
	MigrationBackoffLimit int32 = 3
	MigrationTTLSeconds   int32 = 300
)

// KagentiSidecarMode describes how the kagenti webhook injects its
// authentication sidecar into AF pods. The operator detects the mode at
// runtime and adjusts AF listen/health/metrics ports accordingly.
type KagentiSidecarMode int

const (
	// KagentiSidecarNone means kagenti is not active (SPIRE disabled or
	// kagenti not installed).
	KagentiSidecarNone KagentiSidecarMode = iota

	// KagentiSidecarEnvoy is kagenti 0.2.x: an envoy-proxy sidecar that
	// intercepts traffic via iptables + ORIGINAL_DST routing. The
	// application container keeps its original listen port because envoy
	// transparently proxies to it.
	KagentiSidecarEnvoy

	// KagentiSidecarAuthbridge is kagenti 0.3.x+: an authbridge-proxy
	// binary that takes the declared containerPort and shifts the
	// application container to port+1 via the PORT env var. The operator
	// must NOT pre-shift; it keeps AF on PortHTTPS so that authbridge
	// occupies 8443 and AF moves to 8444.
	KagentiSidecarAuthbridge
)

// AFListenPort returns the port the operator writes into the AF container
// spec and config.yaml. The kagenti webhook handles the actual port
// shifting at admission time — authbridge takes this port and moves the
// application to port+1.
func (m KagentiSidecarMode) AFListenPort() int32 {
	return PortHTTPS
}

// ShiftsPorts reports whether AF metrics and health ports must be shifted
// away from defaults to avoid conflicts with the kagenti sidecar.
func (m KagentiSidecarMode) ShiftsPorts() bool {
	return m != KagentiSidecarNone
}

// PDB constant.
const PDBMaxUnavailable = 1

// LLMProviderVertexAI identifies the Vertex AI LLM provider.
const LLMProviderVertexAI = "vertex_ai"

// llmCredentialsFileAPIKey and llmCredentialsFileVertexJSON are the two
// filenames a resolved LLM profile's apiKeyFile may point to within its
// credentials mount: a flat API key for every provider except vertex_ai,
// whose client instead expects GCP service-account JSON bytes (#233, #279).
const (
	llmCredentialsFileAPIKey     = "api_key"
	llmCredentialsFileVertexJSON = "credentials.json"
)

// LLMProviderOpenAI is the canonical provider name users set in the CR.
// KA consumes this as-is; the operator translates it for AF.
const LLMProviderOpenAI = "openai"

// LLMProviderOpenAICompatible is what AF expects for OpenAI-compatible
// endpoints (kubernaut#1487). The operator emits this to AF when the CR
// specifies "openai". This translation will be removed when upstream
// normalizes the config (kubernaut#1488).
const LLMProviderOpenAICompatible = "openai_compatible"

// LLMProviderAnthropic identifies the native Anthropic LLM provider.
const LLMProviderAnthropic = "anthropic"

// anthropicFamilyReasoningProviders are the providers routed to the
// Anthropic thinking API (native and Vertex-hosted Claude), mirroring
// upstream's pkg/shared/types.anthropicFamilyProviders. Anthropic has no
// "thinking enabled, zero effort" wire state, so effort: "none" combined
// with enabled: true is a genuine contradiction for these providers only.
var anthropicFamilyReasoningProviders = map[string]bool{
	LLMProviderAnthropic: true,
	LLMProviderVertexAI:  true,
}

// Kagenti discovery labels for A2A agent auto-discovery.
const (
	KagentiAgentTypeLabel                = "kagenti.io/type"
	KagentiA2AProtocolLabel              = "protocol.kagenti.io/a2a"
	KagentiClientRegistrationInjectLabel = "kagenti.io/client-registration-inject"
)

// AgentTLSPortName is the service port name that signals to the kagenti-operator
// that the agent card endpoint requires TLS.
const AgentTLSPortName = "agent-tls"

// OCP service-CA injection annotation.
const OCPServiceCAInjectAnnotation = "service.beta.openshift.io/inject-cabundle"

// OCPServingCertAnnotation is the OCP annotation that triggers automatic
// TLS certificate generation for a Service.
const OCPServingCertAnnotation = "service.beta.openshift.io/serving-cert-secret-name"

// DefaultWorkflowNamespace is the namespace used for workflow execution
// when not overridden in the CR spec.
const DefaultWorkflowNamespace = "kubernaut-workflows"

// InterServiceCAConfigMapName is the ConfigMap that holds the OCP service-ca
// trust bundle for inter-service TLS verification.
const InterServiceCAConfigMapName = "inter-service-ca"

// InterServiceTLSCertDir is the mount path for server-side TLS certificates
// provisioned by the OCP service-ca operator.
const InterServiceTLSCertDir = "/etc/tls"

// InterServiceTLSCAFile is the mount path for the CA certificate used by
// clients to verify inter-service TLS connections. OCP service-ca injects
// the bundle under the key "service-ca.crt".
const InterServiceTLSCAFile = "/etc/tls-ca/service-ca.crt"

// TrustBundleConfigMapName is the operator-managed ConfigMap that merges the
// OCP service-ca bundle (from InterServiceCAConfigMapName) with the
// cluster's default ingress/router CA (read from
// openshift-config-managed/default-ingress-cert) under the same
// "service-ca.crt" key used by InterServiceCAConfigMapName today. Every
// client-side mount of InterServiceCAConfigMapName is repointed at this
// ConfigMap instead, so a single trust file verifies both internal Service
// TLS (signed by service-ca) and Route-based TLS -- MCP Gateway, Keycloak
// OIDC (signed by the cluster's IngressController) -- without any CR change.
const TrustBundleConfigMapName = "inter-service-trust-bundle"

// DefaultSSLMode is the PostgreSQL connection SSL mode used when not
// explicitly configured in the CR.
const DefaultSSLMode = "verify-full"

// OCP monitoring stack endpoints. These are always available on OCP clusters
// and are hardcoded rather than discovered (OCP-only operator).
// Thanos Querier federates both cluster Prometheus and User Workload
// Monitoring Prometheus, providing a unified view of all metrics including
// user-namespace ServiceMonitors (Istio, app metrics, etc.).
const (
	OCPPrometheusURL   = "https://thanos-querier.openshift-monitoring.svc:9091"
	OCPAlertManagerURL = "https://alertmanager-main.openshift-monitoring.svc:9094"
)

// effectivePrometheusURL returns spec.monitoring.prometheus.url when set,
// falling back to the built-in OCP Thanos Querier route (#298).
func effectivePrometheusURL(knV2 *kubernautv1alpha2.Kubernaut) string {
	if u := knV2.Spec.Monitoring.Prometheus.URL; u != "" {
		return u
	}
	return OCPPrometheusURL
}

// effectiveAlertManagerURL returns spec.monitoring.alertManager.url when
// set, falling back to the built-in OCP AlertManager route (#298).
func effectiveAlertManagerURL(knV2 *kubernautv1alpha2.Kubernaut) string {
	if u := knV2.Spec.Monitoring.AlertManager.URL; u != "" {
		return u
	}
	return OCPAlertManagerURL
}

// effectiveEMTLSCaFile resolves EM's single external.tlsCaFile config key
// (#424). EM's upstream Config.External.TLSCaFile
// (kubernaut/internal/config/effectivenessmonitor/config.go) is, by
// design, one CA bundle shared by both its Prometheus and AlertManager
// HTTP clients -- there is no per-destination key to wire the two CR
// fields into independently, unlike KA's PrometheusToolConfig/
// AlertmanagerToolConfig, which are genuinely separate structs/keys.
// Monitoring.Prometheus.TLSCaFile therefore takes precedence over
// Monitoring.AlertManager.TLSCaFile when both are set (EM's primary
// assessment-scoring consumer), falling back to the existing
// service-ca-injected default when neither is set.
func effectiveEMTLSCaFile(knV2 *kubernautv1alpha2.Kubernaut, defaultPath string) string {
	return withDefault(withDefault(knV2.Spec.Monitoring.Prometheus.TLSCaFile, knV2.Spec.Monitoring.AlertManager.TLSCaFile), defaultPath)
}

// OCP well-known namespaces.
const (
	OCPDNSNamespace        = "openshift-dns"
	OCPMonitoringNamespace = "openshift-monitoring"
	OCPAlertManagerSAName  = "alertmanager-main"
	OCPIngressNamespace    = "openshift-ingress"
)

// AllComponents returns the ordered list of all managed components.
func AllComponents() []string {
	return []string{
		ComponentGateway,
		ComponentDataStorage,
		ComponentAIAnalysis,
		ComponentSignalProcessing,
		ComponentRemediationOrchestrator,
		ComponentWorkflowExecution,
		ComponentEffectivenessMonitor,
		ComponentNotification,
		ComponentKubernautAgent,
		ComponentAuthWebhook,
		ComponentAPIFrontend,
		ComponentFleetMetadataCache,
	}
}

// isComponentActive returns whether a component should be deployed.
// Always-on components return true; opt-in components check their spec gate.
// knV2 is only consulted for FleetMetadataCache -- Fleet's CRD surface lives
// exclusively in v1alpha2 (Fleet v1alpha2 migration).
func isComponentActive(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, component string) bool {
	switch component {
	case ComponentAPIFrontend:
		return kn.Spec.APIFrontendEnabled()
	case ComponentGateway:
		return kn.Spec.GatewayEnabled()
	case ComponentConsole:
		return kn.Spec.ConsoleEnabled()
	case ComponentFleetMetadataCache:
		return knV2.Spec.FleetMetadataCacheEnabled()
	default:
		return true
	}
}

// ActiveComponents returns the list of components that should be deployed
// for the given CR spec.
func ActiveComponents(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []string {
	var active []string
	for _, c := range AllComponents() {
		if isComponentActive(kn, knV2, c) {
			active = append(active, c)
		}
	}
	return active
}

// CommonLabels returns the base label set applied to every managed resource.
// Mirrors the Helm chart's kubernaut.labels helper.
func CommonLabels(kn *kubernautv1alpha1.Kubernaut) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "kubernaut-operator",
		"app.kubernetes.io/part-of":    "kubernaut",
		"app.kubernetes.io/instance":   kn.Name,
	}
}

// ComponentLabels returns labels for a specific component, including the
// common labels plus an app.kubernetes.io/component and the legacy "app" label
// used by Helm chart selectors.
func ComponentLabels(kn *kubernautv1alpha1.Kubernaut, component string) map[string]string {
	labels := CommonLabels(kn)
	labels["app.kubernetes.io/component"] = component
	labels["app"] = component
	return labels
}

// SelectorLabels returns the minimal label set used in Deployment.spec.selector.
func SelectorLabels(component string) map[string]string {
	return map[string]string{
		"app": component,
	}
}

// componentEnvSuffix maps a DeploymentParams.ImageName to the RELATED_IMAGE
// env var suffix. The env var is RELATED_IMAGE_<SUFFIX>.
var componentEnvSuffix = map[string]string{
	"gateway":                 "GATEWAY",
	"datastorage":             "DATA_STORAGE",
	"aianalysis":              "AIANALYSIS",
	"signalprocessing":        "SIGNALPROCESSING",
	"remediationorchestrator": "REMEDIATIONORCHESTRATOR",
	"workflowexecution":       "WORKFLOWEXECUTION",
	"effectivenessmonitor":    "EFFECTIVENESSMONITOR",
	"notification":            "NOTIFICATION",
	"kubernautagent":          "KUBERNAUT_AGENT",
	"authwebhook":             "AUTHWEBHOOK",
	"apifrontend":             "API_FRONTEND",
	"db-migrate":              "DB_MIGRATE",
	"init-ubi-minimal":        "INIT_UBI_MINIMAL",
	"console":                 "CONSOLE",
	"oauth2-proxy":            "OAUTH2_PROXY",
	"fleetmetadatacache":      "FLEETMETADATACACHE",
}

// ResolveImage returns the fully-qualified container image for a component.
// Resolution order:
//  1. CR spec.image.overrides[imageName]  (user override)
//  2. RELATED_IMAGE_<SUFFIX> env var       (set by OLM / manager.yaml)
//  3. Error
func ResolveImage(kn *kubernautv1alpha1.Kubernaut, imageName string) (string, error) {
	if kn.Spec.Image.Overrides != nil {
		if img, ok := kn.Spec.Image.Overrides[imageName]; ok && img != "" {
			return img, nil
		}
	}

	suffix, ok := componentEnvSuffix[imageName]
	if !ok {
		suffix = strings.ToUpper(strings.ReplaceAll(imageName, "-", "_"))
	}
	envKey := "RELATED_IMAGE_" + suffix
	if img := os.Getenv(envKey); img != "" {
		return img, nil
	}

	return "", fmt.Errorf("no image found for component %q: set RELATED_IMAGE_%s or spec.image.overrides[%q]", imageName, suffix, imageName)
}

// ObjectMeta returns a standard ObjectMeta for namespaced resources.
func ObjectMeta(kn *kubernautv1alpha1.Kubernaut, name, component string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: kn.Namespace,
		Labels:    ComponentLabels(kn, component),
	}
}

// SetOwnerReference sets the owner reference on a namespaced resource
// so it is garbage-collected when the Kubernaut CR is deleted.
func SetOwnerReference(kn *kubernautv1alpha1.Kubernaut, obj metav1.Object, scheme *runtime.Scheme) error {
	return controllerutil.SetControllerReference(kn, obj, scheme)
}

// PodSecurityContext returns the restricted-profile pod security context
// matching the Helm chart's kubernaut.podSecurityContext helper.
func PodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// ContainerSecurityContext returns the restricted-profile container security context
// matching the Helm chart's kubernaut.containerSecurityContext helper.
func ContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

// DefaultResources returns sensible defaults when the user hasn't specified limits/requests.
func DefaultResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}

// MergeResources returns the user-specified resources if non-zero, otherwise defaults.
func MergeResources(userSpec corev1.ResourceRequirements) corev1.ResourceRequirements {
	if len(userSpec.Requests) > 0 || len(userSpec.Limits) > 0 {
		return userSpec
	}
	return DefaultResources()
}

// ServicePort returns a ServicePort with the given name and port number.
func ServicePort(name string, port int32) corev1.ServicePort {
	return corev1.ServicePort{
		Name:       name,
		Port:       port,
		TargetPort: intstr.FromInt32(port),
		Protocol:   corev1.ProtocolTCP,
	}
}

// DataStorageURL returns the in-cluster DataStorage service URL.
func DataStorageURL(namespace string) string {
	return fmt.Sprintf("https://data-storage-service.%s.svc.cluster.local:8443", namespace)
}

// DataStorageHealthURL returns DataStorage's cross-service readiness-check
// endpoint (DD-PLATFORM-010, BR-AUDIT-005 v2.0, #360). It is an unauthenticated
// /readyz route on the SAME main API port as DataStorageURL, not a separate
// port -- registered as a top-level route outside the DD-AUTH-014
// auth-middleware group, so it differs from DataStorageURL only by path.
// Every upstream service that writes audit events (and KubernautAgent, via
// integrations.dataStorage.healthUrl) requires this as a REQUIRED config
// field (internal/config.DataStorageConfig.HealthURL /
// pkg/apifrontend/config.AgentConfig.DSHealthURL); omitting it causes those
// services to fail closed at startup.
func DataStorageHealthURL(namespace string) string {
	return DataStorageURL(namespace) + "/readyz"
}

// GatewayURL returns the in-cluster Gateway service URL (HTTPS via service-ca).
func GatewayURL(namespace string) string {
	return fmt.Sprintf("https://gateway-service.%s.svc.cluster.local:8443", namespace)
}

// FleetMetadataCacheURL returns the in-cluster FMC service URL. Plain HTTP,
// not HTTPS: upstream's FMC binary (cmd/fleetmetadatacache) has no TLS
// server support (ServerConfig has no cert fields, buildFMCServers()
// constructs a bare http.Server) and its own Helm chart serves the api port
// unencrypted, so there is no server-side cert for the operator to
// provision here either. FMC is only reachable from Gateway/
// RemediationOrchestrator pods in the same namespace (enforced by
// fleetMetadataCacheNetworkPolicy), the same trust boundary already
// accepted for unencrypted Valkey traffic elsewhere in this operator.
func FleetMetadataCacheURL(namespace string) string {
	return fmt.Sprintf("http://fleetmetadatacache-service.%s.svc.cluster.local:8080", namespace)
}

// resolveFleetEndpoint returns the effective spec.fleet.endpoint value.
// When the user leaves it empty and FMC is active (fleet.enabled=true,
// backend=fleetmetadatacache -- KubernautSpec.FleetMetadataCacheEnabled()),
// the in-cluster FMC service URL is auto-derived -- the whole point of the
// operator deploying FMC is that Gateway/RemediationOrchestrator don't need
// the user to separately wire up its address. backend=acm still requires an
// explicit endpoint (enforced in validation.go). Fleet's entire CRD surface
// lives in v1alpha2 (Fleet v1alpha2 migration), so this takes knV2 only.
func resolveFleetEndpoint(knV2 *kubernautv1alpha2.Kubernaut) string {
	fleet := &knV2.Spec.Fleet
	if fleet.Endpoint == "" && knV2.Spec.FleetMetadataCacheEnabled() {
		return FleetMetadataCacheURL(knV2.Namespace)
	}
	return fleet.Endpoint
}

// effectiveFleetOAuth2SecretRef resolves a component's nilable Fleet
// override (F1 -- api/v1alpha2 collapsed every component's bespoke
// FleetOAuth2CredentialsSecretRef field into a shared *FleetOverrideSpec),
// falling back to the shared spec.fleet.oauth2.credentialsSecretRef when
// the override is nil or its own field is empty.
func effectiveFleetOAuth2SecretRef(override *kubernautv1alpha2.FleetOverrideSpec, fleetDefault string) string {
	if override == nil {
		return fleetDefault
	}
	return withDefault(override.OAuth2CredentialsSecretRef, fleetDefault)
}

// PostgreSQLPort returns the effective PostgreSQL port, defaulting to 5432.
func PostgreSQLPort(kn *kubernautv1alpha1.Kubernaut) int32 {
	if kn.Spec.PostgreSQL.Port != 0 {
		return kn.Spec.PostgreSQL.Port
	}
	return DefaultPostgreSQLPort
}

// ResolveWorkflowNamespace returns the effective workflow namespace name.
func ResolveWorkflowNamespace(kn *kubernautv1alpha1.Kubernaut) string {
	if kn.Spec.WorkflowExecution.WorkflowNamespace != "" {
		return kn.Spec.WorkflowExecution.WorkflowNamespace
	}
	return DefaultWorkflowNamespace
}

// AIAnalysisPolicyName returns the AI analysis policy ConfigMap name,
// defaulting to "aianalysis-policy" when not overridden. Singular,
// matching SignalProcessingPolicyName below: the ConfigMap holds exactly
// one Rego file (key "approval.rego") in both cases, so there's no
// multiplicity to justify a plural name (was "aianalysis-policies" before
// this rename; see ADR-CRD-001 F11 for the upstream Helm chart's identical
// inconsistency and the corresponding proposal filed there).
func AIAnalysisPolicyName(kn *kubernautv1alpha1.Kubernaut) string {
	if kn.Spec.AIAnalysis.Policy.ConfigMapName != "" {
		return kn.Spec.AIAnalysis.Policy.ConfigMapName
	}
	return "aianalysis-policy"
}

// SignalProcessingPolicyName returns the signal processing policy ConfigMap name,
// defaulting to "signalprocessing-policy" when not overridden.
func SignalProcessingPolicyName(kn *kubernautv1alpha1.Kubernaut) string {
	if kn.Spec.SignalProcessing.Policy.ConfigMapName != "" {
		return kn.Spec.SignalProcessing.Policy.ConfigMapName
	}
	return "signalprocessing-policy"
}

// KubernautAgentLLMRuntimeConfigName returns the Kubernaut Agent LLM runtime
// ConfigMap name, defaulting to "kubernaut-agent-llm-runtime" when not overridden.
func KubernautAgentLLMRuntimeConfigName(kn *kubernautv1alpha1.Kubernaut) string {
	if kn.Spec.KubernautAgent.RuntimeConfigMapName != "" {
		return kn.Spec.KubernautAgent.RuntimeConfigMapName
	}
	return "kubernaut-agent-llm-runtime"
}

// ResolveLLMProfile looks up a named LLM profile in spec.llmProfiles.
// Returns the zero-value profile and false when ref is empty or does not
// match any key. Callers that already validated the CR (ValidateKubernaut)
// can treat a false ok as unreachable; renderers still guard defensively.
func ResolveLLMProfile(kn *kubernautv1alpha1.Kubernaut, ref string) (kubernautv1alpha1.LLMProfileSpec, bool) {
	if ref == "" {
		return kubernautv1alpha1.LLMProfileSpec{}, false
	}
	p, ok := kn.Spec.LLMProfiles[ref]
	return p, ok
}

// EffectiveKALLMProfileRef returns the profile name KA's investigator LLM
// calls resolve to: spec.kubernautAgent.llmProfileRef when explicitly set,
// otherwise the sole entry in spec.llmProfiles when it defines exactly one
// profile. This is the root of the whole llmProfileRef fallback chain
// (AFLLMProfileRef falls back to this; severity-triage/alignmentCheck fall
// back to AF's), so inferring it here alone lowers the barrier for the
// common single-provider case everywhere a component would otherwise share
// KA's identity, without requiring every downstream field to repeat the
// same cardinality check.
//
// Deliberately a count of map entries, not a fixed conventional key name
// (e.g. a profile literally named "primary"): a naming convention is an
// implicit contract a user has to already know, whereas "there was only
// one, so it's unambiguous" requires no naming convention. Returns "" when
// llmProfiles has zero or 2+ entries and no explicit ref was given —
// validateLLMProfileRefs turns that into a descriptive error rather than
// silently guessing.
func EffectiveKALLMProfileRef(kn *kubernautv1alpha1.Kubernaut) string {
	if kn.Spec.KubernautAgent.LLMProfileRef != "" {
		return kn.Spec.KubernautAgent.LLMProfileRef
	}
	if len(kn.Spec.LLMProfiles) == 1 {
		for name := range kn.Spec.LLMProfiles {
			return name
		}
	}
	return ""
}

// phaseCredentialsVolumeName returns the Volume/VolumeMount name for the
// dedicated Secret mount of a spec.kubernautAgent.phaseModels override
// whose profile has a different credentialsSecretName than KA's own
// profile (#233). Keyed by phase so concurrently-configured phase
// overrides never collide.
func phaseCredentialsVolumeName(phase string) string {
	return "phase-credentials-" + phase
}

// phaseCredentialsMountPath returns the mount directory for a phase
// override's dedicated credentials Secret (#233). KA resolves each
// phase's own apiKeyFile independently as of kubernaut#1728, so this no
// longer needs to alias KA's base "llm-credentials" mount.
func phaseCredentialsMountPath(phase string) string {
	return "/etc/kubernaut-agent/phase-credentials/" + phase
}

// AFLLMProfileRef returns the name of the profile API Frontend's own LLM
// connection resolves to: its own spec.apiFrontend.llmProfileRef when set,
// defaulting to KA's effective ref (EffectiveKALLMProfileRef) otherwise.
// This gives AF an independent LLM identity while preserving today's
// default behavior of implicitly sharing KA's profile -- including KA's own
// single-profile inference -- when AF doesn't specify its own.
func AFLLMProfileRef(kn *kubernautv1alpha1.Kubernaut) string {
	if kn.Spec.APIFrontend.LLMProfileRef != "" {
		return kn.Spec.APIFrontend.LLMProfileRef
	}
	return EffectiveKALLMProfileRef(kn)
}

// severityTriageCredentialsVolumeName is the Volume/VolumeMount name for
// severityTriage.llmProfileRef's dedicated Secret mount, used whenever its
// resolved profile has a different credentialsSecretName than API
// Frontend's own resolved profile (#234). AF resolves severityTriage.llm
// independently of agent.llm (resolveLLMKey() in
// pkg/apifrontend/config/config.go), so — outside the vertex_ai-vs-vertex_ai
// exception blocked by validateAFLLMProfileRefs (kubernaut#1731) — a
// distinct mount is always safe.
func severityTriageCredentialsVolumeName() string {
	return "severity-triage-credentials"
}

// severityTriageCredentialsMountPath is the mount directory for
// severityTriage.llmProfileRef's dedicated credentials Secret (#234).
func severityTriageCredentialsMountPath() string {
	return "/etc/apifrontend/severity-triage-credentials"
}

// ValkeyAddr returns the Valkey address in host:port format.
func ValkeyAddr(spec *kubernautv1alpha1.ValkeySpec) string {
	port := spec.Port
	if port == 0 {
		port = DefaultValkeyPort
	}
	return fmt.Sprintf("%s:%d", spec.Host, port)
}

// validHostname matches DNS names and IPv4/IPv6 addresses. Rejects strings
// containing shell metacharacters, whitespace, or DSN parameter separators.
var validHostname = regexp.MustCompile(`^[a-zA-Z0-9._:[\]-]+$`)

// ValidateHostname returns an error if host contains characters that could
// be used for shell or DSN parameter injection.
func ValidateHostname(host string) error {
	if host == "" {
		return fmt.Errorf("hostname must not be empty")
	}
	if !validHostname.MatchString(host) {
		return fmt.Errorf("hostname %q contains invalid characters", host)
	}
	return nil
}

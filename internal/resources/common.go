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
	"regexp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
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
)

// Well-known ports used across services.
const (
	PortHTTP    int32 = 8080
	PortHTTPS   int32 = 8443
	PortMetrics int32 = 9090
	// PortAuthWebhookService is the standard HTTPS port (443) exposed by the
	// auth-webhook Kubernetes Service, distinct from PortHTTPS (8443) used by
	// application containers.
	PortAuthWebhookService int32 = 443
	PortWebhookServer      int32 = 9443
	PortHealthProbe        int32 = 8081
)

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

// PDB constant.
const PDBMaxUnavailable = 1

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

// OCP monitoring stack endpoints. These are always available on OCP clusters
// and are hardcoded rather than discovered (OCP-only operator).
const (
	OCPPrometheusURL   = "https://prometheus-k8s.openshift-monitoring.svc:9091"
	OCPAlertManagerURL = "https://alertmanager-main.openshift-monitoring.svc:9094"
)

// OCP monitoring namespace and service account used for signal source RBAC.
const (
	OCPMonitoringNamespace = "openshift-monitoring"
	OCPAlertManagerSAName  = "alertmanager-main"
)

// DefaultPostgreSQLImage is the RHEL10 PostgreSQL 16 image used for the
// data-storage init container on OCP (restricted-v2 SCC compatible).
const DefaultPostgreSQLImage = "registry.redhat.io/rhel10/postgresql-16:16-1"

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
	}
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

// Image constructs a fully-qualified container image reference.
// Pattern: {Registry}/{Namespace}{Separator}{service}:{Tag}
// or       {Registry}/{Namespace}{Separator}{service}@{Digest}
// Returns an error if the spec would produce an invalid image reference.
func Image(spec *kubernautv1alpha1.ImageSpec, service string) (string, error) {
	if spec.Registry == "" {
		return "", fmt.Errorf("image registry must not be empty for service %q", service)
	}
	if spec.Tag == "" && spec.Digest == "" {
		return "", fmt.Errorf("image tag or digest must be set for service %q", service)
	}

	ns := spec.Namespace
	sep := spec.Separator
	if sep == "" {
		sep = "/"
	}

	var repo string
	if ns != "" {
		repo = fmt.Sprintf("%s%s%s", ns, sep, service)
	} else {
		repo = service
	}

	if spec.Digest != "" {
		return fmt.Sprintf("%s/%s@%s", spec.Registry, repo, spec.Digest), nil
	}
	return fmt.Sprintf("%s/%s:%s", spec.Registry, repo, spec.Tag), nil
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
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
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
	return fmt.Sprintf("https://data-storage-service.%s.svc.cluster.local:8080", namespace)
}

// GatewayURL returns the in-cluster Gateway service URL (HTTPS via service-ca).
func GatewayURL(namespace string) string {
	return fmt.Sprintf("https://gateway-service.%s.svc.cluster.local:8080", namespace)
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
// defaulting to "aianalysis-policies" when not overridden.
func AIAnalysisPolicyName(kn *kubernautv1alpha1.Kubernaut) string {
	if kn.Spec.AIAnalysis.Policy.ConfigMapName != "" {
		return kn.Spec.AIAnalysis.Policy.ConfigMapName
	}
	return "aianalysis-policies"
}

// SignalProcessingPolicyName returns the signal processing policy ConfigMap name,
// defaulting to "signalprocessing-policy" when not overridden.
func SignalProcessingPolicyName(kn *kubernautv1alpha1.Kubernaut) string {
	if kn.Spec.SignalProcessing.Policy.ConfigMapName != "" {
		return kn.Spec.SignalProcessing.Policy.ConfigMapName
	}
	return "signalprocessing-policy"
}

// KubernautAgentSDKConfigName returns the Kubernaut Agent SDK ConfigMap name,
// defaulting to "kubernaut-agent-sdk-config" when not overridden.
func KubernautAgentSDKConfigName(kn *kubernautv1alpha1.Kubernaut) string {
	if kn.Spec.KubernautAgent.LLM.SdkConfigMapName != "" {
		return kn.Spec.KubernautAgent.LLM.SdkConfigMapName
	}
	return "kubernaut-agent-sdk-config"
}

// ValkeyAddr returns the Valkey address in host:port format.
func ValkeyAddr(spec *kubernautv1alpha1.ValkeySpec) string {
	port := spec.Port
	if port == 0 {
		port = DefaultValkeyPort
	}
	return fmt.Sprintf("%s:%d", spec.Host, port)
}

// componentsNeedingNSRole lists components that require namespace-scoped Roles
// and RoleBindings. Currently all components need NS roles; split from
// AllComponents() if a component is added without RBAC needs.
var componentsNeedingNSRole = AllComponents()

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

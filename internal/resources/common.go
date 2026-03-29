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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	ComponentHolmesGPTAPI            = "holmesgpt-api"
	ComponentAuthWebhook             = "authwebhook"
)

// Well-known ports used across services.
const (
	PortHTTP  int32 = 8080
	PortHTTPS int32 = 8443
)

// DefaultWorkflowNamespace is the namespace used for workflow execution
// when not overridden in the CR spec.
const DefaultWorkflowNamespace = "kubernaut-workflows"

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
const DefaultPostgreSQLImage = "registry.redhat.io/rhel10/postgresql-16"

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
		ComponentHolmesGPTAPI,
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
		RunAsNonRoot: boolPtr(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// ContainerSecurityContext returns the restricted-profile container security context
// matching the Helm chart's kubernaut.containerSecurityContext helper.
func ContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: boolPtr(false),
		ReadOnlyRootFilesystem:   boolPtr(true),
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
	return fmt.Sprintf("http://data-storage-service.%s.svc.cluster.local:8080", namespace)
}

// GatewayURL returns the in-cluster Gateway service URL.
func GatewayURL(namespace string) string {
	return fmt.Sprintf("http://gateway-service.%s.svc.cluster.local:8080", namespace)
}

// ValkeyAddr returns the Valkey address in host:port format.
func ValkeyAddr(spec *kubernautv1alpha1.ValkeySpec) string {
	port := spec.Port
	if port == 0 {
		port = 6379
	}
	return fmt.Sprintf("%s:%d", spec.Host, port)
}

// PostgreSQLHost returns the PostgreSQL DSN host:port.
func PostgreSQLHost(spec *kubernautv1alpha1.PostgreSQLSpec) string {
	port := spec.Port
	if port == 0 {
		port = 5432
	}
	return fmt.Sprintf("%s:%d", spec.Host, port)
}

func boolPtr(b bool) *bool { return &b }

func int32Ptr(i int32) *int32 { return &i }

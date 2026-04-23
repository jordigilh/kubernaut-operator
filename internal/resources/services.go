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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// serviceDefinition maps a component to its Kubernetes Service name, ports,
// and optional annotations. Matches the Helm chart v1.3.0-rc11 topology.
type serviceDefinition struct {
	Component   string
	ServiceName string
	Ports       []corev1.ServicePort
	Annotations map[string]string
}

// apiServices are multi-port Services for components that expose HTTP APIs.
var apiServices = []serviceDefinition{
	{ComponentGateway, "gateway-service",
		[]corev1.ServicePort{ServicePort("http", PortHTTP), ServicePort("health", PortHealthProbe), ServicePort("metrics", PortMetrics)},
		map[string]string{OCPServingCertAnnotation: GatewayTLSSecretName}},
	{ComponentDataStorage, "data-storage-service",
		[]corev1.ServicePort{ServicePort("http", PortHTTP), ServicePort("health", PortHealthProbe)},
		map[string]string{OCPServingCertAnnotation: DataStorageTLSSecretName}},
	{ComponentAIAnalysis, "aianalysis-service",
		[]corev1.ServicePort{ServicePort("api", PortHTTP), ServicePort("metrics", PortMetrics), ServicePort("health", PortHealthProbe)},
		nil},
	{ComponentKubernautAgent, "kubernaut-agent",
		[]corev1.ServicePort{ServicePort("http", PortHTTP), ServicePort("health", PortHealthProbe), ServicePort("metrics", PortMetrics)},
		map[string]string{OCPServingCertAnnotation: KubernautAgentTLSSecretName}},
}

// metricsServiceDefinitions are single-port metrics-only Services for
// controller-style components that have no external HTTP API.
var metricsServiceDefinitions = []serviceDefinition{
	{ComponentSignalProcessing, "signalprocessing-controller-metrics",
		[]corev1.ServicePort{ServicePort("metrics", PortMetrics)}, nil},
	{ComponentRemediationOrchestrator, "remediationorchestrator-controller",
		[]corev1.ServicePort{ServicePort("metrics", PortMetrics)}, nil},
	{ComponentWorkflowExecution, "workflowexecution-controller-metrics",
		[]corev1.ServicePort{ServicePort("metrics", PortMetrics)}, nil},
	{ComponentEffectivenessMonitor, "effectivenessmonitor-metrics",
		[]corev1.ServicePort{ServicePort("metrics", PortMetrics)}, nil},
	{ComponentNotification, "notification-metrics",
		[]corev1.ServicePort{ServicePort("metrics", PortMetrics)}, nil},
}

// Inter-service TLS secret names provisioned by the OCP service-ca operator.
const (
	GatewayTLSSecretName        = "gateway-tls"
	DataStorageTLSSecretName    = "datastorage-tls"
	KubernautAgentTLSSecretName = "kubernautagent-tls"
)

// Services builds all API Services for the Kubernaut deployment.
// Annotations for OCP service-ca TLS provisioning are set per-service.
func Services(kn *kubernautv1alpha1.Kubernaut) []*corev1.Service {
	var services []*corev1.Service
	for _, def := range apiServices {
		services = append(services, buildService(kn, def))
	}

	// authwebhook uses port 443 → 9443 and needs OCP service-ca for TLS certs.
	awSvc := &corev1.Service{
		ObjectMeta: ObjectMeta(kn, "authwebhook-service", ComponentAuthWebhook),
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(ComponentAuthWebhook),
			Ports: []corev1.ServicePort{{
				Name:       "https",
				Port:       PortAuthWebhookService,
				TargetPort: intstr.FromString("webhook"),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
	awSvc.Annotations = map[string]string{
		OCPServingCertAnnotation: "authwebhook-tls",
	}
	services = append(services, awSvc)

	return services
}

// MetricsServices builds dedicated metrics-only Services for controller-style
// components that have no external HTTP API. Matches the Helm chart v1.3.0-rc11
// topology where these components expose only a :9090 metrics port.
func MetricsServices(kn *kubernautv1alpha1.Kubernaut) []*corev1.Service {
	var services []*corev1.Service
	for _, def := range metricsServiceDefinitions {
		services = append(services, buildService(kn, def))
	}
	return services
}

func buildService(kn *kubernautv1alpha1.Kubernaut, def serviceDefinition) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: ObjectMeta(kn, def.ServiceName, def.Component),
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(def.Component),
			Ports:    def.Ports,
		},
	}
	if len(def.Annotations) > 0 {
		svc.Annotations = def.Annotations
	}
	return svc
}

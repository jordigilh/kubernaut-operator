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

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// serviceDefinition maps a component to its Kubernetes Service name, ports,
// and optional annotations.
type serviceDefinition struct {
	Component   string
	ServiceName string
	Ports       []corev1.ServicePort
	Annotations map[string]string
}

// apiServices are multi-port Services for components that expose HTTP APIs.
var apiServices = []serviceDefinition{
	{ComponentGateway, "gateway-service",
		[]corev1.ServicePort{ServicePort("https", PortHTTPS), ServicePort("health", PortHealthProbe), ServicePort("metrics", PortMetrics)},
		nil},
	{ComponentDataStorage, "data-storage-service",
		[]corev1.ServicePort{ServicePort("https", PortHTTPS), ServicePort("health", PortHealthProbe)},
		nil},
	{ComponentAIAnalysis, "aianalysis-service",
		[]corev1.ServicePort{ServicePort("https", PortHTTPS), ServicePort("metrics", PortMetrics), ServicePort("health", PortHealthProbe)},
		nil},
	{ComponentKubernautAgent, "kubernaut-agent",
		[]corev1.ServicePort{ServicePort("https", PortHTTPS), ServicePort("health", PortHealthProbe), ServicePort("metrics", PortMetrics)},
		nil},
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
	GatewayTLSSecretName            = "gateway-tls"
	DataStorageTLSSecretName        = "datastorage-tls"
	KubernautAgentTLSSecretName     = "kubernautagent-tls"
	APIFrontendTLSSecretName        = "apifrontend-tls"
	FleetMetadataCacheTLSSecretName = "fleetmetadatacache-tls"
)

// Services builds all API Services for the Kubernaut deployment.
// Annotations for OCP service-ca TLS provisioning are set per-service.
func Services(kn *kubernautv1alpha2.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, sidecar KagentiSidecarMode) []*corev1.Service {
	services := make([]*corev1.Service, 0, len(apiServices)+2)
	for _, def := range apiServices {
		if def.Component == ComponentGateway && !kn.Spec.GatewayEnabled() {
			continue
		}
		svc := buildService(kn, def)
		applyTLSServiceMetadata(kn, svc, def.Component)
		services = append(services, svc)
	}

	// authwebhook uses port 443 → 9443 and requires a serving certificate from
	// the selected runtime TLS source.
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
	applyTLSServiceMetadata(kn, awSvc, ComponentAuthWebhook)
	services = append(services, awSvc)

	if kn.Spec.APIFrontendEnabled() {
		afMetricsPort := PortMetrics
		afHealthPort := PortHealthProbe
		if sidecar.ShiftsPorts() {
			afMetricsPort = 9092
			afHealthPort = 8082
		}
		if kn.Spec.APIFrontend.MetricsPort != nil {
			afMetricsPort = *kn.Spec.APIFrontend.MetricsPort
		}
		if kn.Spec.APIFrontend.HealthPort != nil {
			afHealthPort = *kn.Spec.APIFrontend.HealthPort
		}
		afSvc := buildService(kn, serviceDefinition{
			ComponentAPIFrontend, "apifrontend",
			[]corev1.ServicePort{
				ServicePort("https", PortHTTPS),
				ServicePort("health", afHealthPort),
				ServicePort("metrics", afMetricsPort),
				{
					Name:       AgentTLSPortName,
					Port:       PortAuthWebhookService, // 443
					TargetPort: intstr.FromInt32(PortHTTPS),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			nil,
		})
		applyTLSServiceMetadata(kn, afSvc, ComponentAPIFrontend)
		services = append(services, afSvc)
	}

	if knV2.Spec.FleetMetadataCacheEnabled() {
		fmcService := FleetMetadataCacheService(kn)
		applyTLSServiceMetadata(knV2, fmcService, ComponentFleetMetadataCache)
		services = append(services, fmcService)
	}

	return services
}

// MetricsServices builds dedicated metrics-only Services for controller-style
// components that have no external HTTP API. These components expose only a
// :9090 metrics port.
func MetricsServices(kn *kubernautv1alpha2.Kubernaut) []*corev1.Service {
	services := make([]*corev1.Service, 0, len(metricsServiceDefinitions))
	for _, def := range metricsServiceDefinitions {
		services = append(services, buildService(kn, def))
	}
	return services
}

func buildService(kn *kubernautv1alpha2.Kubernaut, def serviceDefinition) *corev1.Service {
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

// TLSSecretName returns the serving Secret selected for a component. The
// controller validates the TLS source before building Services/Deployments;
// this helper keeps existing pure resource-builder signatures intact.
func TLSSecretName(kn *kubernautv1alpha2.Kubernaut, component string) string {
	material, err := ResolveTLSMaterial(kn)
	if err != nil {
		return ""
	}
	return material.ServiceTLSSecretNames[tlsServiceKey(component)]
}

// tlsServiceKey keeps Kubernetes component names decoupled from the stable
// TLS configuration keys. In particular, the Service names use hyphens while
// the CRD keys intentionally remain DNS-independent component identifiers.
func tlsServiceKey(component string) string {
	switch component {
	case ComponentGateway:
		return TLSServiceGateway
	case ComponentDataStorage:
		return TLSServiceDataStorage
	case ComponentKubernautAgent:
		return TLSServiceKubernautAgent
	case ComponentAPIFrontend:
		return TLSServiceAPIFrontend
	case ComponentAuthWebhook:
		return TLSServiceAuthWebhook
	case ComponentFleetMetadataCache:
		return TLSServiceFleetMetadataCache
	default:
		return component
	}
}

func applyTLSServiceMetadata(kn *kubernautv1alpha2.Kubernaut, svc *corev1.Service, component string) {
	secretName := TLSSecretName(kn, component)
	if secretName == "" {
		return
	}
	material, err := ResolveTLSMaterial(kn)
	if err != nil {
		return
	}
	if material.Source == TLSMaterialSourceOpenShiftServiceCA {
		if svc.Annotations == nil {
			svc.Annotations = make(map[string]string, 1)
		}
		svc.Annotations[OCPServingCertAnnotation] = secretName
	}
}

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

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// serviceDefinitions maps component to service name, port, and port name.
var serviceDefinitions = []struct {
	Component   string
	ServiceName string
	Port        int32
	PortName    string
}{
	{ComponentGateway, "gateway-service", PortHTTP, "http"},
	{ComponentDataStorage, "data-storage-service", PortHTTP, "http"},
	{ComponentAIAnalysis, "aianalysis-service", PortHTTP, "http"},
	{ComponentSignalProcessing, "signalprocessing-service", PortHTTP, "http"},
	{ComponentRemediationOrchestrator, "remediationorchestrator-service", PortHTTP, "http"},
	{ComponentWorkflowExecution, "workflowexecution-service", PortHTTP, "http"},
	{ComponentEffectivenessMonitor, "effectivenessmonitor-service", PortHTTP, "http"},
	{ComponentNotification, "notification-service", PortHTTP, "http"},
	{ComponentHolmesGPTAPI, "holmesgpt-api-service", PortHTTP, "http"},
	{ComponentAuthWebhook, "authwebhook-service", PortHTTPS, "https"},
}

// Services builds all Services for the Kubernaut deployment.
func Services(kn *kubernautv1alpha1.Kubernaut) []*corev1.Service {
	var services []*corev1.Service
	for _, def := range serviceDefinitions {
		svc := &corev1.Service{
			ObjectMeta: ObjectMeta(kn, def.ServiceName, def.Component),
			Spec: corev1.ServiceSpec{
				Selector: SelectorLabels(def.Component),
				Ports:    []corev1.ServicePort{ServicePort(def.PortName, def.Port)},
			},
		}
		services = append(services, svc)
	}
	return services
}

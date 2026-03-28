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

// serviceDefinitions maps component to service name and port.
var serviceDefinitions = []struct {
	Component   string
	ServiceName string
	Port        int32
}{
	{ComponentGateway, "gateway-service", 8080},
	{ComponentDataStorage, "data-storage-service", 8080},
	{ComponentAIAnalysis, "aianalysis-service", 8080},
	{ComponentSignalProcessing, "signalprocessing-service", 8080},
	{ComponentRemediationOrchestrator, "remediationorchestrator-service", 8080},
	{ComponentWorkflowExecution, "workflowexecution-service", 8080},
	{ComponentEffectivenessMonitor, "effectivenessmonitor-service", 8080},
	{ComponentNotification, "notification-service", 8080},
	{ComponentHolmesGPTAPI, "holmesgpt-api-service", 8080},
	{ComponentAuthWebhook, "authwebhook-service", 8443},
}

// Services builds all Services for the Kubernaut deployment.
func Services(kn *kubernautv1alpha1.Kubernaut) []*corev1.Service {
	var services []*corev1.Service
	for _, def := range serviceDefinitions {
		svc := &corev1.Service{
			ObjectMeta: ObjectMeta(kn, def.ServiceName, def.Component),
			Spec: corev1.ServiceSpec{
				Selector: SelectorLabels(def.Component),
				Ports:    []corev1.ServicePort{ServicePort(def.Port)},
			},
		}
		services = append(services, svc)
	}
	return services
}

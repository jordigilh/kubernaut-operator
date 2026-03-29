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

// serviceAccountNames maps components to their ServiceAccount names,
// matching the Helm chart conventions.
var serviceAccountNames = map[string]string{
	ComponentGateway:                 "gateway",
	ComponentDataStorage:             "data-storage-sa",
	ComponentAIAnalysis:              "aianalysis-controller",
	ComponentSignalProcessing:        "signalprocessing-controller",
	ComponentRemediationOrchestrator: "remediationorchestrator-controller",
	ComponentWorkflowExecution:       "workflowexecution-controller",
	ComponentEffectivenessMonitor:    "effectivenessmonitor-controller",
	ComponentNotification:            "notification-controller",
	ComponentHolmesGPTAPI:            "holmesgpt-api-sa",
	ComponentAuthWebhook:             "authwebhook",
}

// ServiceAccountName returns the canonical SA name for a component.
func ServiceAccountName(component string) string {
	if name, ok := serviceAccountNames[component]; ok {
		return name
	}
	return component
}

// ServiceAccount builds a ServiceAccount for the given component.
func ServiceAccount(kn *kubernautv1alpha1.Kubernaut, component string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: ObjectMeta(kn, ServiceAccountName(component), component),
	}
}

// WorkflowRunnerServiceAccount returns the SA used by workflow Jobs/PipelineRuns
// in the workflow namespace.
func WorkflowRunnerServiceAccount(kn *kubernautv1alpha1.Kubernaut) *corev1.ServiceAccount {
	ns := kn.Spec.WorkflowExecution.WorkflowNamespace
	if ns == "" {
		ns = DefaultWorkflowNamespace
	}
	sa := &corev1.ServiceAccount{
		ObjectMeta: ObjectMeta(kn, "kubernaut-workflow-runner", ComponentWorkflowExecution),
	}
	sa.Namespace = ns
	return sa
}

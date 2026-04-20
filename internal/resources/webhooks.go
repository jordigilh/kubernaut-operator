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
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// MutatingWebhookConfiguration builds the AuthWebhook MutatingWebhookConfiguration.
// Mirrors the Helm chart's authwebhook/webhooks.yaml with three mutating
// webhooks for workflowexecution, remediationapprovalrequest, and
// remediationrequest status mutations (audit attribution).
// OCP service-CA injects the caBundle via the inject-cabundle annotation.
func MutatingWebhookConfiguration(kn *kubernautv1alpha1.Kubernaut) *admissionregistrationv1.MutatingWebhookConfiguration {
	namespacedScope := admissionregistrationv1.NamespacedScope

	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:        kn.Namespace + "-authwebhook-mutating",
			Labels:      CommonLabels(kn),
			Annotations: map[string]string{OCPServiceCAInjectAnnotation: "true"},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name:                    "workflowexecution.mutate.kubernaut.ai",
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             sideEffectPtr(admissionregistrationv1.SideEffectClassNone),
				FailurePolicy:           failurePolicyPtr(admissionregistrationv1.Fail),
				MatchPolicy:             matchPolicyPtr(admissionregistrationv1.Equivalent),
				TimeoutSeconds:          int32Ptr(10),
				ClientConfig:            webhookClientConfig(kn, "/mutate-workflowexecution"),
				Rules: []admissionregistrationv1.RuleWithOperations{{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"kubernaut.ai"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"workflowexecutions/status"},
						Scope:       &namespacedScope,
					},
				}},
			},
			{
				Name:                    "remediationapprovalrequest.mutate.kubernaut.ai",
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             sideEffectPtr(admissionregistrationv1.SideEffectClassNone),
				FailurePolicy:           failurePolicyPtr(admissionregistrationv1.Fail),
				MatchPolicy:             matchPolicyPtr(admissionregistrationv1.Equivalent),
				TimeoutSeconds:          int32Ptr(10),
				ClientConfig:            webhookClientConfig(kn, "/mutate-remediationapprovalrequest"),
				Rules: []admissionregistrationv1.RuleWithOperations{{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"kubernaut.ai"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"remediationapprovalrequests/status"},
						Scope:       &namespacedScope,
					},
				}},
			},
			{
				Name:                    "remediationrequest.mutate.kubernaut.ai",
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             sideEffectPtr(admissionregistrationv1.SideEffectClassNone),
				FailurePolicy:           failurePolicyPtr(admissionregistrationv1.Fail),
				MatchPolicy:             matchPolicyPtr(admissionregistrationv1.Equivalent),
				TimeoutSeconds:          int32Ptr(10),
				ClientConfig:            webhookClientConfig(kn, "/mutate-remediationrequest"),
				Rules: []admissionregistrationv1.RuleWithOperations{{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"kubernaut.ai"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"remediationrequests/status"},
						Scope:       &namespacedScope,
					},
				}},
			},
		},
	}
}

// ValidatingWebhookConfiguration builds the AuthWebhook ValidatingWebhookConfiguration.
// Mirrors the Helm chart's authwebhook/webhooks.yaml with three validating
// webhooks: notificationrequest deletion (attribution), remediationworkflow
// CUD (schema validation), and actiontype CUD (schema validation).
// OCP service-CA injects the caBundle via the inject-cabundle annotation.
func ValidatingWebhookConfiguration(kn *kubernautv1alpha1.Kubernaut) *admissionregistrationv1.ValidatingWebhookConfiguration {
	namespacedScope := admissionregistrationv1.NamespacedScope

	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:        kn.Namespace + "-authwebhook-validating",
			Labels:      CommonLabels(kn),
			Annotations: map[string]string{OCPServiceCAInjectAnnotation: "true"},
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				Name:                    "notificationrequest.validate.kubernaut.ai",
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             sideEffectPtr(admissionregistrationv1.SideEffectClassNone),
				FailurePolicy:           failurePolicyPtr(admissionregistrationv1.Fail),
				MatchPolicy:             matchPolicyPtr(admissionregistrationv1.Equivalent),
				TimeoutSeconds:          int32Ptr(10),
				ClientConfig:            webhookClientConfig(kn, "/validate-notificationrequest-delete"),
				Rules: []admissionregistrationv1.RuleWithOperations{{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Delete},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"kubernaut.ai"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"notificationrequests"},
						Scope:       &namespacedScope,
					},
				}},
			},
			{
				Name:                    "remediationworkflow.validate.kubernaut.ai",
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             sideEffectPtr(admissionregistrationv1.SideEffectClassNoneOnDryRun),
				FailurePolicy:           failurePolicyPtr(admissionregistrationv1.Fail),
				MatchPolicy:             matchPolicyPtr(admissionregistrationv1.Equivalent),
				TimeoutSeconds:          int32Ptr(15),
				ClientConfig:            webhookClientConfig(kn, "/validate-remediationworkflow"),
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": kn.Namespace,
					},
				},
				Rules: []admissionregistrationv1.RuleWithOperations{{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
						admissionregistrationv1.Delete,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"kubernaut.ai"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"remediationworkflows"},
						Scope:       &namespacedScope,
					},
				}},
			},
			{
				Name:                    "actiontype.validate.kubernaut.ai",
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             sideEffectPtr(admissionregistrationv1.SideEffectClassNoneOnDryRun),
				FailurePolicy:           failurePolicyPtr(admissionregistrationv1.Fail),
				MatchPolicy:             matchPolicyPtr(admissionregistrationv1.Equivalent),
				TimeoutSeconds:          int32Ptr(15),
				ClientConfig:            webhookClientConfig(kn, "/validate-actiontype"),
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": kn.Namespace,
					},
				},
				Rules: []admissionregistrationv1.RuleWithOperations{{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
						admissionregistrationv1.Delete,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"kubernaut.ai"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"actiontypes"},
						Scope:       &namespacedScope,
					},
				}},
			},
		},
	}
}

func webhookClientConfig(kn *kubernautv1alpha1.Kubernaut, path string) admissionregistrationv1.WebhookClientConfig {
	port := PortAuthWebhookService
	return admissionregistrationv1.WebhookClientConfig{
		Service: &admissionregistrationv1.ServiceReference{
			Namespace: kn.Namespace,
			Name:      "authwebhook-service",
			Path:      strPtr(path),
			Port:      &port,
		},
	}
}

func strPtr(s string) *string { return &s }
func sideEffectPtr(v admissionregistrationv1.SideEffectClass) *admissionregistrationv1.SideEffectClass {
	return &v
}
func failurePolicyPtr(v admissionregistrationv1.FailurePolicyType) *admissionregistrationv1.FailurePolicyType {
	return &v
}
func matchPolicyPtr(v admissionregistrationv1.MatchPolicyType) *admissionregistrationv1.MatchPolicyType {
	return &v
}
func int32Ptr(v int32) *int32 { return &v }

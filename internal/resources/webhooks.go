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
// OCP service-CA injects the caBundle via the inject-cabundle annotation.
func MutatingWebhookConfiguration(kn *kubernautv1alpha1.Kubernaut) *admissionregistrationv1.MutatingWebhookConfiguration {
	clientConfig, rules := webhookClientConfigAndRules(kn, "/mutate")

	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:        kn.Namespace + "-authwebhook-mutating",
			Labels:      CommonLabels(kn),
			Annotations: map[string]string{OCPServiceCAInjectAnnotation: "true"},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{{
			Name:                    "mutating.authwebhook.kubernaut.ai",
			AdmissionReviewVersions: []string{"v1"},
			SideEffects:             sideEffectPtr(admissionregistrationv1.SideEffectClassNone),
			FailurePolicy:           failurePolicyPtr(admissionregistrationv1.Fail),
			MatchPolicy:             matchPolicyPtr(admissionregistrationv1.Equivalent),
			ClientConfig:            clientConfig,
			Rules:                   rules,
		}},
	}
}

// ValidatingWebhookConfiguration builds the AuthWebhook ValidatingWebhookConfiguration.
// OCP service-CA injects the caBundle via the inject-cabundle annotation.
func ValidatingWebhookConfiguration(kn *kubernautv1alpha1.Kubernaut) *admissionregistrationv1.ValidatingWebhookConfiguration {
	clientConfig, rules := webhookClientConfigAndRules(kn, "/validate")

	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:        kn.Namespace + "-authwebhook-validating",
			Labels:      CommonLabels(kn),
			Annotations: map[string]string{OCPServiceCAInjectAnnotation: "true"},
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{
			Name:                    "validating.authwebhook.kubernaut.ai",
			AdmissionReviewVersions: []string{"v1"},
			SideEffects:             sideEffectPtr(admissionregistrationv1.SideEffectClassNone),
			FailurePolicy:           failurePolicyPtr(admissionregistrationv1.Fail),
			MatchPolicy:             matchPolicyPtr(admissionregistrationv1.Equivalent),
			ClientConfig:            clientConfig,
			Rules:                   rules,
		}},
	}
}

func webhookClientConfigAndRules(kn *kubernautv1alpha1.Kubernaut, path string) (admissionregistrationv1.WebhookClientConfig, []admissionregistrationv1.RuleWithOperations) {
	port := PortAuthWebhookService
	scope := admissionregistrationv1.AllScopes

	clientConfig := admissionregistrationv1.WebhookClientConfig{
		Service: &admissionregistrationv1.ServiceReference{
			Namespace: kn.Namespace,
			Name:      "authwebhook-service",
			Path:      strPtr(path),
			Port:      &port,
		},
	}

	rules := []admissionregistrationv1.RuleWithOperations{{
		Operations: []admissionregistrationv1.OperationType{
			admissionregistrationv1.Create,
			admissionregistrationv1.Update,
		},
		Rule: admissionregistrationv1.Rule{
			APIGroups:   []string{"kubernaut.ai"},
			APIVersions: []string{"v1alpha1"},
			Resources:   []string{"actiontypes"},
			Scope:       &scope,
		},
	}}

	return clientConfig, rules
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

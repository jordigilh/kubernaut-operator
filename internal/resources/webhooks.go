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
func MutatingWebhookConfiguration(kn *kubernautv1alpha1.Kubernaut, caBundle []byte) *admissionregistrationv1.MutatingWebhookConfiguration {
	ns := kn.Namespace
	sideEffects := admissionregistrationv1.SideEffectClassNone
	failurePolicy := admissionregistrationv1.Fail
	matchPolicy := admissionregistrationv1.Equivalent
	scope := admissionregistrationv1.AllScopes
	port := PortHTTPS

	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "authwebhook-mutating",
			Labels: CommonLabels(kn),
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{{
			Name:                    "mutating.authwebhook.kubernaut.ai",
			AdmissionReviewVersions: []string{"v1"},
			SideEffects:             &sideEffects,
			FailurePolicy:           &failurePolicy,
			MatchPolicy:             &matchPolicy,
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service: &admissionregistrationv1.ServiceReference{
					Namespace: ns,
					Name:      "authwebhook-service",
					Path:      strPtr("/mutate"),
					Port:      &port,
				},
				CABundle: caBundle,
			},
			Rules: []admissionregistrationv1.RuleWithOperations{{
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
			}},
		}},
	}
}

// ValidatingWebhookConfiguration builds the AuthWebhook ValidatingWebhookConfiguration.
func ValidatingWebhookConfiguration(kn *kubernautv1alpha1.Kubernaut, caBundle []byte) *admissionregistrationv1.ValidatingWebhookConfiguration {
	ns := kn.Namespace
	sideEffects := admissionregistrationv1.SideEffectClassNone
	failurePolicy := admissionregistrationv1.Fail
	matchPolicy := admissionregistrationv1.Equivalent
	scope := admissionregistrationv1.AllScopes
	port := PortHTTPS

	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "authwebhook-validating",
			Labels: CommonLabels(kn),
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{
			Name:                    "validating.authwebhook.kubernaut.ai",
			AdmissionReviewVersions: []string{"v1"},
			SideEffects:             &sideEffects,
			FailurePolicy:           &failurePolicy,
			MatchPolicy:             &matchPolicy,
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service: &admissionregistrationv1.ServiceReference{
					Namespace: ns,
					Name:      "authwebhook-service",
					Path:      strPtr("/validate"),
					Port:      &port,
				},
				CABundle: caBundle,
			},
			Rules: []admissionregistrationv1.RuleWithOperations{{
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
			}},
		}},
	}
}

func strPtr(s string) *string { return &s }

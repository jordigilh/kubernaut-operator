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

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// GatewayAlertManagerConfig builds a namespace-scoped AlertmanagerConfig CR
// that routes alerts matching the kubernaut-system namespace to the Gateway
// webhook. This eliminates the need to manually edit the global AlertManager
// secret in openshift-monitoring.
//
// Authorization is included only when Gateway authentication is enabled and
// spec.gateway.auth.alertManager.tokenSecretName is set (#377): the referenced
// Secret is bring-your-own, never minted or owned by the operator (consistent
// with spec.postgresql.secretName and spec.llmProfiles[*].credentialsSecretName),
// since the token belongs to a Kubernetes identity in openshift-monitoring that
// the operator has no business impersonating or provisioning credentials for.
// It must be bound to the gateway-signal-source ClusterRole (handled by the
// operator's RBAC provisioning) so the bearer token is authorized by Gateway's
// SAR middleware. When authentication is enabled but the token name is unset,
// the CR is still valid but unauthenticated calls will be rejected by Gateway
// -- the controller surfaces this via the ConditionAlertManagerAuthConfigured
// status condition rather than silently referencing a Secret that may not exist.
func GatewayAlertManagerConfig(kn *kubernautv1alpha2.Kubernaut) *monitoringv1alpha1.AlertmanagerConfig {
	gwURL := fmt.Sprintf("https://gateway-service.%s.svc.cluster.local:%d/api/v1/signals/prometheus",
		kn.Namespace, PortHTTPS)

	httpConfig := &monitoringv1alpha1.HTTPConfig{
		TLSConfig: &monitoringv1.SafeTLSConfig{
			CA: monitoringv1.SecretOrConfigMap{
				ConfigMap: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: TrustBundleConfigMapName},
					Key:                  "service-ca.crt",
				},
			},
		},
	}
	if kn.Spec.Gateway.Auth.AuthenticationEnabled() {
		secretName := kn.Spec.Gateway.Auth.AlertManager.TokenSecretName
		if secretName != "" {
			httpConfig.Authorization = &monitoringv1.SafeAuthorization{
				Type: "Bearer",
				Credentials: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "token",
				},
			}
		}
	}

	return &monitoringv1alpha1.AlertmanagerConfig{
		ObjectMeta: ObjectMeta(kn, "kubernaut-gateway-alerts", ComponentGateway),
		Spec: monitoringv1alpha1.AlertmanagerConfigSpec{
			Route: &monitoringv1alpha1.Route{
				Receiver:      "gateway-webhook",
				GroupBy:       []string{"alertname", "namespace"},
				GroupWait:     nonEmptyDurationPtr("5s"),
				GroupInterval: nonEmptyDurationPtr("5s"),
			},
			Receivers: []monitoringv1alpha1.Receiver{
				{
					Name: "gateway-webhook",
					WebhookConfigs: []monitoringv1alpha1.WebhookConfig{
						{
							URL:          &gwURL,
							SendResolved: boolPtr(false),
							HTTPConfig:   httpConfig,
						},
					},
				},
			},
		},
	}
}

func nonEmptyDurationPtr(d monitoringv1.NonEmptyDuration) *monitoringv1.NonEmptyDuration { return &d }

// AnnotationCreatedBy marks resources created (not adopted) by the operator.
// This is provenance only: #514 retains every workflow namespace on uninstall,
// regardless of its creator, to preserve provisioning-owned content.
const AnnotationCreatedBy = "kubernaut.ai/created-by"

// WorkflowNamespace builds the Namespace resource for workflow execution.
func WorkflowNamespace(kn *kubernautv1alpha2.Kubernaut) *corev1.Namespace {
	labels := CommonLabels(kn)
	for k, v := range RestrictedPSALabels() {
		labels[k] = v
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ResolveWorkflowNamespace(kn),
			Labels: labels,
			Annotations: map[string]string{
				AnnotationCreatedBy: "kubernaut-operator",
			},
		},
	}
}

// RestrictedPSALabels returns the Pod Security Admission labels that pin a
// namespace to the "restricted" level. Used unconditionally (no CR
// opt-out) on kubernaut-workflows as a defense-in-depth backstop for the
// WorkflowExecution controller's spawned Job/Tekton pods (#208, companion
// to kubernaut BR-WE-018/GAP-03): even if the controller's
// SecurityContext-authoring code ever regresses, the API server
// independently rejects non-compliant pods at admission time.
func RestrictedPSALabels() map[string]string {
	return map[string]string{
		"pod-security.kubernetes.io/enforce": "restricted",
		"pod-security.kubernetes.io/audit":   "restricted",
		"pod-security.kubernetes.io/warn":    "restricted",
	}
}

// EnsureRestrictedPSALabels patches labels with any missing/mismatched
// RestrictedPSALabels() entries in place. Returns true if it changed
// anything, so callers can skip an unnecessary Update when the namespace
// already converged. Mirrors ensurePSALabels' (privileged, SPIFFE-CSI
// namespace) shape for the "restricted" level.
func EnsureRestrictedPSALabels(labels map[string]string) bool {
	changed := false
	for k, v := range RestrictedPSALabels() {
		if labels[k] != v {
			labels[k] = v
			changed = true
		}
	}
	return changed
}

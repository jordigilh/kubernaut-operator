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

	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// SSE streams (live status, investigation progress) require long-lived
// connections. OCP HAProxy defaults to 30s which kills them prematurely.
const routeSSETimeout = "3600s"

// GatewayRoute builds the OCP Route for external access to the Gateway.
// Returns nil if Route creation is disabled.
func GatewayRoute(kn *kubernautv1alpha1.Kubernaut) *routev1.Route {
	if !kn.Spec.Gateway.Route.RouteEnabled() {
		return nil
	}

	route := &routev1.Route{
		ObjectMeta: ObjectMeta(kn, "gateway-route", ComponentGateway),
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: "gateway-service",
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("https"),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationReencrypt,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		},
	}
	route.Annotations = map[string]string{
		"haproxy.router.openshift.io/timeout": routeSSETimeout,
	}

	if kn.Spec.Gateway.Route.Hostname != "" {
		route.Spec.Host = kn.Spec.Gateway.Route.Hostname
	}

	return route
}

// GatewayRouteStub returns a minimal Route object suitable for deletion lookups
// when the Route feature is disabled. It carries just enough metadata for
// deleteIfExists to find the resource.
func GatewayRouteStub(kn *kubernautv1alpha1.Kubernaut) *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-route",
			Namespace: kn.Namespace,
		},
	}
}

// APIFrontendRoute builds the OCP Route for external access to the AF.
// Returns nil if Route creation is disabled (default).
func APIFrontendRoute(kn *kubernautv1alpha1.Kubernaut) *routev1.Route {
	if !kn.Spec.APIFrontend.Route.AFRouteEnabled() {
		return nil
	}

	route := &routev1.Route{
		ObjectMeta: ObjectMeta(kn, "apifrontend-route", ComponentAPIFrontend),
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: "apifrontend",
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("https"),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationReencrypt,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		},
	}
	route.Annotations = map[string]string{
		"haproxy.router.openshift.io/timeout": routeSSETimeout,
	}

	if kn.Spec.APIFrontend.Route.Hostname != "" {
		route.Spec.Host = kn.Spec.APIFrontend.Route.Hostname
	}

	return route
}

// APIFrontendRouteStub returns a minimal Route object suitable for deletion
// lookups when the AF Route is disabled.
func APIFrontendRouteStub(kn *kubernautv1alpha1.Kubernaut) *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apifrontend-route",
			Namespace: kn.Namespace,
		},
	}
}

// GatewayAlertManagerConfig builds a namespace-scoped AlertmanagerConfig CR
// that routes alerts matching the kubernaut-system namespace to the Gateway
// webhook. This eliminates the need to manually edit the global AlertManager
// secret in openshift-monitoring.
//
// The AlertManager SA must be bound to the gateway-signal-source ClusterRole
// (handled by the operator's RBAC provisioning) so that the bearer token
// included via credentials_file is authorized by the Gateway's SAR middleware.
//
// Returns nil when monitoring is disabled.
func GatewayAlertManagerConfig(kn *kubernautv1alpha1.Kubernaut) *monitoringv1alpha1.AlertmanagerConfig {
	if !kn.Spec.Monitoring.MonitoringEnabled() {
		return nil
	}

	gwURL := fmt.Sprintf("https://gateway-service.%s.svc.cluster.local:%d/api/v1/signals/prometheus",
		kn.Namespace, PortHTTPS)

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
							HTTPConfig: &monitoringv1alpha1.HTTPConfig{
								Authorization: &monitoringv1.SafeAuthorization{
									Type: "Bearer",
									Credentials: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "alertmanager-gateway-token"},
										Key:                  "token",
									},
								},
								TLSConfig: &monitoringv1.SafeTLSConfig{
									InsecureSkipVerify: boolPtr(true),
								},
							},
						},
					},
				},
			},
		},
	}
}

// GatewayAlertManagerTokenSecret builds the Secret containing a long-lived SA
// token for the AlertManager → Gateway webhook authentication. The token is
// projected from the alertmanager-main ServiceAccount in openshift-monitoring,
// but since AlertmanagerConfig webhook_configs reference a Secret in the local
// namespace, the operator creates this bridging Secret.
// Returns nil when monitoring is disabled.
func GatewayAlertManagerTokenSecret(kn *kubernautv1alpha1.Kubernaut) *corev1.Secret {
	if !kn.Spec.Monitoring.MonitoringEnabled() {
		return nil
	}
	return &corev1.Secret{
		ObjectMeta: ObjectMeta(kn, "alertmanager-gateway-token", ComponentGateway),
		Type:       corev1.SecretTypeOpaque,
	}
}

func nonEmptyDurationPtr(d monitoringv1.NonEmptyDuration) *monitoringv1.NonEmptyDuration { return &d }

// dbSecretsYAML is the typed structure for db-secrets.yaml, ensuring values
// are properly YAML-encoded instead of interpolated via fmt.Sprintf.
type dbSecretsYAML struct {
	Host     string `json:"host" yaml:"host"`
	Port     int32  `json:"port" yaml:"port"`
	DBName   string `json:"dbname" yaml:"dbname"`
	User     string `json:"user" yaml:"user"`
	Password string `json:"password" yaml:"password"`
}

// DataStorageDBSecret derives the datastorage-db-secret from the user-provided
// PostgreSQL secret. The DataStorage service expects a "db-secrets.yaml" key
// with YAML content containing host, port, dbname, user, and password.
// Returns an error if any required key is missing from the source secret.
func DataStorageDBSecret(kn *kubernautv1alpha1.Kubernaut, pgSecret *corev1.Secret) (*corev1.Secret, error) {
	requiredKeys := []string{"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB"}
	for _, key := range requiredKeys {
		if _, ok := pgSecret.Data[key]; !ok {
			return nil, fmt.Errorf("PostgreSQL secret %q is missing required key %q", pgSecret.Name, key)
		}
	}

	content, err := yaml.Marshal(dbSecretsYAML{
		Host:     kn.Spec.PostgreSQL.Host,
		Port:     PostgreSQLPort(kn),
		DBName:   string(pgSecret.Data["POSTGRES_DB"]),
		User:     string(pgSecret.Data["POSTGRES_USER"]),
		Password: string(pgSecret.Data["POSTGRES_PASSWORD"]),
	})
	if err != nil {
		return nil, fmt.Errorf("marshalling db-secrets.yaml: %w", err)
	}

	return &corev1.Secret{
		ObjectMeta: ObjectMeta(kn, "datastorage-db-secret", ComponentDataStorage),
		Data: map[string][]byte{
			"db-secrets.yaml": content,
		},
	}, nil
}

// AnnotationCreatedBy marks resources created (not adopted) by the operator.
// Used to distinguish operator-created namespaces from pre-existing ones so
// that deletion does not destroy foreign namespaces.
const AnnotationCreatedBy = "kubernaut.ai/created-by"

// WorkflowNamespace builds the Namespace resource for workflow execution.
func WorkflowNamespace(kn *kubernautv1alpha1.Kubernaut) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ResolveWorkflowNamespace(kn),
			Labels: CommonLabels(kn),
			Annotations: map[string]string{
				AnnotationCreatedBy: "kubernaut-operator",
			},
		},
	}
}

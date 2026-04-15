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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

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
				TargetPort: intstr.FromString("http"),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationEdge,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		},
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

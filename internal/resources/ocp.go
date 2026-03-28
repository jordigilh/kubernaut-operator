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

	pgPort := kn.Spec.PostgreSQL.Port
	if pgPort == 0 {
		pgPort = 5432
	}

	user := string(pgSecret.Data["POSTGRES_USER"])
	password := string(pgSecret.Data["POSTGRES_PASSWORD"])
	dbname := string(pgSecret.Data["POSTGRES_DB"])

	yamlContent := fmt.Sprintf(
		"host: %s\nport: %d\ndbname: %s\nuser: %s\npassword: %s\n",
		kn.Spec.PostgreSQL.Host, pgPort, dbname, user, password,
	)

	return &corev1.Secret{
		ObjectMeta: ObjectMeta(kn, "datastorage-db-secret", ComponentDataStorage),
		Data: map[string][]byte{
			"db-secrets.yaml": []byte(yamlContent),
		},
	}, nil
}

// WorkflowNamespace builds the Namespace resource for workflow execution.
func WorkflowNamespace(kn *kubernautv1alpha1.Kubernaut) *corev1.Namespace {
	wfNs := kn.Spec.WorkflowExecution.WorkflowNamespace
	if wfNs == "" {
		wfNs = DefaultWorkflowNamespace
	}

	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   wfNs,
			Labels: CommonLabels(kn),
		},
	}
}

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
	"strings"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGatewayRoute_EnabledByDefault(t *testing.T) {
	kn := testKubernaut()
	route := GatewayRoute(kn)

	if route == nil {
		t.Fatal("GatewayRoute should not be nil when enabled by default")
	}
	if route.Name != "gateway-route" {
		t.Errorf("name = %q, want %q", route.Name, "gateway-route")
	}
	if route.Namespace != "kubernaut-system" {
		t.Errorf("namespace = %q, want %q", route.Namespace, "kubernaut-system")
	}
	if route.Spec.To.Name != "gateway-service" {
		t.Errorf("route target = %q, want %q", route.Spec.To.Name, "gateway-service")
	}
	if route.Spec.TLS == nil {
		t.Fatal("route should have TLS config")
	}
	if route.Spec.TLS.Termination != routev1.TLSTerminationEdge {
		t.Errorf("TLS termination = %q, want %q", route.Spec.TLS.Termination, routev1.TLSTerminationEdge)
	}
	if route.Spec.TLS.InsecureEdgeTerminationPolicy != routev1.InsecureEdgeTerminationPolicyRedirect {
		t.Errorf("insecure policy = %q, want Redirect", route.Spec.TLS.InsecureEdgeTerminationPolicy)
	}
}

func TestGatewayRouteStub_MinimalMetadata(t *testing.T) {
	kn := testKubernaut()
	stub := GatewayRouteStub(kn)

	if stub.Name != "gateway-route" {
		t.Errorf("Name = %q, want %q", stub.Name, "gateway-route")
	}
	if stub.Namespace != kn.Namespace {
		t.Errorf("Namespace = %q, want %q", stub.Namespace, kn.Namespace)
	}
}

func TestGatewayRoute_DisabledExplicitly(t *testing.T) {
	kn := testKubernaut()
	disabled := false
	kn.Spec.Gateway.Route.Enabled = &disabled
	route := GatewayRoute(kn)

	if route != nil {
		t.Error("GatewayRoute should be nil when explicitly disabled")
	}
}

func TestGatewayRoute_CustomHostname(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.Gateway.Route.Hostname = "kubernaut.example.com"
	route := GatewayRoute(kn)

	if route.Spec.Host != "kubernaut.example.com" {
		t.Errorf("route host = %q, want %q", route.Spec.Host, "kubernaut.example.com")
	}
}

func TestGatewayRoute_NoHostnameByDefault(t *testing.T) {
	kn := testKubernaut()
	route := GatewayRoute(kn)

	if route.Spec.Host != "" {
		t.Errorf("route host should be empty by default, got %q", route.Spec.Host)
	}
}

func TestDataStorageDBSecret_DerivesFromPGSecret(t *testing.T) {
	kn := testKubernaut()
	pgSecret := &corev1.Secret{
		Data: map[string][]byte{
			"POSTGRES_USER":     []byte("kubernaut"),
			"POSTGRES_PASSWORD": []byte("s3cret"),
			"POSTGRES_DB":       []byte("action_history"),
		},
	}

	dsSecret, err := DataStorageDBSecret(kn, pgSecret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dsSecret.Name != "datastorage-db-secret" {
		t.Errorf("name = %q, want %q", dsSecret.Name, "datastorage-db-secret")
	}

	yamlContent := string(dsSecret.Data["db-secrets.yaml"])
	if !strings.Contains(yamlContent, "host: pg.example.com") {
		t.Errorf("db-secrets.yaml should contain PG host, got:\n%s", yamlContent)
	}
	if !strings.Contains(yamlContent, "port: 5432") {
		t.Errorf("db-secrets.yaml should contain PG port, got:\n%s", yamlContent)
	}
	if !strings.Contains(yamlContent, "dbname: action_history") {
		t.Errorf("db-secrets.yaml should contain dbname, got:\n%s", yamlContent)
	}
	if !strings.Contains(yamlContent, "user: kubernaut") {
		t.Errorf("db-secrets.yaml should contain user, got:\n%s", yamlContent)
	}
	if !strings.Contains(yamlContent, "password: s3cret") {
		t.Errorf("db-secrets.yaml should contain password, got:\n%s", yamlContent)
	}
}

func TestDataStorageDBSecret_DefaultPort(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.PostgreSQL.Port = 0
	pgSecret := &corev1.Secret{
		Data: map[string][]byte{
			"POSTGRES_USER":     []byte("u"),
			"POSTGRES_PASSWORD": []byte("p"),
			"POSTGRES_DB":       []byte("d"),
		},
	}

	dsSecret, err := DataStorageDBSecret(kn, pgSecret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yamlContent := string(dsSecret.Data["db-secrets.yaml"])
	if !strings.Contains(yamlContent, "port: 5432") {
		t.Errorf("should default to port 5432, got:\n%s", yamlContent)
	}
}

func TestDataStorageDBSecret_MissingKey_ReturnsError(t *testing.T) {
	kn := testKubernaut()
	pgSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pg-secret"},
		Data: map[string][]byte{
			"POSTGRES_USER": []byte("u"),
		},
	}

	_, err := DataStorageDBSecret(kn, pgSecret)
	if err == nil {
		t.Error("DataStorageDBSecret should return error when required keys are missing")
	}
}

func TestWorkflowNamespace_DefaultName(t *testing.T) {
	kn := testKubernaut()
	ns := WorkflowNamespace(kn)

	if ns.Name != "kubernaut-workflows" {
		t.Errorf("name = %q, want %q", ns.Name, "kubernaut-workflows")
	}
	if ns.Labels["app.kubernetes.io/managed-by"] != "kubernaut-operator" {
		t.Error("workflow namespace should have managed-by label")
	}
}

func TestWorkflowNamespace_CustomName(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.WorkflowExecution.WorkflowNamespace = "my-workflows"
	ns := WorkflowNamespace(kn)

	if ns.Name != "my-workflows" {
		t.Errorf("name = %q, want %q", ns.Name, "my-workflows")
	}
}

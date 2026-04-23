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
	"testing"
)

func TestServices_APIServiceCount(t *testing.T) {
	kn := testKubernaut()
	svcs := Services(kn)
	// 4 API services + 1 authwebhook = 5
	if len(svcs) != 5 {
		t.Errorf("Services() should return 5, got %d", len(svcs))
	}
}

func TestServices_MetricsServiceCount(t *testing.T) {
	kn := testKubernaut()
	svcs := MetricsServices(kn)
	if len(svcs) != 5 {
		t.Errorf("MetricsServices() should return 5, got %d", len(svcs))
	}
}

func TestServices_AllInCorrectNamespace(t *testing.T) {
	kn := testKubernaut()
	all := append(Services(kn), MetricsServices(kn)...)
	for _, svc := range all {
		if svc.Namespace != "kubernaut-system" {
			t.Errorf("Service %q namespace = %q, want %q", svc.Name, svc.Namespace, "kubernaut-system")
		}
	}
}

func TestServices_AuthWebhookOn443(t *testing.T) {
	kn := testKubernaut()
	for _, svc := range Services(kn) {
		if svc.Name == "authwebhook-service" {
			if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].Port != 443 {
				t.Errorf("authwebhook-service port should be 443, got %v", svc.Spec.Ports)
			}
			if svc.Annotations[OCPServingCertAnnotation] != "authwebhook-tls" {
				t.Errorf("authwebhook-service should have serving-cert annotation, got %v", svc.Annotations)
			}
			return
		}
	}
	t.Error("Services() should contain authwebhook-service")
}

func TestServices_APIServicesHaveHTTPPort(t *testing.T) {
	kn := testKubernaut()
	for _, svc := range Services(kn) {
		if svc.Name == "authwebhook-service" {
			continue
		}
		found := false
		for _, p := range svc.Spec.Ports {
			if p.Port == 8080 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Service %q should have port 8080", svc.Name)
		}
	}
}

func TestServices_ExpectedAPIServiceNames(t *testing.T) {
	kn := testKubernaut()
	svcs := Services(kn)
	names := make(map[string]bool, len(svcs))
	for _, svc := range svcs {
		names[svc.Name] = true
	}

	expected := []string{
		"gateway-service",
		"data-storage-service",
		"aianalysis-service",
		"kubernaut-agent",
		"authwebhook-service",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("Services() missing expected service %q", name)
		}
	}
}

func TestServices_ExpectedMetricsServiceNames(t *testing.T) {
	kn := testKubernaut()
	svcs := MetricsServices(kn)
	names := make(map[string]bool, len(svcs))
	for _, svc := range svcs {
		names[svc.Name] = true
	}

	expected := []string{
		"signalprocessing-controller-metrics",
		"remediationorchestrator-controller",
		"workflowexecution-controller-metrics",
		"effectivenessmonitor-metrics",
		"notification-metrics",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("MetricsServices() missing expected service %q", name)
		}
	}
}

func TestServices_SelectorsMatchComponents(t *testing.T) {
	kn := testKubernaut()
	all := append(Services(kn), MetricsServices(kn)...)

	knownComponents := make(map[string]bool)
	for _, c := range AllComponents() {
		knownComponents[c] = true
	}

	for _, svc := range all {
		app, ok := svc.Spec.Selector["app"]
		if !ok {
			t.Errorf("Service %q missing 'app' selector", svc.Name)
			continue
		}
		if !knownComponents[app] {
			t.Errorf("Service %q selector app=%q is not a known component", svc.Name, app)
		}
	}
}

func TestServices_GatewayMultiPort(t *testing.T) {
	kn := testKubernaut()
	for _, svc := range Services(kn) {
		if svc.Name == "gateway-service" {
			wantPorts := map[string]int32{"http": 8080, "health": 8081, "metrics": 9090}
			gotPorts := make(map[string]int32)
			for _, p := range svc.Spec.Ports {
				gotPorts[p.Name] = p.Port
			}
			for name, port := range wantPorts {
				if gotPorts[name] != port {
					t.Errorf("gateway-service port %q = %d, want %d", name, gotPorts[name], port)
				}
			}
			return
		}
	}
	t.Fatal("gateway-service not found")
}

func TestServices_KubernautAgentServingCert(t *testing.T) {
	kn := testKubernaut()
	for _, svc := range Services(kn) {
		if svc.Name == "kubernaut-agent" {
			v, ok := svc.Annotations[OCPServingCertAnnotation]
			if !ok {
				t.Fatal("kubernaut-agent missing serving-cert-secret-name annotation")
			}
			if v != KubernautAgentTLSSecretName {
				t.Errorf("kubernaut-agent annotation = %q, want %q", v, KubernautAgentTLSSecretName)
			}
			return
		}
	}
	t.Fatal("kubernaut-agent service not found")
}

func TestServices_GatewayHasServingCertAnnotation(t *testing.T) {
	kn := testKubernaut()
	for _, svc := range Services(kn) {
		if svc.Name == "gateway-service" {
			v, ok := svc.Annotations[OCPServingCertAnnotation]
			if !ok {
				t.Fatal("gateway-service missing serving-cert-secret-name annotation")
			}
			if v != GatewayTLSSecretName {
				t.Errorf("gateway-service annotation = %q, want %q", v, GatewayTLSSecretName)
			}
			return
		}
	}
	t.Fatal("gateway-service not found")
}

func TestServices_DataStorageHasServingCertAnnotation(t *testing.T) {
	kn := testKubernaut()
	for _, svc := range Services(kn) {
		if svc.Name == "data-storage-service" {
			v, ok := svc.Annotations[OCPServingCertAnnotation]
			if !ok {
				t.Fatal("data-storage-service missing serving-cert-secret-name annotation")
			}
			if v != DataStorageTLSSecretName {
				t.Errorf("data-storage-service annotation = %q, want %q", v, DataStorageTLSSecretName)
			}
			return
		}
	}
	t.Fatal("data-storage-service not found")
}

func TestServices_MetricsServicesOnlyExposePort9090(t *testing.T) {
	kn := testKubernaut()
	for _, svc := range MetricsServices(kn) {
		if len(svc.Spec.Ports) != 1 {
			t.Errorf("metrics service %q should have exactly 1 port, got %d", svc.Name, len(svc.Spec.Ports))
			continue
		}
		if svc.Spec.Ports[0].Port != 9090 {
			t.Errorf("metrics service %q port = %d, want 9090", svc.Name, svc.Spec.Ports[0].Port)
		}
	}
}

func TestPodDisruptionBudgets_Count10(t *testing.T) {
	kn := testKubernaut()
	pdbs := PodDisruptionBudgets(kn)
	if len(pdbs) != 10 {
		t.Errorf("PodDisruptionBudgets() should return 10, got %d", len(pdbs))
	}
}

func TestPodDisruptionBudgets_MaxUnavailable1(t *testing.T) {
	kn := testKubernaut()
	for _, pdb := range PodDisruptionBudgets(kn) {
		if pdb.Spec.MaxUnavailable == nil {
			t.Errorf("PDB %q should have MaxUnavailable set", pdb.Name)
			continue
		}
		if pdb.Spec.MaxUnavailable.IntValue() != 1 {
			t.Errorf("PDB %q MaxUnavailable = %d, want 1", pdb.Name, pdb.Spec.MaxUnavailable.IntValue())
		}
	}
}

func TestPodDisruptionBudgets_CorrectNamespace(t *testing.T) {
	kn := testKubernaut()
	for _, pdb := range PodDisruptionBudgets(kn) {
		if pdb.Namespace != "kubernaut-system" {
			t.Errorf("PDB %q namespace = %q, want %q", pdb.Name, pdb.Namespace, "kubernaut-system")
		}
	}
}

func TestPodDisruptionBudgets_HaveSelectors(t *testing.T) {
	kn := testKubernaut()
	for _, pdb := range PodDisruptionBudgets(kn) {
		if pdb.Spec.Selector == nil || len(pdb.Spec.Selector.MatchLabels) == 0 {
			t.Errorf("PDB %q should have a non-empty selector", pdb.Name)
		}
	}
}

func TestPodDisruptionBudgets_NamesMatchComponents(t *testing.T) {
	kn := testKubernaut()
	pdbs := PodDisruptionBudgets(kn)
	components := AllComponents()
	if len(pdbs) != len(components) {
		t.Fatalf("PDB count = %d, component count = %d", len(pdbs), len(components))
	}
	for i, pdb := range pdbs {
		if pdb.Name != components[i] {
			t.Errorf("PDB[%d] name = %q, want %q (no -pdb suffix)", i, pdb.Name, components[i])
		}
	}
}

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

func TestServices_Count10(t *testing.T) {
	kn := testKubernaut()
	svcs := Services(kn)
	if len(svcs) != 10 {
		t.Errorf("Services() should return 10, got %d", len(svcs))
	}
}

func TestServices_AllInCorrectNamespace(t *testing.T) {
	kn := testKubernaut()
	for _, svc := range Services(kn) {
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

func TestServices_OthersOn8080(t *testing.T) {
	kn := testKubernaut()
	for _, svc := range Services(kn) {
		if svc.Name == "authwebhook-service" {
			continue
		}
		if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].Port != 8080 {
			t.Errorf("Service %q port should be 8080, got %v", svc.Name, svc.Spec.Ports)
		}
	}
}

func TestServices_SelectorsMatchDeployments(t *testing.T) {
	kn := testKubernaut()
	svcs := Services(kn)

	knownComponents := make(map[string]bool)
	for _, c := range AllComponents() {
		knownComponents[c] = true
	}

	for _, svc := range svcs {
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

func TestServices_ExpectedNames(t *testing.T) {
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
		"signalprocessing-service",
		"remediationorchestrator-service",
		"workflowexecution-service",
		"effectivenessmonitor-service",
		"notification-service",
		"kubernaut-agent-service",
		"authwebhook-service",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("Services() missing expected service %q", name)
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

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

func TestPodDisruptionBudgets_SelectorsMatchComponentSelectorLabels(t *testing.T) {
	kn := testKubernaut()
	components := AllComponents()
	pdbs := PodDisruptionBudgets(kn)
	if len(pdbs) != len(components) {
		t.Fatalf("PDB count = %d, component count = %d", len(pdbs), len(components))
	}
	for i, pdb := range pdbs {
		component := components[i]
		if pdb.Name != component {
			t.Fatalf("PDB[%d] name = %q, want %q (index mismatch)", i, pdb.Name, component)
		}
		want := SelectorLabels(component)
		if pdb.Spec.Selector == nil {
			t.Errorf("PDB %q should have selector", pdb.Name)
			continue
		}
		got := pdb.Spec.Selector.MatchLabels
		if len(got) != len(want) {
			t.Errorf("PDB %q selector len = %d, want %d (got %#v want %#v)", pdb.Name, len(got), len(want), got, want)
			continue
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("PDB %q selector %q = %q, want %q", pdb.Name, k, got[k], v)
			}
		}
	}
}

func TestPodDisruptionBudgets_LabelsIncludeManagedBy(t *testing.T) {
	kn := testKubernaut()
	for _, pdb := range PodDisruptionBudgets(kn) {
		if pdb.Labels["app.kubernetes.io/managed-by"] != "kubernaut-operator" {
			t.Errorf("PDB %q labels missing app.kubernetes.io/managed-by=kubernaut-operator, got %#v", pdb.Name, pdb.Labels)
		}
	}
}

func TestPodDisruptionBudgets_CorrectNamespace(t *testing.T) {
	kn := testKubernaut()
	for _, pdb := range PodDisruptionBudgets(kn) {
		if pdb.Namespace != testSystemNamespace {
			t.Errorf("PDB %q namespace = %q, want %q", pdb.Name, pdb.Namespace, testSystemNamespace)
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

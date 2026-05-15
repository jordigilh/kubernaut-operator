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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PodDisruptionBudgets", func() {
	It("returns PDBs only for active components", func() {
		kn := testKubernaut()
		pdbs := PodDisruptionBudgets(kn)
		Expect(len(pdbs)).To(Equal(len(ActiveComponents(kn))))
	})

	It("sets MaxUnavailable=1 on default components and MinAvailable=1 on DS/AF", func() {
		kn := testKubernaut()
		for _, pdb := range PodDisruptionBudgets(kn) {
			switch pdb.Name {
			case ComponentDataStorage, ComponentAPIFrontend:
				Expect(pdb.Spec.MinAvailable).NotTo(BeNil(), "PDB %q should have MinAvailable set", pdb.Name)
				Expect(pdb.Spec.MinAvailable.IntValue()).To(Equal(1), "PDB %q MinAvailable = %d, want 1", pdb.Name, pdb.Spec.MinAvailable.IntValue())
			default:
				Expect(pdb.Spec.MaxUnavailable).NotTo(BeNil(), "PDB %q should have MaxUnavailable set", pdb.Name)
				Expect(pdb.Spec.MaxUnavailable.IntValue()).To(Equal(1), "PDB %q MaxUnavailable = %d, want 1", pdb.Name, pdb.Spec.MaxUnavailable.IntValue())
			}
		}
	})

	It("aligns selectors with component selector labels by index", func() {
		kn := testKubernaut()
		components := ActiveComponents(kn)
		pdbs := PodDisruptionBudgets(kn)
		Expect(len(pdbs)).To(Equal(len(components)), "PDB count = %d, component count = %d", len(pdbs), len(components))
		for i, pdb := range pdbs {
			component := components[i]
			Expect(pdb.Name).To(Equal(component), "PDB[%d] name = %q, want %q (index mismatch)", i, pdb.Name, component)
			want := SelectorLabels(component)
			Expect(pdb.Spec.Selector).NotTo(BeNil(), "PDB %q should have selector", pdb.Name)
			got := pdb.Spec.Selector.MatchLabels
			Expect(len(got)).To(Equal(len(want)), "PDB %q selector len = %d, want %d (got %#v want %#v)", pdb.Name, len(got), len(want), got, want)
			for k, v := range want {
				Expect(got[k]).To(Equal(v), "PDB %q selector %q = %q, want %q", pdb.Name, k, got[k], v)
			}
		}
	})

	It("labels every PDB with managed-by kubernaut-operator", func() {
		kn := testKubernaut()
		for _, pdb := range PodDisruptionBudgets(kn) {
			Expect(pdb.Labels["app.kubernetes.io/managed-by"]).To(Equal("kubernaut-operator"), "PDB %q labels missing app.kubernetes.io/managed-by=kubernaut-operator, got %#v", pdb.Name, pdb.Labels)
		}
	})

	It("places every PDB in the system namespace", func() {
		kn := testKubernaut()
		for _, pdb := range PodDisruptionBudgets(kn) {
			Expect(pdb.Namespace).To(Equal(testSystemNamespace), "PDB %q namespace = %q, want %q", pdb.Name, pdb.Namespace, testSystemNamespace)
		}
	})

	It("names PDBs after components without a -pdb suffix", func() {
		kn := testKubernaut()
		pdbs := PodDisruptionBudgets(kn)
		components := ActiveComponents(kn)
		Expect(len(pdbs)).To(Equal(len(components)), "PDB count = %d, component count = %d", len(pdbs), len(components))
		for i, pdb := range pdbs {
			Expect(pdb.Name).To(Equal(components[i]), "PDB[%d] name = %q, want %q (no -pdb suffix)", i, pdb.Name, components[i])
		}
	})
})

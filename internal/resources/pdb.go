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
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// PodDisruptionBudgets builds PDBs for all active Kubernaut components.
// Each PDB allows at most 1 unavailable pod, matching the Helm chart.
// Opt-in components (e.g. APIFrontend) are skipped when not configured.
func PodDisruptionBudgets(kn *kubernautv1alpha2.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []*policyv1.PodDisruptionBudget {
	pdbs := make([]*policyv1.PodDisruptionBudget, 0, len(PDBComponents()))

	for _, component := range PDBComponents() {
		if !isComponentActive(kn, knV2, component) {
			continue
		}
		workload := workloadForComponent(kn, knV2, component)
		if workload != nil && workload.PDB != nil && workload.PDB.Enabled != nil && !*workload.PDB.Enabled {
			continue
		}
		minAvailable, maxUnavailable := workloadPDBValues(workload)
		pdb := &policyv1.PodDisruptionBudget{
			ObjectMeta: ObjectMeta(kn, component, component),
			Spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable:   minAvailable,
				MaxUnavailable: maxUnavailable,
				Selector:       &metav1.LabelSelector{MatchLabels: SelectorLabels(component)},
			},
		}
		pdbs = append(pdbs, pdb)
	}

	return pdbs
}

func workloadPDBValues(workload *kubernautv1alpha2.WorkloadSpec) (*intstr.IntOrString, *intstr.IntOrString) {
	if workload != nil && workload.PDB != nil {
		if workload.PDB.MinAvailable != nil {
			value := *workload.PDB.MinAvailable
			return &value, nil
		}
		if workload.PDB.MaxUnavailable != nil {
			value := *workload.PDB.MaxUnavailable
			return nil, &value
		}
	}
	maxUnavailable := intstr.FromInt32(PDBMaxUnavailable)
	return nil, &maxUnavailable
}

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

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// PodDisruptionBudgets builds PDBs for all 10 Kubernaut components.
// Each PDB allows at most 1 unavailable pod, matching the Helm chart.
func PodDisruptionBudgets(kn *kubernautv1alpha1.Kubernaut) []*policyv1.PodDisruptionBudget {
	maxUnavailable := intstr.FromInt32(PDBMaxUnavailable)
	pdbs := make([]*policyv1.PodDisruptionBudget, 0, len(AllComponents()))

	for _, component := range AllComponents() {
		pdbs = append(pdbs, &policyv1.PodDisruptionBudget{
			ObjectMeta: ObjectMeta(kn, component, component),
			Spec: policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: &maxUnavailable,
				Selector:       &metav1.LabelSelector{MatchLabels: SelectorLabels(component)},
			},
		})
	}

	return pdbs
}

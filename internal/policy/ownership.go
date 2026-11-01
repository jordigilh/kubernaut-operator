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

package policy

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OwnershipError enforces the standard NetworkPolicy ownership boundary.
// Administrator-owned policies are never adopted, updated, or deleted.
func OwnershipError(existing, desired client.Object) error {
	key := client.ObjectKey{Name: desired.GetName(), Namespace: desired.GetNamespace()}
	labels := existing.GetLabels()
	if labels[ManagedPolicyLabel] != "true" {
		return fmt.Errorf("refusing to overwrite unmanaged NetworkPolicy %s", key)
	}
	desiredLabels := desired.GetLabels()
	for _, label := range []string{ManagedByLabel, PolicyNamespaceLabel, instanceLabel} {
		if labels[label] != desiredLabels[label] {
			return fmt.Errorf("refusing to overwrite NetworkPolicy %s with different %s ownership", key, label)
		}
	}
	if !existing.GetDeletionTimestamp().IsZero() {
		return fmt.Errorf("refusing to overwrite terminating NetworkPolicy %s", key)
	}
	refs := existing.GetOwnerReferences()
	if len(refs) == 0 {
		return nil
	}
	if len(refs) != 1 || existing.GetNamespace() != desiredLabels[PolicyNamespaceLabel] {
		return fmt.Errorf("refusing to overwrite NetworkPolicy %s with foreign or invalid-scope owners %v", key, refs)
	}
	ref := refs[0]
	if ref.APIVersion != "kubernaut.ai/v1alpha2" || ref.Kind != "Kubernaut" ||
		ref.Name != desiredLabels[instanceLabel] || ref.Controller == nil || !*ref.Controller {
		return fmt.Errorf("refusing to overwrite NetworkPolicy %s with foreign owner %v", key, ref)
	}
	return nil
}

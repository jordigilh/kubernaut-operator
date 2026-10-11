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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// OwnershipError is the native-provider ownership boundary, separate from the
// generic resource marker. All five policy identity labels remain mandatory.
// See docs/design/ISSUE-514-OWNERSHIP-CONTRACT.md.
func OwnershipError(existing, desired *unstructured.Unstructured) error {
	key := client.ObjectKey{Name: desired.GetName(), Namespace: desired.GetNamespace()}
	labels := existing.GetLabels()
	if labels[ManagedPolicyLabel] != "true" {
		return fmt.Errorf("refusing to overwrite unmanaged native policy %s", key)
	}
	desiredLabels := desired.GetLabels()
	for _, label := range []string{ManagedByLabel, PolicyNamespaceLabel, "app.kubernetes.io/instance", ProviderLabel} {
		if labels[label] != desiredLabels[label] {
			return fmt.Errorf("refusing to overwrite native policy %s with different %s ownership", key, label)
		}
	}
	if !existing.GetDeletionTimestamp().IsZero() {
		return fmt.Errorf("refusing to overwrite terminating native policy %s", key)
	}
	refs := existing.GetOwnerReferences()
	if len(refs) == 0 {
		return nil
	}
	// Namespaced native policies use the Kubernaut controller reference;
	// cluster-scoped policies must not carry an invalid cross-scope owner.
	if len(refs) != 1 || existing.GetNamespace() != desiredLabels[PolicyNamespaceLabel] {
		return fmt.Errorf("refusing to overwrite native policy %s with foreign or invalid-scope owners %v", key, refs)
	}
	ref := refs[0]
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil || gv.Group != kubernautv1alpha2.GroupVersion.Group || ref.Kind != "Kubernaut" ||
		ref.Name != desiredLabels["app.kubernetes.io/instance"] || ref.Controller == nil || !*ref.Controller {
		return fmt.Errorf("refusing to overwrite native policy %s with foreign owner %v", key, ref)
	}
	// Complete matching policy markers permit bounded same-identity reinstall.
	// The controller repairs a stale reference using the current desired owner.
	return nil
}

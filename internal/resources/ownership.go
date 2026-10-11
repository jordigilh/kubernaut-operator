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
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	// AnnotationOwnerNamespace disambiguates singleton instances across namespaces.
	AnnotationOwnerNamespace = "kubernaut.ai/owner-namespace"
	// AnnotationOwnerUID records provenance without an invalid cross-scope owner reference.
	AnnotationOwnerUID = "kubernaut.ai/owner-uid"
)

// ErrOwnershipConflict identifies an authorization failure, not spec drift.
var ErrOwnershipConflict = errors.New("ownership conflict")

// ResourceOwnershipError authorizes existing resources; see the approved
// docs/design/ISSUE-514-OWNERSHIP-CONTRACT.md. A complete marker is the explicit
// legacy/reinstall exception; a name or a spec hash alone never authorizes writes.
func ResourceOwnershipError(kn *kubernautv1alpha2.Kubernaut, obj metav1.Object, controllerOwned bool) error {
	if !obj.GetDeletionTimestamp().IsZero() {
		return fmt.Errorf("%w: resource is terminating", ErrOwnershipConflict)
	}
	completeMarker, err := ownershipMarkerMatches(kn, obj)
	if err != nil {
		return err
	}
	currentOwner, err := resourceOwnerMatches(kn, obj, controllerOwned)
	if err != nil {
		return err
	}
	if currentOwner || completeMarker {
		return nil
	}
	return fmt.Errorf("%w: no current controller owner or complete operator ownership marker", ErrOwnershipConflict)
}

func resourceOwnerMatches(kn *kubernautv1alpha2.Kubernaut, obj metav1.Object, controllerOwned bool) (bool, error) {
	refs := obj.GetOwnerReferences()
	if len(refs) == 0 {
		return false, nil
	}
	if !controllerOwned || obj.GetNamespace() != kn.Namespace || len(refs) != 1 {
		return false, fmt.Errorf("%w: foreign or invalid-scope owner references %v", ErrOwnershipConflict, refs)
	}
	ref := refs[0]
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil || gv.Group != kubernautv1alpha2.GroupVersion.Group || ref.Kind != "Kubernaut" || ref.Name != kn.Name || ref.Controller == nil || !*ref.Controller {
		return false, fmt.Errorf("%w: foreign owner reference %v", ErrOwnershipConflict, ref)
	}
	if uid := obj.GetAnnotations()[AnnotationOwnerUID]; uid != "" && uid != string(ref.UID) {
		return false, fmt.Errorf("%w: owner uid marker %q contradicts reference %q", ErrOwnershipConflict, uid, ref.UID)
	}
	return ref.UID == kn.UID, nil
}

func ownershipMarkerMatches(kn *kubernautv1alpha2.Kubernaut, obj metav1.Object) (bool, error) {
	annotations := obj.GetAnnotations()
	if namespace := annotations[AnnotationOwnerNamespace]; namespace != "" && namespace != kn.Namespace {
		return false, fmt.Errorf("%w: owner namespace %q differs from %q", ErrOwnershipConflict, namespace, kn.Namespace)
	}
	if annotations[AnnotationOwnerUID] != "" && annotations[AnnotationOwnerNamespace] == "" {
		return false, fmt.Errorf("%w: owner uid marker lacks an owner namespace", ErrOwnershipConflict)
	}
	complete := true
	expectedLabels := CommonLabels(kn)
	for _, key := range []string{"app.kubernetes.io/managed-by", "app.kubernetes.io/part-of", "app.kubernetes.io/instance"} {
		value, present := obj.GetLabels()[key]
		if present && value != expectedLabels[key] {
			return false, fmt.Errorf("%w: %s=%q differs from %q", ErrOwnershipConflict, key, value, expectedLabels[key])
		}
		complete = complete && present
	}
	return complete, nil
}

// StampOwnership records the explicit identity for operator-managed resources.
func StampOwnership(kn *kubernautv1alpha2.Kubernaut, obj metav1.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string, 3)
	}
	for key, value := range CommonLabels(kn) {
		labels[key] = value
	}
	obj.SetLabels(labels)
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 2)
	}
	annotations[AnnotationOwnerNamespace] = kn.Namespace
	annotations[AnnotationOwnerUID] = string(kn.UID)
	obj.SetAnnotations(annotations)
}

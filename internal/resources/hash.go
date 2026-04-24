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
	"crypto/sha256"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AnnotationSpecHash is the annotation key used to store the SHA-256 hash of
// the desired object state. The operator compares this against the live object
// to skip unnecessary API server writes.
const AnnotationSpecHash = "kubernaut.ai/spec-hash"

// SpecHash computes a SHA-256 hex digest of the meaningful content of a
// client.Object. Server-managed metadata (resourceVersion, UID,
// creationTimestamp, managedFields, generation) and the spec-hash annotation
// itself are excluded so the hash is stable across reconcile loops.
// OwnerReferences are included so that CR re-creation triggers re-adoption.
func SpecHash(obj client.Object) string {
	copy := obj.DeepCopyObject().(client.Object)
	stripServerMetadata(copy)
	data, err := json.Marshal(copy)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// stripServerMetadata zeroes out fields that the API server manages or that
// would cause the hash to change between reconcile loops without any
// meaningful spec drift.
func stripServerMetadata(obj client.Object) {
	obj.SetResourceVersion("")
	obj.SetUID("")
	obj.SetCreationTimestamp(metav1.Time{})
	obj.SetManagedFields(nil)
	obj.SetGeneration(0)
	obj.SetSelfLink("")
	obj.SetDeletionTimestamp(nil)
	obj.SetDeletionGracePeriodSeconds(nil)

	annotations := obj.GetAnnotations()
	if annotations != nil {
		delete(annotations, AnnotationSpecHash)
		if len(annotations) == 0 {
			obj.SetAnnotations(nil)
		} else {
			obj.SetAnnotations(annotations)
		}
	}
}

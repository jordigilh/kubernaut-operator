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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newConfigMap(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "ns"},
		Data:       data,
	}
}

func TestSpecHash_Stability(t *testing.T) {
	cm := newConfigMap(map[string]string{"key": "val"})
	h1 := SpecHash(cm)
	h2 := SpecHash(cm)
	if h1 != h2 {
		t.Fatalf("expected stable hash, got %s != %s", h1, h2)
	}
	if h1 == "" {
		t.Fatal("hash must not be empty")
	}
}

func TestSpecHash_Sensitivity(t *testing.T) {
	cm1 := newConfigMap(map[string]string{"key": "val1"})
	cm2 := newConfigMap(map[string]string{"key": "val2"})
	if SpecHash(cm1) == SpecHash(cm2) {
		t.Fatal("different data should produce different hashes")
	}
}

func TestSpecHash_IgnoresServerMetadata(t *testing.T) {
	base := newConfigMap(map[string]string{"key": "val"})
	baseHash := SpecHash(base)

	withRV := base.DeepCopy()
	withRV.ResourceVersion = "12345"
	if SpecHash(withRV) != baseHash {
		t.Fatal("resourceVersion must not affect hash")
	}

	withUID := base.DeepCopy()
	withUID.UID = types.UID("abc-123")
	if SpecHash(withUID) != baseHash {
		t.Fatal("UID must not affect hash")
	}

	withGen := base.DeepCopy()
	withGen.Generation = 42
	if SpecHash(withGen) != baseHash {
		t.Fatal("generation must not affect hash")
	}

	withMF := base.DeepCopy()
	withMF.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "test"}}
	if SpecHash(withMF) != baseHash {
		t.Fatal("managedFields must not affect hash")
	}
}

func TestSpecHash_IgnoresHashAnnotation(t *testing.T) {
	base := newConfigMap(map[string]string{"key": "val"})
	baseHash := SpecHash(base)

	withAnnotation := base.DeepCopy()
	withAnnotation.Annotations = map[string]string{AnnotationSpecHash: "old-hash"}
	if SpecHash(withAnnotation) != baseHash {
		t.Fatal("spec-hash annotation must not affect hash")
	}
}

func TestSpecHash_PreservesOtherAnnotations(t *testing.T) {
	cm1 := newConfigMap(map[string]string{"key": "val"})

	cm2 := cm1.DeepCopy()
	cm2.Annotations = map[string]string{"custom-annotation": "value"}

	if SpecHash(cm1) == SpecHash(cm2) {
		t.Fatal("non-spec-hash annotations should affect hash")
	}
}

func TestSpecHash_OwnerRefChange(t *testing.T) {
	cm1 := newConfigMap(map[string]string{"key": "val"})

	cm2 := cm1.DeepCopy()
	cm2.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "kubernaut.ai/v1alpha1",
			Kind:       "Kubernaut",
			Name:       "my-kubernaut",
			UID:        types.UID("new-uid"),
		},
	}

	if SpecHash(cm1) == SpecHash(cm2) {
		t.Fatal("ownerReference change must produce different hash for re-adoption")
	}
}

func TestSpecHash_DoesNotMutateOriginal(t *testing.T) {
	cm := newConfigMap(map[string]string{"key": "val"})
	cm.ResourceVersion = "999"
	cm.UID = "original-uid"
	cm.Annotations = map[string]string{AnnotationSpecHash: "old", "keep": "this"}

	_ = SpecHash(cm)

	if cm.ResourceVersion != "999" {
		t.Fatal("SpecHash must not mutate the original ResourceVersion")
	}
	if cm.UID != "original-uid" {
		t.Fatal("SpecHash must not mutate the original UID")
	}
	if cm.Annotations[AnnotationSpecHash] != "old" {
		t.Fatal("SpecHash must not mutate the original annotations")
	}
	if cm.Annotations["keep"] != "this" {
		t.Fatal("SpecHash must not lose other annotations from the original")
	}
}

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

var _ = Describe("SpecHash", func() {
	It("is stable for the same spec", func() {
		cm := newConfigMap(map[string]string{"key": "val"})
		h1 := SpecHash(cm)
		h2 := SpecHash(cm)
		Expect(h1).To(Equal(h2))
		Expect(h1).NotTo(BeEmpty())
	})

	It("differs when data differs", func() {
		cm1 := newConfigMap(map[string]string{"key": "val1"})
		cm2 := newConfigMap(map[string]string{"key": "val2"})
		Expect(SpecHash(cm1)).NotTo(Equal(SpecHash(cm2)))
	})

	It("ignores server metadata fields", func() {
		base := newConfigMap(map[string]string{"key": "val"})
		baseHash := SpecHash(base)

		withRV := base.DeepCopy()
		withRV.ResourceVersion = "12345"
		Expect(SpecHash(withRV)).To(Equal(baseHash))

		withUID := base.DeepCopy()
		withUID.UID = types.UID("abc-123")
		Expect(SpecHash(withUID)).To(Equal(baseHash))

		withGen := base.DeepCopy()
		withGen.Generation = 42
		Expect(SpecHash(withGen)).To(Equal(baseHash))

		withMF := base.DeepCopy()
		withMF.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "test"}}
		Expect(SpecHash(withMF)).To(Equal(baseHash))
	})

	It("ignores the spec-hash annotation", func() {
		base := newConfigMap(map[string]string{"key": "val"})
		baseHash := SpecHash(base)

		withAnnotation := base.DeepCopy()
		withAnnotation.Annotations = map[string]string{AnnotationSpecHash: "old-hash"}
		Expect(SpecHash(withAnnotation)).To(Equal(baseHash))
	})

	It("includes non-spec-hash annotations in the hash", func() {
		cm1 := newConfigMap(map[string]string{"key": "val"})

		cm2 := cm1.DeepCopy()
		cm2.Annotations = map[string]string{"custom-annotation": "value"}

		Expect(SpecHash(cm1)).NotTo(Equal(SpecHash(cm2)))
	})

	It("changes when owner references change", func() {
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

		Expect(SpecHash(cm1)).NotTo(Equal(SpecHash(cm2)))
	})

	It("does not mutate the original object", func() {
		cm := newConfigMap(map[string]string{"key": "val"})
		cm.ResourceVersion = "999"
		cm.UID = "original-uid"
		cm.Annotations = map[string]string{AnnotationSpecHash: "old", "keep": "this"}

		_ = SpecHash(cm)

		Expect(cm.ResourceVersion).To(Equal("999"))
		Expect(cm.UID).To(Equal(types.UID("original-uid")))
		Expect(cm.Annotations[AnnotationSpecHash]).To(Equal("old"))
		Expect(cm.Annotations["keep"]).To(Equal("this"))
	})
})

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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("Issue 514 pure ownership contract", func() {
	const otherIdentity = "other"
	var kn *kubernautv1alpha2.Kubernaut
	var object *corev1.ConfigMap
	BeforeEach(func() {
		kn = testKubernaut()
		kn.UID = types.UID("current-owner")
		object = &corev1.ConfigMap{ObjectMeta: ObjectMeta(kn, "managed", ComponentGateway)}
	})

	owner := func(uid types.UID) metav1.OwnerReference {
		return metav1.OwnerReference{APIVersion: kubernautv1alpha2.GroupVersion.String(), Kind: "Kubernaut", Name: "kubernaut", UID: uid, Controller: ptr.To(true)}
	}

	DescribeTable("UT-OWN-514-001 [AC-3, AC-6, SI-10; SOC2 CC6.1; ASVS v5.0.0-V8.3.1] deny ambiguous/foreign ownership without changing the object",
		func(mutate func(), controllerOwned bool) {
			mutate()
			before := object.DeepCopy()
			err := ResourceOwnershipError(kn, object, controllerOwned)
			Expect(errors.Is(err, ErrOwnershipConflict)).To(BeTrue(), "expected ownership conflict, got %v", err)
			Expect(object).To(Equal(before))
		},
		Entry("no marker or owner", func() { object.Labels = nil }, true),
		Entry("only managed-by", func() { object.Labels = map[string]string{"app.kubernetes.io/managed-by": "kubernaut-operator"} }, false),
		Entry("Helm manager", func() { object.Labels["app.kubernetes.io/managed-by"] = "Helm" }, true),
		Entry("wrong instance", func() { object.Labels["app.kubernetes.io/instance"] = otherIdentity }, false),
		Entry("wrong part-of", func() { object.Labels["app.kubernetes.io/part-of"] = otherIdentity }, true),
		Entry("wrong namespace marker", func() { object.Annotations = map[string]string{AnnotationOwnerNamespace: otherIdentity} }, false),
		Entry("partial provenance", func() { object.Annotations = map[string]string{AnnotationOwnerUID: "old"} }, false),
		Entry("different resource namespace", func() {
			object.Namespace = otherIdentity
			object.OwnerReferences = []metav1.OwnerReference{owner(kn.UID)}
		}, true),
		Entry("foreign owner kind", func() {
			ref := owner(kn.UID)
			ref.Kind = "Deployment"
			object.OwnerReferences = []metav1.OwnerReference{ref}
		}, true),
		Entry("foreign owner name", func() {
			ref := owner("old")
			ref.Name = otherIdentity
			object.OwnerReferences = []metav1.OwnerReference{ref}
		}, true),
		Entry("foreign owner group", func() {
			ref := owner(kn.UID)
			ref.APIVersion = "foreign.io/v1"
			object.OwnerReferences = []metav1.OwnerReference{ref}
		}, true),
		Entry("non-controller owner", func() {
			ref := owner(kn.UID)
			ref.Controller = ptr.To(false)
			object.OwnerReferences = []metav1.OwnerReference{ref}
		}, true),
		Entry("nil controller flag", func() {
			ref := owner(kn.UID)
			ref.Controller = nil
			object.OwnerReferences = []metav1.OwnerReference{ref}
		}, true),
		Entry("multiple owners", func() { object.OwnerReferences = []metav1.OwnerReference{owner(kn.UID), owner("foreign")} }, true),
		Entry("stale owner without marker", func() { object.OwnerReferences = []metav1.OwnerReference{owner("old")}; object.Labels = nil }, true),
		Entry("owner marker contradicts reference", func() {
			object.OwnerReferences = []metav1.OwnerReference{owner(kn.UID)}
			object.Annotations = map[string]string{AnnotationOwnerNamespace: kn.Namespace, AnnotationOwnerUID: "foreign"}
		}, true),
		Entry("owner on cluster/cross-namespace resource", func() { object.OwnerReferences = []metav1.OwnerReference{owner(kn.UID)} }, false),
		Entry("terminating object", func() { now := metav1.Now(); object.DeletionTimestamp = &now }, true),
	)

	DescribeTable("UT-OWN-514-002 [AC-6, CM-3; SOC2 CC8.1] accepts current ownership or the bounded complete-marker reinstall exception",
		func(mutate func(), controllerOwned bool) {
			mutate()
			before := object.DeepCopy()
			Expect(ResourceOwnershipError(kn, object, controllerOwned)).NotTo(HaveOccurred())
			Expect(object).To(Equal(before))
		},
		Entry("legacy full marker without controller", func() {}, true),
		Entry("legacy unowned full marker", func() {}, false),
		Entry("current controller without labels", func() { object.Labels = nil; object.OwnerReferences = []metav1.OwnerReference{owner(kn.UID)} }, true),
		Entry("current controller with full marker", func() { object.OwnerReferences = []metav1.OwnerReference{owner(kn.UID)} }, true),
		Entry("stale same-identity controller with explicit full marker", func() {
			object.OwnerReferences = []metav1.OwnerReference{owner("old")}
			object.Annotations = map[string]string{AnnotationOwnerNamespace: kn.Namespace, AnnotationOwnerUID: "old"}
		}, true),
		Entry("marked unowned reinstall", func() {
			object.Annotations = map[string]string{AnnotationOwnerNamespace: kn.Namespace, AnnotationOwnerUID: "old"}
		}, false),
	)

	It("UT-OWN-514-003 [CM-6, CM-8] stamps complete identity idempotently without erasing unrelated metadata", func() {
		object.Labels = map[string]string{"team": "sre"}
		object.Annotations = map[string]string{"external-controller": "keep"}
		StampOwnership(kn, object)
		Expect(object.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "kubernaut-operator"))
		Expect(object.Labels).To(HaveKeyWithValue("app.kubernetes.io/instance", kn.Name))
		Expect(object.Labels).To(HaveKeyWithValue("app.kubernetes.io/part-of", "kubernaut"))
		Expect(object.Labels).To(HaveKeyWithValue("team", "sre"))
		Expect(object.Annotations).To(HaveKeyWithValue(AnnotationOwnerNamespace, kn.Namespace))
		Expect(object.Annotations).To(HaveKeyWithValue(AnnotationOwnerUID, string(kn.UID)))
		Expect(object.Annotations).To(HaveKeyWithValue("external-controller", "keep"))
		before := object.DeepCopy()
		StampOwnership(kn, object)
		Expect(object).To(Equal(before))
		object.Labels = nil
		object.Annotations = nil
		StampOwnership(kn, object)
		Expect(object.Labels).To(Equal(CommonLabels(kn)))
	})
})

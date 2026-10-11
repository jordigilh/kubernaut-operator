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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Native policy ownership authorization", func() {
	var desired *unstructured.Unstructured
	BeforeEach(func() {
		desired = &unstructured.Unstructured{}
		desired.SetAPIVersion("cilium.io/v2")
		desired.SetKind("CiliumNetworkPolicy")
		desired.SetName("kubernaut-gateway")
		desired.SetNamespace("kubernaut-system")
		desired.SetLabels(map[string]string{
			ManagedPolicyLabel: "true", ManagedByLabel: "kubernaut-operator",
			PolicyNamespaceLabel: "kubernaut-system", "app.kubernetes.io/instance": "kubernaut", ProviderLabel: "cilium",
		})
	})

	DescribeTable("UT-OWN-514-005 [AC-3, AC-6; SOC2 CC6.6] rejects incomplete or contradictory provider policy identity",
		func(label, value string) {
			existing := desired.DeepCopy()
			labels := existing.GetLabels()
			labels[label] = value
			existing.SetLabels(labels)
			before := existing.DeepCopy()
			Expect(OwnershipError(existing, desired)).To(HaveOccurred())
			Expect(existing).To(Equal(before))
		},
		Entry("managed-policy missing", ManagedPolicyLabel, ""),
		Entry("managed-policy false", ManagedPolicyLabel, "false"),
		Entry("foreign manager", ManagedByLabel, "platform"),
		Entry("foreign namespace", PolicyNamespaceLabel, "other"),
		Entry("foreign instance", "app.kubernetes.io/instance", "other"),
		Entry("foreign provider", ProviderLabel, "calico"),
	)

	It("UT-OWN-514-005 [AC-6] rejects a foreign owner even with all five matching markers", func() {
		existing := desired.DeepCopy()
		existing.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "platform", UID: types.UID("platform-uid")}})
		Expect(OwnershipError(existing, desired)).To(MatchError(ContainSubstring("owner")))
	})

	It("UT-OWN-514-005 [AC-6] rejects terminating policies instead of reinstalling them", func() {
		existing := desired.DeepCopy()
		now := metav1.Now()
		existing.SetDeletionTimestamp(&now)
		Expect(OwnershipError(existing, desired)).To(MatchError(ContainSubstring("terminating")))
	})

	It("UT-OWN-514-005 [CM-3] accepts the complete provider identity without adding generic adoption markers", func() {
		existing := desired.DeepCopy()
		Expect(OwnershipError(existing, desired)).To(Succeed())
		Expect(existing).To(Equal(desired))
	})

	It("UT-OWN-514-005 [CM-3] allows a controller reference only to the same namespaced Kubernaut identity", func() {
		existing := desired.DeepCopy()
		existing.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "kubernaut.ai/v1alpha2", Kind: "Kubernaut", Name: "kubernaut", UID: types.UID("old-install-uid"), Controller: new(true)}})
		Expect(OwnershipError(existing, desired)).To(Succeed(), "complete markers authorize bounded same-identity reinstall")
		existing.SetNamespace("")
		Expect(OwnershipError(existing, desired)).To(MatchError(ContainSubstring("invalid-scope")))
		existing.SetNamespace(desired.GetNamespace())
		existing.SetOwnerReferences(append(existing.GetOwnerReferences(), metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "platform", UID: types.UID("platform-uid")}))
		Expect(OwnershipError(existing, desired)).To(MatchError(ContainSubstring("owners")))
	})
})

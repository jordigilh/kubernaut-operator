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
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("standard NetworkPolicy ownership authorization", func() {
	var desired *networkingv1.NetworkPolicy

	BeforeEach(func() {
		desired = &networkingv1.NetworkPolicy{
			TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "kubernaut-gateway", Namespace: "kubernaut-system",
				Labels: map[string]string{
					ManagedPolicyLabel: "true", ManagedByLabel: "kubernaut-operator",
					PolicyNamespaceLabel: "kubernaut-system", instanceLabel: "kubernaut",
				},
			},
		}
	})

	DescribeTable("UT-OWN-514-005 [AC-3, AC-6; SOC2 CC6.6; ASVS V4.1] rejects incomplete policy identity",
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
		Entry("foreign instance", instanceLabel, "other"),
	)

	It("UT-OWN-514-006 [AC-6; SOC2 CC6.6; ASVS V4.1] rejects foreign owners", func() {
		existing := desired.DeepCopy()
		existing.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "platform", UID: types.UID("platform-uid")}})
		Expect(OwnershipError(existing, desired)).To(MatchError(ContainSubstring("owner")))
	})

	It("UT-OWN-514-008 [CM-3; SOC2 CC8.1] accepts the complete operator-owned identity", func() {
		Expect(OwnershipError(desired.DeepCopy(), desired)).To(Succeed())
	})

	It("UT-OWN-514-007 [AC-6] rejects a terminating NetworkPolicy", func() {
		existing := desired.DeepCopy()
		now := metav1.Now()
		existing.SetDeletionTimestamp(&now)
		Expect(OwnershipError(existing, desired)).To(MatchError(ContainSubstring("terminating")))
	})

	It("UT-OWN-514-009 [CM-3] permits same-identity reinstall owner repair", func() {
		existing := desired.DeepCopy()
		existing.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: "kubernaut.ai/v1alpha2", Kind: "Kubernaut", Name: "kubernaut",
			UID: types.UID("old-install"), Controller: new(true),
		}})
		Expect(OwnershipError(existing, desired)).To(Succeed())
	})

	It("rejects an invalid owner scope or multiple owners", func() {
		existing := desired.DeepCopy()
		existing.SetNamespace("")
		existing.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "platform", UID: types.UID("platform")}})
		Expect(OwnershipError(existing, desired)).To(MatchError(ContainSubstring("invalid-scope")))
	})
})

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

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("administrator-owned external exposure", Label("unit"), func() {
	It("UT-EXP-565-STATUS-001 [CM-6, SI-4; SOC2 CC7.2; ASVS v5.0.0-V16.5.2]: reports internal exposure without RouteReady", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})

		Expect(r.finalizeDeployStatus(ctx, kn)).To(Succeed())
		Expect(findCondition(kn.Status.Conditions, "RouteReady")).To(BeNil(),
			"the operator must not claim an external Route readiness contract")

		exposure := findCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionExposureReady)
		Expect(exposure).NotTo(BeNil())
		Expect(exposure.Status).To(Equal(metav1.ConditionTrue))
		Expect(exposure.Reason).To(Equal(ReasonExposureInternal))
		Expect(exposure.Message).To(ContainSubstring("administrator"))
	})
})

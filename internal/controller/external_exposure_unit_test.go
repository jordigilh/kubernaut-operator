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
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("administrator-owned external exposure", Label("unit"), func() {
	It("UT-EXP-565-STATUS-001 [CM-6, SI-4; SOC2 CC7.2; ASVS v5.0.0-V16.5.2]: reports internal exposure without RouteReady", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})

		Expect(r.finalizeDeployStatus(ctx, kn)).To(Succeed())
		Expect(findCondition(kn.Status.Conditions, retiredConditionRouteReady)).To(BeNil(),
			"the operator must not claim an external Route readiness contract")

		exposure := findCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionExposureReady)
		Expect(exposure).NotTo(BeNil())
		Expect(exposure.Status).To(Equal(metav1.ConditionTrue))
		Expect(exposure.Reason).To(Equal(ReasonExposureInternal))
		Expect(exposure.Message).To(ContainSubstring("administrator"))
	})

	It("UT-EXP-574-STATUS-002 [CM-6, SI-4; SOC2 CC7.2; ASVS v5.0.0-V16.5.2]: removes a stale RouteReady condition written by a pre-#572 operator", func() {
		kn := newMinimalCR()
		kn.Status.Conditions = []metav1.Condition{{
			Type:               retiredConditionRouteReady,
			Status:             metav1.ConditionFalse,
			Reason:             "RouteDisabled",
			Message:            "operator-managed Routes are disabled",
			ObservedGeneration: 3,
		}}
		r := businessUnitReconciler(kn, interceptor.Funcs{})

		Expect(r.finalizeDeployStatus(ctx, kn)).To(Succeed())

		Expect(findCondition(kn.Status.Conditions, retiredConditionRouteReady)).To(BeNil(),
			"reconciliation must drop the retired RouteReady condition from upgraded objects")
		exposure := findCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionExposureReady)
		Expect(exposure).NotTo(BeNil())
		Expect(exposure.Status).To(Equal(metav1.ConditionTrue))
		Expect(exposure.Reason).To(Equal(ReasonExposureInternal))

		By("pruning again on a follow-up reconcile without the stale condition")
		Expect(r.finalizeDeployStatus(ctx, kn)).To(Succeed())
		Expect(findCondition(kn.Status.Conditions, retiredConditionRouteReady)).To(BeNil())
		Expect(findCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionExposureReady)).NotTo(BeNil())
	})

	It("UT-EXP-574-STATUS-003 [CM-6, SI-4; SOC2 CC7.2; ASVS v5.0.0-V16.5.2]: reports failed status writes without claiming the stale condition was removed", func() {
		kn := newMinimalCR()
		kn.Status.Conditions = []metav1.Condition{{
			Type:               retiredConditionRouteReady,
			Status:             metav1.ConditionFalse,
			Reason:             "RouteDisabled",
			Message:            "operator-managed Routes are disabled",
			ObservedGeneration: 3,
		}}
		kn.Status.Phase = kubernautv1alpha2.PhaseRunning
		r := businessUnitReconciler(kn, interceptor.Funcs{SubResourcePatch: func(context.Context, client.Client,
			string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
			return fmt.Errorf("injected status failure")
		}})

		Expect(r.finalizeDeployStatus(ctx, kn)).To(MatchError("injected status failure"))
		Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseRunning),
			"the in-memory status must roll back so a retry re-attempts the pruning")
		Expect(findCondition(kn.Status.Conditions, retiredConditionRouteReady)).NotTo(BeNil(),
			"the stale condition must remain pending until the status write succeeds")
	})
})

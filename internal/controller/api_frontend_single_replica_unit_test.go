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
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("API Frontend single-replica cleanup", Label("unit"), func() {
	It("reports an error when reading a legacy AF HPA fails", func() {
		kn := newMinimalCR()
		kn.UID = "unit-af-hpa-read-error"
		r := businessUnitReconciler(kn, interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return fmt.Errorf("simulated HPA read failure")
			},
		})

		err := r.pruneLegacyAPIFrontendHPAs(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("getting legacy api frontend HPA")))
	})

	It("reports an error when deleting an owned legacy AF HPA fails", func() {
		kn := newMinimalCR()
		kn.UID = "unit-af-hpa-delete-error"
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resources.ComponentAPIFrontend + "-hpa",
				Namespace: kn.Namespace,
			},
		}
		r := businessUnitReconciler(kn, interceptor.Funcs{
			Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
				return fmt.Errorf("simulated HPA delete failure")
			},
		})
		seedBusinessOwnedResources(r, kn, hpa)

		err := r.pruneLegacyAPIFrontendHPAs(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("deleting legacy api frontend HPA")))
	})

	It("propagates legacy AF HPA cleanup errors from deployWorkloads", func() {
		kn := newMinimalCR()
		kn.UID = "unit-af-hpa-deploy-workloads-error"
		r := businessUnitReconciler(kn, interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return fmt.Errorf("simulated HPA read failure")
			},
		})

		err := r.deployWorkloads(ctx, kn, kn, nil, telemetryMaterialRevisionSet{})
		Expect(err).To(MatchError(ContainSubstring("getting legacy api frontend HPA")))
	})

	It("merges desired metadata when the live metadata map is nil", func() {
		merged := mergePreservedMetadata(nil, map[string]string{"managed": "true"})
		Expect(merged).To(HaveKeyWithValue("managed", "true"))
	})
})

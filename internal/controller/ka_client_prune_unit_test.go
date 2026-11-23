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
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// #655 scope extension: the AIAnalysis→Kubernaut Agent channel is the
// AgentSession CRD (DD-AA-KA-001), so the kubernaut-agent-client-aianalysis
// RoleBinding is no longer rendered and must be pruned from existing
// installs. A failed prune must surface as an error, not be silently
// swallowed — otherwise the stale grant would linger undetected.
var _ = Describe("stale AIAnalysis KA client RoleBinding prune", Label("unit"), func() {
	It("reports an error when deleting the owned stale RoleBinding fails", func() {
		kn := newMinimalCR()
		kn.UID = "unit-ka-client-rb-delete-error"
		r := businessUnitReconciler(kn, interceptor.Funcs{
			Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
				if rb, ok := obj.(*rbacv1.RoleBinding); ok && rb.Name == "kubernaut-agent-client-aianalysis" {
					return fmt.Errorf("simulated KA client RoleBinding delete failure")
				}
				return nil
			},
		})
		seedBusinessOwnedResources(r, kn, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kubernaut-agent-client-aianalysis",
				Namespace: kn.Namespace,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     kn.Namespace + "-kubernaut-agent-client",
			},
		})

		err := r.deployNamespacedCoreRBAC(ctx, kn, kn)
		Expect(err).To(MatchError(ContainSubstring("removing stale aianalysis KA client RoleBinding")))
	})
})

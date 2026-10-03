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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("controller manager wiring", func() {
	It("starts the reconciler with discovered optional APIs", func() {
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: k8sClient.Scheme(),
			Metrics: metricsserver.Options{
				BindAddress: "0",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect((&KubernautReconciler{}).SetupWithManager(mgr)).To(Succeed())

		managerCtx, managerCancel := context.WithCancel(ctx)
		defer managerCancel()
		managerErrors := make(chan error, 1)
		go func() {
			managerErrors <- mgr.Start(managerCtx)
		}()

		Eventually(func() bool {
			return mgr.GetCache().WaitForCacheSync(managerCtx)
		}).WithTimeout(30 * time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())

		managerCancel()
		Eventually(managerErrors).WithTimeout(30 * time.Second).Should(Receive(BeNil()))
	})
})

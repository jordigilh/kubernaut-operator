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

package networkpolicy

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestNetworkPolicyE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting kubernaut-operator NetworkPolicy e2e suite (kind+Calico) -- #342 Phase 1\n")
	RunSpecs(t, "networkpolicy e2e suite")
}

var _ = BeforeSuite(func() {
	// buildKubernautCR() drives api/v1alpha1's ConvertTo(), which logs via
	// controller-runtime's log.FromContext -- silence it here rather than
	// leaving the default "log.SetLogger(...) was never called" stack
	// trace dump on every run of this standalone (non-controller) suite.
	logf.SetLogger(logr.Discard())

	ctx := context.Background()

	By("creating a throwaway kind cluster with the default CNI disabled")
	Expect(createKindCluster(ctx)).To(Succeed())

	By("installing pinned Calico v3.29.3 via the Tigera operator")
	Expect(installCalico(ctx)).To(Succeed())
})

var _ = AfterSuite(func() {
	deleteKindCluster(context.Background())
})

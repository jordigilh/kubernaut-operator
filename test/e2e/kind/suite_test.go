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

// Package kind contains the platform-neutral and native-provider E2E suite.
// The suite creates its own throwaway Kind cluster so it never depends on an
// OpenShift kubeconfig or on provider resources being present in the caller's
// cluster.
package kind

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck
)

func TestKindE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting kubernaut-operator Kind provider E2E suite\n") //nolint:errcheck
	RunSpecs(t, "kind provider e2e suite")
}

var _ = BeforeSuite(func() {
	var err error
	configuredProvider, err = providerFromEnvironment()
	Expect(err).NotTo(HaveOccurred())

	clusterContext = kubeContext()
	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	By("creating a throwaway Kind cluster")
	Expect(createKindCluster(ctx, configuredProvider)).To(Succeed())

	By("waiting for the Kubernetes control plane")
	Expect(waitForCluster(ctx, configuredProvider)).To(Succeed())

	switch configuredProvider {
	case providerCilium:
		By("installing the pinned Cilium provider")
		Expect(installCilium(ctx)).To(Succeed())
	case providerCalico:
		By("installing the pinned Calico provider")
		Expect(installCalico(ctx)).To(Succeed())
	}

	By("waiting for Kubernetes nodes")
	Expect(waitForNodes(ctx)).To(Succeed())

	By("waiting for cluster DNS")
	Expect(waitForDNS(ctx)).To(Succeed())

	By("loading the operator image into Kind")
	Expect(loadOperatorImage(ctx)).To(Succeed())

	By("loading the Kubernaut infrastructure images into Kind")
	Expect(loadInfrastructureImages(ctx)).To(Succeed())

	By("installing the operator from the production manifests")
	Expect(installOperator(ctx)).To(Succeed())

	By("installing the Kubernaut E2E prerequisites")
	Expect(ensureKubernautInfrastructure(ctx)).To(Succeed())
})

var _ = AfterSuite(func() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupProbeWorkloads(ctx)
	deleteKindCluster(ctx)
})

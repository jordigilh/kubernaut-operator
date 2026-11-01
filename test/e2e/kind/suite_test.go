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

// Package kind contains the portable Kubernetes NetworkPolicy E2E suite. The
// suite creates its own throwaway Kind cluster so it never depends on an
// OpenShift kubeconfig or CNI-specific resources in the caller's cluster.
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
	_, _ = fmt.Fprintf(
		GinkgoWriter, "Starting kubernaut-operator Kind portable NetworkPolicy E2E suite\n",
	) //nolint:errcheck
	RunSpecs(t, "kind portable NetworkPolicy e2e suite")
}

var _ = BeforeSuite(func() {
	var err error
	configuredTLS, err = tlsSourceFromEnvironment()
	Expect(err).NotTo(HaveOccurred())

	clusterContext = kubeContext()
	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	By("creating a throwaway Kind cluster")
	Expect(createKindCluster(ctx)).To(Succeed())

	By("waiting for the Kubernetes control plane")
	Expect(waitForCluster(ctx)).To(Succeed())

	By("waiting for Kubernetes nodes")
	Expect(waitForNodes(ctx)).To(Succeed())

	By("waiting for cluster DNS")
	Expect(waitForDNS(ctx)).To(Succeed())

	if configuredTLS == tlsCertManager {
		By("installing the pinned cert-manager source")
		Expect(installCertManager(ctx)).To(Succeed())
	}

	By("loading the operator image into Kind")
	Expect(loadOperatorImage(ctx)).To(Succeed())

	By("loading the contract and dependency images into Kind")
	Expect(loadInfrastructureImages(ctx)).To(Succeed())

	By("installing the operator from the Helm chart")
	Expect(installOperator(ctx)).To(Succeed())

	By("installing the Kubernaut E2E prerequisites")
	Expect(ensureKubernautInfrastructure(ctx)).To(Succeed())
	if configuredTLS == tlsManualAdmin {
		By("creating administrator-owned manual TLS fixtures before the CR")
		Expect(ensureManualAdminTLSFixtures(ctx)).To(Succeed())
	}
	if configuredTLS == tlsCertManager {
		By("creating cert-manager-issued runtime TLS material")
		Expect(ensureCertManagerRuntimeTLS(ctx)).To(Succeed())
	}
})

var _ = AfterSuite(func() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupProbeWorkloads(ctx)
	deleteKindCluster(ctx)
})

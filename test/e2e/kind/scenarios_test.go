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

package kind

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("Kind operator journey and native provider contract", Ordered, func() {
	var ctx context.Context

	BeforeAll(func() {
		ctx = context.Background()
		Expect(ensureProbeWorkloads(ctx)).To(Succeed())
		Expect(applyKubernautCR(ctx)).To(Succeed())
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			collectDiagnostics(ctx)
			collectProbeDiagnostics(ctx)
		}
	})

	It(
		"E2E-TLS-GAP-001 / E2E-TLS-CERTMANAGER-001 [SC-8, SC-13, SC-17, SI-4; "+
			"SOC2 CC6, CC7, A1; ASVS v5.0.0-V12.1.3, v5.0.0-V13.2.1, v5.0.0-V16.5.2] drives the CR through "+
			"validation, migration, deployment, "+
			"TLS source, and provider status",
		func() {
			By("waiting for the real operator to validate the CR")
			Eventually(func(g Gomega) {
				g.Expect(kubernautCondition(ctx, "BYOValidated")).To(Equal("True"))
			}).Should(Succeed())

			By("waiting for the operator-owned migration to complete")
			Eventually(func(g Gomega) {
				g.Expect(kubernautCondition(ctx, "MigrationComplete")).To(Equal("True"))
			}).Should(Succeed())

			By("waiting for service manifests and native policy reconciliation")
			Eventually(func(g Gomega) {
				g.Expect(kubernautCondition(ctx, "ServicesDeployed")).To(Equal("True"))
			}).Should(Succeed())

			By("waiting for the operator to report all managed workloads running")
			Eventually(func(g Gomega) {
				g.Expect(kubernautPhase(ctx)).To(Equal(string(kubernautv1alpha2.PhaseRunning)))
			}).Should(Succeed())

			By("verifying the selected runtime TLS source and non-empty webhook trust")
			Expect(kubernautCondition(ctx, "TLSReady")).To(Equal("True"))
			Expect(secretTLSMaterialPresent(ctx, "kubernaut-internal-ca")).To(BeTrue())
			Expect(webhookCABundlePresent(ctx, "mutating")).To(BeTrue())
			Expect(webhookCABundlePresent(ctx, "validating")).To(BeTrue())
			if configuredTLS == tlsCertManager {
				Eventually(func(g Gomega) {
					for _, name := range []string{
						"kubernaut-internal-ca",
						"gateway-tls",
						"datastorage-tls",
						"kubernautagent-tls",
						"apifrontend-tls",
						"authwebhook-tls",
					} {
						owner, err := secretOwnerKind(ctx, name)
						g.Expect(err).NotTo(HaveOccurred(), name)
						g.Expect(owner).To(Equal("Certificate"), name)
					}
				}).Should(Succeed())
			}

			if configuredProvider == providerGeneric {
				Expect(kubernautCondition(ctx, "ProviderDetected")).To(Equal("False"))
				Expect(kubernautCondition(ctx, "ProviderPolicyReady")).To(Equal("False"))
				Expect(noManagedPolicies(ctx)).To(Succeed())
				return
			}

			Expect(kubernautCondition(ctx, "ProviderDetected")).To(Equal("True"))
			Expect(kubernautCondition(ctx, "ProviderPolicyReady")).To(Equal("True"))
			Eventually(func(g Gomega) {
				g.Expect(managedNativePolicyExists(ctx)).To(Succeed())
			}).Should(Succeed())
			Expect(ensureNoRawNetworkPolicy(ctx)).To(Succeed())
		})

	It("proves provider enforcement through the reconciled policy", func() {
		if configuredProvider == providerGeneric {
			Eventually(func(g Gomega) {
				allowed, err := probeHTTP(ctx, "probe-target")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(allowed).To(BeTrue(), "generic Kubernetes must preserve ordinary pod connectivity")
			}).Should(Succeed())
			Eventually(func(g Gomega) {
				allowed, err := probeHTTPFromRole(ctx, "probe-target", probeUnmanagedClientRole)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(allowed).To(BeTrue(), "generic Kubernetes must not receive an implicit raw policy")
			}).Should(Succeed())
			return
		}

		Expect(kubernautCondition(ctx, "ProviderPolicyReady")).To(Equal("True"))
		Expect(ensureNoRawNetworkPolicy(ctx)).To(Succeed())

		Eventually(func(g Gomega) {
			allowed, err := probeHTTP(ctx, "probe-target")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(allowed).To(BeTrue(), "a provider-managed endpoint must accept traffic from a managed endpoint")
		}).Should(Succeed())
		Eventually(func(g Gomega) {
			allowed, err := probeHTTPFromRole(ctx, "probe-target", probeUnmanagedClientRole)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(allowed).To(BeFalse(), "the reconciled provider policy must deny an unmanaged source")
		}).Should(Succeed())
	})

	It("E2E-TLS-CERTMANAGER-002 [SC-8, SC-12, SC-13, SI-4; SOC2 CC7, A1; "+
		"ASVS v5.0.0-V11.1.1, v5.0.0-V11.1.2, v5.0.0-V12.1.1, v5.0.0-V16.5.2] rotates a cert-manager leaf "+
		"without losing operator trust", func() {
		if configuredTLS != tlsCertManager {
			return
		}

		before, err := secretTLSCertificate(ctx, "gateway-tls")
		Expect(err).NotTo(HaveOccurred())
		By("requesting a cert-manager reissuance through the Certificate spec")
		Expect(patchCertificateForRotation(ctx, "gateway")).To(Succeed())

		Eventually(func(g Gomega) {
			current, getErr := secretTLSCertificate(ctx, "gateway-tls")
			g.Expect(getErr).NotTo(HaveOccurred())
			g.Expect(current).NotTo(Equal(before))
		}).Should(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(kubernautCondition(ctx, "TLSReady")).To(Equal("True"))
			owner, ownerErr := secretOwnerKind(ctx, "gateway-tls")
			g.Expect(ownerErr).NotTo(HaveOccurred())
			g.Expect(owner).To(Equal("Certificate"))
			g.Expect(webhookCABundlePresent(ctx, "mutating")).To(BeTrue())
			g.Expect(webhookCABundlePresent(ctx, "validating")).To(BeTrue())
		}).Should(Succeed())
	})

	It("preserves a user-owned provider policy while cleaning up the CR journey", func() {
		if configuredProvider != providerGeneric {
			Expect(applyUnmanagedNativePolicy(ctx)).To(Succeed())
			Expect(unmanagedNativePolicyExists(ctx)).To(Succeed())
		}

		By("deleting the Kubernaut CR through its finalizer path")
		Expect(deleteKubernautCR(ctx)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(noManagedPolicies(ctx)).To(Succeed())
			g.Expect(ensureNoRawNetworkPolicy(ctx)).To(Succeed())
			if configuredProvider != providerGeneric {
				g.Expect(unmanagedNativePolicyExists(ctx)).To(Succeed())
			}
		}).Should(Succeed())
	})

	AfterAll(func() {
		Expect(deleteProbeWorkloads(context.Background())).To(Succeed())
	})
})

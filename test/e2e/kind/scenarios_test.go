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
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/jordigilh/kubernaut-operator/internal/policy"
)

var _ = Describe("Kind platform and native provider contract", Ordered, func() {
	var ctx context.Context

	BeforeAll(func() {
		ctx = context.Background()
		Expect(ensureProbeWorkloads(ctx)).To(Succeed())
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			collectDiagnostics(ctx)
			collectProbeDiagnostics(ctx)
		}
	})

	It("detects only the active supported provider and never uses a raw policy fallback", func() {
		detection, err := liveDetection(ctx, policyProviderForTest())
		Expect(err).NotTo(HaveOccurred())

		intent, err := intentForProbe()
		Expect(err).NotTo(HaveOccurred())
		objects, err := policy.Render(detection, intent)
		Expect(err).NotTo(HaveOccurred())

		switch configuredProvider {
		case providerGeneric:
			Expect(detection.Ready).To(BeFalse())
			Expect(detection.Provider).To(Equal(policy.ProviderNone))
			Expect(objects).To(BeEmpty())
		case providerCilium:
			Expect(detection.Ready).To(BeTrue())
			Expect(detection.Provider).To(Equal(policy.ProviderCilium))
		case providerCalico:
			Expect(detection.Ready).To(BeTrue())
			Expect(detection.Provider).To(Equal(policy.ProviderCalico))
		}
		Expect(ensureNoRawNetworkPolicy(ctx)).To(Succeed())
	})

	It("fails closed for unsupported, inactive, and incompatible detections", func() {
		intent, err := intentForProbe()
		Expect(err).NotTo(HaveOccurred())

		unsupported := policy.Detect(policy.DiscoverySnapshot{}, policy.Provider("Unsupported"))
		Expect(unsupported.Ready).To(BeFalse())
		Expect(unsupported.Reason).To(Equal(policy.ReasonUnsupportedProvider))
		objects, err := policy.Render(unsupported, intent)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(BeEmpty())

		inactive := policy.Detect(policy.DiscoverySnapshot{Candidates: map[policy.Provider]policy.ProviderSnapshot{
			policy.ProviderCilium: {
				Provider:                   policy.ProviderCilium,
				APIAvailable:               true,
				Active:                     false,
				SchemaValid:                true,
				APIServerIdentityAvailable: true,
				RequiredGVKs:               policy.RequiredGVKs(policy.ProviderCilium),
			},
		}}, policy.ProviderCilium)
		Expect(inactive.Ready).To(BeFalse())
		Expect(inactive.Reason).To(Equal(policy.ReasonNoActiveInstallation))
		objects, err = policy.Render(inactive, intent)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(BeEmpty())

		outOfRange := policy.Detect(policy.DiscoverySnapshot{Candidates: map[policy.Provider]policy.ProviderSnapshot{
			policy.ProviderCalico: {
				Provider:                   policy.ProviderCalico,
				APIAvailable:               true,
				Active:                     true,
				Version:                    "3.30.9",
				SchemaValid:                true,
				APIServerIdentityAvailable: true,
				RequiredGVKs:               policy.RequiredGVKs(policy.ProviderCalico),
			},
		}}, policy.ProviderCalico)
		Expect(outOfRange.Ready).To(BeFalse())
		Expect(outOfRange.Reason).To(Equal(policy.ReasonUnsupportedVersion))
		objects, err = policy.Render(outOfRange, intent)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(BeEmpty())

		invalidGVK := schema.GroupVersionKind{Group: "example.invalid", Version: "v1", Kind: "Policy"}
		Expect(policy.ProviderForGVK(invalidGVK)).To(Equal(policy.ProviderNone))
	})

	It("submits the native policy and proves the provider enforcement boundary", func() {
		detection, err := liveDetection(ctx, policyProviderForTest())
		Expect(err).NotTo(HaveOccurred())
		intent, err := intentForProbe()
		Expect(err).NotTo(HaveOccurred())
		objects, err := policy.Render(detection, intent)
		Expect(err).NotTo(HaveOccurred())

		if configuredProvider == providerGeneric {
			Expect(objects).To(BeEmpty())
			Eventually(func(g Gomega) {
				allowed, probeErr := probeHTTP(ctx, "probe-target")
				g.Expect(probeErr).NotTo(HaveOccurred())
				g.Expect(allowed).To(BeTrue(), "generic Kind must preserve ordinary pod connectivity")
			}).Should(Succeed())
			Eventually(func(g Gomega) {
				allowed, probeErr := probeHTTP(ctx, "probe-blocked")
				g.Expect(probeErr).NotTo(HaveOccurred())
				g.Expect(allowed).To(BeTrue(), "generic Kind must not receive an implicit raw policy")
			}).Should(Succeed())
			return
		}

		Expect(objects).To(HaveLen(1))
		Expect(applyNativePolicies(ctx, objects)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(managedNativePolicyExists(ctx)).To(Succeed())
		}).Should(Succeed())
		Expect(ensureNoRawNetworkPolicy(ctx)).To(Succeed())

		Eventually(func(g Gomega) {
			allowed, probeErr := probeHTTP(ctx, "probe-target")
			g.Expect(probeErr).NotTo(HaveOccurred())
			g.Expect(allowed).To(BeTrue(), "managed endpoints must be able to communicate")
		}).Should(Succeed())
		Eventually(func(g Gomega) {
			allowed, probeErr := probeHTTP(ctx, "probe-blocked")
			g.Expect(probeErr).NotTo(HaveOccurred())
			g.Expect(allowed).To(BeFalse(), "unmanaged endpoints must remain outside the allow-list")
		}).Should(Succeed())
	})

	It("cleans up only managed native policy objects", func() {
		if configuredProvider != providerGeneric {
			Expect(applyUnmanagedNativePolicy(ctx)).To(Succeed())
			Expect(unmanagedNativePolicyExists(ctx)).To(Succeed())
		}
		Expect(deleteManagedNativePolicies(ctx)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(noManagedPolicies(ctx)).To(Succeed())
			g.Expect(ensureNoRawNetworkPolicy(ctx)).To(Succeed())
			if configuredProvider != providerGeneric {
				g.Expect(unmanagedNativePolicyExists(ctx)).To(Succeed())
			}
		}).Should(Succeed())
	})

	AfterAll(func() {
		cleanupCtx := context.Background()
		Expect(deleteProbeNamespace(cleanupCtx)).To(Succeed())
	})
})

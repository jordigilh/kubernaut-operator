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
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("Kind operator journey and portable NetworkPolicy contract", Ordered, func() {
	var ctx context.Context
	var provisioned, namespaces []ownershipWitness

	BeforeAll(func() {
		ctx = context.Background()
		Expect(ensureProbeWorkloads(ctx)).To(Succeed())
		var err error
		provisioned, err = captureProvisioningWitnesses(ctx)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			collectDiagnostics(ctx)
			collectProbeDiagnostics(ctx)
		}
	})

	It("E2E-OWN-514-001 [AC-3, AC-6, CM-3, SI-4; SOC2 CC6.1, CC6.6, CC7.2, CC8.1; "+
		"ASVS v5.0.0-V8.3.1, v5.0.0-V16.2.1] preserves administrator resources across failed install, "+
		"recovery, upgrade, uninstall and marked reinstall", func() {
		By("creating administrator resources before the real CR")
		witnesses, err := createOwnershipConflicts(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(deleteOwnershipConflicts(ctx)).To(Succeed()) })
		Expect(applyKubernautCR(ctx)).To(Succeed())
		Eventually(func(g Gomega) {
			kn, getErr := ownershipObject(ctx, "kubernaut", "kubernaut", kubernautNamespace)
			g.Expect(getErr).NotTo(HaveOccurred())
			encoded, encodeErr := kn.MarshalJSON()
			g.Expect(encodeErr).NotTo(HaveOccurred())
			g.Expect(string(encoded)).To(ContainSubstring("ownership conflict"))
			g.Expect(kubernautCondition(ctx, "CRDsInstalled")).To(Equal("False"))
			events, eventErr := kubectl(ctx, "get", "events", "-n", kubernautNamespace,
				"--field-selector=reason=OwnershipConflict", "-o", "name")
			g.Expect(eventErr).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(events)).NotTo(BeEmpty())
			g.Expect(assertOwnershipWitnesses(ctx, witnesses)).To(Succeed())
		}).Should(Succeed())

		By("uninstalling the failed CR without destroying the conflicting resources")
		Expect(deleteKubernautCR(ctx)).To(Succeed())
		Expect(assertOwnershipWitnesses(ctx, witnesses)).To(Succeed())
		Expect(assertProvisioningWitnesses(ctx, provisioned)).To(Succeed())
		_, err = kubectl(ctx, "delete", "secret", "datastorage-db-secret", "-n", kubernautNamespace)
		Expect(err).NotTo(HaveOccurred())
		witnesses = witnesses[1:]
		if configuredTLS == tlsManualAdmin {
			Expect(ensureManualTLSWebhookFixtures(ctx, activeManualTLSSelection)).To(Succeed())
		}
		Expect(applyKubernautCR(ctx)).To(Succeed())
		Expect(completeMigrationJob(ctx)).To(Succeed())
		Eventually(kubernautPhase).WithArguments(ctx).Should(Equal(string(kubernautv1alpha2.PhaseRunning)))
		Expect(assertOwnershipWitnesses(ctx, witnesses)).To(Succeed())
		namespaces, err = captureOwnershipWitnesses(ctx, []ownershipWitness{
			{resource: "namespace", name: kubernautNamespace},
			{resource: "namespace", name: workflowNamespaceName},
		})
		Expect(err).NotTo(HaveOccurred())
		content, err := createWorkflowOwnershipContent(ctx)
		Expect(err).NotTo(HaveOccurred())
		provisioned = append(provisioned, content...)
		old, err := ownershipObject(ctx, "kubernaut", "kubernaut", kubernautNamespace)
		Expect(err).NotTo(HaveOccurred())
		leftover, err := managedOwnershipRole(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(leftover.GetAnnotations()).To(HaveKeyWithValue("kubernaut.ai/owner-uid", string(old.GetUID())))

		By("upgrading a real CR and waiting for the new generation to reconcile")
		_, err = kubectl(ctx, "patch", "kubernaut", "kubernaut", "-n", kubernautNamespace, "--type=merge", "-p",
			`{"spec":{"workflowExecution":{"cooldownPeriod":"2m"}}}`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			g.Expect(ownershipGenerationReady(ctx)).To(Succeed())
		}).Should(Succeed())

		By("uninstalling successfully while preserving foreign ingress and partially marked RBAC")
		Expect(deleteKubernautCR(ctx)).To(Succeed())
		Expect(assertOwnershipWitnesses(ctx, witnesses)).To(Succeed())
		Expect(assertRetainedNamespaces(ctx, namespaces)).To(Succeed())
		Expect(assertProvisioningWitnesses(ctx, provisioned)).To(Succeed())
		Eventually(func(g Gomega) {
			output, getErr := kubectl(ctx, "get", "deployment", "workflowexecution-controller", "-n", kubernautNamespace,
				"--ignore-not-found", "-o", "name")
			g.Expect(getErr).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(output)).To(BeEmpty())
		}).Should(Succeed())

		By("recreating a deliberately marked leftover and proving bounded new-UID reinstall repair")
		leftover.SetUID("")
		leftover.SetResourceVersion("")
		leftover.SetManagedFields(nil)
		leftover.SetCreationTimestamp(metav1.Time{})
		Expect(applyYAML(ctx, leftover)).To(Succeed())
		if configuredTLS == tlsManualAdmin {
			Expect(ensureManualTLSWebhookFixtures(ctx, activeManualTLSSelection)).To(Succeed())
		}
		Expect(applyKubernautCR(ctx)).To(Succeed())
		Expect(completeMigrationJob(ctx)).To(Succeed())
		Eventually(kubernautPhase).WithArguments(ctx).Should(Equal(string(kubernautv1alpha2.PhaseRunning)))
		fresh, err := ownershipObject(ctx, "kubernaut", "kubernaut", kubernautNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(fresh.GetUID()).NotTo(Equal(old.GetUID()))
		repaired, err := ownershipObject(ctx, "clusterrole", leftover.GetName(), "")
		Expect(err).NotTo(HaveOccurred())
		Expect(repaired.GetAnnotations()).To(HaveKeyWithValue("kubernaut.ai/owner-uid", string(fresh.GetUID())))
		Expect(assertOwnershipWitnesses(ctx, witnesses)).To(Succeed())
		Expect(assertRetainedNamespaces(ctx, namespaces)).To(Succeed())
		Expect(assertProvisioningWitnesses(ctx, provisioned)).To(Succeed())
	})

	It(
		"E2E-MON-424-001 / E2E-TLS-GAP-001 / E2E-TLS-CERTMANAGER-001 / "+
			"E2E-AUDIT-209-001 / "+
			"E2E-TLS-MANUAL-001 / E2E-TLS-ADMIN-001 / E2E-TLS-CLEANUP-001 [SC-8, SC-13, SC-17, SI-4; "+
			"SOC2 CC6, CC7, A1; ASVS v5.0.0-V12.1.3, v5.0.0-V13.2.1, v5.0.0-V16.5.2] drives the CR through "+
			"validation, migration, deployment, "+
			"TLS source, and NetworkPolicy status",
		func() {
			By("waiting for the real operator to validate the CR")
			Eventually(func(g Gomega) {
				g.Expect(kubernautCondition(ctx, "BYOValidated")).To(Equal(conditionTrue))
			}).Should(Succeed())

			By("injecting completion for the contract migration Job")
			Expect(completeMigrationJob(ctx)).To(Succeed())

			By("waiting for the operator-owned migration to complete")
			Eventually(func(g Gomega) {
				g.Expect(kubernautCondition(ctx, "MigrationComplete")).To(Equal(conditionTrue))
			}).Should(Succeed())

			By("waiting for service manifests and standard NetworkPolicy reconciliation")
			Eventually(func(g Gomega) {
				g.Expect(kubernautCondition(ctx, "ServicesDeployed")).To(Equal(conditionTrue))
			}).Should(Succeed())

			By("verifying the live audit HMAC Secret, ConfigMap, and Deployment projection")
			finalizer, err := kubectl(ctx, "get", "secret", resources.DataStorageAuditHashKeySecretName,
				"-n", kubernautNamespace, "-o", "jsonpath={.metadata.finalizers[0]}")
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(finalizer)).To(Equal(resources.DataStorageAuditHashKeyFinalizer))
			stateFinalizer, err := kubectl(ctx, "get", "configmap", resources.DataStorageAuditHashKeyStateConfigMapName,
				"-n", kubernautNamespace, "-o", "jsonpath={.metadata.finalizers[0]}")
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(stateFinalizer)).To(Equal(resources.DataStorageAuditHashKeyFinalizer))
			auditConfig, err := kubectl(ctx, "get", "configmap", "datastorage-config", "-n", kubernautNamespace,
				"-o", "jsonpath={.data.config\\.yaml}")
			Expect(err).NotTo(HaveOccurred())
			Expect(auditConfig).To(ContainSubstring("hashKeySecretsFile: /etc/datastorage/secrets/audit-hmac-key.yaml"))
			deployment, err := kubectl(ctx, "get", "deployment",
				resources.DeploymentName(resources.ComponentDataStorage), "-n", kubernautNamespace, "-o", "yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment).To(ContainSubstring("audit-hmac-key.yaml"))
			Expect(deployment).To(ContainSubstring("readOnly: true"))

			By("waiting for the operator to report all managed workloads running")
			Eventually(func(g Gomega) {
				g.Expect(kubernautPhase(ctx)).To(Equal(string(kubernautv1alpha2.PhaseRunning)))
			}).Should(Succeed())

			By("verifying the selected runtime TLS source and non-empty webhook trust")
			Expect(kubernautCondition(ctx, "TLSReady")).To(Equal(conditionTrue))
			Expect(secretTLSMaterialPresent(ctx, defaultInternalCASecretName)).To(BeTrue())
			Expect(selectedTrustBundlePresent(ctx)).To(BeTrue())
			Expect(webhookCABundlePresent(ctx, "mutating")).To(BeTrue())
			Expect(webhookCABundlePresent(ctx, "validating")).To(BeTrue())
			caKey, err := tlsProbeCAKey()
			Expect(err).NotTo(HaveOccurred())
			Expect(verifyTLSWorkloadTrust(ctx, caKey)).To(Succeed())

			switch configuredTLS {
			case tlsManualAdmin:
				By("proving manual TLS material is administrator-owned and unchanged")
				Expect(assertManualTLSFixtureUnchanged(ctx, activeManualTLSSelection)).To(Succeed())
				fixture, err := activeManualTLSFixture()
				Expect(err).NotTo(HaveOccurred())
				Expect(assertManualTLSWebhookBundlesUnchanged(ctx, fixture)).To(Succeed())
				Expect(webhookOpenShiftCAInjectionPresent(ctx, "mutating")).To(BeFalse())
				Expect(webhookOpenShiftCAInjectionPresent(ctx, "validating")).To(BeFalse())
				Expect(exerciseManualAdminAliases(ctx)).To(Succeed())
			}

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

			Eventually(func(g Gomega) {
				g.Expect(kubernautCondition(ctx, "NetworkPolicyReady")).To(Equal(conditionTrue))
			}).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(managedNativePolicyExists(ctx)).To(Succeed())
			}).Should(Succeed())
			Expect(ensureManagedPoliciesHaveNoEgress(ctx)).To(Succeed())
		})

	It(
		"E2E-KA-527-001 [CM-2, CM-3, CM-6, SA-11, SI-4; SOC2 CC7.2, CC8.1; "+
			"ASVS v5.0.0-V2.2.1, v5.0.0-V14.2.4, v5.0.0-V16.5.2] reconciles the live KA evidence handoff "+
			"default and explicit override",
		func() {
			assertLiveKAConfigOverride(ctx,
				"maxEvidenceHandoffBytes: 262144",
				`{"spec":{"kubernautAgent":{"workflowDiscovery":{"maxEvidenceHandoffBytes":131072}}}}`,
				"maxEvidenceHandoffBytes: 131072",
			)
		},
	)

	It(
		"E2E-KA-567-001 [CM-2, CM-3, CM-6, SA-11, SI-4; SOC2 CC7.2, CC8.1; "+
			"ASVS v5.0.0-V2.2.1, v5.0.0-V14.2.4, v5.0.0-V16.5.2] reconciles the live KA investigation "+
			"concurrency default and explicit override",
		func() {
			assertLiveKAConfigOverride(ctx,
				"maxConcurrentInvestigations: 50",
				`{"spec":{"kubernautAgent":{"session":{"maxConcurrentInvestigations":75}}}}`,
				"maxConcurrentInvestigations: 75",
			)
		},
	)

	It(
		"E2E-TLS-FAIL-CLOSED-001 [SC-8, SC-13, SI-4, SI-10; SOC2 CC6, CC7; "+
			"ASVS v5.0.0-V12.2.1, v5.0.0-V16.5.2] refuses invalid administrator "+
			"material without a plaintext fallback",
		func() {
			if configuredTLS != tlsManualAdmin {
				return
			}

			fixture, err := activeManualTLSFixture()
			Expect(err).NotTo(HaveOccurred())
			corrupt, err := corruptManualTLSSecret(fixture, "gateway")
			Expect(err).NotTo(HaveOccurred())
			By("invalidating an administrator-owned serving certificate")
			Expect(applyYAML(ctx, corrupt)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(kubernautCondition(ctx, "TLSReady")).To(Equal("False"))
			}).Should(Succeed())
			Expect(serviceHasNoPlaintextPort(ctx, "data-storage-service")).To(BeTrue())

			By("restoring the administrator-owned material")
			Expect(applyYAML(ctx, fixture.secrets[fixture.serviceTLSSecretNames["gateway"]])).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(kubernautCondition(ctx, "TLSReady")).To(Equal(conditionTrue))
				g.Expect(assertManualTLSFixtureUnchanged(ctx, activeManualTLSSelection)).To(Succeed())
				fixture, fixtureErr := activeManualTLSFixture()
				g.Expect(fixtureErr).NotTo(HaveOccurred())
				g.Expect(assertManualTLSWebhookBundlesUnchanged(ctx, fixture)).To(Succeed())
			}).Should(Succeed())
		},
	)

	It("proves the standard policy is applied without claiming dataplane enforcement", func() {
		Expect(kubernautCondition(ctx, "NetworkPolicyReady")).To(Equal(conditionTrue))
		Expect(managedNativePolicyExists(ctx)).To(Succeed())
		Expect(ensureManagedPoliciesHaveNoEgress(ctx)).To(Succeed())
		By("proving the Kubernetes API client remains usable")
		_, err := kubectl(ctx, "get", "kubernaut", "kubernaut", "-n", kubernautNamespace, "-o", "name")
		Expect(err).NotTo(HaveOccurred())
	})

	It("E2E-POLICY-PORTABLE-001 [AC-3, SC-7, SI-4; SOC2 CC6.1, CC7.2; "+
		"ASVS V4.1, V4.2] does not generate egress restrictions", func() {
		Expect(kubernautCondition(ctx, "NetworkPolicyReady")).To(Equal(conditionTrue))
		Expect(ensureManagedPoliciesHaveNoEgress(ctx)).To(Succeed())
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
			g.Expect(kubernautCondition(ctx, "TLSReady")).To(Equal(conditionTrue))
			owner, ownerErr := secretOwnerKind(ctx, "gateway-tls")
			g.Expect(ownerErr).NotTo(HaveOccurred())
			g.Expect(owner).To(Equal("Certificate"))
			g.Expect(webhookCABundlePresent(ctx, "mutating")).To(BeTrue())
			g.Expect(webhookCABundlePresent(ctx, "validating")).To(BeTrue())
		}).Should(Succeed())
	})

	It("preserves a user-owned NetworkPolicy while cleaning up the CR journey", func() {
		Expect(applyUnmanagedNativePolicy(ctx)).To(Succeed())
		Expect(unmanagedNativePolicyExists(ctx)).To(Succeed())

		By("deleting the Kubernaut CR through its finalizer path")
		Expect(deleteKubernautCR(ctx)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(assertRetainedNamespaces(ctx, namespaces)).To(Succeed())
			g.Expect(assertProvisioningWitnesses(ctx, provisioned)).To(Succeed())
			g.Expect(noManagedPolicies(ctx)).To(Succeed())
			g.Expect(unmanagedNativePolicyExists(ctx)).To(Succeed())
			if configuredTLS == tlsManualAdmin {
				g.Expect(assertManualTLSFixtureUnchanged(ctx, activeManualTLSSelection)).To(Succeed())
			}
		}).Should(Succeed())
	})

	AfterAll(func() {
		Expect(deleteProbeWorkloads(context.Background())).To(Succeed())
	})
})

// assertLiveKAConfigOverride verifies a live default and an explicit CR override.
func assertLiveKAConfigOverride(ctx context.Context, defaultFragment, patchJSON, overrideFragment string) {
	By("verifying the operator-generated live KA ConfigMap and read-only ConfigMap mount")
	Eventually(func(g Gomega) {
		config, err := kubectl(ctx, "get", "configmap", "kubernaut-agent-config", "-n", kubernautNamespace,
			"-o", "jsonpath={.data.config\\.yaml}")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(config).To(ContainSubstring(defaultFragment))
		deployment, deploymentErr := kubectl(ctx, "get", "deployment", "kubernaut-agent", "-n", kubernautNamespace,
			"-o", "yaml")
		g.Expect(deploymentErr).NotTo(HaveOccurred())
		g.Expect(deployment).To(ContainSubstring("name: kubernaut-agent-config"))
		g.Expect(deployment).To(ContainSubstring("readOnly: true"))
	}).Should(Succeed())

	By("patching the live CR with an explicit configuration override")
	patchOutput, patchErr := kubectl(ctx, "patch", "kubernaut", "kubernaut", "-n", kubernautNamespace,
		"--type=merge", "-p", patchJSON)
	Expect(patchErr).NotTo(HaveOccurred(), patchOutput)
	Eventually(func(g Gomega) {
		config, err := kubectl(ctx, "get", "configmap", "kubernaut-agent-config", "-n", kubernautNamespace,
			"-o", "jsonpath={.data.config\\.yaml}")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(config).To(ContainSubstring(overrideFragment))
	}).Should(Succeed())
}

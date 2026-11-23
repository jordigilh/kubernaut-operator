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

package resources

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
)

// #648: the consumer fleet identities (GW/RO/AF) authenticate to FMC with
// their in-cluster ServiceAccount token (kubernaut#2661 peer→facade
// contract, #647). Their SAs must disable automount and their Deployments
// must mount a projected short-TTL SA token at the default path instead —
// the same hardening KubernautAgent already has (projectedServiceAccountTokenVolume).
// #653: EffectivenessMonitor joins the projected-token set — its SA-token
// reads (DataStorage querier, OCP monitoring) also run on the default path
// and get the same bounded lifetime.
// IA-5 (authenticator management) / CM-6: bounded token lifetime, no
// long-lived automounted credential.
var _ = Describe("SA projected tokens", func() {
	consumers := []string{ComponentGateway, ComponentRemediationOrchestrator, ComponentAPIFrontend, ComponentEffectivenessMonitor}

	Context("ServiceAccount", func() {
		It("UT-648-SA-001 [IA-5]: disables token automount for consumer fleet SAs", func() {
			kn := testKubernaut()
			for _, component := range consumers {
				sa := ServiceAccount(kn, component)
				Expect(sa.AutomountServiceAccountToken).NotTo(BeNil(),
					"ServiceAccount(%q).AutomountServiceAccountToken must be set", component)
				Expect(*sa.AutomountServiceAccountToken).To(BeFalse(),
					"ServiceAccount(%q) must disable automount; its Deployment mounts a projected short-TTL token instead", component)
			}
		})

		It("UT-648-SA-002 [CM-6]: leaves FMC's SA automount enabled", func() {
			kn, _ := testKubernautWithFMC()
			sa := ServiceAccount(kn, ComponentFleetMetadataCache)
			Expect(sa.AutomountServiceAccountToken).To(BeNil(),
				"UT-FLEET-647-SA-001 pins FMC's automount — only consumer identities are hardened")
		})

		It("UT-648-SA-003 [CM-6]: membership of projectedTokenComponents matches the SAs that disable automount", func() {
			kn := testKubernaut()
			automountDisabled := make([]string, 0, len(projectedTokenComponents))
			for _, component := range AllComponents() {
				sa := ServiceAccount(kn, component)
				if sa.AutomountServiceAccountToken != nil && !*sa.AutomountServiceAccountToken {
					automountDisabled = append(automountDisabled, component)
				}
			}
			expected := make([]string, 0, len(projectedTokenComponents))
			for component := range projectedTokenComponents {
				expected = append(expected, component)
			}
			Expect(automountDisabled).To(ConsistOf(expected),
				"every SA that disables automount must be a projectedTokenComponents member, and vice versa")
		})

		It("UT-648-SA-004 [CM-6]: sa-token volume presence matches projectedTokenComponents across all Deployments", func() {
			// Cross-file invariant, both directions: a member's Deployment
			// must mount the projected token (disabling automount without it
			// leaves the pod with no SA token at all — a silent auth
			// failure), and a non-member's Deployment must not carry a
			// redundant projected token next to its automounted one.
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			deps := getAllDeployments(kn)
			apiFrontend, err := APIFrontendDeployment(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			deps = append(deps, apiFrontend)
			console, err := ConsoleDeployment(testKubernautWithConsole())
			Expect(err).NotTo(HaveOccurred())
			deps = append(deps, console)
			fmc, err := FleetMetadataCacheDeployment(kn, knV2)
			Expect(err).NotTo(HaveOccurred())
			deps = append(deps, fmc)

			membersSeen := make(map[string]bool, len(projectedTokenComponents))
			for _, dep := range deps {
				component := dep.Spec.Template.Labels["app"]
				Expect(component).NotTo(BeEmpty(), "Deployment %q has no app label", dep.Name)
				hasSAToken := findVolume(dep, "sa-token") != nil
				Expect(hasSAToken).To(Equal(projectedTokenComponents[component]),
					"Deployment for %q sa-token volume presence must match projectedTokenComponents membership", component)
				if projectedTokenComponents[component] {
					membersSeen[component] = true
				}
			}
			Expect(membersSeen).To(HaveLen(len(projectedTokenComponents)),
				"every projectedTokenComponents member's Deployment must be swept here; update the fixture list together with the map")
		})
	})

	Context("Deployment", func() {
		build := func(component string) *appsv1.Deployment {
			kn := testKubernaut()
			knV2 := testKnV2(kn)
			var (
				dep *appsv1.Deployment
				err error
			)
			switch component {
			case ComponentGateway:
				dep, err = GatewayDeployment(kn, knV2)
			case ComponentRemediationOrchestrator:
				dep, err = RemediationOrchestratorDeployment(kn, knV2)
			case ComponentAPIFrontend:
				dep, err = APIFrontendDeployment(kn, knV2)
			case ComponentEffectivenessMonitor:
				dep, err = EffectivenessMonitorDeployment(kn, knV2)
			}
			Expect(err).NotTo(HaveOccurred(), component)
			Expect(dep).NotTo(BeNil(), component)
			return dep
		}

		It("UT-648-TOKEN-001 [IA-5]: mounts a projected short-TTL SA token at the default path", func() {
			for _, component := range consumers {
				dep := build(component)

				volume := findVolume(dep, "sa-token")
				Expect(volume).NotTo(BeNil(), "Deployment for %q must mount the sa-token volume", component)
				Expect(volume.Projected).NotTo(BeNil(), "Deployment for %q sa-token must be projected", component)

				found := false
				for _, source := range volume.Projected.Sources {
					if source.ServiceAccountToken != nil {
						found = true
						Expect(source.ServiceAccountToken.Path).To(Equal("token"),
							"Deployment for %q must project the token at the default path read by auth.NewDefaultTokenSource()", component)
						Expect(source.ServiceAccountToken.ExpirationSeconds).NotTo(BeNil(),
							"Deployment for %q token projection must bound ExpirationSeconds", component)
						Expect(*source.ServiceAccountToken.ExpirationSeconds).To(Equal(int64(3600)),
							"Deployment for %q token TTL must mirror the KA pattern (1h, kubelet-rotated)", component)
					}
				}
				Expect(found).To(BeTrue(), "Deployment for %q sa-token volume must project a ServiceAccountToken", component)

				mount := dep.Spec.Template.Spec.Containers[0].VolumeMounts
				foundMount := false
				for _, vm := range mount {
					if vm.Name == "sa-token" {
						foundMount = true
						Expect(vm.MountPath).To(Equal("/var/run/secrets/kubernetes.io/serviceaccount"),
							"Deployment for %q must mount the projected token at the automount path", component)
						Expect(vm.ReadOnly).To(BeTrue(), component)
					}
				}
				Expect(foundMount).To(BeTrue(), "Deployment for %q container must mount sa-token", component)
			}
		})
	})
})

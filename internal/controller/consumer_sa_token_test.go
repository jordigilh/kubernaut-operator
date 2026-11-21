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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// #648: the consumer fleet identities (GW/RO/AF) authenticate to FMC with
// their in-cluster ServiceAccount token (kubernaut#2661 peer→facade
// contract, #647). Through a real envtest reconcile, their live
// ServiceAccounts must disable automount and their live Deployments must
// mount a projected short-TTL SA token at the default path — same path,
// same identity, bounded lifetime, so no behavior change (IA-5/CM-6).
var _ = Describe("Kubernaut Lifecycle", func() {

	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	Describe("Consumer SA projected tokens (#648)", func() {
		deploymentSAComponents := []struct {
			component, saName string
		}{
			{resources.ComponentGateway, resources.ServiceAccountName(resources.ComponentGateway)},
			{resources.ComponentRemediationOrchestrator, resources.ServiceAccountName(resources.ComponentRemediationOrchestrator)},
			{resources.ComponentAPIFrontend, resources.ServiceAccountName(resources.ComponentAPIFrontend)},
		}

		It("IT-648-RECONCILE-TOKEN-001 [IA-5, CM-6]: reconciliation renders consumer SA automount off plus projected short-TTL tokens at the default path", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			enableFleetMetadataCache(ctx)
			reconcileToRunning(ctx)

			for _, c := range deploymentSAComponents {
				By("verifying the " + c.component + " ServiceAccount disables token automount")
				sa := &corev1.ServiceAccount{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: c.saName, Namespace: testNamespace}, sa)).To(Succeed())
				Expect(sa.AutomountServiceAccountToken).NotTo(BeNil(),
					"%s SA must set AutomountServiceAccountToken explicitly", c.component)
				Expect(*sa.AutomountServiceAccountToken).To(BeFalse(),
					"%s SA must disable automount; its Deployment mounts a projected token instead", c.component)

				By("verifying the " + c.component + " Deployment mounts the projected short-TTL SA token at the default path")
				dep := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: resources.DeploymentName(c.component), Namespace: testNamespace,
				}, dep)).To(Succeed())

				volumeFound := false
				for _, volume := range dep.Spec.Template.Spec.Volumes {
					if volume.Name != "sa-token" {
						continue
					}
					volumeFound = true
					Expect(volume.Projected).NotTo(BeNil(),
						"%s sa-token volume must be projected", c.component)
					tokenFound := false
					for _, source := range volume.Projected.Sources {
						if source.ServiceAccountToken != nil {
							tokenFound = true
							Expect(source.ServiceAccountToken.Path).To(Equal("token"),
								"%s token must project at the default path read by auth.NewDefaultTokenSource()", c.component)
							Expect(source.ServiceAccountToken.ExpirationSeconds).NotTo(BeNil(),
								"%s token projection must bound ExpirationSeconds", c.component)
							Expect(*source.ServiceAccountToken.ExpirationSeconds).To(Equal(int64(3600)),
								"%s token TTL must mirror the KA pattern (1h, kubelet-rotated)", c.component)
						}
					}
					Expect(tokenFound).To(BeTrue(),
						"%s sa-token volume must project a ServiceAccountToken", c.component)
				}
				Expect(volumeFound).To(BeTrue(), "%s Deployment must mount the sa-token volume", c.component)

				mountFound := false
				for _, mount := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
					if mount.Name == "sa-token" {
						mountFound = true
						Expect(mount.MountPath).To(Equal("/var/run/secrets/kubernetes.io/serviceaccount"),
							"%s must mount the projected token at the automount path", c.component)
						Expect(mount.ReadOnly).To(BeTrue(), "%s sa-token mount must be read-only", c.component)
					}
				}
				Expect(mountFound).To(BeTrue(), "%s container must mount sa-token", c.component)
			}

			By("regression: FMC's own SA keeps automount enabled (UT-FLEET-647-SA-001 pins it)")
			fmcSA := &corev1.ServiceAccount{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resources.ServiceAccountName(resources.ComponentFleetMetadataCache), Namespace: testNamespace,
			}, fmcSA)).To(Succeed())
			Expect(fmcSA.AutomountServiceAccountToken).To(BeNil(),
				"FMC authenticates with the auto-mounted SA token — automount must not be disabled")
		})
	})
})

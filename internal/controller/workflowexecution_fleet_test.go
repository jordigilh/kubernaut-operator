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

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// Business acceptance criteria (#235, DD-235, AC-6 least-privilege):
// WorkflowExecution (WE) is the only fleet-aware component that calls MCP
// write tools (resources_create_or_update/resources_delete), so its
// spec.workflowExecution.fleet.oauth2CredentialsSecretRef must reach the
// deployed workload as its own, independently-mounted credential -- never
// silently substituted by the shared spec.fleet.oauth2.credentialsSecretRef
// every other fleet-aware component falls back to. This proves the
// no-fallback guarantee established at UT level (internal/resources) is
// actually wired into KubernautReconciler's production dispatch path, not
// just correct in an isolated resource-builder call (pyramid invariant).
var _ = Describe("WorkflowExecution fleet write-scoped OAuth2 credential wiring (#235)", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("[AC-6] renders WE's own credential (never the shared one) into both the workflowexecution-config ConfigMap and the workflowexecution Deployment's Secret mount", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

		knV2 := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), knV2)).To(Succeed())
		t := true
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &t, Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled: true, TokenURL: "https://keycloak.example.com/token",
				CredentialsSecretRef: "shared-fleet-oauth2-creds",
			},
		}
		knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = "we-write-oauth2-creds"
		Expect(k8sClient.Update(ctx, knV2)).To(Succeed(),
			"[AC-6] a WE-specific write-scoped credential alongside the shared read-only one must be accepted end-to-end")

		reconcileToRunning(ctx)

		By("verifying the workflowexecution-config ConfigMap renders WE's own credential, not the shared one")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: "workflowexecution-config", Namespace: testNamespace,
		}, cm)).To(Succeed())
		data := cm.Data["workflowexecution.yaml"]
		Expect(data).To(ContainSubstring("fleet:"), "WE config should contain the fleet block when fleet+oauth2 are enabled, got:\n%s", data)
		Expect(data).To(ContainSubstring("credentialsSecretRef: we-write-oauth2-creds"),
			"[AC-6] WE config must render its own write-scoped credentialsSecretRef, got:\n%s", data)
		Expect(data).NotTo(ContainSubstring("shared-fleet-oauth2-creds"),
			"[AC-6] WE config must never render the shared read-only credentialsSecretRef, got:\n%s", data)

		By("verifying the workflowexecution Deployment mounts WE's own credential Secret, not the shared one")
		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: "workflowexecution-controller", Namespace: testNamespace,
		}, dep)).To(Succeed())

		var fleetVol *corev1.Volume
		for i := range dep.Spec.Template.Spec.Volumes {
			if dep.Spec.Template.Spec.Volumes[i].Name == "fleet-oauth2" {
				fleetVol = &dep.Spec.Template.Spec.Volumes[i]
			}
		}
		Expect(fleetVol).NotTo(BeNil(), "workflowexecution Deployment should have a fleet-oauth2 volume")
		Expect(fleetVol.Secret).NotTo(BeNil())
		Expect(fleetVol.Secret.SecretName).To(Equal("we-write-oauth2-creds"),
			"[AC-6] WE Deployment must mount its own write-scoped Secret, never the shared fleet credential")

		var fleetMountPath string
		for _, c := range dep.Spec.Template.Spec.Containers {
			for _, m := range c.VolumeMounts {
				if m.Name == "fleet-oauth2" {
					fleetMountPath = m.MountPath
				}
			}
		}
		Expect(fleetMountPath).To(Equal("/etc/workflowexecution/we-write-oauth2-creds"),
			"WE's fleet-oauth2 mount path must be distinct from every other component's own /etc/<component> convention")
	})
})

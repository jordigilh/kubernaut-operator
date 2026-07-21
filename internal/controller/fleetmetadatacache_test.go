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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// newCRWithFMCEnabled returns a minimal CR (route disabled) with
// spec.fleet and spec.fleetMetadataCache configured and enabled -- the
// minimum needed to pass validateFleetMetadataCache (#200).
func newCRWithFMCEnabled() *kubernautv1alpha1.Kubernaut {
	cr := newCRWithRouteDisabled()
	t := true
	cr.Spec.Fleet = kubernautv1alpha1.FleetSpec{
		Enabled: &t, Backend: "fleetmetadatacache",
		MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
		OAuth2: kubernautv1alpha1.OAuth2Spec{
			Enabled: true, TokenURL: "https://keycloak.example.com/token",
			CredentialsSecretRef: "fleet-oauth2-creds",
		},
	}
	cr.Spec.FleetMetadataCache = kubernautv1alpha1.FleetMetadataCacheSpec{Enabled: &t}
	return cr
}

var _ = Describe("Kubernaut Lifecycle", func() {

	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	Describe("FleetMetadataCache Lifecycle (#200)", func() {
		It("creates FMC Deployment, Service, ConfigMap, ClusterRole, and CRB when enabled", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			reconcileToRunning(ctx)

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resources.DeploymentName(resources.ComponentFleetMetadataCache), Namespace: testNamespace,
			}, dep)).To(Succeed())

			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "fleetmetadatacache-service", Namespace: testNamespace,
			}, svc)).To(Succeed())

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "fleetmetadatacache-config", Namespace: testNamespace,
			}, cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey("config.yaml"))

			cr := &rbacv1.ClusterRole{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: testNamespace + "-fleetmetadatacache",
			}, cr)).To(Succeed())

			crb := &rbacv1.ClusterRoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: testNamespace + "-fleetmetadatacache-binding",
			}, crb)).To(Succeed())
		})

		It("skips all FMC resources when fleetMetadataCache is disabled (default)", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			dep := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: resources.DeploymentName(resources.ComponentFleetMetadataCache), Namespace: testNamespace,
			}, dep)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "FMC Deployment should not exist when disabled")

			svc := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "fleetmetadatacache-service", Namespace: testNamespace,
			}, svc)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "FMC Service should not exist when disabled")

			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "fleetmetadatacache-config", Namespace: testNamespace,
			}, cm)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "FMC ConfigMap should not exist when disabled")

			cr := &rbacv1.ClusterRole{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: testNamespace + "-fleetmetadatacache",
			}, cr)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "FMC ClusterRole should not exist when disabled")
		})

		It("still creates FMC's ServiceAccount even when disabled (matches AuthWebhook/APIFrontend precedent)", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
			reconcileToRunning(ctx)

			sa := &corev1.ServiceAccount{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resources.ServiceAccountName(resources.ComponentFleetMetadataCache), Namespace: testNamespace,
			}, sa)).To(Succeed())
		})

		It("includes fleetmetadatacache in per-service status when reaching Running", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			reconcileToRunning(ctx)

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseRunning))

			found := false
			for _, svc := range kn.Status.Services {
				if svc.Name == resources.ComponentFleetMetadataCache {
					found = true
					Expect(svc.Ready).To(BeTrue())
				}
			}
			Expect(found).To(BeTrue(), "fleetmetadatacache should be included in per-service status once active and ready")
		})

		It("cleans up FMC Deployment, Service, ConfigMap, ClusterRole, and CRB when disabled after being enabled", func() {
			createBYOSecrets(ctx)
			cr := newCRWithFMCEnabled()
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			reconcileToRunning(ctx)

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resources.DeploymentName(resources.ComponentFleetMetadataCache), Namespace: testNamespace,
			}, dep)).To(Succeed(), "sanity: FMC Deployment should exist before disabling")

			By("disabling fleetMetadataCache")
			existing := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
			f := false
			existing.Spec.FleetMetadataCache.Enabled = &f
			// Also disable spec.fleet itself: backend=fleetmetadatacache with
			// FMC no longer operator-managed would otherwise require an
			// explicit BYO endpoint (validateFleetConfig) -- out of scope for
			// this cleanup-on-disable test.
			existing.Spec.Fleet.Enabled = &f
			Expect(k8sClient.Update(ctx, existing)).To(Succeed())

			r := newReconciler()
			for i := 0; i < 3; i++ {
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
				Expect(err).NotTo(HaveOccurred())
			}

			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: resources.DeploymentName(resources.ComponentFleetMetadataCache), Namespace: testNamespace,
			}, dep)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "FMC Deployment should be deleted after disabling")

			svc := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "fleetmetadatacache-service", Namespace: testNamespace,
			}, svc)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "FMC Service should be deleted after disabling")

			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "fleetmetadatacache-config", Namespace: testNamespace,
			}, cm)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "FMC ConfigMap should be deleted after disabling")

			clusterRole := &rbacv1.ClusterRole{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: testNamespace + "-fleetmetadatacache",
			}, clusterRole)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "FMC ClusterRole should be deleted after disabling")

			crb := &rbacv1.ClusterRoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: testNamespace + "-fleetmetadatacache-binding",
			}, crb)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "FMC ClusterRoleBinding should be deleted after disabling")
		})

		It("rejects the CR when fleetMetadataCache is enabled but spec.fleet.mcpGatewayEndpoint is missing", func() {
			createBYOSecrets(ctx)
			cr := newCRWithFMCEnabled()
			cr.Spec.Fleet.MCPGatewayEndpoint = ""
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha1.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha1.PhaseError))
		})

		It("emits a FleetMetadataCacheUnused warning event when enabled but spec.fleet.backend is not fleetmetadatacache", func() {
			createBYOSecrets(ctx)
			cr := newCRWithFMCEnabled()
			cr.Spec.Fleet.Backend = "acm"
			cr.Spec.Fleet.Endpoint = "https://search-search-api.example.com:4010"
			cr.Spec.Fleet.TokenSecretName = "acm-search-token"
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			r := reconcileToRunning(ctx)

			recorder := r.Recorder.(*events.FakeRecorder)
			var collected []string
		drain:
			for {
				select {
				case ev := <-recorder.Events:
					collected = append(collected, ev)
				default:
					break drain
				}
			}
			Expect(collected).To(ContainElement(ContainSubstring("FleetMetadataCacheUnused")),
				"expected a FleetMetadataCacheUnused warning event when backend=acm, got: %v", collected)
		})

		It("does not emit FleetMetadataCacheUnused when backend=fleetmetadatacache (the consuming configuration)", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())

			r := reconcileToRunning(ctx)

			recorder := r.Recorder.(*events.FakeRecorder)
			var collected []string
		drainOK:
			for {
				select {
				case ev := <-recorder.Events:
					collected = append(collected, ev)
				default:
					break drainOK
				}
			}
			Expect(collected).NotTo(ContainElement(ContainSubstring("FleetMetadataCacheUnused")))
		})
	})
})

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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// newCRWithFMCEnabled returns a minimal CR (route disabled) with
// spec.fleet and spec.fleetMetadataCache configured and enabled -- the
// minimum needed to pass validateFleetMetadataCache (#200). Fleet moved to
// v1alpha2-only (fleet-branch-remove-v1alpha1): the returned CR itself
// carries no Fleet data (v1alpha1 has no field for it); callers must create
// it via k8sClient and then apply enableFleetMetadataCache to configure
// Fleet on the v1alpha2 storage view.
func newCRWithFMCEnabled() *kubernautv1alpha2.Kubernaut {
	return newCRWithRouteDisabled()
}

// defaultFMCFleetSpec is the spec.fleet configuration newCRWithFMCEnabled()
// configured before Fleet moved to v1alpha2-only (backend=fleetmetadatacache,
// the consuming configuration).
func defaultFMCFleetSpec() kubernautv1alpha2.FleetSpec {
	t := true
	return kubernautv1alpha2.FleetSpec{
		Enabled:    &t,
		ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443"},
		MCPGateway: kubernautv1alpha2.FleetMCPGatewaySpec{Endpoint: "https://mcp-gateway.example.com/sse", Type: "eaigw", Namespace: testNamespace},
		// testNamespace ("default") always exists in envtest -- these
		// generic lifecycle fixtures aren't exercising namespace-scoped
		// RBAC (see mcpgatewaynamespacerbac_*_test.go for that), so reusing
		// it avoids needing an explicit ensureNamespace call per test.
		OAuth2: kubernautv1alpha2.FleetOAuth2Spec{
			TokenURL:             "https://keycloak.example.com/token",
			CredentialsSecretRef: "fleet-oauth2-creds",
		},
	}
}

// enableFleetMetadataCache fetches the singleton's v1alpha2 storage view
// and configures spec.fleet/spec.fleetMetadataCache for FMC, matching what
// newCRWithFMCEnabled() configured before Fleet moved to v1alpha2-only.
func enableFleetMetadataCache(ctx context.Context) {
	enableFleetMetadataCacheWithFleet(ctx, defaultFMCFleetSpec())
}

// enableFleetMetadataCacheWithFleet is like enableFleetMetadataCache but
// lets the caller override spec.fleet (e.g. to test a missing
// mcpGateway.endpoint or a non-fleetmetadatacache backend) while still
// enabling FMC (fleet.enabled=true, fleet.backend=fleetmetadatacache).
func enableFleetMetadataCacheWithFleet(ctx context.Context, fleet kubernautv1alpha2.FleetSpec) {
	knV2 := &kubernautv1alpha2.Kubernaut{}
	Expect(k8sClient.Get(ctx, singletonKey(), knV2)).To(Succeed())
	knV2.Spec.Fleet = fleet
	// #235/DD-235: WorkflowExecution's own write-scoped credential is
	// independently required whenever Fleet is enabled, and
	// never falls back to the shared fleet.oauth2.credentialsSecretRef.
	// Set unconditionally here (harmless when fleet.oauth2 ends up
	// disabled in a caller's override) so every FMC-focused test in this
	// file keeps reaching its own FMC-specific reconcile outcome instead
	// of tripping over an unrelated WE validation failure.
	knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
	Expect(k8sClient.Update(ctx, knV2)).To(Succeed())
}

var _ = Describe("Kubernaut Lifecycle", func() {

	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("rejects an active Fleet when a Secret-backed scope-check CA is missing", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

		kn := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
		fleet := defaultFMCFleetSpec()
		fleet.ScopeCheck.TLS = &kubernautv1alpha2.FleetTrustSpec{
			Source:          kubernautv1alpha2.FleetTrustSourceSecret,
			CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "missing-fleet-ca"},
		}
		kn.Spec.Fleet = fleet
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
		Expect(k8sClient.Update(ctx, kn)).To(Succeed())

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())

		updated := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
		condition := meta.FindStatusCondition(updated.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Message).To(ContainSubstring("spec.fleet.scopeCheck.tls.caCertSecretRef"))
	})

	Describe("FleetMetadataCache Lifecycle (#200)", func() {
		It("creates FMC Deployment, Service, ConfigMap, namespace-scoped Role, and RoleBinding when enabled", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			enableFleetMetadataCache(ctx)
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
			Expect(cm.Data["config.yaml"]).To(ContainSubstring("certDir: /etc/tls"))

			By("verifying the FMC Deployment mounts the serving Secret at the configured cert directory")
			servingSecretMounted := false
			for _, volume := range dep.Spec.Template.Spec.Volumes {
				if volume.Name == "tls-certs" {
					servingSecretMounted = true
					Expect(volume.Secret).NotTo(BeNil())
					Expect(volume.Secret.SecretName).To(Equal(resources.FleetMetadataCacheTLSSecretName))
				}
			}
			Expect(servingSecretMounted).To(BeTrue())
			servingMountFound := false
			for _, mount := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
				if mount.Name == "tls-certs" {
					servingMountFound = true
					Expect(mount.MountPath).To(Equal(resources.InterServiceTLSCertDir))
					Expect(mount.ReadOnly).To(BeTrue())
				}
			}
			Expect(servingMountFound).To(BeTrue())
			Expect(svc.Annotations).To(HaveKeyWithValue(resources.OCPServingCertAnnotation, resources.FleetMetadataCacheTLSSecretName))

			// kubernaut-operator#455: mcpGateway.namespace is now mandatory,
			// so the cluster-scoped ClusterRole/CRB variant (empty
			// namespace) is no longer a reachable state -- FMC's MCP
			// Gateway CRD watch always grants via the namespace-scoped
			// Role/RoleBinding instead (MCPGatewayNamespaceRBAC, DD-362).
			role := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: testNamespace + "-fleetmetadatacache-mcpgateway", Namespace: testNamespace,
			}, role)).To(Succeed())

			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: testNamespace + "-fleetmetadatacache-mcpgateway-binding", Namespace: testNamespace,
			}, rb)).To(Succeed())
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
			enableFleetMetadataCache(ctx)
			reconcileToRunning(ctx)

			kn := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseRunning))

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
			enableFleetMetadataCache(ctx)
			reconcileToRunning(ctx)

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resources.DeploymentName(resources.ComponentFleetMetadataCache), Namespace: testNamespace,
			}, dep)).To(Succeed(), "sanity: FMC Deployment should exist before disabling")

			By("disabling fleet (FMC deployment is derived from fleet.enabled+backend, no separate toggle)")
			existing := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), existing)).To(Succeed())
			f := false
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

		It("rejects the CR when fleetMetadataCache is enabled but spec.fleet.mcpGateway.endpoint is missing", func() {
			createBYOSecrets(ctx)
			cr := newCRWithFMCEnabled()
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			fleet := defaultFMCFleetSpec()
			fleet.MCPGateway.Endpoint = ""
			enableFleetMetadataCacheWithFleet(ctx, fleet)

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())

			kn := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), kn)).To(Succeed())
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
		})

	})
})

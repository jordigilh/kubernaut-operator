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
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	return newMinimalCR()
}

// defaultFMCFleetSpec is the spec.fleet configuration newCRWithFMCEnabled()
// configured before Fleet moved to v1alpha2-only (backend=fleetmetadatacache,
// the consuming configuration).
func defaultFMCFleetSpec() kubernautv1alpha2.FleetSpec {
	t := true
	return kubernautv1alpha2.FleetSpec{
		Enabled:    &t,
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

// acmSourceCAPEM generates a parseable self-signed CA certificate for the
// #647 ACM source CA Secret fixture — validateFleetTrustSecrets parses the
// PEM, so placeholder text is rejected as an invalid CA (by design).
func acmSourceCAPEM() []byte {
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(647),
		Subject:               pkix.Name{CommonName: "acm-source-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, template, template, &key.PublicKey, key)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
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

	Describe("FleetMetadataCache Lifecycle (#200)", func() {
		It("IT-FLEET-521-RECONCILE-FMC: creates FMC resources and renders the derived endpoint/trust", func() {
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

			gatewayConfig := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gateway-config", Namespace: testNamespace}, gatewayConfig)).To(Succeed())
			Expect(gatewayConfig.Data["config.yaml"]).NotTo(ContainSubstring("backend:"),
				"#647/1e: the peer-model backend key is gone from the rendered fleet block")
			Expect(gatewayConfig.Data["config.yaml"]).To(ContainSubstring("endpoint: https://fleetmetadatacache-service." + testNamespace + ".svc.cluster.local:8080"))
			Expect(gatewayConfig.Data["config.yaml"]).To(ContainSubstring("tlsCAFile: /etc/tls-ca/service-ca.crt"))

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

		It("IT-FLEET-557-RECONCILE-MCP-CA: wires MCP Gateway CA rendering and read-only mounts through Reconcile", func() {
			const mcpGatewayCAVolumeName = "fleet-mcp-gateway-ca"
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "mcp-gateway-ca", Namespace: testNamespace},
				Data:       map[string][]byte{"ca.crt": telemetryCAPEM()},
			})).To(Succeed())
			fleet := defaultFMCFleetSpec()
			fleet.MCPGateway.TLS = &kubernautv1alpha2.FleetTrustSpec{
				Source:          kubernautv1alpha2.FleetTrustSourceSecret,
				CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "mcp-gateway-ca"},
			}
			cr := newMinimalCR()
			cr.Spec.Fleet = fleet
			cr.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			reconcileToRunning(ctx)

			gatewayConfig := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gateway-config", Namespace: testNamespace}, gatewayConfig)).To(Succeed())
			Expect(gatewayConfig.Data["config.yaml"]).To(ContainSubstring("mcpGatewayTLSCAFile: /etc/fleet-tls/mcp-gateway/ca.crt"))
			fmcConfig := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "fleetmetadatacache-config", Namespace: testNamespace}, fmcConfig)).To(Succeed())
			Expect(fmcConfig.Data["config.yaml"]).To(ContainSubstring("tlsCAFile: /etc/fleet-tls/mcp-gateway/ca.crt"))

			gateway := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(resources.ComponentGateway), Namespace: testNamespace}, gateway)).To(Succeed())
			foundVolume := false
			for _, volume := range gateway.Spec.Template.Spec.Volumes {
				if volume.Name == mcpGatewayCAVolumeName {
					foundVolume = true
					Expect(volume.Secret).NotTo(BeNil())
					Expect(volume.Secret.SecretName).To(Equal("mcp-gateway-ca"))
				}
			}
			Expect(foundVolume).To(BeTrue())
			foundMount := false
			for _, mount := range gateway.Spec.Template.Spec.Containers[0].VolumeMounts {
				if mount.Name == mcpGatewayCAVolumeName && mount.MountPath == "/etc/fleet-tls/mcp-gateway" {
					foundMount = true
					Expect(mount.ReadOnly).To(BeTrue())
				}
			}
			Expect(foundMount).To(BeTrue())

			fmc := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(resources.ComponentFleetMetadataCache), Namespace: testNamespace}, fmc)).To(Succeed())
			fmcVolume := false
			for _, volume := range fmc.Spec.Template.Spec.Volumes {
				if volume.Name == mcpGatewayCAVolumeName {
					fmcVolume = true
					Expect(volume.Secret.SecretName).To(Equal("mcp-gateway-ca"))
				}
			}
			Expect(fmcVolume).To(BeTrue())
		})

		// #647: IT-FLEET-521-RECONCILE-ACM was removed with the peer-model
		// scope-check lane — the ACM surface is FMC's source, covered by
		// IT-FLEET-647-SRC-001 below. Consumers render no backend and no
		// fleet token anymore.

		// #647 (DD-FLEET-017 v2 D4.1a): FMC's ACM Search source surface,
		// rendered only when the source is selected.
		It("IT-FLEET-647-SRC-001: renders the ACM source config and mounts through Reconcile, with unchanged RBAC", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			enableFleetMetadataCache(ctx)

			knV2 := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), knV2)).To(Succeed())
			knV2.Spec.FleetMetadataCache.Sources = &kubernautv1alpha2.FleetMetadataCacheSourcesSpec{
				ACM: &kubernautv1alpha2.FleetACMSourceSpec{
					Endpoint:       "https://acm-search.example.com",
					TokenSecretRef: &kubernautv1alpha2.SecretKeyRef{Name: "acm-search-token"},
					TLS: &kubernautv1alpha2.FleetTrustSpec{
						Source:          kubernautv1alpha2.FleetTrustSourceSecret,
						CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "acm-source-ca"},
					},
				},
			}
			Expect(k8sClient.Update(ctx, knV2)).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "acm-search-token", Namespace: testNamespace},
				Data:       map[string][]byte{"token": []byte("acm-bearer-token")},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "acm-source-ca", Namespace: testNamespace},
				Data:       map[string][]byte{"ca.crt": acmSourceCAPEM()},
			})).To(Succeed())
			reconcileToRunning(ctx)

			fmcConfig := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "fleetmetadatacache-config", Namespace: testNamespace}, fmcConfig)).To(Succeed())
			data := fmcConfig.Data["config.yaml"]
			Expect(data).To(ContainSubstring("sources:"))
			Expect(data).To(ContainSubstring("endpoint: https://acm-search.example.com"))
			Expect(data).To(ContainSubstring("tokenPath: /etc/fleet-token/token"))
			Expect(data).To(ContainSubstring("caFile: /etc/fleet-tls/acm-source/ca.crt"))
			Expect(data).NotTo(ContainSubstring("acm-search-token"),
				"token Secret must be referenced by mount path only, never by name (SOC2 CC6.1)")

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(resources.ComponentFleetMetadataCache), Namespace: testNamespace}, deployment)).To(Succeed())
			foundToken, foundCA := false, false
			for _, volume := range deployment.Spec.Template.Spec.Volumes {
				if volume.Name == "fleet-acm-source-token" {
					foundToken = true
					Expect(volume.Secret.SecretName).To(Equal("acm-search-token"))
				}
				if volume.Name == "fleet-acm-source-ca" {
					foundCA = true
					Expect(volume.Secret.SecretName).To(Equal("acm-source-ca"))
				}
			}
			Expect(foundToken).To(BeTrue())
			Expect(foundCA).To(BeTrue())
			for _, container := range deployment.Spec.Template.Spec.Containers {
				for _, mount := range container.VolumeMounts {
					if mount.Name == "fleet-acm-source-token" || mount.Name == "fleet-acm-source-ca" {
						Expect(mount.ReadOnly).To(BeTrue(), "%s must be read-only", mount.Name)
					}
				}
			}

			// Acceptance (c): no Search RBAC through this change. With
			// mcpGateway.namespace set (DD-362), FMC's watch RBAC is the
			// namespace-scoped Role; its rules must equal a build of the
			// same CR with the source selection removed.
			live := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), live)).To(Succeed())
			withoutSource := live.DeepCopy()
			withoutSource.Spec.FleetMetadataCache.Sources = nil
			roleName := testNamespace + "-fleetmetadatacache-mcpgateway"
			role := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: testNamespace}, role)).To(Succeed())
			expectedRoles, _ := resources.MCPGatewayNamespaceRBAC(live, withoutSource)
			var expectedRules []rbacv1.PolicyRule
			for _, r := range expectedRoles {
				if r.Name == roleName {
					expectedRules = r.Rules
				}
			}
			Expect(expectedRules).NotTo(BeNil())
			Expect(role.Rules).To(Equal(expectedRules),
				"selecting the ACM source must not grant the FMC ServiceAccount any Search RBAC (AC-6)")
			for _, rule := range role.Rules {
				Expect(rule.APIGroups).NotTo(ContainElement("search.open-cluster-management.io"))
			}
		})

		It("IT-FLEET-647-SRC-002 [CM-7, IA-5; ASVS v5.0.0-V5.1.1]: fails closed before rollout when the ACM token Secret is missing", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			enableFleetMetadataCache(ctx)

			knV2 := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), knV2)).To(Succeed())
			knV2.Spec.FleetMetadataCache.Sources = &kubernautv1alpha2.FleetMetadataCacheSourcesSpec{
				ACM: &kubernautv1alpha2.FleetACMSourceSpec{
					Endpoint:       "https://acm-search.example.com",
					TokenSecretRef: &kubernautv1alpha2.SecretKeyRef{Name: "missing-acm-source-token"},
				},
			}
			Expect(k8sClient.Update(ctx, knV2)).To(Succeed())

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
			Expect(condition.Message).To(ContainSubstring("spec.fleetMetadataCache.sources.acm.tokenSecretRef"))
		})

		It("IT-FLEET-647-SRC-003 [CM-7, SC-8]: fails closed before rollout when the Secret-backed ACM source CA is missing", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newCRWithFMCEnabled())).To(Succeed())
			enableFleetMetadataCache(ctx)

			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "acm-search-token", Namespace: testNamespace},
				Data:       map[string][]byte{"token": []byte("acm-bearer-token")},
			})).To(Succeed())

			knV2 := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, singletonKey(), knV2)).To(Succeed())
			knV2.Spec.FleetMetadataCache.Sources = &kubernautv1alpha2.FleetMetadataCacheSourcesSpec{
				ACM: &kubernautv1alpha2.FleetACMSourceSpec{
					Endpoint:       "https://acm-search.example.com",
					TokenSecretRef: &kubernautv1alpha2.SecretKeyRef{Name: "acm-search-token"},
					TLS: &kubernautv1alpha2.FleetTrustSpec{
						Source:          kubernautv1alpha2.FleetTrustSourceSecret,
						CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "missing-acm-source-ca"},
					},
				},
			}
			Expect(k8sClient.Update(ctx, knV2)).To(Succeed())

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
			Expect(condition.Message).To(ContainSubstring("spec.fleetMetadataCache.sources.acm.tls.caCertSecretRef"))
		})

		It("skips all FMC resources when fleetMetadataCache is disabled (default)", func() {
			createBYOSecrets(ctx)
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
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
			Expect(k8sClient.Create(ctx, newMinimalCR())).To(Succeed())
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

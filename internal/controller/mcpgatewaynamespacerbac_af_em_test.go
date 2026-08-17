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
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// Business acceptance criteria (#227, BR-FLEET-003/BR-INTEGRATION-054,
// AC-6 least-privilege): once the shared spec.fleet.mcpGatewayNamespace
// (DD-362 -- no per-component override) resolves to a real namespace, the
// operator must grant AF/EM a namespace-scoped Role/RoleBinding there
// instead of leaving the cluster-wide MCP Gateway CRD read grant on their
// ClusterRole -- mirroring FMC/SP's existing #224 Finding 5 pattern. This
// proves the reconciler wiring end-to-end via envtest, not just the
// resource-builder unit output covered in internal/resources/rbac_test.go.
var _ = Describe("AF/EM namespace-scoped MCP Gateway RBAC wiring (#227)", func() {
	ctx := context.Background()
	const mcpGatewayTargetNS = "mcpgateway-target-ns"

	ensureNamespace := func(name string) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		err := k8sClient.Create(ctx, ns)
		Expect(err == nil || errors.IsAlreadyExists(err)).To(BeTrue(), "creating namespace %s: %v", name, err)
	}

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("creates AF/EM's namespace-scoped Role+RoleBinding once the shared spec.fleet.mcpGatewayNamespace resolves, and drops the MCP Gateway CRD rules from their ClusterRole", func() {
		ensureNamespace(mcpGatewayTargetNS)
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())

		knV2 := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), knV2)).To(Succeed())
		t := true
		knV2.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &t, Backend: "fleetmetadatacache", Endpoint: "http://fleetmetadatacache.example.com",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
			MCPGatewayNamespace: mcpGatewayTargetNS,
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled: true, TokenURL: "https://keycloak.example.com/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}
		// #235/DD-235: WorkflowExecution's own write-scoped credential is
		// independently required and never falls back to the shared one
		// above.
		knV2.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
		Expect(k8sClient.Update(ctx, knV2)).To(Succeed())

		reconcileToRunning(ctx)

		By("verifying AF's namespace-scoped Role/RoleBinding exist in the shared target namespace")
		afRole := &rbacv1.Role{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-apifrontend-mcpgateway", Namespace: mcpGatewayTargetNS,
		}, afRole)).To(Succeed())
		Expect(hasMCPGatewayCRDRule(afRole.Rules)).To(BeTrue(),
			"apifrontend-mcpgateway Role should carry the MCP Gateway CRD rules, got: %+v", afRole.Rules)

		afRB := &rbacv1.RoleBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-apifrontend-mcpgateway-binding", Namespace: mcpGatewayTargetNS,
		}, afRB)).To(Succeed())

		By("verifying EM's namespace-scoped Role/RoleBinding exist in the shared target namespace")
		emRole := &rbacv1.Role{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-effectivenessmonitor-mcpgateway", Namespace: mcpGatewayTargetNS,
		}, emRole)).To(Succeed())
		Expect(hasMCPGatewayCRDRule(emRole.Rules)).To(BeTrue(),
			"effectivenessmonitor-mcpgateway Role should carry the MCP Gateway CRD rules, got: %+v", emRole.Rules)

		emRB := &rbacv1.RoleBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: testNamespace + "-effectivenessmonitor-mcpgateway-binding", Namespace: mcpGatewayTargetNS,
		}, emRB)).To(Succeed())

		By("verifying AF's cluster-scoped ClusterRole no longer carries the MCP Gateway CRD rules")
		afCR := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-apifrontend-role"}, afCR)).To(Succeed())
		Expect(hasMCPGatewayCRDRule(afCR.Rules)).To(BeFalse(),
			"apifrontend-role ClusterRole should omit the MCP Gateway CRD rules once the shared namespace resolves, got: %+v", afCR.Rules)

		By("verifying EM's cluster-scoped ClusterRole no longer carries the MCP Gateway CRD rules")
		emCR := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testNamespace + "-effectivenessmonitor-controller"}, emCR)).To(Succeed())
		Expect(hasMCPGatewayCRDRule(emCR.Rules)).To(BeFalse(),
			"effectivenessmonitor-controller ClusterRole should omit the MCP Gateway CRD rules once the shared namespace resolves, got: %+v", emCR.Rules)
	})
})

// hasMCPGatewayCRDRule reports whether rules grant read access to either
// gatewayType's MCP Gateway CRD group (see resources.mcpGatewayCRDPolicyRules).
func hasMCPGatewayCRDRule(rules []rbacv1.PolicyRule) bool {
	for _, r := range rules {
		for _, g := range r.APIGroups {
			if g == "gateway.envoyproxy.io" || g == "aigateway.envoyproxy.io" || g == "mcp.kuadrant.io" {
				return true
			}
		}
	}
	return false
}

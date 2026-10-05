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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// newMinimalV1alpha2CR returns a minimal, otherwise-valid v1alpha2 Kubernaut
// CR with Fleet unset (inert). Individual tests mutate spec.fleet to exercise
// ADR-CRD-001 F12's CEL rule directly against the real apiserver via envtest
// -- CEL rules are only enforced by the apiserver, not by any Go code path,
// so this is admission-level coverage that pure-Go schema tests cannot provide.
func newMinimalV1alpha2CR(name string) *kubernautv1alpha2.Kubernaut {
	return &kubernautv1alpha2.Kubernaut{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: kubernautv1alpha2.KubernautSpec{
			Image: kubernautv1alpha2.ImageSpec{
				PullPolicy: corev1.PullIfNotPresent,
			},
			PostgreSQL: kubernautv1alpha2.PostgreSQLSpec{
				SecretName: pgSecretName,
				Host:       "postgresql",
			},
			Valkey: kubernautv1alpha2.ValkeySpec{
				SecretName: vkSecretName,
				Host:       "valkey",
			},
			LLMProfiles: map[string]kubernautv1alpha2.LLMProfileSpec{
				"primary": {
					Provider:              "openai",
					Model:                 "gpt-4o",
					Endpoint:              "http://llm-gateway:8080",
					CredentialsSecretName: llmSecretName,
				},
			},
			// KubernautAgent.LLMProfileRef intentionally omitted: F10
			// infers "primary" since it's the sole entry above.
			AIAnalysis: kubernautv1alpha2.AIAnalysisSpec{
				Policy: kubernautv1alpha2.PolicyConfigMapRef{ConfigMapName: "aianalysis-policy"},
			},
			SignalProcessing: kubernautv1alpha2.SignalProcessingSpec{
				Policy: kubernautv1alpha2.PolicyConfigMapRef{ConfigMapName: "signalprocessing-policy"},
			},
		},
	}
}

var _ = Describe("v1alpha2 Fleet nested API admission", func() {
	ctx := context.Background()

	var created *kubernautv1alpha2.Kubernaut

	AfterEach(func() {
		if created != nil {
			_ = k8sClient.Delete(ctx, created)
			created = nil
		}
	})

	It("accepts active Fleet with nested MCP Gateway, scope-check, and OAuth2 configuration", func() {
		cr := newMinimalV1alpha2CR("fleet-nested-active")
		t := true
		cr.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &t,
			MCPGateway: kubernautv1alpha2.FleetMCPGatewaySpec{
				Type: "eaigw", Endpoint: "https://mcp-gateway.example.com/sse", Namespace: testNamespace,
			},
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{
				Backend: "fleetmetadatacache", Endpoint: "https://fleet-metadata-cache.fleet-system.svc.cluster.local:8443",
			},
			OAuth2: kubernautv1alpha2.FleetOAuth2Spec{
				TokenURL:             "https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		created = cr

		fetched := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, fetched)).To(Succeed())
		Expect(fetched.Spec.Fleet.Enabled).NotTo(BeNil())
		Expect(*fetched.Spec.Fleet.Enabled).To(BeTrue())
		Expect(fetched.Spec.Fleet.MCPGateway.Endpoint).To(Equal(cr.Spec.Fleet.MCPGateway.Endpoint))
		Expect(fetched.Spec.Fleet.ScopeCheck.Backend).To(Equal("fleetmetadatacache"))
		Expect(fetched.Spec.Fleet.OAuth2.TokenURL).To(Equal(cr.Spec.Fleet.OAuth2.TokenURL))
	})

	It("accepts disabled Fleet with pre-staged nested configuration and no OAuth2 fields", func() {
		cr := newMinimalV1alpha2CR("fleet-nested-disabled")
		f := false
		cr.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &f,
			MCPGateway: kubernautv1alpha2.FleetMCPGatewaySpec{
				Type: "eaigw", Endpoint: "https://mcp-gateway.example.com/sse", Namespace: testNamespace,
			},
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: "acm"},
		}

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		created = cr
	})

	It("rejects an unsupported MCP Gateway type at CRD admission", func() {
		cr := newMinimalV1alpha2CR("fleet-nested-invalid-gateway")
		t := true
		cr.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:    &t,
			MCPGateway: kubernautv1alpha2.FleetMCPGatewaySpec{Type: "unsupported"},
		}

		err := k8sClient.Create(ctx, cr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Unsupported value"))
	})

	It("accepts fleet unset entirely", func() {
		cr := newMinimalV1alpha2CR("f12-accept-fleet-unset")

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		created = cr
	})
})

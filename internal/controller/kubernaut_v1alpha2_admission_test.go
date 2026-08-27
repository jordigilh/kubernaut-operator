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
// so this is admission-level coverage that api/v1alpha1's pure-Go conversion
// tests cannot provide.
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

var _ = Describe("v1alpha2 Fleet OAuth2 admission (ADR-CRD-001 F12)", func() {
	ctx := context.Background()

	var created *kubernautv1alpha2.Kubernaut

	AfterEach(func() {
		if created != nil {
			_ = k8sClient.Delete(ctx, created)
			created = nil
		}
	})

	It("rejects fleet.enabled=true with mcpGatewayEndpoint set but oauth2.enabled omitted", func() {
		cr := newMinimalV1alpha2CR("f12-reject-no-oauth2")
		t := true
		cr.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:            &t,
			Backend:            "fleetmetadatacache",
			Endpoint:           "https://fleet-metadata-cache.fleet-system.svc.cluster.local:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse",
			MCPGatewayType:     "eaigw",
			// OAuth2 left at zero value -- Enabled defaults to false.
		}

		err := k8sClient.Create(ctx, cr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("fleet.oauth2.enabled must be true"))
	})

	It("rejects fleet.enabled=true with mcpGatewayEndpoint set and oauth2.enabled explicitly false", func() {
		cr := newMinimalV1alpha2CR("f12-reject-oauth2-false")
		t := true
		cr.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:            &t,
			Backend:            "fleetmetadatacache",
			Endpoint:           "https://fleet-metadata-cache.fleet-system.svc.cluster.local:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse",
			MCPGatewayType:     "eaigw",
			OAuth2:             kubernautv1alpha2.OAuth2Spec{Enabled: false},
		}

		err := k8sClient.Create(ctx, cr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("fleet.oauth2.enabled must be true"))
	})

	It("accepts fleet.enabled=true with mcpGatewayEndpoint set and oauth2.enabled=true", func() {
		cr := newMinimalV1alpha2CR("f12-accept-oauth2-true")
		t := true
		cr.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:             &t,
			Backend:             "fleetmetadatacache",
			Endpoint:            "https://fleet-metadata-cache.fleet-system.svc.cluster.local:8443",
			MCPGatewayEndpoint:  "https://mcp-gateway.example.com/sse",
			MCPGatewayType:      "eaigw",
			MCPGatewayNamespace: testNamespace,
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled:              true,
				TokenURL:             "https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/token",
				CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		created = cr

		fetched := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, fetched)).To(Succeed())
		Expect(fetched.Spec.Fleet.OAuth2.Enabled).To(BeTrue())
	})

	It("accepts fleet.enabled=false with mcpGatewayEndpoint set but oauth2 unset (pre-staged, inert config)", func() {
		cr := newMinimalV1alpha2CR("f12-accept-disabled-prestaged")
		f := false
		cr.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:            &f,
			Backend:            "fleetmetadatacache",
			Endpoint:           "https://fleet-metadata-cache.fleet-system.svc.cluster.local:8443",
			MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse",
			MCPGatewayType:     "eaigw",
			// OAuth2 intentionally left unset: Enabled=false means every
			// other Fleet field (including OAuth2) is documented as inert,
			// so pre-staging config while disabled must not be rejected.
		}

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		created = cr
	})

	It("accepts fleet unset entirely", func() {
		cr := newMinimalV1alpha2CR("f12-accept-fleet-unset")

		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		created = cr
	})
})

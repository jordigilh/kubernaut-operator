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

package v1alpha1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	v1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// fullV1alpha1Kubernaut builds a Kubernaut CR that exercises every ADR-CRD-001
// finding (F1, F3-F8) the conversion webhook has to handle, so the round-trip
// tests below have real fixtures to work with instead of zero-value structs.
func fullV1alpha1Kubernaut() *Kubernaut {
	rps := 42
	return &Kubernaut{
		ObjectMeta: metav1.ObjectMeta{Name: SingletonName, Namespace: "kubernaut-system"},
		Spec: KubernautSpec{
			Image: ImageSpec{PullPolicy: "IfNotPresent"},
			PostgreSQL: PostgreSQLSpec{
				SecretName: "pg-secret", Host: "pg.example.com", Port: 5432, SSLMode: "verify-full",
			},
			Valkey: ValkeySpec{SecretName: "valkey-secret", Host: "valkey.example.com", Port: 6379},
			// F4: Ansible lives at the top level in v1alpha1, relocated under
			// WorkflowExecution in v1alpha2.
			Ansible: AnsibleSpec{
				Enabled: true, APIURL: "https://awx.example.com", OrganizationID: 1,
				TokenSecretRef:  &SecretKeyRef{Name: "awx-token", Key: "token"},
				CACertSecretRef: &CACertSecretRef{Name: "awx-ca", Key: "ca.crt"},
			},
			// F8: lowercase, normalized to INFO in v2. Logging lives per-component
			// (not at the top level of KubernautSpec); Notification is as good a
			// representative as any since convertLoggingSpecToV2 is shared code.
			Notification: NotificationSpec{Logging: LoggingSpec{Level: "info"}},
			LLMProfiles: map[string]LLMProfileSpec{
				"primary": {Provider: "openai", Model: "gpt-4o", Endpoint: "http://llm:8080", CredentialsSecretName: "llm-creds"},
			},
			KubernautAgent: KubernautAgentSpec{
				LLMProfileRef: "primary",
				AlignmentCheck: AlignmentCheckSpec{
					Enabled: true,
					// F5: inline LLM block with no working credentials path in v1alpha1.
					LLM: &AlignmentCheckLLMSpec{Provider: "openai", Model: "gpt-4o-mini", Endpoint: "http://llm:8080"},
				},
				ServerRateLimit: &KARateLimitSpec{RequestsPerSecond: &rps},
			},
			Gateway: GatewaySpec{Enabled: ptr.To(true), AlertManagerTokenSecretName: "alertmanager-gateway-token"},
			SignalProcessing: SignalProcessingSpec{
				Policy: PolicyConfigMapRef{ConfigMapName: "sp-policy"},
			},
			RemediationOrchestrator: RemediationOrchestratorSpec{},
			WorkflowExecution:       WorkflowExecutionSpec{WorkflowNamespace: "workflows"},
			EffectivenessMonitor:    EffectivenessMonitorSpec{},
			AIAnalysis:              AIAnalysisSpec{Policy: PolicyConfigMapRef{ConfigMapName: "aa-policy"}},
			APIFrontend: APIFrontendSpec{
				Enabled: ptr.To(true),
				Auth: APIFrontendAuthSpec{
					IssuerURL: "https://idp.example.com/realms/kubernaut",
					// F6 applies to JWTProviderSpec.JWKSURL (each jwtProviders[]
					// entry), which becomes a required field in v1alpha2 -- not
					// this struct's own singular JWKSURL, which stays optional/
					// runtime-derived in both versions and is left as-is by the
					// conversion webhook.
					JWTProviders: []JWTProviderSpec{
						{Name: "rhbk", IssuerURL: "https://idp.example.com/realms/kubernaut"}, // JWKSURL left empty
					},
				},
			},
			// F3: NetworkPolicies explicitly disabled -- today's default.
			NetworkPolicies: NetworkPoliciesSpec{Enabled: ptr.To(false)},
		},
		Status: KubernautStatus{
			Phase:      "Running",
			Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllServicesReady", Message: "ok"}},
			Services:   []ServiceStatus{{Name: "gateway", Ready: true, ReadyReplicas: 1, DesiredReplicas: 1}},
		},
	}
}

var _ = Describe("Kubernaut v1alpha1 <-> v1alpha2 conversion webhook", func() {
	Describe("ConvertTo (v1alpha1 -> v1alpha2, upgrade)", func() {
		It("maps unchanged leaf specs directly", func() {
			src := fullV1alpha1Kubernaut()
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.PostgreSQL.Host).To(Equal("pg.example.com"))
			Expect(dst.Spec.PostgreSQL.SSLMode).To(Equal("verify-full"))
			Expect(dst.Spec.Valkey.SecretName).To(Equal("valkey-secret"))
			Expect(dst.Spec.LLMProfiles).To(HaveKey("primary"))
			Expect(dst.ObjectMeta.Name).To(Equal(SingletonName))
		})

		It("F4: relocates spec.ansible into spec.workflowExecution.ansible", func() {
			src := fullV1alpha1Kubernaut()
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.WorkflowExecution.Ansible.Enabled).To(BeTrue())
			Expect(dst.Spec.WorkflowExecution.Ansible.APIURL).To(Equal("https://awx.example.com"))
			Expect(dst.Spec.WorkflowExecution.Ansible.TokenSecretRef.Name).To(Equal("awx-token"))
		})

		It("#377: carries gateway.alertManagerTokenSecretName over unchanged", func() {
			src := fullV1alpha1Kubernaut()
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.Gateway.AlertManagerTokenSecretName).To(Equal("alertmanager-gateway-token"))
		})

		It("F8: normalizes LoggingSpec.Level to uppercase", func() {
			src := fullV1alpha1Kubernaut()
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.Notification.Logging.Level).To(Equal("INFO"))
		})

		It("F6: derives each jwtProviders[].jwksURL from its issuerURL using the Keycloak convention when empty", func() {
			src := fullV1alpha1Kubernaut()
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.APIFrontend.Auth.JWTProviders).To(HaveLen(1))
			Expect(dst.Spec.APIFrontend.Auth.JWTProviders[0].JWKSURL).
				To(Equal("https://idp.example.com/realms/kubernaut/protocol/openid-connect/certs"))
		})

		It("F6: preserves an explicitly-set jwtProviders[].jwksURL without re-deriving it", func() {
			src := fullV1alpha1Kubernaut()
			src.Spec.APIFrontend.Auth.JWTProviders[0].JWKSURL = "https://idp.example.com/custom-jwks"
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.APIFrontend.Auth.JWTProviders[0].JWKSURL).To(Equal("https://idp.example.com/custom-jwks"))
		})

		It("F5: drops the AlignmentCheck inline LLM block (no working v1alpha1 credentials path)", func() {
			src := fullV1alpha1Kubernaut()
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.KubernautAgent.AlignmentCheck.LLMProfileRef).To(BeEmpty())
			Expect(dst.Spec.KubernautAgent.AlignmentCheck.Enabled).To(BeTrue())
		})

		It("F3: NetworkPolicies has no Enabled field and is always created regardless of the v1alpha1 value", func() {
			src := fullV1alpha1Kubernaut()
			Expect(*src.Spec.NetworkPolicies.Enabled).To(BeFalse(), "fixture must start from the disabled case")
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.NetworkPolicies).To(Equal(v1alpha2.NetworkPoliciesSpec{}))
		})

		DescribeTable("backfills policy.configMapName with the resource-builder default when a v1alpha1 CR omitted it "+
			"(v1alpha2 makes aiAnalysis/signalProcessing themselves required, closing the loophole where omitting "+
			"the whole block bypassed Policy's own required-ness -- see KubernautSpec.AIAnalysis doc comment)",
			func(clearPolicy func(*Kubernaut), getV2ConfigMapName func(*v1alpha2.Kubernaut) string, wantDefault string) {
				src := fullV1alpha1Kubernaut()
				clearPolicy(src)
				dst := &v1alpha2.Kubernaut{}
				Expect(src.ConvertTo(dst)).To(Succeed())

				Expect(getV2ConfigMapName(dst)).To(Equal(wantDefault))
			},
			Entry("AIAnalysis",
				func(k *Kubernaut) { k.Spec.AIAnalysis.Policy.ConfigMapName = "" },
				func(k *v1alpha2.Kubernaut) string { return k.Spec.AIAnalysis.Policy.ConfigMapName },
				defaultAIAnalysisPolicyConfigMapName),
			Entry("SignalProcessing",
				func(k *Kubernaut) { k.Spec.SignalProcessing.Policy.ConfigMapName = "" },
				func(k *v1alpha2.Kubernaut) string { return k.Spec.SignalProcessing.Policy.ConfigMapName },
				defaultSignalProcessingPolicyConfigMapName),
		)

		It("preserves an explicitly-set aiAnalysis/signalProcessing policy.configMapName without backfilling it", func() {
			src := fullV1alpha1Kubernaut()
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.AIAnalysis.Policy.ConfigMapName).To(Equal("aa-policy"))
			Expect(dst.Spec.SignalProcessing.Policy.ConfigMapName).To(Equal("sp-policy"))
		})

		DescribeTable("Fleet migration: v1alpha2's per-component Fleet override is always nil (no v1alpha1 source)",
			func(getV2Fleet func(*v1alpha2.Kubernaut) *v1alpha2.FleetOverrideSpec) {
				src := fullV1alpha1Kubernaut()
				dst := &v1alpha2.Kubernaut{}
				Expect(src.ConvertTo(dst)).To(Succeed())

				Expect(getV2Fleet(dst)).To(BeNil())
			},
			Entry("KubernautAgent", func(k *v1alpha2.Kubernaut) *v1alpha2.FleetOverrideSpec { return k.Spec.KubernautAgent.Fleet }),
			Entry("Gateway", func(k *v1alpha2.Kubernaut) *v1alpha2.FleetOverrideSpec { return k.Spec.Gateway.Fleet }),
			Entry("RemediationOrchestrator", func(k *v1alpha2.Kubernaut) *v1alpha2.FleetOverrideSpec { return k.Spec.RemediationOrchestrator.Fleet }),
			Entry("EffectivenessMonitor", func(k *v1alpha2.Kubernaut) *v1alpha2.FleetOverrideSpec { return k.Spec.EffectivenessMonitor.Fleet }),
			Entry("APIFrontend", func(k *v1alpha2.Kubernaut) *v1alpha2.FleetOverrideSpec { return k.Spec.APIFrontend.Fleet }),
			Entry("SignalProcessing", func(k *v1alpha2.Kubernaut) *v1alpha2.FleetOverrideSpec { return k.Spec.SignalProcessing.Fleet }),
		)

		It("Fleet migration: spec.fleet and spec.fleetMetadataCache are always zero-valued (no v1alpha1 source)", func() {
			src := fullV1alpha1Kubernaut()
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.Fleet).To(Equal(v1alpha2.FleetSpec{}))
			Expect(dst.Spec.FleetMetadataCache).To(Equal(v1alpha2.FleetMetadataCacheSpec{}))
		})

		It("F2: leaves the new top-level Monitoring field unset (no v1alpha1 source)", func() {
			src := fullV1alpha1Kubernaut()
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.Monitoring).To(Equal(v1alpha2.MonitoringSpec{}))
		})

		It("carries apiFrontend.rbac.roleBindings over unchanged", func() {
			src := fullV1alpha1Kubernaut()
			src.Spec.APIFrontend.RBAC = &APIFrontendRBACSpec{
				RoleBindings: []ToolRoleBinding{{Role: "sre", ClusterRoleName: "sre-role", Groups: []string{"platform-engineering"}}},
			}
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Spec.APIFrontend.RBAC).NotTo(BeNil())
			Expect(dst.Spec.APIFrontend.RBAC.RoleBindings).To(ConsistOf(
				v1alpha2.ToolRoleBinding{Role: "sre", ClusterRoleName: "sre-role", Groups: []string{"platform-engineering"}},
			))

			roundTripped := &Kubernaut{}
			Expect(roundTripped.ConvertFrom(dst)).To(Succeed())
			Expect(roundTripped.Spec.APIFrontend.RBAC.RoleBindings).To(Equal(src.Spec.APIFrontend.RBAC.RoleBindings))
		})

		It("carries status over unchanged", func() {
			src := fullV1alpha1Kubernaut()
			dst := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(string(dst.Status.Phase)).To(Equal("Running"))
			Expect(dst.Status.Services).To(HaveLen(1))
			Expect(dst.Status.Services[0].Name).To(Equal("gateway"))
		})
	})

	Describe("ConvertFrom (v1alpha2 -> v1alpha1, downgrade)", func() {
		It("F4: relocates spec.workflowExecution.ansible back to the top-level spec.ansible", func() {
			hub := &v1alpha2.Kubernaut{Spec: v1alpha2.KubernautSpec{
				WorkflowExecution: v1alpha2.WorkflowExecutionSpec{
					Ansible: v1alpha2.AnsibleSpec{Enabled: true, APIURL: "https://awx.example.com"},
				},
			}}
			dst := &Kubernaut{}
			Expect(dst.ConvertFrom(hub)).To(Succeed())

			Expect(dst.Spec.Ansible.Enabled).To(BeTrue())
			Expect(dst.Spec.Ansible.APIURL).To(Equal("https://awx.example.com"))
		})

		It("F3: sets NetworkPolicies.Enabled=true unconditionally, reflecting v1alpha2's always-on behavior", func() {
			hub := &v1alpha2.Kubernaut{}
			dst := &Kubernaut{}
			Expect(dst.ConvertFrom(hub)).To(Succeed())

			Expect(dst.Spec.NetworkPolicies.Enabled).NotTo(BeNil())
			Expect(*dst.Spec.NetworkPolicies.Enabled).To(BeTrue())
		})

		It("F5: reconstructs the AlignmentCheck inline LLM block from the referenced profile", func() {
			hub := &v1alpha2.Kubernaut{Spec: v1alpha2.KubernautSpec{
				LLMProfiles: map[string]v1alpha2.LLMProfileSpec{
					"align": {Provider: "anthropic", Model: "claude", Endpoint: "http://llm:9090"},
				},
				KubernautAgent: v1alpha2.KubernautAgentSpec{
					AlignmentCheck: v1alpha2.AlignmentCheckSpec{Enabled: true, LLMProfileRef: "align"},
				},
			}}
			dst := &Kubernaut{}
			Expect(dst.ConvertFrom(hub)).To(Succeed())

			Expect(dst.Spec.KubernautAgent.AlignmentCheck.LLM).NotTo(BeNil())
			Expect(dst.Spec.KubernautAgent.AlignmentCheck.LLM.Provider).To(Equal("anthropic"))
			Expect(dst.Spec.KubernautAgent.AlignmentCheck.LLM.Endpoint).To(Equal("http://llm:9090"))
		})

		It("F5: leaves the inline LLM block nil when llmProfileRef points at a profile that no longer exists", func() {
			hub := &v1alpha2.Kubernaut{Spec: v1alpha2.KubernautSpec{
				KubernautAgent: v1alpha2.KubernautAgentSpec{
					AlignmentCheck: v1alpha2.AlignmentCheckSpec{Enabled: true, LLMProfileRef: "missing"},
				},
			}}
			dst := &Kubernaut{}
			Expect(dst.ConvertFrom(hub)).To(Succeed())

			Expect(dst.Spec.KubernautAgent.AlignmentCheck.LLM).To(BeNil())
		})

		It("Fleet migration: drops a fully-configured spec.fleet/fleetMetadataCache without erroring (no v1alpha1 destination)", func() {
			hub := &v1alpha2.Kubernaut{Spec: v1alpha2.KubernautSpec{
				Fleet: v1alpha2.FleetSpec{
					Enabled: ptr.To(true), Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
					MCPGatewayEndpoint: "https://mcp-gateway.example.com/sse", MCPGatewayType: "eaigw",
				},
				Gateway: v1alpha2.GatewaySpec{Fleet: &v1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: "gw-secret"}},
			}}
			dst := &Kubernaut{}
			Expect(dst.ConvertFrom(hub)).To(Succeed(), "downgrading a CR with Fleet configured must not error -- Fleet is simply dropped, logged as lossy")
		})
	})

	Describe("round-trip fidelity (v1alpha1 -> v1alpha2 -> v1alpha1)", func() {
		It("preserves fields unaffected by F1-F8 exactly", func() {
			src := fullV1alpha1Kubernaut()
			hub := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(hub)).To(Succeed())
			roundTripped := &Kubernaut{}
			Expect(roundTripped.ConvertFrom(hub)).To(Succeed())

			Expect(roundTripped.Spec.PostgreSQL).To(Equal(src.Spec.PostgreSQL))
			Expect(roundTripped.Spec.Valkey).To(Equal(src.Spec.Valkey))
			Expect(roundTripped.Spec.LLMProfiles).To(Equal(src.Spec.LLMProfiles))
			Expect(roundTripped.Spec.Ansible).To(Equal(src.Spec.Ansible))
			Expect(roundTripped.Status).To(Equal(src.Status))
		})

		It("#377: preserves gateway.alertManagerTokenSecretName through a full round-trip", func() {
			src := fullV1alpha1Kubernaut()
			hub := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(hub)).To(Succeed())
			roundTripped := &Kubernaut{}
			Expect(roundTripped.ConvertFrom(hub)).To(Succeed())

			Expect(roundTripped.Spec.Gateway.AlertManagerTokenSecretName).To(Equal(src.Spec.Gateway.AlertManagerTokenSecretName))
		})

		It("documents the F3 exception: an explicitly-disabled NetworkPolicies does not round-trip", func() {
			src := fullV1alpha1Kubernaut()
			Expect(*src.Spec.NetworkPolicies.Enabled).To(BeFalse())
			hub := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(hub)).To(Succeed())
			roundTripped := &Kubernaut{}
			Expect(roundTripped.ConvertFrom(hub)).To(Succeed())

			// This is the intended, documented behavior change (ADR-CRD-001 F3):
			// v1alpha2 has no opt-out, so downgrading back to v1alpha1 reports
			// the only value that reflects actual runtime behavior: enabled.
			Expect(*roundTripped.Spec.NetworkPolicies.Enabled).To(BeTrue())
		})

		It("documents the F5 exception: an inline AlignmentCheck LLM block does not round-trip", func() {
			src := fullV1alpha1Kubernaut()
			Expect(src.Spec.KubernautAgent.AlignmentCheck.LLM).NotTo(BeNil())
			hub := &v1alpha2.Kubernaut{}
			Expect(src.ConvertTo(hub)).To(Succeed())
			roundTripped := &Kubernaut{}
			Expect(roundTripped.ConvertFrom(hub)).To(Succeed())

			// F5: v1alpha1's inline LLM block never had a working credentials
			// path, so there was nothing to preserve a reference to; this
			// downgrade correctly comes back empty rather than fabricating one.
			Expect(roundTripped.Spec.KubernautAgent.AlignmentCheck.LLM).To(BeNil())
		})
	})

	Describe("conversion webhook HTTP wiring", func() {
		// This exercises the exact conversion.NewWebhookHandler(scheme, registry)
		// object cmd/main.go registers at /convert (see registerConversionWebhook),
		// end-to-end through its real HTTP ServeHTTP dispatch -- not just this
		// package's own ConvertTo/ConvertFrom helpers -- proving the Hub/
		// Convertible interfaces are wired correctly for controller-runtime's
		// shared conversion webhook machinery to find and use. No live TLS/OLM
		// server is needed for this: it is the same pattern this codebase
		// already uses for admission webhooks (see internal/webhook), which
		// are also never exercised through a live envtest webhook server.
		var scheme *runtime.Scheme

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			Expect(AddToScheme(scheme)).To(Succeed())
			Expect(v1alpha2.AddToScheme(scheme)).To(Succeed())
		})

		postConvertReview := func(handler http.Handler, req *apiextensionsv1.ConversionRequest) *apiextensionsv1.ConversionReview {
			body, err := json.Marshal(&apiextensionsv1.ConversionReview{Request: req})
			Expect(err).NotTo(HaveOccurred())

			httpReq := httptest.NewRequest(http.MethodPost, "/convert", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httpReq)
			Expect(rec.Code).To(Equal(http.StatusOK))

			resp := &apiextensionsv1.ConversionReview{}
			Expect(json.Unmarshal(rec.Body.Bytes(), resp)).To(Succeed())
			return resp
		}

		It("converts a v1alpha1 object to v1alpha2 via the real HTTP handler", func() {
			handler := conversion.NewWebhookHandler(scheme, conversion.NewRegistry())

			src := fullV1alpha1Kubernaut()
			src.TypeMeta = metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "Kubernaut"}
			raw, err := json.Marshal(src)
			Expect(err).NotTo(HaveOccurred())

			review := postConvertReview(handler, &apiextensionsv1.ConversionRequest{
				UID:               "test-uid-1",
				DesiredAPIVersion: v1alpha2.GroupVersion.String(),
				Objects:           []runtime.RawExtension{{Raw: raw}},
			})

			Expect(review.Response).NotTo(BeNil())
			Expect(review.Response.UID).To(BeEquivalentTo("test-uid-1"))
			Expect(review.Response.Result.Status).To(Equal(metav1.StatusSuccess))
			Expect(review.Response.ConvertedObjects).To(HaveLen(1))

			converted := &v1alpha2.Kubernaut{}
			Expect(json.Unmarshal(review.Response.ConvertedObjects[0].Raw, converted)).To(Succeed())
			Expect(converted.Spec.PostgreSQL.Host).To(Equal("pg.example.com"))
			Expect(converted.Spec.WorkflowExecution.Ansible.APIURL).To(Equal("https://awx.example.com"))
		})

		It("converts a v1alpha2 object back to v1alpha1 via the real HTTP handler", func() {
			handler := conversion.NewWebhookHandler(scheme, conversion.NewRegistry())

			hub := &v1alpha2.Kubernaut{
				TypeMeta: metav1.TypeMeta{APIVersion: v1alpha2.GroupVersion.String(), Kind: "Kubernaut"},
				Spec: v1alpha2.KubernautSpec{
					PostgreSQL: v1alpha2.PostgreSQLSpec{Host: "pg.example.com", Port: 5432},
				},
			}
			raw, err := json.Marshal(hub)
			Expect(err).NotTo(HaveOccurred())

			review := postConvertReview(handler, &apiextensionsv1.ConversionRequest{
				UID:               "test-uid-2",
				DesiredAPIVersion: GroupVersion.String(),
				Objects:           []runtime.RawExtension{{Raw: raw}},
			})

			Expect(review.Response.Result.Status).To(Equal(metav1.StatusSuccess))
			Expect(review.Response.ConvertedObjects).To(HaveLen(1))

			converted := &Kubernaut{}
			Expect(json.Unmarshal(review.Response.ConvertedObjects[0].Raw, converted)).To(Succeed())
			Expect(converted.Spec.PostgreSQL.Host).To(Equal("pg.example.com"))
			// F3: proves the same round-trip behavior change is visible through
			// the real HTTP handler, not just the Go-level ConvertFrom helper.
			Expect(converted.Spec.NetworkPolicies.Enabled).NotTo(BeNil())
			Expect(*converted.Spec.NetworkPolicies.Enabled).To(BeTrue())
		})
	})
})

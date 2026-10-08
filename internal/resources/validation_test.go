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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	malformedURL          = "not-a-url"
	testTelemetryCertFile = "/etc/telemetry/tls.crt"
)

var _ = Describe("IA-2: AF multi-provider JWT authentication", func() {
	withAFProviders := func(providers []kubernautv1alpha2.JWTProviderSpec) *kubernautv1alpha2.Kubernaut {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = providers
		return kn
	}

	keycloakProvider := kubernautv1alpha2.JWTProviderSpec{
		Name:      "keycloak",
		IssuerURL: "https://keycloak.example.com/realms/kubernaut",
		JWKSURL:   "https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs",
		Audiences: []string{"kubernaut-console"},
	}

	spireProvider := kubernautv1alpha2.JWTProviderSpec{
		Name:      "spire",
		IssuerURL: "https://spire.example.com",
		Audiences: []string{"kubernaut-workload"},
	}

	It("IA-2: accepts AF with multiple concurrent OIDC providers for multi-source authentication", func() {
		kn := withAFProviders([]kubernautv1alpha2.JWTProviderSpec{keycloakProvider, spireProvider})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(),
			"IA-2: platform must support concurrent JWT validation from multiple OIDC issuers")
	})

	It("IA-2: satisfies authentication requirement when jwtProviders replaces top-level issuerURL", func() {
		kn := withAFProviders([]kubernautv1alpha2.JWTProviderSpec{keycloakProvider})
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(),
			"IA-2: multi-provider config is sufficient — top-level issuerURL not required")
	})

	It("IA-2: rejects AF deployment without any authentication source", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		kn.Spec.APIFrontend.Auth.JWTProviders = nil
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("IA-2"),
			"IA-2: error must reference FedRAMP control when no auth source is configured")
	})
})

var _ = Describe("SC-23: per-provider audience binding", func() {
	It("SC-23: rejects provider without audience binding — tokens would lack session authenticity", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{
			{
				Name:      "no-audience",
				IssuerURL: "https://idp.example.com",
				Audiences: []string{},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).NotTo(BeEmpty())
		Expect(errs[0].Error()).To(ContainSubstring("audiences"),
			"SC-23: validation must require at least one audience for session authenticity")
	})

	It("SC-23: accepts provider with multiple audiences for federated token validation", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{
			{
				Name:      "multi-aud",
				IssuerURL: "https://idp.example.com",
				Audiences: []string{"kubernaut-console", "kubernaut-api"},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(),
			"SC-23: multi-audience binding is valid for federated token validation")
	})
})

var _ = Describe("SC-8: JWKS endpoint transmission confidentiality", func() {
	It("SC-8: rejects non-TLS JWKS endpoint for AF provider", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{
			{
				Name:      "insecure",
				IssuerURL: "https://idp.example.com",
				JWKSURL:   "http://idp.example.com/jwks",
				Audiences: []string{"kubernaut"},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("scheme must be https"),
			"SC-8: token signature verification must use encrypted channel")
		Expect(errs[0].Error()).To(ContainSubstring("allowInsecureIssuers"),
			"SC-8: error must reference AF-specific insecure flag")
	})

	It("SC-8: accepts non-TLS JWKS endpoint when insecure issuers explicitly allowed", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.AllowInsecureIssuers = true
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{
			{
				Name:      "dev",
				IssuerURL: "https://idp.example.com",
				JWKSURL:   "http://idp.example.com/jwks",
				Audiences: []string{"kubernaut"},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(),
			"SC-8: dev/test environments may use HTTP JWKS when explicitly opted in")
	})

	It("SC-8: accepts HTTPS JWKS endpoint for AF provider", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{
			{
				Name:      "secure",
				IssuerURL: "https://idp.example.com",
				JWKSURL:   "https://idp.example.com/jwks",
				Audiences: []string{"kubernaut"},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(),
			"SC-8: HTTPS JWKS endpoints satisfy transmission confidentiality")
	})
})

var _ = Describe("CM-6: provider identity uniqueness", func() {
	It("CM-6: rejects duplicate provider names — configuration must be unambiguous", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{
			{Name: "keycloak", IssuerURL: "https://kc1.example.com", Audiences: []string{"aud1"}},
			{Name: "keycloak", IssuerURL: "https://kc2.example.com", Audiences: []string{"aud2"}},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).NotTo(BeEmpty())
		Expect(errs[0].Error()).To(ContainSubstring("duplicate"),
			"CM-6: duplicate provider names create ambiguous configuration")
	})

	It("CM-6: rejects provider without issuer identity — configuration incomplete", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha2.JWTProviderSpec{
			{Name: "no-issuer", IssuerURL: "", Audiences: []string{"kubernaut"}},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).NotTo(BeEmpty())
		Expect(errs[0].Error()).To(ContainSubstring("issuerURL"),
			"CM-6: each provider must have a non-empty issuerURL for deterministic config")
	})
})

var _ = Describe("PostgreSQL SSLMode Validation", func() {
	It("rejects sslMode=disable", func() {
		kn := testKubernaut()
		kn.Spec.PostgreSQL.SSLMode = "disable"
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("disable"))
		Expect(errs[0].Error()).To(ContainSubstring("SC-8"))
	})

	It("accepts sslMode=verify-full", func() {
		kn := testKubernaut()
		kn.Spec.PostgreSQL.SSLMode = DefaultSSLMode
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts sslMode=verify-ca", func() {
		kn := testKubernaut()
		kn.Spec.PostgreSQL.SSLMode = "verify-ca"
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts empty sslMode (defaults to verify-full in ConfigMap)", func() {
		kn := testKubernaut()
		kn.Spec.PostgreSQL.SSLMode = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})
})

var _ = Describe("APIFrontend Validation", func() {
	It("rejects invalid agentCardURL", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.AgentCardURL = malformedURL
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("agentCardURL"))
	})

	It("accepts valid agentCardURL", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.AgentCardURL = "https://kubernaut.example.com/.well-known/agent.json"
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts empty agentCardURL", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.AgentCardURL = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects empty configMapName in rbacRolesConfigMapRef", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBACRolesConfigMapRef = &kubernautv1alpha2.ConfigMapRef{ConfigMapName: ""} //nolint:staticcheck // exercising deprecated-field backward compat
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("configMapName"))
	})

	It("rejects AF without OAuth/OIDC issuerURL (FedRAMP IA-2, CM-6)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("issuerURL"))
		Expect(errs[0].Error()).To(ContainSubstring("OAuth/OIDC"))
		Expect(errs[0].Error()).To(ContainSubstring("IA-2"))
	})

	It("accepts valid issuerURL when AF is enabled", func() {
		kn := testKubernautWithAF()
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("IA-2: skips issuerURL requirement when kagenti authbridge sidecar is active", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		errs := ValidateKubernaut(kn, KagentiSidecarAuthbridge)
		Expect(errs).To(BeEmpty(),
			"IA-2: issuerURL must not be required when kagenti auto-detection is available")
	})

	It("IA-2: skips issuerURL requirement when kagenti envoy sidecar is active", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		errs := ValidateKubernaut(kn, KagentiSidecarEnvoy)
		Expect(errs).To(BeEmpty(),
			"IA-2: issuerURL must not be required when kagenti auto-detection is available (envoy mode)")
	})

	It("skips issuerURL check when AF is disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		kn.Spec.APIFrontend.Auth.IssuerURL = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("skips validation when AF is disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		kn.Spec.APIFrontend.AgentCardURL = malformedURL
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})
})

var _ = Describe("ToolRoleBinding Validation", func() {
	It("rejects duplicate role names in roleBindings", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha2.ToolRoleBinding{
				{Role: "sre", Groups: []string{"team-a"}},
				{Role: "sre", Groups: []string{"team-b"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("duplicate"))
	})

	It("accepts valid roleBindings with known persona names", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha2.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-team"}},
				{Role: "cicd", Groups: []string{"ci-bots"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts empty roleBindings list", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects unknown persona name in roleBindings", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha2.ToolRoleBinding{
				{Role: "unknown-persona", Groups: []string{"team-x"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("unknown"))
	})

	It("rejects invalid sarCacheTTL format", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{
			SARCacheTTL: "not-a-duration",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("sarCacheTTL"))
	})

	// --- Issue #181: Custom ClusterRole references ---

	It("[AC-3] rejects roleBinding with both role and clusterRoleName set", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha2.ToolRoleBinding{
				{Role: "sre", ClusterRoleName: "my-custom-role", Groups: []string{"team-a"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("mutually exclusive"))
	})

	It("[AC-3] rejects roleBinding with neither role nor clusterRoleName set", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha2.ToolRoleBinding{
				{Groups: []string{"team-a"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("one of role or clusterRoleName"))
	})

	It("[AC-3] accepts roleBinding with only clusterRoleName set", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha2.ToolRoleBinding{
				{ClusterRoleName: "my-custom-role", Groups: []string{"team-a"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("[AC-6] accepts mixed persona and custom clusterRoleName bindings", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha2.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-team"}},
				{ClusterRoleName: "my-custom-role", Groups: []string{"custom-team"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})
})

var _ = Describe("AlignmentCheck Validation", func() {
	withAlignmentCheck := func(ac kubernautv1alpha2.AlignmentCheckSpec) *kubernautv1alpha2.Kubernaut {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.AlignmentCheck = ac
		return kn
	}

	It("skips validation when alignmentCheck is disabled", func() {
		kn := withAlignmentCheck(kubernautv1alpha2.AlignmentCheckSpec{
			Enabled: false,
			Timeout: "not-a-duration",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts valid alignmentCheck configuration", func() {
		kn := withAlignmentCheck(kubernautv1alpha2.AlignmentCheckSpec{
			Enabled:       true,
			Timeout:       "10s",
			MaxStepTokens: 500,
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects invalid timeout duration", func() {
		kn := withAlignmentCheck(kubernautv1alpha2.AlignmentCheckSpec{
			Enabled: true,
			Timeout: "not-a-duration",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("timeout"))
		Expect(errs[0].Error()).To(ContainSubstring("invalid Go duration"))
	})

	It("rejects timeout below 1s", func() {
		kn := withAlignmentCheck(kubernautv1alpha2.AlignmentCheckSpec{
			Enabled: true,
			Timeout: "500ms",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("timeout"))
		Expect(errs[0].Error()).To(ContainSubstring("between"))
	})

	It("rejects timeout above 60s", func() {
		kn := withAlignmentCheck(kubernautv1alpha2.AlignmentCheckSpec{
			Enabled: true,
			Timeout: "120s",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("timeout"))
		Expect(errs[0].Error()).To(ContainSubstring("between"))
	})

	It("accepts timeout at lower bound (1s)", func() {
		kn := withAlignmentCheck(kubernautv1alpha2.AlignmentCheckSpec{
			Enabled: true,
			Timeout: "1s",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts timeout at upper bound (60s)", func() {
		kn := withAlignmentCheck(kubernautv1alpha2.AlignmentCheckSpec{
			Enabled: true,
			Timeout: "60s",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects negative maxStepTokens", func() {
		kn := withAlignmentCheck(kubernautv1alpha2.AlignmentCheckSpec{
			Enabled:       true,
			MaxStepTokens: -1,
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("maxStepTokens"))
		Expect(errs[0].Error()).To(ContainSubstring("positive"))
	})

	It("accepts an alignment LLM profile reference", func() {
		kn := withAlignmentCheck(kubernautv1alpha2.AlignmentCheckSpec{
			Enabled:       true,
			LLMProfileRef: "primary",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accumulates multiple errors", func() {
		kn := withAlignmentCheck(kubernautv1alpha2.AlignmentCheckSpec{
			Enabled:       true,
			Timeout:       "not-a-duration",
			MaxStepTokens: -1,
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(2))
	})
})

var _ = Describe("DryRun Validation", func() {
	It("skips validation when dryRun is disabled", func() {
		kn := testKubernaut()
		kn.Spec.RemediationOrchestrator.DryRun = false
		kn.Spec.RemediationOrchestrator.DryRunHoldPeriod = "not-a-duration"
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts valid dryRunHoldPeriod", func() {
		kn := testKubernaut()
		kn.Spec.RemediationOrchestrator.DryRun = true
		kn.Spec.RemediationOrchestrator.DryRunHoldPeriod = "1h"
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects invalid dryRunHoldPeriod", func() {
		kn := testKubernaut()
		kn.Spec.RemediationOrchestrator.DryRun = true
		kn.Spec.RemediationOrchestrator.DryRunHoldPeriod = "not-a-duration"
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("dryRunHoldPeriod"))
		Expect(errs[0].Error()).To(ContainSubstring("invalid Go duration"))
	})

	It("accepts empty dryRunHoldPeriod (uses kubebuilder default)", func() {
		kn := testKubernaut()
		kn.Spec.RemediationOrchestrator.DryRun = true
		kn.Spec.RemediationOrchestrator.DryRunHoldPeriod = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})
})

var _ = Describe("Interactive Mode Validation", func() {
	boolPtr := func(v bool) *bool { return &v }

	withInteractiveMode := func(spec kubernautv1alpha2.InteractiveSpec) *kubernautv1alpha2.Kubernaut {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.Interactive = &spec
		return kn
	}

	It("skips validation when interactive is nil", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.Interactive = nil
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("skips validation when interactive is disabled", func() {
		kn := withInteractiveMode(kubernautv1alpha2.InteractiveSpec{
			Enabled:    boolPtr(false),
			SessionTTL: "not-a-duration",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts valid interactive configuration", func() {
		kn := withInteractiveMode(kubernautv1alpha2.InteractiveSpec{
			Enabled:           boolPtr(true),
			SessionTTL:        "30m",
			InactivityTimeout: "10m",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects invalid sessionTTL", func() {
		kn := withInteractiveMode(kubernautv1alpha2.InteractiveSpec{
			Enabled:    boolPtr(true),
			SessionTTL: "not-a-duration",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("sessionTTL"))
	})

	It("rejects invalid inactivityTimeout", func() {
		kn := withInteractiveMode(kubernautv1alpha2.InteractiveSpec{
			Enabled:           boolPtr(true),
			InactivityTimeout: "not-a-duration",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("inactivityTimeout"))
	})

	It("accumulates multiple duration errors", func() {
		kn := withInteractiveMode(kubernautv1alpha2.InteractiveSpec{
			Enabled:           boolPtr(true),
			SessionTTL:        "bad1",
			InactivityTimeout: "bad2",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(2))
	})
})

var _ = Describe("Policy Prerequisite Validation", func() {
	It("accepts empty policy configMapNames (defaults used)", func() {
		kn := testKubernaut()
		kn.Spec.AIAnalysis.Policy.ConfigMapName = ""
		kn.Spec.SignalProcessing.Policy.ConfigMapName = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts user-provided policy configMapNames", func() {
		kn := testKubernaut()
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})
})

var _ = Describe("LLM Profile Content Validation", func() {
	It("rejects empty provider", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Provider = ""
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring(`llmProfiles["primary"].provider`))
	})

	It("rejects empty model", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Model = ""
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring(`llmProfiles["primary"].model`))
	})

	It("rejects empty credentialsSecretName", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.CredentialsSecretName = ""
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring(`llmProfiles["primary"].credentialsSecretName`))
	})

	It("accumulates all missing fields for one profile", func() {
		kn := testKubernaut()
		kn.Spec.LLMProfiles["primary"] = kubernautv1alpha2.LLMProfileSpec{}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(3))
	})

	It("accepts a valid profile", func() {
		kn := testKubernaut()
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("UT-VL-196-001 [SI-10]: provider openai without endpoint fails validation", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Provider = LLMProviderOpenAI
		profile.Endpoint = ""
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		endpointErr := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "endpoint") {
				endpointErr = true
			}
		}
		Expect(endpointErr).To(BeTrue(),
			"provider openai must require endpoint (both KA and AF need it)")
	})

	It("UT-VL-196-002 [SI-10]: provider openai with endpoint passes validation", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Provider = LLMProviderOpenAI
		profile.Endpoint = "http://llm-gateway:8080"
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(),
			"provider openai with endpoint and credentials should pass validation")
	})

	It("rejects tlsCertFile set without tlsKeyFile", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.TLSCertFile = testMTLSCertFile
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "tlsCertFile") && strings.Contains(e.Error(), "tlsKeyFile") {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})

	It("rejects mTLS cert/key pair set without tlsClientSecretRef", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.TLSCertFile = testMTLSCertFile
		profile.TLSKeyFile = testMTLSKeyFile
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "tlsClientSecretRef") {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})

	It("rejects tlsClientSecretRef set without an mTLS cert/key pair", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.TLSClientSecretRef = testVolumeLLMTLSClient
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "tlsClientSecretRef") {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})

	It("accepts a complete mTLS configuration", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.TLSCertFile = testMTLSCertFile
		profile.TLSKeyFile = testMTLSKeyFile
		profile.TLSClientSecretRef = testVolumeLLMTLSClient
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("LR-040 [SI-10]: an administrator may explicitly disable reasoning while still declaring an effort tier for later re-enablement", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Provider = LLMProviderAnthropic
		profile.Reasoning = &kubernautv1alpha2.LLMReasoningSpec{Enabled: false, Effort: "none"}
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(), "SI-10: effort:none with enabled:false is not a contradiction (reasoning is off, so no wire-level conflict exists)")
	})

	It("LR-041 [SI-10]: an administrator may enable Anthropic's lowest real reasoning tier without being incorrectly blocked", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Provider = LLMProviderAnthropic
		profile.Reasoning = &kubernautv1alpha2.LLMReasoningSpec{Enabled: true, Effort: "minimal"}
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(), "SI-10: minimal is Anthropic's lowest real tier, not a contradiction — validation must not false-positive on legitimate configurations")
	})

	It("LR-042 [SI-10]: rejects a profile that would deploy successfully but fail every Anthropic LLM call at runtime, and names which profile to fix", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Provider = LLMProviderAnthropic
		profile.Reasoning = &kubernautv1alpha2.LLMReasoningSpec{Enabled: true, Effort: "none"}
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).NotTo(BeEmpty(), "SI-10: anthropic has no \"thinking enabled, zero effort\" wire state — left undetected here, this ships a CR that reconciles cleanly but causes KA/AF to fail every LLM call against Anthropic's API at runtime, only surfacing as a production incident")
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), `llmProfiles["primary"]`) && strings.Contains(e.Error(), "reasoning") {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "SI-10: the error must name the offending profile so an operator with dozens of profiles can find and fix it without trial-and-error, got: %v", errs)
	})

	It("LR-043 [SI-10]: rejects the same runtime-failure-inducing contradiction for Vertex-hosted Claude, not just native Anthropic", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Provider = LLMProviderVertexAI
		profile.VertexProject = "example-gcp-project"
		profile.VertexLocation = testVertexLocation
		profile.Reasoning = &kubernautv1alpha2.LLMReasoningSpec{Enabled: true, Effort: "none"}
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "reasoning") {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "SI-10: vertex_ai routes to the same Claude thinking API as native anthropic, so it shares the same runtime-failure risk and must be caught the same way, got: %v", errs)
	})

	It("LR-044 [SI-10]: does not block a legitimate OpenAI configuration that has no analogous wire-level conflict", func() {
		kn := testKubernaut()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.Provider = LLMProviderOpenAI
		profile.Endpoint = "http://llm-gateway:8080"
		profile.Reasoning = &kubernautv1alpha2.LLMReasoningSpec{Enabled: true, Effort: "none"}
		kn.Spec.LLMProfiles["primary"] = profile
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(), "SI-10: the none+enabled runtime-failure contradiction is specific to Anthropic-family providers — validation must not over-broadly reject OpenAI configurations that have no such conflict")
	})
})

var _ = Describe("LLM Profile Referential Integrity", func() {
	It("accepts a missing kubernautAgent.llmProfileRef when spec.llmProfiles defines exactly one profile (auto-inferred)", func() {
		kn := testKubernaut() // testKubernaut() defines exactly one profile ("primary")
		kn.Spec.KubernautAgent.LLMProfileRef = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
		Expect(EffectiveKALLMProfileRef(kn)).To(Equal("primary"),
			"the sole entry in spec.llmProfiles must be inferred, not a fixed conventional name")
	})

	It("rejects a missing kubernautAgent.llmProfileRef when spec.llmProfiles defines more than one profile (ambiguous)", func() {
		kn := testKubernaut()
		kn.Spec.LLMProfiles["secondary"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: LLMProviderOpenAI, Model: "gpt-4o-mini", CredentialsSecretName: "llm-creds-2",
		}
		kn.Spec.KubernautAgent.LLMProfileRef = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "kubernautAgent.llmProfileRef") &&
				strings.Contains(e.Error(), "required") && strings.Contains(e.Error(), "ambiguous") {
				found = true
			}
		}
		Expect(found).To(BeTrue())
		Expect(EffectiveKALLMProfileRef(kn)).To(BeEmpty())
	})

	It("rejects a missing kubernautAgent.llmProfileRef when spec.llmProfiles is empty", func() {
		kn := testKubernaut()
		kn.Spec.LLMProfiles = map[string]kubernautv1alpha2.LLMProfileSpec{}
		kn.Spec.KubernautAgent.LLMProfileRef = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "kubernautAgent.llmProfileRef") && strings.Contains(e.Error(), "required") {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})

	It("rejects kubernautAgent.llmProfileRef referencing an undefined profile", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.LLMProfileRef = "does-not-exist"
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("kubernautAgent.llmProfileRef"))
		Expect(errs[0].Error()).To(ContainSubstring(`"does-not-exist"`))
	})

	It("accepts kubernautAgent.llmProfileRef referencing a defined profile", func() {
		kn := testKubernaut()
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects kubernautAgent.llmProfileRef when spec.llmProfiles is empty", func() {
		kn := testKubernaut()
		kn.Spec.LLMProfiles = nil
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1), "an empty spec.llmProfiles map must not panic and should surface exactly the undefined-profile error")
		Expect(errs[0].Error()).To(ContainSubstring("kubernautAgent.llmProfileRef"))
		Expect(errs[0].Error()).To(ContainSubstring(`"primary"`))
	})

	It("rejects apiFrontend.llmProfileRef referencing an undefined profile", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.LLMProfileRef = "does-not-exist"
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "apiFrontend.llmProfileRef") {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})

	It("accepts an empty apiFrontend.llmProfileRef (defaults to KA's profile)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.LLMProfileRef = ""
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts apiFrontend.llmProfileRef referencing its own defined profile", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["af-profile"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: LLMProviderVertexAI, Model: "gemini-2.5-flash", CredentialsSecretName: "af-llm-creds",
		}
		kn.Spec.APIFrontend.LLMProfileRef = "af-profile"
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects invalid phaseModels key", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.PhaseModels = map[string]string{"banana": "primary"}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring(`invalid phase key "banana"`))
	})

	It("reports multiple invalid phaseModels keys", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.PhaseModels = map[string]string{
			"rca": "primary", "banana": "primary", "unknown": "primary",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(2))
	})

	It("accepts empty phaseModels", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.PhaseModels = nil
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects a phaseModels entry with an empty profile ref (no fallback, unlike llmProfileRef fields)", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.PhaseModels = map[string]string{"rca": ""}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring(`phaseModels["rca"]`))
		Expect(errs[0].Error()).To(ContainSubstring("undefined profile"))
	})

	It("rejects phaseModels value referencing an undefined profile", func() {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.PhaseModels = map[string]string{"rca": "does-not-exist"}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "phaseModels") && strings.Contains(e.Error(), `"does-not-exist"`) {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})

	It("accepts phaseModels referencing a profile that shares KA's credentialsSecretName", func() {
		kn := testKubernaut()
		primary := kn.Spec.LLMProfiles["primary"]
		kn.Spec.LLMProfiles["lightweight"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: "openai", Model: "gpt-4o-mini", Endpoint: "http://llm-gateway:8080",
			CredentialsSecretName: primary.CredentialsSecretName,
		}
		kn.Spec.KubernautAgent.PhaseModels = map[string]string{"workflow_discovery": "lightweight"}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("#233: accepts phaseModels referencing a profile with a different credentialsSecretName than KA's (KA #1726/#1728 fixed independent per-phase apiKeyFile resolution)", func() {
		kn := testKubernaut()
		kn.Spec.LLMProfiles["other-creds"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: "openai", Model: "gpt-4o-mini", Endpoint: "http://llm-gateway:8080",
			CredentialsSecretName: "different-secret",
		}
		kn.Spec.KubernautAgent.PhaseModels = map[string]string{"workflow_discovery": "other-creds"}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("#233: accepts a phaseModels entry with a different provider AND a different credentialsSecretName than KA's", func() {
		kn := testKubernaut()
		kn.Spec.LLMProfiles["vertex-phase"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: LLMProviderVertexAI, Model: "gemini-2.5-flash",
			CredentialsSecretName: "vertex-phase-creds",
			VertexProject:         "example-gcp-project", VertexLocation: "us-central1",
		}
		kn.Spec.KubernautAgent.PhaseModels = map[string]string{"rca": "vertex-phase"}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(),
			"#233: cross-provider phase overrides are representable now that KA resolves each phase's own apiKeyFile independently")
	})
})

var _ = Describe("OTLP telemetry TLS contract", func() {
	setGatewayTelemetry := func(kn *kubernautv1alpha2.Kubernaut, telemetry kubernautv1alpha2.TelemetrySpec) {
		kn.Spec.Gateway.Config.Telemetry = telemetry
	}

	setNetworkTelemetry := func(endpoint string) kubernautv1alpha2.TelemetrySpec {
		return kubernautv1alpha2.TelemetrySpec{Endpoint: endpoint}
	}

	It("accepts disabled, log-sink-only, and stdout-only telemetry without TLS material", func() {
		cases := []kubernautv1alpha2.TelemetrySpec{
			{},
			{LogSink: boolPtr(true)},
			{Endpoint: "stdout"},
		}
		for _, telemetry := range cases {
			kn := testKubernaut()
			setGatewayTelemetry(kn, telemetry)
			Expect(ValidateKubernaut(kn, KagentiSidecarNone)).To(BeEmpty(), "telemetry=%#v", telemetry)
		}
	})

	It("accepts an explicit HTTPS endpoint while keeping TLS implicit in the rendered contract", func() {
		kn := testKubernaut()
		setGatewayTelemetry(kn, setNetworkTelemetry("https://otel-collector:4317"))
		Expect(ValidateKubernaut(kn, KagentiSidecarNone)).To(BeEmpty())
	})

	DescribeTable("rejects non-TLS endpoint forms", func(endpoint string) {
		kn := testKubernaut()
		setGatewayTelemetry(kn, setNetworkTelemetry(endpoint))
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).NotTo(BeEmpty())
		Expect(strings.Join(errorStrings(errs), "; ")).To(ContainSubstring("spec.gateway.config.telemetry.endpoint"))
	},
		Entry("http URI", "http://otel-collector:4317"),
		Entry("other URI scheme", "ftp://otel-collector:4317"),
		Entry("missing port", "otel-collector"),
		Entry("invalid port", "otel-collector:not-a-port"),
		Entry("whitespace", "otel-collector:4317 "),
		Entry("loopback address", "127.0.0.1:4317"),
		Entry("IPv6 loopback address", "[::1]:4317"),
		Entry("link-local metadata address", "169.254.169.254:4317"),
		Entry("metadata hostname", "metadata.google.internal:4317"),
	)

	It("rejects ambiguous CA sources and requires an absolute file path", func() {
		kn := testKubernaut()
		telemetry := setNetworkTelemetry("otel-collector:4317")
		telemetry.TLS.CAFile = "relative/ca.pem"
		telemetry.TLS.CACertSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "telemetry-ca"}
		setGatewayTelemetry(kn, telemetry)

		joined := strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("caFile"))
		Expect(joined).To(ContainSubstring("caCertSecretRef"))
	})

	It("requires a named CA Secret and validates the full numeric port range", func() {
		kn := testKubernaut()
		telemetry := setNetworkTelemetry("otel-collector:0")
		telemetry.TLS.CACertSecretRef = &kubernautv1alpha2.CACertSecretRef{}
		setGatewayTelemetry(kn, telemetry)
		joined := strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("caCertSecretRef.name must not be empty"))
		Expect(joined).To(ContainSubstring("port from 1 to 65535"))

		for _, endpoint := range []string{"otel-collector:65536", "otel-collector:+1"} {
			Expect(validateTelemetryEndpoint(endpoint)).To(MatchError(ContainSubstring("TCP port")), endpoint)
		}
		Expect(allASCIIDigits("")).To(BeFalse())
		Expect(allASCIIDigits("12a")).To(BeFalse())
	})

	It("requires a complete client pair and validates Secret-backed client paths", func() {
		kn := testKubernaut()
		telemetry := setNetworkTelemetry("otel-collector:4317")
		telemetry.TLS.CertFile = testTelemetryCertFile
		telemetry.TLS.TLSClientSecretRef = "telemetry-client"
		setGatewayTelemetry(kn, telemetry)

		joined := strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("certFile"))
		Expect(joined).To(ContainSubstring("keyFile"))

		telemetry.TLS = kubernautv1alpha2.TelemetryTLSConfig{TLSClientSecretRef: "telemetry-client"}
		setGatewayTelemetry(kn, telemetry)
		joined = strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("requires both certFile and keyFile"))

		telemetry.TLS.CertFile = "relative/tls.crt"
		telemetry.TLS.KeyFile = "/etc/telemetry/tls.key"
		setGatewayTelemetry(kn, telemetry)
		joined = strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("absolute"))

		telemetry.TLS.CertFile = testTelemetryCertFile
		telemetry.TLS.KeyFile = "/etc/other/tls.key"
		setGatewayTelemetry(kn, telemetry)
		joined = strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("same directory"))

		telemetry.TLS.KeyFile = testTelemetryCertFile
		setGatewayTelemetry(kn, telemetry)
		joined = strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("different paths"))

		telemetry.TLS.KeyFile = "/etc/telemetry/ca.crt"
		telemetry.TLS.CACertSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "telemetry-ca"}
		setGatewayTelemetry(kn, telemetry)
		joined = strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("must not overwrite"))

		telemetry.TLS.CertFile = "/etc/collector/tls.crt"
		telemetry.TLS.KeyFile = "/etc/collector/tls.key"
		setGatewayTelemetry(kn, telemetry)
		joined = strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("mounted below /etc/telemetry"))
	})

	It("rejects Secret keys that could escape the projected Secret item path", func() {
		kn := testKubernaut()
		telemetry := setNetworkTelemetry("otel-collector:4317")
		telemetry.TLS.CACertSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "telemetry-ca", Key: "../ca.crt"}
		setGatewayTelemetry(kn, telemetry)
		joined := strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("without path separators"))
	})

	It("uses the same validation policy for DataStorage and Kubernaut Agent", func() {
		kn := testKubernaut()
		kn.Spec.DataStorage.Telemetry = setNetworkTelemetry("http://otel-collector:4317")
		kn.Spec.KubernautAgent.Telemetry = setNetworkTelemetry("otel-collector")
		joined := strings.Join(errorStrings(ValidateKubernaut(kn, KagentiSidecarNone)), "; ")
		Expect(joined).To(ContainSubstring("spec.dataStorage.telemetry.endpoint"))
		Expect(joined).To(ContainSubstring("spec.kubernautAgent.telemetry.endpoint"))
	})
})

func errorStrings(errs []error) []string {
	result := make([]string, len(errs))
	for i, err := range errs {
		result[i] = err.Error()
	}
	return result
}

var _ = Describe("API Frontend Severity Triage LLM Validation", func() {
	It("accepts a nil severityTriage (defaults to inheriting AF's resolved profile)", func() {
		kn := testKubernautWithAF()
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts an empty severityTriage.llmProfileRef (inherits AF's resolved profile)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects severityTriage.llmProfileRef referencing an undefined profile", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{
			LLMProfileRef: "does-not-exist",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "severityTriage.llmProfileRef") {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})

	It("accepts severityTriage.llmProfileRef sharing AF's resolved profile's credentialsSecretName", func() {
		kn := testKubernautWithAF()
		primary := kn.Spec.LLMProfiles["primary"]
		kn.Spec.LLMProfiles["triage"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: "openai", Model: "gpt-4o-mini", Endpoint: "http://llm-gateway:8080",
			CredentialsSecretName: primary.CredentialsSecretName,
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{
			LLMProfileRef: "triage",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("#234: accepts severityTriage.llmProfileRef with a different credentialsSecretName than AF's resolved profile (non-vertex_ai; AF resolves severityTriage.llm independently)", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["triage-other-creds"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: "openai", Model: "gpt-4o-mini", Endpoint: "http://llm-gateway:8080",
			CredentialsSecretName: "different-secret",
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{
			LLMProfileRef: "triage-other-creds",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("#234: accepts severityTriage.llmProfileRef with a different provider AND a different credentialsSecretName than AF's resolved profile", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["triage-anthropic"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: "anthropic", Model: "claude-sonnet-4-5",
			CredentialsSecretName: "triage-anthropic-creds",
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{
			LLMProfileRef: "triage-anthropic",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(),
			"#234: AF resolves severityTriage.llm independently via resolveLLMKey(), so cross-provider triage overrides are safe")
	})

	It("#279: accepts severityTriage.llmProfileRef with a different credentialsSecretName than AF's resolved profile when both use vertex_ai, now that kubernaut#1731 is fixed and each renders its own apiKeyFile", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["af-vertex"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: LLMProviderVertexAI, Model: "gemini-2.5-pro",
			CredentialsSecretName: "af-vertex-creds",
			VertexProject:         "example-gcp-project", VertexLocation: "us-central1",
		}
		kn.Spec.LLMProfiles["triage-vertex"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: LLMProviderVertexAI, Model: "gemini-2.5-flash",
			CredentialsSecretName: "triage-vertex-creds",
			VertexProject:         "example-gcp-project", VertexLocation: "us-central1",
		}
		kn.Spec.APIFrontend.LLMProfileRef = "af-vertex"
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{
			LLMProfileRef: "triage-vertex",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(),
			"#279: cross-credential vertex_ai-vs-vertex_ai overrides are safe now that each resolves its own apiKeyFile, matching every other provider combination unblocked in #234")
	})

	It("#234: accepts severityTriage.llmProfileRef and AF's resolved profile both vertex_ai when they share the same credentialsSecretName", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["af-vertex"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: LLMProviderVertexAI, Model: "gemini-2.5-pro",
			CredentialsSecretName: "af-vertex-creds",
			VertexProject:         "example-gcp-project", VertexLocation: "us-central1",
		}
		kn.Spec.LLMProfiles["triage-vertex-same-creds"] = kubernautv1alpha2.LLMProfileSpec{
			Provider: LLMProviderVertexAI, Model: "gemini-2.5-flash",
			CredentialsSecretName: "af-vertex-creds",
			VertexProject:         "example-gcp-project", VertexLocation: "us-central1",
		}
		kn.Spec.APIFrontend.LLMProfileRef = "af-vertex"
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{
			LLMProfileRef: "triage-vertex-same-creds",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts llmEnabled=false regardless of profile ref validity concerns", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha2.APIFrontendSeverityTriageSpec{
			LLMEnabled: &disabled,
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})
})

var _ = Describe("Fleet Config Validation", func() {
	validFleet := func() *kubernautv1alpha2.Kubernaut {
		kn := testKubernaut()
		enabled := true
		kn.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled: &enabled,
			MCPGateway: kubernautv1alpha2.FleetMCPGatewaySpec{
				Type: "eaigw", Endpoint: "https://mcp-gateway.example.com/sse", Namespace: "mcp-system",
			},
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{
				Backend: "fleetmetadatacache", Endpoint: "https://fmc.kubernaut.svc:8443",
			},
			OAuth2: kubernautv1alpha2.FleetOAuth2Spec{
				TokenURL: "https://keycloak.example.com/token", CredentialsSecretRef: "fleet-oauth2-creds",
			},
		}
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
		return kn
	}

	It("accepts a nil or disabled Fleet spec", func() {
		kn := testKubernaut()
		Expect(ValidateFleet(testKnV2(kn))).To(BeEmpty())

		disabled := false
		kn.Spec.Fleet = kubernautv1alpha2.FleetSpec{Enabled: &disabled, ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: "invalid"}}
		Expect(ValidateFleet(testKnV2(kn))).To(BeEmpty())
	})

	It("rejects active Fleet without a scope-check backend", func() {
		kn := validFleet()
		kn.Spec.Fleet.ScopeCheck.Backend = ""
		errs := ValidateFleet(kn)
		Expect(errs).NotTo(BeEmpty())
		Expect(errs[0].Error()).To(ContainSubstring("spec.fleet.scopeCheck.backend"))
	})

	It("rejects an unsupported scope-check backend", func() {
		kn := validFleet()
		kn.Spec.Fleet.ScopeCheck.Backend = "valkey"
		errs := ValidateFleet(kn)
		Expect(errs).NotTo(BeEmpty())
		Expect(errs[0].Error()).To(ContainSubstring("spec.fleet.scopeCheck.backend"))
		Expect(errs[0].Error()).To(ContainSubstring("valkey"))
	})

	It("requires an explicit ACM endpoint and token Secret", func() {
		kn := validFleet()
		kn.Spec.Fleet.ScopeCheck.Backend = "acm"
		kn.Spec.Fleet.ScopeCheck.Endpoint = ""
		kn.Spec.Fleet.ScopeCheck.TokenSecretRef = nil
		errs := ValidateFleet(kn)
		Expect(errs).To(HaveLen(2))
		Expect(errs[0].Error()).To(ContainSubstring("spec.fleet.scopeCheck.endpoint"))
		Expect(errs[1].Error()).To(ContainSubstring("spec.fleet.scopeCheck.tokenSecretRef"))
	})

	It("accepts active Fleet with the operator-managed FMC backend", func() {
		Expect(ValidateFleet(validFleet())).To(BeEmpty())
	})

	It("rejects an insecure explicit FMC endpoint", func() {
		kn := validFleet()
		kn.Spec.Fleet.ScopeCheck.Endpoint = "http://fmc.kubernaut.svc:8080"
		errs := ValidateFleet(kn)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("https"))
	})

	It("accepts active Fleet with ACM and a typed token Secret reference", func() {
		kn := validFleet()
		kn.Spec.Fleet.ScopeCheck = kubernautv1alpha2.FleetScopeCheckSpec{
			Backend: "acm", Endpoint: "https://acm-search.example.com/graphql",
			TokenSecretRef: &kubernautv1alpha2.SecretKeyRef{Name: "acm-search-token", Key: "bearer"},
		}
		Expect(ValidateFleet(kn)).To(BeEmpty())
	})

	It("requires the MCP Gateway endpoint, type, and namespace when Fleet is active", func() {
		kn := validFleet()
		kn.Spec.Fleet.MCPGateway = kubernautv1alpha2.FleetMCPGatewaySpec{}
		errs := ValidateFleet(kn)
		Expect(errs).To(HaveLen(3))
		Expect(errs[0].Error()).To(ContainSubstring("spec.fleet.mcpGateway.endpoint"))
		Expect(errs[1].Error()).To(ContainSubstring("spec.fleet.mcpGateway.type"))
		Expect(errs[2].Error()).To(ContainSubstring("spec.fleet.mcpGateway.namespace"))
	})

	It("requires Fleet OAuth2 token URL, shared credentials, and the independent WE credential", func() {
		kn := validFleet()
		kn.Spec.Fleet.ScopeCheck = kubernautv1alpha2.FleetScopeCheckSpec{
			Backend: "acm", Endpoint: "https://acm-search.example.com/graphql",
			TokenSecretRef: &kubernautv1alpha2.SecretKeyRef{Name: "acm-search-token"},
		}
		kn.Spec.Fleet.OAuth2 = kubernautv1alpha2.FleetOAuth2Spec{}
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = ""
		errs := ValidateFleet(kn)
		Expect(errs).To(HaveLen(3))
		Expect(errs[0].Error()).To(ContainSubstring("spec.fleet.oauth2.tokenURL"))
		Expect(errs[1].Error()).To(ContainSubstring("spec.fleet.oauth2.credentialsSecretRef"))
		Expect(errs[2].Error()).To(ContainSubstring("spec.workflowExecution.fleet.oauth2CredentialsSecretRef"))
	})

	It("validates each Fleet trust lane by source", func() {
		kn := validFleet()
		kn.Spec.Fleet.ScopeCheck.TLS = &kubernautv1alpha2.FleetTrustSpec{Source: kubernautv1alpha2.FleetTrustSourceFile, CAFile: "relative-ca.pem"}
		kn.Spec.Fleet.OAuth2.TLS = &kubernautv1alpha2.FleetTrustSpec{Source: kubernautv1alpha2.FleetTrustSourceSecret, CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "oauth2-ca"}}
		errs := ValidateFleet(kn)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("spec.fleet.scopeCheck.tls.caFile"))

		kn.Spec.Fleet.ScopeCheck.TLS.CAFile = "/etc/fleet/ca.pem"
		Expect(ValidateFleet(kn)).To(BeEmpty())
	})

	It("rejects source-incompatible trust payloads", func() {
		kn := validFleet()
		kn.Spec.Fleet.OAuth2.TLS = &kubernautv1alpha2.FleetTrustSpec{
			Source: kubernautv1alpha2.FleetTrustSourceSystem, CAFile: "/etc/fleet/ca.pem",
		}
		errs := ValidateFleet(kn)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("spec.fleet.oauth2.tls"))
	})
})

var _ = Describe("FleetMetadataCache Validation", func() {
	It("leaves FMC validation inert when Fleet is disabled", func() {
		Expect(ValidateFleet(testKnV2(testKubernaut()))).To(BeEmpty())
	})

	It("requires the nested MCP Gateway endpoint when FMC is active", func() {
		kn := testKubernaut()
		enabled := true
		kn.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:    &enabled,
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: "fleetmetadatacache"},
			MCPGateway: kubernautv1alpha2.FleetMCPGatewaySpec{Type: "eaigw", Namespace: "mcp-system"},
			OAuth2:     kubernautv1alpha2.FleetOAuth2Spec{TokenURL: "https://keycloak.example.com/token", CredentialsSecretRef: "fmc-oauth2-creds"},
		}
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
		errs := ValidateFleet(kn)
		Expect(errs).NotTo(BeEmpty())
		Expect(errs[0].Error()).To(ContainSubstring("spec.fleet.mcpGateway.endpoint"))
	})

	It("requires the FMC-specific effective credentials override when shared credentials are absent", func() {
		kn := testKubernaut()
		enabled := true
		kn.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:    &enabled,
			MCPGateway: kubernautv1alpha2.FleetMCPGatewaySpec{Type: "eaigw", Endpoint: "https://mcp-gateway.example.com/sse", Namespace: "mcp-system"},
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: "fleetmetadatacache"},
			OAuth2:     kubernautv1alpha2.FleetOAuth2Spec{TokenURL: "https://keycloak.example.com/token"},
		}
		kn.Spec.Gateway.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testGatewayFleetOAuth2SecretRef}
		kn.Spec.RemediationOrchestrator.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testROFleetOAuth2SecretRef}
		kn.Spec.SignalProcessing.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testSPFleetOAuth2SecretRef}
		kn.Spec.APIFrontend.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testAFFleetOAuth2SecretRef}
		kn.Spec.EffectivenessMonitor.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testEMFleetOAuth2SecretRef}
		kn.Spec.KubernautAgent.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testKAFleetOAuth2SecretRef}
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
		errs := ValidateFleet(kn)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("spec.fleetMetadataCache.fleet.oauth2CredentialsSecretRef"))
	})

	It("accepts an FMC-specific credentials override without a shared credential", func() {
		kn := testKubernaut()
		enabled := true
		kn.Spec.Fleet = kubernautv1alpha2.FleetSpec{
			Enabled:    &enabled,
			MCPGateway: kubernautv1alpha2.FleetMCPGatewaySpec{Type: "eaigw", Endpoint: "https://mcp-gateway.example.com/sse", Namespace: "mcp-system"},
			ScopeCheck: kubernautv1alpha2.FleetScopeCheckSpec{Backend: "fleetmetadatacache"},
			OAuth2:     kubernautv1alpha2.FleetOAuth2Spec{TokenURL: "https://keycloak.example.com/token"},
		}
		kn.Spec.Gateway.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testGatewayFleetOAuth2SecretRef}
		kn.Spec.RemediationOrchestrator.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testROFleetOAuth2SecretRef}
		kn.Spec.SignalProcessing.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testSPFleetOAuth2SecretRef}
		kn.Spec.APIFrontend.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testAFFleetOAuth2SecretRef}
		kn.Spec.EffectivenessMonitor.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testEMFleetOAuth2SecretRef}
		kn.Spec.KubernautAgent.Fleet = &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: testKAFleetOAuth2SecretRef}
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
		kn.Spec.FleetMetadataCache = kubernautv1alpha2.FleetMetadataCacheSpec{Fleet: &kubernautv1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: "fmc-own-oauth2-creds"}}
		Expect(ValidateFleet(kn)).To(BeEmpty())
	})
})

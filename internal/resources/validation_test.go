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

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

const malformedURL = "not-a-url"

var _ = Describe("KA JWKS URL Validation", func() {
	withInteractive := func(providers []kubernautv1alpha1.JWTProviderSpec, allowInsecure bool) *kubernautv1alpha1.Kubernaut {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.Interactive = &kubernautv1alpha1.InteractiveSpec{
			JWTProviders:      providers,
			AllowInsecureJWKS: allowInsecure,
		}
		return kn
	}

	validProvider := func(name string) kubernautv1alpha1.JWTProviderSpec {
		return kubernautv1alpha1.JWTProviderSpec{
			Name:      name,
			IssuerURL: "https://login.kubernaut.ai/realms/kubernaut",
			JWKSURL:   "https://login.kubernaut.ai/realms/kubernaut/protocol/openid-connect/certs",
			Audiences: []string{"kubernaut-apifrontend"},
		}
	}

	It("accepts HTTPS JWKS URL", func() {
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{
			validProvider("rhbk"),
		}, false)
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects HTTP JWKS URL without override", func() {
		p := validProvider("dev")
		p.JWKSURL = "http://mock-jwks:8080/.well-known/jwks.json"
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{p}, false)
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("scheme must be https"))
		Expect(errs[0].Error()).To(ContainSubstring("allowInsecureJWKS"))
	})

	It("accepts HTTP JWKS URL with allowInsecureJWKS=true", func() {
		p := validProvider("dev")
		p.JWKSURL = "http://mock-jwks:8080/.well-known/jwks.json"
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{p}, true)
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects JWKS URL longer than 2048 characters", func() {
		p := validProvider("long")
		p.JWKSURL = "https://example.com/" + strings.Repeat("a", 2040)
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{p}, false)
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("must be <= 2048 characters"))
	})

	It("rejects malformed JWKS URL", func() {
		p := validProvider("bad")
		p.JWKSURL = malformedURL
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{p}, false)
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("must be an absolute URL"))
	})

	It("reports multiple errors for multiple bad providers", func() {
		p1 := validProvider("http1")
		p1.JWKSURL = "http://foo.com/jwks"
		p2 := validProvider("http2")
		p2.JWKSURL = "http://bar.com/jwks"
		kn := withInteractive([]kubernautv1alpha1.JWTProviderSpec{p1, p2}, false)
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(2))
	})

	It("returns no errors when interactive is nil", func() {
		kn := testKubernaut()
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("returns no errors when providers list is empty", func() {
		kn := withInteractive(nil, false)
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})
})

var _ = Describe("IA-2: AF multi-provider JWT authentication", func() {
	withAFProviders := func(providers []kubernautv1alpha1.JWTProviderSpec) *kubernautv1alpha1.Kubernaut {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.JWTProviders = providers
		return kn
	}

	keycloakProvider := kubernautv1alpha1.JWTProviderSpec{
		Name:      "keycloak",
		IssuerURL: "https://keycloak.example.com/realms/kubernaut",
		JWKSURL:   "https://keycloak.example.com/realms/kubernaut/protocol/openid-connect/certs",
		Audiences: []string{"kubernaut-console"},
	}

	spireProvider := kubernautv1alpha1.JWTProviderSpec{
		Name:      "spire",
		IssuerURL: "https://spire.example.com",
		Audiences: []string{"kubernaut-workload"},
	}

	It("IA-2: accepts AF with multiple concurrent OIDC providers for multi-source authentication", func() {
		kn := withAFProviders([]kubernautv1alpha1.JWTProviderSpec{keycloakProvider, spireProvider})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(),
			"IA-2: platform must support concurrent JWT validation from multiple OIDC issuers")
	})

	It("IA-2: satisfies authentication requirement when jwtProviders replaces top-level issuerURL", func() {
		kn := withAFProviders([]kubernautv1alpha1.JWTProviderSpec{keycloakProvider})
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
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
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
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
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
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
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
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
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
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
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
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
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
		kn.Spec.APIFrontend.Auth.JWTProviders = []kubernautv1alpha1.JWTProviderSpec{
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
		kn.Spec.APIFrontend.RBACRolesConfigMapRef = &kubernautv1alpha1.ConfigMapRef{ConfigMapName: ""}
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
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
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
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-team"}},
				{Role: "cicd", Groups: []string{"ci-bots"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts empty roleBindings list", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects unknown persona name in roleBindings", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "unknown-persona", Groups: []string{"team-x"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("unknown"))
	})

	It("rejects invalid sarCacheTTL format", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			SARCacheTTL: "not-a-duration",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("sarCacheTTL"))
	})

	// --- Issue #181: Custom ClusterRole references ---

	It("[AC-3] rejects roleBinding with both role and clusterRoleName set", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", ClusterRoleName: "my-custom-role", Groups: []string{"team-a"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("mutually exclusive"))
	})

	It("[AC-3] rejects roleBinding with neither role nor clusterRoleName set", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Groups: []string{"team-a"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("one of role or clusterRoleName"))
	})

	It("[AC-3] accepts roleBinding with only clusterRoleName set", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{ClusterRoleName: "my-custom-role", Groups: []string{"team-a"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("[AC-6] accepts mixed persona and custom clusterRoleName bindings", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-team"}},
				{ClusterRoleName: "my-custom-role", Groups: []string{"custom-team"}},
			},
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})
})

var _ = Describe("AlignmentCheck Validation", func() {
	withAlignmentCheck := func(ac kubernautv1alpha1.AlignmentCheckSpec) *kubernautv1alpha1.Kubernaut {
		kn := testKubernaut()
		kn.Spec.KubernautAgent.AlignmentCheck = ac
		return kn
	}

	It("skips validation when alignmentCheck is disabled", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled: false,
			Timeout: "not-a-duration",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts valid alignmentCheck configuration", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled:       true,
			Timeout:       "10s",
			MaxStepTokens: 500,
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects invalid timeout duration", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled: true,
			Timeout: "not-a-duration",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("timeout"))
		Expect(errs[0].Error()).To(ContainSubstring("invalid Go duration"))
	})

	It("rejects timeout below 1s", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled: true,
			Timeout: "500ms",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("timeout"))
		Expect(errs[0].Error()).To(ContainSubstring("between"))
	})

	It("rejects timeout above 60s", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled: true,
			Timeout: "120s",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("timeout"))
		Expect(errs[0].Error()).To(ContainSubstring("between"))
	})

	It("accepts timeout at lower bound (1s)", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled: true,
			Timeout: "1s",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts timeout at upper bound (60s)", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled: true,
			Timeout: "60s",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects negative maxStepTokens", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled:       true,
			MaxStepTokens: -1,
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("maxStepTokens"))
		Expect(errs[0].Error()).To(ContainSubstring("positive"))
	})

	It("rejects empty provider when llm is set", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled: true,
			LLM: &kubernautv1alpha1.AlignmentCheckLLMSpec{
				Model: "gpt-4o",
			},
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("llm.provider"))
	})

	It("rejects empty model when llm is set", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled: true,
			LLM: &kubernautv1alpha1.AlignmentCheckLLMSpec{
				Provider: "openai",
			},
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("llm.model"))
	})

	It("accepts valid llm configuration", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled: true,
			LLM: &kubernautv1alpha1.AlignmentCheckLLMSpec{
				Provider: "openai",
				Model:    "gpt-4o",
			},
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accumulates multiple errors", func() {
		kn := withAlignmentCheck(kubernautv1alpha1.AlignmentCheckSpec{
			Enabled:       true,
			Timeout:       "not-a-duration",
			MaxStepTokens: -1,
			LLM:           &kubernautv1alpha1.AlignmentCheckLLMSpec{},
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(4))
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

	withInteractiveMode := func(spec kubernautv1alpha1.InteractiveSpec) *kubernautv1alpha1.Kubernaut {
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
		kn := withInteractiveMode(kubernautv1alpha1.InteractiveSpec{
			Enabled:    boolPtr(false),
			SessionTTL: "not-a-duration",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts valid interactive configuration", func() {
		kn := withInteractiveMode(kubernautv1alpha1.InteractiveSpec{
			Enabled:           boolPtr(true),
			SessionTTL:        "30m",
			InactivityTimeout: "10m",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects invalid sessionTTL", func() {
		kn := withInteractiveMode(kubernautv1alpha1.InteractiveSpec{
			Enabled:    boolPtr(true),
			SessionTTL: "not-a-duration",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("sessionTTL"))
	})

	It("rejects invalid inactivityTimeout", func() {
		kn := withInteractiveMode(kubernautv1alpha1.InteractiveSpec{
			Enabled:           boolPtr(true),
			InactivityTimeout: "not-a-duration",
		})
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Error()).To(ContainSubstring("inactivityTimeout"))
	})

	It("accumulates multiple duration errors", func() {
		kn := withInteractiveMode(kubernautv1alpha1.InteractiveSpec{
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
		kn.Spec.LLMProfiles["primary"] = kubernautv1alpha1.LLMProfileSpec{}
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
		profile.Provider = "openai"
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
		profile.Provider = "openai"
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
})

var _ = Describe("LLM Profile Referential Integrity", func() {
	It("rejects a missing kubernautAgent.llmProfileRef", func() {
		kn := testKubernaut()
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
		kn.Spec.LLMProfiles["af-profile"] = kubernautv1alpha1.LLMProfileSpec{
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
		kn.Spec.LLMProfiles["lightweight"] = kubernautv1alpha1.LLMProfileSpec{
			Provider: "openai", Model: "gpt-4o-mini", Endpoint: "http://llm-gateway:8080",
			CredentialsSecretName: primary.CredentialsSecretName,
		}
		kn.Spec.KubernautAgent.PhaseModels = map[string]string{"workflow_discovery": "lightweight"}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects phaseModels referencing a profile with a different credentialsSecretName than KA's", func() {
		kn := testKubernaut()
		kn.Spec.LLMProfiles["other-creds"] = kubernautv1alpha1.LLMProfileSpec{
			Provider: "openai", Model: "gpt-4o-mini", Endpoint: "http://llm-gateway:8080",
			CredentialsSecretName: "different-secret",
		}
		kn.Spec.KubernautAgent.PhaseModels = map[string]string{"workflow_discovery": "other-creds"}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "phaseModels") && strings.Contains(e.Error(), "credentialsSecretName") {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})
})

var _ = Describe("API Frontend Severity Triage LLM Validation", func() {
	It("accepts a nil severityTriage (defaults to inheriting AF's resolved profile)", func() {
		kn := testKubernautWithAF()
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("accepts an empty severityTriage.llmProfileRef (inherits AF's resolved profile)", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects severityTriage.llmProfileRef referencing an undefined profile", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{
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
		kn.Spec.LLMProfiles["triage"] = kubernautv1alpha1.LLMProfileSpec{
			Provider: "openai", Model: "gpt-4o-mini", Endpoint: "http://llm-gateway:8080",
			CredentialsSecretName: primary.CredentialsSecretName,
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{
			LLMProfileRef: "triage",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})

	It("rejects severityTriage.llmProfileRef with a different credentialsSecretName than AF's resolved profile", func() {
		kn := testKubernautWithAF()
		kn.Spec.LLMProfiles["triage-other-creds"] = kubernautv1alpha1.LLMProfileSpec{
			Provider: "openai", Model: "gpt-4o-mini", Endpoint: "http://llm-gateway:8080",
			CredentialsSecretName: "different-secret",
		}
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{
			LLMProfileRef: "triage-other-creds",
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "severityTriage.llmProfileRef") && strings.Contains(e.Error(), "credentialsSecretName") {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})

	It("accepts llmEnabled=false regardless of profile ref validity concerns", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.APIFrontend.SeverityTriage = &kubernautv1alpha1.APIFrontendSeverityTriageSpec{
			LLMEnabled: &disabled,
		}
		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty())
	})
})

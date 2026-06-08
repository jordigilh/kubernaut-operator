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
	"strings"
	"testing"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func kagentiAuthbridgeConfigMap(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "authbridge-config",
			Namespace: "kagenti-system",
		},
		Data: data,
	}
}

const testKagentiIssuerURL = "https://keycloak.example.com/realms/kagenti"

func testKubernautCRWithAuth(issuerURL string) *kubernautv1alpha1.Kubernaut {
	kn := testKubernautCR(true, true)
	kn.Spec.APIFrontend.Auth.IssuerURL = issuerURL
	return kn
}

// IA-2: when kagenti is not active, the operator must not attempt OIDC
// auto-detection — the CR is the only source of auth configuration.
func TestResolveKagentiOIDCDefaults_NilWhenNoSidecar(t *testing.T) {
	r := newFakeReconciler()
	kn := testKubernautCRWithAuth("")

	defaults, err := r.resolveKagentiOIDCDefaults(context.Background(), kn, resources.KagentiSidecarNone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defaults != nil {
		t.Error("expected nil defaults when sidecar is not active")
	}
}

// IA-2: the operator derives the OIDC issuer from the kagenti authbridge-config
// ConfigMap so the AF validates tokens against the correct identity provider.
func TestResolveKagentiOIDCDefaults_DetectsFromAuthbridgeConfig(t *testing.T) {
	cm := kagentiAuthbridgeConfigMap(map[string]string{
		"ISSUER":         testKagentiIssuerURL,
		"KEYCLOAK_URL":   "http://keycloak-service.keycloak.svc:8080",
		"KEYCLOAK_REALM": "kagenti",
	})
	r := newFakeReconciler(cm)
	kn := testKubernautCRWithAuth("")

	defaults, err := r.resolveKagentiOIDCDefaults(context.Background(), kn, resources.KagentiSidecarAuthbridge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defaults == nil {
		t.Fatal("expected non-nil defaults")
	}
	if defaults.IssuerURL != testKagentiIssuerURL {
		t.Errorf("issuerURL = %q, want kagenti realm URL", defaults.IssuerURL)
	}
	if defaults.JWKSURL != "http://keycloak-service.keycloak.svc:8080/realms/kagenti/protocol/openid-connect/certs" {
		t.Errorf("jwksURL = %q, want in-cluster JWKS endpoint", defaults.JWKSURL)
	}
	if !defaults.AllowInsecureIssuers {
		t.Error("allowInsecureIssuers should be true when KEYCLOAK_URL uses http://")
	}
}

// SC-8: when the in-cluster Keycloak uses HTTPS, the operator must not
// enable insecure issuers — transmission confidentiality is preserved.
func TestResolveKagentiOIDCDefaults_SecureWhenKeycloakIsHTTPS(t *testing.T) {
	cm := kagentiAuthbridgeConfigMap(map[string]string{
		"ISSUER":         testKagentiIssuerURL,
		"KEYCLOAK_URL":   "https://keycloak-service.keycloak.svc:8443",
		"KEYCLOAK_REALM": "kagenti",
	})
	r := newFakeReconciler(cm)
	kn := testKubernautCRWithAuth("")

	defaults, err := r.resolveKagentiOIDCDefaults(context.Background(), kn, resources.KagentiSidecarAuthbridge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defaults.AllowInsecureIssuers {
		t.Error("allowInsecureIssuers must be false when KEYCLOAK_URL is https (SC-8)")
	}
	if defaults.JWKSURL != "https://keycloak-service.keycloak.svc:8443/realms/kagenti/protocol/openid-connect/certs" {
		t.Errorf("jwksURL = %q, want HTTPS JWKS endpoint", defaults.JWKSURL)
	}
}

// IA-2: auto-detection works for both kagenti 0.2.x (envoy) and 0.3.x+ (authbridge)
// since both versions maintain the same authbridge-config ConfigMap schema.
func TestResolveKagentiOIDCDefaults_WorksWithEnvoySidecar(t *testing.T) {
	cm := kagentiAuthbridgeConfigMap(map[string]string{
		"ISSUER":         testKagentiIssuerURL,
		"KEYCLOAK_URL":   "http://keycloak-service.keycloak.svc:8080",
		"KEYCLOAK_REALM": "kagenti",
	})
	r := newFakeReconciler(cm)
	kn := testKubernautCRWithAuth("")

	defaults, err := r.resolveKagentiOIDCDefaults(context.Background(), kn, resources.KagentiSidecarEnvoy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defaults == nil {
		t.Fatal("expected non-nil defaults for envoy sidecar mode")
	}
	if defaults.IssuerURL != testKagentiIssuerURL {
		t.Errorf("issuerURL = %q, want kagenti realm URL", defaults.IssuerURL)
	}
}

// CM-6: when the CR explicitly provides issuerURL, the auto-detected value
// must not override it — operator-defined configuration takes precedence.
func TestResolveKagentiOIDCDefaults_SkipsWhenCRHasIssuerAndConfigMapMissing(t *testing.T) {
	r := newFakeReconciler()
	kn := testKubernautCRWithAuth("https://custom-idp.example.com/realms/custom")

	defaults, err := r.resolveKagentiOIDCDefaults(context.Background(), kn, resources.KagentiSidecarAuthbridge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defaults != nil {
		t.Error("expected nil defaults when CR has issuerURL and ConfigMap is absent")
	}
}

// IA-2: when kagenti sidecar is active but the authbridge-config ConfigMap is
// missing and the CR has no issuerURL, the operator must fail with a clear
// error rather than silently proceeding without authentication.
func TestResolveKagentiOIDCDefaults_ErrorsWhenConfigMapMissingAndNoOverride(t *testing.T) {
	r := newFakeReconciler()
	kn := testKubernautCRWithAuth("")

	_, err := r.resolveKagentiOIDCDefaults(context.Background(), kn, resources.KagentiSidecarAuthbridge)
	if err == nil {
		t.Fatal("expected error when ConfigMap missing and no CR override")
	}
	if got := err.Error(); !strings.Contains(got, "authbridge-config") || !strings.Contains(got, "not found") {
		t.Errorf("error should reference authbridge-config not found, got: %s", got)
	}
}

// IA-2: when the ISSUER key is missing from the ConfigMap but the CR has
// an explicit issuerURL, the operator should proceed without error.
func TestResolveKagentiOIDCDefaults_SkipsWhenIssuerKeyMissingButCRHasOverride(t *testing.T) {
	cm := kagentiAuthbridgeConfigMap(map[string]string{
		"KEYCLOAK_URL":   "http://keycloak-service.keycloak.svc:8080",
		"KEYCLOAK_REALM": "kagenti",
	})
	r := newFakeReconciler(cm)
	kn := testKubernautCRWithAuth("https://custom-idp.example.com/realms/custom")

	defaults, err := r.resolveKagentiOIDCDefaults(context.Background(), kn, resources.KagentiSidecarAuthbridge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defaults != nil {
		t.Error("expected nil defaults when ISSUER key missing but CR has override")
	}
}

// IA-2: when the ISSUER key is missing and the CR has no override, the
// operator must fail rather than proceeding without authentication.
func TestResolveKagentiOIDCDefaults_ErrorsWhenIssuerKeyMissingAndNoOverride(t *testing.T) {
	cm := kagentiAuthbridgeConfigMap(map[string]string{
		"KEYCLOAK_URL":   "http://keycloak-service.keycloak.svc:8080",
		"KEYCLOAK_REALM": "kagenti",
	})
	r := newFakeReconciler(cm)
	kn := testKubernautCRWithAuth("")

	_, err := r.resolveKagentiOIDCDefaults(context.Background(), kn, resources.KagentiSidecarAuthbridge)
	if err == nil {
		t.Fatal("expected error when ISSUER key is missing and no CR override")
	}
	if got := err.Error(); !strings.Contains(got, "ISSUER") {
		t.Errorf("error should reference missing ISSUER key, got: %s", got)
	}
}

// CM-6: when KEYCLOAK_URL or KEYCLOAK_REALM are absent, the operator still
// detects issuerURL but omits jwksURL and keeps allowInsecureIssuers=false.
func TestResolveKagentiOIDCDefaults_IssuerOnlyWhenKeycloakURLMissing(t *testing.T) {
	cm := kagentiAuthbridgeConfigMap(map[string]string{
		"ISSUER": "https://keycloak.example.com/realms/kagenti",
	})
	r := newFakeReconciler(cm)
	kn := testKubernautCRWithAuth("")

	defaults, err := r.resolveKagentiOIDCDefaults(context.Background(), kn, resources.KagentiSidecarAuthbridge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defaults.IssuerURL != testKagentiIssuerURL {
		t.Errorf("issuerURL = %q, want kagenti realm URL", defaults.IssuerURL)
	}
	if defaults.JWKSURL != "" {
		t.Errorf("jwksURL should be empty when KEYCLOAK_URL is missing, got %q", defaults.JWKSURL)
	}
	if defaults.AllowInsecureIssuers {
		t.Error("allowInsecureIssuers should be false when KEYCLOAK_URL is missing")
	}
}

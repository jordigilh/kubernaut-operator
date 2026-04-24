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
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

func TestMain(m *testing.M) {
	cleanup := setTestRelatedImages()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func testKubernaut() *kubernautv1alpha1.Kubernaut {
	return &kubernautv1alpha1.Kubernaut{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernautv1alpha1.SingletonName,
			Namespace: "kubernaut-system",
		},
		Spec: kubernautv1alpha1.KubernautSpec{
			Image: kubernautv1alpha1.ImageSpec{
				PullPolicy: corev1.PullIfNotPresent,
			},
			PostgreSQL: kubernautv1alpha1.PostgreSQLSpec{
				SecretName: "postgresql-secret",
				Host:       "pg.example.com",
				Port:       5432,
			},
			Valkey: kubernautv1alpha1.ValkeySpec{
				SecretName: "valkey-secret",
				Host:       "valkey.example.com",
				Port:       6379,
			},
			KubernautAgent: kubernautv1alpha1.KubernautAgentSpec{
				LLM: kubernautv1alpha1.LLMSpec{
					Provider:              "openai",
					Model:                 "gpt-4o",
					CredentialsSecretName: "llm-creds",
				},
			},
			AIAnalysis: kubernautv1alpha1.AIAnalysisSpec{
				Policy: kubernautv1alpha1.PolicyConfigMapRef{ConfigMapName: "aianalysis-policies"},
			},
			SignalProcessing: kubernautv1alpha1.SignalProcessingSpec{
				Policy: kubernautv1alpha1.PolicyConfigMapRef{ConfigMapName: "signalprocessing-policy"},
			},
		},
	}
}

// setTestRelatedImages sets RELATED_IMAGE env vars for all components
// so that ResolveImage works in tests. Call cleanup() when done.
func setTestRelatedImages() (cleanup func()) {
	envs := map[string]string{
		"RELATED_IMAGE_GATEWAY":                 "quay.io/kubernaut-ai/gateway:v1.3.0",
		"RELATED_IMAGE_DATA_STORAGE":            "quay.io/kubernaut-ai/datastorage:v1.3.0",
		"RELATED_IMAGE_AIANALYSIS":              "quay.io/kubernaut-ai/aianalysis:v1.3.0",
		"RELATED_IMAGE_SIGNALPROCESSING":        "quay.io/kubernaut-ai/signalprocessing:v1.3.0",
		"RELATED_IMAGE_REMEDIATIONORCHESTRATOR": "quay.io/kubernaut-ai/remediationorchestrator:v1.3.0",
		"RELATED_IMAGE_WORKFLOWEXECUTION":       "quay.io/kubernaut-ai/workflowexecution:v1.3.0",
		"RELATED_IMAGE_EFFECTIVENESSMONITOR":    "quay.io/kubernaut-ai/effectivenessmonitor:v1.3.0",
		"RELATED_IMAGE_NOTIFICATION":            "quay.io/kubernaut-ai/notification:v1.3.0",
		"RELATED_IMAGE_KUBERNAUT_AGENT":         "quay.io/kubernaut-ai/kubernautagent:v1.3.0",
		"RELATED_IMAGE_AUTHWEBHOOK":             "quay.io/kubernaut-ai/authwebhook:v1.3.0",
		"RELATED_IMAGE_DB_MIGRATE":              "quay.io/kubernaut-ai/db-migrate:v1.3.0",
	}
	for k, v := range envs {
		os.Setenv(k, v)
	}
	return func() {
		for k := range envs {
			os.Unsetenv(k)
		}
	}
}

func TestResolveImage_FromEnvVar(t *testing.T) {
	kn := testKubernaut()
	got, err := ResolveImage(kn, "gateway")
	if err != nil {
		t.Fatalf("ResolveImage() unexpected error: %v", err)
	}
	want := "quay.io/kubernaut-ai/gateway:v1.3.0"
	if got != want {
		t.Errorf("ResolveImage() = %q, want %q", got, want)
	}
}

func TestResolveImage_OverrideTakesPrecedence(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.Image.Overrides = map[string]string{
		"gateway": "myregistry.internal/custom-gateway:v2.0.0",
	}
	got, err := ResolveImage(kn, "gateway")
	if err != nil {
		t.Fatalf("ResolveImage() unexpected error: %v", err)
	}
	want := "myregistry.internal/custom-gateway:v2.0.0"
	if got != want {
		t.Errorf("ResolveImage() = %q, want %q", got, want)
	}
}

func TestResolveImage_NoEnvVar_ReturnsError(t *testing.T) {
	prev := os.Getenv("RELATED_IMAGE_GATEWAY")
	os.Unsetenv("RELATED_IMAGE_GATEWAY")
	defer os.Setenv("RELATED_IMAGE_GATEWAY", prev)

	kn := testKubernaut()
	_, err := ResolveImage(kn, "gateway")
	if err == nil {
		t.Error("ResolveImage() should return error when no env var and no override")
	}
}

func TestResolveImage_AllComponents(t *testing.T) {
	kn := testKubernaut()
	components := []string{
		"gateway", "datastorage", "aianalysis", "signalprocessing",
		"remediationorchestrator", "workflowexecution", "effectivenessmonitor",
		"notification", "kubernautagent", "authwebhook",
	}
	for _, c := range components {
		_, err := ResolveImage(kn, c)
		if err != nil {
			t.Errorf("ResolveImage(%q) unexpected error: %v", c, err)
		}
	}
}

func TestResolveImage_MigrationUsesDbMigrateImage(t *testing.T) {
	kn := testKubernaut()
	got, err := ResolveImage(kn, "db-migrate")
	if err != nil {
		t.Fatalf("ResolveImage(db-migrate) unexpected error: %v", err)
	}
	want := "quay.io/kubernaut-ai/db-migrate:v1.3.0"
	if got != want {
		t.Errorf("ResolveImage(db-migrate) = %q, want %q", got, want)
	}
}

func TestCommonLabels(t *testing.T) {
	kn := testKubernaut()
	labels := CommonLabels(kn)

	checks := map[string]string{
		"app.kubernetes.io/managed-by": "kubernaut-operator",
		"app.kubernetes.io/part-of":    "kubernaut",
		"app.kubernetes.io/instance":   "kubernaut",
	}
	for k, want := range checks {
		if got := labels[k]; got != want {
			t.Errorf("CommonLabels[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestComponentLabels_IncludesAppLabel(t *testing.T) {
	kn := testKubernaut()
	labels := ComponentLabels(kn, ComponentGateway)

	if got := labels["app"]; got != "gateway" {
		t.Errorf("ComponentLabels[app] = %q, want %q", got, "gateway")
	}
	if got := labels["app.kubernetes.io/component"]; got != "gateway" {
		t.Errorf("ComponentLabels[component] = %q, want %q", got, "gateway")
	}
	if _, ok := labels["app.kubernetes.io/managed-by"]; !ok {
		t.Error("ComponentLabels should include common labels")
	}
}

func TestSelectorLabels(t *testing.T) {
	labels := SelectorLabels(ComponentGateway)
	if len(labels) != 1 {
		t.Fatalf("SelectorLabels should have exactly 1 key, got %d", len(labels))
	}
	if got := labels["app"]; got != "gateway" {
		t.Errorf("SelectorLabels[app] = %q, want %q", got, "gateway")
	}
}

func TestObjectMeta_NamespaceAndLabels(t *testing.T) {
	kn := testKubernaut()
	om := ObjectMeta(kn, "gateway-config", ComponentGateway)

	if om.Name != "gateway-config" {
		t.Errorf("ObjectMeta.Name = %q, want %q", om.Name, "gateway-config")
	}
	if om.Namespace != "kubernaut-system" {
		t.Errorf("ObjectMeta.Namespace = %q, want %q", om.Namespace, "kubernaut-system")
	}
	if om.Labels["app"] != "gateway" {
		t.Errorf("ObjectMeta.Labels[app] = %q, want %q", om.Labels["app"], "gateway")
	}
}

func TestDataStorageURL(t *testing.T) {
	got := DataStorageURL("kubernaut-system")
	want := "https://data-storage-service.kubernaut-system.svc.cluster.local:8080"
	if got != want {
		t.Errorf("DataStorageURL() = %q, want %q", got, want)
	}
}

func TestGatewayURL(t *testing.T) {
	got := GatewayURL("kubernaut-system")
	want := "https://gateway-service.kubernaut-system.svc.cluster.local:8080"
	if got != want {
		t.Errorf("GatewayURL() = %q, want %q", got, want)
	}
}

func TestInterServiceTLSCAFile_MatchesOCPServiceCA(t *testing.T) {
	if InterServiceTLSCAFile != "/etc/tls-ca/service-ca.crt" {
		t.Errorf("InterServiceTLSCAFile = %q, want /etc/tls-ca/service-ca.crt (OCP service-ca key)", InterServiceTLSCAFile)
	}
}

func TestValkeyAddr(t *testing.T) {
	tests := []struct {
		name string
		spec kubernautv1alpha1.ValkeySpec
		want string
	}{
		{"explicit port", kubernautv1alpha1.ValkeySpec{Host: "valkey.local", Port: 6380}, "valkey.local:6380"},
		{"default port", kubernautv1alpha1.ValkeySpec{Host: "valkey.local"}, "valkey.local:6379"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValkeyAddr(&tt.spec); got != tt.want {
				t.Errorf("ValkeyAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateHostname_Valid(t *testing.T) {
	for _, host := range []string{"pg.local", "192.168.1.1", "my-host.example.com", "[::1]"} {
		if err := ValidateHostname(host); err != nil {
			t.Errorf("ValidateHostname(%q) should be valid, got: %v", host, err)
		}
	}
}

func TestValidateHostname_Invalid(t *testing.T) {
	for _, host := range []string{"", "host;rm -rf /", "host user=admin", "a b"} {
		if err := ValidateHostname(host); err == nil {
			t.Errorf("ValidateHostname(%q) should be invalid", host)
		}
	}
}

func TestPodSecurityContext_RestrictedProfile(t *testing.T) {
	psc := PodSecurityContext()
	if psc.RunAsNonRoot == nil || !*psc.RunAsNonRoot {
		t.Error("PodSecurityContext must set RunAsNonRoot=true")
	}
	if psc.SeccompProfile == nil || psc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Error("PodSecurityContext must set SeccompProfile=RuntimeDefault")
	}
}

func TestContainerSecurityContext_RestrictedProfile(t *testing.T) {
	csc := ContainerSecurityContext()
	if csc.AllowPrivilegeEscalation == nil || *csc.AllowPrivilegeEscalation {
		t.Error("ContainerSecurityContext must set AllowPrivilegeEscalation=false")
	}
	if csc.ReadOnlyRootFilesystem == nil || !*csc.ReadOnlyRootFilesystem {
		t.Error("ContainerSecurityContext must set ReadOnlyRootFilesystem=true")
	}
	if csc.Capabilities == nil || len(csc.Capabilities.Drop) == 0 || csc.Capabilities.Drop[0] != "ALL" {
		t.Error("ContainerSecurityContext must drop ALL capabilities")
	}
}

func TestMergeResources_UsesDefaultsWhenEmpty(t *testing.T) {
	res := MergeResources(corev1.ResourceRequirements{})
	if res.Requests.Cpu().IsZero() {
		t.Error("MergeResources should return default CPU request when user spec is empty")
	}
}

func TestMergeResources_UsesUserSpecWhenProvided(t *testing.T) {
	cpu := resource.MustParse("100m")
	userSpec := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: cpu,
		},
	}
	res := MergeResources(userSpec)
	if res.Requests.Cpu().String() != "100m" {
		t.Errorf("MergeResources should use user spec, got CPU=%s", res.Requests.Cpu().String())
	}
}

func TestAllComponents_Count(t *testing.T) {
	components := AllComponents()
	if len(components) != 10 {
		t.Errorf("AllComponents() should return 10 components, got %d", len(components))
	}
}

func TestSetOwnerReference_SetsControllerRef(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kubernautv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme core: %v", err)
	}

	kn := testKubernaut()
	kn.UID = types.UID("test-uid-1234")

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: kn.Namespace},
	}
	if err := SetOwnerReference(kn, cm, scheme); err != nil {
		t.Fatalf("SetOwnerReference: %v", err)
	}

	refs := cm.GetOwnerReferences()
	if len(refs) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(refs))
	}
	ref := refs[0]
	if ref.Kind != "Kubernaut" {
		t.Errorf("OwnerRef.Kind = %q, want Kubernaut", ref.Kind)
	}
	if ref.UID != kn.UID {
		t.Errorf("OwnerRef.UID = %q, want %q", ref.UID, kn.UID)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Error("OwnerRef.Controller should be true")
	}
}

func TestServiceAccountName_UnknownComponent_ReturnsSelf(t *testing.T) {
	got := ServiceAccountName("custom-thing")
	if got != "custom-thing" {
		t.Errorf("ServiceAccountName(unknown) = %q, want %q", got, "custom-thing")
	}
}

func TestIntPtrDefault_NonNil_ReturnsValue(t *testing.T) {
	val := 0
	got := intPtrDefault(&val, 42)
	if got != 0 {
		t.Errorf("intPtrDefault(ptr(0), 42) = %d, want 0", got)
	}
}

func TestIntPtrDefault_Nil_ReturnsDefault(t *testing.T) {
	got := intPtrDefault(nil, 42)
	if got != 42 {
		t.Errorf("intPtrDefault(nil, 42) = %d, want 42", got)
	}
}

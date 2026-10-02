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

package kind

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	postgresImage    = "docker.io/library/postgres:16.4"
	valkeyImage      = "docker.io/valkey/valkey:8.0.2"
	postgresUser     = "kubernaut"
	postgresPassword = "kind-postgres-password" //nolint:gosec // disposable Kind fixture credential
	postgresDB       = "kubernaut"
	valkeyPassword   = "kind-valkey-password"
)

func ensureKubernautInfrastructure(ctx context.Context) error {
	if err := ensureNamespace(ctx, kubernautNamespace); err != nil {
		return err
	}
	objects := []interface{}{
		postgresSecret(),
		valkeySecret(),
		llmSecret(),
		aiAnalysisPolicyConfigMap(),
		signalProcessingPolicyConfigMap(),
		postgresDeployment(),
		postgresService(),
		valkeyDeployment(),
		valkeyService(),
	}
	if err := applyYAML(ctx, objects...); err != nil {
		return fmt.Errorf("applying Kubernaut infrastructure: %w", err)
	}
	for _, deployment := range []string{"postgresql", "valkey"} {
		if _, err := kubectl(
			ctx, "wait", "--for=condition=Available", "deployment/"+deployment,
			"-n", kubernautNamespace, "--timeout=10m",
		); err != nil {
			return fmt.Errorf("waiting for %s prerequisite: %w", deployment, err)
		}
	}
	return nil
}

func ensureNamespace(ctx context.Context, namespace string) error {
	output, err := kubectl(ctx, "create", "namespace", namespace)
	if err != nil && !strings.Contains(output, "AlreadyExists") {
		return fmt.Errorf("creating namespace %s: %w", namespace, err)
	}
	return nil
}

func postgresSecret() *corev1.Secret {
	return &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret", Namespace: kubernautNamespace},
		StringData: map[string]string{
			"POSTGRES_USER":     postgresUser,
			"POSTGRES_PASSWORD": postgresPassword,
			"POSTGRES_DB":       postgresDB,
		},
	}
}

func valkeySecret() *corev1.Secret {
	return &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: "valkey-secret", Namespace: kubernautNamespace},
		StringData: map[string]string{
			"valkey-secrets.yaml": fmt.Sprintf("password: %q\n", valkeyPassword),
		},
	}
}

func llmSecret() *corev1.Secret {
	return &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: "llm-credentials", Namespace: kubernautNamespace},
		StringData: map[string]string{"api-key": "kind-e2e-not-a-real-credential"},
	}
}

func aiAnalysisPolicyConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: "aianalysis-policy", Namespace: kubernautNamespace},
		Data:       map[string]string{"approval.rego": "package approval\ndefault allow = true\n"},
	}
}

func signalProcessingPolicyConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: "signalprocessing-policy", Namespace: kubernautNamespace},
		Data:       map[string]string{"policy.rego": "package signalprocessing\ndefault allow = true\n"},
	}
}

func postgresDeployment() *appsv1.Deployment {
	labels := prerequisiteLabels("postgresql")
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql", Namespace: kubernautNamespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "postgresql", Image: postgresImage,
					Command: []string{"bash", "-ceu"},
					Args: []string{`mkdir -p /tmp/postgres-tls
          openssl req -x509 -nodes -newkey rsa:2048 \
            -keyout /tmp/postgres-tls/server.key \
            -out /tmp/postgres-tls/server.crt \
            -days 2 -subj /CN=postgresql
          chown postgres:postgres /tmp/postgres-tls/server.key /tmp/postgres-tls/server.crt
          chmod 600 /tmp/postgres-tls/server.key
exec docker-entrypoint.sh postgres \
  -c ssl=on \
  -c ssl_cert_file=/tmp/postgres-tls/server.crt \
  -c ssl_key_file=/tmp/postgres-tls/server.key`},
					Env: []corev1.EnvVar{
						{Name: "POSTGRES_USER", Value: postgresUser},
						{Name: "POSTGRES_PASSWORD", Value: postgresPassword},
						{Name: "POSTGRES_DB", Value: postgresDB},
					},
					Ports: []corev1.ContainerPort{{Name: "postgres", ContainerPort: 5432}},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{
							Command: []string{"pg_isready", "-U", postgresUser, "-d", postgresDB},
						}},
						InitialDelaySeconds: 5,
						PeriodSeconds:       2,
					},
				}}},
			},
		},
	}
}

func postgresService() *corev1.Service {
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: "postgresql", Namespace: kubernautNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "postgresql"},
			Ports:    []corev1.ServicePort{{Port: 5432, TargetPort: intstr.FromInt(5432)}},
		},
	}
}

func valkeyDeployment() *appsv1.Deployment {
	labels := prerequisiteLabels("valkey")
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: "valkey", Namespace: kubernautNamespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "valkey", Image: valkeyImage,
					Args:  []string{"--requirepass", valkeyPassword, "--save", ""},
					Ports: []corev1.ContainerPort{{Name: "valkey", ContainerPort: 6379}},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{
							Command: []string{"valkey-cli", "-a", valkeyPassword, "ping"},
						}},
						InitialDelaySeconds: 3,
						PeriodSeconds:       2,
					},
				}}},
			},
		},
	}
}

func valkeyService() *corev1.Service {
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: "valkey", Namespace: kubernautNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "valkey"},
			Ports:    []corev1.ServicePort{{Port: 6379, TargetPort: intstr.FromInt(6379)}},
		},
	}
}

// prerequisiteLabels places the disposable BYO dependencies in the same
// provider-policy trust domain as the Kubernaut workloads. Native Cilium and
// Calico policies identify allowed in-namespace endpoints by the instance and
// managed-by labels; no owner reference is set, so these remain user-owned
// prerequisites and are never removed by the operator's cleanup path.
func prerequisiteLabels(app string) map[string]string {
	labels := map[string]string{"app": app}
	if configuredProvider == providerCilium || configuredProvider == providerCalico {
		labels["app.kubernetes.io/managed-by"] = "kubernaut-operator"
		labels["app.kubernetes.io/instance"] = kubernautv1alpha2.SingletonName
	}
	return labels
}

func applyKubernautCR(ctx context.Context) error {
	if err := applyYAML(ctx, kubernautCR()); err != nil {
		return fmt.Errorf("creating Kubernaut CR: %w", err)
	}
	return nil
}

func kubernautCR() *kubernautv1alpha2.Kubernaut {
	provider := kubernautv1alpha2.NetworkPolicyProviderAuto
	switch configuredProvider {
	case providerCilium:
		provider = kubernautv1alpha2.NetworkPolicyProviderCilium
	case providerCalico:
		provider = kubernautv1alpha2.NetworkPolicyProviderCalico
	}
	return &kubernautv1alpha2.Kubernaut{
		TypeMeta:   metav1.TypeMeta{APIVersion: kubernautv1alpha2.GroupVersion.String(), Kind: "Kubernaut"},
		ObjectMeta: metav1.ObjectMeta{Name: kubernautv1alpha2.SingletonName, Namespace: kubernautNamespace},
		Spec: kubernautv1alpha2.KubernautSpec{
			Image: kubernautv1alpha2.ImageSpec{
				PullPolicy: corev1.PullIfNotPresent,
				Overrides:  kubernautImageOverrides(),
			},
			PostgreSQL: kubernautv1alpha2.PostgreSQLSpec{
				SecretName: "postgresql-secret",
				Host:       "postgresql.kubernaut-system.svc.cluster.local",
				SSLMode:    "require",
			},
			Valkey: kubernautv1alpha2.ValkeySpec{
				SecretName: "valkey-secret",
				Host:       "valkey.kubernaut-system.svc.cluster.local",
			},
			AIAnalysis: kubernautv1alpha2.AIAnalysisSpec{
				Policy: kubernautv1alpha2.PolicyConfigMapRef{ConfigMapName: "aianalysis-policy"},
			},
			SignalProcessing: kubernautv1alpha2.SignalProcessingSpec{
				Policy: kubernautv1alpha2.PolicyConfigMapRef{ConfigMapName: "signalprocessing-policy"},
			},
			LLMProfiles: map[string]kubernautv1alpha2.LLMProfileSpec{
				"primary": {
					Provider:              "openai",
					Model:                 "gpt-4o",
					Endpoint:              "http://llm.invalid",
					CredentialsSecretName: "llm-credentials",
				},
			},
			KubernautAgent: kubernautv1alpha2.KubernautAgentSpec{LLMProfileRef: "primary"},
			Gateway:        kubernautv1alpha2.GatewaySpec{Enabled: ptr.To(false)},
			APIFrontend:    kubernautv1alpha2.APIFrontendSpec{Enabled: ptr.To(false)},
			TLS: kubernautv1alpha2.TLSConfigSpec{
				Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
				DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
			},
			NetworkPolicies: kubernautv1alpha2.NetworkPoliciesSpec{Provider: provider},
		},
	}
}

func kubernautImageOverrides() map[string]string {
	tag := strings.TrimSpace(os.Getenv("KUBERNAUT_IMAGE_TAG"))
	if tag == "" {
		tag = "1.6.0-rc20"
	}
	repository := strings.TrimRight(strings.TrimSpace(os.Getenv("KUBERNAUT_IMAGE_REPOSITORY")), "/")
	if repository == "" {
		repository = "quay.io/kubernaut-ai"
	}
	images := []string{
		"gateway", "datastorage", "aianalysis", "signalprocessing", "remediationorchestrator",
		"workflowexecution", "effectivenessmonitor", "notification", "kubernautagent", "authwebhook",
		"apifrontend", "db-migrate", "console", "fleetmetadatacache",
	}
	overrides := make(map[string]string, len(images))
	for _, image := range images {
		overrides[image] = fmt.Sprintf("%s/%s:%s", repository, image, tag)
	}
	return overrides
}

func kubernautCondition(ctx context.Context, condition string) (string, error) {
	output, err := kubectl(ctx, "get", "kubernaut", kubernautv1alpha2.SingletonName, "-n", kubernautNamespace,
		"-o", fmt.Sprintf(`jsonpath={.status.conditions[?(@.type=="%s")].status}`, condition))
	return strings.TrimSpace(output), err
}

func kubernautPhase(ctx context.Context) (string, error) {
	output, err := kubectl(ctx, "get", "kubernaut", kubernautv1alpha2.SingletonName, "-n", kubernautNamespace,
		"-o", "jsonpath={.status.phase}")
	return strings.TrimSpace(output), err
}

func deleteKubernautCR(ctx context.Context) error {
	if _, err := kubectl(
		ctx, "delete", "kubernaut", kubernautv1alpha2.SingletonName,
		"-n", kubernautNamespace, "--ignore-not-found=true", "--wait=false",
	); err != nil {
		return fmt.Errorf("deleting Kubernaut CR: %w", err)
	}
	return pollUntilSuccess(ctx, 5*time.Minute, 2*time.Second, func() error {
		output, err := kubectl(ctx, "get", "kubernaut", kubernautv1alpha2.SingletonName, "-n", kubernautNamespace)
		if err != nil && (strings.Contains(output, "NotFound") || strings.Contains(output, "not found")) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("checking Kubernaut CR deletion: %w", err)
		}
		return fmt.Errorf("kubernaut CR still exists")
	})
}

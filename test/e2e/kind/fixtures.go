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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	"github.com/jordigilh/kubernaut-operator/internal/policy"
)

const (
	probeClientRole = "client"
	probeTargetRole = "target"
	blockedRole     = "blocked"
	probeImage      = "docker.io/curlimages/curl:8.11.1"
	serverImage     = "docker.io/hashicorp/http-echo:1.0.0"
	userPolicyName  = "user-owned-policy"
)

func ensureProbeWorkloads(ctx context.Context) error {
	output, err := kubectl(ctx, "create", "namespace", probeNamespace)
	if err != nil && !strings.Contains(output, "AlreadyExists") {
		return fmt.Errorf("creating probe namespace: %w", err)
	}

	clientLabels := probeLabels(probeClientRole)
	targetLabels := probeLabels(probeTargetRole)
	blockedLabels := map[string]string{
		"app":       "blocked",
		"e2e-role":  blockedRole,
		"component": "unmanaged",
	}

	objects := []interface{}{
		probeDeployment(
			probeNamespace, "probe-client", clientLabels, probeImage,
			[]string{"sh", "-c"}, []string{"while true; do sleep 3600; done"},
		),
		probeDeployment(
			probeNamespace, "probe-target", targetLabels, serverImage,
			nil, []string{"-listen=:8080", "-text=ok"},
		),
		probeDeployment(
			probeNamespace, "probe-blocked", blockedLabels, serverImage,
			nil, []string{"-listen=:8080", "-text=blocked"},
		),
		probeService(probeNamespace, "probe-target", targetLabels),
		probeService(probeNamespace, "probe-blocked", blockedLabels),
	}
	if err := applyYAML(ctx, objects...); err != nil {
		return err
	}
	for _, name := range []string{"probe-client", "probe-target", "probe-blocked"} {
		if _, err := kubectl(
			ctx, "wait", "--for=condition=Available", "deployment/"+name,
			"-n", probeNamespace, "--timeout=5m",
		); err != nil {
			return fmt.Errorf("waiting for probe deployment %s: %w", name, err)
		}
	}
	return nil
}

func probeLabels(role string) map[string]string {
	return map[string]string{
		"app":                          "probe",
		"e2e-role":                     role,
		"app.kubernetes.io/managed-by": "kubernaut-operator",
		"app.kubernetes.io/instance":   probeNamespace,
		"app.kubernetes.io/component":  "probe",
	}
}

func probeDeployment(
	namespace, name string, labels map[string]string, image string, command, args []string,
) *appsv1.Deployment {
	replicas := int32(1)
	selectorLabels := map[string]string{"e2e-role": labels["e2e-role"]}
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "probe",
						Image:   image,
						Command: command,
						Args:    args,
						Ports:   []corev1.ContainerPort{{ContainerPort: 8080}},
					}},
				},
			},
		},
	}
}

func probeService(namespace, name string, labels map[string]string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"e2e-role": labels["e2e-role"]},
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       8080,
				TargetPort: intstr.FromInt32(8080),
			}},
		},
	}
}

func applyYAML(ctx context.Context, objects ...interface{}) error {
	doc := make([]string, 0, len(objects))
	for _, object := range objects {
		encoded, err := yaml.Marshal(object)
		if err != nil {
			return fmt.Errorf("marshalling fixture: %w", err)
		}
		doc = append(doc, string(encoded))
	}
	if _, err := kubectlStdin(ctx, strings.Join(doc, "\n---\n"), "apply", "-f", "-"); err != nil {
		return fmt.Errorf("applying fixture: %w", err)
	}
	return nil
}

func applyNativePolicies(ctx context.Context, objects []policy.RenderedPolicy) error {
	for _, rendered := range objects {
		encoded, err := yaml.Marshal(rendered.Object.Object)
		if err != nil {
			return fmt.Errorf("marshalling %s/%s: %w", rendered.Object.GetKind(), rendered.Object.GetName(), err)
		}
		if _, err := kubectlStdin(ctx, string(encoded), "apply", "-f", "-"); err != nil {
			return fmt.Errorf("applying %s/%s: %w", rendered.Object.GetKind(), rendered.Object.GetName(), err)
		}
	}
	return nil
}

func probeHTTP(ctx context.Context, serviceName string) (bool, error) {
	podOutput, err := kubectl(
		ctx, "get", "pods", "-n", probeNamespace, "-l", "e2e-role="+probeClientRole,
		"-o", "jsonpath={.items[0].metadata.name}",
	)
	if err != nil {
		return false, fmt.Errorf("finding probe client pod: %w", err)
	}
	podName := strings.TrimSpace(podOutput)
	if podName == "" {
		return false, fmt.Errorf("probe client pod is not ready")
	}
	probeArgs := []string{
		"exec", "-n", probeNamespace, podName, "--", "curl", "--fail", "--silent", "--show-error",
		"--connect-timeout", "2", "--max-time", "4", "http://" + serviceName + ":8080",
	}
	_, err = kubectl(ctx, probeArgs...)
	if err != nil {
		return false, nil //nolint:nilerr // curl failure is the expected denied-probe result
	}
	return true, nil
}

func nativePolicyResource(provider policyProvider) string {
	switch provider {
	case providerCilium:
		return "ciliumnetworkpolicies.cilium.io"
	case providerCalico:
		return "networkpolicies.projectcalico.org"
	default:
		return ""
	}
}

func deleteManagedNativePolicies(ctx context.Context) error {
	resource := nativePolicyResource(configuredProvider)
	if resource == "" {
		return nil
	}
	if _, err := kubectl(
		ctx, "delete", resource, "-n", probeNamespace, "-l", managedPolicyLabel, "--ignore-not-found=true",
	); err != nil {
		return fmt.Errorf("deleting managed native policies: %w", err)
	}
	return nil
}

func noManagedPolicies(ctx context.Context) error {
	resource := nativePolicyResource(configuredProvider)
	if resource == "" {
		resource = "networkpolicies.networking.k8s.io"
	}
	output, err := kubectl(
		ctx, "get", resource, "-n", probeNamespace, "-l", managedPolicyLabel,
		"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}",
	)
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) != "" && !strings.Contains(output, "No resources found") {
		return fmt.Errorf("managed policy objects remain: %s", strings.TrimSpace(output))
	}
	return nil
}

func managedNativePolicyExists(ctx context.Context) error {
	resource := nativePolicyResource(configuredProvider)
	if resource == "" {
		return fmt.Errorf("native policy resource is not configured")
	}
	output, err := kubectl(
		ctx, "get", resource, "-n", probeNamespace, "-l", managedPolicyLabel,
		"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}",
	)
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) == "" {
		return fmt.Errorf("managed native policy has not been submitted")
	}
	return nil
}

func applyUnmanagedNativePolicy(ctx context.Context) error {
	resource := nativePolicyResource(configuredProvider)
	if resource == "" {
		return fmt.Errorf("native policy resource is not configured")
	}

	var object map[string]interface{}
	switch configuredProvider {
	case providerCilium:
		object = map[string]interface{}{
			"apiVersion": "cilium.io/v2",
			"kind":       "CiliumNetworkPolicy",
			"metadata": map[string]interface{}{
				"name":      userPolicyName,
				"namespace": probeNamespace,
				"labels": map[string]string{
					policy.ManagedPolicyLabel: "false",
					policy.ManagedByLabel:     "user",
				},
			},
			"spec": map[string]interface{}{
				"endpointSelector": map[string]interface{}{
					"matchLabels": map[string]string{"k8s:app": "user-owned"},
				},
			},
		}
	case providerCalico:
		object = map[string]interface{}{
			"apiVersion": "projectcalico.org/v3",
			"kind":       "NetworkPolicy",
			"metadata": map[string]interface{}{
				"name":      userPolicyName,
				"namespace": probeNamespace,
				"labels": map[string]string{
					policy.ManagedPolicyLabel: "false",
					policy.ManagedByLabel:     "user",
				},
			},
			"spec": map[string]interface{}{
				"selector": "app == 'user-owned'",
				"types":    []string{"Ingress"},
				"ingress":  []interface{}{map[string]interface{}{"action": "Allow"}},
			},
		}
	default:
		return fmt.Errorf("native policy resource is not configured")
	}

	encoded, err := yaml.Marshal(object)
	if err != nil {
		return fmt.Errorf("marshalling user-owned policy: %w", err)
	}
	if _, err := kubectlStdin(ctx, string(encoded), "apply", "-f", "-"); err != nil {
		return fmt.Errorf("applying user-owned policy: %w", err)
	}
	return nil
}

func unmanagedNativePolicyExists(ctx context.Context) error {
	resource := nativePolicyResource(configuredProvider)
	if resource == "" {
		return fmt.Errorf("native policy resource is not configured")
	}
	if _, err := kubectl(ctx, "get", resource, userPolicyName, "-n", probeNamespace); err != nil {
		return fmt.Errorf("user-owned policy is missing: %w", err)
	}
	return nil
}

func deleteProbeNamespace(ctx context.Context) error {
	if _, err := kubectl(
		ctx, "delete", "namespace", probeNamespace, "--ignore-not-found=true", "--wait=false",
	); err != nil {
		return err
	}
	return pollUntilSuccess(ctx, 2*time.Minute, 2*time.Second, func() error {
		output, err := kubectl(ctx, "get", "namespace", probeNamespace)
		if err != nil && (strings.Contains(output, "NotFound") || strings.Contains(output, "not found")) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("checking probe namespace deletion: %w", err)
		}
		if strings.TrimSpace(output) != "" {
			return fmt.Errorf("probe namespace still exists")
		}
		return fmt.Errorf("probe namespace still exists")
	})
}

func ensureNoRawNetworkPolicy(ctx context.Context) error {
	output, err := kubectl(
		ctx, "get", "networkpolicies.networking.k8s.io", "-n", probeNamespace,
		"-l", managedPolicyLabel,
		"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}",
	)
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) != "" && !strings.Contains(output, "No resources found") {
		return fmt.Errorf("raw Kubernetes NetworkPolicy fallback exists: %s", strings.TrimSpace(output))
	}
	return nil
}

func intentForProbe() (policy.Intent, error) {
	return policy.BuildIntent(probeNamespace, []string{"probe"}, nil)
}

func policyProviderForTest() policy.Provider {
	switch configuredProvider {
	case providerCilium:
		return policy.ProviderCilium
	case providerCalico:
		return policy.ProviderCalico
	default:
		return policy.ProviderAuto
	}
}

func collectProbeDiagnostics(ctx context.Context) {
	_, _ = kubectl(ctx, "get", "all", "-n", probeNamespace, "-o", "wide") //nolint:errcheck
	_, _ = kubectl(ctx, "describe", "pods", "-n", probeNamespace)         //nolint:errcheck
}

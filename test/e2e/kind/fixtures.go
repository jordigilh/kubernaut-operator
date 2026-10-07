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
)

const (
	probeClientRole          = "client"
	probeUnmanagedClientRole = "unmanaged-client"
	probeTargetRole          = "target"
	probeImage               = "docker.io/curlimages/curl:8.11.1"
	serverImage              = "docker.io/hashicorp/http-echo:1.0.0"
	userPolicyName           = "user-owned-policy"
	policyProbeComponent     = "aianalysis"
)

func ensureProbeWorkloads(ctx context.Context) error {
	output, err := kubectl(ctx, "create", "namespace", probeNamespace)
	if err != nil && !strings.Contains(output, "AlreadyExists") {
		return fmt.Errorf("creating probe namespace: %w", err)
	}

	clientLabels := managedProbeLabels(probeClientRole)
	unmanagedClientLabels := map[string]string{
		"app":      "probe",
		"e2e-role": probeUnmanagedClientRole,
	}
	targetLabels := managedProbeLabels(probeTargetRole)

	objects := []interface{}{
		probeDeployment(
			probeNamespace, "probe-client", clientLabels, probeImage,
			[]string{"sh", "-c"}, []string{"while true; do sleep 3600; done"},
		),
		probeDeployment(
			probeNamespace, "probe-unmanaged-client", unmanagedClientLabels, probeImage,
			[]string{"sh", "-c"}, []string{"while true; do sleep 3600; done"},
		),
		probeDeployment(
			probeNamespace, "probe-target", targetLabels, serverImage,
			nil, []string{"-listen=:8080", "-text=ok"},
		),
		probeService(probeNamespace, "probe-target", targetLabels),
	}
	if err := applyYAML(ctx, objects...); err != nil {
		return err
	}
	for _, name := range []string{"probe-client", "probe-unmanaged-client", "probe-target"} {
		if _, err := kubectl(
			ctx, "wait", "--for=condition=Available", "deployment/"+name,
			"-n", probeNamespace, "--timeout=5m",
		); err != nil {
			return fmt.Errorf("waiting for probe deployment %s: %w", name, err)
		}
	}
	return nil
}

// ensureMonitoringWorkloads creates disposable Prometheus- and
// Alertmanager-shaped HTTP Services for the qualified native-provider lane.
// The responses use the exact API paths called by the Agent's
// get_metric_names/get_alerts clients, while the third Service provides a
// distinct backend that must remain denied by the native allowlist.
func ensureMonitoringWorkloads(ctx context.Context) error {
	if err := ensureNamespace(ctx, monitoringNamespace); err != nil {
		return err
	}

	prometheusLabels := monitoringBackendLabels("prometheus")
	alertManagerLabels := monitoringBackendLabels("alertmanager")
	blockedLabels := monitoringBackendLabels("blocked")
	objects := []interface{}{
		monitoringDeployment(
			monitoringPrometheusServiceName, prometheusLabels,
			`{"status":"success","data":["kubernaut_e2e_metric"]}`,
		),
		monitoringService(monitoringPrometheusServiceName, prometheusLabels, monitoringPrometheusServicePort),
		monitoringDeployment(monitoringAlertManagerServiceName, alertManagerLabels, `[]`),
		monitoringService(monitoringAlertManagerServiceName, alertManagerLabels, monitoringAlertManagerServicePort),
		monitoringDeployment(
			monitoringBlockedServiceName, blockedLabels,
			`{"status":"success","data":["should_not_be_reachable"]}`,
		),
		monitoringService(monitoringBlockedServiceName, blockedLabels, monitoringBlockedServicePort),
	}
	if err := applyYAML(ctx, objects...); err != nil {
		return fmt.Errorf("applying monitoring fixtures: %w", err)
	}
	for _, name := range []string{
		monitoringPrometheusServiceName,
		monitoringAlertManagerServiceName,
		monitoringBlockedServiceName,
	} {
		if _, err := kubectl(
			ctx, "wait", "--for=condition=Available", "deployment/"+name,
			"-n", monitoringNamespace, "--timeout=5m",
		); err != nil {
			return fmt.Errorf("waiting for monitoring deployment %s: %w", name, err)
		}
	}
	return nil
}

func deleteMonitoringWorkloads(ctx context.Context) error {
	for _, resource := range []string{
		"deployment/" + monitoringPrometheusServiceName,
		"service/" + monitoringPrometheusServiceName,
		"deployment/" + monitoringAlertManagerServiceName,
		"service/" + monitoringAlertManagerServiceName,
		"deployment/" + monitoringBlockedServiceName,
		"service/" + monitoringBlockedServiceName,
	} {
		if _, err := kubectl(
			ctx, "delete", resource, "-n", monitoringNamespace,
			"--ignore-not-found=true", "--wait=false",
		); err != nil {
			return fmt.Errorf("deleting monitoring fixture %s: %w", resource, err)
		}
	}
	if _, err := kubectl(
		ctx, "delete", "namespace", monitoringNamespace,
		"--ignore-not-found=true", "--wait=false",
	); err != nil {
		return fmt.Errorf("deleting monitoring fixture namespace: %w", err)
	}
	return nil
}

func monitoringBackendLabels(role string) map[string]string {
	return map[string]string{
		"app":             "kubernaut-monitoring-e2e",
		"monitoring-role": role,
	}
}

func monitoringDeployment(name string, labels map[string]string, response string) *appsv1.Deployment {
	replicas := int32(1)
	selectorLabels := map[string]string{"monitoring-role": labels["monitoring-role"]}
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: monitoringNamespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  "monitoring",
					Image: serverImage,
					Args:  []string{"-listen=:8080", "-text=" + response},
					Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}},
				}}},
			},
		},
	}
}

func monitoringService(name string, labels map[string]string, port int32) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: monitoringNamespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"monitoring-role": labels["monitoring-role"]},
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Protocol:   corev1.ProtocolTCP,
				Port:       port,
				TargetPort: intstr.FromInt32(8080),
			}},
		},
	}
}

func probeLabels(role string) map[string]string {
	return map[string]string{
		"app":      "probe",
		"e2e-role": role,
	}
}

func managedProbeLabels(role string) map[string]string {
	labels := probeLabels(role)
	labels["app"] = policyProbeComponent
	labels["app.kubernetes.io/managed-by"] = "kubernaut-operator"
	labels["app.kubernetes.io/instance"] = "kubernaut"
	labels["app.kubernetes.io/component"] = policyProbeComponent
	return labels
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

func probeHTTP(ctx context.Context, serviceName string) (bool, error) {
	return probeHTTPFromRole(ctx, serviceName, probeClientRole)
}

func probeHTTPFromRole(ctx context.Context, serviceName, role string) (bool, error) {
	podOutput, err := kubectl(
		ctx, "get", "pods", "-n", probeNamespace, "-l", "e2e-role="+role,
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

func monitoringPrometheusURL() string {
	return fmt.Sprintf(
		"http://%s.%s.svc:%d", monitoringPrometheusServiceName,
		monitoringNamespace, monitoringPrometheusServicePort,
	)
}

func monitoringAlertManagerURL() string {
	return fmt.Sprintf(
		"http://%s.%s.svc:%d", monitoringAlertManagerServiceName,
		monitoringNamespace, monitoringAlertManagerServicePort,
	)
}

func agentMonitoringConfigContains(ctx context.Context, value string) error {
	output, err := kubectl(
		ctx, "get", "configmap", "kubernaut-agent-config", "-n", kubernautNamespace,
		"-o", "jsonpath={.data.config\\.yaml}",
	)
	if err != nil {
		return fmt.Errorf("reading kubernaut-agent monitoring config: %w", err)
	}
	if !strings.Contains(output, value) {
		return fmt.Errorf("kubernaut-agent config does not contain %q", value)
	}
	return nil
}

func agentMonitoringGET(ctx context.Context, serviceName string, port int32, path string) (string, error) {
	podOutput, err := kubectl(
		ctx, "get", "pods", "-n", kubernautNamespace, "-l", "app=kubernaut-agent",
		"-o", "jsonpath={.items[0].metadata.name}",
	)
	if err != nil {
		return "", fmt.Errorf("finding kubernaut-agent pod: %w", err)
	}
	podName := strings.TrimSpace(podOutput)
	if podName == "" {
		return "", fmt.Errorf("kubernaut-agent pod is not ready")
	}
	url := fmt.Sprintf("http://%s.%s.svc:%d%s", serviceName, monitoringNamespace, port, path)
	output, err := kubectl(
		ctx, "exec", "-n", kubernautNamespace, podName, "--",
		"wget", "-q", "-O", "-", "-T", "4", url,
	)
	if err != nil {
		return "", fmt.Errorf("kubernaut-agent GET %s: %w", url, err)
	}
	return strings.TrimSpace(output), nil
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
					"kubernaut.ai/managed-policy":  "false",
					"app.kubernetes.io/managed-by": "user",
				},
			},
			"spec": map[string]interface{}{
				"endpointSelector": map[string]interface{}{
					"matchLabels": map[string]string{"k8s:app": "user-owned"},
				},
				"ingress": []interface{}{map[string]interface{}{
					"fromEndpoints": []interface{}{map[string]interface{}{
						"matchLabels": map[string]string{"k8s:app": "user-owned"},
					}},
				}},
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
					"kubernaut.ai/managed-policy":  "false",
					"app.kubernetes.io/managed-by": "user",
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

// deleteProbeWorkloads removes only the fixtures created by this suite.
// probeNamespace intentionally aliases kubernautNamespace so provider-native
// selectors see the same namespace as the reconciled Kubernaut workloads; the
// namespace also contains operator-managed resources and must never be deleted
// as probe cleanup.
func deleteProbeWorkloads(ctx context.Context) error {
	for _, resource := range []string{
		"deployment/probe-client",
		"deployment/probe-unmanaged-client",
		"deployment/probe-target",
		"service/probe-target",
	} {
		if _, err := kubectl(
			ctx, "delete", resource, "-n", probeNamespace,
			"--ignore-not-found=true", "--wait=false",
		); err != nil {
			return fmt.Errorf("deleting probe %s: %w", resource, err)
		}
	}
	if resource := nativePolicyResource(configuredProvider); resource != "" {
		if _, err := kubectl(
			ctx, "delete", resource, userPolicyName, "-n", probeNamespace,
			"--ignore-not-found=true", "--wait=false",
		); err != nil {
			return fmt.Errorf("deleting user-owned provider policy: %w", err)
		}
	}

	return pollUntilSuccess(ctx, 2*time.Minute, 2*time.Second, func() error {
		for _, resource := range []string{
			"deployment/probe-client",
			"deployment/probe-unmanaged-client",
			"deployment/probe-target",
			"service/probe-target",
		} {
			output, err := kubectl(ctx, "get", resource, "-n", probeNamespace)
			if err != nil && (strings.Contains(output, "NotFound") || strings.Contains(output, "not found")) {
				continue
			}
			if err != nil {
				return fmt.Errorf("checking probe %s deletion: %w", resource, err)
			}
			return fmt.Errorf("probe %s still exists", resource)
		}
		return nil
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

func collectProbeDiagnostics(ctx context.Context) {
	_, _ = kubectl(ctx, "get", "all", "-n", probeNamespace, "-o", "wide") //nolint:errcheck
	_, _ = kubectl(ctx, "describe", "pods", "-n", probeNamespace)         //nolint:errcheck
}

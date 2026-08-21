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

package networkpolicy

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

const (
	// systemNamespace mirrors kn.Namespace: the namespace the operator
	// deploys its own component NetworkPolicies and Deployments into.
	systemNamespace = "kubernaut-system"
	// monitoringNamespace mirrors OCPMonitoringNamespace, the tier-1
	// default destination for Prometheus/AlertManager egress.
	monitoringNamespace = resources.OCPMonitoringNamespace
	// altMonitoringNamespace stands in for a platform-operator-supplied,
	// non-default monitoring stack (NPE2E-005 tier 2).
	altMonitoringNamespace = "custom-monitoring"

	stubImage    = "docker.io/hashicorp/http-echo:1.0.0"
	clientImage  = "docker.io/curlimages/curl:8.11.1"
	probeTimeout = 5 * time.Second
)

// buildKubernautCR returns a minimal, valid Kubernaut CR (plus its v1alpha2
// projection) with NetworkPolicies enabled, matching just enough spec for
// resources.NetworkPolicies() to render every always-on component's policy
// without panicking on an unset dependency. Mirrors the shape of the
// unexported testKubernaut() helper in internal/resources's own test suite
// (not reusable across packages), trimmed to only what NetworkPolicies()
// reads.
func buildKubernautCR() (*kubernautv1alpha1.Kubernaut, *kubernautv1alpha2.Kubernaut) {
	npEnabled := true
	kn := &kubernautv1alpha1.Kubernaut{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernautv1alpha1.SingletonName,
			Namespace: systemNamespace,
		},
		Spec: kubernautv1alpha1.KubernautSpec{
			NetworkPolicies: kubernautv1alpha1.NetworkPoliciesSpec{Enabled: &npEnabled},
		},
	}
	knV2 := &kubernautv1alpha2.Kubernaut{}
	if err := kn.ConvertTo(knV2); err != nil {
		panic(fmt.Sprintf("buildKubernautCR: ConvertTo: %v", err))
	}
	return kn, knV2
}

// renderNetworkPolicy renders every component NetworkPolicy for kn/knV2 and
// returns the one named component+"-netpol", or nil if not present (e.g.
// component disabled).
func renderNetworkPolicy(
	kn *kubernautv1alpha1.Kubernaut,
	knV2 *kubernautv1alpha2.Kubernaut,
	component string,
) *networkingv1.NetworkPolicy {
	for _, np := range resources.NetworkPolicies(kn, knV2, resources.KagentiSidecarNone) {
		if np.Name == component+"-netpol" {
			return np
		}
	}
	return nil
}

// applyYAML pipes a YAML document to kubectl apply -f - against this
// suite's kind cluster.
func applyYAML(ctx context.Context, doc string) error {
	full := append([]string{"--context", kubeContext()}, "apply", "-f", "-")
	out, err := runCmdStdin(ctx, doc, "kubectl", full...)
	if err != nil {
		return fmt.Errorf("kubectl apply: %w: %s", err, out)
	}
	return nil
}

func marshalYAML(obj interface{}) string {
	b, err := yaml.Marshal(obj)
	if err != nil {
		panic(fmt.Sprintf("marshalYAML: %v", err))
	}
	return string(b)
}

// applyNetworkPolicy renders obj to YAML and applies it.
func applyNetworkPolicy(ctx context.Context, np *networkingv1.NetworkPolicy) error {
	np.TypeMeta = metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"}
	return applyYAML(ctx, marshalYAML(np))
}

func deleteNetworkPolicy(ctx context.Context, namespace, name string) error {
	_, err := kubectl(ctx, "delete", "networkpolicy", name, "-n", namespace, "--ignore-not-found")
	return err
}

// ensureNamespace creates namespace ns idempotently.
func ensureNamespace(ctx context.Context, ns string) error {
	out, err := kubectl(ctx, "create", "namespace", ns)
	if err != nil && !strings.Contains(out, "AlreadyExists") {
		return err
	}
	return nil
}

// deployStub creates a Deployment running http-echo (listening on port,
// responding 200) plus a matching Service, in namespace ns, labeled with
// labels. servicePort/targetPort lets callers reproduce OCP's
// AlertManager-style Service-port-remaps-to-container-port DNAT (NPE2E-004).
func deployStub(ctx context.Context, ns, name string, labels map[string]string, servicePort, targetPort int32) error {
	replicas := int32(1)
	dep := &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "stub",
							Image: stubImage,
							Args:  []string{"-listen=:" + fmt.Sprint(targetPort), "-text=ok"},
							Ports: []corev1.ContainerPort{{ContainerPort: targetPort}},
						},
					},
				},
			},
		},
	}
	svc := &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Port: servicePort, TargetPort: intstr.FromInt32(targetPort)},
			},
		},
	}
	doc := marshalYAML(dep) + "\n---\n" + marshalYAML(svc)
	if err := applyYAML(ctx, doc); err != nil {
		return err
	}
	_, err := kubectl(ctx, "wait", "--for=condition=Available", "deployment/"+name, "-n", ns, "--timeout=120s")
	return err
}

// deployClient creates a Deployment running a curl-capable image with no
// listening port, used as the source pod for egress probes (labels
// determine which component NetworkPolicy governs it, e.g. app=
// effectivenessmonitor).
func deployClient(ctx context.Context, ns, name string, labels map[string]string) error {
	replicas := int32(1)
	dep := &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "client",
							Image:   clientImage,
							Command: []string{"sleep", "infinity"},
						},
					},
				},
			},
		},
	}
	if err := applyYAML(ctx, marshalYAML(dep)); err != nil {
		return err
	}
	_, err := kubectl(ctx, "wait", "--for=condition=Available", "deployment/"+name, "-n", ns, "--timeout=120s")
	return err
}

// probeTCP execs into the first pod matching labels in systemNamespace
// (where every source-of-traffic fixture in this suite lives -- EM/AF/KA
// all carry their real component NetworkPolicy in that namespace) and
// attempts a TCP connection to host:port, returning true if it succeeds
// within probeTimeout. Uses curl's connect-only mode so it exercises pure
// network reachability rather than any HTTP semantics of the target.
func probeTCP(ctx context.Context, labels map[string]string, host string, port int32) (bool, error) {
	sel := labelSelectorString(labels)
	podName, err := kubectl(ctx, "get", "pod", "-n", systemNamespace, "-l", sel,
		"-o", "jsonpath={.items[0].metadata.name}")
	if err != nil || strings.TrimSpace(podName) == "" {
		return false, fmt.Errorf("finding pod in %s with labels %v: %w", systemNamespace, labels, err)
	}
	podName = strings.TrimSpace(podName)

	target := fmt.Sprintf("%s:%d", host, port)
	connectTimeout := fmt.Sprint(int(probeTimeout.Seconds()))
	_, err = kubectl(ctx, "exec", "-n", systemNamespace, podName, "--",
		"curl", "-s", "-o", "/dev/null", "--connect-timeout", connectTimeout, target)
	if err != nil {
		return false, nil //nolint:nilerr // a curl connect failure is the expected "denied" outcome, not a test-infra error
	}
	return true, nil
}

func labelSelectorString(labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

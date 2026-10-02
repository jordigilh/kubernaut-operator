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
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/jordigilh/kubernaut-operator/internal/policy"
)

type policyProvider string

var (
	configuredProvider policyProvider
	clusterContext     string
)

const (
	providerGeneric policyProvider = "generic"
	providerCilium  policyProvider = "cilium"
	providerCalico  policyProvider = "calico"

	kindNodeImage = "kindest/node:v1.35.0"

	ciliumVersion  = "1.20.2"
	ciliumHelmRepo = "https://helm.cilium.io/"

	calicoVersion          = "v3.31.4"
	tigeraOperatorManifest = "https://raw.githubusercontent.com/projectcalico/calico/" +
		"v3.31.4/manifests/tigera-operator.yaml"

	probeNamespace = "kubernaut-provider-e2e"

	managedPolicyLabel = "kubernaut.ai/managed-policy=true"
)

const calicoInstallation = `apiVersion: operator.tigera.io/v1
kind: Installation
metadata:
  name: default
spec:
  calicoNetwork:
    ipPools:
      - name: default-ipv4-ippool
        blockSize: 26
        cidr: 192.168.0.0/16
        encapsulation: VXLANCrossSubnet
        natOutgoing: Enabled
        nodeSelector: all()
---
apiVersion: operator.tigera.io/v1
kind: APIServer
metadata:
  name: default
spec: {}
`

func providerFromEnvironment() (policyProvider, error) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("KUBERNAUT_E2E_PROVIDER")))
	if value == "" {
		value = string(providerGeneric)
	}
	switch policyProvider(value) {
	case providerGeneric, providerCilium, providerCalico:
		return policyProvider(value), nil
	default:
		return "", fmt.Errorf("invalid KUBERNAUT_E2E_PROVIDER value; expected generic, cilium, or calico")
	}
}

func kindClusterName() string {
	if name := strings.TrimSpace(os.Getenv("KIND_CLUSTER_NAME")); name != "" {
		return name
	}
	return "kubernaut-provider-e2e"
}

func kubeContext() string {
	return "kind-" + kindClusterName()
}

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	_, _ = fmt.Fprintf(GinkgoWriter, "+ %s %s\n", name, strings.Join(args, " ")) //nolint:errcheck
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	_, _ = fmt.Fprint(GinkgoWriter, string(output)) //nolint:errcheck
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(output), nil
}

func runCmdStdin(ctx context.Context, input, name string, args ...string) (string, error) {
	_, _ = fmt.Fprintf(
		GinkgoWriter, "+ %s %s (stdin: %d bytes)\n", name, strings.Join(args, " "), len(input),
	) //nolint:errcheck
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.CombinedOutput()
	_, _ = fmt.Fprint(GinkgoWriter, string(output)) //nolint:errcheck
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(output), nil
}

func kubectl(ctx context.Context, args ...string) (string, error) {
	fullArgs := append([]string{"--context", clusterContext}, args...)
	return runCmd(ctx, "kubectl", fullArgs...)
}

func kubectlStdin(ctx context.Context, input string, args ...string) (string, error) {
	fullArgs := append([]string{"--context", clusterContext}, args...)
	return runCmdStdin(ctx, input, "kubectl", fullArgs...)
}

func helm(ctx context.Context, args ...string) (string, error) {
	return runCmd(ctx, "helm", args...)
}

func helmInCluster(ctx context.Context, args ...string) (string, error) {
	fullArgs := append([]string{"--kube-context", clusterContext}, args...)
	return runCmd(ctx, "helm", fullArgs...)
}

func createKindCluster(ctx context.Context, provider policyProvider) error {
	configFile, err := os.CreateTemp("", "kubernaut-kind-*.yaml")
	if err != nil {
		return fmt.Errorf("creating Kind config: %w", err)
	}
	configPath := configFile.Name()
	defer func() {
		_ = os.Remove(configPath)
	}()

	disableDefaultCNI := provider != providerGeneric
	config := fmt.Sprintf(`kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  disableDefaultCNI: %t
  podSubnet: 10.244.0.0/16
nodes:
  - role: control-plane
    image: %s
`, disableDefaultCNI, kindNodeImage)
	if _, err := configFile.WriteString(config); err != nil {
		_ = configFile.Close()
		return fmt.Errorf("writing Kind config: %w", err)
	}
	if err := configFile.Close(); err != nil {
		return fmt.Errorf("closing Kind config: %w", err)
	}

	if _, err := runCmd(
		ctx, "kind", "create", "cluster", "--name", kindClusterName(), "--config", configPath,
	); err != nil {
		return fmt.Errorf("creating Kind cluster: %w", err)
	}
	return nil
}

func deleteKindCluster(ctx context.Context) {
	if os.Getenv("KEEP_CLUSTER") != "" {
		_, _ = fmt.Fprintf(
			GinkgoWriter, "KEEP_CLUSTER is set; retaining Kind cluster %q\n", kindClusterName(),
		) //nolint:errcheck
		return
	}
	if _, err := runCmd(ctx, "kind", "delete", "cluster", "--name", kindClusterName()); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "warning: deleting Kind cluster failed: %v\n", err) //nolint:errcheck
	}
}

func waitForCluster(ctx context.Context, provider policyProvider) error {
	if provider != providerGeneric {
		return pollUntilSuccess(ctx, 5*time.Minute, 5*time.Second, func() error {
			if _, err := kubectl(ctx, "get", "--raw=/readyz"); err != nil {
				return fmt.Errorf("waiting for Kubernetes API readiness: %w", err)
			}
			return nil
		})
	}
	return waitForNodes(ctx)
}

func waitForNodes(ctx context.Context) error {
	if _, err := kubectl(ctx, "wait", "--for=condition=Ready", "node", "--all", "--timeout=10m"); err != nil {
		return fmt.Errorf("waiting for Kind node: %w", err)
	}
	return nil
}

func waitForDNS(ctx context.Context) error {
	if _, err := kubectl(
		ctx, "wait", "--for=condition=Available", "deployment/coredns", "-n", "kube-system", "--timeout=10m",
	); err != nil {
		return fmt.Errorf("waiting for CoreDNS: %w", err)
	}
	return nil
}

func installCilium(ctx context.Context) error {
	if _, err := helm(ctx, "repo", "add", "cilium", ciliumHelmRepo, "--force-update"); err != nil {
		return fmt.Errorf("adding Cilium Helm repository: %w", err)
	}
	if _, err := helm(ctx, "repo", "update"); err != nil {
		return fmt.Errorf("updating Cilium Helm repository: %w", err)
	}

	args := []string{
		"upgrade", "--install", "cilium", "cilium/cilium",
		"--version", ciliumVersion,
		"--namespace", "kube-system",
		"--set", "image.pullPolicy=IfNotPresent",
		"--set", "kubeProxyReplacement=false",
		"--set", "ipam.mode=kubernetes",
		"--set", "operator.replicas=1",
		"--wait", "--timeout", "10m",
	}
	if _, err := helmInCluster(ctx, args...); err != nil {
		return fmt.Errorf("installing Cilium %s: %w", ciliumVersion, err)
	}
	if _, err := kubectl(ctx, "rollout", "status", "daemonset/cilium", "-n", "kube-system", "--timeout=10m"); err != nil {
		return fmt.Errorf("waiting for Cilium daemonset: %w", err)
	}
	if _, err := kubectl(
		ctx, "rollout", "status", "deployment/cilium-operator", "-n", "kube-system", "--timeout=10m",
	); err != nil {
		return fmt.Errorf("waiting for Cilium operator: %w", err)
	}
	if _, err := kubectl(ctx, "get", "ciliumnetworkpolicies.cilium.io", "-A"); err != nil {
		return fmt.Errorf("checking Cilium policy API: %w", err)
	}
	return nil
}

func installCalico(ctx context.Context) error {
	if _, err := kubectl(ctx, "apply", "-f", tigeraOperatorManifest); err != nil {
		return fmt.Errorf("installing Tigera operator %s: %w", calicoVersion, err)
	}
	if _, err := kubectl(
		ctx, "rollout", "status", "deployment/tigera-operator", "-n", "tigera-operator", "--timeout=10m",
	); err != nil {
		return fmt.Errorf("waiting for Tigera operator: %w", err)
	}

	if err := pollUntilSuccess(ctx, 5*time.Minute, 5*time.Second, func() error {
		if _, err := kubectlStdin(ctx, calicoInstallation, "apply", "-f", "-"); err != nil {
			return fmt.Errorf("applying Calico installation: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	calicoNamespace, err := waitForCalicoNodeNamespace(ctx)
	if err != nil {
		return err
	}
	if _, err := kubectl(
		ctx, "rollout", "status", "daemonset/calico-node", "-n", calicoNamespace, "--timeout=10m",
	); err != nil {
		return fmt.Errorf("waiting for Calico node agent: %w", err)
	}
	if _, err := kubectl(
		ctx, "rollout", "status", "deployment/calico-kube-controllers", "-n", calicoNamespace, "--timeout=10m",
	); err != nil {
		return fmt.Errorf("waiting for Calico controllers: %w", err)
	}
	if err := waitForCalicoAPI(ctx); err != nil {
		return err
	}
	return nil
}

func waitForCalicoNodeNamespace(ctx context.Context) (string, error) {
	var namespace string
	err := pollUntilSuccess(ctx, 10*time.Minute, 5*time.Second, func() error {
		output, err := kubectl(
			ctx, "get", "daemonsets", "-A",
			"-o", "jsonpath={range .items[*]}{.metadata.namespace}/{.metadata.name}{\"\\n\"}{end}",
		)
		if err != nil {
			return err
		}
		for _, resource := range strings.Fields(output) {
			parts := strings.SplitN(resource, "/", 2)
			if len(parts) == 2 && strings.EqualFold(parts[1], "calico-node") {
				namespace = parts[0]
				return nil
			}
		}
		if strings.TrimSpace(output) == "" {
			return fmt.Errorf("calico node daemonset has not been created")
		}
		return fmt.Errorf("calico node daemonset has not been created: %s", strings.TrimSpace(output))
	})
	if err != nil {
		return "", fmt.Errorf("waiting for Calico node daemonset: %w", err)
	}
	return namespace, nil
}

func waitForCalicoAPI(ctx context.Context) error {
	err := pollUntilSuccess(ctx, 10*time.Minute, 5*time.Second, func() error {
		output, err := kubectl(
			ctx, "get", "apiservice", "v3.projectcalico.org",
			"-o", "jsonpath={.status.conditions[?(@.type==\"Available\")].status}",
		)
		if err != nil {
			return err
		}
		if strings.TrimSpace(output) != "True" {
			return fmt.Errorf("calico apiservice is not available: %q", strings.TrimSpace(output))
		}
		if _, err := kubectl(ctx, "get", "networkpolicies.projectcalico.org", "-A"); err != nil {
			return fmt.Errorf("calico policy API is not serving: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("waiting for Calico policy API: %w", err)
	}
	return nil
}

func pollUntilSuccess(ctx context.Context, timeout, interval time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	lastErr := fn()
	for lastErr != nil {
		if time.Now().After(deadline) {
			return lastErr
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
		lastErr = fn()
	}
	return nil
}

func liveDetection(ctx context.Context, requested policy.Provider) (policy.DetectionResult, error) {
	config, err := kubeClientConfig()
	if err != nil {
		return policy.DetectionResult{}, err
	}
	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		return policy.DetectionResult{}, fmt.Errorf("creating Kubernetes HTTP client: %w", err)
	}
	restMapper, err := apiutil.NewDynamicRESTMapper(config, httpClient)
	if err != nil {
		return policy.DetectionResult{}, fmt.Errorf("creating Kubernetes REST mapper: %w", err)
	}

	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		appsv1.AddToScheme,
		apiextensionsv1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			return policy.DetectionResult{}, fmt.Errorf("registering Kubernetes scheme: %w", err)
		}
	}
	kubeClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return policy.DetectionResult{}, fmt.Errorf("creating Kubernetes client: %w", err)
	}
	detector := policy.Detector{Client: kubeClient, RESTMapper: restMapper}
	return policy.Detect(detector.Discover(ctx), requested), nil
}

func kubeClientConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: clusterContext}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig for %s: %w", clusterContext, err)
	}
	return config, nil
}

func collectDiagnostics(ctx context.Context) {
	_, _ = kubectl(ctx, "get", "pods", "-A", "-o", "wide")                 //nolint:errcheck
	_, _ = kubectl(ctx, "get", "events", "-A", "--sort-by=.lastTimestamp") //nolint:errcheck
	if configuredProvider == providerCilium {
		_, _ = kubectl(ctx, "get", "ciliumnetworkpolicies.cilium.io", "-A", "-o", "yaml") //nolint:errcheck
	}
	if configuredProvider == providerCalico {
		_, _ = kubectl(ctx, "get", "networkpolicies.projectcalico.org", "-A", "-o", "yaml") //nolint:errcheck
	}
}

func cleanupProbeNamespace(ctx context.Context) {
	if _, err := kubectl(
		ctx, "delete", "namespace", probeNamespace, "--ignore-not-found=true", "--wait=false",
	); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "warning: deleting probe namespace failed: %v\n", err) //nolint:errcheck
	}
}

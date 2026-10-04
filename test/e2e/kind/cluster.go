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
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

type policyProvider string
type tlsSource string

var (
	configuredProvider policyProvider
	configuredTLS      tlsSource
	clusterContext     string
)

const (
	providerGeneric policyProvider = "generic"
	providerCilium  policyProvider = "cilium"
	providerCalico  policyProvider = "calico"

	tlsDevelopment tlsSource = "development"
	tlsHook        tlsSource = "hook"
	tlsManualAdmin tlsSource = "manual-admin"
	tlsCertManager tlsSource = "certmanager"

	kindNodeImage = "kindest/node:v1.35.0"

	ciliumVersion  = "1.20.2"
	ciliumHelmRepo = "https://helm.cilium.io/"

	calicoVersion             = "v3.31.4"
	calicoOperatorCRDManifest = "https://raw.githubusercontent.com/projectcalico/calico/" +
		"v3.31.4/manifests/operator-crds.yaml"
	tigeraOperatorManifest = "https://raw.githubusercontent.com/projectcalico/calico/" +
		"v3.31.4/manifests/tigera-operator.yaml"

	certManagerVersion  = "v1.20.2"
	certManagerManifest = "https://github.com/cert-manager/cert-manager/releases/download/" +
		certManagerVersion + "/cert-manager.yaml"
	certManagerOwnerReferencePatch = `[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":` +
		`"--enable-certificate-owner-ref=true"}]`

	operatorNamespace  = "kubernaut-operator-system"
	kubernautNamespace = "kubernaut-system"
	conditionTrue      = "True"
	// Provider probes run in the Kubernaut namespace so the operator's
	// namespace-scoped policy selectors are exercised by the real workload.
	probeNamespace = kubernautNamespace

	defaultOperatorImage   = "localhost/kubernaut-operator:1.6.0-rc20"
	defaultContractImage   = "localhost/kubernaut-operator-e2e-contract:1.6.0-rc20"
	operatorDeploymentName = "kubernaut-operator-controller-manager"

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

func tlsSourceFromEnvironment() (tlsSource, error) {
	return tlsSourceForValue(os.Getenv("KUBERNAUT_E2E_TLS_SOURCE"))
}

// TLSSourceForValue is the pure source-selector contract used by the Kind
// harness and its unit tests. The returned values intentionally describe the
// infrastructure lane rather than an API mode: manual and
// AdministratorManaged share one isolated lane but are exercised separately by
// the journey.
func TLSSourceForValue(value string) (string, error) {
	source, err := tlsSourceForValue(value)
	return string(source), err
}

// TLSModeForSource maps a Kind lane selector and manual/admin scenario
// selection to the exact v1alpha2 TLS mode emitted by the production CR
// fixture. Keeping this pure makes it possible to test that a lane cannot
// silently fall back to DevelopmentSelfSigned.
func TLSModeForSource(source, manualSelection string) (kubernautv1alpha2.TLSMode, error) {
	lane, err := tlsSourceForValue(source)
	if err != nil {
		return "", err
	}
	switch lane {
	case tlsDevelopment:
		return kubernautv1alpha2.TLSModeDevelopmentSelfSigned, nil
	case tlsHook:
		return kubernautv1alpha2.TLSModeHook, nil
	case tlsCertManager:
		return kubernautv1alpha2.TLSModeCertManager, nil
	case tlsManualAdmin:
		switch strings.ToLower(strings.TrimSpace(manualSelection)) {
		case "", string(manualTLSSelectionManual):
			return kubernautv1alpha2.TLSModeManual, nil
		case string(manualTLSSelectionAdmin), "administratormanaged":
			return kubernautv1alpha2.TLSModeAdministratorManaged, nil
		default:
			return "", fmt.Errorf("invalid manual TLS selection %q; expected manual or administrator-managed", manualSelection)
		}
	default:
		return "", fmt.Errorf("unsupported TLS lane %q", lane)
	}
}

func tlsSourceForValue(value string) (tlsSource, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "development", "developmentsigned", "developmentselfsigned", "development-self-signed":
		return tlsDevelopment, nil
	case "hook":
		return tlsHook, nil
	case "manual-admin", string(manualTLSSelectionManual), "administrator-managed", "administratormanaged":
		return tlsManualAdmin, nil
	case "certmanager", "cert-manager":
		return tlsCertManager, nil
	default:
		return "", fmt.Errorf(
			"invalid KUBERNAUT_E2E_TLS_SOURCE value; expected development, hook, manual-admin, or certmanager",
		)
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

func operatorImage() string {
	if image := strings.TrimSpace(os.Getenv("KUBERNAUT_OPERATOR_IMAGE")); image != "" {
		return image
	}
	return defaultOperatorImage
}

func contractImage() string {
	if image := strings.TrimSpace(os.Getenv("KUBERNAUT_E2E_CONTRACT_IMAGE")); image != "" {
		return image
	}
	return defaultContractImage
}

func kustomizeBinary() string {
	if binary := strings.TrimSpace(os.Getenv("KUSTOMIZE_BIN")); binary != "" {
		return binary
	}
	return "kustomize"
}

func loadOperatorImage(ctx context.Context) error {
	if _, err := runCmd(ctx, "kind", "load", "docker-image", operatorImage(), "--name", kindClusterName()); err != nil {
		return fmt.Errorf("loading operator image %q into Kind: %w", operatorImage(), err)
	}
	return nil
}

func loadInfrastructureImages(ctx context.Context) error {
	for _, image := range []string{contractImage(), postgresImage, valkeyImage} {
		if _, err := runCmd(ctx, "kind", "load", "docker-image", image, "--name", kindClusterName()); err != nil {
			return fmt.Errorf("loading contract or dependency image %q into Kind: %w", image, err)
		}
	}
	return nil
}

func installOperator(ctx context.Context) error {
	manifests, err := runCmd(ctx, kustomizeBinary(), "build", filepath.Join(repositoryRoot(), "config", "kind-e2e"))
	if err != nil {
		return fmt.Errorf("building Kind operator manifests: %w", err)
	}
	if _, err := kubectlStdin(ctx, manifests, "apply", "-f", "-"); err != nil {
		return fmt.Errorf("installing Kind operator manifests: %w", err)
	}
	if _, err := kubectl(
		ctx, "set", "image", "deployment/"+operatorDeploymentName,
		"manager="+operatorImage(), "-n", operatorNamespace,
	); err != nil {
		return fmt.Errorf("selecting operator image %q: %w", operatorImage(), err)
	}
	if _, err := kubectl(
		ctx, "rollout", "status", "deployment/"+operatorDeploymentName,
		"-n", operatorNamespace, "--timeout=10m",
	); err != nil {
		return fmt.Errorf("waiting for operator deployment: %w", err)
	}
	return nil
}

func repositoryRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
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
	podSubnet := "10.244.0.0/16"
	if provider == providerCalico {
		podSubnet = "192.168.0.0/16"
	}
	config := fmt.Sprintf(`kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  disableDefaultCNI: %t
  podSubnet: %s
kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      taints: []
nodes:
  - role: control-plane
    image: %s
  - role: worker
    image: %s
`, disableDefaultCNI, podSubnet, kindNodeImage, kindNodeImage)
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
	// Create avoids kubectl's client-side last-applied annotation, which exceeds
	// Kubernetes' annotation size limit for the large Installation CRD schema.
	if _, err := kubectl(ctx, "create", "-f", calicoOperatorCRDManifest); err != nil {
		return fmt.Errorf("installing calico operator CRDs %s: %w", calicoVersion, err)
	}
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

func installCertManager(ctx context.Context) error {
	if _, err := kubectl(ctx, "apply", "-f", certManagerManifest); err != nil {
		return fmt.Errorf("installing cert-manager %s: %w", certManagerVersion, err)
	}
	// The release manifest leaves certificate owner references disabled by
	// default. Enable them so the E2E can verify that cert-manager, rather than
	// the Kubernaut operator, owns the generated TLS Secrets.
	if _, err := kubectl(
		ctx, "patch", "deployment/cert-manager", "-n", "cert-manager",
		"--type=json", "-p", certManagerOwnerReferencePatch,
	); err != nil {
		return fmt.Errorf("enabling cert-manager Certificate owner references: %w", err)
	}
	for _, deployment := range []string{"cert-manager", "cert-manager-cainjector", "cert-manager-webhook"} {
		if _, err := kubectl(
			ctx, "rollout", "status", "deployment/"+deployment,
			"-n", "cert-manager", "--timeout=10m",
		); err != nil {
			return fmt.Errorf("waiting for cert-manager deployment %s: %w", deployment, err)
		}
	}
	for _, crd := range []string{
		"issuers.cert-manager.io",
		"certificates.cert-manager.io",
		"clusterissuers.cert-manager.io",
	} {
		if _, err := kubectl(ctx, "wait", "--for=condition=Established", "crd/"+crd, "--timeout=5m"); err != nil {
			return fmt.Errorf("waiting for cert-manager CRD %s: %w", crd, err)
		}
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
		if strings.TrimSpace(output) != conditionTrue {
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

func collectDiagnostics(ctx context.Context) {
	_, _ = kubectl(ctx, "get", "pods", "-A", "-o", "wide")                          //nolint:errcheck
	_, _ = kubectl(ctx, "get", "events", "-A", "--sort-by=.lastTimestamp")          //nolint:errcheck
	_, _ = kubectl(ctx, "get", "kubernaut", "-n", kubernautNamespace, "-o", "yaml") //nolint:errcheck
	if configuredProvider == providerCilium {
		_, _ = kubectl(ctx, "get", "ciliumnetworkpolicies.cilium.io", "-A", "-o", "yaml") //nolint:errcheck
	}
	if configuredProvider == providerCalico {
		_, _ = kubectl(ctx, "get", "networkpolicies.projectcalico.org", "-A", "-o", "yaml") //nolint:errcheck
	}
}

func cleanupProbeWorkloads(ctx context.Context) {
	if err := deleteProbeWorkloads(ctx); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "warning: deleting probe workloads failed: %v\n", err) //nolint:errcheck
	}
}

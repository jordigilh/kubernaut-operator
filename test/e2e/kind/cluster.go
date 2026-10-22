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

type tlsSource string

var (
	configuredTLS  tlsSource
	clusterContext string
)

const (
	tlsManualAdmin tlsSource = "manual-admin"
	tlsCertManager tlsSource = "certmanager"

	kindNodeImage = "kindest/node:v1.35.0"

	certManagerVersion  = "v1.20.2"
	certManagerManifest = "https://github.com/cert-manager/cert-manager/releases/download/" +
		certManagerVersion + "/cert-manager.yaml"
	certManagerOwnerReferencePatch = `[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":` +
		`"--enable-certificate-owner-ref=true"}]`

	operatorNamespace   = "kubernaut-operator-system"
	kubernautNamespace  = "kubernaut-system"
	monitoringNamespace = "kubernaut-e2e-monitoring"
	conditionTrue       = "True"
	// Portable policy probes run in the Kubernaut namespace so the operator's
	// namespace-scoped selectors are exercised by the real workload.
	probeNamespace = kubernautNamespace

	defaultOperatorImage   = "localhost/kubernaut-operator:ci"
	defaultContractImage   = "localhost/kubernaut-operator-e2e-contract:ci"
	operatorDeploymentName = "kubernaut-operator-controller-manager"

	managedPolicyLabel = "kubernaut.ai/managed-policy=true"
)

func tlsSourceFromEnvironment() (tlsSource, error) {
	return tlsSourceForValue(os.Getenv("KUBERNAUT_E2E_TLS_SOURCE"))
}

// TLSSourceForValue is the pure source-selector contract used by the Kind
// harness and its unit tests.
func TLSSourceForValue(value string) (string, error) {
	source, err := tlsSourceForValue(value)
	return string(source), err
}

// TLSModeForSource maps a Kind lane selector and manual scenario selection to
// the exact v1alpha2 TLS mode emitted by the production CR fixture.
func TLSModeForSource(source, manualSelection string) (kubernautv1alpha2.TLSMode, error) {
	lane, err := tlsSourceForValue(source)
	if err != nil {
		return "", err
	}
	switch lane {
	case tlsCertManager:
		return kubernautv1alpha2.TLSModeCertManager, nil
	case tlsManualAdmin:
		switch strings.ToLower(strings.TrimSpace(manualSelection)) {
		case "", string(manualTLSSelectionManual):
			return kubernautv1alpha2.TLSModeManual, nil
		case string(manualTLSSelectionAdmin):
			return kubernautv1alpha2.TLSModeManual, nil
		default:
			return "", fmt.Errorf("invalid manual TLS selection %q; expected manual or admin", manualSelection)
		}
	default:
		return "", fmt.Errorf("unsupported TLS lane %q", lane)
	}
}

func tlsSourceForValue(value string) (tlsSource, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "manual-admin", string(manualTLSSelectionManual), "admin":
		return tlsManualAdmin, nil
	case "", "certmanager", "cert-manager":
		return tlsCertManager, nil
	default:
		return "", fmt.Errorf(
			"invalid KUBERNAUT_E2E_TLS_SOURCE value; expected certmanager or manual-admin",
		)
	}
}

func kindClusterName() string {
	if name := strings.TrimSpace(os.Getenv("KIND_CLUSTER_NAME")); name != "" {
		return name
	}
	return "kubernaut-e2e"
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

func helmInCluster(ctx context.Context, args ...string) (string, error) {
	fullArgs := append([]string{"--kube-context", clusterContext}, args...)
	return runCmd(ctx, helmBinary(), fullArgs...)
}

func helmBinary() string {
	if binary := strings.TrimSpace(os.Getenv("HELM_BIN")); binary != "" {
		return binary
	}
	return "helm"
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

func loadOperatorImage(ctx context.Context) error {
	if _, err := runCmd(ctx, "kind", "load", "docker-image", operatorImage(), "--name", kindClusterName()); err != nil {
		return fmt.Errorf("loading operator image %q into Kind: %w", operatorImage(), err)
	}
	return nil
}

func loadInfrastructureImages(ctx context.Context) error {
	images := []string{contractImage(), postgresImage, valkeyImage}
	for _, image := range images {
		if _, err := runCmd(ctx, "kind", "load", "docker-image", image, "--name", kindClusterName()); err != nil {
			return fmt.Errorf("loading contract or dependency image %q into Kind: %w", image, err)
		}
	}
	return nil
}

func installOperator(ctx context.Context) error {
	imageArgs, err := operatorImageValues(operatorImage())
	if err != nil {
		return err
	}
	args := make([]string, 0, 11+len(imageArgs))
	args = append(args,
		"install", "kubernaut-operator", operatorChartPath(),
		"--namespace", operatorNamespace,
		"--create-namespace",
		"--set", "webhook.tls.mode=development",
		"--wait", "--timeout", "10m",
	)
	args = append(args, imageArgs...)
	if _, err := helmInCluster(ctx, args...); err != nil {
		return fmt.Errorf("installing operator Helm chart: %w", err)
	}
	if _, err := kubectl(
		ctx, "rollout", "status", "deployment/"+operatorDeploymentName,
		"-n", operatorNamespace, "--timeout=10m",
	); err != nil {
		return fmt.Errorf("waiting for operator deployment: %w", err)
	}
	return nil
}

func operatorImageValues(image string) ([]string, error) {
	if at := strings.LastIndex(image, "@"); at > strings.LastIndex(image, "/") {
		return []string{
			"--set-string", "image.repository=" + image[:at],
			"--set-string", "image.digest=" + image[at+1:],
		}, nil
	}
	colon := strings.LastIndex(image, ":")
	if colon <= strings.LastIndex(image, "/") {
		return nil, fmt.Errorf("operator image %q must include a tag or digest", image)
	}
	return []string{
		"--set-string", "image.repository=" + image[:colon],
		"--set-string", "image.tag=" + image[colon+1:],
	}, nil
}

func operatorChartPath() string {
	return filepath.Join(repositoryRoot(), "charts", "kubernaut-operator")
}

func repositoryRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func createKindCluster(ctx context.Context) error {
	configFile, err := os.CreateTemp("", "kubernaut-kind-*.yaml")
	if err != nil {
		return fmt.Errorf("creating Kind config: %w", err)
	}
	configPath := configFile.Name()
	defer func() { _ = os.Remove(configPath) }()
	config := fmt.Sprintf(`kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  disableDefaultCNI: false
  podSubnet: 10.244.0.0/16
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
`, kindNodeImage, kindNodeImage)
	if _, err := configFile.WriteString(config); err != nil {
		_ = configFile.Close()
		return fmt.Errorf("writing Kind config: %w", err)
	}
	if err := configFile.Close(); err != nil {
		return fmt.Errorf("closing Kind config: %w", err)
	}
	if _, err := runCmd(ctx, "kind", "create", "cluster", "--name", kindClusterName(),
		"--config", configPath); err != nil {
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

func waitForCluster(ctx context.Context) error { return waitForNodes(ctx) }

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

//nolint:unparam // all current E2E callers intentionally use a two-second cadence.
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
	_, _ = kubectl(ctx, "logs", "deployment/"+operatorDeploymentName, "-n", operatorNamespace,
		"--all-containers", "--tail=200") //nolint:errcheck
}

func cleanupProbeWorkloads(ctx context.Context) {
	if err := deleteProbeWorkloads(ctx); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "warning: deleting probe workloads failed: %v\n", err) //nolint:errcheck
	}
}

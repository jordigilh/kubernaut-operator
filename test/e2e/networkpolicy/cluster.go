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

// Package networkpolicy is an e2e suite that proves the operator's
// generated NetworkPolicy objects (internal/resources.NetworkPolicies())
// actually allow/deny the traffic they claim to, on a real
// NetworkPolicy-enforcing CNI. Neither this repo's UT tier (rendered-object
// shape only) nor its envtest-backed IT tier (no CNI at all) can prove
// enforcement -- see #342.
//
// This suite provisions its own throwaway kind cluster with Calico
// (kindnet, kind's default CNI, is not used here because it hardcodes
// FailOpen: true, which silently allows traffic when a policy is
// malformed -- unacceptable for negative/deny assertions). Versions are
// pinned to what was validated in the #342 preflight spike:
//   - kind node image kindest/node:v1.36.1 (matches upstream kubernaut CI)
//   - Calico v3.29.3 via the Tigera operator (testdata/calico/)
package networkpolicy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
)

const (
	// kindNodeImage pins the kind node's Kubernetes version. Matches
	// upstream kubernaut CI's own pin, validated in the #342 spike.
	kindNodeImage = "kindest/node:v1.36.1"

	// calicoManifestDir holds the pinned Calico v3.29.3 manifests
	// (vendored, not fetched at runtime, so CI doesn't depend on
	// raw.githubusercontent.com availability). See testdata/calico/README
	// for the exact upstream source and version.
	calicoManifestDir = "testdata/calico"

	clusterReadyTimeout = 3 * time.Minute
)

// kindClusterName is the throwaway cluster used for this suite. Override
// via KIND_CLUSTER_NAME to avoid collisions when multiple suites/users
// share a CI runner or dev host.
func kindClusterName() string {
	if name := os.Getenv("KIND_CLUSTER_NAME"); name != "" {
		return name
	}
	return "kubernaut-npe2e"
}

// kubeContext is the kubeconfig context kind writes for a cluster named by
// kindClusterName().
func kubeContext() string {
	return "kind-" + kindClusterName()
}

// runCmd executes name with args, streaming combined output to
// GinkgoWriter for CI log visibility, and returns the combined output plus
// any error. Deliberately minimal (no cmd.Dir mutation, no global os.Chdir)
// so it's safe to call from any working directory without side effects on
// other suites in the same test binary.
func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	fmt.Fprintf(GinkgoWriter, "+ %s %s\n", name, strings.Join(args, " ")) //nolint:errcheck
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(out)) //nolint:errcheck
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}

// kubectl runs kubectl against this suite's kind cluster context.
func kubectl(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"--context", kubeContext()}, args...)
	return runCmd(ctx, "kubectl", full...)
}

// pollUntilSuccess retries fn every interval until it returns nil or
// timeout elapses, returning fn's last error on timeout. Uses
// time.NewTimer+select rather than time.Sleep (forbidden repo-wide by
// forbidigo) so it also respects ctx cancellation between attempts.
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
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		lastErr = fn()
	}
	return nil
}

// runCmdStdin is runCmd, but feeds stdin (e.g. a YAML document for
// `kubectl apply -f -`) to the child process.
func runCmdStdin(ctx context.Context, stdin, name string, args ...string) (string, error) {
	fmt.Fprintf(GinkgoWriter, "+ %s %s (stdin: %d bytes)\n", name, strings.Join(args, " "), len(stdin)) //nolint:errcheck
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	fmt.Fprint(GinkgoWriter, string(out)) //nolint:errcheck
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}

// createKindCluster creates a throwaway kind cluster with the default CNI
// disabled (Calico is installed separately by installCalico). Reuses an
// existing cluster of the same name if one is already running (useful for
// local iteration via KEEP_CLUSTER=1), otherwise creates fresh.
func createKindCluster(ctx context.Context) error {
	existing, _ := runCmd(ctx, "kind", "get", "clusters")
	for _, line := range strings.Split(existing, "\n") {
		if strings.TrimSpace(line) == kindClusterName() {
			fmt.Fprintf(GinkgoWriter, "reusing existing kind cluster %q\n", kindClusterName()) //nolint:errcheck
			return nil
		}
	}

	cfgPath := filepath.Join(os.TempDir(), "kind-config-"+kindClusterName()+".yaml")
	cfg := fmt.Sprintf(`kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  disableDefaultCNI: true
  podSubnet: 192.168.0.0/16
nodes:
  - role: control-plane
    image: %s
`, kindNodeImage)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		return fmt.Errorf("writing kind config: %w", err)
	}

	if _, err := runCmd(ctx, "kind", "create", "cluster", "--name", kindClusterName(), "--config", cfgPath); err != nil {
		return fmt.Errorf("kind create cluster: %w", err)
	}
	return nil
}

// deleteKindCluster tears down the suite's cluster, unless KEEP_CLUSTER is
// set (handy for local debugging of a failed run).
func deleteKindCluster(ctx context.Context) {
	if os.Getenv("KEEP_CLUSTER") != "" {
		fmt.Fprintf(GinkgoWriter, "KEEP_CLUSTER set, leaving cluster %q running\n", kindClusterName()) //nolint:errcheck
		return
	}
	if _, err := runCmd(ctx, "kind", "delete", "cluster", "--name", kindClusterName()); err != nil {
		fmt.Fprintf(GinkgoWriter, "warning: kind delete cluster failed: %v\n", err) //nolint:errcheck
	}
}

// installCalico applies the vendored, pinned Calico v3.29.3 manifests via
// the Tigera operator and waits for the installation to report Available.
// No-ops if Calico is already installed (reused-cluster local iteration).
func installCalico(ctx context.Context) error {
	if out, err := kubectl(ctx, "get", "tigerastatus", "calico"); err == nil && strings.Contains(out, "True") {
		fmt.Fprintln(GinkgoWriter, "Calico already installed and available, skipping install") //nolint:errcheck
		return nil
	}

	operatorManifest := filepath.Join(calicoManifestDir, "tigera-operator.yaml")
	crManifest := filepath.Join(calicoManifestDir, "custom-resources.yaml")

	if out, err := kubectl(ctx, "create", "-f", operatorManifest); err != nil && !strings.Contains(out, "AlreadyExists") {
		return fmt.Errorf("installing tigera-operator: %w", err)
	}
	if _, err := kubectl(ctx, "wait", "--for=condition=Available", "deployment/tigera-operator",
		"-n", "tigera-operator", "--timeout="+clusterReadyTimeout.String()); err != nil {
		return fmt.Errorf("waiting for tigera-operator: %w", err)
	}
	if out, err := kubectl(ctx, "create", "-f", crManifest); err != nil && !strings.Contains(out, "AlreadyExists") {
		return fmt.Errorf("installing calico custom-resources: %w", err)
	}
	// The tigera-operator reconciles the Installation CR asynchronously, so
	// the tigerastatus/{calico,apiserver} objects this waits on may not
	// exist yet the instant custom-resources.yaml is created -- unlike a
	// Deployment (created synchronously by `kubectl create -f`), `kubectl
	// wait` errors immediately (NotFound) rather than waiting for a
	// not-yet-existing object, so this polls the whole wait call rather
	// than relying on kubectl wait's own internal retry alone.
	if err := pollUntilSuccess(ctx, clusterReadyTimeout, 5*time.Second, func() error {
		_, err := kubectl(ctx, "wait", "--for=condition=Available", "tigerastatus/calico", "--timeout=30s")
		return err
	}); err != nil {
		return fmt.Errorf("waiting for calico rollout: %w", err)
	}
	if err := pollUntilSuccess(ctx, clusterReadyTimeout, 5*time.Second, func() error {
		_, err := kubectl(ctx, "wait", "--for=condition=Available", "tigerastatus/apiserver", "--timeout=30s")
		return err
	}); err != nil {
		return fmt.Errorf("waiting for calico apiserver rollout: %w", err)
	}
	if _, err := kubectl(ctx, "wait", "--for=condition=Ready", "node", "--all",
		"--timeout="+clusterReadyTimeout.String()); err != nil {
		return fmt.Errorf("waiting for node ready: %w", err)
	}
	// CoreDNS pods are scheduled at cluster-create time but can't get a pod
	// IP (and therefore can't become Ready) until a CNI is installed --
	// fixtures.go's stub Services rely on in-cluster DNS resolution, so
	// this must be Available before any test runs.
	if _, err := kubectl(ctx, "wait", "--for=condition=Available", "deployment/coredns",
		"-n", "kube-system", "--timeout="+clusterReadyTimeout.String()); err != nil {
		return fmt.Errorf("waiting for coredns: %w", err)
	}
	return nil
}

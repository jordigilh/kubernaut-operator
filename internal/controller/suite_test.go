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
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/api/config/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kubernautaiv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	for k, v := range map[string]string{
		"RELATED_IMAGE_GATEWAY":                 "quay.io/kubernaut-ai/gateway:test",
		"RELATED_IMAGE_DATA_STORAGE":            "quay.io/kubernaut-ai/datastorage:test",
		"RELATED_IMAGE_AIANALYSIS":              "quay.io/kubernaut-ai/aianalysis:test",
		"RELATED_IMAGE_SIGNALPROCESSING":        "quay.io/kubernaut-ai/signalprocessing:test",
		"RELATED_IMAGE_REMEDIATIONORCHESTRATOR": "quay.io/kubernaut-ai/remediationorchestrator:test",
		"RELATED_IMAGE_WORKFLOWEXECUTION":       "quay.io/kubernaut-ai/workflowexecution:test",
		"RELATED_IMAGE_EFFECTIVENESSMONITOR":    "quay.io/kubernaut-ai/effectivenessmonitor:test",
		"RELATED_IMAGE_NOTIFICATION":            "quay.io/kubernaut-ai/notification:test",
		"RELATED_IMAGE_KUBERNAUT_AGENT":         "quay.io/kubernaut-ai/kubernautagent:test",
		"RELATED_IMAGE_AUTHWEBHOOK":             "quay.io/kubernaut-ai/authwebhook:test",
		"RELATED_IMAGE_API_FRONTEND":            "quay.io/kubernaut-ai/apifrontend:test",
		"RELATED_IMAGE_CONSOLE":                 "quay.io/kubernaut-ai/console:test",
		"RELATED_IMAGE_DB_MIGRATE":              "quay.io/kubernaut-ai/db-migrate:test",
		"RELATED_IMAGE_INIT_UBI_MINIMAL":        "registry.access.redhat.com/ubi10/ubi-minimal:latest",
		"RELATED_IMAGE_OAUTH2_PROXY":            "quay.io/oauth2-proxy/oauth2-proxy:v7.9.0",
	} {
		Expect(os.Setenv(k, v)).To(Succeed())
	}

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = kubernautaiv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = apiextensionsv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	Expect(configv1.Install(scheme.Scheme)).To(Succeed())
	err = monitoringv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = monitoringv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	By("bootstrapping test environment")
	openshiftModDir := modDir("github.com/openshift/api")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		CRDInstallOptions: envtest.CRDInstallOptions{
			Paths: []string{
				filepath.Join(openshiftModDir, "config", "v1",
					"zz_generated.crd-manifests",
					"0000_10_config-operator_01_ingresses.crd.yaml"),
			},
		},
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	clientset, err := kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	finalizeTerminatingNamespaces(ctx, clientset)
})

// finalizeTerminatingNamespaces runs for the life of the suite, clearing a
// Namespace's spec.finalizers as soon as it enters Terminating (#358, #359).
//
// envtest runs a real kube-apiserver but no kube-controller-manager, so the
// namespace-lifecycle controller that normally does this after sweeping a
// terminating namespace's contents never runs -- a Namespace, once deleted,
// keeps its default "kubernetes" finalizer and its object is never actually
// removed from etcd for the life of the test binary (a documented envtest
// limitation: https://github.com/kubernetes-sigs/controller-runtime/issues/880).
// Every operator-managed Kubernaut CR shares one hardcoded workflow
// namespace (resources.DefaultWorkflowNamespace) across the whole
// internal/controller suite; any spec whose finalizer cleanup legitimately
// deletes it (KubernautReconciler.deleteOperatorManagedWorkflowNamespace)
// would otherwise permanently wedge that namespace in Terminating, and every
// later spec in the same test binary that tries to create content in it
// fails with "forbidden: unable to create new content ... because it is
// being terminated" -- the exact cascading-failure signature reported in
// #358/#359. This makes namespace deletion behave like a real cluster for
// the rest of the suite, eliminating the wedge at its structural root
// instead of relying on every current and future spec remembering to call
// stripWorkflowNamespaceCreatedByAnnotation before it deletes the CR.
func finalizeTerminatingNamespaces(ctx context.Context, clientset *kubernetes.Clientset) {
	go func() {
		defer GinkgoRecover()
		for {
			if ctx.Err() != nil {
				return
			}
			watcher, err := clientset.CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{})
			if err != nil {
				// forbidigo forbids time.Sleep() (TESTING_GUIDELINES.md); a
				// context-aware timer gives the same reconnect backoff
				// without blocking ctx cancellation.
				timer := time.NewTimer(200 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				continue
			}
			drainNamespaceTerminationEvents(ctx, clientset, watcher.ResultChan())
			watcher.Stop()
		}
	}()
}

// drainNamespaceTerminationEvents processes watch events until the channel
// closes (e.g. the envtest apiserver's watch timeout) or ctx is cancelled,
// finalizing any Namespace observed in the Terminating phase.
func drainNamespaceTerminationEvents(ctx context.Context, clientset *kubernetes.Clientset, events <-chan watch.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			ns, ok := event.Object.(*corev1.Namespace)
			if !ok || ns.Status.Phase != corev1.NamespaceTerminating || len(ns.Spec.Finalizers) == 0 {
				continue
			}
			ns.Spec.Finalizers = nil
			if _, err := clientset.CoreV1().Namespaces().Finalize(ctx, ns, metav1.UpdateOptions{}); err != nil && ctx.Err() == nil {
				logf.Log.Info("finalizeTerminatingNamespaces: failed to finalize namespace, will retry on next watch event", "namespace", ns.Name, "error", err.Error())
			}
		}
	}
}

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// modDir returns the on-disk cache directory for a Go module dependency.
func modDir(mod string) string {
	out, err := exec.Command("go", "mod", "download", "-json", mod).CombinedOutput()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "go mod download %s: %s", mod, out)
	var info struct{ Dir string }
	ExpectWithOffset(1, json.Unmarshal(out, &info)).To(Succeed(), "parse go mod download output")
	return info.Dir
}

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "k8s" {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

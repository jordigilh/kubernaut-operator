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

package platformneutrality

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var (
	testContext context.Context
	cancel      context.CancelFunc
	testEnv     *envtest.Environment
	testConfig  *rest.Config
	testScheme  *runtime.Scheme
)

func TestPlatformNeutrality(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Platform Neutrality Suite")
}

var _ = BeforeSuite(func() {
	testContext, cancel = context.WithCancel(context.Background())

	testScheme = runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(testScheme)).To(Succeed())
	Expect(kubernautv1alpha2.AddToScheme(testScheme)).To(Succeed())

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	if binaryAssetsDirectory := getFirstFoundEnvTestBinaryDir(); binaryAssetsDirectory != "" {
		testEnv.BinaryAssetsDirectory = binaryAssetsDirectory
	}
	testConfig, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(testConfig).NotTo(BeNil())
})

// getFirstFoundEnvTestBinaryDir locates envtest binaries when the suite is
// run directly instead of through make setup-envtest. Keep this local because
// the controller package's helper is intentionally unexported.
func getFirstFoundEnvTestBinaryDir() string {
	if configured := os.Getenv("KUBEBUILDER_ASSETS"); configured != "" {
		return configured
	}
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "k8s" {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

var _ = AfterSuite(func() {
	if cancel != nil {
		cancel()
	}
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})

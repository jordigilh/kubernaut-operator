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

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jordigilh/kubernaut-operator/test/utils"
)

var (
	// projectImage is the operator image to deploy. Override via IMG env var.
	// When running on OCP the image must already be pushed to a registry
	// accessible by the cluster (internal registry or external like quay.io).
	projectImage = os.Getenv("IMG")
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting kubernaut-operator E2E test suite (OCP)\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	if projectImage == "" {
		projectImage = "controller:latest"
	}

	By("building the operator image")
	cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", projectImage))
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the operator image")

	By("pushing the operator image")
	cmd = exec.Command("make", "docker-push", fmt.Sprintf("IMG=%s", projectImage))
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to push the operator image")
})

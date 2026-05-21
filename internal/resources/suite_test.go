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

package resources

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestResources(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resources Suite")
}

var _ = BeforeSuite(func() {
	for k, v := range map[string]string{
		"RELATED_IMAGE_GATEWAY":                 "quay.io/kubernaut-ai/gateway:v1.3.0",
		"RELATED_IMAGE_DATA_STORAGE":            "quay.io/kubernaut-ai/datastorage:v1.3.0",
		"RELATED_IMAGE_AIANALYSIS":              "quay.io/kubernaut-ai/aianalysis:v1.3.0",
		"RELATED_IMAGE_SIGNALPROCESSING":        "quay.io/kubernaut-ai/signalprocessing:v1.3.0",
		"RELATED_IMAGE_REMEDIATIONORCHESTRATOR": "quay.io/kubernaut-ai/remediationorchestrator:v1.3.0",
		"RELATED_IMAGE_WORKFLOWEXECUTION":       "quay.io/kubernaut-ai/workflowexecution:v1.3.0",
		"RELATED_IMAGE_EFFECTIVENESSMONITOR":    "quay.io/kubernaut-ai/effectivenessmonitor:v1.3.0",
		"RELATED_IMAGE_NOTIFICATION":            "quay.io/kubernaut-ai/notification:v1.3.0",
		"RELATED_IMAGE_KUBERNAUT_AGENT":         "quay.io/kubernaut-ai/kubernautagent:v1.3.0",
		"RELATED_IMAGE_AUTHWEBHOOK":             "quay.io/kubernaut-ai/authwebhook:v1.3.0",
		"RELATED_IMAGE_API_FRONTEND":            "quay.io/kubernaut-ai/apifrontend:v1.3.0",
		"RELATED_IMAGE_DB_MIGRATE":              "quay.io/kubernaut-ai/db-migrate:v1.3.0",
	} {
		Expect(os.Setenv(k, v)).To(Succeed())
	}
})

var _ = AfterSuite(func() {
	for _, k := range []string{
		"RELATED_IMAGE_GATEWAY",
		"RELATED_IMAGE_DATA_STORAGE",
		"RELATED_IMAGE_AIANALYSIS",
		"RELATED_IMAGE_SIGNALPROCESSING",
		"RELATED_IMAGE_REMEDIATIONORCHESTRATOR",
		"RELATED_IMAGE_WORKFLOWEXECUTION",
		"RELATED_IMAGE_EFFECTIVENESSMONITOR",
		"RELATED_IMAGE_NOTIFICATION",
		"RELATED_IMAGE_KUBERNAUT_AGENT",
		"RELATED_IMAGE_AUTHWEBHOOK",
		"RELATED_IMAGE_API_FRONTEND",
		"RELATED_IMAGE_DB_MIGRATE",
	} {
		Expect(os.Unsetenv(k)).To(Succeed())
	}
})

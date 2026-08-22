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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/yaml"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// minimalSampleCRPath is relative to this package (internal/resources) --
// the same file docs/installation/00-quickstart.md embeds verbatim.
const minimalSampleCRPath = "../../config/samples/v1alpha2_kubernaut_minimal.yaml"

// loadMinimalSampleCR reads and unmarshals the quickstart sample CR,
// failing the calling spec immediately if the file is missing or malformed
// (a doc-drift signal in its own right, distinct from validation failures).
func loadMinimalSampleCR() *kubernautv1alpha2.Kubernaut {
	data, err := os.ReadFile(minimalSampleCRPath)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "quickstart minimal sample CR must exist and be readable")

	knV2 := &kubernautv1alpha2.Kubernaut{}
	ExpectWithOffset(1, yaml.Unmarshal(data, knV2)).To(Succeed(), "quickstart minimal sample CR must be valid YAML")
	return knV2
}

// BR-UX-001 (ADR-CRD-001): CM-6 -- the quickstart doc's minimal sample CR
// must stay in lockstep with the operator's actual business-rule
// validation (internal/resources/validation.go). If a future change to
// validation.go adds a new reconcile-time requirement, this spec fails
// instead of the sample silently going stale (see #173/#374 for the
// operator/Helm drift class this guards against).
//
// This spec alone cannot catch drift in the CRD's structural schema
// (fields with no omitempty/+optional) -- that layer is only enforced by
// API-server admission and is covered separately by the envtest-backed IT
// in internal/controller (IT-CRD-MINSAMPLE-001).
var _ = Describe("Quickstart minimal sample CR [BR-UX-001, CM-6]", func() {
	It("passes ValidateKubernaut and ValidateFleet with zero errors", func() {
		knV2 := loadMinimalSampleCR()

		kn := &kubernautv1alpha1.Kubernaut{}
		Expect(kn.ConvertFrom(knV2)).To(Succeed(), "the v1alpha2 sample must convert cleanly to the v1alpha1 spoke view")

		errs := ValidateKubernaut(kn, KagentiSidecarNone)
		Expect(errs).To(BeEmpty(), "the quickstart doc's minimal sample must be a genuinely valid CR")

		errs = ValidateFleet(knV2)
		Expect(errs).To(BeEmpty(), "the quickstart doc's minimal sample must be valid against Fleet's v1alpha2 rules too (fleet is unset/disabled here, so this should trivially pass)")
	})
})

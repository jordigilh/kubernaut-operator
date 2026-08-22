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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/yaml"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// quickstartMinimalSampleCRPath mirrors internal/resources/sample_cr_test.go's
// path constant -- both specs load the exact same file from disk to prove it
// stays valid against both independent enforcement layers: Go-level
// business rules there (ValidateKubernaut/ValidateFleet), CRD structural
// schema (real apiserver admission) here.
const quickstartMinimalSampleCRPath = "../../config/samples/v1alpha2_kubernaut_minimal.yaml"

// BR-UX-001 (ADR-CRD-001), CM-6: proves docs/installation/00-quickstart.md's
// minimal sample CR is still admission-valid against the CRD's real
// structural schema (fields with no omitempty/+optional in
// api/v1alpha2/kubernaut_types.go). This is the one enforcement layer
// internal/resources/sample_cr_test.go's UT cannot reach -- schema
// admission happens before any Go reconcile/validation code ever runs, so
// only a real apiserver (envtest here) can catch drift in it. If a future
// schema change (e.g. a new required field) silently makes the sample
// invalid, this spec fails instead of the doc going stale (the same class
// of drift #173/#374 already burned time on for operator/Helm parity).
var _ = Describe("Quickstart minimal sample CR admission [BR-UX-001, CM-6]", func() {
	It("is accepted by the real apiserver's CRD structural schema", func() {
		data, err := os.ReadFile(quickstartMinimalSampleCRPath)
		Expect(err).NotTo(HaveOccurred(), "quickstart minimal sample CR must exist and be readable")

		kn := &kubernautv1alpha2.Kubernaut{}
		Expect(yaml.Unmarshal(data, kn)).To(Succeed(), "quickstart minimal sample CR must be valid YAML")

		// Retarget name/namespace to this suite's shared test namespace so
		// this spec can't collide with the "kubernaut" singleton other
		// specs in this package create/delete in the same namespace.
		kn.Name = "quickstart-minimal-sample"
		kn.Namespace = testNamespace

		Expect(k8sClient.Create(ctx, kn)).To(Succeed(),
			"the quickstart doc's minimal sample must be accepted as-is by the live CRD schema")

		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, kn)
		})
	})
})

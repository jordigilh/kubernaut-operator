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

package v1alpha2

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Issue #546: AI Analysis CRD parity", func() {
	It("exposes lowConfidenceFloor as an optional AI Analysis field", func() {
		crd := loadKubernautCRD()
		spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
		aiAnalysis := spec.Properties["aiAnalysis"]

		floor, ok := aiAnalysis.Properties["lowConfidenceFloor"]
		Expect(ok).To(BeTrue())
		Expect(floor.Type).To(Equal("string"))
		Expect(aiAnalysis.Required).NotTo(ContainElement("lowConfidenceFloor"))
	})
})

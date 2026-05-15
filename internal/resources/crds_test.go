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
	"fmt"
	"io/fs"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/jordigilh/kubernaut/pkg/shared/assets"
)

var _ = Describe("EnsureCRDs embedded assets", func() {
	It("parses each embedded CRD YAML", func() {
		entries, err := fs.ReadDir(assets.CRDsFS, "crds")
		Expect(err).NotTo(HaveOccurred())

		var errs []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := fs.ReadFile(assets.CRDsFS, "crds/"+entry.Name())
			if err != nil {
				errs = append(errs, fmt.Sprintf("reading %s: %v", entry.Name(), err))
				continue
			}

			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := sigsyaml.Unmarshal(data, crd); err != nil {
				errs = append(errs, fmt.Sprintf("CRD %s fails to unmarshal: %v", entry.Name(), err))
				continue
			}

			if crd.Name == "" {
				errs = append(errs, fmt.Sprintf("CRD %s has no metadata.name", entry.Name()))
			}
			if crd.Spec.Group == "" {
				errs = append(errs, fmt.Sprintf("CRD %s has no spec.group", entry.Name()))
			}
		}
		Expect(errs).To(BeEmpty())
	})

	It("preserves fields through yamlToUnstructured JSON round-trip", func() {
		entries, err := fs.ReadDir(assets.CRDsFS, "crds")
		Expect(err).NotTo(HaveOccurred())

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := fs.ReadFile(assets.CRDsFS, "crds/"+entry.Name())
			Expect(err).NotTo(HaveOccurred(), "reading %s", entry.Name())
			raw := string(data)

			obj, err := yamlToUnstructured(data)
			Expect(err).NotTo(HaveOccurred(), "yamlToUnstructured(%s)", entry.Name())

			roundTripped, err := obj.MarshalJSON()
			Expect(err).NotTo(HaveOccurred(), "re-marshalling %s", entry.Name())

			if strings.Contains(raw, "serviceAccountName") {
				Expect(string(roundTripped)).To(ContainSubstring("serviceAccountName"), "%s: raw YAML has serviceAccountName but yamlToUnstructured round-trip lost it", entry.Name())
			}

			Expect(obj.GetName()).NotTo(BeEmpty(), "%s has no metadata.name after yamlToUnstructured", entry.Name())
		}
	})

	It("embeds at least 9 CRD files", func() {
		entries, err := fs.ReadDir(assets.CRDsFS, "crds")
		Expect(err).NotTo(HaveOccurred())
		count := 0
		for _, e := range entries {
			if !e.IsDir() {
				count++
			}
		}
		Expect(count).To(BeNumerically(">=", 9))
	})
})

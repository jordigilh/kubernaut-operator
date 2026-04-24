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
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/jordigilh/kubernaut/pkg/shared/assets"
)

func TestEnsureCRDs_EmbeddedYAMLParses(t *testing.T) {
	entries, err := fs.ReadDir(assets.CRDsFS, "crds")
	if err != nil {
		t.Fatalf("reading embedded CRDs: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(assets.CRDsFS, "crds/"+entry.Name())
		if err != nil {
			t.Errorf("reading %s: %v", entry.Name(), err)
			continue
		}

		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := sigsyaml.Unmarshal(data, crd); err != nil {
			t.Errorf("CRD %s fails to unmarshal: %v", entry.Name(), err)
			continue
		}

		if crd.Name == "" {
			t.Errorf("CRD %s has no metadata.name", entry.Name())
		}
		if crd.Spec.Group == "" {
			t.Errorf("CRD %s has no spec.group", entry.Name())
		}
	}
}

func TestEnsureCRDs_UnstructuredRoundTrip_PreservesAllFields(t *testing.T) {
	entries, err := fs.ReadDir(assets.CRDsFS, "crds")
	if err != nil {
		t.Fatalf("reading embedded CRDs: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(assets.CRDsFS, "crds/"+entry.Name())
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}
		raw := string(data)

		jsonBytes, err := sigsyaml.YAMLToJSON(data)
		if err != nil {
			t.Fatalf("converting %s to JSON: %v", entry.Name(), err)
		}

		obj := &unstructured.Unstructured{}
		if err := json.Unmarshal(jsonBytes, &obj.Object); err != nil {
			t.Fatalf("unmarshalling %s to unstructured: %v", entry.Name(), err)
		}

		roundTripped, err := json.Marshal(obj.Object)
		if err != nil {
			t.Fatalf("re-marshalling %s: %v", entry.Name(), err)
		}

		if strings.Contains(raw, "serviceAccountName") {
			if !strings.Contains(string(roundTripped), "serviceAccountName") {
				t.Errorf("%s: raw YAML has serviceAccountName but unstructured round-trip lost it", entry.Name())
			}
		}

		if obj.GetName() == "" {
			t.Errorf("%s has no metadata.name after unstructured parse", entry.Name())
		}
	}
}

func TestEnsureCRDs_EmbeddedCRDCount(t *testing.T) {
	entries, err := fs.ReadDir(assets.CRDsFS, "crds")
	if err != nil {
		t.Fatalf("reading embedded CRDs: %v", err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	if count < 9 {
		t.Errorf("expected at least 9 embedded CRD files, got %d", count)
	}
}

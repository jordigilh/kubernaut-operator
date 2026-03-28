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
	"context"
	"io/fs"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/jordigilh/kubernaut/pkg/shared/assets"
)

func TestEnsureCRDs_CreatesAllCRDs(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register apiextensions scheme: %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	if err := EnsureCRDs(ctx, c); err != nil {
		t.Fatalf("EnsureCRDs() first call error: %v", err)
	}

	entries, err := fs.ReadDir(assets.CRDsFS, "crds")
	if err != nil {
		t.Fatalf("reading embedded CRDs: %v", err)
	}

	crdCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		crdCount++
		data, err := fs.ReadFile(assets.CRDsFS, "crds/"+entry.Name())
		if err != nil {
			t.Fatalf("reading CRD %s: %v", entry.Name(), err)
		}

		expected := &apiextensionsv1.CustomResourceDefinition{}
		if err := sigsyaml.Unmarshal(data, expected); err != nil {
			t.Fatalf("unmarshalling CRD %s: %v", entry.Name(), err)
		}

		actual := &apiextensionsv1.CustomResourceDefinition{}
		if err := c.Get(ctx, types.NamespacedName{Name: expected.Name}, actual); err != nil {
			t.Errorf("CRD %q should exist after EnsureCRDs: %v", expected.Name, err)
		}
	}

	if crdCount == 0 {
		t.Error("embedded CRDs directory should contain at least one CRD file")
	}
}

func TestEnsureCRDs_UpdatesExistingCRD(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register apiextensions scheme: %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	if err := EnsureCRDs(ctx, c); err != nil {
		t.Fatalf("EnsureCRDs() first call error: %v", err)
	}

	// Second call should succeed (update path).
	if err := EnsureCRDs(ctx, c); err != nil {
		t.Fatalf("EnsureCRDs() second call (update) error: %v", err)
	}
}

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

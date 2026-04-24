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
	"encoding/json"
	"fmt"
	"io/fs"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/jordigilh/kubernaut/pkg/shared/assets"
)

const crdFieldManager = "kubernaut-operator"

// EnsureCRDs reads the embedded CRD YAMLs from the shared assets package
// and applies them to the cluster using Server-Side Apply. SSA preserves the
// full OpenAPI schema (including deeply nested properties like
// serviceAccountName) that can be lost during Go type round-tripping with
// typed CRD structs.
func EnsureCRDs(ctx context.Context, c client.Client) error {
	entries, err := fs.ReadDir(assets.CRDsFS, "crds")
	if err != nil {
		return fmt.Errorf("reading embedded CRD directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := fs.ReadFile(assets.CRDsFS, "crds/"+entry.Name())
		if err != nil {
			return fmt.Errorf("reading embedded CRD %s: %w", entry.Name(), err)
		}

		obj := &unstructured.Unstructured{}
		jsonBytes, err := yaml.YAMLToJSON(data)
		if err != nil {
			return fmt.Errorf("converting CRD %s YAML to JSON: %w", entry.Name(), err)
		}
		if err := json.Unmarshal(jsonBytes, &obj.Object); err != nil {
			return fmt.Errorf("unmarshalling CRD %s: %w", entry.Name(), err)
		}

		obj.SetManagedFields(nil)
		if err := c.Patch(ctx, obj, client.Apply, client.FieldOwner(crdFieldManager), client.ForceOwnership); err != nil {
			return fmt.Errorf("applying CRD %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// CRDName returns the cluster-scoped name for a given embedded CRD file,
// useful for testing.
func CRDName(ctx context.Context, c client.Client, name string) (types.NamespacedName, error) {
	data, err := fs.ReadFile(assets.CRDsFS, "crds/"+name)
	if err != nil {
		return types.NamespacedName{}, err
	}
	obj := &unstructured.Unstructured{}
	jsonBytes, _ := yaml.YAMLToJSON(data)
	if err := json.Unmarshal(jsonBytes, &obj.Object); err != nil {
		return types.NamespacedName{}, err
	}
	return types.NamespacedName{Name: obj.GetName()}, nil
}

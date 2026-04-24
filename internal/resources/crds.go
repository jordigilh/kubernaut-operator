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
	"fmt"
	"io/fs"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"

	"github.com/jordigilh/kubernaut/pkg/shared/assets"
)

var crdGVR = schema.GroupVersionResource{
	Group:    "apiextensions.k8s.io",
	Version:  "v1",
	Resource: "customresourcedefinitions",
}

// EnsureCRDs reads the embedded CRD YAMLs from the shared assets package
// and applies them to the cluster using the dynamic client.
//
// We bypass the controller-runtime typed client because it registers
// apiextensionsv1 in its scheme, causing automatic conversion from
// unstructured to typed Go structs. That round-trip silently drops deeply
// nested JSONSchemaProps properties (e.g. serviceAccountName under
// execution.properties). The dynamic client sends the raw JSON as-is.
func EnsureCRDs(ctx context.Context, cfg *rest.Config) error {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating dynamic client for CRDs: %w", err)
	}
	crdClient := dyn.Resource(crdGVR)

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

		desired, err := yamlToUnstructured(data)
		if err != nil {
			return fmt.Errorf("parsing CRD %s: %w", entry.Name(), err)
		}

		existing, err := crdClient.Get(ctx, desired.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			if _, createErr := crdClient.Create(ctx, desired, metav1.CreateOptions{}); createErr != nil {
				return fmt.Errorf("creating CRD %s: %w", desired.GetName(), createErr)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("getting CRD %s: %w", desired.GetName(), err)
		}

		desired.SetResourceVersion(existing.GetResourceVersion())
		if _, err := crdClient.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating CRD %s: %w", desired.GetName(), err)
		}
	}

	return nil
}

func yamlToUnstructured(data []byte) (*unstructured.Unstructured, error) {
	jsonBytes, err := yaml.YAMLToJSON(data)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(jsonBytes); err != nil {
		return nil, err
	}
	return obj, nil
}

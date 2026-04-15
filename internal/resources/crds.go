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

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	"github.com/jordigilh/kubernaut/pkg/shared/assets"
)

// EnsureCRDs reads the embedded CRD YAMLs from the shared assets package
// and applies them to the cluster. Uses CreateOrUpdate with a DeepEqual
// guard to skip no-op writes and avoid unnecessary API server load.
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

		desired := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(data, desired); err != nil {
			return fmt.Errorf("unmarshalling CRD %s: %w", entry.Name(), err)
		}

		existing := &apiextensionsv1.CustomResourceDefinition{}
		existing.Name = desired.Name
		_, err = controllerutil.CreateOrUpdate(ctx, c, existing, func() error {
			specChanged := !equality.Semantic.DeepEqual(existing.Spec, desired.Spec)
			labelsChanged := !equality.Semantic.DeepEqual(existing.Labels, desired.Labels)
			annotationsChanged := !equality.Semantic.DeepEqual(existing.Annotations, desired.Annotations)
			if !specChanged && !labelsChanged && !annotationsChanged {
				return nil
			}
			existing.Spec = desired.Spec
			existing.Labels = desired.Labels
			existing.Annotations = desired.Annotations
			return nil
		})
		if err != nil {
			return fmt.Errorf("ensuring CRD %s: %w", desired.Name, err)
		}
	}

	return nil
}

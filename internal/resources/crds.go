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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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

		if err := ensureSharedCRD(ctx, crdClient, desired); err != nil {
			logf.FromContext(ctx).Error(err, "operand CRD reconciliation failed", "kind", desired.GetKind(), "namespace", "", "name", desired.GetName())
			return err
		}
	}

	return nil
}

// ensureSharedCRD applies the shared-schema exception from
// docs/design/ISSUE-514-OWNERSHIP-CONTRACT.md. Instance owner references are
// invalid for these cluster-wide schemas; explicit operator marking is required.
func ensureSharedCRD(ctx context.Context, api dynamic.ResourceInterface, desired *unstructured.Unstructured) error {
	labels := desired.GetLabels()
	if labels == nil {
		labels = make(map[string]string, 1)
	}
	labels["app.kubernetes.io/managed-by"] = "kubernaut-operator"
	desired.SetLabels(labels)
	annotations := desired.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	annotations[AnnotationSpecHash] = SpecHash(desired)
	desired.SetAnnotations(annotations)

	live, err := api.Get(ctx, desired.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, createErr := api.Create(ctx, desired, metav1.CreateOptions{}); createErr == nil {
			logf.FromContext(ctx).Info("shared operand CRD created", "kind", desired.GetKind(), "namespace", "", "name", desired.GetName())
			return nil
		} else if !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("creating CRD %s: %w", desired.GetName(), createErr)
		}
		live, err = api.Get(ctx, desired.GetName(), metav1.GetOptions{})
	}
	if err != nil {
		return fmt.Errorf("getting CRD %s: %w", desired.GetName(), err)
	}
	if live.GetLabels()["app.kubernetes.io/managed-by"] != "kubernaut-operator" || len(live.GetOwnerReferences()) != 0 || !live.GetDeletionTimestamp().IsZero() {
		return fmt.Errorf("%w for CustomResourceDefinition %q: existing manager=%q owners=%v; review schema compatibility and explicitly transfer shared CRD ownership before upgrade", ErrOwnershipConflict, desired.GetName(), live.GetLabels()["app.kubernetes.io/managed-by"], live.GetOwnerReferences())
	}
	if live.GetAnnotations()[AnnotationSpecHash] == desired.GetAnnotations()[AnnotationSpecHash] {
		return nil
	}
	for key, value := range live.GetLabels() {
		labels[key] = value
	}
	desired.SetLabels(labels)
	for key, value := range live.GetAnnotations() {
		if key != AnnotationSpecHash {
			annotations[key] = value
		}
	}
	desired.SetAnnotations(annotations)
	desired.SetResourceVersion(live.GetResourceVersion())
	if _, err := api.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating CRD %s: %w", desired.GetName(), err)
	}
	logf.FromContext(ctx).Info("shared operand CRD updated", "kind", desired.GetKind(), "namespace", "", "name", desired.GetName())
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

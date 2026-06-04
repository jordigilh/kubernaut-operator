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

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	spireAPIGroup   = "spire.spiffe.io"
	spireAPIVersion = "v1alpha1"
)

// ClusterSPIFFEID builds an unstructured ClusterSPIFFEID resource that
// registers a SPIFFE identity for the apifrontend ServiceAccount. Returns
// nil when SPIRE is not enabled in the CR.
func ClusterSPIFFEID(kn *kubernautv1alpha1.Kubernaut) (*unstructured.Unstructured, error) {
	if !kn.Spec.APIFrontend.SPIRE.SPIREEnabled() {
		return nil, nil
	}

	ns := kn.Namespace
	saName := ComponentAPIFrontend

	spiffeID := fmt.Sprintf("spiffe://kubernaut/%s/%s", ns, saName)

	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(spireAPIGroup + "/" + spireAPIVersion)
	obj.SetKind("ClusterSPIFFEID")
	obj.SetName("kubernaut-apifrontend")
	obj.SetLabels(ComponentLabels(kn, ComponentAPIFrontend))

	spec := map[string]interface{}{
		"spiffeIDTemplate": spiffeID,
		"podSelector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"app.kubernetes.io/component": ComponentAPIFrontend,
			},
		},
		"namespaceSelector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"kubernetes.io/metadata.name": ns,
			},
		},
	}

	if kn.Spec.APIFrontend.SPIRE.ClassName != "" {
		spec["className"] = kn.Spec.APIFrontend.SPIRE.ClassName
	}

	if err := unstructured.SetNestedMap(obj.Object, spec, "spec"); err != nil {
		return nil, fmt.Errorf("setting ClusterSPIFFEID spec: %w", err)
	}
	return obj, nil
}

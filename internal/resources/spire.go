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

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	spireAPIGroup       = "spire.spiffe.io"
	spireAPIVersion     = "v1alpha1"
	clusterSPIFFEIDName = "kubernaut-apifrontend"
)

// ClusterSPIFFEIDReference returns the identity and name used by the
// operator-owned API Frontend ClusterSPIFFEID. It is used only to remove the
// exact registration when SPIRE is disabled or the Kubernaut resource is
// deleted; provider-owned SPIRE infrastructure is never selected here.
func ClusterSPIFFEIDReference() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(spireAPIGroup + "/" + spireAPIVersion)
	obj.SetKind("ClusterSPIFFEID")
	obj.SetName(clusterSPIFFEIDName)
	return obj
}

// AFSpiffeID returns the resolved SPIFFE ID for the API Frontend ServiceAccount.
// It is useful to qualification tooling that needs the expected concrete
// identity; ClusterSPIFFEID itself keeps SPIRE's trust-domain template by
// default so the provider remains authoritative.
func AFSpiffeID(kn *kubernautv1alpha2.Kubernaut) string {
	td := "localtest.me"
	if kn.Spec.APIFrontend.SPIRE.TrustDomain != "" {
		td = kn.Spec.APIFrontend.SPIRE.TrustDomain
	}
	return fmt.Sprintf("spiffe://%s/ns/%s/sa/%s", td, kn.Namespace, ComponentAPIFrontend)
}

// ClusterSPIFFEID builds an unstructured ClusterSPIFFEID resource that
// registers a SPIFFE identity for the apifrontend ServiceAccount. Returns
// nil when SPIRE is not enabled in the CR.
//
// The spiffeIDTemplate uses SPIRE's {{ .TrustDomain }} variable by default so
// the identity matches whatever trust domain the cluster's SPIRE server is
// configured with (FedRAMP SC-8, IA-5). The path follows the standard
// /ns/{namespace}/sa/{serviceaccount} convention.
func ClusterSPIFFEID(kn *kubernautv1alpha2.Kubernaut) (*unstructured.Unstructured, error) {
	if !kn.Spec.APIFrontend.SPIRE.SPIREEnabled() {
		// SPIRE disabled: no identity to register, not an error. Callers
		// guard on a nil result before use.
		return nil, nil //nolint:nilnil
	}

	ns := kn.Namespace
	saName := ComponentAPIFrontend

	td := "{{ .TrustDomain }}"
	if kn.Spec.APIFrontend.SPIRE.TrustDomain != "" {
		td = kn.Spec.APIFrontend.SPIRE.TrustDomain
	}
	spiffeID := fmt.Sprintf("spiffe://%s/ns/%s/sa/%s", td, ns, saName)

	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(spireAPIGroup + "/" + spireAPIVersion)
	obj.SetKind("ClusterSPIFFEID")
	obj.SetName(clusterSPIFFEIDName)
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

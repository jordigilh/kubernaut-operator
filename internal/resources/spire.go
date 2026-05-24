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
	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	spireAPIGroup   = "spire.spiffe.io"
	spireAPIVersion = "v1alpha1"

	clusterSPIFFEIDName = "kubernaut-apifrontend"
	spiffeIDTemplate    = "spiffe://{{ .TrustDomain }}/ns/{{ .PodMeta.Namespace }}/sa/{{ .PodSpec.ServiceAccountName }}"

	SPIRECSIDriver    = "csi.spiffe.io"
	SPIREVolumeName   = "spiffe-workload-api"
	SPIREMountPath    = "/spiffe-workload-api"
	SPIRESidecarPort  = int32(9443)
	SPIREWorkloadAddr = "unix://" + SPIREMountPath + "/spire-agent.sock"
)

// ClusterSPIFFEID builds an unstructured ClusterSPIFFEID CR that registers
// the AF pods with SPIRE for SVID issuance. Returns nil when SPIRE or AF
// is not enabled.
func ClusterSPIFFEID(kn *kubernautv1alpha1.Kubernaut) *unstructured.Unstructured {
	if !kn.Spec.APIFrontendEnabled() || !kn.Spec.APIFrontend.SPIRE.SPIREEnabled() {
		return nil
	}

	csid := &unstructured.Unstructured{}
	csid.SetAPIVersion(spireAPIGroup + "/" + spireAPIVersion)
	csid.SetKind("ClusterSPIFFEID")
	csid.SetName(clusterSPIFFEIDName)
	csid.SetLabels(ComponentLabels(kn, ComponentAPIFrontend))

	spec := map[string]interface{}{
		"spiffeIDTemplate": spiffeIDTemplate,
		"podSelector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"app": ComponentAPIFrontend,
			},
		},
		"namespaceSelector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"kubernetes.io/metadata.name": kn.Namespace,
			},
		},
	}

	if kn.Spec.APIFrontend.SPIRE.ClassName != "" {
		spec["className"] = kn.Spec.APIFrontend.SPIRE.ClassName
	}

	_ = unstructured.SetNestedMap(csid.Object, spec, "spec")
	return csid
}

// ClusterSPIFFEIDStub returns a minimal ClusterSPIFFEID for deletion lookup.
func ClusterSPIFFEIDStub(kn *kubernautv1alpha1.Kubernaut) *unstructured.Unstructured {
	csid := &unstructured.Unstructured{}
	csid.SetAPIVersion(spireAPIGroup + "/" + spireAPIVersion)
	csid.SetKind("ClusterSPIFFEID")
	csid.SetName(clusterSPIFFEIDName)
	return csid
}

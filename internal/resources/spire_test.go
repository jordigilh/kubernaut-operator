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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ClusterSPIFFEID", func() {
	It("returns nil when SPIRE is not enabled", func() {
		kn := testKubernautWithAF()
		csid := ClusterSPIFFEID(kn)

		Expect(csid).To(BeNil(), "ClusterSPIFFEID should be nil when SPIRE is disabled")
	})

	It("returns nil when AF is not enabled", func() {
		kn := testKubernaut()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		csid := ClusterSPIFFEID(kn)

		Expect(csid).To(BeNil(), "ClusterSPIFFEID should be nil when AF is disabled")
	})

	It("SC-8: sets correct GVK for SPIRE ClusterSPIFFEID", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		csid := ClusterSPIFFEID(kn)

		Expect(csid).NotTo(BeNil())
		Expect(csid.GetAPIVersion()).To(Equal("spire.spiffe.io/v1alpha1"))
		Expect(csid.GetKind()).To(Equal("ClusterSPIFFEID"))
	})

	It("SC-8: is cluster-scoped with deterministic name", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		csid := ClusterSPIFFEID(kn)

		Expect(csid).NotTo(BeNil())
		Expect(csid.GetName()).To(Equal("kubernaut-apifrontend"))
		Expect(csid.GetNamespace()).To(BeEmpty(), "ClusterSPIFFEID must be cluster-scoped")
	})

	It("IA-5: uses SPIFFE ID template with trust domain, namespace, and SA", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		csid := ClusterSPIFFEID(kn)

		Expect(csid).NotTo(BeNil())
		template, found, err := unstructuredNestedString(csid.Object, "spec", "spiffeIDTemplate")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(template).To(Equal("spiffe://{{ .TrustDomain }}/ns/{{ .PodMeta.Namespace }}/sa/{{ .PodSpec.ServiceAccountName }}"))
	})

	It("selects AF pods by app label", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		csid := ClusterSPIFFEID(kn)

		Expect(csid).NotTo(BeNil())
		podSelector, found, err := unstructuredNestedStringMap(csid.Object, "spec", "podSelector", "matchLabels")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(podSelector).To(HaveKeyWithValue("app", ComponentAPIFrontend))
	})

	It("selects the CR namespace", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		csid := ClusterSPIFFEID(kn)

		Expect(csid).NotTo(BeNil())
		nsSelector, found, err := unstructuredNestedStringMap(csid.Object, "spec", "namespaceSelector", "matchLabels")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(nsSelector).To(HaveKeyWithValue("kubernetes.io/metadata.name", kn.Namespace))
	})

	It("sets className when configured", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		kn.Spec.APIFrontend.SPIRE.ClassName = "zero-trust-workload-identity-manager-spire"
		csid := ClusterSPIFFEID(kn)

		Expect(csid).NotTo(BeNil())
		className, found, err := unstructuredNestedString(csid.Object, "spec", "className")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(className).To(Equal("zero-trust-workload-identity-manager-spire"))
	})

	It("omits className when not configured", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		csid := ClusterSPIFFEID(kn)

		Expect(csid).NotTo(BeNil())
		_, found, _ := unstructuredNestedString(csid.Object, "spec", "className")
		Expect(found).To(BeFalse(), "className should be absent when not configured")
	})

	It("carries apifrontend component labels", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		csid := ClusterSPIFFEID(kn)

		Expect(csid).NotTo(BeNil())
		labels := csid.GetLabels()
		Expect(labels).To(HaveKeyWithValue("app", ComponentAPIFrontend))
		Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "kubernaut-operator"))
	})
})

var _ = Describe("ClusterSPIFFEIDStub", func() {
	It("has correct GVK and name for deletion lookup", func() {
		kn := testKubernaut()
		stub := ClusterSPIFFEIDStub(kn)

		Expect(stub.GetAPIVersion()).To(Equal("spire.spiffe.io/v1alpha1"))
		Expect(stub.GetKind()).To(Equal("ClusterSPIFFEID"))
		Expect(stub.GetName()).To(Equal("kubernaut-apifrontend"))
		Expect(stub.GetNamespace()).To(BeEmpty())
	})
})

func unstructuredNestedString(obj map[string]interface{}, fields ...string) (string, bool, error) {
	val, found := unstructuredNestedField(obj, fields...)
	if !found {
		return "", false, nil
	}
	s, ok := val.(string)
	if !ok {
		return "", true, nil
	}
	return s, true, nil
}

func unstructuredNestedStringMap(obj map[string]interface{}, fields ...string) (map[string]string, bool, error) {
	val, found := unstructuredNestedField(obj, fields...)
	if !found {
		return nil, false, nil
	}
	m, ok := val.(map[string]interface{})
	if !ok {
		return nil, true, nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result, true, nil
}

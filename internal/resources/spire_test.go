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

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func nestedStr(obj *unstructured.Unstructured, fields ...string) (string, bool, error) {
	return unstructured.NestedString(obj.Object, fields...)
}

func nestedStrMap(obj *unstructured.Unstructured, fields ...string) (map[string]string, bool, error) {
	return unstructured.NestedStringMap(obj.Object, fields...)
}

var _ = Describe("ClusterSPIFFEID", func() {
	It("returns nil when SPIRE is disabled", func() {
		kn := testKubernaut()
		kn.Spec.APIFrontend.SPIRE.Enabled = false
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(obj).To(BeNil())
	})

	It("SC-8: uses SPIRE TrustDomain template variable by default", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(obj).NotTo(BeNil())

		tmpl, found, err := nestedStr(obj, "spec", "spiffeIDTemplate")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(tmpl).To(Equal("spiffe://{{ .TrustDomain }}/ns/kubernaut-system/sa/apifrontend"),
			"SC-8: must use SPIRE {{ .TrustDomain }} so the identity matches the cluster's trust domain")
	})

	It("SC-8: uses standard /ns/{ns}/sa/{sa} SPIFFE path format", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())

		tmpl, _, _ := nestedStr(obj, "spec", "spiffeIDTemplate")
		Expect(tmpl).To(ContainSubstring("/ns/"))
		Expect(tmpl).To(ContainSubstring("/sa/"))
	})

	It("uses literal trust domain when spec.apiFrontend.spire.trustDomain is set", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		kn.Spec.APIFrontend.SPIRE.TrustDomain = "custom.example.com"
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())

		tmpl, _, _ := nestedStr(obj, "spec", "spiffeIDTemplate")
		Expect(tmpl).To(Equal("spiffe://custom.example.com/ns/kubernaut-system/sa/apifrontend"),
			"trustDomain override must replace {{ .TrustDomain }} with the literal value")
	})

	It("sets podSelector for apifrontend component", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())

		labels, found, err := nestedStrMap(obj, "spec", "podSelector", "matchLabels")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/component", "apifrontend"))
	})

	It("sets namespaceSelector to CR namespace", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())

		labels, found, err := nestedStrMap(obj, "spec", "namespaceSelector", "matchLabels")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(labels).To(HaveKeyWithValue("kubernetes.io/metadata.name", "kubernaut-system"))
	})

	It("includes className when set in CR", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		kn.Spec.APIFrontend.SPIRE.ClassName = "zero-trust-workload-identity-manager-spire"
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())

		cn, found, err := nestedStr(obj, "spec", "className")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(cn).To(Equal("zero-trust-workload-identity-manager-spire"))
	})

	It("omits className when not set in CR", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		kn.Spec.APIFrontend.SPIRE.ClassName = ""
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())

		_, found, _ := nestedStr(obj, "spec", "className")
		Expect(found).To(BeFalse(), "className should be omitted when not set")
	})

	It("sets correct metadata", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())

		Expect(obj.GetName()).To(Equal("kubernaut-apifrontend"))
		Expect(obj.GetAPIVersion()).To(Equal("spire.spiffe.io/v1alpha1"))
		Expect(obj.GetKind()).To(Equal("ClusterSPIFFEID"))
		Expect(obj.GetLabels()).To(HaveKeyWithValue("app.kubernetes.io/component", "apifrontend"))
	})

	It("uses CR namespace in SPIFFE path, not a template variable", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.SPIRE.Enabled = true
		kn.ObjectMeta.Namespace = "custom-namespace"
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())

		tmpl, _, _ := nestedStr(obj, "spec", "spiffeIDTemplate")
		Expect(tmpl).To(ContainSubstring("/ns/custom-namespace/sa/apifrontend"))

		nsLabels, _, _ := nestedStrMap(obj, "spec", "namespaceSelector", "matchLabels")
		Expect(nsLabels).To(HaveKeyWithValue("kubernetes.io/metadata.name", "custom-namespace"))
	})

	It("IA-5: returns nil when SPIRE spec is explicitly disabled", func() {
		kn := testKubernaut()
		kn.Spec.APIFrontend.SPIRE = kubernautv1alpha1.APIFrontendSPIRESpec{Enabled: false}
		obj, err := ClusterSPIFFEID(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(obj).To(BeNil())
	})
})

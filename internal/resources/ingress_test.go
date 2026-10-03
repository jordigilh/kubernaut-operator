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

	networkingv1 "k8s.io/api/networking/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("generic Ingress resources", func() {
	It("renders an explicit TLS Ingress for every enabled component", func() {
		kn := testKubernaut()
		kn.Spec.Gateway.Ingress = kubernautv1alpha2.IngressSpec{
			Enabled:          boolPtr(true),
			IngressClassName: "nginx",
			Host:             "gateway.example.test",
			TLSSecretName:    "gateway-ingress-tls",
			Annotations:      map[string]string{"nginx.ingress.kubernetes.io/proxy-read-timeout": "3600"},
		}
		enabled := true
		kn.Spec.APIFrontend.Enabled = &enabled
		kn.Spec.APIFrontend.Ingress = kubernautv1alpha2.IngressSpec{
			Enabled:          boolPtr(true),
			IngressClassName: "nginx",
			Host:             "api.example.test",
			TLSSecretName:    "api-ingress-tls",
		}
		kn.Spec.Console.Enabled = &enabled
		kn.Spec.Console.Ingress = kubernautv1alpha2.IngressSpec{
			Enabled:          boolPtr(true),
			IngressClassName: "nginx",
			Host:             "console.example.test",
			TLSSecretName:    "console-ingress-tls",
		}

		objects, err := Ingresses(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(HaveLen(3))

		byName := make(map[string]*networkingv1.Ingress, len(objects))
		for _, object := range objects {
			byName[object.Name] = object
			Expect(object.Spec.IngressClassName).NotTo(BeNil())
			Expect(*object.Spec.IngressClassName).To(Equal("nginx"))
			Expect(object.Spec.TLS).To(HaveLen(1))
			Expect(object.Spec.TLS[0].SecretName).NotTo(BeEmpty())
		}

		Expect(byName["gateway-ingress"].Spec.Rules[0].Host).To(Equal("gateway.example.test"))
		Expect(byName["gateway-ingress"].Spec.Rules[0].IngressRuleValue.HTTP.Paths[0].Backend.Service.Name).To(Equal("gateway-service"))
		Expect(byName["gateway-ingress"].Spec.Rules[0].IngressRuleValue.HTTP.Paths[0].Backend.Service.Port.Name).To(Equal("https"))
		Expect(byName["gateway-ingress"].Annotations).To(HaveKeyWithValue("nginx.ingress.kubernetes.io/proxy-read-timeout", "3600"))
		Expect(byName["apifrontend-ingress"].Spec.Rules[0].IngressRuleValue.HTTP.Paths[0].Backend.Service.Name).To(Equal("apifrontend"))
		Expect(byName["console-ingress"].Spec.Rules[0].IngressRuleValue.HTTP.Paths[0].Backend.Service.Name).To(Equal("console"))
	})

	It("does not render an Ingress when exposure is not explicitly enabled", func() {
		objects, err := Ingresses(testKubernaut())
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).To(BeEmpty())
	})

	DescribeTable("rejects incomplete generic exposure", func(spec kubernautv1alpha2.IngressSpec, field string) {
		kn := testKubernaut()
		kn.Spec.Gateway.Ingress = spec
		_, err := Ingresses(kn)
		Expect(err).To(MatchError(ContainSubstring(field)))
	},
		Entry("missing class", kubernautv1alpha2.IngressSpec{Enabled: boolPtr(true), Host: "gateway.example.test", TLSSecretName: "tls"}, "ingressClassName"),
		Entry("missing host", kubernautv1alpha2.IngressSpec{Enabled: boolPtr(true), IngressClassName: "nginx", TLSSecretName: "tls"}, "host"),
		Entry("missing TLS Secret", kubernautv1alpha2.IngressSpec{Enabled: boolPtr(true), IngressClassName: "nginx", Host: "gateway.example.test"}, "tlsSecretName"),
	)
})

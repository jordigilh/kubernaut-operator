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

var _ = Describe("MCPGatewayHTTPRoute", func() {
	It("returns nil when AF is disabled", func() {
		kn := testKubernaut()
		Expect(MCPGatewayHTTPRoute(kn)).To(BeNil())
	})

	It("returns an HTTPRoute when AF is enabled", func() {
		kn := testKubernautWithAF()
		route := MCPGatewayHTTPRoute(kn)
		Expect(route).NotTo(BeNil())
		Expect(route.GetKind()).To(Equal("HTTPRoute"))
		Expect(route.GetName()).To(Equal("apifrontend-mcp"))
		Expect(route.GetNamespace()).To(Equal(kn.Namespace))

		rules, found, err := unstructuredNestedSlice(route.Object, "spec", "rules")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(rules).To(HaveLen(1))
	})
})

var _ = Describe("MCPServerRegistration", func() {
	It("returns nil when AF is disabled", func() {
		kn := testKubernaut()
		Expect(MCPServerRegistration(kn)).To(BeNil())
	})

	It("returns an MCPServerRegistration when AF is enabled", func() {
		kn := testKubernautWithAF()
		reg := MCPServerRegistration(kn)
		Expect(reg).NotTo(BeNil())
		Expect(reg.GetKind()).To(Equal("MCPServerRegistration"))
		Expect(reg.GetName()).To(Equal("kubernaut-apifrontend"))
		Expect(reg.GetNamespace()).To(Equal(kn.Namespace))

		spec, found, err := unstructuredNestedMap(reg.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(spec["serverName"]).To(Equal("kubernaut-apifrontend"))
		Expect(spec["transport"]).To(Equal("streamable-http"))
		Expect(spec["endpointURL"]).To(ContainSubstring("/mcp"))
	})

	It("includes authentication when auth issuerURL is set", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = "https://login.kubernaut.ai/realms/kubernaut"
		reg := MCPServerRegistration(kn)
		Expect(reg).NotTo(BeNil())

		spec, _, _ := unstructuredNestedMap(reg.Object, "spec")
		auth, ok := spec["authentication"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(auth["type"]).To(Equal("bearer-jwt"))
		Expect(auth["issuerURL"]).To(Equal("https://login.kubernaut.ai/realms/kubernaut"))
	})
})

func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	val, found, err := unstructuredNestedField(obj, fields...)
	if err != nil || !found {
		return nil, found, err
	}
	s, ok := val.([]interface{})
	return s, ok, nil
}

func unstructuredNestedMap(obj map[string]interface{}, fields ...string) (map[string]interface{}, bool, error) {
	val, found, err := unstructuredNestedField(obj, fields...)
	if err != nil || !found {
		return nil, found, err
	}
	m, ok := val.(map[string]interface{})
	return m, ok, nil
}

func unstructuredNestedField(obj map[string]interface{}, fields ...string) (interface{}, bool, error) {
	var val interface{} = obj
	for _, f := range fields {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		val, ok = m[f]
		if !ok {
			return nil, false, nil
		}
	}
	return val, true, nil
}

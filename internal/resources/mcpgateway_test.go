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
	It("returns an HTTPRoute when AF is enabled", func() {
		kn := testKubernautWithAF()
		route, err := MCPGatewayHTTPRoute(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(route).NotTo(BeNil())
		Expect(route.GetKind()).To(Equal("HTTPRoute"))
		Expect(route.GetName()).To(Equal("apifrontend-mcp"))
		Expect(route.GetNamespace()).To(Equal(kn.Namespace))

		rules, found := unstructuredNestedSlice(route.Object, "spec", "rules")
		Expect(found).To(BeTrue())
		Expect(rules).To(HaveLen(1))
	})

	It("includes parentRefs for kagenti-gateway", func() {
		kn := testKubernautWithAF()
		route, err := MCPGatewayHTTPRoute(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(route).NotTo(BeNil())
		parentRefs, found := unstructuredNestedSlice(route.Object, "spec", "parentRefs")
		Expect(found).To(BeTrue(), "parentRefs should be set")
		Expect(parentRefs).To(HaveLen(1))
		ref := parentRefs[0].(map[string]interface{})
		Expect(ref["name"]).To(Equal("kagenti-gateway"))
	})
})

var _ = Describe("MCPServerRegistration", func() {
	It("returns an MCPServerRegistration when AF is enabled", func() {
		kn := testKubernautWithAF()
		reg, err := MCPServerRegistration(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(reg).NotTo(BeNil())
		Expect(reg.GetKind()).To(Equal("MCPServerRegistration"))
		Expect(reg.GetName()).To(Equal("kubernaut-apifrontend"))
		Expect(reg.GetNamespace()).To(Equal(kn.Namespace))

		spec, found := unstructuredNestedMap(reg.Object, "spec")
		Expect(found).To(BeTrue())
		Expect(spec["serverName"]).To(Equal("kubernaut-apifrontend"))
		Expect(spec["transport"]).To(Equal("streamable-http"))
		Expect(spec["endpointURL"]).To(ContainSubstring("/mcp"))
		Expect(spec["endpointURL"]).To(ContainSubstring("apifrontend."+kn.Namespace+".svc.cluster.local"),
			"endpointURL must use the Service name without a -service suffix")
	})

	It("includes authentication when auth issuerURL is set", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.Auth.IssuerURL = "https://login.kubernaut.ai/realms/kubernaut"
		reg, err := MCPServerRegistration(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(reg).NotTo(BeNil())

		spec, _ := unstructuredNestedMap(reg.Object, "spec")
		auth, ok := spec["authentication"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(auth["type"]).To(Equal("bearer-jwt"))
		Expect(auth["issuerURL"]).To(Equal("https://login.kubernaut.ai/realms/kubernaut"))
	})
})

func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool) {
	val, found := unstructuredNestedField(obj, fields...)
	if !found {
		return nil, false
	}
	s, ok := val.([]interface{})
	return s, ok
}

func unstructuredNestedMap(obj map[string]interface{}, fields ...string) (map[string]interface{}, bool) {
	val, found := unstructuredNestedField(obj, fields...)
	if !found {
		return nil, false
	}
	m, ok := val.(map[string]interface{})
	return m, ok
}

func unstructuredNestedField(obj map[string]interface{}, fields ...string) (interface{}, bool) {
	var val interface{} = obj
	for _, f := range fields {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, false
		}
		val, ok = m[f]
		if !ok {
			return nil, false
		}
	}
	return val, true
}

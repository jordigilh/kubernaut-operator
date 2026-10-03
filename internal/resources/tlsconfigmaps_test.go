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

var _ = Describe("generic TLS trust ConfigMaps", func() {
	It("publishes the same validated CA without OpenShift injection annotations", func() {
		kn := testKubernaut()
		configMaps := GenericTLSConfigMaps(kn, []byte("ca-pem"))
		Expect(configMaps).To(HaveLen(5))
		for _, configMap := range configMaps {
			Expect(configMap.Annotations).NotTo(HaveKey(OCPServiceCAInjectAnnotation), configMap.Name)
			Expect(configMap.Data).To(HaveKeyWithValue("service-ca.crt", "ca-pem"), configMap.Name)
		}
	})
})

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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

var _ = Describe("MergeTrustBundle", func() {
	It("concatenates both CAs when both are present", func() {
		merged := MergeTrustBundle("service-ca-pem", "router-ca-pem")
		Expect(merged).To(ContainSubstring("service-ca-pem"))
		Expect(merged).To(ContainSubstring("router-ca-pem"))
	})

	It("returns only the service CA when the router CA is empty (fail-open)", func() {
		merged := MergeTrustBundle("service-ca-pem", "")
		Expect(merged).To(Equal("service-ca-pem"))
	})

	It("returns only the router CA when the service CA is empty", func() {
		merged := MergeTrustBundle("", "router-ca-pem")
		Expect(merged).To(Equal("router-ca-pem"))
	})

	It("returns an empty string when both are empty, without erroring", func() {
		Expect(MergeTrustBundle("", "")).To(Equal(""))
	})

	It("ignores whitespace-only input the same as empty", func() {
		Expect(MergeTrustBundle("  \n  ", "router-ca-pem")).To(Equal("router-ca-pem"))
	})
})

var _ = Describe("TrustBundleConfigMap", func() {
	var kn *kubernautv1alpha1.Kubernaut

	BeforeEach(func() {
		kn = testKubernaut()
	})

	It("merges both reads into a single ConfigMap under the service-ca.crt key", func() {
		stubTrustBundleReadsForTest("service-ca-pem", nil, "router-ca-pem", nil)

		cm := TrustBundleConfigMap(kn)

		Expect(cm.Name).To(Equal(TrustBundleConfigMapName))
		Expect(cm.Namespace).To(Equal(kn.Namespace))
		Expect(cm.Data["service-ca.crt"]).To(And(ContainSubstring("service-ca-pem"), ContainSubstring("router-ca-pem")))
	})

	It("fails open to service-ca only when the router CA read fails (e.g. absent on this cluster)", func() {
		stubTrustBundleReadsForTest("service-ca-pem", nil, "", errors.New("configmaps \"default-ingress-cert\" not found"))

		cm := TrustBundleConfigMap(kn)

		Expect(cm.Data["service-ca.crt"]).To(Equal("service-ca-pem"))
	})

	It("fails open to router-ca only when the service-ca read fails (e.g. not yet injected)", func() {
		stubTrustBundleReadsForTest("", errors.New("configmaps \"inter-service-ca\" not found"), "router-ca-pem", nil)

		cm := TrustBundleConfigMap(kn)

		Expect(cm.Data["service-ca.crt"]).To(Equal("router-ca-pem"))
	})

	It("produces an empty (but present) bundle when both reads fail, never blocking reconciliation", func() {
		stubTrustBundleReadsForTest("", errors.New("transient"), "", errors.New("transient"))

		cm := TrustBundleConfigMap(kn)

		Expect(cm.Data["service-ca.crt"]).To(Equal(""))
	})

	It("does not request OCP service-ca injection -- content is entirely operator-computed", func() {
		stubTrustBundleReadsForTest("service-ca-pem", nil, "router-ca-pem", nil)

		cm := TrustBundleConfigMap(kn)

		Expect(cm.Annotations).NotTo(HaveKey(OCPServiceCAInjectAnnotation))
	})
})

// stubTrustBundleReadsForTest swaps both live-read seams for the duration of
// the current spec (restored via DeferCleanup), letting tests simulate the
// service-ca/default-ingress-cert reads' success/failure without a real
// in-cluster config.
func stubTrustBundleReadsForTest(serviceCA string, serviceCAErr error, routerCA string, routerCAErr error) {
	originalServiceCA := readServiceCAFunc
	originalRouterCA := readDefaultIngressCertFunc
	readServiceCAFunc = func(string) (string, error) { return serviceCA, serviceCAErr }
	readDefaultIngressCertFunc = func() (string, error) { return routerCA, routerCAErr }
	DeferCleanup(func() {
		readServiceCAFunc = originalServiceCA
		readDefaultIngressCertFunc = originalRouterCA
	})
}

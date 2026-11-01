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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("LLM profile TLS Secret validation (#532)", Label("unit"), func() {
	It("UT-532-CTRL-001 [SC-12, SI-10; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V12.3.2] validates and deduplicates a custom CA Secret key", func() {
		kn := newMinimalCR()
		profile := kn.Spec.LLMProfiles["primary"]
		profile.TLSCaFile = "/etc/kubernaut-agent/llm-tls-client/custom-ca.pem"
		profile.TLSCaSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "custom-ca", Key: "custom-ca.pem"}
		kn.Spec.LLMProfiles["primary"] = profile

		collector := newPrecreatedReferenceCollector()
		appendPrecreatedLLMReferences(collector, kn)
		appendPrecreatedLLMReferences(collector, kn)

		var caReference *precreatedReference
		for i := range collector.references {
			if collector.references[i].path == `spec.llmProfiles["primary"].tlsCaSecretRef` {
				caReference = &collector.references[i]
				break
			}
		}
		Expect(caReference).NotTo(BeNil())
		Expect(caReference.requiredKeys).To(Equal([]string{"custom-ca.pem"}))
		Expect(caReference.pemKeys).To(Equal([]string{"custom-ca.pem"}))

		r := businessUnitReconciler(kn, interceptor.Funcs{})
		caData := validPrecreatedCAData()["ca.crt"]
		seedPrecreatedObject(r, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "custom-ca", Namespace: kn.Namespace},
			Data:       map[string][]byte{"custom-ca.pem": caData},
		})
		Expect(r.validatePrecreatedReference(context.Background(), kn.Namespace, *caReference)).To(Succeed())

		badReference := *caReference
		badReference.name = "bad-custom-ca"
		seedPrecreatedObject(r, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: badReference.name, Namespace: kn.Namespace},
			Data:       map[string][]byte{"custom-ca.pem": []byte("not-a-certificate")},
		})
		Expect(r.validatePrecreatedReference(context.Background(), kn.Namespace, badReference)).To(MatchError(ContainSubstring("does not contain valid CA PEM")))

		defaultCR := newMinimalCR()
		defaultProfile := defaultCR.Spec.LLMProfiles["primary"]
		defaultProfile.TLSCaFile = "/etc/kubernaut-agent/llm-tls-client/ca.crt"
		defaultProfile.TLSCaSecretRef = &kubernautv1alpha2.CACertSecretRef{Name: "default-ca"}
		defaultCR.Spec.LLMProfiles["primary"] = defaultProfile
		defaultCollector := newPrecreatedReferenceCollector()
		appendPrecreatedLLMReferences(defaultCollector, defaultCR)
		var defaultReference *precreatedReference
		for i := range defaultCollector.references {
			if defaultCollector.references[i].path == `spec.llmProfiles["primary"].tlsCaSecretRef` {
				defaultReference = &defaultCollector.references[i]
				break
			}
		}
		Expect(defaultReference).NotTo(BeNil())
		seedPrecreatedObject(r, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "default-ca", Namespace: kn.Namespace},
			Data:       map[string][]byte{"ca.crt": caData},
		})
		Expect(r.validatePrecreatedReference(context.Background(), kn.Namespace, *defaultReference)).To(Succeed())
	})
})

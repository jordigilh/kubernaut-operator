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

package webhook

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// createRequestFor builds an admission.Request for a CREATE of the given
// Kubernaut CR, matching the shape the kube-apiserver sends in production.
func createRequestFor(kn *kubernautv1alpha1.Kubernaut) admission.Request {
	kn.TypeMeta = metav1.TypeMeta{APIVersion: "kubernaut.ai/v1alpha1", Kind: "Kubernaut"}
	raw, err := json.Marshal(kn)
	Expect(err).NotTo(HaveOccurred())
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

var _ = Describe("NewSingletonValidator wiring", func() {
	var scheme *runtime.Scheme

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(kubernautv1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	// This test constructs SingletonValidator exactly the way cmd/main.go's
	// registerSingletonWebhook does (via NewSingletonValidator) and drives it
	// through admission.Webhook.Handle -- the same entry point the real
	// webhook server invokes -- to prove the decoder is actually wired, not
	// just present as an unused struct field. A hand constructed
	// &SingletonValidator{Client: ...} literal with the decoder set directly
	// in the test would launder exactly the bug this guards against: the
	// production wiring path (cmd/main.go -> NewSingletonValidator) is what
	// must produce a working decoder, not the test itself.
	It("does not error decoding a real CREATE request", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		wh := &admission.Webhook{Handler: NewSingletonValidator(fakeClient, scheme)}

		req := createRequestFor(&kubernautv1alpha1.Kubernaut{
			ObjectMeta: metav1.ObjectMeta{Name: kubernautv1alpha1.SingletonName},
		})

		resp := wh.Handle(context.Background(), req)

		Expect(resp.Allowed).To(BeTrue(), "unexpected denial/error result: %+v", resp.Result)
		Expect(resp.Result).NotTo(BeNil())
		Expect(resp.Result.Code).To(Equal(int32(200)))
	})

	It("denies a CREATE request whose name is not the singleton name", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		wh := &admission.Webhook{Handler: NewSingletonValidator(fakeClient, scheme)}

		req := createRequestFor(&kubernautv1alpha1.Kubernaut{
			ObjectMeta: metav1.ObjectMeta{Name: "not-the-singleton"},
		})

		resp := wh.Handle(context.Background(), req)

		Expect(resp.Allowed).To(BeFalse())
	})

	It("denies a second Kubernaut CR when one already exists", func() {
		existing := &kubernautv1alpha1.Kubernaut{
			ObjectMeta: metav1.ObjectMeta{Name: kubernautv1alpha1.SingletonName, Namespace: "kubernaut-system"},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
		wh := &admission.Webhook{Handler: NewSingletonValidator(fakeClient, scheme)}

		req := createRequestFor(&kubernautv1alpha1.Kubernaut{
			ObjectMeta: metav1.ObjectMeta{Name: kubernautv1alpha1.SingletonName, Namespace: "other-namespace"},
		})

		resp := wh.Handle(context.Background(), req)

		Expect(resp.Allowed).To(BeFalse())
	})
})

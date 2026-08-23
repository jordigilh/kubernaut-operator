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
	"fmt"
	"net/http"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SingletonValidator is a validating admission webhook handler that enforces
// the singleton constraint on Kubernaut CRs: only one instance with the
// canonical name is allowed cluster-wide.
//
// Construct via NewSingletonValidator, not this literal: controller-runtime
// no longer auto-injects a decoder into admission.Handler implementations
// (the old DecoderInjector/InjectDecoder mechanism was removed), so a
// zero-value decoder field here is nil and Handle panics on every request.
type SingletonValidator struct {
	Client  client.Client
	decoder admission.Decoder
}

// NewSingletonValidator builds a SingletonValidator with a working decoder
// bound to scheme. This is the only supported way to construct one.
func NewSingletonValidator(c client.Client, scheme *runtime.Scheme) *SingletonValidator {
	return &SingletonValidator{
		Client:  c,
		decoder: admission.NewDecoder(scheme),
	}
}

func (v *SingletonValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx).WithName("singleton-webhook")

	if req.Operation != admissionv1.Create {
		return admission.Allowed("only CREATE is gated")
	}

	kn := &kubernautv1alpha1.Kubernaut{}
	if err := v.decoder.Decode(req, kn); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if kn.Name != kubernautv1alpha1.SingletonName {
		return admission.Denied(fmt.Sprintf(
			"Kubernaut CR name must be %q; got %q",
			kubernautv1alpha1.SingletonName, kn.Name))
	}

	existing := &kubernautv1alpha1.KubernautList{}
	if err := v.Client.List(ctx, existing); err != nil {
		log.Error(err, "failed to list Kubernaut CRs for singleton check")
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("listing Kubernaut CRs: %w", err))
	}

	if len(existing.Items) > 0 {
		return admission.Denied(fmt.Sprintf(
			"a Kubernaut CR already exists (%s/%s); only one instance is allowed cluster-wide",
			existing.Items[0].Namespace, existing.Items[0].Name))
	}

	return admission.Allowed("singleton constraint satisfied")
}

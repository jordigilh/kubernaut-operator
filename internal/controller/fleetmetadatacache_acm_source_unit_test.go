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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// #647 (DD-FLEET-017 v2 D4.1a): unit-tier coverage for the FMC ACM source
// Secret surfaces — the business-unit coverage gate requires these paths
// exercised without envtest, mirroring the scopeCheck lane's
// UT-FLEET-521-PREFLIGHT-SECRET-001 pattern.
var _ = Describe("FMC ACM source trust boundaries", Label("unit"), func() {
	withACMSource := func(tokenSecretRef *kubernautv1alpha2.SecretKeyRef, tls *kubernautv1alpha2.FleetTrustSpec) *kubernautv1alpha2.Kubernaut {
		kn := newMinimalCR()
		kn.Spec.Fleet = defaultFMCFleetSpec()
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
		kn.Spec.FleetMetadataCache.Sources = &kubernautv1alpha2.FleetMetadataCacheSourcesSpec{
			ACM: &kubernautv1alpha2.FleetACMSourceSpec{
				Endpoint:       "https://acm-search.example.com",
				TokenSecretRef: tokenSecretRef,
				TLS:            tls,
			},
		}
		return kn
	}

	It("UT-FLEET-647-UNIT-001: ignores the ACM source Secret lanes when no source is selected", func() {
		kn := newMinimalCR()
		kn.Spec.Fleet = defaultFMCFleetSpec()
		kn.Spec.WorkflowExecution.Fleet.OAuth2CredentialsSecretRef = testWEFleetOAuth2SecretRef
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.validateFleetMetadataCacheACMSourceSecrets(ctx, kn)).To(Succeed())
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(Succeed())
		Expect(fleetTrustSecretReferenced(kn, "any-secret")).To(BeFalse())
	})

	It("UT-FLEET-647-UNIT-002 [CM-7, IA-5]: validates the ACM source token Secret before deployment", func() {
		kn := withACMSource(&kubernautv1alpha2.SecretKeyRef{Name: "acm-search-token"}, nil)
		r := businessUnitReconciler(kn, interceptor.Funcs{})

		By("missing Secret fails closed with the sources.acm path")
		Expect(r.validateFleetMetadataCacheACMSourceSecrets(ctx, kn)).To(MatchError(ContainSubstring("spec.fleetMetadataCache.sources.acm.tokenSecretRef")))
		result, err := r.phaseValidate(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		condition := meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Reason).To(Equal(ReasonFleetACMSourceSecretInvalid))

		By("wrong key fails closed")
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "acm-search-token", Namespace: kn.Namespace}, Data: map[string][]byte{"wrong": []byte("token")}}
		Expect(r.Create(ctx, secret)).To(Succeed())
		Expect(r.validateFleetMetadataCacheACMSourceSecrets(ctx, kn)).To(MatchError(ContainSubstring("missing required key")))

		By("default key token passes")
		secret.Data["token"] = []byte("bearer")
		Expect(r.Update(ctx, secret)).To(Succeed())
		Expect(r.validateFleetMetadataCacheACMSourceSecrets(ctx, kn)).To(Succeed())
	})

	It("UT-FLEET-647-UNIT-003 [CM-7, SC-8]: validates the Secret-backed ACM source CA lane", func() {
		kn := withACMSource(
			&kubernautv1alpha2.SecretKeyRef{Name: "acm-search-token"},
			&kubernautv1alpha2.FleetTrustSpec{
				Source:          kubernautv1alpha2.FleetTrustSourceSecret,
				CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "acm-source-ca"},
			})
		r := businessUnitReconciler(kn, interceptor.Funcs{})

		By("missing CA Secret fails closed with the sources.acm.tls path")
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(MatchError(ContainSubstring("spec.fleetMetadataCache.sources.acm.tls.caCertSecretRef")))
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(MatchError(ContainSubstring("could not be read")))

		By("a Secret lacking the CA key fails closed")
		caSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "acm-source-ca", Namespace: kn.Namespace}, Data: map[string][]byte{"other": []byte("x")}}
		Expect(r.Create(ctx, caSecret)).To(Succeed())
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(MatchError(ContainSubstring("is missing key")))

		By("a parseable CA passes and the Secret is watched")
		caSecret.Data["ca.crt"] = acmSourceCAPEM()
		Expect(r.Update(ctx, caSecret)).To(Succeed())
		Expect(r.validateFleetTrustSecrets(ctx, kn)).To(Succeed())
		Expect(fleetTrustSecretReferenced(kn, "acm-source-ca")).To(BeTrue(),
			"a rotated or deleted ACM source CA Secret must trigger revalidation")
		Expect(fleetTrustSecretReferenced(kn, "unrelated-secret")).To(BeFalse())
	})
})

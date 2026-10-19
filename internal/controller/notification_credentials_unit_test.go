package controller

import (
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Notification provider-neutral Secret references", Label("unit"), func() {
	It("UT-NOT-SECRET-005 [IA-5, CM-6] records namespace-local Secret keys without provider assumptions", func() {
		kn := newMinimalCR()
		kn.Spec.Notification.Credentials = []kubernautv1alpha2.NotificationCredentialSpec{
			{
				Name:      "teams-primary",
				SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-teams", Key: "webhook-url"},
			},
			{
				Name:      "pagerduty-critical",
				SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-pagerduty", Key: "routing-key"},
			},
		}

		collector := newPrecreatedReferenceCollector()
		appendPrecreatedFeatureSecretReferences(collector, kn)
		Expect(collector.errs).To(BeEmpty())
		Expect(collector.references).To(ConsistOf(
			precreatedReference{
				kind:         precreatedSecretReference,
				path:         "spec.notification.credentials[0].secretRef",
				name:         "notification-teams",
				requiredKeys: []string{"webhook-url"},
			},
			precreatedReference{
				kind:         precreatedSecretReference,
				path:         "spec.notification.credentials[1].secretRef",
				name:         "notification-pagerduty",
				requiredKeys: []string{"routing-key"},
			},
		))
	})
})

package resources

import (
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Notification provider-neutral credentials", func() {
	It("UT-NOT-API-001 [CM-6, SI-10] rejects invalid duplicate credential identities and Secret references", func() {
		kn := testKubernaut()
		kn.Spec.Notification.Credentials = []kubernautv1alpha2.NotificationCredentialSpec{
			{
				Name:      "pagerduty-critical",
				SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-pagerduty", Key: "routing-key"},
			},
			{
				Name:      "pagerduty-critical",
				SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "NOT A SECRET NAME", Key: "bad/key"},
			},
			{
				Name:      "pagerduty/critical",
				SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-pagerduty", Key: "routing-key"},
			},
		}

		errs := ValidateKubernaut(kn)
		Expect(errs).To(HaveLen(4))
		Expect(errs[0].Error()).To(ContainSubstring("credentials[1].name must be unique"))
		Expect(errs[1].Error()).To(ContainSubstring("credentials[1].secretRef.name"))
		Expect(errs[2].Error()).To(ContainSubstring("credentials[1].secretRef.key"))
		Expect(errs[3].Error()).To(ContainSubstring("credentials[2].name must be a valid credential file name"))
	})

	It("UT-NOT-API-002 [IA-5, CM-6] accepts opaque provider credential values", func() {
		kn := testKubernaut()
		kn.Spec.Notification.Credentials = []kubernautv1alpha2.NotificationCredentialSpec{{
			Name:      "pagerduty-critical",
			SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-pagerduty", Key: "routing-key"},
		}}

		Expect(ValidateKubernaut(kn)).To(BeEmpty())
	})

	It("UT-NOT-DEP-002 [IA-5, SC-28] projects Slack, Teams, and PagerDuty credentials read-only", func() {
		kn := testKubernaut()
		kn.Spec.Notification.Credentials = []kubernautv1alpha2.NotificationCredentialSpec{
			{
				Name:      "slack-ops",
				SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-slack", Key: "webhook-url"},
			},
			{
				Name:      "teams-primary",
				SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-teams", Key: "webhook-url"},
			},
			{
				Name:      "pagerduty-critical",
				SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-pagerduty", Key: "routing-key"},
			},
		}

		dep, err := NotificationDeployment(kn, testKnV2(kn))
		Expect(err).NotTo(HaveOccurred())

		var credentials *corev1.Volume
		for i := range dep.Spec.Template.Spec.Volumes {
			if dep.Spec.Template.Spec.Volumes[i].Name == notificationCredentialsVolumeName {
				credentials = &dep.Spec.Template.Spec.Volumes[i]
				break
			}
		}
		Expect(credentials).NotTo(BeNil())
		Expect(credentials.Projected).NotTo(BeNil())
		Expect(credentials.Projected.Sources).To(HaveLen(3))
		Expect(credentials.Projected.Sources[0].Secret.Name).To(Equal("notification-slack"))
		Expect(credentials.Projected.Sources[0].Secret.Items[0].Key).To(Equal("webhook-url"))
		Expect(credentials.Projected.Sources[0].Secret.Items[0].Path).To(Equal("slack-ops"))
		Expect(credentials.Projected.Sources[1].Secret.Name).To(Equal("notification-teams"))
		Expect(credentials.Projected.Sources[1].Secret.Items[0].Path).To(Equal("teams-primary"))
		Expect(credentials.Projected.Sources[2].Secret.Name).To(Equal("notification-pagerduty"))
		Expect(credentials.Projected.Sources[2].Secret.Items[0].Path).To(Equal("pagerduty-critical"))

		var mount *corev1.VolumeMount
		for i := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
			if dep.Spec.Template.Spec.Containers[0].VolumeMounts[i].Name == notificationCredentialsVolumeName {
				mount = &dep.Spec.Template.Spec.Containers[0].VolumeMounts[i]
				break
			}
		}
		Expect(mount).NotTo(BeNil())
		Expect(mount.MountPath).To(Equal(notificationCredentialsMountPath))
		Expect(mount.ReadOnly).To(BeTrue())
	})

	It("UT-NOT-ROUTE-004 [CM-6, CC8.1] keeps generated routing provider-neutral", func() {
		kn := testKubernaut()
		kn.Spec.Notification.Credentials = []kubernautv1alpha2.NotificationCredentialSpec{
			{
				Name:      "slack-ops",
				SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-slack", Key: "webhook-url"},
			},
		}

		cm, err := NotificationRoutingConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data["routing.yaml"]).To(ContainSubstring("console"))
		Expect(cm.Data["routing.yaml"]).NotTo(ContainSubstring("slackConfigs"))
		Expect(cm.Data["routing.yaml"]).NotTo(ContainSubstring("credentialsSecretName"))
	})
})

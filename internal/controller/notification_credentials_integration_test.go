package controller

import (
	"context"
	"encoding/json"
	"strings"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Notification credential reconciliation", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("IT-NOT-CRD-RECON-001 [IA-5, SC-28, CM-6; SOC2 CC6.1, CC6.6, CC8.1; ASVS v5.0.0-V2.2.2, v5.0.0-V8.2.1, v5.0.0-V16.5.2] projects provider-neutral credentials and preserves BYO routing", func() {
		createBYOSecrets(ctx)

		routing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "notification-routing", Namespace: testNamespace},
			Data: map[string]string{"routing.yaml": `route:
  receiver: slack-ops
receivers:
  - name: slack-ops
    slackConfigs:
      - channel: "#ops"
        credentialRef: slack-ops
  - name: teams-primary
    teamsConfigs:
      - credentialRef: teams-primary
  - name: pagerduty-critical
    pagerdutyConfigs:
      - credentialRef: pagerduty-critical
`},
		}
		Expect(k8sClient.Create(ctx, routing)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, routing))).To(Succeed())
		})

		credentialObjects := []*corev1.Secret{
			{ObjectMeta: metav1.ObjectMeta{Name: "notification-slack", Namespace: testNamespace}, Data: map[string][]byte{"webhook-url": []byte("https://hooks.slack.example/test")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "notification-teams", Namespace: testNamespace}, Data: map[string][]byte{"webhook-url": []byte("https://teams.example/test")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "notification-pagerduty", Namespace: testNamespace}, Data: map[string][]byte{"routing-key": []byte("pagerduty-routing-key")}},
		}
		for _, secret := range credentialObjects {
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func(secret *corev1.Secret) func() {
				return func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, secret))).To(Succeed()) }
			}(secret))
		}

		cr := newMinimalCR()
		cr.Spec.Notification.Routing = &kubernautv1alpha2.ConfigMapRef{ConfigMapName: routing.Name}
		cr.Spec.Notification.Credentials = []kubernautv1alpha2.NotificationCredentialSpec{
			{Name: "slack-ops", SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-slack", Key: "webhook-url"}},
			{Name: "teams-primary", SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-teams", Key: "webhook-url"}},
			{Name: "pagerduty-critical", SecretRef: kubernautv1alpha2.NotificationSecretRef{Name: "notification-pagerduty", Key: "routing-key"}},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		reconcileToRunning(ctx)

		live := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), live)).To(Succeed())
		Expect(live.Status.Phase).To(Equal(kubernautv1alpha2.PhaseRunning))

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "notification-controller", Namespace: testNamespace}, deployment)).To(Succeed())
		var credentials *corev1.Volume
		for i := range deployment.Spec.Template.Spec.Volumes {
			if deployment.Spec.Template.Spec.Volumes[i].Name == "credentials" {
				credentials = &deployment.Spec.Template.Spec.Volumes[i]
				break
			}
		}
		Expect(credentials).NotTo(BeNil())
		Expect(credentials.Projected).NotTo(BeNil())
		Expect(credentials.Projected.Sources).To(HaveLen(3))

		rotatedSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: credentialObjects[0].Name, Namespace: testNamespace}, rotatedSecret)).To(Succeed())
		rotatedSecret.Data["webhook-url"] = []byte("https://hooks.slack.example/rotated")
		Expect(k8sClient.Update(ctx, rotatedSecret)).To(Succeed())
		requests := newReconciler().precreatedConfigurationToKubernaut(ctx, rotatedSecret)
		Expect(requests).To(ConsistOf(reconcile.Request{NamespacedName: singletonKey()}))

		serialized, err := json.Marshal(deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(serialized)).NotTo(ContainSubstring("https://hooks.slack.example/test"))
		Expect(string(serialized)).NotTo(ContainSubstring("https://teams.example/test"))
		Expect(string(serialized)).NotTo(ContainSubstring("pagerduty-routing-key"))
		statusJSON, err := json.Marshal(live.Status)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(statusJSON)).NotTo(ContainSubstring("https://hooks.slack.example/test"))
		Expect(string(statusJSON)).NotTo(ContainSubstring("https://teams.example/test"))
		Expect(string(statusJSON)).NotTo(ContainSubstring("pagerduty-routing-key"))
		Expect(string(statusJSON)).NotTo(ContainSubstring("https://hooks.slack.example/rotated"))

		generatedRouting := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "notification-routing-config", Namespace: testNamespace}, generatedRouting)
		Expect(err).To(HaveOccurred())
		Expect(strings.Contains(err.Error(), "not found")).To(BeTrue())
	})
})

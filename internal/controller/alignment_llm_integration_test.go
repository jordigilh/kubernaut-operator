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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("alignment sibling-agent reconciliation wiring", func() {
	AfterEach(func() {
		ctx := context.Background()
		for _, name := range []string{"alignment-llm-credentials", "alignment-oauth", "alignment-tls"} {
			secret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, secret); err == nil {
				_ = k8sClient.Delete(ctx, secret)
			}
		}
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		cleanupClusterScoped(ctx)
	})

	It("IT-419-RECONCILE-001 [CM-6, IA-5, AC-6, SC-8; SOC2 CC6.1, CC6.6, CC8.1; ASVS v5.0.0-V2.2.1, v5.0.0-V8.2.1, v5.0.0-V13.3.1] reconciles the independent alignment profile into ConfigMap and Deployment resources", func() {
		ctx := context.Background()
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "alignment-llm-credentials", Namespace: testNamespace},
			Data:       map[string][]byte{"api_key": []byte("alignment-api-key")},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "alignment-oauth", Namespace: testNamespace},
			Data:       map[string][]byte{"client-id": []byte("alignment-client"), "client-secret": []byte("alignment-secret")},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "alignment-tls", Namespace: testNamespace},
			Data:       validPrecreatedTLSData(),
		})).To(Succeed())

		kn := newMinimalCR()
		kn.Spec.APIFrontend.LLMProfileRef = "primary"
		kn.Spec.KubernautAgent.AlignmentCheck = kubernautv1alpha2.AlignmentCheckSpec{
			Enabled:       true,
			LLMProfileRef: "alignment-profile",
		}
		kn.Spec.LLMProfiles["alignment-profile"] = kubernautv1alpha2.LLMProfileSpec{
			Provider:              "anthropic",
			Model:                 "claude-3-5-sonnet",
			Endpoint:              "https://alignment.example/v1",
			CredentialsSecretName: "alignment-llm-credentials",
			TLSCaFile:             "/etc/kubernaut-agent/alignment-tls/ca.crt",
			TLSCertFile:           "/etc/kubernaut-agent/alignment-tls/client.crt",
			TLSKeyFile:            "/etc/kubernaut-agent/alignment-tls/client.key",
			TLSClientSecretRef:    "alignment-tls",
			OAuth2: kubernautv1alpha2.OAuth2Spec{
				Enabled:              true,
				TokenURL:             "https://idp.example/token",
				CredentialsSecretRef: "alignment-oauth",
			},
		}
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		reconcileToRunning(ctx)

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent-config", Namespace: testNamespace}, configMap)).To(Succeed())
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("provider: anthropic"))
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("apiKeyFile: /etc/kubernaut-agent/alignment-credentials/api_key"))
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("credentialsDir: /etc/kubernaut-agent/alignment-oauth2"))
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("tlsCertFile: /etc/kubernaut-agent/alignment-tls/client.crt"))

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(resources.ComponentKubernautAgent), Namespace: testNamespace}, deployment)).To(Succeed())
		var foundVolume bool
		for _, volume := range deployment.Spec.Template.Spec.Volumes {
			if volume.Name == "alignment-credentials" {
				foundVolume = true
				Expect(volume.Secret).NotTo(BeNil())
				Expect(volume.Secret.SecretName).To(Equal("alignment-llm-credentials"))
			}
		}
		Expect(foundVolume).To(BeTrue(), "alignment credentials Secret volume must be rendered")
		var foundMount bool
		for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
			if mount.Name == "alignment-credentials" && mount.MountPath == "/etc/kubernaut-agent/alignment-credentials" {
				foundMount = true
				Expect(mount.ReadOnly).To(BeTrue())
			}
		}
		Expect(foundMount).To(BeTrue(), "alignment credentials Secret mount must be read-only")
		for _, expected := range []struct {
			name string
			path string
		}{
			{name: "alignment-oauth2-credentials", path: "/etc/kubernaut-agent/alignment-oauth2"},
			{name: "alignment-llm-tls-client", path: "/etc/kubernaut-agent/alignment-tls"},
		} {
			found := false
			for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
				if mount.Name == expected.name && mount.MountPath == expected.path {
					found = true
					Expect(mount.ReadOnly).To(BeTrue())
				}
			}
			Expect(found).To(BeTrue(), "expected read-only mount %s at %s", expected.name, expected.path)
		}
	})

	It("IT-419-RECONCILE-002 [CM-6, IA-5, AC-6; SOC2 CC6.6, CC7.2, CC8.1; ASVS v5.0.0-V2.2.1, v5.0.0-V8.2.1, v5.0.0-V16.5.2] rejects a missing alignment Secret before deployment", func() {
		ctx := context.Background()
		createBYOSecrets(ctx)
		kn := newMinimalCR()
		kn.Spec.APIFrontend.LLMProfileRef = "primary"
		kn.Spec.KubernautAgent.AlignmentCheck = kubernautv1alpha2.AlignmentCheckSpec{
			Enabled:       true,
			LLMProfileRef: "alignment-profile",
		}
		kn.Spec.LLMProfiles["alignment-profile"] = kubernautv1alpha2.LLMProfileSpec{
			Provider:              "anthropic",
			Model:                 "claude-3-5-sonnet",
			CredentialsSecretName: "missing-alignment-credentials",
		}
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())

		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		condition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonPrecreatedConfigurationInvalid))
		deployment := &appsv1.Deployment{}
		getErr := k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(resources.ComponentKubernautAgent), Namespace: testNamespace}, deployment)
		Expect(apierrors.IsNotFound(getErr)).To(BeTrue())
	})
})

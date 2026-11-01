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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("LLM profile TLS material reconciliation wiring (#532)", func() {
	AfterEach(func() {
		ctx := context.Background()
		for _, name := range []string{
			"primary-ca-532", "primary-client-532", "workflow-ca-532", "workflow-client-532",
			"alignment-credentials-532", "alignment-ca-532", "alignment-client-532",
		} {
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

	It("IT-532-RECONCILE-001 [SC-8, SC-12, AC-6, IA-5, SI-10; SOC2 CC6.1, CC6.6, CC7.2, CC8.1; ASVS v5.0.0-V2.2.1, v5.0.0-V8.2.1, v5.0.0-V12.3.1, v5.0.0-V13.3.1] reconciles independent CA/client material for all three sibling agents", func() {
		ctx := context.Background()
		createBYOSecrets(ctx)
		createLLMProfileTLSSecret(ctx, "primary-ca-532", validPrecreatedCAData())
		createLLMProfileTLSSecret(ctx, "primary-client-532", validPrecreatedTLSData())
		createLLMProfileTLSSecret(ctx, "workflow-ca-532", validPrecreatedCAData())
		createLLMProfileTLSSecret(ctx, "workflow-client-532", validPrecreatedTLSData())
		createLLMProfileTLSSecret(ctx, "alignment-ca-532", validPrecreatedCAData())
		createLLMProfileTLSSecret(ctx, "alignment-client-532", validPrecreatedTLSData())
		createLLMProfileCredentialSecret(ctx, "alignment-credentials-532")

		kn := newMinimalCR()
		kn.Spec.APIFrontend.Enabled = ptr.To(false)
		kn.Spec.LLMProfiles["primary"] = llmProfileTLSFor532(
			"openai", "gpt-4o", llmSecretName, "primary-ca-532", "primary-client-532", "/etc/kubernaut-agent/llm-tls-client",
		)
		kn.Spec.LLMProfiles["workflow-tls"] = llmProfileTLSFor532(
			"anthropic", "claude-sonnet-4-6", llmSecretName, "workflow-ca-532", "workflow-client-532", "/etc/kubernaut-agent/workflow-tls",
		)
		kn.Spec.LLMProfiles["alignment-tls"] = llmProfileTLSFor532(
			"anthropic", "claude-sonnet-4-6", "alignment-credentials-532", "alignment-ca-532", "alignment-client-532", "/etc/kubernaut-agent/alignment-tls",
		)
		kn.Spec.KubernautAgent.WorkflowDiscovery.LLMProfileRef = "workflow-tls"
		kn.Spec.KubernautAgent.AlignmentCheck = kubernautv1alpha2.AlignmentCheckSpec{
			Enabled:       true,
			LLMProfileRef: "alignment-tls",
		}
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		reconcileToRunning(ctx)

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kubernaut-agent-config", Namespace: testNamespace}, configMap)).To(Succeed())
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("tlsCaFile: /etc/kubernaut-agent/llm-tls-client/ca.crt"))
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("tlsCaFile: /etc/kubernaut-agent/alignment-tls/ca.crt"))
		Expect(configMap.Data["config.yaml"]).NotTo(ContainSubstring("llm-api-key"))
		Expect(configMap.Data["config.yaml"]).NotTo(ContainSubstring("primary-ca-532"))
		Expect(configMap.Data["config.yaml"]).NotTo(ContainSubstring("primary-client-532"))

		runtimeConfigMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.KubernautAgentLLMRuntimeConfigName(kn), Namespace: testNamespace}, runtimeConfigMap)).To(Succeed())
		Expect(runtimeConfigMap.Data["llm-runtime.yaml"]).To(ContainSubstring("tlsCaFile: /etc/kubernaut-agent/workflow-tls/ca.crt"))

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(resources.ComponentKubernautAgent), Namespace: testNamespace}, deployment)).To(Succeed())
		expectProjectedLLMTLSSecret(deployment, "llm-tls-client", "primary-ca-532", "primary-client-532")
		expectProjectedLLMTLSSecret(deployment, "workflow-discovery-llm-tls-client", "workflow-ca-532", "workflow-client-532")
		expectProjectedLLMTLSSecret(deployment, "alignment-llm-tls-client", "alignment-ca-532", "alignment-client-532")
	})

	It("IT-532-RECONCILE-002 [SC-8, SC-12, SI-10, CM-6; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V2.2.1, v5.0.0-V12.3.2, v5.0.0-V16.5.2] rejects invalid profile CA material before workload creation", func() {
		ctx := context.Background()
		createBYOSecrets(ctx)
		createLLMProfileTLSSecret(ctx, "primary-ca-532", map[string][]byte{"ca.crt": []byte("not-a-certificate")})
		createLLMProfileTLSSecret(ctx, "primary-client-532", validPrecreatedTLSData())

		kn := newMinimalCR()
		kn.Spec.APIFrontend.Enabled = ptr.To(false)
		kn.Spec.LLMProfiles["primary"] = llmProfileTLSFor532(
			"openai", "gpt-4o", llmSecretName, "primary-ca-532", "primary-client-532", "/etc/kubernaut-agent/llm-tls-client",
		)
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
		Expect(condition.Message).NotTo(ContainSubstring("not-a-certificate"))
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(resources.ComponentKubernautAgent), Namespace: testNamespace}, deployment)).ToNot(Succeed())
	})
})

func createLLMProfileTLSSecret(ctx context.Context, name string, data map[string][]byte) {
	Expect(k8sClient.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Data:       data,
	})).To(Succeed())
}

func createLLMProfileCredentialSecret(ctx context.Context, name string) {
	createLLMProfileTLSSecret(ctx, name, map[string][]byte{"api_key": []byte("profile-api-key")})
}

func llmProfileTLSFor532(provider, model, credentialSecret, caSecret, clientSecret, mountPath string) kubernautv1alpha2.LLMProfileSpec {
	return kubernautv1alpha2.LLMProfileSpec{
		Provider:              provider,
		Model:                 model,
		Endpoint:              "https://llm.example/v1",
		CredentialsSecretName: credentialSecret,
		TLSCaFile:             mountPath + "/ca.crt",
		TLSCaSecretRef:        &kubernautv1alpha2.CACertSecretRef{Name: caSecret},
		TLSCertFile:           mountPath + "/tls.crt",
		TLSKeyFile:            mountPath + "/tls.key",
		TLSClientSecretRef:    clientSecret,
	}
}

func expectProjectedLLMTLSSecret(deployment *appsv1.Deployment, volumeName, caSecret, clientSecret string) {
	var volume *corev1.Volume
	for i := range deployment.Spec.Template.Spec.Volumes {
		if deployment.Spec.Template.Spec.Volumes[i].Name == volumeName {
			volume = &deployment.Spec.Template.Spec.Volumes[i]
			break
		}
	}
	Expect(volume).NotTo(BeNil(), "expected volume %q", volumeName)
	Expect(volume.Projected).NotTo(BeNil(), "expected projected volume %q", volumeName)
	seen := map[string]bool{}
	for _, source := range volume.Projected.Sources {
		if source.Secret != nil {
			seen[source.Secret.Name] = true
			switch source.Secret.Name {
			case caSecret:
				Expect(source.Secret.Items).To(ConsistOf(corev1.KeyToPath{Key: "ca.crt", Path: "ca.crt"}))
			case clientSecret:
				Expect(source.Secret.Items).To(HaveLen(2))
				Expect(source.Secret.Items).To(ContainElement(HaveField("Key", corev1.TLSCertKey)))
				Expect(source.Secret.Items).To(ContainElement(HaveField("Key", corev1.TLSPrivateKeyKey)))
			}
		}
	}
	Expect(seen).To(HaveLen(2), "only the selected sibling CA/client Secrets may be projected")
	Expect(seen).To(HaveKey(caSecret))
	Expect(seen).To(HaveKey(clientSecret))
	for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == volumeName {
			Expect(mount.ReadOnly).To(BeTrue(), "LLM TLS material mount %q must be read-only", volumeName)
			return
		}
	}
	Fail("expected read-only mount for " + volumeName)
}

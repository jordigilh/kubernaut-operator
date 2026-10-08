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

var _ = Describe("OTLP telemetry reconciliation wiring", func() {
	AfterEach(func() {
		ctx := context.Background()
		for _, name := range []string{
			"gateway-telemetry-ca", "gateway-telemetry-client",
			"datastorage-telemetry-ca", "datastorage-telemetry-client",
			"agent-telemetry-ca", "agent-telemetry-client",
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

	It("IT-TELEMETRY-TLS-001 [AC-6, IA-5, SC-8, SC-12, SC-13, SI-4; ASVS v5.0.0-V12.1.3, v5.0.0-V12.3.1, v5.0.0-V12.3.2, v5.0.0-V12.3.4, v5.0.0-V14.2.4] reconciles all three network telemetry producers with read-only Secret mounts and non-sensitive revisions", func() {
		ctx := context.Background()
		createBYOSecrets(ctx)
		secrets := integrationTelemetrySecrets()
		for _, secret := range secrets {
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		}

		kn := newCRWithRouteDisabled()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeDevelopmentSelfSigned
		kn.Spec.TLS.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{}
		kn.Spec.Gateway.Config.Telemetry = integrationTelemetrySpec("gateway-telemetry-ca", "gateway-telemetry-client")
		kn.Spec.DataStorage.Telemetry = integrationTelemetrySpec("datastorage-telemetry-ca", "datastorage-telemetry-client")
		kn.Spec.KubernautAgent.Telemetry = integrationTelemetrySpec("agent-telemetry-ca", "agent-telemetry-client")
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		reconcileToRunning(ctx)

		for _, component := range []string{resources.ComponentGateway, resources.ComponentDataStorage, resources.ComponentKubernautAgent} {
			dep := getTelemetryDeployment(ctx, component)
			Expect(dep.Spec.Template.Annotations).To(HaveKey(resources.AnnotationTelemetryMaterialRevision), component)
			Expect(dep.Spec.Template.Annotations[resources.AnnotationTelemetryMaterialRevision]).To(HaveLen(64), component)
			Expect(findTelemetryVolume(dep, "telemetry-material")).NotTo(BeNil(), component)
			Expect(findTelemetryMount(dep, "telemetry-material", resources.TelemetryMountDir)).To(BeTrue(), component)
		}

		for _, configMapName := range []string{"gateway-config", "datastorage-config", "kubernaut-agent-config"} {
			configMap := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: testNamespace}, configMap)).To(Succeed())
			Expect(configMap.Data["config.yaml"]).To(ContainSubstring("endpoint: otel-collector:4317"), configMapName)
			Expect(configMap.Data["config.yaml"]).To(ContainSubstring("caFile: /etc/telemetry/ca.crt"), configMapName)
		}
	})

	It("IT-TELEMETRY-TLS-002/005/007 [AC-6, IA-5, SC-8, SC-12, SC-13, SI-4; ASVS v5.0.0-V2.2.1, v5.0.0-V12.3.2, v5.0.0-V14.2.4] preserves existing Deployments on invalid Secret rotation and rolls only the affected producer after recovery", func() {
		ctx := context.Background()
		createBYOSecrets(ctx)
		for _, secret := range integrationTelemetrySecrets() {
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		}

		kn := newCRWithRouteDisabled()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeDevelopmentSelfSigned
		kn.Spec.TLS.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{}
		kn.Spec.Gateway.Config.Telemetry = integrationTelemetrySpec("gateway-telemetry-ca", "gateway-telemetry-client")
		kn.Spec.DataStorage.Telemetry = integrationTelemetrySpec("datastorage-telemetry-ca", "datastorage-telemetry-client")
		kn.Spec.KubernautAgent.Telemetry = integrationTelemetrySpec("agent-telemetry-ca", "agent-telemetry-client")
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := reconcileToRunning(ctx)

		before := telemetryDeploymentRevisions(ctx)
		ca := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "datastorage-telemetry-ca", Namespace: testNamespace}, ca)).To(Succeed())
		ca.Data["ca.crt"] = []byte("not a certificate")
		Expect(k8sClient.Update(ctx, ca)).To(Succeed())

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		afterInvalid := telemetryDeploymentRevisions(ctx)
		Expect(afterInvalid).To(Equal(before), "invalid Secret rotation must not replace any last-known-good Deployment")

		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		condition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Reason).To(Equal(ReasonTelemetrySecretInvalid))

		ca.Data["ca.crt"] = telemetryCAPEM()
		Expect(k8sClient.Update(ctx, ca)).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())

		afterValid := telemetryDeploymentRevisions(ctx)
		Expect(afterValid[resources.ComponentDataStorage]).NotTo(Equal(before[resources.ComponentDataStorage]))
		Expect(afterValid[resources.ComponentGateway]).To(Equal(before[resources.ComponentGateway]))
		Expect(afterValid[resources.ComponentKubernautAgent]).To(Equal(before[resources.ComponentKubernautAgent]))
	})

	It("IT-TELEMETRY-TLS-003 [SC-7, SC-8, CM-6; ASVS v5.0.0-V12.1.3, v5.0.0-V12.2.1] preserves local-only telemetry without network Secret requirements or mounts", func() {
		ctx := context.Background()
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeDevelopmentSelfSigned
		kn.Spec.TLS.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{}
		logSink := true
		kn.Spec.Gateway.Config.Telemetry = kubernautv1alpha2.TelemetrySpec{LogSink: &logSink}
		kn.Spec.DataStorage.Telemetry = kubernautv1alpha2.TelemetrySpec{Endpoint: "stdout"}
		kn.Spec.KubernautAgent.Telemetry = kubernautv1alpha2.TelemetrySpec{}
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		reconcileToRunning(ctx)

		for _, component := range []string{resources.ComponentGateway, resources.ComponentDataStorage, resources.ComponentKubernautAgent} {
			dep := getTelemetryDeployment(ctx, component)
			Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(resources.AnnotationTelemetryMaterialRevision), component)
			Expect(findTelemetryVolume(dep, "telemetry-material")).To(BeNil(), component)
			Expect(findTelemetryVolume(dep, "telemetry-ca")).To(BeNil(), component)
			Expect(findTelemetryVolume(dep, "telemetry-client")).To(BeNil(), component)
		}
		gatewayConfig := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gateway-config", Namespace: testNamespace}, gatewayConfig)).To(Succeed())
		Expect(gatewayConfig.Data["config.yaml"]).To(ContainSubstring("logSink: true"))
		dataStorageConfig := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "datastorage-config", Namespace: testNamespace}, dataStorageConfig)).To(Succeed())
		Expect(dataStorageConfig.Data["config.yaml"]).To(ContainSubstring("endpoint: stdout"))
	})

	It("IT-TELEMETRY-TLS-002 [IA-5, SC-8, SI-10; ASVS v5.0.0-V12.3.2, v5.0.0-V12.3.4] reports a missing referenced Secret before deployment", func() {
		ctx := context.Background()
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeDevelopmentSelfSigned
		kn.Spec.TLS.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{}
		kn.Spec.DataStorage.Telemetry = kubernautv1alpha2.TelemetrySpec{
			Endpoint: "otel-collector:4317",
			TLS: kubernautv1alpha2.TelemetryTLSConfig{
				CACertSecretRef: &kubernautv1alpha2.CACertSecretRef{Name: "missing-telemetry-ca"},
			},
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
		Expect(condition.Reason).To(Equal(ReasonTelemetrySecretInvalid))
		dep := &appsv1.Deployment{}
		getErr := k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(resources.ComponentDataStorage), Namespace: testNamespace}, dep)
		Expect(apierrors.IsNotFound(getErr)).To(BeTrue())
	})
})

func integrationTelemetrySpec(caName, clientName string) kubernautv1alpha2.TelemetrySpec {
	return kubernautv1alpha2.TelemetrySpec{
		Endpoint: "otel-collector:4317",
		TLS: kubernautv1alpha2.TelemetryTLSConfig{
			CACertSecretRef:    &kubernautv1alpha2.CACertSecretRef{Name: caName},
			CertFile:           resources.TelemetryMountDir + "/tls.crt",
			KeyFile:            resources.TelemetryMountDir + "/tls.key",
			TLSClientSecretRef: clientName,
		},
	}
}

func integrationTelemetrySecrets() []*corev1.Secret {
	secrets := make([]*corev1.Secret, 0, 6)
	for _, name := range []string{"gateway-telemetry-ca", "datastorage-telemetry-ca", "agent-telemetry-ca"} {
		secrets = append(secrets, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Data:       map[string][]byte{"ca.crt": telemetryCAPEM()},
		})
	}
	for _, name := range []string{"gateway-telemetry-client", "datastorage-telemetry-client", "agent-telemetry-client"} {
		key, certificate := telemetryClientKeyPair()
		secrets = append(secrets, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Type:       corev1.SecretTypeTLS,
			Data: map[string][]byte{
				corev1.TLSCertKey:       certificate,
				corev1.TLSPrivateKeyKey: key,
			},
		})
	}
	return secrets
}

func getTelemetryDeployment(ctx context.Context, component string) *appsv1.Deployment {
	dep := &appsv1.Deployment{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(component), Namespace: testNamespace}, dep)).To(Succeed())
	return dep
}

func telemetryDeploymentRevisions(ctx context.Context) map[string]string {
	revisions := make(map[string]string, 3)
	for _, component := range []string{resources.ComponentGateway, resources.ComponentDataStorage, resources.ComponentKubernautAgent} {
		dep := getTelemetryDeployment(ctx, component)
		revisions[component] = dep.Spec.Template.Annotations[resources.AnnotationTelemetryMaterialRevision]
	}
	return revisions
}

func findTelemetryVolume(dep *appsv1.Deployment, name string) *corev1.Volume {
	for index := range dep.Spec.Template.Spec.Volumes {
		if dep.Spec.Template.Spec.Volumes[index].Name == name {
			return &dep.Spec.Template.Spec.Volumes[index]
		}
	}
	return nil
}

func findTelemetryMount(dep *appsv1.Deployment, name, path string) bool {
	for _, mount := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == name && mount.MountPath == path && mount.ReadOnly {
			return true
		}
	}
	return false
}

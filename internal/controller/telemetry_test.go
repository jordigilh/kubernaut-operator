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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("Telemetry controller material lifecycle", func() {
	It("validates administrator-owned CA and client Secrets without mutating them", func() {
		kn := telemetryUnitKubernaut()
		kn.Spec.DataStorage.Telemetry = telemetrySecretSpec()
		ca := telemetryCASecret("telemetry-ca", "ca.crt")
		clientSecret := telemetryClientSecret()
		r := newTelemetryUnitReconciler(kn, ca, clientSecret, telemetryTrustBundle(kn.Namespace))

		Expect(r.validateTelemetrySecrets(context.Background(), kn)).To(Succeed())

		storedCA := &corev1.Secret{}
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(ca), storedCA)).To(Succeed())
		Expect(storedCA.OwnerReferences).To(BeEmpty())
		Expect(storedCA.Data).To(Equal(ca.Data))
	})

	It("rejects malformed CA and incomplete client material before deployment", func() {
		kn := telemetryUnitKubernaut()
		kn.Spec.DataStorage.Telemetry = telemetrySecretSpec()
		ca := telemetryCASecret("telemetry-ca", "wrong-key")
		ca.Data["wrong-key"] = []byte("not a certificate")
		clientSecret := telemetryClientSecret()
		delete(clientSecret.Data, corev1.TLSPrivateKeyKey)
		r := newTelemetryUnitReconciler(kn, ca, clientSecret, telemetryTrustBundle(kn.Namespace))

		err := r.validateTelemetrySecrets(context.Background(), kn)
		Expect(err).To(MatchError(ContainSubstring("caCertSecretRef")))

		ca.Data["ca.crt"] = telemetryCAPEM()
		Expect(r.Update(context.Background(), ca)).To(Succeed())
		err = r.validateTelemetrySecrets(context.Background(), kn)
		Expect(err).To(MatchError(ContainSubstring("tlsClientSecretRef")))
	})

	It("rejects malformed ambient trust before a rollout revision is accepted", func() {
		kn := telemetryUnitKubernaut()
		kn.Spec.DataStorage.Telemetry = kubernautv1alpha2.TelemetrySpec{Endpoint: "otel-collector:4317"}
		trust := telemetryTrustBundle(kn.Namespace)
		trust.Data["service-ca.crt"] = "not a certificate"
		r := newTelemetryUnitReconciler(kn, trust)

		Expect(r.validateTelemetryAmbientTrust(context.Background(), kn)).To(MatchError(ContainSubstring("valid ca pem")))
		_, err := r.telemetryMaterialRevisions(context.Background(), kn)
		Expect(err).To(MatchError(ContainSubstring("valid ca pem")))
	})

	It("maps referenced Secret events and changes only non-sensitive revision metadata", func() {
		kn := telemetryUnitKubernaut()
		kn.Spec.DataStorage.Telemetry = telemetrySecretSpec()
		ca := telemetryCASecret("telemetry-ca", "ca.crt")
		ca.ResourceVersion = "7"
		clientSecret := telemetryClientSecret()
		clientSecret.ResourceVersion = "9"
		trust := telemetryTrustBundle(kn.Namespace)
		trust.ResourceVersion = "12"
		r := newTelemetryUnitReconciler(kn, ca, clientSecret, trust)

		requests := r.telemetrySecretToKubernaut(context.Background(), ca)
		Expect(requests).To(Equal([]reconcile.Request{{NamespacedName: types.NamespacedName{Name: kn.Name, Namespace: kn.Namespace}}}))

		first, err := r.telemetryMaterialRevisions(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(HaveKey(resources.ComponentDataStorage))
		Expect(first[resources.ComponentDataStorage]).NotTo(ContainSubstring("telemetry-ca"))

		rotatedCA := telemetryCASecret("telemetry-ca", "ca.crt")
		rotatedCA.ResourceVersion = "8"
		rotatedClient := telemetryClientSecret()
		rotatedClient.ResourceVersion = "9"
		rotatedTrust := telemetryTrustBundle(kn.Namespace)
		rotatedTrust.ResourceVersion = "12"
		rotatedReconciler := newTelemetryUnitReconciler(kn, rotatedCA, rotatedClient, rotatedTrust)
		second, err := rotatedReconciler.telemetryMaterialRevisions(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(second[resources.ComponentDataStorage]).NotTo(Equal(first[resources.ComponentDataStorage]))

		dep := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": resources.ComponentDataStorage}},
		}}}
		stampTelemetryMaterialRevision(dep, second)
		Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(resources.AnnotationTelemetryMaterialRevision, second[resources.ComponentDataStorage]))
		Expect(dep.Spec.Template.Annotations[resources.AnnotationTelemetryMaterialRevision]).NotTo(ContainSubstring("not a certificate"))
	})

	It("does not resolve ambient trust when all telemetry lanes are local-only", func() {
		kn := telemetryUnitKubernaut()
		r := newTelemetryUnitReconciler(kn)

		revisions, err := r.telemetryMaterialRevisions(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions).To(BeEmpty())
	})

	It("tracks a manual runtime CA Secret as ambient trust without exposing its contents", func() {
		kn := telemetryUnitKubernaut()
		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		kn.Spec.TLS.AdministratorManaged = &kubernautv1alpha2.AdministratorManagedTLSConfig{
			InternalCASecretName: "internal-ca",
		}
		kn.Spec.DataStorage.Telemetry = kubernautv1alpha2.TelemetrySpec{Endpoint: "otel-collector:4317"}
		ca := telemetryCASecret("internal-ca", "ca.crt")
		ca.ResourceVersion = "21"
		r := newTelemetryUnitReconciler(kn, ca)

		requests := r.telemetrySecretToKubernaut(context.Background(), ca)
		Expect(requests).To(Equal([]reconcile.Request{{NamespacedName: types.NamespacedName{Name: kn.Name, Namespace: kn.Namespace}}}))
		first, err := r.telemetryMaterialRevisions(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())

		rotatedCA := telemetryCASecret("internal-ca", "ca.crt")
		rotatedCA.ResourceVersion = "22"
		rotatedReconciler := newTelemetryUnitReconciler(kn, rotatedCA)
		second, err := rotatedReconciler.telemetryMaterialRevisions(context.Background(), kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(second[resources.ComponentDataStorage]).NotTo(Equal(first[resources.ComponentDataStorage]))
		Expect(second[resources.ComponentDataStorage]).NotTo(ContainSubstring("telemetry-test-ca"))
	})

	It("maps effective ambient trust ConfigMap events only to network telemetry producers", func() {
		kn := telemetryUnitKubernaut()
		kn.Spec.DataStorage.Telemetry = kubernautv1alpha2.TelemetrySpec{Endpoint: "otel-collector:4317"}
		trust := telemetryTrustBundle(kn.Namespace)
		r := newTelemetryUnitReconciler(kn, trust)
		expected := []reconcile.Request{{NamespacedName: types.NamespacedName{Name: kn.Name, Namespace: kn.Namespace}}}

		Expect(r.telemetryAmbientConfigMapToKubernaut(context.Background(), trust)).To(Equal(expected))

		openshiftIngress := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: "default-ingress-cert", Namespace: openshiftConfigManagedNamespace,
		}}
		Expect(r.telemetryAmbientConfigMapToKubernaut(context.Background(), openshiftIngress)).To(Equal(expected))

		localOnly := kn.DeepCopy()
		localOnly.Spec.DataStorage.Telemetry = kubernautv1alpha2.TelemetrySpec{Endpoint: "stdout"}
		localReconciler := newTelemetryUnitReconciler(localOnly, trust)
		Expect(localReconciler.telemetryAmbientConfigMapToKubernaut(context.Background(), trust)).To(BeEmpty())

		wrongNamespace := trust.DeepCopy()
		wrongNamespace.Namespace = "unrelated"
		Expect(r.telemetryAmbientConfigMapToKubernaut(context.Background(), wrongNamespace)).To(BeEmpty())
	})

	It("recognizes only ambient trust ConfigMaps and respects manual Secret ownership", func() {
		kn := telemetryUnitKubernaut()
		kn.Spec.DataStorage.Telemetry = kubernautv1alpha2.TelemetrySpec{Endpoint: "otel-collector:4317"}

		Expect(telemetryAmbientConfigMap(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: "default-ingress-cert", Namespace: openshiftConfigManagedNamespace,
		}})).To(BeTrue())
		Expect(telemetryAmbientConfigMap(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: resources.InterServiceCAConfigMapName, Namespace: kn.Namespace,
		}})).To(BeTrue())
		Expect(telemetryAmbientConfigMap(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: "unrelated", Namespace: kn.Namespace,
		}})).To(BeFalse())

		trust := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: resources.InterServiceCAConfigMapName, Namespace: kn.Namespace,
		}}
		Expect(ambientTrustConfigMapRelevant(kn, trust)).To(BeTrue())

		kn.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		kn.Spec.TLS.AdministratorManaged = &kubernautv1alpha2.AdministratorManagedTLSConfig{
			InternalCASecretName: "internal-ca",
		}
		Expect(ambientTrustConfigMapRelevant(kn, trust)).To(BeFalse())
		kn.Spec.TLS.AdministratorManaged.InternalCASecretName = ""
		Expect(ambientTrustConfigMapRelevant(kn, trust)).To(BeTrue())
	})
})

func telemetryUnitKubernaut() *kubernautv1alpha2.Kubernaut {
	return &kubernautv1alpha2.Kubernaut{
		ObjectMeta: metav1.ObjectMeta{Name: kubernautv1alpha2.SingletonName, Namespace: "telemetry-test"},
		Spec: kubernautv1alpha2.KubernautSpec{
			DataStorage:    kubernautv1alpha2.DataStorageSpec{},
			KubernautAgent: kubernautv1alpha2.KubernautAgentSpec{},
		},
	}
}

func telemetrySecretSpec() kubernautv1alpha2.TelemetrySpec {
	return kubernautv1alpha2.TelemetrySpec{
		Endpoint: "otel-collector:4317",
		TLS: kubernautv1alpha2.TelemetryTLSConfig{
			CACertSecretRef:    &kubernautv1alpha2.CACertSecretRef{Name: "telemetry-ca"},
			CertFile:           resources.TelemetryMountDir + "/tls.crt",
			KeyFile:            resources.TelemetryMountDir + "/tls.key",
			TLSClientSecretRef: "telemetry-client",
		},
	}
}

func telemetryCASecret(name, key string) *corev1.Secret {
	data := map[string][]byte{key: telemetryCAPEM()}
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "telemetry-test"}, Data: data}
}

func telemetryClientSecret() *corev1.Secret {
	key, certificate := telemetryClientKeyPair()
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "telemetry-client", Namespace: "telemetry-test"},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certificate,
			corev1.TLSPrivateKeyKey: key,
		},
	}
}

func telemetryCAPEM() []byte {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "telemetry-test-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func telemetryClientKeyPair() ([]byte, []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "telemetry-test-client"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func telemetryTrustBundle(namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: resources.TrustBundleConfigMapName, Namespace: namespace},
		Data:       map[string]string{"service-ca.crt": string(telemetryCAPEM())},
	}
}

func newTelemetryUnitReconciler(objects ...runtime.Object) *KubernautReconciler {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
	return &KubernautReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build(),
		Scheme: scheme,
	}
}

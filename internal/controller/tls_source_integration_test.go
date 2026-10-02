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
	"encoding/pem"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("runtime TLS source wiring", func() {
	It("IT-TLS-GAP-001 validates cert-manager output and issuer discovery without adopting Secrets", func() {
		ctx := context.Background()
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				Issuer: kubernautv1alpha2.TLSIssuerRef{
					Name:  "kubernaut-ca",
					Kind:  "Issuer",
					Group: "cert-manager.io",
				},
				InternalCASecretName:  "kubernaut-internal-ca",
				ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
			},
		}

		development := kn.DeepCopy()
		development.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}
		generated, err := resources.DevelopmentSelfSignedTLSSecrets(development, nil, time.Now().UTC().Add(-time.Hour))
		Expect(err).NotTo(HaveOccurred())

		caSecret := generated[0].DeepCopy()
		caSecret.Name = "kubernaut-internal-ca"
		caSecret.Data["tls.crt"] = append([]byte(nil), caSecret.Data["ca.crt"]...)
		delete(caSecret.Data, "ca.crt")

		issuer := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "cert-manager.io/v1",
			"kind":       "Issuer",
			"metadata": map[string]interface{}{
				"name":      "kubernaut-ca",
				"namespace": testNamespace,
			},
		}}
		crd := &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "issuers.cert-manager.io"},
		}
		objects := make([]runtime.Object, 0, len(generated)+2)
		objects = append(objects, caSecret, issuer, crd)
		for _, secret := range generated[1:] {
			secretCopy := secret.DeepCopy()
			objects = append(objects, secretCopy)
		}

		r := newReconcilerWithCRDScheme(objects...)
		material, err := r.validateTLSConfiguration(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(resources.TLSMaterialSourceCertManager))
		Expect(material.OwnsSecrets).To(BeFalse())

		for _, object := range objects {
			secret, ok := object.(*corev1.Secret)
			if !ok {
				continue
			}
			Expect(secret.OwnerReferences).To(BeEmpty(), "cert-manager/runtime input Secret %q must remain user-owned", secret.Name)
		}
	})

	It("IT-TLS-GAP-002 fails closed when cert-manager discovery is absent", func() {
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				Issuer:                kubernautv1alpha2.TLSIssuerRef{Name: "missing-issuer"},
				InternalCASecretName:  "missing-ca",
				ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
			},
		}

		r := newReconcilerWithCRDScheme()
		_, err := r.validateTLSConfiguration(context.Background(), kn)
		Expect(err).To(MatchError(ContainSubstring("cert-manager api is not installed")))
	})

	It("IT-TLS-ROTATION-GAP-001 preserves the previous trust root when a leaf write fails", func() {
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{RotationBefore: "9600h"},
		}
		initialTime := time.Now().UTC().Add(-20 * 24 * time.Hour)
		initial, err := resources.DevelopmentSelfSignedTLSSecrets(kn, nil, initialTime)
		Expect(err).NotTo(HaveOccurred())

		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		baseClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(secretRuntimeObjects(initial)...).Build()
		failingClient := &tlsLeafWriteFailingClient{Client: baseClient}
		r := &KubernautReconciler{
			Client:   failingClient,
			Scheme:   scheme,
			Recorder: events.NewFakeRecorder(100),
			now:      func() time.Time { return initialTime.Add(20 * 24 * time.Hour) },
		}
		material, err := resources.ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())

		_, _, err = r.ensureDevelopmentSelfSignedTLS(context.Background(), kn, material)
		Expect(err).To(MatchError(ContainSubstring("simulated leaf secret update failure")))

		ca := &corev1.Secret{}
		Expect(baseClient.Get(context.Background(), client.ObjectKey{Namespace: kn.Namespace, Name: material.InternalCASecretName}, ca)).To(Succeed())
		Expect(certificateCount(ca.Data["ca.crt"])).To(Equal(2), "the previous root must remain published after a failed leaf update")
		gateway := &corev1.Secret{}
		Expect(baseClient.Get(context.Background(), client.ObjectKey{Namespace: kn.Namespace, Name: resources.GatewayTLSSecretName}, gateway)).To(Succeed())
		Expect(resources.ValidateServingTLSSecretForService(gateway, ca, resources.TLSServiceGateway, kn.Namespace)).To(Succeed())
	})
})

func certManagerServiceTLSSecretNames() map[string]string {
	return map[string]string{
		resources.TLSServiceGateway:        resources.GatewayTLSSecretName,
		resources.TLSServiceDataStorage:    resources.DataStorageTLSSecretName,
		resources.TLSServiceKubernautAgent: resources.KubernautAgentTLSSecretName,
		resources.TLSServiceAPIFrontend:    resources.APIFrontendTLSSecretName,
		resources.TLSServiceAuthWebhook:    "authwebhook-tls",
	}
}

type tlsLeafWriteFailingClient struct {
	client.Client
}

func (c *tlsLeafWriteFailingClient) Update(ctx context.Context, object client.Object, opts ...client.UpdateOption) error {
	if object.GetName() == resources.GatewayTLSSecretName {
		return fmt.Errorf("simulated leaf secret update failure")
	}
	return c.Client.Update(ctx, object, opts...)
}

func secretRuntimeObjects(secrets []*corev1.Secret) []runtime.Object {
	objects := make([]runtime.Object, 0, len(secrets))
	for _, secret := range secrets {
		objects = append(objects, secret)
	}
	return objects
}

func certificateCount(data []byte) int {
	count := 0
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			return count
		}
		if block.Type == "CERTIFICATE" {
			count++
		}
		data = rest
	}
	return count
}

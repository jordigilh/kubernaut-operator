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
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
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
	It("IT-TLS-MANUAL-001 [AC-6, IA-5, SC-8, SC-17; SOC2 CC6, CC7; ASVS v5.0.0-V12.1.3, v5.0.0-V13.3.1, v5.0.0-V13.3.2] leaves the administrator-owned trust ConfigMap unchanged", func() {
		ctx := context.Background()
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeManual}
		trust := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: resources.InterServiceCAConfigMapName, Namespace: kn.Namespace},
			Data:       map[string]string{"ca.crt": "administrator-ca"},
		}
		r := newReconcilerWithCRDScheme(trust)
		Expect(kubernautv1alpha2.AddToScheme(r.Scheme)).To(Succeed())

		Expect(r.ensureGenericTLSConfigMaps(ctx, kn, []byte("operator-ca"))).To(Succeed())
		live := &corev1.ConfigMap{}
		Expect(r.Get(ctx, client.ObjectKeyFromObject(trust), live)).To(Succeed())
		Expect(live.Data).To(Equal(map[string]string{"ca.crt": "administrator-ca"}))
		Expect(live.OwnerReferences).To(BeEmpty())

		volume := resources.InterServiceTLSCAVolume(kn)
		Expect(volume.ConfigMap).NotTo(BeNil())
		Expect(volume.ConfigMap.Name).To(Equal(resources.InterServiceCAConfigMapName))
	})

	It("IT-TLS-ADMIN-001 [AC-6, IA-5, SC-8, SC-17; SOC2 CC6, CC7; ASVS v5.0.0-V12.1.3, v5.0.0-V13.3.1, v5.0.0-V13.3.2] leaves the explicit administrator-managed trust ConfigMap unchanged", func() {
		ctx := context.Background()
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeAdministratorManaged,
			AdministratorManaged: &kubernautv1alpha2.AdministratorManagedTLSConfig{
				InternalCASecretName:  "administrator-internal-ca",
				ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
			},
		}
		trust := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: resources.InterServiceCAConfigMapName, Namespace: kn.Namespace},
			Data:       map[string]string{"ca.crt": "administrator-ca", "custom.crt": "must-survive"},
		}
		r := newReconcilerWithCRDScheme(trust)
		Expect(kubernautv1alpha2.AddToScheme(r.Scheme)).To(Succeed())

		Expect(r.ensureGenericTLSConfigMaps(ctx, kn, []byte("operator-ca"))).To(Succeed())
		live := &corev1.ConfigMap{}
		Expect(r.Get(ctx, client.ObjectKeyFromObject(trust), live)).To(Succeed())
		Expect(live.Data).To(Equal(map[string]string{"ca.crt": "administrator-ca", "custom.crt": "must-survive"}))
		Expect(live.OwnerReferences).To(BeEmpty())

		derived := &corev1.ConfigMap{}
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.TrustBundleConfigMapName}, derived)).To(Succeed())
		Expect(derived.Data["ca.crt"]).To(Equal("operator-ca"))
	})

	It("IT-TLS-DEV-001 [AC-6, SC-8, SC-12, SC-13; SOC2 CC6, CC7; ASVS v5.0.0-V11.1.1, v5.0.0-V12.1.1, v5.0.0-V13.2.1] wires development TLS through the reconciler", func() {
		ctx := context.Background()
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}
		r := newReconcilerWithCRDScheme()
		Expect(kubernautv1alpha2.AddToScheme(r.Scheme)).To(Succeed())

		material, caPEM, err := r.ensureRuntimeTLS(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(resources.TLSMaterialSourceDevelopmentSelfSigned))
		Expect(material.OwnsSecrets).To(BeTrue())
		Expect(caPEM).NotTo(BeEmpty())
		Expect(r.ensureGenericTLSConfigMaps(ctx, kn, caPEM)).To(Succeed())
		Expect(r.deployAdmissionWebhooks(ctx, kn, material, caPEM)).To(Succeed())

		gateway := &corev1.Secret{}
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.GatewayTLSSecretName}, gateway)).To(Succeed())
		Expect(resources.ValidateServingTLSSecret(gateway)).To(Succeed())
		trust := &corev1.ConfigMap{}
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.TrustBundleConfigMapName}, trust)).To(Succeed())
		Expect(trust.Data["ca.crt"]).To(Equal(string(caPEM)))
	})

	It("IT-TLS-ROTATION-GAP-002 reads development TLS Secrets from the API server during cache lag", func() {
		ctx := context.Background()
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeHook}
		initialTime := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
		initial, err := resources.DevelopmentSelfSignedTLSSecrets(kn, nil, initialTime)
		Expect(err).NotTo(HaveOccurred())

		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
		cachedObjects := make([]runtime.Object, 0, len(initial))
		apiObjects := make([]runtime.Object, 0, len(initial)-1)
		for _, secret := range initial {
			cachedObjects = append(cachedObjects, secret.DeepCopy())
			if secret.Name != resources.GatewayTLSSecretName {
				apiObjects = append(apiObjects, secret.DeepCopy())
			}
		}
		initialGateway := &corev1.Secret{}
		for _, secret := range initial {
			if secret.Name == resources.GatewayTLSSecretName {
				initialGateway = secret
				break
			}
		}
		cachedClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(cachedObjects...).Build()
		apiReader := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(apiObjects...).Build()
		r := &KubernautReconciler{
			Client:    cachedClient,
			APIReader: apiReader,
			Scheme:    scheme,
			Recorder:  events.NewFakeRecorder(100),
			now:       func() time.Time { return initialTime.Add(time.Minute) },
		}
		material, err := resources.ResolveTLSMaterial(kn)
		Expect(err).NotTo(HaveOccurred())

		_, _, err = r.ensureDevelopmentSelfSignedTLS(ctx, kn, material)
		Expect(err).NotTo(HaveOccurred())

		gateway := &corev1.Secret{}
		Expect(cachedClient.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.GatewayTLSSecretName}, gateway)).To(Succeed())
		Expect(gateway.Data[corev1.TLSCertKey]).NotTo(Equal(
			initialGateway.Data[corev1.TLSCertKey],
		))
	})

	It("IT-TLS-HOOK-001 [AC-6, SC-8, SC-12, SC-13; SOC2 CC6, CC7, A1; ASVS v5.0.0-V11.1.1, v5.0.0-V12.1.1, v5.0.0-V13.2.1] wires hook TLS through the reconciler", func() {
		ctx := context.Background()
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeHook,
			Hooks: &kubernautv1alpha2.TLSHooksConfig{TLSCerts: kubernautv1alpha2.TLSCertsConfig{
				ExtraSANs: []string{"localhost"},
			}},
		}
		r := newReconcilerWithCRDScheme()
		Expect(kubernautv1alpha2.AddToScheme(r.Scheme)).To(Succeed())

		material, caPEM, err := r.ensureRuntimeTLS(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(resources.TLSMaterialSourceDevelopmentSelfSigned))
		Expect(material.OwnsSecrets).To(BeTrue())
		Expect(caPEM).NotTo(BeEmpty())
		Expect(r.ensureGenericTLSConfigMaps(ctx, kn, caPEM)).To(Succeed())
		Expect(r.deployAdmissionWebhooks(ctx, kn, material, caPEM)).To(Succeed())

		gateway := &corev1.Secret{}
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: resources.GatewayTLSSecretName}, gateway)).To(Succeed())
		Expect(resources.ValidateServingTLSSecretForService(gateway, &corev1.Secret{
			Data: map[string][]byte{"ca.crt": caPEM},
		}, resources.TLSServiceGateway, kn.Namespace)).To(Succeed())
		mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
		Expect(r.Get(ctx, client.ObjectKey{Name: kn.Namespace + "-authwebhook-mutating"}, mwc)).To(Succeed())
		Expect(mwc.Webhooks[0].ClientConfig.CABundle).To(Equal(caPEM))
	})

	It("IT-TLS-MANUAL-002 [IA-5, SC-8, SC-13, SC-17; SOC2 CC6, CC7; ASVS v5.0.0-V11.1.1, v5.0.0-V12.1.3, v5.0.0-V13.3.1] validates administrator-owned serving, signing, and webhook material", func() {
		ctx := context.Background()
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeManual}

		generatedCR := kn.DeepCopy()
		generatedCR.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeHook}
		generated, err := resources.DevelopmentSelfSignedTLSSecrets(generatedCR, nil, time.Now().UTC())
		Expect(err).NotTo(HaveOccurred())

		trust := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: resources.InterServiceCAConfigMapName, Namespace: kn.Namespace},
			Data:       map[string]string{"ca.crt": string(generated[0].Data["ca.crt"])},
		}
		mwc := resources.MutatingWebhookConfiguration(kn)
		vwc := resources.ValidatingWebhookConfiguration(kn)
		for index := range mwc.Webhooks {
			mwc.Webhooks[index].ClientConfig.CABundle = []byte("administrator-webhook-ca")
		}
		for index := range vwc.Webhooks {
			vwc.Webhooks[index].ClientConfig.CABundle = []byte("administrator-webhook-ca")
		}

		objects := make([]runtime.Object, 0, len(generated)+3)
		objects = append(objects, trust, mwc, vwc)
		for _, secret := range generated[1:] {
			objects = append(objects, secret.DeepCopy())
		}
		r := newReconcilerWithCRDScheme(objects...)

		material, err := r.validateTLSConfiguration(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(material.Source).To(Equal(resources.TLSMaterialSourceAdministratorManaged))
		Expect(material.InternalCAConfigMapName).To(Equal(resources.InterServiceCAConfigMapName))

		resolved, caPEM, err := r.ensureRuntimeTLS(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved.Source).To(Equal(resources.TLSMaterialSourceAdministratorManaged))
		Expect(caPEM).To(Equal(generated[0].Data["ca.crt"]))
	})

	It("IT-TLS-WEBHOOK-001 [SC-8, SC-13, SC-17, SI-4; SOC2 CC6, CC7; ASVS v5.0.0-V12.1.3, v5.0.0-V13.2.1, v5.0.0-V16.5.2] preserves manual webhook CA bundles", func() {
		ctx := context.Background()
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeManual}
		mwc := resources.MutatingWebhookConfiguration(kn)
		vwc := resources.ValidatingWebhookConfiguration(kn)
		for index := range mwc.Webhooks {
			mwc.Webhooks[index].ClientConfig.CABundle = []byte("administrator-webhook-ca")
		}
		for index := range vwc.Webhooks {
			vwc.Webhooks[index].ClientConfig.CABundle = []byte("administrator-webhook-ca")
		}
		r := newReconcilerWithCRDScheme(mwc, vwc)
		material := resources.TLSMaterial{
			Source:                resources.TLSMaterialSourceAdministratorManaged,
			ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
		}

		Expect(r.deployAdmissionWebhooks(ctx, kn, material, []byte("wrong-inter-service-ca"))).To(Succeed())
		updatedMWC := &admissionregistrationv1.MutatingWebhookConfiguration{}
		Expect(r.Get(ctx, client.ObjectKey{Name: mwc.Name}, updatedMWC)).To(Succeed())
		Expect(updatedMWC.Annotations).NotTo(HaveKey(resources.OCPServiceCAInjectAnnotation))
		Expect(updatedMWC.Webhooks[0].ClientConfig.CABundle).To(Equal([]byte("administrator-webhook-ca")))
		updatedVWC := &admissionregistrationv1.ValidatingWebhookConfiguration{}
		Expect(r.Get(ctx, client.ObjectKey{Name: vwc.Name}, updatedVWC)).To(Succeed())
		Expect(updatedVWC.Webhooks[0].ClientConfig.CABundle).To(Equal([]byte("administrator-webhook-ca")))
	})

	It("IT-OWN-514-023 [AC-6, IA-5; ASVS v5.0.0-V13.3.1] preserves cert-manager finalizers during a full resource update", func() {
		ctx := context.Background()
		kn := newMinimalCR()
		kn.UID = "owner-uid"
		kn.Generation = 1
		scheme := schemeForTLSResource()

		desired := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "cert-manager.io/v1",
			"kind":       "Certificate",
			"metadata": map[string]interface{}{
				"name":      "gateway-tls",
				"namespace": kn.Namespace,
			},
			"spec": map[string]interface{}{"secretName": "gateway-tls"},
		}}
		Expect(resources.SetOwnerReference(kn, desired, scheme)).To(Succeed())
		resources.StampOwnership(kn, desired)
		setHashAnnotation(desired, resources.SpecHash(desired))
		live := desired.DeepCopy()
		live.SetFinalizers([]string{"cert-manager.io/finalizer", "acme.example/cleanup"})
		annotations := live.GetAnnotations()
		annotations[resources.AnnotationSpecHash] = "stale-hash"
		annotations["cert-manager.io/issue-temporary-certificate"] = "true"
		live.SetAnnotations(annotations)

		r := &KubernautReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(live).Build(),
			Scheme:   scheme,
			Recorder: events.NewFakeRecorder(100),
		}
		Expect(r.ensureCertManagerResource(ctx, kn, desired)).To(Succeed())

		updated := &unstructured.Unstructured{}
		updated.SetAPIVersion("cert-manager.io/v1")
		updated.SetKind("Certificate")
		Expect(r.Get(ctx, client.ObjectKey{Name: "gateway-tls", Namespace: kn.Namespace}, updated)).To(Succeed())
		Expect(updated.GetFinalizers()).To(Equal([]string{"cert-manager.io/finalizer", "acme.example/cleanup"}))
		Expect(updated.GetAnnotations()).To(HaveKeyWithValue("cert-manager.io/issue-temporary-certificate", "true"))
	})

	It("IT-TLS-PARITY-001 [AC-6, CM-6, SC-12, SC-13, SI-4; SOC2 CC6, CC7, CC8; ASVS v5.0.0-V11.1.1, v5.0.0-V12.1.1, v5.0.0-V13.3.1, v5.0.0-V13.3.2] provisions chart-compatible cert-manager resources and owns only those resources", func() {
		ctx := context.Background()
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{
			Mode: kubernautv1alpha2.TLSModeHelmCertManager,
			CertManager: &kubernautv1alpha2.CertManagerTLSConfig{
				Issuer: kubernautv1alpha2.TLSIssuerRef{
					Name:  "customer-issuer",
					Kind:  "Issuer",
					Group: "cert-manager.io",
				},
			},
		}
		objects := []runtime.Object{
			&unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "cert-manager.io/v1",
				"kind":       "Issuer",
				"metadata": map[string]interface{}{
					"name":      "customer-issuer",
					"namespace": testNamespace,
				},
			}},
			&apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "issuers.cert-manager.io"}},
			&apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "certificates.cert-manager.io"}},
		}
		r := newReconcilerWithCRDScheme(objects...)
		Expect(kubernautv1alpha2.AddToScheme(r.Scheme)).To(Succeed())

		Expect(r.ensureCertManagerTLSResources(ctx, kn)).To(Succeed())

		certificate := &unstructured.Unstructured{}
		certificate.SetAPIVersion("cert-manager.io/v1")
		certificate.SetKind("Certificate")
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: "gateway-tls"}, certificate)).To(Succeed())
		Expect(certificate.GetOwnerReferences()).To(HaveLen(1))
		Expect(certificate.GetOwnerReferences()[0].UID).To(Equal(kn.UID))

		outputSecret := &corev1.Secret{}
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: "gateway-tls"}, outputSecret)).To(MatchError(ContainSubstring("not found")))
	})

	It("IT-TLS-CERTMANAGER-READY-001 [SC-8, SI-4, SI-10; SOC2 CC7, A1; ASVS v5.0.0-V12.2.1, v5.0.0-V16.5.2] waits for every Certificate Ready condition", func() {
		kn := newMinimalCR()
		kn.Spec.TLS = kubernautv1alpha2.TLSConfigSpec{Mode: kubernautv1alpha2.TLSModeHelmCertManager}
		certificates, err := resources.CertManagerTLSResources(kn)
		Expect(err).NotTo(HaveOccurred())

		objects := make([]runtime.Object, 0, len(certificates))
		for _, certificate := range certificates {
			if certificate.GetKind() == "Certificate" {
				certificate.Object["status"] = map[string]interface{}{
					"conditions": []interface{}{map[string]interface{}{
						"type": "Ready", "status": "True",
					}},
				}
			}
			objects = append(objects, certificate)
		}
		r := newReconcilerWithCRDScheme(objects...)
		Expect(r.validateCertManagerCertificatesReady(context.Background(), kn)).To(Succeed())

		firstCertificate := &unstructured.Unstructured{}
		firstCertificate.SetAPIVersion(certificates[1].GetAPIVersion())
		firstCertificate.SetKind(certificates[1].GetKind())
		Expect(r.Get(context.Background(), client.ObjectKey{Namespace: kn.Namespace, Name: certificates[1].GetName()}, firstCertificate)).To(Succeed())
		firstCertificate.Object["status"] = map[string]interface{}{
			"conditions": []interface{}{map[string]interface{}{
				"type": "Ready", "status": "False", "reason": "Pending", "message": "waiting for issuer",
			}},
		}
		Expect(r.Update(context.Background(), firstCertificate)).To(Succeed())
		Expect(r.validateCertManagerCertificatesReady(context.Background(), kn)).To(MatchError(ContainSubstring("Pending")))
	})

	It("IT-TLS-GAP-001 [AC-6, IA-5, SC-8, SC-12, SC-13, SC-17; SOC2 CC6, CC7; ASVS v5.0.0-V11.1.1, v5.0.0-V12.1.1, v5.0.0-V12.1.3, v5.0.0-V13.2.1, v5.0.0-V13.3.1] validates cert-manager output and issuer discovery without adopting Secrets", func() {
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

	It("IT-TLS-GAP-002 [SC-8, SI-4, SI-10; SOC2 CC7, A1; ASVS v5.0.0-V12.2.1, v5.0.0-V16.5.2] fails closed when cert-manager discovery is absent", func() {
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

	It("IT-TLS-ROTATION-GAP-001 [SC-8, SC-12, SC-13, SI-4; SOC2 CC7, A1; ASVS v5.0.0-V11.1.1, v5.0.0-V11.1.2, v5.0.0-V12.1.1, v5.0.0-V16.5.2] preserves the previous trust root when a leaf write fails", func() {
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

func schemeForTLSResource() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(kubernautv1alpha2.AddToScheme(scheme)).To(Succeed())
	return scheme
}

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

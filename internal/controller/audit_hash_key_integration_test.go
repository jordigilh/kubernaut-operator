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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("DataStorage audit HMAC reconciliation wiring", func() {
	ctx := context.Background()

	AfterEach(func() {
		cleanupNamespacedResources(ctx)
		deleteCRIfExists(ctx)
		deleteBYOSecrets(ctx)
		for _, name := range []string{"customer-audit-key", resources.DataStorageAuditHashKeySecretName} {
			secret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: name}, secret); err == nil {
				_ = k8sClient.Delete(ctx, secret)
			}
		}
	})

	It("IT-AUDIT-209-INT-001 [AU-9, SI-4, SOC2 CC7.2] provisions the generated key through Reconcile", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())

		generated := &corev1.Secret{}
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: resources.DataStorageAuditHashKeySecretName}, generated)
		}, timeout, interval).Should(Succeed())
		Expect(generated.Immutable).NotTo(BeNil())
		Expect(*generated.Immutable).To(BeTrue())
		Expect(len(generated.Data[resources.DataStorageAuditHashKeySecretDataKey])).To(BeNumerically(">", len("hmacKey: ")))

		state := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: resources.DataStorageAuditHashKeyStateConfigMapName}, state)).To(Succeed())
		Expect(state.Data).To(HaveKeyWithValue("secretName", resources.DataStorageAuditHashKeySecretName))
		Expect(state.Data).To(HaveKeyWithValue("source", resources.DataStorageAuditHashKeySourceGenerated))

		firstFingerprint, err := resources.DataStorageAuditHashKeyFingerprint(generated, resources.DataStorageAuditHashKeyDefault)
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(generated), generated)).To(Succeed())
		secondFingerprint, err := resources.DataStorageAuditHashKeyFingerprint(generated, resources.DataStorageAuditHashKeyDefault)
		Expect(err).NotTo(HaveOccurred())
		Expect(secondFingerprint).To(Equal(firstFingerprint))
	})

	It("IT-DS-550-INT-001 [CM-3, CM-6, SI-4, SOC2 CC7.2] renders nested DataStorage configuration through Reconcile", func() {
		createBYOSecrets(ctx)
		cr := newCRWithRouteDisabled()
		maxConcurrent := 42
		cr.Spec.DataStorage.Server = &kubernautv1alpha2.DataStorageServerSpec{
			MaxBodySize:           "10485760",
			MaxConcurrentRequests: &maxConcurrent,
		}
		dlqMaxLen := 1234
		cr.Spec.DataStorage.Redis = &kubernautv1alpha2.DataStorageRedisSpec{DLQMaxLen: &dlqMaxLen}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		reconcileToDeployPhase(ctx)

		configMap := &corev1.ConfigMap{}
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "datastorage-config"}, configMap)
		}, timeout, interval).Should(Succeed())
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("maxBodySize: \"10485760\""))
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("maxConcurrentRequests: 42"))
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("dlqMaxLen: 1234"))

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "data-storage"}, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).NotTo(BeEmpty())
	})

	It("IT-DS-550-ROT-001 [AU-9, AC-6, SI-4; SOC2 CC7.2; ASVS V13.3.1, V16.5.2] rolls DataStorage after signing Secret material changes", func() {
		createBYOSecrets(ctx)
		cr := newCRWithRouteDisabled()
		cr.Spec.TLS.Mode = kubernautv1alpha2.TLSModeManual
		cr.Spec.TLS.Manual = &kubernautv1alpha2.ManualTLSConfig{
			InternalCASecretName:  "kubernaut-internal-ca",
			ServiceTLSSecretNames: certManagerServiceTLSSecretNames(),
		}
		cr.Spec.DataStorage.Audit = &kubernautv1alpha2.DataStorageAuditSpec{
			SigningCertificate: &kubernautv1alpha2.SigningCertSpec{SecretName: "customer-signing"},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		runtimeSecrets, err := testRuntimeTLSSecrets(cr, nil, time.Now())
		Expect(err).NotTo(HaveOccurred())
		for _, secret := range runtimeSecrets {
			if secret.Name == "customer-signing" {
				continue
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		}
		for _, webhook := range []client.Object{
			resources.MutatingWebhookConfigurationWithCABundle(cr, runtimeSecrets[0].Data["ca.crt"]),
			resources.ValidatingWebhookConfigurationWithCABundle(cr, runtimeSecrets[0].Data["ca.crt"]),
		} {
			Expect(k8sClient.Create(ctx, webhook)).To(Succeed())
		}

		signingFixture := cr.DeepCopy()
		signingFixture.Spec.DataStorage.Audit.SigningCertificate = nil
		signingSecrets, err := testRuntimeTLSSecrets(signingFixture, nil, time.Now())
		Expect(err).NotTo(HaveOccurred())
		defaultSigningName := resources.DataStorageSigningSecretName(signingFixture)
		for _, secret := range signingSecrets {
			if secret.Name != defaultSigningName {
				continue
			}
			secret.Name = "customer-signing"
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		}

		r := reconcileToDeployPhase(ctx)
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: resources.DeploymentName(resources.ComponentDataStorage)}, deployment)).To(Succeed())
		before := deployment.Spec.Template.Annotations[resources.AnnotationDataStorageSigningMaterialRevision]
		Expect(before).To(HaveLen(64))

		signing := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "customer-signing", Namespace: testNamespace}, signing)).To(Succeed())
		if signing.Data == nil {
			signing.Data = map[string][]byte{}
		}
		signing.Data["rotation-marker"] = []byte("rotated")
		Expect(k8sClient.Update(ctx, signing)).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)).To(Succeed())
		after := deployment.Spec.Template.Annotations[resources.AnnotationDataStorageSigningMaterialRevision]
		Expect(after).To(HaveLen(64))
		Expect(after).NotTo(Equal(before))
	})

	It("IT-DS-550-ROT-002 [SI-4, CM-3, SOC2 CC7.2] maps signing Secret changes to the singleton", func() {
		cr := newCRWithRouteDisabled()
		cr.Spec.DataStorage.Audit = &kubernautv1alpha2.DataStorageAuditSpec{
			SigningCertificate: &kubernautv1alpha2.SigningCertSpec{SecretName: "customer-signing"},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		r := newReconciler()
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "customer-signing", Namespace: testNamespace}}
		Expect(r.dataStorageSigningSecretToKubernaut(ctx, secret)).To(ConsistOf(
			reconcile.Request{NamespacedName: singletonKey()},
		))
		Expect(r.dataStorageSigningSecretToKubernaut(ctx, &corev1.ConfigMap{})).To(BeNil())
	})

	It("IT-AUDIT-209-INT-002 [AC-6, IA-5, SOC2 CC6.6] uses a BYO key without adopting it", func() {
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		setDataStorageAuditHashKey(kn, &kubernautv1alpha2.DataStorageAuditHashKeySpec{
			ExistingSecret: "customer-audit-key",
			SecretKey:      "customHMACKey",
		})
		customerSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "customer-audit-key", Namespace: testNamespace},
			Data: map[string][]byte{
				resources.DataStorageAuditHashKeySecretDataKey: []byte("customHMACKey: " + strings.Repeat("x", 32)),
			},
		}
		Expect(k8sClient.Create(ctx, customerSecret)).To(Succeed())
		before := customerSecret.DeepCopy()
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())

		stored := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(customerSecret), stored)).To(Succeed())
		Expect(stored.Data).To(Equal(before.Data))
		Expect(stored.OwnerReferences).To(BeEmpty())
		Expect(stored.Labels).To(BeEmpty())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: resources.DataStorageAuditHashKeyStateConfigMapName}, &corev1.ConfigMap{})).To(Succeed())
	})

	It("IT-AUDIT-209-INT-003 [SI-10, ASVS V16.5.2] blocks invalid BYO material before migration", func() {
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		setDataStorageAuditHashKey(kn, &kubernautv1alpha2.DataStorageAuditHashKeySpec{ExistingSecret: "customer-audit-key"})
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "customer-audit-key", Namespace: testNamespace},
			Data:       map[string][]byte{resources.DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: short")},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())

		r := newReconciler()
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		current := fetchKnV2(ctx)
		condition := meta.FindStatusCondition(current.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonAuditHashKeyInvalid))
	})

	It("IT-AUDIT-209-INT-004 [AU-9, CC8.1, ASVS V16.5.2] fails closed after initialized key loss", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := newReconciler()
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: resources.DataStorageAuditHashKeySecretName}, secret)).To(Succeed())
		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		current := fetchKnV2(ctx)
		condition := meta.FindStatusCondition(current.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonAuditHashKeyInvalid))
	})

	It("IT-AUDIT-209-INT-005 [AC-6, CC6.6, ASVS V13.3.2] preserves a foreign default Secret", func() {
		createBYOSecrets(ctx)
		foreign := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: resources.DataStorageAuditHashKeySecretName, Namespace: testNamespace},
			Data:       map[string][]byte{resources.DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + strings.Repeat("z", 32))},
		}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		before := foreign.DeepCopy()
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := newReconciler()
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		stored := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), stored)).To(Succeed())
		Expect(stored.Data).To(Equal(before.Data))
		current := fetchKnV2(ctx)
		condition := meta.FindStatusCondition(current.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	})

	It("IT-AUDIT-209-INT-006 [SI-4, ASVS V16.1.1/V16.2.1] maps audit Secret and state events to the singleton", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := newReconciler()
		secretRequests := r.auditHashKeySecretToKubernaut(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: resources.DataStorageAuditHashKeySecretName, Namespace: testNamespace,
		}})
		Expect(secretRequests).To(ConsistOf(reconcile.Request{NamespacedName: singletonKey()}))
		stateRequests := r.auditHashKeyStateToKubernaut(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: resources.DataStorageAuditHashKeyStateConfigMapName, Namespace: testNamespace,
		}})
		Expect(stateRequests).To(ConsistOf(reconcile.Request{NamespacedName: singletonKey()}))
	})

	It("IT-AUDIT-209-INT-007 [AU-9, CM-6, ASVS v5.0.0-V13.3.1/V16.5.2] carries audit wiring through migration and deployment", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		reconcileToDeployPhase(ctx)

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "datastorage-config"}, configMap)).To(Succeed())
		Expect(configMap.Data["config.yaml"]).To(ContainSubstring("hashKeySecretsFile: /etc/datastorage/secrets/audit-hmac-key.yaml"))
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: resources.DeploymentName(resources.ComponentDataStorage)}, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Volumes).To(ContainElement(HaveField("Name", "secrets")))
		foundProjection := false
		foundReadOnlyMount := false
		for _, volume := range deployment.Spec.Template.Spec.Volumes {
			if volume.Name != "secrets" || volume.Projected == nil {
				continue
			}
			for _, source := range volume.Projected.Sources {
				if source.Secret != nil && source.Secret.Name == resources.DataStorageAuditHashKeySecretName {
					Expect(source.Secret.Items).To(ConsistOf([]corev1.KeyToPath{{
						Key: resources.DataStorageAuditHashKeySecretDataKey, Path: resources.DataStorageAuditHashKeySecretDataKey,
					}}))
					foundProjection = true
				}
			}
		}
		Expect(foundProjection).To(BeTrue())
		for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
			if mount.Name == "secrets" {
				foundReadOnlyMount = mount.ReadOnly
			}
		}
		Expect(foundReadOnlyMount).To(BeTrue())
	})

	It("IT-AUDIT-209-INT-008 [AC-6, AU-9, ASVS v5.0.0-V13.3.2/V16.5.2] rejects a foreign Secret during provisioning recovery", func() {
		createBYOSecrets(ctx)
		Expect(k8sClient.Create(ctx, newCRWithRouteDisabled())).To(Succeed())
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		current := fetchKnV2(ctx)
		ref := resources.DataStorageAuditHashKeyReferenceFor(current)
		state := resources.DataStorageAuditHashKeyStateConfigMap(current, ref)
		state.Data[resources.DataStorageAuditHashKeyStatePhaseKey] = resources.DataStorageAuditHashKeyStateProvisioning
		resources.StampOwnership(current, state)
		Expect(k8sClient.Create(ctx, state)).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: ref.SecretName, Namespace: testNamespace},
			Data:       map[string][]byte{resources.DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + strings.Repeat("f", 32))},
		})).To(Succeed())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		current = fetchKnV2(ctx)
		condition := meta.FindStatusCondition(current.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonAuditHashKeyInvalid))
	})

	It("IT-AUDIT-209-INT-009 [AU-9, CC8.1, ASVS v5.0.0-V11.1.1] blocks a changed BYO key after commitment", func() {
		createBYOSecrets(ctx)
		kn := newCRWithRouteDisabled()
		setDataStorageAuditHashKey(kn, &kubernautv1alpha2.DataStorageAuditHashKeySpec{ExistingSecret: "customer-audit-key"})
		customerSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "customer-audit-key", Namespace: testNamespace},
			Data:       map[string][]byte{resources.DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + strings.Repeat("x", 32))},
		}
		Expect(k8sClient.Create(ctx, customerSecret)).To(Succeed())
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		r := newReconciler()
		for range 2 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
			Expect(err).NotTo(HaveOccurred())
		}
		customerSecret.Data[resources.DataStorageAuditHashKeySecretDataKey] = []byte("hmacKey: " + strings.Repeat("y", 32))
		Expect(k8sClient.Update(ctx, customerSecret)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		current := fetchKnV2(ctx)
		condition := meta.FindStatusCondition(current.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonAuditHashKeyInvalid))
	})
})

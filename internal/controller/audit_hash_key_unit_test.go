package controller

import (
	"context"
	"errors"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("DataStorage audit HMAC lifecycle", Label("unit"), func() {
	It("UT-AUDIT-209-CTRL-001 [AU-9, SI-4, CC7.2] creates a stable generated key before deployment validation", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, clientInterceptorNone())

		_, err := r.phaseValidate(context.Background(), kn, kn)
		Expect(err).NotTo(HaveOccurred())

		generated := &corev1.Secret{}
		Expect(r.Get(context.Background(), client.ObjectKey{
			Namespace: kn.Namespace,
			Name:      "datastorage-audit-hmac-key",
		}, generated)).To(Succeed())
		Expect(generated.Immutable).NotTo(BeNil())
		Expect(*generated.Immutable).To(BeTrue())
		Expect(generated.Data).To(HaveKey("audit-hmac-key.yaml"))
		Expect(len(generated.Data["audit-hmac-key.yaml"])).To(BeNumerically(">", len("hmacKey: ")))

		firstFingerprint, err := resources.DataStorageAuditHashKeyFingerprint(generated, resources.DataStorageAuditHashKeyDefault)
		Expect(err).NotTo(HaveOccurred())
		_, err = r.phaseValidate(context.Background(), kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(generated), generated)).To(Succeed())
		secondFingerprint, err := resources.DataStorageAuditHashKeyFingerprint(generated, resources.DataStorageAuditHashKeyDefault)
		Expect(err).NotTo(HaveOccurred())
		Expect(secondFingerprint).To(Equal(firstFingerprint))
	})

	It("UT-AUDIT-209-CTRL-002 [AU-9, CC8.1, ASVS V16.5.2] does not regenerate a lost initialized key", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, clientInterceptorNone())
		ref := resources.DataStorageAuditHashKeyReferenceFor(kn)
		generated, err := resources.NewDataStorageAuditHashKeySecret(kn, ref)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Create(context.Background(), generated)).To(Succeed())
		state := resources.DataStorageAuditHashKeyStateConfigMap(kn, ref)
		fingerprint, err := resources.DataStorageAuditHashKeyFingerprint(generated, ref.SecretKey)
		Expect(err).NotTo(HaveOccurred())
		state.Data["keyFingerprint"] = fingerprint
		Expect(r.Create(context.Background(), state)).To(Succeed())
		generated.Finalizers = nil
		Expect(r.Update(context.Background(), generated)).To(Succeed())
		Expect(r.Delete(context.Background(), generated)).To(Succeed())

		_, err = r.phaseValidate(context.Background(), kn, kn)
		Expect(err).NotTo(HaveOccurred())
		missing := &corev1.Secret{}
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(generated), missing)).To(HaveOccurred())
		condition := apiMeta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	})

	It("UT-AUDIT-209-CTRL-003 [AC-6, IA-5, ASVS V13.3.1/V13.3.2] validates and preserves an administrator-managed Secret", func() {
		kn := newMinimalCR()
		kn.Spec.DataStorage.AuditHashKey = &kubernautv1alpha2.DataStorageAuditHashKeySpec{
			ExistingSecret: "customer-audit-key",
			SecretKey:      "customHMACKey",
		}
		r := businessUnitReconciler(kn, clientInterceptorNone())
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "customer-audit-key", Namespace: kn.Namespace},
			Data: map[string][]byte{
				resources.DataStorageAuditHashKeySecretDataKey: []byte("customHMACKey: " + strings.Repeat("x", 32)),
			},
		}
		Expect(r.Create(context.Background(), secret)).To(Succeed())

		result, err := r.phaseValidate(context.Background(), kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))
		stored := &corev1.Secret{}
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(secret), stored)).To(Succeed())
		Expect(stored.OwnerReferences).To(BeEmpty())
		Expect(stored.Labels).To(BeEmpty())
		state := &corev1.ConfigMap{}
		Expect(r.Get(context.Background(), client.ObjectKey{Namespace: kn.Namespace, Name: resources.DataStorageAuditHashKeyStateConfigMapName}, state)).To(Succeed())
		Expect(state.Data).To(HaveKeyWithValue("source", resources.DataStorageAuditHashKeySourceExisting))
	})

	It("UT-AUDIT-209-CTRL-004 [SI-10, ASVS V16.5.2] blocks an invalid administrator-managed Secret before migration", func() {
		kn := newMinimalCR()
		kn.Spec.DataStorage.AuditHashKey = &kubernautv1alpha2.DataStorageAuditHashKeySpec{ExistingSecret: "customer-audit-key"}
		r := businessUnitReconciler(kn, clientInterceptorNone())
		sentinel := "secret-material-sentinel"
		Expect(r.Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "customer-audit-key", Namespace: kn.Namespace},
			Data:       map[string][]byte{resources.DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + sentinel)},
		})).To(Succeed())

		result, err := r.phaseValidate(context.Background(), kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		refreshBusinessUnitCR(r, kn)
		condition := apiMeta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonAuditHashKeyInvalid))
		Expect(condition.Message).NotTo(ContainSubstring(sentinel))
		eventRecorder := r.Recorder.(*events.FakeRecorder)
		Expect(<-eventRecorder.Events).NotTo(ContainSubstring(sentinel))
	})

	It("UT-AUDIT-209-CTRL-005 [AU-9, CC8.1] rejects changing the effective audit key identity", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.phaseValidate(context.Background(), kn, kn)).To(Equal(ctrl.Result{}))

		kn.Spec.DataStorage.AuditHashKey = &kubernautv1alpha2.DataStorageAuditHashKeySpec{ExistingSecret: "customer-audit-key"}
		Expect(r.Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "customer-audit-key", Namespace: kn.Namespace},
			Data:       map[string][]byte{resources.DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + strings.Repeat("y", 32))},
		})).To(Succeed())
		result, err := r.phaseValidate(context.Background(), kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		refreshBusinessUnitCR(r, kn)
		condition := apiMeta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)
		Expect(condition.Reason).To(Equal(ReasonAuditHashKeyInvalid))
	})

	It("UT-AUDIT-209-CTRL-006 [SI-4, ASVS V16.1.1/V16.2.1] maps key and state changes to the singleton", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, clientInterceptorNone())
		secretRequests := r.auditHashKeySecretToKubernaut(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: resources.DataStorageAuditHashKeySecretName, Namespace: kn.Namespace},
		})
		Expect(secretRequests).To(ConsistOf(reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}))
		stateRequests := r.auditHashKeyStateToKubernaut(context.Background(), &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: resources.DataStorageAuditHashKeyStateConfigMapName, Namespace: kn.Namespace},
		})
		Expect(stateRequests).To(ConsistOf(reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}))
	})

	It("UT-AUDIT-209-CTRL-007 [SI-10] fails on state ownership, read, and identity conflicts", func() {
		kn := newMinimalCR()
		foreignState := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name:      resources.DataStorageAuditHashKeyStateConfigMapName,
			Namespace: kn.Namespace,
		}, Data: map[string]string{"secretName": resources.DataStorageAuditHashKeySecretName, "secretKey": "hmacKey", "source": resources.DataStorageAuditHashKeySourceGenerated}}
		r := businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.Create(context.Background(), foreignState)).To(Succeed())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("ownership conflict")))

		r = businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if _, ok := object.(*corev1.ConfigMap); ok && key.Name == resources.DataStorageAuditHashKeyStateConfigMapName {
				return errors.New("state read unavailable")
			}
			return api.Get(ctx, key, object, options...)
		}})
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("reading audit HMAC state")))

		kn.Spec.DataStorage.AuditHashKey = &kubernautv1alpha2.DataStorageAuditHashKeySpec{ExistingSecret: "missing-audit"}
		r = businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("not found")))

		kn.Spec.DataStorage.AuditHashKey = nil
		r = businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.CreateOption) error {
			if _, ok := object.(*corev1.Secret); ok && object.GetName() == resources.DataStorageAuditHashKeySecretName {
				return errors.New("secret create denied")
			}
			return api.Create(ctx, object, options...)
		}})
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("creating generated audit HMAC secret")))

		state := resources.DataStorageAuditHashKeyStateConfigMap(kn, resources.DataStorageAuditHashKeyReferenceFor(kn))
		state.Data["secretName"] = "different"
		Expect(validateAuditHashKeyState(state, resources.DataStorageAuditHashKeyReferenceFor(kn))).To(MatchError(ContainSubstring("identity cannot change")))
		state = resources.DataStorageAuditHashKeyStateConfigMap(kn, resources.DataStorageAuditHashKeyReferenceFor(kn))
		state.Data["source"] = resources.DataStorageAuditHashKeySourceExisting
		Expect(validateAuditHashKeyState(state, resources.DataStorageAuditHashKeyReferenceFor(kn))).To(MatchError(ContainSubstring("source cannot change")))
		state = resources.DataStorageAuditHashKeyStateConfigMap(kn, resources.DataStorageAuditHashKeyReferenceFor(kn))
		state.Data[resources.DataStorageAuditHashKeyStatePhaseKey] = resources.DataStorageAuditHashKeyStateProvisioning
		state.Data["keyFingerprint"] = "pending"
		Expect(validateAuditHashKeyState(state, resources.DataStorageAuditHashKeyReferenceFor(kn))).To(MatchError(ContainSubstring("not committed")))
		Expect(validateAuditHashKeyState(&corev1.ConfigMap{}, resources.DataStorageAuditHashKeyReferenceFor(kn))).To(MatchError(ContainSubstring("not protected")))
	})

	It("UT-AUDIT-209-CTRL-008 [SI-10, ASVS V13.3.1] handles generated Secret markers and read failures", func() {
		kn := newMinimalCR()
		ref := resources.DataStorageAuditHashKeyReferenceFor(kn)
		invalid, err := resources.NewDataStorageAuditHashKeySecret(kn, ref)
		Expect(err).NotTo(HaveOccurred())
		invalid.Annotations = nil
		r := businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.Create(context.Background(), invalid)).To(Succeed())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("already exists before its state marker")))
		state := resources.DataStorageAuditHashKeyStateConfigMap(kn, ref)
		fingerprint, err := resources.DataStorageAuditHashKeyFingerprint(invalid, ref.SecretKey)
		Expect(err).NotTo(HaveOccurred())
		state.Data["keyFingerprint"] = fingerprint
		resources.StampOwnership(kn, state)
		Expect(r.Create(context.Background(), state)).To(Succeed())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("not an operator-generated secret")))

		r = businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if _, ok := object.(*corev1.Secret); ok && key.Name == ref.SecretName {
				return errors.New("secret read unavailable")
			}
			return api.Get(ctx, key, object, options...)
		}})
		Expect(r.validateEffectiveAuditHashKeySecret(context.Background(), kn, ref, "")).To(MatchError(ContainSubstring("reading audit HMAC secret")))
		Expect(operatorGeneratedAuditHashKeySecret(nil)).To(BeFalse())
		Expect(operatorGeneratedAuditHashKeySecret(&corev1.Secret{})).To(BeFalse())
		immutable := true
		Expect(operatorGeneratedAuditHashKeySecret(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				resources.DataStorageAuditHashKeyManagedAnnotation: "true",
				resources.DataStorageAuditHashKeySourceAnnotation:  resources.DataStorageAuditHashKeySourceGenerated,
			}},
			Immutable: &immutable,
		})).To(BeFalse())
	})

	It("UT-AUDIT-209-CTRL-009 [SI-10] propagates generated-key and Secret read failures", func() {
		kn := newMinimalCR()
		original := newGeneratedAuditHashKeySecret
		DeferCleanup(func() { newGeneratedAuditHashKeySecret = original })
		newGeneratedAuditHashKeySecret = func(*kubernautv1alpha2.Kubernaut, resources.DataStorageAuditHashKeyReference) (*corev1.Secret, error) {
			return nil, errors.New("generator unavailable")
		}
		r := businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("generator unavailable")))

		newGeneratedAuditHashKeySecret = original
		r = businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if _, ok := object.(*corev1.Secret); ok && key.Name == resources.DataStorageAuditHashKeySecretName {
				return errors.New("secret read unavailable")
			}
			return api.Get(ctx, key, object, options...)
		}})
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("reading audit HMAC secret")))
		ref := resources.DataStorageAuditHashKeyReferenceFor(kn)
		_, err := r.createOrGetAuditHashKeySecret(context.Background(), kn, ref)
		Expect(err).To(MatchError(ContainSubstring("reading audit HMAC secret")))
	})

	It("UT-AUDIT-209-CTRL-010 [SI-4] handles ignored watch objects and list failures", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.auditHashKeyStateToKubernaut(context.Background(), &corev1.Secret{})).To(BeNil())
		Expect(r.auditHashKeyStateToKubernaut(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: kn.Namespace}})).To(BeNil())
		Expect(r.auditHashKeySecretToKubernaut(context.Background(), &corev1.ConfigMap{})).To(BeNil())
		Expect(r.auditHashKeySecretToKubernaut(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: kn.Namespace}})).To(BeEmpty())
		r = businessUnitReconciler(kn, interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return errors.New("list unavailable")
		}})
		Expect(r.auditHashKeySecretToKubernaut(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: resources.DataStorageAuditHashKeySecretName, Namespace: kn.Namespace}})).To(BeNil())
		r.APIReader = nil
		Expect(r.auditHashKeySecretToKubernaut(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: resources.DataStorageAuditHashKeySecretName, Namespace: kn.Namespace}})).To(BeNil())
	})

	It("UT-AUDIT-209-CTRL-011 [AC-6, AU-9] rejects a pre-existing default Secret before initialization", func() {
		kn := newMinimalCR()
		ref := resources.DataStorageAuditHashKeyReferenceFor(kn)
		existing, err := resources.NewDataStorageAuditHashKeySecret(kn, ref)
		Expect(err).NotTo(HaveOccurred())
		r := businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.Create(context.Background(), existing)).To(Succeed())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("already exists before its state marker")))
		r = businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if _, ok := object.(*corev1.Secret); ok && key.Name == ref.SecretName {
				return errors.New("default Secret read unavailable")
			}
			return api.Get(ctx, key, object, options...)
		}})
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("reading audit HMAC secret")))
	})

	It("UT-AUDIT-209-CTRL-013 [AU-9, CC8.1] rejects fingerprint drift, invalid initialized material, and fingerprint errors", func() {
		kn := newMinimalCR()
		ref := resources.DataStorageAuditHashKeyReferenceFor(kn)
		secret, err := resources.NewDataStorageAuditHashKeySecret(kn, ref)
		Expect(err).NotTo(HaveOccurred())
		fingerprint, err := resources.DataStorageAuditHashKeyFingerprint(secret, ref.SecretKey)
		Expect(err).NotTo(HaveOccurred())
		state := resources.DataStorageAuditHashKeyStateConfigMap(kn, ref)
		state.Data["keyFingerprint"] = fingerprint
		r := businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.Create(context.Background(), secret)).To(Succeed())
		resources.StampOwnership(kn, state)
		Expect(r.Create(context.Background(), state)).To(Succeed())
		secret.Data[resources.DataStorageAuditHashKeySecretDataKey] = []byte("hmacKey: " + strings.Repeat("z", 32))
		Expect(r.Update(context.Background(), secret)).To(Succeed())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("fingerprint changed")))

		bad := resources.DataStorageAuditHashKeyStateConfigMap(kn, ref)
		bad.Data["keyFingerprint"] = "known"
		badSecret, err := resources.NewDataStorageAuditHashKeySecret(kn, ref)
		Expect(err).NotTo(HaveOccurred())
		badSecret.Data[resources.DataStorageAuditHashKeySecretDataKey] = []byte("hmacKey: short")
		r = businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.Create(context.Background(), badSecret)).To(Succeed())
		resources.StampOwnership(kn, bad)
		Expect(r.Create(context.Background(), bad)).To(Succeed())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(HaveOccurred())

		missingFingerprint := resources.DataStorageAuditHashKeyStateConfigMap(kn, ref)
		goodSecret, err := resources.NewDataStorageAuditHashKeySecret(kn, ref)
		Expect(err).NotTo(HaveOccurred())
		r = businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.Create(context.Background(), goodSecret)).To(Succeed())
		resources.StampOwnership(kn, missingFingerprint)
		Expect(r.Create(context.Background(), missingFingerprint)).To(Succeed())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("fingerprint is missing")))

		originalFingerprint := dataStorageAuditHashKeyFingerprint
		DeferCleanup(func() { dataStorageAuditHashKeyFingerprint = originalFingerprint })
		dataStorageAuditHashKeyFingerprint = func(*corev1.Secret, string) (string, error) {
			return "", errors.New("fingerprint unavailable")
		}
		validated := newMinimalCR()
		validatedRef := resources.DataStorageAuditHashKeyReferenceFor(validated)
		validatedSecret, err := resources.NewDataStorageAuditHashKeySecret(validated, validatedRef)
		Expect(err).NotTo(HaveOccurred())
		validatedFingerprint, err := resources.DataStorageAuditHashKeyFingerprint(validatedSecret, validatedRef.SecretKey)
		Expect(err).NotTo(HaveOccurred())
		validatedState := resources.DataStorageAuditHashKeyStateConfigMap(validated, validatedRef)
		validatedState.Data["keyFingerprint"] = validatedFingerprint
		validatedReconciler := businessUnitReconciler(validated, clientInterceptorNone())
		Expect(validatedReconciler.Create(context.Background(), validatedSecret)).To(Succeed())
		resources.StampOwnership(validated, validatedState)
		Expect(validatedReconciler.Create(context.Background(), validatedState)).To(Succeed())
		Expect(validatedReconciler.ensureDataStorageAuditHashKey(context.Background(), validated)).To(MatchError(ContainSubstring("fingerprint unavailable")))

		fresh := newMinimalCR()
		freshReconciler := businessUnitReconciler(fresh, clientInterceptorNone())
		Expect(freshReconciler.ensureDataStorageAuditHashKey(context.Background(), fresh)).To(MatchError(ContainSubstring("fingerprint unavailable")))
	})

	It("UT-AUDIT-209-CTRL-014 [AU-9, CP-10] resumes after state-marker creation failure", func() {
		kn := newMinimalCR()
		failed := businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.CreateOption) error {
			if _, ok := object.(*corev1.ConfigMap); ok && object.GetName() == resources.DataStorageAuditHashKeyStateConfigMapName {
				return errors.New("state marker unavailable")
			}
			return api.Create(ctx, object, options...)
		}})
		Expect(failed.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("state marker unavailable")))
		retry := businessUnitReconciler(kn, clientInterceptorNone())
		Expect(retry.ensureDataStorageAuditHashKey(context.Background(), kn)).To(Succeed())
	})

	It("UT-AUDIT-209-CTRL-015 [AU-9, CP-10] resumes an existing provisioning marker", func() {
		kn := newMinimalCR()
		ref := resources.DataStorageAuditHashKeyReferenceFor(kn)
		state := resources.DataStorageAuditHashKeyStateConfigMap(kn, ref)
		state.Data[resources.DataStorageAuditHashKeyStatePhaseKey] = resources.DataStorageAuditHashKeyStateProvisioning
		r := businessUnitReconciler(kn, clientInterceptorNone())
		resources.StampOwnership(kn, state)
		Expect(r.Create(context.Background(), state)).To(Succeed())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(Succeed())
		ready := &corev1.ConfigMap{}
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(state), ready)).To(Succeed())
		Expect(ready.Data).To(HaveKeyWithValue(resources.DataStorageAuditHashKeyStatePhaseKey, resources.DataStorageAuditHashKeyStateReady))
	})

	It("UT-AUDIT-209-CTRL-017 [SI-10] validates the audit Secret reference before state mutation", func() {
		kn := newMinimalCR()
		kn.Spec.DataStorage.AuditHashKey = &kubernautv1alpha2.DataStorageAuditHashKeySpec{ExistingSecret: "bad name"}
		r := businessUnitReconciler(kn, clientInterceptorNone())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("invalid")))
		state := &corev1.ConfigMap{}
		Expect(r.Get(context.Background(), client.ObjectKey{Namespace: kn.Namespace, Name: resources.DataStorageAuditHashKeyStateConfigMapName}, state)).To(HaveOccurred())
	})

	It("UT-AUDIT-209-CTRL-018 [SI-10] rejects a nil audit state identity", func() {
		Expect(validateAuditHashKeyIdentity(nil, resources.DataStorageAuditHashKeyReferenceFor(newMinimalCR()))).To(MatchError(ContainSubstring("state configmap is nil")))
	})

	It("UT-AUDIT-209-CTRL-019 [AC-6, SI-4] rejects a terminating BYO Secret", func() {
		kn := newMinimalCR()
		ref := resources.DataStorageAuditHashKeyReferenceFor(kn)
		ref.Generated = false
		ref.SecretName = "customer-audit-key"
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: ref.SecretName, Namespace: kn.Namespace, DeletionTimestamp: &metav1.Time{Time: time.Now()},
		}, Data: map[string][]byte{resources.DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + strings.Repeat("x", 32))}}
		r := businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if current, ok := object.(*corev1.Secret); ok && key.Name == ref.SecretName {
				*current = *secret.DeepCopy()
				return nil
			}
			return api.Get(ctx, key, object, options...)
		}})
		Expect(r.validateEffectiveAuditHashKeySecret(context.Background(), kn, ref, "")).To(MatchError(ContainSubstring("terminating")))
	})

	It("UT-AUDIT-209-CTRL-020 [SI-10, CP-10] permits correcting a BYO reference before commitment", func() {
		kn := newMinimalCR()
		kn.Spec.DataStorage.AuditHashKey = &kubernautv1alpha2.DataStorageAuditHashKeySpec{ExistingSecret: "old-audit-key"}
		oldRef := resources.DataStorageAuditHashKeyReferenceFor(kn)
		state := resources.DataStorageAuditHashKeyStateConfigMap(kn, oldRef)
		state.Data[resources.DataStorageAuditHashKeyStatePhaseKey] = resources.DataStorageAuditHashKeyStateProvisioning
		r := businessUnitReconciler(kn, clientInterceptorNone())
		resources.StampOwnership(kn, state)
		Expect(r.Create(context.Background(), state)).To(Succeed())
		kn.Spec.DataStorage.AuditHashKey.ExistingSecret = "new-audit-key"
		newRef := resources.DataStorageAuditHashKeyReferenceFor(kn)
		Expect(r.Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: newRef.SecretName, Namespace: kn.Namespace},
			Data:       map[string][]byte{resources.DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + strings.Repeat("n", 32))},
		})).To(Succeed())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(Succeed())
		ready := &corev1.ConfigMap{}
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(state), ready)).To(Succeed())
		Expect(ready.Data).To(HaveKeyWithValue("secretName", "new-audit-key"))
		Expect(ready.Data).To(HaveKeyWithValue(resources.DataStorageAuditHashKeyStatePhaseKey, resources.DataStorageAuditHashKeyStateReady))
	})

	It("UT-AUDIT-209-CTRL-021 [SI-4, CP-10] reports a pending-state update failure", func() {
		kn := newMinimalCR()
		kn.Spec.DataStorage.AuditHashKey = &kubernautv1alpha2.DataStorageAuditHashKeySpec{ExistingSecret: "old-audit-key"}
		oldRef := resources.DataStorageAuditHashKeyReferenceFor(kn)
		state := resources.DataStorageAuditHashKeyStateConfigMap(kn, oldRef)
		state.Data[resources.DataStorageAuditHashKeyStatePhaseKey] = resources.DataStorageAuditHashKeyStateProvisioning
		r := businessUnitReconciler(kn, clientInterceptorNone())
		resources.StampOwnership(kn, state)
		Expect(r.Create(context.Background(), state)).To(Succeed())
		kn.Spec.DataStorage.AuditHashKey.ExistingSecret = "new-audit-key"
		newRef := resources.DataStorageAuditHashKeyReferenceFor(kn)
		Expect(r.Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: newRef.SecretName, Namespace: kn.Namespace},
			Data:       map[string][]byte{resources.DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + strings.Repeat("n", 32))},
		})).To(Succeed())
		r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Update: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.UpdateOption) error {
			if _, ok := object.(*corev1.ConfigMap); ok && object.GetName() == resources.DataStorageAuditHashKeyStateConfigMapName {
				return errors.New("pending state update unavailable")
			}
			return api.Update(ctx, object, options...)
		}})
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("pending state update unavailable")))
	})

	It("UT-AUDIT-209-CTRL-016 [AC-6, AU-9] rejects a foreign Secret during provisioning recovery", func() {
		kn := newMinimalCR()
		ref := resources.DataStorageAuditHashKeyReferenceFor(kn)
		state := resources.DataStorageAuditHashKeyStateConfigMap(kn, ref)
		state.Data[resources.DataStorageAuditHashKeyStatePhaseKey] = resources.DataStorageAuditHashKeyStateProvisioning
		foreign := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: ref.SecretName, Namespace: kn.Namespace},
			Data: map[string][]byte{
				resources.DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + strings.Repeat("f", 32)),
			},
		}
		r := businessUnitReconciler(kn, clientInterceptorNone())
		resources.StampOwnership(kn, state)
		Expect(r.Create(context.Background(), state)).To(Succeed())
		Expect(r.Create(context.Background(), foreign)).To(Succeed())
		Expect(r.ensureDataStorageAuditHashKey(context.Background(), kn)).To(MatchError(ContainSubstring("not an operator-generated secret")))
	})
})

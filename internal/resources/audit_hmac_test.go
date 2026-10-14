package resources

import (
	"errors"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("DataStorage audit HMAC resources", Label("unit"), func() {
	It("UT-AUDIT-209-RES-001 [AU-9, ASVS V11.1.1] creates a 256-bit immutable generated Secret", func() {
		kn := testKubernaut()
		ref := DataStorageAuditHashKeyReferenceFor(kn)
		secret, err := NewDataStorageAuditHashKeySecret(kn, ref)
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.Immutable).NotTo(BeNil())
		Expect(*secret.Immutable).To(BeTrue())
		Expect(secret.Data[DataStorageAuditHashKeySecretDataKey]).To(ContainSubstring("hmacKey:"))
		Expect(ValidateDataStorageAuditHashKeySecret(secret, ref.SecretKey)).To(Succeed())
	})

	It("UT-AUDIT-209-RES-002 [AC-6, IA-5] rejects generating an administrator-managed Secret", func() {
		kn := testKubernaut()
		ref := DataStorageAuditHashKeyReferenceFor(kn)
		ref.Generated = false
		_, err := NewDataStorageAuditHashKeySecret(kn, ref)
		Expect(err).To(MatchError(ContainSubstring("administrator-managed")))
	})

	It("UT-AUDIT-209-RES-007 [SI-10] rejects an invalid generated key name", func() {
		kn := testKubernaut()
		ref := DataStorageAuditHashKeyReferenceFor(kn)
		ref.SecretKey = "bad\nkey"
		_, err := NewDataStorageAuditHashKeySecret(kn, ref)
		Expect(err).To(MatchError(ContainSubstring("unsupported characters")))
	})

	It("UT-AUDIT-209-RES-003 [SI-10] reports random-source failure without creating key material", func() {
		original := auditHashKeyRandomRead
		DeferCleanup(func() { auditHashKeyRandomRead = original })
		auditHashKeyRandomRead = func([]byte) (int, error) { return 0, errors.New("random source unavailable") }

		_, err := NewDataStorageAuditHashKeySecret(testKubernaut(), DataStorageAuditHashKeyReferenceFor(testKubernaut()))
		Expect(err).To(MatchError(ContainSubstring("generating audit HMAC key")))
	})

	DescribeTable("UT-AUDIT-209-RES-004 [SI-10, ASVS V16.5.2] rejects invalid Secret material",
		func(data map[string][]byte, key string) {
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "audit"}, Data: data}
			Expect(ValidateDataStorageAuditHashKeySecret(secret, key)).To(HaveOccurred())
		},
		Entry("nil Secret", nil, "hmacKey"),
		Entry("missing projected key", map[string][]byte{}, "hmacKey"),
		Entry("invalid YAML", map[string][]byte{DataStorageAuditHashKeySecretDataKey: []byte("[")}, "hmacKey"),
		Entry("missing inner key", map[string][]byte{DataStorageAuditHashKeySecretDataKey: []byte("other: value")}, "hmacKey"),
		Entry("weak inner key", map[string][]byte{DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: short")}, "hmacKey"),
		Entry("unsupported inner key name", map[string][]byte{DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + strings.Repeat("x", 32))}, "bad\nkey"),
		Entry("empty inner key name", map[string][]byte{DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: " + strings.Repeat("x", 32))}, ""),
		Entry("leading or trailing whitespace", map[string][]byte{DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: '" + strings.Repeat("x", 32) + " '")}, "hmacKey"),
	)

	It("UT-AUDIT-209-RES-005 [SI-10] accepts JSON secret files and configured inner keys", func() {
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "audit"}, Data: map[string][]byte{
			DataStorageAuditHashKeySecretDataKey: []byte(`{"custom": "` + strings.Repeat("x", 32) + `"}`),
		}}
		Expect(ValidateDataStorageAuditHashKeySecret(secret, "custom")).To(Succeed())
	})

	It("UT-AUDIT-209-RES-006 [SI-10] rejects a nil Secret object", func() {
		Expect(ValidateDataStorageAuditHashKeySecret(nil, "hmacKey")).To(MatchError(ContainSubstring("secret is nil")))
	})

	It("UT-AUDIT-209-RES-008 [AU-9] renders protected state markers for both sources", func() {
		kn := testKubernaut()
		generated := DataStorageAuditHashKeyStateConfigMap(kn, DataStorageAuditHashKeyReferenceFor(kn))
		Expect(generated.Finalizers).To(ContainElement(DataStorageAuditHashKeyFinalizer))
		byo := DataStorageAuditHashKeyReference{SecretName: "customer-audit", SecretKey: "hmacKey"}
		Expect(DataStorageAuditHashKeyStateConfigMap(kn, byo).Data).To(HaveKeyWithValue("source", DataStorageAuditHashKeySourceExisting))
	})

	It("UT-AUDIT-209-RES-009 [AU-9] does not fingerprint invalid key material", func() {
		_, err := DataStorageAuditHashKeyFingerprint(nil, "hmacKey")
		Expect(err).To(HaveOccurred())
	})

	It("UT-AUDIT-209-RES-010 [AU-9, ASVS v5.0.0-V11.1.1] uses the independently known SHA-256 fingerprint", func() {
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "audit"}, Data: map[string][]byte{
			DataStorageAuditHashKeySecretDataKey: []byte("hmacKey: 0123456789abcdef0123456789abcdef\n"),
		}}
		fingerprint, err := DataStorageAuditHashKeyFingerprint(secret, "hmacKey")
		Expect(err).NotTo(HaveOccurred())
		Expect(fingerprint).To(Equal("3eb1bd439947eb762998e566ccc2e099c791118b2f40579cc4f7da2b5061b7f9"))
	})

	It("UT-AUDIT-209-RES-011 [SI-10] rejects an invalid Kubernetes Secret name", func() {
		ref := DataStorageAuditHashKeyReference{SecretName: "bad name", SecretKey: DataStorageAuditHashKeyDefault, Generated: true}
		Expect(ValidateDataStorageAuditHashKeyReference(ref)).To(MatchError(ContainSubstring("invalid")))
	})
})

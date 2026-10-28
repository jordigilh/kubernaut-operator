package resources

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Issue 587 BYO dependency boundary", Label("unit"), func() {
	It("UT-587-BUILD-001 [AC-6; SOC2 CC6.6; ASVS v5.0.0-V8.3.1, V13.3.1] projects PostgreSQL and Valkey directly from user-owned Secrets", func() {
		kn := testKubernaut()
		deployment, err := DataStorageDeployment(kn)
		Expect(err).NotTo(HaveOccurred())

		var projectedSecretNames []string
		for _, volume := range deployment.Spec.Template.Spec.Volumes {
			if volume.Projected == nil {
				continue
			}
			for _, source := range volume.Projected.Sources {
				if source.Secret != nil {
					projectedSecretNames = append(projectedSecretNames, source.Secret.Name)
				}
			}
		}

		Expect(projectedSecretNames).To(ContainElement(kn.Spec.PostgreSQL.SecretName))
		Expect(projectedSecretNames).To(ContainElement(kn.Spec.Valkey.SecretName))
		Expect(projectedSecretNames).NotTo(ContainElement("datastorage-db-secret"))
	})
})

var _ = Describe("Issue 587 credential contracts", Label("unit"), func() {
	It("UT-587-CRED-004 [IA-5, SI-10; SOC2 CC7.2; ASVS v5.0.0-V13.3.1, V13.3.2] accepts matching PostgreSQL and Valkey documents", func() {
		postgres := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret"}, Data: map[string][]byte{
			"POSTGRES_USER":                []byte("user"),
			"POSTGRES_PASSWORD":            []byte("password"),
			"POSTGRES_DB":                  []byte("database"),
			PostgreSQLDataStorageSecretKey: []byte("username: user\npassword: password\n"),
		}}
		valkey := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "valkey-secret"}, Data: map[string][]byte{
			"valkey-secrets.yaml": []byte("password: password\n"),
		}}
		Expect(ValidatePostgreSQLSecret(postgres)).To(Succeed())
		Expect(ValidateValkeySecret(valkey)).To(Succeed())
	})

	DescribeTable("UT-587-CRED-005 [IA-5, SI-10; SOC2 CC7.2; ASVS v5.0.0-V2.2.1, V13.3.2] rejects invalid PostgreSQL credential documents",
		func(secret *corev1.Secret, expected string) {
			Expect(ValidatePostgreSQLSecret(secret)).To(MatchError(ContainSubstring(expected)))
		},
		Entry("nil secret", (*corev1.Secret)(nil), "secret is nil"),
		Entry("missing user", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret"}, Data: map[string][]byte{
			"POSTGRES_PASSWORD": []byte("password"), "POSTGRES_DB": []byte("database"), PostgreSQLDataStorageSecretKey: []byte("username: user\npassword: password\n"),
		}}, "POSTGRES_USER"),
		Entry("empty password", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret"}, Data: map[string][]byte{
			"POSTGRES_USER": []byte("user"), "POSTGRES_PASSWORD": []byte(" "), "POSTGRES_DB": []byte("database"), PostgreSQLDataStorageSecretKey: []byte("username: user\npassword: password\n"),
		}}, "POSTGRES_PASSWORD"),
		Entry("missing database", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret"}, Data: map[string][]byte{
			"POSTGRES_USER": []byte("user"), "POSTGRES_PASSWORD": []byte("password"), PostgreSQLDataStorageSecretKey: []byte("username: user\npassword: password\n"),
		}}, "POSTGRES_DB"),
		Entry("missing runtime document", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret"}, Data: map[string][]byte{
			"POSTGRES_USER": []byte("user"), "POSTGRES_PASSWORD": []byte("password"), "POSTGRES_DB": []byte("database"),
		}}, PostgreSQLDataStorageSecretKey),
		Entry("malformed runtime document", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret"}, Data: map[string][]byte{
			"POSTGRES_USER": []byte("user"), "POSTGRES_PASSWORD": []byte("password"), "POSTGRES_DB": []byte("database"), PostgreSQLDataStorageSecretKey: []byte("["),
		}}, "invalid credential YAML"),
		Entry("missing username", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret"}, Data: map[string][]byte{
			"POSTGRES_USER": []byte("user"), "POSTGRES_PASSWORD": []byte("password"), "POSTGRES_DB": []byte("database"), PostgreSQLDataStorageSecretKey: []byte("password: password\n"),
		}}, "non-empty username"),
		Entry("missing password", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret"}, Data: map[string][]byte{
			"POSTGRES_USER": []byte("user"), "POSTGRES_PASSWORD": []byte("password"), "POSTGRES_DB": []byte("database"), PostgreSQLDataStorageSecretKey: []byte("username: user\n"),
		}}, "non-empty password"),
		Entry("username mismatch", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret"}, Data: map[string][]byte{
			"POSTGRES_USER": []byte("user"), "POSTGRES_PASSWORD": []byte("password"), "POSTGRES_DB": []byte("database"), PostgreSQLDataStorageSecretKey: []byte("username: other\npassword: password\n"),
		}}, "inconsistent database username"),
		Entry("password mismatch", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "postgresql-secret"}, Data: map[string][]byte{
			"POSTGRES_USER": []byte("user"), "POSTGRES_PASSWORD": []byte("password"), "POSTGRES_DB": []byte("database"), PostgreSQLDataStorageSecretKey: []byte("username: user\npassword: other\n"),
		}}, "inconsistent database password"),
	)

	DescribeTable("UT-587-CRED-006 [IA-5, SI-10; SOC2 CC7.2; ASVS v5.0.0-V13.3.1, V13.3.2] rejects invalid Valkey credential documents",
		func(secret *corev1.Secret, expected string) {
			Expect(ValidateValkeySecret(secret)).To(MatchError(ContainSubstring(expected)))
		},
		Entry("nil secret", (*corev1.Secret)(nil), "secret is nil"),
		Entry("missing key", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "valkey-secret"}}, "valkey-secrets.yaml"),
		Entry("malformed document", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "valkey-secret"}, Data: map[string][]byte{"valkey-secrets.yaml": []byte("[")}}, "invalid credential YAML"),
		Entry("empty password", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "valkey-secret"}, Data: map[string][]byte{"valkey-secrets.yaml": []byte("password: \"\"\n")}}, "non-empty password"),
	)
})

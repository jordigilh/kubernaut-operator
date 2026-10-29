package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("Issue 587 BYO and migration boundaries", Label("unit"), func() {
	It("UT-587-CRED-001 [AC-6, IA-5, SI-10; SOC2 CC6.6, CC8.1; ASVS v5.0.0-V13.3.1, V13.3.2] accepts a canonical PostgreSQL Secret contract", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})

		_, err, handled := r.validateDatabaseInputs(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeFalse())
	})

	It("UT-587-CRED-002 [AC-6, IA-5, SI-10; SOC2 CC6.6, CC7.2; ASVS v5.0.0-V13.3.1, V13.3.2] rejects a PostgreSQL Secret without the DataStorage credential document", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		secret := &corev1.Secret{}
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: kn.Spec.PostgreSQL.SecretName}, secret)).To(Succeed())
		delete(secret.Data, resources.PostgreSQLDataStorageSecretKey)
		Expect(r.Update(ctx, secret)).To(Succeed())

		result, err, handled := r.validateDatabaseInputs(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeTrue())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		Expect(meta.IsStatusConditionFalse(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)).To(BeTrue())
	})

	It("UT-587-CRED-003 [IA-5, SI-10; SOC2 CC7.2; ASVS v5.0.0-V2.2.1, V16.5.2] rejects malformed Valkey credential content", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		secret := &corev1.Secret{}
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: kn.Spec.Valkey.SecretName}, secret)).To(Succeed())
		secret.Data["valkey-secrets.yaml"] = []byte("password: [malformed")
		Expect(r.Update(ctx, secret)).To(Succeed())

		result, err, handled := r.validateDatabaseInputs(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeTrue())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		Expect(meta.IsStatusConditionFalse(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)).To(BeTrue())
	})

	It("UT-587-BYO-001 [AC-3, AC-6; SOC2 CC6.1, CC6.6; ASVS v5.0.0-V8.3.1, V13.3.1] does not create an operator-owned database credential Secret", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		secret := &corev1.Secret{}
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: kn.Spec.PostgreSQL.SecretName}, secret)).To(Succeed())
		secret.Data["db-secrets.yaml"] = []byte("username: kubernaut\npassword: unit-secret\n")
		Expect(r.Update(ctx, secret)).To(Succeed())

		Expect(r.ensureMigrationPrereqs(ctx, kn)).To(Succeed())
		derived := &corev1.Secret{}
		Expect(apierrors.IsNotFound(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: "datastorage-db-secret"}, derived))).To(BeTrue())
	})

	It("UT-587-MIG-001 [CM-3, SI-4; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V16.2.1, V16.5.2] preserves a terminal failed migration Job for the current dependency revision", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		job, err := resources.MigrationJob(kn)
		Expect(err).NotTo(HaveOccurred())
		credentialRevision, err := r.postgresqlCredentialRevision(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		job.Annotations = map[string]string{resources.AnnotationPostgreSQLCredentialRevision: credentialRevision}
		job.Annotations[resources.AnnotationSpecHash] = resources.SpecHash(job)
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "database authentication failed"}}
		Expect(r.Create(ctx, job)).To(Succeed())
		Expect(r.Status().Update(ctx, job)).To(Succeed())

		result, err := r.ensureMigrationJob(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		live := &batchv1.Job{}
		Expect(r.Get(ctx, types.NamespacedName{Namespace: kn.Namespace, Name: job.Name}, live)).To(Succeed())
	})

	It("UT-587-MIG-002 [CM-3, SI-4; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V16.2.1, V16.5.2] retains a failed Job when only its spec changes", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		job, err := resources.MigrationJob(kn)
		Expect(err).NotTo(HaveOccurred())
		credentialRevision, err := r.postgresqlCredentialRevision(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		job.Annotations = map[string]string{
			resources.AnnotationPostgreSQLCredentialRevision: credentialRevision,
			resources.AnnotationSpecHash:                     "old-revision",
		}
		job.Spec.Template.Spec.Containers[0].Image = "quay.io/example/old-db-migrate:fixture"
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
		Expect(r.Create(ctx, job)).To(Succeed())
		Expect(r.Status().Update(ctx, job)).To(Succeed())

		result, err := r.ensureMigrationJob(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueError))
		live := &batchv1.Job{}
		Expect(r.Get(ctx, types.NamespacedName{Namespace: kn.Namespace, Name: job.Name}, live)).To(Succeed())
		Expect(live.Spec.Template.Spec.Containers[0].Image).To(Equal("quay.io/example/old-db-migrate:fixture"))
	})

	It("UT-587-MIG-003 [CM-3, SI-4; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V16.2.1, V16.5.2] retries only a failed migration Job with a stale dependency revision", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		job, err := resources.MigrationJob(kn)
		Expect(err).NotTo(HaveOccurred())
		job.Annotations = map[string]string{
			resources.AnnotationPostgreSQLCredentialRevision: "old-credential-revision",
			resources.AnnotationSpecHash:                     "old-revision",
		}
		desiredJob, err := resources.MigrationJob(kn)
		Expect(err).NotTo(HaveOccurred())
		currentCredentialRevision, err := r.postgresqlCredentialRevision(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		desiredJob.Annotations = map[string]string{
			resources.AnnotationPostgreSQLCredentialRevision: currentCredentialRevision,
		}
		kn.Status.LastMigrationHash = resources.SpecHash(desiredJob)
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
		Expect(r.Create(ctx, job)).To(Succeed())
		Expect(r.Status().Update(ctx, job)).To(Succeed())

		result, err := r.ensureMigrationJob(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Millisecond))
		Expect(apierrors.IsNotFound(r.Get(ctx, types.NamespacedName{Namespace: kn.Namespace, Name: job.Name}, &batchv1.Job{}))).To(BeTrue())
	})

	It("UT-587-MIG-004 [IA-5, SI-4; SOC2 CC7.2; ASVS v5.0.0-V16.2.1] fails migration reconciliation when the PostgreSQL Secret cannot be read", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if key.Name == kn.Spec.PostgreSQL.SecretName {
				return fmt.Errorf("postgresql secret read denied")
			}
			return api.Get(ctx, key, object, options...)
		}})
		_, err := r.ensureMigrationJob(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("reading PostgreSQL Secret")))
	})

	It("UT-587-MIG-005 [CM-3, SI-4; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V16.2.1, V16.5.2] skips a running migration Job after a previously successful hash", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		job, err := resources.MigrationJob(kn)
		Expect(err).NotTo(HaveOccurred())
		credentialRevision, err := r.postgresqlCredentialRevision(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		job.Annotations = map[string]string{resources.AnnotationPostgreSQLCredentialRevision: credentialRevision}
		desiredHash := resources.SpecHash(job)
		job.Annotations[resources.AnnotationSpecHash] = desiredHash
		Expect(r.Create(ctx, job)).To(Succeed())
		kn.Status.LastMigrationHash = desiredHash

		result, err := r.ensureMigrationJob(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	It("UT-587-REV-002 [CM-3, SI-4; SOC2 CC7.2, CC8.1; ASVS v5.0.0-V16.5.2] fails DataStorage rollout when Valkey revision metadata cannot be read", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if key.Name == kn.Spec.Valkey.SecretName {
				return fmt.Errorf("valkey secret read denied")
			}
			return api.Get(ctx, key, object, options...)
		}})
		Expect(r.phaseDeploy(ctx, kn, kn)).To(MatchError(ContainSubstring("resolving DataStorage dependency revision")))
	})

	It("UT-587-DEP-001 [CM-3, SI-4] blocks deployment when the migration hash is stale for the current spec", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		job, err := resources.MigrationJob(kn)
		Expect(err).NotTo(HaveOccurred())
		credentialRevision, err := r.postgresqlCredentialRevision(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		job.Annotations = map[string]string{resources.AnnotationPostgreSQLCredentialRevision: credentialRevision}
		currentHash := resources.SpecHash(job)
		kn.Status.Phase = kubernautv1alpha2.PhaseRunning
		kn.Status.LastMigrationHash = currentHash
		result, err := r.migrationDeploymentGate(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		kn.Spec.PostgreSQL.SecretName = "postgresql-mismatch"
		Expect(r.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: "postgresql-mismatch", Namespace: kn.Namespace,
		}})).To(Succeed())
		result, err = r.migrationDeploymentGate(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueMigrationPoll))

		readDenied := businessUnitReconciler(kn, interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch,
			key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if key.Name == kn.Spec.PostgreSQL.SecretName {
				return fmt.Errorf("postgresql secret read denied")
			}
			return api.Get(ctx, key, object, options...)
		}})
		_, err = readDenied.migrationDeploymentGate(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("migration credential revision")))
		Expect(readDenied.ensureMigrationReadyForDeployment(ctx, kn)).To(MatchError(ContainSubstring("migration credential revision")))

		kn.Status.Phase = kubernautv1alpha2.PhaseMigrating
		result, err = r.migrationDeploymentGate(ctx, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueMigrationPoll))
		Expect(r.phaseDeploy(ctx, kn, kn)).To(MatchError("database migration is not complete for the current dependency revision"))
		kn.Status.Phase = kubernautv1alpha2.PhaseRunning
		kn.Status.LastMigrationHash = ""
		Expect(r.migrationDeploymentGate(ctx, kn)).To(Equal(ctrl.Result{}))
		kn.Status.LastMigrationHash = "stale"
		result, err = r.reconcileAfterMigration(ctx, kn, kn)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(requeueMigrationPoll))
		withoutControllerImage("RELATED_IMAGE_DB_MIGRATE")
		_, err = r.migrationDeploymentGate(ctx, kn)
		Expect(err).To(MatchError(ContainSubstring("building migration job for deployment gate")))
	})
})

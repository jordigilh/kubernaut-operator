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

package resources

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

func mustMigrationJob(kn *kubernautv1alpha1.Kubernaut) *batchv1.Job {
	job, err := MigrationJob(kn)
	Expect(err).NotTo(HaveOccurred())
	return job
}

var _ = Describe("MigrationConfigMap", func() {
	It("contains SQL migration files", func() {
		kn := testKubernaut()
		cm, err := MigrationConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())

		Expect(cm.Name).To(Equal("kubernaut-migrations"), "name = %q, want %q", cm.Name, "kubernaut-migrations")

		Expect(len(cm.Data)).To(BeNumerically(">=", 7), "migration ConfigMap should contain at least 7 SQL files (v1.3.0), got %d", len(cm.Data))

		for name, content := range cm.Data {
			Expect(strings.HasSuffix(name, ".sql")).To(BeTrue(), "migration file %q should have .sql extension", name)
			hasDDL := strings.Contains(content, "CREATE") ||
				strings.Contains(content, "ALTER") ||
				strings.Contains(content, "DROP")
			Expect(hasDDL).To(BeTrue(), "migration file %q should contain SQL DDL, got %d bytes", name, len(content))
		}
	})

	It("contains 001_v1_schema.sql", func() {
		kn := testKubernaut()
		cm, err := MigrationConfigMap(kn)
		Expect(err).NotTo(HaveOccurred())

		_, ok := cm.Data["001_v1_schema.sql"]
		Expect(ok).To(BeTrue(), "migration ConfigMap should contain 001_v1_schema.sql")
	})
})

var _ = Describe("MigrationJob", func() {
	It("has expected Job metadata and lifecycle fields", func() {
		kn := testKubernaut()
		job := mustMigrationJob(kn)

		Expect(job.Name).To(Equal("kubernaut-db-migration"), "name = %q, want %q", job.Name, "kubernaut-db-migration")
		Expect(job.Namespace).To(Equal(testSystemNamespace), "namespace = %q, want %q", job.Namespace, testSystemNamespace)

		Expect(job.Spec.BackoffLimit).NotTo(BeNil())
		Expect(*job.Spec.BackoffLimit).To(Equal(int32(3)), "Job should have backoffLimit=3")
		Expect(job.Spec.TTLSecondsAfterFinished).NotTo(BeNil())
		Expect(*job.Spec.TTLSecondsAfterFinished).To(Equal(int32(300)), "Job should have TTLSecondsAfterFinished=300")
	})

	It("configures the db-migrate container", func() {
		kn := testKubernaut()
		job := mustMigrationJob(kn)

		Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1), "Job should have 1 container, got %d", len(job.Spec.Template.Spec.Containers))

		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.Name).To(Equal("db-migrate"), "container name = %q, want %q", container.Name, "db-migrate")

		wantImage := "quay.io/kubernaut-ai/db-migrate:v1.3.0"
		Expect(container.Image).To(Equal(wantImage), "image = %q, want %q", container.Image, wantImage)

		Expect(len(container.Command)).To(BeNumerically(">", 0))
		Expect(container.Command[0]).To(Equal("goose"), "command should start with goose, got %v", container.Command)

		Expect(container.Resources.Requests).NotTo(BeNil(), "migration container should have resource requests")
	})

	It("loads env from the PostgreSQL secret", func() {
		kn := testKubernaut()
		job := mustMigrationJob(kn)

		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.EnvFrom).NotTo(BeEmpty(), "container should have EnvFrom for PG secret")

		ref := container.EnvFrom[0].SecretRef
		Expect(ref).NotTo(BeNil(), "EnvFrom should reference postgresql-secret, got %v", ref)
		Expect(ref.Name).To(Equal("postgresql-secret"), "EnvFrom should reference postgresql-secret, got %v", ref)
	})

	It("mounts the migrations ConfigMap", func() {
		kn := testKubernaut()
		job := mustMigrationJob(kn)

		container := job.Spec.Template.Spec.Containers[0]
		found := false
		for _, vm := range container.VolumeMounts {
			if vm.Name == "migrations" && vm.MountPath == "/migrations" {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "container should mount migrations volume at /migrations")

		volFound := false
		for _, v := range job.Spec.Template.Spec.Volumes {
			if v.Name == "migrations" && v.ConfigMap != nil && v.ConfigMap.Name == "kubernaut-migrations" {
				volFound = true
			}
		}
		Expect(volFound).To(BeTrue(), "Job should have a migrations volume backed by kubernaut-migrations ConfigMap")
	})

	It("sets security contexts", func() {
		kn := testKubernaut()
		job := mustMigrationJob(kn)

		psc := job.Spec.Template.Spec.SecurityContext
		Expect(psc).NotTo(BeNil())
		Expect(psc.RunAsNonRoot).NotTo(BeNil())
		Expect(*psc.RunAsNonRoot).To(BeTrue(), "Job pod should have RunAsNonRoot=true")

		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.SecurityContext).NotTo(BeNil(), "Job container should have security context")
	})

	It("runs goose with the PostgreSQL host and port", func() {
		kn := testKubernaut()
		job := mustMigrationJob(kn)

		container := job.Spec.Template.Spec.Containers[0]
		cmdStr := strings.Join(container.Command, " ")
		Expect(cmdStr).To(ContainSubstring("pg.example.com"), "goose command should reference PG host, got:\n%s", cmdStr)
		Expect(cmdStr).To(ContainSubstring("5432"), "goose command should reference PG port, got:\n%s", cmdStr)
	})
})

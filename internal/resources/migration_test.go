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
	"testing"
)

func TestMigrationConfigMap_ContainsSQL(t *testing.T) {
	kn := testKubernaut()
	cm, err := MigrationConfigMap(kn)
	if err != nil {
		t.Fatalf("MigrationConfigMap() error: %v", err)
	}

	if cm.Name != "kubernaut-migrations" {
		t.Errorf("name = %q, want %q", cm.Name, "kubernaut-migrations")
	}

	if len(cm.Data) == 0 {
		t.Fatal("migration ConfigMap should contain at least one SQL file")
	}

	for name, content := range cm.Data {
		if !strings.HasSuffix(name, ".sql") {
			t.Errorf("migration file %q should have .sql extension", name)
		}
		if !strings.Contains(content, "CREATE") {
			t.Errorf("migration file %q should contain SQL DDL (CREATE), got %d bytes", name, len(content))
		}
	}
}

func TestMigrationConfigMap_Contains001Schema(t *testing.T) {
	kn := testKubernaut()
	cm, err := MigrationConfigMap(kn)
	if err != nil {
		t.Fatalf("MigrationConfigMap() error: %v", err)
	}

	if _, ok := cm.Data["001_v1_schema.sql"]; !ok {
		t.Error("migration ConfigMap should contain 001_v1_schema.sql")
	}
}

func TestMigrationJob_Structure(t *testing.T) {
	kn := testKubernaut()
	job := MigrationJob(kn)

	if job.Name != "kubernaut-db-migration" {
		t.Errorf("name = %q, want %q", job.Name, "kubernaut-db-migration")
	}
	if job.Namespace != "kubernaut-system" {
		t.Errorf("namespace = %q, want %q", job.Namespace, "kubernaut-system")
	}

	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 3 {
		t.Error("Job should have backoffLimit=3")
	}
	if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != 300 {
		t.Error("Job should have TTLSecondsAfterFinished=300")
	}
}

func TestMigrationJob_Container(t *testing.T) {
	kn := testKubernaut()
	job := MigrationJob(kn)

	if len(job.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Job should have 1 container, got %d", len(job.Spec.Template.Spec.Containers))
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Name != "db-migrate" {
		t.Errorf("container name = %q, want %q", container.Name, "db-migrate")
	}

	wantImage := "quay.io/kubernaut-ai/db-migrate:v1.3.0"
	if container.Image != wantImage {
		t.Errorf("image = %q, want %q", container.Image, wantImage)
	}

	if len(container.Command) == 0 || container.Command[0] != "goose" {
		t.Errorf("command should start with goose, got %v", container.Command)
	}
}

func TestMigrationJob_EnvFromPGSecret(t *testing.T) {
	kn := testKubernaut()
	job := MigrationJob(kn)

	container := job.Spec.Template.Spec.Containers[0]
	if len(container.EnvFrom) == 0 {
		t.Fatal("container should have EnvFrom for PG secret")
	}

	ref := container.EnvFrom[0].SecretRef
	if ref == nil || ref.Name != "postgresql-secret" {
		t.Errorf("EnvFrom should reference postgresql-secret, got %v", ref)
	}
}

func TestMigrationJob_MountsMigrationsCM(t *testing.T) {
	kn := testKubernaut()
	job := MigrationJob(kn)

	container := job.Spec.Template.Spec.Containers[0]
	found := false
	for _, vm := range container.VolumeMounts {
		if vm.Name == "migrations" && vm.MountPath == "/migrations" {
			found = true
		}
	}
	if !found {
		t.Error("container should mount migrations volume at /migrations")
	}

	volFound := false
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == "migrations" && v.ConfigMap != nil && v.ConfigMap.Name == "kubernaut-migrations" {
			volFound = true
		}
	}
	if !volFound {
		t.Error("Job should have a migrations volume backed by kubernaut-migrations ConfigMap")
	}
}

func TestMigrationJob_SecurityContext(t *testing.T) {
	kn := testKubernaut()
	job := MigrationJob(kn)

	psc := job.Spec.Template.Spec.SecurityContext
	if psc == nil || psc.RunAsNonRoot == nil || !*psc.RunAsNonRoot {
		t.Error("Job pod should have RunAsNonRoot=true")
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.SecurityContext == nil {
		t.Error("Job container should have security context")
	}
}

func TestMigrationJob_GooseCommand_ContainsPGHost(t *testing.T) {
	kn := testKubernaut()
	job := MigrationJob(kn)

	container := job.Spec.Template.Spec.Containers[0]
	cmdStr := strings.Join(container.Command, " ")
	if !strings.Contains(cmdStr, "pg.example.com") {
		t.Errorf("goose command should reference PG host, got:\n%s", cmdStr)
	}
	if !strings.Contains(cmdStr, "5432") {
		t.Errorf("goose command should reference PG port, got:\n%s", cmdStr)
	}
}

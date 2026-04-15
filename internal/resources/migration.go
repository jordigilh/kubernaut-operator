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
	"fmt"
	"io/fs"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	"github.com/jordigilh/kubernaut/pkg/shared/assets"
)

const migrationJobName = "kubernaut-db-migration"

// MigrationConfigMap builds the ConfigMap containing all embedded SQL migration files.
func MigrationConfigMap(kn *kubernautv1alpha1.Kubernaut) (*corev1.ConfigMap, error) {
	entries, err := fs.ReadDir(assets.MigrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}

	data := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := fs.ReadFile(assets.MigrationsFS, "migrations/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}
		data[entry.Name()] = string(content)
	}

	return &corev1.ConfigMap{
		ObjectMeta: ObjectMeta(kn, "kubernaut-migrations", "migration"),
		Data:       data,
	}, nil
}

// MigrationJob builds the database migration Job.
// It uses the db-migrate image which bundles the goose CLI and runs
// migrations from the mounted ConfigMap.
func MigrationJob(kn *kubernautv1alpha1.Kubernaut) (*batchv1.Job, error) {
	pgPort := PostgreSQLPort(kn)

	img, err := Image(&kn.Spec.Image, "db-migrate")
	if err != nil {
		return nil, err
	}

	backoffLimit := MigrationBackoffLimit
	ttlSeconds := MigrationTTLSeconds

	return &batchv1.Job{
		ObjectMeta: ObjectMeta(kn, migrationJobName, "migration"),
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: ComponentLabels(kn, "migration")},
				Spec: corev1.PodSpec{
					RestartPolicy:   corev1.RestartPolicyOnFailure,
					SecurityContext: PodSecurityContext(),
					Containers: []corev1.Container{{
						Name:            "db-migrate",
						Image:           img,
						ImagePullPolicy: kn.Spec.Image.PullPolicy,
						// $(VAR) references are expanded by the kubelet before
						// container start using all env vars including envFrom.
						Command: []string{"goose", "-dir", "/migrations", "postgres",
							fmt.Sprintf("host=%s port=%d dbname=$(POSTGRES_DB) user=$(POSTGRES_USER) password=$(POSTGRES_PASSWORD) sslmode=disable",
								kn.Spec.PostgreSQL.Host, pgPort),
							"up",
						},
						EnvFrom: []corev1.EnvFromSource{{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: kn.Spec.PostgreSQL.SecretName},
							},
						}},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "migrations",
							MountPath: "/migrations",
							ReadOnly:  true,
						}},
						SecurityContext: ContainerSecurityContext(),
						Resources:       DefaultResources(),
					}},
					Volumes: []corev1.Volume{
						configMapVolume("migrations", "kubernaut-migrations"),
					},
					ImagePullSecrets: kn.Spec.Image.PullSecrets,
				},
			},
		},
	}, nil
}

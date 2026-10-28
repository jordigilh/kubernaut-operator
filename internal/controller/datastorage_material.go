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
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// dataStorageSigningMaterialRevision returns a non-sensitive revision for the
// effective DataStorage audit-signing Secret. The Secret contents never enter
// metadata; resourceVersion changes are sufficient to trigger a fresh pod.
func (r *KubernautReconciler) dataStorageSigningMaterialRevision(ctx context.Context, kn *kubernautv1alpha2.Kubernaut) (string, error) {
	secretName := resources.DataStorageSigningSecretName(kn)
	if secretName == "" {
		return "", nil
	}

	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: kn.Namespace, Name: secretName}
	if err := r.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return hashDataStorageSigningRevision(fmt.Sprintf("%s/%s@missing", key.Namespace, key.Name)), nil
		}
		return "", fmt.Errorf("reading DataStorage signing Secret %q for revision: %w", secretName, err)
	}
	return hashDataStorageSigningRevision(fmt.Sprintf("%s/%s@%s", key.Namespace, key.Name, secret.ResourceVersion)), nil
}

func hashDataStorageSigningRevision(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// hashDataStorageDependencyRevision hashes non-sensitive dependency identity
// and resource-version material for rollout annotations.
func hashDataStorageDependencyRevision(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// postgresqlCredentialRevision returns a non-sensitive revision for the
// administrator-owned PostgreSQL Secret referenced by the Kubernaut CR.
func (r *KubernautReconciler) postgresqlCredentialRevision(ctx context.Context, kn *kubernautv1alpha2.Kubernaut) (string, error) {
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: kn.Namespace, Name: kn.Spec.PostgreSQL.SecretName}
	if err := r.Get(ctx, key, secret); err != nil {
		return "", fmt.Errorf("reading PostgreSQL Secret %q for credential revision: %w", key.Name, err)
	}
	return hashDataStorageDependencyRevision(fmt.Sprintf("postgresql/%s/%s@%s", key.Namespace, key.Name, secret.ResourceVersion)), nil
}

// dataStorageDependencyRevision combines PostgreSQL and Valkey Secret
// revisions without reading credential bytes into the resulting value.
func (r *KubernautReconciler) dataStorageDependencyRevision(ctx context.Context, kn *kubernautv1alpha2.Kubernaut) (string, error) {
	postgresqlRevision, err := r.postgresqlCredentialRevision(ctx, kn)
	if err != nil {
		return "", err
	}
	valkey := &corev1.Secret{}
	key := client.ObjectKey{Namespace: kn.Namespace, Name: kn.Spec.Valkey.SecretName}
	if err := r.Get(ctx, key, valkey); err != nil {
		return "", fmt.Errorf("reading Valkey Secret %q for dependency revision: %w", key.Name, err)
	}
	return hashDataStorageDependencyRevision(fmt.Sprintf("%s|valkey/%s/%s@%s", postgresqlRevision, key.Namespace, key.Name, valkey.ResourceVersion)), nil
}

func stampDataStorageSigningMaterialRevision(dep *appsv1.Deployment, revisions telemetryMaterialRevisionSet) {
	if dep.Spec.Template.Labels["app"] != resources.ComponentDataStorage || revisions.DataStorageSigning == "" {
		return
	}
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = make(map[string]string, 1)
	}
	dep.Spec.Template.Annotations[resources.AnnotationDataStorageSigningMaterialRevision] = revisions.DataStorageSigning
}

// stampDataStorageDependencyRevision adds the BYO dependency revision only to
// the DataStorage pod template, keeping unrelated component hashes stable.
func stampDataStorageDependencyRevision(dep *appsv1.Deployment, revisions telemetryMaterialRevisionSet) {
	if dep.Spec.Template.Labels["app"] != resources.ComponentDataStorage || revisions.DataStorageDependency == "" {
		return
	}
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = make(map[string]string, 1)
	}
	dep.Spec.Template.Annotations[resources.AnnotationDataStorageDependencyRevision] = revisions.DataStorageDependency
}

// byoDependencySecretToKubernaut maps changes to administrator-owned
// PostgreSQL and Valkey Secrets to the singleton without adopting or mutating
// the Secret.
func (r *KubernautReconciler) byoDependencySecretToKubernaut(ctx context.Context, object client.Object) []reconcile.Request {
	secret, ok := object.(*corev1.Secret)
	if !ok || secret == nil {
		return nil
	}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	instances := &kubernautv1alpha2.KubernautList{}
	if err := reader.List(ctx, instances, client.InNamespace(secret.Namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "failed to list Kubernaut resources for BYO dependency Secret event",
			"namespace", secret.Namespace, "name", secret.Name)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(instances.Items))
	for index := range instances.Items {
		kn := &instances.Items[index]
		if kn.Spec.PostgreSQL.SecretName == secret.Name || kn.Spec.Valkey.SecretName == secret.Name {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
		}
	}
	return requests
}

// dataStorageSigningSecretToKubernaut maps changes to non-owned administrator
// or cert-manager signing Secrets to DataStorage's singleton CR.
func (r *KubernautReconciler) dataStorageSigningSecretToKubernaut(ctx context.Context, object client.Object) []reconcile.Request {
	secret, ok := object.(*corev1.Secret)
	if !ok || secret == nil {
		return nil
	}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	instances := &kubernautv1alpha2.KubernautList{}
	if err := reader.List(ctx, instances, client.InNamespace(secret.Namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "listing Kubernaut objects for DataStorage signing Secret event",
			"namespace", secret.Namespace, "name", secret.Name)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(instances.Items))
	for index := range instances.Items {
		kn := &instances.Items[index]
		if resources.DataStorageSigningSecretName(kn) == secret.Name {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
		}
	}
	return requests
}

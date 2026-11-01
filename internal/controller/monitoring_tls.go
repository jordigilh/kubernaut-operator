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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	monitoringTLSSecretKind = "secret"
)

// monitoringTLSConfigured reports whether the CR selects an administrator-owned
// monitoring CA Secret.
func monitoringTLSConfigured(kn *kubernautv1alpha2.Kubernaut) bool {
	if kn == nil {
		return false
	}
	return kn.Spec.Monitoring.TLS != nil &&
		kn.Spec.Monitoring.TLS.CACertSecretRef != nil &&
		kn.Spec.Monitoring.TLS.CACertSecretRef.Name != ""
}

// monitoringTLSReference resolves the administrator-owned Secret that backs
// the shared monitoring CA.
func monitoringTLSReference(kn *kubernautv1alpha2.Kubernaut) (kind, name, key string, ok bool) {
	if kn == nil {
		return "", "", "", false
	}
	return monitoringTLSSecretReference(kn.Spec.Monitoring.TLS)
}

// monitoringTLSSecretReference extracts the effective Secret identity and key.
func monitoringTLSSecretReference(tls *kubernautv1alpha2.MonitoringTLSSpec) (string, string, string, bool) {
	if tls == nil || tls.CACertSecretRef == nil || tls.CACertSecretRef.Name == "" {
		return "", "", "", false
	}
	return monitoringTLSSecretKind, tls.CACertSecretRef.Name, monitoringTLSKey(tls.CACertSecretRef.Key), true
}

// monitoringTLSKey returns the default CA key when the reference omits one.
func monitoringTLSKey(key string) string {
	if key == "" {
		return monitoringTLSDefaultCAKey
	}
	return key
}

// monitoringTLSMaterialRevision returns a non-sensitive revision for the
// effective monitoring CA. It intentionally uses only object identity and
// resourceVersion, never certificate bytes, so valid CA rotation rolls the
// three consumers without leaking trust material into pod metadata.
func (r *KubernautReconciler) monitoringTLSMaterialRevision(ctx context.Context, kn *kubernautv1alpha2.Kubernaut) (string, error) {
	if !monitoringTLSConfigured(kn) {
		return "", nil
	}
	kind, name, key, _ := monitoringTLSReference(kn)
	objectKey := client.ObjectKey{Namespace: kn.Namespace, Name: name}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, objectKey, secret); err != nil {
		return "", fmt.Errorf("monitoring tls Secret %q could not be read: %w", name, err)
	}
	input := fmt.Sprintf("%s=%s/%s@%s#%s", kind, objectKey.Namespace, name, secret.ResourceVersion, key)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input))), nil
}

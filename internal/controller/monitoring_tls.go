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
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

const (
	monitoringTLSSecretKind    = "secret"
	monitoringTLSConfigMapKind = "configMap"
)

func monitoringTLSConfigured(kn *kubernautv1alpha2.Kubernaut) bool {
	if kn == nil {
		return false
	}
	return kn.Spec.Monitoring.TLS != nil ||
		kn.Spec.Monitoring.Prometheus.URL != "" ||
		kn.Spec.Monitoring.AlertManager.URL != ""
}

// monitoringTLSReference resolves the effective Kubernetes object that backs
// the shared monitoring CA. The source contract is namespace-local, and the
// interService default follows the same manual-mode ownership boundary used by
// resources.MonitoringTLSVolume.
func monitoringTLSReference(kn *kubernautv1alpha2.Kubernaut) (kind, name, key string, ok bool) {
	if kn == nil {
		return "", "", "", false
	}
	tls := kn.Spec.Monitoring.TLS
	source := kubernautv1alpha2.MonitoringTLSSourceInterService
	if tls != nil && tls.Source != "" {
		source = tls.Source
	}
	switch source {
	case kubernautv1alpha2.MonitoringTLSSourceSystem:
		return "", "", "", false
	case kubernautv1alpha2.MonitoringTLSSourceSecret:
		return monitoringTLSSecretReference(tls)
	case kubernautv1alpha2.MonitoringTLSSourceConfigMap:
		return monitoringTLSConfigMapReference(tls)
	case kubernautv1alpha2.MonitoringTLSSourceInterService:
		return monitoringTLSInterServiceReference(kn)
	default:
		return "", "", "", false
	}
}

func monitoringTLSSecretReference(tls *kubernautv1alpha2.MonitoringTLSSpec) (string, string, string, bool) {
	if tls == nil || tls.CACertSecretRef == nil || tls.CACertSecretRef.Name == "" {
		return "", "", "", false
	}
	return monitoringTLSSecretKind, tls.CACertSecretRef.Name, monitoringTLSKey(tls.CACertSecretRef.Key), true
}

func monitoringTLSConfigMapReference(tls *kubernautv1alpha2.MonitoringTLSSpec) (string, string, string, bool) {
	if tls == nil || tls.CACertConfigMapRef == nil || tls.CACertConfigMapRef.Name == "" {
		return "", "", "", false
	}
	return monitoringTLSConfigMapKind, tls.CACertConfigMapRef.Name, monitoringTLSKey(tls.CACertConfigMapRef.Key), true
}

func monitoringTLSInterServiceReference(kn *kubernautv1alpha2.Kubernaut) (string, string, string, bool) {
	if kn.Spec.TLS.Mode != kubernautv1alpha2.TLSModeManual {
		return monitoringTLSConfigMapKind, resources.TrustBundleConfigMapName, "service-ca.crt", true
	}
	if cfg := kn.Spec.TLS.Manual; cfg != nil && cfg.InternalCASecretName != "" {
		return monitoringTLSSecretKind, cfg.InternalCASecretName, monitoringTLSDefaultCAKey, true
	}
	return monitoringTLSConfigMapKind, resources.InterServiceCAConfigMapName, monitoringTLSDefaultCAKey, true
}

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
	kind, name, key, ok := monitoringTLSReference(kn)
	if !ok {
		return "", nil
	}
	objectKey := client.ObjectKey{Namespace: kn.Namespace, Name: name}
	var resourceVersion string
	switch kind {
	case monitoringTLSSecretKind:
		secret := &corev1.Secret{}
		if err := r.Get(ctx, objectKey, secret); err != nil {
			return "", fmt.Errorf("monitoring tls Secret %q could not be read: %w", name, err)
		}
		resourceVersion = secret.ResourceVersion
	default:
		configMap := &corev1.ConfigMap{}
		if err := r.Get(ctx, objectKey, configMap); err != nil {
			return "", fmt.Errorf("monitoring tls ConfigMap %q could not be read: %w", name, err)
		}
		resourceVersion = configMap.ResourceVersion
	}
	input := fmt.Sprintf("%s=%s/%s@%s#%s", kind, objectKey.Namespace, name, resourceVersion, key)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input))), nil
}

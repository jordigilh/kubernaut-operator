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
	"strings"
	"testing"

	"github.com/jordigilh/kubernaut-operator/internal/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const authbridgeCMName = "authbridge-config-apifrontend"

func authbridgeConfigMap(ns string, configYAML string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authbridgeCMName,
			Namespace: ns,
		},
		Data: map[string]string{
			"config.yaml": configYAML,
		},
	}
}

func TestEnsureAuthbridgeMetricsBypass_AddsMetrics(t *testing.T) {
	kn := testKubernautCR(true, true)
	cm := authbridgeConfigMap(kn.Namespace, `bypass:
  inbound_paths:
  - /.well-known/*
  - /healthz
  - /readyz
  - /livez
mode: envoy-sidecar
`)
	r := newReconcilerWithCRD(newAgentRuntimeCRD(), newTestNamespace(nil), cm)

	if err := r.ensureAuthbridgeMetricsBypass(context.Background(), kn, resources.KagentiSidecarAuthbridge); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.ConfigMap{}
	if err := r.Get(context.Background(), keyFor(cm), updated); err != nil {
		t.Fatalf("failed to get updated configmap: %v", err)
	}

	data := updated.Data["config.yaml"]
	if !strings.Contains(data, "/metrics") {
		t.Errorf("expected /metrics in bypass.inbound_paths, got:\n%s", data)
	}
}

func TestEnsureAuthbridgeMetricsBypass_NoopWhenAlreadyPresent(t *testing.T) {
	kn := testKubernautCR(true, true)
	cm := authbridgeConfigMap(kn.Namespace, `bypass:
  inbound_paths:
  - /.well-known/*
  - /healthz
  - /readyz
  - /livez
  - /metrics
mode: envoy-sidecar
`)
	r := newReconcilerWithCRD(newAgentRuntimeCRD(), newTestNamespace(nil), cm)

	if err := r.ensureAuthbridgeMetricsBypass(context.Background(), kn, resources.KagentiSidecarAuthbridge); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.ConfigMap{}
	if err := r.Get(context.Background(), keyFor(cm), updated); err != nil {
		t.Fatalf("failed to get updated configmap: %v", err)
	}

	count := strings.Count(updated.Data["config.yaml"], "/metrics")
	if count != 1 {
		t.Errorf("expected exactly 1 /metrics entry, found %d", count)
	}
}

func TestEnsureAuthbridgeMetricsBypass_NoopWhenSidecarNone(t *testing.T) {
	kn := testKubernautCR(true, true)
	cm := authbridgeConfigMap(kn.Namespace, `bypass:
  inbound_paths:
  - /healthz
mode: envoy-sidecar
`)
	r := newReconcilerWithCRD(newAgentRuntimeCRD(), newTestNamespace(nil), cm)

	if err := r.ensureAuthbridgeMetricsBypass(context.Background(), kn, resources.KagentiSidecarNone); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.ConfigMap{}
	if err := r.Get(context.Background(), keyFor(cm), updated); err != nil {
		t.Fatalf("failed to get updated configmap: %v", err)
	}

	if strings.Contains(updated.Data["config.yaml"], "/metrics") {
		t.Error("should not patch when sidecar is None")
	}
}

func TestEnsureAuthbridgeMetricsBypass_NoopWhenCMMissing(t *testing.T) {
	kn := testKubernautCR(true, true)
	r := newReconcilerWithCRD(newAgentRuntimeCRD(), newTestNamespace(nil))

	if err := r.ensureAuthbridgeMetricsBypass(context.Background(), kn, resources.KagentiSidecarAuthbridge); err != nil {
		t.Fatalf("should not error when ConfigMap is missing: %v", err)
	}
}

func TestEnsureAuthbridgeMetricsBypass_PreservesOtherFields(t *testing.T) {
	kn := testKubernautCR(true, true)
	cm := authbridgeConfigMap(kn.Namespace, `bypass:
  inbound_paths:
  - /healthz
  - /readyz
identity:
  client_id: spiffe://example.com/ns/test/sa/apifrontend
  type: client-secret
mode: envoy-sidecar
`)
	r := newReconcilerWithCRD(newAgentRuntimeCRD(), newTestNamespace(nil), cm)

	if err := r.ensureAuthbridgeMetricsBypass(context.Background(), kn, resources.KagentiSidecarAuthbridge); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.ConfigMap{}
	if err := r.Get(context.Background(), keyFor(cm), updated); err != nil {
		t.Fatalf("failed to get updated configmap: %v", err)
	}

	data := updated.Data["config.yaml"]
	if !strings.Contains(data, "/metrics") {
		t.Error("expected /metrics in bypass.inbound_paths")
	}
	if !strings.Contains(data, "client-secret") {
		t.Error("identity.type should be preserved")
	}
	if !strings.Contains(data, "envoy-sidecar") {
		t.Error("mode should be preserved")
	}
}

func keyFor(cm *corev1.ConfigMap) client.ObjectKey {
	return client.ObjectKey{Namespace: cm.Namespace, Name: cm.Name}
}

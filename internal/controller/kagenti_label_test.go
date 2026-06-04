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
	"testing"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newTestNamespace(labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "kubernaut-system",
			Labels: labels,
		},
	}
}

func testKubernautCR(afEnabled bool, spireEnabled bool) *kubernautv1alpha1.Kubernaut {
	kn := &kubernautv1alpha1.Kubernaut{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernaut",
			Namespace: "kubernaut-system",
		},
		Spec: kubernautv1alpha1.KubernautSpec{
			APIFrontend: kubernautv1alpha1.APIFrontendSpec{
				SPIRE: kubernautv1alpha1.APIFrontendSPIRESpec{
					Enabled: spireEnabled,
				},
			},
		},
	}
	if !afEnabled {
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
	}
	return kn
}

func TestEnsureKagentiNamespaceLabel_AddsWhenSPIREEnabled(t *testing.T) {
	ns := newTestNamespace(nil)
	kn := testKubernautCR(true, true)
	r := newFakeReconciler(ns)

	if err := r.ensureKagentiNamespaceLabel(context.Background(), kn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.Namespace{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "kubernaut-system"}, updated); err != nil {
		t.Fatalf("failed to get namespace: %v", err)
	}
	if updated.Labels["kagenti-enabled"] != "true" {
		t.Errorf("expected kagenti-enabled=true, got %q", updated.Labels["kagenti-enabled"])
	}
}

func TestEnsureKagentiNamespaceLabel_RemovesWhenSPIREDisabled(t *testing.T) {
	ns := newTestNamespace(map[string]string{"kagenti-enabled": "true"})
	kn := testKubernautCR(true, false)
	r := newFakeReconciler(ns)

	if err := r.ensureKagentiNamespaceLabel(context.Background(), kn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.Namespace{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "kubernaut-system"}, updated); err != nil {
		t.Fatalf("failed to get namespace: %v", err)
	}
	if _, ok := updated.Labels["kagenti-enabled"]; ok {
		t.Error("expected kagenti-enabled label to be removed")
	}
}

func TestEnsureKagentiNamespaceLabel_RemovesWhenAFDisabled(t *testing.T) {
	ns := newTestNamespace(map[string]string{"kagenti-enabled": "true"})
	kn := testKubernautCR(false, true)
	r := newFakeReconciler(ns)

	if err := r.ensureKagentiNamespaceLabel(context.Background(), kn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.Namespace{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "kubernaut-system"}, updated); err != nil {
		t.Fatalf("failed to get namespace: %v", err)
	}
	if _, ok := updated.Labels["kagenti-enabled"]; ok {
		t.Error("expected kagenti-enabled label to be removed when AF is disabled")
	}
}

func TestEnsureKagentiNamespaceLabel_NoopWhenAlreadyCorrect(t *testing.T) {
	ns := newTestNamespace(map[string]string{"kagenti-enabled": "true", "other": "label"})
	kn := testKubernautCR(true, true)
	r := newFakeReconciler(ns)

	if err := r.ensureKagentiNamespaceLabel(context.Background(), kn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.Namespace{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "kubernaut-system"}, updated); err != nil {
		t.Fatalf("failed to get namespace: %v", err)
	}
	if updated.Labels["kagenti-enabled"] != "true" {
		t.Error("label should still be present")
	}
	if updated.Labels["other"] != "label" {
		t.Error("other labels should be preserved")
	}
}

func TestEnsureKagentiNamespaceLabel_NoopWhenAlreadyAbsent(t *testing.T) {
	ns := newTestNamespace(map[string]string{"other": "label"})
	kn := testKubernautCR(true, false)
	r := newFakeReconciler(ns)

	if err := r.ensureKagentiNamespaceLabel(context.Background(), kn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.Namespace{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "kubernaut-system"}, updated); err != nil {
		t.Fatalf("failed to get namespace: %v", err)
	}
	if _, ok := updated.Labels["kagenti-enabled"]; ok {
		t.Error("label should not be added")
	}
	if updated.Labels["other"] != "label" {
		t.Error("other labels should be preserved")
	}
}

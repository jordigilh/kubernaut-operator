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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

func newFakeReconciler(objs ...runtime.Object) *KubernautReconciler {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		builder = builder.WithRuntimeObjects(o)
	}
	return &KubernautReconciler{
		Client:   builder.Build(),
		Scheme:   scheme,
		Recorder: events.NewFakeRecorder(100),
	}
}

func TestClearStaleServingCertErrors_NoAnnotation(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "plain-svc", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}},
	}
	r := newFakeReconciler(svc)

	if err := r.clearStaleServingCertErrors(context.Background(), svc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClearStaleServingCertErrors_ClearsWhenSecretMissing(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stale-svc",
			Namespace: "default",
			Annotations: map[string]string{
				resources.OCPServingCertAnnotation:                            "my-tls",
				"service.beta.openshift.io/serving-cert-generation-error":     "UID mismatch",
				"service.beta.openshift.io/serving-cert-generation-error-num": "5",
			},
		},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 443, Protocol: corev1.ProtocolTCP}}},
	}
	r := newFakeReconciler(svc)

	if err := r.clearStaleServingCertErrors(context.Background(), svc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.Service{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, updated); err != nil {
		t.Fatalf("failed to get updated service: %v", err)
	}
	if _, ok := updated.Annotations["service.beta.openshift.io/serving-cert-generation-error"]; ok {
		t.Error("expected error annotation to be cleared")
	}
	if _, ok := updated.Annotations["service.beta.openshift.io/serving-cert-generation-error-num"]; ok {
		t.Error("expected error-num annotation to be cleared")
	}
}

func TestClearStaleServingCertErrors_PreservesWhenSecretExists(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-tls", Namespace: "default"},
		Data:       map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ok-svc",
			Namespace: "default",
			Annotations: map[string]string{
				resources.OCPServingCertAnnotation:                            "existing-tls",
				"service.beta.openshift.io/serving-cert-generation-error":     "some error",
				"service.beta.openshift.io/serving-cert-generation-error-num": "1",
			},
		},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 443, Protocol: corev1.ProtocolTCP}}},
	}
	r := newFakeReconciler(svc, secret)

	if err := r.clearStaleServingCertErrors(context.Background(), svc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &corev1.Service{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, updated); err != nil {
		t.Fatalf("failed to get service: %v", err)
	}
	if _, ok := updated.Annotations["service.beta.openshift.io/serving-cert-generation-error"]; !ok {
		t.Error("expected error annotation to be preserved when secret exists")
	}
}

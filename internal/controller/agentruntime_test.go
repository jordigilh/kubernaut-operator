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

	"github.com/jordigilh/kubernaut-operator/internal/resources"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const agentRuntimeCRDName = "agentruntimes.agent.kagenti.dev"

func agentRuntimeGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "agent.kagenti.dev",
		Version: "v1alpha1",
		Kind:    "AgentRuntime",
	}
}

func newAgentRuntimeCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: agentRuntimeCRDName},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "agent.kagenti.dev",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "AgentRuntime",
				Plural:   "agentruntimes",
				Singular: "agentruntime",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{Name: "v1alpha1", Served: true, Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:                   "object",
							XPreserveUnknownFields: boolPtr(true),
						},
					},
				},
			},
		},
	}
}

func boolPtr(b bool) *bool { return &b }

func newReconcilerWithCRD(objs ...runtime.Object) *KubernautReconciler {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = apiextensionsv1.AddToScheme(s)

	builder := fake.NewClientBuilder().WithScheme(s)
	for _, o := range objs {
		builder = builder.WithRuntimeObjects(o)
	}
	return &KubernautReconciler{
		Client:   builder.Build(),
		Scheme:   s,
		Recorder: events.NewFakeRecorder(100),
	}
}

func getAgentRuntime(ctx context.Context, c client.Client, ns, name string) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(agentRuntimeGVK())
	return obj, c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, obj)
}

// ---------------------------------------------------------------------------
// Unit tests: ensureAgentRuntimeCR
// ---------------------------------------------------------------------------

func TestEnsureAgentRuntimeCR_CreatesWhenSidecarActive(t *testing.T) {
	crd := newAgentRuntimeCRD()
	ns := newTestNamespace(nil)
	kn := testKubernautCR(true, true)
	r := newReconcilerWithCRD(crd, ns)

	if err := r.ensureAgentRuntimeCR(context.Background(), kn, resources.KagentiSidecarAuthbridge); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ar, err := getAgentRuntime(context.Background(), r.Client, kn.Namespace, string(resources.ComponentAPIFrontend))
	if err != nil {
		t.Fatalf("AgentRuntime should exist: %v", err)
	}

	spec, ok := ar.Object["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("AgentRuntime spec should be a map")
	}
	if spec["type"] != "agent" {
		t.Errorf("expected type=agent, got %v", spec["type"])
	}
	targetRef, ok := spec["targetRef"].(map[string]interface{})
	if !ok {
		t.Fatal("targetRef should be a map")
	}
	if targetRef["kind"] != "Deployment" {
		t.Errorf("expected targetRef.kind=Deployment, got %v", targetRef["kind"])
	}
	if targetRef["name"] != string(resources.ComponentAPIFrontend) {
		t.Errorf("expected targetRef.name=%s, got %v", resources.ComponentAPIFrontend, targetRef["name"])
	}

	labels := ar.GetLabels()
	if labels["app.kubernetes.io/managed-by"] != "kubernaut-operator" {
		t.Errorf("expected managed-by=kubernaut-operator, got %q", labels["app.kubernetes.io/managed-by"])
	}
}

func TestEnsureAgentRuntimeCR_NoopWhenAlreadyExists(t *testing.T) {
	crd := newAgentRuntimeCRD()
	ns := newTestNamespace(nil)
	kn := testKubernautCR(true, true)
	r := newReconcilerWithCRD(crd, ns)

	if err := r.ensureAgentRuntimeCR(context.Background(), kn, resources.KagentiSidecarAuthbridge); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Second call should be a no-op.
	if err := r.ensureAgentRuntimeCR(context.Background(), kn, resources.KagentiSidecarAuthbridge); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if _, err := getAgentRuntime(context.Background(), r.Client, kn.Namespace, string(resources.ComponentAPIFrontend)); err != nil {
		t.Fatalf("AgentRuntime should still exist: %v", err)
	}
}

func TestEnsureAgentRuntimeCR_DeletesWhenSidecarDisabled(t *testing.T) {
	crd := newAgentRuntimeCRD()
	ns := newTestNamespace(nil)
	kn := testKubernautCR(true, true)
	r := newReconcilerWithCRD(crd, ns)

	// Pre-create
	if err := r.ensureAgentRuntimeCR(context.Background(), kn, resources.KagentiSidecarAuthbridge); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Disable sidecar
	if err := r.ensureAgentRuntimeCR(context.Background(), kn, resources.KagentiSidecarNone); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := getAgentRuntime(context.Background(), r.Client, kn.Namespace, string(resources.ComponentAPIFrontend))
	if err == nil {
		t.Fatal("AgentRuntime should be deleted when sidecar is disabled")
	}
}

func TestEnsureAgentRuntimeCR_NoopWhenSidecarDisabledAndNoCR(t *testing.T) {
	crd := newAgentRuntimeCRD()
	ns := newTestNamespace(nil)
	kn := testKubernautCR(true, false)
	r := newReconcilerWithCRD(crd, ns)

	if err := r.ensureAgentRuntimeCR(context.Background(), kn, resources.KagentiSidecarNone); err != nil {
		t.Fatalf("should be no-op: %v", err)
	}
}

func TestEnsureAgentRuntimeCR_SkipsWhenCRDNotInstalled(t *testing.T) {
	ns := newTestNamespace(nil)
	kn := testKubernautCR(true, true)
	r := newFakeReconciler(ns) // no CRD registered

	if err := r.ensureAgentRuntimeCR(context.Background(), kn, resources.KagentiSidecarAuthbridge); err != nil {
		t.Fatalf("should skip gracefully: %v", err)
	}
}

func TestEnsureAgentRuntimeCR_WorksWithEnvoySidecarMode(t *testing.T) {
	crd := newAgentRuntimeCRD()
	ns := newTestNamespace(nil)
	kn := testKubernautCR(true, true)
	r := newReconcilerWithCRD(crd, ns)

	if err := r.ensureAgentRuntimeCR(context.Background(), kn, resources.KagentiSidecarEnvoy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := getAgentRuntime(context.Background(), r.Client, kn.Namespace, string(resources.ComponentAPIFrontend)); err != nil {
		t.Fatalf("AgentRuntime should exist for envoy mode: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Unit tests: deleteAgentRuntimeCR
// ---------------------------------------------------------------------------

func TestDeleteAgentRuntimeCR_DeletesExisting(t *testing.T) {
	crd := newAgentRuntimeCRD()
	ns := newTestNamespace(nil)
	kn := testKubernautCR(true, true)
	r := newReconcilerWithCRD(crd, ns)

	// Pre-create
	if err := r.ensureAgentRuntimeCR(context.Background(), kn, resources.KagentiSidecarAuthbridge); err != nil {
		t.Fatalf("create: %v", err)
	}

	errs := r.deleteAgentRuntimeCR(context.Background(), kn)
	if len(errs) > 0 {
		t.Fatalf("delete errors: %v", errs)
	}

	_, err := getAgentRuntime(context.Background(), r.Client, kn.Namespace, string(resources.ComponentAPIFrontend))
	if err == nil {
		t.Fatal("AgentRuntime should be deleted")
	}
}

func TestDeleteAgentRuntimeCR_NoopWhenNotExists(t *testing.T) {
	crd := newAgentRuntimeCRD()
	ns := newTestNamespace(nil)
	kn := testKubernautCR(true, true)
	r := newReconcilerWithCRD(crd, ns)

	errs := r.deleteAgentRuntimeCR(context.Background(), kn)
	if len(errs) > 0 {
		t.Fatalf("should not error: %v", errs)
	}
}

func TestDeleteAgentRuntimeCR_SkipsWhenCRDNotInstalled(t *testing.T) {
	ns := newTestNamespace(nil)
	kn := testKubernautCR(true, true)
	r := newFakeReconciler(ns)

	errs := r.deleteAgentRuntimeCR(context.Background(), kn)
	if len(errs) > 0 {
		t.Fatalf("should skip when CRD absent: %v", errs)
	}
}

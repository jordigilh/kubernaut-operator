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
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	"github.com/jordigilh/kubernaut/pkg/shared/assets"
)

const sharedCRDStatusKind = "Status"

var _ = Describe("UT-OWN-514-004 [AC-3, AC-6, CM-3; SOC2 CC6.6; ASVS v5.0.0-V8.3.1] shared operand CRD ownership", func() {
	var api *sharedCRDAPIFixture
	var server *httptest.Server
	BeforeEach(func() {
		api = &sharedCRDAPIFixture{objects: make(map[string]*unstructured.Unstructured)}
		server = httptest.NewServer(api)
	})
	AfterEach(func() { server.Close() })

	It("creates explicitly marked CRDs and is idempotent on the next install", func() {
		cfg := &rest.Config{Host: server.URL}
		Expect(EnsureCRDs(context.Background(), cfg)).To(Succeed())
		api.mu.Lock()
		Expect(api.objects).NotTo(BeEmpty())
		for _, object := range api.objects {
			Expect(object.GetLabels()).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "kubernaut-operator"))
		}
		writes := api.writes
		api.mu.Unlock()
		Expect(EnsureCRDs(context.Background(), cfg)).To(Succeed())
		api.mu.Lock()
		defer api.mu.Unlock()
		Expect(api.writes).To(Equal(writes))
	})

	It("stamps ownership and a hash when a desired CRD has no metadata maps", func() {
		dyn, err := dynamic.NewForConfig(&rest.Config{Host: server.URL})
		Expect(err).NotTo(HaveOccurred())
		desired := &unstructured.Unstructured{}
		desired.SetAPIVersion("apiextensions.k8s.io/v1")
		desired.SetKind("CustomResourceDefinition")
		desired.SetName("ownership-fixtures.kubernaut.ai")
		Expect(ensureSharedCRD(context.Background(), dyn.Resource(crdGVR), desired)).To(Succeed())
		Expect(desired.GetLabels()).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "kubernaut-operator"))
		Expect(desired.GetAnnotations()[AnnotationSpecHash]).NotTo(BeEmpty())
	})

	DescribeTable("does not overwrite administrator, Helm or foreign-owned schemas",
		func(labels map[string]string, refs []metav1.OwnerReference) {
			api.seed(labels, refs)
			err := EnsureCRDs(context.Background(), &rest.Config{Host: server.URL})
			Expect(err).To(MatchError(ContainSubstring("ownership conflict")))
			Expect(err.Error()).To(ContainSubstring("CustomResourceDefinition"))
			api.mu.Lock()
			defer api.mu.Unlock()
			Expect(api.writes).To(BeZero())
		},
		Entry("unmarked", nil, nil),
		Entry("Helm-owned", map[string]string{"app.kubernetes.io/managed-by": "Helm"}, nil),
		Entry("foreign owner despite matching label", map[string]string{"app.kubernetes.io/managed-by": "kubernaut-operator"}, []metav1.OwnerReference{{Kind: "Platform", APIVersion: "platform.io/v1", Name: "provider", Controller: ptr.To(true)}}),
	)

	It("rechecks a raced administrator CRD instead of accepting AlreadyExists", func() {
		api.race = true
		err := EnsureCRDs(context.Background(), &rest.Config{Host: server.URL})
		Expect(err).To(MatchError(ContainSubstring("ownership conflict")))
		api.mu.Lock()
		defer api.mu.Unlock()
		Expect(api.writes).To(BeZero())
	})

	It("updates an explicitly operator-managed schema and preserves unrelated metadata", func() {
		api.seed(map[string]string{"app.kubernetes.io/managed-by": "kubernaut-operator", "team": "sre"}, nil)
		api.mu.Lock()
		for _, object := range api.objects {
			object.Object["spec"] = map[string]interface{}{"legacy": "schema"}
			object.SetAnnotations(map[string]string{"team-note": "preserve"})
		}
		api.mu.Unlock()
		Expect(EnsureCRDs(context.Background(), &rest.Config{Host: server.URL})).To(Succeed())
		api.mu.Lock()
		defer api.mu.Unlock()
		Expect(api.writes).To(Equal(len(api.objects)))
		for _, object := range api.objects {
			Expect(object.GetLabels()).To(HaveKeyWithValue("team", "sre"))
			Expect(object.GetAnnotations()).To(HaveKeyWithValue("team-note", "preserve"))
		}
	})

	DescribeTable("surfaces Kubernetes API failures without treating them as authorization",
		func(method string, raced bool) {
			api.failMethod = method
			api.race = raced
			if method == http.MethodPut {
				api.seed(map[string]string{"app.kubernetes.io/managed-by": "kubernaut-operator"}, nil)
			}
			err := EnsureCRDs(context.Background(), &rest.Config{Host: server.URL})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "API errors must remain observable: %v", err)
			api.mu.Lock()
			defer api.mu.Unlock()
			Expect(api.writes).To(BeZero())
		},
		Entry("initial read denied", http.MethodGet, false),
		Entry("create denied", http.MethodPost, false),
		Entry("update denied", http.MethodPut, false),
		Entry("raced object cannot be read", http.MethodGet, true),
	)
})

type sharedCRDAPIFixture struct {
	mu         sync.Mutex
	objects    map[string]*unstructured.Unstructured
	writes     int
	race       bool
	failMethod string
}

func (a *sharedCRDAPIFixture) seed(labels map[string]string, refs []metav1.OwnerReference) {
	entries, err := fs.ReadDir(assets.CRDsFS, "crds")
	Expect(err).NotTo(HaveOccurred())
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, readErr := fs.ReadFile(assets.CRDsFS, "crds/"+entry.Name())
		Expect(readErr).NotTo(HaveOccurred())
		object, parseErr := yamlToUnstructured(data)
		Expect(parseErr).NotTo(HaveOccurred())
		object.SetLabels(labels)
		object.SetOwnerReferences(refs)
		object.SetResourceVersion("7")
		a.objects[object.GetName()] = object
	}
}

func (a *sharedCRDAPIFixture) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	defer GinkgoRecover()
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	name := path.Base(request.URL.Path)
	if request.Method == a.failMethod && !a.race {
		w.WriteHeader(http.StatusForbidden)
		status := apierrors.NewForbidden(crdGVR.GroupResource(), name, fmt.Errorf("fixture API denial")).ErrStatus
		status.Kind, status.APIVersion = sharedCRDStatusKind, "v1"
		Expect(json.NewEncoder(w).Encode(status)).To(Succeed())
		return
	}
	if request.Method == http.MethodGet {
		object, found := a.objects[name]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			status := apierrors.NewNotFound(crdGVR.GroupResource(), name).ErrStatus
			status.Kind, status.APIVersion = sharedCRDStatusKind, "v1"
			Expect(json.NewEncoder(w).Encode(status)).To(Succeed())
			return
		}
		Expect(json.NewEncoder(w).Encode(object)).To(Succeed())
		return
	}
	object := &unstructured.Unstructured{}
	Expect(json.NewDecoder(request.Body).Decode(object)).To(Succeed())
	if a.race && request.Method == http.MethodPost {
		a.race = false
		foreign := object.DeepCopy()
		foreign.SetLabels(nil)
		a.objects[object.GetName()] = foreign
		w.WriteHeader(http.StatusConflict)
		status := apierrors.NewAlreadyExists(crdGVR.GroupResource(), object.GetName()).ErrStatus
		status.Kind, status.APIVersion = sharedCRDStatusKind, "v1"
		Expect(json.NewEncoder(w).Encode(status)).To(Succeed())
		return
	}
	a.writes++
	object.SetResourceVersion("8")
	a.objects[object.GetName()] = object
	Expect(json.NewEncoder(w).Encode(object)).To(Succeed())
}

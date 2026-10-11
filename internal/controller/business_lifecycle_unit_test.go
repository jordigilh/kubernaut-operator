package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

// Only the external Kubernetes transport is simulated. Resource generation and
// every lifecycle decision execute unchanged; this fixture starts no API server.
type crdUnitTransport struct{}

const unitMissingReference = "missing"

func (crdUnitTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Request: request,
		Body: io.NopCloser(strings.NewReader(`{"apiVersion":"apiextensions.k8s.io/v1","kind":"CustomResourceDefinition","metadata":{"name":"fixture","resourceVersion":"1"}}`)),
	}, nil
}

func businessUnitReconciler(kn *kubernautv1alpha2.Kubernaut, faults interceptor.Funcs) *KubernautReconciler {
	objects := []client.Object{kn,
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: pgSecretName, Namespace: kn.Namespace}, Data: map[string][]byte{
			"POSTGRES_USER": []byte("kubernaut"), "POSTGRES_PASSWORD": []byte("unit-secret"), "POSTGRES_DB": []byte("kubernaut"),
		}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: vkSecretName, Namespace: kn.Namespace},
			Data: map[string][]byte{"valkey-secrets.yaml": []byte("password: unit-secret")}},
	}
	if kn.Spec.TLS.Mode == "" {
		objects = append(objects, &configv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}})
	}
	for _, name := range []string{"servicemonitors.monitoring.coreos.com", "prometheusrules.monitoring.coreos.com",
		"alertmanagerconfigs.monitoring.coreos.com"} {
		objects = append(objects, &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: name}})
	}
	certificate := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
	}}
	api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(objects...).
		WithStatusSubresource(&kubernautv1alpha2.Kubernaut{}, &batchv1.Job{}, &appsv1.Deployment{}, certificate).
		WithInterceptorFuncs(faults).Build()
	return &KubernautReconciler{Client: api, APIReader: api, Scheme: api.Scheme(),
		Recorder: events.NewFakeRecorder(10000), now: time.Now,
		RestCfg: &rest.Config{Host: "https://unit.invalid", Transport: crdUnitTransport{}},
	}
}

func refreshBusinessUnitCR(r *KubernautReconciler, kn *kubernautv1alpha2.Kubernaut) {
	ExpectWithOffset(1, r.Get(ctx, client.ObjectKeyFromObject(kn), kn)).To(Succeed())
}

var _ = Describe("independent business lifecycle decisions", Label("unit"), func() {
	for _, mode := range []kubernautv1alpha2.TLSMode{"", kubernautv1alpha2.TLSModeDevelopmentSelfSigned} {
		It(fmt.Sprintf("UT-AUDIT-515-LIFECYCLE-001: converges migration, deployment, readiness and cleanup with TLS mode %q", mode), func() {
			kn := newMinimalCR()
			kn.UID = "unit-lifecycle"
			kn.Spec.TLS.Mode = mode
			if mode == kubernautv1alpha2.TLSModeDevelopmentSelfSigned {
				kn.Spec.TLS.DevelopmentSelfSigned = &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{}
			}
			kn.Spec.APIFrontend.Route.Enabled = ptr.To(false)
			r := businessUnitReconciler(kn, interceptor.Funcs{})
			request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}
			_, err := r.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			refreshBusinessUnitCR(r, kn)
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseMigrating))
			jobs := &batchv1.JobList{}
			Expect(r.List(ctx, jobs)).To(Succeed())
			Expect(jobs.Items).To(HaveLen(1))
			jobs.Items[0].Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
			Expect(r.Status().Update(ctx, &jobs.Items[0])).To(Succeed())
			_, err = r.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			deployments := &appsv1.DeploymentList{}
			Expect(r.List(ctx, deployments)).To(Succeed())
			Expect(deployments.Items).To(HaveLen(len(resources.ActiveComponents(kn, kn))))
			for index := range deployments.Items {
				dep := &deployments.Items[index]
				Expect(dep.Spec.Template.Annotations).To(HaveKey(resources.AnnotationConfigMapHash))
				dep.Status.ReadyReplicas = ptr.Deref(dep.Spec.Replicas, 1)
				Expect(r.Status().Update(ctx, dep)).To(Succeed())
			}
			_, err = r.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			refreshBusinessUnitCR(r, kn)
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseRunning))
			Expect(kn.Status.LastMigrationHash).NotTo(BeEmpty())
			Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionServicesDeployed)).To(BeTrue())
			Expect(r.Delete(ctx, kn)).To(Succeed())
			_, err = r.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			roles := &corev1.NamespaceList{}
			Expect(r.List(ctx, roles)).To(Succeed())
			Expect(roles.Items).To(BeEmpty(), "the operator-owned workflow namespace is cleaned up")
		})
	}

	It("UT-AUDIT-515-LIFECYCLE-002: validates database inputs independently and never starts a migration on invalid configuration", func() {
		for _, mutate := range []func(*kubernautv1alpha2.Kubernaut){
			func(kn *kubernautv1alpha2.Kubernaut) { kn.Spec.PostgreSQL.Host = "bad;host" },
			func(kn *kubernautv1alpha2.Kubernaut) { kn.Spec.Valkey.Host = "bad;host" },
			func(kn *kubernautv1alpha2.Kubernaut) { kn.Spec.PostgreSQL.SecretName = unitMissingReference },
			func(kn *kubernautv1alpha2.Kubernaut) { kn.Spec.Valkey.SecretName = unitMissingReference },
			func(kn *kubernautv1alpha2.Kubernaut) { kn.Spec.KubernautAgent.LLMProfileRef = unitMissingReference },
		} {
			kn := newMinimalCR()
			mutate(kn)
			r := businessUnitReconciler(kn, interceptor.Funcs{})
			result, err := r.phaseValidate(ctx, kn, kn)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(requeueError))
			refreshBusinessUnitCR(r, kn)
			Expect(kn.Status.Phase).To(Equal(kubernautv1alpha2.PhaseError))
			Expect(meta.IsStatusConditionFalse(kn.Status.Conditions, kubernautv1alpha2.ConditionBYOValidated)).To(BeTrue())
			jobs := &batchv1.JobList{}
			Expect(r.List(ctx, jobs)).To(Succeed())
			Expect(jobs.Items).To(BeEmpty())
		}
	})

	It("UT-AUDIT-515-LIFECYCLE-003: surfaces each deployment write failure without claiming ServicesDeployed", func() {
		kn := newMinimalCR()
		seen := make(map[string]bool)
		r := businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch,
			object client.Object, opts ...client.CreateOption) error {
			seen[fmt.Sprintf("%T/%s", object, object.GetName())] = true
			return api.Create(ctx, object, opts...)
		}})
		Expect(r.phaseDeploy(ctx, kn, kn)).To(Succeed())
		Expect(seen).To(HaveKey("*v1.Deployment/data-storage"))
		for target := range seen {
			kn = newMinimalCR()
			injected := false
			r = businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch,
				object client.Object, opts ...client.CreateOption) error {
				if fmt.Sprintf("%T/%s", object, object.GetName()) == target {
					injected = true
					return fmt.Errorf("injected create failure for %s", target)
				}
				return api.Create(ctx, object, opts...)
			}})
			Expect(r.phaseDeploy(ctx, kn, kn)).To(MatchError(ContainSubstring("injected create failure")), target)
			Expect(injected).To(BeTrue(), target)
			refreshBusinessUnitCR(r, kn)
			Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionServicesDeployed)).To(BeFalse(), target)
		}
	})
})

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/zapr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("truthful reconciliation audit", Label("unit"), func() {
	It("UT-AUDIT-515-AUDIT-001: logs committed phase transitions with old/new values and CR identity", func() {
		kn := newMinimalCR()
		kn.Generation = 7
		kn.Status.Phase = kubernautv1alpha2.PhaseDeploying
		api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithStatusSubresource(kn).WithObjects(kn).Build()
		Expect(api.Get(ctx, client.ObjectKeyFromObject(kn), kn)).To(Succeed())
		core, logs := observer.New(zap.InfoLevel)
		logCtx := logf.IntoContext(ctx, zapr.NewLogger(zap.New(core)))
		r := &KubernautReconciler{Client: api}
		Expect(r.patchStatus(logCtx, kn, func() { r.setPhase(kn, kubernautv1alpha2.PhaseRunning) })).To(Succeed())
		transitions := logs.FilterMessage("reconciliation phase transitioned").All()
		Expect(transitions).To(HaveLen(1))
		Expect(transitions[0].ContextMap()).To(HaveKeyWithValue("oldPhase", kubernautv1alpha2.PhaseDeploying))
		Expect(transitions[0].ContextMap()).To(HaveKeyWithValue("newPhase", kubernautv1alpha2.PhaseRunning))
		Expect(transitions[0].ContextMap()).To(HaveKeyWithValue("namespace", kn.Namespace))
		Expect(transitions[0].ContextMap()).To(HaveKeyWithValue("generation", BeEquivalentTo(7)))
		Expect(transitions[0].ContextMap()).To(HaveKeyWithValue("resourceVersion", kn.ResourceVersion))
	})

	It("UT-AUDIT-515-AUDIT-002: retains finalizer and records incomplete cleanup even past the old timeout", func() {
		kn := newMinimalCR()
		kn.UID = "audit-cleanup-owner"
		kn.Finalizers = []string{kubernautv1alpha2.FinalizerName}
		kn.DeletionTimestamp = &metav1.Time{Time: time.Now().Add(-time.Hour)}
		role, _ := resources.AnsibleRBAC(kn)
		api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(kn, role).
			WithInterceptorFuncs(interceptor.Funcs{Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
				return fmt.Errorf("injected cleanup failure")
			}}).Build()
		recorder := events.NewFakeRecorder(100)
		r := &KubernautReconciler{Client: api, Scheme: api.Scheme(), Recorder: recorder, now: time.Now}
		_, err, retry := r.runFinalizerCleanup(ctx, kn, kn)
		Expect(err).To(HaveOccurred())
		Expect(retry).To(BeTrue())
		Expect(kn.Finalizers).To(ContainElement(kubernautv1alpha2.FinalizerName))
		messages := make([]string, 0, len(recorder.Events))
		for len(recorder.Events) > 0 {
			messages = append(messages, <-recorder.Events)
		}
		Expect(strings.Join(messages, "\n")).To(ContainSubstring("CleanupFailed"))
		Expect(strings.Join(messages, "\n")).NotTo(ContainSubstring("FinalizerTimeout"))
		Expect(strings.Join(messages, "\n")).NotTo(ContainSubstring("CleanupComplete"))
	})

	It("UT-AUDIT-515-AUDIT-003: keeps CR identity distinct from a cluster-scoped child in reconciliation logs", func() {
		kn := newMinimalCR()
		kn.UID = "audit-distinct-identity"
		kn.Generation = 9
		kn.Finalizers = []string{kubernautv1alpha2.FinalizerName}
		kn.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		r := businessUnitReconciler(kn, interceptor.Funcs{Delete: deniedUnitDelete})
		role, _ := resources.AnsibleRBAC(kn)
		seedBusinessOwnedResources(r, kn, role)
		refreshBusinessUnitCR(r, kn)
		core, logs := observer.New(zap.InfoLevel)
		logCtx := logf.IntoContext(ctx, zapr.NewLogger(zap.New(core)))
		_, err := r.Reconcile(logCtx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
		Expect(err).To(MatchError(ContainSubstring(unitAPIDenial)))
		failures := logs.FilterMessage("resource deletion failed").All()
		Expect(failures).To(HaveLen(1))
		fields := failures[0].ContextMap()
		Expect(fields).To(HaveKeyWithValue("kubernautName", kn.Name))
		Expect(fields).To(HaveKeyWithValue("kubernautNamespace", kn.Namespace))
		Expect(fields).To(HaveKeyWithValue("kubernautUID", BeEquivalentTo(kn.UID)))
		Expect(fields).To(HaveKeyWithValue("kind", "ClusterRole"))
		Expect(fields).To(HaveKeyWithValue("name", role.Name))
		Expect(fields).To(HaveKeyWithValue("namespace", ""))
		Expect(fields).To(HaveKeyWithValue("generation", BeEquivalentTo(kn.Generation)))
		Expect(fields).To(HaveKeyWithValue("resourceVersion", kn.ResourceVersion))
		Expect(fields).To(HaveKeyWithValue("objectResourceVersion", role.ResourceVersion))
	})
})

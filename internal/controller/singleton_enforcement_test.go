package controller

import (
	"context"
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("cluster-wide activation enforcement", Label("unit"), func() {
	It("UT-AUDIT-515-SINGLETON-001: rejects persisted competitors before adding finalizers or running phases", func() {
		first := newMinimalCR()
		first.UID = types.UID("first")
		second := first.DeepCopy()
		second.Namespace = "other-namespace"
		second.UID = types.UID("second")
		api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(first, second).Build()
		r := &KubernautReconciler{Client: api, APIReader: api, Scheme: api.Scheme()}
		for _, kn := range []*kubernautv1alpha2.Kubernaut{first, second} {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
			Expect(err).To(MatchError(ContainSubstring("singleton")))
			live := &kubernautv1alpha2.Kubernaut{}
			Expect(api.Get(ctx, client.ObjectKeyFromObject(kn), live)).To(Succeed())
			Expect(live.Finalizers).To(BeEmpty())
		}
		jobs := &batchv1.JobList{}
		deployments := &appsv1.DeploymentList{}
		Expect(api.List(ctx, jobs)).To(Succeed())
		Expect(api.List(ctx, deployments)).To(Succeed())
		Expect(jobs.Items).To(BeEmpty())
		Expect(deployments.Items).To(BeEmpty())
	})

	It("UT-AUDIT-515-SINGLETON-002: reads competitors from the API even when the manager cache sees only one CR", func() {
		first := newMinimalCR()
		second := first.DeepCopy()
		second.Namespace = "other-namespace"
		cached := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(first).Build()
		api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(first, second).Build()
		r := &KubernautReconciler{Client: cached, APIReader: api, Scheme: cached.Scheme()}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(first)})
		Expect(err).To(MatchError(ContainSubstring("singleton")))
		live := &kubernautv1alpha2.Kubernaut{}
		Expect(cached.Get(ctx, client.ObjectKeyFromObject(first), live)).To(Succeed())
		Expect(live.Finalizers).To(BeEmpty())
	})

	It("UT-AUDIT-515-SINGLETON-003: fails closed on an authoritative list error", func() {
		kn := newMinimalCR()
		api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(kn).
			WithInterceptorFuncs(interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return fmt.Errorf("singleton API unavailable")
			}}).Build()
		r := &KubernautReconciler{Client: api, APIReader: api, Scheme: api.Scheme()}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
		Expect(err).To(MatchError(ContainSubstring("singleton API unavailable")))
	})

	It("UT-AUDIT-515-SINGLETON-004: rejects a cached holder replaced under the same key with a different UID", func() {
		kn := newMinimalCR()
		kn.UID = "stale-holder"
		replacement := kn.DeepCopy()
		replacement.UID = "replacement-holder"
		cached := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(kn).Build()
		api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(replacement).Build()
		r := &KubernautReconciler{Client: cached, APIReader: api}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
		Expect(err).To(MatchError(ContainSubstring("singleton is absent, replaced or terminating")))
		Expect(cached.Get(ctx, client.ObjectKeyFromObject(kn), kn)).To(Succeed())
		Expect(kn.Finalizers).To(BeEmpty())
	})

	It("UT-AUDIT-515-SINGLETON-005: fails closed when cached persistence has disappeared", func() {
		kn := newMinimalCR()
		cached := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(kn).Build()
		api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).Build()
		r := &KubernautReconciler{Client: cached, APIReader: api}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
		Expect(err).To(MatchError(ContainSubstring("singleton")))
	})
})

var _ = Describe("cluster-wide activation through the real API", func() {
	It("IT-AUDIT-515-SINGLETON-001 [AC-3, CM-5, SI-10]: blocks cross-namespace competitors admitted without the webhook", func() {
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "singleton-competitor-"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		first := newMinimalCR()
		second := first.DeepCopy()
		second.Namespace = namespace.Name
		Expect(k8sClient.Create(ctx, first)).To(Succeed())
		Expect(k8sClient.Create(ctx, second)).To(Succeed())
		DeferCleanup(func() {
			for _, kn := range []*kubernautv1alpha2.Kubernaut{first, second} {
				live := &kubernautv1alpha2.Kubernaut{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(kn), live)).To(Succeed())
				live.Finalizers = nil
				Expect(k8sClient.Update(ctx, live)).To(Succeed())
				Expect(k8sClient.Delete(ctx, live)).To(Succeed())
			}
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})
		r := newReconciler()
		for _, kn := range []*kubernautv1alpha2.Kubernaut{first, second} {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
			Expect(err).To(MatchError(ContainSubstring("singleton")))
			live := &kubernautv1alpha2.Kubernaut{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(kn), live)).To(Succeed())
			Expect(live.Finalizers).To(BeEmpty())
			jobs := &batchv1.JobList{}
			deployments := &appsv1.DeploymentList{}
			Expect(k8sClient.List(ctx, jobs, client.InNamespace(kn.Namespace))).To(Succeed())
			Expect(k8sClient.List(ctx, deployments, client.InNamespace(kn.Namespace))).To(Succeed())
			Expect(jobs.Items).To(BeEmpty())
			Expect(deployments.Items).To(BeEmpty())
		}
	})

	It("IT-AUDIT-515-SINGLETON-002: permits the remaining singleton only after its competitor is deleted", func() {
		first := newMinimalCR()
		Expect(k8sClient.Create(ctx, first)).To(Succeed())
		DeferCleanup(func() { deleteCRIfExists(ctx) })
		_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(first)})
		Expect(err).NotTo(HaveOccurred())
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "singleton-deleting-"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, namespace)).To(Succeed()) })
		competitor := newMinimalCR()
		competitor.Namespace = namespace.Name
		Expect(k8sClient.Create(ctx, competitor)).To(Succeed())
		_, err = newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(first)})
		Expect(err).To(MatchError(ContainSubstring("singleton")))
		Expect(k8sClient.Delete(ctx, competitor)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(competitor), &kubernautv1alpha2.Kubernaut{}))
		}, timeout, interval).Should(BeTrue())
		_, err = newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(first)})
		Expect(err).NotTo(HaveOccurred())
		live := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(first), live)).To(Succeed())
		Expect(live.Finalizers).To(ContainElement(kubernautv1alpha2.FinalizerName))
	})

	It("IT-AUDIT-515-SINGLETON-003: concurrently persisted admission-bypass competitors cannot both activate", func() {
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "singleton-race-"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		first := newMinimalCR()
		second := first.DeepCopy()
		second.Namespace = namespace.Name
		DeferCleanup(func() {
			for _, kn := range []*kubernautv1alpha2.Kubernaut{first, second} {
				Expect(k8sClient.Delete(ctx, kn)).To(Succeed())
			}
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for _, kn := range []*kubernautv1alpha2.Kubernaut{first, second} {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				results <- k8sClient.Create(ctx, kn)
			}()
		}
		close(start)
		workers.Wait()
		Expect(<-results).To(Succeed())
		Expect(<-results).To(Succeed())
		for _, kn := range []*kubernautv1alpha2.Kubernaut{first, second} {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)})
			Expect(err).To(MatchError(ContainSubstring("singleton")))
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(kn), kn)).To(Succeed())
			Expect(kn.Finalizers).To(BeEmpty())
			Expect(kn.Status.Phase).To(BeEmpty())
		}
	})

	It("IT-AUDIT-515-SINGLETON-004: a deleting finalizer-held holder blocks takeover until cleanup completes", func() {
		holder := newMinimalCR()
		Expect(k8sClient.Create(ctx, holder)).To(Succeed())
		_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(holder)})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(holder), holder)).To(Succeed())
		Expect(k8sClient.Delete(ctx, holder)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(holder), holder)).To(Succeed())
		Expect(holder.DeletionTimestamp.IsZero()).To(BeFalse())
		Expect(holder.Finalizers).To(ContainElement(kubernautv1alpha2.FinalizerName))
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "singleton-replacement-"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		competitor := newMinimalCR()
		competitor.Namespace = namespace.Name
		Expect(k8sClient.Create(ctx, competitor)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(competitor), competitor)).To(Succeed())
			competitor.Finalizers = nil
			Expect(k8sClient.Update(ctx, competitor)).To(Succeed())
			Expect(k8sClient.Delete(ctx, competitor)).To(Succeed())
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})
		_, err = newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(competitor)})
		Expect(err).To(MatchError(ContainSubstring("singleton")))
		_, err = newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(holder)})
		Expect(err).NotTo(HaveOccurred(), "deletion cleanup must remain possible despite the competitor")
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(holder), &kubernautv1alpha2.Kubernaut{}))
		}, timeout, interval).Should(BeTrue())
		_, err = newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(competitor)})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(competitor), competitor)).To(Succeed())
		Expect(competitor.Finalizers).To(ContainElement(kubernautv1alpha2.FinalizerName))
	})
})

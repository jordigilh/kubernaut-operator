package controller

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/policy"
)

var _ = Describe("native isolation activation safety", Label("unit"), func() {
	It("UT-AUDIT-515-POLICY-001: blocks required isolation and preserves the last policy when discovery is unavailable", func() {
		kn := newMinimalCR()
		Expect(json.Unmarshal([]byte(`{"mode":"Required"}`), &kn.Spec.NetworkPolicies)).To(Succeed())
		intent, err := policy.BuildIntent(kn.Namespace, []string{"kubernaut-agent"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.InstanceName = kn.Name
		objects, err := policy.Render(policy.DetectionResult{Provider: policy.ProviderCilium, Ready: true}, intent)
		Expect(err).NotTo(HaveOccurred())
		lastPolicy := objects[0].Object
		api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithStatusSubresource(kn).WithObjects(kn, lastPolicy).Build()
		r := &KubernautReconciler{Client: api, Scheme: api.Scheme()}
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(MatchError(ContainSubstring("native network isolation")))
		live := lastPolicy.DeepCopy()
		Expect(api.Get(ctx, client.ObjectKeyFromObject(lastPolicy), live)).To(Succeed())
		Expect(live.Object["spec"]).To(Equal(lastPolicy.Object["spec"]))
	})

	It("UT-AUDIT-515-POLICY-002: acknowledges explicit inherited isolation without claiming provider discovery", func() {
		kn := newMinimalCR()
		Expect(json.Unmarshal([]byte(`{"mode":"Inherited"}`), &kn.Spec.NetworkPolicies)).To(Succeed())
		api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithStatusSubresource(kn).WithObjects(kn).Build()
		r := &KubernautReconciler{Client: api, Scheme: api.Scheme()}
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(Succeed())
		live := &kubernautv1alpha2.Kubernaut{}
		Expect(api.Get(ctx, client.ObjectKeyFromObject(kn), live)).To(Succeed())
		condition := meta.FindStatusCondition(live.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("InheritedIsolation"))
		provider := meta.FindStatusCondition(live.Status.Conditions, kubernautv1alpha2.ConditionProviderDetected)
		Expect(provider).NotTo(BeNil())
		Expect(provider.Status).To(Equal(metav1.ConditionFalse))
	})

	It("UT-AUDIT-515-POLICY-003: stops reporting Running after required policy readiness is lost", func() {
		kn := newMinimalCR()
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		kn.Status.Phase = kubernautv1alpha2.PhaseRunning
		api := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithStatusSubresource(kn).WithObjects(kn).Build()
		r := &KubernautReconciler{Client: api, Scheme: api.Scheme()}
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(MatchError(ContainSubstring("native network isolation")))
		live := &kubernautv1alpha2.Kubernaut{}
		Expect(api.Get(ctx, client.ObjectKeyFromObject(kn), live)).To(Succeed())
		Expect(live.Status.Phase).To(Equal(kubernautv1alpha2.PhaseDegraded))
		Expect(meta.IsStatusConditionFalse(live.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)).To(BeTrue())
	})
})

var _ = Describe("required isolation through real reconciliation", func() {
	It("IT-AUDIT-515-POLICY-001 [SC-7, AC-4, CM-6]: blocks migration and workloads when a native provider is not qualified", func() {
		kn := newMinimalCR()
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		Expect(k8sClient.Create(ctx, kn)).To(Succeed())
		DeferCleanup(func() { deleteCRIfExists(ctx) })
		r := newReconciler()
		request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kn)}
		_, err := r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred(), "the first pass only adds the protecting finalizer")
		_, err = r.Reconcile(ctx, request)
		Expect(err).To(MatchError(ContainSubstring("native network isolation")))
		jobs := &batchv1.JobList{}
		Expect(k8sClient.List(ctx, jobs, client.InNamespace(kn.Namespace))).To(Succeed())
		Expect(jobs.Items).To(BeEmpty())
		live := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(kn), live)).To(Succeed())
		Expect(live.Status.Phase).NotTo(Equal(kubernautv1alpha2.PhaseRunning))
		condition := meta.FindStatusCondition(live.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	})
})

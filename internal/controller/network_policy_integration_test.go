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
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/policy"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("portable NetworkPolicy controller wiring", Label("integration"), func() {
	It("IT-NP-555-RECONCILE-001 [AC-3, AC-6, SC-7; SOC2 CC6.1, CC6.6, CC7.2; ASVS V4.1, V4.2, V4.3] reconciles standard policies without provider APIs and preserves API-client access", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		cr.Spec.NetworkPolicies.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{
			Namespace: "monitoring",
			PodLabels: map[string]string{"app": "prometheus"},
		}}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
			cleanupClusterScoped(ctx)
		})

		reconcileToDeployPhase(ctx)

		policies := &networkingv1.NetworkPolicyList{}
		Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace), client.MatchingLabels{"kubernaut.ai/managed-policy": "true"})).To(Succeed())
		Expect(policies.Items).To(HaveLen(expectedNetworkPolicyCount(cr, cr)), "AuthWebhook and Console ingress remain administrator-owned")
		for _, networkPolicy := range policies.Items {
			Expect(networkPolicy.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
			Expect(networkPolicy.Spec.Egress).To(BeEmpty())
			Expect(networkPolicy.Labels["kubernaut.ai/managed-policy"]).To(Equal("true"))
			Expect(networkPolicy.OwnerReferences).To(HaveLen(1))
		}

		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed(), "the same Kubernetes API client remains usable after policy reconciliation")
		condition := findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionNetworkPolicyReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))

		Expect(k8sClient.Get(ctx, singletonKey(), cr)).To(Succeed())
		cr.Spec.APIFrontend.Enabled = ptr.To(false)
		Expect(k8sClient.Update(ctx, cr)).To(Succeed())
		Expect(newReconciler().reconcileNetworkPolicies(ctx, cr, cr)).To(Succeed())
		stale := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "kubernaut-apifrontend"}, stale)).To(MatchError(ContainSubstring("not found")))
	})

	It("IT-NP-555-OWNERSHIP-002 [AC-3, AC-6; SOC2 CC6.6; ASVS V4.1] refuses an administrator-owned same-name policy", func() {
		createBYOSecrets(ctx)
		foreign := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{
			Name: "kubernaut-gateway", Namespace: testNamespace,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "administrator"},
		}}
		existing := &networkingv1.NetworkPolicy{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), existing); err == nil {
			Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), &networkingv1.NetworkPolicy{}))
			}, timeout, interval).Should(BeTrue())
		} else {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		cr := newMinimalCR()
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() {
			deleteCRIfExists(ctx)
			_ = k8sClient.Delete(ctx, foreign)
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
			cleanupClusterScoped(ctx)
		})

		r := newReconciler()
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred(), "the first pass only adds the finalizer")
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: singletonKey()})
		Expect(err).To(MatchError(ContainSubstring("ownership")))
		live := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "administrator"))
		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		Expect(findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionNetworkPolicyReady).Status).To(Equal(metav1.ConditionFalse))
	})

	It("IT-NP-555-LIFECYCLE-003 [AC-3, AC-6, CM-3; SOC2 CC6.6, CC8.1; ASVS V4.1, V4.2] updates and cleans owned policies without deleting user policy", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() {
			deleteCRIfExists(ctx)
			_ = client.IgnoreNotFound(k8sClient.Delete(ctx, &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{
				Name: "user-owned-policy", Namespace: testNamespace,
			}}))
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
			cleanupClusterScoped(ctx)
		})
		reconcileToDeployPhase(ctx)

		before := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "kubernaut-gateway"}, before)).To(Succeed())
		cr = &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), cr)).To(Succeed())
		cr.Spec.NetworkPolicies.Gateway.IngressNamespaces = []string{"monitoring"}
		Expect(k8sClient.Update(ctx, cr)).To(Succeed())
		Expect(newReconciler().reconcileNetworkPolicies(ctx, cr, cr)).To(Succeed())
		after := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(before), after)).To(Succeed())
		Expect(after.Spec.Ingress).NotTo(Equal(before.Spec.Ingress))

		userOwned := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{
			Name: "user-owned-policy", Namespace: testNamespace,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "administrator"},
		}}
		Expect(k8sClient.Create(ctx, userOwned)).To(Succeed())
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Eventually(func() error {
			live := &kubernautv1alpha2.Kubernaut{}
			if err := k8sClient.Get(ctx, singletonKey(), live); apierrors.IsNotFound(err) {
				return nil
			} else if err != nil {
				return err
			}
			_, err := newReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: singletonKey()})
			return err
		}, timeout, interval).Should(Succeed())
		managed := &networkingv1.NetworkPolicyList{}
		Expect(k8sClient.List(ctx, managed, client.InNamespace(testNamespace), client.MatchingLabels{"kubernaut.ai/managed-policy": "true"})).To(Succeed())
		Expect(managed.Items).To(BeEmpty())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(userOwned), &networkingv1.NetworkPolicy{})).To(Succeed())
	})

	It("IT-NP-555-FAILCLOSED-004 [AC-3, SI-4; SOC2 CC7.2; ASVS V4.1, V16.1] exposes invalid selector input and does not apply policies", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		cr.Spec.NetworkPolicies.Gateway.IngressCIDRs = []string{"not-a-cidr"}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
			cleanupClusterScoped(ctx)
		})

		r := newReconciler()
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred(), "the first pass only adds the finalizer")
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: singletonKey()})
		Expect(err).To(MatchError(ContainSubstring("ingress CIDR")))
		policies := &networkingv1.NetworkPolicyList{}
		Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace), client.MatchingLabels{"kubernaut.ai/managed-policy": "true"})).To(Succeed())
		Expect(policies.Items).To(BeEmpty())
		status := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), status)).To(Succeed())
		Expect(findCondition(status.Status.Conditions, kubernautv1alpha2.ConditionNetworkPolicyReady).Status).To(Equal(metav1.ConditionFalse))
	})

	It("IT-NP-555-SCRAPE-005 [AC-3, AC-6, SC-7; SOC2 CC6.1, CC6.6; ASVS V4.1] updates source-qualified metrics ingress through full reconciliation", func() {
		createBYOSecrets(ctx)
		cr := newMinimalCR()
		cr.Spec.NetworkPolicies.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{
			Namespace: "monitoring", PodLabels: map[string]string{"app": "prometheus"},
		}}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		DeferCleanup(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
			cleanupClusterScoped(ctx)
		})
		DeferCleanup(func() {
			deleteCRIfExists(ctx)
			cleanupNamespacedResources(ctx)
			deleteBYOSecrets(ctx)
			cleanupClusterScoped(ctx)
		})
		reconcileToDeployPhase(ctx)
		policyObject := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "kubernaut-kubernaut-agent"}, policyObject)).To(Succeed())
		Expect(policyObject.Spec.Egress).To(BeEmpty())
		Expect(policyObject.Spec.Ingress).To(ContainElement(HaveField("Ports", Not(BeEmpty()))))

		live := &kubernautv1alpha2.Kubernaut{}
		Expect(k8sClient.Get(ctx, singletonKey(), live)).To(Succeed())
		live.Spec.NetworkPolicies.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{
			Namespace: "monitoring", PodLabels: map[string]string{"app": "prometheus", "team": "platform"},
		}}
		Expect(k8sClient.Update(ctx, live)).To(Succeed())
		_, err := newReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: singletonKey()})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policyObject), policyObject)).To(Succeed())
		Expect(policyObject.Spec.Egress).To(BeEmpty())
	})

	It("IT-NP-555-API-ERROR-006 [AC-3, SI-4; SOC2 CC7.2; ASVS V16.1] surfaces real API write and cleanup failures", func() {
		createCR := newMinimalCR()
		createFailure := businessUnitReconciler(createCR, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.CreateOption) error {
			if _, ok := object.(*networkingv1.NetworkPolicy); ok {
				return fmt.Errorf("injected NetworkPolicy create failure")
			}
			return api.Create(ctx, object, options...)
		}})
		Expect(createFailure.reconcileNetworkPolicies(ctx, createCR, createCR)).To(MatchError(ContainSubstring("create failure")))
		getFailure := businessUnitReconciler(newMinimalCR(), interceptor.Funcs{Get: func(ctx context.Context, api client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if _, ok := object.(*networkingv1.NetworkPolicy); ok {
				return fmt.Errorf("injected NetworkPolicy read failure")
			}
			return api.Get(ctx, key, object, options...)
		}})
		Expect(getFailure.reconcileNetworkPolicies(ctx, createCR, createCR)).To(MatchError(ContainSubstring("read failure")))

		updateCR := newMinimalCR()
		updateReconciler := businessUnitReconciler(updateCR, interceptor.Funcs{})
		Expect(updateReconciler.reconcileNetworkPolicies(ctx, updateCR, updateCR)).To(Succeed())
		updateCR.Spec.NetworkPolicies.Gateway.IngressNamespaces = []string{"monitoring"}
		updateFailure := updateReconciler
		updateFailure.Client = interceptor.NewClient(updateReconciler.Client.(client.WithWatch), interceptor.Funcs{Update: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.UpdateOption) error {
			if _, ok := object.(*networkingv1.NetworkPolicy); ok {
				return fmt.Errorf("injected NetworkPolicy update failure")
			}
			return api.Update(ctx, object, options...)
		}})
		Expect(updateFailure.reconcileNetworkPolicies(ctx, updateCR, updateCR)).To(MatchError(ContainSubstring("update failure")))

		listFailure := businessUnitReconciler(newMinimalCR(), interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return fmt.Errorf("injected NetworkPolicy list failure")
		}})
		Expect(listFailure.pruneNetworkPolicies(ctx, testNamespace, kubernautv1alpha2.SingletonName, map[string]struct{}{})).To(MatchError(ContainSubstring("list failure")))
		deleteFailure := businessUnitReconciler(newMinimalCR(), interceptor.Funcs{Delete: deniedUnitDelete})
		policyObject := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{
			Name: "kubernaut-gateway", Namespace: testNamespace,
			Labels: map[string]string{policy.ManagedPolicyLabel: "true", policy.ManagedByLabel: "kubernaut-operator", policy.PolicyNamespaceLabel: testNamespace, "app.kubernetes.io/instance": kubernautv1alpha2.SingletonName},
		}}
		Expect(deleteFailure.Create(ctx, policyObject)).To(Succeed())
		Expect(deleteFailure.deleteNetworkPolicies(ctx, testNamespace, kubernautv1alpha2.SingletonName)).NotTo(BeEmpty())
	})

	It("IT-NP-555-WIRING-007 [CM-8, SI-4; SOC2 CC7.2] registers standard policy and optional capability watches", func() {
		mapper := meta.NewDefaultRESTMapper(k8sClient.Scheme().PrioritizedVersionsAllGroups())
		for _, gvk := range []schema.GroupVersionKind{
			{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"},
			{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"},
			{Group: "monitoring.coreos.com", Version: "v1alpha1", Kind: "AlertmanagerConfig"},
			{Group: "config.openshift.io", Version: "v1", Kind: "APIServer"},
		} {
			mapper.Add(gvk, meta.RESTScopeNamespace)
		}
		manager, err := ctrl.NewManager(&rest.Config{Host: "https://unit.invalid"}, ctrl.Options{
			Scheme: k8sClient.Scheme(), Controller: controllerconfig.Controller{SkipNameValidation: ptr.To(true)},
			MapperProvider: func(*rest.Config, *http.Client) (meta.RESTMapper, error) { return mapper, nil },
		})
		Expect(err).NotTo(HaveOccurred())
		Expect((&KubernautReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}).SetupWithManager(manager)).To(Succeed())
	})

	It("IT-NP-555-WIRING-008 [CM-8; SOC2 CC7.2] evaluates optional API discovery explicitly", func() {
		Expect(monitoringResourceSupported(nil, "ServiceMonitor")).To(BeFalse())
		Expect(openShiftAPIServerWatchSupported(nil)).To(BeFalse())
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "monitoring.coreos.com", Version: "v1"}, {Group: "config.openshift.io", Version: "v1"}})
		mapper.Add(schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"}, meta.RESTScopeNamespace)
		mapper.Add(schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "APIServer"}, meta.RESTScopeRoot)
		Expect(monitoringResourceSupported(mapper, "ServiceMonitor")).To(BeTrue())
		Expect(openShiftAPIServerWatchSupported(mapper)).To(BeTrue())
	})

	It("IT-NP-555-COVERAGE-009 [CM-3; SOC2 CC8.1] recognizes a correctable pending BYO audit-key marker", func() {
		state := &corev1.ConfigMap{Data: map[string]string{
			resources.DataStorageAuditHashKeyStatePhaseKey: resources.DataStorageAuditHashKeyStateProvisioning,
			"source": resources.DataStorageAuditHashKeySourceExisting,
		}}
		ref := resources.DataStorageAuditHashKeyReference{Generated: false}
		Expect(canCorrectPendingBYOIdentity(state, ref)).To(BeTrue())
		state.Data["keyFingerprint"] = "fingerprint"
		Expect(canCorrectPendingBYOIdentity(state, ref)).To(BeFalse())
		ref.Generated = true
		delete(state.Data, "keyFingerprint")
		Expect(canCorrectPendingBYOIdentity(state, ref)).To(BeFalse())
	})
})

package controller

import (
	"context"
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/policy"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("portable NetworkPolicy unit lifecycle", Label("unit"), func() {
	It("UT-NP-555-CTRL-001 [AC-3, AC-6, SC-7; SOC2 CC6.1, CC6.6; ASVS V4.1, V4.2] applies, updates, and prunes selector policies", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})

		Expect(r.reconcileNetworkPolicies(ctx, kn, kn)).To(Succeed())
		policies := &networkingv1.NetworkPolicyList{}
		Expect(r.List(ctx, policies, client.InNamespace(kn.Namespace))).To(Succeed())
		Expect(policies.Items).To(HaveLen(len(resources.ActiveComponents(kn, kn))))
		for _, object := range policies.Items {
			Expect(object.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
			Expect(object.Spec.Egress).To(BeEmpty())
		}

		kn.Spec.NetworkPolicies.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{
			Namespace: "monitoring", PodLabels: map[string]string{"app": "prometheus"},
		}}
		Expect(r.reconcileNetworkPolicies(ctx, kn, kn)).To(Succeed())
		Expect(r.reconcileNetworkPolicies(ctx, kn, kn)).To(Succeed(), "a matching hash must make the second reconcile idempotent")

		kn.Spec.APIFrontend.Enabled = ptr.To(false)
		Expect(r.reconcileNetworkPolicies(ctx, kn, kn)).To(Succeed())
		stale := &networkingv1.NetworkPolicy{}
		Expect(r.Get(ctx, client.ObjectKey{Namespace: kn.Namespace, Name: "kubernaut-apifrontend"}, stale)).To(MatchError(ContainSubstring("not found")))
		Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionNetworkPolicyReady)).To(BeTrue())
	})

	It("UT-NP-555-CTRL-002 [AC-3, AC-6; SOC2 CC6.6; ASVS V4.1] preserves administrator policy ownership and surfaces failure status", func() {
		kn := newMinimalCR()
		foreign := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{
			Name: "kubernaut-gateway", Namespace: kn.Namespace,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "administrator"},
		}}
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(r.Create(ctx, foreign)).To(Succeed())
		Expect(r.reconcileNetworkPolicies(ctx, kn, kn)).To(MatchError(ContainSubstring("ownership")))
		live := &networkingv1.NetworkPolicy{}
		Expect(r.Get(ctx, client.ObjectKeyFromObject(foreign), live)).To(Succeed())
		Expect(live.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "administrator"))
		Expect(meta.IsStatusConditionFalse(kn.Status.Conditions, kubernautv1alpha2.ConditionNetworkPolicyReady)).To(BeTrue())

		invalid := newMinimalCR()
		invalid.Spec.NetworkPolicies.Gateway.IngressCIDRs = []string{"not-a-cidr"}
		invalidReconciler := businessUnitReconciler(invalid, interceptor.Funcs{})
		Expect(invalidReconciler.reconcileNetworkPolicies(ctx, invalid, invalid)).To(MatchError(ContainSubstring("CIDR")))
		Expect(meta.IsStatusConditionFalse(invalid.Status.Conditions, kubernautv1alpha2.ConditionNetworkPolicyReady)).To(BeTrue())
	})

	It("UT-NP-555-CTRL-003 [AC-3, SI-4; SOC2 CC7.2; ASVS V16.1] preserves existing policy state on API failures", func() {
		kn := newMinimalCR()
		readFailure := businessUnitReconciler(kn, interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return fmt.Errorf("injected NetworkPolicy read failure")
		}})
		Expect(readFailure.reconcileNetworkPolicies(ctx, kn, kn)).To(MatchError(ContainSubstring("read failure")))

		createFailure := businessUnitReconciler(kn.DeepCopy(), interceptor.Funcs{Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
			return fmt.Errorf("injected NetworkPolicy create failure")
		}})
		Expect(createFailure.reconcileNetworkPolicies(ctx, kn.DeepCopy(), kn.DeepCopy())).To(MatchError(ContainSubstring("create failure")))
	})

	It("UT-NP-555-CTRL-004 [AC-3, SI-4; SOC2 CC6.6, CC7.2] covers clean-failure and ownership-safe lifecycle branches", func() {
		invalidNamespace := newMinimalCR()
		invalidNamespace.Namespace = ""
		Expect(businessUnitReconciler(invalidNamespace, interceptor.Funcs{}).reconcileNetworkPolicies(ctx, invalidNamespace, invalidNamespace)).To(HaveOccurred())

		invalidPort := newMinimalCR()
		invalidPort.Spec.APIFrontend.MetricsPort = ptr.To[int32](-1)
		Expect(businessUnitReconciler(invalidPort, interceptor.Funcs{}).reconcileNetworkPolicies(ctx, invalidPort, invalidPort)).To(MatchError(ContainSubstring("invalid metrics port")))

		listFailure := newMinimalCR()
		listReconciler := businessUnitReconciler(listFailure, interceptor.Funcs{List: func(ctx context.Context, api client.WithWatch, list client.ObjectList, options ...client.ListOption) error {
			if _, ok := list.(*networkingv1.NetworkPolicyList); ok {
				return fmt.Errorf("injected NetworkPolicy list failure")
			}
			return api.List(ctx, list, options...)
		}})
		Expect(listReconciler.reconcileNetworkPolicies(ctx, listFailure, listFailure)).To(MatchError(ContainSubstring("list failure")))

		schemeFailure := newMinimalCR()
		schemeReconciler := businessUnitReconciler(schemeFailure, interceptor.Funcs{})
		schemeReconciler.Scheme = runtime.NewScheme()
		Expect(schemeReconciler.reconcileNetworkPolicies(ctx, schemeFailure, schemeFailure)).To(HaveOccurred())

		desiredIntent, err := policy.BuildIntent(testNamespace, []string{"gateway"})
		Expect(err).NotTo(HaveOccurred())
		rendered, err := policy.Render(desiredIntent)
		Expect(err).NotTo(HaveOccurred())
		raced := businessUnitReconciler(newMinimalCR(), interceptor.Funcs{Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
			return apierrors.NewAlreadyExists(schema.GroupResource{Group: "networking.k8s.io", Resource: "networkpolicies"}, "kubernaut-gateway")
		}})
		Expect(raced.ensureNetworkPolicy(ctx, rendered[0].Object)).To(HaveOccurred())
		racedSuccess := businessUnitReconciler(newMinimalCR(), interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.CreateOption) error {
			if err := api.Create(ctx, object, options...); err != nil {
				return err
			}
			return apierrors.NewAlreadyExists(schema.GroupResource{Group: "networking.k8s.io", Resource: "networkpolicies"}, object.GetName())
		}})
		Expect(racedSuccess.ensureNetworkPolicy(ctx, rendered[0].Object.DeepCopy())).To(Succeed())

		updateFailure := businessUnitReconciler(newMinimalCR(), interceptor.Funcs{Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
			return fmt.Errorf("injected NetworkPolicy update failure")
		}})
		live := rendered[0].Object.DeepCopy()
		live.Namespace = testNamespace
		live.Labels = map[string]string{policy.ManagedPolicyLabel: "true", policy.ManagedByLabel: "kubernaut-operator", policy.PolicyNamespaceLabel: testNamespace, "app.kubernetes.io/instance": "kubernaut"}
		Expect(updateFailure.Create(ctx, live)).To(Succeed())
		live.Spec.PodSelector.MatchLabels["app"] = "changed"
		Expect(updateFailure.ensureNetworkPolicy(ctx, live)).To(MatchError(ContainSubstring("update failure")))

		statusFailure := businessUnitReconciler(newMinimalCR(), interceptor.Funcs{SubResourcePatch: deniedUnitStatus})
		Expect(statusFailure.patchNetworkPolicyStatus(ctx, newMinimalCR(), false, fmt.Errorf("status failure"))).To(HaveOccurred())
		running := newMinimalCR()
		running.Status.Phase = kubernautv1alpha2.PhaseRunning
		Expect(businessUnitReconciler(running, interceptor.Funcs{}).patchNetworkPolicyStatus(ctx, running, false, fmt.Errorf("policy lost"))).To(HaveOccurred())

		deleteFailure := businessUnitReconciler(newMinimalCR(), interceptor.Funcs{Delete: deniedUnitDelete})
		deleteObject := rendered[0].Object.DeepCopy()
		deleteObject.Namespace = testNamespace
		deleteObject.Labels = live.Labels
		Expect(deleteFailure.Create(ctx, deleteObject)).To(Succeed())
		Expect(deleteFailure.pruneNetworkPolicies(ctx, testNamespace, "kubernaut", map[string]struct{}{})).To(HaveOccurred())
		Expect(deleteFailure.deleteNetworkPolicies(ctx, testNamespace, "kubernaut")).NotTo(BeEmpty())
		cleanupFailure := businessUnitReconciler(newMinimalCR(), interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return fmt.Errorf("injected cleanup list failure")
		}})
		Expect(cleanupFailure.deleteNetworkPolicies(ctx, testNamespace, "kubernaut")).To(ContainElement(MatchError(ContainSubstring("cleanup list failure"))))
		notFound := businessUnitReconciler(newMinimalCR(), interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return apierrors.NewNotFound(schema.GroupResource{Group: "networking.k8s.io", Resource: "networkpolicies"}, testNamespace)
		}})
		Expect(notFound.deleteNetworkPolicies(ctx, testNamespace, "kubernaut")).To(BeEmpty())
	})

	It("UT-NP-555-CTRL-005 [AC-3, SI-4; SOC2 CC7.2] blocks phase activation on policy failure and propagates deployment errors", func() {
		invalid := newMinimalCR()
		invalid.Spec.NetworkPolicies.Gateway.IngressCIDRs = []string{"invalid"}
		_, phaseErr := businessUnitReconciler(invalid, interceptor.Funcs{}).reconcilePhases(ctx, invalid, invalid)
		Expect(phaseErr).To(MatchError(ContainSubstring("ingress CIDR")))

		kn := newMinimalCR()
		job, err := resources.MigrationJob(kn)
		Expect(err).NotTo(HaveOccurred())
		kn.Status.LastMigrationHash = resources.SpecHash(job)
		deployFailure := businessUnitReconciler(kn, interceptor.Funcs{Create: func(ctx context.Context, api client.WithWatch, object client.Object, options ...client.CreateOption) error {
			if _, ok := object.(*appsv1.Deployment); ok {
				return fmt.Errorf("injected deployment failure")
			}
			return api.Create(ctx, object, options...)
		}})
		_, phaseErr = deployFailure.reconcilePhases(ctx, kn, kn)
		Expect(phaseErr).To(MatchError(ContainSubstring("deployment failure")))
	})

	It("UT-NP-555-CTRL-006 [CM-8, SI-4; SOC2 CC7.2] wires the controller and optional capability mappings", func() {
		mapper := meta.NewDefaultRESTMapper(k8sClient.Scheme().PrioritizedVersionsAllGroups())
		for _, gvk := range []schema.GroupVersionKind{
			{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"},
			{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"},
			{Group: "monitoring.coreos.com", Version: "v1alpha1", Kind: "AlertmanagerConfig"},
			{Group: configv1.GroupName, Version: "v1", Kind: "APIServer"},
		} {
			mapper.Add(gvk, meta.RESTScopeNamespace)
		}
		manager, err := ctrl.NewManager(&rest.Config{Host: "https://unit.invalid"}, ctrl.Options{
			Scheme: k8sClient.Scheme(), Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0",
			MapperProvider: func(*rest.Config, *http.Client) (meta.RESTMapper, error) { return mapper, nil },
		})
		Expect(err).NotTo(HaveOccurred())
		reconciler := &KubernautReconciler{}
		Expect(reconciler.SetupWithManager(manager)).To(Succeed())
		Expect(reconciler.APIReader).NotTo(BeNil())
		Expect(monitoringResourceSupported(mapper, "ServiceMonitor")).To(BeTrue())
		Expect(openShiftAPIServerWatchSupported(mapper)).To(BeTrue())

		kn := newMinimalCR()
		api := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(api.apiServerToKubernaut(ctx, &configv1.APIServer{})).To(HaveLen(1))
	})

	It("UT-NP-555-CTRL-007 [AC-6; SOC2 CC6.6; ASVS V4.1] preserves foreign policies and reports delete failures", func() {
		kn := newMinimalCR()
		foreign := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{
			Name: "foreign", Namespace: kn.Namespace,
			Labels: map[string]string{policy.ManagedPolicyLabel: "true", policy.ManagedByLabel: "administrator", policy.PolicyNamespaceLabel: kn.Namespace, "app.kubernetes.io/instance": kn.Name},
		}}
		preserve := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(preserve.Create(ctx, foreign)).To(Succeed())
		Expect(preserve.deleteObservedNetworkPolicy(ctx, foreign)).To(Succeed())
		Expect(preserve.Get(ctx, client.ObjectKeyFromObject(foreign), &networkingv1.NetworkPolicy{})).To(Succeed())

		denied := businessUnitReconciler(kn, interceptor.Funcs{Delete: deniedUnitDelete})
		owned := foreign.DeepCopy()
		owned.ResourceVersion = ""
		owned.Labels[policy.ManagedByLabel] = "kubernaut-operator"
		Expect(denied.Create(ctx, owned)).To(Succeed())
		Expect(denied.deleteObservedNetworkPolicy(ctx, owned)).To(HaveOccurred())
	})
})

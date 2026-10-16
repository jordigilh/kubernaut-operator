package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

type businessMappedClient struct {
	client.Client
	mapper meta.RESTMapper
}

const policyDeleteOperation = "delete"

func (c businessMappedClient) RESTMapper() meta.RESTMapper { return c.mapper }

func ciliumBusinessReconciler(kn *kubernautv1alpha2.Kubernaut, faults interceptor.Funcs) *KubernautReconciler {
	r := businessUnitReconciler(kn, faults)
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "cilium.io", Version: "v2"}})
	mapper.Add(policy.RequiredGVKs(policy.ProviderCilium)[0], meta.RESTScopeNamespace)
	r.Client = businessMappedClient{Client: r.Client, mapper: mapper}
	ExpectWithOffset(1, r.Create(ctx, ciliumPolicyCRD())).To(Succeed())
	ExpectWithOffset(1, r.Create(ctx, &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cilium", Namespace: "kube-system"},
		Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "cilium", Image: "quay.io/cilium/cilium:v1.19.2"}},
		}}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, NumberReady: 1},
	})).To(Succeed())
	return r
}

var _ = Describe("independent provider policy lifecycle", Label("unit"), func() {
	It("UT-AUDIT-515-NATIVE-001: discovers, creates, retains, repairs and prunes exact native policies independently of envtest", func() {
		kn := newMinimalCR()
		kn.UID = "unit-native"
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		kn.Spec.NetworkPolicies.Provider = kubernautv1alpha2.NetworkPolicyProviderCilium
		kn.Spec.NetworkPolicies.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{Namespace: "metrics", PodLabels: map[string]string{"app": "prometheus"}}}
		kn.Spec.APIFrontend.MetricsPort = ptr.To[int32](9180)
		kn.Spec.Monitoring.Prometheus.Enabled = ptr.To(false)
		kn.Spec.Monitoring.AlertManager.Enabled = ptr.To(false)
		r := ciliumBusinessReconciler(kn, interceptor.Funcs{})
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionProviderDetected)).To(BeTrue())
		Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)).To(BeTrue())
		Expect(generatedMetricsPorts(kn, kn)).To(HaveKeyWithValue(resources.ComponentAPIFrontend, int32(9180)))
		Expect(generatedMetricsPorts(kn, kn)).NotTo(HaveKey(resources.ComponentAuthWebhook))
		policies := &unstructured.UnstructuredList{}
		policies.SetAPIVersion("cilium.io/v2")
		policies.SetKind("CiliumNetworkPolicyList")
		Expect(r.List(ctx, policies)).To(Succeed())
		Expect(policies.Items).To(HaveLen(len(resources.ActiveComponents(kn, kn))))
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(Succeed(), "the same desired policies must be retained")
		live := &policies.Items[0]
		live.SetAnnotations(nil)
		Expect(r.Update(ctx, live)).To(Succeed())
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(Succeed())
		stale := policies.Items[0].DeepCopy()
		stale.SetName("stale-native")
		stale.SetResourceVersion("")
		Expect(r.Create(ctx, stale)).To(Succeed())
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(Succeed())
		Expect(apierrors.IsNotFound(r.Get(ctx, client.ObjectKeyFromObject(stale), stale))).To(BeTrue())
		kn.Spec.Monitoring.Prometheus.Enabled = ptr.To(true)
		kn.Spec.Monitoring.Prometheus.URL = "https://missing.metrics.svc:9090"
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(Succeed(), "partial endpoint resolution is represented in status")
		Expect(meta.IsStatusConditionFalse(kn.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)).To(BeTrue())
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationInherited
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(MatchError(ContainSubstring("inherited isolation")))
	})

	It("UT-AUDIT-515-NATIVE-002: rejects native read, create, raced-read, update and pruning failures", func() {
		kn := newMinimalCR()
		desired := nativePolicyOwnershipFixture()
		desired.SetNamespace(kn.Namespace)
		for _, operation := range []string{"get", "create", "race", "update", "list", policyDeleteOperation} {
			r := businessUnitReconciler(kn, interceptor.Funcs{})
			r.Client = businessMappedClient{Client: r.Client, mapper: meta.NewDefaultRESTMapper(nil)}
			if operation == "update" || operation == policyDeleteOperation {
				Expect(r.Create(ctx, desired.DeepCopy())).To(Succeed())
			}
			faults := interceptor.Funcs{}
			switch operation {
			case "get":
				faults.Get = func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					return fmt.Errorf("injected read")
				}
			case "create":
				faults.Create = func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
					return fmt.Errorf("injected create")
				}
			case "race":
				faults.Create = func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
					return apierrors.NewAlreadyExists(schema.GroupResource{Group: "cilium.io", Resource: "ciliumnetworkpolicies"}, desired.GetName())
				}
			case "update":
				faults.Update = func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
					return fmt.Errorf("injected update")
				}
			case "list":
				faults.List = func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					return fmt.Errorf("injected list")
				}
			case policyDeleteOperation:
				faults.Delete = func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
					return fmt.Errorf("injected delete")
				}
			}
			r.Client = interceptor.NewClient(r.Client.(businessMappedClient).Client.(client.WithWatch), faults)
			if operation == "list" || operation == policyDeleteOperation {
				Expect(r.pruneProviderPolicies(ctx, desired.GetLabels()[policy.PolicyNamespaceLabel], desired.GetLabels()["app.kubernetes.io/instance"], nil)).To(HaveOccurred(), operation)
			} else {
				Expect(r.ensureProviderPolicy(ctx, desired.DeepCopy())).To(HaveOccurred(), operation)
			}
		}
	})

	It("UT-AUDIT-515-NATIVE-003: reports discovered-provider status separately from incomplete policy readiness", func() {
		kn := newMinimalCR()
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		detection := policy.DetectionResult{Provider: policy.ProviderCilium, Ready: true, Reason: policy.ReasonProviderReady,
			PolicyReason: policy.ReasonMonitoringUnavailable, PolicyMessage: "missing endpoint", Message: "provider detected"}
		Expect(r.patchProviderPolicyStatus(ctx, kn, detection, false, nil)).To(Succeed())
		Expect(meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady).Message).To(Equal("missing endpoint"))
		Expect(meta.IsStatusConditionTrue(kn.Status.Conditions, kubernautv1alpha2.ConditionProviderDetected)).To(BeTrue())
		Expect(r.resolveMonitoringDestinations(ctx, kn, policy.DetectionResult{})).To(BeEmpty())
		Expect(providerSupportsMonitoring(policy.ProviderNone)).To(BeFalse())
	})

	It("UT-AUDIT-515-NATIVE-004: fails closed on invalid generated scraper ports and tolerates absent optional APIs during pruning", func() {
		kn := newMinimalCR()
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		kn.Spec.NetworkPolicies.Provider = kubernautv1alpha2.NetworkPolicyProviderCilium
		kn.Spec.NetworkPolicies.ScrapeSources = []kubernautv1alpha2.MetricsScrapeSource{{Namespace: "metrics", PodLabels: map[string]string{"app": "prometheus"}}}
		kn.Spec.APIFrontend.MetricsPort = ptr.To[int32](-1)
		kn.Spec.Monitoring.Prometheus.Enabled = ptr.To(false)
		kn.Spec.Monitoring.AlertManager.Enabled = ptr.To(false)
		r := ciliumBusinessReconciler(kn, interceptor.Funcs{})
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(HaveOccurred())
		Expect(meta.IsStatusConditionFalse(kn.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)).To(BeTrue())
		r = businessUnitReconciler(kn, interceptor.Funcs{List: func(context.Context, client.WithWatch,
			client.ObjectList, ...client.ListOption) error {
			return &meta.NoKindMatchError{GroupKind: schema.GroupKind{Kind: "absent optional policy"}}
		}})
		Expect(r.pruneProviderPolicies(ctx, kn.Namespace, kn.Name, nil)).To(Succeed())
	})

	It("UT-AUDIT-515-NATIVE-005: resolves OpenShift OVN DNS isolation to openshift-dns rather than generic kube-system", func() {
		kn := newMinimalCR()
		kn.Spec.NetworkPolicies.Mode = kubernautv1alpha2.NetworkIsolationRequired
		kn.Spec.NetworkPolicies.Provider = kubernautv1alpha2.NetworkPolicyProviderOVN
		kn.Spec.Monitoring.Prometheus.Enabled = ptr.To(false)
		kn.Spec.Monitoring.AlertManager.Enabled = ptr.To(false)
		r := businessUnitReconciler(kn, interceptor.Funcs{})
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{policy.RequiredGVKs(policy.ProviderOVN)[0].GroupVersion()})
		for _, gvk := range policy.RequiredGVKs(policy.ProviderOVN) {
			mapper.Add(gvk, meta.RESTScopeRoot)
		}
		r.Client = businessMappedClient{Client: r.Client, mapper: mapper}
		for _, object := range []client.Object{
			ovnPolicyCRD("AdminNetworkPolicy", "adminnetworkpolicies", true),
			ovnPolicyCRD("BaselineAdminNetworkPolicy", "baselineadminnetworkpolicies", false),
			&configv1.Network{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}, Spec: configv1.NetworkSpec{NetworkType: "OVNKubernetes"}},
			&configv1.ClusterVersion{ObjectMeta: metav1.ObjectMeta{Name: "version"},
				Status: configv1.ClusterVersionStatus{Desired: configv1.Release{Version: "4.22.16"}}},
		} {
			Expect(r.Create(ctx, object)).To(Succeed())
		}
		dnsService := ovnDNSServiceFixture()
		Expect(r.Create(ctx, dnsService)).To(Succeed())
		Expect(r.Create(ctx, ovnDNSEndpointSliceFixture(dnsService))).To(Succeed())
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(Succeed())
		policies := &unstructured.UnstructuredList{}
		policies.SetAPIVersion("policy.networking.k8s.io/v1alpha1")
		policies.SetKind("AdminNetworkPolicyList")
		Expect(r.List(ctx, policies)).To(Succeed())
		Expect(policies.Items).NotTo(BeEmpty())
		Expect(fmt.Sprint(policies.Items)).To(ContainSubstring(resources.OCPDNSNamespace))
		Expect(fmt.Sprint(policies.Items)).To(ContainSubstring("test.kubernaut.ai/dns-backend"))
		Expect(r.Delete(ctx, dnsService)).To(Succeed())
		Expect(r.reconcileProviderPolicies(ctx, kn, kn)).To(MatchError(ContainSubstring("dns-default")))
		condition := meta.FindStatusCondition(kn.Status.Conditions, kubernautv1alpha2.ConditionProviderPolicyReady)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(policy.ReasonDNSUnavailable))
	})

	It("UT-AUDIT-515-SCRAPE-007: derives custom metrics authorization from generated Services and excludes metrics-less components", func() {
		kn := newMinimalCR()
		kn.Spec.APIFrontend.MetricsPort = ptr.To[int32](19180)
		ports := generatedMetricsPorts(kn, kn)
		Expect(ports).To(HaveKeyWithValue(resources.ComponentAPIFrontend, int32(19180)))
		Expect(ports).NotTo(HaveKey(resources.ComponentAuthWebhook))
		Expect(ports).NotTo(HaveKey(resources.ComponentConsole))
		kn.Spec.APIFrontend.Enabled = ptr.To(false)
		Expect(generatedMetricsPorts(kn, kn)).NotTo(HaveKey(resources.ComponentAPIFrontend))
	})

	DescribeTable("UT-AUDIT-518-DNS-WATCH-001: requeues only eligible instances on exact DNS dependency changes",
		func(provider kubernautv1alpha2.NetworkPolicyProvider, mode string, expected bool) {
			kn := newMinimalCR()
			kn.Spec.NetworkPolicies.Provider = provider
			kn.Spec.NetworkPolicies.Mode = mode
			kn.Spec.Monitoring.Prometheus.Enabled = ptr.To(false)
			kn.Spec.Monitoring.AlertManager.Enabled = ptr.To(false)
			r := businessUnitReconciler(kn, interceptor.Funcs{})
			service := ovnDNSServiceFixture()
			for _, object := range []client.Object{service, ovnDNSEndpointSliceFixture(service)} {
				requests := r.monitoringObjectToKubernaut(ctx, object)
				if expected {
					Expect(requests).To(HaveLen(1))
					Expect(requests[0].NamespacedName).To(Equal(client.ObjectKeyFromObject(kn)))
				} else {
					Expect(requests).To(BeEmpty())
				}
			}
			wrongNamespace := service.DeepCopy()
			wrongNamespace.Namespace = "another-dns"
			wrongName := service.DeepCopy()
			wrongName.Name = "another-dns"
			unlabeledSlice := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: service.Name, Namespace: service.Namespace}}
			for _, object := range []client.Object{wrongNamespace, wrongName, unlabeledSlice} {
				Expect(r.monitoringObjectToKubernaut(ctx, object)).To(BeEmpty())
			}
		},
		Entry("default provider", kubernautv1alpha2.NetworkPolicyProvider(""), "", true),
		Entry("Auto", kubernautv1alpha2.NetworkPolicyProviderAuto, kubernautv1alpha2.NetworkIsolationRequired, true),
		Entry("OVN", kubernautv1alpha2.NetworkPolicyProviderOVN, kubernautv1alpha2.NetworkIsolationRequired, true),
		Entry("Inherited", kubernautv1alpha2.NetworkPolicyProviderOVN, kubernautv1alpha2.NetworkIsolationInherited, false),
		Entry("Cilium", kubernautv1alpha2.NetworkPolicyProviderCilium, kubernautv1alpha2.NetworkIsolationRequired, false),
		Entry("Calico", kubernautv1alpha2.NetworkPolicyProviderCalico, kubernautv1alpha2.NetworkIsolationRequired, false),
	)

	It("UT-AUDIT-515-WATCH-001: configures actual controller registration without starting an API server", func() {
		mapper := meta.NewDefaultRESTMapper(k8sClient.Scheme().PrioritizedVersionsAllGroups())
		for gvk := range k8sClient.Scheme().AllKnownTypes() {
			if gvk.Version == "__internal" || gvk.Kind == "" {
				continue
			}
			mapper.Add(gvk, meta.RESTScopeNamespace)
		}
		manager, err := ctrl.NewManager(&rest.Config{Host: "https://unit.invalid"}, ctrl.Options{
			Scheme: k8sClient.Scheme(), Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0",
			MapperProvider: func(*rest.Config, *http.Client) (meta.RESTMapper, error) { return mapper, nil },
		})
		Expect(err).NotTo(HaveOccurred())
		r := &KubernautReconciler{}
		Expect(r.SetupWithManager(manager)).To(Succeed())
		Expect(r.Client).NotTo(BeNil())
		Expect(r.Scheme).NotTo(BeNil())
		Expect(r.RestCfg).NotTo(BeNil())
		Expect(r.APIReader).To(BeIdenticalTo(manager.GetAPIReader()))
		Expect(r.now()).To(BeTemporally("~", time.Now(), time.Second))
		Expect(monitoringResourceSupported(mapper, "ServiceMonitor")).To(BeTrue())
		Expect(openShiftAPIServerWatchSupported(mapper)).To(BeTrue())
		kn := newMinimalCR()
		api := businessUnitReconciler(kn, interceptor.Funcs{})
		Expect(api.apiServerToKubernaut(ctx, &configv1.APIServer{})).To(HaveLen(1))
		Expect(api.monitoringObjectToKubernaut(ctx, nil)).To(BeEmpty())
		Expect(api.monitoringObjectToKubernaut(ctx, &corev1.Service{})).To(BeEmpty())
	})
})

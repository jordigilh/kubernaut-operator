package policy

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

var _ = Describe("native policy boundary decisions", func() {
	It("UT-AUDIT-515-INTENT-001: rejects every deprecated intent lane rather than silently ignoring restrictions", func() {
		_, err := BuildIntent("", []string{"gateway"}, nil)
		Expect(err).To(HaveOccurred())
		_, err = BuildIntent("kubernaut", []string{"", ""}, nil)
		Expect(err).To(HaveOccurred())
		for _, spec := range []kubernautv1alpha2.NetworkPoliciesSpec{
			{APIServerPort: 6443}, {Monitoring: kubernautv1alpha2.NetworkPolicyMonitoringOverride{PrometheusPort: 9090}},
			{IdP: kubernautv1alpha2.NetworkPolicyIdPEgressOverride{ExtraPorts: []int32{8443}}},
			{DataStorage: kubernautv1alpha2.NetworkPolicyIngressOverride{IngressCIDRs: []string{"10.0.0.0/8"}}},
			{KubernautAgent: kubernautv1alpha2.NetworkPolicyIngressOverride{IngressCIDRs: []string{"10.0.0.0/8"}}},
		} {
			Expect(ValidateNativeOverrides(spec)).To(HaveOccurred())
		}
	})

	It("UT-AUDIT-515-RENDER-001: never falls back to policy on failed detection and normalizes legacy instance identity", func() {
		objects, err := Render(DetectionResult{Ready: true}, Intent{StaticAPIServerCIDRs: []string{"10.0.0.1/32"}})
		Expect(err).To(HaveOccurred())
		Expect(objects).To(BeEmpty())
		for _, detection := range []DetectionResult{{Ready: false}, {Ready: true, Provider: ProviderNone}} {
			objects, err = Render(detection, Intent{})
			Expect(err).NotTo(HaveOccurred())
			Expect(objects).To(BeEmpty())
		}
		intent := Intent{Namespace: "kubernaut", Components: []string{"gateway"}}
		intent.DNS = ovnDNSDestinationForTest()
		for _, provider := range []Provider{ProviderCilium, ProviderCalico, ProviderOVN} {
			objects, err = Render(DetectionResult{Ready: true, Provider: provider}, intent)
			Expect(err).NotTo(HaveOccurred())
			Expect(objects).NotTo(BeEmpty())
			Expect(objects[0].Object.GetLabels()).To(HaveKeyWithValue("app.kubernetes.io/instance", "kubernaut"))
		}
		Expect(effectiveDNSNamespace(intent)).To(Equal("kube-system"))
		Expect(endpointSelector("kubernaut", "", "gateway")).To(HaveKey("matchLabels"))
		ordered := ovnMonitoringEgressRules([]MonitoringDestination{
			{Name: MonitoringPrometheus, ServiceName: "prometheus", Namespace: "monitoring", BackendPort: 9090},
			{Name: MonitoringAlertManager, ServiceName: "alertmanager", Namespace: "monitoring", BackendPort: 9093},
		})
		Expect(ordered).To(HaveLen(3))
		Expect(ordered[0]).To(HaveKeyWithValue("name", "allow-monitoring-alertmanager"))
		Expect(ordered[1]).To(HaveKeyWithValue("name", "allow-monitoring-prometheus"))
		Expect(ordered[2]).To(HaveKeyWithValue("action", "Deny"))
	})

	It("UT-AUDIT-515-DISCOVERY-004: rejects unsupported selections and absent active installations", func() {
		Expect(Detect(DiscoverySnapshot{}, Provider("Unknown")).Reason).To(Equal(ReasonUnsupportedProvider))
		Expect(candidateFailureReason(ProviderSnapshot{APIAvailable: true}, ProviderCilium)).To(Equal(ReasonNoActiveInstallation))
		Expect(candidateFailureReason(ProviderSnapshot{Active: true}, ProviderCilium)).To(Equal(ReasonProviderUnavailable))
		Expect(versionSupported(ProviderNone, "1.19.0", "1.19.0")).To(BeFalse())
		daemon := appsv1.DaemonSet{Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "calico", Args: []string{"unrelated", "--datastore-type=kubernetes"}}},
		}}}}
		value, found := calicoDatastoreType(daemon)
		Expect(found).To(BeTrue())
		Expect(value).To(Equal("kubernetes"))
		daemon.Labels = map[string]string{"k8s-app": "cilium"}
		Expect(isProviderDaemonSet(daemon, "cilium")).To(BeTrue())
		Expect(providerVersionFromImages([]corev1.Container{{Image: "image-without-version"}})).To(BeEmpty())
	})
	It("UT-AUDIT-515-RESOLUTION-001: rejects unsupported providers, missing readers and invalid in-cluster URLs", func() {
		endpoint := MonitoringEndpoint{Name: MonitoringPrometheus, URL: "https://prometheus.monitoring.svc:9090"}
		_, err := ResolveMonitoringDestinations(context.Background(), nil, MonitoringResolutionOptions{Provider: ProviderNone})
		Expect(err).To(HaveOccurred())
		Expect(ResolveMonitoringDestinations(context.Background(), nil, MonitoringResolutionOptions{Provider: ProviderCilium})).To(BeEmpty())
		_, err = ResolveMonitoringDestinations(context.Background(), nil, MonitoringResolutionOptions{Provider: ProviderCilium, Endpoints: []MonitoringEndpoint{endpoint}})
		Expect(err).To(MatchError(ContainSubstring("kubernetes client is required")))
		for _, url := range []string{"ftp://prometheus.monitoring.svc", "https://127.0.0.1:9090", "https://prometheus.monitoring.svc:0",
			"https://prometheus.monitoring.svc:65536", "https://1-invalid.monitoring.svc", "https://prometheus.svc"} {
			_, err = MonitoringServiceReferenceFromURL(url)
			Expect(err).To(HaveOccurred(), url)
		}
		reference, err := MonitoringServiceReferenceFromURL("https://prometheus.monitoring.svc.cluster.local:9090")
		Expect(err).NotTo(HaveOccurred())
		Expect(reference).To(Equal(MonitoringServiceReference{Name: "prometheus", Namespace: "monitoring"}))
	})

	It("UT-AUDIT-515-RESOLUTION-002: validates destination identity, uniqueness, service ports and provider-specific narrowing", func() {
		valid := MonitoringDestination{Name: MonitoringPrometheus, ServiceName: "prometheus", Namespace: "monitoring",
			ServicePort: 9090, BackendPort: 8080, ServiceSelector: map[string]string{"app": "prometheus"},
			ServicePorts: []MonitoringServicePort{{Port: 9090, Protocol: "TCP"}}}
		Expect(ValidateMonitoringDestinations(ProviderNone, []MonitoringDestination{valid})).To(HaveOccurred())
		Expect(ValidateMonitoringDestinations(ProviderCilium, []MonitoringDestination{{}})).To(HaveOccurred())
		Expect(ValidateMonitoringDestinations(ProviderCilium, []MonitoringDestination{valid, valid})).To(MatchError(ContainSubstring("more than once")))
		for _, mutate := range []func(*MonitoringDestination){
			func(d *MonitoringDestination) { d.ServicePort = 0 },
			func(d *MonitoringDestination) { d.BackendPort = 0 },
			func(d *MonitoringDestination) { d.ServicePorts = nil },
		} {
			destination := valid
			mutate(&destination)
			Expect(ValidateMonitoringDestinations(ProviderCilium, []MonitoringDestination{destination})).To(HaveOccurred())
		}
		wrongPort := valid
		wrongPort.ServicePorts = []MonitoringServicePort{{Port: 9091, Protocol: "TCP"}}
		Expect(ValidateMonitoringDestinations(ProviderCalico, []MonitoringDestination{wrongPort})).To(MatchError(ContainSubstring("configured service port")))
		noSelector := valid
		noSelector.ServiceSelector = nil
		Expect(ValidateMonitoringDestinations(ProviderOVN, []MonitoringDestination{noSelector})).To(MatchError(ContainSubstring("no Service selector")))
		Expect(validateMonitoringDestination(ProviderNone, valid)).To(Succeed())
	})

	It("UT-AUDIT-515-RESOLUTION-003: normalizes defaults but rejects invalid numeric or named ports", func() {
		port, err := normalizeMonitoringServicePort(corev1.ServicePort{Port: 9090})
		Expect(err).NotTo(HaveOccurred())
		Expect(port.TargetPort).To(Equal(int32(9090)))
		Expect(port.Protocol).To(Equal("TCP"))
		for _, invalid := range []corev1.ServicePort{
			{Port: 0}, {Port: 65536}, {Port: 9090, TargetPort: intstr.FromInt(-1)},
			{Port: 9090, TargetPort: intstr.FromInt(65536)}, {Port: 9090, TargetPort: intstr.FromString("")},
			{Port: 9090, TargetPort: intstr.FromString("not a label")}, {Port: 9090, TargetPort: intstr.IntOrString{Type: 2}},
		} {
			_, err = normalizeMonitoringServicePorts([]corev1.ServicePort{invalid})
			Expect(err).To(HaveOccurred())
		}
		for _, ports := range [][]corev1.ServicePort{nil, {{Port: 0}}, {{Port: 9091}}, {{Port: 9090, Protocol: corev1.ProtocolUDP}}} {
			service := monitoringService(map[string]string{"app": "prometheus"}, ports...)
			_, err = ResolveMonitoringDestinations(context.Background(), monitoringClient(service), MonitoringResolutionOptions{
				Provider: ProviderCilium, Endpoints: []MonitoringEndpoint{{Name: MonitoringPrometheus, URL: "https://prometheus.monitoring.svc:9090"}},
			})
			Expect(err).To(HaveOccurred())
		}
		service := monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{Port: 9090}, corev1.ServicePort{Port: 9091})
		_, _, err = selectMonitoringServicePort(service, 0, ProviderCilium)
		Expect(err).To(MatchError(ContainSubstring("ambiguous")))
	})

	It("UT-AUDIT-515-RESOLUTION-004: rejects duplicate endpoint names instead of retaining the first authorization", func() {
		service := monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{Port: 9090})
		for _, endpoints := range [][]MonitoringEndpoint{
			{{Name: "unknown", URL: "https://prometheus.monitoring.svc:9090"}},
			{{Name: MonitoringPrometheus, URL: "https://prometheus.monitoring.svc:9090"},
				{Name: " PROMETHEUS ", URL: "https://prometheus.monitoring.svc:9090"}},
		} {
			destinations, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(service), MonitoringResolutionOptions{
				Provider: ProviderCilium, Endpoints: endpoints,
			})
			Expect(err).To(HaveOccurred())
			Expect(destinations).To(BeEmpty())
		}
	})

	It("UT-AUDIT-515-RESOLUTION-005: rejects named-port lookup failures and invalid ready backend ports", func() {
		service := monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{Port: 9090})
		_, err := resolveNamedTargetPort(context.Background(), monitoringClient(service), service, MonitoringServicePort{TargetPortName: "web"})
		Expect(err).To(MatchError(ContainSubstring("no name")))
		port := MonitoringServicePort{ServicePortName: "metrics", TargetPortName: "web"}
		api := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithInterceptorFuncs(interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return fmt.Errorf("injected slice failure")
			},
		}).Build()
		_, err = resolveNamedTargetPort(context.Background(), api, service, port)
		Expect(err).To(MatchError(ContainSubstring("injected slice failure")))
		_, err = resolveNamedTargetPort(context.Background(), monitoringClient(service), service, port)
		Expect(err).To(MatchError(ContainSubstring("no ready")))
		for _, invalid := range []discoveryv1.EndpointPort{
			{Name: ptr.To("metrics")}, {Name: ptr.To("metrics"), Port: ptr.To[int32](0)},
			{Name: ptr.To("metrics"), Port: ptr.To[int32](9090), Protocol: ptr.To(corev1.ProtocolUDP)},
		} {
			_, _, err = endpointSliceTargetPort(discoveryv1.EndpointSlice{Ports: []discoveryv1.EndpointPort{invalid}}, "metrics", "web", corev1.ProtocolTCP)
			Expect(err).To(HaveOccurred())
			slice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Name: "ready-invalid", Namespace: service.Namespace,
					Labels: map[string]string{discoveryv1.LabelServiceName: service.Name}},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"},
					Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}}},
				Ports: []discoveryv1.EndpointPort{invalid},
			}
			_, err = resolveNamedTargetPort(context.Background(), monitoringClient(service, slice), service, port)
			Expect(err).To(HaveOccurred())
		}
	})

	It("UT-AUDIT-515-DISCOVERY-001: fails closed without an API client, with failed discovery and malformed versions", func() {
		snapshot := (Detector{}).Discover(context.Background())
		Expect(snapshot.Candidates[ProviderOVN].Diagnostic).To(Equal("Kubernetes client is unavailable"))
		Expect(Detect(snapshot, ProviderAuto).Ready).To(BeFalse())
		api := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithInterceptorFuncs(interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return fmt.Errorf("injected discovery failure")
			},
		}).Build()
		Expect((Detector{Client: api}).Discover(context.Background()).Candidates[ProviderCalico].Active).To(BeFalse())
		for _, version := range []string{"", "1", "99999999999999999999999999999.19", "1.999999999999999999999999999999"} {
			_, _, valid := parseMajorMinor(version)
			Expect(valid).To(BeFalse(), version)
		}
		Expect(versionSupported(ProviderNone, "1.19.0", "")).To(BeFalse())
		Expect(RequiredGVKs(ProviderNone)).To(BeEmpty())
		Expect(ProviderForGVK(schema.GroupVersionKind{Kind: "Unknown"})).To(Equal(ProviderNone))
		Expect(firstFailureReason([]string{"unknown"})).To(Equal(ReasonNoSupportedProvider))
		Expect(versionSupported(ProviderCilium, "not-a-version", "")).To(BeFalse())
		Expect(versionSupported(ProviderOVN, "", "not-a-version")).To(BeFalse())
		Expect(hasRequiredGVKs(nil, ProviderCilium)).To(BeFalse())
	})

	It("UT-AUDIT-515-DISCOVERY-002: rejects unsupported GVKs and malformed schema containers", func() {
		gvk := RequiredGVKs(ProviderCilium)[0]
		for _, root := range []*apiextensionsv1.JSONSchemaProps{nil, {Type: "string"},
			{Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{}},
		} {
			Expect(schemaSupportsPolicy(gvk, root)).To(BeFalse())
		}
		unknown := schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "Unknown"}
		Expect((Detector{}).discoverResource(context.Background(), unknown)).To(BeFalse())
		Expect(schemaSupportsPolicy(unknown, &apiextensionsv1.JSONSchemaProps{Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"spec": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{}},
		}})).To(BeFalse())
		for _, egress := range []apiextensionsv1.JSONSchemaProps{{Type: "string"}, {Type: "array"},
			{Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"}}},
		} {
			Expect(schemaSupportsMonitoringFields(gvk, egress)).To(BeFalse())
		}
		Expect(schemaSupportsMonitoringFields(unknown, apiextensionsv1.JSONSchemaProps{Type: "array",
			Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{}}},
		})).To(BeTrue())
		crd := testProviderCRD(gvk, "ciliumnetworkpolicies", apiextensionsv1.NamespaceScoped)
		crd.Spec.Versions[0].Served = false
		Expect(crdHasSupportedSchema(crd, gvk)).To(BeFalse())
		crd.Spec.Group = "wrong.example"
		api := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(crd).Build()
		Expect((Detector{Client: api, RESTMapper: testRESTMapper(gvk, meta.RESTScopeNamespace)}).discoverResource(context.Background(), gvk)).To(BeFalse())
		api = fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return fmt.Errorf("discovery API denied")
			},
		}).Build()
		Expect((Detector{Client: api, RESTMapper: testRESTMapper(gvk, meta.RESTScopeNamespace)}).discoverResource(context.Background(), gvk)).To(BeFalse())
	})

	It("UT-AUDIT-515-DISCOVERY-003: preserves legacy OVN configuration support without accepting another network plugin", func() {
		for _, plugin := range []string{"OVNKubernetes", "other"} {
			network := &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "config.openshift.io/v1", "kind": "Network", "metadata": map[string]interface{}{"name": "cluster"},
				"spec": map[string]interface{}{"defaultNetwork": map[string]interface{}{"type": plugin}},
			}}
			api := fake.NewClientBuilder().WithScheme(testDiscoveryScheme()).WithRuntimeObjects(network).Build()
			candidate := (Detector{Client: api}).discoverOVN(context.Background())
			Expect(candidate.Active).To(Equal(plugin == "OVNKubernetes"))
			Expect(candidate.SchemaValid).To(BeFalse())
		}
		daemon := appsv1.DaemonSet{}
		Expect(calicoDatastoreType(daemon)).To(BeEmpty())
	})
})

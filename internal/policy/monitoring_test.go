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

package policy

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const monitoringTestNamespace = "kubernaut-system"

var _ = Describe("monitoring destination resolution", func() {
	It("resolves a numeric Cilium targetPort from a configured Service URL", func() {
		service := monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{
			Name:       "web",
			Protocol:   corev1.ProtocolTCP,
			Port:       9090,
			TargetPort: intstr.FromInt(8080),
		})

		destinations, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(service), MonitoringResolutionOptions{
			Provider: ProviderCilium,
			Endpoints: []MonitoringEndpoint{{
				Name: MonitoringPrometheus,
				URL:  "https://prometheus.monitoring.svc:9090",
			}},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(destinations).To(HaveLen(1))
		Expect(destinations[0]).To(MatchFields(IgnoreExtras, Fields{
			"Name":        Equal(MonitoringPrometheus),
			"ServiceName": Equal("prometheus"),
			"Namespace":   Equal("monitoring"),
			"ServicePort": Equal(int32(9090)),
			"BackendPort": Equal(int32(8080)),
		}))
	})

	It("resolves bare BYO Service hosts in the Kubernaut namespace", func() {
		postgres := monitoringServiceWithName("postgresql", map[string]string{"app": "postgresql"}, corev1.ServicePort{
			Name: "postgresql", Protocol: corev1.ProtocolTCP, Port: 5432, TargetPort: intstr.FromInt(5432),
		})
		postgres.Namespace = monitoringTestNamespace
		valkey := monitoringServiceWithName("valkey", map[string]string{"app": "valkey"}, corev1.ServicePort{
			Name: "valkey", Protocol: corev1.ProtocolTCP, Port: 6379, TargetPort: intstr.FromInt(6379),
		})
		valkey.Namespace = monitoringTestNamespace

		destinations, err := ResolveServiceDestinations(context.Background(), fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(postgres, valkey).Build(), ProviderOVN, []ServiceEndpoint{
			{Name: "postgresql", Host: "postgresql", Namespace: monitoringTestNamespace, Port: 5432},
			{Name: "valkey", Host: "valkey", Namespace: monitoringTestNamespace, Port: 6379},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(destinations).To(HaveLen(2))
		Expect(destinations[0]).To(MatchFields(IgnoreExtras, Fields{
			"Name":            Equal("postgresql"),
			"Namespace":       Equal("kubernaut-system"),
			"ServiceSelector": Equal(map[string]string{"app": "postgresql"}),
			"BackendPort":     Equal(int32(5432)),
		}))
		Expect(destinations[1].Name).To(Equal("valkey"))
	})

	It("fails closed when a BYO host is not a resolvable in-cluster Service", func() {
		_, err := ResolveServiceDestinations(context.Background(), fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).Build(), ProviderOVN, []ServiceEndpoint{{
			Name: "postgresql", Host: "postgres.example.com", Namespace: "kubernaut-system", Port: 5432,
		}})

		Expect(err).To(MatchError(ContainSubstring("in-cluster service")))
	})

	It("rejects unsupported clients, missing readers, malformed endpoints, duplicates, and unsafe Services", func() {
		_, err := ResolveServiceDestinations(context.Background(), nil, Provider("unsupported"), nil)
		Expect(err).To(MatchError(ContainSubstring("unsupported for provider")))

		_, err = ResolveServiceDestinations(context.Background(), nil, ProviderOVN, nil)
		Expect(err).To(MatchError(ContainSubstring("kubernetes client is required")))

		service := monitoringServiceWithName("postgresql", map[string]string{"app": "postgresql"}, corev1.ServicePort{
			Name: "postgresql", Protocol: corev1.ProtocolTCP, Port: 5432, TargetPort: intstr.FromInt(5432),
		})
		service.Namespace = "kubernaut-system"
		api := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(service).Build()

		_, err = ResolveServiceDestinations(context.Background(), api, ProviderOVN, []ServiceEndpoint{{
			Name: "", Host: "postgresql", Namespace: service.Namespace, Port: 5432,
		}})
		Expect(err).To(MatchError(ContainSubstring("destination name is required")))

		destinations, err := ResolveServiceDestinations(context.Background(), api, ProviderOVN, []ServiceEndpoint{
			{Name: "postgresql", Host: "postgresql", Namespace: service.Namespace, Port: 5432},
			{Name: "POSTGRESQL", Host: "postgresql", Namespace: service.Namespace, Port: 5432},
		})
		Expect(err).To(MatchError(ContainSubstring("configured more than once")))
		Expect(destinations).To(HaveLen(1))

		_, err = ResolveServiceDestinations(context.Background(), api, ProviderOVN, []ServiceEndpoint{{
			Name: "valkey", Host: "valkey", Namespace: service.Namespace, Port: 6379,
		}})
		Expect(err).To(MatchError(ContainSubstring("resolving BYO service")))

		selectorless := service.DeepCopy()
		selectorless.Spec.Selector = nil
		selectorless.Name = "selectorless"
		selectorless.ResourceVersion = ""
		Expect(api.Create(context.Background(), selectorless)).To(Succeed())
		_, err = ResolveServiceDestinations(context.Background(), api, ProviderOVN, []ServiceEndpoint{{
			Name: "selectorless", Host: selectorless.Name, Namespace: selectorless.Namespace, Port: 5432,
		}})
		Expect(err).To(MatchError(ContainSubstring("has no selector")))

		_, err = ResolveServiceDestinations(context.Background(), api, ProviderOVN, []ServiceEndpoint{{
			Name: "postgresql", Host: service.Name, Namespace: service.Namespace, Port: 6379,
		}})
		Expect(err).To(MatchError(ContainSubstring("requested TCP port")))

		named := service.DeepCopy()
		named.Name = "named"
		named.ResourceVersion = ""
		named.Spec.Ports[0].TargetPort = intstr.FromString("postgresql")
		Expect(api.Create(context.Background(), named)).To(Succeed())
		_, err = ResolveServiceDestinations(context.Background(), api, ProviderOVN, []ServiceEndpoint{{
			Name: "named", Host: named.Name, Namespace: named.Namespace, Port: 5432,
		}})
		Expect(err).To(MatchError(ContainSubstring("has no ready endpointslice endpoint")))
	})

	It("validates bare Service-reference inputs before registering dependency watches", func() {
		_, err := ServiceReferenceFromHost("", "kubernaut-system")
		Expect(err).To(MatchError(ContainSubstring("host is required")))
		_, err = ServiceReferenceFromHost("invalid_host", "kubernaut-system")
		Expect(err).To(MatchError(ContainSubstring("valid in-cluster service")))
		_, err = ServiceReferenceFromHost("postgresql", "invalid_namespace")
		Expect(err).To(MatchError(ContainSubstring("valid namespace")))
	})

	It("resolves an OVN Service selector and numeric backend port", func() {
		service := monitoringService(map[string]string{
			"app":  "prometheus",
			"tier": "monitoring",
		}, corev1.ServicePort{
			Name:       "web",
			Protocol:   corev1.ProtocolTCP,
			Port:       9091,
			TargetPort: intstr.FromInt(9091),
		})

		destinations, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(service), MonitoringResolutionOptions{
			Provider: ProviderOVN,
			Endpoints: []MonitoringEndpoint{{
				Name: MonitoringPrometheus,
				URL:  "https://prometheus.monitoring.svc:9091",
			}},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(destinations).To(HaveLen(1))
		Expect(destinations[0]).To(MatchFields(IgnoreExtras, Fields{
			"ServiceSelector": Equal(map[string]string{"app": "prometheus", "tier": "monitoring"}),
			"BackendPort":     Equal(int32(9091)),
		}))
	})

	It("resolves a named Cilium or Calico targetPort from an unambiguous EndpointSlice port", func() {
		service := monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{
			Name:       "web",
			Protocol:   corev1.ProtocolTCP,
			Port:       9090,
			TargetPort: intstr.FromString("http"),
		})
		endpointPort := int32(8080)
		ready := true
		endpoints := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prometheus-1",
				Namespace: "monitoring",
				Labels:    map[string]string{discoveryv1.LabelServiceName: "prometheus"},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{{
				Name:     ptr.To("web"),
				Protocol: ptr.To(corev1.ProtocolTCP),
				Port:     &endpointPort,
			}},
			Endpoints: []discoveryv1.Endpoint{{
				Addresses:  []string{"10.0.0.10"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			}},
		}

		for _, provider := range []Provider{ProviderCilium, ProviderCalico} {
			destinations, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(service, endpoints), MonitoringResolutionOptions{
				Provider: provider,
				Endpoints: []MonitoringEndpoint{{
					Name: MonitoringPrometheus,
					URL:  "https://prometheus.monitoring.svc:9090",
				}},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(destinations[0].BackendPort).To(Equal(int32(8080)))
		}
	})

	It("resolves a named OVN targetPort from an unambiguous ready EndpointSlice port", func() {
		service := monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{
			Name:       "metrics",
			Protocol:   corev1.ProtocolTCP,
			Port:       9094,
			TargetPort: intstr.FromString("web"),
		})
		endpointPort := int32(9095)
		ready := true
		endpoints := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prometheus-1",
				Namespace: "monitoring",
				Labels:    map[string]string{discoveryv1.LabelServiceName: "prometheus"},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{{
				Name:     ptr.To("metrics"),
				Protocol: ptr.To(corev1.ProtocolTCP),
				Port:     &endpointPort,
			}},
			Endpoints: []discoveryv1.Endpoint{{
				Addresses:  []string{"10.0.0.10"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			}},
		}

		destinations, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(service, endpoints), MonitoringResolutionOptions{
			Provider: ProviderOVN,
			Endpoints: []MonitoringEndpoint{{
				Name: MonitoringAlertManager,
				URL:  "https://prometheus.monitoring.svc:9094",
			}},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(destinations[0].BackendPort).To(Equal(int32(9095)))
	})

	It("rejects a named Cilium targetPort that resolves to multiple backend ports", func() {
		service := monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{
			Name:       "web",
			Protocol:   corev1.ProtocolTCP,
			Port:       9090,
			TargetPort: intstr.FromString("http"),
		})
		ready := true
		firstPort := int32(8080)
		secondPort := int32(8081)
		firstSlice := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prometheus-1",
				Namespace: "monitoring",
				Labels:    map[string]string{discoveryv1.LabelServiceName: "prometheus"},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{{
				Name: ptr.To("web"), Port: &firstPort, Protocol: ptr.To(corev1.ProtocolTCP),
			}},
			Endpoints: []discoveryv1.Endpoint{{Conditions: discoveryv1.EndpointConditions{Ready: &ready}}},
		}
		secondSlice := firstSlice.DeepCopy()
		secondSlice.Name = "prometheus-2"
		secondSlice.Ports[0].Port = &secondPort

		_, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(service, firstSlice, secondSlice), MonitoringResolutionOptions{
			Provider: ProviderCilium,
			Endpoints: []MonitoringEndpoint{{
				Name: MonitoringPrometheus,
				URL:  "https://prometheus.monitoring.svc:9090",
			}},
		})

		Expect(err).To(MatchError(ContainSubstring("multiple backend ports")))
	})

	It("does not resolve a named Cilium targetPort from an unready EndpointSlice", func() {
		service := monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{
			Name:       "web",
			Protocol:   corev1.ProtocolTCP,
			Port:       9090,
			TargetPort: intstr.FromString("http"),
		})
		notReady := false
		ready := true
		backendPort := int32(8080)
		endpointSlice := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prometheus-1",
				Namespace: "monitoring",
				Labels:    map[string]string{discoveryv1.LabelServiceName: "prometheus"},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{{
				Name: ptr.To("web"), Protocol: ptr.To(corev1.ProtocolTCP), Port: &backendPort,
			}},
			Endpoints: []discoveryv1.Endpoint{{
				Addresses:  []string{"10.0.0.10"},
				Conditions: discoveryv1.EndpointConditions{Ready: &notReady},
			}},
		}
		readySlice := endpointSlice.DeepCopy()
		readySlice.Name = "prometheus-2"
		readySlice.Ports[0].Name = ptr.To("other")
		readySlice.Endpoints[0].Conditions.Ready = &ready

		_, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(service, endpointSlice, readySlice), MonitoringResolutionOptions{
			Provider: ProviderCilium,
			Endpoints: []MonitoringEndpoint{{
				Name: MonitoringPrometheus,
				URL:  "https://prometheus.monitoring.svc:9090",
			}},
		})

		Expect(err).To(MatchError(ContainSubstring("could not be resolved")))
	})

	It("retains healthy monitoring destinations when another endpoint is unresolved", func() {
		prometheus := monitoringServiceWithName("prometheus", map[string]string{"app": "prometheus"}, corev1.ServicePort{
			Name: "web", Protocol: corev1.ProtocolTCP, Port: 9090, TargetPort: intstr.FromInt(8080),
		})

		destinations, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(prometheus), MonitoringResolutionOptions{
			Provider: ProviderCilium,
			Endpoints: []MonitoringEndpoint{
				{Name: MonitoringPrometheus, URL: "https://prometheus.monitoring.svc:9090"},
				{Name: MonitoringAlertManager, URL: "https://missing.monitoring.svc:9093"},
			},
		})

		Expect(err).To(MatchError(ContainSubstring("alertmanager")))
		Expect(destinations).To(HaveLen(1))
		Expect(destinations[0].Name).To(Equal(MonitoringPrometheus))
	})

	It("resolves an omitted URL port from a single TCP Service port", func() {
		service := monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{
			Protocol:   corev1.ProtocolTCP,
			Port:       9090,
			TargetPort: intstr.FromInt(9090),
		})

		destinations, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(service), MonitoringResolutionOptions{
			Provider: ProviderCalico,
			Endpoints: []MonitoringEndpoint{{
				Name: MonitoringPrometheus,
				URL:  "https://prometheus.monitoring.svc",
			}},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(destinations[0].ServicePort).To(Equal(int32(9090)))
	})

	It("rejects an ambiguous Calico Service instead of broadening egress", func() {
		service := monitoringService(map[string]string{"app": "prometheus"},
			corev1.ServicePort{Protocol: corev1.ProtocolTCP, Port: 9090, TargetPort: intstr.FromInt(9090)},
			corev1.ServicePort{Protocol: corev1.ProtocolTCP, Port: 9091, TargetPort: intstr.FromInt(9091)},
		)

		_, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(service), MonitoringResolutionOptions{
			Provider: ProviderCalico,
			Endpoints: []MonitoringEndpoint{{
				Name: MonitoringPrometheus,
				URL:  "https://prometheus.monitoring.svc:9090",
			}},
		})

		Expect(err).To(MatchError(ContainSubstring("exactly one TCP service port")))
	})

	It("rejects external, selector-less, headless, and malformed destinations", func() {
		cases := []struct {
			name    string
			service *corev1.Service
			url     string
			err     string
		}{
			{
				name:    "external host",
				service: monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{Port: 9090, TargetPort: intstr.FromInt(9090)}),
				url:     "https://prometheus.example.com:9090",
				err:     "must reference an in-cluster service",
			},
			{
				name:    "selector-less Service",
				service: monitoringService(nil, corev1.ServicePort{Port: 9090, TargetPort: intstr.FromInt(9090)}),
				url:     "https://prometheus.monitoring.svc:9090",
				err:     "has no selector",
			},
			{
				name: "ExternalName Service",
				service: func() *corev1.Service {
					svc := monitoringService(nil, corev1.ServicePort{Port: 9090, TargetPort: intstr.FromInt(9090)})
					svc.Spec.Type = corev1.ServiceTypeExternalName
					svc.Spec.ExternalName = "prometheus.example.com"
					return svc
				}(),
				url: "https://prometheus.monitoring.svc:9090",
				err: "externalName",
			},
			{
				name: "headless Service",
				service: func() *corev1.Service {
					svc := monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{Port: 9090, TargetPort: intstr.FromInt(9090)})
					svc.Spec.ClusterIP = corev1.ClusterIPNone
					return svc
				}(),
				url: "https://prometheus.monitoring.svc:9090",
				err: "headless",
			},
			{
				name:    "malformed URL",
				service: monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{Port: 9090, TargetPort: intstr.FromInt(9090)}),
				url:     "https://prometheus.monitoring.svc:not-a-port",
				err:     "invalid monitoring URL",
			},
			{
				name:    "plaintext URL",
				service: monitoringService(map[string]string{"app": "prometheus"}, corev1.ServicePort{Port: 9090, TargetPort: intstr.FromInt(9090)}),
				url:     "http://prometheus.monitoring.svc:9090",
				err:     "must use https",
			},
		}

		for _, test := range cases {
			By(test.name)
			_, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(test.service), MonitoringResolutionOptions{
				Provider: ProviderCilium,
				Endpoints: []MonitoringEndpoint{{
					Name: MonitoringPrometheus,
					URL:  test.url,
				}},
			})
			Expect(err).To(MatchError(ContainSubstring(test.err)))
		}
	})
})

var _ = Describe("monitoring native policy rendering", func() {
	It("adds exact backend-port Cilium Service rules only to the Agent policy", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"gateway", "kubernaut-agent"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.InstanceName = policyTestInstance
		intent.Monitoring = []MonitoringDestination{
			{
				Name: "prometheus", ServiceName: "prometheus", Namespace: "monitoring", ServicePort: 9090, BackendPort: 8080,
				ServiceSelector: map[string]string{"app": "prometheus"},
				ServicePorts:    []MonitoringServicePort{{Port: 9090, Protocol: string(corev1.ProtocolTCP), TargetPort: 8080}},
			},
		}

		objects, err := Render(DiscoveryResultForTest(ProviderCilium, "1.20.2"), intent)
		Expect(err).NotTo(HaveOccurred())

		for _, object := range objects {
			spec, found, err := unstructured.NestedMap(object.Object.Object, "spec")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			egress, found, err := unstructured.NestedSlice(spec, "egress")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			if object.Object.GetName() != "kubernaut-kubernaut-agent" {
				for _, rule := range egress {
					Expect(rule.(map[string]interface{})).NotTo(HaveKey("toServices"))
				}
				continue
			}
			monitoringRule := findMonitoringCiliumRule(egress)
			Expect(monitoringRule).NotTo(BeNil())
			Expect(monitoringRule["toServices"]).NotTo(BeNil())
			ports := monitoringRule["toPorts"].([]interface{})[0].(map[string]interface{})["ports"].([]interface{})
			Expect(ports[0].(map[string]interface{})).To(HaveKeyWithValue("port", "8080"))
		}
	})

	It("renders Calico Service selector rules with an exact backend port", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"kubernaut-agent"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.Monitoring = []MonitoringDestination{{
			Name: "prometheus", ServiceName: "prometheus", Namespace: "monitoring", ServicePort: 9090, BackendPort: 8080,
			ServiceSelector: map[string]string{"app": "prometheus"},
			ServicePorts:    []MonitoringServicePort{{Port: 9090, Protocol: string(corev1.ProtocolTCP), TargetPort: 8080}},
		}}

		objects, err := Render(DiscoveryResultForTest(ProviderCalico, "3.31.4"), intent)
		Expect(err).NotTo(HaveOccurred())
		spec, found, err := unstructured.NestedMap(objects[0].Object.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		egress, found, err := unstructured.NestedSlice(spec, "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		monitoringRule := findMonitoringCalicoRule(egress)
		Expect(monitoringRule).NotTo(BeNil())
		Expect(monitoringRule).To(HaveKeyWithValue("protocol", "TCP"))
		destination := monitoringRule["destination"].(map[string]interface{})
		Expect(destination).To(HaveKeyWithValue("namespaceSelector", "projectcalico.org/name == 'monitoring'"))
		Expect(destination).To(HaveKeyWithValue("selector", "app == 'prometheus'"))
		Expect(destination["ports"]).To(Equal([]interface{}{int64(8080)}))
		Expect(destination).NotTo(HaveKey("services"))
	})

	It("renders Calico dependency rules with an exact backend port", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"kubernaut-agent"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.Dependencies = []MonitoringDestination{{
			Name: "postgresql", ServiceName: "postgresql", Namespace: "kubernaut-system", ServicePort: 5432, BackendPort: 5432,
			ServiceSelector: map[string]string{"app": "postgresql"},
			ServicePorts:    []MonitoringServicePort{{Port: 5432, Protocol: string(corev1.ProtocolTCP), TargetPort: 5432}},
		}}

		objects, err := Render(DiscoveryResultForTest(ProviderCalico, "3.31.4"), intent)
		Expect(err).NotTo(HaveOccurred())
		spec, found, err := unstructured.NestedMap(objects[0].Object.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		egress, found, err := unstructured.NestedSlice(spec, "egress")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		dependencyRule := findCalicoServiceRule(egress, "postgresql")
		Expect(dependencyRule).NotTo(BeNil())
		Expect(dependencyRule).To(HaveKeyWithValue("protocol", "TCP"))
		destination := dependencyRule["destination"].(map[string]interface{})
		Expect(destination).To(HaveKeyWithValue("namespaceSelector", "projectcalico.org/name == 'kubernaut-system'"))
		Expect(destination).To(HaveKeyWithValue("selector", "app == 'postgresql'"))
		Expect(destination["ports"]).To(Equal([]interface{}{int64(5432)}))
		Expect(destination).NotTo(HaveKey("services"))
	})

	It("fails closed when a dependency destination bypasses resolution", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"kubernaut-agent"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.Dependencies = []MonitoringDestination{{
			Name: "postgresql", ServiceName: "postgresql", Namespace: "kubernaut-system", ServicePort: 5432,
			BackendPort: 5432,
		}}

		_, err = Render(DiscoveryResultForTest(ProviderCalico, "3.31.4"), intent)
		Expect(err).To(MatchError(ContainSubstring("no Service selector")))
	})

	It("fails closed when a renderer receives an unsafe monitoring destination", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"kubernaut-agent"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.Monitoring = []MonitoringDestination{{
			Name: "prometheus", ServiceName: "prometheus", Namespace: "monitoring", ServicePort: 9090,
			BackendPort: 9090, ServiceSelector: map[string]string{"app": "prometheus"},
			ServicePorts: []MonitoringServicePort{
				{Port: 9090, Protocol: string(corev1.ProtocolTCP), TargetPort: 9090},
				{Port: 9091, Protocol: string(corev1.ProtocolTCP), TargetPort: 9091},
			},
		}}

		_, err = Render(DiscoveryResultForTest(ProviderCalico, "3.31.4"), intent)
		Expect(err).To(MatchError(ContainSubstring("exactly one TCP service port")))
	})
})

func monitoringClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func monitoringService(selector map[string]string, ports ...corev1.ServicePort) *corev1.Service {
	return monitoringServiceWithName("prometheus", selector, ports...)
}

func monitoringServiceWithName(name string, selector map[string]string, ports ...corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "monitoring"},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports:    ports,
		},
	}
}

func findMonitoringCiliumRule(egress []interface{}) map[string]interface{} {
	for _, item := range egress {
		rule, ok := item.(map[string]interface{})
		if ok {
			if _, found := rule["toServices"]; found {
				return rule
			}
		}
	}
	return nil
}

func findMonitoringCalicoRule(egress []interface{}) map[string]interface{} {
	for _, item := range egress {
		rule, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		destination, ok := rule["destination"].(map[string]interface{})
		if ok {
			if destination["namespaceSelector"] == "projectcalico.org/name == 'monitoring'" {
				return rule
			}
		}
	}
	return nil
}

func findCalicoServiceRule(egress []interface{}, serviceName string) map[string]interface{} {
	for _, item := range egress {
		rule, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		destination, ok := rule["destination"].(map[string]interface{})
		if !ok {
			continue
		}
		if destination["selector"] == fmt.Sprintf("app == '%s'", serviceName) {
			return rule
		}
	}
	return nil
}

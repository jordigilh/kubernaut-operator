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

	It("resolves a named Cilium targetPort from an unambiguous EndpointSlice port", func() {
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

		destinations, err := ResolveMonitoringDestinations(context.Background(), monitoringClient(service, endpoints), MonitoringResolutionOptions{
			Provider: ProviderCilium,
			Endpoints: []MonitoringEndpoint{{
				Name: MonitoringPrometheus,
				URL:  "https://prometheus.monitoring.svc:9090",
			}},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(destinations[0].BackendPort).To(Equal(int32(8080)))
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
				ServicePorts: []MonitoringServicePort{{Port: 9090, Protocol: string(corev1.ProtocolTCP), TargetPort: 8080}},
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

	It("renders Calico Service rules without an explicit egress port", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"kubernaut-agent"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.Monitoring = []MonitoringDestination{{
			Name: "prometheus", ServiceName: "prometheus", Namespace: "monitoring", ServicePort: 9090, BackendPort: 8080,
			ServicePorts: []MonitoringServicePort{{Port: 9090, Protocol: string(corev1.ProtocolTCP), TargetPort: 8080}},
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
		destination := monitoringRule["destination"].(map[string]interface{})
		Expect(destination).To(HaveKey("services"))
		Expect(destination).NotTo(HaveKey("ports"))
	})

	It("fails closed when a renderer receives an unsafe monitoring destination", func() {
		intent, err := BuildIntent("kubernaut-system", []string{"kubernaut-agent"}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.Monitoring = []MonitoringDestination{{
			Name: "prometheus", ServiceName: "prometheus", Namespace: "monitoring", ServicePort: 9090,
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
			if _, found := destination["services"]; found {
				return rule
			}
		}
	}
	return nil
}

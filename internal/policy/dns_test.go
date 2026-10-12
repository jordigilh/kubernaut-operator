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
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("Service-derived DNS authorization", func() {
	It("UT-AUDIT-518-DNS-001: resolves distinct UDP/TCP backend ports and excludes metrics", func() {
		service := dnsServiceForTest()
		slice := dnsEndpointSliceForTest(service)
		// A second ready slice agreeing on each backend is not ambiguous. An
		// unready slice with different ports must not authorize those ports.
		second := slice.DeepCopy()
		second.Name = "dns-second"
		second.Endpoints[0].Conditions.Ready = nil
		unready := slice.DeepCopy()
		unready.Name = "dns-unready"
		unready.Endpoints[0].Conditions.Ready = ptr.To(false)
		unready.Ports[0].Port = ptr.To[int32](9999)
		reader := monitoringClient(service, slice, second, unready)
		destination, err := ResolveDNSDestination(context.Background(), reader, client.ObjectKeyFromObject(service))
		Expect(err).NotTo(HaveOccurred())
		Expect(destination).To(Equal(DNSDestination{
			ServiceName: service.Name, Namespace: service.Namespace, ServiceSelector: service.Spec.Selector,
			UDPPort: 5353, TCPPort: 5454,
		}))
		intent, err := BuildIntent("kubernaut-system", []string{MonitoringAgentComponent}, nil)
		Expect(err).NotTo(HaveOccurred())
		intent.DNS = destination
		objects, err := Render(DiscoveryResultForTest(ProviderOVN, "4.22.16"), intent)
		Expect(err).NotTo(HaveOccurred())
		for _, object := range objects {
			egress, found, readErr := unstructured.NestedSlice(object.Object.Object, "spec", "egress")
			Expect(readErr).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(egress).To(ContainElement(Equal(map[string]interface{}{
				"name": "allow-dns", "action": "Allow",
				"to": []interface{}{map[string]interface{}{"pods": map[string]interface{}{
					"namespaceSelector": map[string]interface{}{"matchLabels": map[string]interface{}{
						"kubernetes.io/metadata.name": "openshift-dns",
					}},
					"podSelector": map[string]interface{}{"matchLabels": map[string]interface{}{
						"test.kubernaut.ai/dns-backend": "platform",
					}},
				}}},
				"ports": []interface{}{
					map[string]interface{}{"portNumber": map[string]interface{}{"protocol": "TCP", "port": float64(5454)}},
					map[string]interface{}{"portNumber": map[string]interface{}{"protocol": "UDP", "port": float64(5353)}},
				},
			})))
			Expect(fmt.Sprint(object.Object.Object)).NotTo(ContainSubstring("9154"))
			Expect(fmt.Sprint(object.Object.Object)).NotTo(ContainSubstring("9999"))
		}
		service.Spec.Selector["test.kubernaut.ai/dns-backend"] = "changed"
		Expect(destination.ServiceSelector).To(HaveKeyWithValue("test.kubernaut.ai/dns-backend", "platform"))
	})

	It("UT-AUDIT-518-DNS-002: resolves numeric and omitted backend ports without assuming 5353", func() {
		service := dnsServiceForTest()
		service.Spec.Ports[0].TargetPort = intstr.FromInt(65535)
		service.Spec.Ports[1].Protocol = "" // Kubernetes defaults protocol to TCP.
		service.Spec.Ports[1].TargetPort = intstr.IntOrString{}
		service.Spec.Ports[2].TargetPort = intstr.FromInt(-1) // Unrelated metrics must not be selected.
		destination, err := ResolveDNSDestination(context.Background(), monitoringClient(service), client.ObjectKeyFromObject(service))
		Expect(err).NotTo(HaveOccurred())
		Expect(destination.UDPPort).To(Equal(int32(65535)))
		Expect(destination.TCPPort).To(Equal(int32(53)))
	})

	DescribeTable("UT-AUDIT-518-DNS-003: rejects unsafe or incomplete DNS Services without returning authorization",
		func(change func(*corev1.Service), message string) {
			service := dnsServiceForTest()
			change(service)
			destination, err := ResolveDNSDestination(context.Background(), monitoringClient(service), client.ObjectKeyFromObject(service))
			Expect(err).To(MatchError(ContainSubstring(message)))
			Expect(destination).To(Equal(DNSDestination{}))
		},
		Entry("ExternalName", func(s *corev1.Service) { s.Spec.Type = corev1.ServiceTypeExternalName }, "externalName"),
		Entry("headless", func(s *corev1.Service) { s.Spec.ClusterIP = corev1.ClusterIPNone }, "headless"),
		Entry("empty selector", func(s *corev1.Service) { s.Spec.Selector = nil }, "no selector"),
		Entry("no ports", func(s *corev1.Service) { s.Spec.Ports = nil }, "no ports"),
		Entry("not ClusterIP", func(s *corev1.Service) { s.Spec.Type = corev1.ServiceTypeLoadBalancer }, "ClusterIP"),
		Entry("TCP only", func(s *corev1.Service) { s.Spec.Ports = s.Spec.Ports[1:] }, "UDP DNS port 53"),
		Entry("UDP only", func(s *corev1.Service) {
			s.Spec.Ports = s.Spec.Ports[:1]
			s.Spec.Ports[0].TargetPort = intstr.FromInt(5353)
		}, "TCP DNS port 53"),
		Entry("metrics is not DNS", func(s *corev1.Service) { s.Spec.Ports = s.Spec.Ports[2:] }, "UDP DNS port 53"),
		Entry("wrong protocol", func(s *corev1.Service) { s.Spec.Ports[0].Protocol = corev1.ProtocolSCTP }, "UDP DNS port 53"),
		Entry("ambiguous UDP frontend", func(s *corev1.Service) { s.Spec.Ports = append(s.Spec.Ports, s.Spec.Ports[0]) }, "multiple UDP DNS ports"),
		Entry("invalid numeric backend", func(s *corev1.Service) { s.Spec.Ports[0].TargetPort = intstr.FromInt(65536) }, "invalid targetPort"),
		Entry("invalid named backend", func(s *corev1.Service) { s.Spec.Ports[0].TargetPort = intstr.FromString("INVALID") }, "invalid named targetPort"),
		Entry("invalid identity", func(s *corev1.Service) {
			s.Name = "INVALID"
			s.Spec.Ports[0].TargetPort = intstr.FromInt(5353)
			s.Spec.Ports[1].TargetPort = intstr.FromInt(5454)
		}, "valid service and namespace"),
		Entry("invalid selector", func(s *corev1.Service) {
			s.Spec.Selector = map[string]string{"invalid key": "value"}
			s.Spec.Ports[0].TargetPort = intstr.FromInt(5353)
			s.Spec.Ports[1].TargetPort = intstr.FromInt(5454)
		}, "invalid service selector"),
	)

	DescribeTable("UT-AUDIT-518-DNS-004: fails closed on absent, wrong-protocol or ambiguous ready backend evidence",
		func(change func(*discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice, message string) {
			service := dnsServiceForTest()
			slices := change(dnsEndpointSliceForTest(service))
			objects := make([]client.Object, 1, len(slices)+1)
			objects[0] = service
			for _, slice := range slices {
				objects = append(objects, slice)
			}
			destination, err := ResolveDNSDestination(context.Background(), monitoringClient(objects...), client.ObjectKeyFromObject(service))
			Expect(err).To(MatchError(ContainSubstring(message)))
			Expect(destination).To(Equal(DNSDestination{}))
		},
		Entry("no slices", func(*discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice { return nil }, "no ready endpointslice"),
		Entry("unready", func(s *discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice {
			s.Endpoints[0].Conditions.Ready = ptr.To(false)
			return []*discoveryv1.EndpointSlice{s}
		}, "no ready endpointslice"),
		Entry("unrelated Service", func(s *discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice {
			s.Labels[discoveryv1.LabelServiceName] = "other"
			return []*discoveryv1.EndpointSlice{s}
		}, "no ready endpointslice"),
		Entry("wrong port name", func(s *discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice {
			s.Ports[0].Name = ptr.To("not-dns")
			return []*discoveryv1.EndpointSlice{s}
		}, "could not be resolved"),
		Entry("wrong UDP protocol", func(s *discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice {
			s.Ports[0].Protocol = ptr.To(corev1.ProtocolTCP)
			return []*discoveryv1.EndpointSlice{s}
		}, "valid UDP backend port"),
		Entry("wrong TCP protocol", func(s *discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice {
			s.Ports[1].Protocol = ptr.To(corev1.ProtocolUDP)
			return []*discoveryv1.EndpointSlice{s}
		}, "valid TCP backend port"),
		Entry("nil backend port", func(s *discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice {
			s.Ports[0].Port = nil
			return []*discoveryv1.EndpointSlice{s}
		}, "valid UDP backend port"),
		Entry("zero backend port", func(s *discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice {
			s.Ports[0].Port = ptr.To[int32](0)
			return []*discoveryv1.EndpointSlice{s}
		}, "valid UDP backend port"),
		Entry("ambiguous ready UDP backends", func(s *discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice {
			second := s.DeepCopy()
			second.Name = "dns-second"
			second.Ports[0].Port = ptr.To[int32](5354)
			return []*discoveryv1.EndpointSlice{s, second}
		}, "multiple backend ports"),
	)

	It("UT-AUDIT-518-DNS-005: reports API discovery failures and refuses missing DNS render input", func() {
		service := dnsServiceForTest()
		key := client.ObjectKeyFromObject(service)
		_, err := ResolveDNSDestination(context.Background(), nil, key)
		Expect(err).To(MatchError(ContainSubstring("kubernetes client")))
		_, err = ResolveDNSDestination(context.Background(), monitoringClient(), key)
		Expect(err).To(MatchError(ContainSubstring("dns-default")))
		reader := fake.NewClientBuilder().WithScheme(monitoringClient().Scheme()).WithObjects(service).
			WithInterceptorFuncs(interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return fmt.Errorf("dns endpoint read denied")
			}}).Build()
		_, err = ResolveDNSDestination(context.Background(), reader, key)
		Expect(err).To(MatchError(ContainSubstring("dns endpoint read denied")))
		intent, err := BuildIntent("kubernaut-system", []string{"gateway"}, nil)
		Expect(err).NotTo(HaveOccurred())
		objects, err := Render(DiscoveryResultForTest(ProviderOVN, "4.22.16"), intent)
		Expect(err).To(MatchError(ContainSubstring("valid service and namespace")))
		Expect(objects).To(BeNil())
	})

	It("UT-AUDIT-518-DNS-006: rejects malformed authorization independently of discovery", func() {
		for _, change := range []func(*DNSDestination){
			func(d *DNSDestination) { d.ServiceSelector = nil },
			func(d *DNSDestination) { d.Namespace = "INVALID" },
			func(d *DNSDestination) { d.ServiceSelector = map[string]string{"app": "invalid value"} },
			func(d *DNSDestination) { d.UDPPort = 0 },
			func(d *DNSDestination) { d.UDPPort = 65536 },
			func(d *DNSDestination) { d.TCPPort = 0 },
			func(d *DNSDestination) { d.TCPPort = 65536 },
		} {
			destination := ovnDNSDestinationForTest()
			change(&destination)
			Expect(validateDNSDestination(destination)).To(HaveOccurred())
		}
	})
})

func dnsServiceForTest() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "dns-default", Namespace: "openshift-dns"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"test.kubernaut.ai/dns-backend": "platform"},
			Ports: []corev1.ServicePort{
				{Name: "dns-udp", Protocol: corev1.ProtocolUDP, Port: 53, TargetPort: intstr.FromString("resolver-udp")},
				{Name: "dns-tcp", Protocol: corev1.ProtocolTCP, Port: 53, TargetPort: intstr.FromString("resolver-tcp")},
				{Name: "metrics", Protocol: corev1.ProtocolTCP, Port: 9154, TargetPort: intstr.FromInt(9154)},
			}},
	}
}

func dnsEndpointSliceForTest(service *corev1.Service) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "dns-backends", Namespace: service.Namespace,
			Labels: map[string]string{discoveryv1.LabelServiceName: service.Name}},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{Name: ptr.To("dns-udp"), Protocol: ptr.To(corev1.ProtocolUDP), Port: ptr.To[int32](5353)},
			{Name: ptr.To("dns-tcp"), Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To[int32](5454)},
			{Name: ptr.To("metrics"), Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To[int32](9154)},
		},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.53"},
			Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}}},
	}
}

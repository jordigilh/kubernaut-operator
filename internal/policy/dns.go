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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DNSDestination binds DNS authorization to a selector-backed Service and its
// actual UDP/TCP backend ports. It intentionally excludes metrics and CIDRs.
// See docs/design/PR-518-OVN-ISOLATION-DNS.md.
type DNSDestination struct {
	ServiceName     string
	Namespace       string
	ServiceSelector map[string]string
	UDPPort         int32
	TCPPort         int32
}

// ResolveDNSDestination resolves the platform DNS Service without guessing pod
// labels or translating its frontend port into an assumed backend port.
func ResolveDNSDestination(ctx context.Context, reader client.Reader, key client.ObjectKey) (DNSDestination, error) {
	if reader == nil {
		return DNSDestination{}, fmt.Errorf("kubernetes client is required to resolve DNS")
	}
	service := &corev1.Service{}
	if err := reader.Get(ctx, key, service); err != nil {
		return DNSDestination{}, fmt.Errorf("resolving DNS service %s/%s: %w", key.Namespace, key.Name, err)
	}
	if err := validateMonitoringService(service); err != nil {
		return DNSDestination{}, fmt.Errorf("resolving DNS service %s/%s: %w", key.Namespace, key.Name, err)
	}
	if service.Spec.Type != "" && service.Spec.Type != corev1.ServiceTypeClusterIP {
		return DNSDestination{}, fmt.Errorf("dns service %s/%s must be a ClusterIP service", key.Namespace, key.Name)
	}
	udpPort, err := resolveDNSServicePort(ctx, reader, service, corev1.ProtocolUDP)
	if err != nil {
		return DNSDestination{}, fmt.Errorf("resolving DNS service %s/%s UDP backend: %w", key.Namespace, key.Name, err)
	}
	tcpPort, err := resolveDNSServicePort(ctx, reader, service, corev1.ProtocolTCP)
	if err != nil {
		return DNSDestination{}, fmt.Errorf("resolving DNS service %s/%s TCP backend: %w", key.Namespace, key.Name, err)
	}
	destination := DNSDestination{
		ServiceName: service.Name, Namespace: service.Namespace,
		ServiceSelector: copyStringMap(service.Spec.Selector), UDPPort: udpPort, TCPPort: tcpPort,
	}
	if err := validateDNSDestination(destination); err != nil {
		return DNSDestination{}, fmt.Errorf("validating dns service %s/%s: %w", key.Namespace, key.Name, err)
	}
	return destination, nil
}

func resolveDNSServicePort(ctx context.Context, reader client.Reader, service *corev1.Service, protocol corev1.Protocol) (int32, error) {
	var selected corev1.ServicePort
	found := false
	for _, candidate := range service.Spec.Ports {
		candidateProtocol := candidate.Protocol
		if candidateProtocol == "" {
			candidateProtocol = corev1.ProtocolTCP
		}
		if candidate.Port != 53 || candidateProtocol != protocol {
			continue
		}
		if found {
			return 0, fmt.Errorf("service exposes multiple %s DNS ports", protocol)
		}
		selected, found = candidate, true
	}
	if !found {
		return 0, fmt.Errorf("service does not expose %s DNS port 53", protocol)
	}
	port, err := normalizeMonitoringServicePort(selected)
	if err != nil {
		return 0, err
	}
	if port.TargetPortName != "" {
		return resolveNamedTargetPort(ctx, reader, service, port)
	}
	return port.TargetPort, nil
}

func validateDNSDestination(destination DNSDestination) error {
	if len(validation.IsDNS1035Label(destination.ServiceName)) > 0 || len(validation.IsDNS1123Label(destination.Namespace)) > 0 {
		return fmt.Errorf("dns destination requires a valid service and namespace")
	}
	if len(destination.ServiceSelector) == 0 {
		return fmt.Errorf("dns destination requires a nonempty service selector")
	}
	for key, value := range destination.ServiceSelector {
		if len(validation.IsQualifiedName(key)) > 0 || len(validation.IsValidLabelValue(value)) > 0 {
			return fmt.Errorf("dns destination has an invalid service selector")
		}
	}
	if destination.UDPPort < 1 || destination.UDPPort > 65535 || destination.TCPPort < 1 || destination.TCPPort > 65535 {
		return fmt.Errorf("dns destination requires valid UDP and TCP backend ports")
	}
	return nil
}

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
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	MonitoringPrometheus     = "prometheus"
	MonitoringAlertManager   = "alertmanager"
	MonitoringAgentComponent = "kubernaut-agent"
)

// MonitoringEndpoint is an enabled monitoring integration endpoint supplied
// by the controller's capability-aware runtime configuration view.
type MonitoringEndpoint struct {
	Name string
	URL  string
}

// MonitoringServiceReference identifies the in-cluster Service addressed by a
// monitoring endpoint URL. Controllers use it to scope event-driven watches
// without duplicating URL parsing or accepting external destinations.
type MonitoringServiceReference struct {
	Name      string
	Namespace string
}

// MonitoringServicePort records the Service port and its backend target. The
// full Service port set is retained so provider adapters can enforce their
// different least-privilege requirements. ServicePortName is the name carried
// by EndpointSlice ports; TargetPortName is the Service's pod target-port
// name, which is not the EndpointSlice lookup key.
type MonitoringServicePort struct {
	Port            int32
	Protocol        string
	ServicePortName string
	TargetPort      int32
	TargetPortName  string
}

// MonitoringDestination is the provider-neutral, normalized destination for
// one monitoring integration. BackendPort is required by Cilium and OVN and
// may be zero for Calico when the Service uses a named targetPort, because
// Calico's native Service match performs that translation itself.
type MonitoringDestination struct {
	Name            string
	ServiceName     string
	Namespace       string
	ServicePort     int32
	BackendPort     int32
	ServiceSelector map[string]string
	ServicePorts    []MonitoringServicePort
}

// MonitoringResolutionOptions controls provider-specific capability checks
// while keeping the resulting destination contract provider-neutral.
type MonitoringResolutionOptions struct {
	Provider  Provider
	Endpoints []MonitoringEndpoint
}

// ResolveMonitoringDestinations parses enabled monitoring URLs, resolves
// selector-backed in-cluster Services, and returns deterministic normalized
// destinations. It never produces a CIDR or all-egress fallback.
func ResolveMonitoringDestinations(ctx context.Context, reader client.Reader, options MonitoringResolutionOptions) ([]MonitoringDestination, error) {
	if options.Provider != ProviderCilium && options.Provider != ProviderCalico && options.Provider != ProviderOVN {
		return nil, fmt.Errorf("monitoring destinations are unsupported for provider %s", options.Provider)
	}
	if len(options.Endpoints) == 0 {
		return nil, nil
	}
	if reader == nil {
		return nil, fmt.Errorf("kubernetes client is required to resolve monitoring destinations")
	}

	endpoints := append([]MonitoringEndpoint(nil), options.Endpoints...)
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].Name < endpoints[j].Name })
	destinationsByName := make(map[string]MonitoringDestination, len(endpoints))
	seen := make(map[string]struct{}, len(endpoints))
	resolutionErrors := make([]error, 0, len(endpoints))
	for _, endpoint := range endpoints {
		name := strings.ToLower(strings.TrimSpace(endpoint.Name))
		if !supportedMonitoringEndpoint(name) {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("unsupported monitoring endpoint %q", name))
			continue
		}
		if _, exists := seen[name]; exists {
			delete(destinationsByName, name)
			resolutionErrors = append(resolutionErrors, fmt.Errorf("monitoring endpoint %q is configured more than once", name))
			continue
		}
		seen[name] = struct{}{}
		destination, err := resolveMonitoringEndpoint(ctx, reader, options.Provider, MonitoringEndpoint{Name: name, URL: endpoint.URL})
		if err != nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("resolving %s monitoring endpoint: %w", name, err))
			continue
		}
		if err := validateMonitoringDestination(options.Provider, destination); err != nil {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("resolving %s monitoring endpoint: %w", name, err))
			continue
		}
		destinationsByName[name] = destination
	}

	destinations := make([]MonitoringDestination, 0, len(destinationsByName))
	for _, endpoint := range endpoints {
		name := strings.ToLower(strings.TrimSpace(endpoint.Name))
		if destination, ok := destinationsByName[name]; ok {
			destinations = append(destinations, destination)
			delete(destinationsByName, name)
		}
	}
	if err := ValidateMonitoringDestinations(options.Provider, destinations); err != nil {
		return nil, errors.Join(append(resolutionErrors, err)...)
	}
	return destinations, errors.Join(resolutionErrors...)
}

func resolveMonitoringEndpoint(ctx context.Context, reader client.Reader, provider Provider, endpoint MonitoringEndpoint) (MonitoringDestination, error) {
	serviceName, namespace, requestedPort, err := parseMonitoringURL(endpoint.URL)
	if err != nil {
		return MonitoringDestination{}, err
	}
	service := &corev1.Service{}
	if err := reader.Get(ctx, client.ObjectKey{Name: serviceName, Namespace: namespace}, service); err != nil {
		return MonitoringDestination{}, fmt.Errorf("resolving monitoring service %s/%s: %w", namespace, serviceName, err)
	}
	if err := validateMonitoringService(service); err != nil {
		return MonitoringDestination{}, fmt.Errorf("resolving monitoring service %s/%s: %w", namespace, serviceName, err)
	}
	servicePorts, selected, err := selectMonitoringServicePort(service, requestedPort, provider)
	if err != nil {
		return MonitoringDestination{}, fmt.Errorf("resolving monitoring service %s/%s: %w", namespace, serviceName, err)
	}
	backendPort := selected.TargetPort
	if (provider == ProviderCilium || provider == ProviderOVN) && selected.TargetPortName != "" {
		backendPort, err = resolveNamedTargetPort(ctx, reader, service, selected)
		if err != nil {
			return MonitoringDestination{}, fmt.Errorf("resolving monitoring service %s/%s targetPort: %w", namespace, serviceName, err)
		}
	}
	return MonitoringDestination{
		Name:            endpoint.Name,
		ServiceName:     serviceName,
		Namespace:       namespace,
		ServicePort:     selected.Port,
		BackendPort:     backendPort,
		ServiceSelector: copyStringMap(service.Spec.Selector),
		ServicePorts:    servicePorts,
	}, nil
}

// ValidateMonitoringDestinations checks the provider-specific representation
// before rendering. A provider must reject a destination it cannot express
// without widening the requested traffic contract.
func ValidateMonitoringDestinations(provider Provider, destinations []MonitoringDestination) error {
	if len(destinations) == 0 {
		return nil
	}
	if provider != ProviderCilium && provider != ProviderCalico && provider != ProviderOVN {
		return fmt.Errorf("monitoring destinations are unsupported for provider %s", provider)
	}
	seen := make(map[string]struct{}, len(destinations))
	for _, destination := range destinations {
		if destination.Name == "" || destination.ServiceName == "" || destination.Namespace == "" {
			return fmt.Errorf("monitoring destination identity is incomplete")
		}
		if _, exists := seen[destination.Name]; exists {
			return fmt.Errorf("monitoring endpoint %q is configured more than once", destination.Name)
		}
		seen[destination.Name] = struct{}{}
		if err := validateMonitoringDestination(provider, destination); err != nil {
			return err
		}
	}
	return nil
}

func validateMonitoringDestination(provider Provider, destination MonitoringDestination) error {
	if destination.ServicePort < 1 || destination.ServicePort > 65535 {
		return fmt.Errorf("monitoring endpoint %q has an invalid service port", destination.Name)
	}
	switch provider {
	case ProviderCilium:
		return validateCiliumMonitoringDestination(destination)
	case ProviderCalico:
		return validateCalicoMonitoringDestination(destination)
	case ProviderOVN:
		return validateOVNMonitoringDestination(destination)
	}
	return nil
}

func validateCiliumMonitoringDestination(destination MonitoringDestination) error {
	if destination.BackendPort < 1 || destination.BackendPort > 65535 {
		return fmt.Errorf("monitoring endpoint %q has no resolved numeric backend port", destination.Name)
	}
	if !containsTCPServicePort(destination.ServicePorts, destination.ServicePort) {
		return fmt.Errorf("monitoring endpoint %q does not resolve to a TCP service port", destination.Name)
	}
	return nil
}

func validateCalicoMonitoringDestination(destination MonitoringDestination) error {
	if len(destination.ServicePorts) != 1 || destination.ServicePorts[0].Protocol != string(corev1.ProtocolTCP) {
		return fmt.Errorf("monitoring endpoint %q requires exactly one TCP service port for Calico", destination.Name)
	}
	if destination.ServicePorts[0].Port != destination.ServicePort {
		return fmt.Errorf("monitoring endpoint %q does not resolve to its configured service port", destination.Name)
	}
	return nil
}

func validateOVNMonitoringDestination(destination MonitoringDestination) error {
	if len(destination.ServiceSelector) == 0 {
		return fmt.Errorf("monitoring endpoint %q has no Service selector for OVN", destination.Name)
	}
	return validateCiliumMonitoringDestination(destination)
}

func supportedMonitoringEndpoint(name string) bool {
	return name == MonitoringPrometheus || name == MonitoringAlertManager
}

func parseMonitoringURL(raw string) (string, string, int32, error) {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return "", "", 0, fmt.Errorf("invalid monitoring URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", 0, fmt.Errorf("monitoring URL must use http or https")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || net.ParseIP(host) != nil {
		return "", "", 0, fmt.Errorf("monitoring URL must reference an in-cluster service")
	}
	serviceName, namespace, err := parseServiceHost(host)
	if err != nil {
		return "", "", 0, err
	}
	port := int32(0)
	if parsed.Port() != "" {
		value, err := strconv.ParseUint(parsed.Port(), 10, 16)
		if err != nil || value == 0 {
			return "", "", 0, fmt.Errorf("monitoring URL has an invalid port")
		}
		port = int32(value)
	}
	return serviceName, namespace, port, nil
}

func parseServiceHost(host string) (string, string, error) {
	var name string
	switch {
	case strings.HasSuffix(host, ".svc.cluster.local"):
		name = strings.TrimSuffix(host, ".svc.cluster.local")
	case strings.HasSuffix(host, ".svc"):
		name = strings.TrimSuffix(host, ".svc")
	default:
		return "", "", fmt.Errorf("monitoring URL must reference an in-cluster service")
	}
	parts := strings.Split(name, ".")
	if len(parts) != 2 || len(validation.IsDNS1035Label(parts[0])) > 0 || len(validation.IsDNS1123Label(parts[1])) > 0 {
		return "", "", fmt.Errorf("monitoring URL must reference a valid service and namespace")
	}
	return parts[0], parts[1], nil
}

func validateMonitoringService(service *corev1.Service) error {
	if service.Spec.Type == corev1.ServiceTypeExternalName {
		return fmt.Errorf("externalName services are not supported")
	}
	if service.Spec.ClusterIP == corev1.ClusterIPNone {
		return fmt.Errorf("headless services are not supported")
	}
	if len(service.Spec.Selector) == 0 {
		return fmt.Errorf("service has no selector")
	}
	if len(service.Spec.Ports) == 0 {
		return fmt.Errorf("service exposes no ports")
	}
	return nil
}

func selectMonitoringServicePort(service *corev1.Service, requestedPort int32, provider Provider) ([]MonitoringServicePort, MonitoringServicePort, error) {
	ports, err := normalizeMonitoringServicePorts(service.Spec.Ports)
	if err != nil {
		return nil, MonitoringServicePort{}, err
	}
	selected := matchingTCPServicePorts(ports, requestedPort)
	if len(selected) == 0 {
		return nil, MonitoringServicePort{}, fmt.Errorf("service does not expose the requested TCP port")
	}
	if requestedPort == 0 && len(selected) != 1 {
		return nil, MonitoringServicePort{}, fmt.Errorf("service port is ambiguous; configure a port explicitly")
	}
	if provider == ProviderCalico && len(ports) != 1 {
		return nil, MonitoringServicePort{}, fmt.Errorf("calico requires exactly one TCP service port")
	}
	return ports, selected[0], nil
}

func normalizeMonitoringServicePorts(servicePorts []corev1.ServicePort) ([]MonitoringServicePort, error) {
	ports := make([]MonitoringServicePort, 0, len(servicePorts))
	for _, servicePort := range servicePorts {
		port, err := normalizeMonitoringServicePort(servicePort)
		if err != nil {
			return nil, err
		}
		ports = append(ports, port)
	}
	return ports, nil
}

func normalizeMonitoringServicePort(servicePort corev1.ServicePort) (MonitoringServicePort, error) {
	if servicePort.Port < 1 || servicePort.Port > 65535 {
		return MonitoringServicePort{}, fmt.Errorf("service exposes an invalid port")
	}
	protocol := servicePort.Protocol
	if protocol == "" {
		protocol = corev1.ProtocolTCP
	}
	port := MonitoringServicePort{
		Port:            servicePort.Port,
		Protocol:        string(protocol),
		ServicePortName: servicePort.Name,
	}
	targetPort := servicePort.TargetPort
	if targetPort.Type == intstr.Int && targetPort.IntVal == 0 {
		targetPort = intstr.FromInt(int(servicePort.Port))
	}
	switch targetPort.Type {
	case intstr.Int:
		if targetPort.IntVal < 1 || targetPort.IntVal > 65535 {
			return MonitoringServicePort{}, fmt.Errorf("service exposes an invalid targetPort")
		}
		port.TargetPort = targetPort.IntVal
	case intstr.String:
		if targetPort.StrVal == "" || len(validation.IsDNS1123Label(targetPort.StrVal)) > 0 {
			return MonitoringServicePort{}, fmt.Errorf("service exposes an invalid named targetPort")
		}
		port.TargetPortName = targetPort.StrVal
	default:
		return MonitoringServicePort{}, fmt.Errorf("service exposes an unsupported targetPort")
	}
	return port, nil
}

func matchingTCPServicePorts(ports []MonitoringServicePort, requestedPort int32) []MonitoringServicePort {
	selected := make([]MonitoringServicePort, 0, len(ports))
	for _, port := range ports {
		if port.Protocol == string(corev1.ProtocolTCP) && (requestedPort == 0 || port.Port == requestedPort) {
			selected = append(selected, port)
		}
	}
	return selected
}

func containsTCPServicePort(ports []MonitoringServicePort, servicePort int32) bool {
	for _, port := range ports {
		if port.Port == servicePort && port.Protocol == string(corev1.ProtocolTCP) {
			return true
		}
	}
	return false
}

func copyStringMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func resolveNamedTargetPort(ctx context.Context, reader client.Reader, service *corev1.Service, servicePort MonitoringServicePort) (int32, error) {
	targetPortName := servicePort.TargetPortName
	if servicePort.ServicePortName == "" {
		return 0, fmt.Errorf("named targetPort %q cannot be matched because the service port has no name", targetPortName)
	}
	slices := &discoveryv1.EndpointSliceList{}
	if err := reader.List(ctx, slices, client.InNamespace(service.Namespace), client.MatchingLabels{discoveryv1.LabelServiceName: service.Name}); err != nil {
		return 0, fmt.Errorf("listing endpointslices: %w", err)
	}
	var resolved int32
	readyEndpoint := false
	for _, slice := range slices.Items {
		if !endpointSliceHasReadyEndpoint(slice) {
			continue
		}
		readyEndpoint = true
		port, found, err := endpointSliceTargetPort(slice, servicePort.ServicePortName, targetPortName)
		if err != nil {
			return 0, err
		}
		if found {
			if resolved != 0 && resolved != port {
				return 0, fmt.Errorf("named targetPort %q resolves to multiple backend ports", targetPortName)
			}
			resolved = port
		}
	}
	if !readyEndpoint {
		return 0, fmt.Errorf("named targetPort %q has no ready endpointslice endpoint", targetPortName)
	}
	if resolved == 0 {
		return 0, fmt.Errorf("named targetPort %q could not be resolved", targetPortName)
	}
	return resolved, nil
}

func endpointSliceHasReadyEndpoint(slice discoveryv1.EndpointSlice) bool {
	for _, endpoint := range slice.Endpoints {
		if endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready {
			return true
		}
	}
	return false
}

func endpointSliceTargetPort(slice discoveryv1.EndpointSlice, servicePortName, targetPortName string) (int32, bool, error) {
	for _, port := range slice.Ports {
		if port.Name == nil || *port.Name != servicePortName {
			continue
		}
		protocol := corev1.ProtocolTCP
		if port.Protocol != nil {
			protocol = *port.Protocol
		}
		if protocol != corev1.ProtocolTCP || port.Port == nil || *port.Port < 1 || *port.Port > 65535 {
			return 0, false, fmt.Errorf("named targetPort %q has no valid TCP backend port", targetPortName)
		}
		return *port.Port, true, nil
	}
	return 0, false, nil
}

// MonitoringServiceReferenceFromURL parses and validates an endpoint URL for
// use by controller event mapping. It shares the same in-cluster-only parser
// as destination resolution, so external or malformed URLs never widen the
// watch scope.
func MonitoringServiceReferenceFromURL(raw string) (MonitoringServiceReference, error) {
	serviceName, namespace, _, err := parseMonitoringURL(raw)
	if err != nil {
		return MonitoringServiceReference{}, err
	}
	return MonitoringServiceReference{Name: serviceName, Namespace: namespace}, nil
}

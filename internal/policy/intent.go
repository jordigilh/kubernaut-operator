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
	"fmt"
	"net"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// Intent is the portable traffic contract rendered into standard Kubernetes
// NetworkPolicy objects. It contains no provider, API-server address, or
// provider-specific identity because egress enforcement is administrator-owned.
type Intent struct {
	Namespace        string
	InstanceName     string
	Components       []string
	ScrapeSources    []kubernautv1alpha2.MetricsScrapeSource
	MetricsPorts     map[string]int32
	IngressOverrides map[string]kubernautv1alpha2.NetworkPolicyNamedIngressOverride
}

// BuildIntent constructs the common policy intent for one Kubernaut namespace.
func BuildIntent(namespace string, components []string) (Intent, error) {
	if namespace == "" {
		return Intent{}, fmt.Errorf("policy namespace is required")
	}
	unique := make(map[string]struct{}, len(components))
	for _, component := range components {
		if component == "" {
			continue
		}
		unique[component] = struct{}{}
	}
	if len(unique) == 0 {
		return Intent{}, fmt.Errorf("at least one policy component is required")
	}
	result := make([]string, 0, len(unique))
	for component := range unique {
		result = append(result, component)
	}
	sort.Strings(result)
	return Intent{
		Namespace:        namespace,
		InstanceName:     namespace,
		Components:       result,
		MetricsPorts:     make(map[string]int32),
		IngressOverrides: make(map[string]kubernautv1alpha2.NetworkPolicyNamedIngressOverride),
	}, nil
}

// ValidateNetworkPoliciesSpec validates the portions of the CRD contract that
// are not fully enforced by the structural schema, including programmatic
// callers that bypass API-server validation.
func ValidateNetworkPoliciesSpec(spec kubernautv1alpha2.NetworkPoliciesSpec) error {
	if err := validateScrapeSources(spec.ScrapeSources); err != nil {
		return err
	}
	for component, override := range map[string]kubernautv1alpha2.NetworkPolicyNamedIngressOverride{
		"gateway":     spec.Gateway,
		"apifrontend": spec.APIFrontend,
		"console":     spec.Console,
	} {
		if err := validateIngressOverride(component, override.NetworkPolicyIngressOverride); err != nil {
			return err
		}
		for _, namespace := range override.IngressNamespaces {
			if len(validation.IsDNS1123Label(namespace)) != 0 {
				return fmt.Errorf("network policy %s ingress namespace %q is invalid", component, namespace)
			}
		}
	}
	for component, override := range map[string]kubernautv1alpha2.NetworkPolicyIngressOverride{
		"datastorage":    spec.DataStorage,
		"kubernautAgent": spec.KubernautAgent,
	} {
		if err := validateIngressOverride(component, override); err != nil {
			return err
		}
	}
	return nil
}

func validateIngressOverride(component string, override kubernautv1alpha2.NetworkPolicyIngressOverride) error {
	for _, cidr := range override.IngressCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("network policy %s ingress CIDR %q is invalid: %w", component, cidr, err)
		}
	}
	for index, selector := range override.IngressNamespaceSelectors {
		if _, err := metav1.LabelSelectorAsSelector(&selector); err != nil {
			return fmt.Errorf("network policy %s ingress namespace selector %d is invalid: %w", component, index, err)
		}
	}
	return nil
}

// validateScrapeSources protects programmatic callers from selector injection
// and reserved namespace identity keys.
func validateScrapeSources(sources []kubernautv1alpha2.MetricsScrapeSource) error {
	if len(sources) > 16 {
		return fmt.Errorf("at most 16 metrics scrape sources are supported")
	}
	for index, source := range sources {
		if len(validation.IsDNS1123Label(source.Namespace)) != 0 {
			return fmt.Errorf("metrics scrape source %d requires a valid exact namespace", index)
		}
		if len(source.PodLabels) == 0 || len(source.PodLabels) > 16 {
			return fmt.Errorf("metrics scrape source %d requires 1 to 16 exact pod labels", index)
		}
		for key, value := range source.PodLabels {
			if len(validation.IsQualifiedName(key)) != 0 || len(validation.IsValidLabelValue(value)) != 0 {
				return fmt.Errorf("metrics scrape source %d has an invalid pod label", index)
			}
			if key == "kubernetes.io/metadata.name" || strings.HasPrefix(key, "kubernetes.io/") {
				return fmt.Errorf("metrics scrape source %d cannot override reserved namespace identity", index)
			}
		}
	}
	return nil
}

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
	"sort"
	"strings"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// APIServerIdentity describes the provider-native service identity used for
// Kubernetes API access. It intentionally has no CIDR field.
type APIServerIdentity struct {
	ServiceName string
	Namespace   string
}

// Intent is the provider-neutral traffic contract. Provider adapters may
// express this contract only with their native identity and selector fields;
// they must reject any part they cannot express safely.
type Intent struct {
	Namespace            string
	InstanceName         string
	Components           []string
	APIServerIdentity    APIServerIdentity
	DNSNamespace         string
	StaticAPIServerCIDRs []string
}

// BuildIntent constructs the common policy intent for one Kubernaut namespace.
// staticAPIServerCIDRs is retained as an input guard for callers migrating
// from the legacy renderer; it is never copied into the resulting intent.
func BuildIntent(namespace string, components, staticAPIServerCIDRs []string) (Intent, error) {
	if namespace == "" {
		return Intent{}, fmt.Errorf("policy namespace is required")
	}
	if len(staticAPIServerCIDRs) > 0 {
		return Intent{}, fmt.Errorf("static API-server CIDRs are not supported by native policy adapters")
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
		Namespace:    namespace,
		InstanceName: namespace,
		Components:   result,
		APIServerIdentity: APIServerIdentity{
			ServiceName: "kubernetes",
			Namespace:   "default",
		},
		DNSNamespace: "kube-system",
	}, nil
}

// ValidateNativeOverrides rejects legacy raw-NetworkPolicy tuning fields that
// the bounded native adapters cannot express without silently changing the
// requested traffic contract. Ignoring one of these fields would be worse than
// refusing reconciliation because the user could believe the policy was
// narrowed when the provider object did not change.
func ValidateNativeOverrides(spec kubernautv1alpha2.NetworkPoliciesSpec) error {
	unsupported := make([]string, 0, 16)
	if spec.APIServerCIDR != "" {
		unsupported = append(unsupported, "apiServerCIDR")
	}
	if len(spec.APIServerCIDRs) > 0 {
		unsupported = append(unsupported, "apiServerCIDRs")
	}
	if spec.APIServerPort != 0 {
		unsupported = append(unsupported, "apiServerPort")
	}
	if hasEgressOverride(spec.Monitoring.Namespace, spec.Monitoring.PrometheusPort, spec.Monitoring.AlertManagerPort) {
		unsupported = append(unsupported, "monitoring")
	}
	for name, override := range map[string]kubernautv1alpha2.NetworkPolicyEgressOverride{
		"externalWebhooks": spec.ExternalWebhooks,
		"llm":              spec.LLM,
		"mcpGateway":       spec.MCPGateway,
		"prometheus":       spec.Prometheus,
	} {
		if hasEgressOverride(override.CIDR, override.Port) {
			unsupported = append(unsupported, name)
		}
	}
	if hasEgressOverride(spec.IdP.CIDR, spec.IdP.Port) || len(spec.IdP.ExtraPorts) > 0 {
		unsupported = append(unsupported, "idp")
	}
	for name, override := range map[string]kubernautv1alpha2.NetworkPolicyNamedIngressOverride{
		"gateway":     spec.Gateway,
		"apifrontend": spec.APIFrontend,
		"console":     spec.Console,
	} {
		if hasNamedIngressOverride(override) {
			unsupported = append(unsupported, name)
		}
	}
	for name, override := range map[string]kubernautv1alpha2.NetworkPolicyIngressOverride{
		"datastorage":    spec.DataStorage,
		"kubernautAgent": spec.KubernautAgent,
	} {
		if hasIngressOverride(override) {
			unsupported = append(unsupported, name)
		}
	}
	if len(unsupported) == 0 {
		return nil
	}
	sort.Strings(unsupported)
	return fmt.Errorf("network policy fields are not supported by native adapters: %s", strings.Join(unsupported, ", "))
}

func hasEgressOverride(cidr string, ports ...int32) bool {
	if cidr != "" {
		return true
	}
	for _, port := range ports {
		if port != 0 {
			return true
		}
	}
	return false
}

func hasIngressOverride(spec kubernautv1alpha2.NetworkPolicyIngressOverride) bool {
	return len(spec.IngressCIDRs) > 0 || len(spec.IngressNamespaceSelectors) > 0
}

func hasNamedIngressOverride(spec kubernautv1alpha2.NetworkPolicyNamedIngressOverride) bool {
	return hasIngressOverride(spec.NetworkPolicyIngressOverride) || len(spec.IngressNamespaces) > 0
}

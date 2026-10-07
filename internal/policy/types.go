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
	"sort"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Provider is the native policy provider selected for a Kubernaut instance.
type Provider string

const (
	ProviderAuto   Provider = "Auto"
	ProviderCilium Provider = "Cilium"
	ProviderCalico Provider = "Calico"
	ProviderOVN    Provider = "OVN"
	ProviderNone   Provider = "None"
)

const (
	ManagedPolicyLabel   = "kubernaut.ai/managed-policy"
	ProviderLabel        = "kubernaut.ai/policy-provider"
	PolicyNamespaceLabel = "kubernaut.ai/policy-namespace"
	ManagedByLabel       = "app.kubernetes.io/managed-by"
)

// ManagedResourceKind describes one provider-native resource family that may
// be pruned when the selected provider changes or the Kubernaut is deleted.
type ManagedResourceKind struct {
	GVK        schema.GroupVersionKind
	ListKind   string
	Namespaced bool
}

// ManagedResourceKinds returns only provider-native policy families owned by
// this operator. It never includes the core Kubernetes NetworkPolicy GVK.
func ManagedResourceKinds() []ManagedResourceKind {
	return []ManagedResourceKind{
		{GVK: schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}, ListKind: "CiliumNetworkPolicyList", Namespaced: true},
		{GVK: schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}, ListKind: "NetworkPolicyList", Namespaced: true},
		{GVK: schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "AdminNetworkPolicy"}, ListKind: "AdminNetworkPolicyList", Namespaced: false},
		{GVK: schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "BaselineAdminNetworkPolicy"}, ListKind: "BaselineAdminNetworkPolicyList", Namespaced: false},
	}
}

// ProviderForGVK returns the provider ownership label associated with one of
// the native policy families managed by this operator. Unknown GVKs are not
// considered operator-owned and return ProviderNone.
func ProviderForGVK(gvk schema.GroupVersionKind) Provider {
	switch gvk {
	case schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}:
		return ProviderCilium
	case schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}:
		return ProviderCalico
	case schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "AdminNetworkPolicy"},
		schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "BaselineAdminNetworkPolicy"}:
		return ProviderOVN
	default:
		return ProviderNone
	}
}

const (
	ReasonProviderReady         = "ProviderReady"
	ReasonNoSupportedProvider   = "NoSupportedProvider"
	ReasonNoActiveInstallation  = "NoActiveInstallation"
	ReasonAmbiguousProvider     = "AmbiguousProvider"
	ReasonUnsupportedProvider   = "UnsupportedProvider"
	ReasonUnsupportedVersion    = "UnsupportedVersion"
	ReasonSchemaInvalid         = "SchemaInvalid"
	ReasonProviderUnavailable   = "ProviderUnavailable"
	ReasonMonitoringUnavailable = "MonitoringUnavailable"
)

// ResourceCapability records discovery and schema evidence for one provider
// resource. A CRD's existence is not enough; the served GVK and structural
// schema are checked before a provider is considered usable.
type ResourceCapability struct {
	GVK         schema.GroupVersionKind
	CRDName     string
	SchemaValid bool
}

// ProviderSnapshot is the immutable result of provider/platform discovery.
// It is deliberately separate from Detect so unit tests and future discovery
// backends can exercise selection without a live Kubernetes API.
type ProviderSnapshot struct {
	Provider                   Provider
	APIAvailable               bool
	Active                     bool
	Version                    string
	Platform                   string
	PlatformVersion            string
	SchemaValid                bool
	APIServerIdentityAvailable bool
	RequiredGVKs               []schema.GroupVersionKind
	Evidence                   []string
	Diagnostic                 string
}

// DiscoverySnapshot contains all bounded-provider candidates found during a
// single reconciliation. Candidates are keyed by their canonical Provider.
type DiscoverySnapshot struct {
	Candidates map[Provider]ProviderSnapshot
}

// DetectionResult is the provider decision consumed by the policy lifecycle.
type DetectionResult struct {
	Requested     Provider
	Provider      Provider
	Ready         bool
	Reason        string
	Message       string
	PolicyReason  string
	PolicyMessage string
	Version       string
	Platform      string
	Candidates    []Provider
	Evidence      []string
}

// RequiredGVKs returns the native policy GVKs required by each adapter.
func RequiredGVKs(provider Provider) []schema.GroupVersionKind {
	switch provider {
	case ProviderCilium:
		return []schema.GroupVersionKind{{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}}
	case ProviderCalico:
		return []schema.GroupVersionKind{{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}}
	case ProviderOVN:
		return []schema.GroupVersionKind{
			{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "AdminNetworkPolicy"},
			{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "BaselineAdminNetworkPolicy"},
		}
	default:
		return nil
	}
}

func supportedProviders() []Provider {
	return []Provider{ProviderCilium, ProviderCalico, ProviderOVN}
}

// Detect selects one provider without ever treating a raw Kubernetes
// NetworkPolicy as a fallback. Auto selection requires exactly one active,
// schema-compatible, version-qualified candidate. Explicit selection only
// narrows the candidate set; it does not bypass validation.
func Detect(snapshot DiscoverySnapshot, requested Provider) DetectionResult {
	requested = normalizeProvider(requested)
	result := DetectionResult{Requested: requested, Provider: ProviderNone}

	if requested != ProviderAuto && !isSupportedProvider(requested) {
		result.Reason = ReasonUnsupportedProvider
		result.Message = "requested policy provider is not in the supported provider matrix"
		return result
	}

	providers := supportedProviders()
	if requested != ProviderAuto {
		providers = []Provider{requested}
	}
	valid := make([]ProviderSnapshot, 0, len(providers))
	failureReasons := make([]string, 0, len(providers))
	for _, provider := range providers {
		candidate, ok := snapshot.Candidates[provider]
		if !ok {
			failureReasons = append(failureReasons, ReasonProviderUnavailable)
			continue
		}
		if reason := candidateFailureReason(candidate, provider); reason != "" {
			failureReasons = append(failureReasons, reason)
			continue
		}
		valid = append(valid, candidate)
	}

	if len(valid) == 1 {
		candidate := valid[0]
		result.Provider = candidate.Provider
		result.Ready = true
		result.Reason = ReasonProviderReady
		result.Version = candidate.Version
		if candidate.Provider == ProviderOVN {
			result.Version = candidate.PlatformVersion
		}
		result.Platform = candidate.Platform
		result.Evidence = append([]string(nil), candidate.Evidence...)
		result.Message = "native policy provider is active and schema-compatible"
		return result
	}
	if len(valid) > 1 {
		result.Reason = ReasonAmbiguousProvider
		result.Candidates = make([]Provider, 0, len(valid))
		for _, candidate := range valid {
			result.Candidates = append(result.Candidates, candidate.Provider)
		}
		sort.Slice(result.Candidates, func(i, j int) bool { return result.Candidates[i] < result.Candidates[j] })
		result.Message = "multiple supported policy providers have active installation evidence; select one explicitly"
		return result
	}

	if requested == ProviderAuto && len(snapshot.Candidates) == 0 {
		result.Reason = ReasonNoSupportedProvider
	} else {
		result.Reason = firstFailureReason(failureReasons)
	}
	if result.Reason == "" {
		result.Reason = ReasonNoSupportedProvider
	}
	if requested == ProviderAuto {
		result.Message = "no single supported native policy provider is active; no provider policy objects were created"
	} else {
		result.Message = "the explicitly selected policy provider is unavailable or incompatible; no provider policy objects were created"
	}
	return result
}

func candidateFailureReason(candidate ProviderSnapshot, provider Provider) string {
	if !candidate.Active {
		if candidate.APIAvailable {
			return ReasonNoActiveInstallation
		}
		return ReasonNoSupportedProvider
	}
	if !candidate.APIAvailable {
		return ReasonProviderUnavailable
	}
	if !candidate.SchemaValid || !hasRequiredGVKs(candidate.RequiredGVKs, provider) {
		return ReasonSchemaInvalid
	}
	if !candidate.APIServerIdentityAvailable {
		return ReasonProviderUnavailable
	}
	if !versionSupported(provider, candidate.Version, candidate.PlatformVersion) {
		return ReasonUnsupportedVersion
	}
	return ""
}

func normalizeProvider(provider Provider) Provider {
	if provider == "" {
		return ProviderAuto
	}
	return provider
}

func isSupportedProvider(provider Provider) bool {
	for _, supported := range supportedProviders() {
		if provider == supported {
			return true
		}
	}
	return false
}

func hasRequiredGVKs(actual []schema.GroupVersionKind, provider Provider) bool {
	for _, expected := range RequiredGVKs(provider) {
		found := false
		for _, candidate := range actual {
			if candidate == expected {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func firstFailureReason(reasons []string) string {
	priority := []string{
		ReasonUnsupportedProvider,
		ReasonUnsupportedVersion,
		ReasonSchemaInvalid,
		ReasonNoActiveInstallation,
		ReasonNoSupportedProvider,
		ReasonProviderUnavailable,
	}
	for _, preferred := range priority {
		for _, actual := range reasons {
			if actual == preferred {
				return preferred
			}
		}
	}
	return ""
}

func versionSupported(provider Provider, version, platformVersion string) bool {
	value := version
	if provider == ProviderOVN {
		value = platformVersion
	}
	major, minor, ok := parseMajorMinor(value)
	if !ok {
		return false
	}
	switch provider {
	case ProviderCilium:
		return major == 1 && (minor == 19 || minor == 20)
	case ProviderCalico:
		return major == 3 && (minor == 31 || minor == 32)
	case ProviderOVN:
		return major == 4 && minor >= 19 && minor <= 22
	default:
		return false
	}
}

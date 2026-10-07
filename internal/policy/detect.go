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
	"regexp"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	schemaTypeArray  = "array"
	schemaTypeObject = "object"
)

// Detector combines provider API/schema discovery with active-installation
// evidence. It never creates provider objects or provider CRDs.
type Detector struct {
	Client     client.Client
	RESTMapper meta.RESTMapper
}

// Discover builds a snapshot for the bounded provider matrix. Discovery
// failures are represented as diagnostics on candidates so the controller can
// publish a condition and continue the core lifecycle without retry storms.
func (d Detector) Discover(ctx context.Context) DiscoverySnapshot {
	snapshot := DiscoverySnapshot{Candidates: make(map[Provider]ProviderSnapshot, 3)}
	snapshot.Candidates[ProviderCilium] = d.discoverCilium(ctx)
	snapshot.Candidates[ProviderCalico] = d.discoverCalico(ctx)
	snapshot.Candidates[ProviderOVN] = d.discoverOVN(ctx)
	return snapshot
}

func (d Detector) discoverCilium(ctx context.Context) ProviderSnapshot {
	result := ProviderSnapshot{Provider: ProviderCilium}
	result.RequiredGVKs = d.discoverResources(ctx, ProviderCilium)
	result.APIAvailable = len(result.RequiredGVKs) > 0
	result.SchemaValid = len(result.RequiredGVKs) == len(RequiredGVKs(ProviderCilium))
	version, evidence := d.daemonSetEvidence(ctx, func(ds appsv1.DaemonSet) bool {
		return isProviderDaemonSet(ds, "cilium")
	})
	result.Version = version
	result.Evidence = evidence
	result.Active = len(evidence) > 0
	result.APIServerIdentityAvailable = result.SchemaValid
	if !result.Active {
		result.Diagnostic = "no ready Cilium daemonset was discovered"
	}
	if !result.SchemaValid {
		result.Diagnostic = "CiliumNetworkPolicy GVK/schema is unavailable"
	}
	return result
}

func (d Detector) discoverCalico(ctx context.Context) ProviderSnapshot {
	result := ProviderSnapshot{Provider: ProviderCalico}
	result.RequiredGVKs = d.discoverResources(ctx, ProviderCalico)
	result.APIAvailable = len(result.RequiredGVKs) > 0
	result.SchemaValid = len(result.RequiredGVKs) == len(RequiredGVKs(ProviderCalico))
	version, evidence := d.daemonSetEvidence(ctx, func(ds appsv1.DaemonSet) bool {
		return isProviderDaemonSet(ds, "calico-node")
	})
	result.Version = version
	result.Evidence = evidence
	result.Active = len(evidence) > 0
	result.APIServerIdentityAvailable = d.calicoKubernetesDatastore(ctx)
	if !result.Active {
		result.Diagnostic = "no ready Calico daemonset was discovered"
	}
	if !result.SchemaValid {
		result.Diagnostic = "Calico NetworkPolicy GVK/schema is unavailable"
	}
	if result.Active && !result.APIServerIdentityAvailable {
		result.Diagnostic = "Calico Kubernetes datastore is unavailable; service-based API-server identity cannot be expressed"
	}
	return result
}

func (d Detector) discoverOVN(ctx context.Context) ProviderSnapshot {
	result := ProviderSnapshot{Provider: ProviderOVN}
	result.RequiredGVKs = d.discoverResources(ctx, ProviderOVN)
	result.APIAvailable = len(result.RequiredGVKs) > 0
	result.SchemaValid = len(result.RequiredGVKs) == len(RequiredGVKs(ProviderOVN))

	if d.Client == nil {
		result.Diagnostic = "Kubernetes client is unavailable"
		return result
	}
	network := &unstructured.Unstructured{}
	network.SetAPIVersion("config.openshift.io/v1")
	network.SetKind("Network")
	err := d.Client.Get(ctx, client.ObjectKey{Name: "cluster"}, network)
	if err != nil {
		result.Diagnostic = "OpenShift Network configuration is unavailable"
		return result
	}
	networkType, _, _ := unstructured.NestedString(network.Object, "spec", "networkType")
	if networkType == "" {
		// Older OpenShift representations exposed the plugin below
		// spec.defaultNetwork.type. Prefer the current NetworkSpec field while
		// retaining compatibility with that representation.
		networkType, _, _ = unstructured.NestedString(network.Object, "spec", "defaultNetwork", "type")
	}
	if networkType != "OVNKubernetes" {
		result.Diagnostic = "OpenShift defaultNetwork.type is not OVNKubernetes"
		return result
	}
	result.Active = true
	result.Platform = "OpenShift"
	result.APIServerIdentityAvailable = result.SchemaValid
	result.Evidence = []string{"config.openshift.io/Network:cluster", "defaultNetwork.type=OVNKubernetes"}

	clusterVersion := &unstructured.Unstructured{}
	clusterVersion.SetAPIVersion("config.openshift.io/v1")
	clusterVersion.SetKind("ClusterVersion")
	if err := d.Client.Get(ctx, client.ObjectKey{Name: "version"}, clusterVersion); err == nil {
		result.Platform = "OpenShift"
		if version, found, _ := unstructured.NestedString(clusterVersion.Object, "status", "desired", "version"); found {
			result.PlatformVersion = version
		}
	}
	if !result.SchemaValid {
		result.Diagnostic = "OVN AdminNetworkPolicy/BaselineAdminNetworkPolicy GVK/schema is unavailable"
	}
	return result
}

func (d Detector) discoverResources(ctx context.Context, provider Provider) []schema.GroupVersionKind {
	resources := RequiredGVKs(provider)
	valid := make([]schema.GroupVersionKind, 0, len(resources))
	if d.Client == nil || d.RESTMapper == nil {
		return valid
	}
	for _, expected := range resources {
		if d.discoverResource(ctx, expected) {
			valid = append(valid, expected)
		}
	}
	return valid
}

func (d Detector) discoverResource(ctx context.Context, expected schema.GroupVersionKind) bool {
	crdName, ok := crdNameForGVK(expected)
	if !ok {
		return false
	}
	if !d.resourceMappingMatches(expected, crdName) {
		return false
	}
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := d.Client.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
		// Calico's supported v3 API is commonly served by its aggregation
		// API server while the backing objects remain crd.projectcalico.org/v1
		// CRDs. In that mode there is no projectcalico.org CRD to inspect;
		// RESTMapper discovery plus active Calico evidence is the runtime API
		// and compatibility signal. Native v3 CRDs still take the stricter
		// schema-validation path below.
		if expected.Group == "projectcalico.org" && apierrors.IsNotFound(err) {
			return true
		}
		return false
	}
	if !crdIdentityMatches(crd, expected, crdName) {
		return false
	}
	return crdHasSupportedSchema(crd, expected)
}

func (d Detector) resourceMappingMatches(expected schema.GroupVersionKind, crdName string) bool {
	mapping, err := d.RESTMapper.RESTMapping(expected.GroupKind())
	if err != nil || mapping.GroupVersionKind.GroupVersion() != expected.GroupVersion() {
		return false
	}
	plural := strings.TrimSuffix(crdName, "."+expected.Group)
	return expectedScope(expected) == mapping.Scope.Name() && mapping.Resource.Resource == plural
}

func crdIdentityMatches(crd *apiextensionsv1.CustomResourceDefinition, expected schema.GroupVersionKind, crdName string) bool {
	plural := strings.TrimSuffix(crdName, "."+expected.Group)
	return crd.Spec.Group == expected.Group &&
		crd.Spec.Names.Kind == expected.Kind &&
		crd.Spec.Names.Plural == plural &&
		crd.Spec.Scope == expectedCRDScope(expected)
}

func crdHasSupportedSchema(crd *apiextensionsv1.CustomResourceDefinition, expected schema.GroupVersionKind) bool {
	for _, version := range crd.Spec.Versions {
		if version.Name != expected.Version || !version.Served || version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
			continue
		}
		if schemaSupportsPolicy(expected, version.Schema.OpenAPIV3Schema) {
			return true
		}
	}
	return false
}

// schemaSupportsPolicy verifies the provider-specific fields consumed by the
// renderer, not merely that a CRD has a structural object-shaped spec. A stale
// or unrelated CRD with the expected GVK must not be enough to make the
// operator submit a security policy that the provider cannot interpret.
func schemaSupportsPolicy(gvk schema.GroupVersionKind, root *apiextensionsv1.JSONSchemaProps) bool {
	if root == nil || root.Type != schemaTypeObject || root.Properties == nil {
		return false
	}
	spec, ok := root.Properties["spec"]
	if !ok || spec.Type != schemaTypeObject || spec.Properties == nil {
		return false
	}

	var fields map[string]string
	switch gvk {
	case schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}:
		fields = map[string]string{"endpointSelector": schemaTypeObject, "enableDefaultDeny": schemaTypeObject, "ingress": schemaTypeArray, "egress": schemaTypeArray}
	case schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}:
		fields = map[string]string{"selector": "string", "order": "number", "types": schemaTypeArray, "ingress": schemaTypeArray, "egress": schemaTypeArray}
	case schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "AdminNetworkPolicy"}:
		fields = map[string]string{"priority": "integer", "subject": schemaTypeObject, "ingress": schemaTypeArray, "egress": schemaTypeArray}
	case schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "BaselineAdminNetworkPolicy"}:
		fields = map[string]string{"subject": schemaTypeObject, "ingress": schemaTypeArray, "egress": schemaTypeArray}
	default:
		return false
	}
	for field, expectedType := range fields {
		property, ok := spec.Properties[field]
		if !ok || property.Type != expectedType {
			return false
		}
	}
	return schemaSupportsMonitoringFields(gvk, spec.Properties["egress"])
}

func schemaSupportsMonitoringFields(gvk schema.GroupVersionKind, egress apiextensionsv1.JSONSchemaProps) bool {
	if gvk.Group != "cilium.io" && gvk.Group != "projectcalico.org" {
		return true
	}
	if egress.Type != schemaTypeArray || egress.Items == nil || egress.Items.Schema == nil {
		return false
	}
	item := egress.Items.Schema
	if item.Type != schemaTypeObject || item.Properties == nil {
		return false
	}
	switch gvk {
	case schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}:
		return item.Properties["toServices"].Type == schemaTypeArray && item.Properties["toPorts"].Type == schemaTypeArray
	case schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}:
		destination, ok := item.Properties["destination"]
		if !ok || destination.Type != schemaTypeObject || destination.Properties == nil {
			return false
		}
		return destination.Properties["services"].Type == schemaTypeObject
	default:
		return true
	}
}

func expectedScope(gvk schema.GroupVersionKind) meta.RESTScopeName {
	if gvk.Group == "policy.networking.k8s.io" {
		return meta.RESTScopeRoot.Name()
	}
	return meta.RESTScopeNamespace.Name()
}

func expectedCRDScope(gvk schema.GroupVersionKind) apiextensionsv1.ResourceScope {
	if expectedScope(gvk) == meta.RESTScopeRoot.Name() {
		return apiextensionsv1.ClusterScoped
	}
	return apiextensionsv1.NamespaceScoped
}

func (d Detector) daemonSetEvidence(ctx context.Context, match func(appsv1.DaemonSet) bool) (string, []string) {
	if d.Client == nil {
		return "", nil
	}
	daemonSets := &appsv1.DaemonSetList{}
	if err := d.Client.List(ctx, daemonSets); err != nil {
		return "", nil
	}
	var version string
	evidence := make([]string, 0, 2)
	for _, daemonSet := range daemonSets.Items {
		if !match(daemonSet) || daemonSet.Status.NumberReady == 0 {
			continue
		}
		evidence = append(evidence, fmt.Sprintf("daemonset/%s/%s ready", daemonSet.Namespace, daemonSet.Name))
		if version == "" {
			version = providerVersionFromImages(daemonSet.Spec.Template.Spec.Containers)
		}
	}
	return version, evidence
}

// calicoKubernetesDatastore reports whether every ready Calico node agent
// advertises the Kubernetes datastore. Calico's service-match destination is
// not supported by the etcd datastore, so emitting the API-server service rule
// without this capability would create a policy that fails closed for API
// traffic while appearing successfully reconciled.
func (d Detector) calicoKubernetesDatastore(ctx context.Context) bool {
	if d.Client == nil {
		return false
	}
	daemonSets := &appsv1.DaemonSetList{}
	if err := d.Client.List(ctx, daemonSets); err != nil {
		return false
	}
	found := false
	for _, daemonSet := range daemonSets.Items {
		if !isProviderDaemonSet(daemonSet, "calico-node") || daemonSet.Status.NumberReady == 0 {
			continue
		}
		found = true
		if datastore, ok := calicoDatastoreType(daemonSet); !ok || !strings.EqualFold(datastore, "kubernetes") {
			return false
		}
	}
	return found
}

func calicoDatastoreType(daemonSet appsv1.DaemonSet) (string, bool) {
	containers := make([]corev1.Container, 0, len(daemonSet.Spec.Template.Spec.InitContainers)+len(daemonSet.Spec.Template.Spec.Containers))
	containers = append(containers, daemonSet.Spec.Template.Spec.InitContainers...)
	containers = append(containers, daemonSet.Spec.Template.Spec.Containers...)
	for _, container := range containers {
		for _, env := range container.Env {
			if env.Name == "DATASTORE_TYPE" && env.Value != "" {
				return strings.TrimSpace(env.Value), true
			}
		}
		for _, arg := range container.Args {
			if value, found := strings.CutPrefix(arg, "--datastore-type="); found && value != "" {
				return strings.TrimSpace(value), true
			}
		}
	}
	return "", false
}

func isProviderDaemonSet(daemonSet appsv1.DaemonSet, expected string) bool {
	if strings.EqualFold(daemonSet.Name, expected) {
		return true
	}
	for _, key := range []string{"k8s-app", "app", "app.kubernetes.io/name", "name"} {
		if strings.EqualFold(strings.TrimSpace(daemonSet.Labels[key]), expected) {
			return true
		}
	}
	return false
}

func providerVersionFromImages(containers []corev1.Container) string {
	for _, container := range containers {
		if match := providerVersionPattern.FindStringSubmatch(container.Image); len(match) == 4 {
			return strings.Join(match[1:], ".")
		}
	}
	return ""
}

var providerVersionPattern = regexp.MustCompile(`(?:^|[/@:])v?(\d+)\.(\d+)\.(\d+)`)

func crdNameForGVK(gvk schema.GroupVersionKind) (string, bool) {
	switch gvk {
	case schema.GroupVersionKind{Group: "cilium.io", Version: "v2", Kind: "CiliumNetworkPolicy"}:
		return "ciliumnetworkpolicies.cilium.io", true
	case schema.GroupVersionKind{Group: "projectcalico.org", Version: "v3", Kind: "NetworkPolicy"}:
		return "networkpolicies.projectcalico.org", true
	case schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "AdminNetworkPolicy"}:
		return "adminnetworkpolicies.policy.networking.k8s.io", true
	case schema.GroupVersionKind{Group: "policy.networking.k8s.io", Version: "v1alpha1", Kind: "BaselineAdminNetworkPolicy"}:
		return "baselineadminnetworkpolicies.policy.networking.k8s.io", true
	default:
		return "", false
	}
}

func parseMajorMinor(version string) (int, int, bool) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

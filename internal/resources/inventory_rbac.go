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

package resources

import (
	"sort"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	apiFrontendInventoryReadClusterRoleBase        = "apifrontend-inventory-read"
	apiFrontendInventoryReadClusterRoleBindingBase = "apifrontend-inventory-read-binding"
)

// APIFrontendInventoryReadClusterRoleName returns the stable name of the
// separate AF metadata-inventory ClusterRole.
func APIFrontendInventoryReadClusterRoleName(kn *kubernautv1alpha2.Kubernaut) string {
	return clusterRoleName(kn, apiFrontendInventoryReadClusterRoleBase)
}

// APIFrontendInventoryReadClusterRoleBindingName returns the stable name of
// the binding for the separate AF metadata-inventory ClusterRole.
func APIFrontendInventoryReadClusterRoleBindingName(kn *kubernautv1alpha2.Kubernaut) string {
	return clusterRoleName(kn, apiFrontendInventoryReadClusterRoleBindingBase)
}

// APIFrontendInventoryReadClusterRole builds the read-only metadata boundary
// for AF inventory. The additional roles are the administrator-approved
// Gateway source roles; their read rules are projected without copying writes,
// subresources, or non-resource URLs.
func APIFrontendInventoryReadClusterRole(kn *kubernautv1alpha2.Kubernaut, additionalRoles []rbacv1.ClusterRole) *rbacv1.ClusterRole {
	if kn == nil || !kn.Spec.APIFrontendEnabled() {
		return nil
	}

	labels := CommonLabels(kn)
	labels[LabelCoreClusterRBAC] = LabelValueTrue
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   APIFrontendInventoryReadClusterRoleName(kn),
			Labels: labels,
		},
		Rules: inventoryReadRules(additionalRoles),
	}
}

// APIFrontendInventoryReadClusterRoleBinding binds only AF to the separate
// inventory role. It deliberately does not reuse the mixed AF or Gateway
// ClusterRoles.
func APIFrontendInventoryReadClusterRoleBinding(kn *kubernautv1alpha2.Kubernaut) *rbacv1.ClusterRoleBinding {
	if kn == nil || !kn.Spec.APIFrontendEnabled() {
		return nil
	}

	labels := CommonLabels(kn)
	labels[LabelCoreClusterRBAC] = LabelValueTrue
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   APIFrontendInventoryReadClusterRoleBindingName(kn),
			Labels: labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     APIFrontendInventoryReadClusterRoleName(kn),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      ServiceAccountName(ComponentAPIFrontend),
			Namespace: kn.Namespace,
		}},
	}
}

// gatewayTargetResourceReadRules is the shared operator-owned target-resource
// read boundary. Gateway uses the complete get/list/watch rules; AF inventory
// projects only the get/list subset. Gateway's RemediationRequest, lease,
// TokenReview, and SubjectAccessReview rules remain operational permissions
// and are intentionally not part of inventory.
func gatewayTargetResourceReadRules() []rbacv1.PolicyRule {
	baseRules := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"nodes", "pods", "services", "persistentvolumes", "persistentvolumeclaims"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list", "watch"}},
	}
	ownerChainRules := ownerChainResolutionRules()
	rules := make([]rbacv1.PolicyRule, 0, len(baseRules)+len(ownerChainRules))
	rules = append(rules, baseRules...)
	return append(rules, ownerChainRules...)
}

func inventoryReadRules(additionalRoles []rbacv1.ClusterRole) []rbacv1.PolicyRule {
	sources := gatewayTargetResourceReadRules()
	for _, role := range additionalRoles {
		sources = append(sources, role.Rules...)
	}

	byKey := make(map[string]rbacv1.PolicyRule, len(sources))
	for _, source := range sources {
		rule, ok := inventoryReadRule(source)
		if !ok {
			continue
		}
		key := inventoryReadRuleKey(rule)
		if existing, exists := byKey[key]; exists {
			existing.Verbs = mergeSortedStrings(existing.Verbs, rule.Verbs)
			byKey[key] = existing
			continue
		}
		byKey[key] = rule
	}

	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	rules := make([]rbacv1.PolicyRule, 0, len(keys))
	for _, key := range keys {
		rules = append(rules, byKey[key])
	}
	return rules
}

func inventoryReadRule(source rbacv1.PolicyRule) (rbacv1.PolicyRule, bool) {
	if len(source.NonResourceURLs) > 0 {
		return rbacv1.PolicyRule{}, false
	}

	resources := make([]string, 0, len(source.Resources))
	seenResources := make(map[string]struct{}, len(source.Resources))
	for _, resource := range source.Resources {
		if strings.Contains(resource, "/") {
			continue
		}
		if _, seen := seenResources[resource]; seen {
			continue
		}
		seenResources[resource] = struct{}{}
		resources = append(resources, resource)
	}
	if len(resources) == 0 {
		return rbacv1.PolicyRule{}, false
	}

	verbs := make([]string, 0, 2)
	seenVerbs := make(map[string]struct{}, 2)
	for _, verb := range source.Verbs {
		if verb != "*" && verb != "get" && verb != "list" {
			continue
		}
		if verb == "*" {
			verbs = []string{"get", "list"}
			break
		}
		if _, seen := seenVerbs[verb]; seen {
			continue
		}
		seenVerbs[verb] = struct{}{}
		verbs = append(verbs, verb)
	}
	if len(verbs) == 0 {
		return rbacv1.PolicyRule{}, false
	}

	groups := append([]string(nil), source.APIGroups...)
	resourceNames := append([]string(nil), source.ResourceNames...)
	sort.Strings(groups)
	sort.Strings(resources)
	sort.Strings(resourceNames)
	sort.Strings(verbs)
	return rbacv1.PolicyRule{
		APIGroups:     groups,
		Resources:     resources,
		ResourceNames: resourceNames,
		Verbs:         verbs,
	}, true
}

func inventoryReadRuleKey(rule rbacv1.PolicyRule) string {
	return strings.Join([]string{
		strings.Join(rule.APIGroups, "\x00"),
		strings.Join(rule.Resources, "\x00"),
		strings.Join(rule.ResourceNames, "\x00"),
	}, "\x00\x01")
}

func mergeSortedStrings(left, right []string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	merged := make([]string, 0, len(left)+len(right))
	for _, value := range append(append([]string(nil), left...), right...) {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	sort.Strings(merged)
	return merged
}

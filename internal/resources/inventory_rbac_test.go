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
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func inventoryRulesFor(role *rbacv1.ClusterRole, apiGroup, resource string) []rbacv1.PolicyRule {
	rules := make([]rbacv1.PolicyRule, 0)
	for _, rule := range role.Rules {
		if slices.Contains(rule.APIGroups, apiGroup) && slices.Contains(rule.Resources, resource) {
			rules = append(rules, rule)
		}
	}
	return rules
}

func inventoryRuleFor(role *rbacv1.ClusterRole, apiGroup, resource string) rbacv1.PolicyRule {
	rules := inventoryRulesFor(role, apiGroup, resource)
	Expect(rules).To(HaveLen(1), "expected one inventory rule for %s/%s", apiGroup, resource)
	return rules[0]
}

var _ = Describe("API Frontend inventory RBAC (#623)", func() {
	It("projects only Gateway target-resource metadata reads into a separate role", func() {
		kn := testKubernaut()
		role := APIFrontendInventoryReadClusterRole(kn, nil)

		Expect(role).NotTo(BeNil())
		Expect(role.Name).To(Equal(clusterRoleName(kn, "apifrontend-inventory-read")))

		for _, want := range []struct {
			apiGroup string
			resource string
		}{
			{"", "namespaces"},
			{"", "nodes"},
			{"", "pods"},
			{"", "services"},
			{"", "persistentvolumes"},
			{"", "persistentvolumeclaims"},
			{"apps", "deployments"},
			{"apps", "replicasets"},
			{"apps", "statefulsets"},
			{"apps", "daemonsets"},
			{"batch", "jobs"},
			{"batch", "cronjobs"},
			{"autoscaling", "horizontalpodautoscalers"},
			{"policy", "poddisruptionbudgets"},
			{"networking.k8s.io", "ingresses"},
		} {
			rule := inventoryRuleFor(role, want.apiGroup, want.resource)
			Expect(rule.Verbs).To(ConsistOf("get", "list"), "%s/%s must be metadata-read only", want.apiGroup, want.resource)
		}

		Expect(inventoryRulesFor(role, "kubernaut.ai", "remediationrequests")).To(BeEmpty(),
			"Gateway remediation-request access must not become inventory access")
		Expect(inventoryRulesFor(role, "coordination.k8s.io", "leases")).To(BeEmpty(),
			"Gateway lease access must not become inventory access")
		Expect(inventoryRulesFor(role, "authentication.k8s.io", "tokenreviews")).To(BeEmpty(),
			"Gateway TokenReview access must not become inventory access")
		Expect(inventoryRulesFor(role, "authorization.k8s.io", "subjectaccessreviews")).To(BeEmpty(),
			"Gateway SAR access must not become inventory access")
	})

	It("preserves explicit additional-role reads, wildcards, dynamic resources, and resource names", func() {
		kn := testKubernaut()
		additional := []rbacv1.ClusterRole{{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-reader-like"},
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{"widgets.example.io"}, Resources: []string{"widgets"}, Verbs: []string{"get", "list", "watch", "create"}},
				{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"*"}},
				{APIGroups: []string{"widgets.example.io"}, Resources: []string{"widgets/status"}, Verbs: []string{"get", "list"}},
				{NonResourceURLs: []string{"/metrics"}, Verbs: []string{"get"}},
				{APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{"bootstrap"}, Verbs: []string{"get", "list"}},
			},
		}}

		role := APIFrontendInventoryReadClusterRole(kn, additional)

		Expect(inventoryRuleFor(role, "widgets.example.io", "widgets").Verbs).To(ConsistOf("get", "list"))
		Expect(inventoryRuleFor(role, "*", "*").Verbs).To(ConsistOf("get", "list"),
			"a wildcard grant must remain wildcard while writes are removed")
		Expect(inventoryRuleFor(role, "", "secrets")).To(Equal(rbacv1.PolicyRule{
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			ResourceNames: []string{"bootstrap"},
			Verbs:         []string{"get", "list"},
		}), "explicit administrator-approved sensitive metadata reads must not be narrowed")
		Expect(inventoryRulesFor(role, "widgets.example.io", "widgets/status")).To(BeEmpty(),
			"subresources must not be projected")
		for _, rule := range role.Rules {
			Expect(rule.NonResourceURLs).To(BeEmpty(), "non-resource URLs must not be projected")
		}
	})

	It("normalizes source order and duplicate rules deterministically", func() {
		kn := testKubernaut()
		first := rbacv1.ClusterRole{Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"example.io"}, Resources: []string{"widgets"}, Verbs: []string{"list", "get"},
		}}}
		second := rbacv1.ClusterRole{Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"example.io"}, Resources: []string{"widgets"}, Verbs: []string{"get"},
		}}}

		forward := APIFrontendInventoryReadClusterRole(kn, []rbacv1.ClusterRole{first, second})
		reverse := APIFrontendInventoryReadClusterRole(kn, []rbacv1.ClusterRole{second, first})

		Expect(reverse.Rules).To(Equal(forward.Rules))
	})

	It("deduplicates readable fields and drops rules without get or list", func() {
		kn := testKubernaut()
		additional := []rbacv1.ClusterRole{{
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{"example.io"}, Resources: []string{"widgets", "widgets"}, Verbs: []string{"get", "get"}},
				{APIGroups: []string{"example.io"}, Resources: []string{"watch-only"}, Verbs: []string{"watch"}},
			},
		}}

		role := APIFrontendInventoryReadClusterRole(kn, additional)

		Expect(inventoryRuleFor(role, "example.io", "widgets")).To(Equal(rbacv1.PolicyRule{
			APIGroups: []string{"example.io"},
			Resources: []string{"widgets"},
			Verbs:     []string{"get"},
		}))
		Expect(inventoryRulesFor(role, "example.io", "watch-only")).To(BeEmpty())
	})

	It("binds only the API Frontend ServiceAccount to the projection", func() {
		kn := testKubernaut()
		binding := APIFrontendInventoryReadClusterRoleBinding(kn)

		Expect(binding).NotTo(BeNil())
		Expect(binding.Name).To(Equal(clusterRoleName(kn, "apifrontend-inventory-read-binding")))
		Expect(binding.RoleRef).To(Equal(rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRoleName(kn, "apifrontend-inventory-read"),
		}))
		Expect(binding.Subjects).To(ConsistOf(rbacv1.Subject{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      ServiceAccountName(ComponentAPIFrontend),
			Namespace: kn.Namespace,
		}))
	})

	It("does not create inventory RBAC when API Frontend is disabled", func() {
		kn := testKubernaut()
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled

		Expect(APIFrontendInventoryReadClusterRole(kn, nil)).To(BeNil())
		Expect(APIFrontendInventoryReadClusterRoleBinding(kn)).To(BeNil())
	})

	It("keeps AF inventory RBAC independent from Gateway deployment", func() {
		kn := testKubernaut()
		disabled := false
		kn.Spec.Gateway.Enabled = &disabled

		Expect(APIFrontendInventoryReadClusterRole(kn, nil)).NotTo(BeNil())
		Expect(APIFrontendInventoryReadClusterRoleBinding(kn)).NotTo(BeNil())
	})
})

var _ = Describe("API Frontend inventory RBAC source aggregation (#623)", func() {
	It("uses effective rules supplied by an aggregated additional ClusterRole", func() {
		kn := testKubernaut()
		aggregated := []rbacv1.ClusterRole{{
			ObjectMeta: metav1.ObjectMeta{Name: "aggregated-reader"},
			AggregationRule: &rbacv1.AggregationRule{
				ClusterRoleSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"rbac.example.io/aggregate": "true"}}},
			},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{"apps.example.io"}, Resources: []string{"widgets"}, Verbs: []string{"get", "list"},
			}},
		}}

		role := APIFrontendInventoryReadClusterRole(kn, aggregated)
		Expect(inventoryRulesFor(role, "apps.example.io", "widgets")).To(HaveLen(1))
	})
})

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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// Issue #662 / DD-662: the console read capability model is permanent.
//
// The owner decision (2026-11-23) ratifies DD-636's interim state as the
// long-term contract:
//
//   - no built-in persona carries the console REST read capabilities; the
//     coarse console-reader ClusterRole IS the model, not a placeholder;
//   - the operator never binds console-reader implicitly -- not through the
//     persona path, not through the derived console-access path;
//   - the only supported bind path is an explicit administrator choice
//     (roleBindings[].clusterRoleName or an external ClusterRoleBinding);
//   - the Console nginx /api/v1/console/ proxy is transport only (#660):
//     capability authorization happens entirely at AF via the named
//     capability SARs, so the proxy surface works unchanged whether or not
//     any group is bound.
//
// These tests pin the operator side of that contract so a future change
// cannot silently couple login, persona, or console-access grants to console
// reads.
var _ = Describe("Issue #662: console read capability model (DD-662)", func() {
	knWithPersonaBindings := func() *kubernautv1alpha2.Kubernaut {
		kn := testKubernautWithAF()
		kn.Namespace = testSystemNamespace
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha2.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-group"}},
				{Role: "ai-orchestrator", Groups: []string{"ai-group"}},
				{Role: "cicd", Groups: []string{"cicd-group"}},
				{Role: "observability", Groups: []string{"obs-group"}},
				{Role: "l3-audit", Groups: []string{"audit-group"}},
				{Role: "remediation-approver", Groups: []string{"approver-group"}},
			},
		}
		return kn
	}

	Describe("no implicit console-reader binding", func() {
		It("never references console-reader from the persona or console-access binding paths", func() {
			kn := knWithPersonaBindings()

			By("persona tool CRBs reference only persona roles")
			crbs := ToolClusterRoleBindings(kn)
			Expect(crbs).NotTo(BeEmpty())
			for _, crb := range crbs {
				Expect(crb.RoleRef.Name).NotTo(ContainSubstring("console-reader"),
					"persona binding %q must not reference any console-reader role", crb.Name)
			}

			By("the derived console-access CRB references only the console gate")
			// consoleAccessGroups unset: derived as the union of all bound
			// groups, i.e. every group above can log in -- and must still not
			// gain console reads.
			gateCRB := ConsoleAccessClusterRoleBinding(kn)
			Expect(gateCRB).NotTo(BeNil())
			Expect(gateCRB.RoleRef.Name).To(Equal(ConsoleAccessClusterRoleName(kn)),
				"console login must never imply console reads (DD-636 decision 1, ratified by DD-662)")
		})

		It("keeps console-reader unbound when no roleBindings exist at all", func() {
			kn := testKubernautWithAF()
			Expect(ToolClusterRoleBindings(kn)).To(BeEmpty())
			Expect(ConsoleAccessClusterRoleBinding(kn)).To(BeNil(),
				"zero-config installs must render no console-access CRB and no console-reader binding")
		})
	})

	Describe("the explicit administrator bind path", func() {
		It("binds console-reader only through an explicit clusterRoleName reference", func() {
			kn := testKubernautWithAF()
			kn.Namespace = testSystemNamespace
			kn.Spec.APIFrontend.RBAC = &kubernautv1alpha2.APIFrontendRBACSpec{
				RoleBindings: []kubernautv1alpha2.ToolRoleBinding{
					{
						// The rendered role name is namespace-prefixed; the
						// custom path uses it verbatim (documented contract).
						ClusterRoleName: testSystemNamespace + "-console-reader",
						Groups:          []string{"console-readers"},
					},
				},
			}

			crbs := ToolClusterRoleBindings(kn)
			Expect(crbs).To(HaveLen(1))
			Expect(crbs[0].RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(crbs[0].RoleRef.Name).To(Equal(testSystemNamespace + "-console-reader"))
			Expect(crbs[0].Subjects).To(ConsistOf(rbacv1.Subject{Kind: "Group", Name: "console-readers"}))
		})
	})

	Describe("the rendered role stays the permanent coarse model", func() {
		It("keeps exactly one console-reader rule with the three capabilities and no widening verbs", func() {
			kn := testKubernautWithAF()
			role := ConsoleReaderClusterRole(kn)
			Expect(role).NotTo(BeNil())
			Expect(role.Rules).To(HaveLen(1),
				"console-reader stays one coarse rule; splitting or widening needs a new DD")
			rule := role.Rules[0]
			Expect(rule.Verbs).To(ConsistOf("use"))
			Expect(rule.Resources).To(ConsistOf("tools"))
			Expect(rule.ResourceNames).To(ConsistOf(
				"kubernaut_read_policy_scope",
				"kubernaut_list_managed_resources",
				"kubernaut_get_managed_resource",
			))
		})
	})
})

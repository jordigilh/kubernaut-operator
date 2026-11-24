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
)

// Issue #662 / DD-662: the console read capability model is permanent.
//
// The owner decision (2026-11-23) ratifies DD-636's interim state as the
// long-term contract:
//
//   - no built-in persona carries the console REST read capabilities; the
//     coarse console-reader ClusterRole IS the model, not a placeholder;
//   - the operator never binds console-reader implicitly;
//   - the Console nginx /api/v1/console/ proxy is transport only (#660):
//     capability authorization happens entirely at AF via the named
//     capability SARs, so the proxy surface works unchanged whether or not
//     any group is bound.
//
// #682/#684 strengthened the model to roles-only: the operator renders no
// user-facing ClusterRoleBinding at all, so the former explicit bind path
// (spec.apiFrontend.rbac.roleBindings[].clusterRoleName) is gone too and
// the only supported bind path is an administrator-owned external
// ClusterRoleBinding.
//
// These tests pin the operator side of that contract so a future change
// cannot silently couple login or console-access grants to console reads.
var _ = Describe("Issue #662: console read capability model (DD-662)", func() {
	Describe("no implicit console-reader binding", func() {
		It("renders no user-facing ClusterRoleBinding at all (roles-only model)", func() {
			kn := testKubernautWithAF()

			By("every operator-rendered CRB binds a ServiceAccount, never a Group or User")
			for _, crb := range ClusterRoleBindings(kn, testKnV2(kn)) {
				for _, subject := range crb.Subjects {
					Expect(subject.Kind).To(Equal("ServiceAccount"),
						"CRB %q must not bind a user-facing subject %q (%s): the operator renders roles only (#682/#684)",
						crb.Name, subject.Name, subject.Kind)
				}
			}
		})

		It("keeps console-reader unbound in a zero-config install", func() {
			kn := testKubernautWithAF()
			Expect(kn.Spec.APIFrontend.RBAC).To(BeNil(),
				"precondition: the roles-only CRD has no binding configuration to set")
			for _, crb := range ClusterRoleBindings(kn, testKnV2(kn)) {
				Expect(crb.RoleRef.Name).NotTo(ContainSubstring("console-reader"),
					"no operator CRB may reference console-reader; binding it is an administrator decision (DD-662, #682)")
			}
		})

		It("renders the console-reader role even though nothing binds it", func() {
			kn := testKubernautWithAF()
			Expect(ConsoleReaderClusterRole(kn)).NotTo(BeNil(),
				"the coarse role must exist for an administrator to bind externally")
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

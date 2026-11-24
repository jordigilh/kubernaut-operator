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
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// Issue #636: audit of console direct-action roles and permissions.
//
// Every console direct action is authorized by AF through a Kubernetes
// SubjectAccessReview against a synthetic kubernaut.ai resource. This file
// pins the audited contract:
//
//   - login (kubernaut.ai/console, verb use) is a separate, opt-in gate that
//     grants no action by itself;
//   - every tool action (investigation, approval, workflow catalog read,
//     remediation, observation) is a named kubernaut.ai/tools "use" grant;
//   - the console REST read projection capabilities
//     (kubernaut_read_policy_scope, kubernaut_list_managed_resources,
//     kubernaut_get_managed_resource, kubernaut#2584 / DD-AF-018) are the same
//     kubernaut.ai/tools resource with a capability resourceName and are
//     granted to no built-in persona; the dedicated console-reader role makes
//     them bindable without inventing a persona mapping.
//
// Since #682/#684 the operator renders roles only: the six former built-in
// persona ClusterRoles are gone and their audited tool sets live on as the
// documented example use-case bundles in the installation guide. The
// persona-matrix tests below became documentation drift guards over those
// bundles, and gained the #683 documentation rules: alert-capable bundles
// carry both alert tool names (list_alerts on A2A, kubernaut_list_alerts on
// MCP, #664), and approval reads stay with the human/approver bundles
// (#665 resolution, DD-278 separation of duties).
var _ = Describe("Issue #636: console direct-action RBAC audit", func() {
	consoleCapabilities := []string{
		"kubernaut_read_policy_scope",
		"kubernaut_list_managed_resources",
		"kubernaut_get_managed_resource",
	}

	findRole := func(roles []*rbacv1.ClusterRole, suffix string) *rbacv1.ClusterRole {
		for _, r := range roles {
			if strings.HasSuffix(r.Name, suffix) {
				return r
			}
		}
		return nil
	}

	// documentedBundles parses the example use-case bundles from the
	// installation guide (delimited by bundle-matrix markers) into real
	// ClusterRoles, so the assertions below check exactly what an
	// administrator copying the documentation would deploy.
	documentedBundles := func() []*rbacv1.ClusterRole {
		// #nosec G304 -- fixed repo-relative documentation path, not user input.
		path := filepath.Join("..", "..", "docs", "installation", "02-configure-services.md")
		raw, err := os.ReadFile(path) //nolint:gosec // fixed repo-relative documentation path, not user input
		Expect(err).NotTo(HaveOccurred(), "installation guide must exist at %s", path)
		content := string(raw)
		start := strings.Index(content, "<!-- bundle-matrix-start -->")
		end := strings.Index(content, "<!-- bundle-matrix-end -->")
		Expect(start).To(BeNumerically(">", -1), "bundle-matrix-start marker missing from the installation guide")
		Expect(end).To(BeNumerically(">", start), "bundle-matrix-end marker missing from the installation guide")

		block := content[start+len("<!-- bundle-matrix-start -->") : end]
		// Strip the surrounding fenced-code markers, if any.
		block = strings.TrimPrefix(block, "\n```yaml")
		block = strings.TrimSuffix(block, "```\n")

		var bundles []*rbacv1.ClusterRole
		for _, doc := range strings.Split(block, "\n---\n") {
			if strings.TrimSpace(doc) == "" {
				continue
			}
			var bundle rbacv1.ClusterRole
			err := yaml.Unmarshal([]byte(doc), &bundle)
			Expect(err).NotTo(HaveOccurred(), "each documented bundle must be a valid YAML ClusterRole")
			bundles = append(bundles, &bundle)
		}
		Expect(bundles).NotTo(BeEmpty(), "the installation guide must document at least one bundle")
		return bundles
	}

	Describe("documented example use-case bundles", func() {
		// Golden lists pin the complete audited bundle-to-tool matrix, not
		// just membership of the names added here: a future upstream rename or
		// accidental swap must fail loudly instead of silently drifting.
		// #683: the alert-capable bundles (sre, ai-orchestrator) carry both
		// the A2A name (list_alerts) and the MCP alias
		// (kubernaut_list_alerts), because AF authorizes the exact tool name
		// each transport uses (#664).
		goldenBundleTools := map[string][]string{
			"kubernaut-sre": {
				"kubernaut_list_remediations", "kubernaut_get_remediation",
				"kubernaut_approve", "kubernaut_cancel_remediation", "kubernaut_watch",
				"kubernaut_await_session", "kubernaut_investigate",
				"kubernaut_discover_workflows", "kubernaut_select_workflow",
				"kubernaut_present_decision", "kubernaut_list_workflows",
				"kubernaut_get_remediation_history", "kubernaut_get_effectiveness",
				"kubernaut_get_audit_trail", "kubernaut_message", "kubernaut_complete",
				"kubernaut_cancel", "kubernaut_status", "kubernaut_reconnect",
				"kubectl_get", "kubectl_list", "kubectl_list_events",
				"kubernaut_check_existing_remediation", "kubernaut_remediate",
				"list_alerts", "kubernaut_list_alerts", "get_alert_details", "kubernaut_investigate_alert",
				"kubernaut_get_approval_request", "kubernaut_complete_no_action",
				// #636: approval visibility (DD-278 amendment) and read-only
				// fleet discovery.
				"kubernaut_list_approval_requests", "list_clusters",
			},
			"kubernaut-ai-orchestrator": {
				"kubernaut_list_remediations", "kubernaut_get_remediation", "kubernaut_watch",
				"kubernaut_await_session", "kubernaut_investigate",
				"kubernaut_discover_workflows", "kubernaut_select_workflow",
				"kubernaut_present_decision", "kubernaut_message", "kubernaut_complete",
				"kubernaut_cancel", "kubernaut_status", "kubernaut_reconnect",
				"kubectl_get", "kubectl_list", "kubectl_list_events",
				"kubernaut_check_existing_remediation", "kubernaut_remediate",
				"list_alerts", "kubernaut_list_alerts", "get_alert_details", "kubernaut_investigate_alert",
				// #636: read-only fleet discovery only. Approval reads remain
				// with the human bundles (DD-278 separation of duties).
				"list_clusters",
			},
			"kubernaut-cicd": {
				"kubernaut_list_remediations", "kubernaut_get_remediation",
				"kubernaut_watch", "kubernaut_await_session",
			},
			"kubernaut-observability": {
				"kubernaut_list_remediations", "kubernaut_get_remediation", "kubernaut_watch",
				"kubernaut_await_session", "kubernaut_get_effectiveness",
				"kubernaut_list_workflows",
			},
			"kubernaut-l3-audit": {
				"kubernaut_list_remediations", "kubernaut_get_remediation",
				"kubernaut_list_workflows", "kubernaut_get_remediation_history",
				"kubernaut_get_effectiveness", "kubernaut_get_audit_trail",
			},
			"kubernaut-remediation-approver": {
				"kubernaut_approve", "kubernaut_list_approval_requests",
				"kubernaut_get_approval_request",
				"kubernaut_list_remediations", "kubernaut_get_remediation",
				"kubernaut_watch", "kubernaut_await_session",
			},
		}

		It("documents exactly the audited tool set for every bundle", func() {
			bundles := documentedBundles()
			Expect(bundles).To(HaveLen(len(goldenBundleTools)))
			for name, want := range goldenBundleTools {
				bundle := findRole(bundles, name)
				Expect(bundle).NotTo(BeNil(), "bundle %q not documented", name)
				Expect(bundle.Rules).To(HaveLen(1))
				Expect(bundle.Rules[0].ResourceNames).To(ConsistOf(want),
					"bundle %q tool set drifted from the audited contract", name)
			}
		})

		It("documents every bundle as use-only named grants on kubernaut.ai/tools", func() {
			for _, bundle := range documentedBundles() {
				Expect(bundle.Rules).To(HaveLen(1), "bundle %q should have exactly one rule", bundle.Name)
				rule := bundle.Rules[0]
				Expect(rule.APIGroups).To(ConsistOf(kubernautAPIGroup), "bundle %q apiGroup", bundle.Name)
				Expect(rule.Resources).To(ConsistOf("tools"), "bundle %q resource", bundle.Name)
				Expect(rule.Verbs).To(ConsistOf("use"), "bundle %q verb", bundle.Name)
				Expect(rule.ResourceNames).NotTo(BeEmpty(), "bundle %q should name its tools", bundle.Name)
			}
		})

		It("never grants a console REST read capability to a documented bundle", func() {
			for _, bundle := range documentedBundles() {
				// Per-name negation: NotTo(ContainElements(list)) would only
				// fail on a bundle leaking ALL capabilities, silently passing
				// a single-capability leak.
				for _, capability := range consoleCapabilities {
					Expect(bundle.Rules[0].ResourceNames).NotTo(ContainElement(capability),
						"bundle %q must not implicitly grant console reads; use the console-reader role",
						bundle.Name)
				}
			}
		})

		It("keeps approval reads out of the automated ai-orchestrator bundle (DD-278, #665)", func() {
			bundle := findRole(documentedBundles(), "kubernaut-ai-orchestrator")
			Expect(bundle).NotTo(BeNil())
			approvalTools := []string{
				"kubernaut_approve", "kubernaut_list_approval_requests",
				"kubernaut_get_approval_request", "kubernaut_complete_no_action",
			}
			for _, tool := range approvalTools {
				Expect(bundle.Rules[0].ResourceNames).NotTo(ContainElement(tool),
					"approval decisions and approval reads stay with human/approver bundles (DD-278)")
			}
		})

		It("[#683] alert-capable bundles carry both the A2A and MCP alert tool names", func() {
			for _, name := range []string{"kubernaut-sre", "kubernaut-ai-orchestrator"} {
				bundle := findRole(documentedBundles(), name)
				Expect(bundle).NotTo(BeNil(), "alert-capable bundle %q not documented", name)
				for _, alertTool := range []string{"list_alerts", "kubernaut_list_alerts"} {
					Expect(bundle.Rules[0].ResourceNames).To(ContainElement(alertTool),
						"alert-capable bundle %q must document %q: AF authorizes the exact tool name each transport uses (#664/#683)",
						name, alertTool)
				}
			}
		})

		It("renders none of the documented bundles as operator ClusterRoles", func() {
			kn := testKubernautWithAF()
			rendered := ClusterRoles(kn, testKnV2(kn))
			for _, bundle := range documentedBundles() {
				for _, r := range rendered {
					Expect(r.Name).NotTo(Equal(bundle.Name),
						"documented bundle %q must stay admin-owned, never operator-rendered (#682/#684)", bundle.Name)
				}
			}
		})
	})

	Describe("console-access gate", func() {
		It("grants no direct action: only kubernaut.ai/console use", func() {
			role := ConsoleAccessClusterRole(testKubernautWithAF())
			Expect(role).NotTo(BeNil())
			Expect(role.Rules).To(HaveLen(1))
			Expect(role.Rules[0].APIGroups).To(ConsistOf(kubernautAPIGroup))
			Expect(role.Rules[0].Resources).To(ConsistOf("console"))
			Expect(role.Rules[0].Verbs).To(ConsistOf("use"))
			Expect(role.Rules[0].ResourceNames).To(BeEmpty(),
				"the console gate is coarse-grained and unnamed")
		})
	})

	Describe("ConsoleReaderClusterRole", func() {
		It("grants exactly the three console REST read capabilities", func() {
			kn := testKubernautWithAF()
			role := ConsoleReaderClusterRole(kn)
			Expect(role).NotTo(BeNil())
			Expect(role.Name).To(Equal(kn.Namespace + "-console-reader"))
			Expect(role.Namespace).To(BeEmpty(), "role is cluster-scoped by design")
			Expect(role.Rules).To(HaveLen(1))
			rule := role.Rules[0]
			Expect(rule.APIGroups).To(ConsistOf(kubernautAPIGroup))
			Expect(rule.Resources).To(ConsistOf("tools"))
			Expect(rule.Verbs).To(ConsistOf("use"))
			Expect(rule.ResourceNames).To(ConsistOf(consoleCapabilities))
		})

		It("returns nil when AF is disabled", func() {
			kn := testKubernautWithAF()
			disabled := false
			kn.Spec.APIFrontend.Enabled = &disabled
			Expect(ConsoleReaderClusterRole(kn)).To(BeNil())
		})

		It("is rendered by the ClusterRoles aggregator and prunes cleanly when AF is disabled", func() {
			kn := testKubernautWithAF()
			roles := ClusterRoles(kn, testKnV2(kn))
			names := make([]string, 0, len(roles))
			for _, role := range roles {
				Expect(role).NotTo(BeNil(), "the aggregator must never contain nil entries")
				names = append(names, role.Name)
			}
			Expect(names).To(ContainElement(kn.Namespace + "-console-reader"))

			disabled := false
			kn.Spec.APIFrontend.Enabled = &disabled
			for _, role := range ClusterRoles(kn, testKnV2(kn)) {
				Expect(role).NotTo(BeNil(), "AF-disabled aggregation must not contain nil entries")
				Expect(role.Name).NotTo(Equal(kn.Namespace+"-console-reader"),
					"AF-disabled aggregation must drop the console-reader role")
			}
		})
	})

	Describe("authorization boundary of the rendered console roles", func() {
		// ruleAllows mirrors Kubernetes RBAC matching for the subset of
		// attributes AF actually reviews: apiGroup, resource, optional
		// resourceName, verb. It deliberately reports false for anything the
		// rules do not name, proving deny-by-default.
		ruleAllows := func(rules []rbacv1.PolicyRule, group, resource, name, verb string) bool {
			for _, rule := range rules {
				if !slices.Contains(rule.APIGroups, group) && !slices.Contains(rule.APIGroups, "*") {
					continue
				}
				if !slices.Contains(rule.Resources, resource) && !slices.Contains(rule.Resources, "*") {
					continue
				}
				if !slices.Contains(rule.Verbs, verb) && !slices.Contains(rule.Verbs, "*") {
					continue
				}
				if len(rule.ResourceNames) == 0 {
					return true
				}
				if slices.Contains(rule.ResourceNames, name) {
					return true
				}
			}
			return false
		}

		It("allows each audited console read capability and denies everything else", func() {
			rules := ConsoleReaderClusterRole(testKubernautWithAF()).Rules

			for _, capability := range consoleCapabilities {
				Expect(ruleAllows(rules, kubernautAPIGroup, "tools", capability, "use")).
					To(BeTrue(), "capability %q must be allowed for the console-reader role", capability)
			}

			By("denying tool actions the role was not built for")
			Expect(ruleAllows(rules, kubernautAPIGroup, "tools", "kubernaut_approve", "use")).To(BeFalse())
			Expect(ruleAllows(rules, kubernautAPIGroup, "tools", "kubernaut_remediate", "use")).To(BeFalse())
			Expect(ruleAllows(rules, kubernautAPIGroup, "tools", "kubernaut_list_workflows", "use")).To(BeFalse())

			By("denying the console gate and non-use verbs")
			Expect(ruleAllows(rules, kubernautAPIGroup, "console", "", "use")).To(BeFalse())
			Expect(ruleAllows(rules, kubernautAPIGroup, "tools", "kubernaut_read_policy_scope", "get")).To(BeFalse())
			Expect(ruleAllows(rules, "authorization.k8s.io", "subjectaccessreviews", "", "create")).To(BeFalse())

			By("denying an unnamed tools request (capabilities require an exact name)")
			Expect(ruleAllows(rules, kubernautAPIGroup, "tools", "", "use")).To(BeFalse())
		})

		It("treats the console gate as authentication-only: it authorizes no tool", func() {
			rules := ConsoleAccessClusterRole(testKubernautWithAF()).Rules
			for _, capability := range consoleCapabilities {
				Expect(ruleAllows(rules, kubernautAPIGroup, "tools", capability, "use")).To(BeFalse())
			}
			Expect(ruleAllows(rules, kubernautAPIGroup, "tools", "kubernaut_approve", "use")).To(BeFalse())
			Expect(ruleAllows(rules, kubernautAPIGroup, "console", "", "use")).To(BeTrue())
		})

		It("denies actions to a bundle that holds only the console gate and vice versa", func() {
			By("a console-reader grant does not satisfy the console gate")
			readerRules := ConsoleReaderClusterRole(testKubernautWithAF()).Rules
			Expect(ruleAllows(readerRules, kubernautAPIGroup, "console", "", "use")).To(BeFalse())

			By("a documented tool bundle grant does not satisfy the console gate")
			sreRules := findRole(documentedBundles(), "kubernaut-sre").Rules
			Expect(ruleAllows(sreRules, kubernautAPIGroup, "console", "", "use")).To(BeFalse())
		})
	})
})

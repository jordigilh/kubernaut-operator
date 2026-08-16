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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/utils/ptr"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	testWorkflowRunnerSAName = "kubernaut-workflow-runner"
	testCustomWorkflowNS     = "my-wf-ns"
	testClusterRoleKind      = "ClusterRole"
	kubernautAPIGroup        = "kubernaut.ai"
)

var _ = Describe("ClusterRoles", func() {
	It("returns exactly 17 roles (14 base + 2 monitoring + 1 console-access)", func() {
		kn := testKubernaut()
		roles := ClusterRoles(kn, testKnV2(kn))
		Expect(roles).To(HaveLen(17), "ClusterRoles() should return exactly 17 roles (14 base + 2 monitoring + 1 console-access, #289), got %d", len(roles))
	})

	It("contains expected role names", func() {
		kn := testKubernaut()
		roles := ClusterRoles(kn, testKnV2(kn))
		names := make(map[string]bool, len(roles))
		for _, r := range roles {
			names[r.Name] = true
		}

		ns := kn.Namespace
		expectedNames := []string{
			ns + "-gateway-role",
			ns + "-aianalysis-controller",
			ns + "-kubernaut-agent-client",
			ns + "-kubernaut-agent-investigator",
			ns + "-signalprocessing-controller",
			ns + "-remediationorchestrator-controller",
			ns + "-workflowexecution-controller",
			ns + "-workflow-runner",
			ns + "-effectivenessmonitor-controller",
			ns + "-notification-controller",
			ns + "-data-storage-auth-middleware",
			ns + "-data-storage-client",
			ns + "-authwebhook-role",
			ns + "-alertmanager-view",
			ns + "-gateway-signal-source",
		}

		for _, name := range expectedNames {
			Expect(names[name]).To(BeTrue(), "ClusterRoles() missing expected role %q", name)
		}
	})

	It("include common managed-by labels", func() {
		kn := testKubernaut()
		for _, cr := range ClusterRoles(kn, testKnV2(kn)) {
			Expect(cr.Labels["app.kubernetes.io/managed-by"]).To(Equal(testOperatorManagedByValue),
				"ClusterRole %q missing managed-by label", cr.Name)
		}
	})

	Describe("GatewayClusterRole", func() {
		It("has PVC and HPA rules", func() {
			kn := testKubernaut()
			roles := ClusterRoles(kn, testKnV2(kn))
			var gw *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == kn.Namespace+"-gateway-role" {
					gw = r
					break
				}
			}
			Expect(gw).NotTo(BeNil(), "gateway-role ClusterRole not found")

			type resourceCheck struct {
				apiGroup string
				resource string
			}
			want := []resourceCheck{
				{"", "persistentvolumeclaims"},
				{"autoscaling", "horizontalpodautoscalers"},
			}

			for _, w := range want {
				found := false
				for _, rule := range gw.Rules {
					for _, g := range rule.APIGroups {
						if g != w.apiGroup {
							continue
						}
						for _, r := range rule.Resources {
							if r == w.resource {
								found = true
							}
						}
					}
				}
				Expect(found).To(BeTrue(), "gateway-role missing %s/%s", w.apiGroup, w.resource)
			}
		})

		// #277: ownerChainResolutionRules() shrunk to only genuinely
		// universal, non-ecosystem-specific kinds (PDB + networking.k8s.io).
		// OLM/Istio/cert-manager/ArgoCD/Routes/KubeVirt are no longer
		// unconditionally granted -- operators needing that ecosystem
		// coverage supply their own ClusterRole via
		// spec.additionalClusterRoles (least-privilege, SC-7/AC-6).
		It("has only the universal owner-chain resolution rules, not ecosystem-specific ones", func() {
			kn := testKubernaut()
			roles := ClusterRoles(kn, testKnV2(kn))
			var gw *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == kn.Namespace+"-gateway-role" {
					gw = r
					break
				}
			}
			Expect(gw).NotTo(BeNil(), "gateway-role ClusterRole not found")

			foundGroups := make(map[string]bool)
			for _, rule := range gw.Rules {
				for _, g := range rule.APIGroups {
					foundGroups[g] = true
				}
			}

			wantGroups := []string{"policy", "networking.k8s.io"}
			for _, g := range wantGroups {
				Expect(foundGroups[g]).To(BeTrue(), "gateway-role missing universal owner-chain API group %q", g)
			}

			droppedGroups := []string{
				"operators.coreos.com", "packages.operators.coreos.com",
				"security.istio.io", "networking.istio.io",
				"cert-manager.io", "argoproj.io", "route.openshift.io",
				"kubevirt.io", "cdi.kubevirt.io",
			}
			for _, g := range droppedGroups {
				Expect(foundGroups[g]).To(BeFalse(),
					"gateway-role should no longer unconditionally grant ecosystem-specific API group %q (#277 least-privilege shrink)", g)
			}
		})
	})

	Describe("KubernautAgent investigator", func() {
		It("has service mesh and GitOps rules", func() {
			kn := testKubernaut()
			roles := ClusterRoles(kn, testKnV2(kn))
			var investigator *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == kn.Namespace+"-kubernaut-agent-investigator" {
					investigator = r
					break
				}
			}
			Expect(investigator).NotTo(BeNil(), "kubernaut-agent-investigator ClusterRole not found")

			wantGroups := []string{
				"cert-manager.io",
				"argoproj.io",
				"policy.linkerd.io",
				"security.istio.io",
				"networking.istio.io",
				kubernautAPIGroup,
				"kubevirt.io",
				"cdi.kubevirt.io",
			}
			foundGroups := make(map[string]bool)
			for _, rule := range investigator.Rules {
				for _, g := range rule.APIGroups {
					foundGroups[g] = true
				}
			}
			for _, g := range wantGroups {
				Expect(foundGroups[g]).To(BeTrue(), "kubernaut-agent-investigator missing API group %q", g)
			}
		})

		It("has OCP and core Kubernetes rules", func() {
			kn := testKubernaut()
			roles := ClusterRoles(kn, testKnV2(kn))
			var investigator *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == kn.Namespace+"-kubernaut-agent-investigator" {
					investigator = r
					break
				}
			}
			Expect(investigator).NotTo(BeNil(), "kubernaut-agent-investigator ClusterRole not found")

			wantGroups := []string{
				"operators.coreos.com",
				"packages.operators.coreos.com",
				"route.openshift.io",
				"apps.openshift.io",
				"security.openshift.io",
				"image.openshift.io",
				"build.openshift.io",
				"machine.openshift.io",
				"machineconfiguration.openshift.io",
				"quota.openshift.io",
				"network.operator.openshift.io",
				"rbac.authorization.k8s.io",
				"admissionregistration.k8s.io",
				"apiextensions.k8s.io",
				"scheduling.k8s.io",
			}
			foundGroups := make(map[string]bool)
			for _, rule := range investigator.Rules {
				for _, g := range rule.APIGroups {
					foundGroups[g] = true
				}
			}
			for _, g := range wantGroups {
				Expect(foundGroups[g]).To(BeTrue(), "kubernaut-agent-investigator missing API group %q", g)
			}
		})

		It("has events with create and patch verbs", func() {
			kn := testKubernaut()
			roles := ClusterRoles(kn, testKnV2(kn))
			var investigator *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == kn.Namespace+"-kubernaut-agent-investigator" {
					investigator = r
					break
				}
			}
			Expect(investigator).NotTo(BeNil())

			found := false
			for _, rule := range investigator.Rules {
				if len(rule.APIGroups) > 0 && rule.APIGroups[0] == "" {
					for _, res := range rule.Resources {
						if res == "events" {
							Expect(rule.Verbs).To(ContainElements("get", "list", "watch", "create", "patch"))
							found = true
						}
					}
				}
			}
			Expect(found).To(BeTrue(), "kubernaut-agent-investigator should have events with create/patch verbs")
		})

		It("has get permission on nodes/proxy for kubelet proxy API access", func() {
			kn := testKubernaut()
			roles := ClusterRoles(kn, testKnV2(kn))
			var investigator *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == kn.Namespace+"-kubernaut-agent-investigator" {
					investigator = r
					break
				}
			}
			Expect(investigator).NotTo(BeNil())

			found := false
			for _, rule := range investigator.Rules {
				if len(rule.APIGroups) > 0 && rule.APIGroups[0] == "" {
					for _, res := range rule.Resources {
						if res == "nodes/proxy" {
							Expect(rule.Verbs).To(ContainElement("get"))
							found = true
						}
					}
				}
			}
			Expect(found).To(BeTrue(), "kubernaut-agent-investigator should have get permission on nodes/proxy for nodes_log and nodes_stats_summary tools (#205)")
		})
	})

	Describe("Workflow runner", func() {
		It("has mesh and GitOps rules", func() {
			kn := testKubernaut()
			roles := ClusterRoles(kn, testKnV2(kn))
			var runner *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == kn.Namespace+"-workflow-runner" {
					runner = r
					break
				}
			}
			Expect(runner).NotTo(BeNil(), "workflow-runner ClusterRole not found")

			wantGroups := []string{
				"argoproj.io",
				"cert-manager.io",
				"policy.linkerd.io",
				"security.istio.io",
				"networking.istio.io",
			}
			foundGroups := make(map[string]bool)
			for _, rule := range runner.Rules {
				for _, g := range rule.APIGroups {
					foundGroups[g] = true
				}
			}
			for _, g := range wantGroups {
				Expect(foundGroups[g]).To(BeTrue(), "workflow-runner missing API group %q", g)
			}
		})
	})

	Describe("EffectivenessMonitor controller", func() {
		// #277: see the matching Gateway test above for rationale.
		It("has only the universal owner-chain resolution rules, not ecosystem-specific ones", func() {
			kn := testKubernaut()
			roles := ClusterRoles(kn, testKnV2(kn))
			var em *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == kn.Namespace+"-effectivenessmonitor-controller" {
					em = r
					break
				}
			}
			Expect(em).NotTo(BeNil(), "effectivenessmonitor-controller ClusterRole not found")

			foundGroups := make(map[string]bool)
			for _, rule := range em.Rules {
				for _, g := range rule.APIGroups {
					foundGroups[g] = true
				}
			}

			wantGroups := []string{"policy", "networking.k8s.io"}
			for _, g := range wantGroups {
				Expect(foundGroups[g]).To(BeTrue(), "effectivenessmonitor-controller missing universal owner-chain API group %q", g)
			}

			droppedGroups := []string{
				"operators.coreos.com", "packages.operators.coreos.com",
				"security.istio.io", "networking.istio.io",
				"cert-manager.io", "argoproj.io", "route.openshift.io",
				"kubevirt.io", "cdi.kubevirt.io",
			}
			for _, g := range droppedGroups {
				Expect(foundGroups[g]).To(BeFalse(),
					"effectivenessmonitor-controller should no longer unconditionally grant ecosystem-specific API group %q (#277 least-privilege shrink)", g)
			}
		})
	})

	Describe("Data storage client", func() {
		It("has expanded verbs", func() {
			kn := testKubernaut()
			roles := ClusterRoles(kn, testKnV2(kn))
			var dsClient *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == kn.Namespace+"-data-storage-client" {
					dsClient = r
					break
				}
			}
			Expect(dsClient).NotTo(BeNil(), "data-storage-client ClusterRole not found")
			Expect(dsClient.Rules).NotTo(BeEmpty(), "data-storage-client has no rules")
			rule := dsClient.Rules[0]
			wantVerbs := []string{"create", "get", "list", "update", "delete"}
			verbs := make(map[string]bool)
			for _, v := range rule.Verbs {
				verbs[v] = true
			}
			for _, v := range wantVerbs {
				Expect(verbs[v]).To(BeTrue(), "data-storage-client missing verb %q", v)
			}
		})
	})
})

var _ = Describe("ClusterRoleBindings", func() {
	It("restricts subject namespaces", func() {
		kn := testKubernaut()
		crbs := ClusterRoleBindings(kn, testKnV2(kn))

		allowedNamespaces := map[string]bool{
			kn.Namespace:             true,
			DefaultWorkflowNamespace: true,
			OCPMonitoringNamespace:   true,
		}

		for _, crb := range crbs {
			Expect(crb.Subjects).NotTo(BeEmpty(), "CRB %q has no subjects", crb.Name)
			for _, subj := range crb.Subjects {
				Expect(allowedNamespaces[subj.Namespace]).To(BeTrue(),
					"CRB %q subject %q has namespace %q, want one of %v",
					crb.Name, subj.Name, subj.Namespace, allowedNamespaces)
			}
		}
	})

	It("binds workflow runner to the workflow namespace", func() {
		kn := testKubernaut()
		crbs := ClusterRoleBindings(kn, testKnV2(kn))
		ns := kn.Namespace

		wantName := ns + "-workflow-runner-binding"
		wantRef := ns + "-workflow-runner"

		found := false
		for _, crb := range crbs {
			if crb.Name == wantName {
				found = true
				Expect(crb.RoleRef.Name).To(Equal(wantRef),
					"workflow-runner CRB roleRef = %q, want %q", crb.RoleRef.Name, wantRef)
				Expect(crb.Subjects).NotTo(BeEmpty(), "workflow-runner CRB has no subjects")
				Expect(crb.Subjects[0].Name).To(Equal(testWorkflowRunnerSAName),
					"workflow-runner CRB subject = %q, want %q", crb.Subjects[0].Name, testWorkflowRunnerSAName)
				Expect(crb.Subjects[0].Namespace).To(Equal(DefaultWorkflowNamespace),
					"workflow-runner CRB subject namespace = %q, want %q", crb.Subjects[0].Namespace, DefaultWorkflowNamespace)
			}
		}
		Expect(found).To(BeTrue(), "ClusterRoleBindings() should include %q", wantName)
	})

	Context("when monitoring is enabled", func() {
		var crbMap map[string]*rbacv1.ClusterRoleBinding
		var ns string

		BeforeEach(func() {
			kn := testKubernaut()
			crbs := ClusterRoleBindings(kn, testKnV2(kn))
			ns = kn.Namespace
			crbMap = make(map[string]*rbacv1.ClusterRoleBinding, len(crbs))
			for _, crb := range crbs {
				crbMap[crb.Name] = crb
			}
		})

		It("binds cluster-monitoring-view for effectiveness monitor", func() {
			crb, ok := crbMap[ns+"-effectivenessmonitor-monitoring-view"]
			Expect(ok).To(BeTrue(), "missing %s-effectivenessmonitor-monitoring-view CRB", ns)
			Expect(crb.RoleRef.Name).To(Equal("cluster-monitoring-view"),
				"roleRef = %q, want %q", crb.RoleRef.Name, "cluster-monitoring-view")
		})

		It("binds cluster-monitoring-view for kubernaut agent", func() {
			crb, ok := crbMap[ns+"-kubernaut-agent-monitoring-view"]
			Expect(ok).To(BeTrue(), "missing %s-kubernaut-agent-monitoring-view CRB", ns)
			Expect(crb.RoleRef.Name).To(Equal("cluster-monitoring-view"),
				"roleRef = %q, want %q", crb.RoleRef.Name, "cluster-monitoring-view")
		})

		It("binds alertmanager gateway signal source to the OCP SA", func() {
			crb, ok := crbMap[ns+"-alertmanager-gateway-signal-source"]
			Expect(ok).To(BeTrue(), "missing %s-alertmanager-gateway-signal-source CRB", ns)
			Expect(crb.RoleRef.Name).To(Equal(ns+"-gateway-signal-source"),
				"roleRef = %q, want %q", crb.RoleRef.Name, ns+"-gateway-signal-source")
			Expect(crb.Subjects).NotTo(BeEmpty(), "CRB has no subjects")
			subj := crb.Subjects[0]
			Expect(subj.Name).To(Equal(OCPAlertManagerSAName),
				"subject name = %q, want %q", subj.Name, OCPAlertManagerSAName)
			Expect(subj.Namespace).To(Equal(OCPMonitoringNamespace),
				"subject namespace = %q, want %q", subj.Namespace, OCPMonitoringNamespace)
		})

		It("binds cluster-monitoring-view for apifrontend", func() {
			crb, ok := crbMap[ns+"-apifrontend-monitoring-view"]
			Expect(ok).To(BeTrue(), "missing %s-apifrontend-monitoring-view CRB", ns)
			Expect(crb.RoleRef.Name).To(Equal("cluster-monitoring-view"),
				"roleRef = %q, want %q", crb.RoleRef.Name, "cluster-monitoring-view")
		})
	})

})

var _ = Describe("DataStorageClientRoleBindings", func() {
	It("returns eleven bindings", func() {
		kn := testKubernaut()
		rbs := DataStorageClientRoleBindings(kn)
		Expect(rbs).To(HaveLen(11), "DataStorageClientRoleBindings() should return 11, got %d", len(rbs))
	})

	It("all reference the data-storage-client ClusterRole", func() {
		kn := testKubernaut()
		rbs := DataStorageClientRoleBindings(kn)
		wantRef := kn.Namespace + "-data-storage-client"
		for _, rb := range rbs {
			Expect(rb.RoleRef.Name).To(Equal(wantRef),
				"RoleBinding %q should reference %q, got %q", rb.Name, wantRef, rb.RoleRef.Name)
			Expect(rb.RoleRef.Kind).To(Equal(testClusterRoleKind),
				"RoleBinding %q should reference kind ClusterRole, got %q", rb.Name, rb.RoleRef.Kind)
		}
	})
})

var _ = Describe("NamespaceRoles", func() {
	It("all grant secrets and configmaps access", func() {
		kn := testKubernaut()
		roles := NamespaceRoles(kn, testKnV2(kn))

		Expect(roles).To(HaveLen(11), "NamespaceRoles() should return 11, got %d", len(roles))

		for _, role := range roles {
			Expect(role.Rules).To(HaveLen(1), "Role %q should have exactly 1 rule, got %d", role.Name, len(role.Rules))
			rule := role.Rules[0]
			hasConfigmaps := false
			hasSecrets := false
			for _, r := range rule.Resources {
				if r == "configmaps" {
					hasConfigmaps = true
				}
				if r == "secrets" {
					hasSecrets = true
				}
			}
			Expect(hasConfigmaps && hasSecrets).To(BeTrue(), "Role %q should grant configmaps+secrets access", role.Name)
		}
	})
})

var _ = Describe("NamespaceRoleBindings", func() {
	It("match NamespaceRoles by name", func() {
		kn := testKubernaut()
		roles := NamespaceRoles(kn, testKnV2(kn))
		rbs := NamespaceRoleBindings(kn, testKnV2(kn))

		Expect(rbs).To(HaveLen(len(roles)),
			"NamespaceRoleBindings count %d != NamespaceRoles count %d", len(rbs), len(roles))

		roleNames := make(map[string]bool)
		for _, r := range roles {
			roleNames[r.Name] = true
		}
		for _, rb := range rbs {
			Expect(roleNames[rb.RoleRef.Name]).To(BeTrue(),
				"RoleBinding %q references non-existent role %q", rb.Name, rb.RoleRef.Name)
		}
	})
})

var _ = Describe("WorkflowNamespaceRBAC", func() {
	It("uses the default workflow namespace", func() {
		kn := testKubernaut()
		roles, rbs := WorkflowNamespaceRBAC(kn)

		for _, r := range roles {
			Expect(r.Namespace).To(Equal(DefaultWorkflowNamespace),
				"workflow Role %q should be in kubernaut-workflows, got %q", r.Name, r.Namespace)
		}
		for _, rb := range rbs {
			Expect(rb.Namespace).To(Equal(DefaultWorkflowNamespace),
				"workflow RoleBinding %q should be in kubernaut-workflows, got %q", rb.Name, rb.Namespace)
		}
	})

	It("uses a custom workflow namespace when set", func() {
		kn := testKubernaut()
		kn.Spec.WorkflowExecution.WorkflowNamespace = testCustomWorkflowNS
		roles, rbs := WorkflowNamespaceRBAC(kn)

		for _, r := range roles {
			Expect(r.Namespace).To(Equal(testCustomWorkflowNS),
				"workflow Role %q should be in my-wf-ns, got %q", r.Name, r.Namespace)
		}
		for _, rb := range rbs {
			Expect(rb.Namespace).To(Equal(testCustomWorkflowNS),
				"workflow RoleBinding %q should be in my-wf-ns, got %q", rb.Name, rb.Namespace)
		}
	})
})

var _ = Describe("AnsibleRBAC", func() {
	It("grants AWX jobs permission", func() {
		kn := testKubernaut()
		kn.Spec.Ansible.Enabled = true
		ns := kn.Namespace
		cr, crb := AnsibleRBAC(kn)

		wantCR := ns + "-workflowexecution-awx"
		Expect(cr.Name).To(Equal(wantCR), "AWX ClusterRole name = %q, want %q", cr.Name, wantCR)

		found := false
		for _, rule := range cr.Rules {
			for _, res := range rule.Resources {
				if res == "awxjobs" {
					found = true
				}
			}
		}
		Expect(found).To(BeTrue(), "AWX ClusterRole should grant access to awxjobs resources")

		Expect(crb.RoleRef.Name).To(Equal(wantCR), "AWX CRB roleRef = %q, want %q", crb.RoleRef.Name, wantCR)
	})
})

var _ = Describe("KubernautAgentClientRoleBinding", func() {
	It("grants AI analysis access", func() {
		kn := testKubernaut()
		rb := KubernautAgentClientRoleBinding(kn)

		Expect(rb.Name).To(Equal("kubernaut-agent-client-aianalysis"), "Name = %q, want %q", rb.Name, "kubernaut-agent-client-aianalysis")
		Expect(rb.Namespace).To(Equal(kn.Namespace), "Namespace = %q, want %q", rb.Namespace, kn.Namespace)
		wantRoleRef := kn.Namespace + "-kubernaut-agent-client"
		Expect(rb.RoleRef.Name).To(Equal(wantRoleRef), "RoleRef.Name = %q, want %q", rb.RoleRef.Name, wantRoleRef)
		Expect(rb.RoleRef.Kind).To(Equal(testClusterRoleKind), "RoleRef.Kind = %q, want ClusterRole", rb.RoleRef.Kind)
		Expect(rb.Subjects).NotTo(BeEmpty(), "RoleBinding has no subjects")
		subj := rb.Subjects[0]
		Expect(subj.Name).To(Equal(ServiceAccountName(ComponentAIAnalysis)),
			"Subject.Name = %q, want %q", subj.Name, ServiceAccountName(ComponentAIAnalysis))
		Expect(subj.Namespace).To(Equal(kn.Namespace),
			"Subject.Namespace = %q, want %q", subj.Namespace, kn.Namespace)
	})
})

var _ = Describe("KubernautAgentClientAPIfrontendRoleBinding", func() {
	It("grants apifrontend SA access to kubernaut-agent-client ClusterRole (#1287)", func() {
		kn := testKubernautWithAF()
		rb := KubernautAgentClientAPIfrontendRoleBinding(kn)

		Expect(rb.Name).To(Equal("kubernaut-agent-client-apifrontend"))
		Expect(rb.Namespace).To(Equal(kn.Namespace))
		wantRoleRef := kn.Namespace + "-kubernaut-agent-client"
		Expect(rb.RoleRef.Name).To(Equal(wantRoleRef))
		Expect(rb.RoleRef.Kind).To(Equal(testClusterRoleKind))
		Expect(rb.Subjects).NotTo(BeEmpty())
		subj := rb.Subjects[0]
		Expect(subj.Name).To(Equal(ServiceAccountName(ComponentAPIFrontend)))
		Expect(subj.Namespace).To(Equal(kn.Namespace))
	})
})

var _ = Describe("MonitoringCRBNames", func() {
	It("returns all five namespace-prefixed names", func() {
		kn := testKubernaut()
		names := MonitoringCRBNames(kn)
		Expect(names).To(HaveLen(5), "MonitoringCRBNames() count = %d, want 5", len(names))
		ns := kn.Namespace
		for _, name := range names {
			Expect(name).To(HavePrefix(ns),
				"MonitoringCRBNames entry %q should be namespace-prefixed with %q", name, ns)
		}
	})
})

var _ = Describe("MonitoringClusterRoleNames", func() {
	It("returns two namespace-prefixed names", func() {
		kn := testKubernaut()
		names := MonitoringClusterRoleNames(kn)
		Expect(names).To(HaveLen(2), "MonitoringClusterRoleNames() count = %d, want 2", len(names))
		ns := kn.Namespace
		for _, name := range names {
			Expect(name).To(HavePrefix(ns),
				"MonitoringClusterRoleNames entry %q should be namespace-prefixed with %q", name, ns)
		}
	})
})

// AdditionalComponentCRB/CRBName (#277): generalized from KA-only to also
// support Gateway and EffectivenessMonitor, since none of the three has a
// legitimate reason to see a different set of ecosystem CRDs than the
// others -- they all resolve the same owner-reference chains, driven by a
// single shared spec.additionalClusterRoles list (BR-PLATFORM-005: the
// operator only creates the binding, never the referenced ClusterRole,
// keeping least-privilege in the administrator's hands).
var _ = Describe("AdditionalComponentCRB", func() {
	Describe("AdditionalComponentCRBName", func() {
		It("returns the short form, scoped per component", func() {
			kn := testKubernaut()
			name := AdditionalComponentCRBName(kn, ComponentKubernautAgent, "my-kafka-reader")
			want := "kubernaut-system-kubernaut-agent-ext-my-kafka-reader"
			Expect(name).To(Equal(want), "got %q, want %q", name, want)
		})

		It("produces distinct names for the same role name across components", func() {
			kn := testKubernaut()
			kaName := AdditionalComponentCRBName(kn, ComponentKubernautAgent, "shared-role")
			gwName := AdditionalComponentCRBName(kn, ComponentGateway, "shared-role")
			emName := AdditionalComponentCRBName(kn, ComponentEffectivenessMonitor, "shared-role")

			Expect(kaName).NotTo(Equal(gwName), "KA and Gateway CRB names must differ for the same role name")
			Expect(kaName).NotTo(Equal(emName), "KA and EM CRB names must differ for the same role name")
			Expect(gwName).NotTo(Equal(emName), "Gateway and EM CRB names must differ for the same role name")
		})

		It("does not truncate when exactly at the limit", func() {
			kn := testKubernaut()
			prefix := kn.Namespace + "-" + ComponentKubernautAgent + "-ext-"
			roleName := strings.Repeat("a", maxK8sNameLen-len(prefix))
			name := AdditionalComponentCRBName(kn, ComponentKubernautAgent, roleName)
			Expect(name).To(HaveLen(maxK8sNameLen), "expected name length %d, got %d", maxK8sNameLen, len(name))
			Expect(name).To(Equal(prefix+roleName), "name should not be truncated when exactly at limit")
		})

		It("truncates long names", func() {
			kn := testKubernaut()
			prefix := kn.Namespace + "-" + ComponentKubernautAgent + "-ext-"
			roleName := strings.Repeat("x", maxK8sNameLen-len(prefix)+50)
			name := AdditionalComponentCRBName(kn, ComponentKubernautAgent, roleName)

			Expect(len(name)).To(BeNumerically("<=", maxK8sNameLen), "name exceeds 253 chars: len=%d", len(name)) //nolint:ginkgolinter // dynamic length bound
			Expect(strings.HasPrefix(name, prefix)).To(BeTrue(), "name should start with %q, got %q", prefix, name)
		})

		It("maps different long names to different hashes", func() {
			kn := testKubernaut()
			prefix := kn.Namespace + "-" + ComponentKubernautAgent + "-ext-"
			base := strings.Repeat("a", maxK8sNameLen-len(prefix)+10)

			name1 := AdditionalComponentCRBName(kn, ComponentKubernautAgent, base+"1")
			name2 := AdditionalComponentCRBName(kn, ComponentKubernautAgent, base+"2")
			Expect(name1).NotTo(Equal(name2), "different long role names should produce different CRB names")
		})

		It("is stable for the same role name", func() {
			kn := testKubernaut()
			name1 := AdditionalComponentCRBName(kn, ComponentKubernautAgent, "role-a")
			name2 := AdditionalComponentCRBName(kn, ComponentKubernautAgent, "role-b")

			name1Again := AdditionalComponentCRBName(kn, ComponentKubernautAgent, "role-a")
			name2Again := AdditionalComponentCRBName(kn, ComponentKubernautAgent, "role-b")

			Expect(name1 == name1Again && name2 == name2Again).To(BeTrue(),
				"reordering should not change CRB names (names are per-role, not positional)")
		})
	})

	DescribeTable("has the expected structure for each component",
		func(component string) {
			kn := testKubernaut()
			crName := "strimzi-kafka-reader"
			saName := ServiceAccountName(component)
			crb := AdditionalComponentCRB(kn, component, saName, crName)

			Expect(crb.RoleRef.Kind).To(Equal(testClusterRoleKind), "RoleRef.Kind = %q, want ClusterRole", crb.RoleRef.Kind)
			Expect(crb.RoleRef.Name).To(Equal(crName), "RoleRef.Name = %q, want %q", crb.RoleRef.Name, crName)
			Expect(crb.Subjects).To(HaveLen(1), "expected 1 subject, got %d", len(crb.Subjects))
			Expect(crb.Subjects[0].Name).To(Equal(saName), "subject SA = %q, want %q", crb.Subjects[0].Name, saName)
			Expect(crb.Subjects[0].Namespace).To(Equal(kn.Namespace),
				"subject namespace = %q, want %q", crb.Subjects[0].Namespace, kn.Namespace)
		},
		Entry("kubernaut-agent", ComponentKubernautAgent),
		Entry("gateway", ComponentGateway),
		Entry("effectivenessmonitor", ComponentEffectivenessMonitor),
	)

	It("sets expected labels", func() {
		kn := testKubernaut()
		crb := AdditionalComponentCRB(kn, ComponentKubernautAgent, ServiceAccountName(ComponentKubernautAgent), "test-role")

		Expect(crb.Labels["app.kubernetes.io/managed-by"]).To(Equal(testOperatorManagedByValue), "missing managed-by label")
		Expect(crb.Labels["app.kubernetes.io/instance"]).To(Equal(kn.Name), "missing instance label")
		Expect(crb.Labels[LabelAdditionalComponentRBAC]).To(Equal(LabelValueTrue), "missing additional-component-rbac label")
	})

	It("has no entries when the spec list is nil", func() {
		knV2 := testKnV2(testKubernaut())
		knV2.Spec.AdditionalClusterRoles = nil
		Expect(knV2.Spec.AdditionalClusterRoles).To(BeEmpty(), "empty additional list should have length 0")
	})
})

// --- SAR Tool Authorization Tests (issue #118) ---

var _ = Describe("ToolClusterRoles", func() {
	It("returns 6 roles when AF is enabled", func() {
		kn := testKubernautWithAF()
		roles := ToolClusterRoles(kn)
		Expect(roles).To(HaveLen(6), "ToolClusterRoles() should return exactly 6 persona-based tool roles")
	})

	It("returns empty when AF is disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		roles := ToolClusterRoles(kn)
		Expect(roles).To(BeEmpty(), "ToolClusterRoles() should return empty when AF is disabled")
	})

	It("each tool ClusterRole uses apiGroup kubernaut.ai, resource tools, verb use", func() {
		kn := testKubernautWithAF()
		for _, cr := range ToolClusterRoles(kn) {
			Expect(cr.Rules).To(HaveLen(1), "tool ClusterRole %q should have exactly 1 rule", cr.Name)
			rule := cr.Rules[0]
			Expect(rule.APIGroups).To(ConsistOf(kubernautAPIGroup), "tool ClusterRole %q apiGroup", cr.Name)
			Expect(rule.Resources).To(ConsistOf("tools"), "tool ClusterRole %q resource", cr.Name)
			Expect(rule.Verbs).To(ConsistOf("use"), "tool ClusterRole %q verb", cr.Name)
			Expect(rule.ResourceNames).NotTo(BeEmpty(), "tool ClusterRole %q should have resourceNames", cr.Name)
		}
	})

	DescribeTable("persona tool counts",
		func(suffix string, expectedCount int) {
			kn := testKubernautWithAF()
			roles := ToolClusterRoles(kn)
			var found *rbacv1.ClusterRole
			for _, r := range roles {
				if strings.HasSuffix(r.Name, suffix) {
					found = r
					break
				}
			}
			Expect(found).NotTo(BeNil(), "tool ClusterRole with suffix %q not found", suffix)
			Expect(found.Rules[0].ResourceNames).To(HaveLen(expectedCount),
				"tool ClusterRole %q should have %d resourceNames, got %d",
				found.Name, expectedCount, len(found.Rules[0].ResourceNames))
		},
		Entry("SRE", "tool-sre", 29),
		Entry("AI-orchestrator", "tool-ai-orchestrator", 21),
		Entry("CICD", "tool-cicd", 4),
		Entry("Observability", "tool-observability", 6),
		Entry("L3-audit", "tool-l3-audit", 6),
		Entry("Remediation-approver", "tool-remediation-approver", 7),
	)

	// #278: sre already holds kubernaut_approve (it's the only console-interactive
	// persona today), so it must also see kubernaut_get_approval_request to make an
	// informed decision, and kubernaut_complete_no_action to dismiss/escalate --
	// otherwise the approval gate silently breaks end-to-end for the persona it's
	// meant to serve. Asserted by name (not just count) so a future accidental swap
	// of a different tool doesn't silently satisfy the count-only check above.
	It("SRE persona includes approval-visibility and completion tools (#278)", func() {
		kn := testKubernautWithAF()
		roles := ToolClusterRoles(kn)
		var sre *rbacv1.ClusterRole
		for _, r := range roles {
			if strings.HasSuffix(r.Name, "tool-sre") {
				sre = r
				break
			}
		}
		Expect(sre).NotTo(BeNil(), "tool-sre ClusterRole not found")
		Expect(sre.Rules[0].ResourceNames).To(ContainElements(
			"kubernaut_get_approval_request", "kubernaut_complete_no_action",
		), "tool-sre should be able to view RAR details and complete-without-action (#278)")
	})

	It("tool ClusterRole names are namespace-prefixed", func() {
		kn := testKubernautWithAF()
		for _, cr := range ToolClusterRoles(kn) {
			Expect(cr.Name).To(HavePrefix(kn.Namespace+"-"),
				"tool ClusterRole %q should be namespace-prefixed", cr.Name)
		}
	})

	It("ToolClusterRoleNames returns all 6 names for finalizer cleanup", func() {
		kn := testKubernautWithAF()
		names := ToolClusterRoleNames(kn)
		Expect(names).To(HaveLen(6), "ToolClusterRoleNames() should return 6 names")
		for _, n := range names {
			Expect(n).To(HavePrefix(kn.Namespace+"-"),
				"ToolClusterRoleNames entry %q should be namespace-prefixed", n)
		}
	})
})

var _ = Describe("ToolClusterRoleBindings", func() {
	It("returns CRBs matching spec roleBindings", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-team"}},
				{Role: "cicd", Groups: []string{"ci-bots"}},
			},
		}
		crbs := ToolClusterRoleBindings(kn)
		Expect(crbs).To(HaveLen(2), "ToolClusterRoleBindings() should return 2 CRBs")
	})

	It("returns empty when no roleBindings specified", func() {
		kn := testKubernautWithAF()
		crbs := ToolClusterRoleBindings(kn)
		Expect(crbs).To(BeEmpty(), "ToolClusterRoleBindings() should return empty when no roleBindings")
	})

	It("CRB subjects are Group kind with correct group names", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-team", "platform-eng"}},
			},
		}
		crbs := ToolClusterRoleBindings(kn)
		Expect(crbs).To(HaveLen(1))
		for _, subj := range crbs[0].Subjects {
			Expect(subj.Kind).To(Equal("Group"), "CRB subject kind should be Group")
		}
		subjectNames := make([]string, 0, len(crbs[0].Subjects))
		for _, s := range crbs[0].Subjects {
			subjectNames = append(subjectNames, s.Name)
		}
		Expect(subjectNames).To(ConsistOf("sre-team", "platform-eng"))
	})

	It("CRB roleRef points to namespace-prefixed tool ClusterRole", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-team"}},
			},
		}
		crbs := ToolClusterRoleBindings(kn)
		Expect(crbs).To(HaveLen(1))
		Expect(crbs[0].RoleRef.Name).To(Equal(kn.Namespace+"-tool-sre"),
			"CRB roleRef should point to namespace-prefixed tool ClusterRole")
		Expect(crbs[0].RoleRef.Kind).To(Equal("ClusterRole"))
	})

	It("duplicate roles with different groups are merged", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", Groups: []string{"team-a"}},
				{Role: "sre", Groups: []string{"team-b"}},
			},
		}
		crbs := ToolClusterRoleBindings(kn)
		Expect(crbs).To(HaveLen(1), "duplicate roles should be merged into a single CRB")
		subjectNames := make([]string, 0, len(crbs[0].Subjects))
		for _, s := range crbs[0].Subjects {
			subjectNames = append(subjectNames, s.Name)
		}
		Expect(subjectNames).To(ConsistOf("team-a", "team-b"))
	})

	It("ToolCRBNames returns all CRB names for finalizer cleanup", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-team"}},
				{Role: "cicd", Groups: []string{"ci-bots"}},
			},
		}
		names := ToolCRBNames(kn)
		Expect(names).To(HaveLen(2))
		for _, n := range names {
			Expect(n).To(HavePrefix(kn.Namespace+"-"),
				"ToolCRBNames entry %q should be namespace-prefixed", n)
		}
	})

	// --- Issue #181: Custom ClusterRole references ---

	It("[AC-3] creates CRB referencing user-managed ClusterRole for clusterRoleName binding", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{ClusterRoleName: "my-custom-investigator", Groups: []string{"junior-sres"}},
			},
		}
		crbs := ToolClusterRoleBindings(kn)
		Expect(crbs).To(HaveLen(1))
		Expect(crbs[0].RoleRef.Name).To(Equal("my-custom-investigator"),
			"custom clusterRoleName CRB should reference the user-managed ClusterRole directly")
		Expect(crbs[0].RoleRef.Kind).To(Equal("ClusterRole"))
		Expect(crbs[0].RoleRef.APIGroup).To(Equal(rbacv1.GroupName))
		Expect(crbs[0].Subjects).To(HaveLen(1))
		Expect(crbs[0].Subjects[0].Name).To(Equal("junior-sres"))
	})

	It("[AC-3] mixed persona + custom bindings coexist in same CR", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", Groups: []string{"senior-sres"}},
				{ClusterRoleName: "my-custom-investigator", Groups: []string{"junior-sres"}},
			},
		}
		crbs := ToolClusterRoleBindings(kn)
		Expect(crbs).To(HaveLen(2), "should return CRBs for both persona and custom bindings")

		personaCRB := crbs[0]
		Expect(personaCRB.RoleRef.Name).To(Equal(kn.Namespace+"-tool-sre"),
			"persona CRB should use namespace-prefixed ClusterRole name")

		customCRB := crbs[1]
		Expect(customCRB.RoleRef.Name).To(Equal("my-custom-investigator"),
			"custom CRB should reference the user-managed ClusterRole directly")
	})

	It("[AC-6] ToolCRBNames includes names for custom clusterRoleName bindings", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-team"}},
				{ClusterRoleName: "my-custom-role", Groups: []string{"custom-team"}},
			},
		}
		names := ToolCRBNames(kn)
		Expect(names).To(HaveLen(2), "ToolCRBNames should include both persona and custom CRB names")
	})
})

// --- Issue #289: console-access RBAC gate ---

var _ = Describe("ConsoleAccessClusterRole", func() {
	It("[CA-010] returns a ClusterRole with apiGroup kubernaut.ai, resource console, verb use", func() {
		kn := testKubernautWithAF()
		cr := ConsoleAccessClusterRole(kn)
		Expect(cr).NotTo(BeNil())
		Expect(cr.Rules).To(HaveLen(1))
		rule := cr.Rules[0]
		Expect(rule.APIGroups).To(ConsistOf(kubernautAPIGroup))
		Expect(rule.Resources).To(ConsistOf("console"))
		Expect(rule.Verbs).To(ConsistOf("use"))
	})

	It("[CA-011] name is namespace-prefixed", func() {
		kn := testKubernautWithAF()
		cr := ConsoleAccessClusterRole(kn)
		Expect(cr).NotTo(BeNil())
		Expect(cr.Name).To(Equal(kn.Namespace + "-console-access"))
	})

	It("[CA-012] returns nil when AF is disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		Expect(ConsoleAccessClusterRole(kn)).To(BeNil())
	})

	It("[CA-013] renders even when no groups are bound anywhere", func() {
		kn := testKubernautWithAF()
		Expect(kn.Spec.APIFrontend.RBAC).To(BeNil(), "precondition: no roleBindings configured")
		Expect(ConsoleAccessClusterRole(kn)).NotTo(BeNil(),
			"ClusterRole should render unconditionally, matching upstream's always-render behavior")
	})
})

var _ = Describe("effectiveConsoleAccessGroups", func() {
	It("[CA-020] returns nil when RBAC is nil", func() {
		kn := testKubernautWithAF()
		Expect(kn.Spec.APIFrontend.RBAC).To(BeNil())
		Expect(effectiveConsoleAccessGroups(kn)).To(BeNil())
	})

	It("[CA-021] nil ConsoleAccessGroups derives the deduplicated union of roleBindings groups", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings: []kubernautv1alpha1.ToolRoleBinding{
				{Role: "sre", Groups: []string{"sre-team", "platform-eng"}},
				{Role: "cicd", Groups: []string{"ci-bots"}},
				{Role: "l3-audit", Groups: []string{"platform-eng"}},
			},
		}
		Expect(effectiveConsoleAccessGroups(kn)).To(ConsistOf("sre-team", "platform-eng", "ci-bots"))
	})

	It("[CA-022] derivation collapses the #289 live scenario (6 roleBindings, one shared group) to a single group", func() {
		kn := testKubernautWithAF()
		roleBindings := make([]kubernautv1alpha1.ToolRoleBinding, 0, 6)
		for _, role := range []string{"sre", "ai-orchestrator", "cicd", "observability", "l3-audit", "remediation-approver"} {
			roleBindings = append(roleBindings, kubernautv1alpha1.ToolRoleBinding{
				Role: role, Groups: []string{"platform-engineering"},
			})
		}
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{RoleBindings: roleBindings}
		Expect(effectiveConsoleAccessGroups(kn)).To(Equal([]string{"platform-engineering"}))
	})

	It("[CA-023] explicit empty list opts out regardless of roleBindings content", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings:        []kubernautv1alpha1.ToolRoleBinding{{Role: "sre", Groups: []string{"sre-team"}}},
			ConsoleAccessGroups: []string{},
		}
		Expect(effectiveConsoleAccessGroups(kn)).To(BeEmpty())
	})

	It("[CA-024] explicit non-empty list is used verbatim, ignoring roleBindings", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			RoleBindings:        []kubernautv1alpha1.ToolRoleBinding{{Role: "sre", Groups: []string{"sre-team"}}},
			ConsoleAccessGroups: []string{"console-only-group"},
		}
		Expect(effectiveConsoleAccessGroups(kn)).To(ConsistOf("console-only-group"))
	})
})

var _ = Describe("ConsoleAccessClusterRoleBinding", func() {
	It("[CA-030] returns nil when AF is disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			ConsoleAccessGroups: []string{"some-group"},
		}
		Expect(ConsoleAccessClusterRoleBinding(kn)).To(BeNil())
	})

	It("[CA-031] returns nil when the effective group list is empty", func() {
		kn := testKubernautWithAF()
		Expect(ConsoleAccessClusterRoleBinding(kn)).To(BeNil())
	})

	It("[CA-032] one Group-kind subject per effective group, no explicit APIGroup", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			ConsoleAccessGroups: []string{"group-a", "group-b"},
		}
		crb := ConsoleAccessClusterRoleBinding(kn)
		Expect(crb).NotTo(BeNil())
		Expect(crb.Subjects).To(HaveLen(2))
		subjectNames := make([]string, 0, len(crb.Subjects))
		for _, s := range crb.Subjects {
			Expect(s.Kind).To(Equal("Group"))
			Expect(s.APIGroup).To(BeEmpty(), "Group subjects should not set an explicit APIGroup (matches ToolClusterRoleBindings convention)")
			subjectNames = append(subjectNames, s.Name)
		}
		Expect(subjectNames).To(ConsistOf("group-a", "group-b"))
	})

	It("[CA-033] roleRef points to the namespace-prefixed console-access ClusterRole", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			ConsoleAccessGroups: []string{"group-a"},
		}
		crb := ConsoleAccessClusterRoleBinding(kn)
		Expect(crb).NotTo(BeNil())
		Expect(crb.RoleRef.Kind).To(Equal("ClusterRole"))
		Expect(crb.RoleRef.APIGroup).To(Equal(rbacv1.GroupName))
		Expect(crb.RoleRef.Name).To(Equal(kn.Namespace + "-console-access"))
	})

	It("[CA-034] CRB name is namespace-prefixed and static regardless of group count", func() {
		kn := testKubernautWithAF()
		kn.Spec.APIFrontend.RBAC = &kubernautv1alpha1.APIFrontendRBACSpec{
			ConsoleAccessGroups: []string{"group-a", "group-b", "group-c"},
		}
		crb := ConsoleAccessClusterRoleBinding(kn)
		Expect(crb).NotTo(BeNil())
		Expect(crb.Name).To(Equal(kn.Namespace + "-console-access-binding"))
	})
})

var _ = Describe("APIFrontend ClusterRole", func() {
	It("is included when AF is enabled", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		found := false
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "apifrontend ClusterRole should be present when AF is enabled")
	})

	It("grants InvestigationSession CRUD under kubernaut.ai API group", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		ruleMap := map[string][]string{}
		for _, rule := range afRole.Rules {
			for _, res := range rule.Resources {
				key := rule.APIGroups[0] + "/" + res
				ruleMap[key] = rule.Verbs
			}
		}

		Expect(ruleMap).To(HaveKey("kubernaut.ai/investigationsessions"))
		Expect(ruleMap["kubernaut.ai/investigationsessions"]).To(ContainElements("get", "list", "watch", "create", "update", "delete"))

		Expect(ruleMap).NotTo(HaveKey("apifrontend.kubernaut.ai/investigationsessions"),
			"old apifrontend.kubernaut.ai API group must not be present")

		Expect(ruleMap).NotTo(HaveKey("/users"),
			"impersonate verb removed per unified security model (ADR-022)")
	})

	It("grants expanded remediationrequests and remediationapprovalrequests permissions", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		ruleMap := map[string][]string{}
		for _, rule := range afRole.Rules {
			for _, res := range rule.Resources {
				key := rule.APIGroups[0] + "/" + res
				ruleMap[key] = rule.Verbs
			}
		}

		Expect(ruleMap).To(HaveKey("kubernaut.ai/remediationrequests"))
		Expect(ruleMap["kubernaut.ai/remediationrequests"]).To(ContainElements("get", "list", "watch", "create", "update", "patch"))

		Expect(ruleMap).To(HaveKey("kubernaut.ai/remediationapprovalrequests"))
		Expect(ruleMap["kubernaut.ai/remediationapprovalrequests"]).To(ContainElements("get", "list", "watch", "create", "update", "patch"))

		Expect(ruleMap).To(HaveKey("kubernaut.ai/remediationapprovalrequests/status"))
		Expect(ruleMap["kubernaut.ai/remediationapprovalrequests/status"]).To(ContainElements("get", "update", "patch"))
	})

	It("grants core resource read access for kubectl_get/kubectl_list tools", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		ruleMap := map[string][]string{}
		for _, rule := range afRole.Rules {
			for _, res := range rule.Resources {
				key := rule.APIGroups[0] + "/" + res
				ruleMap[key] = rule.Verbs
			}
		}

		Expect(ruleMap).To(HaveKey("/pods"))
		Expect(ruleMap["/pods"]).To(ContainElements("get", "list"))
		Expect(ruleMap).To(HaveKey("/replicationcontrollers"))
		Expect(ruleMap["/replicationcontrollers"]).To(ContainElements("get", "list"))
		Expect(ruleMap).To(HaveKey("/events"))
		Expect(ruleMap["/events"]).To(ContainElements("get", "list", "create", "patch"))

		Expect(ruleMap).To(HaveKey("apps/deployments"))
		Expect(ruleMap["apps/deployments"]).To(ContainElements("get", "list"))
		Expect(ruleMap).To(HaveKey("apps/replicasets"))
		Expect(ruleMap).To(HaveKey("apps/statefulsets"))
		Expect(ruleMap).To(HaveKey("apps/daemonsets"))

		Expect(ruleMap).To(HaveKey("batch/jobs"))
		Expect(ruleMap["batch/jobs"]).To(ContainElements("get"))
		Expect(ruleMap).To(HaveKey("batch/cronjobs"))
	})

	It("has a matching ClusterRoleBinding when AF is enabled", func() {
		kn := testKubernautWithAF()
		bindings := ClusterRoleBindings(kn, testKnV2(kn))
		roleName := clusterRoleName(kn, "apifrontend-role")
		found := false
		for _, crb := range bindings {
			if crb.RoleRef.Name == roleName {
				found = true
				Expect(crb.Subjects).NotTo(BeEmpty())
				Expect(crb.Subjects[0].Name).To(Equal(ServiceAccountName(ComponentAPIFrontend)))
				break
			}
		}
		Expect(found).To(BeTrue(), "ClusterRoleBinding for apifrontend should exist")
	})

	It("is excluded when AF is disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.APIFrontend.Enabled = &disabled
		roles := ClusterRoles(kn, testKnV2(kn))
		for _, r := range roles {
			Expect(r.Name).NotTo(Equal(clusterRoleName(kn, "apifrontend-role")),
				"apifrontend ClusterRole should not be present when AF is disabled")
		}
		bindings := ClusterRoleBindings(kn, testKnV2(kn))
		for _, crb := range bindings {
			Expect(crb.RoleRef.Name).NotTo(Equal(clusterRoleName(kn, "apifrontend-role")),
				"apifrontend CRB should not be present when AF is disabled")
		}
	})

	It("is excluded when Gateway is disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.Gateway.Enabled = &disabled
		roles := ClusterRoles(kn, testKnV2(kn))
		for _, r := range roles {
			Expect(r.Name).NotTo(Equal(clusterRoleName(kn, "gateway-role")),
				"gateway ClusterRole should not be present when Gateway is disabled")
		}
		bindings := ClusterRoleBindings(kn, testKnV2(kn))
		for _, crb := range bindings {
			Expect(crb.RoleRef.Name).NotTo(Equal(clusterRoleName(kn, "gateway-role")),
				"gateway CRB should not be present when Gateway is disabled")
		}
	})

	It("includes gateway ClusterRole and CRB when Gateway is enabled by default", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		roleNames := make([]string, 0, len(roles))
		for _, r := range roles {
			roleNames = append(roleNames, r.Name)
		}
		Expect(roleNames).To(ContainElement(clusterRoleName(kn, "gateway-role")))

		bindings := ClusterRoleBindings(kn, testKnV2(kn))
		crbRoleRefs := make([]string, 0, len(bindings))
		for _, crb := range bindings {
			crbRoleRefs = append(crbRoleRefs, crb.RoleRef.Name)
		}
		Expect(crbRoleRefs).To(ContainElement(clusterRoleName(kn, "gateway-role")))
	})

	It("excludes gateway data-storage-client RoleBinding when Gateway is disabled", func() {
		kn := testKubernautWithAF()
		disabled := false
		kn.Spec.Gateway.Enabled = &disabled
		rbs := DataStorageClientRoleBindings(kn)
		for _, rb := range rbs {
			Expect(rb.Name).NotTo(Equal("data-storage-client-gateway"),
				"data-storage-client-gateway RoleBinding should not be present when Gateway is disabled")
		}
	})

	It("includes subjectaccessreviews/create permission", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		found := false
		for _, rule := range afRole.Rules {
			for _, g := range rule.APIGroups {
				if g != "authorization.k8s.io" {
					continue
				}
				for _, res := range rule.Resources {
					if res == "subjectaccessreviews" {
						Expect(rule.Verbs).To(ContainElement("create"))
						found = true
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "apifrontend ClusterRole should include subjectaccessreviews/create")
	})

	It("grants remediationrequests/status get+update+patch for cancel flow", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		found := false
		for _, rule := range afRole.Rules {
			if len(rule.APIGroups) > 0 && rule.APIGroups[0] == kubernautAPIGroup {
				for _, res := range rule.Resources {
					if res == "remediationrequests/status" {
						Expect(rule.Verbs).To(ContainElements("get", "update", "patch"))
						found = true
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "apifrontend ClusterRole should include remediationrequests/status with get+update+patch")
	})

	It("does not grant patch on investigationsessions (AC-6 least privilege)", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		for _, rule := range afRole.Rules {
			if len(rule.APIGroups) > 0 && rule.APIGroups[0] == kubernautAPIGroup {
				for _, res := range rule.Resources {
					if res == "investigationsessions" || res == "investigationsessions/status" {
						Expect(rule.Verbs).NotTo(ContainElement("patch"),
							"apifrontend ClusterRole should not grant patch on %s (AC-6)", res)
					}
				}
			}
		}
	})

	It("includes remediationrequests with full CRUD+watch verbs", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		found := false
		for _, rule := range afRole.Rules {
			for _, g := range rule.APIGroups {
				if g != kubernautAPIGroup {
					continue
				}
				for _, res := range rule.Resources {
					if res == "remediationrequests" {
						Expect(rule.Verbs).To(ContainElements("get", "list", "watch", "create", "update", "patch"))
						found = true
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "apifrontend ClusterRole should include remediationrequests with full CRUD+watch verbs")
	})

	It("includes aianalyses read-only access for kubernaut_await_session", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		found := false
		for _, rule := range afRole.Rules {
			if len(rule.APIGroups) > 0 && rule.APIGroups[0] == kubernautAPIGroup {
				for _, res := range rule.Resources {
					if res == "aianalyses" {
						Expect(rule.Verbs).To(ContainElements("get", "list", "watch"))
						found = true
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "apifrontend ClusterRole should include aianalyses get/list/watch")
	})

	It("includes effectivenessassessments read-only access for status/subscribe EA streaming (#176)", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		found := false
		for _, rule := range afRole.Rules {
			if len(rule.APIGroups) > 0 && rule.APIGroups[0] == kubernautAPIGroup {
				for _, res := range rule.Resources {
					if res == "effectivenessassessments" {
						Expect(rule.Verbs).To(ContainElements("get", "list", "watch"))
						found = true
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "apifrontend ClusterRole should include effectivenessassessments get/list/watch")
	})

	It("includes tokenreviews create for TokenReview auth", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		found := false
		for _, rule := range afRole.Rules {
			if len(rule.APIGroups) > 0 && rule.APIGroups[0] == "authentication.k8s.io" {
				for _, res := range rule.Resources {
					if res == "tokenreviews" {
						Expect(rule.Verbs).To(ContainElement("create"))
						found = true
					}
				}
			}
		}
		Expect(found).To(BeTrue(), "apifrontend ClusterRole should include tokenreviews/create")
	})

	It("grants KubeVirt and CDI read access for VM triage", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		foundGroups := make(map[string]bool)
		for _, rule := range afRole.Rules {
			for _, g := range rule.APIGroups {
				foundGroups[g] = true
			}
		}
		Expect(foundGroups["kubevirt.io"]).To(BeTrue(), "apifrontend ClusterRole missing kubevirt.io API group")
		Expect(foundGroups["cdi.kubevirt.io"]).To(BeTrue(), "apifrontend ClusterRole missing cdi.kubevirt.io API group")
	})

	It("includes services/kubernaut-agent create for KA SAR gate (#137)", func() {
		kn := testKubernautWithAF()
		roles := ClusterRoles(kn, testKnV2(kn))
		var afRole *rbacv1.ClusterRole
		for _, r := range roles {
			if r.Name == clusterRoleName(kn, "apifrontend-role") {
				afRole = r
				break
			}
		}
		Expect(afRole).NotTo(BeNil())

		found := false
		for _, rule := range afRole.Rules {
			if len(rule.APIGroups) > 0 && rule.APIGroups[0] == "" &&
				len(rule.Resources) > 0 && rule.Resources[0] == "services" &&
				len(rule.ResourceNames) > 0 && rule.ResourceNames[0] == "kubernaut-agent" {
				Expect(rule.Verbs).To(ContainElement("create"))
				found = true
			}
		}
		Expect(found).To(BeTrue(), "apifrontend ClusterRole should include services/kubernaut-agent create for KA SAR gate")
	})
})

// #224: mcpGatewayCRDPolicyRules extracts the gatewayType-conditional MCP
// Gateway CRD watch rules FMC already had (#200) into a shared helper
// reused by SP/AF/EM's ClusterRoles and by MCPGatewayNamespaceRBAC's
// namespace-scoped Role variant.
var _ = Describe("mcpGatewayCRDPolicyRules", func() {
	It("returns Envoy AI Gateway CRD rules for eaigw", func() {
		rules := mcpGatewayCRDPolicyRules("eaigw")
		apiGroups := make([]string, 0, len(rules))
		for _, r := range rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).To(ContainElement("gateway.envoyproxy.io"))
		Expect(apiGroups).To(ContainElement("aigateway.envoyproxy.io"))
		Expect(apiGroups).NotTo(ContainElement("mcp.kuadrant.io"))
	})

	It("returns Kuadrant CRD rules for kuadrant", func() {
		rules := mcpGatewayCRDPolicyRules(mcpGatewayTypeKuadrant)
		apiGroups := make([]string, 0, len(rules))
		for _, r := range rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).To(ContainElement("mcp.kuadrant.io"))
		Expect(apiGroups).To(ContainElement("gateway.networking.k8s.io"))
		Expect(apiGroups).NotTo(ContainElement("gateway.envoyproxy.io"))
	})

	It("matches FleetMetadataCache's pre-existing rule sets exactly (extracted helper, no behavior change)", func() {
		kn, knV2 := testKubernautWithFMC()
		Expect(fleetMetadataCacheClusterRole(kn, knV2, CommonLabels(kn)).Rules).To(Equal(mcpGatewayCRDPolicyRules("eaigw")))

		knV2.Spec.Fleet.MCPGatewayType = mcpGatewayTypeKuadrant
		Expect(fleetMetadataCacheClusterRole(kn, knV2, CommonLabels(kn)).Rules).To(Equal(mcpGatewayCRDPolicyRules(mcpGatewayTypeKuadrant)))
	})
})

var _ = Describe("SignalProcessing fleet RBAC", func() {
	It("omits MCP Gateway CRD rules when fleet is disabled", func() {
		kn := testKubernaut()
		cr := signalprocessingClusterRole(kn, testKnV2(kn), CommonLabels(kn))
		apiGroups := make([]string, 0, len(cr.Rules))
		for _, r := range cr.Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).NotTo(ContainElement("gateway.envoyproxy.io"))
	})

	It("gains MCP Gateway CRD rules when fleet enabled with mcpGatewayEndpoint set and no namespace override", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		cr := signalprocessingClusterRole(kn, knV2, CommonLabels(kn))
		apiGroups := make([]string, 0, len(cr.Rules))
		for _, r := range cr.Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).To(ContainElement("gateway.envoyproxy.io"), "signalprocessing ClusterRole should gain MCP Gateway CRD rules when fleet enabled")
		Expect(apiGroups).To(ContainElement("kubernaut.ai"), "signalprocessing ClusterRole should keep its pre-existing rules")
	})

	It("omits MCP Gateway CRD rules from the ClusterRole when its effective mcpGatewayNamespace resolves non-empty (moved to a namespace-scoped Role instead)", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		knV2.Spec.SignalProcessing.Fleet = &kubernautv1alpha2.FleetOverrideSpec{Namespace: testSPMCPGatewayNamespace}
		cr := signalprocessingClusterRole(kn, knV2, CommonLabels(kn))
		apiGroups := make([]string, 0, len(cr.Rules))
		for _, r := range cr.Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).NotTo(ContainElement("gateway.envoyproxy.io"), "signalprocessing ClusterRole should not grant cluster-wide MCP Gateway CRD access once a namespace resolves")
	})

	It("falls back to the shared spec.fleet.mcpGatewayNamespace when signalProcessing has no override", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		knV2.Spec.Fleet.MCPGatewayNamespace = testSharedMCPGatewayNamespace
		cr := signalprocessingClusterRole(kn, knV2, CommonLabels(kn))
		apiGroups := make([]string, 0, len(cr.Rules))
		for _, r := range cr.Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).NotTo(ContainElement("gateway.envoyproxy.io"), "signalprocessing ClusterRole should defer to the namespace-scoped Role once the shared namespace resolves")
	})
})

// #224 Finding 4: AF/EM's ClusterRegistry construction hardcodes
// registry.RegistryConfig{} (cluster-wide) upstream -- their ClusterRole
// must stay cluster-scoped regardless of any mcpGatewayNamespace hint
// (which they don't even have a field for). Tracked upstream separately.
var _ = Describe("APIFrontend/EffectivenessMonitor fleet RBAC", func() {
	It("apifrontendClusterRole gains MCP Gateway CRD rules when fleet enabled with mcpGatewayEndpoint set", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		kn.Spec.APIFrontend = kubernautv1alpha1.APIFrontendSpec{
			Auth: kubernautv1alpha1.APIFrontendAuthSpec{IssuerURL: "https://login.kubernaut.ai/realms/kubernaut", Audience: "kubernaut-apifrontend"},
		}
		cr := apifrontendClusterRole(kn, knV2, CommonLabels(kn))
		apiGroups := make([]string, 0, len(cr.Rules))
		for _, r := range cr.Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).To(ContainElement("gateway.envoyproxy.io"))
	})

	It("apifrontendClusterRole omits MCP Gateway CRD rules when fleet is disabled", func() {
		kn := testKubernautWithAF()
		cr := apifrontendClusterRole(kn, testKnV2(kn), CommonLabels(kn))
		apiGroups := make([]string, 0, len(cr.Rules))
		for _, r := range cr.Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).NotTo(ContainElement("gateway.envoyproxy.io"))
	})

	It("effectivenessMonitorControllerClusterRole gains MCP Gateway CRD rules when fleet enabled with mcpGatewayEndpoint set", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		cr := effectivenessMonitorControllerClusterRole(kn, knV2, CommonLabels(kn))
		apiGroups := make([]string, 0, len(cr.Rules))
		for _, r := range cr.Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).To(ContainElement("gateway.envoyproxy.io"))
	})

	It("effectivenessMonitorControllerClusterRole omits MCP Gateway CRD rules when fleet is disabled", func() {
		kn := testKubernaut()
		cr := effectivenessMonitorControllerClusterRole(kn, testKnV2(kn), CommonLabels(kn))
		apiGroups := make([]string, 0, len(cr.Rules))
		for _, r := range cr.Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).NotTo(ContainElement("gateway.envoyproxy.io"))
	})
})

var _ = Describe("FMC scope-check RBAC (#1993 gap)", func() {
	Context("ClusterRoles", func() {
		It("omits fleetmetadatacache-auth-middleware and fmc-scope-check-client when FMC is disabled", func() {
			kn := testKubernaut()
			roles := ClusterRoles(kn, testKnV2(kn))
			for _, r := range roles {
				Expect(r.Name).NotTo(Equal(clusterRoleName(kn, "fleetmetadatacache-auth-middleware")))
				Expect(r.Name).NotTo(Equal(clusterRoleName(kn, "fmc-scope-check-client")))
			}
		})

		It("grants fleetmetadatacache-auth-middleware TokenReview/SAR create when FMC is enabled", func() {
			kn, knV2 := testKubernautWithFMC()
			roles := ClusterRoles(kn, knV2)

			var cr *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == clusterRoleName(kn, "fleetmetadatacache-auth-middleware") {
					cr = r
				}
			}
			Expect(cr).NotTo(BeNil(), "fleetmetadatacache-auth-middleware ClusterRole not found")

			var grantsTokenReview, grantsSAR bool
			for _, rule := range cr.Rules {
				for _, ag := range rule.APIGroups {
					if ag == "authentication.k8s.io" {
						grantsTokenReview = true
					}
					if ag == "authorization.k8s.io" {
						grantsSAR = true
					}
				}
			}
			Expect(grantsTokenReview).To(BeTrue(), "must grant authentication.k8s.io tokenreviews create, mirroring data-storage-auth-middleware")
			Expect(grantsSAR).To(BeTrue(), "must grant authorization.k8s.io subjectaccessreviews create, mirroring data-storage-auth-middleware")
		})

		It("grants fmc-scope-check-client get on the fleetmetadatacache-service by resource name when FMC is enabled", func() {
			kn, knV2 := testKubernautWithFMC()
			roles := ClusterRoles(kn, knV2)

			var cr *rbacv1.ClusterRole
			for _, r := range roles {
				if r.Name == clusterRoleName(kn, "fmc-scope-check-client") {
					cr = r
				}
			}
			Expect(cr).NotTo(BeNil(), "fmc-scope-check-client ClusterRole not found")
			Expect(cr.Rules).To(ContainElement(rbacv1.PolicyRule{
				APIGroups:     []string{""},
				Resources:     []string{"services"},
				ResourceNames: []string{"fleetmetadatacache-service"},
				Verbs:         []string{"get"},
			}))
		})
	})

	Context("ClusterRoleBindings", func() {
		It("omits the FMC scope-check bindings when FMC is disabled", func() {
			kn := testKubernaut()
			crbs := ClusterRoleBindings(kn, testKnV2(kn))
			for _, crb := range crbs {
				Expect(crb.Name).NotTo(Equal(clusterRoleName(kn, "fleetmetadatacache-auth-middleware-binding")))
				Expect(crb.Name).NotTo(Equal(clusterRoleName(kn, "gateway-fmc-scope-check-client")))
				Expect(crb.Name).NotTo(Equal(clusterRoleName(kn, "remediationorchestrator-fmc-scope-check-client")))
			}
		})

		It("binds FMC's own SA to fleetmetadatacache-auth-middleware when FMC is enabled", func() {
			kn, knV2 := testKubernautWithFMC()
			crbs := ClusterRoleBindings(kn, knV2)

			var crb *rbacv1.ClusterRoleBinding
			for _, c := range crbs {
				if c.Name == clusterRoleName(kn, "fleetmetadatacache-auth-middleware-binding") {
					crb = c
				}
			}
			Expect(crb).NotTo(BeNil(), "fleetmetadatacache-auth-middleware-binding CRB not found")
			Expect(crb.RoleRef.Name).To(Equal(clusterRoleName(kn, "fleetmetadatacache-auth-middleware")))
			Expect(crb.Subjects).To(HaveLen(1))
			Expect(crb.Subjects[0].Name).To(Equal(ServiceAccountName(ComponentFleetMetadataCache)))
			Expect(crb.Subjects[0].Namespace).To(Equal(kn.Namespace))
		})

		It("binds Gateway and RemediationOrchestrator SAs to fmc-scope-check-client when FMC is enabled", func() {
			kn, knV2 := testKubernautWithFMC()
			crbs := ClusterRoleBindings(kn, knV2)

			bound := map[string]string{}
			for _, c := range crbs {
				if c.RoleRef.Name == clusterRoleName(kn, "fmc-scope-check-client") {
					Expect(c.Subjects).To(HaveLen(1))
					bound[c.Name] = c.Subjects[0].Name
				}
			}
			Expect(bound).To(HaveKeyWithValue(clusterRoleName(kn, "gateway-fmc-scope-check-client"), ServiceAccountName(ComponentGateway)))
			Expect(bound).To(HaveKeyWithValue(clusterRoleName(kn, "remediationorchestrator-fmc-scope-check-client"), ServiceAccountName(ComponentRemediationOrchestrator)))
		})

		It("omits the Gateway fmc-scope-check-client binding when Gateway is disabled", func() {
			kn, knV2 := testKubernautWithFMC()
			kn.Spec.Gateway.Enabled = ptr.To(false)
			crbs := ClusterRoleBindings(kn, knV2)
			for _, crb := range crbs {
				Expect(crb.Name).NotTo(Equal(clusterRoleName(kn, "gateway-fmc-scope-check-client")))
			}
		})
	})
})

var _ = Describe("MCPGatewayNamespaceRBAC", func() {
	It("returns nothing for either FMC or SignalProcessing when their effective namespace is empty", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		roles, rbs := MCPGatewayNamespaceRBAC(kn, knV2)
		Expect(roles).To(BeEmpty())
		Expect(rbs).To(BeEmpty())
	})

	It("returns a Role/RoleBinding pair for SignalProcessing when its effective namespace resolves", func() {
		kn, knV2 := testKubernautWithFleetMCP()
		knV2.Spec.SignalProcessing.Fleet = &kubernautv1alpha2.FleetOverrideSpec{Namespace: testSPMCPGatewayNamespace}
		roles, rbs := MCPGatewayNamespaceRBAC(kn, knV2)
		Expect(roles).To(HaveLen(1))
		Expect(roles[0].Namespace).To(Equal(testSPMCPGatewayNamespace))
		apiGroups := make([]string, 0, len(roles[0].Rules))
		for _, r := range roles[0].Rules {
			apiGroups = append(apiGroups, r.APIGroups...)
		}
		Expect(apiGroups).To(ContainElement("gateway.envoyproxy.io"))
		Expect(rbs).To(HaveLen(1))
		Expect(rbs[0].Namespace).To(Equal(testSPMCPGatewayNamespace))
		Expect(rbs[0].Subjects[0].Name).To(Equal(ServiceAccountName(ComponentSignalProcessing)))
	})

	It("returns a Role/RoleBinding pair for FleetMetadataCache when its effective namespace resolves", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.FleetMetadataCache.Fleet = &kubernautv1alpha2.FleetOverrideSpec{Namespace: testFMCMCPGatewayNamespace}
		roles, rbs := MCPGatewayNamespaceRBAC(kn, knV2)
		Expect(roles).To(HaveLen(1))
		Expect(roles[0].Namespace).To(Equal(testFMCMCPGatewayNamespace))
		Expect(rbs).To(HaveLen(1))
		Expect(rbs[0].Subjects[0].Name).To(Equal(ServiceAccountName(ComponentFleetMetadataCache)))
	})

	It("FleetMetadataCache falls back to the shared spec.fleet.mcpGatewayNamespace when it has no override", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.Fleet.MCPGatewayNamespace = testSharedMCPGatewayNamespace
		roles, _ := MCPGatewayNamespaceRBAC(kn, knV2)
		// SP also falls back to the same shared namespace here since
		// testKubernautWithFMC() already satisfies mcpGatewayRemoteReadsEnabled
		// (fleet.enabled + mcpGatewayEndpoint) -- SP's cluster classification
		// is orthogonal to FMC's backend choice (BR-INTEGRATION-054).
		var fmcRole *rbacv1.Role
		for _, r := range roles {
			if r.Namespace == testSharedMCPGatewayNamespace && strings.Contains(r.Name, "fleetmetadatacache") {
				fmcRole = r
			}
		}
		Expect(fmcRole).NotTo(BeNil())
	})

	It("returns both when both FMC and SignalProcessing resolve to (possibly different) namespaces", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.FleetMetadataCache.Fleet = &kubernautv1alpha2.FleetOverrideSpec{Namespace: testFMCMCPGatewayNamespace}
		knV2.Spec.Fleet.MCPGatewayEndpoint = "https://mcp-gateway.example.com/sse"
		knV2.Spec.Fleet.MCPGatewayType = "eaigw"
		knV2.Spec.SignalProcessing.Fleet = &kubernautv1alpha2.FleetOverrideSpec{Namespace: testSPMCPGatewayNamespace}
		roles, rbs := MCPGatewayNamespaceRBAC(kn, knV2)
		Expect(roles).To(HaveLen(2))
		Expect(rbs).To(HaveLen(2))
	})
})

var _ = Describe("FleetMetadataCache RBAC namespace retrofit", func() {
	It("fleetMetadataCacheClusterRole/Binding are excluded from ClusterRoles()/ClusterRoleBindings() once FMC's effective namespace resolves", func() {
		kn, knV2 := testKubernautWithFMC()
		knV2.Spec.FleetMetadataCache.Fleet = &kubernautv1alpha2.FleetOverrideSpec{Namespace: testFMCMCPGatewayNamespace}

		for _, cr := range ClusterRoles(kn, knV2) {
			Expect(cr.Name).NotTo(Equal(clusterRoleName(kn, "fleetmetadatacache")),
				"FMC's ClusterRole should be superseded by MCPGatewayNamespaceRBAC's namespace-scoped Role once a namespace resolves")
		}
		for _, crb := range ClusterRoleBindings(kn, knV2) {
			Expect(crb.Name).NotTo(Equal(clusterRoleName(kn, "fleetmetadatacache-binding")))
		}
	})

	It("still includes fleetMetadataCacheClusterRole/Binding when FMC has no effective namespace (regression)", func() {
		kn, knV2 := testKubernautWithFMC()
		found := false
		for _, cr := range ClusterRoles(kn, knV2) {
			if cr.Name == clusterRoleName(kn, "fleetmetadatacache") {
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})
})

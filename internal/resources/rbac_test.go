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
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
)

func TestClusterRoles_Count(t *testing.T) {
	kn := testKubernaut()
	// Monitoring enabled by default → includes alertmanager-view + gateway-signal-source
	roles := ClusterRoles(kn)
	if len(roles) < 13 {
		t.Errorf("ClusterRoles() should return at least 13 roles (base), got %d", len(roles))
	}
}

func TestClusterRoles_MonitoringDisabledReducesCount(t *testing.T) {
	kn := testKubernaut()
	enabled := false
	kn.Spec.Monitoring.Enabled = &enabled

	withMonitoring := ClusterRoles(testKubernaut())
	withoutMonitoring := ClusterRoles(kn)

	if len(withoutMonitoring) >= len(withMonitoring) {
		t.Errorf("disabling monitoring should reduce ClusterRole count: %d vs %d",
			len(withoutMonitoring), len(withMonitoring))
	}
}

func TestClusterRoles_ContainsExpectedNames(t *testing.T) {
	kn := testKubernaut()
	roles := ClusterRoles(kn)
	names := make(map[string]bool, len(roles))
	for _, r := range roles {
		names[r.Name] = true
	}

	ns := kn.Namespace
	expectedNames := []string{
		ns + "-gateway-role",
		ns + "-aianalysis-controller",
		ns + "-holmesgpt-api-client",
		ns + "-holmesgpt-api-investigator",
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
		if !names[name] {
			t.Errorf("ClusterRoles() missing expected role %q", name)
		}
	}
}

func TestClusterRoleBindings_SubjectNamespace(t *testing.T) {
	kn := testKubernaut()
	crbs := ClusterRoleBindings(kn)

	allowedNamespaces := map[string]bool{
		kn.Namespace:             true,
		DefaultWorkflowNamespace: true,
		OCPMonitoringNamespace:   true,
	}

	for _, crb := range crbs {
		if len(crb.Subjects) == 0 {
			t.Errorf("CRB %q has no subjects", crb.Name)
			continue
		}
		for _, subj := range crb.Subjects {
			if !allowedNamespaces[subj.Namespace] {
				t.Errorf("CRB %q subject %q has namespace %q, want one of %v",
					crb.Name, subj.Name, subj.Namespace, allowedNamespaces)
			}
		}
	}
}

func TestClusterRoleBindings_WorkflowRunnerBinding(t *testing.T) {
	kn := testKubernaut()
	crbs := ClusterRoleBindings(kn)
	ns := kn.Namespace

	wantName := ns + "-workflow-runner-binding"
	wantRef := ns + "-workflow-runner"

	found := false
	for _, crb := range crbs {
		if crb.Name == wantName {
			found = true
			if crb.RoleRef.Name != wantRef {
				t.Errorf("workflow-runner CRB roleRef = %q, want %q", crb.RoleRef.Name, wantRef)
			}
			if len(crb.Subjects) == 0 {
				t.Fatal("workflow-runner CRB has no subjects")
			}
			if crb.Subjects[0].Name != "kubernaut-workflow-runner" {
				t.Errorf("workflow-runner CRB subject = %q, want %q", crb.Subjects[0].Name, "kubernaut-workflow-runner")
			}
			if crb.Subjects[0].Namespace != DefaultWorkflowNamespace {
				t.Errorf("workflow-runner CRB subject namespace = %q, want %q", crb.Subjects[0].Namespace, DefaultWorkflowNamespace)
			}
		}
	}
	if !found {
		t.Errorf("ClusterRoleBindings() should include %q", wantName)
	}
}

func TestDataStorageClientRoleBindings_Count10(t *testing.T) {
	kn := testKubernaut()
	rbs := DataStorageClientRoleBindings(kn)
	if len(rbs) != 10 {
		t.Errorf("DataStorageClientRoleBindings() should return 10, got %d", len(rbs))
	}
}

func TestDataStorageClientRoleBindings_AllRefClusterRole(t *testing.T) {
	kn := testKubernaut()
	rbs := DataStorageClientRoleBindings(kn)
	wantRef := kn.Namespace + "-data-storage-client"
	for _, rb := range rbs {
		if rb.RoleRef.Name != wantRef {
			t.Errorf("RoleBinding %q should reference %q, got %q", rb.Name, wantRef, rb.RoleRef.Name)
		}
		if rb.RoleRef.Kind != "ClusterRole" {
			t.Errorf("RoleBinding %q should reference kind ClusterRole, got %q", rb.Name, rb.RoleRef.Kind)
		}
	}
}

func TestNamespaceRoles_AllHaveSecretsConfigmapsAccess(t *testing.T) {
	kn := testKubernaut()
	roles := NamespaceRoles(kn)

	if len(roles) != 10 {
		t.Errorf("NamespaceRoles() should return 10, got %d", len(roles))
	}

	for _, role := range roles {
		if len(role.Rules) != 1 {
			t.Errorf("Role %q should have exactly 1 rule, got %d", role.Name, len(role.Rules))
			continue
		}
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
		if !hasConfigmaps || !hasSecrets {
			t.Errorf("Role %q should grant configmaps+secrets access", role.Name)
		}
	}
}

func TestNamespaceRoleBindings_MatchRoles(t *testing.T) {
	kn := testKubernaut()
	roles := NamespaceRoles(kn)
	rbs := NamespaceRoleBindings(kn)

	if len(rbs) != len(roles) {
		t.Errorf("NamespaceRoleBindings count %d != NamespaceRoles count %d", len(rbs), len(roles))
	}

	roleNames := make(map[string]bool)
	for _, r := range roles {
		roleNames[r.Name] = true
	}
	for _, rb := range rbs {
		if !roleNames[rb.RoleRef.Name] {
			t.Errorf("RoleBinding %q references non-existent role %q", rb.Name, rb.RoleRef.Name)
		}
	}
}

func TestWorkflowNamespaceRBAC_UsesDefaultNamespace(t *testing.T) {
	kn := testKubernaut()
	roles, rbs := WorkflowNamespaceRBAC(kn)

	for _, r := range roles {
		if r.Namespace != "kubernaut-workflows" {
			t.Errorf("workflow Role %q should be in kubernaut-workflows, got %q", r.Name, r.Namespace)
		}
	}
	for _, rb := range rbs {
		if rb.Namespace != "kubernaut-workflows" {
			t.Errorf("workflow RoleBinding %q should be in kubernaut-workflows, got %q", rb.Name, rb.Namespace)
		}
	}
}

func TestWorkflowNamespaceRBAC_UsesCustomNamespace(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.WorkflowExecution.WorkflowNamespace = "my-wf-ns"
	roles, rbs := WorkflowNamespaceRBAC(kn)

	for _, r := range roles {
		if r.Namespace != "my-wf-ns" {
			t.Errorf("workflow Role %q should be in my-wf-ns, got %q", r.Name, r.Namespace)
		}
	}
	for _, rb := range rbs {
		if rb.Namespace != "my-wf-ns" {
			t.Errorf("workflow RoleBinding %q should be in my-wf-ns, got %q", rb.Name, rb.Namespace)
		}
	}
}

func TestAnsibleRBAC_AwxJobsPermission(t *testing.T) {
	kn := testKubernaut()
	kn.Spec.Ansible.Enabled = true
	ns := kn.Namespace
	cr, crb := AnsibleRBAC(kn)

	wantCR := ns + "-workflowexecution-awx"
	if cr.Name != wantCR {
		t.Errorf("AWX ClusterRole name = %q, want %q", cr.Name, wantCR)
	}

	found := false
	for _, rule := range cr.Rules {
		for _, res := range rule.Resources {
			if res == "awxjobs" {
				found = true
			}
		}
	}
	if !found {
		t.Error("AWX ClusterRole should grant access to awxjobs resources")
	}

	if crb.RoleRef.Name != wantCR {
		t.Errorf("AWX CRB roleRef = %q, want %q", crb.RoleRef.Name, wantCR)
	}
}

func TestClusterRoleBindings_MonitoringCRBs(t *testing.T) {
	kn := testKubernaut()
	crbs := ClusterRoleBindings(kn)
	ns := kn.Namespace

	crbMap := make(map[string]*rbacv1.ClusterRoleBinding, len(crbs))
	for _, crb := range crbs {
		crbMap[crb.Name] = crb
	}

	t.Run("cluster-monitoring-view for EM", func(t *testing.T) {
		crb, ok := crbMap[ns+"-effectivenessmonitor-monitoring-view"]
		if !ok {
			t.Fatalf("missing %s-effectivenessmonitor-monitoring-view CRB", ns)
		}
		if crb.RoleRef.Name != "cluster-monitoring-view" {
			t.Errorf("roleRef = %q, want %q", crb.RoleRef.Name, "cluster-monitoring-view")
		}
	})

	t.Run("cluster-monitoring-view for HAPI", func(t *testing.T) {
		crb, ok := crbMap[ns+"-holmesgpt-api-monitoring-view"]
		if !ok {
			t.Fatalf("missing %s-holmesgpt-api-monitoring-view CRB", ns)
		}
		if crb.RoleRef.Name != "cluster-monitoring-view" {
			t.Errorf("roleRef = %q, want %q", crb.RoleRef.Name, "cluster-monitoring-view")
		}
	})

	t.Run("alertmanager signal source binds OCP SA", func(t *testing.T) {
		crb, ok := crbMap[ns+"-alertmanager-gateway-signal-source"]
		if !ok {
			t.Fatalf("missing %s-alertmanager-gateway-signal-source CRB", ns)
		}
		if crb.RoleRef.Name != ns+"-gateway-signal-source" {
			t.Errorf("roleRef = %q, want %q", crb.RoleRef.Name, ns+"-gateway-signal-source")
		}
		if len(crb.Subjects) == 0 {
			t.Fatal("CRB has no subjects")
		}
		subj := crb.Subjects[0]
		if subj.Name != OCPAlertManagerSAName {
			t.Errorf("subject name = %q, want %q", subj.Name, OCPAlertManagerSAName)
		}
		if subj.Namespace != OCPMonitoringNamespace {
			t.Errorf("subject namespace = %q, want %q", subj.Namespace, OCPMonitoringNamespace)
		}
	})
}

func TestClusterRoleBindings_MonitoringDisabled_NoMonitoringCRBs(t *testing.T) {
	kn := testKubernaut()
	disabled := false
	kn.Spec.Monitoring.Enabled = &disabled
	ns := kn.Namespace

	crbs := ClusterRoleBindings(kn)
	monitoringNames := map[string]bool{
		ns + "-effectivenessmonitor-alertmanager-view-binding": true,
		ns + "-effectivenessmonitor-monitoring-view":           true,
		ns + "-holmesgpt-api-monitoring-view":                  true,
		ns + "-alertmanager-gateway-signal-source":             true,
	}

	for _, crb := range crbs {
		if monitoringNames[crb.Name] {
			t.Errorf("monitoring CRB %q should not exist when monitoring is disabled", crb.Name)
		}
	}
}

func TestClusterRoles_HaveCommonLabels(t *testing.T) {
	kn := testKubernaut()
	for _, cr := range ClusterRoles(kn) {
		if cr.Labels["app.kubernetes.io/managed-by"] != "kubernaut-operator" {
			t.Errorf("ClusterRole %q missing managed-by label", cr.Name)
		}
	}
}

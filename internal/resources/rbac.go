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
	"crypto/sha256"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	// LabelAdditionalComponentRBAC marks CRBs created for user-specified
	// additional ClusterRoles (spec.additionalClusterRoles) so the
	// controller can list-and-diff-prune them generically. Renamed from
	// LabelAdditionalAgentRBAC (#277): the mechanism now binds each entry
	// to KA, Gateway, and EM's ServiceAccounts, not just KA's.
	LabelAdditionalComponentRBAC = "kubernaut.ai/additional-component-rbac"

	// LabelCoreClusterRBAC marks every ClusterRole/ClusterRoleBinding
	// returned by ClusterRoles()/ClusterRoleBindings() (#341) so the
	// controller can prune orphans with a single generic label-selector
	// diff instead of a dedicated static-name delete function per
	// conditionally-gated component. Scope is deliberately limited to
	// these two aggregators -- additional-component CRBs, tool RBAC, and
	// console-access's CRB already have their own correct, narrower
	// lifecycle management (their own label-selector sweep or inline
	// nil-checks) and are not part of this label's coverage.
	LabelCoreClusterRBAC = "kubernaut.ai/core-cluster-rbac"

	// LabelMCPGatewayNamespaceRBAC marks every namespace-scoped Role/
	// RoleBinding returned by MCPGatewayNamespaceRBAC() (#354) so the
	// controller can list them across all namespaces and prune any whose
	// namespace no longer matches the current desired (namespace, name)
	// set -- e.g. after an administrator changes a component's effective
	// mcpGatewayNamespace to a different namespace. CommonLabels alone is
	// too broad for this purpose (it's stamped on nearly every operator-
	// managed object), so a dedicated marker is needed the same way
	// LabelCoreClusterRBAC is for cluster-scoped RBAC (#341).
	LabelMCPGatewayNamespaceRBAC = "kubernaut.ai/mcp-gateway-namespace-rbac"

	// LabelValueTrue is the canonical string value for boolean-true labels.
	LabelValueTrue = "true"

	// maxK8sNameLen is the maximum length for a Kubernetes object name.
	maxK8sNameLen = 253
)

// markCoreClusterRBAC stamps LabelCoreClusterRBAC=true onto every object in
// objs, mutating each in place and returning the same slice for chaining.
// Used by ClusterRoles()/ClusterRoleBindings() so every entry they build --
// regardless of which internal helper constructed its Labels map -- carries
// a uniform marker the generic prune pass (#341) can select on.
func markCoreClusterRBAC[T metav1.Object](objs []T) []T {
	for _, o := range objs {
		labels := o.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[LabelCoreClusterRBAC] = LabelValueTrue
		o.SetLabels(labels)
	}
	return objs
}

// clusterRoleName returns a namespace-scoped ClusterRole name to prevent
// collisions when multiple Kubernaut CRs exist in different namespaces.
func clusterRoleName(kn *kubernautv1alpha1.Kubernaut, base string) string {
	return kn.Namespace + "-" + base
}

// ClusterRoles builds all ClusterRoles needed by the Kubernaut deployment,
// matching the Helm chart definitions with namespace-prefixed names. knV2
// supplies Fleet-gated rules (Fleet's entire CRD surface lives in v1alpha2,
// Fleet v1alpha2 migration).
func ClusterRoles(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []*rbacv1.ClusterRole {
	labels := CommonLabels(kn)
	roles := []*rbacv1.ClusterRole{
		aianalysisControllerClusterRole(kn, labels),
		kubernautAgentClientClusterRole(kn, labels),
		kubernautAgentInvestigatorClusterRole(kn, labels),
		signalprocessingClusterRole(kn, knV2, labels),
		remediationOrchestratorClusterRole(kn, labels),
		workflowExecutionControllerClusterRole(kn, labels),
		workflowRunnerClusterRole(kn, labels),
		effectivenessMonitorControllerClusterRole(kn, knV2, labels),
		notificationControllerClusterRole(kn, labels),
		dataStorageAuthMiddlewareClusterRole(kn, labels),
		dataStorageClientClusterRole(kn, labels),
		authWebhookClusterRole(kn, labels),
	}

	if kn.Spec.GatewayEnabled() {
		roles = append(roles, gatewayClusterRole(kn, labels))
	}

	roles = append(roles, alertmanagerViewClusterRole(kn, labels))
	if kn.Spec.GatewayEnabled() {
		roles = append(roles, gatewaySignalSourceClusterRole(kn, labels))
	}

	if kn.Spec.APIFrontendEnabled() {
		roles = append(roles, apifrontendClusterRole(kn, knV2, labels))
		roles = append(roles, ConsoleAccessClusterRole(kn))
	}

	// #224 Finding 5: once the shared spec.fleet.mcpGatewayNamespace
	// resolves (DD-362 -- no per-component override), FMC's MCP Gateway
	// CRD rules move to a namespace-scoped Role instead (see
	// MCPGatewayNamespaceRBAC) -- the cluster-scoped ClusterRole would
	// otherwise be a permission-less no-op, so it's omitted entirely
	// rather than left behind as dead weight.
	if knV2.Spec.FleetMetadataCacheEnabled() && knV2.Spec.Fleet.MCPGatewayNamespace == "" {
		roles = append(roles, fleetMetadataCacheClusterRole(kn, knV2, labels))
	}

	// #1993 (ADR-068 gap closure, IA-2/AC-3/AC-17): the GW/RO -> FMC
	// scope-check REST API previously carried no application-level auth.
	// Unconditional on FMC's mcpGatewayNamespace resolution (unlike the
	// role above) since this is unrelated to MCP Gateway CRD access.
	if knV2.Spec.FleetMetadataCacheEnabled() {
		roles = append(roles,
			fleetMetadataCacheAuthMiddlewareClusterRole(kn, labels),
			fmcScopeCheckClientClusterRole(kn, labels),
		)
	}

	return markCoreClusterRBAC(roles)
}

// ClusterRoleBindings builds all CRBs, binding SAs in the CR namespace.
// All names are namespace-prefixed for multi-instance safety. knV2 supplies
// FleetMetadataCache's gating (Fleet v1alpha2 migration).
func ClusterRoleBindings(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []*rbacv1.ClusterRoleBinding {
	labels := CommonLabels(kn)
	ns := kn.Namespace
	p := func(base string) string { return clusterRoleName(kn, base) }

	crbs := []*rbacv1.ClusterRoleBinding{
		clusterRoleBinding(p("aianalysis-controller-binding"), p("aianalysis-controller"), ServiceAccountName(ComponentAIAnalysis), ns, labels),
		clusterRoleBinding(p("kubernaut-agent-investigator-binding"), p("kubernaut-agent-investigator"), ServiceAccountName(ComponentKubernautAgent), ns, labels),
		clusterRoleBinding(p("kubernaut-agent-auth-middleware-binding"), p("data-storage-auth-middleware"), ServiceAccountName(ComponentKubernautAgent), ns, labels),
		clusterRoleBinding(p("signalprocessing-controller-binding"), p("signalprocessing-controller"), ServiceAccountName(ComponentSignalProcessing), ns, labels),
		clusterRoleBinding(p("remediationorchestrator-controller-binding"), p("remediationorchestrator-controller"), ServiceAccountName(ComponentRemediationOrchestrator), ns, labels),
		clusterRoleBinding(p("workflowexecution-controller-binding"), p("workflowexecution-controller"), ServiceAccountName(ComponentWorkflowExecution), ns, labels),
		clusterRoleBinding(p("effectivenessmonitor-controller-binding"), p("effectivenessmonitor-controller"), ServiceAccountName(ComponentEffectivenessMonitor), ns, labels),
		clusterRoleBinding(p("notification-controller-binding"), p("notification-controller"), ServiceAccountName(ComponentNotification), ns, labels),
		clusterRoleBinding(p("data-storage-auth-middleware-binding"), p("data-storage-auth-middleware"), ServiceAccountName(ComponentDataStorage), ns, labels),
		clusterRoleBinding(p("authwebhook-binding"), p("authwebhook-role"), ServiceAccountName(ComponentAuthWebhook), ns, labels),
	}

	crbs = append(crbs,
		clusterRoleBinding(p("workflow-runner-binding"), p("workflow-runner"),
			"kubernaut-workflow-runner", ResolveWorkflowNamespace(kn), labels),
	)

	if kn.Spec.GatewayEnabled() {
		crbs = append(crbs,
			clusterRoleBinding(p("gateway-role-binding"), p("gateway-role"),
				ServiceAccountName(ComponentGateway), ns, labels),
		)
	}

	crbs = append(crbs,
		clusterRoleBinding(p("effectivenessmonitor-alertmanager-view-binding"), p("alertmanager-view"),
			ServiceAccountName(ComponentEffectivenessMonitor), ns, labels),
		clusterRoleBinding(p("effectivenessmonitor-monitoring-view"), "cluster-monitoring-view",
			ServiceAccountName(ComponentEffectivenessMonitor), ns, labels),
		clusterRoleBinding(p("kubernaut-agent-monitoring-view"), "cluster-monitoring-view",
			ServiceAccountName(ComponentKubernautAgent), ns, labels),
	)
	if kn.Spec.GatewayEnabled() {
		crbs = append(crbs,
			clusterRoleBinding(p("alertmanager-gateway-signal-source"), p("gateway-signal-source"),
				OCPAlertManagerSAName, OCPMonitoringNamespace, labels),
		)
	}
	if kn.Spec.APIFrontendEnabled() {
		crbs = append(crbs,
			clusterRoleBinding(p("apifrontend-monitoring-view"), "cluster-monitoring-view",
				ServiceAccountName(ComponentAPIFrontend), ns, labels),
		)
	}

	if kn.Spec.APIFrontendEnabled() {
		crbs = append(crbs,
			clusterRoleBinding(p("apifrontend-binding"), p("apifrontend-role"),
				ServiceAccountName(ComponentAPIFrontend), ns, labels),
		)
	}

	if knV2.Spec.FleetMetadataCacheEnabled() && knV2.Spec.Fleet.MCPGatewayNamespace == "" {
		crbs = append(crbs, fleetMetadataCacheClusterRoleBinding(kn, labels))
	}

	// #1993: FMC's own auth-middleware binding, plus its two fixed,
	// in-chart scope-check API callers (GW/RO -- mirrors upstream Helm's
	// fmc-scope-check-client-rbac.yaml, which hardcodes these two rather
	// than ranging over a configurable list).
	if knV2.Spec.FleetMetadataCacheEnabled() {
		crbs = append(crbs,
			clusterRoleBinding(p("fleetmetadatacache-auth-middleware-binding"), p("fleetmetadatacache-auth-middleware"),
				ServiceAccountName(ComponentFleetMetadataCache), ns, labels),
		)
		if kn.Spec.GatewayEnabled() {
			crbs = append(crbs,
				clusterRoleBinding(p("gateway-fmc-scope-check-client"), p("fmc-scope-check-client"),
					ServiceAccountName(ComponentGateway), ns, labels),
			)
		}
		crbs = append(crbs,
			clusterRoleBinding(p("remediationorchestrator-fmc-scope-check-client"), p("fmc-scope-check-client"),
				ServiceAccountName(ComponentRemediationOrchestrator), ns, labels),
		)
	}

	return markCoreClusterRBAC(crbs)
}

// DataStorageClientRoleBindings builds the RoleBindings that grant
// data-storage-client ClusterRole access to each consuming SA.
func DataStorageClientRoleBindings(kn *kubernautv1alpha1.Kubernaut) []*rbacv1.RoleBinding {
	labels := CommonLabels(kn)
	ns := kn.Namespace

	consumers := []struct {
		name, sa string
	}{
		{"data-storage-client-aianalysis", ServiceAccountName(ComponentAIAnalysis)},
		{"data-storage-client-signalprocessing", ServiceAccountName(ComponentSignalProcessing)},
		{"data-storage-client-remediationorchestrator", ServiceAccountName(ComponentRemediationOrchestrator)},
		{"data-storage-client-workflowexecution", ServiceAccountName(ComponentWorkflowExecution)},
		{"data-storage-client-effectivenessmonitor", ServiceAccountName(ComponentEffectivenessMonitor)},
		{"data-storage-client-notification", ServiceAccountName(ComponentNotification)},
		{"data-storage-client-kubernaut-agent", ServiceAccountName(ComponentKubernautAgent)},
		{"data-storage-client-authwebhook", ServiceAccountName(ComponentAuthWebhook)},
		{"data-storage-client-datastorage", ServiceAccountName(ComponentDataStorage)},
		{"data-storage-client-apifrontend", ServiceAccountName(ComponentAPIFrontend)},
	}

	if kn.Spec.GatewayEnabled() {
		consumers = append(consumers, struct{ name, sa string }{
			"data-storage-client-gateway", ServiceAccountName(ComponentGateway),
		})
	}

	rbs := make([]*rbacv1.RoleBinding, 0, len(consumers))
	for _, c := range consumers {
		rbs = append(rbs, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      c.name,
				Namespace: ns,
				Labels:    labels,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     clusterRoleName(kn, "data-storage-client"),
			},
			Subjects: []rbacv1.Subject{{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      c.sa,
				Namespace: ns,
			}},
		})
	}
	return rbs
}

// KubernautAgentClientRoleBinding creates a namespace-scoped RoleBinding granting
// the aianalysis SA access to the kubernaut-agent-client ClusterRole.
// Scoped to namespace instead of cluster-wide because the ClusterRole only
// targets a named service.
func KubernautAgentClientRoleBinding(kn *kubernautv1alpha1.Kubernaut) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernaut-agent-client-aianalysis",
			Namespace: kn.Namespace,
			Labels:    CommonLabels(kn),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRoleName(kn, "kubernaut-agent-client"),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      ServiceAccountName(ComponentAIAnalysis),
			Namespace: kn.Namespace,
		}},
	}
}

// KubernautAgentClientAPIfrontendRoleBinding creates a namespace-scoped
// RoleBinding granting the apifrontend SA access to the kubernaut-agent-client
// ClusterRole (trusted intermediary model, #1287).
func KubernautAgentClientAPIfrontendRoleBinding(kn *kubernautv1alpha1.Kubernaut) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernaut-agent-client-apifrontend",
			Namespace: kn.Namespace,
			Labels:    CommonLabels(kn),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRoleName(kn, "kubernaut-agent-client"),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      ServiceAccountName(ComponentAPIFrontend),
			Namespace: kn.Namespace,
		}},
	}
}

// NamespaceRoles builds the namespace-scoped Roles for secrets/configmaps access
// per the kubernaut.nsRoleForSecrets pattern. Access is granted to ALL
// secrets/configmaps in the operator namespace rather than per-resource names
// because the namespace is dedicated to operator workloads and components
// dynamically reference each other's ConfigMaps.
func NamespaceRoles(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []*rbacv1.Role {
	labels := CommonLabels(kn)
	ns := kn.Namespace

	active := ActiveComponents(kn, knV2)
	roles := make([]*rbacv1.Role, 0, len(active))
	for _, c := range active {
		roles = append(roles, &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      c + "-ns-role",
				Namespace: ns,
				Labels:    labels,
			},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"configmaps", "secrets"},
				Verbs:     []string{"get", "list", "watch"},
			}},
		})
	}
	return roles
}

// NamespaceRoleBindings builds the matching RoleBindings for NamespaceRoles.
func NamespaceRoleBindings(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []*rbacv1.RoleBinding {
	labels := CommonLabels(kn)
	ns := kn.Namespace

	active := ActiveComponents(kn, knV2)
	rbs := make([]*rbacv1.RoleBinding, 0, len(active))
	for _, c := range active {
		rbs = append(rbs, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      c + "-ns-rolebinding",
				Namespace: ns,
				Labels:    labels,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "Role",
				Name:     c + "-ns-role",
			},
			Subjects: []rbacv1.Subject{{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      ServiceAccountName(c),
				Namespace: ns,
			}},
		})
	}
	return rbs
}

// WorkflowNamespaceRBAC returns the Roles and RoleBindings in the workflow namespace
// for datastorage-dep-reader and workflowexecution-dep-reader.
func WorkflowNamespaceRBAC(kn *kubernautv1alpha1.Kubernaut) ([]*rbacv1.Role, []*rbacv1.RoleBinding) {
	wfNs := ResolveWorkflowNamespace(kn)
	labels := CommonLabels(kn)
	ns := kn.Namespace

	roles := []*rbacv1.Role{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "datastorage-dep-reader", Namespace: wfNs, Labels: labels},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"secrets", "configmaps"},
				Verbs:     []string{"get"},
			}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "workflowexecution-dep-reader", Namespace: wfNs, Labels: labels},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"secrets", "configmaps"},
				Verbs:     []string{"get", "list", "watch"},
			}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "workflow-runner-ns-writer", Namespace: wfNs, Labels: labels},
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get", "list", "create", "delete", "patch", "update"}},
				{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list", "create", "update", "patch"}},
				{APIGroups: []string{""}, Resources: []string{"services"}, Verbs: []string{"get", "list", "create", "update", "patch"}},
				{APIGroups: []string{""}, Resources: []string{"persistentvolumeclaims"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
				{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
				{APIGroups: []string{"batch"}, Resources: []string{"jobs"}, Verbs: []string{"get", "list", "create", "delete"}},
			},
		},
	}

	rbs := []*rbacv1.RoleBinding{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "datastorage-dep-reader-binding", Namespace: wfNs, Labels: labels},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "datastorage-dep-reader"},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: ServiceAccountName(ComponentDataStorage), Namespace: ns}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "workflowexecution-dep-reader-binding", Namespace: wfNs, Labels: labels},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "workflowexecution-dep-reader"},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: ServiceAccountName(ComponentWorkflowExecution), Namespace: ns}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "workflow-runner-ns-writer-binding", Namespace: wfNs, Labels: labels},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "workflow-runner-ns-writer"},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: "kubernaut-workflow-runner", Namespace: wfNs}},
		},
	}

	return roles, rbs
}

// MCPGatewayNamespaceRBAC returns the namespace-scoped Roles/RoleBindings
// granting MCP Gateway CRD read access to FMC, SignalProcessing, AF, and EM
// when the shared spec.fleet.mcpGatewayNamespace (DD-362 -- no
// per-component override) resolves non-empty (#224 Finding 5). When empty,
// the corresponding cluster-scoped ClusterRole rule in
// fleetMetadataCacheClusterRole/signalprocessingClusterRole/
// apifrontendClusterRole/effectivenessMonitorControllerClusterRole is used
// instead -- callers must not double-grant.
//
// AF/EM were originally excluded here (#224 Finding 4: their upstream
// ClusterRegistry construction always used registry.RegistryConfig{},
// cluster-wide watch, no Namespace field to populate). That blocker closed
// upstream via kubernaut#1720 (confirmed against
// cmd/apifrontend/backend_deps.go and cmd/effectivenessmonitor/main.go,
// both now read registry.RegistryConfig{Namespace: cfg.Fleet.Namespace}),
// so AF/EM are included below on the same terms as FMC/SP (#227).
func MCPGatewayNamespaceRBAC(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) ([]*rbacv1.Role, []*rbacv1.RoleBinding) {
	labels := CommonLabels(kn)
	labels[LabelMCPGatewayNamespaceRBAC] = LabelValueTrue
	ns := kn.Namespace
	rules := mcpGatewayCRDPolicyRules(knV2.Spec.Fleet.MCPGatewayType)

	// One code path shared by FMC, SP, AF, and EM -- all grant the same
	// rules, differing only in whether they're active, their effective
	// namespace, their role-name base, and the ServiceAccount bound to the
	// RoleBinding.
	grants := []struct {
		active    bool
		namespace string
		roleBase  string
		component string
	}{
		{knV2.Spec.FleetMetadataCacheEnabled(), knV2.Spec.Fleet.MCPGatewayNamespace, "fleetmetadatacache-mcpgateway", ComponentFleetMetadataCache},
		{mcpGatewayRemoteReadsEnabled(knV2), knV2.Spec.Fleet.MCPGatewayNamespace, "signalprocessing-mcpgateway", ComponentSignalProcessing},
		{mcpGatewayRemoteReadsEnabled(knV2), knV2.Spec.Fleet.MCPGatewayNamespace, "apifrontend-mcpgateway", ComponentAPIFrontend},
		{mcpGatewayRemoteReadsEnabled(knV2), knV2.Spec.Fleet.MCPGatewayNamespace, "effectivenessmonitor-mcpgateway", ComponentEffectivenessMonitor},
	}

	var roles []*rbacv1.Role
	var rbs []*rbacv1.RoleBinding
	for _, g := range grants {
		if !g.active || g.namespace == "" {
			continue
		}
		roleName := clusterRoleName(kn, g.roleBase)
		roles = append(roles, &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: roleName, Namespace: g.namespace, Labels: labels},
			Rules:      rules,
		})
		rbs = append(rbs, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: roleName + "-binding", Namespace: g.namespace, Labels: labels},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: roleName},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: ServiceAccountName(g.component), Namespace: ns}},
		})
	}

	return roles, rbs
}

// MonitoringCRBNames returns the names of all monitoring-related ClusterRoleBindings.
// Used by the finalizer to always attempt cleanup regardless of current Monitoring.Enabled.
func MonitoringCRBNames(kn *kubernautv1alpha1.Kubernaut) []string {
	p := func(base string) string { return clusterRoleName(kn, base) }
	return []string{
		p("effectivenessmonitor-alertmanager-view-binding"),
		p("effectivenessmonitor-monitoring-view"),
		p("kubernaut-agent-monitoring-view"),
		p("alertmanager-gateway-signal-source"),
		p("apifrontend-monitoring-view"),
	}
}

// MonitoringClusterRoleNames returns the names of all monitoring-related ClusterRoles.
func MonitoringClusterRoleNames(kn *kubernautv1alpha1.Kubernaut) []string {
	p := func(base string) string { return clusterRoleName(kn, base) }
	return []string{
		p("alertmanager-view"),
		p("gateway-signal-source"),
	}
}

// AnsibleRBAC returns the conditional AWX RBAC resources.
func AnsibleRBAC(kn *kubernautv1alpha1.Kubernaut) (*rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding) {
	labels := CommonLabels(kn)
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "workflowexecution-awx"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"awx.ansible.com"},
				Resources: []string{"awxjobs"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}

	wfNs := ResolveWorkflowNamespace(kn)
	crb := clusterRoleBinding(clusterRoleName(kn, "workflowexecution-awx-binding"),
		clusterRoleName(kn, "workflowexecution-awx"),
		"kubernaut-workflow-runner", wfNs, labels)

	return cr, crb
}

// AdditionalComponentCRBName computes a name-safe CRB name for a
// user-provided ClusterRole binding, scoped per component (#277 --
// generalized from KA-only) so the same ClusterRole name can be bound
// independently to KA, Gateway, and EM. If the computed name exceeds the
// K8s 253-char limit, the role-name portion is truncated and a short
// SHA-256 suffix is appended for uniqueness.
func AdditionalComponentCRBName(kn *kubernautv1alpha1.Kubernaut, component, clusterRoleName string) string {
	prefix := kn.Namespace + "-" + component + "-ext-"
	name := prefix + clusterRoleName
	if len(name) <= maxK8sNameLen {
		return name
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(clusterRoleName)))[:8]
	maxRoleLen := maxK8sNameLen - len(prefix) - 1 - 8 // 1 for hyphen before hash
	truncated := clusterRoleName[:maxRoleLen]
	return prefix + truncated + "-" + hash
}

// AdditionalComponentCRB builds a ClusterRoleBinding that binds a
// user-specified ClusterRole to the given component's ServiceAccount (#277
// -- generalized from KA-only to also support Gateway and EM, since none of
// the three components has a legitimate reason to see a different set of
// ecosystem CRDs than the others; they all resolve the same owner-reference
// chains via the same shared spec.additionalClusterRoles list). The CRB is
// labeled with CommonLabels plus a distinctive additional-component-rbac
// label for the generic prune pass.
func AdditionalComponentCRB(kn *kubernautv1alpha1.Kubernaut, component, saName, crName string) *rbacv1.ClusterRoleBinding {
	labels := CommonLabels(kn)
	labels[LabelAdditionalComponentRBAC] = LabelValueTrue

	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   AdditionalComponentCRBName(kn, component, crName),
			Labels: labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     crName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      saName,
			Namespace: kn.Namespace,
		}},
	}
}

// toolPersona defines a persona-based tool role with its allowed tool resourceNames.
type toolPersona struct {
	name          string
	resourceNames []string
}

// toolPersonas lists the 6 upstream persona-based tool roles and their allowed tools,
// aligned with the kubernaut Helm chart values.yaml after PRs #1231, #1233, #1236.
var toolPersonas = []toolPersona{
	{
		// tool-sre carries kubernaut_approve plus approval-visibility/completion tools
		// (kubernaut_get_approval_request, kubernaut_complete_no_action) even though
		// tool-remediation-approver exists as the least-privilege home for approval
		// decisions (see kubernaut#1235/#1239). This is intentional and interim: sre
		// is currently the only console-interactive persona, so it's the only one that
		// can reach a pending approval at all. Revisit once the console gains a
		// management UI that lets remediation-approver log in and act independently
		// (see docs/design/DD-278-sre-approval-visibility.md, #278).
		name: "tool-sre",
		resourceNames: []string{
			"kubernaut_list_remediations", "kubernaut_get_remediation",
			"kubernaut_approve", "kubernaut_cancel_remediation", "kubernaut_watch",
			"kubernaut_await_session", "kubernaut_investigate",
			"kubernaut_discover_workflows", "kubernaut_select_workflow", "kubernaut_present_decision",
			"kubernaut_list_workflows", "kubernaut_get_remediation_history",
			"kubernaut_get_effectiveness", "kubernaut_get_audit_trail",
			"kubernaut_message", "kubernaut_complete",
			"kubernaut_cancel", "kubernaut_status", "kubernaut_reconnect",
			"kubectl_get", "kubectl_list", "kubectl_list_events",
			"kubernaut_check_existing_remediation", "kubernaut_remediate",
			"list_alerts", "get_alert_details", "kubernaut_investigate_alert",
			"kubernaut_get_approval_request", "kubernaut_complete_no_action",
		},
	},
	{
		name: "tool-ai-orchestrator",
		resourceNames: []string{
			"kubernaut_list_remediations", "kubernaut_get_remediation", "kubernaut_watch",
			"kubernaut_await_session", "kubernaut_investigate",
			"kubernaut_discover_workflows", "kubernaut_select_workflow", "kubernaut_present_decision",
			"kubernaut_message", "kubernaut_complete",
			"kubernaut_cancel", "kubernaut_status", "kubernaut_reconnect",
			"kubectl_get", "kubectl_list", "kubectl_list_events",
			"kubernaut_check_existing_remediation", "kubernaut_remediate",
			"list_alerts", "get_alert_details", "kubernaut_investigate_alert",
		},
	},
	{
		name: "tool-cicd",
		resourceNames: []string{
			"kubernaut_list_remediations", "kubernaut_get_remediation", "kubernaut_watch",
			"kubernaut_await_session",
		},
	},
	{
		name: "tool-observability",
		resourceNames: []string{
			"kubernaut_list_remediations", "kubernaut_get_remediation", "kubernaut_watch",
			"kubernaut_await_session",
			"kubernaut_get_effectiveness", "kubernaut_list_workflows",
		},
	},
	{
		name: "tool-l3-audit",
		resourceNames: []string{
			"kubernaut_list_remediations", "kubernaut_get_remediation",
			"kubernaut_list_workflows", "kubernaut_get_remediation_history",
			"kubernaut_get_effectiveness", "kubernaut_get_audit_trail",
		},
	},
	{
		name: "tool-remediation-approver",
		resourceNames: []string{
			"kubernaut_approve", "kubernaut_list_approval_requests",
			"kubernaut_get_approval_request",
			"kubernaut_list_remediations", "kubernaut_get_remediation", "kubernaut_watch",
			"kubernaut_await_session",
		},
	},
}

// ToolClusterRoles builds persona-based tool ClusterRoles for SAR authorization.
// Each role grants verb "use" on resource "tools" in apiGroup "kubernaut.ai"
// with specific resourceNames matching upstream kubernaut tool definitions.
// Returns empty when AF is disabled.
func ToolClusterRoles(kn *kubernautv1alpha1.Kubernaut) []*rbacv1.ClusterRole {
	if !kn.Spec.APIFrontendEnabled() {
		return nil
	}
	labels := CommonLabels(kn)
	roles := make([]*rbacv1.ClusterRole, 0, len(toolPersonas))
	for _, p := range toolPersonas {
		roles = append(roles, &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:   clusterRoleName(kn, p.name),
				Labels: labels,
			},
			Rules: []rbacv1.PolicyRule{{
				APIGroups:     []string{"kubernaut.ai"},
				Resources:     []string{"tools"},
				ResourceNames: p.resourceNames,
				Verbs:         []string{"use"},
			}},
		})
	}
	return roles
}

// ToolClusterRoleNames returns the names of all tool ClusterRoles for finalizer cleanup.
func ToolClusterRoleNames(kn *kubernautv1alpha1.Kubernaut) []string {
	names := make([]string, 0, len(toolPersonas))
	for _, p := range toolPersonas {
		names = append(names, clusterRoleName(kn, p.name))
	}
	return names
}

// ToolClusterRoleBindings builds CRBs that bind tool ClusterRoles to OIDC groups
// as specified in spec.apiFrontend.rbac.roleBindings.
// For built-in persona roles (Role field), the CRB references the operator-managed
// ClusterRole with a namespace-prefixed name.
// For custom ClusterRole references (ClusterRoleName field), the CRB references the
// user-managed ClusterRole directly without namespace-prefixing.
// Duplicate roles are merged: subjects from all entries with the same role are
// combined into a single CRB.
// Returns empty when no roleBindings are specified.
func ToolClusterRoleBindings(kn *kubernautv1alpha1.Kubernaut) []*rbacv1.ClusterRoleBinding {
	if kn.Spec.APIFrontend.RBAC == nil || len(kn.Spec.APIFrontend.RBAC.RoleBindings) == 0 {
		return nil
	}

	type bindingKey struct {
		roleRefName string
		isCustom    bool
	}

	bindings := kn.Spec.APIFrontend.RBAC.RoleBindings
	merged := make(map[string][]string, len(bindings))
	order := make([]bindingKey, 0, len(bindings))
	for _, rb := range bindings {
		var key string
		var isCustom bool
		if rb.ClusterRoleName != "" {
			key = "custom:" + rb.ClusterRoleName
			isCustom = true
		} else {
			key = "persona:" + rb.Role
			isCustom = false
		}
		if _, exists := merged[key]; !exists {
			order = append(order, bindingKey{roleRefName: key, isCustom: isCustom})
		}
		merged[key] = append(merged[key], rb.Groups...)
	}

	labels := CommonLabels(kn)
	crbs := make([]*rbacv1.ClusterRoleBinding, 0, len(merged))
	for _, bk := range order {
		groups := merged[bk.roleRefName]
		subjects := make([]rbacv1.Subject, 0, len(groups))
		for _, g := range groups {
			subjects = append(subjects, rbacv1.Subject{
				Kind: "Group",
				Name: g,
			})
		}

		var crbName, roleRefName string
		if bk.isCustom {
			name := bk.roleRefName[len("custom:"):]
			crbName = clusterRoleName(kn, "tool-custom-"+name+"-binding")
			roleRefName = name
		} else {
			name := bk.roleRefName[len("persona:"):]
			crbName = clusterRoleName(kn, "tool-"+name+"-binding")
			roleRefName = clusterRoleName(kn, "tool-"+name)
		}

		crbs = append(crbs, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:   crbName,
				Labels: labels,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     roleRefName,
			},
			Subjects: subjects,
		})
	}
	return crbs
}

// ToolCRBNames returns the names of all tool CRBs for finalizer cleanup.
func ToolCRBNames(kn *kubernautv1alpha1.Kubernaut) []string {
	crbs := ToolClusterRoleBindings(kn)
	names := make([]string, 0, len(crbs))
	for _, crb := range crbs {
		names = append(names, crb.Name)
	}
	return names
}

// ConsoleAccessClusterRoleName returns the name of the coarse-grained
// console-access ClusterRole for finalizer cleanup.
func ConsoleAccessClusterRoleName(kn *kubernautv1alpha1.Kubernaut) string {
	return clusterRoleName(kn, "console-access")
}

// ConsoleAccessCRBName returns the name of the console-access
// ClusterRoleBinding for finalizer cleanup.
func ConsoleAccessCRBName(kn *kubernautv1alpha1.Kubernaut) string {
	return clusterRoleName(kn, "console-access-binding")
}

// ConsoleAccessClusterRole builds the coarse-grained kubernaut.ai/console
// "use" ClusterRole gating all console/tool access (kubernaut#1919). It
// renders unconditionally whenever AF is enabled, independent of whether
// any groups are currently bound to it -- mirroring upstream's own
// always-render behavior for the equivalent Helm-templated ClusterRole.
// Returns nil when AF is disabled.
func ConsoleAccessClusterRole(kn *kubernautv1alpha1.Kubernaut) *rbacv1.ClusterRole {
	if !kn.Spec.APIFrontendEnabled() {
		return nil
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ConsoleAccessClusterRoleName(kn),
			Labels: CommonLabels(kn),
		},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"kubernaut.ai"},
			Resources: []string{"console"},
			Verbs:     []string{"use"},
		}},
	}
}

// effectiveConsoleAccessGroups returns the OIDC groups granted console
// access. When RBAC.ConsoleAccessGroups is explicitly set (including an
// explicit empty list to opt out), it is used verbatim. When unset (nil),
// it defaults to the deduplicated union of all groups already present in
// RoleBindings, so upgrading to an AF version enforcing the console gate
// does not silently deny existing deployments' tool calls (#289). Nil
// RBAC returns nil (no roleBindings configured at all).
//
// ConsoleAccessGroups deliberately has no `omitempty` JSON tag: Go's
// encoding/json treats a non-nil empty slice as "empty" and would drop it
// from the wire payload on a typed-client Update, collapsing an explicit
// "[]" opt-out back into "unset" (nil) and silently defeating the
// derive-vs-opt-out distinction this function implements. The apiserver
// still treats an explicit JSON null (a nil slice) as field-absent, so the
// nil/unset/derive-default case is unaffected by this.
func effectiveConsoleAccessGroups(kn *kubernautv1alpha1.Kubernaut) []string {
	if kn.Spec.APIFrontend.RBAC == nil {
		return nil
	}
	if kn.Spec.APIFrontend.RBAC.ConsoleAccessGroups != nil {
		return kn.Spec.APIFrontend.RBAC.ConsoleAccessGroups
	}

	roleBindings := kn.Spec.APIFrontend.RBAC.RoleBindings
	seen := make(map[string]struct{}, len(roleBindings))
	groups := make([]string, 0, len(roleBindings))
	for _, rb := range roleBindings {
		for _, g := range rb.Groups {
			if _, ok := seen[g]; ok {
				continue
			}
			seen[g] = struct{}{}
			groups = append(groups, g)
		}
	}
	return groups
}

// ConsoleAccessClusterRoleBinding builds the ClusterRoleBinding granting
// effectiveConsoleAccessGroups the console-access ClusterRole. Returns nil
// when AF is disabled or the effective group list is empty (no groups to
// bind, or an explicit opt-out) -- callers must delete any previously
// created CRB in that case rather than treating nil as "no-op".
func ConsoleAccessClusterRoleBinding(kn *kubernautv1alpha1.Kubernaut) *rbacv1.ClusterRoleBinding {
	if !kn.Spec.APIFrontendEnabled() {
		return nil
	}
	groups := effectiveConsoleAccessGroups(kn)
	if len(groups) == 0 {
		return nil
	}

	subjects := make([]rbacv1.Subject, 0, len(groups))
	for _, g := range groups {
		subjects = append(subjects, rbacv1.Subject{
			Kind: "Group",
			Name: g,
		})
	}

	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ConsoleAccessCRBName(kn),
			Labels: CommonLabels(kn),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     ConsoleAccessClusterRoleName(kn),
		},
		Subjects: subjects,
	}
}

// ownerChainResolutionRules returns the read-only PolicyRules needed for
// traversing Kubernetes owner-reference chains. Used by Gateway and EM to
// correlate workloads with their higher-order controllers.
//
// #277: shrunk to only genuinely universal, non-ecosystem-specific kinds
// (PDB + core networking). This previously also unconditionally granted
// read access to OLM, Istio, cert-manager, ArgoCD, OpenShift Routes, and
// KubeVirt/CDI CRDs regardless of whether a cluster actually ran any of
// those ecosystems -- an unbounded, ever-growing list (every new ecosystem
// needed a code change here) that also violated least-privilege (SC-7/AC-6)
// by forcing permission surface onto clusters that don't run them. Clusters
// that do run one of those ecosystems and want Kubernaut's owner-chain
// correlation to see it now supply their own ClusterRole and reference its
// name via spec.additionalClusterRoles -- the same mechanism KA already
// used pre-#277, generalized to Gateway/EM too. See
// docs/architecture/decisions (kubernaut DD-GATEWAY-018) for the full
// alternatives analysis.
func ownerChainResolutionRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies", "ingresses"}, Verbs: []string{"get", "list", "watch"}},
	}
}

// --- private helpers ---

func clusterRoleBinding(name, roleName, saName, saNamespace string, labels map[string]string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      saName,
			Namespace: saNamespace,
		}},
	}
}

// --- ClusterRole definitions (namespace-prefixed for multi-instance safety) ---

func gatewayClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	ocr := ownerChainResolutionRules()
	rules := make([]rbacv1.PolicyRule, 0, 10+len(ocr)) //nolint:mnd
	rules = append(rules, []rbacv1.PolicyRule{
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests"}, Verbs: []string{"create", "get", "list", "watch", "update", "patch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests/status"}, Verbs: []string{"update", "patch"}},
		{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"nodes", "pods", "services", "persistentvolumes", "persistentvolumeclaims"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "create", "update", "delete"}},
		{APIGroups: []string{"authentication.k8s.io"}, Resources: []string{"tokenreviews"}, Verbs: []string{"create"}},
		{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"subjectaccessreviews"}, Verbs: []string{"create"}},
	}...)
	rules = append(rules, ocr...)
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "gateway-role"), Labels: labels},
		Rules:      rules,
	}
}

func aianalysisControllerClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "aianalysis-controller"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"aianalyses"}, Verbs: []string{"get", "list", "watch", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"aianalyses/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"investigationsessions"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"investigationsessions/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
		},
	}
}

func kubernautAgentClientClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "kubernaut-agent-client"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{"kubernaut-agent"}, Verbs: []string{"create", "get"}},
		},
	}
}

func kubernautAgentInvestigatorClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	rules := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"pods", "pods/log", "services", "endpoints", "configmaps", "secrets", "nodes", "namespaces", "replicationcontrollers", "persistentvolumeclaims", "persistentvolumes", "resourcequotas", "serviceaccounts"}, Verbs: []string{"get", "list", "watch"}},
		// Kubelet proxy API access for nodes_log and nodes_stats_summary tools (#205).
		{APIGroups: []string{""}, Resources: []string{"nodes/proxy"}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"get", "list", "watch", "create", "patch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"storageclasses", "csidrivers", "volumeattachments", "csinodes"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"events.k8s.io"}, Resources: []string{"events"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"discovery.k8s.io"}, Resources: []string{"endpointslices"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies", "ingresses"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"cert-manager.io"}, Resources: []string{"certificates", "clusterissuers", "certificaterequests"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"argoproj.io"}, Resources: []string{"applications"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"policy.linkerd.io"}, Resources: []string{"servers", "authorizationpolicies", "meshtlsauthentications"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"security.istio.io"}, Resources: []string{"authorizationpolicies", "peerauthentications", "requestauthentications"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"networking.istio.io"}, Resources: []string{"virtualservices", "destinationrules", "gateways", "serviceentries"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"monitoring.coreos.com"}, Resources: []string{"prometheusrules", "servicemonitors", "podmonitors", "probes"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"metrics.k8s.io"}, Resources: []string{"pods", "nodes"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"config.openshift.io"}, Resources: []string{"nodes", "clusteroperators", "clusterversions", "infrastructures"}, Verbs: []string{"get", "list", "watch"}},
		// OCP: OLM
		{APIGroups: []string{"operators.coreos.com"}, Resources: []string{"clusterserviceversions", "subscriptions", "installplans", "operatorgroups", "catalogsources", "operatorconditions"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"packages.operators.coreos.com"}, Resources: []string{"packagemanifests"}, Verbs: []string{"get", "list", "watch"}},
		// OCP: networking, builds, images, legacy apps, SCCs
		{APIGroups: []string{"route.openshift.io"}, Resources: []string{"routes"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"apps.openshift.io"}, Resources: []string{"deploymentconfigs"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"security.openshift.io"}, Resources: []string{"securitycontextconstraints"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"image.openshift.io"}, Resources: []string{"imagestreams", "imagestreamtags"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"build.openshift.io"}, Resources: []string{"builds", "buildconfigs"}, Verbs: []string{"get", "list", "watch"}},
		// OCP: machine management
		{APIGroups: []string{"machine.openshift.io"}, Resources: []string{"machines", "machinesets", "machinehealthchecks"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"machineconfiguration.openshift.io"}, Resources: []string{"machineconfigs", "machineconfigpools"}, Verbs: []string{"get", "list", "watch"}},
		// OCP: quotas, network operator
		{APIGroups: []string{"quota.openshift.io"}, Resources: []string{"clusterresourcequotas", "appliedclusterresourcequotas"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"network.operator.openshift.io"}, Resources: []string{"operatorpkis", "egressrouters"}, Verbs: []string{"get", "list", "watch"}},
		// Core K8s: RBAC, admission, CRDs, scheduling
		{APIGroups: []string{"rbac.authorization.k8s.io"}, Resources: []string{"roles", "rolebindings", "clusterroles", "clusterrolebindings"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"admissionregistration.k8s.io"}, Resources: []string{"mutatingwebhookconfigurations", "validatingwebhookconfigurations"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"apiextensions.k8s.io"}, Resources: []string{"customresourcedefinitions"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"scheduling.k8s.io"}, Resources: []string{"priorityclasses"}, Verbs: []string{"get", "list", "watch"}},
		// Kubernaut CRDs: the agent needs read access to list remediations, investigations, etc.
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{
			"remediationrequests", "remediationworkflows", "remediationapprovalrequests",
			"investigationsessions", "aianalyses", "signalprocessings",
			"effectivenessassessments", "workflowexecutions", "actiontypes",
		}, Verbs: []string{"get", "list", "watch"}},
		// Interactive session locking via Leases (v1.5+); list needed for ReconcileOrphanedLeases
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "create", "update", "delete", "list"}},
		// KubeVirt / CDI: VM investigation and enrichment
		{APIGroups: []string{"kubevirt.io"}, Resources: []string{"virtualmachines", "virtualmachineinstances", "virtualmachineinstancemigrations"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"cdi.kubevirt.io"}, Resources: []string{"datavolumes"}, Verbs: []string{"get", "list", "watch"}},
	}

	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "kubernaut-agent-investigator"), Labels: labels},
		Rules:      rules,
	}
}

// mcpGatewayCRDPolicyRules returns the gatewayType-conditional PolicyRules
// granting read access to the MCP Gateway CRDs (Backend/MCPRoute for Envoy
// AI Gateway, MCPServerRegistration/Gateway/HTTPRoute for Kuadrant) that
// represent managed clusters (#224). Extracted from FMC's original,
// unconditional rule set (#200) so SP/AF/EM's ClusterRoles -- and
// MCPGatewayNamespaceRBAC's namespace-scoped Role variant for FMC/SP --
// can share the exact same rules rather than re-deriving them.
// mcpGatewayTypeKuadrant is spec.fleet.mcpGatewayType's Kuadrant value
// (validated against validMCPGatewayTypes in validation.go).
const mcpGatewayTypeKuadrant = "kuadrant"

func mcpGatewayCRDPolicyRules(gatewayType string) []rbacv1.PolicyRule {
	if gatewayType == mcpGatewayTypeKuadrant {
		return []rbacv1.PolicyRule{
			{APIGroups: []string{"mcp.kuadrant.io"}, Resources: []string{"mcpserverregistrations"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"gateway.networking.k8s.io"}, Resources: []string{"gateways", "httproutes"}, Verbs: []string{"get", "list", "watch"}},
		}
	}
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"gateway.envoyproxy.io"}, Resources: []string{"backends"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"aigateway.envoyproxy.io"}, Resources: []string{"mcproutes"}, Verbs: []string{"get", "list", "watch"}},
	}
}

// mcpGatewayRemoteReadsEnabled reports whether a component should construct
// a ClusterRegistry for MCP Gateway remote-cluster reads (BR-FLEET-003,
// BR-INTEGRATION-054): fleet enabled and an MCP Gateway endpoint is
// configured. Shared gating condition confirmed against
// cmd/signalprocessing/main.go, cmd/apifrontend/backend_deps.go, and
// cmd/effectivenessmonitor/main.go (preflight Finding 4).
func mcpGatewayRemoteReadsEnabled(knV2 *kubernautv1alpha2.Kubernaut) bool {
	fleet := &knV2.Spec.Fleet
	return fleet.Enabled != nil && *fleet.Enabled && fleet.MCPGatewayEndpoint != ""
}

func signalprocessingClusterRole(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	rules := []rbacv1.PolicyRule{
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"signalprocessings", "remediationrequests"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"signalprocessings/status", "signalprocessings/finalizers"}, Verbs: []string{"get", "update", "patch"}},
		{APIGroups: []string{""}, Resources: []string{"pods", "services", "namespaces", "nodes"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
	}
	// #224: grant MCP Gateway CRD read access here only when the shared
	// spec.fleet.mcpGatewayNamespace (DD-362) is empty; a resolved
	// namespace moves these rules to a namespace-scoped Role instead (see
	// MCPGatewayNamespaceRBAC).
	if mcpGatewayRemoteReadsEnabled(knV2) && knV2.Spec.Fleet.MCPGatewayNamespace == "" {
		rules = append(rules, mcpGatewayCRDPolicyRules(knV2.Spec.Fleet.MCPGatewayType)...)
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "signalprocessing-controller"), Labels: labels},
		Rules:      rules,
	}
}

func remediationOrchestratorClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "remediationorchestrator-controller"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests", "remediationapprovalrequests"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests/status", "remediationapprovalrequests/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests/finalizers"}, Verbs: []string{"update"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"signalprocessings"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"signalprocessings/status"}, Verbs: []string{"get"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"aianalyses"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"aianalyses/status"}, Verbs: []string{"get"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions/status"}, Verbs: []string{"get"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"notificationrequests"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"notificationrequests/status"}, Verbs: []string{"get"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"effectivenessassessments"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"effectivenessassessments/status"}, Verbs: []string{"get"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"pods", "nodes", "services", "namespaces", "persistentvolumes", "configmaps"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
}

func workflowExecutionControllerClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "workflowexecution-controller"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions/finalizers"}, Verbs: []string{"update"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"pipelineruns"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"taskruns"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"batch"}, Resources: []string{"jobs"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"serviceaccounts/token"}, Verbs: []string{"create"}},
			{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		},
	}
}

// workflowRunnerClusterRole contains only cluster-wide read access and CRD
// operations. Write access to secrets, configmaps, PVCs, etc. is scoped to
// the workflow namespace via workflowRunnerNamespaceRole (see WorkflowNamespaceRBAC).
func workflowRunnerClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "workflow-runner"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"apps"}, Resources: []string{"deployments", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "patch", "update"}},
			{APIGroups: []string{"apps"}, Resources: []string{"replicasets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "delete", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"pods/eviction"}, Verbs: []string{"create"}},
			{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get", "list", "patch", "update"}},
			{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list", "patch"}},
			{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"serviceaccounts/token"}, Verbs: []string{"create"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions"}, Verbs: []string{"get"}},
			{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"storageclasses"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{""}, Resources: []string{"endpoints"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"argoproj.io"}, Resources: []string{"applications"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"cert-manager.io"}, Resources: []string{"certificates"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"cert-manager.io"}, Resources: []string{"clusterissuers"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"policy.linkerd.io"}, Resources: []string{"authorizationpolicies", "servers", "meshtlsauthentications"}, Verbs: []string{"get", "list", "delete"}},
			{APIGroups: []string{"security.istio.io"}, Resources: []string{"authorizationpolicies", "peerauthentications", "requestauthentications"}, Verbs: []string{"get", "list", "delete"}},
			{APIGroups: []string{"networking.istio.io"}, Resources: []string{"virtualservices", "destinationrules", "gateways", "serviceentries"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
		},
	}
}

func effectivenessMonitorControllerClusterRole(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	ocr := ownerChainResolutionRules()
	rules := make([]rbacv1.PolicyRule, 0, 9+len(ocr)) //nolint:mnd
	rules = append(rules, []rbacv1.PolicyRule{
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"effectivenessassessments"}, Verbs: []string{"get", "list", "watch", "update", "patch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"effectivenessassessments/status"}, Verbs: []string{"get", "update", "patch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"pods", "nodes", "services", "persistentvolumeclaims", "configmaps"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
	}...)
	rules = append(rules, ocr...)
	// #227: EM's upstream ClusterRegistry now reads registry.RegistryConfig{
	// Namespace: cfg.Fleet.Namespace} (kubernaut#1720, closing #224 Finding
	// 4's kubernaut#1686 blocker), so the cluster-scoped MCP Gateway CRD
	// rules are only needed while the shared spec.fleet.mcpGatewayNamespace
	// (DD-362) is unresolved -- once it resolves, MCPGatewayNamespaceRBAC
	// grants a namespace-scoped Role instead (mirroring FMC/SP).
	if mcpGatewayRemoteReadsEnabled(knV2) && knV2.Spec.Fleet.MCPGatewayNamespace == "" {
		rules = append(rules, mcpGatewayCRDPolicyRules(knV2.Spec.Fleet.MCPGatewayType)...)
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "effectivenessmonitor-controller"), Labels: labels},
		Rules:      rules,
	}
}

func notificationControllerClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "notification-controller"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"notificationrequests"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"notificationrequests/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"notificationrequests/finalizers"}, Verbs: []string{"update"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
		},
	}
}

func dataStorageAuthMiddlewareClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "data-storage-auth-middleware"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"authentication.k8s.io"}, Resources: []string{"tokenreviews"}, Verbs: []string{"create"}},
			{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"subjectaccessreviews"}, Verbs: []string{"create"}},
		},
	}
}

func dataStorageClientClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "data-storage-client"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{"data-storage-service"}, Verbs: []string{"create", "get", "list", "update", "delete"}},
		},
	}
}

// fleetMetadataCacheAuthMiddlewareClusterRole grants FMC's own SA the
// TokenReview/SAR permissions its auth middleware needs to authenticate
// GW/RO callers of its scope-check REST API (#1993, ADR-068 gap closure).
// Mirrors dataStorageAuthMiddlewareClusterRole.
func fleetMetadataCacheAuthMiddlewareClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "fleetmetadatacache-auth-middleware"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"authentication.k8s.io"}, Resources: []string{"tokenreviews"}, Verbs: []string{"create"}},
			{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"subjectaccessreviews"}, Verbs: []string{"create"}},
		},
	}
}

// fmcScopeCheckClientClusterRole grants a synthetic "get" permission on
// FMC's Service, used purely as the SubjectAccessReview target FMC's auth
// middleware checks against callers of its scope-check API (#1993).
// Structurally mirrors dataStorageClientClusterRole/gatewaySignalSourceClusterRole.
func fmcScopeCheckClientClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "fmc-scope-check-client"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{fleetMetadataCacheServiceName}, Verbs: []string{"get"}},
		},
	}
}

func authWebhookClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "authwebhook-role"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions", "remediationapprovalrequests", "notificationrequests", "remediationrequests", "actiontypes"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationworkflows"}, Verbs: []string{"get", "list", "watch", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationworkflows/finalizers"}, Verbs: []string{"update"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions/status", "remediationapprovalrequests/status", "remediationrequests/status", "remediationworkflows/status", "actiontypes/status"}, Verbs: []string{"update", "patch"}},
			{APIGroups: []string{"", "events.k8s.io"}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
		},
	}
}

func alertmanagerViewClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "alertmanager-view"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"monitoring.coreos.com"}, Resources: []string{"alertmanagers/api"}, Verbs: []string{"get"}},
		},
	}
}

func gatewaySignalSourceClusterRole(kn *kubernautv1alpha1.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "gateway-signal-source"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{"gateway-service"}, Verbs: []string{"create"}},
		},
	}
}

func apifrontendClusterRole(kn *kubernautv1alpha1.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	rules := []rbacv1.PolicyRule{
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"investigationsessions"}, Verbs: []string{"get", "list", "watch", "create", "update", "delete"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"investigationsessions/status"}, Verbs: []string{"get", "update"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests/status"}, Verbs: []string{"get", "update", "patch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationapprovalrequests"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationapprovalrequests/status"}, Verbs: []string{"get", "update", "patch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"aianalyses"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"effectivenessassessments"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"subjectaccessreviews"}, Verbs: []string{"create"}},
		{APIGroups: []string{"authentication.k8s.io"}, Resources: []string{"tokenreviews"}, Verbs: []string{"create"}},
		// KA DD-AUTH-014 SAR gate: AF SA must be able to "create" on services/kubernaut-agent
		{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{"kubernaut-agent"}, Verbs: []string{"create"}},
		// kubectl_list_events
		{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"get", "list", "create", "patch"}},
		// kubectl_get / kubectl_list triage tools (AF SA reads cluster state)
		{APIGroups: []string{""}, Resources: []string{"pods", "replicationcontrollers", "services", "configmaps", "secrets", "endpoints", "namespaces", "nodes", "persistentvolumeclaims"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"ingresses", "networkpolicies"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"cert-manager.io"}, Resources: []string{"certificates"}, Verbs: []string{"get", "list"}},
		// KubeVirt / CDI: VM triage and investigation
		{APIGroups: []string{"kubevirt.io"}, Resources: []string{"virtualmachines", "virtualmachineinstances", "virtualmachineinstancemigrations"}, Verbs: []string{"get", "list"}},
		{APIGroups: []string{"cdi.kubevirt.io"}, Resources: []string{"datavolumes"}, Verbs: []string{"get", "list"}},
	}
	// #227: AF's upstream ClusterRegistry now reads registry.RegistryConfig{
	// Namespace: cfg.Fleet.Namespace} (kubernaut#1720, closing #224 Finding
	// 4's kubernaut#1686 blocker), so the cluster-scoped MCP Gateway CRD
	// rules are only needed while the shared spec.fleet.mcpGatewayNamespace
	// (DD-362) is unresolved -- once it resolves, MCPGatewayNamespaceRBAC
	// grants a namespace-scoped Role instead (mirroring FMC/SP).
	if mcpGatewayRemoteReadsEnabled(knV2) && knV2.Spec.Fleet.MCPGatewayNamespace == "" {
		rules = append(rules, mcpGatewayCRDPolicyRules(knV2.Spec.Fleet.MCPGatewayType)...)
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "apifrontend-role"), Labels: labels},
		Rules:      rules,
	}
}

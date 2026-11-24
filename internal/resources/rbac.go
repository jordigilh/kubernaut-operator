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

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

const (
	// FleetReadGroup is the Kubernetes group emitted by the OIDC provider for
	// read-only Fleet callers. The "oidc:" prefix is part of the Kubernetes
	// group identity because the supported OpenShift/OIDC configuration uses
	// that fixed group-claim prefix (see docs/installation/05-fleet-multi-cluster.md).
	FleetReadGroup = "oidc:kubernaut-fleet-read"

	// FleetExecutionGroup is the Kubernetes group emitted by the OIDC provider
	// for the dedicated WorkflowExecution caller identity. WorkflowExecution
	// must be a member of both FleetReadGroup and FleetExecutionGroup; other
	// Fleet callers should only be members of FleetReadGroup.
	FleetExecutionGroup = "oidc:kubernaut-fleet-workflow-execution"

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
	// these two aggregators -- additional-component CRBs have their own
	// correct, narrower lifecycle management (their own label-selector
	// sweep) and are not part of this label's coverage. Since #682/#684
	// (roles-only model) no user-facing RBAC exists outside these
	// aggregators (the Fleet caller protocol groups excepted -- fixed
	// machine identities, see DD-682).
	LabelCoreClusterRBAC = "kubernaut.ai/core-cluster-rbac"

	// LabelMCPGatewayNamespaceRBAC marks every namespace-scoped Role/
	// RoleBinding returned by MCPGatewayNamespaceRBAC() (#354) so the
	// controller can list them across all namespaces and prune any whose
	// namespace no longer matches the current desired (namespace, name)
	// set -- e.g. after an administrator changes a component's effective
	// MCP Gateway namespace to a different namespace. CommonLabels alone is
	// too broad for this purpose (it's stamped on nearly every operator-
	// managed object), so a dedicated marker is needed the same way
	// LabelCoreClusterRBAC is for cluster-scoped RBAC (#341).
	LabelMCPGatewayNamespaceRBAC = "kubernaut.ai/mcp-gateway-namespace-rbac"

	// LabelValueTrue is the canonical string value for boolean-true labels.
	LabelValueTrue = "true"

	// maxK8sNameLen is the maximum length for a Kubernetes object name.
	maxK8sNameLen = 253

	trustedWorkflowCatalogAPIFrontendRoleBase = "apifrontend-workflow-catalog"
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
func clusterRoleName(kn *kubernautv1alpha2.Kubernaut, base string) string {
	return kn.Namespace + "-" + base
}

// ClusterRoles builds all ClusterRoles needed by the Kubernaut deployment,
// matching the Helm chart definitions with namespace-prefixed names. knV2
// supplies Fleet-gated rules (Fleet's entire CRD surface lives in v1alpha2,
// Fleet v1alpha2 migration).
func ClusterRoles(kn *kubernautv1alpha2.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []*rbacv1.ClusterRole {
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
		// #636: console REST read projection capabilities. Rendered but
		// deliberately unbound -- the console-access (login) grant must not
		// imply action authorization. #662/DD-662 made this the permanent
		// model: no persona carries these capabilities. #682/#684 made the
		// model roles-only: the operator renders the role and never any
		// user-facing ClusterRoleBinding; binding is an explicit
		// administrator choice through an external ClusterRoleBinding.
		roles = append(roles, ConsoleReaderClusterRole(kn))
		// #663: user-side local-target get/list for the console inventory
		// projection. Rendered but deliberately unbound, exactly like
		// console-reader -- which users may read the local target stays an
		// administrator decision (#682/#684 roles-only model).
		roles = append(roles, ConsoleInventoryReaderClusterRole(kn))
	}

	// #224 Finding 5: once the shared spec.fleet.mcpGateway.namespace
	// resolves (DD-362 -- no per-component override), FMC's MCP Gateway
	// CRD rules move to a namespace-scoped Role instead (see
	// MCPGatewayNamespaceRBAC) -- the cluster-scoped ClusterRole would
	// otherwise be a permission-less no-op, so it's omitted entirely
	// rather than left behind as dead weight.
	if knV2.Spec.FleetMetadataCacheEnabled() && knV2.Spec.Fleet.MCPGateway.Namespace == "" {
		roles = append(roles, fleetMetadataCacheClusterRole(kn, knV2, labels))
	}

	// #1993 (ADR-068 gap closure, IA-2/AC-3/AC-17): the GW/RO -> FMC
	// scope-check REST API previously carried no application-level auth.
	// Unconditional on FMC's MCP Gateway namespace resolution (unlike the
	// role above) since this is unrelated to MCP Gateway CRD access.
	if knV2.Spec.FleetMetadataCacheEnabled() {
		roles = append(roles,
			fleetMetadataCacheAuthMiddlewareClusterRole(kn, labels),
			fmcScopeCheckClientClusterRole(kn, labels),
		)
	}

	if fleetCallerRBACEnabled(knV2) {
		roles = append(roles,
			fleetCallerNodeReaderClusterRole(kn, labels),
			fleetCallerWorkflowExecutionClusterRole(kn, labels),
		)
	}

	return markCoreClusterRBAC(roles)
}

// ClusterRoleBindings builds all CRBs, binding service accounts in the CR
// namespace and, when remote Fleet access is enabled, the fixed Fleet caller
// groups. All operator-generated names are namespace-prefixed for
// multi-instance safety. knV2 supplies FleetMetadataCache's gating (Fleet
// v1alpha2 migration).
func ClusterRoleBindings(kn *kubernautv1alpha2.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []*rbacv1.ClusterRoleBinding {
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

	crbs = append(crbs, monitoringClusterRoleBindings(kn, labels)...)

	if kn.Spec.APIFrontendEnabled() {
		crbs = append(crbs,
			clusterRoleBinding(p("apifrontend-binding"), p("apifrontend-role"),
				ServiceAccountName(ComponentAPIFrontend), ns, labels),
		)
	}

	if knV2.Spec.FleetMetadataCacheEnabled() && knV2.Spec.Fleet.MCPGateway.Namespace == "" {
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

	if fleetCallerRBACEnabled(knV2) {
		crbs = append(crbs,
			groupClusterRoleBinding(p("fleet-caller-read-view-binding"), "view", FleetReadGroup, labels),
			groupClusterRoleBinding(p("fleet-caller-read-node-reader-binding"), p("fleet-caller-node-reader"), FleetReadGroup, labels),
			groupClusterRoleBinding(p("fleet-caller-execution-binding"), p("fleet-caller-workflow-execution"), FleetExecutionGroup, labels),
		)
	}

	return markCoreClusterRBAC(crbs)
}

// monitoringClusterRoleBindings builds the CRBs granting monitoring-stack
// access (Alertmanager/Thanos-Querier) to the components that poll it
// directly. Extracted from ClusterRoleBindings (#468) to keep that
// function under the funlen budget.
func monitoringClusterRoleBindings(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) []*rbacv1.ClusterRoleBinding {
	ns := kn.Namespace
	p := func(base string) string { return clusterRoleName(kn, base) }

	crbs := []*rbacv1.ClusterRoleBinding{
		clusterRoleBinding(p("effectivenessmonitor-alertmanager-view-binding"), p("alertmanager-view"),
			ServiceAccountName(ComponentEffectivenessMonitor), ns, labels),
		clusterRoleBinding(p("effectivenessmonitor-monitoring-view"), "cluster-monitoring-view",
			ServiceAccountName(ComponentEffectivenessMonitor), ns, labels),
		clusterRoleBinding(p("kubernaut-agent-monitoring-view"), "cluster-monitoring-view",
			ServiceAccountName(ComponentKubernautAgent), ns, labels),
		// #468: KA's get_alerts/get_silences tools
		// (pkg/kubernautagent/tools/alertmanager) call alertmanagers/api
		// directly, the same permission EM's binding above grants --
		// cluster-monitoring-view only covers prometheuses/api.
		clusterRoleBinding(p("kubernaut-agent-alertmanager-view-binding"), p("alertmanager-view"),
			ServiceAccountName(ComponentKubernautAgent), ns, labels),
	}

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

	return crbs
}

// DataStorageClientRoleBindings builds the RoleBindings that grant
// data-storage-client ClusterRole access to each consuming SA.
func DataStorageClientRoleBindings(kn *kubernautv1alpha2.Kubernaut) []*rbacv1.RoleBinding {
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

// KubernautAgentClientAPIfrontendRoleBinding creates a namespace-scoped
// RoleBinding granting the apifrontend SA access to the kubernaut-agent-client
// ClusterRole (trusted intermediary model, #1287).
func KubernautAgentClientAPIfrontendRoleBinding(kn *kubernautv1alpha2.Kubernaut) *rbacv1.RoleBinding {
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
func NamespaceRoles(kn *kubernautv1alpha2.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []*rbacv1.Role {
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
func NamespaceRoleBindings(kn *kubernautv1alpha2.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) []*rbacv1.RoleBinding {
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

type trustedWorkflowCatalogGrant struct {
	roleBase  string
	component string
	resources []string
	verbs     []string
}

var trustedWorkflowCatalogGrants = []trustedWorkflowCatalogGrant{
	{
		roleBase:  "kubernaut-agent-workflow-catalog",
		component: ComponentKubernautAgent,
		resources: []string{"remediationworkflows", "actiontypes"},
		verbs:     []string{"list", "watch"},
	},
	{
		roleBase:  "authwebhook-workflow-catalog",
		component: ComponentAuthWebhook,
		resources: []string{"remediationworkflows"},
		verbs:     []string{"list"},
	},
	{
		roleBase:  "workflowexecution-workflow-catalog",
		component: ComponentWorkflowExecution,
		resources: []string{"remediationworkflows"},
		verbs:     []string{"list"},
	},
	{
		roleBase:  "aianalysis-workflow-catalog",
		component: ComponentAIAnalysis,
		resources: []string{"remediationworkflows"},
		verbs:     []string{"list"},
	},
	{
		roleBase:  "remediationorchestrator-workflow-catalog",
		component: ComponentRemediationOrchestrator,
		resources: []string{"remediationworkflows"},
		verbs:     []string{"get", "list"},
	},
	{
		// APIFrontend's approval workflow identity validation is read-only and namespaced (#634).
		roleBase:  trustedWorkflowCatalogAPIFrontendRoleBase,
		component: ComponentAPIFrontend,
		resources: []string{"remediationworkflows"},
		verbs:     []string{"get", "list"},
	},
}

// TrustedWorkflowCatalogRBAC returns namespace-scoped catalog permissions for
// the controller/service namespace (#610; decision documented in
// docs/design/ISSUE-610-TRUSTED-WORKFLOW-CATALOG-RBAC.md). The
// workflow-execution namespace is a separate concern: catalog discovery and
// trusted-workflow verification must use kn.Namespace regardless of
// spec.workflowExecution.workflowNamespace.
func TrustedWorkflowCatalogRBAC(kn *kubernautv1alpha2.Kubernaut) ([]*rbacv1.Role, []*rbacv1.RoleBinding) {
	labels := CommonLabels(kn)
	roles := make([]*rbacv1.Role, 0, len(trustedWorkflowCatalogGrants))
	bindings := make([]*rbacv1.RoleBinding, 0, len(trustedWorkflowCatalogGrants))
	for _, grant := range trustedWorkflowCatalogGrants {
		if grant.component == ComponentAPIFrontend && !kn.Spec.APIFrontendEnabled() {
			continue
		}
		roleName := clusterRoleName(kn, grant.roleBase)
		roles = append(roles, &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: kn.Namespace,
				Labels:    labels,
			},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{kubernautv1alpha2.GroupVersion.Group},
				Resources: grant.resources,
				Verbs:     grant.verbs,
			}},
		})
		bindings = append(bindings, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName + "-binding",
				Namespace: kn.Namespace,
				Labels:    labels,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "Role",
				Name:     roleName,
			},
			Subjects: []rbacv1.Subject{{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      ServiceAccountName(grant.component),
				Namespace: kn.Namespace,
			}},
		})
	}
	return roles, bindings
}

// APIFrontendWorkflowCatalogRBACNames returns the names of the optional
// APIFrontend trusted-workflow catalog Role and RoleBinding. The names are
// available even when APIFrontend is disabled so reconciliation can remove a
// grant left by an enabled-to-disabled transition.
func APIFrontendWorkflowCatalogRBACNames(kn *kubernautv1alpha2.Kubernaut) (string, string) {
	roleName := clusterRoleName(kn, trustedWorkflowCatalogAPIFrontendRoleBase)
	return roleName, roleName + "-binding"
}

// WorkflowNamespaceRBAC returns the Roles and RoleBindings in the workflow namespace
// for datastorage-dep-reader and workflowexecution-dep-reader.
func WorkflowNamespaceRBAC(kn *kubernautv1alpha2.Kubernaut) ([]*rbacv1.Role, []*rbacv1.RoleBinding) {
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
// when the shared spec.fleet.mcpGateway.namespace (DD-362 -- no
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
func MCPGatewayNamespaceRBAC(kn *kubernautv1alpha2.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut) ([]*rbacv1.Role, []*rbacv1.RoleBinding) {
	labels := CommonLabels(kn)
	labels[LabelMCPGatewayNamespaceRBAC] = LabelValueTrue
	ns := kn.Namespace
	rules := mcpGatewayCRDPolicyRules(knV2.Spec.Fleet.MCPGateway.Type)

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
		{knV2.Spec.FleetMetadataCacheEnabled(), knV2.Spec.Fleet.MCPGateway.Namespace, "fleetmetadatacache-mcpgateway", ComponentFleetMetadataCache},
		{mcpGatewayRemoteReadsEnabled(knV2), knV2.Spec.Fleet.MCPGateway.Namespace, "signalprocessing-mcpgateway", ComponentSignalProcessing},
		{mcpGatewayRemoteReadsEnabled(knV2), knV2.Spec.Fleet.MCPGateway.Namespace, "apifrontend-mcpgateway", ComponentAPIFrontend},
		{mcpGatewayRemoteReadsEnabled(knV2), knV2.Spec.Fleet.MCPGateway.Namespace, "effectivenessmonitor-mcpgateway", ComponentEffectivenessMonitor},
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
func MonitoringCRBNames(kn *kubernautv1alpha2.Kubernaut) []string {
	p := func(base string) string { return clusterRoleName(kn, base) }
	return []string{
		p("effectivenessmonitor-alertmanager-view-binding"),
		p("effectivenessmonitor-monitoring-view"),
		p("kubernaut-agent-monitoring-view"),
		p("kubernaut-agent-alertmanager-view-binding"),
		p("alertmanager-gateway-signal-source"),
		p("apifrontend-monitoring-view"),
	}
}

// MonitoringClusterRoleNames returns the names of all monitoring-related ClusterRoles.
func MonitoringClusterRoleNames(kn *kubernautv1alpha2.Kubernaut) []string {
	p := func(base string) string { return clusterRoleName(kn, base) }
	return []string{
		p("alertmanager-view"),
		p("gateway-signal-source"),
	}
}

// AnsibleRBAC returns the conditional AWX RBAC resources.
func AnsibleRBAC(kn *kubernautv1alpha2.Kubernaut) (*rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding) {
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
func AdditionalComponentCRBName(kn *kubernautv1alpha2.Kubernaut, component, clusterRoleName string) string {
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
func AdditionalComponentCRB(kn *kubernautv1alpha2.Kubernaut, component, saName, crName string) *rbacv1.ClusterRoleBinding {
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

// retiredPersonaClusterRoleSuffixes lists the six persona tool ClusterRole
// name suffixes rendered by pre-v1.6 operators (#118/#636). #682/#684
// removed the roles with the roles-only model; the list survives only for
// the finalizer sweep so an upgrading pre-GA cluster does not keep the
// retired tool sets after deleting its Kubernaut instance.
var retiredPersonaClusterRoleSuffixes = []string{
	"tool-sre", "tool-ai-orchestrator", "tool-cicd",
	"tool-observability", "tool-l3-audit", "tool-remediation-approver",
}

// RetiredToolClusterRoleNames returns the namespace-prefixed names of the
// six persona tool ClusterRoles a pre-v1.6 operator rendered. The finalizer
// deletes them on instance deletion; reconcile-time removal is deliberately
// not performed (DD-682 no-migration decision).
func RetiredToolClusterRoleNames(kn *kubernautv1alpha2.Kubernaut) []string {
	names := make([]string, 0, len(retiredPersonaClusterRoleSuffixes))
	for _, suffix := range retiredPersonaClusterRoleSuffixes {
		names = append(names, clusterRoleName(kn, suffix))
	}
	return names
}

// ConsoleAccessClusterRoleName returns the name of the coarse-grained
// console-access ClusterRole for label-prune and finalizer cleanup (#682:
// the role stays operator-rendered; any user-facing binding is
// administrator-owned).
func ConsoleAccessClusterRoleName(kn *kubernautv1alpha2.Kubernaut) string {
	return clusterRoleName(kn, "console-access")
}

// ConsoleAccessClusterRole builds the coarse-grained kubernaut.ai/console
// "use" ClusterRole gating all console/tool access (kubernaut#1919). It
// renders unconditionally whenever AF is enabled, independent of whether
// any groups are currently bound to it -- mirroring upstream's own
// always-render behavior for the equivalent Helm-templated ClusterRole.
// Returns nil when AF is disabled.
func ConsoleAccessClusterRole(kn *kubernautv1alpha2.Kubernaut) *rbacv1.ClusterRole {
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

// ConsoleReaderClusterRoleName returns the name of the console-reader
// ClusterRole (#636).
func ConsoleReaderClusterRoleName(kn *kubernautv1alpha2.Kubernaut) string {
	return clusterRoleName(kn, "console-reader")
}

// ConsoleReaderClusterRole builds the least-privilege role for the console
// REST read projection (kubernaut#2584 / DD-AF-018): policy scope and
// managed-resource inventory reads. AF authorizes these through the same
// named kubernaut.ai/tools "use" SubjectAccessReview as every tool action,
// so the capabilities are explicit resourceNames rather than a wildcard.
//
// The role is rendered whenever AF is enabled and deliberately has no
// binding: no persona ever carries these reads (DD-662 permanent model),
// and no login/console-access grant may imply them (#636). #682/#684 made
// the model roles-only: the operator never renders a user-facing
// ClusterRoleBinding, so administrators bind this role through their own
// external ClusterRoleBinding. See
// docs/design/DD-662-console-read-capability-model.md and
// docs/design/DD-682-roles-only-rbac.md.
//
// The read additionally requires the invoking user's own Kubernetes get/list
// grants on the local target resources; those are rendered separately as
// ConsoleInventoryReaderClusterRole (#663), and binding them stays an
// administrator decision.
func ConsoleReaderClusterRole(kn *kubernautv1alpha2.Kubernaut) *rbacv1.ClusterRole {
	if !kn.Spec.APIFrontendEnabled() {
		return nil
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ConsoleReaderClusterRoleName(kn),
			Labels: CommonLabels(kn),
		},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"kubernaut.ai"},
			Resources: []string{"tools"},
			ResourceNames: []string{
				"kubernaut_read_policy_scope",
				"kubernaut_list_managed_resources",
				"kubernaut_get_managed_resource",
			},
			Verbs: []string{"use"},
		}},
	}
}

// ConsoleInventoryReaderClusterRoleName returns the name of the
// console-inventory-reader ClusterRole (#663).
func ConsoleInventoryReaderClusterRoleName(kn *kubernautv1alpha2.Kubernaut) string {
	return clusterRoleName(kn, "console-inventory-reader")
}

// ConsoleInventoryReaderClusterRole builds the user-side local-target grant
// for the console managed-inventory projection (#663, closing DD-636 F2).
//
// AF authorizes a console user's local-target reads through
// ResourceAttributes SubjectAccessReviews AS THE INVOKING USER, not the AF
// ServiceAccount: list reads check verb "list" per allowlisted kind (carrying
// the namespace/name filters when the caller supplied them, so the default
// unfiltered list is a cluster-scoped SAR) and exact reads check verb "get"
// with the exact group/version/resource/namespace/name (kubernaut
// pkg/apifrontend/policyread service.go authorize/authorizeTarget against
// DefaultLocalResourceKinds, pinned at
// v1.6.0-rc9.0.20261120145050-6bd88d5dc344). The kind set below mirrors that
// pinned allowlist exactly: 11 Gateway-aligned kinds; secrets and configmaps
// are intentionally excluded upstream and stay excluded here. Any widening
// must follow an upstream allowlist change, not an operator-side decision.
//
// The role is rendered whenever AF is enabled and deliberately has no
// binding: which users or groups may read the local target is the
// administrator's decision, and the upstream persona↔capability mapping is
// still open (#662). Administrators bind it with their own
// ClusterRoleBinding (cluster-wide) or a namespaced RoleBinding
// (namespace-filtered reads only); the operator never renders the binding
// (#682 moves every user-facing binding out of the CR). A namespaced
// RoleBinding satisfies only namespace-filtered reads for the eight
// namespaced kinds; nodes, persistentvolumes and namespaces are
// cluster-scoped and always require a cluster-level binding, and the
// default unfiltered list needs one too. See
// docs/design/DD-636-console-direct-action-rbac.md and
// docs/installation/02-configure-services.md.
func ConsoleInventoryReaderClusterRole(kn *kubernautv1alpha2.Kubernaut) *rbacv1.ClusterRole {
	if !kn.Spec.APIFrontendEnabled() {
		return nil
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ConsoleInventoryReaderClusterRoleName(kn),
			Labels: CommonLabels(kn),
		},
		Rules: []rbacv1.PolicyRule{
			// Mirrors pkg/apifrontend/policyread DefaultLocalResourceKinds
			// (pinned module version in the doc comment above).
			{APIGroups: []string{""}, Resources: []string{"pods", "services", "nodes", "persistentvolumes", "namespaces"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments", "statefulsets", "daemonsets", "replicasets"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list"}},
		},
	}
}

// ownerChainResolutionRules returns the universal, non-ecosystem-specific
// reads shared by Gateway and EffectivenessMonitor while resolving owner
// references. Ecosystem-specific owner-chain reads remain administrator-
// supplied through spec.additionalClusterRoles.
func ownerChainResolutionRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"ingresses"}, Verbs: []string{"get", "list", "watch"}},
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

func groupClusterRoleBinding(name, roleName, groupName string, labels map[string]string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:     rbacv1.GroupKind,
			APIGroup: rbacv1.GroupName,
			Name:     groupName,
		}},
	}
}

// --- ClusterRole definitions (namespace-prefixed for multi-instance safety) ---

func gatewayClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	targetReads := gatewayTargetResourceReadRules()
	rules := make([]rbacv1.PolicyRule, 0, len(targetReads)+5)
	rules = append(rules, []rbacv1.PolicyRule{
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests"}, Verbs: []string{"create", "get", "list", "watch", "update", "patch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests/status"}, Verbs: []string{"update", "patch"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "create", "update", "delete"}},
		{APIGroups: []string{"authentication.k8s.io"}, Resources: []string{"tokenreviews"}, Verbs: []string{"create"}},
		{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"subjectaccessreviews"}, Verbs: []string{"create"}},
	}...)
	rules = append(rules, targetReads...)
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "gateway-role"), Labels: labels},
		Rules:      rules,
	}
}

func aianalysisControllerClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "aianalysis-controller"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"aianalyses"}, Verbs: []string{"get", "list", "watch", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"aianalyses/status"}, Verbs: []string{"get", "update", "patch"}},
			// DD-2582: AIA performs a fresh RemediationRequest parent gate read
			// and watches the parent for authorization/cancellation changes.
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests"}, Verbs: []string{"get", "list", "watch"}},
			// #400: AgentSessionCreator.GetOrCreate sets an ownerReference from
			// AgentSession back to this AIAnalysis CR via
			// controllerutil.SetControllerReference, which defaults
			// blockOwnerDeletion to true. Kubernetes'
			// OwnerReferencesPermissionEnforcement admission plugin requires
			// "update" on the owner's own /finalizers subresource for that to be
			// permitted -- every sibling controller that owns its own CR
			// (signalprocessing, remediationorchestrator, workflowexecution,
			// notification, authwebhook) already grants this; this was the sole
			// omission, blocking every AgentSession creation cluster-wide with
			// "cannot set blockOwnerDeletion if an ownerReference refers to a
			// resource you can't set finalizers on".
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"aianalyses/finalizers"}, Verbs: []string{"update"}},
			// v1.6.0-rc4 (kubernaut#2214, DD-AA-KA-001 Amendment): AA no
			// longer interacts with InvestigationSession at all (full RBAC
			// removal, not just narrowing -- #368 originally kept a
			// get/list/watch + status-write grant for a terminal-phase-close
			// path, K8sISPhaseUpdater; that path no longer exists). AF's
			// AgentSessionTerminalCloseReconciler now owns IS terminal-phase
			// closure exclusively, driven by watching AgentSession (see
			// apifrontendClusterRole).
			//
			// AgentSessionCreator.GetOrCreate (DD-AA-KA-001, BR-AA-KA-065.1): "create"
			// for the initial dispatch write; "get"/"list"/"watch" because AgentSession
			// is namespace-scoped in this controller's mgr.GetCache() ByObject config,
			// so Get() reads go through the informer cache, not a direct API call.
			// "delete" (BR-AI-009, DD-AA-KA-001 amendment) is for
			// AgentSessionCreator.DeleteForRetry, called from
			// InvestigatingHandler.retryCapacityExceeded to discard a stale
			// Failed+CapacityExceeded AgentSession so the retry attempt gets a fresh
			// one; also used by DeleteForCascadeCancel (kubernaut#2214) on
			// external ParentCancelled. "update" (kubernaut#2214 finalizer
			// fix, CI run 32525130330): DeleteForRetry/DeleteForCascadeCancel
			// strip AA's own TerminalCloseFinalizer before deleting, which
			// needs Update, not just Delete. No "agentsessions/status" grant
			// -- AA never writes Status, only KA does (BR-AA-KA-065.9).
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"agentsessions"}, Verbs: []string{"get", "list", "watch", "create", "update", "delete"}},
			{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
		},
	}
}

func kubernautAgentClientClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "kubernaut-agent-client"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{"kubernaut-agent"}, Verbs: []string{"create", "get"}},
		},
	}
}

func kubernautAgentInvestigatorClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
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
		{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"ingresses"}, Verbs: []string{"get", "list", "watch"}},
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
			"remediationrequests", "remediationapprovalrequests",
			"investigationsessions", "aianalyses", "signalprocessings",
			"effectivenessassessments", "workflowexecutions",
		}, Verbs: []string{"get", "list", "watch"}},
		// AgentSession (#380, parity with kubernaut#2231/PR#2250): KA's
		// Dispatcher Reconciler's dispatchCleanupFinalizer is a
		// metadata-only write on the base resource (not the status
		// subresource), so "update"/"patch" are needed alongside the
		// pre-existing read verbs. agentsessions/status is KA's
		// BR-AA-KA-065.9 status-reporting write path to AA -- without it,
		// both the finalizer add/remove and every status write 403.
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"agentsessions"}, Verbs: []string{"get", "list", "watch", "update", "patch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"agentsessions/status"}, Verbs: []string{"get", "update", "patch"}},
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
// mcpGatewayTypeKuadrant is spec.fleet.mcpGateway.type's Kuadrant value
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
	return fleet.Enabled != nil && *fleet.Enabled && fleet.MCPGateway.Endpoint != ""
}

// fleetCallerRBACEnabled reports whether the operator should provision the
// hub-side group bindings used by identities exchanged through the MCP
// Gateway. Backend-only Fleet configuration does not cause remote Kubernetes
// calls, so it must not receive these additional cluster-wide permissions.
func fleetCallerRBACEnabled(knV2 *kubernautv1alpha2.Kubernaut) bool {
	return mcpGatewayRemoteReadsEnabled(knV2)
}

func fleetCallerNodeReaderClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "fleet-caller-node-reader"), Labels: labels},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"nodes"},
			Verbs:     []string{"get", "list", "watch"},
		}},
	}
}

func fleetCallerWorkflowExecutionClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "fleet-caller-workflow-execution"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"batch"}, Resources: []string{"jobs"}, Verbs: []string{"create", "get", "list", "watch", "delete", "patch", "update"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"pipelineruns"}, Verbs: []string{"create", "get", "list", "watch", "delete", "patch", "update"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"taskruns"}, Verbs: []string{"get"}},
		},
	}
}

func signalprocessingClusterRole(kn *kubernautv1alpha2.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	rules := []rbacv1.PolicyRule{
		// DD-2582: SP performs a fresh RemediationRequest parent gate read and
		// watches the parent for authorization/cancellation changes.
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"signalprocessings"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"signalprocessings/status", "signalprocessings/finalizers"}, Verbs: []string{"get", "update", "patch"}},
		{APIGroups: []string{""}, Resources: []string{"pods", "services", "namespaces", "nodes"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
	}
	// #224: grant MCP Gateway CRD read access here only when the shared
	// spec.fleet.mcpGateway.namespace (DD-362) is empty; a resolved
	// namespace moves these rules to a namespace-scoped Role instead (see
	// MCPGatewayNamespaceRBAC).
	if mcpGatewayRemoteReadsEnabled(knV2) && knV2.Spec.Fleet.MCPGateway.Namespace == "" {
		rules = append(rules, mcpGatewayCRDPolicyRules(knV2.Spec.Fleet.MCPGateway.Type)...)
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "signalprocessing-controller"), Labels: labels},
		Rules:      rules,
	}
}

func remediationOrchestratorClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "remediationorchestrator-controller"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests", "remediationapprovalrequests"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests/status", "remediationapprovalrequests/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests/finalizers"}, Verbs: []string{"update"}},
			// DD-2582: RO watches InvestigationSession and reads failures to map
			// them to the owning RemediationRequest.
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"investigationsessions"}, Verbs: []string{"get", "list", "watch"}},
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
			// #645: PVC targets need metadata reads for RO's scope gate
			// (BR-SCOPE-010); read-only, same verb set as PVs (AC-6).
			{APIGroups: []string{""}, Resources: []string{"pods", "nodes", "services", "namespaces", "persistentvolumes", "persistentvolumeclaims", "configmaps"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
}

func workflowExecutionControllerClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "workflowexecution-controller"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions/finalizers"}, Verbs: []string{"update"}},
			// DD-2582: WFE performs a fresh RemediationRequest parent gate read
			// and watches the parent for authorization/cancellation changes.
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"pipelineruns"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"taskruns"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"batch"}, Resources: []string{"jobs"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			// #599 / DD-2582: verified cancellation observes owned Job/Tekton
			// Pods before persisting the terminal Cancelled phase. The cached
			// client requires list/watch to synchronize even when the executor
			// only performs a namespaced List; no Pod write access is needed.
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "watch"}},
			// #411: BR-WE-019/DD-WE-008's countPodCreationAttempts (kubernaut
			// core, pkg/workflowexecution/executor/job.go) counts
			// "SuccessfulCreate" Events on the Job object through the manager's
			// client to work around job.Status.Failed not incrementing for
			// PodFailurePolicy-Ignore-tolerated failures (OOM-kill,
			// node-disruption). That List/Get on core Events makes
			// controller-runtime register a cluster-scoped cache watch/list
			// reflector on *v1.Event -- write-only create/patch (the
			// Recorder.Event() pattern every other controller uses) left that
			// cache permanently failing to sync, blocking all
			// WorkflowExecution reconciliation.
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"get", "list", "watch", "create", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"serviceaccounts/token"}, Verbs: []string{"create"}},
			{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		},
	}
}

// workflowRunnerClusterRole contains only cluster-wide read access and CRD
// operations. Write access to secrets, configmaps, PVCs, etc. is scoped to
// the workflow namespace via workflowRunnerNamespaceRole (see WorkflowNamespaceRBAC).
func workflowRunnerClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
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

func effectivenessMonitorControllerClusterRole(kn *kubernautv1alpha2.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
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
	// rules are only needed while the shared spec.fleet.mcpGateway.namespace
	// (DD-362) is unresolved -- once it resolves, MCPGatewayNamespaceRBAC
	// grants a namespace-scoped Role instead (mirroring FMC/SP).
	if mcpGatewayRemoteReadsEnabled(knV2) && knV2.Spec.Fleet.MCPGateway.Namespace == "" {
		rules = append(rules, mcpGatewayCRDPolicyRules(knV2.Spec.Fleet.MCPGateway.Type)...)
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "effectivenessmonitor-controller"), Labels: labels},
		Rules:      rules,
	}
}

func notificationControllerClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
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

func dataStorageAuthMiddlewareClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "data-storage-auth-middleware"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"authentication.k8s.io"}, Resources: []string{"tokenreviews"}, Verbs: []string{"create"}},
			{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"subjectaccessreviews"}, Verbs: []string{"create"}},
		},
	}
}

func dataStorageClientClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
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
func fleetMetadataCacheAuthMiddlewareClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
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
func fmcScopeCheckClientClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "fmc-scope-check-client"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{fleetMetadataCacheServiceName}, Verbs: []string{"get"}},
		},
	}
}

func authWebhookClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
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

func alertmanagerViewClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "alertmanager-view"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"monitoring.coreos.com"}, Resources: []string{"alertmanagers/api"}, Verbs: []string{"get"}},
		},
	}
}

func gatewaySignalSourceClusterRole(kn *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "gateway-signal-source"), Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{"gateway-service"}, Verbs: []string{"create"}},
		},
	}
}

// apifrontendClusterRole builds the API Frontend permissions, including the
// InvestigationSession patch operations required by the interactive lifecycle.
func apifrontendClusterRole(kn *kubernautv1alpha2.Kubernaut, knV2 *kubernautv1alpha2.Kubernaut, labels map[string]string) *rbacv1.ClusterRole {
	rules := []rbacv1.PolicyRule{
		// kubernaut#566: API Frontend backfills the RemediationRequest
		// OwnerReference with a base-resource Patch. The status subresource
		// has its own patch grant because Kubernetes authorizes it separately.
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"investigationsessions"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"investigationsessions/status"}, Verbs: []string{"get", "update", "patch"}},
		// kubernaut#2172 (AwaitAgentSessionInteractive, DD-AA-KA-001
		// Amendment Gap 1): read-only watch of KA's own ack signal.
		// kubernaut#2214 (v1.6.0-rc4, finalizer redesign, CI RCA PR #2222
		// run 32513171970): AgentSessionTerminalCloseReconciler adds/removes
		// its own metadata-only finalizer (terminalCloseFinalizer) to
		// reliably observe deletion via Reconcile instead of a best-effort
		// raw watch delete event -- update/patch only touch
		// metadata.finalizers. No "agentsessions/status" grant -- AF never
		// writes Status, only KA does (BR-AA-KA-065.9).
		{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"agentsessions"}, Verbs: []string{"get", "list", "watch", "update", "patch"}},
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
		{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"ingresses"}, Verbs: []string{"get", "list"}},
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
	// rules are only needed while the shared spec.fleet.mcpGateway.namespace
	// (DD-362) is unresolved -- once it resolves, MCPGatewayNamespaceRBAC
	// grants a namespace-scoped Role instead (mirroring FMC/SP).
	if mcpGatewayRemoteReadsEnabled(knV2) && knV2.Spec.Fleet.MCPGateway.Namespace == "" {
		rules = append(rules, mcpGatewayCRDPolicyRules(knV2.Spec.Fleet.MCPGateway.Type)...)
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName(kn, "apifrontend-role"), Labels: labels},
		Rules:      rules,
	}
}

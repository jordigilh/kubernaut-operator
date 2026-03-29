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
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// ClusterRoles builds all ClusterRoles needed by the Kubernaut deployment,
// matching the Helm chart definitions.
func ClusterRoles(kn *kubernautv1alpha1.Kubernaut) []*rbacv1.ClusterRole {
	labels := CommonLabels(kn)
	roles := []*rbacv1.ClusterRole{
		gatewayClusterRole(labels),
		aianalysisControllerClusterRole(labels),
		holmesgptAPIClientClusterRole(labels),
		holmesgptAPIInvestigatorClusterRole(labels),
		signalprocessingClusterRole(labels),
		remediationOrchestratorClusterRole(labels),
		workflowExecutionControllerClusterRole(labels),
		workflowRunnerClusterRole(labels),
		effectivenessMonitorControllerClusterRole(labels),
		notificationControllerClusterRole(labels),
		dataStorageAuthMiddlewareClusterRole(labels),
		dataStorageClientClusterRole(labels),
		authWebhookClusterRole(labels),
	}

	if kn.Spec.Monitoring.MonitoringEnabled() {
		roles = append(roles, alertmanagerViewClusterRole(labels))
		roles = append(roles, gatewaySignalSourceClusterRole(labels))
	}

	return roles
}

// ClusterRoleBindings builds all CRBs, binding SAs in the CR namespace.
func ClusterRoleBindings(kn *kubernautv1alpha1.Kubernaut) []*rbacv1.ClusterRoleBinding {
	labels := CommonLabels(kn)
	ns := kn.Namespace

	crbs := []*rbacv1.ClusterRoleBinding{
		clusterRoleBinding("gateway-role-binding", "gateway-role", ServiceAccountName(ComponentGateway), ns, labels),
		clusterRoleBinding("aianalysis-controller-binding", "aianalysis-controller", ServiceAccountName(ComponentAIAnalysis), ns, labels),
		clusterRoleBinding("holmesgpt-api-investigator-binding", "holmesgpt-api-investigator", ServiceAccountName(ComponentHolmesGPTAPI), ns, labels),
		clusterRoleBinding("holmesgpt-api-auth-middleware-binding", "data-storage-auth-middleware", ServiceAccountName(ComponentHolmesGPTAPI), ns, labels),
		clusterRoleBinding("holmesgpt-api-client-aianalysis-binding", "holmesgpt-api-client", ServiceAccountName(ComponentAIAnalysis), ns, labels),
		clusterRoleBinding("signalprocessing-controller-binding", "signalprocessing-controller", ServiceAccountName(ComponentSignalProcessing), ns, labels),
		clusterRoleBinding("remediationorchestrator-controller-binding", "remediationorchestrator-controller", ServiceAccountName(ComponentRemediationOrchestrator), ns, labels),
		clusterRoleBinding("remediationorchestrator-view-binding", "view", ServiceAccountName(ComponentRemediationOrchestrator), ns, labels),
		clusterRoleBinding("workflowexecution-controller-binding", "workflowexecution-controller", ServiceAccountName(ComponentWorkflowExecution), ns, labels),
		clusterRoleBinding("effectivenessmonitor-controller-binding", "effectivenessmonitor-controller", ServiceAccountName(ComponentEffectivenessMonitor), ns, labels),
		clusterRoleBinding("effectivenessmonitor-view-binding", "view", ServiceAccountName(ComponentEffectivenessMonitor), ns, labels),
		clusterRoleBinding("notification-controller-binding", "notification-controller", ServiceAccountName(ComponentNotification), ns, labels),
		clusterRoleBinding("data-storage-auth-middleware-binding", "data-storage-auth-middleware", ServiceAccountName(ComponentDataStorage), ns, labels),
		clusterRoleBinding("authwebhook-binding", "authwebhook-role", ServiceAccountName(ComponentAuthWebhook), ns, labels),
	}

	wfNs := kn.Spec.WorkflowExecution.WorkflowNamespace
	if wfNs == "" {
		wfNs = DefaultWorkflowNamespace
	}
	crbs = append(crbs,
		clusterRoleBinding("kubernaut-workflow-runner-binding", "kubernaut-workflow-runner",
			"kubernaut-workflow-runner", wfNs, labels),
	)

	if kn.Spec.Monitoring.MonitoringEnabled() {
		crbs = append(crbs,
			clusterRoleBinding("effectivenessmonitor-alertmanager-view-binding", "kubernaut-alertmanager-view",
				ServiceAccountName(ComponentEffectivenessMonitor), ns, labels),
			clusterRoleBinding("effectivenessmonitor-monitoring-view", "cluster-monitoring-view",
				ServiceAccountName(ComponentEffectivenessMonitor), ns, labels),
			clusterRoleBinding("holmesgpt-api-monitoring-view", "cluster-monitoring-view",
				ServiceAccountName(ComponentHolmesGPTAPI), ns, labels),
			clusterRoleBinding("alertmanager-gateway-signal-source", "gateway-signal-source",
				OCPAlertManagerSAName, OCPMonitoringNamespace, labels),
		)
	}

	return crbs
}

// DataStorageClientRoleBindings builds the RoleBindings that grant
// data-storage-client ClusterRole access to each consuming SA.
func DataStorageClientRoleBindings(kn *kubernautv1alpha1.Kubernaut) []*rbacv1.RoleBinding {
	labels := CommonLabels(kn)
	ns := kn.Namespace

	consumers := []struct {
		name, sa string
	}{
		{"data-storage-client-gateway", ServiceAccountName(ComponentGateway)},
		{"data-storage-client-aianalysis", ServiceAccountName(ComponentAIAnalysis)},
		{"data-storage-client-signalprocessing", ServiceAccountName(ComponentSignalProcessing)},
		{"data-storage-client-remediationorchestrator", ServiceAccountName(ComponentRemediationOrchestrator)},
		{"data-storage-client-workflowexecution", ServiceAccountName(ComponentWorkflowExecution)},
		{"data-storage-client-effectivenessmonitor", ServiceAccountName(ComponentEffectivenessMonitor)},
		{"data-storage-client-notification", ServiceAccountName(ComponentNotification)},
		{"data-storage-client-holmesgpt-api", ServiceAccountName(ComponentHolmesGPTAPI)},
		{"data-storage-client-authwebhook", ServiceAccountName(ComponentAuthWebhook)},
		{"data-storage-client-datastorage", ServiceAccountName(ComponentDataStorage)},
	}

	var rbs []*rbacv1.RoleBinding
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
				Name:     "data-storage-client",
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

// NamespaceRoles builds the namespace-scoped Roles for secrets/configmaps access
// per the kubernaut.nsRoleForSecrets pattern.
func NamespaceRoles(kn *kubernautv1alpha1.Kubernaut) []*rbacv1.Role {
	labels := CommonLabels(kn)
	ns := kn.Namespace

	componentsNeedingNsRole := []string{
		ComponentGateway,
		ComponentDataStorage,
		ComponentAIAnalysis,
		ComponentSignalProcessing,
		ComponentRemediationOrchestrator,
		ComponentWorkflowExecution,
		ComponentEffectivenessMonitor,
		ComponentNotification,
		ComponentHolmesGPTAPI,
		ComponentAuthWebhook,
	}

	var roles []*rbacv1.Role
	for _, c := range componentsNeedingNsRole {
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
func NamespaceRoleBindings(kn *kubernautv1alpha1.Kubernaut) []*rbacv1.RoleBinding {
	labels := CommonLabels(kn)
	ns := kn.Namespace

	components := []string{
		ComponentGateway,
		ComponentDataStorage,
		ComponentAIAnalysis,
		ComponentSignalProcessing,
		ComponentRemediationOrchestrator,
		ComponentWorkflowExecution,
		ComponentEffectivenessMonitor,
		ComponentNotification,
		ComponentHolmesGPTAPI,
		ComponentAuthWebhook,
	}

	var rbs []*rbacv1.RoleBinding
	for _, c := range components {
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
	wfNs := kn.Spec.WorkflowExecution.WorkflowNamespace
	if wfNs == "" {
		wfNs = DefaultWorkflowNamespace
	}
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
	}

	return roles, rbs
}

// MonitoringCRBNames returns the names of all monitoring-related ClusterRoleBindings.
// Used by the finalizer to always attempt cleanup regardless of current Monitoring.Enabled.
func MonitoringCRBNames(kn *kubernautv1alpha1.Kubernaut) []string {
	return []string{
		"effectivenessmonitor-alertmanager-view-binding",
		"effectivenessmonitor-monitoring-view",
		"holmesgpt-api-monitoring-view",
		"alertmanager-gateway-signal-source",
	}
}

// MonitoringClusterRoleNames returns the names of all monitoring-related ClusterRoles.
func MonitoringClusterRoleNames() []string {
	return []string{
		"kubernaut-alertmanager-view",
		"gateway-signal-source",
	}
}

// AnsibleRBAC returns the conditional AWX RBAC resources.
func AnsibleRBAC(kn *kubernautv1alpha1.Kubernaut) (*rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding) {
	labels := CommonLabels(kn)
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "workflowexecution-awx", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"awx.ansible.com"},
				Resources: []string{"awxjobs"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}

	wfNs := kn.Spec.WorkflowExecution.WorkflowNamespace
	if wfNs == "" {
		wfNs = DefaultWorkflowNamespace
	}
	crb := clusterRoleBinding("workflowexecution-awx-binding", "workflowexecution-awx",
		"kubernaut-workflow-runner", wfNs, labels)

	return cr, crb
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

// --- ClusterRole definitions (match Helm chart templates 1:1) ---

func gatewayClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "gateway-role", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests"}, Verbs: []string{"create", "get", "list", "watch", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests/status"}, Verbs: []string{"update", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"nodes", "pods", "services", "persistentvolumes"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "create", "update", "delete"}},
			{APIGroups: []string{"authentication.k8s.io"}, Resources: []string{"tokenreviews"}, Verbs: []string{"create"}},
			{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"subjectaccessreviews"}, Verbs: []string{"create"}},
		},
	}
}

func aianalysisControllerClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "aianalysis-controller", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"aianalyses"}, Verbs: []string{"get", "list", "watch", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"aianalyses/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
		},
	}
}

func holmesgptAPIClientClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "holmesgpt-api-client", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{"holmesgpt-api"}, Verbs: []string{"create", "get"}},
		},
	}
}

func holmesgptAPIInvestigatorClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "holmesgpt-api-investigator", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"pods", "pods/log", "events", "services", "endpoints", "configmaps", "secrets", "nodes", "namespaces", "replicationcontrollers", "persistentvolumeclaims", "resourcequotas"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"events.k8s.io"}, Resources: []string{"events"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"cert-manager.io"}, Resources: []string{"certificates", "clusterissuers", "certificaterequests"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"argoproj.io"}, Resources: []string{"applications"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"policy.linkerd.io"}, Resources: []string{"servers", "authorizationpolicies", "meshtlsauthentications"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"security.istio.io"}, Resources: []string{"authorizationpolicies", "peerauthentications", "requestauthentications"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"networking.istio.io"}, Resources: []string{"virtualservices", "destinationrules", "gateways", "serviceentries"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"monitoring.coreos.com"}, Resources: []string{"prometheusrules", "servicemonitors", "podmonitors", "probes"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
}

func signalprocessingClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "signalprocessing-controller", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"signalprocessings", "remediationrequests"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"signalprocessings/status", "signalprocessings/finalizers"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"pods", "services", "namespaces", "nodes"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		},
	}
}

func remediationOrchestratorClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "remediationorchestrator-controller", Labels: labels},
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
			{APIGroups: []string{"security.istio.io"}, Resources: []string{"authorizationpolicies", "peerauthentications", "requestauthentications"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"networking.istio.io"}, Resources: []string{"virtualservices", "destinationrules", "gateways", "serviceentries"}, Verbs: []string{"get", "list"}},
		},
	}
}

func workflowExecutionControllerClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "workflowexecution-controller", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions/finalizers"}, Verbs: []string{"update"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"pipelineruns"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"taskruns"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"batch"}, Resources: []string{"jobs"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
			{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"security.istio.io"}, Resources: []string{"authorizationpolicies", "peerauthentications", "requestauthentications"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"networking.istio.io"}, Resources: []string{"virtualservices", "destinationrules", "gateways", "serviceentries"}, Verbs: []string{"get", "list"}},
		},
	}
}

func workflowRunnerClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "kubernaut-workflow-runner", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"apps"}, Resources: []string{"deployments", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list", "patch", "update"}},
			{APIGroups: []string{"apps"}, Resources: []string{"replicasets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "delete", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"pods/eviction"}, Verbs: []string{"create"}},
			{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list", "create", "update", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get", "list", "patch", "update"}},
			{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list", "patch"}},
			{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"services"}, Verbs: []string{"get", "list", "create", "update", "patch"}},
			{APIGroups: []string{"argoproj.io"}, Resources: []string{"applications"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get", "list", "create", "delete", "patch", "update"}},
			{APIGroups: []string{""}, Resources: []string{"serviceaccounts/token"}, Verbs: []string{"create"}},
			{APIGroups: []string{"cert-manager.io"}, Resources: []string{"certificates"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"cert-manager.io"}, Resources: []string{"clusterissuers"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
			{APIGroups: []string{""}, Resources: []string{"persistentvolumeclaims"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"policy.linkerd.io"}, Resources: []string{"authorizationpolicies", "servers", "meshtlsauthentications"}, Verbs: []string{"get", "list", "delete"}},
			{APIGroups: []string{"security.istio.io"}, Resources: []string{"authorizationpolicies", "peerauthentications", "requestauthentications"}, Verbs: []string{"get", "list", "delete"}},
			{APIGroups: []string{"networking.istio.io"}, Resources: []string{"virtualservices", "destinationrules", "gateways", "serviceentries"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions"}, Verbs: []string{"get"}},
			{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"storageclasses"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{""}, Resources: []string{"endpoints"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"batch"}, Resources: []string{"jobs"}, Verbs: []string{"get", "list", "create", "delete"}},
		},
	}
}

func effectivenessMonitorControllerClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "effectivenessmonitor-controller", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"effectivenessassessments"}, Verbs: []string{"get", "list", "watch", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"effectivenessassessments/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationrequests"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"pods", "nodes", "services", "persistentvolumeclaims", "configmaps"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"security.istio.io"}, Resources: []string{"authorizationpolicies", "peerauthentications", "requestauthentications"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"networking.istio.io"}, Resources: []string{"virtualservices", "destinationrules", "gateways", "serviceentries"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
		},
	}
}

func notificationControllerClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "notification-controller", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"notificationrequests"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"notificationrequests/status"}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"notificationrequests/finalizers"}, Verbs: []string{"update"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
		},
	}
}

func dataStorageAuthMiddlewareClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "data-storage-auth-middleware", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"authentication.k8s.io"}, Resources: []string{"tokenreviews"}, Verbs: []string{"create"}},
			{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"subjectaccessreviews"}, Verbs: []string{"create"}},
		},
	}
}

func dataStorageClientClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "data-storage-client", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{"data-storage-service"}, Verbs: []string{"create", "get", "list", "update", "delete"}},
		},
	}
}

func authWebhookClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "authwebhook-role", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions", "remediationapprovalrequests", "notificationrequests", "remediationrequests", "actiontypes"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationworkflows"}, Verbs: []string{"get", "list", "watch", "update", "patch"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"remediationworkflows/finalizers"}, Verbs: []string{"update"}},
			{APIGroups: []string{"kubernaut.ai"}, Resources: []string{"workflowexecutions/status", "remediationapprovalrequests/status", "remediationrequests/status", "remediationworkflows/status", "actiontypes/status"}, Verbs: []string{"update", "patch"}},
		},
	}
}

func alertmanagerViewClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "kubernaut-alertmanager-view", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"monitoring.coreos.com"}, Resources: []string{"alertmanagers/api"}, Verbs: []string{"get"}},
		},
	}
}

func gatewaySignalSourceClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "gateway-signal-source", Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"services"}, ResourceNames: []string{"gateway-service"}, Verbs: []string{"create"}},
		},
	}
}

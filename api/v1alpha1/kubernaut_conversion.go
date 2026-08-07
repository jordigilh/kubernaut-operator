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

package v1alpha1

import (
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/conversion"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
)

// conversionLog is used for the lossy/behavior-changing conversion cases
// below. The apiextensions.k8s.io/v1 ConversionReview API has no Warnings
// field (unlike AdmissionReview) -- verified against
// k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1.ConversionResponse
// -- so there is no way for a conversion webhook to surface a client-visible
// warning on `kubectl apply`/`get`. A structured log entry on the operator's
// own output is the closest available signal; see the D5 migration guide
// for the user-facing callout these log lines cannot replace.
var conversionLog = logf.Log.WithName("kubernaut-conversion-webhook")

// keycloakJWKSSuffix is the best-effort default JWKS path convention used
// when deriving JWTProviderSpec.JWKSURL from IssuerURL for v1alpha1 CRs that
// left it empty (F6). This matches Keycloak/RHBK's URL convention
// (issuer + "/protocol/openid-connect/certs"), already used elsewhere in
// this codebase for the single-provider AuthWebhook default (see
// internal/controller/kubernaut_controller.go). It is a heuristic, not
// authoritative OIDC discovery (RFC 8414 well-known metadata) -- operators
// whose IdP uses a different JWKS path must set jwksURL explicitly.
const keycloakJWKSSuffix = "/protocol/openid-connect/certs"

// defaultAIAnalysisPolicyConfigMapName and defaultSignalProcessingPolicyConfigMapName
// mirror the fallback names internal/resources/common.go's AIAnalysisPolicyName /
// SignalProcessingPolicyName apply when a v1alpha1 CR leaves policy.configMapName
// empty (relying on a conventionally-named, user-provided ConfigMap). v1alpha2
// makes spec.aiAnalysis/spec.signalProcessing themselves required (not just the
// nested Policy field), closing a structural-schema loophole where omitting the
// whole block bypassed Policy's own required-ness (see KubernautSpec.AIAnalysis
// doc comment in api/v1alpha2/kubernaut_types.go). Without this backfill, an
// existing v1alpha1 CR that used the pre-v1alpha2 fallback would fail to convert
// to a schema-valid v1alpha2 object once v1alpha2 becomes the storage version.
const (
	defaultAIAnalysisPolicyConfigMapName       = "aianalysis-policies"
	defaultSignalProcessingPolicyConfigMapName = "signalprocessing-policy"
)

// ConvertTo converts this v1alpha1 Kubernaut to the v1alpha2 hub version.
// See docs/design/ADR-CRD-001-v1alpha2-redesign.md for the full field
// mapping and rationale (F1-F9).
func (src *Kubernaut) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*v1alpha2.Kubernaut)
	if !ok {
		return fmt.Errorf("ConvertTo: expected *v1alpha2.Kubernaut, got %T", dstRaw)
	}

	dst.ObjectMeta = src.ObjectMeta

	dst.Spec = v1alpha2.KubernautSpec{
		Image:                   convertImageSpecToV2(src.Spec.Image),
		PostgreSQL:              convertPostgreSQLSpecToV2(src.Spec.PostgreSQL),
		Valkey:                  convertValkeySpecToV2(src.Spec.Valkey),
		Notification:            convertNotificationSpecToV2(src.Spec.Notification),
		AIAnalysis:              convertAIAnalysisSpecToV2(src.Spec.AIAnalysis),
		SignalProcessing:        convertSignalProcessingSpecToV2(src.Spec.SignalProcessing),
		RemediationOrchestrator: convertRemediationOrchestratorSpecToV2(src.Spec.RemediationOrchestrator),
		WorkflowExecution:       convertWorkflowExecutionSpecToV2(src.Spec.WorkflowExecution, src.Spec.Ansible), // F4: relocated
		EffectivenessMonitor:    convertEffectivenessMonitorSpecToV2(src.Spec.EffectivenessMonitor),
		Monitoring:              v1alpha2.MonitoringSpec{}, // F2: no v1alpha1 source, left unset
		LLMProfiles:             convertLLMProfilesToV2(src.Spec.LLMProfiles),
		KubernautAgent:          convertKubernautAgentSpecToV2(src.Spec.KubernautAgent, src.Spec.LLMProfiles),
		Gateway:                 convertGatewaySpecToV2(src.Spec.Gateway),
		AuthWebhook:             convertAuthWebhookSpecToV2(src.Spec.AuthWebhook),
		DataStorage:             convertDataStorageSpecToV2(src.Spec.DataStorage),
		NetworkPolicies:         convertNetworkPoliciesSpecToV2(src.Spec.NetworkPolicies),
		APIFrontend:             convertAPIFrontendSpecToV2(src.Spec.APIFrontend),
		Console:                 convertConsoleSpecToV2(src.Spec.Console),
		Fleet:                   convertFleetSpecToV2(src.Spec.Fleet),
		FleetMetadataCache:      convertFleetMetadataCacheSpecToV2(src.Spec.FleetMetadataCache),
	}

	dst.Status = convertStatusToV2(src.Status)

	return nil
}

// ConvertFrom converts the v1alpha2 hub version to this v1alpha1 Kubernaut.
// See docs/design/ADR-CRD-001-v1alpha2-redesign.md for the full field
// mapping and rationale (F1-F9).
func (dst *Kubernaut) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*v1alpha2.Kubernaut)
	if !ok {
		return fmt.Errorf("ConvertFrom: expected *v1alpha2.Kubernaut, got %T", srcRaw)
	}

	dst.ObjectMeta = src.ObjectMeta

	dst.Spec = KubernautSpec{
		Image:                   convertImageSpecToV1(src.Spec.Image),
		PostgreSQL:              convertPostgreSQLSpecToV1(src.Spec.PostgreSQL),
		Valkey:                  convertValkeySpecToV1(src.Spec.Valkey),
		Ansible:                 convertAnsibleSpecToV1(src.Spec.WorkflowExecution.Ansible), // F4: relocated back
		Notification:            convertNotificationSpecToV1(src.Spec.Notification),
		AIAnalysis:              convertAIAnalysisSpecToV1(src.Spec.AIAnalysis),
		SignalProcessing:        convertSignalProcessingSpecToV1(src.Spec.SignalProcessing),
		RemediationOrchestrator: convertRemediationOrchestratorSpecToV1(src.Spec.RemediationOrchestrator),
		WorkflowExecution:       convertWorkflowExecutionSpecToV1(src.Spec.WorkflowExecution),
		EffectivenessMonitor:    convertEffectivenessMonitorSpecToV1(src.Spec.EffectivenessMonitor),
		LLMProfiles:             convertLLMProfilesToV1(src.Spec.LLMProfiles),
		KubernautAgent:          convertKubernautAgentSpecToV1(src.Spec.KubernautAgent, src.Spec.LLMProfiles),
		Gateway:                 convertGatewaySpecToV1(src.Spec.Gateway),
		AuthWebhook:             convertAuthWebhookSpecToV1(src.Spec.AuthWebhook),
		DataStorage:             convertDataStorageSpecToV1(src.Spec.DataStorage),
		NetworkPolicies:         convertNetworkPoliciesSpecToV1(src.Spec.NetworkPolicies, src.Name, src.Namespace),
		APIFrontend:             convertAPIFrontendSpecToV1(src.Spec.APIFrontend),
		Console:                 convertConsoleSpecToV1(src.Spec.Console),
		Fleet:                   convertFleetSpecToV1(src.Spec.Fleet),
		FleetMetadataCache:      convertFleetMetadataCacheSpecToV1(src.Spec.FleetMetadataCache),
	}

	dst.Status = convertStatusToV1(src.Status)

	return nil
}

// ---------- F1: FleetOverrideSpec <-> flat per-component fields ----------

// fleetOverrideOAuth2ToV1 extracts the OAuth2CredentialsSecretRef half of a
// v1alpha2 FleetOverrideSpec for the 5 v1alpha1 components that only ever
// had FleetOAuth2CredentialsSecretRef (no namespace override): Gateway,
// RemediationOrchestrator, EffectivenessMonitor, KubernautAgent, APIFrontend.
func fleetOverrideOAuth2ToV1(f *v1alpha2.FleetOverrideSpec) string {
	if f == nil {
		return ""
	}
	return f.OAuth2CredentialsSecretRef
}

// fleetOverrideFromOAuth2 builds a v1alpha2 FleetOverrideSpec from a v1alpha1
// flat FleetOAuth2CredentialsSecretRef, or nil when empty (no override).
func fleetOverrideFromOAuth2(oauth2SecretRef string) *v1alpha2.FleetOverrideSpec {
	if oauth2SecretRef == "" {
		return nil
	}
	return &v1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: oauth2SecretRef}
}

// fleetOverrideFromOAuth2AndNamespace builds a v1alpha2 FleetOverrideSpec
// from v1alpha1's SignalProcessing/FleetMetadataCache pair of separate
// FleetOAuth2CredentialsSecretRef and MCPGatewayNamespace fields, or nil when
// both are empty.
func fleetOverrideFromOAuth2AndNamespace(oauth2SecretRef, namespace string) *v1alpha2.FleetOverrideSpec {
	if oauth2SecretRef == "" && namespace == "" {
		return nil
	}
	return &v1alpha2.FleetOverrideSpec{OAuth2CredentialsSecretRef: oauth2SecretRef, Namespace: namespace}
}

// ---------- F8: LoggingSpec case normalization ----------

func convertLoggingSpecToV2(l LoggingSpec) v1alpha2.LoggingSpec {
	return v1alpha2.LoggingSpec{Level: strings.ToUpper(l.Level)}
}

func convertLoggingSpecToV1(l v1alpha2.LoggingSpec) LoggingSpec {
	// v1alpha1's enum accepts both cases; v1alpha2's value is already
	// uppercase, so a direct copy is always valid in this direction.
	return LoggingSpec{Level: l.Level}
}

// ---------- Leaf/simple specs (unchanged shape, direct field copies) ----------

func convertImageSpecToV2(s ImageSpec) v1alpha2.ImageSpec {
	return v1alpha2.ImageSpec{PullPolicy: s.PullPolicy, PullSecrets: s.PullSecrets, Overrides: s.Overrides}
}

func convertImageSpecToV1(s v1alpha2.ImageSpec) ImageSpec {
	return ImageSpec{PullPolicy: s.PullPolicy, PullSecrets: s.PullSecrets, Overrides: s.Overrides}
}

func convertPostgreSQLSpecToV2(s PostgreSQLSpec) v1alpha2.PostgreSQLSpec {
	return v1alpha2.PostgreSQLSpec{SecretName: s.SecretName, Host: s.Host, Port: s.Port, SSLMode: s.SSLMode}
}

func convertPostgreSQLSpecToV1(s v1alpha2.PostgreSQLSpec) PostgreSQLSpec {
	return PostgreSQLSpec{SecretName: s.SecretName, Host: s.Host, Port: s.Port, SSLMode: s.SSLMode}
}

func convertValkeyTLSSpecToV2(s *ValkeyTLSSpec) *v1alpha2.ValkeyTLSSpec {
	if s == nil {
		return nil
	}
	return &v1alpha2.ValkeyTLSSpec{Enabled: s.Enabled, CASecretName: s.CASecretName, ClientCertSecretName: s.ClientCertSecretName}
}

func convertValkeyTLSSpecToV1(s *v1alpha2.ValkeyTLSSpec) *ValkeyTLSSpec {
	if s == nil {
		return nil
	}
	return &ValkeyTLSSpec{Enabled: s.Enabled, CASecretName: s.CASecretName, ClientCertSecretName: s.ClientCertSecretName}
}

func convertValkeySpecToV2(s ValkeySpec) v1alpha2.ValkeySpec {
	return v1alpha2.ValkeySpec{SecretName: s.SecretName, Host: s.Host, Port: s.Port, TLS: convertValkeyTLSSpecToV2(s.TLS)}
}

func convertValkeySpecToV1(s v1alpha2.ValkeySpec) ValkeySpec {
	return ValkeySpec{SecretName: s.SecretName, Host: s.Host, Port: s.Port, TLS: convertValkeyTLSSpecToV1(s.TLS)}
}

func convertSlackSpecToV2(s SlackSpec) v1alpha2.SlackSpec {
	return v1alpha2.SlackSpec{SecretName: s.SecretName, Channel: s.Channel}
}

func convertSlackSpecToV1(s v1alpha2.SlackSpec) SlackSpec {
	return SlackSpec{SecretName: s.SecretName, Channel: s.Channel}
}

func convertConfigMapRefToV2(r *ConfigMapRef) *v1alpha2.ConfigMapRef {
	if r == nil {
		return nil
	}
	return &v1alpha2.ConfigMapRef{ConfigMapName: r.ConfigMapName}
}

func convertConfigMapRefToV1(r *v1alpha2.ConfigMapRef) *ConfigMapRef {
	if r == nil {
		return nil
	}
	return &ConfigMapRef{ConfigMapName: r.ConfigMapName}
}

func convertNotificationSpecToV2(s NotificationSpec) v1alpha2.NotificationSpec {
	return v1alpha2.NotificationSpec{
		Slack:     convertSlackSpecToV2(s.Slack),
		Routing:   convertConfigMapRefToV2(s.Routing),
		Logging:   convertLoggingSpecToV2(s.Logging),
		Resources: s.Resources,
	}
}

func convertNotificationSpecToV1(s v1alpha2.NotificationSpec) NotificationSpec {
	return NotificationSpec{
		Slack:     convertSlackSpecToV1(s.Slack),
		Routing:   convertConfigMapRefToV1(s.Routing),
		Logging:   convertLoggingSpecToV1(s.Logging),
		Resources: s.Resources,
	}
}

// derivePolicyConfigMapName backfills a v1alpha1 CR's policy.configMapName
// with defaultName when empty, matching the resource-builder fallback
// (internal/resources/common.go's AIAnalysisPolicyName/SignalProcessingPolicyName)
// so pre-v1alpha2 CRs that relied on that implicit convention still produce a
// schema-valid v1alpha2 object now that the field is required. See the
// default*PolicyConfigMapName const doc above.
func derivePolicyConfigMapName(fieldPath, configMapName, defaultName string) string {
	if configMapName != "" {
		return configMapName
	}
	conversionLog.Info("backfilled empty policy.configMapName for v1alpha2 (relied on resource-builder default in v1alpha1)",
		"field", fieldPath, "defaultConfigMapName", defaultName)
	return defaultName
}

func convertAIAnalysisSpecToV2(s AIAnalysisSpec) v1alpha2.AIAnalysisSpec {
	return v1alpha2.AIAnalysisSpec{
		Policy: v1alpha2.PolicyConfigMapRef{
			ConfigMapName: derivePolicyConfigMapName("spec.aiAnalysis.policy.configMapName", s.Policy.ConfigMapName, defaultAIAnalysisPolicyConfigMapName),
		},
		ConfidenceThreshold: s.ConfidenceThreshold,
		Logging:             convertLoggingSpecToV2(s.Logging),
		Resources:           s.Resources,
	}
}

func convertAIAnalysisSpecToV1(s v1alpha2.AIAnalysisSpec) AIAnalysisSpec {
	return AIAnalysisSpec{
		Policy:              PolicyConfigMapRef{ConfigMapName: s.Policy.ConfigMapName},
		ConfidenceThreshold: s.ConfidenceThreshold,
		Logging:             convertLoggingSpecToV1(s.Logging),
		Resources:           s.Resources,
	}
}

// ---------- F1: SignalProcessingSpec ----------

func convertSignalProcessingSpecToV2(s SignalProcessingSpec) v1alpha2.SignalProcessingSpec {
	return v1alpha2.SignalProcessingSpec{
		Policy: v1alpha2.PolicyConfigMapRef{
			ConfigMapName: derivePolicyConfigMapName("spec.signalProcessing.policy.configMapName", s.Policy.ConfigMapName, defaultSignalProcessingPolicyConfigMapName),
		},
		ProactiveSignalMappings: convertConfigMapRefToV2(s.ProactiveSignalMappings),
		Logging:                 convertLoggingSpecToV2(s.Logging),
		Resources:               s.Resources,
		Fleet:                   fleetOverrideFromOAuth2AndNamespace(s.FleetOAuth2CredentialsSecretRef, s.MCPGatewayNamespace),
	}
}

func convertSignalProcessingSpecToV1(s v1alpha2.SignalProcessingSpec) SignalProcessingSpec {
	out := SignalProcessingSpec{
		Policy:                  PolicyConfigMapRef{ConfigMapName: s.Policy.ConfigMapName},
		ProactiveSignalMappings: convertConfigMapRefToV1(s.ProactiveSignalMappings),
		Logging:                 convertLoggingSpecToV1(s.Logging),
		Resources:               s.Resources,
	}
	if s.Fleet != nil {
		out.FleetOAuth2CredentialsSecretRef = s.Fleet.OAuth2CredentialsSecretRef
		out.MCPGatewayNamespace = s.Fleet.Namespace
	}
	return out
}

// ---------- RemediationOrchestrator and its unchanged nested types ----------

func convertROTimeoutsSpecToV2(s ROTimeoutsSpec) v1alpha2.ROTimeoutsSpec {
	return v1alpha2.ROTimeoutsSpec{
		Global: s.Global, Processing: s.Processing, Analyzing: s.Analyzing,
		Executing: s.Executing, AwaitingApproval: s.AwaitingApproval, Verifying: s.Verifying,
	}
}

func convertROTimeoutsSpecToV1(s v1alpha2.ROTimeoutsSpec) ROTimeoutsSpec {
	return ROTimeoutsSpec{
		Global: s.Global, Processing: s.Processing, Analyzing: s.Analyzing,
		Executing: s.Executing, AwaitingApproval: s.AwaitingApproval, Verifying: s.Verifying,
	}
}

func convertRORoutingSpecToV2(s RORoutingSpec) v1alpha2.RORoutingSpec {
	return v1alpha2.RORoutingSpec{
		ConsecutiveFailureThreshold: s.ConsecutiveFailureThreshold, ConsecutiveFailureCooldown: s.ConsecutiveFailureCooldown,
		RecentlyRemediatedCooldown: s.RecentlyRemediatedCooldown, ExponentialBackoffBase: s.ExponentialBackoffBase,
		ExponentialBackoffMax: s.ExponentialBackoffMax, ExponentialBackoffMaxExponent: s.ExponentialBackoffMaxExponent,
		ScopeBackoffBase: s.ScopeBackoffBase, ScopeBackoffMax: s.ScopeBackoffMax,
		NoActionRequiredDelayHours: s.NoActionRequiredDelayHours, IneffectiveChainThreshold: s.IneffectiveChainThreshold,
		RecurrenceCountThreshold: s.RecurrenceCountThreshold, IneffectiveTimeWindow: s.IneffectiveTimeWindow,
	}
}

func convertRORoutingSpecToV1(s v1alpha2.RORoutingSpec) RORoutingSpec {
	return RORoutingSpec{
		ConsecutiveFailureThreshold: s.ConsecutiveFailureThreshold, ConsecutiveFailureCooldown: s.ConsecutiveFailureCooldown,
		RecentlyRemediatedCooldown: s.RecentlyRemediatedCooldown, ExponentialBackoffBase: s.ExponentialBackoffBase,
		ExponentialBackoffMax: s.ExponentialBackoffMax, ExponentialBackoffMaxExponent: s.ExponentialBackoffMaxExponent,
		ScopeBackoffBase: s.ScopeBackoffBase, ScopeBackoffMax: s.ScopeBackoffMax,
		NoActionRequiredDelayHours: s.NoActionRequiredDelayHours, IneffectiveChainThreshold: s.IneffectiveChainThreshold,
		RecurrenceCountThreshold: s.RecurrenceCountThreshold, IneffectiveTimeWindow: s.IneffectiveTimeWindow,
	}
}

func convertRemediationOrchestratorSpecToV2(s RemediationOrchestratorSpec) v1alpha2.RemediationOrchestratorSpec {
	return v1alpha2.RemediationOrchestratorSpec{
		Timeouts:                convertROTimeoutsSpecToV2(s.Timeouts),
		Routing:                 convertRORoutingSpecToV2(s.Routing),
		EffectivenessAssessment: v1alpha2.ROEffectivenessSpec{StabilizationWindow: s.EffectivenessAssessment.StabilizationWindow},
		AsyncPropagation: v1alpha2.ROAsyncPropagationSpec{
			GitOpsSyncDelay: s.AsyncPropagation.GitOpsSyncDelay, OperatorReconcileDelay: s.AsyncPropagation.OperatorReconcileDelay,
			ProactiveAlertDelay: s.AsyncPropagation.ProactiveAlertDelay,
		},
		DryRun:           s.DryRun,
		DryRunHoldPeriod: s.DryRunHoldPeriod,
		Notifications:    v1alpha2.RONotificationsSpec{NotifySelfResolved: s.Notifications.NotifySelfResolved},
		Retention:        v1alpha2.RORetentionSpec{Period: s.Retention.Period},
		Logging:          convertLoggingSpecToV2(s.Logging),
		Resources:        s.Resources,
		Fleet:            fleetOverrideFromOAuth2(s.FleetOAuth2CredentialsSecretRef),
	}
}

func convertRemediationOrchestratorSpecToV1(s v1alpha2.RemediationOrchestratorSpec) RemediationOrchestratorSpec {
	out := RemediationOrchestratorSpec{
		Timeouts:                convertROTimeoutsSpecToV1(s.Timeouts),
		Routing:                 convertRORoutingSpecToV1(s.Routing),
		EffectivenessAssessment: ROEffectivenessSpec{StabilizationWindow: s.EffectivenessAssessment.StabilizationWindow},
		AsyncPropagation: ROAsyncPropagationSpec{
			GitOpsSyncDelay: s.AsyncPropagation.GitOpsSyncDelay, OperatorReconcileDelay: s.AsyncPropagation.OperatorReconcileDelay,
			ProactiveAlertDelay: s.AsyncPropagation.ProactiveAlertDelay,
		},
		DryRun:           s.DryRun,
		DryRunHoldPeriod: s.DryRunHoldPeriod,
		Notifications:    RONotificationsSpec{NotifySelfResolved: s.Notifications.NotifySelfResolved},
		Retention:        RORetentionSpec{Period: s.Retention.Period},
		Logging:          convertLoggingSpecToV1(s.Logging),
		Resources:        s.Resources,
	}
	out.FleetOAuth2CredentialsSecretRef = fleetOverrideOAuth2ToV1(s.Fleet)
	return out
}

// ---------- F1 + F4: WorkflowExecutionSpec (Fleet added, Ansible relocated) ----------

func convertAnsibleSpecToV2(s AnsibleSpec) v1alpha2.AnsibleSpec {
	return v1alpha2.AnsibleSpec{
		Enabled: s.Enabled, APIURL: s.APIURL, OrganizationID: s.OrganizationID,
		TokenSecretRef:  (*v1alpha2.SecretKeyRef)(convertSecretKeyRefToV2(s.TokenSecretRef)),
		CACertSecretRef: convertCACertSecretRefToV2(s.CACertSecretRef),
	}
}

func convertSecretKeyRefToV2(r *SecretKeyRef) *SecretKeyRef {
	// SecretKeyRef is identical in both versions; kept as a v1alpha1 type
	// alias-free copy for the one caller that needs a *SecretKeyRef.
	if r == nil {
		return nil
	}
	cp := *r
	return &cp
}

func convertCACertSecretRefToV2(r *CACertSecretRef) *v1alpha2.CACertSecretRef {
	if r == nil {
		return nil
	}
	return &v1alpha2.CACertSecretRef{Name: r.Name, Key: r.Key}
}

func convertCACertSecretRefToV1(r *v1alpha2.CACertSecretRef) *CACertSecretRef {
	if r == nil {
		return nil
	}
	return &CACertSecretRef{Name: r.Name, Key: r.Key}
}

func convertAnsibleSpecToV1(s v1alpha2.AnsibleSpec) AnsibleSpec {
	return AnsibleSpec{
		Enabled: s.Enabled, APIURL: s.APIURL, OrganizationID: s.OrganizationID,
		TokenSecretRef:  (*SecretKeyRef)(s.TokenSecretRef),
		CACertSecretRef: convertCACertSecretRefToV1(s.CACertSecretRef),
	}
}

func convertWorkflowExecutionSpecToV2(s WorkflowExecutionSpec, ansible AnsibleSpec) v1alpha2.WorkflowExecutionSpec {
	return v1alpha2.WorkflowExecutionSpec{
		WorkflowNamespace: s.WorkflowNamespace,
		CooldownPeriod:    s.CooldownPeriod,
		Tekton:            v1alpha2.TektonSpec{Enabled: s.Tekton.Enabled},
		Ansible:           convertAnsibleSpecToV2(ansible), // F4: relocated from top-level spec.ansible
		Fleet:             nil,                             // F1: new field, no v1alpha1 source
		Logging:           convertLoggingSpecToV2(s.Logging),
		Resources:         s.Resources,
	}
}

func convertWorkflowExecutionSpecToV1(s v1alpha2.WorkflowExecutionSpec) WorkflowExecutionSpec {
	if s.Fleet != nil {
		conversionLog.Info("dropping workflowExecution.fleet on downgrade to v1alpha1: field has no v1alpha1 equivalent",
			"oauth2CredentialsSecretRef", s.Fleet.OAuth2CredentialsSecretRef != "", "namespace", s.Fleet.Namespace != "")
	}
	return WorkflowExecutionSpec{
		WorkflowNamespace: s.WorkflowNamespace,
		CooldownPeriod:    s.CooldownPeriod,
		Tekton:            TektonSpec{Enabled: s.Tekton.Enabled},
		Logging:           convertLoggingSpecToV1(s.Logging),
		Resources:         s.Resources,
	}
}

// ---------- F1: EffectivenessMonitorSpec ----------

func convertEffectivenessMonitorSpecToV2(s EffectivenessMonitorSpec) v1alpha2.EffectivenessMonitorSpec {
	return v1alpha2.EffectivenessMonitorSpec{
		Assessment: v1alpha2.EMAssessmentSpec{StabilizationWindow: s.Assessment.StabilizationWindow, ValidityWindow: s.Assessment.ValidityWindow},
		Logging:    convertLoggingSpecToV2(s.Logging),
		Resources:  s.Resources,
		Fleet:      fleetOverrideFromOAuth2(s.FleetOAuth2CredentialsSecretRef),
	}
}

func convertEffectivenessMonitorSpecToV1(s v1alpha2.EffectivenessMonitorSpec) EffectivenessMonitorSpec {
	if s.Fleet != nil && s.Fleet.Namespace != "" {
		conversionLog.Info("dropping effectivenessMonitor.fleet.namespace on downgrade to v1alpha1: field has no v1alpha1 equivalent")
	}
	return EffectivenessMonitorSpec{
		Assessment:                      EMAssessmentSpec{StabilizationWindow: s.Assessment.StabilizationWindow, ValidityWindow: s.Assessment.ValidityWindow},
		Logging:                         convertLoggingSpecToV1(s.Logging),
		Resources:                       s.Resources,
		FleetOAuth2CredentialsSecretRef: fleetOverrideOAuth2ToV1(s.Fleet),
	}
}

// ---------- F1 + F5 + F6: KubernautAgentSpec ----------

// deriveJWKSURL applies the best-effort keycloakJWKSSuffix default when a
// JWTProviderSpec's JWKSURL is empty (F6). See the const doc for caveats.
func deriveJWKSURL(issuerURL, jwksURL string) string {
	if jwksURL != "" {
		return jwksURL
	}
	derived := strings.TrimRight(issuerURL, "/") + keycloakJWKSSuffix
	conversionLog.Info("derived jwksURL for v1alpha2 (was empty in v1alpha1)", "issuerURL", issuerURL, "derivedJWKSURL", derived)
	return derived
}

func convertJWTProviderSpecToV2(p JWTProviderSpec) v1alpha2.JWTProviderSpec {
	return v1alpha2.JWTProviderSpec{
		Name:          p.Name,
		IssuerURL:     p.IssuerURL,
		JWKSURL:       deriveJWKSURL(p.IssuerURL, p.JWKSURL),
		Audiences:     p.Audiences,
		ClaimMappings: convertClaimMappingsSpecToV2(p.ClaimMappings),
	}
}

func convertJWTProviderSpecToV1(p v1alpha2.JWTProviderSpec) JWTProviderSpec {
	return JWTProviderSpec{
		Name:          p.Name,
		IssuerURL:     p.IssuerURL,
		JWKSURL:       p.JWKSURL,
		Audiences:     p.Audiences,
		ClaimMappings: convertClaimMappingsSpecToV1(p.ClaimMappings),
	}
}

func convertJWTProviderListToV2(ps []JWTProviderSpec) []v1alpha2.JWTProviderSpec {
	if ps == nil {
		return nil
	}
	out := make([]v1alpha2.JWTProviderSpec, len(ps))
	for i, p := range ps {
		out[i] = convertJWTProviderSpecToV2(p)
	}
	return out
}

func convertJWTProviderListToV1(ps []v1alpha2.JWTProviderSpec) []JWTProviderSpec {
	if ps == nil {
		return nil
	}
	out := make([]JWTProviderSpec, len(ps))
	for i, p := range ps {
		out[i] = convertJWTProviderSpecToV1(p)
	}
	return out
}

func convertClaimMappingsSpecToV2(c *ClaimMappingsSpec) *v1alpha2.ClaimMappingsSpec {
	if c == nil {
		return nil
	}
	return &v1alpha2.ClaimMappingsSpec{Username: c.Username, Groups: c.Groups}
}

func convertClaimMappingsSpecToV1(c *v1alpha2.ClaimMappingsSpec) *ClaimMappingsSpec {
	if c == nil {
		return nil
	}
	return &ClaimMappingsSpec{Username: c.Username, Groups: c.Groups}
}

func convertInteractiveSpecToV2(s *InteractiveSpec) *v1alpha2.InteractiveSpec {
	if s == nil {
		return nil
	}
	return &v1alpha2.InteractiveSpec{
		Enabled: s.Enabled, SessionTTL: s.SessionTTL, InactivityTimeout: s.InactivityTimeout,
		MaxConcurrentSessions: s.MaxConcurrentSessions, RateLimitPerUser: s.RateLimitPerUser,
		JWTProviders: convertJWTProviderListToV2(s.JWTProviders), AllowInsecureJWKS: s.AllowInsecureJWKS,
	}
}

func convertInteractiveSpecToV1(s *v1alpha2.InteractiveSpec) *InteractiveSpec {
	if s == nil {
		return nil
	}
	return &InteractiveSpec{
		Enabled: s.Enabled, SessionTTL: s.SessionTTL, InactivityTimeout: s.InactivityTimeout,
		MaxConcurrentSessions: s.MaxConcurrentSessions, RateLimitPerUser: s.RateLimitPerUser,
		JWTProviders: convertJWTProviderListToV1(s.JWTProviders), AllowInsecureJWKS: s.AllowInsecureJWKS,
	}
}

// convertAlignmentCheckSpecToV2 implements F5: v1alpha1's inline
// {Provider,Model,Endpoint} LLM shape never had a working credentials path
// (same bug class as #237), so there is no live profile to reference --
// LLMProfileRef is left empty and the conversion is logged as lossy.
func convertAlignmentCheckSpecToV2(s AlignmentCheckSpec) v1alpha2.AlignmentCheckSpec {
	if s.LLM != nil && (s.LLM.Provider != "" || s.LLM.Model != "" || s.LLM.Endpoint != "") {
		conversionLog.Info("dropping kubernautAgent.alignmentCheck.llm on upgrade to v1alpha2: "+
			"no working credentials path existed in v1alpha1 (see #237); set alignmentCheck.llmProfileRef manually",
			"provider", s.LLM.Provider, "model", s.LLM.Model)
	}
	return v1alpha2.AlignmentCheckSpec{Enabled: s.Enabled, Timeout: s.Timeout, MaxStepTokens: s.MaxStepTokens}
}

// convertAlignmentCheckSpecToV1 implements F5's downgrade path: best-effort
// reconstruction of {Provider,Model,Endpoint} by looking up LLMProfileRef in
// the hub object's own spec.llmProfiles (shared, unchanged between
// versions) -- more complete than a bare empty LLM block, though Endpoint
// may differ from any credentials-bearing endpoint the v1alpha1 shape could
// never actually use anyway.
func convertAlignmentCheckSpecToV1(s v1alpha2.AlignmentCheckSpec, profiles map[string]v1alpha2.LLMProfileSpec) AlignmentCheckSpec {
	out := AlignmentCheckSpec{Enabled: s.Enabled, Timeout: s.Timeout, MaxStepTokens: s.MaxStepTokens}
	if s.LLMProfileRef == "" {
		return out
	}
	profile, ok := profiles[s.LLMProfileRef]
	if !ok {
		conversionLog.Info("dropping kubernautAgent.alignmentCheck.llmProfileRef on downgrade to v1alpha1: "+
			"referenced profile not found in spec.llmProfiles", "llmProfileRef", s.LLMProfileRef)
		return out
	}
	out.LLM = &AlignmentCheckLLMSpec{Provider: profile.Provider, Model: profile.Model, Endpoint: profile.Endpoint}
	return out
}

func convertKubernautAgentSpecToV2(s KubernautAgentSpec, _ map[string]LLMProfileSpec) v1alpha2.KubernautAgentSpec {
	return v1alpha2.KubernautAgentSpec{
		LLMProfileRef:                 s.LLMProfileRef,
		RuntimeConfigMapName:          s.RuntimeConfigMapName,
		PhaseModels:                   s.PhaseModels,
		MaxTurns:                      s.MaxTurns,
		Session:                       v1alpha2.SessionSpec{TTL: s.Session.TTL},
		Audit:                         v1alpha2.AuditSpec{Enabled: s.Audit.Enabled},
		AlignmentCheck:                convertAlignmentCheckSpecToV2(s.AlignmentCheck),
		Summarizer:                    v1alpha2.SummarizerSpec{Threshold: s.Summarizer.Threshold, MaxToolOutputSize: s.Summarizer.MaxToolOutputSize},
		Safety:                        convertSafetySpecToV2(s.Safety),
		Interactive:                   convertInteractiveSpecToV2(s.Interactive),
		AdditionalClusterRoleBindings: s.AdditionalClusterRoleBindings,
		ServerRateLimit:               (*v1alpha2.KARateLimitSpec)(s.ServerRateLimit),
		Shutdown:                      v1alpha2.ShutdownSpec{DrainSeconds: s.Shutdown.DrainSeconds},
		Logging:                       convertLoggingSpecToV2(s.Logging),
		Resources:                     s.Resources,
		Fleet:                         fleetOverrideFromOAuth2(s.FleetOAuth2CredentialsSecretRef),
	}
}

func convertKubernautAgentSpecToV1(s v1alpha2.KubernautAgentSpec, profiles map[string]v1alpha2.LLMProfileSpec) KubernautAgentSpec {
	return KubernautAgentSpec{
		LLMProfileRef:                   s.LLMProfileRef,
		RuntimeConfigMapName:            s.RuntimeConfigMapName,
		PhaseModels:                     s.PhaseModels,
		MaxTurns:                        s.MaxTurns,
		Session:                         SessionSpec{TTL: s.Session.TTL},
		Audit:                           AuditSpec{Enabled: s.Audit.Enabled},
		AlignmentCheck:                  convertAlignmentCheckSpecToV1(s.AlignmentCheck, profiles),
		Summarizer:                      SummarizerSpec{Threshold: s.Summarizer.Threshold, MaxToolOutputSize: s.Summarizer.MaxToolOutputSize},
		Safety:                          convertSafetySpecToV1(s.Safety),
		Interactive:                     convertInteractiveSpecToV1(s.Interactive),
		AdditionalClusterRoleBindings:   s.AdditionalClusterRoleBindings,
		ServerRateLimit:                 (*KARateLimitSpec)(s.ServerRateLimit),
		Shutdown:                        ShutdownSpec{DrainSeconds: s.Shutdown.DrainSeconds},
		Logging:                         convertLoggingSpecToV1(s.Logging),
		Resources:                       s.Resources,
		FleetOAuth2CredentialsSecretRef: fleetOverrideOAuth2ToV1(s.Fleet),
	}
}

func convertSafetySpecToV2(s SafetySpec) v1alpha2.SafetySpec {
	return v1alpha2.SafetySpec{
		Sanitization: v1alpha2.SanitizationSpec{InjectionPatternsEnabled: s.Sanitization.InjectionPatternsEnabled, CredentialScrubEnabled: s.Sanitization.CredentialScrubEnabled},
		Anomaly: v1alpha2.AnomalySpec{
			MaxToolCallsPerTool: s.Anomaly.MaxToolCallsPerTool, MaxTotalToolCalls: s.Anomaly.MaxTotalToolCalls,
			MaxRepeatedFailures: s.Anomaly.MaxRepeatedFailures,
		},
	}
}

func convertSafetySpecToV1(s v1alpha2.SafetySpec) SafetySpec {
	return SafetySpec{
		Sanitization: SanitizationSpec{InjectionPatternsEnabled: s.Sanitization.InjectionPatternsEnabled, CredentialScrubEnabled: s.Sanitization.CredentialScrubEnabled},
		Anomaly: AnomalySpec{
			MaxToolCallsPerTool: s.Anomaly.MaxToolCallsPerTool, MaxTotalToolCalls: s.Anomaly.MaxTotalToolCalls,
			MaxRepeatedFailures: s.Anomaly.MaxRepeatedFailures,
		},
	}
}

// ---------- LLMProfiles (unchanged, map copy) ----------

func convertLLMProfileSpecToV2(p LLMProfileSpec) v1alpha2.LLMProfileSpec {
	return v1alpha2.LLMProfileSpec{
		Provider: p.Provider, Model: p.Model, CredentialsSecretName: p.CredentialsSecretName, Endpoint: p.Endpoint,
		Temperature: p.Temperature, MaxRetries: p.MaxRetries, TimeoutSeconds: p.TimeoutSeconds,
		VertexProject: p.VertexProject, VertexLocation: p.VertexLocation, BedrockRegion: p.BedrockRegion,
		AzureAPIVersion: p.AzureAPIVersion, TLSCaFile: p.TLSCaFile, TLSCertFile: p.TLSCertFile,
		TLSKeyFile: p.TLSKeyFile, TLSClientSecretRef: p.TLSClientSecretRef,
		OAuth2:    v1alpha2.OAuth2Spec{Enabled: p.OAuth2.Enabled, TokenURL: p.OAuth2.TokenURL, Scopes: p.OAuth2.Scopes, CredentialsSecretRef: p.OAuth2.CredentialsSecretRef},
		Reasoning: (*v1alpha2.LLMReasoningSpec)(p.Reasoning),
	}
}

func convertLLMProfileSpecToV1(p v1alpha2.LLMProfileSpec) LLMProfileSpec {
	return LLMProfileSpec{
		Provider: p.Provider, Model: p.Model, CredentialsSecretName: p.CredentialsSecretName, Endpoint: p.Endpoint,
		Temperature: p.Temperature, MaxRetries: p.MaxRetries, TimeoutSeconds: p.TimeoutSeconds,
		VertexProject: p.VertexProject, VertexLocation: p.VertexLocation, BedrockRegion: p.BedrockRegion,
		AzureAPIVersion: p.AzureAPIVersion, TLSCaFile: p.TLSCaFile, TLSCertFile: p.TLSCertFile,
		TLSKeyFile: p.TLSKeyFile, TLSClientSecretRef: p.TLSClientSecretRef,
		OAuth2:    OAuth2Spec{Enabled: p.OAuth2.Enabled, TokenURL: p.OAuth2.TokenURL, Scopes: p.OAuth2.Scopes, CredentialsSecretRef: p.OAuth2.CredentialsSecretRef},
		Reasoning: (*LLMReasoningSpec)(p.Reasoning),
	}
}

func convertLLMProfilesToV2(profiles map[string]LLMProfileSpec) map[string]v1alpha2.LLMProfileSpec {
	if profiles == nil {
		return nil
	}
	out := make(map[string]v1alpha2.LLMProfileSpec, len(profiles))
	for k, v := range profiles {
		out[k] = convertLLMProfileSpecToV2(v)
	}
	return out
}

func convertLLMProfilesToV1(profiles map[string]v1alpha2.LLMProfileSpec) map[string]LLMProfileSpec {
	if profiles == nil {
		return nil
	}
	out := make(map[string]LLMProfileSpec, len(profiles))
	for k, v := range profiles {
		out[k] = convertLLMProfileSpecToV1(v)
	}
	return out
}

// ---------- F1: GatewaySpec ----------

func convertGatewaySpecToV2(s GatewaySpec) v1alpha2.GatewaySpec {
	return v1alpha2.GatewaySpec{
		Enabled: s.Enabled,
		Route:   v1alpha2.RouteSpec{Enabled: s.Route.Enabled, Hostname: s.Route.Hostname},
		Config: v1alpha2.GatewayConfigSpec{
			K8sRequestTimeout: s.Config.K8sRequestTimeout, TrustedProxyCIDRs: s.Config.TrustedProxyCIDRs,
			CORS: v1alpha2.GatewayCORSSpec{
				AllowedOrigins: s.Config.CORS.AllowedOrigins, AllowedMethods: s.Config.CORS.AllowedMethods,
				AllowCredentials: s.Config.CORS.AllowCredentials, MaxAge: s.Config.CORS.MaxAge,
			},
			DeduplicationCooldown: s.Config.DeduplicationCooldown,
		},
		Logging:   convertLoggingSpecToV2(s.Logging),
		Resources: s.Resources,
		Fleet:     fleetOverrideFromOAuth2(s.FleetOAuth2CredentialsSecretRef),
	}
}

func convertGatewaySpecToV1(s v1alpha2.GatewaySpec) GatewaySpec {
	return GatewaySpec{
		Enabled: s.Enabled,
		Route:   RouteSpec{Enabled: s.Route.Enabled, Hostname: s.Route.Hostname},
		Config: GatewayConfigSpec{
			K8sRequestTimeout: s.Config.K8sRequestTimeout, TrustedProxyCIDRs: s.Config.TrustedProxyCIDRs,
			CORS: GatewayCORSSpec{
				AllowedOrigins: s.Config.CORS.AllowedOrigins, AllowedMethods: s.Config.CORS.AllowedMethods,
				AllowCredentials: s.Config.CORS.AllowCredentials, MaxAge: s.Config.CORS.MaxAge,
			},
			DeduplicationCooldown: s.Config.DeduplicationCooldown,
		},
		Logging:                         convertLoggingSpecToV1(s.Logging),
		Resources:                       s.Resources,
		FleetOAuth2CredentialsSecretRef: fleetOverrideOAuth2ToV1(s.Fleet),
	}
}

// ---------- Unchanged leaf specs ----------

func convertAuthWebhookSpecToV2(s AuthWebhookSpec) v1alpha2.AuthWebhookSpec {
	return v1alpha2.AuthWebhookSpec{Logging: convertLoggingSpecToV2(s.Logging), Resources: s.Resources}
}

func convertAuthWebhookSpecToV1(s v1alpha2.AuthWebhookSpec) AuthWebhookSpec {
	return AuthWebhookSpec{Logging: convertLoggingSpecToV1(s.Logging), Resources: s.Resources}
}

func convertDataStorageSpecToV2(s DataStorageSpec) v1alpha2.DataStorageSpec {
	return v1alpha2.DataStorageSpec{
		EndpointPropagationDelay: s.EndpointPropagationDelay,
		Logging:                  convertLoggingSpecToV2(s.Logging),
		Resources:                s.Resources,
		Retention:                (*v1alpha2.RetentionSpec)(s.Retention),
		SigningCert:              (*v1alpha2.SigningCertSpec)(s.SigningCert),
	}
}

func convertDataStorageSpecToV1(s v1alpha2.DataStorageSpec) DataStorageSpec {
	return DataStorageSpec{
		EndpointPropagationDelay: s.EndpointPropagationDelay,
		Logging:                  convertLoggingSpecToV1(s.Logging),
		Resources:                s.Resources,
		Retention:                (*RetentionSpec)(s.Retention),
		SigningCert:              (*SigningCertSpec)(s.SigningCert),
	}
}

func convertConsoleSpecToV2(s ConsoleSpec) v1alpha2.ConsoleSpec {
	return v1alpha2.ConsoleSpec{
		Enabled:   s.Enabled,
		Auth:      v1alpha2.ConsoleAuthSpec{SecretName: s.Auth.SecretName},
		Route:     v1alpha2.ConsoleRouteSpec{Enabled: s.Route.Enabled, Host: s.Route.Host},
		Resources: s.Resources,
	}
}

func convertConsoleSpecToV1(s v1alpha2.ConsoleSpec) ConsoleSpec {
	return ConsoleSpec{
		Enabled:   s.Enabled,
		Auth:      ConsoleAuthSpec{SecretName: s.Auth.SecretName},
		Route:     ConsoleRouteSpec{Enabled: s.Route.Enabled, Host: s.Route.Host},
		Resources: s.Resources,
	}
}

func convertFleetSpecToV2(s FleetSpec) v1alpha2.FleetSpec {
	return v1alpha2.FleetSpec{
		Enabled: s.Enabled, Backend: s.Backend, Endpoint: s.Endpoint, CASecretName: s.CASecretName,
		TokenSecretName: s.TokenSecretName, MCPGatewayEndpoint: s.MCPGatewayEndpoint, MCPGatewayType: s.MCPGatewayType,
		OAuth2:              v1alpha2.OAuth2Spec{Enabled: s.OAuth2.Enabled, TokenURL: s.OAuth2.TokenURL, Scopes: s.OAuth2.Scopes, CredentialsSecretRef: s.OAuth2.CredentialsSecretRef},
		MCPGatewayNamespace: s.MCPGatewayNamespace,
	}
}

func convertFleetSpecToV1(s v1alpha2.FleetSpec) FleetSpec {
	return FleetSpec{
		Enabled: s.Enabled, Backend: s.Backend, Endpoint: s.Endpoint, CASecretName: s.CASecretName,
		TokenSecretName: s.TokenSecretName, MCPGatewayEndpoint: s.MCPGatewayEndpoint, MCPGatewayType: s.MCPGatewayType,
		OAuth2:              OAuth2Spec{Enabled: s.OAuth2.Enabled, TokenURL: s.OAuth2.TokenURL, Scopes: s.OAuth2.Scopes, CredentialsSecretRef: s.OAuth2.CredentialsSecretRef},
		MCPGatewayNamespace: s.MCPGatewayNamespace,
	}
}

// ---------- F1: FleetMetadataCacheSpec ----------

func convertFleetMetadataCacheSpecToV2(s FleetMetadataCacheSpec) v1alpha2.FleetMetadataCacheSpec {
	return v1alpha2.FleetMetadataCacheSpec{
		Enabled:      s.Enabled,
		Fleet:        fleetOverrideFromOAuth2AndNamespace(s.FleetOAuth2CredentialsSecretRef, s.MCPGatewayNamespace),
		SyncInterval: s.SyncInterval,
		KeyTTL:       s.KeyTTL,
		Logging:      convertLoggingSpecToV2(s.Logging),
		Resources:    s.Resources,
	}
}

func convertFleetMetadataCacheSpecToV1(s v1alpha2.FleetMetadataCacheSpec) FleetMetadataCacheSpec {
	out := FleetMetadataCacheSpec{
		Enabled:      s.Enabled,
		SyncInterval: s.SyncInterval,
		KeyTTL:       s.KeyTTL,
		Logging:      convertLoggingSpecToV1(s.Logging),
		Resources:    s.Resources,
	}
	if s.Fleet != nil {
		out.FleetOAuth2CredentialsSecretRef = s.Fleet.OAuth2CredentialsSecretRef
		out.MCPGatewayNamespace = s.Fleet.Namespace
	}
	return out
}

// ---------- F1 + F6: APIFrontendSpec ----------

func convertAPIFrontendSpecToV2(s APIFrontendSpec) v1alpha2.APIFrontendSpec {
	return v1alpha2.APIFrontendSpec{
		Enabled:               s.Enabled,
		Route:                 v1alpha2.APIFrontendRouteSpec{Enabled: s.Route.Enabled, Hostname: s.Route.Hostname},
		SPIRE:                 v1alpha2.APIFrontendSPIRESpec{Enabled: s.SPIRE.Enabled, ClassName: s.SPIRE.ClassName, TrustDomain: s.SPIRE.TrustDomain},
		Auth:                  convertAPIFrontendAuthSpecToV2(s.Auth),
		RateLimit:             v1alpha2.APIFrontendRateLimitSpec(s.RateLimit),
		Shutdown:              v1alpha2.APIFrontendShutdownSpec{DrainSeconds: s.Shutdown.DrainSeconds},
		LLMProfileRef:         s.LLMProfileRef,
		SeverityTriage:        convertSeverityTriageSpecToV2(s.SeverityTriage),
		AgentCardURL:          s.AgentCardURL,
		RBACRolesConfigMapRef: convertConfigMapRefToV2(s.RBACRolesConfigMapRef),
		RBAC:                  convertAPIFrontendRBACSpecToV2(s.RBAC),
		Logging:               convertLoggingSpecToV2(s.Logging),
		MetricsPort:           s.MetricsPort,
		HealthPort:            s.HealthPort,
		Resources:             s.Resources,
		Fleet:                 fleetOverrideFromOAuth2(s.FleetOAuth2CredentialsSecretRef),
	}
}

func convertAPIFrontendSpecToV1(s v1alpha2.APIFrontendSpec) APIFrontendSpec {
	if s.Fleet != nil && s.Fleet.Namespace != "" {
		conversionLog.Info("dropping apiFrontend.fleet.namespace on downgrade to v1alpha1: field has no v1alpha1 equivalent")
	}
	return APIFrontendSpec{
		Enabled:                         s.Enabled,
		Route:                           APIFrontendRouteSpec{Enabled: s.Route.Enabled, Hostname: s.Route.Hostname},
		SPIRE:                           APIFrontendSPIRESpec{Enabled: s.SPIRE.Enabled, ClassName: s.SPIRE.ClassName, TrustDomain: s.SPIRE.TrustDomain},
		Auth:                            convertAPIFrontendAuthSpecToV1(s.Auth),
		RateLimit:                       APIFrontendRateLimitSpec(s.RateLimit),
		Shutdown:                        APIFrontendShutdownSpec{DrainSeconds: s.Shutdown.DrainSeconds},
		LLMProfileRef:                   s.LLMProfileRef,
		SeverityTriage:                  convertSeverityTriageSpecToV1(s.SeverityTriage),
		AgentCardURL:                    s.AgentCardURL,
		RBACRolesConfigMapRef:           convertConfigMapRefToV1(s.RBACRolesConfigMapRef), //nolint:staticcheck // round-trip conversion must preserve the deprecated field, not migrate off it
		RBAC:                            convertAPIFrontendRBACSpecToV1(s.RBAC),
		Logging:                         convertLoggingSpecToV1(s.Logging),
		MetricsPort:                     s.MetricsPort,
		HealthPort:                      s.HealthPort,
		Resources:                       s.Resources,
		FleetOAuth2CredentialsSecretRef: fleetOverrideOAuth2ToV1(s.Fleet),
	}
}

func convertAPIFrontendAuthSpecToV2(s APIFrontendAuthSpec) v1alpha2.APIFrontendAuthSpec {
	return v1alpha2.APIFrontendAuthSpec{
		IssuerURL: s.IssuerURL, Audience: s.Audience, TokenReviewAudience: s.TokenReviewAudience,
		JWKSURL: s.JWKSURL, OIDCCAFile: s.OIDCCAFile, AllowInsecureIssuers: s.AllowInsecureIssuers,
		JWTProviders: convertJWTProviderListToV2(s.JWTProviders),
	}
}

func convertAPIFrontendAuthSpecToV1(s v1alpha2.APIFrontendAuthSpec) APIFrontendAuthSpec {
	return APIFrontendAuthSpec{
		IssuerURL: s.IssuerURL, Audience: s.Audience, TokenReviewAudience: s.TokenReviewAudience,
		JWKSURL: s.JWKSURL, OIDCCAFile: s.OIDCCAFile, AllowInsecureIssuers: s.AllowInsecureIssuers,
		JWTProviders: convertJWTProviderListToV1(s.JWTProviders),
	}
}

func convertSeverityTriageSpecToV2(s *APIFrontendSeverityTriageSpec) *v1alpha2.APIFrontendSeverityTriageSpec {
	if s == nil {
		return nil
	}
	return &v1alpha2.APIFrontendSeverityTriageSpec{LLMProfileRef: s.LLMProfileRef, LLMEnabled: s.LLMEnabled}
}

func convertSeverityTriageSpecToV1(s *v1alpha2.APIFrontendSeverityTriageSpec) *APIFrontendSeverityTriageSpec {
	if s == nil {
		return nil
	}
	return &APIFrontendSeverityTriageSpec{LLMProfileRef: s.LLMProfileRef, LLMEnabled: s.LLMEnabled}
}

func convertToolRoleBindingsToV2(rb []ToolRoleBinding) []v1alpha2.ToolRoleBinding {
	if rb == nil {
		return nil
	}
	out := make([]v1alpha2.ToolRoleBinding, len(rb))
	for i, r := range rb {
		out[i] = v1alpha2.ToolRoleBinding{Role: r.Role, ClusterRoleName: r.ClusterRoleName, Groups: r.Groups}
	}
	return out
}

func convertToolRoleBindingsToV1(rb []v1alpha2.ToolRoleBinding) []ToolRoleBinding {
	if rb == nil {
		return nil
	}
	out := make([]ToolRoleBinding, len(rb))
	for i, r := range rb {
		out[i] = ToolRoleBinding{Role: r.Role, ClusterRoleName: r.ClusterRoleName, Groups: r.Groups}
	}
	return out
}

func convertAPIFrontendRBACSpecToV2(s *APIFrontendRBACSpec) *v1alpha2.APIFrontendRBACSpec {
	if s == nil {
		return nil
	}
	return &v1alpha2.APIFrontendRBACSpec{
		SARCacheTTL: s.SARCacheTTL, RoleBindings: convertToolRoleBindingsToV2(s.RoleBindings), ConsoleAccessGroups: s.ConsoleAccessGroups,
	}
}

func convertAPIFrontendRBACSpecToV1(s *v1alpha2.APIFrontendRBACSpec) *APIFrontendRBACSpec {
	if s == nil {
		return nil
	}
	return &APIFrontendRBACSpec{
		SARCacheTTL: s.SARCacheTTL, RoleBindings: convertToolRoleBindingsToV1(s.RoleBindings), ConsoleAccessGroups: s.ConsoleAccessGroups,
	}
}

// ---------- F3: NetworkPoliciesSpec ----------

// convertNetworkPoliciesSpecToV2 implements F3: v1alpha1's Enabled *bool
// opt-out has no v1alpha2 equivalent -- NetworkPolicies are unconditional.
// When the source CR explicitly disabled them (today's default), this is a
// behavior change on upgrade (policies will now be created); logged so
// operators upgrading from v1alpha1 are aware. Always returns a zero-value
// NetworkPoliciesSpec today because v1alpha1 has no source fields for any of
// v1alpha2's new override groups (CIDRs, per-component ingress/egress,
// etc.) -- kept as a function (rather than inlined) so the F3 upgrade path
// has a single, obvious call site to extend if a future v1alpha3 needs to.
//
//nolint:unparam // intentionally always zero-value; see doc comment above
func convertNetworkPoliciesSpecToV2(s NetworkPoliciesSpec) v1alpha2.NetworkPoliciesSpec {
	if s.Enabled == nil || !*s.Enabled {
		conversionLog.Info("networkPolicies.enabled was false (or unset) in v1alpha1; " +
			"v1alpha2 always creates NetworkPolicies -- this CR will now get them where it previously did not " +
			"(see ADR-CRD-001 F3 and the v1alpha2 migration guide)")
	}
	return v1alpha2.NetworkPoliciesSpec{}
}

// convertNetworkPoliciesSpecToV1 implements F3's downgrade path: sets
// enabled:true unconditionally, reflecting v1alpha2's actual (always-on)
// runtime behavior, for round-trip fidelity.
func convertNetworkPoliciesSpecToV1(_ v1alpha2.NetworkPoliciesSpec, _, _ string) NetworkPoliciesSpec {
	enabled := true
	return NetworkPoliciesSpec{Enabled: &enabled}
}

// ---------- Status (unchanged shape) ----------

func convertStatusToV2(s KubernautStatus) v1alpha2.KubernautStatus {
	return v1alpha2.KubernautStatus{
		Phase:                       v1alpha2.KubernautPhase(s.Phase),
		Conditions:                  s.Conditions,
		Services:                    convertServiceStatusListToV2(s.Services),
		LastMigrationHash:           s.LastMigrationHash,
		LastMigrationTime:           s.LastMigrationTime,
		BoundAdditionalClusterRoles: s.BoundAdditionalClusterRoles,
		BoundToolRoleBindings:       s.BoundToolRoleBindings,
	}
}

func convertStatusToV1(s v1alpha2.KubernautStatus) KubernautStatus {
	return KubernautStatus{
		Phase:                       KubernautPhase(s.Phase),
		Conditions:                  s.Conditions,
		Services:                    convertServiceStatusListToV1(s.Services),
		LastMigrationHash:           s.LastMigrationHash,
		LastMigrationTime:           s.LastMigrationTime,
		BoundAdditionalClusterRoles: s.BoundAdditionalClusterRoles,
		BoundToolRoleBindings:       s.BoundToolRoleBindings,
	}
}

func convertServiceStatusListToV2(ss []ServiceStatus) []v1alpha2.ServiceStatus {
	if ss == nil {
		return nil
	}
	out := make([]v1alpha2.ServiceStatus, len(ss))
	for i, s := range ss {
		out[i] = v1alpha2.ServiceStatus{Name: s.Name, Ready: s.Ready, ReadyReplicas: s.ReadyReplicas, DesiredReplicas: s.DesiredReplicas}
	}
	return out
}

func convertServiceStatusListToV1(ss []v1alpha2.ServiceStatus) []ServiceStatus {
	if ss == nil {
		return nil
	}
	out := make([]ServiceStatus, len(ss))
	for i, s := range ss {
		out[i] = ServiceStatus{Name: s.Name, Ready: s.Ready, ReadyReplicas: s.ReadyReplicas, DesiredReplicas: s.DesiredReplicas}
	}
	return out
}

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
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kubernautv1alpha1 "github.com/jordigilh/kubernaut-operator/api/v1alpha1"
)

// APIFrontendServiceMonitor builds the ServiceMonitor for the apifrontend service.
func APIFrontendServiceMonitor(kn *kubernautv1alpha1.Kubernaut) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: ObjectMeta(kn, "apifrontend-monitor", ComponentAPIFrontend),
		Spec: monitoringv1.ServiceMonitorSpec{
			JobLabel: "app.kubernetes.io/name",
			Selector: metav1.LabelSelector{
				MatchLabels: SelectorLabels(ComponentAPIFrontend),
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:     "metrics",
					Path:     "/metrics",
					Interval: monitoringv1.Duration("15s"),
					Scheme:   schemePtr("http"),
					RelabelConfigs: []monitoringv1.RelabelConfig{
						{
							SourceLabels: []monitoringv1.LabelName{"__address__"},
							TargetLabel:  "job",
							Replacement:  strPtr("apifrontend"),
						},
					},
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{kn.Namespace},
			},
		},
	}
}

// APIFrontendPrometheusRule builds the PrometheusRule with alert rules for AF.
func APIFrontendPrometheusRule(kn *kubernautv1alpha1.Kubernaut) *monitoringv1.PrometheusRule {
	return &monitoringv1.PrometheusRule{
		ObjectMeta: ObjectMeta(kn, "apifrontend-rules", ComponentAPIFrontend),
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{
					Name: "apifrontend.availability",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "ApifrontendDown",
							Expr:        intstr.FromString(`up{job="apifrontend"} == 0`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "API Frontend is down", "description": "The API Frontend service has been unreachable for more than 5 minutes.", "runbook_url": "https://docs.kubernaut.ai/runbooks/apifrontend-down"},
						},
					},
				},
				{
					Name: "apifrontend.latency",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "ApifrontendHighLatencyP95",
							Expr:        intstr.FromString(`histogram_quantile(0.95, sum(rate(af_http_request_duration_seconds_bucket{job="apifrontend"}[5m])) by (le)) > 0.5`),
							For:         durationPtr("2m"),
							Labels:      map[string]string{"severity": "warning"},
							Annotations: map[string]string{"summary": "API Frontend P95 latency > 500ms", "runbook_url": "https://docs.kubernaut.ai/runbooks/apifrontend-high-latency"},
						},
						{
							Alert:       "ApifrontendHighLatencyP99",
							Expr:        intstr.FromString(`histogram_quantile(0.99, sum(rate(af_http_request_duration_seconds_bucket{job="apifrontend"}[5m])) by (le)) > 1.0`),
							For:         durationPtr("2m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "API Frontend P99 latency > 1s", "runbook_url": "https://docs.kubernaut.ai/runbooks/apifrontend-high-latency"},
						},
					},
				},
				{
					Name: "apifrontend.errors",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "ApifrontendHighErrorRate",
							Expr:        intstr.FromString(`sum(rate(af_http_requests_total{job="apifrontend",status=~"5.."}[5m])) / sum(rate(af_http_requests_total{job="apifrontend"}[5m])) > 0.01`),
							For:         durationPtr("2m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "API Frontend error rate > 1%", "runbook_url": "https://docs.kubernaut.ai/runbooks/apifrontend-high-error-rate"},
						},
					},
				},
				{
					Name: "apifrontend.auth",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "ApifrontendAuthFailureSpike",
							Expr:        intstr.FromString(`sum(rate(af_http_requests_total{job="apifrontend",status="401"}[5m])) / sum(rate(af_http_requests_total{job="apifrontend"}[5m])) > 0.1`),
							For:         durationPtr("2m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "API Frontend auth failure rate > 10%", "runbook_url": "https://docs.kubernaut.ai/runbooks/apifrontend-auth-failure"},
						},
					},
				},
				{
					Name: "apifrontend.circuitbreaker",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "ApifrontendCircuitBreakerOpenKA",
							Expr:        intstr.FromString(`af_circuit_breaker_state{job="apifrontend",dependency="ka"} == 2`),
							For:         durationPtr("2m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "KA circuit breaker is open", "runbook_url": "https://docs.kubernaut.ai/runbooks/apifrontend-circuit-breaker"},
						},
						{
							Alert:       "ApifrontendCircuitBreakerOpenDS",
							Expr:        intstr.FromString(`af_circuit_breaker_state{job="apifrontend",dependency="ds"} == 2`),
							For:         durationPtr("2m"),
							Labels:      map[string]string{"severity": "warning"},
							Annotations: map[string]string{"summary": "DS circuit breaker is open", "runbook_url": "https://docs.kubernaut.ai/runbooks/apifrontend-circuit-breaker"},
						},
					},
				},
				{
					Name: "apifrontend.tools",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "ApifrontendToolErrorRate",
							Expr:        intstr.FromString(`sum(rate(af_tool_calls_total{job="apifrontend",result=~"error|timeout|panic"}[5m])) / sum(rate(af_tool_calls_total{job="apifrontend"}[5m])) > 0.05`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "MCP tool error rate > 5%", "runbook_url": "https://docs.kubernaut.ai/runbooks/apifrontend-tool-error-rate"},
						},
					},
				},
			},
		},
	}
}

// DataStorageServiceMonitor builds the ServiceMonitor for the DataStorage service.
func DataStorageServiceMonitor(kn *kubernautv1alpha1.Kubernaut) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: ObjectMeta(kn, "datastorage-monitor", ComponentDataStorage),
		Spec: monitoringv1.ServiceMonitorSpec{
			JobLabel: "app.kubernetes.io/name",
			Selector: metav1.LabelSelector{
				MatchLabels: SelectorLabels(ComponentDataStorage),
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:     "metrics",
					Path:     "/metrics",
					Interval: monitoringv1.Duration("15s"),
					Scheme:   schemePtr("http"),
					RelabelConfigs: []monitoringv1.RelabelConfig{
						{
							SourceLabels: []monitoringv1.LabelName{"__address__"},
							TargetLabel:  "job",
							Replacement:  strPtr("datastorage"),
						},
					},
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{kn.Namespace},
			},
		},
	}
}

// DataStoragePrometheusRule builds the PrometheusRule with DLQ and health alerts for DS.
func DataStoragePrometheusRule(kn *kubernautv1alpha1.Kubernaut) *monitoringv1.PrometheusRule {
	return &monitoringv1.PrometheusRule{
		ObjectMeta: ObjectMeta(kn, "datastorage-rules", ComponentDataStorage),
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{
					Name: "datastorage.availability",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "DataStorageDown",
							Expr:        intstr.FromString(`up{job="datastorage"} == 0`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "DataStorage is down"},
						},
					},
				},
				{
					Name: "datastorage.dlq",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "DataStorageDLQDepthHigh",
							Expr:        intstr.FromString(`ds_dlq_depth{job="datastorage"} > 100`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "warning"},
							Annotations: map[string]string{"summary": "DS DLQ depth > 100 for 5m"},
						},
						{
							Alert:       "DataStorageDLQProcessingErrors",
							Expr:        intstr.FromString(`rate(ds_dlq_processing_errors_total{job="datastorage"}[5m]) > 0.1`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "DS DLQ processing error rate elevated"},
						},
					},
				},
				{
					Name: "datastorage.latency",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "DataStorageHighLatencyP95",
							Expr:        intstr.FromString(`histogram_quantile(0.95, sum(rate(ds_http_request_duration_seconds_bucket{job="datastorage"}[5m])) by (le)) > 1`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "warning"},
							Annotations: map[string]string{"summary": "DataStorage P95 latency > 1s"},
						},
					},
				},
				{
					Name: "datastorage.database",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "DataStorageDBConnectionPoolExhausted",
							Expr:        intstr.FromString(`ds_db_pool_idle_connections{job="datastorage"} == 0 and ds_db_pool_active_connections{job="datastorage"} >= ds_db_pool_max_connections{job="datastorage"}`),
							For:         durationPtr("2m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "DS database connection pool exhausted"},
						},
					},
				},
			},
		},
	}
}

// KubernautAgentServiceMonitor builds the ServiceMonitor for the kubernaut-agent service.
func KubernautAgentServiceMonitor(kn *kubernautv1alpha1.Kubernaut) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: ObjectMeta(kn, "kubernautagent-monitor", ComponentKubernautAgent),
		Spec: monitoringv1.ServiceMonitorSpec{
			JobLabel: "app.kubernetes.io/name",
			Selector: metav1.LabelSelector{
				MatchLabels: SelectorLabels(ComponentKubernautAgent),
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:     "metrics",
					Path:     "/metrics",
					Interval: monitoringv1.Duration("15s"),
					Scheme:   schemePtr("http"),
					RelabelConfigs: []monitoringv1.RelabelConfig{
						{
							SourceLabels: []monitoringv1.LabelName{"__address__"},
							TargetLabel:  "job",
							Replacement:  strPtr("kubernautagent"),
						},
					},
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{kn.Namespace},
			},
		},
	}
}

// KubernautAgentPrometheusRule builds the PrometheusRule with alert rules for KA.
func KubernautAgentPrometheusRule(kn *kubernautv1alpha1.Kubernaut) *monitoringv1.PrometheusRule {
	return &monitoringv1.PrometheusRule{
		ObjectMeta: ObjectMeta(kn, "kubernautagent-rules", ComponentKubernautAgent),
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{
					Name: "kubernautagent.availability",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "KubernautAgentDown",
							Expr:        intstr.FromString(`up{job="kubernautagent"} == 0`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "Kubernaut Agent is down", "description": "The Kubernaut Agent service has been unreachable for more than 5 minutes.", "runbook_url": "https://docs.kubernaut.ai/runbooks/kubernautagent-down"},
						},
					},
				},
				{
					Name: "kubernautagent.sessions",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "KubernautAgentSessionDurationHigh",
							Expr:        intstr.FromString(`histogram_quantile(0.95, sum(rate(ka_session_duration_seconds_bucket{job="kubernautagent"}[15m])) by (le)) > 600`),
							For:         durationPtr("10m"),
							Labels:      map[string]string{"severity": "warning"},
							Annotations: map[string]string{"summary": "KA P95 session duration > 10 minutes", "runbook_url": "https://docs.kubernaut.ai/runbooks/kubernautagent-session-duration"},
						},
						{
							Alert:       "KubernautAgentActiveSessionsSaturated",
							Expr:        intstr.FromString(`ka_active_sessions{job="kubernautagent"} >= 10`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "warning"},
							Annotations: map[string]string{"summary": "KA active sessions >= 10 for 5m", "runbook_url": "https://docs.kubernaut.ai/runbooks/kubernautagent-sessions-saturated"},
						},
					},
				},
				{
					Name: "kubernautagent.tools",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "KubernautAgentToolErrorRate",
							Expr:        intstr.FromString(`sum(rate(ka_tool_calls_total{job="kubernautagent",result=~"error|timeout|panic"}[5m])) / sum(rate(ka_tool_calls_total{job="kubernautagent"}[5m])) > 0.05`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "KA tool error rate > 5%", "runbook_url": "https://docs.kubernaut.ai/runbooks/kubernautagent-tool-error-rate"},
						},
					},
				},
				{
					Name: "kubernautagent.llm",
					Rules: []monitoringv1.Rule{
						{
							Alert:       "KubernautAgentLLMErrorRate",
							Expr:        intstr.FromString(`sum(rate(ka_llm_requests_total{job="kubernautagent",status="error"}[5m])) / sum(rate(ka_llm_requests_total{job="kubernautagent"}[5m])) > 0.05`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "critical"},
							Annotations: map[string]string{"summary": "KA LLM error rate > 5%", "runbook_url": "https://docs.kubernaut.ai/runbooks/kubernautagent-llm-error-rate"},
						},
						{
							Alert:       "KubernautAgentLLMHighLatency",
							Expr:        intstr.FromString(`histogram_quantile(0.95, sum(rate(ka_llm_request_duration_seconds_bucket{job="kubernautagent"}[5m])) by (le)) > 30`),
							For:         durationPtr("5m"),
							Labels:      map[string]string{"severity": "warning"},
							Annotations: map[string]string{"summary": "KA LLM P95 latency > 30s", "runbook_url": "https://docs.kubernaut.ai/runbooks/kubernautagent-llm-latency"},
						},
					},
				},
			},
		},
	}
}

func durationPtr(d monitoringv1.Duration) *monitoringv1.Duration { return &d }
func schemePtr(s monitoringv1.Scheme) *monitoringv1.Scheme       { return &s }

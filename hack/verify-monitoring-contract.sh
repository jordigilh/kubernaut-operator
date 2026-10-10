#!/usr/bin/env bash

# shellcheck disable=SC2016

set -euo pipefail

# E2E-MON-513-001

namespace="${1:-${KUBERNAUT_NAMESPACE:-}}"
kubectl_bin="${KUBECTL_BIN:-kubectl}"
jq_bin="${JQ_BIN:-jq}"

fail() {
	echo "monitoring contract validation failed: $*" >&2
	exit 1
}

[[ -n "${namespace}" ]] || fail "usage: $0 <namespace>"
command -v "${kubectl_bin}" >/dev/null 2>&1 || fail "kubectl binary not found: ${kubectl_bin}"
command -v "${jq_bin}" >/dev/null 2>&1 || fail "jq binary not found: ${jq_bin}"

kubectl_args=(-n "${namespace}")
if [[ -n "${KUBECTL_CONTEXT:-}" ]]; then
	kubectl_args+=(--context "${KUBECTL_CONTEXT}")
fi

get_json() {
	local resource=$1
	"${kubectl_bin}" "${kubectl_args[@]}" get "${resource}" -o json
}

services_json=$(get_json services) || fail "unable to read Services in namespace ${namespace}"
deployments_json=$(get_json deployments) || fail "unable to read Deployments in namespace ${namespace}"
monitors_json=$(get_json servicemonitors.monitoring.coreos.com) || fail "unable to read ServiceMonitors in namespace ${namespace}"

operator_label='app.kubernetes.io/managed-by'
operator_value='kubernaut-operator'

operator_service_count=$(
	printf '%s' "${services_json}" |
		"${jq_bin}" --arg label "${operator_label}" --arg value "${operator_value}" \
			'[.items[] | select(.metadata.labels[$label] == $value)] | length'
)
operator_monitor_count=$(
	printf '%s' "${monitors_json}" |
		"${jq_bin}" --arg label "${operator_label}" --arg value "${operator_value}" \
			'[.items[] | select(.metadata.labels[$label] == $value)] | length'
)

if [[ -n "${EXPECTED_OPERATOR_SERVICES:-}" && "${operator_service_count}" -ne "${EXPECTED_OPERATOR_SERVICES}" ]]; then
	fail "expected ${EXPECTED_OPERATOR_SERVICES} operator Services, found ${operator_service_count}"
fi
if [[ -n "${EXPECTED_MONITORS:-}" && "${operator_monitor_count}" -ne "${EXPECTED_MONITORS}" ]]; then
	fail "expected ${EXPECTED_MONITORS} operator ServiceMonitors, found ${operator_monitor_count}"
fi

if "${jq_bin}" -e --arg label "${operator_label}" --arg value "${operator_value}" '
	[.items[] | select(.metadata.labels[$label] == $value and .metadata.name == "authwebhook-monitor")] | length == 0
' <<<"${monitors_json}" >/dev/null; then
	:
else
	fail "operator-owned authwebhook-monitor must not exist"
fi

while IFS= read -r monitor; do
	monitor_name=$(printf '%s' "${monitor}" | "${jq_bin}" -r '.metadata.name')
	selector=$(printf '%s' "${monitor}" | "${jq_bin}" -c '.spec.selector.matchLabels // {}')

	service=$(
		printf '%s' "${services_json}" |
			"${jq_bin}" -c --argjson selector "${selector}" '
				def matches($labels; $wanted):
					all($wanted | to_entries[]; $labels[.key] == .value);
				[.items[] | select(matches(.metadata.labels; $selector))] | .[0] // empty
			'
	)
	[[ -n "${service}" ]] || fail "ServiceMonitor ${monitor_name} selects no Service"

	while IFS= read -r endpoint_port; do
		[[ -n "${endpoint_port}" ]] || fail "ServiceMonitor ${monitor_name} has an unnamed endpoint port"
		if ! printf '%s' "${service}" | "${jq_bin}" -e --arg port "${endpoint_port}" \
			'any(.spec.ports[]?; .name == $port)' >/dev/null; then
			service_name=$(printf '%s' "${service}" | "${jq_bin}" -r '.metadata.name')
			fail "ServiceMonitor ${monitor_name} endpoint ${endpoint_port} is not exposed by Service ${service_name}"
		fi
	done < <(printf '%s' "${monitor}" | "${jq_bin}" -r '.spec.endpoints[]?.port // empty')
done < <(
	printf '%s' "${monitors_json}" |
		"${jq_bin}" -c --arg label "${operator_label}" --arg value "${operator_value}" \
			'.items[] | select(.metadata.labels[$label] == $value)'
)

while IFS= read -r service; do
	service_name=$(printf '%s' "${service}" | "${jq_bin}" -r '.metadata.name')
	component=$(printf '%s' "${service}" | "${jq_bin}" -r '.spec.selector.app // empty')
	[[ -n "${component}" ]] || fail "operator Service ${service_name} has no app selector"

	deployment=$(
		printf '%s' "${deployments_json}" |
			"${jq_bin}" -c --arg component "${component}" \
				'[.items[] | select(.spec.selector.matchLabels.app == $component)] | .[0] // empty'
	)
	[[ -n "${deployment}" ]] || fail "Service ${service_name} selects component ${component} without a Deployment"

	while IFS= read -r service_port; do
		port_name=$(printf '%s' "${service_port}" | "${jq_bin}" -r '.name')
		target_port=$(printf '%s' "${service_port}" | "${jq_bin}" -r '.targetPort // .port | tostring')
		if ! printf '%s' "${deployment}" | "${jq_bin}" -e --arg target "${target_port}" '
			[.spec.template.spec.containers[]?.ports[]?]
			| any(.[]; (.containerPort | tostring) == $target or (.name // "") == $target)
		' >/dev/null; then
			deployment_name=$(printf '%s' "${deployment}" | "${jq_bin}" -r '.metadata.name')
			fail "Service ${service_name} port ${port_name} targets ${target_port}, absent from Deployment ${deployment_name}"
		fi
	done < <(printf '%s' "${service}" | "${jq_bin}" -c '.spec.ports[]?')
done < <(
	printf '%s' "${services_json}" |
		"${jq_bin}" -c --arg label "${operator_label}" --arg value "${operator_value}" \
			'.items[] | select(.metadata.labels[$label] == $value)'
)

echo "LIVE_MONITORING_CONTRACT_PASS monitors=${operator_monitor_count} operatorServices=${operator_service_count}"

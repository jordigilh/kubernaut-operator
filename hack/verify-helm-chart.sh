#!/usr/bin/env bash

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
chart_dir="${repo_root}/charts/kubernaut-operator"
helm_bin="${HELM_BIN:-helm}"

fail() {
	echo "Helm chart validation failed: $*" >&2
	exit 1
}

[[ -d "${chart_dir}" ]] || fail "chart directory is missing"
command -v "${helm_bin}" >/dev/null 2>&1 || fail "Helm binary not found: ${helm_bin}"

cmp -s "${repo_root}/config/crd/bases/kubernaut.ai_kubernauts.yaml" \
	"${chart_dir}/files/kubernaut.ai_kubernauts.yaml" \
	|| fail "chart CRD source is out of sync with config/crd/bases"

cmp -s "${repo_root}/config/rbac/role.yaml" \
	"${chart_dir}/files/manager-role.yaml" \
	|| fail "chart manager RBAC source is out of sync with config/rbac/role.yaml"

"${helm_bin}" template kubernaut-operator "${chart_dir}" \
	--namespace default --include-crds >/dev/null \
	|| fail "default chart render failed"

"${helm_bin}" template kubernaut-operator "${chart_dir}" \
	--namespace default --set webhook.tls.mode=manual \
	--set webhook.tls.existingSecret=operator-webhook-cert \
	--set webhook.tls.caBundle=Y2E= >/dev/null \
	|| fail "manual TLS chart render failed"

"${helm_bin}" template kubernaut-operator "${chart_dir}" \
	--namespace default --set webhook.tls.mode=certManager \
	--set webhook.tls.certManager.issuerRef.name=operator-issuer \
	--set webhook.tls.certManager.issuerRef.kind=ClusterIssuer >/dev/null \
	|| fail "cert-manager TLS chart render failed"

"${helm_bin}" template kubernaut-operator "${chart_dir}" \
	--namespace default --set webhook.tls.mode=openshift >/dev/null \
	|| fail "OpenShift TLS chart render failed"

echo "Helm chart validation passed"

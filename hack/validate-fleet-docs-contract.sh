#!/usr/bin/env bash

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

fail() {
	echo "Fleet documentation contract validation failed: $*" >&2
	exit 1
}

# These are active user-facing contract surfaces. Historical design notes,
# old issue test plans, and released changelog entries are intentionally not
# scanned: they describe the contract that existed at that point in time.
active_files=(
	"${repo_root}/docs/installation/03-deploy.md"
	"${repo_root}/docs/installation/04-fleet-mcp-gateway.md"
	"${repo_root}/docs/installation/05-fleet-multi-cluster.md"
	"${repo_root}/docs/installation/README.md"
	"${repo_root}/config/samples/v1alpha2_kubernaut.yaml"
)

for file in "${active_files[@]}"; do
	[[ -f "${file}" ]] || fail "missing active contract file ${file}"
done

for pattern in \
	'mcpGatewayEndpoint' \
	'mcpGatewayType' \
	'mcpGatewayNamespace' \
	'fleet\.backend' \
	'fleet\.endpoint' \
	'fleet\.caSecretName' \
	'fleet\.tokenSecretName' \
	'fleet\.resilience' \
	'scopeCheck\.endpoint' \
	'scopeCheck\.tls' \
	'scopeCheck\.tokenSecretRef' \
	'fleet\.oauth2\.enabled'; do
	if grep -EnH "${pattern}" "${active_files[@]}" >/dev/null; then
		fail "removed or relocated Fleet field ${pattern} appears in active documentation"
	fi
done

unreleased=$(awk '/^## \[Unreleased\]/{active=1; next} /^## \[/{active=0} active' "${repo_root}/CHANGELOG.md")
for pattern in \
	'mcpGatewayEndpoint' \
	'mcpGatewayType' \
	'mcpGatewayNamespace' \
	'fleet\.backend' \
	'fleet\.endpoint' \
	'fleet\.caSecretName' \
	'fleet\.tokenSecretName' \
	'fleet\.resilience' \
	'scopeCheck\.endpoint' \
	'scopeCheck\.tls' \
	'scopeCheck\.tokenSecretRef' \
	'fleet\.oauth2\.enabled'; do
	if grep -E "${pattern}" <<<"${unreleased}" >/dev/null; then
		fail "removed or relocated Fleet field ${pattern} appears in CHANGELOG Unreleased"
	fi
done

echo "Fleet documentation contract validation passed"

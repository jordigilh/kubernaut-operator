#!/usr/bin/env bash

set -euo pipefail

# CI-COVERAGE-GAP-001

profile="${1:-cover-unit-internal-final.out}"

fail() {
	echo "business unit coverage validation failed: $*" >&2
	exit 1
}

[[ -f "${profile}" ]] || fail "coverage profile is missing: ${profile}"

# These are the business-logic entry points changed by Issue #513, its
# development-TLS rotation follow-up, and Issue #514 ownership authorization.
# The repository-wide unit floor remains
# intentionally separate because unrelated policy and resource packages have
# existing, independently tracked coverage debt.
required_functions=(
	"internal/resources/monitoring.go:componentServiceMonitor"
	"internal/resources/services.go:Services"
	"internal/resources/tls.go:existingDevelopmentCA"
	"internal/resources/tls.go:reusableDevelopmentLeaf"
	"internal/resources/tls.go:reusableDevelopmentSigningCertificate"
	"internal/resources/ownership.go:ResourceOwnershipError"
	"internal/resources/ownership.go:resourceOwnerMatches"
	"internal/resources/ownership.go:ownershipMarkerMatches"
	"internal/resources/ownership.go:StampOwnership"
	"internal/resources/crds.go:ensureSharedCRD"
	"internal/policy/ownership.go:OwnershipError"
)

coverage_output=$(go tool cover -func="${profile}")
for requirement in "${required_functions[@]}"; do
	path=${requirement%%:*}
	fn=${requirement#*:}
	coverage=$(
		printf '%s\n' "${coverage_output}" |
			awk -v path="/${path}" -v fn="${fn}" 'index($1, path ":") > 0 && $2 == fn { print $3; exit }'
	)
	[[ "${coverage}" == "100.0%" ]] || fail "${path}:${fn} is ${coverage:-unreported}; expected 100.0%"
	done

echo "business unit coverage validation passed: ${#required_functions[@]} changed entry points at 100.0%"

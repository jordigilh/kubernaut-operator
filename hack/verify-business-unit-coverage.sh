#!/usr/bin/env bash

set -euo pipefail

# CI-COVERAGE-GAP-001

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
profile="${1:-cover-unit.out}"

fail() {
	echo "business unit coverage validation failed: $*" >&2
	exit 1
}

[[ -f "${profile}" ]] || fail "coverage profile is missing: ${profile}"

# No rounded percentages, changed-function allowlists or integration merge.
# Inventory every handwritten API/internal/bootstrap function; reject both raw
# zero-execution statements and absent function instrumentation.
go run "${repo_root}/hack/businesscoverage" -root "${repo_root}" -baseline "${BUSINESS_COVERAGE_BASELINE:-cover-unit-baseline.out}" \
	-report "${BUSINESS_COVERAGE_REPORT:-cover-business-unit.json}" "${profile}"

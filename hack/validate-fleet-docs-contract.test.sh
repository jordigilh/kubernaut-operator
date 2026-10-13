#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
validator="${script_dir}/validate-fleet-docs-contract.sh"

output=$("${validator}")
[[ "${output}" == *"Fleet documentation contract validation passed"* ]] || {
	echo "unexpected validator output: ${output}" >&2
	exit 1
}

echo "Fleet documentation contract validator test passed"

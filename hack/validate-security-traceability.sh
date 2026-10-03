#!/usr/bin/env bash

set -euo pipefail

# CI-CONTROLS-GAP-001

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
matrix="${repo_root}/docs/security/ISSUE-488-CONTROL-TRACEABILITY.md"

fail() {
	echo "security traceability validation failed: $*" >&2
	exit 1
}

[[ -f "${matrix}" ]] || fail "missing ${matrix}"

grep -Fq 'NIST SP 800-53 Rev. 5' "${matrix}" || fail "missing NIST SP 800-53 Rev. 5 declaration"
grep -Fq 'OWASP ASVS 5.0.0' "${matrix}" || fail "missing OWASP ASVS 5.0.0 declaration"
grep -Eq 'v5\.0\.0-V[0-9]+\.[0-9]+\.[0-9]+' "${matrix}" || fail "missing versioned ASVS requirement IDs"

for control in AC-3 AC-6 SC-7 SC-8 SC-12 SC-13 SC-17 IA-2 IA-5 SI-4 AU-2 AU-3 AU-12 SI-10 CM-2 CM-3 CM-6 CM-8; do
	grep -Eq "(^|[^A-Za-z0-9-])${control}([^A-Za-z0-9-]|$)" "${matrix}" || fail "missing NIST/FedRAMP control ${control}"
done

for status in verified 'partially verified' 'not verified' 'not applicable'; do
	grep -Fq "${status}" "${matrix}" || fail "missing status ${status}"
done

for artifact in \
	'internal/resources/tls.go' \
	'internal/controller/kubernaut_controller.go' \
	'internal/controller/tls_source_integration_test.go' \
	'internal/resources/tls_test.go' \
	'test/e2e/kind/' \
	'.github/workflows/test.yml' \
	'Makefile'; do
	grep -Fq "${artifact}" "${matrix}" || fail "missing evidence artifact ${artifact}"
done

grep -Eiq 'does not claim (formal )?(FedRAMP|ASVS) (authorization|compliance|conformance)' "${matrix}" \
	|| fail "matrix must state that formal FedRAMP/ASVS claims are not made"
grep -Fq 'OVN/OpenShift' "${matrix}" || fail "missing deferred OVN/OpenShift residual risk"
grep -Fqi 'dedicated operator Helm chart' "${matrix}" || fail "missing deferred Helm residual risk"

echo "security traceability validation passed"

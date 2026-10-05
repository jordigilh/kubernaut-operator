#!/usr/bin/env bash

set -euo pipefail

# CI-CONTROLS-GAP-001

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
matrix="${repo_root}/docs/security/ISSUE-488-CONTROL-TRACEABILITY.md"
tls_matrix="${repo_root}/docs/security/ISSUE-491-TLS-CONTROL-ATTESTATION.md"
ci_matrix="${repo_root}/docs/design/ISSUE-501-OPERATOR-UPSTREAM-CI-BOUNDARY.md"

fail() {
	echo "security traceability validation failed: $*" >&2
	exit 1
}

[[ -f "${matrix}" ]] || fail "missing ${matrix}"
[[ -f "${tls_matrix}" ]] || fail "missing ${tls_matrix}"
[[ -f "${ci_matrix}" ]] || fail "missing ${ci_matrix}"

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

# Issue #491 has a dedicated matrix because its Helm TLS parity assertions add
# ownership, crypto, cainjector, path, and rotation evidence to the #488 scope.
grep -Fq 'engineering attestation' "${tls_matrix}" || fail "TLS matrix is missing its engineering-attestation boundary"
grep -Fq 'does not establish' "${tls_matrix}" || fail "TLS matrix must reject formal compliance claims"
grep -Fq 'FedRAMP' "${tls_matrix}" || fail "TLS matrix is missing FedRAMP scope"
grep -Fq 'SOC 2' "${tls_matrix}" || fail "TLS matrix is missing SOC 2 scope"
grep -Fq 'OWASP ASVS' "${tls_matrix}" || fail "TLS matrix is missing OWASP ASVS scope"

grep -Fq 'does not claim formal' "${ci_matrix}" || fail "CI boundary document must reject formal compliance claims"
for control in SA-12 CM-3 CM-8 AU-3 AU-12 RA-5 CA-7; do
	grep -Eq "(^|[^A-Za-z0-9-])${control}([^A-Za-z0-9-]|$)" "${ci_matrix}" \
		|| fail "CI boundary document is missing NIST/FedRAMP control ${control}"
done
for requirement in v5.0.0-V15.1.2 v5.0.0-V15.2.4 v5.0.0-V16.2.1 v5.0.0-V16.5.3; do
	grep -Fq "${requirement}" "${ci_matrix}" \
		|| fail "CI boundary document is missing ASVS requirement ${requirement}"
done
for evidence_id in CI-501-BOUNDARY-001 CI-501-PROVENANCE-001 CI-501-ARTIFACT-001 CI-501-GATE-001; do
	grep -R -n --include='*.sh' --include='*.md' -F "${evidence_id}" "${repo_root}/hack" "${repo_root}/docs" >/dev/null \
		|| fail "CI boundary evidence ${evidence_id} has no executable/documented artifact"
done

for control in AC-6 CM-6 IA-5 SC-8 SC-12 SC-13 SC-17 SI-4 SI-10; do
	grep -Eq "(^|[^A-Za-z0-9-])${control}([^A-Za-z0-9-]|$)" "${tls_matrix}" \
		|| fail "TLS matrix is missing NIST/FedRAMP control ${control}"
done

for control in CC6 CC7 CC8 A1; do
	grep -Eq "(^|[^A-Za-z0-9-])${control}([^A-Za-z0-9-]|$)" "${tls_matrix}" \
		|| fail "TLS matrix is missing SOC 2 control ${control}"
done

for requirement in \
	'v5.0.0-V11.1.1' \
	'v5.0.0-V11.1.2' \
	'v5.0.0-V12.1.1' \
	'v5.0.0-V12.1.2' \
	'v5.0.0-V12.1.3' \
	'v5.0.0-V12.2.1' \
	'v5.0.0-V13.2.1' \
	'v5.0.0-V13.3.1' \
	'v5.0.0-V13.3.2' \
	'v5.0.0-V16.5.2'; do
	grep -Fq "${requirement}" "${tls_matrix}" || fail "TLS matrix is missing ASVS requirement ${requirement}"
done

for artifact in \
	'internal/resources/tls.go' \
	'internal/resources/tls_certmanager.go' \
	'internal/resources/tls_certmanager_test.go' \
	'internal/controller/tls_source_integration_test.go' \
	'internal/controller/kubernaut_lifecycle_test.go' \
	'internal/resources/webhooks_test.go' \
	'test/e2e/kind/' \
	'test/e2e/kind/tls_fixtures.go' \
	'test/e2e/kind/contract/selector_test.go' \
	'.github/workflows/test.yml' \
	'hack/verify-test-pyramid.sh' \
	'docs/tests/498/TEST_PLAN.md'; do
	grep -Fq "${artifact}" "${tls_matrix}" || fail "TLS matrix is missing evidence artifact ${artifact}"
done

# Every parity evidence ID must exist in an executable test or CI harness. This
# prevents a control row from becoming a documentation-only assertion.
for test_id in \
	'UT-TLS-491-001' \
	'UT-TLS-491-002' \
	'UT-TLS-491-003' \
	'UT-TLS-491-004' \
	'UT-TLS-491-005' \
	'UT-TLS-491-006' \
	'UT-TLS-491-007' \
	'UT-TLS-491-008' \
	'UT-TLS-491-009' \
	'UT-TLS-491-010' \
	'UT-TLS-491-011' \
	'IT-TLS-MANUAL-001' \
	'IT-TLS-MANUAL-002' \
	'IT-TLS-WEBHOOK-001' \
	'IT-TLS-PARITY-001' \
	'IT-TLS-CERTMANAGER-READY-001' \
	'IT-TLS-GAP-001' \
	'IT-TLS-GAP-002' \
	'IT-TLS-ROTATION-GAP-001' \
	'IT-TLS-GAP-003' \
	'E2E-TLS-CERTMANAGER-001' \
	'E2E-TLS-CERTMANAGER-002' \
	'UT-TLS-498-001' \
	'UT-TLS-498-002' \
	'UT-TLS-498-003' \
	'UT-TLS-498-004' \
	'E2E-TLS-HOOK-001' \
	'E2E-TLS-HOOK-002' \
	'E2E-TLS-MANUAL-001' \
	'E2E-TLS-ADMIN-001' \
	'E2E-TLS-CLEANUP-001' \
	'E2E-TLS-FAIL-CLOSED-001'; do
	grep -R -n --include='*_test.go' -F "${test_id}" "${repo_root}/internal" "${repo_root}/test" >/dev/null \
		|| fail "TLS evidence ID ${test_id} has no executable test"
	grep -Fq "${test_id}" "${tls_matrix}" || fail "TLS matrix omits evidence ID ${test_id}"
done

grep -Eiq 'does not claim (a )?formal (FedRAMP|SOC 2|OWASP ASVS)' "${tls_matrix}" \
	|| fail "TLS matrix must state that formal control claims are not made"

echo "security traceability validation passed"

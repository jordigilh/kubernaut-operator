#!/usr/bin/env bash

set -euo pipefail

# CI-CONTROLS-GAP-001

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
matrix="${repo_root}/docs/security/ISSUE-488-CONTROL-TRACEABILITY.md"
tls_matrix="${repo_root}/docs/security/ISSUE-491-TLS-CONTROL-ATTESTATION.md"
ci_matrix="${repo_root}/docs/design/ISSUE-501-OPERATOR-UPSTREAM-CI-BOUNDARY.md"
otlp_matrix="${repo_root}/docs/security/ISSUE-478-CONTROL-TRACEABILITY.md"
monitoring_matrix="${repo_root}/docs/security/ISSUE-513-CONTROL-TRACEABILITY.md"
gateway_auth_matrix="${repo_root}/docs/security/ISSUE-539-CONTROL-TRACEABILITY.md"
ownership_matrix="${repo_root}/docs/security/ISSUE-514-CONTROL-TRACEABILITY.md"
fleet_matrix="${repo_root}/docs/security/ISSUE-431-CONTROL-TRACEABILITY.md"
audit_hmac_matrix="${repo_root}/docs/security/ISSUE-209-CONTROL-TRACEABILITY.md"
handoff_matrix="${repo_root}/docs/security/ISSUE-527-CONTROL-TRACEABILITY.md"
workflowexecution_retention_matrix="${repo_root}/docs/security/ISSUE-480-CONTROL-TRACEABILITY.md"
console_matrix="${repo_root}/docs/security/ISSUE-476-CONTROL-TRACEABILITY.md"
notification_matrix="${repo_root}/docs/security/ISSUE-549-CONTROL-TRACEABILITY.md"

fail() {
	echo "security traceability validation failed: $*" >&2
	exit 1
}

[[ -f "${matrix}" ]] || fail "missing ${matrix}"
[[ -f "${tls_matrix}" ]] || fail "missing ${tls_matrix}"
[[ -f "${ci_matrix}" ]] || fail "missing ${ci_matrix}"
[[ -f "${otlp_matrix}" ]] || fail "missing ${otlp_matrix}"
[[ -f "${monitoring_matrix}" ]] || fail "missing ${monitoring_matrix}"
[[ -f "${gateway_auth_matrix}" ]] || fail "missing ${gateway_auth_matrix}"
[[ -f "${audit_hmac_matrix}" ]] || fail "missing ${audit_hmac_matrix}"
[[ -f "${handoff_matrix}" ]] || fail "missing ${handoff_matrix}"
[[ -f "${workflowexecution_retention_matrix}" ]] || fail "missing ${workflowexecution_retention_matrix}"
[[ -f "${console_matrix}" ]] || fail "missing ${console_matrix}"
[[ -f "${notification_matrix}" ]] || fail "missing ${notification_matrix}"

grep -Fq 'NIST SP 800-53' "${notification_matrix}" || fail "Issue #549 matrix is missing NIST scope"
grep -Fq 'SOC 2' "${notification_matrix}" || fail "Issue #549 matrix is missing SOC 2 scope"
grep -Fq 'OWASP ASVS 5.0.0' "${notification_matrix}" || fail "Issue #549 matrix is missing ASVS scope"
if ! grep -Fq 'formal FedRAMP' "${notification_matrix}" \
	|| ! grep -Fq 'formal SOC 2' "${notification_matrix}" \
	|| ! grep -Fq 'formal OWASP' "${notification_matrix}" \
	|| ! grep -Fq 'ASVS conformance' "${notification_matrix}"; then
	fail "Issue #549 matrix must reject formal compliance claims"
fi
for control in AC-6 IA-5 CM-3 CM-6 SC-28 SI-10; do
	grep -Eq "(^|[^A-Za-z0-9-])${control}([^A-Za-z0-9-]|$)" "${notification_matrix}" \
		|| fail "Issue #549 matrix is missing control ${control}"
done
for requirement in v5.0.0-V2.1.1 v5.0.0-V2.2.1 v5.0.0-V2.2.2 v5.0.0-V2.2.3 v5.0.0-V8.2.1 v5.0.0-V8.3.1 v5.0.0-V16.5.2; do
	grep -Fq "${requirement}" "${notification_matrix}" \
		|| fail "Issue #549 matrix is missing ASVS requirement ${requirement}"
done
for evidence_id in UT-NOT-API-001 UT-NOT-DEP-002 UT-NOT-ROUTE-004 UT-NOT-SECRET-005 IT-NOT-CRD-RECON-001 E2E-NOT-SECRET-001; do
	grep -R -n --include='*.go' -F "${evidence_id}" "${repo_root}/internal" "${repo_root}/test" >/dev/null \
		|| fail "Issue #549 evidence ${evidence_id} has no executable artifact"
	grep -Fq "${evidence_id}" "${notification_matrix}" \
		|| fail "Issue #549 matrix omits evidence ID ${evidence_id}"
done

grep -Fq 'NIST SP 800-53 Rev. 5' "${matrix}" || fail "missing NIST SP 800-53 Rev. 5 declaration"
grep -Fq 'OWASP ASVS 5.0.0' "${matrix}" || fail "missing OWASP ASVS 5.0.0 declaration"
grep -Eq 'v5\.0\.0-V[0-9]+\.[0-9]+\.[0-9]+' "${matrix}" || fail "missing versioned ASVS requirement IDs"

grep -Fq 'NIST SP 800-53' "${audit_hmac_matrix}" || fail "audit HMAC matrix is missing NIST scope"
grep -Fq 'SOC 2' "${audit_hmac_matrix}" || fail "audit HMAC matrix is missing SOC 2 scope"
grep -Fq 'OWASP ASVS v5.0.0' "${audit_hmac_matrix}" || fail "audit HMAC matrix is missing ASVS scope"
for requirement in v5.0.0-V11.1.1 v5.0.0-V13.3.1 v5.0.0-V13.3.2 v5.0.0-V16.1.1 v5.0.0-V16.2.1 v5.0.0-V16.5.2; do
	grep -Fq "${requirement}" "${audit_hmac_matrix}" || fail "audit HMAC matrix is missing ASVS requirement ${requirement}"
done
for evidence_id in UT-AUDIT-209-CM-001 UT-AUDIT-209-RES-001 UT-AUDIT-209-CTRL-001 IT-AUDIT-209-INT-001 IT-AUDIT-209-INT-007 CONTRACT-AUDIT-209-001 E2E-AUDIT-209-001; do
	grep -R -n --include='*.go' -F "${evidence_id}" "${repo_root}/internal" "${repo_root}/test" >/dev/null \
		|| fail "audit HMAC evidence ${evidence_id} has no executable artifact"
done

grep -Fq 'NIST SP 800-53 Rev. 5' "${handoff_matrix}" || fail "Issue #527 matrix is missing NIST SP 800-53 Rev. 5 scope"
grep -Fq 'SOC 2' "${handoff_matrix}" || fail "Issue #527 matrix is missing SOC 2 scope"
grep -Fq 'OWASP ASVS 5.0.0' "${handoff_matrix}" || fail "Issue #527 matrix is missing OWASP ASVS 5.0.0 scope"
for requirement in v5.0.0-V2.2.1 v5.0.0-V2.2.2 v5.0.0-V13.3.1 v5.0.0-V14.2.4 v5.0.0-V16.5.2; do
	grep -Fq "${requirement}" "${handoff_matrix}" || fail "Issue #527 matrix is missing ASVS requirement ${requirement}"
done
for evidence_id in UT-KA-527-API-001 UT-KA-527-VAL-001 UT-KA-527-VAL-002 UT-KA-527-CM-001 UT-KA-527-CM-002 UT-KA-527-CTRL-001 IT-KA-527-RECONCILE-001 E2E-KA-527-001; do
	grep -R -n --include='*.go' -F "${evidence_id}" "${repo_root}/internal" "${repo_root}/api" "${repo_root}/test" >/dev/null \
		|| fail "Issue #527 evidence ${evidence_id} has no executable artifact"
done

grep -Fq 'NIST SP 800-53 Rev. 5' "${workflowexecution_retention_matrix}" \
	|| fail "Issue #480 matrix is missing NIST SP 800-53 Rev. 5 scope"
grep -Fq 'SOC 2' "${workflowexecution_retention_matrix}" \
	|| fail "Issue #480 matrix is missing SOC 2 scope"
grep -Fq 'OWASP ASVS 5.0.0' "${workflowexecution_retention_matrix}" \
	|| fail "Issue #480 matrix is missing OWASP ASVS 5.0.0 scope"
grep -Fq 'AU-11' "${workflowexecution_retention_matrix}" \
	|| fail "Issue #480 matrix is missing the upstream AU-11 boundary"
grep -Eiq 'does not claim (formal )?(FedRAMP|SOC 2|OWASP ASVS)' "${workflowexecution_retention_matrix}" \
	|| fail "Issue #480 matrix must reject formal control claims"
for requirement in v5.0.0-V2.1.1 v5.0.0-V2.1.3 v5.0.0-V2.2.1 v5.0.0-V2.2.2 v5.0.0-V2.2.3; do
	grep -Fq "${requirement}" "${workflowexecution_retention_matrix}" \
		|| fail "Issue #480 matrix is missing ASVS requirement ${requirement}"
done
for evidence_id in UT-WE-480-API-001 UT-WE-480-CM-001 UT-WE-480-CM-002 IT-WE-480-001 IT-WE-480-ADMISSION-001 IT-WE-480-ADMISSION-002 IT-WE-480-ADMISSION-003 IT-WE-480-ADMISSION-004; do
	grep -R -n --include='*_test.go' -F "${evidence_id}" "${repo_root}/internal" "${repo_root}/api" >/dev/null \
		|| fail "Issue #480 evidence ${evidence_id} has no executable test"
	grep -Fq "${evidence_id}" "${workflowexecution_retention_matrix}" \
		|| fail "Issue #480 matrix omits evidence ID ${evidence_id}"
done

grep -Fq 'FedRAMP/NIST' "${console_matrix}" || fail "Issue #476 matrix is missing FedRAMP/NIST scope"
grep -Fq 'SOC 2' "${console_matrix}" || fail "Issue #476 matrix is missing SOC 2 scope"
grep -Fq 'OWASP ASVS 5.0.0' "${console_matrix}" || fail "Issue #476 matrix is missing OWASP ASVS 5.0.0 scope"
grep -Eiq 'does not claim formal (FedRAMP|SOC 2|OWASP ASVS)' "${console_matrix}" \
	|| fail "Issue #476 matrix must reject formal compliance claims"
for control in CM-3 CM-6 SI-10; do
	grep -Eq "(^|[^A-Za-z0-9-])${control}([^A-Za-z0-9-]|$)" "${console_matrix}" \
		|| fail "Issue #476 matrix is missing control ${control}"
done
for requirement in v5.0.0-V1.2.3 v5.0.0-V2.1.1 v5.0.0-V2.2.1 v5.0.0-V2.2.2 v5.0.0-V15.2.4; do
	grep -Fq "${requirement}" "${console_matrix}" \
		|| fail "Issue #476 matrix is missing ASVS requirement ${requirement}"
done
for artifact in \
	'api/v1alpha2/kubernaut_types.go' \
	'internal/resources/console.go' \
	'internal/resources/console_test.go' \
	'internal/controller/kubernaut_lifecycle_test.go' \
	'docs/tests/476/TEST_PLAN.md' \
	'hack/verify-console-release.sh'; do
	grep -Fq "${artifact}" "${console_matrix}" || fail "Issue #476 matrix is missing evidence artifact ${artifact}"
done
for test_id in UT-CN-476-001 UT-CN-476-002 UT-CN-476-003 IT-CN-476-001 IT-CN-476-002 CI-CN-476-001; do
	grep -R -n --include='*_test.go' -F "${test_id}" "${repo_root}/internal" >/dev/null 2>&1 \
		|| grep -R -n --include='*.sh' -F "${test_id}" "${repo_root}/hack" >/dev/null 2>&1 \
		|| fail "Issue #476 evidence ID ${test_id} has no executable artifact"
	grep -Fq "${test_id}" "${console_matrix}" || fail "Issue #476 matrix omits evidence ID ${test_id}"
done

for control in AC-3 AC-6 SC-7 SC-8 SC-12 SC-13 SC-17 IA-2 IA-5 SI-4 AU-2 AU-3 AU-12 SI-10 CM-2 CM-3 CM-6 CM-8; do
	grep -Eq "(^|[^A-Za-z0-9-])${control}([^A-Za-z0-9-]|$)" "${matrix}" || fail "missing NIST/FedRAMP control ${control}"
done

grep -Fq 'NIST SP 800-53 Rev. 5' "${otlp_matrix}" || fail "OTLP matrix is missing NIST SP 800-53 Rev. 5 scope"
grep -Fq 'OWASP ASVS 5.0.0' "${otlp_matrix}" || fail "OTLP matrix is missing OWASP ASVS 5.0.0 scope"
for control in AC-6 CM-6 IA-5 SC-7 SC-8 'SC-8(1)' SC-12 SC-13 SI-4 SI-10 SA-11; do
	grep -Fq "${control}" "${otlp_matrix}" || fail "OTLP matrix is missing control ${control}"
done
for requirement in v5.0.0-V12.1.3 v5.0.0-V12.2.1 v5.0.0-V12.3.1 v5.0.0-V12.3.2 v5.0.0-V12.3.3 v5.0.0-V12.3.4 v5.0.0-V12.3.5 v5.0.0-V13.3.1 v5.0.0-V13.3.2 v5.0.0-V14.2.4; do
	grep -Fq "${requirement}" "${otlp_matrix}" || fail "OTLP matrix is missing ASVS requirement ${requirement}"
done
for artifact in internal/resources/validation.go internal/resources/telemetry.go internal/controller/telemetry.go internal/controller/telemetry_integration_test.go internal/resources/telemetry_runtime_test.go; do
	grep -Fq "${artifact}" "${otlp_matrix}" || fail "OTLP matrix is missing evidence artifact ${artifact}"
done
grep -Eiq 'does not claim formal (FedRAMP|NIST|OWASP ASVS)' "${otlp_matrix}" || fail "OTLP matrix must reject formal compliance claims"

for status in verified 'partially verified' 'not verified' 'not applicable'; do
	grep -Fq "${status}" "${matrix}" || fail "missing status ${status}"
done

for artifact in \
	'internal/resources/tls.go' \
	'internal/controller/kubernaut_controller.go' \
	'internal/controller/tls_source_unit_test.go' \
	'internal/controller/runtime_tls_wiring_integration_test.go' \
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
	'internal/controller/tls_source_unit_test.go' \
	'internal/controller/runtime_tls_wiring_integration_test.go' \
	'internal/controller/certmanager_lifecycle_test.go' \
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
	'UT-TLS-MANUAL-001' \
	'UT-TLS-MANUAL-002' \
	'UT-TLS-WEBHOOK-001' \
	'UT-TLS-PARITY-001' \
	'UT-TLS-CERTMANAGER-READY-001' \
	'UT-TLS-GAP-001' \
	'UT-TLS-GAP-002' \
	'UT-TLS-ROTATION-GAP-001' \
	'UT-TLS-ROTATION-GAP-002' \
	'IT-TLS-AUDIT-515-001' \
	'IT-TLS-AUDIT-515-002' \
	'IT-TLS-AUDIT-515-003' \
	'IT-TLS-AUDIT-515-004' \
	'IT-TLS-GAP-003' \
	'E2E-TLS-CERTMANAGER-001' \
	'E2E-TLS-CERTMANAGER-002' \
	'UT-TLS-498-001' \
	'UT-TLS-498-002' \
	'UT-TLS-498-003' \
	'UT-TLS-498-004' \
	'UT-TLS-ROTATION-GAP-003' \
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

# Issue #513 monitoring contract evidence is validated separately from the
# broader #488 matrix so a generated-resource change cannot silently lose its
# Service/ServiceMonitor ownership and port evidence.
grep -Fq 'NIST/FedRAMP' "${monitoring_matrix}" || fail "Issue #513 matrix is missing NIST/FedRAMP scope"
grep -Fq 'SOC 2' "${monitoring_matrix}" || fail "Issue #513 matrix is missing SOC 2 scope"
grep -Fq 'OWASP ASVS 5.0.0' "${monitoring_matrix}" || fail "Issue #513 matrix is missing OWASP ASVS scope"
grep -Eiq 'does not claim formal (FedRAMP|SOC 2|ASVS)' "${monitoring_matrix}" \
	|| fail "Issue #513 matrix must reject formal control claims"
for control in AC-6 CM-3 CM-6 CM-8 SI-4 AU-2 AU-3 AU-12; do
	grep -Eq "(^|[^A-Za-z0-9-])${control}([^A-Za-z0-9-]|$)" "${monitoring_matrix}" \
		|| fail "Issue #513 matrix is missing NIST/FedRAMP control ${control}"
done
for requirement in v5.0.0-V8.2.1 v5.0.0-V8.3.1 v5.0.0-V16.1.1 v5.0.0-V16.2.1 v5.0.0-V16.5.2; do
	grep -Fq "${requirement}" "${monitoring_matrix}" \
		|| fail "Issue #513 matrix is missing ASVS requirement ${requirement}"
done
for artifact in \
	'internal/resources/services.go' \
	'internal/resources/monitoring.go' \
	'internal/controller/kubernaut_controller.go' \
	'docs/tests/513/TEST_PLAN.md' \
	'hack/verify-monitoring-contract.sh' \
	'hack/verify-business-unit-coverage.sh' \
	'hack/verify-test-pyramid.sh'; do
	grep -Fq "${artifact}" "${monitoring_matrix}" || fail "Issue #513 matrix is missing evidence artifact ${artifact}"
done
for test_id in \
	'UT-MON-513-001' \
	'UT-MON-513-002' \
	'UT-MON-513-003' \
	'UT-MON-513-004' \
	'UT-MON-513-CTRL-001' \
	'UT-MON-513-CTRL-002' \
	'UT-MON-513-CTRL-003' \
	'UT-MON-513-CTRL-004' \
	'UT-MON-513-CTRL-005' \
	'UT-MON-513-CTRL-006' \
	'UT-MON-513-CTRL-007' \
	'IT-MON-513-008' \
	'IT-MON-513-009' \
	'IT-MON-513-010'; do
	grep -R -n --include='*_test.go' -F "${test_id}" "${repo_root}/internal" >/dev/null \
		|| fail "Issue #513 evidence ID ${test_id} has no executable test"
	grep -Fq "${test_id}" "${monitoring_matrix}" || fail "Issue #513 matrix omits evidence ID ${test_id}"
done

# Issue #539 Gateway authentication contract evidence is independently checked
# because its explicit opt-in and administrator-owned Secret boundary are
# security-sensitive even though they reuse existing reconciliation paths.
for scope in 'NIST SP 800-53 Rev. 5' 'FedRAMP' 'SOC 2' 'OWASP ASVS 5.0.0'; do
	grep -Fq "${scope}" "${gateway_auth_matrix}" || fail "Issue #539 matrix is missing ${scope} scope"
done
grep -Fq 'does not claim formal FedRAMP' "${gateway_auth_matrix}" \
	|| fail "Issue #539 matrix must reject formal compliance claims"
for control in AC-3 AC-6 CM-3 CM-6 CM-8 IA-2 IA-5 SI-4 SI-10; do
	grep -Eq "(^|[^A-Za-z0-9-])${control}([^A-Za-z0-9-]|$)" "${gateway_auth_matrix}" \
		|| fail "Issue #539 matrix is missing control ${control}"
done
for requirement in v5.0.0-V2.2.1 v5.0.0-V8.2.1 v5.0.0-V8.3.1 v5.0.0-V13.3.1 v5.0.0-V13.3.2 v5.0.0-V15.2.4 v5.0.0-V16.1.1 v5.0.0-V16.5.2; do
	grep -Fq "${requirement}" "${gateway_auth_matrix}" \
		|| fail "Issue #539 matrix is missing ASVS requirement ${requirement}"
done
for artifact in \
	'api/v1alpha2/kubernaut_types.go' \
	'api/v1alpha2/clean_break_test.go' \
	'internal/resources/configmaps.go' \
	'internal/resources/configmaps_test.go' \
	'internal/resources/ocp.go' \
	'internal/resources/ocp_test.go' \
	'internal/controller/kubernaut_controller.go' \
	'internal/controller/kubernaut_lifecycle_test.go' \
	'internal/controller/monitoring_wiring_integration_test.go' \
	'config/crd/bases/kubernaut.ai_kubernauts.yaml' \
	'bundle/manifests/kubernaut.ai_kubernauts.yaml' \
	'charts/kubernaut-operator/files/kubernaut.ai_kubernauts.yaml' \
	'dist/install.yaml'; do
	grep -Fq "${artifact}" "${gateway_auth_matrix}" || fail "Issue #539 matrix is missing evidence artifact ${artifact}"
done
for test_id in UT-539-GW-AUTH-API-001 UT-539-GW-AUTH-CM-001 UT-539-GW-AUTH-CM-002 \
	UT-539-GW-AUTH-AM-001 UT-539-GW-AUTH-AM-002 UT-539-GW-AUTH-CONDITION-001 \
	IT-539-GW-AUTH-RECONCILE IT-539-GW-AUTH-CONDITION; do
	grep -R -n --include='*_test.go' -F "${test_id}" "${repo_root}/api" "${repo_root}/internal" >/dev/null \
		|| fail "Issue #539 evidence ID ${test_id} has no executable test"
	grep -Fq "${test_id}" "${gateway_auth_matrix}" || fail "Issue #539 matrix omits evidence ID ${test_id}"
done

# Issue #514's deny-by-default ownership boundary must have independent logic,
# real-Reconcile wiring, and installed-operator journey artifacts. This static
# check does not claim that a live lane has executed.
[[ -f "${ownership_matrix}" ]] || fail "missing ${ownership_matrix}"
[[ -f "${fleet_matrix}" ]] || fail "missing ${fleet_matrix}"

grep -Fq 'not a formal FedRAMP, SOC 2, or OWASP ASVS certification claim' "${fleet_matrix}" \
	|| fail "Fleet matrix must preserve the engineering-evidence disclaimer"
for artifact in \
	'internal/resources/configmaps_test.go' \
	'internal/resources/deployments_test.go' \
	'internal/controller/business_api_boundaries_unit_test.go'; do
	grep -Fq "${artifact}" "${fleet_matrix}" \
		|| fail "Fleet evidence is missing artifact ${artifact}"
done
grep -Fq 'docs/tests/431/TEST_PLAN.md' "${fleet_matrix}" \
	|| fail "Fleet matrix is missing its test plan reference"
for test_id in \
	'UT-FLEET-521-ACM-001' \
	'UT-FLEET-521-RENDER-SECRET-001' \
	'UT-FLEET-521-RENDER-SECRET-002' \
	'UT-FLEET-521-PREFLIGHT-SECRET-001' \
	'UT-FLEET-521-MOUNT-SECRET-001' \
	'UT-FLEET-521-MOUNT-SECRET-002'; do
	grep -R -n --include='*_test.go' -F "${test_id}" "${repo_root}/internal" >/dev/null \
		|| fail "Fleet evidence ID ${test_id} has no executable test"
	grep -Fq "${test_id}" "${fleet_matrix}" || fail "Fleet matrix omits evidence ID ${test_id}"
	grep -Fq "${test_id}" "${repo_root}/docs/tests/431/TEST_PLAN.md" || fail "Fleet test plan omits evidence ID ${test_id}"
done
for scope in 'NIST SP 800-53 Rev. 5' 'SOC 2' 'OWASP ASVS 5.0.0'; do
	grep -Fq "${scope}" "${ownership_matrix}" || fail "ownership matrix is missing ${scope}"
done
grep -Fq 'does not claim formal' "${ownership_matrix}" || fail 'ownership matrix must reject formal compliance claims'
for control in AC-3 AC-6 CM-3 CM-6 CM-8 SI-4 SI-10 AU-2 AU-3 AU-12; do
	grep -Fq "${control}" "${ownership_matrix}" || fail "ownership matrix is missing ${control}"
done
for requirement in v5.0.0-V8.2.1 v5.0.0-V8.3.1 v5.0.0-V13.3.1 v5.0.0-V13.3.2 v5.0.0-V16.1.1 v5.0.0-V16.2.1 v5.0.0-V16.5.2; do
	grep -Fq "${requirement}" "${ownership_matrix}" || fail "ownership matrix is missing ${requirement}"
done
for test_id in UT-OWN-514-001 UT-OWN-514-002 UT-OWN-514-003 UT-OWN-514-004 UT-OWN-514-005 \
	UT-OWN-514-006 UT-OWN-514-007 UT-OWN-514-008 UT-OWN-514-009 \
	IT-OWN-514-001 IT-OWN-514-002 IT-OWN-514-003 IT-OWN-514-004 IT-OWN-514-005 \
	IT-OWN-514-006 IT-OWN-514-007 IT-OWN-514-008 IT-OWN-514-009 IT-OWN-514-010 \
	IT-OWN-514-011 IT-OWN-514-012 IT-OWN-514-013 IT-OWN-514-014 IT-OWN-514-015 \
	IT-OWN-514-016 IT-OWN-514-017 IT-OWN-514-018 IT-OWN-514-019 IT-OWN-514-020 \
	IT-OWN-514-021 IT-OWN-514-022 UT-OWN-514-023 E2E-OWN-514-001; do
	grep -R -n --include='*_test.go' -F "${test_id}" "${repo_root}/internal" "${repo_root}/test" >/dev/null \
		|| fail "ownership evidence ID ${test_id} has no executable test"
	grep -Fq "${test_id}" "${ownership_matrix}" || fail "ownership matrix omits ${test_id}"
done

# Every executable test identifier is a unique traceability key. Duplicate IDs
# make a passing result ambiguous and can silently satisfy the wrong matrix row.
duplicate_ids=$(
	{
		grep -RhoE --include='*_test.go' '\b(UT|IT|E2E)-[A-Z0-9]+-[0-9]+-[0-9]+\b' \
			"${repo_root}/internal" "${repo_root}/test" || true
	} | sort | uniq -c | awk '$1 > 1 {print $2}'
)
[[ -z "${duplicate_ids}" ]] || fail "duplicate executable test IDs: ${duplicate_ids//$'\n'/, }"

echo "security traceability validation passed"

#!/usr/bin/env bash

set -euo pipefail

# CI-PYRAMID-GAP-001

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${repo_root}"

fail() {
	echo "test pyramid validation failed: $*" >&2
	exit 1
}

require_text() {
	local file=$1
	local pattern=$2
	local description=$3
	grep -Eq "${pattern}" "${file}" || fail "${description} (${file})"
}

# The unit and integration targets must remain independent. A merged `go test
# ./...` invocation is not evidence that both tiers ran with their own floors.
require_text Makefile '^UT_PKGS := .*internal/resources' 'unit package set is missing resource tests'
require_text Makefile '^IT_PKGS := \.\/internal\/controller' 'integration package set is missing controller tests'
require_text Makefile '^test-unit: .*fmt vet' 'unit target is missing formatting/vet prerequisites'
require_text Makefile 'go test \$\(UT_PKGS\) -coverprofile cover-unit\.out' 'unit target does not write independent coverage'
require_text Makefile 'UNIT_COVERAGE_THRESHOLD' 'unit target does not enforce its coverage floor'
require_text Makefile '^test-integration: .*setup-envtest' 'integration target is missing envtest setup'
require_text Makefile 'go test \$\(IT_PKGS\) -coverprofile cover-integration\.out' 'integration target does not write independent coverage'
require_text Makefile 'INTEGRATION_COVERAGE_THRESHOLD' 'integration target does not enforce its coverage floor'

# The live harness must exercise production installation and the real CR
# journey. These checks intentionally inspect the call graph rather than only
# looking for a test name.
require_text test/e2e/kind/suite_test.go 'loadOperatorImage\(ctx\)' 'Kind suite does not load the operator image'
require_text test/e2e/kind/suite_test.go 'installOperator\(ctx\)' 'Kind suite does not install production manifests'
require_text test/e2e/kind/scenarios_test.go 'applyKubernautCR\(ctx\)' 'Kind suite does not create a real Kubernaut CR'
require_text test/e2e/kind/scenarios_test.go 'ProviderPolicyReady' 'Kind suite does not assert provider policy status'
require_text test/e2e/kind/scenarios_test.go 'TLSReady' 'Kind suite does not assert runtime TLS readiness'
require_text test/e2e/kind/scenarios_test.go 'E2E-TLS-CERTMANAGER-002' 'Kind suite does not assert cert-manager leaf rotation'
require_text .github/workflows/test.yml 'KUBERNAUT_E2E_TLS_SOURCE: certmanager' 'CI does not run the pinned cert-manager lane'
require_text test/e2e/kind/cluster.go 'certManagerVersion[[:space:]]*=[[:space:]]*"v1\.20\.2"' 'Kind suite does not pin cert-manager v1.20.2'
require_text .github/workflows/test.yml 'KIND_VERSION=v0\.31\.0' 'cert-manager lane does not pin Kind v0.31.0'
require_text .github/workflows/test.yml 'KUBECTL_VERSION=v1\.35\.0' 'cert-manager lane does not pin kubectl v1.35.0'

if grep -R -n --include='*.go' -E 'policy\.Render|liveDetection|applyNativePolicies' test/e2e/kind >/dev/null; then
	fail 'Kind E2E calls policy rendering/detection/application helpers directly'
fi

# Checkpoint W: every approved new adapter/builder has a production caller and
# both focused logic and controller-wiring evidence. Deferred OVN/OpenShift and
# Helm rows are deliberately excluded from this approved workstream.
check_wiring() {
	local symbol=$1
	local production_path=$2
	local unit_path=$3
	local integration_path=$4
	grep -R -n --include='*.go' --exclude='*_test.go' -F "${symbol}" "${production_path}" >/dev/null \
		|| fail "${symbol} has no production caller"
	grep -Fq "${symbol}" "${unit_path}" || fail "${symbol} has no unit/resource evidence"
	grep -Fq 'IT-TLS-' "${integration_path}" || fail "${symbol} has no controller wiring evidence"
}

check_wiring 'ResolveTLSMaterial' internal/controller internal/resources/tls_test.go internal/controller/tls_source_integration_test.go
check_wiring 'DevelopmentSelfSignedTLSSecrets' internal/controller internal/resources/tls_test.go internal/controller/tls_source_integration_test.go
check_wiring 'GenericTLSConfigMaps' internal/controller internal/resources/tlsconfigmaps_test.go internal/controller/tls_source_integration_test.go
check_wiring 'MutatingWebhookConfigurationWithCABundle' internal/controller internal/resources/webhooks_test.go internal/controller/tls_source_integration_test.go

grep -Fq 'OVN/OpenShift' docs/test-plans/issue-488-gap-closure-test-plan.md \
	|| fail 'deferred OVN/OpenShift status is not recorded in the approved test plan'
grep -Fq 'dedicated operator Helm chart' docs/test-plans/issue-488-gap-closure-test-plan.md \
	|| fail 'deferred Helm status is not recorded in the approved test plan'

echo 'test pyramid validation passed'

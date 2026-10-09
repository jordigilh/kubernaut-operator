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
require_text Makefile '^test-ci-boundary:' 'CI boundary target is missing'
require_text Makefile '^test-pyramid: .*test-ci-boundary' 'test pyramid does not enforce the CI boundary'

# A release tag must not publish before both upstream qualification and the
# operator-local gates have completed successfully.
require_text .github/workflows/release.yml '^  qualification:' 'release workflow is missing upstream qualification gate'
require_text .github/workflows/release.yml '^  operator-gates:' 'release workflow is missing operator-local gates'
require_text .github/workflows/release.yml 'Run unit, integration, security, and pyramid gates' 'release workflow does not run local test gates'
require_text .github/workflows/release.yml '^  operator-e2e-image:' 'release workflow does not build an operator image for E2E'
require_text .github/workflows/release.yml '^  operator-e2e:' 'release workflow is missing the contract-only Kind gate'
require_text .github/workflows/release.yml 'run: make test-e2e-kind' 'release workflow does not run the contract-only Kind gate'
require_text .github/workflows/release.yml 'needs: \[prepare, qualification, operator-e2e\]' 'image build is not gated by qualification and the post-test E2E gate'

# The hosted test pipeline must establish the test -> image -> E2E order. The
# image artifact is deliberately shared so every lane validates the same
# operator build instead of rebuilding a different image in each runner.
require_text .github/workflows/test.yml '^  build-image:' 'test workflow is missing the post-test operator image build'
require_text .github/workflows/test.yml '^    needs: test' 'operator image build is not gated by unit and integration tests'
require_text .github/workflows/test.yml '^  kind-e2e:' 'test workflow is missing the Kind E2E matrix'
require_text .github/workflows/test.yml '^    needs: build-image' 'Kind E2E is not gated by the operator image build'
require_text .github/workflows/test.yml 'actions/upload-artifact@' 'test workflow does not publish the built operator image artifact'
require_text .github/workflows/test.yml 'actions/download-artifact@' 'Kind E2E does not download the built operator image artifact'
require_text .github/workflows/test.yml 'docker load' 'Kind E2E does not load the built operator image artifact'
require_text .github/workflows/test.yml 'KUBERNAUT_OPERATOR_IMAGE:' 'Kind E2E does not pass the built operator image to the harness'
require_text Makefile '^KIND_OPERATOR_VERSION \?= ci$' 'E2E harness default is not the tested :ci image contract'
require_text test/e2e/kind/cluster.go 'defaultOperatorImage[[:space:]]*=[[:space:]]*"localhost/kubernaut-operator:ci"' \
	'Kind harness default is not the tested :ci image'
require_text test/e2e/helm/suite_test.go 'return "localhost/kubernaut-operator:ci"' \
	'Helm harness default is not the tested :ci image'
require_text .github/workflows/test.yml '^  helm-bootstrap:' 'test workflow is missing the Helm bootstrap matrix'
require_text .github/workflows/test.yml 'KUBERNAUT_HELM_E2E_TLS_PROFILE:' 'Helm bootstrap matrix does not select TLS profiles'
require_text .github/workflows/test.yml 'KUBERNAUT_HELM_E2E_DISCONNECTED:' 'Helm bootstrap matrix does not exercise disconnected bootstrap'
require_text .github/workflows/test.yml 'run: make test-e2e-kind-helm' 'Helm bootstrap matrix does not run the Helm lifecycle suite'
require_text .github/workflows/test.yml 'HELM_VERSION=v4\.' 'Kind CI is not qualified with Helm 4'
require_text .github/workflows/release.yml 'HELM_VERSION=v4\.' 'release CI is not qualified with Helm 4'

# Helm is the supported generic-Kubernetes/Kind operator installation path.
# Keep the chart render gate and the live harness tied to the same source of
# truth so a passing Kustomize-only path cannot hide a broken chart.
require_text Makefile '^test-helm:' 'Helm chart gate is missing'
require_text Makefile '^test-e2e-kind: fmt vet .*Helm-backed operator contract Kind E2E suite' \
	'Kind E2E target is not Helm-backed'
require_text Makefile 'command -v \$\(HELM_BIN\)' 'Kind E2E target does not require Helm'
require_text test/e2e/kind/cluster.go '"install", "kubernaut-operator", operatorChartPath\(\)' \
	'Kind harness does not install the operator Helm chart'
require_text test/e2e/kind/cluster.go 'webhook\.tls\.mode=development' \
	'Kind harness does not select the chart development TLS profile'
require_text test/e2e/helm/suite_test.go 'operator-only Helm lifecycle' \
	'Helm E2E suite is missing its lifecycle specification'

# The live harness must exercise production installation and the real CR
# journey. These checks intentionally inspect the call graph rather than only
# looking for a test name.
require_text test/e2e/kind/suite_test.go 'loadOperatorImage\(ctx\)' 'Kind suite does not load the operator image'
require_text test/e2e/kind/suite_test.go 'installOperator\(ctx\)' 'Kind suite does not install the operator Helm chart'
require_text test/e2e/kind/scenarios_test.go 'applyKubernautCR\(ctx\)' 'Kind suite does not create a real Kubernaut CR'
require_text test/e2e/kind/scenarios_test.go 'ProviderPolicyReady' 'Kind suite does not assert provider policy status'
require_text test/e2e/kind/scenarios_test.go 'TLSReady' 'Kind suite does not assert runtime TLS readiness'
require_text test/e2e/kind/scenarios_test.go 'E2E-TLS-CERTMANAGER-002' 'Kind suite does not assert cert-manager leaf rotation'
require_text test/e2e/kind/scenarios_test.go 'E2E-TLS-HOOK-001' 'Kind suite does not assert the hook TLS journey'
require_text test/e2e/kind/scenarios_test.go 'E2E-TLS-MANUAL-001' 'Kind suite does not assert the manual TLS journey'
require_text test/e2e/kind/scenarios_test.go 'E2E-TLS-ADMIN-001' 'Kind suite does not assert the AdministratorManaged TLS journey'
require_text test/e2e/kind/scenarios_test.go 'E2E-TLS-FAIL-CLOSED-001' 'Kind suite does not assert invalid TLS fail-closed behavior'
require_text test/e2e/kind/scenarios_test.go 'E2E-TLS-CLEANUP-001' 'Kind suite does not assert source-specific TLS cleanup'
require_text test/e2e/kind/contract/selector_test.go 'UT-TLS-498-001' 'TLS selector contract has no unit evidence'
require_text .github/workflows/test.yml 'tls_source: certmanager' 'CI does not run the pinned cert-manager lane'
require_text .github/workflows/test.yml 'tls_source: development' 'CI does not name the development TLS lane explicitly'
require_text .github/workflows/test.yml 'tls_source: hook' 'CI does not run the hook TLS lane'
require_text .github/workflows/test.yml 'tls_source: manual-admin' 'CI does not run the manual/admin TLS lane'
require_text test/e2e/kind/cluster.go 'certManagerVersion[[:space:]]*=[[:space:]]*"v1\.20\.2"' 'Kind suite does not pin cert-manager v1.20.2'
require_text .github/workflows/test.yml 'KIND_VERSION=v0\.31\.0' 'cert-manager lane does not pin Kind v0.31.0'
require_text .github/workflows/test.yml 'KUBECTL_VERSION=v1\.35\.0' 'cert-manager lane does not pin kubectl v1.35.0'

if grep -R -n --include='*.go' -E 'policy\.Render|liveDetection|applyNativePolicies' test/e2e/kind >/dev/null; then
	fail 'Kind E2E calls policy rendering/detection/application helpers directly'
fi

# Checkpoint W: every approved new adapter/builder has a production caller and
# both focused logic and controller-wiring evidence. Deferred OVN/OpenShift rows
# are deliberately excluded from this approved workstream; Helm remains
# separately owned by Issue #489 and is checked by the chart gate above.
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
grep -Fq 'Owned by Issue #489' docs/test-plans/issue-488-gap-closure-test-plan.md \
	|| fail 'separate Helm ownership is not recorded in the approved test plan'

echo 'test pyramid validation passed'

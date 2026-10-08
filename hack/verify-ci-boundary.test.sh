#!/usr/bin/env bash
#
# Fixture-based tests for hack/verify-ci-boundary.sh.
# Scenario IDs map to docs/design/ISSUE-501-OPERATOR-UPSTREAM-CI-BOUNDARY.md.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly BOUNDARY="$SCRIPT_DIR/verify-ci-boundary.sh"

pass_count=0
fail_count=0
current_test=""
tmp_dir=""

cleanup() {
  if [[ -n "$tmp_dir" ]]; then
    rm -rf "$tmp_dir"
  fi
}
trap cleanup EXIT

fail() {
  fail_count=$((fail_count + 1))
  echo "FAIL: $current_test: $*" >&2
}

assert_exit() {
  local actual="$1" expected="$2"
  [[ "$actual" -eq "$expected" ]] || fail "expected exit $expected, got $actual (output: $LAST_OUTPUT)"
}

assert_contains() {
  local haystack="$1" needle="$2"
  [[ "$haystack" == *"$needle"* ]] || fail "expected output to contain [$needle], got [$haystack]"
}

new_fixture() {
  cleanup
  tmp_dir="$(mktemp -d)"
  mkdir -p "$tmp_dir/.github/workflows" "$tmp_dir/test/e2e/kind/contract" "$tmp_dir/charts/kubernaut-operator"
  cat > "$tmp_dir/Makefile" <<'EOF'
KIND_CONTRACT_IMAGE ?= localhost/kubernaut-operator-e2e-contract:test
test-e2e-kind: fmt vet ## Run the isolated Helm-backed operator contract Kind E2E suite.
	command -v $(HELM_BIN)
	docker build -f test/e2e/kind/contract/Dockerfile -t $(KIND_CONTRACT_IMAGE) test/e2e/kind/contract
	KUBERNAUT_E2E_CONTRACT_IMAGE=$(KIND_CONTRACT_IMAGE) go test ./test/e2e/kind/
EOF
  cat > "$tmp_dir/charts/kubernaut-operator/Chart.yaml" <<'EOF'
apiVersion: v2
name: kubernaut-operator
version: 0.0.0
EOF
  cat > "$tmp_dir/.github/workflows/test.yml" <<'EOF'
name: Tests
on: [push]
jobs:
  test:
    steps:
      - uses: actions/checkout@v7
      - run: make test
EOF
  cat > "$tmp_dir/test/e2e/kind/journey.go" <<'EOF'
func kubernautCR() {
	_ = ContractImageOverrides(contractImage())
}
EOF
  cat > "$tmp_dir/test/e2e/kind/cluster.go" <<'EOF'
func installOperator() {
	_ = helmInCluster("install", "kubernaut-operator", operatorChartPath())
	_ = "webhook.tls.mode=development"
}

func loadInfrastructureImages() {
	_ = contractImage()
}
EOF
  cat > "$tmp_dir/test/e2e/kind/contract/Dockerfile" <<'EOF'
FROM docker.io/library/alpine:3.22.1
EOF
}

run_boundary() {
  local output_file="$tmp_dir/output.txt"
  if "$BOUNDARY" --root "$tmp_dir" >"$output_file" 2>&1; then
    LAST_EXIT=0
  else
    LAST_EXIT=$?
  fi
  LAST_OUTPUT="$(cat "$output_file")"
}

test_CI_501_BOUNDARY_001_accepts_contract_only_repository() {
  new_fixture
  run_boundary
  assert_exit "$LAST_EXIT" 0
  assert_contains "$LAST_OUTPUT" "CI boundary validation passed"
}

test_CI_501_BOUNDARY_002_rejects_upstream_checkout() {
  new_fixture
  cat >> "$tmp_dir/.github/workflows/test.yml" <<'EOF'
      - uses: actions/checkout@v7
        with:
          repository: "jordigilh/kubernaut"
EOF
  run_boundary
  assert_exit "$LAST_EXIT" 1
  assert_contains "$LAST_OUTPUT" "upstream checkout"
}

test_CI_501_BOUNDARY_003_rejects_upstream_application_image() {
  new_fixture
  cat >> "$tmp_dir/.github/workflows/test.yml" <<'EOF'
      - run: docker pull quay.io/kubernaut-ai/gateway:v1.6.0-rc1
EOF
  run_boundary
  assert_exit "$LAST_EXIT" 1
  assert_contains "$LAST_OUTPUT" "upstream application image"
}

test_CI_501_BOUNDARY_004_rejects_missing_contract_wiring() {
  new_fixture
  rm "$tmp_dir/test/e2e/kind/contract/Dockerfile"
  run_boundary
  assert_exit "$LAST_EXIT" 1
  assert_contains "$LAST_OUTPUT" "contract fixture"
}

test_CI_501_BOUNDARY_005_allows_immutable_qualification_evidence_url() {
  new_fixture
  cat >> "$tmp_dir/.github/workflows/release.yml" <<'EOF'
      - run: curl -fsSL https://github.com/jordigilh/kubernaut/releases/download/v1.6.0-rc1/operator-compatibility-qualification.json
EOF
  run_boundary
  assert_exit "$LAST_EXIT" 0
  assert_contains "$LAST_OUTPUT" "CI boundary validation passed"
}

test_CI_501_BOUNDARY_006_rejects_upstream_workflow_dispatch() {
  new_fixture
  cat >> "$tmp_dir/.github/workflows/test.yml" <<'EOF'
      - run: gh workflow run compatibility.yml --repo jordigilh/kubernaut
EOF
  run_boundary
  assert_exit "$LAST_EXIT" 1
  assert_contains "$LAST_OUTPUT" "upstream CI invocation"
}

test_CI_501_BOUNDARY_007_rejects_kind_kustomize_operator_install() {
  new_fixture
  cat >> "$tmp_dir/test/e2e/kind/cluster.go" <<'EOF'
func legacyInstall() {
	_ = "kustomize build config/kind-e2e"
}
EOF
  run_boundary
  assert_exit "$LAST_EXIT" 1
  assert_contains "$LAST_OUTPUT" "Kind operator installation must not use Kustomize"
}

for test_name in $(declare -F | awk '{print $3}' | grep '^test_'); do
  current_test="$test_name"
  before_failures="$fail_count"
  "$test_name"
  if [[ "$fail_count" -eq "$before_failures" ]]; then
    pass_count=$((pass_count + 1))
    echo "PASS: $test_name"
  fi
done

echo "CI boundary tests: ${pass_count} passed, ${fail_count} failed"
[[ "$fail_count" -eq 0 ]]

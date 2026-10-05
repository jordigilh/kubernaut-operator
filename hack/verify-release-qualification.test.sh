#!/usr/bin/env bash
#
# Fixture-based tests for hack/verify-release-qualification.sh.
# Scenario IDs map to docs/design/ISSUE-501-OPERATOR-UPSTREAM-CI-BOUNDARY.md.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly VERIFY="$SCRIPT_DIR/verify-release-qualification.sh"

pass_count=0
fail_count=0
current_test=""
tmp_dir=""
readonly matching_operator_sha="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
operator_sha="$matching_operator_sha"

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

new_manifest() {
  cleanup
  tmp_dir="$(mktemp -d)"
  operator_sha="$matching_operator_sha"
  cat > "$tmp_dir/qualification.json" <<EOF
{
  "schema": "kubernaut-compatibility-qualification/v1",
  "result": "passed",
  "upstream": {
    "repository": "jordigilh/kubernaut",
    "ref": "refs/tags/v1.6.0-rc1",
    "sha": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "images": {
      "gateway": "quay.io/kubernaut-ai/gateway@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
      "datastorage": "quay.io/kubernaut-ai/datastorage@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
    },
    "chart": "oci://quay.io/kubernaut-ai/kubernaut@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
  },
  "operator": {
    "repository": "jordigilh/kubernaut-operator",
    "ref": "refs/heads/release/v1.6",
    "sha": "$operator_sha",
    "image": "quay.io/kubernaut-ai/kubernaut-operator@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
  },
  "qualification": {
    "workflow_run_id": "123456789",
    "workflow_url": "https://github.com/jordigilh/kubernaut/actions/runs/123456789",
    "profile": "release-candidate",
    "completed_at": "2026-10-04T00:00:00Z"
  }
}
EOF
}

run_verify() {
  local output_file="$tmp_dir/output.txt"
  if "$VERIFY" \
    --manifest "$tmp_dir/qualification.json" \
    --operator-sha "$operator_sha" \
    --expected-upstream-ref "refs/tags/v1.6.0-rc1" \
    >"$output_file" 2>&1; then
    LAST_EXIT=0
  else
    LAST_EXIT=$?
  fi
  LAST_OUTPUT="$(cat "$output_file")"
}

test_CI_501_PROVENANCE_001_accepts_matching_qualification() {
  new_manifest
  run_verify
  assert_exit "$LAST_EXIT" 0
  assert_contains "$LAST_OUTPUT" "release qualification validation passed"
}

test_CI_501_PROVENANCE_002_rejects_operator_sha_mismatch() {
  new_manifest
  operator_sha="1111111111111111111111111111111111111111"
  run_verify
  assert_exit "$LAST_EXIT" 1
  assert_contains "$LAST_OUTPUT" "operator SHA"
}

test_CI_501_PROVENANCE_003_rejects_failed_qualification() {
  new_manifest
  sed -i'' -e 's/"result": "passed"/"result": "failed"/' "$tmp_dir/qualification.json"
  run_verify
  assert_exit "$LAST_EXIT" 1
  assert_contains "$LAST_OUTPUT" "result must be passed"
}

test_CI_501_PROVENANCE_004_rejects_mutable_image_reference() {
  new_manifest
  sed -i'' -e 's#quay.io/kubernaut-ai/gateway@sha256:[a-f0-9]*#quay.io/kubernaut-ai/gateway:latest#' "$tmp_dir/qualification.json"
  run_verify
  assert_exit "$LAST_EXIT" 1
  assert_contains "$LAST_OUTPUT" "immutable digest"
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

echo "Release qualification tests: ${pass_count} passed, ${fail_count} failed"
[[ "$fail_count" -eq 0 ]]

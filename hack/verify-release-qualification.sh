#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: verify-release-qualification.sh --manifest PATH --operator-sha SHA \
  [--expected-upstream-ref REF] [--expected-upstream-repository REPOSITORY]

Validate the upstream compatibility qualification record for an operator release.
EOF
}

manifest=""
operator_sha=""
expected_upstream_ref=""
expected_upstream_repository="jordigilh/kubernaut"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --manifest)
      [[ $# -ge 2 ]] || { echo "missing value for --manifest" >&2; exit 2; }
      manifest="$2"
      shift 2
      ;;
    --operator-sha)
      [[ $# -ge 2 ]] || { echo "missing value for --operator-sha" >&2; exit 2; }
      operator_sha="$2"
      shift 2
      ;;
    --expected-upstream-ref)
      [[ $# -ge 2 ]] || { echo "missing value for --expected-upstream-ref" >&2; exit 2; }
      expected_upstream_ref="$2"
      shift 2
      ;;
    --expected-upstream-repository)
      [[ $# -ge 2 ]] || { echo "missing value for --expected-upstream-repository" >&2; exit 2; }
      expected_upstream_repository="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

[[ -n "$manifest" ]] || { echo "--manifest is required" >&2; exit 2; }
[[ -f "$manifest" ]] || { echo "qualification manifest not found: $manifest" >&2; exit 2; }
[[ "$operator_sha" =~ ^[0-9a-f]{40}$ ]] || { echo "--operator-sha must be a 40-character lowercase Git SHA" >&2; exit 2; }

python3 - "$manifest" "$operator_sha" "$expected_upstream_ref" "$expected_upstream_repository" <<'PY'
import json
import re
import sys
from datetime import datetime

manifest_path, expected_operator_sha, expected_upstream_ref, expected_upstream_repository = sys.argv[1:]


def fail(message):
    print(f"release qualification validation failed: {message}", file=sys.stderr)
    raise SystemExit(1)


def require(mapping, key, path):
    if not isinstance(mapping, dict) or key not in mapping:
        fail(f"missing {path}.{key}")
    return mapping[key]


try:
    with open(manifest_path, encoding="utf-8") as manifest_file:
        document = json.load(manifest_file)
except (OSError, json.JSONDecodeError) as error:
    fail(f"cannot read JSON manifest: {error}")

if require(document, "schema", "manifest") != "kubernaut-compatibility-qualification/v1":
    fail("unsupported manifest schema")
if require(document, "result", "manifest") != "passed":
    fail("result must be passed")

upstream = require(document, "upstream", "manifest")
if require(upstream, "repository", "upstream") != expected_upstream_repository:
    fail("unexpected upstream repository")

actual_upstream_ref = require(upstream, "ref", "upstream")
if expected_upstream_ref and actual_upstream_ref != expected_upstream_ref:
    fail(f"upstream ref mismatch: expected {expected_upstream_ref}, got {actual_upstream_ref}")
if not re.fullmatch(r"refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?", actual_upstream_ref):
    fail("upstream ref must be an immutable version tag")

upstream_sha = require(upstream, "sha", "upstream")
if not isinstance(upstream_sha, str) or not re.fullmatch(r"[0-9a-f]{40}", upstream_sha):
    fail("upstream SHA must be a 40-character lowercase Git SHA")

images = require(upstream, "images", "upstream")
if not isinstance(images, dict) or not images:
    fail("upstream images must be a non-empty object")
digest_pattern = re.compile(r"@sha256:[0-9a-f]{64}$")
for name, image in images.items():
    if not isinstance(image, str) or not digest_pattern.search(image):
        fail(f"upstream image {name!r} must use an immutable digest")

chart = require(upstream, "chart", "upstream")
if not isinstance(chart, str) or not digest_pattern.search(chart):
    fail("upstream chart must use an immutable digest")

operator = require(document, "operator", "manifest")
if require(operator, "repository", "operator") != "jordigilh/kubernaut-operator":
    fail("unexpected operator repository")
actual_operator_sha = require(operator, "sha", "operator")
if actual_operator_sha != expected_operator_sha:
    fail(f"operator SHA mismatch: expected {expected_operator_sha}, got {actual_operator_sha}")
operator_ref = require(operator, "ref", "operator")
if not isinstance(operator_ref, str) or not re.fullmatch(r"refs/(heads|tags)/[^\s]+", operator_ref):
    fail("operator ref must identify a branch or tag")
operator_image = require(operator, "image", "operator")
if not isinstance(operator_image, str) or not digest_pattern.search(operator_image):
    fail("operator image must use an immutable digest")

qualification = require(document, "qualification", "manifest")
run_id = require(qualification, "workflow_run_id", "qualification")
if not str(run_id).strip():
    fail("qualification workflow_run_id must not be empty")
workflow_url = require(qualification, "workflow_url", "qualification")
expected_workflow_prefix = f"https://github.com/{expected_upstream_repository}/actions/runs/"
if not isinstance(workflow_url, str) or not workflow_url.startswith(expected_workflow_prefix):
    fail("qualification workflow_url must point to the expected upstream workflow")
if require(qualification, "profile", "qualification") != "release-candidate":
    fail("qualification profile must be release-candidate")
completed_at = require(qualification, "completed_at", "qualification")
if not isinstance(completed_at, str):
    fail("qualification completed_at must be an ISO-8601 timestamp")
try:
    datetime.fromisoformat(completed_at.replace("Z", "+00:00"))
except ValueError:
    fail("qualification completed_at must be an ISO-8601 timestamp")

print(f"release qualification validation passed for operator SHA {expected_operator_sha}")
PY

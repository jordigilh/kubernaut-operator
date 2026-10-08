#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: verify-ci-boundary.sh [--root PATH]

Validate that operator CI and the operator Kind harness do not consume the
full upstream Kubernaut application and that the contract fixture remains wired.
EOF
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --root)
      [[ $# -ge 2 ]] || { echo "missing value for --root" >&2; exit 2; }
      repo_root="$2"
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

repo_root="$(cd "$repo_root" && pwd)"

fail() {
  printf 'CI boundary validation failed: %s\n' "$*" >&2
  exit 1
}

require_file() {
  local relative_path="$1" description="$2"
  [[ -f "$repo_root/$relative_path" ]] || fail "missing ${description}: ${relative_path}"
}

require_text() {
  local relative_path="$1" pattern="$2" description="$3"
  grep -Eq -- "$pattern" "$repo_root/$relative_path" \
    || fail "${description}: ${relative_path}"
}

scan_forbidden() {
  local pattern="$1" description="$2"
  shift 2

  local matches
  matches="$(grep -RInE -- "$pattern" "$@" 2>/dev/null || true)"
  if [[ -n "$matches" ]]; then
    printf 'CI boundary validation failed: %s\n%s\n' "$description" "$matches" >&2
    exit 1
  fi
}

workflow_root="$repo_root/.github/workflows"
require_file ".github/workflows/test.yml" "operator test workflow"
require_file "Makefile" "operator Makefile"
require_file "charts/kubernaut-operator/Chart.yaml" "operator Helm chart"
require_file "test/e2e/kind/contract/Dockerfile" "contract fixture"
require_file "test/e2e/kind/journey.go" "Kind contract journey"
require_file "test/e2e/kind/cluster.go" "Kind image-loading harness"

require_text "Makefile" '^test-e2e-kind: fmt vet .*Helm-backed operator contract Kind E2E suite' \
  "Kind E2E target must use the operator Helm chart"
require_text "Makefile" 'command -v \$\(HELM_BIN\)' \
  "Kind E2E target must require Helm"
require_text "test/e2e/kind/cluster.go" '"install", "kubernaut-operator", operatorChartPath\(\)' \
  "Kind harness must install the operator Helm chart"
require_text "test/e2e/kind/cluster.go" 'webhook\.tls\.mode=development' \
  "Kind harness must select a chart TLS profile"

scan_forbidden \
  '(kustomizeBinary|KUSTOMIZE_BIN|config/kind-e2e|kustomize[[:space:]]+build)' \
  "Kind operator installation must not use Kustomize" \
  "$repo_root/test/e2e/kind"

# These patterns are intentionally scoped to workflow files, the Kind harness,
# and Makefile. Production manager defaults may legitimately contain operand
# image references; they are runtime configuration, not CI dependencies.
scan_forbidden \
  'repository:[[:space:]]*[^#]*/kubernaut([.]git)?([^A-Za-z0-9_-]|$)' \
  "upstream checkout" \
  "$workflow_root" "$repo_root/test/e2e/kind" "$repo_root/config/kind-e2e" "$repo_root/Makefile"

scan_forbidden \
  '(raw[.]githubusercontent[.]com|github[.]com)/[^[:space:]/]+/kubernaut([.]git)?([^A-Za-z0-9_/-]|$)' \
  "upstream repository reference" \
  "$workflow_root" "$repo_root/test/e2e/kind" "$repo_root/config/kind-e2e" "$repo_root/Makefile"

scan_forbidden \
  '(raw[.]githubusercontent[.]com|github[.]com)/[^[:space:]/]+/kubernaut([.]git)?/(actions|archive|tree|contents|blob|pull|commit|issues)([/[:space:]]|$)' \
  "upstream repository operation" \
  "$workflow_root" "$repo_root/test/e2e/kind" "$repo_root/config/kind-e2e" "$repo_root/Makefile"

scan_forbidden \
  '(quay[.]io|ghcr[.]io|docker[.]io)/[^[:space:]/]+/(gateway|datastorage|aianalysis|signalprocessing|remediationorchestrator|workflowexecution|effectivenessmonitor|notification|kubernautagent|authwebhook|apifrontend|console|fleetmetadatacache|db-migrate)([:@])' \
  "upstream application image" \
  "$workflow_root" "$repo_root/test/e2e/kind" "$repo_root/config/kind-e2e" "$repo_root/Makefile"

scan_forbidden \
  '(gh[[:space:]]+(workflow[[:space:]]+run|run)[^#]*/kubernaut([^A-Za-z0-9_-]|$)|/repos/[^[:space:]/]+/kubernaut/(actions|dispatches))' \
  "upstream CI invocation" \
  "$workflow_root" "$repo_root/test/e2e/kind" "$repo_root/config/kind-e2e" "$repo_root/Makefile"

require_text "Makefile" 'KIND_CONTRACT_IMAGE' "Kind contract image variable"
require_text "Makefile" 'KUBERNAUT_E2E_CONTRACT_IMAGE' "Kind contract image handoff"
require_text "Makefile" 'test/e2e/kind/contract/Dockerfile' "Kind contract image build"
require_text "test/e2e/kind/journey.go" 'ContractImageOverrides\(contractImage\(\)\)' "complete contract image override map"
require_text "test/e2e/kind/cluster.go" 'contractImage\(\)' "contract image loading"

echo "CI boundary validation passed"

#!/usr/bin/env bash

set -euo pipefail

# CI-CN-476-001

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
console_digest='quay.io/kubernaut-ai/kubernaut-console@sha256:69fc00fd29d6d08f3375a00615f70f11037200fff58fcb1682dec67fa9906752'
old_console_digest='sha256:98f75c56b8f2a4e2ee0276e31f353175b91dec9e95478775843590c37f876062'

image_files=(
	"${repo_root}/config/manager/manager.yaml"
	"${repo_root}/bundle/manifests/kubernaut-operator.clusterserviceversion.yaml"
	"${repo_root}/dist/install.yaml"
	"${repo_root}/charts/kubernaut-operator/values.yaml"
	"${repo_root}/hack/airgap/imageset-config.yaml"
)

schema_files=(
	"${repo_root}/config/crd/bases/kubernaut.ai_kubernauts.yaml"
	"${repo_root}/bundle/manifests/kubernaut.ai_kubernauts.yaml"
	"${repo_root}/charts/kubernaut-operator/files/kubernaut.ai_kubernauts.yaml"
	"${repo_root}/dist/install.yaml"
)

fail() {
	echo "console release verification failed: $*" >&2
	exit 1
}

for file in "${image_files[@]}"; do
	[[ -f "${file}" ]] || fail "missing image artifact ${file}"
	refs=$(grep -Eo 'quay\.io/kubernaut-ai/kubernaut-console@sha256:[0-9a-f]{64}' "${file}" | sort -u || true)
	[[ "${refs}" == "${console_digest}" ]] || fail "${file} has inconsistent Console image references: ${refs:-<none>}"
	if grep -Fq "${old_console_digest}" "${file}"; then
		fail "${file} still contains the superseded 1.1.0 Console digest"
	fi
done

grep -Fq 'EnableRawThinking *bool `json:"enableRawThinking,omitempty"`' \
	"${repo_root}/api/v1alpha2/kubernaut_types.go" \
	|| fail 'v1alpha2 ConsoleSpec does not expose enableRawThinking'

for file in "${schema_files[@]}"; do
	[[ -f "${file}" ]] || fail "missing generated schema artifact ${file}"
	grep -Fq 'enableRawThinking:' "${file}" || fail "${file} omits enableRawThinking"
	grep -Fq 'default: false' "${file}" || fail "${file} does not default enableRawThinking to false"
done

echo 'console release verification passed'

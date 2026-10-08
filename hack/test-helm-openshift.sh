#!/usr/bin/env bash

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ssh_host=${KUBERNAUT_HELM_OCP_SSH_HOST:-helios08}
remote_kubeconfig=${KUBERNAUT_HELM_OCP_KUBECONFIG:-/root/.kcli/clusters/kubernaut492-sno-a/auth/kubeconfig}
namespace=${KUBERNAUT_HELM_OCP_NAMESPACE:-issue-489-helm-ocp}
image_repository=${KUBERNAUT_HELM_OCP_IMAGE_REPOSITORY:-}
image_tag=${KUBERNAUT_HELM_OCP_IMAGE_TAG:-}
image_digest=${KUBERNAUT_HELM_OCP_IMAGE_DIGEST:-}
image_puller_namespace=${KUBERNAUT_HELM_OCP_IMAGE_PULLER_NAMESPACE:-}

# The caller must build and push a reachable image before this lane starts. The
# lane only transfers the chart and creates a temporary cross-namespace puller
# binding when requested.

fail() {
	echo "OpenShift Helm qualification failed: $*" >&2
	exit 1
}

[[ -n "${ssh_host}" ]] || fail "KUBERNAUT_HELM_OCP_SSH_HOST is required"
[[ -n "${image_repository}" ]] || fail "KUBERNAUT_HELM_OCP_IMAGE_REPOSITORY is required"
[[ -n "${image_tag}" || -n "${image_digest}" ]] || \
	fail "KUBERNAUT_HELM_OCP_IMAGE_TAG or KUBERNAUT_HELM_OCP_IMAGE_DIGEST is required"

command -v ssh >/dev/null 2>&1 || fail "ssh is required"
command -v scp >/dev/null 2>&1 || fail "scp is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

archive=$(mktemp "${TMPDIR:-/tmp}/kubernaut-operator-chart-ocp.XXXXXX.tgz")
remote_archive=/tmp/kubernaut-operator-chart-ocp.tgz
trap 'rm -f "${archive}"' EXIT

# COPYFILE_DISABLE prevents macOS from adding AppleDouble entries that Helm
# interprets as chart templates when the archive is transferred to Linux.
COPYFILE_DISABLE=1 tar --no-xattrs --exclude='._*' --exclude='.DS_Store' -czf "${archive}" \
	-C "${repo_root}/charts" kubernaut-operator
scp -q "${archive}" "${ssh_host}:${remote_archive}"

ssh "${ssh_host}" bash -s -- \
	"${remote_archive}" "${remote_kubeconfig}" "${namespace}" \
	"${image_repository}" "${image_tag:-__EMPTY__}" "${image_digest:-__EMPTY__}" \
	"${image_puller_namespace}" <<'REMOTE'
set -euo pipefail

remote_archive=$1
remote_kubeconfig=$2
namespace=$3
image_repository=$4
image_tag=$5
image_digest=$6
image_puller_namespace=$7
[[ "${image_tag}" == __EMPTY__ ]] && image_tag=
[[ "${image_digest}" == __EMPTY__ ]] && image_digest=
release=kubernaut-operator
secondary_namespace=${namespace}-second
chart_root=/tmp/kubernaut-operator-chart-ocp
crd_name=kubernauts.kubernaut.ai
webhook_name=kubernaut-operator-singleton
manager_name=kubernaut-operator-controller-manager
webhook_secret=kubernaut-operator-webhook-cert

k() {
	kubectl --kubeconfig "${remote_kubeconfig}" "$@"
}

h() {
	helm --kubeconfig "${remote_kubeconfig}" "$@"
}

require_equal() {
	local actual=$1
	local expected=$2
	local description=$3
	[[ "${actual}" == "${expected}" ]] || {
		echo "${description}: expected ${expected@Q}, got ${actual@Q}" >&2
		exit 1
	}
}

require_nonempty() {
	local value=$1
	local description=$2
	[[ -n "${value}" ]] || {
		echo "${description}: expected a non-empty value" >&2
		exit 1
	}
}

cleanup() {
	set +e
	h uninstall "${release}" --namespace "${namespace}" --wait --timeout 5m >/dev/null 2>&1
	for test_namespace in "${namespace}" "${secondary_namespace}"; do
		k -n "${test_namespace}" patch kubernaut kubernaut --type=merge \
			-p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1
		k -n "${test_namespace}" delete kubernaut kubernaut \
			--ignore-not-found=true --wait=true >/dev/null 2>&1
		k delete namespace "${test_namespace}" --ignore-not-found=true \
			--wait=true >/dev/null 2>&1
	done
	k delete crd "${crd_name}" --ignore-not-found=true --wait=true >/dev/null 2>&1
	if [[ -n "${image_puller_namespace}" ]]; then
		k -n "${image_puller_namespace}" delete rolebinding \
			"issue489-image-puller-${namespace}" --ignore-not-found=true >/dev/null 2>&1
	fi
	rm -rf "${chart_root}" "${remote_archive}"
}

command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required" >&2; exit 1; }
command -v helm >/dev/null 2>&1 || { echo "helm is required" >&2; exit 1; }
k get namespace openshift-config >/dev/null
k get namespace openshift-service-ca >/dev/null
if k get crd "${crd_name}" >/dev/null 2>&1; then
	echo "refusing to run over a pre-existing ${crd_name}; use a clean qualification cluster" >&2
	exit 1
fi
if k get validatingwebhookconfiguration "${webhook_name}" >/dev/null 2>&1; then
	echo "refusing to run over a pre-existing ${webhook_name}; use a clean qualification cluster" >&2
	exit 1
fi
if k get namespace "${namespace}" >/dev/null 2>&1 || k get namespace "${secondary_namespace}" >/dev/null 2>&1; then
	echo "test namespace already exists; choose KUBERNAUT_HELM_OCP_NAMESPACE explicitly" >&2
	exit 1
fi
if [[ -n "${image_puller_namespace}" ]] && k -n "${image_puller_namespace}" \
	get rolebinding "issue489-image-puller-${namespace}" >/dev/null 2>&1; then
	echo "image-puller RoleBinding already exists; choose a new test namespace" >&2
	exit 1
fi

trap cleanup EXIT

rm -rf "${chart_root}"
mkdir -p "${chart_root}"
tar -xzf "${remote_archive}" -C "${chart_root}"
chart="${chart_root}/kubernaut-operator"

k create namespace "${namespace}" --dry-run=client -o yaml | k apply -f - >/dev/null
if [[ -n "${image_puller_namespace}" ]]; then
	k -n "${image_puller_namespace}" create rolebinding "issue489-image-puller-${namespace}" \
		--clusterrole=system:image-puller \
		--group="system:serviceaccounts:${namespace}" \
		--dry-run=client -o yaml | k apply -f - >/dev/null
fi

image_args=(--set-string "image.repository=${image_repository}")
if [[ -n "${image_tag}" ]]; then
	image_args+=(--set-string "image.tag=${image_tag}")
else
	image_args+=(--set "image.tag=")
fi
if [[ -n "${image_digest}" ]]; then
	image_args+=(--set-string "image.digest=${image_digest}")
else
	image_args+=(--set "image.digest=")
fi

h install "${release}" "${chart}" --namespace "${namespace}" --create-namespace \
	--set webhook.tls.mode=openshift "${image_args[@]}" --wait --timeout 15m
k -n "${namespace}" rollout status "deployment/${manager_name}" --timeout=5m

pod_fs_group=$(k -n "${namespace}" get deployment "${manager_name}" \
	-o jsonpath='{.spec.template.spec.securityContext.fsGroup}')
pod_host_users=$(k -n "${namespace}" get deployment "${manager_name}" \
	-o jsonpath='{.spec.template.spec.hostUsers}')
require_equal "${pod_fs_group}" "" "the chart pod template must not force fsGroup"
require_equal "${pod_host_users}" "" "the chart must defer hostUsers to OpenShift by default"

secret_type=$(k -n "${namespace}" get secret "${webhook_secret}" -o jsonpath='{.type}')
require_equal "${secret_type}" "kubernetes.io/tls" "OpenShift service-CA Secret type"
serving_cert=$(k -n "${namespace}" get secret "${webhook_secret}" -o jsonpath='{.data.tls\.crt}')
serving_key=$(k -n "${namespace}" get secret "${webhook_secret}" -o jsonpath='{.data.tls\.key}')
require_nonempty "${serving_cert}" "OpenShift service-CA serving certificate"
require_nonempty "${serving_key}" "OpenShift service-CA serving key"

failure_policy=$(k get validatingwebhookconfiguration "${webhook_name}" -o jsonpath='{.webhooks[0].failurePolicy}')
scope=$(k get validatingwebhookconfiguration "${webhook_name}" -o jsonpath='{.webhooks[0].rules[0].scope}')
ca_bundle=$(k get validatingwebhookconfiguration "${webhook_name}" -o jsonpath='{.webhooks[0].clientConfig.caBundle}')
require_equal "${failure_policy}" "Fail" "webhook failure policy"
require_equal "${scope}" "Namespaced" "webhook scope"
require_nonempty "${ca_bundle}" "OpenShift service-CA webhook bundle"

apply_kubernaut() {
	local target_namespace=$1
	cat <<EOF | k apply -f -
apiVersion: kubernaut.ai/v1alpha2
kind: Kubernaut
metadata:
  name: kubernaut
  namespace: ${target_namespace}
spec:
  aiAnalysis:
    policy:
      configMapName: bootstrap-test
  kubernautAgent: {}
  llmProfiles:
    primary:
      credentialsSecretName: llm-credentials
      model: test-model
      provider: openai
  postgresql:
    host: postgresql.example.invalid
    secretName: postgresql-secret
  signalProcessing:
    policy:
      configMapName: bootstrap-test
  valkey:
    host: valkey.example.invalid
    secretName: valkey-secret
EOF
}

k create namespace "${secondary_namespace}" --dry-run=client -o yaml | k apply -f - >/dev/null
apply_kubernaut "${namespace}"
set +e
second_output=$(apply_kubernaut "${secondary_namespace}" 2>&1)
second_status=$?
set -e
[[ ${second_status} -ne 0 ]] || {
	echo "the singleton webhook allowed a second Kubernaut CR: ${second_output}" >&2
	exit 1
}
grep -Eq 'already exists|only one instance' <<<"${second_output}" || {
	echo "the second CR failed for an unexpected reason: ${second_output}" >&2
	exit 1
}

k -n "${namespace}" patch kubernaut kubernaut --type=merge \
	-p '{"metadata":{"finalizers":["issue489.test/retain"]}}' >/dev/null
finalizers=$(k -n "${namespace}" get kubernaut kubernaut -o jsonpath='{.metadata.finalizers}')
grep -q 'issue489.test/retain' <<<"${finalizers}" || {
	echo "test finalizer was not retained: ${finalizers}" >&2
	exit 1
}
cat <<EOF | k apply -f - >/dev/null
apiVersion: v1
kind: ConfigMap
metadata:
  name: operand-sentinel
  namespace: ${namespace}
data:
  lifecycle: retained
EOF

certificate_hash() {
	k -n "${namespace}" get secret "${webhook_secret}" -o jsonpath='{.data.tls\.crt}' \
		| base64 -d | sha256sum | cut -c1-64
}

certificate_before=$(certificate_hash)
h upgrade "${release}" "${chart}" --namespace "${namespace}" --reuse-values \
	--set-string podAnnotations.issue489Upgrade=enabled --wait --timeout 15m
certificate_after=$(certificate_hash)
require_equal "${certificate_after}" "${certificate_before}" \
	"OpenShift service-CA certificate across Helm upgrade"

h uninstall "${release}" --namespace "${namespace}" --wait --timeout 5m
require_equal "$(k get crd "${crd_name}" -o jsonpath='{.metadata.annotations.helm\.sh/resource-policy}')" \
	"keep" "CRD uninstall policy"
k get kubernaut kubernaut -n "${namespace}" >/dev/null
k get configmap operand-sentinel -n "${namespace}" >/dev/null
retained_finalizers=$(k get kubernaut kubernaut -n "${namespace}" -o jsonpath='{.metadata.finalizers}')
grep -q 'issue489.test/retain' <<<"${retained_finalizers}" || {
	echo "CR finalizer was not retained after uninstall: ${retained_finalizers}" >&2
	exit 1
}
if k get deployment "${manager_name}" -n "${namespace}" >/dev/null 2>&1; then
	echo "manager Deployment survived Helm uninstall" >&2
	exit 1
fi
if k get validatingwebhookconfiguration "${webhook_name}" >/dev/null 2>&1; then
	echo "singleton webhook survived Helm uninstall" >&2
	exit 1
fi

h install "${release}" "${chart}" --namespace "${namespace}" --set webhook.tls.mode=openshift \
	"${image_args[@]}" --wait --timeout 15m
k -n "${namespace}" rollout status "deployment/${manager_name}" --timeout=5m
reinstalled_finalizers=$(k get kubernaut kubernaut -n "${namespace}" -o jsonpath='{.metadata.finalizers}')
grep -q 'issue489.test/retain' <<<"${reinstalled_finalizers}" || {
	echo "CR finalizer was not retained after reinstall: ${reinstalled_finalizers}" >&2
	exit 1
}

echo "OpenShift Helm bootstrap qualification passed on ${remote_kubeconfig}"
REMOTE

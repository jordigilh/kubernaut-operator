#!/usr/bin/env bash

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
chart="${repo_root}/charts/kubernaut-dev-bootstrap"
rendered=$(mktemp)
kind_rendered=$(mktemp)
trap 'rm -f "${rendered}" "${kind_rendered}"' EXIT

helm_bin=${HELM_BIN:-helm}
command -v "${helm_bin}" >/dev/null 2>&1 || {
	printf 'helm is required to validate the development bootstrap chart\n' >&2
	exit 1
}

"${helm_bin}" lint "${chart}"
"${helm_bin}" template kubernaut-dev-bootstrap "${chart}" \
	--namespace kubernaut-system >"${rendered}"

for required in \
	'kind: Secret' \
	'name: postgresql-secret' \
	'name: valkey-secret' \
	'name: llm-credentials' \
	'name: aianalysis-policy' \
	'name: signalprocessing-policy' \
	'name: postgresql' \
	'name: valkey' \
	'ssl = on' \
	'hostnossl all all 0.0.0.0/0 reject' \
	'kind: Deployment' \
	'readinessProbe:' \
	'pg_hba: |-' \
	"hba_file = '/tmp/bootstrap-config/pg_hba'" \
	'registry.redhat.io/rhel10/postgresql-16@sha256:877ac0f8207ada1559ef73b70e92616255b95d3b6ef6a1af314c0f67edfde96e' \
	'docker.io/valkey/valkey@sha256:57bcc49c6ade1813ef25206c571b65b66bb0094235ff7fb767941622892297d9'; do
	grep -Fq "${required}" "${rendered}" || {
		printf 'development bootstrap render is missing: %s\n' "${required}" >&2
		exit 1
	}
done

if "${helm_bin}" template kubernaut-dev-bootstrap "${chart}" \
	--namespace kubernaut-system --set postgresql.sslMode=disable >/dev/null 2>&1; then
	printf 'development bootstrap accepted unsupported PostgreSQL sslMode=disable\n' >&2
	exit 1
fi

"${helm_bin}" template kubernaut-dev-bootstrap "${chart}" \
	--namespace kubernaut-system \
	--set postgresql.commandMode=official \
	--set postgresql.securityContext.enabled=false \
	--set-string postgresql.image.repository=docker.io/library/postgres \
	--set-string postgresql.image.tag=16.4 \
	--set-string postgresql.image.digest=sha256:e62fbf9d3e2b49816a32c400ed2dba83e3b361e6833e624024309c35d334b412 \
	--set valkey.securityContext.enabled=false >"${kind_rendered}"

grep -Fq 'docker.io/library/postgres@sha256:e62fbf9d3e2b49816a32c400ed2dba83e3b361e6833e624024309c35d334b412' "${kind_rendered}"
grep -Fq 'command:' "${kind_rendered}"
grep -Fq 'docker-entrypoint.sh postgres' "${kind_rendered}"

printf 'development bootstrap chart validation passed\n'

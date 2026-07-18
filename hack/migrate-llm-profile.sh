#!/usr/bin/env bash
#
# migrate-llm-profile.sh converts a 1.5-shape Kubernaut CR
# (spec.kubernautAgent.llm) to the 1.6-shape spec.llmProfiles +
# llmProfileRef introduced by #187/#216. See docs/upgrade-1.5-to-1.6.md.
#
# This is upgrade-time tooling only: it is never built into the operator's
# container image, and nothing else in this repo depends on it.
#
# Requires: yq (https://github.com/mikefarah/yq), v4.x, on PATH.
#
# Usage:
#   hack/migrate-llm-profile.sh [-in old-cr.yaml] [-out new-cr.yaml]
#   hack/migrate-llm-profile.sh < old-cr.yaml > new-cr.yaml
#
# The base spec.kubernautAgent.llm block is copied wholesale (minus the two
# fields handled specially below) into spec.llmProfiles.primary, rather than
# naming each field explicitly: this means the script doesn't need updating
# every time a provider gains a new LLMProfileSpec field upstream.
#
# Phase overrides that set a different provider than the base profile, or
# an inline apiKey, have no equivalent in the new schema (phaseModels
# values must reference a profile sharing the base profile's
# credentialsSecretName) and are reported as errors -- all of them, not
# just the first -- rather than silently dropped or converted into an
# invalid CR.
set -euo pipefail

readonly CROSS_PROVIDER_ISSUE="https://github.com/jordigilh/kubernaut/issues/1676"
readonly PRIMARY_PROFILE="primary"

usage() {
  cat <<'USAGE'
Migrates a Kubernaut CR from the 1.5 spec.kubernautAgent.llm shape to the
1.6 spec.llmProfiles + llmProfileRef shape (see docs/upgrade-1.5-to-1.6.md).

Usage:
  migrate-llm-profile.sh [-in old-cr.yaml] [-out new-cr.yaml]
USAGE
}

die() {
  echo "migrate-llm-profile: $*" >&2
  exit 1
}

command -v yq >/dev/null 2>&1 || die "yq not found on PATH -- install https://github.com/mikefarah/yq (v4.x)"

in_path=""
out_path=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -in) in_path="${2:-}"; shift 2 ;;
    -out) out_path="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

doc="$workdir/doc.yaml"
if [[ -n "$in_path" ]]; then
  cp "$in_path" "$doc"
else
  cat > "$doc"
fi

if ! parse_err=$(yq eval '.' "$doc" 2>&1 >/dev/null); then
  die "parsing input YAML: $parse_err"
fi

# Idempotent no-op: absent spec, absent kubernautAgent, absent llm, or an
# already-migrated CR (llm already removed) all fall through here
# unmodified. This lets the script be run repeatedly without first checking
# a CR's migration state.
has_llm="$(yq eval '.spec.kubernautAgent.llm != null' "$doc")"
if [[ "$has_llm" != "true" ]]; then
  if [[ -n "$out_path" ]]; then
    cp "$doc" "$out_path"
  else
    cat "$doc"
  fi
  exit 0
fi

base_provider="$(yq eval '.spec.kubernautAgent.llm.provider // ""' "$doc")"
runtime_cm="$(yq eval '.spec.kubernautAgent.llm.runtimeConfigMapName // ""' "$doc")"

# Pass 1: read and validate every phase override *before* mutating anything,
# capturing model/endpoint into arrays keyed by phase. This must all happen
# before any -i write to $doc: yq's `del()` on a piped copy (see the
# spec.llmProfiles.primary assignment below) mutates the shared underlying
# node, not just the copy, so .spec.kubernautAgent.llm.phaseModels would
# already be gone by a later pass if we re-queried $doc for it then.
errors=()
valid_phases=()
declare -A phase_model
declare -A phase_endpoint
mapfile -t phases < <(yq eval '.spec.kubernautAgent.llm.phaseModels // {} | keys | .[]' "$doc")
for phase in "${phases[@]}"; do
  [[ -n "$phase" ]] || continue
  # NB: `export` is required here, not just `PHASE="$phase" cmd=$(yq ...)` --
  # that form has no command word (it's two assignments), so bash never
  # puts PHASE in yq's actual process environment; strenv(PHASE) would
  # silently see an empty string.
  export PHASE="$phase"
  api_key="$(yq eval '.spec.kubernautAgent.llm.phaseModels[strenv(PHASE)].apiKey // ""' "$doc")"
  override_provider="$(yq eval '.spec.kubernautAgent.llm.phaseModels[strenv(PHASE)].provider // ""' "$doc")"

  if [[ -n "$api_key" ]]; then
    errors+=("phase \"$phase\": sets an inline apiKey, which has no equivalent in spec.llmProfiles (credentials are Secret-based only) -- create a Secret with this key and add a spec.llmProfiles entry + spec.kubernautAgent.phaseModels[\"$phase\"] referencing it by hand")
    continue
  fi
  if [[ -n "$override_provider" && "$override_provider" != "$base_provider" ]]; then
    errors+=("phase \"$phase\": overrides provider from \"$base_provider\" to \"$override_provider\", which is not representable in the new schema -- a phaseModels profile must share the base profile's credentialsSecretName (tracked in $CROSS_PROVIDER_ISSUE); resolve by hand")
    continue
  fi
  valid_phases+=("$phase")
  phase_model["$phase"]="$(yq eval '.spec.kubernautAgent.llm.phaseModels[strenv(PHASE)].model // ""' "$doc")"
  phase_endpoint["$phase"]="$(yq eval '.spec.kubernautAgent.llm.phaseModels[strenv(PHASE)].endpoint // ""' "$doc")"
done

if [[ ${#errors[@]} -gt 0 ]]; then
  echo "migrate-llm-profile: ${#errors[@]} phase override(s) require manual migration:" >&2
  for e in "${errors[@]}"; do
    echo "- $e" >&2
  done
  exit 1
fi

# Pass 2: all phases validated and their override data captured -- perform
# the actual migration.
yq eval -i '.spec.llmProfiles.primary = (.spec.kubernautAgent.llm | del(.phaseModels) | del(.runtimeConfigMapName))' "$doc"

for phase in "${valid_phases[@]}"; do
  export PHASE="$phase"
  yq eval -i '.spec.llmProfiles[strenv(PHASE)] = .spec.llmProfiles.primary' "$doc"
  if [[ -n "${phase_model[$phase]}" ]]; then
    MODEL="${phase_model[$phase]}" yq eval -i '.spec.llmProfiles[strenv(PHASE)].model = strenv(MODEL)' "$doc"
  fi
  if [[ -n "${phase_endpoint[$phase]}" ]]; then
    ENDPOINT="${phase_endpoint[$phase]}" yq eval -i '.spec.llmProfiles[strenv(PHASE)].endpoint = strenv(ENDPOINT)' "$doc"
  fi
  yq eval -i '.spec.kubernautAgent.phaseModels[strenv(PHASE)] = strenv(PHASE)' "$doc"
done

if [[ -n "$runtime_cm" ]]; then
  RUNTIME_CM="$runtime_cm" yq eval -i '.spec.kubernautAgent.runtimeConfigMapName = strenv(RUNTIME_CM)' "$doc"
fi

PRIMARY="$PRIMARY_PROFILE" yq eval -i '.spec.kubernautAgent.llmProfileRef = strenv(PRIMARY)' "$doc"
yq eval -i 'del(.spec.kubernautAgent.llm)' "$doc"

if [[ -n "$out_path" ]]; then
  cp "$doc" "$out_path"
else
  cat "$doc"
fi

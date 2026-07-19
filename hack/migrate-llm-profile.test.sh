#!/usr/bin/env bash
#
# Fixture-based test suite for hack/migrate-llm-profile.sh. No bats/shellspec
# convention exists elsewhere in this repo, so this intentionally avoids
# adding a new test-framework dependency: each scenario is a `test_*`
# function asserting on the script's exit code, stderr, and stdout (queried
# with yq), discovered and run by the driver at the bottom of this file.
#
# Run via: make test-hack-scripts
# Scenario IDs (MIG-001..015) match docs/tests/219/TEST_PLAN.md.
set -uo pipefail
# Without this, `fixture | run_migrate` would run run_migrate (the last
# stage of the pipeline) in a subshell, silently discarding every variable
# it sets (TMP_OUT, LAST_EXIT, LAST_STDERR, ...) once the pipe closes.
# lastpipe makes the last stage run in the current shell instead -- it only
# takes effect with job control off, which is already the case for a
# non-interactive script like this one.
shopt -s lastpipe

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly MIGRATE="$SCRIPT_DIR/migrate-llm-profile.sh"

pass_count=0
fail_count=0
current_test=""

fail() {
  fail_count=$((fail_count + 1))
  echo "FAIL: $current_test: $*" >&2
}

# run_migrate <fixture-yaml-string...> -- writes the fixture to a temp file,
# runs the script against it, and leaves the temp dir/output/exit code in
# $TMP_IN/$TMP_OUT/$LAST_STDOUT/$LAST_STDERR/$LAST_EXIT for the caller to
# assert on. A fresh temp dir per call avoids cross-test contamination.
run_migrate() {
  TMP_DIR="$(mktemp -d)"
  TMP_IN="$TMP_DIR/in.yaml"
  TMP_OUT="$TMP_DIR/out.yaml"
  TMP_ERR="$TMP_DIR/err.txt"
  cat > "$TMP_IN"
  if "$MIGRATE" -in "$TMP_IN" -out "$TMP_OUT" 2>"$TMP_ERR"; then
    LAST_EXIT=0
  else
    LAST_EXIT=$?
  fi
  LAST_STDERR="$(cat "$TMP_ERR")"
}

yq_at() {
  yq eval "$1" "$TMP_OUT"
}

assert_exit() {
  [[ "$LAST_EXIT" -eq "$1" ]] || fail "expected exit $1, got $LAST_EXIT (stderr: $LAST_STDERR)"
}

assert_eq() {
  local actual="$1" expected="$2" what="$3"
  [[ "$actual" == "$expected" ]] || fail "$what: expected [$expected], got [$actual]"
}

assert_contains() {
  local haystack="$1" needle="$2" what="$3"
  [[ "$haystack" == *"$needle"* ]] || fail "$what: expected to contain [$needle], got [$haystack]"
}

assert_not_contains() {
  local haystack="$1" needle="$2" what="$3"
  [[ "$haystack" != *"$needle"* ]] || fail "$what: expected NOT to contain [$needle], got [$haystack]"
}

# --- fixtures -----------------------------------------------------------

fixture_base_only() {
  cat <<'EOF'
apiVersion: kubernaut.ai/v1alpha1
kind: Kubernaut
metadata:
  name: kubernaut
  namespace: kubernaut-system
spec:
  postgresql:
    secretName: postgresql-secret
  kubernautAgent:
    llm:
      provider: openai
      model: gpt-4o
      credentialsSecretName: llm-credentials
      endpoint: https://api.openai.com/v1
    maxTurns: 40
EOF
}

# phase_override mirrors the real pre-#187 sample CR: a vertex_ai base
# profile with a workflow_discovery phase override that only changes the
# model.
fixture_phase_override() {
  cat <<'EOF'
apiVersion: kubernaut.ai/v1alpha1
kind: Kubernaut
metadata:
  name: kubernaut
  namespace: kubernaut-system
spec:
  kubernautAgent:
    llm:
      provider: vertex_ai
      model: claude-sonnet-4-6
      credentialsSecretName: llm-credentials
      vertexProject: example-gcp-project
      vertexLocation: us-central1
      maxRetries: 3
      timeoutSeconds: 120
      phaseModels:
        workflow_discovery:
          model: claude-haiku-4-5
    logging:
      level: debug
    maxTurns: 40
    audit:
      enabled: true
EOF
}

# --- scenarios ------------------------------------------------------------

test_MIG_001() {
  fixture_base_only | run_migrate
  assert_exit 0
  assert_eq "$(yq_at '.spec.llmProfiles.primary.provider')" "openai" "primary.provider"
  assert_eq "$(yq_at '.spec.llmProfiles.primary.model')" "gpt-4o" "primary.model"
  assert_eq "$(yq_at '.spec.llmProfiles.primary.credentialsSecretName')" "llm-credentials" "primary.credentialsSecretName"
  assert_eq "$(yq_at '.spec.llmProfiles.primary.endpoint')" "https://api.openai.com/v1" "primary.endpoint"
  assert_eq "$(yq_at '.spec.kubernautAgent.llmProfileRef')" "primary" "kubernautAgent.llmProfileRef"
  assert_eq "$(yq_at '.spec.kubernautAgent.llm == null')" "true" "kubernautAgent.llm must be removed"
  assert_eq "$(yq_at '.spec.kubernautAgent.phaseModels == null')" "true" "no phaseModels expected"
}

test_MIG_002() {
  cat <<'EOF' | run_migrate
spec:
  kubernautAgent:
    llm:
      provider: openai
      model: gpt-4o
      credentialsSecretName: llm-credentials
      endpoint: https://api.openai.com/v1
      runtimeConfigMapName: my-custom-runtime-cm
EOF
  assert_exit 0
  assert_eq "$(yq_at '.spec.kubernautAgent.runtimeConfigMapName')" "my-custom-runtime-cm" "kubernautAgent.runtimeConfigMapName"
  assert_eq "$(yq_at '.spec.llmProfiles.primary.runtimeConfigMapName == null')" "true" "runtimeConfigMapName must not remain on the profile"
}

test_MIG_003() {
  fixture_phase_override | run_migrate
  assert_exit 0
  assert_eq "$(yq_at '.spec.kubernautAgent.phaseModels.workflow_discovery')" "workflow_discovery" "phaseModels ref"
  assert_eq "$(yq_at '.spec.llmProfiles.workflow_discovery.model')" "claude-haiku-4-5" "phase profile model"
  assert_eq "$(yq_at '.spec.llmProfiles.workflow_discovery.provider')" "vertex_ai" "phase profile inherits base provider"
  assert_eq "$(yq_at '.spec.llmProfiles.workflow_discovery.credentialsSecretName')" "llm-credentials" "phase profile must share base credentialsSecretName"
  assert_eq "$(yq_at '.spec.llmProfiles.workflow_discovery.vertexProject')" "example-gcp-project" "phase profile inherits non-overridden fields"
}

test_MIG_004() {
  cat <<'EOF' | run_migrate
spec:
  kubernautAgent:
    llm:
      provider: openai
      model: gpt-4o
      credentialsSecretName: llm-credentials
      endpoint: https://api.openai.com/v1
      phaseModels:
        rca:
          model: gpt-4o-mini
          endpoint: https://api.openai.com/v1/rca-pool
EOF
  assert_exit 0
  assert_eq "$(yq_at '.spec.llmProfiles.rca.model')" "gpt-4o-mini" "rca profile model"
  assert_eq "$(yq_at '.spec.llmProfiles.rca.endpoint')" "https://api.openai.com/v1/rca-pool" "rca profile endpoint"
}

test_MIG_005() {
  cat <<'EOF' | run_migrate
spec:
  kubernautAgent:
    llm:
      provider: vertex_ai
      model: claude-sonnet-4-6
      credentialsSecretName: llm-credentials
      phaseModels:
        rca:
          provider: openai
          model: gpt-4o
EOF
  assert_exit 1
  assert_contains "$LAST_STDERR" '"rca"' "error names the phase"
  assert_contains "$LAST_STDERR" "vertex_ai" "error names the base provider"
  assert_contains "$LAST_STDERR" "openai" "error names the override provider"
  assert_contains "$LAST_STDERR" "jordigilh/kubernaut/issues/1676" "error cites the cross-credential tracking issue"
  [[ ! -s "$TMP_OUT" ]] || fail "no output should be written on validation failure"
}

test_MIG_006() {
  cat <<'EOF' | run_migrate
spec:
  kubernautAgent:
    llm:
      provider: openai
      model: gpt-4o
      credentialsSecretName: llm-credentials
      endpoint: https://api.openai.com/v1
      phaseModels:
        validation:
          model: gpt-4o-mini
          apiKey: sk-inline-secret-value
EOF
  assert_exit 1
  assert_contains "$LAST_STDERR" '"validation"' "error names the phase"
  assert_contains "$LAST_STDERR" "apiKey" "error names the offending field"
  assert_not_contains "$LAST_STDERR" "sk-inline-secret-value" "error must not leak the inline credential value"
}

test_MIG_007() {
  cat <<'EOF' | run_migrate
spec:
  kubernautAgent:
    llm:
      provider: vertex_ai
      model: claude-sonnet-4-6
      credentialsSecretName: llm-credentials
      phaseModels:
        rca:
          provider: openai
          model: gpt-4o
        validation:
          model: gpt-4o-mini
          apiKey: sk-inline-secret-value
EOF
  assert_exit 1
  assert_contains "$LAST_STDERR" '"rca"' "aggregated error includes rca"
  assert_contains "$LAST_STDERR" '"validation"' "aggregated error includes validation"
  assert_contains "$LAST_STDERR" "2 phase override(s)" "error reports the aggregate count"
}

test_MIG_008() {
  local cr
  cr="$(cat <<'EOF'
spec:
  llmProfiles:
    primary:
      provider: openai
      model: gpt-4o
      credentialsSecretName: llm-credentials
      endpoint: https://api.openai.com/v1
  kubernautAgent:
    llmProfileRef: primary
EOF
)"
  echo "$cr" | run_migrate
  assert_exit 0
  diff <(echo "$cr") "$TMP_OUT" >/dev/null || fail "already-migrated CR should pass through byte-for-byte unchanged"
}

test_MIG_009() {
  fixture_base_only | run_migrate
  assert_exit 0
  assert_eq "$(yq_at '.metadata.name')" "kubernaut" "metadata.name preserved"
  assert_eq "$(yq_at '.metadata.namespace')" "kubernaut-system" "metadata.namespace preserved"
  assert_eq "$(yq_at '.spec.postgresql.secretName')" "postgresql-secret" "unrelated spec field preserved"
  assert_eq "$(yq_at '.spec.kubernautAgent.maxTurns')" "40" "unrelated kubernautAgent field preserved"
}

# MIG-010 (round-trip through the real internal/resources.ValidateKubernaut)
# is a documented manual check, not an automated assertion here -- see
# docs/tests/219/TEST_PLAN.md section 3.

test_MIG_011() {
  cat <<'EOF' | run_migrate
spec:
  kubernautAgent:
    llm:
      provider: openai
      model: gpt-4o
      credentialsSecretName: llm-credentials
      endpoint: https://api.openai.com/v1
      oauth2:
        enabled: true
        tokenURL: https://idp.example.com/oauth2/token
        scopes: ["llm.invoke"]
        credentialsSecretRef: llm-oauth2-creds
EOF
  assert_exit 0
  assert_eq "$(yq_at '.spec.llmProfiles.primary.oauth2.enabled')" "true" "oauth2.enabled"
  assert_eq "$(yq_at '.spec.llmProfiles.primary.oauth2.tokenURL')" "https://idp.example.com/oauth2/token" "oauth2.tokenURL"
  assert_eq "$(yq_at '.spec.llmProfiles.primary.oauth2.scopes[0]')" "llm.invoke" "oauth2.scopes"
  assert_eq "$(yq_at '.spec.llmProfiles.primary.oauth2.credentialsSecretRef')" "llm-oauth2-creds" "oauth2.credentialsSecretRef"
}

test_MIG_012() {
  cat <<'EOF' | run_migrate
spec:
  kubernautAgent:
    llm:
      provider: openai
      model: gpt-4o
      credentialsSecretName: llm-credentials
      endpoint: https://api.openai.com/v1
      tlsCertFile: /etc/kubernaut-agent/llm-tls-client/tls.crt
      tlsKeyFile: /etc/kubernaut-agent/llm-tls-client/tls.key
      tlsClientSecretRef: llm-client-tls
EOF
  assert_exit 0
  assert_eq "$(yq_at '.spec.llmProfiles.primary.tlsCertFile')" "/etc/kubernaut-agent/llm-tls-client/tls.crt" "tlsCertFile"
  assert_eq "$(yq_at '.spec.llmProfiles.primary.tlsKeyFile')" "/etc/kubernaut-agent/llm-tls-client/tls.key" "tlsKeyFile"
  assert_eq "$(yq_at '.spec.llmProfiles.primary.tlsClientSecretRef')" "llm-client-tls" "tlsClientSecretRef"
}

test_MIG_013() {
  printf 'spec:\n  kubernautAgent: [this is not a map' | run_migrate
  assert_exit 1
  assert_contains "$LAST_STDERR" "parsing input YAML" "clear parse error, not a silent failure"
}

test_MIG_014() {
  local cr
  cr="$(cat <<'EOF'
metadata:
  name: kubernaut
spec:
  postgresql:
    secretName: postgresql-secret
EOF
)"
  echo "$cr" | run_migrate
  assert_exit 0
  diff <(echo "$cr") "$TMP_OUT" >/dev/null || fail "CR with no kubernautAgent should pass through unchanged"
}

test_MIG_015() {
  local cr
  cr="$(cat <<'EOF'
metadata:
  name: kubernaut
EOF
)"
  echo "$cr" | run_migrate
  assert_exit 0
  diff <(echo "$cr") "$TMP_OUT" >/dev/null || fail "CR with no spec should pass through unchanged"
}

# --- driver -----------------------------------------------------------

command -v yq >/dev/null 2>&1 || { echo "yq not found on PATH -- see Makefile's 'yq' target" >&2; exit 1; }

for t in $(declare -F | awk '{print $3}' | grep '^test_MIG_' | sort); do
  current_test="$t"
  before=$fail_count
  "$t"
  if [[ $fail_count -eq $before ]]; then
    pass_count=$((pass_count + 1))
    echo "PASS: $t"
  fi
done

echo ""
echo "$pass_count passed, $fail_count failed"
[[ $fail_count -eq 0 ]]

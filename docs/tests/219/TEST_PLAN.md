# IEEE 829 Test Plan — Issue #219: 1.5→1.6 LLM profile migration guidance/helper

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-219                                             |
| **Issue**          | #219 — Provide 1.5→1.6 LLM profile migration guidance/helper (`spec.kubernautAgent.llm` → `spec.llmProfiles`) |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-07-18                                         |
| **Revised**        | 2026-07-19 — replaced the Go implementation (`internal/migrate`, `hack/migrate-llm-profile`) with a standalone bash+yq script. Rationale: a Go CLI forces end users to either build/publish a binary per platform or clone the whole module and `go run` it, which is disproportionate for a single-use, one-time upgrade helper. A single `curl`-able bash script needs only `yq` (a common, single-static-binary CLI most operators already have or can trivially install), with no Go toolchain or repo clone required. Maintaining two independent implementations of the same non-trivial branching logic (cross-provider rejection, apiKey rejection, phase-override merge) in two languages, where only one would ever actually be run, was rejected as a drift risk — this revision is a full replacement, not an addition. |
| **Scope**          | `hack/migrate-llm-profile.sh`, `hack/migrate-llm-profile.test.sh`, `docs/upgrade-1.5-to-1.6.md` |

## 1. Objective

#187/#216 removed `spec.kubernautAgent.llm` entirely in favor of
`spec.llmProfiles` + `llmProfileRef`, with no automated in-operator
conversion (accepted, pre-1.0). This left a real gap for anyone upgrading an
existing 1.5 CR: no documentation and no tooling to translate the old shape
to the new one.

Verify that:
1. A best-effort bash+yq conversion script (`hack/migrate-llm-profile.sh`)
   correctly translates the common case (base LLM config, optional
   same-provider/model-only phase overrides) to the new shape.
2. The script *refuses* — with a clear, actionable error on stderr and a
   non-zero exit code, not a silently invalid or lossy CR — the two cases
   the new schema cannot represent: phase overrides with a different
   `provider`, and phase overrides with an inline `apiKey` (no Secret-based
   credential equivalent).
3. The script's output is structurally correct (right paths, right values)
   for a fixture mirroring the actual pre-refactor sample CR (`vertex_ai` +
   `workflow_discovery` phase override), and has been manually confirmed to
   pass the real `internal/resources.ValidateKubernaut` admission logic with
   zero LLM-profile-related errors (see MIG-010 below — this one scenario is
   a documented manual check, not an automated bash assertion, since
   invoking the real Go validation function from bash would require
   reintroducing Go tooling this revision specifically removes).
4. Unrelated CR content (metadata, other spec fields) survives the
   transform untouched.
5. A new `docs/upgrade-1.5-to-1.6.md` exists so a human can actually
   run/read this without spelunking shell source.

## 2. Test Strategy

Fixture-based bash tests only (`hack/migrate-llm-profile.test.sh`) — this is
a stateless YAML→YAML transform with no Kubernetes API interaction, so no
envtest/integration tier applies. There is no bats/shellspec convention
elsewhere in this repo, so rather than introduce a new test-framework
dependency for one script, the test runner is plain bash: each scenario is a
function that runs the script against an inline fixture and asserts on exit
code, stderr content, and `yq`-queried fields of the output, matching the
assertion style already used by `internal/resources`' Ginkgo suites (one
behavioral claim per assertion, not a single diff-the-whole-document check
in the general case).

Pyramid Invariant is satisfied at this single tier by design: there is no
wiring point into the running operator (the script is standalone dev-time
tooling, never invoked by anything in `cmd/main.go` or the container image),
so IT/E2E tiers are inapplicable, not skipped for convenience.

`yq` (`github.com/mikefarah/yq/v4`) is provisioned via the Makefile's
existing `go-install-tool` pattern (mirrors `controller-gen`/`envtest`/
`golangci-lint`) — pinned version, `bin/yq`, no manual setup for
CI or developers. This is orthogonal to the script's own runtime dependency
on `yq` being present on the *end user's* machine (documented in
`docs/upgrade-1.5-to-1.6.md`).

## 3. Test Scenarios (`hack/migrate-llm-profile.test.sh`)

| ID      | FedRAMP | Description | Automated? |
|---------|---------|-------------|------------|
| MIG-001 | CM-6    | migrates a base-only old CR (no `phaseModels`) — `llmProfiles.primary` populated from `kubernautAgent.llm`, `kubernautAgent.llmProfileRef` set to `"primary"`, `kubernautAgent.llm` removed | Yes |
| MIG-002 | CM-6    | migrates `llm.runtimeConfigMapName` to the new `kubernautAgent.runtimeConfigMapName` location (moved up one level in the new schema) | Yes |
| MIG-003 | CM-6    | migrates a phase override that only changes `model` — creates a new named profile sharing the base's `credentialsSecretName`, wires `phaseModels[phase]` to it | Yes |
| MIG-004 | CM-6    | migrates a phase override that also changes `endpoint` — new profile carries the overridden endpoint | Yes |
| MIG-005 | CM-6    | rejects (clear error, not silent drop/corruption) a phase override with a `provider` different from the base profile's, naming the phase and citing the cross-credential constraint (`jordigilh/kubernaut#1676`) | Yes |
| MIG-006 | IA-5    | rejects a phase override with an inline `apiKey` — new schema is Secret-only for credentials; error tells the user to create a Secret and wire it by hand; error text must NOT echo the key value | Yes |
| MIG-007 | CM-6    | aggregates errors from multiple bad phases in one call rather than stopping at the first | Yes |
| MIG-008 | CM-6    | is a clear no-op (success, input returned unchanged) when given an already-migrated CR (`spec.llmProfiles` present, no `spec.kubernautAgent.llm`) | Yes |
| MIG-009 | CM-6    | leaves unrelated CR content (`metadata`, `spec.postgresql`, `spec.valkey`, etc.) untouched through the transform | Yes |
| MIG-010 | CM-6    | migrated output, using a fixture mirroring the actual pre-refactor sample CR (`vertex_ai` + `workflow_discovery` phase override), passes the real `internal/resources.ValidateKubernaut` with zero LLM-profile-related errors | Manual (documented in PR description) |
| MIG-011 | IA-5    | OAuth2 config (`llm.oauth2`) carried into the new profile unchanged | Yes |
| MIG-012 | SC-8    | mTLS fields (`tlsCertFile`/`tlsKeyFile`/`tlsClientSecretRef`) carried into the new profile unchanged | Yes |
| MIG-013 | CM-6    | rejects malformed top-level YAML with a clear parse error, not a silent empty/garbage output | Yes |
| MIG-014 | CM-6    | is a no-op on a CR with no `spec.kubernautAgent` at all | Yes |
| MIG-015 | CM-6    | is a no-op on a CR with no `spec` at all | Yes |

## 4. Acceptance Criteria

- All automated scenarios above pass via `make test-hack-scripts`, run in CI
  (`.github/workflows/test.yml`) alongside the existing Go test job.
- `shellcheck hack/migrate-llm-profile.sh hack/migrate-llm-profile.test.sh`
  reports no warnings.
- MIG-010 manually verified once against the fixture and recorded in the PR
  description (script output piped through a throwaway Go program calling
  `resources.ValidateKubernaut` directly, since no such entry point exists
  in bash without reintroducing Go tooling).
- `docs/upgrade-1.5-to-1.6.md` exists, follows the structure of
  `docs/upgrade-1.4-to-1.5.md`, documents the breaking change with the
  before/after example, and explains both script-supported and
  manual-only migration paths.
- No change to `cmd/main.go`'s import graph, and no new Go source files —
  the migration helper is pure bash+yq dev-time tooling, not shipped in the
  operator's container image.

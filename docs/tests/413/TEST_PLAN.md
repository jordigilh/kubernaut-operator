# IEEE 829 Test Plan — Issue #413: kubernaut-agent fleet OAuth2 mount path stale vs kubernaut's current hardcoded string

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-413                                             |
| **Issue**          | #413 — kubernaut-agent fleet OAuth2 mount path stale vs kubernaut's current hardcoded string (kubernaut#1729 broke #204's contract) — silent fallback to built-in K8s tools |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-08-25                                         |
| **Scope**          | `internal/resources/{deployments,configmaps}.go`, `internal/resources/deployments_test.go`, `internal/controller/kubernaut_controller_test.go` |

## 1. Objective

#204 (2026-07-23) mounted KA's fleet-oauth2 credentials Secret at the
unhyphenated `/etc/kubernautagent/<credentialsSecretRef>` because upstream's
`registerFleetTools()` hardcoded that literal path at the time. `kubernaut`
issue #1729 (2026-08-01, 9 days later) changed that hardcoded prefix to the
hyphenated `/etc/kubernaut-agent/<credentialsSecretRef>` to match the Helm
chart's mount convention, without checking `kubernaut-operator`'s CRD-based
deployment path. This silently invalidated #204's contract: KA mounts the
Secret at one path and looks for it at another, so
`fleetOAuth2CredentialsBasePath()` fails to find the credential files, KA
falls back to an unauthenticated/static transport, and fleet tool discovery
degrades to KA's built-in single-cluster tools -- all without any
user-visible error (fail-open).

### Preflight evidence (direct source reads, no inference from the issue text)

1. `kubernaut-operator`'s [internal/resources/deployments.go](../../../internal/resources/deployments.go)
   still mounted the fleet-oauth2 Secret at the unhyphenated
   `/etc/kubernautagent` (the #204 contract).
2. `kubernaut` repo (`main` @ `10a6e10e`, 2026-08-23) confirms the new,
   hyphenated convention in three independent, mutually-consistent places:
   - `cmd/kubernautagent/toolregistry.go`'s `fleetOAuth2CredentialsBasePath()`
     returns `"/etc/kubernaut-agent/" + secretRef`.
   - `charts/kubernaut/templates/kubernaut-agent/kubernaut-agent.yaml` mounts
     `fleet-oauth2-credentials` at `"/etc/kubernaut-agent/{{ credentialsSecretRef }}"`.
   - Every other KA mount in both repos (config, llm-runtime, credentials,
     oauth2, llm-tls-client) already uses the hyphenated
     `/etc/kubernaut-agent` prefix -- KA's fleet-oauth2 mount was the only
     exception, and the exception's premise no longer holds.
3. No spike required: the fix direction is unambiguous and matches the
   issue's own primary suggested fix (align the operator to KA's current,
   triply-cross-referenced convention, rather than asking upstream to
   revert #1729's Helm-chart-parity work).

**Explicitly out of scope**: reverting `kubernaut`#1729 (higher blast
radius, undoes Helm-chart parity; the issue itself frames this as the less
preferred alternative). Adding a cross-repo contract test that would catch
future drift automatically (candidate for a follow-up issue, non-blocking
here).

## 2. Test Strategy

Standard TDD (RED -> GREEN -> REFACTOR). Two pre-existing unit tests already
assert the (now-stale) mount path exactly, so RED is a direct flip of their
expected value plus their now-incorrect descriptions; GREEN is a one-line
constant change. One new integration test is added to satisfy the Pyramid
Invariant, since no existing IT test asserted the Deployment mount path
end-to-end through the reconcile loop.

## 3. Test Scenarios

### 3.1 KA Deployment mount (`internal/resources/deployments_test.go`, `KubernautAgentDeployment`)

Business objective: the fleet OAuth2 client credentials must land exactly
where KA's `fleetOAuth2CredentialsBasePath()` currently reads them, or KA
silently runs fleet tool discovery unauthenticated (or not at all) instead
of failing closed with an actionable error (IA-5: authenticator management).

| ID      | FedRAMP | Description | Automated? |
|---------|---------|--------------|------------|
| KFG-021 | IA-5    | The fleet-oauth2 Secret is mounted at `/etc/kubernaut-agent/<effective credentialsSecretRef>` (KA's current hyphenated convention) when fleet OAuth2 is enabled | Yes |
| KFG-021b | IA-5   | KA's own `FleetOAuth2CredentialsSecretRef` override is used for the mount when set, still at the hyphenated path, not the shared `credentialsSecretRef` | Yes |

### 3.2 Controller integration (`internal/controller/kubernaut_controller_test.go`)

Business objective: prove the reconcile loop actually persists the
corrected, hyphenated mount path to a live Deployment object end-to-end via
a real envtest API server and controller run, not just via Go-level struct
construction in unit tests (Pyramid Invariant: IT proves wiring).

| ID      | FedRAMP | Description | Automated? |
|---------|---------|--------------|------------|
| KFG-061 | IA-5    | A Kubernaut CR with `spec.fleet.oauth2.enabled=true` reconciles successfully and KA's live Deployment mounts `fleet-oauth2` at `/etc/kubernaut-agent/fleet-oauth2-creds`, not the stale unhyphenated `/etc/kubernautagent` | Yes |

### 3.3 No behavior change (regression)

| ID      | Description | Automated? |
|---------|--------------|------------|
| N/A     | All pre-existing KA ConfigMap/Deployment/NetworkPolicy fixtures unrelated to the fleet-oauth2 mount path continue to render byte-identical output (full `internal/resources` and `internal/controller` suites re-run, 0 unrelated regressions) | Yes |

## 4. Acceptance Criteria

- KFG-021/KFG-021b (unit) and KFG-061 (integration) all pass via
  `make test-unit` and `make test-integration`.
- `make lint` reports 0 new issues.
- `make manifests generate` produces no diff (no CRD/API schema change --
  this is a resource-builder constant fix only).
- No regressions in the existing `internal/resources`
  (target 96%+ coverage) or `internal/controller` (target 78%+ coverage)
  suites.
- No remaining references to the stale unhyphenated `/etc/kubernautagent`
  path in production code or active tests (historical references in
  `docs/tests/204/TEST_PLAN.md` and `CHANGELOG.md` are left as-is: they
  document the state of the world at the time #204 was written, not current
  behavior).

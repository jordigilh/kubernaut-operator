# Issue #514 — Ownership safety test plan

**Contract:** `docs/design/ISSUE-514-OWNERSHIP-CONTRACT.md`
**Status:** ownership implementation complete; final configured host gates passed.
Final-image live qualification is blocked by Podman storage exhaustion. No
release/compliance claim. Results: [VALIDATION.md](VALIDATION.md).

Ginkgo/Gomega tests assert business outcomes, not just helper invocation. All
existing resource builders and controller logic are real; mocks inject only
Kubernetes API errors/races. Envtest lacks GC, so real-cluster cleanup remains a
separate journey. No sleep, pending tests, or skipped security scenarios.

| Business assertion | Unit evidence (logic) | Envtest evidence (wiring) | E2E evidence (journey) |
|---|---|---|---|
| `BA-OWN-514-01`: administrator same-name objects are unchanged, including forged matching hashes and AlreadyExists races | `UT-OWN-514-001` ownership rejection matrix | `IT-OWN-514-001`, `002`; `IT-OWN-514-009` API race | `E2E-OWN-514-001` installed operator conflict, recovery and preservation |
| `BA-OWN-514-02`: authorized creation/upgrade/reinstall is idempotent; no foreign-owner/namespace takeover | `UT-OWN-514-002`, `003` identity/reinstall matrix | `IT-OWN-514-003`, `004`, `016` | `E2E-OWN-514-001` recovery and new-UID reinstall |
| `BA-OWN-514-03`: disable, stale pruning and uninstall remove only eligible operator resources, with identity/version preconditions | `UT-OWN-514-001`, `002` shared authorization predicate | `IT-OWN-514-005`, `006`, `011`; actual Reconcile deletion/replacement race | `E2E-OWN-514-001` failed-install/uninstall preservation |
| `BA-OWN-514-04`: TLS, migration and shared CRD seams cannot bypass ownership | `UT-OWN-514-004` shared CRD real dynamic-client requests | `IT-OWN-514-007`, `008`, `013`, `018`; actual Reconcile rejects completed foreign Job/Certificate, shared CRD and stale foreign webhook CA before metadata preservation | existing TLS/provider E2E lanes plus `E2E-OWN-514-001` |
| `BA-OWN-514-05`: conflicts expose object/owner identity without logging credential data | rejection tests have owner/marker diagnostics | `IT-OWN-514-001`, `002` capture Warning events, structured logs and false readiness | `E2E-OWN-514-001` CR condition/event observation |
| `BA-OWN-514-06`: provider/platform policies retain their five-marker ownership boundary, reject foreign owners and repair only same-identity reinstall references | `UT-OWN-514-005` independent policy authorization matrix; existing provider logic tests | `IT-OWN-514-019` actual Reconcile update/prune/finalizer and matching-hash owner repair; existing provider lifecycle tests | existing Cilium/Calico unmanaged-policy preservation journey |
| `BA-OWN-514-07`: neither namespace is deleted; provisioning-owned workflows, DB/Valkey and administrator inputs survive uninstall/reinstall; prepared administrator namespaces remain read-only | `UT-OWN-514-001`, `002` authorization matrix and existing workflow namespace builder tests | `IT-OWN-514-010`, `012`, `015`, `016`, `017`; namespace UID retention/reuse regression | `E2E-OWN-514-001` namespace UID, workflow ConfigMap, DB/Valkey Deployment/Service and input Secret/ConfigMap witnesses; final journey cleanup rechecks preservation |

## Control-objective mapping

Versioned objective details and residual limits are recorded in
`docs/security/ISSUE-514-CONTROL-TRACEABILITY.md`. NIST SP 800-53 Rev. 5/FedRAMP
`AC-3`, `AC-6`, `SI-10` map to deny-by-default mutations and cleanup; `CM-3`,
`CM-6`, `CM-8` to deterministic identity and the upgrade boundary; `AU-2`, `AU-3`,
`AU-12`, `SI-4` to actionable security diagnostics. SOC 2 `CC6.1`, `CC6.6`,
`CC7.2`, `CC8.1` map to authorized changes, detection and recovery. OWASP ASVS
5.0.0 `v5.0.0-V8.2.1`, `v5.0.0-V8.3.1`, `v5.0.0-V13.3.1`,
`v5.0.0-V13.3.2`, `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1`,
`v5.0.0-V16.5.2` are adapted operator-boundary objectives, not full application
conformance. This plan does not claim formal FedRAMP authorization, SOC 2
attestation, or ASVS compliance.

## Verification record

Historical evidence is retained in the session's `opencode` temporary directory:

- `514-red-namespace-content.log`: RED for `IT-OWN-514-015` proved namespace
  deletion could indirectly destroy an unmarked administrator ConfigMap. The
  approved retention fix removes namespace deletion entirely.
- `514-red-workflow-identity.log`: RED for `IT-OWN-514-016` proved restricted PSA
  could bypass a conflicting Kubernaut namespace identity. The read-only exception
  now excludes operator claims, owner references and terminating namespaces.
- `514-green-workflow-identity.log`: focused real envtest reconciliation and
  namespace retention/reuse tests passed after both fixes (42.188 seconds).
- `514-test-isolation-fix.log` and `514-kind-live.log`: earlier full-gate and
  DevelopmentSelfSigned evidence **predates** the final retention/identity
  refinements and cannot qualify the final change.
- `514-kind-hook.log`: earlier Hook rotation failure (unchanged certificate after
  deleting `gateway-tls`); the retention-image rerun in
  `514-kind-hook-final.log` passed all eight journey specs, including rotation.
- `514-red-webhook-trust.log`: `IT-OWN-514-018` failed with "Expected an error,
  got nil", proving a stale foreign webhook observation could supply CA material
  before a later owned read. The authorization gate now precedes preservation.
- `514-red-provider-unit.log` / `514-red-provider-wiring.log`: RED proved matching
  provider markers could override foreign owners in update/prune/finalization.
  `UT-OWN-514-005` and `IT-OWN-514-019` now also protect owner identity and scope;
  a matching-hash same-identity reinstall explicitly repairs the owner reference.
- `514-green-ownership-final.log`: focused real envtest reconciliation passed
  after the webhook/provider refinements (39.707 seconds). Full verification
  follows refactoring the shared provider-delete path.

Record final full-gate and live results here after execution; never count a static
pyramid check or a compiling E2E scenario as live execution. Required commands:
`go build ./...`, `golangci-lint run`, `make test`, `make test-hack-scripts`,
`make manifests generate`, `git diff --exit-code config/`, compile-only tests;
`make test-e2e-kind` on isolated clusters for the applicable TLS/provider lanes.

The live suite uses the real Helm-installed operator and public CR APIs. Its
application containers and migration completion are contract fixtures: this
proves operator ownership/wiring, not upstream business-service qualification,
FedRAMP authorization, SOC 2 attestation, or full OWASP ASVS conformance.

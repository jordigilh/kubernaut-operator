# PR #515 audit remediation

**Status:** implementation approved by the user's `proceed`, after clarification
that the existing cluster-wide singleton topology must be preserved. No new
coordination object, permissions, fixed CR namespace or CRD-scope migration is
authorized or introduced for singleton enforcement.

## Singleton spike: existing primitives are sufficient for activation safety

The validating webhook already rejects a second CR cluster-wide, but its
List-before-CREATE is not atomic. The controller previously checked only the
name. Kubernetes persistence plus the existing finalizer provide the minimal
defense-in-depth contract:

1. Before adding a finalizer or entering active phases, use the manager's
   uncached API reader to require exactly one persisted CR, matching the
   requested namespace/name/UID. API errors fail closed.
2. A reconciliation adds the existing finalizer before any active phase. Its
   next reconciliation repeats the authoritative singleton check.
3. An ordinary deleting holder remains persisted until cleanup/finalization;
   it therefore continues to block a different CR's activation. Deletion may
   clean up its own resources even when a conflicting CR exists.
4. If concurrent admission persists competing objects, fail active
   reconciliation closed with actionable diagnostics until the administrator
   removes the conflict. Do not silently select a replacement or support a
   multiple-instance topology.

Two distinct CRs cannot both observe themselves as the sole persisted CR while
the finalizer-protected first CR remains present. A competitor appearing after
the first check cannot pass its own authoritative check. A serial worker alone,
cached List or admission mutex would not establish this property.

**Spike result: YES** for at-most-one-active-CR enforcement with existing
primitives and unchanged permissions. This is not atomic pre-CREATE admission
or protection against an administrator deliberately removing finalizers or
force-deleting an active instance. Such destructive recovery remains an
administrator action requiring orphan-resource review; #517's ownership contract
must still protect those resources. The existing forced-cleanup timeout is also
reported honestly, not treated as successful cleanup.

Alternatives not selected: a namespace restriction (breaks existing namespace
flexibility), cluster-scoped CR migration (breaking storage/API change), and a
new reservation Lease (new lifecycle/RBAC without necessity for this invariant).

## Other approved repair boundaries

- Apply one explicit HTTP/2 policy to both actual metrics and webhook servers.
  Preserve administrator opt-in and certificate-watcher behavior.
- Detect live Service selector/port/type drift despite unchanged hash metadata;
  preserve allocated addressing and unrelated platform metadata during repair.
- Replace negative-only monitoring assertions with positive Reconcile-created
  monitor/rule/Service assertions; disconnection of the production call must fail.
- Keep unit profiles independent of envtest. Existing coverage debt is measured,
  never hidden by integration coverage or arbitrary business-logic exclusions.
- Depend on the separately reviewed #517 ownership changes rather than copying
  or overwriting that work. Full application qualification remains upstream
  under the approved issue #501 CI boundary.
- Required (default) native isolation blocks migration and new workloads on
  failed provider discovery; Inherited explicitly acknowledges administrator
  isolation without attesting it. Existing policies are retained on failure or
  inherited selection. B1 was selected over silent best-effort (B2) or dropping
  inherited-isolation deployments entirely (B3).
- Typed scraper sources use an exact namespace and 1–16 exact pod labels; empty
  means no external ingress. C1 was selected over external policy-only (C2) or
  guessing scraper identities from query endpoints (C3). Authorization derives
  from actual generated Service metrics ports. Cilium/Calico ingress and OVN
  component-scoped ANPs do not broaden access to other component ports.
  No raw CIDRs, namespace-only or wildcard scraper access is introduced.
- See `docs/security/PR-515-MIGRATION.md` for the intentional default change,
  administrator trust boundaries, upgrade preflight and rollback limits.

## Wiring manifest and RED evidence

Controller chains below are implemented in
`internal/controller/kubernaut_controller.go`; protocol construction is in
`cmd/main.go`, strict telemetry validation in `internal/controller/telemetry.go`,
and internal issuer identity resolution in `internal/resources/tls_certmanager.go`.
These are production paths, not test-only builder substitutes.

| Behavior | Production entry point | Tests |
|---|---|---|
| Activation authorization | `KubernautReconciler.Reconcile` before finalizer/phases | UT-AUDIT-515-SINGLETON-001–005; IT-AUDIT-515-SINGLETON-001–004 |
| Service convergence | `ensureServices` → `ensureResource` → `contentDrifted` | UT-AUDIT-515-DRIFT-001–003; IT-AUDIT-515-DRIFT-001 |
| Webhook protocol | `cmd/main.go` manager construction and actual webhook server | UT-AUDIT-515-TLS-001/002 and protocol-negotiation tests |
| Positive monitoring wiring | `Reconcile` → `reconcilePrometheusMonitoring` → `deployMonitoring` | strengthened IT-MON-513-008 and wiring-disconnection mutation |
| Required isolation and retained policies | `reconcilePhases`/`deployWorkloads` → `reconcileProviderPolicies` | UT-AUDIT-515-POLICY-001–003; IT-AUDIT-515-POLICY-001 |
| Typed scraper ingress | `reconcileProviderPolicies` → generated Service port intent → `policy.Render` | UT-AUDIT-515-SCRAPE-001–007; provider policy reconciliation including custom AF port 19180 |
| Truthful audit | `patchStatus` and `runFinalizerCleanup` | UT-AUDIT-515-AUDIT-001/002 and real-API phase/cleanup assertions |
| Complete telemetry client-chain validation | `phaseValidate` → `validateTelemetrySecrets` → strict PEM parser | UT-AUDIT-515-TELEMETRY-006; IT-TELEMETRY-AUDIT-515-001 |
| Stable automatic issuer selection | `ensureRuntimeTLS` → `resolveCertManagerIssuer` → `selectSingleCertManagerIssuer` | UT-AUDIT-515-CERTMANAGER-008; IT-TLS-AUDIT-515-004 |
| Internal issuer identities | `selectSingleCertManagerIssuer` → `resources.CertManagerBootstrapIssuerNames` | UT-AUDIT-515-SERIALIZATION-001; UT-AUDIT-515-CERTMANAGER-008; IT-TLS-AUDIT-515-004 |

RED observed before production changes: three independent fake-API activation
specs failed, two real-API activation specs failed, two Service drift specs
failed, and the actual manager's explicit-webhook-server assertion failed.
The independent unit invocation started no envtest control plane.

Issuer selection regression: automatic namespaced selection worked only before
the first provisioning pass. Subsequent passes counted operator-created bootstrap
Issuers as external candidates and blocked activation. RED was observed in both
independent unit and envtest lifecycle tests. The fix excludes only generated
Issuer identities with an exact current-CR controller reference (API version,
kind, name and nonempty UID). Matching names/labels or previous-UID references
are insufficient; multiple external candidates still fail closed. Explicit
issuer references are unchanged. This does not replace #517's broader resource
ownership/adoption/deletion protections.

Controls remain engineering mappings, not certification: activation AC-3/CM-5/
SI-10 and ASVS 5.0 V2.3.2/V15.4.2; protocol CM-6/SC-5/SI-2; convergence CM-2/
CM-6/SI-4/SI-7; verification SA-11/CA-2/CA-7. Exact source/version references
and applicability limits remain in `docs/security/` and the independent audit.

Current snapshot-bound verification, exact coverage scope/exclusions, mutation
provenance and outstanding dependency/qualification gates are recorded in
`docs/security/PR-515-REMEDIATION-EVIDENCE.md`. No local gate is an assertion of
formal compliance or full application E2E readiness.

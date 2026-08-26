# v1alpha2 CRD Field Coverage Audit

Tracking issue: #421

> **Snapshot as of 2026-08-26.** This report is a point-in-time audit
> artifact, not a live tracker -- every finding below was split out into its
> own GitHub issue for day-to-day work, and those issues (not this file) are
> expected to become stale-free as gaps are closed:
>
> - #422 -- `networkPolicies.*` (24 fields) never wired into rendering
> - #423 -- 7 cross-consumer consistency gaps (#417-class, lower severity)
> - #424 -- `monitoring.*.tlsCaFile` no production consumer
> - #425-#441 -- per-component test-coverage backlog (the remaining 103
>   non-`COVERED` fields, one issue per component)
>
> Keep this file for its methodology, calibration results, and the full
> field-by-field inventory (including the 154 `COVERED` fields, which the
> issues above don't list) -- but check the linked issues, not this file's
> checklists, for current status.

## Scope and methodology

- **Scope**: `v1alpha2` only -- 405 exported struct fields in [api/v1alpha2/kubernaut_types.go](../../../api/v1alpha2/kubernaut_types.go) (290 leaf/scalar fields + 115 nested-object fields; this report's per-field detail focuses on the 290 leaves, since that's where test assertions actually land).
- **Coverage definition ("full behavioral")**, per field: (1) structural -- a test sets/reads it and asserts an effect on rendered output; (2) cross-consumer consistency -- every production consumer file has sibling test coverage, the exact bug class caught in #417; (3) default-value test for fields with `+kubebuilder:default`; (4) validation-error test for fields with a rule in `internal/resources/validation.go`; (5) control-tag traceability (`[AC-x]`/`[SC-x]`/`[IA-x]`/`[AU-x]`/`[CM-x]`/`[SI-x]`) for security/compliance-relevant fields.
- **Tooling** (methodology refinement from the original plan, disclosed here for transparency): rather than flattening the *generated* CRD YAML and separately mapping JSON names back to Go identifiers, the field inventory was extracted directly from the Go AST of `kubernaut_types.go` (same kubebuilder markers, descriptions, and defaults -- just parsed at the source instead of the post-`controller-gen` YAML -- and it hands back native Go field/struct names for free, which the usage-indexing step needs anyway).
- **Usage indexing**: initial textual grep hit a hard wall -- 147 of 290 leaf fields (51%) share a Go field name with at least one other field elsewhere in the CRD (`Enabled`, `SecretName`, `LLMProfileRef`, `Host`, `Port`, ...), so a bare-name grep cannot attribute a hit to the correct field (verified concretely: `alignmentCheck.llmProfileRef` and `kubernautAgent.llmProfileRef` produced byte-identical grep results despite #419 already having established they have completely different consumers). This was replaced with a `go/packages` + `go/types` type-checker pass (see scratch tool, not committed) that resolves every struct-field read (`ka.LLMProfileRef`) and composite-literal write (`KubernautAgentSpec{LLMProfileRef: ...}`) to its exact declaring (package, struct type, field name) -- eliminating the name-collision ambiguity entirely. It also cross-references each v1alpha2 field against a same-named field in `api/v1alpha1` (most fields' actual production reads happen against the converted `kn` v1alpha1 view, not the raw `knV2` v1alpha2 one).
- **Consistency check scope**: restricted to the three hand-written business-logic packages (`internal/resources`, `internal/controller`, `internal/webhook`). `api/v1alpha2` has zero `_test.go` files by design (pure hub/storage type package) and `api/v1alpha1`'s conversion round-trip tests frequently assert whole-struct equality rather than per-field keyed literals -- both produced unusable false-positive rates in calibration (98 and 55 spurious flags respectively) and were excluded after inspection.
- **Default-value tests** are flagged separately as **needs manual verification** (a non-gating, informational dimension) rather than automatically pass/failed: statically proving "some test omits this field AND asserts the default value renders" requires semantic reasoning (distinguishing omission-fixtures from override-fixtures) that a textual/AST heuristic cannot do reliably in the time available.

## Calibration

Per the plan, ~15 flagged fields were manually re-verified by reading the actual source (not just trusting the tool):

- `kubernautAgent.alignmentCheck.llmProfileRef` and `kubernautAgent.llmProfileRef` -- reproduced the exact #419/#417 ground truth (the former is read only inside the conversion bridge with zero downstream consumers; the latter is fully covered) confirming the type-checked matching is precise where grep wasn't.
- `remediationOrchestrator.timeouts.analyzing` (flagged UNTESTED) -- confirmed by direct read of `configmaps.go`/`configmaps_test.go`: the field is consumed (`withDefault(ro.Timeouts.Analyzing, "10m")`) but genuinely has zero test references anywhere.
- `networkPolicies.apiServerCIDR`/`.apiServerCIDRs` (flagged as having **no production consumer**) -- confirmed by direct read of `networkpolicies.go`: `NetworkPolicies()` reads `kn.Spec.NetworkPolicies` (the **v1alpha1** struct), which does not have these fields at all -- they were added in v1alpha2 only. This pattern was checked across all 24 `networkPolicies.*` leaf fields via the same method (grepping every `knV2.Spec.NetworkPolicies*`/`knV2.Spec.Monitoring.*` call site) and confirmed to hold for the whole subtree -- see priority finding #1 below.
- `monitoring.prometheus.tlsCaFile` (flagged as having no production consumer) -- confirmed by direct read: `configmaps.go` has several *different*, unrelated `TLSCaFile`-named fields on other structs (correctly not matched, proving the type-checked matcher doesn't false-positive on name collisions), and no code path reads `Monitoring.Prometheus.TLSCaFile`/`Monitoring.AlertManager.TLSCaFile` specifically.
- Fields flagged with a `VALIDATION` gap: **zero** -- every field with a rule in `validation.go` does have `validation_test.go` coverage per this pass; spot-checked `kubernautAgent.llmProfileRef` and `apiFrontend.severityTriage.llmProfileRef` directly.
- Several `COVERED` + security-relevant fields (`valkey.tls.*`, `*.fleet.oauth2CredentialsSecretRef`) were spot-checked and their reported control-tag samples (`[SC-8]`, `[CM-6]`, `[AC-6]`) were confirmed to be real, on-point `It(...)` descriptions, not heuristic false matches.

**Calibration verdict**: the type-checked usage index is trustworthy at the individual-field level (no false positives found in ~15 manual checks). The weakest dimension is `CONSISTENCY` at file-level granularity, which is why it was narrowed to package-level and restricted to business-logic packages -- it still cannot detect a #417-class bug precisely at *function* granularity (two functions in the same already-tested file), which remains a fundamentally manual-review concern no static heuristic here resolves.

## Overall summary (290 leaf fields)

- **COVERED**: 154 (53%)
- **PARTIAL**: 65 (22%)
- **UNTESTED**: 71 (24%)

- Security/compliance-relevant fields: 119 total -- 32 COVERED, 55 PARTIAL, 32 UNTESTED.
- Fields with `+kubebuilder:default` needing a manual default-rendering check: 139.

## Priority findings (read this section first)

1. **`spec.networkPolicies.*` -- 24 fields, 100% dead on arrival.** Every tuning field under `NetworkPoliciesSpec` (`apiServerCIDR(s)`, `apiServerPort`, per-destination `cidr`/`port` overrides for monitoring/externalWebhooks/externalRegistry/llm/mcpGateway/prometheus, and per-component `ingressCIDRs`/`ingressNamespaces`/`ingressNamespaceSelectors` overrides for gateway/apifrontend/console/datastorage/kubernautAgent) is new in v1alpha2 and accepted by the CRD schema, but `internal/resources/networkpolicies.go`'s builder functions read `kn.Spec.NetworkPolicies` -- the **v1alpha1** struct, which has none of these fields -- and never read the corresponding `knV2` fields either (confirmed: `knV2` is used in that file only for Fleet/Monitoring-URL egress, never for any `NetworkPolicies.*` field). A cluster-admin setting any of these 24 fields in a v1alpha2 CR gets silent no-op behavior: no validation error, no effect on the rendered NetworkPolicy. This is a strictly worse variant of the #417 bug class (not "wrong inference," but "never wired at all") and is security-relevant (these are meant to tighten network isolation). **Recommend filing immediately as its own issue, independent of this audit's broader test-coverage gaps.**
2. **`kubernautAgent.alignmentCheck.llmProfileRef` -- already tracked as #419.** Confirmed again here as `CONVERSION-ONLY` with zero downstream consumers.
3. **Cross-consumer consistency gaps (7 fields, high-confidence, business-package-scoped):** `image.pullSecrets`, `postgresql.sslMode`, `notification.routing.configMapName`, `signalProcessing.proactiveSignalMappings.configMapName`, `gateway.resources`, `apiFrontend.rbacRolesConfigMapRef.configMapName`, `additionalClusterRoles` -- each has a production consumer in one package (mostly `internal/controller/kubernaut_controller.go`) with zero test coverage in that same package, even though the field is tested elsewhere (`internal/resources`). Same shape as #417's gap, lower severity since the field IS correctly wired, just untested at that specific call site.
4. **`monitoring.prometheus.tlsCaFile` / `monitoring.alertManager.tlsCaFile` -- no production consumer found.** Distinct from finding #1 (different subtree); worth a quick look alongside it since both are TLS/monitoring-egress-relevant.
5. **32 security/compliance-relevant fields are entirely UNTESTED** (see the per-component detail below for the full list) -- these are the highest-value targets for new regression tests given FedRAMP control traceability requirements.

## Component: `apiFrontend` (45 fields)

Summary: COVERED=26 | PARTIAL=12 | UNTESTED=7

- `spec.apiFrontend.healthPort` (*int32) -- **UNTESTED**
  - zero test references found
- `spec.apiFrontend.metricsPort` (*int32) -- **UNTESTED**
  - zero test references found
- `spec.apiFrontend.rateLimit.ipRequestsPerSec` (*int) -- **UNTESTED** **[security-relevant]**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=10000`
- `spec.apiFrontend.rateLimit.maxConcurrentSessions` (*int) -- **UNTESTED** **[security-relevant]**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=50`
- `spec.apiFrontend.rateLimit.toolCallsPerMinute` (*int) -- **UNTESTED** **[security-relevant]**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=600`
- `spec.apiFrontend.rateLimit.userRequestsPerSec` (*int) -- **UNTESTED** **[security-relevant]**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=100`
- `spec.apiFrontend.resources` (corev1.ResourceRequirements) -- **UNTESTED**
  - zero test references found
- `spec.apiFrontend.auth.allowInsecureIssuers` (bool) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.apiFrontend.auth.audience` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default="kubernaut-apifrontend"`
- `spec.apiFrontend.auth.jwksURL` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.apiFrontend.auth.jwtProviders.audiences` ([]string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.apiFrontend.auth.jwtProviders.issuerURL` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.apiFrontend.auth.jwtProviders.jwksURL` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.apiFrontend.auth.oidcCaFile` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.apiFrontend.enabled` (*bool) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=true`
- `spec.apiFrontend.rbac.consoleAccessAuthorizationCheckEnabled` (*bool) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=false`
- `spec.apiFrontend.rbac.consoleAccessGroups` ([]string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.apiFrontend.rbac.sarCacheTTL` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default="30s"`
- `spec.apiFrontend.rbacRolesConfigMapRef.configMapName` (string) -- **PARTIAL** **[security-relevant]**
  - CONSISTENCY: production file(s) with no sibling test reference: ['internal/controller']
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `kubernautAgent` (37 fields)

Summary: COVERED=13 | PARTIAL=15 | UNTESTED=9

- `spec.kubernautAgent.interactive.maxConcurrentSessions` (*int) -- **UNTESTED**
  - zero test references found
- `spec.kubernautAgent.interactive.rateLimitPerUser` (*int) -- **UNTESTED** **[security-relevant]**
  - zero test references found
- `spec.kubernautAgent.maxTurns` (int) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=40`
- `spec.kubernautAgent.resources` (corev1.ResourceRequirements) -- **UNTESTED**
  - zero test references found
- `spec.kubernautAgent.safety.anomaly.maxRepeatedFailures` (*int) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=3`
- `spec.kubernautAgent.safety.anomaly.maxTotalToolCalls` (*int) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=40`
- `spec.kubernautAgent.safety.sanitization.credentialScrubEnabled` (*bool) -- **UNTESTED** **[security-relevant]**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=true`
- `spec.kubernautAgent.safety.sanitization.injectionPatternsEnabled` (*bool) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=true`
- `spec.kubernautAgent.session.ttl` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="30m"`
- `spec.kubernautAgent.alignmentCheck.llmProfileRef` (string) -- **PARTIAL** **[security-relevant]**
  - CONVERSION-ONLY: only read inside the v1<->v2 conversion bridge -- verify the value actually reaches a resource builder downstream
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.kubernautAgent.alignmentCheck.maxStepTokens` (int) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=500`
- `spec.kubernautAgent.audit.batchSize` (*int) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=50`
- `spec.kubernautAgent.audit.bufferSize` (*int) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=10000`
- `spec.kubernautAgent.audit.enabled` (*bool) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=true`
- `spec.kubernautAgent.audit.flushIntervalSeconds` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default="1"`
- `spec.kubernautAgent.serverRateLimit.burst` (*int) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=100`
- `spec.kubernautAgent.serverRateLimit.requestsPerSecond` (*int) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=50`
- `spec.kubernautAgent.summarizer.threshold` (int) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=8000`
- `spec.kubernautAgent.telemetry.endpoint` (string) -- **PARTIAL**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
- `spec.kubernautAgent.telemetry.logSink` (*bool) -- **PARTIAL**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - DEFAULT (manual check needed): `+kubebuilder:default=false`
- `spec.kubernautAgent.telemetry.tls.caFile` (string) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.kubernautAgent.telemetry.tls.certFile` (string) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.kubernautAgent.telemetry.tls.enabled` (*bool) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=false`
- `spec.kubernautAgent.telemetry.tls.keyFile` (string) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `remediationOrchestrator` (29 fields)

Summary: COVERED=15 | PARTIAL=0 | UNTESTED=14

- `spec.remediationOrchestrator.asyncPropagation.gitOpsSyncDelay` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="3m"`
- `spec.remediationOrchestrator.asyncPropagation.operatorReconcileDelay` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="1m"`
- `spec.remediationOrchestrator.asyncPropagation.proactiveAlertDelay` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="5m"`
- `spec.remediationOrchestrator.effectivenessAssessment.stabilizationWindow` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="5m"`
- `spec.remediationOrchestrator.resources` (corev1.ResourceRequirements) -- **UNTESTED**
  - zero test references found
- `spec.remediationOrchestrator.routing.consecutiveFailureCooldown` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="1h"`
- `spec.remediationOrchestrator.routing.consecutiveFailureThreshold` (*int) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=3`
- `spec.remediationOrchestrator.routing.ineffectiveChainThreshold` (*int) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=3`
- `spec.remediationOrchestrator.routing.ineffectiveTimeWindow` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="4h"`
- `spec.remediationOrchestrator.routing.recentlyRemediatedCooldown` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="5m"`
- `spec.remediationOrchestrator.routing.recurrenceCountThreshold` (*int) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=5`
- `spec.remediationOrchestrator.timeouts.analyzing` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="10m"`
- `spec.remediationOrchestrator.timeouts.executing` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="30m"`
- `spec.remediationOrchestrator.timeouts.verifying` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="30m"`

## Component: `gateway` (27 fields)

Summary: COVERED=17 | PARTIAL=9 | UNTESTED=1

- `spec.gateway.config.cors.allowedMethods` ([]string) -- **UNTESTED**
  - zero test references found
- `spec.gateway.config.cors.allowCredentials` (*bool) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=false`
- `spec.gateway.config.telemetry.endpoint` (string) -- **PARTIAL**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
- `spec.gateway.config.telemetry.logSink` (*bool) -- **PARTIAL**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - DEFAULT (manual check needed): `+kubebuilder:default=false`
- `spec.gateway.config.telemetry.tls.caFile` (string) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.gateway.config.telemetry.tls.certFile` (string) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.gateway.config.telemetry.tls.enabled` (*bool) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=false`
- `spec.gateway.config.telemetry.tls.keyFile` (string) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.gateway.enabled` (*bool) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=true`
- `spec.gateway.resources` (corev1.ResourceRequirements) -- **PARTIAL**
  - CONSISTENCY: production file(s) with no sibling test reference: ['internal/resources']

## Component: `networkPolicies` (24 fields)

Summary: COVERED=0 | PARTIAL=0 | UNTESTED=24

- `spec.networkPolicies.apiServerCIDR` (string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.apiServerCIDRs` ([]string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.apiServerPort` (int32) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.apifrontend.ingressNamespaces` ([]string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.console.ingressNamespaces` ([]string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.datastorage.ingressCIDRs` ([]string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.datastorage.ingressNamespaceSelectors` ([]metav1.LabelSelector) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.externalRegistry.cidr` (string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
  - DEFAULT (manual check needed): `+kubebuilder:default="0.0.0.0/0"`
- `spec.networkPolicies.externalRegistry.port` (int32) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.externalWebhooks.cidr` (string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
  - DEFAULT (manual check needed): `+kubebuilder:default="0.0.0.0/0"`
- `spec.networkPolicies.externalWebhooks.port` (int32) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.gateway.ingressNamespaces` ([]string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.idp.extraPorts` ([]int32) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.kubernautAgent.ingressCIDRs` ([]string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.kubernautAgent.ingressNamespaceSelectors` ([]metav1.LabelSelector) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.llm.cidr` (string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
  - DEFAULT (manual check needed): `+kubebuilder:default="0.0.0.0/0"`
- `spec.networkPolicies.llm.port` (int32) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.mcpGateway.cidr` (string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
  - DEFAULT (manual check needed): `+kubebuilder:default="0.0.0.0/0"`
- `spec.networkPolicies.mcpGateway.port` (int32) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.monitoring.alertManagerPort` (int32) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
  - DEFAULT (manual check needed): `+kubebuilder:default=9093`
- `spec.networkPolicies.monitoring.namespace` (string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.networkPolicies.monitoring.prometheusPort` (int32) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
  - DEFAULT (manual check needed): `+kubebuilder:default=9090`
- `spec.networkPolicies.prometheus.cidr` (string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
  - DEFAULT (manual check needed): `+kubebuilder:default="0.0.0.0/0"`
- `spec.networkPolicies.prometheus.port` (int32) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert

## Component: `llmProfiles` (23 fields)

Summary: COVERED=16 | PARTIAL=4 | UNTESTED=3

- `spec.llmProfiles.azureApiVersion` (string) -- **UNTESTED**
  - zero test references found
- `spec.llmProfiles.bedrockRegion` (string) -- **UNTESTED**
  - zero test references found
- `spec.llmProfiles.timeoutSeconds` (*int) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default=120`
- `spec.llmProfiles.tlsCaFile` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.llmProfiles.tlsCertFile` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.llmProfiles.tlsClientSecretRef` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.llmProfiles.tlsKeyFile` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `dataStorage` (21 fields)

Summary: COVERED=10 | PARTIAL=9 | UNTESTED=2

- `spec.dataStorage.endpointPropagationDelay` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="10s"`
- `spec.dataStorage.resources` (corev1.ResourceRequirements) -- **UNTESTED**
  - zero test references found
- `spec.dataStorage.retention.defaultDays` (*int) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=2555`
- `spec.dataStorage.signingCert.mountPath` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default="/etc/certs"`
- `spec.dataStorage.signingCert.secretName` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.dataStorage.telemetry.endpoint` (string) -- **PARTIAL**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
- `spec.dataStorage.telemetry.logSink` (*bool) -- **PARTIAL**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - DEFAULT (manual check needed): `+kubebuilder:default=false`
- `spec.dataStorage.telemetry.tls.caFile` (string) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.dataStorage.telemetry.tls.certFile` (string) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.dataStorage.telemetry.tls.enabled` (*bool) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default=false`
- `spec.dataStorage.telemetry.tls.keyFile` (string) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `fleet` (18 fields)

Summary: COVERED=16 | PARTIAL=2 | UNTESTED=0

- `spec.fleet.caSecretName` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.fleet.tokenSecretName` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `workflowExecution` (13 fields)

Summary: COVERED=7 | PARTIAL=4 | UNTESTED=2

- `spec.workflowExecution.cooldownPeriod` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="1m"`
- `spec.workflowExecution.resources` (corev1.ResourceRequirements) -- **UNTESTED**
  - zero test references found
- `spec.workflowExecution.ansible.caCertSecretRef.key` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default="ca.crt"`
- `spec.workflowExecution.ansible.caCertSecretRef.name` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.workflowExecution.ansible.tokenSecretRef.key` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default="token"`
- `spec.workflowExecution.ansible.tokenSecretRef.name` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `valkey` (6 fields)

Summary: COVERED=5 | PARTIAL=1 | UNTESTED=0

- `spec.valkey.secretName` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `monitoring` (6 fields)

Summary: COVERED=4 | PARTIAL=0 | UNTESTED=2

- `spec.monitoring.alertManager.tlsCaFile` (string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert
- `spec.monitoring.prometheus.tlsCaFile` (string) -- **UNTESTED** **[security-relevant]**
  - no production consumer either -- field is entirely inert

## Component: `fleetMetadataCache` (6 fields)

Summary: COVERED=6 | PARTIAL=0 | UNTESTED=0

_All fields in this component are COVERED._

## Component: `notification` (5 fields)

Summary: COVERED=2 | PARTIAL=2 | UNTESTED=1

- `spec.notification.resources` (corev1.ResourceRequirements) -- **UNTESTED**
  - zero test references found
- `spec.notification.routing.configMapName` (string) -- **PARTIAL**
  - CONSISTENCY: production file(s) with no sibling test reference: ['internal/controller']
- `spec.notification.slack.secretName` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `signalProcessing` (5 fields)

Summary: COVERED=3 | PARTIAL=1 | UNTESTED=1

- `spec.signalProcessing.resources` (corev1.ResourceRequirements) -- **UNTESTED**
  - zero test references found
- `spec.signalProcessing.proactiveSignalMappings.configMapName` (string) -- **PARTIAL**
  - CONSISTENCY: production file(s) with no sibling test reference: ['internal/controller']

## Component: `effectivenessMonitor` (5 fields)

Summary: COVERED=2 | PARTIAL=0 | UNTESTED=3

- `spec.effectivenessMonitor.assessment.stabilizationWindow` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="30s"`
- `spec.effectivenessMonitor.assessment.validityWindow` (string) -- **UNTESTED**
  - zero test references found
  - DEFAULT (manual check needed): `+kubebuilder:default="300s"`
- `spec.effectivenessMonitor.resources` (corev1.ResourceRequirements) -- **UNTESTED**
  - zero test references found

## Component: `console` (5 fields)

Summary: COVERED=4 | PARTIAL=1 | UNTESTED=0

- `spec.console.auth.secretName` (string) -- **PARTIAL** **[security-relevant]**
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `postgresql` (4 fields)

Summary: COVERED=2 | PARTIAL=2 | UNTESTED=0

- `spec.postgresql.secretName` (string) -- **PARTIAL** **[security-relevant]**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
- `spec.postgresql.sslMode` (string) -- **PARTIAL** **[security-relevant]**
  - CONSISTENCY: production file(s) with no sibling test reference: ['internal/controller']
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag
  - DEFAULT (manual check needed): `+kubebuilder:default="verify-full"`

## Component: `aiAnalysis` (4 fields)

Summary: COVERED=3 | PARTIAL=0 | UNTESTED=1

- `spec.aiAnalysis.resources` (corev1.ResourceRequirements) -- **UNTESTED**
  - zero test references found

## Component: `image` (3 fields)

Summary: COVERED=1 | PARTIAL=2 | UNTESTED=0

- `spec.image.pullPolicy` (corev1.PullPolicy) -- **PARTIAL**
  - STRUCTURAL: field is referenced by a test but no assertion was found nearby (may just be constructing a fixture)
  - DEFAULT (manual check needed): `+kubebuilder:default="IfNotPresent"`
- `spec.image.pullSecrets` ([]corev1.LocalObjectReference) -- **PARTIAL** **[security-relevant]**
  - CONSISTENCY: production file(s) with no sibling test reference: ['internal/resources']
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `authWebhook` (2 fields)

Summary: COVERED=1 | PARTIAL=0 | UNTESTED=1

- `spec.authWebhook.resources` (corev1.ResourceRequirements) -- **UNTESTED**
  - zero test references found

## Component: `additionalClusterRoles` (1 fields)

Summary: COVERED=0 | PARTIAL=1 | UNTESTED=0

- `spec.additionalClusterRoles` ([]string) -- **PARTIAL** **[security-relevant]**
  - CONSISTENCY: production file(s) with no sibling test reference: ['internal/controller']
  - CONTROL-TAG: security/compliance-relevant field with no test carrying an [AC-x]/[SC-x]/[IA-x]/[AU-x]/[CM-x]/[SI-x] tag

## Component: `debug` (1 fields)

Summary: COVERED=1 | PARTIAL=0 | UNTESTED=0

_All fields in this component are COVERED._

## Fields needing a manual default-rendering check

These fields have a `+kubebuilder:default` and were not part of the automated pass/fail dimensions above (see methodology) -- listed here as a checklist, grouped by component, independent of their overall status:

- **aiAnalysis**: `aiAnalysis.logging.level`="INFO"
- **apiFrontend**: `apiFrontend.enabled`=true, `apiFrontend.route.enabled`=false, `apiFrontend.auth.audience`="kubernaut-apifrontend", `apiFrontend.rateLimit.ipRequestsPerSec`=10000, `apiFrontend.rateLimit.userRequestsPerSec`=100, `apiFrontend.rateLimit.maxConcurrentSessions`=50, `apiFrontend.rateLimit.toolCallsPerMinute`=600, `apiFrontend.severityTriage.llmEnabled`=true, `apiFrontend.severityTriage.cacheTTLSeconds`=30, `apiFrontend.severityTriage.llmConfidence`="0.7", `apiFrontend.rbac.sarCacheTTL`="30s", `apiFrontend.rbac.consoleAccessAuthorizationCheckEnabled`=false, `apiFrontend.logging.level`="INFO", `apiFrontend.session.disconnectTTL`="10m", `apiFrontend.session.retentionTTL`="720h", `apiFrontend.mcp.sessionIdleTimeout`="30m", `apiFrontend.mcp.toolTimeout`="30s", `apiFrontend.mcp.toolTimeouts`={"kubernaut_investigate":"15m","kubernaut_await_session":"3m","kubernaut_watch":"15m","kubernaut_discover_workflows":"60s"}
- **authWebhook**: `authWebhook.logging.level`="INFO"
- **console**: `console.enabled`=false, `console.route.enabled`=true
- **dataStorage**: `dataStorage.endpointPropagationDelay`="10s", `dataStorage.logging.level`="INFO", `dataStorage.retention.interval`="24h", `dataStorage.retention.batchSize`=1000, `dataStorage.retention.defaultDays`=2555, `dataStorage.signingCert.mountPath`="/etc/certs", `dataStorage.telemetry.logSink`=false, `dataStorage.telemetry.tls.enabled`=false, `dataStorage.database.maxOpenConns`=100, `dataStorage.database.maxIdleConns`=20, `dataStorage.database.connMaxLifetime`="1h", `dataStorage.database.connMaxIdleTime`="10m", `dataStorage.server.readTimeout`="30s", `dataStorage.server.writeTimeout`="30s"
- **debug**: `debug.pprofEnabled`=false
- **effectivenessMonitor**: `effectivenessMonitor.assessment.stabilizationWindow`="30s", `effectivenessMonitor.assessment.validityWindow`="300s", `effectivenessMonitor.logging.level`="INFO"
- **fleet**: `fleet.enabled`=false, `fleet.oauth2.enabled`=false
- **fleetMetadataCache**: `fleetMetadataCache.enabled`=false, `fleetMetadataCache.syncInterval`="30s", `fleetMetadataCache.keyTTL`="45s", `fleetMetadataCache.logging.level`="INFO"
- **gateway**: `gateway.enabled`=true, `gateway.route.enabled`=true, `gateway.config.k8sRequestTimeout`="15s", `gateway.config.cors.allowCredentials`=false, `gateway.config.cors.maxAge`=300, `gateway.config.deduplicationCooldown`="5m", `gateway.config.telemetry.logSink`=false, `gateway.config.telemetry.tls.enabled`=false, `gateway.config.server.maxConcurrentRequests`=100, `gateway.config.server.readTimeout`="3600s", `gateway.config.server.writeTimeout`="3600s", `gateway.config.server.idleTimeout`="120s", `gateway.config.retry.maxAttempts`=3, `gateway.config.retry.initialBackoff`="100ms", `gateway.config.retry.maxBackoff`="5s", `gateway.logging.level`="INFO"
- **image**: `image.pullPolicy`="IfNotPresent"
- **kubernautAgent**: `kubernautAgent.maxTurns`=40, `kubernautAgent.session.ttl`="30m", `kubernautAgent.audit.enabled`=true, `kubernautAgent.audit.flushIntervalSeconds`="1", `kubernautAgent.audit.bufferSize`=10000, `kubernautAgent.audit.batchSize`=50, `kubernautAgent.alignmentCheck.enabled`=false, `kubernautAgent.alignmentCheck.timeout`="10s", `kubernautAgent.alignmentCheck.maxStepTokens`=500, `kubernautAgent.summarizer.threshold`=8000, `kubernautAgent.summarizer.maxToolOutputSize`=100000, `kubernautAgent.telemetry.logSink`=false, `kubernautAgent.telemetry.tls.enabled`=false, `kubernautAgent.safety.sanitization.injectionPatternsEnabled`=true, `kubernautAgent.safety.sanitization.credentialScrubEnabled`=true, `kubernautAgent.safety.anomaly.maxToolCallsPerTool`=10, `kubernautAgent.safety.anomaly.maxTotalToolCalls`=40, `kubernautAgent.safety.anomaly.maxRepeatedFailures`=3, `kubernautAgent.serverRateLimit.requestsPerSecond`=50, `kubernautAgent.serverRateLimit.burst`=100, `kubernautAgent.shutdown.drainSeconds`=15, `kubernautAgent.logging.level`="INFO"
- **llmProfiles**: `llmProfiles.maxRetries`=3, `llmProfiles.timeoutSeconds`=120, `llmProfiles.oauth2.enabled`=false, `llmProfiles.reasoning.enabled`=false, `llmProfiles.reasoning.capabilityOverride`=auto
- **monitoring**: `monitoring.prometheus.enabled`=true, `monitoring.alertManager.enabled`=true
- **networkPolicies**: `networkPolicies.monitoring.prometheusPort`=9090, `networkPolicies.monitoring.alertManagerPort`=9093, `networkPolicies.externalWebhooks.cidr`="0.0.0.0/0", `networkPolicies.externalRegistry.cidr`="0.0.0.0/0", `networkPolicies.llm.cidr`="0.0.0.0/0", `networkPolicies.mcpGateway.cidr`="0.0.0.0/0", `networkPolicies.prometheus.cidr`="0.0.0.0/0"
- **notification**: `notification.slack.channel`="#kubernaut-alerts", `notification.logging.level`="INFO"
- **postgresql**: `postgresql.port`=5432, `postgresql.sslMode`="verify-full"
- **remediationOrchestrator**: `remediationOrchestrator.timeouts.global`="1h", `remediationOrchestrator.timeouts.processing`="5m", `remediationOrchestrator.timeouts.analyzing`="10m", `remediationOrchestrator.timeouts.executing`="30m", `remediationOrchestrator.timeouts.awaitingApproval`="15m", `remediationOrchestrator.timeouts.verifying`="30m", `remediationOrchestrator.routing.consecutiveFailureThreshold`=3, `remediationOrchestrator.routing.consecutiveFailureCooldown`="1h", `remediationOrchestrator.routing.recentlyRemediatedCooldown`="5m", `remediationOrchestrator.routing.exponentialBackoffBase`="1m", `remediationOrchestrator.routing.exponentialBackoffMax`="10m", `remediationOrchestrator.routing.exponentialBackoffMaxExponent`=4, `remediationOrchestrator.routing.scopeBackoffBase`="5s", `remediationOrchestrator.routing.scopeBackoffMax`="5m", `remediationOrchestrator.routing.noActionRequiredDelayHours`=24, `remediationOrchestrator.routing.ineffectiveChainThreshold`=3, `remediationOrchestrator.routing.recurrenceCountThreshold`=5, `remediationOrchestrator.routing.ineffectiveTimeWindow`="4h", `remediationOrchestrator.effectivenessAssessment.stabilizationWindow`="5m", `remediationOrchestrator.asyncPropagation.gitOpsSyncDelay`="3m", `remediationOrchestrator.asyncPropagation.operatorReconcileDelay`="1m", `remediationOrchestrator.asyncPropagation.proactiveAlertDelay`="5m", `remediationOrchestrator.dryRun`=false, `remediationOrchestrator.dryRunHoldPeriod`="1h", `remediationOrchestrator.notifications.notifySelfResolved`=false, `remediationOrchestrator.retention.period`="24h", `remediationOrchestrator.logging.level`="INFO"
- **signalProcessing**: `signalProcessing.logging.level`="INFO"
- **valkey**: `valkey.port`=6379
- **workflowExecution**: `workflowExecution.workflowNamespace`="kubernaut-workflows", `workflowExecution.cooldownPeriod`="1m", `workflowExecution.ansible.enabled`=false, `workflowExecution.ansible.organizationID`=1, `workflowExecution.ansible.tokenSecretRef.key`="token", `workflowExecution.ansible.caCertSecretRef.key`="ca.crt", `workflowExecution.logging.level`="INFO"

## Known limitations

- Grep/AST-based matching is name- and structure-based, not semantic -- it cannot tell whether a test's assertion is *meaningful* (e.g. it can't detect a test that sets a field and asserts on an unrelated part of the output).
- The `CONSISTENCY` dimension is package-level, not function-level -- it would not have caught #417 on its own (two specific functions in an already-partially-tested file); it only catches the coarser case of a whole package with a production consumer and zero test presence.
- `DEFAULT` and the finer points of `CONTROL-TAG` proximity (nearest preceding `It(...)` within 200 lines) are heuristics tuned during calibration, not proven exhaustively across all 290 fields.
- The 115 nested-object (non-leaf) fields -- especially `*Spec` pointer fields like `kubernautAgent.interactive`, `alignmentCheck` -- are not individually scored here for "is nil-vs-non-nil behavior tested," only their leaf children are. A follow-up pass could specifically target pointer-typed optional blocks.

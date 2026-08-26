# IEEE 829 Test Plan — Issues #422-#441: Close v1alpha2 CRD field-coverage gaps

| Field              | Value                                              |
|--------------------|-----------------------------------------------------|
| **Test Plan ID**   | TP-421                                             |
| **Issues**         | #422 (`networkPolicies.*` dead fields), #423 (cross-consumer consistency), #424 (`monitoring.*.tlsCaFile` dead fields), #425-#441 (per-component test-coverage backlog) |
| **Source audit**   | [docs/tests/421/CRD_FIELD_COVERAGE_AUDIT.md](CRD_FIELD_COVERAGE_AUDIT.md) (#421) |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-08-26                                         |
| **Scope**          | `api/v1alpha2/kubernaut_types.go`, `internal/resources/networkpolicies.go`, `internal/resources/configmaps.go`, `internal/resources/deployments.go`, `internal/controller/kubernaut_controller.go` |

## 1. Objective

The #421 v1alpha2 CRD field-coverage audit found 136 of 290 leaf fields (47%)
in `PARTIAL` or `UNTESTED` status, split into 20 tracking issues. This PR
closes all 20 in one combined change set, organized into four workstreams:

1. **#422 -- `networkPolicies.*` wiring (21 of 24 fields).** Every tuning
   field under `NetworkPoliciesSpec` is accepted by the v1alpha2 CRD schema
   but `internal/resources/networkpolicies.go` never reads the `knV2`
   equivalent -- setting any of them today is a silent no-op with no
   validation error. This is a security regression (these fields exist to
   tighten network isolation). 21 of the 24 fields get wired into their
   builder functions; the remaining 3 (`console.ingressNamespaces`,
   `externalRegistry.cidr`/`.port`) are carved out to follow-up issues (see
   §1.1).
2. **#424 -- `monitoring.*.tlsCaFile` wiring (2 fields).** Confirmed via
   source read, subagent type-checked search, and git archaeology on the
   historical `#298` fix (`111e983`) that `Monitoring.Prometheus.TLSCaFile`/
   `Monitoring.AlertManager.TLSCaFile` have zero production consumers --
   EM/KA/AF all hardcode per-component `service-ca.crt` paths regardless of
   these fields. Wire them in as overrides of the hardcoded defaults.
3. **#423 -- cross-consumer consistency gaps (7 fields).** Each field is
   correctly wired in one package but has zero test coverage in the package
   that actually consumes it (`internal/controller`, mostly) -- the same bug
   *class* as the live #417 incident, lower severity since these are
   correctly wired today. 5 confirmed as stated, 1 reinterpreted, 1 is an
   audit false positive (see §1.2).
4. **#425-#441 -- per-component test-coverage backfill (~103 fields).**
   `PARTIAL`/`UNTESTED` fields with a correct production consumer but no
   (or incomplete) test coverage: `corev1.ResourceRequirements` passthroughs,
   config-rendering fields, Secret/volume-mount fields, and single-field
   gaps (control tags, default-value checks, consistency).

**Explicitly out of scope**: no CRD schema redesign; no Helm-side changes;
the 2 carved-out `networkPolicies.*` fields (tracked as new follow-up
issues, not implemented here); `kubernautAgent.alignmentCheck.llmProfileRef`
(tracked separately as #419, `CONVERSION-ONLY` by design).

### 1.1 #422 scope decision: 21 of 24 fields, 2 follow-up issues filed

| Carved out | Reason | Disposition |
|---|---|---|
| `networkPolicies.console.ingressNamespaces` | No `consoleNetworkPolicy()` builder exists at all today -- Console gets zero NetworkPolicy regardless of this field. Wiring it requires designing a new builder, not overriding an existing one. | New follow-up issue: "Add a Console NetworkPolicy builder" |
| `networkPolicies.externalRegistry.{cidr,port}` | Image-pull happens at kubelet/node level, outside any pod's NetworkPolicy scope. No runtime hook exists in this operator to express a per-pod egress rule for the container-image registry. | New follow-up issue: "Determine ExternalRegistry NetworkPolicy target (kubelet-level enforcement is out of pod-NetworkPolicy scope)" |

The remaining 21 fields (id`p.cidr`/`.port`/`.extraPorts` counted as one
wiring unit, per the corrected field count below) are wired in this PR.

**Field-count correction (spike finding):** `NetworkPolicyIdPEgressOverride`
([api/v1alpha2/kubernaut_types.go:2334-2339](../../../api/v1alpha2/kubernaut_types.go))
embeds `NetworkPolicyEgressOverride` (`CIDR`, `Port`) inline and adds its own
`ExtraPorts`. The audit's AST-based leaf counter attributed only
`idp.extraPorts` to this type (an artifact of Go's embedded-field
promotion), undercounting by 2. The true `networkPolicies.*` field count is
26, not 24; the existing single-unit "IdP egress override" wiring task
already covers all 3 (`cidr`, `port`, `extraPorts`) together.

### 1.2 #423 field-level findings

| Field | Audit claim | Verified disposition |
|---|---|---|
| `image.pullSecrets` | consumer in `internal/resources`, no test in that package | Confirmed as stated |
| `postgresql.sslMode` | consumer in `internal/controller`, no test in that package | Confirmed as stated |
| `notification.routing.configMapName` | consumer in `internal/controller`, no test in that package | Confirmed as stated |
| `gateway.resources` | consumer in `internal/resources`, no test in that package | Confirmed as stated |
| `additionalClusterRoles` | consumer in `internal/controller`, no test in that package | Confirmed as stated |
| `signalProcessing.proactiveSignalMappings.configMapName` | consumer in `internal/controller`, no test in that package | **Reinterpreted**: the controller only nil-checks the *parent* `ProactiveSignalMappings` pointer before calling `buildCoreConfigMaps`, never reads the leaf `ConfigMapName` field itself. The new regression test targets that pointer-check path, not a direct read of the leaf field. |
| `apiFrontend.rbacRolesConfigMapRef.configMapName` | consumer in `internal/controller`, no test in that package | **False positive**: zero reads of this field exist anywhere in `internal/controller`. Its real (and already-tested) consumer is [internal/resources/validation.go:334](../../../internal/resources/validation.go). Closed via documentation note in the PR, no speculative test added. |

## 2. Test Strategy

Standard project TDD (RED -> GREEN -> REFACTOR), kept as separate sequential
phases per [AGENTS.md](../../../AGENTS.md), following the Pyramid Invariant:
**UT proves logic, IT proves wiring, E2E proves the journey.**

- **UT** (`internal/resources/*_test.go`): pure resource-builder-level
  coverage -- every new/backfilled `.resources`, config-rendering, TLS CA
  override, Secret/volume-mount, and `networkPolicies.*` override field gets
  an assertion on the builder's rendered output, following each file's
  existing `Describe`/`Context`/`It` conventions.
- **IT** (`internal/controller/*_test.go`, envtest-backed): the
  `networkPolicies.*` wiring and the 5 confirmed #423 consistency gaps
  (`postgresql.sslMode`, `notification.routing.configMapName`,
  `additionalClusterRoles`, the `signalProcessing` pointer-check,
  `image.pullSecrets` if its consumer sits in the reconcile path) get
  controller-level coverage proving the field reaches the reconciler's
  actual output, not just the builder function in isolation.
- **E2E**: no new E2E test required -- this work closes coverage gaps on
  already-deployed CRD fields; existing `test/e2e/e2e_test.go` generic
  assertions are unaffected.
- **Regression discipline**: per #417/#422 precedent, a newly-added test for
  code presumed already-correct (Phase 3's #423 batch) is expected to pass
  immediately. An unexpected first-run failure is itself a newly-discovered
  bug requiring escalation before continuing, not a test to "fix until
  green."

## 3. Wiring Manifest (CHECKPOINT C / Wiring Verification, per AGENTS.md)

### 3.1 `networkPolicies.*` (#422, 21 fields -> 26 leaves incl. IdP)

| Component | Production Entry Point | Wiring Code Location | Test ID prefix |
|---|---|---|---|
| API server CIDR(s)/port | `apiServerEgressRule` used by every builder via `baseEgress()` | `internal/resources/networkpolicies.go` | NP-APISERVER |
| Gateway ingress override | `gatewayNetworkPolicy` | `internal/resources/networkpolicies.go` | NP-GW |
| DataStorage ingress override | `dataStorageNetworkPolicy` | `internal/resources/networkpolicies.go` | NP-DS |
| APIFrontend ingress override | `apifrontendNetworkPolicy` | `internal/resources/networkpolicies.go` | NP-AF-ING |
| APIFrontend IdP egress override (cidr/port/extraPorts) | `apifrontendEgressRules` | `internal/resources/networkpolicies.go` | NP-IDP |
| KubernautAgent ingress override | `kubernautAgentNetworkPolicy` | `internal/resources/networkpolicies.go` | NP-KA-ING |
| KubernautAgent/LLM egress override | `kubernautAgentNetworkPolicy` (LLM CIDR/port) | `internal/resources/networkpolicies.go` | NP-LLM |
| Monitoring namespace/ports override | `monitoringEgressRules`/`monitoringDestinationEgressRule` (shared EM+KA) | `internal/resources/networkpolicies.go` | NP-MON |
| MCPGateway/Fleet destination override | `fleetDestinationsEgressRule` | `internal/resources/networkpolicies.go` | NP-MCPGW |
| Prometheus destination CIDR/port override | `monitoringDestinationEgressRule` | `internal/resources/networkpolicies.go` | NP-PROM |
| ExternalWebhooks CIDR/port override | `notificationNetworkPolicy` (existing hardcoded Slack-webhook rule) | `internal/resources/networkpolicies.go` | NP-WEBHOOK |
| Port validation bounds (`+kubebuilder:validation:Minimum=1`/`Maximum=65535`) | N/A (schema-only) | `api/v1alpha2/kubernaut_types.go` | NP-BOUNDS |

**Carved out (follow-up issues, not this PR):** `console.ingressNamespaces`, `externalRegistry.{cidr,port}`.

### 3.2 `monitoring.*.tlsCaFile` (#424, 2 fields)

| Component | Production Entry Point | Wiring Code Location | Test ID prefix |
|---|---|---|---|
| EffectivenessMonitor TLS CA override | `EffectivenessMonitorConfigMap` (`emExternalYAML`, split into Prometheus/AlertManager-specific fields) | `internal/resources/configmaps.go` | TLS-EM |
| KubernautAgent TLS CA override | `kaToolsConfig` (`kaIntegrationsPrometheusYAML.TLSCaFile`/`kaIntegrationsAlertmanagerYAML.TLSCaFile`, already independent fields) | `internal/resources/configmaps.go` | TLS-KA |
| APIFrontend severity-triage TLS CA override | `afSeverityTriageConfig` (`PrometheusTLSCAFile`) | `internal/resources/configmaps.go` | TLS-AF |

## 4. Test Scenarios

### 4.1 `networkPolicies.*` overrides (`internal/resources/networkpolicies_test.go`)

| ID | Description | Control tag | Automated? |
|---|---|---|---|
| NP-APISERVER-001 | `apiServerCIDR` override replaces the default API-server egress peer | [AC-4, SC-7] | Yes |
| NP-APISERVER-002 | `apiServerCIDRs` merges additional `/32` peers alongside `apiServerCIDR` | [AC-4, SC-7] | Yes |
| NP-APISERVER-003 | `apiServerPort` override replaces the default egress port | [AC-4, SC-7] | Yes |
| NP-APISERVER-004 | Unset fields preserve today's default CIDR/port (no-override regression guard) | [AC-4, SC-7] | Yes |
| NP-GW-001 | `gateway.ingressNamespaces` restricts Gateway's ingress-allow peers to the named namespaces | [AC-4, SC-7] | Yes |
| NP-GW-002 | Unset preserves today's default ingress behavior | [AC-4, SC-7] | Yes |
| NP-DS-001 | `datastorage.ingressCIDRs` adds CIDR-based ingress peers to DataStorage | [AC-4, SC-7] | Yes |
| NP-DS-002 | `datastorage.ingressNamespaceSelectors` adds selector-based ingress peers | [AC-4, SC-7] | Yes |
| NP-DS-003 | Unset preserves today's default ingress behavior | [AC-4, SC-7] | Yes |
| NP-AF-ING-001 | `apifrontend.ingressNamespaces` restricts AF's ingress-allow peers | [AC-4, SC-7] | Yes |
| NP-AF-ING-002 | Unset preserves today's default ingress behavior | [AC-4, SC-7] | Yes |
| NP-IDP-001 | `idp.cidr` replaces the default `0.0.0.0/0` IdP egress peer | [AC-4, SC-7, IA-8] | Yes |
| NP-IDP-002 | `idp.port` replaces the default egress port | [AC-4, SC-7, IA-8] | Yes |
| NP-IDP-003 | `idp.extraPorts` opens additional egress ports alongside `idp.port` | [AC-4, SC-7, IA-8] | Yes |
| NP-IDP-004 | Unset preserves today's default IdP egress behavior | [AC-4, SC-7, IA-8] | Yes |
| NP-KA-ING-001 | `kubernautAgent.ingressCIDRs` adds CIDR-based ingress peers to KA | [AC-4, SC-7] | Yes |
| NP-KA-ING-002 | `kubernautAgent.ingressNamespaceSelectors` adds selector-based ingress peers | [AC-4, SC-7] | Yes |
| NP-KA-ING-003 | Unset preserves today's default (AIAnalysis/APIFrontend only) ingress behavior | [AC-4, SC-7] | Yes |
| NP-LLM-001 | `llm.cidr` replaces the default allow-all LLM egress peer | [AC-4, SC-7] | Yes |
| NP-LLM-002 | `llm.port` replaces the default egress port | [AC-4, SC-7] | Yes |
| NP-LLM-003 | Unset preserves today's default LLM egress behavior (existing #417/#418 regression tests must remain green) | [AC-4, SC-7] | Yes |
| NP-MON-001 | `monitoring.namespace` scopes the EM/KA ingress-scrape-allow and egress-destination peers | [AC-4, SC-7] | Yes |
| NP-MON-002 | `monitoring.prometheusPort`/`.alertManagerPort` override the manual escape-hatch ports independent of the URL-based auto-detected ports | [AC-4, SC-7] | Yes |
| NP-MON-003 | Unset preserves today's OCP-default namespace/ports (existing `MON-005`-style tests must remain green) | [AC-4, SC-7] | Yes |
| NP-MCPGW-001 | `mcpGateway.cidr`/`.port` override Fleet's MCP Gateway destination egress rule | [AC-4, SC-7] | Yes |
| NP-MCPGW-002 | Unset preserves today's default | [AC-4, SC-7] | Yes |
| NP-PROM-001 | `prometheus.cidr`/`.port` override the Prometheus destination egress rule (distinct from `monitoring.*`) | [AC-4, SC-7] | Yes |
| NP-PROM-002 | Unset preserves today's default | [AC-4, SC-7] | Yes |
| NP-WEBHOOK-001 | `externalWebhooks.cidr`/`.port` override the Slack-webhook egress rule | [AC-4, SC-7] | Yes |
| NP-WEBHOOK-002 | Unset preserves today's hardcoded default | [AC-4, SC-7] | Yes |
| NP-BOUNDS-001 | CRD schema rejects `port` values outside 1-65535 for all new int32 port fields | [SI-10] | Yes (envtest/`make manifests generate` diff review) |

### 4.2 `monitoring.*.tlsCaFile` (`internal/resources/configmaps_test.go`)

| ID | Description | Control tag | Automated? |
|---|---|---|---|
| TLS-EM-001 | `monitoring.prometheus.tlsCaFile` overrides EM's hardcoded Prometheus CA path | [SC-8, SC-12] | Yes |
| TLS-EM-002 | `monitoring.alertManager.tlsCaFile` overrides EM's hardcoded AlertManager CA path independently of Prometheus's | [SC-8, SC-12] | Yes |
| TLS-EM-003 | Unset preserves EM's default `/etc/ssl/em/service-ca.crt` for both | [SC-8, SC-12] | Yes |
| TLS-KA-001 | `monitoring.prometheus.tlsCaFile` overrides KA's hardcoded Prometheus CA path | [SC-8, SC-12] | Yes |
| TLS-KA-002 | `monitoring.alertManager.tlsCaFile` overrides KA's hardcoded AlertManager CA path | [SC-8, SC-12] | Yes |
| TLS-KA-003 | Unset preserves KA's default `/etc/ssl/ka/service-ca.crt` for both | [SC-8, SC-12] | Yes |
| TLS-AF-001 | `monitoring.prometheus.tlsCaFile` overrides AF severity-triage's hardcoded CA path | [SC-8, SC-12] | Yes |
| TLS-AF-002 | Unset preserves AF's default `/etc/ssl/af/service-ca.crt` | [SC-8, SC-12] | Yes |

### 4.3 Cross-consumer consistency (#423) regressions

| ID | Description | Location | Automated? |
|---|---|---|---|
| CONS-001 | `image.pullSecrets` propagate onto rendered pod specs | `internal/resources/deployments_test.go` or `migration_test.go` | Yes |
| CONS-002 | `postgresql.sslMode` reaches the controller's rendered DB config | `internal/controller/*_test.go` | Yes |
| CONS-003 | `notification.routing.configMapName` reaches the controller's ConfigMap build path | `internal/controller/*_test.go` | Yes |
| CONS-004 | `additionalClusterRoles` reach the controller's RBAC binding path | `internal/controller/*_test.go` | Yes |
| CONS-005 | `gateway.resources` propagate onto the rendered Gateway Deployment | `internal/resources/deployments_test.go` | Yes |
| CONS-006 | `signalProcessing.proactiveSignalMappings` nil-vs-non-nil pointer check reaches `buildCoreConfigMaps` | `internal/controller/*_test.go` | Yes |
| (doc-only) | `apiFrontend.rbacRolesConfigMapRef.configMapName` false positive documented in PR, real consumer already tested in `internal/resources/validation.go` | N/A | N/A |

**CONS-004 refinement (post-implementation):** `internal/controller/additionalcomponentrbac_test.go`
already had 3 passing tests exercising `deployAdditionalComponentRBAC` (the real consumer) end
to end, so the "no test in that package" finding was a near-miss, not a hard gap -- every
existing test there sets the field via v1alpha1's deprecated, KA-nested
`spec.kubernautAgent.additionalClusterRoleBindings`, which the conversion webhook relocates to
v1alpha2's top-level `spec.additionalClusterRoles` before the controller ever sees it. None set
the v1alpha2 top-level field directly (the literal string the audit tool matched on), and none
carried a FedRAMP control tag. CONS-004 closes both: it sets
`spec.additionalClusterRoles` directly on a v1alpha2-typed CR and carries `[AC-6]`.

**Bug found during coverage backfill (beyond the original audit scope):**
`kubernautAgent.alignmentCheck.llmProfileRef` (v1alpha2, added to replace v1alpha1's
`AlignmentCheckLLMSpec{Provider,Model,Endpoint}` literal-field pattern -- same
never-had-a-working-credentials-path bug class as #237) was documented as the fix but
`kaAlignmentConfig` in `internal/resources/configmaps.go` never actually read it -- the field
was entirely inert in production, not just untested (the audit's "CONVERSION-ONLY" finding
undersold the severity). Escalated to the user; decision was to wire it in now, consistent
with the #422/#424 precedent of fixing discovered-inert fields rather than only test-covering
them. Fix: `kaAlignmentConfig` now resolves `llmProfileRef` via `ResolveLLMProfile` and
populates `ai.alignmentCheck.llm`'s provider/model/endpoint, taking precedence over the legacy
`ac.LLM` literal (which is preserved as a fallback for backward compatibility with existing
v1alpha1 CRs). Credentials-file mounting (the dedicated-Secret-volume pattern used by
`severityTriage.llmProfileRef`) was deliberately left out of scope: `kaAlignLLMYAML` has no
credentials-file field today, and adding one would require touching `internal/resources/deployments.go`
volume/mount wiring -- a larger change than "make the field reach a resource builder." Filed as a
known follow-on if a future profile with distinct credentials is needed for alignment checks.

### 4.4 Component test-coverage backfill (#425-#441, batched by file cluster)

| Cluster | Fields | File | Automated? |
|---|---|---|---|
| `.resources` passthrough | 10 fields (apiFrontend, kubernautAgent, remediationOrchestrator, dataStorage, workflowExecution, notification, signalProcessing, effectivenessMonitor, aiAnalysis, authWebhook) + `kaResources()` 3-case default-merge | `internal/resources/deployments_test.go` | Yes |
| config-rendering | ~60 fields across apiFrontend/EM/RO/gateway/KA/llmProfiles/notification/dataStorage/workflowExecution/valkey/fleet, plus control-tag additions to existing `PARTIAL` fields | `internal/resources/configmaps_test.go` | Yes |
| Secret/volume-mount | fleet, valkey, notification.slack, llmProfiles.tlsClientSecretRef, dataStorage.signingCert | `internal/resources/deployments_test.go` | Yes |
| single-field | console.auth.secretName tag, postgresql.secretName, image.pullPolicy, dataStorage.retention.defaultDays, gateway.enabled predicate | scattered (`console_test.go`, `deployments_test.go`, controller tests) | Yes |

Full per-field detail is in [CRD_FIELD_COVERAGE_AUDIT.md](CRD_FIELD_COVERAGE_AUDIT.md)'s
per-component sections; this plan does not re-enumerate all ~103 individual
fields to avoid duplicating that document, but every one is in scope for
this PR's Phase 4.

## 5. FedRAMP Control-Objective Traceability

Every security/compliance-relevant field touched by this PR carries its
control tag in the corresponding `It(...)` description, per the existing
project convention (`It("UT-XX-NN [AC-4, CC6.1]: ...")`):

| Control family | Fields |
|---|---|
| **AC-4 / AC-6** (information flow enforcement, least privilege) | all `networkPolicies.*` fields, `additionalClusterRoles`, `apiFrontend.rbac.*` |
| **SC-7 / SC-8 / SC-12** (boundary protection, transmission confidentiality, key/cert management) | all `networkPolicies.*` egress/ingress overrides, `monitoring.*.tlsCaFile`, `llmProfiles.tls*`, `dataStorage.signingCert.*`, `dataStorage.telemetry.tls.*`, `kubernautAgent.telemetry.tls.*`, `gateway.config.telemetry.tls.*`, `workflowExecution.ansible.*SecretRef` |
| **IA-8** (identification/authentication of non-organizational users) | `networkPolicies.idp.*` |
| **AU-x** (audit) | `kubernautAgent.audit.*` |
| **CM-6** (configuration settings) | `postgresql.sslMode`, `image.pullPolicy`, `dataStorage.retention.defaultDays` |
| **SI-10** (input validation) | new port-bound kubebuilder markers (`networkPolicies.*.port` fields) |

## 6. Acceptance Criteria

- All scenarios in §4 pass via `make test-unit` and `make test-integration`.
- `go build ./...` succeeds; `golangci-lint run` reports 0 new issues.
- `make manifests generate` regenerates `config/crd/bases/kubernaut.ai_kubernauts.yaml`
  with the 4 new port-bound validation markers and no other unrelated diff
  (`git diff --exit-code config/` reviewed manually for the expected delta).
- `make test` full suite passes with tier coverage targets held: 96%+ on
  `internal/resources/`, 78%+ on `internal/controller/`.
- No `XIt`/`PIt`/`Skip()` in any new test.
- Single PR closes #422, #423, #424, #425, #426, #427, #428, #429, #430,
  #431, #432, #433, #434, #435, #436, #437, #438, #439, #440, #441.
- 2 new follow-up issues filed for the #422 carve-outs (Console
  NetworkPolicy builder, ExternalRegistry target).

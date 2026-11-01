# Issue #419 business test plan

## Scope

Verify that alignment check is an independent sibling agent whose complete LLM
profile is validated, rendered, mounted, and preflight-checked through the
operator's production reconciliation path.

These tests are business-behavior tests, not coverage-only tests.

## Pyramid

### Unit

| ID | Business behavior | Control objectives |
|---|---|---|
| `UT-419-VAL-001` | Enabled alignment rejects an undefined profile reference. | FedRAMP/NIST CM-6, SI-10; SOC 2 CC6.6, CC8.1; ASVS V2.2.1, V2.2.2 |
| `UT-419-VAL-002` / `UT-419-VAL-003` / `UT-419-VAL-004` | OAuth2, tuning, and mTLS path inputs fail closed instead of being silently omitted or mis-mounted. | FedRAMP/NIST CM-6, SI-10, IA-5, SC-8; SOC 2 CC6.6, CC8.1; ASVS V2.2.1, V13.3.1 |
| `UT-419-CM-001` | A distinct alignment profile renders complete provider, credential, OAuth2, TLS, reasoning, retry, and timeout settings. | FedRAMP/NIST IA-5, SC-8; SOC 2 CC6.6, CC8.1; ASVS V8.2.1, V13.3.1 |
| `UT-419-CM-002` | Vertex alignment profiles use `credentials.json` and preserve project/location. | FedRAMP/NIST IA-5; SOC 2 CC6.6; ASVS V8.2.1 |
| `UT-419-CM-004` / `UT-419-CM-005` | Workflow discovery receives the same complete profile semantics and only reuses shared OAuth2 credentials when the Secret is shared. | FedRAMP/NIST CM-6, IA-5, SC-8; SOC 2 CC6.6, CC8.1; ASVS V8.2.1, V13.3.1 |
| `UT-419-DEP-001` | Distinct alignment API/OAuth2/mTLS Secrets are mounted read-only. | FedRAMP/NIST IA-5, AC-6, SC-12; SOC 2 CC6.1, CC6.6; ASVS V8.2.1, V13.3.1 |
| `UT-419-DEP-002` | A shared profile Secret is not redundantly mounted. | FedRAMP/NIST AC-6; SOC 2 CC6.6; ASVS V8.2.1 |
| `UT-419-DEP-003` / `UT-419-DEP-004` | Ambiguous or unresolved alignment references fail closed without guessing a Secret mount. | FedRAMP/NIST CM-6, IA-5; SOC 2 CC6.6; ASVS V2.2.1 |
| `UT-419-DEP-005` / `UT-419-DEP-006` | Workflow discovery mounts OAuth2 and custom-named mTLS Secret keys safely. | FedRAMP/NIST IA-5, SC-8, SC-12, SI-10; SOC 2 CC6.1, CC6.6; ASVS V8.2.1, V13.3.1 |
| `UT-419-CM-003` | OAuth2 credentials directory is shared only when the OAuth2 Secret is shared. | FedRAMP/NIST IA-5; SOC 2 CC6.6; ASVS V8.2.1 |
| `UT-419-CTRL-001` | Alignment API/OAuth2/mTLS Secret references are collected for preflight validation. | FedRAMP/NIST IA-5, AC-6; SOC 2 CC6.6, CC8.1; ASVS V8.2.1, V13.3.1 |

### Integration

| ID | Business behavior | Control objectives |
|---|---|---|
| `IT-419-RECONCILE-001` | Reconciliation reaches generated ConfigMap and Deployment resources with independent alignment profile data and read-only API/OAuth2/mTLS mounts. | FedRAMP/NIST CM-6, IA-5, AC-6, SC-8; SOC 2 CC6.1, CC6.6, CC8.1; ASVS V2.2.1, V8.2.1, V13.3.1 |
| `IT-419-RECONCILE-002` | A missing alignment Secret blocks deployment and exposes the pre-created-configuration validation condition. | FedRAMP/NIST IA-5, SI-10; SOC 2 CC7.2, CC8.1; ASVS V2.2.1, V8.2.1, V16.5.2 |

## Coverage target

The touched resource and controller packages must retain at least 90% unit
coverage. Additional tests are justified only by distinct profile, credential,
transport, validation, or reconciliation business cases.

## Residual dependency

Upstream [`kubernaut#2516`](https://github.com/jordigilh/kubernaut/pull/2516),
at commit `50eafbb4cd12c06488939c8100c348a2de5badf2`, makes the alignment
runtime consume the complete configuration rendered by the operator. Operator
tests establish the resource and reconciliation contract; upstream tests
establish runtime consumption.

# IEEE 829 Test Plan — Issue #565 external exposure boundary

| Field | Value |
|---|---|
| Test Plan ID | TP-565 |
| Issue | [kubernaut-operator#565](https://github.com/jordigilh/kubernaut-operator/issues/565) |
| Design decision | [`DD-565-EXTERNAL-EXPOSURE-BOUNDARY.md`](../../design/DD-565-EXTERNAL-EXPOSURE-BOUNDARY.md) |
| Scope | CR-managed external exposure removal, internal Service delivery, Console callback validation, RBAC, and lifecycle wiring |
| Framework | Ginkgo v2/Gomega; envtest for production reconciliation |

## Pyramid invariant

- **UT proves logic:** schema/RBAC contracts, callback validation, internal URL
  rendering, and internal-only status.
- **IT proves wiring:** real reconciler reaches Running without Route/Ingress
  APIs and preserves internal Service creation.
- **E2E proves the journey:** the operator-only journey does not require an
  operator-created external exposure object; live OpenShift qualification uses
  an administrator-owned exposure fixture when external access is tested.

Tests assert business behavior and control objectives; no test exists only to
increase coverage.

## Evidence matrix

| ID | Business behavior | Controls |
|---|---|---|
| `UT-EXP-565-API-001` | Route/Ingress fields are absent from all component CRD contracts. | FedRAMP CM-3/CM-6/SI-10; SOC 2 CC8.1; ASVS V16.5.2 |
| `UT-EXP-565-RBAC-001` | Operator manager cannot create/manage Route or Kubernetes Ingress resources. | FedRAMP AC-6/SC-7; SOC 2 CC6.1/CC6.6; ASVS V8.2.1/V8.3.1/V13.1.1 |
| `UT-EXP-565-STATUS-001` | Status reports internal/admin-owned exposure and never reports RouteReady. | FedRAMP CM-6/SI-4; SOC 2 CC7.2; ASVS V16.5.2 |
| `UT-EXP-565-INTERNAL-001` | Alertmanager uses the exact internal Gateway Service HTTPS FQDN. | FedRAMP SC-8/SC-13/SC-17; SOC 2 CC6.1/CC7.2; ASVS V12.2.1/V13.2.1 |
| `UT-TLS-2545-OP-001` | Core operator Deployments omit legacy ambient CA environment variables and ambient-only writable volumes. | FedRAMP AC-6/CM-7; SOC 2 CC7.2; ASVS V15.2.5 |
| `UT-TLS-2545-OP-002` | Core operator ConfigMaps render the upstream runtime `tlsCaFile` path. | FedRAMP SC-8/SC-13; SOC 2 CC7.2; ASVS V12.3.1 |
| `UT-EXP-565-CONSOLE-001/002` | Console accepts only an explicit administrator-owned HTTPS callback URL. | FedRAMP AC-6/SC-8/SI-10; SOC 2 CC6.1/CC8.1; ASVS V2.2.1/V12.2.1 |
| `IT-EXP-565-RECONCILE-001` | Real reconciliation reaches Running with internal Services and no external exposure objects. | FedRAMP SC-7/CM-6/SI-4; SOC 2 CC7.2; ASVS V16.5.2 |
| `E2E-TLS-2545-OP-001` | Live Deployments rely on upstream ambient CA ownership and omit legacy static CA env/ambient-only volumes. | FedRAMP AC-6/CM-7/SC-8; SOC 2 CC7.2; ASVS V12.3.1/V15.2.5 |
| `E2E-EXP-565-001` | Live OCP journey keeps Gateway on ClusterIP and creates no operator-managed Gateway Route. | FedRAMP SC-7/SC-8/SI-4; SOC 2 CC6.1/CC7.2; ASVS V12.2.1/V16.5.2 |

## Coverage and gates

- Unit business coverage: at least 90%; zero missed handwritten business
  statements is the preferred gate.
- Controller integration coverage: at least 78%.
- No pending/skipped tests and no `time.Sleep`.
- Generated CRD/RBAC/bundle/chart/installer artifacts must match source.
- This plan is engineering evidence, not a formal compliance claim.

## Execution evidence

The OCP E2E suite collects one failure-only run of the upstream-compatible
`quay.io/kubernaut-ai/must-gather:latest` image through `oc adm must-gather` before
cleanup. Override the image with `KUBERNAUT_MUST_GATHER_IMAGE`, set
`KUBERNAUT_E2E_MUST_GATHER_DIR` to choose the artifact directory, and set
`KUBERNAUT_E2E_PRESERVE_NAMESPACES=true` to retain the operator and CR
namespaces for post-failure inspection.

- `make test-unit`: passed; zero missed business statements; internal unit
  coverage **100.0%**.
- `make test-integration`: passed; controller integration coverage **79.1%**.
- `go build ./...`, `make lint`, `make test-security-traceability`, and
  `make test-pyramid`: passed.
- Live OCP E2E was attempted through `helios08`. The cluster/API discovery was
  healthy, but publishing the disposable operator image was blocked because
  certificate-based `system:admin` access did not provide a registry bearer
  token. No cluster resources were changed and no live E2E claim is made.
- A follow-up OCP run used operator image digest
  `sha256:0403a9afd8d50a1b80bf54a31c7d21460cd1685e9e21adaadb169315476526ed`
  and deployed the operator successfully. The operand images were still
  `kubernaut` `1.6.0-rc20`, which predates upstream PR #2546, so
  EffectivenessMonitor failed in the old `os.CreateTemp` path before the new
  TLS/exposure assertions ran. Must-gather completed successfully at
  `/root/issue565-e2e-runs/issue565-rerun-20261023T183954Z/failure-20261023T184304Z/must-gather`.
  Final runtime qualification remains blocked on the upstream post-#2546 RC.

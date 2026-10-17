# IEEE 829 test plan — Issue #532

## Scope

Verify that operator-owned LLM profile CA and mTLS material is validated,
projected read-only, path-aligned, isolated between investigation,
workflow-discovery, and alignment siblings, and rejected before workload
creation when invalid.

## Business matrix

| ID | Tier | Business case | Controls |
|---|---|---|---|
| `UT-532-VAL-001` | Unit | CA Secret reference requires a rendered CA path. | SC-8, SI-10 |
| `UT-532-VAL-002` | Unit | CA Secret keys cannot contain path separators. | SC-12, SI-10 |
| `UT-532-CM-001` | Unit | Workflow CA/cert/key paths resolve to the workflow material mount. | SC-8, SC-12, CM-6 |
| `UT-532-CM-002` | Unit | Alignment CA/cert/key paths resolve independently. | SC-8, SC-12, CM-6 |
| `UT-532-DEP-001` | Unit | Investigation CA-only material is read-only and projected from its CA Secret. | SC-8, AC-6, IA-5 |
| `UT-532-DEP-002` | Unit | Workflow and alignment use only their selected CA/client Secrets. | AC-6, IA-5, SC-12 |
| `UT-532-DEP-003` | Unit | Identical effective material is reused rather than duplicated. | AC-6, SC-12 |
| `UT-532-AF-CM-001` | Unit | API Frontend main profile TLS paths match its material mount. | SC-8, SC-12, AC-6 |
| `UT-532-AF-DEP-001` | Unit | API Frontend main profile CA/client material is read-only and selected-key-only. | SC-8, SC-12, IA-5 |
| `UT-532-AF-CM-002` | Unit | Severity-triage uses independent TLS paths and material. | SC-8, SC-12, AC-6 |
| `IT-532-RECONCILE-001` | Integration | Real reconciliation renders all three sibling ConfigMap paths and Deployment mounts. | SC-8, SC-12, AC-6, IA-5, SI-10 |
| `IT-532-RECONCILE-002` | Integration | Invalid CA PEM produces `BYOValidated=False` and no Kubernaut Agent Deployment. | SC-8, SI-10, CM-6 |

## Gates

- Ginkgo/Gomega unit and envtest integration tests; no coverage-only cases.
- Unit coverage remains at least 90%; baseline before this change was 100% for
  `internal/resources`.
- `go build ./...`, `golangci-lint run`, `make manifests generate`, unit and
  integration suites, and the repository test-pyramid/security-traceability
  gates pass.

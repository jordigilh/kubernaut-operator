# Issue #512 deployment tunability control traceability

**Matrix ID:** KO-SEC-TR-512-001
**Version:** 0.1
**Scope:** v1alpha2 workload tuning, secure defaults, PDB/HPA behavior, and
reconciliation wiring

This is an engineering evidence matrix, not a formal FedRAMP authorization,
SOC 2 attestation, or OWASP ASVS conformance claim. FedRAMP references use
the corresponding NIST SP 800-53 Rev. 5 control objectives. OWASP identifiers
are versioned requirements from OWASP ASVS 5.0.0.

Authoritative references:

- [NIST SP 800-53 Rev. 5 Update 1](https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final)
- [FedRAMP control reference](https://www.fedramp.gov/2026/reference/controls/)
- [OWASP ASVS 5.0.0](https://github.com/OWASP/ASVS/releases/tag/v5.0.0)

## Evidence matrix

| Business assertion | Implementation evidence | Automated evidence | FedRAMP/NIST | SOC 2 | OWASP ASVS 5.0.0 | Status / residual risk |
|---|---|---|---|---|---|---|
| `BA-TUNE-512-001`: workload overrides retain restricted pod/container defaults while allowing placement and replica tuning. | `WorkloadSpec`; `workloadPodSecurityContext`; `workloadContainerSecurityContext`; `buildDeployment`; Console builder. | `UT-TUNE-512-DEPLOY-001`; `UT-TUNE-512-CONSOLE-001`; `UT-TUNE-512-VALIDATE-002`; `IT-TUNE-512-RECONCILE-001`. | `AC-6`, `CM-6`, `CM-7` | `CC6.1`, `CC6.6` | `v5.0.0-V15.1.1`, `v5.0.0-V15.1.2`, `v5.0.0-V15.2.1` | Partially verified: cluster admission/SCC/PSA enforcement remains platform evidence. |
| `BA-TUNE-512-002`: replicas, node selectors, tolerations, affinity, and topology spread are carried through the production reconciliation path. | `DeploymentParams`; `buildDeployment`; `ConsoleDeployment`; controller `ensureDeployments`. | `UT-TUNE-512-DEPLOY-001`; `IT-TUNE-512-RECONCILE-001`. | `CM-6`, `SI-4`, `SA-11` | `CC7.2`, `A1` | `v5.0.0-V16.5.2` | Partially verified: live scheduler placement and multi-zone behavior require Kind/OpenShift qualification. |
| `BA-TUNE-512-003`: PDB defaults remain Helm-compatible, explicit PDB opt-out/thresholds work, and HPA creation is disabled unless explicitly enabled. | `PodDisruptionBudgets`; `DataStorageHPA`; `APIFrontendHPA`; controller optional-resource reconciliation and stale-resource pruning. | `UT-TUNE-512-PDB-001`; `UT-TUNE-512-PDB-002`; `UT-TUNE-512-PDB-003`; `UT-TUNE-512-HPA-001`; `UT-TUNE-512-HPA-002`; `IT-TUNE-512-RECONCILE-001`; `IT-TUNE-512-RECONCILE-002`. | `CM-6`, `SI-4`, `SA-11` | `CC7.2`, `A1` | `v5.0.0-V16.5.2` | Partially verified: metrics-server behavior and voluntary disruption require live-cluster evidence. |
| `BA-TUNE-512-004`: invalid PDB/HPA combinations and security-baseline weakening fail before workload activation; generated API artifacts remain synchronized. | `validateWorkloadTuning`; CRD CEL validation markers; generated deepcopy/CRD/bundle/dist artifacts. | `UT-TUNE-512-VALIDATE-001`; `UT-TUNE-512-VALIDATE-002`; `UT-TUNE-512-API-001`; `make manifests generate`; `make bundle`; `make build-installer`; `Makefile`. | `CM-2`, `CM-3`, `CM-6`, `CM-8` | `CC8.1` | `v5.0.0-V16.1.1`, `v5.0.0-V16.2.1`, `v5.0.0-V16.5.2` | Partially verified: release-signing and organizational change-control evidence remain external. |

## Pyramid invariant

- **Unit:** resource tests assert actual Deployment, PDB, HPA, security, and
  validation behavior; they do not exist only to increase coverage.
- **Integration:** envtest drives the production reconciler and verifies
  creation, default opt-in/opt-out behavior, and stale-resource pruning.
- **Manifest/Kind:** generated artifacts and Helm/render checks validate the
  install contract; live scheduler, metrics-server, SCC/PSA, and disruption
  behavior remain deployment-specific evidence.

The repository evidence does not establish formal FedRAMP authorization, SOC 2
compliance, or OWASP ASVS conformance. Full platform qualification, cluster
admission review, metrics-server availability, image provenance, and production
availability remain residual risks.

## Evidence artifact inventory

The implementation and generated-artifact review covers:

- `api/v1alpha2/kubernaut_types.go`;
- `internal/resources/deployments.go`;
- `internal/resources/pdb.go`;
- `internal/resources/hpa.go`;
- `internal/resources/validation.go`;
- `internal/controller/kubernaut_controller.go`;
- `internal/controller/kubernaut_lifecycle_test.go`;
- `internal/resources/workload_tuning_test.go`;
- `config/crd/bases/kubernaut.ai_kubernauts.yaml`;
- `bundle/manifests/kubernaut.ai_kubernauts.yaml`;
- `dist/install.yaml`;
- `Makefile`.

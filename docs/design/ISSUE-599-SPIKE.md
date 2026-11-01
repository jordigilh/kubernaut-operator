# Issue #599 — WorkflowExecution Pod RBAC spike

| Field | Value |
|---|---|
| Issue | https://github.com/jordigilh/kubernaut-operator/issues/599 |
| Operator branch | `fix/599-workflowexecution-pod-rbac` |
| Spike scope | Upstream cancellation semantics and Fleet authorization boundary |
| Date | 2026-11-01 |
| Status | RBAC gap confirmed; upstream release boundary documented |

## Question

Can the remaining uncertainty behind the proposed WorkflowExecution Pod
permission be reduced before implementation, without changing operator
production code?

## Evidence reviewed

The read-only upstream source review used kubernaut PR #2583 at commit
`d2e9b3e7a7fca349e1a39cb211db266c4616bab2`.

### Verified cancellation requires Pod reads

- `pkg/workflowexecution/executor/job.go` implements `JobExecutor.VerifyCancellation`.
  It first verifies that the owned Job is gone, then lists a namespaced
  `corev1.PodList` with the `job-name=<job>` label. Any Pod other than
  `Succeeded` or `Failed` keeps cancellation unverified.
- `pkg/workflowexecution/executor/tekton.go` implements
  `TektonExecutor.VerifyCancellation`. It lists TaskRuns, then lists Pods by
  `tekton.dev/pipelineRun` and `tekton.dev/taskRun` labels so a terminal
  PipelineRun/TaskRun does not incorrectly imply that every workload Pod has
  stopped.
- `internal/controller/workflowexecution/workflowexecution_controller.go`
  adds the exact upstream marker:

  ```go
  //+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
  ```

  The marker comment explains that `watch` is required for the cached client
  informer even when the application call is `List`.

### Parent-gate and cancellation semantics remain fail-closed

- `workflowexecution_pending.go` performs a fresh `APIReader` RemediationRequest
  gate before backend creation or cancellation. A failed/cancelled parent moves
  the WFE to `Cancelling`; a non-executing parent does not authorize work.
- `workflowexecution_lifecycle.go` calls `RequestCancellation` and then
  `VerifyCancellation`. It persists `Cancelled` only when the executor returns
  `Terminated=true`; active Pods, request errors, and verification errors remain
  observable and retryable.
- Upstream tests cover the boundary directly:
  - `UT-WE-2582-001`: pre-created WFE cannot dispatch before the parent reaches
    `Executing`.
  - `UT-WE-2582-002`: malformed parent references fail closed into verified
    cancellation flow.
  - `UT-WE-2582-003`: Job cancellation remains unverified while an owned Pod is
    running.
  - `UT-WE-2582-004`: Tekton cancellation remains unverified for active
    TaskRuns, TaskRun-owned Pods, and PipelineRun-owned Pods.

## Fleet assessment

The operator's existing Fleet contract remains sufficient for Pod reads:

- `oidc:kubernaut-fleet-workflow-execution` is intentionally limited to Jobs
  and Tekton resources.
- The WorkflowExecution Fleet identity is also a member of
  `oidc:kubernaut-fleet-read`.
- That read group is bound to Kubernetes `view`, which supplies read-only Pod
  access. Adding Pods to the dedicated execution role would duplicate access
  and weaken the read/write separation in DD-457.

The existing operator unit test already asserts the dedicated Fleet role's
exact rule set and the read-group `view` binding. In addition, the upstream
Fleet E2E infrastructure (`test/infrastructure/fleet_e2e.go`) provisions the
remote spoke, applies exchanged-identity read/write RBAC, binds the read
identity to `view`, and executes remote Fleet scenarios through the real token
exchange path. That is sufficient system-level evidence for this issue's Fleet
boundary. The upstream E2E uses its environment-specific identity subject;
DD-457's fixed `oidc:` group names remain covered by the operator's exact RBAC
unit contract.

## Release boundary

The operator currently pins kubernaut `v1.6.0-rc29` and its matching
WorkflowExecution image digest. That release does not contain
`VerifyCancellation`, `Cancelling`, or `Cancelled`; those changes are in
kubernaut PR #2583, which remains open without a merge commit at the time of
this spike. The embedded WorkflowExecution CRD therefore also remains on the
pre-cancellation phase schema.

This operator issue supplies the least-privilege RBAC prerequisite without
pinning an unreleased application image or dependency revision. The compatible
operand image, shared CRD assets, and upstream Fleet cancellation client fixes
must be synchronized when #2583 is released. The remote Tekton GVK behavior is
upstream application scope, not an operator ClusterRole or Fleet-group change.

## Spike decision

**YES — the operator-side RBAC prerequisite is confirmed.** Add only core Pod
`get`, `list`, and `watch` to the managed `workflowexecution-controller`
ClusterRole. This matches the upstream marker and the exact cancellation
implementation under review. No Pod write verbs, Pod subresources, parent-gate
changes, CRD changes, unreleased operand pin, or Fleet execution-role changes
are justified in this operator issue.

**No new live Fleet run is required for #599.** The existing upstream Fleet E2E
already proves the exchanged-identity/spoke authorization architecture. A new
run would only repeat that evidence and is not justified by this operator-only
Pod-read rule, especially because the Fleet execution role is deliberately
unchanged.

## Optional follow-up validation

If a disposable or authorized spoke is available during release qualification,
the following remains useful as supplemental evidence:

1. Check the execution group alone cannot read Pods.
2. Check the read group can `get`, `list`, and `watch` Pods through `view`.
3. Check the WorkflowExecution identity, carrying both documented groups, can
   read Pods and can still perform only the documented execution writes.
4. Repeat through the real RFC 8693/MCP path and retain the spoke audit/SAR
   evidence.

No production implementation was performed during this spike.

# Issue #501 — Operator/upstream CI boundary and release qualification

**Issue:** [kubernaut-operator#501](https://github.com/jordigilh/kubernaut-operator/issues/501)  
**Related work:** [PR #500](https://github.com/jordigilh/kubernaut-operator/pull/500), [Issue #498](https://github.com/jordigilh/kubernaut-operator/issues/498)  
**Status:** Approved operating model; operator-side gates implemented; upstream workflow implementation is a separate follow-up  
**Date:** 2026-10-04

## 1. Decision summary

The repositories have a one-way CI/CD dependency:

```text
kubernaut-operator CI/CD
  operator source + Kubernetes/dependency fixtures
  contract and reconciliation qualification
        |
        | upstream CI may consume an operator SHA/image
        v
kubernaut CI/CD
  full Kubernaut application images
  operator compatibility and release qualification
```

The operator repository MUST NOT check out, build, pull, deploy, or invoke CI
for the full upstream Kubernaut application. Upstream CI MAY check out and build
the operator, install its CRD/manifests, and deploy the complete Kubernaut
application.

The upstream qualification result is a release gate for the operator, but the
operator repository remains the owner of its Git tags and published artifacts.
Upstream qualifies an exact operator commit; it does not create or push the
operator tag.

This document defines the process and evidence contract. The upstream workflow
that consumes the operator is tracked separately so the operator repository does
not acquire a reverse CI dependency.

## 2. Repository responsibilities

### 2.1 `kubernaut-operator`

The operator repository owns:

- unit tests for resource builders, webhooks, policies, and API contracts;
- envtest controller and reconciliation tests;
- CRD, status, resource-shape, ownership, TLS, RBAC, and dependency-contract
  tests;
- optional Kind tests using deterministic operator-contract and dependency
  fixture images;
- generated CRDs/manifests, lint, build, SBOM, vulnerability scanning, signing,
  provenance, bundle, and catalog artifacts;
- operator branch protection, Git tags, and operator image publication.

The operator's Kind suite is contract-only. It loads the local operator image,
the local `test/e2e/kind/contract` image, and pinned Kubernetes/dependency
fixtures. It does not qualify the behavior of the upstream Kubernaut services.

### 2.2 `kubernaut`

The upstream repository owns:

- full Kubernaut application image and chart builds;
- compatibility qualification against an exact operator SHA;
- full application lifecycle, TLS, service-integration, upgrade, and
  refactor-compatibility tests;
- the upstream RC/release image and chart digest record;
- a machine-readable qualification record and a status/check on the exact
  operator commit.

The upstream workflow must not rely on a mutable operator image tag. For an
unpublished operator candidate it checks out the operator source at the
resolved SHA and builds the image locally or from an immutable build artifact.

## 3. Development PR flow

For an upstream pull request targeting `main`:

1. Upstream CI checks out the upstream PR revision.
2. In a separate checkout, it checks out the current operator `main` branch.
3. It resolves and records the operator SHA at the start of the run.
4. It builds the operator image from that SHA.
5. It installs the operator CRD and production manifests.
6. It deploys the full upstream application and runs compatibility tests.
7. It records the upstream PR SHA, operator SHA, image identifiers, and result.

The word “current” means the branch revision resolved by that run, not a
mutable registry `latest` image. A retry may resolve a newer branch SHA; every
run must retain its own resolved SHA and evidence.

For an upstream pull request targeting a maintenance branch, the matching
operator maintenance branch is used instead of `main`.

Operator pull requests do not trigger upstream CI. They run the operator's own
unit, integration, security-traceability, pyramid, and contract/dependency
checks. Full application compatibility is covered by upstream PR and release
qualification.

## 4. Release-candidate flow

The release candidate is published by upstream first, then the matching
operator candidate is published only after compatibility qualification passes.

### 4.1 Select the release line and upstream RC

1. Select the maintenance release line, for example `v1.6`.
2. Select the corresponding upstream and operator maintenance branches,
   conventionally `release/v1.x`.
3. Resolve the newest published upstream RC matching that release line, for
   example `v1.6.0-rc1`.
4. Verify that the selected tag belongs to the selected upstream release line.
5. Record the immutable upstream tag and commit SHA.

The workflow must fail closed if the matching operator maintenance branch does
not exist. It must not silently substitute `main`. RC-specific branch names
may be used while preparing a release, but the release line and resolved RC
tag must be explicit in the evidence record.

### 4.2 Publish the upstream RC

Upstream publishes the RC images and chart first. The upstream release record
must include the digest of every image and the chart/package digest or
immutable version used by qualification.

The RC remains a prerelease until compatibility qualification succeeds. A
published RC is never treated as a production approval merely because its build
and security scans passed.

### 4.3 Qualify the operator SHA

The upstream qualification run then:

1. resolves the operator maintenance-branch head to SHA `C`;
2. records SHA `C` before building or testing;
3. builds the operator image from SHA `C`;
4. installs the CRD and production manifests from SHA `C`;
5. deploys the published upstream RC images;
6. runs full lifecycle, TLS, upgrade/refactor, and service-integration tests;
7. writes a qualification record tying the upstream RC to SHA `C`;
8. publishes a success or failure status against operator SHA `C`.

The upstream run therefore determines which operator SHA is **qualified**, but
the operator team still owns the decision to create the operator tag and
publish the operator artifacts.

### 4.4 Tag and publish the operator

After a successful qualification:

1. The operator release owner creates the operator tag at exactly SHA `C`:

   ```bash
   git tag -a v1.6.0-rc1 C -m "Kubernaut Operator v1.6.0-rc1"
   git push origin v1.6.0-rc1
   ```

2. The operator release workflow verifies that the tag resolves to SHA `C`.
3. It verifies the upstream qualification status and evidence for SHA `C`.
4. It runs or verifies the operator-local release gates:
   - unit tests;
   - envtest integration tests;
   - security traceability and test-pyramid checks;
   - required operator contract/dependency checks;
   - build and generated-manifest validation;
   - SBOM and vulnerability scan;
   - image signing and provenance;
   - bundle and catalog generation/publication.
5. It builds the operator image from the tagged SHA and publishes it by
   immutable digest.

No full Kubernaut application deployment is repeated in the operator release
workflow. The upstream qualification has already provided that evidence.

The upstream RC artifact is evidence and input data; it is not the source from
which the operator Git tag is created. The operator tag always points to a Git
commit in the operator repository.

## 5. Qualification evidence contract

The upstream workflow must produce a durable, machine-readable record equivalent
to the following:

```json
{
  "schema": "kubernaut-compatibility-qualification/v1",
  "result": "passed",
  "upstream": {
    "repository": "jordigilh/kubernaut",
    "ref": "refs/tags/v1.6.0-rc1",
    "sha": "<upstream-sha>",
    "images": {
      "gateway": "quay.io/kubernaut-ai/gateway@sha256:<digest>",
      "datastorage": "quay.io/kubernaut-ai/datastorage@sha256:<digest>"
    },
    "chart": "oci://quay.io/kubernaut-ai/kubernaut@sha256:<digest>"
  },
  "operator": {
    "repository": "jordigilh/kubernaut-operator",
    "ref": "refs/heads/release/v1.6",
    "sha": "<operator-sha>",
    "image": "quay.io/kubernaut-ai/kubernaut-operator@sha256:<digest>"
  },
  "qualification": {
    "workflow_run_id": "<run-id>",
    "workflow_url": "https://github.com/.../actions/runs/<run-id>",
    "profile": "release-candidate",
    "completed_at": "2026-10-04T00:00:00Z"
  }
}
```

The operator release must verify at minimum:

- the qualification result is `passed`;
- the evidence operator SHA equals the tag SHA;
- the upstream ref is the intended RC/release line;
- the referenced upstream image and chart digests are immutable;
- the qualification run belongs to the expected upstream workflow.

The operator release consumes the upstream record from the GitHub Release asset
`operator-compatibility-qualification.json`. The asset is retained as a workflow
artifact during the operator release and embedded in the published
`operator-release-provenance.json` record alongside the operator image digests.

## 6. Failure ownership and rollback

| Failure | Owner | Required action |
|---|---|---|
| Operator unit, integration, contract, or packaging failure | Operator team | Fix the operator branch; do not publish the candidate. |
| Full-app compatibility failure against operator SHA | Joint triage; operator owns operator behavior, upstream owns application behavior | Keep the candidate unpublished for the failing side; rerun with a new exact SHA or corrected upstream RC. |
| Upstream image/chart build, scan, or digest failure | Upstream team | Do not qualify or pair the RC until corrected. |
| Infrastructure-only qualification failure | Workflow owner | Preserve diagnostics, classify the failure, and rerun without changing the source SHAs. |
| Failure after an image/tag has been published | Owning release team | Mark the candidate withdrawn or superseded; never overwrite an immutable tag or digest. Publish a new candidate identifier. |

If the operator branch advances after qualification, the release must tag the
recorded qualified SHA `C`, not the newer unqualified head. If a published
operator candidate must be corrected, use a new RC/candidate identifier rather
than rebuilding over the existing tag.

## 7. Security and control-objective traceability

This is an engineering process and evidence contract. It does not claim formal
FedRAMP authorization, SOC 2 attestation, or OWASP ASVS compliance.

| Evidence behavior | FedRAMP/NIST-oriented objective | SOC 2 objective | OWASP ASVS 5.0.0 | Planned evidence |
|---|---|---|---|---|
| Operator workflows cannot consume the full upstream application | `SA-12`, `CM-3` | `CC8` change control | `v5.0.0-V15.2.4` dependency source control | `CI-501-BOUNDARY-001`, workflow boundary guard |
| Exact repository SHAs and image/chart digests are retained | `CM-8`, `AU-3`, `AU-12` | `CC7` monitoring and investigation | `v5.0.0-V15.1.2`, `v5.0.0-V16.2.1` | `CI-501-PROVENANCE-001`, qualification manifest |
| SBOM, scanning, signing, and provenance gate publication | `SA-12`, `RA-5` | `CC7`, `CC8` | `v5.0.0-V15.1.2`, `v5.0.0-V15.2.4` | `CI-501-ARTIFACT-001`, release artifacts |
| Unqualified operator SHAs cannot be published | `CM-3`, `SI-2` | `CC8`, `A1` | `v5.0.0-V16.5.3` fail-secure behavior | `CI-501-GATE-001`, release preflight |
| Qualification failures and ownership are observable | `AU-3`, `AU-12`, `CA-7` | `CC7`, `A1` | `v5.0.0-V16.1.1`, `v5.0.0-V16.3.4` | Workflow logs, status checks, diagnostics, evidence record |

The evidence identifiers above are requirements for the implementation work;
they are not claims that the upstream workflow already exists in this
repository.

## 8. Operator-side implementation boundary

The operator-side implementation in this repository adds:

- this design document;
- a static CI-boundary guard (`hack/verify-ci-boundary.sh`) that rejects upstream
  source/image/CI references
  from operator workflows and the Kind contract harness;
- release evidence validation (`hack/verify-release-qualification.sh`) that
  checks an upstream qualification record for the exact tag SHA;
- a release workflow qualification gate and operator-local release-gates job;
- release-manifest publication linking operator artifacts to the upstream
  qualification record.

The upstream checkout/build/qualification workflow is intentionally not added
to this repository. It must be implemented and owned in the upstream repository
as a linked follow-up to Issue #501.

## 9. Non-goals

- The operator Kind contract suite is not a substitute for full Kubernaut
  application qualification.
- Production operand image defaults in operator manifests are not removed;
  they are runtime configuration, not operator CI dependencies.
- The operator repository does not dispatch upstream workflows.
- A successful build or vulnerability scan alone does not establish application
  compatibility or formal compliance.

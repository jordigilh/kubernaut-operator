# PR #518: scoped OVN isolation and DNS verification

## Outcome and boundary

The approved repository repairs are implemented: common OVN ANP/BANP allowances
now end with explicit denies, and DNS authorization uses the platform Service's
selector and independently resolved UDP/TCP backend ports. The disposable SNO
check confirms routed HTTP and independent UDP/TCP DNS remain healthy while the
tested unauthorized pod flows time out. Cleanup is independently verified.

This is not closure of the complete F1-F9 audit, whole-operator production
readiness or upstream application qualification. In particular, the separately
audited external/BYO application dependency inventory remains incomplete; strict
egress exposes that gap rather than hiding it with broad Allow rules. Internal
managed-to-managed all-port trust is intentionally unchanged. The OCP SOP is
Route -> ClusterIP Service; application NodePorts are outside acceptance scope.

Historical audits and spike receipts remain immutable. This document is a new,
subsequent source-bound evidence record.

## Source and approval

- Implementation commit: `a30477bc5b621c19fe8e7773dcd3e56ad2cf8339`.
- Baseline: `d602a3949c4938300a282f8e6a5b2d6740e6de17`.
- Approval and alternatives: [`PR-518-OVN-ISOLATION-DNS.md`](../design/PR-518-OVN-ISOLATION-DNS.md).
- No CRD schema, effective RBAC, chart, bundle, new condition type or policy
  family change. Existing ownership authorization, conditional deletion,
  finalizer retention and namespace retention paths are unchanged.

## Repository verification

| Evidence | Result and exact scope |
|---|---|
| RED: `UT-AUDIT-518-ISOLATION-001` | Failed against the baseline because common OVN ingress lacks the terminal deny. |
| RED: `IT-AUDIT-518-DNS-001` | Actual envtest `Reconcile` persisted `app=dns`, failing the Service-derived selector assertion. |
| GREEN: unit policy tests and envtest regression | Passed. The persisted common policy has distinct UDP/TCP backend ports; dependency selector/port changes are reconciled; loss of DNS retains the native policy and sets existing policy readiness false with `DNSUnavailable`. |
| `UT-AUDIT-518-DNS-001`-`006` | Numeric/default/named ports, metrics exclusion, valid identity, nonempty selector, missing or ambiguous DNS Service/ready backend evidence, protocol mismatches and API errors. Unsafe resolution returns no authorization. |
| `UT-AUDIT-518-DNS-WATCH-001` | Exact DNS Service/EndpointSlice events requeue fail-closed OVN/Auto instances; wrong names/namespaces, unlabeled slices, unsupported providers and other providers do not qualify as DNS dependency events. |
| CHECKPOINT W | Live-checkout `gopls references` resolves `ResolveDNSDestination` to `reconcileProviderPolicies` at controller line 2076. Existing registered Service/EndpointSlice watches call the extended mapper. The envtest case drives production `Reconcile`, not a render-only test. |
| Build/lint/compilation | `go build ./...`, pinned `bin/golangci-lint run` (zero issues), and `go test ./... -run='^$' -timeout=30s` pass. |
| Generation | `make manifests generate` produces no tracked diff in `api/`, `config/`, `charts/` or `bundle/`. This is not independent fresh RBAC regeneration proof and does not close F8. |
| Full checks | `make test` passes unit, envtest, Helm, security traceability, CI boundary and pyramid checks. Controller integration is 79.8%; process-bootstrap `cmd` integration is separate at 22.7%. |
| Independent unit accounting | An external Go AST/profile analyzer, not the repository verifier, confirms 5,813/5,813 business statements across 733 functions, zero missed blocks, zero missing functions and zero baseline-only executions. Integration is not merged into this calculation. |

Business scope is handwritten runtime Go under `api/`, `internal/` and `cmd/`,
excluding generated code and the four documented process/registration functions.
The raw all-instrumented unit profile is not 100%: 6,388/7,096 statements are
executed. Neither percentage is a security or application-readiness attestation.

## Independent negative assertions

Two read-only Go overlays deliberately corrupt otherwise valid production data;
they do not edit the checkout or alter live objects:

1. Replace the controller's resolved DNS selector with `app=dns`: the envtest
   integration test fails specifically at "the DNS peer must come from the
   persisted Service", with the persisted wrong selector and expected custom
   Service selector in its assertion output.
2. Render the TCP rule using the UDP backend port: the unit test fails at its
   complete DNS rule assertion (TCP 5454 versus incorrectly rendered 5353).

Both counterexamples exit 1 due to the intended Gomega assertions, not a build,
client, API-server-startup or probe-process failure. Normal source passes.

## Live packet evidence

The disposable follow-up is limited to OpenShift 4.22.16/OVNKubernetes on
`kubernaut492-sno-a`: restricted fixture pods, a ClusterIP Service, an HTTP Route
and a common ANP selecting only managed probe pods in a unique temporary
namespace. No existing workload, DNS/router object, platform policy, BANP
`default`, operator installation, RBAC or SCC change is permitted.

The adapter consumes a read-only live DNS Service/EndpointSlice snapshot and
calls the actual resolver and renderer. Detection readiness is supplied; this
is not live operator discovery or an installed operator's reconciliation path.
The source snapshot preceded commit creation; separate SHA-256 comparisons bind
every changed runtime source file to the implementation commit above. This is
runtime-source evidence, not proof of an operator binary's embedded commit or a
final/upstream release artifact.

The executed run was **2026-10-11 23:46:09-23:56:35 UTC**. Each row below has two
consecutive matching observations, fresh client processes/connections and a
per-run HTTP body. DNS queries request the fixture Service's actual A record,
check the response ID/rcode/address, and independently exercise UDP and TCP
through both `dns-default` Service port 53 and DNS pod backend port 5353.

| Adapted common ANP state | Managed pod/ClusterIP HTTP | Admitted external HTTP Route | Outsider pod ingress | Managed -> unmanaged pod egress | UDP DNS, Service/backend | TCP DNS, Service/backend |
|---|---|---|---|---|---|---|
| No policy | Pass | HTTP 200 | Pass | Pass | Pass | Pass |
| Repaired renderer | Pass | HTTP 200 | Timeout | Timeout | Pass | Pass |
| Replace only DNS pod selector with a nonmatching label | Pass | HTTP 200 | Timeout | Timeout | Timeout | Timeout |
| Restore repaired DNS selector | Pass | HTTP 200 | Timeout | Timeout | Pass | Pass |
| Remove disposable ANP | Pass | HTTP 200 | Pass | Pass | Pass | Pass |

The negative pod probes target a server demonstrably listening on TCP 8080, with
same-stage managed positive controls and successful restoration. Denied requests
report actual timeouts, not connection refusal, HTTP errors or failed probe
processes. The DNS selector-only negative and restoration preserve the terminal
denies and distinguish DNS authorization from permissive fall-through.

The persisted Route targets the ClusterIP Service's named HTTP port and is
`Admitted=True`. The observed router is host-networked; its labels are **not**
credited as an ANP pod-peer identity. HTTP is tested through the existing router
using the admitted Route Host header, not an application NodePort. This does not
qualify TLS, authentication, every router publishing configuration or a
host/node/perimeter deny. Native OVN reports `SetupSucceeded`/Ready-In-Zone, but
that status alone is not the packet evidence.

The namespace `audit518-route-fcf117c2d6`, its pods, ConfigMap, Service and Route,
and ANP `audit518-route-fcf117c2d6-anp` were removed using exact run identities and
atomic UID/resourceVersion deletion preconditions. No finalizers were removed.
Separate authenticated API reads confirm the namespace and ANP are NotFound;
ANP/BANP lists remain empty, as before the run. Existing DNS/router/workloads and
permissions were not changed. Fixture image cache entries may remain; shared
cache removal was neither required nor authorized.

## Evidence location

Executed helpers, profiles, complete logs and receipts are retained in the
session's external artifact directory:

`/private/var/folders/r7/gktmmltd1zq7wqhsjjslwsm80000gn/T/opencode/pr518-remediation/`

Key artifacts are `dns-isolation-full-checks.log`,
`dns-isolation-final-checks.log`, `dns-isolation-independent-coverage-final.json`,
`negative-dns-probe-receipt.json`, `committed-runtime-binding.json`,
`repaired-dns-live-snapshot.json` and `repaired-ovn-render.json`.

Render SHA-256: `dc21c0297ab9371885443c68996544483d9a9745e9d70a1b7b9e69f9b31ab2a4`.

Derived, reviewer-readable summary:
[`evidence/pr518-ovn-dns-route.json`](evidence/pr518-ovn-dns-route.json).

- Raw live receipt: `sno-a-route-dns-repair-receipt.json`, SHA-256
  `871bbb6ab3d9827407f8e8a29d9b0a427379e609d750fbe79a51947337928d8e`.
- Independent cleanup: `sno-a-route-dns-independent-cleanup.json`, SHA-256
  `81e07ddb78c52659d0d9229994e09cc9b7e6bcf3e25901be892244b713affc96`.
- Complete event log: `sno-a-route-dns-repair.jsonl`, SHA-256
  `c7a371ffb68c729021331bc8390952f18c4941b8fede6726a97bb9b57b6c08a2`.
- Executed helper: `sno_route_dns_repair.py`, SHA-256
  `35f8af23f4d5e346d2402ffaf78e859fc8a263b5b8965c16fa2e7f4a331cae61`.

## Control objectives and remaining gates

Engineering mappings use the independently verified official control register:
NIST SP 800-53 catalog 5.2.0/FedRAMP 2026 AC-4, SC-7/SC-7(5), CM-6 and SA-11;
adapted ASVS 5.0.0 `V13.1.1`/`V13.2.4`. They do not claim FedRAMP authorization,
ASVS conformance or production certification.

Unexecuted gates include the complete operator/ANP/BANP lifecycle, IPv6 packets,
component-to-component port microsegmentation, complete external application
dependencies, real application TLS/OIDC, scrapes/alerts, recovery and final
release/upstream qualification. The remaining F1-F9 decisions require separate
approval. Passing CI does not replace those gates.

**Confidence: 96% in these bounded repairs.** Justification: two full repository
check runs, independent zero-miss unit accounting, actual production envtest
wiring, assertion-level source counterexamples, a repeatable positive/negative/
restoration packet matrix on the supported OCP minor, and independent cleanup.
Limits remain one SNO, IPv4, an adapted common ANP and fixture HTTP/DNS traffic,
not the complete installed operator/application/release journey.

# PR #518: OVN pod isolation and DNS repair

## Approval and scope

The user approved addressing the reviewed default-deny and DNS-selector findings
with `ok address them`. Approval is limited to these repairs and their unit,
controller and disposable SNO validation. The OCP exposure SOP is a Route backed
by a ClusterIP Service. Application NodePorts are outside this task.

Preserve the existing all-port trust domain between selected managed pods. Do
not introduce component-to-component port microsegmentation, a new policy family,
CRD fields, effective permissions, reconciliation phases or condition types.
Preserve singleton, ownership, conditional cleanup, finalizer and namespace
retention contracts. No existing SNO workload, router, DNS object, system label,
BANP `default`, RBAC or SCC may be modified by the disposable validation.

## Evidence and alternatives

The current OVN common ANP/BANP render only Allow rules. Live, fresh IPv4 TCP
8080 pod-IP requests reached unauthorized peers under the adapted ANP. Terminal
pod-ingress and network-egress denies blocked those paths while retaining the
managed positive control. The complete operator/ANP/BANP lifecycle was not run.

The DNS rule selects `app=dns`, but the observed `openshift-dns/dns-default`
Service selects `dns.operator.openshift.io/daemonset-dns=default`. The Service's
UDP/TCP port 53 resolves to pod port 5353. Correcting only the selector restored
Service-name resolution under terminal denies without reopening denied pod
flows. Independent UDP/TCP DNS queries and Route traffic still need validation.

DNS approaches:

1. Hardcode the observed label/5353: small change, but repeats the cluster-shape
   assumption and does not follow selector or backend-port changes. Rejected.
2. Resolve the platform DNS Service and its protocol-specific backend ports:
   selected approach, following the existing Service/EndpointSlice resolution
   pattern and using existing read permissions.
3. Allow the DNS namespace or all its ports: avoids discovery but authorizes
   unrelated workloads and metrics. Rejected.

## Implementation and wiring manifest

- Retain the established OpenShift DNS Service identity, `openshift-dns/dns-default`.
  Resolve its nonempty selector and exactly one UDP and TCP port 53; named
  targetPorts require unambiguous ready EndpointSlice backend ports. Do not
  authorize the metrics port or guess a backend port from a Service frontend.
- Carry typed, validated DNS destination data into the OVN renderer. Missing,
  invalid or ambiguous DNS resolution fails policy readiness observably, retains
  existing policies and prevents activation through the existing policy gate.
- Reuse Service/EndpointSlice watches for DNS dependency reconciliation.
- Append scoped terminal pod-ingress and IPv4/IPv6 network-egress Deny rules
  after the common managed/API/DNS allowances. Preserve higher-priority
  monitoring and component/port-scoped scraper allowances. An Allow-only list
  must not be credited with default-deny semantics.

| Component | Production entry point | Wiring location | Integration proof |
|---|---|---|---|
| DNS Service/backend resolution | `Reconcile` policy gate | `reconcileProviderPolicies` -> `policy.ResolveDNSDestination` | `IT-AUDIT-518-DNS-001` |
| DNS dependency event mapping | registered Service/EndpointSlice watches | `monitoringObjectToKubernaut` | `IT-AUDIT-518-DNS-001`; independent mapper UT |
| Terminal deny and narrowed DNS rules | existing policy render/persist path | `policy.Render` -> `renderOVN`; `ensureRenderedProviderPolicies` | `IT-AUDIT-518-DNS-001`; `UT-AUDIT-518-ISOLATION-001` |

## TDD and validation plan

Estimated engineering phases (not an elapsed-time promise):

- RED, 30-60 minutes: persisted-policy DNS selector/backend assertions, missing
  DNS retention/readiness assertions and exact terminal-deny renderer assertions.
- GREEN, 45-90 minutes: minimal typed DNS resolution, controller wiring,
  dependency mapping and explicit native deny rendering. Both UT and envtest
  wiring must pass before GREEN is complete.
- REFACTOR/verification, 45-90 minutes plus live infrastructure time: resolver
  error/ambiguity/default/protocol coverage, watch changes, independent zero-miss
  unit accountability, build/lint/full `make test`, repeat generation and scoped
  Route/ClusterIP packet evidence with explicit UDP/TCP DNS queries.

No CRD/chart/bundle permission change is planned. Run `make manifests generate`
and verify no unexpected generated diff. A required permission or public API
change is a separate approval gate, not an incidental fix.

## Limits and compatibility

The current common intent does not represent every BYO database, LLM, IdP, Fleet,
callback or telemetry dependency. Deny rules expose that separately audited F4
gap; do not invent broad egress grants or claim complete application readiness.

Contingency: a DNS resolution failure reports the existing policy-readiness
condition as false, prevents activation and retains the previously authorized
policies. It does not delete policies or reopen unmatched traffic. Deployment
rollback requires the administrator's normal reviewed operator-image rollback;
neither this repair nor its probes add an administrator-owned isolation bypass.
Before production rollout, complete the separately approved dependency inventory
and validate real application traffic. The scoped fixtures are not a substitute.

These repairs do not close all F1-F9 findings or qualify upstream applications,
actual scrapes/alerts, final release artifacts, administrator-owned perimeter controls or
IPv6 packets. Route positive/negative evidence is distinct from NodePort
diagnostics. Preserve immutable historical audits and publish new source-bound
evidence separately.

Control objectives are engineering mappings, not certification: NIST AC-4,
SC-7/SC-7(5), CM-6 and SA-11; adapted ASVS 5.0.0 V13.1.1/V13.2.4. Official
source/version applicability remains in the verified independent control
register and `docs/security/ISSUE-513-CONTROL-TRACEABILITY.md`.

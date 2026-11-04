# Issue #609 qualification evidence boundary

This document defines what the operator repository's opt-in qualification lane
does and does not prove. It is engineering evidence guidance, not a live
qualification result. A live result is valid only when the generated JSON
evidence file, immutable image references, and executable command output are
available together.

## Executable lane

Run the lane with approved immutable operator and Keycloak references:

```bash
KUBERNAUT_E2E_QUALIFICATION_OPERATOR_IMAGE='registry.example/operator@sha256:<64 hex>' \
KUBERNAUT_E2E_QUALIFICATION_KEYCLOAK_IMAGE='registry.example/keycloak@sha256:<64 hex>' \
make test-e2e-kind-qualification
```

The lane is opt-in, uses upstream Keycloak, requires the administrator-owned
manual TLS source, binds the Keycloak host port to loopback, and records the
operator image, Keycloak image, Git commit, Kubernetes context, TLS source, and
container tool in the redacted evidence file.

The default Kind contract lane remains unchanged. The qualification lane does
not change the CRD schema or claim a live result when its immutable inputs are
missing.

## Covered evidence

The executable IDs are:

- `E2E-CN-609-HTTP-001`: authorization-code login, configured claims,
  Secure/HttpOnly/SameSite=Lax cookies, and no access-token response leakage.
- `E2E-CN-609-HTTP-002`: authenticated A2A and MCP access, forged identity and
  bearer-header rejection, unauthenticated A2A/MCP denial, and test-value
  absence from OAuth2 Proxy logs.
- `E2E-CN-609-HTTP-003`: live cookie refresh with post-refresh `iss`, `sub`,
  and `sid` validation.
- `E2E-CN-609-HTTP-004`: logout cookie expiry and `sid` rotation on relogin.
- `IT-CN-609-001`: a fresh reconciler repairs persisted OAuth2 Proxy drift and
  rolls the Console workload.

## Explicit deployment boundary

The lane does not mutate or forge Keycloak-signed tokens. Therefore the JSON
evidence records these as gaps rather than implying coverage:

- deployed missing, malformed, and mismatched `iss`, `sub`, and `sid` token
  cases require a controlled signing-key fixture;
- backward compatibility with an existing OAuth2 Proxy session across an
  upgrade is not supported;
- OAuth2 Proxy v7.9 rollback is not supported or claimed.

The supported floor is OAuth2 Proxy v7.15.5+, where the operator-owned alpha
`additionalClaims` configuration is available. This lane does not qualify
rollback behavior.
Qualification starts from a clean deployment using an approved immutable
v7.15.5+ reference; a v7.9 image must not be used as a qualification input.

## Redaction and provenance rules

Evidence may contain image references, commit IDs, statuses, and bounded
descriptions. It must not contain bearer tokens, OAuth2 Proxy cookie values,
Keycloak client secrets, passwords, or raw authorization headers. The HTTP lane
checks response bodies and the OAuth2 Proxy log stream for the test values; a
failure is a failed test, not a successful qualification with omitted detail.

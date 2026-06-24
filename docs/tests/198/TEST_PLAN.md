# IEEE 829 Test Plan — Issue #198: Console nginx proxy_pass TLS

| Field              | Value                                              |
|--------------------|----------------------------------------------------|
| **Test Plan ID**   | TP-198                                             |
| **Issue**          | #198 — Console nginx proxy_pass uses http:// but AF serves TLS on 8443 |
| **Author**         | kubernaut-operator agent                           |
| **Created**        | 2026-06-24                                         |
| **Scope**          | `internal/resources/console.go` — nginx ConfigMap and Deployment |

## 1. Objective

Verify the console nginx reverse proxy connects to AF over HTTPS with
proper TLS verification using the OCP service-ca trust bundle.

## 2. Test Strategy

Unit tests only (no envtest). All tests exercise the `ConsoleNginxConfigMap`
and `ConsoleDeployment` resource builders.

## 3. Test Scenarios

| ID            | FedRAMP | Description                                                         |
|---------------|---------|---------------------------------------------------------------------|
| UT-CN-198-001 | SC-8    | proxy_pass uses https:// scheme for AF upstream                     |
| UT-CN-198-002 | SC-8    | server.conf includes proxy_ssl_trusted_certificate directive        |
| UT-CN-198-003 | SC-8    | server.conf includes proxy_ssl_verify on                            |
| UT-CD-198-001 | SC-8    | console container mounts tls-ca volume at /etc/tls-ca               |
| UT-CD-198-002 | CM-6    | tls-ca volume sources from inter-service-ca ConfigMap               |
| UT-CN-198-004 | SC-8    | proxy_pass does NOT use http:// (negative check)                    |

## 4. Acceptance Criteria

- All 6 scenarios pass (`make test-unit`)
- No regressions in existing console tests (UT-CD-*, UT-CN-*, UT-CS-*, UT-CR-*)
- `make build` succeeds

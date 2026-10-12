# Issue #424: monitoring client TLS trust contract

## Decision

The v1alpha2 API replaces the two accepted-but-unused fields
`spec.monitoring.prometheus.tlsCaFile` and
`spec.monitoring.alertManager.tlsCaFile` with one shared
`spec.monitoring.tls` contract. The old fields are removed because v1alpha2
has not been published; no compatibility aliases are provided.

`source` is one of `interService`, `system`, `secret`, or `configMap`.
`secret` uses `caCertSecretRef.name/key` and `configMap` uses
`caCertConfigMapRef.name/key`; both default `key` to `ca.crt` and are
namespace-local. The operator validates existence and PEM certificate
material before deployment. Administrator-owned Secret and ConfigMap data is
mounted read-only and is never copied or mutated.

All file-based monitoring clients use `/etc/monitoring-tls/ca.crt`:

* EffectivenessMonitor Prometheus and AlertManager clients
* Kubernaut Agent Prometheus and AlertManager tools
* API Frontend Prometheus severity-triage when enabled

`system` uses the container trust store and therefore renders no CA file.
Serving TLS (`spec.tls`) remains a separate contract and is not changed by
monitoring trust configuration.

## Migration

Before v1alpha2 publication, replace either legacy field with:

```yaml
spec:
  monitoring:
    tls:
      source: secret
      caCertSecretRef:
        name: monitoring-ca
        key: ca.crt
```

For a cluster or endpoint trusted by the image's system roots, use
`source: system`. For the operator/platform service CA, use the default
`interService` source. Existing serving-TLS Secret and CA settings do not
belong under `monitoring.tls`.

## Evidence

The API type, generated CRD/deepcopy output, resource unit tests,
controller trust-material validation tests, and deployment rendering provide
repository evidence for the contract. The control-objective anchors are
FedRAMP/NIST `SC-8`, `SC-12`, `SC-13`, `CM-3`, `CM-6`, and `SI-10`, SOC 2
`CC6.1`/`CC7.2`, and OWASP ASVS v5.0.0 `V2.2.1`, `V2.2.2`, `V12.3.1`,
`V12.3.2`, and `V16.5.2`. These are evidence mappings, not a certification
claim. Live Kind/OCP E2E validation still requires a cluster with the selected
monitoring endpoint and is reported separately in the implementation handoff.

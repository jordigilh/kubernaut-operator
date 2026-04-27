# Contributing to Kubernaut Operator

Thank you for your interest in contributing to the Kubernaut Operator!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/<you>/kubernaut-operator`
3. Create a feature branch: `git checkout -b feat/my-feature`

## Development

### Prerequisites

- Go 1.25+
- `operator-sdk` v1.42+
- Access to an OCP 4.18+ cluster (for E2E tests)
- `envtest` binaries: `make setup-envtest`

### Build and Test

```bash
make build          # Build the operator binary
make test           # Run unit and integration tests (envtest)
make test-e2e       # Run E2E tests (requires OCP cluster)
make lint           # Run linters
```

### Code Style

- Follow idiomatic Go conventions
- Use `k8s.io/utils/ptr` instead of custom pointer helpers
- All new ConfigMap YAML content must use typed structs with `yaml.Marshal`
- Wrap errors with `%w` for proper `errors.Is`/`errors.As` chains
- Add kubebuilder validation markers for new API fields

### Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
fix(security): validate PostgreSQL hostname before use
feat(api): add port range validation markers
refactor: replace fmt.Fprintf YAML with typed structs
chore: regenerate manifests and bundle
docs: expand README with installation guide
```

## Developer Certificate of Origin (DCO)

By contributing, you certify that you wrote the contribution or have the right
to submit it under the Apache 2.0 license. Sign off your commits:

```bash
git commit -s -m "feat: my contribution"
```

## Reporting Issues

- Use GitHub Issues for bugs and feature requests
- For security vulnerabilities, see [SECURITY.md](SECURITY.md)

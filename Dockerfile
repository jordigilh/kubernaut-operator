# Kubernaut Operator - Multi-Architecture Dockerfile
#
# Base images follow the Kubernaut platform convention (ADR-027):
#   builder:    ubi10/go-toolset   -- RHEL-native Go cross-compile
#   production: scratch            -- zero CVE surface, no shell
#
# Usage:
#   podman build --build-arg VERSION=v1.3.2 -t quay.io/kubernaut-ai/kubernaut-operator:v1.3.2 .

# ============================================================================
# Stage 1: Build
# ============================================================================
FROM registry.access.redhat.com/ubi10/go-toolset:1.25 AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG GIT_COMMIT=unknown

WORKDIR /opt/app-root/src
COPY --chown=1001:0 go.mod go.sum ./
RUN go mod download

COPY --chown=1001:0 cmd/ cmd/
COPY --chown=1001:0 api/ api/
COPY --chown=1001:0 internal/ internal/

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a \
    -ldflags "-s -w -X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT}" \
    -o manager cmd/main.go

# ============================================================================
# Stage 2: Production runtime (scratch -- zero CVE surface)
# ============================================================================
FROM scratch

COPY --from=builder /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /opt/app-root/src/manager /manager

USER 65534

LABEL org.opencontainers.image.source="https://github.com/jordigilh/kubernaut-operator" \
      org.opencontainers.image.title="kubernaut-operator" \
      org.opencontainers.image.description="Kubernetes operator for the Kubernaut AI platform" \
      org.opencontainers.image.vendor="Kubernaut AI" \
      org.opencontainers.image.licenses="Apache-2.0"

ENTRYPOINT ["/manager"]

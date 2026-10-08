# syntax=docker/dockerfile:1

FROM golang:1.25.3-alpine AS build
WORKDIR /src
COPY main.go .
RUN CGO_ENABLED=0 GO111MODULE=off GOOS=linux go build -trimpath -ldflags='-s -w' -o /spire-probe .

# The helper image is pinned by digest. It provides the real SPIFFE Workload
# API client and renews the files consumed by the probe; the probe itself only
# enforces the expected URI SAN and mTLS peer identity.
FROM ghcr.io/spiffe/spiffe-helper:0.11.0@sha256:1c92e5998ad3621e3323f25aabe40f5a88ba730ee0edb248aa8c943ca504e8a2
COPY --from=build /spire-probe /usr/local/bin/spire-probe
ENTRYPOINT ["/spiffe-helper"]

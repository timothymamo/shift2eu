# shift2eu as a minimal container image (docs/adr/0013-ship-shift2eu-as-a-distroless-container-image.md).
# It is the only image the release publishes: the HTTP API's is retired (ADR 0037).
#
#   make image                                   # version, commit and date from git
#   docker buildx build --platform linux/amd64,linux/arm64 .
#
# Base images are pinned by the digest of their multi-arch index, with the tag kept in the reference so
# Renovate's dockerfile manager updates both. test/container pins the build settings to .goreleaser.yaml.

# The builder runs on the build host's platform and cross-compiles for the target one: the binary is
# pure Go, so no emulation is needed. Its tag is the Go version in go.mod.
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build

WORKDIR /src

# go.mod's go line is the toolchain pin: every make target exports it as GOTOOLCHAIN (#347, ADR 0035) and
# CI's setup-go sets GOTOOLCHAIN=local. With local, a base image whose Go differs fails the build with
# Go's own error instead of fetching or building with another toolchain. The official golang image sets
# it too; this line keeps the pin in the repository should a base image drop it or set another (#383).
ENV GOTOOLCHAIN=local

# Modules first, in their own step, so a source change does not download them again. The module and
# build caches are cache mounts: they persist between builds but are never part of a layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY cmd/ cmd/
COPY internal/ internal/

# Set by docker buildx from --platform.
ARG TARGETOS
ARG TARGETARCH
# The defaults match internal/version; make image passes the values make build uses.
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

# The same settings as the builds entry of .goreleaser.yaml.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOWORK=off GOFLAGS=-mod=readonly GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags "-s -w -X github.com/timothymamo/shift2eu/internal/version.version=${VERSION} -X github.com/timothymamo/shift2eu/internal/version.commit=${COMMIT} -X github.com/timothymamo/shift2eu/internal/version.date=${DATE}" \
    -o /out/shift2eu ./cmd/shift2eu

# distroless static: CA certificates (for the live sources' TLS calls), tzdata, /etc/passwd with the
# nonroot user (65532), and no shell, package manager or Go toolchain.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329

ARG VERSION=dev
ARG COMMIT=none

LABEL org.opencontainers.image.title="shift2eu" \
      org.opencontainers.image.description="Assess what of a hyperscaler's infrastructure can move to an EU cloud provider" \
      org.opencontainers.image.source="https://github.com/timothymamo/shift2eu" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/shift2eu /shift2eu

# Mount the inventory here, as docs/guide/docker.md does (one command):
#   docker run --rm --read-only --network none --user "$(id -u):$(id -g)" --mount type=bind,src="$PWD/inventory.json",dst=/work/inventory.json,readonly shift2eu assess --inventory inventory.json --target scaleway
WORKDIR /work
USER nonroot:nonroot
ENTRYPOINT ["/shift2eu"]

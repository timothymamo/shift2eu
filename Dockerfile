# shift2eu as a minimal container image (docs/adr/0013-ship-shift2eu-as-a-distroless-container-image.md),
# and the HTTP API's binary, shift2eu-server, as a second one, from the stage named server (PRD #103,
# docs/adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md).
#
#   make image                                   # the CLI; version, commit and date from git
#   make server-image                            # the server, the same way
#   docker buildx build --platform linux/amd64,linux/arm64 .
#   docker buildx build --platform linux/amd64,linux/arm64 --target server .
#
# Base images are pinned by the digest of their multi-arch index, with the tag kept in the reference so
# Renovate's dockerfile manager updates both. test/container pins the build settings to .goreleaser.yaml.

# The builder runs on the build host's platform and cross-compiles for the target one: the binary is
# pure Go, so no emulation is needed. Its tag is the Go version in go.mod.
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build

WORKDIR /src

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

# The HTTP API, with the same settings as the shift2eu-server entry of .goreleaser.yaml.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOWORK=off GOFLAGS=-mod=readonly GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags "-s -w -X github.com/timothymamo/shift2eu/internal/version.version=${VERSION} -X github.com/timothymamo/shift2eu/internal/version.commit=${COMMIT} -X github.com/timothymamo/shift2eu/internal/version.date=${DATE}" \
    -o /out/shift2eu-server ./cmd/shift2eu-server

# The HTTP API's image, built with --target server: the same base and promises as the CLI's below. It
# comes before the CLI's, so the last stage, which docker build . and make image build, stays the CLI.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329 AS server

ARG VERSION=dev
ARG COMMIT=none

LABEL org.opencontainers.image.title="shift2eu-server" \
      org.opencontainers.image.description="Serve shift2eu's assessments of what can move to an EU cloud provider over an HTTP API" \
      org.opencontainers.image.source="https://github.com/timothymamo/shift2eu" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/shift2eu-server /shift2eu-server

# The binary listens on 127.0.0.1:8080 by default, which a container does not publish, so the CMD gives it
# --listen 0.0.0.0:8080. Off loopback it needs a bearer token of at least 32 characters from
# SHIFT2EU_SERVER_TOKEN or a file (--token-file), never both: without one the container refuses to start
# (exit 2) unless --insecure-no-auth. Arguments to docker run replace the CMD, so an operator who passes
# any must give --listen 0.0.0.0:8080 again. Run it behind a reverse proxy that terminates TLS
# (docs/guide/reverse-proxy.md):
#   docker run --rm --read-only --cap-drop ALL --security-opt no-new-privileges -p 127.0.0.1:8080:8080 -e SHIFT2EU_SERVER_TOKEN shift2eu-server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/shift2eu-server"]
CMD ["--listen", "0.0.0.0:8080"]

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

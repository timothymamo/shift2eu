# Install

`shift2eu` comes as release archives for Linux, macOS and Windows, as container images on
`ghcr.io`, and from source. This page says what each release publishes, which image tag to pull, and
how to build the binaries and the images yourself.

`shift2eu` images and binaries start at `v0.2.0`. `v0.1.0` and `v0.1.0-rc.1` were published under the
project's old names: see [Releases before the rename](verify-releases.md#releases-before-the-rename-v010-and-v010-rc1).

> **The HTTP API is retired** ([ADR 0037](../adr/0037-retire-the-http-api-and-release-the-cli-alone.md)).
> `shift2eu-server` was retired in 0.11.0: releases no longer ship its archive, image or OpenAPI
> document. The existing images `ghcr.io/timothymamo/shift2eu-server` stay available but get no
> updates, security fixes included. Run assessments with the CLI, or in the browser on the site.

## The release archives

Each [release](https://github.com/timothymamo/shift2eu/releases) publishes one archive per platform,
`shift2eu_<version>_<os>_<arch>`, for `linux`, `darwin` and `windows` on `amd64` and `arm64`
(`.tar.gz`, or `.zip` for Windows), and one `checksums.txt` covering all of them.

`<version>` is the tag without its `v` (`shift2eu_X.Y.Z_linux_amd64.tar.gz` for `vX.Y.Z`). Each
archive holds the `shift2eu` binary with `README.md`, `LICENSE`, the security policy `SECURITY.md`,
the documentation under `docs/` and the sample inventories under `examples/` that [Try it](try-it.md)
runs on. The binary is static and needs nothing else: [Assess an inventory](assess.md) shows how to
run it.
[Verify a release](verify-releases.md#verify-an-archive) shows how to check an archive against
`checksums.txt`, which detects a corrupted download, and against the build provenance the release
workflow attests for it, which detects a tampered release (from the first release after `v0.3.0`).

## The images

Each release publishes a minimal image to `ghcr.io/timothymamo/shift2eu`: the static `shift2eu` binary on
[distroless static](https://github.com/GoogleContainerTools/distroless) (CA certificates and a
non-root user, no shell or package manager), for `linux/amd64` and `linux/arm64`
([ADR 0013](../adr/0013-ship-shift2eu-as-a-distroless-container-image.md),
[ADR 0015](../adr/0015-publish-the-signed-image-to-ghcr-with-docker-buildx.md)). It is tagged with
the release tag (`vX.Y.Z`), `X.Y` and, for the highest release version that is not a pre-release,
`latest`: a patch to an older line does not move it. A pre-release tag (`v0.2.0-rc.1`) gets only its
own image tag and is a pre-release on GitHub too, never the latest release. Pin a release tag, or better its digest
([Verify a release](verify-releases.md)); `docker run ghcr.io/timothymamo/shift2eu:v0.2.0 version` reports that version without its `v`
(`0.2.0`). The commands in these pages use `v0.2.0`; put the release you want in its place:

```bash
docker pull ghcr.io/timothymamo/shift2eu:v0.2.0
```

[Run with Docker](docker.md) shows how to run it.

## Build from source

From a clone, with the tools [Development](../contributing/development.md) lists, `make build` builds
`./bin/shift2eu`.

To build the image yourself from a clone instead, `make image` tags `shift2eu:<version>`, `shift2eu:dev` and
`shift2eu:latest`, stamped from git (`docker build -t shift2eu .` builds it without the version stamp); use
`shift2eu` in place of `ghcr.io/timothymamo/shift2eu:v0.2.0` in the commands of [Run with Docker](docker.md).
`make image PLATFORMS=linux/amd64,linux/arm64` builds both release platforms (with Docker, that needs
the containerd image store). `make image` runs `docker`, or `nerdctl` where there is no `docker`, as in
the project's Lima VM; set `DOCKER=` to choose.

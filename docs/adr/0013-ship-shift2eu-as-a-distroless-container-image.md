# 0013. Ship shift2eu as a distroless container image, built but not yet published

- Status: Accepted. Amended by PRD #103: the `Dockerfile` builds a second image, the HTTP API
  (Consequences). Amended 2026-11-20 by
  [ADR 0037](0037-retire-the-http-api-and-release-the-cli-alone.md) (issue #393): the release no longer
  publishes the HTTP API's image (Consequences).
- Date: 2026-11-11

## Context

`shift2eu` ships as GoReleaser archives and `make build`. Running it means downloading the binary for the
platform or having a Go toolchain. A container image lets people run `shift2eu assess` with `docker run`,
pinned to a version, in CI pipelines and on hosts with no Go (issue #55).

Forces:

- **Small and inert.** A smaller image pulls faster and has less inside it to patch or attack. The
  binary is pure Go (`CGO_ENABLED=0`), so it needs no libc.
- **TLS later.** The live sources (AWS, Azure, GCP) will call cloud APIs over TLS and need CA
  certificates in the image.
- **Least privilege.** The image must run as non-root and with a read-only root filesystem.
- **No data in layers.** Real inventories and reports (CLAUDE.md rule 2) and credentials (rule 3) must
  never reach a layer, the build stage's included.
- **One build.** The image's binary is built like the release binary: the same Go toolchain version
  and flags. It is not byte-identical: the build context leaves `.git` out, so `go build` stamps no
  `vcs.*` build info, which the GoReleaser build does.
- **Reproducible bases.** Base images are pinned and updated by Renovate (rule 7).

## Decision

A multi-stage `Dockerfile` at the root builds `./cmd/shift2eu` in the `golang` image and copies only the
binary into `gcr.io/distroless/static-debian13:nonroot` (Debian 13, trixie). CI builds it for both Linux release platforms
and smoke-tests it on every pull request; publishing it to a registry is a follow-up.

- **Final stage: distroless static, `nonroot` tag.** It holds CA certificates, tzdata, an `/etc/passwd`
  with the `nonroot` user (65532) and nothing to run but the binary: no shell, no package manager.
  `USER nonroot:nonroot`, `ENTRYPOINT ["/shift2eu"]` in exec form, `WORKDIR /work` for the mounted
  inventory, and the OCI labels `source`, `version`, `revision`, `licenses`.
- **Builder: `golang:<go.mod version>-alpine`.** The tag is the exact Go version in `go.mod`.
  It runs on `$BUILDPLATFORM` and cross-compiles with `GOOS=$TARGETOS GOARCH=$TARGETARCH`, so
  `docker buildx --platform linux/amd64,linux/arm64` builds both without emulating the compiler. The
  final stage has no `RUN`, so nothing executes on the target platform either: the arm64 build needs no
  QEMU, and CI installs none (no privileged `binfmt` container). The builder's libc does not matter, as
  the binary links none; alpine is the smallest to pull.
- **Same build as the release.** `CGO_ENABLED=0`, `GOWORK=off` and `GOFLAGS=-mod=readonly` (issue
  #102, ADR 0012), `-trimpath`, `-ldflags "-s -w"` and the
  `internal/version` `-X` flags, as in `.goreleaser.yaml`. `test/container` reads those settings from
  `.goreleaser.yaml` and fails when the `Dockerfile` drifts from them, or from the `go.mod` version.
- **Versioning from git.** `VERSION`, `COMMIT` and `DATE` are build args, defaulting to the values in
  `internal/version`. `make image` passes the values `make build` computes and tags `shift2eu:<version>`,
  `shift2eu:dev` and `shift2eu:latest`, locally only, so `docker run ... shift2eu` finds it; CI passes the same.
- **Digest pinning.** Every base image is `image:tag@sha256:<digest>`, the digest of the multi-arch
  index, so both platforms resolve and a rebuild uses the same bytes. The tag stays in the reference,
  so Renovate's `dockerfile` manager (enabled by `config:recommended`) updates tag and digest together.
  `test/container` fails on an unpinned `FROM`. The BuildKit image CI's `setup-buildx-action` runs is
  pinned the same way, through a `# renovate:` comment the shared regex manager reads with its digest.
- **Nothing in layers but the binary.** Modules and the build cache are BuildKit cache mounts, never
  layer content. `.dockerignore` is an allow list (`go.mod`, `go.sum`, `cmd/`, `internal/`) minus tests,
  `testdata` and any inventory or report file, with `.git`, `.worktrees`, `bin`, coverage and credential
  files also named explicitly. The final stage copies `/shift2eu` and nothing else.
- **Distribution: not yet.** This change builds the image in CI without pushing it. Publishing to GHCR
  from `release.yml`, signing (cosign) and SBOM or provenance attestations are a follow-up issue: they
  need `packages: write`, an identity for signing and a review of the release workflow's closed step
  list (`check.ReleaseWorkflow`). Amended: issue #72 publishes it, from release jobs of their own with
  docker buildx rather than GoReleaser `dockers` or `ko`; see
  [ADR 0015](0015-publish-the-signed-image-to-ghcr-with-docker-buildx.md).

## Alternatives considered

- **`scratch` as the final stage.** Smaller, but the CA bundle (and tzdata, if the binary ever needs
  it) and an `/etc/passwd` entry for the user would have to be copied from the builder by hand, and
  kept current with it. distroless maintains them; it is built from Debian packages that its own
  scanners track. Measured in CI (run 43033381347, linux/amd64): the `shift2eu` binary is 3.17 MB and the
  distroless `static-debian13` layers about 5.8 MB, for about 9.0 MB uncompressed. tzdata is most of
  the base (4.39 MB, as trixie splits it into `tzdata` and `tzdata-legacy`), then base-files (0.75 MB)
  and the CA bundle (0.32 MB), the rest under 0.2 MB.
  `scratch` would save roughly those 5.8 MB, less what has to be copied in by hand; on an image this
  size that does not outweigh maintaining the files ourselves.
- **distroless `static` without `nonroot`, or `USER 65532:65532`.** Works the same; the `nonroot` tag
  makes the default user non-root even if `USER` is dropped, and names the home directory credentials
  are mounted into (`/home/nonroot`).
- **`debian:bookworm-slim` or `alpine` final stage.** Brings a shell and a package manager, tens of MB
  more and more CVEs to triage, for nothing the binary uses.
- **`golang:<version>-bookworm` builder.** Equivalent output, larger pull.
- **Build the image with GoReleaser (`dockers`/`ko`) only.** Ties the image to a release tag; a
  `Dockerfile` also builds locally and on every pull request. GoReleaser can reuse this `Dockerfile`
  when publishing lands.
- **Publish to GHCR now.** Widens this change into the release workflow and signing; split out to keep
  the reviewable surface small.

## Consequences

- `docker build .` and `make image` produce a working image; CI fails a pull request whose `Dockerfile`
  does not build for both platforms, does not run read-only as non-root, or cannot assess the e2e
  inventory fixture.
- A Go version bump in `go.mod` must bump the builder's tag and digest too; `test/container` fails
  until it does, and the build stage's `GOTOOLCHAIN=local` makes a stale builder fail with Go's own
  error rather than fetch another toolchain (#383). The official `golang` image sets it too; the
  Dockerfile's line keeps the pin in the repository should a base image drop it or set another value.
  Renovate puts the `golang` image in the "Go toolchain" group with the `go` directive and
  `dev/lima.yaml`, so both move in one PR.
- A new directory the build needs at the root must be added to the `.dockerignore` allow list.
- Live sources will read credentials through the SDK default chain from `-e` variables or a read-only
  mount at `docker run` time; the image never contains them.
- Follow-up: publish to GHCR from the release, signed, with an SBOM. Done in issue #72:
  [ADR 0015](0015-publish-the-signed-image-to-ghcr-with-docker-buildx.md).
- Amended by PRD #103 (milestone 4): the builder also compiles `./cmd/shift2eu-server`, with the same
  settings as its own `.goreleaser.yaml` builds entry, and a stage named `server` ships it on the same
  distroless `nonroot` base, with `ENTRYPOINT ["/shift2eu-server"]` and `EXPOSE 8080`. The CLI stays the
  last stage, so `docker build .` and `make image` are unchanged; `--target server` and
  `make server-image` build the server's image. `test/container` holds both stages to the same rules.
- Amended (2026-11-20) by [ADR 0037](0037-retire-the-http-api-and-release-the-cli-alone.md) (issue #393):
  the HTTP API is retired and the release ships the CLI alone. The `server` stage, `make server-image`
  and the server's image are no longer built or published; the paragraph above describes the state
  before that change.

# 0015. Publish the signed container image to GHCR from release jobs of their own, with docker buildx

- Status: Accepted. Amended by issue #92: a pre-release tag is a GitHub pre-release too (Tags, below).
  Amended by PRD #103: a second image, `shift2eu-server`, is published the same way (Consequences).
  Amended by issue #99: an independent `verify` job checks both published images (Consequences).
  Amended by [ADR 0021](0021-propose-releases-with-release-please-and-tag-with-a-github-app.md) (issue
  #142): the release App creates the tags, and the `v*` tag ruleset is split in two (Consequences);
  since issue #157 that App is the release-tag App, the only App that bypasses `Release tag creation`.
  Amended by issue #147: each binary ships in release archives of its own (Consequences).
  Amended by issues #156 and #163: GoReleaser uploads to a draft that a job of its own publishes last,
  after the archives and `checksums.txt` are attested, and a `verify-archives` job verifies them
  (Consequences).
  Amended 2026-11-20 by [ADR 0037](0037-retire-the-http-api-and-release-the-cli-alone.md) (issue #393):
  the release ships the CLI alone; the `shift2eu-server` image and its jobs are gone (Consequences).
- Date: 2026-11-11

## Context

ADR 0013 added a distroless image that CI builds for `linux/amd64` and `linux/arm64` on every pull
request, and left publishing it as a follow-up (issue #72). People still have to clone the repository and
build the image; there is nothing to `docker pull`, pin to a version, or verify as coming from this
repository's release.

Forces:

- **One build.** The published image must be the `Dockerfile`'s: its builder stage is what
  `test/container` pins to `go.mod` and `.goreleaser.yaml`, and its final stage is the distroless choice
  of ADR 0013. `shift2eu version` in the image must report what the release binary reports.
- **Least privilege per job.** Pushing needs `packages: write`, keyless signing an OIDC token
  (`id-token: write`), and GitHub attestations `attestations: write`. The goreleaser job already holds
  `contents: write` (it creates the release); one job holding all of them would let a compromise of
  either step use both. The signing identity is the most valuable of them: whatever holds it can
  publish an image that verifies as this repository's release, so only first-party and Sigstore code
  may run next to it.
- **A closed release workflow.** `check.ReleaseWorkflow` reads `release.yml` as a closed schema (ADR
  0012): every job and step is listed, no env or build cache is carried in, and no `GO*`/`CGO*` names or
  env files appear. The new job must fit that model.
- **Verifiable.** A user must be able to check that an image was built by this repository's release
  workflow from a tag, and see what it contains, with standard tools.
- **Pinned and updated by Renovate** (CLAUDE.md rule 7): every action by commit, every tool by exact
  version.

## Decision

Three jobs in `.github/workflows/release.yml` publish the image, after `test` and `goreleaser`. `image`
builds the `Dockerfile` with `docker/build-push-action` on a pinned BuildKit and pushes the multi-arch
index to `ghcr.io/timothymamo/shift2eu`. `sbom` generates an SPDX SBOM of the pushed index. `sign` signs
the index with cosign (keyless, GitHub OIDC) and attaches build provenance and the SBOM as GitHub
attestations, pushed to the registry beside the image.

- **Only a commit on `main` is released, by mistake-proofing.** The `test` job checks out the whole
  history and first runs `git merge-base --is-ancestor "$GITHUB_SHA" origin/main`, failing if
  `origin/main` is missing from the checkout. Every other job needs `test`, so a `v*` tag pushed by
  mistake on a commit that is not on `main` publishes neither binaries nor an image. The guard is not a
  security boundary: GitHub runs the workflow file of the tagged commit, so anyone who may push a `v*`
  tag can tag a branch whose `release.yml` drops the guard, and get an image signed with this
  repository's release identity. Only the tag ruleset below keeps that out.
- **Publishing waits for the release.** `image` needs `test` and `goreleaser`: a failed GoReleaser run
  publishes no image.
- **Three jobs, split by what they may do and what they run.** Job-level permissions, with the
  workflow's default still `{}`:
  - `image`: `contents: read` (checkout) and `packages: write` (push). No OIDC token, so nothing in it
    can sign, although it runs the `docker/*` actions. Its output is the index digest.
  - `sbom`: `packages: read` only, to pull the image while the package is still private. It runs
    `anchore/sbom-action`, which downloads the third-party syft binary; a compromised syft can neither
    push nor sign here, at worst write a false SBOM that the next job attests. The SBOM leaves the job
    as a workflow artifact (`actions/upload-artifact`, kept one day).
  - `sign`: `packages: write` (push the signatures and attestations), `id-token: write` (the OIDC
    token for cosign and the attestations' Sigstore certificate), `attestations: write` (store the
    attestations in the repository). No checkout, no `docker/*` and no `anchore` action: only
    `sigstore/cosign-installer` and cosign, and GitHub's `actions/download-artifact` and
    `actions/attest`. It logs in with `cosign login ghcr.io --password-stdin`, fed the job's
    `GITHUB_TOKEN` through the step's `env:`, which writes the credential where `actions/attest` reads
    it, so no third-party login action runs there.

  The goreleaser job keeps `contents: write` alone.
- **Steps**, a closed list. `image`: `actions/checkout` (full history and tags, no persisted
  credentials); a `run:` step computing the build arguments and the `latest` decision;
  `docker/setup-buildx-action` with the BuildKit image pinned by tag and digest in `driver-opts` (no
  QEMU, as ADR 0013 explains); `docker/login-action` to `ghcr.io` with the job's `GITHUB_TOKEN`;
  `docker/metadata-action` for the tags; `docker/build-push-action`. `sbom`: `docker/login-action`;
  `anchore/sbom-action`; `actions/upload-artifact`. `sign`: `sigstore/cosign-installer`; `cosign login`;
  `cosign sign --yes --recursive ghcr.io/timothymamo/shift2eu@<digest>`; `actions/attest` (provenance);
  `actions/download-artifact`; `actions/attest` (SBOM).
- **Tags.** The git tag (`vX.Y.Z`) and `X.Y` from `metadata-action`'s semver rules. `latest` only when
  the tag is the highest release tag on `main`: the `latest` job (issue #173, amendment below) sorts
  `git tag -l 'v*.*.*' --merged refs/remotes/origin/main` by version, leaving out pre-releases, and
  both image jobs pass its `true` or `false` to `metadata-action`
  (`flavor: latest=${{ needs.latest.outputs.latest }}`). A patch to
  an older line (`v1.2.4` after `v1.3.0`) does not move `latest`, which `latest=auto` would. A
  pre-release tag (`v0.1.0-rc.1`) gets only its own tag: no `X.Y`, no `latest`. The GitHub release
  follows the same rule (issue #92): `.goreleaser.yaml` sets `release.prerelease: auto`, so a tag with
  a pre-release suffix is published as a GitHub pre-release, never the latest release, and a plain
  `vX.Y.Z` as a normal one, GitHub's "Latest" only when it is the highest release tag, as for the
  images' `latest` (issue #173, amendment below); `check.GitHubRelease` pins that section to exactly that one key. Counting only tags
  merged into `main` keeps a stray tag off `main`, one the guard refused and nobody deleted, from
  outranking every later release and freezing `latest`.
- **One release at a time.** The workflow's `concurrency` group `release`, with
  `cancel-in-progress: false`, queues a tag's run while another runs. Two tags pushed together would
  otherwise race on `latest`, and whichever run pushed last would win, not the highest tag. A run in
  progress is never cancelled, so no release stops half published. GitHub keeps one pending run per
  group: a third tag pushed while one run waits replaces the waiting one, which is then re-run by hand.
- **Same stamp as the release binaries.** `VERSION` is the tag without its `v` (GoReleaser's
  `.Version`), `COMMIT` is `git rev-parse --short HEAD` on a full clone (`.ShortCommit`: git picks the
  short length from the object count, so both jobs clone the whole history), and `DATE` the commit date
  in RFC 3339, UTC. GoReleaser's `.Date` is the time GoReleaser runs, which another job cannot know, so
  `.goreleaser.yaml` now stamps `.CommitDate` instead: both builds report the same date, and a rebuild
  of a tag reports the same date too. Tag-derived values reach the `run:` script through the step's
  `env:`, never through `${{ }}` inside the script.
- **No cache in.** No `cache-from`/`cache-to`: BuildKit on a fresh runner starts empty, as setup-go's
  `cache: false` does for the goreleaser job. The `Dockerfile`'s cache mounts live only for that build.
- **Labels from the `Dockerfile` only.** `metadata-action`'s labels are not passed, so the
  `Dockerfile`'s OCI labels (`source`, `version`, `revision`, ...) are the image's. The index gets the
  `org.opencontainers.image.source` annotation as well, so GHCR links the multi-arch package to the
  repository.
- **Signed and attested by digest, never by tag.** cosign signs the index digest that
  `build-push-action` returns, with `--recursive`, so each platform's manifest is signed as well and a
  per-platform digest verifies too. cosign v3 stores the signature in the Sigstore bundle format as an
  OCI referrer, so verifying needs cosign v3 or later. `actions/attest` attests that digest twice, the
  SLSA build provenance (its default) and the SBOM (`sbom-path`), and pushes the bundles to the registry
  (`push-to-registry: true`); `create-storage-record: false`, as storage records are an organisation API
  and would need `artifact-metadata: write`. `actions/attest-build-provenance` and `actions/attest-sbom`
  are, from v4, thin wrappers around `actions/attest`, and `attest-sbom` cannot turn the storage record
  off, so the workflow uses `actions/attest` directly. The SBOM is SPDX JSON from syft
  (`anchore/sbom-action`, exact `syft-version`), read from the registry by digest, kept only as a
  one-day workflow artifact and uploaded nowhere else (the release would need `contents: write`). syft
  reads the index for the runner's platform, `linux/amd64`; the arm64 image holds the same Go modules
  and the same distroless package versions.
- **BuildKit's own attestations off** (`provenance: false`, `sbom: false`). They would add unsigned
  attestation manifests to the index (shown by GHCR as an `unknown/unknown` platform) that duplicate,
  less trustworthily, the signed GitHub attestations. There is one signed source of provenance and
  SBOM, and it is the one `gh attestation verify` checks.
- **Pins.** Every action by full commit SHA with its version in a comment (Renovate's `github-actions`
  manager). cosign (`cosign-release`), syft (`syft-version`) and the BuildKit image carry a
  `# renovate:` comment; the shared regex manager reads a value of the form `image=<name>:<tag>@<digest>`
  (setup-buildx's `driver-opts`) since this change.
- **Dry run on every pull request.** CI's `image` job runs the same `latest` decision on sample tag
  lists, standing for the tags on `main` that `git tag -l --merged` lists in the release (one where
  the sample release is the highest, one where it is not, one pre-release), feeds the results to the
  same tag rules and checks the tags, builds the multi-arch image with the same settings into an OCI
  archive, and generates and checks the SBOM from it. Pushing, signing and attesting need the OIDC token
  and write access, so only a tag exercises them; the first tag, ideally a `-rc` pre-release, is verified
  by hand with the README's commands.
- **Public package.** GHCR creates a new package private. After the first push it is made public once,
  by hand, in the package settings; later pushes keep it public.
- **Required repository setting: a `v*` tag ruleset.** Who may push a `v*` tag decides who can publish
  under the release identity, and that is a repository setting, not code. A tag ruleset on
  `refs/tags/v*` restricts creation, update and deletion to maintainers (optionally requiring signed
  commits). It is set by hand and is the security boundary; the ancestry guard only catches mistakes.
  It is a precondition of every release: the `release-tag` skill checks that it exists
  (`gh api repos/{owner}/{repo}/rulesets`) and stops before tagging if it does not.
  Since issue #142 (ADR 0021) the release App creates the tags, and the ruleset is split in two,
  because a ruleset's bypass list applies to all its rules: `Release tag creation` restricts creation,
  bypassed by repository admins and the release App; `Release tags` restricts update, deletion and
  non-fast-forward, bypassed by repository admins only. The skill accepts their union. Since issue #157
  the App that creates the tags is the release-tag App, in a job of its own, and it is the only App the
  skill accepts as a bypass actor of `Release tag creation` (ADR 0021, amendment).
- **Verification instructions** pin an exact identity per release: `cosign verify` with
  `--certificate-identity .../release.yml@refs/tags/vX.Y.Z` and the GitHub OIDC issuer, and
  `gh attestation verify` with `--source-ref refs/tags/vX.Y.Z`, `--signer-workflow` and
  `--deny-self-hosted-runners`, on the digest the tag resolves to once.

## Alternatives considered

- **GoReleaser `dockers` / `dockers_v2` with `docker_manifests`.** GoReleaser copies its own prebuilt
  binaries into the build context, so the `Dockerfile`'s builder stage, which ADR 0013 and
  `test/container` pin, would not run; a second, release-only Dockerfile would drift from the tested one.
  It would also put `packages: write` and `id-token: write` in the job that holds `contents: write`.
- **`ko`.** Builds Go images without a `Dockerfile` on its own base image choice; it ignores the
  `Dockerfile` and the distroless `nonroot` final stage this project tested and documented.
- **One publishing job holding push, OIDC token and attestation permissions** (this ADR's first
  version). Fewer jobs, but the third-party syft binary and the `docker/*` actions run next to the
  signing identity: any of them, compromised, could push and keyless-sign an image under this
  repository's release identity.
- **`docker/login-action` in the sign job.** It works, but is one more third-party action next to the
  signing identity; `cosign login` does the same with code the job runs anyway.
- **Push from the goreleaser job.** One job fewer, but every write permission in one place, and
  `check.ReleaseWorkflow` would have to allow the goreleaser job's runner state (docker login config,
  BuildKit) next to goreleaser.
- **BuildKit `provenance: mode=max` and `sbom: true` instead of GitHub attestations.** No extra actions,
  but the attestations are unsigned unless signed separately, and `gh attestation verify` does not read
  them.
- **cosign `attest` for SBOM and provenance.** Works, but the provenance would be ours to write; the
  `actions/attest-*` actions produce SLSA provenance from the run itself and make it visible in the
  repository's attestations, verifiable with `gh attestation verify`.
- **Pin BuildKit in a job-level `env:` as `ci.yml` does.** `check.ReleaseWorkflow` forbids job-level
  `env:` in the release workflow; the pin goes into `driver-opts` with the regex manager extended.

## Consequences

- `docker pull ghcr.io/timothymamo/shift2eu:<version>` works for every release from the first tag on, for
  both platforms; `cosign verify` and `gh attestation verify` check signature, provenance and SBOM. The
  README documents the exact commands.
- `check.ReleaseWorkflow` lists the `image`, `sbom` and `sign` jobs, their permissions and their steps.
- `shift2eu version` in the release binaries and in the image now reports the commit date, not the build
  time; a binary from `make build` still reports the time it was built.
- `shift2eu version` prints the version without its leading `v` (`0.1.0` for the tag `v0.1.0`).
- A failed GoReleaser run publishes no image; a failed `image`, `sbom` or `sign` job does not undo the
  GitHub release. A failure is fixed forward with a new tag. An image pushed by a run whose `sign` job
  failed stays unsigned and fails verification.
- A tag on a commit that is not on `main` fails the release run at its first check, and never counts
  for `latest`. Someone who may push `v*` tags can still bypass the check with an edited workflow, so
  no release is cut until the `v*` tag ruleset exists.
- Release runs are serialised; a tag pushed while one runs waits for it.
- After the first push the GHCR package must be made public by hand, once.
- New actions to keep updated: `docker/login-action`, `docker/metadata-action`,
  `sigstore/cosign-installer`, `anchore/sbom-action`, `actions/upload-artifact`,
  `actions/download-artifact`, `actions/attest`; Renovate groups them with the other actions.
- Amended by PRD #103 (milestone 4): the release publishes a second image,
  `ghcr.io/timothymamo/shift2eu-server`, the HTTP API of
  [ADR 0019](0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md), built from the
  `Dockerfile`'s `server` stage (`--target server`) with the same platforms, stamp, tag rules, SBOM,
  signature and attestations. It has jobs of its own, `server-image`, `server-sbom` and `server-sign`, with
  the same split permissions as `image`, `sbom` and `sign`, rather than a matrix over the two images: a
  matrix job's `outputs` keep the value of one leg only, so the sign job could not read each image's
  digest, and one job per image keeps each one's privileges and steps a closed list.
  `check.ReleaseWorkflow` builds both images' jobs from the same step lists, so the two cannot drift, and
  `check.DryRunMirrorsRelease` holds CI's dry run of each image to its release build. GoReleaser builds
  `shift2eu-server` beside `shift2eu` with the same settings, and each release archive holds both
  binaries (until issue #147, below). That state was never released: no release was tagged between
  the two changes, so `v0.2.0` and earlier ship the CLI alone and the first release with
  `shift2eu-server` ships it in archives of its own. The GHCR package `shift2eu-server` must be made public by hand after its first push, as
  `shift2eu`'s was.
- Amended (2026-11-20) by [ADR 0037](0037-retire-the-http-api-and-release-the-cli-alone.md) (issue #393):
  the HTTP API is retired and the release ships the CLI alone. The `server-image`, `server-sbom` and
  `server-sign` jobs are gone, and no new `shift2eu-server` image is published; the images already
  published stay on GHCR without updates. The paragraph above describes the state before that change.
- Amended by issue #99: a ninth job, `verify`, checks what every release published, independently of
  the agents. Until then the published images were checked by hand, or by the `release-tag` skill inside
  the agents' Lima VM ([ADR 0017](0017-run-a-rootless-container-runtime-in-the-lima-vm.md)); audit
  finding M1 of PR #96 noted that this check is not independent: the agents that run it share the VM,
  and what runs there can alter the check. `verify` needs `image`, `sign`, `server-image` and
  `server-sign` and runs on a fresh GitHub-hosted runner; every script in it starts with
  `set -euo pipefail`. For each image it resolves the tag to a digest and fails unless that is the
  digest this run's image job built (a tag can be moved between the build and the check, a digest
  cannot), and, by that digest, runs the README's "Verify the image": `cosign verify` with the exact
  release identity (`release.yml@refs/tags/<tag>`, GitHub's OIDC issuer, the `push` trigger) and the
  tagged commit (`--certificate-github-workflow-sha`); `gh attestation verify` of the build provenance
  and of the SPDX SBOM, with the repository, the signer workflow, the tag as source ref, the tagged
  commit as source digest (`--source-digest`) and self-hosted runners denied; and the image itself with
  `docker run --rm --read-only --network none`: the CLI's `version`, which must report the tag's
  version, and the server's `--help`, which must exit 0, since `shift2eu-server` has no `version`
  subcommand. So the signature and both attestations are bound to the tagged commit, not only to the
  tag name. The image run is the runner's platform, `linux/amd64`; `linux/arm64` is checked by its
  signature (cosign signed the index recursively) and its attestations only, never run.

  Then it checks the tag rule above for both images, on the digests this run built: `vX.Y.Z` always;
  `X.Y` for every release; and a pre-release moves neither `X.Y` nor `latest`, so a pre-release fails
  only if `X.Y` or `latest` resolves to its digest (a shipped `X.Y` of an earlier release is fine).
  `latest` follows the highest release tag, which, as in the `latest` job, is the highest of the release tags
  merged into `main`, read by ancestry: the job fetches `main` and the `v*` tags of the public repository
  treeless (`git init` and `git fetch --filter=tree:0`, no token) into a repository of its own, with no
  checkout, so no repository code runs, and picks the highest with the `latest` job's filter, sort and
  pick, on its own: an independent check, not a read of that job's output. Not
  GitHub's "latest release", which by default is the most recently published release rather than the
  highest version (`publish` aligns it since issue #173, and `verify-archives` checks it, but `verify`
  does not rely on it), and not every tag on the remote, which would count a tag off `main`. No release tag merged into `main`
  is an error. When the tag is the highest, `latest` must be its digest. When a higher release's images
  are published, `latest` must not be this digest. When a higher release tag merged into `main` has no
  published image, for any reason (its own run still in flight, failed, replaced by a newer pending run
  in the concurrency group, or never run), the `latest` check is skipped with a `::notice::` naming
  that tag. That window is not covered: a release that wrongly moved `latest` passes while the higher
  tag stays unpublished. It includes a stray tag pushed on a commit of `main` whose run never publishes,
  which also freezes `latest` in the `latest` job's decision, since that job counts it as the highest
  too.
  So the notice is acted on, not ignored: the `release-tag` skill checks that the named tag has a
  release run queued or in progress, and otherwise reports the tag as stray or unpublished to the
  maintainer, who decides whether to remove it or re-run its release. A re-run of an older release's
  `verify` after a newer release has shipped passes, since `latest` is then the newer release's. Only the
  registry's own answer that this tag does not exist counts as an absent tag: the inspect exits with
  status 1 and its stderr holds `<image>:<tag>: not found` (a fixed, case-sensitive string) for this
  very reference, and nothing else is absence: a `MANIFEST_UNKNOWN` that does not name the reference
  fails the job like any other error. That not-found text was derived from containerd's resolver
  source, which buildx uses, and from the registry's 404, not observed on a runner; if buildx words
  it otherwise, the job fails closed (a pre-release, or a release whose higher tag is unpublished,
  fails `verify`) and the step must be corrected, not worked around. The rule allows an absent tag
  for `X.Y` and `latest` of a pre-release and for the higher tag above. The digest is read from stdout alone, with
  stderr kept apart, so a warning docker writes there is never read as the digest, and a read that
  succeeds must print exactly one `sha256:` digest of 64 hex digits. Any other answer on any read, a
  5xx, a rate limit, a failed login, a missing docker, a broken credential helper or another
  reference's not-found, is printed and fails the job, so no check passes on an error it took for
  absence.

  `verify` does not check the higher tag's run itself. That would need `actions: read` on the job, a
  wider permission for a case only someone who can already cut a release can cause (pushing a `v*`
  tag); the `release-tag` skill checks it instead.

  Its permissions are `packages: read` (the images and their signatures) and `attestations: read`
  (`gh attestation verify` reads the attestations through the repository's API), nothing else: no
  `id-token`, so nothing in it can sign, and no `contents`, as it checks out no repository code; its
  only action is `sigstore/cosign-installer`, by commit, with cosign by exact release.

  `verify` runs after the images and tags are published, so it detects a bad release, it does not
  prevent one. A failed `verify` is a failed release, never retried around: the `release-tag` skill
  reports the job's result, and on a failure the maintainer marks the GitHub release as bad (edits its
  notes, or deletes it), points `latest` (and `X.Y` if wrong) back at the last good release's digest or
  deletes the bad image tag, and fixes forward with a new patch release; a published tag and digest are
  never reused. `check.ReleaseWorkflow` pins the job's keys, needs, permissions and steps, and
  `check.DryRunMirrorsRelease` holds CI's dry run to run each image it built with the same
  `docker run` flags and command as `verify`, each image's run on that image's own dry-run build
  (`$IMAGE` for the CLI, `$SERVER_IMAGE` for the server), and refuses a `docker run` line of `verify`
  continued onto the next line, which it could not compare whole; pushing, signing and attesting still
  need a tag, so the rest of `verify` runs only in a release.
- Amended by issue #147: each binary ships in release archives of its own, from the same tag and
  version. `.goreleaser.yaml` has two `archives:` entries, each with an `id:` equal to the one build its
  `ids:` names (GoReleaser names an entry without `id:` `default`, and refuses two of them): the CLI's
  `shift2eu_<version>_<os>_<arch>`, the name it always had, and the HTTP API's
  `shift2eu-server_<version>_<os>_<arch>`; both `.tar.gz` (`.zip` on Windows) with `LICENSE`,
  `README.md` and `docs/`. One `checksums.txt` covers every archive. A user who wants one binary
  downloads one, and neither archive carries a binary its user did not ask for. The archives are
  verified as before, against `checksums.txt` (`docs/guide/verify-releases.md`, "Verify an archive");
  they and `checksums.txt` are neither signed nor attested, only the images are, so signing them is
  left to a follow-up that changes `release.yml`. `check.GoReleaserArchives` pins the section as a
  closed schema, and CI's `test` job runs `.github/scripts/verify-archives.sh`
  (`check.DryRunVerifiesArchives`): a GoReleaser snapshot of the release, failed unless each archive
  holds exactly its own binary and otherwise only `LICENSE`, `README.md` and Markdown pages under
  `docs/` (since issues #188 and #178 also the sample inventories under `examples/` and the inventory
  guide's jq programs, `docs/guide/inventory/<source>.jq`, each listed by exact path), every built
  platform has one archive of each and `checksums.txt` matches every archive. The script builds into a
  temporary directory, so a local run leaves `dist/` alone.
- Amended by issues #156 and #163: the release is published last, into a release whose assets cannot
  change once it is public, and its archives are attested and verified like the images.
  - **Draft, then publish.** GitHub's release immutability (Settings, General, Releases) locks a
    release's tag and assets when it is published, so every asset must be uploaded before then.
    GoReleaser v2.20.1, the version `release.yml` pins, already creates every GitHub release as a draft,
    uploads to it and undrafts it as its last step (`PublishRelease`), and fails fast on an existing
    immutable release for the tag; with `release.draft: true` it skips that last step and leaves the
    draft. `.goreleaser.yaml` sets `draft: true`, so a job of `release.yml`, `publish`, publishes it
    after `attest-archives`, holding `contents: write` and nothing else: the draft it checked, by the
    id the API listed it with (`gh api -X PATCH repos/timothymamo/shift2eu/releases/$id -F
    draft=false`; `gh` is given the repository in the path, so none reads it from a git remote).
    Publishing also sends `make_latest`, from the `latest` job's decision below (amended by issue
    #173); `prerelease: auto` still marks a pre-release tag at creation. GitHub finds no draft by its tag, so a re-run of a failed
    `goreleaser` job would create a second draft for the tag beside the one the failed run left:
    `replace_existing_draft: true` makes GoReleaser delete that draft (it matches drafts by name, the
    tag) and start again. A re-run after `publish` succeeded fails in GoReleaser rather than change the
    published release: on an immutable release it refuses at once, and without immutability its first
    upload finds the asset already there and fails, as `replace_existing_artifacts` is off.
    `check.GitHubRelease` holds the section to exactly `draft: true`, `prerelease: auto` and
    `replace_existing_draft: true`, in that order. The image jobs still need only `goreleaser`, and
    push and sign while the release is a draft: if `attest-archives` or `publish` then fails, the
    images are public while the release is still a draft. That is accepted rather than make the image
    jobs wait for `publish`, which would lengthen every release by the attestation and the check; the
    recovery below finishes the release. The signing identity
    (`release.yml@refs/tags/vX.Y.Z`), the `verify` job and release-please (`skip-github-release`, ADR
    0021) do not change. The maintainer turns release immutability on once this lands; no workflow
    changes repository settings.
  - **Archive attestations.** The `goreleaser` job hands `checksums.txt` and the archives on as a
    workflow artifact kept for seven days; `attest-archives` downloads it on a runner of its own, checks
    it with `sha256sum -c checksums.txt`, and attests every archive and `checksums.txt` with
    `actions/attest-build-provenance`, by `subject-path`. The bundled OpenAPI document
    `shift2eu-server_openapi.json` (issue #139, ADR 0019) goes the same way: GoReleaser copies its
    extra files into neither `dist/` nor the artifact, so the `goreleaser` job copies it into `dist/`
    under that name after GoReleaser, and the upload and the subjects name it exactly. Both the upload and the subjects name each
    archive by its extension (`shift2eu_*.tar.gz`, `shift2eu_*.zip`, and the same for
    `shift2eu-server_*`): a bare `shift2eu_*` in `dist/` also matches GoReleaser's build directories and
    would attest the bare binaries beside the archives. One attestation then names each file as a
    subject, so a single archive verifies on its own with `gh attestation verify <archive>`, and so does
    `checksums.txt`; `subject-checksums` would have attested the archives through `checksums.txt` but
    not `checksums.txt` itself. The job holds `id-token: write`, `attestations: write` and
    `contents: read`: it can sign and store attestations, never write the release, and the
    `goreleaser` job, which writes the release, holds no OIDC token. A keyless cosign signature of
    `checksums.txt` was left out: it would be a second identity check of the same files, and
    `gh attestation verify` already binds each file to `release.yml`, the tag and the commit.
    The archives use `actions/attest-build-provenance`, the dedicated provenance action issue #163
    names, while the sign jobs call `actions/attest`: from v4 the former wraps the latter with the
    provenance predicate built in, so the attestation is the same, and the archives, unlike the images,
    need no registry push and no storage-record switch, which is why the images use `actions/attest`.
  - **Check the draft before publishing.** `publish` downloads the same workflow artifact for the
    `checksums.txt` that was attested and, before it edits anything, reads the draft from the API's
    list of the releases (a draft is not readable by its tag; a `contents: write` token sees drafts, and
    every asset carries a `digest`, `sha256:<hex>`). It publishes nothing if the tag already has a
    published release (publishing it again needs a human), and nothing unless there is exactly one
    draft for the tag, its asset names are exactly `checksums.txt` and the files it lists, every
    asset's digest is the one `checksums.txt` records, and its `checksums.txt` is the attested file
    itself. Once published, an immutable release can never shed an unattested asset, so it is refused
    while still a draft. The check and the publication are one step, and the draft is published by
    the id the list gave it, never by its tag: `gh release edit <tag>` resolves a tag to a published
    release before a draft, and a step of its own would look the tag up again, so a release published,
    or a draft swapped in, after the check would be published in the place of the one checked.
  - **Every release asset is in `checksums.txt`.** A new release asset (an OpenAPI document, an SBOM,
    a signature) must be added to `checksums.txt` (`checksum.extra_files`, or GoReleaser's own
    checksum of its artifacts) and to the upload and `subject-path` lists of `release.yml`, which
    `check.ReleaseWorkflow` pins exactly. Otherwise `attest-archives`' `sha256sum -c` fails on a file
    the artifact does not hold, or `publish` refuses the draft for an asset `checksums.txt` does not
    list (issue #139). CI's dry run (`verify-archives.sh`) catches most of them first: what
    GoReleaser would upload, read from its `artifacts.json`, must be exactly the attested subjects, so
    a new asset registered by a pipe a snapshot runs (an SBOM, a signature, a source archive,
    `checksum.extra_files`) fails there. `release.extra_files` does not: GoReleaser adds those only in
    the release pipe's publish step, which `--snapshot` skips. That key is refused by
    `check.GitHubRelease` instead, which holds `release:` to a closed list, and by `publish`'s draft
    check.
  - **Recovery.** A failed `attest-archives` or `publish` leaves the release a draft. Within the seven
    days the workflow artifact is kept, "Re-run failed jobs" on the release run runs the failed job and
    the jobs after it from the same artifact, the same draft and the same commit. A full re-run (or a
    re-run after the artifact has expired) runs `goreleaser` again: `replace_existing_draft` deletes the
    draft and uploads a new one with new archives (and so new digests and a new attestation), and the
    image jobs rebuild and push the images again under the same tags; their `verify` job then checks
    the new digests. "Re-run failed jobs" (`gh run rerun <id> --failed`) does not re-run the
    successful `latest` job, so `publish` reuses the first attempt's `latest` decision (amended by
    issue #173). `publish` therefore refuses a stale `true`: while `LATEST` is `true`, it reads
    GitHub's current "Latest" (`releases/latest`, `tag_name`) before anything else, and when that is
    another tag, higher by `sort -V`, it fails and leaves the release a draft; only the API's 404 (no
    "Latest" yet) passes, and any other failed read fails the step. GNU `sort -V` ranks a pre-release
    (`vX.Y.Z-rc.N`) above `vX.Y.Z`, but `releases/latest` never returns a release flagged as a
    pre-release, GoReleaser flags rc tags as pre-releases (`prerelease: auto`) and `verify-archives`
    refuses a pre-release as "Latest"; so the guard meets such a tag only when a release with a
    pre-release tag name was un-flagged by hand. It then fails closed and leaves the draft: the
    maintainer marks that release a pre-release again (or sets the correct "Latest") and re-runs the
    release run. The images' `latest` tag has no such guard: a partial re-run of a failed image job
    pushes `latest` from that stale decision. So when a higher release tag has been merged into
    `main` since the first attempt, always re-run the whole run (`gh run rerun <id>`, without
    `--failed`), which runs `latest` again. The `release-tag` skill holds the maintainer's steps.
  - **No fake-`gh` dry run.** The scripts of `publish`'s check and `verify-archives` run only in a
    release: CI does not run them against a fake `gh`. They are pinned verbatim by `check.ReleaseWorkflow`,
    were run against a stub `gh` and sample releases when written and again in review, and fail closed
    (each check exits non-zero before the draft is published). A CI harness would have to extract them from the YAML and
    fake the API's paging and asset digests, and would test the stub as much as the script.
  - **GitHub's "Latest" and the image's `latest`** (amended by issue #173). Left to its default, GitHub
    marks the most recently published non-pre-release release its "Latest", so a patch to an older
    line (`v1.2.4` after `v1.3.0`) would take over GitHub's "Latest", and with it every
    `releases/latest/download/...` URL, while the images' `latest` stays on `v1.3.0`. GitHub's
    "Latest" therefore follows the same rule as the images' `latest` tag, and the same decision. A
    thirteenth job, `latest`, needs only `test` and decides once per run whether the tag is the highest
    release tag merged into `main` (`git tag -l 'v*.*.*' --merged refs/remotes/origin/main`, the
    pre-release filter, `sort -V`, `tail -n 1`): it checks out the whole history (`fetch-depth: 0`,
    `persist-credentials: false`) with `contents: read` alone, runs no repository code, refuses a
    checkout without `origin/main` (the run shell, `bash -e`, has no `pipefail`, so the pipeline would
    otherwise leave the highest tag empty and `latest` false without failing), and writes
    `latest=true` or `latest=false` as its output. Both image jobs and `publish` need it and read that
    output (`needs.latest.outputs.latest`); none of them computes it again. Deciding once is what keeps
    them agreeing: were each job to read the tags from its own checkout, a release tag merged into
    `main` between two checkouts would give the images one answer and GitHub's "Latest" another within
    the same run. `publish` keeps no checkout: it takes the decision through `env:` as `LATEST`,
    refuses any value but exactly `true` or `false`, refuses a stale `true` when a higher release is
    already GitHub's "Latest" (see **Recovery**), and publishes with
    `-F draft=false -f make_latest="$LATEST"`. `make_latest` is sent as a string field (`-f`): the API
    takes the string `"true"`, `"false"` or `"legacy"`, and `-F` would send a JSON boolean. A
    pre-release is never the highest (the filter drops it) and is published as a GitHub pre-release,
    which GitHub never makes "Latest". The job's permissions are unchanged, `contents: write` alone.
    `check.ReleaseWorkflow` pins the `latest` job and its line byte for byte, and
    `check.DryRunMirrorsRelease` holds CI's dry run to that job's line.

    Consequence, accepted and shared with the images' `latest` tag: a `vX.Y.Z` tag merged into `main`
    whose release is never published (its run failed, or, still queued, was replaced by a newer tag's
    run in the concurrency group) is the highest release tag, so every later run decides
    `latest=false`, and GitHub's "Latest" stays on the previous release, as the images' `latest` does,
    until that tag's release is published or the tag is deleted. `verify` and `verify-archives` only
    print a `::notice::` naming the tag; they do not fail. The `release-tag` skill acts on the notice:
    re-run the failed release run, or delete the stray tag, as the maintainer decides.
  - **Verify the archives.** A twelfth job, `verify-archives`, runs after `publish` (a draft's assets
    are not public), on a fresh runner with `contents: read` and `attestations: read` and no checkout:
    it downloads every asset of the release, fails unless the release is published (not a draft) and
    holds exactly `checksums.txt` and the files it lists, checks their digests and runs
    `gh attestation verify` on each with `--signer-workflow`, `--source-ref refs/tags/<tag>`,
    `--source-digest` and `--deny-self-hosted-runners`. Its last step checks GitHub's "Latest"
    (issue #173): it fetches `main` and the `v*` tags treeless into a repository of its own, as
    `verify` does, computes the highest release tag with the same line on its own, an independent
    check of the `latest` job's decision rather than a read of it, fails when there is none, and
    reads `releases/latest`'s `tag_name`. A pre-release tag must never be GitHub's "Latest", checked
    first and without any condition, so the skip below cannot let one through. When the tag is the
    highest, GitHub's "Latest" must be the tag. When the highest release tag's release is published,
    GitHub's "Latest" must not be the tag. When the API answers 404 for the highest release tag's
    release (not published yet, for any reason), the check is skipped with a `::notice::`, the same
    window and handling as `verify`'s `latest` check; any other answer fails the job. Like `verify`,
    it detects a bad release rather than prevents one, and a failure is handled the same way.
  - `check.ReleaseWorkflow` pins the three new jobs as it pins the others, with the attest action as
    `actions/attest-build-provenance` by commit. CI's `test` job (`.github/scripts/verify-archives.sh`)
    mirrors the hand-over on a snapshot: the files the upload patterns pick must pass
    `sha256sum -c checksums.txt`, and must be exactly the archives and `checksums.txt`, which must also
    be exactly what GoReleaser would upload to the release.
    `docs/guide/verify-releases.md` ("Verify an archive") and the `release-tag` skill check an archive's
    attestation after each release. Releases up to `v0.3.0` have no archive attestations.

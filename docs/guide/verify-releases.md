# Verify a release

Every published image is signed and carries build provenance and an SBOM, and every release archive
carries build provenance. This page shows how to check all three for an image before you run it, inside
the project's Lima VM too, how to check a release archive against its checksums and its provenance, and
how to verify the releases published before the project's rename.

> The HTTP API is retired ([ADR 0037](../adr/0037-retire-the-http-api-and-release-the-cli-alone.md)):
> releases no longer publish `shift2eu-server` archives, its image or its OpenAPI document. Its images
> up to the last release that shipped them stay on `ghcr.io` but get no updates. Use the CLI, or the
> site in your browser.

## Verify the image

Every published image is signed with [cosign](https://github.com/sigstore/cosign), keyless, by the
release workflow's GitHub identity for that tag, and carries build provenance and an SPDX SBOM as
GitHub attestations. You need cosign v3 or later, which reads the signature from the registry in the
bundle format the release writes, and the [GitHub CLI](https://cli.github.com/). The release
workflow's `verify` job runs these same checks on every release, on a fresh GitHub-hosted runner with
read-only permissions, and fails the release when one fails. It runs after the image is published, so
it detects a bad release rather than prevents one; run the checks yourself before you trust an image.

A tag can be moved; a digest cannot. Resolve the release tag to its digest once, then verify and run
that digest:

```bash
TAG=v0.2.0
DIGEST="$(docker buildx imagetools inspect "ghcr.io/timothymamo/shift2eu:$TAG" --format '{{.Manifest.Digest}}')"
IMAGE="ghcr.io/timothymamo/shift2eu@$DIGEST"

# The signature: made by release.yml, run on a push of exactly this tag.
cosign verify "$IMAGE" \
  --certificate-identity "https://github.com/timothymamo/shift2eu/.github/workflows/release.yml@refs/tags/$TAG" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-trigger push

# The build provenance (SLSA), then the SBOM: from release.yml, for this tag, on GitHub's runners.
gh attestation verify "oci://$IMAGE" --repo timothymamo/shift2eu \
  --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
  --source-ref "refs/tags/$TAG" --deny-self-hosted-runners
gh attestation verify "oci://$IMAGE" --repo timothymamo/shift2eu \
  --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
  --source-ref "refs/tags/$TAG" --deny-self-hosted-runners \
  --predicate-type https://spdx.dev/Document/v2.3

docker run --rm "$IMAGE" version
```

Inside the project's Lima VM, which has rootless nerdctl and no Docker
([ADR 0017](../adr/0017-run-a-rootless-container-runtime-in-the-lima-vm.md)), resolve the digest with
[crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane), which reads only the
index and pulls nothing, and run with `nerdctl`; the `cosign` and `gh` commands are the same:

```bash
# Replaces the DIGEST= line and the final `docker run` line above; TAG, IMAGE, cosign and gh stay as they are.
DIGEST="$(crane digest "ghcr.io/timothymamo/shift2eu:$TAG")"
nerdctl run --rm --read-only --network none "$IMAGE" version
```

Each platform's image is signed too, so `cosign verify` also accepts the `linux/amd64` or
`linux/arm64` digest that `docker buildx imagetools inspect` lists. `gh attestation verify ...
--format json` prints the attested statements, the SBOM's packages among them. The SBOM is attested
on the index digest but generated from its `linux/amd64` image; the `linux/arm64` image is built from
the same source, Go modules and distroless base (`crane digest --platform linux/arm64 ...` prints one
platform's digest). The image's labels
name the source repository (`org.opencontainers.image.source`) and the commit
(`org.opencontainers.image.revision`).

## Verify an archive

The release archives, `shift2eu_*` ([Install](install.md#the-release-archives)), are listed in one
`checksums.txt`, published with the release. Each archive and `checksums.txt` itself carry GitHub
build provenance, attested by the release workflow for its tag, before the release is published
(issue #163). The digest check alone detects a
download corrupted in transit or on a mirror; the attestation adds who built the file: `release.yml`,
run on a push of exactly that tag, from that commit, on GitHub's runners. Whoever could replace an
archive and its `checksums.txt` cannot make that attestation. The release is published only if it
holds exactly `checksums.txt` and the files it lists, each with the digest it records, and the
release published is the draft that was checked. The release workflow's `verify-archives` job then
runs these checks on every asset of every release, on a fresh runner with read-only permissions, and
fails the release when one fails. It also checks that GitHub's "Latest" release, the one
`releases/latest/download/...` serves, is the highest version released, the release the image's
`latest` tag names: a patch to an older line is published without becoming "Latest", and a
pre-release never becomes it. The release decides that once per run, in its `latest` job, and both
the image's `latest` tag and GitHub's "Latest" follow that one decision, so they cannot disagree
within a run; `verify-archives`, like `verify` for the image, reads the highest release tag again
on its own, as an independent check. A release tag on `main` whose release is never published (a
failed run, or a queued run replaced by a newer one) holds both back on the previous release until
it is published or the tag is deleted; the check then passes with a notice naming that tag. Check an
archive, with `TAG` set to the release you want:

```bash
TAG=vX.Y.Z
VERSION="${TAG#v}"
ARCHIVE="shift2eu_${VERSION}_linux_amd64.tar.gz"

gh release download "$TAG" --repo timothymamo/shift2eu --pattern "$ARCHIVE" --pattern checksums.txt
awk -v a="$ARCHIVE" '$2 == a' checksums.txt > "$ARCHIVE.sha256"
sha256sum --check "$ARCHIVE.sha256"   # macOS: shasum -a 256 --check "$ARCHIVE.sha256"

# The build provenance (SLSA): from release.yml, for this tag, on GitHub's runners.
gh attestation verify "$ARCHIVE" --repo timothymamo/shift2eu \
  --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
  --source-ref "refs/tags/$TAG" --deny-self-hosted-runners
tar -xzf "$ARCHIVE"
```

`awk` copies the line of `checksums.txt` whose name is exactly the archive's, and the check must print
`<archive>: OK`. A wrong digest prints `FAILED` and exits non-zero; a name missing from `checksums.txt`
leaves the copied file empty, which fails the check too (non-zero exit, no `OK`) instead of checking
nothing. `gh attestation verify` must print `✓ Verification succeeded!` and exits non-zero otherwise.
It works the same on macOS. `gh attestation verify checksums.txt ...`, with the same flags, verifies
`checksums.txt` itself.

On Windows, in PowerShell, for a `.zip` archive:

```powershell
$Tag = "vX.Y.Z"
$Archive = "shift2eu_$($Tag.TrimStart('v'))_windows_amd64.zip"
gh release download $Tag --repo timothymamo/shift2eu --pattern $Archive --pattern checksums.txt
$Want = @(Get-Content checksums.txt | Where-Object { $_.EndsWith("  $Archive") })
if ($Want.Count -ne 1) { throw "$Archive is not in checksums.txt exactly once" }
if ((Get-FileHash -Algorithm SHA256 $Archive).Hash -ne $Want[0].Split(' ')[0]) { throw "$Archive does not match checksums.txt" }   # -ne ignores case
gh attestation verify $Archive --repo timothymamo/shift2eu `
  --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml `
  --source-ref "refs/tags/$Tag" --deny-self-hosted-runners
if ($LASTEXITCODE -ne 0) { throw "$Archive has no build provenance from release.yml for $Tag" }
Expand-Archive $Archive
```

Each archive holds only the `shift2eu` binary (`shift2eu.exe` on Windows), with `README.md`,
`LICENSE` and the Markdown pages under `docs/`. Releases that still shipped the HTTP API's
`shift2eu-server_*` archives and `shift2eu-server_openapi.json` list them in the same `checksums.txt`:
check them the same way, by their names. The archives' attestations start with the first release after `v0.3.0`: for `v0.3.0` and earlier,
only the digest check applies, and `checksums.txt` comes from the same release as the archive, so it
cannot tell a compromised release from a sound one; run the signed image instead where that matters.

## Releases before the rename: v0.1.0 and v0.1.0-rc.1

The project was called `eu-sov-cloud` until v0.2.0
([ADR 0020](../adr/0020-rename-the-project-to-shift2eu.md)). `v0.1.0` and `v0.1.0-rc.1` keep the
names they were published with: binaries `eusov_*`, image `ghcr.io/timothymamo/eusov`, and the
signing identity of the repository's old name. Verify them with the commands above, with these
changes:

```bash
TAG=v0.1.0   # or v0.1.0-rc.1
DIGEST="$(docker buildx imagetools inspect "ghcr.io/timothymamo/eusov:$TAG" --format '{{.Manifest.Digest}}')"
IMAGE="ghcr.io/timothymamo/eusov@$DIGEST"

cosign verify "$IMAGE" \
  --certificate-identity "https://github.com/timothymamo/eu-sov-cloud/.github/workflows/release.yml@refs/tags/$TAG" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-trigger push

gh attestation verify "oci://$IMAGE" --repo timothymamo/eu-sov-cloud \
  --signer-workflow timothymamo/eu-sov-cloud/.github/workflows/release.yml \
  --source-ref "refs/tags/$TAG" --deny-self-hosted-runners
```

Add `--predicate-type https://spdx.dev/Document/v2.3` to the `gh attestation verify` line for the
SBOM, as above. GitHub redirects the old repository name to the new one.

# Verify a release

Every published image is signed and carries build provenance and an SBOM, and every release archive
carries build provenance. This page shows how to check all three for an image before you run it, inside
the project's Lima VM too, how to check a release archive against its checksums and its provenance, how
to check the live site, https://www.shift2eu.com, against its release's site bundle, and how to verify
the releases published before the project's rename.

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
sha256sum --check "$ARCHIVE.sha256"

# The build provenance (SLSA): from release.yml, for this tag, on GitHub's runners.
gh attestation verify "$ARCHIVE" --repo timothymamo/shift2eu \
  --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
  --source-ref "refs/tags/$TAG" --deny-self-hosted-runners
tar -xzf "$ARCHIVE"
```

`awk` copies the line of `checksums.txt` whose name is exactly the archive's, and the check must print
`<archive>: OK`; on macOS, run `shasum -a 256 --check "$ARCHIVE.sha256"` instead of `sha256sum`. A
wrong digest prints `FAILED` and exits non-zero; a name missing from `checksums.txt` leaves the copied
file empty, which fails the check too (non-zero exit, no `OK`) instead of checking nothing.
`gh attestation verify` must print `✓ Verification succeeded!` and exits non-zero otherwise.
It works the same on macOS. `gh attestation verify checksums.txt ...`, with the same flags, verifies
`checksums.txt` itself.

The release's site bundle, `shift2eu-site_<tag>.tar.gz` (for example `shift2eu-site_vX.Y.Z.tar.gz`), is
one more asset of the same release, listed in the same `checksums.txt` and covered by the same
attestation: check it with the commands above, with `ARCHIVE="shift2eu-site_$TAG.tar.gz"`. The site
bundle starts with the first release after `v0.12.0`: `v0.12.0` and earlier have none.
[Verify the site](#verify-the-site) uses it.

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
`LICENSE`, `NOTICE`, `SECURITY.md`, the Markdown pages under `docs/`, the jq programs
[Build an inventory](inventory.md) runs and the sample inventories under `examples/`
([Install](install.md#the-release-archives)). Older releases list the retired HTTP API's assets
([Install](install.md)) in the same `checksums.txt`: check them the same way, by their names. The archives'
attestations start with the first release after `v0.3.0`: for `v0.3.0` and earlier, only the digest
check applies, and `checksums.txt` comes from the same release as the archive, so it cannot tell a
compromised release from a sound one; run the signed image instead where that matters.

## Verify the site

The site, https://www.shift2eu.com, runs the assessment in your browser from the files of one release's
site bundle. Every release builds that bundle, `shift2eu-site_<tag>.tar.gz`, publishes it as a release
asset and attests it with GitHub build provenance (above), and the bundle's `manifest.json` lists every
other file in it with its SHA-256. The site's [verification page](https://www.shift2eu.com/verify.html)
summarises this section.

**Type the address yourself.** Type https://www.shift2eu.com into the address bar rather than following
an `http://` link: on a first visit, before your browser has seen the site's HSTS header, a network
attacker can strip HTTPS from an `http://` link and serve a look-alike page. This guide is served by
GitHub over HTTPS, so it carries the advice where that attacker cannot replace it. If the site's
verification page says that HSTS is waived, the same risk covers returning visits too: type the address
every time, or run the bundle yourself (below).

Every page shows the release it was built from, in its footer (and on the verification page), such as
`vX.Y.Z`: that is the release to check. The live site is up from the site's first deploy. Check the live
site against that release, with `TAG` set to its version.

Paste the block into your shell as it is, bash or zsh. Its first two lines set `TAG` and `D`, a new, empty
directory, in your shell. The check itself is one command, a subshell from `(` to `)`, which your shell
reads whole before it runs any of it: `set -euo pipefail` stops the subshell at the first command that
fails, so nothing after a failed step runs, and its options and its `cd` end with it, so your shell keeps
its own. The check works only in `$D`, so no file from an earlier run can stand in for one this run did
not fetch. In order, it downloads the release's bundle; verifies its build provenance (SLSA: from
`release.yml`, for this tag, on GitHub's runners); unpacks it; checks that its manifest names `$TAG`, so
the bundle is the release you meant; turns the manifest into a list of digests, refusing a path that could
leave `live/` (defence in depth: the manifest is attested); fetches every file it lists from the live site,
over HTTPS only; and compares each file with its digest. The block holds no comment lines, since an
interactive zsh runs a line starting with `#` as a command. On a Mac without `sha256sum`, write
`shasum -a 256` in its place.

```bash
TAG=vX.Y.Z
D="$(mktemp -d)"; echo "$D"
( set -euo pipefail
  cd "$D"
  BUNDLE="shift2eu-site_$TAG.tar.gz"
  gh release download "$TAG" --repo timothymamo/shift2eu --pattern "$BUNDLE"
  gh attestation verify "$BUNDLE" --repo timothymamo/shift2eu \
    --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
    --source-ref "refs/tags/$TAG" --deny-self-hosted-runners
  mkdir site
  tar -xzf "$BUNDLE" -C site
  test "$(jq -r .version site/manifest.json)" = "$TAG" || { echo "FAIL: the bundle's manifest is not $TAG's" >&2; false; }
  jq -r '.files[]
    | if (.path | test("^[A-Za-z0-9._/-]+$") and (contains("..") | not) and (startswith("/") | not))
      then "\(.sha256)  \(.path)" else error("unsafe path in manifest.json: \(.path)") end' \
    site/manifest.json > manifest.sha256
  mkdir live
  while read -r sum f; do
    mkdir -p "live/$(dirname "$f")"
    curl --fail --silent --show-error --proto '=https' --compressed \
      --output "live/$f" "https://www.shift2eu.com/$f"
  done < manifest.sha256
  cd live
  sha256sum --check ../manifest.sha256
); [ "$?" -eq 0 ] && echo "PASS: every file matches $TAG's manifest" || echo "FAIL: stopped at the first failed step"
```

`gh attestation verify` must print `✓ Verification succeeded!`, the check must print `OK` for every
file, and the block's last line `PASS`. The subshell's status is read after it, not by an `&&` on the
subshell itself: a shell ignores `set -e` inside a command whose status an `&&` or `||` tests, so the
check would not stop at a failed step. The command leaves out `--source-digest` on purpose: the release runs only on a pushed `v*.*.*` tag,
a ruleset guards who may create one, and `--signer-workflow`, `--source-ref "refs/tags/$TAG"` and
`--deny-self-hosted-runners` bind the bundle to that tag's run; a commit digest read from the same GitHub
would add nothing. With a clone you trust, you can bind it to a commit as well:
`C="$(git rev-list -n1 "$TAG")"`, then `git merge-base --is-ancestor "$C" origin/main`, and add
`--source-digest "$C"` to the command.

`--compressed` asks for, and decodes, the compressed form the site serves its largest script in:
the manifest's digests are of the files themselves. A `FAILED` line, or a file `curl` cannot fetch, means
the live site does not serve that release's files; report it as the repository's
[security policy](../../SECURITY.md) says. The address `https://www.shift2eu.com/` serves `index.html`.
Check it in the same shell, where `$D` is still set: the commands read the bundle's manifest from `$D`.

```bash
cd "${D:?run the site check first}" && \
  curl --fail --silent --show-error --proto '=https' --compressed https://www.shift2eu.com/ | sha256sum && \
  jq -r '.files[] | select(.path == "index.html") | .sha256' site/manifest.json
```

The first line must print the digest the second prints. A first digest of `e3b0c442...`, the digest of
empty input, means `curl` failed (its error is on stderr), not that the site serves another page.
The release that is the highest release tag on `main` checks the live site after it deploys it, once
the maintainer has switched the deploy on, and its check does more than this one: it also checks the
root, the service worker's paths, the redirects, the certificate and HSTS.

What the check shows, and what it does not:

- It is one point in time from one vantage point. The HTML is the root of trust and nothing pins it: a
  page served to someone else, at another time or through another network, may differ from the one you
  checked.
- A released page from a poisoned cache can stay in a browser's cache for up to its `max-age`
  (300 seconds) after the site is repaired. A page uploaded with a leaked key sets its own caching (up
  to a year) and can register a service worker, both of which can outlast the repair in a visitor's
  browser.
- It compares only the files the manifest names: a path outside the manifests, such as a look-alike page
  at a new path, is not checked.

**Run the bundle yourself.** The bundle runs unchanged on any origin. Serve the verified, extracted
bundle with any static file server on your own machine and open it in your browser: you run exactly
the attested files, and the site's host sees nothing. For example, in the same shell, from the directory
the check above worked in (`$D`), then open `http://localhost:8000/`:

```bash
python3 -m http.server 8000 --bind localhost --directory "$D/site"
```

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

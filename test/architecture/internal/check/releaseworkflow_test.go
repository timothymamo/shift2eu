package check_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// The fixture below is .github/workflows/release.yml in parts: the top level,
// then the twelve jobs, each with the comments and keys above its steps
// (releaseJob.head) and its steps in order. Rendering it rebuilds the file byte
// for byte, so a case changes one part and leaves the rest exactly as the
// release has it, comments included: the check must tolerate a full-line
// comment above a job, between two steps, inside a with: mapping and on a
// value.

// releaseWorkflowTop is .github/workflows/release.yml up to its jobs:.
const releaseWorkflowTop = `name: Release

on:
  push:
    tags: ["v*.*.*"]

# No permission by default; each job asks for its own.
permissions: {}

# One release at a time, across every tag: two tags pushed together would
# otherwise race to move latest, and the run that finished last would win
# whichever tag it carried. A run in progress is never cancelled, so no
# release stops half published. GitHub keeps one pending run per group: a
# waiting run may be replaced by a newer tag's, and must then be re-run by
# hand (ADR 0015).
concurrency:
  group: release
  cancel-in-progress: false

# The steps of all sixteen jobs are a closed list that test/architecture holds
# (check.ReleaseWorkflow): add no step, run: or env: here without changing it.
# The release ships the CLI alone: the HTTP API's image, archive and API
# document are gone (issue #393), and the site's bundle is published beside the
# CLI's archives (PRD #297 milestone 3). latest alone decides, once
# per run, whether the tag is the
# highest release tag on main, so the image's latest tag and GitHub's "Latest"
# release cannot disagree. goreleaser hands the archives on, attest-archives
# attests them, publish alone publishes the draft release, and verify-archives
# checks what it published (issues #156, #163, #173). site-programs builds the
# deploy program and the live check, and site-deploy is the one job that holds
# the site's key: it deploys the published bundle of the highest release tag
# only while SITE_DEPLOY_ENABLED is true, and site-check checks the live site
# after it (PRD #297 milestone 3, ADR 0042).
jobs:
`

// testJobHead is the test job up to its steps:.
const testJobHead = `  test:
    name: Test
    runs-on: ubuntu-latest
    timeout-minutes: 20
    permissions:
      contents: read
    steps:
`

// testStepTemplates are the test job's steps: the whole history with every branch, the
// guard that the tagged commit is on main (ADR 0015), setup-go without the
// Actions cache, and make test.
var testStepTemplates = []string{
	`      # The whole history, with every branch: the guard below needs origin/main.
      - uses: {{pin actions/checkout}}
        with:
          fetch-depth: 0
          persist-credentials: false
`,
	`      # Only a commit on main is released (ADR 0015). Every other job needs
      # this one, so a tag on any other commit publishes nothing.
      - name: The tagged commit is on main
        run: |
          git rev-parse --verify --quiet refs/remotes/origin/main > /dev/null || {
            echo "origin/main is missing from the checkout" >&2
            exit 1
          }
          git merge-base --is-ancestor "$GITHUB_SHA" refs/remotes/origin/main || {
            echo "$GITHUB_SHA is not on main: nothing is released" >&2
            exit 1
          }
`,
	`      - uses: {{pin actions/setup-go}}
        with:
          go-version-file: go.mod
          cache: false # see the comment on the goreleaser job
`,
	`      - name: Test
        run: make test
`,
}

// latestJobHead is the latest job up to its steps: the one job of the run that
// decides whether the tag is the highest release tag on main, which the images'
// latest tag and GitHub's "Latest" release both follow (issue #173).
const latestJobHead = `  # Decides, once per run, whether the tag is the highest release tag on main:
  # the one decision the image's latest tag and GitHub's "Latest" release both
  # follow, so a tag pushed while the run is in flight cannot leave them
  # disagreeing (issue #173). Git, sed and sort alone, and read-only: no
  # repository code runs, the whole history brings origin/main, and the token
  # is not kept.
  latest:
    name: Latest
    needs: test
    runs-on: ubuntu-latest
    timeout-minutes: 5
    permissions:
      contents: read # check out the repository
    outputs:
      latest: ${{ steps.decide.outputs.latest }}
    steps:
`

// latestStepTemplates are the latest job's steps: the whole history, and the one step
// that reads the release tags merged into main and writes latest, true only for
// the highest of them.
var latestStepTemplates = []string{
	`      # The whole history, with every branch: the decision below needs
      # origin/main. No repository code runs here, so the token is not kept.
      - uses: {{pin actions/checkout}}
        with:
          fetch-depth: 0
          persist-credentials: false
`,
	`      # The release tags merged into main, pre-releases left out, by version
      # order: only the highest of them moves the image's latest tag and
      # GitHub's "Latest" release, so a patch on an older line moves neither,
      # and a stray tag off main (one the guard refused and nobody deleted)
      # cannot hold them back. The step refuses a checkout without origin/main:
      # the run shell is bash -e without pipefail, so the pipeline would
      # otherwise leave highest empty and latest false without failing.
      - name: Decide whether the tag is the highest release tag on main
        id: decide
        env:
          TAG: ${{ github.ref_name }}
        run: |
          git rev-parse --verify --quiet refs/remotes/origin/main > /dev/null || {
            echo "origin/main is missing from the checkout" >&2
            exit 1
          }
          highest="$(git tag -l 'v*.*.*' --merged refs/remotes/origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V | tail -n 1)"
          latest=false
          if [ "$TAG" = "$highest" ]; then latest=true; fi
          echo "latest=$latest" >> "$GITHUB_OUTPUT"
`,
}

// siteJobHead is the site job up to its steps: the site's bundle, built at the
// tag on a runner of its own (PRD #297 milestone 3). It holds no key and no
// secret: its digest is a job output, which the jobs that smoke-test, attest
// and publish the bundle bind it to.
const siteJobHead = `  # The site's bundle, built at the tag on a runner of its own (PRD #297
  # milestone 3): no key, no secret and no OIDC token, so nothing in it can
  # publish or deploy. Its sha256 is a job output, which the smoke test, the
  # attestation and goreleaser bind the bundle they handle to.
  site:
    name: Site bundle
    needs: test
    runs-on: ubuntu-latest
    timeout-minutes: 15
    permissions:
      contents: read # check out the repository
    outputs:
      sha256: ${{ steps.digest.outputs.sha256 }}
    steps:
`

// siteStepTemplates are the site job's steps: the repository at the tag, Go
// without the Actions cache, make site, the bundle's digest as an output, and
// the bundle as a workflow artifact for the jobs of this run alone, kept a
// week as the archives are (PRD #297 milestone 3).
var siteStepTemplates = []string{
	`      # The tag's own commit. ci/sitebuild reads no history, so a shallow
      # checkout is enough, and the token is not kept.
      - uses: {{pin actions/checkout}}
        with:
          persist-credentials: false
`,
	`      - uses: {{pin actions/setup-go}}
        with:
          go-version-file: go.mod
          cache: false
`,
	`      # The bundle the release publishes, built exactly as make site builds
      # it: bin/site and the archive bin/shift2eu-site_<tag>.tar.gz. The tag
      # comes through env:, never pasted into the script.
      - name: Build the site's bundle
        env:
          SITE_VERSION: ${{ github.ref_name }}
        run: make site
`,
	`      # The digest of the one file the release publishes beside the CLI's
      # archives: the jobs after this one check what they handle against it.
      - name: The bundle's digest
        id: digest
        env:
          TAG: ${{ github.ref_name }}
        run: |
          set -euo pipefail
          sha256="$(sha256sum "bin/shift2eu-site_$TAG.tar.gz" | cut -d' ' -f1)"
          echo "sha256=$sha256" >> "$GITHUB_OUTPUT"
`,
	`      # The bundle for the jobs of this run alone: the published release is
      # what the deploy and the live check read, so a week is enough.
      - uses: {{pin actions/upload-artifact}}
        with:
          name: site
          path: bin/shift2eu-site_*.tar.gz
          if-no-files-found: error
          retention-days: 7
`,
}

// siteBrowserJobHead is the site-browser job up to its steps: the smoke test
// of the very bundle the site job built, checked against that job's digest and
// served from a directory of its own (PRD #297 milestone 3). No secret and no
// OIDC token.
const siteBrowserJobHead = `  # The smoke test of that exact bundle (PRD #297 milestone 3): the artifact
  # the site job uploaded, checked against that job's digest, unpacked, and
  # served to test/browser in the pinned headless Chromium. No secret and no
  # OIDC token, so nothing in it can publish or deploy.
  site-browser:
    name: Site browser smoke test
    needs: site
    runs-on: ubuntu-latest
    timeout-minutes: 30
    permissions:
      contents: read # check out the repository
    steps:
`

// siteBrowserStepTemplates are the site-browser job's steps: the repository,
// Go without the Actions cache, the artifact the site job uploaded, the check
// of it against that job's digest, and make test-browser on the unpacked
// bundle, which is the bundle the release attests and publishes.
var siteBrowserStepTemplates = []string{
	`      - uses: {{pin actions/checkout}}
        with:
          persist-credentials: false
`,
	`      - uses: {{pin actions/setup-go}}
        with:
          go-version-file: go.mod
          cache: false
`,
	`      - uses: {{pin actions/download-artifact}}
        with:
          name: site
          path: bin
`,
	`      # What is smoke-tested is what the site job built: the digest the site
      # job gave is compared before anything is unpacked. The tag comes through
      # env:, never pasted into the script.
      - name: Check and unpack the bundle
        env:
          TAG: ${{ github.ref_name }}
          SITE_SHA256: ${{ needs.site.outputs.sha256 }}
        run: |
          set -euo pipefail
          printf '%s  %s\n' "$SITE_SHA256" "bin/shift2eu-site_$TAG.tar.gz" | sha256sum -c -
          mkdir bin/release-bundle
          tar -xzf "bin/shift2eu-site_$TAG.tar.gz" -C bin/release-bundle
`,
	`      # The bundle the release publishes, not one built again: plain make
      # test-browser would build its own with ci/sitebuild and test something
      # the release never attested.
      - name: Smoke test the release's bundle in a headless Chromium
        run: make test-browser TEST_BROWSER_BUNDLE=bin/release-bundle
`,
}

// goreleaserJobHead is the goreleaser job up to its steps:.
const goreleaserJobHead = `  # A job of its own, on a fresh runner: no go env file and no environment or
  # PATH that the tests set reaches goreleaser. setup-go's Actions cache
  # (module and build cache) would carry state over from the test job or from
  # CI on main, so both jobs set cache: false.
  goreleaser:
    name: GoReleaser
    needs: [test, site]
    runs-on: ubuntu-latest
    timeout-minutes: 15
    permissions:
      contents: write # create the GitHub release and upload artifacts
    steps:
`

// goreleaserStepTemplates are the goreleaser job's steps: no repository code runs
// before goreleaser, which is asked for exactly release --clean, and the
// archives and their checksums leave the job as a workflow artifact for the
// attest job to attest on a runner of its own (issue #163). Nothing is copied
// into dist/ beside them: the release publishes the CLI's archives alone
// (issue #393).
var goreleaserStepTemplates = []string{
	`      - uses: {{pin actions/checkout}}
        with:
          fetch-depth: 0
          persist-credentials: false
`,
	`      - uses: {{pin actions/setup-go}}
        with:
          go-version-file: go.mod
          cache: false
`,
	`      # The bundle the site job built, of this run: bin/ is git-ignored, so
      # GoReleaser's dirty-tree check does not trip on it.
      - uses: {{pin actions/download-artifact}}
        with:
          name: site
          path: bin
`,
	`      # The bundle GoReleaser lists in checksums.txt and uploads to the draft
      # is the bundle the site job built and the smoke test tested: a file
      # changed on its way through the workflow artifact fails here, before
      # GoReleaser runs.
      - name: Check the site bundle against the site job's digest
        env:
          TAG: ${{ github.ref_name }}
          SITE_SHA256: ${{ needs.site.outputs.sha256 }}
        run: printf '%s  %s\n' "$SITE_SHA256" "bin/shift2eu-site_$TAG.tar.gz" | sha256sum -c -
`,
	`      - uses: {{pin goreleaser/goreleaser-action}}
        with:
          distribution: goreleaser
          version: "{{release goreleaser}}" # exact pin; Renovate updates it (github-actions manager)
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
`,
	`      # The archives and their checksums leave the job as
      # a workflow artifact, for the attest job to attest on a runner of its
      # own (issue #163). What is handed on is what this run wrote in
      # dist/ and nothing else. The archives are named by their extension, not
      # by their name alone: dist/shift2eu_* also matches GoReleaser's build
      # directories (dist/shift2eu_linux_amd64_v1/ ...), and would hand on 12
      # bare binaries beside the archives. It is kept a week, so a failed
      # attestation or publication is recovered by re-running the failed jobs.
      - uses: {{pin actions/upload-artifact}}
        with:
          name: archives
          path: |
            dist/shift2eu_*.tar.gz
            dist/shift2eu_*.zip
            dist/checksums.txt
          if-no-files-found: error
          retention-days: 7
`,
}

// attestArchivesJobHead is the attest-archives job up to its steps: the
// archives and checksums.txt attested with GitHub build provenance (issue
// #163), before the release is published. It holds an OIDC token to sign with
// and the repository's attestations to write, and reads the repository: no
// contents: write, so nothing in it can publish or replace the release.
const attestArchivesJobHead = `  # Attests every archive and checksums.txt with GitHub build provenance, from
  # this workflow on the tag (issue #163), before the release is published: an
  # immutable release locks its assets when it is published, so nothing may be
  # added to it afterwards. Least privilege: an OIDC token to sign with, the
  # repository's attestations to write, and read-only access to the repository.
  attest-archives:
    name: Attest the archives
    needs: [goreleaser, site]
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      id-token: write # the OIDC token the provenance is signed with
      attestations: write # store the provenance in the repository
      contents: read # download the archives' workflow artifact
    steps:
`

// attestArchivesStepTemplates are the attest-archives job's steps: GitHub's own
// download of the archives' workflow artifact, the check of the archives
// against checksums.txt, and one attestation covering every archive and
// checksums.txt itself (issue #163).
var attestArchivesStepTemplates = []string{
	`      - uses: {{pin actions/download-artifact}}
        with:
          name: archives
`,
	`      # The site's bundle, downloaded beside the archives: the release
      # carries it as one more asset, listed in the same checksums.txt.
      - uses: {{pin actions/download-artifact}}
        with:
          name: site
`,
	`      # What is attested is what GoReleaser wrote, the site's bundle included:
      # a file changed on the way through a workflow artifact fails here,
      # before anything is signed. checksums.txt is held to exactly one
      # site bundle, and to the digest the site job gave it, so a GoReleaser
      # run whose extra_files matched nothing fails here too.
      - name: Check the archives and the site bundle against checksums.txt
        env:
          TAG: ${{ github.ref_name }}
          SITE_SHA256: ${{ needs.site.outputs.sha256 }}
        run: |
          set -euo pipefail
          sha256sum -c checksums.txt
          grep -qxF "$SITE_SHA256  shift2eu-site_$TAG.tar.gz" checksums.txt
          test "$(grep -cF '  shift2eu-site_' checksums.txt)" = 1
`,
	`      # One attestation covers every archive, the site's bundle and their
      # checksums, so a release whose files were replaced fails verification
      # (issue #163). The archives are subjects named by their extension: a
      # bare shift2eu_* also matches GoReleaser's build directories, and would
      # attest the 12 bare binaries beside the archives.
      - name: Attest the archives
        uses: {{pin actions/attest-build-provenance}}
        with:
          subject-path: |
            shift2eu_*.tar.gz
            shift2eu_*.zip
            shift2eu-site_*.tar.gz
            checksums.txt
`,
}

// publishJobHead is the publish job up to its steps: the one job that publishes
// the release, after everything is uploaded and attested, holding contents:
// write and nothing else (issue #156).
const publishJobHead = `  # The release is published last (issue #156): GoReleaser uploads every asset
  # to a draft, so nothing can be added to or replaced in the release once it is
  # public, and this job alone publishes it, after the archives are attested.
  # It reads the draft before it publishes it, and holds contents: write and
  # nothing else: nothing in it can sign, push or attest. It checks nothing out
  # and runs no repository code: whether the tag is the highest release tag on
  # main is the latest job's one decision, by which it moves GitHub's "Latest"
  # release, as the image's latest tag follows it (issue #173).
  publish:
    name: Publish the release
    needs: [test, goreleaser, attest-archives, latest, site-browser]
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      contents: write # publish the draft release
    steps:
`

// publishStepTemplates are the publish job's steps (issues #156, #163, #173): the
// attested checksums.txt out of the archives workflow artifact, and the one step
// that checks the draft release's assets against it and publishes exactly that
// draft, by the id the API listed it with, with GitHub's "Latest" release moved
// by the latest job's decision alone, through env: and never pasted into the
// script. While that decision is true, the step first refuses a "Latest"
// release that is already higher than the tag: only a partial re-run of this
// run, which does not re-run the latest job, can hand it such a stale decision
// (issue #173). The tag, that decision and the token come through env:, and
// every gh command is given the repository in its own arguments, so none reads
// it from a git remote. No checkout: this job runs no repository code.
var publishStepTemplates = []string{
	`      # The checksums.txt the attest-archives job attested, from the workflow
      # artifact it attests (issue #156): what is published must be exactly what
      # was attested, and this file is the list of it.
      - uses: {{pin actions/download-artifact}}
        with:
          name: archives
`,
	`      # An immutable release locks its assets when it is published, so the
      # draft is read before it is published, and the release published is the
      # one that was checked (issues #156, #163): the draft's asset names must
      # be exactly the names checksums.txt lists plus checksums.txt itself, each
      # asset's digest must be the one that file records, and its checksums.txt
      # must be the very file this run attested. A draft that holds anything
      # else is never published. A draft is not readable by its tag, so the
      # API's list of the releases is read, and the draft is published by the id
      # that list gives it, in this one step: publishing by tag would let gh
      # resolve a published release, or a swapped draft, in its place. GitHub's
      # "Latest" release is moved by the latest job's one decision, and nothing
      # but true or false publishes; a decision that is already stale, made by a
      # first attempt while "Latest" has since moved to a higher release, takes
      # nothing over (issue #173).
      - name: Check the draft's assets and publish it
        env:
          TAG: ${{ github.ref_name }}
          LATEST: ${{ needs.latest.outputs.latest }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          # Whether this tag is the highest release tag on main is the latest
          # job's decision, made once for this release: the
          # step refuses anything but the two values it can hold, rather than
          # publish a release with a make_latest nobody decided (issue #173).
          case "$LATEST" in
            true|false) ;;
            *)
              echo "latest is '$LATEST', want true or false" >&2
              exit 1
              ;;
          esac
` + publishStaleDecisionCheck + `          tmp="$(mktemp -d)"
          # A draft release is not readable by its tag. The API lists it among
          # the releases, with draft: true, and gives every asset the digest of
          # its bytes. --paginate --slurp hands the pages over as one array of
          # pages, which add joins into one list of releases.
          gh api --paginate --slurp "repos/timothymamo/shift2eu/releases" > "$tmp/releases.json"
          # A tag that already has a published release is refused rather than
          # checked: publishing it again needs a human.
          if [ "$(jq --arg tag "$TAG" 'add | map(select(.draft == false and .tag_name == $tag)) | length' "$tmp/releases.json")" != 0 ]; then
            echo "repos/timothymamo/shift2eu already publishes a release for $TAG" >&2
            exit 1
          fi
          if [ "$(jq --arg tag "$TAG" 'add | map(select(.draft and .tag_name == $tag)) | length' "$tmp/releases.json")" != 1 ]; then
            echo "repos/timothymamo/shift2eu holds no single draft release for $TAG" >&2
            exit 1
          fi
          # The draft is published by the id this response gives it, and never
          # by the tag again: gh resolves a tag to a published release before a
          # draft, so a release published, or a draft swapped, after the checks
          # below would be published in the place of the one that was checked.
          id="$(jq --arg tag "$TAG" -r 'add | map(select(.draft and .tag_name == $tag)) | .[0].id' "$tmp/releases.json")"
          # The names checksums.txt lists, and checksums.txt itself: what that
          # draft must hold exactly, so nothing is published that the
          # attestation never covered.
          {
            sed -e 's/^[0-9a-f]\{64\}  //' checksums.txt
            echo checksums.txt
          } | sort > "$tmp/attested.txt"
          jq --argjson id "$id" -r 'add | map(select(.id == $id)) | .[0].assets[] | .name' "$tmp/releases.json" | sort > "$tmp/published.txt"
          if ! cmp -s "$tmp/attested.txt" "$tmp/published.txt"; then
            echo "the draft release for $TAG does not hold what checksums.txt lists:" >&2
            diff -u "$tmp/attested.txt" "$tmp/published.txt" >&2 || true
            exit 1
          fi
          # Every asset's digest is the one checksums.txt lists: the bytes the
          # attestation covers are the bytes that are published.
          while read -r hash name; do printf 'sha256:%s %s\n' "$hash" "$name"; done < checksums.txt | sort > "$tmp/attested-digests.txt"
          jq --argjson id "$id" -r 'add | map(select(.id == $id)) | .[0].assets[] | select(.name != "checksums.txt") | "\(.digest) \(.name)"' "$tmp/releases.json" | sort > "$tmp/published-digests.txt"
          if ! cmp -s "$tmp/attested-digests.txt" "$tmp/published-digests.txt"; then
            echo "the draft release for $TAG holds an asset whose digest checksums.txt does not list:" >&2
            diff -u "$tmp/attested-digests.txt" "$tmp/published-digests.txt" >&2 || true
            exit 1
          fi
          # checksums.txt is attested too: the draft's copy must be the very
          # file this run attested, not one written after the attestation.
          if [ "$(jq --argjson id "$id" -r 'add | map(select(.id == $id)) | .[0].assets[] | select(.name == "checksums.txt") | .digest' "$tmp/releases.json")" != "sha256:$(sha256sum checksums.txt | cut -d' ' -f1)" ]; then
            echo "the draft release's checksums.txt is not the one this run attested" >&2
            exit 1
          fi
          # The draft this job read and checked, and no other, published with
          # GitHub's "Latest" release moved by the latest job's one decision:
          # true only for the highest release tag on main, so a patch on an
          # older line is published without taking it over (issue #173).
          gh api -X PATCH "repos/timothymamo/shift2eu/releases/$id" -F draft=false -f make_latest="$LATEST"
`,
}

// draftCheckBlock is the check that the release is published at all, which the
// deploy job's check step and the live check's hold before they read anything
// from the release's bundle: a draft's assets are not what was attested, and a
// deploy or a check of them would read a bundle the release never published.
const draftCheckBlock = `          if [ "$(gh release view "$TAG" --repo timothymamo/shift2eu --json isDraft --jq '.isDraft')" != false ]; then
            echo "the release of $TAG is still a draft, so its bundle was never published" >&2
            exit 1
          fi
`

// deployBundleDownload is the deploy job's download of the published bundle, the
// line the draft check must come before.
const deployBundleDownload = `          gh release download "$TAG" --repo timothymamo/shift2eu --pattern "shift2eu-site_$TAG.tar.gz"`

// publishByID is the command the publish job publishes with: the draft it just
// checked, by the id the API listed it with, and never by its tag, which gh
// resolves to a published release before a draft (issues #156, #163), with
// GitHub's "Latest" release moved by the latest job's one decision, so the
// release and its image's latest tag move together (issue #173).
const publishByID = `gh api -X PATCH "repos/timothymamo/shift2eu/releases/$id" -F draft=false -f make_latest="$LATEST"`

// publishByTagStep is the publication the second review of issue #163 refused:
// a step of its own, after the check, that resolves the tag again, so a release
// published for the tag, or a draft swapped in, would be published in the place
// of the one that was checked.
const publishByTagStep = `      - name: Publish the draft release
        env:
          TAG: ${{ github.ref_name }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: gh release edit "$TAG" --repo timothymamo/shift2eu --draft=false
`

// latestGuard is the latest job's refusal of a checkout without origin/main:
// the highest release tag is read in a pipeline, and the run shell has no
// pipefail, so without the guard a failed git would leave highest empty and
// latest false without failing the step.
const latestGuard = `          git rev-parse --verify --quiet refs/remotes/origin/main > /dev/null || {
            echo "origin/main is missing from the checkout" >&2
            exit 1
          }
`

// publishExistingReleaseCheck is the publish script's refusal of a tag that
// already holds a published release: a draft is checked and published only for
// a tag whose one release is that draft (auditor 1, issue #163).
const publishExistingReleaseCheck = `          if [ "$(jq --arg tag "$TAG" 'add | map(select(.draft == false and .tag_name == $tag)) | length' "$tmp/releases.json")" != 0 ]; then
            echo "repos/timothymamo/shift2eu already publishes a release for $TAG" >&2
            exit 1
          fi
`

// publishStaleDecisionCheck is the publish script's refusal, while the latest
// job's decision is true, to take GitHub's "Latest" release from a release
// higher than the tag (reviewer, issue #173): a partial re-run (gh run rerun
// --failed) does not re-run the successful latest job, so the decision its
// first attempt made can reach this step after a newer release has become
// "Latest". It runs before anything is read for the draft's check or
// published, so the release stays a draft, and only the API's 404, its answer
// when no release is latest yet, is no latest release: any other failed read
// fails the step, as verify-archives' latest_tag does.
const publishStaleDecisionCheck = `          # A partial re-run of this release run (gh run rerun --failed) does
          # not re-run the successful latest job, so a decision made by that
          # job's first attempt can reach this step after GitHub's "Latest"
          # release has moved on: taking it over from a higher release is
          # refused, before anything is read for the check below or published,
          # and the release stays a draft (issue #173).
          if [ "$LATEST" = true ]; then
            err="$(mktemp)"
            if current="$(gh api "repos/timothymamo/shift2eu/releases/latest" --jq '.tag_name' 2> "$err")"; then
              rm -f "$err"
            elif grep -qF -e "HTTP 404" "$err"; then
              # The repository publishes no latest release yet: the first
              # release has nothing to take "Latest" over from.
              current=""
              rm -f "$err"
            else
              echo "GitHub's latest release could not be read:" >&2
              cat "$err" >&2
              rm -f "$err"
              exit 1
            fi
            if [ -n "$current" ] && [ "$current" != "$TAG" ] && [ "$(printf '%s\n%s\n' "$TAG" "$current" | sort -V | tail -n 1)" = "$current" ]; then
              echo "GitHub's latest release is $current, higher than $TAG: the latest decision is stale, re-run the whole release run rather than its failed jobs" >&2
              exit 1
            fi
          fi
`

// verifyArchivesJobHead is the verify-archives job up to its steps: the
// independent check of what the published release carries (issue #163) and of
// GitHub's "Latest" release (issue #173), on a fresh runner after publish, with
// read-only access to the release and to the repository's attestations, no
// OIDC token and no checkout.
const verifyArchivesJobHead = `  # A fresh runner that verifies what the published release carries (issue
  # #163): it downloads every asset of the release, checks that the release is
  # published and holds exactly checksums.txt and the files it lists, checks
  # their digests and verifies each of them against the build provenance this
  # workflow made for this tag and commit. It runs after publish, so it is the
  # job that checks GitHub's "Latest" release against the highest release tag
  # merged into main (issue #173): a pre-release is never that release, whatever
  # the highest is and whether its own release is published yet. Read-only:
  # contents: read and attestations: read, no OIDC token and nothing it can
  # write, and no checkout, so no repository code runs.
  verify-archives:
    name: Verify the published archives
    needs: publish
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      contents: read # download the release's assets
      attestations: read # gh attestation verify reads them through the repository's API
    steps:
`

// verifyArchivesStepTemplates are the verify-archives job's steps: every asset of the
// release, the check that it is published and holds exactly checksums.txt and
// the files that file lists, their digests, the provenance of every one of
// them (docs/guide/verify-releases.md), and the check of GitHub's "Latest"
// release: never a pre-release, and otherwise the highest release tag merged
// into main, the version the image's latest tag names (issue #173).
var verifyArchivesStepTemplates = []string{
	`      # Every asset of the release, not the patterns the workflow expects to
      # find: what is checked is what was published. gh is given the repository
      # with --repo here too, since no checkout runs and gh reads no repository
      # from GITHUB_REPOSITORY.
      - name: Download the published archives
        env:
          TAG: ${{ github.ref_name }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: gh release download "$TAG" --repo timothymamo/shift2eu
`,
	`      # docs/guide/verify-releases.md on this release: the release is
      # published, not a draft, and holds exactly checksums.txt and the files it
      # lists, whose digests are checked first; then the provenance of every one
      # of them, made by this workflow, for this tag and commit, on GitHub's
      # runners.
      - name: Verify the published archives
        env:
          TAG: ${{ github.ref_name }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          if [ "$(gh release view "$TAG" --repo timothymamo/shift2eu --json isDraft --jq '.isDraft')" != false ]; then
            echo "the release of $TAG is still a draft, so it was never published" >&2
            exit 1
          fi
          tmp="$(mktemp -d)"
          # The files checksums.txt lists, and checksums.txt itself: what the
          # release must hold exactly, so nothing is verified that the
          # release does not carry, and nothing is left unverified.
          {
            sed -e 's/^[0-9a-f]\{64\}  //' checksums.txt
            echo checksums.txt
          } | sort > "$tmp/attested.txt"
          # Every file the release holds, dotfiles included: ls -1 and the
          # shell's * both skip a name that starts with ".", which would let a
          # file hide from this check and from the loop after it. find, not
          # ls -A, so shellcheck has no SC2012 to raise (issue #177).
          find . -mindepth 1 -maxdepth 1 -printf '%P\n' | sort > "$tmp/downloaded.txt"
          if ! cmp -s "$tmp/attested.txt" "$tmp/downloaded.txt"; then
            echo "the release of $TAG does not hold what checksums.txt lists:" >&2
            diff -u "$tmp/attested.txt" "$tmp/downloaded.txt" >&2 || true
            exit 1
          fi
          sha256sum -c checksums.txt
          # dotglob makes * list the dotfiles too, so every downloaded file is
          # a subject and none is left unverified.
          shopt -s dotglob
          for subject in *; do
            gh attestation verify "$subject" --repo timothymamo/shift2eu \
              --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
              --source-ref "refs/tags/$TAG" --source-digest "$GITHUB_SHA" --deny-self-hosted-runners
          done
`,
	`      # GitHub's "Latest" release, which releases/latest/download/... serves, is
      # the most recently published release rather than the highest version, so
      # a patch on an older line would take it over (issue #173). It must name
      # the highest release tag merged into main, the version the images' latest
      # tag names: the same filter, sort and pick the image jobs' stamp makes,
      # read by ancestry here as the verify job reads it, from the public
      # repository into a bare repository of this job's own, so no checkout runs
      # and no repository code runs. A higher tag whose release is not published
      # yet has no release to check against, so a notice says so and this run
      # does not check it, as the image latest check does.
      - name: Check GitHub's latest release
        env:
          TAG: ${{ github.ref_name }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          git init -q tags
          git -C tags fetch -q --filter=tree:0 --no-tags https://github.com/timothymamo/shift2eu.git '+refs/heads/main:refs/remotes/origin/main' '+refs/tags/v*:refs/tags/v*'
          highest="$(git -C tags tag -l 'v*.*.*' --merged refs/remotes/origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V | tail -n 1)"
          if [ -z "$highest" ]; then
            echo "no release tag is merged into main, so GitHub's latest release cannot be checked" >&2
            exit 1
          fi
          # published reports whether the release of a tag is published: only
          # the API's 404, its answer for a tag it holds no release for (a draft
          # is not published and has none), is an unpublished release; any other
          # answer, a 5xx, a rate limit, a failed login or a missing gh, is
          # printed and fails the step.
          published() {
            err="$(mktemp)"
            if gh api "repos/timothymamo/shift2eu/releases/tags/$1" > /dev/null 2> "$err"; then
              rm -f "$err"
              return 0
            fi
            if grep -qF -e "HTTP 404" "$err"; then
              rm -f "$err"
              return 1
            fi
            echo "the release of $1 could not be read:" >&2
            cat "$err" >&2
            rm -f "$err"
            return 2
          }
          # latest_tag prints the tag GitHub calls its latest release, or
          # nothing when the repository publishes none.
          latest_tag() {
            err="$(mktemp)"
            if out="$(gh api "repos/timothymamo/shift2eu/releases/latest" --jq '.tag_name' 2> "$err")"; then
              printf '%s\n' "$out"
              rm -f "$err"
              return 0
            fi
            if grep -qF -e "HTTP 404" "$err"; then
              rm -f "$err"
              return 0
            fi
            echo "GitHub's latest release could not be read:" >&2
            cat "$err" >&2
            rm -f "$err"
            return 1
          }
          latest="$(latest_tag)"
          # A pre-release is never GitHub's "Latest" release, whatever the
          # highest release tag is, and without a condition: while the highest
          # release tag's own release is still unpublished the check below is
          # skipped with a notice, which must not let a pre-release through.
          case "$TAG" in
            *-*)
              if [ "$latest" = "$TAG" ]; then
                echo "GitHub's latest release is $TAG, a pre-release, and a pre-release is never the latest release" >&2
                exit 1
              fi
              ;;
          esac
          if [ "$highest" = "$TAG" ]; then
            if [ "$latest" != "$TAG" ]; then
              echo "GitHub's latest release is '$latest', not $TAG, the highest release tag" >&2
              exit 1
            fi
          else
            rc=0
            published "$highest" || rc=$?
            case "$rc" in
              0)
                if [ "$latest" = "$TAG" ]; then
                  echo "GitHub's latest release is $TAG, which is not the highest release tag, $highest" >&2
                  exit 1
                fi
                ;;
              1)
                echo "::notice::the release of $highest is not published yet, so this run does not check GitHub's latest release"
                ;;
              *)
                exit 1
                ;;
            esac
          fi
`,
}

// siteProgramsJobHead is the site-programs job up to its steps: the release's
// two site programs, built on a runner of its own (PRD #297 milestone 3). It
// holds no key, no secret and no OIDC token; their digests are job outputs,
// which the jobs that deploy and check the site bind the binaries they run to,
// and the artifacts are kept ninety days, so a deploy's re-run still finds
// them once the run that built them has expired.
const siteProgramsJobHead = `  # The release's two site programs, built on a runner of its own (PRD #297
  # milestone 3): no key, no secret and no OIDC token, so nothing in them can
  # deploy. Their sha256 are job outputs, which the jobs that deploy and check
  # the site bind the binaries they run to, and the artifacts are kept ninety
  # days: the deploy and the live check are re-run by re-running this job, never
  # the job that built the bundle.
  site-programs:
    name: Site programs
    needs: test
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      contents: read # check out the repository
    outputs:
      sitedeploy: ${{ steps.digests.outputs.sitedeploy }}
      sitecheck: ${{ steps.digests.outputs.sitecheck }}
    steps:
`

// siteProgramsStepTemplates are the site-programs job's steps: the repository
// at the tag, Go without the Actions cache, make site-programs, the two
// programs' digests as the job's outputs, and each program as an artifact of
// its own, so the deploy job downloads the deploy program alone and the live
// check the check alone (PRD #297 milestone 3).
var siteProgramsStepTemplates = []string{
	`      - uses: {{pin actions/checkout}}
        with:
          persist-credentials: false
`,
	`      - uses: {{pin actions/setup-go}}
        with:
          go-version-file: go.mod
          cache: false
`,
	`      # The two programs the deploy and the live check run, built exactly as
      # the release builds them: bin/sitedeploy and bin/sitecheck, static and
      # without build paths. The static build's environment lives in the
      # Makefile, never here.
      - name: Build the site's programs
        run: make site-programs
`,
	`      - name: The programs' digests
        id: digests
        run: |
          set -euo pipefail
          echo "sitedeploy=$(sha256sum bin/sitedeploy | cut -d' ' -f1)" >> "$GITHUB_OUTPUT"
          echo "sitecheck=$(sha256sum bin/sitecheck | cut -d' ' -f1)" >> "$GITHUB_OUTPUT"
`,
	`      - uses: {{pin actions/upload-artifact}}
        with:
          name: sitedeploy
          path: bin/sitedeploy
          if-no-files-found: error
          retention-days: 90
`,
	`      - uses: {{pin actions/upload-artifact}}
        with:
          name: sitecheck
          path: bin/sitecheck
          if-no-files-found: error
          retention-days: 90
`,
}

// siteDeployJobHead is the site-deploy job up to its steps: the one job of the
// release that holds the site's key (PRD #297 milestone 3, ADR 0042). It runs
// only when the tag is the highest release tag on main and the maintainer has
// set SITE_DEPLOY_ENABLED to true, behind the site environment and in a
// concurrency group of its own; it holds contents: read and attestations: read
// and nothing else, and checks nothing out.
const siteDeployJobHead = `  # The one job of the release that holds the site's key (PRD #297 milestone 3,
  # ADR 0042). It deploys the published bundle of the highest release tag on
  # main, and only while the maintainer has set SITE_DEPLOY_ENABLED to true:
  # behind the site environment, whose deployment branch policy admits tags
  # alone, and in a concurrency group of its own, so two deploys never
  # interleave and a deploy in progress is never cancelled. contents: read and
  # attestations: read are all it holds: it publishes nothing to the repository,
  # mints no OIDC token, and checks nothing out.
  site-deploy:
    name: Deploy the site
    needs: [latest, verify, verify-archives, attest-archives, site, site-browser, site-programs]
    if: needs.latest.outputs.latest == 'true' && vars.SITE_DEPLOY_ENABLED == 'true'
    runs-on: ubuntu-latest
    timeout-minutes: 15
    environment: site
    concurrency:
      group: site-deploy
      cancel-in-progress: false
    permissions:
      contents: read # download the published release's bundle
      attestations: read # gh attestation verify reads them through the repository's API
    steps:
`

// siteDeployStepTemplates are the site-deploy job's steps: the deploy program
// from the site-programs job, the published release's bundle checked against
// the site job's digest and its attestation and unpacked, the release tags on
// main that the deploy program checks the site's version against, the manifests
// of every higher release, and the deploy itself, the one step of the whole
// workflow that holds the site's key (PRD #297 milestone 3, ADR 0042).
var siteDeployStepTemplates = []string{
	`      - uses: {{pin actions/download-artifact}}
        with:
          name: sitedeploy
`,
	`      # The bundle comes from the published release, never from the artifact of
      # a run that may have expired: the release must be published, and the
      # bundle is checked against the site job's digest and its attestation for
      # this workflow, this tag and this commit on GitHub's runners before
      # anything is unpacked. The program is checked against its own job's
      # digest. The tag comes through env:, never pasted into the script.
      - name: Check the site bundle and the deploy program
        env:
          TAG: ${{ github.ref_name }}
          SITE_SHA256: ${{ needs.site.outputs.sha256 }}
          SITEDEPLOY_SHA256: ${{ needs.site-programs.outputs.sitedeploy }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          if [ "$(gh release view "$TAG" --repo timothymamo/shift2eu --json isDraft --jq '.isDraft')" != false ]; then
            echo "the release of $TAG is still a draft, so its bundle was never published" >&2
            exit 1
          fi
          gh release download "$TAG" --repo timothymamo/shift2eu --pattern "shift2eu-site_$TAG.tar.gz"
          printf '%s  %s\n%s  %s\n' "$SITE_SHA256" "shift2eu-site_$TAG.tar.gz" "$SITEDEPLOY_SHA256" sitedeploy | sha256sum -c -
          gh attestation verify "shift2eu-site_$TAG.tar.gz" --repo timothymamo/shift2eu \
            --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
            --source-ref "refs/tags/$TAG" --source-digest "$GITHUB_SHA" --deny-self-hosted-runners
          mkdir bundle
          tar -xzf "shift2eu-site_$TAG.tar.gz" -C bundle
          chmod u+x sitedeploy
`,
	`      # The release tags on main, in the latest job's filter and sort, so the
      # deploy program compares the site's version with every release tag it
      # must not be older than, and this tag's own line is checked to be one of
      # them. The repository is read by a treeless fetch: no checkout runs. The
      # list holds exactly vX.Y.Z lines, as the deploy program requires.
      - name: The release tags on main
        env:
          TAG: ${{ github.ref_name }}
        run: |
          set -euo pipefail
          git init -q tags
          git -C tags fetch -q --filter=tree:0 --no-tags https://github.com/timothymamo/shift2eu.git '+refs/heads/main:refs/remotes/origin/main' '+refs/tags/v*:refs/tags/v*'
          git -C tags tag -l 'v*.*.*' --merged refs/remotes/origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V > releases.txt
          grep -qxF "$TAG" releases.txt || { echo "$TAG is not a release tag on main" >&2; exit 1; }
`,
	`      # Without this the deploy program could never refuse to roll the live
      # site back: it reads the manifest of every release higher than this tag
      # to know which site is live. The tags come from releases.txt alone,
      # never from a page or an API answer. A tag whose release is not published
      # yet (its own run is queued behind this one, so it deploys after this
      # bundle), a release that is still a draft, and a release without the
      # bundle each give a notice and the loop moves on; any other failed read
      # fails the step. A manifest is read only from the bundle that release
      # attested, for that tag, on GitHub's runners.
      - name: The higher releases' manifests
        env:
          TAG: ${{ github.ref_name }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          mkdir manifests higher
          mapfile -t tags < releases.txt
          for t in "${tags[@]}"; do
            if [ "$t" = "$TAG" ] || [ "$(printf '%s\n%s\n' "$TAG" "$t" | sort -V | tail -n 1)" != "$t" ]; then
              continue
            fi
            err="$(mktemp)"
            if ! gh api "repos/timothymamo/shift2eu/releases/tags/$t" > "$higher/release.json" 2> "$err"; then
              if grep -qF -e "HTTP 404" "$err"; then
                echo "::notice::the release of $t is not published yet, so its manifest is not read"
                rm -f "$err"
                continue
              fi
              echo "the release of $t could not be read:" >&2
              cat "$err" >&2
              rm -f "$err"
              exit 1
            fi
            rm -f "$err"
            if [ "$(jq -r '.draft' "$higher/release.json")" != false ]; then
              echo "::notice::the release of $t is still a draft, so its manifest is not read"
              continue
            fi
            if [ "$(jq --arg name "shift2eu-site_$t.tar.gz" -r '[.assets[].name] | map(select(. == $name)) | length' "$higher/release.json")" != 1 ]; then
              echo "::notice::the release of $t publishes no shift2eu-site_$t.tar.gz, so its manifest is not read"
              continue
            fi
            gh release download "$t" --repo timothymamo/shift2eu --pattern "shift2eu-site_$t.tar.gz" --dir higher
            gh attestation verify "higher/shift2eu-site_$t.tar.gz" --repo timothymamo/shift2eu \
              --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
              --source-ref "refs/tags/$t" --deny-self-hosted-runners
            tar -xzOf "higher/shift2eu-site_$t.tar.gz" manifest.json > "manifests/$t.json"
          done
`,
	`      # The one step of the workflow that holds the site's key, and the only
      # place either secret is named: the deploy program uploads the bundle to
      # the bucket, with the higher releases' manifests and the release tags on
      # main beside it. The step has no GH_TOKEN, so nothing in it can write to
      # the repository. The bucket and the region are the site environment's
      # variables, so no name is written into this document.
      - name: Deploy the site
        env:
          SCW_ACCESS_KEY: ${{ secrets.SCW_ACCESS_KEY }}
          SCW_SECRET_KEY: ${{ secrets.SCW_SECRET_KEY }}
          TAG: ${{ github.ref_name }}
          SITE_BUCKET: ${{ vars.SITE_BUCKET }}
          SITE_REGION: ${{ vars.SITE_REGION }}
        run: ./sitedeploy -bundle bundle -version "$TAG" -bucket "$SITE_BUCKET" -region "$SITE_REGION" -releases releases.txt -manifests manifests
`,
}

// siteCheckJobHead is the site-check job up to its steps: the live check of the
// site the deploy job published (PRD #297 milestone 3). No key and no
// environment: it reads the published release, its own program and site/records
// at the tag, and holds contents: read alone.
const siteCheckJobHead = `  # The live check after the deploy (PRD #297 milestone 3): the published
  # release's manifest against the site that is live. No key and no
  # environment: contents: read alone. The bundle comes from the published
  # release, checked against the site job's digest, and site/records at the tag
  # gives the HSTS and preload lists the check reads at their default paths.
  site-check:
    name: Check the live site
    needs: [latest, site, site-programs, site-deploy]
    if: needs.latest.outputs.latest == 'true' && vars.SITE_DEPLOY_ENABLED == 'true'
    runs-on: ubuntu-latest
    timeout-minutes: 15
    permissions:
      contents: read # check out the repository
    steps:
`

// siteCheckStepTemplates are the site-check job's steps: the repository at the
// tag, the live check program from the site-programs job, the published
// release's bundle checked against the site job's digest and unpacked for its
// manifest, the release tags on main as the deploy job reads them, and the
// live check itself, at the defaults milestone 4 may change (PRD #297
// milestone 3).
var siteCheckStepTemplates = []string{
	`      # site/records' HSTS and preload lists, which the check reads at their
      # default paths, at the tag: no repository code runs, and the token is not
      # kept.
      - uses: {{pin actions/checkout}}
        with:
          persist-credentials: false
`,
	`      - uses: {{pin actions/download-artifact}}
        with:
          name: sitecheck
          path: bin
`,
	`      # The bundle the release published, checked against the site job's digest
      # before anything is read from it, and the check program against its own
      # job's digest. No attestation is verified here: the deploy job verifies the
      # one bundle this run deploys, and this reads the same bundle and digest.
      - name: Check the site bundle and the live check program
        env:
          TAG: ${{ github.ref_name }}
          SITE_SHA256: ${{ needs.site.outputs.sha256 }}
          SITECHECK_SHA256: ${{ needs.site-programs.outputs.sitecheck }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          if [ "$(gh release view "$TAG" --repo timothymamo/shift2eu --json isDraft --jq '.isDraft')" != false ]; then
            echo "the release of $TAG is still a draft, so its bundle was never published" >&2
            exit 1
          fi
          gh release download "$TAG" --repo timothymamo/shift2eu --pattern "shift2eu-site_$TAG.tar.gz" --dir bin
          printf '%s  %s\n%s  %s\n' "$SITE_SHA256" "bin/shift2eu-site_$TAG.tar.gz" "$SITECHECK_SHA256" bin/sitecheck | sha256sum -c -
          tar -xzOf "bin/shift2eu-site_$TAG.tar.gz" manifest.json > bin/manifest.json
          chmod u+x bin/sitecheck
`,
	`      # The release tags on main, as the deploy job reads them: the check holds
      # the live pages' footers to the releases it must not be older than.
      - name: The release tags on main
        env:
          TAG: ${{ github.ref_name }}
        run: |
          set -euo pipefail
          git init -q tags
          git -C tags fetch -q --filter=tree:0 --no-tags https://github.com/timothymamo/shift2eu.git '+refs/heads/main:refs/remotes/origin/main' '+refs/tags/v*:refs/tags/v*'
          git -C tags tag -l 'v*.*.*' --merged refs/remotes/origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V > bin/releases.txt
          grep -qxF "$TAG" bin/releases.txt || { echo "$TAG is not a release tag on main" >&2; exit 1; }
`,
	`      # The check's own defaults: the HSTS and preload lists at the paths
      # site/records holds them at, and milestone 4's -plain-http-www to come.
      - name: Check the live site
        run: bin/sitecheck -manifest bin/manifest.json -tags bin/releases.txt
`,
}

// siteSkippedJobHead is the site-skipped job up to its steps: the visible
// notice that the deploy's conditions did not hold (PRD #297 milestone 3). It
// is a job of its own, since a step-level condition would still run the deploy
// job, with its environment's deployment, on every release, and it holds no
// permission and no secret.
const siteSkippedJobHead = `  # The notice that the deploy's conditions did not hold (PRD #297 milestone 3):
  # a job of its own, because a step-level condition would still run the deploy
  # job, with its environment's deployment, on every release. No permission and
  # no key, and the variable's value is never interpolated into the text it
  # prints, so what the maintainer set can start no workflow command.
  site-skipped:
    name: Site deploy skipped
    needs: latest
    if: needs.latest.outputs.latest != 'true' || vars.SITE_DEPLOY_ENABLED != 'true'
    runs-on: ubuntu-latest
    timeout-minutes: 5
    permissions: {}
    steps:
`

// siteSkippedStepTemplates are the site-skipped job's one step: the reason the
// deploy did not run, as a notice and as the summary's note, and a failure when
// the deploy's conditions do hold, so a job GitHub ran by mistake is not read as
// a skip (PRD #297 milestone 3).
var siteSkippedStepTemplates = []string{
	`      - name: Say that the site deploy is skipped
        env:
          LATEST: ${{ needs.latest.outputs.latest }}
          ENABLED: ${{ vars.SITE_DEPLOY_ENABLED }}
        run: |
          set -euo pipefail
          if [ "$LATEST" != true ]; then
            reason="the tag is not the highest release tag on main"
          elif [ "$ENABLED" != true ]; then
            reason="SITE_DEPLOY_ENABLED is not true"
          else
            echo "the deploy's conditions hold, so this job should not have run" >&2
            exit 1
          fi
          echo "::notice title=Site deploy skipped::The site deploy and its live check are skipped: $reason"
          printf '> [!NOTE]\n> The site deploy and its live check are skipped: %s.\n' "$reason" >> "$GITHUB_STEP_SUMMARY"
`,
}

// imageJobHead is the image job up to its steps: its digest output, the two
// permissions it needs to push, and no OIDC token.
const imageJobHead = `  # Builds and pushes the container image to GHCR (ADR 0015), after GoReleaser
  # uploads the draft release: a failed GoReleaser publishes no image, but the
  # image does not wait for attest-archives or publish, so if either fails it
  # is public while the release is still a draft (ADR 0015). It can push,
  # nothing more: no OIDC token, so nothing in it can sign. It builds from the
  # Dockerfile with the build arguments GoReleaser stamps into the release
  # binaries, and carries no build cache in: BuildKit starts empty on a fresh
  # runner. It tags latest by the latest job's one decision alone, so its latest
  # and GitHub's "Latest" release name the same version (issue #173).
  image:
    name: Container image
    needs: [test, goreleaser, latest]
    runs-on: ubuntu-latest
    timeout-minutes: 20
    permissions:
      contents: read # check out the repository
      packages: write # push the image to ghcr.io
    outputs:
      digest: ${{ steps.build.outputs.digest }}
    steps:
`

// imageStepTemplates are the image job's steps: the build arguments GoReleaser stamps,
// a build of two platforms from an empty BuildKit, pushed, without BuildKit's
// own provenance or SBOM, tagged latest by the latest job's decision alone.
var imageStepTemplates = []string{
	`      # The whole history and every tag, as GoReleaser checks it out: git picks
      # the length of a short commit from the number of objects it holds, so the
      # stamp reads the commit as GoReleaser does.
      - uses: {{pin actions/checkout}}
        with:
          fetch-depth: 0
          persist-credentials: false
`,
	`      # The values GoReleaser stamps into the release binaries: .Version (the
      # tag without its v), .ShortCommit and .CommitDate (RFC 3339, UTC).
      # Whether the tag is the latest release is not decided here: the latest
      # job decides it once, and metadata-action reads that output, so the image
      # and GitHub's "Latest" release cannot disagree (issue #173).
      - name: Build arguments, as GoReleaser stamps them
        id: stamp
        env:
          TAG: ${{ github.ref_name }}
        run: |
          {
            echo "version=${TAG#v}"
            echo "commit=$(git rev-parse --short HEAD)"
            echo "date=$(TZ=UTC git log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ)"
          } >> "$GITHUB_OUTPUT"
`,
	`      # No QEMU: the builder stage cross-compiles and the final stage runs
      # nothing, so linux/arm64 builds without emulation (ADR 0013).
      - uses: {{pin docker/setup-buildx-action}}
        with:
          # The BuildKit image, pinned by tag and multi-arch index digest.
          # renovate: datasource=docker depName=moby/buildkit versioning=semver
          driver-opts: image=moby/buildkit:{{image moby/buildkit}}
`,
	`      - uses: {{pin docker/login-action}}
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
`,
	`      # vX.Y.Z (the git tag) and X.Y, plus latest for the highest release tag;
      # a pre-release such as v0.1.0-rc.1 gets its own tag only.
      - name: Image tags
        id: tags
        uses: {{pin docker/metadata-action}}
        with:
          images: ghcr.io/timothymamo/shift2eu
          flavor: latest=${{ needs.latest.outputs.latest }}
          tags: |
            type=semver,pattern={{raw}}
            type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}
`,
	`      # Labels come from the Dockerfile alone. BuildKit's own provenance and
      # SBOM are off: the sign job's attestations replace them (ADR 0015).
      - name: Build and push linux/amd64 and linux/arm64
        id: build
        uses: {{pin docker/build-push-action}}
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: true
          tags: ${{ steps.tags.outputs.tags }}
          annotations: index:org.opencontainers.image.source=https://github.com/timothymamo/shift2eu
          provenance: false
          sbom: false
          build-args: |
            VERSION=${{ steps.stamp.outputs.version }}
            COMMIT=${{ steps.stamp.outputs.commit }}
            DATE=${{ steps.stamp.outputs.date }}
`,
}

// sbomJobHead is the SBOM job up to its steps: read-only to the registry, no
// OIDC token, no way to push.
const sbomJobHead = `  # The SBOM of the pushed index, read from the registry by digest. syft is
  # third-party code it downloads, so this job can only read the registry:
  # it cannot push, and has no OIDC token to sign with. The SBOM leaves the job
  # as a workflow artifact for the sign job to attest.
  sbom:
    name: SBOM
    needs: image
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      packages: read # pull the image from ghcr.io, private until made public
    steps:
`

// sbomStepTemplates are the SBOM job's steps: log in read-only, syft the pushed index
// by digest, and upload the SPDX document under the name the sign job
// downloads.
var sbomStepTemplates = []string{
	`      - uses: {{pin docker/login-action}}
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
`,
	`      - uses: {{pin anchore/sbom-action}}
        with:
          image: registry:ghcr.io/timothymamo/shift2eu@${{ needs.image.outputs.digest }}
          format: spdx-json
          output-file: shift2eu.spdx.json
          upload-artifact: false
          upload-release-assets: false
          # renovate: datasource=github-releases depName=anchore/syft
          syft-version: {{release syft}}
`,
	`      - uses: {{pin actions/upload-artifact}}
        with:
          name: sbom
          path: shift2eu.spdx.json
          if-no-files-found: error
          retention-days: 1
`,
}

// signJobHead is the sign job up to its steps: the only job holding the
// signing identity, and one that must check nothing out and run no docker/* or
// anchore action.
const signJobHead = `  # The only job holding the signing identity (id-token: write). It runs
  # sigstore's cosign and GitHub's own actions only: no checkout, no docker/*
  # or anchore action. Everything is signed and attested by digest.
  sign:
    name: Sign and attest
    needs: [image, sbom]
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      packages: write # push the signatures and attestations to ghcr.io
      id-token: write # GitHub OIDC token for keyless cosign and attestation signing
      attestations: write # store the provenance and SBOM attestations in the repository
    steps:
`

// signStepTemplates are the sign job's steps: cosign by exact release, a login fed the
// token through env:, a recursive signature by digest, the provenance
// attestation, the SBOM artifact, and its attestation.
var signStepTemplates = []string{
	`      - uses: {{pin sigstore/cosign-installer}}
        with:
          # renovate: datasource=github-releases depName=sigstore/cosign
          cosign-release: {{release cosign}}
`,
	`      # cosign writes the registry credential where actions/attest reads it.
      - name: Log in to ghcr.io
        env:
          ACTOR: ${{ github.actor }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: printf '%s' "$GITHUB_TOKEN" | cosign login ghcr.io --username "$ACTOR" --password-stdin
`,
	`      # Keyless, through the job's OIDC identity; the index and, recursively,
      # each platform's image, so a per-platform digest verifies too.
      - name: Sign the image
        env:
          DIGEST: ${{ needs.image.outputs.digest }}
        run: cosign sign --yes --recursive "ghcr.io/timothymamo/shift2eu@${DIGEST}"
`,
	`      # SLSA build provenance: actions/attest's default without sbom-path.
      - name: Attest the build provenance
        uses: {{pin actions/attest}}
        with:
          subject-name: ghcr.io/timothymamo/shift2eu
          subject-digest: ${{ needs.image.outputs.digest }}
          push-to-registry: true
          create-storage-record: false # an organisation API; needs artifact-metadata: write
`,
	`      - uses: {{pin actions/download-artifact}}
        with:
          name: sbom
`,
	`      - name: Attest the SBOM
        uses: {{pin actions/attest}}
        with:
          subject-name: ghcr.io/timothymamo/shift2eu
          subject-digest: ${{ needs.image.outputs.digest }}
          sbom-path: shift2eu.spdx.json
          push-to-registry: true
          create-storage-record: false
`,
}

// verifyJobHead is the verify job up to its steps: the independent check of
// what the release published (issue #99), on a fresh runner after the image is
// built, signed and attested, with read-only access to the registry and to
// the repository's attestations, no OIDC token and no checkout.
const verifyJobHead = `  # The independent check of what the release published (issue #99): a fresh
  # runner, after the image is signed and attested, with read-only access to
  # the registry and no OIDC token, so nothing in it can sign or push. It needs
  # the image job too: its verify step compares the digest the tag resolves to
  # with the digest this run built, so a tag moved between the build and the
  # check fails here. A tag can be moved and a digest cannot, so every check
  # below reads a digest, never a tag.
  verify:
    name: Verify the published image
    needs: [image, sign]
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      packages: read # the image and its signature
      attestations: read # gh attestation verify reads them through the repository's API
    steps:
`

// verifyStepTemplates are the verify job's steps: cosign by exact release, a cosign
// login, the checks of docs/guide/verify-releases.md on the image this run
// built - the tag resolving to that digest, the signature for this tag and
// commit, the build provenance, the SBOM, and the image itself - and the tag
// rule, whose highest release tag is read from main's ancestry without a
// checkout.
var verifyStepTemplates = []string{
	`      - uses: {{pin sigstore/cosign-installer}}
        with:
          # renovate: datasource=github-releases depName=sigstore/cosign
          cosign-release: {{release cosign}}
`,
	`      # cosign writes the registry credential where docker and buildx read it,
      # so the checks below pull what the job may read with its own token.
      - name: Log in to ghcr.io
        env:
          ACTOR: ${{ github.actor }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: printf '%s' "$GITHUB_TOKEN" | cosign login ghcr.io --username "$ACTOR" --password-stdin
`,
	`      # docs/guide/verify-releases.md, on the digest this run built: the tag
      # must resolve to that digest, and that image must carry the signature
      # this workflow made for exactly this tag and this commit, the build
      # provenance and the SBOM, and it must run and report the version of the
      # tag.
      - name: Verify the CLI image
        env:
          TAG: ${{ github.ref_name }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          BUILT: ${{ needs.image.outputs.digest }}
        run: |
          set -euo pipefail
          DIGEST="$(docker buildx imagetools inspect "ghcr.io/timothymamo/shift2eu:$TAG" --format '{{.Manifest.Digest}}')"
          if [ "$DIGEST" != "$BUILT" ]; then
            echo "ghcr.io/timothymamo/shift2eu:$TAG resolves to $DIGEST, not the digest this run built, $BUILT" >&2
            exit 1
          fi
          IMAGE="ghcr.io/timothymamo/shift2eu@$DIGEST"
          cosign verify "$IMAGE" \
            --certificate-identity "https://github.com/timothymamo/shift2eu/.github/workflows/release.yml@refs/tags/$TAG" \
            --certificate-oidc-issuer https://token.actions.githubusercontent.com \
            --certificate-github-workflow-trigger push \
            --certificate-github-workflow-sha "$GITHUB_SHA"
          gh attestation verify "oci://$IMAGE" --repo timothymamo/shift2eu \
            --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
            --source-ref "refs/tags/$TAG" --source-digest "$GITHUB_SHA" --deny-self-hosted-runners
          gh attestation verify "oci://$IMAGE" --repo timothymamo/shift2eu \
            --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
            --source-ref "refs/tags/$TAG" --source-digest "$GITHUB_SHA" --deny-self-hosted-runners \
            --predicate-type https://spdx.dev/Document/v2.3
          docker run --rm --read-only --network none "$IMAGE" version | tee version.txt
          grep -qF "shift2eu ${TAG#v} (" version.txt
`,
	`      # The tag rule ADR 0015 states: vX.Y.Z always, X.Y for every release,
      # latest for the highest release tag alone, and a pre-release, which
      # moves neither X.Y nor latest. The highest is the tag the image job's
      # stamp read: the release tags merged into main. They are read by
      # ancestry here too, from the public repository into a bare repository of
      # this job's own, so no checkout runs and no repository code runs. Only a
      # registry saying the tag does not exist is read as an absent tag, which
      # the rule allows for the one tag whose own run has not published it, the
      # highest, and a notice then says so; any other answer from the registry,
      # a 5xx, a rate limit or a failed login, fails the job.
      - name: The tags follow the release rule
        env:
          TAG: ${{ github.ref_name }}
          CLI_BUILT: ${{ needs.image.outputs.digest }}
        run: |
          set -euo pipefail
          version="${TAG#v}"
          minor="${version%%-*}"
          minor="${minor%.*}"
          git init -q tags
          git -C tags fetch -q --filter=tree:0 --no-tags https://github.com/timothymamo/shift2eu.git '+refs/heads/main:refs/remotes/origin/main' '+refs/tags/v*:refs/tags/v*'
          highest="$(git -C tags tag -l 'v*.*.*' --merged refs/remotes/origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V | tail -n 1)"
          if [ -z "$highest" ]; then
            echo "no release tag is merged into main, so X.Y and latest cannot be checked" >&2
            exit 1
          fi
          # digest_or_absent prints the digest its tag resolves to, or nothing
          # when the registry says the tag does not exist. The digest is what
          # the inspect writes on stdout, in stderr kept apart, so a docker
          # warning is never read as one, and what succeeds must be exactly one
          # sha256 line. Absence is exit status 1 with the fixed, case-sensitive
          # "$1: not found" in stderr, which names this very reference, and
          # nothing else is absence: any other answer, a 5xx, a rate limit, a
          # failed login, a missing docker or a broken credential helper, is
          # printed and fails the step, whose caller assigns the digest in a
          # command of its own so set -e sees the failure.
          digest_or_absent() {
            err="$(mktemp)"
            if out="$(docker buildx imagetools inspect "$1" --format '{{.Manifest.Digest}}' 2> "$err")"; then
              if [[ "$out" =~ ^sha256:[0-9a-f]{64}$ ]]; then
                printf '%s\n' "$out"
              else
                echo "$1 answered '$out' rather than one sha256 digest" >&2
                rm -f "$err"
                return 1
              fi
            else
              rc=$?
              if [ "$rc" -eq 1 ] && grep -qF -e "$1: not found" "$err"; then
                :
              else
                echo "$1 could not be read, docker exited $rc:" >&2
                cat "$err" >&2
                rm -f "$err"
                return 1
              fi
            fi
            rm -f "$err"
          }
          check() {
            image="$1"
            digest="$2"
            minor_digest="$(digest_or_absent "$image:$minor")"
            case "$TAG" in
              *-*)
                if [ "$minor_digest" = "$digest" ]; then
                  echo "$image:$minor is $TAG, a pre-release, and a pre-release moves neither X.Y nor latest" >&2
                  exit 1
                fi
                latest_digest="$(digest_or_absent "$image:latest")"
                if [ "$latest_digest" = "$digest" ]; then
                  echo "$image:latest is $TAG, a pre-release, and a pre-release moves neither X.Y nor latest" >&2
                  exit 1
                fi
                ;;
              *)
                if [ "$minor_digest" != "$digest" ]; then
                  echo "$image:$minor is not $TAG" >&2
                  exit 1
                fi
                if [ "$highest" = "$TAG" ]; then
                  latest_digest="$(digest_or_absent "$image:latest")"
                  if [ "$latest_digest" != "$digest" ]; then
                    echo "$image:latest is not $TAG, the highest release tag" >&2
                    exit 1
                  fi
                else
                  highest_digest="$(digest_or_absent "$image:$highest")"
                  if [ -z "$highest_digest" ]; then
                    echo "::notice::$image:$highest is not published yet, so this run does not check $image:latest"
                  else
                    latest_digest="$(digest_or_absent "$image:latest")"
                    if [ "$latest_digest" = "$digest" ]; then
                      echo "$image:latest is $TAG, which is not the highest release tag, $highest" >&2
                      exit 1
                    fi
                  fi
                fi
                ;;
            esac
          }
          check ghcr.io/timothymamo/shift2eu "$CLI_BUILT"
`,
}

// inVerifyStep returns the verify job's steps with the first old replaced by
// new in the one step whose text holds part: a case changes one step of the
// job alone.
func inVerifyStep(t *testing.T, part, old, new string) []string {
	t.Helper()

	out := slices.Clone(verifySteps)

	i := stepIndex(t, out, part)
	if !strings.Contains(out[i], old) {
		t.Fatalf("the verify job's step holding %q holds no %q", part, old)
	}

	out[i] = strings.Replace(out[i], old, new, 1)

	return out
}

// inStepOf returns steps with the first old replaced by new in the one step
// whose text holds part: a case changes one step of a job alone, even when the
// text it changes appears in another step of the same job.
func inStepOf(t *testing.T, steps []string, part, old, new string) []string {
	t.Helper()

	out := slices.Clone(steps)

	i := stepIndex(t, out, part)
	if !strings.Contains(out[i], old) {
		t.Fatalf("the step holding %q holds no %q", part, old)
	}

	out[i] = strings.Replace(out[i], old, new, 1)

	return out
}

// releaseJob is one job of the release workflow fixture: its key in jobs:, the
// comments and keys above its steps, and its steps in order.
type releaseJob struct {
	key   string
	head  string
	steps []string
}

// text returns the job as the release workflow writes it.
func (j releaseJob) text() string {
	return j.head + strings.Join(j.steps, "")
}

// releaseFixture is .github/workflows/release.yml in parts.
type releaseFixture struct {
	top  string
	jobs []releaseJob
}

// releaseWorkflowFixture returns .github/workflows/release.yml in parts, with
// the pins the repository's release.yml holds.
func releaseWorkflowFixture() releaseFixture {
	return releaseWorkflowFixtureFrom(repoRelease)
}

// releaseWorkflowFixtureFrom returns the release workflow in parts, with the
// pins of release, a release.yml: every placeholder of the step templates
// (workflowPins) filled in from it.
func releaseWorkflowFixtureFrom(release []byte) releaseFixture {
	pins := pinsOf(release)

	return releaseFixture{
		top: releaseWorkflowTop,
		jobs: []releaseJob{
			{key: "test", head: testJobHead, steps: expandSteps(pins, testStepTemplates)},
			{key: "latest", head: latestJobHead, steps: expandSteps(pins, latestStepTemplates)},
			{key: "site", head: siteJobHead, steps: expandSteps(pins, siteStepTemplates)},
			{key: "site-browser", head: siteBrowserJobHead, steps: expandSteps(pins, siteBrowserStepTemplates)},
			{key: "goreleaser", head: goreleaserJobHead, steps: expandSteps(pins, goreleaserStepTemplates)},
			{key: "attest-archives", head: attestArchivesJobHead, steps: expandSteps(pins, attestArchivesStepTemplates)},
			{key: "publish", head: publishJobHead, steps: expandSteps(pins, publishStepTemplates)},
			{key: "image", head: imageJobHead, steps: expandSteps(pins, imageStepTemplates)},
			{key: "sbom", head: sbomJobHead, steps: expandSteps(pins, sbomStepTemplates)},
			{key: "sign", head: signJobHead, steps: expandSteps(pins, signStepTemplates)},
			{key: "verify", head: verifyJobHead, steps: expandSteps(pins, verifyStepTemplates)},
			{key: "verify-archives", head: verifyArchivesJobHead, steps: expandSteps(pins, verifyArchivesStepTemplates)},
			{key: "site-programs", head: siteProgramsJobHead, steps: expandSteps(pins, siteProgramsStepTemplates)},
			{key: "site-deploy", head: siteDeployJobHead, steps: expandSteps(pins, siteDeployStepTemplates)},
			{key: "site-check", head: siteCheckJobHead, steps: expandSteps(pins, siteCheckStepTemplates)},
			{key: "site-skipped", head: siteSkippedJobHead, steps: expandSteps(pins, siteSkippedStepTemplates)},
		},
	}
}

// The steps of each job with the pins of the repository's release.yml: the
// cases change one part of them.
var (
	testSteps           = expandSteps(releasePins, testStepTemplates)
	latestSteps         = expandSteps(releasePins, latestStepTemplates)
	siteSteps           = expandSteps(releasePins, siteStepTemplates)
	siteBrowserSteps    = expandSteps(releasePins, siteBrowserStepTemplates)
	goreleaserSteps     = expandSteps(releasePins, goreleaserStepTemplates)
	attestArchivesSteps = expandSteps(releasePins, attestArchivesStepTemplates)
	publishSteps        = expandSteps(releasePins, publishStepTemplates)
	imageSteps          = expandSteps(releasePins, imageStepTemplates)
	sbomSteps           = expandSteps(releasePins, sbomStepTemplates)
	signSteps           = expandSteps(releasePins, signStepTemplates)
	verifySteps         = expandSteps(releasePins, verifyStepTemplates)
	verifyArchivesSteps = expandSteps(releasePins, verifyArchivesStepTemplates)

	siteProgramsSteps = expandSteps(releasePins, siteProgramsStepTemplates)
	siteDeploySteps   = expandSteps(releasePins, siteDeployStepTemplates)
	siteCheckSteps    = expandSteps(releasePins, siteCheckStepTemplates)
	siteSkippedSteps  = expandSteps(releasePins, siteSkippedStepTemplates)
)

// expandSteps returns steps with every placeholder filled in from pins. A step
// without one is returned as it is.
func expandSteps(pins workflowPins, steps []string) []string {
	out := make([]string, len(steps))
	for i, step := range steps {
		out[i] = pins.expand(step)
	}

	return out
}

// render returns the fixture as the release workflow file.
func (f releaseFixture) render() string {
	blocks := make([]string, 0, len(f.jobs))
	for _, j := range f.jobs {
		blocks = append(blocks, j.text())
	}

	return f.top + strings.Join(blocks, "\n")
}

// index returns the index of job key.
func (f releaseFixture) index(t *testing.T, key string) int {
	t.Helper()

	for i, j := range f.jobs {
		if j.key == key {
			return i
		}
	}

	t.Fatalf("the fixture holds no job %q", key)

	return -1
}

// withSteps returns the workflow with the steps of job key replaced by steps.
func (f releaseFixture) withSteps(t *testing.T, key string, steps []string) string {
	t.Helper()

	jobs := slices.Clone(f.jobs)
	jobs[f.index(t, key)].steps = steps

	return releaseFixture{top: f.top, jobs: jobs}.render()
}

// inJob returns the workflow with old replaced by new in the text of job key
// alone: its keys and steps, comments included. It fails unless that job holds
// old exactly once, so that a case that mistypes the fixture text cannot pass
// for the wrong reason.
func (f releaseFixture) inJob(t *testing.T, key, old, new string) string {
	t.Helper()

	jobs := slices.Clone(f.jobs)

	i := f.index(t, key)
	if got := strings.Count(jobs[i].text(), old); got != 1 {
		t.Fatalf("the fixture's %s job holds %q %d times, want exactly once", key, old, got)
	}

	jobs[i].head = strings.Replace(jobs[i].text(), old, new, 1)
	jobs[i].steps = nil

	return releaseFixture{top: f.top, jobs: jobs}.render()
}

// withoutJob returns the workflow without job key.
func (f releaseFixture) withoutJob(t *testing.T, key string) string {
	t.Helper()

	i := f.index(t, key)

	jobs := slices.Clone(f.jobs)
	jobs = append(jobs[:i], jobs[i+1:]...)

	return releaseFixture{top: f.top, jobs: jobs}.render()
}

// stepIndex returns the index of the one step of steps that holds part.
func stepIndex(t *testing.T, steps []string, part string) int {
	t.Helper()

	found := -1

	for i, step := range steps {
		if !strings.Contains(step, part) {
			continue
		}

		if found >= 0 {
			t.Fatalf("more than one step holds %q", part)
		}

		found = i
	}

	if found < 0 {
		t.Fatalf("no step holds %q", part)
	}

	return found
}

// withoutStep returns steps without the one holding part.
func withoutStep(t *testing.T, steps []string, part string) []string {
	t.Helper()

	i := stepIndex(t, steps, part)

	out := make([]string, 0, len(steps))
	out = append(out, steps[:i]...)
	out = append(out, steps[i+1:]...)

	return out
}

// swappedSteps returns steps with the two holding a and b swapped.
func swappedSteps(t *testing.T, steps []string, a, b string) []string {
	t.Helper()

	out := slices.Clone(steps)
	ia := stepIndex(t, out, a)
	ib := stepIndex(t, out, b)
	out[ia], out[ib] = out[ib], out[ia]

	return out
}

// beforeStep returns steps with step, a sequence entry without its dash,
// inserted before the one whose text holds head.
func beforeStep(t *testing.T, steps []string, head, step string) []string {
	t.Helper()

	i := stepIndex(t, steps, head)

	out := make([]string, 0, len(steps)+1)
	out = append(out, steps[:i]...)
	out = append(out, "      - "+step+"\n")
	out = append(out, steps[i:]...)

	return out
}

// TestReleaseWorkflow pins what the release workflow may say. It is read as a
// closed schema: the top-level keys, the twelve jobs, their keys and their steps
// are fixed, so no step runs repository code, sets the go command's environment
// or swaps the goreleaser document before goreleaser runs; only a commit on
// main is released; the publishing privileges stay split over three jobs, so
// the image can push but not sign, the SBOM job can only read, and the sign
// job checks nothing out and runs sigstore's and GitHub's actions alone; and
// the verify job reads only the registry and the attestations, compares each
// tag to the digest this run built, and applies the release rule to the
// release tags merged into main. The latest job decides once per run whether
// the tag is the highest release tag on main; both image jobs tag latest by
// that one output, and the publish job moves GitHub's "Latest" release by it,
// so a tag pushed while the run is in flight cannot leave them disagreeing;
// verify-archives, the job after publish, reads the highest itself and checks
// GitHub's "Latest" release against it, and never accepts a pre-release there
// (issue #173). The probes are the steps the reviews and audits of issues #40,
// #72 and #99 found a lexical check let through.
func TestReleaseWorkflow(t *testing.T) {
	t.Parallel()

	release := releaseWorkflowFixture()
	releaseWorkflow := release.render()

	// The cases write a pin release.yml holds as its placeholder
	// (workflowPins), which the helpers below fill in from release.yml, so
	// that a Renovate update of the workflow needs no edit of a case.
	pin := releasePins.expand

	// The BuildKit image release.yml pins, by tag and digest, for the cases
	// that change one of the two.
	buildkitTag, buildkitDigest, _ := strings.Cut(pin("{{image moby/buildkit}}"), "@sha256:")

	// beforeGoreleaser inserts step before the goreleaser step.
	beforeGoreleaser := func(step string) string {
		return strings.Replace(releaseWorkflow, "      - uses: goreleaser/goreleaser-action@",
			"      - "+pin(step)+"\n      - uses: goreleaser/goreleaser-action@", 1)
	}

	// inTestJob inserts step before the test job's make test.
	inTestJob := func(step string) string {
		return strings.Replace(releaseWorkflow, "      - name: Test\n", "      - "+pin(step)+"\n      - name: Test\n", 1)
	}

	// in replaces old with new in one job alone, failing unless that job holds
	// old exactly once.
	in := func(key, old, new string) string {
		return release.inJob(t, key, pin(old), pin(new))
	}

	// steps returns the workflow with the steps of one job replaced.
	steps := func(key string, steps []string) string {
		return release.withSteps(t, key, expandSteps(releasePins, steps))
	}

	// afterPublish returns the workflow with the stale-decision check moved
	// from where the fixture holds it to after the PATCH that publishes the
	// draft, so that the check no longer runs before anything is published.
	afterPublish := func() string {
		return strings.Replace(in("publish", publishStaleDecisionCheck, ""), publishByID, publishByID+"\n"+publishStaleDecisionCheck, 1)
	}

	// afterDownload returns the workflow with the deploy job's draft check moved
	// from before the release's download to after it, so a draft's bundle is
	// downloaded before the release is known to be published.
	afterDownload := func() string {
		return steps("site-deploy", inStepOf(t, siteDeploySteps, "name: Check the site bundle and the deploy program",
			draftCheckBlock+deployBundleDownload, deployBundleDownload+"\n"+strings.TrimSuffix(draftCheckBlock, "\n")))
	}

	// inDeployStep and inCheckStep change one step of the deploy job and of the
	// live check, whose steps hold the same lines twice (the attestation flags,
	// the release tags' pipeline).
	inDeployStep := func(part, old, new string) string {
		return steps("site-deploy", inStepOf(t, siteDeploySteps, part, old, new))
	}
	inCheckStep := func(part, old, new string) string {
		return steps("site-check", inStepOf(t, siteCheckSteps, part, old, new))
	}

	// proxyInBuildStep adds a proxy variable to the env: of the site-programs
	// job's build step: none of the five may be named anywhere in the document,
	// so a step cannot point the key's upload at a proxy of its own.
	proxyInBuildStep := func(name string) string {
		return in("site-programs", "      - name: Build the site's programs\n        run: make site-programs\n",
			"      - name: Build the site's programs\n        env:\n          "+name+": http://192.0.2.1:8080\n        run: make site-programs\n")
	}

	tests := []struct {
		name     string
		workflow string
		// wantIn is a part of the message the check must return; "" means
		// none at all.
		wantIn string
	}{
		{name: "the accepted shape", workflow: releaseWorkflow},
		{name: "CRLF line ends", workflow: strings.ReplaceAll(releaseWorkflow, "\n", "\r\n")},
		{
			// Every pin a Renovate update changes is matched by form: the actions'
			// commits, goreleaser's, cosign's and syft's versions, and the BuildKit
			// image in the image job's driver-opts. Each newer pin is derived
			// from the one it replaces (bumpPins), so the case follows a
			// Renovate update of release.yml.
			name:     "a newer pin of every action and tool",
			workflow: string(bumpPins(t, []byte(releaseWorkflow))),
		},

		// The probes of the review and audit, each before the goreleaser step.
		{name: "probe: go env -w GOFLAGS", workflow: beforeGoreleaser(`run: go env -w "G""OFLAGS=-tags=sneaky"`), wantIn: "goreleaser: steps holds 7 steps, want exactly 6"},
		{name: "probe: GOFLAGS to github.env", workflow: beforeGoreleaser(`run: echo "G""OFLAGS=-tags=x" >> "${{ github.env }}"`), wantIn: "github.env"},
		{name: "probe: GITHUB_ENV spelled at run time", workflow: beforeGoreleaser(`run: n=GITHUB_E; n=${n}NV; echo X=1 >> "${!n}"`), wantIn: "goreleaser: steps holds 7 steps, want exactly 6"},
		{name: "probe: go work init", workflow: beforeGoreleaser(`run: go work init .`), wantIn: "goreleaser: steps holds 7 steps, want exactly 6"},
		{name: "probe: go mod vendor", workflow: beforeGoreleaser(`run: go mod vendor`), wantIn: "goreleaser: steps holds 7 steps, want exactly 6"},
		{name: "probe: another goreleaser document copied in", workflow: beforeGoreleaser(`run: cp build/other.yaml .goreleaser.yaml`), wantIn: "goreleaser: steps holds 7 steps, want exactly 6"},
		{name: "probe: an action that sets the environment", workflow: beforeGoreleaser(`uses: example/sets-env@0123456789abcdef0123456789abcdef01234567`), wantIn: "goreleaser: steps holds 7 steps, want exactly 6"},
		{name: "probe: a repository script", workflow: beforeGoreleaser(`run: ./scripts/prepare.sh`), wantIn: "goreleaser: steps holds 7 steps, want exactly 6"},
		{name: "probe: make test back in the goreleaser job", workflow: beforeGoreleaser(`run: make test`), wantIn: "goreleaser: steps holds 7 steps, want exactly 6"},
		{name: "a step added to the test job", workflow: inTestJob(`run: go env -w GOFLAGS=-x`), wantIn: "holds 5 steps, want exactly 4"},
		{
			name:     "a step replaced in the goreleaser job",
			workflow: in("goreleaser", "      - uses: {{pin actions/setup-go}}\n        with:\n          go-version-file: go.mod\n          cache: false\n", "      - run: ./scripts/prepare.sh\n"),
			wantIn:   `want actions/setup-go holds the key "run"`,
		},
		{
			name:     "the test job running another command",
			workflow: in("test", "        run: make test\n", "        run: make test && go env -w GOFLAGS=-x\n"),
			wantIn:   "want make test: run is",
		},

		// The whole history and the guard that the tagged commit is on main
		// (ADR 0015): the second step of the test job, before anything runs
		// repository code, and exact.
		{name: "the test checkout without the full history", workflow: in("test", "          fetch-depth: 0\n", ""), wantIn: "fetch-depth"},
		{name: "no guard step", workflow: steps("test", withoutStep(t, testSteps, "name: The tagged commit is on main")), wantIn: "holds 3 steps, want exactly 4"},
		{name: "the guard after setup-go", workflow: steps("test", swappedSteps(t, testSteps, "name: The tagged commit is on main", "actions/setup-go@")), wantIn: `want the guard that the tagged commit is on main holds the key "uses"`},
		{
			name:     "the guard without the ancestry check",
			workflow: in("test", `git merge-base --is-ancestor "$GITHUB_SHA" refs/remotes/origin/main || {`, `git rev-parse --verify --quiet "$GITHUB_SHA" > /dev/null || {`),
			wantIn:   "merge-base",
		},
		{
			name:     "the guard without the origin/main check",
			workflow: in("test", "git rev-parse --verify --quiet refs/remotes/origin/main > /dev/null", "git rev-parse --verify --quiet HEAD > /dev/null"),
			wantIn:   "refs/remotes/origin/main",
		},
		{
			name:     "an expression in the guard script",
			workflow: in("test", `git merge-base --is-ancestor "$GITHUB_SHA" refs/remotes/origin/main`, `git merge-base --is-ancestor "${{ github.sha }}" refs/remotes/origin/main`),
			wantIn:   "holds a ${{ }} expression",
		},
		{name: "an if on the guard", workflow: in("test", "      - name: The tagged commit is on main\n", "      - name: The tagged commit is on main\n        if: always()\n"), wantIn: `key "if"`},
		{name: "a shell on the guard", workflow: in("test", "      - name: The tagged commit is on main\n", "      - name: The tagged commit is on main\n        shell: bash\n"), wantIn: `key "shell"`},

		// setup-go's Actions cache (GOMODCACHE and GOCACHE) would carry
		// state from the test job, or from CI on main, into the goreleaser
		// job, so both jobs turn it off.
		{name: "setup-go caching in the test job", workflow: in("test", "          cache: false # see the comment on the goreleaser job\n", "          cache: true # see the comment on the goreleaser job\n"), wantIn: `jobs: test: steps: step 3`},
		{name: "setup-go caching in the goreleaser job", workflow: in("goreleaser", "          cache: false\n", "          cache: true\n"), wantIn: `jobs: goreleaser: steps: step 2`},
		{name: "setup-go without cache: false", workflow: in("goreleaser", "          cache: false\n", ""), wantIn: `no "cache"`},

		// The tokens through which a step sets the environment or the PATH of
		// the steps after it, wherever they are, comments included.
		{name: "github.path in a comment", workflow: in("goreleaser", "{{pin goreleaser/goreleaser-action}}", "{{commit goreleaser/goreleaser-action}} # ${{ github.path }}"), wantIn: "github.path"},
		{name: "GITHUB.ENV in upper case", workflow: in("goreleaser", "{{pin goreleaser/goreleaser-action}}", "{{commit goreleaser/goreleaser-action}} # ${{ GITHUB.ENV }}"), wantIn: "github.env"},
		{name: "GITHUB_ENV in a comment", workflow: in("goreleaser", "{{pin goreleaser/goreleaser-action}}", "{{commit goreleaser/goreleaser-action}} # GITHUB_ENV"), wantIn: "GITHUB_ENV"},
		{name: "GOEXPERIMENT in a comment", workflow: in("test", "{{pin actions/checkout}}", "{{commit actions/checkout}} # GOEXPERIMENT"), wantIn: "GOEXPERIMENT"},

		// One release at a time, across every tag (ADR 0015): a queued run
		// waits, none is cancelled, and every tag shares the one group. The
		// block is exact, so a flow mapping, another group or a third key is
		// refused rather than read.
		{name: "no concurrency block", workflow: strings.Replace(releaseWorkflow, "concurrency:\n  group: release\n  cancel-in-progress: false\n", "", 1), wantIn: `holds no "concurrency"`},
		{name: "no concurrency group", workflow: strings.Replace(releaseWorkflow, "  group: release\n", "", 1), wantIn: `holds no "group"`},
		{name: "a concurrency that cancels the run in progress", workflow: strings.Replace(releaseWorkflow, "  cancel-in-progress: false\n", "  cancel-in-progress: true\n", 1), wantIn: `cancel-in-progress is "true"`},
		{name: "a concurrency group per tag", workflow: strings.Replace(releaseWorkflow, "  group: release\n", "  group: release-${{ github.ref }}\n", 1), wantIn: "group is"},
		{name: "a concurrency in flow form", workflow: strings.Replace(releaseWorkflow, "concurrency:\n  group: release\n  cancel-in-progress: false\n", "concurrency: {group: release, cancel-in-progress: false}\n", 1), wantIn: "want a block below it"},
		{name: "a third concurrency key", workflow: strings.Replace(releaseWorkflow, "  cancel-in-progress: false\n", "  cancel-in-progress: false\n  queue: max\n", 1), wantIn: `holds the key "queue"`},

		// The keys around the steps.
		{name: "a workflow-level env", workflow: strings.Replace(releaseWorkflow, "jobs:\n", "env:\n  SNEAKY: x\njobs:\n", 1), wantIn: `key "env"`},
		{name: "a workflow-level defaults", workflow: strings.Replace(releaseWorkflow, "jobs:\n", "defaults:\n  run:\n    shell: bash\njobs:\n", 1), wantIn: `key "defaults"`},
		{name: "a sixth job", workflow: releaseWorkflow + "  other:\n    runs-on: ubuntu-latest\n", wantIn: `key "other"`},
		{name: "a job-level env", workflow: in("image", "    needs: [test, goreleaser, latest]\n", "    needs: [test, goreleaser, latest]\n    env:\n      SNEAKY: x\n"), wantIn: `key "env"`},
		{name: "a container", workflow: in("image", "    needs: [test, goreleaser, latest]\n", "    needs: [test, goreleaser, latest]\n    container: golang\n"), wantIn: `key "container"`},
		{name: "no needs on goreleaser", workflow: in("goreleaser", "    needs: [test, site]\n", ""), wantIn: `no "needs"`},
		{name: "goreleaser needs another job", workflow: in("goreleaser", "    needs: [test, site]\n", "    needs: [image, site]\n"), wantIn: "needs"},
		{name: "write permission at the top", workflow: strings.Replace(releaseWorkflow, "permissions: {}", "permissions:\n  contents: write", 1), wantIn: "permissions"},
		{name: "write-all at the top", workflow: strings.Replace(releaseWorkflow, "permissions: {}", "permissions: write-all", 1), wantIn: "permissions"},
		{name: "write permission for the tests", workflow: in("test", "      contents: read\n", "      contents: write\n"), wantIn: "contents"},
		{name: "another trigger", workflow: strings.Replace(releaseWorkflow, "  push:\n", "  workflow_dispatch:\n  push:\n", 1), wantIn: "workflow_dispatch"},
		{name: "a step-level if", workflow: in("test", "        run: make test\n", "        run: make test\n        if: false\n"), wantIn: `key "if"`},
		{name: "a working-directory", workflow: in("test", "        run: make test\n", "        run: make test\n        working-directory: sub\n"), wantIn: `key "working-directory"`},
		{name: "an env on another step", workflow: in("goreleaser", "          cache: false\n", "          cache: false\n        env:\n          A: b\n"), wantIn: `key "env"`},
		{name: "a local action", workflow: in("test", "{{commit actions/setup-go}}", "./.github/actions/setup"), wantIn: "uses"},
		{name: "a reusable workflow", workflow: in("goreleaser", "    runs-on: ubuntu-latest\n", "    uses: example/repo/.github/workflows/x.yml@main\n"), wantIn: `holds no "runs-on", want exactly name, runs-on, permissions, steps, needs`},

		// What goreleaser is asked to do.
		{name: "an argument glued to --clean", workflow: in("goreleaser", "args: release --clean", "args: release --clean-f.goreleaser.prod.yaml"), wantIn: "args"},
		{name: "a --config", workflow: in("goreleaser", "args: release --clean", "args: release --clean --config x.yaml"), wantIn: "args"},
		{name: "no args", workflow: in("goreleaser", "          args: release --clean\n", ""), wantIn: `no "args"`},
		{name: "a workdir", workflow: in("goreleaser", "distribution: goreleaser", "distribution: goreleaser\n          workdir: sub"), wantIn: `key "workdir"`},
		{name: "a goreleaser version range", workflow: in("goreleaser", `"{{release goreleaser}}"`, `"~> v2"`), wantIn: "version"},
		{name: "an action pinned by tag", workflow: in("goreleaser", "{{commit goreleaser/goreleaser-action}}", "goreleaser/goreleaser-action@v7"), wantIn: "goreleaser-action@v7"},
		{name: "a second key in the goreleaser env", workflow: in("goreleaser", "          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n", "          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n          OTHER: x\n"), wantIn: "OTHER"},
		{name: "a GITHUB_TOKEN from somewhere else", workflow: in("goreleaser", "GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}", "GITHUB_TOKEN: ${{ vars.TOKEN }}"), wantIn: "GITHUB_TOKEN"},
		{name: "a flow-style env", workflow: in("goreleaser", "        env:\n          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n", "        env: {GITHUB_TOKEN: x, A: b}\n"), wantIn: "env"},
		{name: "a repeated key", workflow: in("goreleaser", "    needs: [test, site]\n", "    needs: [test, site]\n    needs: [test, site]\n"), wantIn: "repeated"},
		{name: "a tab", workflow: in("test", "{{pin actions/checkout}}", strings.Replace(pin("{{pin actions/checkout}}"), " # ", " #\t", 1)), wantIn: "U+0009"},

		// The draft-then-publish flow (issue #156): GoReleaser uploads every
		// asset to a draft release, which .goreleaser.yaml's release.draft asks
		// for (check.GitHubRelease pins it), and one job of its own publishes it
		// last, after the archives are attested, with contents: write alone, so
		// nothing can be added to or replaced in the release once it is public
		// and no other job publishes it (ADR 0015).
		{name: "no publish job", workflow: release.withoutJob(t, "publish"), wantIn: `no "publish"`},
		{name: "the publish job on another runner", workflow: in("publish", "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-24.04\n"), wantIn: "runs-on"},
		{name: "the publish job without needs", workflow: in("publish", "    needs: [test, goreleaser, attest-archives, latest, site-browser]\n", ""), wantIn: `no "needs"`},
		{name: "the publish job not needing the attestation job", workflow: in("publish", "    needs: [test, goreleaser, attest-archives, latest, site-browser]\n", "    needs: [test, goreleaser, latest, site-browser]\n"), wantIn: "jobs: publish: needs is"},
		{name: "the publish job needing the attestation job first", workflow: in("publish", "    needs: [test, goreleaser, attest-archives, latest, site-browser]\n", "    needs: [attest-archives, test, goreleaser, latest, site-browser]\n"), wantIn: "jobs: publish: needs is"},
		{name: "the publish job needing the attestation job only", workflow: in("publish", "    needs: [test, goreleaser, attest-archives, latest, site-browser]\n", "    needs: [attest-archives]\n"), wantIn: "jobs: publish: needs is"},
		{name: "the publish job not needing the latest job", workflow: in("publish", "    needs: [test, goreleaser, attest-archives, latest, site-browser]\n", "    needs: [test, goreleaser, attest-archives, site-browser]\n"), wantIn: `jobs: publish: needs is ["test" "goreleaser" "attest-archives" "site-browser"], want exactly ["test" "goreleaser" "attest-archives" "latest" "site-browser"]`},
		{name: "another permission for the publish job", workflow: in("publish", "      contents: write # publish the draft release\n", "      contents: write # publish the draft release\n      packages: write # push\n"), wantIn: "packages"},
		{name: "no contents: write for the publish job", workflow: in("publish", "      contents: write # publish the draft release\n", ""), wantIn: "contents"},
		// GitHub's "Latest" release follows the image's latest tag (issue
		// #173): the publish job sends make_latest from the latest job's one
		// decision, through env: and never pasted into the script, and never
		// omitted and never a constant, so publishing a patch on an older line
		// does not take GitHub's "Latest" over. Only exactly true or false
		// publishes.
		{name: "the publish step without make_latest", workflow: in("publish", ` -f make_latest="$LATEST"`, ""), wantIn: "want the check and publication of the draft: run is"},
		{name: "the publish step with make_latest always true", workflow: in("publish", `-f make_latest="$LATEST"`, `-f make_latest=true`), wantIn: "want the check and publication of the draft: run is"},
		{name: "the publish step with make_latest always false", workflow: in("publish", `-f make_latest="$LATEST"`, `-f make_latest=false`), wantIn: "want the check and publication of the draft: run is"},
		{name: "the publish step with make_latest from github.ref_name", workflow: in("publish", `-f make_latest="$LATEST"`, `-f make_latest=${{ github.ref_name }}`), wantIn: "want the check and publication of the draft: run holds a ${{ }} expression"},
		{name: "the publish step sending make_latest as a typed field", workflow: in("publish", `-f make_latest="$LATEST"`, `-F make_latest="$LATEST"`), wantIn: "want the check and publication of the draft: run is"},
		{name: "the publish step without the true or false guard on LATEST", workflow: in("publish", `          case "$LATEST" in
            true|false) ;;
            *)
              echo "latest is '$LATEST', want true or false" >&2
              exit 1
              ;;
          esac
`, ""), wantIn: "want the check and publication of the draft: run is"},
		// The publication itself (issues #156, #163): the publish job publishes
		// the draft it checked, by the id the API listed it with, and a tag that
		// already holds a published release is refused.
		{name: "the publish job publishing nothing", workflow: in("publish", publishByID, `gh release view "$TAG"`), wantIn: "-X PATCH"},
		{name: "the publish job leaving the release a draft", workflow: in("publish", "-F draft=false", "-F draft=true"), wantIn: "-F draft=false"},
		{name: "the publication by tag instead of by the checked draft's id", workflow: in("publish", publishByID, `gh release edit "$TAG" --repo timothymamo/shift2eu --draft=false`), wantIn: "releases/$id"},
		{name: "the publication of another release", workflow: in("publish", "releases/$id", "releases/tags/latest"), wantIn: "releases/$id"},
		{name: "the publication of another repository", workflow: in("publish", "repos/timothymamo/shift2eu/releases/$id", "repos/example/shift2eu/releases/$id"), wantIn: "repos/timothymamo/shift2eu/releases/$id"},
		{name: "the publish job's tag pasted into the script", workflow: in("publish", "-F draft=false", "-F name=${{ github.ref_name }}"), wantIn: "want the check and publication of the draft: run holds a ${{ }} expression"},
		{name: "the publish job without the token from env", workflow: in("publish", "          TAG: ${{ github.ref_name }}\n          LATEST: ${{ needs.latest.outputs.latest }}\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n        run: |\n          set -euo pipefail\n", "          TAG: ${{ github.ref_name }}\n          LATEST: ${{ needs.latest.outputs.latest }}\n        run: |\n          set -euo pipefail\n"), wantIn: "env holds"},
		{name: "the publish job without the latest decision from env", workflow: in("publish", "          LATEST: ${{ needs.latest.outputs.latest }}\n", ""), wantIn: "env holds"},
		// An immutable release locks its assets when it is published (issue
		// #156), so the draft is checked against the attested checksums.txt in
		// the same step that publishes it: its asset names and digests must be
		// the ones that file lists, its checksums.txt the attested one, and a
		// tag with a release already published is refused rather than published
		// again (reviewer S2 and auditor 1, issue #163).
		{name: "no check and publication of the draft", workflow: steps("publish", withoutStep(t, publishSteps, "name: Check the draft's assets and publish it")), wantIn: "jobs: publish: steps holds 1 steps, want exactly 2"},
		{name: "the attested checksums.txt not downloaded before publishing", workflow: steps("publish", withoutStep(t, publishSteps, "actions/download-artifact@")), wantIn: "jobs: publish: steps holds 1 steps, want exactly 2"},
		{name: "the publication before the download of checksums.txt", workflow: steps("publish", swappedSteps(t, publishSteps, "actions/download-artifact@", "name: Check the draft's assets and publish it")), wantIn: "jobs: publish: steps: step 1"},
		{name: "publish without the check that the tag holds no published release", workflow: in("publish", publishExistingReleaseCheck, ""), wantIn: "already publishes a release for $TAG"},
		{name: "a draft check of the names alone", workflow: in("publish", "          # Every asset's digest is the one checksums.txt lists: the bytes the\n          # attestation covers are the bytes that are published.\n          while read -r hash name; do printf 'sha256:%s %s\\n' \"$hash\" \"$name\"; done < checksums.txt | sort > \"$tmp/attested-digests.txt\"\n", ""), wantIn: "the draft release for $TAG holds an asset whose digest checksums.txt does not list"},
		{name: "a draft check that trusts the draft's checksums.txt", workflow: in("publish", "            echo \"the draft release's checksums.txt is not the one this run attested\" >&2\n", "            echo \"the draft release's checksums.txt is the attested one\" >&2\n"), wantIn: "the draft release's checksums.txt is not the one this run attested"},
		{name: "a separate publish step by tag", workflow: steps("publish", slices.Concat(publishSteps, []string{publishByTagStep})), wantIn: "jobs: publish: steps holds 3 steps, want exactly 2"},
		{name: "an extra step in the publish job", workflow: steps("publish", beforeStep(t, publishSteps, "      - name: Check the draft's assets and publish it", "run: ./scripts/prepare.sh")), wantIn: "jobs: publish: steps holds 3 steps, want exactly 2"},
		{name: "the publish job checking the repository out again", workflow: steps("publish", beforeStep(t, publishSteps, "      - uses: actions/download-artifact@", "uses: {{pin actions/checkout}}\n        with:\n          fetch-depth: 0\n          persist-credentials: false")), wantIn: "jobs: publish: steps holds 3 steps, want exactly 2"},
		{name: "the publish step in the goreleaser job", workflow: steps("goreleaser", beforeStep(t, goreleaserSteps, "      - uses: actions/upload-artifact@", "name: Check the draft's assets and publish it\n        run: "+publishByID)), wantIn: "jobs: goreleaser: steps holds 7 steps, want exactly 6"},
		{name: "the publish step in the test job", workflow: inTestJob("name: Check the draft's assets and publish it\n        run: " + publishByID), wantIn: "holds 5 steps, want exactly 4"},

		// A partial re-run (gh run rerun --failed) does not re-run the
		// successful latest job, so a decision its first attempt made can reach
		// the publish step after GitHub's "Latest" release moved to a higher
		// tag (reviewer, issue #173). While LATEST is true, taking "Latest" over
		// from a higher release is refused before anything is read or
		// published, and the release stays a draft; only the API's 404 is no
		// latest release, and any other failed read fails the step.
		{name: "publish without the stale-decision check", workflow: in("publish", publishStaleDecisionCheck, ""), wantIn: `esac\ntmp=\"$(mktemp -d)\"`},
		{name: "the stale-decision check comparing the tags without sort -V", workflow: in("publish", "| sort -V | tail -n 1", "| sort | tail -n 1"), wantIn: "sort | tail -n 1"},
		{name: "the stale-decision check reading any failed read as no latest release", workflow: in("publish", `            elif grep -qF -e "HTTP 404" "$err"; then`, `            elif true; then`), wantIn: "elif true; then"},
		{name: "the stale-decision check after the publication", workflow: afterPublish(), wantIn: `make_latest=\"$LATEST\"\n# A partial re-run`},
		{name: "the stale-decision check reading another repository's latest release", workflow: in("publish", `gh api "repos/timothymamo/shift2eu/releases/latest" --jq '.tag_name'`, `gh api "repos/example/shift2eu/releases/latest" --jq '.tag_name'`), wantIn: "repos/example/shift2eu/releases/latest"},

		// Whether the tag is the highest release tag on main is decided once per
		// run, in a job of its own (issue #173), which both image jobs and the
		// publish job read. There the decision cannot be made twice from two
		// checkouts at two moments of one run, so a tag pushed in between cannot
		// leave the image's latest tag and GitHub's "Latest" release naming
		// different versions. The job checks the repository out read-only, runs
		// git, sed and sort alone, and holds the decision as its only output.
		{name: "no latest job", workflow: release.withoutJob(t, "latest"), wantIn: `holds no "latest"`},
		{name: "the latest job on another runner", workflow: in("latest", "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-24.04\n"), wantIn: "jobs: latest: runs-on is"},
		{name: "the latest job without needs", workflow: in("latest", "    needs: test\n", ""), wantIn: `jobs: latest holds no "needs"`},
		{name: "the latest job not needing the tests", workflow: in("latest", "    needs: test\n", "    needs: goreleaser\n"), wantIn: "jobs: latest: needs is"},
		{name: "the latest job deciding after the images are built", workflow: in("latest", "    needs: test\n", "    needs: [test, goreleaser]\n"), wantIn: "jobs: latest: needs is"},
		{name: "contents: write for the latest job", workflow: in("latest", "      contents: read # check out the repository\n", "      contents: write # check out the repository\n"), wantIn: "jobs: latest: permissions: contents is"},
		{name: "packages: write for the latest job", workflow: in("latest", "      contents: read # check out the repository\n", "      contents: read # check out the repository\n      packages: write # push\n"), wantIn: `jobs: latest: permissions holds the key "packages"`},
		{name: "no contents: read for the latest job", workflow: in("latest", "      contents: read # check out the repository\n", ""), wantIn: "jobs: latest: permissions is empty, want exactly contents: read"},
		{name: "the latest job without its output", workflow: in("latest", "    outputs:\n      latest: ${{ steps.decide.outputs.latest }}\n", ""), wantIn: `jobs: latest holds no "outputs"`},
		{name: "another latest output", workflow: in("latest", "      latest: ${{ steps.decide.outputs.latest }}\n", "      latest: ${{ steps.decide.outputs.highest }}\n"), wantIn: "jobs: latest: outputs: latest is"},
		{name: "the latest job keeping the credentials", workflow: in("latest", "          persist-credentials: false\n", "          persist-credentials: true\n"), wantIn: "jobs: latest: steps: step 1"},
		{name: "the latest job without the whole history", workflow: in("latest", "          fetch-depth: 0\n", ""), wantIn: "jobs: latest: steps: step 1"},
		{name: "an extra step in the latest job", workflow: steps("latest", beforeStep(t, latestSteps, "      - name: Decide whether the tag is the highest release tag on main", "run: ./scripts/prepare.sh")), wantIn: "jobs: latest: steps holds 3 steps, want exactly 2"},
		{name: "a missing step in the latest job", workflow: steps("latest", withoutStep(t, latestSteps, "name: Decide whether the tag is the highest release tag on main")), wantIn: "jobs: latest: steps holds 1 steps, want exactly 2"},
		{name: "the latest check without the origin/main guard", workflow: in("latest", latestGuard, ""), wantIn: "want the decision of the latest release: run is"},
		{name: "the latest check whose guard does not exit", workflow: in("latest", "            exit 1\n", "            exit 0\n"), wantIn: "want the decision of the latest release: run is"},
		// Only the highest release tag on main moves latest, in the same filter,
		// sort and pick the verify jobs read independently: without the ancestry
		// filter a stray tag off main would hold latest back, without the
		// pre-release filter a pre-release would take it, and a lexical sort
		// would pick another tag.
		{name: "the latest check counting every tag, not only those merged into main", workflow: in("latest", "git tag -l 'v*.*.*' --merged refs/remotes/origin/main", "git tag -l 'v*.*.*'"), wantIn: "want the decision of the latest release: run is"},
		{name: "the latest check counting the tags of another branch", workflow: in("latest", "--merged refs/remotes/origin/main", "--merged refs/heads/main"), wantIn: "want the decision of the latest release: run is"},
		{name: "the latest check sorting the tags without sort -V", workflow: in("latest", "| sort -V | tail -n 1", "| sort | tail -n 1"), wantIn: "want the decision of the latest release: run is"},
		{name: "the latest check without the pre-release filter", workflow: in("latest", `| sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p'`, ""), wantIn: "want the decision of the latest release: run is"},
		{name: "the latest check with latest always", workflow: in("latest", "          latest=false\n", "          latest=true\n"), wantIn: "want the decision of the latest release: run is"},
		{name: "the latest check with the decision pasted into the script", workflow: in("latest", `if [ "$TAG" = "$highest" ]; then latest=true; fi`, `if [ "${{ github.ref_name }}" = "$highest" ]; then latest=true; fi`), wantIn: "want the decision of the latest release: run holds a ${{ }} expression"},

		// The jobs are a closed list: the image that publishes, the SBOM that
		// reads, the sign job that signs.
		{name: "no image job", workflow: release.withoutJob(t, "image"), wantIn: `no "image"`},
		{name: "no sbom job", workflow: release.withoutJob(t, "sbom"), wantIn: `no "sbom"`},
		{name: "no sign job", workflow: release.withoutJob(t, "sign"), wantIn: `no "sign"`},

		// Publishing waits for the release: the image job needs the tests,
		// goreleaser and the latest decision, the SBOM job the image, the sign job
		// both the image and its SBOM.
		{name: "the image job needing only the tests", workflow: in("image", "    needs: [test, goreleaser, latest]\n", "    needs: [test]\n"), wantIn: "jobs: image: needs is"},
		{name: "the image job needing goreleaser first", workflow: in("image", "    needs: [test, goreleaser, latest]\n", "    needs: [goreleaser, test, latest]\n"), wantIn: "jobs: image: needs is"},
		{name: "the latest job not needed by the image job", workflow: in("image", "    needs: [test, goreleaser, latest]\n", "    needs: [test, goreleaser]\n"), wantIn: `jobs: image: needs is ["test" "goreleaser"], want exactly ["test" "goreleaser" "latest"]`},
		{name: "the sbom job needing the sign job", workflow: in("sbom", "    needs: image\n", "    needs: sign\n"), wantIn: "needs"},
		{name: "the sign job needing only the image", workflow: in("sign", "    needs: [image, sbom]\n", "    needs: [image]\n"), wantIn: "needs"},
		{name: "the sign job needing the tests too", workflow: in("sign", "    needs: [image, sbom]\n", "    needs: [image, sbom, test]\n"), wantIn: "needs"},

		// The image job publishes and nothing more: contents: read, packages:
		// write, and no OIDC token or attestation scope, so nothing in it can
		// sign.
		{name: "the image job on another runner", workflow: in("image", "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-24.04\n"), wantIn: "runs-on"},
		{name: "the image job without needs", workflow: in("image", "    needs: [test, goreleaser, latest]\n", ""), wantIn: `no "needs"`},
		{name: "a job-level if on the image job", workflow: in("image", "    needs: [test, goreleaser, latest]\n", "    needs: [test, goreleaser, latest]\n    if: true\n"), wantIn: `key "if"`},
		{name: "contents: write for the image job", workflow: in("image", "      contents: read # check out the repository\n", "      contents: write # check out the repository\n"), wantIn: "contents"},
		{name: "id-token: write for the image job", workflow: in("image", "      packages: write # push the image to ghcr.io\n", "      packages: write # push the image to ghcr.io\n      id-token: write # sign\n"), wantIn: "id-token"},
		{name: "attestations: write for the image job", workflow: in("image", "      packages: write # push the image to ghcr.io\n", "      packages: write # push the image to ghcr.io\n      attestations: write # store them\n"), wantIn: "attestations"},
		{name: "an extra image permission", workflow: in("image", "      packages: write # push the image to ghcr.io\n", "      packages: write # push the image to ghcr.io\n      artifact-metadata: write # an organisation API\n"), wantIn: "artifact-metadata"},
		{name: "no packages: write for the image job", workflow: in("image", "      packages: write # push the image to ghcr.io\n", ""), wantIn: "packages"},

		// The goreleaser job keeps contents: write alone.
		{name: "packages: write for goreleaser too", workflow: in("goreleaser", "      contents: write # create the GitHub release and upload artifacts\n", "      contents: write # create the GitHub release and upload artifacts\n      packages: write # push the image\n"), wantIn: "packages"},
		{name: "id-token: write for goreleaser too", workflow: in("goreleaser", "      contents: write # create the GitHub release and upload artifacts\n", "      contents: write # create the GitHub release and upload artifacts\n      id-token: write # sign\n"), wantIn: "id-token"},
		{name: "no contents: write for goreleaser", workflow: in("goreleaser", "      contents: write # create the GitHub release and upload artifacts\n", ""), wantIn: "contents"},

		// The SBOM job only reads the registry: syft is third-party code, so
		// packages: read alone and no OIDC token.
		{name: "the sbom job on another runner", workflow: in("sbom", "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-24.04\n"), wantIn: "runs-on"},
		{name: "the sbom job without needs", workflow: in("sbom", "    needs: image\n", ""), wantIn: `no "needs"`},
		{name: "packages: write for the sbom job", workflow: in("sbom", "      packages: read # pull the image from ghcr.io, private until made public\n", "      packages: write # pull the image from ghcr.io, private until made public\n"), wantIn: "packages"},
		{name: "contents: read for the sbom job", workflow: in("sbom", "      packages: read # pull the image from ghcr.io, private until made public\n", "      contents: read # a checkout\n      packages: read # pull the image from ghcr.io, private until made public\n"), wantIn: "contents"},
		{name: "id-token: write for the sbom job", workflow: in("sbom", "      packages: read # pull the image from ghcr.io, private until made public\n", "      packages: read # pull the image from ghcr.io, private until made public\n      id-token: write # sign\n"), wantIn: "id-token"},

		// The image job's steps are a closed list too.
		{name: "an extra step in the image job", workflow: steps("image", beforeStep(t, imageSteps, "      - uses: docker/setup-buildx-action@", "run: ./scripts/prepare.sh")), wantIn: "holds 7 steps, want exactly 6"},
		{name: "a missing step in the image job", workflow: steps("image", withoutStep(t, imageSteps, "docker/login-action@")), wantIn: "holds 5 steps, want exactly 6"},
		{name: "two image steps swapped", workflow: steps("image", swappedSteps(t, imageSteps, "docker/setup-buildx-action@", "docker/login-action@")), wantIn: "want docker/setup-buildx-action: uses is"},
		{name: "build-push-action pinned by tag", workflow: in("image", "{{commit docker/build-push-action}}", "docker/build-push-action@v7"), wantIn: "build-push-action@v7"},
		{name: "setup-buildx-action pinned by tag", workflow: in("image", "{{commit docker/setup-buildx-action}}", "docker/setup-buildx-action@v4"), wantIn: "setup-buildx-action@v4"},
		{name: "metadata-action pinned by tag", workflow: in("image", "{{commit docker/metadata-action}}", "docker/metadata-action@v6"), wantIn: "metadata-action@v6"},
		{name: "login-action pinned by tag", workflow: in("image", "{{commit docker/login-action}}", "docker/login-action@v4"), wantIn: "login-action@v4"},

		// The image job's digest output: what the SBOM job reads the image by
		// and the sign job signs and attests.
		{name: "the image job without its outputs", workflow: in("image", "    outputs:\n      digest: ${{ steps.build.outputs.digest }}\n", ""), wantIn: `no "outputs"`},
		{name: "a second output on the image job", workflow: in("image", "      digest: ${{ steps.build.outputs.digest }}\n", "      digest: ${{ steps.build.outputs.digest }}\n      url: https://example.com\n"), wantIn: "outputs"},
		{name: "another digest output", workflow: in("image", "      digest: ${{ steps.build.outputs.digest }}\n", "      digest: ${{ steps.build.outputs.imageid }}\n"), wantIn: "digest"},

		// The build: two platforms, pushing, no cache in, BuildKit's own
		// attestations off, and nothing but the with: keys the check lists.
		{name: "a cache-from on the build", workflow: in("image", "          push: true\n", "          push: true\n          cache-from: type=gha\n"), wantIn: "cache-from"},
		{name: "a cache-to on the build", workflow: in("image", "          push: true\n", "          push: true\n          cache-to: type=gha,mode=max\n"), wantIn: "cache-to"},
		{name: "labels from metadata-action", workflow: in("image", "          push: true\n", "          push: true\n          labels: ${{ steps.tags.outputs.labels }}\n"), wantIn: "labels"},
		{name: "push: false", workflow: in("image", "          push: true\n", "          push: false\n"), wantIn: "push"},
		{name: "one platform", workflow: in("image", "          platforms: linux/amd64,linux/arm64\n", "          platforms: linux/amd64\n"), wantIn: "platforms"},
		{name: "a third platform", workflow: in("image", "          platforms: linux/amd64,linux/arm64\n", "          platforms: linux/amd64,linux/arm64,linux/arm/v7\n"), wantIn: "platforms"},
		{name: "another source annotation", workflow: in("image", "          annotations: index:org.opencontainers.image.source=https://github.com/timothymamo/shift2eu\n", "          annotations: index:org.opencontainers.image.source=https://example.com\n"), wantIn: "annotations"},
		{name: "BuildKit's provenance on", workflow: in("image", "          provenance: false\n", "          provenance: true\n"), wantIn: "with: provenance"},
		{name: "BuildKit's SBOM on", workflow: in("image", "          sbom: false\n", "          sbom: true\n"), wantIn: "with: sbom"},
		{name: "a build argument from git", workflow: in("image", "            VERSION=${{ steps.stamp.outputs.version }}\n", "            VERSION=$(git describe --tags)\n"), wantIn: "build-args"},

		// The stamp: the arguments GoReleaser stamps and the tag through env:.
		// Whether the tag is the latest release is the latest job's one decision
		// (issue #173), so the stamp neither reads the highest release tag nor
		// writes latest: a stamp that did either could disagree with GitHub's
		// "Latest" release, and with the other image.
		{name: "an extra stamp env", workflow: in("image", "          TAG: ${{ github.ref_name }}\n", "          TAG: ${{ github.ref_name }}\n          REF: ${{ github.ref }}\n"), wantIn: "want the stamp of the build arguments: env holds"},
		{
			name: "the image's stamp still computing the highest release tag",
			workflow: in("image", `          } >> "$GITHUB_OUTPUT"
`, `          highest="$(git tag -l 'v*.*.*' --merged refs/remotes/origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V | tail -n 1)"
          } >> "$GITHUB_OUTPUT"
`),
			wantIn: "want the stamp of the build arguments: run is",
		},
		{
			name: "the image's stamp writing its own latest",
			workflow: in("image", `          } >> "$GITHUB_OUTPUT"
`, `          echo "latest=true" >> "$GITHUB_OUTPUT"
          } >> "$GITHUB_OUTPUT"
`),
			wantIn: "want the stamp of the build arguments: run is",
		},
		{
			name:     "an interpolation inside a run script",
			workflow: in("image", `            echo "version=${TAG#v}"`+"\n", `            echo "version=${{ github.ref_name }}"`+"\n"),
			wantIn:   "holds a ${{ }} expression",
		},
		{name: "an extra line in the stamp script", workflow: in("image", "          } >> \"$GITHUB_OUTPUT\"\n", "          } >> \"$GITHUB_OUTPUT\"\n          echo sneaky=1\n"), wantIn: "run is"},
		{name: "a stamp script in a chomped block", workflow: in("image", "        run: |\n", "        run: |-\n"), wantIn: "run has lines below it, want one value on its line"},
		{name: "a stamp script in a folded block", workflow: in("image", "        run: |\n", "        run: >\n"), wantIn: "run has lines below it, want one value on its line"},
		{name: "a stamp script line indented less than its first", workflow: in("image", "          } >> \"$GITHUB_OUTPUT\"\n", "          } >> \"$GITHUB_OUTPUT\"\n         ; echo sneaky=1\n"), wantIn: "a line of run indented less than its first"},
		{name: "a comment line before the stamp script's first line", workflow: in("image", "        run: |\n", "        run: |\n          # a comment the reader must not treat as one\n"), wantIn: "run has a line before its first content line"},

		// The pins Renovate updates, by form: an exact release, an exact syft,
		// the BuildKit image by tag and multi-arch index digest.
		{name: "a BuildKit tag without a digest", workflow: in("image", "image=moby/buildkit:{{image moby/buildkit}}", "image=moby/buildkit:"+buildkitTag), wantIn: "driver-opts"},
		{name: "a BuildKit tag that is not an exact release", workflow: in("image", "image=moby/buildkit:"+buildkitTag+"@sha256:", "image=moby/buildkit:"+buildkitTag[:strings.LastIndex(buildkitTag, ".")]+"@sha256:"), wantIn: "driver-opts"},
		{name: "a BuildKit digest that is not 64 hex", workflow: in("image", "sha256:"+buildkitDigest, "sha256:"+buildkitDigest[:8]), wantIn: "driver-opts"},

		// Another registry, another image name: the checks below read the one
		// the release publishes.
		{name: "another registry", workflow: in("image", "          registry: ghcr.io\n", "          registry: docker.io\n"), wantIn: "registry"},
		{name: "another user", workflow: in("image", "          username: ${{ github.actor }}\n", "          username: someone\n"), wantIn: "username"},
		{name: "another image name", workflow: in("image", "          images: ghcr.io/timothymamo/shift2eu\n", "          images: ghcr.io/example/shift2eu\n"), wantIn: "images"},

		// latest comes from the latest job's one decision alone: no literal, no
		// other step, no latest=auto, and not the stamp's own output again.
		{name: "latest from the stamp again", workflow: in("image", "          flavor: latest=${{ needs.latest.outputs.latest }}\n", "          flavor: latest=${{ steps.stamp.outputs.latest }}\n"), wantIn: "want docker/metadata-action: with: flavor is"},
		{name: "latest always", workflow: in("image", "          flavor: latest=${{ needs.latest.outputs.latest }}\n", "          flavor: latest=true\n"), wantIn: "want docker/metadata-action: with: flavor is"},
		{name: "latest=auto", workflow: in("image", "          flavor: latest=${{ needs.latest.outputs.latest }}\n", "          flavor: latest=auto\n"), wantIn: "want docker/metadata-action: with: flavor is"},

		// The literal blocks the image job's tags, build arguments and stamp
		// script are: a line a YAML reader keeps as content (a # line, a blank
		// line, a line after the last one) is part of the value, and a block form
		// the reader does not follow (chomped |-, folded >, a line indented less
		// than the first) is refused, so none of them slips a line past the exact
		// value the check lists.
		{name: "a comment line inside the tags block", workflow: in("image", "            type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}\n", "            # a comment the reader must not treat as one\n            type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}\n"), wantIn: "with: tags"},
		{name: "a comment line after the build arguments", workflow: in("image", "            DATE=${{ steps.stamp.outputs.date }}\n", "            DATE=${{ steps.stamp.outputs.date }}\n            # a comment the reader must not treat as one\n"), wantIn: "with: build-args"},
		{name: "a comment line after the tags block content", workflow: in("image", "            type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}\n", "            type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}\n            # a trailing note\n"), wantIn: "with: tags"},
		{name: "a blank line inside the tags block", workflow: in("image", "            type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}\n", "\n            type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}\n"), wantIn: "with: tags"},
		{name: "another tag rule", workflow: in("image", "            type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}\n", "            type=semver,pattern={{major}}\n"), wantIn: "with: tags"},

		// The SBOM job's steps: log in read-only, the pushed index by digest,
		// and the SPDX document under the name the sign job downloads.
		{name: "an extra step in the sbom job", workflow: steps("sbom", beforeStep(t, sbomSteps, "      - uses: anchore/sbom-action@", "run: ./scripts/prepare.sh")), wantIn: "holds 4 steps, want exactly 3"},
		{name: "a missing step in the sbom job", workflow: steps("sbom", withoutStep(t, sbomSteps, "actions/upload-artifact@")), wantIn: "holds 2 steps, want exactly 3"},
		{name: "two sbom steps swapped", workflow: steps("sbom", swappedSteps(t, sbomSteps, "docker/login-action@", "anchore/sbom-action@")), wantIn: "want docker/login-action: uses is"},
		{name: "the SBOM read by tag", workflow: in("sbom", "image: registry:ghcr.io/timothymamo/shift2eu@${{ needs.image.outputs.digest }}", "image: registry:ghcr.io/timothymamo/shift2eu:latest"), wantIn: "with: image"},
		{name: "another SBOM format", workflow: in("sbom", "          format: spdx-json\n", "          format: cyclonedx-json\n"), wantIn: "format"},
		{name: "the SBOM uploaded to the run", workflow: in("sbom", "          upload-artifact: false\n", "          upload-artifact: true\n"), wantIn: "upload-artifact"},
		{name: "the SBOM uploaded to the release", workflow: in("sbom", "          upload-release-assets: false\n", "          upload-release-assets: true\n"), wantIn: "upload-release-assets"},
		{name: "a syft version range", workflow: in("sbom", "syft-version: {{release syft}}", "syft-version: ~> v1"), wantIn: "syft-version"},
		{name: "another SBOM artifact name", workflow: in("sbom", "          name: sbom\n", "          name: sboms\n"), wantIn: "name"},
		{name: "another SBOM artifact path", workflow: in("sbom", "          path: shift2eu.spdx.json\n", "          path: report.json\n"), wantIn: "path"},
		{name: "a tolerated missing SBOM", workflow: in("sbom", "          if-no-files-found: error\n", "          if-no-files-found: warn\n"), wantIn: "if-no-files-found"},
		{name: "a longer SBOM retention", workflow: in("sbom", "          retention-days: 1\n", "          retention-days: 7\n"), wantIn: "retention-days"},
		{name: "sbom-action pinned by tag", workflow: in("sbom", "{{commit anchore/sbom-action}}", "anchore/sbom-action@v0"), wantIn: "sbom-action@v0"},

		// The sign job: the signing identity, with GitHub's and sigstore's code
		// alone, no checkout, and everything by digest.
		{name: "the sign job on another runner", workflow: in("sign", "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-24.04\n"), wantIn: "runs-on"},
		{name: "the sign job without needs", workflow: in("sign", "    needs: [image, sbom]\n", ""), wantIn: `no "needs"`},
		{name: "an extra step in the sign job", workflow: steps("sign", beforeStep(t, signSteps, "      - uses: actions/download-artifact@", "uses: {{commit actions/checkout}}")), wantIn: "holds 7 steps, want exactly 6"},
		{name: "a checkout in place of the cosign installer", workflow: steps("sign", slices.Replace(slices.Clone(signSteps), 0, 1, "      - uses: {{pin actions/checkout}}\n")), wantIn: "cosign-installer"},
		{name: "a docker login in place of the cosign login", workflow: steps("sign", slices.Replace(slices.Clone(signSteps), 1, 2, "      - uses: {{pin docker/login-action}}\n        with:\n          registry: ghcr.io\n")), wantIn: `want the cosign login holds the key "uses"`},
		{name: "an anchore action in place of the provenance attestation", workflow: steps("sign", slices.Replace(slices.Clone(signSteps), 3, 4, "      - uses: {{pin anchore/sbom-action}}\n")), wantIn: `want actions/attest (the build provenance) holds no "with", want exactly name, uses, with`},
		{name: "two sign steps swapped", workflow: steps("sign", swappedSteps(t, signSteps, "name: Attest the build provenance", "actions/download-artifact@")), wantIn: `want actions/download-artifact holds the key "name"`},
		{name: "an upload in place of the artifact download", workflow: in("sign", "      - uses: {{pin actions/download-artifact}}\n", "      - uses: {{pin actions/upload-artifact}}\n"), wantIn: "download-artifact"},
		{name: "a cosign installer pinned by tag", workflow: in("sign", "{{commit sigstore/cosign-installer}}", "sigstore/cosign-installer@v4"), wantIn: "cosign-installer@v4"},
		{name: "a cosign release range", workflow: in("sign", "cosign-release: {{release cosign}}", "cosign-release: v3"), wantIn: "cosign-release"},

		// The login: the token and the actor through env:, never pasted into
		// the script, and nothing else in the environment.
		{name: "an extra env on the cosign login", workflow: in("sign", "          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n", "          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n          EXTRA: x\n"), wantIn: "env holds"},
		{
			name:     "the token pasted into the cosign login",
			workflow: in("sign", `run: printf '%s' "$GITHUB_TOKEN" | cosign login ghcr.io --username "$ACTOR" --password-stdin`, `run: printf '%s' "${{ secrets.GITHUB_TOKEN }}" | cosign login ghcr.io --username "$ACTOR" --password-stdin`),
			wantIn:   "holds a ${{ }} expression",
		},
		{name: "the cosign login without the actor from env", workflow: in("sign", `cosign login ghcr.io --username "$ACTOR"`, `cosign login ghcr.io --username "github-actions"`), wantIn: "run is"},

		// The signature: keyless, recursive, by digest, with the digest through
		// env:.
		{name: "the sign step without --recursive", workflow: in("sign", `run: cosign sign --yes --recursive "ghcr.io/timothymamo/shift2eu@${DIGEST}"`, `run: cosign sign --yes "ghcr.io/timothymamo/shift2eu@${DIGEST}"`), wantIn: "--recursive"},
		{name: "the sign step by tag", workflow: in("sign", `run: cosign sign --yes --recursive "ghcr.io/timothymamo/shift2eu@${DIGEST}"`, `run: cosign sign --yes --recursive "ghcr.io/timothymamo/shift2eu:${DIGEST}"`), wantIn: "@${DIGEST}"},
		{name: "the sign step without the digest env", workflow: in("sign", "        env:\n          DIGEST: ${{ needs.image.outputs.digest }}\n", ""), wantIn: `no "env"`},
		{name: "a shell on the sign step", workflow: in("sign", "        env:\n          DIGEST: ${{ needs.image.outputs.digest }}\n", "        shell: bash\n        env:\n          DIGEST: ${{ needs.image.outputs.digest }}\n"), wantIn: `key "shell"`},
		{name: "GOFLAGS on the sign step", workflow: in("sign", "          DIGEST: ${{ needs.image.outputs.digest }}\n", "          DIGEST: ${{ needs.image.outputs.digest }}\n          GOFLAGS: -tags=sneaky\n"), wantIn: `the signature: env holds ["DIGEST: ${{ needs.image.outputs.digest }}" "GOFLAGS: -tags=sneaky"]`},

		// The attestations: the image and its SBOM by digest, pushed to the
		// registry, and nothing stored in an organisation API.
		{name: "provenance not pushed to the registry", workflow: in("sign", "          push-to-registry: true\n          create-storage-record: false # an organisation API; needs artifact-metadata: write\n", "          push-to-registry: false\n          create-storage-record: false # an organisation API; needs artifact-metadata: write\n"), wantIn: "push-to-registry"},
		{name: "the SBOM attestation not pushed to the registry", workflow: in("sign", "          sbom-path: shift2eu.spdx.json\n          push-to-registry: true\n", "          sbom-path: shift2eu.spdx.json\n          push-to-registry: false\n"), wantIn: "push-to-registry"},
		{name: "the provenance attestation stored in the repository", workflow: in("sign", "          create-storage-record: false # an organisation API; needs artifact-metadata: write\n", "          create-storage-record: true # an organisation API; needs artifact-metadata: write\n"), wantIn: "create-storage-record"},
		{name: "the SBOM attestation stored in the repository", workflow: in("sign", "          push-to-registry: true\n          create-storage-record: false\n", "          push-to-registry: true\n          create-storage-record: true\n"), wantIn: "create-storage-record"},
		{name: "a provenance attestation of another subject", workflow: in("sign", "      - name: Attest the build provenance\n        uses: {{pin actions/attest}}\n        with:\n          subject-name: ghcr.io/timothymamo/shift2eu\n", "      - name: Attest the build provenance\n        uses: {{pin actions/attest}}\n        with:\n          subject-name: ghcr.io/example/shift2eu\n"), wantIn: "subject-name"},
		{name: "an SBOM attestation of another digest", workflow: in("sign", "          subject-digest: ${{ needs.image.outputs.digest }}\n          sbom-path: shift2eu.spdx.json\n", "          subject-digest: ${{ github.sha }}\n          sbom-path: shift2eu.spdx.json\n"), wantIn: "subject-digest"},
		{name: "an SBOM attestation without the SBOM", workflow: in("sign", "          sbom-path: shift2eu.spdx.json\n", ""), wantIn: `no "sbom-path"`},
		{name: "the provenance attested from a file", workflow: in("sign", "      - name: Attest the build provenance\n        uses: {{pin actions/attest}}\n        with:\n          subject-name: ghcr.io/timothymamo/shift2eu\n", "      - name: Attest the build provenance\n        uses: {{pin actions/attest}}\n        with:\n          subject-path: dist/shift2eu\n"), wantIn: "subject-path"},
		{name: "an artifact downloaded under another name", workflow: in("sign", "      - uses: {{pin actions/download-artifact}}\n        with:\n          name: sbom\n", "      - uses: {{pin actions/download-artifact}}\n        with:\n          name: shift2eu-sbom\n"), wantIn: "name"},
		{name: "another action in place of the artifact download", workflow: in("sign", "      - uses: {{pin actions/download-artifact}}\n", "      - uses: example/attest@0123456789abcdef0123456789abcdef01234567\n"), wantIn: "download-artifact"},

		// The HTTP API's image is gone with the server (issue #393): the release
		// publishes the CLI's image alone, so a fourth image job, or the jobs
		// that read, sign and run it, are refused as keys the release does not
		// hold, and for the names no line may use.
		{name: "a server-image job beside the image job", workflow: in("verify", "  # The independent check", "  server-image:\n    name: Container image (shift2eu-server)\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n  # The independent check"), wantIn: `jobs holds the key "server-image"`},
		{name: "a comment naming the HTTP API's binary", workflow: strings.Replace(releaseWorkflow, "# No permission by default; each job asks for its own.", "# The shift2eu-server jobs are gone (issue #393).\n# No permission by default; each job asks for its own.", 1), wantIn: "shift2eu-server"},
		{name: "a comment naming the bundled OpenAPI document", workflow: strings.Replace(releaseWorkflow, "# No permission by default; each job asks for its own.", "# The bundled api/openapi.bundled.json is gone (issue #393).\n# No permission by default; each job asks for its own.", 1), wantIn: "openapi"},
		// The name is retired in any case it is written in: a comment that
		// capitalises it names the same binary or document, and is refused too.
		{name: "a comment naming the HTTP API's binary in another case", workflow: strings.Replace(releaseWorkflow, "# No permission by default; each job asks for its own.", "# The Shift2eu-Server jobs are gone (issue #393).\n# No permission by default; each job asks for its own.", 1), wantIn: "shift2eu-server"},
		{name: "a comment naming the bundled API document in another case", workflow: strings.Replace(releaseWorkflow, "# No permission by default; each job asks for its own.", "# The bundled OpenAPI document is gone (issue #393).\n# No permission by default; each job asks for its own.", 1), wantIn: "openapi"},
		{name: "a target on the CLI image", workflow: in("image", "          context: .\n", "          context: .\n          target: server\n"), wantIn: `key "target"`},

		// The independent check of what the release published (issue #99): a job
		// of its own, after the image is built, signed and attested, read-only
		// to the registry and holding no OIDC token, which compares the tag to
		// the digest this run built, verifies the signature, the provenance and
		// the SBOM, runs the image and checks the tag rule. What it must say is a
		// closed list too.
		{name: "no verify job", workflow: release.withoutJob(t, "verify"), wantIn: `no "verify"`},
		{name: "the verify job needing only the sign job", workflow: in("verify", "    needs: [image, sign]\n", "    needs: [sign]\n"), wantIn: "needs"},
		{name: "the verify job needing the jobs in the other order", workflow: in("verify", "    needs: [image, sign]\n", "    needs: [sign, image]\n"), wantIn: "needs"},

		// The built digest comes from the image job, not from the signature:
		// without it the job cannot tell the digest this run built from the
		// one the tag resolves to now, and a tag moved between the build and
		// the check would pass.
		{name: "the verify job without the image job", workflow: in("verify", "    needs: [image, sign]\n", "    needs: [sign, sbom]\n"), wantIn: "needs"},
		{name: "the verify job needing the tests too", workflow: in("verify", "    needs: [image, sign]\n", "    needs: [image, sign, test]\n"), wantIn: "needs"},
		{name: "the verify job without needs", workflow: in("verify", "    needs: [image, sign]\n", ""), wantIn: `no "needs"`},

		// It may read the registry and the repository's attestations and nothing
		// more: no OIDC token, so nothing in it can sign an image of its own, and
		// no scope it omits, which a job-level permissions block sets to none.
		{name: "the verify job on a self-hosted runner", workflow: in("verify", "    runs-on: ubuntu-latest\n", "    runs-on: self-hosted\n"), wantIn: "runs-on"},
		{name: "the verify job without permissions", workflow: in("verify", "    permissions:\n      packages: read # the image and its signature\n      attestations: read # gh attestation verify reads them through the repository's API\n", ""), wantIn: `no "permissions"`},
		{name: "packages: write for the verify job", workflow: in("verify", "      packages: read # the image and its signature\n", "      packages: write # the image and its signature\n"), wantIn: "packages"},
		{name: "no packages: read for the verify job", workflow: in("verify", "      packages: read # the image and its signature\n", ""), wantIn: "packages"},
		{name: "attestations: write for the verify job", workflow: in("verify", "      attestations: read # gh attestation verify reads them through the repository's API\n", "      attestations: write # gh attestation verify reads them through the repository's API\n"), wantIn: "attestations"},
		{name: "no attestations: read for the verify job", workflow: in("verify", "      attestations: read # gh attestation verify reads them through the repository's API\n", ""), wantIn: "attestations"},
		{name: "contents: read for the verify job", workflow: in("verify", "      attestations: read # gh attestation verify reads them through the repository's API\n", "      attestations: read # gh attestation verify reads them through the repository's API\n      contents: read # a checkout\n"), wantIn: "contents"},
		{name: "id-token: write for the verify job", workflow: in("verify", "      attestations: read # gh attestation verify reads them through the repository's API\n", "      attestations: read # gh attestation verify reads them through the repository's API\n      id-token: write # sign\n"), wantIn: "id-token"},
		{name: "id-token: none for the verify job", workflow: in("verify", "      attestations: read # gh attestation verify reads them through the repository's API\n", "      attestations: read # gh attestation verify reads them through the repository's API\n      id-token: none # nothing to sign\n"), wantIn: "id-token"},

		// Its steps are a closed list: cosign by exact release, a cosign login,
		// the check of the image, and the tag rule, and nothing else. No
		// checkout runs repository code, and every action is pinned by commit.
		{name: "an extra step in the verify job", workflow: steps("verify", beforeStep(t, verifySteps, "      - name: Log in to ghcr.io", "run: ./scripts/prepare.sh")), wantIn: "holds 5 steps, want exactly 4"},
		{name: "a checkout in the verify job", workflow: steps("verify", beforeStep(t, verifySteps, "      - name: Log in to ghcr.io", "uses: {{commit actions/checkout}}")), wantIn: "holds 5 steps, want exactly 4"},
		{name: "a missing step in the verify job", workflow: steps("verify", withoutStep(t, verifySteps, "name: Log in to ghcr.io")), wantIn: "holds 3 steps, want exactly 4"},
		{name: "two verify steps swapped", workflow: steps("verify", swappedSteps(t, verifySteps, "name: Verify the CLI image", "name: The tags follow the release rule")), wantIn: "want the CLI image's verification: name is"},
		{name: "a cosign installer pinned by tag in the verify job", workflow: in("verify", "{{commit sigstore/cosign-installer}}", "sigstore/cosign-installer@v4"), wantIn: "cosign-installer@v4"},
		{name: "a cosign release range in the verify job", workflow: in("verify", "cosign-release: {{release cosign}}", "cosign-release: v3"), wantIn: "cosign-release"},

		// The published image is resolved to its digest and used by digest.
		{name: "the CLI image verified by tag, not by digest", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", `IMAGE="ghcr.io/timothymamo/shift2eu@$DIGEST"`, `IMAGE="ghcr.io/timothymamo/shift2eu:$TAG"`)), wantIn: "want the CLI image's verification: run is"},
		{name: "the CLI image's tag resolved from no registry", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", `DIGEST="$(docker buildx imagetools inspect "ghcr.io/timothymamo/shift2eu:$TAG" --format '{{.Manifest.Digest}}')"`, `DIGEST="latest"`)), wantIn: "want the CLI image's verification: run is"},

		// The tag must resolve to the digest this run built (audit L1): the
		// digest the image job pushed, that the sign job signed and attested.
		{name: "the CLI image's tag compared to no built digest", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "          if [ \"$DIGEST\" != \"$BUILT\" ]; then\n            echo \"ghcr.io/timothymamo/shift2eu:$TAG resolves to $DIGEST, not the digest this run built, $BUILT\" >&2\n            exit 1\n          fi\n", "")), wantIn: "want the CLI image's verification: run is"},
		{name: "the tag rule's CLI digest from another job", workflow: in("verify", "          CLI_BUILT: ${{ needs.image.outputs.digest }}\n", "          CLI_BUILT: ${{ needs.goreleaser.outputs.digest }}\n"), wantIn: "env holds"},

		// The signature: made by this workflow, for exactly this tag, on a push
		// of it, by this commit (audit L2), with GitHub's token as the issuer.
		{name: "the CLI image verified without the certificate identity", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "            --certificate-identity \"https://github.com/timothymamo/shift2eu/.github/workflows/release.yml@refs/tags/$TAG\" \\\n", "")), wantIn: "want the CLI image's verification: run is"},
		{name: "the CLI image's certificate identity naming another ref", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "release.yml@refs/tags/$TAG", "release.yml@refs/heads/main")), wantIn: "want the CLI image's verification: run is"},
		{name: "the CLI image verified without the OIDC issuer", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "            --certificate-oidc-issuer https://token.actions.githubusercontent.com \\\n", "")), wantIn: "want the CLI image's verification: run is"},
		{name: "the CLI image verified without the push trigger", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "            --certificate-github-workflow-trigger push \\\n", "")), wantIn: "want the CLI image's verification: run is"},
		{name: "the CLI image verified without the certificate's commit", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "            --certificate-github-workflow-sha \"$GITHUB_SHA\"\n", "")), wantIn: "want the CLI image's verification: run is"},
		{name: "the CLI image's certificate naming another commit", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", `--certificate-github-workflow-sha "$GITHUB_SHA"`, `--certificate-github-workflow-sha "$TAG"`)), wantIn: "want the CLI image's verification: run is"},

		// The provenance and the SBOM: both attested from this workflow, for this
		// tag and this commit (audit L2), on GitHub's runners, and read through
		// the repository's API.
		{name: "the provenance verified without the repository", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "--repo timothymamo/shift2eu", "--repo example/shift2eu")), wantIn: "want the CLI image's verification: run is"},
		{name: "the provenance verified without the signer workflow", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "            --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \\\n", "")), wantIn: "want the CLI image's verification: run is"},
		{name: "the provenance verified without the source ref", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", `--source-ref "refs/tags/$TAG" --source-digest "$GITHUB_SHA" `, "")), wantIn: "want the CLI image's verification: run is"},
		{name: "the provenance verified without the source digest", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", `--source-ref "refs/tags/$TAG" --source-digest "$GITHUB_SHA" --deny-self-hosted-runners`+"\n", `--source-ref "refs/tags/$TAG" --deny-self-hosted-runners`+"\n")), wantIn: "want the CLI image's verification: run is"},
		{name: "the SBOM verified without the source digest", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", `--source-digest "$GITHUB_SHA" --deny-self-hosted-runners \`+"\n", `--deny-self-hosted-runners \\`+"\n")), wantIn: "want the CLI image's verification: run is"},
		{name: "self-hosted runners not denied", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "--deny-self-hosted-runners", "--no-deny")), wantIn: "want the CLI image's verification: run is"},
		{name: "the SBOM attested under another predicate type", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "https://spdx.dev/Document/v2.3", "https://spdx.dev/Document/v2.2")), wantIn: "want the CLI image's verification: run is"},

		// The image itself: run with a read-only root filesystem and no network,
		// its version compared to the tag's.
		{name: "the CLI image run without --network none", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", `--network none "$IMAGE" version`, `"$IMAGE" version`)), wantIn: "want the CLI image's verification: run is"},
		{name: "the CLI image run without --read-only", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", `docker run --rm --read-only --network none "$IMAGE" version`, `docker run --rm --network none "$IMAGE" version`)), wantIn: "want the CLI image's verification: run is"},
		{name: "the CLI image run without --rm", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", `docker run --rm --read-only --network none "$IMAGE" version`, `docker run --read-only --network none "$IMAGE" version`)), wantIn: "want the CLI image's verification: run is"},
		{name: "the CLI image's version not compared to the tag's", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", `grep -qF "shift2eu ${TAG#v} (" version.txt`, `true`)), wantIn: "want the CLI image's verification: run is"},

		// The tag rule (ADR 0015): vX.Y.Z always, X.Y for every release, latest
		// for the highest release tag alone, and a pre-release moving neither.
		{name: "the tag rule without the latest tag", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `"$image:latest"`, `"$image:newest"`)), wantIn: "want the tag rule: run is"},
		{name: "the tag rule without the minor tag", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `"$image:$minor"`, `"$image:$TAG"`)), wantIn: "want the tag rule: run is"},
		{name: "the tag rule without the pre-release case", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", "              *-*)\n", "              x-*)\n")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule checking another image too", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", "          check ghcr.io/timothymamo/shift2eu \"$CLI_BUILT\"\n", "          check ghcr.io/timothymamo/shift2eu \"$CLI_BUILT\"\n          check ghcr.io/example/shift2eu \"$CLI_BUILT\"\n")), wantIn: "want the tag rule: run is"},

		// The tag rule's branch for a pre-release (review B1, audit M1): a
		// pre-release must not move X.Y or latest, but their existence is fine,
		// so the rule compares digests rather than asking whether the tag
		// exists. A pre-release on a minor line that has already shipped is the
		// common case, and it must pass.
		{name: "the pre-release rule back to a tag that merely exists", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", "                if [ \"$minor_digest\" = \"$digest\" ]; then\n", "                if [ -n \"$minor_digest\" ]; then\n")), wantIn: "want the tag rule: run is"},
		{name: "the pre-release rule without its X.Y check", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", "                if [ \"$minor_digest\" = \"$digest\" ]; then\n", "                if false; then\n")), wantIn: "want the tag rule: run is"},
		{name: "the pre-release rule without its latest check", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", "                if [ \"$latest_digest\" = \"$digest\" ]; then\n", "                if false; then\n")), wantIn: "want the tag rule: run is"},

		// Only absence is absence (review N1, audit N1): each of the rule's
		// reads treats a tag the registry says does not exist as absent, which
		// the rule allows where it allows it, and fails the step on any other
		// answer. Every read is pinned alone, so none can go back to swallowing
		// a 5xx, a rate limit or a failed login, and a pre-release may still
		// leave X.Y or latest absent. Absence is the registry's own not-found
		// for this reference alone: the fixed, case-sensitive "$1: not found"
		// text on exit status 1, and nothing else, so MANIFEST_UNKNOWN stays a
		// failure and never an absent tag.
		{name: "the tag rule taking any registry error for an absent tag", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `              if [ "$rc" -eq 1 ] && grep -qF -e "$1: not found" "$err"; then`+"\n", "              if true; then\n")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule reading MANIFEST_UNKNOWN as an absent tag again", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `grep -qF -e "$1: not found" "$err"`, `grep -qF -e "$1: not found" -e 'MANIFEST_UNKNOWN' "$err"`)), wantIn: "want the tag rule: run is"},

		// The digest is stdout alone, one sha256 line (audit F2, review N1):
		// stderr of its own, so a docker warning cannot be read as the digest
		// an equality check then compares, and nothing but one sha256 line is
		// a digest; and absence is the registry's not-found for this very
		// reference with exit status 1 alone (audit F1, review S2), so a
		// missing docker, a broken credential helper, a 404 from the token
		// endpoint or another tag's not-found all fail the step.
		{name: "the tag rule's digest read with stderr merged into it", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `--format '{{.Manifest.Digest}}' 2> "$err")"`, `--format '{{.Manifest.Digest}}' 2>&1)"`)), wantIn: "want the tag rule: run is"},
		{name: "the tag rule without the check that a digest is one sha256 line", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `              if [[ "$out" =~ ^sha256:[0-9a-f]{64}$ ]]; then`+"\n", "              if [ -n \"$out\" ]; then\n")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule taking any not-found for an absent tag", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `grep -qF -e "$1: not found" "$err"`, `grep -qF -e "not found" "$err"`)), wantIn: "want the tag rule: run is"},
		{name: "the tag rule's absent-tag match ignoring case again", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `              if [ "$rc" -eq 1 ] && grep -qF -e "$1: not found" "$err"; then`+"\n", `              if [ "$rc" -eq 1 ] && grep -qiF -e "$1: not found" "$err"; then`+"\n")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule reading any failed inspect's stderr as an absent tag", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `              if [ "$rc" -eq 1 ] && grep -qF -e "$1: not found" "$err"; then`+"\n", `              if grep -qF -e "$1: not found" "$err"; then`+"\n")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule reading a pre-release's X.Y with its error silenced", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `            minor_digest="$(digest_or_absent "$image:$minor")"`+"\n", `            minor_digest="$(digest_or_absent "$image:$minor" 2> /dev/null)"`+"\n")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule reading a pre-release's latest with its error silenced", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `                latest_digest="$(digest_or_absent "$image:latest")"`+"\n", `                latest_digest="$(digest_or_absent "$image:latest" 2> /dev/null)"`+"\n")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule reading the highest with its error silenced", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `                  highest_digest="$(digest_or_absent "$image:$highest")"`+"\n", `                  highest_digest="$(digest_or_absent "$image:$highest" 2> /dev/null)"`+"\n")), wantIn: "want the tag rule: run is"},

		// The highest release tag is the one the stamp decided latest from: the
		// release tags merged into main, in the same filter, sort and pick
		// (review M1, audit M2a). Without the ancestry filter a stray tag off
		// main would be highest, and every later release's verify would fail.
		{name: "the tag rule counting every tag, not only those merged into main", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", "git -C tags tag -l 'v*.*.*' --merged refs/remotes/origin/main", "git -C tags tag -l 'v*.*.*'")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule counting the tags of another branch", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", "--merged refs/remotes/origin/main", "--merged refs/heads/main")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule fetching no tags", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", " '+refs/tags/v*:refs/tags/v*'", "")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule checking out the repository", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", "          git init -q tags\n", "          git clone -q --no-tags --depth 1 https://github.com/timothymamo/shift2eu.git tags\n")), wantIn: "want the tag rule: run is"},

		// The timing of the latest check (review M2, audit M2b): only a
		// published higher tag can hold latest back, so a re-run of an older
		// release passes once a newer one has shipped, and a newer tag whose
		// run is still in flight is no reason to fail.
		{name: "the tag rule without its notice for a highest tag not yet published", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `                    echo "::notice::$image:$highest is not published yet, so this run does not check $image:latest"`+"\n", "")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule without the check that latest is not this tag", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `                    if [ "$latest_digest" = "$digest" ]; then`+"\n", "                    if false; then\n")), wantIn: "want the tag rule: run is"},

		// Fail closed (review S1, audit L3): every script of the job opens with
		// set -euo pipefail, an empty highest is an error, and a registry error
		// is not read as an absent tag.
		{name: "the CLI image's verification without set -euo pipefail", workflow: steps("verify", inVerifyStep(t, "name: Verify the CLI image", "        run: |\n          set -euo pipefail\n", "        run: |\n")), wantIn: "want the CLI image's verification: run is"},
		{name: "the tag rule without set -euo pipefail", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", "        run: |\n          set -euo pipefail\n", "        run: |\n")), wantIn: "want the tag rule: run is"},
		{name: "a shell on the tag rule step", workflow: in("verify", "      - name: The tags follow the release rule\n", "      - name: The tags follow the release rule\n        shell: bash\n"), wantIn: `key "shell"`},
		{name: "the tag rule without the guard on an empty highest", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", "          if [ -z \"$highest\" ]; then\n            echo \"no release tag is merged into main, so X.Y and latest cannot be checked\" >&2\n            exit 1\n          fi\n", "")), wantIn: "want the tag rule: run is"},
		{name: "the tag rule reading the highest release's latest with its error silenced", workflow: steps("verify", inVerifyStep(t, "name: The tags follow the release rule", `                  latest_digest="$(digest_or_absent "$image:latest")"`+"\n", `                  latest_digest="$(digest_or_absent "$image:latest" 2> /dev/null)"`+"\n")), wantIn: "want the tag rule: run is"},

		// The attestation of the archives (issue #163): a job of its own attests
		// every archive and checksums.txt with GitHub build provenance, made by
		// this workflow on the tag, holding an OIDC token and the repository's
		// attestations to write and read-only access beside them: no contents:
		// write, so nothing in it can publish or replace the release. What it
		// attests is the whole release, so an archive family or checksums.txt
		// left out is refused, and so is a step other than GitHub's own
		// provenance action.
		{name: "no attest-archives job", workflow: release.withoutJob(t, "attest-archives"), wantIn: `no "attest-archives"`},
		{name: "the attest-archives job on another runner", workflow: in("attest-archives", "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-24.04\n"), wantIn: "runs-on"},
		{name: "the attest-archives job without needs", workflow: in("attest-archives", "    needs: [goreleaser, site]\n", ""), wantIn: `no "needs"`},
		{name: "the attest-archives job not needing goreleaser", workflow: in("attest-archives", "    needs: [goreleaser, site]\n", "    needs: [site]\n"), wantIn: "needs"},
		{name: "contents: write for the attest-archives job", workflow: in("attest-archives", "      contents: read # download the archives' workflow artifact\n", "      contents: write # download the archives' workflow artifact\n"), wantIn: "contents"},
		{name: "no contents: read for the attest-archives job", workflow: in("attest-archives", "      contents: read # download the archives' workflow artifact\n", ""), wantIn: "contents"},
		{name: "packages: read for the attest-archives job", workflow: in("attest-archives", "      id-token: write # the OIDC token the provenance is signed with\n", "      id-token: write # the OIDC token the provenance is signed with\n      packages: read # pull the images\n"), wantIn: "packages"},
		{name: "no id-token: write for the attest-archives job", workflow: in("attest-archives", "      id-token: write # the OIDC token the provenance is signed with\n", ""), wantIn: "id-token"},
		{name: "attestations: read for the attest-archives job", workflow: in("attest-archives", "      attestations: write # store the provenance in the repository\n", "      attestations: read # store the provenance in the repository\n"), wantIn: "attestations"},
		{name: "an extra step in the attest-archives job", workflow: steps("attest-archives", beforeStep(t, attestArchivesSteps, "      - name: Check the archives and the site bundle against checksums.txt", "run: ./scripts/prepare.sh")), wantIn: "holds 5 steps, want exactly 4"},
		{name: "a missing step in the attest-archives job", workflow: steps("attest-archives", withoutStep(t, attestArchivesSteps, "name: Check the archives and the site bundle against checksums.txt")), wantIn: "holds 3 steps, want exactly 4"},
		{name: "the archives not downloaded", workflow: in("attest-archives", "      - uses: {{pin actions/download-artifact}}\n        with:\n          name: archives\n", "      - uses: {{pin actions/upload-artifact}}\n        with:\n          name: archives\n"), wantIn: "download-artifact"},
		{name: "the archives downloaded under another name", workflow: in("attest-archives", "          name: archives\n", "          name: dist\n"), wantIn: "name"},
		{name: "the archives not checked against checksums.txt", workflow: in("attest-archives", "          sha256sum -c checksums.txt\n", "          sha256sum checksums.txt\n"), wantIn: "want the check of the archives and the site bundle against checksums.txt: run is"},
		{name: "the attest action pinned by tag", workflow: in("attest-archives", "{{commit actions/attest-build-provenance}}", "actions/attest-build-provenance@v4"), wantIn: "attest-build-provenance@v4"},
		{name: "another action in place of the provenance attestation", workflow: steps("attest-archives", slices.Replace(slices.Clone(attestArchivesSteps), 3, 4, "      - uses: {{pin actions/attest}}\n")), wantIn: "attest-build-provenance"},
		{name: "checksums.txt not attested", workflow: in("attest-archives", "            shift2eu-site_*.tar.gz\n            checksums.txt\n", "            shift2eu-site_*.tar.gz\n"), wantIn: "subject-path"},
		{name: "a bare shift2eu_* subject pattern", workflow: in("attest-archives", "            shift2eu_*.tar.gz\n", "            shift2eu_*\n"), wantIn: "subject-path"},
		{name: "the archives attested by digest instead", workflow: in("attest-archives", "          subject-path: |\n            shift2eu_*.tar.gz\n            shift2eu_*.zip\n            shift2eu-site_*.tar.gz\n            checksums.txt\n", "          subject-digest: sha256:0000000000000000000000000000000000000000000000000000000000000000\n"), wantIn: `no "subject-path"`},
		{name: "the archives not handed on by goreleaser", workflow: steps("goreleaser", withoutStep(t, goreleaserSteps, "actions/upload-artifact@")), wantIn: "holds 5 steps, want exactly 6"},
		// What the artifact hands on and what attest-archives attests is the
		// archives and checksums.txt. Nothing is copied into dist/ beside them:
		// the bundled OpenAPI document the release used to publish is gone
		// (issue #393), so a step that copies it back is refused.
		{
			name:     "the OpenAPI document put beside the archives again",
			workflow: steps("goreleaser", beforeStep(t, goreleaserSteps, "      - uses: actions/upload-artifact@", "name: Put the OpenAPI document beside the archives\n        run: cp api/openapi.bundled.json dist/shift2eu-server_openapi.json")),
			wantIn:   "holds 7 steps, want exactly 6",
		},
		// The archives artifact is kept a week, so a failed attestation or
		// publication is recovered by re-running the failed jobs; the SBOM
		// artifacts stay a day.
		{name: "the archives artifact kept one day", workflow: in("goreleaser", "          retention-days: 7\n", "          retention-days: 1\n"), wantIn: "retention-days"},

		// The independent check of the published archives (issue #163), beside
		// the image verify job: a fresh runner after publish, read-only to the
		// release and to the repository's attestations, which downloads every
		// asset of the release, refuses a release that is still a draft and one
		// that holds anything but checksums.txt and the files it lists, checks
		// their digests, and runs gh attestation verify on every downloaded file
		// itself, with the release's workflow, its tag and commit, and GitHub's
		// runners only.
		{name: "no verify-archives job", workflow: release.withoutJob(t, "verify-archives"), wantIn: `no "verify-archives"`},
		{name: "the verify-archives job on a self-hosted runner", workflow: in("verify-archives", "    runs-on: ubuntu-latest\n", "    runs-on: self-hosted\n"), wantIn: "runs-on"},
		{name: "the verify-archives job without needs", workflow: in("verify-archives", "    needs: publish\n", ""), wantIn: `no "needs"`},
		{name: "the verify-archives job not needing the publish job", workflow: in("verify-archives", "    needs: publish\n", "    needs: attest-archives\n"), wantIn: "needs"},
		{name: "the verify-archives job needing the attestation job too", workflow: in("verify-archives", "    needs: publish\n", "    needs: [attest-archives, publish]\n"), wantIn: "needs"},
		{name: "contents: write for the verify-archives job", workflow: in("verify-archives", "      contents: read # download the release's assets\n", "      contents: write # download the release's assets\n"), wantIn: "contents"},
		{name: "no contents: read for the verify-archives job", workflow: in("verify-archives", "      contents: read # download the release's assets\n", ""), wantIn: "contents"},
		{name: "attestations: write for the verify-archives job", workflow: in("verify-archives", "      attestations: read # gh attestation verify reads them through the repository's API\n", "      attestations: write # gh attestation verify reads them through the repository's API\n"), wantIn: "attestations"},
		{name: "id-token: write for the verify-archives job", workflow: in("verify-archives", "      attestations: read # gh attestation verify reads them through the repository's API\n", "      attestations: read # gh attestation verify reads them through the repository's API\n      id-token: write # sign\n"), wantIn: "id-token"},
		{name: "packages: read for the verify-archives job", workflow: in("verify-archives", "      attestations: read # gh attestation verify reads them through the repository's API\n", "      attestations: read # gh attestation verify reads them through the repository's API\n      packages: read # pull the images\n"), wantIn: "packages"},
		{name: "an extra step in the verify-archives job", workflow: steps("verify-archives", beforeStep(t, verifyArchivesSteps, "      - name: Download the published archives", "run: ./scripts/prepare.sh")), wantIn: "holds 4 steps, want exactly 3"},
		{name: "a checkout in the verify-archives job", workflow: steps("verify-archives", beforeStep(t, verifyArchivesSteps, "      - name: Download the published archives", "uses: {{commit actions/checkout}}")), wantIn: "holds 4 steps, want exactly 3"},
		{name: "a missing step in the verify-archives job", workflow: steps("verify-archives", withoutStep(t, verifyArchivesSteps, "name: Verify the published archives")), wantIn: "holds 2 steps, want exactly 3"},
		{name: "the published archives not downloaded", workflow: in("verify-archives", `gh release download "$TAG"`, `gh release view "$TAG"`), wantIn: `gh release download \"$TAG\" --repo timothymamo/shift2eu`},
		{name: "the download without --repo", workflow: in("verify-archives", `gh release download "$TAG" --repo timothymamo/shift2eu`, `gh release download "$TAG"`), wantIn: `want exactly "gh release download \"$TAG\" --repo timothymamo/shift2eu"`},
		{name: "a download of the expected patterns alone", workflow: in("verify-archives", `gh release download "$TAG" --repo timothymamo/shift2eu`, `gh release download "$TAG" --repo timothymamo/shift2eu --pattern 'shift2eu_*' --pattern checksums.txt`), wantIn: `want exactly "gh release download \"$TAG\" --repo timothymamo/shift2eu"`},
		// What the release holds is exactly checksums.txt and the files it
		// lists, and it is published: an asset the attestation does not cover,
		// or one the release never got, fails here rather than pass unverified.
		{name: "the release's file set not checked", workflow: in("verify-archives", "            echo \"the release of $TAG does not hold what checksums.txt lists:\" >&2\n", "            echo \"the release of $TAG holds exactly checksums.txt and its files\" >&2\n"), wantIn: "the release of $TAG does not hold what checksums.txt lists"},
		{name: "a release left a draft", workflow: in("verify-archives", "            echo \"the release of $TAG is still a draft, so it was never published\" >&2\n", "            echo \"the release of $TAG is published\" >&2\n"), wantIn: "the release of $TAG is still a draft, so it was never published"},
		{name: "the verification without set -euo pipefail", workflow: in("verify-archives", "        run: |\n          set -euo pipefail\n          if [", "        run: |\n          if ["), wantIn: "want the archives' verification: run is"},
		{name: "the digests not checked", workflow: in("verify-archives", "          sha256sum -c checksums.txt\n", ""), wantIn: "sha256sum -c checksums.txt"},
		{name: "a verification of another repository", workflow: in("verify-archives", `gh attestation verify "$subject" --repo timothymamo/shift2eu`, `gh attestation verify "$subject" --repo example/shift2eu`), wantIn: "--repo timothymamo/shift2eu"},
		{name: "the verification without the signer workflow", workflow: in("verify-archives", "--signer-workflow timothymamo/shift2eu/.github/workflows/release.yml", "--signer-workflow example/repo/.github/workflows/other.yml"), wantIn: "--signer-workflow timothymamo/shift2eu/.github/workflows/release.yml"},
		{name: "the verification without the source ref", workflow: in("verify-archives", `--source-ref "refs/tags/$TAG"`, `--source-ref "refs/heads/main"`), wantIn: `--source-ref \"refs/tags/$TAG\"`},
		{name: "the verification without the source digest", workflow: in("verify-archives", `--source-digest "$GITHUB_SHA"`, `--source-digest "$TAG"`), wantIn: `--source-digest \"$GITHUB_SHA\"`},
		{name: "self-hosted runners not denied", workflow: in("verify-archives", "--deny-self-hosted-runners", "--no-deny"), wantIn: "--deny-self-hosted-runners"},
		{name: "the verification without gh attestation verify", workflow: in("verify-archives", `gh attestation verify "$subject"`, `cosign verify "$subject"`), wantIn: `gh attestation verify \"$subject\"`},
		// Every downloaded file is a subject: the download directory holds the
		// release's assets alone, so no pattern of the workflow's own decides
		// what is verified, and a list that could leave one out is refused.
		{name: "the old glob list of subjects", workflow: in("verify-archives", "for subject in *; do", "for subject in shift2eu_* checksums.txt; do"), wantIn: "for subject in shift2eu_* checksums.txt"},
		{name: "a subject list leaving checksums.txt out", workflow: in("verify-archives", "for subject in *; do", "for subject in shift2eu_*; do"), wantIn: "for subject in shift2eu_*; do"},
		// A name that starts with "." is a file like any other, and an asset must
		// not be able to hide from the check or from the loop (reviewer N2, issue
		// #163): the file set and the subjects are both read with the dotfiles
		// in.
		{name: "the file set read without the dotfiles", workflow: in("verify-archives", "          find . -mindepth 1 -maxdepth 1 -printf '%P\\n' | sort > \"$tmp/downloaded.txt\"\n", "          ls -1 | sort > \"$tmp/downloaded.txt\"\n"), wantIn: "find . -mindepth 1 -maxdepth 1 -printf"},
		{name: "the subjects read without the dotfiles", workflow: in("verify-archives", "          shopt -s dotglob\n", ""), wantIn: "shopt -s dotglob"},

		// GitHub's "Latest" release is checked by the job that runs after
		// publish, against its own independent reading of the highest release
		// tag, which the image's latest tag follows (issue #173): a release that
		// is not the highest must not be GitHub's "Latest", a pre-release never
		// is one, and while the highest release tag has no release yet the check
		// is skipped with a notice, as the image latest check skips it. Each case
		// below names the step whose run it changes: a run mismatch prints the
		// whole script, so only the step tells the cases apart.
		{name: "no check of GitHub's latest release", workflow: steps("verify-archives", withoutStep(t, verifyArchivesSteps, "name: Check GitHub's latest release")), wantIn: "jobs: verify-archives: steps holds 2 steps, want exactly 3"},
		{name: "the latest release not compared to the tag", workflow: in("verify-archives", `echo "GitHub's latest release is '$latest', not $TAG, the highest release tag"`, `echo "GitHub's latest release is $latest"`), wantIn: "want the check of GitHub's latest release: run is"},
		{name: "the latest release not refused for a tag that is not the highest", workflow: in("verify-archives", `echo "GitHub's latest release is $TAG, which is not the highest release tag, $highest"`, `echo "GitHub's latest release is $TAG"`), wantIn: "want the check of GitHub's latest release: run is"},
		{name: "the latest check without the notice for a highest release not published yet", workflow: in("verify-archives", `echo "::notice::the release of $highest is not published yet, so this run does not check GitHub's latest release"`, `echo "the release of $highest is not published yet"`), wantIn: "want the check of GitHub's latest release: run is"},
		{name: "the latest check counting every tag, not only those merged into main", workflow: in("verify-archives", "git -C tags tag -l 'v*.*.*' --merged refs/remotes/origin/main", "git -C tags tag -l 'v*.*.*'"), wantIn: "want the check of GitHub's latest release: run is"},
		{name: "the latest check sorting the tags without sort -V", workflow: in("verify-archives", "| sort -V | tail -n 1", "| sort | tail -n 1"), wantIn: "want the check of GitHub's latest release: run is"},
		{name: "the latest check without the pre-release filter", workflow: in("verify-archives", `| sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p'`, ""), wantIn: "want the check of GitHub's latest release: run is"},
		{name: "the latest check without the guard on an empty highest", workflow: in("verify-archives", "          if [ -z \"$highest\" ]; then\n            echo \"no release tag is merged into main, so GitHub's latest release cannot be checked\" >&2\n            exit 1\n          fi\n", ""), wantIn: "want the check of GitHub's latest release: run is"},
		{name: "the latest release read from another repository", workflow: in("verify-archives", `gh api "repos/timothymamo/shift2eu/releases/latest"`, `gh api "repos/example/shift2eu/releases/latest"`), wantIn: "want the check of GitHub's latest release: run is"},
		{name: "the latest check reading any failed read as an unpublished release", workflow: in("verify-archives", "            if grep -qF -e \"HTTP 404\" \"$err\"; then\n              rm -f \"$err\"\n              return 1\n            fi\n", "            if true; then\n              rm -f \"$err\"\n              return 1\n            fi\n"), wantIn: "want the check of GitHub's latest release: run is"},
		// A pre-release is never GitHub's "Latest" release, and the check is
		// unconditional (reviewer B2, issue #173): while the highest release
		// tag's own release is unpublished the rest of the step is skipped with
		// a notice, which must not let a pre-release through as "Latest".
		{
			name: "the latest check without the pre-release check",
			workflow: in("verify-archives", `          case "$TAG" in
            *-*)
              if [ "$latest" = "$TAG" ]; then
                echo "GitHub's latest release is $TAG, a pre-release, and a pre-release is never the latest release" >&2
                exit 1
              fi
              ;;
          esac
`, ""),
			wantIn: "want the check of GitHub's latest release: run is",
		},
		{
			name: "the pre-release check only while the highest release is published",
			workflow: in("verify-archives", `          case "$TAG" in
            *-*)
              if [ "$latest" = "$TAG" ]; then
                echo "GitHub's latest release is $TAG, a pre-release, and a pre-release is never the latest release" >&2
                exit 1
              fi
              ;;
          esac
`, `          published "$highest" || true
          if [ "$TAG" = "$latest" ]; then
            case "$TAG" in
              *-*)
                echo "GitHub's latest release is $TAG, a pre-release, and a pre-release is never the latest release" >&2
                exit 1
                ;;
            esac
          fi
`),
			wantIn: "want the check of GitHub's latest release: run is",
		},

		// Every job is bounded by a timeout (ADR 0042), the site's two jobs
		// included: a run whose step hangs is ended rather than left to GitHub's
		// default, and every job's value is the one the ADR lists.
		{name: "no timeout on the test job", workflow: in("test", "    timeout-minutes: 20\n", ""), wantIn: `holds no "timeout-minutes"`},
		{name: "no timeout on the latest job", workflow: in("latest", "    timeout-minutes: 5\n", ""), wantIn: `holds no "timeout-minutes"`},
		{name: "no timeout on the site job", workflow: in("site", "    timeout-minutes: 15\n", ""), wantIn: `jobs: site holds no "timeout-minutes"`},
		{name: "no timeout on the site-browser job", workflow: in("site-browser", "    timeout-minutes: 30\n", ""), wantIn: `jobs: site-browser holds no "timeout-minutes"`},
		{name: "no timeout on the goreleaser job", workflow: in("goreleaser", "    timeout-minutes: 15\n", ""), wantIn: `jobs: goreleaser holds no "timeout-minutes"`},
		{name: "no timeout on the attest-archives job", workflow: in("attest-archives", "    timeout-minutes: 10\n", ""), wantIn: `jobs: attest-archives holds no "timeout-minutes"`},
		{name: "no timeout on the publish job", workflow: in("publish", "    timeout-minutes: 10\n", ""), wantIn: `jobs: publish holds no "timeout-minutes"`},
		{name: "no timeout on the verify-archives job", workflow: in("verify-archives", "    timeout-minutes: 10\n", ""), wantIn: `jobs: verify-archives holds no "timeout-minutes"`},
		{name: "a longer timeout on the site browser", workflow: in("site-browser", "    timeout-minutes: 30\n", "    timeout-minutes: 60\n"), wantIn: `timeout-minutes is "60"`},
		{name: "a shorter timeout on the site's bundle", workflow: in("site", "    timeout-minutes: 15\n", "    timeout-minutes: 5\n"), wantIn: `timeout-minutes is "5"`},
		{name: "a timeout as an expression", workflow: in("publish", "    timeout-minutes: 10\n", "    timeout-minutes: ${{ vars.TIMEOUT }}\n"), wantIn: "timeout-minutes is"},

		// The site's bundle is published by every release (PRD #297 milestone
		// 3): the site job builds it at the tag and gives its digest as an
		// output, the site-browser job smoke-tests that very bundle, goreleaser
		// lists and uploads it beside the CLI's archives, attest-archives
		// attests it, and publish waits for the smoke test, so a bundle that
		// failed it is never published. Neither job holds a key, a secret or an
		// OIDC token, and neither has a condition, an environment or a
		// concurrency of its own.
		{name: "no site job", workflow: release.withoutJob(t, "site"), wantIn: `no "site"`},
		{name: "no site-browser job", workflow: release.withoutJob(t, "site-browser"), wantIn: `no "site-browser"`},
		{name: "the site job on another runner", workflow: in("site", "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-24.04\n"), wantIn: "jobs: site: runs-on is"},
		{name: "the site job not needing the tests", workflow: in("site", "    needs: test\n", "    needs: latest\n"), wantIn: "jobs: site: needs is"},
		{name: "a job-level if on the site job", workflow: in("site", "    needs: test\n", "    needs: test\n    if: always()\n"), wantIn: `key "if"`},
		{name: "an environment on the site job", workflow: in("site", "    needs: test\n", "    needs: test\n    environment: site\n"), wantIn: `key "environment"`},
		{name: "a concurrency on the site job", workflow: in("site", "    needs: test\n", "    needs: test\n    concurrency: site-deploy\n"), wantIn: `key "concurrency"`},
		{name: "packages: write for the site job", workflow: in("site", "      contents: read # check out the repository\n", "      contents: read # check out the repository\n      packages: write # push\n"), wantIn: `jobs: site: permissions holds the key "packages"`},
		{name: "id-token: write for the site job", workflow: in("site", "      contents: read # check out the repository\n", "      contents: read # check out the repository\n      id-token: write # sign\n"), wantIn: `jobs: site: permissions holds the key "id-token"`},
		{name: "no contents: read for the site job", workflow: in("site", "      contents: read # check out the repository\n", ""), wantIn: "jobs: site: permissions is empty"},
		{name: "the site job without its digest output", workflow: in("site", "    outputs:\n      sha256: ${{ steps.digest.outputs.sha256 }}\n", ""), wantIn: `jobs: site holds no "outputs"`},
		{name: "another site digest output", workflow: in("site", "      sha256: ${{ steps.digest.outputs.sha256 }}\n", "      sha256: ${{ steps.build.outputs.sha256 }}\n"), wantIn: "jobs: site: outputs: sha256 is"},
		{name: "the site built with another target", workflow: in("site", "        run: make site\n", "        run: make test-browser\n"), wantIn: "make site"},
		{name: "the site built without its version", workflow: in("site", "        env:\n          SITE_VERSION: ${{ github.ref_name }}\n", ""), wantIn: `no "env"`},
		{name: "the site's version from an input", workflow: in("site", "          SITE_VERSION: ${{ github.ref_name }}\n", "          SITE_VERSION: ${{ inputs.version }}\n"), wantIn: "SITE_VERSION"},
		{name: "the site artifact kept longer", workflow: in("site", "          retention-days: 7\n", "          retention-days: 90\n"), wantIn: "retention-days"},
		{name: "the site artifact with a tolerated miss", workflow: in("site", "          if-no-files-found: error\n", "          if-no-files-found: warn\n"), wantIn: "if-no-files-found"},
		{name: "the site artifact under another name", workflow: in("site", "          name: site\n", "          name: bundle\n"), wantIn: "name"},
		{name: "the site artifact of a narrower pattern", workflow: in("site", "          path: bin/shift2eu-site_*.tar.gz\n", "          path: bin/site\n"), wantIn: "path"},
		{name: "the site's digest without set -euo pipefail", workflow: in("site", "          set -euo pipefail\n", ""), wantIn: "set -euo pipefail"},
		{name: "the site's digest of another file", workflow: in("site", `          sha256="$(sha256sum "bin/shift2eu-site_$TAG.tar.gz" | cut -d' ' -f1)"`+"\n", `          sha256="$(sha256sum "bin/site/index.html" | cut -d' ' -f1)"`+"\n"), wantIn: "sha256sum"},
		{name: "an extra step in the site job", workflow: steps("site", beforeStep(t, siteSteps, "      - name: Build the site's bundle", "run: ./scripts/prepare.sh")), wantIn: "holds 6 steps, want exactly 5"},
		{name: "a missing step in the site job", workflow: steps("site", withoutStep(t, siteSteps, "name: The bundle's digest")), wantIn: "holds 4 steps, want exactly 5"},
		{name: "GITHUB_ENV in the site job", workflow: in("site", "        run: make site\n", "        run: make site && echo X=1 >> \"$GITHUB_ENV\"\n"), wantIn: "GITHUB_ENV"},
		{name: "GOFLAGS in the site job", workflow: in("site", "        run: make site\n", "        run: make site && go env -w GOFLAGS=-x\n"), wantIn: "GOFLAGS"},
		{name: "the site job's upload pinned by tag", workflow: in("site", "{{commit actions/upload-artifact}}", "actions/upload-artifact@v7"), wantIn: "upload-artifact@v7"},
		{name: "the site job's checkout pinned by tag", workflow: in("site", "{{commit actions/checkout}}", "actions/checkout@v7"), wantIn: "checkout@v7"},

		// The smoke test is of the very bundle the site job built: the artifact
		// uploaded, checked against that job's digest and unpacked, and
		// make test-browser pointed at it. No secret and no OIDC token.
		{name: "the site-browser job on another runner", workflow: in("site-browser", "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-24.04\n"), wantIn: "jobs: site-browser: runs-on is"},
		{name: "the site-browser job not needing the site job", workflow: in("site-browser", "    needs: site\n", "    needs: test\n"), wantIn: "jobs: site-browser: needs is"},
		{name: "an environment on the site-browser job", workflow: in("site-browser", "    needs: site\n", "    needs: site\n    environment: site\n"), wantIn: `key "environment"`},
		{name: "id-token: write for the site-browser job", workflow: in("site-browser", "      contents: read # check out the repository\n", "      contents: read # check out the repository\n      id-token: write # sign\n"), wantIn: `jobs: site-browser: permissions holds the key "id-token"`},
		{name: "attestations: write for the site-browser job", workflow: in("site-browser", "      contents: read # check out the repository\n", "      contents: read # check out the repository\n      attestations: write # store it\n"), wantIn: `jobs: site-browser: permissions holds the key "attestations"`},
		{name: "a secret in the site-browser job", workflow: in("site-browser", "        run: make test-browser TEST_BROWSER_BUNDLE=bin/release-bundle\n", "        env:\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n        run: make test-browser TEST_BROWSER_BUNDLE=bin/release-bundle\n"), wantIn: `holds the key "env"`},
		{name: "the site-browser job testing a bundle it builds itself", workflow: in("site-browser", "        run: make test-browser TEST_BROWSER_BUNDLE=bin/release-bundle\n", "        run: make test-browser\n"), wantIn: "TEST_BROWSER_BUNDLE"},
		{name: "the site-browser job testing the bundle make site keeps", workflow: in("site-browser", "TEST_BROWSER_BUNDLE=bin/release-bundle", "TEST_BROWSER_BUNDLE=bin/site"), wantIn: "TEST_BROWSER_BUNDLE"},
		{name: "the site-browser job without the digest check", workflow: in("site-browser", "          printf '%s  %s\\n' \"$SITE_SHA256\" \"bin/shift2eu-site_$TAG.tar.gz\" | sha256sum -c -\n", ""), wantIn: "sha256sum -c -"},
		{name: "the site-browser's digest from another job", workflow: in("site-browser", "          SITE_SHA256: ${{ needs.site.outputs.sha256 }}\n", "          SITE_SHA256: ${{ needs.goreleaser.outputs.sha256 }}\n"), wantIn: "needs.site.outputs.sha256"},
		{name: "the site-browser's checkout keeping the token", workflow: in("site-browser", "          persist-credentials: false\n", "          persist-credentials: true\n"), wantIn: "persist-credentials"},
		{name: "the bundle unpacked outside bin", workflow: in("site-browser", "          mkdir bin/release-bundle\n", "          mkdir release-bundle\n"), wantIn: "bin/release-bundle"},
		{name: "an extra step in the site-browser job", workflow: steps("site-browser", beforeStep(t, siteBrowserSteps, "      - name: Check and unpack the bundle", "run: ./scripts/prepare.sh")), wantIn: "holds 6 steps, want exactly 5"},
		{name: "a missing step in the site-browser job", workflow: steps("site-browser", withoutStep(t, siteBrowserSteps, "name: Check and unpack the bundle")), wantIn: "holds 4 steps, want exactly 5"},
		{name: "a secret in the site-browser's check step", workflow: in("site-browser", "          TAG: ${{ github.ref_name }}\n", "          TAG: ${{ github.ref_name }}\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n"), wantIn: "env holds"},
		{name: "the site-browser job's download pinned by tag", workflow: in("site-browser", "{{commit actions/download-artifact}}", "actions/download-artifact@v8"), wantIn: "download-artifact@v8"},

		// goreleaser lists and uploads the bundle the site job built and the
		// smoke test tested, bound to the site job's digest, and hands on the
		// archives alone.
		{name: "goreleaser not needing the site job", workflow: in("goreleaser", "    needs: [test, site]\n", "    needs: [test]\n"), wantIn: "jobs: goreleaser: needs is"},
		{name: "the site bundle not downloaded by goreleaser", workflow: steps("goreleaser", withoutStep(t, goreleaserSteps, "name: site\n")), wantIn: "holds 5 steps, want exactly 6"},
		{name: "the bundle's digest not checked before goreleaser", workflow: steps("goreleaser", withoutStep(t, goreleaserSteps, "name: Check the site bundle against the site job's digest")), wantIn: "holds 5 steps, want exactly 6"},
		{name: "the site bundle downloaded after goreleaser", workflow: steps("goreleaser", swappedSteps(t, goreleaserSteps, "actions/download-artifact@", "goreleaser/goreleaser-action@")), wantIn: "jobs: goreleaser: steps: step 3"},
		{name: "the goreleaser upload handing the bundle on", workflow: in("goreleaser", "            dist/checksums.txt\n", "            dist/checksums.txt\n            bin/shift2eu-site_*.tar.gz\n"), wantIn: "with: path"},
		{name: "the bundle checked against another digest", workflow: in("goreleaser", "          SITE_SHA256: ${{ needs.site.outputs.sha256 }}\n", "          SITE_SHA256: ${{ needs.goreleaser.outputs.sha256 }}\n"), wantIn: "needs.site.outputs.sha256"},

		// The bundle is attested beside the archives, bound to checksums.txt
		// and to the site job's digest.
		{name: "the attest-archives job not needing the site job", workflow: in("attest-archives", "    needs: [goreleaser, site]\n", "    needs: [goreleaser]\n"), wantIn: "jobs: attest-archives: needs is"},
		{name: "the site bundle not downloaded by attest-archives", workflow: steps("attest-archives", withoutStep(t, attestArchivesSteps, "name: site\n")), wantIn: "holds 3 steps, want exactly 4"},
		{name: "the bundle not held to checksums.txt", workflow: in("attest-archives", `          grep -qxF "$SITE_SHA256  shift2eu-site_$TAG.tar.gz" checksums.txt`+"\n", ""), wantIn: "grep -qxF"},
		{name: "checksums.txt not held to one site bundle", workflow: in("attest-archives", `          test "$(grep -cF '  shift2eu-site_' checksums.txt)" = 1`+"\n", `          test "$(grep -cF '  shift2eu-site_' checksums.txt)" -ge 1`+"\n"), wantIn: "grep -cF"},
		{name: "the site bundle's digest from another job", workflow: in("attest-archives", "          SITE_SHA256: ${{ needs.site.outputs.sha256 }}\n", "          SITE_SHA256: ${{ needs.goreleaser.outputs.sha256 }}\n"), wantIn: "needs.site.outputs.sha256"},
		{name: "the site bundle not attested", workflow: in("attest-archives", "            shift2eu_*.zip\n            shift2eu-site_*.tar.gz\n", "            shift2eu_*.zip\n"), wantIn: "subject-path"},

		// A bundle that failed the smoke test is never published: the release
		// stays a draft.
		{name: "the publish job not needing the smoke test", workflow: in("publish", "    needs: [test, goreleaser, attest-archives, latest, site-browser]\n", "    needs: [test, goreleaser, attest-archives, latest]\n"), wantIn: "jobs: publish: needs is"},

		// The release deploys the site behind a switch (PRD #297 milestone 3,
		// ADR 0042): site-programs builds the deploy program and the live check,
		// site-deploy is the one job that holds the site's key, site-check checks
		// the live site after it, and site-skipped says why neither ran. Each is a
		// closed job: its keys are the check's, its steps a list, and either
		// secret's name appears on exactly one line of the whole document.
		{name: "no site-programs job", workflow: release.withoutJob(t, "site-programs"), wantIn: `no "site-programs"`},
		{name: "no site-deploy job", workflow: release.withoutJob(t, "site-deploy"), wantIn: `no "site-deploy"`},
		{name: "no site-check job", workflow: release.withoutJob(t, "site-check"), wantIn: `no "site-check"`},
		{name: "no site-skipped job", workflow: release.withoutJob(t, "site-skipped"), wantIn: `no "site-skipped"`},
		{name: "no timeout on the site-programs job", workflow: in("site-programs", "    timeout-minutes: 10\n", ""), wantIn: `holds no "timeout-minutes"`},
		{name: "no timeout on the site-deploy job", workflow: in("site-deploy", "    timeout-minutes: 15\n", ""), wantIn: `holds no "timeout-minutes"`},
		{name: "no timeout on the site-check job", workflow: in("site-check", "    timeout-minutes: 15\n", ""), wantIn: `holds no "timeout-minutes"`},
		{name: "no timeout on the site-skipped job", workflow: in("site-skipped", "    timeout-minutes: 5\n", ""), wantIn: `holds no "timeout-minutes"`},
		{name: "a longer timeout on the site deploy", workflow: in("site-deploy", "    timeout-minutes: 15\n", "    timeout-minutes: 20\n"), wantIn: `timeout-minutes is "20"`},

		// The deploy waits for everything that could still change what it
		// deploys: the latest decision, the image's and the assets' verification,
		// the attestation, the bundle and its smoke test, and its own program.
		{
			name:     "the deploy job not needing the image's verification",
			workflow: in("site-deploy", "    needs: [latest, verify, verify-archives, attest-archives, site, site-browser, site-programs]\n", "    needs: [latest, verify-archives, attest-archives, site, site-browser, site-programs]\n"),
			wantIn:   "jobs: site-deploy: needs is",
		},
		{
			name:     "the deploy job not needing the latest decision",
			workflow: in("site-deploy", "    needs: [latest, verify, verify-archives, attest-archives, site, site-browser, site-programs]\n", "    needs: [verify, verify-archives, attest-archives, site, site-browser, site-programs]\n"),
			wantIn:   "jobs: site-deploy: needs is",
		},
		{
			name:     "the deploy job needing the programs first",
			workflow: in("site-deploy", "    needs: [latest, verify, verify-archives, attest-archives, site, site-browser, site-programs]\n", "    needs: [site-programs, latest, verify, verify-archives, attest-archives, site, site-browser]\n"),
			wantIn:   "jobs: site-deploy: needs is",
		},

		// The deploy runs for the highest release tag on main, and only while the
		// maintainer's switch is true: both halves, joined by &&, each value a
		// quoted string.
		{name: "no condition on the deploy job", workflow: in("site-deploy", "    if: needs.latest.outputs.latest == 'true' && vars.SITE_DEPLOY_ENABLED == 'true'\n", ""), wantIn: `holds no "if"`},
		{name: "the deploy job's condition without the latest half", workflow: in("site-deploy", "    if: needs.latest.outputs.latest == 'true' && vars.SITE_DEPLOY_ENABLED == 'true'\n", "    if: vars.SITE_DEPLOY_ENABLED == 'true'\n"), wantIn: "if is"},
		{name: "the deploy job's condition without the switch half", workflow: in("site-deploy", "    if: needs.latest.outputs.latest == 'true' && vars.SITE_DEPLOY_ENABLED == 'true'\n", "    if: needs.latest.outputs.latest == 'true'\n"), wantIn: "if is"},
		{name: "the deploy job's condition with ||", workflow: in("site-deploy", " == 'true' && vars.SITE_DEPLOY_ENABLED", " == 'true' || vars.SITE_DEPLOY_ENABLED"), wantIn: "if is"},
		{name: "the deploy job's condition unquoted", workflow: in("site-deploy", "vars.SITE_DEPLOY_ENABLED == 'true'", "vars.SITE_DEPLOY_ENABLED == true"), wantIn: "if is"},
		{name: "an always() beside the deploy job's condition", workflow: in("site-deploy", "    if: needs.latest.outputs.latest", "    if: always() && needs.latest.outputs.latest"), wantIn: "if is"},

		// The site environment is the deploy's alone: it admits tags only, and it
		// is what a deployment is recorded under.
		{name: "no environment on the deploy job", workflow: in("site-deploy", "    environment: site\n", ""), wantIn: `holds no "environment"`},
		{name: "another environment on the deploy job", workflow: in("site-deploy", "    environment: site\n", "    environment: production\n"), wantIn: "environment is"},
		{name: "the deploy job's environment in mapping form", workflow: in("site-deploy", "    environment: site\n", "    environment:\n      name: site\n      url: https://example.com\n"), wantIn: "environment"},
		{name: "an environment on the site check", workflow: in("site-check", "    permissions:\n", "    environment: site\n    permissions:\n"), wantIn: `key "environment"`},
		{name: "an environment on the programs job", workflow: in("site-programs", "    permissions:\n", "    environment: site\n    permissions:\n"), wantIn: `key "environment"`},

		// One deploy at a time, and a deploy in progress is never cancelled.
		{name: "no concurrency on the deploy job", workflow: in("site-deploy", "    concurrency:\n      group: site-deploy\n      cancel-in-progress: false\n", ""), wantIn: `holds no "concurrency"`},
		{name: "a deploy concurrency that cancels the run in progress", workflow: in("site-deploy", "      cancel-in-progress: false\n", "      cancel-in-progress: true\n"), wantIn: "cancel-in-progress"},
		{name: "a deploy concurrency group per tag", workflow: in("site-deploy", "      group: site-deploy\n", "      group: site-deploy-${{ github.ref }}\n"), wantIn: "group is"},

		// The deploy reads the published release and attests nothing: contents:
		// read and attestations: read alone, no OIDC token and no write.
		{name: "id-token: write for the deploy job", workflow: in("site-deploy", "      attestations: read # gh attestation verify reads them through the repository's API\n", "      attestations: read # gh attestation verify reads them through the repository's API\n      id-token: write # sign\n"), wantIn: "id-token"},
		{name: "no attestations: read for the deploy job", workflow: in("site-deploy", "      attestations: read # gh attestation verify reads them through the repository's API\n", ""), wantIn: "attestations"},
		{name: "contents: write for the deploy job", workflow: in("site-deploy", "      contents: read # download the published release's bundle\n", "      contents: write # download the published release's bundle\n"), wantIn: "contents"},
		{name: "deployments: write for the deploy job", workflow: in("site-deploy", "      attestations: read # gh attestation verify reads them through the repository's API\n", "      attestations: read # gh attestation verify reads them through the repository's API\n      deployments: write # the environment's record\n"), wantIn: "deployments"},

		// The deploy's steps are a closed list, the deploy last: nothing of the
		// repository runs in this job, and nothing runs after the deploy.
		{name: "a checkout in the deploy job", workflow: steps("site-deploy", beforeStep(t, siteDeploySteps, "      - uses: actions/download-artifact@", "uses: {{pin actions/checkout}}\n        with:\n          persist-credentials: false")), wantIn: "holds 6 steps, want exactly 5"},
		{name: "a setup-go after the deploy step", workflow: steps("site-deploy", slices.Concat(siteDeploySteps, []string{"      - uses: " + pin("{{pin actions/setup-go}}") + "\n        with:\n          go-version-file: go.mod\n          cache: false\n"})), wantIn: "holds 6 steps, want exactly 5"},
		{name: "the deploy before the higher releases' manifests", workflow: steps("site-deploy", swappedSteps(t, siteDeploySteps, "name: Deploy the site", "name: The higher releases' manifests")), wantIn: "jobs: site-deploy: steps: step 4"},
		{name: "no deploy step", workflow: steps("site-deploy", withoutStep(t, siteDeploySteps, "name: Deploy the site")), wantIn: "holds 4 steps, want exactly 5"},
		{name: "an extra step in the programs job", workflow: steps("site-programs", beforeStep(t, siteProgramsSteps, "      - name: Build the site's programs", "run: ./scripts/prepare.sh")), wantIn: "holds 7 steps, want exactly 6"},
		{name: "an extra step in the skip notice", workflow: steps("site-skipped", beforeStep(t, siteSkippedSteps, "      - name: Say that the site deploy is skipped", "run: ./scripts/prepare.sh")), wantIn: "holds 2 steps, want exactly 1"},

		// Each of the two secrets appears on exactly one line of the whole
		// document, both in the env: of the deploy step, which holds no other
		// secret and no token: the one place the key is used.
		{name: "the key in the deploy job's env", workflow: in("site-deploy", "    permissions:\n", "    env:\n      SCW_ACCESS_KEY: ${{ secrets.SCW_ACCESS_KEY }}\n    permissions:\n"), wantIn: "SCW_ACCESS_KEY"},
		{name: "the key in the bundle check's env", workflow: in("site-deploy", "          SITEDEPLOY_SHA256: ${{ needs.site-programs.outputs.sitedeploy }}\n", "          SITEDEPLOY_SHA256: ${{ needs.site-programs.outputs.sitedeploy }}\n          SCW_ACCESS_KEY: ${{ secrets.SCW_ACCESS_KEY }}\n"), wantIn: "SCW_ACCESS_KEY"},
		{name: "the two secrets swapped", workflow: in("site-deploy", "          SCW_ACCESS_KEY: ${{ secrets.SCW_ACCESS_KEY }}\n", "          SCW_ACCESS_KEY: ${{ secrets.SCW_SECRET_KEY }}\n"), wantIn: "env holds"},
		{name: "a token beside the key in the deploy step", workflow: in("site-deploy", "          SITE_REGION: ${{ vars.SITE_REGION }}\n", "          SITE_REGION: ${{ vars.SITE_REGION }}\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n"), wantIn: "GH_TOKEN"},
		{name: "the key in another job's step", workflow: in("site-check", "          SITECHECK_SHA256: ${{ needs.site-programs.outputs.sitecheck }}\n", "          SITECHECK_SHA256: ${{ needs.site-programs.outputs.sitecheck }}\n          SCW_ACCESS_KEY: ${{ secrets.SCW_ACCESS_KEY }}\n"), wantIn: "SCW_ACCESS_KEY"},

		// No line may name a proxy or a certificate variable: a step that sets
		// one would change what the deploy program's client trusts or where it
		// connects.
		{name: "HTTPS_PROXY in the programs' build step", workflow: proxyInBuildStep("HTTPS_PROXY"), wantIn: "HTTPS_PROXY"},
		{name: "HTTP_PROXY in the programs' build step", workflow: proxyInBuildStep("HTTP_PROXY"), wantIn: "HTTP_PROXY"},
		{name: "NO_PROXY in the programs' build step", workflow: proxyInBuildStep("NO_PROXY"), wantIn: "NO_PROXY"},
		{name: "SSL_CERT_FILE in the programs' build step", workflow: proxyInBuildStep("SSL_CERT_FILE"), wantIn: "SSL_CERT_FILE"},
		{name: "SSL_CERT_DIR in the programs' build step", workflow: proxyInBuildStep("SSL_CERT_DIR"), wantIn: "SSL_CERT_DIR"},
		{name: "GITHUB_ENV in the deploy job", workflow: in("site-deploy", "          mkdir manifests higher\n", "          mkdir manifests higher\n          echo X=1 >> \"$GITHUB_ENV\"\n"), wantIn: "GITHUB_ENV"},
		{name: "GITHUB_PATH in the deploy job", workflow: in("site-deploy", "          mkdir manifests higher\n", "          mkdir manifests higher\n          echo /x >> \"$GITHUB_PATH\"\n"), wantIn: "GITHUB_PATH"},
		{name: "github.env in the deploy job", workflow: in("site-deploy", "          mkdir manifests higher\n", "          mkdir manifests higher\n          echo X=1 >> \"${{ github.env }}\"\n"), wantIn: "github.env"},

		// The deploy program is asked for exactly the bundle, the tag, the site
		// environment's bucket and region, and the two files beside it. The tag
		// comes through env:, never pasted into the script.
		{name: "the deploy asking for the bucket's list", workflow: in("site-deploy", "-manifests manifests\n", "-manifests manifests -list-bucket\n"), wantIn: "run is"},
		{name: "the deploy reading another tag list", workflow: in("site-deploy", "-releases releases.txt", "-releases site-tags.txt"), wantIn: "run is"},
		{name: "the deploy's version from another variable", workflow: in("site-deploy", `-version "$TAG"`, `-version "$SITE_VERSION"`), wantIn: "run is"},
		{name: "the deploy's tag pasted into the script", workflow: in("site-deploy", `-version "$TAG"`, `-version "${{ github.ref_name }}"`), wantIn: "holds a ${{ }} expression"},
		{name: "the bucket from a secret", workflow: in("site-deploy", "          SITE_BUCKET: ${{ vars.SITE_BUCKET }}\n", "          SITE_BUCKET: ${{ secrets.SITE_BUCKET }}\n"), wantIn: "vars.SITE_BUCKET"},

		// The published bundle is bound to the site job's digest and to this
		// workflow's attestation for this tag and commit, on GitHub's runners.
		{name: "the deploy verifying the bundle without the signer workflow", workflow: inDeployStep("name: Check the site bundle and the deploy program", "            --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \\\n", ""), wantIn: "--signer-workflow"},
		{name: "the deploy verifying the bundle from another signer workflow", workflow: inDeployStep("name: Check the site bundle and the deploy program", "            --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \\\n", "            --signer-workflow example/repo/.github/workflows/other.yml \\\n"), wantIn: "run is"},
		{name: "the deploy verifying the bundle without the source ref", workflow: inDeployStep("name: Check the site bundle and the deploy program", `--source-ref "refs/tags/$TAG" --source-digest`, `--source-digest`), wantIn: "--source-ref"},
		{name: "the deploy verifying the bundle without the source digest", workflow: inDeployStep("name: Check the site bundle and the deploy program", `--source-digest "$GITHUB_SHA" --deny-self-hosted-runners`, `--deny-self-hosted-runners`), wantIn: "--source-digest"},
		{name: "the deploy not denying self-hosted runners", workflow: inDeployStep("name: Check the site bundle and the deploy program", " --deny-self-hosted-runners\n", "\n"), wantIn: "--deny-self-hosted-runners"},
		{name: "the deploy program's digest not checked", workflow: inDeployStep("name: Check the site bundle and the deploy program", `          printf '%s  %s\n%s  %s\n' "$SITE_SHA256" "shift2eu-site_$TAG.tar.gz" "$SITEDEPLOY_SHA256" sitedeploy | sha256sum -c -`, `          printf '%s  %s\n' "$SITE_SHA256" "shift2eu-site_$TAG.tar.gz" | sha256sum -c -`), wantIn: "SITEDEPLOY_SHA256"},
		{name: "the deploy's bundle digest from another job", workflow: in("site-deploy", "          SITE_SHA256: ${{ needs.site.outputs.sha256 }}\n", "          SITE_SHA256: ${{ needs.goreleaser.outputs.sha256 }}\n"), wantIn: "needs.site.outputs.sha256"},

		// Only a published release is deployed: the draft check comes before the
		// download, so a draft's bundle is never unpacked.
		{name: "the deploy without the draft check", workflow: inDeployStep("name: Check the site bundle and the deploy program", draftCheckBlock, ""), wantIn: "draft"},
		{name: "the deploy's draft check after the download", workflow: afterDownload(), wantIn: "run is"},

		// The higher releases' manifests: the one reason the deploy program can
		// refuse to roll the live site back. Each tag comes from releases.txt, and
		// each manifest comes from the bundle that release attested.
		{name: "the higher releases read without the signer workflow", workflow: inDeployStep("name: The higher releases' manifests", "              --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \\\n", ""), wantIn: "--signer-workflow"},
		{name: "a higher release read under this tag's source ref", workflow: inDeployStep("name: The higher releases' manifests", `--source-ref "refs/tags/$t"`, `--source-ref "refs/tags/$TAG"`), wantIn: `refs/tags/$t`},
		{name: "the higher releases without the draft check", workflow: inDeployStep("name: The higher releases' manifests", `            if [ "$(jq -r '.draft' "$higher/release.json")" != false ]; then`+"\n", "            if false; then\n"), wantIn: ".draft"},
		{name: "the higher releases without the notice for an unpublished release", workflow: inDeployStep("name: The higher releases' manifests", `                echo "::notice::the release of $t is not published yet, so its manifest is not read"`+"\n", ""), wantIn: "::notice::the release of $t is not published yet"},
		{name: "the higher releases without the check for the bundle", workflow: inDeployStep("name: The higher releases' manifests", `            if [ "$(jq --arg name "shift2eu-site_$t.tar.gz" -r '[.assets[].name] | map(select(. == $name)) | length' "$higher/release.json")" != 1 ]; then`+"\n", "            if false; then\n"), wantIn: "publishes no shift2eu-site_"},
		{name: "the higher releases read from somewhere else than releases.txt", workflow: inDeployStep("name: The higher releases' manifests", "          mapfile -t tags < releases.txt\n", "          mapfile -t tags < <(gh release list --repo timothymamo/shift2eu --json tagName --jq '.[].tagName')\n"), wantIn: "mapfile -t tags < releases.txt"},

		// The live check runs after the deploy, on the same conditions, and reads
		// the published bundle and its own program: contents: read alone, no
		// environment and no secret but the token.
		{name: "no condition on the site check", workflow: in("site-check", "    if: needs.latest.outputs.latest == 'true' && vars.SITE_DEPLOY_ENABLED == 'true'\n", ""), wantIn: `holds no "if"`},
		{name: "the site check's condition other than the deploy's", workflow: in("site-check", "    if: needs.latest.outputs.latest == 'true' && vars.SITE_DEPLOY_ENABLED == 'true'\n", "    if: needs.latest.outputs.latest == 'true'\n"), wantIn: "if is"},
		{name: "the site check not needing the deploy", workflow: in("site-check", "    needs: [latest, site, site-programs, site-deploy]\n", "    needs: [latest, site, site-programs]\n"), wantIn: "jobs: site-check: needs is"},
		{name: "attestations: read for the site check", workflow: in("site-check", "      contents: read # check out the repository\n", "      contents: read # check out the repository\n      attestations: read # gh attestation verify reads them\n"), wantIn: "attestations"},
		{name: "id-token: write for the site check", workflow: in("site-check", "      contents: read # check out the repository\n", "      contents: read # check out the repository\n      id-token: write # sign\n"), wantIn: "id-token"},
		{name: "the site check's checkout without persist-credentials", workflow: in("site-check", "          persist-credentials: false\n", ""), wantIn: `no "persist-credentials"`},
		{name: "the site check's checkout keeping the token", workflow: in("site-check", "          persist-credentials: false\n", "          persist-credentials: true\n"), wantIn: "persist-credentials"},
		{name: "another secret in the site check", workflow: in("site-check", "          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n", "          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n          OTHER: ${{ secrets.OTHER_TOKEN }}\n"), wantIn: "OTHER_TOKEN"},
		{name: "the site check's program digest from another job", workflow: in("site-check", "          SITECHECK_SHA256: ${{ needs.site-programs.outputs.sitecheck }}\n", "          SITECHECK_SHA256: ${{ needs.site-programs.outputs.sitedeploy }}\n"), wantIn: "needs.site-programs.outputs.sitecheck"},
		{name: "the site check without the draft check", workflow: inCheckStep("name: Check the site bundle and the live check program", draftCheckBlock, ""), wantIn: "draft"},
		{name: "the site check asking for a plain HTTP www", workflow: in("site-check", "bin/sitecheck -manifest bin/manifest.json -tags bin/releases.txt\n", "bin/sitecheck -manifest bin/manifest.json -tags bin/releases.txt -plain-http-www refuse-or-close\n"), wantIn: "run is"},
		{name: "the site check with a retry bound of its own", workflow: in("site-check", "bin/sitecheck -manifest bin/manifest.json -tags bin/releases.txt\n", "bin/sitecheck -manifest bin/manifest.json -tags bin/releases.txt -retry-bound 1s\n"), wantIn: "run is"},
		{name: "the site check against another host", workflow: in("site-check", "bin/sitecheck -manifest bin/manifest.json -tags bin/releases.txt\n", "bin/sitecheck -manifest bin/manifest.json -tags bin/releases.txt -host example.com\n"), wantIn: "run is"},

		// The release tags the deploy and the live check hand their programs are
		// the latest job's: the release tags merged into main, in version order.
		{name: "the deploy's release tags sorted lexically", workflow: inDeployStep("name: The release tags on main", "| sort -V > releases.txt", "| sort > releases.txt"), wantIn: "sort -V"},
		{name: "the deploy's release tags from another branch", workflow: inDeployStep("name: The release tags on main", "--merged refs/remotes/origin/main", "--merged refs/heads/main"), wantIn: "refs/remotes/origin/main"},
		{name: "the live check's release tags without the pre-release filter", workflow: inCheckStep("name: The release tags on main", ` | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p'`, ""), wantIn: "sed -nE"},

		// The programs are built as the release builds them, kept ninety days so a
		// deploy is re-run by re-running their job alone, and bound to the digest
		// the job gives: nothing of the key is here.
		{name: "the programs built with another target", workflow: in("site-programs", "        run: make site-programs\n", "        run: make site\n"), wantIn: "make site-programs"},
		{name: "a token in the programs' build step", workflow: in("site-programs", "        run: make site-programs\n", "        env:\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n        run: make site-programs\n"), wantIn: "GH_TOKEN"},
		{name: "contents: write for the programs job", workflow: in("site-programs", "      contents: read # check out the repository\n", "      contents: write # check out the repository\n"), wantIn: "contents"},
		{name: "the deploy program's artifact kept a week", workflow: in("site-programs", "          path: bin/sitedeploy\n          if-no-files-found: error\n          retention-days: 90\n", "          path: bin/sitedeploy\n          if-no-files-found: error\n          retention-days: 7\n"), wantIn: "retention-days"},
		{name: "the live check's artifact kept a week", workflow: in("site-programs", "          path: bin/sitecheck\n          if-no-files-found: error\n          retention-days: 90\n", "          path: bin/sitecheck\n          if-no-files-found: error\n          retention-days: 7\n"), wantIn: "retention-days"},
		{name: "the live check's digest of another file", workflow: in("site-programs", `          echo "sitecheck=$(sha256sum bin/sitecheck | cut -d' ' -f1)" >> "$GITHUB_OUTPUT"`+"\n", `          echo "sitecheck=$(sha256sum bin/sitedeploy | cut -d' ' -f1)" >> "$GITHUB_OUTPUT"`+"\n"), wantIn: "sha256sum"},
		{name: "another output name for the deploy program", workflow: in("site-programs", "      sitedeploy: ${{ steps.digests.outputs.sitedeploy }}\n", "      sitedeploy: ${{ steps.digest.outputs.sitedeploy }}\n"), wantIn: "sitedeploy is"},

		// The skip notice is the deploy's condition negated, it holds no
		// permission and no key, and the variable's value is never interpolated
		// into the text it prints.
		{name: "no condition on the skip notice", workflow: in("site-skipped", "    if: needs.latest.outputs.latest != 'true' || vars.SITE_DEPLOY_ENABLED != 'true'\n", ""), wantIn: `holds no "if"`},
		{name: "the skip notice's condition with only the switch", workflow: in("site-skipped", "    if: needs.latest.outputs.latest != 'true' || vars.SITE_DEPLOY_ENABLED != 'true'\n", "    if: vars.SITE_DEPLOY_ENABLED != 'true'\n"), wantIn: "if is"},
		{name: "a permission on the skip notice", workflow: in("site-skipped", "    permissions: {}\n", "    permissions:\n      contents: read\n"), wantIn: "permissions"},
		{name: "the switch pasted into the skip notice", workflow: in("site-skipped", `          printf '> [!NOTE]\n> The site deploy and its live check are skipped: %s.\n' "$reason" >> "$GITHUB_STEP_SUMMARY"`+"\n", `          printf '> [!NOTE]\n> The site deploy and its live check are skipped: %s.\n' "${{ vars.SITE_DEPLOY_ENABLED }}" >> "$GITHUB_STEP_SUMMARY"`+"\n"), wantIn: "holds a ${{ }} expression"},
		{name: "the switch in another job's env", workflow: in("site-programs", "        run: make site-programs\n", "        env:\n          SITE_DEPLOY_ENABLED: ${{ vars.SITE_DEPLOY_ENABLED }}\n        run: make site-programs\n"), wantIn: "SITE_DEPLOY_ENABLED"},

		// Every action is pinned by commit, the deploy's and the live check's too.
		{name: "the deploy job's download pinned by tag", workflow: in("site-deploy", "{{commit actions/download-artifact}}", "actions/download-artifact@v8"), wantIn: "download-artifact@v8"},
		{name: "the site check's checkout pinned by tag", workflow: in("site-check", "{{commit actions/checkout}}", "actions/checkout@main"), wantIn: "checkout@main"},

		{name: "an empty document", workflow: "", wantIn: "release workflow"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := check.ReleaseWorkflow([]byte(tc.workflow))

			if tc.wantIn == "" {
				if len(got) != 0 {
					t.Errorf("ReleaseWorkflow() = %q, want no message", got)
				}

				return
			}

			if !strings.Contains(strings.Join(got, "\n"), tc.wantIn) {
				t.Errorf("ReleaseWorkflow() = %q, want a message containing %q", got, tc.wantIn)
			}
		})
	}
}

// TestReleaseWorkflowFixtureIsTheWorkflow pins that the fixture the cases
// above change one part of is .github/workflows/release.yml itself: rendering
// it rebuilds the file byte for byte, comments included. A release.yml that
// moves on without the fixture would leave every case below probing a document
// the release no longer runs.
func TestReleaseWorkflowFixtureIsTheWorkflow(t *testing.T) {
	t.Parallel()

	want, err := os.ReadFile(filepath.FromSlash("../../../../.github/workflows/release.yml"))
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}

	if diff := cmp.Diff(string(want), releaseWorkflowFixture().render()); diff != "" {
		t.Errorf("the fixture is not .github/workflows/release.yml (-want +got):\n%s", diff)
	}
}

// TestStrayReleaseFiles pins the files at the repository root that change what
// goreleaser builds without the check seeing them: the other goreleaser
// document names, and go.work, go.work.sum and vendor, which the go command
// goreleaser runs reads and the check's go list (GOWORK=off, -mod=readonly)
// does not. What counts is what git could commit, as git ls-files --cached
// --others --exclude-standard lists it: a developer's ignored go.work never
// reaches the release, a tracked or force-added one does.
func TestStrayReleaseFiles(t *testing.T) {
	t.Parallel()

	names := []string{
		".config/goreleaser.yml",
		".config/goreleaser.yaml",
		".goreleaser.yml",
		"goreleaser.yml",
		"goreleaser.yaml",
		"go.work",
		"go.work.sum",
	}

	// repo returns a new git repository that ignores go.work and go.work.sum,
	// as the repository's .gitignore does.
	repo := func(t *testing.T) string {
		t.Helper()

		root := t.TempDir()
		git(t, root, "init", "--quiet")
		writeFileContent(t, root, ".gitignore", "go.work\ngo.work.sum\n")

		return root
	}

	t.Run("a clean root", func(t *testing.T) {
		t.Parallel()

		root := repo(t)
		writeFile(t, root, ".goreleaser.yaml")
		writeFile(t, root, "go.mod")
		writeFile(t, root, "sub/vendor/x.go")
		writeFile(t, root, "sub/go.work")
		writeFile(t, root, "vendored/x.go")

		assertNoStray(t, root)
	})

	t.Run("an ignored go.work and go.work.sum", func(t *testing.T) {
		t.Parallel()

		root := repo(t)
		writeFile(t, root, "go.work")
		writeFile(t, root, "go.work.sum")

		assertNoStray(t, root)
	})

	for _, name := range names {
		t.Run("untracked "+name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			git(t, root, "init", "--quiet")
			writeFile(t, root, name)

			assertStray(t, root, name)
		})

		t.Run("force-added "+name, func(t *testing.T) {
			t.Parallel()

			root := repo(t)
			writeFile(t, root, name)
			git(t, root, "add", "--force", name)

			assertStray(t, root, name)
		})
	}

	t.Run("a tracked go.work gone from the working tree", func(t *testing.T) {
		t.Parallel()

		root := repo(t)
		writeFile(t, root, "go.work")
		git(t, root, "add", "--force", "go.work")

		if err := os.Remove(filepath.Join(root, "go.work")); err != nil {
			t.Fatalf("remove go.work: %v", err)
		}

		assertStray(t, root, "go.work")
	})

	t.Run("an untracked, not ignored vendor directory", func(t *testing.T) {
		t.Parallel()

		root := repo(t)
		writeFile(t, root, "vendor/modules.txt")
		writeFile(t, root, "vendor/example.com/m/x.go")

		assertStray(t, root, "vendor")
	})

	t.Run("a go.work symlink to nothing", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		git(t, root, "init", "--quiet")

		if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "go.work")); err != nil {
			t.Fatalf("create the symlink: %v", err)
		}

		assertStray(t, root, "go.work")
	})

	t.Run("outside git", func(t *testing.T) {
		t.Parallel()

		// A .git file pointing nowhere stops git from looking further up for
		// a repository, so git fails here.
		root := t.TempDir()
		writeFileContent(t, root, ".git", "gitdir: missing\n")

		if got, err := check.StrayReleaseFiles(t.Context(), root); err == nil {
			t.Fatalf("StrayReleaseFiles() = %q, nil; want an error", got)
		}
	})
}

func assertNoStray(t *testing.T, root string) {
	t.Helper()

	got, err := check.StrayReleaseFiles(t.Context(), root)
	if err != nil {
		t.Fatalf("StrayReleaseFiles(): %v", err)
	}

	if len(got) != 0 {
		t.Errorf("StrayReleaseFiles() = %q, want no message", got)
	}
}

func assertStray(t *testing.T, root, name string) {
	t.Helper()

	got, err := check.StrayReleaseFiles(t.Context(), root)
	if err != nil {
		t.Fatalf("StrayReleaseFiles(): %v", err)
	}

	if len(got) != 1 || !strings.HasPrefix(got[0], name+" ") {
		t.Errorf("StrayReleaseFiles() = %q, want one message starting %q", got, name+" ")
	}
}

// writeFile creates the file name, slash-separated, under root, with the
// directories it needs.
func writeFile(t *testing.T, root, name string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create the directory for %s: %v", name, err)
	}

	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// writeFileContent creates the file name, slash-separated, under root, with
// content and the directories it needs.
func writeFileContent(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create the directory for %s: %v", name, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

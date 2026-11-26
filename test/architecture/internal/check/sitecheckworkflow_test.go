package check_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// This file pins the scheduled live site check (PRD #297 milestone 4, ADR
// 0043): .github/workflows/site-check.yml, which repeats the release's live
// check at least daily and opens one issue, assigned to the maintainer, when it
// fails.
//
// The fixture below is the workflow the cases change one part of, and rendering
// it is .github/workflows/site-check.yml itself, comments included: a workflow
// that moves on without the fixture would leave every case probing a document
// the repository no longer runs.
//
// The pins are the ones milestone 4's Layer 3 lists: one schedule and no other
// trigger, no permission by default and exactly two jobs, the checking job with
// contents: read and attestations: read and no actions: read, its checkout
// keeping no credential, every action pinned by commit SHA, the tag list read
// exactly as release.yml's site-check job reads it, the release tags merged into
// main and nothing else, the live version matched against that list and accepted
// only within the version window whose anchor is the highest tag's committer
// date and whose bound is check.SiteCheckWindow, the anchor passed to the
// script through env: and never pasted into it, the attestation verified with
// the deploy's and the guide's flags and bound to the tag's commit, the full
// check's HSTS and preload records read at the release's tag and passed
// through env:, the window covering the HTML's max-age, and an alert job that
// holds issues: write alone, fires whenever the check did not succeed (a job
// past its timeout concludes cancelled, where failure() is false), creates its
// label idempotently before it opens the issue, and writes fixed text plus the
// failed run's URL, never an output of the checking job.

// siteCheckWorkflow is .github/workflows/site-check.yml.
const siteCheckWorkflow = `name: Live site check

# The release's live check, repeated at least daily: a page changed between
# releases is noticed within a day (PRD #297 milestone 4, ADR 0043). The live
# version is untrusted page text, matched against the repository's own tag list
# and accepted only within the window after the highest release tag's commit,
# so a release whose run was queued past the window fails the check once. A
# release deploying while this runs can fail it once, like a release queued
# past the window. The alert job opens or comments on one issue because GitHub
# notifies a failed scheduled run only to whoever last changed its cron line,
# and disables a schedule after 60 days without repository activity; the
# runbook says how to re-enable it.
on:
  schedule:
    - cron: '23 5 * * *'

# No permission by default; each job asks for its own.
permissions: {}

jobs:
  check:
    name: Check the live site
    runs-on: ubuntu-latest
    timeout-minutes: 30
    permissions:
      contents: read
      attestations: read
    steps:
      # The whole history, so the tag list and the highest tag's commit are read
      # from the repository itself, and the checkout keeps no credential: no
      # step here needs one.
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          fetch-depth: 0
          persist-credentials: false
      - uses: actions/setup-go@d8cbb8045d7f43706b9177cceb36c1f91a6644d5 # v7.1.0
        with:
          go-version-file: go.mod
          cache: false # see the comment on the release's test job
      # The release tags on main, read exactly as release.yml's site-check job
      # reads them, and the committer date of the highest one's commit, which
      # contents: read can read and actions: read cannot. The window is
      # anchored there.
      - name: The expected release
        id: expected
        run: |
          set -euo pipefail
          git fetch --quiet --force origin '+refs/heads/main:refs/remotes/origin/main' '+refs/tags/v*:refs/tags/v*'
          mapfile -t tags < <(git tag -l 'v*.*.*' --merged refs/remotes/origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V)
          expected="${tags[-1]}"
          printf '%s\n' "${tags[@]}" > "${RUNNER_TEMP}/tags.txt"
          anchor="$(git log -1 --format=%cI "${expected}")"
          echo "anchor=${anchor}" >> "${GITHUB_OUTPUT}"
      # The live version is read from the page and never trusted: the program
      # refuses one that is not vX.Y.Z, that is no tag of the repository, or
      # that is below the highest tag outside the window, and prints the tag
      # list's own entry. The grep re-checks that entry against the tag list, so
      # every command after it takes its tag from the repository, never from
      # the page. The anchor reaches the script through env: and is never pasted
      # into it: a ${{ }} in a run: is the runner pasting text as code.
      - name: The live version
        id: live
        env:
          ANCHOR: ${{ steps.expected.outputs.anchor }}
        run: |
          set -euo pipefail
          live="$(go run ./ci/sitecheck live-version -tags "${RUNNER_TEMP}/tags.txt" -anchor "${ANCHOR}" -window 180m)"
          grep -qxF "${live}" "${RUNNER_TEMP}/tags.txt"
          echo "tag=${live}" >> "${GITHUB_OUTPUT}"
      # The commit the tag names, read from the repository itself and handed to
      # the attestation through env: as --source-digest: the provenance must
      # bind the release's commit, not only its ref, so a tag that moved after
      # the release fails the check.
      - name: The release's commit
        id: commit
        env:
          TAG: ${{ steps.live.outputs.tag }}
        run: |
          set -euo pipefail
          digest="$(git rev-parse "refs/tags/${TAG}^{commit}")"
          echo "digest=${digest}" >> "${GITHUB_OUTPUT}"
      # The release the page names, downloaded and its build provenance verified
      # with the workflow's token alone, the signer workflow, the tag's own ref
      # and commit, and no self-hosted runner: the deploy's and the guide's
      # flags.
      - name: Verify the release's attestation
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          TAG: ${{ steps.live.outputs.tag }}
          DIGEST: ${{ steps.commit.outputs.digest }}
        run: |
          set -euo pipefail
          mkdir -p "${RUNNER_TEMP}/bundle"
          gh release download "${TAG}" --repo timothymamo/shift2eu --pattern "shift2eu-site_${TAG}.tar.gz" --dir "${RUNNER_TEMP}/bundle"
          gh attestation verify "${RUNNER_TEMP}/bundle/shift2eu-site_${TAG}.tar.gz" --repo timothymamo/shift2eu --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml --source-ref "refs/tags/${TAG}" --source-digest "${DIGEST}" --deny-self-hosted-runners
          tar -xzf "${RUNNER_TEMP}/bundle/shift2eu-site_${TAG}.tar.gz" -C "${RUNNER_TEMP}/bundle"
      # The whole check against the release's own manifest, and HSTS and the
      # preload list against the records the release's tag holds, read from the
      # repository and never from the working tree: the files, /, the
      # service-worker paths, the headers, the redirects, both certificates and
      # HSTS against the recorded state.
      - name: Check the live site
        env:
          TAG: ${{ steps.live.outputs.tag }}
        run: |
          set -euo pipefail
          git show "refs/tags/${TAG}:site/records/hsts.json" > "${RUNNER_TEMP}/hsts.json"
          git show "refs/tags/${TAG}:site/records/preload.json" > "${RUNNER_TEMP}/preload.json"
          go run ./ci/sitecheck -manifest "${RUNNER_TEMP}/bundle/manifest.json" -tags "${RUNNER_TEMP}/tags.txt" -hsts-record "${RUNNER_TEMP}/hsts.json" -preload-record "${RUNNER_TEMP}/preload.json"
  alert:
    name: Alert
    needs: check
    # A job that exceeds its timeout-minutes concludes cancelled, and failure()
    # is false then: the alert fires whenever the check did not succeed.
    if: ${{ always() && needs.check.result != 'success' }}
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      issues: write
    steps:
      # One issue with a fixed label, assigned to the maintainer, commented on
      # when it is already open: editing an issue notifies no one. The label is
      # created first, idempotently (--force): an issue takes a label the
      # repository does not have yet from no run, and no run but this one could
      # create it. The issue holds fixed text and the failed run's URL only,
      # never an output of the check job and never site content.
      - name: Open or comment on the alert issue
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GH_REPO: ${{ github.repository }}
          RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
          LABEL: live site check
          TITLE: The live site check failed
        run: |
          set -euo pipefail
          body="$(printf 'The scheduled live site check failed: %s\n' "${RUN_URL}")"
          gh label create "${LABEL}" --repo "${GH_REPO}" --force --color ededed --description "The scheduled live site check"
          existing="$(gh issue list --repo "${GH_REPO}" --label "${LABEL}" --state open --json number --jq '.[0].number // empty')"
          if [ -n "${existing}" ]; then
            gh issue comment "${existing}" --repo "${GH_REPO}" --body "${body}"
          else
            gh issue create --repo "${GH_REPO}" --title "${TITLE}" --body "${body}" --label "${LABEL}" --assignee timothymamo
          fi
`

// siteCheckWindowFlag matches the -window flag of the live-version step with its
// literal value, which the check holds to check.SiteCheckWindow.
var siteCheckWindowFlag = regexp.MustCompile(`-window ([0-9]+(?:[smh]))`)

// siteCheckWindowLiteral returns the window the fixture's live-version step
// passes.
func siteCheckWindowLiteral(t *testing.T, workflow string) time.Duration {
	t.Helper()

	match := siteCheckWindowFlag.FindStringSubmatch(workflow)
	if match == nil {
		t.Fatalf("the workflow has no -window literal for its live-version step: the version window is its bound (PRD #297 milestone 4, ADR 0043)")
	}

	window, err := time.ParseDuration(match[1])
	if err != nil {
		t.Fatalf("parse the window %q: %v", match[1], err)
	}

	return window
}

// siteCheckWorkflowFile reads one workflow of .github/workflows, as the
// repository holds it.
func siteCheckWorkflowFile(t *testing.T, name string) []byte {
	t.Helper()

	return siteCheckRepositoryFile(t, ".github/workflows/"+name)
}

// siteCheckRepositoryFile reads one file of the repository, as it holds it,
// from this package's directory.
func siteCheckRepositoryFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.FromSlash("../../../../" + path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return data
}

// TestSiteCheckWorkflow pins the scheduled check's workflow as a closed schema:
// the fixture itself passes, and every case changes one part of it that the
// check must refuse.
func TestSiteCheckWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("the fixture is accepted", func(t *testing.T) {
		t.Parallel()

		if err := check.SiteCheckWorkflow([]byte(siteCheckWorkflow)); err != nil {
			t.Fatalf("SiteCheckWorkflow(the fixture) = %v, want no error: the fixture is the workflow the cases change one part of", err)
		}
	})

	tests := []struct {
		name string
		// src replaces the whole fixture when it is set; otherwise old is
		// replaced by new, exactly once.
		src      string
		old, new string
	}{
		{
			name: "a push trigger beside the schedule",
			old:  "on:\n  schedule:",
			new:  "on:\n  push:\n  schedule:",
		},
		{
			name: "a workflow_dispatch trigger beside the schedule",
			old:  "on:\n  schedule:",
			new:  "on:\n  workflow_dispatch:\n  schedule:",
		},
		{
			name: "a cron entry that does not run daily",
			old:  "    - cron: '23 5 * * *'",
			new:  "    - cron: '23 5 * * 0'",
		},
		{
			name: "a second cron entry",
			old:  "    - cron: '23 5 * * *'",
			new:  "    - cron: '23 5 * * *'\n    - cron: '23 17 * * *'",
		},
		{
			name: "a permission by default",
			old:  "permissions: {}",
			new:  "permissions:\n  contents: read",
		},
		{
			name: "a third job",
			old:  "            gh issue create --repo \"${GH_REPO}\" --title \"${TITLE}\" --body \"${body}\" --label \"${LABEL}\" --assignee timothymamo\n          fi",
			new:  "            gh issue create --repo \"${GH_REPO}\" --title \"${TITLE}\" --body \"${body}\" --label \"${LABEL}\" --assignee timothymamo\n          fi\n  extra:\n    name: Extra\n    runs-on: ubuntu-latest\n    timeout-minutes: 5\n    permissions:\n      contents: read\n    steps:\n      - name: Nothing\n        run: echo nothing",
		},
		{
			name: "the alert job renamed",
			old:  "\n  alert:\n",
			new:  "\n  notify:\n",
		},
		{
			name: "a job with an environment",
			old:  "    timeout-minutes: 30",
			new:  "    timeout-minutes: 30\n    environment: site",
		},
		{
			name: "a secret of another name",
			old:  "          GH_REPO: ${{ github.repository }}",
			new:  "          GH_REPO: ${{ secrets.SITE_DEPLOY_KEY }}",
		},
		{
			name: "actions: read in the checking job",
			old:  "      attestations: read",
			new:  "      attestations: read\n      actions: read",
		},
		{
			name: "no attestations: read in the checking job",
			old:  "      attestations: read",
			new:  "",
		},
		{
			name: "no timeout on the checking job",
			old:  "    timeout-minutes: 30",
			new:  "",
		},
		{
			name: "an action pinned by a tag",
			old:  "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
			new:  "actions/checkout@v7.0.1",
		},
		{
			name: "a checkout that keeps the credential",
			old:  "          persist-credentials: false",
			new:  "          persist-credentials: true",
		},
		{
			name: "a window other than the pinned constant",
			old:  "-window 180m",
			new:  "-window 60m",
		},
		{
			name: "a tag list that is not the release tags on main",
			old:  "git tag -l 'v*.*.*' --merged refs/remotes/origin/main",
			new:  "git tag -l 'v*.*.*'",
		},
		{
			name: "a tag list without its vX.Y.Z filter",
			old:  " | sed -nE '/^v[0-9]+\\.[0-9]+\\.[0-9]+$/p' | sort -V",
			new:  " | sort -V",
		},
		{
			name: "a fetch that takes neither main nor a release tag",
			old:  "git fetch --quiet --force origin '+refs/heads/main:refs/remotes/origin/main' '+refs/tags/v*:refs/tags/v*'",
			new:  "git fetch --quiet --force --tags origin",
		},
		{
			name: "an anchor that is not the committer date",
			old:  "git log -1 --format=%cI",
			new:  "git log -1 --format=%aI",
		},
		{
			name: "the anchor pasted into the run instead of passed through env",
			old: `        env:
          ANCHOR: ${{ steps.expected.outputs.anchor }}
        run: |
          set -euo pipefail
          live="$(go run ./ci/sitecheck live-version -tags "${RUNNER_TEMP}/tags.txt" -anchor "${ANCHOR}" -window 180m)"`,
			new: `        run: |
          set -euo pipefail
          live="$(go run ./ci/sitecheck live-version -tags "${RUNNER_TEMP}/tags.txt" -anchor "${{ steps.expected.outputs.anchor }}" -window 180m)"`,
		},
		{
			name: "an expression pasted into the live version's run script",
			old:  `-anchor "${ANCHOR}"`,
			new:  `-anchor "${{ steps.expected.outputs.anchor }}"`,
		},
		{
			name: "another signer workflow",
			old:  "--signer-workflow timothymamo/shift2eu/.github/workflows/release.yml",
			new:  "--signer-workflow timothymamo/shift2eu/.github/workflows/ci.yml",
		},
		{
			name: "another source ref",
			old:  "--source-ref \"refs/tags/${TAG}\"",
			new:  "--source-ref \"refs/heads/main\"",
		},
		{
			name: "no deny-self-hosted-runners",
			old:  " --deny-self-hosted-runners",
			new:  "",
		},
		{
			name: "the live version not re-checked against the tag list",
			old:  "          grep -qxF \"${live}\" \"${RUNNER_TEMP}/tags.txt\"",
			new:  "          # the tag list is not re-checked here",
		},
		{
			name: "the full check without the tag list",
			old:  "go run ./ci/sitecheck -manifest \"${RUNNER_TEMP}/bundle/manifest.json\" -tags \"${RUNNER_TEMP}/tags.txt\"",
			new:  "go run ./ci/sitecheck -manifest \"${RUNNER_TEMP}/bundle/manifest.json\"",
		},
		{
			name: "the full check without the manifest",
			old:  "go run ./ci/sitecheck -manifest \"${RUNNER_TEMP}/bundle/manifest.json\" -tags \"${RUNNER_TEMP}/tags.txt\"",
			new:  "go run ./ci/sitecheck -tags \"${RUNNER_TEMP}/tags.txt\"",
		},
		{
			name: "the full check without the HSTS record",
			old:  ` -hsts-record "${RUNNER_TEMP}/hsts.json"`,
			new:  "",
		},
		{
			name: "the full check without the preload record",
			old:  ` -preload-record "${RUNNER_TEMP}/preload.json"`,
			new:  "",
		},
		{
			name: "the full check with the working tree's HSTS record instead of the tag's",
			old:  `git show "refs/tags/${TAG}:site/records/hsts.json" > "${RUNNER_TEMP}/hsts.json"`,
			new:  `cp site/records/hsts.json "${RUNNER_TEMP}/hsts.json"`,
		},
		{
			name: "the full check with HEAD's preload record instead of the tag's",
			old:  `git show "refs/tags/${TAG}:site/records/preload.json" > "${RUNNER_TEMP}/preload.json"`,
			new:  `git show "HEAD:site/records/preload.json" > "${RUNNER_TEMP}/preload.json"`,
		},
		{
			name: "a full check without the tag it reads the records at",
			old:  "        env:\n          TAG: ${{ steps.live.outputs.tag }}\n        run: |\n          set -euo pipefail\n          git show",
			new:  "        run: |\n          set -euo pipefail\n          git show",
		},
		{
			name: "an attestation verified without --source-digest",
			old:  ` --source-digest "${DIGEST}"`,
			new:  "",
		},
		{
			name: "a source digest read from HEAD instead of the tag's commit",
			old:  `git rev-parse "refs/tags/${TAG}^{commit}"`,
			new:  "git rev-parse HEAD",
		},
		{
			name: "an expected release step that writes an output no step reads",
			old:  `          anchor="$(git log -1 --format=%cI "${expected}")"`,
			new:  `          anchor="$(git log -1 --format=%cI "${expected}")"` + "\n" + `          echo "tag=${expected}" >> "${GITHUB_OUTPUT}"`,
		},
		{
			name: "an alert job that does not need the check",
			old:  "    needs: check",
			new:  "    needs: []",
		},
		{
			name: "an alert job that runs only on failure and not on a timeout",
			old:  "    if: ${{ always() && needs.check.result != 'success' }}",
			new:  "    if: failure()",
		},
		{
			name: "an alert job that runs on success",
			old:  "    if: ${{ always() && needs.check.result != 'success' }}",
			new:  "    if: always()",
		},
		{
			name: "an alert job that skips a cancelled check",
			old:  "    if: ${{ always() && needs.check.result != 'success' }}",
			new:  "    if: ${{ !cancelled() && needs.check.result != 'success' }}",
		},
		{
			name: "an alert job with another permission",
			old:  "      issues: write",
			new:  "      issues: write\n      contents: read",
		},
		{
			name: "an alert job that checks the repository out",
			old:  "    permissions:\n      issues: write\n    steps:",
			new:  "    permissions:\n      issues: write\n    steps:\n      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
		},
		{
			name: "an alert whose label is not created first",
			old:  "          gh label create \"${LABEL}\" --repo \"${GH_REPO}\" --force --color ededed --description \"The scheduled live site check\"\n",
			new:  "",
		},
		{
			name: "a label created without --force",
			old:  "--force --color ededed",
			new:  "--color ededed",
		},
		{
			name: "an alert that writes an output of the checking job",
			old:  "          if [ -n \"${existing}\" ]; then",
			new:  "          echo \"${{ needs.check.outputs.tag }}\"\n          if [ -n \"${existing}\" ]; then",
		},
		{
			name: "an alert that writes a step output",
			old:  "          existing=\"$(gh issue list --repo \"${GH_REPO}\" --label \"${LABEL}\" --state open --json number --jq '.[0].number // empty')\"",
			new:  "          echo \"${{ steps.live.outputs.tag }}\"\n          existing=\"$(gh issue list --repo \"${GH_REPO}\" --label \"${LABEL}\" --state open --json number --jq '.[0].number // empty')\"",
		},
		{
			name: "an alert expression outside the fixed four",
			old:  "          TITLE: The live site check failed",
			new:  "          TITLE: ${{ vars.SITE_BUCKET }}",
		},
		{
			name: "another alert label",
			old:  "          LABEL: live site check",
			new:  "          LABEL: site-down",
		},
		{
			name: "another assignee",
			old:  "--assignee timothymamo",
			new:  "--assignee someone",
		},
		{
			name: "an alert that opens no issue",
			old:  "            gh issue create --repo \"${GH_REPO}\" --title \"${TITLE}\" --body \"${body}\" --label \"${LABEL}\" --assignee timothymamo",
			new:  "            echo \"${TITLE} ${body}\"",
		},
		{
			name: "an alert that comments on no issue",
			old:  "            gh issue comment \"${existing}\" --repo \"${GH_REPO}\" --body \"${body}\"",
			new:  "            echo \"${body}\"",
		},
		{
			name: "a file that is empty",
			src:  " \n",
		},
		{
			name: "a line with a tab",
			src:  "on:\n\tschedule:\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workflow := siteCheckWorkflow

			switch {
			case tc.src != "":
				workflow = tc.src
			case tc.old != "":
				if count := strings.Count(workflow, tc.old); count != 1 {
					t.Fatalf("the case's old text appears %d times in the fixture, want exactly once: the case changes one part of it", count)
				}

				workflow = strings.Replace(workflow, tc.old, tc.new, 1)
			}

			if err := check.SiteCheckWorkflow([]byte(workflow)); err == nil {
				t.Errorf("SiteCheckWorkflow accepted a workflow that has %s, want an error: the check pins it (PRD #297 milestone 4, ADR 0043)", tc.name)
			}
		})
	}
}

// TestSiteCheckWorkflowFixtureIsTheWorkflow pins that the fixture the cases
// change one part of is .github/workflows/site-check.yml itself: rendering it
// is the workflow, comments included, so the cases probe a document the
// workflow still runs.
func TestSiteCheckWorkflowFixtureIsTheWorkflow(t *testing.T) {
	t.Parallel()

	if diff := cmp.Diff(string(siteCheckWorkflowFile(t, "site-check.yml")), siteCheckWorkflow); diff != "" {
		t.Errorf("the fixture is not .github/workflows/site-check.yml (-want +got):\n%s", diff)
	}
}

// TestSiteCheckWindowIsTheFixtures pins that the exported window the check holds
// the workflow to is the literal the fixture's live-version step passes, so the
// constant and the workflow cannot drift.
func TestSiteCheckWindowIsTheFixtures(t *testing.T) {
	t.Parallel()

	if window := siteCheckWindowLiteral(t, siteCheckWorkflow); window != check.SiteCheckWindow {
		t.Errorf("the fixture passes -window %v, and check.SiteCheckWindow is %v, want the same: the workflow test pins the window's bound (PRD #297 milestone 4, ADR 0043)", window, check.SiteCheckWindow)
	}
}

// TestSiteCheckWindowCoversTheRelease pins that the window is at least the
// delay from the release tag's commit to the end of the release run: release
// please's release-please and tag jobs, the longest chain of release.yml's jobs
// that ends at site-check, and the HTML's max-age. A shorter window would fail
// the scheduled check on a release that is still deploying.
func TestSiteCheckWindowCoversTheRelease(t *testing.T) {
	t.Parallel()

	minimum, err := check.MinSiteCheckWindow(
		siteCheckWorkflowFile(t, "release-please.yml"),
		siteCheckWorkflowFile(t, "release.yml"),
	)
	if err != nil {
		t.Fatalf("MinSiteCheckWindow() failed: %v", err)
	}

	if check.SiteCheckWindow < minimum {
		t.Errorf("check.SiteCheckWindow is %v, want at least %v: release-please's two jobs, release.yml's longest chain to site-check and the HTML's max-age (PRD #297 milestone 4, ADR 0043)",
			check.SiteCheckWindow, minimum)
	}
}

// livecheckMaxHTMLAgeConst matches the live check's MaxHTMLAge declaration.
var livecheckMaxHTMLAgeConst = regexp.MustCompile(`(?m)^const MaxHTMLAge = ([0-9]+)$`)

// TestSiteCheckHTMLMaxAgeIsTheLivechecks pins that the HTML's max-age the
// window's bound covers, check.SiteCheckHTMLMaxAge, is the live check's own
// MaxHTMLAge: two constants that must move together. The live check's is
// ci/sitecheck/internal/livecheck's, behind ci/sitecheck's internal directory,
// which Go's internal rule keeps out of reach of test/architecture, so its
// value is read from its source.
func TestSiteCheckHTMLMaxAgeIsTheLivechecks(t *testing.T) {
	t.Parallel()

	match := livecheckMaxHTMLAgeConst.FindSubmatch(siteCheckRepositoryFile(t, "ci/sitecheck/internal/livecheck/headers.go"))
	if match == nil {
		t.Fatalf("ci/sitecheck/internal/livecheck/headers.go holds no `const MaxHTMLAge = <seconds>` line: the scheduled check's window covers the HTML's max-age (PRD #297 milestone 4, ADR 0043)")
	}

	seconds, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatalf("parse ci/sitecheck/internal/livecheck's MaxHTMLAge %q: %v", match[1], err)
	}

	if want := time.Duration(seconds) * time.Second; check.SiteCheckHTMLMaxAge != want {
		t.Errorf("check.SiteCheckHTMLMaxAge is %v, and ci/sitecheck/internal/livecheck's MaxHTMLAge is %v, want the same: the window's bound covers the HTML's max-age (PRD #297 milestone 4, ADR 0043)",
			check.SiteCheckHTMLMaxAge, want)
	}
}

package check_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// The commits the release-please workflow's two actions are pinned to, as
// Renovate moves them (v3.3.0 and v5.0.0 when issues #157 and #159 were
// decided).
const (
	appCommit              = "64ab729a6294000f5aa4a4bc0b73d1f32bc45510"
	releasePleaseActionPin = "45996ed1f6d02564a971a2fa1b5860e934307cf7"
)

// appToken is the with: of the step that mints one of the two Apps' tokens:
// the client ID and the private key its environment holds, and the permission-*
// inputs that narrow the token to what the job does. appID is the input issue
// #159 replaces with client-id: a case that writes it is a workflow the check
// must refuse. A field left empty is left out of the step, as a job that reads
// too little of its environment writes it.
type appToken struct {
	appID       string
	clientID    string
	privateKey  string
	permissions []string
}

// step renders the token step, named name, that mints the token.
func (a appToken) step(name string) string {
	with := make([]string, 0, 3+len(a.permissions))

	if a.appID != "" {
		with = append(with, "          app-id: "+a.appID)
	}

	if a.clientID != "" {
		with = append(with, "          client-id: "+a.clientID)
	}

	if a.privateKey != "" {
		with = append(with, "          private-key: "+a.privateKey)
	}

	for _, p := range a.permissions {
		with = append(with, "          "+p)
	}

	return "      - name: " + name + "\n" +
		"        id: app\n" +
		"        uses: actions/create-github-app-token@" + appCommit + " # v3.3.0\n" +
		"        with:\n" +
		strings.Join(with, "\n") + "\n"
}

// releasePleaseAppToken is the release-please App's token: the two Apps of
// issue #157 split the work, and this one opens the release PR, labels it and,
// once it is tagged, relabels it. It holds Contents and Pull requests write and
// nothing else, and it bypasses no ruleset.
var releasePleaseAppToken = appToken{
	clientID:    "${{ vars.RELEASE_APP_CLIENT_ID }}",
	privateKey:  "${{ secrets.RELEASE_APP_PRIVATE_KEY }}",
	permissions: []string{"permission-contents: write", "permission-pull-requests: write"},
}

// tagAppToken is the tag App's token: Contents write, to create the tag, and
// Pull requests read, to check the merged release PR, and nothing else. It is
// the only App that may bypass the Release tag creation ruleset.
var tagAppToken = appToken{
	clientID:    "${{ vars.RELEASE_TAG_APP_CLIENT_ID }}",
	privateKey:  "${{ secrets.RELEASE_TAG_APP_PRIVATE_KEY }}",
	permissions: []string{"permission-contents: write", "permission-pull-requests: read"},
}

// releasePleaseWorkflow is the accepted shape, which the privilege check reads
// too: it publishes nothing itself, so that check must accept it, and refuse it
// as soon as one of its jobs writes through GITHUB_TOKEN.
var releasePleaseWorkflow = releasePleaseAppsWorkflow(releasePleaseAppToken, tagAppToken, releasePleaseAppToken)

// releasePleaseAppsWorkflowFrom returns .github/workflows/release-please.yml's
// fixture built from releasePlease, a release-please.yml, as
// releaseWorkflowFixtureFrom builds the release fixture from a release.yml
// (issue #440): every placeholder of the fixture filled in with that workflow's
// pins (workflowPins). RED: the fixture still holds the two actions' pins as
// literals, so it comes out unchanged whatever workflow it is handed, and
// TestReleasePleaseFixturesFollowThePins fails until those literals are
// placeholders.
func releasePleaseAppsWorkflowFrom(releasePlease []byte) string {
	return pinsOf(releasePlease).expand(releasePleaseWorkflow)
}

// releasePleaseActionStep opens or updates the release PR with the
// release-please App's token. Every input but token would override
// release-please-config.json, so none is set. With skip-github-release in that
// configuration release-please creates no GitHub Release and so no tag:
// GoReleaser creates the Release, and the tag job creates the tag.
var releasePleaseActionStep = `      - uses: googleapis/release-please-action@` + releasePleaseActionPin + ` # v5.0.0
        with:
          token: ${{ steps.app.outputs.token }}
`

// releasePleaseGuardStep refuses to create the tag while release.yml has a run
// in any status GitHub may hold one in: requested, waiting, pending, queued or
// in_progress. With a group of its own this workflow is never cancelled by a
// release, so it waits instead of losing one: a tag pushed at a release.yml
// that GitHub then cancels publishes nothing, and a run that is only requested
// or waiting may still publish. It reads the runs with github.token, the
// automatic token of the job's actions: read, never an App's token.
//
// Its env: holds that token, the repository and the release-please App's own
// slug, APP_SLUG: shift2eu-release, the same constant the tagging step reads:
// the tag job holds the App it tags for itself, and reads no output of the job
// that runs the third-party release-please action.
const releasePleaseGuardStep = `      - name: Wait for no release to be queued or in progress
        id: guard
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
          APP_SLUG: shift2eu-release
        run: |
          set -euo pipefail
          for status in requested waiting pending queued in_progress; do
            runs="$(gh api "repos/${GH_REPO}/actions/workflows/release.yml/runs?status=${status}&per_page=1" --jq '.total_count')"
            if [ "${runs}" != "0" ]; then
              echo "::error::release.yml has ${runs} run(s) ${status}: nothing is tagged" >&2
              exit 1
            fi
          done
`

// releasePleaseTagStep creates the vX.Y.Z tag of the merged release PR still
// labelled autorelease: pending, on that PR's merge commit, through the API
// with the tag App's token. It only reports which PR it tagged and which tag it
// created: the relabelling is the relabel job's, with the release-please App's
// token, which is the App that labels this PR. A tag created with GITHUB_TOKEN
// starts no workflow and the ruleset refuses it; a tag created with git push is
// not the App's either (ADR 0015).
//
// Its env: holds exactly the App's token, the repository and the release-please
// App's own slug, APP_SLUG: shift2eu-release, a constant of this file and not a
// job output, so that only a PR the release-please App opened is a candidate: a
// hand-written PR labelled autorelease: pending is never tagged. The job that
// runs the third-party release-please action hands this step nothing it must
// trust. Its gh api endpoints are the release PR's own (pulls, pulls/<n>), the
// merge commit's ancestry (compare), the manifest at that commit (contents) and
// the tag (git/refs): nothing creates or edits a GitHub Release, and no script
// calls a host of its own.
const releasePleaseTagStep = `      - name: Tag the merged release PR
        id: tag
        env:
          GH_TOKEN: ${{ steps.app.outputs.token }}
          GH_REPO: ${{ github.repository }}
          APP_SLUG: shift2eu-release
        run: |
          set -euo pipefail
          pending="autorelease: pending"
          pr="$(gh api "repos/${GH_REPO}/pulls?state=closed&per_page=20" --jq ".[] | select(.merged_at != null and any(.labels[]; .name == \"${pending}\")) | .number")"
          if [ -z "${pr}" ]; then
            echo "pr=" >> "${GITHUB_OUTPUT}"
            exit 0
          fi
          merge="$(gh api "repos/${GH_REPO}/pulls/${pr}" --jq .merge_commit_sha)"
          ancestry="$(gh api "repos/${GH_REPO}/compare/${merge}...main" --jq .status)"
          [ "${ancestry}" = "identical" ] || [ "${ancestry}" = "ahead" ] || { echo "::error::${merge} is not on main" >&2; exit 1; }
          version="$(gh api -H 'Accept: application/vnd.github.raw+json' "repos/${GH_REPO}/contents/.release-please-manifest.json?ref=${merge}" | jq -er '.["."]')"
          tag="v${version}"
          gh api --method POST "repos/${GH_REPO}/git/refs" -f ref="refs/tags/${tag}" -f sha="${merge}" --silent
          echo "pr=${pr}" >> "${GITHUB_OUTPUT}"
          echo "tag=${tag}" >> "${GITHUB_OUTPUT}"
`

// releasePleaseRelabelStep labels the tagged release PR autorelease: tagged and
// removes autorelease: pending, with the release-please App's token. It runs
// only when the tag job tagged a PR, and it checks again that the tag is on
// that PR's merge commit before it relabels, so a tag that moved leaves the
// label alone. Tagged first, then no longer pending: a run that stops between
// the two leaves both labels, and the next run finishes the relabelling.
const releasePleaseRelabelStep = `      - name: Relabel the tagged release PR
        env:
          GH_TOKEN: ${{ steps.app.outputs.token }}
          GH_REPO: ${{ github.repository }}
          PR: ${{ needs.tag.outputs.pr }}
          TAG: ${{ needs.tag.outputs.tag }}
        run: |
          set -euo pipefail
          merge="$(gh api "repos/${GH_REPO}/pulls/${PR}" --jq .merge_commit_sha)"
          target="$(gh api "repos/${GH_REPO}/git/ref/tags/${TAG}" --jq .object.sha)"
          if [ "${target}" != "${merge}" ]; then
            echo "::error::${TAG} is not on the merge commit of #${PR}" >&2
            exit 1
          fi
          gh api --method POST "repos/${GH_REPO}/issues/${PR}/labels" -f "labels[]=autorelease: tagged" --silent
          gh api --method DELETE "repos/${GH_REPO}/issues/${PR}/labels/autorelease:%20pending" --silent
`

// The env: of the tag job's two tokenless steps, as the accepted shape writes
// them: a case that changes one of the values a step holds prefixes its step's,
// since the guard and the tagging step share GH_REPO and APP_SLUG and differ in
// GH_TOKEN alone.
const (
	taggingStepEnv = "          GH_TOKEN: ${{ steps.app.outputs.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG: shift2eu-release\n"
	guardStepEnv   = "          GH_TOKEN: ${{ github.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG: shift2eu-release\n"
)

// releasePleaseAppsWorkflow returns .github/workflows/release-please.yml in the
// shape issues #157 and #159 want: one workflow on a push to main, with
// permissions: {} at the top level, a concurrency group of its own, and three
// jobs. The release-please job, in the release-please environment, mints the
// release-please App's token from that environment's client ID and private key
// and opens the release PR, and declares no output: the job that runs the
// third-party release-please action hands the jobs after it nothing. The tag
// job, in the release-tag environment, mints the tag App's token from that
// environment's client ID and private key, holds the release-please App's slug
// as the constant its guard and tag steps read, waits for no release.yml run in
// flight, and creates the tag. The relabel job, in the release-please
// environment, mints the release-please App's token again and, when the tag job
// tagged a PR, relabels it. Each token step passes client-id, never the
// deprecated app-id, and no job reads the other App's variable or secret.
func releasePleaseAppsWorkflow(release, tag, relabel appToken) string {
	return `name: Release please

# A push to main alone proposes a version: the tag is created after the release
# PR merges, never on a push of a tag.
on:
  push:
    branches: [main]

# Nothing through GITHUB_TOKEN writes: the Apps' tokens do the writing.
permissions: {}

# A group of its own, never the group release.yml uses. GitHub keeps one
# pending run per group and cancels the older pending one when a new run
# queues, so sharing that group would let a later push to main cancel a release
# that is waiting to run. A run in progress is never cancelled (ADR 0015).
concurrency:
  group: release-please
  cancel-in-progress: false

jobs:
  release-please:
    name: Release please
    runs-on: ubuntu-latest
    timeout-minutes: 15
    # Holds the release-please App's client ID and private key, limited to main.
    environment: release-please
    # GITHUB_TOKEN reads only: the App's token does the writing.
    permissions:
      contents: read
    steps:
` + release.step("Mint the release-please App's token") + releasePleaseActionStep + `  tag:
    name: Tag
    runs-on: ubuntu-latest
    timeout-minutes: 15
    needs: release-please
    # Holds the tag App's client ID and private key, limited to main.
    environment: release-tag
    # GITHUB_TOKEN reads only: contents for the job, actions for the guard's
    # read of release.yml's runs.
    permissions:
      contents: read
      actions: read
    outputs:
      pr: ${{ steps.tag.outputs.pr }}
      tag: ${{ steps.tag.outputs.tag }}
    steps:
` + tag.step("Mint the tag App's token") + releasePleaseGuardStep + releasePleaseTagStep + `  relabel:
    name: Relabel
    runs-on: ubuntu-latest
    timeout-minutes: 10
    needs: [release-please, tag]
    if: needs.tag.outputs.pr != ''
    # Holds the release-please App's client ID and private key, limited to main.
    environment: release-please
    permissions:
      contents: read
    steps:
` + relabel.step("Mint the release-please App's token to relabel") + releasePleaseRelabelStep
}

// mutateEvery returns doc with every occurrence of old replaced by new, failing
// unless doc holds old at least once: a case that breaks a part every job
// shares names the part, not one job.
func mutateEvery(t *testing.T, doc, old, new string) string {
	t.Helper()

	if !strings.Contains(doc, old) {
		t.Fatalf("the fixture holds no %q", old)
	}

	return strings.ReplaceAll(doc, old, new)
}

// lineNumber matches the line a message names, which a case that shifts the
// document's lines changes.
var lineNumber = regexp.MustCompile(`line [0-9]+: `)

// added returns the messages of got whose text, with the line numbers removed,
// the accepted shape's messages do not hold: what the check says of got that it
// does not say of the shape it accepts. A check that accepts the shape reports
// nothing for it, so added is then got.
func added(messages, got []string) []string {
	known := make(map[string]bool, len(messages))

	for _, m := range messages {
		known[lineNumber.ReplaceAllString(m, "")] = true
	}

	var out []string

	for _, m := range got {
		if !known[lineNumber.ReplaceAllString(m, "")] {
			out = append(out, m)
		}
	}

	return out
}

// TestReleasePleaseWorkflow pins the release-please workflow of issues #157 and
// #159: one workflow that proposes the next version in a release PR on main and
// tags that PR when it merges, with the two Apps the maintainer decided on. The
// release-please App, held by the release-please environment, opens the release
// PR and relabels it once it is tagged; the tag App, held by the release-tag
// environment, creates the tag and bypasses the Release tag creation ruleset.
// Each App's token is minted where that App's work is done, from that
// environment's client-id and private key alone, and narrowed to the
// permissions the job needs, so neither job can read the other App's
// credentials, and the release-please job's token, which may merge a PR into
// main, is not the one that creates the tag. The tag job reads no output of the
// job that runs the third-party release-please action: the release-please
// App's public slug reaches its guard and tag steps as the constant
// APP_SLUG: shift2eu-release, and the release-please job declares no output.
// Both token steps pass client-id, never the deprecated app-id (#159).
//
// The check returns one message per problem and none for the accepted shape. A
// case compares what the check reports of its mutation with what it reports of
// the accepted shape, so a message the accepted shape already draws cannot make
// a case pass: each case adds a violation of its own, and wantIn is a part of
// it that names what the case changes.
func TestReleasePleaseWorkflow(t *testing.T) {
	t.Parallel()

	acceptedWorkflow := releasePleaseWorkflow
	accepted := check.ReleasePleaseWorkflow([]byte(acceptedWorkflow))

	// beforeReleasePlease returns the workflow with step inserted before the
	// release-please step, as a caller writes a step in the release-please
	// job.
	beforeReleasePlease := func(step string) string {
		return mutate(t, acceptedWorkflow, releasePleaseActionStep, "      - "+step+"\n"+releasePleaseActionStep)
	}

	// withTokens returns the workflow whose three token steps mint release,
	// tag and relabel.
	withTokens := releasePleaseAppsWorkflow

	tests := []struct {
		name     string
		workflow string
		// wantIn is a part of a message the check must return for the case
		// that the accepted shape does not draw; "" means none at all.
		wantIn string
	}{
		// The accepted shape, and the reading that must not depend on the
		// line ends.
		{name: "the accepted shape", workflow: acceptedWorkflow},
		{name: "CRLF line ends", workflow: strings.ReplaceAll(acceptedWorkflow, "\n", "\r\n")},

		// Two Apps, two environments, two token steps, and client-id on each
		// of them: one App's token may merge a PR into main and the other's may
		// bypass the tag ruleset, so neither job may hold the other's
		// credentials, and app-id is the input issue #159 replaces.
		{
			name:     "an app-id instead of client-id in the release-please job",
			workflow: withTokens(appToken{appID: "${{ vars.RELEASE_APP_ID }}", privateKey: releasePleaseAppToken.privateKey, permissions: releasePleaseAppToken.permissions}, tagAppToken, releasePleaseAppToken),
			wantIn:   "app-id",
		},
		{
			name:     "an app-id as well as client-id in the tag job",
			workflow: withTokens(releasePleaseAppToken, appToken{appID: "${{ vars.RELEASE_TAG_APP_ID }}", clientID: tagAppToken.clientID, privateKey: tagAppToken.privateKey, permissions: tagAppToken.permissions}, releasePleaseAppToken),
			wantIn:   "app-id",
		},
		{
			name:     "no client-id on the tag job's token",
			workflow: withTokens(releasePleaseAppToken, appToken{privateKey: tagAppToken.privateKey, permissions: tagAppToken.permissions}, releasePleaseAppToken),
			wantIn:   "client-id",
		},
		{
			name:     "no client-id on the release-please job's token",
			workflow: withTokens(appToken{privateKey: releasePleaseAppToken.privateKey, permissions: releasePleaseAppToken.permissions}, tagAppToken, releasePleaseAppToken),
			wantIn:   "client-id",
		},
		{
			name:     "the release-please job's client-id from a secret",
			workflow: withTokens(appToken{clientID: "${{ secrets.RELEASE_APP_CLIENT_ID }}", privateKey: releasePleaseAppToken.privateKey, permissions: releasePleaseAppToken.permissions}, tagAppToken, releasePleaseAppToken),
			wantIn:   "RELEASE_APP_CLIENT_ID",
		},
		{
			name:     "the release-please job's private-key from a variable",
			workflow: withTokens(appToken{clientID: releasePleaseAppToken.clientID, privateKey: "${{ vars.RELEASE_APP_PRIVATE_KEY }}", permissions: releasePleaseAppToken.permissions}, tagAppToken, releasePleaseAppToken),
			wantIn:   "secrets.RELEASE_APP_PRIVATE_KEY",
		},
		{
			name:     "the relabel job's client-id from the tag App",
			workflow: withTokens(releasePleaseAppToken, tagAppToken, appToken{clientID: "${{ vars.RELEASE_TAG_APP_CLIENT_ID }}", privateKey: releasePleaseAppToken.privateKey, permissions: releasePleaseAppToken.permissions}),
			wantIn:   "RELEASE_TAG_APP_CLIENT_ID",
		},
		{
			name:     "the relabel job's private-key from a variable",
			workflow: withTokens(releasePleaseAppToken, tagAppToken, appToken{clientID: releasePleaseAppToken.clientID, privateKey: "${{ vars.RELEASE_APP_PRIVATE_KEY }}", permissions: releasePleaseAppToken.permissions}),
			wantIn:   "secrets.RELEASE_APP_PRIVATE_KEY",
		},
		{
			name:     "the tag job's client-id from the release-please App",
			workflow: withTokens(releasePleaseAppToken, appToken{clientID: "${{ vars.RELEASE_APP_CLIENT_ID }}", privateKey: tagAppToken.privateKey, permissions: tagAppToken.permissions}, releasePleaseAppToken),
			wantIn:   "RELEASE_TAG_APP_CLIENT_ID",
		},
		{
			name:     "the tag job's private-key from a variable",
			workflow: withTokens(releasePleaseAppToken, appToken{clientID: tagAppToken.clientID, privateKey: "${{ vars.RELEASE_TAG_APP_PRIVATE_KEY }}", permissions: tagAppToken.permissions}, releasePleaseAppToken),
			wantIn:   "secrets.RELEASE_TAG_APP_PRIVATE_KEY",
		},
		{
			// The release-please job, in the release-please environment, reading
			// the tag App's variable: the one job that may merge reads a secret
			// of the App that may create a tag.
			name:     "the release-please job's client-id is the tag App's",
			workflow: withTokens(appToken{clientID: "${{ vars.RELEASE_TAG_APP_CLIENT_ID }}", privateKey: releasePleaseAppToken.privateKey, permissions: releasePleaseAppToken.permissions}, tagAppToken, releasePleaseAppToken),
			wantIn:   "RELEASE_TAG_APP_CLIENT_ID",
		},
		{
			// The other direction: the tag job, whose token bypasses the tag
			// ruleset, reading the release-please App's secret.
			name:     "the tag job's private-key is the release-please App's",
			workflow: withTokens(releasePleaseAppToken, appToken{clientID: tagAppToken.clientID, privateKey: "${{ secrets.RELEASE_APP_PRIVATE_KEY }}", permissions: tagAppToken.permissions}, releasePleaseAppToken),
			wantIn:   "RELEASE_TAG_APP_PRIVATE_KEY",
		},

		// The token is narrowed when it is minted: the release-please App's to
		// Contents and Pull requests write, which relabels the release PR, and
		// the tag App's to Contents write alone, with Pull requests read to
		// check that PR. Any other scope is one the job does not need.
		{
			name:     "the tag job's token asking for permission-pull-requests: write",
			workflow: withTokens(releasePleaseAppToken, appToken{clientID: tagAppToken.clientID, privateKey: tagAppToken.privateKey, permissions: []string{"permission-contents: write", "permission-pull-requests: write"}}, releasePleaseAppToken),
			wantIn:   "permission-pull-requests",
		},
		{
			name:     "the release-please job's token without permission-contents: write",
			workflow: withTokens(appToken{clientID: releasePleaseAppToken.clientID, privateKey: releasePleaseAppToken.privateKey, permissions: []string{"permission-pull-requests: write"}}, tagAppToken, releasePleaseAppToken),
			wantIn:   "permission-contents",
		},
		{
			name:     "the tag job's token without permission-contents: write",
			workflow: withTokens(releasePleaseAppToken, appToken{clientID: tagAppToken.clientID, privateKey: tagAppToken.privateKey, permissions: []string{"permission-pull-requests: read"}}, releasePleaseAppToken),
			wantIn:   "permission-contents",
		},
		{
			name:     "a third permission on the release-please job's token",
			workflow: withTokens(appToken{clientID: releasePleaseAppToken.clientID, privateKey: releasePleaseAppToken.privateKey, permissions: []string{"permission-contents: write", "permission-pull-requests: write", "permission-issues: write"}}, tagAppToken, releasePleaseAppToken),
			wantIn:   "permission-issues",
		},
		{
			name:     "a third permission on the tag job's token",
			workflow: withTokens(releasePleaseAppToken, appToken{clientID: tagAppToken.clientID, privateKey: tagAppToken.privateKey, permissions: []string{"permission-contents: write", "permission-pull-requests: read", "permission-issues: write"}}, releasePleaseAppToken),
			wantIn:   "permission-issues",
		},
		{
			name:     "permission-pull-requests: read on the release-please job's token",
			workflow: withTokens(appToken{clientID: releasePleaseAppToken.clientID, privateKey: releasePleaseAppToken.privateKey, permissions: []string{"permission-contents: write", "permission-pull-requests: read"}}, tagAppToken, releasePleaseAppToken),
			wantIn:   "permission-pull-requests",
		},

		// Each App's credentials live in an environment of its own, limited to
		// main, and each job runs in the environment whose App it needs.
		{
			name:     "the tag job in the release-please environment",
			workflow: mutate(t, acceptedWorkflow, "    environment: release-tag\n", "    environment: release-please\n"),
			wantIn:   "environment",
		},
		{
			name:     "no environment on the tag job",
			workflow: mutate(t, acceptedWorkflow, "    environment: release-tag\n", ""),
			wantIn:   "release-tag",
		},
		{
			name:     "the relabel job in the release-tag environment",
			workflow: mutate(t, acceptedWorkflow, "    if: needs.tag.outputs.pr != ''\n    # Holds the release-please App's client ID and private key, limited to main.\n    environment: release-please\n", "    if: needs.tag.outputs.pr != ''\n    environment: release-tag\n"),
			wantIn:   "environment",
		},
		{
			name:     "no environment on the release-please job",
			workflow: mutate(t, acceptedWorkflow, "    timeout-minutes: 15\n    # Holds the release-please App's client ID and private key, limited to main.\n    environment: release-please\n", "    timeout-minutes: 15\n"),
			wantIn:   "environment",
		},

		// The jobs run in order: the tag after the release PR is opened, the
		// relabel after the tag is created.
		{
			name:     "the tag job not depending on the release-please job",
			workflow: mutate(t, acceptedWorkflow, "    needs: release-please\n", ""),
			wantIn:   "needs",
		},
		{
			name:     "the relabel job not depending on the tag job",
			workflow: mutate(t, acceptedWorkflow, "    needs: [release-please, tag]", "    needs: release-please"),
			wantIn:   "tag",
		},

		// The job's own permissions are reads: the Apps' tokens, not
		// GITHUB_TOKEN, write, and with permissions: {} at the top level a job
		// that declares none holds none. That is not a privilege this check
		// refuses.
		{
			name:     "no job-level permissions",
			workflow: mutate(t, acceptedWorkflow, "    # GITHUB_TOKEN reads only: the App's token does the writing.\n    permissions:\n      contents: read\n    steps:\n", "    steps:\n"),
		},
		{
			name:     "write-all at the tag job level",
			workflow: mutate(t, acceptedWorkflow, "    permissions:\n      contents: read\n      actions: read\n", "    permissions: write-all\n"),
			wantIn:   "write-all",
		},
		{
			name:     "contents: write at a job level",
			workflow: mutateEvery(t, acceptedWorkflow, "      contents: read\n", "      contents: write\n"),
			wantIn:   "contents",
		},
		{
			name:     "actions: write at the tag job level",
			workflow: mutate(t, acceptedWorkflow, "      actions: read\n", "      actions: write\n"),
			wantIn:   "actions",
		},

		// The jobs run on the runner GitHub provides, never on a host this
		// repository does not control: each holds an App's token.
		{
			name:     "a self-hosted runner",
			workflow: mutateEvery(t, acceptedWorkflow, "runs-on: ubuntu-latest", "runs-on: self-hosted"),
			wantIn:   "runs-on",
		},
		{
			name:     "a self-hosted runner in a list",
			workflow: mutateEvery(t, acceptedWorkflow, "runs-on: ubuntu-latest", "runs-on: [self-hosted, linux]"),
			wantIn:   "runs-on",
		},

		// The trigger: a push to main alone. Any other trigger, any other
		// branch and any tag runs the workflow where no release PR belongs.
		{
			name:     "a pull_request trigger",
			workflow: mutate(t, acceptedWorkflow, "    branches: [main]\n", "    branches: [main]\n  pull_request:\n"),
			wantIn:   "pull_request",
		},
		{
			name:     "a workflow_dispatch trigger",
			workflow: mutate(t, acceptedWorkflow, "  push:\n    branches: [main]\n", "  workflow_dispatch:\n  push:\n    branches: [main]\n"),
			wantIn:   "workflow_dispatch",
		},
		{
			name:     "a second branch",
			workflow: mutate(t, acceptedWorkflow, "    branches: [main]", "    branches: [main, next]"),
			wantIn:   "branches",
		},
		{
			name:     "a tags trigger",
			workflow: mutate(t, acceptedWorkflow, "    branches: [main]\n", "    branches: [main]\n    tags: [\"v*.*.*\"]\n"),
			wantIn:   "tags",
		},
		{
			// on: push with nothing below it runs on a push of any branch and
			// any tag.
			name:     "a bare on: push",
			workflow: mutate(t, acceptedWorkflow, "on:\n  push:\n    branches: [main]\n", "on: push\n"),
			wantIn:   "push",
		},

		// Nothing through GITHUB_TOKEN: the Apps' tokens write, so a
		// workflow-level permission is a privilege the workflow does not need.
		{
			name:     "no top-level permissions",
			workflow: mutate(t, acceptedWorkflow, "permissions: {}\n", ""),
			wantIn:   "no top-level permissions",
		},
		{
			name:     "a top-level permissions with a scope",
			workflow: mutate(t, acceptedWorkflow, "permissions: {}", "permissions:\n  contents: write"),
			wantIn:   "permissions",
		},

		// The release-please step reads the App's token, never GITHUB_TOKEN or
		// github.token: a tag pushed with either does not start release.yml,
		// and the ruleset refuses it.
		{
			name:     "the release-please step reading secrets.GITHUB_TOKEN",
			workflow: mutate(t, acceptedWorkflow, "          token: ${{ steps.app.outputs.token }}", "          token: ${{ secrets.GITHUB_TOKEN }}"),
			wantIn:   "GITHUB_TOKEN",
		},
		{
			name:     "the release-please step reading github.token",
			workflow: mutate(t, acceptedWorkflow, "          token: ${{ steps.app.outputs.token }}", "          token: ${{ github.token }}"),
			wantIn:   "github.token",
		},
		{
			name:     "the release-please step reading another step's token",
			workflow: mutate(t, acceptedWorkflow, "          token: ${{ steps.app.outputs.token }}", "          token: ${{ steps.mint.outputs.token }}"),
			wantIn:   "steps.mint.outputs.token",
		},
		{
			name:     "no token on the release-please step",
			workflow: mutate(t, acceptedWorkflow, "        with:\n          token: ${{ steps.app.outputs.token }}\n", ""),
			wantIn:   "token",
		},
		{
			// The release-please step runs where the release-please App's
			// credentials are, in the release-please job: in the tag job it
			// would read the release-tag environment's variables and fail.
			name:     "the release-please action in the tag job",
			workflow: mutate(t, mutate(t, acceptedWorkflow, releasePleaseActionStep, ""), releasePleaseTagStep, releasePleaseActionStep+releasePleaseTagStep),
			wantIn:   "the release-please job",
		},

		// The tag job's steps: no tag while a release.yml run is in flight, and
		// the tag created through the API with the tag App's token.
		{
			name:     "no tagging step",
			workflow: mutate(t, acceptedWorkflow, releasePleaseTagStep, ""),
			wantIn:   "tag",
		},
		{
			// The tag is created where the tag App's credentials are, in the
			// tag job: a step with id: tag in the relabel job would hold the
			// release-please App's token, which bypasses no ruleset.
			name:     "the tagging step in the relabel job",
			workflow: mutate(t, mutate(t, acceptedWorkflow, releasePleaseTagStep, ""), releasePleaseRelabelStep, releasePleaseTagStep+releasePleaseRelabelStep),
			wantIn:   "the tag job",
		},
		{
			name:     "the tagging step reading secrets.GITHUB_TOKEN",
			workflow: mutate(t, acceptedWorkflow, "          GH_TOKEN: ${{ steps.app.outputs.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG:", "          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG:"),
			wantIn:   "GITHUB_TOKEN",
		},
		{
			name:     "the tagging step reading github.token",
			workflow: mutate(t, acceptedWorkflow, "          GH_TOKEN: ${{ steps.app.outputs.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG:", "          GH_TOKEN: ${{ github.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG:"),
			wantIn:   "github.token",
		},
		{
			name:     "no GH_TOKEN on the tagging step",
			workflow: mutate(t, acceptedWorkflow, "          GH_TOKEN: ${{ steps.app.outputs.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG:", "          GH_REPO: ${{ github.repository }}\n          APP_SLUG:"),
			wantIn:   "GH_TOKEN",
		},

		// The release-please App's slug is the author the steps of the tag job
		// check the merged release PR against, and each holds it as the constant
		// APP_SLUG: shift2eu-release, the App's public slug, never a job output
		// of the job that runs the third-party release-please action, and never
		// a variable or a secret of the repository. The tagging step's env: holds
		// nothing else.
		{
			name:     "no APP_SLUG on the tagging step",
			workflow: mutate(t, acceptedWorkflow, taggingStepEnv, "          GH_TOKEN: ${{ steps.app.outputs.token }}\n          GH_REPO: ${{ github.repository }}\n"),
			wantIn:   "APP_SLUG",
		},
		{
			name:     "an APP_SLUG from a variable",
			workflow: mutate(t, acceptedWorkflow, taggingStepEnv, "          GH_TOKEN: ${{ steps.app.outputs.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG: ${{ vars.RELEASE_APP_SLUG }}\n"),
			wantIn:   "APP_SLUG",
		},
		{
			// The tag job trusts nothing the release-please job reports: the
			// slug it tags for is its own constant, so a job output here is a
			// value the third-party action's job hands it.
			name:     "the tagging step's APP_SLUG from the release-please job's output",
			workflow: mutate(t, acceptedWorkflow, taggingStepEnv, "          GH_TOKEN: ${{ steps.app.outputs.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG: ${{ needs.release-please.outputs.app-slug }}\n"),
			wantIn:   "APP_SLUG",
		},
		{
			name:     "the guard's APP_SLUG from the release-please job's output",
			workflow: mutate(t, acceptedWorkflow, guardStepEnv, "          GH_TOKEN: ${{ github.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG: ${{ needs.release-please.outputs.app-slug }}\n"),
			wantIn:   "APP_SLUG",
		},
		{
			name:     "another slug on the tagging step",
			workflow: mutate(t, acceptedWorkflow, taggingStepEnv, "          GH_TOKEN: ${{ steps.app.outputs.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG: someone-else\n"),
			wantIn:   "APP_SLUG",
		},
		{
			name:     "another slug on the guard",
			workflow: mutate(t, acceptedWorkflow, guardStepEnv, "          GH_TOKEN: ${{ github.token }}\n          GH_REPO: ${{ github.repository }}\n          APP_SLUG: someone-else\n"),
			wantIn:   "APP_SLUG",
		},
		{
			// Anywhere in the tag job, not on APP_SLUG alone: a second
			// reference to the release-please job's outputs is a second value
			// the tag job trusts, whatever env: key it reaches the shell through.
			name:     "the guard reading the release-please job's output as well",
			workflow: mutate(t, acceptedWorkflow, guardStepEnv, guardStepEnv+"          RELEASE_SLUG: ${{ needs.release-please.outputs.app-slug }}\n"),
			wantIn:   "needs.release-please.outputs",
		},
		{
			// The release-please job declares no output at all: the App's slug
			// is the tag job's own constant, and an output on that job invites
			// the job after it to trust a value the third-party action's job
			// reports.
			name:     "an app-slug output on the release-please job",
			workflow: mutate(t, acceptedWorkflow, "    # GITHUB_TOKEN reads only: the App's token does the writing.\n    permissions:\n      contents: read\n    steps:\n", "    # GITHUB_TOKEN reads only: the App's token does the writing.\n    permissions:\n      contents: read\n    outputs:\n      app-slug: ${{ steps.app.outputs.app-slug }}\n    steps:\n"),
			wantIn:   "outputs",
		},
		{
			name:     "another env key on the tagging step",
			workflow: mutate(t, acceptedWorkflow, taggingStepEnv, taggingStepEnv+"          GH_DEBUG: \"1\"\n"),
			wantIn:   "GH_DEBUG",
		},
		{
			// The guard's env: is exactly its token, the repository and the
			// release-please App's slug, as the tagging step's is: a variable it
			// holds is a value its script reads and one more thing the tag job
			// trusts, whatever the value reads.
			name:     "an extra env key on the guard",
			workflow: mutate(t, acceptedWorkflow, guardStepEnv, guardStepEnv+"          EXTRA: x\n"),
			wantIn:   "EXTRA",
		},

		// The only needs expressions the workflow holds are the tag job's two
		// outputs, read by the relabel job's if: and by its step's env:. Every
		// other reach for a job's outputs is a value a job trusts: the bracket
		// form reads the same outputs under another spelling, toJSON(needs)
		// hands the shell every output of every job before it, and a third
		// output of the tag job is one no step of it ever wrote.
		{
			name:     "a needs expression in the bracket form on the tagging step",
			workflow: mutate(t, acceptedWorkflow, taggingStepEnv, taggingStepEnv+"          RELEASE_SLUG: ${{ needs['release-please'].outputs.x }}\n"),
			wantIn:   "needs[",
		},
		{
			name:     "toJSON(needs) on the relabelling step",
			workflow: mutate(t, acceptedWorkflow, "          TAG: ${{ needs.tag.outputs.tag }}\n", "          TAG: ${{ needs.tag.outputs.tag }}\n          ALL_OUTPUTS: ${{ toJSON(needs) }}\n"),
			wantIn:   "needs",
		},
		{
			name:     "another output of the tag job on the relabelling step",
			workflow: mutate(t, acceptedWorkflow, "          TAG: ${{ needs.tag.outputs.tag }}\n", "          TAG: ${{ needs.tag.outputs.tag }}\n          OTHER: ${{ needs.tag.outputs.other }}\n"),
			wantIn:   "needs.tag.outputs.other",
		},

		// GitHub Actions matches context names case-insensitively, so a
		// reach for a job's outputs has to be refused in every spelling: the
		// check may not answer Needs, NEEDS or NeEdS where it answers needs.
		{
			name:     "a needs expression in the bracket form and upper case on the tagging step",
			workflow: mutate(t, acceptedWorkflow, taggingStepEnv, taggingStepEnv+"          RELEASE_SLUG: ${{ Needs['release-please'].outputs.x }}\n"),
			wantIn:   "Needs[",
		},
		{
			name:     "toJSON of the upper-case needs context on the relabelling step",
			workflow: mutate(t, acceptedWorkflow, "          TAG: ${{ needs.tag.outputs.tag }}\n", "          TAG: ${{ needs.tag.outputs.tag }}\n          ALL_OUTPUTS: ${{ toJSON(NEEDS) }}\n"),
			wantIn:   "NEEDS",
		},
		{
			name:     "an upper-case needs expression on the relabelling step",
			workflow: mutate(t, acceptedWorkflow, "          TAG: ${{ needs.tag.outputs.tag }}\n", "          TAG: ${{ needs.tag.outputs.tag }}\n          OTHER: ${{ NEEDS.tag.outputs.other }}\n"),
			wantIn:   "NEEDS.tag.outputs.other",
		},

		// An allowed output is the whole of the expression it appears in, and
		// nothing more: ${{ needs.tag.outputs.pr }} is the tagged PR's number
		// alone, while the same reference inside a larger expression is a
		// value the expression computes and no longer that number. The relabel
		// job's if: stays the one allowed condition on the tagged PR.
		{
			name:     "an allowed tag output beside a fallback on the relabelling step",
			workflow: mutate(t, acceptedWorkflow, "          PR: ${{ needs.tag.outputs.pr }}\n", "          PR: ${{ needs.tag.outputs.pr || 'x' }}\n"),
			wantIn:   "||",
		},
		{
			name:     "an allowed tag output inside fromJSON on the relabelling step",
			workflow: mutate(t, acceptedWorkflow, "          PR: ${{ needs.tag.outputs.pr }}\n", "          PR: ${{ fromJSON(needs.tag.outputs.pr)[0] }}\n"),
			wantIn:   "fromJSON",
		},
		{
			// The same larger expression under an env: key of its own, which
			// the step's env: check only names: the value it holds is the
			// needs reach's to refuse, whatever key it reaches the shell
			// through.
			name:     "an allowed tag output in a fallback under another env key of the relabelling step",
			workflow: mutate(t, acceptedWorkflow, "          TAG: ${{ needs.tag.outputs.tag }}\n", "          TAG: ${{ needs.tag.outputs.tag }}\n          FALLBACK: ${{ needs.tag.outputs.pr || 'x' }}\n"),
			wantIn:   "||",
		},
		{
			name:     "an allowed tag output inside fromJSON under another env key of the relabelling step",
			workflow: mutate(t, acceptedWorkflow, "          TAG: ${{ needs.tag.outputs.tag }}\n", "          TAG: ${{ needs.tag.outputs.tag }}\n          FROM: ${{ fromJSON(needs.tag.outputs.pr)[0] }}\n"),
			wantIn:   "fromJSON",
		},

		// The guard reads release.yml's runs through the repository it names
		// in GH_REPO, so that name is github.repository and never the
		// caller's default: another repository's runs would let a release.yml
		// run in flight publish while this workflow tags.
		{
			name:     "the guard has no GH_REPO",
			workflow: mutate(t, acceptedWorkflow, guardStepEnv, "          GH_TOKEN: ${{ github.token }}\n          APP_SLUG: shift2eu-release\n"),
			wantIn:   "GH_REPO",
		},
		{
			name:     "the guard's GH_REPO is not github.repository",
			workflow: mutate(t, acceptedWorkflow, guardStepEnv, "          GH_TOKEN: ${{ github.token }}\n          GH_REPO: someone/else\n          APP_SLUG: shift2eu-release\n"),
			wantIn:   "GH_REPO",
		},

		// The tagging step creates a tag and nothing else: a GitHub Release is
		// GoReleaser's, from release.yml, and no script calls a host of its own.
		{
			name:     "a tagging step creating a GitHub Release",
			workflow: mutate(t, acceptedWorkflow, "          pending=\"autorelease: pending\"\n", "          gh release create v1.2.3 --generate-notes\n          pending=\"autorelease: pending\"\n"),
			wantIn:   "gh release",
		},
		{
			name:     "a tagging step calling the releases endpoint",
			workflow: mutate(t, acceptedWorkflow, "          pending=\"autorelease: pending\"\n", "          gh api \"repos/${{ github.repository }}/releases\" --jq '.[].id'\n          pending=\"autorelease: pending\"\n"),
			wantIn:   "releases",
		},
		{
			// The list of the release PRs is read through pulls, never through
			// the issues listing: neither App's token holds an Issues
			// permission, so GitHub answers repos/<r>/issues with 403.
			name:     "a tagging step listing the issues endpoint",
			workflow: mutate(t, acceptedWorkflow, "          pending=\"autorelease: pending\"\n", "          gh api \"repos/${GH_REPO}/issues?state=closed\" --jq '.[].number'\n          pending=\"autorelease: pending\"\n"),
			wantIn:   "endpoint issues",
		},
		{
			name:     "a tagging step calling a host of its own with curl",
			workflow: mutate(t, acceptedWorkflow, "          pending=\"autorelease: pending\"\n", "          curl -fsS https://example.com/release > /dev/null\n          pending=\"autorelease: pending\"\n"),
			wantIn:   "curl",
		},
		{
			name:     "a step pushing a tag with git push",
			workflow: beforeReleasePlease("name: Push the tag\n        run: git push origin v1.2.3"),
			wantIn:   "git push",
		},

		// One release at a time, and never a tag at a release that is already
		// queued or running: with a group of its own this workflow is never
		// cancelled, so the step that tags waits until release.yml holds no run.
		{
			name:     "no wait for a release in flight",
			workflow: mutate(t, acceptedWorkflow, releasePleaseGuardStep, ""),
			wantIn:   "release.yml",
		},
		{
			name:     "a wait that reads another workflow's runs",
			workflow: mutate(t, acceptedWorkflow, "actions/workflows/release.yml/runs", "actions/workflows/ci.yml/runs"),
			wantIn:   "release.yml",
		},

		// Every status GitHub may hold a run of release.yml in is waited for: a
		// run that is requested, waiting or pending may still start and publish,
		// so a tag created while one exists can be cancelled by it. Each case
		// leaves one status out of the guard's loop.
		{
			name:     "a wait that leaves out requested runs",
			workflow: mutate(t, acceptedWorkflow, "for status in requested waiting pending queued in_progress; do", "for status in waiting pending queued in_progress; do"),
			wantIn:   "requested",
		},
		{
			name:     "a wait that leaves out waiting runs",
			workflow: mutate(t, acceptedWorkflow, "for status in requested waiting pending queued in_progress; do", "for status in requested pending queued in_progress; do"),
			wantIn:   "waiting",
		},
		{
			name:     "a wait that leaves out pending runs",
			workflow: mutate(t, acceptedWorkflow, "for status in requested waiting pending queued in_progress; do", "for status in requested waiting queued in_progress; do"),
			wantIn:   "pending",
		},
		{
			name:     "a wait that leaves out queued runs",
			workflow: mutate(t, acceptedWorkflow, "for status in requested waiting pending queued in_progress; do", "for status in requested waiting pending in_progress; do"),
			wantIn:   "queued",
		},
		{
			name:     "a wait that leaves out in_progress runs",
			workflow: mutate(t, acceptedWorkflow, "for status in requested waiting pending queued in_progress; do", "for status in requested waiting pending queued; do"),
			wantIn:   "in_progress",
		},

		// The wait reads release.yml's runs with GITHUB_TOKEN alone: either
		// App's token is for the release PR and the tag, and handing it to a
		// third step widens its reach.
		{
			name:     "a wait reading the release-please App's token",
			workflow: mutate(t, acceptedWorkflow, "          GH_TOKEN: ${{ github.token }}\n", "          GH_TOKEN: ${{ steps.app.outputs.token }}\n"),
			wantIn:   "guard",
		},
		{
			name:     "a wait reading secrets.GITHUB_TOKEN",
			workflow: mutate(t, acceptedWorkflow, "          GH_TOKEN: ${{ github.token }}\n", "          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n"),
		},
		{
			name:     "no GH_TOKEN on the wait",
			workflow: mutate(t, acceptedWorkflow, "          GH_TOKEN: ${{ github.token }}\n", ""),
			wantIn:   "GH_TOKEN",
		},

		// Each App's token reaches the steps of the job that needs it and no
		// more: release-please's token and the relabel step's GH_TOKEN for the
		// release-please App, the tagging step's GH_TOKEN for the tag App.
		// Anywhere else it is a wider reach than the workflow needs.
		{
			name:     "the App token on a step of its own",
			workflow: beforeReleasePlease("name: Read the App token\n        env:\n          GH_TOKEN: ${{ steps.app.outputs.token }}\n        run: gh api \"repos/${{ github.repository }}/pulls\" --jq '.[].number'"),
			wantIn:   "outputs.token",
		},

		// Every action is pinned by a 40-hex commit digest, which a Renovate
		// update moves and which a tag or branch never is.
		{
			name:     "the App token action pinned by tag",
			workflow: mutateEvery(t, acceptedWorkflow, "create-github-app-token@"+appCommit, "create-github-app-token@v1"),
			wantIn:   "create-github-app-token@v1",
		},
		{
			name:     "the release-please action pinned by branch",
			workflow: mutate(t, acceptedWorkflow, "release-please-action@"+releasePleaseActionPin, "release-please-action@main"),
			wantIn:   "release-please-action@main",
		},
		{
			name:     "the release-please action pinned by a short commit",
			workflow: mutate(t, acceptedWorkflow, "release-please-action@"+releasePleaseActionPin, "release-please-action@0123456"),
			wantIn:   "release-please-action@0123456",
		},

		// Nothing publishes here: publishing, signing and attesting stay in
		// release.yml, so the signing identity keeps naming that workflow. Each
		// case adds the step to the release-please job and wants it named.
		{
			name:     "a goreleaser step",
			workflow: beforeReleasePlease("uses: goreleaser/goreleaser-action@a4f02ab5355a589de60d917a659ce617ed19b3de"),
			wantIn:   "goreleaser",
		},
		{
			name:     "a goreleaser run",
			workflow: beforeReleasePlease("name: Release\n        run: goreleaser release --clean"),
			wantIn:   "goreleaser",
		},
		{
			name:     "a cosign step",
			workflow: beforeReleasePlease("uses: sigstore/cosign-installer@1e69f48acb82d1966a394da916b4c1698aa569d6"),
			wantIn:   "cosign",
		},
		{
			name:     "a docker login step",
			workflow: beforeReleasePlease("uses: docker/login-action@8aa5831fb8d48d8f76594c7ddde61ae58019f550"),
			wantIn:   "docker/login-action",
		},
		{
			name:     "a docker build and push step",
			workflow: beforeReleasePlease("uses: docker/build-push-action@dc802804100637a589fabce1cb79ff13a1411302"),
			wantIn:   "docker/build-push-action",
		},
		{
			name:     "an attest step",
			workflow: beforeReleasePlease("uses: actions/attest@9000827ccba6bdab643e8b6fd33ac0654aef8333"),
			wantIn:   "actions/attest",
		},
		{
			name:     "a docker push run",
			workflow: beforeReleasePlease("name: Push the image\n        run: docker push ghcr.io/timothymamo/shift2eu:latest"),
			wantIn:   "docker push",
		},

		// The concurrency group is this workflow's own: GitHub keeps one pending
		// run per group and cancels the older pending one when a new run queues,
		// so sharing the group release.yml uses would let a later push to main
		// cancel a release that is waiting to run. A run in progress is never
		// cancelled (ADR 0015).
		{
			name:     "no concurrency",
			workflow: mutate(t, acceptedWorkflow, "concurrency:\n  group: release-please\n  cancel-in-progress: false\n", ""),
			wantIn:   "concurrency",
		},
		{
			name:     "no concurrency group",
			workflow: mutate(t, acceptedWorkflow, "  group: release-please\n", ""),
			wantIn:   "group",
		},
		{
			name:     "the concurrency group release.yml uses",
			workflow: mutate(t, acceptedWorkflow, "group: release-please", "group: release"),
			wantIn:   "release.yml",
		},
		{
			name:     "a concurrency that cancels the run in progress",
			workflow: mutate(t, acceptedWorkflow, "cancel-in-progress: false", "cancel-in-progress: true"),
			wantIn:   "cancel-in-progress",
		},

		// A document the check cannot read tells it nothing, so it is refused
		// rather than passed.
		{name: "an empty document", workflow: "", wantIn: "empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := check.ReleasePleaseWorkflow([]byte(tc.workflow))

			if tc.wantIn == "" {
				if len(got) != 0 {
					t.Errorf("ReleasePleaseWorkflow() = %q, want no message", got)
				}

				return
			}

			fresh := added(accepted, got)
			if len(fresh) == 0 {
				t.Errorf("ReleasePleaseWorkflow() adds no message to the accepted shape's %q, want one naming %q", got, tc.wantIn)

				return
			}

			if joined := strings.Join(fresh, "\n"); !strings.Contains(joined, tc.wantIn) {
				t.Errorf("ReleasePleaseWorkflow() = %q, want a message containing %q", fresh, tc.wantIn)
			}
		})
	}
}

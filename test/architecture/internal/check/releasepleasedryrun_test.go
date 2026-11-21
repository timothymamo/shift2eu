package check_test

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// ciReleasePleaseDryRun is the part of .github/workflows/ci.yml that
// check.ReleasePleaseDryRun pins (issue #142), built from the repository's
// ci.yml: the cases below change one part of it.
var ciReleasePleaseDryRun = ciReleasePleaseDryRunFrom(repoCI)

// ciReleasePleaseDryRunTemplate is ciReleasePleaseDryRun with its pin a
// placeholder (workflowPins): the workflow runs on a pull_request and on a push
// to main, GITHUB_TOKEN only reads, and one job runs
// .github/scripts/release-please-dry-run.sh, which reads the release-please
// workflow's guard and tag steps and runs their scripts against a fake gh. The
// dry run reaches no network, writes nothing and needs no credential, so the
// job holds no write permission and no secret.
const ciReleasePleaseDryRunTemplate = `name: CI

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

jobs:
  release-please:
    name: Release please dry run
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: {{pin actions/checkout}}
        with:
          persist-credentials: false
      - name: Tag and guard scripts on sample API responses
        run: .github/scripts/release-please-dry-run.sh
`

// ciReleasePleaseDryRunFrom returns ciReleasePleaseDryRunTemplate with the pins
// of ci, a ci.yml, as ciDryRunFrom fills the image job's in (issue #440).
func ciReleasePleaseDryRunFrom(ci []byte) string {
	return pinsOf(ci).expand(ciReleasePleaseDryRunTemplate)
}

// TestReleasePleaseDryRun pins the CI half of issue #142: ci.yml runs the dry
// run of release-please.yml's guard and tag steps on every pull request and on
// every push to main, in a job that holds no write permission and no secret.
// A pull request that changes how the tag's version is read or when the step
// refuses to tag therefore fails before it can merge, and the dry run itself
// cannot reach the repository or the network with anything but a read.
//
// The check returns one message per problem and none for
// ciReleasePleaseDryRun, whose every part each case mutates once.
func TestReleasePleaseDryRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ci   string
		// wantIn is a part of the message the check must return; "" means
		// none at all.
		wantIn string
	}{
		// The accepted shape.
		{name: "the accepted shape", ci: ciReleasePleaseDryRun},

		// The dry run must run, and run where a change to the workflow is
		// reviewed before it merges: on the pull request that changes it, and
		// on the push to main that follows the merge.
		{
			name:   "no job running the dry run",
			ci:     mutate(t, ciReleasePleaseDryRun, "        run: .github/scripts/release-please-dry-run.sh", "        run: .github/scripts/other.sh"),
			wantIn: ".github/scripts/release-please-dry-run.sh",
		},
		{
			name:   "no pull_request trigger",
			ci:     mutate(t, ciReleasePleaseDryRun, "  pull_request:\n", ""),
			wantIn: "pull_request",
		},
		{
			name:   "no push to main trigger",
			ci:     mutate(t, ciReleasePleaseDryRun, "  push:\n    branches: [main]\n", ""),
			wantIn: "push",
		},

		// The dry run reaches no network, writes nothing and needs no
		// credential, so any write permission or secret it holds is a reach it
		// does not use and cannot be trusted with.
		{
			name:   "a write permission on the dry run job",
			ci:     mutate(t, ciReleasePleaseDryRun, "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-latest\n    permissions:\n      contents: write\n"),
			wantIn: "write",
		},
		{
			// A job that declares no permissions: holds the workflow's, so a
			// top-level write-all is the dry run job's too.
			name:   "a top-level write-all",
			ci:     mutate(t, ciReleasePleaseDryRun, "permissions:\n  contents: read\n", "permissions: write-all\n"),
			wantIn: "write-all",
		},
		{
			name:   "a secret on the dry run step",
			ci:     mutate(t, ciReleasePleaseDryRun, "      - name: Tag and guard scripts on sample API responses\n", "      - name: Tag and guard scripts on sample API responses\n        env:\n          GH_TOKEN: ${{ secrets.RELEASE_APP_PRIVATE_KEY }}\n"),
			wantIn: "secret",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := check.ReleasePleaseDryRun([]byte(tc.ci))
			joined := strings.Join(got, "\n")

			if tc.wantIn == "" {
				if len(got) != 0 {
					t.Errorf("ReleasePleaseDryRun() = %q, want no message", got)
				}

				return
			}

			if !strings.Contains(joined, tc.wantIn) {
				t.Errorf("ReleasePleaseDryRun() = %q, want a message containing %q", got, tc.wantIn)
			}
		})
	}
}

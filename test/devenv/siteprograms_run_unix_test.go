//go:build unix

// Pins the make target the release's site-programs job runs (PRD #297 milestone 3): make site-programs
// builds the two programs the deploy and the live check run, bin/sitedeploy and bin/sitecheck, exactly as
// the release builds them - static, with -trimpath, each from its own package - so a pull request builds
// what the release builds, and the static build's environment is the Makefile's to set rather than a
// workflow line's.
//
// The test runs the real recipe of the real Makefile with a stand-in go first on PATH: no test builds the
// programs themselves.
package devenv_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// siteProgramsTarget is the target the release's site-programs job runs.
const siteProgramsTarget = "site-programs"

// siteProgramsGoScript is the stand-in go the test puts first on PATH: it logs the environment the recipe
// gives it and its arguments, one per line between the run's start and its "end" line, so the test can tell
// the two builds apart and say what each was handed.
const siteProgramsGoScript = `#!/bin/sh
printf 'CGO_ENABLED=%s\n' "${CGO_ENABLED-unset}" >> "$FAKE_GO_LOG"
for arg in "$@"; do printf 'arg=%s\n' "$arg" >> "$FAKE_GO_LOG"; done
printf 'end\n' >> "$FAKE_GO_LOG"
`

// siteProgramsGoRuns returns the logged runs of the stand-in go, each as the lines it logged in order.
func siteProgramsGoRuns(t *testing.T, log string) [][]string {
	t.Helper()

	var (
		runs [][]string
		run  []string
	)

	for _, line := range splitLines(readFileString(t, log)) {
		if line == "end" {
			runs = append(runs, run)
			run = nil

			continue
		}

		run = append(run, line)
	}

	if len(run) != 0 {
		t.Fatalf("the stand-in go logged a run that never ended: %q", run)
	}

	return runs
}

// TestMakeSiteProgramsBuildsTheDeployAndLiveCheckPrograms pins make site-programs (PRD #297 milestone 3):
// one static, -trimpath go build into bin/sitedeploy from ./ci/sitedeploy and one into bin/sitecheck from
// ./ci/sitecheck, so the release's site-programs job builds the very programs its site-deploy and
// site-check jobs run, and their digests bind what those jobs run to what the job built.
func TestMakeSiteProgramsBuildsTheDeployAndLiveCheckPrograms(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	log := filepath.Join(dir, "go.log")

	writeFileMode(t, filepath.Join(binDir, "go"), []byte(siteProgramsGoScript), 0o700)

	env := makeEnv(t, map[string]string{
		"PATH":        binDir + ":/usr/local/bin:/usr/bin:/bin",
		"FAKE_GO_LOG": log,
	})

	stdout, stderr, err := runMake(t, []string{siteProgramsTarget}, env)
	if err != nil {
		t.Fatalf("make %s exited non-zero: %v\nstdout:\n%s\nstderr:\n%s", siteProgramsTarget, err, stdout, stderr)
	}

	// The static build is the Makefile's: the binaries the release ships are
	// built with cgo off and without the build paths of the runner that built
	// them, whatever go's default for the host would be.
	want := [][]string{
		{"CGO_ENABLED=0", "arg=build", "arg=-trimpath", "arg=-o", "arg=bin/sitedeploy", "arg=./ci/sitedeploy"},
		{"CGO_ENABLED=0", "arg=build", "arg=-trimpath", "arg=-o", "arg=bin/sitecheck", "arg=./ci/sitecheck"},
	}

	if diff := cmp.Diff(want, siteProgramsGoRuns(t, log)); diff != "" {
		t.Errorf("make %s does not build exactly the two site programs as the release builds them (-want +got):\n%s\nstdout:\n%s\nstderr:\n%s",
			siteProgramsTarget, diff, stdout, stderr)
	}

	// The recipe runs nothing else: one go command per program, no test and no
	// go run of another builder.
	if got := strings.Count(readFileString(t, log), "arg=build\n"); got != len(want) {
		t.Errorf("make %s ran %d go builds, want exactly %d: one for each program the release runs", siteProgramsTarget, got, len(want))
	}
}

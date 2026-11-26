package github_test

// The summary line names the refused check (issue #640), the GitHub side's
// half: every configuration refusal of the publish program's GitHub side
// prints "<class line>: <check phrase>: see runbook", the phrase being fixed
// words, never a value, a token, a key, a path or a number the step read.
// This file is that fixed list, one row per refusal, each driven by the real
// function that makes it, and each read back through the core's
// publish.OutcomeOf, which the entry point maps to the run's one line.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// TestGitHubRefusalNamesTheFailedCheck pins the GitHub side's fixed list:
// every configuration refusal's class, one line naming the check, and exit 9.
func TestGitHubRefusalNamesTheFailedCheck(t *testing.T) {
	t.Parallel()

	const (
		lineConfigToken      = "configuration invalid: GH_TOKEN is set and not empty: see runbook"
		lineConfigClientOpen = "configuration invalid: the client is open: see runbook"
		lineConfigTempDir    = "configuration invalid: the temporary directory is absolute and an existing directory: see runbook"
		lineConfigTimeout    = "configuration invalid: the timeout is not negative: see runbook"
	)

	for _, tc := range []struct {
		name string
		// source is the refusal's site in the GitHub side.
		source string
		// want is the one line the entry point prints.
		want string
		// drive makes the refusal through the real function and returns its
		// error.
		drive func(t *testing.T) error
	}{
		// The client's configuration.
		{
			name:   "an empty token",
			source: "client.go: NewClient",
			want:   lineConfigToken,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  newFakeGH(t).Path,
					Token:   github.NewToken(""),
					TempDir: t.TempDir(),
					Scope:   testScope(),
				})

				return err
			},
		},
		{
			name:   "a relative gh path",
			source: "client.go: NewClient",
			want:   "configuration invalid: the gh path is absolute: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  "bin/" + fakeGHName,
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: t.TempDir(),
					Scope:   testScope(),
				})

				return err
			},
		},
		{
			name:   "a gh path that names no file",
			source: "client.go: NewClient",
			want:   "configuration invalid: the gh path names an existing file: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  filepath.Join(t.TempDir(), "no-such-gh"),
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: t.TempDir(),
					Scope:   testScope(),
				})

				return err
			},
		},
		{
			name:   "a relative temporary directory",
			source: "client.go: NewClient",
			want:   lineConfigTempDir,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  newFakeGH(t).Path,
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: "tmp",
					Scope:   testScope(),
				})

				return err
			},
		},
		{
			name:   "a temporary directory that does not exist",
			source: "client.go: NewClient",
			want:   lineConfigTempDir,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  newFakeGH(t).Path,
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: filepath.Join(t.TempDir(), "missing"),
					Scope:   testScope(),
				})

				return err
			},
		},
		{
			name:   "a scope the core's NewRun refuses",
			source: "client.go: validateScope",
			want:   "configuration invalid: GITHUB_RUN_ID is a decimal of up to 20 digits: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				scope := testScope()
				scope.RunID = "12ab"

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  newFakeGH(t).Path,
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: t.TempDir(),
					Scope:   scope,
				})

				return err
			},
		},
		{
			name:   "a scope commit that is not 40 lowercase hex",
			source: "client.go: validateScope",
			want:   "configuration invalid: the scope's commit is 40 lowercase hex: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				scope := testScope()
				scope.Commit = "0123"

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  newFakeGH(t).Path,
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: t.TempDir(),
					Scope:   scope,
				})

				return err
			},
		},
		{
			name:   "a publish scope without its commit",
			source: "client.go: NewPublishClient",
			want:   "configuration invalid: a publish scope names its commit: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewPublishClient(github.ClientConfig{
					GHPath:  newFakeGH(t).Path,
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: t.TempDir(),
					Scope:   artifactScope(),
				})

				return err
			},
		},
		{
			name:   "a negative call timeout",
			source: "client.go: NewClient",
			want:   lineConfigTimeout,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:      newFakeGH(t).Path,
					Token:       github.NewToken(fixtureToken(t)),
					TempDir:     t.TempDir(),
					Scope:       testScope(),
					CallTimeout: -time.Second,
				})

				return err
			},
		},
		{
			name:   "gh's configuration directory that cannot be made",
			source: "client.go: NewClient",
			want:   "configuration invalid: gh's configuration directory is made: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  newFakeGH(t).Path,
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: unwritableDir(t),
					Scope:   testScope(),
				})

				return err
			},
		},
		{
			name:   "gh's configuration directory that cannot be removed",
			source: "client.go: Client.Close",
			want:   "configuration invalid: gh's configuration directory is removed: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				dir := t.TempDir()

				client, err := github.NewClient(github.ClientConfig{
					GHPath:  newFakeGH(t).Path,
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: dir,
					Scope:   testScope(),
				})
				if err != nil {
					t.Fatalf("NewClient: %v", err)
				}

				closeAtEnd(t, client)

				makeUnwritable(t, dir)

				return client.Close()
			},
		},
		{
			name:   "a call on no open client",
			source: "client.go: Client.Call",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				var client *github.Client

				_, err := client.Call(t.Context(), github.Request{Method: "GET", Endpoint: fixtureReleaseEndpoint})

				return err
			},
		},

		// The calls that hold no client at all: one phrase, "the client is
		// open", one row per call.
		{
			name:   "a release check with no client",
			source: "release.go: CheckRelease",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.CheckRelease(t.Context(), nil, fixtureTag)
			},
		},
		{
			name:   "an image check with no client",
			source: "release.go: CheckImageDigest",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.CheckImageDigest(t.Context(), nil, fixtureTag, fixtureDigest)
			},
		},
		{
			name:   "an issue listing with no client",
			source: "issues.go: ListAuditIssues",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.ListAuditIssues(t.Context(), nil)

				return err
			},
		},
		{
			name:   "an issue creation with no client",
			source: "issues.go: CreateIssue",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.CreateIssue(t.Context(), nil, publish.IssueText{})
			},
		},
		{
			name:   "a commit-runs check with no client",
			source: "commitruns.go: CheckCommitRuns",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.CheckCommitRuns(t.Context(), nil, nil)
			},
		},
		{
			name:   "an advisory listing with no client",
			source: "advisories.go: ListAuditAdvisories",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.ListAuditAdvisories(t.Context(), nil)

				return err
			},
		},
		{
			name:   "advisory writes with no client",
			source: "advisories.go: FileAdvisories",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.FileAdvisories(t.Context(), nil, github.AdvisoryPlan{})
			},
		},
		{
			name:   "a report publication with no client",
			source: "report.go: PublishReport",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.PublishReport(t.Context(), nil, github.ReportPlan{})
			},
		},
		{
			name:   "an artifact check with no client",
			source: "artifacts.go: CheckArtifacts",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.CheckArtifacts(t.Context(), nil, github.JobModel)

				return err
			},
		},
		{
			name:   "a publication with no publish client",
			source: "publish.go: Publish",
			want:   lineConfigClientOpen,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.Publish(t.Context(), github.PublishConfig{})

				return err
			},
		},
		{
			name:   "a publication with no scanner",
			source: "publish.go: Publish",
			want:   "configuration invalid: the scanner is open: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t).Path)

				_, err := github.Publish(t.Context(), github.PublishConfig{
					Client: client,
					Run:    publishRun(t),
				})

				return err
			},
		},
		{
			name:   "a publication of a run that is not the client's",
			source: "publish.go: Publish",
			want:   "configuration invalid: the run is the client's: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t).Path)

				run := publishRun(t)
				run.ID = "1987654321"

				_, err := github.Publish(t.Context(), github.PublishConfig{
					Client:   client,
					Gitleaks: newGitleaks(t),
					Run:      run,
				})

				return err
			},
		},

		// The result and the previous report the run reads from its checkout.
		{
			name:   "a result path that is not absolute and clean",
			source: "load.go: LoadResult",
			want:   "configuration invalid: the result's path is absolute and clean: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.LoadResult("result/"+valueMarker, fixtureCommit)

				return err
			},
		},
		{
			name:   "a previous report's directory that is not absolute and clean",
			source: "load.go: LoadPrevious",
			want:   "configuration invalid: the previous report's directory is absolute and clean: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.LoadPrevious("reports/" + valueMarker)

				return err
			},
		},
		{
			name:   "a previous report's directory that does not exist",
			source: "load.go: LoadPrevious",
			want:   "configuration invalid: the previous report's directory is a directory: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.LoadPrevious(filepath.Join(t.TempDir(), "missing"))

				return err
			},
		},
		{
			name:   "a previous report's directory that cannot be listed",
			source: "load.go: LoadPrevious",
			want:   "configuration invalid: the previous report's directory can be listed: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.LoadPrevious(unlistableDir(t))

				return err
			},
		},

		// The artifact check and its hand-off file.
		{
			name:   "an artifact check of a job the program does not have",
			source: "artifacts.go: CheckArtifacts",
			want:   "configuration invalid: the job is model or publish: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.CheckArtifacts(t.Context(), nil, github.Job(99))

				return err
			},
		},
		{
			name:   "an artifact file in a relative directory",
			source: "artifacts.go: WriteArtifact",
			want:   "configuration invalid: the artifact's directory is absolute: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				return github.WriteArtifact("tmp/"+valueMarker, github.Artifact{Name: artifactChecksName})
			},
		},
		{
			name:   "an artifact file that already exists",
			source: "artifacts.go: WriteArtifact",
			want:   "configuration invalid: the artifact file is created: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, github.ArtifactFile), []byte("the file the step already had\n"), 0o600); err != nil {
					t.Fatalf("write %s: %v", filepath.Join(dir, github.ArtifactFile), err)
				}

				return github.WriteArtifact(dir, github.Artifact{Name: artifactChecksName})
			},
		},

		// gitleaks, the scanner every text passes before the first write.
		{
			name:   "a relative gitleaks path",
			source: "gitleaks.go: NewGitleaks",
			want:   "configuration invalid: the gitleaks path is absolute and clean: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewGitleaks(github.GitleaksConfig{Path: "bin/" + fakeGitleaksName, TempDir: t.TempDir()})

				return err
			},
		},
		{
			name:   "a gitleaks temporary directory that is not absolute",
			source: "gitleaks.go: NewGitleaks",
			want:   lineConfigTempDir,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewGitleaks(github.GitleaksConfig{Path: newFakeGitleaks(t).Path, TempDir: "tmp"})

				return err
			},
		},
		{
			name:   "a negative scan timeout",
			source: "gitleaks.go: NewGitleaks",
			want:   lineConfigTimeout,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewGitleaks(github.GitleaksConfig{
					Path:    newFakeGitleaks(t).Path,
					TempDir: t.TempDir(),
					Timeout: -time.Second,
				})

				return err
			},
		},
		{
			name:   "gitleaks' home directory that cannot be made",
			source: "gitleaks.go: NewGitleaks",
			want:   "configuration invalid: gitleaks' home directory is made: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewGitleaks(github.GitleaksConfig{Path: newFakeGitleaks(t).Path, TempDir: unwritableDir(t)})

				return err
			},
		},
		{
			name:   "gitleaks' home directory that cannot be removed",
			source: "gitleaks.go: Gitleaks.Close",
			want:   "configuration invalid: gitleaks' home directory is removed: see runbook",
			drive: func(t *testing.T) error {
				t.Helper()

				dir := t.TempDir()

				gitleaks, err := github.NewGitleaks(github.GitleaksConfig{Path: newFakeGitleaks(t).Path, TempDir: dir})
				if err != nil {
					t.Fatalf("NewGitleaks: %v", err)
				}

				closeAtEnd(t, gitleaks)

				makeUnwritable(t, dir)

				return gitleaks.Close()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.drive(t)
			if err == nil {
				t.Fatalf("%s refused nothing, want an error wrapping publish.ErrConfig", tc.source)
			}

			if !errors.Is(err, publish.ErrConfig) {
				t.Errorf("%s wraps %v, want it to wrap publish.ErrConfig: the configuration is exit 9", tc.source, err)
			}

			outcome := publish.OutcomeOf(err)

			if got := outcome.Summary(); got != tc.want {
				t.Errorf("%s: the summary line is %q, want %q", tc.source, got, tc.want)
			}

			if got := outcome.Code(); got != 9 {
				t.Errorf("%s: the exit code is %d, want 9 (%q)", tc.source, got, tc.want)
			}

			if strings.Contains(outcome.Summary(), valueMarker) {
				t.Errorf("%s: the summary %q holds a value of the test, want the check's own fixed words", tc.source, outcome.Summary())
			}
		})
	}
}

// valueMarker is text only an invented value of a test holds, so a test can
// show that no value reaches the run's line.
const valueMarker = "MARKER-VALUE-NEVER-PRINTED"

// artifactChecksName is the checks job's artifact, the name the artifact file
// fixtures carry.
const artifactChecksName = "checks-result"

// publishRun returns the run the fixture client's scope names.
func publishRun(t *testing.T) publish.Run {
	t.Helper()

	run, err := publish.NewRun(fixtureServerURL, fixtureRepository, fixtureRunID, fixtureDate)
	if err != nil {
		t.Fatalf("NewRun(%q, %q, %q, %q): %v", fixtureServerURL, fixtureRepository, fixtureRunID, fixtureDate, err)
	}

	return run
}

// newGitleaks returns a scanner over the fake gitleaks, closed at the end of the
// test.
func newGitleaks(t *testing.T) *github.Gitleaks {
	t.Helper()

	gitleaks, err := github.NewGitleaks(github.GitleaksConfig{Path: newFakeGitleaks(t).Path, TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewGitleaks: %v", err)
	}

	closeAtEnd(t, gitleaks)

	return gitleaks
}

// closeAtEnd closes what a refused Close leaves open, once the test's
// directories are writable again: a client or a scanner whose Close the test
// made fail still holds a directory, and the test's own temporary directory
// must go.
func closeAtEnd(t *testing.T, closer interface{ Close() error }) {
	t.Helper()

	t.Cleanup(func() { _ = closer.Close() })
}

// unwritableDir returns an existing directory the test cannot write to: the
// fresh directory a client or a scanner makes under it cannot be created
// there. The mode is restored before the test's temporary directory is
// removed, which is why this cleanup is registered after t.TempDir's.
func unwritableDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	makeUnwritable(t, dir)

	return dir
}

// unlistableDir returns an existing directory the test cannot list: its mode is
// write and search, never read.
func unlistableDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	if err := os.Chmod(dir, 0o300); err != nil { //nolint:gosec // G302: the test's own directory, made unlistable on purpose
		t.Fatalf("chmod %s 0300: %v", dir, err)
	}

	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: the test's own directory, its mode restored so the temporary directory can be removed
			t.Errorf("chmod %s 0700: %v", dir, err)
		}
	})

	return dir
}

// makeUnwritable removes the test's write permission from dir, and restores it
// before the test's temporary directory is removed.
func makeUnwritable(t *testing.T, dir string) {
	t.Helper()

	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // G302: the test's own directory, made unwritable on purpose
		t.Fatalf("chmod %s 0500: %v", dir, err)
	}

	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: the test's own directory, its mode restored so the temporary directory can be removed
			t.Errorf("chmod %s 0700: %v", dir, err)
		}
	})
}

package github_test

// The summary line names the refused check (issue #640), the GitHub side's
// half: every refusal prints "<class line>: <check phrase>: see runbook", the
// phrase being fixed English that names the check, never a value, a token, a
// key, a path, an ID or a number the step read. This file is that fixed list,
// one row per refusal, each driven by the real function that makes it, and
// each read back through the core's publish.OutcomeOf, which the entry point
// maps to the run's one line.
//
// Every class is here: the run's configuration (9), the result's content (3),
// a scan (4), the previous report (7), the duplicate drafts (6) and the private
// findings not filed (5), whose two lines are unchanged, and GitHub's own
// failure (8), whose phrase names the call by what it does, never by its URL,
// its answer, its status text or an ID read from an answer.

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

// The fixed lines of the GitHub side's refusals (issue #640): the one line
// each prints, "<class line>: <check phrase>: see runbook".
const (
	lineConfigToken     = "configuration invalid: GH_TOKEN unset or empty: see runbook"
	lineConfigClient    = "configuration invalid: no open client: see runbook"
	lineConfigScanner   = "configuration invalid: no open scanner: see runbook"
	lineConfigTempDir   = "configuration invalid: the temporary directory not absolute or no directory: see runbook"
	lineConfigTimeout   = "configuration invalid: a negative timeout: see runbook"
	lineConfigGHDir     = "configuration invalid: gh's configuration directory not made: see runbook"
	lineConfigGHDirGone = "configuration invalid: gh's configuration directory not removed: see runbook"
	lineConfigGHPath    = "configuration invalid: the gh path not absolute: see runbook"
	lineConfigGHFile    = "configuration invalid: the gh path names no file: see runbook"
	lineConfigScopeID   = "configuration invalid: GITHUB_RUN_ID not a decimal of up to 20 digits: see runbook"
	lineConfigCommit    = "configuration invalid: the scope's commit not 40 lowercase hex: see runbook"
	lineConfigPublish   = "configuration invalid: a publish scope without its commit: see runbook"
	lineConfigRun       = "configuration invalid: the run is not the client's: see runbook"
	lineConfigResult    = "configuration invalid: the result's path not absolute and clean: see runbook"
	lineConfigReports   = "configuration invalid: the reports directory not absolute and clean: see runbook"
	lineConfigDir       = "configuration invalid: the reports directory no directory: see runbook"
	lineConfigListable  = "configuration invalid: the reports directory not listable: see runbook"
	lineConfigGitleaks  = "configuration invalid: the gitleaks path not absolute and clean: see runbook"
	lineConfigScanHome  = "configuration invalid: gitleaks' home directory not made: see runbook"
	lineConfigScanGone  = "configuration invalid: gitleaks' home directory not removed: see runbook"
	lineConfigJob       = "configuration invalid: the job neither model nor publish: see runbook"
	lineConfigArtDir    = "configuration invalid: the artifact's directory not absolute: see runbook"
	lineConfigArtFile   = "configuration invalid: the artifact file not created: see runbook"

	lineResultFile         = "result refused: the result not one regular file under the cap: see runbook"
	lineResultTag          = "result refused: the release tag not vX.Y.Z: see runbook"
	lineResultNoRelease    = "result refused: no release of the tag: see runbook"
	lineResultNoLatest     = "result refused: no latest release: see runbook"
	lineResultNotLatest    = "result refused: the tag not the latest release's: see runbook"
	lineResultDigest       = "result refused: the image digest not sha256: and 64 lowercase hex: see runbook"
	lineResultNoAttesting  = "result refused: no attestation of the digest: see runbook"
	lineResultNoProvenance = "result refused: no provenance attestation names the digest: see runbook"

	lineScanReport   = "publish refused: the report: see runbook"
	lineScanIssue    = "publish refused: an issue: see runbook"
	lineScanAdvisory = "publish refused: an advisory: see runbook"
	lineScanFields   = "publish refused: the report's model fields: see runbook"

	linePreviousRegular = "previous report unreadable: the newest report not a regular file: see runbook"
	linePreviousCap     = "previous report unreadable: the newest report over the cap: see runbook"
	linePreviousRead    = "previous report unreadable: the newest report not readable: see runbook"

	lineDuplicates = "publish refused: duplicate draft advisories: see runbook step 11"
	lineNotFiled   = "private findings not filed: see runbook"

	lineGitHubIssues         = "GitHub call failed: listing the audit issues: see runbook"
	lineGitHubIssueCreate    = "GitHub call failed: creating the audit issue: see runbook"
	lineGitHubAdvisories     = "GitHub call failed: listing the audit advisories: see runbook"
	lineGitHubAdvisoryCreate = "GitHub call failed: creating an advisory: see runbook"
	lineGitHubAdvisoryUpdate = "GitHub call failed: updating an advisory: see runbook"
	lineGitHubTagRelease     = "GitHub call failed: reading the tag's release: see runbook"
	lineGitHubLatestRelease  = "GitHub call failed: reading the latest release: see runbook"
	lineGitHubAttestations   = "GitHub call failed: listing the image's attestations: see runbook"
	lineGitHubArtifacts      = "GitHub call failed: listing the run's artifacts: see runbook"
	lineGitHubCommitRuns     = "GitHub call failed: looking up the commit's runs: see runbook"
	lineGitHubReportBranch   = "GitHub call failed: reading the report's branch: see runbook"
	lineGitHubAuditedCommit  = "GitHub call failed: reading the audited commit: see runbook"
	lineGitHubBlob           = "GitHub call failed: writing the report's blob: see runbook"
	lineGitHubTree           = "GitHub call failed: writing the report's tree: see runbook"
	lineGitHubCommit         = "GitHub call failed: writing the report's commit: see runbook"
	lineGitHubCompare        = "GitHub call failed: comparing the audited commit with the report's: see runbook"
	lineGitHubRef            = "GitHub call failed: writing the report's branch: see runbook"
	lineGitHubPull           = "GitHub call failed: opening the report's pull request: see runbook"
	lineGitHubGH             = "GitHub call failed: running gh: see runbook"
)

// TestGitHubRefusalNamesTheFailedCheck pins the GitHub side's fixed list:
// every refusal's class, one line naming the check, and its exit code.
func TestGitHubRefusalNamesTheFailedCheck(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// source is the refusal's site in the GitHub side.
		source string
		// class is the error class the refusal wraps.
		class error
		// want is the one line the entry point prints, and code its exit code.
		want string
		code int
		// leaks are values the case plants in its input, which the line must
		// not hold.
		leaks []string
		// drive makes the refusal through the real function and returns its
		// error.
		drive func(t *testing.T) error
	}{
		// The client's configuration.
		{
			name:   "an empty token",
			source: "client.go: NewClient",
			class:  publish.ErrConfig,
			want:   lineConfigToken,
			code:   9,
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
			class:  publish.ErrConfig,
			want:   lineConfigGHPath,
			code:   9,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  "bin/" + valueMarker,
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
			class:  publish.ErrConfig,
			want:   lineConfigGHFile,
			code:   9,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  filepath.Join(t.TempDir(), valueMarker),
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
			class:  publish.ErrConfig,
			want:   lineConfigTempDir,
			code:   9,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  newFakeGH(t).Path,
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: valueMarker,
					Scope:   testScope(),
				})

				return err
			},
		},
		{
			name:   "a temporary directory that does not exist",
			source: "client.go: NewClient",
			class:  publish.ErrConfig,
			want:   lineConfigTempDir,
			code:   9,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewClient(github.ClientConfig{
					GHPath:  newFakeGH(t).Path,
					Token:   github.NewToken(fixtureToken(t)),
					TempDir: filepath.Join(t.TempDir(), valueMarker),
					Scope:   testScope(),
				})

				return err
			},
		},
		{
			name:   "a scope the core's NewRun refuses",
			source: "client.go: validateScope",
			class:  publish.ErrConfig,
			want:   lineConfigScopeID,
			code:   9,
			leaks:  []string{"12ab"},
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
			class:  publish.ErrConfig,
			want:   lineConfigCommit,
			code:   9,
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
			class:  publish.ErrConfig,
			want:   lineConfigPublish,
			code:   9,
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
			class:  publish.ErrConfig,
			want:   lineConfigTimeout,
			code:   9,
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
			class:  publish.ErrConfig,
			want:   lineConfigGHDir,
			code:   9,
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
			class:  publish.ErrConfig,
			want:   lineConfigGHDirGone,
			code:   9,
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
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				var client *github.Client

				_, err := client.Call(t.Context(), github.Request{Method: "GET", Endpoint: fixtureReleaseEndpoint})

				return err
			},
		},

		// The calls that hold no client at all: one phrase, "no open client",
		// one row per call.
		{
			name:   "a release check with no client",
			source: "release.go: CheckRelease",
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.CheckRelease(t.Context(), nil, fixtureTag)
			},
		},
		{
			name:   "an image check with no client",
			source: "release.go: CheckImageDigest",
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.CheckImageDigest(t.Context(), nil, fixtureTag, fixtureDigest)
			},
		},
		{
			name:   "an issue listing with no client",
			source: "issues.go: ListAuditIssues",
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.ListAuditIssues(t.Context(), nil)

				return err
			},
		},
		{
			name:   "an issue creation with no client",
			source: "issues.go: CreateIssue",
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.CreateIssue(t.Context(), nil, publish.IssueText{})
			},
		},
		{
			name:   "a commit-runs check with no client",
			source: "commitruns.go: CheckCommitRuns",
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.CheckCommitRuns(t.Context(), nil, nil)
			},
		},
		{
			name:   "an advisory listing with no client",
			source: "advisories.go: ListAuditAdvisories",
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.ListAuditAdvisories(t.Context(), nil)

				return err
			},
		},
		{
			name:   "advisory writes with no client",
			source: "advisories.go: FileAdvisories",
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.FileAdvisories(t.Context(), nil, github.AdvisoryPlan{})
			},
		},
		{
			name:   "a report publication with no client",
			source: "report.go: PublishReport",
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				return github.PublishReport(t.Context(), nil, github.ReportPlan{})
			},
		},
		{
			name:   "an artifact check with no client",
			source: "artifacts.go: CheckArtifacts",
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.CheckArtifacts(t.Context(), nil, github.JobModel)

				return err
			},
		},
		{
			name:   "a publication with no publish client",
			source: "publish.go: Publish",
			class:  publish.ErrConfig,
			want:   lineConfigClient,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.Publish(t.Context(), github.PublishConfig{})

				return err
			},
		},
		{
			name:   "a publication with no scanner",
			source: "publish.go: Publish",
			class:  publish.ErrConfig,
			want:   lineConfigScanner,
			code:   9,
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
			class:  publish.ErrConfig,
			want:   lineConfigRun,
			code:   9,
			leaks:  []string{"1987654321"},
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
			class:  publish.ErrConfig,
			want:   lineConfigResult,
			code:   9,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.LoadResult("result/"+valueMarker, fixtureCommit)

				return err
			},
		},
		{
			name:   "a previous report's directory that is not absolute and clean",
			source: "load.go: LoadPrevious",
			class:  publish.ErrConfig,
			want:   lineConfigReports,
			code:   9,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.LoadPrevious("reports/" + valueMarker)

				return err
			},
		},
		{
			name:   "a previous report's directory that does not exist",
			source: "load.go: LoadPrevious",
			class:  publish.ErrConfig,
			want:   lineConfigDir,
			code:   9,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.LoadPrevious(filepath.Join(t.TempDir(), valueMarker))

				return err
			},
		},
		{
			name:   "a previous report's directory that cannot be listed",
			source: "load.go: LoadPrevious",
			class:  publish.ErrConfig,
			want:   lineConfigListable,
			code:   9,
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
			class:  publish.ErrConfig,
			want:   lineConfigJob,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.CheckArtifacts(t.Context(), nil, github.Job(99))

				return err
			},
		},
		{
			name:   "an artifact file in a relative directory",
			source: "artifacts.go: WriteArtifact",
			class:  publish.ErrConfig,
			want:   lineConfigArtDir,
			code:   9,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				return github.WriteArtifact("tmp/"+valueMarker, github.Artifact{Name: artifactChecksName})
			},
		},
		{
			name:   "an artifact file that already exists",
			source: "artifacts.go: WriteArtifact",
			class:  publish.ErrConfig,
			want:   lineConfigArtFile,
			code:   9,
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
			class:  publish.ErrConfig,
			want:   lineConfigGitleaks,
			code:   9,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewGitleaks(github.GitleaksConfig{Path: "bin/" + valueMarker, TempDir: t.TempDir()})

				return err
			},
		},
		{
			name:   "a gitleaks temporary directory that is not absolute",
			source: "gitleaks.go: NewGitleaks",
			class:  publish.ErrConfig,
			want:   lineConfigTempDir,
			code:   9,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewGitleaks(github.GitleaksConfig{Path: newFakeGitleaks(t).Path, TempDir: valueMarker})

				return err
			},
		},
		{
			name:   "a negative scan timeout",
			source: "gitleaks.go: NewGitleaks",
			class:  publish.ErrConfig,
			want:   lineConfigTimeout,
			code:   9,
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
			class:  publish.ErrConfig,
			want:   lineConfigScanHome,
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.NewGitleaks(github.GitleaksConfig{Path: newFakeGitleaks(t).Path, TempDir: unwritableDir(t)})

				return err
			},
		},
		{
			name:   "gitleaks' home directory that cannot be removed",
			source: "gitleaks.go: Gitleaks.Close",
			class:  publish.ErrConfig,
			want:   lineConfigScanGone,
			code:   9,
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

		// The result's content: the result's class, exit 3. The core's own
		// list is its package's test; these are the checks of the side the
		// run's result names a release, an image or a file of the checkout.
		{
			name:   "a result that is not one regular file",
			source: "load.go: LoadResult",
			class:  publish.ErrResultInvalid,
			want:   lineResultFile,
			code:   3,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := github.LoadResult(filepath.Join(t.TempDir(), valueMarker), fixtureCommit)

				return err
			},
		},
		{
			name:   "a release tag that is no release tag",
			source: "release.go: CheckRelease",
			class:  publish.ErrResultInvalid,
			want:   lineResultTag,
			code:   3,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t).Path)

				return github.CheckRelease(t.Context(), client, "not-a-tag-"+valueMarker)
			},
		},
		{
			name:   "a tag that names no release",
			source: "release.go: CheckRelease",
			class:  publish.ErrResultInvalid,
			want:   lineResultNoRelease,
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "GET",
					Endpoint: fixtureReleaseEndpoint,
					Status:   404,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				return github.CheckRelease(t.Context(), client, fixtureTag)
			},
		},
		{
			name:   "a repository that holds no latest release",
			source: "release.go: CheckRelease",
			class:  publish.ErrResultInvalid,
			want:   lineResultNoLatest,
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t,
					Route{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": fixtureTag}},
					Route{Method: "GET", Endpoint: fixtureLatestReleaseEndpoint, Status: 404, Body: map[string]string{"message": valueMarker}},
				).Path)

				return github.CheckRelease(t.Context(), client, fixtureTag)
			},
		},
		{
			name:   "a tag that is not the latest release's",
			source: "release.go: CheckRelease",
			class:  publish.ErrResultInvalid,
			want:   lineResultNotLatest,
			code:   3,
			leaks:  []string{"v0.13.0"},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t,
					Route{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": fixtureTag}},
					Route{Method: "GET", Endpoint: fixtureLatestReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": "v0.13.0"}},
				).Path)

				return github.CheckRelease(t.Context(), client, fixtureTag)
			},
		},
		{
			name:   "a digest that is outside its pattern",
			source: "release.go: CheckImageDigest",
			class:  publish.ErrResultInvalid,
			want:   lineResultDigest,
			code:   3,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t).Path)

				return github.CheckImageDigest(t.Context(), client, fixtureTag, "sha512:"+valueMarker)
			},
		},
		{
			name:   "a digest GitHub attests nothing for",
			source: "release.go: CheckImageDigest",
			class:  publish.ErrResultInvalid,
			want:   lineResultNoAttesting,
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "GET",
					Endpoint: fixtureAttestationsEndpoint,
					Status:   404,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				return github.CheckImageDigest(t.Context(), client, fixtureTag, fixtureDigest)
			},
		},
		{
			name:   "a digest no provenance attestation names",
			source: "release.go: CheckImageDigest",
			class:  publish.ErrResultInvalid,
			want:   lineResultNoProvenance,
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "GET",
					Endpoint: fixtureAttestationsEndpoint,
					Status:   200,
					Body:     map[string]any{"attestations": []any{}},
				}).Path)

				return github.CheckImageDigest(t.Context(), client, fixtureTag, fixtureDigest)
			},
		},

		// The scan of the texts a run would publish: one phrase per kind of
		// text, never the text the scan refused.
		{
			name:   "a report that holds a token",
			source: "publish.go: scanAll",
			class:  publish.ErrScanRefused,
			want:   lineScanReport,
			code:   4,
			leaks:  []string{modelToken()},
			drive: func(t *testing.T) error {
				t.Helper()

				finding := fixtureFinding("G304", "low", "hardening")
				finding["scenario"] = "a scenario holding " + modelToken()

				_, err := runPublish(t, fixtureResultFileFrom(t, fixtureResultDocument(finding)), fixtureReportsDir(t))

				return err
			},
		},
		{
			name:   "an issue that holds a token",
			source: "publish.go: scanAll",
			class:  publish.ErrScanRefused,
			want:   lineScanIssue,
			code:   4,
			leaks:  []string{modelToken()},
			drive: func(t *testing.T) error {
				t.Helper()

				finding := fixtureFinding("G301", "low", "hardening")
				finding["scenario"] = "a scenario holding " + modelToken()

				dir := fixtureReportsDir(t)
				writeFixtureReport(t, dir, renderFixtureReport(t, fixtureResult(t, finding), publish.PrivateNone, nil, "2026-12-01", "1"))

				_, err := runPublish(t, fixtureResultFile(t), dir)

				return err
			},
		},
		{
			name:   "an advisory that holds a token",
			source: "publish.go: scanAll",
			class:  publish.ErrScanRefused,
			want:   lineScanAdvisory,
			code:   4,
			leaks:  []string{modelToken()},
			drive: func(t *testing.T) error {
				t.Helper()

				finding := privateFindingOf()
				finding["evidence"] = "a trace holding " + modelToken()

				fake := newFakeGH(t, publishRoutesWithAdvisories(nil, []Route{advisoryCreateRoute(201)})...)

				_, err := runPublishOver(t, fake, fixtureResultFile(t, finding), fixtureReportsDir(t))

				return err
			},
		},
		{
			name:   "a report whose model field holds a 40-hex run that is no commit",
			source: "publish.go: scanAll",
			class:  publish.ErrScanRefused,
			want:   lineScanFields,
			code:   4,
			leaks:  []string{fixtureOtherCommitSHA},
			drive: func(t *testing.T) error {
				t.Helper()

				document := fixtureResultDocument(fixtureFinding("G304", "low", "hardening"))
				document["changes"] = map[string]any{"dependencies": []any{}, "narrative": "a revert of " + fixtureOtherCommitSHA}

				routes := append([]Route{{
					Method:   "GET",
					Endpoint: commitRunEndpoint(fixtureOtherCommitSHA),
					Status:   404,
					Body:     map[string]string{"message": valueMarker},
				}}, publishRoutes()...)

				_, err := runPublishOver(t, newFakeGH(t, routes...), fixtureResultFileFrom(t, document), fixtureReportsDir(t))

				return err
			},
		},

		// The previous report: the newest report of the checkout's audits.
		{
			name:   "a newest report that is a symbolic link",
			source: "load.go: LoadPrevious",
			class:  publish.ErrPreviousReport,
			want:   linePreviousRegular,
			code:   7,
			leaks:  []string{"2026-11-01", valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				dir := fixtureReportsDir(t)
				target := filepath.Join(dir, valueMarker+".md")

				if err := os.WriteFile(target, []byte("a report\n"), 0o600); err != nil {
					t.Fatalf("write %s: %v", target, err)
				}

				if err := os.Symlink(target, filepath.Join(dir, reportNameOf("2026-11-01", "1"))); err != nil {
					t.Fatalf("link the report: %v", err)
				}

				_, err := github.LoadPrevious(dir)

				return err
			},
		},
		{
			name:   "a newest report over the cap",
			source: "load.go: LoadPrevious",
			class:  publish.ErrPreviousReport,
			want:   linePreviousCap,
			code:   7,
			leaks:  []string{"2026-11-01"},
			drive: func(t *testing.T) error {
				t.Helper()

				dir := fixtureReportsDir(t)
				path := filepath.Join(dir, reportNameOf("2026-11-01", "1"))

				if err := os.WriteFile(path, []byte(strings.Repeat("r", github.MaxPreviousReportBytes+1)), 0o600); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}

				_, err := github.LoadPrevious(dir)

				return err
			},
		},
		{
			name:   "a newest report that cannot be read",
			source: "load.go: LoadPrevious",
			class:  publish.ErrPreviousReport,
			want:   linePreviousRead,
			code:   7,
			leaks:  []string{"2026-11-01"},
			drive: func(t *testing.T) error {
				t.Helper()

				dir := fixtureReportsDir(t)
				path := filepath.Join(dir, reportNameOf("2026-11-01", "1"))

				if err := os.WriteFile(path, []byte("a report\n"), 0o600); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}

				makeUnreadable(t, path)

				_, err := github.LoadPrevious(dir)

				return err
			},
		},

		// The two lines issue #640 leaves as they are: two drafts of one
		// fingerprint already names its check, and a private finding GitHub
		// refuses to file is one check.
		{
			name:   "two drafts of one fingerprint",
			source: "advisories.go: PlanAdvisories",
			class:  publish.ErrDuplicateDrafts,
			want:   lineDuplicates,
			code:   6,
			leaks:  []string{fixtureGHSA, fixtureGHSA2},
			drive: func(t *testing.T) error {
				t.Helper()

				fp := fixtureFingerprint(t, privateFindingOf())

				existing := []github.Advisory{
					advisoryOf(fixtureGHSA, fp),
					advisoryOf(fixtureGHSA2, fp),
				}

				_, err := github.PlanAdvisories([]publish.PrivateFinding{{Finding: privateFinding(t), Fingerprint: fp}}, existing, fixtureRun(t))

				return err
			},
		},
		{
			name:   "a private finding GitHub refuses to file",
			source: "advisories.go: FileAdvisories",
			class:  publish.ErrPrivateNotFiled,
			want:   lineNotFiled,
			code:   5,
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "POST",
					Endpoint: fixtureRepoPrefix + "/security-advisories",
					Status:   403,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				return github.FileAdvisories(t.Context(), client, advisoryCreatePlan(t))
			},
		},

		// GitHub's own failure: the call by what it does, never its URL, its
		// answer or its status text.
		{
			name:   "a failed issue listing",
			source: "issues.go: ListAuditIssues",
			class:  publish.ErrGitHub,
			want:   lineGitHubIssues,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "GET",
					Endpoint: fixtureIssuesQuery + "page=1",
					Status:   500,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				_, err := github.ListAuditIssues(t.Context(), client)

				return err
			},
		},
		{
			name:   "a failed issue creation",
			source: "issues.go: CreateIssue",
			class:  publish.ErrGitHub,
			want:   lineGitHubIssueCreate,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "POST",
					Endpoint: fixtureIssuesEndpoint,
					Status:   500,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				return github.CreateIssue(t.Context(), client, publish.IssueText{
					Fingerprint: fixtureFingerprint(t, privateFindingOf()),
					Title:       "Security audit finding " + valueMarker,
					Body:        "the body of the issue",
				})
			},
		},
		{
			name:   "a failed advisory listing",
			source: "advisories.go: ListAuditAdvisories",
			class:  publish.ErrGitHub,
			want:   lineGitHubAdvisories,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "GET",
					Endpoint: fixtureAdvisoriesEndpoint,
					Status:   500,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				_, err := github.ListAuditAdvisories(t.Context(), client)

				return err
			},
		},
		{
			name:   "a failed advisory creation",
			source: "advisories.go: FileAdvisories",
			class:  publish.ErrGitHub,
			want:   lineGitHubAdvisoryCreate,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "POST",
					Endpoint: fixtureRepoPrefix + "/security-advisories",
					Status:   500,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				return github.FileAdvisories(t.Context(), client, advisoryCreatePlan(t))
			},
		},
		{
			name:   "a failed advisory update",
			source: "advisories.go: FileAdvisories",
			class:  publish.ErrGitHub,
			want:   lineGitHubAdvisoryUpdate,
			code:   8,
			leaks:  []string{valueMarker, fixtureGHSA},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "PATCH",
					Endpoint: advisoryPatchEndpoint(fixtureGHSA),
					Status:   500,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				return github.FileAdvisories(t.Context(), client, advisoryUpdatePlan(t))
			},
		},
		{
			name:   "a failed read of the tag's release",
			source: "release.go: CheckRelease",
			class:  publish.ErrGitHub,
			want:   lineGitHubTagRelease,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "GET",
					Endpoint: fixtureReleaseEndpoint,
					Status:   500,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				return github.CheckRelease(t.Context(), client, fixtureTag)
			},
		},
		{
			name:   "a failed read of the latest release",
			source: "release.go: CheckRelease",
			class:  publish.ErrGitHub,
			want:   lineGitHubLatestRelease,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t,
					Route{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": fixtureTag}},
					Route{Method: "GET", Endpoint: fixtureLatestReleaseEndpoint, Status: 500, Body: map[string]string{"message": valueMarker}},
				).Path)

				return github.CheckRelease(t.Context(), client, fixtureTag)
			},
		},
		{
			name:   "a failed attestation listing",
			source: "release.go: CheckImageDigest",
			class:  publish.ErrGitHub,
			want:   lineGitHubAttestations,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "GET",
					Endpoint: fixtureAttestationsEndpoint,
					Status:   500,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				return github.CheckImageDigest(t.Context(), client, fixtureTag, fixtureDigest)
			},
		},
		{
			name:   "a failed artifact listing",
			source: "artifacts.go: CheckArtifacts",
			class:  publish.ErrGitHub,
			want:   lineGitHubArtifacts,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newArtifactClient(t, newFakeGH(t, Route{
					Method:   "GET",
					Endpoint: fixtureArtifactsEndpoint,
					Status:   500,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				_, err := github.CheckArtifacts(t.Context(), client, github.JobModel)

				return err
			},
		},
		{
			name:   "a failed commit lookup",
			source: "commitruns.go: CheckCommitRuns",
			class:  publish.ErrGitHub,
			want:   lineGitHubCommitRuns,
			code:   8,
			leaks:  []string{valueMarker, fixtureOtherCommitSHA},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGH(t, Route{
					Method:   "GET",
					Endpoint: commitRunEndpoint(fixtureOtherCommitSHA),
					Status:   500,
					Body:     map[string]string{"message": valueMarker},
				}).Path)

				return github.CheckCommitRuns(t.Context(), client, []string{fixtureOtherCommitSHA})
			},
		},
		{
			name:   "a failed read of the report's branch",
			source: "report.go: PublishReport",
			class:  publish.ErrGitHub,
			want:   lineGitHubReportBranch,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				return reportRefusal(t, fixtureBranchEndpoint)
			},
		},
		{
			name:   "a failed read of the audited commit",
			source: "report.go: PublishReport",
			class:  publish.ErrGitHub,
			want:   lineGitHubAuditedCommit,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				return reportRefusal(t, fixtureCommitEndpoint)
			},
		},
		{
			name:   "a failed write of the report's blob",
			source: "report.go: PublishReport",
			class:  publish.ErrGitHub,
			want:   lineGitHubBlob,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				return reportRefusal(t, fixtureBlobsEndpoint)
			},
		},
		{
			name:   "a failed write of the report's tree",
			source: "report.go: PublishReport",
			class:  publish.ErrGitHub,
			want:   lineGitHubTree,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				return reportRefusal(t, fixtureTreesEndpoint)
			},
		},
		{
			name:   "a failed write of the report's commit",
			source: "report.go: PublishReport",
			class:  publish.ErrGitHub,
			want:   lineGitHubCommit,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				return reportRefusal(t, fixtureCommitsEndpoint)
			},
		},
		{
			name:   "a failed comparison of the audited commit with the report's",
			source: "report.go: PublishReport",
			class:  publish.ErrGitHub,
			want:   lineGitHubCompare,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				return reportRefusal(t, reportCompareEndpoint())
			},
		},
		{
			name:   "a failed write of the report's branch",
			source: "report.go: PublishReport",
			class:  publish.ErrGitHub,
			want:   lineGitHubRef,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				return reportRefusal(t, fixtureRefsEndpoint)
			},
		},
		{
			name:   "a failed opening of the report's pull request",
			source: "report.go: PublishReport",
			class:  publish.ErrGitHub,
			want:   lineGitHubPull,
			code:   8,
			leaks:  []string{valueMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				return reportRefusal(t, fixturePullsEndpoint)
			},
		},
		{
			name:   "a gh that answers no status line",
			source: "client.go: Client.Call",
			class:  publish.ErrGitHub,
			want:   lineGitHubGH,
			code:   8,
			leaks:  []string{fakeOutputMarker},
			drive: func(t *testing.T) error {
				t.Helper()

				client := newFixtureClient(t, newFakeGitleaksExit(t, 1).Path)

				_, err := client.Call(t.Context(), github.Request{Method: "GET", Endpoint: fixtureReleaseEndpoint})

				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.drive(t)
			if err == nil {
				t.Fatalf("%s refused nothing, want an error wrapping %v", tc.source, tc.class)
			}

			if !errors.Is(err, tc.class) {
				t.Errorf("%s wraps %v, want it to wrap %v: the class decides the exit code", tc.source, err, tc.class)
			}

			outcome := publish.OutcomeOf(err)

			if got := outcome.Summary(); got != tc.want {
				t.Errorf("%s: the summary line is %q, want %q", tc.source, got, tc.want)
			}

			if got := outcome.Code(); got != tc.code {
				t.Errorf("%s: the exit code is %d, want %d (%q)", tc.source, got, tc.code, tc.want)
			}

			for _, leak := range append([]string{valueMarker}, tc.leaks...) {
				if !strings.Contains(outcome.Summary(), leak) {
					continue
				}

				// valueMarker is the phrase's own exception: no line holds it
				// unless the case planted it in the phrase's words, which none
				// does.
				t.Errorf("%s: the summary %q holds the value %q, want the check's own fixed words", tc.source, outcome.Summary(), leak)
			}

			if !strings.Contains(outcome.Summary(), "see runbook") {
				t.Errorf("%s: the summary %q does not point at the runbook: every line does", tc.source, outcome.Summary())
			}
		})
	}
}

// reportRefusal drives PublishReport with the run's own plan and a 500 from
// one endpoint, valueMarker in its answer: a GitHub failure of that call.
func reportRefusal(t *testing.T, endpoint string) error {
	t.Helper()

	routes := make([]Route, 0, len(reportRoutes()))

	for _, r := range reportRoutes() {
		if r.Endpoint == endpoint {
			r = Route{Method: r.Method, Endpoint: endpoint, Status: 500, Body: map[string]string{"message": valueMarker}}
		}

		routes = append(routes, r)
	}

	client := newFixtureClient(t, newFakeGH(t, routes...).Path)

	return github.PublishReport(t.Context(), client, fixturePlan(t))
}

// reportCompareEndpoint is the compare call of the run's report: the audited
// commit against the commit the report's write answers with.
func reportCompareEndpoint() string {
	return fixtureRepoPrefix + "/compare/" + fixtureCommit + "..." + fixtureReportCommitSHA
}

// runPublish drives a whole publish run over the run's own answers: the result
// at resultPath, no previous report, and the fake gitleaks.
func runPublish(t *testing.T, resultPath, reportsDir string) (github.Published, error) {
	t.Helper()

	return runPublishOver(t, newFakeGH(t, publishRoutes()...), resultPath, reportsDir)
}

// runPublishOver drives a whole publish run over fake's answers.
func runPublishOver(t *testing.T, fake *FakeGH, resultPath, reportsDir string) (github.Published, error) {
	t.Helper()

	client := newFixtureClient(t, fake.Path)

	return github.Publish(t.Context(), publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), resultPath, reportsDir))
}

// advisoryOf returns the audit App's draft of fp with GHSA id ghsa.
func advisoryOf(ghsa string, fp publish.Fingerprint) github.Advisory {
	return github.Advisory{
		GHSAID:      ghsa,
		Summary:     advisorySummary(fp),
		Description: draftDescription("open"),
		State:       github.AdvisoryStateDraft,
		AuthorLogin: github.AuditBotLogin,
		AuthorType:  github.AuditBotType,
	}
}

// privateFinding returns the model's private finding, parsed as the result
// holds it.
func privateFinding(t *testing.T) publish.Finding {
	t.Helper()

	return fixtureResult(t, privateFindingOf()).Findings[0]
}

// advisoryCreatePlan returns the plan of a run whose one private finding has
// no draft yet: one create.
func advisoryCreatePlan(t *testing.T) github.AdvisoryPlan {
	t.Helper()

	fp := fixtureFingerprint(t, privateFindingOf())

	plan, err := github.PlanAdvisories([]publish.PrivateFinding{{Finding: privateFinding(t), Fingerprint: fp}}, nil, fixtureRun(t))
	if err != nil {
		t.Fatalf("PlanAdvisories of a new private finding: %v", err)
	}

	return plan
}

// advisoryUpdatePlan returns the plan of a run whose private finding has the
// audit App's draft already: one update.
func advisoryUpdatePlan(t *testing.T) github.AdvisoryPlan {
	t.Helper()

	fp := fixtureFingerprint(t, privateFindingOf())

	plan, err := github.PlanAdvisories([]publish.PrivateFinding{{Finding: privateFinding(t), Fingerprint: fp}}, []github.Advisory{advisoryOf(fixtureGHSA, fp)}, fixtureRun(t))
	if err != nil {
		t.Fatalf("PlanAdvisories of a filed private finding: %v", err)
	}

	return plan
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

// makeUnreadable removes every permission from the file at path, and restores
// its mode before the test's temporary directory is removed. The test runs as
// a user the mode binds, so the file cannot be read.
func makeUnreadable(t *testing.T, path string) {
	t.Helper()

	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod %s 0000: %v", path, err)
	}

	t.Cleanup(func() {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Errorf("chmod %s 0600: %v", path, err)
		}
	})
}

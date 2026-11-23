package github_test

// Group L of PR 4b: the publish run (PRD #175 milestone 2, "Report and
// triage"; ADR 0040). Every text is rendered and scanned before the first
// write: a scan that refuses means no write at all, and the run's writes are
// the report's then the issues'. A private finding is never filed and never
// dropped silently: the report states that it was not, and the run ends with
// outcome 5.

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// newFixtureGitleaks returns a scanner for the fake gitleaks.
func newFixtureGitleaks(t *testing.T, fake *FakeGitleaks) *github.Gitleaks {
	t.Helper()

	g, err := github.NewGitleaks(github.GitleaksConfig{Path: fake.Path, TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewGitleaks: %v", err)
	}

	t.Cleanup(func() {
		if err := g.Close(); err != nil {
			t.Errorf("Close(): %v", err)
		}
	})

	return g
}

// publishRoutes returns the answers of a whole publish run: the release, the
// issues' first page, the issue one is filed as and the report's writes.
func publishRoutes(issues ...map[string]any) []Route {
	list := make([]any, 0, len(issues))
	for _, issue := range issues {
		list = append(list, issue)
	}

	routes := []Route{
		{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": fixtureTag}},
		{Method: "GET", Endpoint: fixtureIssuesQuery + "page=1", Status: 200, Body: list},
		{Method: "POST", Endpoint: fixtureIssuesEndpoint, Status: 201, Body: map[string]any{"number": 1}},
	}

	return append(routes, reportRoutes()...)
}

// publishConfig returns the run's configuration: the result file, the reports
// directory, the fixture run, an invented key, and the client and scanner the
// caller built.
func publishConfig(t *testing.T, client *github.Client, g *github.Gitleaks, resultPath, reportsDir string) github.PublishConfig {
	t.Helper()

	return github.PublishConfig{
		ResultPath: resultPath,
		ReportsDir: reportsDir,
		Run:        fixtureRun(t),
		Key:        fixtureKey(t),
		Client:     client,
		Gitleaks:   g,
	}
}

// TestPublishReadsScansThenWrites pins a whole publish run without issues: its
// calls in one order - the reads, then the report's writes - and the run's URL
// in the commit message and the pull request's body.
func TestPublishReadsScansThenWrites(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t, publishRoutes()...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t), fixtureReportsDir(t))

	published, err := github.Publish(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Publish = %v, want no error", err)
	}

	if diff := cmp.Diff(github.Published{Public: 0, Private: publish.PrivateNone}, published); diff != "" {
		t.Errorf("the run's counts (-want +got):\n%s", diff)
	}

	calls := fake.Calls(t)

	want := []string{
		"GET " + fixtureReleaseEndpoint,
		"GET " + fixtureIssuesQuery + "page=1",
		"GET " + fixtureBranchEndpoint,
		"GET " + fixtureCommitEndpoint,
		"POST " + fixtureBlobsEndpoint,
		"POST " + fixtureTreesEndpoint,
		"POST " + fixtureCommitsEndpoint,
		"GET " + fixtureRepoPrefix + "/compare/" + fixtureCommit + "..." + fixtureReportCommitSHA,
		"POST " + fixtureRefsEndpoint,
		"POST " + fixturePullsEndpoint,
	}

	var got []string
	for _, c := range calls {
		got = append(got, endpointOf(c))

		if strings.Contains(c.Endpoint, "security-advisories") {
			t.Errorf("the run called %s, want no advisory endpoint: the advisories are PR 4c's", c.Endpoint)
		}
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the run's calls (-want +got):\n%s", diff)
	}

	runURL := fixtureRun(t).URL()

	commits := recordedBodies[recordedCommit](t, calls, "POST", fixtureCommitsEndpoint)
	if len(commits) != 1 || !strings.Contains(commits[0].Message, runURL) {
		t.Errorf("the commit's message %+v holds no run URL %q", commits, runURL)
	}

	pulls := recordedBodies[recordedPull](t, calls, "POST", fixturePullsEndpoint)
	if len(pulls) != 1 || !strings.Contains(pulls[0].Body, runURL) {
		t.Errorf("the pull request's body %+v holds no run URL %q", pulls, runURL)
	}
}

// TestPublishWritesNothingWhenAScanRefuses pins the fail-closed order: every
// text is scanned before the first write, so a gitleaks hit or a 40-hex run
// that is no commit of the repository leaves the repository untouched and its
// branch unfetched.
func TestPublishWritesNothingWhenAScanRefuses(t *testing.T) {
	t.Parallel()

	leak := fixtureFinding("G304", "low", "hardening")
	leak["scenario"] = "a scenario holding " + fakeGitleaksHit

	uncommitted := fixtureResultDocument(fixtureFinding("G304", "low", "hardening"))
	uncommitted["changes"] = map[string]any{"dependencies": []any{}, "narrative": "a revert of " + fixtureOtherCommitSHA}

	for _, tc := range []struct {
		name string
		// result is the model's result document, and extra the answers the scan
		// needs beside the run's own.
		result map[string]any
		extra  []Route
	}{
		{name: "a gitleaks hit", result: fixtureResultDocument(leak)},
		{
			name:   "a 40-hex run that is no commit",
			result: uncommitted,
			extra: []Route{{
				Method:   "GET",
				Endpoint: commitRunEndpoint(fixtureOtherCommitSHA),
				Status:   404,
				Body:     map[string]string{"message": "Not Found"},
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			routes := append(slices.Clone(tc.extra), publishRoutes()...)

			fake := newFakeGH(t, routes...)
			client := newFixtureClient(t, fake.Path)

			path := fixtureResultFileFrom(t, tc.result)
			cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), path, fixtureReportsDir(t))

			if _, err := github.Publish(t.Context(), cfg); !errors.Is(err, publish.ErrScanRefused) {
				t.Fatalf("Publish = %v, want an error wrapping publish.ErrScanRefused", err)
			}

			calls := fake.Calls(t)

			if writes := writesOf(calls); len(writes) != 0 {
				t.Errorf("the refused run wrote %v, want nothing: every scan runs before the first write", writes)
			}

			for _, c := range calls {
				if c.Endpoint == fixtureBranchEndpoint {
					t.Errorf("the refused run looked up the report's branch, want the writes' phase never to start")
				}
			}
		})
	}
}

// TestPublishFilesAPrivateFindingNowhere pins a run with a private finding: no
// advisory side exists yet, so the report says the private findings were not
// filed, the report and its issues are published all the same, and the run ends
// with outcome 5.
func TestPublishFilesAPrivateFindingNowhere(t *testing.T) {
	t.Parallel()

	// The previous report holds one public finding the run files an issue for.
	open := fixtureFinding("G301", "low", "hardening")
	previous := renderFixtureReport(t, fixtureResult(t, open), publish.PrivateNone, nil, "2026-12-01", "1")

	dir := fixtureReportsDir(t)
	writeFixtureReport(t, dir, previous)

	// The run's own finding is private: a confirmed high one of a private
	// category.
	fake := newFakeGH(t, publishRoutes()...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)),
		fixtureResultFile(t, fixtureFinding("G302", "high", "injection")), dir)

	published, err := github.Publish(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Publish = %v, want no error: the report is published whatever the private findings", err)
	}

	if published.Private != publish.PrivateNotFiled {
		t.Errorf("the run's private state is %v, want PrivateNotFiled", published.Private)
	}

	outcome := publish.OutcomeOf(nil).WithCounts(published.Public, published.Private)
	if outcome.Code() != 5 {
		t.Errorf("the run's outcome is %q, %d, want the not-filed outcome, exit 5", outcome.Summary(), outcome.Code())
	}

	calls := fake.Calls(t)

	blobs := recordedBodies[recordedBlob](t, calls, "POST", fixtureBlobsEndpoint)
	if len(blobs) != 1 {
		t.Fatalf("the run made %d blob(s), want 1: the report is published", len(blobs))
	}

	if !strings.Contains(blobs[0].Content, "private findings: not filed") {
		t.Errorf("the report holds no not-filed line: a private finding is never dropped silently")
	}

	var filed int

	for _, c := range calls {
		if c.Method == "POST" && c.Endpoint == fixtureIssuesEndpoint {
			filed++
		}
	}

	if filed != 1 {
		t.Errorf("the run filed %d issue(s), want 1: the previous report's public finding is still filed", filed)
	}

	for _, c := range calls {
		if strings.Contains(c.Endpoint, "security-advisories") {
			t.Errorf("the run called %s, want no advisory endpoint: the advisories are PR 4c's", c.Endpoint)
		}
	}
}

// TestPublishRefusesAReleaseThatIsNoRelease pins the release check's outcome: a
// tag that names no release refuses the result, and nothing is written.
func TestPublishRefusesAReleaseThatIsNoRelease(t *testing.T) {
	t.Parallel()

	routes := publishRoutes()
	routes[0] = Route{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 404, Body: map[string]string{"message": "Not Found"}}

	fake := newFakeGH(t, routes...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t), fixtureReportsDir(t))

	if _, err := github.Publish(t.Context(), cfg); !errors.Is(err, publish.ErrResultInvalid) {
		t.Fatalf("Publish with no release = %v, want an error wrapping publish.ErrResultInvalid", err)
	}

	if writes := writesOf(fake.Calls(t)); len(writes) != 0 {
		t.Errorf("the refused run wrote %v, want nothing", writes)
	}
}

// TestPublishStopsOnABrokenPreviousReport pins the reading that precedes every
// call: a newest report the core cannot read fails the run before gh runs.
func TestPublishStopsOnABrokenPreviousReport(t *testing.T) {
	t.Parallel()

	dir := fixtureReportsDir(t)
	if err := os.WriteFile(filepath.Join(dir, reportNameOf("2026-12-01", "1")), []byte("not a report\n"), 0o600); err != nil {
		t.Fatalf("write the broken report: %v", err)
	}

	fake := newFakeGH(t)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t), dir)

	if _, err := github.Publish(t.Context(), cfg); !errors.Is(err, publish.ErrPreviousReport) {
		t.Fatalf("Publish with a broken previous report = %v, want an error wrapping publish.ErrPreviousReport", err)
	}

	if calls := fake.Calls(t); len(calls) != 0 {
		t.Errorf("the run made %d call(s), want none: the reports are read before GitHub", len(calls))
	}
}

// TestPublishRefusesAConfigItCannotRun pins the run's own refusals: no client,
// no scanner, and a run that is not the client's.
func TestPublishRefusesAConfigItCannotRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// breakIt changes the configuration to one Publish must refuse.
		breakIt func(t *testing.T, cfg *github.PublishConfig)
	}{
		{name: "no client", breakIt: func(_ *testing.T, cfg *github.PublishConfig) { cfg.Client = nil }},
		{name: "no scanner", breakIt: func(_ *testing.T, cfg *github.PublishConfig) { cfg.Gitleaks = nil }},
		{
			name: "another run's repository",
			breakIt: func(t *testing.T, cfg *github.PublishConfig) {
				t.Helper()

				run, err := publish.NewRun(fixtureServerURL, "example/other", fixtureRunID, fixtureDate)
				if err != nil {
					t.Fatalf("NewRun: %v", err)
				}

				cfg.Run = run
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t)
			client := newFixtureClient(t, fake.Path)
			cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t), fixtureReportsDir(t))

			tc.breakIt(t, &cfg)

			if _, err := github.Publish(t.Context(), cfg); !errors.Is(err, publish.ErrConfig) {
				t.Fatalf("Publish = %v, want an error wrapping publish.ErrConfig", err)
			}

			if calls := fake.Calls(t); len(calls) != 0 {
				t.Errorf("the refused run made %d call(s), want none", len(calls))
			}
		})
	}
}

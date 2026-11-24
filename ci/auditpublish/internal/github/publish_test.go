package github_test

// Group L of PR 4b, extended by 4c: the publish run (PRD #175 milestone 2,
// "Report and triage"; ADR 0040). Every text is rendered and scanned before the
// first write: a scan that refuses means no write at all, and the run's writes
// are the advisories', then the report's, then the issues'. A private finding is
// filed as a draft advisory (4c's P1) and never dropped silently: a create
// GitHub refuses is the not-filed path, whose report states it and whose run
// ends with outcome 5 (P2, in publish_advisories_test.go).

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

// recordedIssue is the body of an issue write, as the fake gh recorded it.
type recordedIssue struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

// modelToken returns a token the core's scan refuses, invented here: the shape
// of a GitHub token, in a model field.
func modelToken() string { return "ghp_" + strings.Repeat("z9Y8x7W6", 6) }

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
// issues' first page, the issue one is filed as, the advisory listing and the
// report's writes.
func publishRoutes(issues ...map[string]any) []Route {
	return publishRoutesOverListing([]Route{advisoryListingRoute(nil, "")}, issues...)
}

// publishRoutesOverListing returns a whole publish run's answers with the
// advisory listing's routes the caller gives, so a test can give the run the
// advisories it reads (or a listing it refuses).
func publishRoutesOverListing(listing []Route, issues ...map[string]any) []Route {
	list := make([]any, 0, len(issues))
	for _, issue := range issues {
		list = append(list, issue)
	}

	routes := []Route{
		{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": fixtureTag}},
		{Method: "GET", Endpoint: fixtureIssuesQuery + "page=1", Status: 200, Body: list},
		{Method: "POST", Endpoint: fixtureIssuesEndpoint, Status: 201, Body: map[string]any{"number": 1}},
	}

	routes = append(routes, listing...)

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
		"GET " + fixtureAdvisoriesEndpoint,
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

	tokenInNarrative := fixtureResultDocument()
	tokenInNarrative["changes"] = map[string]any{"dependencies": []any{}, "narrative": "a narrative holding " + modelToken()}

	runBurst := fixtureResultDocument()
	runBurst["changes"] = map[string]any{
		"dependencies": []any{},
		"narrative":    "a revert of " + strings.Join(numberedRuns(github.MaxCommitRuns+1), ", "),
	}

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
		{name: "a token in a model field", result: tokenInNarrative},
		{name: "more 40-hex runs than the cap", result: runBurst},
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

// TestPublishFilesAPrivateFindingAsADraft pins 4c's P1, a run with a private
// finding: the run lists the repository's advisories and files one draft for
// the finding, then publishes the report saying the private findings were
// handled privately and the issues. Every advisory call runs before the
// report's first write, and the draft is the finding's own text. A create
// GitHub refuses is P2's not-filed path, pinned in publish_advisories_test.go.
func TestPublishFilesAPrivateFindingAsADraft(t *testing.T) {
	t.Parallel()

	// The previous report holds one public finding the run files an issue for.
	open := fixtureFinding("G301", "low", "hardening")
	previous := renderFixtureReport(t, fixtureResult(t, open), publish.PrivateNone, nil, "2026-12-01", "1")

	dir := fixtureReportsDir(t)
	writeFixtureReport(t, dir, previous)

	// The run's own finding is private: a confirmed high one of a private
	// category.
	finding := fixtureFinding("G302", "high", "injection")
	fp := fixtureFingerprint(t, finding)

	fake := newFakeGH(t, publishRoutesWithAdvisories(nil, []Route{advisoryCreateRoute(201)})...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t, finding), dir)

	published, err := github.Publish(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Publish = %v, want no error: the draft is filed", err)
	}

	if diff := cmp.Diff(github.Published{Public: 0, Private: publish.PrivateHandled}, published); diff != "" {
		t.Errorf("the run's counts (-want +got):\n%s", diff)
	}

	outcome := publish.OutcomeOf(nil).WithCounts(published.Public, published.Private)
	if want := "audit published: public findings 0, private findings: handled privately"; outcome.Summary() != want || outcome.Code() != 0 {
		t.Errorf("the run's outcome is %q, %d, want %q, 0", outcome.Summary(), outcome.Code(), want)
	}

	calls := fake.Calls(t)

	// The listing, then the draft's create, then the report's first write.
	listing, created, branch := -1, -1, len(calls)

	for i, c := range calls {
		switch {
		case c.Method == "GET" && c.Endpoint == fixtureAdvisoriesEndpoint:
			listing = i
		case c.Method == "POST" && c.Endpoint == fixtureRepoPrefix+"/security-advisories":
			created = i
		case c.Endpoint == fixtureBranchEndpoint && i < branch:
			branch = i
		}
	}

	switch {
	case listing < 0:
		t.Errorf("the run read no advisory listing, want the one page: %v", calls)
	case created < 0:
		t.Errorf("the run created no draft, want one: %v", calls)
	case listing >= created || created >= branch:
		t.Errorf("the advisory calls are at %d and %d, the report's first write at %d, want the listing, the draft and then the report", listing, created, branch)
	}

	drafts := recordedBodies[map[string]any](t, calls, "POST", fixtureRepoPrefix+"/security-advisories")
	if len(drafts) != 1 {
		t.Fatalf("the run created %d draft(s), want 1", len(drafts))
	}

	if summary, _ := drafts[0]["summary"].(string); summary != advisorySummary(fp) {
		t.Errorf("the draft's summary is %q, want %q", summary, advisorySummary(fp))
	}

	if description, _ := drafts[0]["description"].(string); !strings.Contains(description, "Audit status: new\nRun: "+fixtureRun(t).URL()) {
		t.Errorf("the draft's description does not end with the new status and the run's URL:\n%s", description)
	}

	blobs := recordedBodies[recordedBlob](t, calls, "POST", fixtureBlobsEndpoint)
	if len(blobs) != 1 {
		t.Fatalf("the run made %d blob(s), want 1: the report is published", len(blobs))
	}

	if !strings.Contains(blobs[0].Content, "private findings: handled privately") {
		t.Errorf("the report holds no handled-privately line:\n%s", blobs[0].Content)
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

// TestPublishScansThePreviousReportsIssuesBeforeWriting pins the texts a run
// inherits rather than renders: an issue's body holds a previous report's
// finding, so that finding is scanned - gitleaks, the core's tokens and the
// commit-run check - before the first write, and a hit leaves the repository
// untouched and its branch unfetched.
func TestPublishScansThePreviousReportsIssuesBeforeWriting(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// scenario is the previous finding's scenario: the text an issue would
		// carry into the repository unscanned.
		scenario string
		extra    []Route
	}{
		{name: "a gitleaks hit", scenario: "a scenario holding " + fakeGitleaksHit},
		{name: "a token", scenario: "a scenario holding " + modelToken()},
		{
			// A long, high-entropy run of the model's own characters: the
			// previous report's cell holds it, so the issue rendered from that
			// finding would carry it into the repository unscanned.
			name:     "an encoded run",
			scenario: "a scenario holding " + strings.Repeat("A1b2C3d4E5f6G7h8I9j0", 2),
		},
		{
			name:     "a 40-hex run that is no commit",
			scenario: "a revert of " + fixtureOtherCommitSHA,
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

			// The previous report holds one open public finding, which the run
			// files an issue for, whose text is the case's.
			finding := fixtureFinding("G301", "low", "hardening")
			finding["scenario"] = tc.scenario

			dir := fixtureReportsDir(t)
			previous := renderFixtureReport(t, fixtureResult(t, finding), publish.PrivateNone, nil, "2026-12-01", "1")
			writeFixtureReport(t, dir, previous)

			routes := append(slices.Clone(tc.extra), publishRoutes()...)

			fake := newFakeGH(t, routes...)
			client := newFixtureClient(t, fake.Path)
			cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t), dir)

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

// TestPublishScansEveryTextItWouldWrite pins the set of texts the scanner reads:
// the report's body, the pull request's title and body, the commit message and
// each issue's title and body, and nothing else. The texts are read back from
// the writes and from the scanner's record, so a text added to one side alone
// fails.
func TestPublishScansEveryTextItWouldWrite(t *testing.T) {
	t.Parallel()

	// A previous report of two public findings, neither of them filed: the run
	// plans two issues beside the report and its pull request.
	result := fixtureResult(t,
		fixtureFinding("G301", "low", "hardening"),
		fixtureFinding("G302", "informational", "hardening"),
	)

	dir := fixtureReportsDir(t)
	writeFixtureReport(t, dir, renderFixtureReport(t, result, publish.PrivateNone, nil, "2026-12-01", "1"))

	fake := newFakeGH(t, publishRoutes()...)
	client := newFixtureClient(t, fake.Path)
	scanner := newFakeGitleaks(t)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, scanner), fixtureResultFile(t), dir)

	if _, err := github.Publish(t.Context(), cfg); err != nil {
		t.Fatalf("Publish = %v, want no error", err)
	}

	calls := fake.Calls(t)

	blobs := recordedBodies[recordedBlob](t, calls, "POST", fixtureBlobsEndpoint)
	pulls := recordedBodies[recordedPull](t, calls, "POST", fixturePullsEndpoint)
	commits := recordedBodies[recordedCommit](t, calls, "POST", fixtureCommitsEndpoint)
	issues := recordedBodies[recordedIssue](t, calls, "POST", fixtureIssuesEndpoint)

	if len(blobs) != 1 || len(pulls) != 1 || len(commits) != 1 || len(issues) != 2 {
		t.Fatalf("the run wrote %d blob(s), %d pull(s), %d commit(s) and %d issue(s), want 1, 1, 1 and 2",
			len(blobs), len(pulls), len(commits), len(issues))
	}

	want := []string{blobs[0].Content, pulls[0].Title, pulls[0].Body, commits[0].Message}

	for _, issue := range issues {
		want = append(want, issue.Title, issue.Body)
	}

	var got []string

	for _, call := range scanner.Calls(t) {
		got = append(got, call.Stdin)
	}

	slices.Sort(want)
	slices.Sort(got)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the texts scanned (-want +got):\n%s", diff)
	}
}

// TestPublishStopsAtTheFirstRefusedIssue pins the issue writes' place: the
// report is published first, and an issue the API refuses is GitHub's failure
// with no later issue filed.
func TestPublishStopsAtTheFirstRefusedIssue(t *testing.T) {
	t.Parallel()

	result := fixtureResult(t,
		fixtureFinding("G301", "low", "hardening"),
		fixtureFinding("G302", "informational", "hardening"),
	)

	dir := fixtureReportsDir(t)
	writeFixtureReport(t, dir, renderFixtureReport(t, result, publish.PrivateNone, nil, "2026-12-01", "1"))

	routes := []Route{
		{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": fixtureTag}},
		{Method: "GET", Endpoint: fixtureIssuesQuery + "page=1", Status: 200, Body: []any{}},
		{Method: "POST", Endpoint: fixtureIssuesEndpoint, Status: 422, Body: map[string]string{"message": "the issue was refused"}},
		advisoryListingRoute(nil, ""),
	}
	routes = append(routes, reportRoutes()...)

	fake := newFakeGH(t, routes...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t), dir)

	if _, err := github.Publish(t.Context(), cfg); !errors.Is(err, publish.ErrGitHub) {
		t.Fatalf("Publish with a refused issue = %v, want an error wrapping publish.ErrGitHub", err)
	}

	calls := fake.Calls(t)

	var filed int

	for _, c := range calls {
		if c.Method == "POST" && c.Endpoint == fixtureIssuesEndpoint {
			filed++
		}
	}

	if filed != 1 {
		t.Errorf("the run filed %d issue(s), want exactly 1: the first refused issue stops the run", filed)
	}

	if writes := writesOf(calls); !slices.Contains(writes, fixturePullsEndpoint) {
		t.Errorf("the run's writes are %v, want the report's pull request among them: the report is published first", writes)
	}
}

// TestPublishFilesAnIssueOnlyForAFingerprintNoIssueCarries pins the listing's
// match on a whole run: an issue of the audit App's holding the fingerprint - in
// state open or closed - means no issue is filed for it, and a fingerprint no
// issue carries still gets its issue. The listing is read with state=all, so a
// closed issue of the App's is one filed already, not one to file anew.
func TestPublishFilesAnIssueOnlyForAFingerprintNoIssueCarries(t *testing.T) {
	t.Parallel()

	// The previous report holds two open public findings: the run files an issue
	// for each unless the listing carries the fingerprint.
	matched := fixtureFinding("G301", "low", "hardening")
	unmatched := fixtureFinding("G302", "low", "hardening")

	fp := func(f map[string]any) publish.Fingerprint {
		return publish.PlainFingerprint(f["rule"].(string), f["file"].(string), f["sink"].(string))
	}

	matchedTitle, unmatchedTitle := issueTitleOf(fp(matched)), issueTitleOf(fp(unmatched))

	dir := fixtureReportsDir(t)
	writeFixtureReport(t, dir, renderFixtureReport(t,
		fixtureResult(t, matched, unmatched), publish.PrivateNone, nil, "2026-12-01", "1"))

	for _, tc := range []struct {
		name string
		// issues is the audit App's listing the run reads.
		issues []map[string]any
		// want are the titles of the issues the run files, in its plan's order.
		want []string
	}{
		{name: "no issue at all", want: []string{matchedTitle, unmatchedTitle}},
		{
			name:   "the bot's open issue of the matched fingerprint",
			issues: []map[string]any{botEntryInState(matchedTitle, "open")},
			want:   []string{unmatchedTitle},
		},
		{
			name:   "the bot's closed issue of the matched fingerprint",
			issues: []map[string]any{botEntryInState(matchedTitle, "closed")},
			want:   []string{unmatchedTitle},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, publishRoutes(tc.issues...)...)
			client := newFixtureClient(t, fake.Path)
			cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t), dir)

			if _, err := github.Publish(t.Context(), cfg); err != nil {
				t.Fatalf("Publish = %v, want no error", err)
			}

			var got []string

			for _, issue := range recordedBodies[recordedIssue](t, fake.Calls(t), "POST", fixtureIssuesEndpoint) {
				got = append(got, issue.Title)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("the issues the run filed (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPublishFilesNoIssueForItsOwnFindings pins the report being opened: its own
// public findings are counted in the run's line and get no issue, since an issue
// is for a report already merged on main.
func TestPublishFilesNoIssueForItsOwnFindings(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t, publishRoutes()...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)),
		fixtureResultFile(t, fixtureFinding("G304", "low", "hardening")), fixtureReportsDir(t))

	published, err := github.Publish(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Publish = %v, want no error", err)
	}

	if published.Public != 1 {
		t.Errorf("the run's public count is %d, want 1: the report's own public total", published.Public)
	}

	outcome := publish.OutcomeOf(nil).WithCounts(published.Public, published.Private)
	if want := "audit published: public findings 1, private findings: none"; outcome.Summary() != want {
		t.Errorf("the run's line is %q, want %q", outcome.Summary(), want)
	}

	for _, c := range fake.Calls(t) {
		if c.Method == "POST" && c.Endpoint == fixtureIssuesEndpoint {
			t.Errorf("the run filed an issue for its own report's finding, want none: an issue is for a merged report")
		}
	}
}

// TestPublishRefusesAConfigItCannotRun pins the run's own refusals: no client,
// no scanner, and a run that is not the client's in its repository, its ID or
// its date.
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
		{
			name: "another run's ID",
			breakIt: func(t *testing.T, cfg *github.PublishConfig) {
				t.Helper()

				run, err := publish.NewRun(fixtureServerURL, fixtureRepository, "1234567891", fixtureDate)
				if err != nil {
					t.Fatalf("NewRun: %v", err)
				}

				cfg.Run = run
			},
		},
		{
			name: "another run's date",
			breakIt: func(t *testing.T, cfg *github.PublishConfig) {
				t.Helper()

				run, err := publish.NewRun(fixtureServerURL, fixtureRepository, fixtureRunID, "2026-12-04")
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

package github_test

// Group F of PR 4b: the report's publication (PRD #175 milestone 2, "Report
// and triage"; ADR 0040, "The publish program's GitHub side"). One commit on
// the run's own branch and one pull request, through the Git data API, in one
// order: every sha the API answers is the next call's value, the compare that
// proves the commit adds exactly the report runs before any ref points at it,
// an existing branch is a failed publish and no write follows a refused one. A
// write is never retried, a timed-out one included.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// The writes of a report publication, in their order: a refused one leaves
// every later one unmade.
var reportWrites = []string{
	fixtureBlobsEndpoint,
	fixtureTreesEndpoint,
	fixtureCommitsEndpoint,
	fixtureRefsEndpoint,
	fixturePullsEndpoint,
}

// recordedBlob, recordedTree, recordedCommit, recordedRef and recordedPull are
// the bodies of the report's writes, as the fake gh recorded them.
type recordedBlob struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type recordedTree struct {
	BaseTree string `json:"base_tree"`
	Tree     []struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		Sha  string `json:"sha"`
	} `json:"tree"`
}

type recordedCommit struct {
	Message string   `json:"message"`
	Tree    string   `json:"tree"`
	Parents []string `json:"parents"`
}

type recordedRef struct {
	Ref string `json:"ref"`
	Sha string `json:"sha"`
}

type recordedPull struct {
	Title               string `json:"title"`
	Body                string `json:"body"`
	Head                string `json:"head"`
	Base                string `json:"base"`
	MaintainerCanModify bool   `json:"maintainer_can_modify"`
	Draft               bool   `json:"draft"`
}

// fixturePlan returns the plan a report publication files: the report at the
// run's path and the run's templates.
func fixturePlan(t *testing.T) github.ReportPlan {
	t.Helper()

	return github.ReportPlan{
		Report:    publish.Rendered{Path: fixtureReportPath, Body: fixtureReportBody},
		Templates: fixtureTemplates(t, fixtureResult(t)),
	}
}

// auditCommitAnswer is the audited commit's answer: its tree is the base tree
// of the report's tree.
func auditCommitAnswer() map[string]any {
	return map[string]any{"sha": fixtureCommit, "tree": map[string]string{"sha": fixtureBaseTreeSHA}}
}

// compareAnswer is the compare endpoint's answer for the given files.
func compareAnswer(files ...map[string]string) map[string]any {
	list := make([]any, 0, len(files))
	for _, f := range files {
		list = append(list, f)
	}

	return map[string]any{"files": list}
}

// reportRoutes returns the happy path's answers: no branch yet, the audited
// commit's tree, the blob, the tree, the commit, the compare, the ref and the
// pull request.
func reportRoutes() []Route {
	return []Route{
		{Method: "GET", Endpoint: fixtureBranchEndpoint, Status: 404, Body: map[string]string{"message": "Not Found"}},
		{Method: "GET", Endpoint: fixtureCommitEndpoint, Status: 200, Body: auditCommitAnswer()},
		{Method: "POST", Endpoint: fixtureBlobsEndpoint, Status: 201, Body: map[string]string{"sha": fixtureBlobSHA}},
		{Method: "POST", Endpoint: fixtureTreesEndpoint, Status: 201, Body: map[string]string{"sha": fixtureAnswerTreeSHA}},
		{Method: "POST", Endpoint: fixtureCommitsEndpoint, Status: 201, Body: map[string]string{"sha": fixtureReportCommitSHA}},
		{
			Method:   "GET",
			Endpoint: fixtureRepoPrefix + "/compare/" + fixtureCommit + "..." + fixtureReportCommitSHA,
			Status:   200,
			Body:     compareAnswer(map[string]string{"filename": fixtureReportPath, "status": "added"}),
		},
		{
			Method:   "POST",
			Endpoint: fixtureRefsEndpoint,
			Status:   201,
			Body:     map[string]any{"ref": fixtureBranchRef, "object": map[string]string{"sha": fixtureReportCommitSHA}},
		},
		{Method: "POST", Endpoint: fixturePullsEndpoint, Status: 201, Body: map[string]any{"number": 1}},
	}
}

// newTimeoutClient returns a client for ghPath whose every call runs under
// timeout: the short deadline a route outlives.
func newTimeoutClient(t *testing.T, ghPath string, timeout time.Duration) *github.Client {
	t.Helper()

	return newTestClient(t, github.ClientConfig{
		GHPath:      ghPath,
		Token:       github.NewToken(fixtureToken(t)),
		TempDir:     t.TempDir(),
		Scope:       testScope(),
		CallTimeout: timeout,
	})
}

// endpointOf reads one recorded call as its method and endpoint, the shape the
// order comparison holds.
func endpointOf(c Call) string { return c.Method + " " + c.Endpoint }

// recordedBodies returns the bodies of every recorded call of a method and
// endpoint, decoded.
func recordedBodies[T any](t *testing.T, calls []Call, method, endpoint string) []T {
	t.Helper()

	var out []T

	for _, c := range calls {
		if c.Method != method || c.Endpoint != endpoint {
			continue
		}

		var v T
		if err := json.Unmarshal(c.Body, &v); err != nil {
			t.Fatalf("the recorded body of %s %s is no body: %v", method, endpoint, err)
		}

		out = append(out, v)
	}

	return out
}

// writesOf returns the endpoints of the recorded writes, in their order.
func writesOf(calls []Call) []string {
	var out []string

	for _, c := range calls {
		if c.Method == "POST" {
			out = append(out, c.Endpoint)
		}
	}

	return out
}

// TestPublishReportFilesTheReportAndItsPullRequest pins a whole publication:
// the calls, in one order, each next call carrying the sha the previous answer
// gave, and the compare that proves the commit adds exactly the report running
// before any ref points at it.
func TestPublishReportFilesTheReportAndItsPullRequest(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t, reportRoutes()...)
	client := newFixtureClient(t, fake.Path)
	plan := fixturePlan(t)

	if err := github.PublishReport(t.Context(), client, plan); err != nil {
		t.Fatalf("PublishReport = %v, want no error", err)
	}

	calls := fake.Calls(t)

	want := []string{
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
		t.Fatalf("the publication's calls (-want +got):\n%s", diff)
	}

	// The blob is the report's body, exactly.
	blobs := recordedBodies[recordedBlob](t, calls, "POST", fixtureBlobsEndpoint)
	if len(blobs) != 1 {
		t.Fatalf("the publication made %d blob(s), want 1", len(blobs))
	}

	if blobs[0].Content != plan.Report.Body || blobs[0].Encoding != "utf-8" {
		t.Errorf("the blob is %+v, want the report's body with encoding utf-8", blobs[0])
	}

	// The tree's base is the tree of the audited commit's answer, and its one
	// entry is the report at the run's path, holding the blob's sha.
	trees := recordedBodies[recordedTree](t, calls, "POST", fixtureTreesEndpoint)
	if len(trees) != 1 {
		t.Fatalf("the publication made %d tree(s), want 1", len(trees))
	}

	if trees[0].BaseTree != fixtureBaseTreeSHA {
		t.Errorf("the tree's base is %q, want the audited commit's tree %q", trees[0].BaseTree, fixtureBaseTreeSHA)
	}

	if len(trees[0].Tree) != 1 {
		t.Fatalf("the tree holds %d entries, want 1: %+v", len(trees[0].Tree), trees[0].Tree)
	}

	entry := trees[0].Tree[0]
	if entry.Path != fixtureReportPath || entry.Mode != "100644" || entry.Type != "blob" || entry.Sha != fixtureBlobSHA {
		t.Errorf("the tree's entry is %+v, want the report at %q, mode 100644, type blob, sha %q", entry, fixtureReportPath, fixtureBlobSHA)
	}

	// The commit's tree is the tree's answer, its one parent the audited
	// commit, its message the run's.
	commits := recordedBodies[recordedCommit](t, calls, "POST", fixtureCommitsEndpoint)
	if len(commits) != 1 {
		t.Fatalf("the publication made %d commit(s), want 1", len(commits))
	}

	if commits[0].Tree != fixtureAnswerTreeSHA {
		t.Errorf("the commit's tree is %q, want the tree's answer %q", commits[0].Tree, fixtureAnswerTreeSHA)
	}

	if diff := cmp.Diff([]string{fixtureCommit}, commits[0].Parents); diff != "" {
		t.Errorf("the commit's parents (-want +got):\n%s", diff)
	}

	if commits[0].Message != plan.Templates.CommitMessage {
		t.Errorf("the commit's message is %q, want the run's %q", commits[0].Message, plan.Templates.CommitMessage)
	}

	// The compare is of the audited commit and the commit just made, and it
	// runs before the ref that points at that commit.
	compareIndex, refIndex := -1, -1

	for i, c := range calls {
		switch endpointOf(c) {
		case "GET " + fixtureRepoPrefix + "/compare/" + fixtureCommit + "..." + fixtureReportCommitSHA:
			compareIndex = i
		case "POST " + fixtureRefsEndpoint:
			refIndex = i
		}
	}

	if compareIndex < 0 || refIndex < 0 || compareIndex > refIndex {
		t.Errorf("the compare is call %d and the ref call %d, want the compare before the ref: no workflow runs an unproven commit",
			compareIndex, refIndex)
	}

	// The ref is this run's branch, at the commit's answer.
	refs := recordedBodies[recordedRef](t, calls, "POST", fixtureRefsEndpoint)
	if len(refs) != 1 {
		t.Fatalf("the publication made %d ref(s), want 1", len(refs))
	}

	if refs[0].Ref != fixtureBranchRef {
		t.Errorf("the ref is %q, want %q", refs[0].Ref, fixtureBranchRef)
	}

	if refs[0].Sha != fixtureReportCommitSHA {
		t.Errorf("the ref's sha is %q, want the commit's answer %q", refs[0].Sha, fixtureReportCommitSHA)
	}

	// The pull request is this run's, into main, no draft and no write for its
	// author.
	pulls := recordedBodies[recordedPull](t, calls, "POST", fixturePullsEndpoint)
	if len(pulls) != 1 {
		t.Fatalf("the publication made %d pull request(s), want 1", len(pulls))
	}

	pull := pulls[0]
	if pull.Title != plan.Templates.PRTitle || pull.Body != plan.Templates.PRBody {
		t.Errorf("the pull request's texts are %q, %q, want the run's", pull.Title, pull.Body)
	}

	if pull.Head != fixtureBranch || pull.Base != "main" || pull.Draft || pull.MaintainerCanModify {
		t.Errorf("the pull request is %+v, want head %q, base main, no draft and no maintainer edit", pull, fixtureBranch)
	}
}

// TestPublishReportRefusesAnExistingBranch pins the report branch of a run
// that ran already: the lookup answers 200, which is a failed publish - never
// the GitHub failure's outcome - and nothing is written.
func TestPublishReportRefusesAnExistingBranch(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t, Route{
		Method:   "GET",
		Endpoint: fixtureBranchEndpoint,
		Status:   200,
		Body:     map[string]any{"ref": fixtureBranchRef, "object": map[string]string{"sha": fixtureCommit}},
	})
	client := newFixtureClient(t, fake.Path)

	err := github.PublishReport(t.Context(), client, fixturePlan(t))
	if !errors.Is(err, github.ErrBranchExists) {
		t.Fatalf("PublishReport with an existing branch = %v, want an error wrapping ErrBranchExists", err)
	}

	outcome := publish.OutcomeOf(err)
	if outcome.Code() != 1 || outcome.Summary() != "publish failed: see runbook" {
		t.Errorf("the outcome is %q, %d, want the failed publish's line and code 1", outcome.Summary(), outcome.Code())
	}

	if writes := writesOf(fake.Calls(t)); len(writes) != 0 {
		t.Errorf("the run wrote %v, want nothing: an existing branch is no write", writes)
	}
}

// TestPublishReportRefusesABranchLookupItCannotRead pins a branch lookup that
// is neither absent nor present: any other status is GitHub's failure, and
// nothing follows.
func TestPublishReportRefusesABranchLookupItCannotRead(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t, Route{
		Method:   "GET",
		Endpoint: fixtureBranchEndpoint,
		Status:   500,
		Body:     map[string]string{"message": "the branch lookup failed"},
	})
	client := newFixtureClient(t, fake.Path)

	if err := github.PublishReport(t.Context(), client, fixturePlan(t)); !errors.Is(err, publish.ErrGitHub) {
		t.Fatalf("PublishReport with a failed branch lookup = %v, want an error wrapping publish.ErrGitHub", err)
	}

	if writes := writesOf(fake.Calls(t)); len(writes) != 0 {
		t.Errorf("the run wrote %v, want nothing: a failed lookup is no write", writes)
	}
}

// TestPublishReportRefusesACommitThatIsNotTheReport pins the compare check: the
// report commit must add exactly one file, the run's report, or no ref and no
// pull request follow.
func TestPublishReportRefusesACommitThatIsNotTheReport(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		files []map[string]string
	}{
		{
			name: "the report and another file",
			files: []map[string]string{
				{"filename": fixtureReportPath, "status": "added"},
				{"filename": "docs/security/audits/other.md", "status": "added"},
			},
		},
		{name: "the report modified", files: []map[string]string{{"filename": fixtureReportPath, "status": "modified"}}},
		{name: "another file added", files: []map[string]string{{"filename": "docs/security/audits/other.md", "status": "added"}}},
		{name: "no file at all", files: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			routes := reportRoutes()
			for i, r := range routes {
				if r.Method == "GET" && strings.Contains(r.Endpoint, "/compare/") {
					routes[i].Body = compareAnswer(tc.files...)
				}
			}

			fake := newFakeGH(t, routes...)
			client := newFixtureClient(t, fake.Path)

			err := github.PublishReport(t.Context(), client, fixturePlan(t))
			if !errors.Is(err, github.ErrReportDiff) {
				t.Fatalf("PublishReport with a compare of %+v = %v, want an error wrapping ErrReportDiff", tc.files, err)
			}

			if code := publish.OutcomeOf(err).Code(); code != 1 {
				t.Errorf("the outcome is %d, want 1: a refused diff is a failed publish", code)
			}

			for _, c := range fake.Calls(t) {
				if c.Method == "POST" && (c.Endpoint == fixtureRefsEndpoint || c.Endpoint == fixturePullsEndpoint) {
					t.Errorf("the run made %s %s, want no ref and no pull request after a refused compare", c.Method, c.Endpoint)
				}
			}
		})
	}
}

// TestPublishReportStopsAtTheFirstRefusedWrite pins every write: a 422 through
// the write helper is GitHub's failure, and no later write is made.
func TestPublishReportStopsAtTheFirstRefusedWrite(t *testing.T) {
	t.Parallel()

	for i, endpoint := range reportWrites {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()

			routes := reportRoutes()
			for j, r := range routes {
				if r.Method == "POST" && r.Endpoint == endpoint {
					routes[j].Status = 422
					routes[j].Body = map[string]string{"message": "the write was refused"}
				}
			}

			fake := newFakeGH(t, routes...)
			client := newFixtureClient(t, fake.Path)

			err := github.PublishReport(t.Context(), client, fixturePlan(t))
			if !errors.Is(err, publish.ErrGitHub) {
				t.Fatalf("PublishReport with a 422 on %s = %v, want an error wrapping publish.ErrGitHub", endpoint, err)
			}

			want := reportWrites[:i+1]
			if diff := cmp.Diff(want, writesOf(fake.Calls(t))); diff != "" {
				t.Errorf("the writes made (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPublishReportNeverRetriesAWrite pins the write that outlives its call's
// deadline: it is one refused write, not a second attempt, and nothing follows
// it (ADR 0040, "The publish program's GitHub side").
func TestPublishReportNeverRetriesAWrite(t *testing.T) {
	t.Parallel()

	const (
		callTimeout = 100 * time.Millisecond
		routeDelay  = 10 * time.Second
	)

	routes := reportRoutes()
	for i, r := range routes {
		if r.Method == "POST" && r.Endpoint == fixtureBlobsEndpoint {
			routes[i].Delay = routeDelay
		}
	}

	fake := newFakeGH(t, routes...)
	client := newTimeoutClient(t, fake.Path, callTimeout)

	err := github.PublishReport(t.Context(), client, fixturePlan(t))
	if !errors.Is(err, publish.ErrGitHub) {
		t.Fatalf("PublishReport with a write past its deadline = %v, want an error wrapping publish.ErrGitHub", err)
	}

	want := []string{fixtureBlobsEndpoint}
	if diff := cmp.Diff(want, writesOf(fake.Calls(t))); diff != "" {
		t.Errorf("the writes made (-want +got):\n%s", diff)
	}
}

// TestPublishReportRefusesAnAnswerWithoutItsSha pins the binding's other side:
// an answer whose sha (or tree.sha) is missing or is no 40-hex value is
// GitHub's failure, and the next write is never made.
func TestPublishReportRefusesAnAnswerWithoutItsSha(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// breakIt changes the routes so that one answer holds no usable sha.
		breakIt func([]Route) []Route
	}{
		{
			name: "the audited commit's answer without its tree",
			breakIt: func(routes []Route) []Route {
				for i, r := range routes {
					if r.Method == "GET" && r.Endpoint == fixtureCommitEndpoint {
						routes[i].Body = map[string]any{"sha": fixtureCommit}
					}
				}

				return routes
			},
		},
		{
			name: "a blob answer with a sha that is no sha",
			breakIt: func(routes []Route) []Route {
				for i, r := range routes {
					if r.Method == "POST" && r.Endpoint == fixtureBlobsEndpoint {
						routes[i].Body = map[string]string{"sha": "not-a-sha"}
					}
				}

				return routes
			},
		},
		{
			name: "a tree answer without its sha",
			breakIt: func(routes []Route) []Route {
				for i, r := range routes {
					if r.Method == "POST" && r.Endpoint == fixtureTreesEndpoint {
						routes[i].Body = map[string]string{"url": "https://api.github.com/"}
					}
				}

				return routes
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, tc.breakIt(reportRoutes())...)
			client := newFixtureClient(t, fake.Path)

			if err := github.PublishReport(t.Context(), client, fixturePlan(t)); !errors.Is(err, publish.ErrGitHub) {
				t.Fatalf("PublishReport = %v, want an error wrapping publish.ErrGitHub", err)
			}

			for _, c := range fake.Calls(t) {
				if c.Method == "POST" && c.Endpoint == fixturePullsEndpoint {
					t.Errorf("the run opened a pull request after an answer without a usable sha")
				}
			}
		})
	}
}

// TestPublishReportKeepsTheAnswersOutOfItsErrors pins what no refusal prints:
// the response's text and the run's repository are in no error.
func TestPublishReportKeepsTheAnswersOutOfItsErrors(t *testing.T) {
	t.Parallel()

	routes := reportRoutes()
	for i, r := range routes {
		if r.Method == "POST" && r.Endpoint == fixturePullsEndpoint {
			routes[i].Status = 422
			routes[i].Body = map[string]string{"message": releaseMarker}
		}
	}

	fake := newFakeGH(t, routes...)
	client := newFixtureClient(t, fake.Path)

	err := github.PublishReport(t.Context(), client, fixturePlan(t))
	if err == nil {
		t.Fatal("PublishReport with a refused pull request = nil, want an error")
	}

	for _, hidden := range []string{releaseMarker, fixtureRepository, "https://", fixtureBranch, fakeOutputMarker} {
		if strings.Contains(err.Error(), hidden) {
			t.Errorf("the error names %q: %v", hidden, err)
		}
	}
}

// TestReportWritesCoverEveryWriteOfTheOrder pins the test's own list: the
// writes the refusals walk are the writes a publication makes, so a write the
// implementation adds without the tests is a case the order test fails.
func TestReportWritesCoverEveryWriteOfTheOrder(t *testing.T) {
	t.Parallel()

	var fromRoutes []string

	for _, r := range reportRoutes() {
		if r.Method == "POST" {
			fromRoutes = append(fromRoutes, r.Endpoint)
		}
	}

	if diff := cmp.Diff(fromRoutes, reportWrites); diff != "" {
		t.Errorf("the report's writes (-want +got):\n%s", diff)
	}
}

package github_test

// Group G of PR 4b: the issues of the previous report's merged public findings
// (PRD #175 milestone 2, "Report and triage"; ADR 0040). One security issue per
// fingerprint the audit App did not file already: the listing is paged and read
// whole or not at all, a match is the bot's own issue by login and type - open
// or closed, and never a pull request - and an outsider's issue carrying the
// fingerprint suppresses nothing.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// fixtureIssueBody and fixtureIssueTitle are the texts of a filed issue: the
// title holds the finding's fingerprint, the body its report's.
const fixtureIssueBody = "Security audit finding, filed by an earlier run.\n"

// issuePageLink is the Link header a page that has a next one carries.
func issuePageLink(page int) string {
	return `<https://api.github.com/repositories/1/issues?page=` + fmt.Sprint(page) + `>; rel="next"`
}

// issueEntry returns one entry of the issues' listing: the bot's, unless
// another login or type is given.
func issueEntry(title, login, kind string) map[string]any {
	return map[string]any{"title": title, "user": map[string]any{"login": login, "type": kind}}
}

// botEntry returns the audit App's issue of title.
func botEntry(title string) map[string]any {
	return issueEntry(title, github.AuditBotLogin, github.AuditBotType)
}

// issueTitleOf returns the title a run files for a fingerprint.
func issueTitleOf(fp publish.Fingerprint) string {
	return "Security audit finding " + string(fp) + ": hardening (low)"
}

// TestListAuditIssuesReadsEveryPageOrNone pins the listing: page 1 and on while
// the answer has a next page, every entry in order, and a listing that still
// has a next page after MaxIssuePages fails closed: it is never truncated.
func TestListAuditIssuesReadsEveryPageOrNone(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		routes []Route
		want   []github.Issue
		calls  int
		err    error
	}{
		{
			name:   "one page",
			routes: []Route{{Method: "GET", Endpoint: fixtureIssuesQuery + "page=1", Status: 200, Body: []any{botEntry("one"), botEntry("two")}}},
			want: []github.Issue{
				{Title: "one", UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
				{Title: "two", UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
			},
			calls: 1,
		},
		{
			name: "three pages",
			routes: []Route{
				{Method: "GET", Endpoint: fixtureIssuesQuery + "page=1", Status: 200, Body: []any{botEntry("one")}, Link: issuePageLink(2)},
				{Method: "GET", Endpoint: fixtureIssuesQuery + "page=2", Status: 200, Body: []any{botEntry("two")}, Link: issuePageLink(3)},
				{Method: "GET", Endpoint: fixtureIssuesQuery + "page=3", Status: 200, Body: []any{botEntry("three")}},
			},
			want: []github.Issue{
				{Title: "one", UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
				{Title: "two", UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
				{Title: "three", UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
			},
			calls: 3,
		},
		{
			name:   "an entry that is a pull request",
			routes: []Route{{Method: "GET", Endpoint: fixtureIssuesQuery + "page=1", Status: 200, Body: []any{map[string]any{"title": "a pull request", "user": map[string]any{"login": github.AuditBotLogin, "type": github.AuditBotType}, "pull_request": map[string]string{"url": "https://api.github.com/"}}}}},
			want:   []github.Issue{{Title: "a pull request", UserLogin: github.AuditBotLogin, UserType: github.AuditBotType, PullRequest: true}},
			calls:  1,
		},
		{
			name:   "a failed call",
			routes: []Route{{Method: "GET", Endpoint: fixtureIssuesQuery + "page=1", Status: 500, Body: map[string]string{"message": "the listing failed"}}},
			err:    publish.ErrGitHub,
			calls:  1,
		},
		{
			name:   "an answer that is no listing",
			routes: []Route{{Method: "GET", Endpoint: fixtureIssuesQuery + "page=1", Status: 200, Body: map[string]string{"message": "the listing failed"}}},
			err:    publish.ErrGitHub,
			calls:  1,
		},
		{
			name:   "an entry without its user",
			routes: []Route{{Method: "GET", Endpoint: fixtureIssuesQuery + "page=1", Status: 200, Body: []any{map[string]string{"title": "one"}}}},
			err:    publish.ErrGitHub,
			calls:  1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, tc.routes...)
			client := newFixtureClient(t, fake.Path)

			issues, err := github.ListAuditIssues(t.Context(), client)

			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("ListAuditIssues = %+v, %v, want an error wrapping %v", issues, err, tc.err)
				}
			} else if err != nil {
				t.Fatalf("ListAuditIssues = %v, want no error", err)
			}

			if diff := cmp.Diff(tc.want, issues); diff != "" {
				t.Errorf("the listing (-want +got):\n%s", diff)
			}

			if calls := fake.Calls(t); len(calls) != tc.calls {
				t.Errorf("the listing made %d call(s), want %d: %+v", len(calls), tc.calls, calls)
			}
		})
	}
}

// TestListAuditIssuesFailsClosedOverItsPages pins the page cap: a listing that
// still has a next page after the tenth is GitHub's failure, never a truncated
// list of the audit App's issues.
func TestListAuditIssuesFailsClosedOverItsPages(t *testing.T) {
	t.Parallel()

	if github.MaxIssuePages != 10 {
		t.Errorf("MaxIssuePages = %d, want 10", github.MaxIssuePages)
	}

	var routes []Route

	for page := 1; page <= github.MaxIssuePages; page++ {
		routes = append(routes, Route{
			Method:   "GET",
			Endpoint: fixtureIssuesQuery + "page=" + fmt.Sprint(page),
			Status:   200,
			Body:     []any{botEntry("an issue")},
			Link:     issuePageLink(page + 1),
		})
	}

	fake := newFakeGH(t, routes...)
	client := newFixtureClient(t, fake.Path)

	issues, err := github.ListAuditIssues(t.Context(), client)
	if !errors.Is(err, publish.ErrGitHub) {
		t.Fatalf("ListAuditIssues over its pages = %+v, %v, want an error wrapping publish.ErrGitHub", issues, err)
	}

	if issues != nil {
		t.Errorf("the listing is %+v, want nothing: a listing over the cap is never truncated", issues)
	}

	if calls := fake.Calls(t); len(calls) != github.MaxIssuePages {
		t.Errorf("the listing read %d page(s), want exactly %d", len(calls), github.MaxIssuePages)
	}
}

// issuePrevious returns the previous report PlanIssues reads, and the
// fingerprints of its three findings: two the run must file an issue for, and
// one its later report fixed.
func issuePrevious(t *testing.T) (previous *publish.Previous, open, fixed, informational publish.Fingerprint) {
	t.Helper()

	openFinding := fixtureFinding("G301", "low", "hardening")
	fixedFinding := fixtureFinding("G302", "low", "hardening")
	informationalFinding := fixtureFinding("G303", "informational", "configuration")
	suspected := unconfirmed(fixtureFinding("G304", "low", "hardening"))

	// The first report holds every finding the next one carries forward.
	first := fixturePrevious(t, fixtureResult(t, openFinding, fixedFinding, informationalFinding, suspected), publish.PrivateNone, nil, "2026-12-01", "1")

	// The second report carries the open and informational findings forward,
	// fixes the other, and keeps the suspected one under needs investigation.
	second := fixturePrevious(t, fixtureResult(t, openFinding, informationalFinding, suspected), publish.PrivateNone, first, "2026-12-02", "2")

	fp := func(f map[string]any) publish.Fingerprint {
		return publish.PlainFingerprint(f["rule"].(string), f["file"].(string), f["sink"].(string))
	}

	return second, fp(openFinding), fp(fixedFinding), fp(informationalFinding)
}

// TestPlanIssuesFilesOneIssuePerUnmatchedOpenFingerprint pins the plan: only
// the previous report's public findings that are new or open, in the table's
// order, and only those no issue of the audit App carries already.
func TestPlanIssuesFilesOneIssuePerUnmatchedOpenFingerprint(t *testing.T) {
	t.Parallel()

	previous, open, fixed, informational := issuePrevious(t)

	for _, tc := range []struct {
		name     string
		existing []github.Issue
		want     []publish.Fingerprint
	}{
		{name: "no issue at all", want: []publish.Fingerprint{open, informational}},
		{name: "the bot's issue of the open finding", existing: []github.Issue{
			{Title: issueTitleOf(open), UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
		}, want: []publish.Fingerprint{informational}},
		{name: "both of the bot's issues", existing: []github.Issue{
			{Title: issueTitleOf(open), UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
			{Title: issueTitleOf(informational), UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
		}, want: nil},
		{name: "the bot's issue of the fixed finding", existing: []github.Issue{
			{Title: issueTitleOf(fixed), UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
		}, want: []publish.Fingerprint{open, informational}},
		{name: "an outsider's issue of the open finding", existing: []github.Issue{
			{Title: issueTitleOf(open), UserLogin: "mallory", UserType: "User"},
		}, want: []publish.Fingerprint{open, informational}},
		{name: "another login of the Bot type", existing: []github.Issue{
			{Title: issueTitleOf(open), UserLogin: "another-app[bot]", UserType: github.AuditBotType},
		}, want: []publish.Fingerprint{open, informational}},
		{name: "the bot's login of another type", existing: []github.Issue{
			{Title: issueTitleOf(open), UserLogin: github.AuditBotLogin, UserType: "User"},
		}, want: []publish.Fingerprint{open, informational}},
		{name: "the bot's pull request", existing: []github.Issue{
			{Title: issueTitleOf(open), UserLogin: github.AuditBotLogin, UserType: github.AuditBotType, PullRequest: true},
		}, want: []publish.Fingerprint{open, informational}},
		{name: "a title with more hex after the fingerprint", existing: []github.Issue{
			{Title: string(open) + "0", UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
		}, want: []publish.Fingerprint{open, informational}},
		{name: "a title with a letter before the fingerprint", existing: []github.Issue{
			{Title: "x" + string(open), UserLogin: github.AuditBotLogin, UserType: github.AuditBotType},
		}, want: []publish.Fingerprint{open, informational}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			issues, err := github.PlanIssues(previous, tc.existing, fixtureRun(t))
			if err != nil {
				t.Fatalf("PlanIssues = %v, want no error", err)
			}

			var got []publish.Fingerprint

			for _, issue := range issues {
				got = append(got, issue.Fingerprint)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("the planned issues (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPlanIssuesRendersEachIssue pins the plan's texts: one issue per
// fingerprint, its title a code-free sentence naming it, and no evidence of
// any finding in its body.
func TestPlanIssuesRendersEachIssue(t *testing.T) {
	t.Parallel()

	previous, open, _, informational := issuePrevious(t)

	issues, err := github.PlanIssues(previous, nil, fixtureRun(t))
	if err != nil {
		t.Fatalf("PlanIssues = %v, want no error", err)
	}

	want := []publish.Fingerprint{open, informational}

	var got []publish.Fingerprint

	for _, issue := range issues {
		got = append(got, issue.Fingerprint)

		if issue.Title == "" || issue.Body == "" {
			t.Errorf("the issue of %s is %+v, want a title and a body", issue.Fingerprint, issue)
		}

		if !strings.Contains(issue.Title, string(issue.Fingerprint)) {
			t.Errorf("the issue's title %q holds no fingerprint %q", issue.Title, issue.Fingerprint)
		}
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the planned issues (-want +got):\n%s", diff)
	}
}

// TestPlanIssuesWithoutAPreviousReportFilesNothing pins the first report: no
// previous report is no finding to file an issue for.
func TestPlanIssuesWithoutAPreviousReportFilesNothing(t *testing.T) {
	t.Parallel()

	issues, err := github.PlanIssues(nil, nil, fixtureRun(t))
	if err != nil {
		t.Fatalf("PlanIssues(nil) = %v, want no error", err)
	}

	if issues != nil {
		t.Errorf("PlanIssues(nil) = %+v, want nothing: a first report files no issue", issues)
	}
}

// TestCreateIssueFilesExactlyTitleBodyAndLabel pins the one write of an issue:
// exactly {title, body, labels: ["security"]}, and a refused one is GitHub's
// failure.
func TestCreateIssueFilesExactlyTitleBodyAndLabel(t *testing.T) {
	t.Parallel()

	issue := publish.IssueText{
		Fingerprint: "fp-0123456789ab",
		Title:       "Security audit finding fp-0123456789ab: hardening (low)",
		Body:        fixtureIssueBody,
	}

	t.Run("an accepted issue", func(t *testing.T) {
		t.Parallel()

		fake := newFakeGH(t, Route{Method: "POST", Endpoint: fixtureIssuesEndpoint, Status: 201, Body: map[string]any{"number": 1}})
		client := newFixtureClient(t, fake.Path)

		if err := github.CreateIssue(t.Context(), client, issue); err != nil {
			t.Fatalf("CreateIssue = %v, want no error", err)
		}

		calls := fake.Calls(t)
		if len(calls) != 1 || calls[0].Method != "POST" || calls[0].Endpoint != fixtureIssuesEndpoint {
			t.Fatalf("CreateIssue made %+v, want one POST to %s", calls, fixtureIssuesEndpoint)
		}

		var body map[string]any
		if err := json.Unmarshal(calls[0].Body, &body); err != nil {
			t.Fatalf("the recorded body is no body: %v", err)
		}

		want := map[string]any{"title": issue.Title, "body": issue.Body, "labels": []any{"security"}}
		if diff := cmp.Diff(want, body); diff != "" {
			t.Errorf("the issue's body (-want +got):\n%s", diff)
		}
	})

	t.Run("a refused issue", func(t *testing.T) {
		t.Parallel()

		fake := newFakeGH(t, Route{Method: "POST", Endpoint: fixtureIssuesEndpoint, Status: 422, Body: map[string]string{"message": "the issue was refused"}})
		client := newFixtureClient(t, fake.Path)

		if err := github.CreateIssue(t.Context(), client, issue); !errors.Is(err, publish.ErrGitHub) {
			t.Fatalf("CreateIssue of a refused write = %v, want an error wrapping publish.ErrGitHub", err)
		}

		if calls := fake.Calls(t); len(calls) != 1 {
			t.Errorf("the refused issue made %d call(s), want exactly 1: a write is never retried", len(calls))
		}
	})
}

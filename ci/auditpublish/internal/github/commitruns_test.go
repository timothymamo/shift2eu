package github_test

// Group H of PR 4b: the commit-run check (PRD #175 milestone 2, ADR 0040's
// residual risk of a hex-encoded secret). Every distinct maximal 40-hex run of
// the public model fields must be a commit of the repository: a run that is no
// commit, and more runs than the cap, refuse the publication before any write.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// commitRunEndpoint is the lookup of one 40-hex run.
func commitRunEndpoint(sha string) string { return fixtureRepoPrefix + "/git/commits/" + sha }

// commitAnswers returns one 200 answer per run, recording it as a commit.
func commitAnswers(runs ...string) []Route {
	routes := make([]Route, 0, len(runs))

	for _, run := range runs {
		routes = append(routes, Route{Method: "GET", Endpoint: commitRunEndpoint(run), Status: 200, Body: map[string]string{"sha": run}})
	}

	return routes
}

// numberedRuns returns n distinct 40-hex runs.
func numberedRuns(n int) []string {
	out := make([]string, 0, n)

	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("%040x", i))
	}

	return out
}

// TestCheckCommitRunsLooksUpEveryDistinctRunOnce pins the check's reading: the
// maximal 40-hex runs of the fields, either case, in lower case and once each,
// and only those: a 39- or 41-hex run is no run.
func TestCheckCommitRunsLooksUpEveryDistinctRunOnce(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		fields []string
		want   []string
	}{
		{
			name:   "one run",
			fields: []string{"a commit " + fixtureOtherCommitSHA + " of the history"},
			want:   []string{fixtureOtherCommitSHA},
		},
		{
			name:   "one run twice and another",
			fields: []string{fixtureOtherCommitSHA, "and " + fixtureOtherCommitSHA, fixtureCommit},
			want:   []string{fixtureOtherCommitSHA, fixtureCommit},
		},
		{
			name:   "a run in upper case",
			fields: []string{strings.ToUpper(fixtureOtherCommitSHA)},
			want:   []string{fixtureOtherCommitSHA},
		},
		{
			name:   "a 39-hex and a 41-hex run",
			fields: []string{fixtureOtherCommitSHA[:39], fixtureOtherCommitSHA + "a"},
			want:   nil,
		},
		{
			name:   "no hex run at all",
			fields: []string{"the report's text, with no run in it", ""},
			want:   nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, commitAnswers(tc.want...)...)
			client := newFixtureClient(t, fake.Path)

			if err := github.CheckCommitRuns(t.Context(), client, tc.fields); err != nil {
				t.Fatalf("CheckCommitRuns(%v) = %v, want no error", tc.fields, err)
			}

			var got []string

			for _, c := range fake.Calls(t) {
				got = append(got, strings.TrimPrefix(c.Endpoint, fixtureRepoPrefix+"/git/commits/"))
			}

			want := tc.want
			if want == nil {
				want = []string{}
			}

			if got == nil {
				got = []string{}
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("the runs looked up (-want +got):\n%s", diff)
			}
		})
	}
}

// TestCheckCommitRunsRefusesARunThatIsNoCommit pins the check's verdict: a run
// that is no commit of the repository refuses the publication - a 404, and a
// 200 whose answer names another commit than the one looked up, names none or
// is no commit at all - and the refusal names no run.
func TestCheckCommitRunsRefusesARunThatIsNoCommit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// answer is the lookup's answer: one that is no commit of the run.
		answer Route
	}{
		{
			name: "a 404",
			answer: Route{
				Method:   "GET",
				Endpoint: commitRunEndpoint(fixtureOtherCommitSHA),
				Status:   404,
				Body:     map[string]string{"message": "Not Found"},
			},
		},
		{
			name: "an answer without a sha",
			answer: Route{
				Method:   "GET",
				Endpoint: commitRunEndpoint(fixtureOtherCommitSHA),
				Status:   200,
				Body:     map[string]string{"message": "no sha"},
			},
		},
		{
			name: "an answer that is no commit",
			answer: Route{
				Method:   "GET",
				Endpoint: commitRunEndpoint(fixtureOtherCommitSHA),
				Status:   200,
				Body:     "no commit",
			},
		},
		{
			name: "another commit's answer",
			answer: Route{
				Method:   "GET",
				Endpoint: commitRunEndpoint(fixtureOtherCommitSHA),
				Status:   200,
				Body:     map[string]string{"sha": fixtureSHA},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, tc.answer)
			client := newFixtureClient(t, fake.Path)

			err := github.CheckCommitRuns(t.Context(), client, []string{"a commit " + fixtureOtherCommitSHA})
			if !errors.Is(err, publish.ErrScanRefused) {
				t.Fatalf("CheckCommitRuns of a run that is no commit = %v, want an error wrapping publish.ErrScanRefused", err)
			}

			if strings.Contains(err.Error(), fixtureOtherCommitSHA) {
				t.Errorf("the error names the run: %v", err)
			}
		})
	}
}

// TestCheckCommitRunsRefusesAFailedLookup pins the other answer: a failed call
// is GitHub's failure, never the scan's refusal.
func TestCheckCommitRunsRefusesAFailedLookup(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t, Route{
		Method:   "GET",
		Endpoint: commitRunEndpoint(fixtureOtherCommitSHA),
		Status:   500,
		Body:     map[string]string{"message": "the lookup failed"},
	})
	client := newFixtureClient(t, fake.Path)

	if err := github.CheckCommitRuns(t.Context(), client, []string{fixtureOtherCommitSHA}); !errors.Is(err, publish.ErrGitHub) {
		t.Fatalf("CheckCommitRuns with a failed lookup = %v, want an error wrapping publish.ErrGitHub", err)
	}
}

// TestCheckCommitRunsCapsTheRunsItLooksUp pins the cap: MaxCommitRuns distinct
// runs pass, one more refuses the publication before any call.
func TestCheckCommitRunsCapsTheRunsItLooksUp(t *testing.T) {
	t.Parallel()

	if github.MaxCommitRuns != 20 {
		t.Errorf("MaxCommitRuns = %d, want 20", github.MaxCommitRuns)
	}

	t.Run("the cap's runs", func(t *testing.T) {
		t.Parallel()

		runs := numberedRuns(github.MaxCommitRuns)

		fake := newFakeGH(t, commitAnswers(runs...)...)
		client := newFixtureClient(t, fake.Path)

		if err := github.CheckCommitRuns(t.Context(), client, runs); err != nil {
			t.Fatalf("CheckCommitRuns of %d runs = %v, want no error", len(runs), err)
		}

		if calls := fake.Calls(t); len(calls) != len(runs) {
			t.Errorf("the check made %d call(s), want %d", len(calls), len(runs))
		}
	})

	t.Run("one run over the cap", func(t *testing.T) {
		t.Parallel()

		runs := numberedRuns(github.MaxCommitRuns + 1)

		fake := newFakeGH(t)
		client := newFixtureClient(t, fake.Path)

		if err := github.CheckCommitRuns(t.Context(), client, runs); !errors.Is(err, publish.ErrScanRefused) {
			t.Fatalf("CheckCommitRuns of %d runs = %v, want an error wrapping publish.ErrScanRefused", len(runs), err)
		}

		if calls := fake.Calls(t); len(calls) != 0 {
			t.Errorf("the refused check made %d call(s), want none: the cap is refused before any lookup", len(calls))
		}
	})
}

package github_test

// Group C of PR 4a: the artifact check (PRD #175 milestone 2, "Each job that
// downloads an artifact downloads it by exact name"; ADR 0040, "The publish
// program's GitHub side"). One listing of the run's artifacts decides whether a
// job may download: exactly the job's names, no name twice, nothing expired and
// no second page. GitHub's failure is the core's GitHub class, and the check's
// one summary line and exit code come from the error's class alone.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// artifactsResponseMarker is text only the API's answer holds, so a test can
// show that none of it reaches an error.
const artifactsResponseMarker = "RESPONSE-TEXT-MARKER"

// artifactEntry is one entry of the run's artifact listing.
type artifactEntry struct {
	name    string
	expired bool
}

// artifactsRoute is the artifact check's answer: the entries GitHub lists and
// the total it counts, which differ when a second page exists.
func artifactsRoute(total int, entries ...artifactEntry) Route {
	artifacts := make([]map[string]any, 0, len(entries))

	for _, e := range entries {
		artifacts = append(artifacts, map[string]any{"name": e.name, "expired": e.expired})
	}

	return Route{
		Method:   "GET",
		Endpoint: fixtureArtifactsEndpoint,
		Status:   200,
		Body:     map[string]any{"total_count": total, "artifacts": artifacts},
	}
}

// artifactCase is one answer to the check and the verdict it must draw.
type artifactCase struct {
	name  string
	route Route
	// want is the class the error must wrap; nil when the check accepts.
	want error
}

// artifactCases is the check's answers for one job's expected names: the right
// list, and each way a list is wrong.
func artifactCases(expected []artifactEntry) []artifactCase {
	extra := append(append([]artifactEntry{}, expected...), artifactEntry{name: "unexpected-artifact"})
	duplicate := append(append([]artifactEntry{}, expected...), expected[0])
	expired := append([]artifactEntry{}, expected...)
	expired[0].expired = true
	missing := expected[1:]

	return []artifactCase{
		{name: "exactly the job's artifacts", route: artifactsRoute(len(expected), expected...), want: nil},
		{name: "an extra artifact", route: artifactsRoute(len(extra), extra...), want: github.ErrArtifacts},
		{name: "a name twice", route: artifactsRoute(len(duplicate), duplicate...), want: github.ErrArtifacts},
		{name: "an expected artifact missing", route: artifactsRoute(len(missing), missing...), want: github.ErrArtifacts},
		{name: "a second page", route: artifactsRoute(len(expected)+1, expected...), want: github.ErrArtifacts},
		{name: "an expired artifact", route: artifactsRoute(len(expected), expired...), want: github.ErrArtifacts},
		{
			name:  "a non-2xx answer",
			route: Route{Method: "GET", Endpoint: fixtureArtifactsEndpoint, Status: 403, Body: map[string]string{"message": artifactsResponseMarker}},
			want:  publish.ErrGitHub,
		},
		{
			name:  "an answer that is not the artifact listing",
			route: Route{Method: "GET", Endpoint: fixtureArtifactsEndpoint, Status: 200, Body: map[string]string{"message": artifactsResponseMarker}},
			want:  publish.ErrGitHub,
		},
	}
}

// TestCheckArtifactsRefusesAnyListThatIsNotExactlyTheJobs pins the check for
// both jobs that download an artifact: one call to the run's listing, accepted
// only for exactly the job's names, and an error that names no artifact, no URL
// and no response text.
func TestCheckArtifactsRefusesAnyListThatIsNotExactlyTheJobs(t *testing.T) {
	t.Parallel()

	for _, j := range []struct {
		name     string
		job      github.Job
		expected []artifactEntry
	}{
		{name: "the model job", job: github.JobModel, expected: []artifactEntry{{name: github.ArtifactChecksResult}}},
		{name: "the publish job", job: github.JobPublish, expected: []artifactEntry{{name: github.ArtifactChecksResult}, {name: github.ArtifactModelResult}}},
	} {
		t.Run(j.name, func(t *testing.T) {
			t.Parallel()

			for _, tc := range artifactCases(j.expected) {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					fake := newFakeGH(t, tc.route)
					client := newArtifactClient(t, fake.Path)

					err := github.CheckArtifacts(t.Context(), client, j.job)

					switch {
					case tc.want == nil:
						if err != nil {
							t.Errorf("CheckArtifacts(%s) = %v, want no error", j.name, err)
						}
					case !errors.Is(err, tc.want):
						t.Fatalf("CheckArtifacts(%s) = %v, want an error wrapping %v", j.name, err, tc.want)
					}

					if err != nil {
						for _, hidden := range []string{
							artifactsResponseMarker,
							"unexpected-artifact",
							github.ArtifactChecksResult,
							github.ArtifactModelResult,
							"https://",
							"api.github.com",
							fixtureRepository,
						} {
							if strings.Contains(err.Error(), hidden) {
								t.Errorf("the error names %q: %v", hidden, err)
							}
						}
					}

					calls := fake.Calls(t)
					if len(calls) != 1 || calls[0].Endpoint != fixtureArtifactsEndpoint {
						t.Errorf("the check made %+v, want one call to %s", calls, fixtureArtifactsEndpoint)
					}
				})
			}
		})
	}
}

// TestArtifactsOutcomeMapsTheClassToTheLineAndCode pins what the artifacts
// subcommand prints and returns: the exact lines of ADR 0040 and the PRD's
// runbook step 11, chosen by the error's class alone.
func TestArtifactsOutcomeMapsTheClassToTheLineAndCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		err      error
		wantLine string
		wantCode int
	}{
		{name: "a run whose artifacts are as expected", err: nil, wantLine: "artifacts: as expected", wantCode: 0},
		{name: "an unexpected artifact list", err: fmt.Errorf("the check: %w", github.ErrArtifacts), wantLine: "artifacts: unexpected: see runbook step 11", wantCode: 3},
		{name: "a failed GitHub call", err: fmt.Errorf("the call: %w", publish.ErrGitHub), wantLine: "GitHub call failed: see runbook", wantCode: 8},
		{name: "an invalid configuration", err: fmt.Errorf("the flags: %w", publish.ErrConfig), wantLine: "configuration invalid: see runbook", wantCode: 9},
		{name: "any other failure", err: errors.New("some failure"), wantLine: "publish failed: see runbook", wantCode: 1},
		{name: "a refused endpoint", err: fmt.Errorf("the call: %w", github.ErrEndpointRefused), wantLine: "publish failed: see runbook", wantCode: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			line, code := github.ArtifactsOutcome(tc.err)
			if line != tc.wantLine {
				t.Errorf("ArtifactsOutcome(%v) line = %q, want %q", tc.err, line, tc.wantLine)
			}

			if code != tc.wantCode {
				t.Errorf("ArtifactsOutcome(%v) code = %d, want %d", tc.err, code, tc.wantCode)
			}
		})
	}
}

// TestArtifactsOutcomeKeepsTheCoresGitHubAndConfigLines pins that the two lines
// the artifact check shares with the publish run are not copies that could
// drift: they are the core's outcome for the same class.
func TestArtifactsOutcomeKeepsTheCoresGitHubAndConfigLines(t *testing.T) {
	t.Parallel()

	for _, err := range []error{publish.ErrGitHub, publish.ErrConfig} {
		line, code := github.ArtifactsOutcome(err)
		outcome := publish.OutcomeOf(err)

		if line != outcome.Summary() || code != outcome.Code() {
			t.Errorf("ArtifactsOutcome(%v) = %q, %d, want the core's %q, %d", err, line, code, outcome.Summary(), outcome.Code())
		}
	}
}

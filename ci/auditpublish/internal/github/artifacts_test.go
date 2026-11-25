package github_test

// Group C of PR 4a: the artifact check (PRD #175 milestone 2, "Each job that
// downloads an artifact downloads it by exact name"; ADR 0040, "The publish
// program's GitHub side"). One listing of the run's artifacts decides whether a
// job may download: exactly the job's names, no name twice, nothing expired and
// no second page, and the artifact the job downloads carries a positive ID and
// a sha256:<64 hex> digest, the two values the download is bound to. GitHub's
// failure is the core's GitHub class, and the check's one summary line and exit
// code come from the error's class alone.

import (
	"encoding/json"
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

// artifactEntry is one entry of the run's artifact listing. id and digest are
// the values the listing carries for the download: a test that leaves them out
// gets the entry's own, one per position, so a listing is always one the check
// can bind. override writes an entry the check must refuse: a key it sets is
// written in place of the entry's own, and a key it sets to nil is written
// not at all.
type artifactEntry struct {
	name     string
	expired  bool
	id       int64
	digest   string
	override map[string]any
}

// artifactEntryID and artifactEntryDigest are the ID and digest the listing
// carries for the entry at index i: invented, and different per position so
// that a test can tell the entries apart.
func artifactEntryID(i int) int64 { return int64(1000 + i) }

func artifactEntryDigest(i int) string { return fmt.Sprintf("sha256:%064x", 1000+i) }

// artifactEntryBody is one entry as GitHub writes it.
func artifactEntryBody(e artifactEntry, i int) map[string]any {
	id := e.id
	if id == 0 {
		id = artifactEntryID(i)
	}

	digest := e.digest
	if digest == "" {
		digest = artifactEntryDigest(i)
	}

	body := map[string]any{"name": e.name, "expired": e.expired, "id": id, "digest": digest}

	for key, value := range e.override {
		if value == nil {
			delete(body, key)

			continue
		}

		body[key] = value
	}

	return body
}

// artifactsRoute is the artifact check's answer: the entries GitHub lists and
// the total it counts, which differ when a second page exists.
func artifactsRoute(total int, entries ...artifactEntry) Route {
	artifacts := make([]map[string]any, 0, len(entries))

	for i, e := range entries {
		artifacts = append(artifacts, artifactEntryBody(e, i))
	}

	return Route{
		Method:   "GET",
		Endpoint: fixtureArtifactsEndpoint,
		Status:   200,
		Body:     map[string]any{"total_count": total, "artifacts": artifacts},
	}
}

// artifactListingJSON is the listing of entries as GitHub writes it, so that a
// case can put data after it: the fake gh writes a json.RawMessage body
// verbatim.
func artifactListingJSON(t *testing.T, total int, entries ...artifactEntry) []byte {
	t.Helper()

	artifacts := make([]map[string]any, 0, len(entries))

	for i, e := range entries {
		artifacts = append(artifacts, artifactEntryBody(e, i))
	}

	body, err := json.Marshal(map[string]any{"total_count": total, "artifacts": artifacts})
	if err != nil {
		t.Fatalf("marshal the artifact listing: %v", err)
	}

	return body
}

// artifactCase is one answer to the check and the verdict it must draw.
type artifactCase struct {
	name  string
	route Route
	// want is the class the error must wrap; nil when the check accepts.
	want error
}

// artifactCases is the check's answers for one job's expected names: the right
// list, and each way a list is wrong. download is the artifact the job
// downloads, the entry whose ID and digest the check binds: the cases below
// answer it without an ID, with an ID that is no ID, and without a digest or
// with one that is no sha256:<64 lower-case hex>.
func artifactCases(t *testing.T, expected []artifactEntry, download string) []artifactCase {
	t.Helper()

	extra := append(append([]artifactEntry{}, expected...), artifactEntry{name: "unexpected-artifact"})
	duplicate := append(append([]artifactEntry{}, expected...), expected[0])
	expired := append([]artifactEntry{}, expected...)
	expired[0].expired = true
	missing := expected[1:]
	listing := artifactListingJSON(t, len(expected), expected...)

	// downloaded answers the job's download entry with override, and every other
	// entry as it is: the listing stays the one the check accepts but for the
	// entry the download is bound to.
	downloaded := func(override map[string]any) []artifactEntry {
		entries := append([]artifactEntry{}, expected...)

		for i := range entries {
			if entries[i].name == download {
				entries[i].override = override
			}
		}

		return entries
	}

	// withDownload is the case's listing, at the job's own size: the check counts
	// the entries the answer lists.
	withDownload := func(override map[string]any) Route {
		entries := downloaded(override)

		return artifactsRoute(len(entries), entries...)
	}

	// withTrailing answers the one call with the listing followed by trailing
	// data, written after the JSON document as it is.
	withTrailing := func(trailing string) Route {
		return Route{
			Method:   "GET",
			Endpoint: fixtureArtifactsEndpoint,
			Status:   200,
			Body:     json.RawMessage(listing),
			Trailing: trailing,
		}
	}

	return []artifactCase{
		{name: "exactly the job's artifacts", route: artifactsRoute(len(expected), expected...), want: nil},
		{name: "an extra artifact", route: artifactsRoute(len(extra), extra...), want: github.ErrArtifacts},
		{name: "a name twice", route: artifactsRoute(len(duplicate), duplicate...), want: github.ErrArtifacts},
		{name: "an expected artifact missing", route: artifactsRoute(len(missing), missing...), want: github.ErrArtifacts},
		{name: "a second page", route: artifactsRoute(len(expected)+1, expected...), want: github.ErrArtifacts},
		{name: "an expired artifact", route: artifactsRoute(len(expected), expired...), want: github.ErrArtifacts},
		// The download is bound to the ID and the digest the listing carries for
		// the artifact the job downloads (Artifact), so an entry without a
		// positive ID or without a sha256:<64 lower-case hex> digest is an answer
		// the check refuses, and not one a step downloads unbound.
		{name: "the downloaded artifact without an ID", route: withDownload(map[string]any{"id": nil}), want: publish.ErrGitHub},
		{name: "the downloaded artifact with ID 0", route: withDownload(map[string]any{"id": 0}), want: publish.ErrGitHub},
		{name: "the downloaded artifact with an ID that is no number", route: withDownload(map[string]any{"id": "1000"}), want: publish.ErrGitHub},
		{name: "the downloaded artifact without a digest", route: withDownload(map[string]any{"digest": nil}), want: publish.ErrGitHub},
		{name: "the downloaded artifact with a truncated digest", route: withDownload(map[string]any{"digest": "sha256:0123456789abcdef"}), want: publish.ErrGitHub},
		{name: "the downloaded artifact with an upper-case digest", route: withDownload(map[string]any{"digest": "sha256:" + strings.Repeat("A", 64)}), want: publish.ErrGitHub},
		{name: "the downloaded artifact with a digest of another algorithm", route: withDownload(map[string]any{"digest": "sha512:" + strings.Repeat("a", 64)}), want: publish.ErrGitHub},
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
		{
			// The listing is read with one Decode, which must be the whole
			// answer: data after the object is no listing, as the allow list's
			// decodeObject already refuses it (ADR 0040).
			name:  "a listing followed by a second object",
			route: withTrailing(`{"total_count":0,"artifacts":[]}`),
			want:  publish.ErrGitHub,
		},
		{
			name:  "a listing followed by junk",
			route: withTrailing("\nthe rest of the answer, which is no JSON\n"),
			want:  publish.ErrGitHub,
		},
		{
			// Whitespace after the object is no data after it: the decoder
			// reads it and the listing stands.
			name:  "a listing followed by whitespace alone",
			route: withTrailing(" \n\t\r\n"),
			want:  nil,
		},
	}
}

// TestCheckArtifactsRefusesAnyListThatIsNotExactlyTheJobs pins the check for
// both jobs that download an artifact: one call to the run's listing, accepted
// only for exactly the job's names, the artifact the job downloads returned
// with the listing's ID and digest, and an error that names no artifact, no URL
// and no response text.
func TestCheckArtifactsRefusesAnyListThatIsNotExactlyTheJobs(t *testing.T) {
	t.Parallel()

	for _, j := range []struct {
		name string
		job  github.Job
		// download is the artifact the job downloads, the one the check returns.
		download string
		expected []artifactEntry
	}{
		{
			name: "the model job", job: github.JobModel, download: github.ArtifactChecksResult,
			expected: []artifactEntry{{name: github.ArtifactChecksResult}},
		},
		{
			name: "the publish job", job: github.JobPublish, download: github.ArtifactModelResult,
			expected: []artifactEntry{{name: github.ArtifactChecksResult}, {name: github.ArtifactModelResult}},
		},
	} {
		t.Run(j.name, func(t *testing.T) {
			t.Parallel()

			for _, tc := range artifactCases(t, j.expected, j.download) {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					fake := newFakeGH(t, tc.route)
					client := newArtifactClient(t, fake.Path)

					artifact, err := github.CheckArtifacts(t.Context(), client, j.job)

					switch {
					case tc.want == nil:
						if err != nil {
							t.Errorf("CheckArtifacts(%s) = %v, want no error", j.name, err)
						}

						if artifact.Name != j.download {
							t.Errorf("CheckArtifacts(%s) returned %q, want the artifact the job downloads, %q", j.name, artifact.Name, j.download)
						}

						if artifact.ID <= 0 {
							t.Errorf("CheckArtifacts(%s) returned the artifact's ID %d, want the listing's positive ID: the download is bound to it", j.name, artifact.ID)
						}

						if !strings.HasPrefix(artifact.Digest, "sha256:") || len(artifact.Digest) != len("sha256:")+64 {
							t.Errorf("CheckArtifacts(%s) returned the artifact's digest %q, want the listing's sha256:<64 hex>", j.name, artifact.Digest)
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

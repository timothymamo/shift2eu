package github_test

// Group D of PR 4b: the release and image-digest checks (PRD #175 milestone 2,
// "Report and triage"; ADR 0040, "The publish program's GitHub side"). The
// result's tag must name a release of the repository, and its image digest,
// when it has one, an attestation of that repository whose subject is the
// digest and whose SLSA provenance names the workflow ref refs/tags/<tag>.
// The digest check runs on a client that is read-only by construction: the
// audit App has no Attestations permission, so the image subcommand holds the
// job's token and reads nothing else (maintainer decision 2).

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// releaseMarker is text only an answer of the checks holds, so a test can show
// that none of it reaches an error.
const releaseMarker = "RESPONSE-TEXT-MARKER"

// newImageClient returns a client for ghPath, built through NewImageClient.
func newImageClient(t *testing.T, ghPath string) *github.Client {
	t.Helper()

	c, err := github.NewImageClient(github.ClientConfig{
		GHPath:  ghPath,
		Token:   github.NewToken(fixtureToken(t)),
		TempDir: t.TempDir(),
		Scope:   testScope(),
	})
	if err != nil {
		t.Fatalf("NewImageClient: %v", err)
	}

	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close(): %v", err)
		}
	})

	return c
}

// TestCheckRelease pins the release lookup: one call to the tag's release,
// accepted only by a release of exactly that tag, and a tag the endpoint table
// refuses refused before gh runs.
func TestCheckRelease(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		tag  string
		// routes answer the lookup; none means the case's tag never reaches gh.
		routes []Route
		want   error
		calls  int
	}{
		{
			name:   "the tag's release",
			tag:    fixtureTag,
			routes: []Route{{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": fixtureTag}}},
			want:   nil,
			calls:  1,
		},
		{
			name:   "no release of that tag",
			tag:    fixtureTag,
			routes: []Route{{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 404, Body: map[string]string{"message": releaseMarker}}},
			want:   publish.ErrResultInvalid,
			calls:  1,
		},
		{
			name:   "a release of another tag",
			tag:    fixtureTag,
			routes: []Route{{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": "v0.11.0"}}},
			want:   publish.ErrGitHub,
			calls:  1,
		},
		{
			name:   "an answer that is no release",
			tag:    fixtureTag,
			routes: []Route{{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: []string{releaseMarker}}},
			want:   publish.ErrGitHub,
			calls:  1,
		},
		{
			name:   "a failed call",
			tag:    fixtureTag,
			routes: []Route{{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 500, Body: map[string]string{"message": releaseMarker}}},
			want:   publish.ErrGitHub,
			calls:  1,
		},
		{
			name:   "a tag that is no release tag",
			tag:    "v0.12",
			routes: nil,
			want:   publish.ErrResultInvalid,
			calls:  0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, tc.routes...)
			client := newFixtureClient(t, fake.Path)

			err := github.CheckRelease(t.Context(), client, tc.tag)

			switch {
			case tc.want == nil:
				if err != nil {
					t.Errorf("CheckRelease(%q) = %v, want no error", tc.tag, err)
				}
			case !errors.Is(err, tc.want):
				t.Errorf("CheckRelease(%q) = %v, want an error wrapping %v", tc.tag, err, tc.want)
			}

			if err != nil {
				for _, hidden := range []string{releaseMarker, fixtureTag, fixtureRepository, "https://", "api.github.com"} {
					if strings.Contains(err.Error(), hidden) {
						t.Errorf("the error names %q: %v", hidden, err)
					}
				}
			}

			if calls := fake.Calls(t); len(calls) != tc.calls {
				t.Errorf("the check made %d call(s), want %d: %+v", len(calls), tc.calls, calls)
			}
		})
	}
}

// TestCheckImageDigest pins the digest check: no digest is nothing to check and
// no call; otherwise one attestation listing of the digest, accepted only by an
// attestation whose subject is the digest, whose predicate is the SLSA
// provenance and whose workflow names refs/tags/<tag> in this repository.
func TestCheckImageDigest(t *testing.T) {
	t.Parallel()

	sbom := "https://spdx.dev/Document"

	// wrongPayload is an attestation whose payload is no base64: it is skipped
	// as any other non-matching attestation is.
	wrongPayload := attestation(t, fixtureStatement(fixtureDigestHex(), "refs/tags/"+fixtureTag, fixtureWorkflowRepository(), "https://slsa.dev/provenance/v1"))
	wrongPayload["bundle"].(map[string]any)["dsseEnvelope"].(map[string]any)["payload"] = "***not-base64***"

	for _, tc := range []struct {
		name   string
		digest string
		routes []Route
		want   error
		calls  int
	}{
		{
			name:   "the digest's attestation",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 200, Body: attestationListing(
				attestation(t, fixtureStatement(fixtureDigestHex(), "refs/tags/"+fixtureTag, fixtureWorkflowRepository(), "https://slsa.dev/provenance/v1")),
			)}},
			want:  nil,
			calls: 1,
		},
		{
			name:   "the digest's attestation after an SBOM",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 200, Body: attestationListing(
				attestation(t, fixtureStatement(fixtureDigestHex(), "refs/tags/"+fixtureTag, fixtureWorkflowRepository(), sbom)),
				attestation(t, fixtureStatement(fixtureDigestHex(), "refs/tags/"+fixtureTag, fixtureWorkflowRepository(), "https://slsa.dev/provenance/v1")),
			)}},
			want:  nil,
			calls: 1,
		},
		{
			name:   "another subject digest",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 200, Body: attestationListing(
				attestation(t, fixtureStatement(strings.Repeat("2", 64), "refs/tags/"+fixtureTag, fixtureWorkflowRepository(), "https://slsa.dev/provenance/v1")),
			)}},
			want:  publish.ErrResultInvalid,
			calls: 1,
		},
		{
			name:   "another workflow ref",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 200, Body: attestationListing(
				attestation(t, fixtureStatement(fixtureDigestHex(), "refs/heads/main", fixtureWorkflowRepository(), "https://slsa.dev/provenance/v1")),
			)}},
			want:  publish.ErrResultInvalid,
			calls: 1,
		},
		{
			name:   "another repository's workflow",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 200, Body: attestationListing(
				attestation(t, fixtureStatement(fixtureDigestHex(), "refs/tags/"+fixtureTag, fixtureServerURL+"/example/other", "https://slsa.dev/provenance/v1")),
			)}},
			want:  publish.ErrResultInvalid,
			calls: 1,
		},
		{
			name:   "another predicate type",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 200, Body: attestationListing(
				attestation(t, fixtureStatement(fixtureDigestHex(), "refs/tags/"+fixtureTag, fixtureWorkflowRepository(), sbom)),
			)}},
			want:  publish.ErrResultInvalid,
			calls: 1,
		},
		{
			name:   "an undecodable payload",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 200, Body: attestationListing(wrongPayload)}},
			want:   publish.ErrResultInvalid,
			calls:  1,
		},
		{
			name:   "no attestation at all",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 200, Body: attestationListing()}},
			want:   publish.ErrResultInvalid,
			calls:  1,
		},
		{
			name:   "no attestation of the digest",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 404, Body: map[string]string{"message": releaseMarker}}},
			want:   publish.ErrResultInvalid,
			calls:  1,
		},
		{
			name:   "a failed call",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 500, Body: map[string]string{"message": releaseMarker}}},
			want:   publish.ErrGitHub,
			calls:  1,
		},
		{
			name:   "an answer that is no attestation listing",
			digest: fixtureDigest,
			routes: []Route{{Method: "GET", Endpoint: fixtureAttestationsEndpoint, Status: 200, Body: map[string]string{"message": releaseMarker}}},
			want:   publish.ErrGitHub,
			calls:  1,
		},
		{
			name:   "a digest the endpoint table refuses",
			digest: "sha256:" + strings.Repeat("Z", 64),
			routes: nil,
			want:   publish.ErrResultInvalid,
			calls:  0,
		},
		{
			name:   "no digest at all",
			digest: "",
			routes: nil,
			want:   nil,
			calls:  0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, tc.routes...)
			client := newImageClient(t, fake.Path)

			err := github.CheckImageDigest(t.Context(), client, fixtureTag, tc.digest)

			switch {
			case tc.want == nil:
				if err != nil {
					t.Errorf("CheckImageDigest(%q) = %v, want no error", tc.digest, err)
				}
			case !errors.Is(err, tc.want):
				t.Errorf("CheckImageDigest(%q) = %v, want an error wrapping %v", tc.digest, err, tc.want)
			}

			if err != nil {
				for _, hidden := range []string{releaseMarker, fixtureDigest, fixtureTag, "refs/", fixtureRepository} {
					if strings.Contains(err.Error(), hidden) {
						t.Errorf("the error names %q: %v", hidden, err)
					}
				}
			}

			if calls := fake.Calls(t); len(calls) != tc.calls {
				t.Errorf("the check made %d call(s), want %d: %+v", len(calls), tc.calls, calls)
			}
		})
	}
}

// TestNewImageClientIsReadOnlyByConstruction pins the image subcommand's
// client: built through NewImageClient it writes nothing, whatever its config
// says, so the job's token reaches no wider than the attestations it reads.
func TestNewImageClientIsReadOnlyByConstruction(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t)

	c, err := github.NewImageClient(github.ClientConfig{
		GHPath:  fake.Path,
		Token:   github.NewToken(fixtureToken(t)),
		TempDir: t.TempDir(),
		Scope:   testScope(),
		// The flag the constructor must not depend on.
		ReadOnly: false,
	})
	if err != nil {
		t.Fatalf("NewImageClient: %v", err)
	}

	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close(): %v", err)
		}
	})

	_, err = c.Call(t.Context(), github.Request{
		Method:   "POST",
		Endpoint: fixtureBlobsEndpoint,
		Body:     map[string]any{"content": "aGVsbG8=", "encoding": "utf-8"},
	})
	if !errors.Is(err, github.ErrEndpointRefused) {
		t.Fatalf("a POST on the image client = %v, want an error wrapping ErrEndpointRefused", err)
	}

	if calls := fake.Calls(t); len(calls) != 0 {
		t.Errorf("the refused write ran gh %d time(s), want 0: %+v", len(calls), calls)
	}
}

// TestImageOutcomeMapsTheClassToTheLineAndCode pins the image subcommand's one
// summary line and exit code, from the error's class alone.
func TestImageOutcomeMapsTheClassToTheLineAndCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		err      error
		wantLine string
		wantCode int
	}{
		{name: "a digest as expected", err: nil, wantLine: "image digest: as expected", wantCode: 0},
		{name: "a refused result", err: fmt.Errorf("the check: %w", publish.ErrResultInvalid), wantLine: "result refused: see runbook", wantCode: 3},
		{name: "a failed GitHub call", err: fmt.Errorf("the call: %w", publish.ErrGitHub), wantLine: "GitHub call failed: see runbook", wantCode: 8},
		{name: "an invalid configuration", err: fmt.Errorf("the flags: %w", publish.ErrConfig), wantLine: "configuration invalid: see runbook", wantCode: 9},
		{name: "any other failure", err: errors.New("some failure"), wantLine: "publish failed: see runbook", wantCode: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			line, code := github.ImageOutcome(tc.err)
			if line != tc.wantLine {
				t.Errorf("ImageOutcome(%v) line = %q, want %q", tc.err, line, tc.wantLine)
			}

			if code != tc.wantCode {
				t.Errorf("ImageOutcome(%v) code = %d, want %d", tc.err, code, tc.wantCode)
			}
		})
	}
}

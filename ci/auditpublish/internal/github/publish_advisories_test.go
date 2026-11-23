package github_test

// Group P of PR 4c: the publish run's draft advisories (PRD #175 milestone 2,
// "Only what the App wrote", runbook step 11's exit codes 5 and 6; ADR 0040).
// P1 - a private finding filed as a draft, the listing, the POST and then the
// report - is the test publish_test.go pins, since 4c changed what that run
// does. This file holds the rest: a refused creation is the not-filed path with
// its exit 5 and no further advisory call, a duplicate is exit 6 with no write
// at all, a PATCH reaches only one of the same run's listing's App drafts,
// every advisory text is scanned before the first write, and the evidence
// reaches the advisory and no other text of the run.

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// privateScenarioMarker is text only the private finding holds, so a test can
// show that a refused run wrote nothing of it.
const privateScenarioMarker = "the private scenario marker"

// privateFindingOf returns the model's private finding the advisory tests file:
// a confirmed high finding of a private category, its scenario a marker no
// other text of the run may hold.
func privateFindingOf() map[string]any {
	f := fixtureFinding("G302", "high", "injection")
	f["scenario"] = privateScenarioMarker

	return f
}

// fixtureFingerprint returns the keyed fingerprint a fixture finding gets: the
// one its draft advisory's summary holds.
func fixtureFingerprint(t *testing.T, f map[string]any) publish.Fingerprint {
	t.Helper()

	fp, err := publish.KeyedFingerprint(fixtureKey(t), f["rule"].(string), f["file"].(string), f["sink"].(string))
	if err != nil {
		t.Fatalf("KeyedFingerprint: %v", err)
	}

	return fp
}

// draftEntry returns the listing entry of the audit App's draft whose summary
// holds fp, its description's status line stating status.
func draftEntry(ghsa string, fp publish.Fingerprint, status string) map[string]any {
	entry := auditAdvisoryEntry(ghsa, advisorySummary(fp), github.AdvisoryStateDraft)
	entry["description"] = draftDescription(status)

	return entry
}

// closedEntry returns the listing entry of the audit App's closed advisory
// whose summary holds fp, closed at closedAt.
func closedEntry(ghsa string, fp publish.Fingerprint, status, closedAt string) map[string]any {
	entry := auditAdvisoryEntry(ghsa, advisorySummary(fp), "closed")
	entry["description"] = draftDescription(status)
	entry["closed_at"] = closedAt

	return entry
}

// publishRoutesWithAdvisories returns a whole publish run's answers: the
// advisory listing the run reads, the release, the issues and the report's
// writes, and the advisory writes the caller adds.
func publishRoutesWithAdvisories(listing []map[string]any, writes []Route, issues ...map[string]any) []Route {
	routes := publishRoutesOverListing([]Route{advisoryListingRoute(listing, "")}, issues...)

	return append(routes, writes...)
}

// advisoryCreateRoute returns the answer of a draft's create.
func advisoryCreateRoute(status int) Route {
	return Route{
		Method:   "POST",
		Endpoint: fixtureRepoPrefix + "/security-advisories",
		Status:   status,
		Body:     map[string]any{"ghsa_id": fixtureGHSA2},
	}
}

// advisoryCreates returns the bodies of the run's draft creates.
func advisoryCreates(t *testing.T, calls []Call) []map[string]any {
	t.Helper()

	return recordedBodies[map[string]any](t, calls, "POST", fixtureRepoPrefix+"/security-advisories")
}

// advisoryEndpoints returns the endpoints of every advisory call, the writes
// and the listing, in their order.
func advisoryEndpoints(calls []Call) []string {
	var out []string

	for _, c := range calls {
		if strings.Contains(c.Endpoint, "security-advisories") {
			out = append(out, endpointOf(c))
		}
	}

	return out
}

// recordedBodiesText returns every recorded call's body as one text, so a test
// can show that a text reached no write at all.
func recordedBodiesText(calls []Call) string {
	var b strings.Builder

	for _, c := range calls {
		b.Write(c.Body)
		b.WriteString("\n")
	}

	return b.String()
}

// crashBytes are an invented crash input's bytes: binary, none of them the
// 0x30 that starts a DER private key, and none printable, so the base64 text
// the scan reads decodes to no token.
var crashBytes = []byte{
	0x80, 0xa5, 0xca, 0xef, 0x94, 0xb9, 0xde, 0x83, 0xa8, 0xcd, 0xf2, 0x97,
	0xbc, 0xe1, 0x86, 0xab, 0xd0, 0xf5, 0x9a, 0xbf, 0xe4, 0x89, 0xae, 0xd3,
}

// crashInput returns an invented base64 crash input: bytes that decode to no
// token and to no printable text, so only the advisory's own scan could refuse
// it - and it must not, since an advisory legitimately holds PoCs.
func crashInput() string {
	return base64.StdEncoding.EncodeToString(crashBytes)
}

// TestPublishReportsAPrivateFindingNotFiled pins the runbook's exit 5: a create
// GitHub refuses is the private findings not filed. The fallback report says so
// and is published, the issues are published, and no advisory call follows the
// refused one; nothing of the private finding reaches any other write.
func TestPublishReportsAPrivateFindingNotFiled(t *testing.T) {
	t.Parallel()

	// The previous report holds one public finding the run files an issue for.
	open := fixtureFinding("G301", "low", "hardening")
	previous := renderFixtureReport(t, fixtureResult(t, open), publish.PrivateNone, nil, "2026-12-01", "1")

	dir := fixtureReportsDir(t)
	writeFixtureReport(t, dir, previous)

	for _, status := range []int{403, 404, 422} {
		t.Run("a create answered "+strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, publishRoutesWithAdvisories(nil, []Route{advisoryCreateRoute(status)})...)
			client := newFixtureClient(t, fake.Path)

			cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t, privateFindingOf()), dir)

			published, err := github.Publish(t.Context(), cfg)
			if err != nil {
				t.Fatalf("Publish = %v, want no error: a refused creation is the not-filed path", err)
			}

			want := github.Published{Public: 0, Private: publish.PrivateNotFiled}
			if diff := cmp.Diff(want, published); diff != "" {
				t.Errorf("the run's counts (-want +got):\n%s", diff)
			}

			outcome := publish.OutcomeOf(nil).WithCounts(published.Public, published.Private)
			if outcome.Code() != 5 || outcome.Summary() != "private findings not filed: see runbook" {
				t.Errorf("the run's outcome is %q, %d, want the not-filed line, exit 5", outcome.Summary(), outcome.Code())
			}

			calls := fake.Calls(t)

			// The listing, then the one refused create, and nothing after it.
			wantCalls := []string{
				"GET " + fixtureAdvisoriesEndpoint,
				"POST " + fixtureRepoPrefix + "/security-advisories",
			}

			if diff := cmp.Diff(wantCalls, advisoryEndpoints(calls)); diff != "" {
				t.Errorf("the run's advisory calls (-want +got):\n%s", diff)
			}

			blobs := recordedBodies[recordedBlob](t, calls, "POST", fixtureBlobsEndpoint)
			if len(blobs) != 1 {
				t.Fatalf("the run made %d blob(s), want 1: the report is published", len(blobs))
			}

			if !strings.Contains(blobs[0].Content, "private findings: not filed") {
				t.Errorf("the report holds no not-filed line: a refused creation is never silent")
			}

			var filed int

			for _, c := range calls {
				if c.Method == "POST" && c.Endpoint == fixtureIssuesEndpoint {
					filed++
				}
			}

			if filed != 1 {
				t.Errorf("the run filed %d issue(s), want 1: the issues are published after a refused creation", filed)
			}

			if bodies := recordedBodiesText(calls); strings.Contains(bodies, privateScenarioMarker) {
				t.Errorf("a write holds %q, want the private findings dropped: a refused creation files them nowhere",
					privateScenarioMarker)
			}
		})
	}
}

// TestPublishRefusesTwoDraftsOfOneFingerprint pins the runbook's exit 6: two of
// the App's own drafts holding one fingerprint are no plan, so the run makes no
// write at all and reads no branch.
func TestPublishRefusesTwoDraftsOfOneFingerprint(t *testing.T) {
	t.Parallel()

	fp := fixtureFingerprint(t, privateFindingOf())

	listing := []map[string]any{
		draftEntry(fixtureGHSA, fp, "open"),
		draftEntry(fixtureGHSA2, fp, "open"),
	}

	fake := newFakeGH(t, publishRoutesWithAdvisories(listing, nil)...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t, privateFindingOf()), fixtureReportsDir(t))

	published, err := github.Publish(t.Context(), cfg)

	if !errors.Is(err, publish.ErrDuplicateDrafts) {
		t.Fatalf("Publish = %+v, %v, want an error wrapping publish.ErrDuplicateDrafts", published, err)
	}

	outcome := publish.OutcomeOf(err)
	if outcome.Code() != 6 {
		t.Errorf("the run's outcome is %q, %d, want the duplicate outcome, exit 6", outcome.Summary(), outcome.Code())
	}

	calls := fake.Calls(t)

	for _, c := range calls {
		if c.Method != "GET" {
			t.Errorf("the duplicate run wrote %s %s, want no write at all", c.Method, c.Endpoint)
		}

		if c.Endpoint == fixtureBranchEndpoint {
			t.Errorf("the duplicate run looked up the report's branch, want no write at all")
		}
	}
}

// TestPublishPatchesOnlyTheAppDraft pins the PATCH bound on a whole run: only
// an App-authored draft with no credit and no collaborator, shown by this run's
// own listing, is ever patched. A published or triage advisory of the finding's
// fingerprint, another author's draft and a draft a person is credited on are
// no draft of the run's to update - a PATCH of a published advisory with state
// draft would reopen it - and a closed one is never patched either, whichever
// status it holds: a finding of its fingerprint is filed anew or dismissed, and
// every case leaves it untouched.
func TestPublishPatchesOnlyTheAppDraft(t *testing.T) {
	t.Parallel()

	fp := fixtureFingerprint(t, privateFindingOf())

	inState := func(state string) map[string]any {
		return auditAdvisoryEntry(fixtureGHSA, advisorySummary(fp), state)
	}

	foreignDraft := func() map[string]any {
		entry := inState(github.AdvisoryStateDraft)
		entry["author"] = map[string]any{"login": "someone", "type": "User"}

		return entry
	}

	creditedDraft := func() map[string]any {
		entry := inState(github.AdvisoryStateDraft)
		entry["credits"] = []any{map[string]any{"login": "someone"}}

		return entry
	}

	for _, tc := range []struct {
		name string
		// entry is the one advisory of the run's listing.
		entry map[string]any
		// wantCreates is how many drafts the run files, and wantStatus the
		// status line of the one it files, empty when it files none.
		wantCreates int
		wantStatus  string
	}{
		{
			name:        "a published advisory of the finding's fingerprint",
			entry:       inState("published"),
			wantCreates: 1,
			wantStatus:  "new",
		},
		{
			name:        "a triage advisory of the finding's fingerprint",
			entry:       inState("triage"),
			wantCreates: 1,
			wantStatus:  "new",
		},
		{
			name:        "another author's draft of the finding's fingerprint",
			entry:       foreignDraft(),
			wantCreates: 1,
			wantStatus:  "new",
		},
		{
			name:        "a draft of the finding's fingerprint with a foreign credit",
			entry:       creditedDraft(),
			wantCreates: 1,
			wantStatus:  "new",
		},
		{
			name:        "a closed advisory of the finding's fingerprint marked not reproduced",
			entry:       closedEntry(fixtureGHSA, fp, "not reproduced", "2026-12-01T00:00:00Z"),
			wantCreates: 1,
			wantStatus:  "regressed",
		},
		{
			name:  "a closed advisory of the finding's fingerprint that says open",
			entry: closedEntry(fixtureGHSA, fp, "open", "2026-12-01T00:00:00Z"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, publishRoutesWithAdvisories(
				[]map[string]any{tc.entry},
				[]Route{advisoryCreateRoute(201)},
			)...)
			client := newFixtureClient(t, fake.Path)
			cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t, privateFindingOf()), fixtureReportsDir(t))

			published, err := github.Publish(t.Context(), cfg)
			if err != nil {
				t.Fatalf("Publish = %v, want no error", err)
			}

			if published.Private != publish.PrivateHandled {
				t.Errorf("the run's private state is %v, want PrivateHandled: the run reproduced a private finding", published.Private)
			}

			calls := fake.Calls(t)

			for _, c := range calls {
				if c.Method == "PATCH" {
					t.Errorf("the run patched %s, want no PATCH: the listing shows no App draft of the finding's fingerprint", c.Endpoint)
				}
			}

			want := []string{"GET " + fixtureAdvisoriesEndpoint}
			if tc.wantCreates == 1 {
				want = append(want, "POST "+fixtureRepoPrefix+"/security-advisories")
			}

			if diff := cmp.Diff(want, advisoryEndpoints(calls)); diff != "" {
				t.Errorf("the run's advisory calls (-want +got):\n%s", diff)
			}

			creates := advisoryCreates(t, calls)
			if len(creates) != tc.wantCreates {
				t.Fatalf("the run created %d draft(s), want %d", len(creates), tc.wantCreates)
			}

			if tc.wantStatus != "" {
				description, _ := creates[0]["description"].(string)
				if !strings.Contains(description, "\nAudit status: "+tc.wantStatus+"\n") {
					t.Errorf("the draft's description states no %q status:\n%s", tc.wantStatus, description)
				}
			}
		})
	}
}

// TestPublishScansTheAdvisoryTexts pins the scan of the advisory texts before
// the first write: a token in the evidence, one base64-encoded there, and a
// gitleaks hit are refused with no write at all, while a base64 crash input -
// which an advisory legitimately holds - is published.
func TestPublishScansTheAdvisoryTexts(t *testing.T) {
	t.Parallel()

	token := modelToken()

	for _, tc := range []struct {
		name string
		// evidence is the private finding's evidence, and refused whether the
		// run must stop before its first write.
		evidence string
		refused  bool
	}{
		{name: "a raw token", evidence: "the trace holds " + token, refused: true},
		{name: "a base64-encoded token", evidence: "the trace holds " + base64.StdEncoding.EncodeToString([]byte(token)), refused: true},
		{name: "a gitleaks hit", evidence: "the trace holds " + fakeGitleaksHit, refused: true},
		{name: "a base64 crash input", evidence: "the trace holds " + crashInput()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			finding := privateFindingOf()
			finding["evidence"] = tc.evidence

			fake := newFakeGH(t, publishRoutesWithAdvisories(nil, []Route{advisoryCreateRoute(201)})...)
			client := newFixtureClient(t, fake.Path)
			cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t, finding), fixtureReportsDir(t))

			published, err := github.Publish(t.Context(), cfg)

			if !tc.refused {
				if err != nil {
					t.Fatalf("Publish = %v, want no error: an advisory holds base64 crash inputs", err)
				}

				created := advisoryCreates(t, fake.Calls(t))
				if len(created) != 1 {
					t.Fatalf("the run created %d draft(s), want 1", len(created))
				}

				description, _ := created[0]["description"].(string)
				if !strings.Contains(description, tc.evidence) {
					t.Errorf("the draft's description holds no crash input:\n%s", description)
				}

				if published.Private != publish.PrivateHandled {
					t.Errorf("the run's private state is %v, want PrivateHandled", published.Private)
				}

				return
			}

			if !errors.Is(err, publish.ErrScanRefused) {
				t.Fatalf("Publish = %+v, %v, want an error wrapping publish.ErrScanRefused", published, err)
			}

			calls := fake.Calls(t)

			if writes := writesOf(calls); len(writes) != 0 {
				t.Errorf("the refused run wrote %v, want nothing: every scan runs before the first write", writes)
			}

			for _, c := range calls {
				if c.Method == "PATCH" {
					t.Errorf("the refused run patched %s, want no write at all", c.Endpoint)
				}

				if c.Endpoint == fixtureBranchEndpoint {
					t.Errorf("the refused run looked up the report's branch, want the writes' phase never to start")
				}
			}
		})
	}
}

// TestPublishHoldsTheEvidenceInTheAdvisoryOnly pins where the evidence goes on
// a whole run: into the draft advisory's description, and into no blob, pull
// request, commit or issue.
func TestPublishHoldsTheEvidenceInTheAdvisoryOnly(t *testing.T) {
	t.Parallel()

	const marker = "the evidence marker of the run"

	open := fixtureFinding("G301", "low", "hardening")
	previous := renderFixtureReport(t, fixtureResult(t, open), publish.PrivateNone, nil, "2026-12-01", "1")

	dir := fixtureReportsDir(t)
	writeFixtureReport(t, dir, previous)

	finding := privateFindingOf()
	finding["evidence"] = marker

	fake := newFakeGH(t, publishRoutesWithAdvisories(nil, []Route{advisoryCreateRoute(201)})...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t, finding), dir)

	if _, err := github.Publish(t.Context(), cfg); err != nil {
		t.Fatalf("Publish = %v, want no error", err)
	}

	calls := fake.Calls(t)

	created := advisoryCreates(t, calls)
	if len(created) != 1 {
		t.Fatalf("the run created %d draft(s), want 1", len(created))
	}

	if description, _ := created[0]["description"].(string); !strings.Contains(description, marker) {
		t.Errorf("the draft's description holds no evidence marker:\n%s", description)
	}

	blobs := recordedBodies[recordedBlob](t, calls, "POST", fixtureBlobsEndpoint)
	pulls := recordedBodies[recordedPull](t, calls, "POST", fixturePullsEndpoint)
	commits := recordedBodies[recordedCommit](t, calls, "POST", fixtureCommitsEndpoint)
	issues := recordedBodies[recordedIssue](t, calls, "POST", fixtureIssuesEndpoint)

	if len(blobs) != 1 || len(pulls) != 1 || len(commits) != 1 || len(issues) != 1 {
		t.Fatalf("the run wrote %d blob(s), %d pull(s), %d commit(s) and %d issue(s), want 1, 1, 1 and 1",
			len(blobs), len(pulls), len(commits), len(issues))
	}

	for _, text := range []string{
		blobs[0].Content,
		pulls[0].Title,
		pulls[0].Body,
		commits[0].Message,
		issues[0].Title,
		issues[0].Body,
	} {
		if strings.Contains(text, marker) {
			t.Errorf("a public text holds the evidence marker, want it in the draft advisory only:\n%s", text)
		}
	}
}

// TestPublishMarksAnUnreproducedDraft pins a run with no private finding of its
// own: the App draft whose fingerprint it does not hold is marked not
// reproduced, with the listing's description and the state alone, and the
// report says it found no private finding.
func TestPublishMarksAnUnreproducedDraft(t *testing.T) {
	t.Parallel()

	listing := []map[string]any{draftEntry(fixtureGHSA, libraryFingerprint, "open")}

	fake := newFakeGH(t, publishRoutesWithAdvisories(
		listing,
		[]Route{{Method: "PATCH", Endpoint: advisoryPatchEndpoint(fixtureGHSA), Status: 200, Body: map[string]any{"ghsa_id": fixtureGHSA}}},
	)...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)),
		fixtureResultFile(t, fixtureFinding("G304", "low", "hardening")), fixtureReportsDir(t))

	published, err := github.Publish(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Publish = %v, want no error", err)
	}

	want := github.Published{Public: 1, Private: publish.PrivateNone}
	if diff := cmp.Diff(want, published); diff != "" {
		t.Errorf("the run's counts (-want +got):\n%s", diff)
	}

	calls := fake.Calls(t)

	patched := recordedBodies[map[string]any](t, calls, "PATCH", advisoryPatchEndpoint(fixtureGHSA))
	if len(patched) != 1 {
		t.Fatalf("the run patched %d time(s), want the one not-reproduced mark", len(patched))
	}

	if diff := cmp.Diff(notReproducedBody(draftDescription("not reproduced")), patched[0]); diff != "" {
		t.Errorf("the mark's body (-want +got):\n%s", diff)
	}

	blobs := recordedBodies[recordedBlob](t, calls, "POST", fixtureBlobsEndpoint)
	if len(blobs) != 1 || !strings.Contains(blobs[0].Content, "private findings: none") {
		t.Errorf("the report holds no none line: %+v", blobs)
	}
}

// TestPublishDismissesARecurrence pins a dismissed recurrence on a whole run:
// the most recently closed App advisory of the finding's fingerprint says open,
// so no draft is filed and none is patched, and the report says the private
// finding was handled privately.
func TestPublishDismissesARecurrence(t *testing.T) {
	t.Parallel()

	fp := fixtureFingerprint(t, privateFindingOf())

	listing := []map[string]any{closedEntry(fixtureGHSA, fp, "open", "2026-12-01T00:00:00Z")}

	fake := newFakeGH(t, publishRoutesWithAdvisories(listing, nil)...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t, privateFindingOf()), fixtureReportsDir(t))

	published, err := github.Publish(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Publish = %v, want no error", err)
	}

	if published.Private != publish.PrivateHandled {
		t.Errorf("the run's private state is %v, want PrivateHandled: a dismissal is no drop", published.Private)
	}

	calls := fake.Calls(t)

	if diff := cmp.Diff([]string{"GET " + fixtureAdvisoriesEndpoint}, advisoryEndpoints(calls)); diff != "" {
		t.Errorf("the run's advisory calls (-want +got):\n%s", diff)
	}

	blobs := recordedBodies[recordedBlob](t, calls, "POST", fixtureBlobsEndpoint)
	if len(blobs) != 1 || !strings.Contains(blobs[0].Content, "private findings: handled privately") {
		t.Errorf("the report holds no handled-privately line: %+v", blobs)
	}
}

// TestPublishStopsOnAListingItCannotRead pins the listing's refusals on a whole
// run: a failed call and a listing over its pages are GitHub's failure, and
// nothing is written.
func TestPublishStopsOnAListingItCannotRead(t *testing.T) {
	t.Parallel()

	eleventhPage := Route{
		Method:   "GET",
		Endpoint: advisoryPageEndpoint(cursorOf(github.MaxAdvisoryPages)),
		Status:   200,
		Body:     []any{},
	}

	for _, tc := range []struct {
		name    string
		listing []Route
	}{
		{
			name:    "a failed call",
			listing: []Route{{Method: "GET", Endpoint: fixtureAdvisoriesEndpoint, Status: 500, Body: map[string]string{"message": "the listing failed"}}},
		},
		{
			name:    "a listing over its pages",
			listing: append(advisoryPages(github.MaxAdvisoryPages, true), eleventhPage),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, publishRoutesOverListing(tc.listing)...)
			client := newFixtureClient(t, fake.Path)
			cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)),
				fixtureResultFile(t, fixtureFinding("G304", "low", "hardening")), fixtureReportsDir(t))

			published, err := github.Publish(t.Context(), cfg)

			if !errors.Is(err, publish.ErrGitHub) {
				t.Fatalf("Publish = %+v, %v, want an error wrapping publish.ErrGitHub", published, err)
			}

			calls := fake.Calls(t)

			if writes := writesOf(calls); len(writes) != 0 {
				t.Errorf("the refused run wrote %v, want nothing", writes)
			}

			for _, c := range calls {
				if c.Method == "PATCH" {
					t.Errorf("the refused run patched %s, want nothing written", c.Endpoint)
				}

				if c.Endpoint == fixtureBranchEndpoint {
					t.Errorf("the refused run looked up the report's branch, want nothing written")
				}
			}
		})
	}
}

// TestPublishStopsOnARefusedAdvisoryUpdate pins a failed write's place: the
// advisories are written before the report, so a refused update publishes no
// report and files no issue.
func TestPublishStopsOnARefusedAdvisoryUpdate(t *testing.T) {
	t.Parallel()

	listing := []map[string]any{draftEntry(fixtureGHSA, libraryFingerprint, "open")}

	fake := newFakeGH(t, publishRoutesWithAdvisories(
		listing,
		[]Route{{Method: "PATCH", Endpoint: advisoryPatchEndpoint(fixtureGHSA), Status: 500, Body: map[string]string{"message": "the update failed"}}},
	)...)
	client := newFixtureClient(t, fake.Path)
	cfg := publishConfig(t, client, newFixtureGitleaks(t, newFakeGitleaks(t)), fixtureResultFile(t), fixtureReportsDir(t))

	published, err := github.Publish(t.Context(), cfg)

	if !errors.Is(err, publish.ErrGitHub) {
		t.Fatalf("Publish = %+v, %v, want an error wrapping publish.ErrGitHub", published, err)
	}

	for _, c := range fake.Calls(t) {
		switch {
		case c.Method == "POST" && c.Endpoint == fixtureBlobsEndpoint:
			t.Errorf("the refused run published the report's blob, want no report after a failed advisory write")
		case c.Method == "POST" && c.Endpoint == fixturePullsEndpoint:
			t.Errorf("the refused run opened the report's pull request, want no report after a failed advisory write")
		case c.Method == "POST" && c.Endpoint == fixtureIssuesEndpoint:
			t.Errorf("the refused run filed an issue, want none after a failed advisory write")
		}
	}
}

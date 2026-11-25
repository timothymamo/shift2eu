package github_test

// Group L of PR 4c: the draft advisories' GitHub side (PRD #175 milestone 2,
// "Only what the App wrote", "It tracks private findings by fingerprint"; ADR
// 0040, rows 14-16). The listing is read whole or not at all, page after page
// while its Link says there is a next one, and never past the caller's bound.
// Only the audit App's own draft or closed advisory with no credit and no
// collaborator is ever matched, so a PATCH can reach no other advisory: never
// one of another author's, never a triage, published or withdrawn one, never
// one a person is credited on. A write is made once and never retried, and a
// refused create is the run's not-filed path with no further advisory call.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

const (
	// fixtureGHSA2 is another identifier of the advisory rows.
	fixtureGHSA2 = "GHSA-5555-6666-7777"

	// libraryFingerprint is an invented fingerprint of the shape a keyed one
	// has: the summaries of the listing's fixtures hold it.
	libraryFingerprint = publish.Fingerprint("fp-0123456789ab")

	// fixtureDraftBody is a draft description's text without its status line.
	fixtureDraftBody = "Security audit finding, filed by an earlier run.\n"
)

// advisorySummary returns the summary of an advisory holding fp: ADR 0040's
// template, which AdvisoryFingerprint reads a fingerprint from.
func advisorySummary(fp publish.Fingerprint) string {
	return "Audit finding " + string(fp) + " (high, injection)"
}

// draftDescription returns a draft description whose last status line states
// status: the text SetAdvisoryStatus replaces.
func draftDescription(status string) string {
	return fixtureDraftBody + "\nAudit status: " + status + "\n"
}

// advisoryEntry returns one entry of the repository's advisory listing with
// every key the library reads: the audit App's, with no credit and no
// collaborator unless the caller sets one.
func advisoryEntry(ghsa, summary, state, login, kind string) map[string]any {
	return map[string]any{
		"ghsa_id":             ghsa,
		"summary":             summary,
		"description":         draftDescription("open"),
		"state":               state,
		"author":              map[string]any{"login": login, "type": kind},
		"closed_at":           nil,
		"credits":             []any{},
		"credits_detailed":    []any{},
		"collaborating_users": []any{},
		"collaborating_teams": []any{},
	}
}

// auditAdvisoryEntry returns the audit App's entry of the listing.
func auditAdvisoryEntry(ghsa, summary, state string) map[string]any {
	return advisoryEntry(ghsa, summary, state, github.AuditBotLogin, github.AuditBotType)
}

// advisoryListingRoute returns the listing's first page: the entries, and the
// Link header when the page has a next one.
func advisoryListingRoute(entries []map[string]any, link string) Route {
	list := make([]any, 0, len(entries))
	for _, e := range entries {
		list = append(list, e)
	}

	return Route{Method: "GET", Endpoint: fixtureAdvisoriesEndpoint, Status: 200, Body: list, Link: link}
}

// advisoryPageLink returns the Link header of a page that has a next one,
// from the path GitHub writes - repositories/<id>/..., never the repository's.
func advisoryPageLink(cursor string) string {
	return `<https://api.github.com/repositories/1/security-advisories?per_page=100&after=` + cursor + `>; rel="next"`
}

// advisoryPageEndpoint returns the listing's page after cursor.
func advisoryPageEndpoint(cursor string) string {
	return fixtureAdvisoriesEndpoint + "&after=" + cursor
}

// cursorOf returns the cursor of the listing's page i.
func cursorOf(i int) string { return "cursor" + strconv.Itoa(i) }

// advisoryPages returns the routes of the listing's first pages, chained by
// cursors: each page links to the next one unless it is the last and lastLinks
// is false.
func advisoryPages(pages int, lastLinks bool) []Route {
	routes := make([]Route, 0, pages)
	endpoint := fixtureAdvisoriesEndpoint

	for i := 1; i <= pages; i++ {
		link := ""
		if i < pages || lastLinks {
			link = advisoryPageLink(cursorOf(i))
		}

		routes = append(routes, Route{
			Method:   "GET",
			Endpoint: endpoint,
			Status:   200,
			Body:     []any{auditAdvisoryEntry(fixtureGHSA, advisorySummary(libraryFingerprint), github.AdvisoryStateDraft)},
			Link:     link,
		})

		endpoint = advisoryPageEndpoint(cursorOf(i))
	}

	return routes
}

// matchableAdvisory returns an advisory as ListAuditAdvisories maps it: the
// audit App's, with no credit and no collaborator, its summary holding fp.
func matchableAdvisory(ghsa, state string, fp publish.Fingerprint, description string) github.Advisory {
	return github.Advisory{
		GHSAID:      ghsa,
		Summary:     advisorySummary(fp),
		Description: description,
		State:       state,
		AuthorLogin: github.AuditBotLogin,
		AuthorType:  github.AuditBotType,
	}
}

// closedOn returns a day of the fixture month as an advisory's closed_at.
func closedOn(day int) time.Time {
	return time.Date(2026, time.December, day, 0, 0, 0, 0, time.UTC)
}

// libraryFinding returns one finding of the model's result, as the core's
// struct holds it: a confirmed high finding of a private category, which Place
// puts in a draft advisory.
func libraryFinding(rule, severity string) publish.Finding {
	return publish.Finding{
		Check:     "tree-review",
		Rule:      rule,
		File:      "internal/cli/run.go",
		Line:      12,
		Sink:      "os.Open",
		Severity:  severity,
		Confirmed: true,
		Category:  "injection",
		Private:   true,
		Scenario:  "a scenario",
		Evidence:  "an evidence",
		Fix:       "a fix",
	}
}

// libraryPrivateFinding returns the finding with its keyed fingerprint.
func libraryPrivateFinding(t *testing.T, rule string) publish.PrivateFinding {
	t.Helper()

	f := libraryFinding(rule, "high")

	fp, err := publish.KeyedFingerprint(fixtureKey(t), f.Rule, f.File, f.Sink)
	if err != nil {
		t.Fatalf("KeyedFingerprint: %v", err)
	}

	return publish.PrivateFinding{Finding: f, Fingerprint: fp}
}

// wantAdvisoryText returns the text a write of the finding under status sends:
// the core's rendering, which the plan must copy.
func wantAdvisoryText(t *testing.T, f publish.PrivateFinding, status publish.AdvisoryStatus) publish.AdvisoryText {
	t.Helper()

	text, err := publish.RenderAdvisory(f, status, fixtureRun(t))
	if err != nil {
		t.Fatalf("RenderAdvisory(%v): %v", status, err)
	}

	return text
}

// createBody returns the body a create must send: the text, the given severity
// when it is not empty, and one vulnerability of this module.
func createBody(text publish.AdvisoryText) map[string]any {
	body := map[string]any{
		"summary":     text.Summary,
		"description": text.Description,
		"vulnerabilities": []any{map[string]any{
			"package": map[string]any{"ecosystem": "go", "name": "github.com/timothymamo/shift2eu"},
			// The three keys are written and null: GitHub's create wants the
			// element's whole shape, and none of them is the model's.
			"vulnerable_version_range": nil,
			"patched_versions":         nil,
			"vulnerable_functions":     nil,
		}},
	}

	if text.Severity != "" {
		body["severity"] = text.Severity
	}

	return body
}

// openUpdateBody returns the body an update of a reproduced draft must send:
// the text and state draft, never a severity, so a maintainer's choice on the
// draft stands.
func openUpdateBody(text publish.AdvisoryText) map[string]any {
	return map[string]any{"summary": text.Summary, "description": text.Description, "state": github.AdvisoryStateDraft}
}

// notReproducedBody returns the body an update of a draft the run did not
// reproduce must send: the description with its status line replaced, and the
// state.
func notReproducedBody(description string) map[string]any {
	return map[string]any{"description": description, "state": github.AdvisoryStateDraft}
}

// TestListAuditAdvisoriesMapsOnePage pins the listing's one page: every entry
// returned, foreign ones included, each field mapped as it is - a null author
// as no author at all, a null description as none, the credits of both lists
// and the collaborating users and teams counted.
func TestListAuditAdvisoriesMapsOnePage(t *testing.T) {
	t.Parallel()

	credited := advisoryEntry(fixtureGHSA, "a summary", github.AdvisoryStateDraft, github.AuditBotLogin, github.AuditBotType)
	credited["description"] = "a description"
	credited["closed_at"] = "2026-12-01T00:00:00Z"
	credited["credits"] = []any{map[string]any{"login": "someone"}}
	credited["credits_detailed"] = []any{
		map[string]any{"login": "another"},
		map[string]any{"email": "someone@example.com"},
	}
	credited["collaborating_users"] = []any{map[string]any{"login": "a-collaborator"}}
	credited["collaborating_teams"] = []any{map[string]any{"name": "a-team"}}

	noAuthor := advisoryEntry(fixtureGHSA2, "another summary", "published", "someone", "User")
	noAuthor["author"] = nil
	noAuthor["description"] = nil

	fake := newFakeGH(t, advisoryListingRoute([]map[string]any{credited, noAuthor}, ""))
	client := newFixtureClient(t, fake.Path)

	advisories, err := github.ListAuditAdvisories(t.Context(), client)
	if err != nil {
		t.Fatalf("ListAuditAdvisories = %v, want no error", err)
	}

	want := []github.Advisory{
		{
			GHSAID:        fixtureGHSA,
			Summary:       "a summary",
			Description:   "a description",
			State:         github.AdvisoryStateDraft,
			AuthorLogin:   github.AuditBotLogin,
			AuthorType:    github.AuditBotType,
			ClosedAt:      closedOn(1),
			Credits:       3,
			Collaborators: 2,
		},
		{
			GHSAID:      fixtureGHSA2,
			Summary:     "another summary",
			State:       "published",
			AuthorLogin: "",
			AuthorType:  "",
		},
	}

	if diff := cmp.Diff(want, advisories); diff != "" {
		t.Errorf("the listing (-want +got):\n%s", diff)
	}

	if calls := fake.Calls(t); len(calls) != 1 || endpointOf(calls[0]) != "GET "+fixtureAdvisoriesEndpoint {
		t.Errorf("the listing's calls are %v, want the one page", calls)
	}
}

// TestListAuditAdvisoriesFollowsTheCursor pins the pages after the first: the
// next page's endpoint is the fixed one and the Link's after cursor, read from
// GitHub's repositories/<id>/ path, and never the Link's host or path.
func TestListAuditAdvisoriesFollowsTheCursor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		link string
	}{
		{name: "a cursor", link: advisoryPageLink("Y3Vyc29y")},
		{
			name: "a cursor before another parameter",
			link: `<https://api.github.com/repositories/1/security-advisories?per_page=100&after=Y3Vyc29y&page=2>; rel="next"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			second := auditAdvisoryEntry(fixtureGHSA2, "a second summary", "closed")

			fake := newFakeGH(t,
				advisoryListingRoute([]map[string]any{auditAdvisoryEntry(fixtureGHSA, "a summary", github.AdvisoryStateDraft)}, tc.link),
				Route{Method: "GET", Endpoint: advisoryPageEndpoint("Y3Vyc29y"), Status: 200, Body: []any{second}},
			)
			client := newFixtureClient(t, fake.Path)

			advisories, err := github.ListAuditAdvisories(t.Context(), client)
			if err != nil {
				t.Fatalf("ListAuditAdvisories = %v, want no error", err)
			}

			want := []github.Advisory{
				{
					GHSAID:      fixtureGHSA,
					Summary:     "a summary",
					Description: draftDescription("open"),
					State:       github.AdvisoryStateDraft,
					AuthorLogin: github.AuditBotLogin,
					AuthorType:  github.AuditBotType,
				},
				{
					GHSAID:      fixtureGHSA2,
					Summary:     "a second summary",
					Description: draftDescription("open"),
					State:       "closed",
					AuthorLogin: github.AuditBotLogin,
					AuthorType:  github.AuditBotType,
				},
			}

			if diff := cmp.Diff(want, advisories); diff != "" {
				t.Errorf("the listing (-want +got):\n%s", diff)
			}

			var got []string

			for _, c := range fake.Calls(t) {
				got = append(got, endpointOf(c))
			}

			wantCalls := []string{"GET " + fixtureAdvisoriesEndpoint, "GET " + advisoryPageEndpoint("Y3Vyc29y")}

			if diff := cmp.Diff(wantCalls, got); diff != "" {
				t.Errorf("the listing's calls (-want +got):\n%s", diff)
			}
		})
	}
}

// TestListAuditAdvisoriesFailsClosedOverItsPages pins the caller's bound: ten
// pages are read, and a tenth page that still says there is a next one fails
// closed - the eleventh page is never requested.
func TestListAuditAdvisoriesFailsClosedOverItsPages(t *testing.T) {
	t.Parallel()

	if github.MaxAdvisoryPages != 10 {
		t.Errorf("MaxAdvisoryPages = %d, want 10", github.MaxAdvisoryPages)
	}

	t.Run("ten pages, the last without a next one", func(t *testing.T) {
		t.Parallel()

		fake := newFakeGH(t, advisoryPages(github.MaxAdvisoryPages, false)...)
		client := newFixtureClient(t, fake.Path)

		advisories, err := github.ListAuditAdvisories(t.Context(), client)
		if err != nil {
			t.Fatalf("ListAuditAdvisories = %v, want no error", err)
		}

		if len(advisories) != github.MaxAdvisoryPages {
			t.Errorf("the listing holds %d entr(y/ies), want one per page", len(advisories))
		}

		if calls := fake.Calls(t); len(calls) != github.MaxAdvisoryPages {
			t.Errorf("the listing read %d page(s), want %d", len(calls), github.MaxAdvisoryPages)
		}
	})

	t.Run("a tenth page that still says there is a next one", func(t *testing.T) {
		t.Parallel()

		routes := advisoryPages(github.MaxAdvisoryPages, true)
		// The eleventh page exists and is never read.
		routes = append(routes, Route{
			Method:   "GET",
			Endpoint: advisoryPageEndpoint(cursorOf(github.MaxAdvisoryPages)),
			Status:   200,
			Body:     []any{},
		})

		fake := newFakeGH(t, routes...)
		client := newFixtureClient(t, fake.Path)

		advisories, err := github.ListAuditAdvisories(t.Context(), client)

		if !errors.Is(err, publish.ErrGitHub) {
			t.Fatalf("ListAuditAdvisories over its pages = %+v, %v, want an error wrapping publish.ErrGitHub", advisories, err)
		}

		if advisories != nil {
			t.Errorf("the listing is %+v, want nothing: a listing over the cap is never truncated", advisories)
		}

		if calls := fake.Calls(t); len(calls) != github.MaxAdvisoryPages {
			t.Errorf("the listing read %d page(s), want exactly %d: the eleventh is never requested", len(calls), github.MaxAdvisoryPages)
		}
	})
}

// TestListAuditAdvisoriesRefusesWhatIsNoListing pins every answer the listing
// refuses: a failed call, a body that is no array, an entry whose fields are
// not the listing's, and a next link without a usable cursor - which GitHub's
// own failure is, never the endpoint table's refusal.
func TestListAuditAdvisoriesRefusesWhatIsNoListing(t *testing.T) {
	t.Parallel()

	valid := func() map[string]any {
		return auditAdvisoryEntry(fixtureGHSA, "a summary", github.AdvisoryStateDraft)
	}

	for _, tc := range []struct {
		name string
		// route answers the listing's first page.
		route Route
	}{
		{
			name:  "a failed call",
			route: Route{Method: "GET", Endpoint: fixtureAdvisoriesEndpoint, Status: 500, Body: map[string]string{"message": "the listing failed"}},
		},
		{
			name: "a body that is no array",
			// An object: an empty array is the listing with no advisory, which
			// a run with no advisory at all must accept.
			route: Route{Method: "GET", Endpoint: fixtureAdvisoriesEndpoint, Status: 200, Body: map[string]string{"message": "not a listing"}},
		},
		{
			name:  "an entry without ghsa_id",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWithout("ghsa_id")}, ""),
		},
		{
			name:  "an entry whose summary is no string",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("summary", 7)}, ""),
		},
		{
			name:  "an entry whose state is no string",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("state", 7)}, ""),
		},
		{
			name:  "an entry whose description is no string",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("description", 7)}, ""),
		},
		{
			name:  "an entry whose author is no object",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("author", "someone")}, ""),
		},
		{
			name:  "an entry whose closed_at is no RFC 3339",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("closed_at", "yesterday")}, ""),
		},
		{
			name:  "an entry whose credits are no array",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("credits", "someone")}, ""),
		},
		{
			name:  "an entry whose credits_detailed are no array",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("credits_detailed", "someone")}, ""),
		},
		{
			name:  "an entry whose collaborating users are no array",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("collaborating_users", "a-collaborator")}, ""),
		},
		{
			name:  "an entry whose collaborating teams are no array",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("collaborating_teams", "a-team")}, ""),
		},
		// A credit or collaborator key the answer lacks, or one that is null,
		// is no empty list: counted as none of them it would fail open on an
		// advisory a person is on, which the run would then match and PATCH.
		{
			name:  "an entry without credits",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWithout("credits")}, ""),
		},
		{
			name:  "an entry whose credits are null",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("credits", nil)}, ""),
		},
		{
			name:  "an entry without credits_detailed",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWithout("credits_detailed")}, ""),
		},
		{
			name:  "an entry whose credits_detailed are null",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("credits_detailed", nil)}, ""),
		},
		{
			name:  "an entry without collaborating users",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWithout("collaborating_users")}, ""),
		},
		{
			name:  "an entry whose collaborating users are null",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("collaborating_users", nil)}, ""),
		},
		{
			name:  "an entry without collaborating teams",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWithout("collaborating_teams")}, ""),
		},
		{
			name:  "an entry whose collaborating teams are null",
			route: advisoryListingRoute([]map[string]any{advisoryEntryWith("collaborating_teams", nil)}, ""),
		},
		{
			name: "a next link without a cursor",
			route: advisoryListingRoute([]map[string]any{valid()},
				`<https://api.github.com/repositories/1/security-advisories?per_page=100>; rel="next"`),
		},
		{
			name: "a next link with a cursor holding a space",
			route: advisoryListingRoute([]map[string]any{valid()},
				`<https://api.github.com/repositories/1/security-advisories?per_page=100&after=a b>; rel="next"`),
		},
		{
			name:  "a next link with a cursor over 200 characters",
			route: advisoryListingRoute([]map[string]any{valid()}, advisoryPageLink(strings.Repeat("a", 201))),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, tc.route)
			client := newFixtureClient(t, fake.Path)

			advisories, err := github.ListAuditAdvisories(t.Context(), client)

			if !errors.Is(err, publish.ErrGitHub) {
				t.Fatalf("ListAuditAdvisories = %+v, %v, want an error wrapping publish.ErrGitHub", advisories, err)
			}

			if advisories != nil {
				t.Errorf("the refused listing is %+v, want nothing", advisories)
			}

			if calls := fake.Calls(t); len(calls) != 1 {
				t.Errorf("the refused listing made %d call(s), want 1: a refused cursor is refused before the next page", len(calls))
			}
		})
	}
}

// advisoryEntryWith returns one valid listing entry with one field set to value.
func advisoryEntryWith(name string, value any) map[string]any {
	entry := auditAdvisoryEntry(fixtureGHSA, "a summary", github.AdvisoryStateDraft)
	entry[name] = value

	return entry
}

// advisoryEntryWithout returns one valid listing entry without one field.
func advisoryEntryWithout(name string) map[string]any {
	entry := auditAdvisoryEntry(fixtureGHSA, "a summary", github.AdvisoryStateDraft)
	delete(entry, name)

	return entry
}

// TestPlanAdvisoriesNeverMatchesAForeignAdvisory pins the match: only the
// audit App's own draft or closed advisory, whose ID the PATCH row accepts and
// with no credit and no collaborator, is ever matched - so a PATCH can never
// reach a triage, published or withdrawn advisory, another author's, one a
// person is on, or one whose ID the row would refuse. Each fixture holds the
// finding's fingerprint, so a match would be visible as an update; with no
// finding at all it would be visible as a not-reproduced mark.
func TestPlanAdvisoriesNeverMatchesAForeignAdvisory(t *testing.T) {
	t.Parallel()

	finding := libraryPrivateFinding(t, "G309")
	fp := finding.Fingerprint

	base := func() github.Advisory {
		return matchableAdvisory(fixtureGHSA, github.AdvisoryStateDraft, fp, draftDescription("open"))
	}

	for _, tc := range []struct {
		name     string
		advisory func() github.Advisory
	}{
		{
			name: "a private vulnerability report",
			advisory: func() github.Advisory {
				a := base()
				a.State = "triage"

				return a
			},
		},
		{
			name: "a published advisory",
			advisory: func() github.Advisory {
				a := base()
				a.State = "published"

				return a
			},
		},
		{
			name: "a withdrawn advisory",
			advisory: func() github.Advisory {
				a := base()
				a.State = "withdrawn"

				return a
			},
		},
		{
			name: "another author's draft",
			advisory: func() github.Advisory {
				a := base()
				a.AuthorLogin = "someone"

				return a
			},
		},
		{
			name: "a draft whose login is the App's but whose type is a user's",
			advisory: func() github.Advisory {
				a := base()
				a.AuthorType = "User"

				return a
			},
		},
		{
			name: "a draft with a credit",
			advisory: func() github.Advisory {
				a := base()
				a.Credits = 1

				return a
			},
		},
		{
			name: "a draft with a collaborating user",
			advisory: func() github.Advisory {
				a := base()
				a.Collaborators = 1

				return a
			},
		},
		{
			name: "a draft whose GHSA ID holds a group of zeros",
			advisory: func() github.Advisory {
				a := base()
				a.GHSAID = "GHSA-0000-0000-0000"

				return a
			},
		},
		{
			name: "a draft whose GHSA ID is lowercase",
			advisory: func() github.Advisory {
				a := base()
				a.GHSAID = "ghsa-2222-3333-4444"

				return a
			},
		},
		{
			name: "a draft whose GHSA ID's last group is short",
			advisory: func() github.Advisory {
				a := base()
				a.GHSAID = "GHSA-2222-3333-444"

				return a
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plan, err := github.PlanAdvisories([]publish.PrivateFinding{finding}, []github.Advisory{tc.advisory()}, fixtureRun(t))
			if err != nil {
				t.Fatalf("PlanAdvisories = %v, want no error", err)
			}

			want := []github.AdvisoryWrite{{
				Action: github.AdvisoryCreate,
				Status: publish.AdvisoryNew,
				Text:   wantAdvisoryText(t, finding, publish.AdvisoryNew),
			}}

			if diff := cmp.Diff(want, plan.Writes()); diff != "" {
				t.Errorf("the plan's writes (-want +got):\n%s", diff)
			}

			if !plan.Reproduced() {
				t.Errorf("the plan does not report the finding reproduced, want it to: a new draft is filed")
			}
		})

		t.Run(tc.name+", with no finding", func(t *testing.T) {
			t.Parallel()

			plan, err := github.PlanAdvisories(nil, []github.Advisory{tc.advisory()}, fixtureRun(t))
			if err != nil {
				t.Fatalf("PlanAdvisories = %v, want no error", err)
			}

			if writes := plan.Writes(); len(writes) != 0 {
				t.Errorf("the plan's writes are %+v, want none: a foreign advisory is never marked not reproduced", writes)
			}

			if plan.Reproduced() {
				t.Errorf("the plan reports a private finding reproduced, want none: the run has no finding")
			}
		})
	}
}

// TestPlanAdvisoriesUpdatesTheAppDraft pins the match: the App's own draft
// holding the finding's fingerprint is updated, open, with the run's text.
func TestPlanAdvisoriesUpdatesTheAppDraft(t *testing.T) {
	t.Parallel()

	finding := libraryPrivateFinding(t, "G309")

	existing := []github.Advisory{
		// A closed one and another draft of no fingerprint's finding come
		// first, so the match is by fingerprint and never by position.
		func() github.Advisory {
			a := matchableAdvisory(fixtureGHSA2, "closed", libraryFingerprint, draftDescription("open"))
			a.ClosedAt = closedOn(3)

			return a
		}(),
		matchableAdvisory(fixtureGHSA, github.AdvisoryStateDraft, finding.Fingerprint, draftDescription("open")),
	}

	plan, err := github.PlanAdvisories([]publish.PrivateFinding{finding}, existing, fixtureRun(t))
	if err != nil {
		t.Fatalf("PlanAdvisories = %v, want no error", err)
	}

	want := []github.AdvisoryWrite{{
		Action: github.AdvisoryUpdate,
		GHSAID: fixtureGHSA,
		Status: publish.AdvisoryOpen,
		Text:   wantAdvisoryText(t, finding, publish.AdvisoryOpen),
	}}

	if diff := cmp.Diff(want, plan.Writes()); diff != "" {
		t.Errorf("the plan's writes (-want +got):\n%s", diff)
	}
}

// TestPlanAdvisoriesCreatesANewDraft pins the first filing: no advisory holds
// the finding's fingerprint, so the run creates one, new.
func TestPlanAdvisoriesCreatesANewDraft(t *testing.T) {
	t.Parallel()

	finding := libraryPrivateFinding(t, "G309")

	for _, tc := range []struct {
		name     string
		existing []github.Advisory
		// after are the writes the plan makes after the finding's create: the
		// other fingerprint's draft is never this finding's draft, so it is
		// only marked not reproduced.
		after []github.AdvisoryWrite
	}{
		{name: "no advisory at all"},
		{
			name: "another fingerprint's draft",
			existing: []github.Advisory{
				matchableAdvisory(fixtureGHSA2, github.AdvisoryStateDraft, libraryFingerprint, draftDescription("open")),
			},
			after: []github.AdvisoryWrite{{
				Action: github.AdvisoryUpdate,
				GHSAID: fixtureGHSA2,
				Status: publish.AdvisoryNotReproduced,
				Text:   publish.AdvisoryText{Description: draftDescription("not reproduced")},
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plan, err := github.PlanAdvisories([]publish.PrivateFinding{finding}, tc.existing, fixtureRun(t))
			if err != nil {
				t.Fatalf("PlanAdvisories = %v, want no error", err)
			}

			want := append([]github.AdvisoryWrite{{
				Action: github.AdvisoryCreate,
				Status: publish.AdvisoryNew,
				Text:   wantAdvisoryText(t, finding, publish.AdvisoryNew),
			}}, tc.after...)

			if diff := cmp.Diff(want, plan.Writes()); diff != "" {
				t.Errorf("the plan's writes (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPlanAdvisoriesMarksADraftNotReproduced pins the other half of the plan:
// a draft whose fingerprint no finding of the run holds is marked not
// reproduced by replacing only the listing's last status line. A draft that
// already says so is written no more, and a draft whose description holds no
// status line - a maintainer's rewrite - is skipped rather than refused, so a
// rewrite can never stop the run. A closed advisory is never written at all,
// whichever status its description states: a PATCH with state draft would
// reopen it.
func TestPlanAdvisoriesMarksADraftNotReproduced(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// state is the listing entry's state, draft when empty.
		state       string
		description string
		want        []github.AdvisoryWrite
	}{
		{
			name:        "a draft that says open",
			description: draftDescription("open"),
			want: []github.AdvisoryWrite{{
				Action: github.AdvisoryUpdate,
				GHSAID: fixtureGHSA,
				Status: publish.AdvisoryNotReproduced,
				Text:   publish.AdvisoryText{Description: draftDescription("not reproduced")},
			}},
		},
		{
			name:        "a draft that already says not reproduced",
			description: draftDescription("not reproduced"),
		},
		{
			name:        "a draft without a status line",
			description: "a maintainer's own description, with no status line\n",
		},
		{
			// A closed one with no finding holding its fingerprint: an earlier
			// run's advisory, whose mark would reopen it.
			name:        "a closed advisory that says open",
			state:       github.AdvisoryStateClosed,
			description: draftDescription("open"),
		},
		{
			name:        "a closed advisory that says new",
			state:       github.AdvisoryStateClosed,
			description: draftDescription("new"),
		},
		{
			name:        "a closed advisory that says regressed",
			state:       github.AdvisoryStateClosed,
			description: draftDescription("regressed"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			state := tc.state
			if state == "" {
				state = github.AdvisoryStateDraft
			}

			existing := []github.Advisory{
				matchableAdvisory(fixtureGHSA, state, libraryFingerprint, tc.description),
			}

			plan, err := github.PlanAdvisories(nil, existing, fixtureRun(t))
			if err != nil {
				t.Fatalf("PlanAdvisories = %v, want no error", err)
			}

			if diff := cmp.Diff(tc.want, plan.Writes()); diff != "" {
				t.Errorf("the plan's writes (-want +got):\n%s", diff)
			}

			if plan.Reproduced() {
				t.Errorf("the plan reports a private finding reproduced, want none: the run has no finding")
			}
		})
	}
}

// TestPlanAdvisoriesPrefersTheDraftToTheClosed pins the precedence of a draft
// over a closed advisory of the same fingerprint, in either listing order: the
// draft is updated and the closed one never written.
func TestPlanAdvisoriesPrefersTheDraftToTheClosed(t *testing.T) {
	t.Parallel()

	finding := libraryPrivateFinding(t, "G309")

	draft := matchableAdvisory(fixtureGHSA, github.AdvisoryStateDraft, finding.Fingerprint, draftDescription("open"))

	closed := matchableAdvisory(fixtureGHSA2, "closed", finding.Fingerprint, draftDescription("not reproduced"))
	closed.ClosedAt = closedOn(2)

	for _, tc := range []struct {
		name     string
		existing []github.Advisory
	}{
		{name: "the draft first", existing: []github.Advisory{draft, closed}},
		{name: "the closed one first", existing: []github.Advisory{closed, draft}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plan, err := github.PlanAdvisories([]publish.PrivateFinding{finding}, tc.existing, fixtureRun(t))
			if err != nil {
				t.Fatalf("PlanAdvisories = %v, want no error", err)
			}

			want := []github.AdvisoryWrite{{
				Action: github.AdvisoryUpdate,
				GHSAID: fixtureGHSA,
				Status: publish.AdvisoryOpen,
				Text:   wantAdvisoryText(t, finding, publish.AdvisoryOpen),
			}}

			if diff := cmp.Diff(want, plan.Writes()); diff != "" {
				t.Errorf("the plan's writes (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPlanAdvisoriesRegressedOrDismissed pins a recurrence with no draft: a
// finding whose most recently closed advisory an earlier run marked not
// reproduced is filed anew, regressed, and one whose most recent closed
// advisory says anything else is dismissed with no write. The most recent is
// read by its closed_at, never by the listing's order, and a tie is the
// listing's first.
func TestPlanAdvisoriesRegressedOrDismissed(t *testing.T) {
	t.Parallel()

	finding := libraryPrivateFinding(t, "G309")

	closedWith := func(ghsa, status string, day int) github.Advisory {
		a := matchableAdvisory(ghsa, "closed", finding.Fingerprint, draftDescription(status))
		a.ClosedAt = closedOn(day)

		return a
	}

	for _, tc := range []struct {
		name     string
		existing []github.Advisory
		// wantStatus is the status of the one create, empty for none.
		wantStatus publish.AdvisoryStatus
	}{
		{
			name:       "the most recent closed one says not reproduced",
			existing:   []github.Advisory{closedWith(fixtureGHSA2, "open", 5), closedWith(fixtureGHSA, "not reproduced", 10)},
			wantStatus: publish.AdvisoryRegressed,
		},
		{
			name:     "the most recent closed one says open",
			existing: []github.Advisory{closedWith(fixtureGHSA2, "not reproduced", 5), closedWith(fixtureGHSA, "open", 10)},
		},
		{
			name:       "the reverse listing order is no tie-breaker",
			existing:   []github.Advisory{closedWith(fixtureGHSA, "not reproduced", 10), closedWith(fixtureGHSA2, "open", 5)},
			wantStatus: publish.AdvisoryRegressed,
		},
		{
			name:       "a tie of two closed ones, the first not reproduced",
			existing:   []github.Advisory{closedWith(fixtureGHSA, "not reproduced", 10), closedWith(fixtureGHSA2, "open", 10)},
			wantStatus: publish.AdvisoryRegressed,
		},
		{
			name:     "a tie of two closed ones, the first open",
			existing: []github.Advisory{closedWith(fixtureGHSA, "open", 10), closedWith(fixtureGHSA2, "not reproduced", 10)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plan, err := github.PlanAdvisories([]publish.PrivateFinding{finding}, tc.existing, fixtureRun(t))
			if err != nil {
				t.Fatalf("PlanAdvisories = %v, want no error", err)
			}

			if !plan.Reproduced() {
				t.Errorf("the plan reports no private finding reproduced, want the one finding: a dismissal is no drop")
			}

			if tc.wantStatus == 0 {
				if writes := plan.Writes(); len(writes) != 0 {
					t.Errorf("the plan's writes are %+v, want none: the finding is dismissed", writes)
				}

				return
			}

			want := []github.AdvisoryWrite{{
				Action: github.AdvisoryCreate,
				Status: tc.wantStatus,
				Text:   wantAdvisoryText(t, finding, tc.wantStatus),
			}}

			if diff := cmp.Diff(want, plan.Writes()); diff != "" {
				t.Errorf("the plan's writes (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPlanAdvisoriesReproduced pins what a run's report states: it reproduced a
// private finding when there is one, a dismissed one included, and none when
// the run has no private finding.
func TestPlanAdvisoriesReproduced(t *testing.T) {
	t.Parallel()

	t.Run("no private finding", func(t *testing.T) {
		t.Parallel()

		plan, err := github.PlanAdvisories(nil, nil, fixtureRun(t))
		if err != nil {
			t.Fatalf("PlanAdvisories = %v, want no error", err)
		}

		if plan.Reproduced() {
			t.Errorf("the plan reports a private finding reproduced, want none")
		}

		if writes := plan.Writes(); len(writes) != 0 {
			t.Errorf("the plan's writes are %+v, want none", writes)
		}
	})

	t.Run("a dismissed finding", func(t *testing.T) {
		t.Parallel()

		finding := libraryPrivateFinding(t, "G309")

		dismissed := matchableAdvisory(fixtureGHSA, "closed", finding.Fingerprint, draftDescription("open"))
		dismissed.ClosedAt = closedOn(4)

		plan, err := github.PlanAdvisories([]publish.PrivateFinding{finding}, []github.Advisory{dismissed}, fixtureRun(t))
		if err != nil {
			t.Fatalf("PlanAdvisories = %v, want no error", err)
		}

		if !plan.Reproduced() {
			t.Errorf("the plan reports no private finding reproduced, want the dismissed one")
		}

		if writes := plan.Writes(); len(writes) != 0 {
			t.Errorf("the plan's writes are %+v, want none", writes)
		}
	})
}

// TestPlanAdvisoriesRefusesTwoDraftsOfOneFingerprint pins the duplicate: two of
// the App's own drafts holding one fingerprint are no plan at all, so the run
// writes nothing - whatever the findings, even with none holding that
// fingerprint - and ends at the runbook's exit 6. A foreign draft of the same
// fingerprint is no duplicate, since it is never matched.
func TestPlanAdvisoriesRefusesTwoDraftsOfOneFingerprint(t *testing.T) {
	t.Parallel()

	finding := libraryPrivateFinding(t, "G309")

	draft := func(ghsa string) github.Advisory {
		return matchableAdvisory(ghsa, github.AdvisoryStateDraft, finding.Fingerprint, draftDescription("open"))
	}

	foreign := draft(fixtureGHSA2)
	foreign.AuthorLogin = "someone"

	for _, tc := range []struct {
		name     string
		findings []publish.PrivateFinding
		existing []github.Advisory
	}{
		{name: "two drafts and the finding", findings: []publish.PrivateFinding{finding}, existing: []github.Advisory{draft(fixtureGHSA), draft(fixtureGHSA2)}},
		{name: "two drafts and no finding", existing: []github.Advisory{draft(fixtureGHSA), draft(fixtureGHSA2)}},
		{
			name:     "two drafts of a fingerprint no finding holds",
			findings: []publish.PrivateFinding{finding},
			existing: []github.Advisory{
				matchableAdvisory(fixtureGHSA, github.AdvisoryStateDraft, libraryFingerprint, draftDescription("open")),
				matchableAdvisory(fixtureGHSA2, github.AdvisoryStateDraft, libraryFingerprint, draftDescription("open")),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plan, err := github.PlanAdvisories(tc.findings, tc.existing, fixtureRun(t))

			if !errors.Is(err, publish.ErrDuplicateDrafts) {
				t.Fatalf("PlanAdvisories = %+v, %v, want an error wrapping publish.ErrDuplicateDrafts", plan.Writes(), err)
			}

			if writes := plan.Writes(); len(writes) != 0 {
				t.Errorf("the duplicate plan's writes are %+v, want none", writes)
			}
		})
	}

	t.Run("a draft and a foreign draft of one fingerprint", func(t *testing.T) {
		t.Parallel()

		plan, err := github.PlanAdvisories([]publish.PrivateFinding{finding}, []github.Advisory{foreign, draft(fixtureGHSA)}, fixtureRun(t))
		if err != nil {
			t.Fatalf("PlanAdvisories = %v, want no error: a foreign draft is never matched", err)
		}

		want := []github.AdvisoryWrite{{
			Action: github.AdvisoryUpdate,
			GHSAID: fixtureGHSA,
			Status: publish.AdvisoryOpen,
			Text:   wantAdvisoryText(t, finding, publish.AdvisoryOpen),
		}}

		if diff := cmp.Diff(want, plan.Writes()); diff != "" {
			t.Errorf("the plan's writes (-want +got):\n%s", diff)
		}
	})
}

// TestFileAdvisoriesSendsTheFixedBodies pins every body a write sends: a create
// with the finding's severity and one vulnerability of this module and no
// state, an update of a reproduced draft with the text and state draft and no
// severity, an update of a draft not reproduced with the listing's description
// and the state, and the plan's order.
func TestFileAdvisoriesSendsTheFixedBodies(t *testing.T) {
	t.Parallel()

	t.Run("a create with a known severity", func(t *testing.T) {
		t.Parallel()

		finding := libraryPrivateFinding(t, "G309")
		plan := planOf(t, []publish.PrivateFinding{finding}, nil)

		fake := newFakeGH(t, Route{Method: "POST", Endpoint: fixtureRepoPrefix + "/security-advisories", Status: 201, Body: map[string]any{"ghsa_id": fixtureGHSA}})
		client := newFixtureClient(t, fake.Path)

		if err := github.FileAdvisories(t.Context(), client, plan); err != nil {
			t.Fatalf("FileAdvisories = %v, want no error", err)
		}

		got := recordedBodies[map[string]any](t, fake.Calls(t), "POST", fixtureRepoPrefix+"/security-advisories")
		if len(got) != 1 {
			t.Fatalf("the run made %d create(s), want 1", len(got))
		}

		if diff := cmp.Diff(createBody(wantAdvisoryText(t, finding, publish.AdvisoryNew)), got[0]); diff != "" {
			t.Errorf("the create's body (-want +got):\n%s", diff)
		}
	})

	t.Run("a create without a known severity", func(t *testing.T) {
		t.Parallel()

		f := libraryFinding("G309", "severe")

		fp, err := publish.KeyedFingerprint(fixtureKey(t), f.Rule, f.File, f.Sink)
		if err != nil {
			t.Fatalf("KeyedFingerprint: %v", err)
		}

		finding := publish.PrivateFinding{Finding: f, Fingerprint: fp}
		plan := planOf(t, []publish.PrivateFinding{finding}, nil)

		fake := newFakeGH(t, Route{Method: "POST", Endpoint: fixtureRepoPrefix + "/security-advisories", Status: 201, Body: map[string]any{"ghsa_id": fixtureGHSA}})
		client := newFixtureClient(t, fake.Path)

		if err := github.FileAdvisories(t.Context(), client, plan); err != nil {
			t.Fatalf("FileAdvisories = %v, want no error", err)
		}

		got := recordedBodies[map[string]any](t, fake.Calls(t), "POST", fixtureRepoPrefix+"/security-advisories")
		if len(got) != 1 {
			t.Fatalf("the run made %d create(s), want 1", len(got))
		}

		if _, ok := got[0]["severity"]; ok {
			t.Errorf("the create's body holds a severity %v, want none: the finding's is unknown", got[0]["severity"])
		}

		if diff := cmp.Diff(createBody(wantAdvisoryText(t, finding, publish.AdvisoryNew)), got[0]); diff != "" {
			t.Errorf("the create's body (-want +got):\n%s", diff)
		}
	})

	t.Run("the plan's order", func(t *testing.T) {
		t.Parallel()

		reproduced := libraryPrivateFinding(t, "G309")
		fresh := libraryPrivateFinding(t, "G310")

		existing := []github.Advisory{
			matchableAdvisory(fixtureGHSA, github.AdvisoryStateDraft, reproduced.Fingerprint, draftDescription("open")),
			matchableAdvisory(fixtureGHSA2, github.AdvisoryStateDraft, libraryFingerprint, draftDescription("open")),
		}

		plan := planOf(t, []publish.PrivateFinding{reproduced, fresh}, existing)

		fake := newFakeGH(t,
			Route{Method: "POST", Endpoint: fixtureRepoPrefix + "/security-advisories", Status: 201, Body: map[string]any{"ghsa_id": "GHSA-8888-9999-cccc"}},
			Route{Method: "PATCH", Endpoint: advisoryPatchEndpoint(fixtureGHSA), Status: 200, Body: map[string]any{"ghsa_id": fixtureGHSA}},
			Route{Method: "PATCH", Endpoint: advisoryPatchEndpoint(fixtureGHSA2), Status: 200, Body: map[string]any{"ghsa_id": fixtureGHSA2}},
		)
		client := newFixtureClient(t, fake.Path)

		if err := github.FileAdvisories(t.Context(), client, plan); err != nil {
			t.Fatalf("FileAdvisories = %v, want no error", err)
		}

		calls := fake.Calls(t)

		var got []string
		for _, c := range calls {
			got = append(got, endpointOf(c))
		}

		want := []string{
			"PATCH " + advisoryPatchEndpoint(fixtureGHSA),
			"POST " + fixtureRepoPrefix + "/security-advisories",
			"PATCH " + advisoryPatchEndpoint(fixtureGHSA2),
		}

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("the writes' order (-want +got):\n%s", diff)
		}

		if body := recordedBodies[map[string]any](t, calls, "PATCH", advisoryPatchEndpoint(fixtureGHSA)); len(body) != 1 ||
			cmp.Diff(openUpdateBody(wantAdvisoryText(t, reproduced, publish.AdvisoryOpen)), body[0]) != "" {
			t.Errorf("the open update's body is %+v, want %+v", body, openUpdateBody(wantAdvisoryText(t, reproduced, publish.AdvisoryOpen)))
		}

		if body := recordedBodies[map[string]any](t, calls, "PATCH", advisoryPatchEndpoint(fixtureGHSA2)); len(body) != 1 ||
			cmp.Diff(notReproducedBody(draftDescription("not reproduced")), body[0]) != "" {
			t.Errorf("the not-reproduced update's body is %+v, want %+v", body, notReproducedBody(draftDescription("not reproduced")))
		}
	})
}

// advisoryPatchEndpoint returns the endpoint of one advisory's update.
func advisoryPatchEndpoint(ghsa string) string {
	return fixtureRepoPrefix + "/security-advisories/" + ghsa
}

// planOf returns the plan of a run's findings and listing, and fails the test
// when the core refuses one.
func planOf(t *testing.T, findings []publish.PrivateFinding, existing []github.Advisory) github.AdvisoryPlan {
	t.Helper()

	plan, err := github.PlanAdvisories(findings, existing, fixtureRun(t))
	if err != nil {
		t.Fatalf("PlanAdvisories: %v", err)
	}

	return plan
}

// TestFileAdvisoriesRefusesACreation pins the not-filed path: a create GitHub
// refuses with 403, 404 or 422 is the run's private findings not filed, and no
// advisory call follows it - the later create and the update are never made.
func TestFileAdvisoriesRefusesACreation(t *testing.T) {
	t.Parallel()

	for _, status := range []int{403, 404, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()

			first := libraryPrivateFinding(t, "G309")
			second := libraryPrivateFinding(t, "G310")

			existing := []github.Advisory{
				matchableAdvisory(fixtureGHSA, github.AdvisoryStateDraft, libraryFingerprint, draftDescription("open")),
			}

			plan := planOf(t, []publish.PrivateFinding{first, second}, existing)

			fake := newFakeGH(t,
				Route{Method: "POST", Endpoint: fixtureRepoPrefix + "/security-advisories", Status: status, Body: map[string]string{"message": "refused"}},
				Route{Method: "PATCH", Endpoint: advisoryPatchEndpoint(fixtureGHSA), Status: 200, Body: map[string]any{"ghsa_id": fixtureGHSA}},
			)
			client := newFixtureClient(t, fake.Path)

			err := github.FileAdvisories(t.Context(), client, plan)
			if !errors.Is(err, publish.ErrPrivateNotFiled) {
				t.Fatalf("FileAdvisories = %v, want an error wrapping publish.ErrPrivateNotFiled", err)
			}

			calls := fake.Calls(t)
			if len(calls) != 1 || endpointOf(calls[0]) != "POST "+fixtureRepoPrefix+"/security-advisories" {
				t.Errorf("the refused run's calls are %v, want the one refused create and nothing after it", calls)
			}
		})
	}
}

// TestFileAdvisoriesRefusesACreationAfterAWrite pins what a refused create is
// worth once the run has written: only a create refused before any advisory
// write of the run is the private findings not filed - the first write, as
// TestFileAdvisoriesRefusesACreation pins. A run whose earlier create or update
// went through has filed part of its findings, so the refusal is GitHub's
// failure, never "not filed", and no advisory call follows it.
func TestFileAdvisoriesRefusesACreationAfterAWrite(t *testing.T) {
	t.Parallel()

	create := "POST " + fixtureRepoPrefix + "/security-advisories"

	t.Run("a create answered 201, then a create refused", func(t *testing.T) {
		t.Parallel()

		first := libraryPrivateFinding(t, "G309")
		second := libraryPrivateFinding(t, "G310")

		for _, status := range []int{403, 422} {
			t.Run(fmt.Sprint(status), func(t *testing.T) {
				t.Parallel()

				plan := planOf(t, []publish.PrivateFinding{first, second}, nil)

				fake := newFakeGH(t,
					Route{Method: "POST", Endpoint: fixtureRepoPrefix + "/security-advisories", Status: 201, Body: map[string]any{"ghsa_id": fixtureGHSA}},
					Route{Method: "POST", Endpoint: fixtureRepoPrefix + "/security-advisories", Status: status, Body: map[string]string{"message": "refused"}},
				)
				client := newFixtureClient(t, fake.Path)

				err := github.FileAdvisories(t.Context(), client, plan)

				if errors.Is(err, publish.ErrPrivateNotFiled) {
					t.Fatalf("FileAdvisories = %v, want no publish.ErrPrivateNotFiled: the run's first draft was written", err)
				}

				if !errors.Is(err, publish.ErrGitHub) {
					t.Fatalf("FileAdvisories = %v, want an error wrapping publish.ErrGitHub", err)
				}

				var got []string
				for _, c := range fake.Calls(t) {
					got = append(got, endpointOf(c))
				}

				if diff := cmp.Diff([]string{create, create}, got); diff != "" {
					t.Errorf("the refused run's calls (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("an update answered 200, then a create refused", func(t *testing.T) {
		t.Parallel()

		first := libraryPrivateFinding(t, "G309")
		second := libraryPrivateFinding(t, "G310")

		existing := []github.Advisory{
			matchableAdvisory(fixtureGHSA, github.AdvisoryStateDraft, first.Fingerprint, draftDescription("open")),
		}

		plan := planOf(t, []publish.PrivateFinding{first, second}, existing)

		fake := newFakeGH(t,
			Route{Method: "PATCH", Endpoint: advisoryPatchEndpoint(fixtureGHSA), Status: 200, Body: map[string]any{"ghsa_id": fixtureGHSA}},
			Route{Method: "POST", Endpoint: fixtureRepoPrefix + "/security-advisories", Status: 403, Body: map[string]string{"message": "refused"}},
		)
		client := newFixtureClient(t, fake.Path)

		err := github.FileAdvisories(t.Context(), client, plan)

		if errors.Is(err, publish.ErrPrivateNotFiled) {
			t.Fatalf("FileAdvisories = %v, want no publish.ErrPrivateNotFiled: the run's update went through first", err)
		}

		if !errors.Is(err, publish.ErrGitHub) {
			t.Fatalf("FileAdvisories = %v, want an error wrapping publish.ErrGitHub", err)
		}

		var got []string
		for _, c := range fake.Calls(t) {
			got = append(got, endpointOf(c))
		}

		want := []string{"PATCH " + advisoryPatchEndpoint(fixtureGHSA), create}

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("the refused run's calls (-want +got):\n%s", diff)
		}
	})
}

// TestFileAdvisoriesStopsOnAFailedWrite pins every other failure: a create
// answered 500 and an update answered 403 are GitHub's failure, and no call
// follows the first one that failed.
func TestFileAdvisoriesStopsOnAFailedWrite(t *testing.T) {
	t.Parallel()

	t.Run("a create answered 500", func(t *testing.T) {
		t.Parallel()

		first := libraryPrivateFinding(t, "G309")
		second := libraryPrivateFinding(t, "G310")

		plan := planOf(t, []publish.PrivateFinding{first, second}, nil)

		fake := newFakeGH(t, Route{Method: "POST", Endpoint: fixtureRepoPrefix + "/security-advisories", Status: 500, Body: map[string]string{"message": "the create failed"}})
		client := newFixtureClient(t, fake.Path)

		if err := github.FileAdvisories(t.Context(), client, plan); !errors.Is(err, publish.ErrGitHub) {
			t.Fatalf("FileAdvisories = %v, want an error wrapping publish.ErrGitHub", err)
		}

		if calls := fake.Calls(t); len(calls) != 1 {
			t.Errorf("the run's calls are %v, want the one refused create", calls)
		}
	})

	t.Run("an update answered 403", func(t *testing.T) {
		t.Parallel()

		finding := libraryPrivateFinding(t, "G309")

		existing := []github.Advisory{
			matchableAdvisory(fixtureGHSA, github.AdvisoryStateDraft, finding.Fingerprint, draftDescription("open")),
			matchableAdvisory(fixtureGHSA2, github.AdvisoryStateDraft, libraryFingerprint, draftDescription("open")),
		}

		plan := planOf(t, []publish.PrivateFinding{finding}, existing)

		fake := newFakeGH(t, Route{Method: "PATCH", Endpoint: advisoryPatchEndpoint(fixtureGHSA), Status: 403, Body: map[string]string{"message": "the update was refused"}})
		client := newFixtureClient(t, fake.Path)

		if err := github.FileAdvisories(t.Context(), client, plan); !errors.Is(err, publish.ErrGitHub) {
			t.Fatalf("FileAdvisories = %v, want an error wrapping publish.ErrGitHub: only a refused create is not filed", err)
		}

		if calls := fake.Calls(t); len(calls) != 1 {
			t.Errorf("the run's calls are %v, want the one refused update", calls)
		}
	})
}

// TestFileAdvisoriesRunsUnderItsDeadline pins a write that outlives its
// deadline: it is made once - never retried, since it may have been applied -
// and nothing follows it. The next run's listing reconciles.
//
// The deadline is generous against what a loaded machine costs, and the route's
// delay stays well beyond it. The fake gh is this test binary, so a call's fork
// and exec of it and the record of the call it appends have to fit inside the
// deadline before the "one call" below can be read at all: on a six-core VM at
// load average 45 that took up to 199 ms, and a 200 ms deadline ended 14 of 300
// calls before the fake had recorded anything - a run that made its one write
// and was read as one that made none (issue #253). The deadline is two seconds,
// ten times that measured 199 ms and the same as its siblings' in issues_test.go
// and report_test.go; the delay is five deadlines, so a FileAdvisories that
// ignored its deadline still reaches the fake's answer and returns no error,
// which the first check refuses.
func TestFileAdvisoriesRunsUnderItsDeadline(t *testing.T) {
	t.Parallel()

	const (
		callTimeout = 2 * time.Second
		routeDelay  = 10 * time.Second
	)

	t.Run("a create past its deadline", func(t *testing.T) {
		t.Parallel()

		first := libraryPrivateFinding(t, "G309")
		second := libraryPrivateFinding(t, "G310")

		plan := planOf(t, []publish.PrivateFinding{first, second}, nil)

		fake := newFakeGH(t, Route{
			Method:   "POST",
			Endpoint: fixtureRepoPrefix + "/security-advisories",
			Status:   201,
			Body:     map[string]any{"ghsa_id": fixtureGHSA},
			Delay:    routeDelay,
		})
		client := newTimeoutClient(t, fake.Path, callTimeout)

		if err := github.FileAdvisories(t.Context(), client, plan); !errors.Is(err, publish.ErrGitHub) {
			t.Fatalf("FileAdvisories = %v, want an error wrapping publish.ErrGitHub", err)
		}

		if calls := fake.Calls(t); len(calls) != 1 {
			t.Errorf("the run's calls are %v, want the one create that timed out: a write past its deadline is made once and never retried, and no calls means either the write was never made or the fake gh could not start inside the deadline (issue #253)", calls)
		}
	})

	t.Run("an update past its deadline", func(t *testing.T) {
		t.Parallel()

		finding := libraryPrivateFinding(t, "G309")

		existing := []github.Advisory{
			matchableAdvisory(fixtureGHSA, github.AdvisoryStateDraft, finding.Fingerprint, draftDescription("open")),
			matchableAdvisory(fixtureGHSA2, github.AdvisoryStateDraft, libraryFingerprint, draftDescription("open")),
		}

		plan := planOf(t, []publish.PrivateFinding{finding}, existing)

		fake := newFakeGH(t, Route{
			Method:   "PATCH",
			Endpoint: advisoryPatchEndpoint(fixtureGHSA),
			Status:   200,
			Body:     map[string]any{"ghsa_id": fixtureGHSA},
			Delay:    routeDelay,
		})
		client := newTimeoutClient(t, fake.Path, callTimeout)

		if err := github.FileAdvisories(t.Context(), client, plan); !errors.Is(err, publish.ErrGitHub) {
			t.Fatalf("FileAdvisories = %v, want an error wrapping publish.ErrGitHub", err)
		}

		if calls := fake.Calls(t); len(calls) != 1 {
			t.Errorf("the run's calls are %v, want the one update that timed out: a write past its deadline is made once and never retried, and no calls means either the write was never made or the fake gh could not start inside the deadline (issue #253)", calls)
		}
	})
}

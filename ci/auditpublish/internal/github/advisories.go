package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// MaxAdvisoryPages is the most pages ListAuditAdvisories reads: a tenth page
// whose answer still carries a Link to a next one fails closed
// (publish.ErrGitHub). The advisory listing's row cannot count pages, since
// it pages by an opaque after cursor, so this bound is the caller's.
const MaxAdvisoryPages = 10

// The advisory states GitHub's listing names. Only the audit App's drafts and
// closed advisories are ever matched; triage (a private vulnerability report),
// published and withdrawn ones never are.
const (
	AdvisoryStateDraft  = "draft"
	AdvisoryStateClosed = "closed"
)

// The one vulnerability every draft names: this module, as Go's ecosystem
// knows it. None of it is the model's.
const (
	advisoryEcosystem = "go"
	advisoryPackage   = "github.com/timothymamo/shift2eu"
)

// advisoryCursor is a next page's after cursor as the listing's row accepts
// it.
var advisoryCursor = regexp.MustCompile(`^[A-Za-z0-9+/=_%-]{1,200}$`)

// ghsaID is a GHSA identifier as the PATCH row's pattern accepts it: an
// advisory whose ID is anything else is never matched, so the endpoint table
// never refuses a planned write halfway through a run's writes.
var ghsaID = regexp.MustCompile(`^` + ghsaPattern + `$`)

// Advisory is one entry of the repository's advisory listing, the fields
// PlanAdvisories reads and nothing else.
type Advisory struct {
	// GHSAID is the advisory's GHSA identifier, the only ID a PATCH names.
	GHSAID      string
	Summary     string
	Description string
	// State is GitHub's: triage, draft, published, closed or withdrawn.
	State string
	// AuthorLogin and AuthorType are the advisory's author's login and type.
	AuthorLogin string
	AuthorType  string
	// ClosedAt is when the advisory was closed, the zero time when it is not.
	ClosedAt time.Time
	// Credits is the number of credited users (credits and credits_detailed),
	// Collaborators the number of collaborating users and teams: an advisory
	// with any is not the App's alone and is never matched.
	Credits       int
	Collaborators int
}

// advisoryEntry is one entry of the listing as the answer holds it. A pointer
// field the answer lacks, or a field of another JSON type, is an answer that
// is not the listing; a null list is an empty one.
type advisoryEntry struct {
	GHSAID      *string `json:"ghsa_id"`
	Summary     *string `json:"summary"`
	Description *string `json:"description"`
	State       *string `json:"state"`
	Author      *struct {
		Login *string `json:"login"`
		Type  *string `json:"type"`
	} `json:"author"`
	ClosedAt           *string           `json:"closed_at"`
	Credits            []json.RawMessage `json:"credits"`
	CreditsDetailed    []json.RawMessage `json:"credits_detailed"`
	CollaboratingUsers []json.RawMessage `json:"collaborating_users"`
	CollaboratingTeams []json.RawMessage `json:"collaborating_teams"`
}

// ListAuditAdvisories lists the repository's security advisories: GET
// R/security-advisories?per_page=100 (row 14), then, while the answer's Link
// header has rel="next", the same with &after=<the next link's after
// cursor>, at most MaxAdvisoryPages pages. A tenth page with a next one, a
// next link without a cursor or with one the row refuses, a non-2xx status,
// an answer that is not a JSON array of advisories and a failed call wrap
// publish.ErrGitHub. It returns every entry, foreign ones included:
// PlanAdvisories decides which it may match. No error names an advisory.
//
// Only the next link's cursor is read, never its host or its path (GitHub
// writes repositories/<id>/...): every page's endpoint is the fixed one.
func ListAuditAdvisories(ctx context.Context, c *Client) ([]Advisory, error) {
	if c == nil {
		return nil, fmt.Errorf("advisory listing: no client: %w", publish.ErrConfig)
	}

	base := "repos/" + c.scope.Repository + "/security-advisories?per_page=100"
	endpoint := base

	out := []Advisory{}

	for page := 1; ; page++ {
		resp, err := read(ctx, c, endpoint)
		if err != nil {
			return nil, fmt.Errorf("advisory listing: %w", err)
		}

		if resp.Status/100 != 2 {
			return nil, fmt.Errorf("advisory listing: the status is not 2xx: %w", publish.ErrGitHub)
		}

		advisories, err := decodeAdvisories(resp.Body)
		if err != nil {
			return nil, err
		}

		out = append(out, advisories...)

		cursor, next, err := nextCursor(resp.Link)
		if err != nil {
			return nil, err
		}

		if !next {
			return out, nil
		}

		if page == MaxAdvisoryPages {
			return nil, fmt.Errorf("advisory listing: more than %d pages: %w", MaxAdvisoryPages, publish.ErrGitHub)
		}

		endpoint = base + "&after=" + cursor
	}
}

// decodeAdvisories reads one page of the listing.
func decodeAdvisories(body []byte) ([]Advisory, error) {
	var entries []advisoryEntry
	if json.Unmarshal(body, &entries) != nil || entries == nil {
		return nil, fmt.Errorf("advisory listing: the answer is not the listing: %w", publish.ErrGitHub)
	}

	out := make([]Advisory, 0, len(entries))

	for _, e := range entries {
		a, ok := e.advisory()
		if !ok {
			return nil, fmt.Errorf("advisory listing: an entry that is no advisory: %w", publish.ErrGitHub)
		}

		out = append(out, a)
	}

	return out, nil
}

// advisory maps one entry, and reports false for one that is no advisory.
func (e advisoryEntry) advisory() (Advisory, bool) {
	if e.GHSAID == nil || e.Summary == nil || e.State == nil {
		return Advisory{}, false
	}

	a := Advisory{
		GHSAID:        *e.GHSAID,
		Summary:       *e.Summary,
		State:         *e.State,
		Credits:       len(e.Credits) + len(e.CreditsDetailed),
		Collaborators: len(e.CollaboratingUsers) + len(e.CollaboratingTeams),
	}

	if e.Description != nil {
		a.Description = *e.Description
	}

	if e.Author != nil {
		if e.Author.Login == nil || e.Author.Type == nil {
			return Advisory{}, false
		}

		a.AuthorLogin, a.AuthorType = *e.Author.Login, *e.Author.Type
	}

	if e.ClosedAt != nil {
		t, err := time.Parse(time.RFC3339, *e.ClosedAt)
		if err != nil {
			return Advisory{}, false
		}

		a.ClosedAt = t
	}

	return a, true
}

// nextCursor reads the after cursor of the Link header's rel="next" entry. It
// reports false for a header without one, and refuses (publish.ErrGitHub) a
// next entry with no URL, no after parameter, or a cursor the listing's row
// would refuse: a hostile Link is GitHub's failure, never a call.
func nextCursor(link string) (string, bool, error) {
	for _, part := range strings.Split(link, ",") {
		target, params, found := strings.Cut(strings.TrimSpace(part), ">")
		if !found || !strings.HasPrefix(target, "<") || !isNextRel(params) {
			continue
		}

		_, query, _ := strings.Cut(target[1:], "?")

		for _, param := range strings.Split(query, "&") {
			if cursor, ok := strings.CutPrefix(param, "after="); ok {
				if !advisoryCursor.MatchString(cursor) {
					return "", false, fmt.Errorf("advisory listing: a next page's cursor the row refuses: %w", publish.ErrGitHub)
				}

				return cursor, true, nil
			}
		}

		return "", false, fmt.Errorf("advisory listing: a next page without a cursor: %w", publish.ErrGitHub)
	}

	return "", false, nil
}

// isNextRel reports whether a Link entry's parameters hold rel="next".
func isNextRel(params string) bool {
	for _, p := range strings.Split(params, ";") {
		if strings.TrimSpace(p) == `rel="next"` {
			return true
		}
	}

	return false
}

// AdvisoryAction is what one planned advisory write does.
type AdvisoryAction int

// The two advisory writes.
const (
	// AdvisoryCreate is POST R/security-advisories (row 15): a new draft.
	AdvisoryCreate AdvisoryAction = iota + 1
	// AdvisoryUpdate is PATCH R/security-advisories/<GHSA ID> (row 16) of one
	// of the audit App's drafts, with state "draft".
	AdvisoryUpdate
)

// AdvisoryWrite is one write of an AdvisoryPlan.
type AdvisoryWrite struct {
	Action AdvisoryAction
	// GHSAID is the draft an AdvisoryUpdate PATCHes: one the plan's listing
	// showed as the audit App's draft. Empty for an AdvisoryCreate.
	GHSAID string
	// Status is the status the text's line states.
	Status publish.AdvisoryStatus
	// Text is what the write sends. A not-reproduced update sends only its
	// description (the listing's, with the status line replaced) and the
	// state; an open update the summary, the description and the state; a
	// create the summary, the description, the severity when known and the
	// one vulnerability of this module.
	Text publish.AdvisoryText
}

// AdvisoryPlan is the advisory writes of a run, which only PlanAdvisories
// builds: its fields are unexported, so FileAdvisories PATCHes only a GHSA
// that the run's own listing showed as one of the audit App's drafts.
type AdvisoryPlan struct {
	writes     []AdvisoryWrite
	reproduced bool
}

// Writes returns a copy of the plan's writes, in the order FileAdvisories
// makes them.
func (p AdvisoryPlan) Writes() []AdvisoryWrite {
	return append([]AdvisoryWrite(nil), p.writes...)
}

// Reproduced reports whether the run reproduced any private finding, a
// dismissed one included: the report's private state is then "handled
// privately" (or "not filed"), otherwise "none".
func (p AdvisoryPlan) Reproduced() bool { return p.reproduced }

// matched is one matchable advisory of the listing, with its fingerprint.
type matched struct {
	advisory    Advisory
	fingerprint publish.Fingerprint
}

// matchable reports the fingerprint of an advisory PlanAdvisories may match:
// the audit App's own draft or closed one, with no credit and no
// collaborator (the App adds none, so any is a person's), whose ID is a GHSA
// identifier and whose summary holds a fingerprint. Every other advisory is
// never matched and never written.
func matchable(a Advisory) (publish.Fingerprint, bool) {
	if a.AuthorLogin != AuditBotLogin || a.AuthorType != AuditBotType || !ghsaID.MatchString(a.GHSAID) ||
		(a.State != AdvisoryStateDraft && a.State != AdvisoryStateClosed) ||
		a.Credits != 0 || a.Collaborators != 0 {
		return "", false
	}

	return publish.AdvisoryFingerprint(a.Summary)
}

// PlanAdvisories plans the run's advisory writes from its private findings
// and the advisory listing. An advisory is matchable when its AuthorLogin is
// AuditBotLogin, its AuthorType AuditBotType, its State draft or closed, it
// has no credit and no collaborator, and its summary holds a fingerprint
// (publish.AdvisoryFingerprint); every other entry is never matched and
// never written. For each finding, in the result's order:
//
//   - a matchable draft with its fingerprint: an AdvisoryUpdate of it with
//     status open, every closed one with the fingerprint ignored (a draft
//     an earlier run marked not reproduced is open again);
//   - no draft, and the most recently closed matchable advisory with the
//     fingerprint (by ClosedAt, a tie the listing's first) has the status not
//     reproduced: an AdvisoryCreate with status regressed;
//   - no draft, and a closed one otherwise: dismissed, no write;
//   - none: an AdvisoryCreate with status new.
//
// Then, in the listing's order, each matchable draft whose fingerprint no
// finding holds and whose status is not already not reproduced: an
// AdvisoryUpdate with status not reproduced (publish.SetAdvisoryStatus). A
// draft whose description holds no status line (a maintainer's rewrite) is
// skipped, never written.
//
// Two matchable drafts with one fingerprint are publish.ErrDuplicateDrafts,
// whatever the findings: no plan, so no write. A text the core refuses is its
// error.
func PlanAdvisories(findings []publish.PrivateFinding, existing []Advisory, run publish.Run) (AdvisoryPlan, error) {
	drafts := map[publish.Fingerprint]Advisory{}
	closed := map[publish.Fingerprint][]Advisory{}

	var draftOrder []matched

	for _, a := range existing {
		fp, ok := matchable(a)
		if !ok {
			continue
		}

		if a.State == AdvisoryStateClosed {
			closed[fp] = append(closed[fp], a)

			continue
		}

		if _, twice := drafts[fp]; twice {
			return AdvisoryPlan{}, fmt.Errorf("advisory plan: two of the App's drafts with one fingerprint: %w", publish.ErrDuplicateDrafts)
		}

		drafts[fp] = a
		draftOrder = append(draftOrder, matched{advisory: a, fingerprint: fp})
	}

	plan := AdvisoryPlan{reproduced: len(findings) > 0}
	found := map[publish.Fingerprint]bool{}

	for _, f := range findings {
		found[f.Fingerprint] = true

		w, ok, err := planFinding(f, drafts, closed[f.Fingerprint], run)
		if err != nil {
			return AdvisoryPlan{}, err
		}

		if ok {
			plan.writes = append(plan.writes, w)
		}
	}

	for _, d := range draftOrder {
		if found[d.fingerprint] {
			continue
		}

		status, ok := publish.ParseAdvisoryStatus(d.advisory.Description)
		if !ok || status == publish.AdvisoryNotReproduced {
			continue
		}

		description, err := publish.SetAdvisoryStatus(d.advisory.Description, publish.AdvisoryNotReproduced)
		if err != nil {
			return AdvisoryPlan{}, err
		}

		plan.writes = append(plan.writes, AdvisoryWrite{
			Action: AdvisoryUpdate,
			GHSAID: d.advisory.GHSAID,
			Status: publish.AdvisoryNotReproduced,
			Text:   publish.AdvisoryText{Description: description},
		})
	}

	return plan, nil
}

// planFinding plans one finding's write, and reports false for a dismissed
// one: no draft, and a most recently closed advisory not marked not
// reproduced.
func planFinding(f publish.PrivateFinding, drafts map[publish.Fingerprint]Advisory, closed []Advisory, run publish.Run) (AdvisoryWrite, bool, error) {
	action, status, ghsa := AdvisoryCreate, publish.AdvisoryNew, ""

	if draft, ok := drafts[f.Fingerprint]; ok {
		action, status, ghsa = AdvisoryUpdate, publish.AdvisoryOpen, draft.GHSAID
	} else if len(closed) > 0 {
		latest := closed[0]

		for _, a := range closed[1:] {
			if a.ClosedAt.After(latest.ClosedAt) {
				latest = a
			}
		}

		if s, ok := publish.ParseAdvisoryStatus(latest.Description); !ok || s != publish.AdvisoryNotReproduced {
			return AdvisoryWrite{}, false, nil
		}

		status = publish.AdvisoryRegressed
	}

	text, err := publish.RenderAdvisory(f, status, run)
	if err != nil {
		return AdvisoryWrite{}, false, err
	}

	return AdvisoryWrite{Action: action, GHSAID: ghsa, Status: status, Text: text}, true, nil
}

// FileAdvisories makes the plan's writes in order, each through the one write
// helper: one call, never retried, a non-2xx status or an ended context
// publish.ErrGitHub, and nothing after a failed write. A create answered 403,
// 404 or 422 is GitHub refusing advisory creation to the App: it returns an
// error wrapping publish.ErrPrivateNotFiled, and no further advisory call is
// made. An answer's body is never read.
func FileAdvisories(ctx context.Context, c *Client, plan AdvisoryPlan) error {
	if c == nil {
		return fmt.Errorf("advisories: no client: %w", publish.ErrConfig)
	}

	endpoint := "repos/" + c.scope.Repository + "/security-advisories"

	for _, w := range plan.writes {
		switch w.Action {
		case AdvisoryCreate:
			resp, err := send(ctx, c, "POST", endpoint, createBody(w.Text))
			if err != nil {
				if errors.Is(err, publish.ErrGitHub) && refusedCreation(resp.Status) {
					return fmt.Errorf("advisories: GitHub refused the creation: %w", publish.ErrPrivateNotFiled)
				}

				return fmt.Errorf("advisories: the create: %w", err)
			}
		case AdvisoryUpdate:
			if _, err := send(ctx, c, "PATCH", endpoint+"/"+w.GHSAID, updateBody(w)); err != nil {
				return fmt.Errorf("advisories: the update: %w", err)
			}
		default:
			return fmt.Errorf("advisories: a write that is none of the two: %w", publish.ErrConfig)
		}
	}

	return nil
}

// refusedCreation reports whether a create's status is GitHub refusing
// advisory creation to the App, rather than failing.
func refusedCreation(status int) bool {
	return status == 403 || status == 404 || status == 422
}

// createBody is a create's body: the text, the severity only when known, and
// the one vulnerability of this module. Never a state, a CVE or a credit.
func createBody(text publish.AdvisoryText) map[string]any {
	body := map[string]any{
		"summary":     text.Summary,
		"description": text.Description,
		"vulnerabilities": []any{map[string]any{
			"package":                  map[string]any{"ecosystem": advisoryEcosystem, "name": advisoryPackage},
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

// updateBody is an update's body, state draft and never a severity, so the
// maintainer's severity or CVSS on the draft stands: the summary and the
// description of a reproduced draft, the description alone of one marked not
// reproduced.
func updateBody(w AdvisoryWrite) map[string]any {
	if w.Status == publish.AdvisoryNotReproduced {
		return map[string]any{"description": w.Text.Description, "state": AdvisoryStateDraft}
	}

	return map[string]any{"summary": w.Text.Summary, "description": w.Text.Description, "state": AdvisoryStateDraft}
}

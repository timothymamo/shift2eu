package github

import (
	"context"
	"errors"
	"time"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// errNotImplemented is the PR 4c skeleton's: every advisory function returns
// it until its behaviour is built test-first.
var errNotImplemented = errors.New("not implemented")

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

// ListAuditAdvisories lists the repository's security advisories: GET
// R/security-advisories?per_page=100 (row 14), then, while the answer's Link
// header has rel="next", the same with &after=<the next link's after
// cursor>, at most MaxAdvisoryPages pages. A tenth page with a next one, a
// next link without a cursor or with one the row refuses, a non-2xx status,
// an answer that is not a JSON array of advisories and a failed call wrap
// publish.ErrGitHub. It returns every entry, foreign ones included:
// PlanAdvisories decides which it may match. No error names an advisory.
func ListAuditAdvisories(_ context.Context, _ *Client) ([]Advisory, error) {
	return nil, errNotImplemented
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

// PlanAdvisories plans the run's advisory writes from its private findings
// and the advisory listing. An advisory is matchable when its AuthorLogin is
// AuditBotLogin, its AuthorType AuditBotType, its State draft or closed, it
// has no credit and no collaborator, and its summary holds a fingerprint
// (publish.AdvisoryFingerprint); every other entry is never matched and
// never written. For each finding, in the result's order:
//
//   - a matchable draft with its fingerprint: an AdvisoryUpdate of it with
//     status open, every closed one with the fingerprint ignored;
//   - no draft, and the most recently closed matchable advisory with the
//     fingerprint (by ClosedAt) has the status not reproduced: an
//     AdvisoryCreate with status regressed;
//   - no draft, and a closed one otherwise: dismissed, no write;
//   - none: an AdvisoryCreate with status new.
//
// Then, in the listing's order, each matchable draft whose fingerprint no
// finding holds and whose status is not already not reproduced: an
// AdvisoryUpdate with status not reproduced (publish.SetAdvisoryStatus).
//
// Two matchable drafts with one fingerprint are publish.ErrDuplicateDrafts,
// whatever the findings: no plan, so no write. A text the core refuses is its
// error.
func PlanAdvisories(_ []publish.PrivateFinding, _ []Advisory, _ publish.Run) (AdvisoryPlan, error) {
	return AdvisoryPlan{}, errNotImplemented
}

// FileAdvisories makes the plan's writes in order, each through the one write
// helper: one call, never retried, a non-2xx status or an ended context
// publish.ErrGitHub, and nothing after a failed write. A create answered 403,
// 404 or 422 is GitHub refusing advisory creation to the App: it returns an
// error wrapping publish.ErrPrivateNotFiled, and no further advisory call is
// made.
func FileAdvisories(_ context.Context, _ *Client, _ AdvisoryPlan) error {
	return errNotImplemented
}

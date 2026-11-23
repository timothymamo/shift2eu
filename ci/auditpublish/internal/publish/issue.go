package publish

import "errors"

// errNotImplemented is the stub error of PR 4b's skeleton (PRD #175 milestone
// 2): every function that returns it is declared, not built yet.
var errNotImplemented = errors.New("not implemented")

// PreviousFinding is one row of the previous merged report's findings table,
// as PublicFindings returns it: its fingerprint and fixed vocabulary cells,
// and its three model cells turned back into text.
type PreviousFinding struct {
	// Fingerprint is the row's fingerprint, fp- and 12 lowercase hex.
	Fingerprint Fingerprint
	// Severity, Category and Status are the row's fixed vocabulary cells:
	// low or informational, a public category, and new, open or fixed.
	Severity string
	Category string
	Status   string
	// Location, Scenario and Fix are the row's model cells with TableCell's
	// escaping undone: the code span's delimiters removed and every \| read
	// as |. The cell none is the empty text, and a fixed row's are empty.
	Location string
	Scenario string
	Fix      string
	// Report is the path of the report the row was read from:
	// docs/security/audits/<its name>.
	Report string
}

// PublicFindings returns the previous report's findings table, every row,
// fixed ones included, in the table's order; the needs-investigation table
// and the False positives are not part of it. It returns nil for a nil or
// unparsed Previous and for a report with no public findings.
func (p *Previous) PublicFindings() []PreviousFinding {
	return nil
}

// IssueText is one security issue a run files for a merged public finding:
// its title and body, and every escaped fragment of model text the body
// holds, which ScanPublic's encoded-run refusal reads.
type IssueText struct {
	// Fingerprint is the finding's fingerprint, which the title holds.
	Fingerprint Fingerprint
	Title       string
	Body        string
	ModelFields []string
}

// RenderIssue renders the issue of a merged public finding: the title
// "Security audit finding <fingerprint>: <category> (<severity>)" and a fixed
// body that holds the finding's location, scenario and fix as CodeBlocks, its
// report's path and the run's URL. It never holds a finding's evidence. It
// refuses (ErrConfig) a Run NewRun would refuse, and (ErrPreviousReport) a
// finding whose fingerprint, severity, category or status is outside the
// findings table's vocabulary or whose status is fixed.
func RenderIssue(_ PreviousFinding, _ Run) (IssueText, error) {
	return IssueText{}, errNotImplemented
}

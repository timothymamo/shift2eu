package publish

import "errors"

// errNotImplemented is the PR 4c skeleton's: every advisory function returns
// it until its behaviour is built test-first.
var errNotImplemented = errors.New("not implemented")

// AdvisoryStatus is a draft advisory's tracking state, which the fixed line
// "Audit status: <status>" of its description holds: the only place a
// private finding's history lives (PRD #175 milestone 2, "It tracks private
// findings by fingerprint"). The zero value is none of the four.
type AdvisoryStatus int

// The four advisory statuses.
const (
	// AdvisoryNew is a private finding filed for the first time: a new
	// draft.
	AdvisoryNew AdvisoryStatus = iota + 1
	// AdvisoryOpen is a private finding this run reproduced whose draft
	// exists: the draft is updated.
	AdvisoryOpen
	// AdvisoryNotReproduced is a draft whose finding this run did not
	// reproduce.
	AdvisoryNotReproduced
	// AdvisoryRegressed is a recurrence of a finding whose most recently
	// closed advisory an earlier run had marked not reproduced: a new draft.
	AdvisoryRegressed
)

// PrivateFinding is one finding of the run that Place puts in a draft
// advisory, with its keyed fingerprint.
type PrivateFinding struct {
	Finding     Finding
	Fingerprint Fingerprint
}

// AdvisoryText is one draft advisory's text as a run sends it: the summary
// and the description, and the severity GitHub's advisory API takes, empty
// for none.
type AdvisoryText struct {
	// Fingerprint is the finding's keyed fingerprint, which the summary
	// holds.
	Fingerprint Fingerprint
	// Summary is "Audit finding <fingerprint> (<severity>, <category>)",
	// unknown in place of a value outside the vocabulary: no model text.
	Summary string
	// Description is the fixed body: the finding's fields as CodeBlocks
	// (evidence included; a history secret's commit and rule only), then the
	// line "Audit status: <status>" and the run's URL.
	Description string
	// Severity is GitHub's severity of the finding (critical, high, medium or
	// low; informational is low), empty for an unknown one, which the create
	// body then leaves out.
	Severity string
}

// PrivateFindings returns the run's findings that Place puts in a draft
// advisory, in the result's order, each with its keyed fingerprint. A key
// KeyedFingerprint refuses is its ErrConfig; two private findings with one
// fingerprint are ErrResultInvalid.
func PrivateFindings(_ Result, _ FingerprintKey) ([]PrivateFinding, error) {
	return nil, errNotImplemented
}

// RenderAdvisory renders the draft advisory of a private finding under the
// given status (AdvisoryNew, AdvisoryOpen or AdvisoryRegressed): the summary
// "Audit finding <fingerprint> (<severity>, <category>)", unknown in place of
// an unknown value, and a fixed description. For PlacementAdvisory and
// PlacementAdvisoryUnconfirmed the description holds the check, the rule, the
// location (file and line), the sink, the scenario, the evidence and the fix,
// each model value as a CodeBlock, and "unconfirmed" for the latter; for
// PlacementAdvisoryHistorySecret only the commit (the finding's file) and the
// rule, and the line "unconfirmed: maintainer to check", never the sink, the
// scenario, the evidence or the fix. Both end with "Audit status: <new|open|
// regressed>" and the run's URL. The evidence goes into an advisory only,
// never into a report or an issue.
//
// It refuses (ErrConfig) a Run NewRun would refuse, a finding Place does not
// put in an advisory, a fingerprint that is no fingerprint, and the status
// AdvisoryNotReproduced or one that is none of the four (SetAdvisoryStatus
// marks a draft not reproduced).
func RenderAdvisory(_ PrivateFinding, _ AdvisoryStatus, _ Run) (AdvisoryText, error) {
	return AdvisoryText{}, errNotImplemented
}

// ParseAdvisoryStatus reads a description's status: its last line that is
// exactly "Audit status: " and one of new, open, not reproduced or regressed.
// It reports false for a description with no such line.
func ParseAdvisoryStatus(_ string) (AdvisoryStatus, bool) {
	return 0, false
}

// SetAdvisoryStatus returns description with its last status line replaced by
// "Audit status: <status>": the text of a draft whose finding this run did not
// reproduce, which the run has no finding to render afresh from. It refuses
// (ErrConfig) a status that is none of the four, and a description with no
// status line (ParseAdvisoryStatus reports false).
func SetAdvisoryStatus(_ string, _ AdvisoryStatus) (string, error) {
	return "", errNotImplemented
}

// AdvisoryFingerprint reads the fingerprint of an advisory's summary: a
// summary that starts with "Audit finding ", a fingerprint (fp- and 12
// lowercase hex) and " (". It reports false for any other summary, a
// maintainer's rewording included: that advisory then matches no finding.
func AdvisoryFingerprint(_ string) (Fingerprint, bool) {
	return "", false
}

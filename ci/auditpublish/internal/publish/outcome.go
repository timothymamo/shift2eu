package publish

import (
	"errors"
	"strconv"
)

// outcomeKind is one row of ADR 0040's exit-code table.
type outcomeKind int

const (
	outcomeFailed outcomeKind = iota
	outcomePublished
	outcomeResultInvalid
	outcomeScanRefused
	outcomePrivateNotFiled
	outcomeDuplicateDrafts
	outcomePreviousReport
	outcomeGitHub
	outcomeConfig
)

// Outcome is how a publish run ended: its exit code and the one summary line
// the entry point prints. The line holds no finding's text, no count of
// private findings, no API response and no error message.
type Outcome struct {
	kind    outcomeKind
	counted bool
	public  int
	private PrivateState
}

// classes maps each error class to its outcome, in the order OutcomeOf tests
// them: an error that wraps two classes gets the first.
var classes = []struct {
	err  error
	kind outcomeKind
}{
	{ErrScanRefused, outcomeScanRefused},
	{ErrResultInvalid, outcomeResultInvalid},
	{ErrPrivateNotFiled, outcomePrivateNotFiled},
	{ErrDuplicateDrafts, outcomeDuplicateDrafts},
	{ErrPreviousReport, outcomePreviousReport},
	{ErrGitHub, outcomeGitHub},
	{ErrConfig, outcomeConfig},
}

// OutcomeOf classifies a run's error by its class alone, never its text: nil
// is a published run, whose counts WithCounts sets; an error of no class is a
// failed one.
func OutcomeOf(err error) Outcome {
	if err == nil {
		return Outcome{kind: outcomePublished}
	}

	for _, c := range classes {
		if errors.Is(err, c.err) {
			return Outcome{kind: c.kind}
		}
	}

	return Outcome{kind: outcomeFailed}
}

// WithCounts sets a published run's public total and private state, which
// its summary line states. A published run whose private findings were not
// filed is the not-filed outcome, exit code 5, exactly as if the entry point
// had returned ErrPrivateNotFiled: the job fails, never goes green. It leaves
// any other outcome as it is.
func (o Outcome) WithCounts(public int, private PrivateState) Outcome {
	if o.kind != outcomePublished {
		return o
	}

	if private == PrivateNotFiled {
		return Outcome{kind: outcomePrivateNotFiled}
	}

	o.counted, o.public, o.private = true, public, private

	return o
}

// published reports whether o is a published run whose counts are set and
// valid: a published outcome without them fails closed.
func (o Outcome) published() (string, bool) {
	if o.kind != outcomePublished || !o.counted || o.public < 0 {
		return "", false
	}

	return o.private.line()
}

// Code is the exit code. Code 2 is left to the Go runtime's panic exit.
func (o Outcome) Code() int {
	if _, ok := o.published(); ok {
		return 0
	}

	switch o.kind {
	case outcomeResultInvalid:
		return 3
	case outcomeScanRefused:
		return 4
	case outcomePrivateNotFiled:
		return 5
	case outcomeDuplicateDrafts:
		return 6
	case outcomePreviousReport:
		return 7
	case outcomeGitHub:
		return 8
	case outcomeConfig:
		return 9
	case outcomeFailed, outcomePublished:
		return 1
	default:
		return 1
	}
}

// Summary is the one line the entry point prints.
func (o Outcome) Summary() string {
	if line, ok := o.published(); ok {
		return "audit published: public findings " + strconv.Itoa(o.public) + ", " + line
	}

	switch o.kind {
	case outcomeResultInvalid:
		return "result refused: see runbook"
	case outcomeScanRefused:
		return "publish refused: see runbook"
	case outcomePrivateNotFiled:
		return "private findings not filed: see runbook"
	case outcomeDuplicateDrafts:
		return "publish refused: duplicate draft advisories: see runbook step 11"
	case outcomePreviousReport:
		return "previous report unreadable: see runbook"
	case outcomeGitHub:
		return "GitHub call failed: see runbook"
	case outcomeConfig:
		return "configuration invalid: see runbook"
	case outcomeFailed, outcomePublished:
		return "publish failed: see runbook"
	default:
		return "publish failed: see runbook"
	}
}

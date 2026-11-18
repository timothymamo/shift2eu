package review

import "errors"

// Model is the Anthropic model every review asks, named to the Claude Code CLI
// with --model (ADR 0033).
const Model = "claude-opus-5-5"

// MaxDiffBytes is the largest unified diff, in bytes, the reviewer sends to the
// model (ADR 0033). A larger diff fails the run with ErrDiffTooLarge and posts
// no review.
const MaxDiffBytes = 300_000

// The errors a run fails with. Each one means nothing was posted, except a
// failed dismissal, which comes after the review.
var (
	// ErrEventRefused is returned by the poster for any event but
	// EventComment and EventRequestChanges, before any HTTP call.
	ErrEventRefused = errors.New("review event refused: only COMMENT and REQUEST_CHANGES are posted")
	// ErrDiffTooLarge is returned when the pull request's diff is over
	// MaxDiffBytes.
	ErrDiffTooLarge = errors.New("diff over the AI review's size limit")
	// ErrDraft is returned for a draft pull request, which is not reviewed.
	ErrDraft = errors.New("pull request is a draft")
	// ErrGitHub is returned for a GitHub API call that failed or answered
	// something the reviewer cannot read.
	ErrGitHub = errors.New("GitHub API error")
	// ErrModel is returned for a model call that failed or did not end its
	// turn (a Claude Code CLI that could not start, timed out, reported an
	// error such as a usage limit, or printed no result).
	ErrModel = errors.New("model error")
	// ErrMalformedAnswer is returned for a model answer ParseAnswer refuses.
	ErrMalformedAnswer = errors.New("malformed model answer")
	// ErrSettings is returned by SettingsFromEnv for a missing or invalid
	// setting.
	ErrSettings = errors.New("invalid AI review settings")
)

// Event is a review event as the GitHub API names it.
type Event string

// The only events the reviewer posts. There is no APPROVE on purpose.
const (
	EventComment        Event = "COMMENT"
	EventRequestChanges Event = "REQUEST_CHANGES"
)

// Severity is a finding's severity, the review-change skill's words.
type Severity string

// The severities ParseAnswer accepts.
const (
	SeverityBlocker    Severity = "blocker"
	SeveritySuggestion Severity = "suggestion"
	SeverityNit        Severity = "nit"
)

// Category is what a finding is about.
type Category string

// The categories ParseAnswer accepts. CategoryPromptInjection is an attempt in
// the pull request to instruct the reviewer; it is always blocking.
const (
	CategoryRequirements    Category = "requirements"
	CategoryCorrectness     Category = "correctness"
	CategoryDomain          Category = "domain"
	CategoryArchitecture    Category = "architecture"
	CategoryTests           Category = "tests"
	CategoryReadability     Category = "readability"
	CategorySecurity        Category = "security"
	CategoryPromptInjection Category = "prompt-injection"
)

// Finding is one problem the model reports. Path and Line are optional: Line 0
// means no line, and a Line above 0 needs a Path, relative to the repository
// root.
type Finding struct {
	Severity Severity
	Category Category
	Path     string
	Line     int
	Title    string
	Body     string
}

// Blocking reports whether the finding blocks: severity blocker, or category
// prompt-injection whatever its severity.
func (f Finding) Blocking() bool {
	return f.Severity == SeverityBlocker || f.Category == CategoryPromptInjection
}

// countBlocking returns how many of findings are blocking.
func countBlocking(findings []Finding) int {
	n := 0

	for _, f := range findings {
		if f.Blocking() {
			n++
		}
	}

	return n
}

// Answer is the model's parsed answer: a summary and the findings, possibly
// none.
type Answer struct {
	Summary  string
	Findings []Finding
}

// Decide returns the event of a review with these findings: EventRequestChanges
// when a finding is blocking and requestChanges (the AI_REVIEW_REQUEST_CHANGES
// switch) is on, EventComment otherwise. The model never names the event.
func Decide(findings []Finding, requestChanges bool) Event {
	if requestChanges && countBlocking(findings) > 0 {
		return EventRequestChanges
	}

	return EventComment
}

package review

import "errors"

// Model is the Anthropic model every review asks, named to the Claude Code CLI
// with --model (ADR 0033).
const Model = "claude-opus-5-5"

// MaxDiffBytes is the largest unified diff, in bytes, the reviewer sends to the
// model (ADR 0033): the pull request's diff once FilterDiff has left the
// generated and golden files out (issue #324), in one model call. A larger one
// is split into at most MaxDiffParts parts of at most this size (SplitDiff,
// issue #367), and one that cannot be split so ends in the skipped outcome.
const MaxDiffBytes = 300_000

// MaxRawDiffBytes is the largest diff, in bytes, the GitHub client reads before
// anything is left out (issue #324, ADR 0033). It is four times MaxDiffBytes,
// room for a schema version bump whose golden files and generated bundle are
// most of its diff, while a diff the client reads still costs little memory. A
// larger diff is refused with ErrDiffTooLarge, and the run ends in the skipped
// outcome (issue #367): it is never filtered in part, since a diff cut short is
// no diff.
const MaxRawDiffBytes = 4 * MaxDiffBytes

// The errors a run fails with. Each one means nothing was posted, except a
// failed dismissal, which comes after the review. ErrDiffTooLarge and two
// answers in turn whose shape ParseAnswer refuses do not fail a run: Run ends
// those in the skipped outcome (Result.Skip, issues #367, #376).
var (
	// ErrEventRefused is returned by the poster for any event but
	// EventComment and EventRequestChanges, before any HTTP call.
	ErrEventRefused = errors.New("review event refused: only COMMENT and REQUEST_CHANGES are posted")
	// ErrDiffTooLarge is returned when the pull request's diff is over
	// MaxRawDiffBytes, or SplitDiff cannot split what FilterDiff keeps of it
	// into at most MaxDiffParts parts of at most MaxDiffBytes. Run takes the
	// skipped outcome for it.
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

// CategoryUncategorised is the category ParseAnswer gives a finding whose
// category is missing or, once normalised, not one of the accepted categories,
// so one category the model got wrong costs no finding (issue #292). A category
// containing "inject" or "prompt" is read as CategoryPromptInjection first and
// never falls back here. It is not an accepted category itself, and the prompt never
// offers it: a model that writes "uncategorised" gets the same fallback. It
// never blocks by itself; the finding's severity decides.
const CategoryUncategorised Category = "uncategorised"

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

package publish

import (
	"regexp"
	"strconv"
	"strings"
)

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

// statusPrefix starts the description's status line.
const statusPrefix = "Audit status: "

// statusWords is each status's word on the status line.
var statusWords = map[AdvisoryStatus]string{
	AdvisoryNew:           "new",
	AdvisoryOpen:          "open",
	AdvisoryNotReproduced: "not reproduced",
	AdvisoryRegressed:     "regressed",
}

// advisorySeverities is GitHub's advisory severity of each known severity:
// informational has none of its own, so it is low.
var advisorySeverities = map[string]string{
	"critical":      "critical",
	"high":          "high",
	"medium":        "medium",
	"low":           "low",
	"informational": "low",
}

// summaryFingerprint reads the fingerprint of ADR 0040's summary template.
var summaryFingerprint = regexp.MustCompile(`^Audit finding (fp-[0-9a-f]{12}) \(`)

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
func PrivateFindings(r Result, key FingerprintKey) ([]PrivateFinding, error) {
	var private []PrivateFinding

	seen := make(map[Fingerprint]bool)

	for _, f := range r.Findings {
		if !Place(f).Private() {
			continue
		}

		fp, err := KeyedFingerprint(key, f.Rule, f.File, f.Sink)
		if err != nil {
			return nil, err
		}

		if seen[fp] {
			return nil, refusal(CheckFingerprintTwice, "findings: two private findings with one fingerprint")
		}

		seen[fp] = true

		private = append(private, PrivateFinding{Finding: f, Fingerprint: fp})
	}

	return private, nil
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
// It refuses (ErrConfig) a Run NewRun would refuse, and (ErrResultInvalid,
// the result's own content, issue #640) a finding Place does not put in an
// advisory, a fingerprint that is no fingerprint, and the status
// AdvisoryNotReproduced or one that is none of the four (SetAdvisoryStatus
// marks a draft not reproduced).
func RenderAdvisory(f PrivateFinding, status AdvisoryStatus, run Run) (AdvisoryText, error) {
	if err := run.validate(); err != nil {
		return AdvisoryText{}, err
	}

	placement := Place(f.Finding)
	if placement != PlacementAdvisory && placement != PlacementAdvisoryUnconfirmed && placement != PlacementAdvisoryHistorySecret {
		return AdvisoryText{}, refusal(CheckAdvisoryNotPrivate, "advisory: the finding is not private")
	}

	if !fingerprintPattern.MatchString(string(f.Fingerprint)) {
		return AdvisoryText{}, refusal(CheckAdvisoryFingerprint, "advisory: the fingerprint is no fingerprint")
	}

	word, ok := statusWords[status]
	if !ok || status == AdvisoryNotReproduced {
		return AdvisoryText{}, refusal(CheckAdvisoryRenderStatus, "advisory: the status is not new, open or regressed")
	}

	severity := vocabularyWord(f.Finding.Severity, severityRank)
	category := vocabularyWord(f.Finding.Category, categories)

	var b strings.Builder

	b.WriteString("Security audit finding " + string(f.Fingerprint) + ", found by the scheduled security audit.\n\n")

	if placement == PlacementAdvisoryHistorySecret {
		writeHistorySecret(&b, f.Finding)
	} else {
		writeFindingFields(&b, f.Finding, severity, category)
	}

	b.WriteString("\n" + statusPrefix + word + "\n")
	b.WriteString("Run: " + run.URL() + "\n")

	return AdvisoryText{
		Fingerprint: f.Fingerprint,
		Summary:     "Audit finding " + string(f.Fingerprint) + " (" + severity + ", " + category + ")",
		Description: b.String(),
		Severity:    advisorySeverities[f.Finding.Severity],
	}, nil
}

// vocabularyWord returns v when the vocabulary holds it, and unknown
// otherwise: the only words of a finding's severity or category an advisory
// writes outside a code span.
func vocabularyWord[V any](v string, vocabulary map[string]V) string {
	if _, ok := vocabulary[v]; ok {
		return v
	}

	return "unknown"
}

// writeHistorySecret writes a history secret's fields: the commit and the
// rule only, since the sink, the scenario, the evidence and the fix may hold
// the secret.
func writeHistorySecret(b *strings.Builder, f Finding) {
	b.WriteString("A secret in the repository's history: unconfirmed: maintainer to check.\n")
	b.WriteString("It was not tried against its service.\n\n")
	b.WriteString("Commit: " + CodeSpan(f.File) + "\n")
	b.WriteString("Rule: " + CodeSpan(f.Rule) + "\n")
}

// writeFindingFields writes any other private finding's fields, each model
// value in a code span or a fenced code block.
func writeFindingFields(b *strings.Builder, f Finding, severity, category string) {
	confirmed := "no: unconfirmed"
	if f.Confirmed {
		confirmed = "yes"
	}

	check := f.Check
	if !isCheckID(check) {
		check = CodeSpan(check)
	}

	location := CodeSpan(f.File)
	if f.Line > 0 {
		location += ":" + strconv.Itoa(f.Line)
	}

	b.WriteString("Severity: " + severity + "\n")
	b.WriteString("Category: " + category + "\n")
	b.WriteString("Confirmed: " + confirmed + "\n")
	b.WriteString("Check: " + check + "\n")
	b.WriteString("Rule: " + CodeSpan(f.Rule) + "\n")
	b.WriteString("Location: " + location + "\n")

	for _, block := range []struct{ heading, text string }{
		{"Sink", f.Sink},
		{"Scenario", f.Scenario},
		{"Evidence", f.Evidence},
		{"Recommended fix", f.Fix},
	} {
		b.WriteString("\n" + block.heading + ":\n\n" + CodeBlock(block.text) + "\n")
	}
}

// lastStatusLine returns the byte range of the description's last status line,
// without its line ending (LF, or CR LF read as LF), and the status it holds.
func lastStatusLine(description string) (start, end int, status AdvisoryStatus, ok bool) {
	for lineStart := 0; lineStart <= len(description); {
		lineEnd := strings.IndexByte(description[lineStart:], '\n')

		var next int

		if lineEnd < 0 {
			lineEnd = len(description)
			next = len(description) + 1
		} else {
			lineEnd += lineStart
			next = lineEnd + 1
		}

		contentEnd := lineEnd
		if contentEnd > lineStart && description[contentEnd-1] == '\r' {
			contentEnd--
		}

		if s, found := statusOfLine(description[lineStart:contentEnd]); found {
			start, end, status, ok = lineStart, contentEnd, s, true
		}

		lineStart = next
	}

	return start, end, status, ok
}

// statusOfLine reads a line that is exactly "Audit status: " and one of the
// four words.
func statusOfLine(line string) (AdvisoryStatus, bool) {
	word, found := strings.CutPrefix(line, statusPrefix)
	if !found {
		return 0, false
	}

	for status, w := range statusWords {
		if w == word {
			return status, true
		}
	}

	return 0, false
}

// ParseAdvisoryStatus reads a description's status: its last line that is
// exactly "Audit status: " and one of new, open, not reproduced or regressed.
// It reports false for a description with no such line.
func ParseAdvisoryStatus(description string) (AdvisoryStatus, bool) {
	_, _, status, ok := lastStatusLine(description)

	return status, ok
}

// SetAdvisoryStatus returns description with its last status line replaced by
// "Audit status: <status>": the text of a draft whose finding this run did not
// reproduce, which the run has no finding to render afresh from. It refuses
// (ErrResultInvalid) a status that is none of the four, and a description with no
// status line (ParseAdvisoryStatus reports false).
func SetAdvisoryStatus(description string, status AdvisoryStatus) (string, error) {
	word, known := statusWords[status]
	if !known {
		return "", refusal(CheckAdvisorySetStatus, "advisory: the status is none of the four")
	}

	start, end, _, ok := lastStatusLine(description)
	if !ok {
		return "", refusal(CheckAdvisoryStatusLine, "advisory: the description holds no status line")
	}

	return description[:start] + statusPrefix + word + description[end:], nil
}

// AdvisoryFingerprint reads the fingerprint of an advisory's summary: a
// summary that starts with "Audit finding ", a fingerprint (fp- and 12
// lowercase hex) and " (". It reports false for any other summary, a
// maintainer's rewording included: that advisory then matches no finding.
func AdvisoryFingerprint(summary string) (Fingerprint, bool) {
	m := summaryFingerprint.FindStringSubmatch(summary)
	if m == nil {
		return "", false
	}

	return Fingerprint(m[1]), true
}

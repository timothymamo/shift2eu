package publish_test

import (
	"math"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// The run every fixture names. The values are invented (CLAUDE.md rule 2): the
// repository is example.com's, the commit is hex counting up, and the digest is
// one repeated digit.
const (
	testServerURL  = "https://github.com"
	testRepository = "example/shift2eu"
	testDate       = "2026-11-23"
	testRunID      = "18422000001"
	testCommit     = "0123456789abcdef0123456789abcdef01234567"
	testTag        = "v0.1.0"
	testDigest     = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testReportPath = "docs/security/audits/" + testDate + "-" + testRunID + ".md"
)

// The severities and categories the result schema names (ADR 0040).
const (
	sevCritical      = "critical"
	sevHigh          = "high"
	sevMedium        = "medium"
	sevLow           = "low"
	sevInformational = "informational"

	catInjection      = "injection"
	catConfiguration  = "configuration"
	catHardening      = "hardening"
	catSupplyChain    = "supply chain"
	catDependency     = "dependency vulnerability"
	catDenialOfSvc    = "denial of service"
	catInfoDisclosure = "information disclosure"
)

// checkIDs lists the coverage table's check ids in the order ADR 0040 renders
// them.
var checkIDs = []string{
	"dependencies",
	"history-secrets",
	"workflows",
	"scorecard",
	"scorecard-admin",
	"release-integrity",
	"image-pull",
	"binary-vulnerabilities",
	"image-scan",
	"image-runtime",
	"hostile-inventories",
	"fuzzing",
	"supply-chain",
	"tree-review",
	"agent-surface",
}

// testRun builds the run the report fixtures are rendered for.
func testRun(t *testing.T, date, id string) publish.Run {
	t.Helper()

	run, err := publish.NewRun(testServerURL, testRepository, id, date)
	if err != nil {
		t.Fatalf("NewRun(%q, %q, %q, %q): %v", testServerURL, testRepository, id, date, err)
	}

	return run
}

// ranChecks returns one check per check id, in the table's order, each one ran.
func ranChecks() []publish.Check {
	checks := make([]publish.Check, 0, len(checkIDs))
	for _, id := range checkIDs {
		checks = append(checks, publish.Check{ID: id, Ran: true})
	}

	return checks
}

// checksWithNotRun returns one check per check id, in the table's order: the
// ones why lists did not run, with why's value as their reason, and the rest
// ran.
func checksWithNotRun(why map[string]string) []publish.Check {
	checks := make([]publish.Check, 0, len(checkIDs))

	for _, id := range checkIDs {
		reason, didNotRun := why[id]
		checks = append(checks, publish.Check{ID: id, Ran: !didNotRun, NotRun: reason})
	}

	return checks
}

// fixtureResult returns a result every check ran in, with findings, the run's
// fixed values and one entry in each of the sections the model writes.
func fixtureResult(findings ...publish.Finding) publish.Result {
	return publish.Result{
		Schema:      publish.SchemaVersion,
		Commit:      testCommit,
		ReleaseTag:  testTag,
		ImageDigest: testDigest,
		Tools: []publish.Tool{
			{Name: "go", Version: "1.27.4"},
			{Name: "gitleaks", Version: "v8.29.0"},
		},
		Checks:   ranChecks(),
		Findings: findings,
		Changes: publish.Changes{
			Dependencies: []string{"github.com/google/go-cmp v0.7.0 to v0.8.0"},
			Narrative:    "No dependency changed maintainer.",
		},
		Limits: []string{"scorecard's admin checks need a read the job does not hold."},
	}
}

// renderReport renders in and fails the test when the core refuses it.
func renderReport(t *testing.T, in publish.ReportInput) publish.Rendered {
	t.Helper()

	rendered, err := publish.RenderReport(in)
	if err != nil {
		t.Fatalf("RenderReport: %v", err)
	}

	return rendered
}

// section is one ## section of a rendered report: its heading's text and the
// lines below it, up to the next ## heading.
type section struct {
	Name  string
	Lines []string
}

var (
	// sectionHeading is a ## heading line.
	sectionHeading = regexp.MustCompile(`^## (.*)$`)
	// fenceRun is a line that opens or closes a fenced code block: up to three
	// spaces, then at least three backticks.
	fenceRun = regexp.MustCompile("^ {0,3}(`{3,})")
	// titleHeading is a # heading line.
	titleHeading = regexp.MustCompile(`^# (.*)$`)
	// tableSeparator is one cell of a table's delimiter row.
	tableSeparator = regexp.MustCompile(`^:?-{3,}:?$`)
	// inlineLink is an inline link or image; group 1 is its destination.
	inlineLink = regexp.MustCompile(`!?\[[^\]]*\]\(([^)]*)\)`)
)

// investigationHeading is the sub-heading of the Findings section that holds
// the suspected findings.
const investigationHeading = "### Needs investigation"

// parseSections splits a rendered report into its ## sections, in order. Lines
// inside a fenced code block are never headings: model text cannot start a
// section of the report.
func parseSections(t *testing.T, body string) []section {
	t.Helper()

	var (
		out     []section
		inFence bool
	)

	for _, line := range strings.Split(body, "\n") {
		if m := fenceRun.FindStringSubmatch(line); m != nil {
			inFence = !inFence

			if len(out) > 0 {
				out[len(out)-1].Lines = append(out[len(out)-1].Lines, line)
			}

			continue
		}

		if !inFence {
			if m := sectionHeading.FindStringSubmatch(line); m != nil {
				out = append(out, section{Name: m[1]})

				continue
			}
		}

		if len(out) > 0 {
			out[len(out)-1].Lines = append(out[len(out)-1].Lines, line)
		}
	}

	if len(out) == 0 {
		t.Fatalf("the report holds no ## section:\n%s", body)
	}

	return out
}

// sectionOf returns the named section of a rendered report, and fails the test
// when the report has none.
func sectionOf(t *testing.T, body, name string) section {
	t.Helper()

	for _, s := range parseSections(t, body) {
		if s.Name == name {
			return s
		}
	}

	t.Fatalf("the report holds no %q section:\n%s", name, body)

	return section{}
}

// sectionText returns a named section's lines, joined with LF.
func sectionText(t *testing.T, body, name string) string {
	t.Helper()

	return strings.Join(sectionOf(t, body, name).Lines, "\n")
}

// tableRows returns the table rows of lines, each row's cells trimmed, with the
// delimiter row left out.
func tableRows(t *testing.T, lines []string) [][]string {
	t.Helper()

	var rows [][]string

	for _, line := range lines {
		if !strings.HasPrefix(line, "|") {
			continue
		}

		cells := splitTableRow(strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|"))
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}

		separator := true

		for _, cell := range cells {
			if !tableSeparator.MatchString(cell) {
				separator = false

				break
			}
		}

		if separator {
			continue
		}

		rows = append(rows, cells)
	}

	return rows
}

// splitTableRow splits a table row's inner text into its cells at every | that
// is not written \|, as GFM's table parser does: an escaped pipe is part of the
// cell, never a cell boundary.
func splitTableRow(inner string) []string {
	cells := make([]string, 0, strings.Count(inner, "|")+1)

	var cell strings.Builder

	for i := 0; i < len(inner); i++ {
		switch {
		case inner[i] == '\\' && i+1 < len(inner) && inner[i+1] == '|':
			cell.WriteString(`\|`)
			i++
		case inner[i] == '|':
			cells = append(cells, cell.String())
			cell.Reset()
		default:
			cell.WriteByte(inner[i])
		}
	}

	return append(cells, cell.String())
}

// tableOf returns the rows of the one table in a named section, and fails the
// test when the section holds none.
func tableOf(t *testing.T, body, name string) [][]string {
	t.Helper()

	rows := tableRows(t, sectionOf(t, body, name).Lines)
	if len(rows) == 0 {
		t.Fatalf("the %q section holds no table:\n%s", name, body)
	}

	return rows
}

// findingsTableOf returns the rows of the Findings section's first table, the
// one above the Needs investigation heading.
func findingsTableOf(t *testing.T, body string) [][]string {
	t.Helper()

	s := sectionOf(t, body, "Findings")

	cut := len(s.Lines)

	for i, line := range s.Lines {
		if strings.TrimSpace(line) == investigationHeading {
			cut = i

			break
		}
	}

	return tableRows(t, s.Lines[:cut])
}

// investigationTableOf returns the rows of the Needs investigation table inside
// the Findings section.
func investigationTableOf(t *testing.T, body string) [][]string {
	t.Helper()

	s := sectionOf(t, body, "Findings")

	start := -1

	for i, line := range s.Lines {
		if strings.TrimSpace(line) == investigationHeading {
			start = i

			break
		}
	}

	if start < 0 {
		t.Fatalf("the Findings section holds no %q heading:\n%s", investigationHeading, body)
	}

	return tableRows(t, s.Lines[start+1:])
}

// rowHolding returns the first row whose cell at column holds want, and fails
// the test when no row does.
func rowHolding(t *testing.T, rows [][]string, column int, want string) []string {
	t.Helper()

	for _, row := range rows {
		if column < len(row) && row[column] == want {
			return row
		}
	}

	t.Fatalf("no row holds %q in column %d: %v", want, column, rows)

	return nil
}

// lineHolding returns the first line of text that holds want, and fails the
// test when no line does.
func lineHolding(t *testing.T, text, want string) string {
	t.Helper()

	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}

	t.Fatalf("no line holds %q:\n%s", want, text)

	return ""
}

// codeFreeLines returns the report's lines with every fenced code block dropped
// and every code span blanked, so a link or a heading inside model text is not
// read as one. It is how the repository's own link and title checks read a page.
func codeFreeLines(body string) []string {
	var (
		out     []string
		inFence bool
		fence   string
	)

	for _, line := range strings.Split(body, "\n") {
		if inFence {
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				inFence = false
			}

			continue
		}

		if m := fenceRun.FindStringSubmatch(line); m != nil {
			inFence, fence = true, m[1]

			continue
		}

		out = append(out, blankCodeSpans(line))
	}

	return out
}

// blankCodeSpans replaces every code span of a line with spaces of the same
// length: a span is a run of backticks closed by a run of exactly as many.
func blankCodeSpans(line string) string {
	var b strings.Builder

	for i := 0; i < len(line); {
		if line[i] != '`' {
			b.WriteByte(line[i])

			i++

			continue
		}

		open := i

		for i < len(line) && line[i] == '`' {
			i++
		}

		length := i - open
		closing := -1

		for j := i; j < len(line); {
			if line[j] != '`' {
				j++

				continue
			}

			run := j

			for run < len(line) && line[run] == '`' {
				run++
			}

			if run-j == length {
				closing = run

				break
			}

			j = run
		}

		if closing < 0 {
			b.WriteString(line[open:i])

			continue
		}

		b.WriteString(strings.Repeat(" ", closing-open))

		i = closing
	}

	return b.String()
}

// linksIn returns every inline link and image destination of a rendered text,
// read as the repository's link check reads a page: never inside a code span or
// a fenced code block.
func linksIn(body string) []string {
	var out []string

	for _, line := range codeFreeLines(body) {
		for _, m := range inlineLink.FindAllStringSubmatch(line, -1) {
			out = append(out, m[1])
		}
	}

	return out
}

// titleLines returns the 1-based lines of the report's # headings, outside
// fenced code.
func titleLines(body string) []int {
	lines := codeFreeLines(body)

	var out []int

	for i, line := range lines {
		if titleHeading.MatchString(line) {
			out = append(out, i+1)
		}
	}

	return out
}

// entropy returns the Shannon entropy of s over its own byte frequencies, in
// bits per character: the quantity the encoded-run refusal measures.
func entropy(s string) float64 {
	counts := make(map[byte]int, len(s))

	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}

	keys := make([]byte, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	total := float64(len(s))
	h := 0.0

	for _, k := range keys {
		p := float64(counts[k]) / total
		h -= p * math.Log2(p)
	}

	return h
}

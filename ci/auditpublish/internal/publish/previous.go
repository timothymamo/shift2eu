package publish

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Previous is the previous merged report, as ParsePreviousReport read it: its
// findings table, its needs-investigation list and its False positives, by
// their fingerprints and their fixed vocabulary cells only.
type Previous struct {
	// parsed is set by ParsePreviousReport alone: RenderReport refuses a
	// Previous without it, the zero one and one built by hand.
	parsed         bool
	name           string
	date           time.Time
	runID          string
	findings       []previousRow
	investigation  map[Fingerprint]bool
	falsePositives []falsePositive
}

// previousRow is one row of the previous findings table. A new or open row
// also keeps its three model cells, byte for byte as escapedCell accepted
// them, so a finding that turns private can keep its public row.
type previousRow struct {
	fingerprint Fingerprint
	severity    string
	category    string
	status      string
	location    string
	scenario    string
	fix         string
}

// reportNamePattern is the name of a report a run reads: its UTC date and its
// run ID. No other file of the directory is a report.
var reportNamePattern = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2})-([1-9][0-9]{0,19})\.md$`)

// NewestReport returns the newest report name among names, a listing of
// docs/security/audits: of the names matching YYYY-MM-DD-<run_id>.md, the one
// with the latest date, compared as the name's digits, and then the largest
// run ID as a number. It returns false when no name matches.
func NewestReport(names []string) (string, bool) {
	var (
		newest            string
		newestDate, newID string
	)

	for _, name := range names {
		m := reportNamePattern.FindStringSubmatch(name)
		if m == nil {
			continue
		}

		date, id := m[1], m[2]

		if newest == "" || date > newestDate || date == newestDate && longerOrGreater(id, newID) {
			newest, newestDate, newID = name, date, id
		}
	}

	return newest, newest != ""
}

// longerOrGreater compares two decimals with no leading zero as numbers.
func longerOrGreater(a, b string) bool {
	if len(a) != len(b) {
		return len(a) > len(b)
	}

	return a > b
}

// isFalsePositive reports whether fp is a row of the False positives section.
func (p *Previous) isFalsePositive(fp Fingerprint) bool {
	for _, row := range p.falsePositives {
		if row.fingerprint == fp {
			return true
		}
	}

	return false
}

// row returns the findings table's row of fp.
func (p *Previous) row(fp Fingerprint) (previousRow, bool) {
	for _, row := range p.findings {
		if row.fingerprint == fp {
			return row, true
		}
	}

	return previousRow{}, false
}

// previousError is a previous report the core cannot read, for the rule it
// broke. It names no text of the report.
func previousError(rule string) error {
	return Refuse(CheckPreviousNotRendered, fmt.Errorf("previous report: %s: %w", rule, ErrPreviousReport))
}

// ParsePreviousReport reads the previous merged report, name being its file
// name in docs/security/audits. It checks exactly these parts against what
// RenderReport writes: the title, the eight sections in order and no other
// heading, the findings and needs-investigation tables, the private-findings
// line in the summary and in its section, the False positives table, and, in
// a first report, First report. as the Changes section's only line before the
// dependencies' line, every status new and every false positive one of its
// own findings. It does not check the other sections' text. A part that
// breaks its rule is an error wrapping ErrPreviousReport.
func ParsePreviousReport(name string, data []byte) (Previous, error) {
	m := reportNamePattern.FindStringSubmatch(name)
	if m == nil {
		return Previous{}, previousError("not a report name")
	}

	date, ok := parseDate(m[1])
	if !ok {
		return Previous{}, previousError("the name's date is no date")
	}

	text := string(data)
	if !utf8.ValidString(text) || !strings.HasSuffix(text, "\n") {
		return Previous{}, previousError("not a rendered report")
	}

	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if lines[0] != titlePrefix+m[1] {
		return Previous{}, previousError("not the report's title")
	}

	sections, err := splitSections(lines[1:])
	if err != nil {
		return Previous{}, err
	}

	prev := Previous{name: name, date: date, runID: m[2], investigation: map[Fingerprint]bool{}}

	if err := prev.readFindings(sections[headingFindings]); err != nil {
		return Previous{}, err
	}

	if err := readPrivate(sections["Summary"], sections["Private findings"]); err != nil {
		return Previous{}, err
	}

	if err := prev.readFalsePositives(sections["False positives"]); err != nil {
		return Previous{}, err
	}

	if err := prev.consistent(sections["Changes since the last audit"]); err != nil {
		return Previous{}, err
	}

	prev.parsed = true

	return prev, nil
}

// splitSections splits the lines after the title into the eight sections,
// and refuses any other heading or another order.
func splitSections(lines []string) (map[string][]string, error) {
	sections := make(map[string][]string, len(sectionHeadings))

	var (
		current string
		next    int
	)

	for _, line := range lines {
		if name, ok := strings.CutPrefix(line, "## "); ok {
			if next >= len(sectionHeadings) || name != sectionHeadings[next] {
				return nil, previousError("not the report's sections")
			}

			current = name
			next++
			sections[current] = []string{}

			continue
		}

		switch {
		case strings.HasPrefix(line, "#") && line != headingNeedsInvest:
			return nil, previousError("a heading the report does not have")
		case current == "" && line != "":
			return nil, previousError("text before the first section")
		case current != "":
			sections[current] = append(sections[current], line)
		}
	}

	if next != len(sectionHeadings) {
		return nil, previousError("not the report's sections")
	}

	return sections, nil
}

// nonBlank returns the lines that are not empty.
func nonBlank(lines []string) []string {
	var out []string

	for _, line := range lines {
		if line != "" {
			out = append(out, line)
		}
	}

	return out
}

// readFindings reads the findings table and the needs-investigation table
// under the Findings section.
func (p *Previous) readFindings(lines []string) error {
	cut := -1

	for i, line := range lines {
		if line == headingNeedsInvest {
			if cut >= 0 {
				return previousError("two needs-investigation headings")
			}

			cut = i
		}
	}

	if cut < 0 {
		return previousError("no needs-investigation heading")
	}

	public, err := readFindingsTable(nonBlank(lines[:cut]), noPublicFindings, true)
	if err != nil {
		return err
	}

	suspected, err := readFindingsTable(nonBlank(lines[cut+1:]), noneText, false)
	if err != nil {
		return err
	}

	p.findings = public

	for _, row := range suspected {
		if _, dup := p.row(row.fingerprint); dup || p.investigation[row.fingerprint] {
			return previousError("a fingerprint listed twice")
		}

		p.investigation[row.fingerprint] = true
	}

	return nil
}

// publicCategories are the categories a public finding can have.
var publicCategories = map[string]bool{
	"supply chain":             true,
	"dependency vulnerability": true,
	"configuration":            true,
	"hardening":                true,
	"denial of service":        true,
	"information disclosure":   true,
}

// readFindingsTable reads one findings table, or its empty text. allowFixed
// is set for the findings table, whose rows may be fixed ones.
func readFindingsTable(lines []string, empty string, allowFixed bool) ([]previousRow, error) {
	if len(lines) == 1 && lines[0] == empty {
		return nil, nil
	}

	if len(lines) < 3 || lines[0] != findingsHeader || lines[1] != findingsDelimiter {
		return nil, previousError("not a findings table")
	}

	var rows []previousRow

	seen := map[Fingerprint]bool{}

	for _, line := range lines[2:] {
		cells, ok := splitRow(line)
		if !ok || len(cells) != 7 {
			return nil, previousError("not a findings row of seven cells")
		}

		row := previousRow{fingerprint: Fingerprint(cells[0]), severity: cells[1], category: cells[2], status: cells[3]}

		switch {
		case !fingerprintPattern.MatchString(cells[0]):
			return nil, previousError("a fingerprint cell that is no fingerprint")
		case seen[row.fingerprint]:
			return nil, previousError("a fingerprint listed twice")
		case row.severity != "low" && row.severity != "informational":
			return nil, previousError("a severity cell outside the public severities")
		case !publicCategories[row.category]:
			return nil, previousError("a category cell outside the public categories")
		}

		switch row.status {
		case statusNew, statusOpen:
			if !escapedCell(cells[4]) || !escapedCell(cells[5]) || !escapedCell(cells[6]) {
				return nil, previousError("a model cell that is no table cell")
			}

			row.location, row.scenario, row.fix = cells[4], cells[5], cells[6]
		case statusFixed:
			if !allowFixed || cells[4] != noCell || cells[5] != noCell || cells[6] != noCell {
				return nil, previousError("not a fixed row")
			}
		default:
			return nil, previousError("a status cell outside the vocabulary")
		}

		seen[row.fingerprint] = true
		rows = append(rows, row)
	}

	return rows, nil
}

// readPrivate checks that the Summary and the Private findings section hold
// one and the same private-findings line.
func readPrivate(summary, section []string) error {
	lines := nonBlank(section)
	if len(lines) != 1 || !isPrivateLine(lines[0]) {
		return previousError("not one private-findings line")
	}

	var inSummary []string

	for _, line := range summary {
		if strings.HasPrefix(line, "- private findings: ") {
			inSummary = append(inSummary, strings.TrimPrefix(line, "- "))
		}
	}

	if len(inSummary) != 1 || inSummary[0] != lines[0] {
		return previousError("the summary's private-findings line differs from its section's")
	}

	return nil
}

func isPrivateLine(line string) bool {
	for _, s := range []PrivateState{PrivateNone, PrivateHandled, PrivateNotFiled} {
		if l, _ := s.line(); l == line {
			return true
		}
	}

	return false
}

// readFalsePositives reads the False positives table, or its empty text.
func (p *Previous) readFalsePositives(section []string) error {
	lines := nonBlank(section)

	if len(lines) == 1 && lines[0] == noneText {
		return nil
	}

	if len(lines) < 3 || lines[0] != falsePositiveHeader || lines[1] != falsePositiveDelim {
		return previousError("not a False positives table")
	}

	for _, line := range lines[2:] {
		cells, ok := splitRow(line)

		switch {
		case !ok || len(cells) != 2:
			return previousError("not a False positives row of two cells")
		case !fingerprintPattern.MatchString(cells[0]):
			return previousError("a fingerprint cell that is no fingerprint")
		case p.isFalsePositive(Fingerprint(cells[0])):
			return previousError("a fingerprint listed twice")
		case !escapedCell(cells[1]) || cells[1] == "none":
			return previousError("a reason that is no table cell")
		}

		p.falsePositives = append(p.falsePositives, falsePositive{fingerprint: Fingerprint(cells[0]), reason: cells[1]})
	}

	return nil
}

// consistent refuses a report that contradicts what RenderReport writes. A
// first report, the one whose Changes section reads First report., has no
// previous report to carry a row from: every status in it is new, and every
// False positives row is a finding of its own tables.
//
// First report. is then the section's only line before the dependencies'
// line, as RenderReport writes it: anywhere else, or beside other lines, it is
// refused, so a report cannot both be a first one and list changes.
func (p *Previous) consistent(changes []string) error {
	lines := nonBlank(changes)

	if !slices.Contains(lines, firstReport) {
		return nil
	}

	if lines[0] != firstReport || len(lines) < 2 || !strings.HasPrefix(lines[1], dependenciesPrefix) {
		return previousError("First report. is not the Changes section's only line before the dependencies")
	}

	listed := map[Fingerprint]bool{}

	for _, row := range p.findings {
		if row.status != statusNew {
			return previousError("a first report with a status other than new")
		}

		listed[row.fingerprint] = true
	}

	for fp := range p.investigation {
		listed[fp] = true
	}

	for _, row := range p.falsePositives {
		if !listed[row.fingerprint] {
			return previousError("a first report's false positive that is none of its findings")
		}
	}

	return nil
}

// splitRow splits a table row into its trimmed cells at every | that is not
// written \|, as GFM's table parser does.
func splitRow(line string) ([]string, bool) {
	if !strings.HasPrefix(line, "| ") || !strings.HasSuffix(line, " |") || len(line) < 4 {
		return nil, false
	}

	inner := line[1 : len(line)-1]

	var (
		cells []string
		cell  strings.Builder
	)

	for i := 0; i < len(inner); i++ {
		switch {
		case inner[i] == '\\' && i+1 < len(inner) && inner[i+1] == '|':
			cell.WriteString(`\|`)
			i++
		case inner[i] == '|':
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteByte(inner[i])
		}
	}

	return append(cells, strings.TrimSpace(cell.String())), true
}

// escapedCell reports whether cell is exactly what TableCell writes for some
// text no field of the schema refuses: a cell that is not cannot be carried
// into a new report as it is.
func escapedCell(cell string) bool {
	if cell == "none" {
		return true
	}

	n := 0
	for n < len(cell) && cell[n] == '`' {
		n++
	}

	if n == 0 || len(cell) < 2*n+2 {
		return false
	}

	text := strings.ReplaceAll(cell[n+1:len(cell)-n-1], `\|`, "|")
	if text == "" {
		return false
	}

	for _, r := range text {
		if !textRune(r) {
			return false
		}
	}

	return TableCell(text) == cell
}

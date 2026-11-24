package publish

import (
	"fmt"
	"strings"
)

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
	if p == nil || !p.parsed || len(p.findings) == 0 {
		return nil
	}

	out := make([]PreviousFinding, 0, len(p.findings))

	for _, row := range p.findings {
		f := PreviousFinding{
			Fingerprint: row.fingerprint,
			Severity:    row.severity,
			Category:    row.category,
			Status:      row.status,
			Report:      reportsDir + p.name,
		}

		if row.status != statusFixed {
			f.Location, f.Scenario, f.Fix = unescapeCell(row.location), unescapeCell(row.scenario), unescapeCell(row.fix)
		}

		out = append(out, f)
	}

	return out
}

// unescapeCell undoes TableCell for a cell escapedCell accepted: the run of
// backticks and the one space at each end removed, every \| read as |. The
// cell none is the empty text.
func unescapeCell(cell string) string {
	if cell == "none" {
		return ""
	}

	n := 0
	for n < len(cell) && cell[n] == '`' {
		n++
	}

	if n == 0 || len(cell) < 2*n+2 {
		return ""
	}

	return strings.ReplaceAll(cell[n+1:len(cell)-n-1], `\|`, "|")
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
func RenderIssue(f PreviousFinding, run Run) (IssueText, error) {
	if err := run.validate(); err != nil {
		return IssueText{}, err
	}

	switch {
	case !fingerprintPattern.MatchString(string(f.Fingerprint)):
		return IssueText{}, fmt.Errorf("issue: a fingerprint that is no fingerprint: %w", ErrPreviousReport)
	case f.Severity != "low" && f.Severity != "informational":
		return IssueText{}, fmt.Errorf("issue: a severity outside the public severities: %w", ErrPreviousReport)
	case !publicCategories[f.Category]:
		return IssueText{}, fmt.Errorf("issue: a category outside the public categories: %w", ErrPreviousReport)
	case f.Status != statusNew && f.Status != statusOpen:
		return IssueText{}, fmt.Errorf("issue: a status other than new or open: %w", ErrPreviousReport)
	}

	fields := []string{CodeBlock(f.Location), CodeBlock(f.Scenario), CodeBlock(f.Fix)}

	body := "Security audit finding " + string(f.Fingerprint) + ", " + f.Status + " in the scheduled security audit's report.\n" +
		"\n" +
		"Severity: " + f.Severity + "\n" +
		"Category: " + f.Category + "\n" +
		"\n" +
		"Location:\n" +
		"\n" + fields[0] + "\n" +
		"\n" +
		"Scenario:\n" +
		"\n" + fields[1] + "\n" +
		"\n" +
		"Recommended fix:\n" +
		"\n" + fields[2] + "\n" +
		"\n" +
		"Report: " + f.Report + "\n" +
		"Run: " + run.URL() + "\n"

	return IssueText{
		Fingerprint: f.Fingerprint,
		Title:       "Security audit finding " + string(f.Fingerprint) + ": " + f.Category + " (" + f.Severity + ")",
		Body:        body,
		ModelFields: fields,
	}, nil
}

package publish

import (
	"strconv"
	"strings"
)

// Rendered is a rendered report: its path in the repository, its text, and
// every escaped fragment of model text the text holds, which ScanPublic's
// encoded-run refusal reads.
type Rendered struct {
	Path        string
	Body        string
	ModelFields []string
	// PublicTotal is the summary's public total: the kept findings placed
	// public or needing investigation, the count the run's summary line
	// states (Outcome.WithCounts).
	PublicTotal int
}

// Templates are the run's fixed texts besides the report: the branch, the pull
// request's title and body, and the commit message. They hold no model text.
type Templates struct {
	Branch        string
	PRTitle       string
	PRBody        string
	CommitMessage string
}

// The report's fixed texts, which ParsePreviousReport reads back.
const (
	reportsDir          = "docs/security/audits/"
	titlePrefix         = "# Security audit report, "
	headingFindings     = "Findings"
	headingNeedsInvest  = "### Needs investigation"
	findingsHeader      = "| Fingerprint | Severity | Category | Status | Location | Scenario | Recommended fix |"
	findingsDelimiter   = "|---|---|---|---|---|---|---|"
	falsePositiveHeader = "| Fingerprint | Reason |"
	falsePositiveDelim  = "|---|---|"
	coverageHeader      = "| Check | Coverage |"
	coverageDelimiter   = "|---|---|"
	noPublicFindings    = "No public findings."
	noneText            = "None."
	firstReport         = "First report."
	dependenciesPrefix  = "Dependencies changed since the last audit:"
)

// sectionHeadings are the report's ## sections, in the template's order.
var sectionHeadings = []string{
	"Summary",
	"Scope",
	headingFindings,
	"Private findings",
	"Coverage",
	"Changes since the last audit",
	"False positives",
	"Limits",
}

// reportName is the name of a report the core reads, and of one it writes.
func reportName(run Run) string {
	return run.Date + "-" + run.ID + ".md"
}

// RenderReport renders the run's report at docs/security/audits/<date>-<run
// id>.md. It refuses an input ParseResult would have refused, a Run NewRun
// would have refused, two findings with one fingerprint, and a private state
// that does not match the findings (ADR 0040, "Rendering").
func RenderReport(in ReportInput) (Rendered, error) {
	p, err := newPlan(in)
	if err != nil {
		return Rendered{}, err
	}

	r := reportWriter{}

	r.line(titlePrefix + p.run.Date)
	r.summary(p)
	r.scope(p)
	r.findings(p)
	r.section("Private findings")
	r.line(p.privateLine)
	r.coverage(p)
	r.changes(p)
	r.falsePositives(p)
	r.limits(p)

	return Rendered{
		Path:        reportsDir + reportName(p.run),
		Body:        r.b.String(),
		ModelFields: r.fields,
	}, nil
}

// RenderTemplates renders the branch, the pull request's title and body and
// the commit message from the run's date, ID and URL, the commit, the tag, the
// public total and the private-findings line. It refuses what RenderReport
// refuses.
func RenderTemplates(in ReportInput) (Templates, error) {
	p, err := newPlan(in)
	if err != nil {
		return Templates{}, err
	}

	date, id, total := p.run.Date, p.run.ID, strconv.Itoa(p.total)
	commit, tag := p.result.Commit, p.result.ReleaseTag

	return Templates{
		Branch:  "security-audit/" + date + "-" + id,
		PRTitle: "Security audit report, " + date + " (run " + id + ")",
		PRBody: "Security audit report of run " + id + ", " + date + " (UTC).\n" +
			"\n" +
			"Commit: " + commit + "\n" +
			"Release: " + tag + "\n" +
			"Public findings: " + total + "\n" +
			p.privateLine + "\n" +
			"Run: " + p.run.URL() + "\n" +
			"\n" +
			"The report adds " + reportsDir + reportName(p.run) + " and nothing else.",
		CommitMessage: "docs(security): audit report " + date + ", run " + id + "\n" +
			"\n" +
			"Commit " + commit + ", release " + tag + ", public findings " + total + ", " + p.privateLine + ".\n" +
			"Run: " + p.run.URL(),
	}, nil
}

// reportWriter builds a report's text and records each fragment of model text
// it writes.
type reportWriter struct {
	b      strings.Builder
	fields []string
}

func (r *reportWriter) line(s string) {
	r.b.WriteString(s)
	r.b.WriteByte('\n')
}

func (r *reportWriter) section(name string) {
	r.line("")
	r.line("## " + name)
	r.line("")
}

// model records an escaped fragment of model text and returns it.
func (r *reportWriter) model(fragment string) string {
	r.fields = append(r.fields, fragment)

	return fragment
}

func (r *reportWriter) summary(p plan) {
	r.section("Summary")
	r.line("The audit of commit " + p.result.Commit + " and release " + p.result.ReleaseTag +
		" by the scheduled security audit. [Security](../README.md) describes the audit and its reports.")
	r.line("")

	public := "- Public findings: " + strconv.Itoa(p.total)

	var parts []string

	for _, severity := range []string{"critical", "high", "medium", "low", "informational"} {
		if n := p.bySeverity[severity]; n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+severity)
		}
	}

	if len(parts) > 0 {
		public += " (" + strings.Join(parts, ", ") + ")"
	}

	r.line(public)
	r.line("- Listed under needs investigation: " + strconv.Itoa(len(p.investigation)))
	r.line("- " + p.privateLine)
}

func (r *reportWriter) scope(p plan) {
	r.section("Scope")
	r.line("- Commit: " + p.result.Commit)

	// The digest is the model's text, however tight its pattern: it reaches
	// the report as a code span, and the scanner reads it as a model field, so
	// the hex rule refuses a digest that decodes to printable ASCII.
	image := "image not pulled"
	if p.result.ImageDigest != "" {
		image = "image " + r.model(CodeSpan(p.result.ImageDigest))
	}

	r.line("- Release: " + p.result.ReleaseTag + ", " + image)

	if len(p.result.Tools) == 0 {
		r.line("- Tool versions: none")
	} else {
		r.line("- Tool versions:")

		for _, tool := range p.result.Tools {
			r.line("  - " + tool.Name + " " + r.model(CodeSpan(tool.Version)))
		}
	}

	r.line("- Model: " + ModelName + ", effort " + ModelEffort)
	r.line("- Run: [run " + p.run.ID + "](" + p.run.URL() + "), " + p.run.Date + " (UTC)")
}

func (r *reportWriter) findings(p plan) {
	r.section(headingFindings)
	r.table(p.public, noPublicFindings)
	r.line("")
	r.line(headingNeedsInvest)
	r.line("")
	r.table(p.investigation, noneText)
}

// table writes a findings table, or empty when it has no row.
func (r *reportWriter) table(rows []tableRow, empty string) {
	if len(rows) == 0 {
		r.line(empty)

		return
	}

	r.line(findingsHeader)
	r.line(findingsDelimiter)

	for _, row := range rows {
		cells := []string{string(row.fingerprint), row.severity, row.category, row.status}

		if row.status == statusFixed {
			cells = append(cells, row.location, row.scenario, row.fix)
		} else {
			cells = append(cells, r.model(row.location), r.model(row.scenario), r.model(row.fix))
		}

		r.line("| " + strings.Join(cells, " | ") + " |")
	}
}

func (r *reportWriter) coverage(p plan) {
	r.section("Coverage")
	r.line(coverageHeader)
	r.line(coverageDelimiter)

	for i, c := range checkNames {
		r.line("| " + c.name + " | " + p.coverage[i] + " |")
	}
}

func (r *reportWriter) changes(p plan) {
	r.section("Changes since the last audit")

	if p.previous == nil {
		r.line(firstReport)
	} else {
		r.line("The previous report is " + p.previous.name + ".")
		r.line("")

		if len(p.changeLines) == 0 {
			r.line("No public finding changed its status.")
		}

		for _, l := range p.changeLines {
			r.line(l)
		}

		if p.gapDays > GapThresholdDays {
			r.line("")
			r.line("Gap in the schedule: the previous report is " + strconv.Itoa(p.gapDays) + " days older than this one.")
		}
	}

	r.line("")

	if len(p.result.Changes.Dependencies) == 0 {
		r.line(dependenciesPrefix + " none.")
	} else {
		r.line(dependenciesPrefix)
		r.line("")

		for _, d := range p.result.Changes.Dependencies {
			r.line("- " + r.model(CodeSpan(d)))
		}
	}

	r.line("")
	r.line("Narrative: " + r.model(CodeSpan(p.result.Changes.Narrative)))
}

func (r *reportWriter) falsePositives(p plan) {
	r.section("False positives")

	if len(p.falsePositives) == 0 {
		r.line(noneText)

		return
	}

	r.line(falsePositiveHeader)
	r.line(falsePositiveDelim)

	for _, fp := range p.falsePositives {
		r.line("| " + string(fp.fingerprint) + " | " + r.model(fp.reason) + " |")
	}
}

func (r *reportWriter) limits(p plan) {
	r.section("Limits")

	if len(p.result.Limits) == 0 {
		r.line(noneText)

		return
	}

	for _, l := range p.result.Limits {
		r.line("- " + r.model(CodeSpan(l)))
	}
}

package publish

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// PrivateState is what the public texts say about private findings: only
// whether they exist and were handled, never how many.
type PrivateState int

// The three private states. The entry point sets the state after filing the
// draft advisories.
const (
	// PrivateNone is a run with no private finding.
	PrivateNone PrivateState = iota
	// PrivateHandled is a run whose private findings were filed as draft
	// advisories.
	PrivateHandled
	// PrivateNotFiled is a run whose private findings could not be filed.
	PrivateNotFiled
)

// line returns the state's private-findings line.
func (s PrivateState) line() (string, bool) {
	switch s {
	case PrivateNone:
		return "private findings: none", true
	case PrivateHandled:
		return "private findings: handled privately", true
	case PrivateNotFiled:
		return "private findings: not filed", true
	default:
		return "", false
	}
}

// ReportInput is what RenderReport and RenderTemplates render: the accepted
// result, the run, the private state the entry point set after filing, and
// the previous merged report, nil when this run is the first.
type ReportInput struct {
	Result   Result
	Run      Run
	Private  PrivateState
	Previous *Previous
}

// The statuses of a public finding.
const (
	statusNew   = "new"
	statusOpen  = "open"
	statusFixed = "fixed"
)

// noCell is the cell of a fixed row the run has no value for.
const noCell = "—"

// tableRow is one row of the findings or the needs-investigation table. Every
// model cell is escaped already.
type tableRow struct {
	fingerprint Fingerprint
	severity    string
	category    string
	status      string
	location    string
	scenario    string
	fix         string
}

// falsePositive is one row of the False positives table: a fingerprint and
// its reason, a table cell.
type falsePositive struct {
	fingerprint Fingerprint
	reason      string
}

// plan is everything a run's public texts state, derived once from the input
// and rendered by RenderReport and RenderTemplates alike.
type plan struct {
	run            Run
	result         Result
	privateLine    string
	public         []tableRow
	investigation  []tableRow
	changeLines    []string
	falsePositives []falsePositive
	coverage       []string
	// total counts the kept findings placed public or needing
	// investigation; bySeverity counts them per severity.
	total      int
	bySeverity map[string]int
	previous   *Previous
	gapDays    int
}

// keptFinding is a finding that reaches the report: placed public or needing
// investigation, and not dropped as a false positive.
type keptFinding struct {
	finding     Finding
	fingerprint Fingerprint
	placement   Placement
}

// newPlan derives the plan: it validates the input, places each finding,
// drops recorded false positives, derives the statuses, the fixed rows, the
// Changes lines, the False positives rows and the coverage.
func newPlan(in ReportInput) (plan, error) {
	if err := in.Run.validate(); err != nil {
		return plan{}, err
	}

	if err := validateResult(in.Result); err != nil {
		return plan{}, err
	}

	runDate, _ := parseDate(in.Run.Date)

	p := plan{run: in.Run, result: in.Result, previous: in.Previous, bySeverity: map[string]int{}}

	if p.previous != nil {
		if !p.previous.parsed {
			return plan{}, fmt.Errorf("previous report: not read by ParsePreviousReport: %w", ErrPreviousReport)
		}

		// Unix seconds, not time.Time.Sub, which saturates at about 292
		// years: a previous report of the year 0001 gives its true gap.
		p.gapDays = int((runDate.Unix() - p.previous.date.Unix()) / secondsPerDay)
	}

	kept, found, private, err := keep(in.Result.Findings, in.Previous)
	if err != nil {
		return plan{}, err
	}

	line, ok := in.Private.line()

	switch {
	case !ok:
		return plan{}, fmt.Errorf("private state: not one of the three: %w", ErrConfig)
	case len(private) > 0 && in.Private == PrivateNone:
		return plan{}, fmt.Errorf("private state: none, but a finding is private: %w", ErrConfig)
	case len(private) == 0 && in.Private != PrivateNone:
		return plan{}, fmt.Errorf("private state: set, but no finding is private: %w", ErrConfig)
	}

	p.privateLine = line

	carried := p.rows(kept, found, private)
	p.falsePositiveRows(kept)
	p.coverageRows(kept, carried)

	return p, nil
}

// secondsPerDay turns the Unix seconds between two report dates into days.
const secondsPerDay = 24 * 60 * 60

// keep places every finding and returns the kept ones in the tables' order,
// the plain fingerprints of every finding of the run (private and dropped
// ones included: a finding the run found is no fixed one), and the check of
// each finding placed private, by its plain fingerprint. Two findings with one
// rule, file and sink are refused: one fingerprint is one finding.
func keep(findings []Finding, previous *Previous) ([]keptFinding, map[Fingerprint]bool, map[Fingerprint]string, error) {
	var kept []keptFinding

	found := make(map[Fingerprint]bool, len(findings))
	first := make(map[Fingerprint]int, len(findings))
	private := map[Fingerprint]string{}

	for i, f := range findings {
		fp := PlainFingerprint(f.Rule, f.File, f.Sink)

		if j, seen := first[fp]; seen {
			return nil, nil, nil, invalid("findings["+strconv.Itoa(i)+"]",
				"the same rule, file and sink as findings["+strconv.Itoa(j)+"]")
		}

		first[fp] = i
		found[fp] = true

		placement := Place(f)

		if placement.Private() {
			private[fp] = f.Check

			continue
		}

		if previous != nil && previous.isFalsePositive(fp) {
			continue
		}

		kept = append(kept, keptFinding{finding: f, fingerprint: fp, placement: placement})
	}

	slices.SortFunc(kept, func(a, b keptFinding) int {
		return tableOrder(a.finding.Severity, a.fingerprint, b.finding.Severity, b.fingerprint)
	})

	return kept, found, private, nil
}

// rows builds both tables, the statuses and the Changes lines, and returns
// the checks of the findings it carried (ADR 0040, "The carried row"): a
// finding the previous report listed as public new or open that this run
// places private keeps its public row, open, with the previous report's
// cells byte for byte and a still-open Changes line, so the report shows
// nothing of this run's private placement. Every other previous row this run
// did not find becomes a fixed row.
func (p *plan) rows(kept []keptFinding, found map[Fingerprint]bool, private map[Fingerprint]string) map[string]bool {
	carried := map[string]bool{}
	for _, k := range kept {
		status, change := p.status(k)

		row := tableRow{
			fingerprint: k.fingerprint,
			severity:    k.finding.Severity,
			category:    k.finding.Category,
			status:      status,
			location:    TableCell(location(k.finding)),
			scenario:    TableCell(k.finding.Scenario),
			fix:         TableCell(k.finding.Fix),
		}

		p.total++
		p.bySeverity[k.finding.Severity]++

		if k.placement == PlacementInvestigation {
			p.investigation = append(p.investigation, row)

			continue
		}

		p.public = append(p.public, row)

		if change != "" {
			p.changeLines = append(p.changeLines, "- "+string(k.fingerprint)+": "+change)
		}
	}

	if p.previous == nil {
		return carried
	}

	for _, prev := range p.previous.findings {
		if check, ok := private[prev.fingerprint]; ok && prev.status != statusFixed {
			p.public = append(p.public, tableRow{
				fingerprint: prev.fingerprint,
				severity:    prev.severity,
				category:    prev.category,
				status:      statusOpen,
				location:    prev.location,
				scenario:    prev.scenario,
				fix:         prev.fix,
			})
			p.changeLines = append(p.changeLines, "- "+string(prev.fingerprint)+": still open")
			p.total++
			p.bySeverity[prev.severity]++
			carried[check] = true

			continue
		}

		if prev.status == statusFixed || found[prev.fingerprint] {
			continue
		}

		p.public = append(p.public, tableRow{
			fingerprint: prev.fingerprint,
			severity:    prev.severity,
			category:    prev.category,
			status:      statusFixed,
			location:    noCell,
			scenario:    noCell,
			fix:         noCell,
		})
		p.changeLines = append(p.changeLines, "- "+string(prev.fingerprint)+": fixed")
	}

	slices.SortFunc(p.public, func(a, b tableRow) int {
		return tableOrder(a.severity, a.fingerprint, b.severity, b.fingerprint)
	})
	slices.Sort(p.changeLines)

	return carried
}

// tableOrder is the findings tables' order: most severe first, then by
// fingerprint.
func tableOrder(severityA string, a Fingerprint, severityB string, b Fingerprint) int {
	if ra, rb := severityRank[severityA], severityRank[severityB]; ra != rb {
		return ra - rb
	}

	return strings.Compare(string(a), string(b))
}

// status derives a kept finding's status from the previous report, and the
// Changes line of a public one.
func (p *plan) status(k keptFinding) (string, string) {
	if p.previous == nil {
		return statusNew, ""
	}

	prev, inTable := p.previous.row(k.fingerprint)
	inInvestigation := p.previous.investigation[k.fingerprint]

	if k.placement == PlacementInvestigation {
		if inTable || inInvestigation {
			return statusOpen, ""
		}

		return statusNew, ""
	}

	switch {
	case inTable && prev.status == statusFixed:
		return statusOpen, "regressed"
	case inTable, inInvestigation:
		return statusOpen, "still open"
	default:
		return statusNew, ""
	}
}

// falsePositiveRows carries the previous report's rows forward unchanged, then
// adds one row for each kept finding with a dismissal whose fingerprint has no
// row yet. A private finding is never kept, so its dismissal is never written.
func (p *plan) falsePositiveRows(kept []keptFinding) {
	listed := map[Fingerprint]bool{}

	if p.previous != nil {
		for _, fp := range p.previous.falsePositives {
			p.falsePositives = append(p.falsePositives, fp)
			listed[fp.fingerprint] = true
		}
	}

	for _, k := range kept {
		if k.finding.Dismissal == "" || listed[k.fingerprint] {
			continue
		}

		p.falsePositives = append(p.falsePositives, falsePositive{
			fingerprint: k.fingerprint,
			reason:      TableCell(k.finding.Dismissal),
		})
		listed[k.fingerprint] = true
	}
}

// coverageRows derives each check's coverage, in the table's order: findings
// when it ran and a kept or a carried finding is its, run when it ran
// otherwise (its only findings private included), and not run (<why>) when it
// did not.
func (p *plan) coverageRows(kept []keptFinding, carried map[string]bool) {
	withFindings := map[string]bool{}
	for check := range carried {
		withFindings[check] = true
	}

	for _, k := range kept {
		withFindings[k.finding.Check] = true
	}

	checks := map[string]Check{}
	for _, c := range p.result.Checks {
		checks[c.ID] = c
	}

	for _, c := range checkNames {
		check := checks[c.id]

		switch {
		case !check.Ran:
			p.coverage = append(p.coverage, whyNotRun[check.NotRun])
		case withFindings[c.id]:
			p.coverage = append(p.coverage, "findings")
		default:
			p.coverage = append(p.coverage, "run")
		}
	}
}

// location is a finding's file:line, or its file when the line is 0. It is
// model text, so it reaches a table only as a table cell.
func location(f Finding) string {
	if f.Line == 0 {
		return f.File
	}

	return f.File + ":" + strconv.Itoa(f.Line)
}

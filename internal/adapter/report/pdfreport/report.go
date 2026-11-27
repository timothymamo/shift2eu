package pdfreport

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
)

// Reporter renders an assessment, or a comparison of several targets'
// assessments, as a PDF. It holds only the version line it was built with, so
// one Reporter may render any number of reports, concurrently too.
type Reporter struct {
	versionLine string
}

// New returns a PDF reporter whose every page footer and About this report
// name versionLine: the line shift2eu version prints for the build, which the
// composition root passes, since an adapter may not import internal/version.
func New(versionLine string) *Reporter {
	return &Reporter{versionLine: versionLine}
}

// Report writes the PDF report of a and meta to w, laid out as the package
// documentation describes. It builds the whole file before writing it in one
// call, so w never receives part of a report; a write error is returned,
// wrapped. When ctx is already done Report writes nothing and returns
// ctx.Err().
func (r *Reporter) Report(ctx context.Context, w io.Writer, a assessment.Assessment, meta analysis.ReportMeta) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	subject := []seg{value(furnitureSize, a.Target)}

	file := r.render(subject, func(d *document) {
		d.paragraph(0, titleSize, titleLeading, words(helveticaBold, titleSize, "Migration assessment: "),
			value(titleSize, a.Target))
		writeFacts(d, meta, a.Source, []assessment.Assessment{a}, false)

		noted := new(bool)
		writeBody(d, a, 1, noted)
		writeDetails(d, a, 1)
		writeHow(d, hasEstimates(a))
		writeAbout(d, r.versionLine, meta, a.Source, []assessment.Assessment{a})
	})

	if _, err := io.WriteString(w, file); err != nil {
		return fmt.Errorf("write pdf report: %w", err)
	}

	return nil
}

// render lays the report out with draw, then adds the page header naming
// subject and the footer with the page number and the version line to every
// page, and returns the whole file.
func (r *Reporter) render(subject []seg, draw func(d *document)) string {
	header := layoutSegs(contentWidth,
		append([]seg{words(helvetica, furnitureSize, "shift2eu migration assessment: ")}, subject...)...)
	version := layoutSegs(contentWidth, value(furnitureSize, r.versionLine))

	headerBottom := headerTop - len(header)*furnitureLeading
	footerTop := footerBase + (len(version)+1)*furnitureLeading

	d := newDocument(headerBottom-ruleGap-bodyGap, footerTop+ruleGap+bodyGap)
	draw(d)

	for i, p := range d.pages {
		furniture := &document{pages: []*page{p}, y: headerTop}

		for _, line := range header {
			furniture.drawLine(line, marginLeft, furnitureSize, furnitureLeading)
		}

		p.rules = append(p.rules, rule{x: marginLeft, y: headerBottom - ruleGap/2, w: contentWidth},
			rule{x: marginLeft, y: footerTop + ruleGap/2, w: contentWidth})

		furniture.y = footerTop

		for _, line := range version {
			furniture.drawLine(line, marginLeft, furnitureSize, furnitureLeading)
		}

		pageLine := layoutSegs(contentWidth, words(helvetica, furnitureSize,
			"Page "+strconv.Itoa(i+1)+" of "+strconv.Itoa(len(d.pages))))
		furniture.drawLine(pageLine[0], marginLeft, furnitureSize, furnitureLeading)
	}

	return writeFile(d.pages)
}

// writeFacts writes the header facts under the title: the inventory file (left
// out when it has no name), the source, one catalog line per assessment and
// one region line per assessment that has a region. named says whether a
// region line names its target, as a comparison's does.
func writeFacts(d *document, meta analysis.ReportMeta, source string, targets []assessment.Assessment, named bool) {
	d.space(4)

	if meta.InventoryName != "" {
		d.body(words(helvetica, bodySize, "Inventory file: "), value(bodySize, meta.InventoryName))
	}

	d.body(words(helvetica, bodySize, "Source: "), value(bodySize, source))

	for _, a := range targets {
		d.body(catalogLine(bodySize, "Catalog: ", a)...)
	}

	for _, a := range targets {
		line := regionLine(bodySize, a)
		if line == nil {
			continue
		}

		prefix := []seg{words(helvetica, bodySize, "Region: ")}
		if named {
			prefix = append(prefix, value(bodySize, a.Target), words(helvetica, bodySize, ": "))
		}

		d.body(append(prefix, line...)...)
	}
}

// catalogLine renders a target's catalog snapshot: its display name, source
// URL and retrieval date, a UTC calendar date.
func catalogLine(size int, label string, a assessment.Assessment) []seg {
	return []seg{
		words(helvetica, size, label),
		value(size, a.Target),
		words(helvetica, size, " snapshot from "),
		value(size, a.Snapshot.SourceURL),
		words(helvetica, size, ", retrieved "+a.Snapshot.RetrievedOn.UTC().Format(dateLayout)),
	}
}

// regionLine renders the region of a with how it was chosen: the region
// --region chose, else the target's default for estimates, else nothing.
func regionLine(size int, a assessment.Assessment) []seg {
	switch {
	case a.Region != "":
		return []seg{value(size, a.Region), words(helvetica, size, " (chosen with --region)")}
	case a.EstimateRegion != "":
		return []seg{value(size, a.EstimateRegion), words(helvetica, size, " (the target's default for estimates)")}
	default:
		return nil
	}
}

// writeBody writes a's sections from Summary to Cost estimate at the heading
// level given. noted says whether the report has written the global note yet.
func writeBody(d *document, a assessment.Assessment, level int, noted *bool) {
	writeSummary(d, a, level)
	writeResources(d, a, level, noted)
	writeRequiredChanges(d, a.Verdicts, level)
	writeBlockers(d, a.Blockers, a.Verdicts, level)

	if a.ShowWorkloads() {
		writeWorkloads(d, a, level)
	}

	if hasEstimates(a) {
		writeCostEstimate(d, a, level)
	}
}

// fitCounts are a summary's rows: each fit with its count, then the total.
func fitCounts(s assessment.Summary) [][2]string {
	return [][2]string{
		{string(mapping.Direct), strconv.Itoa(s.Direct)},
		{string(mapping.Partial), strconv.Itoa(s.Partial)},
		{string(mapping.None), strconv.Itoa(s.None)},
		{"total", strconv.Itoa(s.Total)},
	}
}

// writeSummary writes the number of resources per fit and in total, and with
// estimates on the monthly total.
func writeSummary(d *document, a assessment.Assessment, level int) {
	d.title(level, "Summary")

	t := table{widths: []int{120, 80}, header: headerRow("Fit", "Resources")}
	for _, row := range fitCounts(a.Summary) {
		t.rows = append(t.rows, []cell{{{words(helvetica, tableSize, row[0])}}, {{words(helvetica, tableSize, row[1])}}})
	}

	d.drawTable(t)

	if hasEstimates(a) {
		d.body(append([]seg{words(helvetica, bodySize, "Monthly estimate: ")},
			totalSegs(bodySize, *a.Total, estimatesOf(a.Verdicts))...)...)
	}
}

// writeResources writes one table row per verdict, in the assessment's order:
// the ID, category, fit and best offering, and with estimates on the plan and
// the monthly estimate.
func writeResources(d *document, a assessment.Assessment, level int, noted *bool) {
	d.title(level, "Resources")

	if len(a.Verdicts) == 0 {
		d.body(words(helvetica, bodySize, "None."))

		return
	}

	estimates := hasEstimates(a)

	t := table{widths: []int{140, 120, 50, 185}, header: headerRow("ID", "Category", "Fit", "Best offering")}
	if estimates {
		t.widths = []int{85, 85, 35, 110, 80, 100}
		t.header = headerRow("ID", "Category", "Fit", "Best offering", "Plan", "Monthly estimate")
	}

	for _, v := range a.Verdicts {
		row := []cell{
			{{value(tableSize, v.Resource.ID)}},
			{{value(tableSize, string(v.Resource.Category))}},
			{{words(helvetica, tableSize, string(v.Fit))}},
			{bestOfferingSegs(tableSize, v)},
		}

		if estimates {
			row = append(row, cell{planCellSegs(tableSize, v.Estimate)}, cell{estimateCellSegs(tableSize, v.Estimate)})
		}

		t.rows = append(t.rows, row)
	}

	d.drawTable(t)
	writeGlobalNote(d, a.Verdicts, noted)
}

// writeGlobalNote writes the global note under the first table that shows a
// global offering, once per report.
func writeGlobalNote(d *document, verdicts []assessment.Verdict, noted *bool) {
	if *noted || !slices.ContainsFunc(verdicts, assessment.Verdict.ShowsGlobal) {
		return
	}

	d.body(words(helvetica, bodySize, globalNoteText))

	*noted = true
}

// none writes "None." for an empty list.
func none(d *document) { d.body(words(helvetica, bodySize, "None.")) }

// writeRequiredChanges writes one item per partial verdict with required
// changes: the capabilities its best offering lacks, and that offering.
func writeRequiredChanges(d *document, verdicts []assessment.Verdict, level int) {
	d.title(level, "Required changes")

	items := 0

	for _, v := range verdicts {
		listed := v.ChangesToList()
		if len(listed) == 0 {
			continue
		}

		segs := []seg{value(bodySize, v.Resource.ID), words(helvetica, bodySize, ": ")}
		segs = append(segs, valueList(bodySize, capabilityNames(listed))...)
		segs = append(segs, words(helvetica, bodySize, " (not offered by "), value(bodySize, v.BestOffering))
		segs = append(segs, globalMark(bodySize, v.BestOfferingGlobal())...)
		segs = append(segs, words(helvetica, bodySize, ")"))
		d.item(0, segs...)

		items++
	}

	if items == 0 {
		none(d)
	}
}

// capabilityNames returns capabilities as strings.
func capabilityNames[C ~string](capabilities []C) []string {
	out := make([]string, 0, len(capabilities))
	for _, c := range capabilities {
		out = append(out, string(c))
	}

	return out
}

// writeBlockers writes one item per blocker, then one per "affected by a
// blocker on" note, or "None.".
func writeBlockers(d *document, blockers []assessment.Blocker, verdicts []assessment.Verdict, level int) {
	d.title(level, "Blockers")

	if len(blockers) == 0 {
		none(d)

		return
	}

	writeBlockerItems(d, blockers, 0)

	for _, v := range verdicts {
		for _, id := range v.AffectedBy {
			d.item(0, value(bodySize, v.Resource.ID), words(helvetica, bodySize, ": affected by a blocker on "),
				value(bodySize, id))
		}
	}
}

// writeBlockerItems writes one item per blocker, "ID: reason".
func writeBlockerItems(d *document, blockers []assessment.Blocker, indent int) {
	for _, b := range blockers {
		d.item(indent, append([]seg{value(bodySize, b.ResourceID), words(helvetica, bodySize, ": ")},
			reasonSegs(bodySize, b.Reason)...)...)
	}
}

// writeWorkloads writes one subsection per workload group, "No workload" last:
// its fit counts, with estimates its total and source cost, and its blockers.
func writeWorkloads(d *document, a assessment.Assessment, level int) {
	d.title(level, "Workloads")

	estimates := workloadEstimates(a)
	size := headingSize(level + 1)

	for i, w := range a.Workloads {
		if w.NoWorkload {
			d.title(level+1, "No workload")
		} else {
			d.section(level+1, value(size, w.Name))
		}

		d.body(words(helvetica, bodySize, "Fit: "+strconv.Itoa(w.Summary.Direct)+" direct, "+
			strconv.Itoa(w.Summary.Partial)+" partial, "+strconv.Itoa(w.Summary.None)+" none"))

		if w.Total != nil {
			segs := append([]seg{words(helvetica, bodySize, "Monthly estimate: ")},
				totalSegs(bodySize, *w.Total, estimates[i])...)
			d.body(append(segs, words(helvetica, bodySize, ", "+basisNote(a)))...)

			for _, line := range sourceCostLines(bodySize, w.SourceCost) {
				d.body(line...)
			}
		}

		if len(w.Blockers) == 0 {
			d.body(words(helvetica, bodySize, "Blockers: none"))

			continue
		}

		d.body(words(helvetica, bodySize, "Blockers:"))
		writeBlockerItems(d, w.Blockers, bulletIndent)
	}
}

// writeCostEstimate writes the cost summary: the total, its basis, the region
// the prices are for, the hours basis and the source's cost per currency.
func writeCostEstimate(d *document, a assessment.Assessment, level int) {
	d.title(level, "Cost estimate")

	d.item(0, append([]seg{words(helvetica, bodySize, "Total: ")},
		totalSegs(bodySize, *a.Total, estimatesOf(a.Verdicts))...)...)
	d.item(0, words(helvetica, bodySize, "Estimate: "+basisNote(a)+"; "+fixedFeeNote))
	d.item(0, regionSegs(bodySize, "Prices in ", a.EstimateRegion)...)
	d.item(0, words(helvetica, bodySize, "Hours: "+hoursLine(a)))

	for _, line := range sourceCostLines(bodySize, a.SourceCost) {
		d.item(0, line...)
	}
}

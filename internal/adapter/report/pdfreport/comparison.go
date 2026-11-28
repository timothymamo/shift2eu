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

// ReportComparison writes the PDF comparison report of c and meta to w, laid
// out as the package documentation describes: the title block naming every
// target, the comparison, one section per target, then How this report is
// worked out and About this report once.
//
// The comparison must hold at least one target, and every target must assess
// the same resources in the same order (assessment.Comparison.Validate).
// Otherwise ReportComparison writes nothing and returns an error. When ctx is
// already done it writes nothing and returns ctx.Err().
func (r *Reporter) ReportComparison(ctx context.Context, w io.Writer, c assessment.Comparison, meta analysis.ReportMeta) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(c.Targets) == 0 {
		// The package may not import errors (depguard reporters-stay-offline).
		return fmt.Errorf("render pdf comparison: the comparison holds no target")
	}

	if err := c.Validate(); err != nil {
		return fmt.Errorf("render pdf comparison: %w", err)
	}

	assessments := make([]assessment.Assessment, 0, len(c.Targets))
	for _, t := range c.Targets {
		assessments = append(assessments, t.Assessment)
	}

	source := assessments[0].Source
	subject := []seg{words(helvetica, furnitureSize, "comparison of "+strconv.Itoa(len(c.Targets))+" targets")}

	file := r.render(subject, func(d *document) {
		title := []seg{words(helveticaBold, titleSize, "Migration assessment: ")}

		for i, a := range assessments {
			if i > 0 {
				title = append(title, words(helveticaBold, titleSize, nameSeparator))
			}

			title = append(title, value(titleSize, a.Target))
		}

		writeTitle(d, title...)
		writeFacts(d, meta, source, assessments, true)

		d.title(1, "Comparison")
		writeComparisonSummary(d, assessments)
		writeComparisonResources(d, assessments)

		noted := new(bool)
		for _, a := range assessments {
			writeGlobalNote(d, a.Verdicts, noted)
		}

		writeSharedBlockers(d, c)
		d.banner()

		for _, a := range assessments {
			d.styledSection(headingStyle{ink: inkColour, goldLeft: true}, 1, value(sectionSize, a.Target))
			writeBody(d, a, 2, noted)
			writeDetails(d, a, 2)
		}

		writeHow(d, slices.ContainsFunc(assessments, hasEstimates))
		writeAbout(d, r.versionLine, meta, source, assessments)
	})

	if _, err := io.WriteString(w, file); err != nil {
		return fmt.Errorf("write pdf comparison report: %w", err)
	}

	return nil
}

// targetColumns returns n column widths sharing what is left of the content
// width after used points, each at most most points.
func targetColumns(n, used, most int) []int {
	width := min(most, (contentWidth-used)/n)

	widths := make([]int, n)
	for i := range widths {
		widths[i] = width
	}

	return widths
}

// writeComparisonSummary writes the counts per fit with one column per target,
// then one estimate row per target with estimates. It ranks no target.
func writeComparisonSummary(d *document, assessments []assessment.Assessment) {
	d.title(2, "Summary")

	t := table{widths: append([]int{90}, targetColumns(len(assessments), 90, 120)...), header: headerRow("Fit")}

	for _, a := range assessments {
		t.header = append(t.header, cell{{value(tableSize, a.Target)}})
	}

	counts := make([][][2]string, len(assessments))
	for i, a := range assessments {
		counts[i] = fitCounts(a.Summary)
	}

	labels := []seg{
		badge(mapping.Direct, ""), badge(mapping.Partial, ""), badge(mapping.None, ""), words(helvetica, tableSize, "total"),
	}

	for row := range counts[0] {
		cells := []cell{{{labels[row]}}}

		for _, c := range counts {
			cells = append(cells, cell{{words(helvetica, tableSize, c[row][1])}})
		}

		t.rows = append(t.rows, cells)
	}

	for i, a := range assessments {
		if !hasEstimates(a) {
			continue
		}

		cells := make([]cell, len(assessments)+1)
		cells[0] = cell{{value(tableSize, a.Target), words(helvetica, tableSize, " estimate")}}
		cells[i+1] = cell{totalSegs(tableSize, *a.Total, estimatesOf(a.Verdicts))}
		t.rows = append(t.rows, cells)
	}

	d.drawTable(t)

	if slices.ContainsFunc(assessments, hasEstimates) {
		d.body(plain("Estimates are list prices on each catalog's retrieval date, " + excludedNote +
			"; each target's own section gives its basis."))
	}
}

// writeComparisonResources writes one row per resource, in the inventory's
// order: the ID and category, then one cell per target that stacks its fit,
// its best offering and, with estimates, its monthly estimate.
func writeComparisonResources(d *document, assessments []assessment.Assessment) {
	d.title(2, "Resources")

	first := assessments[0]
	if len(first.Verdicts) == 0 {
		none(d)

		return
	}

	t := table{widths: append([]int{95, 95}, targetColumns(len(assessments), 190, 305)...), header: headerRow("ID", "Category")}

	for _, a := range assessments {
		t.header = append(t.header, cell{{value(tableSize, a.Target)}})
	}

	for i, v := range first.Verdicts {
		row := []cell{
			{{value(tableSize, v.Resource.ID)}},
			{{value(tableSize, string(v.Resource.Category))}},
		}

		for _, a := range assessments {
			tv := a.Verdicts[i]
			stacked := cell{{badge(tv.Fit, "")}}

			if tv.BestOffering != "" {
				stacked = append(stacked, append([]seg{value(tableSize, tv.BestOffering)},
					globalMark(tableSize, tv.BestOfferingGlobal())...))
			}

			if hasEstimates(a) && tv.Estimate != nil {
				stacked = append(stacked, estimateCellSegs(tableSize, tv.Estimate))
			}

			row = append(row, stacked)
		}

		t.rows = append(t.rows, row)
	}

	d.drawTable(t)
}

// writeSharedBlockers writes one item per resource blocked on every target,
// with one nested item per target, named by its display name, giving its
// reasons, or "None.".
func writeSharedBlockers(d *document, c assessment.Comparison) {
	// Target keys are unique by contract, and Validate has checked that every
	// key a shared blocker names is compared.
	names := make(map[string]string, len(c.Targets))
	for _, t := range c.Targets {
		names[t.Key] = t.Assessment.Target
	}

	var items []func(*document)

	for _, blocker := range c.SharedBlockers {
		items = append(items, func(d *document) { d.item(0, value(bodySize, blocker.ResourceID)) })

		for _, t := range blocker.Targets {
			items = append(items, func(d *document) {
				d.item(bulletIndent, append([]seg{value(bodySize, names[t.Key]), plain(": ")},
					reasonsSegs(bodySize, t.Reasons)...)...)
			})
		}
	}

	d.noticeBox(warnBox, 2, warnInkColour, true, "Blockers on every target", items)
}

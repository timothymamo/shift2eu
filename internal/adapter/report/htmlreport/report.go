package htmlreport

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
)

// dateLayout is how a report shows the catalog's retrieval date: a calendar date.
const dateLayout = "2006-01-02"

// The heading levels of a report: the title, its sections, the sections inside
// them (a comparison's own sections, a target's sections in a comparison, the
// workloads of a single-target report) and the workloads of a target in a
// comparison. The page holds the fragment under its own h1, so the title is an
// h2.
const (
	titleLevel   = 2
	sectionLevel = 3
)

// The separators of the report's own words: between reasons, and between names
// (tied offerings, required changes, relations).
const (
	reasonSeparator = "; "
	nameSeparator   = ", "
)

// Reporter renders an assessment, or a comparison of several targets, as an
// HTML fragment. It holds no state, so one Reporter may render any number of
// reports, concurrently too.
type Reporter struct{}

// New returns an HTML reporter.
func New() *Reporter {
	return &Reporter{}
}

// Report writes the HTML fragment of a and meta to w, laid out as the package
// documentation describes.
//
// The output depends on a and meta only. Report builds the whole fragment
// before writing it to w in one call, so w never receives a half-rendered
// fragment from a failure while rendering; a write error is returned, wrapped.
// When ctx is already done Report writes nothing and returns ctx.Err().
func (*Reporter) Report(ctx context.Context, w io.Writer, a assessment.Assessment, meta analysis.ReportMeta) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var f fragment

	writeHeader(&f, a, meta)
	writeBody(&f, a, sectionLevel)

	return write(w, &f, "html report")
}

// ReportComparison writes the HTML fragment of the comparison c and meta to w,
// laid out as the package documentation describes.
//
// The output depends on c and meta only. ReportComparison builds the whole
// fragment before writing it to w in one call; a write error is returned,
// wrapped. A comparison markdown.Reporter refuses (no target, targets that
// assess different resources, a shared blocker naming a target not compared)
// is refused too, and nothing is written. When ctx is already done
// ReportComparison writes nothing and returns ctx.Err().
func (*Reporter) ReportComparison(ctx context.Context, w io.Writer, c assessment.Comparison, meta analysis.ReportMeta) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(c.Targets) == 0 {
		// The package may not import errors (depguard reporters-stay-offline).
		return fmt.Errorf("render html comparison: the comparison holds no target")
	}

	if err := c.Validate(); err != nil {
		return fmt.Errorf("render html comparison: %w", err)
	}

	var f fragment

	writeComparisonHeader(&f, c.Targets, meta)
	f.part(partComparison, func() {
		f.heading(sectionLevel, text("Comparison"))
		writeComparisonSummary(&f, c.Targets)
		writeComparisonResources(&f, c.Targets)
		writeSharedBlockers(&f, c)
	})

	for _, t := range c.Targets {
		f.part(partTarget, func() {
			f.heading(sectionLevel, code(t.Assessment.Target))
			writeBody(&f, t.Assessment, sectionLevel+1)
		})
	}

	return write(w, &f, "html comparison report")
}

// write renders f and writes it to w in one call. what names the report in an
// error.
func write(w io.Writer, f *fragment, what string) error {
	var b strings.Builder

	if err := f.render(&b); err != nil {
		return fmt.Errorf("render %s: %w", what, err)
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write %s: %w", what, err)
	}

	return nil
}

// writeHeader writes the title and the list that says what was assessed
// against what. The inventory file item is left out when the inventory has no
// file name.
func writeHeader(f *fragment, a assessment.Assessment, meta analysis.ReportMeta) {
	f.heading(titleLevel, text("Migration assessment: ").code(a.Target))

	var items []item

	if meta.InventoryName != "" {
		items = append(items, item{Text: text("Inventory file: ").code(meta.InventoryName)})
	}

	items = append(items,
		item{Text: text("Source: ").code(a.Source)},
		item{Text: catalogLine(code(a.Target), a)},
	)

	if region := renderRegion(a); region != nil {
		items = append(items, item{Text: text("Region: ").then(region)})
	}

	f.list(items)
}

// catalogLine is the header's catalog item for the target named name.
func catalogLine(name rich, a assessment.Assessment) rich {
	return text("Catalog: ").then(name).words(" snapshot from ").code(a.Snapshot.SourceURL).
		words(", retrieved " + a.Snapshot.RetrievedOn.UTC().Format(dateLayout))
}

// writeBody writes the sections of the report of a, each headed at level and
// each in its part section: its summary, resources, required changes and
// blockers, then the workloads when a.ShowWorkloads and the cost estimate
// when a has estimates.
func writeBody(f *fragment, a assessment.Assessment, level int) {
	f.part(partSummary, func() { writeSummary(f, a.Summary, level) })
	f.part(partResources, func() { writeResources(f, a, level) })
	f.part(partChanges, func() { writeRequiredChanges(f, a.Verdicts, level) })
	f.part(partBlockers, func() { writeBlockers(f, a.Blockers, a.Verdicts, level) })

	if a.ShowWorkloads() {
		f.part(partWorkloads, func() { writeWorkloads(f, a, level) })
	}

	if hasEstimates(a) {
		f.part(partCost, func() { writeCostEstimate(f, a, level) })
	}
}

// writeSummary writes the number of resources per fit, and in total.
func writeSummary(f *fragment, s assessment.Summary, level int) {
	f.heading(level, text("Summary"))
	f.table([]rich{text("Fit"), text("Resources")}, []row{
		{Class: classRowDirect, Cells: cells(text(string(mapping.Direct)), text(strconv.Itoa(s.Direct)))},
		{Class: classRowPartial, Cells: cells(text(string(mapping.Partial)), text(strconv.Itoa(s.Partial)))},
		{Class: classRowNone, Cells: cells(text(string(mapping.None)), text(strconv.Itoa(s.None)))},
		{Class: classRowTotal, Cells: cells(text("total"), text(strconv.Itoa(s.Total)))},
	})
}

// fitCell is the Fit cell of a verdict whose fit is fit: the fit's word, with
// the badge class of that fit, chosen from the mapping's closed set of fits.
// A fit the report does not know gets the badge class alone, never a class
// made from its text.
func fitCell(fit mapping.Fit) cell {
	c := classFit

	switch fit {
	case mapping.Direct:
		c = classFitDirect
	case mapping.Partial:
		c = classFitPartial
	case mapping.None:
		c = classFitNone
	default:
	}

	return cell{Text: text(string(fit)), Class: c}
}

// writeResources writes one table row per verdict of a, in its order, with
// the columns of the Markdown report: Plan and Monthly estimate only when a
// has estimates, Relations only when some resource has a relation.
func writeResources(f *fragment, a assessment.Assessment, level int) {
	estimates := hasEstimates(a)
	relations := hasRelations(a.Verdicts)

	header := []rich{text("ID"), text("Raw type"), text("Category"), text("Region"), text("Fit"), text("Best offering")}
	if estimates {
		header = append(header, text("Plan"), text("Monthly estimate"))
	}

	if relations {
		header = append(header, text("Relations"))
	}

	header = append(header, text("Reasons"))

	rows := make([]row, 0, len(a.Verdicts))

	for _, v := range a.Verdicts {
		r := cells(code(v.Resource.ID), code(v.Resource.RawType), code(string(v.Resource.Category)),
			code(v.Resource.Region))
		r = append(r, fitCell(v.Fit))
		r = append(r, cells(renderBestOffering(v))...)

		if estimates {
			r = append(r, cells(renderPlan(v.Estimate), renderEstimate(v.Estimate))...)
		}

		if relations {
			r = append(r, cells(renderRelations(v))...)
		}

		r = append(r, cells(renderReasons(v.Reasons))...)
		rows = append(rows, row{Cells: r})
	}

	f.heading(level, text("Resources"))
	f.table(header, rows)
}

// writeRequiredChanges writes one list item per partial verdict that has
// required changes, in the order given: the capabilities the best offering
// lacks, and that offering. It writes "None." when there is no such verdict.
func writeRequiredChanges(f *fragment, verdicts []assessment.Verdict, level int) {
	f.heading(level, text("Required changes"))

	var items []item

	for _, v := range verdicts {
		listed := v.ChangesToList()
		if len(listed) == 0 {
			continue
		}

		changes := make([]rich, 0, len(listed))
		for _, c := range listed {
			changes = append(changes, code(string(c)))
		}

		items = append(items, item{Text: code(v.Resource.ID).words(": ").join(changes, nameSeparator).
			words(" (not offered by ").code(v.BestOffering).words(")")})
	}

	if len(items) == 0 {
		f.paragraph(text("None."))

		return
	}

	f.list(items)
}

// writeBlockers writes one list item per blocker, in the order given, then one
// item per "affected by" note of the verdicts (assessment.Verdict.AffectedBy),
// in verdict order, or "None." when there is no blocker.
func writeBlockers(f *fragment, blockers []assessment.Blocker, verdicts []assessment.Verdict, level int) {
	f.heading(level, text("Blockers"))

	if len(blockers) == 0 {
		f.paragraph(text("None."))

		return
	}

	items := blockerItems(blockers)

	for _, v := range verdicts {
		for _, id := range v.AffectedBy {
			items = append(items, item{Text: code(v.Resource.ID).words(": affected by a blocker on ").code(id)})
		}
	}

	f.list(items)
}

// blockerItems returns one list item per blocker, "ID: reason".
func blockerItems(blockers []assessment.Blocker) []item {
	items := make([]item, 0, len(blockers))

	for _, blocker := range blockers {
		items = append(items, item{Text: code(blocker.ResourceID).words(": ").then(renderReason(blocker.Reason))})
	}

	return items
}

// renderBestOffering renders the best-offering cell of v: the best offering's
// name, followed by "(tie: ...)" with the other candidates a reader could take
// for it, in candidate order.
func renderBestOffering(v assessment.Verdict) rich {
	best := code(v.BestOffering)

	names := v.Ties()
	if len(names) == 0 {
		return best
	}

	ties := make([]rich, 0, len(names))
	for _, name := range names {
		ties = append(ties, code(name))
	}

	return best.words(" (tie: ").join(ties, nameSeparator).words(")")
}

// renderReasons renders reasons one after the other, separated by "; ".
func renderReasons(reasons []mapping.Reason) rich {
	rendered := make([]rich, 0, len(reasons))
	for _, r := range reasons {
		rendered = append(rendered, renderReason(r))
	}

	return rich{}.join(rendered, reasonSeparator)
}

// renderReason renders a reason from its parts: literal sentence text as the
// report's words, every other part, a name, as code. It never uses the
// reason's String method.
func renderReason(r mapping.Reason) rich {
	var out rich

	for _, p := range r.Parts() {
		if p.Kind == mapping.PartLiteral {
			out = out.words(p.Text)

			continue
		}

		// PartTarget, PartOffering, PartCategory, PartCapability, and any kind
		// added later: a value is shown literally whatever it is.
		out = out.code(p.Text)
	}

	return out
}

// writeComparisonHeader writes the title naming every target, the inventory
// file item (left out when the inventory has no file name), the source, taken
// from the first target, one catalog item per target and one region item per
// target that has a region.
func writeComparisonHeader(f *fragment, targets []assessment.TargetAssessment, meta analysis.ReportMeta) {
	names := make([]rich, 0, len(targets))
	for _, t := range targets {
		names = append(names, code(t.Assessment.Target))
	}

	f.heading(titleLevel, text("Migration assessment: ").join(names, nameSeparator))

	var items []item

	if meta.InventoryName != "" {
		items = append(items, item{Text: text("Inventory file: ").code(meta.InventoryName)})
	}

	items = append(items, item{Text: text("Source: ").code(targets[0].Assessment.Source)})

	for i, t := range targets {
		items = append(items, item{Text: catalogLine(names[i], t.Assessment)})
	}

	for i, t := range targets {
		if region := renderRegion(t.Assessment); region != nil {
			items = append(items, item{Text: text("Region: ").then(names[i]).words(": ").then(region)})
		}
	}

	f.list(items)
}

// writeComparisonSummary writes one row per fit, and a total, with one column
// per target holding that target's count, then one estimate row per target
// with estimates, holding that target's total in its own column, and the basis
// the totals are given on. It ranks no target.
func writeComparisonSummary(f *fragment, targets []assessment.TargetAssessment) {
	header := []rich{text("Fit")}
	direct := row{Class: classRowDirect, Cells: cells(text(string(mapping.Direct)))}
	partial := row{Class: classRowPartial, Cells: cells(text(string(mapping.Partial)))}
	none := row{Class: classRowNone, Cells: cells(text(string(mapping.None)))}
	total := row{Class: classRowTotal, Cells: cells(text("total"))}

	for _, t := range targets {
		s := t.Assessment.Summary
		header = append(header, code(t.Assessment.Target))
		direct.Cells = append(direct.Cells, cells(text(strconv.Itoa(s.Direct)))...)
		partial.Cells = append(partial.Cells, cells(text(strconv.Itoa(s.Partial)))...)
		none.Cells = append(none.Cells, cells(text(strconv.Itoa(s.None)))...)
		total.Cells = append(total.Cells, cells(text(strconv.Itoa(s.Total)))...)
	}

	rows := []row{direct, partial, none, total}
	estimated := false

	for i, t := range targets {
		if !hasEstimates(t.Assessment) {
			continue
		}

		estimated = true
		r := make([]cell, len(targets)+1)
		r[0] = cell{Text: code(t.Assessment.Target).words(" estimate")}
		r[i+1] = cell{Text: renderTotal(*t.Assessment.Total, estimatesOf(t.Assessment.Verdicts))}
		rows = append(rows, row{Cells: r})
	}

	f.heading(sectionLevel+1, text("Summary"))
	f.table(header, rows)

	if estimated {
		f.paragraph(text("Estimates are list prices on each catalog's retrieval date, " + excludedNote +
			"; each target's own section gives its basis."))
	}
}

// writeComparisonResources writes one row per resource, in the first target's
// verdict order: the ID, the category, and each target's fit and best
// offering, without the tie note, and its monthly estimate when that target
// has estimates.
func writeComparisonResources(f *fragment, targets []assessment.TargetAssessment) {
	header := []rich{text("ID"), text("Category"), text("Region")}

	for _, t := range targets {
		name := code(t.Assessment.Target)
		header = append(header, name.words(" fit"), name.words(" best offering"))

		if hasEstimates(t.Assessment) {
			header = append(header, name.words(" monthly estimate"))
		}
	}

	rows := make([]row, 0, len(targets[0].Assessment.Verdicts))

	for i, first := range targets[0].Assessment.Verdicts {
		r := cells(code(first.Resource.ID), code(string(first.Resource.Category)), code(first.Resource.Region))

		for _, t := range targets {
			v := t.Assessment.Verdicts[i]
			r = append(r, fitCell(v.Fit))
			r = append(r, cells(code(v.BestOffering))...)

			if hasEstimates(t.Assessment) {
				r = append(r, cells(renderEstimate(v.Estimate))...)
			}
		}

		rows = append(rows, row{Cells: r})
	}

	f.heading(sectionLevel+1, text("Resources"))
	f.table(header, rows)
}

// writeSharedBlockers writes one list item per resource blocked on every
// target, with a nested item per target giving that target's reasons, or
// "None." when there is none. A target is named by its display name, never
// its key.
func writeSharedBlockers(f *fragment, c assessment.Comparison) {
	f.heading(sectionLevel+1, text("Blockers on every target"))

	if len(c.SharedBlockers) == 0 {
		f.paragraph(text("None."))

		return
	}

	// assessment.Comparison.Validate has checked that every key a shared
	// blocker names is here.
	names := make(map[string]string, len(c.Targets))
	for _, t := range c.Targets {
		names[t.Key] = t.Assessment.Target
	}

	items := make([]item, 0, len(c.SharedBlockers))

	for _, blocker := range c.SharedBlockers {
		children := make([]rich, 0, len(blocker.Targets))
		for _, t := range blocker.Targets {
			children = append(children, code(names[t.Key]).words(": ").then(renderReasons(t.Reasons)))
		}

		items = append(items, item{Text: code(blocker.ResourceID), Children: children})
	}

	f.list(items)
}

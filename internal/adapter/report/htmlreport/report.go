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
// them (a target's sections in a comparison, the workloads of a single-target
// report) and the workloads of a target in a comparison. The page holds the fragment under its own h1, so the title is an
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
// laid out as the package documentation describes: the header naming every
// target, then each target's own sections, and no combined block (issue
// #373, item 05).
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

// headerSeparator is the dot between the header line's items, which a reader
// sees and a screen reader, reading the items of a list, does not need.
const headerSeparator = "·"

// writeHeader writes the title and the one line that says what was assessed
// against what (issue #373, item 10-1): the source, the inventory file (left
// out when the inventory has no file name), the catalog's retrieval date as a
// link to the catalog's own page, and the region, when there is one, as a
// list whose items the stylesheet sets on one line.
func writeHeader(f *fragment, a assessment.Assessment, meta analysis.ReportMeta) {
	f.heading(titleLevel, title().code(a.Target))

	items := headerStart(a.Source, meta)
	items = append(items, headerItem(catalogLink(a)))

	if region := renderRegion(a); region != nil {
		items = append(items, headerItem(text("region ").then(region)))
	}

	f.markerlessList(items)
}

// title is the start of the title, the words the stylesheet sets above the
// target's name.
func title() rich {
	return rich{}.span("Migration assessment").words(" ")
}

// headerStart returns the header line's first items: the source, and the
// inventory file when it has a name.
func headerStart(source string, meta analysis.ReportMeta) []item {
	items := []item{{Text: text("source ").code(source)}}

	if meta.InventoryName != "" {
		items = append(items, headerItem(code(meta.InventoryName)))
	}

	return items
}

// headerItem is an item of the header line after its first: r, after the
// separator.
func headerItem(r rich) item {
	return item{Text: rich{}.separator(headerSeparator).words(" ").then(r)}
}

// catalogLink is "catalog retrieved <date>", linking the catalog's own page.
func catalogLink(a assessment.Assessment) rich {
	return rich{}.link("catalog retrieved "+a.Snapshot.RetrievedOn.UTC().Format(dateLayout), a.Snapshot.SourceURL)
}

// writeBody writes the sections of the report of a, each headed at level and
// each in its part section: its summary, resources, required changes and
// blockers, the last two in one box so the stylesheet sets them side by side
// (issue #373, item 12), then the workloads when a.ShowWorkloads and the cost
// estimate when a has estimates.
func writeBody(f *fragment, a assessment.Assessment, level int) {
	f.part(partSummary, func() { writeSummary(f, a, level) })
	f.part(partResources, func() { writeResources(f, a, level) })
	f.box(func() {
		f.part(partChanges, func() { writeRequiredChanges(f, a.Verdicts, level) })
		f.part(partBlockers, func() { writeBlockers(f, a.Blockers, a.Verdicts, level) })
	})

	if a.ShowWorkloads() {
		f.part(partWorkloads, func() { writeWorkloads(f, a, level) })
	}

	if hasEstimates(a) {
		f.part(partCost, func() { writeCostEstimate(f, a, level) })
	}
}

// writeSummary writes the summary as boxes (issue #373, item 10-2): one per
// fit, its badge, the number of resources of that fit and its caption, then,
// when a has estimates, one with the monthly estimate. The direct box's
// caption is the total its count is of; the partial and none boxes say what
// that fit means for the move (item 03-2).
func writeSummary(f *fragment, a assessment.Assessment, level int) {
	s := a.Summary

	f.heading(level, text("Summary"))

	for _, fit := range []struct {
		fit     mapping.Fit
		count   int
		caption string
	}{
		{fit: mapping.Direct, count: s.Direct, caption: "of " + strconv.Itoa(s.Total) + resourcesNoun(s.Total)},
		{fit: mapping.Partial, count: s.Partial, caption: "needs changes"},
		{fit: mapping.None, count: s.None, caption: "blocked"},
	} {
		f.paragraph(rich{}.badge(fitClass(fit.fit), string(fit.fit)).words(" ").
			figure(classNone, strconv.Itoa(fit.count)).words(" ").span(fit.caption))
	}

	if hasEstimates(a) {
		f.paragraph(summaryEstimate(*a.Total))
	}
}

// fitClass is the badge class of fit, chosen from the mapping's closed set of
// fits by an exhaustive switch. A fit the report does not know gets the badge
// class alone, never a class made from its text.
func fitClass(fit mapping.Fit) class {
	switch fit {
	case mapping.Direct:
		return classFitDirect
	case mapping.Partial:
		return classFitPartial
	case mapping.None:
		return classFitNone
	default:
		return classFit
	}
}

// fitCell is the Fit cell of a verdict whose fit is fit: the fit's word in a
// span the stylesheet draws as a badge, with the badge class of that fit.
func fitCell(fit mapping.Fit) cell {
	return cell{Text: rich{}.span(string(fit)), Class: fitClass(fit)}
}

// writeResources writes one table row per verdict of a, in its order, with
// the columns of the Markdown report but the region (issue #373, item 04):
// Plan and Monthly estimate only when a has estimates, Relations only when
// some resource has a relation.
func writeResources(f *fragment, a assessment.Assessment, level int) {
	estimates := hasEstimates(a)
	relations := hasRelations(a.Verdicts)

	header := []rich{text("ID"), text("Raw type"), text("Category"), text("Fit"), text("Best offering")}
	if estimates {
		header = append(header, text("Plan"), text("Monthly estimate"))
	}

	if relations {
		header = append(header, text("Relations"))
	}

	header = append(header, text("Reasons"))

	rows := make([]row, 0, len(a.Verdicts))

	for _, v := range a.Verdicts {
		r := cells(code(v.Resource.ID), code(v.Resource.RawType), code(string(v.Resource.Category)))
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
	f.table(tableResources, header, rows)
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

	f.markerlessList(items)
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

// writeComparisonHeader writes the title naming every target and the header
// line: the source, taken from the first target, the inventory file (left out
// when the inventory has no file name), then one item per target naming it,
// its catalog's retrieval date as a link to the catalog's own page, and its
// region when it has one.
func writeComparisonHeader(f *fragment, targets []assessment.TargetAssessment, meta analysis.ReportMeta) {
	names := make([]rich, 0, len(targets))
	for _, t := range targets {
		names = append(names, code(t.Assessment.Target))
	}

	f.heading(titleLevel, title().join(names, nameSeparator))

	items := headerStart(targets[0].Assessment.Source, meta)

	for i, t := range targets {
		line := names[i].words(" ").then(catalogLink(t.Assessment))
		if region := renderRegion(t.Assessment); region != nil {
			line = line.words(", region ").then(region)
		}

		items = append(items, headerItem(line))
	}

	f.markerlessList(items)
}

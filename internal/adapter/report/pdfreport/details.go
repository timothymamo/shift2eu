package pdfreport

import (
	"strconv"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
)

// writeDetails writes Resource details: one block per verdict, in the
// assessment's order, headed by the resource's ID.
func writeDetails(d *document, a assessment.Assessment, level int) {
	d.title(level, "Resource details")

	if len(a.Verdicts) == 0 {
		none(d)

		return
	}

	blockers := make(map[string][]mapping.Reason, len(a.Blockers))
	for _, b := range a.Blockers {
		blockers[b.ResourceID] = append(blockers[b.ResourceID], b.Reason)
	}

	for _, v := range a.Verdicts {
		writeVerdictDetails(d, a, v, blockers[v.Resource.ID], level+1)
	}
}

// label returns a line's leading words, "Label: ".
func label(text string) seg { return words(helvetica, bodySize, text+": ") }

// plain returns the report's own words in the body size.
func plain(text string) seg { return words(helvetica, bodySize, text) }

// writeVerdictDetails writes one resource's block: what the inventory gives,
// the fit and best offering, relations, every candidate with its fit and
// missing capabilities, the reasons, the required changes, the blockers and,
// with estimates on, the estimate's breakdown.
func writeVerdictDetails(d *document, a assessment.Assessment, v assessment.Verdict, blockers []mapping.Reason, level int) {
	r := v.Resource

	d.section(level, value(headingSize(level), r.ID))

	d.body(label("Raw type"), value(bodySize, r.RawType))
	d.body(label("Category"), value(bodySize, string(r.Category)))
	if r.Region != "" {
		d.body(label("Region"), value(bodySize, r.Region))
	} else {
		d.body(label("Region"), plain("none"))
	}

	if r.Workload != "" {
		d.body(label("Workload"), value(bodySize, r.Workload))
	}

	d.body(label("Fit"), plain(string(v.Fit)))

	if v.BestOffering != "" {
		d.body(append([]seg{label("Best offering")}, bestOfferingSegs(bodySize, v)...)...)
	} else {
		d.body(label("Best offering"), plain("none"))
	}

	if len(r.Relations) > 0 {
		segs := []seg{label("Relations")}

		for i, rel := range r.Relations {
			if i > 0 {
				segs = append(segs, plain(nameSeparator))
			}

			segs = append(segs, plain(string(rel.Type)+" "), value(bodySize, rel.Target))
		}

		d.body(segs...)
	}

	for _, id := range v.AffectedBy {
		d.body(plain("Affected by a blocker on "), value(bodySize, id))
	}

	writeCandidates(d, v.Candidates)

	if len(v.Reasons) > 0 {
		d.body(append([]seg{label("Reasons")}, reasonsSegs(bodySize, v.Reasons)...)...)
	}

	if listed := v.ChangesToList(); len(listed) > 0 {
		d.body(append([]seg{label("Required changes")}, valueList(bodySize, capabilityNames(listed))...)...)
	} else {
		d.body(label("Required changes"), plain("none"))
	}

	if len(blockers) > 0 {
		d.body(append([]seg{label("Blockers")}, reasonsSegs(bodySize, blockers)...)...)
	} else {
		d.body(label("Blockers"), plain("none"))
	}

	if hasEstimates(a) && v.Estimate != nil {
		var count *int
		if r.Size != nil {
			count = r.Size.Count
		}

		writeBreakdown(d, count, *v.Estimate)
	}
}

// writeCandidates writes the table of every candidate offering, best first as
// the mapping orders them, or the sentence that no offering serves the
// category.
func writeCandidates(d *document, candidates []mapping.Candidate) {
	if len(candidates) == 0 {
		d.body(plain("No offering in the catalog serves this category."))

		return
	}

	t := table{widths: []int{200, 60, 235}, header: headerRow("Offering", "Fit", "Missing capabilities")}

	for _, c := range candidates {
		missing := []seg{words(helvetica, tableSize, "none")}
		if len(c.Missing) > 0 {
			missing = valueList(tableSize, capabilityNames(c.Missing))
		}

		t.rows = append(t.rows, []cell{
			{append([]seg{value(tableSize, c.Offering.Name)}, globalMark(tableSize, c.Offering.Global)...)},
			{{words(helvetica, tableSize, string(c.Fit))}},
			{missing},
		})
	}

	d.drawTable(t)
}

// writeBreakdown writes an estimate's breakdown: the plan, the parts and the
// monthly total, the count and hours with what was assumed, the billing cap
// and the storage not included, or "not estimated" and why.
func writeBreakdown(d *document, count *int, e estimate.Estimate) {
	if !e.Estimated {
		d.body(append([]seg{label("Estimate"), plain("not estimated: ")}, notEstimatedSegs(bodySize, e)...)...)

		if e.Reason == estimate.NoPlanLargeEnough && e.Largest != nil {
			d.body(label("Largest plan"), value(bodySize, e.Largest.Name), plain(" ("+planSize(*e.Largest)+")"))
		}

		return
	}

	d.body(label("Estimate"), plain(euros(e.Monthly)+" a month"))

	if e.Plan != nil {
		text := " (" + planSize(*e.Plan)
		if e.Plan.IncludedStorageGiB > 0 {
			text += ", " + strconv.FormatInt(e.Plan.IncludedStorageGiB, 10) + " GiB storage included"
		}

		text += ")"

		if e.Plan.Priced {
			text += ", plan price " + euros(e.Plan.Price) + " a month"
		}

		d.item(0, label("Plan"), value(bodySize, e.Plan.Name), plain(text))
	}

	d.item(0, plain("Plan part: "+euros(e.Breakdown.Plan)))
	d.item(0, plain("Storage part: "+euros(e.Breakdown.Storage)))
	d.item(0, plain("Fixed fee: "+euros(e.Breakdown.FixedFee)))

	switch {
	case e.CountAssumed:
		d.item(0, plain("Count: 1, assumed: the resource gives none"))
	case count != nil:
		d.item(0, plain("Count: "+strconv.Itoa(*count)))
	}

	if e.HoursAssumed {
		d.item(0, plain("Hours: "+hoursPerMonth+" a month, assumed: the resource gives none"))
	} else {
		d.item(0, plain("Hours: as the inventory gives them"))
	}

	if e.BillingCapHours > 0 {
		d.item(0, plain("Billing cap: the plan is billed for at most "+strconv.FormatInt(e.BillingCapHours, 10)+
			" hours a month"))
	}

	if e.StorageNotIncluded != "" {
		d.item(0, plain(storageNotIncluded(e.StorageNotIncluded)+" not included"))
	}
}

// writeHow writes How this report is worked out: fixed text explaining fits,
// the best offering and ties, required changes and blockers, workloads,
// estimates (or that they were off) and how values are shown.
func writeHow(d *document, estimates bool) {
	d.title(1, "How this report is worked out")

	paragraph := func(lead, text string) {
		d.space(3)
		d.body(words(helveticaBold, bodySize, lead+" "), plain(text))
	}

	paragraph("Fits.", "A resource's fit is direct when an offering of the target serves its taxonomy category "+
		"and offers every capability the resource uses, partial when the offering serving the category lacks "+
		"some of them, and none when no offering serves the category or the resource is not classified.")
	paragraph("Best offering.", "Every offering that serves the category is a candidate. Candidates are "+
		"ordered direct before partial, then by how few capabilities they lack, then by name; the first is "+
		"the best offering. A tie names the other candidates with the same fit and as many missing "+
		"capabilities, which would serve as well.")
	paragraph("Required changes and blockers.", "A required change is a capability the resource uses that its "+
		"best offering does not offer: the resource moves once it no longer depends on it. A blocker is a "+
		"resource with fit none: nothing in the catalog serves it. A resource that depends on or is a replica "+
		"of a blocked resource is affected by a blocker on it; the note is not transitive.")
	paragraph("Workloads.", "Resources are grouped by the workload the inventory names, and those that name "+
		"none form the No workload group. A workload's counts and blockers are those of its resources.")

	if estimates {
		paragraph("Estimates.", "A resource is matched to the cheapest plan, among its best offering and the "+
			"offerings tied with it, that covers its vCPUs and memory; a Redis cache is matched on memory "+
			"alone, and block, object and file storage have no plan: they cost the stored GiB × the price "+
			"per GiB. A monthly estimate is plan price × count × hours / 730, with 730 "+
			"hours for a resource that gives none and a count of 1 when it gives no count; a plan billed for at "+
			"most a number of hours a month costs plan price × count × min(hours, cap) / cap. Storage "+
			"above what the plan includes is priced per GiB, per node × count except for a managed database, "+
			"whose storage is counted once. A fixed fee is the offering's fee once per resource × hours / 730, "+
			"never capped. "+
			"Amounts are rounded to the cent, half away from zero. Prices are list prices on the catalog's "+
			"retrieval date, excluding VAT and discounts: an estimate is not a quote.")
	} else {
		paragraph("Estimates.", "Estimates were off for this report (--no-estimate): it shows no plan and "+
			"no amount.")
	}

	paragraph("Values.", "Every value from the inventory or a catalog is shown in Courier, as it is stored. "+
		"A character the PDF's core fonts cannot show is written as a visible escape in Go's style: \\u and "+
		"four hex digits up to U+FFFF (\\u0416 is the Cyrillic letter Zhe), \\U and eight hex digits above it, "+
		"\\x and two hex digits for a byte that is not valid UTF-8, and \\\\ for a backslash, so an escape "+
		"never looks like a value's own text. A no-break space and a soft hyphen are escaped too, so they "+
		"never pass for a space or for nothing.")
	paragraph("Readiness score.", "shift2eu gives no readiness score: the fits, blockers, required changes "+
		"and estimates above are the assessment.")
}

// writeAbout writes About this report: the version line, every catalog
// snapshot used, the options and the inventory.
func writeAbout(d *document, versionLine string, meta analysis.ReportMeta, source string, targets []assessment.Assessment) {
	d.title(1, "About this report")

	d.body(label("Version"), value(bodySize, versionLine))

	estimates := false

	for _, a := range targets {
		estimates = estimates || hasEstimates(a)

		segs := catalogLine(bodySize, "Catalog: ", a)
		if a.Snapshot.DefaultRegion != "" {
			segs = append(segs, plain(", default region for estimates "), value(bodySize, a.Snapshot.DefaultRegion))
		}

		d.item(0, segs...)
	}

	for _, a := range targets {
		if line := regionLine(bodySize, a); line != nil {
			d.item(0, append([]seg{label("Region"), value(bodySize, a.Target), plain(": ")}, line...)...)
		}
	}

	if estimates {
		d.item(0, plain("Estimates: on"))
	} else {
		d.item(0, plain("Estimates: off"))
	}

	if meta.InventoryName != "" {
		d.item(0, label("Inventory file"), value(bodySize, meta.InventoryName))
	}

	d.item(0, label("Source"), value(bodySize, source))
}

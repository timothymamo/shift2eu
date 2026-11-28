package pdfreport

import (
	"slices"
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
)

// dateLayout is how the PDF shows a catalog's retrieval date: a UTC calendar
// date.
const dateLayout = "2006-01-02"

// The report's own words about an estimate, as the Markdown report words them
// (ADR 0024, ADR 0027).
const (
	excludedNote  = "excluding VAT and discounts"
	fixedFeeNote  = "a fixed fee is the offering's listed fee"
	hoursPerMonth = "730"
	hoursNote     = "plan price × count × hours / " + hoursPerMonth + ", " + hoursPerMonth +
		" hours for a resource that gives none"
)

// nameSeparator joins names in one line or cell.
const nameSeparator = ", "

// globalNoteText says once per report what the global mark means (ADR 0048).
const globalNoteText = "(global) means not bound to a region you choose, including the one --region " +
	"names: the provider serves the offering from its own locations, which may be outside the EU and " +
	"which the target's SOURCES.md names."

// currencySigns are the signs put before an amount in a currency the report
// knows; an amount in any other currency is shown after its code alone.
var currencySigns = map[string]string{
	"EUR": "€",
	"USD": "$",
	"GBP": "£",
}

// hasEstimates reports whether a was assessed with estimates on: its Total is
// set exactly then (assessment.AssessWith).
func hasEstimates(a assessment.Assessment) bool { return a.Total != nil }

// euros renders an amount in euros as the Markdown report does: "€2,535.00".
func euros(m estimate.Money) string { return currencySigns["EUR"] + thousands(m.String()) }

// thousands puts a comma between each group of three digits of the whole part
// of a plain decimal, as estimate.Money.String writes it.
func thousands(decimal string) string {
	whole, fraction, found := strings.Cut(decimal, ".")

	var b strings.Builder

	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}

		b.WriteRune(digit)
	}

	if found {
		b.WriteString("." + fraction)
	}

	return b.String()
}

// milliGiB renders an amount in thousandths of a GiB as an exact decimal
// without trailing zeros: 2000 is "2", 1500 is "1.5".
func milliGiB(m int64) string {
	whole := strconv.FormatInt(m/1000, 10)

	fraction := m % 1000
	if fraction == 0 {
		return whole
	}

	digits := strconv.FormatInt(fraction+1000, 10)[1:]

	return whole + "." + strings.TrimRight(digits, "0")
}

// planSize renders a plan's vCPUs and memory: "1 vCPU, 2 GiB", or its memory
// alone for a plan whose target gives no vCPU count.
func planSize(p estimate.Plan) string {
	if p.VCPUs == 0 {
		return milliGiB(p.MemoryMilliGiB) + " GiB"
	}

	return strconv.FormatInt(p.VCPUs, 10) + " vCPU, " + milliGiB(p.MemoryMilliGiB) + " GiB"
}

// regionSegs returns prefix in Helvetica followed by the region as a value:
// escaped, in Courier.
func regionSegs(size int, prefix, region string) []seg {
	return []seg{words(helvetica, size, prefix), value(size, region)}
}

// notEstimatedSegs says in plain words why e is not estimated, as the Markdown
// report does, the region named as a value.
func notEstimatedSegs(size int, e estimate.Estimate) []seg {
	switch e.Reason {
	case estimate.NoFit:
		return []seg{words(helvetica, size, "no fit")}
	case estimate.UsagePriced:
		return []seg{words(helvetica, size, "usage-priced")}
	case estimate.NeverPriced:
		return []seg{words(helvetica, size, "not priced")}
	case estimate.NoSize:
		return []seg{words(helvetica, size, "size unknown")}
	case estimate.NoPlan:
		return []seg{words(helvetica, size, "no plan")}
	case estimate.NoPlanLargeEnough:
		return []seg{words(helvetica, size, "no plan large enough")}
	case estimate.NotOffered:
		return regionSegs(size, "not offered in ", e.Region)
	case estimate.NoPrice:
		return regionSegs(size, "no price in ", e.Region)
	case estimate.NoStoragePrice:
		return regionSegs(size, "no storage price in ", e.Region)
	default:
		return []seg{value(size, string(e.Reason))}
	}
}

// reasonOrder is the order a total names the reasons its resources are not
// estimated in: the order estimate.For checks them.
var reasonOrder = []estimate.Reason{
	estimate.NoFit,
	estimate.UsagePriced,
	estimate.NeverPriced,
	estimate.NoSize,
	estimate.NoPlan,
	estimate.NoPlanLargeEnough,
	estimate.NotOffered,
	estimate.NoPrice,
	estimate.NoStoragePrice,
}

// notEstimatedReasons returns the phrases of the estimates that are not
// estimated, each once, in reasonOrder, then any reason the report does not
// know in the estimates' order, joined with ", ".
func notEstimatedReasons(size int, estimates []estimate.Estimate) []seg {
	var (
		keys    []string
		phrases [][]seg
	)

	add := func(e estimate.Estimate) {
		phrase := notEstimatedSegs(size, e)

		key := ""
		for _, s := range phrase {
			key += s.text + "\x00"
		}

		if !slices.Contains(keys, key) {
			keys = append(keys, key)
			phrases = append(phrases, phrase)
		}
	}

	for _, r := range reasonOrder {
		for _, e := range estimates {
			if !e.Estimated && e.Reason == r {
				add(e)
			}
		}
	}

	for _, e := range estimates {
		if !e.Estimated && !slices.Contains(reasonOrder, e.Reason) {
			add(e)
		}
	}

	var out []seg

	for i, phrase := range phrases {
		if i > 0 {
			out = append(out, words(helvetica, size, nameSeparator))
		}

		out = append(out, phrase...)
	}

	return out
}

// totalSegs renders a total as the Markdown report does: "€2,535.00 per month
// for 4 of 8 resources", the storage note, or, when none of its resources could
// be estimated, "none of 8 resources could be estimated" and the reasons.
func totalSegs(size int, t estimate.Total, estimates []estimate.Estimate) []seg {
	noun := " resources"
	if t.Resources == 1 {
		noun = " resource"
	}

	if t.Estimated == 0 && t.Resources > 0 {
		out := []seg{words(helvetica, size, "none of "+strconv.Itoa(t.Resources)+noun+" could be estimated")}

		if reasons := notEstimatedReasons(size, estimates); len(reasons) > 0 {
			out = append(out, words(helvetica, size, " ("))
			out = append(out, reasons...)
			out = append(out, words(helvetica, size, ")"))
		}

		return out
	}

	line := euros(t.Monthly) + " per month for " + strconv.Itoa(t.Estimated) + " of " +
		strconv.Itoa(t.Resources) + noun

	if t.StorageNotIncluded > 0 {
		noun := " estimates"
		if t.StorageNotIncluded == 1 {
			noun = " estimate"
		}

		line += " (storage not included in " + strconv.Itoa(t.StorageNotIncluded) + noun + ")"
	}

	return []seg{words(helvetica, size, line)}
}

// storageNotIncluded names in plain words the storage an estimate leaves out
// (ADR 0032).
func storageNotIncluded(s estimate.StorageNotIncluded) string {
	switch s {
	case estimate.BootVolume:
		return "boot volume"
	case estimate.DataStorage:
		return "data storage"
	default:
		return "storage"
	}
}

// estimateCellSegs renders the Monthly estimate cell of an estimate as the
// Markdown report words it.
func estimateCellSegs(size int, e *estimate.Estimate) []seg {
	if e == nil {
		return nil
	}

	if !e.Estimated {
		return append([]seg{words(helvetica, size, "not estimated: ")}, notEstimatedSegs(size, *e)...)
	}

	text := euros(e.Monthly)
	if !e.Breakdown.FixedFee.IsZero() {
		text += " (includes a fixed fee of " + euros(e.Breakdown.FixedFee) + ")"
	}

	if e.CountAssumed {
		text += " (count assumed 1)"
	}

	if e.HoursAssumed {
		text += " (" + hoursPerMonth + " hours assumed)"
	}

	if e.StorageNotIncluded != "" {
		text += " (" + storageNotIncluded(e.StorageNotIncluded) + " not included)"
	}

	return []seg{words(helvetica, size, text)}
}

// planCellSegs renders the Plan cell of an estimate: the plan's name and size,
// or "no plan large enough" with the largest plan's.
func planCellSegs(size int, e *estimate.Estimate) []seg {
	switch {
	case e == nil:
		return nil
	case e.Plan != nil:
		return []seg{value(size, e.Plan.Name), words(helvetica, size, " ("+planSize(*e.Plan)+")")}
	case e.Reason == estimate.NoPlanLargeEnough && e.Largest != nil:
		return []seg{
			words(helvetica, size, "no plan large enough (largest: "),
			value(size, e.Largest.Name),
			words(helvetica, size, ", "+planSize(*e.Largest)+")"),
		}
	default:
		return nil
	}
}

// reasonSegs renders a reason from its parts: literal text as the report's own
// words, every other part, a name, as a value.
func reasonSegs(size int, r mapping.Reason) []seg {
	parts := r.Parts()
	out := make([]seg, 0, len(parts))

	for _, p := range parts {
		if p.Kind == mapping.PartLiteral {
			out = append(out, words(helvetica, size, p.Text))

			continue
		}

		out = append(out, value(size, p.Text))
	}

	return out
}

// reasonsSegs renders reasons one after the other, separated by "; ".
func reasonsSegs(size int, reasons []mapping.Reason) []seg {
	var out []seg

	for i, r := range reasons {
		if i > 0 {
			out = append(out, words(helvetica, size, "; "))
		}

		out = append(out, reasonSegs(size, r)...)
	}

	return out
}

// globalMark returns the mark after the name of a global offering.
func globalMark(size int, global bool) []seg {
	if !global {
		return nil
	}

	return []seg{words(helvetica, size, " (global)")}
}

// bestOfferingSegs renders a verdict's best offering, marked when global, and
// "(tie: ...)" with the other candidates a reader could take for it.
func bestOfferingSegs(size int, v assessment.Verdict) []seg {
	out := append([]seg{value(size, v.BestOffering)}, globalMark(size, v.BestOfferingGlobal())...)

	ties := v.Ties()
	if len(ties) == 0 {
		return out
	}

	global := v.GlobalTies()

	out = append(out, words(helvetica, size, " (tie: "))

	for i, name := range ties {
		if i > 0 {
			out = append(out, words(helvetica, size, nameSeparator))
		}

		out = append(out, value(size, name))
		out = append(out, globalMark(size, slices.Contains(global, name))...)
	}

	return append(out, words(helvetica, size, ")"))
}

// valueList renders values joined with ", ".
func valueList(size int, values []string) []seg {
	out := make([]seg, 0, 2*len(values))

	for i, v := range values {
		if i > 0 {
			out = append(out, words(helvetica, size, nameSeparator))
		}

		out = append(out, value(size, v))
	}

	return out
}

// basisNote is the basis an estimate is given on: the snapshot's retrieval date
// and what the prices leave out.
func basisNote(a assessment.Assessment) string {
	return "list prices on " + a.Snapshot.RetrievedOn.UTC().Format(dateLayout) + ", " + excludedNote
}

// hoursLine is the hours basis of a's plan prices, with one clause per
// distinct billing cap of its estimated resources, ascending (ADR 0029).
func hoursLine(a assessment.Assessment) string {
	var caps []int64

	for _, v := range a.Verdicts {
		if v.Estimate == nil || !v.Estimate.Estimated || v.Estimate.BillingCapHours <= 0 {
			continue
		}

		if !slices.Contains(caps, v.Estimate.BillingCapHours) {
			caps = append(caps, v.Estimate.BillingCapHours)
		}
	}

	slices.Sort(caps)

	line := hoursNote

	for _, c := range caps {
		h := strconv.FormatInt(c, 10)
		line += "; a plan billed for at most " + h + " hours a month costs plan price × count × min(hours, " +
			h + ") / " + h
	}

	return line
}

// estimatesOf returns the estimates of verdicts, in their order, leaving out a
// verdict without one.
func estimatesOf(verdicts []assessment.Verdict) []estimate.Estimate {
	var out []estimate.Estimate

	for _, v := range verdicts {
		if v.Estimate != nil {
			out = append(out, *v.Estimate)
		}
	}

	return out
}

// workloadEstimates returns, for each workload group of a, the estimates of the
// resources it names, in a.Verdicts' order, in one pass over the verdicts.
func workloadEstimates(a assessment.Assessment) [][]estimate.Estimate {
	groups := make(map[string][]int)

	for i, w := range a.Workloads {
		for _, id := range w.ResourceIDs {
			groups[id] = append(groups[id], i)
		}
	}

	out := make([][]estimate.Estimate, len(a.Workloads))

	for _, v := range a.Verdicts {
		if v.Estimate == nil {
			continue
		}

		for _, i := range groups[v.Resource.ID] {
			out[i] = append(out[i], *v.Estimate)
		}
	}

	return out
}

// sourceCostLines renders one "Source cost" line per currency of sc, or one
// saying the inventory gives none.
func sourceCostLines(size int, sc estimate.SourceCost) [][]seg {
	if len(sc.Currencies) == 0 {
		return [][]seg{{words(helvetica, size, "Source cost: none in the inventory")}}
	}

	lines := make([][]seg, 0, len(sc.Currencies))

	for _, c := range sc.Currencies {
		text := " " + currencySigns[c.Currency] + thousands(c.Monthly.String())
		if c.Resources != sc.Resources {
			text += " for " + strconv.Itoa(c.Resources) + " of " + strconv.Itoa(sc.Resources) + " resources"
		}

		lines = append(lines, []seg{
			words(helvetica, size, "Source cost: "),
			value(size, c.Currency),
			words(helvetica, size, text),
		})
	}

	return lines
}

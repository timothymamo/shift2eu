package htmlreport

import (
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
)

// The report's own words about an estimate, as the Markdown report has them:
// the basis every total is given on (ADR 0024, PRD #190), and the hours basis
// of a plan price (ADR 0027).
const (
	excludedNote = "excluding VAT and discounts"
	fixedFeeNote = "a fixed fee is the offering's listed fee"
	hoursNote    = "plan price × count × hours / " + hoursPerMonth + ", " + hoursPerMonth +
		" hours for a resource that gives none"
	hoursPerMonth = "730"
)

// currencySymbols are the signs a report puts before an amount in a currency it
// knows. An amount in any other currency is shown after its code alone.
var currencySymbols = map[string]string{
	"EUR": "€",
	"USD": "$",
	"GBP": "£",
}

// hasEstimates reports whether a was assessed with estimates on: its Total is
// set exactly then (assessment.AssessWith).
func hasEstimates(a assessment.Assessment) bool {
	return a.Total != nil
}

// hasRelations reports whether any verdict's resource has a relation.
func hasRelations(verdicts []assessment.Verdict) bool {
	for _, v := range verdicts {
		if len(v.Resource.Relations) > 0 {
			return true
		}
	}

	return false
}

// renderRegion renders the region the header names, with how it was chosen:
// the region --region chose, else the region the estimates are for, the
// target's default. It is nil when there is neither.
func renderRegion(a assessment.Assessment) rich {
	switch {
	case a.Region != "":
		return code(a.Region).words(" (chosen with ").code("--region").words(")")
	case a.EstimateRegion != "":
		return code(a.EstimateRegion).words(" (the target's default for estimates)")
	default:
		return nil
	}
}

// renderPlan renders the Plan cell of an estimate: the chosen plan's name and
// size, or, when no plan is large enough, the largest plan's. It is empty when
// the estimate names no plan.
func renderPlan(e *estimate.Estimate) rich {
	switch {
	case e == nil:
		return nil
	case e.Plan != nil:
		return code(e.Plan.Name).words(" (" + planSize(*e.Plan) + ")")
	case e.Reason == estimate.NoPlanLargeEnough && e.Largest != nil:
		return text("no plan large enough (largest: ").code(e.Largest.Name).words(", " + planSize(*e.Largest) + ")")
	default:
		return nil
	}
}

// planSize renders a plan's vCPUs and memory: "1 vCPU, 2 GiB".
func planSize(p estimate.Plan) string {
	return strconv.FormatInt(p.VCPUs, 10) + " vCPU, " + milliGiB(p.MemoryMilliGiB) + " GiB"
}

// milliGiB renders an amount in thousandths of a GiB as an exact decimal
// without trailing zeros: 2000 is "2", 1500 is "1.5", 512 is "0.512".
func milliGiB(m int64) string {
	whole := strconv.FormatInt(m/1000, 10)

	fraction := m % 1000
	if fraction == 0 {
		return whole
	}

	digits := strconv.FormatInt(fraction+1000, 10)[1:]

	return whole + "." + strings.TrimRight(digits, "0")
}

// renderEstimate renders the Monthly estimate cell of an estimate: its amount,
// with the fixed fee it includes, then what it assumed for a count or hours
// the resource does not give and the storage it leaves out, or "not
// estimated: " and the reason in plain words.
func renderEstimate(e *estimate.Estimate) rich {
	if e == nil {
		return nil
	}

	if !e.Estimated {
		return text("not estimated: ").then(renderNotEstimated(*e))
	}

	cell := euros(e.Monthly)
	if !e.Breakdown.FixedFee.IsZero() {
		cell += " (includes a fixed fee of " + euros(e.Breakdown.FixedFee) + ")"
	}

	if e.CountAssumed {
		cell += " (count assumed 1)"
	}

	if e.HoursAssumed {
		cell += " (" + hoursPerMonth + " hours assumed)"
	}

	if e.StorageNotIncluded != "" {
		cell += " (" + renderStorageNotIncluded(e.StorageNotIncluded) + " not included)"
	}

	return text(cell)
}

// renderStorageNotIncluded names in plain words the storage an estimate leaves
// out (ADR 0032). A value the report does not know is "storage".
func renderStorageNotIncluded(s estimate.StorageNotIncluded) string {
	switch s {
	case estimate.BootVolume:
		return "boot volume"
	case estimate.DataStorage:
		return "data storage"
	default:
		return "storage"
	}
}

// renderNotEstimated says in plain words why e is not estimated. A reason the
// report does not know is shown as its code.
func renderNotEstimated(e estimate.Estimate) rich {
	switch e.Reason {
	case estimate.NoFit:
		return text("no fit")
	case estimate.UsagePriced:
		return text("usage-priced")
	case estimate.NoSize:
		return text("size unknown")
	case estimate.NoPlan:
		return text("no plan")
	case estimate.NoPlanLargeEnough:
		return text("no plan large enough")
	case estimate.NotOffered:
		return text("not offered in ").code(e.Region)
	case estimate.NoPrice:
		return text("no price in ").code(e.Region)
	case estimate.NoStoragePrice:
		return text("no storage price in ").code(e.Region)
	default:
		return text(string(e.Reason))
	}
}

// renderRelations renders the Relations cell of v: each relation of its
// resource, in the inventory's order, as its type and its target.
func renderRelations(v assessment.Verdict) rich {
	rendered := make([]rich, 0, len(v.Resource.Relations))
	for _, rel := range v.Resource.Relations {
		rendered = append(rendered, text(string(rel.Type)+" ").code(rel.Target))
	}

	return rich{}.join(rendered, nameSeparator)
}

// reasonOrder is the order a total names the reasons its resources are not
// estimated in: the order estimate.For checks them. A reason the report does
// not know comes after these, in the order the estimates give it.
var reasonOrder = []estimate.Reason{
	estimate.NoFit,
	estimate.UsagePriced,
	estimate.NoSize,
	estimate.NoPlan,
	estimate.NoPlanLargeEnough,
	estimate.NotOffered,
	estimate.NoPrice,
	estimate.NoStoragePrice,
}

// renderTotal renders a total as the Markdown report does: "€2,535.00 per
// month for 4 of 8 resources", then "(storage not included in 2 estimates)",
// or, for a total that estimates none of at least one resource, "none of 8
// resources could be estimated" and the reasons of estimates, the estimates it
// sums.
func renderTotal(t estimate.Total, estimates []estimate.Estimate) rich {
	noun := " resources"
	if t.Resources == 1 {
		noun = " resource"
	}

	if t.Estimated == 0 && t.Resources > 0 {
		line := text("none of " + strconv.Itoa(t.Resources) + noun + " could be estimated")
		if reasons := notEstimatedReasons(estimates); len(reasons) > 0 {
			line = line.words(" (").then(reasons).words(")")
		}

		return line
	}

	line := euros(t.Monthly) + " per month for " + strconv.Itoa(t.Estimated) + " of " +
		strconv.Itoa(t.Resources) + noun

	if t.StorageNotIncluded > 0 {
		estimates := " estimates"
		if t.StorageNotIncluded == 1 {
			estimates = " estimate"
		}

		line += " (storage not included in " + strconv.Itoa(t.StorageNotIncluded) + estimates + ")"
	}

	return text(line)
}

// notEstimatedReasons renders why the estimates that are not estimated are
// not: each one's phrase once, in reasonOrder, then any reason the report does
// not know in the order the estimates give it, joined with ", ".
func notEstimatedReasons(estimates []estimate.Estimate) rich {
	var (
		phrases []rich
		seen    []string
	)

	add := func(e estimate.Estimate) {
		p := renderNotEstimated(e)
		if key := plain(p); !contains(seen, key) {
			seen = append(seen, key)
			phrases = append(phrases, p)
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
		if !e.Estimated && !contains(reasonOrder, e.Reason) {
			add(e)
		}
	}

	return rich{}.join(phrases, nameSeparator)
}

// plain returns r's text with each value marked as code, so two phrases
// compare equal exactly when they render the same.
func plain(r rich) string {
	var b strings.Builder

	for _, s := range r {
		if s.Code {
			b.WriteString("\x00" + s.Text + "\x00")

			continue
		}

		b.WriteString(s.Text)
	}

	return b.String()
}

// contains reports whether list holds v.
func contains[T comparable](list []T, v T) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}

	return false
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

// workloadEstimates returns, for each group of a.Workloads at the same index,
// the estimates of the resources it names, in a.Verdicts' order, in one pass
// over the verdicts.
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

// basisNote is the basis an estimate is given on: the snapshot's retrieval
// date and what the prices leave out.
func basisNote(a assessment.Assessment) string {
	return "list prices on " + a.Snapshot.RetrievedOn.UTC().Format(dateLayout) + ", " + excludedNote
}

// euros renders an amount in euros: "€2,535.00".
func euros(m estimate.Money) string {
	return currencySymbols["EUR"] + thousands(m.String())
}

// thousands puts a comma between each group of three digits of the whole part
// of a plain decimal: "1234567.89" is "1,234,567.89".
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

// sourceCostItems returns one "Source cost" item per currency of sc, in its
// order, each "for N of M resources" unless every resource has a cost in that
// currency, or one item saying the inventory gives none. The amount is never
// converted.
func sourceCostItems(sc estimate.SourceCost) []item {
	if len(sc.Currencies) == 0 {
		return []item{{Text: text("Source cost: none in the inventory")}}
	}

	items := make([]item, 0, len(sc.Currencies))

	for _, c := range sc.Currencies {
		line := text("Source cost: ").code(c.Currency).words(" " + currencySymbols[c.Currency] + thousands(c.Monthly.String()))
		if c.Resources != sc.Resources {
			line = line.words(" for " + strconv.Itoa(c.Resources) + " of " + strconv.Itoa(sc.Resources) + " resources")
		}

		items = append(items, item{Text: line})
	}

	return items
}

// writeWorkloads writes the per-workload section: one subsection per group of
// a.Workloads, in its order ("No workload" last), with its fit counts, its
// estimate total and source cost when a has estimates, and its blockers.
func writeWorkloads(f *fragment, a assessment.Assessment, level int) {
	f.heading(level, text("Workloads"))

	estimates := workloadEstimates(a)

	for i, w := range a.Workloads {
		name := text("No workload")
		if !w.NoWorkload {
			name = code(w.Name)
		}

		f.heading(level+1, name)

		items := []item{{Text: text("Fit: " + strconv.Itoa(w.Summary.Direct) + " direct, " +
			strconv.Itoa(w.Summary.Partial) + " partial, " + strconv.Itoa(w.Summary.None) + " none")}}

		if w.Total != nil {
			items = append(items, item{Text: text("Monthly estimate: ").then(renderTotal(*w.Total, estimates[i])).
				words(", " + basisNote(a))})
			items = append(items, sourceCostItems(w.SourceCost)...)
		}

		if len(w.Blockers) == 0 {
			items = append(items, item{Text: text("Blockers: none")})
		} else {
			blockers := blockerItems(w.Blockers)
			children := make([]rich, 0, len(blockers))

			for _, b := range blockers {
				children = append(children, b.Text)
			}

			items = append(items, item{Text: text("Blockers:"), Children: children})
		}

		f.list(items)
	}
}

// writeCostEstimate writes the cost summary of an assessment with estimates:
// the total, the basis it is given on, the region the prices are for, the
// hours basis with any billing cap (hoursLine) and the source's cost per
// currency.
func writeCostEstimate(f *fragment, a assessment.Assessment, level int) {
	f.heading(level, text("Cost estimate"))

	items := []item{
		{Text: text("Total: ").then(renderTotal(*a.Total, estimatesOf(a.Verdicts)))},
		{Text: text("Estimate: " + basisNote(a) + "; " + fixedFeeNote)},
		{Text: text("Prices in ").code(a.EstimateRegion)},
		{Text: text("Hours: " + hoursLine(a))},
	}

	f.list(append(items, sourceCostItems(a.SourceCost)...))
}

// hoursLine is the hours basis of a's plan prices: hoursNote, then one clause
// per distinct billing cap of a's estimated resources, ascending (ADR 0029).
func hoursLine(a assessment.Assessment) string {
	var caps []int64

	for _, v := range a.Verdicts {
		if v.Estimate == nil || !v.Estimate.Estimated || v.Estimate.BillingCapHours <= 0 {
			continue
		}

		caps = insertCap(caps, v.Estimate.BillingCapHours)
	}

	line := hoursNote

	for _, c := range caps {
		h := strconv.FormatInt(c, 10)
		line += "; a plan billed for at most " + h + " hours a month costs plan price × count × min(hours, " +
			h + ") / " + h
	}

	return line
}

// insertCap returns caps, ascending and without repeats, with c in it.
func insertCap(caps []int64, c int64) []int64 {
	i := 0
	for i < len(caps) && caps[i] < c {
		i++
	}

	if i < len(caps) && caps[i] == c {
		return caps
	}

	caps = append(caps, 0)
	copy(caps[i+1:], caps[i:])
	caps[i] = c

	return caps
}

package markdown

import (
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
)

// The report's own words about an estimate: the basis every total is given on
// (ADR 0024, PRD #190), and the hours basis of a plan price (ADR 0027).
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

// hasEstimates reports whether a was assessed with estimates on: its Total is set
// exactly then (assessment.AssessWith).
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

// renderRegion renders the region the header names, with how it was chosen: the
// region --region chose, whether or not estimates are on, else the region the
// estimates are for, the target's default. It is "" when there is neither, so a
// report without estimates and without a chosen region says nothing of one.
func renderRegion(a assessment.Assessment) string {
	switch {
	case a.Region != "":
		return codeSpan(a.Region, false) + " (chosen with `--region`)"
	case a.EstimateRegion != "":
		return codeSpan(a.EstimateRegion, false) + " (the target's default for estimates)"
	default:
		return ""
	}
}

// renderPlan renders the Plan cell of an estimate: the chosen plan's name and
// size, or, when no plan is large enough, the largest plan's. It is empty when
// the estimate names no plan, as for a resource priced by storage or by usage,
// one with no fit and one whose size is unknown.
func renderPlan(e *estimate.Estimate) string {
	switch {
	case e == nil:
		return ""
	case e.Plan != nil:
		return codeSpan(e.Plan.Name, true) + " (" + planSize(*e.Plan) + ")"
	case e.Reason == estimate.NoPlanLargeEnough && e.Largest != nil:
		return "no plan large enough (largest: " + codeSpan(e.Largest.Name, true) + ", " + planSize(*e.Largest) + ")"
	default:
		return ""
	}
}

// planSize renders a plan's vCPUs and memory: "1 vCPU, 2 GiB".
func planSize(p estimate.Plan) string {
	return strconv.FormatInt(p.VCPUs, 10) + " vCPU, " + milliGiB(p.MemoryMilliGiB) + " GiB"
}

// milliGiB renders an amount in thousandths of a GiB as an exact decimal without
// trailing zeros: 2000 is "2", 1500 is "1.5", 512 is "0.512".
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
// with the fixed fee it includes when there is one, then what it assumed for a
// count or hours the resource does not give, or "not estimated: " and the reason
// in plain words.
func renderEstimate(e *estimate.Estimate) string {
	if e == nil {
		return ""
	}

	if !e.Estimated {
		return "not estimated: " + renderNotEstimated(*e)
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

	return cell
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
// report does not know is shown as its code, escaped.
func renderNotEstimated(e estimate.Estimate) string {
	switch e.Reason {
	case estimate.NoFit:
		return "no fit"
	case estimate.UsagePriced:
		return "usage-priced"
	case estimate.NoSize:
		return "size unknown"
	case estimate.NotOffered:
		return "not offered in " + codeSpan(e.Region, true)
	case estimate.NoPlan:
		return "no plan"
	case estimate.NoPlanLargeEnough:
		return "no plan large enough"
	case estimate.NoPrice:
		return "no price in " + codeSpan(e.Region, true)
	case estimate.NoStoragePrice:
		return "no storage price in " + codeSpan(e.Region, true)
	default:
		return escapeText(string(e.Reason))
	}
}

// renderRelations renders the Relations cell of v: each relation of its resource,
// in the inventory's order, as its type and its target, joined with ", ".
func renderRelations(v assessment.Verdict) string {
	rendered := make([]string, 0, len(v.Resource.Relations))
	for _, rel := range v.Resource.Relations {
		rendered = append(rendered, escapeText(string(rel.Type))+" "+codeSpan(rel.Target, true))
	}

	return strings.Join(rendered, nameSeparator)
}

// reasonOrder is the order a total names the reasons its resources are not
// estimated in: the order estimate.For checks them. A reason the report does not
// know comes after these, in the order the estimates give it.
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

// renderTotal renders a total: "€2,535.00 per month for 4 of 8 resources", with
// "resource" when there is one, then "(storage not included in 2 estimates)"
// when some estimates leave out storage their plan does not include (ADR 0032).
// A total of at least one resource that estimates none has no amount (issue
// #246): "none of 8 resources could be estimated (" and the reasons of
// estimates, the estimates it sums, then ")" (notEstimatedReasons).
func renderTotal(t estimate.Total, estimates []estimate.Estimate) string {
	noun := " resources"
	if t.Resources == 1 {
		noun = " resource"
	}

	if t.Estimated == 0 && t.Resources > 0 {
		return "none of " + strconv.Itoa(t.Resources) + noun + " could be estimated (" +
			notEstimatedReasons(estimates) + ")"
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

	return line
}

// notEstimatedReasons renders why the estimates that are not estimated are not:
// each one's phrase as its Monthly estimate cell gives it after "not estimated:
// ", once each, in reasonOrder, then any reason the report does not know in
// the order the estimates give it, joined with ", ".
func notEstimatedReasons(estimates []estimate.Estimate) string {
	var phrases []string

	add := func(e estimate.Estimate) {
		if p := renderNotEstimated(e); !contains(phrases, p) {
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

	return strings.Join(phrases, nameSeparator)
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
// verdict without one; with ids set, only those of the resources it names.
func estimatesOf(verdicts []assessment.Verdict, ids []string) []estimate.Estimate {
	var out []estimate.Estimate

	for _, v := range verdicts {
		if v.Estimate == nil || (ids != nil && !contains(ids, v.Resource.ID)) {
			continue
		}

		out = append(out, *v.Estimate)
	}

	return out
}

// basisNote is the basis an estimate is given on: the snapshot's retrieval date,
// never its age, and what the prices leave out.
func basisNote(a assessment.Assessment) string {
	return "list prices on " + a.Snapshot.RetrievedOn.UTC().Format(dateLayout) + ", " + excludedNote
}

// euros renders an amount in euros: "€2,535.00".
func euros(m estimate.Money) string {
	return currencySymbols["EUR"] + thousands(m.String())
}

// renderCurrencyCost renders a source cost in one currency: its code, then the
// amount with the currency's sign when the report knows one, never converted.
func renderCurrencyCost(c estimate.CurrencyCost) string {
	return codeSpan(c.Currency, false) + " " + currencySymbols[c.Currency] + thousands(c.Monthly.String())
}

// thousands puts a comma between each group of three digits of the whole part of
// a plain decimal, as estimate.Money.String writes it: "1234567.89" is
// "1,234,567.89".
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

// writeSourceCost writes one "Source cost" item per currency of sc, in its order,
// each "for N of M resources" unless every resource has a cost in that currency,
// or one item saying the inventory gives none. Each line starts with indent.
func writeSourceCost(b *strings.Builder, sc estimate.SourceCost, indent string) {
	if len(sc.Currencies) == 0 {
		b.WriteString(indent + "- Source cost: none in the inventory\n")

		return
	}

	for _, c := range sc.Currencies {
		line := indent + "- Source cost: " + renderCurrencyCost(c)
		if c.Resources != sc.Resources {
			line += " for " + strconv.Itoa(c.Resources) + " of " + strconv.Itoa(sc.Resources) + " resources"
		}

		b.WriteString(line + "\n")
	}
}

// writeWorkloads writes the per-workload section: one subsection per group of
// a.Workloads, in its order ("No workload" last), with its fit counts, its
// estimate total and source cost when a has estimates, and its blockers. It
// starts with the blank line that ends the section before it and ends with a
// single newline.
func writeWorkloads(b *strings.Builder, a assessment.Assessment, heading string) {
	b.WriteString("\n" + heading + " Workloads\n")

	for _, w := range a.Workloads {
		name := "No workload"
		if !w.NoWorkload {
			name = codeSpan(w.Name, false)
		}

		b.WriteString("\n" + heading + "# " + name + "\n\n")
		b.WriteString("- Fit: " + strconv.Itoa(w.Summary.Direct) + " direct, " + strconv.Itoa(w.Summary.Partial) +
			" partial, " + strconv.Itoa(w.Summary.None) + " none\n")

		if w.Total != nil {
			b.WriteString("- Monthly estimate: " + renderTotal(*w.Total, estimatesOf(a.Verdicts, w.ResourceIDs)) +
				", " + basisNote(a) + "\n")
			writeSourceCost(b, w.SourceCost, "")
		}

		if len(w.Blockers) == 0 {
			b.WriteString("- Blockers: none\n")

			continue
		}

		b.WriteString("- Blockers:\n")
		writeBlockerItems(b, w.Blockers, "  ")
	}
}

// writeCostEstimate writes the cost summary of an assessment with estimates: the
// total, the basis it is given on, the region the prices are for, the hours
// basis with any billing cap (hoursLine) and the source's cost per currency. It starts with the blank line that
// ends the section before it and ends with a single newline.
func writeCostEstimate(b *strings.Builder, a assessment.Assessment, heading string) {
	b.WriteString("\n" + heading + " Cost estimate\n\n")
	b.WriteString("- Total: " + renderTotal(*a.Total, estimatesOf(a.Verdicts, nil)) + "\n")
	b.WriteString("- Estimate: " + basisNote(a) + "; " + fixedFeeNote + "\n")
	b.WriteString("- Prices in " + codeSpan(a.EstimateRegion, false) + "\n")
	b.WriteString("- Hours: " + hoursLine(a) + "\n")
	writeSourceCost(b, a.SourceCost, "")
}

// hoursLine is the hours basis of a's plan prices: hoursNote, then one clause
// per distinct billing cap of a's estimated resources, ascending and joined with
// "; ", saying how a plan with that cap is priced (ADR 0029). Without a capped
// resource it is hoursNote alone.
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

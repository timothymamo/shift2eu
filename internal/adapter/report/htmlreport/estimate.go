package htmlreport

import (
	"slices"
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
)

// The report's own words about an estimate, as the Markdown report has them:
// the basis every total is given on (ADR 0024, PRD #190), and the hours a
// month a plan price assumes for a resource that gives none (ADR 0027).
const (
	excludedNote  = "excluding VAT and discounts"
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

// renderRegion renders the region the header names: the region --region
// chose, else the region the estimates are for, the target's default, without
// a note on how it was chosen (issue #373, item 10-1). It is nil when there is
// neither.
func renderRegion(a assessment.Assessment) rich {
	switch {
	case a.Region != "":
		return code(a.Region)
	case a.EstimateRegion != "":
		return code(a.EstimateRegion)
	default:
		return nil
	}
}

// resourcesNoun is " resource" for one and " resources" for any other number.
func resourcesNoun(n int) string {
	if n == 1 {
		return " resource"
	}

	return " resources"
}

// summaryEstimate is the Summary's estimate box: its caption, the monthly
// total as its figure, and how many resources it prices, or, when it prices
// none of at least one resource, that none could be estimated.
func summaryEstimate(t estimate.Total) rich {
	box := rich{}.span("Monthly estimate").words(" ")

	if t.Estimated == 0 && t.Resources > 0 {
		return box.figure(classNone, "not estimated").words(" ").
			span("none of " + strconv.Itoa(t.Resources) + resourcesNoun(t.Resources) + " could be estimated")
	}

	return box.figure(classNone, euros(t.Monthly)).words(" ").
		span(strconv.Itoa(t.Estimated) + " of " + strconv.Itoa(t.Resources) + " priced · excl. VAT")
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
		if key := plain(p); !slices.Contains(seen, key) {
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
		if !e.Estimated && !slices.Contains(reasonOrder, e.Reason) {
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

// sourceCost renders the source's cost of sc: per currency of sc, in its
// order, the currency and the amount, "for N of M" unless every resource has
// a cost in that currency, joined with ", "; or nil when the inventory gives
// none. The amount is never converted.
func sourceCost(sc estimate.SourceCost) rich {
	if len(sc.Currencies) == 0 {
		return nil
	}

	parts := make([]rich, 0, len(sc.Currencies))

	for _, c := range sc.Currencies {
		part := code(c.Currency).words(" " + currencySymbols[c.Currency] + thousands(c.Monthly.String()))
		if c.Resources != sc.Resources {
			part = part.words(" for " + strconv.Itoa(c.Resources) + " of " + strconv.Itoa(sc.Resources))
		}

		parts = append(parts, part)
	}

	return rich{}.join(parts, nameSeparator)
}

// writeWorkloads writes the per-workload section (issue #373, item 13): one
// box per group of a.Workloads, in its order ("No workload" last), holding its
// name, one badge per fit with its count, and, when a has estimates, its
// monthly total and how many of its resources it prices, with the source's
// cost. The resources themselves are in the Resources table and the blockers
// in the Blockers section, so a box names none.
func writeWorkloads(f *fragment, a assessment.Assessment, level int) {
	f.heading(level, text("Workloads"))

	for _, w := range a.Workloads {
		f.box(func() {
			name := text("No workload")
			if !w.NoWorkload {
				name = code(w.Name)
			}

			f.heading(level+1, name)

			var badges rich

			for _, fit := range []struct {
				fit   mapping.Fit
				count int
			}{
				{fit: mapping.Direct, count: w.Summary.Direct},
				{fit: mapping.Partial, count: w.Summary.Partial},
				{fit: mapping.None, count: w.Summary.None},
			} {
				if len(badges) > 0 {
					badges = badges.words(" ")
				}

				badges = badges.badge(fitClass(fit.fit), strconv.Itoa(fit.count)+" "+string(fit.fit))
			}

			f.paragraph(badges)

			if w.Total != nil {
				writeWorkloadEstimate(f, *w.Total, w.SourceCost)
			}
		})
	}
}

// writeWorkloadEstimate writes a workload box's estimate: its monthly total
// as a figure, then how many of its resources it prices and the source's
// cost.
func writeWorkloadEstimate(f *fragment, t estimate.Total, sc estimate.SourceCost) {
	of := strconv.Itoa(t.Resources)

	if t.Estimated == 0 && t.Resources > 0 {
		f.paragraph(rich{}.figure(classNone, "not estimated"))
	} else {
		f.paragraph(rich{}.figure(classNone, euros(t.Monthly)).words(" ").span("/ month"))
	}

	line := text(strconv.Itoa(t.Estimated) + " of " + of + " priced")
	if cost := sourceCost(sc); cost != nil {
		line = line.words(" · source cost ").then(cost)
	} else {
		line = line.words(" · no source cost")
	}

	f.paragraph(line)
}

// writeCostEstimate writes the cost summary of an assessment with estimates
// (issue #373, item 14): the total, alone in its figure, with "per month, N of
// M resources" beside it, then one closing line: the region and date of the
// list prices and what they exclude, the storage the estimates leave out, and
// the source's cost. The Markdown report keeps the hours basis and the fixed
// fees in full.
func writeCostEstimate(f *fragment, a assessment.Assessment, level int) {
	f.heading(level, text("Cost estimate"))

	t := *a.Total

	if t.Estimated == 0 && t.Resources > 0 {
		line := rich{}.figure(classTotal, "none of "+strconv.Itoa(t.Resources)+resourcesNoun(t.Resources)+" could be estimated")
		if reasons := notEstimatedReasons(estimatesOf(a.Verdicts)); len(reasons) > 0 {
			line = line.words(" (").then(reasons).words(")")
		}

		f.paragraph(line)
	} else {
		f.paragraph(rich{}.figure(classTotal, euros(t.Monthly)).words(" ").
			span("per month, " + strconv.Itoa(t.Estimated) + " of " + strconv.Itoa(t.Resources) + resourcesNoun(t.Resources)))
	}

	f.paragraph(costNote(a))
}

// costNote is the Cost estimate's closing line.
func costNote(a assessment.Assessment) rich {
	note := text("List prices")
	if a.EstimateRegion != "" {
		note = note.words(" in ").code(a.EstimateRegion)
	}

	note = note.words(" on " + a.Snapshot.RetrievedOn.UTC().Format(dateLayout) + ", " + excludedNote + ".")

	if n := a.Total.StorageNotIncluded; n > 0 {
		estimates := " estimates"
		if n == 1 {
			estimates = " estimate"
		}

		note = note.words(" Storage not included in " + strconv.Itoa(n) + estimates + ".")
	}

	if cost := sourceCost(a.SourceCost); cost != nil {
		return note.words(" Source cost: ").then(cost).words(".")
	}

	return note.words(" No source cost in the inventory.")
}

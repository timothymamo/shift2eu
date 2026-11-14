package jsonreport

import (
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
)

// estimateCurrency is the currency of every estimate (ADR 0024).
const estimateCurrency = "EUR"

// excludedFromEstimates is what the list prices of an estimate leave out.
var excludedFromEstimates = []string{"VAT", "discounts"}

// estimateBasis is what an assessment's estimates are given on, and their
// total: the region and whether --region chose it, the snapshot's retrieval
// date, the currency, what the prices leave out and the hours a month a plan
// price is for.
type estimateBasis struct {
	Region       string   `json:"region"`
	RegionChosen bool     `json:"regionChosen"`
	PriceDate    string   `json:"priceDate"`
	Currency     string   `json:"currency"`
	Excluded     []string `json:"excluded"`
	HoursBasis   int      `json:"hoursBasis"`
	Total        total    `json:"total"`
}

// total sums some estimates: the amount over the estimated resources, how many
// those are and how many resources there are.
type total struct {
	Amount    string `json:"amount"`
	Estimated int    `json:"estimated"`
	Resources int    `json:"resources"`
}

// sourceCost is what the source charges, per currency, never converted, and how
// many resources the inventory gives a cost for.
type sourceCost struct {
	Covered    int            `json:"covered"`
	Resources  int            `json:"resources"`
	Currencies []currencyCost `json:"currencies"`
}

// currencyCost is the source's monthly cost in one currency.
type currencyCost struct {
	Currency  string `json:"currency"`
	Amount    string `json:"amount"`
	Resources int    `json:"resources"`
}

// workload is one group of assessment.Assessment.Workloads.
type workload struct {
	Name       string    `json:"name"`
	NoWorkload bool      `json:"noWorkload"`
	Resources  int       `json:"resources"`
	Summary    summary   `json:"summary"`
	Blockers   []blocker `json:"blockers"`
	// Estimate and SourceCost are written only when the assessment has
	// estimates.
	Estimate   *total      `json:"estimate,omitempty"`
	SourceCost *sourceCost `json:"sourceCost,omitempty"`
}

// relation is one relation of a resource, in the inventory's vocabulary.
type relation struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

// plan is the plan an estimate chose, or the largest plan when none is large
// enough. Price is written only when the plan has a price in the estimate
// region.
type plan struct {
	Name               string `json:"name"`
	VCPUs              int64  `json:"vcpus"`
	MemoryGiB          string `json:"memoryGiB"`
	IncludedStorageGiB int64  `json:"includedStorageGiB"`
	Price              string `json:"price,omitempty"`
}

// resourceEstimate is one resource's estimate: its amount, breakdown and
// whether it assumed a count of 1 or 730 hours when it is estimated, the reason
// code when it is not. The two flags are pointers so that an estimated resource
// always writes both, false included, and one that is not writes neither.
// BillingCapHours is the cap on the billed hours of the estimated plan, written
// only when the plan's offering has one (ADR 0029).
type resourceEstimate struct {
	Estimated       bool       `json:"estimated"`
	Amount          string     `json:"amount,omitempty"`
	CountAssumed    *bool      `json:"countAssumed,omitempty"`
	HoursAssumed    *bool      `json:"hoursAssumed,omitempty"`
	BillingCapHours int64      `json:"billingCapHours,omitempty"`
	Breakdown       *breakdown `json:"breakdown,omitempty"`
	Reason          string     `json:"reason,omitempty"`
}

// breakdown is what an estimate is made of.
type breakdown struct {
	Plan     string `json:"plan"`
	Storage  string `json:"storage"`
	FixedFee string `json:"fixedFee"`
}

// newEstimateBasis returns the estimate basis and total of a, or nil when a has
// no estimates (its Total is set exactly when it has, assessment.AssessWith).
func newEstimateBasis(a assessment.Assessment) *estimateBasis {
	if a.Total == nil {
		return nil
	}

	return &estimateBasis{
		Region:       a.EstimateRegion,
		RegionChosen: a.Region != "",
		PriceDate:    a.Snapshot.RetrievedOn.UTC().Format(dateLayout),
		Currency:     estimateCurrency,
		Excluded:     append([]string(nil), excludedFromEstimates...),
		HoursBasis:   estimate.HoursPerMonth,
		Total:        newTotal(*a.Total),
	}
}

// newTotal returns t as a total object.
func newTotal(t estimate.Total) total {
	return total{Amount: t.Monthly.String(), Estimated: t.Estimated, Resources: t.Resources}
}

// newAssessmentSourceCost returns the source cost of a, or nil when a has no
// estimates: a document without estimates says nothing about money.
func newAssessmentSourceCost(a assessment.Assessment) *sourceCost {
	if a.Total == nil {
		return nil
	}

	return newSourceCost(a.SourceCost)
}

// newSourceCost returns sc as a source cost object, its currencies never nil.
func newSourceCost(sc estimate.SourceCost) *sourceCost {
	currencies := make([]currencyCost, 0, len(sc.Currencies))
	for _, c := range sc.Currencies {
		currencies = append(currencies, currencyCost{
			Currency:  c.Currency,
			Amount:    c.Monthly.String(),
			Resources: c.Resources,
		})
	}

	return &sourceCost{Covered: sc.Covered, Resources: sc.Resources, Currencies: currencies}
}

// newWorkloads returns one workload per group, in the order given, never nil.
// A group's estimate and source cost are written only when it has a Total, that
// is when the assessment has estimates.
func newWorkloads(groups []assessment.WorkloadSummary) []workload {
	out := make([]workload, 0, len(groups))

	for _, g := range groups {
		w := workload{
			Name:       g.Name,
			NoWorkload: g.NoWorkload,
			Resources:  len(g.ResourceIDs),
			Summary:    newSummary(g.Summary),
			Blockers:   newBlockers(g.Blockers),
		}

		if g.Total != nil {
			t := newTotal(*g.Total)
			w.Estimate = &t
			w.SourceCost = newSourceCost(g.SourceCost)
		}

		out = append(out, w)
	}

	return out
}

// newRelations returns the relations of v's resource, in the inventory's order,
// never nil.
func newRelations(v assessment.Verdict) []relation {
	out := make([]relation, 0, len(v.Resource.Relations))
	for _, r := range v.Resource.Relations {
		out = append(out, relation{Type: string(r.Type), Target: r.Target})
	}

	return out
}

// orEmpty returns s, or an empty slice for nil, so the document writes [].
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

// newPlan returns the plan e names: its chosen plan, or the largest plan when no
// plan is large enough. It is nil when e is nil or names no plan.
func newPlan(e *estimate.Estimate) *plan {
	if e == nil {
		return nil
	}

	p := e.Plan
	if p == nil && e.Reason == estimate.NoPlanLargeEnough {
		p = e.Largest
	}

	if p == nil {
		return nil
	}

	out := &plan{
		Name:               p.Name,
		VCPUs:              p.VCPUs,
		MemoryGiB:          milliGiB(p.MemoryMilliGiB),
		IncludedStorageGiB: p.IncludedStorageGiB,
	}

	if p.Priced {
		out.Price = p.Price.String()
	}

	return out
}

// newResourceEstimate returns e as a resource estimate, or nil when e is nil
// (estimates off).
func newResourceEstimate(e *estimate.Estimate) *resourceEstimate {
	if e == nil {
		return nil
	}

	if !e.Estimated {
		return &resourceEstimate{Reason: string(e.Reason)}
	}

	countAssumed, hoursAssumed := e.CountAssumed, e.HoursAssumed

	return &resourceEstimate{
		Estimated:       true,
		Amount:          e.Monthly.String(),
		CountAssumed:    &countAssumed,
		HoursAssumed:    &hoursAssumed,
		BillingCapHours: e.BillingCapHours,
		Breakdown: &breakdown{
			Plan:     e.Breakdown.Plan.String(),
			Storage:  e.Breakdown.Storage.String(),
			FixedFee: e.Breakdown.FixedFee.String(),
		},
	}
}

// milliGiB writes an amount in thousandths of a GiB as an exact decimal without
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

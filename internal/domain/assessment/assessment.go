package assessment

import (
	"slices"

	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
)

// Blocker is one reason a resource cannot move to the target at all.
type Blocker struct {
	// ResourceID is the ID of the resource that cannot move.
	ResourceID string
	// Reason is why it cannot move.
	Reason mapping.Reason
}

// Verdict is the assessment of one resource: its mapping, plus the best
// offering and the changes moving to it requires.
type Verdict struct {
	// Resource is the resource assessed.
	Resource inventory.Resource
	// Fit is the mapping's fit.
	Fit mapping.Fit
	// BestOffering is the name of the best candidate offering, or empty when
	// there is no candidate.
	BestOffering string
	// Candidates are the mapping's candidates, best first.
	Candidates []mapping.Candidate
	// Reasons are the mapping's reasons for Fit.
	Reasons []mapping.Reason
	// RequiredChanges lists the capabilities the resource uses that the best
	// offering lacks, sorted, and nil when there are none or there is no
	// candidate.
	RequiredChanges []taxonomy.Capability
	// Estimate is the resource's plan and monthly estimate in the assessment's
	// EstimateRegion (estimate.For), and nil when estimates are off. It is set
	// for every verdict when they are on, estimated or not.
	Estimate *estimate.Estimate
	// AffectedBy holds the IDs of the resources this resource depends on or is
	// a replica of (inventory.DependsOn, inventory.ReplicaOf) that have fit
	// mapping.None in this assessment, each once, sorted in byte order, and is
	// nil when there is none (ADR 0028). The note is not transitive.
	AffectedBy []string
}

// Ties returns the names of the other candidates a reader could take for the
// best one: those with the best candidate's fit and as many missing
// capabilities, in candidate order. The best candidate is the first one. Ties
// is nil when there is no such candidate. Every report format shows the same
// ties.
func (v Verdict) Ties() []string {
	if len(v.Candidates) == 0 {
		return nil
	}

	first := v.Candidates[0]

	var ties []string

	for _, c := range v.Candidates[1:] {
		if c.Fit == first.Fit && len(c.Missing) == len(first.Missing) {
			ties = append(ties, c.Offering.Name)
		}
	}

	return ties
}

// BestOfferingGlobal reports whether the best offering is global
// (catalog.Offering.Global): not bound to a region the user chooses (PRD #581,
// ADR 0048). It is false when there is no candidate. Every report format marks
// the same offerings.
func (v Verdict) BestOfferingGlobal() bool {
	return v.BestOffering != "" && len(v.Candidates) > 0 && v.Candidates[0].Offering.Global
}

// GlobalTies returns the names of the ties (Ties) whose offering is global, in
// the order of Ties, and nil when there is none.
func (v Verdict) GlobalTies() []string {
	if len(v.Candidates) == 0 {
		return nil
	}

	first := v.Candidates[0]

	var global []string

	for _, c := range v.Candidates[1:] {
		if c.Fit == first.Fit && len(c.Missing) == len(first.Missing) && c.Offering.Global {
			global = append(global, c.Offering.Name)
		}
	}

	return global
}

// IsGlobalTie reports whether name is one of GlobalTies.
func (v Verdict) IsGlobalTie(name string) bool {
	return slices.Contains(v.GlobalTies(), name)
}

// ShowsGlobal reports whether a report that shows v's best offering and its
// ties names a global offering.
func (v Verdict) ShowsGlobal() bool {
	return v.BestOfferingGlobal() || len(v.GlobalTies()) > 0
}

// ChangesToList returns the required changes a report lists for the verdict:
// RequiredChanges for a partial fit, and nil for any other fit. Every report
// format lists the same changes.
func (v Verdict) ChangesToList() []taxonomy.Capability {
	if v.Fit != mapping.Partial {
		return nil
	}

	return v.RequiredChanges
}

// Summary counts the verdicts per fit.
type Summary struct {
	// Total is the number of resources assessed.
	Total int
	// Direct is the number of resources with fit mapping.Direct.
	Direct int
	// Partial is the number of resources with fit mapping.Partial.
	Partial int
	// None is the number of resources with fit mapping.None.
	None int
}

// Assessment is the result of assessing one inventory against one target's
// catalog.
type Assessment struct {
	// Target is the catalog's target display name.
	Target string
	// Snapshot is where and when the catalog's data was taken.
	Snapshot catalog.Snapshot
	// Source names the source the inventory was taken from.
	Source string
	// Verdicts holds one verdict per resource, in resource ID order.
	Verdicts []Verdict
	// Blockers holds one blocker per reason of every verdict with fit
	// mapping.None, in resource ID order, and is nil when there is none.
	Blockers []Blocker
	// Summary counts Verdicts per fit.
	Summary Summary
	// Region is the one target region the assessment is scoped to
	// (Options.Region), and "" when availability was judged across all of the
	// target's regions.
	Region string
	// EstimateRegion is the region the estimates are for: Region when set, else
	// the snapshot's default region. It is "" when estimates are off.
	EstimateRegion string
	// Total sums the verdicts' estimates (estimate.Sum, in verdict order), and is
	// nil when estimates are off.
	Total *estimate.Total
	// Workloads holds one group per workload, by Name in byte order, then the
	// "No workload" group last when some resource names no workload. It is nil
	// when there is no resource (ADR 0028).
	Workloads []WorkloadSummary
	// SourceCost is estimate.SumSourceCost over every resource, in verdict
	// order, whether estimates are on or off.
	SourceCost estimate.SourceCost
}

// Options are the choices an assessment takes besides its inventory and
// catalog (PRD #190, ADR 0027). The zero Options is today's assessment: every
// region, no estimates.
type Options struct {
	// Region scopes availability to one target region, compared exactly with the
	// offerings' regions (mapping.MapIn), and is the estimate region. "" judges
	// availability across all of the target's regions, as Assess does, and
	// estimates in the snapshot's default region. AssessWith does not check that
	// the target lists it: the application layer does.
	Region string
	// Estimate turns plan matching and estimates on. When it is false, no
	// verdict has an Estimate, the assessment has no EstimateRegion and no
	// Total, and every fit is what it would be for the same inventory with its
	// sizes removed.
	Estimate bool
}

// AssessWith assesses inv against cat as Assess does, with the region scope and
// the estimates opts asks for. Each resource is mapped with
// mapping.MapIn(r, cat, opts.Region); a resource left with fit mapping.None by
// the region is a blocker, with the reason mapping.NotOfferedInRegion. When
// opts.Estimate is true, each verdict gets estimate.For(result,
// EstimateRegion) and the assessment the Total of them. Sizes never change a
// fit, a blocker or the summary counts.
//
// Whatever opts holds, each verdict gets its AffectedBy notes, the assessment
// its Workloads and its SourceCost (ADR 0028); only each group's Total depends
// on opts.Estimate. Workloads, relations and costs change no fit, blocker,
// count or estimate.
//
// AssessWith(inv, cat, Options{}) equals Assess(inv, cat).
func AssessWith(inv inventory.Inventory, cat catalog.Catalog, opts Options) Assessment {
	resources := inv.Resources()

	a := Assessment{
		Target:   cat.Target(),
		Snapshot: cat.Snapshot(),
		Source:   inv.Source(),
		Verdicts: make([]Verdict, 0, len(resources)),
		Summary:  Summary{Total: len(resources)},
		Region:   opts.Region,
	}

	var estimates []estimate.Estimate

	if opts.Estimate {
		a.EstimateRegion = opts.Region
		if a.EstimateRegion == "" {
			a.EstimateRegion = cat.Snapshot().DefaultRegion
		}

		estimates = make([]estimate.Estimate, 0, len(resources))
	}

	for _, r := range resources {
		result := mapping.MapIn(r, cat, opts.Region)
		v := verdict(result)

		if opts.Estimate {
			e := estimate.For(result, a.EstimateRegion)
			v.Estimate = &e
			estimates = append(estimates, e)
		}

		a.Verdicts = append(a.Verdicts, v)

		switch v.Fit {
		case mapping.Direct:
			a.Summary.Direct++
		case mapping.Partial:
			a.Summary.Partial++
		case mapping.None:
			a.Summary.None++

			for _, reason := range v.Reasons {
				a.Blockers = append(a.Blockers, Blocker{ResourceID: v.Resource.ID, Reason: reason})
			}
		}
	}

	if opts.Estimate {
		total := estimate.Sum(estimates)
		a.Total = &total
	}

	noteAffected(a.Verdicts)
	a.Workloads = workloads(a, opts.Estimate)
	a.SourceCost = estimate.SumSourceCost(verdictResources(a.Verdicts))

	return a
}

// Assess maps every resource of inv onto cat and gathers the verdicts, blockers
// and summary counts. Fits and reasons come from mapping.Map unchanged. Resources
// come from the inventory in ID order, so the same inventory and catalog always
// give the same assessment. It judges availability across all of the target's
// regions and estimates nothing: AssessWith takes a region and turns estimates
// on.
func Assess(inv inventory.Inventory, cat catalog.Catalog) Assessment {
	return AssessWith(inv, cat, Options{})
}

// verdict turns a mapping result into a verdict.
func verdict(result mapping.Result) Verdict {
	v := Verdict{
		Resource:   result.Resource,
		Fit:        result.Fit,
		Candidates: result.Candidates,
		Reasons:    result.Reasons,
	}

	if best, ok := result.Best(); ok {
		v.BestOffering = best.Offering.Name
		v.RequiredChanges = slices.Clone(best.Missing)
	}

	return v
}

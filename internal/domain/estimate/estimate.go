package estimate

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
)

// HoursPerMonth is the hours of an average month, 365 × 24 / 12: the hours a
// monthly list price stands for, and the hoursPerMonth a resource that gives
// none runs (ADR 0025).
const HoursPerMonth = 730

// Method is how a taxonomy category is priced.
type Method string

const (
	// ByPlan is a category priced by plan: compute.vm, compute.baremetal,
	// kubernetes.managed (its nodes), database.postgresql and database.mysql.
	ByPlan Method = "plan"
	// ByStorage is a category priced by stored GiB: storage.block and
	// storage.object.
	ByStorage Method = "storage"
	// ByUsage is a category priced by requests or messages, which this tool does
	// not estimate: serverless.function and messaging.queue.
	ByUsage Method = "usage"
	// NotPriced is a category that is never priced: taxonomy.Unclassified, and
	// any category this package does not know.
	NotPriced Method = "not-priced"
)

// PricingOf returns how category c is priced.
func PricingOf(c taxonomy.Category) Method {
	switch c {
	case taxonomy.ComputeVM, taxonomy.ComputeBareMetal, taxonomy.KubernetesManaged,
		taxonomy.DatabasePostgreSQL, taxonomy.DatabaseMySQL:
		return ByPlan
	case taxonomy.StorageBlock, taxonomy.StorageObject:
		return ByStorage
	case taxonomy.ServerlessFunction, taxonomy.MessagingQueue:
		return ByUsage
	default:
		return NotPriced
	}
}

// Reason says why a resource is not estimated.
type Reason string

// The reasons a resource is not estimated, checked in this order: the first
// that applies is the one an Estimate gives.
const (
	// NoFit is the reason for a resource with fit mapping.None, including one
	// that is unclassified or not offered in the chosen region.
	NoFit Reason = "no-fit"
	// UsagePriced is the reason for a resource whose category is priced by usage
	// (ByUsage), or never priced (NotPriced) while still having a fit.
	UsagePriced Reason = "usage-priced"
	// NoSize is the reason for a resource that does not give the size its
	// category is priced by: vcpus and memoryGiB for ByPlan (both needed),
	// storageGiB for ByStorage. A size or usage that is NaN, infinite or
	// negative, which only a resource built without inventory.New can hold, is
	// no size either.
	NoSize Reason = "no-size"
	// NoPlan is the reason for a ByPlan resource whose best candidate offerings
	// list no plan at all.
	NoPlan Reason = "no-plan"
	// NoPlanLargeEnough is the reason for a ByPlan resource whose vCPUs or memory
	// no plan of the best candidate offerings covers. Estimate.Largest names the
	// largest plan.
	NoPlanLargeEnough Reason = "no-plan-large-enough"
	// NoPrice is the reason for a resource whose covering plans (ByPlan), or
	// whose best candidate offerings' storage (ByStorage), have no price in the
	// estimate region. For ByPlan, Estimate.Plan names the covering plan that
	// would have been chosen.
	NoPrice Reason = "no-price"
	// NoStoragePrice is the reason for a ByPlan resource with storage above
	// what its chosen plan includes, whose offering has no per-GiB storage price
	// in the estimate region. Estimate.Plan names the chosen plan.
	NoStoragePrice Reason = "no-storage-price"
)

// Plan is the plan an estimate chose, or names in a reason, as the catalog
// lists it.
type Plan struct {
	// Name is the plan's name.
	Name string
	// VCPUs is the plan's vCPU count.
	VCPUs int64
	// MemoryMilliGiB is the plan's memory in thousandths of a GiB.
	MemoryMilliGiB int64
	// IncludedStorageGiB is the storage the plan includes, in GiB.
	IncludedStorageGiB int64
	// Priced is true when the plan has a price in the estimate region. It is
	// false for the plan of Reason NoPrice, and may be false for Largest.
	Priced bool
	// Price is the plan's monthly price in the estimate region when Priced, and
	// zero otherwise. A price of 0 with Priced true is a free plan.
	Price Money
}

// Breakdown is what an estimate is made of. Its parts sum to Estimate.Monthly.
type Breakdown struct {
	// Plan is the plan price × count × hoursPerMonth / 730 on an uncapped
	// offering, and the plan price × count × min(hoursPerMonth, cap) / cap on an
	// offering whose PlanBillingCapHours is cap (ADR 0029); zero for ByStorage.
	// The plan is chosen by monthly price, before this scaling (see cheapest).
	Plan Money
	// Storage is the storage the estimate prices × the per-GiB price, never
	// scaled by hours: for compute.vm, compute.baremetal and kubernetes.managed
	// the GiB above what the plan includes per instance or node, × count; for
	// database.postgresql and database.mysql the GiB of the service's data above
	// what the plan includes, once (not × count: the snapshots' database storage
	// prices and included storage are for the whole service); for storage.block
	// the volume's GiB × count; for storage.object the stored GiB.
	Storage Money
	// FixedFee is the offering's fixed fee in the estimate region × hoursPerMonth
	// / 730, never capped by PlanBillingCapHours, once per resource and never ×
	// count (ADR 0026); zero when the
	// offering charges none.
	FixedFee Money
}

// Estimate is the plan and monthly list price of one resource at one target.
type Estimate struct {
	// Region is the target region the estimate is for, whether or not the
	// resource could be estimated.
	Region string
	// Estimated is true when Monthly holds the resource's estimate. When it is
	// false, Reason says why, and Monthly and Breakdown are zero: a resource that
	// is not estimated is never counted as 0 EUR.
	Estimated bool
	// Reason is why the resource is not estimated, and "" when it is.
	Reason Reason
	// Offering is the name of the offering the estimate is on: the chosen plan's
	// offering, Largest's offering for NoPlanLargeEnough, the offering whose
	// storage price was used for ByStorage, and "" otherwise.
	Offering string
	// Plan is the chosen plan: set when Estimated for ByPlan, and for NoPrice
	// and NoStoragePrice on ByPlan; nil otherwise.
	Plan *Plan
	// Largest is the largest plan of the best candidate offerings, set for
	// NoPlanLargeEnough only.
	Largest *Plan
	// Monthly is the estimated monthly list price, exact, excluding VAT and
	// discounts, set when Estimated.
	Monthly Money
	// Breakdown is what Monthly is made of, set when Estimated.
	Breakdown Breakdown
	// CountAssumed is true when the estimate priced a resource whose category
	// takes a count but that gives none, as one (ADR 0025). It is set only when
	// Estimated.
	CountAssumed bool
	// HoursAssumed is true when the estimate priced a ByPlan resource that gives
	// no hoursPerMonth as running 730 hours (ADR 0025). It is set only when
	// Estimated.
	HoursAssumed bool
}

// For estimates the resource of result, a mapping onto a target's catalog, in
// region, the estimate region (the region the user chose, or the snapshot's
// default region). See the package documentation and ADR 0027 for the rules.
//
// For only reads result. It expects result to come from mapping.Map or
// mapping.MapIn on a resource from an inventory.Inventory and a catalog from
// catalog.New.
func For(result mapping.Result, region string) Estimate {
	if result.Fit == mapping.None {
		return Estimate{Region: region, Reason: NoFit}
	}

	r := result.Resource
	offerings := bestOfferings(result.Candidates)

	switch PricingOf(r.Category) {
	case ByPlan:
		return byPlan(r, offerings, region)
	case ByStorage:
		return byStorage(r, offerings, region)
	default:
		return Estimate{Region: region, Reason: UsagePriced}
	}
}

// bestOfferings returns the offerings of the best candidates: the first one
// and every other with its fit and as many missing capabilities, in candidate
// order (assessment.Verdict.Ties names the same offerings).
func bestOfferings(candidates []mapping.Candidate) []catalog.Offering {
	if len(candidates) == 0 {
		return nil
	}

	first := candidates[0]

	var out []catalog.Offering

	for _, c := range candidates {
		if c.Fit == first.Fit && len(c.Missing) == len(first.Missing) {
			out = append(out, c.Offering)
		}
	}

	return out
}

// offeredPlan is one plan of one of the best candidate offerings.
type offeredPlan struct {
	offering catalog.Offering
	plan     catalog.Plan
}

// byPlan estimates a resource priced by plan.
func byPlan(r inventory.Resource, offerings []catalog.Offering, region string) Estimate {
	e := Estimate{Region: region}

	if r.Size == nil || r.Size.VCPUs == nil || r.Size.MemoryGiB == nil {
		e.Reason = NoSize

		return e
	}

	vcpus, okVCPUs := exact(*r.Size.VCPUs)
	memoryGiB, okMemory := exact(*r.Size.MemoryGiB)
	stored, okStored := optionalExact(r.Size.StorageGiB)
	count, countAssumed, okCount := countOf(r.Size)
	hours, hoursAssumed, okHours := hoursOf(r.Usage)

	if !okVCPUs || !okMemory || !okStored || !okCount || !okHours {
		e.Reason = NoSize

		return e
	}

	memory := new(big.Rat).Mul(memoryGiB, big.NewRat(1000, 1))

	var all []offeredPlan

	for _, o := range offerings {
		for _, p := range o.Plans {
			all = append(all, offeredPlan{offering: o, plan: p})
		}
	}

	if len(all) == 0 {
		e.Reason = NoPlan

		return e
	}

	var covering []offeredPlan

	for _, op := range all {
		if big.NewRat(op.plan.VCPUs, 1).Cmp(vcpus) >= 0 && big.NewRat(op.plan.MemoryMilliGiB, 1).Cmp(memory) >= 0 {
			covering = append(covering, op)
		}
	}

	if len(covering) == 0 {
		largest := all[0]
		for _, op := range all[1:] {
			if larger(op, largest) {
				largest = op
			}
		}

		e.Reason = NoPlanLargeEnough
		e.Offering = largest.offering.Name
		e.Largest = planOf(largest, region)

		return e
	}

	chosen, ok := cheapest(covering, region)
	if !ok {
		first := covering[0]
		for _, op := range covering[1:] {
			if smaller(op, first) {
				first = op
			}
		}

		e.Reason = NoPrice
		e.Offering = first.offering.Name
		e.Plan = planOf(first, region)

		return e
	}

	e.Offering = chosen.offering.Name
	e.Plan = planOf(chosen, region)

	storage := new(big.Rat)

	if stored != nil {
		extra := new(big.Rat).Sub(stored, big.NewRat(chosen.plan.IncludedStorageGiB, 1))
		if extra.Sign() > 0 {
			perGiB, ok := chosen.offering.StoragePrice(region)
			if !ok {
				e.Reason = NoStoragePrice

				return e
			}

			storage.Mul(extra, amount(perGiB))

			if !storedOnce(r.Category) {
				storage.Mul(storage, count)
			}
		}
	}

	planPrice := e.Plan.Price.rat()
	planPrice.Mul(planPrice, count)
	planPrice.Mul(planPrice, planShare(hours, chosen.offering.PlanBillingCapHours))

	fee := new(big.Rat)
	if a, ok := chosen.offering.FixedFee(region); ok {
		fee.Mul(amount(a), monthShare(hours))
	}

	e.Estimated = true
	e.Breakdown = Breakdown{Plan: fromRat(planPrice), Storage: fromRat(storage), FixedFee: fromRat(fee)}
	e.Monthly = e.Breakdown.Plan.Add(e.Breakdown.Storage).Add(e.Breakdown.FixedFee)
	e.CountAssumed = countAssumed
	e.HoursAssumed = hoursAssumed

	return e
}

// byStorage estimates a resource priced by stored GiB, on the best candidate
// offering with the lowest per-GiB price in region, ties by offering name.
func byStorage(r inventory.Resource, offerings []catalog.Offering, region string) Estimate {
	e := Estimate{Region: region}

	if r.Size == nil || r.Size.StorageGiB == nil {
		e.Reason = NoSize

		return e
	}

	stored, okStored := exact(*r.Size.StorageGiB)
	count, countAssumed, okCount := countOf(r.Size)

	if !okStored || (r.Category == taxonomy.StorageBlock && !okCount) {
		e.Reason = NoSize

		return e
	}

	var (
		chosen string
		perGiB catalog.Amount
		found  bool
	)

	for _, o := range offerings {
		a, ok := o.StoragePrice(region)
		if !ok {
			continue
		}

		if !found || a < perGiB || (a == perGiB && o.Name < chosen) {
			chosen, perGiB, found = o.Name, a, true
		}
	}

	if !found {
		e.Reason = NoPrice

		return e
	}

	storage := new(big.Rat).Mul(stored, amount(perGiB))

	if r.Category == taxonomy.StorageBlock {
		storage.Mul(storage, count)
		e.CountAssumed = countAssumed
	}

	e.Estimated = true
	e.Offering = chosen
	e.Breakdown = Breakdown{Storage: fromRat(storage)}
	e.Monthly = e.Breakdown.Storage

	return e
}

// cheapest returns the covering plan priced in region that is cheapest, ties
// broken by smaller, and false when none is priced there. It compares monthly
// plan prices, not the hours-scaled Breakdown.Plan: that picks the cheapest
// estimate only while every covering offering bills the same share, which
// holds while the offerings of a category in one target share one
// PlanBillingCapHours (ADR 0029). A target mixing capped and uncapped
// offerings in one category could get a plan that is dearer at partial hours.
func cheapest(covering []offeredPlan, region string) (offeredPlan, bool) {
	var (
		best      offeredPlan
		bestPrice catalog.Amount
		found     bool
	)

	for _, op := range covering {
		a, ok := op.offering.PlanPrice(op.plan.Name, region)
		if !ok {
			continue
		}

		if !found || a < bestPrice || (a == bestPrice && smaller(op, best)) {
			best, bestPrice, found = op, a, true
		}
	}

	return best, found
}

// smaller reports whether a comes before b in the plan order: fewer vCPUs, less
// memory, then plan name and offering name in byte order.
func smaller(a, b offeredPlan) bool {
	return comparePlans(a, b) < 0
}

// larger reports whether a is a larger plan than b: more vCPUs, more memory,
// then the smaller plan name and the smaller offering name in byte order.
func larger(a, b offeredPlan) bool {
	if a.plan.VCPUs != b.plan.VCPUs {
		return a.plan.VCPUs > b.plan.VCPUs
	}

	if a.plan.MemoryMilliGiB != b.plan.MemoryMilliGiB {
		return a.plan.MemoryMilliGiB > b.plan.MemoryMilliGiB
	}

	return compareNames(a, b) < 0
}

// comparePlans orders plans by vCPUs, memory, plan name and offering name.
func comparePlans(a, b offeredPlan) int {
	switch {
	case a.plan.VCPUs != b.plan.VCPUs:
		return compareInt(a.plan.VCPUs, b.plan.VCPUs)
	case a.plan.MemoryMilliGiB != b.plan.MemoryMilliGiB:
		return compareInt(a.plan.MemoryMilliGiB, b.plan.MemoryMilliGiB)
	default:
		return compareNames(a, b)
	}
}

// compareNames orders plans by plan name, then offering name, in byte order.
func compareNames(a, b offeredPlan) int {
	if d := strings.Compare(a.plan.Name, b.plan.Name); d != 0 {
		return d
	}

	return strings.Compare(a.offering.Name, b.offering.Name)
}

// compareInt returns -1, 0 or +1 as a is less than, equal to or greater than b.
func compareInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// planOf returns op's plan as an estimate names it, with its price in region.
func planOf(op offeredPlan, region string) *Plan {
	p := &Plan{
		Name:               op.plan.Name,
		VCPUs:              op.plan.VCPUs,
		MemoryMilliGiB:     op.plan.MemoryMilliGiB,
		IncludedStorageGiB: op.plan.IncludedStorageGiB,
	}

	if a, ok := op.offering.PlanPrice(op.plan.Name, region); ok {
		p.Priced = true
		p.Price = FromAmount(a)
	}

	return p
}

// storedOnce reports whether the storage of category c is the whole service's
// data, counted once and not × count: the managed databases (ADR 0027).
func storedOnce(c taxonomy.Category) bool {
	return c == taxonomy.DatabasePostgreSQL || c == taxonomy.DatabaseMySQL
}

// countOf returns the resource's count, or 1 and true when it gives none. The
// last result is false for a negative count, which no inventory holds
// (inventory.New refuses it) and which is no size at all.
func countOf(size *inventory.Size) (*big.Rat, bool, bool) {
	if size.Count == nil {
		return big.NewRat(1, 1), true, true
	}

	if *size.Count < 0 {
		return nil, false, false
	}

	return big.NewRat(int64(*size.Count), 1), false, true
}

// hoursOf returns the hours a month the resource runs, or HoursPerMonth and
// true when it gives none. The last result is false for a value exact refuses.
func hoursOf(usage *inventory.Usage) (*big.Rat, bool, bool) {
	if usage == nil || usage.HoursPerMonth == nil {
		return big.NewRat(HoursPerMonth, 1), true, true
	}

	hours, ok := exact(*usage.HoursPerMonth)

	return hours, false, ok
}

// monthShare returns hours / HoursPerMonth, the share of a monthly price that
// many hours cost when the price stands for HoursPerMonth hours.
func monthShare(hours *big.Rat) *big.Rat {
	return new(big.Rat).Quo(hours, big.NewRat(HoursPerMonth, 1))
}

// planShare returns the share of a plan's monthly price that hours cost on an
// offering whose plans are billed at most capHours a month: min(hours,
// capHours) / capHours, or monthShare(hours) when capHours is 0, uncapped
// (ADR 0029).
func planShare(hours *big.Rat, capHours int64) *big.Rat {
	if capHours <= 0 {
		return monthShare(hours)
	}

	limit := big.NewRat(capHours, 1)

	billed := hours
	if hours.Cmp(limit) > 0 {
		billed = limit
	}

	return new(big.Rat).Quo(billed, limit)
}

// amount returns the catalog amount a in euros, exactly.
func amount(a catalog.Amount) *big.Rat {
	return big.NewRat(int64(a), catalog.AmountScale)
}

// decimals is how many fractional digits of an inventory number are read
// (ADR 0027): 10^6 bounds every denominator of the arithmetic.
const decimals = 1_000_000

// exact returns v as an exact rational: the decimal of its shortest round-trip
// representation, so 0.1 is exactly 1/10, rounded to six fractional digits,
// half away from zero, so 5e-324 is 0 and 100.0000004 is 100 (ADR 0027). It
// returns false for NaN, ±Inf and a negative value: inventory.New refuses
// them, and a resource built without it that carries one has no size, never a
// size of 0 or a negative price.
func exact(v float64) (*big.Rat, bool) {
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(v, 'g', -1, 64))
	if !ok || r.Sign() < 0 {
		return nil, false
	}

	scaled := r.Mul(r, big.NewRat(decimals, 1))
	whole, rest := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))

	// The value is not negative, so half away from zero is half up.
	if rest.Lsh(rest, 1).Cmp(scaled.Denom()) >= 0 {
		whole.Add(whole, big.NewInt(1))
	}

	return new(big.Rat).SetFrac(whole, big.NewInt(decimals)), true
}

// optionalExact is exact for an optional value: nil and true when v is nil.
func optionalExact(v *float64) (*big.Rat, bool) {
	if v == nil {
		return nil, true
	}

	return exact(*v)
}

// Total sums the estimates of an assessment.
type Total struct {
	// Monthly is the exact sum of Monthly over the estimated resources only.
	Monthly Money
	// Estimated is how many resources were estimated.
	Estimated int
	// Resources is how many resources there are, estimated or not: a report
	// shows "Monthly for Estimated of Resources resources".
	Resources int
}

// Sum totals estimates: Monthly over those with Estimated true, how many those
// are, and len(estimates).
func Sum(estimates []Estimate) Total {
	t := Total{Resources: len(estimates)}

	for _, e := range estimates {
		if e.Estimated {
			t.Monthly = t.Monthly.Add(e.Monthly)
			t.Estimated++
		}
	}

	return t
}

package catalog

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// Amount is an exact amount of money in ten-thousandths of the currency unit:
// Amount(125000) is 12.50 EUR. It is never a float (ADR 0024).
type Amount int64

// AmountScale is the number of Amount units in one currency unit.
const AmountScale = 10_000

// MaxAmount is the largest Amount a Price may hold: one million currency units.
const MaxAmount Amount = 1_000_000 * AmountScale

// EUR is the only currency a Price may hold.
const EUR = "EUR"

// The caps on a plan's numbers. They are far above any real plan, and bound the
// arithmetic an estimate does with them.
const (
	// MaxPlanVCPUs is the most vCPUs a plan may have.
	MaxPlanVCPUs = 1 << 16
	// MaxPlanMemoryMilliGiB is the most memory a plan may have, in thousandths of
	// a GiB: 16,777,216 GiB.
	MaxPlanMemoryMilliGiB = (1 << 24) * 1_000
	// MaxIncludedStorageGiB is the most storage a plan may include, in GiB.
	MaxIncludedStorageGiB = 1 << 30
	// MaxBillingCapHours is the largest PlanBillingCapHours: the hours of a
	// 31-day month.
	MaxBillingCapHours = 744
)

// String formats a as a plain decimal: the integer part, a point, and the
// fraction with trailing zeros dropped but at least two digits kept, with a
// leading minus sign when negative. Amount(125000) is "12.50",
// Amount(130125) is "13.0125", Amount(400) is "0.04", Amount(0) is "0.00".
func (a Amount) String() string {
	return decimal(int64(a), AmountScale, 2)
}

// Price is a monthly list price, excluding VAT.
type Price struct {
	// Amount is the monthly price: at least 0, at most MaxAmount.
	Amount Amount
	// Currency is exactly EUR.
	Currency string
	// Regions lists the offering regions the price applies to; nil means every
	// region of the offering. In a Catalog they are sorted, free of duplicates
	// and nil when empty.
	Regions []string
}

// Plan is one size an offering sells, with its prices.
type Plan struct {
	// Name is the target's name for the plan, unique within the offering,
	// compared with every run of white space treated as a single space.
	Name string
	// VCPUs is the number of virtual (or, for bare metal, physical) CPU threads:
	// at least 1, at most MaxPlanVCPUs.
	VCPUs int64
	// MemoryMilliGiB is the memory in thousandths of a GiB: at least 1, at most
	// MaxPlanMemoryMilliGiB.
	MemoryMilliGiB int64
	// IncludedStorageGiB is the storage the plan includes, in GiB: at least 0,
	// at most MaxIncludedStorageGiB.
	IncludedStorageGiB int64
	// Prices are the plan's monthly prices, at least one, no two applying to the
	// same region. In a Catalog they are sorted by their first region, the
	// every-region price first.
	Prices []Price
}

var (
	// ErrInvalidPlan is returned for a plan whose name is blank or not valid
	// text, or whose vCPUs, memory or included storage is out of range.
	ErrInvalidPlan = errors.New("invalid plan")
	// ErrDuplicatePlan is returned when two plans of one offering share a name,
	// compared with every run of white space treated as a single space.
	ErrDuplicatePlan = errors.New("duplicate plan name")
	// ErrUnpricedPlan is returned for a plan with no price.
	ErrUnpricedPlan = errors.New("plan has no price")
	// ErrInvalidPrice is returned for a price whose amount is below 0 or above
	// MaxAmount, whose currency is not exactly EUR, or that lists a region that
	// is blank or not valid text.
	ErrInvalidPrice = errors.New("invalid price")
	// ErrPriceRegion is returned for a price that lists a region the offering is
	// not available in.
	ErrPriceRegion = errors.New("price region is not a region of the offering")
	// ErrOverlappingPrices is returned when two prices of one plan, or two
	// storage prices of one offering, apply to the same region. A price with no
	// region applies to every region.
	ErrOverlappingPrices = errors.New("overlapping prices")
	// ErrMissingFee is returned for an offering whose fixed fees leave one of its
	// regions without a fee: a region with no fee would read as free.
	ErrMissingFee = errors.New("offering region has no fixed fee")
	// ErrInvalidBillingCap is returned for an offering whose PlanBillingCapHours
	// is negative or above MaxBillingCapHours.
	ErrInvalidBillingCap = errors.New("invalid plan billing cap")
)

// regionSet holds an offering's regions, each once, so a price region is
// looked up in constant time however many regions the offering lists or
// repeats (ADR 0024).
type regionSet map[string]struct{}

// newRegionSet returns the set of regions.
func newRegionSet(regions []string) regionSet {
	set := make(regionSet, len(regions))
	for _, r := range regions {
		set[r] = struct{}{}
	}

	return set
}

// normalisePlans checks the plans of an offering available in regions, in input
// order, and returns a copy of them sorted by name, nil when empty.
func normalisePlans(plans []Plan, regions regionSet) ([]Plan, error) {
	seen := make(map[string]struct{}, len(plans))
	out := make([]Plan, 0, len(plans))

	for i, p := range plans {
		if strings.TrimSpace(p.Name) == "" {
			return nil, fmt.Errorf("plans[%d]: %w: empty name", i, ErrInvalidPlan)
		}

		if err := text.Validate(p.Name); err != nil {
			return nil, fmt.Errorf("plans[%d]: %w: name: %w", i, ErrInvalidPlan, err)
		}

		key := collapseSpace(p.Name)
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("plans[%d]: %w %s", i, ErrDuplicatePlan, text.Quote(p.Name))
		}

		seen[key] = struct{}{}

		n, err := normalisePlan(p, regions)
		if err != nil {
			return nil, fmt.Errorf("plan %s: %w", text.Quote(p.Name), err)
		}

		out = append(out, n)
	}

	if len(out) == 0 {
		return nil, nil
	}

	slices.SortFunc(out, func(a, b Plan) int { return strings.Compare(a.Name, b.Name) })

	return out, nil
}

// normalisePlan checks the numbers and prices of a plan with a valid name and
// returns a copy of it with its prices normalised.
func normalisePlan(p Plan, regions regionSet) (Plan, error) {
	switch {
	case p.VCPUs < 1:
		return Plan{}, fmt.Errorf("%w: vcpus %d, want at least 1", ErrInvalidPlan, p.VCPUs)
	case p.VCPUs > MaxPlanVCPUs:
		return Plan{}, fmt.Errorf("%w: vcpus %d, want at most %d", ErrInvalidPlan, p.VCPUs, MaxPlanVCPUs)
	case p.MemoryMilliGiB < 1:
		return Plan{}, fmt.Errorf("%w: memoryGiB %s, want more than 0",
			ErrInvalidPlan, decimal(p.MemoryMilliGiB, 1_000, 0))
	case p.MemoryMilliGiB > MaxPlanMemoryMilliGiB:
		return Plan{}, fmt.Errorf("%w: memoryGiB %s, want at most %s", ErrInvalidPlan,
			decimal(p.MemoryMilliGiB, 1_000, 0), decimal(MaxPlanMemoryMilliGiB, 1_000, 0))
	case p.IncludedStorageGiB < 0:
		return Plan{}, fmt.Errorf("%w: storageGiB %d, want at least 0", ErrInvalidPlan, p.IncludedStorageGiB)
	case p.IncludedStorageGiB > MaxIncludedStorageGiB:
		return Plan{}, fmt.Errorf("%w: storageGiB %d, want at most %d",
			ErrInvalidPlan, p.IncludedStorageGiB, MaxIncludedStorageGiB)
	}

	prices, err := normalisePrices("prices", p.Prices, regions)
	if err != nil {
		return Plan{}, err
	}

	if prices == nil {
		return Plan{}, ErrUnpricedPlan
	}

	p.Prices = prices

	return p, nil
}

// normalisePrices checks a list of prices, named list in errors, of an offering
// available in regions, and returns a copy of it with each price's regions
// sorted, de-duplicated and nil when empty, sorted by first region with the
// every-region price first, and nil when empty.
func normalisePrices(list string, prices []Price, regions regionSet) ([]Price, error) {
	out := make([]Price, 0, len(prices))
	// owner maps a region to the index of the first price that applies to it.
	owner := make(map[string]int)
	every := -1

	for i, p := range prices {
		n, err := normalisePrice(p, regions)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", list, i, err)
		}

		if err := checkOverlap(list, i, n, owner, every); err != nil {
			return nil, err
		}

		if n.Regions == nil {
			every = i
		}

		for _, r := range n.Regions {
			owner[r] = i
		}

		out = append(out, n)
	}

	if len(out) == 0 {
		return nil, nil
	}

	slices.SortFunc(out, comparePrices)

	return out, nil
}

// normaliseFixedFees checks the fixed fees of an offering available in regions
// as a price list named fixedFees, then that they cover every region of the
// offering (ADR 0026), and returns them normalised as normalisePrices does: nil
// when there are none.
func normaliseFixedFees(fees []Price, regions regionSet) ([]Price, error) {
	out, err := normalisePrices("fixedFees", fees, regions)
	if err != nil || out == nil {
		return out, err
	}

	if out[0].Regions == nil {
		return out, nil
	}

	covered := make(map[string]struct{}, len(regions))
	for _, fee := range out {
		for _, r := range fee.Regions {
			covered[r] = struct{}{}
		}
	}

	var missing []string

	for r := range regions {
		if _, ok := covered[r]; !ok {
			missing = append(missing, r)
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("fixedFees: %w: region %s", ErrMissingFee, text.Quote(slices.Min(missing)))
	}

	return out, nil
}

// checkOverlap reports an ErrOverlappingPrices error when price p, at index i
// of list, applies to a region an earlier price applies to: owner maps each
// region to its earlier price, and every is the index of an earlier
// every-region price, or -1.
func checkOverlap(list string, i int, p Price, owner map[string]int, every int) error {
	switch {
	case every >= 0 && p.Regions == nil:
		return fmt.Errorf("%s[%d]: %w: applies to every region, as %s[%d] does", list, i, ErrOverlappingPrices, list, every)
	case every >= 0:
		return fmt.Errorf("%s[%d]: %w: %s[%d] applies to every region", list, i, ErrOverlappingPrices, list, every)
	case p.Regions == nil && len(owner) > 0:
		first := firstOwned(owner)

		return fmt.Errorf("%s[%d]: %w: applies to every region, and %s[%d] applies to region %s",
			list, i, ErrOverlappingPrices, list, owner[first], text.Quote(first))
	}

	for _, r := range p.Regions {
		if j, ok := owner[r]; ok {
			return fmt.Errorf("%s[%d]: %w: applies to region %s, as %s[%d] does",
				list, i, ErrOverlappingPrices, text.Quote(r), list, j)
		}
	}

	return nil
}

// firstOwned returns the smallest region in owner, so an error names the same
// region on every run.
func firstOwned(owner map[string]int) string {
	keys := make([]string, 0, len(owner))
	for r := range owner {
		keys = append(keys, r)
	}

	return slices.Min(keys)
}

// normalisePrice checks one price of an offering available in regions and
// returns a copy of it with its regions sorted, de-duplicated and nil when
// empty.
func normalisePrice(p Price, regions regionSet) (Price, error) {
	if p.Amount < 0 {
		return Price{}, fmt.Errorf("%w: amount %s, want at least 0", ErrInvalidPrice, p.Amount)
	}

	if p.Amount > MaxAmount {
		return Price{}, fmt.Errorf("%w: amount %s, want at most %s", ErrInvalidPrice, p.Amount, MaxAmount)
	}

	if p.Currency != EUR {
		return Price{}, fmt.Errorf("%w: currency %s, want %s", ErrInvalidPrice, text.Quote(p.Currency), text.Quote(EUR))
	}

	for i, r := range p.Regions {
		if strings.TrimSpace(r) == "" {
			return Price{}, fmt.Errorf("%w: regions[%d]: empty region", ErrInvalidPrice, i)
		}

		if err := text.Validate(r); err != nil {
			return Price{}, fmt.Errorf("%w: regions[%d]: %w", ErrInvalidPrice, i, err)
		}

		if _, ok := regions[r]; !ok {
			return Price{}, fmt.Errorf("%w: region %s is not a region of the offering", ErrPriceRegion, text.Quote(r))
		}
	}

	p.Regions = sortedUnique(slices.Clone(p.Regions))

	return p, nil
}

// comparePrices orders two prices of one list by their first region, the
// every-region price (nil Regions) first. Once overlaps are refused no two
// prices share a region, so the order is total.
func comparePrices(a, b Price) int {
	switch {
	case a.Regions == nil && b.Regions == nil:
		return 0
	case a.Regions == nil:
		return -1
	case b.Regions == nil:
		return 1
	default:
		return strings.Compare(a.Regions[0], b.Regions[0])
	}
}

// clonePlans returns a copy of plans that shares no memory with it, nil when
// plans is nil.
func clonePlans(plans []Plan) []Plan {
	if plans == nil {
		return nil
	}

	out := make([]Plan, len(plans))
	for i, p := range plans {
		p.Prices = clonePrices(p.Prices)
		out[i] = p
	}

	return out
}

// clonePrices returns a copy of prices that shares no memory with it, nil when
// prices is nil.
func clonePrices(prices []Price) []Price {
	if prices == nil {
		return nil
	}

	out := make([]Price, len(prices))
	for i, p := range prices {
		p.Regions = slices.Clone(p.Regions)
		out[i] = p
	}

	return out
}

// decimal formats v, a count of 1/scale units where scale is a power of ten, as
// a plain decimal with trailing zeros dropped from the fraction but at least
// minDigits fractional digits kept, and a leading minus sign when negative.
func decimal(v, scale int64, minDigits int) string {
	sign := ""
	abs := uint64(v) //nolint:gosec // two's complement magnitude, negated below when v < 0
	if v < 0 {
		sign = "-"
		abs = -abs
	}

	digits := len(fmt.Sprint(scale)) - 1
	whole, frac := abs/uint64(scale), abs%uint64(scale) //nolint:gosec // scale is a positive power of ten

	fraction := strings.TrimRight(fmt.Sprintf("%0*d", digits, frac), "0")
	for len(fraction) < minDigits {
		fraction += "0"
	}

	if fraction == "" {
		return fmt.Sprintf("%s%d", sign, whole)
	}

	return fmt.Sprintf("%s%d.%s", sign, whole, fraction)
}

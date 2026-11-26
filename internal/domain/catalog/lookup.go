package catalog

import (
	"slices"
)

// This file holds the lookups an estimate makes in a catalog (PRD #190
// milestone 6, ADR 0027): the regions a target lists, whether an offering is
// available in a region, and the price of a plan or of a GiB of storage in a
// region. Every region is compared exactly, as FixedFee compares it: no case
// folding, no trimming.

// Regions returns every region the catalog's target lists, sorted and free of
// duplicates: the union of every offering's regions and the snapshot's default
// region. A region the snapshot declares but no offering is available in is not
// in the domain (ADR 0024) and so is not listed. A global offering lists no
// region and so adds none. The result is a fresh slice: changing it does not
// change the catalog.
func (c Catalog) Regions() []string {
	regions := []string{c.snapshot.DefaultRegion}
	for _, o := range c.offerings {
		regions = append(regions, o.Regions...)
	}

	slices.Sort(regions)

	return slices.Compact(regions)
}

// AvailableIn reports whether the offering is available in region, compared
// exactly with its regions. A global offering (Offering.Global) is available
// in every non-empty region, whether or not the target declares it. It is
// false for "" on every offering: no region asked, no availability claimed.
func (o Offering) AvailableIn(region string) bool {
	return region != "" && (o.Global || slices.Contains(o.Regions, region))
}

// PlanPrice returns the monthly price of the offering's plan named plan in
// region, and true. plan is compared exactly with the plans' names. A price
// whose Regions is nil applies to every region of the offering; a global
// offering's prices name no region, so they apply in every non-empty region.
// It returns 0 and false when the offering has no plan of that name, is not
// available in region (AvailableIn), or no price of the plan applies to
// region: a plan unpriced in a region is "not estimated" there, never free.
func (o Offering) PlanPrice(plan, region string) (Amount, bool) {
	i := slices.IndexFunc(o.Plans, func(p Plan) bool { return p.Name == plan })
	if i < 0 {
		return 0, false
	}

	return o.priceIn(o.Plans[i].Prices, region)
}

// StoragePrice returns the monthly price of one GiB of the offering's storage
// in region, and true. A price whose Regions is nil applies to every region of
// the offering; a global offering's prices name no region, so they apply in
// every non-empty region. It returns 0 and false when the offering is not
// available in region (AvailableIn) or no storage price applies to it.
func (o Offering) StoragePrice(region string) (Amount, bool) {
	return o.priceIn(o.StoragePrices, region)
}

// priceIn returns the amount of the first of prices that applies to region, a
// region the offering is available in, and true; 0 and false when the offering
// is not available there or no price applies to it.
func (o Offering) priceIn(prices []Price, region string) (Amount, bool) {
	if !o.AvailableIn(region) {
		return 0, false
	}

	for _, p := range prices {
		if p.Regions == nil || slices.Contains(p.Regions, region) {
			return p.Amount, true
		}
	}

	return 0, false
}

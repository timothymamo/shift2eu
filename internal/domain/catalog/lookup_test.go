package catalog_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// The three lookups below are what an estimate makes in a catalog: which regions
// a target lists, whether an offering serves one of them, and what a plan or a
// GiB of storage costs there.

// snapshotWith is the valid snapshot with another default region, which is the
// region estimates use when the user names none.
func snapshotWith(defaultRegion string) catalog.Snapshot {
	s := validSnapshot()
	s.DefaultRegion = defaultRegion

	return s
}

// TestRegions pins the regions a target lists: the sorted, de-duplicated union of
// every offering's regions and the snapshot's default region.
func TestRegions(t *testing.T) {
	t.Parallel()

	zoneCatalog := []catalog.Offering{
		{
			Name:       "Alpha Compute",
			Categories: []taxonomy.Category{taxonomy.ComputeVM},
			Regions:    []string{"b", "a"},
		},
		{
			Name:       "Beta Storage",
			Categories: []taxonomy.Category{taxonomy.StorageObject},
			Regions:    []string{"c", "a"},
		},
	}

	tests := []struct {
		name      string
		snapshot  catalog.Snapshot
		offerings []catalog.Offering
		want      []string
	}{
		{
			name:      "every offering region and the default, sorted and de-duplicated",
			snapshot:  snapshotWith("z"),
			offerings: zoneCatalog,
			want:      []string{"a", "b", "c", "z"},
		},
		{
			name:      "the default region is already an offering region",
			snapshot:  snapshotWith("a"),
			offerings: zoneCatalog,
			want:      []string{"a", "b", "c"},
		},
		{
			name:      "a catalog without offerings lists its default region",
			snapshot:  snapshotWith("z"),
			offerings: nil,
			want:      []string{"z"},
		},
		{
			name:     "one offering in one region",
			snapshot: snapshotWith("z"),
			offerings: []catalog.Offering{{
				Name:       "Alpha Compute",
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Regions:    []string{"a"},
			}},
			want: []string{"a", "z"},
		},
		{
			// A global offering adds no region: it is available in every region
			// and in none of its own, so --region keeps accepting exactly the
			// regions the target declares (PRD #581, The region model).
			name:     "a global offering adds no region",
			snapshot: snapshotWith("z"),
			offerings: append(slices.Clone(zoneCatalog), catalog.Offering{
				Name:       "DNS Zone",
				Categories: []taxonomy.Category{taxonomy.NetworkDNS},
				Global:     true,
			}),
			want: []string{"a", "b", "c", "z"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cat := newCatalog(t, targetName, tc.snapshot, tc.offerings)

			got := cat.Regions()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("Regions() mismatch (-want +got):\n%s", diff)
			}

			// The result is a fresh slice: changing it leaves the catalog as it was.
			if len(got) > 0 {
				got[0] = "mutated"
			}

			if diff := cmp.Diff(tc.want, cat.Regions()); diff != "" {
				t.Errorf("Regions() after changing an earlier result (-want +got):\n%s", diff)
			}
		})
	}
}

// TestOfferingAvailableIn pins that availability is an exact comparison with the
// offering's regions, and that "" is no region at all.
func TestOfferingAvailableIn(t *testing.T) {
	t.Parallel()

	cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{
		{
			Name:       "Instances",
			Categories: []taxonomy.Category{taxonomy.ComputeVM},
			Regions:    []string{"nl-ams", "fr-par"},
		},
	})

	offering := cat.Offerings()[0]

	tests := []struct {
		name   string
		region string
		want   bool
	}{
		{name: "a region the offering lists", region: "fr-par", want: true},
		{name: "another region the offering lists", region: "nl-ams", want: true},
		{name: "a region the offering does not list", region: "pl-waw", want: false},
		{name: "the region in another case", region: "FR-PAR", want: false},
		{name: "a region with white space around it", region: " fr-par ", want: false},
		{name: "the empty region", region: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := offering.AvailableIn(tc.region); got != tc.want {
				t.Errorf("AvailableIn(%s) = %t, want %t", text.Quote(tc.region), got, tc.want)
			}
		})
	}
}

// TestOfferingPlanPrice pins the plan price in a region: a price with no regions
// applies to every region of the offering, a region the offering does not list
// has no price at all, and a plan priced 0 in a region is free there, not
// unpriced.
func TestOfferingPlanPrice(t *testing.T) {
	t.Parallel()

	cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{
		{
			Name:       "Instances",
			Categories: []taxonomy.Category{taxonomy.ComputeVM},
			Regions:    []string{"fr-par", "nl-ams"},
			Plans: []catalog.Plan{
				{
					Name:           "Everywhere",
					VCPUs:          1,
					MemoryMilliGiB: 1000,
					Prices:         []catalog.Price{{Amount: 400, Currency: catalog.EUR}},
				},
				{
					Name:           "Paris Only",
					VCPUs:          2,
					MemoryMilliGiB: 2000,
					Prices: []catalog.Price{
						{Amount: 500, Currency: catalog.EUR, Regions: []string{"fr-par"}},
					},
				},
				{
					Name:           "Free",
					VCPUs:          1,
					MemoryMilliGiB: 1000,
					Prices:         []catalog.Price{{Amount: 0, Currency: catalog.EUR}},
				},
			},
		},
	})

	offering := cat.Offerings()[0]

	tests := []struct {
		name   string
		plan   string
		region string
		want   catalog.Amount
		wantOK bool
	}{
		{name: "an every-region price, in one region", plan: "Everywhere", region: "fr-par", want: 400, wantOK: true},
		{name: "an every-region price, in another region", plan: "Everywhere", region: "nl-ams", want: 400, wantOK: true},
		{name: "a one-region price, in its region", plan: "Paris Only", region: "fr-par", want: 500, wantOK: true},
		{name: "a one-region price, in another region", plan: "Paris Only", region: "nl-ams", want: 0, wantOK: false},
		{name: "a free plan is priced 0", plan: "Free", region: "fr-par", want: 0, wantOK: true},
		{name: "the region in another case", plan: "Everywhere", region: "FR-PAR", want: 0, wantOK: false},
		{name: "the empty region", plan: "Everywhere", region: "", want: 0, wantOK: false},
		{name: "a region the offering does not list", plan: "Everywhere", region: "pl-waw", want: 0, wantOK: false},
		{name: "a plan the offering does not sell", plan: "Missing", region: "fr-par", want: 0, wantOK: false},
		{name: "a plan name in another case", plan: "everywhere", region: "fr-par", want: 0, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := offering.PlanPrice(tc.plan, tc.region)
			if ok != tc.wantOK {
				t.Fatalf("PlanPrice(%s, %s) reports a price: %t, want %t",
					text.Quote(tc.plan), text.Quote(tc.region), ok, tc.wantOK)
			}

			if got != tc.want {
				t.Errorf("PlanPrice(%s, %s) = %s, want %s",
					text.Quote(tc.plan), text.Quote(tc.region), got, tc.want)
			}
		})
	}
}

// TestOfferingStoragePrice pins the per-GiB storage price in a region, with the
// same rules as a plan price: an every-region price applies everywhere, and an
// offering without storage prices has none in any region.
func TestOfferingStoragePrice(t *testing.T) {
	t.Parallel()

	cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{
		{
			Name:       "Block Storage",
			Categories: []taxonomy.Category{taxonomy.StorageBlock},
			Regions:    []string{"fr-par", "nl-ams", "pl-waw"},
			StoragePrices: []catalog.Price{
				{Amount: 0, Currency: catalog.EUR},
			},
		},
		{
			Name:       "Object Storage",
			Categories: []taxonomy.Category{taxonomy.StorageObject},
			Regions:    []string{"fr-par"},
			StoragePrices: []catalog.Price{
				{Amount: 250, Currency: catalog.EUR, Regions: []string{"fr-par"}},
			},
		},
		{
			Name:       "Instances",
			Categories: []taxonomy.Category{taxonomy.ComputeVM},
			Regions:    []string{"fr-par"},
		},
	})

	offerings := cat.Offerings()

	block := offeringNamed(t, offerings, "Block Storage")
	object := offeringNamed(t, offerings, "Object Storage")
	machines := offeringNamed(t, offerings, "Instances")

	tests := []struct {
		name     string
		offering catalog.Offering
		region   string
		want     catalog.Amount
		wantOK   bool
	}{
		{name: "an every-region price, free storage", offering: block, region: "nl-ams", want: 0, wantOK: true},
		{name: "a one-region price, in its region", offering: object, region: "fr-par", want: 250, wantOK: true},
		{name: "a one-region price, elsewhere", offering: object, region: "nl-ams", want: 0, wantOK: false},
		{name: "the empty region", offering: object, region: "", want: 0, wantOK: false},
		{name: "an offering without storage prices", offering: machines, region: "fr-par", want: 0, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tc.offering.StoragePrice(tc.region)
			if ok != tc.wantOK {
				t.Fatalf("StoragePrice(%s) of %s reports a price: %t, want %t",
					text.Quote(tc.region), text.Quote(tc.offering.Name), ok, tc.wantOK)
			}

			if got != tc.want {
				t.Errorf("StoragePrice(%s) of %s = %s, want %s",
					text.Quote(tc.region), text.Quote(tc.offering.Name), got, tc.want)
			}
		})
	}
}

// offeringNamed returns the offering with that name, failing the test when the
// catalog dropped it.
func offeringNamed(t *testing.T, offerings []catalog.Offering, name string) catalog.Offering {
	t.Helper()

	i := slices.IndexFunc(offerings, func(o catalog.Offering) bool { return o.Name == name })
	if i < 0 {
		t.Fatalf("the catalog holds no offering named %s", text.Quote(name))
	}

	return offerings[i]
}

// globalOffering is a network.dns offering recorded as global, the one category
// the taxonomy marks global (PRD #581, The region model): it is in no declared
// region and is available in every non-empty one.
func globalOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "DNS Zone",
		Categories: []taxonomy.Category{taxonomy.NetworkDNS},
		Global:     true,
	}
}

// TestOfferingAvailableInGlobal pins availability on a global offering: true for
// every non-empty region, the target's declared ones and any other, and false for
// "", as for a regional offering (no region asked, no availability claimed).
func TestOfferingAvailableInGlobal(t *testing.T) {
	t.Parallel()

	cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{globalOffering()})
	offering := cat.Offerings()[0]

	tests := []struct {
		name   string
		region string
		want   bool
	}{
		{name: "a declared region", region: "fr-par", want: true},
		{name: "another declared region", region: "nl-ams", want: true},
		{name: "a region the target declares nowhere", region: "de-fra", want: true},
		{name: "a region in another case", region: "FR-PAR", want: true},
		{name: "the empty region", region: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := offering.AvailableIn(tc.region); got != tc.want {
				t.Errorf("AvailableIn(%s) = %t, want %t", text.Quote(tc.region), got, tc.want)
			}
		})
	}
}

// TestOfferingGlobalPrices pins the prices and the fixed fee of a global
// offering (PRD #581, Estimates): they apply in the estimate region, whatever it
// is, because the offering is offered there; and "" gets none of them, as on a
// regional offering.
func TestOfferingGlobalPrices(t *testing.T) {
	t.Parallel()

	offering := globalOffering()
	offering.Plans = []catalog.Plan{{
		Name:           "Zone",
		VCPUs:          1,
		MemoryMilliGiB: 1000,
		Prices:         []catalog.Price{{Amount: 1200, Currency: catalog.EUR}},
	}}
	offering.StoragePrices = []catalog.Price{{Amount: 40, Currency: catalog.EUR}}
	offering.FixedFees = []catalog.Price{{Amount: 500, Currency: catalog.EUR}}

	cat := newCatalog(t, targetName, validSnapshot(), []catalog.Offering{offering})

	got := cat.Offerings()[0]

	t.Run("every non-empty region", func(t *testing.T) {
		t.Parallel()

		for _, region := range []string{"fr-par", "nl-ams", "de-fra", "FR-PAR"} {
			if amount, ok := got.PlanPrice("Zone", region); !ok || amount != 1200 {
				t.Errorf("PlanPrice(\"Zone\", %s) = (%s, %t), want (12.00, true)", text.Quote(region), amount, ok)
			}

			if amount, ok := got.StoragePrice(region); !ok || amount != 40 {
				t.Errorf("StoragePrice(%s) = (%s, %t), want (0.40, true)", text.Quote(region), amount, ok)
			}

			if amount, ok := got.FixedFee(region); !ok || amount != 500 {
				t.Errorf("FixedFee(%s) = (%s, %t), want (5.00, true)", text.Quote(region), amount, ok)
			}
		}
	})

	t.Run("the empty region", func(t *testing.T) {
		t.Parallel()

		if amount, ok := got.PlanPrice("Zone", ""); ok {
			t.Errorf("PlanPrice(\"Zone\", \"\") = (%s, true), want no price", amount)
		}

		if amount, ok := got.StoragePrice(""); ok {
			t.Errorf("StoragePrice(\"\") = (%s, true), want no price", amount)
		}

		if amount, ok := got.FixedFee(""); ok {
			t.Errorf("FixedFee(\"\") = (%s, true), want no fee", amount)
		}
	})

	t.Run("a plan the offering does not sell", func(t *testing.T) {
		t.Parallel()

		if amount, ok := got.PlanPrice("Missing", "fr-par"); ok {
			t.Errorf("PlanPrice(\"Missing\", \"fr-par\") = (%s, true), want no price", amount)
		}
	})
}

package testutil

import (
	"testing"

	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
)

// The fixture below is what a report test needs once an assessment carries
// plans, estimates, relations and workloads. The priced fixture cannot be it:
// every one of its plans covers a plan-shaped resource, it charges no fixed fee
// and it serves no usage-priced category, so a report built on it can show
// neither "no plan large enough" nor a fixed fee nor "usage-priced".
//
// Every name, region and amount is invented, so a report test can never pass by
// agreeing with a real provider's price list. The prices are round numbers, so a
// test states each estimate exactly: a plan costs its price × count × hours / 730,
// the object storage its stored GiB × its per-GiB price, and the Kubernetes
// offering charges its fixed fee once.
const (
	// ReportSecondRegion is the report fixture's second region. The compute plans
	// cost a fifth more there than in PricedDefaultRegion, so two reports of one
	// inventory for the two regions differ in their estimates and in nothing else
	// but the region name.
	ReportSecondRegion = "xx-ams"

	// reportComputeOffering serves compute.vm with three plans, priced per region:
	// 10.00, 20.00 and 40.00 EUR a month in PricedDefaultRegion, and 12.00, 24.00
	// and 48.00 in ReportSecondRegion. The largest plan is 4 vCPUs and 8 GiB, so a
	// larger virtual machine has no plan large enough.
	reportComputeOffering = "Example Virtual Machines"
	// reportKubernetesOffering serves kubernetes.managed with a plan of its own
	// and a fixed fee per cluster, so an estimate of it has a fixed-fee part.
	reportKubernetesOffering = "Example Kubernetes"
	// reportObjectOffering serves storage.object by the stored GiB.
	reportObjectOffering = "Example Object Storage"
	// reportUsageOffering serves serverless.function, which is priced by usage: a
	// function has a fit but no estimate, with the reason "usage-priced".
	reportUsageOffering = "Example Functions"
)

// ReportOfferings is the report fixture catalog: a compute offering with three
// plans priced per region, a Kubernetes offering with a plan and a fixed fee, an
// object storage offering priced by the GiB, and a usage-priced function
// offering. It returns a fresh slice on every call, so a test may mutate its
// result.
func ReportOfferings() []catalog.Offering {
	return []catalog.Offering{
		{
			Name:         reportComputeOffering,
			Categories:   []taxonomy.Category{taxonomy.ComputeVM},
			Capabilities: []taxonomy.Capability{taxonomy.IPv6, taxonomy.Snapshots},
			Regions:      []string{PricedDefaultRegion, ReportSecondRegion},
			Plans: []catalog.Plan{
				{
					Name:               "EX-S",
					VCPUs:              1,
					MemoryMilliGiB:     2000,
					IncludedStorageGiB: 20,
					Prices: []catalog.Price{
						{Amount: 120000, Currency: catalog.EUR, Regions: []string{ReportSecondRegion}},
						{Amount: 100000, Currency: catalog.EUR, Regions: []string{PricedDefaultRegion}},
					},
				},
				{
					Name:               "EX-M",
					VCPUs:              2,
					MemoryMilliGiB:     4000,
					IncludedStorageGiB: 40,
					Prices: []catalog.Price{
						{Amount: 240000, Currency: catalog.EUR, Regions: []string{ReportSecondRegion}},
						{Amount: 200000, Currency: catalog.EUR, Regions: []string{PricedDefaultRegion}},
					},
				},
				{
					Name:               "EX-L",
					VCPUs:              4,
					MemoryMilliGiB:     8000,
					IncludedStorageGiB: 80,
					Prices: []catalog.Price{
						{Amount: 480000, Currency: catalog.EUR, Regions: []string{ReportSecondRegion}},
						{Amount: 400000, Currency: catalog.EUR, Regions: []string{PricedDefaultRegion}},
					},
				},
			},
			StoragePrices: []catalog.Price{{Amount: 1000, Currency: catalog.EUR}},
		},
		{
			Name:         reportKubernetesOffering,
			Categories:   []taxonomy.Category{taxonomy.KubernetesManaged},
			Capabilities: []taxonomy.Capability{taxonomy.Autoscaling},
			Regions:      []string{PricedDefaultRegion, ReportSecondRegion},
			Plans: []catalog.Plan{
				{
					Name:           "KN-S",
					VCPUs:          1,
					MemoryMilliGiB: 2000,
					Prices:         []catalog.Price{{Amount: 100000, Currency: catalog.EUR}},
				},
			},
			FixedFees: []catalog.Price{{Amount: 50000, Currency: catalog.EUR}},
		},
		{
			Name:          reportObjectOffering,
			Categories:    []taxonomy.Category{taxonomy.StorageObject},
			Capabilities:  []taxonomy.Capability{taxonomy.IPv6, taxonomy.ObjectVersioning},
			Regions:       []string{PricedDefaultRegion, ReportSecondRegion},
			StoragePrices: []catalog.Price{{Amount: 500, Currency: catalog.EUR}},
		},
		{
			Name:       reportUsageOffering,
			Categories: []taxonomy.Category{taxonomy.ServerlessFunction},
			Regions:    []string{PricedDefaultRegion, ReportSecondRegion},
		},
	}
}

// ReportCatalog is the report fixture catalog, built through catalog.New on the
// priced fixture's invented snapshot (the target Example Cloud, its source URL
// and its retrieval date), so a report test shows one dated price list.
func ReportCatalog(tb testing.TB) catalog.Catalog {
	tb.Helper()

	return ReportCatalogIn(tb, pricedTarget, PricedSnapshot())
}

// ReportCatalogIn is the report fixture's offerings as the catalog of target,
// taken from snapshot: a comparison test gives the same plans and prices to two
// targets whose display names, snapshots and default regions differ.
func ReportCatalogIn(tb testing.TB, target string, snapshot catalog.Snapshot) catalog.Catalog {
	tb.Helper()

	cat, err := catalog.New(target, snapshot, ReportOfferings())
	if err != nil {
		tb.Fatalf("catalog.New() returned unexpected error: %v", err)

		return catalog.Catalog{}
	}

	return cat
}

// ReportCapHours is the hours a month the report fixture's capped compute
// offering bills its plans for (ADR 0029): 672, the hours of four weeks. Every
// value of the fixture is invented, the cap included, so a test states the
// capped arithmetic exactly.
const ReportCapHours int64 = 672

// ReportCappedOfferings is ReportOfferings with its compute offering billing a
// plan for at most ReportCapHours hours a month (ADR 0029). Every plan, price
// and region is the report fixture's, so a report built on it differs from the
// report fixture's exactly where the cap shows: in the hours line's cap clause,
// and in the billingCapHours of a plan-priced estimate.
func ReportCappedOfferings() []catalog.Offering {
	offerings := ReportOfferings()
	// ReportOfferings puts the compute offering first. It is the offering the
	// fixture's compute.vm resources are priced on, which is the cap a report
	// shows.
	offerings[0].PlanBillingCapHours = ReportCapHours

	return offerings
}

// ReportCappedCatalog is ReportCappedOfferings as the report fixture's catalog:
// the target Example Cloud, on the priced fixture's snapshot.
func ReportCappedCatalog(tb testing.TB) catalog.Catalog {
	tb.Helper()

	return ReportCappedCatalogIn(tb, pricedTarget, PricedSnapshot())
}

// ReportCappedCatalogIn is the capped report fixture's offerings as the catalog
// of target, taken from snapshot, so a comparison test gives the cap to one
// target and not to another.
func ReportCappedCatalogIn(tb testing.TB, target string, snapshot catalog.Snapshot) catalog.Catalog {
	tb.Helper()

	cat, err := catalog.New(target, snapshot, ReportCappedOfferings())
	if err != nil {
		tb.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	return cat
}

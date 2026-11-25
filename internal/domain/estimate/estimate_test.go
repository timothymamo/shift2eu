package estimate_test

import (
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The catalog below is the priced fixture's, whose plans and prices are invented,
// or an invented catalog built here for the categories the fixture does not
// serve. No real provider price appears in this file.

const (
	// pricedTarget is the display name the priced fixture's snapshot belongs to.
	pricedTarget = "Example Cloud"
	// pricedSource names the source the test inventories were taken from.
	pricedSource = "aws"
)

// f is a given float64, as an inventory size or usage carries it.
func f(v float64) *float64 { return &v }

// n is a given int, as an inventory count carries it.
func n(v int) *int { return &v }

// newCatalog builds a catalog through catalog.New over the priced fixture's
// snapshot, so For reads the same canonical form the program does.
func newCatalog(t *testing.T, offerings []catalog.Offering) catalog.Catalog {
	t.Helper()

	cat, err := catalog.New(pricedTarget, testutil.PricedSnapshot(), offerings)
	if err != nil {
		t.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	return cat
}

// newResource builds a resource through inventory.New, so For reads the same
// canonical form the program does.
func newResource(t *testing.T, r inventory.Resource) inventory.Resource {
	t.Helper()

	inv, err := inventory.New(pricedSource, []inventory.Resource{r})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv.Resources()[0]
}

// plan is one plan of a test offering: priced in the given regions, or, with no
// region named, everywhere the offering is sold.
func plan(name string, vcpus, memoryMilliGiB, includedStorageGiB int64, amount catalog.Amount, regions ...string) catalog.Plan {
	return catalog.Plan{
		Name:               name,
		VCPUs:              vcpus,
		MemoryMilliGiB:     memoryMilliGiB,
		IncludedStorageGiB: includedStorageGiB,
		Prices:             []catalog.Price{price(amount, regions...)},
	}
}

// price is one monthly price, in the given regions or everywhere.
func price(amount catalog.Amount, regions ...string) catalog.Price {
	p := catalog.Price{Amount: amount, Currency: catalog.EUR}
	if len(regions) > 0 {
		p.Regions = regions
	}

	return p
}

// pricedPlan is the plan an estimate chose: priced in the estimate region.
func pricedPlan(name string, vcpus, memoryMilliGiB, includedStorageGiB int64, amount catalog.Amount) *estimate.Plan {
	return &estimate.Plan{
		Name:               name,
		VCPUs:              vcpus,
		MemoryMilliGiB:     memoryMilliGiB,
		IncludedStorageGiB: includedStorageGiB,
		Priced:             true,
		Price:              estimate.FromAmount(amount),
	}
}

// unpricedPlan is a plan the estimate named but could not price in the estimate
// region.
func unpricedPlan(name string, vcpus, memoryMilliGiB, includedStorageGiB int64) *estimate.Plan {
	return &estimate.Plan{
		Name:               name,
		VCPUs:              vcpus,
		MemoryMilliGiB:     memoryMilliGiB,
		IncludedStorageGiB: includedStorageGiB,
	}
}

// TestPricingOf pins how each taxonomy category is priced, and that a category
// the package does not know is never priced.
func TestPricingOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		category taxonomy.Category
		want     estimate.Method
	}{
		{name: "compute.vm", category: taxonomy.ComputeVM, want: estimate.ByPlan},
		{name: "compute.baremetal", category: taxonomy.ComputeBareMetal, want: estimate.ByPlan},
		{name: "kubernetes.managed", category: taxonomy.KubernetesManaged, want: estimate.ByPlan},
		{name: "database.postgresql", category: taxonomy.DatabasePostgreSQL, want: estimate.ByPlan},
		{name: "database.mysql", category: taxonomy.DatabaseMySQL, want: estimate.ByPlan},
		{name: "storage.block", category: taxonomy.StorageBlock, want: estimate.ByStorage},
		{name: "storage.object", category: taxonomy.StorageObject, want: estimate.ByStorage},
		{name: "serverless.function", category: taxonomy.ServerlessFunction, want: estimate.ByUsage},
		{name: "messaging.queue", category: taxonomy.MessagingQueue, want: estimate.ByUsage},
		// PRD #552: caches and MongoDB databases are priced by plan, file shares
		// by GiB, topics and serverless containers by usage, and load balancers
		// and registries are never priced (ADR 0046).
		{name: "cache.redis", category: taxonomy.CacheRedis, want: estimate.ByPlan},
		{name: "database.mongodb", category: taxonomy.DatabaseMongoDB, want: estimate.ByPlan},
		{name: "storage.file", category: taxonomy.StorageFile, want: estimate.ByStorage},
		{name: "messaging.topic", category: taxonomy.MessagingTopic, want: estimate.ByUsage},
		{name: "serverless.container", category: taxonomy.ServerlessContainer, want: estimate.ByUsage},
		{name: "network.loadbalancer", category: taxonomy.NetworkLoadBalancer, want: estimate.NotPriced},
		{name: "registry.container", category: taxonomy.RegistryContainer, want: estimate.NotPriced},
		// PRD #571's two categories are priced by plan, on vCPUs and memory, as
		// every plan-priced category but cache.redis is.
		{name: "search.opensearch", category: searchCategory, want: estimate.ByPlan},
		{name: "messaging.kafka", category: kafkaCategory, want: estimate.ByPlan},
		{name: "unclassified", category: taxonomy.Unclassified, want: estimate.NotPriced},
		{name: "a category the taxonomy does not know", category: taxonomy.Category("quantum.widget"), want: estimate.NotPriced},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := estimate.PricingOf(tc.category); got != tc.want {
				t.Errorf("PricingOf(%q) = %q, want %q", tc.category, got, tc.want)
			}
		})
	}
}

// computeResource is a sized compute.vm resource, the category most cases price.
func computeResource(size *inventory.Size, usage *inventory.Usage) inventory.Resource {
	return inventory.Resource{
		ID:       "i-0123456789abcdef0",
		RawType:  "AWS::EC2::Instance",
		Category: taxonomy.ComputeVM,
		Region:   "eu-west-1",
		Size:     size,
		Usage:    usage,
	}
}

// TestFor pins one estimate per priced category, the plan choice and its tie
// breaks, every "not estimated" reason and its precedence, the plan share hours
// scale a plan price by (capped by an offering's PlanBillingCapHours, ADR 0029)
// and the exact arithmetic with the display rounding that follows it.
func TestFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resource inventory.Resource
		// handBuilt builds the resource without inventory.New, as only a caller
		// that skips the validation can: a size or usage may then hold a number
		// the validation would have refused.
		handBuilt   bool
		offerings   []catalog.Offering
		region      string
		want        estimate.Estimate
		wantMonthly string
		wantExact   string
		// wantPlanExact is pinned when the plan part of the breakdown is what the
		// case is about.
		wantPlanExact string
	}{
		// The worked examples of the contract, one per priced category.
		{
			name:      "compute.vm: the cheapest covering plan, with the default count and hours",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Virtual Machines",
				Plan:         pricedPlan("EX-M", 2, 4500, 40, 250000),
				Monthly:      estimate.FromAmount(250000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(250000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "25.00",
			wantExact:   "25",
		},
		{
			name: "compute.vm: count, hours and the storage above what the plan includes",
			resource: computeResource(&inventory.Size{
				VCPUs:      f(2),
				MemoryGiB:  f(4),
				StorageGiB: f(100),
				Count:      n(3),
			}, &inventory.Usage{HoursPerMonth: f(365)}),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Virtual Machines",
				Plan:      pricedPlan("EX-M", 2, 4500, 40, 250000),
				// 25 × 3 × 365 / 730 = 37.5; (100 − 40) × 0.04 × 3 = 7.2.
				Monthly: estimate.Ratio(447, 10),
				Breakdown: estimate.Breakdown{
					Plan:    estimate.Ratio(75, 2),
					Storage: estimate.Ratio(36, 5),
				},
			},
			wantMonthly: "44.70",
			wantExact:   "447/10",
		},
		{
			// The smallest plan includes no storage, and the offering prices it, so
			// a resource that gives no storage size leaves its boot volume out
			// (ADR 0032).
			name:      "compute.vm: a small resource takes the smallest covering plan",
			resource:  computeResource(&inventory.Size{VCPUs: f(1), MemoryGiB: f(1)}, nil),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:             testutil.PricedDefaultRegion,
				Estimated:          true,
				Offering:           "Example Virtual Machines",
				Plan:               pricedPlan("EX-S", 1, 2000, 0, 120000),
				Monthly:            estimate.FromAmount(120000),
				Breakdown:          estimate.Breakdown{Plan: estimate.FromAmount(120000)},
				CountAssumed:       true,
				HoursAssumed:       true,
				StorageNotIncluded: estimate.BootVolume,
			},
			wantMonthly: "12.00",
		},
		{
			// Bare metal is priced like compute.vm: a plan per server, storage
			// above what the plan includes for each server, and the count.
			name: "compute.baremetal: a plan, storage per server, and a count",
			resource: inventory.Resource{
				ID:       "i-0123456789abcdef0",
				RawType:  "AWS::EC2::Instance",
				Category: taxonomy.ComputeBareMetal,
				Region:   "eu-west-1",
				Size:     &inventory.Size{VCPUs: f(8), MemoryGiB: f(16), StorageGiB: f(200), Count: n(2)},
			},
			offerings: []catalog.Offering{bareMetalOffering()},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Bare Metal",
				Plan:      pricedPlan("BM-1", 8, 16000, 100, 480000),
				// 48 × 2 = 96; (200 − 100) × 0.05 × 2 = 10.
				Monthly: estimate.FromAmount(1060000),
				Breakdown: estimate.Breakdown{
					Plan:    estimate.FromAmount(960000),
					Storage: estimate.FromAmount(100000),
				},
				HoursAssumed: true,
			},
			wantMonthly: "106.00",
			wantExact:   "106",
		},
		{
			name: "kubernetes.managed: storage per node and the fixed fee once, not per node",
			resource: inventory.Resource{
				ID:       "cluster-example",
				RawType:  "AWS::EKS::Cluster",
				Category: taxonomy.KubernetesManaged,
				Region:   "eu-west-1",
				Size: &inventory.Size{
					VCPUs:      f(4),
					MemoryGiB:  f(8),
					StorageGiB: f(80),
					Count:      n(3),
				},
			},
			offerings: []catalog.Offering{kubernetesOffering()},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Kubernetes",
				Plan:      pricedPlan("NK-1", 4, 8000, 50, 300000),
				// 30 × 3 = 90; (80 − 50) × 0.03 × 3 = 2.7; the fee is 9 once.
				Monthly: estimate.Ratio(1017, 10),
				Breakdown: estimate.Breakdown{
					Plan:     estimate.FromAmount(900000),
					Storage:  estimate.Ratio(27, 10),
					FixedFee: estimate.FromAmount(90000),
				},
				HoursAssumed: true,
			},
			wantMonthly: "101.70",
			wantExact:   "1017/10",
		},
		{
			// Hours scale the rates of a monthly price list, the plan price and
			// the fixed fee (ADR 0026); storage is a quantity, not a rate, so half
			// the hours cost half the plan and half the fee but all the storage.
			name: "kubernetes.managed: half the hours halve the plan and the fee, not the storage",
			resource: inventory.Resource{
				ID:       "cluster-example",
				RawType:  "AWS::EKS::Cluster",
				Category: taxonomy.KubernetesManaged,
				Region:   "eu-west-1",
				Size: &inventory.Size{
					VCPUs:      f(4),
					MemoryGiB:  f(8),
					StorageGiB: f(80),
					Count:      n(3),
				},
				Usage: &inventory.Usage{HoursPerMonth: f(365)},
			},
			offerings: []catalog.Offering{kubernetesOffering()},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Kubernetes",
				Plan:      pricedPlan("NK-1", 4, 8000, 50, 300000),
				// 30 × 3 × 365/730 = 45; (80 − 50) × 0.03 × 3 = 2.7, unscaled;
				// the fee 9 × 365/730 = 4.5, once and never × 3.
				Monthly: estimate.Ratio(261, 5),
				Breakdown: estimate.Breakdown{
					Plan:     estimate.FromAmount(450000),
					Storage:  estimate.Ratio(27, 10),
					FixedFee: estimate.Ratio(9, 2),
				},
			},
			wantMonthly: "52.20",
			wantExact:   "261/5",
		},
		// ADR 0029: an offering may cap the hours a month it bills a plan for, and
		// its plan prices are then the price of that many hours. The cap applies to
		// plan prices alone, never to fixed fees or storage, and an offering without
		// a cap estimates as it always did.
		{
			name:      "plan billing cap: half the cap costs half the plan price",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(336)}),
			offerings: []catalog.Offering{cappedComputeOffering(672)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:          testutil.PricedDefaultRegion,
				Estimated:       true,
				Offering:        "Example Compute Plans",
				Plan:            pricedPlan("CAP-M", 2, 4000, 20, 380000),
				BillingCapHours: 672,
				// 38.00 × min(336, 672) / 672 = 19.00, exactly half.
				Monthly:      estimate.FromAmount(190000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(190000)},
				CountAssumed: true,
			},
			wantMonthly:   "19.00",
			wantExact:     "19",
			wantPlanExact: "19",
		},
		{
			name:      "plan billing cap: exactly the cap costs the whole plan price",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(672)}),
			offerings: []catalog.Offering{cappedComputeOffering(672)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:          testutil.PricedDefaultRegion,
				Estimated:       true,
				Offering:        "Example Compute Plans",
				Plan:            pricedPlan("CAP-M", 2, 4000, 20, 380000),
				BillingCapHours: 672,
				Monthly:         estimate.FromAmount(380000),
				Breakdown:       estimate.Breakdown{Plan: estimate.FromAmount(380000)},
				CountAssumed:    true,
			},
			wantMonthly:   "38.00",
			wantExact:     "38",
			wantPlanExact: "38",
		},
		{
			name:      "plan billing cap: a 730-hour month costs the whole plan price",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(730)}),
			offerings: []catalog.Offering{cappedComputeOffering(672)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:          testutil.PricedDefaultRegion,
				Estimated:       true,
				Offering:        "Example Compute Plans",
				Plan:            pricedPlan("CAP-M", 2, 4000, 20, 380000),
				BillingCapHours: 672,
				Monthly:         estimate.FromAmount(380000),
				Breakdown:       estimate.Breakdown{Plan: estimate.FromAmount(380000)},
				CountAssumed:    true,
			},
			wantMonthly:   "38.00",
			wantExact:     "38",
			wantPlanExact: "38",
		},
		{
			// 744 is more hours than the cap, and the plan is never priced above its
			// monthly price: the share stops at 1.
			name:      "plan billing cap: 744 hours never costs more than the plan price",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(744)}),
			offerings: []catalog.Offering{cappedComputeOffering(672)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:          testutil.PricedDefaultRegion,
				Estimated:       true,
				Offering:        "Example Compute Plans",
				Plan:            pricedPlan("CAP-M", 2, 4000, 20, 380000),
				BillingCapHours: 672,
				Monthly:         estimate.FromAmount(380000),
				Breakdown:       estimate.Breakdown{Plan: estimate.FromAmount(380000)},
				CountAssumed:    true,
			},
			wantMonthly:   "38.00",
			wantExact:     "38",
			wantPlanExact: "38",
		},
		{
			// Hours not given are the average month of 730 hours, which reaches the
			// cap: the whole plan price, and no hours are assumed beyond ADR 0025.
			name:      "plan billing cap: no hoursPerMonth costs the whole plan price",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings: []catalog.Offering{cappedComputeOffering(672)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:          testutil.PricedDefaultRegion,
				Estimated:       true,
				Offering:        "Example Compute Plans",
				Plan:            pricedPlan("CAP-M", 2, 4000, 20, 380000),
				BillingCapHours: 672,
				Monthly:         estimate.FromAmount(380000),
				Breakdown:       estimate.Breakdown{Plan: estimate.FromAmount(380000)},
				CountAssumed:    true,
				HoursAssumed:    true,
			},
			wantMonthly:   "38.00",
			wantExact:     "38",
			wantPlanExact: "38",
		},
		{
			// The same offering without a cap keeps the old arithmetic: the plan
			// price is the price of 730 hours, so 336 hours cost 38 × 336 / 730.
			name:      "no plan billing cap: 336 hours scale the plan price by 336/730",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(336)}),
			offerings: []catalog.Offering{cappedComputeOffering(0)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Compute Plans",
				Plan:         pricedPlan("CAP-M", 2, 4000, 20, 380000),
				Monthly:      estimate.Ratio(6384, 365),
				Breakdown:    estimate.Breakdown{Plan: estimate.Ratio(6384, 365)},
				CountAssumed: true,
			},
			wantMonthly:   "17.49",
			wantExact:     "6384/365",
			wantPlanExact: "6384/365",
		},
		{
			name: "plan billing cap: the count multiplies the capped plan price",
			resource: computeResource(&inventory.Size{
				VCPUs:     f(2),
				MemoryGiB: f(4),
				Count:     n(3),
			}, &inventory.Usage{HoursPerMonth: f(336)}),
			offerings: []catalog.Offering{cappedComputeOffering(672)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:          testutil.PricedDefaultRegion,
				Estimated:       true,
				Offering:        "Example Compute Plans",
				Plan:            pricedPlan("CAP-M", 2, 4000, 20, 380000),
				BillingCapHours: 672,
				// 38.00 × 3 × 336 / 672 = 57.00.
				Monthly:   estimate.FromAmount(570000),
				Breakdown: estimate.Breakdown{Plan: estimate.FromAmount(570000)},
			},
			wantMonthly:   "57.00",
			wantExact:     "57",
			wantPlanExact: "57",
		},
		{
			// The cap applies to plan prices only: the fixed fee stays fee × hours /
			// 730 — 9 × 336 / 730, not 9 × 336 / 672 — and storage is never scaled by
			// hours at all (ADR 0026, ADR 0029).
			name: "plan billing cap: the fixed fee keeps hours/730 and storage is unscaled",
			resource: inventory.Resource{
				ID:       "cluster-example",
				RawType:  "AWS::EKS::Cluster",
				Category: taxonomy.KubernetesManaged,
				Region:   "eu-west-1",
				Size: &inventory.Size{
					VCPUs:      f(4),
					MemoryGiB:  f(8),
					StorageGiB: f(80),
					Count:      n(3),
				},
				Usage: &inventory.Usage{HoursPerMonth: f(336)},
			},
			offerings: []catalog.Offering{kubernetesOfferingWithCap(672)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:          testutil.PricedDefaultRegion,
				Estimated:       true,
				Offering:        "Example Kubernetes",
				Plan:            pricedPlan("NK-1", 4, 8000, 50, 300000),
				BillingCapHours: 672,
				// 30 × 3 × 336/672 = 45; (80 − 50) × 0.03 × 3 = 2.7, unscaled;
				// the fee 9 × 336/730 = 1512/365, not 9 × 336/672 = 4.5.
				Monthly: estimate.Ratio(7569, 146),
				Breakdown: estimate.Breakdown{
					Plan:     estimate.FromAmount(450000),
					Storage:  estimate.Ratio(27, 10),
					FixedFee: estimate.Ratio(1512, 365),
				},
			},
			wantMonthly:   "51.84",
			wantExact:     "7569/146",
			wantPlanExact: "45",
		},
		{
			// The plan choice compares monthly list prices, not the hours-scaled
			// cost a cap gives them (ADR 0029): the capped plan at 38.00 is chosen
			// though 336 hours cost 19.00 on it and only 18.41 (40 × 336/730) on the
			// uncapped 40.00 plan.
			name:      "plan billing cap: the plan choice compares monthly list prices, not scaled costs",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(336)}),
			offerings: []catalog.Offering{cappedComputeOffering(672), uncappedComputeOffering()},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:          testutil.PricedDefaultRegion,
				Estimated:       true,
				Offering:        "Example Compute Plans",
				Plan:            pricedPlan("CAP-M", 2, 4000, 20, 380000),
				BillingCapHours: 672,
				Monthly:         estimate.FromAmount(190000),
				Breakdown:       estimate.Breakdown{Plan: estimate.FromAmount(190000)},
				CountAssumed:    true,
			},
			wantMonthly:   "19.00",
			wantExact:     "19",
			wantPlanExact: "19",
		},
		{
			// Database storage is the service's data, not one volume per node
			// (ADR 0027), so it is counted once even with a count of 3.
			name: "database.postgresql: storage once, not per node",
			resource: inventory.Resource{
				ID:       "db-example",
				RawType:  "AWS::RDS::DBInstance",
				Category: taxonomy.DatabasePostgreSQL,
				Region:   "eu-west-1",
				Size: &inventory.Size{
					VCPUs:      f(2),
					MemoryGiB:  f(4),
					StorageGiB: f(100),
					Count:      n(3),
				},
			},
			offerings: []catalog.Offering{postgresOffering()},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example PostgreSQL",
				Plan:      pricedPlan("PG-1", 2, 4000, 20, 200000),
				// 20 × 3 = 60; (100 − 20) × 0.10 = 8.
				Monthly: estimate.FromAmount(680000),
				Breakdown: estimate.Breakdown{
					Plan:    estimate.FromAmount(600000),
					Storage: estimate.FromAmount(80000),
				},
				HoursAssumed: true,
			},
			wantMonthly: "68.00",
			wantExact:   "68",
		},
		{
			name: "database.mysql: storage once, with the default count and hours",
			resource: inventory.Resource{
				ID:       "db-mysql-example",
				RawType:  "AWS::RDS::DBInstance",
				Category: taxonomy.DatabaseMySQL,
				Region:   "eu-west-1",
				Size: &inventory.Size{
					VCPUs:      f(2),
					MemoryGiB:  f(4),
					StorageGiB: f(50),
				},
			},
			offerings: []catalog.Offering{mysqlOffering(800)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example MySQL",
				Plan:      pricedPlan("MY-1", 2, 4000, 20, 180000),
				// 18 × 1; (50 − 20) × 0.08 = 2.4.
				Monthly: estimate.Ratio(102, 5),
				Breakdown: estimate.Breakdown{
					Plan:    estimate.FromAmount(180000),
					Storage: estimate.Ratio(12, 5),
				},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "20.40",
			wantExact:   "102/5",
		},
		{
			name:      "storage.object: the stored GiB at the per-GiB price",
			resource:  objectResource(&inventory.Size{StorageGiB: f(1000)}),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Object Storage",
				Monthly:   estimate.FromAmount(150000),
				Breakdown: estimate.Breakdown{Storage: estimate.FromAmount(150000)},
			},
			wantMonthly: "15.00",
			wantExact:   "15",
		},
		{
			name:      "storage.block: the volume's GiB per volume",
			resource:  blockResource(&inventory.Size{StorageGiB: f(500), Count: n(2)}),
			offerings: []catalog.Offering{blockOffering(200)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Block Storage",
				Monthly:   estimate.FromAmount(200000),
				Breakdown: estimate.Breakdown{Storage: estimate.FromAmount(200000)},
			},
			wantMonthly: "20.00",
			wantExact:   "20",
		},
		{
			name:      "storage.block: a volume with no count is one volume",
			resource:  blockResource(&inventory.Size{StorageGiB: f(100)}),
			offerings: []catalog.Offering{blockOffering(200)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Block Storage",
				Monthly:      estimate.FromAmount(20000),
				Breakdown:    estimate.Breakdown{Storage: estimate.FromAmount(20000)},
				CountAssumed: true,
			},
			wantMonthly: "2.00",
		},
		{
			// The estimate is exact; only the display rounds, and half a cent
			// goes away from zero.
			name:      "storage.block: half a cent is displayed as one cent",
			resource:  blockResource(&inventory.Size{StorageGiB: f(0.5)}),
			offerings: []catalog.Offering{blockOffering(100)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Block Storage",
				Monthly:      estimate.Ratio(1, 200),
				Breakdown:    estimate.Breakdown{Storage: estimate.Ratio(1, 200)},
				CountAssumed: true,
			},
			wantMonthly: "0.01",
			wantExact:   "1/200",
		},
		{
			// Hours that are not a multiple of 730 make a fraction no cent can
			// express: the exact amount stays a fraction, the display rounds.
			name:      "compute.vm: hours that are not a multiple of 730",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(700)}),
			offerings: []catalog.Offering{singlePlanOffering("P-1", 2, 4000, 0, 250000)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Virtual Machines",
				Plan:      pricedPlan("P-1", 2, 4000, 0, 250000),
				// 25 × 700 / 730.
				Monthly:      estimate.Ratio(1750, 73),
				Breakdown:    estimate.Breakdown{Plan: estimate.Ratio(1750, 73)},
				CountAssumed: true,
			},
			wantMonthly: "23.97",
			wantExact:   "1750/73",
		},
		{
			// The inventory's caps, multiplied out: no wrap, no rounding before
			// the display. 1,000,000 EUR × 100,000 × 744 / 730.
			name: "the largest count, hours and price do not wrap",
			resource: computeResource(&inventory.Size{
				VCPUs:     f(1),
				MemoryGiB: f(1),
				Count:     n(100000),
			}, &inventory.Usage{HoursPerMonth: f(744)}),
			offerings: []catalog.Offering{
				singlePlanOffering("XL-1", 1, 1000, 0, catalog.MaxAmount),
			},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Virtual Machines",
				Plan:      pricedPlan("XL-1", 1, 1000, 0, catalog.MaxAmount),
				Monthly:   estimate.Ratio(37200000000000, 365),
				Breakdown: estimate.Breakdown{Plan: estimate.Ratio(37200000000000, 365)},
			},
			wantMonthly: "101917808219.18",
			wantExact:   "7440000000000/73",
		},

		// The plan choice, and its tie breaks: cheapest first, then fewer vCPUs,
		// less memory, the plan name and the offering name.
		{
			name:     "plan choice: the cheapest covering plan wins, though it is larger",
			resource: computeResource(&inventory.Size{VCPUs: f(1), MemoryGiB: f(1)}, nil),
			offerings: []catalog.Offering{namedComputeOffering("Example Virtual Machines",
				plan("P-Small", 2, 4000, 0, 100000),
				plan("P-Big", 4, 8000, 0, 90000),
			)},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Virtual Machines",
				Plan:         pricedPlan("P-Big", 4, 8000, 0, 90000),
				Monthly:      estimate.FromAmount(90000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(90000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "9.00",
		},
		{
			name:     "plan choice: an equal price takes the plan with fewer vCPUs",
			resource: computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings: []catalog.Offering{namedComputeOffering("Example Virtual Machines",
				plan("P-Four", 4, 4000, 0, 100000),
				plan("P-Two", 2, 4000, 0, 100000),
			)},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Virtual Machines",
				Plan:         pricedPlan("P-Two", 2, 4000, 0, 100000),
				Monthly:      estimate.FromAmount(100000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(100000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "10.00",
		},
		{
			name:     "plan choice: equal vCPUs take the plan with less memory",
			resource: computeResource(&inventory.Size{VCPUs: f(1), MemoryGiB: f(1)}, nil),
			offerings: []catalog.Offering{namedComputeOffering("Example Virtual Machines",
				plan("P-Fat", 2, 8000, 0, 100000),
				plan("P-Lean", 2, 4000, 0, 100000),
			)},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Virtual Machines",
				Plan:         pricedPlan("P-Lean", 2, 4000, 0, 100000),
				Monthly:      estimate.FromAmount(100000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(100000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "10.00",
		},
		{
			name:     "plan choice: equal plans take the smaller plan name",
			resource: computeResource(&inventory.Size{VCPUs: f(1), MemoryGiB: f(1)}, nil),
			offerings: []catalog.Offering{namedComputeOffering("Example Virtual Machines",
				plan("P-Bravo", 2, 4000, 0, 100000),
				plan("P-Alpha", 2, 4000, 0, 100000),
			)},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Virtual Machines",
				Plan:         pricedPlan("P-Alpha", 2, 4000, 0, 100000),
				Monthly:      estimate.FromAmount(100000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(100000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "10.00",
		},
		{
			// Two offerings tie on the plan: the plan is chosen from the one
			// whose name sorts first.
			name:     "plan choice: equal plans take the smaller offering name",
			resource: computeResource(&inventory.Size{VCPUs: f(1), MemoryGiB: f(1)}, nil),
			offerings: []catalog.Offering{
				namedComputeOffering("Example Compute B", plan("P-One", 2, 4000, 0, 100000)),
				namedComputeOffering("Example Compute A", plan("P-One", 2, 4000, 0, 100000)),
			},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Compute A",
				Plan:         pricedPlan("P-One", 2, 4000, 0, 100000),
				Monthly:      estimate.FromAmount(100000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(100000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "10.00",
		},

		{
			// The best candidate offerings are those tied with the best one on fit
			// and missing capabilities: a worse fit is never priced, however cheap
			// its plan is.
			name: "plan choice: only the best offerings are priced, however cheap another is",
			resource: inventory.Resource{
				ID:           "i-0123456789abcdef0",
				RawType:      "AWS::EC2::Instance",
				Category:     taxonomy.ComputeVM,
				Region:       "eu-west-1",
				Capabilities: []taxonomy.Capability{taxonomy.IPv6, taxonomy.Snapshots},
				Size:         &inventory.Size{VCPUs: f(1), MemoryGiB: f(1)},
			},
			offerings: []catalog.Offering{
				{
					Name:         "Alpha Compute",
					Categories:   []taxonomy.Category{taxonomy.ComputeVM},
					Capabilities: []taxonomy.Capability{taxonomy.IPv6},
					Regions:      []string{testutil.PricedDefaultRegion},
				},
				{
					Name:       "Zulu Compute",
					Categories: []taxonomy.Category{taxonomy.ComputeVM},
					Regions:    []string{testutil.PricedDefaultRegion},
					Plans:      []catalog.Plan{plan("Z-1", 1, 1000, 0, 10000)},
				},
			},
			region: testutil.PricedDefaultRegion,
			want:   estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoPlan},
		},
		{
			name:     "storage.object: the lowest per-GiB price among the best offerings",
			resource: objectResource(&inventory.Size{StorageGiB: f(1000)}),
			offerings: []catalog.Offering{
				objectOffering("Example Object Storage A", 250),
				objectOffering("Example Object Storage B", 150),
			},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Object Storage B",
				Monthly:   estimate.FromAmount(150000),
				Breakdown: estimate.Breakdown{Storage: estimate.FromAmount(150000)},
			},
			wantMonthly: "15.00",
		},
		{
			name:     "storage.object: an equal price takes the smaller offering name",
			resource: objectResource(&inventory.Size{StorageGiB: f(1000)}),
			offerings: []catalog.Offering{
				objectOffering("Example Object Storage B", 150),
				objectOffering("Example Object Storage A", 150),
			},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Object Storage A",
				Monthly:   estimate.FromAmount(150000),
				Breakdown: estimate.Breakdown{Storage: estimate.FromAmount(150000)},
			},
			wantMonthly: "15.00",
		},
		{
			// A storage category ignores an offering's plans: it is priced per
			// GiB, and a plan price is never added to it.
			name:      "storage.block: the offering's plans are ignored",
			resource:  blockResource(&inventory.Size{StorageGiB: f(500)}),
			offerings: []catalog.Offering{blockOfferingWithPlan(200)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Block Storage",
				Monthly:      estimate.FromAmount(100000),
				Breakdown:    estimate.Breakdown{Storage: estimate.FromAmount(100000)},
				CountAssumed: true,
			},
			wantMonthly: "10.00",
		},

		// A size is read as the decimal it prints as, not as the nearest binary
		// float: 0.1 GiB is exactly a tenth of a GiB, so a plan of exactly 100
		// milli-GiB covers it. Read as a binary float it is a hair more, and no
		// plan of this catalog would cover it.
		{
			name:     "reading a size: 0.1 GiB fits a plan of exactly 100 milli-GiB",
			resource: computeResource(&inventory.Size{VCPUs: f(1), MemoryGiB: f(0.1)}, nil),
			offerings: []catalog.Offering{namedComputeOffering("Example Virtual Machines",
				plan("P-Tenth", 1, 100, 0, 100000),
				plan("P-Big", 4, 8000, 0, 120000),
			)},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Virtual Machines",
				Plan:         pricedPlan("P-Tenth", 1, 100, 0, 100000),
				Monthly:      estimate.FromAmount(100000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(100000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "10.00",
			wantExact:   "10",
		},
		{
			// The same reading for the stored GiB: a tenth of a GiB at 0.015 EUR
			// per GiB is 0.0015 EUR exactly, a binary reading of 0.1 would not be.
			name:      "reading a size: a tenth of a GiB costs a tenth of the per-GiB price",
			resource:  objectResource(&inventory.Size{StorageGiB: f(0.1)}),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Object Storage",
				Monthly:   estimate.Ratio(3, 2000),
				Breakdown: estimate.Breakdown{Storage: estimate.Ratio(3, 2000)},
			},
			wantMonthly: "0.00",
			wantExact:   "3/2000",
		},

		// Every "not estimated" reason, and the order that decides between them.
		{
			// NoFit comes first: the catalog serves no messaging.queue at all.
			name:      "no-fit: a category the catalog does not serve anywhere",
			resource:  queueResource(),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoFit},
		},
		{
			name: "no-fit: an unclassified resource is never estimated, whatever its size",
			resource: inventory.Resource{
				ID:       "misc-example",
				RawType:  "AWS::CloudFormation::CustomResource",
				Category: taxonomy.Unclassified,
				Region:   "eu-west-1",
				Size:     &inventory.Size{},
			},
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoFit},
		},
		{
			// UsagePriced comes before NoSize: a serverless function's memory is
			// no size an estimate could use, and a function is priced per
			// invocation anyway.
			name: "usage-priced: a serverless function with a fit",
			resource: inventory.Resource{
				ID:       "fn-example",
				RawType:  "AWS::Lambda::Function",
				Category: taxonomy.ServerlessFunction,
				Region:   "eu-west-1",
				Size:     &inventory.Size{MemoryGiB: f(2)},
			},
			offerings: []catalog.Offering{functionsOffering()},
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.UsagePriced},
		},
		{
			name:      "usage-priced: a message queue with a fit",
			resource:  queueResource(),
			offerings: []catalog.Offering{queueOffering()},
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.UsagePriced},
		},
		{
			name:      "no-size: a compute.vm resource without a size",
			resource:  computeResource(nil, nil),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a compute.vm resource with vCPUs but no memory",
			resource:  computeResource(&inventory.Size{VCPUs: f(2)}, nil),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a compute.vm resource with memory but no vCPUs",
			resource:  computeResource(&inventory.Size{MemoryGiB: f(4)}, nil),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a storage.object resource without a size",
			resource:  objectResource(nil),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a storage.block resource with a count but no stored GiB",
			resource:  blockResource(&inventory.Size{Count: n(2)}),
			offerings: []catalog.Offering{blockOffering(200)},
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			// Scaleway sells no plans, so a direct fit has nothing to price.
			name:      "no-plan: the best offerings sell no plan at all",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings: testutil.ScalewayOfferings(),
			region:    "fr-par",
			want:      estimate.Estimate{Region: "fr-par", Reason: estimate.NoPlan},
		},
		{
			// NoSize comes before NoPlan: a plan list cannot fix a missing size.
			name:      "no-size comes before no-plan",
			resource:  computeResource(nil, nil),
			offerings: testutil.ScalewayOfferings(),
			region:    "fr-par",
			want:      estimate.Estimate{Region: "fr-par", Reason: estimate.NoSize},
		},
		{
			name:      "no-plan-large-enough: no plan covers the resource",
			resource:  computeResource(&inventory.Size{VCPUs: f(8), MemoryGiB: f(8)}, nil),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Reason:    estimate.NoPlanLargeEnough,
				Offering:  "Example Virtual Machines",
				Largest:   pricedPlan("EX-L", 4, 8000, 80, 540000),
				Monthly:   estimate.Money{},
				Breakdown: estimate.Breakdown{},
			},
		},
		{
			// NoPlanLargeEnough comes before the region's prices: too large is a
			// fact about the plans, not about a region's price list, and the
			// largest plan names its price only when the region has one. The
			// offering is offered in the estimate region, so the case is about
			// the plans alone.
			name:     "no-plan-large-enough comes before the prices of the region",
			resource: computeResource(&inventory.Size{VCPUs: f(8), MemoryGiB: f(8)}, nil),
			offerings: []catalog.Offering{{
				Name:       "Example Virtual Machines",
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Regions:    []string{testutil.PricedDefaultRegion, testutil.ReportSecondRegion},
				Plans:      []catalog.Plan{plan("EX-L", 4, 8000, 80, 540000, testutil.ReportSecondRegion)},
			}},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPlanLargeEnough,
				Offering: "Example Virtual Machines",
				Largest:  unpricedPlan("EX-L", 4, 8000, 80),
			},
		},
		{
			// The vCPUs fit every plan and the memory fits none: too large is
			// decided by either number, and Largest names the biggest plan.
			name:      "no-plan-large-enough: the vCPUs fit but the memory does not",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(64)}, nil),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPlanLargeEnough,
				Offering: "Example Virtual Machines",
				Largest:  pricedPlan("EX-L", 4, 8000, 80, 540000),
			},
		},
		{
			// Largest takes the most vCPUs, then the most memory.
			name:     "no-plan-large-enough: the largest plan has the most memory of the plans with the most vCPUs",
			resource: computeResource(&inventory.Size{VCPUs: f(8), MemoryGiB: f(8)}, nil),
			offerings: []catalog.Offering{namedComputeOffering("Example Virtual Machines",
				plan("P-Small", 4, 4000, 0, 100000),
				plan("P-Fat", 4, 8000, 0, 200000),
			)},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPlanLargeEnough,
				Offering: "Example Virtual Machines",
				Largest:  pricedPlan("P-Fat", 4, 8000, 0, 200000),
			},
		},
		{
			// Then the smaller plan name.
			name:     "no-plan-large-enough: equally large plans take the smaller plan name",
			resource: computeResource(&inventory.Size{VCPUs: f(8), MemoryGiB: f(8)}, nil),
			offerings: []catalog.Offering{namedComputeOffering("Example Virtual Machines",
				plan("P-Bravo", 4, 8000, 0, 100000),
				plan("P-Alpha", 4, 8000, 0, 100000),
			)},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPlanLargeEnough,
				Offering: "Example Virtual Machines",
				Largest:  pricedPlan("P-Alpha", 4, 8000, 0, 100000),
			},
		},
		{
			// And then the smaller offering name.
			name:     "no-plan-large-enough: equally large plans take the smaller offering name",
			resource: computeResource(&inventory.Size{VCPUs: f(8), MemoryGiB: f(8)}, nil),
			offerings: []catalog.Offering{
				namedComputeOffering("Example Compute B", plan("P-One", 4, 8000, 0, 100000)),
				namedComputeOffering("Example Compute A", plan("P-One", 4, 8000, 0, 100000)),
			},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPlanLargeEnough,
				Offering: "Example Compute A",
				Largest:  pricedPlan("P-One", 4, 8000, 0, 100000),
			},
		},
		{
			// NotOffered, not NoPrice: the fixture's compute offering is sold in
			// xx-par and xx-ams only, so the region has no offering to price at
			// all (PRD #190, issue #243).
			name:      "not-offered: the offering is not available in the region",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings: testutil.PricedOfferings(),
			region:    "xx-zz",
			want:      estimate.Estimate{Region: "xx-zz", Reason: estimate.NotOffered},
		},
		{
			// NoPrice stays for the other case: the offering is offered in the
			// estimate region, and only the covering plan's price is missing there.
			name:     "no-price: the offering is offered in the region but its covering plan has no price there",
			resource: computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings: []catalog.Offering{{
				Name:       "Example Virtual Machines",
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Regions:    []string{testutil.PricedDefaultRegion, testutil.ReportSecondRegion},
				Plans:      []catalog.Plan{plan("EX-M", 2, 4500, 40, 250000, testutil.ReportSecondRegion)},
			}},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPrice,
				Offering: "Example Virtual Machines",
				Plan:     unpricedPlan("EX-M", 2, 4500, 40),
			},
		},
		{
			// A storage category whose offering is sold in another region only is
			// not offered in this one; the fixture's object offering is sold in
			// the default region alone.
			name:      "not-offered: a storage category whose offering is not in the region",
			resource:  objectResource(&inventory.Size{StorageGiB: f(1000)}),
			offerings: testutil.PricedOfferings(),
			region:    testutil.ReportSecondRegion,
			want:      estimate.Estimate{Region: testutil.ReportSecondRegion, Reason: estimate.NotOffered},
		},
		{
			// The other storage case: the offering is offered in the estimate
			// region, and only its per-GiB price is missing there.
			name:     "no-price: a storage offering is offered in the region but prices no GiB there",
			resource: objectResource(&inventory.Size{StorageGiB: f(1000)}),
			offerings: []catalog.Offering{{
				Name:          "Example Object Storage",
				Categories:    []taxonomy.Category{taxonomy.StorageObject},
				Regions:       []string{testutil.PricedDefaultRegion, testutil.ReportSecondRegion},
				StoragePrices: []catalog.Price{price(150, testutil.ReportSecondRegion)},
			}},
			region: testutil.PricedDefaultRegion,
			want:   estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoPrice},
		},
		{
			// NotOffered comes before NoStoragePrice: the offering is not offered
			// there, so there is no plan whose included storage could be exceeded.
			name:      "not-offered comes before no-storage-price",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4), StorageGiB: f(100)}, nil),
			offerings: testutil.PricedOfferings(),
			region:    "xx-zz",
			want:      estimate.Estimate{Region: "xx-zz", Reason: estimate.NotOffered},
		},
		{
			name:      "no-storage-price: the plan is priced but the offering has no per-GiB price",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4), StorageGiB: f(50)}, nil),
			offerings: []catalog.Offering{singlePlanOffering("P-1", 2, 4000, 0, 250000)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoStoragePrice,
				Offering: "Example Virtual Machines",
				Plan:     pricedPlan("P-1", 2, 4000, 0, 250000),
			},
		},
		{
			// A Kubernetes offering with a fee but no plan price in the region
			// is not estimated: a fee alone never makes an estimate.
			name: "no-price: a fixed fee alone makes no estimate",
			resource: inventory.Resource{
				ID:       "cluster-example",
				RawType:  "AWS::EKS::Cluster",
				Category: taxonomy.KubernetesManaged,
				Region:   "eu-west-1",
				Size:     &inventory.Size{VCPUs: f(2), MemoryGiB: f(4)},
			},
			offerings: []catalog.Offering{
				{
					Name:       "Example Kubernetes",
					Categories: []taxonomy.Category{taxonomy.KubernetesManaged},
					Regions:    []string{"xx-ams", "xx-par"},
					Plans:      []catalog.Plan{plan("NK-1", 4, 8000, 50, 300000, "xx-ams")},
					FixedFees:  []catalog.Price{price(90000)},
				},
			},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPrice,
				Offering: "Example Kubernetes",
				Plan:     unpricedPlan("NK-1", 4, 8000, 50),
			},
		},
		{
			// NoPlanLargeEnough comes before NotOffered too: whether a plan covers
			// the resource is a fact about the plans, not about the region the
			// offering is sold in, so an offering sold elsewhere alone is still too
			// small rather than not offered. Largest names the plan, priced in the
			// estimate region only when the region's price list has it.
			name:     "no-plan-large-enough comes before not-offered",
			resource: computeResource(&inventory.Size{VCPUs: f(8), MemoryGiB: f(8)}, nil),
			offerings: []catalog.Offering{{
				Name:       "Example Virtual Machines",
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Regions:    []string{testutil.ReportSecondRegion},
				Plans:      []catalog.Plan{plan("EX-L", 4, 8000, 80, 540000)},
			}},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPlanLargeEnough,
				Offering: "Example Virtual Machines",
				Largest:  unpricedPlan("EX-L", 4, 8000, 80),
			},
		},
		{
			// NoPlan comes before NotOffered: an offering that sells no plan has
			// nothing to price wherever it is sold, so the reason is the plan list
			// and not the region.
			name:     "no-plan comes before not-offered",
			resource: computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings: []catalog.Offering{{
				Name:       "Example Virtual Machines",
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Regions:    []string{testutil.ReportSecondRegion},
			}},
			region: testutil.PricedDefaultRegion,
			want:   estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoPlan},
		},
		{
			// NoPrice comes before NoStoragePrice: the covering plan has no price in
			// the region and the resource's storage is above what the plan includes
			// while the offering prices no GiB there, so both reasons could apply.
			// A plan that is never chosen has no included storage to exceed, and
			// no price to add storage to: the plan's missing price is the reason,
			// and the plan named is the covering one.
			name:     "no-price comes before no-storage-price",
			resource: computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4), StorageGiB: f(100)}, nil),
			offerings: []catalog.Offering{{
				Name:       "Example Virtual Machines",
				Categories: []taxonomy.Category{taxonomy.ComputeVM},
				Regions:    []string{testutil.PricedDefaultRegion, testutil.ReportSecondRegion},
				Plans:      []catalog.Plan{plan("P-1", 2, 4000, 20, 250000, testutil.ReportSecondRegion)},
			}},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPrice,
				Offering: "Example Virtual Machines",
				Plan:     unpricedPlan("P-1", 2, 4000, 20),
			},
		},
		{
			// Two best candidates, tied on fit and missing capabilities: one is sold
			// in the estimate region with a priced covering plan, the other is sold
			// elsewhere with a cheaper covering plan. The cheaper plan is never
			// chosen: only an offering sold in the region can be priced there.
			name:     "plan choice: an offering not sold in the region is not priced, however cheap its plan is",
			resource: computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings: []catalog.Offering{
				{
					Name:       "Example Virtual Machines",
					Categories: []taxonomy.Category{taxonomy.ComputeVM},
					Regions:    []string{testutil.PricedDefaultRegion},
					Plans:      []catalog.Plan{plan("X-M", 2, 4000, 40, 300000)},
				},
				{
					Name:       "Example Compute Elsewhere",
					Categories: []taxonomy.Category{taxonomy.ComputeVM},
					Regions:    []string{testutil.ReportSecondRegion},
					Plans:      []catalog.Plan{plan("Y-S", 2, 4000, 20, 100000)},
				},
			},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Virtual Machines",
				Plan:         pricedPlan("X-M", 2, 4000, 40, 300000),
				Monthly:      estimate.FromAmount(300000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(300000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "30.00",
		},
		{
			// The same two best candidates, the other way round: the offering sold
			// in the region offers a larger covering plan that has no price there,
			// the one sold elsewhere a smaller one. The reason is NoPrice, not
			// NotOffered, and the plan it names is the larger plan of the offering
			// that is sold there, not the smaller plan of an offering that is not.
			name:     "no-price: named on the covering plan of the offering sold in the region",
			resource: computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings: []catalog.Offering{
				{
					Name:       "Example Virtual Machines",
					Categories: []taxonomy.Category{taxonomy.ComputeVM},
					Regions:    []string{testutil.PricedDefaultRegion, testutil.ReportSecondRegion},
					Plans:      []catalog.Plan{plan("X-L", 4, 8000, 80, 400000, testutil.ReportSecondRegion)},
				},
				{
					Name:       "Example Compute Elsewhere",
					Categories: []taxonomy.Category{taxonomy.ComputeVM},
					Regions:    []string{testutil.ReportSecondRegion},
					Plans:      []catalog.Plan{plan("Y-S", 2, 4000, 20, 100000)},
				},
			},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPrice,
				Offering: "Example Virtual Machines",
				Plan:     unpricedPlan("X-L", 4, 8000, 80),
			},
		},

		// A resource built without inventory.New can carry a number the validation
		// would have refused. A size or usage that is not a positive finite number
		// is no size at all: such a resource is never estimated with 0 EUR, and
		// never with a negative amount.
		{
			name:      "no-size: a hand-built compute.vm with NaN vCPUs",
			resource:  computeResource(&inventory.Size{VCPUs: f(math.NaN()), MemoryGiB: f(4)}, nil),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built compute.vm with infinite vCPUs",
			resource:  computeResource(&inventory.Size{VCPUs: f(math.Inf(1)), MemoryGiB: f(4)}, nil),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built compute.vm with negative vCPUs",
			resource:  computeResource(&inventory.Size{VCPUs: f(-2), MemoryGiB: f(4)}, nil),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built compute.vm with NaN memory",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(math.NaN())}, nil),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built compute.vm with infinite memory",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(math.Inf(1))}, nil),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built compute.vm with negative memory",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(-4)}, nil),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built compute.vm with NaN hours",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(math.NaN())}),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built compute.vm with infinite hours",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(math.Inf(1))}),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built compute.vm with minus infinite hours",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(math.Inf(-1))}),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built compute.vm with negative hours",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(-1)}),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built compute.vm with a negative count",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4), Count: n(-3)}, nil),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built storage.object with NaN stored GiB",
			resource:  objectResource(&inventory.Size{StorageGiB: f(math.NaN())}),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built storage.object with infinite stored GiB",
			resource:  objectResource(&inventory.Size{StorageGiB: f(math.Inf(1))}),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:      "no-size: a hand-built storage.object with negative stored GiB",
			resource:  objectResource(&inventory.Size{StorageGiB: f(-100)}),
			handBuilt: true,
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},

		// A number is read to six fractional digits, rounded half away from zero,
		// before the exact arithmetic, so no float can make the fractions grow
		// (ADR 0027).
		{
			name:      "reading a number: 5e-324 hours are read as no hours at all",
			resource:  computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(5e-324)}),
			offerings: []catalog.Offering{singlePlanOffering("P-1", 2, 4000, 0, 250000)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Virtual Machines",
				Plan:         pricedPlan("P-1", 2, 4000, 0, 250000),
				CountAssumed: true,
			},
			wantMonthly:   "0.00",
			wantExact:     "0",
			wantPlanExact: "0",
		},
		{
			name:      "reading a number: stored GiB beyond the sixth decimal are dropped",
			resource:  objectResource(&inventory.Size{StorageGiB: f(100.0000004)}),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Object Storage",
				// 100 GiB × 0.015 EUR, not 100.0000004 GiB × 0.015 EUR.
				Monthly:   estimate.Ratio(3, 2),
				Breakdown: estimate.Breakdown{Storage: estimate.Ratio(3, 2)},
			},
			wantMonthly:   "1.50",
			wantExact:     "3/2",
			wantPlanExact: "0",
		},

		// PRD #552's categories (ADR 0046). A cache is matched on memory alone,
		// a MongoDB database as a PostgreSQL one, a file share by the GiB, and
		// the rest is not priced or priced by usage.
		{
			// The resource gives 8 GiB and two nodes: CACHE-8 and CACHE-16 cover
			// it, and the cheaper one wins. The offering prices storage, which a
			// cache estimate never adds and never notes.
			name:     "cache.redis: the cheapest covering plan, per node",
			resource: cacheResource(&inventory.Size{MemoryGiB: f(8), Count: n(2)}, nil),
			offerings: []catalog.Offering{cacheOffering("Example Cache",
				cachePlan("CACHE-4", 0, 4, 100000),
				cachePlan("CACHE-8", 0, 8, 200000),
				cachePlan("CACHE-16", 0, 16, 350000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Cache",
				Plan:         pricedPlan("CACHE-8", 0, 8000, 0, 200000),
				Monthly:      estimate.FromAmount(400000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(400000)},
				HoursAssumed: true,
			},
			wantMonthly: "40.00",
			wantExact:   "40",
		},
		{
			// vCPUs take no part in matching a cache: the cheaper plan has one
			// vCPU and more memory, the dearer one two vCPUs and too little.
			name:     "cache.redis: a plan with given vCPUs is still matched on memory alone",
			resource: cacheResource(&inventory.Size{MemoryGiB: f(8)}, nil),
			offerings: []catalog.Offering{cacheOffering("Example Cache",
				cachePlan("CACHE-16-1CPU", 1, 16, 200000),
				cachePlan("CACHE-8-2CPU", 2, 8, 250000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Cache",
				Plan:         pricedPlan("CACHE-16-1CPU", 1, 16000, 0, 200000),
				Monthly:      estimate.FromAmount(200000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(200000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "20.00",
			wantExact:   "20",
		},
		{
			// Two covering plans at the same price: the one with less memory wins.
			name:     "cache.redis: a price tie goes to the plan with less memory",
			resource: cacheResource(&inventory.Size{MemoryGiB: f(8)}, nil),
			offerings: []catalog.Offering{cacheOffering("Example Cache",
				cachePlan("CACHE-16", 0, 16, 200000),
				cachePlan("CACHE-8", 0, 8, 200000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Cache",
				Plan:         pricedPlan("CACHE-8", 0, 8000, 0, 200000),
				Monthly:      estimate.FromAmount(200000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(200000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "20.00",
		},
		{
			// A tie of equal price is broken by memory alone, not by vCPUs: the
			// plan with less memory wins though it has more vCPUs, and though the
			// other has fewer.
			name:     "cache.redis: a price tie ignores vCPUs and goes to the plan with less memory",
			resource: cacheResource(&inventory.Size{MemoryGiB: f(8)}, nil),
			offerings: []catalog.Offering{cacheOffering("Example Cache",
				cachePlan("CACHE-16", 1, 16, 200000),
				cachePlan("CACHE-8", 4, 8, 200000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Cache",
				Plan:         pricedPlan("CACHE-8", 4, 8000, 0, 200000),
				Monthly:      estimate.FromAmount(200000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(200000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "20.00",
		},
		{
			// Hours scale a cache's plan price as they scale any plan price: half
			// a month of 730 hours costs half the per-node monthly price of each
			// of the two nodes.
			name: "cache.redis: usage hoursPerMonth scale the per-node plan price",
			resource: cacheResource(&inventory.Size{MemoryGiB: f(8), Count: n(2)},
				&inventory.Usage{HoursPerMonth: f(365)}),
			offerings: []catalog.Offering{cacheOffering("Example Cache",
				cachePlan("CACHE-8", 0, 8, 200000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Cache",
				Plan:      pricedPlan("CACHE-8", 0, 8000, 0, 200000),
				// 20.00 × 365/730 × 2 nodes = 20.00.
				Monthly:   estimate.FromAmount(200000),
				Breakdown: estimate.Breakdown{Plan: estimate.FromAmount(200000)},
			},
			wantMonthly:   "20.00",
			wantExact:     "20",
			wantPlanExact: "20",
		},
		{
			// With memory and price equal, the plan name decides before the
			// offering name: plan AAA of the offering that sorts last wins.
			name:     "cache.redis: a tie of memory and price goes to the plan name",
			resource: cacheResource(&inventory.Size{MemoryGiB: f(8)}, nil),
			offerings: []catalog.Offering{
				cacheOffering("Zulu Cache", cachePlan("AAA", 0, 8, 200000)),
				cacheOffering("Alpha Cache", cachePlan("ZZZ", 0, 8, 200000)),
			},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Zulu Cache",
				Plan:         pricedPlan("AAA", 0, 8000, 0, 200000),
				Monthly:      estimate.FromAmount(200000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(200000)},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "20.00",
		},
		{
			name:     "cache.redis: no memory is no size",
			resource: cacheResource(&inventory.Size{Count: n(2)}, nil),
			offerings: []catalog.Offering{cacheOffering("Example Cache",
				cachePlan("CACHE-8", 0, 8, 200000))},
			region: testutil.PricedDefaultRegion,
			want:   estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:     "cache.redis: no plan large enough names the plan with most memory",
			resource: cacheResource(&inventory.Size{MemoryGiB: f(32)}, nil),
			offerings: []catalog.Offering{cacheOffering("Example Cache",
				cachePlan("CACHE-4", 0, 4, 100000),
				cachePlan("CACHE-8", 0, 8, 200000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPlanLargeEnough,
				Offering: "Example Cache",
				Largest:  pricedPlan("CACHE-8", 0, 8000, 0, 200000),
			},
		},
		{
			// The plan named as the largest is the one with most memory, not the
			// one with most vCPUs: a cache is matched and ordered on memory alone.
			name:     "cache.redis: no plan large enough names the plan with most memory, not most vCPUs",
			resource: cacheResource(&inventory.Size{MemoryGiB: f(32)}, nil),
			offerings: []catalog.Offering{cacheOffering("Example Cache",
				cachePlan("A", 8, 4, 300000),
				cachePlan("B", 1, 8, 200000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPlanLargeEnough,
				Offering: "Example Cache",
				Largest:  pricedPlan("B", 1, 8000, 0, 200000),
			},
		},
		{
			// No covering plan is priced in the region: the plan named is the first
			// in the plan order, by less memory, plan name and offering name, and
			// its vCPUs take no part (ADR 0046).
			name:     "cache.redis: no price names the covering plan with less memory",
			resource: cacheResource(&inventory.Size{MemoryGiB: f(8)}, nil),
			offerings: []catalog.Offering{{
				Name:       "Example Cache",
				Categories: []taxonomy.Category{taxonomy.CacheRedis},
				Regions:    []string{testutil.PricedDefaultRegion, testutil.ReportSecondRegion},
				Plans: []catalog.Plan{
					plan("CACHE-8", 4, 8000, 0, 200000, testutil.ReportSecondRegion),
					plan("CACHE-16", 1, 16000, 0, 300000, testutil.ReportSecondRegion),
				},
			}},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:   testutil.PricedDefaultRegion,
				Reason:   estimate.NoPrice,
				Offering: "Example Cache",
				Plan:     unpricedPlan("CACHE-8", 4, 8000, 0),
			},
		},
		{
			// A MongoDB database is priced as a PostgreSQL one: the plan per node,
			// and the service's storage above what the plan includes, once.
			name: "database.mongodb: a plan per node and the storage above it",
			resource: mongoResource(&inventory.Size{
				VCPUs:      f(2),
				MemoryGiB:  f(4),
				StorageGiB: f(120),
				Count:      n(2),
			}, nil),
			offerings: []catalog.Offering{mongoOffering()},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example MongoDB",
				Plan:         pricedPlan("MO-1", 2, 4000, 20, 200000),
				Monthly:      estimate.FromAmount(500000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(400000), Storage: estimate.FromAmount(100000)},
				HoursAssumed: true,
			},
			wantMonthly:   "50.00",
			wantExact:     "50",
			wantPlanExact: "40",
		},
		// PRD #571's two categories are plan-priced as a database is, but their
		// storage is per node: a search cluster and a Kafka broker set split their
		// data across their nodes, and each source sizes storage per node (PRD #571,
		// Estimates).
		{
			name: "search.opensearch: the cheapest covering plan per data node, and storage per node",
			resource: searchResource(&inventory.Size{
				VCPUs:      f(2),
				MemoryGiB:  f(8),
				StorageGiB: f(200),
				Count:      n(3),
			}, &inventory.Usage{HoursPerMonth: f(365)}),
			offerings: []catalog.Offering{searchOffering("Example Search",
				nodePlan("SEARCH-4x16", 4, 16, 120, 500000),
				nodePlan("SEARCH-2x8", 2, 8, 60, 300000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Search",
				Plan:      pricedPlan("SEARCH-2x8", 2, 8000, 60, 300000),
				// 30 × 3 × 365/730 = 45; (200 − 60) × 0.10 × 3 = 42.
				Monthly: estimate.FromAmount(870000),
				Breakdown: estimate.Breakdown{
					Plan:    estimate.FromAmount(450000),
					Storage: estimate.FromAmount(420000),
				},
			},
			wantMonthly:   "87.00",
			wantExact:     "87",
			wantPlanExact: "45",
		},
		{
			// The plans are given out of price order, so the cheapest covering one
			// wins rather than the first.
			name:     "messaging.kafka: the cheapest covering broker plan, per broker",
			resource: kafkaResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(8), Count: n(3)}, nil),
			offerings: []catalog.Offering{kafkaOffering("Example Kafka",
				nodePlan("KAFKA-4x16", 4, 16, 120, 350000),
				nodePlan("KAFKA-2x8", 2, 8, 60, 200000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:       testutil.PricedDefaultRegion,
				Estimated:    true,
				Offering:     "Example Kafka",
				Plan:         pricedPlan("KAFKA-2x8", 2, 8000, 60, 200000),
				Monthly:      estimate.FromAmount(600000),
				Breakdown:    estimate.Breakdown{Plan: estimate.FromAmount(600000)},
				HoursAssumed: true,
			},
			wantMonthly: "60.00",
			wantExact:   "60",
		},
		{
			// No count: one node, and the estimate says the count was assumed.
			name: "search.opensearch: no count is one data node, with the count assumed",
			resource: searchResource(&inventory.Size{
				VCPUs:      f(2),
				MemoryGiB:  f(8),
				StorageGiB: f(200),
			}, nil),
			offerings: []catalog.Offering{searchOffering("Example Search",
				nodePlan("SEARCH-2x8", 2, 8, 60, 300000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Search",
				Plan:      pricedPlan("SEARCH-2x8", 2, 8000, 60, 300000),
				// 30 × 1; (200 − 60) × 0.10 × 1 = 14.
				Monthly: estimate.FromAmount(440000),
				Breakdown: estimate.Breakdown{
					Plan:    estimate.FromAmount(300000),
					Storage: estimate.FromAmount(140000),
				},
				CountAssumed: true,
				HoursAssumed: true,
			},
			wantMonthly: "44.00",
			wantExact:   "44",
		},
		{
			// A plan is matched on vCPUs and memory together, so either alone is no
			// size, whatever the storage and the count say.
			name: "search.opensearch: no vcpus is no size",
			resource: searchResource(&inventory.Size{
				MemoryGiB:  f(8),
				StorageGiB: f(200),
				Count:      n(3),
			}, nil),
			offerings: []catalog.Offering{searchOffering("Example Search",
				nodePlan("SEARCH-2x8", 2, 8, 60, 300000))},
			region: testutil.PricedDefaultRegion,
			want:   estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			name:     "messaging.kafka: no memory is no size",
			resource: kafkaResource(&inventory.Size{VCPUs: f(2), Count: n(3)}, nil),
			offerings: []catalog.Offering{kafkaOffering("Example Kafka",
				nodePlan("KAFKA-2x8", 2, 8, 60, 200000))},
			region: testutil.PricedDefaultRegion,
			want:   estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoSize},
		},
		{
			// The same size as database.postgresql's "storage once, not per node"
			// case above: there the 80 GiB above what the plan includes are 8.00 for
			// the service, here they are 8.00 for each of the cluster's 3 nodes.
			name: "search.opensearch: a database's size, with its storage per node",
			resource: searchResource(&inventory.Size{
				VCPUs:      f(2),
				MemoryGiB:  f(4),
				StorageGiB: f(100),
				Count:      n(3),
			}, nil),
			offerings: []catalog.Offering{searchOffering("Example Search",
				nodePlan("SEARCH-1", 2, 4, 20, 200000))},
			region: testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example Search",
				Plan:      pricedPlan("SEARCH-1", 2, 4000, 20, 200000),
				// 20 × 3 = 60; (100 − 20) × 0.10 × 3 = 24.
				Monthly: estimate.FromAmount(840000),
				Breakdown: estimate.Breakdown{
					Plan:    estimate.FromAmount(600000),
					Storage: estimate.FromAmount(240000),
				},
				HoursAssumed: true,
			},
			wantMonthly:   "84.00",
			wantExact:     "84",
			wantPlanExact: "60",
		},
		{
			name:      "storage.file: the provisioned GiB, once",
			resource:  fileResource(&inventory.Size{StorageGiB: f(500)}),
			offerings: []catalog.Offering{fileOffering(200)},
			region:    testutil.PricedDefaultRegion,
			want: estimate.Estimate{
				Region:    testutil.PricedDefaultRegion,
				Estimated: true,
				Offering:  "Example File Storage",
				Monthly:   estimate.FromAmount(100000),
				Breakdown: estimate.Breakdown{Storage: estimate.FromAmount(100000)},
			},
			wantMonthly: "10.00",
			wantExact:   "10",
		},
		{
			name:      "usage-priced: a messaging.topic with a fit",
			resource:  topicResource(),
			offerings: []catalog.Offering{topicOffering()},
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.UsagePriced},
		},
		{
			name:      "usage-priced: a serverless.container with a fit",
			resource:  containerResource(),
			offerings: []catalog.Offering{containerOffering()},
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.UsagePriced},
		},
		{
			// A load balancer is billed a flat fee per size, and the size is chosen
			// by bandwidth or connections, which no inventory field records: it is
			// not priced, not usage-priced (ADR 0046).
			name:      "not-priced: a network.loadbalancer with a fit",
			resource:  loadBalancerResource(),
			offerings: []catalog.Offering{loadBalancerOffering()},
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NeverPriced},
		},
		{
			// A registry sells plans with included storage on one target and prices
			// stored GiB on another: one method would misprice one of them.
			name:      "not-priced: a registry.container with a fit",
			resource:  registryResource(),
			offerings: []catalog.Offering{registryOffering()},
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NeverPriced},
		},
		{
			// NoFit still comes first, whatever a category is priced by.
			name:      "no-fit: a cache the catalog does not serve anywhere",
			resource:  cacheResource(&inventory.Size{MemoryGiB: f(8)}, nil),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoFit},
		},
		{
			name:      "no-fit: a load balancer the catalog does not serve anywhere",
			resource:  loadBalancerResource(),
			offerings: testutil.PricedOfferings(),
			region:    testutil.PricedDefaultRegion,
			want:      estimate.Estimate{Region: testutil.PricedDefaultRegion, Reason: estimate.NoFit},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resource := tc.resource
			if !tc.handBuilt {
				resource = newResource(t, tc.resource)
			}

			cat := newCatalog(t, tc.offerings)

			got := estimate.For(mapping.Map(resource, cat), tc.region)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("For() mismatch (-want +got):\n%s", diff)
			}

			if tc.wantMonthly != "" {
				if got := got.Monthly.String(); got != tc.wantMonthly {
					t.Errorf("Monthly.String() = %q, want %q", got, tc.wantMonthly)
				}
			}

			if tc.wantExact != "" {
				if got := got.Monthly.Exact(); got != tc.wantExact {
					t.Errorf("Monthly.Exact() = %q, want %q", got, tc.wantExact)
				}
			}

			if tc.wantPlanExact != "" {
				if got := got.Breakdown.Plan.Exact(); got != tc.wantPlanExact {
					t.Errorf("Breakdown.Plan.Exact() = %q, want %q", got, tc.wantPlanExact)
				}
			}
		})
	}
}

// TestForBillingCapHours pins the hours a month an estimate records when the
// chosen plan's offering caps them (ADR 0029): the cap when the resource is
// estimated by plan on a capped offering, whatever the resource's own hours,
// and none otherwise — on an uncapped offering, for a resource priced by
// storage, and for a resource that is not estimated at all.
func TestForBillingCapHours(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// resource, offerings and region are the case's input, as in TestFor.
		resource  inventory.Resource
		offerings []catalog.Offering
		// wantCap is the BillingCapHours the estimate must record.
		wantCap int64
		// wantReason is the reason of an estimate the case expects not to be
		// estimated.
		wantReason estimate.Reason
		// wantMonthly is the estimate's monthly price, empty when it is not
		// estimated.
		wantMonthly string
	}{
		{
			// 38.00 × min(730, 672) / 672 = 38.00: the plan price is the price of
			// the capped hours, so a 730-hour month costs the whole of it.
			name:        "730 hours on a capped offering cost the whole plan price, and the cap is recorded",
			resource:    computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(730)}),
			offerings:   []catalog.Offering{cappedComputeOffering(672)},
			wantCap:     672,
			wantMonthly: "38.00",
		},
		{
			// 38.00 × min(336, 672) / 672 = 19.00: half the cap costs half the plan
			// price, and the estimate still records the cap it was scaled by.
			name:        "336 hours on a capped offering cost half the plan price, and the cap is recorded",
			resource:    computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(336)}),
			offerings:   []catalog.Offering{cappedComputeOffering(672)},
			wantCap:     672,
			wantMonthly: "19.00",
		},
		{
			// 38.00 × 336 / 730: without a cap the plan price stands for 730 hours,
			// and the estimate records no cap.
			name:        "an offering without a cap records none",
			resource:    computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, &inventory.Usage{HoursPerMonth: f(336)}),
			offerings:   []catalog.Offering{cappedComputeOffering(0)},
			wantMonthly: "17.49",
		},
		{
			// 1000 GiB × 0.015 EUR: a cap is a property of an offering's plan prices,
			// so a resource priced by storage is not capped and records no cap.
			name:        "an offering's cap does not reach a resource priced by storage",
			resource:    objectResource(&inventory.Size{StorageGiB: f(1000)}),
			offerings:   []catalog.Offering{cappedStorageOffering(672)},
			wantMonthly: "15.00",
		},
		{
			// The chosen plan includes 20 GiB and the offering charges no per-GiB
			// storage price, so the resource is not estimated, and records no cap.
			name: "a resource that is not estimated records no cap",
			resource: computeResource(
				&inventory.Size{VCPUs: f(2), MemoryGiB: f(4), StorageGiB: f(100)}, nil),
			offerings:  []catalog.Offering{cappedComputeOffering(672)},
			wantReason: estimate.NoStoragePrice,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cat := newCatalog(t, tc.offerings)
			got := estimate.For(mapping.Map(newResource(t, tc.resource), cat), testutil.PricedDefaultRegion)

			if got.BillingCapHours != tc.wantCap {
				t.Errorf("BillingCapHours = %d, want %d", got.BillingCapHours, tc.wantCap)
			}

			if got.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}

			if tc.wantMonthly != "" {
				if gotMonths := got.Monthly.String(); gotMonths != tc.wantMonthly {
					t.Errorf("Monthly.String() = %q, want %q", gotMonths, tc.wantMonthly)
				}
			}
		})
	}
}

// namedComputeOffering is a compute.vm offering with that name, sold in the
// default region, with the given plans.
func namedComputeOffering(name string, plans ...catalog.Plan) catalog.Offering {
	return catalog.Offering{
		Name:       name,
		Categories: []taxonomy.Category{taxonomy.ComputeVM},
		Regions:    []string{testutil.PricedDefaultRegion},
		Plans:      plans,
	}
}

// TestForStorageNotIncluded pins the note an estimate by plan carries when it
// leaves out storage its plan does not include (ADR 0032): the chosen plan
// includes no storage, the offering prices storage per GiB in the estimate
// region and the resource gives no storage size. The note names what is left out
// by category - a boot volume for compute.vm, compute.baremetal and
// kubernetes.managed, the data storage for database.postgresql and
// database.mysql - and the amount, the breakdown and the totals do not change.
func TestForStorageNotIncluded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// resource, offerings and region are the case's input, as in TestFor.
		resource inventory.Resource
		// handBuilt builds the resource without inventory.New, as only a caller
		// that skips the validation can: the validation refuses a count below 1.
		handBuilt bool
		offerings []catalog.Offering
		region    string
		// want is the note the estimate must carry, "" for none.
		want estimate.StorageNotIncluded
		// wantEstimated is whether the case expects an estimate at all.
		wantEstimated bool
		// wantReason is the reason of a resource the case expects not to be
		// estimated.
		wantReason estimate.Reason
		// wantMonthly is the estimated amount, which the note never changes:
		// empty when the case expects no estimate.
		wantMonthly string
	}{
		{
			// A Gen 3 instance boots from a Block Storage volume its plan does not
			// include, so an estimate for a VM that gives no storage size is the
			// plan price alone and says which storage it leaves out.
			name:          "compute.vm: a plan with no included storage and a resource with no size",
			resource:      computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings:     []catalog.Offering{bootVolumeComputeOffering()},
			region:        testutil.PricedDefaultRegion,
			want:          estimate.BootVolume,
			wantEstimated: true,
			wantMonthly:   "20.00",
		},
		{
			// A resource that gives storageGiB 0 said it needs none beyond the
			// plan, so its estimate leaves nothing out.
			name: "compute.vm: an explicit storage size of 0 gets no note",
			resource: computeResource(&inventory.Size{
				VCPUs: f(2), MemoryGiB: f(4), StorageGiB: f(0),
			}, nil),
			offerings:     []catalog.Offering{bootVolumeComputeOffering()},
			region:        testutil.PricedDefaultRegion,
			wantEstimated: true,
			wantMonthly:   "20.00",
		},
		{
			// A resource with count 0 leaves nothing out, because it is nothing
			// to estimate: its estimate is 0.00, so no storage is left out of it.
			name: "compute.vm: a count of 0 gets no note",
			resource: computeResource(&inventory.Size{
				VCPUs: f(2), MemoryGiB: f(4), Count: n(0),
			}, nil),
			handBuilt:     true,
			offerings:     []catalog.Offering{bootVolumeComputeOffering()},
			region:        testutil.PricedDefaultRegion,
			wantEstimated: true,
			wantMonthly:   "0.00",
		},
		{
			// A storage size the resource gives is priced above what the plan
			// includes, so nothing is left out: 20.00 + 100 GiB × 0.10.
			name: "compute.vm: a storage size the resource gives gets no note",
			resource: computeResource(&inventory.Size{
				VCPUs: f(2), MemoryGiB: f(4), StorageGiB: f(100),
			}, nil),
			offerings:     []catalog.Offering{bootVolumeComputeOffering()},
			region:        testutil.PricedDefaultRegion,
			wantEstimated: true,
			wantMonthly:   "30.00",
		},
		{
			// A plan that includes storage needs no volume to boot: a resource
			// that gives no size needs no storage above what the plan includes.
			name:          "compute.vm: a plan that includes storage gets no note without a size",
			resource:      computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings:     []catalog.Offering{includedStorageComputeOffering()},
			region:        testutil.PricedDefaultRegion,
			wantEstimated: true,
			wantMonthly:   "20.00",
		},
		{
			// The size above what a plan includes is priced as before, and the
			// plan includes a disk, so the estimate leaves nothing out: 20.00 +
			// (100 − 20) GiB × 0.10.
			name: "compute.vm: a plan that includes storage gets no note with a size",
			resource: computeResource(&inventory.Size{
				VCPUs: f(2), MemoryGiB: f(4), StorageGiB: f(100),
			}, nil),
			offerings:     []catalog.Offering{includedStorageComputeOffering()},
			region:        testutil.PricedDefaultRegion,
			wantEstimated: true,
			wantMonthly:   "28.00",
		},
		{
			// Nothing the estimate could charge a boot volume at: the offering
			// prices no storage, so the note would name a part the estimate has
			// no price for.
			name:          "compute.vm: an offering with no storage price at all gets no note",
			resource:      computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings:     []catalog.Offering{zeroStorageUnpricedComputeOffering()},
			region:        testutil.PricedDefaultRegion,
			wantEstimated: true,
			wantMonthly:   "20.00",
		},
		{
			// The offering prices storage, in another region: the estimate region
			// has no price to charge a boot volume at, so there is no note.
			name:          "compute.vm: an offering with no storage price in the region gets no note",
			resource:      computeResource(&inventory.Size{VCPUs: f(2), MemoryGiB: f(4)}, nil),
			offerings:     []catalog.Offering{zeroStorageElsewhereComputeOffering()},
			region:        testutil.PricedDefaultRegion,
			wantEstimated: true,
			wantMonthly:   "20.00",
		},
		{
			name: "compute.baremetal: a plan with no included storage leaves out the boot volume",
			resource: inventory.Resource{
				ID:       "i-0123456789abcdef0",
				RawType:  "AWS::EC2::Instance",
				Category: taxonomy.ComputeBareMetal,
				Region:   "eu-west-1",
				Size:     &inventory.Size{VCPUs: f(8), MemoryGiB: f(16)},
			},
			offerings:     []catalog.Offering{bootVolumeBareMetalOffering()},
			region:        testutil.PricedDefaultRegion,
			want:          estimate.BootVolume,
			wantEstimated: true,
			wantMonthly:   "48.00",
		},
		{
			name: "kubernetes.managed: a node plan with no included storage leaves out the boot volume",
			resource: inventory.Resource{
				ID:       "cluster-example",
				RawType:  "AWS::EKS::Cluster",
				Category: taxonomy.KubernetesManaged,
				Region:   "eu-west-1",
				Size:     &inventory.Size{VCPUs: f(2), MemoryGiB: f(4)},
			},
			offerings:     []catalog.Offering{bootVolumeKubernetesOffering()},
			region:        testutil.PricedDefaultRegion,
			want:          estimate.BootVolume,
			wantEstimated: true,
			wantMonthly:   "30.00",
		},
		{
			name: "database.postgresql: a plan with no included storage leaves out the data storage",
			resource: inventory.Resource{
				ID:       "db-example",
				RawType:  "AWS::RDS::DBInstance",
				Category: taxonomy.DatabasePostgreSQL,
				Region:   "eu-west-1",
				Size:     &inventory.Size{VCPUs: f(2), MemoryGiB: f(4)},
			},
			offerings:     []catalog.Offering{dataStoragePostgresOffering()},
			region:        testutil.PricedDefaultRegion,
			want:          estimate.DataStorage,
			wantEstimated: true,
			wantMonthly:   "20.00",
		},
		{
			name: "database.mysql: a plan with no included storage leaves out the data storage",
			resource: inventory.Resource{
				ID:       "db-mysql-example",
				RawType:  "AWS::RDS::DBInstance",
				Category: taxonomy.DatabaseMySQL,
				Region:   "eu-west-1",
				Size:     &inventory.Size{VCPUs: f(2), MemoryGiB: f(4)},
			},
			offerings:     []catalog.Offering{dataStorageMySQLOffering()},
			region:        testutil.PricedDefaultRegion,
			want:          estimate.DataStorage,
			wantEstimated: true,
			wantMonthly:   "18.00",
		},
		{
			// A resource priced by storage has no plan whose included storage
			// could be short: it is the stored GiB itself, so it carries no note.
			name:          "storage.block: a resource priced by storage gets no note",
			resource:      blockResource(&inventory.Size{StorageGiB: f(500)}),
			offerings:     []catalog.Offering{blockOffering(1000)},
			region:        testutil.PricedDefaultRegion,
			wantEstimated: true,
			wantMonthly:   "50.00",
		},
		{
			// The note is set only on an estimate: a resource that gives no size
			// is not estimated, so it has nothing to say about storage either.
			name:       "a resource that is not estimated, for want of a size, gets no note",
			resource:   computeResource(&inventory.Size{VCPUs: f(2)}, nil),
			offerings:  []catalog.Offering{bootVolumeComputeOffering()},
			region:     testutil.PricedDefaultRegion,
			wantReason: estimate.NoSize,
		},
		{
			name:       "a resource that is not estimated, for want of a fit, gets no note",
			resource:   queueResource(),
			offerings:  testutil.PricedOfferings(),
			region:     testutil.PricedDefaultRegion,
			wantReason: estimate.NoFit,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resource := tc.resource
			if !tc.handBuilt {
				resource = newResource(t, tc.resource)
			}

			cat := newCatalog(t, tc.offerings)
			got := estimate.For(mapping.Map(resource, cat), tc.region)

			if got.Estimated != tc.wantEstimated {
				t.Errorf("Estimated = %t, want %t (reason %q)", got.Estimated, tc.wantEstimated, got.Reason)
			}

			if got.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}

			if got.StorageNotIncluded != tc.want {
				t.Errorf("StorageNotIncluded = %q, want %q", got.StorageNotIncluded, tc.want)
			}

			if tc.wantMonthly != "" {
				if gotMonthly := got.Monthly.String(); gotMonthly != tc.wantMonthly {
					t.Errorf("Monthly.String() = %q, want %q", gotMonthly, tc.wantMonthly)
				}
			}
		})
	}
}

// cappedComputeOffering sells one compute.vm plan at 38.00 EUR a month and bills
// its plans for at most capHours hours a month (ADR 0029); 0 means uncapped.
func cappedComputeOffering(capHours int64) catalog.Offering {
	o := namedComputeOffering("Example Compute Plans", plan("CAP-M", 2, 4000, 20, 380000))
	o.PlanBillingCapHours = capHours

	return o
}

// cappedStorageOffering sells storage.object at 0.015 EUR a GiB and bills its
// plans for at most capHours hours a month (ADR 0029): the cap must not reach a
// resource priced by storage, which has no plan price to scale.
func cappedStorageOffering(capHours int64) catalog.Offering {
	return catalog.Offering{
		Name:                "Example Capped Object Storage",
		Categories:          []taxonomy.Category{taxonomy.StorageObject},
		Regions:             []string{testutil.PricedDefaultRegion},
		StoragePrices:       []catalog.Price{price(150)},
		PlanBillingCapHours: capHours,
	}
}

// uncappedComputeOffering sells one compute.vm plan at 40.00 EUR a month and
// bills it without a cap (ADR 0029): its plan price is the price of 730 hours.
func uncappedComputeOffering() catalog.Offering {
	return namedComputeOffering("Example Uncapped Compute Plans", plan("UNC-M", 2, 4000, 20, 400000))
}

// kubernetesOfferingWithCap is kubernetesOffering with its node plans billed for
// at most capHours hours a month (ADR 0029); 0 means uncapped.
func kubernetesOfferingWithCap(capHours int64) catalog.Offering {
	o := kubernetesOffering()
	o.PlanBillingCapHours = capHours

	return o
}

// singlePlanOffering is a compute.vm offering sold in the default region with one
// plan and no storage price.
func singlePlanOffering(name string, vcpus, memoryMilliGiB, includedStorageGiB int64, amount catalog.Amount) catalog.Offering {
	return namedComputeOffering("Example Virtual Machines", plan(name, vcpus, memoryMilliGiB, includedStorageGiB, amount))
}

// bareMetalOffering sells one bare-metal plan and a per-GiB storage price.
func bareMetalOffering() catalog.Offering {
	return catalog.Offering{
		Name:          "Example Bare Metal",
		Categories:    []taxonomy.Category{taxonomy.ComputeBareMetal},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("BM-1", 8, 16000, 100, 480000)},
		StoragePrices: []catalog.Price{price(500)},
	}
}

// kubernetesOffering sells one node plan, a per-GiB storage price and a fixed
// monthly fee for the control plane (ADR 0026).
func kubernetesOffering() catalog.Offering {
	return catalog.Offering{
		Name:          "Example Kubernetes",
		Categories:    []taxonomy.Category{taxonomy.KubernetesManaged},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("NK-1", 4, 8000, 50, 300000)},
		StoragePrices: []catalog.Price{price(300)},
		FixedFees:     []catalog.Price{price(90000)},
	}
}

// postgresOffering sells one database plan and a per-GiB storage price for the
// service's data.
func postgresOffering() catalog.Offering {
	return catalog.Offering{
		Name:          "Example PostgreSQL",
		Categories:    []taxonomy.Category{taxonomy.DatabasePostgreSQL},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("PG-1", 2, 4000, 20, 200000)},
		StoragePrices: []catalog.Price{price(1000)},
	}
}

// mysqlOffering sells one database plan and a per-GiB storage price for the
// service's data.
func mysqlOffering(storageAmount catalog.Amount) catalog.Offering {
	return catalog.Offering{
		Name:          "Example MySQL",
		Categories:    []taxonomy.Category{taxonomy.DatabaseMySQL},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("MY-1", 2, 4000, 20, 180000)},
		StoragePrices: []catalog.Price{price(storageAmount)},
	}
}

// blockOffering sells block storage at a per-GiB price, and no plan at all: a
// storage category ignores plans.
func blockOffering(storageAmount catalog.Amount) catalog.Offering {
	return catalog.Offering{
		Name:          "Example Block Storage",
		Categories:    []taxonomy.Category{taxonomy.StorageBlock},
		Regions:       []string{testutil.PricedDefaultRegion},
		StoragePrices: []catalog.Price{price(storageAmount)},
	}
}

// objectOffering sells object storage at a per-GiB price, and no plan.
func objectOffering(name string, storageAmount catalog.Amount) catalog.Offering {
	return catalog.Offering{
		Name:          name,
		Categories:    []taxonomy.Category{taxonomy.StorageObject},
		Regions:       []string{testutil.PricedDefaultRegion},
		StoragePrices: []catalog.Price{price(storageAmount)},
	}
}

// blockOfferingWithPlan sells block storage at a per-GiB price beside a plan,
// which a storage category ignores.
func blockOfferingWithPlan(storageAmount catalog.Amount) catalog.Offering {
	o := blockOffering(storageAmount)
	o.Plans = []catalog.Plan{plan("Ignored", 1, 1000, 0, 500000)}

	return o
}

// functionsOffering serves serverless functions, a category priced by usage, and
// sells no plan.
func functionsOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "Example Functions",
		Categories: []taxonomy.Category{taxonomy.ServerlessFunction},
		Regions:    []string{testutil.PricedDefaultRegion},
	}
}

// queueOffering serves message queues, a category priced by usage, and sells no
// plan.
func queueOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "Example Queues",
		Categories: []taxonomy.Category{taxonomy.MessagingQueue},
		Regions:    []string{testutil.PricedDefaultRegion},
	}
}

// objectResource is a storage.object resource with the given size.
func objectResource(size *inventory.Size) inventory.Resource {
	return inventory.Resource{
		ID:       "bucket-example",
		RawType:  "AWS::S3::Bucket",
		Category: taxonomy.StorageObject,
		Region:   "eu-west-1",
		Size:     size,
	}
}

// blockResource is a storage.block resource with the given size.
func blockResource(size *inventory.Size) inventory.Resource {
	return inventory.Resource{
		ID:       "vol-0123456789abcdef0",
		RawType:  "AWS::EC2::Volume",
		Category: taxonomy.StorageBlock,
		Region:   "eu-west-1",
		Size:     size,
	}
}

// queueResource is a messaging.queue resource, a category priced by usage.
func queueResource() inventory.Resource {
	return inventory.Resource{
		ID:       "queue-example",
		RawType:  "AWS::SQS::Queue",
		Category: taxonomy.MessagingQueue,
		Region:   "eu-west-1",
	}
}

// cachePlan is one plan of a cache offering: its memory in GiB and its monthly
// price per node, with the vCPU count the target's page gives, or 0 when it gives
// none (ADR 0046).
func cachePlan(name string, vcpus, memoryGiB int64, amount catalog.Amount) catalog.Plan {
	return plan(name, vcpus, memoryGiB*1000, 0, amount)
}

// searchCategory and kafkaCategory are PRD #571's two categories, as the values
// the taxonomy, the inventory file and the report share. They are written as
// values, not as the package's constants, so this file compiles while those
// constants do not exist yet.
const (
	searchCategory = taxonomy.Category("search.opensearch")
	kafkaCategory  = taxonomy.Category("messaging.kafka")
)

// nodePlan is one plan of a search.opensearch or messaging.kafka offering: the
// vCPUs and memory of one node, the storage it includes per node and its monthly
// price per node.
func nodePlan(name string, vcpus, memoryGiB, includedStorageGiB int64, amount catalog.Amount) catalog.Plan {
	return plan(name, vcpus, memoryGiB*1000, includedStorageGiB, amount)
}

// nodeOffering sells the plans given for category c, each the price of one node,
// and prices the GiB above what a plan includes at storageAmount a GiB.
func nodeOffering(name string, c taxonomy.Category, storageAmount catalog.Amount, plans ...catalog.Plan) catalog.Offering {
	return catalog.Offering{
		Name:          name,
		Categories:    []taxonomy.Category{c},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         plans,
		StoragePrices: []catalog.Price{price(storageAmount)},
	}
}

// searchOffering sells the search.opensearch plans given, each the price of one
// data node, and prices the GiB above what a plan includes at 0.10 EUR a GiB.
func searchOffering(name string, plans ...catalog.Plan) catalog.Offering {
	return nodeOffering(name, searchCategory, 1000, plans...)
}

// kafkaOffering sells the messaging.kafka plans given, priced as a search
// cluster is: per broker, and the GiB above what a plan includes a GiB.
func kafkaOffering(name string, plans ...catalog.Plan) catalog.Offering {
	return nodeOffering(name, kafkaCategory, 1000, plans...)
}

// searchResource is a search.opensearch resource: the size of one data node, the
// data nodes and how much of a month the cluster runs.
func searchResource(size *inventory.Size, usage *inventory.Usage) inventory.Resource {
	return inventory.Resource{
		ID:       "search-example",
		RawType:  "AWS::OpenSearchService::Domain",
		Category: searchCategory,
		Region:   "eu-west-1",
		Size:     size,
		Usage:    usage,
	}
}

// kafkaResource is a messaging.kafka resource, sized per broker.
func kafkaResource(size *inventory.Size, usage *inventory.Usage) inventory.Resource {
	return inventory.Resource{
		ID:       "kafka-example",
		RawType:  "AWS::MSK::Cluster",
		Category: kafkaCategory,
		Region:   "eu-west-1",
		Size:     size,
		Usage:    usage,
	}
}

// cacheOffering sells the cache plans given, all of them matched on memory alone,
// and prices storage per GiB beside them, which a cache estimate never uses.
func cacheOffering(name string, plans ...catalog.Plan) catalog.Offering {
	return catalog.Offering{
		Name:          name,
		Categories:    []taxonomy.Category{taxonomy.CacheRedis},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         plans,
		StoragePrices: []catalog.Price{price(250)},
	}
}

// cacheResource is a cache.redis resource: the memory of one node, its nodes and
// how much of a month it runs.
func cacheResource(size *inventory.Size, usage *inventory.Usage) inventory.Resource {
	return inventory.Resource{
		ID:       "cache-example",
		RawType:  "AWS::ElastiCache::ReplicationGroup",
		Category: taxonomy.CacheRedis,
		Region:   "eu-west-1",
		Size:     size,
		Usage:    usage,
	}
}

// mongoOffering sells one database.mongodb plan and a per-GiB storage price for
// the service's data, as postgresOffering does for PostgreSQL.
func mongoOffering() catalog.Offering {
	return catalog.Offering{
		Name:          "Example MongoDB",
		Categories:    []taxonomy.Category{taxonomy.DatabaseMongoDB},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("MO-1", 2, 4000, 20, 200000)},
		StoragePrices: []catalog.Price{price(1000)},
	}
}

// mongoResource is a database.mongodb resource with the given size and usage.
func mongoResource(size *inventory.Size, usage *inventory.Usage) inventory.Resource {
	return inventory.Resource{
		ID:       "db-example",
		RawType:  "AWS::DocDB::DBCluster",
		Category: taxonomy.DatabaseMongoDB,
		Region:   "eu-west-1",
		Size:     size,
		Usage:    usage,
	}
}

// fileOffering sells storage.file at a per-GiB price, and no plan.
func fileOffering(storageAmount catalog.Amount) catalog.Offering {
	return catalog.Offering{
		Name:          "Example File Storage",
		Categories:    []taxonomy.Category{taxonomy.StorageFile},
		Regions:       []string{testutil.PricedDefaultRegion},
		StoragePrices: []catalog.Price{price(storageAmount)},
	}
}

// fileResource is a storage.file resource with the given size.
func fileResource(size *inventory.Size) inventory.Resource {
	return inventory.Resource{
		ID:       "share-example",
		RawType:  "AWS::EFS::FileSystem",
		Category: taxonomy.StorageFile,
		Region:   "eu-west-1",
		Size:     size,
	}
}

// topicOffering serves messaging.topic, a category priced by usage, and sells no
// plan.
func topicOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "Example Topics",
		Categories: []taxonomy.Category{taxonomy.MessagingTopic},
		Regions:    []string{testutil.PricedDefaultRegion},
	}
}

// topicResource is a messaging.topic resource.
func topicResource() inventory.Resource {
	return inventory.Resource{
		ID:       "topic-example",
		RawType:  "AWS::SNS::Topic",
		Category: taxonomy.MessagingTopic,
		Region:   "eu-west-1",
	}
}

// containerOffering serves serverless.container, a category priced by usage, and
// sells no plan.
func containerOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "Example Serverless Containers",
		Categories: []taxonomy.Category{taxonomy.ServerlessContainer},
		Regions:    []string{testutil.PricedDefaultRegion},
	}
}

// containerResource is a serverless.container resource.
func containerResource() inventory.Resource {
	return inventory.Resource{
		ID:       "container-example",
		RawType:  "AWS::AppRunner::Service",
		Category: taxonomy.ServerlessContainer,
		Region:   "eu-west-1",
	}
}

// loadBalancerOffering serves network.loadbalancer and sells no plan: the target
// charges a flat fee per size, which snapshot format v2 cannot record.
func loadBalancerOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "Example Load Balancer",
		Categories: []taxonomy.Category{taxonomy.NetworkLoadBalancer},
		Regions:    []string{testutil.PricedDefaultRegion},
	}
}

// loadBalancerResource is a network.loadbalancer resource using the capabilities
// given.
func loadBalancerResource(capabilities ...taxonomy.Capability) inventory.Resource {
	return inventory.Resource{
		ID:           "lb-example",
		RawType:      "AWS::ElasticLoadBalancingV2::LoadBalancer",
		Category:     taxonomy.NetworkLoadBalancer,
		Region:       "eu-west-1",
		Capabilities: capabilities,
	}
}

// registryOffering serves registry.container and sells no plan.
func registryOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "Example Container Registry",
		Categories: []taxonomy.Category{taxonomy.RegistryContainer},
		Regions:    []string{testutil.PricedDefaultRegion},
	}
}

// registryResource is a registry.container resource.
func registryResource() inventory.Resource {
	return inventory.Resource{
		ID:       "registry-example",
		RawType:  "AWS::ECR::Repository",
		Category: taxonomy.RegistryContainer,
		Region:   "eu-west-1",
	}
}

// TestNeverPriced pins the reason a category that is never priced gets (ADR
// 0046): its String value is "not-priced", the same text as the Method
// estimate.NotPriced, which is a value of another type and keeps its own name. A
// load balancer with a fit is not "usage-priced": that reason would claim a
// target bills it per use.
func TestNeverPriced(t *testing.T) {
	t.Parallel()

	if got, want := string(estimate.NeverPriced), "not-priced"; got != want {
		t.Errorf("string(estimate.NeverPriced) = %q, want %q", got, want)
	}

	if got, want := string(estimate.NotPriced), "not-priced"; got != want {
		t.Errorf("string(estimate.NotPriced) = %q, want %q", got, want)
	}

	if estimate.NeverPriced == estimate.UsagePriced {
		t.Errorf("estimate.NeverPriced and estimate.UsagePriced are the same value %q, want one reason per rule",
			estimate.NeverPriced)
	}
}

// The offerings below pin the note an estimate by plan carries when it leaves
// out storage its plan does not include (ADR 0032). Every plan, price and region
// is invented, so a case can state each amount exactly.

// bootVolumeComputeOffering sells one compute.vm plan that includes no storage,
// and prices storage at 0.10 EUR a GiB: a VM that gives no storage size leaves a
// boot volume out of its estimate.
func bootVolumeComputeOffering() catalog.Offering {
	return catalog.Offering{
		Name:          "Example Compute Without Included Storage",
		Categories:    []taxonomy.Category{taxonomy.ComputeVM},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("BV-M", 2, 4000, 0, 200000)},
		StoragePrices: []catalog.Price{price(1000)},
	}
}

// includedStorageComputeOffering sells one compute.vm plan that includes 20 GiB,
// and prices storage at 0.10 EUR a GiB: a resource that gives no storage size
// needs none above what the plan includes, so its estimate carries no note.
func includedStorageComputeOffering() catalog.Offering {
	return catalog.Offering{
		Name:          "Example Compute With Included Storage",
		Categories:    []taxonomy.Category{taxonomy.ComputeVM},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("INC-M", 2, 4000, 20, 200000)},
		StoragePrices: []catalog.Price{price(1000)},
	}
}

// zeroStorageUnpricedComputeOffering sells one compute.vm plan that includes no
// storage, and prices no storage at all: the estimate has no price at which a
// boot volume would have been charged, so it carries no note.
func zeroStorageUnpricedComputeOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "Example Compute Without a Storage Price",
		Categories: []taxonomy.Category{taxonomy.ComputeVM},
		Regions:    []string{testutil.PricedDefaultRegion},
		Plans:      []catalog.Plan{plan("NOP-M", 2, 4000, 0, 200000)},
	}
}

// zeroStorageElsewhereComputeOffering sells one compute.vm plan that includes no
// storage, and prices storage only in the fixture's second region: the estimate
// region has no price to charge a boot volume at, so the estimate carries no
// note there.
func zeroStorageElsewhereComputeOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "Example Compute Priced Elsewhere",
		Categories: []taxonomy.Category{taxonomy.ComputeVM},
		Regions:    []string{testutil.PricedDefaultRegion, testutil.ReportSecondRegion},
		Plans:      []catalog.Plan{plan("ELSE-M", 2, 4000, 0, 200000)},
		StoragePrices: []catalog.Price{
			price(1000, testutil.ReportSecondRegion),
		},
	}
}

// bootVolumeBareMetalOffering sells one bare-metal plan that includes no storage,
// and prices storage at 0.05 EUR a GiB.
func bootVolumeBareMetalOffering() catalog.Offering {
	return catalog.Offering{
		Name:          "Example Bare Metal Without Included Storage",
		Categories:    []taxonomy.Category{taxonomy.ComputeBareMetal},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("BM-BV", 8, 16000, 0, 480000)},
		StoragePrices: []catalog.Price{price(500)},
	}
}

// bootVolumeKubernetesOffering sells one kubernetes.managed node plan that
// includes no storage, and prices storage at 0.10 EUR a GiB.
func bootVolumeKubernetesOffering() catalog.Offering {
	return catalog.Offering{
		Name:          "Example Kubernetes Without Included Storage",
		Categories:    []taxonomy.Category{taxonomy.KubernetesManaged},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("KN-BV", 2, 4000, 0, 300000)},
		StoragePrices: []catalog.Price{price(1000)},
	}
}

// dataStoragePostgresOffering sells one database.postgresql plan that includes
// no storage, and prices the service's data at 0.10 EUR a GiB.
func dataStoragePostgresOffering() catalog.Offering {
	return catalog.Offering{
		Name:          "Example PostgreSQL Without Included Storage",
		Categories:    []taxonomy.Category{taxonomy.DatabasePostgreSQL},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("PG-DS", 2, 4000, 0, 200000)},
		StoragePrices: []catalog.Price{price(1000)},
	}
}

// dataStorageMySQLOffering sells one database.mysql plan that includes no
// storage, and prices the service's data at 0.08 EUR a GiB.
func dataStorageMySQLOffering() catalog.Offering {
	return catalog.Offering{
		Name:          "Example MySQL Without Included Storage",
		Categories:    []taxonomy.Category{taxonomy.DatabaseMySQL},
		Regions:       []string{testutil.PricedDefaultRegion},
		Plans:         []catalog.Plan{plan("MY-DS", 2, 4000, 0, 180000)},
		StoragePrices: []catalog.Price{price(800)},
	}
}

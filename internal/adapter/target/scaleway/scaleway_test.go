// Package scaleway_test pins the Scaleway catalog adapter: the target contract in
// internal/adapter/target/targettest, the snapshot header the catalog reports, the
// plans, prices and fixed monthly fees the snapshot records and the default region
// they estimate in, a golden change detector for the parsed catalog, the declared
// EU regions, that a cancelled run reads nothing, and the verdicts PRD 14 asks of
// the data: high availability scoped to a node or a zone, one offering per tier,
// and a capability claimed only when the documentation meets it in full. Since
// PRD #552 milestone 6 the snapshot also serves the cache, MongoDB, load
// balancer, topic, serverless container and registry categories, prices the
// cache and MongoDB offerings by plan, records no price for the other four and
// serves no file share. The
// storage an offering charges beyond what a plan includes is pinned against the
// embedded snapshot too (issue #219): every offering in scope carries a per-GiB
// storage price, or says in SOURCES.md why it has none, and a bare-metal server
// sized above its plan's disks is reported with estimate.NoStoragePrice rather
// than priced from a price Scaleway does not publish. The
// snapshot document format itself, its strictness and its errors are pinned once,
// in the shared reader's own tests (internal/adapter/target/snapshot). It is
// black-box: only the package's exported API is used.
package scaleway_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/adapter/target/snapshot"
	"github.com/timothymamo/shift2eu/internal/adapter/target/targettest"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The adapter implements the catalog source port: pin that at compile time.
var _ analysis.CatalogSource = scaleway.New()

// The catalog the adapter embeds, and the golden file its parsed form renders to.
// Both are relative to this package's directory.
const (
	catalogFile = "catalog.json"
	sourcesFile = "SOURCES.md"
	goldenFile  = "testdata/catalog.golden"
)

// wantRegions is the EU region list catalog.json declares, written out here so
// the test cannot agree with a wrong declaration by construction: the contract
// expectations below and TestRegions compare against this list, never against
// what Regions() returns.
func wantRegions() []string {
	return []string{"fr-par", "it-mil", "nl-ams", "pl-waw"}
}

// TestCatalogContract runs the target contract suite every target adapter must
// pass: the display name, every offering only in a declared EU region, the four
// categories PRD 2 requires and the six PRD #552 milestone 6 adds, the same
// catalog on every call, and cancellation.
func TestCatalogContract(t *testing.T) {
	t.Parallel()

	targettest.Run(t, scaleway.New(), targettest.Expectations{
		Target:        "Scaleway",
		Regions:       wantRegions(),
		DefaultRegion: "fr-par",
		Categories: []taxonomy.Category{
			taxonomy.ComputeVM,
			taxonomy.KubernetesManaged,
			taxonomy.StorageObject,
			taxonomy.DatabasePostgreSQL,
			taxonomy.CacheRedis,
			taxonomy.DatabaseMongoDB,
			taxonomy.NetworkLoadBalancer,
			taxonomy.MessagingTopic,
			taxonomy.ServerlessContainer,
			taxonomy.RegistryContainer,
		},
	})
}

// catalogOf returns the catalog a fresh source serves.
func catalogOf(t *testing.T) catalog.Catalog {
	t.Helper()

	cat, err := scaleway.New().Catalog(t.Context())
	if err != nil {
		t.Fatalf("Catalog() returned unexpected error: %v", err)
	}

	return cat
}

// TestCatalogSnapshotHeader pins that the snapshot comes from the file header: the
// documentation root the data was compiled from, and its retrieval date as UTC
// midnight. A report states both, so neither may be invented by the adapter.
func TestCatalogSnapshotHeader(t *testing.T) {
	t.Parallel()

	snapshot := catalogOf(t).Snapshot()

	// The prices page: it carries the monthly EUR amounts the snapshot records, and
	// it becomes the source once plans and prices are in it (ADR 0024).
	const wantSourceURL = "https://www.scaleway.com/en/pricing/"
	if snapshot.SourceURL != wantSourceURL {
		t.Errorf("Snapshot().SourceURL = %s, want %s", text.Quote(snapshot.SourceURL), text.Quote(wantSourceURL))
	}

	wantRetrievedOn := time.Date(2026, time.November, 26, 0, 0, 0, 0, time.UTC)
	if !snapshot.RetrievedOn.Equal(wantRetrievedOn) {
		t.Errorf("Snapshot().RetrievedOn = %s, want %s",
			snapshot.RetrievedOn.Format(time.RFC3339), wantRetrievedOn.Format(time.RFC3339))
	}

	if got := snapshot.RetrievedOn.Location(); got != time.UTC {
		t.Errorf("Snapshot().RetrievedOn location = %v, want time.UTC", got)
	}
}

// TestCatalogPlansAndPrices pins PRD #190 milestone 4: the snapshot records what an
// estimate needs (ADR 0024). Every offering of a category priced by plan sells at
// least one plan, every offering of a category priced per GiB of storage lists at
// least one storage price, every offering of a category priced by plan whose plans
// include no storage prices the storage beyond the plan, every price is a positive
// monthly amount in EUR, and every offering available in the default region can be
// priced there, so an estimate for that region never borrows another region's price
// and never reads a missing price as zero. leftOut names the offerings the coverage
// rules below are waived for, with the reason, because Scaleway publishes no price
// for them.
func TestCatalogPlansAndPrices(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// The region estimates use when the user names none (ADR 0024): Paris is the
	// region hosting every offering of the snapshot, and the existing default.
	const wantRegion = "fr-par"
	if got := cat.Snapshot().DefaultRegion; got != wantRegion {
		t.Errorf("Snapshot().DefaultRegion = %s, want %s", text.Quote(got), text.Quote(wantRegion))
	}

	// planCategories are the categories PRD #190 prices by plan: a server or a
	// managed cluster runs on one, and so does a managed database node.
	planCategories := []taxonomy.Category{
		taxonomy.ComputeVM,
		taxonomy.ComputeBareMetal,
		taxonomy.KubernetesManaged,
		taxonomy.DatabasePostgreSQL,
		taxonomy.DatabaseMySQL,
		taxonomy.CacheRedis,
		taxonomy.DatabaseMongoDB,
	}

	// storageCategories are the categories PRD #190 prices per GiB of storage
	// kept: block volumes, objects, and the storage a managed database uses.
	storageCategories := []taxonomy.Category{
		taxonomy.StorageBlock,
		taxonomy.StorageObject,
		taxonomy.DatabasePostgreSQL,
		taxonomy.DatabaseMySQL,
		taxonomy.DatabaseMongoDB,
	}

	// leftOut exempts an offering from the two coverage rules, never from the
	// price rules, and gives the reason. An entry is a finding to explain in this
	// test and in SOURCES.md, not a way past a refresh.
	// Redis bills no storage volume: its data lives on the node's local SSD,
	// which the price page prices in no column, so the snapshot records no
	// per-GiB storage price and the offering has none to cover the rule with. Its
	// plans are pinned by TestNewCategoryOfferingsAndPrices, which is what makes
	// the exemption harmless here.
	leftOut := map[string]string{
		"Managed Database for Redis":         "Scaleway bills no storage volume for Redis: the local SSD is priced in no column",
		"Managed Database for Redis Cluster": "Scaleway bills no storage volume for Redis: the local SSD is priced in no column",
	}

	for _, o := range cat.Offerings() {
		reason, exempt := leftOut[o.Name]
		if exempt {
			t.Logf("offering %s is left out of the plan and storage-price rules: %s", text.Quote(o.Name), reason)
		}

		if !exempt && servesAny(o, planCategories) && len(o.Plans) == 0 {
			t.Errorf("offering %s serves a category priced by plan but lists no plan, want at least one",
				text.Quote(o.Name))
		}

		if !exempt && servesAny(o, storageCategories) && len(o.StoragePrices) == 0 {
			t.Errorf("offering %s serves a category priced per GiB of storage but lists no storage price, want at least one",
				text.Quote(o.Name))
		}

		// ADR 0029: a plan billing cap is UpCloud's alone. Scaleway publishes every
		// recorded price per hour and calls its monthly figure that price × 730,
		// with no maximum, so no offering may cap the hours it bills a plan for.
		if o.PlanBillingCapHours != 0 {
			t.Errorf("offering %s has PlanBillingCapHours %d, want 0: Scaleway records no monthly billing cap",
				text.Quote(o.Name), o.PlanBillingCapHours)
		}

		// A plan that includes no storage leaves every GiB of the resource to be
		// priced on its own: under ADR 0024 an empty storagePrices is not estimated,
		// never zero, so a sized resource on such a plan could not be estimated at
		// all. Scaleway sells Instances and Kapsule nodes without a local disk and
		// bills their volumes as Block Storage, so those offerings must price it.
		// Elastic Metal is not hit: its plans include their disks.
		if !exempt && servesAny(o, planCategories) && noPlanIncludesStorage(o) && len(o.StoragePrices) == 0 {
			t.Errorf("offering %s serves a category priced by plan and a plan of it includes no storage but it lists no storage price, want at least one: storage beyond the plan cannot be priced",
				text.Quote(o.Name))
		}

		for _, p := range o.Plans {
			for i, price := range p.Prices {
				checkPrice(t, o, fmt.Sprintf("plan %s prices[%d]", text.Quote(p.Name), i), price)
			}

			if slices.Contains(o.Regions, wantRegion) && !anyPriceAppliesIn(p.Prices, wantRegion) {
				t.Errorf("offering %s plan %s lists no price for the default region %s, want one applying there: its prices apply to %s",
					text.Quote(o.Name), text.Quote(p.Name), text.Quote(wantRegion), regionsListed(p.Prices))
			}
		}

		for i, price := range o.StoragePrices {
			checkPrice(t, o, fmt.Sprintf("storagePrices[%d]", i), price)
		}

		if slices.Contains(o.Regions, wantRegion) && len(o.StoragePrices) > 0 && !anyPriceAppliesIn(o.StoragePrices, wantRegion) {
			t.Errorf("offering %s lists storage prices but none for the default region %s, want one applying there: they apply to %s",
				text.Quote(o.Name), text.Quote(wantRegion), regionsListed(o.StoragePrices))
		}
	}
}

// TestCatalogFixedFees pins the fixed monthly fee per Kubernetes cluster the
// snapshot records (ADR 0026). Scaleway's mutualized Kapsule control plane is free:
// the offering stands for it and carries a fee of 0.00 in every region, which is a
// fee and not "no fee". The dedicated control plane is billed by the node size it
// runs on, 0.11 EUR an hour for Dedicated-4, which is 80.30 EUR a month at 730
// hours; Dedicated-4 is the smallest paid size, so that is a lower bound and not
// the only fee (Dedicated-8 costs 131.40 and Dedicated-16 255.50 a month). Every
// other offering charges no fixed fee, so no estimate adds one where Scaleway
// publishes none: the boolean of FixedFee tells a free region from no fee.
func TestCatalogFixedFees(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// feePerRegion is the control-plane fee each offering charges in each of its
	// regions: 0.00 where the offering stands for the free mutualized control
	// plane, and the Dedicated-4 control plane's 0.11 EUR an hour where it stands
	// for a dedicated one.
	const (
		mutualizedControlPlane = catalog.Amount(0)
		dedicatedControlPlane  = catalog.Amount(803000)
	)

	feePerRegion := map[string]map[string]catalog.Amount{
		// The mutualized control plane is included in the cluster's price.
		"Kubernetes Kapsule": {
			"fr-par": mutualizedControlPlane,
			"it-mil": mutualizedControlPlane,
			"nl-ams": mutualizedControlPlane,
			"pl-waw": mutualizedControlPlane,
		},
		// The dedicated control plane is billed by the size of its node.
		// Dedicated-4 is the smallest paid size, so its 80.30 EUR a month is a
		// lower bound: Dedicated-8 charges 131.40 and Dedicated-16 255.50. Milan
		// bills it like Paris (issue #211).
		"Kubernetes Kapsule Dedicated": {
			"fr-par": dedicatedControlPlane,
			"it-mil": dedicatedControlPlane,
			"nl-ams": dedicatedControlPlane,
			"pl-waw": dedicatedControlPlane,
		},
	}

	for _, o := range cat.Offerings() {
		want, charges := feePerRegion[o.Name]

		if !charges {
			if o.FixedFees != nil {
				t.Errorf("offering %s lists fixed fees %v, want none: Scaleway publishes a control-plane fee for its Kubernetes offerings only",
					text.Quote(o.Name), o.FixedFees)
			}

			for _, region := range o.Regions {
				if fee, ok := o.FixedFee(region); ok {
					t.Errorf("offering %s reports a fixed fee %s in region %s, want none",
						text.Quote(o.Name), fee, text.Quote(region))
				}
			}

			continue
		}

		for _, region := range o.Regions {
			wantFee, ok := want[region]
			if !ok {
				t.Errorf("offering %s: the expectation has no fee for its region %s",
					text.Quote(o.Name), text.Quote(region))

				continue
			}

			got, ok := o.FixedFee(region)
			if !ok {
				t.Errorf("offering %s reports no fixed fee in region %s, want %s",
					text.Quote(o.Name), text.Quote(region), wantFee)

				continue
			}

			if got != wantFee {
				t.Errorf("offering %s: FixedFee(%s) = %s, want %s",
					text.Quote(o.Name), text.Quote(region), got, wantFee)
			}
		}

		for region := range want {
			if !slices.Contains(o.Regions, region) {
				t.Errorf("offering %s: the expectation has a fee for region %s, which is not one of its regions %v",
					text.Quote(o.Name), text.Quote(region), o.Regions)
			}
		}
	}
}

// TestNewCategoryOfferingsAndPrices pins PRD #552 milestone 6 on the embedded
// snapshot: the offerings the new categories gain, the regions each is available
// in, and the plans and prices of the two categories Scaleway prices by plan.
//
// The Redis plans are the price page's node types, priced per node: a Standalone
// offering at the page's main-node price × 730, and a Cluster offering at
// (main node + 2 × additional node) × 730 / 3, the 3-node minimum the page's
// two columns imply, which a 4- to 6-node cluster prices a little high
// (milestone 6's decision 1, SOURCES.md). The MongoDB plans are the page's
// "Price for 1 Node" × 730 on the 1-node offering and "Price for 3 Nodes" × 730
// / 3 per node on the Replica Set, and both price the Block Storage the plans do
// not include at 0.0993 a GiB a month. The load balancer, Topics and Events,
// Serverless Containers and Container Registry are not sold by plan, so they
// list none: their categories are never priced or usage-priced.
func TestNewCategoryOfferingsAndPrices(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	const (
		redis             = "Managed Database for Redis"
		redisCluster      = "Managed Database for Redis Cluster"
		mongodb           = "Managed MongoDB"
		mongodbReplicaSet = "Managed MongoDB Replica Set"
	)

	t.Run("regions", func(t *testing.T) {
		t.Parallel()

		// The availability page and the SDKs agree on these; Milan is left out of
		// the load balancer and the registry because their SDKs do not declare it,
		// and of Redis and MongoDB because both are coming soon there.
		tests := []struct {
			offering string
			want     []string
		}{
			{offering: redis, want: []string{"fr-par", "nl-ams", "pl-waw"}},
			{offering: redisCluster, want: []string{"fr-par", "nl-ams", "pl-waw"}},
			{offering: mongodb, want: []string{"fr-par"}},
			{offering: mongodbReplicaSet, want: []string{"fr-par"}},
			{offering: "Load Balancer", want: []string{"fr-par", "nl-ams", "pl-waw"}},
			{offering: "Topics and Events", want: []string{"fr-par", "nl-ams"}},
			{offering: "Serverless Containers", want: []string{"fr-par", "it-mil", "nl-ams", "pl-waw"}},
			{offering: "Container Registry", want: []string{"fr-par", "nl-ams", "pl-waw"}},
		}

		for _, tc := range tests {
			t.Run(tc.offering, func(t *testing.T) {
				t.Parallel()

				o := offeringNamed(t, cat, tc.offering)

				if diff := cmp.Diff(tc.want, o.Regions); diff != "" {
					t.Errorf("offering %s regions mismatch (-want +got):\n%s", text.Quote(o.Name), diff)
				}
			})
		}
	})

	t.Run("plans", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name               string
			offering           string
			plan               string
			wantVCPUs          int64
			wantMemoryMilliGiB int64
			wantIncludedGiB    int64
			wantAmount         catalog.Amount
		}{
			// Standalone: the main node's hourly price × 730.
			{name: "redis/red1-micro", offering: redis, plan: "RED1-MICRO", wantVCPUs: 2, wantMemoryMilliGiB: 2_000, wantAmount: catalog.Amount(350_400)},
			{name: "redis/red1-2xs", offering: redis, plan: "RED1-2XS", wantVCPUs: 3, wantMemoryMilliGiB: 4_000, wantAmount: catalog.Amount(547_500)},
			{name: "redis/red1-xs", offering: redis, plan: "RED1-XS", wantVCPUs: 4, wantMemoryMilliGiB: 8_000, wantAmount: catalog.Amount(1_000_100)},
			{name: "redis/red1-s", offering: redis, plan: "RED1-S", wantVCPUs: 4, wantMemoryMilliGiB: 12_000, wantAmount: catalog.Amount(1_496_500)},
			{name: "redis/red1-m", offering: redis, plan: "RED1-M", wantVCPUs: 4, wantMemoryMilliGiB: 16_000, wantAmount: catalog.Amount(2_000_200)},
			{name: "redis/red1-l", offering: redis, plan: "RED1-L", wantVCPUs: 8, wantMemoryMilliGiB: 32_000, wantAmount: catalog.Amount(4_000_400)},
			{name: "redis/red1-xl", offering: redis, plan: "RED1-XL", wantVCPUs: 16, wantMemoryMilliGiB: 64_000, wantAmount: catalog.Amount(8_000_800)},
			// Cluster: (main node + 2 × additional node) × 730 / 3, per node.
			{name: "redis-cluster/red1-micro", offering: redisCluster, plan: "RED1-MICRO", wantVCPUs: 2, wantMemoryMilliGiB: 2_000, wantAmount: catalog.Amount(248_200)},
			{name: "redis-cluster/red1-2xs", offering: redisCluster, plan: "RED1-2XS", wantVCPUs: 3, wantMemoryMilliGiB: 4_000, wantAmount: catalog.Amount(382_033)},
			{name: "redis-cluster/red1-xs", offering: redisCluster, plan: "RED1-XS", wantVCPUs: 4, wantMemoryMilliGiB: 8_000, wantAmount: catalog.Amount(732_433)},
			{name: "redis-cluster/red1-s", offering: redisCluster, plan: "RED1-S", wantVCPUs: 4, wantMemoryMilliGiB: 12_000, wantAmount: catalog.Amount(1_097_433)},
			{name: "redis-cluster/red1-m", offering: redisCluster, plan: "RED1-M", wantVCPUs: 4, wantMemoryMilliGiB: 16_000, wantAmount: catalog.Amount(1_464_867)},
			{name: "redis-cluster/red1-l", offering: redisCluster, plan: "RED1-L", wantVCPUs: 8, wantMemoryMilliGiB: 32_000, wantAmount: catalog.Amount(2_934_600)},
			{name: "redis-cluster/red1-xl", offering: redisCluster, plan: "RED1-XL", wantVCPUs: 16, wantMemoryMilliGiB: 64_000, wantAmount: catalog.Amount(5_869_200)},
			// MongoDB, 1 node: the page's "Price for 1 Node" × 730.
			{name: "mongodb/play2-nano", offering: mongodb, plan: "MGDB-PLAY2-NANO", wantVCPUs: 2, wantMemoryMilliGiB: 4_000, wantAmount: catalog.Amount(786_210)},
			{name: "mongodb/pro2-xxs", offering: mongodb, plan: "MGDB-PRO2-XXS", wantVCPUs: 2, wantMemoryMilliGiB: 8_000, wantAmount: catalog.Amount(1_278_230)},
			{name: "mongodb/pro2-xs", offering: mongodb, plan: "MGDB-PRO2-XS", wantVCPUs: 4, wantMemoryMilliGiB: 16_000, wantAmount: catalog.Amount(2_223_580)},
			{name: "mongodb/pro2-s", offering: mongodb, plan: "MGDB-PRO2-S", wantVCPUs: 8, wantMemoryMilliGiB: 32_000, wantAmount: catalog.Amount(4_120_120)},
			{name: "mongodb/pro2-m", offering: mongodb, plan: "MGDB-PRO2-M", wantVCPUs: 16, wantMemoryMilliGiB: 64_000, wantAmount: catalog.Amount(7_900_060)},
			{name: "mongodb/pro2-l", offering: mongodb, plan: "MGDB-PRO2-L", wantVCPUs: 32, wantMemoryMilliGiB: 128_000, wantAmount: catalog.Amount(15_419_790)},
			{name: "mongodb/pop2-2c-8g", offering: mongodb, plan: "MGDB-POP2-2C-8G", wantVCPUs: 2, wantMemoryMilliGiB: 8_000, wantAmount: catalog.Amount(1_320_570)},
			{name: "mongodb/pop2-4c-16g", offering: mongodb, plan: "MGDB-POP2-4C-16G", wantVCPUs: 4, wantMemoryMilliGiB: 16_000, wantAmount: catalog.Amount(2_314_100)},
			{name: "mongodb/pop2-8c-32g", offering: mongodb, plan: "MGDB-POP2-8C-32G", wantVCPUs: 8, wantMemoryMilliGiB: 32_000, wantAmount: catalog.Amount(4_288_750)},
			{name: "mongodb/pop2-16c-64g", offering: mongodb, plan: "MGDB-POP2-16C-64G", wantVCPUs: 16, wantMemoryMilliGiB: 64_000, wantAmount: catalog.Amount(8_245_350)},
			{name: "mongodb/pop2-32c-128g", offering: mongodb, plan: "MGDB-POP2-32C-128G", wantVCPUs: 32, wantMemoryMilliGiB: 128_000, wantAmount: catalog.Amount(16_107_450)},
			{name: "mongodb/pop2-64c-256g", offering: mongodb, plan: "MGDB-POP2-64C-256G", wantVCPUs: 64, wantMemoryMilliGiB: 256_000, wantAmount: catalog.Amount(31_875_450)},
			// Replica Set: the page's "Price for 3 Nodes" × 730 / 3, per node.
			{name: "mongodb-replica-set/play2-nano", offering: mongodbReplicaSet, plan: "MGDB-PLAY2-NANO", wantVCPUs: 2, wantMemoryMilliGiB: 4_000, wantAmount: catalog.Amount(456_737)},
			{name: "mongodb-replica-set/pro2-xxs", offering: mongodbReplicaSet, plan: "MGDB-PRO2-XXS", wantVCPUs: 2, wantMemoryMilliGiB: 8_000, wantAmount: catalog.Amount(878_677)},
			{name: "mongodb-replica-set/pro2-xs", offering: mongodbReplicaSet, plan: "MGDB-PRO2-XS", wantVCPUs: 4, wantMemoryMilliGiB: 16_000, wantAmount: catalog.Amount(1_748_593)},
			{name: "mongodb-replica-set/pro2-s", offering: mongodbReplicaSet, plan: "MGDB-PRO2-S", wantVCPUs: 8, wantMemoryMilliGiB: 32_000, wantAmount: catalog.Amount(3_363_840)},
			{name: "mongodb-replica-set/pro2-m", offering: mongodbReplicaSet, plan: "MGDB-PRO2-M", wantVCPUs: 16, wantMemoryMilliGiB: 64_000, wantAmount: catalog.Amount(6_580_220)},
			{name: "mongodb-replica-set/pro2-l", offering: mongodbReplicaSet, plan: "MGDB-PRO2-L", wantVCPUs: 32, wantMemoryMilliGiB: 128_000, wantAmount: catalog.Amount(12_965_530)},
			{name: "mongodb-replica-set/pop2-2c-8g", offering: mongodbReplicaSet, plan: "MGDB-POP2-2C-8G", wantVCPUs: 2, wantMemoryMilliGiB: 8_000, wantAmount: catalog.Amount(980_390)},
			{name: "mongodb-replica-set/pop2-4c-16g", offering: mongodbReplicaSet, plan: "MGDB-POP2-4C-16G", wantVCPUs: 4, wantMemoryMilliGiB: 16_000, wantAmount: catalog.Amount(1_973_433)},
			{name: "mongodb-replica-set/pop2-8c-32g", offering: mongodbReplicaSet, plan: "MGDB-POP2-8C-32G", wantVCPUs: 8, wantMemoryMilliGiB: 32_000, wantAmount: catalog.Amount(3_789_917)},
			{name: "mongodb-replica-set/pop2-16c-64g", offering: mongodbReplicaSet, plan: "MGDB-POP2-16C-64G", wantVCPUs: 16, wantMemoryMilliGiB: 64_000, wantAmount: catalog.Amount(7_430_183)},
			{name: "mongodb-replica-set/pop2-32c-128g", offering: mongodbReplicaSet, plan: "MGDB-POP2-32C-128G", wantVCPUs: 32, wantMemoryMilliGiB: 128_000, wantAmount: catalog.Amount(14_654_750)},
			{name: "mongodb-replica-set/pop2-64c-256g", offering: mongodbReplicaSet, plan: "MGDB-POP2-64C-256G", wantVCPUs: 64, wantMemoryMilliGiB: 256_000, wantAmount: catalog.Amount(29_152_550)},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				o := offeringNamed(t, cat, tc.offering)
				p := planNamed(t, o, tc.plan)

				if p.VCPUs != tc.wantVCPUs {
					t.Errorf("offering %s plan %s vCPUs = %d, want %d: the page's vCPU column, per node",
						text.Quote(o.Name), text.Quote(p.Name), p.VCPUs, tc.wantVCPUs)
				}

				if p.MemoryMilliGiB != tc.wantMemoryMilliGiB {
					t.Errorf("offering %s plan %s memory = %d thousandths of a GiB, want %d",
						text.Quote(o.Name), text.Quote(p.Name), p.MemoryMilliGiB, tc.wantMemoryMilliGiB)
				}

				if p.IncludedStorageGiB != tc.wantIncludedGiB {
					t.Errorf("offering %s plan %s included storage = %d GiB, want %d: Scaleway includes no storage in these plans",
						text.Quote(o.Name), text.Quote(p.Name), p.IncludedStorageGiB, tc.wantIncludedGiB)
				}

				for _, region := range o.Regions {
					got, ok := o.PlanPrice(tc.plan, region)
					if !ok {
						t.Errorf("offering %s lists no price for plan %s in region %s, want %s a month",
							text.Quote(o.Name), text.Quote(tc.plan), text.Quote(region), tc.wantAmount)

						continue
					}

					if got != tc.wantAmount {
						t.Errorf("offering %s plan %s price in region %s = %s, want %s: the page's hourly price per node × 730, as milestone 6's decision 1 records it",
							text.Quote(o.Name), text.Quote(tc.plan), text.Quote(region), got, tc.wantAmount)
					}
				}
			})
		}
	})

	t.Run("plan counts", func(t *testing.T) {
		t.Parallel()

		// The plans subtest walks the plans the table names, so a plan present in
		// the snapshot and absent here would go unnoticed: pin each offering's
		// count too.
		tests := []struct {
			offering string
			want     int
		}{
			{offering: redis, want: 7},
			{offering: redisCluster, want: 7},
			{offering: mongodb, want: 12},
			{offering: mongodbReplicaSet, want: 12},
		}

		for _, tc := range tests {
			t.Run(tc.offering, func(t *testing.T) {
				t.Parallel()

				o := offeringNamed(t, cat, tc.offering)

				if len(o.Plans) != tc.want {
					t.Errorf("offering %s lists %d plans, want %d: %v",
						text.Quote(o.Name), len(o.Plans), tc.want, planNames(o))
				}
			})
		}
	})

	t.Run("mongodb storage prices", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			offering string
			want     catalog.Amount
		}{
			{offering: mongodb, want: catalog.Amount(993)},
			{offering: mongodbReplicaSet, want: catalog.Amount(993)},
		}

		for _, tc := range tests {
			t.Run(tc.offering, func(t *testing.T) {
				t.Parallel()

				o := offeringNamed(t, cat, tc.offering)

				for _, region := range o.Regions {
					got, ok := o.StoragePrice(region)
					if !ok {
						t.Errorf("offering %s lists no storage price in region %s, want %s a GiB a month",
							text.Quote(o.Name), text.Quote(region), tc.want)

						continue
					}

					if got != tc.want {
						t.Errorf("offering %s storage price in region %s = %s, want %s: Block Storage 5K, 0.000136 an hour × 730",
							text.Quote(o.Name), text.Quote(region), got, tc.want)
					}
				}
			})
		}
	})

	t.Run("redis bills no storage", func(t *testing.T) {
		t.Parallel()

		// The page prices the node's local SSD in no column, so neither Redis
		// offering may carry a per-GiB price, which an estimate would read as a
		// price Scaleway publishes (ADR 0024).
		for _, name := range []string{redis, redisCluster} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				o := offeringNamed(t, cat, name)

				if len(o.StoragePrices) != 0 {
					t.Errorf("offering %s lists %d storage prices, want none: Scaleway bills no storage volume for Redis",
						text.Quote(o.Name), len(o.StoragePrices))
				}
			})
		}
	})

	t.Run("no plans", func(t *testing.T) {
		t.Parallel()

		// PRD #552 milestone 6's decision 3 and 6: a load balancer and a registry
		// are billed by a size an inventory never records, and a topic and a
		// serverless container by usage, so none of the four is sold by plan.
		tests := []struct {
			offering string
			category taxonomy.Category
		}{
			{offering: "Load Balancer", category: taxonomy.NetworkLoadBalancer},
			{offering: "Topics and Events", category: taxonomy.MessagingTopic},
			{offering: "Serverless Containers", category: taxonomy.ServerlessContainer},
			{offering: "Container Registry", category: taxonomy.RegistryContainer},
		}

		for _, tc := range tests {
			t.Run(tc.offering, func(t *testing.T) {
				t.Parallel()

				o := offeringNamed(t, cat, tc.offering)

				if !slices.Contains(o.Categories, tc.category) {
					t.Errorf("offering %s categories = %v, want them to include %s",
						text.Quote(o.Name), o.Categories, text.Quote(string(tc.category)))
				}

				if len(o.Plans) != 0 {
					t.Errorf("offering %s lists plans %v, want none: the category is priced by usage or not priced at all",
						text.Quote(o.Name), planNames(o))
				}

				if len(o.StoragePrices) != 0 {
					t.Errorf("offering %s lists %d storage prices, want none", text.Quote(o.Name), len(o.StoragePrices))
				}

				if len(o.FixedFees) != 0 {
					t.Errorf("offering %s lists fixed fees, want none: Scaleway publishes a control-plane fee for its Kubernetes offerings only",
						text.Quote(o.Name))
				}
			})
		}
	})
}

// planNamed returns o's plan called name, and fails the test if there is none.
func planNamed(t *testing.T, o catalog.Offering, name string) catalog.Plan {
	t.Helper()

	for _, p := range o.Plans {
		if p.Name == name {
			return p
		}
	}

	t.Fatalf("offering %s has no plan called %s: it sells %v", text.Quote(o.Name), text.Quote(name), planNames(o))

	return catalog.Plan{}
}

// TestKapsuleDedicatedInMilan pins issue #211: Scaleway's Containers price page,
// first read 2026-11-13 and re-checked in Milan on 2026-11-19, lists the dedicated
// Kapsule control plane there, so the offering is available and billed in it-mil as
// it is in Paris. Its worker-node plans are the mutualized offering's: the POP2
// nodes, whose price applies to every region, are priced in it-mil, while the PLAY2
// and PRO2 nodes are not priced there at all. The dedicated control plane is a fixed
// fee only (ADR 0026, pinned in TestCatalogFixedFees), never a plan, so nothing in
// Milan can pick or price a Dedicated-N size as if it were a worker node.
func TestKapsuleDedicatedInMilan(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	const (
		mutualized = "Kubernetes Kapsule"
		dedicated  = "Kubernetes Kapsule Dedicated"
		milan      = "it-mil"
	)

	base := offeringNamed(t, cat, mutualized)
	offering := offeringNamed(t, cat, dedicated)

	if !offering.AvailableIn(milan) {
		t.Errorf("offering %s reports AvailableIn(%s) = false, want true: Scaleway lists its control plane there (issue #211)",
			text.Quote(dedicated), text.Quote(milan))
	}

	tests := []struct {
		name string
		plan string
		want map[string]string
	}{
		{
			name: "a POP2 worker node is priced in Milan as in the other regions",
			plan: "POP2-2C-8G",
			want: map[string]string{
				"fr-par": "53.655",
				"it-mil": "53.655",
				"nl-ams": "53.655",
				"pl-waw": "53.655",
			},
		},
		{
			name: "a PLAY2 worker node is not priced in Milan",
			plan: "PLAY2-PICO",
			want: map[string]string{
				"fr-par": "10.4244",
				"it-mil": unpricedPlan,
				"nl-ams": "10.4244",
				"pl-waw": "10.4244",
			},
		},
		{
			name: "a PRO2 worker node is not priced in Milan",
			plan: "PRO2-XXS",
			want: map[string]string{
				"fr-par": "40.953",
				"it-mil": unpricedPlan,
				"nl-ams": "40.953",
				"pl-waw": "40.953",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, pricedRegions(offering, tc.plan)); diff != "" {
				t.Errorf("offering %s plan %s by region (-want +got):\n%s",
					text.Quote(dedicated), text.Quote(tc.plan), diff)
			}
		})
	}

	// Both offerings sell the same worker nodes: only their control plane differs,
	// a fixed fee for the dedicated one (ADR 0026). The price loop below walks the
	// dedicated offering's plans alone, so a plan present in just one of the two
	// offerings would go unnoticed, and so would every price it records there
	// (issue #329). Compare the sets of plan names first, order-independently,
	// naming each plan one offering is missing: the rule is about a worker node
	// sold for both, so it names the plans and says nothing else.
	basePlans, dedicatedPlans := planNames(base), planNames(offering)

	for _, name := range namesMissingFrom(basePlans, dedicatedPlans) {
		t.Errorf("offering %s lists plan %s, want offering %s to list it too: a worker node sold for one offering is sold for both",
			text.Quote(mutualized), text.Quote(name), text.Quote(dedicated))
	}

	for _, name := range namesMissingFrom(dedicatedPlans, basePlans) {
		t.Errorf("offering %s lists plan %s, want offering %s to list it too: a worker node sold for one offering is sold for both",
			text.Quote(dedicated), text.Quote(name), text.Quote(mutualized))
	}

	// Every plan of the dedicated offering is priced exactly where the mutualized
	// one prices it, and nowhere else: the worker nodes of a dedicated cluster are
	// the same nodes, so a plan Scaleway does not sell in a region stays unpriced
	// there rather than borrowing another region's price (ADR 0024).
	for _, plan := range offering.Plans {
		for _, region := range wantRegions() {
			wantAmount, wantPriced := base.PlanPrice(plan.Name, region)
			gotAmount, gotPriced := offering.PlanPrice(plan.Name, region)

			if gotPriced != wantPriced {
				t.Errorf("offering %s plan %s: PlanPrice(%s) reports a price = %t, want %t, as offering %s does",
					text.Quote(dedicated), text.Quote(plan.Name), text.Quote(region), gotPriced, wantPriced, text.Quote(mutualized))

				continue
			}

			if gotAmount != wantAmount {
				t.Errorf("offering %s plan %s: PlanPrice(%s) = %s, want %s, as offering %s prices it",
					text.Quote(dedicated), text.Quote(plan.Name), text.Quote(region), gotAmount, wantAmount, text.Quote(mutualized))
			}
		}
	}

	// The control plane is the offering's fixed fee, never a plan: a plan named
	// Dedicated-N would let an estimate price a control-plane size as a worker
	// node, which is the wrong shape of price altogether (ADR 0026).
	for _, plan := range offering.Plans {
		if strings.HasPrefix(plan.Name, "Dedicated-") {
			t.Errorf("offering %s lists plan %s, want no plan named %s: the control plane is a fixed fee, not a plan",
				text.Quote(dedicated), text.Quote(plan.Name), text.Quote("Dedicated-*"))
		}
	}
}

// planNames returns the names of o's plans, sorted, so that two offerings' plan
// names can be compared as sets: the snapshot's plan order says nothing about
// whether the two sell the same worker nodes.
func planNames(o catalog.Offering) []string {
	names := make([]string, 0, len(o.Plans))

	for _, plan := range o.Plans {
		names = append(names, plan.Name)
	}

	slices.Sort(names)

	return names
}

// namesMissingFrom returns the names of names that other does not list, in the
// order names lists them: the plans an offering sells and the other does not.
func namesMissingFrom(names, other []string) []string {
	var missing []string

	for _, name := range names {
		if !slices.Contains(other, name) {
			missing = append(missing, name)
		}
	}

	return missing
}

// unpricedPlan is what pricedRegions reports for a region an offering does not
// price a plan in: an unpriced plan is "not estimated" there, never free (ADR 0027).
const unpricedPlan = "not priced"

// pricedRegions returns the price of plan in o in each of wantRegions, as the
// amount's decimal string, or unpricedPlan where o prices no plan there. Keying
// the result by region makes a diff read region by region.
func pricedRegions(o catalog.Offering, plan string) map[string]string {
	prices := make(map[string]string, len(wantRegions()))

	for _, region := range wantRegions() {
		amount, ok := o.PlanPrice(plan, region)
		if !ok {
			prices[region] = unpricedPlan

			continue
		}

		prices[region] = amount.String()
	}

	return prices
}

// checkPrice pins what every price of the snapshot must be, wherever it appears: a
// positive monthly amount in EUR, applying only to regions its offering is
// available in (ADR 0024). where names the price inside its offering.
func checkPrice(t *testing.T, o catalog.Offering, where string, price catalog.Price) {
	t.Helper()

	if price.Currency != catalog.EUR {
		t.Errorf("offering %s %s: currency = %s, want %s",
			text.Quote(o.Name), where, text.Quote(price.Currency), text.Quote(catalog.EUR))
	}

	if price.Amount <= 0 {
		t.Errorf("offering %s %s: amount = %s, want more than 0: a price is never zero for unknown",
			text.Quote(o.Name), where, price.Amount)
	}

	for _, region := range price.Regions {
		if !slices.Contains(o.Regions, region) {
			t.Errorf("offering %s %s: applies to region %s, want one of the offering's regions %v",
				text.Quote(o.Name), where, text.Quote(region), o.Regions)
		}
	}
}

// noPlanIncludesStorage reports whether one of o's plans includes no storage, which
// leaves the storage the resource needs to be priced per GiB.
func noPlanIncludesStorage(o catalog.Offering) bool {
	return slices.ContainsFunc(o.Plans, func(p catalog.Plan) bool {
		return p.IncludedStorageGiB == 0
	})
}

// servesAny reports whether o serves one of categories.
func servesAny(o catalog.Offering, categories []taxonomy.Category) bool {
	for _, c := range categories {
		if slices.Contains(o.Categories, c) {
			return true
		}
	}

	return false
}

// anyPriceAppliesIn reports whether one of prices applies in region: a price that
// lists no region applies to every region of its offering (ADR 0024).
func anyPriceAppliesIn(prices []catalog.Price, region string) bool {
	return slices.ContainsFunc(prices, func(p catalog.Price) bool {
		return len(p.Regions) == 0 || slices.Contains(p.Regions, region)
	})
}

// offeringNamed returns the catalog's offering called name, and fails the test if
// there is none.
func offeringNamed(t *testing.T, cat catalog.Catalog, name string) catalog.Offering {
	t.Helper()

	for _, o := range cat.Offerings() {
		if o.Name == name {
			return o
		}
	}

	t.Fatalf("the catalog has no offering called %s: the storage-price rule below assumes it", text.Quote(name))

	return catalog.Offering{}
}

// noStoragePriceMarker is the convention an offering's section of SOURCES.md
// follows when the snapshot deliberately records no per-GiB storage price for it:
// a bullet whose text, once the Markdown bullet and any bold markers are removed,
// begins with these words (today's form is "- **No storage price.** ..."). The
// bullet states which page was read and why no price follows from it (ADR 0024),
// so a missing price is a finding, never an omission. The OVHcloud adapter's test
// pins the same convention; it is duplicated here so that neither target's test
// depends on the other's package.
const noStoragePriceMarker = "No storage price"

// TestCatalogStoragePriceCoverage pins issue #219's acceptance on the embedded
// snapshot: every offering in scope either has a per-GiB storage price applying
// in each of its regions, so an estimate can price the storage above what a plan
// includes (ADR 0027), or its own section of SOURCES.md says why it has none.
// Elastic Metal sells no storage beyond the fixed local disks its plans include:
// its FAQ says "Elastic Metal is not currently compatible with Block Storage, but
// this functionality is planned for the future", and its price page's only
// add-ons are network and Windows licences. So its storagePrices stays empty, and
// the missing price has to be a documented finding rather than an omission. A
// price the snapshot gains must still be sourced in SOURCES.md.
func TestCatalogStoragePriceCoverage(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)
	sections := sourcesSections(t)

	tests := []struct {
		offering string
	}{
		{offering: "Elastic Metal"},
	}

	for _, tc := range tests {
		t.Run(tc.offering, func(t *testing.T) {
			t.Parallel()

			o := offeringNamed(t, cat, tc.offering)

			var missing []string

			for _, region := range o.Regions {
				if _, ok := o.StoragePrice(region); !ok {
					missing = append(missing, region)
				}
			}

			if len(missing) == 0 {
				return
			}

			body, documented := sections[tc.offering]
			if !documented {
				t.Errorf("offering %s has no per-GiB storage price in region(s) %s and no section %s in %s, want a section saying why it has none",
					text.Quote(o.Name), strings.Join(missing, ", "), text.Quote("### "+o.Name), text.Quote(sourcesFile))

				return
			}

			if !hasNoStoragePriceMarker(body) {
				t.Errorf("offering %s has no per-GiB storage price in region(s) %s and its section of %s does not say why, want a bullet whose text begins %q (the reason no price is recorded, and the page it was looked for on)",
					text.Quote(o.Name), strings.Join(missing, ", "), text.Quote(sourcesFile), noStoragePriceMarker)
			}
		})
	}
}

// TestElasticMetalExtraStorageIsNotEstimated pins the estimate side of the same
// finding: Elastic Metal publishes no price for storage beyond a plan's local
// disks, so a bare-metal server sized above the disks of the cheapest plan that
// covers it must be reported with estimate.NoStoragePrice, never priced from a
// price the snapshot does not have (ADR 0024, ADR 0027). 8 vCPU and 64 GiB are
// covered by `EM-A410X-SSD`, whose 2040 GiB of disks the resource's 2100 GiB
// exceed by 60 GiB; the offering prices no GiB, so there is no storage part.
func TestElasticMetalExtraStorageIsNotEstimated(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)
	region := cat.Snapshot().DefaultRegion

	r := inventory.Resource{
		ID:       "res-example",
		RawType:  "AWS::Example::BareMetal",
		Category: taxonomy.ComputeBareMetal,
		Region:   "eu-west-1",
		Size: &inventory.Size{
			VCPUs:      number(8),
			MemoryGiB:  number(64),
			StorageGiB: number(2100),
		},
	}

	inv, err := inventory.New("aws", []inventory.Resource{r})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	got := estimate.For(mapping.Map(inv.Resources()[0], cat), region)

	if got.Reason != estimate.NoStoragePrice {
		t.Errorf("For() reason = %s, want %s: Elastic Metal publishes no per-GiB storage price, so the 60 GiB above plan %s's disks cannot be priced (issue #219)",
			text.Quote(string(got.Reason)), text.Quote(string(estimate.NoStoragePrice)), text.Quote("EM-A410X-SSD"))
	}

	if got.Estimated {
		t.Errorf("For() Estimated = true with monthly %v, want false: no storage price means no estimate, never one at 0 EUR",
			got.Monthly)
	}

	if got.Plan == nil {
		t.Fatalf("For() Plan = nil, want the plan the resource fits: %s, whose disks its 2100 GiB exceed",
			text.Quote("EM-A410X-SSD"))
	}

	if got.Plan.Name != "EM-A410X-SSD" {
		t.Errorf("For() Plan.Name = %s, want %s: it is the plan covering 8 vCPU and 64 GiB, whose 2040 GiB of disks the resource's 2100 GiB exceed",
			text.Quote(got.Plan.Name), text.Quote("EM-A410X-SSD"))
	}

	if got.Offering != "Elastic Metal" {
		t.Errorf("For() Offering = %s, want %s: a %s resource maps onto the bare-metal offering",
			text.Quote(got.Offering), text.Quote("Elastic Metal"), text.Quote(string(taxonomy.ComputeBareMetal)))
	}
}

// number returns v as the inventory number the size fields take: a pointer to it.
func number(v float64) *float64 {
	return &v
}

// sourcesSections returns the body lines of each "### <name>" section of the
// SOURCES.md beside the snapshot, by the heading's name: the lines under it, up to
// the next heading of the same or a higher level.
func sourcesSections(t *testing.T) map[string][]string {
	t.Helper()

	data, err := os.ReadFile(sourcesFile)
	if err != nil {
		t.Fatalf("reading %s: %v", text.Quote(sourcesFile), err)
	}

	sections := make(map[string][]string)

	current := ""

	for _, line := range strings.Split(string(data), "\n") {
		if name, ok := strings.CutPrefix(line, "### "); ok {
			current = strings.TrimSpace(name)
			sections[current] = nil

			continue
		}

		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ") {
			current = ""

			continue
		}

		if current != "" {
			sections[current] = append(sections[current], line)
		}
	}

	return sections
}

// hasNoStoragePriceMarker reports whether body, the lines of one offering's
// section of SOURCES.md, carries the convention's bullet: a line whose text, after
// the Markdown bullet and any leading bold markers, begins with
// noStoragePriceMarker.
func hasNoStoragePriceMarker(body []string) bool {
	for _, line := range body {
		rest := strings.TrimSpace(line)
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "-"))

		for strings.HasPrefix(rest, "**") {
			rest = strings.TrimPrefix(rest, "**")
		}

		if strings.HasPrefix(rest, noStoragePriceMarker) {
			return true
		}
	}

	return false
}

// regionsListed formats the regions prices apply to, one entry per price, for an
// error message.
func regionsListed(prices []catalog.Price) string {
	listed := make([]string, len(prices))
	for i, p := range prices {
		if len(p.Regions) == 0 {
			listed[i] = "every region"

			continue
		}

		listed[i] = strings.Join(p.Regions, ", ")
	}

	return strings.Join(listed, "; ")
}

// TestCatalogGolden pins the whole parsed catalog against a hand-written golden
// file: the snapshot header and its default region, then every offering with its
// categories, capabilities, regions, plans and prices, in the order and canonical
// form the domain gives them. It is a change detector for the snapshot data and
// for the parsing, so a refresh of catalog.json has to update this file in the
// same review, and a price may not change unnoticed.
func TestCatalogGolden(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	testutil.Golden(t, goldenFile, dump(cat))

	// The embed and the shared reader must agree: the golden file is the
	// specification of both, and the analysis may reach the catalog either way.
	data, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatalf("reading the embedded catalog %s: %v", text.Quote(catalogFile), err)
	}

	parsed, err := snapshot.Parse(data)
	if err != nil {
		t.Fatalf("snapshot.Parse() of %s returned unexpected error: %v", text.Quote(catalogFile), err)
	}

	if diff := cmp.Diff(string(dump(cat)), string(dump(parsed.Catalog))); diff != "" {
		t.Errorf("snapshot.Parse() of the embedded file differs from Catalog() (-Catalog +Parse):\n%s", diff)
	}
}

// dump renders cat as the golden file's lines: a header with where and when the
// data was taken and the region estimates use, then one block per offering,
// sorted by name: the offering's line, then one line per plan and one per storage
// price. Every list is comma-separated and an empty one is shown as "-", the
// plans and storage prices included. Lines below an offering are indented so the
// file reads as blocks. Everything comes from slices in the domain's canonical
// order, so the file is the same on every run.
func dump(cat catalog.Catalog) []byte {
	var b strings.Builder

	snapshot := cat.Snapshot()

	fmt.Fprintf(&b, "target: %s\n", cat.Target())
	fmt.Fprintf(&b, "sourceURL: %s\n", snapshot.SourceURL)
	fmt.Fprintf(&b, "retrievedOn: %s\n", snapshot.RetrievedOn.UTC().Format(time.DateOnly))
	fmt.Fprintf(&b, "defaultRegion: %s\n", snapshot.DefaultRegion)

	for _, o := range cat.Offerings() {
		fmt.Fprintf(&b, "%s | %s | %s | %s\n",
			o.Name, join(o.Categories), join(o.Capabilities), join(o.Regions))

		if len(o.Plans) == 0 {
			b.WriteString("  plans: -\n")
		}

		for _, p := range o.Plans {
			fmt.Fprintf(&b, "  plan: %s | %d vcpu | %s GiB | %d GiB storage | %s\n",
				p.Name, p.VCPUs, gib(p.MemoryMilliGiB), p.IncludedStorageGiB, joinPrices(p.Prices))
		}

		if len(o.StoragePrices) == 0 {
			b.WriteString("  storage prices: -\n")
		}

		for _, p := range o.StoragePrices {
			fmt.Fprintf(&b, "  storage price: %s\n", formatPrice(p))
		}
	}

	return []byte(b.String())
}

// joinPrices formats prices as the golden file shows them on a plan's line: each
// price as formatPrice renders it, in the domain's order, separated by "; ", or
// "-" when there are none.
func joinPrices(prices []catalog.Price) string {
	if len(prices) == 0 {
		return "-"
	}

	rendered := make([]string, len(prices))
	for i, p := range prices {
		rendered[i] = formatPrice(p)
	}

	return strings.Join(rendered, "; ")
}

// formatPrice renders a monthly list price as "<amount> <currency> @ <regions>",
// with the amount as catalog.Amount.String formats it and the regions
// comma-separated. A price that lists no region applies to every region of its
// offering, and the "-" says so.
func formatPrice(p catalog.Price) string {
	return fmt.Sprintf("%s %s @ %s", p.Amount, p.Currency, join(p.Regions))
}

// gib formats milliGiB, a memory size in thousandths of a GiB as the domain holds
// it, as a decimal number of GiB with trailing zeros dropped from the fraction:
// 8000 is "8", 7500 is "7.5". A plan's memory is always positive: the domain
// refuses anything below one thousandth of a GiB.
func gib(milliGiB int64) string {
	whole, frac := milliGiB/1_000, milliGiB%1_000

	digits := strings.TrimRight(fmt.Sprintf("%03d", frac), "0")
	if digits == "" {
		return fmt.Sprint(whole)
	}

	return fmt.Sprintf("%d.%s", whole, digits)
}

// join formats values as a comma-separated list, or "-" when there are none.
func join[S ~string](values []S) string {
	if len(values) == 0 {
		return "-"
	}

	strs := make([]string, len(values))
	for i, v := range values {
		strs[i] = string(v)
	}

	return strings.Join(strs, ", ")
}

// TestRegions pins the declared EU region list the reviewer checks against
// Scaleway's published regions, and that callers get a copy: a driving adapter
// cannot change what the adapter declares.
func TestRegions(t *testing.T) {
	t.Parallel()

	want := wantRegions()

	got := scaleway.Regions()
	if !slices.Equal(got, want) {
		t.Fatalf("Regions() = %v, want %v", got, want)
	}

	for i := range got {
		got[i] = "tampered"
	}

	if again := scaleway.Regions(); !slices.Equal(again, want) {
		t.Errorf("Regions() after changing the returned slice = %v, want %v: the result is not a copy", again, want)
	}
}

// TestComputeCategoriesSeparateVirtualMachinesFromBareMetal pins the taxonomy split
// issue #187 asks for: compute.vm is a virtual machine only, and a dedicated
// physical server is compute.baremetal. Elastic Metal is a bare-metal server, so
// it must not tie with Instances for a VM workload and win the tie by name. The
// category values are written out as catalog.json holds them: an offering's
// categories are snapshot data, not a reference to a Go constant.
func TestComputeCategoriesSeparateVirtualMachinesFromBareMetal(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	tests := []struct {
		name     string
		category taxonomy.Category
		want     []string
	}{
		{
			name:     "virtual machines",
			category: taxonomy.Category("compute.vm"),
			want:     []string{"Instances"},
		},
		{
			name:     "bare metal servers",
			category: taxonomy.Category("compute.baremetal"),
			want:     []string{"Elastic Metal"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got []string

			for _, o := range cat.Offerings() {
				if slices.Contains(o.Categories, tc.category) {
					got = append(got, o.Name)
				}
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("offerings serving %s mismatch (-want +got):\n%s", text.Quote(string(tc.category)), diff)
			}
		})
	}
}

// newResource builds r the way an inventory holds it, so the domain gets the
// canonical form: an inventory validates and normalises its resources, and
// mapping.Map expects one that went through it.
func newResource(t *testing.T, category taxonomy.Category, capabilities ...taxonomy.Capability) inventory.Resource {
	t.Helper()

	r := inventory.Resource{
		ID:           "res-example",
		RawType:      "AWS::Example::Resource",
		Category:     category,
		Region:       "eu-west-1",
		Capabilities: capabilities,
	}

	inv, err := inventory.New("aws", []inventory.Resource{r})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv.Resources()[0]
}

// candidateExpectation is what the tests below pin about one candidate: its
// name, its fit, and the capabilities it lacks. Its fields are exported because
// cmp cannot compare a struct with unexported ones.
type candidateExpectation struct {
	Name    string
	Fit     mapping.Fit
	Missing []taxonomy.Capability
}

// candidatesOf lists the candidates a mapping returned, best first. Ties are not
// a field of mapping's answer: two candidates tie when they have the same fit and
// the same number of missing capabilities, and mapping lists them in offering
// name order. Pinning the whole ordered list therefore pins both the ranking and
// the ties.
func candidatesOf(candidates []mapping.Candidate) []candidateExpectation {
	out := make([]candidateExpectation, len(candidates))
	for i, c := range candidates {
		out[i] = candidateExpectation{Name: c.Offering.Name, Fit: c.Fit, Missing: c.Missing}
	}

	return out
}

// checkMapping fails unless got has the wanted fit and exactly the wanted
// candidates, in order.
func checkMapping(t *testing.T, got mapping.Result, wantFit mapping.Fit, want []candidateExpectation) {
	t.Helper()

	r := got.Resource

	if got.Fit != wantFit {
		t.Errorf("Map() of %s with capabilities %v: fit = %q, want %q",
			text.Quote(string(r.Category)), r.Capabilities, got.Fit, wantFit)
	}

	if diff := cmp.Diff(want, candidatesOf(got.Candidates)); diff != "" {
		t.Errorf("Map() of %s with capabilities %v: candidates mismatch (-want +got):\n%s",
			text.Quote(string(r.Category)), r.Capabilities, diff)
	}
}

// TestDatabaseHighAvailabilityScope pins PRD 14 milestone 2's split of high
// availability for Scaleway's Managed Database: the base offering is the
// Standalone tier and lacks high availability, and the High Availability option
// is a variant named after it. The variant survives the loss of a node only, so
// a resource needing zone level is partial on both offerings and pointed at
// neither.
func TestDatabaseHighAvailabilityScope(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	const (
		standalone       = "Managed Database for PostgreSQL and MySQL"
		highAvailability = "Managed Database for PostgreSQL and MySQL High Availability"
	)

	tests := []struct {
		name         string
		capabilities []taxonomy.Capability
		wantFit      mapping.Fit
		want         []candidateExpectation
	}{
		{
			name:         "zone level is offered by neither the node-level variant nor the base offering",
			capabilities: []taxonomy.Capability{taxonomy.ZoneHighAvailability},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: standalone, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
				{Name: highAvailability, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
			},
		},
		{
			name:         "node level is the High Availability variant only",
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: highAvailability, Fit: mapping.Direct},
				{Name: standalone, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkMapping(t, mapping.Map(newResource(t, taxonomy.DatabasePostgreSQL, tc.capabilities...), cat), tc.wantFit, tc.want)
		})
	}
}

// TestKubernetesKapsuleTiers pins PRD 14 milestone 2's tier variant for
// Scaleway's managed Kubernetes: Mutualized is the default tier and the only
// offering for a resource that needs nothing above autoscaling, and the Dedicated
// control plane is a variant named after it that adds node-level high
// availability. Scaleway's control plane is unreachable if the primary zone
// fails, so neither offering claims zone level.
func TestKubernetesKapsuleTiers(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	const (
		mutualized = "Kubernetes Kapsule"
		dedicated  = "Kubernetes Kapsule Dedicated"
	)

	tests := []struct {
		name         string
		capabilities []taxonomy.Capability
		wantFit      mapping.Fit
		want         []candidateExpectation
	}{
		{
			name:         "node level is the Dedicated variant only",
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: dedicated, Fit: mapping.Direct},
				{Name: mutualized, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
			},
		},
		{
			name:    "no capabilities leaves the default tier and its variant tied",
			wantFit: mapping.Direct,
			want: []candidateExpectation{
				{Name: mutualized, Fit: mapping.Direct},
				{Name: dedicated, Fit: mapping.Direct},
			},
		},
		{
			name:         "zone level is offered by neither tier",
			capabilities: []taxonomy.Capability{taxonomy.ZoneHighAvailability},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: mutualized, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
				{Name: dedicated, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkMapping(t, mapping.Map(newResource(t, taxonomy.KubernetesManaged, tc.capabilities...), cat), tc.wantFit, tc.want)
		})
	}
}

// TestNewCategoryBestOfferingAndFit pins PRD #552 milestone 6's mapping
// criterion for the categories the snapshot gains: a resource of each is pointed
// at the offering that takes it over, with fit direct when the offering lists
// every capability the resource lists and partial naming each capability it
// lacks otherwise. Every row goes through mapping.Map on the embedded catalog, so
// the offerings' capabilities, the tiers the snapshot records and the mapping's
// ordering (direct before partial, then by offering name) are pinned together.
//
// The capabilities are the ones a tier variant exists for, and the ones a
// source's resource of the category carries. Scaleway serves a sharded Redis
// cluster; its two-node High Availability topology is a public beta and no
// tier, and the cluster offering carries sharding alone, so a cache that needs
// node level high availability is partial on both offerings (milestone 6's
// decision 1). MongoDB sells a 1-node configuration and a 3-node replica set,
// and documents encryption at rest on neither. The load balancer forwards IPv6
// but not UDP, and neither of its high-availability claims meets the strict
// definition, which is a respawn rather than continuity (decision 3). Topics and
// Events supports no redrive policy, so it has no dead-letter queue, and
// Container Registry states no encryption at rest, so every registry resource is
// partial on it (decision 6).
func TestNewCategoryBestOfferingAndFit(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	const (
		redis                = "Managed Database for Redis"
		redisCluster         = "Managed Database for Redis Cluster"
		mongodb              = "Managed MongoDB"
		mongodbReplicaSet    = "Managed MongoDB Replica Set"
		loadBalancer         = "Load Balancer"
		topicsAndEvents      = "Topics and Events"
		serverlessContainers = "Serverless Containers"
		containerRegistry    = "Container Registry"
	)

	tests := []struct {
		name         string
		category     taxonomy.Category
		capabilities []taxonomy.Capability
		wantFit      mapping.Fit
		want         []candidateExpectation
	}{
		{
			name:     "cache.redis/no capabilities",
			category: taxonomy.CacheRedis,
			wantFit:  mapping.Direct,
			want: []candidateExpectation{
				{Name: redis, Fit: mapping.Direct},
				{Name: redisCluster, Fit: mapping.Direct},
			},
		},
		{
			name:         "cache.redis/sharding",
			category:     taxonomy.CacheRedis,
			capabilities: []taxonomy.Capability{taxonomy.Sharding},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: redisCluster, Fit: mapping.Direct},
				{Name: redis, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.Sharding}},
			},
		},
		{
			name:         "cache.redis/node-high-availability",
			category:     taxonomy.CacheRedis,
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: redis, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
				{Name: redisCluster, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
			},
		},
		{
			name:         "cache.redis/sharding and node-high-availability",
			category:     taxonomy.CacheRedis,
			capabilities: []taxonomy.Capability{taxonomy.Sharding, taxonomy.NodeHighAvailability},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: redisCluster, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
				{Name: redis, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.Sharding}},
			},
		},
		{
			name:     "database.mongodb/no capabilities",
			category: taxonomy.DatabaseMongoDB,
			wantFit:  mapping.Direct,
			want: []candidateExpectation{
				{Name: mongodb, Fit: mapping.Direct},
				{Name: mongodbReplicaSet, Fit: mapping.Direct},
			},
		},
		{
			name:         "database.mongodb/node-high-availability",
			category:     taxonomy.DatabaseMongoDB,
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: mongodbReplicaSet, Fit: mapping.Direct},
				{Name: mongodb, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
			},
		},
		{
			name:         "database.mongodb/encryption-at-rest",
			category:     taxonomy.DatabaseMongoDB,
			capabilities: []taxonomy.Capability{taxonomy.EncryptionAtRest},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: mongodb, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.EncryptionAtRest}},
				{Name: mongodbReplicaSet, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.EncryptionAtRest}},
			},
		},
		{
			name:     "network.loadbalancer/no capabilities",
			category: taxonomy.NetworkLoadBalancer,
			wantFit:  mapping.Direct,
			want:     []candidateExpectation{{Name: loadBalancer, Fit: mapping.Direct}},
		},
		{
			name:         "network.loadbalancer/ipv6",
			category:     taxonomy.NetworkLoadBalancer,
			capabilities: []taxonomy.Capability{taxonomy.IPv6},
			wantFit:      mapping.Direct,
			want:         []candidateExpectation{{Name: loadBalancer, Fit: mapping.Direct}},
		},
		{
			name:         "network.loadbalancer/udp",
			category:     taxonomy.NetworkLoadBalancer,
			capabilities: []taxonomy.Capability{taxonomy.UDP},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: loadBalancer, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.UDP}},
			},
		},
		{
			name:     "messaging.topic/no capabilities",
			category: taxonomy.MessagingTopic,
			wantFit:  mapping.Direct,
			want:     []candidateExpectation{{Name: topicsAndEvents, Fit: mapping.Direct}},
		},
		{
			name:         "messaging.topic/dead-letter-queue",
			category:     taxonomy.MessagingTopic,
			capabilities: []taxonomy.Capability{taxonomy.DeadLetterQueue},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: topicsAndEvents, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.DeadLetterQueue}},
			},
		},
		{
			name:     "serverless.container/no capabilities",
			category: taxonomy.ServerlessContainer,
			wantFit:  mapping.Direct,
			want:     []candidateExpectation{{Name: serverlessContainers, Fit: mapping.Direct}},
		},
		{
			name:         "serverless.container/autoscaling",
			category:     taxonomy.ServerlessContainer,
			capabilities: []taxonomy.Capability{taxonomy.Autoscaling},
			wantFit:      mapping.Direct,
			want:         []candidateExpectation{{Name: serverlessContainers, Fit: mapping.Direct}},
		},
		{
			name:     "registry.container/no capabilities",
			category: taxonomy.RegistryContainer,
			wantFit:  mapping.Direct,
			want:     []candidateExpectation{{Name: containerRegistry, Fit: mapping.Direct}},
		},
		{
			name:         "registry.container/encryption-at-rest",
			category:     taxonomy.RegistryContainer,
			capabilities: []taxonomy.Capability{taxonomy.EncryptionAtRest},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: containerRegistry, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.EncryptionAtRest}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkMapping(t, mapping.Map(newResource(t, tc.category, tc.capabilities...), cat), tc.wantFit, tc.want)
		})
	}
}

// TestNoOfferingForUnservedCategories pins PRD #552 milestone 6's decision 4:
// Scaleway's File Storage is mounted with virtiofs by Scaleway Instances and
// Kapsule, not over NFS, and storage.file is a shared file system several
// machines mount over NFS, so no offering of the snapshot serves it. A file
// share gets fit none, the target-neutral reason naming Scaleway and the
// category, and is listed as a blocker.
func TestNoOfferingForUnservedCategories(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// PRD #552 expects no generally available Scaleway offering to serve this. A
	// later change that adds one updates the PRD with the snapshot, so this test
	// fails first and the review sees it.
	const unserved = taxonomy.StorageFile

	for _, o := range cat.Offerings() {
		if slices.Contains(o.Categories, unserved) {
			t.Errorf("offering %s serves %s, want no Scaleway offering to serve it: File Storage mounts with virtiofs, not over NFS",
				text.Quote(o.Name), text.Quote(string(unserved)))
		}
	}

	r := newResource(t, unserved)

	got := mapping.Map(r, cat)
	if got.Fit != mapping.None {
		t.Errorf("Map() fit = %q, want %q: Scaleway serves no %s",
			got.Fit, mapping.None, text.Quote(string(unserved)))
	}

	wantReasons := []mapping.Reason{{Code: mapping.NoOffering, Target: "Scaleway", Category: unserved}}
	if diff := cmp.Diff(wantReasons, got.Reasons); diff != "" {
		t.Errorf("Map() reasons mismatch (-want +got):\n%s", diff)
	}

	wantSentence := "no offering in the Scaleway catalog for " + string(unserved)
	if reasons := got.Reasons; len(reasons) == 1 && reasons[0].String() != wantSentence {
		t.Errorf("reason = %q, want %q", reasons[0].String(), wantSentence)
	}

	inv, err := inventory.New("aws", []inventory.Resource{r})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	gotBlockers := assessment.Assess(inv, cat).Blockers
	wantBlockers := []assessment.Blocker{{ResourceID: r.ID, Reason: wantReasons[0]}}
	if diff := cmp.Diff(wantBlockers, gotBlockers); diff != "" {
		t.Errorf("Assess() blockers mismatch (-want +got):\n%s", diff)
	}
}

// TestServerlessFunctionsIPv6 pins PRD 14's strict reading of ipv6: it means
// connectivity in both directions, and Scaleway documents Serverless Functions as
// reachable over IPv6 only. The offering therefore lists ipv6 for neither
// direction, and a function that depends on it is partial.
func TestServerlessFunctionsIPv6(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	got := mapping.Map(newResource(t, taxonomy.ServerlessFunction, taxonomy.IPv6), cat)

	checkMapping(t, got, mapping.Partial, []candidateExpectation{
		{Name: "Serverless Functions", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.IPv6}},
	})
}

// TestNoServerlessSQLDatabaseOffering pins PRD 14's decision that Scaleway
// Serverless SQL Database leaves the snapshot: it is PostgreSQL-compatible with
// documented differences, so it does not fully serve database.postgresql and must
// not stand in for a PostgreSQL database, even one whose only capability is
// autoscaling. Managed Database does not autoscale, so that database is partial on
// the two tiers that do serve the category.
func TestNoServerlessSQLDatabaseOffering(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	const removed = "Serverless SQL Database"

	for _, o := range cat.Offerings() {
		if o.Name == removed {
			t.Errorf("the catalog lists an offering called %s, want none: it is not fully compatible with %s",
				text.Quote(o.Name), text.Quote(string(taxonomy.DatabasePostgreSQL)))
		}
	}

	got := mapping.Map(newResource(t, taxonomy.DatabasePostgreSQL, taxonomy.Autoscaling), cat)

	checkMapping(t, got, mapping.Partial, []candidateExpectation{
		{Name: "Managed Database for PostgreSQL and MySQL", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.Autoscaling}},
		{Name: "Managed Database for PostgreSQL and MySQL High Availability", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.Autoscaling}},
	})

	for _, c := range got.Candidates {
		if c.Offering.Name == removed {
			t.Errorf("Map() lists %s as a candidate, want none: it does not fully serve the category",
				text.Quote(c.Offering.Name))
		}
	}
}

// TestHighAvailabilityIsScopedAndStrictlyClaimed pins PRD 14's strict-definition
// rule: Object Storage claims neither high-availability level, because Scaleway
// documents that it rebuilds the data after a whole-zone failure, which is
// recovery, not continuity without manual failover. That every offering is scoped
// is no longer this test's job: ADR 0011 retired the unscoped value, and
// catalog.New refuses a snapshot that lists it (pinned in
// internal/domain/catalog/catalog_test.go), so no offering here can carry it.
func TestHighAvailabilityIsScopedAndStrictlyClaimed(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	var objectStorage catalog.Offering
	for _, o := range cat.Offerings() {
		if o.Name == "Object Storage" {
			objectStorage = o
		}
	}

	if objectStorage.Name == "" {
		t.Fatalf("the catalog has no offering called %s: the strict-definition rule below assumes it", text.Quote("Object Storage"))
	}

	for _, capability := range []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.ZoneHighAvailability} {
		if objectStorage.Has(capability) {
			t.Errorf("offering %s lists %s, want neither high-availability level: rebuilding the data after a zone failure is recovery, not continuity",
				text.Quote(objectStorage.Name), text.Quote(string(capability)))
		}
	}
}

// TestCatalogHonoursCancelledContext pins that a cancelled run reads nothing.
func TestCatalogHonoursCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := scaleway.New().Catalog(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Catalog() with a cancelled context returned error %v, want one wrapping %v", err, context.Canceled)
	}

	if strings.ContainsAny(err.Error(), "\r\n") {
		t.Errorf("Catalog() error = %q, want a single line", err.Error())
	}

	// PRD 11: no error says "OVHcloud" or "UpCloud" when the target is Scaleway.
	// The other adapters' own tests pin the other directions.
	for _, other := range []string{"OVHcloud", "UpCloud"} {
		if strings.Contains(err.Error(), other) {
			t.Errorf("Catalog() error = %q, want no other target's name in it, but it names %q", err.Error(), other)
		}
	}
}

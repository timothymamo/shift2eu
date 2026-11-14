// Package upcloud_test pins the UpCloud catalog adapter: the target contract in
// internal/adapter/target/targettest, the snapshot header the catalog reports, the
// plans, prices and fixed monthly fees the snapshot records and the default region
// they estimate in, the monthly billing cap of its server and node plans (ADR 0029)
// and the estimate that cap gives through the embedded catalog, the rule the
// multi-node database prices follow from the single-node ones, a golden change
// detector for the parsed catalog, the declared EU zones, that a
// cancelled run reads nothing, that no error of one target names another, and the
// verdicts PRD 77 asks of the data: categories UpCloud does not serve are a
// blocker, no offering claims zone-level high availability, and every capability
// that holds only on some plans lives on a tier variant named after that plan,
// with the base offering not listing it (ADR 0011). The snapshot document format
// itself, its strictness, its errors and the SOURCES.md rule are pinned once, in
// the shared reader's own tests (internal/adapter/target/snapshot). It is
// black-box: only the package's exported API is used.
package upcloud_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/target/snapshot"
	"github.com/timothymamo/shift2eu/internal/adapter/target/targettest"
	"github.com/timothymamo/shift2eu/internal/adapter/target/upcloud"
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
var _ analysis.CatalogSource = upcloud.New()

// The catalog the adapter embeds, and the golden file its parsed form renders to.
// Both are relative to this package's directory.
const (
	catalogFile = "catalog.json"
	goldenFile  = "testdata/catalog.golden"
)

// wantRegions is the EU zone list catalog.json declares, written out here so the
// test cannot agree with a wrong declaration by construction: the contract
// expectations below and TestRegions compare against this list, never against
// what Regions() returns. A zone is one UpCloud data centre, and PRD 77 keeps the
// catalog to zones in EU member states, in the order the file lists them.
func wantRegions() []string {
	return []string{"de-fra1", "dk-cph1", "es-mad1", "fi-hel1", "fi-hel2", "nl-ams1", "pl-waw1", "se-sto1"}
}

// nonEUZones are UpCloud zones outside the EU: London and Stavanger, which PRD 77
// leaves out with the United Kingdom and Norway, and the Americas and
// Asia-Pacific. The guard is readable on purpose: it names what a refresh must
// not add.
var nonEUZones = []string{"uk-lon1", "no-svg1", "us-nyc1", "us-chi1", "us-sjo1", "sg-sin1", "au-syd1"}

// objectStorageZones are the zones that host one of UpCloud's Managed Object
// Storage regions: EUROPE-1 in fi-hel2, EUROPE-2 in de-fra1, EUROPE-3 in se-sto1
// and EUROPE-4 in dk-cph1. The snapshot places Object Storage by the zone hosting
// each region, the way PRD 77 decides, so it is the one offering that is not in
// every declared zone.
func objectStorageZones() []string {
	return []string{"de-fra1", "dk-cph1", "fi-hel2", "se-sto1"}
}

// TestCatalogContract runs the target contract suite every target adapter must
// pass: the display name, every offering only in a declared EU zone, the six
// categories PRD 77 requires, the same catalog on every call, and cancellation.
func TestCatalogContract(t *testing.T) {
	t.Parallel()

	targettest.Run(t, upcloud.New(), targettest.Expectations{
		Target:        "UpCloud",
		Regions:       wantRegions(),
		DefaultRegion: "de-fra1",
		Categories: []taxonomy.Category{
			taxonomy.ComputeVM,
			taxonomy.KubernetesManaged,
			taxonomy.StorageObject,
			taxonomy.StorageBlock,
			taxonomy.DatabasePostgreSQL,
			taxonomy.DatabaseMySQL,
		},
	})
}

// catalogOf returns the catalog a fresh source serves.
func catalogOf(t *testing.T) catalog.Catalog {
	t.Helper()

	cat, err := upcloud.New().Catalog(t.Context())
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

	snap := catalogOf(t).Snapshot()

	// The prices were read from the maintainer's saved browser copy of the pricing
	// page, because upcloud.com refuses automated clients outside /docs/ (403). So
	// the snapshot cites that page exactly, not the documentation root it cited
	// before prices, which no price comes from (ADR 0024).
	const wantSourceURL = "https://upcloud.com/pricing/"
	if snap.SourceURL != wantSourceURL {
		t.Errorf("Snapshot().SourceURL = %s, want %s",
			text.Quote(snap.SourceURL), text.Quote(wantSourceURL))
	}

	// The date the prices were taken, not the date the catalog capabilities were
	// last refreshed (ADR 0024).
	wantRetrievedOn := time.Date(2026, time.November, 13, 0, 0, 0, 0, time.UTC)
	if !snap.RetrievedOn.Equal(wantRetrievedOn) {
		t.Errorf("Snapshot().RetrievedOn = %s, want %s",
			snap.RetrievedOn.Format(time.RFC3339), wantRetrievedOn.Format(time.RFC3339))
	}

	if got := snap.RetrievedOn.Location(); got != time.UTC {
		t.Errorf("Snapshot().RetrievedOn location = %v, want time.UTC", got)
	}
}

// TestCatalogPlansAndPrices pins PRD #190 milestone 5: the snapshot records what an
// estimate needs (ADR 0024). Every offering of a category priced by plan sells at
// least one plan, every offering of a category priced per GiB of storage lists at
// least one storage price, every price is a positive monthly amount in EUR, and
// every offering available in the default region can be priced there, so an
// estimate for that region never borrows another region's price and never reads a
// missing price as zero. An offering that sells plans for servers or clusters
// also lists a storage price, so the estimate can price the disk above what the
// plan includes from the same offering. leftOut names the offerings those rules
// are waived for, with the reason, because the snapshot cannot express their price
// as a plan or a storage price; it is empty today and an entry is a finding to
// explain in SOURCES.md. extraStorageLeftOut waives that storage-price rule
// alone for an offering whose plans must still be pinned. A waiver only holds while
// the rule it waives would still fail: an entry that names no offering of the
// catalog, or that waives a rule the data now meets, fails the test, so a refresh
// cannot leave a waiver silently in place.
func TestCatalogPlansAndPrices(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// The zone estimates use when the user names none: de-fra1 hosts every
	// offering in the snapshot, Managed Object Storage included (EUROPE-2 lives
	// there), so no offering is unpriceable in the default region, and it is the
	// existing default.
	const wantRegion = "de-fra1"
	if got := cat.Snapshot().DefaultRegion; got != wantRegion {
		t.Errorf("Snapshot().DefaultRegion = %s, want %s", text.Quote(got), text.Quote(wantRegion))
	}

	// planCategories are the categories PRD #190 prices by plan: a server or a
	// managed cluster runs on one, and so does a managed database node. UpCloud
	// sells no bare metal, so compute.baremetal is absent (ADR 0023). A database
	// plan's price is the price of one node, because ADR 0025 counts database
	// nodes in size.count and the estimate multiplies the plan price by it.
	planCategories := []taxonomy.Category{
		taxonomy.ComputeVM,
		taxonomy.KubernetesManaged,
		taxonomy.DatabasePostgreSQL,
		taxonomy.DatabaseMySQL,
	}

	// storageCategories are the categories PRD #190 prices per GiB of storage
	// kept: block volumes, objects, and the storage a managed database uses.
	storageCategories := []taxonomy.Category{
		taxonomy.StorageBlock,
		taxonomy.StorageObject,
		taxonomy.DatabasePostgreSQL,
		taxonomy.DatabaseMySQL,
	}

	// planStorageCategories are the categories whose plans come with disk that a
	// resource may outgrow: a server and a managed cluster, but not a managed
	// database, whose storage volume is priced on its own offerings already.
	planStorageCategories := []taxonomy.Category{
		taxonomy.ComputeVM,
		taxonomy.KubernetesManaged,
	}

	// leftOut exempts an offering from the coverage and plan-storage rules, never
	// from the price rules, and gives the reason. It is empty today: an entry is a
	// finding to explain in this test and in SOURCES.md, not a way past a refresh.
	leftOut := map[string]string{}

	// kubernetesWorkerStorageLeftOut is why both managed Kubernetes offerings are
	// waived from the extra-storage rule: the reason is the same for both.
	const kubernetesWorkerStorageLeftOut = "UpCloud publishes no per-GiB price for worker-node storage, " +
		"and its node-group example does not match a Cloud Server storage tier; see SOURCES.md"

	// extraStorageLeftOut waives only the extra-storage rule, never the plan rule or
	// the price rules, and gives the reason. The managed Kubernetes offerings keep
	// the plans their nodes run on pinned, because the snapshot does price those;
	// only the storage above what a plan includes has no price the snapshot can
	// record. It is a finding to explain in this test and in SOURCES.md, not a way
	// past a refresh.
	extraStorageLeftOut := map[string]string{
		"Managed Kubernetes":            kubernetesWorkerStorageLeftOut,
		"Managed Kubernetes Production": kubernetesWorkerStorageLeftOut,
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

		// PRD #190 estimates a resource as the plan price times its count and the
		// hours it runs, plus the storage above what the plan includes, priced per
		// GiB. That second summand can only come from a storage price of the same
		// offering as the plan, so an offering that sells plans servers and
		// clusters run on needs both, or every sized resource asking for more disk
		// than the plan includes would be "not estimated".
		extraStorageReason, extraStorageExempt := extraStorageLeftOut[o.Name]
		if extraStorageExempt {
			t.Logf("offering %s is left out of the extra-storage rule: %s", text.Quote(o.Name), extraStorageReason)
		}

		if !exempt && !extraStorageExempt && servesAny(o, planStorageCategories) && len(o.Plans) > 0 && len(o.StoragePrices) == 0 {
			t.Errorf("offering %s lists plans but no storage price, want at least one: the monthly estimate prices the storage above a plan's included storage per GiB, from the same offering",
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

	// A waiver is a finding, not a way past a refresh: it is only valid while the
	// rule it waives would still fail for that offering. A refresh renames or prices
	// offerings, and a waiver that outlived its finding would silently skip a rule the
	// data now meets, so each entry is checked against the catalog instead of trusted.
	// The names are iterated in sorted order, so a failure names the same offering on
	// every run whatever the map order.
	byName := make(map[string]catalog.Offering, len(cat.Offerings()))
	for _, o := range cat.Offerings() {
		byName[o.Name] = o
	}

	for _, name := range slices.Sorted(maps.Keys(leftOut)) {
		o, found := byName[name]
		if !found {
			t.Errorf("leftOut names offering %s, want an offering of the catalog: the waiver names no offering and should be removed",
				text.Quote(name))

			continue
		}

		// leftOut waives the plan rule and the storage-price rule, so it is still
		// needed only while one of them would fail: the offering serves a category
		// priced by plan or per GiB of storage and lists neither a plan nor a storage
		// price. Once it lists either, remove the entry and its reason.
		needsWaiver := (servesAny(o, planCategories) || servesAny(o, storageCategories)) &&
			len(o.Plans) == 0 && len(o.StoragePrices) == 0
		if !needsWaiver {
			t.Errorf("offering %s is now priced; remove its waiver from leftOut: it serves %v and lists %d plans and %d storage prices",
				text.Quote(name), o.Categories, len(o.Plans), len(o.StoragePrices))
		}
	}

	for _, name := range slices.Sorted(maps.Keys(extraStorageLeftOut)) {
		o, found := byName[name]
		if !found {
			t.Errorf("extraStorageLeftOut names offering %s, want an offering of the catalog: the waiver names no offering and should be removed",
				text.Quote(name))

			continue
		}

		// The extra-storage rule fails only for an offering that lists a plan and no
		// storage price, so the waiver is still needed only while the offering lists
		// plans and no storage price. Once it lists a storage price, remove the entry
		// and its reason.
		needsWaiver := len(o.Plans) > 0 && len(o.StoragePrices) == 0
		if !needsWaiver {
			t.Errorf("offering %s is now priced; remove its waiver from extraStorageLeftOut: it lists %d plans and %d storage prices",
				text.Quote(name), len(o.Plans), len(o.StoragePrices))
		}
	}
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

// TestCatalogFixedFees pins the fixed monthly fee per Kubernetes cluster the
// snapshot records (ADR 0026, PRD #190 milestone 5): UpCloud charges the
// Production control plane per cluster in every zone, and includes the
// Development control plane of the base offering, which its pricing page lists
// as "€0/mo" and its billing page as "Free of charge". The Production plan is
// billed by the hour, and its pricing page's "€60/mo" is a 30-day estimate
// ("Monthly prices are estimates based on a 30-day month"), so the fee is
// converted the way ADR 0024's prices are, at 730 hours: 60 × 730 / 720 = 60.8333.
// The free fee is recorded as 0.00 and not left out: the boolean of FixedFee
// tells a free control plane from one the snapshot has no fee for. Every other
// offering charges no fixed fee, so no estimate adds one where UpCloud publishes
// none.
func TestCatalogFixedFees(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// feePerOffering is the control-plane fee each offering charges per cluster in
	// every one of its zones: the Production plan's hourly fee converted to
	// 60.8333 EUR, and 0.00 where the base offering stands for the Development
	// plan, which UpCloud includes.
	const (
		productionControlPlane  = catalog.Amount(608_333)
		developmentControlPlane = catalog.Amount(0)
	)

	feePerOffering := map[string]catalog.Amount{
		"Managed Kubernetes Production": productionControlPlane,
		"Managed Kubernetes":            developmentControlPlane,
	}

	for _, o := range cat.Offerings() {
		want, charges := feePerOffering[o.Name]

		if !charges {
			if o.FixedFees != nil {
				t.Errorf("offering %s lists fixed fees %v, want none: UpCloud publishes a control-plane fee for its Kubernetes offerings only",
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

		// The fee is the same in every zone of the offering, so the regions come
		// from the catalog: a zone added to the offering carries the fee too, and a
		// snapshot that leaves the fee out there fails here instead of reading as a
		// free control plane in that zone (ADR 0026).
		for _, region := range o.Regions {
			got, ok := o.FixedFee(region)
			if !ok {
				t.Errorf("offering %s reports no fixed fee in region %s, want %s",
					text.Quote(o.Name), text.Quote(region), want)

				continue
			}

			if got != want {
				t.Errorf("offering %s: FixedFee(%s) = %s, want %s",
					text.Quote(o.Name), text.Quote(region), got, want)
			}
		}

		// And the fees name those regions and no others: an every-region fee names
		// all of them, a per-zone list holds exactly these.
		if diff := cmp.Diff(o.Regions, feeRegions(o)); diff != "" {
			t.Errorf("offering %s: the regions its fixed fees name are not its regions (-want +got):\n%s",
				text.Quote(o.Name), diff)
		}
	}

	// The expectation names the two Kubernetes offerings alone, so an entry that
	// outlived a rename is a finding rather than a silent pass. The names are
	// iterated in sorted order, so a failure names the same offering on every run
	// whatever the map order.
	byName := make(map[string]catalog.Offering, len(cat.Offerings()))
	for _, o := range cat.Offerings() {
		byName[o.Name] = o
	}

	for _, name := range slices.Sorted(maps.Keys(feePerOffering)) {
		if _, found := byName[name]; !found {
			t.Errorf("feePerOffering names offering %s, want an offering of the catalog: the entry names no offering and should be removed",
				text.Quote(name))
		}
	}
}

// TestCatalogPlanBillingCapHours pins ADR 0029's data: UpCloud bills Cloud
// Servers and Kubernetes nodes by the hour, capped at 672 hours (24 h × 28 days)
// a month, so the three offerings whose plans are server or node plans carry
// that cap. Its databases, block storage and object storage are charged by the
// hour with no cap, their monthly figures being for a 30-day, 730-hour month, so
// they carry none. The map names every offering, so a refresh that adds one
// cannot slip past the rule.
func TestCatalogPlanBillingCapHours(t *testing.T) {
	t.Parallel()

	want := map[string]int64{
		"Cloud Servers":                 672,
		"Managed Kubernetes":            672,
		"Managed Kubernetes Production": 672,
		"Block Storage":                 0,
		"Block Storage Encrypted":       0,
		"Managed Object Storage":        0,
		"Managed PostgreSQL":            0,
		"Managed PostgreSQL Multi-node": 0,
		"Managed MySQL":                 0,
		"Managed MySQL Multi-node":      0,
	}

	offerings := catalogOf(t).Offerings()
	if len(offerings) != len(want) {
		t.Fatalf("Catalog.Offerings() returned %d offerings, want %d: every offering needs a billing-cap expectation",
			len(offerings), len(want))
	}

	for _, o := range offerings {
		hours, found := want[o.Name]
		if !found {
			t.Errorf("offering %s has no plan billing cap expectation, want one: the catalog changed",
				text.Quote(o.Name))

			continue
		}

		if o.PlanBillingCapHours != hours {
			t.Errorf("offering %s has PlanBillingCapHours %d, want %d",
				text.Quote(o.Name), o.PlanBillingCapHours, hours)
		}
	}
}

// TestBillingCapEstimateThroughEmbeddedCatalog pins ADR 0029's acceptance on the
// snapshot the adapter embeds: UpCloud bills one Cloud Servers server for at most
// 672 hours a month, so a server that runs half of that is estimated at half its
// plan's monthly price, and one that runs the cap, or the 730 hours of an average
// month, at all of it. The domain rule and the snapshot data are pinned apart, in
// internal/domain/estimate and TestCatalogPlanBillingCapHours; this joins the two
// through the real embedded catalog, mapped and estimated the way an assessment
// does it.
func TestBillingCapEstimateThroughEmbeddedCatalog(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)
	region := cat.Snapshot().DefaultRegion

	// STARTER-1xCPU-1GB is the cheapest Cloud Servers plan that covers the one
	// vCPU and one GiB the resource has, at 3.00 EUR a month, and it includes the
	// 10 GiB the resource has, so the plan price is the whole estimate.
	tests := []struct {
		name        string
		hours       float64
		wantMonthly string
		wantExact   string
	}{
		{
			name:        "336 hours is half the 672-hour cap, so half the plan price",
			hours:       336,
			wantMonthly: "1.50",
			wantExact:   "3/2",
		},
		{
			name:        "672 hours, the cap, is the whole plan price",
			hours:       672,
			wantMonthly: "3.00",
			wantExact:   "3",
		},
		{
			name:        "730 hours, an average month, is the whole plan price",
			hours:       730,
			wantMonthly: "3.00",
			wantExact:   "3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := inventory.Resource{
				ID:       "server-example",
				RawType:  "AWS::EC2::Instance",
				Category: taxonomy.ComputeVM,
				Region:   "eu-west-1",
				Size: &inventory.Size{
					VCPUs:      number(1),
					MemoryGiB:  number(1),
					StorageGiB: number(10),
					Count:      countOf(1),
				},
				Usage: &inventory.Usage{HoursPerMonth: number(tc.hours)},
			}

			inv, err := inventory.New("aws", []inventory.Resource{r})
			if err != nil {
				t.Fatalf("inventory.New() returned unexpected error: %v", err)
			}

			got := estimate.For(mapping.Map(inv.Resources()[0], cat), region)

			if !got.Estimated {
				t.Fatalf("For() Estimated = false, reason %s, want an estimate on a Cloud Servers plan",
					text.Quote(string(got.Reason)))
			}

			if got.Offering != "Cloud Servers" {
				t.Errorf("For() Offering = %s, want %s", text.Quote(got.Offering), text.Quote("Cloud Servers"))
			}

			const wantPlan = "STARTER-1xCPU-1GB"
			if got.Plan == nil || got.Plan.Name != wantPlan {
				t.Fatalf("For() Plan = %v, want %s", got.Plan, text.Quote(wantPlan))
			}

			if want := tc.wantMonthly; got.Monthly.String() != want {
				t.Errorf("For() Monthly.String() = %q, want %q", got.Monthly.String(), want)
			}

			if want := tc.wantExact; got.Monthly.Exact() != want {
				t.Errorf("For() Monthly.Exact() = %q, want %q", got.Monthly.Exact(), want)
			}

			// The plan is the whole estimate: it includes the resource's 10 GiB of
			// storage and Cloud Servers charges no fixed monthly fee.
			if diff := cmp.Diff(estimate.Breakdown{Plan: got.Monthly}, got.Breakdown); diff != "" {
				t.Errorf("For() Breakdown mismatch, want the plan alone (-want +got):\n%s", diff)
			}
		})
	}
}

// number returns v as the inventory number the size and usage fields take: a
// pointer to it.
func number(v float64) *float64 {
	return &v
}

// countOf returns n as the count of a resource's size.
func countOf(n int) *int {
	return &n
}

// feeRegions lists, sorted, the regions o's fixed fees name, with an every-region
// fee expanded to every region of the offering (ADR 0026).
func feeRegions(o catalog.Offering) []string {
	regions := make(map[string]struct{}, len(o.Regions))

	for _, fee := range o.FixedFees {
		if fee.Regions == nil {
			for _, region := range o.Regions {
				regions[region] = struct{}{}
			}

			continue
		}

		for _, region := range fee.Regions {
			regions[region] = struct{}{}
		}
	}

	return slices.Sorted(maps.Keys(regions))
}

// TestMultiNodeDatabasePrices pins the rule the snapshot records for UpCloud's
// multi-node database plans (maintainer decision 2026-11-14, in SOURCES.md): a
// plan of *Managed MySQL Multi-node* or *Managed PostgreSQL Multi-node* is priced
// per node at two nodes' list price with the documented 5% multi-node discount,
// that is the stored amount of the same-named single-node plan times 95/100,
// rounded half up to ADR 0024's four decimal places. The golden file pins those
// amounts too, but it is regenerated with -update on every refresh, so a refresh
// that changes a single-node price and forgets its multi-node counterpart passes
// there; this test derives each multi-node amount from the single-node one the
// snapshot stores and cannot. It pins what a multi-node plan is besides its
// price: the same plan names, the same vCPUs, memory and included storage, and a
// per-GiB storage price for two nodes, twice its single-node offering's.
func TestMultiNodeDatabasePrices(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// The discount is 95 of 100, applied to catalog.Amount's integer
	// ten-thousandths, so the arithmetic below never uses a float (ADR 0024).
	const (
		discountNumerator   = 95
		discountDenominator = 100
	)

	// Each row pairs a multi-node database offering with the single-node offering
	// it is priced from. The rows are in this order so a failure reads the same way
	// on every run: UpCloud sells MySQL and PostgreSQL with the same plans.
	tests := []struct {
		multiNode  string
		singleNode string
	}{
		{multiNode: "Managed MySQL Multi-node", singleNode: "Managed MySQL"},
		{multiNode: "Managed PostgreSQL Multi-node", singleNode: "Managed PostgreSQL"},
	}

	for _, tc := range tests {
		t.Run(tc.multiNode, func(t *testing.T) {
			t.Parallel()

			multi := offeringNamed(t, cat, tc.multiNode)
			single := offeringNamed(t, cat, tc.singleNode)

			multiPlans := plansByName(multi)
			singlePlans := plansByName(single)

			// The same set of plans, not just one plan: a multi-node offering that
			// sells a size its single-node counterpart does not (or the other way
			// round) has no price to check against, so it is a finding of its own.
			wantNames := slices.Sorted(maps.Keys(singlePlans))
			gotNames := slices.Sorted(maps.Keys(multiPlans))

			if diff := cmp.Diff(wantNames, gotNames); diff != "" {
				t.Errorf("offering %s: plan names differ from offering %s, want the same set (-single-node +multi-node):\n%s",
					text.Quote(multi.Name), text.Quote(single.Name), diff)
			}

			// Sorted plan names, so the failures below name the plans in the same
			// order on every run, whatever order the catalog holds them in.
			for _, name := range gotNames {
				multiPlan := multiPlans[name]

				singlePlan, found := singlePlans[name]
				if !found {
					t.Errorf("offering %s plan %s: offering %s has no plan of that name, want the same plans in both offerings",
						text.Quote(multi.Name), text.Quote(name), text.Quote(single.Name))

					continue
				}

				// A multi-node plan is the same size as its single-node plan: only the
				// price differs, so a refreshed size that reached one offering alone fails.
				if multiPlan.VCPUs != singlePlan.VCPUs {
					t.Errorf("offering %s plan %s: vCPUs = %d, want %d, the vCPUs of offering %s plan %s",
						text.Quote(multi.Name), text.Quote(name), multiPlan.VCPUs, singlePlan.VCPUs,
						text.Quote(single.Name), text.Quote(name))
				}

				if multiPlan.MemoryMilliGiB != singlePlan.MemoryMilliGiB {
					t.Errorf("offering %s plan %s: memory = %d milliGiB, want %d, the memory of offering %s plan %s",
						text.Quote(multi.Name), text.Quote(name), multiPlan.MemoryMilliGiB, singlePlan.MemoryMilliGiB,
						text.Quote(single.Name), text.Quote(name))
				}

				if multiPlan.IncludedStorageGiB != singlePlan.IncludedStorageGiB {
					t.Errorf("offering %s plan %s: included storage = %d GiB, want %d, the included storage of offering %s plan %s",
						text.Quote(multi.Name), text.Quote(name), multiPlan.IncludedStorageGiB, singlePlan.IncludedStorageGiB,
						text.Quote(single.Name), text.Quote(name))
				}

				if len(multiPlan.Prices) != len(singlePlan.Prices) {
					t.Errorf("offering %s plan %s: %d prices, want %d, one per price of offering %s plan %s",
						text.Quote(multi.Name), text.Quote(name), len(multiPlan.Prices), len(singlePlan.Prices),
						text.Quote(single.Name), text.Quote(name))

					continue
				}

				for i, singlePrice := range singlePlan.Prices {
					multiPrice := multiPlan.Prices[i]

					// Half up in integer units: the numerator is at most MaxAmount times
					// 95, far inside int64, and adding half the denominator rounds a
					// remainder of exactly one half away from zero, which is up here
					// because every amount is positive.
					wantAmount := catalog.Amount((int64(singlePrice.Amount)*discountNumerator + discountDenominator/2) / discountDenominator)

					if multiPrice.Amount != wantAmount {
						t.Errorf("offering %s plan %s: prices[%d].amount = %s, want %s: the amount %s of offering %s plan %s times %d/%d, rounded half up to 4 decimal places",
							text.Quote(multi.Name), text.Quote(name), i, multiPrice.Amount, wantAmount, singlePrice.Amount,
							text.Quote(single.Name), text.Quote(name), discountNumerator, discountDenominator)
					}

					if multiPrice.Currency != singlePrice.Currency {
						t.Errorf("offering %s plan %s: prices[%d].currency = %s, want %s, the currency of offering %s plan %s",
							text.Quote(multi.Name), text.Quote(name), i, text.Quote(multiPrice.Currency), text.Quote(singlePrice.Currency),
							text.Quote(single.Name), text.Quote(name))
					}

					if diff := cmp.Diff(singlePrice.Regions, multiPrice.Regions); diff != "" {
						t.Errorf("offering %s plan %s: prices[%d].regions differ from offering %s plan %s, want the same regions (-single-node +multi-node):\n%s",
							text.Quote(multi.Name), text.Quote(name), i, text.Quote(single.Name), text.Quote(name), diff)
					}
				}
			}

			// UpCloud bills a cluster's storage per node: its configuration pages say
			// the per-GiB storage price "applies to each node". They do not say how
			// many nodes a multi-node plan runs, so the snapshot infers the two its
			// plan prices are already quoted for and doubles the per-GiB amount, in
			// catalog.Amount's integer units and without a float (ADR 0024). That
			// overestimates a three-node cluster's storage, which is then estimated a
			// third low, and the snapshot prefers a dearer target to a cheaper one.
			const storageNodeCount = 2

			if len(multi.StoragePrices) != len(single.StoragePrices) {
				t.Errorf("offering %s: %d storage prices, want %d, one per storage price of offering %s",
					text.Quote(multi.Name), len(multi.StoragePrices), len(single.StoragePrices), text.Quote(single.Name))
			}

			for i, singlePrice := range single.StoragePrices {
				if i >= len(multi.StoragePrices) {
					break
				}

				multiPrice := multi.StoragePrices[i]

				// Twice the single-node amount, exactly: 2 x 1014 = 2028, that is
				// 0.2028 EUR per GiB per month.
				wantAmount := singlePrice.Amount * storageNodeCount

				if multiPrice.Amount != wantAmount {
					t.Errorf("offering %s: storagePrices[%d].amount = %s, want %s: the amount %s of offering %s times %d nodes",
						text.Quote(multi.Name), i, multiPrice.Amount, wantAmount, singlePrice.Amount,
						text.Quote(single.Name), storageNodeCount)
				}

				if multiPrice.Currency != singlePrice.Currency {
					t.Errorf("offering %s: storagePrices[%d].currency = %s, want %s, the currency of offering %s",
						text.Quote(multi.Name), i, text.Quote(multiPrice.Currency), text.Quote(singlePrice.Currency),
						text.Quote(single.Name))
				}

				if diff := cmp.Diff(singlePrice.Regions, multiPrice.Regions); diff != "" {
					t.Errorf("offering %s: storagePrices[%d].regions differ from offering %s, want the same regions (-single-node +multi-node):\n%s",
						text.Quote(multi.Name), i, text.Quote(single.Name), diff)
				}
			}
		})
	}
}

// plansByName indexes an offering's plans by name, in one pass, so a plan of the
// other offering is found without a scan per plan.
func plansByName(o catalog.Offering) map[string]catalog.Plan {
	plans := make(map[string]catalog.Plan, len(o.Plans))

	for _, p := range o.Plans {
		plans[p.Name] = p
	}

	return plans
}

// TestCatalogGolden pins the whole parsed catalog against a golden file: every
// offering with its categories, capabilities and zones, and its plans and
// storage prices with the monthly amount, currency and regions each applies to,
// in the order and canonical form the domain gives them. It is a change detector
// for the snapshot data and for the parsing, so a refresh of catalog.json has to
// update this file in the same review. The golden file is generated with -update
// once catalog.json exists; it is not hand-written, so a wrong snapshot cannot
// quietly become the spec.
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
// data was taken, then one line per offering, sorted by name, with every list
// comma-separated and an empty list shown as "-". Under each offering come its
// plans, one line each with the plan's numbers and prices, then its fixed fees,
// one line each per cluster with the fee and the regions it applies to, and then
// its storage prices, so the golden file pins every price the snapshot holds and
// the region it applies to, the fixed monthly fees included. Plans, fees and
// prices are already in the domain's order, so the output never depends on map
// iteration.
func dump(cat catalog.Catalog) []byte {
	var b strings.Builder

	snap := cat.Snapshot()

	fmt.Fprintf(&b, "target: %s\n", cat.Target())
	fmt.Fprintf(&b, "sourceURL: %s\n", snap.SourceURL)
	fmt.Fprintf(&b, "retrievedOn: %s\n", snap.RetrievedOn.UTC().Format(time.DateOnly))

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

		if len(o.FixedFees) == 0 {
			b.WriteString("  fixed fees: -\n")
		}

		for _, fee := range o.FixedFees {
			fmt.Fprintf(&b, "  fixed fee: %s\n", formatPrice(fee))
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

// TestRegions pins the declared EU zone list the reviewer checks against
// UpCloud's locations page, that callers get a copy, and that no offering is
// placed outside that list: no offering lists London, Stavanger or any zone in
// the Americas or Asia-Pacific. It also pins Managed Object Storage to the four
// zones that host one of its regions, the one offering that is not in every zone.
func TestRegions(t *testing.T) {
	t.Parallel()

	want := wantRegions()

	got := upcloud.Regions()
	if !slices.Equal(got, want) {
		t.Fatalf("Regions() = %v, want %v", got, want)
	}

	for _, zone := range got {
		if slices.Contains(nonEUZones, zone) {
			t.Errorf("Regions() declares %s, an UpCloud zone outside the EU: PRD 77 keeps the catalog to EU member states",
				text.Quote(zone))
		}
	}

	for i := range got {
		got[i] = "tampered"
	}

	if again := upcloud.Regions(); !slices.Equal(again, want) {
		t.Errorf("Regions() after changing the returned slice = %v, want %v: the result is not a copy", again, want)
	}

	cat := catalogOf(t)

	for _, o := range cat.Offerings() {
		for _, zone := range o.Regions {
			if !slices.Contains(want, zone) {
				t.Errorf("offering %s is available in zone %s, want one of the declared EU zones %v",
					text.Quote(o.Name), text.Quote(zone), want)
			}
		}
	}

	o := offeringNamed(t, cat, objectStorageName)

	if diff := cmp.Diff(objectStorageZones(), o.Regions); diff != "" {
		t.Errorf("offering %s zones mismatch (-want +got):\n%s", text.Quote(o.Name), diff)
	}
}

// objectStorageName is the one offering PRD 77 places by the zone that hosts each
// of its regions rather than in every zone.
const objectStorageName = "Managed Object Storage"

// TestNoOfferingForUnservedCategories pins PRD 77's criterion that a resource of a
// category UpCloud does not serve gets fit none, the target-neutral reason naming
// UpCloud and the category, and is listed as a blocker. The snapshot serves
// neither serverless functions nor message queues: UpCloud's documentation has no
// FaaS or queue product.
func TestNoOfferingForUnservedCategories(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// PRD 77 expects no generally available UpCloud offering to serve these. A
	// change that adds one updates the PRD with the snapshot, so this test fails
	// first and the review sees it.
	unserved := []taxonomy.Category{taxonomy.ServerlessFunction, taxonomy.MessagingQueue}

	for _, o := range cat.Offerings() {
		for _, category := range o.Categories {
			if slices.Contains(unserved, category) {
				t.Errorf("offering %s serves %s, want no UpCloud offering to serve it",
					text.Quote(o.Name), text.Quote(string(category)))
			}
		}
	}

	for _, category := range unserved {
		t.Run(string(category), func(t *testing.T) {
			t.Parallel()

			r := newResource(t, "res-example", category)

			got := mapping.Map(r, cat)
			if got.Fit != mapping.None {
				t.Errorf("Map() fit = %q, want %q: UpCloud serves no %s",
					got.Fit, mapping.None, text.Quote(string(category)))
			}

			wantReasons := []mapping.Reason{{Code: mapping.NoOffering, Target: "UpCloud", Category: category}}
			if diff := cmp.Diff(wantReasons, got.Reasons); diff != "" {
				t.Errorf("Map() reasons mismatch (-want +got):\n%s", diff)
			}

			wantSentence := "no offering in the UpCloud catalog for " + string(category)
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
		})
	}
}

// TestNoOfferingClaimsZoneHighAvailability pins PRD 77's criterion that no
// offering lists zone-high-availability unless its SOURCES.md section quotes
// documentation that states the service keeps serving through the loss of a whole
// zone or data centre without manual failover (ADR 0011's target rule). Each
// UpCloud zone is one data centre, so two nodes in one zone survive the loss of a
// node, not of the zone, and the list is empty. When a refresh makes it non-empty,
// this test fails first and the review sees the documentation that justifies it.
func TestNoOfferingClaimsZoneHighAvailability(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	var claiming []string

	for _, o := range cat.Offerings() {
		if o.Has(taxonomy.ZoneHighAvailability) {
			claiming = append(claiming, o.Name)
		}
	}

	if diff := cmp.Diff([]string(nil), claiming); diff != "" {
		t.Errorf("offerings claiming %s mismatch (-want +got):\n%s",
			text.Quote(string(taxonomy.ZoneHighAvailability)), diff)
	}
}

// newResource builds r the way an inventory holds it, so the domain gets the
// canonical form: an inventory validates and normalises its resources, and
// mapping.Map expects one that went through it.
func newResource(t *testing.T, id string, category taxonomy.Category, capabilities ...taxonomy.Capability) inventory.Resource {
	t.Helper()

	r := inventory.Resource{
		ID:           id,
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
// the ties, which is what points a resource at a base offering when its variant
// adds nothing the resource needs.
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

// TestTierVariantIsBestOffering pins ADR 0011's tier-variant rule for UpCloud: a
// capability that holds only on some plans is listed on a variant named after that
// plan, and the base offering does not list it. A resource that needs the
// capability is pointed at the variant; one that needs nothing the variant adds is
// pointed at the base offering, which wins the tie by name, and the variant ties
// behind it. A resource that needs zone level is partial on both, because neither
// survives the loss of a data centre, and the base is named.
func TestTierVariantIsBestOffering(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	tests := []struct {
		name         string
		category     taxonomy.Category
		capabilities []taxonomy.Capability
		wantFit      mapping.Fit
		want         []candidateExpectation
	}{
		{
			name:     "block storage needs nothing",
			category: taxonomy.StorageBlock,
			wantFit:  mapping.Direct,
			want: []candidateExpectation{
				{Name: "Block Storage", Fit: mapping.Direct},
				{Name: "Block Storage Encrypted", Fit: mapping.Direct},
			},
		},
		{
			name:         "block storage needs encryption at rest",
			category:     taxonomy.StorageBlock,
			capabilities: []taxonomy.Capability{taxonomy.EncryptionAtRest},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: "Block Storage Encrypted", Fit: mapping.Direct},
				{Name: "Block Storage", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.EncryptionAtRest}},
			},
		},
		{
			name:     "kubernetes needs nothing",
			category: taxonomy.KubernetesManaged,
			wantFit:  mapping.Direct,
			want: []candidateExpectation{
				{Name: "Managed Kubernetes", Fit: mapping.Direct},
				{Name: "Managed Kubernetes Production", Fit: mapping.Direct},
			},
		},
		{
			name:         "kubernetes needs node-level high availability",
			category:     taxonomy.KubernetesManaged,
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: "Managed Kubernetes Production", Fit: mapping.Direct},
				{Name: "Managed Kubernetes", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
			},
		},
		{
			name:         "kubernetes needs autoscaling, which no tier lists",
			category:     taxonomy.KubernetesManaged,
			capabilities: []taxonomy.Capability{taxonomy.Autoscaling},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: "Managed Kubernetes", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.Autoscaling}},
				{Name: "Managed Kubernetes Production", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.Autoscaling}},
			},
		},
		{
			name:     "postgresql needs nothing",
			category: taxonomy.DatabasePostgreSQL,
			wantFit:  mapping.Direct,
			want: []candidateExpectation{
				{Name: "Managed PostgreSQL", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL Multi-node", Fit: mapping.Direct},
			},
		},
		{
			name:         "postgresql needs node-level high availability",
			category:     taxonomy.DatabasePostgreSQL,
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: "Managed PostgreSQL Multi-node", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
			},
		},
		{
			name:         "postgresql needs read replicas",
			category:     taxonomy.DatabasePostgreSQL,
			capabilities: []taxonomy.Capability{taxonomy.ReadReplicas},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: "Managed PostgreSQL Multi-node", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ReadReplicas}},
			},
		},
		{
			name:         "postgresql needs zone-level high availability",
			category:     taxonomy.DatabasePostgreSQL,
			capabilities: []taxonomy.Capability{taxonomy.ZoneHighAvailability},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: "Managed PostgreSQL", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
				{Name: "Managed PostgreSQL Multi-node", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
			},
		},
		{
			name:         "postgresql needs zone-level high availability and read replicas",
			category:     taxonomy.DatabasePostgreSQL,
			capabilities: []taxonomy.Capability{taxonomy.ZoneHighAvailability, taxonomy.ReadReplicas},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: "Managed PostgreSQL Multi-node", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
				{Name: "Managed PostgreSQL", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ReadReplicas, taxonomy.ZoneHighAvailability}},
			},
		},
		{
			name:     "mysql needs nothing",
			category: taxonomy.DatabaseMySQL,
			wantFit:  mapping.Direct,
			want: []candidateExpectation{
				{Name: "Managed MySQL", Fit: mapping.Direct},
				{Name: "Managed MySQL Multi-node", Fit: mapping.Direct},
			},
		},
		{
			name:         "mysql needs node-level high availability",
			category:     taxonomy.DatabaseMySQL,
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: "Managed MySQL Multi-node", Fit: mapping.Direct},
				{Name: "Managed MySQL", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
			},
		},
		{
			name:         "mysql needs read replicas",
			category:     taxonomy.DatabaseMySQL,
			capabilities: []taxonomy.Capability{taxonomy.ReadReplicas},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: "Managed MySQL Multi-node", Fit: mapping.Direct},
				{Name: "Managed MySQL", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ReadReplicas}},
			},
		},
		{
			name:         "mysql needs zone-level high availability",
			category:     taxonomy.DatabaseMySQL,
			capabilities: []taxonomy.Capability{taxonomy.ZoneHighAvailability},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: "Managed MySQL", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
				{Name: "Managed MySQL Multi-node", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
			},
		},
		{
			name:         "mysql needs zone-level high availability and read replicas",
			category:     taxonomy.DatabaseMySQL,
			capabilities: []taxonomy.Capability{taxonomy.ZoneHighAvailability, taxonomy.ReadReplicas},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: "Managed MySQL Multi-node", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
				{Name: "Managed MySQL", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ReadReplicas, taxonomy.ZoneHighAvailability}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkMapping(t, mapping.Map(newResource(t, "res-example", tc.category, tc.capabilities...), cat), tc.wantFit, tc.want)
		})
	}
}

// TestBaseOfferingsOmitVariantCapabilities pins the other half of ADR 0011's
// tier-variant rule: a capability that a tier variant adds is listed on the
// variant alone. If a base offering carried it too, a resource needing nothing
// would tie with the variant for the wrong reason, and the variant would stop
// meaning anything.
func TestBaseOfferingsOmitVariantCapabilities(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	tests := []struct {
		offering     string
		capabilities []taxonomy.Capability
	}{
		{
			offering:     "Block Storage",
			capabilities: []taxonomy.Capability{taxonomy.EncryptionAtRest},
		},
		{
			offering:     "Managed Kubernetes",
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
		},
		{
			offering:     "Managed PostgreSQL",
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.ReadReplicas},
		},
		{
			offering:     "Managed MySQL",
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.ReadReplicas},
		},
	}

	for _, tc := range tests {
		t.Run(tc.offering, func(t *testing.T) {
			t.Parallel()

			o := offeringNamed(t, cat, tc.offering)

			for _, capability := range tc.capabilities {
				if o.Has(capability) {
					t.Errorf("offering %s lists %s, want it on the tier variant that adds it alone",
						text.Quote(o.Name), text.Quote(string(capability)))
				}
			}
		})
	}
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

	t.Fatalf("the catalog has no offering called %s, want one: a test names it", text.Quote(name))

	return catalog.Offering{}
}

// TestCatalogHonoursCancelledContext pins that a cancelled run reads nothing, and
// that the error names the embedded UpCloud catalog and its file on one line.
func TestCatalogHonoursCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := upcloud.New().Catalog(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Catalog() with a cancelled context returned error %v, want one wrapping %v", err, context.Canceled)
	}

	if strings.ContainsAny(err.Error(), "\r\n") {
		t.Errorf("Catalog() error = %q, want a single line", err.Error())
	}

	for _, want := range []string{"embedded UpCloud catalog", catalogFile} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Catalog() error = %q, want it to name %q", err.Error(), want)
		}
	}
}

// TestCatalogErrorNamesNoOtherTarget pins PRD 77's rule that no error says
// "OVHcloud" or "Scaleway" when the target is UpCloud. The other adapters' own
// tests pin the other directions.
func TestCatalogErrorNamesNoOtherTarget(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := upcloud.New().Catalog(ctx)
	if err == nil {
		t.Fatalf("Catalog() with a cancelled context returned no error, want one naming the UpCloud catalog")
	}

	for _, other := range []string{"OVHcloud", "Scaleway"} {
		if strings.Contains(err.Error(), other) {
			t.Errorf("Catalog() error = %q, want no other target's name in it", err.Error())
		}
	}
}
